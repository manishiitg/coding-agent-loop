# Parallel Chats Implementation Plan

## Objective
Enable multiple parallel chat sessions in the frontend, each with independent observer IDs, event polling, and state management.

## Current State Analysis

### Backend Support
- ✅ **EventStore**: Already supports multiple observers via `map[string][]Event` (observerID -> events)
- ✅ **ObserverManager**: Already supports multiple concurrent observers via `map[string]*Observer`
- ✅ **Polling API**: Already supports per-observer polling via `/api/observer/{observer_id}/events`
- ✅ **Thread Safety**: All operations are thread-safe with `sync.RWMutex`
- ❌ **Observer Cleanup**: Observers are NOT cleaned up on context cancellation or session stop
- ❌ **Observer Stats API**: No endpoint to query active observer count/statistics

### Frontend State
- ❌ **Single Chat State**: `useChatStore` maintains single observer ID and event list
- ❌ **No Tab Management**: No UI for multiple chat tabs
- ❌ **Single Polling Loop**: Only one polling interval active at a time

## Implementation Requirements

### Phase 1: Backend Observer Management

#### 1.1 Add Observer Statistics API
**Endpoint**: `GET /api/observer/stats`

**Response**:
```json
{
  "total_observers": 5,
  "active_observers": 3,
  "inactive_observers": 2,
  "total_events": 150,
  "observers": [
    {
      "observer_id": "observer_abc123",
      "session_id": "session_xyz",
      "status": "active",
      "event_count": 50,
      "created_at": "2025-01-09T10:00:00Z",
      "last_activity": "2025-01-09T10:05:00Z"
    }
  ]
}
```

**Implementation**:
- Add handler `handleGetObserverStats` in `polling.go`
- Combine `ObserverManager.GetObserverStats()` and `EventStore.GetStats()`
- Include observer details from `ObserverManager.GetActiveObservers()`
- Register route in `server.go`: `apiRouter.HandleFunc("/observer/stats", api.handleGetObserverStats).Methods("GET")`

#### 1.2 Observer Cleanup on Context Cancellation
**Update**: `handleStopSession` in `server.go`

**Changes**:
- Extract observer ID from active session: `activeSession.ObserverID`
- Mark observer as inactive: `observerManager.UpdateObserverStatus(observerID, "inactive")`
- OR remove observer if session is cleared: `observerManager.RemoveObserver(observerID)`

**New Method Needed**:
```go
func (om *ObserverManager) UpdateObserverStatus(observerID string, status string) bool {
    om.mu.Lock()
    defer om.mu.Unlock()
    observer, exists := om.observers[observerID]
    if !exists {
        return false
    }
    observer.Status = status
    return true
}
```

#### 1.3 Observer Cleanup on Session Clear
**Update**: `handleClearSession` in `server.go`

**Changes**:
- Find observer by session ID: iterate `ObserverManager.observers` to find matching `SessionID`
- Remove observer: `observerManager.RemoveObserver(observerID)`
- This ensures cleared sessions don't leave orphaned observers

#### 1.4 Observer Cleanup on Completion
**Update**: Completion handler in `server.go` (around line 2046)

