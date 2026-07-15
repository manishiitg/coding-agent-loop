package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func notificationTestManifest(secretName string, selected ...string) *WorkflowManifest {
	return &WorkflowManifest{
		Capabilities: WorkflowCapabilities{
			SelectedSecrets: selected,
			Notifications: &WorkflowNotificationConfig{
				SlackWebhookSecretName: secretName,
			},
		},
	}
}

func TestWorkflowNotificationStatusNeverSerializesWebhookURL(t *testing.T) {
	const webhookURL = "https://hooks.slack.com/services/T123/B456/TOPSECRET"
	destination := resolveWorkflowSlackNotificationState(
		notificationTestManifest("SLACK_NOTIFICATION_WEBHOOK_URL", "SLACK_NOTIFICATION_WEBHOOK_URL"),
		webhookURL,
		true,
	)
	encoded, err := json.Marshal(destination)
	if err != nil {
		t.Fatalf("marshal destination: %v", err)
	}
	if strings.Contains(string(encoded), webhookURL) || strings.Contains(string(encoded), "TOPSECRET") {
		t.Fatalf("serialized notification status leaked the webhook URL: %s", encoded)
	}
}

func TestResolveWorkflowSlackNotificationState(t *testing.T) {
	tests := []struct {
		name        string
		manifest    *WorkflowManifest
		secretValue string
		secretFound bool
		wantState   string
	}{
		{name: "not configured", manifest: notificationTestManifest(""), wantState: workflowNotificationStateNotConfigured},
		{name: "not selected", manifest: notificationTestManifest("SLACK_NOTIFICATION_WEBHOOK_URL"), secretValue: "https://hooks.slack.com/services/T/B/S", secretFound: true, wantState: workflowNotificationStateMissingSecret},
		{name: "missing", manifest: notificationTestManifest("SLACK_NOTIFICATION_WEBHOOK_URL", "SLACK_NOTIFICATION_WEBHOOK_URL"), wantState: workflowNotificationStateMissingSecret},
		{name: "invalid", manifest: notificationTestManifest("SLACK_NOTIFICATION_WEBHOOK_URL", "SLACK_NOTIFICATION_WEBHOOK_URL"), secretValue: "https://example.com/hook", secretFound: true, wantState: workflowNotificationStateInvalidSecret},
		{name: "ready", manifest: notificationTestManifest("SLACK_NOTIFICATION_WEBHOOK_URL", "SLACK_NOTIFICATION_WEBHOOK_URL"), secretValue: "https://hooks.slack.com/services/T123/B456/secret", secretFound: true, wantState: workflowNotificationStateReady},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := resolveWorkflowSlackNotificationState(test.manifest, test.secretValue, test.secretFound)
			if got.State != test.wantState {
				t.Fatalf("state = %q, want %q", got.State, test.wantState)
			}
		})
	}
}
