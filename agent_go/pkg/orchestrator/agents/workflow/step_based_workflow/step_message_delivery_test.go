package step_based_workflow

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func registerRunningWorkshopExecution(t *testing.T, registry *WorkshopStepRegistry, executionID, stepID string) *WorkshopStepExecution {
	t.Helper()
	exec := &WorkshopStepExecution{
		ID:        executionID,
		StepID:    stepID,
		Status:    WorkshopStepRunning,
		CreatedAt: time.Now(),
		cancel:    func() {},
	}
	registry.Register(exec)
	return exec
}

func TestWorkshopStepRegistrySendMessageRoutesExactExecution(t *testing.T) {
	registry := newWorkshopStepRegistry()
	registerRunningWorkshopExecution(t, registry, "exec-a", "step-a")
	registerRunningWorkshopExecution(t, registry, "exec-b", "step-b")

	var gotA, gotB string
	if _, err := registry.bindMessageTarget("exec-a", workshopStepMessageTarget{
		provider: "codex-cli",
		phase:    "execution",
		deliver: func(_ context.Context, message string) (string, error) {
			gotA = message
			return WorkshopStepMessageSentToCLI, nil
		},
	}); err != nil {
		t.Fatalf("bind exec-a: %v", err)
	}
	if _, err := registry.bindMessageTarget("exec-b", workshopStepMessageTarget{
		provider: "claude-code",
		phase:    "learnings",
		deliver: func(_ context.Context, message string) (string, error) {
			gotB = message
			return WorkshopStepMessageSentToCLI, nil
		},
	}); err != nil {
		t.Fatalf("bind exec-b: %v", err)
	}

	result := registry.SendMessage(context.Background(), "exec-b", "use the corrected date")
	if result.DeliveryStatus != WorkshopStepMessageSentToCLI {
		t.Fatalf("delivery status = %q, want %q (detail=%q)", result.DeliveryStatus, WorkshopStepMessageSentToCLI, result.Detail)
	}
	if result.ExecutionID != "exec-b" || result.StepID != "step-b" || result.Phase != "learnings" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if gotA != "" {
		t.Fatalf("message leaked to exec-a: %q", gotA)
	}
	if gotB != "use the corrected date" {
		t.Fatalf("exec-b received %q", gotB)
	}
}

func TestWorkshopStepRegistrySendMessageRequiresActiveAgent(t *testing.T) {
	registry := newWorkshopStepRegistry()
	registerRunningWorkshopExecution(t, registry, "exec-running", "step-running")

	result := registry.SendMessage(context.Background(), "exec-running", "hello")
	if result.DeliveryStatus != WorkshopStepMessageNoActiveAgent {
		t.Fatalf("delivery status = %q, want %q", result.DeliveryStatus, WorkshopStepMessageNoActiveAgent)
	}

	missing := registry.SendMessage(context.Background(), "missing", "hello")
	if missing.DeliveryStatus != WorkshopStepMessageNotRunning {
		t.Fatalf("missing delivery status = %q, want %q", missing.DeliveryStatus, WorkshopStepMessageNotRunning)
	}
}

func TestWorkshopStepRegistryOldTargetCleanupDoesNotClearNewPhase(t *testing.T) {
	registry := newWorkshopStepRegistry()
	registerRunningWorkshopExecution(t, registry, "exec", "step")

	oldToken, err := registry.bindMessageTarget("exec", workshopStepMessageTarget{
		phase: "execution",
		deliver: func(_ context.Context, _ string) (string, error) {
			return WorkshopStepMessageSentToCLI, nil
		},
	})
	if err != nil {
		t.Fatalf("bind old target: %v", err)
	}
	if _, err := registry.bindMessageTarget("exec", workshopStepMessageTarget{
		phase: "validation-retry",
		deliver: func(_ context.Context, _ string) (string, error) {
			return WorkshopStepMessageQueued, nil
		},
	}); err != nil {
		t.Fatalf("bind new target: %v", err)
	}

	registry.clearMessageTarget("exec", oldToken)
	result := registry.SendMessage(context.Background(), "exec", "retry with this constraint")
	if result.DeliveryStatus != WorkshopStepMessageQueued || result.Phase != "validation-retry" {
		t.Fatalf("old cleanup cleared/replaced newer target: %+v", result)
	}
}

