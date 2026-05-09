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
	sent []string
}

func (c *testBotConnector) Name() string { return "whatsapp" }
func (c *testBotConnector) IsEnabled() bool {
	return true
}
func (c *testBotConnector) SendNotification(context.Context, string, string, string, *ButtonOptions, *NotificationDestination) (string, error) {
	return "", nil
}
func (c *testBotConnector) SupportsThreads() bool { return false }
func (c *testBotConnector) StartListening(context.Context) error {
	return nil
}
func (c *testBotConnector) StopListening() {}
func (c *testBotConnector) SendThreadMessage(_ context.Context, _ ThreadID, message string) (string, error) {
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
