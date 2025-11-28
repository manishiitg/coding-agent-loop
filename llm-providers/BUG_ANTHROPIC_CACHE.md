# Bug Report: LLM Provider Cache Token Issues

## Issues Summary
1. **Anthropic**: Prompt caching not working despite proper implementation
2. **OpenRouter**: `usage: {include: true}` parameter not being sent to API

---

## Issue 1: Anthropic Prompt Caching Not Working

### Issue Summary
Anthropic's prompt caching feature is not working despite proper implementation of cache control parameters and beta headers. Cache tokens (`CacheReadInputTokens` and `CacheCreationInputTokens`) remain 0 in API responses.

## Environment
- **SDK**: `github.com/anthropics/anthropic-sdk-go v1.16.0`
- **Model**: `claude-haiku-4-5-20251001`
- **Test Context**: 14,977 characters (~3,744 estimated tokens, 2,864 actual tokens)

## Implementation Details

### What's Implemented

1. **Beta Header**: Added `anthropic-beta: prompt-caching-2024-07-31` header per request
   ```go
   stream := a.client.Messages.NewStreaming(ctx, params, 
       anthropicoption.WithHeader("anthropic-beta", "prompt-caching-2024-07-31"))
   ```

2. **Cache Control Structure**: Using proper constructor and TTL
   ```go
   cacheControl := anthropic.NewCacheControlEphemeralParam()
   cacheControl.TTL = anthropic.CacheControlEphemeralTTLTTL5m
   textBlock := anthropic.TextBlockParam{
       Text:         content,
       CacheControl: cacheControl,
   }
   ```

3. **Context Size**: Context exceeds minimum requirements
   - Claude Haiku requires: 2,048 tokens minimum
   - Our context: 2,864 actual tokens (well above minimum)

### Verification Results

✅ **Cache Control in JSON**: Confirmed present in request payload
```json
"cache_control": {"ttl": "5m", "type": "ephemeral"}
```

✅ **Beta Header**: Added to every request
```
[ANTHROPIC DEBUG] Making API call with beta header: anthropic-beta=prompt-caching-2024-07-31
```

✅ **Cache Control Detection**: Found in params before API call
```
[ANTHROPIC DEBUG] Found cache control in params - message 0, block 0: TTL=5m, Type=ephemeral, TextLength=15045
```

❌ **Cache Tokens**: Always 0 in API response
```
[ANTHROPIC DEBUG] API Response - CacheReadInputTokens: 0, CacheCreationInputTokens: 0
```

## Test Results

### Turn 1 (Cache Creation)
- **Input Tokens**: 2,864
- **Output Tokens**: 496
- **CacheCreationInputTokens**: 0 ❌
- **CacheReadInputTokens**: 0

### Turn 2 (Cache Usage)
- **Input Tokens**: 3,372
- **Output Tokens**: 2,135
- **CacheCreationInputTokens**: 0 ❌
- **CacheReadInputTokens**: 0 ❌

## Debug Output

```
[ANTHROPIC DEBUG] Applied cache control: content_length=15045, estimated_tokens=3761, TTL=5m, Type=ephemeral
[ANTHROPIC DEBUG] ✅ cache_control IS in JSON
[ANTHROPIC DEBUG] Cache control JSON: "cache_control":{"ttl":"5m","type":"ephemeral"}
[ANTHROPIC DEBUG] Making API call with beta header: anthropic-beta=prompt-caching-2024-07-31
[ANTHROPIC DEBUG] Model: claude-haiku-4-5-20251001, Messages: 1, System blocks: 0
[ANTHROPIC DEBUG] API Response - CacheReadInputTokens: 0, CacheCreationInputTokens: 0
```

## Possible Root Causes

1. **Beta Header Not Applied**: The SDK's `WithHeader` might not be correctly adding the header to HTTP requests
2. **Model Limitation**: Claude Haiku 4.5 might have different caching requirements or limitations
3. **SDK Version Issue**: The SDK version might have a bug or missing feature
4. **API Requirements**: There might be additional requirements not documented or not implemented
5. **Cache Matching Logic**: The cache might require exact message structure matching that we're not meeting

## Files Modified

- `llm-providers/pkg/adapters/anthropic/anthropic_adapter.go`
  - Added cache control logic in `convertMessages()`
  - Added beta header in `GenerateContent()`
  - Added cache token extraction in `convertResponse()`
  - Added extensive debug logging

- `llm-providers/providers.go`
  - Removed beta header from client initialization (moved to per-request)

