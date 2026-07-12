package orchestrator

import (
	"context"
	"testing"
	"time"
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
