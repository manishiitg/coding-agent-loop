package orchestrator

import "testing"

func TestWorkspaceAdvancedCategoryIncludesProviderMediaTools(t *testing.T) {
	names := getToolNamesByCategory("workspace_advanced")

	for _, name := range []string{
		"read_image",
		"read_video",
		"search_web_llm",
		"image_gen",
		"image_edit",
		"generate_video",
		"text_to_speech",
		"speech_to_text",
		"generate_music",
	} {
		if !names[name] {
			t.Fatalf("workspace_advanced category missing %q", name)
		}
	}
}
