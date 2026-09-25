package server

import (
	"context"
	"strings"
	"testing"
)

// A full Pulse and a fix run on one workflow must not run at once: on
// 2026-09-25 both started on salesoutreach 1.2s apart and worked the same issue.
func TestFullPulseAndFixRunExcludeEachOther(t *testing.T) {
	_, _ = newScheduleRunWorkspaceStub(t)
	ws := "Workflow/exclusive"
	if err := WriteWorkflowManifest(context.Background(), ws, &WorkflowManifest{ID: "exclusive", Label: "exclusive"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ running, trigger string }{
		{manualWorkflowPulseScheduleID, "fix"},
		{pulseFixRunScheduleID, "full"},
	} {
		svc := &SchedulerService{runtimeStates: map[string]*ScheduleRuntimeState{
			workflowScheduleRuntimeKey(ws, tc.running): {LastStatus: "running", LastSessionID: "session-" + tc.running},
		}}
		var err error
		if tc.trigger == "fix" {
			_, err = svc.TriggerPulseFixRun(ws, "1 open issue(s)")
		} else {
			_, err = svc.TriggerPulseNow(ws)
		}
		if err == nil || !strings.Contains(err.Error(), "already running") {
			t.Errorf("%s run started while %s was running: %v", tc.trigger, tc.running, err)
		}
	}
}

// Only Pulse runs count toward the server-wide Pulse cap; normal schedules
// and finished runs do not.
func TestRunningPulseRunsCountsOnlyActivePulseWork(t *testing.T) {
	svc := &SchedulerService{runtimeStates: map[string]*ScheduleRuntimeState{
		workflowScheduleRuntimeKey("Workflow/a", manualWorkflowPulseScheduleID): {LastStatus: "running"},
		workflowScheduleRuntimeKey("Workflow/b", pulseFixRunScheduleID):         {LastStatus: "running"},
		workflowScheduleRuntimeKey("Workflow/c", pulseFixRunScheduleID):         {LastStatus: "success"},
		workflowScheduleRuntimeKey("Workflow/d", "daily-digest"):                {LastStatus: "running"},
	}}
	if got := svc.runningPulseRuns(); got != 2 {
		t.Fatalf("running Pulse runs = %d, want 2", got)
	}
	if svc.runningPulseRuns() < maxConcurrentPulseRuns {
		t.Fatal("two running Pulse runs must reach the cap")
	}
}
