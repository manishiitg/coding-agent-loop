# Token Tracking Test - Log Analysis Criteria

This test validates the agent's token usage tracking feature. **These tests don't use traditional asserts** - instead, logs are analyzed (manually or by LLM) to verify test success.

## Running the Test

```bash
# Basic test (uses OpenAI by default, 3 calls)
mcpagent-test test token-tracking --log-file logs/token-tracking-test.log

# With custom number of calls
mcpagent-test test token-tracking --num-calls 5 --log-file logs/token-tracking-test.log

# With specific model
mcpagent-test test token-tracking --model gpt-4.1 --log-file logs/token-tracking-test.log
```

## What This Test Does

The test performs the following operations:

1. **Initial State Check**: Verifies token usage starts at zero
2. **Single Call Test**: Makes one agent call and verifies token accumulation
3. **Multiple Calls Test**: Makes multiple calls and verifies cumulative token accumulation
4. **Multi-Turn Conversation**: Tests token tracking across a conversation with context
5. **Final Summary**: Provides comprehensive token usage statistics

## Log Analysis Checklist

After running the test, analyze the log file to verify:

### ✅ Test 1: Initial Token Usage (Should Be Zero)

- [ ] Check for "Initial Token Usage" log entry
- [ ] Verify all token counts are zero:
  - `prompt_tokens: 0`
  - `completion_tokens: 0`
  - `total_tokens: 0`
  - `cache_tokens: 0`
  - `reasoning_tokens: 0`
  - `llm_call_count: 0`
  - `cache_enabled_call_count: 0`

**What to look for in logs:**
```
📊 Token Usage: Initial
prompt_tokens=0 completion_tokens=0 total_tokens=0 ...
✅ Initial token usage is zero as expected
```

### ✅ Test 2: Single Call Token Accumulation

- [ ] Check for "After First Call" token usage log
- [ ] Verify token counts increased from zero:
  - `total_tokens > 0`
  - `prompt_tokens > 0` (should be present)
  - `completion_tokens > 0` (should be present)
  - `llm_call_count = 1`
- [ ] Verify token consistency: `total_tokens ≈ prompt_tokens + completion_tokens` (may differ if reasoning tokens included)

**What to look for in logs:**
```
📊 Token Usage: After First Call
prompt_tokens=X completion_tokens=Y total_tokens=Z ...
llm_call_count=1
✅ Token accumulation working - tokens tracked after first call
✅ LLM call count is correct
```

### ✅ Test 3: Multiple Calls Cumulative Accumulation

- [ ] Check for token usage after each additional call
- [ ] Verify cumulative totals increase with each call:
  - `total_tokens` increases after each call
  - `llm_call_count` increases by 1 after each call
- [ ] Verify no token counts decrease (should only accumulate)
- [ ] Check for "Token accumulation verified" messages

**What to look for in logs:**
```
📊 Token Usage: After Call 2
total_tokens=X llm_call_count=2
✅ Token accumulation verified - tokens increased
  previous=Y current=X increase=Z

📊 Token Usage: After Call 3
total_tokens=Y llm_call_count=3
✅ Token accumulation verified - tokens increased
  previous=X current=Y increase=Z
```

### ✅ Test 4: Multi-Turn Conversation Token Accumulation

- [ ] Check for multi-turn conversation calls (3 turns)
- [ ] Verify tokens continue to accumulate across conversation
- [ ] Check for conversation summary with:
  - `tokens_used` (tokens used during conversation)
  - `calls_made` (should be 3)
  - `tokens_per_call` (average)

**What to look for in logs:**
```
Multi-turn conversation call...
  turn=1 question="My name is Alice..."
✅ Conversation call completed

Multi-turn conversation summary
tokens_used=X calls_made=3 tokens_per_call=Y
```

### ✅ Test 5: Final Token Usage Summary

- [ ] Check for final summary with all token metrics
- [ ] Verify token usage averages are calculated:
  - `avg_prompt_tokens`
  - `avg_completion_tokens`
  - `avg_total_tokens`
- [ ] Check cache tokens analysis (if applicable):
  - `total_cache_tokens` (may be 0 if not supported)
  - `cache_enabled_call_count` (may be 0 if not supported)
