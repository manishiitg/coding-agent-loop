package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
)

type testBotConnector struct {
	name            string
	supportsThreads bool
	sent            []string
	sendStarted     chan struct{}
	releaseSend     chan struct{}
}

func (c *testBotConnector) Name() string {
	if c.name != "" {
		return c.name
	}
	return "whatsapp"
}
func (c *testBotConnector) IsEnabled() bool {
	return true
}
func (c *testBotConnector) SendNotification(context.Context, string, string, string, *ButtonOptions, *NotificationDestination) (string, error) {
	return "", nil
}
func (c *testBotConnector) SupportsThreads() bool { return c.supportsThreads }
func (c *testBotConnector) StartListening(context.Context) error {
	return nil
}
func (c *testBotConnector) StopListening() {}
func (c *testBotConnector) SendThreadMessage(_ context.Context, _ ThreadID, message string) (string, error) {
	if c.sendStarted != nil {
		select {
		case <-c.sendStarted:
		default:
			close(c.sendStarted)
		}
	}
	if c.releaseSend != nil {
		<-c.releaseSend
	}
	c.sent = append(c.sent, message)
	return "msg", nil
}
func (c *testBotConnector) SendThreadMessageWithBlocks(_ context.Context, _ ThreadID, message string, _ []MessageBlock) (string, error) {
	return c.SendThreadMessage(context.Background(), ThreadID{}, message)
}
func (c *testBotConnector) UpdateMessage(context.Context, ThreadID, string, string) error {
	return nil
}
func (c *testBotConnector) AddReaction(context.Context, string, string, string) error {
	return nil
}
func (c *testBotConnector) RemoveReaction(context.Context, string, string, string) error {
	return nil
}
func (c *testBotConnector) GetThreadHistory(context.Context, ThreadID) ([]ThreadMessage, error) {
	return nil, nil
}
func (c *testBotConnector) GetChannelName(context.Context, string) string { return "" }
func (c *testBotConnector) SetMessageHandler(BotMessageHandler)           {}
func (c *testBotConnector) SetInteractionHandler(BotInteractionHandler)   {}
func (c *testBotConnector) GetFormatter() MessageFormatter {
	return &WhatsAppFormatter{}
}

func TestThreadlessRunningSessionInjectsWhenBuilderFree(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	followUps := make(chan string, 1)
	manager.SetFollowUpFunc(func(_ context.Context, req map[string]interface{}, _ string, _ string) error {
		followUps <- req["query"].(string)
		return nil
	})

	active := &activeBotSession{
		SessionID:    "session-1",
		UserID:       "user-1",
		Status:       chathistory.BotSessionStatusRunning,
		Platform:     "whatsapp",
		ThreadID:     ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"},
		LastActivity: time.Now(),
		builderDone:  true,
	}

	manager.handleExistingSession(active, BotIncomingMessage{
		Platform:  "whatsapp",
		ChannelID: "dm",
		Text:      "what's going on",
	}, false)

	select {
	case got := <-followUps:
		if got != "what's going on" {
			t.Fatalf("follow-up query = %q, want %q", got, "what's going on")
		}
	case <-time.After(time.Second):
		t.Fatal("expected follow-up injection")
	}
	if len(connector.sent) != 0 {
		t.Fatalf("unexpected connector reply: %#v", connector.sent)
	}
}

