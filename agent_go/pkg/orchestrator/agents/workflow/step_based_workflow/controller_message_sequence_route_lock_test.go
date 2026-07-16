package step_based_workflow

import (
	"testing"
	"time"
)

func TestMessageSequenceRouteLockSerializesOnlyMatchingRoute(t *testing.T) {
	orchestrator := &StepBasedWorkflowOrchestrator{}
	unlockFirst := orchestrator.lockMsgSeqRoute("step/route-a")

	sameRouteAcquired := make(chan struct{})
	go func() {
		unlock := orchestrator.lockMsgSeqRoute("step/route-a")
		close(sameRouteAcquired)
		unlock()
	}()

	differentRouteAcquired := make(chan struct{})
	go func() {
		unlock := orchestrator.lockMsgSeqRoute("step/route-b")
		close(differentRouteAcquired)
		unlock()
	}()

	select {
	case <-differentRouteAcquired:
	case <-time.After(time.Second):
		t.Fatal("different message-sequence routes should execute concurrently")
	}

	select {
	case <-sameRouteAcquired:
		t.Fatal("the same stateful message-sequence route executed concurrently")
	case <-time.After(50 * time.Millisecond):
	}

	unlockFirst()
	select {
	case <-sameRouteAcquired:
	case <-time.After(time.Second):
		t.Fatal("waiting message-sequence route did not resume after its predecessor")
	}
}
