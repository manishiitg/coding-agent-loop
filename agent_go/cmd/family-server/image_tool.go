package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/enginedetect"
	"github.com/manishiitg/mcpagent/llm"
)

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true,
	".gif": true, ".bmp": true, ".heic": true, ".tiff": true,
}

// engineSupportsVision reports whether the engine's own CLI has genuine
// file-read + vision capability for read_image to rely on. Verified working
// (real transcriptions logged) for claude-code, codex-cli, and cursor-cli.
// Pi CLI's underlying `pi` tool has no such thing: multi-llm-provider-go's
// picli adapter has no Read/Glob tool and explicitly rejects image content
// (llmtypes.ImageContent) as unsupported — so telling it to "look at" a file
// wouldn't silently work, it just wouldn't do anything meaningful. Better to
// degrade clearly here than let the model pretend it looked.
func engineSupportsVision(engine string) bool {
	return strings.ToLower(strings.TrimSpace(engine)) != "pi-cli"
}

// bridgeEnvKeys are the process-global vars agentsession sets to put the coding
// agent in bridge-only mode (native Bash/Read/Write replaced by the sandboxed
// bridge). A nested image-reading CLI must NOT inherit them, or its native
// file-read/vision is disabled and it cannot open the image.
var bridgeEnvKeys = []string{"MCP_BRIDGE_BINARY", "MCP_API_URL", "MCP_API_TOKEN", "MCP_BRIDGE_API_URL"}

// withoutBridgeEnv runs fn with the bridge env vars cleared, then restores them,
// and additionally restores the process working directory afterwards. The nested
// image-reading CLI can leave the process cwd changed; if it does, the parent's
// warm-resumed session later notices the drift and prints a "Shell cwd was reset"
// notice that both leaks into and truncates the captured reply. Snapshotting and
// restoring cwd here keeps the parent session's working directory stable so no
// drift is ever detected. Safe because agent turns are serialized (agentTurnMu).
func withoutBridgeEnv(fn func() (string, error)) (string, error) {
	saved := map[string]string{}
	for _, k := range bridgeEnvKeys {
		if v, ok := os.LookupEnv(k); ok {
			saved[k] = v
			_ = os.Unsetenv(k)
		}
	}
	cwd, cwdErr := os.Getwd()
	defer func() {
		for k, v := range saved {
			_ = os.Setenv(k, v)
		}
		if cwdErr == nil {
			_ = os.Chdir(cwd)
		}
	}()
	return fn()
}

// readImageTool lets the agent actually SEE an image. In the bridge-only chat
// runtime the agent only reaches files through the shell (bytes, not pixels), so
// it cannot read a PNG on disk. This tool runs a separate, path-based completion
// through the SAME coding engine (whose CLI keeps its native vision and can open
// a local image file), returning a transcript + description. No OCR, no extra
// API key — it reuses the family's chosen engine.
func readImageTool(engine string) agentsession.Tool {
	return agentsession.Tool{
		Name:        "read_image",
		Description: "Look at an image file (a photo, screenshot, or scan of notes/homework/a worksheet) and get back its text and a description. Use this to actually READ uploaded images — it sees the picture, so transcribe from it instead of guessing or using OCR. Pass the workspace-relative path.",
		Category:    "family_tools",
		Params:      readImageParams,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return runReadImage(ctx, engine, args, func(string) bool { return true })
		},
	}
}

// childReadImageTool is the SAME read_image tool, restricted to paths the
// child can actually see (childCanSee: inside the current activity folder) —
// matching childOpenFile's boundary. Without this restriction the child could
// pass any workspace path (e.g. an answer key elsewhere) since readImageTool's
// own path resolution has no scoping built in.
func childReadImageTool(engine string) agentsession.Tool {
	return agentsession.Tool{
		Name:        "read_image",
		Description: "Look at a photo you (or your parent) uploaded — a scan of your work, a worksheet, a screenshot — and get back its text and a description. Use this to actually READ an uploaded image instead of guessing. Pass the workspace-relative path.",
		Category:    "family_tools",
		Params:      readImageParams,
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			return runReadImage(ctx, engine, args, childCanSee)
		},
	}
}

var readImageParams = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"path":  map[string]interface{}{"type": "string", "description": "workspace-relative path to the image"},
		"query": map[string]interface{}{"type": "string", "description": "optional: what to focus on (default: transcribe everything and describe it)"},
	},
	"required": []string{"path"},
}

func runReadImage(ctx context.Context, engine string, args map[string]interface{}, allowed func(rel string) bool) (string, error) {
	if !engineSupportsVision(engine) {
		return "", fmt.Errorf("the current AI engine (Pi CLI) can't view images directly — switch to Codex CLI, Claude Code, or Cursor CLI to read a photo/scan, or ask the parent to describe what's in it")
	}
	rel, _ := args["path"].(string)
	if !allowed(rel) {
		return "", fmt.Errorf("that image isn't available")
	}
	abs, ok := resolveWorkspacePath(rel)
	if !ok {
		return "", fmt.Errorf("invalid path")
	}
	if info, err := os.Stat(abs); err != nil || info.IsDir() {
		return "", fmt.Errorf("file not found")
	}
	if !imageExts[strings.ToLower(filepath.Ext(abs))] {
		return "", fmt.Errorf("not an image file")
	}
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		query = "Transcribe ALL the text you can see (printed and handwritten) exactly, and briefly describe any diagrams, tables, or figures."
	}
	// Pass the ABSOLUTE path as text — the coding CLI opens it with its native
	// file-read/vision ability (the designed path for CLI providers). The
	// image read runs through enginedetect.Chat with the shell/native tools
	// ENABLED (not the bridge-only chat runtime, which disables them and would
	// stop the CLI reading the file).
	prompt := "You are transcribing an image for a family learning app. Open and look at the image file at this path using your file-read/vision ability, then " + query +
		"\n\nImage path: " + abs +
		"\n\nOnly report what is genuinely visible in the image — never invent content. If it is illegible, say so."

	reply, err := withoutBridgeEnv(func() (string, error) {
		// Allow the CLI's native Read tool so it can actually open/view the
		// image file (the tmux CLI runs --permission-mode dontAsk and only
		// enables tools passed via --allowed-tools).
		return enginedetect.Chat(ctx, engine, "", workspaceRoot(),
			"You are a careful transcriber of images for a family learning app. Report only what is truly visible.",
			[]enginedetect.ChatMessage{{Role: "user", Text: prompt}},
			llm.WithAllowedTools("Read Glob"))
	})
	log.Printf("[read_image] %s err=%v chars=%d", filepath.Base(abs), err, len(reply))
	if err != nil {
		return "", fmt.Errorf("image read failed: %w", err)
	}
	if strings.TrimSpace(reply) == "" {
		return "(the image reader returned nothing)", nil
	}
	return reply, nil
}
