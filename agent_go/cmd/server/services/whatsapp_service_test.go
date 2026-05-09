package services

import (
	"strings"
	"testing"
)

func TestParseWhatsAppWorkflowCommandDetailMode(t *testing.T) {
	for _, input := range []string{"@full", "@verbose", "@details", "@concise", "@short", "@brief"} {
		cmd, arg, ok := parseWhatsAppWorkflowCommand(input)
		if !ok {
			t.Fatalf("expected %q to parse as workflow command", input)
		}
		if cmd == "" {
			t.Fatalf("empty command for %q", input)
		}
		if arg != "" {
			t.Fatalf("arg for %q = %q, want empty", input, arg)
		}
	}
}

func TestParseWhatsAppWorkflowCommandHelp(t *testing.T) {
	for _, input := range []string{"@", "@help", "@commands", "@?"} {
		cmd, _, ok := parseWhatsAppWorkflowCommand(input)
		if !ok {
			t.Fatalf("expected %q to parse as workflow help command", input)
		}
		if cmd != "help" {
			t.Fatalf("command for %q = %q, want help", input, cmd)
		}
	}
}

func TestWhatsAppCommandSuggestionsMentionDetailMode(t *testing.T) {
	list := formatWhatsAppWorkflowList([]whatsappWorkflowCandidate{{Number: 1, Label: "RCA"}})
	if !strings.Contains(list, "@full") || !strings.Contains(list, "@concise") {
		t.Fatalf("workflow list should mention detail mode commands: %q", list)
	}

	unknown := unknownWhatsAppWorkflowCommandMessage("bad")
	if !strings.Contains(unknown, "@full") || !strings.Contains(unknown, "@concise") {
		t.Fatalf("unknown command help should mention detail mode commands: %q", unknown)
	}
}

func TestWhatsAppDetailModeStatusUsesProvider(t *testing.T) {
	w := NewWhatsAppService("")
	w.SetBotThreadStatusProvider(func(threadID ThreadID) BotThreadStatus {
		if threadID.Platform != "whatsapp" || threadID.ChannelID != "chat-1" || threadID.ThreadTS != "chat-1" {
			t.Fatalf("unexpected thread id: %#v", threadID)
		}
		return BotThreadStatus{HasSession: true, DetailMode: "full"}
	})

	status := w.formatWhatsAppDetailModeStatus("chat-1")
	if !strings.Contains(status, "Detail mode: full") {
		t.Fatalf("status should include current detail mode: %q", status)
	}
}
