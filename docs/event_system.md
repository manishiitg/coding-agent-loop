# Event System Architecture

This document provides a comprehensive overview of the event system in the MCP Agent Builder, covering type safety, event grouping, and workspace file operation events.

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
PollingEvent (event_store.go)
├── id: string
├── type: "tool_call_start"          ← Event type discriminator
├── timestamp: ISO string
├── session_id?: string
├── error?: string
└── data: AgentEvent
    ├── type: "tool_call_start"      ← Same as parent
    ├── timestamp: ISO string
    ├── event_index: number
    ├── trace_id?: string
    ├── hierarchy_level: number
    └── data: ToolCallStartEvent     ← Actual typed event data
        ├── tool_name: string
        ├── tool_params: object
        └── server_name: string
```

**Key Insight**: Event data is at `event.data.data`, not `event.data`.

### 🔧 How to Use

#### Type-Safe Event Handling

```typescript
import { isEventType, getEventData, getTypedEventData } from '../../generated/event-types';
import type { ToolCallStartEvent } from '../../generated/event-types';

// Option 1: Type guard with automatic narrowing (recommended)
if (isEventType(event, 'tool_call_start')) {
  // event is now narrowed to the correct type
  const data = getEventData(event);  // Returns ToolCallStartEvent
  console.log(data.tool_name);  // TypeScript knows this exists!
}

// Option 2: Combined type guard and extraction
const data = getTypedEventData(event, 'tool_call_start');
if (data) {
  console.log(data.tool_name);  // TypeScript knows the type
}
```

#### Available Helper Functions

| Function | Purpose | Returns |
|----------|---------|---------|
| `isEventType(event, type)` | Type guard for narrowing | `boolean` (type predicate) |
| `getEventData(event)` | Extract typed data (use after type guard) | `EventTypeToDataMap[T]` |
| `getTypedEventData(event, type)` | Combined guard + extraction | `EventTypeToDataMap[T] \| undefined` |
| `assertEventType(event, type)` | Assert type (throws if wrong) | `EventTypeToDataMap[T]` |
| `getEventDataOrDefault(event, type, default)` | Safe extraction with fallback | `EventTypeToDataMap[T]` |

### 📁 Key Files

| File | Description |
|------|-------------|
| `mcpagent/events/data.go` | All event struct definitions |
| `mcpagent/events/types.go` | EventType constants |
| `agent_go/cmd/schema-gen/main.go` | JSON Schema generator |
| `agent_go/schemas/polling-event.schema.json` | Generated schema |
| `frontend/src/generated/events-bridge.ts` | Generated TypeScript (from json-schema-to-typescript) |
| `frontend/src/generated/event-types.ts` | Type utilities and discriminated unions |

---

## 2. Workspace File Operation Event

### 📋 Overview

The `workspace_file_operation` event system replaces frontend Go code parsing with dedicated backend events. These events are emitted directly from workspace tool handlers, simplifying architecture and improving reliability.

**Key Benefits:**
- **Separation of Concerns**: Backend knows what happened, frontend just displays
- **No Go Code Parsing**: Eliminates complex parsing logic in frontend
- **Single Event Type**: One event for all workspace operations
- **Better Performance**: No parsing overhead in frontend, O(1) file lookups via index
- **More Reliable**: Backend has exact information about operations

### Backend Implementation

#### Event Data Structure

**File**: `mcpagent/events/data.go`

```go
type WorkspaceFileOperationEvent struct {
    BaseEventData
    Operation       string `json:"operation"`        // "read", "update", "delete", "list", "patch", "move"
    Filepath        string `json:"filepath"`         // File path (empty for list operations)
    Folder          string `json:"folder,omitempty"` // Folder path (for list operations)
    Turn            int    `json:"turn"`
    ServerName      string `json:"server_name"`
    ShouldHighlight bool   `json:"should_highlight,omitempty"` // Whether to highlight this file in the UI
}
```

**Logs Folder Exclusion**: `should_highlight` automatically defaults to `false` for files/folders containing "logs/" in their path.

#### Event Emission

All workspace tool handlers emit events after successful operations:
- **handleReadWorkspaceFile**: Emits "read" event
- **handleUpdateWorkspaceFile**: Emits "update" event
- **handleDeleteWorkspaceFile**: Emits "delete" event
- **handleListWorkspaceFiles**: Emits "list" event
- **handleDiffPatchWorkspaceFile**: Emits "patch" event
- **handleMoveWorkspaceFile**: Emits "move" event

### Frontend Implementation

#### Event Handler

**File**: `frontend/src/stores/useWorkspaceStore.ts`

The `processWorkspaceEvent` function handles `workspace_file_operation` events:

**Operation Handling**:
1. **Read/Update/Patch Operations**:
   - Uses file index for O(1) lookup to check if file exists
   - Highlights file and expands folders to show it
   - Respects `should_highlight` flag

2. **Delete Operations**:
   - Removes file from file tree
   - Rebuilds file index after removal

3. **Move Operations**:
   - Handled as separate delete (source) and update (destination) events

#### Performance Optimizations

- **File Index**: `Map<string, PlannerFile>` provides O(1) lookups instead of O(n) tree traversal
- **Index Keys**: Files indexed by `filepath`, `originalFilepath`, and filename
- **Auto-Rebuild**: Index rebuilds automatically when files change

---

## 3. Step-Based Event Grouping

### Objective
Add mode-aware event grouping: agent sessions in chat mode, step-based grouping in workflow mode.

### Implementation Details

#### 1. Mode Detection
```typescript
import { useModeStore } from '../../stores/useModeStore';
const { selectedModeCategory } = useModeStore();
const isWorkflowMode = selectedModeCategory === 'workflow';
```

#### 2. Step Key Extraction
Create `getStepKey` function similar to `getAgentSessionKey`:
- Extract `step_id` from `step_execution_start` and `step_execution_end` events
- Handle nested data structures
- Return `step_session:${step_id}` or null

#### 3. Step Event Grouping
Create `findEventsBetweenStepStartEnd` memo:
- Map step_id -> Set of event IDs between start/end
- Only process `step_execution_start` and `step_execution_end` events

#### 4. UI Integration
Update `renderEventNode` in `EventHierarchy.tsx`:
- For `step_execution_start` events in workflow mode:
  - Add collapse/expand button (similar to agent sessions)
  - Show event count when collapsed
  - Pass `isCollapsed`, `eventCount`, `onToggleCollapse` to EventDispatcher

#### 5. Auto-Collapse Logic
For workflow mode:
- Keep current step (most recent `step_execution_start`) expanded
- Collapse completed steps (have `step_execution_end`)
- Track step completion order
- Respect manually expanded steps

### Data Structure
Step events have:
- `step_id`: string (required identifier)
- `step_index`: number (0-based)
- `step_title`: string
- `step_path`: string (e.g., "step-1" or "step-1-if-true-0")
