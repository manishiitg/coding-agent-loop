# Token Calculation: Use Actual Values Instead of Estimation

## Summary

**Key Principle**: Only use actual token values from LLM responses for decision-making and accumulation. Never store estimated values in cumulative tracking.

**Important**: For summarization and context editing decisions, use **input tokens + cached tokens** (both) because the context window includes both.

**Changes Required**:
1. ✅ Summarization threshold check: Use `currentContextWindowUsage` (actual input + cache) instead of `estimateInputTokens()`
2. ✅ After summarization reset: Use actual tokens (input + cache) from summarization response
3. ✅ **Cumulative accumulation**: Only accumulate if LLM returns actual token values (don't store estimates)
4. ✅ `extractUsageMetricsWithMessages()`: Don't estimate, return 0 if no actual values
5. ✅ Context editing: Use actual input + cache tokens for decisions

**Result**: Summarization and context editing will only trigger based on actual token usage (input + cache), not estimates.

## Problem

Currently, the codebase uses `estimateInputTokens()` (character-based estimation: `chars/4 + 50`) in critical decision-making paths where actual token values should be used:

1. **Summarization threshold check** - Uses `estimateInputTokens()` which doesn't account for cached tokens, leading to incorrect decisions
2. **After summarization reset** - Uses `estimateInputTokens()` instead of actual tokens from the summarization LLM response
3. **Context editing** - Uses estimation for logging (acceptable) but should use actual values where available

### Current Issues

- **Summarization check** (`conversation.go:426`): Uses `estimateInputTokens(llmMessages)` which only estimates ~3,993 tokens, missing the 130,401 cached tokens from previous calls
- **After summarization** (`conversation.go:503`): Resets `currentContextWindowUsage` using estimation instead of actual tokens from summarization response
- **Token tracking**: `currentContextWindowUsage` is updated with actual `PromptTokens` (line 1452) which includes cache, but summarization check happens BEFORE this update

### What's Already Correct ✅

- **Context offloading** (`conversation.go:1404`): Uses `CountTokensForModel()` which performs actual token counting via tiktoken/provider-aware encoding - this is correct
- **Context editing decisions** (`context_editing.go:177`): Uses `CountTokensForModel()` for actual token counting - this is correct

### Impact

- Summarization is not triggered when it should be (threshold check uses wrong value)
- Context window usage tracking is inaccurate after summarization
- Cache tokens are not properly accounted for in decision-making
- **Estimated tokens are being accumulated** in cumulative tracking, causing incorrect triggers for summarization/editing

## Solution

**Use actual token values from LLM responses instead of estimation for:**
1. Summarization threshold checks
2. Context window usage tracking after summarization
3. Any decision-making logic (not just logging)
4. **Cumulative token accumulation** - Only store actual values from LLM responses

**Critical Rule:**
- **If LLM doesn't return token usage, don't accumulate anything**
- **Never store estimated values in cumulative tracking**
- This ensures summarization/editing only trigger based on actual usage, not estimates

**Important distinction:**
- **`CountTokensForModel()`** - Uses actual token counting (tiktoken/provider-aware encoding) - ✅ **CORRECT** for context offloading
- **`estimateInputTokens()`** - Character-based estimation (`chars/4 + 50`) - ❌ **SHOULD NOT** be used for decision-making

**Estimation (`estimateInputTokens`) should ONLY be used for:**
- Context offloading (but we already use `CountTokensForModel()` for this, which is correct)
- Initial estimates before first LLM call (when no actual values exist yet)
- Logging/display purposes only (not for decisions)

## Proposed Changes

### 1. Fix Summarization Threshold Check

**File**: `mcpagent/agent/conversation.go`

**Current** (line 426-430):
```go
estimatedInputTokens := estimateInputTokens(llmMessages)
currentInputTokens := estimatedInputTokens
```

**Change to**:
```go
// Use actual context window usage from previous LLM calls (includes input + cache)
// currentContextWindowUsage is set to usageMetrics.PromptTokens which includes both
a.tokenTrackingMutex.RLock()
currentInputTokens := a.currentContextWindowUsage  // This already includes input + cache
a.tokenTrackingMutex.RUnlock()

// Add estimated tokens for current turn (will be updated with actual after LLM call)
// Note: This is just an estimate for the current turn, actual will include cache too
estimatedInputTokens := estimateInputTokens(llmMessages)
totalInputTokens := currentInputTokens + estimatedInputTokens
```

**Use `totalInputTokens` for threshold check** instead of just `estimatedInputTokens`.

**Important**: `currentContextWindowUsage` is set to `usageMetrics.PromptTokens` (line 1452) which includes both input tokens and cached tokens. This is correct for summarization decisions because the context window includes both.

### 2. Fix After Summarization Reset

**File**: `mcpagent/agent/conversation.go`

**Current** (line 501-507):
```go
a.tokenTrackingMutex.Lock()
estimatedAfterSummary := estimateInputTokens(llmMessages)
a.currentContextWindowUsage = estimatedAfterSummary
a.tokenTrackingMutex.Unlock()
```

**Change to**: Use actual tokens from summarization response. The summarization LLM call returns actual token counts in `summaryResp`. We need to:

1. Get actual tokens from the summarization response (already available in `rebuildMessagesWithSummary`)
2. Calculate actual tokens for the new messages (system + summary + recent)
3. Set `currentContextWindowUsage` to this actual value

**Option A**: Pass summarization response tokens back and calculate:
```go
// In rebuildMessagesWithSummary, return actual tokens for new messages
// Then use that value here
a.tokenTrackingMutex.Lock()
// Use actual tokens from summarization response + estimate for recent messages
// Or better: make another estimation call with actual summarization tokens
a.currentContextWindowUsage = actualSummaryTokens + estimateInputTokens(recentMessages)
a.tokenTrackingMutex.Unlock()
```

**Option B**: Use the actual `PromptTokens` from the next LLM call to update it (simpler, but delayed)

**Recommended**: Option A - calculate actual tokens for new messages immediately after summarization.

### 3. Fix Cumulative Token Accumulation

**File**: `mcpagent/agent/agent.go` and `mcpagent/agent/utils.go`

**Current Issue**:
- `extractUsageMetricsWithMessages()` (line 147-161) falls back to `estimateInputTokens()` if `usage.InputTokens == 0`
- These estimated values then get passed to `accumulateTokenUsage()` and stored in cumulative tracking
- This causes summarization/editing to trigger incorrectly based on estimated values

**Change to**:
1. **Modify `accumulateTokenUsage()`** to only accumulate if we have actual token values from LLM response:
```go
func (a *Agent) accumulateTokenUsage(ctx context.Context, usageMetrics events.UsageMetrics, resp *llmtypes.ContentResponse, turn int) {
	// Only accumulate if we have actual token values from LLM response
	// Check if resp has actual usage data (not estimated)
	hasActualUsage := resp != nil && (
		(resp.Usage != nil && (resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0)) ||
		(len(resp.Choices) > 0 && resp.Choices[0].GenerationInfo != nil &&
			(resp.Choices[0].GenerationInfo.InputTokens != nil || resp.Choices[0].GenerationInfo.OutputTokens != nil)),
	)
	
	if !hasActualUsage {
		// Don't accumulate estimated values - return early
		logger := getLogger(a)
		logger.Debug("Skipping token accumulation - no actual usage data from LLM",
			loggerv2.Int("turn", turn))
		return
	}
	
	// ... rest of accumulation logic
}
```

2. **Modify `extractUsageMetricsWithMessages()`** to not estimate, return 0 if no actual values:
```go
func extractUsageMetricsWithMessages(resp *llmtypes.ContentResponse, messages []llmtypes.MessageContent) observability.UsageMetrics {
	usage := extractUsageMetrics(resp)
	
	// Don't estimate - only return actual values
	// If no actual values, return zeros (caller should handle this)
	// Estimation should only be used for logging/display, not accumulation
	return usage
}
```

**Alternative approach**: Check in `accumulateTokenUsage` if `usageMetrics` values are actual (from resp) or estimated (from estimation function), and only accumulate if actual.

### 4. Context Editing

**File**: `mcpagent/agent/conversation.go` (lines 357, 378) and `context_editing.go` (line 177)

**Current state:**
- Context editing decision logic uses `CountTokensForModel()` (line 177) - ✅ **CORRECT** (actual token counting for individual tool outputs)
- Logging uses `estimateInputTokens()` (lines 357, 378) - ⚠️ **Acceptable for logging only**

**Important Note:**
- Context editing evaluates individual tool outputs using `CountTokensForModel()` - this is correct
- For context editing threshold decisions, we need to consider the total context window usage (input + cache)
- The context editing logic should also consider `currentContextWindowUsage` (input + cache) when making decisions about when to compact

**Recommendation:**
- Keep `CountTokensForModel()` for individual tool output evaluation (already correct)
- Consider using `currentContextWindowUsage` (input + cache) for overall context window threshold checks
- Estimation is acceptable for logging purposes only

## Implementation Details

### Token Value Sources

1. **`currentContextWindowUsage`** - Actual tokens from previous LLM calls (includes cache)
   - Updated after each LLM call with `usageMetrics.PromptTokens` (line 1452)
   - Includes cached tokens from previous turns
   - Reset after summarization

2. **`usageMetrics.PromptTokens`** - Actual input tokens from LLM response
   - Includes: **new input tokens + cached tokens** (both)
   - Source: `resp.Usage.InputTokens` or `resp.Choices[0].GenerationInfo.InputTokens`
   - **This is what we use for summarization/editing decisions** (input + cache)

3. **`cacheTokens`** - Actual cached tokens from LLM response
   - Source: `resp.Usage.CacheTokens` or `GenerationInfo.CacheTokens`

4. **`CountTokensForModel()`** - Actual token counting (tiktoken/provider-aware)
   - Uses model metadata for accurate encoding
   - Used for: Context offloading (large tool output detection)
   - Location: `tool_output_handler.go:95`
   - ✅ **This is correct** - uses actual token counting, not estimation

5. **`estimateInputTokens()`** - Character-based estimation
   - Formula: `(totalChars / 4) + 50`
   - ❌ **Should NOT be used for decision-making**
   - Use only for: Initial estimates before first LLM call (when no actual values exist)

### Key Insight

For summarization and context editing decisions, we need:
- **Total tokens in context** = `currentContextWindowUsage` (from previous calls, includes input + cache) + `estimatedInputTokens` (for current call, will be updated with actual after LLM call)

The current code only uses `estimatedInputTokens`, missing the cached tokens from previous calls.

**Important**: 
- `usageMetrics.PromptTokens` from LLM response = **input tokens + cached tokens** (both)
- `currentContextWindowUsage` is set to `PromptTokens`, so it already includes both
- For summarization/editing decisions, we need input + cache (both), which `currentContextWindowUsage` already provides

## Testing

After implementation, verify:

1. **Summarization triggers correctly** when `currentContextWindowUsage + estimatedInputTokens` exceeds threshold
2. **Input + cache tokens are included** in threshold calculations (both are needed for context window)
3. **After summarization**, `currentContextWindowUsage` reflects actual tokens (input + cache) in new messages
4. **Cumulative tokens only include actual values** - if LLM doesn't return tokens, nothing is accumulated
5. **Summarization/editing don't trigger** based on estimated values (only actual usage)
6. **Context editing uses input + cache** for decision-making (not just input tokens)
7. **Logging shows** both estimated and actual values for comparison

## Files to Modify

1. `mcpagent/agent/conversation.go` - Summarization threshold check (line ~426)
2. `mcpagent/agent/conversation.go` - After summarization reset (line ~503)
3. `mcpagent/agent/agent.go` - `accumulateTokenUsage()` - Only accumulate actual values (line ~1330)
4. `mcpagent/agent/utils.go` - `extractUsageMetricsWithMessages()` - Don't estimate, return 0 if no actual values (line ~147)
5. `mcpagent/agent/context_summarization.go` - Return actual tokens for new messages (if needed)

## Related Code

- `mcpagent/agent/agent.go:1330` - `accumulateTokenUsage()` - Currently accumulates estimated values ❌
- `mcpagent/agent/agent.go:1452` - Updates `currentContextWindowUsage` with actual `PromptTokens`
- `mcpagent/agent/utils.go:147` - `extractUsageMetricsWithMessages()` - Currently estimates if no actual values ❌
- `mcpagent/agent/utils.go:164` - `estimateInputTokens()` function (character-based estimation)
- `mcpagent/agent/tool_output_handler.go:95` - `CountTokensForModel()` function (actual token counting via tiktoken)
- `mcpagent/agent/conversation.go:1404` - Context offloading uses `CountTokensForModel()` ✅
- `mcpagent/agent/context_editing.go:177` - Context editing uses `CountTokensForModel()` ✅
- `mcpagent/agent/context_summarization.go:444` - Summarization LLM call returns actual tokens

