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

### Backend: HIDDEN_EVENTS (sent to frontend but hidden from event list)

**File:** `agent_go/internal/events/event_store.go`

These events reach the frontend (available in `tabEvents` for ChatInput context circle etc.) but are hidden from the event display list by the frontend's `HIDDEN_EVENTS` in `eventModeUtils.ts`.

```go
var HIDDEN_EVENTS = map[string]bool{
    "llm_generation_start":      true,
    "llm_generation_with_retry": true,
    "conversation_start":        true,
    "conversation_turn":         true,
    "system_prompt":             true,
    "agent_start":               true,
    "agent_error":               true,
    "batch_execution_canceled":  true,
}
```

**Note:** `llm_generation_end` is NOT in `HIDDEN_EVENTS` — ChatInput uses it as a fallback source for the context usage circle (token_usage with `conversation_total` is not emitted by all providers). The frontend hides it from the event list display via its own `HIDDEN_EVENTS` in `eventModeUtils.ts`, and it gets grouped with tool calls by the tool call collapsing system.

---

## Tool Call Grouping & Collapsing

**File:** `frontend/src/components/events/EventHierarchy.tsx`

Tool calls are collapsed by default (`hideToolCalls: true` on each `ChatTab`) to reduce noise. The system groups consecutive collapsible events and replaces each group with a `"+ N tool calls"` button.

### Collapsible Event Types

```typescript
const TOOL_CALL_TYPES = new Set([
  'tool_call_start', 'tool_call_end', 'tool_call_error',
  'token_usage', 'llm_generation_end'
]);
```

### How Grouping Works

1. Walk through `displayEvents` sequentially
2. Consecutive runs of `TOOL_CALL_TYPES` events form a group
3. Any non-collapsible event (e.g. `user_message`, `delegation_start`) breaks the group
4. Each hidden group is replaced with an inline `"+ N tool calls"` sentinel
5. Clicking `"+"` expands only that specific group (per-group state, not global toggle)
6. Clicking `"−"` after an expanded group collapses it back

### Sub-Agent Isolation

Sub-agent events (with `correlation_id` starting with `delegation-`) are excluded from grouping. They pass through to the tree builder which re-parents them into their delegation cards. This prevents sub-agent tool calls from appearing in the main agent's collapsed groups.

### Multi-Tab Independence

Each `EventHierarchy` receives a `tabId` prop and looks up its own tab via `state.chatTabs[tabId]` (not `getActiveTab()`). This ensures expand/collapse state is independent per chat panel in multi-chat layouts.

---

## Event Filtering Flow

```
New Event → SKIP_EVENTS (base_bridge.go) → Not stored
                    ↓
Old Event in DB → ShouldShowEvent() → NEVER_SHOW_EVENTS → Not sent
                    ↓                → HIDDEN_EVENTS → Not sent
                    ↓
Event reaches frontend → HIDDEN_EVENTS (eventModeUtils.ts) → Not displayed in event list
                    ↓
Displayed events → Tool Call Grouping (EventHierarchy) → Collapsed into "+ N tool calls"
```

---

## Files Deleted

- `frontend/src/components/events/debug/WorkspaceFileOperationEvent.tsx` - Dead code, event never emitted
- `frontend/src/components/events/ToolCallToggle.tsx` - Replaced by inline sentinels in EventHierarchy
