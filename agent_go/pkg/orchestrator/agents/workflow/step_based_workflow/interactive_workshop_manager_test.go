package step_based_workflow

import (
	"context"
	"testing"
	"time"
)

func TestWorkshopSubAgentNotifierRegistersCancelableExecution(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	notifier := &workshopSubAgentNotifier{registry: registry}

	cancelCalled := make(chan struct{}, 1)
	cancel := func() {
		select {
		case cancelCalled <- struct{}{}:
		default:
		}
	}

	notifier.OnSubAgentStart(WorkshopExecutionStart{
		ID:     "todo-sub-step-9-functional-test-agent",
		Name:   "Functional Test Agent",
		Cancel: cancel,
	})

	exec := registry.Get("todo-sub-step-9-functional-test-agent")
	if exec == nil {
		t.Fatal("expected execution to be registered")
	}
	if exec.cancel == nil {
		t.Fatal("expected registered execution to keep its cancel function")
	}

	cancelled := registry.CancelAll()
	if len(cancelled) != 1 {
		t.Fatalf("expected 1 cancelled execution, got %d", len(cancelled))
	}

	select {
	case <-cancelCalled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected cancel function to be invoked")
	}
}

func TestWorkshopStepRegistryCancelAllMarksLegacyExecutionsCancelled(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	registry.Register(&WorkshopStepExecution{
		ID:     "legacy-sub-agent",
		StepID: "Legacy Sub-Agent",
		Status: WorkshopStepRunning,
		cancel: nil,
	})

	cancelled := registry.CancelAll()
	if len(cancelled) != 1 {
		t.Fatalf("expected 1 cancelled execution, got %d", len(cancelled))
	}

	exec := registry.Get("legacy-sub-agent")
	if exec == nil {
		t.Fatal("expected legacy execution to remain registered")
	}
	if exec.Status != WorkshopStepCancelled {
		t.Fatalf("expected legacy execution status %q, got %q", WorkshopStepCancelled, exec.Status)
	}
}

func TestWorkshopStepRegistryCancelReturnsRawExecutionSnapshot(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	cancelCalled := make(chan struct{}, 1)
	registry.Register(&WorkshopStepExecution{
		ID:     "exec-step-123",
		StepID: "step-123",
		Status: WorkshopStepRunning,
		cancel: func() {
			select {
			case cancelCalled <- struct{}{}:
			default:
			}
		},
	})

	snap, err := registry.Cancel("exec-step-123")
	if err != nil {
		t.Fatalf("expected cancel to succeed, got %v", err)
	}
	if snap.ID != "exec-step-123" {
		t.Fatalf("expected raw execution ID, got %q", snap.ID)
	}
	if snap.StepID != "step-123" {
		t.Fatalf("expected step ID to be preserved, got %q", snap.StepID)
	}
	if snap.Status != WorkshopStepCancelled {
		t.Fatalf("expected cancelled status, got %q", snap.Status)
	}

	select {
	case <-cancelCalled:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected cancel function to be invoked")
	}
}

func TestCompositeSubAgentNotifierForwardsCancelFunc(t *testing.T) {
	var gotCancel context.CancelFunc
	notifier := &recordingSubAgentNotifier{
		onStart: func(_ string, _ string, cancel context.CancelFunc) {
			gotCancel = cancel
		},
	}
	cancel := func() {}

	composite := ChainSubAgentNotifiers(notifier)
	composite.OnSubAgentStart(WorkshopExecutionStart{
		ID:     "agent-1",
		Name:   "Agent 1",
		Cancel: cancel,
	})

	if gotCancel == nil {
		t.Fatal("expected composite notifier to forward cancel func")
	}
}

type recordingSubAgentNotifier struct {
	onStart func(agentID, name string, cancel context.CancelFunc)
}

func (r *recordingSubAgentNotifier) OnSubAgentStart(start WorkshopExecutionStart) {
	if r.onStart != nil {
		r.onStart(start.ID, start.Name, start.Cancel)
	}
}

func (r *recordingSubAgentNotifier) OnSubAgentComplete(agentID, name, result string, err error) {}