func TestWhatsAppResumeCommandBindsExistingSession(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	manager.SetResumeTargetFunc(func(_ context.Context, userID, selector string, filter BotResumeFilter) (*BotResumeTarget, error) {
		if userID != "user-1" {
			t.Fatalf("resume userID = %q, want user-1", userID)
		}
		if selector != "1" {
			t.Fatalf("resume selector = %q, want 1", selector)
		}
		if filter.WorkspacePath != "" || filter.PresetQueryID != "" {
			t.Fatalf("resume filter = %+v, want empty", filter)
		}
		return &BotResumeTarget{
			SessionID:     "chat-1",
			UserID:        "user-1",
			AgentMode:     "workflow_phase",
			Status:        "running",
			Query:         "Build the report workflow",
			WorkspacePath: "Workflow/report",
			PresetQueryID: "preset-report",
			PhaseID:       "workflow-builder",
			WorkshopMode:  "run",
			WorkflowName:  "report",
		}, nil
	})

	manager.HandleIncomingMessage(BotIncomingMessage{
		Platform:        "whatsapp",
		UserID:          "phone",
		WorkspaceUserID: "user-1",
		UserName:        "User",
		ChannelID:       "dm",
		Text:            "@resume 1",
		IsMention:       true,
	})

	if len(connector.sent) != 1 || !strings.Contains(connector.sent[0], "chat-1") || !strings.Contains(connector.sent[0], "report") {
		t.Fatalf("resume ack = %#v, want one ack naming the workflow and session chat-1", connector.sent)
	}
	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	manager.mu.RLock()
	active := manager.sessions[threadID.Key()]
	manager.mu.RUnlock()
	if active == nil {
		t.Fatal("expected resumed session to be bound to WhatsApp thread")
	}
	if active.SessionID != "chat-1" || active.AgentMode != "workflow_phase" || active.PresetQueryID != "preset-report" || active.WorkspacePath != "Workflow/report" {
		t.Fatalf("active metadata = %+v", active)
	}
	// Resuming a live (running) session auto-enables full detail so workflow
	// progress isn't suppressed by concise mode.
	active.mu.Lock()
	full := active.sendFullDetails
	active.mu.Unlock()
	if !full {
		t.Fatal("expected full detail to be auto-enabled when resuming a running session")
	}
}

func TestBareResumeShowsPickerWithoutBinding(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	manager.SetResumeTargetFunc(func(_ context.Context, _ string, _ string, _ BotResumeFilter) (*BotResumeTarget, error) {
		t.Fatal("bare @resume must not resolve/bind a target")
		return nil, nil
	})
	manager.SetResumeListFunc(func(_ context.Context, userID string, _ BotResumeFilter) ([]BotResumeTarget, error) {
		if userID != "user-1" {
			t.Fatalf("resume list userID = %q, want user-1", userID)
		}
		return []BotResumeTarget{
			{SessionID: "chat-1", WorkflowName: "report", Status: "running", Activity: "Step 2: gather data"},
			{SessionID: "chat-2", WorkflowName: "linkedin", Status: "running", HasBackgroundActivity: true, Activity: "background work running"},
		}, nil
	})

	manager.HandleIncomingMessage(BotIncomingMessage{
		Platform:        "whatsapp",
		UserID:          "phone",
		WorkspaceUserID: "user-1",
		UserName:        "User",
		ChannelID:       "dm",
		Text:            "@resume",
		IsMention:       true,
	})

	if len(connector.sent) != 1 {
		t.Fatalf("bare resume sent = %#v, want one picker message", connector.sent)
	}
	reply := connector.sent[0]
	for _, want := range []string{"Sessions you can connect to:", "1. ", "report", "running", "Step 2: gather data", "linkedin", "@resume <number>"} {
		if !strings.Contains(reply, want) {
			t.Fatalf("picker reply %q missing %q", reply, want)
		}
	}

	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	manager.mu.RLock()
	active := manager.sessions[threadID.Key()]
	manager.mu.RUnlock()
	if active != nil {
		t.Fatalf("bare resume must not bind a session, got %+v", active)
	}
}