- `llm-providers/internal/testing/commands/token-usage-test.go`
  - Added multi-turn conversation test with large context
  - Added cache token analysis and reporting

## Next Steps

1. Verify beta header is actually sent in HTTP request (use HTTP proxy/interceptor)
2. Test with different Anthropic models (Claude 3.5 Sonnet, Claude Opus)
3. Test with direct REST API calls (bypass SDK) to isolate SDK issues
4. Check Anthropic API documentation for any additional requirements
5. Contact Anthropic support for clarification on cache requirements

## References

- [Anthropic Prompt Caching Documentation](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching)
- [Anthropic Beta Headers Documentation](https://docs.anthropic.com/en/api/beta-headers)
- SDK: `github.com/anthropics/anthropic-sdk-go v1.16.0`

## Test Command

```bash
cd llm-providers && ./bin/llm-test token-usage --provider anthropic
```

---

## Issue 2: OpenRouter `usage: {include: true}` Parameter Not Being Sent

### Issue Summary
OpenRouter requires `usage: {include: true}` parameter to get detailed cache token information, but this parameter is not being sent in API requests despite being set in the code.

### Root Cause
- `WithOpenRouterUsage()` correctly sets `opts.Metadata.Usage.Include = true` ✅
- OpenAI adapter detects the setting (debug logs confirm) ✅
- **BUT**: OpenAI SDK's `ChatCompletionNewParams` doesn't have a `Usage` field ❌
- Result: The parameter is never included in the actual API request

### Evidence
```
[INFO] [OPENROUTER DEBUG] Usage.Include is set to true, but OpenAI SDK doesn't support usage parameter directly
```

### Impact
- We still receive `prompt_tokens_details` in responses (OpenRouter may return it by default)
- However, `cached_tokens` is always 0, which could be because:
  1. The `usage: {include: true}` parameter is required for accurate cache token reporting
  2. Cache isn't being triggered (exact prefix matching, cache expiration, etc.)
  3. Model-specific caching limitations

### Test Results
OpenRouter was tested for cache token support. Results show that cache tokens are not being detected:

**Turn 1 (Cache Creation)**
- Input Tokens: 2,539
- Output Tokens: 567
- CachedContentTokens: nil ❌
- CacheDiscount: nil ❌

**Turn 2 (Cache Usage)**
- Input Tokens: 3,122
- Output Tokens: 1,269
- CachedContentTokens: nil ❌
- Additional map: nil ❌

### Implementation Status ✅

**Cache Token Extraction**: ✅ **IMPLEMENTED**

The OpenAI adapter has been updated to extract OpenRouter cache tokens:

1. **Detection**: Automatically detects OpenRouter usage by checking if model ID contains "/" (e.g., "moonshotai/kimi-k2")
2. **Extraction**: Parses `prompt_tokens_details.cached_tokens` from the Usage struct JSON
3. **Population**: Sets `CachedContentTokens` and calculates `CacheDiscount` when cache tokens > 0
4. **Storage**: Stores cache tokens in `Additional` map for debugging

**Code Location**: `llm-providers/pkg/adapters/openai/openai_adapter.go`
- Function: `convertResponse()`
- Lines: 504-625

**Test Results**:
- ✅ Cache token extraction working correctly (using typed struct)
- ✅ Successfully extracted `cached_tokens: 2048` in Turn 2 (from earlier test)
- ⚠️ Current tests show `cached_tokens: 0` in all turns

**Test Updates**:
- Test updated to send exact same message structure in Turn 1 and Turn 2 (required for OpenRouter's exact prefix matching)
- Still getting `cached_tokens: 0` - likely due to missing `usage: {include: true}` parameter

**OpenRouter Documentation** ([source](https://openrouter.ai/docs/features/prompt-caching)):
- **Moonshot AI (Kimi K2)**: Caching is **automated** and does not require additional configuration
- Cache reads are charged at the same rate as original input pricing
- To inspect cache usage: Use `usage: {include: true}` in request
- Cache tokens are returned in `prompt_tokens_details.cached_tokens` field
- Requires exact prefix matching for cache hits

**Next Steps**:
- Investigate if OpenAI SDK supports `ExtraBody` or additional parameters
- Consider using custom HTTP client to modify request body
- Or use reflection to add the field to params struct
- Verify if `usage: {include: true}` is actually required for cache token reporting

### Test Command

```bash
cd llm-providers && ./bin/llm-test token-usage --provider openrouter
```

