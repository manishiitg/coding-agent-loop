# Workspace File Operation Event Implementation Plan

## Overview
Replace frontend Go code parsing with a dedicated backend event (`workspace_file_operation`) that is emitted directly from workspace tool handlers. This simplifies the architecture and moves file operation detection to the backend, eliminating the need for frontend to parse Go code.

## Current Architecture (Problems)
1. Frontend parses Go code from `write_code` tool to find workspace tool calls
2. Frontend listens to `tool_call_start`/`tool_call_end` for workspace tools
3. Frontend has Go code parsing logic (`goCodeParser.ts`)
4. Frontend determines which files to highlight
5. Tight coupling: frontend needs to understand Go code structure

## Proposed Architecture (Solution)
1. Backend emits dedicated `workspace_file_operation` events from workspace tool handlers
2. Frontend listens to single event type for all workspace operations
3. No Go code parsing needed in frontend
4. Cleaner separation of concerns

## Backend Changes

### 1. Add New Event Type
**File**: `mcpagent/events/types.go`
- Add constant: `WorkspaceFileOperation EventType = "workspace_file_operation"` (around line 29, with other tool events)
- Update `GetComponentFromEventType()` to return "tool" for this event type (around line 242)

### 2. Add Event Data Structure
**File**: `mcpagent/events/data.go`
- Add `WorkspaceFileOperationEvent` struct after `ToolCallEndEvent` (around line 305):
  ```go
  type WorkspaceFileOperationEvent struct {
      BaseEventData
      Operation  string `json:"operation"`  // "read", "update", "delete", "list", "patch", "move"
      Filepath   string `json:"filepath"`  // File path (empty for list operations)
      Folder     string `json:"folder,omitempty"` // Folder path (for list operations)
      Turn       int    `json:"turn"`
      ServerName string `json:"server_name"`
  }
  ```
- Add `GetEventType()` method returning `WorkspaceFileOperation`
- Add constructor: `NewWorkspaceFileOperationEvent(operation, filepath, folder string, turn int, serverName string) *WorkspaceFileOperationEvent`

### 3. Add Event Emitter Interface and Helpers
**File**: `agent_go/cmd/server/virtual-tools/workspace_tools.go`
- Add interface at top of file:
  ```go
  type WorkspaceEventEmitter interface {
      EmitTypedEvent(ctx context.Context, eventData events.EventData) error
  }
  ```
- Add helper functions:
  ```go
  func getEventEmitterFromContext(ctx context.Context) WorkspaceEventEmitter
  func getTurnFromContext(ctx context.Context) int
  func getServerNameFromContext(ctx context.Context) string
  func emitWorkspaceFileOperation(ctx context.Context, operation, filepath, folder string)
  ```

### 4. Modify Workspace Tool Handlers
**File**: `agent_go/cmd/server/virtual-tools/workspace_tools.go`

Emit events after successful API calls, before returning:

- **handleReadWorkspaceFile** (after line 673, before return): 
  ```go
  emitWorkspaceFileOperation(ctx, "read", filepathStr, "")
  ```

- **handleUpdateWorkspaceFile** (after line 908, before return):
  ```go
  emitWorkspaceFileOperation(ctx, "update", filepath, "")
  ```

- **handleDeleteWorkspaceFile** (after line 1149, before return):
  ```go
  emitWorkspaceFileOperation(ctx, "delete", filepath, "")
  ```

- **handleListWorkspaceFiles** (after line 587, before return):
  ```go
  emitWorkspaceFileOperation(ctx, "list", "", folder)
  ```

- **handleDiffPatchWorkspaceFile** (after line 1762, before return):
  ```go
  emitWorkspaceFileOperation(ctx, "patch", filepath, "")
  ```

- **handleMoveWorkspaceFile** (after line 1237, before return):
  ```go
  // Emit for both source and destination, or create separate event structure
  emitWorkspaceFileOperation(ctx, "move", sourceFilepath, "")
  // Could also emit second event for destination, or extend event structure
  ```

### 5. Inject Event Emitter in Base Orchestrator
**File**: `agent_go/pkg/orchestrator/base_orchestrator.go`

Inject emitter into context before calling executors:

- **WrapWorkspaceToolsWithFolderGuard** (line 795, in wrappedExecutor function):
  ```go
  // Before calling originalExecutor (line 881):
  ctx = context.WithValue(ctx, "workspace_event_emitter", bo.contextAwareBridge)
  return originalExecutor(ctx, args)
  ```

- **ReadWorkspaceFile** (before line 1719):
  ```go
  ctx = context.WithValue(ctx, "workspace_event_emitter", bo.contextAwareBridge)
  readResult, err := readExecutor(ctx, readArgs)
  ```

