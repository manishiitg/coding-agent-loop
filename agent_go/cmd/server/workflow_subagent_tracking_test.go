package server

import (
	"context"
	"strings"
	"testing"
	"time"

	internalevents "github.com/manishiitg/coding-agent-loop/agent_go/internal/events"
	todo_creation_human "github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
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

func TestWorkflowSubAgentTrackingNotifierTracksParentReconciledChildWithoutSyntheticNotification(t *testing.T) {
	eventStore := internalevents.NewEventStore(10)
	defer eventStore.Stop()

	const sessionID = "session-parent-reconciles"
	const agentID = "child-review-1"
	api := &StreamingAPI{
		bgAgentRegistry: NewBackgroundAgentRegistry(),
		eventStore:      eventStore,
	}
	notifier := &workflowSubAgentTrackingNotifier{api: api, sessionID: sessionID}
	notifier.OnSubAgentStart(todo_creation_human.WorkshopExecutionStart{
		ID: agentID, ParentExecutionID: "parent-fixer", Name: "Read-only review", Kind: "workflow_sub_agent",
		Metadata: map[string]string{"suppress_auto_notification": "true", "async_parent_reconciles": "true"},
	})
	notifier.OnSubAgentComplete(agentID, "Read-only review", "review complete", nil)

	tracked := api.bgAgentRegistry.Get(sessionID, agentID)
	if tracked == nil {
		t.Fatal("expected parent-owned child to remain in the execution registry")
	}
	snapshot := tracked.GetSnapshot()
	if snapshot.Status != BGAgentCompleted || snapshot.ParentExecutionID != "parent-fixer" || snapshot.Result != "review complete" {
		t.Fatalf("unexpected tracked child: %#v", snapshot)
	}
	select {
	case got := <-api.bgAgentRegistry.GetNotificationChannel(sessionID):
		t.Fatalf("parent-reconciled child leaked synthetic root notification for %q", got)
	case <-time.After(30 * time.Millisecond):
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

	if pending := api.drainPendingStartNotifications(sessionID); len(pending) != 0 {
		t.Fatalf("interactive start should be UI-only, got queued notification %#v", pending)
	}
	agent.mu.RLock()
	startNotified := agent.startNotified
	agent.mu.RUnlock()
	if !startNotified {
		t.Fatal("interactive start should be marked handled")
	}
}

func TestWorkflowStartNotificationPayloadAndInteractiveDelivery(t *testing.T) {
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
		Name:      "Step: collect-evidence (RCA Project)",
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
		"Started: Step: Step: collect-evidence (RCA Project)",
		"space=rtsrca",
		"group=production",
		"step=collect-evidence",
		"Ack only. No tools; wait.",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected start auto-notification to contain %q, got:\n%s", want, msg)
		}
	}

	api.notifyBackgroundAgentStarted(sessionID, agentID)
	if pending := api.drainPendingStartNotifications(sessionID); len(pending) != 0 {
		t.Fatalf("interactive start should not queue a synthetic turn, got %#v", pending)
	}
	agent := api.bgAgentRegistry.Get(sessionID, agentID)
	agent.mu.RLock()
	startNotified := agent.startNotified
	agent.mu.RUnlock()
	if !startNotified {
		t.Fatal("interactive start should be marked handled without a synthetic turn")
	}
	if events := store.GetAllEventsRaw(sessionID); len(events) != 0 {
		t.Fatalf("notifyBackgroundAgentStarted should not emit a synthetic event, got %d", len(events))
	}
}

func TestBotBackgroundStartStillQueuesOutboundAcknowledgement(t *testing.T) {
	const sessionID = "bot-slack-session"
	const agentID = "bot-background-agent"
	api := &StreamingAPI{
		bgAgentRegistry:           NewBackgroundAgentRegistry(),
		sessionBusy:               map[string]bool{sessionID: true},
		pendingStartNotifications: make(map[string][]string),
		stoppedSessions:           make(map[string]bool),
	}
	api.bgAgentRegistry.Register(sessionID, &BackgroundAgent{
		ID:        agentID,
		Name:      "Prepare report",
		SessionID: sessionID,
		Kind:      "delegation",
		Status:    BGAgentRunning,
		CreatedAt: time.Now(),
	})

	api.notifyBackgroundAgentStarted(sessionID, agentID)

	if pending := api.drainPendingStartNotifications(sessionID); len(pending) != 1 || pending[0] != agentID {
		t.Fatalf("bot start should remain queued for outbound acknowledgement, got %#v", pending)
	}
	api.markSessionStopped(sessionID)
}

