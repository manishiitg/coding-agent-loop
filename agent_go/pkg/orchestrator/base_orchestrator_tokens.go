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

// formatTokensM formats raw token count as string with "M" suffix (e.g., "17.016M")
func formatTokensM(tokens int) string {
	if tokens == 0 {
		return "0.000M"
	}
	millions := float64(tokens) / 1_000_000.0
	return fmt.Sprintf("%.3fM", millions)
}

// GetStepTokenUsage reads token usage from file for a specific step
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
	stepSummary, exists := tokenFile.ByStep[stepKey]
	if !exists {
		return &StepTokenUsage{}
	}

	return &StepTokenUsage{
		InputTokens:     stepSummary.InputTokens,
		OutputTokens:    stepSummary.OutputTokens,
		CacheTokens:     stepSummary.CacheTokens,
		ReasoningTokens: stepSummary.ReasoningTokens,
		LLMCallCount:    stepSummary.LLMCallCount,
	}
}

// EmitStepTokenUsage reads token usage from file and emits a step token usage summary event
func (bo *BaseOrchestrator) EmitStepTokenUsage(ctx context.Context, phase string, step int, stepTitle string, clearAfterEmit bool) {
	if bo.iterationFolder == "" {
		bo.GetLogger().Warnf("⚠️ No iteration folder, cannot read token usage for step %s:%d", phase, step)
		return
	}

	// Read token usage from file
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", bo.iterationFolder, "token_usage.json")

	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err != nil || existingContent == "" {
		bo.GetLogger().Warnf("⚠️ No token usage file found for step %s:%d", phase, step)
		return
	}

	var tokenFile *TokenUsageFile
	if err := json.Unmarshal([]byte(existingContent), &tokenFile); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to parse token usage file: %v", err)
		return
	}

	// Find step data
	stepKey := fmt.Sprintf("%s:%d", phase, step)
	stepSummary, exists := tokenFile.ByStep[stepKey]
	if !exists {
		bo.GetLogger().Warnf("⚠️ No token usage data found for step %s:%d", phase, step)
		return
	}

	// Calculate total for event (events still use old field names)
	totalTokens := stepSummary.InputTokens + stepSummary.OutputTokens

	// Create and emit step token usage event
	stepTokenEvent := events.NewStepTokenUsageEvent(
		phase,
		step,
		stepTitle,
		stepSummary.InputTokens,  // prompt_tokens in event
		stepSummary.OutputTokens, // completion_tokens in event
		totalTokens,              // total_tokens in event
		stepSummary.CacheTokens,
		stepSummary.ReasoningTokens,
		stepSummary.LLMCallCount,
		0, // CacheEnabledCallCount not stored in file (could be calculated if needed)
	)

	bo.emitEvent(ctx, events.StepTokenUsage, stepTokenEvent)

	bo.GetLogger().Infof("📊 Emitted step token usage for %s:%d - Input: %d, Output: %d, Cache: %d, Reasoning: %d, Calls: %d",
		phase, step, stepSummary.InputTokens, stepSummary.OutputTokens, stepSummary.CacheTokens, stepSummary.ReasoningTokens, stepSummary.LLMCallCount)
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

