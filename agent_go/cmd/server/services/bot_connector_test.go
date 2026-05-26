package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/mcpagent/events"
	"mcp-agent-builder-go/agent_go/pkg/chathistory"
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
