# Streaming LLM Output

Real-time streaming of LLM text generation from the backend agent to the frontend UI.

## Overview

The mcpagent library emits per-token streaming events during LLM text generation. These events flow through the existing event pipeline (Agent -> EventStore -> Polling API -> Frontend) and are intercepted by the frontend to display incrementally growing text in the chat UI.

Streaming applies only to **LLM text output**. Tool calls flow through separate `tool_call_start`/`tool_call_end` events and are not streamed.

## Architecture

```
User sends message -> POST /api/query
    |
Backend spawns goroutine -> Agent processes with EnableStreaming=true
    |
LLM generates tokens -> streaming_chunk events emitted (text only)
    |
EventObserver -> EventStore (in-memory only, NOT persisted to DB)
    |
Frontend polls every 500ms -> receives streaming events
    |
ChatArea intercepts chunks -> appends to streamingText[sessionId] in Zustand store
    |
EventDisplay reads streamingText -> renders growing markdown block with cursor
    |
LLM finishes -> streaming_end event -> text persists until llm_generation_end
    |
llm_generation_end event -> clears streaming text, full response in event list
```

## Event Types

Three streaming event types are emitted by the agent:

| Event | When | Data |
|-------|------|------|
| `streaming_start` | LLM generation begins | (none) |
| `streaming_chunk` | Each token/chunk produced | `content` (string), `chunk_index` (int), `is_tool_call` (bool) |
| `streaming_end` | LLM generation finishes | (none) |

### Event JSON Structure

Streaming events follow the standard event wire format:

```json
{
  "id": "session_streaming_chunk_1234_567",
  "type": "streaming_chunk",
  "timestamp": "2025-01-15T10:30:00Z",
  "session_id": "abc-123",
  "data": {
    "type": "streaming_chunk",
    "timestamp": "2025-01-15T10:30:00Z",
    "event_index": 5,
    "hierarchy_level": 1,
    "data": {
      "content": "Hello, how can I ",
      "chunk_index": 3,
      "is_tool_call": false
    }
  }
}
```

The content is at `event.data.data.content` (outer Event -> AgentEvent -> StreamingChunkEvent).

## Backend Implementation

### Enabling Streaming

**File:** `agent_go/pkg/agentwrapper/llm_agent.go` (line ~354)

```go
// Enable streaming for LLM text responses
options = append(options, mcpagent.WithStreaming(true))
```

This single option causes the mcpagent library to emit `streaming_start`, `streaming_chunk`, and `streaming_end` events during LLM text generation.

### Event Flow

1. Agent emits streaming events via its EventObserver
2. `EventObserver.HandleEvent()` stores them in the in-memory `EventStore`
3. The polling API (`handleGetSessionEvents`) returns them alongside other events
4. Streaming events are **not filtered** by event mode (basic/advanced/tiny/micro) -- they pass through all modes

### Database Exclusion

**File:** `agent_go/pkg/database/event_integration.go`

Streaming events are explicitly skipped from database persistence in both observer methods:

```go
// In OnEvent:
if event.Type == events.StreamingStart || event.Type == events.StreamingChunk || event.Type == events.StreamingEnd {
    return
}

// In HandleEvent:
if event.Type == events.StreamingStart || event.Type == events.StreamingChunk || event.Type == events.StreamingEnd {
    return nil
}
```

This is intentional:
- Streaming events are **ephemeral** -- only needed during active generation
- Persisting them causes "Unknown Event Type" errors on page reload
- The full response is captured in `llm_generation_end` which IS persisted

## Frontend Implementation

### State Management

**File:** `frontend/src/stores/useChatStore.ts`

Per-session streaming text is tracked in the Zustand store:

```typescript
// State
streamingText: Record<string, string>           // sessionId -> accumulated text
lastStreamingChunkIndex: Record<string, number>  // sessionId -> last chunk_index (dedup)

// Actions
appendStreamingChunk: (sessionId: string, chunkIndex: number, chunk: string) => void
clearStreamingText: (sessionId: string) => void
```

Key behaviors:
- **Deduplication:** `lastStreamingChunkIndex` tracks the last processed `chunk_index` per session. If a chunk arrives with an index <= the last processed index, it's skipped. This handles overlapping 500ms poll intervals.
- **Type safety:** `appendStreamingChunk` rejects non-string or empty chunks at runtime.
- **Cleanup:** `clearStreamingText` removes both the text and the chunk index tracker.

### Event Interception

**File:** `frontend/src/components/ChatArea.tsx` (in the `pollEvents` callback)

