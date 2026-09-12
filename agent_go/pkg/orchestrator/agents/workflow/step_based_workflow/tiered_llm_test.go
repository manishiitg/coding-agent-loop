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
		},
	}, nil)

	config := resolver.ResolveTier(TierHigh)
	if config == nil {
		t.Fatal("expected tier config")
	}
	if got := config.Primary.Options["reasoning_effort"]; got != "low" {
		t.Fatalf("expected primary reasoning_effort=low, got %v", got)
	}

}

func TestTierResolverDoesNotSubstituteAnotherTier(t *testing.T) {
	resolver := NewTierResolver(&TieredLLMConfig{
		Tier1: &AgentLLMConfig{Provider: "codex-cli", ModelID: "selected"},
	}, nil)
	for _, tier := range []TierLevel{TierMedium, TierLow, TierLevel(99)} {
		if got := resolver.ResolveTier(tier); got != nil {
			t.Fatalf("tier %v silently selected another model: %+v", tier, got)
		}
	}
	if got := NewTierResolver(nil, nil).ResolveTier(TierHigh); got != nil {
		t.Fatalf("missing config selected a model: %+v", got)
	}
}
