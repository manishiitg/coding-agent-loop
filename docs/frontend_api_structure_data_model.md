# API Structure and Data Model

## React Component Structure

**Component Hierarchy:**

```
App
├── ModePresetBar (Global mode/preset selection)
├── ChatTabs (Tab navigation - global, no EventModeProvider)
└── EventModeProvider (Per-tab scope - wraps content area only)
    └── Content Area
        ├── WorkflowLayout (workflow mode)
        │   └── ChatArea (with hideHeader, hideInput, compact)
        └── ChatAreaWithSessionId (chat mode)
            └── ChatArea
                ├── ChatHeader (session title/status)
                ├── EventDisplay (events list - uses EventMode context)
                └── ChatInput (query input)
```

### Key Components

**App.tsx**
- Root component
- Manages global state (mode, presets, file content)
- Creates default tab on page load
- Wraps content with EventModeProvider

**ChatTabs.tsx**
- Tab navigation (create, switch, close tabs)
- Displays session status per tab
- Polls session status every 5 seconds
- Contains EventModeToggle (uses store directly, not context - works outside EventModeProvider)

**ChatArea.tsx**
- Main chat interface
- Manages session-based event polling
- Filters events by active tab's sessionId
- Handles query submission

**EventDisplay.tsx**
- Displays events list
- Receives events as prop (tab-specific)
- No direct store access for events (prevents cross-tab mixing)
- Uses EventMode context (from EventModeProvider) to filter events by basic/advanced mode

**ChatInput.tsx**
- Query input and submission
- Uses active tab's sessionId
- No "New Chat" button (new chats = new tabs)

### State Management

**useChatStore (Zustand)**
- `chatTabs: Record<string, ChatTab>` - All tabs
- `activeTabId: string` - Currently active tab
- `tabEvents: Record<string, PollingEvent[]>` - Events keyed by sessionId
- `tabEventIndices: Record<string, number>` - Last event index per sessionId

**Tab Structure (ChatTab)**
```typescript
{
  tabId: string
  name: string
  sessionId: string | null
  isStreaming: boolean
  isCompleted: boolean
  eventMode: 'basic' | 'advanced'  // Per-tab event display mode
  config: TabConfig
  metadata?: { mode: 'chat' | 'workflow', phaseId?: string }
}
```

### Event Flow

1. **Tab Creation**: `createChatTab()` → generates sessionId → creates tab with sessionId
2. **Query Submission**: `ChatInput` → `ChatArea.submitQuery()` → `POST /api/query` with sessionId
3. **Event Polling**: `ChatArea.pollEvents()` → polls all tabs by sessionId → `GET /api/sessions/{session_id}/events`
4. **Event Storage**: Events stored in `tabEvents[sessionId]` via `addTabEvents()`
5. **Event Display**: `ChatArea` filters `tabEvents` by active tab's `sessionId` → passes to `EventDisplay`

### Isolation Strategy

- **Per-Tab Session IDs**: Each tab has unique `sessionId`
- **Per-Tab Event Storage**: Events stored keyed by `sessionId` in `tabEvents`
- **Per-Tab Event Mode**: Each tab has its own `eventMode` ('basic' | 'advanced') stored in `ChatTab`
- **Prop-Based Filtering**: `ChatArea` filters events by `sessionId` prop
- **EventModeProvider Scope**: Only wraps content area (ChatArea/WorkflowLayout), not ChatTabs
- **No Global Fallbacks**: Components never fall back to global events
- **Frontend-Managed Polling**: Each tab maintains its own `lastEventIndex` for polling state

### Component → API Mapping

**App.tsx**
- No direct API calls (delegates to child components)

**useChatStore (Zustand Store)**
- `createChatTab()` → Generates sessionId and creates tab (no observer registration needed)
- `fetchTabSessionStatus()` → `GET /api/sessions/active` + `GET /api/sessions/{session_id}/status`
- `fetchAllTabSessionStatuses()` → `GET /api/sessions/active` + `GET /api/sessions/{session_id}/status` (for multiple tabs)
- `closeTab()` → `POST /api/session/stop` (if tab has sessionId and is streaming)

