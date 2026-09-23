package step_based_workflow

import (
	"encoding/json"
	"fmt"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"strings"
)

type backgroundReviewScopeKey struct{}
type backgroundReviewScope struct {
	Module, RunID string
	// RunSteps is the workflow's pulse.autonomy.run permission for Goal Work:
	// true (the default, "auto") lets it run existing steps and routes itself.
	RunSteps bool
}

// researchOnly is Architecture: research and propose, never act.
func (s backgroundReviewScope) researchOnly() bool {
	return s.Module == "architecture_review"
}

// goalWork is the strategic_review module in its Goal Work role: it does
// goal-advancing work for the user instead of only proposing it. It prepares
// work in pulse/work/ and, when RunSteps, runs existing workflow steps.
func (s backgroundReviewScope) goalWork() bool {
	return s.Module == "strategic_review"
}

// goalWorkRunTools are the workflow execution tools Goal Work may use when the
// workflow's Run permission is auto. Plan/schedule edits stay withheld.
var goalWorkRunTools = map[string]bool{
	"execute_step": true, "query_step": true, "send_step_message": true, "stop_step": true,
	"run_full_workflow": true, "list_executions": true,
}

func goalWorkToolAllowed(name string, runSteps bool) bool {
	return researchReviewToolAllowed(name) || name == "record_pulse_goal_work" || (runSteps && goalWorkRunTools[name])
}

func filterGoalWorkTools(tools []llmtypes.Tool, executors map[string]interface{}, runSteps bool) ([]llmtypes.Tool, map[string]interface{}) {
	result := []llmtypes.Tool{}
	handlers := map[string]interface{}{}
	for _, tool := range tools {
		if tool.Function != nil && goalWorkToolAllowed(tool.Function.Name, runSteps) {
			result = append(result, tool)
			if handler, ok := executors[tool.Function.Name]; ok {
				handlers[tool.Function.Name] = handler
			}
		}
	}
	return result, handlers
}

// pulseAutonomyRunSteps reads workflow.json pulse.autonomy.run. Only an
// explicit "ask" turns step running off; missing or unreadable means auto.
func pulseAutonomyRunSteps(manifestJSON string) bool {
	var manifest struct {
		Pulse *struct {
			Autonomy *struct {
				Run string `json:"run"`
			} `json:"autonomy"`
		} `json:"pulse"`
	}
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil || manifest.Pulse == nil || manifest.Pulse.Autonomy == nil {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(manifest.Pulse.Autonomy.Run), "ask")
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
		"get_goal_metrics", "get_pulse_state", "record_pulse_finding", "record_pulse_result", "merge_pulse_issues",
		"get_human_input_request", "list_human_input_requests", "create_human_input_request", "resolve_run_concern":
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

// Generic read-only execution replaces specialized read-only reviewer launchers.
type backgroundReadOnlyKey struct{}

func parseBackgroundReadOnlyAccess(args map[string]interface{}, agentType string) (bool, error) {
	value, present := args["access_mode"]
	if !present {
		return false, nil
	}
	mode, ok := value.(string)
	if !ok || (mode != "read_write" && mode != "read_only") {
		return false, fmt.Errorf("access_mode must be read_write or read_only")
	}
	if mode == "read_only" && agentType != "executor" {
		return false, fmt.Errorf("read_only requires an executor")
	}
	return mode == "read_only", nil
}
func readOnlyBackgroundToolAllowed(name string) bool {
	switch name {
	case "execute_shell_command", "read_workspace_file", "list_workspace_files", "search_workspace_files", "read_skill", "get_api_spec", "get_prompt", "get_resource",
		"query_workflow_db", "query_workflow_costs", "get_step_prompts", "get_plan_prompt_health", "get_workflow_config", "get_llm_config", "get_cost_summary",
		"list_executions", "get_sub_agent_conversation", "get_route_description", "get_goal_metrics", "get_pulse_state":
		return true
	}
	return false
}
func filterReadOnlyBackgroundTools(tools []llmtypes.Tool, executors map[string]interface{}) ([]llmtypes.Tool, map[string]interface{}) {
	result := []llmtypes.Tool{}
	handlers := map[string]interface{}{}
	for _, tool := range tools {
		if tool.Function != nil && readOnlyBackgroundToolAllowed(tool.Function.Name) {
			result = append(result, tool)
			if handler, ok := executors[tool.Function.Name]; ok {
				handlers[tool.Function.Name] = handler
			}
		}
	}
	return result, handlers
}
