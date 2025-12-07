package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"mcpagent/events"
)

// AccumulateStepTokens accumulates token usage for a specific step
func (bo *BaseOrchestrator) AccumulateStepTokens(phase string, step int, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens int, llmCallCount int, cacheDiscount float64) {
	bo.stepTokenMutex.Lock()
	defer bo.stepTokenMutex.Unlock()

	key := fmt.Sprintf("%s:%d", phase, step)
	usage, exists := bo.stepTokenAccumulator[key]
	if !exists {
		usage = &StepTokenUsage{}
		bo.stepTokenAccumulator[key] = usage
	}

	usage.PromptTokens += promptTokens
	usage.CompletionTokens += completionTokens
	usage.TotalTokens += totalTokens
	usage.CacheTokens += cacheTokens
	usage.ReasoningTokens += reasoningTokens
	usage.LLMCallCount += llmCallCount
	if cacheTokens > 0 {
		usage.CacheEnabledCallCount++
	}
	usage.CacheDiscountSum += cacheDiscount
}

// GetStepTokenUsage retrieves accumulated token usage for a specific step
func (bo *BaseOrchestrator) GetStepTokenUsage(phase string, step int) *StepTokenUsage {
	bo.stepTokenMutex.RLock()
	defer bo.stepTokenMutex.RUnlock()

	key := fmt.Sprintf("%s:%d", phase, step)
	usage, exists := bo.stepTokenAccumulator[key]
	if !exists {
		return &StepTokenUsage{} // Return zero values if step not found
	}

	// Return a copy to avoid race conditions
	return &StepTokenUsage{
		PromptTokens:          usage.PromptTokens,
		CompletionTokens:      usage.CompletionTokens,
		TotalTokens:           usage.TotalTokens,
		CacheTokens:           usage.CacheTokens,
		ReasoningTokens:       usage.ReasoningTokens,
		LLMCallCount:          usage.LLMCallCount,
		CacheEnabledCallCount: usage.CacheEnabledCallCount,
		CacheDiscountSum:      usage.CacheDiscountSum,
	}
}

// EmitStepTokenUsage emits a step token usage summary event and optionally clears the accumulated data
func (bo *BaseOrchestrator) EmitStepTokenUsage(ctx context.Context, phase string, step int, stepTitle string, clearAfterEmit bool) {
	bo.stepTokenMutex.Lock()
	defer bo.stepTokenMutex.Unlock()

	key := fmt.Sprintf("%s:%d", phase, step)
	usage, exists := bo.stepTokenAccumulator[key]
	if !exists {
		bo.GetLogger().Warnf("⚠️ No token usage data found for step %s:%d", phase, step)
		return
	}

	// Create and emit step token usage event
	stepTokenEvent := events.NewStepTokenUsageEvent(
		phase,
		step,
		stepTitle,
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
		usage.CacheTokens,
		usage.ReasoningTokens,
		usage.LLMCallCount,
		usage.CacheEnabledCallCount,
	)

	bo.emitEvent(ctx, events.StepTokenUsage, stepTokenEvent)

	// Store step title for persistence
	if stepTitle != "" {
		bo.stepTokenTitles[key] = stepTitle
	}

	bo.GetLogger().Infof("📊 Emitted step token usage for %s:%d - Total: %d tokens (Prompt: %d, Completion: %d, Cache: %d, Reasoning: %d, Calls: %d)",
		phase, step, usage.TotalTokens, usage.PromptTokens, usage.CompletionTokens, usage.CacheTokens, usage.ReasoningTokens, usage.LLMCallCount)

	// Clear accumulated data if requested
	if clearAfterEmit {
		delete(bo.stepTokenAccumulator, key)
		delete(bo.stepTokenTitles, key)
	}
}

