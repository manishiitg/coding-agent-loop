package workflowtypes

import "testing"

func testExplicitConfig() *PresetLLMConfig {
	builder := &AgentLLMConfig{Provider: "claude-code", ModelID: "claude-sonnet-5"}
	maintenance := &AgentLLMConfig{Provider: "claude-code", ModelID: "claude-opus-4-8"}
	return &PresetLLMConfig{
		SchemaVersion:  LLMConfigSchemaVersion,
		Mode:           LLMConfigModeExplicit,
		BuilderLLM:     builder,
		MaintenanceLLM: maintenance,
		PulseLLM:       builder,
		TieredConfig: &TieredLLMConfig{
			Tier1: maintenance,
			Tier2: builder,
			Tier3: &AgentLLMConfig{Provider: "claude-code", ModelID: "claude-haiku-4-5-20251001"},
		},
	}
}

func TestValidatePresetLLMConfigExplicit(t *testing.T) {
	cfg := testExplicitConfig()
	cfg.ChiefOfStaffLLM = &AgentLLMConfig{Provider: "claude-code", ModelID: "claude-opus-4-8"}
	if err := ValidatePresetLLMConfigPublic(cfg); err != nil {
		t.Fatalf("ValidatePresetLLMConfigPublic() error = %v", err)
	}
}

func TestValidatePresetLLMConfigRequiresBuilder(t *testing.T) {
	cfg := testExplicitConfig()
	cfg.BuilderLLM = nil
	if err := ValidatePresetLLMConfigPublic(cfg); err == nil {
		t.Fatal("ValidatePresetLLMConfigPublic() error = nil, want missing builder error")
	}
}

func TestNormalizePresetLLMConfigMigratesLegacyExplicitShape(t *testing.T) {
	legacyBuilder := &AgentLLMConfig{Provider: "claude-code", ModelID: "claude-sonnet-5"}
	legacyMaintenance := &AgentLLMConfig{Provider: "claude-code", ModelID: "claude-opus-4-8"}
	cfg := &PresetLLMConfig{
		Provider:                "claude-code",
		LegacyModelID:           "claude-sonnet-5",
		LegacyPhaseLLM:          legacyBuilder,
		LegacyAutoImproveLLM:    legacyMaintenance,
		LegacyLLMAllocationMode: "tiered",
		PulseLLM:                legacyBuilder,
		TieredConfig:            &TieredLLMConfig{Tier1: legacyMaintenance, Tier2: legacyBuilder, Tier3: legacyBuilder},
	}

	if !NormalizePresetLLMConfig(cfg) {
		t.Fatal("NormalizePresetLLMConfig() changed = false")
	}
	if cfg.SchemaVersion != LLMConfigSchemaVersion || cfg.Mode != LLMConfigModeExplicit {
		t.Fatalf("normalized schema/mode = %d/%q", cfg.SchemaVersion, cfg.Mode)
	}
	if cfg.BuilderLLM != legacyBuilder || cfg.MaintenanceLLM != legacyMaintenance {
		t.Fatalf("normalized roles = builder:%+v maintenance:%+v", cfg.BuilderLLM, cfg.MaintenanceLLM)
	}
	if cfg.Provider != "" || cfg.LegacyPhaseLLM != nil || cfg.LegacyAutoImproveLLM != nil || cfg.LegacyLLMAllocationMode != "" {
		t.Fatalf("legacy fields not cleared: %+v", cfg)
	}
}

func providerProfile(provider string) *PresetLLMConfig {
	return &PresetLLMConfig{
		SchemaVersion: LLMConfigSchemaVersion,
		Mode:          LLMConfigModeProviderProfile,
		Provider:      provider,
	}
}

