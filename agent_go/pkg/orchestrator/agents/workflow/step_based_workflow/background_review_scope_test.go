package step_based_workflow

import (
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"testing"
)

func TestResearchReviewKeepsResearchAndReceiptsWithoutImplementationTools(t *testing.T) {
	names := []string{"agent_browser", "web_search", "query_workflow_db", "record_pulse_impact", "record_pulse_result", "create_human_input_request", "update_step_config", "update_workflow_config", "execute_step", "execute_workflow_db", "run_in_background"}
	tools := []llmtypes.Tool{}
	handlers := map[string]interface{}{}
	for _, name := range names {
		tools = append(tools, llmtypes.Tool{Type: "function", Function: &llmtypes.FunctionDefinition{Name: name}})
		handlers[name] = name
	}
	kept, exec := filterResearchReviewTools(tools, handlers)
	if len(kept) != 6 || len(exec) != 6 {
		t.Fatalf("wrong research tools: %+v", exec)
	}
	for _, name := range names[:6] {
		if exec[name] != name {
			t.Fatalf("research/receipt tool missing: %s", name)
		}
	}
	if len(tools) != len(names) {
		t.Fatal("filter mutated parent tool surface")
	}
	for _, id := range []string{"", "..", "../another", "one/two", "one\\two"} {
		if validateBackgroundReviewScope("strategic_review", id) == nil {
			t.Fatalf("unsafe checkpoint scope: %q", id)
		}
	}
	if err := validateBackgroundReviewScope("architecture_review", "pulse-123"); err != nil {
		t.Fatal(err)
	}
}
