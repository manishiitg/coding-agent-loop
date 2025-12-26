# Workflow Multi-Tab Chat Implementation Plan

## Objective
Enable multiple chat tabs within workflow mode, allowing users to run planning and execution phases simultaneously and view their conversations in separate tabs.

## Current State Analysis

### Workflow Chat Architecture
- **Single ChatArea**: `WorkflowLayout` contains one `ChatArea` instance (line 353-359)
- **Phase Switching**: Starting a new phase clears previous events (line 223-225)
- **Single Active Phase**: `useWorkflowStore.activePhase` tracks only one phase at a time
- **Event Clearing**: `handleStartPhase` calls `resetChatState()` before starting new phase
- **Observer Reuse**: Same observer ID is reused across phases (from `useChatStore`)

### Current Flow
1. User starts "planning" phase → ChatArea shows planning conversation
2. User starts "execution" phase → Previous events cleared, ChatArea shows execution conversation
3. Cannot view both planning and execution conversations simultaneously

## Requirements

### Functional Requirements
1. **Multiple Phase Tabs**: Each workflow phase can have its own tab
2. **Parallel Execution**: Planning and execution can run simultaneously
3. **Independent Observers**: Each tab maintains its own observer ID
4. **Tab Management**: Create, switch, close tabs for different phases
5. **Phase Identification**: Tabs show phase name (e.g., "Planning", "Execution")
6. **State Preservation**: Switching tabs preserves conversation state
7. **Event Isolation**: Events from different phases appear in correct tabs

### Technical Requirements
1. **Observer Per Tab**: Each phase tab gets its own observer ID
2. **Independent Polling**: Each tab polls its own observer independently
3. **Event Filtering**: Events filtered by phase/observer ID
4. **Tab State Management**: Extend workflow store for tab management
5. **UI Integration**: Tab bar within workflow chat panel

## Implementation Plan

### Phase 1: Workflow Tab State Management

#### 1.1 Extend Workflow Store
**File**: `frontend/src/stores/useWorkflowStore.ts`

**New State Structure**:
```typescript
interface WorkflowChatTab {
  tabId: string  // Unique ID: `phase_${phaseId}_${timestamp}`
  phaseId: string  // Workflow phase ID (e.g., "planning", "execution")
  phaseName: string  // Display name from WorkflowPhase
  observerId: string  // Unique observer ID for this tab
  sessionId: string | null  // Chat session ID if exists
  isActive: boolean  // Whether this phase is currently running
  createdAt: number  // Timestamp for ordering
}

interface WorkflowStore {
  // ... existing state
  
  // Multi-tab chat state
  workflowChatTabs: Record<string, WorkflowChatTab>  // tabId -> tab
  activeWorkflowTabId: string | null  // Currently selected tab
}
```

**New Actions**:
- `createWorkflowTab(phaseId: string, phaseName: string): string` - Creates new tab, registers observer
- `switchWorkflowTab(tabId: string): void` - Switches active tab
- `closeWorkflowTab(tabId: string): void` - Closes tab, removes observer
- `getWorkflowTab(tabId: string): WorkflowChatTab | undefined`
- `getActiveWorkflowTab(): WorkflowChatTab | undefined`
- `getTabsByPhase(phaseId: string): WorkflowChatTab[]` - Get all tabs for a phase

#### 1.2 Tab Lifecycle
**Tab Creation**:
- Generate `tabId`: `phase_${phaseId}_${Date.now()}`
- Register new observer via `agentApi.registerObserver()`
- Create tab entry in `workflowChatTabs`
- Set as active tab

**Tab Switching**:
- Update `activeWorkflowTabId`
- Stop polling for previous tab (if any)
- Start polling for new tab
- Update ChatArea to use new tab's observer ID

**Tab Closing**:
- Stop polling
- Remove observer via `DELETE /api/observer/{observer_id}`
- Delete tab from `workflowChatTabs`
- If closing active tab, switch to another tab or close chat panel

### Phase 2: Workflow Tab UI Component

#### 2.1 Create WorkflowChatTabs Component
**File**: `frontend/src/components/workflow/WorkflowChatTabs.tsx`

