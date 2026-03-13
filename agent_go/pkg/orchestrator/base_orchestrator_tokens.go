package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/anthropic"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/azure"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/bedrock"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
	"github.com/manishiitg/multi-llm-provider-go/pkg/adapters/vertex"
)

// tokenFileMutex ensures thread-safe access to token_usage.json
// Prevents race conditions when multiple conversations/steps complete concurrently
// Note: TokenUsageEvent is emitted once per conversation (at end) with cumulative totals,
// so there's no duplicate counting, but file writes still need protection from concurrent access
var tokenFileMutex sync.Mutex

// formatTokensM formats raw token count as string with "M" suffix (e.g., "17.016M")
func formatTokensM(tokens int) string {
	if tokens == 0 {
		return "0.000M"
	}
	millions := float64(tokens) / 1_000_000.0
	return fmt.Sprintf("%.3fM", millions)
}

// calculateCostFromTokens calculates the cost for tokens based on model metadata
// Returns cost in USD
func calculateCostFromTokens(tokenCount int, costPer1MTokens float64) float64 {
	if tokenCount <= 0 || costPer1MTokens <= 0 {
		return 0.0
	}
	// Convert from cost per 1M tokens to cost for this token count
	return (float64(tokenCount) / 1_000_000.0) * costPer1MTokens
}

// getModelMetadata retrieves model metadata based on provider and modelID
func getModelMetadata(provider, modelID string) (*llmtypes.ModelMetadata, error) {
	switch strings.ToLower(provider) {
	case "openai", "openrouter":
		// Check if it's an OpenRouter model (contains "/")
		if strings.Contains(modelID, "/") {
			return openai.GetOpenRouterModelMetadata(modelID)
		}
		return openai.GetOpenAIModelMetadata(modelID)
	case "anthropic":
		return anthropic.GetAnthropicModelMetadata(modelID)
	case "vertex":
		// Vertex supports both Gemini and Anthropic models
		if strings.Contains(modelID, "claude") || strings.Contains(modelID, "anthropic") {
			return anthropic.GetAnthropicModelMetadata(modelID)
		}
		return vertex.GetVertexGeminiModelMetadata(modelID)
	case "bedrock":
		return bedrock.GetBedrockModelMetadata(modelID)
	case "azure":
		return azure.GetAzureModelMetadata(modelID)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

// CachePricing holds separated cache read and write costs
type CachePricing struct {
	ReadCost  float64 // Cost for cache reads (discounted rate)
	WriteCost float64 // Cost for cache writes (premium rate, 1.25x)
	TotalCost float64 // Total cache cost (read + write)
}

// calculatePricingFromModelData calculates pricing from ModelTokenData using model metadata
// Returns input, output, reasoning, cache costs, total cost, and context window size
// Cache costs are now separated into read (discounted) and write (premium 1.25x) components
func calculatePricingFromModelData(modelData *ModelTokenData) (inputCost, outputCost, reasoningCost, cacheCost, totalCost float64, contextWindow int) {
	if modelData == nil {
		return 0, 0, 0, 0, 0, 0
	}

	// Get model metadata
	metadata, err := getModelMetadata(modelData.Provider, modelData.ModelID)
	if err != nil || metadata == nil {
		// If metadata is not available, return zeros (pricing will be 0)
		return 0, 0, 0, 0, 0, 0
	}

	contextWindow = metadata.ContextWindow

	// Calculate input cost (excluding cached tokens which are charged separately)
	// Input tokens = total input tokens - all cached tokens (both read and write are charged separately)
	totalCacheTokens := modelData.CacheReadTokens + modelData.CacheWriteTokens
	// Fall back to CacheTokens if separate fields are not set (backward compatibility)
	if totalCacheTokens == 0 && modelData.CacheTokens > 0 {
		totalCacheTokens = modelData.CacheTokens
	}
	inputTokens := modelData.InputTokens - totalCacheTokens
	if inputTokens < 0 {
		// Safety check: cache tokens should not exceed input tokens
		// This could indicate a data inconsistency, but we'll clamp to 0 to prevent negative costs
		inputTokens = 0
	}
	if inputTokens > 0 {
		inputCost = calculateCostFromTokens(inputTokens, metadata.InputCostPer1MTokens)
	}

	// Calculate output cost
	if modelData.OutputTokens > 0 {
		outputCost = calculateCostFromTokens(modelData.OutputTokens, metadata.OutputCostPer1MTokens)
	}

	// Calculate reasoning cost
	if modelData.ReasoningTokens > 0 && metadata.ReasoningCostPer1MTokens > 0 {
		reasoningCost = calculateCostFromTokens(modelData.ReasoningTokens, metadata.ReasoningCostPer1MTokens)
	}

	// Calculate cache costs separately for read and write
	// Cache read tokens are charged at discounted rate (CachedInputCostPer1MTokens)
	// Cache write tokens are charged at premium rate (CachedInputCostWritePer1MTokens, typically 1.25x base)
	var cacheReadCost, cacheWriteCost float64

	if modelData.CacheReadTokens > 0 && metadata.CachedInputCostPer1MTokens > 0 {
		cacheReadCost = calculateCostFromTokens(modelData.CacheReadTokens, metadata.CachedInputCostPer1MTokens)
	}

	if modelData.CacheWriteTokens > 0 && metadata.CachedInputCostWritePer1MTokens > 0 {
		cacheWriteCost = calculateCostFromTokens(modelData.CacheWriteTokens, metadata.CachedInputCostWritePer1MTokens)
	}

	// Backward compatibility: if separate cache fields are not set but CacheTokens is,
	// treat all as read tokens (conservative estimate - discounted rate)
	if modelData.CacheReadTokens == 0 && modelData.CacheWriteTokens == 0 && modelData.CacheTokens > 0 {
		if metadata.CachedInputCostPer1MTokens > 0 {
			cacheReadCost = calculateCostFromTokens(modelData.CacheTokens, metadata.CachedInputCostPer1MTokens)
		}
	}

	cacheCost = cacheReadCost + cacheWriteCost

	// Store the separated costs back to modelData for persistence
	modelData.CacheReadCost = cacheReadCost
	modelData.CacheWriteCost = cacheWriteCost
	modelData.CacheCost = cacheCost

	totalCost = inputCost + outputCost + reasoningCost + cacheCost

	return inputCost, outputCost, reasoningCost, cacheCost, totalCost, contextWindow
}

// GetStepTokenUsage reads token usage from file for a specific step (aggregated across all models)
func (bo *BaseOrchestrator) GetStepTokenUsage(phase string, step int, stepID string) *StepTokenUsage {
	if bo.iterationFolder == "" {
		return &StepTokenUsage{}
	}

	ctx := context.Background()
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", bo.iterationFolder, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		return &StepTokenUsage{}
	}

	var tokenFile *TokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		return &StepTokenUsage{}
	}

	// Use stepID if available (new format), otherwise fall back to numeric index (old format)
	var stepKey string
	if stepID != "" {
		stepKey = fmt.Sprintf("%s:%s", phase, stepID)
	} else {
		stepKey = fmt.Sprintf("%s:%d", phase, step)
	}
	modelMap, exists := tokenFile.ByStepAndModel[stepKey]
	if !exists || modelMap == nil {
		return &StepTokenUsage{}
	}

	// Aggregate across all models for this step
	result := &StepTokenUsage{}
	var maxContextUsagePercent float64
	for _, modelUsage := range modelMap {
		result.InputTokens += modelUsage.InputTokens
		result.OutputTokens += modelUsage.OutputTokens
		result.CacheTokens += modelUsage.CacheTokens
		result.ReasoningTokens += modelUsage.ReasoningTokens
		result.LLMCallCount += modelUsage.LLMCallCount
		// Aggregate pricing
		result.InputCost += modelUsage.InputCost
		result.OutputCost += modelUsage.OutputCost
		result.ReasoningCost += modelUsage.ReasoningCost
		result.CacheCost += modelUsage.CacheCost
		result.TotalCost += modelUsage.TotalCost
		// Track max context usage percentage
		if modelUsage.ContextUsagePercent > maxContextUsagePercent {
			maxContextUsagePercent = modelUsage.ContextUsagePercent
		}
	}
	result.ContextUsagePercent = maxContextUsagePercent

	return result
}