func TestBuildQueryRequestForActivePreservesWorkflowMetadata(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	active := &activeBotSession{
		SessionID:     "chat-1",
		UserID:        "user-1",
		AgentMode:     "workflow_phase",
		PresetQueryID: "preset-report",
		WorkspacePath: "Workflow/report",
		PhaseID:       "workflow-builder",
		WorkshopMode:  "run",
	}

	req := manager.buildQueryRequestForActive(active, "continue", "user-1", "whatsapp", ThreadID{
		Platform:  "whatsapp",
		ChannelID: "dm",
		ThreadTS:  "dm",
	})

	if req["agent_mode"] != "workflow_phase" || req["preset_query_id"] != "preset-report" || req["selected_folder"] != "Workflow/report" || req["phase_id"] != "workflow-builder" {
		t.Fatalf("request did not preserve workflow metadata: %#v", req)
	}
	execOpts, ok := req["execution_options"].(map[string]interface{})
	if !ok || execOpts["workshop_mode"] != "run" {
		t.Fatalf("execution_options = %#v, want workshop_mode run", req["execution_options"])
	}
}

func TestStatusShowsNumberedResumableChats(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	manager.SetResumeListFunc(func(_ context.Context, userID string, filter BotResumeFilter) ([]BotResumeTarget, error) {
		if userID != "user-1" {
			t.Fatalf("resume list userID = %q, want user-1", userID)
		}
		if filter.WorkspacePath != "Workflow/report" || filter.PresetQueryID != "preset-report" {
			t.Fatalf("resume list filter = %+v, want report filter", filter)
		}
		return []BotResumeTarget{
			{SessionID: "chat-1", Query: "newer report chat", Status: "running"},
			{SessionID: "chat-2", Query: "older report chat", Status: "completed"},
		}, nil
	})

	reply := manager.formatBotStatusReply("user-1", "", false, "", false, BotResumeFilter{
		WorkspacePath: "Workflow/report",
		PresetQueryID: "preset-report",
	})

	if !strings.Contains(reply, "Resumable chats for this workflow:") || !strings.Contains(reply, "1. newer report chat - running") || !strings.Contains(reply, "Use `@resume 1`") {
		t.Fatalf("status reply = %q, want numbered resumable chat list", reply)
	}
}

func TestAddRestoredConversationSessionID(t *testing.T) {
	req := map[string]interface{}{"query": "continue"}
	addRestoredConversationSessionID(req, " old-session-1 ")

	if got := req["restored_conversation_session_id"]; got != "old-session-1" {
		t.Fatalf("restored_conversation_session_id = %#v, want old-session-1", got)
	}

	addRestoredConversationSessionID(req, " ")
	if got := req["restored_conversation_session_id"]; got != "old-session-1" {
		t.Fatalf("blank session id should not overwrite existing value, got %#v", got)
	}
}

