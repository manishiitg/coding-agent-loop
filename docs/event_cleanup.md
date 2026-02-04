# Event Cleanup - Progress

## Bug Fix: Cache Events Still Showing

**Issue:** Cache events were appearing in micro mode after recent changes.

**Root Cause:** The frontend `eventModeUtils.ts` had `cache_event` and `comprehensive_cache_event` removed from `ADVANCED_MODE_EVENTS` with the assumption that backend `SKIP_EVENTS` would handle it. But:
1. `SKIP_EVENTS` in `base_bridge.go` only prevents **new** events from being emitted
2. Old events already stored in database were still being sent to frontend
3. Frontend no longer filtered them out

**Fix Applied:**
1. Added `NEVER_SHOW_EVENTS` map in `event_store.go` - filters at polling layer regardless of source
2. Restored cache events to frontend `ADVANCED_MODE_EVENTS` as safety net

---

## Summary of Changes Made

### Backend: Events SKIPPED (not emitted)

**File:** `agent_go/cmd/server/event_bridge/base_bridge.go`

```go
var SKIP_EVENTS = map[string]bool{
    // Tool extras - no UI component
    "tool_execution":     true,
    "tool_output":        true,
    "tool_response":      true,
    "tool_call_progress": true,
    // Cache events - all 9
    "cache_event":               true,
    "comprehensive_cache_event": true,
    "cache_hit":                 true,
    "cache_miss":                true,
    "cache_write":               true,
    "cache_expired":             true,
    "cache_cleanup":             true,
    "cache_error":               true,
    "cache_operation_start":     true,
}
```

### Backend: Events NEVER SHOWN (filtered at polling layer)

**File:** `agent_go/internal/events/event_store.go`

```go
var NEVER_SHOW_EVENTS = map[string]bool{
    // Same list as SKIP_EVENTS - catches old events in database
    "tool_execution":            true,
    "tool_output":               true,
    "tool_response":             true,
    "tool_call_progress":        true,
    "cache_event":               true,
    "comprehensive_cache_event": true,
    "cache_hit":                 true,
    "cache_miss":                true,
    "cache_write":               true,
    "cache_expired":             true,
    "cache_cleanup":             true,
    "cache_error":               true,
    "cache_operation_start":     true,
}
```

### Frontend: Safety Net Filter

**File:** `frontend/src/components/events/eventModeUtils.ts`

```typescript
export const ADVANCED_MODE_EVENTS = new Set([
  'llm_generation_start',
  'llm_generation_with_retry',
  'conversation_start',
  'conversation_turn',
  'step_progress_updated',
  // Cache events - still filter on frontend as safety net
  'cache_event',
  'comprehensive_cache_event',
]);
```

---

## Event Filtering Flow

```
New Event → SKIP_EVENTS (base_bridge.go) → Not stored
                    ↓
Old Event in DB → ShouldShowEventByMode() → NEVER_SHOW_EVENTS → Not sent
                    ↓
Event reaches frontend → ADVANCED_MODE_EVENTS → Not displayed
```

---

## Files Deleted

- `frontend/src/components/events/debug/WorkspaceFileOperationEvent.tsx` - Dead code, event never emitted
