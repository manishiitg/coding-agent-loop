package services

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type progressTestConnector struct {
	testBotConnector
	updates   []string
	deletes   []string
	deleteErr error
}

func (c *progressTestConnector) UpdateMessage(_ context.Context, _ ThreadID, id, text string) error {
	c.updates = append(c.updates, id+":"+text)
	return nil
}
func (c *progressTestConnector) DeleteMessage(_ context.Context, _ ThreadID, id string) error {
	c.deletes = append(c.deletes, id)
	return c.deleteErr
}

func TestSlackProgressUsesOneMessageAndClearsOnCompletion(t *testing.T) {
	c := &progressTestConnector{}
	f := NewBotEventFilter(c, ThreadID{Platform: "slack"}, "session", "", "user")
	f.sendProgressMessage(context.Background(), "Working…")
	f.sendProgressMessage(context.Background(), "Reading results…")
	if len(c.sent) != 1 || len(c.updates) != 1 {
		t.Fatalf("progress appended duplicate statuses: %#v", c)
	}
	f.completionReceived = true
	f.checkSessionDone("unified_completion")
	if len(c.deletes) != 1 || c.deletes[0] != "msg" {
		t.Fatal("completion did not clear progress")
	}
	f.sendProgressMessage(context.Background(), "Stale working…")
	f.clearProgressMessage("Request stopped.")
	if len(c.sent) != 1 || len(c.deletes) != 1 {
		t.Fatal("terminal cleanup was not idempotent")
	}
}

func TestSlackProgressDeletionFailureLeavesTerminalStatus(t *testing.T) {
	c := &progressTestConnector{deleteErr: fmt.Errorf("missing_scope")}
	f := NewBotEventFilter(c, ThreadID{Platform: "slack"}, "session", "", "user")
	f.sendProgressMessage(context.Background(), "Working…")
	f.completionReceived = true
	f.checkSessionDone("agent_end")
	if len(c.updates) != 1 || c.updates[0] != "msg:Request finished." {
		t.Fatal("failed deletion left a working status")
	}
}

func TestSlackProgressClearsWhenWaitingForUser(t *testing.T) {
	c := &progressTestConnector{}
	f := NewBotEventFilter(c, ThreadID{Platform: "slack"}, "session", "", "user")
	f.sendProgressMessage(context.Background(), "Working…")
	f.setBlocking("blocking_human_feedback", "request")
	f.sendProgressMessage(context.Background(), "Stale working…")
	if len(c.sent) != 1 || len(c.deletes) != 1 {
		t.Fatal("waiting turn retained working progress")
	}
}

func TestSlackProgressCompletionDuringSendDoesNotLeaveStaleStatus(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	c := &progressTestConnector{testBotConnector: testBotConnector{sendStarted: started, releaseSend: release}}
	f := NewBotEventFilter(c, ThreadID{Platform: "slack"}, "session", "", "user")
	sent := make(chan struct{})
	go func() { f.sendProgressMessage(context.Background(), "Working…"); close(sent) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("send did not start")
	}
	f.mu.Lock()
	f.sessionDone = true
	f.mu.Unlock()
	cleared := make(chan struct{})
	go func() { f.clearProgressMessage("Request finished."); close(cleared) }()
	close(release)
	select {
	case <-sent:
	case <-time.After(time.Second):
		t.Fatal("send did not finish")
	}
	select {
	case <-cleared:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not finish")
	}
	f.sendProgressMessage(context.Background(), "Stale working…")
	if len(c.sent) != 1 || len(c.deletes) != 1 {
		t.Fatal("in-flight heartbeat survived completion")
	}
}