Streaming events are intercepted in the polling event loop **before** they reach the event list:

```typescript
let hasCompletionEvent = false
for (const event of eventsBeforeFilter) {
  if (event.type === 'streaming_start') {
    chatStore.clearStreamingText(actualSessionId)
    continue  // Don't add to event list
  }
  if (event.type === 'streaming_chunk') {
    // Extract content from event.data.data (AgentEvent -> StreamingChunkEvent)
    chatStore.appendStreamingChunk(actualSessionId, chunkIndex, content)
    continue
  }
  if (event.type === 'streaming_end') {
    continue  // Text persists until completion event
  }

  // Track completion events -- clear is deferred (see below)
  if (event.type === 'llm_generation_end' || event.type === 'conversation_end') {
    hasCompletionEvent = true
  }

  newEvents.push(event)  // Only non-streaming events reach the event list
}

// Defer clearing so React renders the streaming card at least once.
// Without this, chunks appended and then cleared in the same synchronous loop
// would never be rendered (React batches state updates).
if (hasCompletionEvent) {
  const sid = actualSessionId
  const textBeforeClear = useChatStore.getState().streamingText[sid]
  requestAnimationFrame(() => {
    if (useChatStore.getState().streamingText[sid] === textBeforeClear) {
      useChatStore.getState().clearStreamingText(sid)
    }
  })
}
```

This keeps the event list clean -- no hundreds of chunk events cluttering the UI.

### Streaming Text Display

**File:** `frontend/src/components/EventDisplay.tsx`

The streaming text card appears between the event list and the final response:

```tsx
{currentStreamingText && (
  <Card className="border-blue-200 bg-blue-50 ...">
    <CardContent>
      <div className="flex items-center gap-1.5 mb-1">
        <div className="w-1.5 h-1.5 bg-blue-500 rounded-full animate-pulse" />
        <span>Generating...</span>
      </div>
      <ReactMarkdown>{currentStreamingText}</ReactMarkdown>
      <span className="inline-block w-1.5 h-3 bg-blue-500 animate-pulse" />
    </CardContent>
  </Card>
)}
```

Features:
- Blue card with pulsing "Generating..." indicator
- Markdown rendering of accumulated text
- Animated cursor at the end of the text
- Automatically disappears when streaming text is cleared

The component subscribes to the session-specific streaming text:

```typescript
const currentStreamingText = useChatStore(state =>
  sessionId ? state.streamingText[sessionId] || '' : ''
)
```

## Multi-Session Support

Streaming works independently per session. Each session has its own:
- `streamingText[sessionId]` accumulator
- `lastStreamingChunkIndex[sessionId]` dedup tracker

Multiple chat tabs can stream concurrently without interference. The `sessionId` prop passed to `EventDisplay` determines which session's streaming text is shown.

## Polling Configuration

The polling interval is set to **500ms** for streaming responsiveness:

```typescript
// In useChatStore.ts, startPolling:
}, 500)  // 500ms for streaming responsiveness
```

This balances UI responsiveness against network overhead. At 500ms, streaming text updates appear near-real-time while keeping API calls manageable.

## Lifecycle

1. **Start:** `streaming_start` event arrives -> `clearStreamingText(sessionId)` resets any previous text
2. **Accumulate:** `streaming_chunk` events arrive -> text appended to `streamingText[sessionId]`
3. **End:** `streaming_end` event arrives -> text persists (not cleared immediately)
4. **Cleanup:** Completion event (`llm_generation_end`, `conversation_end`, etc.) arrives -> deferred `clearStreamingText(sessionId)` via `requestAnimationFrame` removes the streaming card after one render cycle

The deferred clear is critical: if chunks and completion events arrive in the same poll batch (common for short responses or when polling restarts late), synchronous clearing would prevent React from ever rendering the streaming card. The `requestAnimationFrame` ensures at least one paint before clearing. A text comparison guard prevents the deferred clear from accidentally clearing a new generation's streaming text.

## Troubleshooting

### "Unknown Event Type: streaming_chunk" on page reload
Streaming events were persisted to the database from before the DB exclusion fix. Clear the database or the affected session's events.

### No streaming text appears
- Check browser console for `[ChatArea] streaming_chunk with no content:` warnings -- this logs the raw event structure when content extraction fails
- Verify the backend was recompiled with `WithStreaming(true)`
- Verify polling is active (tab must be streaming or in active sessions list)

### Duplicate text
The `lastStreamingChunkIndex` deduplication should prevent this. If it occurs, check if `chunk_index` values are being emitted correctly by the agent (should be sequential starting from 0).
