package step_based_workflow

import "testing"

func TestTierResolverPreservesModelOptions(t *testing.T) {
	resolver := NewTierResolver(&TieredLLMConfig{
		Tier1: &AgentLLMConfig{
			Provider: "claude-code",
			ModelID:  "sonnet",
			Options: map[string]interface{}{
				"reasoning_effort": "low",
			},
			Fallbacks: []AgentLLMFallback{
				{
					Provider: "claude-code",
					ModelID:  "sonnet",
					Options: map[string]interface{}{
						"reasoning_effort": "medium",
					},
				},
			},
		},
	}, nil)

	config := resolver.ResolveTier(TierHigh)
	if config == nil {
		t.Fatal("expected tier config")
	}
	if got := config.Primary.Options["reasoning_effort"]; got != "low" {
		t.Fatalf("expected primary reasoning_effort=low, got %v", got)
	}
	if len(config.Fallbacks) != 1 {
		t.Fatalf("expected one fallback, got %d", len(config.Fallbacks))
	}
	if got := config.Fallbacks[0].Options["reasoning_effort"]; got != "medium" {
		t.Fatalf("expected fallback reasoning_effort=medium, got %v", got)
	}
}
