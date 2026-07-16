package server

import "testing"

func TestWithChiefNotificationConfigSelectsSecretWithoutLosingCapabilities(t *testing.T) {
	caps := WorkflowCapabilities{
		SelectedServers: []string{"server-a"},
		SelectedSecrets: []string{"EXISTING_SECRET"},
		BrowserMode:     "auto",
	}

	updated := withChiefNotificationConfig(caps, " SLACK_NOTIFICATION_WEBHOOK_URL ")
	if updated.Notifications == nil || updated.Notifications.SlackWebhookSecretName != "SLACK_NOTIFICATION_WEBHOOK_URL" {
		t.Fatalf("notifications = %#v", updated.Notifications)
	}
	if len(updated.SelectedSecrets) != 2 || updated.SelectedSecrets[0] != "EXISTING_SECRET" || updated.SelectedSecrets[1] != "SLACK_NOTIFICATION_WEBHOOK_URL" {
		t.Fatalf("selected secrets = %#v", updated.SelectedSecrets)
	}
	if len(updated.SelectedServers) != 1 || updated.SelectedServers[0] != "server-a" || updated.BrowserMode != "auto" {
		t.Fatalf("unrelated capabilities changed: %#v", updated)
	}
	if len(caps.SelectedSecrets) != 1 {
		t.Fatalf("input capabilities were mutated: %#v", caps.SelectedSecrets)
	}
}

func TestWithChiefNotificationConfigIsIdempotentAndCanDisable(t *testing.T) {
	caps := WorkflowCapabilities{
		SelectedSecrets: []string{"SLACK_NOTIFICATION_WEBHOOK_URL"},
		Notifications: &WorkflowNotificationConfig{
			SlackWebhookSecretName: "SLACK_NOTIFICATION_WEBHOOK_URL",
		},
	}

	updated := withChiefNotificationConfig(caps, "SLACK_NOTIFICATION_WEBHOOK_URL")
	if len(updated.SelectedSecrets) != 1 {
		t.Fatalf("duplicate notification secret attached: %#v", updated.SelectedSecrets)
	}
	disabled := withChiefNotificationConfig(updated, "")
	if disabled.Notifications != nil {
		t.Fatalf("notifications = %#v, want nil", disabled.Notifications)
	}
	if len(disabled.SelectedSecrets) != 1 || disabled.SelectedSecrets[0] != "SLACK_NOTIFICATION_WEBHOOK_URL" {
		t.Fatalf("disabling should not delete a reusable secret selection: %#v", disabled.SelectedSecrets)
	}
}

func TestResolveChiefSlackNotificationState(t *testing.T) {
	caps := withChiefNotificationConfig(WorkflowCapabilities{}, "CHIEF_SLACK_WEBHOOK")
	got := resolveSlackNotificationState(
		"chief-of-staff-slack-webhook",
		"Chief of Staff Slack webhook",
		caps,
		"https://hooks.slack.com/services/T123/B456/secret",
		true,
	)
	if got.State != workflowNotificationStateReady {
		t.Fatalf("state = %q, want ready", got.State)
	}
	if got.SecretName != "CHIEF_SLACK_WEBHOOK" {
		t.Fatalf("secret name = %q", got.SecretName)
	}
}

func TestNotificationToolCategoryIsAvailableToCodingAgents(t *testing.T) {
	if !isMCPBridgeCustomToolCategory("notification_tools") {
		t.Fatal("notification_tools must be exposed through the coding-agent API bridge")
	}
}
