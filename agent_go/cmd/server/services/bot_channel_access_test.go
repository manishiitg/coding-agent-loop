package services

import (
	"context"
	"strings"
	"testing"
)

func TestUnroutedSlackAccessExplainsChannelSetup(t *testing.T) {
	for _, email := range []string{"", "person@example.com"} {
		t.Run(email, func(t *testing.T) {
			t.Setenv("BOT_ALLOWED_EMAILS", "other@example.com")
			m := NewBotConversationManager(nil, "", "")
			c := &testBotConnector{name: "slack", supportsThreads: true}
			m.RegisterConnector(c)
			m.HandleIncomingMessage(BotIncomingMessage{Platform: "slack", UserID: "U1", UserEmail: email, ChannelID: "CNEW", ThreadTS: "new-thread", Text: "hi", IsMention: true})
			c.mu.Lock()
			defer c.mu.Unlock()
			if len(c.sent) != 1 || !strings.Contains(c.sent[0], "This channel isn't connected") || !strings.Contains(c.sent[0], "Setup > Connectors > Slack") {
				t.Fatalf("unexpected channel guidance: %v", c.sent)
			}
		})
	}
	if got := unroutedBotAccessMessage("whatsapp", "account denied"); got != "account denied" {
		t.Fatalf("changed other connector access error: %s", got)
	}
}

func TestUnroutedSlackChannelMentionExplainsSetupForAllowedUser(t *testing.T) {
	t.Setenv("BOT_ALLOWED_EMAILS", "person@example.com")
	m := NewBotConversationManager(nil, "", "")
	c := &testBotConnector{name: "slack", supportsThreads: true}
	m.RegisterConnector(c)

	for _, channelID := range []string{"CNEW", "GPRIVATE"} {
		m.HandleIncomingMessage(BotIncomingMessage{Platform: "slack", UserID: "U1", UserEmail: "person@example.com", ChannelID: channelID, ThreadTS: "thread", Text: "hi", IsMention: true})
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) != 2 {
		t.Fatalf("expected setup guidance for both channels, got %v", c.sent)
	}
	for _, sent := range c.sent {
		if !strings.Contains(sent, "This channel isn't connected") || !strings.Contains(sent, "Setup > Connectors > Slack") || strings.Contains(sent, "Selected provider") {
			t.Fatalf("unexpected channel guidance: %q", sent)
		}
	}
}

func TestUnroutedSlackDirectMessageStillAllowsGenericChat(t *testing.T) {
	m := NewBotConversationManager(nil, "", "")
	c := &testBotConnector{name: "slack", supportsThreads: true}
	m.RegisterConnector(c)
	msg := BotIncomingMessage{Platform: "slack", ChannelID: "DUSER", IsMention: true}
	if !m.authorizeWorkflowRouteForMessage(context.Background(), &msg, ThreadID{Platform: "slack", ChannelID: "DUSER"}, nil) {
		t.Fatal("unrouted Slack direct message was rejected")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sent) != 0 {
		t.Fatalf("direct message got channel setup guidance: %v", c.sent)
	}
}
