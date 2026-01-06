package orchestrator

import (
	"mcpagent/events"
)

// CacheTokens holds separated cache read and write token counts
type CacheTokens struct {
	ReadTokens  int // Tokens read from cache (charged at discount rate)
	WriteTokens int // Tokens written to cache (charged at premium rate, 1.25x)
	Total       int // Total cache tokens (read + write)
}

// extractCacheTokensSeparate extracts cache read and write tokens separately from a TokenUsageEvent
// This is important for accurate pricing: cache reads are discounted, cache writes are premium (1.25x)
// Returns CacheTokens struct with ReadTokens, WriteTokens, and Total
func extractCacheTokensSeparate(tokenEvent *events.TokenUsageEvent) CacheTokens {
	result := CacheTokens{}

	if tokenEvent.GenerationInfo == nil {
		return result
	}

	// Priority 1: Check for cumulative cache tokens (from conversation end event)
	// These are pre-separated cumulative totals
	if cumulativeRead, ok := tokenEvent.GenerationInfo["cumulative_cache_read_tokens"].(int); ok {
		result.ReadTokens = cumulativeRead
	} else if cumulativeRead, ok := tokenEvent.GenerationInfo["cumulative_cache_read_tokens"].(float64); ok {
		result.ReadTokens = int(cumulativeRead)
	}
	if cumulativeWrite, ok := tokenEvent.GenerationInfo["cumulative_cache_write_tokens"].(int); ok {
		result.WriteTokens = cumulativeWrite
	} else if cumulativeWrite, ok := tokenEvent.GenerationInfo["cumulative_cache_write_tokens"].(float64); ok {
		result.WriteTokens = int(cumulativeWrite)
	}

	// If we got cumulative values, calculate total and return
	if result.ReadTokens > 0 || result.WriteTokens > 0 {
		result.Total = result.ReadTokens + result.WriteTokens
		return result
	}

	// Priority 2: Check for legacy cumulative_cache_tokens (backward compatibility)
	// This is the old combined total - we'll treat it as all read tokens (conservative estimate)
	if ct, ok := tokenEvent.GenerationInfo["cumulative_cache_tokens"].(int); ok {
		result.ReadTokens = ct
		result.Total = ct
		return result
	}
	if ct, ok := tokenEvent.GenerationInfo["cumulative_cache_tokens"].(float64); ok {
		result.ReadTokens = int(ct)
		result.Total = int(ct)
		return result
	}

	// Priority 3: Check top-level fields (Additional map fields are copied to top level by ExtractTokenUsageWithCacheInfo)
	// CacheReadInputTokens (Anthropic field) - tokens read from cache (discounted)
	if cacheRead, ok := tokenEvent.GenerationInfo["CacheReadInputTokens"]; ok {
		if cacheReadInt, ok := cacheRead.(int); ok {
			result.ReadTokens += cacheReadInt
		} else if cacheReadFloat, ok := cacheRead.(float64); ok {
			result.ReadTokens += int(cacheReadFloat)
		}
	}

	// CacheCreationInputTokens (Anthropic field) - tokens written to cache (premium, 1.25x)
	if cacheCreate, ok := tokenEvent.GenerationInfo["CacheCreationInputTokens"]; ok {
		if cacheCreateInt, ok := cacheCreate.(int); ok {
			result.WriteTokens += cacheCreateInt
		} else if cacheCreateFloat, ok := cacheCreate.(float64); ok {
			result.WriteTokens += int(cacheCreateFloat)
		}
	}

	// Priority 4: Also check Additional map (in case fields weren't copied to top level)
	if additional, ok := tokenEvent.GenerationInfo["Additional"].(map[string]interface{}); ok {
		// Check CacheReadInputTokens (only if not already found at top level)
		if result.ReadTokens == 0 {
			if cacheRead, ok := additional["CacheReadInputTokens"]; ok {
				if cacheReadInt, ok := cacheRead.(int); ok {
					result.ReadTokens += cacheReadInt
				} else if cacheReadFloat, ok := cacheRead.(float64); ok {
					result.ReadTokens += int(cacheReadFloat)
				}
			}
		}
		// Check CacheCreationInputTokens (only if not already found at top level)
		if result.WriteTokens == 0 {
			if cacheCreate, ok := additional["CacheCreationInputTokens"]; ok {
				if cacheCreateInt, ok := cacheCreate.(int); ok {
					result.WriteTokens += cacheCreateInt
				} else if cacheCreateFloat, ok := cacheCreate.(float64); ok {
					result.WriteTokens += int(cacheCreateFloat)
				}
			}
		}
	}

	// Priority 5: Check for CachedContentTokens (Anthropic field) - treat as read tokens
	if result.ReadTokens == 0 {
		if cachedContent, ok := tokenEvent.GenerationInfo["CachedContentTokens"].(int); ok {
			result.ReadTokens += cachedContent
		} else if cachedContent, ok := tokenEvent.GenerationInfo["CachedContentTokens"].(float64); ok {
			result.ReadTokens += int(cachedContent)
		}
	}

	result.Total = result.ReadTokens + result.WriteTokens
	return result
}

// extractCacheTokens extracts total cache tokens from a TokenUsageEvent's GenerationInfo
// Returns 0 if not available or cannot be extracted
// DEPRECATED: Use extractCacheTokensSeparate for accurate pricing (separates read vs write)
func extractCacheTokens(tokenEvent *events.TokenUsageEvent) int {
	return extractCacheTokensSeparate(tokenEvent).Total
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