func TestNormalizePresetLLMConfigMigratesLegacyCodingAgentToProviderProfile(t *testing.T) {
	cfg := &PresetLLMConfig{
		Provider:                "claude-code",
		LegacyModelID:           "claude-code",
		LegacyLLMAllocationMode: "coding_agent",
	}
	if !NormalizePresetLLMConfig(cfg) {
		t.Fatal("NormalizePresetLLMConfig() changed = false")
	}
	if cfg.Mode != LLMConfigModeProviderProfile || cfg.Provider != "claude-code" {
		t.Fatalf("normalized profile = mode:%q provider:%q", cfg.Mode, cfg.Provider)
	}
	if cfg.BuilderLLM != nil || cfg.TieredConfig != nil || cfg.LegacyModelID != "" {
		t.Fatalf("provider profile retained explicit/legacy fields: %+v", cfg)
	}
}

func TestResolveProviderProfileConfigUsesBuilderDefaults(t *testing.T) {
	tests := []struct {
		provider string
		model    string
		effort   string
	}{
		{provider: "claude-code", model: "claude-opus-4-8", effort: "high"},
		{provider: "codex-cli", model: "gpt-5.6-sol", effort: "high"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			builder, _, ok := ResolveProviderProfileConfig(providerProfile(tt.provider))
			if !ok || builder == nil {
				t.Fatalf("ResolveProviderProfileConfig(%q) = %+v, ok=%v", tt.provider, builder, ok)
			}
			if builder.Provider != tt.provider || builder.ModelID != tt.model {
				t.Fatalf("builder = %+v, want %s/%s", builder, tt.provider, tt.model)
			}
			if got := builder.Options["reasoning_effort"]; got != tt.effort {
				t.Fatalf("reasoning_effort = %#v, want %q", got, tt.effort)
			}
		})
	}
}

func TestResolveProviderProfileMaintenanceConfigUsesProviderDefault(t *testing.T) {
	got, ok := ResolveProviderProfileMaintenanceConfig(providerProfile("claude-code"))
	if !ok || got.Provider != "claude-code" || got.ModelID != "claude-opus-4-8" {
		t.Fatalf("ResolveProviderProfileMaintenanceConfig() = %+v, %v", got, ok)
	}
}

func TestResolveProviderProfileMaintenanceConfigPreservesProviderOptions(t *testing.T) {
	got, ok := ResolveProviderProfileMaintenanceConfig(providerProfile("codex-cli"))
	if !ok || got.Provider != "codex-cli" || got.ModelID != "gpt-5.6-sol" {
		t.Fatalf("ResolveProviderProfileMaintenanceConfig() = %+v, %v", got, ok)
	}
	if got.Options["reasoning_effort"] != "xhigh" {
		t.Fatalf("reasoning_effort = %#v, want xhigh", got.Options["reasoning_effort"])
	}
}

func TestResolveProviderProfilePulseConfigUsesProviderDefault(t *testing.T) {
	got, ok := ResolveProviderProfilePulseConfig(providerProfile("claude-code"))
	if !ok || got.Provider != "claude-code" || got.ModelID != "claude-sonnet-5" {
		t.Fatalf("ResolveProviderProfilePulseConfig() = %+v, %v", got, ok)
	}
}

func TestResolveProviderProfileChiefOfStaffConfigUsesProviderDefault(t *testing.T) {
	got, ok := ResolveProviderProfileChiefOfStaffConfig(providerProfile("claude-code"))
	if !ok || got.Provider != "claude-code" || got.ModelID != "claude-opus-4-8" {
		t.Fatalf("ResolveProviderProfileChiefOfStaffConfig() = %+v, %v", got, ok)
	}
}

func TestResolveCodingAgentMemoryConfigUsesPulseDefault(t *testing.T) {
	got, ok := ResolveCodingAgentMemoryConfig(providerProfile("claude-code"))
	if !ok || got.Provider != "claude-code" || got.ModelID != "claude-sonnet-5" {
		t.Fatalf("ResolveCodingAgentMemoryConfig() = %+v, %v", got, ok)
	}
}
