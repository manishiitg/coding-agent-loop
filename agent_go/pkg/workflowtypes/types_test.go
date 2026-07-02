package workflowtypes

import "testing"

func TestValidatePresetLLMConfigAllowsAutoImproveOverrideWithBaseModel(t *testing.T) {
	cfg := &PresetLLMConfig{
		Provider: "claude-code",
		ModelID:  "claude-opus-4-6",
		AutoImproveLLM: &AgentLLMConfig{
			Provider: "claude-code",
			ModelID:  "claude-sonnet-4-6",
		},
	}

	if err := ValidatePresetLLMConfigPublic(cfg); err != nil {
		t.Fatalf("ValidatePresetLLMConfigPublic() error = %v", err)
	}
}

func TestValidatePresetLLMConfigAllowsPulseOverrideWithBaseModel(t *testing.T) {
	cfg := &PresetLLMConfig{
		Provider: "claude-code",
		ModelID:  "claude-opus-4-6",
		PulseLLM: &AgentLLMConfig{
			Provider: "claude-code",
			ModelID:  "claude-sonnet-4-6",
		},
	}

	if err := ValidatePresetLLMConfigPublic(cfg); err != nil {
		t.Fatalf("ValidatePresetLLMConfigPublic() error = %v", err)
	}
}

func TestValidatePresetLLMConfigAllowsChiefOfStaffOverrideWithBaseModel(t *testing.T) {
	cfg := &PresetLLMConfig{
		Provider: "claude-code",
		ModelID:  "claude-opus-4-6",
		ChiefOfStaffLLM: &AgentLLMConfig{
			Provider: "claude-code",
			ModelID:  "claude-sonnet-5",
		},
	}

	if err := ValidatePresetLLMConfigPublic(cfg); err != nil {
		t.Fatalf("ValidatePresetLLMConfigPublic() error = %v", err)
	}
}

func TestValidatePresetLLMConfigDoesNotTreatPulseOverrideAsBaseModel(t *testing.T) {
	cfg := &PresetLLMConfig{
		PulseLLM: &AgentLLMConfig{
			Provider: "claude-code",
			ModelID:  "claude-sonnet-4-6",
		},
	}

	if err := ValidatePresetLLMConfigPublic(cfg); err == nil {
		t.Fatal("ValidatePresetLLMConfigPublic() error = nil, want missing base model error")
	}
}

func TestValidatePresetLLMConfigDoesNotTreatChiefOfStaffOverrideAsBaseModel(t *testing.T) {
	cfg := &PresetLLMConfig{
		ChiefOfStaffLLM: &AgentLLMConfig{
			Provider: "claude-code",
			ModelID:  "claude-sonnet-5",
		},
	}

	if err := ValidatePresetLLMConfigPublic(cfg); err == nil {
		t.Fatal("ValidatePresetLLMConfigPublic() error = nil, want missing base model error")
	}
}

func TestValidatePresetLLMConfigDoesNotTreatAutoImproveOverrideAsBaseModel(t *testing.T) {
	cfg := &PresetLLMConfig{
		AutoImproveLLM: &AgentLLMConfig{
			Provider: "claude-code",
			ModelID:  "claude-sonnet-4-6",
		},
	}

	if err := ValidatePresetLLMConfigPublic(cfg); err == nil {
		t.Fatal("ValidatePresetLLMConfigPublic() error = nil, want missing base model error")
	}
}

func TestResolveCodingAgentAutoImproveConfigUsesProviderDefault(t *testing.T) {
	cfg := &PresetLLMConfig{
		Provider:          "claude-code",
		ModelID:           "claude-code",
		LLMAllocationMode: LLMAllocationModeCodingAgent,
	}

	got, ok := ResolveCodingAgentAutoImproveConfig(cfg)
	if !ok {
		t.Fatal("ResolveCodingAgentAutoImproveConfig() ok = false")
	}
	if got.Provider != "claude-code" || got.ModelID != "claude-fable-5" {
		t.Fatalf("ResolveCodingAgentAutoImproveConfig() = %+v, want claude-code/claude-fable-5", got)
	}
}

func TestResolveCodingAgentAutoImproveConfigPreservesProviderOptions(t *testing.T) {
	cfg := &PresetLLMConfig{
		Provider:          "codex-cli",
		ModelID:           "codex-cli",
		LLMAllocationMode: LLMAllocationModeCodingAgent,
	}

	got, ok := ResolveCodingAgentAutoImproveConfig(cfg)
	if !ok {
		t.Fatal("ResolveCodingAgentAutoImproveConfig() ok = false")
	}
	if got.Provider != "codex-cli" || got.ModelID != "gpt-5.5" {
		t.Fatalf("ResolveCodingAgentAutoImproveConfig() = %+v, want codex-cli/gpt-5.5", got)
	}
	if got.Options["reasoning_effort"] != "xhigh" {
		t.Fatalf("reasoning_effort = %#v, want xhigh", got.Options["reasoning_effort"])
	}
}

func TestResolveCodingAgentPulseConfigUsesProviderDefault(t *testing.T) {
	cfg := &PresetLLMConfig{
		Provider:          "claude-code",
		ModelID:           "claude-code",
		LLMAllocationMode: LLMAllocationModeCodingAgent,
	}

	got, ok := ResolveCodingAgentPulseConfig(cfg)
	if !ok {
		t.Fatal("ResolveCodingAgentPulseConfig() ok = false")
	}
	if got.Provider != "claude-code" || got.ModelID != "claude-sonnet-5" {
		t.Fatalf("ResolveCodingAgentPulseConfig() = %+v, want claude-code/claude-sonnet-5", got)
	}
	if got.Options["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got.Options["reasoning_effort"])
	}
}

func TestResolveCodingAgentChiefOfStaffConfigUsesProviderDefault(t *testing.T) {
	cfg := &PresetLLMConfig{
		Provider:          "claude-code",
		ModelID:           "claude-code",
		LLMAllocationMode: LLMAllocationModeCodingAgent,
	}

	got, ok := ResolveCodingAgentChiefOfStaffConfig(cfg)
	if !ok {
		t.Fatal("ResolveCodingAgentChiefOfStaffConfig() ok = false")
	}
	if got.Provider != "claude-code" || got.ModelID != "claude-fable-5" {
		t.Fatalf("ResolveCodingAgentChiefOfStaffConfig() = %+v, want claude-code/claude-fable-5", got)
	}
	if got.Options["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got.Options["reasoning_effort"])
	}
}

func TestResolveCodingAgentMemoryConfigUsesPulseDefault(t *testing.T) {
	cfg := &PresetLLMConfig{
		Provider:          "claude-code",
		ModelID:           "claude-code",
		LLMAllocationMode: LLMAllocationModeCodingAgent,
	}

	got, ok := ResolveCodingAgentMemoryConfig(cfg)
	if !ok {
		t.Fatal("ResolveCodingAgentMemoryConfig() ok = false")
	}
	if got.Provider != "claude-code" || got.ModelID != "claude-sonnet-5" {
		t.Fatalf("ResolveCodingAgentMemoryConfig() = %+v, want claude-code/claude-sonnet-5", got)
	}
	if got.Options["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort = %#v, want high", got.Options["reasoning_effort"])
	}
}
