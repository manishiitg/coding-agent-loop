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
