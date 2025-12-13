# Temporary LLM Cascading Flow

## Flow Sequence

**Attempt 1**: tempLLM1 → if FAILED → retry
**Attempt 2**: tempLLM2 → if FAILED → retry  
**Attempt 3+**: original LLM (step LLM → preset LLM → orchestrator default)

## Failure Criteria

**For tempLLM purposes**: Only `ExecutionStatus == "FAILED"` counts as failure.
- `COMPLETED` = success (no retry)
- `PARTIAL` = success (no retry)
- `INCOMPLETE` = success (no retry)
- `FAILED` = failure (triggers next attempt)

## Implementation

**Files**:
- `controller_execution.go`: Retry loop, validation check (line 746)
- `controller_agent_factory.go`: LLM selection logic (lines 152-197)

**Key Logic**:
- `isRetryAfterValidationFailure`: Uses `isValidationFailure()` which only checks `ExecutionStatus == "FAILED"`
- `retryAttempt == 1`: tempLLM1 (if available, learnings folder not empty)
- `retryAttempt == 2`: tempLLM2 (if available, learnings folder not empty, NOT blocked by `shouldSkipTempOverride`)
- `retryAttempt >= 3`: Original LLM chain

**Conditions**:
- `learningsFolderEmpty == false`: Required for tempLLM usage
- `shouldSkipTempOverride`: Only blocks tempLLM1, NOT tempLLM2
- `fallbackToOriginalLLMOnFailure`: When enabled, skips tempLLM1 after failure, but tempLLM2 still used on attempt 2

## Validation Status Handling

**Retry Decision**: Uses `IsSuccessCriteriaMet` (line 1477)
- If `IsSuccessCriteriaMet == true`: Stop retry, step passes
- If `IsSuccessCriteriaMet == false`: Continue retry (regardless of status)

**tempLLM Fallback**: Uses `ExecutionStatus == "FAILED"` (line 746)
- Only FAILED triggers tempLLM fallback logic
- PARTIAL/INCOMPLETE with unmet criteria still retry but don't trigger tempLLM fallback

