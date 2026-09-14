package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProductWebhookTriggerValidationAndDTO(t *testing.T) {
	trigger := productWebhookTrigger{
		ID: "4c98bba9-b433-4bf8-b1de-68eebd143a6c", Name: "Issue opened",
		Enabled: true, Message: "Triage the incoming issue",
		Webhook: &WorkflowWebhookConfig{AuthMode: "bearer", EncryptedSecret: "ciphertext"},
	}
	if err := validateProductWebhook(trigger); err != nil {
		t.Fatal(err)
	}
	dto := productWebhookDTO(trigger)
	if dto.Path != "/api/hooks/product/"+trigger.ID || dto.AuthMode != "bearer" || dto.Message != trigger.Message {
		t.Fatalf("dto = %+v", dto)
	}
	for name, mutate := range map[string]func(*productWebhookTrigger){
		"message": func(value *productWebhookTrigger) { value.Message = "" },
		"auth":    func(value *productWebhookTrigger) { value.Webhook.AuthMode = "none" },
		"secret":  func(value *productWebhookTrigger) { value.Webhook.EncryptedSecret = "" },
	} {
		copy := trigger
		webhook := *trigger.Webhook
		copy.Webhook = &webhook
		mutate(&copy)
		if err := validateProductWebhook(copy); err == nil {
			t.Fatalf("%s should be rejected", name)
		}
	}
}

func TestProductManifestStoresTriggersWithoutPlaintextSecret(t *testing.T) {
	manifest := productProjectManifest{Product: "work", ID: "project-1"}
	manifest.Triggers = []productWebhookTrigger{{
		ID: "4c98bba9-b433-4bf8-b1de-68eebd143a6c", Name: "Deploy", Enabled: true,
		Message: "Inspect this deployment", Webhook: &WorkflowWebhookConfig{AuthMode: "github", EncryptedSecret: "encrypted-value"},
	}}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, `"triggers"`) || !strings.Contains(text, `"encrypted_secret":"encrypted-value"`) {
		t.Fatalf("manifest omitted trigger: %s", text)
	}
	if strings.Contains(text, `"secret"`) {
		t.Fatalf("manifest exposed a plaintext secret field: %s", text)
	}
}
