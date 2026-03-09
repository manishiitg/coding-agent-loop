# Multi-Tab Chat Architecture

Multi-tab chat enables parallel, independent agent sessions within a single browser window. This architecture is built on a **Session-First** principle, ensuring that state is never leaked between tabs.

---

## 📋 Core Principles

1.  **Session Isolation**: Every tab is strictly bound to a unique `session_id`.
2.  **Stateless Components**: UI components (ChatArea, Sidebar) derive their entire state from the session configuration in the store.
3.  **Unified API Communication**: All requests to the `agent_go` backend must include the `X-Session-ID` header.

## 🏗️ Technical Implementation

### 1. State Management (`chatStore.ts`)

The global store tracks all active tabs and their configurations:

```typescript
interface ChatTab {
  id: string;          // session_id
  type: 'chat' | 'workflow';
  config: TabConfig;   // MCP servers, skills, browser settings, etc.
  isRestoring: boolean;
}
```

### 2. Race Condition Prevention: `sessionsBeingRestored`

To prevent duplicate tabs from being created during simultaneous auto-restoration and manual session detection, the system uses a global `sessionsBeingRestored` Set.

- **Check**: Before creating a new tab, the system checks if the ID is in this set.
- **Lock**: If not present, it adds the ID and proceeds with tab creation.
- **Release**: Once the tab is fully initialized and events have been polled, the ID is removed from the set.

### 3. API & Event Routing

- **Header Injection**: The `api.ts` service automatically injects the `X-Session-ID` header into every request.
- **Polling Scoping**: The event polling mechanism (`PollingProvider`) scopes its requests using the active tab's session ID. This ensures that the event stream only contains messages relevant to the current view.
- **Session Stopping**: When a tab is closed, the frontend explicitly calls `/api/session/stop` to terminate any background LLM processes for that specific session.

## 🔄 Tab Lifecycle

1.  **Creation**: A tab is created via the "New Chat" button or by clicking a previous session in the history.
2.  **Configuration Injection**: The `TabConfig` (selected servers, browser mode, etc.) is loaded from the database or preset.
3.  **Active Monitoring**: The `PollingProvider` starts a long-polling loop for that session.
4.  **Persistence**: Every message and configuration change is persisted to the backend SQLite database in real-time.
5.  **Termination**: Closing the tab removes it from the UI but keeps the session in history unless explicitly deleted.

## 🚀 Recent Improvements

- **Strict Tab Activation**: Switching tabs now triggers an immediate event poll to ensure the UI is up-to-date.
- **Memory Management**: Ephemeral chat tabs (those not yet saved to history) are automatically cleaned up if the user navigates away without sending a message.
- **Cross-Tab Awareness**: The "Stop" button in the toolbar correctly identifies which session to terminate, even if multiple agents are running in the background.

## ⚠️ Known Limitations

- **Browser Tab Isolation**: Multi-tab chat works *within* a single browser tab. Opening the app in two separate browser windows/tabs will create two independent polling loops, which may cause performance overhead on the backend.
