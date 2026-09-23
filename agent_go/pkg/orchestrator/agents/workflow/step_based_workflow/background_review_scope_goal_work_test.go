package step_based_workflow

import (
	"strings"
	"testing"
)

func TestGoalWorkScopeSeparatesStrategyFromArchitecture(t *testing.T) {
	goal := backgroundReviewScope{Module: "strategic_review", RunID: "p1"}
	arch := backgroundReviewScope{Module: "architecture_review", RunID: "p1"}
	if !goal.goalWork() || goal.researchOnly() {
		t.Fatal("strategic_review must be Goal Work, not research-only")
	}
	if arch.goalWork() || !arch.researchOnly() {
		t.Fatal("architecture_review stays research-only")
	}
}

func TestGoalWorkToolsFollowThePermissions(t *testing.T) {
	askAll := goalWorkPermissions{}
	for _, name := range []string{"record_pulse_goal_work", "write_workspace_file", "web_search", "create_human_input_request", "get_goal_metrics", "agent_browser"} {
		if !goalWorkToolAllowed(name, askAll) {
			t.Errorf("%s must always be available to Goal Work", name)
		}
	}
	for _, name := range []string{"execute_step", "run_full_workflow"} {
		if goalWorkToolAllowed(name, askAll) {
			t.Errorf("%s must be withheld when Run is ask", name)
		}
		if !goalWorkToolAllowed(name, goalWorkPermissions{Run: true}) {
			t.Errorf("%s must be available when Run is auto", name)
		}
	}
	for _, name := range []string{"update_message_sequence_step", "add_message_sequence_step", "update_step_config", "update_schedule", "create_schedule"} {
		if goalWorkToolAllowed(name, goalWorkPermissions{Run: true, Outward: true}) {
			t.Errorf("%s must be withheld when Change is ask", name)
		}
		if !goalWorkToolAllowed(name, goalWorkPermissions{Change: true}) {
			t.Errorf("%s must be available when Change is auto", name)
		}
	}
	all := goalWorkPermissions{Run: true, Outward: true, Change: true}
	for _, name := range []string{"delete_plan_steps", "delete_schedule", "create_plan", "set_workflow_contract_version", "run_in_background", "notify_user"} {
		if goalWorkToolAllowed(name, all) {
			t.Errorf("%s must never be available to Goal Work", name)
		}
	}
	if researchReviewToolAllowed("record_pulse_goal_work") {
		t.Error("Architecture must not record Goal Work")
	}
}

func TestPulseAutonomyPermissionsDefaults(t *testing.T) {
	cases := map[string]goalWorkPermissions{
		``:                                       {Run: true},
		`{}`:                                     {Run: true},
		`{"pulse":{"enabled":true}}`:             {Run: true},
		`{"pulse":{"autonomy":{"run":"auto"}}}`:  {Run: true},
		`{"pulse":{"autonomy":{"run":"ask"}}}`:   {},
		`{"pulse":{"autonomy":{"run":" ASK "}}}`: {},
		`{"pulse":{"autonomy":{"outward":"auto","change":"auto"}}}`:            {Run: true, Outward: true, Change: true},
		`{"pulse":{"autonomy":{"run":"ask","outward":"ask","change":"Auto"}}}`: {Change: true},
	}
	for raw, want := range cases {
		if got := pulseAutonomyPermissions(raw); got != want {
			t.Errorf("pulseAutonomyPermissions(%s) = %+v, want %+v", raw, got, want)
		}
	}
}

func TestGoalWorkPermissionInstructionsNameEachLevel(t *testing.T) {
	ask := goalWorkPermissionInstructions(goalWorkPermissions{})
	for _, want := range []string{"Run permission: ask", "Outward permission: ask", "Change permission: ask"} {
		if !strings.Contains(ask, want) {
			t.Errorf("ask instructions missing %q: %s", want, ask)
		}
	}
	auto := goalWorkPermissionInstructions(goalWorkPermissions{Run: true, Outward: true, Change: true})
	for _, want := range []string{"Run permission: auto", "Outward permission: auto", "Change permission: auto", "challenge them through a decision"} {
		if !strings.Contains(auto, want) {
			t.Errorf("auto instructions missing %q: %s", want, auto)
		}
	}
}