func TestScheduledRunStartIsUIOnly(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "schedule-cron--abc123_100"
	const agentID = "workflow-step-1"
	api := &StreamingAPI{
		bgAgentRegistry:           NewBackgroundAgentRegistry(),
		eventStore:                store,
		sessionBusy:               map[string]bool{sessionID: true},
		pendingStartNotifications: make(map[string][]string),
	}
	api.bgAgentRegistry.Register(sessionID, &BackgroundAgent{
		ID:        agentID,
		Name:      "Step -> collect",
		SessionID: sessionID,
		Kind:      "workflow_step",
		Status:    BGAgentRunning,
		CreatedAt: time.Now(),
	})

	api.notifyBackgroundAgentStarted(sessionID, agentID)

	if pending := api.drainPendingStartNotifications(sessionID); len(pending) != 0 {
		t.Fatalf("scheduled start notification should not be queued, got %#v", pending)
	}
	if events := store.GetAllEventsRaw(sessionID); len(events) != 0 {
		t.Fatalf("scheduled start should not emit a synthetic turn event, got %d", len(events))
	}
	agent := api.bgAgentRegistry.Get(sessionID, agentID)
	agent.mu.RLock()
	startNotified := agent.startNotified
	agent.mu.RUnlock()
	if !startNotified {
		t.Fatal("scheduled start should be marked handled so it cannot be requeued")
	}
}

func TestScheduledAutoNotificationBoundsCompletionResult(t *testing.T) {
	snap := BackgroundAgentSnapshot{
		ID:     "workflow-step-long",
		Name:   "Step -> long result",
		Status: BGAgentCompleted,
		Result: strings.Repeat("x", scheduledAutoNotificationResultMaxRunes+500),
		Metadata: map[string]string{
			"step_id": "long-result",
		},
	}

	scheduled := (&StreamingAPI{}).buildAutoNotificationMessage("schedule-cron--abc123_100", snap)
	for _, want := range []string{
		"Detailed result omitted from this scheduled notification",
		`query_step(step_id="long-result", execution_id="workflow-step-long")`,
	} {
		if !strings.Contains(scheduled, want) {
			t.Fatalf("scheduled completion missing %q:\n%s", want, scheduled)
		}
	}
	if strings.Contains(scheduled, strings.Repeat("x", 100)) {
		t.Fatal("scheduled completion leaked the oversized raw result into the parent CLI")
	}
	interactive := (&StreamingAPI{}).buildAutoNotificationMessage("interactive-session", snap)
	if !strings.Contains(interactive, strings.Repeat("x", scheduledAutoNotificationResultMaxRunes+500)) {
		t.Fatal("interactive completion result should remain unchanged")
	}
}

func TestWorkflowStartAutoNotificationClearsStaleBusyWithoutActiveTurn(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "session-stale-busy-start"
	const agentID = "flow-stale-busy"

	api := &StreamingAPI{
		bgAgentRegistry:           NewBackgroundAgentRegistry(),
		eventStore:                store,
		sessionBusy:               map[string]bool{sessionID: true},
		sessionBusySince:          map[string]time.Time{sessionID: time.Now().Add(-autoNotificationStaleBusyAfter - time.Second)},
		pendingStartNotifications: make(map[string][]string),
	}
	api.bgAgentRegistry.Register(sessionID, &BackgroundAgent{
		ID:        agentID,
		Name:      "Step -> stale-busy-start",
		SessionID: sessionID,
		Kind:      "workflow_run_tool",
		Status:    BGAgentRunning,
		CreatedAt: time.Now(),
	})

	api.processBatchedBackgroundAgentStarts(sessionID, []string{agentID})

	if api.isSessionBusy(sessionID) {
		t.Fatal("expected stale busy flag to be cleared")
	}
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
	if startNotified {
		t.Fatal("expected stale-busy start notification to remain unmarked when no synthetic turn dispatches")
	}
	if pending := api.drainPendingStartNotifications(sessionID); len(pending) != 1 || pending[0] != agentID {
		t.Fatalf("expected stale-busy start notification to be requeued after dispatch failure, got %#v", pending)
	}
}

func TestWorkflowStartAutoNotificationSkipsCompletedAgent(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "session-completed-before-start-drain"
	const agentID = "flow-completed-before-start"

	api := &StreamingAPI{
		bgAgentRegistry:           NewBackgroundAgentRegistry(),
		eventStore:                store,
		pendingStartNotifications: make(map[string][]string),
	}
	api.bgAgentRegistry.Register(sessionID, &BackgroundAgent{
		ID:        agentID,
		Name:      "Step -> completed-before-start",
		SessionID: sessionID,
		Kind:      "workflow_run_tool",
		Status:    BGAgentCompleted,
		Result:    "done",
		CreatedAt: time.Now(),
	})
	api.queuePendingStartNotification(sessionID, agentID)

	pending := api.filterUnsentStartNotifications(sessionID, api.drainPendingStartNotifications(sessionID))
	if len(pending) != 0 {
		t.Fatalf("expected terminal agent start notification to be suppressed, got %#v", pending)
	}

	api.processBatchedBackgroundAgentStarts(sessionID, []string{agentID})
	events := store.GetAllEventsRaw(sessionID)
	if len(events) != 0 {
		t.Fatalf("expected no start synthetic event for completed agent, got %d", len(events))
	}
}

