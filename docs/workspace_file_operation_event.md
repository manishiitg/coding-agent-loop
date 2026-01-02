# Workspace File Operation Event

## ✅ IMPLEMENTATION COMPLETE

The workspace file operation event system has been fully implemented. Frontend Go code parsing has been replaced with dedicated backend events that are emitted directly from workspace tool handlers, simplifying the architecture and improving reliability.

---

## 📋 Overview

This document describes the `workspace_file_operation` event system that replaces frontend Go code parsing. The backend now emits dedicated events directly from workspace tool handlers, eliminating the need for the frontend to parse Go code to detect file operations.

**Key Benefits:**
- **Separation of Concerns**: Backend knows what happened, frontend just displays
- **No Go Code Parsing**: Eliminates complex parsing logic in frontend
- **Single Event Type**: One event for all workspace operations
- **Better Performance**: No parsing overhead in frontend, O(1) file lookups via index
- **More Reliable**: Backend has exact information about operations
- **Works with Workflow Folders**: Seamlessly integrates with existing workflow folder filtering
- **Optimized Lookups**: File index provides O(1) lookups instead of O(n) tree traversal

---

## Architecture

### Previous Architecture (Problems)

1. Frontend parsed Go code from `write_code` tool to find workspace tool calls
2. Frontend listened to `tool_call_start`/`tool_call_end` for workspace tools
3. Frontend had Go code parsing logic (`goCodeParser.ts`)
4. Frontend determined which files to highlight
5. Tight coupling: frontend needed to understand Go code structure

### Current Architecture (Solution)

1. Backend emits dedicated `workspace_file_operation` events from workspace tool handlers
2. Frontend listens to single event type for all workspace operations
3. No Go code parsing needed in frontend
4. Cleaner separation of concerns

---

## Backend Implementation

### Event Type

**File**: `mcpagent/events/types.go`
- Event type constant: `WorkspaceFileOperation EventType = "workspace_file_operation"`
- Component type: Returns "tool" in `GetComponentFromEventType()`

### Event Data Structure

**File**: `mcpagent/events/data.go`

```go
type WorkspaceFileOperationEvent struct {
    BaseEventData
    Operation       string `json:"operation"`        // "read", "update", "delete", "list", "patch", "move"
    Filepath        string `json:"filepath"`         // File path (empty for list operations)
    Folder          string `json:"folder,omitempty"` // Folder path (for list operations)
    Turn            int    `json:"turn"`
    ServerName      string `json:"server_name"`
    ShouldHighlight bool   `json:"should_highlight,omitempty"` // Whether to highlight this file in the UI (default: true)
}
```

**Key Features:**
- `should_highlight` field: Excludes logs/ folder and its subfolders from highlighting
- Defaults to `true` for backward compatibility
- Constructor: `NewWorkspaceFileOperationEvent(operation, filepath, folder string, turn int, serverName string, shouldHighlight ...bool)`

### Event Emitter Interface

**File**: `agent_go/cmd/server/virtual-tools/workspace_tools.go`

```go
type WorkspaceEventEmitter interface {
    HandleEvent(ctx context.Context, event *events.AgentEvent) error
}
```

**Helper Functions:**
- `getEventEmitterFromContext(ctx context.Context) WorkspaceEventEmitter`
- `getTurnFromContext(ctx context.Context) int`
- `getServerNameFromContext(ctx context.Context) string`
- `emitWorkspaceFileOperation(ctx context.Context, operation, filepath, folder string)`

### Event Emission

All workspace tool handlers emit events after successful operations:

- **handleReadWorkspaceFile**: Emits "read" event
- **handleUpdateWorkspaceFile**: Emits "update" event
- **handleDeleteWorkspaceFile**: Emits "delete" event
- **handleListWorkspaceFiles**: Emits "list" event
- **handleDiffPatchWorkspaceFile**: Emits "patch" event
- **handleMoveWorkspaceFile**: Emits "move" event (source filepath)

