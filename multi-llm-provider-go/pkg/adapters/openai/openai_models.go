package openai

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// OpenAI model name constants
const (
	ModelGPT4o     = "gpt-4o"
	ModelGPT4oMini = "gpt-4o-mini"
	ModelO1Pro     = "o1-pro"
	ModelO3Mini        = "o3-mini"
	ModelO3Pro         = "o3-pro"
	ModelO4Mini        = "o4-mini"
	ModelGPT5          = "gpt-5"
	ModelGPT5Mini      = "gpt-5-mini"
	ModelGPT5Nano      = "gpt-5-nano"
	ModelGPT5Pro       = "gpt-5-pro"
	ModelGPT51         = "gpt-5.1"
	ModelGPT51Mini     = "gpt-5.1-mini"
	ModelGPT51Nano     = "gpt-5.1-nano"
	// GPT-5.2 family: base and Pro (plus specialized Instant/Thinking variants)
	ModelGPT52    = "gpt-5.2"
	ModelGPT52Pro = "gpt-5.2-pro"
	// GPT-5.2 specialized variants
	ModelGPT52Instant  = "gpt-5.2-instant"
	ModelGPT52Thinking = "gpt-5.2-thinking"
	ModelGPT41         = "gpt-4.1"
	ModelGPT41Mini     = "gpt-4.1-mini"
	ModelGPT41Nano     = "gpt-4.1-nano"
)

