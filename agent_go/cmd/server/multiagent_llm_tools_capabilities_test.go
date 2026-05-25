package server

import (
	"context"
	"testing"
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