**Event Timing**: Events are emitted on successful operation completion (after API call succeeds, before returning).

**Logs Folder Exclusion**: The `emitWorkspaceFileOperation` function automatically sets `should_highlight=false` for files/folders containing "logs/" in their path.

### Emitter Injection

**Base Orchestrator** (`agent_go/pkg/orchestrator/base_orchestrator.go`):
- Injects `contextAwareBridge` as emitter in 6 locations:
  - `WrapWorkspaceToolsWithFolderGuard` (folder guard wrapper)
  - `ReadWorkspaceFile`
  - `WriteWorkspaceFile`
  - `DeleteWorkspaceFile`
  - `CleanupDirectory` (for listExecutor call)
  - `ListWorkspaceDirectories`
  - `ListWorkspaceFiles`

**Agent Tool Calls** (`mcpagent/agent/conversation.go`):
- Injects emitter, turn, and server name into context before executing workspace tools
- Agent struct implements `HandleEvent` method to satisfy `WorkspaceEventEmitter` interface:

```go
func (a *Agent) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
    if event != nil && event.Data != nil {
        a.EmitTypedEvent(ctx, event.Data)
    }
    return nil
}
```

**Critical Fix**: Without the `HandleEvent` method implementation, type assertion in `workspace_tools.go` would fail silently, and events would not emit from agent tool calls.

---

## Frontend Implementation

### Event Type Definition

**File**: `frontend/src/generated/events-bridge.ts` and `frontend/src/generated/event-types.ts`

The event type is automatically included in generated TypeScript types:

```typescript
interface WorkspaceFileOperationEvent {
  operation?: string  // 'read' | 'update' | 'delete' | 'list' | 'patch' | 'move'
  filepath?: string
  folder?: string
  turn?: number
  server_name?: string
  should_highlight?: boolean
  // ... other BaseEventData fields
}
```

### Event Handler

**File**: `frontend/src/stores/useWorkspaceStore.ts`

The `processWorkspaceEvent` function handles `workspace_file_operation` events:

**Event Extraction**:
- Uses type-safe `getTypedEventData()` helper from `event-types.ts`
- Falls back to nested data access if typed extraction fails
- Handles multiple event structure variations

**File Index for Performance**:
- **File Index**: `Map<string, PlannerFile>` provides O(1) lookups instead of O(n) tree traversal
- **Index Keys**: Files indexed by `filepath`, `originalFilepath`, and filename
- **Auto-Rebuild**: Index rebuilds automatically when files change:
  - `setFiles()` - when files are fetched
  - `removeFile()` - after file removal
  - `addFile()` - after file addition
  - `updateFile()` - after file update
- **Performance Impact**: File existence checks are now O(1) instead of O(n)

**Operation Handling**:

1. **Read/Update/Patch Operations**:
   - Uses file index for O(1) lookup to check if file exists
   - If file doesn't exist (new file), refreshes file tree first
   - Highlights file and expands folders to show it
   - Respects `should_highlight` flag (skips highlighting if false)

2. **Delete Operations**:
   - Removes file from file tree
   - Rebuilds file index after removal
   - Clears selection if deleted file was selected

3. **List Operations**:
   - No highlighting needed (folder browsing)

4. **Move Operations**:
   - Handled as separate delete (source) and update (destination) events
   - Destination file is highlighted after move completes

**Key Implementation Details**:
- Backend emits full filepaths (e.g., "Workflow/MyProject/file.txt")
- `highlightFile` uses file index for O(1) lookups in raw unfiltered files
- Workspace component handles filtering and path adjustment for display
- `expandFoldersForFile` ensures files are visible even when workflow folder is filtered
- File index eliminates need for recursive tree traversal on every lookup

### Event Processing Flow

```
Backend workspace tool handler
  └── emitWorkspaceFileOperation(ctx, operation, filepath, folder)
  └── Creates WorkspaceFileOperationEvent
  └── Emits via context emitter
  └── Event stored in event store
  └── Frontend polling receives event
  └── ChatArea calls processWorkspaceEvent(event)
  └── useWorkspaceStore processes event
  └── Highlights file or removes from tree
```

