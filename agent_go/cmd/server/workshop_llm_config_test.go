package server

import (
	"testing"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
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

func TestWorkshopResolveLLMConfigExpandsCodingAgentMode(t *testing.T) {
	defaults, ok := llmproviders.GetCodingAgentDefaultTierModels(llmproviders.ProviderClaudeCode)
	if !ok {
		t.Fatal("expected Claude Code coding-agent defaults")
	}
	if defaults.Builder.ModelID != "claude-opus-4-8" ||
		defaults.High.ModelID == "claude-fable-5" ||
		defaults.Medium.ModelID == "claude-fable-5" ||
		defaults.Low.ModelID == "claude-fable-5" ||
		defaults.Maintenance.ModelID != "claude-opus-4-8" ||
		defaults.Pulse.ModelID != "claude-sonnet-5" {
		t.Fatalf("opus 4.8 should be builder/maintenance default and sonnet 5 should remain the pulse default, got defaults: %+v", defaults)
	}

	builder, tiered := workshopResolveLLMConfig(&workflowtypes.PresetLLMConfig{
		SchemaVersion: workflowtypes.LLMConfigSchemaVersion,
		Mode:          workflowtypes.LLMConfigModeProviderProfile,
		Provider:      "claude-code",
	})

	if builder == nil {
		t.Fatal("expected provider profile builder LLM")
	}
	if builder.Provider != defaults.Builder.Provider || builder.ModelID != defaults.Builder.ModelID {
		t.Fatalf("unexpected builder config: %+v", builder)
	}
	maintenance := workshopResolveMaintenanceLLMConfig(&workflowtypes.PresetLLMConfig{
		SchemaVersion: workflowtypes.LLMConfigSchemaVersion,
		Mode:          workflowtypes.LLMConfigModeProviderProfile,
		Provider:      "claude-code",
	})
	if maintenance == nil {
		t.Fatal("expected coding agent maintenance/advisor LLM")
	}
	if maintenance.Provider != defaults.Maintenance.Provider || maintenance.ModelID != defaults.Maintenance.ModelID {
		t.Fatalf("unexpected maintenance config: %+v", maintenance)
	}
	if tiered == nil || tiered.Tier1 == nil || tiered.Tier2 == nil || tiered.Tier3 == nil {
		t.Fatalf("expected full tiered config, got %+v", tiered)
	}
	if tiered.Tier1.Provider != defaults.High.Provider || tiered.Tier1.ModelID != defaults.High.ModelID {
		t.Fatalf("unexpected high tier: %+v", tiered.Tier1)
	}
	if tiered.Tier2.Provider != defaults.Medium.Provider || tiered.Tier2.ModelID != defaults.Medium.ModelID {
		t.Fatalf("unexpected medium tier: %+v", tiered.Tier2)
	}
	if tiered.Tier3.Provider != defaults.Low.Provider || tiered.Tier3.ModelID != defaults.Low.ModelID {
		t.Fatalf("unexpected low tier: %+v", tiered.Tier3)
	}
}