- **WriteWorkspaceFile** (before line 2012): Inject emitter
- **DeleteWorkspaceFile** (before line 2042): Inject emitter
- **CleanupDirectory** (before line 2075): Inject emitter for listExecutor call
- **ListWorkspaceDirectories** (before line 2206): Inject emitter
- **ListWorkspaceFiles** (before line 2296): Inject emitter

### 6. Inject Event Emitter in Agent Tool Calls
**File**: `mcpagent/agent/conversation.go`
- Around line 800-900, before executing workspace tools, inject emitter and turn into context:
  ```go
  // Before tool execution (around line 800)
  ctx = context.WithValue(ctx, "workspace_event_emitter", a)
  ctx = context.WithValue(ctx, "turn", turn+1)
  ctx = context.WithValue(ctx, "server_name", serverName)
  ```

## Frontend Changes

### 7. Add Event Type to Generated Types
**File**: Frontend will need to regenerate types after backend changes
- The event type will be automatically included in polling events
- Add TypeScript interface in generated events if needed:
  ```typescript
  interface WorkspaceFileOperationEvent {
    operation: 'read' | 'update' | 'delete' | 'list' | 'patch' | 'move'
    filepath: string
    folder?: string
    turn: number
    server_name: string
  }
  ```

### 8. Update Workspace Store Event Handler
**File**: `frontend/src/stores/useWorkspaceStore.ts`

- **processWorkspaceEvent** function (line 507): Add handler for `workspace_file_operation` event:
  ```typescript
  if (event.type === 'workspace_file_operation' && event.data) {
    const eventData = event.data as WorkspaceFileOperationEvent
    const { operation, filepath, folder } = eventData
    
    // Backend emits full filepaths (e.g., "Workflow/MyProject/file.txt")
    // highlightFile searches in raw unfiltered files, so full paths work correctly
    // Workspace component handles filtering and path adjustment for display
    
    if (operation === 'read' || operation === 'update' || operation === 'patch') {
      if (filepath) {
        // Check if file exists in raw file tree, refresh if new, then highlight
        const state = get()
        const fileExists = findFileInTree(state.files, filepath)
        if (!fileExists && operation === 'update') {
          // New file created - refresh tree to show it
          get().fetchFiles().then(() => {
            setTimeout(() => {
              get().highlightFile(filepath)
              // Expand folders to show the file (works with workflow folder filtering)
              get().expandFoldersForFile(filepath)
            }, 200)
          })
        } else {
          // File exists - highlight and expand folders
          get().highlightFile(filepath)
          get().expandFoldersForFile(filepath)
        }
      }
    } else if (operation === 'delete') {
      if (filepath) {
        get().removeFile(filepath)
        // Clear selection if deleted file was selected
        const state = get()
        if (state.selectedFile?.path === filepath) {
          set({ selectedFile: null, fileContent: '', showFileContent: false })
        }
      }
    } else if (operation === 'list') {
      // List operation - no highlighting needed
    } else if (operation === 'move') {
      // Handle move operation: remove old path, highlight new path
      // Event should include both source and destination filepaths
      // Implementation depends on event data structure
    }
    
    return true
  }
  ```

- Remove `write_code` parsing logic from `tool_call_start` handler (lines 519-547)
- Remove `write_code` parsing logic from `tool_call_end` handler (lines 587-657)
- Remove workspace tool detection from `tool_call_start` handler (lines 549-574)

### 9. Remove Go Code Parser (Optional Cleanup)
**File**: `frontend/src/utils/goCodeParser.ts`
- Can be kept for other purposes or removed if no longer needed
- Update imports in `useWorkspaceStore.ts` if removed

### 10. Update Event Type Definitions
**File**: `frontend/src/generated/events-bridge.ts` (or similar)
- Ensure `workspace_file_operation` is included in event type union
- Add TypeScript interface for `WorkspaceFileOperationEvent` if needed

## Workflow Folder Integration

### How Workflow Folder Opening Works
1. When a workflow preset is selected, it has a `selectedFolder.filepath` (e.g., "Workflow/MyProject")
2. Frontend `Workspace.tsx` extracts this as `workflowFolderPath` (line 121-140)
3. Files are filtered to show only the workflow folder when in workflow mode (line 142-442)
4. When workflow opens, folders are auto-expanded (line 479-525)
5. `applyPreset` in `useGlobalPresetStore.ts` calls `expandFoldersForFile(folderPath)` (line 672)

