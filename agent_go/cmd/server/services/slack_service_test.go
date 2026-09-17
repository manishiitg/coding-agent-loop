package services

import (
	"encoding/json"
	"github.com/slack-go/slack"
	"strings"
	"testing"
)

func TestSlackUserNotificationDestinationRequiresExplicitSlackDestination(t *testing.T) {
	svc := &SlackService{}

	channelID, threadTS := svc.pickUserNotificationDestination(nil)
	if channelID != "" || threadTS != "" {
		t.Fatalf("user notification destination without Slack hint = %q/%q, want empty", channelID, threadTS)
	}

	channelID, threadTS = svc.pickUserNotificationDestination(&NotificationDestination{})
	if channelID != "" || threadTS != "" {
		t.Fatalf("user notification destination without Slack hint = %q/%q, want empty", channelID, threadTS)
	}

	channelID, threadTS = svc.pickUserNotificationDestination(&NotificationDestination{
		Slack: &SlackDest{ChannelID: "C123", ThreadTS: "171.1"},
	})
	if channelID != "C123" || threadTS != "171.1" {
		t.Fatalf("user notification destination = %q/%q, want C123/171.1", channelID, threadTS)
	}
}

func TestSlackFeedbackNotificationHasNoDefaultFallback(t *testing.T) {
	svc := &SlackService{}

	channelID, threadTS := svc.pickDestination(nil)
	if channelID != "" || threadTS != "" {
		t.Fatalf("feedback notification destination = %q/%q, want empty/empty", channelID, threadTS)
	}
}

func TestSlackConnectionDoesNotRequireDefaultChannel(t *testing.T) {
	svc := &SlackService{enabled: true, client: slack.New("test")}
	if !svc.IsEnabled() {
		t.Fatal("connector requires a legacy channel")
	}
	var cfg SlackConfig
	if err := json.Unmarshal([]byte(`{"enabled":true,"channel_id":"CLEGACY"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "channel_id") {
		t.Fatal("legacy default channel survived serialization")
	}
}
