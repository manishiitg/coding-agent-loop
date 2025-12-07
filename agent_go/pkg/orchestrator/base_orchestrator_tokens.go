package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mcpagent/events"
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

// GetStepTokenUsage reads token usage from file for a specific step (aggregated across all models)
func (bo *BaseOrchestrator) GetStepTokenUsage(phase string, step int) *StepTokenUsage {
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

	stepKey := fmt.Sprintf("%s:%d", phase, step)
	modelMap, exists := tokenFile.ByStepAndModel[stepKey]
	if !exists || modelMap == nil {
		return &StepTokenUsage{}
	}

	// Aggregate across all models for this step
	result := &StepTokenUsage{}
	for _, modelUsage := range modelMap {
		result.InputTokens += modelUsage.InputTokens
		result.OutputTokens += modelUsage.OutputTokens
		result.CacheTokens += modelUsage.CacheTokens
		result.ReasoningTokens += modelUsage.ReasoningTokens
		result.LLMCallCount += modelUsage.LLMCallCount
	}

	return result
}

// EmitStepTokenUsage reads token usage from file and emits a step token usage summary event
// Aggregates tokens across all models used in the step
func (bo *BaseOrchestrator) EmitStepTokenUsage(ctx context.Context, phase string, step int, stepTitle string, clearAfterEmit bool) {
	if bo.iterationFolder == "" {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ No iteration folder, cannot read token usage for step %s:%d", phase, step))
		return
	}

	// Read token usage from file
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", bo.iterationFolder, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ No token usage file found for step %s:%d", phase, step))
		return
	}

	var tokenFile *TokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse token usage file: %v", err))
		return
	}

	// Find step data and aggregate across all models
	stepKey := fmt.Sprintf("%s:%d", phase, step)
	modelMap, exists := tokenFile.ByStepAndModel[stepKey]
	if !exists || modelMap == nil || len(modelMap) == 0 {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ No token usage data found for step %s:%d", phase, step))
		return
	}

	// Aggregate tokens across all models for this step
	var inputTokens, outputTokens, cacheTokens, reasoningTokens, llmCallCount int
	for _, modelUsage := range modelMap {
		inputTokens += modelUsage.InputTokens
		outputTokens += modelUsage.OutputTokens
		cacheTokens += modelUsage.CacheTokens
		reasoningTokens += modelUsage.ReasoningTokens
		llmCallCount += modelUsage.LLMCallCount
	}

	// Calculate total for event
	totalTokens := inputTokens + outputTokens

	// Create and emit step token usage event
	stepTokenEvent := events.NewStepTokenUsageEvent(
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
	)

	bo.emitEvent(ctx, events.StepTokenUsage, stepTokenEvent)

	bo.GetLogger().Info(fmt.Sprintf("📊 Emitted step token usage for %s:%d - Input: %d, Output: %d, Cache: %d, Reasoning: %d, Calls: %d",
		phase, step, inputTokens, outputTokens, cacheTokens, reasoningTokens, llmCallCount))
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
func (bo *BaseOrchestrator) GetStepModelTokenUsage(phase string, step int, modelID string) *ModelTokenUsage {
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

	stepKey := fmt.Sprintf("%s:%d", phase, step)
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
func (bo *BaseOrchestrator) GetStepModels(phase string, step int) map[string]*ModelTokenUsage {
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

	stepKey := fmt.Sprintf("%s:%d", phase, step)
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
			Provider:         usage.Provider,
			InputTokens:      usage.InputTokens,
			OutputTokens:     usage.OutputTokens,
			InputTokensM:     usage.InputTokensM,
			OutputTokensM:    usage.OutputTokensM,
			CacheTokens:      usage.CacheTokens,
			CacheTokensM:     usage.CacheTokensM,
			ReasoningTokens:  usage.ReasoningTokens,
			ReasoningTokensM: usage.ReasoningTokensM,
			LLMCallCount:     usage.LLMCallCount,
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
					Provider:         v.Provider,
					InputTokens:      v.InputTokens,
					OutputTokens:     v.OutputTokens,
					InputTokensM:     v.InputTokensM,
					OutputTokensM:    v.OutputTokensM,
					CacheTokens:      v.CacheTokens,
					CacheTokensM:     v.CacheTokensM,
					ReasoningTokens:  v.ReasoningTokens,
					ReasoningTokensM: v.ReasoningTokensM,
					LLMCallCount:     v.LLMCallCount,
				}
			}
		}
		// Copy existing ByStepAndModel data if it exists (backward compatibility)
		if existingFile.ByStepAndModel != nil {
			for stepKey, modelMap := range existingFile.ByStepAndModel {
				tokenFile.ByStepAndModel[stepKey] = make(map[string]*ModelTokenUsage)
				for modelID, v := range modelMap {
					tokenFile.ByStepAndModel[stepKey][modelID] = &ModelTokenUsage{
						Provider:         v.Provider,
						InputTokens:      v.InputTokens,
						OutputTokens:     v.OutputTokens,
						InputTokensM:     v.InputTokensM,
						OutputTokensM:    v.OutputTokensM,
						CacheTokens:      v.CacheTokens,
						CacheTokensM:     v.CacheTokensM,
						ReasoningTokens:  v.ReasoningTokens,
						ReasoningTokensM: v.ReasoningTokensM,
						LLMCallCount:     v.LLMCallCount,
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
		if existing, exists := tokenFile.ByModel[modelTokenData.ModelID]; exists {
			// Merge with existing data (add raw integers)
			existing.InputTokens += modelTokenData.InputTokens
			existing.OutputTokens += modelTokenData.OutputTokens
			existing.CacheTokens += modelTokenData.CacheTokens
			existing.ReasoningTokens += modelTokenData.ReasoningTokens
			existing.LLMCallCount += modelTokenData.LLMCallCount
			// Recalculate formatted strings
			existing.InputTokensM = formatTokensM(existing.InputTokens)
			existing.OutputTokensM = formatTokensM(existing.OutputTokens)
			existing.CacheTokensM = formatTokensM(existing.CacheTokens)
			existing.ReasoningTokensM = formatTokensM(existing.ReasoningTokens)
		} else {
			// New model - create entry
			tokenFile.ByModel[modelTokenData.ModelID] = &ModelTokenUsage{
				Provider:         modelTokenData.Provider,
				InputTokens:      modelTokenData.InputTokens,
				OutputTokens:     modelTokenData.OutputTokens,
				InputTokensM:     formatTokensM(modelTokenData.InputTokens),
				OutputTokensM:    formatTokensM(modelTokenData.OutputTokens),
				CacheTokens:      modelTokenData.CacheTokens,
				CacheTokensM:     formatTokensM(modelTokenData.CacheTokens),
				ReasoningTokens:  modelTokenData.ReasoningTokens,
				ReasoningTokensM: formatTokensM(modelTokenData.ReasoningTokens),
				LLMCallCount:     modelTokenData.LLMCallCount,
			}
		}
	}

	// Store step+model token data if both stepTokenData and modelTokenData are provided
	if stepTokenData != nil && modelTokenData != nil {
		stepKey := fmt.Sprintf("%s:%d", stepTokenData.Phase, stepTokenData.Step)
		modelID := modelTokenData.ModelID

		// Initialize step map if it doesn't exist
		if tokenFile.ByStepAndModel[stepKey] == nil {
			tokenFile.ByStepAndModel[stepKey] = make(map[string]*ModelTokenUsage)
		}

		// Merge with existing model data for this step if it exists
		if existing, exists := tokenFile.ByStepAndModel[stepKey][modelID]; exists {
			// Merge with existing data (add raw integers)
			existing.InputTokens += modelTokenData.InputTokens
			existing.OutputTokens += modelTokenData.OutputTokens
			existing.CacheTokens += modelTokenData.CacheTokens
			existing.ReasoningTokens += modelTokenData.ReasoningTokens
			existing.LLMCallCount += modelTokenData.LLMCallCount
			// Recalculate formatted strings
			existing.InputTokensM = formatTokensM(existing.InputTokens)
			existing.OutputTokensM = formatTokensM(existing.OutputTokens)
			existing.CacheTokensM = formatTokensM(existing.CacheTokens)
			existing.ReasoningTokensM = formatTokensM(existing.ReasoningTokens)
		} else {
			// New model for this step - create entry
			tokenFile.ByStepAndModel[stepKey][modelID] = &ModelTokenUsage{
				Provider:         modelTokenData.Provider,
				InputTokens:      modelTokenData.InputTokens,
				OutputTokens:     modelTokenData.OutputTokens,
				InputTokensM:     formatTokensM(modelTokenData.InputTokens),
				OutputTokensM:    formatTokensM(modelTokenData.OutputTokens),
				CacheTokens:      modelTokenData.CacheTokens,
				CacheTokensM:     formatTokensM(modelTokenData.CacheTokens),
				ReasoningTokens:  modelTokenData.ReasoningTokens,
				ReasoningTokensM: formatTokensM(modelTokenData.ReasoningTokens),
				LLMCallCount:     modelTokenData.LLMCallCount,
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
