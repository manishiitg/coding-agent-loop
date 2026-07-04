package server

import "testing"

func TestResolvePublishPasswordSecretNameOnlyAllowsPasswordSecrets(t *testing.T) {
	config := &WorkflowPublishConfig{
		Notes: "Use workflow secret PUBLISH_PASSWORD for StatiCrypt.",
		Destinations: []WorkflowPublishDestination{
			{ID: "host", SecretName: "VERCEL_TOKEN", Visibility: "private"},
			{ID: "mirror", SecretName: "STATICRYPT_PASS"},
		},
	}
	status := &WorkflowPublishStatus{
		SecretName: "PUBLISH_PASSWORD",
		Summary:    "Password protected with secret REPORT_PASSPHRASE.",
	}

	for _, name := range []string{"PUBLISH_PASSWORD", "STATICRYPT_PASS", "REPORT_PASSPHRASE"} {
		t.Run(name, func(t *testing.T) {
			got, ok := resolvePublishPasswordSecretName(name, config, status)
			if !ok || got != name {
				t.Fatalf("resolvePublishPasswordSecretName(%q) = (%q, %v), want (%q, true)", name, got, ok, name)
			}
		})
	}

	if got, ok := resolvePublishPasswordSecretName("VERCEL_TOKEN", config, status); ok {
		t.Fatalf("deploy token should not be allowed, got %q", got)
	}
}

func TestResolvePublishPasswordSecretNameInfersSingleSecret(t *testing.T) {
	config := &WorkflowPublishConfig{
		Destinations: []WorkflowPublishDestination{
			{ID: "surge", Visibility: "private", SecretName: "PUBLISH_PASSWORD"},
		},
	}

	got, ok := resolvePublishPasswordSecretName("", config, nil)
	if !ok || got != "PUBLISH_PASSWORD" {
		t.Fatalf("resolvePublishPasswordSecretName(empty) = (%q, %v), want PUBLISH_PASSWORD", got, ok)
	}
}
