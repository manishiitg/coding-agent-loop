package pulseintake

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckRuntimeFindsOnlyStructuredStatusDisagreements(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	run := filepath.Join(root, "Workflow", "demo", "runs", "iteration-1", "default")
	writeRuntimeFixture(t, filepath.Join(run, "run_metadata.json"), `{"status":"completed"}`)
	writeRuntimeFixture(t, filepath.Join(root, "Workflow", "demo", "runs", "run_index.json"), `{"version":1,"active_iteration":"iteration-0","retained_iterations":["iteration-1"],"last_transition":"full_run_rotation"}`)
	writeRuntimeFixture(t, filepath.Join(run, "logs", "step-a", "execution", "execution-attempt-1-iteration-1-timing.json"), `{
  "step_id":"step-a",
  "llm":{"errored_count":1,"canceled_count":0},
  "tools":{"errored_count":0,"calls":[
    {"tool_name":"shell","status":"success","result":"{\"exit_code\":2}"},
    {"tool_name":"normal","status":"success","result":{"stdout":"the word error is ordinary prose"}}
  ]}
}`)

	got := CheckRuntime("Workflow/demo", time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC))
	if got.CoverageStatus != CoverageVerified || got.RunsInspected != 1 {
		t.Fatalf("coverage/result = %+v", got)
	}
	if len(got.Findings) != 2 {
		t.Fatalf("findings = %+v, want two structured signals", got.Findings)
	}
	want := map[string]string{
		"completed_run_child_errors":           severityMedium,
		"tool_success_with_structured_failure": severityHigh,
	}
	for _, finding := range got.Findings {
		if severity, ok := want[finding.Kind]; !ok || finding.Severity != severity {
			t.Fatalf("unexpected finding or severity: %+v", finding)
		}
		delete(want, finding.Kind)
	}
	if len(want) != 0 {
		t.Fatalf("missing findings: %+v", want)
	}
}

func TestCheckRuntimeExposesAuthoritativeRunAndPlanIdentity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	runs := filepath.Join(root, "Workflow", "demo", "runs")
	writeRuntimeFixture(t, filepath.Join(runs, "run_index.json"), `{"version":1,"active_iteration":"iteration-0","retained_iterations":["iteration-8"],"last_transition":"full_run_rotation","full_run_policy":"rotate_iteration_0_to_next_available_iteration","partial_group_policy":"reuse_iteration_0"}`)
	writeRuntimeFixture(t, filepath.Join(runs, "iteration-0", "default", "run_metadata.json"), `{"status":"completed","execution_id":"exec-new","plan_revision":"plan-new","active_slot_at_start":"iteration-0"}`)
	writeRuntimeFixture(t, filepath.Join(runs, "iteration-8", "default", "run_metadata.json"), `{"status":"completed","execution_id":"exec-old","plan_revision":"plan-old","active_slot_at_start":"iteration-0"}`)

	got := CheckRuntime("Workflow/demo", time.Now())
	if got.RunIndex == nil || got.RunIndex.ActiveIteration != "iteration-0" {
		t.Fatalf("run index = %+v", got.RunIndex)
	}
	if len(got.RunIdentities) != 2 {
		t.Fatalf("run identities = %+v", got.RunIdentities)
	}
	roles := map[string]string{}
	plans := map[string]string{}
	for _, identity := range got.RunIdentities {
		roles[identity.RunFolder] = identity.LifecycleRole
		plans[identity.RunFolder] = identity.PlanRevision
	}
	if roles["iteration-0/default"] != "active" || roles["iteration-8/default"] != "retained" {
		t.Fatalf("roles = %+v", roles)
	}
	if plans["iteration-0/default"] != "plan-new" || plans["iteration-8/default"] != "plan-old" {
		t.Fatalf("plans = %+v", plans)
	}
}

func TestCheckRuntimeFlagsExplicitNonCompletion(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	writeRuntimeFixture(t, filepath.Join(root, "Workflow", "demo", "runs", "iteration-1", "default", "run_metadata.json"), `{"status":"failed"}`)
	got := CheckRuntime("Workflow/demo", time.Now())
	if len(got.Findings) != 1 || got.Findings[0].Kind != "run_not_completed" {
		t.Fatalf("findings = %+v", got.Findings)
	}
}

func TestCheckRuntimeDoesNotTreatMissingRunsAsClean(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	got := CheckRuntime("Workflow/demo", time.Now())
	if got.CoverageStatus != CoverageNotInstrumented || len(got.Findings) != 0 {
		t.Fatalf("result = %+v", got)
	}
}

func writeRuntimeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