func TestBlockingHumanFeedbackResponseSubmitsNotification(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	manager.SetFollowUpFunc(func(context.Context, map[string]interface{}, string, string) error {
		t.Fatal("human feedback response should submit to feedback store, not start a follow-up")
		return nil
	})

	submitted := make(chan struct {
		id       string
		response string
	}, 1)
	GetNotificationManager().SetFeedbackResponseFunc(func(uniqueID, response string) error {
		submitted <- struct {
			id       string
			response string
		}{id: uniqueID, response: response}
		return nil
	})
	t.Cleanup(func() {
		GetNotificationManager().SetFeedbackResponseFunc(nil)
	})

	active := &activeBotSession{
		SessionID:         "session-1",
		UserID:            "user-1",
		Status:            chathistory.BotSessionStatusRunning,
		Platform:          "whatsapp",
		ThreadID:          ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"},
		awaitingUserInput: true,
		blockingEventType: "blocking_human_feedback",
		blockingRequestID: "req-1",
	}

	manager.handleBlockingResponse(active, BotIncomingMessage{
		Platform: "whatsapp",
		Text:     "approve",
	})

	select {
	case got := <-submitted:
		if got.id != "req-1" || got.response != "approve" {
			t.Fatalf("submitted = %#v, want req-1/approve", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected feedback response submission")
	}

	active.mu.Lock()
	awaiting := active.awaitingUserInput
	requestID := active.blockingRequestID
	active.mu.Unlock()
	if awaiting || requestID != "" {
		t.Fatalf("blocking state not cleared: awaiting=%v requestID=%q", awaiting, requestID)
	}
}

func TestThreadedCompletedSessionStartsFreshWithRestoreSessionID(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{name: "slack", supportsThreads: true}
	manager.RegisterConnector(connector)

	type startedSession struct {
		req       map[string]interface{}
		sessionID string
	}
	started := make(chan startedSession, 1)
	manager.SetStartSessionFunc(func(_ context.Context, req map[string]interface{}, sessionID string, _ string, _ func(event *events.AgentEvent)) error {
		started <- startedSession{req: req, sessionID: sessionID}
		return nil
	})

	threadID := ThreadID{Platform: "slack", ChannelID: "C123", ThreadTS: "1710000000.000100"}
	active := &activeBotSession{
		SessionID:    "old-session-1",
		UserID:       "user-1",
		Status:       chathistory.BotSessionStatusCompleted,
		Platform:     "slack",
		ThreadID:     threadID,
		LastActivity: time.Now(),
		builderDone:  true,
	}

	manager.handleExistingSession(active, BotIncomingMessage{
		Platform:  "slack",
		ChannelID: "C123",
		ThreadTS:  "1710000000.000100",
		UserID:    "U123",
		Text:      "continue this",
		IsMention: true,
	}, true)

	select {
	case got := <-started:
		if got.sessionID == "old-session-1" {
			t.Fatalf("threaded completed session reused old session ID; want fresh session with restore pointer")
		}
		if got.req["restored_conversation_session_id"] != "old-session-1" {
			t.Fatalf("restored_conversation_session_id = %#v, want old-session-1", got.req["restored_conversation_session_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("expected new threaded session to start")
	}
}

func TestThreadlessCompletedSameRouteReusesSessionID(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	started := make(chan string, 1)
	manager.SetStartSessionFunc(func(_ context.Context, _ map[string]interface{}, sessionID string, _ string, _ func(event *events.AgentEvent)) error {
		started <- sessionID
		return nil
	})

	route := &ChannelRoute{WorkflowID: "wf-old", WorkspacePath: "Workflow/old", WorkshopMode: "run"}
	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	active := &activeBotSession{
		SessionID:    "old-session-1",
		UserID:       "user-1",
		Status:       chathistory.BotSessionStatusCompleted,
		Platform:     "whatsapp",
		ThreadID:     threadID,
		RouteKey:     botRouteKey(route),
		LastActivity: time.Now(),
		builderDone:  true,
	}

	manager.handleExistingSession(active, BotIncomingMessage{
		Platform:       "whatsapp",
		ChannelID:      "dm",
		Text:           "continue this",
		PresetWorkflow: route,
	}, false)

	select {
	case got := <-started:
		if got != "old-session-1" {
			t.Fatalf("session ID = %q, want old-session-1", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected threadless same-route session to start")
	}
}

func TestThreadlessCompletedRouteChangeStartsFreshWithoutRestoreSessionID(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	type startedSession struct {
		req       map[string]interface{}
		sessionID string
	}
	started := make(chan startedSession, 1)
	manager.SetStartSessionFunc(func(_ context.Context, req map[string]interface{}, sessionID string, _ string, _ func(event *events.AgentEvent)) error {
		started <- startedSession{req: req, sessionID: sessionID}
		return nil
	})

	oldRoute := &ChannelRoute{WorkflowID: "wf-old", WorkspacePath: "Workflow/old", WorkshopMode: "run"}
	newRoute := &ChannelRoute{WorkflowID: "wf-new", WorkspacePath: "Workflow/new", WorkshopMode: "builder"}
	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	active := &activeBotSession{
		SessionID:    "old-session-1",
		UserID:       "user-1",
		Status:       chathistory.BotSessionStatusCompleted,
		Platform:     "whatsapp",
		ThreadID:     threadID,
		RouteKey:     botRouteKey(oldRoute),
		LastActivity: time.Now(),
		builderDone:  true,
	}

	manager.handleExistingSession(active, BotIncomingMessage{
		Platform:       "whatsapp",
		ChannelID:      "dm",
		Text:           "start switched workflow",
		PresetWorkflow: newRoute,
	}, false)

	select {
	case got := <-started:
		if got.sessionID == "old-session-1" {
			t.Fatalf("route change reused old session ID; want fresh session")
		}
		if _, ok := got.req["restored_conversation_session_id"]; ok {
			t.Fatalf("route change should not restore previous route, got req %#v", got.req)
		}
		if got.req["preset_query_id"] != "wf-new" {
			t.Fatalf("preset_query_id = %#v, want wf-new", got.req["preset_query_id"])
		}
	case <-time.After(time.Second):
		t.Fatal("expected threadless route-change session to start")
	}
}

func TestThreadlessPlainStopIsForwardedNotControl(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	followUps := make(chan string, 1)
	manager.SetFollowUpFunc(func(_ context.Context, req map[string]interface{}, _ string, _ string) error {
		followUps <- req["query"].(string)
		return nil
	})

	active := &activeBotSession{
		SessionID:    "session-1",
		UserID:       "user-1",
		Status:       chathistory.BotSessionStatusRunning,
		Platform:     "whatsapp",
		ThreadID:     ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"},
		LastActivity: time.Now(),
		builderDone:  true,
	}

	manager.handleExistingSession(active, BotIncomingMessage{
		Platform:  "whatsapp",
		ChannelID: "dm",
		Text:      "stop",
	}, false)

	select {
	case got := <-followUps:
		if got != "stop" {
			t.Fatalf("follow-up query = %q, want stop", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected plain stop to be forwarded")
	}
}

func TestThreadlessPrefixedDoneEndsSession(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	active := &activeBotSession{
		SessionID:    "session-1",
		UserID:       "user-1",
		Status:       chathistory.BotSessionStatusRunning,
		Platform:     "whatsapp",
		ThreadID:     threadID,
		LastActivity: time.Now(),
		builderDone:  true,
	}
	manager.sessions[threadID.Key()] = active

	manager.handleExistingSession(active, BotIncomingMessage{
		Platform:  "whatsapp",
		ChannelID: "dm",
		Text:      "@done",
	}, false)

	manager.mu.RLock()
	_, exists := manager.sessions[threadID.Key()]
	manager.mu.RUnlock()
	if exists {
		t.Fatal("expected @done to remove active threadless session")
	}
}

func TestThreadlessRunningSessionQueuesWhileBuilderBusy(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	followUps := make(chan string, 1)
	manager.SetFollowUpFunc(func(_ context.Context, req map[string]interface{}, _ string, _ string) error {
		followUps <- req["query"].(string)
		return nil
	})

	active := &activeBotSession{
		SessionID:    "session-1",
		UserID:       "user-1",
		Status:       chathistory.BotSessionStatusRunning,
		Platform:     "whatsapp",
		ThreadID:     ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"},
		LastActivity: time.Now(),
		builderDone:  false,
	}

	manager.handleExistingSession(active, BotIncomingMessage{
		Platform:  "whatsapp",
		ChannelID: "dm",
		Text:      "next question",
	}, false)

	select {
	case got := <-followUps:
		t.Fatalf("unexpected immediate follow-up injection: %q", got)
	default:
	}
	if len(connector.sent) != 1 {
		t.Fatalf("connector sent %d replies, want 1", len(connector.sent))
	}
	if len(active.queuedMessages) != 1 {
		t.Fatalf("queued messages = %d, want 1", len(active.queuedMessages))
	}

	if !manager.startNextQueuedMessage(active) {
		t.Fatal("expected queued message to start")
	}
	select {
	case got := <-followUps:
		if got != "next question" {
			t.Fatalf("follow-up query = %q, want %q", got, "next question")
		}
	case <-time.After(time.Second):
		t.Fatal("expected queued follow-up injection")
	}
}

func TestSyntheticFinalSendsWhenEarlierBuilderTextDiffers(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	filter := NewBotEventFilter(connector, threadID, "session-1", "", "user-1")
	filter.MarkMainTextSent("The RCA investigation is complete. Here's a summary of what was found.")

	manager.sessions[threadID.Key()] = &activeBotSession{
		SessionID:    "session-1",
		UserID:       "user-1",
		Status:       chathistory.BotSessionStatusRunning,
		Platform:     "whatsapp",
		ThreadID:     threadID,
		LastActivity: time.Now(),
		eventFilter:  filter,
	}

	const final = "Run completed successfully. Here's the plain-English summary."
	if !manager.SendSyntheticTurnFinalIfNeeded("session-1", final) {
		t.Fatal("expected different synthetic final to be sent")
	}
	if len(connector.sent) != 1 || connector.sent[0] != final {
		t.Fatalf("sent messages = %#v, want final text", connector.sent)
	}
}

func TestSyntheticFinalSuppressesDuplicateBuilderText(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	filter := NewBotEventFilter(connector, threadID, "session-1", "", "user-1")
	filter.MarkMainTextSent("Run completed successfully. Here's the plain-English summary.")

	manager.sessions[threadID.Key()] = &activeBotSession{
		SessionID:    "session-1",
		UserID:       "user-1",
		Status:       chathistory.BotSessionStatusRunning,
		Platform:     "whatsapp",
		ThreadID:     threadID,
		LastActivity: time.Now(),
		eventFilter:  filter,
	}

	if manager.SendSyntheticTurnFinalIfNeeded("session-1", " Run completed successfully. Here's the plain-English summary.\n") {
		t.Fatal("expected duplicate synthetic final to be skipped")
	}
	if len(connector.sent) != 0 {
		t.Fatalf("unexpected sent messages: %#v", connector.sent)
	}
}

func TestExistingSessionDetailModeCommandsToggleFilter(t *testing.T) {
	manager := NewBotConversationManager(nil, "", "")
	connector := &testBotConnector{}
	manager.RegisterConnector(connector)

	threadID := ThreadID{Platform: "whatsapp", ChannelID: "dm", ThreadTS: "dm"}
	filter := NewBotEventFilter(connector, threadID, "session-1", "", "user-1")
	active := &activeBotSession{
		SessionID:    "session-1",
		UserID:       "user-1",
		Status:       chathistory.BotSessionStatusRunning,
		Platform:     "whatsapp",
		ThreadID:     threadID,
		LastActivity: time.Now(),
		eventFilter:  filter,
	}

	stepEvent := BotEventData{
		Type: "llm_generation_end",
		Data: &events.AgentEvent{
			Data: &events.LLMGenerationEndEvent{
				BaseEventData: events.BaseEventData{
					Metadata: map[string]interface{}{
						"current_step_id": "step-1",
					},
				},
				Content: "internal detail",
			},
		},
	}
	if !filter.suppressWorkflowRuntimeChatter(stepEvent) {
		t.Fatal("expected concise default to suppress workflow chatter")
	}

	manager.handleExistingSession(active, BotIncomingMessage{Platform: "whatsapp", ChannelID: "dm", Text: "@full"}, false)
	if !active.sendFullDetails {
		t.Fatal("expected active session to enable full details")
	}
	if filter.suppressWorkflowRuntimeChatter(stepEvent) {
		t.Fatal("expected full mode to allow workflow chatter")
	}

	manager.handleExistingSession(active, BotIncomingMessage{Platform: "whatsapp", ChannelID: "dm", Text: "@concise"}, false)
	if active.sendFullDetails {
		t.Fatal("expected active session to disable full details")
	}
	if !filter.suppressWorkflowRuntimeChatter(stepEvent) {
		t.Fatal("expected concise mode to suppress workflow chatter again")
	}
	if len(connector.sent) != 2 {
		t.Fatalf("sent replies = %d, want 2", len(connector.sent))
	}
	if !strings.Contains(connector.sent[0], "Full mode on") || !strings.Contains(connector.sent[1], "Concise mode on") {
		t.Fatalf("unexpected replies: %#v", connector.sent)
	}
}
