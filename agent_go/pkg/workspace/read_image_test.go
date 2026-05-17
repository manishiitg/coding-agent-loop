package workspace

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestNormalizeReadImageAbsolutePath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	absoluteInput := filepath.Join(root, "_users", "default", "Chats", "sample.png")
	absolutePath, guardPath, err := normalizeReadImageAbsolutePath(absoluteInput)
	if err != nil {
		t.Fatalf("normalizeReadImageAbsolutePath returned error: %v", err)
	}
	if absolutePath != absoluteInput {
		t.Fatalf("absolutePath = %q, want %q", absolutePath, absoluteInput)
	}
	wantGuardPath := filepath.Join("_users", "default", "Chats", "sample.png")
	if guardPath != wantGuardPath {
		t.Fatalf("guardPath = %q, want %q", guardPath, wantGuardPath)
	}
}

func TestNormalizeReadImageAbsolutePathRejectsRelativePath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	_, _, err := normalizeReadImageAbsolutePath("Downloads/sample.png")
	if err == nil {
		t.Fatal("expected relative read_image path to be rejected")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %q, want mention of absolute path", err.Error())
	}
}

func TestNormalizeReadImageAbsolutePathRejectsOutsideWorkspaceDocs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace-docs")
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	_, _, err := normalizeReadImageAbsolutePath(filepath.Join(t.TempDir(), "sample.png"))
	if err == nil {
		t.Fatal("expected path outside workspace-docs to be rejected")
	}
	if !strings.Contains(err.Error(), "workspace docs root") {
		t.Fatalf("error = %q, want mention of workspace docs root", err.Error())
	}
}

func TestReadImageRejectsRelativePathBeforeWorkspaceAPI(t *testing.T) {
	client := NewClient("http://127.0.0.1:1")
	_, err := client.ReadImage(context.Background(), ReadImageParams{
		Filepath: "Downloads/sample.png",
		Query:    "What is in this image?",
	})
	if err == nil {
		t.Fatal("expected relative read_image path to be rejected before API call")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %q, want mention of absolute path", err.Error())
	}
}

func TestReadVideoRejectsRelativePathBeforeWorkspaceAPI(t *testing.T) {
	client := NewClient("http://127.0.0.1:1")
	_, err := client.ReadVideo(context.Background(), ReadVideoParams{
		Filepath: "Downloads/sample.mp4",
		Query:    "Summarize this video.",
	})
	if err == nil {
		t.Fatal("expected relative read_video path to be rejected before API call")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %q, want mention of absolute path", err.Error())
	}
}

func TestReadPDFRejectsRelativePathBeforeWorkspaceAPI(t *testing.T) {
	client := NewClient("http://127.0.0.1:1")
	_, err := client.ReadPDF(context.Background(), ReadPDFParams{
		Filepath: "Downloads/sample.pdf",
	})
	if err == nil {
		t.Fatal("expected relative read_pdf path to be rejected before API call")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %q, want mention of absolute path", err.Error())
	}
}

func TestLLMBackedToolDefinitionsReferenceCapabilityDiscovery(t *testing.T) {
	tests := []struct {
		name       string
		tool       llmToolDefinition
		capability string
		wantFields []string
	}{
		{
			name:       "read_image",
			tool:       imageToolDef,
			capability: "read_image",
			wantFields: []string{"provider", "model_id"},
		},
		{
			name:       "read_video",
			tool:       videoReadToolDef,
			capability: "read_video",
			wantFields: []string{"provider", "model_id"},
		},
		{
			name:       "search_web_llm",
			tool:       searchWebLLMToolDef,
			capability: "search_web",
			wantFields: []string{"provider", "model_id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := json.Marshal(tt.tool())
			if err != nil {
				t.Fatalf("marshal tool definition: %v", err)
			}
			text := string(encoded)
			if !strings.Contains(text, `list_llm_capabilities(capability=\"`+tt.capability+`\", include_models=true)`) {
				t.Fatalf("tool definition missing capability discovery instruction for %s: %s", tt.capability, text)
			}
			for _, field := range tt.wantFields {
				if !strings.Contains(text, `"`+field+`"`) {
					t.Fatalf("tool definition missing field %q: %s", field, text)
				}
			}
		})
	}
}

type llmToolDefinition func() llmtypes.Tool
