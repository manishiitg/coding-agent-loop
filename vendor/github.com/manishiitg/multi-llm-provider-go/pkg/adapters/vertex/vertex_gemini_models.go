package vertex

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Gemini model name constants
const (
	// Gemini 2.5 Series
	ModelGemini25Pro       = "gemini-2.5-pro"
	ModelGemini25Flash     = "gemini-2.5-flash"
	ModelGemini25FlashLite = "gemini-2.5-flash-lite"

	// Gemini 3 Series
	ModelGemini3ProPreview       = "gemini-3-pro-preview"
	ModelGemini31ProPreview      = "gemini-3.1-pro-preview"
	ModelGemini3FlashPreview     = "gemini-3-flash-preview"
	ModelGemini31FlashLitePreview = "gemini-3.1-flash-lite-preview"
)

// normalizeToBaseModel normalizes Gemini model IDs to base model names
// Strips numeric version suffixes (-\d{3}) and -exp suffix
// Examples:
//   - "gemini-3-pro-preview" -> "gemini-3-pro-preview" (keep preview as it's part of base name)
//   - "gemini-3-flash-preview" -> "gemini-3-flash-preview" (keep preview as it's part of base name)
//   - "gemini-1.5-pro-001" -> "gemini-1.5-pro"
//   - "gemini-2.5-flash-exp" -> "gemini-2.5-flash"
func normalizeToBaseModel(modelID string) string {
	// Remove version suffixes like "-001", "-002", etc.
	versionPattern := regexp.MustCompile(`-\d{3}$`)
	baseModelID := versionPattern.ReplaceAllString(modelID, "")

	// Remove experimental suffixes like "-exp" (but keep "-preview" as it's part of model name)
	baseModelID = strings.TrimSuffix(baseModelID, "-exp")

	return baseModelID
}

