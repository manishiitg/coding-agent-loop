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
	WorkflowStatusWorkflowBuilder  = "workflow-builder"
	WorkflowStatusEvalBuilder      = "evaluation-builder"
)

const (
	LLMAllocationModeManual           = "manual"
	LLMAllocationModeTiered           = "tiered"
	LLMAllocationModeCodingAgent      = "coding_agent"
	LLMAllocationModeCodingPlanLegacy = "coding_plan"
)

// Knowledgebase shape — only one shape supported: notes-only (per-topic
// markdown files under knowledgebase/notes/ plus notes/_index.json). The
// legacy graph+notes shape has been removed. KBShapeGraphNotes is kept as a
// recognized-but-deprecated value so existing workflow configs don't fail to
// parse; everything resolves to KBShapeNotesOnly at runtime.
const (
	// KBShapeNotesOnly — per-topic markdown files + notes/_index.json registry.
	// The only supported shape.
	KBShapeNotesOnly = "notes-only"
	// KBShapeGraphNotes is retained as a recognized legacy value for config
	// compatibility. ResolveKBShape collapses it to KBShapeNotesOnly.
	KBShapeGraphNotes = "graph+notes"
)

// ValidKBShape reports whether s is a recognized kb_shape value. Empty is valid
// (resolves to notes-only). Legacy "graph+notes" is accepted but collapsed to
// notes-only at runtime.
func ValidKBShape(s string) bool {
	switch s {
	case "", KBShapeGraphNotes, KBShapeNotesOnly:
		return true
	}
	return false
}

// ResolveKBShape always returns KBShapeNotesOnly. The graph surface has been
// removed; empty and legacy "graph+notes" values both resolve here for
// backward compatibility with configs on disk, but the runtime behavior is
// always notes-only.
func ResolveKBShape(s string) string {
	_ = s
	return KBShapeNotesOnly
}

// PresetLLMConfig represents LLM configuration stored with workflow presets.
type PresetLLMConfig struct {
	// Legacy single-model form (backward compat).
	Provider string `json:"provider,omitempty"`
	ModelID  string `json:"model_id,omitempty"`

	// Agent-specific defaults — take priority over the legacy single-model form.
	LearningLLM *AgentLLMConfig `json:"learning_llm,omitempty"`
	PhaseLLM    *AgentLLMConfig `json:"phase_llm,omitempty"`

	// Feature toggles.
	UseKnowledgebase           *bool  `json:"use_knowledgebase,omitempty"`
	LockKnowledgebase          *bool  `json:"lock_knowledgebase,omitempty"`
	KBShape                    string `json:"kb_shape,omitempty"` // "graph+notes" (default) | "notes-only"
	EnableContextSummarization *bool  `json:"enable_context_summarization,omitempty"`
	EnableContextEditing       *bool  `json:"enable_context_editing,omitempty"`

	// Image generation.
	EnableImageGeneration *bool  `json:"enable_image_generation,omitempty"`
	ImageGenProvider      string `json:"image_gen_provider,omitempty"`
	ImageGenModelID       string `json:"image_gen_model_id,omitempty"`

	// Tiered LLM allocation.
	// Supported values:
	//   - "manual" (default): use explicitly stored provider/model defaults
	//   - "tiered": use explicitly stored tiered_config
	//   - "coding_agent": resolve provider/model_id dynamically through multi-llm-provider-go
	LLMAllocationMode string           `json:"llm_allocation_mode,omitempty"`
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
	PublishedLLMID string                 `json:"published_llm_id,omitempty"`
	Provider       string                 `json:"provider"`
	ModelID        string                 `json:"model_id"`
	Options        map[string]interface{} `json:"options,omitempty"`
	Fallbacks      []AgentLLMFallback     `json:"fallbacks,omitempty"`
}

// AgentLLMFallback represents a fallback LLM model.
type AgentLLMFallback struct {
	PublishedLLMID string                 `json:"published_llm_id,omitempty"`
	Provider       string                 `json:"provider"`
	ModelID        string                 `json:"model_id"`
	Options        map[string]interface{} `json:"options,omitempty"`
}

func agentLLMConfigFromCodingAgentRef(ref llmproviders.CodingAgentTierModelRef) *AgentLLMConfig {
	if ref.Provider == "" || ref.ModelID == "" {
		return nil
	}
	return &AgentLLMConfig{
		Provider: ref.Provider,
		ModelID:  ref.ModelID,
		Options:  ref.Options,
	}
}

func isCodingAgentAllocationMode(mode string) bool {
	return mode == LLMAllocationModeCodingAgent || mode == LLMAllocationModeCodingPlanLegacy
}

// ResolveCodingAgentConfig expands a coding-agent preset into current provider
// package defaults. It intentionally does not persist the resolved models.
func ResolveCodingAgentConfig(config *PresetLLMConfig) (*AgentLLMConfig, *TieredLLMConfig, bool) {
	if config == nil || !isCodingAgentAllocationMode(config.LLMAllocationMode) || config.Provider == "" {
		return nil, nil, false
	}

	defaults, ok := llmproviders.GetCodingAgentDefaultTierModels(llmproviders.Provider(config.Provider))
	if !ok {
		return nil, nil, false
	}

	phase := agentLLMConfigFromCodingAgentRef(defaults.Phase)
	tiered := &TieredLLMConfig{
		Tier1: agentLLMConfigFromCodingAgentRef(defaults.High),
		Tier2: agentLLMConfigFromCodingAgentRef(defaults.Medium),
		Tier3: agentLLMConfigFromCodingAgentRef(defaults.Low),
	}

	if tiered.Tier1 == nil || tiered.Tier2 == nil || tiered.Tier3 == nil {
		return phase, nil, true
	}
	return phase, tiered, true
}

// ResolveCodingPlanConfig is kept for legacy call sites and old saved configs.
func ResolveCodingPlanConfig(config *PresetLLMConfig) (*AgentLLMConfig, *TieredLLMConfig, bool) {
	return ResolveCodingAgentConfig(config)
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
	if isCodingAgentAllocationMode(config.LLMAllocationMode) {
		if config.Provider == "" {
			return fmt.Errorf("provider is required for coding_agent llm_config")
		}
		if _, ok := llmproviders.GetCodingAgentDefaultTierModels(llmproviders.Provider(config.Provider)); !ok {
			return fmt.Errorf("provider %q does not expose coding agent tier defaults", config.Provider)
		}
		return nil
	}

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
