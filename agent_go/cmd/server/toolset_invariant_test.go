package server

import (
	"testing"

	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	todo_creation_human "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
)

func knownWorkshopRegisteredToolNamesOutsideWorkflowPool() map[string]string {
	registered := map[string]string{}
	add := func(source string, names ...string) {
		for _, name := range names {
			registered[name] = source
		}
	}

	add("mcpagent virtual tools", "get_api_spec", "get_prompt", "get_resource")
	add("execution sub-agent/session tools", "call_sub_agent", "call_generic_agent", "get_sub_agent_conversation", "get_route_description")
	add("conditional workspace browser tools", "agent_browser")
	add("server secret management tools",
		"list_secrets", "set_workflow_secret", "delete_workflow_secret",
		"set_user_secret", "delete_user_secret",
	)
	add("auto-improvement context tools", "capture_context")
	add("workshop plan tools",
		"create_plan",
		"add_regular_step", "add_message_sequence_step", "add_routing_step",
		"add_human_input_step", "add_todo_task_step", "add_todo_task_route",
		"update_regular_step", "update_message_sequence_step", "update_routing_step",
		"update_human_input_step", "update_todo_task_step", "update_todo_task_route",
		"delete_todo_task_route", "delete_plan_steps", "cleanup_orphan_step_configs",
		"update_validation_schema",
	)
	add("workshop execution tools",
		"execute_step", "query_step", "send_step_message", "debug_step", "list_executions",
		"stop_step", "stop_all_executions", "run_in_background",
		"run_full_workflow", "run_goal_advisor_review",
	)
	add("workshop review/maintenance tools",
		"update_step_config", "get_step_prompts",
		"review_plan", "mark_changelog_artifact_reviewed",
		"review_workflow_timing", "review_workflow_costs", "review_step_code",
		"get_cost_summary",
		"run_full_evaluation", "validate_evaluation_plan",
	)
	add("workshop workflow/config tools",
		"get_llm_config", "get_workflow_config", "update_workflow_config",
		"update_variable", "add_group", "update_group", "delete_group",
		"list_schedules", "create_schedule", "create_calendar_schedule",
		"update_schedule", "delete_schedule", "trigger_schedule", "get_schedule_runs",
		"list_skills", "import_skill", "uninstall_skill", "search_skills", "install_skill",
		"list_published_llms", "list_provider_models", "test_llm", "set_workflow_llm_config",
	)
	add("workshop report tools",
		"get_report_plan", "upsert_report_widget", "remove_report_widget",
		"move_report_widget", "toggle_report_widget", "set_report_theme",
		"set_section_layout", "validate_report_plan", "preview_report_render",
	)
	add("workshop guidance/status tools",
		"get_workflow_command_guidance", "get_reference_doc",
		"mark_cos_recommendation_status",
	)

	return registered
}