**ChatArea.tsx**
- `pollEvents()` → `GET /api/sessions/{session_id}/events?since={index}` (polls all tabs by sessionId)
- `submitQueryWithQuery()` → `POST /api/query` (with X-Session-ID header)
- `checkActiveSessions()` → `GET /api/sessions/active` (checks for active sessions on mount)
- `reconnectSession()` → `POST /api/sessions/{session_id}/reconnect` (reconnects to active session)
- `getSessionStatus()` → `GET /api/sessions/{session_id}/status` (checks session status)
- `getSessionEvents()` → `GET /api/chat-history/sessions/{session_id}/events` (loads historical events)

**ChatTabs.tsx**
- No direct API calls (uses `useChatStore.fetchAllTabSessionStatuses()`)

**ChatInput.tsx**
- No direct API calls (calls `ChatArea.submitQueryWithQuery()` via prop)

**EventDisplay.tsx**
- `submitHumanFeedback()` → `POST /api/human-feedback/submit` (submits human feedback)

**WorkflowLayout.tsx**
- No direct API calls (delegates to ChatArea)

## Data Model

**Hierarchy: Session → Events**

- **One Session** = One Tab (1:1 relationship)
- **One Session** has **Multiple Events** (1:N relationship)
- **Multiple Tabs** can view the same session (each maintains its own polling state)

### Relationships

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
- Each tab has its own unique `sessionId` (typically, but can share sessions)
- Events are stored in-memory per `sessionId` (for real-time polling)
- Events are also persisted to database with `session_id` (for history)
- Frontend manages polling state (`lastEventIndex`) per tab, enabling multiple independent viewers

## API Structure

### Session Events API (Real-time Event Polling)
```
GET    /api/sessions/{session_id}/events?since={index}
       Returns: { events: Event[], has_more: boolean, session_id: string }
```

**Purpose:** Real-time event delivery via polling. Events stored in-memory keyed by `session_id`. Frontend manages polling state (`since` parameter) per tab.

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

## Data Flow

1. **Tab Creation**: Frontend generates `sessionId` (UUID) and creates tab
2. **Query Submission**: `POST /api/query` with `X-Session-ID` header
3. **Event Emission**: Backend emits events to session's in-memory event store
4. **Event Polling**: Frontend polls `GET /api/sessions/{session_id}/events?since={index}` every 1 second
5. **Event Persistence**: Backend saves events to database with `session_id` for history
6. **Polling State**: Frontend tracks `lastEventIndex` per tab for independent polling positions

## Storage

- **In-Memory**: Events stored in `EventStore` keyed by `session_id` for real-time polling
- **Database**: Events persisted with `session_id` and `chat_session_id` for historical queries
- **Frontend Polling State**: `lastEventIndex` tracked per tab in `tabEventIndices[sessionId]`

## Key Identifiers

- `session_id`: Unique per tab (typically), used for session management, event storage, and database persistence
- Events are stored and retrieved by `session_id` (both in-memory and database)
- Frontend manages polling state per tab, enabling multiple independent viewers of the same session

## API Response Examples

### Get Session Events
```json
GET /api/sessions/{session_id}/events?since=0
Response: {
  "events": [
    {
      "id": "event_123",
      "type": "user_message",
      "timestamp": "2024-01-15T10:30:00Z",
      "data": { "content": "Hello" },
      "session_id": "550e8400-e29b-41d4-a716-446655440000"
    },
    {
      "id": "event_124",
      "type": "agent_message",
      "timestamp": "2024-01-15T10:30:05Z",
      "data": { "content": "Hi there!" },
      "session_id": "550e8400-e29b-41d4-a716-446655440000"
    }
  ],
  "has_more": false,
  "session_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### Active Sessions
```json
GET /api/sessions/active
Response: {
  "active_sessions": [
    {
      "session_id": "550e8400-e29b-41d4-a716-446655440000",
      "agent_mode": "chat",
      "status": "running",
      "query": "Hello",
      "created_at": "2024-01-15T10:29:00Z",
      "last_activity": "2024-01-15T10:30:05Z"
    }
  ],
  "total": 1
}
```

### Query Submission
```json
POST /api/query
Headers: {
  "X-Session-ID": "550e8400-e29b-41d4-a716-446655440000"
}
Request: {
  "query": "What is the weather?",
  "agent_mode": "chat",
  "llm_config": { ... }
}
Response: {
  "status": "started",
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "query_id": "query_123",
  "message": "Query started"
}
```

## Complete Flow: Page Load to Event Display

### 1. Initial Page Load

**Chat Mode:**
```
App.tsx (mounts)
  └── useEffect: hasCompletedInitialSetup = true
  └── useEffect: createDefaultTabIfNeeded()
      └── useChatStore.createChatTab("Chat 1", { mode: 'chat' })
          ├── Generate sessionId: "550e8400-e29b-41d4-a716-446655440000"
          └── Store: Create tab with { tabId, sessionId, metadata: { mode: 'chat' } }
