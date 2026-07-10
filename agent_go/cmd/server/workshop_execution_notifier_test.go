package server

import (
	"context"
	"testing"
	"time"
)

func TestWorkshopExecutionNotifierReportsUnexpectedContextCancelAsFailure(t *testing.T) {
	registry := NewBackgroundAgentRegistry()
	api := &StreamingAPI{
		bgAgentRegistry: registry,
	}
	const (
		sessionID = "pulse-harden-session"
		execID    = "harden-20000"
	)
	agent := &BackgroundAgent{
		ID:        execID,
		Name:      "Harden Workflow",
		SessionID: sessionID,
		Status:    BGAgentRunning,
		CreatedAt: time.Now().Add(-10 * time.Minute),
	}
	registry.Register(sessionID, agent)
	completionCh := registry.GetNotificationChannel(sessionID)

	notifier := &workshopExecutionBgNotifier{api: api, sessionID: sessionID}
	notifier.OnExecutionComplete(execID, agent.Name, "", nil, context.Canceled)

	snap := agent.GetSnapshot()
	if snap.Status != BGAgentFailed {
		t.Fatalf("status = %q, want failed", snap.Status)
	}
	if snap.Error != context.Canceled.Error() {
		t.Fatalf("error = %q, want %q", snap.Error, context.Canceled.Error())
	}
	select {
	case got := <-completionCh:
		if got != execID {
			t.Fatalf("completion id = %q, want %q", got, execID)
		}
	default:
		t.Fatal("expected failed background execution to queue a parent auto-notification")
	}
}

func TestWorkshopExecutionNotifierPreservesExplicitCancellation(t *testing.T) {
	registry := NewBackgroundAgentRegistry()
	api := &StreamingAPI{
		bgAgentRegistry: registry,
	}
	const (
		sessionID = "explicit-stop-session"
		execID    = "harden-stopped"
	)
	agent := &BackgroundAgent{
		ID:        execID,
		Name:      "Harden Workflow",
		SessionID: sessionID,
		Status:    BGAgentCanceled,
		CreatedAt: time.Now(),
	}
	registry.Register(sessionID, agent)
	completionCh := registry.GetNotificationChannel(sessionID)

	notifier := &workshopExecutionBgNotifier{api: api, sessionID: sessionID}
	notifier.OnExecutionComplete(execID, agent.Name, "", nil, context.Canceled)

	if got := agent.GetStatus(); got != BGAgentCanceled {
		t.Fatalf("status = %q, want canceled", got)
	}
	select {
	case got := <-completionCh:
		t.Fatalf("unexpected completion notification for explicitly canceled execution: %q", got)
	default:
	}
}
