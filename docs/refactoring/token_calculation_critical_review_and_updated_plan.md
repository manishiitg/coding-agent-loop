# Token Calculation: Critical Review & Updated Plan

**Date**: 2025-12-21
**Status**: CRITICAL ISSUES IDENTIFIED - Action Required

## Executive Summary

### **CRITICAL FINDINGS FROM LOG ANALYSIS**

1. ❌ **Character-based estimation is WILDLY inaccurate** (up to 3.5x off)
2. ❌ **Threshold checks ignore cumulative token usage**
3. ⚠️ **Potential double-counting or under-counting** in threshold logic
4. ✅ **Cache tokens are properly tracked separately by LLM**

---

## Ground Truth from Actual Logs

### Token Source Hierarchy (Verified)

```
Priority 1: LLM-reported tokens (GROUND TRUTH)
├─ input_tokens: Actual tokens processed by LLM
├─ cache_tokens: Actual cached tokens (SEPARATE field)
└─ output_tokens: Actual tokens generated

Priority 2: Tiktoken estimation
└─ Use ONLY for context offloading decisions

Priority 3: Character-based estimation
└─ MUST BE REMOVED - Too inaccurate for any use
```

### Log Evidence: Estimation Accuracy Crisis

**Example 1**: Massive underestimation
```
estimated_tokens_sent=4291
input_tokens=14900
token_difference=10609 (3.5x OFF! 246% error)
```

**Example 2**: Significant underestimation
```
estimated_tokens_sent=7327
input_tokens=17252
token_difference=9925 (2.4x OFF! 135% error)
```

**Example 3**: Still way off
```
estimated_tokens_sent=30168
input_tokens=46587
token_difference=16419 (1.5x OFF! 54% error)
```

### Cache Token Accounting (Verified ✅)

From logs, cache tokens are **separate** from input tokens:

```
Turn 5:  cache_tokens=16301  input_tokens=17065
Turn 6:  cache_tokens=16296  input_tokens=17785
Turn 10: cache_tokens=24448  input_tokens=26071
```

**Key Insight**: `input_tokens` from LLM already represents total tokens processed. `cache_tokens` is a breakdown showing which portion came from cache (for billing/metrics), NOT an additive value.

---

## CRITICAL ISSUE: Threshold Check Logic

### Current Behavior (From Logs)

**Turn 1**:
```
cumulative_input_tokens=0
current_input_tokens=7319 (ESTIMATED)
estimated_input_tokens=7319
```
✅ Threshold check: `0 + 7319 = 7319` tokens

**Turn 2** (After Turn 1 completes with actual=14467):
```
cumulative_input_tokens=14467 (ACTUAL from Turn 1)
current_input_tokens=7319 (ESTIMATED for Turn 2)
estimated_input_tokens=7319
```
❌ **Threshold check uses ONLY: `7319` tokens**
✅ **SHOULD check: `14467 + 7319 = 21786` tokens**

**Turn 9**:
```
cumulative_input_tokens=147160 (ACTUAL accumulated)
current_input_tokens=7327 (ESTIMATED for Turn 9)
estimated_input_tokens=7327
```
❌ **Threshold check uses ONLY: `7327` tokens**
✅ **SHOULD check: `147160 + 7327 = 154487` tokens**

### The Double-Counting Problem

**CRITICAL**: The system has TWO separate issues:

1. **Threshold Check Ignores Cumulative**:
   - Checking `current_input_tokens` (7327) against threshold
   - SHOULD check `cumulative_input_tokens + current_input_tokens` (147160 + 7327)
   - Result: Summarization NEVER triggers because cumulative is ignored

2. **Estimation Inaccuracy**:
   - Even if we fix #1, using estimated (7327) instead of actual (25506) causes issues
   - Should use: `cumulative (actual) + estimated_for_current_turn`
   - Then update with actual after LLM call

---

## Token Tracking Flow (Actual Behavior)

### Current Implementation:

```
Before LLM Call:
├─ Read: cumulative_input_tokens (147160 actual from previous turns)
├─ Estimate: estimated_tokens_sent (7327 using chars/4 formula)
├─ Set: current_input_tokens = 7327 (estimated)
└─ Threshold Check: Uses ONLY current_input_tokens (7327) ❌

LLM Call:
└─ LLM Returns: input_tokens=25506 (ACTUAL)

After LLM Call:
├─ Update: cumulative_input_tokens += 25506 (new total: 172666)
└─ Problem: Next turn repeats same broken threshold check
```

### What SHOULD Happen:

