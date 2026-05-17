package virtualtools

import "testing"

func TestCreateWorkspaceToolRegistryIncludesProviderMediaTools(t *testing.T) {
	registry := CreateWorkspaceToolRegistry(WorkspaceToolRegistryConfig{
		UserID:    "default",
		SessionID: "registry-test-session",
	})

	toolDefs := map[string]bool{}
	for _, tool := range registry.Tools {
		if tool.Function != nil {
			toolDefs[tool.Function.Name] = true
		}
	}

	for _, name := range []string{
		"execute_shell_command",
		"diff_patch_workspace_file",
		"read_image",
		"read_video",
		"read_pdf",
		"generate_text_llm",
		"search_web_llm",
		"image_gen",
		"image_edit",
		"generate_video",
		"text_to_speech",
		"speech_to_text",
		"generate_music",
	} {
		if !toolDefs[name] {
			t.Fatalf("registry tool definitions missing %q", name)
		}
		if _, ok := registry.Executors[name]; !ok {
			t.Fatalf("registry executors missing %q", name)
		}
		if got := registry.Categories[name]; got != GetWorkspaceAdvancedToolCategory() {
			t.Fatalf("registry category for %q = %q, want %q", name, got, GetWorkspaceAdvancedToolCategory())
		}
	}

	if got := registry.Env["MCP_SESSION_ID"]; got != "registry-test-session" {
		t.Fatalf("registry env MCP_SESSION_ID = %q, want registry-test-session", got)
	}
}
