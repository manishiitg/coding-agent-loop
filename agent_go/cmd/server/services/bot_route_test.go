package services

import (
	"testing"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/chathistory"
)

func TestSlackBotTrafficAllowed(t *testing.T) {
	on := &chathistory.BotConnectorConfig{Enabled: true, BotMode: true}
	off := &chathistory.BotConnectorConfig{Enabled: false, BotMode: false}
	halfOn := &chathistory.BotConnectorConfig{Enabled: true, BotMode: false}
	for _, tc := range []struct {
		name   string
		cfg    *chathistory.BotConnectorConfig
		routed bool
		want   bool
	}{
		{"missing config denies", nil, false, false},
		{"missing config denies routed", nil, true, false},
		{"switch on covers unrouted", on, false, true},
		{"switch on covers routed", on, true, true},
		{"route enables channel while switch off", off, true, true},
		{"unrouted stays silent while switch off", off, false, false},
		{"half switch denies unrouted", halfOn, false, false},
		{"route enables channel while switch half on", halfOn, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := SlackBotTrafficAllowed(tc.cfg, tc.routed); got != tc.want {
				t.Fatalf("SlackBotTrafficAllowed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSlackBotConfigHasRoutes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed string
		want    bool
	}{
		{"empty", "", false},
		{"empty object", "{}", false},
		{"empty array", "[]", false},
		{"garbage", "not-json", false},
		{"one route", `{"C123":{"workflow_id":"demo","workspace_path":"Workflow/demo"}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &chathistory.BotConnectorConfig{AllowedChannels: tc.allowed}
			if got := SlackBotConfigHasRoutes(cfg); got != tc.want {
				t.Fatalf("SlackBotConfigHasRoutes = %v, want %v", got, tc.want)
			}
		})
	}
	if SlackBotConfigHasRoutes(nil) {
		t.Fatal("nil config has routes")
	}
}
