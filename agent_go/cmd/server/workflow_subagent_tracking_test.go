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

// Pin which execution IDs are treated as internal post-step phases. Adding a
// new phase that should skip the chat START notification means updating
// isInternalPostStepExecutionID and adding its prefix here.
func TestIsInternalPostStepExecutionID(t *testing.T) {
	for _, id := range []string{
		"learn-prepare-test-fixtures-12345",
		"kb-update-prepare-test-fixtures-67890",
	} {
		if !isInternalPostStepExecutionID(id) {
			t.Fatalf("expected %q to be classified as an internal post-step phase", id)
		}
	}
	for _, id := range []string{
		"exec-prepare-test-fixtures-1779452802932282000",
		"flow-0001",
		"todo-sub-step-route",
		"learning-not-prefixed", // missing "-" sentinel
		"kb-update", // bare keyword
		"",
	} {
		if isInternalPostStepExecutionID(id) {
			t.Fatalf("expected %q to be classified as a user-facing execution (not internal phase)", id)
		}
	}
}

// Pin the imperative "do not call tools" trailer on the start auto-notification.
// Some agents (notably cursor-cli) interpret a softer "continue tracking …" as
// permission to run a status-check tool, burning 30-60s of turn time before
// the completion notification arrives. The trailer must keep them from doing that.
func TestWorkflowStartAutoNotificationTrailerForbidsToolCalls(t *testing.T) {
	singleAgent := buildBackgroundAgentStartSyntheticMessage("session-x", []string{
		"- Workflow step 'do thing' (ID: id-1) [workflow=Workflow/x group=g step=s] started.",
	})
	multiAgent := buildBackgroundAgentStartSyntheticMessage("session-x", []string{
		"- Workflow step 'do thing one' (ID: id-1) started.",
		"- Workflow step 'do thing two' (ID: id-2) started.",
	})

	for _, tt := range []struct {
		name        string
		msg         string
		maxNewlines int
	}{
		// Single-part: header + trailer only. cursor-cli's tmux paste-compression
		// flips to "[Pasted text +N lines]" above ~2 newlines, so the common
		// case must stay strictly compact.
		{"single-part start notification", singleAgent, 1},
		// Multi-part (rare — several workflows starting at once) inherently
		// needs one line per agent. Paste-compression is acceptable there.
		{"multi-part start notification", multiAgent, 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, want := range []string{
				"[AUTO-NOTIFICATION]",
				"Ack briefly",
				"Do NOT call tools",
				"completion will arrive as a separate AUTO-NOTIFICATION",
			} {
				if !strings.Contains(tt.msg, want) {
					t.Fatalf("start trailer missing required directive %q, got:\n%s", want, tt.msg)
				}
			}
			if got := strings.Count(tt.msg, "\n"); got > tt.maxNewlines {
				t.Fatalf("start notification has %d newlines (max %d) — cursor will paste-compress it:\n%s", got, tt.maxNewlines, tt.msg)
			}
		})
	}
}
