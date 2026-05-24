package virtualtools

import (
	"context"
	"strings"
	"testing"
	"time"

	"mcp-agent-builder-go/agent_go/cmd/server/services"
	"mcp-agent-builder-go/agent_go/pkg/common"
)

type testUserNotificationConnector struct {
	name string
	ch   chan *services.NotificationDestination
}

func (c *testUserNotificationConnector) Name() string {
	if c.name != "" {
		return c.name
	}
	return "test_notify_user"
}
func (c *testUserNotificationConnector) IsEnabled() bool {
	return true
}
func (c *testUserNotificationConnector) SendNotification(context.Context, string, string, string, *services.ButtonOptions, *services.NotificationDestination) (string, error) {
	return "", nil
}
func (c *testUserNotificationConnector) SendUserNotification(ctx context.Context, message, contextMsg string, dest *services.NotificationDestination) (string, error) {
	c.ch <- dest
	return "msg-1", nil
}

func resetHumanToolTestState() {
	parentChatMu.Lock()
	parentChatRegistry = map[string]*ParentChatContext{}
	parentChatMu.Unlock()

	chatInjectMu.Lock()
	chatInject = nil
	chatInjectMu.Unlock()

	store := GetHumanFeedbackStore()
	store.mu.Lock()
	store.requests = make(map[string]*HumanFeedbackRequest)
	store.waiters = make(map[string]chan string)
	store.mu.Unlock()
}

func TestHandleHumanFeedbackRoutesToParentChat(t *testing.T) {
	resetHumanToolTestState()
	t.Cleanup(resetHumanToolTestState)

	RegisterParentChat("workflow-session", &ParentChatContext{
		SessionID:    "builder-session",
		WorkflowPath: "Workflow/upwork",
		GroupName:    "daily-bid",
	})

	var injected string
	SetChatInjector(func(ctx context.Context, sessionID, userID, message string) error {
		if sessionID != "builder-session" {
			t.Fatalf("unexpected parent session: %s", sessionID)
		}
		injected = message
		if err := GetHumanFeedbackStore().SubmitResponse("req-1", "approve"); err != nil {
			t.Fatalf("submit response: %v", err)
		}
		return nil
	})

	ctx := context.WithValue(context.Background(), BGAgentSessionIDKey, "workflow-session")
	got, err := handleHumanFeedback(ctx, map[string]interface{}{
		"unique_id":        "req-1",
		"message_for_user": "Review the drafted cover letter.",
		"options":          []interface{}{"approve", "decline"},
	})
	if err != nil {
		t.Fatalf("handleHumanFeedback returned error: %v", err)
	}
	if got != "approve" {
		t.Fatalf("unexpected response: %q", got)
	}
	if !strings.Contains(injected, "[WORKFLOW_HUMAN_FEEDBACK]") {
		t.Fatalf("expected routed workflow feedback marker, got %q", injected)
	}
	if !strings.Contains(injected, "Review the drafted cover letter.") {
		t.Fatalf("expected question in injected message, got %q", injected)
	}
	if !strings.Contains(injected, "approve") || !strings.Contains(injected, "decline") {
		t.Fatalf("expected options in injected message, got %q", injected)
	}
}

func TestHandleNotifyUserUsesBotDestination(t *testing.T) {
	ch := make(chan *services.NotificationDestination, 1)
	connector := &testUserNotificationConnector{ch: ch}
	manager := services.GetNotificationManager()
	manager.RegisterConnector(connector)
	t.Cleanup(func() {
		manager.UnregisterConnector(connector.Name())
	})

	ctx := context.WithValue(context.Background(), common.UserIDKey, "user-1")
	ctx = context.WithValue(ctx, BotNotificationDestinationKey, &services.NotificationDestination{
		Slack: &services.SlackDest{ChannelID: "C123", ThreadTS: "171.1"},
	})

	if _, err := handleNotifyUser(ctx, map[string]interface{}{"message_for_user": "FYI: done"}); err != nil {
		t.Fatalf("handleNotifyUser returned error: %v", err)
	}

	select {
	case dest := <-ch:
		if dest == nil || dest.UserID != "user-1" {
			t.Fatalf("destination user = %#v, want user-1", dest)
		}
		if dest.Slack == nil || dest.Slack.ChannelID != "C123" || dest.Slack.ThreadTS != "171.1" {
			t.Fatalf("slack destination = %#v, want C123/171.1", dest.Slack)
		}
	case <-time.After(time.Second):
		t.Fatal("expected user notification")
	}
}

func TestHandleNotifyUserFansOutToRegisteredConnectors(t *testing.T) {
	manager := services.GetNotificationManager()
	whatsappCh := make(chan *services.NotificationDestination, 1)
	slackCh := make(chan *services.NotificationDestination, 1)
	whatsappConnector := &testUserNotificationConnector{name: "whatsapp", ch: whatsappCh}
	slackConnector := &testUserNotificationConnector{name: "slack", ch: slackCh}
	manager.RegisterConnector(whatsappConnector)
	manager.RegisterConnector(slackConnector)
	t.Cleanup(func() {
		manager.UnregisterConnector("whatsapp")
		manager.UnregisterConnector("slack")
	})

	ctx := context.WithValue(context.Background(), common.UserIDKey, "user-1")
	ctx = context.WithValue(ctx, BotNotificationDestinationKey, &services.NotificationDestination{
		UserID:   "user-1",
		WhatsApp: &services.WhatsAppDest{ChannelID: "919000000000@s.whatsapp.net"},
	})

	if _, err := handleNotifyUser(ctx, map[string]interface{}{"message_for_user": "FYI: done"}); err != nil {
		t.Fatalf("handleNotifyUser returned error: %v", err)
	}

	select {
	case dest := <-whatsappCh:
		if dest == nil || dest.WhatsApp == nil || dest.WhatsApp.ChannelID == "" {
			t.Fatalf("destination = %#v, want WhatsApp destination", dest)
		}
	case <-time.After(time.Second):
		t.Fatal("expected WhatsApp notification")
	}

	select {
	case <-slackCh:
	case <-time.After(time.Second):
		t.Fatal("expected Slack connector to be considered in fanout")
	}
}
