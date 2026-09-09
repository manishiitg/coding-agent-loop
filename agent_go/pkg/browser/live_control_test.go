package browser

import (
	"context"
	"testing"
	"time"
)

func TestBrowserControlBlocksCommandsUntilReleased(t *testing.T) {
	release, ok := TryTakeBrowserControl("test-control")
	if !ok {
		t.Fatal("cannot take control")
	}
	defer release()
	if _, ok := TryTakeBrowserControl("test-control"); ok {
		t.Fatal("second controller admitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := AcquireBrowserAutomation(ctx, "test-control"); err == nil {
		t.Fatal("command ran during manual control")
	}
	other, ok := TryTakeBrowserControl("other-workflow")
	if !ok {
		t.Fatal("unrelated workflow blocked")
	}
	other()
	release()
	commandDone, err := AcquireBrowserAutomation(context.Background(), "test-control")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := TryTakeBrowserControl("test-control"); ok {
		t.Fatal("control interrupted in-flight command")
	}
	commandDone()
	liveGates.Lock()
	defer liveGates.Unlock()
	if len(liveGates.entries) != 0 {
		t.Fatal("finished gates leaked")
	}
}
