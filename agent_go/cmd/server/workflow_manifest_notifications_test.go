package server

import "testing"

func TestMergeWorkflowCapabilitiesUpdatePreservesNotificationsForOlderClients(t *testing.T) {
	existing := WorkflowCapabilities{
		Notifications: &WorkflowNotificationConfig{SlackWebhookSecretName: "SLACK_WEBHOOK"},
	}
	incoming := &WorkflowCapabilities{BrowserMode: "auto"}
	got := mergeWorkflowCapabilitiesUpdate(existing, incoming)
	if got.Notifications == nil || got.Notifications.SlackWebhookSecretName != "SLACK_WEBHOOK" {
		t.Fatalf("notifications were not preserved: %#v", got.Notifications)
	}
}

func TestMergeWorkflowCapabilitiesUpdateCanDisableNotifications(t *testing.T) {
	existing := WorkflowCapabilities{
		Notifications: &WorkflowNotificationConfig{SlackWebhookSecretName: "SLACK_WEBHOOK"},
	}
	incoming := &WorkflowCapabilities{
		Notifications: &WorkflowNotificationConfig{},
	}
	got := mergeWorkflowCapabilitiesUpdate(existing, incoming)
	if got.Notifications != nil {
		t.Fatalf("notifications = %#v, want disabled", got.Notifications)
	}
}
