package server

import "testing"

func TestMergeWorkflowCapabilitiesUpdatePreservesNotificationsForOlderClients(t *testing.T) {
	globalNames := []string{"SLACK_WEBHOOK", "GLOBAL_APP_TOKEN"}
	existing := WorkflowCapabilities{
		Notifications: &WorkflowNotificationConfig{SlackWebhookSecretName: "SLACK_WEBHOOK"},
	}
	incoming := &WorkflowCapabilities{
		BrowserMode:               "auto",
		SelectedSecrets:           []string{"SLACK_WEBHOOK", "APP_TOKEN"},
		SelectedGlobalSecretNames: &globalNames,
	}
	got := mergeWorkflowCapabilitiesUpdate(existing, incoming)
	if got.Notifications == nil || got.Notifications.SlackWebhookSecretName != "SLACK_WEBHOOK" {
		t.Fatalf("notifications were not preserved: %#v", got.Notifications)
	}
	if len(got.SelectedSecrets) != 1 || got.SelectedSecrets[0] != "APP_TOKEN" {
		t.Fatalf("agent secrets = %#v, want only APP_TOKEN", got.SelectedSecrets)
	}
	if got.SelectedGlobalSecretNames == nil || len(*got.SelectedGlobalSecretNames) != 1 || (*got.SelectedGlobalSecretNames)[0] != "GLOBAL_APP_TOKEN" {
		t.Fatalf("global agent secrets = %#v, want only GLOBAL_APP_TOKEN", got.SelectedGlobalSecretNames)
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