---

## Workflow Folder Integration

### How Workflow Folder Opening Works

1. When a workflow preset is selected, it has a `selectedFolder.filepath` (e.g., "Workflow/MyProject")
2. Frontend `Workspace.tsx` extracts this as `workflowFolderPath`
3. Files are filtered to show only the workflow folder when in workflow mode
4. When workflow opens, folders are auto-expanded
5. `applyPreset` in `useGlobalPresetStore.ts` calls `expandFoldersForFile(folderPath)`

### Integration with Event System

- **Backend emits full filepaths**: Events contain full paths like "Workflow/MyProject/file.txt"
- **Frontend `highlightFile` uses file index**: Uses `fileIndex.has(filepath)` for O(1) lookup instead of O(n) tree traversal
- **Workspace component handles filtering**: Automatically filters to workflow folder when `workflowFolderPath` is set
- **Path adjustment for display**: Workspace component adjusts paths for display (removes workflow folder prefix)
- **Auto-expansion**: `expandFoldersForFile` is called to ensure file is visible in filtered view
- **Performance**: File index eliminates recursive tree searches, providing O(1) lookups even with large file trees

### Key Points

- No path conversion needed in event handler - backend emits full paths, frontend searches in raw tree
- Workspace component's filtering logic handles workflow folder display automatically
- `expandFoldersForFile` ensures files are visible even when workflow folder is filtered
- This works seamlessly with existing workflow folder filtering and auto-expansion

---

## Features

### Should Highlight Flag

The `should_highlight` field allows selective highlighting:
- **Default**: `true` (all files are highlighted)
- **Logs Exclusion**: Automatically set to `false` for files/folders containing "logs/" in path
- **Use Cases**: Prevents highlighting of log files, temporary files, or other non-user-facing files

### Move Operation Handling

Move operations emit two separate events:
1. **Delete event** for source file (removes from tree)
2. **Update event** for destination file (highlights new location)

This approach provides clear separation and works well with the existing event handling logic.

### Error Handling

- Events are only emitted on successful operations
- Errors are handled by `tool_call_error` events (separate event type)
- Frontend gracefully handles missing event data with fallback extraction methods
- No breaking changes if event structure varies

### Performance Optimizations

**File Index System** (Added in performance optimization):
- **O(1) Lookups**: File index (`Map<string, PlannerFile>`) provides constant-time lookups
- **Index Keys**: Files indexed by:
  - `filepath` (adjusted path in workflow mode)
  - `originalFilepath` (original path)
  - Filename (for relative filename lookups)
- **Auto-Rebuild**: Index rebuilds automatically on mutations (O(n) cost, but mutations are infrequent)
- **Performance Impact**: 
  - Before: O(n) tree traversal for every file lookup
  - After: O(1) Map lookup for file existence checks
  - Net improvement: Significant performance gain, especially with large file trees

**Reduced Logging**:
- Removed excessive console.log statements from event processing
- Only error logs remain for debugging
- Reduces overhead during high-frequency event processing

---

## Files Modified

### Backend:
- `mcpagent/events/types.go` - Event type constant
- `mcpagent/events/data.go` - Event data structure and constructor
- `agent_go/cmd/server/virtual-tools/workspace_tools.go` - Helpers and event emission
- `agent_go/pkg/orchestrator/base_orchestrator.go` - Emitter injection in orchestrator methods
- `mcpagent/agent/conversation.go` - Emitter injection for agent tool calls
- `mcpagent/agent/agent.go` - `HandleEvent` method implementation

### Frontend:
- `frontend/src/stores/useWorkspaceStore.ts` - Event handler implementation with file index optimization
- `frontend/src/generated/events-bridge.ts` - Generated TypeScript types
- `frontend/src/generated/event-types.ts` - Type-safe event utilities
- `frontend/src/components/ChatArea.tsx` - Event processing integration
- `frontend/src/components/Workspace.tsx` - Workspace UI component (removed excessive logging)
- `frontend/src/components/workspace/PlannerFileList.tsx` - File list component (removed excessive logging)