```
Before LLM Call:
├─ Read: cumulative_input_tokens (147160 actual from previous turns)
├─ Estimate: estimated_tokens_for_current_turn (7327)
├─ Calculate: total_estimated = 147160 + 7327 = 154487
└─ Threshold Check: total_estimated (154487) vs threshold (100000) ✅

LLM Call:
└─ LLM Returns: input_tokens=25506 (ACTUAL) [17K tokens more than estimated!]

After LLM Call:
├─ Calculate actual: cumulative_input_tokens = 147160 + 25506 = 172666
├─ Update state: cumulative_input_tokens = 172666
└─ Next turn will use 172666 as baseline
```

---

## Recommendations

### IMMEDIATE ACTIONS (Priority 1 - Critical)

#### 1. Fix Threshold Check Logic

**Current**:
```go
// Only checks current estimated tokens
currentInputTokens := estimatedInputTokens
if currentInputTokens > threshold {
    // Trigger summarization
}
```

**Fix**:
```go
// Must include cumulative actual + current estimated
totalTokens := cumulativeInputTokens + estimatedInputTokens
if totalTokens > threshold {
    // Trigger summarization
}
```

#### 2. Remove Character-Based Estimation

**Evidence**:
- 54-246% error rate (1.5x to 3.5x off)
- Cannot be relied upon for ANY decision-making

**Action**: Remove `estimateInputTokens()` function entirely

**Use Instead**:
- For threshold checks: Use `cumulativeInputTokens` (actual from LLM)
- For context offloading: Use `CountTokensForModel()` (tiktoken-based)
- Never use character-based estimation

#### 3. Verify Cache Token Usage

**Question**: Does your threshold calculation need to account for cache tokens?

From logs:
```
Turn 10: cache_tokens=24448, input_tokens=26071
```

**Analysis**:
- `input_tokens` = Total tokens in context (new + cached)
- `cache_tokens` = Portion that was cached (subset of input_tokens)
- **For context window limits**: Use `input_tokens` (already includes everything)
- **For billing/cost**: Use cache vs non-cache breakdown

**Recommendation**:
- Threshold checks should use `input_tokens` only (already complete)
- Cache tokens are for metrics/billing visibility, not threshold calculations

---

## Updated Implementation Plan

### Phase 1: Critical Fix (Immediate)

**File**: Wherever threshold checking occurs (likely in agent/conversation logic)

```go
// BEFORE: ❌ Wrong
estimatedInputTokens := (totalChars / 4) + 50
if estimatedInputTokens > threshold {
    triggerSummarization()
}

// AFTER: ✅ Correct
mutex.RLock()
cumulativeActual := agent.cumulativeInputTokens  // Actual from all previous turns
mutex.RUnlock()

// For current turn, we can only use actual tokens AFTER the LLM call
// So check threshold AFTER LLM response, not before
afterLLMCall := func(actualInputTokens int) {
    totalTokens := cumulativeActual + actualInputTokens
    if totalTokens > threshold {
        triggerSummarization()
    }

    // Update cumulative
    mutex.Lock()
    agent.cumulativeInputTokens = totalTokens
    mutex.Unlock()
}
```

**Alternative** (if you must check before LLM call):
```go
// Use tiktoken for more accurate pre-flight estimation
estimatedForCurrentTurn := CountTokensForModel(messages, model)  // Uses tiktoken
totalEstimated := cumulativeInputTokens + estimatedForCurrentTurn

if totalEstimated > threshold {
    // Trigger summarization BEFORE making the call
    triggerSummarization()
}
```

### Phase 2: Remove Character-Based Estimation

**Find and Remove**:
```go
// DELETE THIS FUNCTION
func estimateInputTokens(messages []Message) int {
    totalChars := 0
    for _, msg := range messages {
        totalChars += len(msg.Content)
    }
    return (totalChars / 4) + 50  // Wildly inaccurate
}
```

**Replace All Usage With**:
- Pre-LLM: `CountTokensForModel()` (tiktoken-based)
- Post-LLM: Use actual `input_tokens` from LLM response
- Threshold checks: Use cumulative actual + estimated current

### Phase 3: Validate Cache Token Handling

**Verify in Code**:
1. Is `input_tokens` from LLM the complete value? (Logs suggest yes)
2. Is `cache_tokens` separate metadata? (Logs suggest yes)
3. Should threshold use `input_tokens` only? (Recommend yes)

**Test Case**:
```go
// Scenario: 100k context, 50k cached
// LLM should return:
//   input_tokens: 100000  (total context)
//   cache_tokens: 50000   (portion cached)
//
// Threshold check should use: 100000 (not 100000+50000)
```

---

## Testing Checklist

After implementing fixes:

- [ ] **Test 1**: Verify threshold triggers correctly
  - Set threshold to 100,000 tokens
  - Run conversation until cumulative > 100,000
  - Confirm summarization triggers

