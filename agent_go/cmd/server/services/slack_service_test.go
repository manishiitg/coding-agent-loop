package services

import "testing"

func TestSlackUserNotificationDestinationRequiresExplicitSlackDestination(t *testing.T) {
	svc := &SlackService{channelID: "CDEFAULT"}

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

func TestSlackFeedbackNotificationDestinationKeepsDefaultFallback(t *testing.T) {
	svc := &SlackService{channelID: "CDEFAULT"}

	channelID, threadTS := svc.pickDestination(nil)
	if channelID != "CDEFAULT" || threadTS != "" {
		t.Fatalf("feedback notification destination = %q/%q, want CDEFAULT/empty", channelID, threadTS)
	}
}
