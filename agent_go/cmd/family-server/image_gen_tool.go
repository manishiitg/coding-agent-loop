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

// generateImageTool lets the agent create an illustrative picture (a diagram,
// a friendly drawing) for study material — not photo-realistic uploads, those
// come from the parent. Routed through the local Codex CLI's native image
// generation (multi-llm-provider-go's codex-cli provider), which uses the
// CLI's own logged-in session — no separate image-generation API key needed,
// the same "reuse the CLI's own auth" pattern as read_image.
func generateImageTool() agentsession.Tool {
	return agentsession.Tool{
		Name: "generate_image",
		Description: "Generate an illustrative image (a simple diagram, a friendly drawing) from a text description, to make " +
			"study material more visual for a child. This is for illustrations you create, not for reading uploaded photos " +
			"(use read_image for that). Save it under shared/ next to the material it illustrates.",
		Category: "family_tools",
		Params: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prompt":      map[string]interface{}{"type": "string", "description": "what to draw, in plain descriptive language"},
				"output_path": map[string]interface{}{"type": "string", "description": "workspace-relative path to save the image (under shared/), e.g. shared/study/<subject>/<topic>/<name>.png"},
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
			if !strings.HasPrefix(outPath, "shared/") {
				return "", fmt.Errorf("output_path must be under shared/")
			}
			abs, ok := resolveWorkspacePath(outPath)
			if !ok {
				return "", fmt.Errorf("invalid output_path")
			}

			model, err := llmproviders.InitializeImageGenerationModel(llmproviders.Config{
				Provider: llmproviders.ProviderCodexCLI,
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
