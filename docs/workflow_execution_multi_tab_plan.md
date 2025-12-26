# Workflow Execution Button Multi-Tab Support Plan

## Problem Statement

The workflow execution button (Start/Stop) currently uses:
- **Observer ID**: From global `useChatStore` (single observer for entire app)
- **Session ID**: From global `getSessionId()` (single session for entire app)
- **Execution Status**: From global `useChatStore.isStreaming` (single status)

With the new multi-tab feature, each tab has:
- Its own `observerId` (stored in `WorkflowChatTab`)
- Its own `sessionId` (stored in `WorkflowChatTab`, set when query starts)
- Potentially its own execution status

### Critical Question: Which Tab's Observer ID?

**Scenario**: User has multiple tabs running in parallel:
- Tab 1: Planning phase (observerId: `obs-planning-1`)
- Tab 2: Execution phase (observerId: `obs-execution-1`)

**When user clicks "Execute" button:**
- Which observer ID should be used?
- Should it reuse the existing execution tab, or create a new one?
- How do we know which tab corresponds to which phase?

**Answer**: The Execute button should:
1. Find existing execution phase tab (using `getTabsByPhase('execution')`)
2. If found and not running → Switch to it and use its observer ID
3. If found and running → Show error or stop first
4. If not found → Create new execution tab (current behavior)
5. For Stop → Stop the execution phase tab (or active tab if it's execution)

### How to Identify Execution Phase Tab

**Key**: Each `WorkflowChatTab` has a `phaseId` field that identifies which phase it belongs to:
- `phaseId: 'planning'` → Planning phase tab
- `phaseId: 'execution'` → Execution phase tab
- `phaseId: 'validation'` → Validation phase tab (if exists)

**Solution**: Use `getTabsByPhase('execution')` to find all execution phase tabs, then:
- If multiple exist, use the one that's currently streaming (running)
- If none are streaming, use the most recently created one
- If none exist, create a new one

**Example Flow**:
```typescript
// In WorkflowToolbar.handleExecute()
const executionTabs = useWorkflowStore.getState().getTabsByPhase('execution')
const runningExecutionTab = executionTabs.find(tab => tab.isStreaming)
const executionTab = runningExecutionTab || executionTabs[0] || null

if (executionTab) {
  // Reuse existing execution tab's observer ID
  setCurrentObserverId(executionTab.observerId)
  switchWorkflowTab(executionTab.tabId)
} else {
  // Create new execution tab
  onStartPhase('execution', options)
}
```

## Current Implementation

### `useWorkflowExecution` Hook
- **Location**: `frontend/src/components/workflow/hooks/useWorkflowExecution.ts`
- **Current behavior**:
  - `startWorkflow()`: Uses `useChatStore.getState().observerId` (global)
  - `stopWorkflow()`: Uses `getSessionId()` (global)
  - `status`: Derived from `useChatStore.isStreaming` (global)

### Execution Flow
1. User clicks "Execute" button in `WorkflowToolbar`
2. Calls `startWorkflow()` from `useWorkflowExecution`
3. `startWorkflow()` gets global `observerId` from `useChatStore`
4. Calls `agentApi.startQuery()` which uses `getObserverId()` (synced from global store)
5. Backend starts execution and associates it with that observer ID
6. Events flow to that observer ID's event stream

### Stop Flow
1. User clicks "Stop" button
2. Calls `stopWorkflow()` from `useWorkflowExecution`
3. `stopWorkflow()` gets global `sessionId` from `getSessionId()`
4. Calls `agentApi.stopSession(sessionId)`
5. Backend cancels execution for that session ID

## Required Changes

### 1. Update `useWorkflowExecution` to Use Active Tab

**Changes needed**:
- Get active workflow tab from `useWorkflowStore`
- Use active tab's `observerId` instead of global `observerId`
- Use active tab's `sessionId` for stopping
- Sync active tab's observer ID to API module before starting
- Fall back to global behavior if no active tab (for backward compatibility)

### 2. Update Session ID Tracking

**Current**: `sessionId` is stored globally in API module
**Needed**: Store `sessionId` in `WorkflowChatTab` when query starts

**Changes**:
- When `startQuery` returns, extract `session_id` from response
- Update the active tab's `sessionId` in `useWorkflowStore`
- Use tab's `sessionId` for stopping

### 3. Update Execution Status

**Current**: Status derived from global `isStreaming`
**Options**:
- **Option A**: Track status per tab in `WorkflowChatTab` (add `isStreaming: boolean`)
- **Option B**: Derive status from active tab's events (check if events are still coming)
- **Option C**: Keep global status but ensure it reflects active tab's state

**Recommendation**: Option A - Add `isStreaming` to `WorkflowChatTab` and update it when:
- Query starts (set to `true`)
- Query stops (set to `false`)
- Tab switches (check tab's status)

### 4. Update API Module Observer ID Sync

**Current**: `ChatArea` syncs observer ID via `setCurrentObserverId()`
**Needed**: Also sync when switching tabs or starting execution

**Changes**:
- In `useWorkflowExecution.startWorkflow()`, sync active tab's observer ID
- In `WorkflowLayout` or `WorkflowChatTabs`, sync observer ID when switching tabs

## Implementation Plan

### Step 1: Update `WorkflowChatTab` Interface
```typescript
export interface WorkflowChatTab {
  tabId: string
  phaseId: string
  phaseName: string
  observerId: string
  sessionId: string | null  // ✅ Already exists
  isActive: boolean
  isStreaming: boolean  // ✅ ADD THIS
  createdAt: number
}
```

### Step 2: Update `useWorkflowStore` Actions
- Add `setTabStreaming(tabId, isStreaming)` action
- Add `updateTabSessionId(tabId, sessionId)` action
- Update `createWorkflowTab` to initialize `isStreaming: false`
- Update `closeWorkflowTab` to stop streaming if active
- `getTabsByPhase(phaseId)` already exists - use it to find execution tabs

### Step 3: Update `useWorkflowExecution` Hook
```typescript
export function useWorkflowExecution(): UseWorkflowExecutionReturn {
  // Get active workflow tab
  const activeTab = useWorkflowStore(state => state.getActiveWorkflowTab())
  
  // Use active tab's observer ID, fallback to global
  const observerId = activeTab?.observerId || useChatStore(state => state.observerId)
  
  // Use active tab's streaming status, fallback to global
  const isStreaming = activeTab?.isStreaming || useChatStore(state => state.isStreaming)
  
  // ... rest of implementation
}
```

### Step 4: Update `handleExecute` in WorkflowToolbar
```typescript
const handleExecute = useCallback(() => {
  if (!isRunning && executionPhase) {
    const workflowStore = useWorkflowStore.getState()
    
    // Find existing execution phase tab
    const executionTabs = workflowStore.getTabsByPhase(EXECUTION_PHASE_ID)
    const existingExecutionTab = executionTabs.length > 0 ? executionTabs[0] : null
    
    if (existingExecutionTab) {
      // Reuse existing execution tab
      // Switch to it if not already active
      if (workflowStore.activeWorkflowTabId !== existingExecutionTab.tabId) {
        workflowStore.switchWorkflowTab(existingExecutionTab.tabId)
      }
      
      // Use existing tab's observer ID
      setCurrentObserverId(existingExecutionTab.observerId)
      
      // Build execution options
      const options = buildExecutionOptions()
      
      // Start execution using existing tab's observer ID
      // (The query will be submitted through ChatArea which uses active tab's observer ID)
      onStartPhase(executionPhase.id, options)
    } else {
      // No existing execution tab - create new one (current behavior)
      const options = buildExecutionOptions()
      onStartPhase(executionPhase.id, options)
    }
  }
}, [isRunning, executionPhase, buildExecutionOptions, onStartPhase])
```

### Step 5: Update `startWorkflow` Function (if still used)
```typescript
const startWorkflow = useCallback(async (presetQueryId: string) => {
  // Get active tab (should be execution phase tab if called from Execute button)
  const activeTab = useWorkflowStore.getState().getActiveWorkflowTab()
  
  if (!activeTab) {
    // Fallback to global behavior (backward compatibility)
    // ... existing logic
    return
  }
  
  // Use active tab's observer ID
  const currentObserverId = activeTab.observerId
  
  // Sync to API module
  setCurrentObserverId(currentObserverId)
  
  // Start query
  const response = await agentApi.startQuery(requestPayload)
  
  // Update tab's session ID from response
  if (response.session_id) {
    useWorkflowStore.getState().updateTabSessionId(activeTab.tabId, response.session_id)
  }
  
  // Set tab's streaming status
  useWorkflowStore.getState().setTabStreaming(activeTab.tabId, true)
}, [...])
```

### Step 6: Update `stopWorkflow` Function
```typescript
const stopWorkflow = useCallback(async () => {
  const workflowStore = useWorkflowStore.getState()
  
  // Find execution phase tab (preferred) or use active tab
  const executionTabs = workflowStore.getTabsByPhase(EXECUTION_PHASE_ID)
  const executionTab = executionTabs.find(tab => tab.isStreaming) || executionTabs[0]
  
  // Use execution tab if found, otherwise use active tab
  const targetTab = executionTab || workflowStore.getActiveWorkflowTab()
  
  if (!targetTab || !targetTab.sessionId) {
    // Fallback to global behavior
    const sessionId = getSessionId()
    if (sessionId) {
      await agentApi.stopSession(sessionId)
    }
    return
  }
  
  // Use target tab's session ID
  const sessionId = targetTab.sessionId
  
  // Stop polling for this tab's observer
  // (ChatArea handles this, but we should also stop tab's streaming)
  
  // Set tab's streaming status
  workflowStore.setTabStreaming(targetTab.tabId, false)
  
  // Call backend to stop session
  await agentApi.stopSession(sessionId)
}, [])
```

### Step 7: Update Tab Switching
- When switching tabs, sync the new tab's observer ID to API module
- Update execution status to reflect active tab's status
- (Already handled in `ChatArea` via `workflowTabId` prop)

### Step 8: Update Query Response Handling
- When `startQuery` returns, extract `session_id` from response
- Update active tab's `sessionId` in store
- Set active tab's `isStreaming` to `true`

## Backend Considerations

### Current Backend Behavior
- **Stop endpoint**: Uses `sessionID` from header (not observer ID)
- **Execution cancellation**: Uses `sessionID` to find workflow orchestrator context
- **Event storage**: Uses `observerID` (isolated per observer)

### Backend Support
✅ **Already supports multiple observers** - Each tab's observer ID has isolated events
✅ **Stop by session ID** - Works correctly, just need to pass correct session ID
⚠️ **Session tracking** - `activeSessions` map only stores one observer ID per session ID
  - This is fine for stopping (uses session ID)
  - But may cause issues if multiple tabs share same session ID

### Recommendation
- Each tab should have its own `sessionId` (already planned)
- Backend already handles this correctly
- No backend changes needed

## Testing Checklist

### Basic Multi-Tab Execution
- [ ] Start execution in Tab 1 (Planning) → Should use Tab 1's observer ID
- [ ] Start execution in Tab 2 (Execution) → Should use Tab 2's observer ID
- [ ] Stop execution in Tab 1 → Should stop Tab 1's session
- [ ] Stop execution in Tab 2 → Should stop Tab 2's session

### Execute Button Behavior
- [ ] Click Execute with no execution tab → Creates new execution tab
- [ ] Click Execute with existing execution tab (not running) → Switches to it, reuses observer ID
- [ ] Click Execute with existing execution tab (running) → Shows error or stops first
- [ ] Click Stop → Stops the execution phase tab (or active tab if it's execution)

### Parallel Execution
- [ ] Planning tab running + Click Execute → Creates/starts execution tab independently
- [ ] Both Planning and Execution running → Each has own observer ID, events isolated
- [ ] Switch tabs while execution running → Status reflects active tab's state

### Edge Cases
- [ ] Start execution without active tab → Should fallback to global behavior
- [ ] Stop execution without active tab → Should fallback to global behavior
- [ ] Close execution tab while running → Should stop execution
- [ ] Multiple execution tabs (shouldn't happen, but test) → Stop button stops active one

## Migration Path

1. **Phase 1**: Add `isStreaming` to `WorkflowChatTab` (non-breaking)
2. **Phase 2**: Update `useWorkflowExecution` to use active tab (with fallback)
3. **Phase 3**: Update `startWorkflow` to sync observer ID and update session ID
4. **Phase 4**: Update `stopWorkflow` to use tab's session ID
5. **Phase 5**: Test thoroughly with multiple tabs

## Edge Cases

1. **No active tab**: Fallback to global behavior
2. **Tab closed while executing**: Stop execution when tab closes
3. **Multiple tabs executing**: Each tab tracks its own status
4. **Tab switch during execution**: Status updates to reflect active tab
5. **Page refresh**: Tabs are recreated, but execution state is lost (expected)

