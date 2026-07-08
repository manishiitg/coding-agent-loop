package server

import (
	"testing"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	todo_creation_human "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
)

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
	for _, n := range []string{"human_feedback", "notify_user", "submit_human_answer", "create_human_input_request", "mark_human_input_consumed"} {
		if !pool[n] || cats[n] != "human_tools" {
			t.Fatalf("workflow pool missing human tool %q (in_pool=%v cat=%q)", n, pool[n], cats[n])
		}
	}

	// human-tool name set (registerable human tools = source of truth)
	humanNames := map[string]bool{}
	for n := range virtualtools.CreateHumanToolExecutors() {
		humanNames[n] = true
	}

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

	// 4. The workshop allow-list must still expose the plan/workflow tools
	//    (guards against accidental loss during refactors).
	workshop := map[string]bool{}
	for _, n := range todo_creation_human.GetToolsForWorkshopMode("workshop") {
		workshop[n] = true
	}
	for _, n := range []string{
		"create_plan", "add_regular_step", "add_routing_step", "add_human_input_step",
		"update_regular_step", "delete_plan_steps",
		"execute_step", "harden_workflow", "create_human_input_request",
		"update_workflow_config", "update_step_config", "get_report_plan",
		"list_schedules", "update_schedule", "get_schedule_runs",
		"execute_shell_command", "diff_patch_workspace_file",
	} {
		if !workshop[n] {
			t.Fatalf("workshop allow-list missing expected tool %q", n)
		}
	}
}
