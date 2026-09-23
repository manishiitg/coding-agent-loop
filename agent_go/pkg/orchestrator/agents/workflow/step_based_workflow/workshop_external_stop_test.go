package step_based_workflow

import (
	"context"
	"errors"
	"testing"
)

type stopExecutionNotifier struct {
	id    string
	step  string
	count int
}

func (*stopExecutionNotifier) OnExecutionStart(WorkshopExecutionStart)                              {}
func (*stopExecutionNotifier) OnExecutionComplete(string, string, string, map[string]string, error) {}
func (n *stopExecutionNotifier) OnExecutionTerminated(id, step string) {
	n.id, n.step = id, step
	n.count++
}

func TestWorkshopSessionStopExecutionCancelsOnlySelectedStep(t *testing.T) {
	registry := NewWorkshopStepRegistry()
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	registry.Register(&WorkshopStepExecution{ID: "first", StepID: "fetch", Status: WorkshopStepRunning, cancel: cancelFirst})
	registry.Register(&WorkshopStepExecution{ID: "second", StepID: "notify", Status: WorkshopStepRunning, cancel: cancelSecond})
	notifier := &stopExecutionNotifier{}
	session := &WorkshopChatSession{StepRegistry: registry, executionNotifier: notifier}

	stopped, err := session.StopExecution("first")
	if err != nil || stopped.Status != WorkshopStepCancelled {
		t.Fatalf("stopped = %+v, err = %v", stopped, err)
	}
	if firstCtx.Err() != context.Canceled || secondCtx.Err() != nil {
		t.Fatalf("cancelled contexts: first=%v second=%v", firstCtx.Err(), secondCtx.Err())
	}
	if notifier.count != 1 || notifier.id != "first" || notifier.step != "fetch" {
		t.Fatalf("termination notification = %+v", notifier)
	}
	if _, err := session.StopExecution("first"); !errors.Is(err, ErrWorkshopExecutionNotCancelable) {
		t.Fatalf("second stop error = %v", err)
	}
}