func TestWorkshopStepRegistrySendMessageSerializesConcurrentDelivery(t *testing.T) {
	registry := newWorkshopStepRegistry()
	registerRunningWorkshopExecution(t, registry, "exec", "step")

	var active int32
	var maxActive int32
	var delivered int32
	if _, err := registry.bindMessageTarget("exec", workshopStepMessageTarget{
		phase: "execution",
		deliver: func(_ context.Context, _ string) (string, error) {
			current := atomic.AddInt32(&active, 1)
			for {
				observed := atomic.LoadInt32(&maxActive)
				if current <= observed || atomic.CompareAndSwapInt32(&maxActive, observed, current) {
					break
				}
			}
			time.Sleep(5 * time.Millisecond)
			atomic.AddInt32(&delivered, 1)
			atomic.AddInt32(&active, -1)
			return WorkshopStepMessageSentToCLI, nil
		},
	}); err != nil {
		t.Fatalf("bind target: %v", err)
	}

	const messageCount = 8
	var wg sync.WaitGroup
	for i := 0; i < messageCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := registry.SendMessage(context.Background(), "exec", "message")
			if result.DeliveryStatus != WorkshopStepMessageSentToCLI {
				t.Errorf("delivery failed: %+v", result)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&delivered); got != messageCount {
		t.Fatalf("delivered = %d, want %d", got, messageCount)
	}
	if got := atomic.LoadInt32(&maxActive); got != 1 {
		t.Fatalf("max concurrent deliveries = %d, want 1", got)
	}
}

func TestWorkshopStepRegistryTerminalLifecycleRejectsMessages(t *testing.T) {
	registry := newWorkshopStepRegistry()
	exec := registerRunningWorkshopExecution(t, registry, "exec", "step")
	if _, err := registry.bindMessageTarget("exec", workshopStepMessageTarget{
		phase: "execution",
		deliver: func(_ context.Context, _ string) (string, error) {
			return WorkshopStepMessageSentToCLI, nil
		},
	}); err != nil {
		t.Fatalf("bind target: %v", err)
	}

	resultText := "done"
	var execErr error
	if skip := finalizeExecStatus(exec, context.Background(), &resultText, &execErr); skip {
		t.Fatal("unexpected skipped completion notification")
	}

	result := registry.SendMessage(context.Background(), "exec", "too late")
	if result.DeliveryStatus != WorkshopStepMessageNotRunning || result.ExecutionStatus != string(WorkshopStepDone) {
		t.Fatalf("terminal execution accepted message: %+v", result)
	}
	snapshot, ok := registry.GetSnapshot("exec")
	if !ok {
		t.Fatal("execution snapshot missing")
	}
	if snapshot.CanReceiveMessage || snapshot.ActiveMessagePhase != "" {
		t.Fatalf("terminal snapshot remains messageable: %+v", snapshot)
	}
}

func TestWorkshopStepRegistryCancelClearsMessageTarget(t *testing.T) {
	registry := newWorkshopStepRegistry()
	registerRunningWorkshopExecution(t, registry, "exec", "step")
	if _, err := registry.bindMessageTarget("exec", workshopStepMessageTarget{
		phase: "execution",
		deliver: func(_ context.Context, _ string) (string, error) {
			return WorkshopStepMessageSentToCLI, nil
		},
	}); err != nil {
		t.Fatalf("bind target: %v", err)
	}

	if _, err := registry.Cancel("exec"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	result := registry.SendMessage(context.Background(), "exec", "too late")
	if result.DeliveryStatus != WorkshopStepMessageNotRunning || result.ExecutionStatus != string(WorkshopStepCancelled) {
		t.Fatalf("canceled execution accepted message: %+v", result)
	}
}