func TestWorkflowStartAutoNotificationDoesNotClearBusyWithActiveTurn(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "session-active-busy-start"
	const agentID = "flow-active-busy"

	api := &StreamingAPI{
		bgAgentRegistry:           NewBackgroundAgentRegistry(),
		eventStore:                store,
		sessionBusy:               map[string]bool{sessionID: true},
		sessionBusySince:          map[string]time.Time{sessionID: time.Now().Add(-autoNotificationStaleBusyAfter - time.Second)},
		pendingStartNotifications: make(map[string][]string),
		agentCancelFuncs: map[string]context.CancelFunc{
			sessionID: context.CancelFunc(func() {}),
		},
	}
	api.bgAgentRegistry.Register(sessionID, &BackgroundAgent{
		ID:        agentID,
		Name:      "Step -> active-busy-start",
		SessionID: sessionID,
		Kind:      "workflow_run_tool",
		Status:    BGAgentRunning,
		CreatedAt: time.Now(),
	})

	api.processBatchedBackgroundAgentStarts(sessionID, []string{agentID})

	if !api.isSessionBusy(sessionID) {
		t.Fatal("expected active busy flag to remain set")
	}
	if pending := api.drainPendingStartNotifications(sessionID); len(pending) != 1 || pending[0] != agentID {
		t.Fatalf("expected queued start notification for active turn, got %#v", pending)
	}
	if events := store.GetAllEventsRaw(sessionID); len(events) != 0 {
		t.Fatalf("expected no synthetic_turn_ready while active turn exists, got %d event(s)", len(events))
	}
}

func TestWorkflowStepStartAndCompletionNotifyMainAgent(t *testing.T) {
	store := internalevents.NewEventStore(10)
	defer store.Stop()

	const sessionID = "session-workflow-step-notify"
	const execID = "workflow-full-123-step-1"

	api := &StreamingAPI{
		bgAgentRegistry:           NewBackgroundAgentRegistry(),
		eventStore:                store,
		sessionBusy:               map[string]bool{sessionID: true},
		pendingStartNotifications: make(map[string][]string),
		completionLoopStarted:     make(map[string]bool),
	}
	ch := api.bgAgentRegistry.GetNotificationChannel(sessionID)

	notifier := &workshopExecutionBgNotifier{
		api:       api,
		sessionID: sessionID,
	}
	notifier.OnExecutionStart(todo_creation_human.WorkshopExecutionStart{
		ID:   execID,
		Name: "Step -> cdp-test",
	})

	if pending := api.drainPendingStartNotifications(sessionID); len(pending) != 0 {
		t.Fatalf("workflow-step start should be UI-only, got queued notification %#v", pending)
	}
	started := api.bgAgentRegistry.Get(sessionID, execID)
	started.mu.RLock()
	startNotified := started.startNotified
	started.mu.RUnlock()
	if !startNotified {
		t.Fatal("workflow-step start should be marked handled")
	}

	notifier.OnExecutionComplete(execID, "Step -> cdp-test", "step completed", map[string]string{
		"execution_type": "workflow-step",
	}, nil)

	deadline := time.After(time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case got := <-ch:
			if got != execID {
				t.Fatalf("expected workflow-step completion notification for %q, got %q", execID, got)
			}
			return
		case <-ticker.C:
			if pending := api.drainPendingCompletions(sessionID); len(pending) > 0 {
				if len(pending) != 1 || pending[0] != execID {
					t.Fatalf("expected pending workflow-step completion for %q, got %#v", execID, pending)
				}
				return
			}
		case <-deadline:
			t.Fatal("expected workflow-step completion notification")
		}
	}
}

// Pin the compact start auto-notification trailer.
func TestWorkflowStartAutoNotificationTrailerIsCompact(t *testing.T) {
	singleAgent := buildBackgroundAgentStartSyntheticMessage("session-x", []string{
		"- Step: do thing [space=x, group=g, step=s]",
	})
	multiAgent := buildBackgroundAgentStartSyntheticMessage("session-x", []string{
		"- Step: do thing one",
		"- Step: do thing two",
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
				"Ack only. No tools; wait.",
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

func TestBotAutoNotificationProgressDirectiveIsChannelSpecific(t *testing.T) {
	slack := botAutoNotificationProgressDirective("bot-slack--abc123", false)
	if !strings.Contains(slack, "Slack progress update") || !strings.Contains(slack, "mrkdwn") {
		t.Fatalf("expected Slack-specific directive, got:\n%s", slack)
	}

	whatsapp := botAutoNotificationProgressDirective("bot-whatsapp--abc123", false)
	if !strings.Contains(whatsapp, "WhatsApp progress update") || !strings.Contains(whatsapp, "plain-text") {
		t.Fatalf("expected WhatsApp-specific directive, got:\n%s", whatsapp)
	}

	generic := botAutoNotificationProgressDirective("bot-discord--abc123", false)
	if !strings.Contains(generic, "Bot progress update") {
		t.Fatalf("expected generic bot directive, got:\n%s", generic)
	}

	if got := botAutoNotificationProgressDirective("bot-whatsapp--abc123", true); got != "" {
		t.Fatalf("final bot notification should not include progress directive, got %q", got)
	}
}
