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
        └── ChatAreaWithObserverId (chat mode)
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
- Manages observer registration and event polling
- Filters events by active tab's observerId
- Handles query submission

**EventDisplay.tsx**
- Displays events list
- Receives events as prop (tab-specific)
- No direct store access for events (prevents cross-tab mixing)
- Uses EventMode context (from EventModeProvider) to filter events by basic/advanced mode

**ChatInput.tsx**
- Query input and submission
- Uses active tab's observerId and sessionId
- No "New Chat" button (new chats = new tabs)

### State Management

**useChatStore (Zustand)**
- `chatTabs: Record<string, ChatTab>` - All tabs
- `activeTabId: string` - Currently active tab
- `tabEvents: Record<string, PollingEvent[]>` - Events keyed by observerId
- `tabEventIndices: Record<string, number>` - Last event index per observerId

**Tab Structure (ChatTab)**
```typescript
{
  tabId: string
  name: string
  observerId: string
  sessionId: string | null
  isStreaming: boolean
  isCompleted: boolean
  eventMode: 'basic' | 'advanced'  // Per-tab event display mode
  config: TabConfig
  metadata?: { mode: 'chat' | 'workflow', phaseId?: string }
}
```

### Event Flow

1. **Tab Creation**: `createChatTab()` → generates sessionId → registers observer → gets observerId
2. **Query Submission**: `ChatInput` → `ChatArea.submitQuery()` → `POST /api/query` with sessionId/observerId
3. **Event Polling**: `ChatArea.pollEvents()` → polls all tabs with observerId → `GET /api/observer/{observer_id}/events`
4. **Event Storage**: Events stored in `tabEvents[observerId]` via `addTabEvents()`
5. **Event Display**: `ChatArea` filters `tabEvents` by active tab's `observerId` → passes to `EventDisplay`

### Isolation Strategy

- **Per-Tab Observer IDs**: Each tab has unique `observerId`
- **Per-Tab Event Storage**: Events stored keyed by `observerId` in `tabEvents`
- **Per-Tab Event Mode**: Each tab has its own `eventMode` ('basic' | 'advanced') stored in `ChatTab`
- **Prop-Based Filtering**: `ChatArea` filters events by `observerId` prop
- **EventModeProvider Scope**: Only wraps content area (ChatArea/WorkflowLayout), not ChatTabs
- **No Global Fallbacks**: Components never fall back to global events

### Component → API Mapping

**App.tsx**
- No direct API calls (delegates to child components)

**useChatStore (Zustand Store)**
- `createChatTab()` → `POST /api/observer/register` (registers observer with sessionId)
- `fetchTabSessionStatus()` → `GET /api/sessions/active` + `GET /api/sessions/{session_id}/status`
- `fetchAllTabSessionStatuses()` → `GET /api/sessions/active` + `GET /api/sessions/{session_id}/status` (for multiple tabs)
- `closeTab()` → `POST /api/session/stop` (if tab has sessionId and is streaming)

**ChatArea.tsx**
- `pollEvents()` → `GET /api/observer/{observer_id}/events` (polls all tabs with observerId)
- `submitQueryWithQuery()` → `POST /api/query` (with X-Session-ID and X-Observer-ID headers)
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

**Hierarchy: Session → Observers → Events**

- **One Session** = One Tab (1:1 relationship)
- **One Session** can have **Multiple Observers** (1:N relationship)
- **One Observer** has **Multiple Events** (1:N relationship)

### Relationships

```
Session (session_id)
  └── Observer 1 (observer_id_1) → Events [event1, event2, ...]
  └── Observer 2 (observer_id_2) → Events [event3, event4, ...]
```

**Key Points:**
- Each tab has its own unique `sessionId`
- Each tab has its own unique `observerId` 
- Events are stored in-memory per `observerId` (for real-time polling)
- Events are also persisted to database with `session_id` (for history)

## API Structure

### Observer API (Real-time Event Polling)
```
POST   /api/observer/register
       Body: { session_id: string }
       Returns: { observer_id: string }

GET    /api/observer/{observer_id}/events?since={index}
       Returns: { events: Event[], last_event_index: number }

GET    /api/observer/{observer_id}/status
       Returns: { observer_id, status, total_events, session_id, agent_mode }

DELETE /api/observer/{observer_id}
```

**Purpose:** Real-time event delivery via polling. Events stored in-memory keyed by `observer_id`.

### Session API (Session Management)
```
POST   /api/session/stop
       Headers: X-Session-ID

POST   /api/session/clear
       Headers: X-Session-ID

GET    /api/sessions/active
       Returns: { active_sessions: ActiveSessionInfo[] }

POST   /api/sessions/{session_id}/reconnect
       Returns: { observer_id: string }

GET    /api/sessions/{session_id}/status
       Returns: { status: "running" | "completed" | "not_found" }
```

**Purpose:** Manage session lifecycle and state.

