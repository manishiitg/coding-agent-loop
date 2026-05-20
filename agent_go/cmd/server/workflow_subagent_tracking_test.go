package server

import (
	"testing"
	"time"

	internalevents "mcp-agent-builder-go/agent_go/internal/events"
)

func TestWorkflowSubAgentTrackingNotifierSignalsCompletion(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "session-sub-agent-completion"
	const agentID = "todo-sub-step-route"

	api := &StreamingAPI{
		bgAgentRegistry: NewBackgroundAgentRegistry(),
		eventStore:      store,
	}
	ch := api.bgAgentRegistry.GetNotificationChannel(sessionID)
	api.bgAgentRegistry.Register(sessionID, &BackgroundAgent{
		ID:                agentID,
		ParentExecutionID: "exec-parent",
		Name:              "Parent -> Route",
		SessionID:         sessionID,
		Kind:              "workflow_sub_agent",
		Status:            BGAgentRunning,
		CreatedAt:         time.Now(),
	})

	notifier := &workflowSubAgentTrackingNotifier{
		api:       api,
		sessionID: sessionID,
	}
	notifier.OnSubAgentComplete(agentID, "Parent -> Route", "sub-agent result", nil)

	select {
	case got := <-ch:
		if got != agentID {
			t.Fatalf("expected completion notification for %q, got %q", agentID, got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected completion notification")
	}

	agent := api.bgAgentRegistry.Get(sessionID, agentID)
	if agent == nil {
		t.Fatal("expected agent to remain registered")
	}
	if got := agent.GetStatus(); got != BGAgentCompleted {
		t.Fatalf("expected completed status, got %q", got)
	}

	events := store.GetAllEventsRaw(sessionID)
	if len(events) != 1 {
		t.Fatalf("expected one background completion event, got %d", len(events))
	}
	if got := events[0].Type; got != "background_agent_completed" {
		t.Fatalf("expected background_agent_completed event, got %q", got)
	}
}