// getOpenAIModels returns the map of OpenAI model metadata
func getOpenAIModels() map[string]*llmtypes.ModelMetadata {
	return map[string]*llmtypes.ModelMetadata{
		ModelGPT4o: {
			ModelID:                    ModelGPT4o,
			ModelName:                  "GPT-4o",
			ContextWindow:              128000, // 128k tokens
			InputCostPer1MTokens:       2.50,
			OutputCostPer1MTokens:      10.00,
			ReasoningCostPer1MTokens:   0.0,   // No separate reasoning tokens
			CachedInputCostPer1MTokens: 0.125, // 95% discount
			Provider:                   "openai",
		},
		ModelGPT4oMini: {
			ModelID:                    ModelGPT4oMini,
			ModelName:                  "GPT-4o Mini",
			ContextWindow:              128000, // 128k tokens
			InputCostPer1MTokens:       0.15,
			OutputCostPer1MTokens:      0.60,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.075, // 50% discount
			Provider:                   "openai",
		},
		ModelO1Pro: {
			ModelID:                    ModelO1Pro,
			ModelName:                  "O1 Pro",
			ContextWindow:              200000, // 200k tokens
			InputCostPer1MTokens:       150.00,
			OutputCostPer1MTokens:      600.00,
			ReasoningCostPer1MTokens:   30.00, // Reasoning tokens are charged separately (estimated ~5% of output cost)
			CachedInputCostPer1MTokens: 7.50,  // 95% discount
			Provider:                   "openai",
		},
		ModelO3Mini: {
			ModelID:                    ModelO3Mini,
			ModelName:                  "O3 Mini",
			ContextWindow:              200000, // 200k tokens
			InputCostPer1MTokens:       3.00,
			OutputCostPer1MTokens:      12.00,
			ReasoningCostPer1MTokens:   0.60, // Reasoning tokens are charged separately
			CachedInputCostPer1MTokens: 0.15, // 95% discount
			Provider:                   "openai",
		},
		ModelO3Pro: {
			ModelID:                    ModelO3Pro,
			ModelName:                  "O3 Pro",
			ContextWindow:              200000, // 200k tokens (estimated, same as o3-mini)
			InputCostPer1MTokens:       20.00,
			OutputCostPer1MTokens:      80.00,
			ReasoningCostPer1MTokens:   4.00, // Reasoning tokens are charged separately (estimated ~5% of output cost)
			CachedInputCostPer1MTokens: 1.00, // 95% discount
			Provider:                   "openai",
		},
		ModelO4Mini: {
			ModelID:                    ModelO4Mini,
			ModelName:                  "O4 Mini",
			ContextWindow:              200000, // 200k tokens
			InputCostPer1MTokens:       1.10,
			OutputCostPer1MTokens:      4.40,
			ReasoningCostPer1MTokens:   0.22,  // Reasoning tokens are charged separately (estimated ~20% of output cost)
			CachedInputCostPer1MTokens: 0.055, // 95% discount
			Provider:                   "openai",
		},
		ModelGPT5: {
			ModelID:                    ModelGPT5,
			ModelName:                  "GPT-5",
			ContextWindow:              400000, // 400k tokens
			InputCostPer1MTokens:       1.25,
			OutputCostPer1MTokens:      10.00,
			ReasoningCostPer1MTokens:   0.0,   // Check if reasoning tokens apply
			CachedInputCostPer1MTokens: 0.125, // 90% discount
			Provider:                   "openai",
		},
		ModelGPT5Mini: {
			ModelID:                    ModelGPT5Mini,
			ModelName:                  "GPT-5 Mini",
			ContextWindow:              400000, // 400k tokens
			InputCostPer1MTokens:       0.25,
			OutputCostPer1MTokens:      2.00,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.025, // 90% discount
			Provider:                   "openai",
		},
		ModelGPT5Nano: {
			ModelID:                    ModelGPT5Nano,
			ModelName:                  "GPT-5 Nano",
			ContextWindow:              400000, // 400k tokens
			InputCostPer1MTokens:       0.05,
			OutputCostPer1MTokens:      0.40,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.005, // 90% discount
			Provider:                   "openai",
		},
		ModelGPT5Pro: {
			ModelID:                    ModelGPT5Pro,
			ModelName:                  "GPT-5 Pro",
			ContextWindow:              400000, // 400k tokens
			InputCostPer1MTokens:       15.00,
			OutputCostPer1MTokens:      120.00,
			ReasoningCostPer1MTokens:   6.00, // Reasoning tokens for high reasoning effort (estimated ~5% of output cost)
			CachedInputCostPer1MTokens: 0.75, // 95% discount
			Provider:                   "openai",
		},
		ModelGPT51: {
			ModelID:                    ModelGPT51,
			ModelName:                  "GPT-5.1",
			ContextWindow:              400000, // 400k tokens
			InputCostPer1MTokens:       1.25,
			OutputCostPer1MTokens:      10.00,
			ReasoningCostPer1MTokens:   1.00,  // Reasoning tokens for reasoning effort
			CachedInputCostPer1MTokens: 0.125, // 90% discount
			Provider:                   "openai",
		},
		ModelGPT51Mini: {
			ModelID:                    ModelGPT51Mini,
			ModelName:                  "GPT-5.1 Mini",
			ContextWindow:              400000, // 400k tokens
			InputCostPer1MTokens:       0.25,
			OutputCostPer1MTokens:      2.00,
			ReasoningCostPer1MTokens:   0.20,  // Reasoning tokens (estimated ~20% of base)
			CachedInputCostPer1MTokens: 0.025, // 90% discount
			Provider:                   "openai",
		},
		ModelGPT51Nano: {
			ModelID:                    ModelGPT51Nano,
			ModelName:                  "GPT-5.1 Nano",
			ContextWindow:              400000, // 400k tokens
			InputCostPer1MTokens:       0.05,
			OutputCostPer1MTokens:      0.40,
			ReasoningCostPer1MTokens:   0.05,  // Reasoning tokens (estimated ~5% of base)
			CachedInputCostPer1MTokens: 0.005, // 90% discount
			Provider:                   "openai",
		},
		// GPT-5.2 base (same pricing as Instant/Thinking family; standard capabilities)
		ModelGPT52: {
			ModelID:                    ModelGPT52,
			ModelName:                  "GPT-5.2",
			ContextWindow:              400000, // 400k tokens (input context)
			InputCostPer1MTokens:       1.75,
			OutputCostPer1MTokens:      14.00,
			ReasoningCostPer1MTokens:   0.0,   // No separate reasoning tokens documented yet
			CachedInputCostPer1MTokens: 0.175, // 90% discount
			Provider:                   "openai",
			// Capabilities (conservative defaults; refine as OpenAI docs evolve)
			SupportsToolCalls:     true,
			SupportsJSONMode:      true,
			SupportsThinkingLevel: false,
		},
		// GPT-5.2 Pro - higher pricing, reasoning-focused
		ModelGPT52Pro: {
			ModelID:                    ModelGPT52Pro,
			ModelName:                  "GPT-5.2 Pro",
			ContextWindow:              400000, // 400k tokens (input context)
			InputCostPer1MTokens:       21.00,
			OutputCostPer1MTokens:      168.00,
			ReasoningCostPer1MTokens:   8.40, // Reasoning tokens (estimated ~5% of output cost)
			CachedInputCostPer1MTokens: 1.05, // 95% discount
			Provider:                   "openai",
			// Capabilities
			SupportsToolCalls:       true,
			SupportsJSONMode:        true,
			SupportsReasoningEffort: true,
			ReasoningEffortLevels:   []string{"low", "medium", "high"},
		},
		// GPT-5.2 Instant - fast, same pricing as base, no explicit reasoning-effort knob
		ModelGPT52Instant: {
			ModelID:                    ModelGPT52Instant,
			ModelName:                  "GPT-5.2 Instant",
			ContextWindow:              400000, // 400k tokens (input context)
			InputCostPer1MTokens:       1.75,
			OutputCostPer1MTokens:      14.00,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.175, // 90% discount
			Provider:                   "openai",
			// Capabilities
			SupportsToolCalls:       true,
			SupportsJSONMode:        true,
			SupportsReasoningEffort: false,
		},
		// GPT-5.2 Thinking - deeper reasoning, same pricing as base, supports reasoning-effort knob
		ModelGPT52Thinking: {
			ModelID:                    ModelGPT52Thinking,
			ModelName:                  "GPT-5.2 Thinking",
			ContextWindow:              400000, // 400k tokens (input context)
			InputCostPer1MTokens:       1.75,
			OutputCostPer1MTokens:      14.00,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.175, // 90% discount
			Provider:                   "openai",
			// Capabilities
			SupportsToolCalls:       true,
			SupportsJSONMode:        true,
			SupportsReasoningEffort: true,
			ReasoningEffortLevels:   []string{"low", "medium", "high"},
		},
		ModelGPT41: {
			ModelID:                    ModelGPT41,
			ModelName:                  "GPT-4.1",
			ContextWindow:              1047576, // ~1M tokens (1,047,576 tokens)
			InputCostPer1MTokens:       2.00,
			OutputCostPer1MTokens:      8.00,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.50, // 75% discount
			Provider:                   "openai",
		},
		ModelGPT41Mini: {
			ModelID:                    ModelGPT41Mini,
			ModelName:                  "GPT-4.1 Mini",
			ContextWindow:              1047576, // ~1M tokens (1,047,576 tokens)
			InputCostPer1MTokens:       0.40,
			OutputCostPer1MTokens:      1.60,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.10, // 75% discount
			Provider:                   "openai",
		},
		ModelGPT41Nano: {
			ModelID:                    ModelGPT41Nano,
			ModelName:                  "GPT-4.1 Nano",
			ContextWindow:              1047576, // ~1M tokens (1,047,576 tokens)
			InputCostPer1MTokens:       0.10,
			OutputCostPer1MTokens:      0.40,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.025, // 75% discount
			Provider:                   "openai",
		},
	}
}