### Query API (Agent Execution)
```
POST   /api/query
       Headers: X-Session-ID, X-Observer-ID
       Body: AgentQueryRequest
       Returns: AgentQueryResponse
```

**Purpose:** Execute agent queries. Events emitted to observer's event store.

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

1. **Tab Creation**: Frontend generates `sessionId` (UUID)
2. **Observer Registration**: `POST /api/observer/register` with `sessionId` → returns `observerId`
3. **Query Submission**: `POST /api/query` with `X-Session-ID` and `X-Observer-ID` headers
4. **Event Emission**: Backend emits events to observer's in-memory event store
5. **Event Polling**: Frontend polls `GET /api/observer/{observer_id}/events` every 1 second
6. **Event Persistence**: Backend saves events to database with `session_id` for history

## Storage

- **In-Memory**: Events stored in `EventStore` keyed by `observer_id` for real-time polling
- **Database**: Events persisted with `session_id` and `chat_session_id` for historical queries

## Key Identifiers

- `session_id`: Unique per tab, used for session management and database persistence
- `observer_id`: Unique per tab, used for real-time event polling
- Events have both `session_id` (for grouping) and are stored per `observer_id` (for isolation)

## API Response Examples

### Observer Registration
```json
POST /api/observer/register
Request: { "session_id": "550e8400-e29b-41d4-a716-446655440000" }
Response: {
  "observer_id": "observer_73ef03dc0e6110cb",
  "status": "created",
  "message": "Observer registered successfully"
}
```

### Get Events
```json
GET /api/observer/{observer_id}/events?since=0
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
  "last_event_index": 1,
  "has_more": false,
  "observer_id": "observer_73ef03dc0e6110cb"
}
```

### Observer Status
```json
GET /api/observer/{observer_id}/status
Response: {
  "observer_id": "observer_73ef03dc0e6110cb",
  "status": "active",
  "created_at": "2024-01-15T10:29:00Z",
  "last_activity": "2024-01-15T10:30:05Z",
  "total_events": 2,
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "agent_mode": "chat"
}
```

### Active Sessions
```json
GET /api/sessions/active
Response: {
  "active_sessions": [
    {
      "session_id": "550e8400-e29b-41d4-a716-446655440000",
      "observer_id": "observer_73ef03dc0e6110cb",
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
  "X-Session-ID": "550e8400-e29b-41d4-a716-446655440000",
  "X-Observer-ID": "observer_73ef03dc0e6110cb"
}
Request: {
  "query": "What is the weather?",
  "agent_mode": "chat",
  "llm_config": { ... }
}
Response: {
  "status": "started",
  "observer_id": "observer_73ef03dc0e6110cb",
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
          ├── API: POST /api/observer/register
          │   Request: { "session_id": "550e8400-e29b-41d4-a716-446655440000" }
          │   Response: { "observer_id": "observer_73ef03dc0e6110cb", ... }
          └── Store: Create tab with { tabId, observerId, sessionId, metadata: { mode: 'chat' } }
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
│   └── Tab: "Chat 1" (mode: 'chat', observerId: observer_73ef03dc0e6110cb)
└── EventModeProvider (per-tab scope)
    └── ChatAreaWithObserverId
        └── ChatArea (observerId: observer_73ef03dc0e6110cb)
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
│   └── Tab: "Planning" (mode: 'workflow', observerId: observer_84fg04ed1f7221dc)
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
      │     "X-Session-ID": "550e8400-e29b-41d4-a716-446655440000",
      │     "X-Observer-ID": "observer_73ef03dc0e6110cb"
      │   }
      │   Request: {
      │     "query": "What is the weather?",
      │     "agent_mode": "chat",
      │     ...
      │   }
      │   Response: { "status": "started", "observer_id": "observer_73ef03dc0e6110cb" }
      ├── Store: setTabStreaming(tabId, true)
      └── Store: addTabEvents(observerId, [userMessageEvent])
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
      │     "X-Session-ID": "660f9511-f39c-52e5-b827-557766551111",
      │     "X-Observer-ID": "observer_84fg04ed1f7221dc"
      │   }
      │   Request: {
      │     "query": "Create a todo list for building a website",
      │     "agent_mode": "workflow",
      │     "preset_query_id": "preset_123",
      │     ...
      │   }
      │   Response: { "status": "workflow_started", "observer_id": "observer_84fg04ed1f7221dc" }
      ├── Store: setTabStreaming(tabId, true)
      └── Store: addTabEvents(observerId, [userMessageEvent])
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
  ├── Get all tabs with observerId (both chat and workflow tabs)
  ├── For each tab:
  │   └── API: GET /api/observer/{observer_id}/events?since={lastIndex}
  │       Response: {
  │         "events": [
  │           { "type": "agent_message", "data": { "content": "It's sunny!" } },
  │           { "type": "agent_end" }
  │         ],
  │         "last_event_index": 2
  │       }
  │   └── Store: addTabEvents(observerId, newEvents)
  └── Store: setTabLastEventIndex(observerId, 2)
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
      └── Render ChatAreaWithObserverId
          └── ChatArea (full mode with header and input)

ChatAreaWithObserverId / WorkflowLayout (re-renders)
  ├── activeTabId changes → "workflow_1766227475472"
  ├── Get tab: { observerId: "observer_6df3888423c03744", metadata: { mode: 'workflow' }, ... }
  └── ChatArea (observerId: "observer_6df3888423c03744")
      └── tabEvents = useMemo(() => 
            tabEventsStore["observer_6df3888423c03744"] || []
          )
      └── EventDisplay (events: [12 events for observer_6df3888423c03744])
```