---

## Testing

### Tested Scenarios

1. ✅ Direct workspace tool calls (read, update, delete) emit events
2. ✅ Workspace tools called from `write_code` execution emit events
3. ✅ Orchestrator direct calls (ReadWorkspaceFile, WriteWorkspaceFile) emit events
4. ✅ Frontend receives and processes events correctly
5. ✅ File highlighting works for all operation types
6. ✅ File tree refresh on new file creation
7. ✅ File removal on delete operations
8. ✅ Workflow folder filtering works with new events
9. ✅ File highlighting in workflow mode (filtered view)
10. ✅ Folder expansion when files are highlighted in workflow mode
11. ✅ Logs folder exclusion (should_highlight=false)
12. ✅ Move operation handling (delete + update events)

---

## Migration Notes

- **Backward Compatible**: Backend changes are backward compatible (new event, doesn't break existing)
- **Frontend Changes**: Removed parsing logic but kept fallback behavior for robustness
- **Parallel Systems**: Both systems can run in parallel during transition
- **Cleanup**: Old frontend parsing logic can be removed after verification
- **No Breaking Changes**: No breaking changes to existing functionality

---

## Implementation Status

### ✅ COMPLETED - All Phases Implemented

**Phase 1-2: Backend Event Infrastructure & Emission** - ✅ COMPLETE
- Event type constant added
- Event struct with `should_highlight` field
- All workspace tool handlers emit events
- Logs folder exclusion logic implemented

**Phase 3: Emitter Injection** - ✅ COMPLETE
- Orchestrator methods inject `contextAwareBridge` (6 locations)
- Folder guard wrapper injects emitter
- Agent tool calls inject emitter via context
- Agent `HandleEvent` method implemented

**Phase 4: Frontend Event Handling** - ✅ COMPLETE
- `useWorkspaceStore.ts` handles `workspace_file_operation` events
- Type-safe event extraction using `getTypedEventData()`
- Fallback extraction methods for robustness
- `ChatArea.tsx` calls `processWorkspaceEvent` for each event
- All operation types handled correctly

**Phase 5: Verification** - ✅ COMPLETE
- All test scenarios verified
- Works with direct tool calls
- Works with `write_code` execution
- Works with workflow folder filtering
- All operation types work correctly

**Phase 6: Performance Optimization** - ✅ COMPLETE
- File index system implemented for O(1) lookups
- Index rebuilds automatically on file mutations
- Replaced O(n) tree searches with O(1) Map lookups
- Removed excessive console logging
- Optimized highlight checks and file existence lookups

---

## Benefits Achieved

1. ✅ **Separation of Concerns**: Backend knows what happened, frontend just displays
2. ✅ **No Go Code Parsing**: Eliminated complex parsing logic in frontend
3. ✅ **Single Event Type**: One event for all workspace operations
4. ✅ **Easier to Extend**: Adding new operations just requires emitting event
5. ✅ **Better Performance**: No parsing overhead in frontend, O(1) file lookups via index
6. ✅ **More Reliable**: Backend has exact information about operations
7. ✅ **Works with Workflow Folders**: Seamlessly integrates with existing workflow folder filtering
8. ✅ **Selective Highlighting**: `should_highlight` flag allows excluding non-user-facing files
9. ✅ **Optimized Lookups**: File index provides O(1) lookups instead of O(n) tree traversal
10. ✅ **Reduced Overhead**: Minimal logging reduces console overhead during high-frequency events

---

## Future Enhancements

1. Additional operation types (if needed)
2. Batch operation events (for multiple file operations)
3. Operation metadata (file size, modification time, etc.)
4. Operation history tracking
5. Undo/redo support based on event history
6. Debouncing/throttling for rapid event sequences (if needed)
7. Virtual scrolling for very large file trees (if needed)