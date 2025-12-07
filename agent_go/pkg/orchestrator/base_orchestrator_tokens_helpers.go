package orchestrator

import (
	"mcpagent/events"
)

// extractCacheTokens extracts cache tokens from a TokenUsageEvent's GenerationInfo
// Returns 0 if not available or cannot be extracted
// Checks multiple sources: cumulative_cache_tokens, CachedContentTokens, and Anthropic cache fields
// Note: ExtractTokenUsageWithCacheInfo copies Additional map fields to top level, so we check both
func extractCacheTokens(tokenEvent *events.TokenUsageEvent) int {
	if tokenEvent.GenerationInfo == nil {
		return 0
	}

	totalCacheTokens := 0

	// Priority 1: Check for cumulative_cache_tokens (from conversation end event)
	// This is the cumulative total, so return it directly if found
	if ct, ok := tokenEvent.GenerationInfo["cumulative_cache_tokens"].(int); ok {
		return ct
	}
	if ct, ok := tokenEvent.GenerationInfo["cumulative_cache_tokens"].(float64); ok {
		return int(ct)
	}

	// Priority 2: Check for CachedContentTokens (Anthropic field, copied to top level)
	if cachedContent, ok := tokenEvent.GenerationInfo["CachedContentTokens"].(int); ok {
		totalCacheTokens += cachedContent
	} else if cachedContent, ok := tokenEvent.GenerationInfo["CachedContentTokens"].(float64); ok {
		totalCacheTokens += int(cachedContent)
	}

	// Priority 3: Check top-level fields (Additional map fields are copied to top level by ExtractTokenUsageWithCacheInfo)
	// Check CacheReadInputTokens (Anthropic field)
	if cacheRead, ok := tokenEvent.GenerationInfo["CacheReadInputTokens"]; ok {
		if cacheReadInt, ok := cacheRead.(int); ok {
			totalCacheTokens += cacheReadInt
		} else if cacheReadFloat, ok := cacheRead.(float64); ok {
			totalCacheTokens += int(cacheReadFloat)
		}
	}

	// Check CacheCreationInputTokens (Anthropic field)
	if cacheCreate, ok := tokenEvent.GenerationInfo["CacheCreationInputTokens"]; ok {
		if cacheCreateInt, ok := cacheCreate.(int); ok {
			totalCacheTokens += cacheCreateInt
		} else if cacheCreateFloat, ok := cacheCreate.(float64); ok {
			totalCacheTokens += int(cacheCreateFloat)
		}
	}

	// Priority 4: Also check Additional map (in case fields weren't copied to top level)
	if additional, ok := tokenEvent.GenerationInfo["Additional"].(map[string]interface{}); ok {
		// Check CacheReadInputTokens
		if cacheRead, ok := additional["CacheReadInputTokens"]; ok {
			if cacheReadInt, ok := cacheRead.(int); ok {
				totalCacheTokens += cacheReadInt
			} else if cacheReadFloat, ok := cacheRead.(float64); ok {
				totalCacheTokens += int(cacheReadFloat)
			}
		}
		// Check CacheCreationInputTokens
		if cacheCreate, ok := additional["CacheCreationInputTokens"]; ok {
			if cacheCreateInt, ok := cacheCreate.(int); ok {
				totalCacheTokens += cacheCreateInt
			} else if cacheCreateFloat, ok := cacheCreate.(float64); ok {
				totalCacheTokens += int(cacheCreateFloat)
			}
		}
	}

	return totalCacheTokens
}

// mergeModelTokenUsage merges existing and current model token usage (additive)
func mergeModelTokenUsage(existing, current *ModelTokenUsage) *ModelTokenUsage {
	return &ModelTokenUsage{
		Provider:         current.Provider,
		PromptTokens:     existing.PromptTokens + current.PromptTokens,
		CompletionTokens: existing.CompletionTokens + current.CompletionTokens,
		TotalTokens:      existing.TotalTokens + current.TotalTokens,
		CacheTokens:      existing.CacheTokens + current.CacheTokens,
		ReasoningTokens:  existing.ReasoningTokens + current.ReasoningTokens,
		LLMCallCount:     existing.LLMCallCount + current.LLMCallCount,
	}
}

// mergeStepTokenUsage merges existing and current step token usage (additive)
func mergeStepTokenUsage(existing, current *StepTokenSummary) *StepTokenSummary {
	// Prefer current step type and title if available, otherwise use existing
	stepType := current.StepType
	if stepType == "" {
		stepType = existing.StepType
	}
	stepTitle := current.StepTitle
	if stepTitle == "" {
		stepTitle = existing.StepTitle
	}
	return &StepTokenSummary{
		StepType:         stepType,
		StepTitle:        stepTitle,
		PromptTokens:     existing.PromptTokens + current.PromptTokens,
		CompletionTokens: existing.CompletionTokens + current.CompletionTokens,
		TotalTokens:      existing.TotalTokens + current.TotalTokens,
		CacheTokens:      existing.CacheTokens + current.CacheTokens,
		ReasoningTokens:  existing.ReasoningTokens + current.ReasoningTokens,
		LLMCallCount:     existing.LLMCallCount + current.LLMCallCount,
	}
}

// mergeModelTokenUsageMaps merges two maps of model token usage (additive)
func mergeModelTokenUsageMaps(existing, current map[string]*ModelTokenUsage) map[string]*ModelTokenUsage {
	result := make(map[string]*ModelTokenUsage)

	// Copy all existing models
	for modelID, usage := range existing {
		result[modelID] = usage
	}

	// Merge or add current models
	for modelID, currentUsage := range current {
		if existingUsage, exists := result[modelID]; exists {
			result[modelID] = mergeModelTokenUsage(existingUsage, currentUsage)
		} else {
			result[modelID] = currentUsage
		}
	}

	return result
}

// mergeStepTokenUsageMaps merges two maps of step token usage (additive)
func mergeStepTokenUsageMaps(existing, current map[string]*StepTokenSummary) map[string]*StepTokenSummary {
	result := make(map[string]*StepTokenSummary)

	// Copy all existing steps
	for stepKey, usage := range existing {
		result[stepKey] = usage
	}

	// Merge or add current steps
	for stepKey, currentUsage := range current {
		if existingUsage, exists := result[stepKey]; exists {
			result[stepKey] = mergeStepTokenUsage(existingUsage, currentUsage)
		} else {
			result[stepKey] = currentUsage
		}
	}

	return result
}