// TestToolSetInvariants guards the bug class that hid notify_user: a tool that is
// allow-listed for a mode but never registered (or vice versa), and accidental
// loss of plan/workflow tools from the workshop allow-list.
func TestToolSetInvariants(t *testing.T) {
	// 1. The workflow tool pool (workflowMode=true) must register every human tool.
	tools, _, cats := createCustomTools(true, "default", "invariant-test")
	pool := map[string]bool{}
	for _, tl := range tools {
		if tl.Function != nil {
			pool[tl.Function.Name] = true
		}
	}
	for _, n := range []string{"human_feedback", "notify_user", "create_human_input_request", "mark_human_input_consumed"} {
		if !pool[n] || cats[n] != "human_tools" {
			t.Fatalf("workflow pool missing human tool %q (in_pool=%v cat=%q)", n, pool[n], cats[n])
		}
	}
	if pool["submit_human_answer"] {
		t.Fatal("workflow pool still exposes removed submit_human_answer tool")
	}

	// human-tool name set (registerable human tools = source of truth)
	humanNames := map[string]bool{}
	for n := range virtualtools.CreateHumanToolExecutors() {
		humanNames[n] = true
	}
	knownOutsidePool := knownWorkshopRegisteredToolNamesOutsideWorkflowPool()

	for _, mode := range []string{"workshop", "run"} {
		allow := todo_creation_human.GetToolsForWorkshopMode(mode)
		allowSet := map[string]bool{}
		for _, n := range allow {
			allowSet[n] = true
		}

		// 2. INVARIANT: every allow-listed HUMAN tool must be registerable in the
		//    workflow pool. (This is exactly what broke for notify_user.)
		for _, n := range allow {
			if humanNames[n] && !pool[n] {
				t.Fatalf("mode=%s: human tool %q is allow-listed but NOT in the workflow pool (would be invisible via get_api_spec)", mode, n)
			}
		}

		// Broad invariant: if a mode allow-lists a tool, it must have a real
		// registration path. Most tools come from the workflow pool; workshop
		// custom/guidance/virtual tools are registered separately and enumerated
		// above. This guards the same bug class as notify_user and Pulse state
		// tools for future additions.
		for _, n := range allow {
			if pool[n] {
				continue
			}
			if _, ok := knownOutsidePool[n]; ok {
				continue
			}
			t.Fatalf("mode=%s: allow-listed tool %q is neither in workflow pool nor known workshop/virtual registrations", mode, n)
		}

		// 3. Non-blocking human/report tools must be usable in both modes.
		for _, n := range virtualtools.WorkshopHumanToolNames() {
			if !pool[n] || cats[n] != "human_tools" {
				t.Fatalf("workflow pool missing workshop human tool %q (in_pool=%v cat=%q)", n, pool[n], cats[n])
			}
			if !allowSet[n] {
				t.Fatalf("mode=%s: expected %q in allow-list", mode, n)
			}
		}
		if allowSet["human_feedback"] {
			t.Fatalf("mode=%s: blocking human_feedback must not be exposed to the workflow builder; ask in chat or use workflow runtime human-input steps instead", mode)
		}
	}

	// 4. Pulse worklist tools are registered in the workflow tool pool and must
	//    be visible in workshop mode, otherwise scheduled Pulse turns can ask
	//    for mark_pulse_module_result/get_pulse_module_state and then fail at
	//    runtime with "not callable in this chat session".
	for _, n := range []string{"get_pulse_module_state", "record_pulse_worklist", "mark_pulse_module_result", "mark_pulse_final_command_result"} {
		if !pool[n] || cats[n] != "workflow" {
			t.Fatalf("workflow pool missing Pulse state tool %q (in_pool=%v cat=%q)", n, pool[n], cats[n])
		}
	}

	// 5. The workshop allow-list must still expose the plan/workflow tools
	//    (guards against accidental loss during refactors).
	workshop := map[string]bool{}
	for _, n := range todo_creation_human.GetToolsForWorkshopMode("workshop") {
		workshop[n] = true
	}
	for _, n := range []string{
		"create_plan", "add_regular_step", "add_routing_step", "add_human_input_step",
		"update_regular_step", "delete_plan_steps",
		"execute_step", "create_human_input_request",
		"update_workflow_config", "update_step_config", "get_report_plan",
		"list_schedules", "update_schedule", "get_schedule_runs",
		"execute_shell_command", "diff_patch_workspace_file",
		"get_pulse_module_state", "record_pulse_worklist", "mark_pulse_module_result", "mark_pulse_final_command_result",
		"mark_changelog_artifact_reviewed",
	} {
		if !workshop[n] {
			t.Fatalf("workshop allow-list missing expected tool %q", n)
		}
	}
	for _, removed := range []string{"improve_learnings", "improve_kb", "improve_db", "review_artifact_sync"} {
		if workshop[removed] {
			t.Fatalf("workshop allow-list still exposes removed dedicated maintenance tool %q", removed)
		}
	}

	run := map[string]bool{}
	for _, n := range todo_creation_human.GetToolsForWorkshopMode("run") {
		run[n] = true
	}
	for _, n := range []string{"record_pulse_worklist", "mark_pulse_module_result", "mark_pulse_final_command_result", "mark_changelog_artifact_reviewed"} {
		if run[n] {
			t.Fatalf("run allow-list must not expose Pulse mutation tool %q", n)
		}
	}
}