// EmitStepTokenUsage reads token usage from file and emits a step token usage summary event
// Aggregates tokens across all models used in the step
func (bo *BaseOrchestrator) EmitStepTokenUsage(ctx context.Context, phase string, step int, stepID string, stepTitle string, clearAfterEmit bool) {
	if bo.iterationFolder == "" {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ No iteration folder, cannot read token usage for step %s:%s", phase, stepID))
		return
	}

	// Read token usage from file
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", bo.iterationFolder, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ No token usage file found for step %s:%s", phase, stepID))
		return
	}

	var tokenFile *TokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse token usage file: %v", err))
		return
	}

	// Find step data and aggregate across all models
	// Use stepID if available (new format), otherwise fall back to numeric index (old format)
	var stepKey string
	if stepID != "" {
		stepKey = fmt.Sprintf("%s:%s", phase, stepID)
	} else {
		stepKey = fmt.Sprintf("%s:%d", phase, step)
	}
	modelMap, exists := tokenFile.ByStepAndModel[stepKey]
	if !exists || modelMap == nil || len(modelMap) == 0 {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ No token usage data found for step %s (key: %s)", stepTitle, stepKey))
		return
	}

	// Aggregate tokens and pricing across all models for this step
	var inputTokens, outputTokens, cacheTokens, reasoningTokens, llmCallCount int
	var inputCost, outputCost, reasoningCost, cacheCost, totalCost float64
	var maxContextUsagePercent float64
	for _, modelUsage := range modelMap {
		inputTokens += modelUsage.InputTokens
		outputTokens += modelUsage.OutputTokens
		cacheTokens += modelUsage.CacheTokens
		reasoningTokens += modelUsage.ReasoningTokens
		llmCallCount += modelUsage.LLMCallCount
		// Aggregate pricing
		inputCost += modelUsage.InputCost
		outputCost += modelUsage.OutputCost
		reasoningCost += modelUsage.ReasoningCost
		cacheCost += modelUsage.CacheCost
		totalCost += modelUsage.TotalCost
		// Track max context usage percentage
		if modelUsage.ContextUsagePercent > maxContextUsagePercent {
			maxContextUsagePercent = modelUsage.ContextUsagePercent
		}
	}

	// Calculate total for event
	totalTokens := inputTokens + outputTokens

	// Create and emit step token usage event with pricing
	stepTokenEvent := events.NewStepTokenUsageEventWithPricing(
		phase,
		step,
		stepTitle,
		inputTokens,  // prompt_tokens in event
		outputTokens, // completion_tokens in event
		totalTokens,  // total_tokens in event
		cacheTokens,
		reasoningTokens,
		llmCallCount,
		0, // CacheEnabledCallCount not stored in file (could be calculated if needed)
		inputCost,
		outputCost,
		reasoningCost,
		cacheCost,
		totalCost,
		maxContextUsagePercent,
	)

	bo.emitEvent(ctx, events.StepTokenUsage, stepTokenEvent)

	bo.GetLogger().Info(fmt.Sprintf("📊 Emitted step token usage for %s:%s (%s) - Input: %d, Output: %d, Cache: %d, Reasoning: %d, Calls: %d, Cost: $%.4f, Context: %.1f%%",
		phase, stepID, stepTitle, inputTokens, outputTokens, cacheTokens, reasoningTokens, llmCallCount, totalCost, maxContextUsagePercent))
}

// GetModelTokenUsage reads token usage from file for a specific model
func (bo *BaseOrchestrator) GetModelTokenUsage(modelID string) *ModelTokenUsage {
	if bo.iterationFolder == "" {
		return &ModelTokenUsage{}
	}

	ctx := context.Background()
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", bo.iterationFolder, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		return &ModelTokenUsage{}
	}

	var tokenFile *TokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		return &ModelTokenUsage{}
	}

	usage, exists := tokenFile.ByModel[modelID]
	if !exists {
		return &ModelTokenUsage{}
	}

	return usage
}

