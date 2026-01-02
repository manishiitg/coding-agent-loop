# Frontend API Structure and Data Model

## 📋 Overview

This document describes the frontend React component structure, API integration, and data flow for the multi-tab chat system. The architecture uses session-based event storage with unified tab management for both chat and workflow modes.

**Key Features:**
- Session-based event polling (not observer-based)
- Unified `ChatTab` interface for chat and workflow modes
- Per-tab event isolation via `sessionId`
- Mode-based rendering (`selectedModeCategory` determines layout)
- Event mode filtering (basic/advanced) per tab

---

## 📁 Key Files & Locations

| Component | File Path | Key Functions/Exports |
|-----------|-----------|---------------------|
| **App Root** | [`frontend/src/App.tsx`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/App.tsx) | `App`, `ChatAreaWithObserverId` wrapper component |
| **Chat Tabs** | [`frontend/src/components/ChatTabs.tsx`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/ChatTabs.tsx) | `ChatTabs` component for tab navigation |
| **Chat Area** | [`frontend/src/components/ChatArea.tsx`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/ChatArea.tsx) | `ChatArea` main chat interface |
| **Workflow Layout** | [`frontend/src/components/workflow/WorkflowLayout.tsx`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/workflow/WorkflowLayout.tsx) | `WorkflowLayout` workflow mode layout |
| **Workflow Chat Tabs** | [`frontend/src/components/workflow/WorkflowChatTabs.tsx`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/workflow/WorkflowChatTabs.tsx) | `WorkflowChatTabs` workflow-specific tabs |
| **Event Mode Provider** | [`frontend/src/components/events/EventModeContext.tsx`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/events/EventModeContext.tsx) | `EventModeProvider` context provider |
| **Chat Store** | [`frontend/src/stores/useChatStore.ts`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/stores/useChatStore.ts) | `useChatStore`, `ChatTab` interface |
| **API Service** | [`frontend/src/services/api.ts`](file:///Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/services/api.ts) | `agentApi.getSessionEvents()`, `agentApi.stopSession()` |

---

## 🔄 Component Hierarchy

```
App
├── ModePresetBar (Global mode/preset selection)
├── ChatTabs (Tab navigation - filters by mode category)
└── EventModeProvider (Per-tab scope - wraps content area)
    └── Content Area (rendered based on selectedModeCategory)
        ├── WorkflowLayout (when selectedModeCategory === 'workflow')
        │   ├── WorkflowChatTabs (workflow-specific tabs)
        │   └── ChatArea (hideHeader, hideInput, compact)
        └── ChatAreaWithObserverId (when selectedModeCategory === 'chat')
            └── ChatArea (full mode with header and input)
                ├── ChatHeader (session title/status)
                ├── EventDisplay (events list - uses EventMode context)
                └── ChatInput (query input)
```

---

## 🧩 Key Components

### App.tsx

**Purpose:** Root component that manages global state and renders mode-specific layouts.

**Key Logic:**
- Renders based on `selectedModeCategory` from `useModeStore` (not tab metadata)
- Creates default tab on page load for chat mode only
- Wraps content area with `EventModeProvider`
- Renders `WorkflowLayout` for workflow mode, `ChatAreaWithObserverId` for chat mode

**Code:**
```typescript
// From App.tsx
{selectedModeCategory === 'workflow' ? (
  <WorkflowLayout onNewChat={startNewChat} />
) : (
  <ChatAreaWithObserverId ref={chatAreaRef} onNewChat={startNewChat} />
)}
```

### ChatAreaWithObserverId

**Purpose:** Wrapper component that passes active tab's `tabId` to `ChatArea`.

**Note:** Despite the name, it uses `sessionId` (not observer ID). The name is a legacy artifact.

**Code:**
```typescript
// From App.tsx
const ChatAreaWithObserverId = forwardRef<ChatAreaRef, { onNewChat: () => void }>(
  ({ onNewChat }, ref) => {
    const activeTabId = useChatStore(state => state.activeTabId)
    return (
      <ChatArea
        ref={ref}
        onNewChat={onNewChat}
        tabId={activeTabId || undefined}
      />
    )
  }
)
```

### ChatTabs.tsx

**Purpose:** Global tab navigation component that filters tabs by mode category.

**Key Behavior:**
- In workflow mode: Only shows chat tabs (workflow tabs shown in `WorkflowChatTabs`)
- In chat mode: Shows all chat tabs
- Polls session status every 5 seconds
- Contains `EventModeToggle` (uses store directly, works outside `EventModeProvider`)

**Code:**
```typescript
// From ChatTabs.tsx
const modeTabs = useMemo(() => {
  if (selectedModeCategory === 'workflow') {
    // In workflow mode, only show chat tabs
    return Object.values(chatTabs).filter(tab => 
      tab.metadata?.mode === 'chat'
    ).sort((a, b) => a.createdAt - b.createdAt)
  }
  // In chat mode, show all chat tabs
  return Object.values(chatTabs).filter(tab => 
    tab.metadata?.mode === selectedModeCategory
  ).sort((a, b) => a.createdAt - b.createdAt)
}, [chatTabs, selectedModeCategory])
```

### ChatArea.tsx

**Purpose:** Main chat interface that manages session-based event polling and query submission.

**Key Features:**
- Accepts `tabId` prop to determine which tab's session to use
- Polls events for all tabs with `sessionId` every 1 second
- Filters events by active tab's `sessionId`
- Handles query submission with `X-Session-ID` header

**Code:**
```typescript
// From ChatArea.tsx
const activeTab = tabId ? getTab(tabId) : getActiveTab()
const sessionId = activeTab?.sessionId

// Filter events by active tab's sessionId
const tabEvents = useMemo(() => {
  if (activeTab?.sessionId) {
    return getTabEvents(activeTab.sessionId)
  }
  return []
}, [activeTab?.sessionId])
```

### EventModeProvider

**Purpose:** Context provider that manages event display mode (basic/advanced) per tab.

**Key Behavior:**
- Gets active tab's `eventMode` from store
- Updates tab's `eventMode` when mode changes
- Provides `mode` and `setMode` via context

**Code:**
```typescript
// From EventModeContext.tsx
export const EventModeProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const activeTab = useChatStore(state => state.getActiveTab())
  const tabEventMode = activeTab?.eventMode || 'basic'
  const [mode, setMode] = useState<EventMode>(tabEventMode)
  
  const setTabMode = useCallback((newMode: EventMode) => {
    setMode(newMode)
    const activeTab = useChatStore.getState().getActiveTab()
    if (activeTab) {
      useChatStore.getState().setTabEventMode(activeTab.tabId, newMode)
    }
  }, [])
  
  return (
    <EventModeContext.Provider value={{ mode, setMode: setTabMode }}>
      {children}
    </EventModeContext.Provider>
  )
}
```

### WorkflowChatTabs

**Purpose:** Workflow-specific tab component shown within `WorkflowLayout`.

**Key Behavior:**
- Only shows workflow tabs (`metadata.mode === 'workflow'`)
- Only shows active tabs (have `sessionId` or `isStreaming`)
- Closes chat area when all workflow tabs are closed

---

## 🔄 State Management

### useChatStore (Zustand)

**State Structure:**
```typescript
interface ChatState {
  // Multi-tab chat state
  chatTabs: Record<string, ChatTab>  // tabId -> tab
  activeTabId: string | null  // Currently selected tab
  
  // Per-tab event storage (keyed by sessionId)
  tabEvents: Record<string, PollingEvent[]>  // sessionId -> events
  tabEventIndices: Record<string, number>  // sessionId -> lastEventIndex
  tabHasMoreOlderEvents: Record<string, boolean>  // sessionId -> hasMoreOlderEvents
  
  // Tab session status (fetched from backend)
  tabSessionStatus: Record<string, TabSessionStatus>  // tabId -> status
  
  // Actions
  createChatTab: (name: string, metadata?: ChatTab['metadata'], existingObserverId?: string) => Promise<string>
  switchTab: (tabId: string) => void
  closeTab: (tabId: string) => Promise<void>
  getTab: (tabId: string) => ChatTab | undefined
  getActiveTab: () => ChatTab | undefined
  getTabsByMode: (mode: 'chat' | 'workflow') => ChatTab[]
  getTabsByPhaseId: (phaseId: string) => ChatTab[]
  // ... event management actions
  getTabEvents: (sessionId: string) => PollingEvent[]
  addTabEvents: (sessionId: string, events: PollingEvent[]) => void
  setTabLastEventIndex: (sessionId: string, index: number) => void
  // ... session status actions
  fetchTabSessionStatus: (tabId: string) => Promise<void>
  fetchAllTabSessionStatuses: (tabIds: string[]) => Promise<void>
}
```

### ChatTab Interface

```typescript
export interface ChatTab {
  tabId: string  // Unique ID: `chat_${timestamp}` or `phase_${phaseId}_${timestamp}`
  name: string  // Display name (e.g., "Chat 1", "Planning", "Execution")
  sessionId: string | null  // Chat session ID (used for API requests)
  isStreaming: boolean  // Whether this tab's execution is currently running
  isCompleted: boolean  // Whether this tab's execution has completed
  eventMode: 'basic' | 'advanced'  // Event display mode for this tab
  config: ChatTabConfig  // Tab-specific configuration (servers, LLM, etc.)
  createdAt: number  // Timestamp for ordering
  lastViewedEventCount: number  // Last event count when viewed (for badge)
  metadata?: {
    phaseId?: string  // For workflow mode: phase ID
    phaseName?: string  // For workflow mode: phase name
    mode?: 'chat' | 'workflow'  // Which mode this tab belongs to
    presetQueryId?: string  // For workflow mode: preset query ID
  }
}
```

---

## 🔄 Event Flow

### Tab Creation Flow

1. **User Action**: User clicks "New Chat" or starts a workflow phase
2. **Tab Creation**: `createChatTab()` generates unique `tabId` and `sessionId`
3. **State Storage**: Tab stored in `chatTabs` record with metadata
4. **UI Update**: Tab appears in tab bar, becomes active tab

### Query Submission Flow

1. **User Input**: User types query in `ChatInput`
2. **Query Submission**: `ChatArea.submitQuery()` called with query text
3. **API Request**: `POST /api/query` with `X-Session-ID` header
4. **Backend Processing**: Backend processes query, emits events to session's event store
5. **State Update**: `setTabStreaming(tabId, true)` called

### Event Polling Flow

1. **Polling Start**: `ChatArea` starts polling when tabs with `sessionId` exist
2. **Event Request**: For each tab with `sessionId`, calls `GET /api/sessions/{session_id}/events?since={index}&event_mode={mode}`
3. **Backend Response**: Returns new events since `lastEventIndex` for that session
4. **Event Storage**: Events stored in `tabEvents[sessionId]` via `addTabEvents()`
5. **UI Update**: `EventDisplay` renders events from active tab's session
6. **Index Update**: `lastEventIndex` updated per tab in `tabEventIndices[sessionId]`

### Tab Switching Flow

1. **User Clicks Tab**: `switchTab(tabId)` called
2. **State Update**: `activeTabId` updated, previous tab's `lastViewedEventCount` saved
3. **Event Display**: `ChatArea` filters events by new active tab's `sessionId`
4. **Badge Update**: New event count calculated for inactive tabs

---

## 🔌 Component → API Mapping

| Component | API Method | Endpoint | Purpose |
|-----------|-----------|----------|---------|
| **useChatStore** | `createChatTab()` | N/A (generates `sessionId` locally) | Creates tab with new `sessionId` |
| **useChatStore** | `fetchTabSessionStatus()` | `GET /api/sessions/active`<br>`GET /api/sessions/{session_id}/status` | Fetches session status for tab |
| **useChatStore** | `fetchAllTabSessionStatuses()` | `GET /api/sessions/active`<br>`GET /api/sessions/{session_id}/status` | Fetches status for multiple tabs |
| **useChatStore** | `closeTab()` | `POST /api/session/stop` | Stops session if tab is streaming |
| **ChatArea** | `pollEvents()` | `GET /api/sessions/{session_id}/events?since={index}&event_mode={mode}` | Polls events for all tabs |
| **ChatArea** | `submitQuery()` | `POST /api/query`<br>Headers: `X-Session-ID` | Submits query with session ID |
| **ChatArea** | `checkActiveSessions()` | `GET /api/sessions/active` | Checks for active sessions on mount |
| **ChatArea** | `reconnectSession()` | `POST /api/sessions/{session_id}/reconnect` | Reconnects to active session |
| **ChatArea** | `getSessionStatus()` | `GET /api/sessions/{session_id}/status` | Checks session status |
| **EventDisplay** | `submitHumanFeedback()` | `POST /api/human-feedback/submit` | Submits human feedback |

---

## 📊 Data Model

### Session → Events Relationship

```
Session (session_id: "abc123")
  └── Events: [event1, event2, event3, ...] ← Stored by sessionId

Tab 1 (sessionId: "abc123")
  └── Frontend tracks: lastEventIndex = 2
  └── Polls: GET /api/sessions/abc123/events?since=2

Tab 2 (sessionId: "abc123")  ← Can view same session
  └── Frontend tracks: lastEventIndex = 0
  └── Polls: GET /api/sessions/abc123/events?since=0
```

**Key Points:**
- Each tab typically has its own unique `sessionId`
- Events are stored in-memory per `sessionId` (for real-time polling)
- Events are also persisted to database with `session_id` (for history)
- Frontend manages polling state (`lastEventIndex`) per tab, enabling multiple independent viewers

---

## 🔌 API Structure

### Session Events API (Real-time Event Polling)

```
GET    /api/sessions/{session_id}/events?since={index}&event_mode={mode}
       Returns: { events: Event[], has_more: boolean, session_id: string }
```

**Purpose:** Real-time event delivery via polling. Events stored in-memory keyed by `session_id`. Frontend manages polling state (`since` parameter) per tab.

**Parameters:**
- `since`: Event index to start from (for polling)
- `event_mode`: `'basic'` or `'advanced'` (filters events by mode)

### Session API (Session Management)

```
POST   /api/session/stop
       Headers: X-Session-ID

POST   /api/session/clear
       Headers: X-Session-ID

GET    /api/sessions/active
       Returns: { active_sessions: ActiveSessionInfo[] }

POST   /api/sessions/{session_id}/reconnect
       Returns: { session_id: string, status: string, agent_mode: string, message: string }

GET    /api/sessions/{session_id}/status
       Returns: { status: "running" | "completed" | "not_found" }
```

**Purpose:** Manage session lifecycle and state.

### Query API (Agent Execution)

```
POST   /api/query
       Headers: X-Session-ID
       Body: AgentQueryRequest
       Returns: AgentQueryResponse { status, session_id, query_id, message? }
```

**Purpose:** Execute agent queries. Events emitted to session's event store.

### Chat History API (Database Persistence)

```
POST   /api/chat-history/sessions
GET    /api/chat-history/sessions?limit={n}&offset={n}
GET    /api/chat-history/sessions/{session_id}
PUT    /api/chat-history/sessions/{session_id}
DELETE /api/chat-history/sessions/{session_id}
GET    /api/chat-history/sessions/{session_id}/events
GET    /api/chat-history/events
```

**Purpose:** Persistent storage and retrieval of sessions and events from database.

---

## 🔄 Complete Flow: Page Load to Event Display

### 1. Initial Page Load

**Chat Mode:**
```
App.tsx (mounts)
  └── useEffect: createDefaultTabIfNeeded()
      └── useChatStore.createChatTab("Chat 1", { mode: 'chat' })
          ├── Generate sessionId: "550e8400-e29b-41d4-a716-446655440000"
          └── Store: Create tab with { tabId, sessionId, metadata: { mode: 'chat' } }
```

**Workflow Mode:**
```
App.tsx (mounts)
  └── useEffect: createDefaultTabIfNeeded()
      └── Skip (workflow mode doesn't create default tab)
          └── Tab created only when user starts a phase/execution
```

**Component Tree - Chat Mode:**
```
App
├── ModePresetBar (renders)
├── ChatTabs
│   └── Tab: "Chat 1" (mode: 'chat', sessionId: 550e8400-...)
└── EventModeProvider (per-tab scope)
    └── ChatAreaWithObserverId
        └── ChatArea (sessionId: 550e8400-...)
            ├── ChatHeader (renders)
            ├── EventDisplay (events: []) ← Empty, no events yet
            └── ChatInput (renders, ready for input)
```

**Component Tree - Workflow Mode (After User Starts Phase):**
```
App
├── ModePresetBar (renders)
├── ChatTabs (shows chat tabs only)
└── EventModeProvider (per-tab scope)
    └── WorkflowLayout
        ├── WorkflowChatTabs
        │   └── Tab: "Planning" (mode: 'workflow', sessionId: 660f9511-...)
        ├── ChatHeader (renders workflow preset name)
        ├── WorkflowCanvas (shows active phase)
        └── ChatArea (hideHeader, hideInput, compact)
            └── EventDisplay (events: []) ← Empty, no events yet
```

### 2. User Submits Query

**Chat Mode:**
```
ChatInput.onSubmit()
  └── ChatArea.submitQuery()
      ├── API: POST /api/query
      │   Headers: { "X-Session-ID": "550e8400-..." }
      │   Request: { "query": "...", "agent_mode": "chat", ... }
      │   Response: { "status": "started", "session_id": "550e8400-..." }
      ├── Store: setTabStreaming(tabId, true)
      └── Store: addTabEvents(sessionId, [userMessageEvent])
```

### 3. Event Polling (Every 1 second)

**Both Modes (Same Flow):**
```
ChatArea.pollEvents() (setInterval, every 1000ms)
  ├── Get all tabs with sessionId (both chat and workflow tabs)
  ├── For each tab:
  │   └── API: GET /api/sessions/{session_id}/events?since={lastIndex}&event_mode={mode}
  │       Response: {
  │         "events": [
  │           { "type": "agent_message", "data": { "content": "..." } },
  │           { "type": "agent_end" }
  │         ],
  │         "has_more": false,
  │         "session_id": "550e8400-..."
  │       }
  │   └── Store: addTabEvents(sessionId, newEvents)
  └── Store: setTabLastEventIndex(sessionId, newLastIndex)
```

### 4. Tab Switching

**Switching Between Chat and Workflow Tabs:**
```
User clicks Tab 2 (workflow tab)
  └── ChatTabs.handleTabClick("workflow_1766227475472")
      └── useChatStore.switchTab("workflow_1766227475472")
          └── Store: activeTabId = "workflow_1766227475472"

App.tsx (re-renders based on selectedModeCategory)
  └── selectedModeCategory === 'workflow'
      └── Render WorkflowLayout
          └── ChatArea (sessionId: "660f9511-...")
              └── EventDisplay (events: tabEvents["660f9511-..."])
```

**Note:** App.tsx renders based on `selectedModeCategory` from `useModeStore`, not tab metadata. Mode switching changes `selectedModeCategory`, which determines which layout to render.

### 5. Session Status Polling (Every 5 seconds)

**Both Modes (Same Flow):**
```
ChatTabs.useEffect (setInterval, every 5000ms)
  └── useChatStore.fetchAllTabSessionStatuses([tabId1, tabId2])
      ├── API: GET /api/sessions/active
      │   Response: {
      │     "active_sessions": [
      │       { "session_id": "550e8400-...", "agent_mode": "chat", "status": "running" },
      │       { "session_id": "660f9511-...", "agent_mode": "workflow", "status": "running" }
      │     ]
      │   }
      └── For each active session:
          └── API: GET /api/sessions/{session_id}/status
              Response: { "status": "running", "agent_mode": "chat" | "workflow", ... }
          └── Store: tabSessionStatus[tabId] = { status: "running", agent_mode: "...", ... }
```

---

## 🏗️ Architecture: Session-Based Event Storage

### Implementation Status

✅ **Completed**: Refactored from observer-based to session-based event storage.

### Architecture

**Backend:**
- **EventStore**: Stores events by `sessionID` (not `observerID`)
- **Polling API**: `/api/sessions/{session_id}/events?since={index}&event_mode={mode}`
- **Query API**: Only requires `X-Session-ID` header (no observer ID needed)

**Frontend:**
- **API Client**: `agentApi.getSessionEvents(sessionId, sinceIndex, options)` 
- **useChatStore**: Tracks `tabEventIndices` by `sessionId`
- **ChatArea**: Polls by `sessionId` instead of `observerId`
- **No observer registration**: Removed - sessions are used directly

### Benefits

- ✅ **Multiple viewers per session**: Each tab maintains its own polling position on frontend
- ✅ **Simpler backend**: No per-observer state tracking, stateless polling
- ✅ **Conceptual clarity**: Events belong to sessions, not observers
- ✅ **True independence**: Multiple tabs can watch the same session independently

### Storage

- **In-Memory**: Events stored by `sessionID` (not `observerID`)
- **Database**: Stores by `session_id` (no change needed)
- **Frontend Polling State**: `lastEventIndex` tracked per tab in `tabEventIndices[sessionId]`

---

## 🔍 For LLMs: Quick Reference

### Rendering Logic

**App.tsx renders based on `selectedModeCategory` from `useModeStore`:**
- `selectedModeCategory === 'workflow'` → Renders `WorkflowLayout`
- `selectedModeCategory === 'chat'` → Renders `ChatAreaWithObserverId`

**Tab filtering in ChatTabs:**
- Workflow mode: Only shows chat tabs (workflow tabs shown in `WorkflowChatTabs`)
- Chat mode: Shows all chat tabs

### API Methods

```typescript
// Get events for a session
agentApi.getSessionEvents(sessionId, sinceIndex, { eventMode: 'basic' | 'advanced' })

// Stop a session
agentApi.stopSession(sessionId)

// Get active sessions
agentApi.getActiveSessions()

// Get session status
agentApi.getSessionStatus(sessionId)

// Submit query
agentApi.startQuery(request, sessionId)
```

### Constraints

✅ **Allowed:**
- Creating multiple tabs with different `sessionId`s
- Switching tabs while polling is active
- Storing events per `sessionId` (not per tab)
- Filtering events by `eventMode` (basic/advanced)

❌ **Forbidden:**
- Using observer IDs (system uses session IDs)
- Rendering based on tab metadata (use `selectedModeCategory` instead)
- Storing events globally (must use `tabEvents[sessionId]`)

---

## 📖 Related Documentation

- [Multi-Tab Chat Architecture](multi_tab_chat_architecture.md) - Complete multi-tab implementation details
- [Frontend API Structure](frontend_api_structure_data_model.md) - This document

---

## Summary

The frontend uses a session-based architecture with unified tab management. `App.tsx` renders layouts based on `selectedModeCategory` from `useModeStore`. Chat tabs are shown in `ChatTabs` (filtered by mode), while workflow tabs are shown in `WorkflowChatTabs` within `WorkflowLayout`. Events are stored per `sessionId` and polled independently per tab. The `EventModeProvider` manages event display mode (basic/advanced) per tab.
