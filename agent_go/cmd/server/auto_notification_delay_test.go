package server

import (
	"strings"
	"testing"
	"time"
)

func TestDelayedCompletionIsRetainedAndLabeledWithActualAge(t *testing.T) {
	now := time.Date(2026, 9, 13, 13, 22, 23, 0, time.UTC)
	completedAt := now.Add(-27*time.Minute - 17*time.Second)
	snap := BackgroundAgentSnapshot{
		ID: "exec-old", Name: "Explore PDFs and Build Current-FY View",
		Status: BGAgentCompleted, Result: "done", CompletedAt: &completedAt,
	}

	context := autoNotificationDelayContext(snap, now)
	if context != ", delayed (finished 27m17s ago)" {
		t.Fatalf("delay context = %q", context)
	}
	actualCompletedAt := time.Now().Add(-6 * time.Minute)
	snap.CompletedAt = &actualCompletedAt
	message := (&StreamingAPI{}).buildAutoNotificationMessage("session", snap)
	if !strings.Contains(message, "delayed (finished") {
		t.Fatalf("delayed durable completion was not labeled:\n%s", message)
	}
}

func TestFreshCompletionHasNoDelayedLabel(t *testing.T) {
	now := time.Now()
	completedAt := now.Add(-delayedAutoNotificationAfter + time.Second)
	snap := BackgroundAgentSnapshot{CompletedAt: &completedAt}
	if got := autoNotificationDelayContext(snap, now); got != "" {
		t.Fatalf("fresh completion delay context = %q, want empty", got)
	}
}

func TestNewerWorkflowStepCompletionSupersedesUndeliveredOlderRun(t *testing.T) {
	const sessionID = "schedule-cron--current-fy"
	registry := NewBackgroundAgentRegistry()
	olderAt := time.Now().Add(-10 * time.Minute)
	newerAt := olderAt.Add(8 * time.Minute)
	metadata := map[string]string{"workflow_path": "Workflow/finance", "step_id": "explore-pdfs"}
	older := &BackgroundAgent{
		ID: "exec-old", SessionID: sessionID, Status: BGAgentCompleted,
		CreatedAt: olderAt.Add(-time.Minute), CompletedAt: &olderAt, Metadata: metadata,
	}
	newer := &BackgroundAgent{
		ID: "exec-new", SessionID: sessionID, Status: BGAgentCompleted,
		CreatedAt: newerAt.Add(-time.Minute), CompletedAt: &newerAt, Metadata: metadata,
	}
	registry.Register(sessionID, older)
	registry.Register(sessionID, newer)
	api := &StreamingAPI{bgAgentRegistry: registry}

	got := api.filterSupersededCompletions(sessionID, []string{older.ID, newer.ID})
	if len(got) != 1 || got[0] != newer.ID {
		t.Fatalf("filtered completions = %#v, want only newer execution", got)
	}
	if !older.GetSnapshot().CompletionNotified {
		t.Fatal("superseded completion was not marked handled; retry sweep could replay it later")
	}
}