// GetAllModelTokenUsage reads all model token usage from file
func (bo *BaseOrchestrator) GetAllModelTokenUsage() map[string]*ModelTokenUsage {
	if bo.iterationFolder == "" {
		return make(map[string]*ModelTokenUsage)
	}

	ctx := context.Background()
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", bo.iterationFolder, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		return make(map[string]*ModelTokenUsage)
	}

	var tokenFile *TokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		return make(map[string]*ModelTokenUsage)
	}

	return tokenFile.ByModel
}

// GetStepModelTokenUsage reads token usage from file for a specific step and model
func (bo *BaseOrchestrator) GetStepModelTokenUsage(phase string, step int, stepID string, modelID string) *ModelTokenUsage {
	if bo.iterationFolder == "" {
		return &ModelTokenUsage{}
	}

	ctx := context.Background()
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", bo.iterationFolder, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		return &ModelTokenUsage{}
	}

	var tokenFile *TokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		return &ModelTokenUsage{}
	}

	// Use stepID if available (new format), otherwise fall back to numeric index (old format)
	var stepKey string
	if stepID != "" {
		stepKey = fmt.Sprintf("%s:%s", phase, stepID)
	} else {
		stepKey = fmt.Sprintf("%s:%d", phase, step)
	}
	if tokenFile.ByStepAndModel == nil {
		return &ModelTokenUsage{}
	}

	modelMap, exists := tokenFile.ByStepAndModel[stepKey]
	if !exists || modelMap == nil {
		return &ModelTokenUsage{}
	}

	usage, exists := modelMap[modelID]
	if !exists {
		return &ModelTokenUsage{}
	}

	return usage
}

// GetStepModels reads all models used in a specific step from file
func (bo *BaseOrchestrator) GetStepModels(phase string, step int, stepID string) map[string]*ModelTokenUsage {
	if bo.iterationFolder == "" {
		return make(map[string]*ModelTokenUsage)
	}

	ctx := context.Background()
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", bo.iterationFolder, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		return make(map[string]*ModelTokenUsage)
	}

	var tokenFile *TokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		return make(map[string]*ModelTokenUsage)
	}

	// Use stepID if available (new format), otherwise fall back to numeric index (old format)
	var stepKey string
	if stepID != "" {
		stepKey = fmt.Sprintf("%s:%s", phase, stepID)
	} else {
		stepKey = fmt.Sprintf("%s:%d", phase, step)
	}
	if tokenFile.ByStepAndModel == nil {
		return make(map[string]*ModelTokenUsage)
	}

	modelMap, exists := tokenFile.ByStepAndModel[stepKey]
	if !exists || modelMap == nil {
		return make(map[string]*ModelTokenUsage)
	}

	// Return a copy to avoid external modifications
	result := make(map[string]*ModelTokenUsage)
	for modelID, usage := range modelMap {
		result[modelID] = &ModelTokenUsage{
			Provider:            usage.Provider,
			InputTokens:         usage.InputTokens,
			OutputTokens:        usage.OutputTokens,
			InputTokensM:        usage.InputTokensM,
			OutputTokensM:       usage.OutputTokensM,
			CacheTokens:         usage.CacheTokens,
			CacheTokensM:        usage.CacheTokensM,
			ReasoningTokens:     usage.ReasoningTokens,
			ReasoningTokensM:    usage.ReasoningTokensM,
			LLMCallCount:        usage.LLMCallCount,
			InputCost:           usage.InputCost,
			OutputCost:          usage.OutputCost,
			ReasoningCost:       usage.ReasoningCost,
			CacheCost:           usage.CacheCost,
			TotalCost:           usage.TotalCost,
			ContextWindowUsage:  usage.ContextWindowUsage,
			ModelContextWindow:  usage.ModelContextWindow,
			ContextUsagePercent: usage.ContextUsagePercent,
		}
	}

	return result
}

