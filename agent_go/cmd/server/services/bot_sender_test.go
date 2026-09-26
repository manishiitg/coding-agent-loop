package services

import (
	"strings"
	"testing"
)

func TestBotSenderLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  BotIncomingMessage
		want string
	}{
		{"name and email", BotIncomingMessage{Platform: "slack", UserID: "U1", UserName: "Asha", UserEmail: "asha@example.com", Text: "hi"}, "From: Asha <asha@example.com> (Slack)\n\nhi"},
		{"id only name", BotIncomingMessage{Platform: "slack", UserID: "U1", UserName: "U1", UserEmail: "asha@example.com", Text: "hi"}, "From: asha@example.com (Slack)\n\nhi"},
		{"nothing resolved", BotIncomingMessage{Platform: "slack", UserID: "U1", UserName: "U1", Text: "hi"}, "From: Slack user U1\n\nhi"},
		{"whatsapp untouched", BotIncomingMessage{Platform: "whatsapp", UserID: "p", UserName: "Mom", Text: "hi"}, "hi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := tc.msg
			withBotSender(&msg)
			withBotSender(&msg) // once per message
			if msg.Text != tc.want {
				t.Fatalf("text = %q, want %q", msg.Text, tc.want)
			}
		})
	}
}

func TestBotSessionTitle(t *testing.T) {
	msg := BotIncomingMessage{Platform: "slack", UserID: "U1", UserName: "Bob", Text: "<@U0BOT> check   the ACME invoice\nplease"}
	if got := botSessionTitle(msg, "#finance"); got != "Bob: check the ACME invoice please · #finance" {
		t.Fatalf("title = %q", got)
	}
	long := BotIncomingMessage{Platform: "slack", UserID: "U1", UserName: "U1", Text: strings.Repeat("word ", 30)}
	if got := botSessionTitle(long, ""); len([]rune(got)) > 48 || !strings.HasSuffix(got, "…") {
		t.Fatalf("long title = %q", got)
	}
	if got := botSessionTitle(BotIncomingMessage{Platform: "whatsapp", Text: "hi"}, ""); got != "" {
		t.Fatalf("whatsapp title = %q", got)
	}
}
