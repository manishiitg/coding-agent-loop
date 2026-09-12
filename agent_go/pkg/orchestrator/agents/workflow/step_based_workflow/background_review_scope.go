package step_based_workflow

import (
	"fmt"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"strings"
)

type backgroundReviewScopeKey struct{}
type backgroundReviewScope struct{ Module, RunID string }

func (s backgroundReviewScope) researchOnly() bool {
	return s.Module == "architecture_review" || s.Module == "strategic_review"
}
func validateBackgroundReviewScope(module, runID string) error {
	if module != "technical_review" && module != "architecture_review" && module != "strategic_review" {
		return fmt.Errorf("unknown review_module %q", module)
	}
	if strings.TrimSpace(runID) == "" || strings.TrimSpace(runID) != runID || runID == "." || runID == ".." || strings.ContainsAny(runID, "/\\\x00") {
		return fmt.Errorf("review_module requires a single safe pulse_run_id")
	}
	return nil
}

// This filters workflow-owned custom tools only. Connected MCP server tools
// retain their existing configured grants; research access never expands them.
func researchReviewToolAllowed(name string) bool {
	switch name {
	case "execute_shell_command", "read_workspace_file", "list_workspace_files", "search_workspace_files", "write_workspace_file", "update_workspace_file",
		"agent_browser", "web_search", "web_fetch", "google_workspace_cli", "read_skill", "get_api_spec", "get_prompt", "get_resource",
		"query_workflow_db", "query_workflow_costs", "get_step_prompts", "get_plan_prompt_health", "get_workflow_config", "get_llm_config", "get_cost_summary",
		"list_llm_capabilities", "list_published_llms", "list_provider_models", "list_executions", "get_sub_agent_conversation", "get_route_description",
		"get_goal_metrics", "get_pulse_state", "record_pulse_finding", "record_pulse_result", "record_pulse_review_focus", "record_pulse_impact", "merge_pulse_issues",
		"get_human_input_request", "list_human_input_requests", "create_human_input_request", "resolve_run_concern", "record_pulse_module_due":
		return true
	}
	return false
}
func filterResearchReviewTools(tools []llmtypes.Tool, executors map[string]interface{}) ([]llmtypes.Tool, map[string]interface{}) {
	result := []llmtypes.Tool{}
	handlers := map[string]interface{}{}
	for _, tool := range tools {
		if tool.Function != nil && researchReviewToolAllowed(tool.Function.Name) {
			result = append(result, tool)
			if handler, ok := executors[tool.Function.Name]; ok {
				handlers[tool.Function.Name] = handler
			}
		}
	}
	return result, handlers
}
