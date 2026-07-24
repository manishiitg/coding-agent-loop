package orchestrator

import (
	"context"
	"testing"
	"time"

	unifiedevents "github.com/manishiitg/mcpagent/events"
)

func TestWaitForTokenPersistenceDrainsTrackedWrites(t *testing.T) {
	bridge := &ContextAwareEventBridge{}
	finished := make(chan struct{})
	bridge.persistTokenUsageAsync("test", func(context.Context) error {
		time.Sleep(20 * time.Millisecond)
		close(finished)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := bridge.WaitForTokenPersistence(ctx); err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("wait returned before persistence finished")
	}
}

func TestTokenPersistenceAttributesRunLevelUsageToWorkflowOrchestrator(t *testing.T) {
	bridge := NewContextAwareEventBridge(&noopListener{}, &silentLoggerV2{})
	persister := &recordingTokenPersister{}
	bridge.SetTokenPersister(persister)
	bridge.SetIterationFolder("iteration-0")

	err := bridge.HandleEvent(context.Background(), &unifiedevents.AgentEvent{
		Type:      unifiedevents.TokenUsage,
		Timestamp: time.Now(),
		Component: "test",
		Data: &unifiedevents.TokenUsageEvent{
			ModelID:          "test-model",
			Provider:         "test-provider",
			PromptTokens:     10,
			CompletionTokens: 5,
		},
	})
	if err != nil {
		t.Fatalf("HandleEvent: %v", err)
	}

	persister.waitForCall(t, time.Second)
	calls := persister.snapshot()
	if len(calls) != 1 {
		t.Fatalf("persist calls = %d, want 1", len(calls))
	}
	if calls[0].stepTokenData == nil {
		t.Fatal("run-level usage must have explicit attribution")
	}
	if got := calls[0].stepTokenData.Phase; got != "workflow_orchestrator" {
		t.Fatalf("phase = %q, want workflow_orchestrator", got)
	}
	if got := calls[0].stepTokenData.StepID; got != "workflow_orchestrator" {
		t.Fatalf("step ID = %q, want workflow_orchestrator", got)
	}
}
