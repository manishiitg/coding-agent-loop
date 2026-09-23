package step_based_workflow

import "testing"

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

func TestGoalWorkToolsFollowTheRunPermission(t *testing.T) {
	for _, name := range []string{"record_pulse_goal_work", "write_workspace_file", "web_search", "create_human_input_request", "get_goal_metrics"} {
		if !goalWorkToolAllowed(name, false) {
			t.Errorf("%s must always be available to Goal Work", name)
		}
	}
	for _, name := range []string{"execute_step", "run_full_workflow"} {
		if goalWorkToolAllowed(name, false) {
			t.Errorf("%s must be withheld when Run is ask", name)
		}
		if !goalWorkToolAllowed(name, true) {
			t.Errorf("%s must be available when Run is auto", name)
		}
	}
	for _, name := range []string{"update_step", "add_step", "create_schedule", "update_schedule", "run_in_background", "notify_user"} {
		if goalWorkToolAllowed(name, true) {
			t.Errorf("%s must never be available to Goal Work", name)
		}
	}
	if researchReviewToolAllowed("record_pulse_goal_work") {
		t.Error("Architecture must not record Goal Work")
	}
}

func TestPulseAutonomyRunStepsDefaultsToAuto(t *testing.T) {
	cases := map[string]bool{
		``:                                       true,
		`{}`:                                     true,
		`{"pulse":{"enabled":true}}`:             true,
		`{"pulse":{"autonomy":{"run":"auto"}}}`:  true,
		`{"pulse":{"autonomy":{"run":"ask"}}}`:   false,
		`{"pulse":{"autonomy":{"run":" ASK "}}}`: false,
	}
	for raw, want := range cases {
		if got := pulseAutonomyRunSteps(raw); got != want {
			t.Errorf("pulseAutonomyRunSteps(%s) = %v, want %v", raw, got, want)
		}
	}
}
