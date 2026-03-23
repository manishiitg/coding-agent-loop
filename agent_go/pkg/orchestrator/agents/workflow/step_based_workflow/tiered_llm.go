package step_based_workflow

import (
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
)

// workshopTierContextKey is used to pass tier overrides via context (concurrent-safe)
type workshopTierContextKey struct{}

// WorkshopTierOverrideKey is the context key for workshop execute_step tier override
var WorkshopTierOverrideKey = workshopTierContextKey{}

// TierLevel represents a tier in the tiered LLM allocation system
type TierLevel int

const (
	TierHigh   TierLevel = 1
	TierMedium TierLevel = 2
	TierLow    TierLevel = 3
)

// TierLevelLabel returns a human-readable label for a tier level
func TierLevelLabel(tier TierLevel) string {
	switch tier {
	case TierHigh:
		return "High"
	case TierMedium:
		return "Medium"
	case TierLow:
		return "Low"
	default:
		return "Unknown"
	}
}

// TieredLLMConfig represents the 3-tier LLM configuration for tiered allocation mode
type TieredLLMConfig struct {
	Tier1 *AgentLLMConfig `json:"tier_1"` // High reasoning
	Tier2 *AgentLLMConfig `json:"tier_2"` // Medium reasoning
	Tier3 *AgentLLMConfig `json:"tier_3"` // Low reasoning
}

// LearningMaturity represents the maturity level of learnings for a step
type LearningMaturity int

const (
	NoLearnings      LearningMaturity = 0 // No learning files exist
	HasLearnings     LearningMaturity = 1 // 1 learning file exists
	MatureLearnings  LearningMaturity = 2 // 2+ learning files exist
	LockedLearnings  LearningMaturity = 3 // Learnings are locked (skill is built, use lowest viable tier)
)

// TierResolver resolves the appropriate LLM tier based on agent type and learning maturity
type TierResolver struct {
	config  *TieredLLMConfig
	apiKeys *orchestrator.APIKeys
}

// NewTierResolver creates a new TierResolver
func NewTierResolver(config *TieredLLMConfig, apiKeys *orchestrator.APIKeys) *TierResolver {
	return &TierResolver{
		config:  config,
		apiKeys: apiKeys,
	}
}

// ResolveTier returns the LLMConfig for a specific tier level
func (tr *TierResolver) ResolveTier(tier TierLevel) *orchestrator.LLMConfig {
	var agentConfig *AgentLLMConfig
	switch tier {
	case TierHigh:
		agentConfig = tr.config.Tier1
	case TierMedium:
		agentConfig = tr.config.Tier2
	case TierLow:
		agentConfig = tr.config.Tier3
	default:
		agentConfig = tr.config.Tier1 // fallback to highest tier
	}

	if agentConfig == nil || agentConfig.Provider == "" || agentConfig.ModelID == "" {
		return nil
	}

	config := &orchestrator.LLMConfig{
		Primary: orchestrator.LLMModel{
			Provider: agentConfig.Provider,
			ModelID:  agentConfig.ModelID,
		},
		Fallbacks: convertAgentFallbacks(agentConfig.Fallbacks),
		APIKeys:   tr.apiKeys,
	}

	return config
}

// convertAgentFallbacks converts AgentLLMFallback slice to orchestrator.LLMModel slice.
func convertAgentFallbacks(fallbacks []AgentLLMFallback) []orchestrator.LLMModel {
	if len(fallbacks) == 0 {
		return nil
	}
	models := make([]orchestrator.LLMModel, len(fallbacks))
	for i, fb := range fallbacks {
		models[i] = orchestrator.LLMModel{
			Provider: fb.Provider,
			ModelID:  fb.ModelID,
		}
	}
	return models
}

// ResolveForExecution returns the LLM for execution agents based on learning maturity
// No Learnings: Tier 1 (High), Has Learnings (1 file): Tier 1 (High), Mature (2+ files): Tier 2 (Medium), Locked: Tier 3 (Low)
// Locked learnings means the skill is fully built — the agent follows a recipe and doesn't need high reasoning.
func (tr *TierResolver) ResolveForExecution(maturity LearningMaturity) (*orchestrator.LLMConfig, TierLevel) {
	switch maturity {
	case LockedLearnings:
		return tr.ResolveTier(TierLow), TierLow
	case MatureLearnings:
		return tr.ResolveTier(TierMedium), TierMedium
	default:
		return tr.ResolveTier(TierHigh), TierHigh
	}
}

// ResolveForLearning returns the LLM for learning agents based on learning maturity
// No Learnings: Tier 2 (Medium), Has Learnings: Tier 2 (Medium), Mature: Tier 3 (Low)
func (tr *TierResolver) ResolveForLearning(maturity LearningMaturity) (*orchestrator.LLMConfig, TierLevel) {
	switch maturity {
	case MatureLearnings:
		return tr.ResolveTier(TierLow), TierLow
	default:
		return tr.ResolveTier(TierMedium), TierMedium
	}
}

// ResolveForValidation returns the LLM for validation agents (always Tier 3)
func (tr *TierResolver) ResolveForValidation() (*orchestrator.LLMConfig, TierLevel) {
	return tr.ResolveTier(TierLow), TierLow
}

// Note: Phase agents use presetPhaseLLM which is independently configured (not part of tiered allocation).
// No resolver method is needed since phase agents don't have maturity-based selection.

// ResolveForConditional returns the LLM for conditional agents based on learning maturity
// No Learnings: Tier 1 (High), Has Learnings (1 file): Tier 1 (High), Mature (2+ files): Tier 2 (Medium), Locked: Tier 3 (Low)
func (tr *TierResolver) ResolveForConditional(maturity LearningMaturity) (*orchestrator.LLMConfig, TierLevel) {
	switch maturity {
	case LockedLearnings:
		return tr.ResolveTier(TierLow), TierLow
	case MatureLearnings:
		return tr.ResolveTier(TierMedium), TierMedium
	default:
		return tr.ResolveTier(TierHigh), TierHigh
	}
}

// GetTier1Config returns the Tier 1 AgentLLMConfig (for populating preset fields in backward-compatible way)
func (tr *TierResolver) GetTier1Config() *AgentLLMConfig {
	return tr.config.Tier1
}