// PersistTokenUsage saves token usage directly to token_usage.json in the iteration folder
// It reads existing token data from the file, merges the new token data, and writes back.
// The file is the single source of truth - no in-memory accumulation.
// Note: TokenUsageEvent is only emitted once per conversation (at end) with cumulative totals,
// so there's no duplicate counting. However, multiple conversations/steps could complete concurrently,
// so we use a mutex to protect file read/write operations.
func (bo *BaseOrchestrator) PersistTokenUsage(ctx context.Context, iterationFolder string,
	stepTokenData *StepTokenData, modelTokenData *ModelTokenData) error {
	if iterationFolder == "" {
		// Removed verbose logging
		return nil
	}

	// Acquire mutex to prevent race conditions when multiple conversations/steps complete concurrently
	tokenFileMutex.Lock()
	defer tokenFileMutex.Unlock()

	// Build file path: runs/{iterationFolder}/token_usage.json
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", iterationFolder, "token_usage.json")

	bo.GetLogger().Debug(fmt.Sprintf("💾 Persisting token usage to: %s", filePath))

	// Read existing token usage file if it exists
	var existingFile *TokenUsageFile
	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err == nil && existingContent != "" {
		// File exists, try to parse it
		if err := json.Unmarshal([]byte(existingContent), &existingFile); err != nil {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse existing token_usage.json: %v (will create new file)", err))
			existingFile = nil
		}
	} else if err != nil {
		// File doesn't exist or error reading - this is expected for new files
		// Only log if it's not a "file not found" type error
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			// Removed verbose logging
		}
		existingFile = nil
	}

	// Build the token usage file structure
	// Start with existing data if available, otherwise create new
	tokenFile := &TokenUsageFile{
		UpdatedAt:      time.Now(),
		ByModel:        make(map[string]*ModelTokenUsage),
		ByStepAndModel: make(map[string]map[string]*ModelTokenUsage),
	}

	// Preserve CreatedAt from existing file, or set to now if new file
	if existingFile != nil {
		tokenFile.CreatedAt = existingFile.CreatedAt
		// Copy existing data
		if existingFile.ByModel != nil {
			for k, v := range existingFile.ByModel {
				tokenFile.ByModel[k] = &ModelTokenUsage{
					Provider:            v.Provider,
					InputTokens:         v.InputTokens,
					OutputTokens:        v.OutputTokens,
					InputTokensM:        v.InputTokensM,
					OutputTokensM:       v.OutputTokensM,
					CacheTokens:         v.CacheTokens,
					CacheTokensM:        v.CacheTokensM,
					ReasoningTokens:     v.ReasoningTokens,
					ReasoningTokensM:    v.ReasoningTokensM,
					LLMCallCount:        v.LLMCallCount,
					InputCost:           v.InputCost,
					OutputCost:          v.OutputCost,
					ReasoningCost:       v.ReasoningCost,
					CacheCost:           v.CacheCost,
					TotalCost:           v.TotalCost,
					ContextWindowUsage:  v.ContextWindowUsage,
					ModelContextWindow:  v.ModelContextWindow,
					ContextUsagePercent: v.ContextUsagePercent,
				}
			}
		}
		// Copy existing ByStepAndModel data if it exists (backward compatibility)
		if existingFile.ByStepAndModel != nil {
			for stepKey, modelMap := range existingFile.ByStepAndModel {
				tokenFile.ByStepAndModel[stepKey] = make(map[string]*ModelTokenUsage)
				for modelID, v := range modelMap {
					tokenFile.ByStepAndModel[stepKey][modelID] = &ModelTokenUsage{
						Provider:            v.Provider,
						InputTokens:         v.InputTokens,
						OutputTokens:        v.OutputTokens,
						InputTokensM:        v.InputTokensM,
						OutputTokensM:       v.OutputTokensM,
						CacheTokens:         v.CacheTokens,
						CacheTokensM:        v.CacheTokensM,
						ReasoningTokens:     v.ReasoningTokens,
						ReasoningTokensM:    v.ReasoningTokensM,
						LLMCallCount:        v.LLMCallCount,
						InputCost:           v.InputCost,
						OutputCost:          v.OutputCost,
						ReasoningCost:       v.ReasoningCost,
						CacheCost:           v.CacheCost,
						TotalCost:           v.TotalCost,
						ContextWindowUsage:  v.ContextWindowUsage,
						ModelContextWindow:  v.ModelContextWindow,
						ContextUsagePercent: v.ContextUsagePercent,
					}
				}
			}
		}
	} else {
		// New file - set CreatedAt to now
		tokenFile.CreatedAt = time.Now()
	}

	// Merge new model token data if provided
	if modelTokenData != nil {
		// Calculate pricing for this model data
		inputCost, outputCost, reasoningCost, cacheCost, totalCost, contextWindow := calculatePricingFromModelData(modelTokenData)

		// Calculate context window usage percentage
		var contextUsagePercent float64
		totalTokens := modelTokenData.InputTokens + modelTokenData.OutputTokens
		if contextWindow > 0 {
			contextUsagePercent = (float64(totalTokens) / float64(contextWindow)) * 100.0
			if contextUsagePercent > 100.0 {
				contextUsagePercent = 100.0
			}
		}

		if existing, exists := tokenFile.ByModel[modelTokenData.ModelID]; exists {
			// Merge with existing data (add raw integers)
			existing.InputTokens += modelTokenData.InputTokens
			existing.OutputTokens += modelTokenData.OutputTokens
			existing.CacheTokens += modelTokenData.CacheTokens
			existing.CacheReadTokens += modelTokenData.CacheReadTokens
			existing.CacheWriteTokens += modelTokenData.CacheWriteTokens
			existing.ReasoningTokens += modelTokenData.ReasoningTokens
			existing.LLMCallCount += modelTokenData.LLMCallCount
			// Recalculate formatted strings
			existing.InputTokensM = formatTokensM(existing.InputTokens)
			existing.OutputTokensM = formatTokensM(existing.OutputTokens)
			existing.CacheTokensM = formatTokensM(existing.CacheTokens)
			existing.CacheReadTokensM = formatTokensM(existing.CacheReadTokens)
			existing.CacheWriteTokensM = formatTokensM(existing.CacheWriteTokens)
			existing.ReasoningTokensM = formatTokensM(existing.ReasoningTokens)
			// Accumulate pricing
			existing.InputCost += inputCost
			existing.OutputCost += outputCost
			existing.ReasoningCost += reasoningCost
			existing.CacheCost += cacheCost
			existing.CacheReadCost += modelTokenData.CacheReadCost
			existing.CacheWriteCost += modelTokenData.CacheWriteCost
			existing.TotalCost += totalCost
			// Update context window tracking
			// Use per-call tokens (not cumulative) to reflect actual context window usage
			// Conversation-end events (LLMCallCount > 1) have cumulative PromptTokens
			// which don't represent actual context window usage per call
			if contextWindow > 0 {
				existing.ModelContextWindow = contextWindow
				if modelTokenData.LLMCallCount <= 1 {
					// Per-call event: use this call's tokens as actual context usage
					currentCallTokens := modelTokenData.InputTokens + modelTokenData.OutputTokens
					existing.ContextWindowUsage = currentCallTokens
					currentPercent := (float64(currentCallTokens) / float64(contextWindow)) * 100.0
					if currentPercent > 100.0 {
						currentPercent = 100.0
					}
					existing.ContextUsagePercent = currentPercent
				}
			}
		} else {
			// New model - create entry
			tokenFile.ByModel[modelTokenData.ModelID] = &ModelTokenUsage{
				Provider:          modelTokenData.Provider,
				InputTokens:       modelTokenData.InputTokens,
				OutputTokens:      modelTokenData.OutputTokens,
				InputTokensM:      formatTokensM(modelTokenData.InputTokens),
				OutputTokensM:     formatTokensM(modelTokenData.OutputTokens),
				CacheTokens:       modelTokenData.CacheTokens,
				CacheTokensM:      formatTokensM(modelTokenData.CacheTokens),
				CacheReadTokens:   modelTokenData.CacheReadTokens,
				CacheReadTokensM:  formatTokensM(modelTokenData.CacheReadTokens),
				CacheWriteTokens:  modelTokenData.CacheWriteTokens,
				CacheWriteTokensM: formatTokensM(modelTokenData.CacheWriteTokens),
				ReasoningTokens:   modelTokenData.ReasoningTokens,
				ReasoningTokensM:  formatTokensM(modelTokenData.ReasoningTokens),
				LLMCallCount:      modelTokenData.LLMCallCount,
				InputCost:         inputCost,
				OutputCost:        outputCost,
				ReasoningCost:     reasoningCost,
				CacheCost:         cacheCost,
				CacheReadCost:     modelTokenData.CacheReadCost,
				CacheWriteCost:    modelTokenData.CacheWriteCost,
				TotalCost:         totalCost,
				ContextWindowUsage:  totalTokens,
				ModelContextWindow:  contextWindow,
				ContextUsagePercent: contextUsagePercent,
			}
		}
	}

	// Store step+model token data if both stepTokenData and modelTokenData are provided
	if stepTokenData != nil && modelTokenData != nil {
		// Use StepID if available (new format), otherwise fall back to step index (old format)
		var stepKey string
		if stepTokenData.StepID != "" {
			stepKey = fmt.Sprintf("%s:%s", stepTokenData.Phase, stepTokenData.StepID)
		} else {
			stepKey = fmt.Sprintf("%s:%d", stepTokenData.Phase, stepTokenData.Step)
		}
		modelID := modelTokenData.ModelID

		// Initialize step map if it doesn't exist
		if tokenFile.ByStepAndModel[stepKey] == nil {
			tokenFile.ByStepAndModel[stepKey] = make(map[string]*ModelTokenUsage)
		}

		// Calculate pricing for this model data
		inputCost, outputCost, reasoningCost, cacheCost, totalCost, contextWindow := calculatePricingFromModelData(modelTokenData)

		// Calculate context window usage percentage
		var contextUsagePercent float64
		totalTokens := modelTokenData.InputTokens + modelTokenData.OutputTokens
		if contextWindow > 0 {
			contextUsagePercent = (float64(totalTokens) / float64(contextWindow)) * 100.0
			if contextUsagePercent > 100.0 {
				contextUsagePercent = 100.0
			}
		}

		// Merge with existing model data for this step if it exists
		if existing, exists := tokenFile.ByStepAndModel[stepKey][modelID]; exists {
			// Merge with existing data (add raw integers)
			existing.InputTokens += modelTokenData.InputTokens
			existing.OutputTokens += modelTokenData.OutputTokens
			existing.CacheTokens += modelTokenData.CacheTokens
			existing.CacheReadTokens += modelTokenData.CacheReadTokens
			existing.CacheWriteTokens += modelTokenData.CacheWriteTokens
			existing.ReasoningTokens += modelTokenData.ReasoningTokens
			existing.LLMCallCount += modelTokenData.LLMCallCount
			// Recalculate formatted strings
			existing.InputTokensM = formatTokensM(existing.InputTokens)
			existing.OutputTokensM = formatTokensM(existing.OutputTokens)
			existing.CacheTokensM = formatTokensM(existing.CacheTokens)
			existing.CacheReadTokensM = formatTokensM(existing.CacheReadTokens)
			existing.CacheWriteTokensM = formatTokensM(existing.CacheWriteTokens)
			existing.ReasoningTokensM = formatTokensM(existing.ReasoningTokens)
			// Accumulate pricing
			existing.InputCost += inputCost
			existing.OutputCost += outputCost
			existing.ReasoningCost += reasoningCost
			existing.CacheCost += cacheCost
			existing.CacheReadCost += modelTokenData.CacheReadCost
			existing.CacheWriteCost += modelTokenData.CacheWriteCost
			existing.TotalCost += totalCost
			// Update context window tracking
			// Use per-call tokens (not cumulative) to reflect actual context window usage
			// Conversation-end events (LLMCallCount > 1) have cumulative PromptTokens
			// which don't represent actual context window usage per call
			if contextWindow > 0 {
				existing.ModelContextWindow = contextWindow
				if modelTokenData.LLMCallCount <= 1 {
					// Per-call event: use this call's tokens as actual context usage
					currentCallTokens := modelTokenData.InputTokens + modelTokenData.OutputTokens
					existing.ContextWindowUsage = currentCallTokens
					currentPercent := (float64(currentCallTokens) / float64(contextWindow)) * 100.0
					if currentPercent > 100.0 {
						currentPercent = 100.0
					}
					existing.ContextUsagePercent = currentPercent
				}
			}
		} else {
			// New model for this step - create entry
			tokenFile.ByStepAndModel[stepKey][modelID] = &ModelTokenUsage{
				Provider:          modelTokenData.Provider,
				InputTokens:       modelTokenData.InputTokens,
				OutputTokens:      modelTokenData.OutputTokens,
				InputTokensM:      formatTokensM(modelTokenData.InputTokens),
				OutputTokensM:     formatTokensM(modelTokenData.OutputTokens),
				CacheTokens:       modelTokenData.CacheTokens,
				CacheTokensM:      formatTokensM(modelTokenData.CacheTokens),
				CacheReadTokens:   modelTokenData.CacheReadTokens,
				CacheReadTokensM:  formatTokensM(modelTokenData.CacheReadTokens),
				CacheWriteTokens:  modelTokenData.CacheWriteTokens,
				CacheWriteTokensM: formatTokensM(modelTokenData.CacheWriteTokens),
				ReasoningTokens:   modelTokenData.ReasoningTokens,
				ReasoningTokensM:  formatTokensM(modelTokenData.ReasoningTokens),
				LLMCallCount:      modelTokenData.LLMCallCount,
				InputCost:         inputCost,
				OutputCost:        outputCost,
				ReasoningCost:     reasoningCost,
				CacheCost:         cacheCost,
				CacheReadCost:     modelTokenData.CacheReadCost,
				CacheWriteCost:    modelTokenData.CacheWriteCost,
				TotalCost:         totalCost,
				ContextWindowUsage:  totalTokens,
				ModelContextWindow:  contextWindow,
				ContextUsagePercent: contextUsagePercent,
			}
		}
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(tokenFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token usage data: %w", err)
	}

	// Write to file
	if err := bo.WriteWorkspaceFile(ctx, filePath, string(jsonData)); err != nil {
		return fmt.Errorf("failed to write token usage file: %w", err)
	}

	bo.GetLogger().Debug("✅ Persisted token usage to file")

	return nil
}

