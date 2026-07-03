package virtualtools

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func TestNormalizeRequiredAbsoluteWorkspaceDocumentPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	input := filepath.Join(root, "_users", "default", "Chats", "generated-images", "hero.png")
	got, err := normalizeRequiredAbsoluteWorkspaceDocumentPath(input, "output_path")
	if err != nil {
		t.Fatalf("normalizeRequiredAbsoluteWorkspaceDocumentPath returned error: %v", err)
	}
	want := "_users/default/Chats/generated-images/hero.png"
	if got != want {
		t.Fatalf("normalized path = %q, want %q", got, want)
	}
}

func TestNormalizeRequiredAbsoluteWorkspaceDocumentPathRejectsRelative(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	_, err := normalizeRequiredAbsoluteWorkspaceDocumentPath("Chats/generated-images/hero.png", "output_path")
	if err == nil {
		t.Fatal("expected relative path to be rejected")
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("error = %q, want mention of absolute path", err.Error())
	}
}

func TestNormalizeRequiredAbsoluteWorkspaceDocumentPathRejectsOutsideWorkspaceDocs(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", filepath.Join(root, "workspace-docs"))

	_, err := normalizeRequiredAbsoluteWorkspaceDocumentPath(filepath.Join(root, "outside.png"), "output_path")
	if err == nil {
		t.Fatal("expected path outside workspace docs root to be rejected")
	}
	if !strings.Contains(err.Error(), "workspace docs root") {
		t.Fatalf("error = %q, want mention of workspace docs root", err.Error())
	}
}

func TestLLMMediaToolDefinitionsReferenceCapabilityDiscovery(t *testing.T) {
	tests := []struct {
		name       string
		tool       func() llmtypes.Tool
		capability string
	}{
		{name: "image_gen", tool: GetImageGenToolDefinition, capability: "generate_image"},
		{name: "image_edit", tool: GetImageEditToolDefinition, capability: "generate_image"},
		{name: generateVideoToolName, tool: GetGenerateVideoToolDefinition, capability: "generate_video"},
		{name: textToSpeechToolName, tool: GetTextToSpeechToolDefinition, capability: "text_to_speech"},
		{name: speechToTextToolName, tool: GetSpeechToTextToolDefinition, capability: "speech_to_text"},
		{name: generateMusicToolName, tool: GetGenerateMusicToolDefinition, capability: "generate_music"},
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
			if !strings.Contains(text, `"provider"`) || !strings.Contains(text, `"model_id"`) {
				t.Fatalf("tool definition should expose both provider and model_id: %s", text)
			}
		})
	}
}

func TestGenerateVideoToolDefinitionIncludesLastFrameInputs(t *testing.T) {
	encoded, err := json.Marshal(GetGenerateVideoToolDefinition())
	if err != nil {
		t.Fatalf("marshal generate_video tool definition: %v", err)
	}
	text := string(encoded)
	for _, field := range []string{`"last_frame"`, `"last_frame_path"`, `"last_frame_mime_type"`} {
		if !strings.Contains(text, field) {
			t.Fatalf("generate_video definition missing %s: %s", field, text)
		}
	}
	if !strings.Contains(text, "first-frame/last-frame") {
		t.Fatalf("generate_video definition should describe first-frame/last-frame interpolation: %s", text)
	}
}
