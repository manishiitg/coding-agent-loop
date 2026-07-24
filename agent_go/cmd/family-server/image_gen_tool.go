package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/manishiitg/coding-agent-loop/agent_go/internal/agentsession"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// imageGenerationProvider picks which multi-llm-provider-go backend actually
// generates the image. Of the family's four selectable chat engines, none of
// Cursor CLI/Claude Code/Pi CLI support image generation at all — only Codex
// CLI does (via its own logged-in CLI session, spun up as a SEPARATE process
// from whatever engine is actually driving the conversation). Prefer
// Vertex/Gemini instead whenever a key for it is actually configured: it's a
// direct API call (no second CLI session to cold-start, so meaningfully
// faster and not subject to the same-turn-deadline risk a nested Codex CLI
// session runs into), and the app already resolves this same key for Pi CLI
// (see internal/enginedetect/detect.go) — same env vars, same precedence.
func imageGenerationProvider() llmproviders.Provider {
	for _, name := range []string{"GEMINI_API_KEY", "VERTEX_API_KEY", "GOOGLE_API_KEY"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return llmproviders.ProviderVertex
		}
	}
	return llmproviders.ProviderCodexCLI
}

// generateImageTool lets the agent create an illustrative picture (a diagram,
// a friendly drawing) for study material — not photo-realistic uploads, those
// come from the parent. Uses imageGenerationProvider() above to pick Codex
// CLI's own logged-in session (default, no separate API key needed — the
// same "reuse the CLI's own auth" pattern as read_image) or Vertex/Gemini
// when a key for it is configured.
func generateImageTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "generate_image",
		Description: "Generate an illustrative image (a simple diagram, a friendly drawing) from a text description, to make " +
			"study material more visual for a child. This is for illustrations you create, not for reading uploaded photos " +
			"(use read_image for that). Save it inside the activity folder next to the material it illustrates, or under " +
			"materials/<subject>/<topic>/ for a source-material illustration.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt":      map[string]interface{}{"type": "string", "description": "what to draw, in plain descriptive language"},
				"output_path": map[string]interface{}{"type": "string", "description": "workspace-relative path to save the image, e.g. <Subject>/<Topic>/<activity>/<name>.png or materials/<subject>/<topic>/<name>.png"},
			},
			"required": []string{"prompt", "output_path"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			prompt, _ := args["prompt"].(string)
			prompt = strings.TrimSpace(prompt)
			if prompt == "" {
				return "", fmt.Errorf("prompt is required")
			}
			outPath, _ := args["output_path"].(string)
			outPath = strings.TrimPrefix(strings.TrimSpace(outPath), "/")
			parts := strings.SplitN(outPath, "/", 2)
			if len(parts) < 2 || (parts[0] != "materials" && !isSubjectDir(parts[0])) {
				return "", fmt.Errorf("output_path must be inside an activity folder (<Subject>/<Topic>/<activity>/...) or materials/<subject>/<topic>/...")
			}
			abs, ok := resolveWorkspacePath(outPath)
			if !ok {
				return "", fmt.Errorf("invalid output_path")
			}

			model, err := llmproviders.InitializeImageGenerationModel(llmproviders.Config{
				Provider: imageGenerationProvider(),
				Context:  ctx,
			})
			if err != nil {
				return "", fmt.Errorf("image generation unavailable: %w", err)
			}
			resp, err := model.GenerateImages(ctx, prompt, llmtypes.WithNumberOfImages(1))
			if err != nil {
				return "", fmt.Errorf("image generation failed: %w", err)
			}
			if resp == nil || len(resp.Images) == 0 {
				return "", fmt.Errorf("image generation returned nothing")
			}

			if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
				return "", err
			}
			if err := os.WriteFile(abs, resp.Images[0].Data, 0o600); err != nil {
				return "", err
			}
			return fmt.Sprintf(`{"status":"ok","saved_path":%q,"mime_type":%q}`, outPath, resp.Images[0].MimeType), nil
		},
	}
}