// AccumulateModelTokens accumulates token usage for a specific model
func (bo *BaseOrchestrator) AccumulateModelTokens(modelID, provider string, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens int, llmCallCount int) {
	bo.modelTokenMutex.Lock()
	defer bo.modelTokenMutex.Unlock()

	usage, exists := bo.modelTokenAccumulator[modelID]
	if !exists {
		usage = &ModelTokenUsageInternal{
			Provider: provider,
		}
		bo.modelTokenAccumulator[modelID] = usage
	}

	usage.PromptTokens += promptTokens
	usage.CompletionTokens += completionTokens
	usage.TotalTokens += totalTokens
	usage.CacheTokens += cacheTokens
	usage.ReasoningTokens += reasoningTokens
	usage.LLMCallCount += llmCallCount
}

// GetModelTokenUsage retrieves accumulated token usage for a specific model (in millions)
func (bo *BaseOrchestrator) GetModelTokenUsage(modelID string) *ModelTokenUsage {
	bo.modelTokenMutex.RLock()
	defer bo.modelTokenMutex.RUnlock()

	usage, exists := bo.modelTokenAccumulator[modelID]
	if !exists {
		return &ModelTokenUsage{} // Return zero values if model not found
	}

	// Convert from raw integers to millions
	const tokensPerMillion = 1_000_000.0
	return &ModelTokenUsage{
		Provider:         usage.Provider,
		PromptTokens:     float64(usage.PromptTokens) / tokensPerMillion,
		CompletionTokens: float64(usage.CompletionTokens) / tokensPerMillion,
		TotalTokens:      float64(usage.TotalTokens) / tokensPerMillion,
		CacheTokens:      float64(usage.CacheTokens) / tokensPerMillion,
		ReasoningTokens:  float64(usage.ReasoningTokens) / tokensPerMillion,
		LLMCallCount:     usage.LLMCallCount,
	}
}

// GetAllModelTokenUsage retrieves all accumulated model token usage (in millions)
func (bo *BaseOrchestrator) GetAllModelTokenUsage() map[string]*ModelTokenUsage {
	bo.modelTokenMutex.RLock()
	defer bo.modelTokenMutex.RUnlock()

	const tokensPerMillion = 1_000_000.0
	result := make(map[string]*ModelTokenUsage)
	for modelID, usage := range bo.modelTokenAccumulator {
		result[modelID] = &ModelTokenUsage{
			Provider:         usage.Provider,
			PromptTokens:     float64(usage.PromptTokens) / tokensPerMillion,
			CompletionTokens: float64(usage.CompletionTokens) / tokensPerMillion,
			TotalTokens:      float64(usage.TotalTokens) / tokensPerMillion,
			CacheTokens:      float64(usage.CacheTokens) / tokensPerMillion,
			ReasoningTokens:  float64(usage.ReasoningTokens) / tokensPerMillion,
			LLMCallCount:     usage.LLMCallCount,
		}
	}
	return result
}

