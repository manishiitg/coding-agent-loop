package minimax

import (
	"fmt"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// MiniMax model name constants
const (
	ModelMiniMaxM25          = "MiniMax-M2.5"
	ModelMiniMaxM25HighSpeed = "MiniMax-M2.5-highspeed"
	ModelMiniMaxM21          = "MiniMax-M2.1"
	ModelMiniMaxM21HighSpeed = "MiniMax-M2.1-highspeed"
	ModelMiniMaxM2           = "MiniMax-M2"
)

// getMiniMaxModels returns the map of MiniMax model metadata
func getMiniMaxModels() map[string]*llmtypes.ModelMetadata {
	return map[string]*llmtypes.ModelMetadata{
		ModelMiniMaxM25: {
			ModelID:                         ModelMiniMaxM25,
			ModelName:                       "MiniMax M2.5",
			ContextWindow:                   1000000,
			InputCostPer1MTokens:            0.30,
			OutputCostPer1MTokens:           1.20,
			CachedInputCostPer1MTokens:      0.03,
			CachedInputCostWritePer1MTokens: 0.375,
			Provider:                        "minimax",
		},
		ModelMiniMaxM25HighSpeed: {
			ModelID:                         ModelMiniMaxM25HighSpeed,
			ModelName:                       "MiniMax M2.5 High Speed",
			ContextWindow:                   1000000,
			InputCostPer1MTokens:            0.60,
			OutputCostPer1MTokens:           2.40,
			CachedInputCostPer1MTokens:      0.03,
			CachedInputCostWritePer1MTokens: 0.375,
			Provider:                        "minimax",
		},
		ModelMiniMaxM21: {
			ModelID:                         ModelMiniMaxM21,
			ModelName:                       "MiniMax M2.1",
			ContextWindow:                   1000000,
			InputCostPer1MTokens:            0.30,
			OutputCostPer1MTokens:           1.20,
			CachedInputCostPer1MTokens:      0.03,
			CachedInputCostWritePer1MTokens: 0.375,
			Provider:                        "minimax",
		},
		ModelMiniMaxM21HighSpeed: {
			ModelID:                         ModelMiniMaxM21HighSpeed,
			ModelName:                       "MiniMax M2.1 High Speed",
			ContextWindow:                   1000000,
			InputCostPer1MTokens:            0.60,
			OutputCostPer1MTokens:           2.40,
			CachedInputCostPer1MTokens:      0.03,
			CachedInputCostWritePer1MTokens: 0.375,
			Provider:                        "minimax",
		},
		ModelMiniMaxM2: {
			ModelID:                         ModelMiniMaxM2,
			ModelName:                       "MiniMax M2",
			ContextWindow:                   1000000,
			InputCostPer1MTokens:            0.30,
			OutputCostPer1MTokens:           1.20,
			CachedInputCostPer1MTokens:      0.03,
			CachedInputCostWritePer1MTokens: 0.375,
			Provider:                        "minimax",
		},
	}
}

// GetAllMiniMaxModels returns a list of all available MiniMax models in display order
func GetAllMiniMaxModels() []*llmtypes.ModelMetadata {
	models := getMiniMaxModels()
	order := []string{
		ModelMiniMaxM25,
		ModelMiniMaxM25HighSpeed,
		ModelMiniMaxM21,
		ModelMiniMaxM21HighSpeed,
		ModelMiniMaxM2,
	}
	result := make([]*llmtypes.ModelMetadata, 0, len(order))
	for _, id := range order {
		if m, ok := models[id]; ok {
			result = append(result, m)
		}
	}
	return result
}

// GetMiniMaxModelMetadata returns metadata for a MiniMax model
func GetMiniMaxModelMetadata(modelID string) (*llmtypes.ModelMetadata, error) {
	models := getMiniMaxModels()
	metadata, exists := models[modelID]
	if !exists {
		return nil, fmt.Errorf("unknown MiniMax model: %s", modelID)
	}
	result := *metadata
	return &result, nil
}
