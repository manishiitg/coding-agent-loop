# Event System Architecture

This document provides a comprehensive overview of the event system in the Multi-Agent Builder, covering type safety, filtering layers, frontend delivery mechanisms, and hierarchical grouping.

---

## 1. Event Type Discriminated Unions

### 📋 Overview

The event type system uses a **TypeScript Discriminated Union** pattern to enforce strict type safety, eliminate unsafe casting, and improve developer experience.

**Key Benefits:**
- **Type Safety**: The compiler guarantees that `event.data.data` matches `event.type`.
- **Zero Casting**: Removes the need for manual `as Type` casts or helper functions.
- **Better IDE Support**: Autocomplete works correctly based on the checked `type`.

### 📊 Wire Format (Actual Structure)

```
PollingEvent (Backend: event_store.go)
├── id: string
├── type: "tool_call_start"          ← Event type discriminator
├── timestamp: ISO string
├── session_id?: string
├── error?: string
└── data: AgentEvent                 ← Unified payload for Orchestrator & MCP
    ├── type: "tool_call_start"      ← Same as parent
    ├── timestamp: ISO string
    ├── event_index: number
    ├── trace_id?: string
    ├── hierarchy_level: number
    ├── correlation_id?: string      ← Used for delegation grouping
    ├── parent_id?: string           ← Used for direct parenting
    └── data: ToolCallStartEvent     ← Actual typed event data
        ├── tool_name: string
        ├── tool_params: object
        └── server_name: string
```

**Key Insight**: Event data is located at `event.data.data`, not `event.data`.

---

## 2. Event Filtering & Storage Flow

The system uses multiple layers of filtering to manage performance, bandwidth, and storage space.

### Layer 1: Emission Filtering (`SKIP_EVENTS`)
**File:** `agent_go/cmd/server/event_bridge/base_bridge.go`  
Events in this map are **discarded immediately** and never reach the memory store or database.
- **Types:** `tool_execution`, `tool_output`, `tool_response`, `tool_call_progress`, and all `cache_*` events.

### Layer 2: Database Storage Filtering (`DB_SKIP_EVENTS`)
**File:** `agent_go/cmd/server/event_bridge/base_bridge.go`  
Events in this map are kept in memory (for real-time SSE and polling) but **never saved to the database**. This saves massive amounts of space and enables "Micro Mode" by default.
- **Types:** `step_progress_updated`, `llm_generation_start`, `conversation_turn`, `streaming_chunk`.
- **CRITICAL EXCEPTION:** `llm_generation_end` and `agent_end` are **NOT** skipped. They are required to clear the "Generating..." state when a session is restored.

### Layer 3: Polling Layer Filtering (`NEVER_SHOW_EVENTS`)
**File:** `agent_go/internal/events/event_store.go`  
Acts as a safety net at the polling layer. It filters out events that might have been stored in the database before `SKIP_EVENTS` was implemented.
- **Types:** Mirrors `SKIP_EVENTS` + ephemeral `streaming_start/chunk` events.

### Layer 4: UI Visibility Filtering (`HIDDEN_EVENTS` & Runtime Logic)
**File:** `agent_go/internal/events/event_store.go` & `EventHierarchy.tsx`  
These events reach the frontend and are available in the state (e.g., for token usage calculations) but are **hidden from the main event list**.
- **Types:** `llm_generation_start`, `agent_start`, `conversation_start`.
- **Runtime Filtering:** `token_usage` events with `context: 'conversation_total'` are stripped from the main view to reduce UI noise.

---

## 3. Delivery Lifecycle: SSE, Resiliency, and Fallback

### Real-Time Delivery (SSEConnection)
The frontend establishes a Server-Sent Events (SSE) connection to receive real-time updates.
- **Store:** Events are deduplicated and stored by session in `useChatStore` (`tabEvents`).
- **Streaming Chunks:** `streaming_chunk` events bypass the database and polling completely. They are delivered exclusively via the SSE buffer for real-time text rendering.

### Auto-Catchup Mechanism
The frontend tracks the highest `event_index` received per session (`tabEventIndices`). 
- On reconnection, the `SSEConnection` sends this index via the `Last-Event-ID` header.
- The backend uses this to automatically replay any events missed during the network drop before resuming real-time delivery.

### Polling Fallback
If the SSE connection fails consecutively (e.g., 5 network failures), the frontend `ChatArea.tsx` orchestration gracefully degrades to HTTP polling (`pollEvents`) to ensure the session remains responsive.

---

## 4. Sub-Agent Isolation & Workflow Grouping

**File:** `frontend/src/components/events/EventHierarchy.tsx`

The UI reconstructs complex multi-agent and workflow executions into an expandable tree using `correlation_id` and `parent_id`.

### Sub-Agent / Delegation Grouping
When a sub-agent is spawned, it generates many events. To prevent these from cluttering the main orchestrator's timeline:
1. The frontend scans for events with a `correlation_id` starting with `delegation-`.
2. These child events are extracted from the flat timeline.
3. They are visually re-parented under the specific `delegation_start` node (rendered as an expandable Sub-Agent Card).

### Parallel Agent Grouping
For concurrent workflows, parallel tool calls are parented under their respective `orchestrator_agent_start` node via `correlation_id`. This guarantees parallel agent tool executions don't incorrectly merge.

### Tool Call Collapsing
To further reduce noise in the main timeline, consecutive tool-related events (`tool_call_start`, `tool_call_end`, `token_usage`, `llm_generation_end`) are collapsed into a single `+ N tool calls` inline button. 
- Because sub-agent events are removed from the flat list (as described above), they never interfere with or get swallowed by the main agent's tool call groups.

---

## 5. Workspace File Operation Events

### 📋 Overview
The `workspace_file_operation` event system replaces frontend Go code parsing with dedicated backend events emitted directly from workspace tool handlers.

#### Event Data Structure
**File**: `agent_go/pkg/workspace/tools.go`

```go
type WorkspaceFileOperationEvent struct {
    BaseEventData
    Operation       string `json:"operation"`        // "read", "update", "delete", "list", "patch", "move"
    Filepath        string `json:"filepath"`         // File path
    Folder          string `json:"folder,omitempty"` // Folder path (for list operations)
    Turn            int    `json:"turn"`
    ServerName      string `json:"server_name"`
    ShouldHighlight bool   `json:"should_highlight,omitempty"`
}
```

**Auto-Highlighting:** `should_highlight` automatically defaults to `false` for files containing "logs/" to reduce sidebar noise.