**Features**:
- Tab bar showing all workflow phase tabs
- Tab display: phase name, status indicator (active/streaming), close button
- "New Phase" button to start new phase in new tab
- Tab switching on click
- Visual indicators: active tab highlight, streaming indicator

**Props**:
```typescript
interface WorkflowChatTabsProps {
  tabs: WorkflowChatTab[]
  activeTabId: string | null
  phases: WorkflowPhase[]  // Available phases
  onTabSelect: (tabId: string) => void
  onTabClose: (tabId: string) => void
  onStartPhase: (phaseId: string, executionOptions?: ExecutionOptions) => void
}
```

**UI Structure**:
```
┌─────────────────────────────────────┐
│ [Planning] [Execution] [+ New Phase]│  ← Tab bar
├─────────────────────────────────────┤
│                                     │
│         ChatArea Content            │  ← Tab content
│                                     │
└─────────────────────────────────────┘
```

#### 2.2 Tab Display Logic
- **Tab Title**: Use `phaseName` from `WorkflowPhase`
- **Status Indicator**: 
  - Green dot = active/streaming
  - Gray dot = inactive
  - Show phase icon if available
- **Close Button**: Only show on hover, disable if it's the only tab

### Phase 3: Update WorkflowLayout

#### 3.1 Replace Single ChatArea with Tabbed Interface
**File**: `frontend/src/components/workflow/WorkflowLayout.tsx`

**Changes**:
- Remove single `chatAreaRef`
- Add `WorkflowChatTabs` component above ChatArea
- Update `handleStartPhase` to create new tab instead of clearing events
- Pass tab management functions to `WorkflowChatTabs`

**Updated `handleStartPhase`**:
```typescript
const handleStartPhase = useCallback(async (phaseId: string, executionOptions?: ExecutionOptions) => {
  // Get phase name
  const phase = useWorkflowStore.getState().getPhaseById(phaseId)
  const phaseName = phase?.name || phaseId
  
  // Create new tab for this phase
  const tabId = useWorkflowStore.getState().createWorkflowTab(phaseId, phaseName)
  
  // Get tab's observer ID
  const tab = useWorkflowStore.getState().getWorkflowTab(tabId)
  if (!tab) return
  
  // Update workflow status in database
  await agentApi.updateWorkflow(activePresetId, phaseId, null, undefined)
  
  // Set active tab
  useWorkflowStore.getState().switchWorkflowTab(tabId)
  setShowChatArea(true)
  
  // Submit query using tab's observer ID
  // Need to pass observerId to ChatArea or submit directly
}, [activePresetId, setShowChatArea])
```

#### 3.2 Update ChatArea Integration
**Option A**: Multiple ChatArea instances (one per tab)
- Render ChatArea for each tab, show/hide based on active tab
- Each ChatArea has its own observer ID

**Option B**: Single ChatArea with tab switching
- Single ChatArea instance
- Switch observer ID when switching tabs
- Update events list based on active tab

**Recommended**: Option B (single instance, switch state)

### Phase 4: ChatArea Tab Awareness

#### 4.1 Extend ChatArea for Workflow Tabs
**File**: `frontend/src/components/ChatArea.tsx`

**New Props** (optional, for workflow mode):
```typescript
interface ChatAreaProps {
  // ... existing props
  workflowTabId?: string  // If provided, use this tab's observer ID
  onWorkflowTabUpdate?: (tabId: string, updates: Partial<WorkflowChatTab>) => void
}
```

**Changes**:
- If `workflowTabId` provided, use tab's observer ID instead of global observer
- Update tab state when events arrive (via `onWorkflowTabUpdate`)
- Filter events by tab's observer ID if needed

#### 4.2 Event Filtering by Tab
**Current**: All events from observer ID are shown
**New**: Filter events by active tab's observer ID

**Implementation**:
- Store events per tab in workflow store
- Or filter events in ChatArea based on active tab's observer ID
- Each tab maintains its own event list

### Phase 5: Backend Integration

