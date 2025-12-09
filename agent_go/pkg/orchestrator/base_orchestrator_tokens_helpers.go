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

// extractLLMCallCount extracts LLM call count from a TokenUsageEvent's GenerationInfo
// Returns 1 if not available (fallback for single-call events like smart routing)
// For conversation end events, this returns the cumulative call count
func extractLLMCallCount(tokenEvent *events.TokenUsageEvent) int {
	if tokenEvent.GenerationInfo == nil {
		return 1 // Default to 1 for single-call events (smart routing, etc.)
	}

	// Check for cumulative_llm_call_count (from conversation end event)
	if count, ok := tokenEvent.GenerationInfo["llm_call_count"].(int); ok {
		return count
	}
	if count, ok := tokenEvent.GenerationInfo["llm_call_count"].(float64); ok {
		return int(count)
	}

	// Fallback: return 1 for single-call events (smart routing, etc.)
	return 1
}