// PersistTokenUsage saves accumulated token usage to token_usage.json in the iteration folder
func (bo *BaseOrchestrator) PersistTokenUsage(ctx context.Context, iterationFolder string) error {
	if iterationFolder == "" {
		bo.GetLogger().Warnf("⚠️ No iteration folder provided, skipping token usage persistence")
		return nil
	}

	// Build file path: runs/{iterationFolder}/token_usage.json
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", iterationFolder, "token_usage.json")

	bo.GetLogger().Infof("💾 Persisting token usage to: %s", filePath)

	// Lock both mutexes to get a consistent snapshot
	bo.stepTokenMutex.RLock()
	bo.modelTokenMutex.RLock()

	// Build the token usage file structure
	tokenFile := &TokenUsageFile{
		UpdatedAt:  time.Now(),
		ByModel:    make(map[string]*ModelTokenUsage),
		ByStep:     make(map[string]*StepTokenSummary),
		ByStepType: make(map[string]*StepTypeTokenUsage),
	}

	// Copy step token titles while holding lock
	stepTitlesCopy := make(map[string]string)
	for k, v := range bo.stepTokenTitles {
		stepTitlesCopy[k] = v
	}

	// Copy model token usage, converting from raw integers to millions
	const tokensPerMillion = 1_000_000.0
	for modelID, usage := range bo.modelTokenAccumulator {
		tokenFile.ByModel[modelID] = &ModelTokenUsage{
			Provider:         usage.Provider,
			PromptTokens:     float64(usage.PromptTokens) / tokensPerMillion,
			CompletionTokens: float64(usage.CompletionTokens) / tokensPerMillion,
			TotalTokens:      float64(usage.TotalTokens) / tokensPerMillion,
			CacheTokens:      float64(usage.CacheTokens) / tokensPerMillion,
			ReasoningTokens:  float64(usage.ReasoningTokens) / tokensPerMillion,
			LLMCallCount:     usage.LLMCallCount,
		}
	}

	// Copy step token usage, converting from raw integers to millions
	// Also aggregate by step type
	stepTypeAggregator := make(map[string]*StepTypeTokenUsage)
	for stepKey, usage := range bo.stepTokenAccumulator {
		stepTitle := stepTitlesCopy[stepKey]
		// Extract step type (phase) from key format "phase:step"
		stepType := ""
		if parts := strings.Split(stepKey, ":"); len(parts) > 0 {
			stepType = parts[0] // e.g., "execution", "validation", "learning"
		}

		// Convert to millions
		promptTokens := float64(usage.PromptTokens) / tokensPerMillion
		completionTokens := float64(usage.CompletionTokens) / tokensPerMillion
		totalTokens := float64(usage.TotalTokens) / tokensPerMillion
		cacheTokens := float64(usage.CacheTokens) / tokensPerMillion
		reasoningTokens := float64(usage.ReasoningTokens) / tokensPerMillion

		// Store individual step
		tokenFile.ByStep[stepKey] = &StepTokenSummary{
			StepType:         stepType,
			StepTitle:        stepTitle,
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
			CacheTokens:      cacheTokens,
			ReasoningTokens:  reasoningTokens,
			LLMCallCount:     usage.LLMCallCount,
		}

		// Aggregate by step type
		if stepType != "" {
			if agg, exists := stepTypeAggregator[stepType]; exists {
				agg.PromptTokens += promptTokens
				agg.CompletionTokens += completionTokens
				agg.TotalTokens += totalTokens
				agg.CacheTokens += cacheTokens
				agg.ReasoningTokens += reasoningTokens
				agg.LLMCallCount += usage.LLMCallCount
			} else {
				stepTypeAggregator[stepType] = &StepTypeTokenUsage{
					StepType:         stepType,
					PromptTokens:     promptTokens,
					CompletionTokens: completionTokens,
					TotalTokens:      totalTokens,
					CacheTokens:      cacheTokens,
					ReasoningTokens:  reasoningTokens,
					LLMCallCount:     usage.LLMCallCount,
				}
			}
		}
	}

	// Copy aggregated step type data
	tokenFile.ByStepType = stepTypeAggregator

	bo.stepTokenMutex.RUnlock()
	bo.modelTokenMutex.RUnlock()

	// Set created_at timestamp
	// Note: This is a new feature - we write fresh data from memory accumulator each time
	// No backward compatibility needed - memory accumulator is the source of truth
	tokenFile.CreatedAt = time.Now()

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(tokenFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token usage data: %w", err)
	}

	// Write to file
	if err := bo.WriteWorkspaceFile(ctx, filePath, string(jsonData)); err != nil {
		return fmt.Errorf("failed to write token usage file: %w", err)
	}

	// Calculate total for logging purposes only (convert back from millions)
	totalTokens := 0.0
	for _, usage := range tokenFile.ByModel {
		totalTokens += usage.TotalTokens
	}

	// Log step type breakdown
	stepTypeBreakdown := make([]string, 0, len(tokenFile.ByStepType))
	for stepType, usage := range tokenFile.ByStepType {
		stepTypeBreakdown = append(stepTypeBreakdown, fmt.Sprintf("%s:%.3fM", stepType, usage.TotalTokens))
	}

	bo.GetLogger().Infof("✅ Persisted token usage: %.3fM tokens across %d models, %d steps, %d step types (%s)",
		totalTokens, len(tokenFile.ByModel), len(tokenFile.ByStep), len(tokenFile.ByStepType), strings.Join(stepTypeBreakdown, ", "))

	return nil
}
