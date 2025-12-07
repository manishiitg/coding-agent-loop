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
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ No token usage data found for step %s:%d", phase, step))
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

	// Removed verbose logging

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
// It reads existing token data from the file, merges it with current in-memory accumulators,
// and preserves the original CreatedAt timestamp.
func (bo *BaseOrchestrator) PersistTokenUsage(ctx context.Context, iterationFolder string) error {
	if iterationFolder == "" {
		// Removed verbose logging
		return nil
	}

	// Build file path: runs/{iterationFolder}/token_usage.json
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", iterationFolder, "token_usage.json")

	// Removed verbose logging

	// Read existing token usage file if it exists
	var existingFile *TokenUsageFile
	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err == nil && existingContent != "" {
		// File exists, try to parse it
		if err := json.Unmarshal([]byte(existingContent), &existingFile); err != nil {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse existing token_usage.json: %v (will create new file)", err))
			existingFile = nil
		} else {
			// Removed verbose logging
		}
	} else if err != nil {
		// File doesn't exist or error reading - this is expected for new files
		// Only log if it's not a "file not found" type error
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			// Removed verbose logging
		}
		existingFile = nil
	}

	// Lock both mutexes to get a consistent snapshot
	bo.stepTokenMutex.RLock()
	bo.modelTokenMutex.RLock()

	// Build the token usage file structure
	// Start with existing data if available, otherwise create new
	tokenFile := &TokenUsageFile{
		UpdatedAt:  time.Now(),
		ByModel:    make(map[string]*ModelTokenUsage),
		ByStep:     make(map[string]*StepTokenSummary),
		ByStepType: make(map[string]*StepTypeTokenUsage),
	}

	// Preserve CreatedAt from existing file, or set to now if new file
	if existingFile != nil {
		tokenFile.CreatedAt = existingFile.CreatedAt
		// Initialize maps from existing data
		if existingFile.ByModel != nil {
			for k, v := range existingFile.ByModel {
				// Deep copy to avoid modifying original
				tokenFile.ByModel[k] = &ModelTokenUsage{
					Provider:         v.Provider,
					PromptTokens:     v.PromptTokens,
					CompletionTokens: v.CompletionTokens,
					TotalTokens:      v.TotalTokens,
					CacheTokens:      v.CacheTokens,
					ReasoningTokens:  v.ReasoningTokens,
					LLMCallCount:     v.LLMCallCount,
				}
			}
		}
		if existingFile.ByStep != nil {
			for k, v := range existingFile.ByStep {
				// Deep copy to avoid modifying original
				tokenFile.ByStep[k] = &StepTokenSummary{
					StepType:         v.StepType,
					StepTitle:        v.StepTitle,
					PromptTokens:     v.PromptTokens,
					CompletionTokens: v.CompletionTokens,
					TotalTokens:      v.TotalTokens,
					CacheTokens:      v.CacheTokens,
					ReasoningTokens:  v.ReasoningTokens,
					LLMCallCount:     v.LLMCallCount,
				}
			}
		}
		if existingFile.ByStepType != nil {
			for k, v := range existingFile.ByStepType {
				// Deep copy to avoid modifying original
				tokenFile.ByStepType[k] = &StepTypeTokenUsage{
					StepType:         v.StepType,
					PromptTokens:     v.PromptTokens,
					CompletionTokens: v.CompletionTokens,
					TotalTokens:      v.TotalTokens,
					CacheTokens:      v.CacheTokens,
					ReasoningTokens:  v.ReasoningTokens,
					LLMCallCount:     v.LLMCallCount,
				}
			}
		}
	} else {
		// New file - set CreatedAt to now
		tokenFile.CreatedAt = time.Now()
	}

	// Copy step token titles while holding lock
	stepTitlesCopy := make(map[string]string)
	for k, v := range bo.stepTokenTitles {
		stepTitlesCopy[k] = v
	}

	// Merge model token usage from in-memory accumulators (convert from raw integers to millions)
	const tokensPerMillion = 1_000_000.0
	for modelID, usage := range bo.modelTokenAccumulator {
		newTokens := &ModelTokenUsage{
			Provider:         usage.Provider,
			PromptTokens:     float64(usage.PromptTokens) / tokensPerMillion,
			CompletionTokens: float64(usage.CompletionTokens) / tokensPerMillion,
			TotalTokens:      float64(usage.TotalTokens) / tokensPerMillion,
			CacheTokens:      float64(usage.CacheTokens) / tokensPerMillion,
			ReasoningTokens:  float64(usage.ReasoningTokens) / tokensPerMillion,
			LLMCallCount:     usage.LLMCallCount,
		}

		// Merge with existing data if present
		if existing, exists := tokenFile.ByModel[modelID]; exists {
			existing.PromptTokens += newTokens.PromptTokens
			existing.CompletionTokens += newTokens.CompletionTokens
			existing.TotalTokens += newTokens.TotalTokens
			existing.CacheTokens += newTokens.CacheTokens
			existing.ReasoningTokens += newTokens.ReasoningTokens
			existing.LLMCallCount += newTokens.LLMCallCount
			// Preserve provider from existing (should be same, but prefer existing)
		} else {
			tokenFile.ByModel[modelID] = newTokens
		}
	}

	// Merge step token usage from in-memory accumulators (convert from raw integers to millions)
	// Also aggregate by step type
	stepTypeAggregator := make(map[string]*StepTypeTokenUsage)
	// Initialize aggregator from existing step types
	for stepType, usage := range tokenFile.ByStepType {
		stepTypeAggregator[stepType] = &StepTypeTokenUsage{
			StepType:         usage.StepType,
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
			CacheTokens:      usage.CacheTokens,
			ReasoningTokens:  usage.ReasoningTokens,
			LLMCallCount:     usage.LLMCallCount,
		}
	}

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

		// Merge individual step data
		if existing, exists := tokenFile.ByStep[stepKey]; exists {
			// Merge with existing step data
			existing.PromptTokens += promptTokens
			existing.CompletionTokens += completionTokens
			existing.TotalTokens += totalTokens
			existing.CacheTokens += cacheTokens
			existing.ReasoningTokens += reasoningTokens
			existing.LLMCallCount += usage.LLMCallCount
			// Update step title if we have a new one (prefer newer title)
			if stepTitle != "" {
				existing.StepTitle = stepTitle
			}
		} else {
			// New step - create entry
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

	// Update aggregated step type data (recalculate from all steps for accuracy)
	// Clear and rebuild from scratch to ensure consistency
	tokenFile.ByStepType = make(map[string]*StepTypeTokenUsage)
	for _, stepSummary := range tokenFile.ByStep {
		if stepSummary.StepType == "" {
			continue
		}
		if agg, exists := tokenFile.ByStepType[stepSummary.StepType]; exists {
			agg.PromptTokens += stepSummary.PromptTokens
			agg.CompletionTokens += stepSummary.CompletionTokens
			agg.TotalTokens += stepSummary.TotalTokens
			agg.CacheTokens += stepSummary.CacheTokens
			agg.ReasoningTokens += stepSummary.ReasoningTokens
			agg.LLMCallCount += stepSummary.LLMCallCount
		} else {
			tokenFile.ByStepType[stepSummary.StepType] = &StepTypeTokenUsage{
				StepType:         stepSummary.StepType,
				PromptTokens:     stepSummary.PromptTokens,
				CompletionTokens: stepSummary.CompletionTokens,
				TotalTokens:      stepSummary.TotalTokens,
				CacheTokens:      stepSummary.CacheTokens,
				ReasoningTokens:  stepSummary.ReasoningTokens,
				LLMCallCount:     stepSummary.LLMCallCount,
			}
		}
	}

	bo.stepTokenMutex.RUnlock()
	bo.modelTokenMutex.RUnlock()

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

	// Removed verbose logging

	return nil
}
