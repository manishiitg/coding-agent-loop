package virtualtools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/services"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
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

func TestHandleHumanFeedbackWaitsForDirectHumanResponseWithoutParentRelay(t *testing.T) {
	resetHumanToolTestState()
	t.Cleanup(resetHumanToolTestState)

	RegisterParentChat("workflow-session", &ParentChatContext{
		SessionID:    "builder-session",
		WorkflowPath: "Workflow/upwork",
		GroupName:    "daily-bid",
	})

	injected := make(chan string, 1)
	SetChatInjector(func(ctx context.Context, sessionID, userID, message string) error {
		injected <- message
		return nil
	})

	ctx := context.WithValue(context.Background(), BGAgentSessionIDKey, "workflow-session")
	type result struct {
		answer string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		answer, err := handleHumanFeedback(ctx, map[string]interface{}{
			"unique_id":        "req-1",
			"message_for_user": "Review the drafted cover letter.",
			"options":          []interface{}{"approve", "decline"},
			"timeout_seconds":  float64(30),
		})
		done <- result{answer: answer, err: err}
	}()

	deadline := time.Now().Add(time.Second)
	for {
		GetHumanFeedbackStore().mu.RLock()
		_, exists := GetHumanFeedbackStore().requests["req-1"]
		GetHumanFeedbackStore().mu.RUnlock()
		if exists {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("human feedback request was not registered")
		}
		time.Sleep(time.Millisecond)
	}
	if err := GetHumanFeedbackStore().SubmitResponse("req-1", "approve"); err != nil {
		t.Fatalf("submit direct response: %v", err)
	}

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("handleHumanFeedback returned error: %v", got.err)
		}
		if got.answer != "approve" {
			t.Fatalf("unexpected response: %q", got.answer)
		}
	case <-time.After(time.Second):
		t.Fatal("human_feedback did not return the direct human response")
	}

	select {
	case message := <-injected:
		t.Fatalf("human feedback was unexpectedly routed to the parent builder: %q", message)
	default:
	}

	GetHumanFeedbackStore().mu.RLock()
	_, requestRetained := GetHumanFeedbackStore().requests["req-1"]
	_, waiterRetained := GetHumanFeedbackStore().waiters["req-1"]
	GetHumanFeedbackStore().mu.RUnlock()
	if requestRetained || waiterRetained {
		t.Fatalf("consumed human response remained in memory: request=%v waiter=%v", requestRetained, waiterRetained)
	}
}

func TestHumanFeedbackTimeoutFromArgs(t *testing.T) {
	tests := []struct {
		name string
		raw  interface{}
		want time.Duration
	}{
		{name: "default", want: 5 * time.Minute},
		{name: "agent value", raw: float64(120), want: 2 * time.Minute},
		{name: "minimum clamp", raw: float64(1), want: 30 * time.Second},
		{name: "maximum clamp", raw: float64(7200), want: 30 * time.Minute},
		{name: "invalid defaults", raw: "soon", want: 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{}
			if tt.raw != nil {
				args["timeout_seconds"] = tt.raw
			}
			if got := humanFeedbackTimeoutFromArgs(args); got != tt.want {
				t.Fatalf("timeout = %s, want %s", got, tt.want)
			}
		})
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

func TestHandleNotifyUserSendsWorkflowSlackWebhook(t *testing.T) {
	original := sendSlackIncomingWebhook
	t.Cleanup(func() { sendSlackIncomingWebhook = original })

	called := false
	sendSlackIncomingWebhook = func(_ context.Context, webhookURL, message string) (string, error) {
		called = true
		if webhookURL != "https://hooks.slack.com/services/T123/B456/secret" {
			t.Fatalf("unexpected webhook URL")
		}
		if message != "Workflow finished" {
			t.Fatalf("message = %q", message)
		}
		return "webhook_ok", nil
	}

	ctx := context.WithValue(context.Background(), BotNotificationDestinationKey, &services.NotificationDestination{
		SlackWebhook: &services.SlackWebhookDest{
			SecretName: "SLACK_NOTIFICATION_WEBHOOK_URL",
			URL:        "https://hooks.slack.com/services/T123/B456/secret",
		},
	})
	raw, err := handleNotifyUser(ctx, map[string]interface{}{"message_for_user": "Workflow finished"})
	if err != nil {
		t.Fatalf("handleNotifyUser: %v", err)
	}
	if !called {
		t.Fatal("workflow Slack webhook was not called")
	}
	var result struct {
		Status    string   `json:"status"`
		Delivered []string `json:"delivered"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Status != "delivered" {
		t.Fatalf("status = %q, result=%s", result.Status, raw)
	}
	found := false
	for _, channel := range result.Delivered {
		if channel == "slack_webhook" {
			found = true
		}
	}
	if !found {
		t.Fatalf("slack_webhook missing from delivered: %v", result.Delivered)
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

func TestHandleNotifyUserEmailToOverridesDestination(t *testing.T) {
	manager := services.GetNotificationManager()
	ch := make(chan *services.NotificationDestination, 1)
	connector := &testUserNotificationConnector{name: "gmail", ch: ch}
	manager.RegisterConnector(connector)
	t.Cleanup(func() {
		manager.UnregisterConnector("gmail")
	})

	ctx := context.WithValue(context.Background(), common.UserIDKey, "user-1")
	ctx = context.WithValue(ctx, BotNotificationDestinationKey, &services.NotificationDestination{
		UserID: "user-1",
		Gmail:  &services.GmailDest{Email: "default@example.com"},
	})

	if _, err := handleNotifyUser(ctx, map[string]interface{}{
		"message_for_user": "FYI: done",
		"email_to":         []interface{}{"Override@Example.com", "ops@example.com"},
		"email_cc":         []interface{}{"cc@example.com"},
	}); err != nil {
		t.Fatalf("handleNotifyUser returned error: %v", err)
	}

	select {
	case dest := <-ch:
		if dest == nil || dest.Gmail == nil || dest.Gmail.Email != "override@example.com, ops@example.com" {
			t.Fatalf("gmail destination = %#v, want replacement To recipients", dest)
		}
		if dest.Content == nil || dest.Content.Gmail == nil {
			t.Fatalf("gmail content = %#v, want Gmail content", dest.Content)
		}
		if got := strings.Join(dest.Content.Gmail.CC, ","); got != "cc@example.com" {
			t.Fatalf("gmail cc = %q, want cc@example.com", got)
		}
	case <-time.After(time.Second):
		t.Fatal("expected Gmail notification")
	}
}
