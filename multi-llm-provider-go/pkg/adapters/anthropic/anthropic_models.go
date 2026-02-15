package anthropic

import (
	"fmt"
	"regexp"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Anthropic model name constants
const (
	// Claude 4.5 Series
	ModelClaudeSonnet45 = "claude-sonnet-4-5"
	ModelClaudeHaiku45  = "claude-haiku-4-5"
	ModelClaudeOpus45   = "claude-opus-4-5"
)

// normalizeToBaseModel normalizes Anthropic model IDs to base model names
// Anthropic models use date suffixes like "-20241022" or "-20250219"
// Examples:
//   - "claude-3-5-sonnet-20241022" -> "claude-3-5-sonnet"
//   - "claude-sonnet-4-5-20250929" -> "claude-sonnet-4-5"
//   - "claude-3-7-sonnet-20250219" -> "claude-3-7-sonnet"
func normalizeToBaseModel(modelID string) string {
	// Remove date suffixes (format: -YYYYMMDD)
	datePattern := regexp.MustCompile(`-\d{8}$`)
	baseModelID := datePattern.ReplaceAllString(modelID, "")

	// Also handle version suffixes like "-v1:0" (used in Bedrock)
	versionPattern := regexp.MustCompile(`-v\d+:\d+$`)
	baseModelID = versionPattern.ReplaceAllString(baseModelID, "")

	return baseModelID
}

// getAnthropicModels returns the map of Anthropic model metadata
func getAnthropicModels() map[string]*llmtypes.ModelMetadata {
	return map[string]*llmtypes.ModelMetadata{
		// Claude 4.5 Series
		ModelClaudeSonnet45: {
			ModelID:                         ModelClaudeSonnet45,
			ModelName:                       "Claude Sonnet 4.5",
			ContextWindow:                   200000, // 200k tokens
			InputCostPer1MTokens:            3.00,
			OutputCostPer1MTokens:           15.00,
			ReasoningCostPer1MTokens:        0.0,
			CachedInputCostPer1MTokens:      0.30, // Cache read pricing (for ≤200K tokens)
			CachedInputCostWritePer1MTokens: 3.75, // Cache write pricing (1.25x base input: 3.00 * 1.25)
			Provider:                        "anthropic",
		},
		ModelClaudeHaiku45: {
			ModelID:                         ModelClaudeHaiku45,
			ModelName:                       "Claude Haiku 4.5",
			ContextWindow:                   200000, // 200k tokens
			InputCostPer1MTokens:            1.00,
			OutputCostPer1MTokens:           5.00,
			ReasoningCostPer1MTokens:        0.0,
			CachedInputCostPer1MTokens:      0.10, // Cache read pricing
			CachedInputCostWritePer1MTokens: 1.25, // Cache write pricing (1.25x base input: 1.00 * 1.25)
			Provider:                        "anthropic",
		},
		ModelClaudeOpus45: {
			ModelID:                         ModelClaudeOpus45,
			ModelName:                       "Claude Opus 4.5",
			ContextWindow:                   200000, // 200k tokens
			InputCostPer1MTokens:            5.00,
			OutputCostPer1MTokens:           25.00,
			ReasoningCostPer1MTokens:        0.0,
			CachedInputCostPer1MTokens:      0.50, // Cache read pricing
			CachedInputCostWritePer1MTokens: 6.25, // Cache write pricing (1.25x base input: 5.00 * 1.25)
			Provider:                        "anthropic",
		},
	}
}

// GetAllAnthropicModels returns a list of all available Anthropic models
func GetAllAnthropicModels() []*llmtypes.ModelMetadata {
	models := getAnthropicModels()
	result := make([]*llmtypes.ModelMetadata, 0, len(models))
	for _, m := range models {
		result = append(result, m)
	}
	return result
}

// GetAnthropicModelMetadata returns metadata for Anthropic models including token limits and pricing
// Pricing includes input, output, cached input read tokens, and cached input write tokens
// Cache read cost: 10% of base input cost
// Cache write cost: 125% of base input cost (25% more than base)
// Accepts both base model names (e.g., "claude-3-5-sonnet") and versioned names (e.g., "claude-3-5-sonnet-20241022")
// Versioned names are normalized to base model names
func GetAnthropicModelMetadata(modelID string) (*llmtypes.ModelMetadata, error) {
	// Normalize model ID to base model name (remove date/version suffixes)
	baseModelID := normalizeToBaseModel(modelID)

	// Anthropic model metadata with comprehensive pricing
	// Only base model names are stored - versioned names are normalized to base names
	models := getAnthropicModels()

	// Look up the model by base model ID
	metadata, exists := models[baseModelID]
	if !exists {
		return nil, fmt.Errorf("unknown Anthropic model: %s (normalized from: %s)", baseModelID, modelID)
	}

	// Return a copy with the original model ID preserved (in case caller needs it)
	result := *metadata
	result.ModelID = modelID // Preserve original model ID if versioned
	return &result, nil
}
