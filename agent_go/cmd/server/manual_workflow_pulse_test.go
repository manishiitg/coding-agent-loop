package server

import "testing"

func TestManualWorkflowPulseScheduleContract(t *testing.T) {
	if manualWorkflowPulseScheduleID == "" {
		t.Fatal("manual workflow Pulse schedule id must not be empty")
	}

	manifest := &WorkflowManifest{ID: "workflow-id", Label: "Workflow"}
	sched := WorkflowSchedule{
		ID:           manualWorkflowPulseScheduleID,
		Name:         "Run workflow + Pulse",
		Mode:         "workshop",
		WorkshopMode: "run",
	}
	sctx := buildScheduleContext("/tmp/workflow", manifest, sched)
	sctx.TriggerSource = "manual"
	sctx.ForcePostRunMonitor = true

	if sctx.Schedule.Mode != "workshop" || sctx.Schedule.WorkshopMode != "run" {
		t.Fatalf("manual Pulse must use the normal workshop run path: %+v", sctx.Schedule)
	}
	if !sctx.ForcePostRunMonitor {
		t.Fatal("manual workflow run must force post-run Pulse without changing workflow config")
	}
	if len(sctx.Schedule.Messages) != 0 {
		t.Fatal("empty messages must select the scheduler's default full-workflow message")
	}
	if !shouldRunPostRunMonitor(sctx, manifest) {
		t.Fatal("one-off run must execute Pulse even when post_run_monitor is disabled")
	}

	sctx.ForcePostRunMonitor = false
	if shouldRunPostRunMonitor(sctx, manifest) {
		t.Fatal("ordinary run must respect the disabled post_run_monitor setting")
	}
}
