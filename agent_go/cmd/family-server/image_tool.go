package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	"github.com/manishiitg/coding-agent-loop/agent_go/internal/enginedetect"
)

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true,
	".gif": true, ".bmp": true, ".heic": true, ".tiff": true,
}

// readImageTool lets the agent actually SEE an image. In the bridge-only chat
// runtime the agent only reaches files through the shell (bytes, not pixels), so
// it cannot read a PNG on disk. This tool runs a separate, path-based completion
// through the SAME coding engine (whose CLI keeps its native vision and can open
// a local image file), returning a transcript + description. No OCR, no extra
// API key — it reuses the family's chosen engine.
func readImageTool(engine string) agentsession.Tool {
	return agentsession.Tool{
		Name: "read_image",
		Description: "Look at an image file (a photo, screenshot, or scan of notes/homework/a worksheet) and get back its text and a description. Use this to actually READ uploaded images — it sees the picture, so transcribe from it instead of guessing or using OCR. Pass the workspace-relative path.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":  map[string]interface{}{"type": "string", "description": "workspace-relative path to the image (from list_files / shared/inbox)"},
				"query": map[string]interface{}{"type": "string", "description": "optional: what to focus on (default: transcribe everything and describe it)"},
			},
			"required": []string{"path"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			rel, _ := args["path"].(string)
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
			// Pass a path relative to the engine's working directory (the workspace)
			// so the CLI is allowed to open it.
			relPath, rerr := filepath.Rel(workspaceRoot(), abs)
			if rerr != nil {
				relPath = abs
			}
			prompt := "You are transcribing an image for a family learning app. Look at this image file in your working directory and " + query +
				"\n\nImage: " + relPath +
				"\n\nOnly report what is genuinely visible in the image — never invent content. If it is illegible, say so."

			// For claude-code, exec the CLI directly with the image path — its native
			// vision reads the local file. Going through enginedetect.Chat runs the CLI
			// in a restricted mode that can't open the file. Other engines fall back.
			if strings.EqualFold(strings.TrimSpace(engine), "claude-code") {
				cctx, cancel := context.WithTimeout(ctx, 150*time.Second)
				defer cancel()
				cmd := exec.CommandContext(cctx, "claude", "-p", prompt)
				cmd.Dir = workspaceRoot()
				out, err := cmd.CombinedOutput()
				text := strings.TrimSpace(string(out))
				if err != nil && text == "" {
					return "", fmt.Errorf("image read failed: %w", err)
				}
				if text == "" {
					return "(the image reader returned nothing)", nil
				}
				return text, nil
			}

			reply, err := enginedetect.Chat(ctx, engine, "", workspaceRoot(),
				"You are a careful transcriber of images for a family learning app. Report only what is truly visible.",
				[]enginedetect.ChatMessage{{Role: "user", Text: prompt}})
			if err != nil {
				return "", fmt.Errorf("image read failed: %w", err)
			}
			if strings.TrimSpace(reply) == "" {
				return "(the image reader returned nothing)", nil
			}
			return reply, nil
		},
	}
}