- [ ] Check reasoning tokens analysis (if applicable):
  - `total_reasoning_tokens` (may be 0 if model doesn't support)
- [ ] Verify token consistency check

**What to look for in logs:**
```
📊 Token Usage: Final Summary
prompt_tokens=X completion_tokens=Y total_tokens=Z ...
llm_call_count=N

Token usage averages per call
avg_prompt_tokens=X avg_completion_tokens=Y avg_total_tokens=Z

✅ Cache tokens detected (if supported)
  total_cache_tokens=X cache_enabled_calls=Y

✅ Reasoning tokens detected (if supported)
  total_reasoning_tokens=X

✅ Token consistency verified: total = prompt + completion
```

## Expected Test Outcome

A successful test should show:

1. **Initial state**: All token counts at zero
2. **Progressive accumulation**: Token counts increase with each LLM call
3. **Correct call counting**: `llm_call_count` matches number of calls made
4. **Cumulative totals**: Total tokens accumulate across all calls
5. **Token consistency**: Total tokens should equal or be close to prompt + completion (may differ if reasoning tokens included)
6. **Cache tokens** (if supported): Cache tokens tracked separately and `cache_enabled_call_count` reflects calls with cache
7. **Reasoning tokens** (if supported): Reasoning tokens tracked separately for models that support it

## Key Metrics to Verify

### Token Counts
- **Prompt tokens**: Should increase with each call (represents input)
- **Completion tokens**: Should increase with each call (represents output)
- **Total tokens**: Should equal or be close to prompt + completion
- **Cache tokens**: May be 0 if provider/model doesn't support cache
- **Reasoning tokens**: May be 0 if model doesn't support reasoning (e.g., o3 models)

### Call Counts
- **LLM call count**: Should match total number of `Ask()` calls made
- **Cache-enabled call count**: Should be ≤ LLM call count (may be 0 if cache not supported)

### Consistency Checks
- Token counts should never decrease (only accumulate)
- `llm_call_count` should never decrease
- Total tokens should be ≥ prompt tokens + completion tokens

## Troubleshooting

### Issue: Token counts remain zero after calls

**Possible causes:**
- Token tracking not initialized properly
- LLM provider not returning token usage
- Agent not accumulating tokens correctly

**Check logs for:**
- "Token accumulation working" messages
- LLM generation events with token usage
- Any error messages about token tracking

### Issue: LLM call count doesn't match expected

**Possible causes:**
- Agent making unexpected additional calls
- Token tracking not called after each LLM call
- Multiple agents created (each has separate tracking)

**Check logs for:**
- Number of "Ask" calls made
- Number of LLM generation events
- Any agent recreation or reset

### Issue: Cache tokens always zero

**This is normal if:**
- Provider doesn't support cache tokens
- Model doesn't support cache
- Cache not enabled in provider configuration

**To test cache tokens:**
- Use a provider/model that supports cache (e.g., OpenAI with certain models)
- Ensure cache is enabled in provider configuration

### Issue: Reasoning tokens always zero

**This is normal if:**
- Model doesn't support reasoning tokens (most models don't)
- Only certain models support reasoning tokens (e.g., OpenAI o3 series)

**To test reasoning tokens:**
- Use a model that supports reasoning tokens (e.g., `gpt-o3-mini`, `gpt-o3`)

### Issue: Total tokens ≠ prompt + completion

**This may be expected if:**
- Reasoning tokens are included in total but not in prompt/completion
- Provider includes additional overhead in total
- Cache tokens are counted separately

**Check:**
- If reasoning tokens are present, they may be included in total
- Verify with provider documentation for token counting method

## Example Successful Log Pattern

```
=== Token Tracking Test ===
✅ LLM initialized model_id=gpt-4.1
✅ Agent created

--- Test 1: Initial Token Usage (Should Be Zero) ---
📊 Token Usage: Initial
  prompt_tokens=0 completion_tokens=0 total_tokens=0 ...
✅ Initial token usage is zero as expected

--- Test 2: Single Call Token Accumulation ---
Making first agent call...
✅ First call completed
📊 Token Usage: After First Call
  prompt_tokens=150 completion_tokens=50 total_tokens=200 ...
  llm_call_count=1
✅ Token accumulation working - tokens tracked after first call
✅ LLM call count is correct

--- Test 3: Multiple Calls Cumulative Accumulation ---
📊 Token Usage: After Call 2
  total_tokens=400 llm_call_count=2
✅ Token accumulation verified - tokens increased
  previous=200 current=400 increase=200

--- Test 4: Multi-Turn Conversation Token Accumulation ---
Multi-turn conversation summary
  tokens_used=600 calls_made=3 tokens_per_call=200

--- Test 5: Final Token Usage Summary ---
📊 Token Usage: Final Summary
  prompt_tokens=750 completion_tokens=250 total_tokens=1000 ...
  llm_call_count=5
Token usage averages per call
  avg_prompt_tokens=150 avg_completion_tokens=50 avg_total_tokens=200
✅ Token consistency verified: total = prompt + completion
✅ All token tracking tests completed successfully
```

## Notes

- Token counts may vary between runs due to LLM response variability
- Cache tokens and reasoning tokens are provider/model dependent
- The test focuses on verifying accumulation logic, not exact token counts
- Token consistency (total = prompt + completion) may differ if reasoning tokens are included in total

