package server

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"
)

func TestComputeWorkflowScheduleMissedStatusClearsMissesBeforeLatestExecution(t *testing.T) {
	sched := WorkflowSchedule{
		ID:             "daily",
		CronExpression: "30 3 * * *",
		Timezone:       "UTC",
		Enabled:        true,
	}
	tracker := WorkflowScheduleExecutionTrack{
		ScheduleID:     sched.ID,
		CronExpression: sched.CronExpression,
		Timezone:       sched.Timezone,
		Enabled:        true,
		WindowStartAt:  mustParseTime(t, "2026-06-10T00:00:00Z"),
		UpdatedAt:      mustParseTime(t, "2026-06-12T03:31:00Z"),
		Executions: []WorkflowScheduleExecutionRecord{
			{StartedAt: mustParseTime(t, "2026-06-12T03:30:20Z")},
		},
	}

	got := ComputeWorkflowScheduleMissedStatus(sched, &tracker, mustParseTime(t, "2026-06-12T06:00:00Z"))
	if got.MissedRunCount != 0 || got.LatestMissedRunAt != nil {
		t.Fatalf("missed status = %+v, want no active misses after latest execution", got)
	}
}

func TestComputeWorkflowScheduleMissedStatusReportsMissesAfterLatestExecution(t *testing.T) {
	sched := WorkflowSchedule{
		ID:             "daily",
		CronExpression: "30 3 * * *",
		Timezone:       "UTC",
		Enabled:        true,
	}
	tracker := WorkflowScheduleExecutionTrack{
		ScheduleID:     sched.ID,
		CronExpression: sched.CronExpression,
		Timezone:       sched.Timezone,
		Enabled:        true,
		WindowStartAt:  mustParseTime(t, "2026-06-10T00:00:00Z"),
		UpdatedAt:      mustParseTime(t, "2026-06-10T03:31:00Z"),
		Executions: []WorkflowScheduleExecutionRecord{
			{StartedAt: mustParseTime(t, "2026-06-10T03:30:20Z")},
		},
	}

	got := ComputeWorkflowScheduleMissedStatus(sched, &tracker, mustParseTime(t, "2026-06-12T06:00:00Z"))
	if got.MissedRunCount != 2 {
		t.Fatalf("missed count = %d, want 2", got.MissedRunCount)
	}
	if got.LatestMissedRunAt == nil || !got.LatestMissedRunAt.Equal(mustParseTime(t, "2026-06-12T03:30:00Z")) {
		t.Fatalf("latest missed = %v, want 2026-06-12T03:30:00Z", got.LatestMissedRunAt)
	}
	if got.MissedRunReason != workflowScheduleMissedReasonNoExecution {
		t.Fatalf("missed reason = %q, want %q", got.MissedRunReason, workflowScheduleMissedReasonNoExecution)
	}
}

func TestEnsureWorkflowScheduleExecutionTrackerResetsWindowOnEnabledChange(t *testing.T) {
	history := &WorkflowScheduleExecutionHistoryFile{
		Version:   workflowScheduleExecutionHistoryVersion,
		Schedules: map[string]WorkflowScheduleExecutionTrack{},
	}
	sched := WorkflowSchedule{
		ID:             "daily",
		CronExpression: "30 3 * * *",
		Timezone:       "UTC",
		Enabled:        true,
	}

	createdAt := mustParseTime(t, "2026-06-10T00:00:00Z")
	tracker, changed := ensureWorkflowScheduleExecutionTracker(history, sched, createdAt)
	if !changed {
		t.Fatal("new tracker was not reported as changed")
	}
	history.Schedules[sched.ID] = tracker

	tracker.Executions = []WorkflowScheduleExecutionRecord{{StartedAt: mustParseTime(t, "2026-06-10T03:30:00Z")}}
	history.Schedules[sched.ID] = tracker

	disabledAt := mustParseTime(t, "2026-06-11T12:00:00Z")
	sched.Enabled = false
	tracker, changed = ensureWorkflowScheduleExecutionTracker(history, sched, disabledAt)
	if !changed {
		t.Fatal("disabling tracker was not reported as changed")
	}
	if tracker.Enabled {
		t.Fatal("tracker remained enabled after disabling schedule")
	}
	if !tracker.WindowStartAt.Equal(disabledAt) {
		t.Fatalf("disabled window start = %s, want %s", tracker.WindowStartAt, disabledAt)
	}
	if len(tracker.Executions) != 0 {
		t.Fatalf("executions length = %d, want 0 after enabled-state reset", len(tracker.Executions))
	}
	history.Schedules[sched.ID] = tracker

	enabledAt := mustParseTime(t, "2026-06-13T09:00:00Z")
	sched.Enabled = true
	tracker, changed = ensureWorkflowScheduleExecutionTracker(history, sched, enabledAt)
	if !changed {
		t.Fatal("re-enabling tracker was not reported as changed")
	}
	if !tracker.Enabled {
		t.Fatal("tracker remained disabled after enabling schedule")
	}
	if !tracker.WindowStartAt.Equal(enabledAt) {
		t.Fatalf("enabled window start = %s, want %s", tracker.WindowStartAt, enabledAt)
	}
}

func TestWorkflowScheduleTrackingWindowStartSurvivesEmptySchedulerState(t *testing.T) {
	workspace := httptest.NewServer(&mockWorkspaceAPI{files: map[string]string{}})
	defer workspace.Close()
	t.Setenv("WORKSPACE_API_URL", workspace.URL)
	workspacePath := "Workflow/demo"
	sched := WorkflowSchedule{
		ID:             "weekly",
		CronExpression: "0 8 * * 0",
		Timezone:       "UTC",
		Enabled:        true,
	}
	// Keep the cursor inside the implementation's seven-day retention window;
	// an empty tracker older than that is intentionally pruned.
	want := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	if err := EnsureWorkflowScheduleExecutionTracker(context.Background(), workspacePath, sched, want); err != nil {
		t.Fatal(err)
	}

	got, ok := WorkflowScheduleTrackingWindowStart(context.Background(), workspacePath, sched.ID)
	if !ok {
		t.Fatal("tracking window was not recovered")
	}
	if !got.Equal(want) {
		t.Fatalf("tracking window = %s, want %s", got, want)
	}

	service := NewSchedulerService(nil)
	sctx := &ScheduleContext{
		WorkspacePath: workspacePath,
		Schedule:      sched,
	}
	if err := service.LoadSchedule(sctx); err != nil {
		t.Fatal(err)
	}
	job := service.jobs[scheduleRuntimeKey(sctx)]
	if job == nil {
		t.Fatal("workflow cron job was not registered")
	}
	if !job.lastFired.Equal(want) {
		t.Fatalf("registered cron cursor = %s, want persisted tracking window %s", job.lastFired, want)
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("failed to parse %q: %v", value, err)
	}
	return parsed
}
