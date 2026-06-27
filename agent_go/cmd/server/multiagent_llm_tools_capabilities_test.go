package server

import (
	"context"
	"testing"

	llm "github.com/manishiitg/multi-llm-provider-go"
)

func TestLLMCapabilitiesIncludeAgyImageProviders(t *testing.T) {
	caps := buildLLMCapabilities(context.Background(), "all", true)

	if !capabilityHasProvider(caps, "read_image", "agy-cli") {
		t.Fatalf("read_image capabilities should include agy-cli")
	}
	if !capabilityHasProvider(caps, "generate_image", "agy-cli") {
		t.Fatalf("generate_image capabilities should include agy-cli")
	}
}

func TestProviderAuthConfiguredTreatsPiProviderKeysAsPiAuth(t *testing.T) {
	configured, source := providerAuthConfigured("pi-cli", &llm.ProviderAPIKeys{
		PiProviderKeys: map[string]string{"zai": "zai-key"},
	})
	if !configured {
		t.Fatalf("pi-cli auth configured = false, want true")
	}
	if source != "Provider-specific Pi API key or workspace provider auth" {
		t.Fatalf("pi-cli auth source = %q", source)
	}
}

func capabilityHasProvider(caps map[string]interface{}, capability, provider string) bool {
	entry, _ := caps[capability].(map[string]interface{})
	if entry == nil {
		return false
	}
	providers, _ := entry["providers"].([]llmCapabilityProvider)
	for _, candidate := range providers {
		if candidate.Provider == provider {
			return true
		}
	}
	return false
}
