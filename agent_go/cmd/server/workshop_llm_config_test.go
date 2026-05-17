package server

import (
	"testing"

	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

func TestWorkshopConvertTieredLLMConfigHandlesPartialTiers(t *testing.T) {
	tiered := workshopConvertTieredLLMConfig(&workflowtypes.TieredLLMConfig{
		Tier2: &workflowtypes.AgentLLMConfig{
			Provider: "kimi",
			ModelID:  "kimi-k2.6",
		},
	})

	if tiered == nil {
		t.Fatal("expected non-nil tiered config")
	}
	if tiered.Tier1 != nil {
		t.Fatalf("expected nil tier1, got %+v", tiered.Tier1)
	}
	if tiered.Tier2 == nil {
		t.Fatal("expected non-nil tier2")
	}
	if tiered.Tier2.Provider != "kimi" || tiered.Tier2.ModelID != "kimi-k2.6" {
		t.Fatalf("unexpected tier2 config: %+v", tiered.Tier2)
	}
	if tiered.Tier3 != nil {
		t.Fatalf("expected nil tier3, got %+v", tiered.Tier3)
	}
}
