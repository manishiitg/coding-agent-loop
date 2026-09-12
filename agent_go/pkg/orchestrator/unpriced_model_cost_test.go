package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"
)

// PLAT-203: harness:cost-ledger:reflection-attribution-and-call-count found
// costs/execution/confida-staging/2026-08-24.json's by_step_and_model block
// for google/gemini-3.7-flash (a pi-cli-routed model) had llm_call_count
// present but no total_cost_usd key at all. Traced this to two compounding
// gaps: (1) pi-cli's own GetModelMetadata (multi-llm-provider-go) returns
// metadata with every *CostPer1MTokens field left at its Go zero value for
// every model it serves -- there is no rate card for pi-cli at all, not
// just for this one model version -- and (2) ModelTokenUsage.TotalCost had
// `omitempty`, so a genuinely-zero-because-unpriced cost was indistinguishable
// from a genuinely-zero-because-free one; the JSON key just vanished.
func TestUnpricedProviderCallsAreExplicitNotAbsent(t *testing.T) {
	modelData := &ModelTokenData{
		Provider:     "pi-cli",
		ModelID:      "google/gemini-3.7-flash",
		InputTokens:  10_000,
		OutputTokens: 2_000,
		LLMCallCount: 4,
	}
	inputCost, outputCost, reasoningCost, cacheCost, totalCost, _, pricingFound := calculatePricingFromModelData(modelData)
	if pricingFound {
		t.Fatal("pi-cli has no rate card for any model; pricingFound should be false")
	}
	if inputCost != 0 || outputCost != 0 || reasoningCost != 0 || cacheCost != 0 || totalCost != 0 {
		t.Fatalf("expected all-zero costs when unpriced, got input=%v output=%v reasoning=%v cache=%v total=%v",
			inputCost, outputCost, reasoningCost, cacheCost, totalCost)
	}

	usage := buildModelTokenUsage(modelData)
	if !usage.Unpriced {
		t.Fatal("ModelTokenUsage.Unpriced should be true for a model with no rate card")
	}

	encoded, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(encoded)
	if !strings.Contains(body, `"total_cost_usd":0`) {
		t.Fatalf("total_cost_usd must be explicitly present (not omitted) for an unpriced call, got: %s", body)
	}
	if !strings.Contains(body, `"unpriced":true`) {
		t.Fatalf("unpriced:true must be present so a $0 total is never mistaken for a genuinely free call, got: %s", body)
	}
}

// A model with a real rate card (claude-code/claude-sonnet-5) must not carry
// the unpriced marker, and its JSON must not regress by suddenly gaining an
// unpriced key that didn't exist before this change.
func TestPricedProviderCallsAreNotMarkedUnpriced(t *testing.T) {
	modelData := &ModelTokenData{
		Provider:     "claude-code",
		ModelID:      "claude-sonnet-5",
		InputTokens:  10_000,
		OutputTokens: 2_000,
		LLMCallCount: 4,
	}
	_, _, _, _, _, _, pricingFound := calculatePricingFromModelData(modelData)
	if !pricingFound {
		t.Fatal("claude-sonnet-5 has a real rate card; pricingFound should be true")
	}

	usage := buildModelTokenUsage(modelData)
	if usage.Unpriced {
		t.Fatal("a priced model must not be marked Unpriced")
	}
	if usage.TotalCost <= 0 {
		t.Fatalf("expected a real nonzero total cost, got %v", usage.TotalCost)
	}

	encoded, err := json.Marshal(usage)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), `"unpriced"`) {
		t.Fatalf("a priced call must not carry the unpriced key at all (omitempty), got: %s", encoded)
	}
}

// TestMuseCLICallsHaveRealPricing pins a real gap found live 2026-09-11:
// getModelMetadata's provider switch had cases for claude-code, codex-cli,
// cursor-cli, and pi-cli but none for muse-cli, so every muse-cli call fell
// through to the "unsupported provider" default and every cost in the
// AgentWorks UI showed $0/unpriced -- even though multi-llm-provider-go's
// musecli.GetMuseModelMetadata already carries real, sourced per-1M-token
// rates for muse-spark-1.3-contributor. Wiring in the missing case is the
// fix; this pins it the same way TestPricedProviderCallsAreNotMarkedUnpriced
// pins claude-code.
func TestMuseCLICallsHaveRealPricing(t *testing.T) {
	modelData := &ModelTokenData{
		Provider:     "muse-cli",
		ModelID:      "muse-spark-1.3-contributor",
		InputTokens:  10_000,
		OutputTokens: 2_000,
		LLMCallCount: 4,
	}
	_, _, _, _, _, _, pricingFound := calculatePricingFromModelData(modelData)
	if !pricingFound {
		t.Fatal("muse-spark-1.3-contributor has a real rate card; pricingFound should be true")
	}

	usage := buildModelTokenUsage(modelData)
	if usage.Unpriced {
		t.Fatal("a priced muse-cli model must not be marked Unpriced")
	}
	if usage.TotalCost <= 0 {
		t.Fatalf("expected a real nonzero total cost, got %v", usage.TotalCost)
	}
}