- [ ] **Test 2**: Verify no double-counting
  - Check that `cumulative + current` doesn't exceed context window
  - Monitor logs for `cumulative_input_tokens` progression

- [ ] **Test 3**: Verify cache tokens handled correctly
  - Enable caching
  - Verify `input_tokens` reflects total context (with cache)
  - Verify threshold uses `input_tokens` not `input_tokens + cache_tokens`

- [ ] **Test 4**: Remove estimation completely
  - Grep codebase for "chars / 4" or similar patterns
  - Ensure no threshold checks use character-based estimation

- [ ] **Test 5**: Log comparison
  - Before fix: Show threshold checks using wrong value
  - After fix: Show threshold checks using `cumulative + current`

---

## Log Patterns to Monitor

### Before Fix (Current Broken State):
```
cumulative_input_tokens=147160
current_input_tokens=7327 (WRONG - only estimated)
estimated_input_tokens=7327
```

### After Fix (Expected):
```
cumulative_input_tokens=147160
current_input_tokens=7327 (estimated for current turn)
total_for_threshold_check=154487 (cumulative + current)
```

Then after LLM call:
```
actual_input_tokens=25506 (from LLM)
new_cumulative=172666 (147160 + 25506)
```

---

## Files to Investigate

Based on log analysis, find and fix:

1. **Threshold checking logic**
   - Search for: "Checking token threshold"
   - File: Likely in agent conversation/orchestration code

2. **Token estimation function**
   - Search for: Character count / 4 pattern
   - Search for: "estimated_tokens_sent"
   - Action: DELETE this function

3. **Cumulative tracking**
   - Search for: "cumulative_input_tokens"
   - Verify: Accumulates actual LLM-reported values only
   - Fix: Include in threshold calculations

4. **Cache token extraction**
   - File: [base_orchestrator_tokens_helpers.go](agent_go/pkg/orchestrator/base_orchestrator_tokens_helpers.go)
   - Status: ✅ Looks correct
   - Action: Verify cache tokens are NOT added to input_tokens

---

## Decision Matrix

| Scenario | Use This | NOT This |
|----------|----------|----------|
| **Threshold check (before LLM)** | `cumulative_actual + tiktoken_estimate` | `chars/4` ❌ |
| **Threshold check (after LLM)** | `cumulative_actual + actual_from_LLM` | `chars/4` ❌ |
| **Context offloading** | `CountTokensForModel()` (tiktoken) | `chars/4` ❌ |
| **Accumulation** | `actual_from_LLM` only | Estimated ❌ |
| **Context window limit** | `input_tokens` (from LLM) | `input + cache` ❌ |
| **Billing/metrics** | Both `input_tokens` and `cache_tokens` | N/A |

---

## CRITICAL: No Double-Counting Rule

**Token Accounting Rule**:

```
Context Window Usage = input_tokens (from LLM)

Where:
  input_tokens = total tokens processed (includes everything)
  cache_tokens = breakdown of cached portion (subset, not additive)

DO NOT DO THIS: ❌
  total = input_tokens + cache_tokens  // WRONG! Double-counts cached tokens

DO THIS: ✅
  total = input_tokens  // Already includes cached portion
```

**Verification Test**:
```go
// If LLM reports:
input_tokens = 26071
cache_tokens = 24448

// Then:
total_for_threshold = 26071 (NOT 26071 + 24448 = 50519)
cache_percentage = 24448 / 26071 = 93.8% (for metrics only)
```

---

## Conclusion

### Problems Identified:

1. ✅ **Character-based estimation is 54-246% off** → Must be removed
2. ✅ **Threshold check ignores cumulative tokens** → Critical bug
3. ✅ **Cache tokens are separate metadata** → Don't add to input_tokens
4. ✅ **Logs show actual behavior** → Ground truth for verification

### Required Changes:

1. **CRITICAL**: Fix threshold check to use `cumulative + current`
2. **CRITICAL**: Remove character-based estimation entirely
3. **HIGH**: Verify cache token handling (likely already correct)
4. **MEDIUM**: Add monitoring for token progression

### Success Criteria:

- Summarization triggers when `cumulative_actual + current_estimated > threshold`
- No character-based estimation in codebase
- Cache tokens used for metrics only, not threshold calculations
- Logs show correct cumulative progression

---

## Next Steps

1. **Locate threshold checking code** - Search for "Checking token threshold" log message
2. **Implement critical fix** - Add cumulative to threshold calculation
3. **Remove estimation** - Delete `chars/4` based functions
4. **Test thoroughly** - Verify summarization triggers correctly
5. **Monitor logs** - Confirm cumulative tracking is accurate

**Timeline**: This is CRITICAL - should be fixed immediately before more incorrect decisions are made based on faulty token tracking.
