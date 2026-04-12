// Package workflowtypes holds the shared type definitions and constants for
// workflow configuration: LLM config (primary + tiered), workflow phase
// status, and phase option selections. Everything here is pure data —
// no persistence, no SQL.
package workflowtypes

import (
	"fmt"

	llmproviders "github.com/manishiitg/multi-llm-provider-go"
)

// Workflow status/phase constants.
const (
	WorkflowStatusPreVerification  = "execution"
	WorkflowStatusPostVerification = "post-verification"
	WorkflowStatusEvalExecution    = "evaluation-execution"
	WorkflowStatusReportExecution  = "report-execution"
	WorkflowStatusWorkflowBuilder  = "workflow-builder"
	WorkflowStatusEvalBuilder      = "evaluation-builder"
)

// PresetLLMConfig represents LLM configuration stored with workflow presets.
type PresetLLMConfig struct {
	// Legacy single-model form (backward compat).
	Provider string `json:"provider,omitempty"`
	ModelID  string `json:"model_id,omitempty"`

	// Agent-specific defaults — take priority over the legacy single-model form.
	LearningLLM *AgentLLMConfig `json:"learning_llm,omitempty"`
	PhaseLLM    *AgentLLMConfig `json:"phase_llm,omitempty"`

	// Feature toggles.
	UseKnowledgebase           *bool `json:"use_knowledgebase,omitempty"`
	EnableContextSummarization *bool `json:"enable_context_summarization,omitempty"`
	EnableContextEditing       *bool `json:"enable_context_editing,omitempty"`

	// Image generation.
	EnableImageGeneration *bool  `json:"enable_image_generation,omitempty"`
	ImageGenProvider      string `json:"image_gen_provider,omitempty"`
	ImageGenModelID       string `json:"image_gen_model_id,omitempty"`

	// Tiered LLM allocation.
	LLMAllocationMode string           `json:"llm_allocation_mode,omitempty"` // "manual" (default) or "tiered"
	TieredConfig      *TieredLLMConfig `json:"tiered_config,omitempty"`
}

// TieredLLMConfig represents the 3-tier LLM configuration for tiered allocation mode.
type TieredLLMConfig struct {
	Tier1 *AgentLLMConfig `json:"tier_1"` // High reasoning
	Tier2 *AgentLLMConfig `json:"tier_2"` // Medium reasoning
	Tier3 *AgentLLMConfig `json:"tier_3"` // Low reasoning
}

// AgentLLMConfig represents LLM configuration for a specific agent type.
type AgentLLMConfig struct {
	Provider  string             `json:"provider"`
	ModelID   string             `json:"model_id"`
	Fallbacks []AgentLLMFallback `json:"fallbacks,omitempty"`
}

// AgentLLMFallback represents a fallback LLM model.
type AgentLLMFallback struct {
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
}

// WorkflowSelectedOption represents a selected option for a workflow phase.
type WorkflowSelectedOption struct {
	OptionID    string `json:"option_id"`
	OptionLabel string `json:"option_label"`
	OptionValue string `json:"option_value"`
	Group       string `json:"group"`
	PhaseID     string `json:"phase_id"`
}

// WorkflowSelectedOptions represents all selected options for a workflow phase.
type WorkflowSelectedOptions struct {
	PhaseID    string                   `json:"phase_id"`
	Selections []WorkflowSelectedOption `json:"selections"`
}

// ValidatePresetLLMConfigPublic accepts either legacy Provider+ModelID or at
// least one non-nil AgentLLMConfig with valid provider and model_id.
func ValidatePresetLLMConfigPublic(config *PresetLLMConfig) error {
	if config.TieredConfig != nil {
		tierConfigs := []struct {
			config *AgentLLMConfig
			name   string
		}{
			{config.TieredConfig.Tier1, "tier_1"},
			{config.TieredConfig.Tier2, "tier_2"},
			{config.TieredConfig.Tier3, "tier_3"},
		}
		for _, tierConfig := range tierConfigs {
			if tierConfig.config == nil {
				return fmt.Errorf("%s is required in tiered_config", tierConfig.name)
			}
			if tierConfig.config.ModelID == "" {
				return fmt.Errorf("model_id is required for %s", tierConfig.name)
			}
			if tierConfig.config.Provider == "" {
				return fmt.Errorf("provider is required for %s", tierConfig.name)
			}
			if _, err := llmproviders.ValidateProvider(tierConfig.config.Provider); err != nil {
				return fmt.Errorf("invalid provider for %s: %w", tierConfig.name, err)
			}
		}
		return nil
	}

	hasLegacyConfig := config.Provider != "" && config.ModelID != ""
	if hasLegacyConfig {
		if _, err := llmproviders.ValidateProvider(config.Provider); err != nil {
			return fmt.Errorf("invalid provider: %w", err)
		}
	}

	agentConfigs := []struct {
		config *AgentLLMConfig
		name   string
	}{
		{config.LearningLLM, "learning_llm"},
		{config.PhaseLLM, "phase_llm"},
	}
	hasValidAgentConfig := false
	for _, agentConfig := range agentConfigs {
		if agentConfig.config != nil {
			if agentConfig.config.ModelID == "" {
				return fmt.Errorf("model_id is required for %s", agentConfig.name)
			}
			if _, err := llmproviders.ValidateProvider(agentConfig.config.Provider); err != nil {
				return fmt.Errorf("invalid provider for %s: %w", agentConfig.name, err)
			}
			hasValidAgentConfig = true
		}
	}
	if !hasLegacyConfig && !hasValidAgentConfig {
		return fmt.Errorf("llm_config must have either legacy provider+model_id or at least one non-nil agent-specific config with valid provider and model_id")
	}
	return nil
}