**Changes**:
- Mark observer as inactive: `observerManager.UpdateObserverStatus(observerID, "inactive")`
- Keep events for history (don't remove observer)
- Update active session status to "completed"

### Phase 2: Frontend Multi-Chat State Management

#### 2.1 Extend Chat Store for Multiple Tabs
**File**: `frontend/src/stores/useChatStore.ts`

**New State Structure**:
```typescript
interface ChatTab {
  tabId: string
  observerId: string
  sessionId: string | null
  events: PollingEvent[]
  lastEventIndex: number
  pollingInterval: NodeJS.Timeout | null
  isStreaming: boolean
  finalResponse: string
  isCompleted: boolean
  // ... other tab-specific state
}

interface MultiChatState {
  tabs: Record<string, ChatTab>
  activeTabId: string | null
  // ... existing global state
}
```

**Actions Needed**:
- `createTab()`: Generate tabId, register observer, initialize tab state
- `switchTab(tabId)`: Set activeTabId, stop polling for previous tab, start polling for new tab
- `closeTab(tabId)`: Stop polling, remove observer via API, delete tab from state
- `getTab(tabId)`: Return tab state
- `getActiveTab()`: Return active tab state

#### 2.2 Update ChatArea Component
**File**: `frontend/src/components/ChatArea.tsx`

**Changes**:
- Accept `tabId` prop instead of using global observer ID
- Use `useChatStore.getTab(tabId)` instead of global state
- Each tab maintains independent polling loop
- Tab-specific event handlers

**Key Updates**:
- `pollEvents`: Use `tab.observerId` instead of global `observerId`
- `submitQuery`: Use `tab.observerId` in request headers
- `handleNewChat`: Create new tab instead of resetting current

### Phase 3: Frontend Tab UI

#### 3.1 Create ChatTabs Component
**File**: `frontend/src/components/ChatTabs.tsx`

**Features**:
- Tab bar with list of active tabs
- Tab display: title, status indicator (streaming/active/completed), close button
- "New Chat" button creates new tab
- Tab switching on click
- Visual indicators: active tab highlight, streaming indicator

**Props**:
```typescript
interface ChatTabsProps {
  tabs: ChatTab[]
  activeTabId: string | null
  onTabSelect: (tabId: string) => void
  onTabClose: (tabId: string) => void
  onNewTab: () => void
}
```

#### 3.2 Update App.tsx
**File**: `frontend/src/App.tsx`

**Changes**:
- Wrap ChatArea with ChatTabs component
- Pass tab management functions to ChatTabs
- Update ChatArea to receive tabId prop
- Handle tab lifecycle (create, switch, close)

### Phase 4: Integration & Testing

#### 4.1 API Integration
**File**: `frontend/src/services/api.ts`

**New Methods**:
- `getObserverStats()`: Call `GET /api/observer/stats`
- `removeObserver(observerId)`: Call `DELETE /api/observer/{observer_id}`

#### 4.2 Polling Management
**Update**: `ChatArea.tsx` polling logic

**Changes**:
- Each tab has independent polling interval
- Stop polling when tab is inactive (not visible)
- Resume polling when tab becomes active
- Clean up polling intervals on tab close

#### 4.3 State Persistence (Optional)
- Persist tab list to localStorage
- Restore tabs on page reload
- Reconnect to active observers on mount

## Implementation Order

1. **Backend Phase 1.1**: Add observer stats API (enables monitoring)
2. **Backend Phase 1.2-1.4**: Observer cleanup on cancellation/clear/completion (prevents memory leaks)
3. **Frontend Phase 2.1**: Extend chat store for multi-tab state
4. **Frontend Phase 2.2**: Update ChatArea to be tab-aware
5. **Frontend Phase 3.1**: Create ChatTabs UI component
6. **Frontend Phase 3.2**: Integrate tabs into App.tsx
7. **Frontend Phase 4**: API integration and polling management
8. **Testing**: Verify parallel chats work independently

## Technical Notes

### Observer Lifecycle
- **Creation**: On tab creation, call `/api/observer/register`
- **Active**: While tab is active and polling
- **Inactive**: When tab is closed or session completes (keep events)
- **Removed**: When session is cleared or tab is explicitly closed

### Event Isolation
- Each observer ID has independent event list in `EventStore`
- Events are keyed by `observerID` in `map[string][]Event`
- No cross-contamination between observers

### Memory Management
- Cleanup observers on session stop/clear
- Mark observers inactive on completion
- Background cleanup removes observers with 0 events (every 5 minutes)
- Frontend should call `DELETE /api/observer/{observer_id}` on tab close

### Concurrency
- Backend is thread-safe with `sync.RWMutex`
- Multiple polling requests can run concurrently
- Each tab polls independently without blocking

## API Endpoints Summary

### Existing (No Changes)
- `POST /api/observer/register` - Register new observer
- `GET /api/observer/{observer_id}/events` - Poll events for observer
- `GET /api/observer/{observer_id}/status` - Get observer status
- `DELETE /api/observer/{observer_id}` - Remove observer

### New
- `GET /api/observer/stats` - Get statistics about all observers

## Files to Modify

### Backend
- `agent_go/cmd/server/polling.go` - Add stats handler, update cleanup
- `agent_go/cmd/server/server.go` - Update stop/clear handlers, register stats route
- `agent_go/internal/events/observer_manager.go` - Add `UpdateObserverStatus` method

### Frontend
- `frontend/src/stores/useChatStore.ts` - Extend for multi-tab state
- `frontend/src/components/ChatArea.tsx` - Make tab-aware
- `frontend/src/components/ChatTabs.tsx` - New component
- `frontend/src/App.tsx` - Integrate tabs
- `frontend/src/services/api.ts` - Add observer stats API

## Success Criteria

1. ✅ Multiple chat tabs can be created simultaneously
2. ✅ Each tab maintains independent observer ID and events
3. ✅ Tabs can be switched without losing state
4. ✅ Tabs can be closed, cleaning up observers
5. ✅ Observers are cleaned up on session stop/clear
6. ✅ Observer stats API returns accurate counts
7. ✅ No memory leaks from orphaned observers
8. ✅ Parallel polling works without conflicts

