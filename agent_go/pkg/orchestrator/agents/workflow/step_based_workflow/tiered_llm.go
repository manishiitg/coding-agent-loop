package step_based_workflow

import (
	"strings"

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

// NormalizeTierOverride normalizes user/config-provided tier strings.
func NormalizeTierOverride(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "high", "medium", "low":
		return strings.ToLower(strings.TrimSpace(v))
	default:
		return ""
	}
}

// ParseTierOverride converts normalized tier strings into TierLevel values.
func ParseTierOverride(v string) (TierLevel, bool) {
	switch NormalizeTierOverride(v) {
	case "high":
		return TierHigh, true
	case "medium":
		return TierMedium, true
	case "low":
		return TierLow, true
	default:
		return TierHigh, false
	}
}

// TieredLLMConfig represents the 3-tier LLM configuration for tiered allocation mode
type TieredLLMConfig struct {
	Tier1 *AgentLLMConfig `json:"tier_1"` // High reasoning
	Tier2 *AgentLLMConfig `json:"tier_2"` // Medium reasoning
	Tier3 *AgentLLMConfig `json:"tier_3"` // Low reasoning
}

// TierResolver resolves the appropriate LLM tier based on agent type.
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
			Options:  agentConfig.Options,
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
			Options:  fb.Options,
		}
	}
	return models
}

// ResolveForExecution returns the default LLM tier for execution agents (Tier 1 / High).
// Explicit step-level overrides and dynamic tier selection (preferred_tier, workshop override)
// are applied by the caller before reaching this resolver.
func (tr *TierResolver) ResolveForExecution() (*orchestrator.LLMConfig, TierLevel) {
	return tr.ResolveTier(TierHigh), TierHigh
}

// ResolveForLearning returns the default LLM tier for learning agents (Tier 2 / Medium).
func (tr *TierResolver) ResolveForLearning() (*orchestrator.LLMConfig, TierLevel) {
	return tr.ResolveTier(TierMedium), TierMedium
}

// Note: Phase agents use presetPhaseLLM which is independently configured (not part of tiered allocation).

// ResolveForConditional returns the default LLM tier for conditional agents (Tier 1 / High).
func (tr *TierResolver) ResolveForConditional() (*orchestrator.LLMConfig, TierLevel) {
	return tr.ResolveTier(TierHigh), TierHigh
}

// GetTier1Config returns the Tier 1 AgentLLMConfig (for populating preset fields in backward-compatible way)
func (tr *TierResolver) GetTier1Config() *AgentLLMConfig {
	return tr.config.Tier1
}