### Integration with New Event System
- **Backend emits full filepaths**: Events contain full paths like "Workflow/MyProject/file.txt"
- **Frontend `highlightFile` searches in raw files**: Uses `findFileInTree(state.files, filepath)` which searches unfiltered file tree
- **Workspace component handles filtering**: Automatically filters to workflow folder when `workflowFolderPath` is set
- **Path adjustment for display**: Workspace component adjusts paths for display (removes workflow folder prefix)
- **Auto-expansion**: `expandFoldersForFile` is called to ensure file is visible in filtered view

### Key Points
- No path conversion needed in event handler - backend emits full paths, frontend searches in raw tree
- Workspace component's filtering logic handles workflow folder display automatically
- `expandFoldersForFile` ensures files are visible even when workflow folder is filtered
- This works seamlessly with existing workflow folder filtering and auto-expansion

## Testing Considerations

1. Test direct workspace tool calls (read, update, delete) emit events
2. Test workspace tools called from `write_code` execution emit events
3. Test orchestrator direct calls (ReadWorkspaceFile, WriteWorkspaceFile) emit events
4. Test frontend receives and processes events correctly
5. Test file highlighting works for all operation types
6. Test file tree refresh on new file creation
7. Test file removal on delete operations
8. Test workflow folder filtering works with new events
9. Test file highlighting in workflow mode (filtered view)
10. Test folder expansion when files are highlighted in workflow mode

## Migration Notes

- Backend changes are backward compatible (new event, doesn't break existing)
- Frontend changes remove parsing logic but keep fallback behavior
- Both systems can run in parallel during transition
- Old frontend parsing can be removed after verification
- No breaking changes to existing functionality

## Implementation Order

1. **Phase 1: Backend Event Infrastructure**
   - Add event type and data structure
   - Add helper functions
   - Test event creation

2. **Phase 2: Backend Event Emission**
   - Modify workspace tool handlers to emit events
   - Inject emitter in base orchestrator
   - Inject emitter in agent tool calls
   - Test events are emitted correctly

3. **Phase 3: Frontend Event Handling**
   - Add event handler in workspace store
   - Test event reception and processing
   - Test file highlighting

4. **Phase 4: Cleanup**
   - Remove Go code parsing logic
   - Remove unused imports
   - Test end-to-end

5. **Phase 5: Verification**
   - Test with direct tool calls
   - Test with write_code execution
   - Test with workflow folder filtering
   - Verify all operation types work

## Files to Modify

### Backend:
- `mcpagent/events/types.go` - Add event type constant
- `mcpagent/events/data.go` - Add event data structure and constructor
- `agent_go/cmd/server/virtual-tools/workspace_tools.go` - Add helpers and emit events
- `agent_go/pkg/orchestrator/base_orchestrator.go` - Inject emitter in context
- `mcpagent/agent/conversation.go` - Inject emitter for agent tool calls

### Frontend:
- `frontend/src/stores/useWorkspaceStore.ts` - Add event handler, remove parsing
- `frontend/src/utils/goCodeParser.ts` - Optional: remove if unused
- Frontend generated types (auto-updated after backend changes)

## Benefits

1. **Separation of Concerns**: Backend knows what happened, frontend just displays
2. **No Go Code Parsing**: Eliminates complex parsing logic in frontend
3. **Single Event Type**: One event for all workspace operations
4. **Easier to Extend**: Adding new operations just requires emitting event
5. **Better Performance**: No parsing overhead in frontend
6. **More Reliable**: Backend has exact information about operations
7. **Works with Workflow Folders**: Seamlessly integrates with existing workflow folder filtering

## Challenges and Solutions

### Challenge 1: Event Emitter Access
- **Problem**: Workspace handlers don't have direct access to event emitter
- **Solution**: Pass emitter through context (Option A)

### Challenge 2: Turn Number
- **Problem**: Turn number may not be available in all contexts
- **Solution**: Extract from context if available, default to 0 for orchestrator calls

### Challenge 3: Generated Code Execution
- **Problem**: `write_code` executes Go code that calls workspace tools via HTTP
- **Solution**: Events are emitted from workspace tool handlers, so they work for both direct calls and generated code calls

### Challenge 4: Workflow Folder Paths
- **Problem**: Need to ensure filepaths work with workflow folder filtering
- **Solution**: Backend emits full paths, frontend searches in raw tree, Workspace component handles filtering

## Open Questions

1. **Move Operation**: Should move operation emit one event or two? (source deletion + destination creation)
   - Recommendation: Emit two events (delete for source, update for destination)

2. **Event Timing**: Emit on start, end, or both?
   - Recommendation: Emit on end (after successful operation) - matches current tool_call_end pattern

3. **Error Handling**: Should we emit events on errors?
   - Recommendation: No, only emit on successful operations (errors are handled by tool_call_error events)

4. **Turn Number Default**: What should turn number be for orchestrator direct calls?
   - Recommendation: Use 0 or extract from context if available