// GetAllOpenAIModels returns a list of all available OpenAI models
func GetAllOpenAIModels() []*llmtypes.ModelMetadata {
	models := getOpenAIModels()
	result := make([]*llmtypes.ModelMetadata, 0, len(models))
	for _, m := range models {
		result = append(result, m)
	}
	return result
}

// GetOpenAIModelMetadata returns metadata for OpenAI models including token limits and pricing
// Pricing includes input, output, reasoning (for reasoning models), and cached input tokens
// Accepts both base model names (e.g., "gpt-4o") and versioned names (e.g., "gpt-4o-2024-11-20")
// Versioned names are normalized to base model names
func GetOpenAIModelMetadata(modelID string) (*llmtypes.ModelMetadata, error) {
	// Normalize model ID to base model name (remove version suffixes)
	baseModelID := normalizeToBaseModel(modelID)

	// OpenAI model metadata with comprehensive pricing
	// Only base model names are stored - versioned names are normalized to base names
	models := getOpenAIModels()

	// Look up by base model name (versioned names are normalized)
	if metadata, exists := models[baseModelID]; exists {
		// Create a copy with the original modelID (preserve versioned name if provided)
		result := *metadata
		result.ModelID = modelID // Keep original modelID (may be versioned)
		return &result, nil
	}

	return nil, fmt.Errorf("model metadata not found for OpenAI model: %s (normalized: %s)", modelID, baseModelID)
}

// normalizeToBaseModel normalizes model IDs to base model names by removing version suffixes
// Examples:
//   - "gpt-4o-2024-08-06" -> "gpt-4o"
//   - "gpt-4o-2024-11-20" -> "gpt-4o"
//   - "gpt-4-turbo-preview" -> "gpt-4-turbo"
//   - "gpt-4o" -> "gpt-4o" (no change)
func normalizeToBaseModel(modelID string) string {
	normalized := strings.ToLower(strings.TrimSpace(modelID))

	// Remove version date patterns (YYYY-MM-DD) using regex to match trailing date suffix
	// e.g., "gpt-4o-2024-08-06" -> "gpt-4o"
	// The regex matches "-YYYY-MM-DD" at the end of the string
	datePattern := regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)
	normalized = datePattern.ReplaceAllString(normalized, "")

	// Remove common suffixes like "-preview"
	normalized = strings.TrimSuffix(normalized, "-preview")

	return normalized
}