**Component Tree:**
```
App
└── ChatTabs
    ├── Tab 1: "Chat 1" (mode: 'chat', inactive)
    └── Tab 2: "Workflow 1" (mode: 'workflow', active) ← Switched to this
└── WorkflowLayout (renders because tab.mode === 'workflow')
    ├── WorkflowCanvas
    └── ChatArea (observerId: observer_6df3888423c03744)
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
      │         "session_id": "...", 
      │         "observer_id": "observer_73ef03dc0e6110cb", 
      │         "agent_mode": "chat",
      │         "status": "running" 
      │       },
      │       { 
      │         "session_id": "...", 
      │         "observer_id": "observer_84fg04ed1f7221dc", 
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
│           ├── API: POST /api/observer/register                │
│           │   Request: { "session_id": "..." }                │
│           │   → Response: { observer_id: "observer_xxx" }     │
│           └── Store: chatTabs[tabId] = {                      │
│                 observerId, sessionId, metadata: { mode: 'chat' }│
│               }                                                │
└─────────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────────┐
│ 2. COMPONENT RENDER (Chat Mode)                                  │
├─────────────────────────────────────────────────────────────────┤
│ selectedModeCategory === 'chat'                                 │
│   └── ChatAreaWithObserverId                                    │
│       ├── Get activeTab from store                              │
│       └── ChatArea (observerId: "observer_xxx")                 │
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
│       │   Headers: X-Session-ID, X-Observer-ID                │
│       │   Request: { "query": "...", "agent_mode": "chat" }    │
│       │   → Response: { status: "started" }                   │
│       ├── Store: addTabEvents(observerId, [userEvent])        │
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
│       │   ├── API: POST /api/observer/register                │
│       │   │   Request: { "session_id": "..." }                │
│       │   │   → Response: { observer_id: "observer_yyy" }     │
│       │   └── Store: chatTabs[tabId] = {                      │
│       │         observerId, sessionId, metadata: { mode: 'workflow', phaseId: 'planning' }│
│       │       }                                                │
│       ├── WorkflowModeHandler.handleChatSubmit() (if planning) │
│       │   └── API: POST /api/workflow/create                  │
│       │       Request: { "preset_query_id": "...", "objective": "..." }│
│       │       → Response: { "workflow": { "id": "workflow_123" } }│
│       ├── API: POST /api/query                                 │
│       │   Headers: X-Session-ID, X-Observer-ID                │
│       │   Request: { "query": "...", "agent_mode": "workflow", │
│       │             "preset_query_id": "...", "step_id": "..." }│
│       │   → Response: { status: "workflow_started" }          │
│       ├── Store: addTabEvents(observerId, [userEvent])        │
│       └── Store: setTabStreaming(tabId, true)                 │
└─────────────────────────────────────────────────────────────────┘
```

### Shared Flow (Both Modes)

```
┌─────────────────────────────────────────────────────────────────┐
│ 4. EVENT POLLING (Every 1s) - Both Modes                       │
├─────────────────────────────────────────────────────────────────┤
│ ChatArea.pollEvents()                                           │
│   ├── Get all tabs with observerId (chat + workflow)          │
│   ├── For each tab:                                            │
│   │   └── API: GET /api/observer/{observer_id}/events?since=0 │
│   │       → Response: {                                        │
│   │             events: [agentEvent1, agentEvent2],           │
│   │             last_event_index: 2                            │
│   │           }                                                │
│   └── Store: addTabEvents(observerId, newEvents)              │
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
│                       └── Render ChatAreaWithObserverId       │
│                           └── ChatArea (observerId: newObserverId)│
│                               └── EventDisplay (events: tabEvents[│
│                                                   newObserverId])│
└─────────────────────────────────────────────────────────────────┘
```

## State Updates Flow

```
API Response → Store Update → Component Re-render

Example: New Event Received
1. API: GET /api/observer/{id}/events
   → Response: { events: [newEvent] }

2. Store: addTabEvents(observerId, [newEvent])
   → tabEvents[observerId] = [...existingEvents, newEvent]

3. ChatArea: tabEvents useMemo recalculates
   → displayEvents = tabEvents[observerId]

4. EventDisplay: Receives new events prop
   → Re-renders with new events
```