```

**Workflow Mode:**
```
App.tsx (mounts)
  └── useEffect: hasCompletedInitialSetup = true
  └── useEffect: createDefaultTabIfNeeded()
      └── Skip (workflow mode doesn't create default tab)
          └── Tab created only when user starts a phase/execution
```

**Component Tree - Chat Mode:**
```
App
├── ModePresetBar (renders)
├── ChatTabs
│   └── Tab: "Chat 1" (mode: 'chat', sessionId: 550e8400-e29b-41d4-a716-446655440000)
└── EventModeProvider (per-tab scope)
    └── ChatAreaWithSessionId
        └── ChatArea (sessionId: 550e8400-e29b-41d4-a716-446655440000)
            ├── ChatHeader (renders)
            ├── EventDisplay (events: []) ← Empty, no events yet (uses EventMode context)
            └── ChatInput (renders, ready for input)
```

**Component Tree - Workflow Mode (Initial Load - No Tabs):**
```
App
├── ModePresetBar (renders)
├── ChatTabs
│   └── (No tabs yet - user must start a phase/execution to create tab)
└── EventModeProvider (per-tab scope)
    └── WorkflowLayout
        ├── ChatHeader (renders workflow preset name)
        ├── WorkflowCanvas (main workflow visualization - user can start phase here)
        └── ChatArea (hideHeader, hideInput, compact)
            └── EventDisplay (events: []) ← Empty, no tab/observer yet
```

**Component Tree - Workflow Mode (After User Starts Phase):**
```
App
├── ModePresetBar (renders)
├── ChatTabs
│   └── Tab: "Planning" (mode: 'workflow', sessionId: 660f9511-f39c-52e5-b827-557766551111)
└── EventModeProvider (per-tab scope)
    └── WorkflowLayout
        ├── ChatHeader (renders workflow preset name)
        ├── WorkflowCanvas (shows active phase)
        └── ChatArea (hideHeader, hideInput, compact)
            └── EventDisplay (events: []) ← Empty, no events yet (uses EventMode context)
```

### 2. User Submits Query

**Chat Mode:**
```
ChatInput.onSubmit()
  └── ChatArea.submitQueryWithQuery("What is the weather?")
      ├── Ensure tab has sessionId (already has: "550e8400-...")
      ├── API: POST /api/query
      │   Headers: {
      │     "X-Session-ID": "550e8400-e29b-41d4-a716-446655440000"
      │   }
      │   Request: {
      │     "query": "What is the weather?",
      │     "agent_mode": "chat",
      │     ...
      │   }
      │   Response: { "status": "started", "session_id": "550e8400-e29b-41d4-a716-446655440000" }
      ├── Store: setTabStreaming(tabId, true)
      └── Store: addTabEvents(sessionId, [userMessageEvent])
```

**Workflow Mode:**
```
WorkflowCanvas.onStartPhase() or ChatArea (if chat panel open)
  └── ChatArea.submitQueryWithQuery("Create a todo list for building a website")
      ├── WorkflowModeHandler.handleChatSubmit() (if in planning phase)
      │   └── API: POST /api/workflow/create
      │       Request: {
      │         "preset_query_id": "preset_123",
      │         "objective": "Create a todo list for building a website"
      │       }
      │       Response: {
      │         "workflow": { "id": "workflow_456", ... },
      │         "status": "created"
      │       }
      ├── API: POST /api/query
      │   Headers: {
      │     "X-Session-ID": "660f9511-f39c-52e5-b827-557766551111"
      │   }
      │   Request: {
      │     "query": "Create a todo list for building a website",
      │     "agent_mode": "workflow",
      │     "preset_query_id": "preset_123",
      │     ...
      │   }
      │   Response: { "status": "workflow_started", "session_id": "660f9511-f39c-52e5-b827-557766551111" }
      ├── Store: setTabStreaming(tabId, true)
      └── Store: addTabEvents(sessionId, [userMessageEvent])
```

**Component Tree - Chat Mode:**
```
App
└── ChatArea
    ├── ChatHeader (shows session title)
    ├── EventDisplay (events: [userMessageEvent]) ← User message appears
    └── ChatInput (disabled, isStreaming: true)
```

**Component Tree - Workflow Mode:**
```
App
└── WorkflowLayout
    ├── WorkflowCanvas (shows workflow visualization)
    └── ChatArea (compact mode)
        └── EventDisplay (events: [userMessageEvent]) ← User message appears
```

### 3. Event Polling (Every 1 second)

**Both Modes (Same Flow):**
```
ChatArea.pollEvents() (setInterval, every 1000ms)
  ├── Get all tabs with sessionId (both chat and workflow tabs)
  ├── For each tab:
  │   └── API: GET /api/sessions/{session_id}/events?since={lastIndex}
  │       Response: {
  │         "events": [
  │           { "type": "agent_message", "data": { "content": "It's sunny!" } },
  │           { "type": "agent_end" }
  │         ],
  │         "has_more": false,
  │         "session_id": "550e8400-e29b-41d4-a716-446655440000"
  │       }
  │   └── Store: addTabEvents(sessionId, newEvents)
  └── Store: setTabLastEventIndex(sessionId, newLastIndex)
```

**Component Tree - Chat Mode:**
```
App
└── ChatArea
    ├── ChatHeader
    ├── EventDisplay (events: [
    │     userMessageEvent,
    │     agentMessageEvent,  ← New event added
    │     agentEndEvent        ← New event added
    │   ])
    └── ChatInput (enabled, isStreaming: false)
```

**Component Tree - Workflow Mode:**
```
App
└── WorkflowLayout
    ├── WorkflowCanvas (updates based on workflow events)
    └── ChatArea (compact)
        └── EventDisplay (events: [
              userMessageEvent,
              workflow_start_event,  ← New event added
              todo_list_event,        ← New event added
              agentEndEvent           ← New event added
            ])
```

### 4. Tab Switching

**Switching Between Chat and Workflow Tabs:**
```
User clicks Tab 2 (workflow tab)
  └── ChatTabs.handleTabClick("workflow_1766227475472")
      └── useChatStore.switchTab("workflow_1766227475472")
          └── Store: activeTabId = "workflow_1766227475472"

App.tsx (re-renders based on selectedModeCategory)
  ├── If tab.metadata.mode === 'workflow':
  │   └── Render WorkflowLayout
  │       └── ChatArea (hideHeader, hideInput, compact)
  └── If tab.metadata.mode === 'chat':
      └── Render ChatAreaWithSessionId
          └── ChatArea (full mode with header and input)

ChatAreaWithSessionId / WorkflowLayout (re-renders)
  ├── activeTabId changes → "workflow_1766227475472"
  ├── Get tab: { sessionId: "660f9511-f39c-52e5-b827-557766551111", metadata: { mode: 'workflow' }, ... }
  └── ChatArea (sessionId: "660f9511-f39c-52e5-b827-557766551111")
      └── tabEvents = useMemo(() => 
            tabEventsStore["660f9511-f39c-52e5-b827-557766551111"] || []
          )
      └── EventDisplay (events: [12 events for session 660f9511-f39c-52e5-b827-557766551111])
```

**Component Tree:**
```
App
└── ChatTabs
    ├── Tab 1: "Chat 1" (mode: 'chat', inactive)
    └── Tab 2: "Workflow 1" (mode: 'workflow', active) ← Switched to this
└── WorkflowLayout (renders because tab.mode === 'workflow')
    ├── WorkflowCanvas
    └── ChatArea (sessionId: 660f9511-f39c-52e5-b827-557766551111)
        └── EventDisplay (events: [12 events]) ← Shows Workflow tab's events
```

### 5. Session Status Polling (Every 5 seconds)

**Both Modes (Same Flow):**
```
ChatTabs.useEffect (setInterval, every 5000ms)
  └── useChatStore.fetchAllTabSessionStatuses([tabId1, tabId2])
      ├── API: GET /api/sessions/active
      │   Response: {
      │     "active_sessions": [
      │       { 
      │         "session_id": "550e8400-e29b-41d4-a716-446655440000", 
      │         "agent_mode": "chat",
      │         "status": "running" 
      │       },
      │       { 
      │         "session_id": "660f9511-f39c-52e5-b827-557766551111", 
      │         "agent_mode": "workflow",
      │         "status": "running" 
      │       }
      │     ]
      │   }
      └── For each active session:
          └── API: GET /api/sessions/{session_id}/status
              Response: { 
                "status": "running", 
                "agent_mode": "chat" | "workflow", 
                ... 
              }
          └── Store: tabSessionStatus[tabId] = { status: "running", agent_mode: "...", ... }
```

**Component Tree:**
```
App
└── ChatTabs
    ├── Tab 1: "Chat 1" (status: "running", agent_mode: "chat" ← Status dot shows green)
    └── Tab 2: "Workflow 1" (status: "running", agent_mode: "workflow" ← Status dot shows green)
```

## Complete Data Flow Diagram

### Chat Mode Flow

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. PAGE LOAD (Chat Mode)                                        │
├─────────────────────────────────────────────────────────────────┤
│ App.tsx                                                         │
│   └── createDefaultTabIfNeeded()                               │
│       └── useChatStore.createChatTab("Chat 1", { mode: 'chat' })│
│           ├── Generate: sessionId = UUID()                    │
│           └── Store: chatTabs[tabId] = {                      │
│                 sessionId, metadata: { mode: 'chat' }         │
│               }                                                │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│ 2. COMPONENT RENDER (Chat Mode)                                  │
├─────────────────────────────────────────────────────────────────┤
│ selectedModeCategory === 'chat'                                 │
│   └── ChatAreaWithSessionId                                    │
│       ├── Get activeTab from store                              │
│       └── ChatArea (sessionId: "550e8400-...")                 │
│           ├── ChatHeader                                        │
│           ├── EventDisplay (events: []) ← Empty              │
│           └── ChatInput                                         │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│ 3. USER SUBMITS QUERY (Chat Mode)                               │
├─────────────────────────────────────────────────────────────────┤
│ ChatInput.onSubmit()                                            │
│   └── ChatArea.submitQueryWithQuery()                          │
│       ├── API: POST /api/query                                 │
│       │   Headers: X-Session-ID                               │
│       │   Request: { "query": "...", "agent_mode": "chat" }    │
│       │   → Response: { status: "started", session_id: "..." }│
│       ├── Store: addTabEvents(sessionId, [userEvent])        │
│       └── Store: setTabStreaming(tabId, true)                 │
└─────────────────────────────────────────────────────────────────┘
```

### Workflow Mode Flow

```
┌─────────────────────────────────────────────────────────────────┐
│ 1. PAGE LOAD (Workflow Mode)                                    │
├─────────────────────────────────────────────────────────────────┤
│ App.tsx                                                         │
│   └── createDefaultTabIfNeeded()                               │
│       └── Skip (workflow mode doesn't create default tab)      │
│           └── No tab created - user must start phase/execution  │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│ 2. COMPONENT RENDER (Workflow Mode - No Tabs Yet)              │
├─────────────────────────────────────────────────────────────────┤
│ selectedModeCategory === 'workflow'                             │
│   └── WorkflowLayout                                            │
│       ├── ChatHeader (shows preset name)                       │
│       ├── WorkflowCanvas (main visualization - user can start) │
│       └── ChatArea (hideHeader, hideInput, compact)            │
│           └── EventDisplay (events: []) ← No tab/observer yet │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│ 3. USER STARTS WORKFLOW PHASE (Workflow Mode)                  │
├─────────────────────────────────────────────────────────────────┤
│ WorkflowCanvas.onStartPhase() or user action                   │
│   └── Tab creation (if needed) + ChatArea.submitQueryWithQuery()│
│       ├── useChatStore.createChatTab("Planning", { mode: 'workflow', phaseId: 'planning' })│
│       │   ├── Generate: sessionId = UUID()                    │
│       │   └── Store: chatTabs[tabId] = {                      │
│       │         sessionId, metadata: { mode: 'workflow', phaseId: 'planning' }│
│       │       }                                                │
│       ├── WorkflowModeHandler.handleChatSubmit() (if planning) │
│       │   └── API: POST /api/workflow/create                  │
│       │       Request: { "preset_query_id": "...", "objective": "..." }│
│       │       → Response: { "workflow": { "id": "workflow_123" } }│
│       ├── API: POST /api/query                                 │
│       │   Headers: X-Session-ID                               │
│       │   Request: { "query": "...", "agent_mode": "workflow", │
│       │             "preset_query_id": "...", "step_id": "..." }│
│       │   → Response: { status: "workflow_started", session_id: "..." }│
│       ├── Store: addTabEvents(sessionId, [userEvent])        │
│       └── Store: setTabStreaming(tabId, true)                 │
└─────────────────────────────────────────────────────────────────┘
```

### Shared Flow (Both Modes)

```
┌─────────────────────────────────────────────────────────────────┐
│ 4. EVENT POLLING (Every 1s) - Both Modes                       │
├─────────────────────────────────────────────────────────────────┤
│ ChatArea.pollEvents()                                           │
│   ├── Get all tabs with sessionId (chat + workflow)          │
│   ├── For each tab:                                            │
│   │   └── API: GET /api/sessions/{session_id}/events?since={index}│
│   │       → Response: {                                        │
│   │             events: [agentEvent1, agentEvent2],           │
│   │             has_more: false,                               │
│   │             session_id: "..."                              │
│   │           }                                                │
│   └── Store: addTabEvents(sessionId, newEvents)              │
│       └── Component re-renders (ChatArea or WorkflowLayout)   │
│           └── EventDisplay (events: [userEvent, agentEvent1,   │
│                                    agentEvent2])               │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│ 5. TAB SWITCH - Between Chat and Workflow                       │
├─────────────────────────────────────────────────────────────────┤
│ User clicks different mode tab                                  │
│   └── ChatTabs.handleTabClick()                                │
│       └── useChatStore.switchTab(tabId)                       │
│           └── activeTabId = tabId                              │
│               └── App.tsx checks tab.metadata.mode            │
│                   ├── If mode === 'workflow':                   │
│                   │   └── Render WorkflowLayout               │
│                   └── If mode === 'chat':                      │
│                       └── Render ChatAreaWithSessionId       │
│                           └── ChatArea (sessionId: newSessionId)│
│                               └── EventDisplay (events: tabEvents[│
│                                                   newSessionId])│
└─────────────────────────────────────────────────────────────────┘
```

## State Updates Flow

```
API Response → Store Update → Component Re-render

Example: New Event Received
1. API: GET /api/sessions/{session_id}/events?since={index}
   → Response: { events: [newEvent], session_id: "..." }

2. Store: addTabEvents(sessionId, [newEvent])
   → tabEvents[sessionId] = [...existingEvents, newEvent]

3. ChatArea: tabEvents useMemo recalculates
   → displayEvents = tabEvents[sessionId]

4. EventDisplay: Receives new events prop
   → Re-renders with new events
```

## Architecture: Session-Based Event Storage

### Implementation Status
✅ **Completed**: Refactored from observer-based to session-based event storage.

### Architecture

**Backend:**
- **EventStore**: Stores events by `sessionID` (not `observerID`)
- **BaseEventBridge**: Stores events using `sessionID`
- **Polling API**: `/api/sessions/{session_id}/events?since={index}`
- **Query API**: Only requires `X-Session-ID` header (no observer ID needed)
- **No event counters**: Removed - timestamp + randomSuffix provides uniqueness

**Frontend:**
- **API Client**: `getSessionEvents(sessionId, sinceIndex)` 
- **useChatStore**: Tracks `tabEventIndices` by `sessionId`
- **ChatArea**: Polls by `sessionId` instead of `observerId`
- **No observer registration**: Removed - sessions are used directly

### Benefits
- ✅ **Multiple viewers per session**: Each tab maintains its own polling position on frontend
- ✅ **Simpler backend**: No per-observer state tracking, stateless polling
- ✅ **Conceptual clarity**: Events belong to sessions, not observers
- ✅ **True independence**: Multiple tabs can watch the same session independently

### Current Architecture

```
Session (session_id: "abc123")
  ├── Events: [event1, event2, event3, ...] ← Stored by sessionID
  │
  ├── Tab 1 (sessionId: "abc123")
  │   └── Frontend tracks: lastEventIndex = 2
  │   └── Polls: GET /api/sessions/abc123/events?since=2
  │
  └── Tab 2 (sessionId: "abc123")  ← Can view same session
      └── Frontend tracks: lastEventIndex = 0
      └── Polls: GET /api/sessions/abc123/events?since=0
```

**Storage:**
- **In-Memory**: Events stored by `sessionID` (not `observerID`)
- **Database**: Stores by `session_id` (no change needed)
- **Polling State**: Managed by frontend per tab in `tabEventIndices[sessionId]`