// getVertexGeminiModels returns the map of Vertex Gemini model metadata
func getVertexGeminiModels() map[string]llmtypes.ModelMetadata {
	return map[string]llmtypes.ModelMetadata{
		// Gemini 2.5 Pro - 1M context window
		ModelGemini25Pro: {
			ModelID:                    ModelGemini25Pro,
			ModelName:                  "Gemini 2.5 Pro",
			ContextWindow:              1000000, // 1M tokens
			InputCostPer1MTokens:       1.25,
			OutputCostPer1MTokens:      10.00,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.3125, // Cache read pricing (for prompts ≤200k tokens)
			Provider:                   "vertex",
			// Capabilities
			SupportsToolCalls:       true,
			SupportsJSONMode:        true,
			SupportsThinkingLevel:   false,
			ThinkingLevels:          nil,
			SupportsReasoningEffort: false,
			SupportsThinkingBudget:  true,
		},
		// Gemini 2.5 Flash - 1M context window
		ModelGemini25Flash: {
			ModelID:                    ModelGemini25Flash,
			ModelName:                  "Gemini 2.5 Flash",
			ContextWindow:              1000000, // 1M tokens
			InputCostPer1MTokens:       0.30,
			OutputCostPer1MTokens:      2.50,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.03, // Cache read pricing (estimated, 90% discount)
			Provider:                   "vertex",
			// Capabilities
			SupportsToolCalls:       true,
			SupportsJSONMode:        true,
			SupportsThinkingLevel:   false,
			ThinkingLevels:          nil,
			SupportsReasoningEffort: false,
		},
		// Gemini 2.5 Flash-Lite - 1M context window
		ModelGemini25FlashLite: {
			ModelID:                    ModelGemini25FlashLite,
			ModelName:                  "Gemini 2.5 Flash-Lite",
			ContextWindow:              1000000, // 1M tokens
			InputCostPer1MTokens:       0.10,
			OutputCostPer1MTokens:      0.40,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.01, // Cache read pricing (estimated, 90% discount)
			Provider:                   "vertex",
			// Capabilities
			SupportsToolCalls:       true,
			SupportsJSONMode:        true,
			SupportsThinkingLevel:   false,
			ThinkingLevels:          nil,
			SupportsReasoningEffort: false,
		},
		// Gemini 3 Pro Preview - 1M context window
		ModelGemini3ProPreview: {
			ModelID:                    ModelGemini3ProPreview,
			ModelName:                  "Gemini 3 Pro Preview",
			ContextWindow:              1000000, // 1M tokens
			InputCostPer1MTokens:       2.00,    // For prompts ≤200k tokens
			OutputCostPer1MTokens:      12.00,   // For prompts ≤200k tokens
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.20, // Cache read pricing (for prompts ≤200k tokens)
			Provider:                   "vertex",
			// Capabilities
			SupportsToolCalls:       true,
			SupportsJSONMode:        true,
			SupportsThinkingLevel:   true,
			ThinkingLevels:          []string{"low", "high"},
			SupportsReasoningEffort: false,
		},
		// Gemini 3.1 Pro Preview - 1M context window
		ModelGemini31ProPreview: {
			ModelID:                    ModelGemini31ProPreview,
			ModelName:                  "Gemini 3.1 Pro Preview",
			ContextWindow:              1000000, // 1M tokens
			InputCostPer1MTokens:       2.00,    // Keep aligned with Gemini 3 Pro Preview until official pricing update
			OutputCostPer1MTokens:      12.00,   // Keep aligned with Gemini 3 Pro Preview until official pricing update
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.20, // Keep aligned with Gemini 3 Pro Preview until official pricing update
			Provider:                   "vertex",
			// Capabilities
			SupportsToolCalls:       true,
			SupportsJSONMode:        true,
			SupportsThinkingLevel:   true,
			ThinkingLevels:          []string{"low", "medium", "high"},
			SupportsReasoningEffort: false,
		},
		// Gemini 3.1 Flash-Lite Preview - 1M context window (launched March 2026)
		ModelGemini31FlashLitePreview: {
			ModelID:                    ModelGemini31FlashLitePreview,
			ModelName:                  "Gemini 3.1 Flash-Lite Preview",
			ContextWindow:              1000000, // 1M tokens
			InputCostPer1MTokens:       0.25,
			OutputCostPer1MTokens:      1.50,
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.025, // Cache read pricing (estimated, 90% discount)
			Provider:                   "vertex",
			// Capabilities
			SupportsToolCalls:       true,
			SupportsJSONMode:        true,
			SupportsThinkingLevel:   false,
			ThinkingLevels:          nil,
			SupportsReasoningEffort: false,
		},
		// Gemini 3 Flash Preview - 1M context window
		ModelGemini3FlashPreview: {
			ModelID:                    ModelGemini3FlashPreview,
			ModelName:                  "Gemini 3 Flash Preview",
			ContextWindow:              1000000, // 1M tokens
			InputCostPer1MTokens:       0.50,    // Pricing from https://blog.google/products/gemini/gemini-3-flash/
			OutputCostPer1MTokens:      3.00,    // Pricing from https://blog.google/products/gemini/gemini-3-flash/
			ReasoningCostPer1MTokens:   0.0,
			CachedInputCostPer1MTokens: 0.05, // Cache read pricing (estimated, 90% discount)
			Provider:                   "vertex",
			// Capabilities
			SupportsToolCalls:       true,
			SupportsJSONMode:        true,
			SupportsThinkingLevel:   false, // Not documented for Flash; reserved for Pro
			ThinkingLevels:          nil,
			SupportsReasoningEffort: false,
		},
	}
}

// GetAllVertexGeminiModels returns a list of all available Vertex Gemini models
func GetAllVertexGeminiModels() []*llmtypes.ModelMetadata {
	models := getVertexGeminiModels()
	result := make([]*llmtypes.ModelMetadata, 0, len(models))
	for _, m := range models {
		// Make a copy to avoid referencing loop variable
		metadata := m
		result = append(result, &metadata)
	}
	return result
}

// GetVertexGeminiModelMetadata returns model metadata for a given Gemini model ID
func GetVertexGeminiModelMetadata(modelID string) (*llmtypes.ModelMetadata, error) {
	baseModelID := normalizeToBaseModel(modelID)

	models := getVertexGeminiModels()

	metadata, exists := models[baseModelID]
	if !exists {
		return nil, fmt.Errorf("unknown Gemini model: %s (normalized from: %s)", baseModelID, modelID)
	}

	// Preserve the original modelID (which may include version suffixes) for consistency
	// with OpenAI/Anthropic adapters
	metadata.ModelID = modelID

	return &metadata, nil
}
