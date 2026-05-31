package step_based_workflow

import (
	"context"
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

func TestFinalizeExecStatus_Timeout(t *testing.T) {
	// 1. Timeout with context deadline exceeded
	exec := &WorkshopStepExecution{
		ID:     "exec-1",
		StepID: "step-1",
		Status: WorkshopStepRunning,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	time.Sleep(2 * time.Millisecond) // Ensure it times out
	defer cancel()

	result := ""
	execErr := ctx.Err() // context.DeadlineExceeded

	skipNotify := finalizeExecStatus(exec, ctx, &result, &execErr)
	if skipNotify {
		t.Fatal("expected skipNotify to be false")
	}
	if exec.Status != WorkshopStepFailed {
		t.Fatalf("expected status to be WorkshopStepFailed, got %v", exec.Status)
	}

	// 2. Cancellation with context canceled
	exec2 := &WorkshopStepExecution{
		ID:     "exec-2",
		StepID: "step-1",
		Status: WorkshopStepRunning,
	}
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()

	execErr2 := ctx2.Err() // context.Canceled
	skipNotify2 := finalizeExecStatus(exec2, ctx2, &result, &execErr2)
	if skipNotify2 {
		t.Fatal("expected skipNotify to be false")
	}
	if exec2.Status != WorkshopStepCancelled {
		t.Fatalf("expected status to be WorkshopStepCancelled, got %v", exec2.Status)
	}
}