// phaseTokenFileMutex ensures thread-safe access to phase token_usage.json in main workspace folder
var phaseTokenFileMutex sync.Mutex

// IsPhaseOnlyAgent checks if a phase is a phase-only agent (not step-based)
// Phase-only agents run independently and don't have step numbers
// This is exported so context_aware_bridge can use it
func IsPhaseOnlyAgent(phase string) bool {
	phaseOnlyPhases := []string{
		"planning",
	}
	for _, p := range phaseOnlyPhases {
		if phase == p {
			return true
		}
	}
	return false
}

// PersistPhaseTokenUsage saves token usage directly to token_usage.json in the main workspace folder
// It reads existing token data from the file, merges the new token data, and writes back.
// The file is the single source of truth - no in-memory accumulation.
// This is used for phase-only agents (planning, plan-improvement, etc.) that don't have iteration folders.
func (bo *BaseOrchestrator) PersistPhaseTokenUsage(ctx context.Context,
	phaseTokenData *PhaseTokenData, modelTokenData *ModelTokenData) error {
	if phaseTokenData == nil || phaseTokenData.Phase == "" {
		return nil
	}

	// Acquire mutex to prevent race conditions when multiple phase agents complete concurrently
	phaseTokenFileMutex.Lock()
	defer phaseTokenFileMutex.Unlock()

	// Build file path: workspace/token_usage.json (main workspace folder, not in runs/)
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "token_usage.json")

	bo.GetLogger().Debug(fmt.Sprintf("💾 Persisting phase token usage to: %s", filePath))

	// Read existing token usage file if it exists
	var existingFile *PhaseTokenUsageFile
	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err == nil && existingContent != "" {
		// File exists, try to parse it
		if err := json.Unmarshal([]byte(existingContent), &existingFile); err != nil {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse existing phase token_usage.json: %v (will create new file)", err))
			existingFile = nil
		}
	} else if err != nil {
		// File doesn't exist or error reading - this is expected for new files
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			bo.GetLogger().Debug("📝 Phase token usage file doesn't exist yet, will create new one")
		}
		existingFile = nil
	}

	// Build the token usage file structure
	// Start with existing data if available, otherwise create new
	tokenFile := &PhaseTokenUsageFile{
		UpdatedAt:       time.Now(),
		ByPhaseAndModel: make(map[string]map[string]*ModelTokenUsage),
		ByModel:         make(map[string]*ModelTokenUsage),
	}

	// Preserve CreatedAt from existing file, or set to now if new file
	if existingFile != nil {
		tokenFile.CreatedAt = existingFile.CreatedAt
		// Copy existing ByPhaseAndModel data
		if existingFile.ByPhaseAndModel != nil {
			for phaseKey, modelMap := range existingFile.ByPhaseAndModel {
				tokenFile.ByPhaseAndModel[phaseKey] = make(map[string]*ModelTokenUsage)
				for modelID, v := range modelMap {
					tokenFile.ByPhaseAndModel[phaseKey][modelID] = &ModelTokenUsage{
						Provider:            v.Provider,
						InputTokens:         v.InputTokens,
						OutputTokens:        v.OutputTokens,
						InputTokensM:        v.InputTokensM,
						OutputTokensM:       v.OutputTokensM,
						CacheTokens:         v.CacheTokens,
						CacheTokensM:        v.CacheTokensM,
						ReasoningTokens:     v.ReasoningTokens,
						ReasoningTokensM:    v.ReasoningTokensM,
						LLMCallCount:        v.LLMCallCount,
						InputCost:           v.InputCost,
						OutputCost:          v.OutputCost,
						ReasoningCost:       v.ReasoningCost,
						CacheCost:           v.CacheCost,
						TotalCost:           v.TotalCost,
						ContextWindowUsage:  v.ContextWindowUsage,
						ModelContextWindow:  v.ModelContextWindow,
						ContextUsagePercent: v.ContextUsagePercent,
					}
				}
			}
		}
		// Copy existing ByModel data
		if existingFile.ByModel != nil {
			for k, v := range existingFile.ByModel {
				tokenFile.ByModel[k] = &ModelTokenUsage{
					Provider:            v.Provider,
					InputTokens:         v.InputTokens,
					OutputTokens:        v.OutputTokens,
					InputTokensM:        v.InputTokensM,
					OutputTokensM:       v.OutputTokensM,
					CacheTokens:         v.CacheTokens,
					CacheTokensM:        v.CacheTokensM,
					ReasoningTokens:     v.ReasoningTokens,
					ReasoningTokensM:    v.ReasoningTokensM,
					LLMCallCount:        v.LLMCallCount,
					InputCost:           v.InputCost,
					OutputCost:          v.OutputCost,
					ReasoningCost:       v.ReasoningCost,
					CacheCost:           v.CacheCost,
					TotalCost:           v.TotalCost,
					ContextWindowUsage:  v.ContextWindowUsage,
					ModelContextWindow:  v.ModelContextWindow,
					ContextUsagePercent: v.ContextUsagePercent,
				}
			}
		}
	} else {
		// New file - set CreatedAt to now
		tokenFile.CreatedAt = time.Now()
	}

	// Merge new model token data if provided (aggregate across all phases)
	if modelTokenData != nil {
		// Calculate pricing for this model data
		inputCost, outputCost, reasoningCost, cacheCost, totalCost, contextWindow := calculatePricingFromModelData(modelTokenData)

		// Calculate context window usage percentage
		var contextUsagePercent float64
		totalTokens := modelTokenData.InputTokens + modelTokenData.OutputTokens
		if contextWindow > 0 {
			contextUsagePercent = (float64(totalTokens) / float64(contextWindow)) * 100.0
			if contextUsagePercent > 100.0 {
				contextUsagePercent = 100.0
			}
		}

		if existing, exists := tokenFile.ByModel[modelTokenData.ModelID]; exists {
			// Merge with existing data (add raw integers)
			existing.InputTokens += modelTokenData.InputTokens
			existing.OutputTokens += modelTokenData.OutputTokens
			existing.CacheTokens += modelTokenData.CacheTokens
			existing.CacheReadTokens += modelTokenData.CacheReadTokens
			existing.CacheWriteTokens += modelTokenData.CacheWriteTokens
			existing.ReasoningTokens += modelTokenData.ReasoningTokens
			existing.LLMCallCount += modelTokenData.LLMCallCount
			// Recalculate formatted strings
			existing.InputTokensM = formatTokensM(existing.InputTokens)
			existing.OutputTokensM = formatTokensM(existing.OutputTokens)
			existing.CacheTokensM = formatTokensM(existing.CacheTokens)
			existing.CacheReadTokensM = formatTokensM(existing.CacheReadTokens)
			existing.CacheWriteTokensM = formatTokensM(existing.CacheWriteTokens)
			existing.ReasoningTokensM = formatTokensM(existing.ReasoningTokens)
			// Accumulate pricing
			existing.InputCost += inputCost
			existing.OutputCost += outputCost
			existing.ReasoningCost += reasoningCost
			existing.CacheCost += cacheCost
			existing.CacheReadCost += modelTokenData.CacheReadCost
			existing.CacheWriteCost += modelTokenData.CacheWriteCost
			existing.TotalCost += totalCost
			// Update context window tracking
			// Use per-call tokens (not cumulative) to reflect actual context window usage
			// Conversation-end events (LLMCallCount > 1) have cumulative PromptTokens
			// which don't represent actual context window usage per call
			if contextWindow > 0 {
				existing.ModelContextWindow = contextWindow
				if modelTokenData.LLMCallCount <= 1 {
					// Per-call event: use this call's tokens as actual context usage
					currentCallTokens := modelTokenData.InputTokens + modelTokenData.OutputTokens
					existing.ContextWindowUsage = currentCallTokens
					currentPercent := (float64(currentCallTokens) / float64(contextWindow)) * 100.0
					if currentPercent > 100.0 {
						currentPercent = 100.0
					}
					existing.ContextUsagePercent = currentPercent
				}
			}
		} else {
			// New model - create entry
			tokenFile.ByModel[modelTokenData.ModelID] = &ModelTokenUsage{
				Provider:          modelTokenData.Provider,
				InputTokens:       modelTokenData.InputTokens,
				OutputTokens:      modelTokenData.OutputTokens,
				InputTokensM:      formatTokensM(modelTokenData.InputTokens),
				OutputTokensM:     formatTokensM(modelTokenData.OutputTokens),
				CacheTokens:       modelTokenData.CacheTokens,
				CacheTokensM:      formatTokensM(modelTokenData.CacheTokens),
				CacheReadTokens:   modelTokenData.CacheReadTokens,
				CacheReadTokensM:  formatTokensM(modelTokenData.CacheReadTokens),
				CacheWriteTokens:  modelTokenData.CacheWriteTokens,
				CacheWriteTokensM: formatTokensM(modelTokenData.CacheWriteTokens),
				ReasoningTokens:   modelTokenData.ReasoningTokens,
				ReasoningTokensM:  formatTokensM(modelTokenData.ReasoningTokens),
				LLMCallCount:      modelTokenData.LLMCallCount,
				InputCost:         inputCost,
				OutputCost:        outputCost,
				ReasoningCost:     reasoningCost,
				CacheCost:         cacheCost,
				CacheReadCost:     modelTokenData.CacheReadCost,
				CacheWriteCost:    modelTokenData.CacheWriteCost,
				TotalCost:         totalCost,
				ContextWindowUsage:  totalTokens,
				ModelContextWindow:  contextWindow,
				ContextUsagePercent: contextUsagePercent,
			}
		}
	}

	// Store phase+model token data if modelTokenData is provided
	// phaseTokenData is guaranteed to be non-nil (checked at function start)
	if modelTokenData != nil {
		phaseKey := phaseTokenData.Phase
		modelID := modelTokenData.ModelID

		// Initialize phase map if it doesn't exist
		if tokenFile.ByPhaseAndModel[phaseKey] == nil {
			tokenFile.ByPhaseAndModel[phaseKey] = make(map[string]*ModelTokenUsage)
		}

		// Calculate pricing for this model data
		inputCost, outputCost, reasoningCost, cacheCost, totalCost, contextWindow := calculatePricingFromModelData(modelTokenData)

		// Calculate context window usage percentage
		var contextUsagePercent float64
		totalTokens := modelTokenData.InputTokens + modelTokenData.OutputTokens
		if contextWindow > 0 {
			contextUsagePercent = (float64(totalTokens) / float64(contextWindow)) * 100.0
			if contextUsagePercent > 100.0 {
				contextUsagePercent = 100.0
			}
		}

		// Merge with existing model data for this phase if it exists
		if existing, exists := tokenFile.ByPhaseAndModel[phaseKey][modelID]; exists {
			// Merge with existing data (add raw integers)
			existing.InputTokens += modelTokenData.InputTokens
			existing.OutputTokens += modelTokenData.OutputTokens
			existing.CacheTokens += modelTokenData.CacheTokens
			existing.CacheReadTokens += modelTokenData.CacheReadTokens
			existing.CacheWriteTokens += modelTokenData.CacheWriteTokens
			existing.ReasoningTokens += modelTokenData.ReasoningTokens
			existing.LLMCallCount += modelTokenData.LLMCallCount
			// Recalculate formatted strings
			existing.InputTokensM = formatTokensM(existing.InputTokens)
			existing.OutputTokensM = formatTokensM(existing.OutputTokens)
			existing.CacheTokensM = formatTokensM(existing.CacheTokens)
			existing.CacheReadTokensM = formatTokensM(existing.CacheReadTokens)
			existing.CacheWriteTokensM = formatTokensM(existing.CacheWriteTokens)
			existing.ReasoningTokensM = formatTokensM(existing.ReasoningTokens)
			// Accumulate pricing
			existing.InputCost += inputCost
			existing.OutputCost += outputCost
			existing.ReasoningCost += reasoningCost
			existing.CacheCost += cacheCost
			existing.CacheReadCost += modelTokenData.CacheReadCost
			existing.CacheWriteCost += modelTokenData.CacheWriteCost
			existing.TotalCost += totalCost
			// Update context window tracking
			// Use per-call tokens (not cumulative) to reflect actual context window usage
			// Conversation-end events (LLMCallCount > 1) have cumulative PromptTokens
			// which don't represent actual context window usage per call
			if contextWindow > 0 {
				existing.ModelContextWindow = contextWindow
				if modelTokenData.LLMCallCount <= 1 {
					// Per-call event: use this call's tokens as actual context usage
					currentCallTokens := modelTokenData.InputTokens + modelTokenData.OutputTokens
					existing.ContextWindowUsage = currentCallTokens
					currentPercent := (float64(currentCallTokens) / float64(contextWindow)) * 100.0
					if currentPercent > 100.0 {
						currentPercent = 100.0
					}
					existing.ContextUsagePercent = currentPercent
				}
			}
		} else {
			// New model for this phase - create entry
			tokenFile.ByPhaseAndModel[phaseKey][modelID] = &ModelTokenUsage{
				Provider:          modelTokenData.Provider,
				InputTokens:       modelTokenData.InputTokens,
				OutputTokens:      modelTokenData.OutputTokens,
				InputTokensM:      formatTokensM(modelTokenData.InputTokens),
				OutputTokensM:     formatTokensM(modelTokenData.OutputTokens),
				CacheTokens:       modelTokenData.CacheTokens,
				CacheTokensM:      formatTokensM(modelTokenData.CacheTokens),
				CacheReadTokens:   modelTokenData.CacheReadTokens,
				CacheReadTokensM:  formatTokensM(modelTokenData.CacheReadTokens),
				CacheWriteTokens:  modelTokenData.CacheWriteTokens,
				CacheWriteTokensM: formatTokensM(modelTokenData.CacheWriteTokens),
				ReasoningTokens:   modelTokenData.ReasoningTokens,
				ReasoningTokensM:  formatTokensM(modelTokenData.ReasoningTokens),
				LLMCallCount:      modelTokenData.LLMCallCount,
				InputCost:         inputCost,
				OutputCost:        outputCost,
				ReasoningCost:     reasoningCost,
				CacheCost:         cacheCost,
				CacheReadCost:     modelTokenData.CacheReadCost,
				CacheWriteCost:    modelTokenData.CacheWriteCost,
				TotalCost:         totalCost,
				ContextWindowUsage:  totalTokens,
				ModelContextWindow:  contextWindow,
				ContextUsagePercent: contextUsagePercent,
			}
		}
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(tokenFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal phase token usage data: %w", err)
	}

	// Write to file
	if err := bo.WriteWorkspaceFile(ctx, filePath, string(jsonData)); err != nil {
		return fmt.Errorf("failed to write phase token usage file: %w", err)
	}

	bo.GetLogger().Debug("✅ Persisted phase token usage to file")

	return nil
}

