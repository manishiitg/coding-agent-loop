package step_based_workflow

import (
	"testing"
	"time"
)

func TestLatestSnapshotForStepPrefersNewestRunningExecution(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	now := time.Now()

	registry.Register(&WorkshopStepExecution{
		ID:        "exec-step-a-old-done",
		StepID:    "step-a",
		Status:    WorkshopStepDone,
		CreatedAt: now.Add(-2 * time.Minute),
	})
	registry.Register(&WorkshopStepExecution{
		ID:        "exec-step-a-old-running",
		StepID:    "step-a",
		Status:    WorkshopStepRunning,
		CreatedAt: now.Add(-time.Minute),
	})
	registry.Register(&WorkshopStepExecution{
		ID:        "exec-step-a-new-running",
		StepID:    "step-a",
		Status:    WorkshopStepRunning,
		CreatedAt: now,
	})

	snapshot, ok, matches := registry.LatestSnapshotForStep("step-a")
	if !ok {
		t.Fatal("expected a matching execution")
	}
	if snapshot.ID != "exec-step-a-new-running" {
		t.Fatalf("expected newest running execution, got %q", snapshot.ID)
	}
	if len(matches) != 3 {
		t.Fatalf("expected three matches, got %d", len(matches))
	}
}

func TestLatestSnapshotForStepFallsBackToCompletedExecution(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	now := time.Now()

	registry.Register(&WorkshopStepExecution{
		ID:        "exec-step-a-old-done",
		StepID:    "step-a",
		Status:    WorkshopStepDone,
		CreatedAt: now.Add(-time.Minute),
	})
	registry.Register(&WorkshopStepExecution{
		ID:        "exec-step-a-new-failed",
		StepID:    "step-a",
		Status:    WorkshopStepFailed,
		CreatedAt: now,
	})

	snapshot, ok, _ := registry.LatestSnapshotForStep("step-a")
	if !ok {
		t.Fatal("expected a matching execution")
	}
	if snapshot.ID != "exec-step-a-new-failed" {
		t.Fatalf("expected newest non-running execution, got %q", snapshot.ID)
	}
}
