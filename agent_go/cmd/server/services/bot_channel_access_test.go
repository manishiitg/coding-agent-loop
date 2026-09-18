package services

import (
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