// GetPhaseTokenUsage reads token usage from file for a specific phase (aggregated across all models)
func (bo *BaseOrchestrator) GetPhaseTokenUsage(phase string) *StepTokenUsage {
	ctx := context.Background()
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		return &StepTokenUsage{}
	}

	var tokenFile *PhaseTokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		return &StepTokenUsage{}
	}

	modelMap, exists := tokenFile.ByPhaseAndModel[phase]
	if !exists || modelMap == nil {
		return &StepTokenUsage{}
	}

	// Aggregate across all models for this phase
	result := &StepTokenUsage{}
	var maxContextUsagePercent float64
	for _, modelUsage := range modelMap {
		result.InputTokens += modelUsage.InputTokens
		result.OutputTokens += modelUsage.OutputTokens
		result.CacheTokens += modelUsage.CacheTokens
		result.ReasoningTokens += modelUsage.ReasoningTokens
		result.LLMCallCount += modelUsage.LLMCallCount
		// Aggregate pricing
		result.InputCost += modelUsage.InputCost
		result.OutputCost += modelUsage.OutputCost
		result.ReasoningCost += modelUsage.ReasoningCost
		result.CacheCost += modelUsage.CacheCost
		result.TotalCost += modelUsage.TotalCost
		// Track max context usage percentage
		if modelUsage.ContextUsagePercent > maxContextUsagePercent {
			maxContextUsagePercent = modelUsage.ContextUsagePercent
		}
	}
	result.ContextUsagePercent = maxContextUsagePercent

	return result
}

