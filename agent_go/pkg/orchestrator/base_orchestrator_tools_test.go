package orchestrator

import (
	"testing"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

func TestWorkspaceAdvancedCategoryIncludesProviderMediaTools(t *testing.T) {
	names := getToolNamesByCategory("workspace_advanced")

	for _, name := range []string{
		"read_image",
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

func TestBaseOrchestratorPreRegistersDelegationCategoriesWithoutMutatingInput(t *testing.T) {
	input := map[string]string{"existing": "workspace"}
	base, err := NewBaseOrchestrator(
		loggerv2.NewNoop(), nil, OrchestratorTypeWorkflow, "", 0, "",
		nil, nil, false, &LLMConfig{}, 1, nil, nil, input,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(input) != 1 {
		t.Fatalf("constructor mutated caller category map: %#v", input)
	}
	wantCategory := virtualtools.GetSubAgentToolCategory()
	for _, tool := range virtualtools.CreateSubAgentTools() {
		if got := base.ToolCategories[tool.Function.Name]; got != wantCategory {
			t.Fatalf("category for %s=%q, want %q", tool.Function.Name, got, wantCategory)
		}
	}
}