#### 5.1 Observer Management
**No Backend Changes Required**
- Backend already supports multiple observers
- Each tab registers its own observer
- Events are isolated by observer ID

#### 5.2 Phase Identification
**Current**: Backend receives phase in query: `Execute workflow phase: ${phaseId}`
**New**: Same approach, but each phase gets its own observer ID

**Event Association**:
- Events include `session_id` which can be used to identify phase
- Or add phase metadata to events (if needed)

### Phase 6: State Management Integration

#### 6.1 Tab State Persistence
**Optional**: Persist tabs to localStorage
- Save tab list on tab create/close
- Restore tabs on page reload
- Reconnect to active observers

#### 6.2 Event Storage Per Tab
**Option A**: Store events in workflow store per tab
```typescript
workflowTabEvents: Record<string, PollingEvent[]>  // tabId -> events
```

**Option B**: Use existing chat store, filter by observer ID
- Keep events in `useChatStore.events`
- Filter by active tab's observer ID when displaying

**Recommended**: Option B (simpler, reuses existing infrastructure)

## Implementation Order

1. **Phase 1.1**: Extend workflow store with tab state management
2. **Phase 1.2**: Implement tab lifecycle functions (create, switch, close)
3. **Phase 2.1**: Create WorkflowChatTabs UI component
4. **Phase 3.1**: Update WorkflowLayout to use tabs
5. **Phase 3.2**: Update handleStartPhase to create tabs
6. **Phase 4.1**: Make ChatArea tab-aware (optional props)
7. **Phase 4.2**: Implement event filtering by tab
8. **Phase 6**: State persistence and cleanup

## Technical Considerations

### Observer Lifecycle
- **Creation**: On phase start, create new observer
- **Active**: While phase is running
- **Inactive**: When phase completes (keep events)
- **Removed**: When tab is closed

### Event Isolation
- Each tab has unique observer ID
- Events stored in EventStore keyed by observer ID
- Frontend filters events by active tab's observer ID

### Memory Management
- Close tabs when workflow preset changes
- Clean up observers on tab close
- Limit number of open tabs (optional: max 5-10)

### Phase Identification
- Tab ID includes phase ID: `phase_${phaseId}_${timestamp}`
- Phase name from `WorkflowPhase` for display
- Backend receives phase in query string

### Concurrent Execution
- Multiple phases can run simultaneously
- Each phase has independent observer and polling
- No conflicts between phases

## Files to Modify

### Frontend
- `frontend/src/stores/useWorkflowStore.ts` - Add tab state management
- `frontend/src/components/workflow/WorkflowLayout.tsx` - Integrate tabs
- `frontend/src/components/workflow/WorkflowChatTabs.tsx` - New component
- `frontend/src/components/ChatArea.tsx` - Optional: tab awareness
- `frontend/src/services/api.ts` - No changes (reuse existing observer APIs)

### Backend
- **No changes required** - Backend already supports multiple observers

## Success Criteria

1. ✅ Multiple phase tabs can be created simultaneously
2. ✅ Planning and execution can run in parallel
3. ✅ Each tab maintains independent observer ID and events
4. ✅ Tabs can be switched without losing state
5. ✅ Tabs can be closed, cleaning up observers
6. ✅ Tab names reflect workflow phase names
7. ✅ Events appear in correct tab based on observer ID
8. ✅ No memory leaks from orphaned observers

## Example User Flow

1. User starts "Planning" phase → Tab "Planning" created, shows planning conversation
2. User starts "Execution" phase → Tab "Execution" created, shows execution conversation
3. User switches to "Planning" tab → Sees planning conversation
4. User switches to "Execution" tab → Sees execution conversation
5. Both phases can run simultaneously
6. User closes "Planning" tab → Tab removed, observer cleaned up
7. "Execution" tab remains active

## Integration with Parallel Chats Feature

This feature is complementary to the parallel chats feature:
- **Parallel Chats**: Multiple independent chat sessions (different workflows/presets)
- **Workflow Multi-Tabs**: Multiple phases within a single workflow session
- Both use the same observer infrastructure
- Workflow tabs are scoped to workflow mode only