// PersistTokenUsage saves token usage directly to token_usage.json in the iteration folder
// It reads existing token data from the file, merges the new token data, and writes back.
// The file is the single source of truth - no in-memory accumulation.
func (bo *BaseOrchestrator) PersistTokenUsage(ctx context.Context, iterationFolder string,
	stepTokenData *StepTokenData, modelTokenData *ModelTokenData) error {
	if iterationFolder == "" {
		bo.GetLogger().Warnf("⚠️ No iteration folder provided, skipping token usage persistence")
		return nil
	}

	// Build file path: runs/{iterationFolder}/token_usage.json
	workspacePath := bo.GetWorkspacePath()
	filePath := filepath.Join(workspacePath, "runs", iterationFolder, "token_usage.json")

	bo.GetLogger().Debugf("💾 Persisting token usage to: %s", filePath)

	// Read existing token usage file if it exists
	var existingFile *TokenUsageFile
	existingContent, err := bo.ReadWorkspaceFile(ctx, filePath)
	if err == nil && existingContent != "" {
		// File exists, try to parse it
		if err := json.Unmarshal([]byte(existingContent), &existingFile); err != nil {
			bo.GetLogger().Warnf("⚠️ Failed to parse existing token_usage.json: %v (will create new file)", err)
			existingFile = nil
		}
	} else if err != nil {
		// File doesn't exist or error reading - this is expected for new files
		// Only log if it's not a "file not found" type error
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			bo.GetLogger().Debugf("📋 Token usage file does not exist yet (will create new): %s", filePath)
		}
		existingFile = nil
	}

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
		if existingFile.ByStep != nil {
			for k, v := range existingFile.ByStep {
				tokenFile.ByStep[k] = &StepTokenSummary{
					StepType:         v.StepType,
					StepTitle:        v.StepTitle,
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
		if existingFile.ByStepType != nil {
			for k, v := range existingFile.ByStepType {
				tokenFile.ByStepType[k] = &StepTypeTokenUsage{
					StepType:         v.StepType,
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

	// Merge new step token data if provided
	if stepTokenData != nil {
		stepKey := fmt.Sprintf("%s:%d", stepTokenData.Phase, stepTokenData.Step)
		if existing, exists := tokenFile.ByStep[stepKey]; exists {
			// Merge with existing step data (add raw integers)
			existing.InputTokens += stepTokenData.InputTokens
			existing.OutputTokens += stepTokenData.OutputTokens
			existing.CacheTokens += stepTokenData.CacheTokens
			existing.ReasoningTokens += stepTokenData.ReasoningTokens
			existing.LLMCallCount += stepTokenData.LLMCallCount
			// Recalculate formatted strings
			existing.InputTokensM = formatTokensM(existing.InputTokens)
			existing.OutputTokensM = formatTokensM(existing.OutputTokens)
			existing.CacheTokensM = formatTokensM(existing.CacheTokens)
			existing.ReasoningTokensM = formatTokensM(existing.ReasoningTokens)
			// Update step title if provided
			if stepTokenData.StepTitle != "" {
				existing.StepTitle = stepTokenData.StepTitle
			}
		} else {
			// New step - create entry
			tokenFile.ByStep[stepKey] = &StepTokenSummary{
				StepType:         stepTokenData.Phase,
				StepTitle:        stepTokenData.StepTitle,
				InputTokens:      stepTokenData.InputTokens,
				OutputTokens:     stepTokenData.OutputTokens,
				InputTokensM:     formatTokensM(stepTokenData.InputTokens),
				OutputTokensM:    formatTokensM(stepTokenData.OutputTokens),
				CacheTokens:      stepTokenData.CacheTokens,
				CacheTokensM:     formatTokensM(stepTokenData.CacheTokens),
				ReasoningTokens:  stepTokenData.ReasoningTokens,
				ReasoningTokensM: formatTokensM(stepTokenData.ReasoningTokens),
				LLMCallCount:     stepTokenData.LLMCallCount,
			}
		}
	}

	// Recalculate aggregated step type data from all steps
	tokenFile.ByStepType = make(map[string]*StepTypeTokenUsage)
	for _, stepSummary := range tokenFile.ByStep {
		if stepSummary.StepType == "" {
			continue
		}
		if agg, exists := tokenFile.ByStepType[stepSummary.StepType]; exists {
			// Add raw integers
			agg.InputTokens += stepSummary.InputTokens
			agg.OutputTokens += stepSummary.OutputTokens
			agg.CacheTokens += stepSummary.CacheTokens
			agg.ReasoningTokens += stepSummary.ReasoningTokens
			agg.LLMCallCount += stepSummary.LLMCallCount
			// Recalculate formatted strings
			agg.InputTokensM = formatTokensM(agg.InputTokens)
			agg.OutputTokensM = formatTokensM(agg.OutputTokens)
			agg.CacheTokensM = formatTokensM(agg.CacheTokens)
			agg.ReasoningTokensM = formatTokensM(agg.ReasoningTokens)
		} else {
			// New step type - create entry
			tokenFile.ByStepType[stepSummary.StepType] = &StepTypeTokenUsage{
				StepType:         stepSummary.StepType,
				InputTokens:      stepSummary.InputTokens,
				OutputTokens:     stepSummary.OutputTokens,
				InputTokensM:     formatTokensM(stepSummary.InputTokens),
				OutputTokensM:    formatTokensM(stepSummary.OutputTokens),
				CacheTokens:      stepSummary.CacheTokens,
				CacheTokensM:     formatTokensM(stepSummary.CacheTokens),
				ReasoningTokens:  stepSummary.ReasoningTokens,
				ReasoningTokensM: formatTokensM(stepSummary.ReasoningTokens),
				LLMCallCount:     stepSummary.LLMCallCount,
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

	bo.GetLogger().Debugf("✅ Persisted token usage to file")

	return nil
}
