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

func TestWorkshopConvertAgentLLMConfigPreservesPublishedOptions(t *testing.T) {
	converted := workshopConvertAgentLLMConfig(&workflowtypes.AgentLLMConfig{
		PublishedLLMID: "claude-low",
		Provider:       "claude-code",
		ModelID:        "sonnet",
		Options: map[string]interface{}{
			"reasoning_effort": "low",
		},
		Fallbacks: []workflowtypes.AgentLLMFallback{
			{
				PublishedLLMID: "claude-medium",
				Provider:       "claude-code",
				ModelID:        "sonnet",
				Options: map[string]interface{}{
					"reasoning_effort": "medium",
				},
			},
		},
	})

	if converted == nil {
		t.Fatal("expected converted config")
	}
	if converted.PublishedLLMID != "claude-low" {
		t.Fatalf("expected published id to be preserved, got %q", converted.PublishedLLMID)
	}
	if got := converted.Options["reasoning_effort"]; got != "low" {
		t.Fatalf("expected primary reasoning_effort=low, got %v", got)
	}
	if len(converted.Fallbacks) != 1 {
		t.Fatalf("expected one fallback, got %d", len(converted.Fallbacks))
	}
	if converted.Fallbacks[0].PublishedLLMID != "claude-medium" {
		t.Fatalf("expected fallback published id to be preserved, got %q", converted.Fallbacks[0].PublishedLLMID)
	}
	if got := converted.Fallbacks[0].Options["reasoning_effort"]; got != "medium" {
		t.Fatalf("expected fallback reasoning_effort=medium, got %v", got)
	}
}
