package server

import "testing"

func TestNotificationDestinationFromQueryResolvesWorkflowSlackWebhookSecret(t *testing.T) {
	req := QueryRequest{
		NotificationSlackWebhookSecretName: "SLACK_NOTIFICATION_WEBHOOK_URL",
		DecryptedSecrets: []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		}{
			{Name: "SLACK_NOTIFICATION_WEBHOOK_URL", Value: "https://hooks.slack.com/services/T123/B456/secret"},
		},
	}
	dest := notificationDestinationFromQuery(req, "user-1")
	if dest == nil || dest.SlackWebhook == nil {
		t.Fatalf("destination = %#v, want Slack webhook", dest)
	}
	if dest.SlackWebhook.SecretName != "SLACK_NOTIFICATION_WEBHOOK_URL" {
		t.Fatalf("secret name = %q", dest.SlackWebhook.SecretName)
	}
	if dest.SlackWebhook.URL != "https://hooks.slack.com/services/T123/B456/secret" {
		t.Fatal("webhook secret was not resolved")
	}
}

func TestNotificationDestinationFromQueryKeepsMissingWorkflowWebhookVisible(t *testing.T) {
	req := QueryRequest{NotificationSlackWebhookSecretName: "MISSING_SLACK_WEBHOOK"}
	dest := notificationDestinationFromQuery(req, "")
	if dest == nil || dest.SlackWebhook == nil {
		t.Fatalf("destination = %#v, want unresolved Slack webhook marker", dest)
	}
	if dest.SlackWebhook.URL != "" {
		t.Fatal("unexpected value for missing webhook secret")
	}
}
