package server

import (
	"strings"
	"testing"
	"time"

	internalevents "mcp-agent-builder-go/agent_go/internal/events"
	todo_creation_human "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
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

func TestWorkflowSubAgentTrackingNotifierSignalsStart(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "session-sub-agent-start"
	const agentID = "todo-sub-step-route"

	api := &StreamingAPI{
		bgAgentRegistry:           NewBackgroundAgentRegistry(),
		eventStore:                store,
		sessionBusy:               map[string]bool{sessionID: true},
		pendingStartNotifications: make(map[string][]string),
	}

	notifier := &workflowSubAgentTrackingNotifier{
		api:       api,
		sessionID: sessionID,
	}
	notifier.OnSubAgentStart(todo_creation_human.WorkshopExecutionStart{
		ID:                agentID,
		Name:              "Route picker",
		Kind:              "workflow_sub_agent",
		ParentExecutionID: "exec-parent",
	})

	agent := api.bgAgentRegistry.Get(sessionID, agentID)
	if agent == nil {
		t.Fatal("expected agent to be registered")
	}
	if got := agent.GetStatus(); got != BGAgentRunning {
		t.Fatalf("expected running status, got %q", got)
	}

	events := store.GetAllEventsRaw(sessionID)
	if len(events) != 1 {
		t.Fatalf("expected one background start event, got %d", len(events))
	}
	if got := events[0].Type; got != "background_agent_started" {
		t.Fatalf("expected background_agent_started event, got %q", got)
	}

	pending := api.drainPendingStartNotifications(sessionID)
	if len(pending) != 1 || pending[0] != agentID {
		t.Fatalf("expected queued start notification for %q, got %#v", agentID, pending)
	}
}

func TestWorkflowStartAutoNotificationPayloadAndDrain(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "session-workflow-start"
	const agentID = "flow-0001"

	api := &StreamingAPI{
		bgAgentRegistry:           NewBackgroundAgentRegistry(),
		eventStore:                store,
		sessionBusy:               map[string]bool{sessionID: true},
		pendingStartNotifications: make(map[string][]string),
	}
	api.bgAgentRegistry.Register(sessionID, &BackgroundAgent{
		ID:        agentID,
		Name:      "Step: collect-evidence (RCA Workflow)",
		SessionID: sessionID,
		Kind:      "workflow_run_tool",
		Status:    BGAgentRunning,
		CreatedAt: time.Now(),
		Metadata: map[string]string{
			"type":          "workflow_run",
			"workflow_path": "Workflow/rtsrca",
			"group_name":    "production",
			"step_id":       "collect-evidence",
		},
	})

	part := backgroundAgentStartNotificationPart(api.bgAgentRegistry.Get(sessionID, agentID).GetSnapshot())
	msg := buildBackgroundAgentStartSyntheticMessage(sessionID, []string{part})
	for _, want := range []string{
		"[AUTO-NOTIFICATION]",
		"Workflow step 'Step: collect-evidence (RCA Workflow)'",
		"workflow=Workflow/rtsrca",
		"group=production",
		"step=collect-evidence",
		"completion will arrive as a separate AUTO-NOTIFICATION",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected start auto-notification to contain %q, got:\n%s", want, msg)
		}
	}

	api.notifyBackgroundAgentStarted(sessionID, agentID)
	if pending := api.drainPendingStartNotifications(sessionID); len(pending) != 1 || pending[0] != agentID {
		t.Fatalf("expected queued start notification for busy session, got %#v", pending)
	} else {
		api.queuePendingStartNotifications(sessionID, pending)
	}

	api.setSessionBusy(sessionID, false)
	api.processBatchedBackgroundAgentStarts(sessionID, api.drainPendingStartNotifications(sessionID))

	events := store.GetAllEventsRaw(sessionID)
	if len(events) != 1 {
		t.Fatalf("expected one synthetic_turn_ready event, got %d", len(events))
	}
	if got := events[0].Type; got != "synthetic_turn_ready" {
		t.Fatalf("expected synthetic_turn_ready, got %q", got)
	}
	agent := api.bgAgentRegistry.Get(sessionID, agentID)
	agent.mu.RLock()
	startNotified := agent.startNotified
	agent.mu.RUnlock()
	if !startNotified {
		t.Fatal("expected start notification to be marked notified after drain")
	}
}
