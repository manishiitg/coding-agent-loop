package step_based_workflow

import (
	"context"
	"errors"
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

func TestCancelRejectsCompletedExecution(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	cancelCalled := false
	exec := &WorkshopStepExecution{
		ID:        "exec-done",
		StepID:    "step-a",
		Status:    WorkshopStepDone,
		CreatedAt: time.Now(),
		cancel:    func() { cancelCalled = true },
	}
	registry.Register(exec)

	before := exec.Snapshot()
	if before.CanCancel {
		t.Fatal("completed execution must not be advertised as cancelable")
	}

	snapshot, err := registry.Cancel(exec.ID)
	if !errors.Is(err, ErrWorkshopExecutionNotCancelable) {
		t.Fatalf("expected ErrWorkshopExecutionNotCancelable, got %v", err)
	}
	if cancelCalled {
		t.Fatal("completed execution cancel function must not be called")
	}
	if snapshot.Status != WorkshopStepDone {
		t.Fatalf("completed status changed to %v", snapshot.Status)
	}
}

func TestCancelRunningExecution(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	cancelCalled := false
	exec := &WorkshopStepExecution{
		ID:        "exec-running",
		StepID:    "step-a",
		Status:    WorkshopStepRunning,
		CreatedAt: time.Now(),
		cancel:    func() { cancelCalled = true },
	}
	registry.Register(exec)

	if !exec.Snapshot().CanCancel {
		t.Fatal("running execution should be cancelable")
	}
	snapshot, err := registry.Cancel(exec.ID)
	if err != nil {
		t.Fatalf("cancel running execution: %v", err)
	}
	if !cancelCalled {
		t.Fatal("running execution cancel function was not called")
	}
	if snapshot.Status != WorkshopStepCancelled || snapshot.CanCancel {
		t.Fatalf("unexpected canceled snapshot: status=%v can_cancel=%v", snapshot.Status, snapshot.CanCancel)
	}
}

func TestFinalizeExecStatus_Timeout(t *testing.T) {
	// 1. Timeout with context deadline exceeded
	exec := &WorkshopStepExecution{
		ID:     "exec-1",
		StepID: "step-1",
		Status: WorkshopStepRunning,
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
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