// GetPhaseModelTokenUsage reads token usage from file for a specific phase and model
func (bo *BaseOrchestrator) GetPhaseModelTokenUsage(phase string, modelID string) *ModelTokenUsage {
	ctx := context.Background()
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		return &ModelTokenUsage{}
	}

	var tokenFile *PhaseTokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		return &ModelTokenUsage{}
	}

	if tokenFile.ByPhaseAndModel == nil {
		return &ModelTokenUsage{}
	}

	modelMap, exists := tokenFile.ByPhaseAndModel[phase]
	if !exists || modelMap == nil {
		return &ModelTokenUsage{}
	}

	usage, exists := modelMap[modelID]
	if !exists {
		return &ModelTokenUsage{}
	}

	return usage
}

// GetAllPhaseTokenUsage reads all phase token usage from file
func (bo *BaseOrchestrator) GetAllPhaseTokenUsage() map[string]map[string]*ModelTokenUsage {
	ctx := context.Background()
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		return make(map[string]map[string]*ModelTokenUsage)
	}

	var tokenFile *PhaseTokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		return make(map[string]map[string]*ModelTokenUsage)
	}

	if tokenFile.ByPhaseAndModel == nil {
		return make(map[string]map[string]*ModelTokenUsage)
	}

	// Return a copy to avoid external modifications
	result := make(map[string]map[string]*ModelTokenUsage)
	for phase, modelMap := range tokenFile.ByPhaseAndModel {
		result[phase] = make(map[string]*ModelTokenUsage)
		for modelID, usage := range modelMap {
			result[phase][modelID] = &ModelTokenUsage{
				Provider:            usage.Provider,
				InputTokens:         usage.InputTokens,
				OutputTokens:        usage.OutputTokens,
				InputTokensM:        usage.InputTokensM,
				OutputTokensM:       usage.OutputTokensM,
				CacheTokens:         usage.CacheTokens,
				CacheTokensM:        usage.CacheTokensM,
				ReasoningTokens:     usage.ReasoningTokens,
				ReasoningTokensM:    usage.ReasoningTokensM,
				LLMCallCount:        usage.LLMCallCount,
				InputCost:           usage.InputCost,
				OutputCost:          usage.OutputCost,
				ReasoningCost:       usage.ReasoningCost,
				CacheCost:           usage.CacheCost,
				TotalCost:           usage.TotalCost,
				ContextWindowUsage:  usage.ContextWindowUsage,
				ModelContextWindow:  usage.ModelContextWindow,
				ContextUsagePercent: usage.ContextUsagePercent,
			}
		}
	}

	return result
}
