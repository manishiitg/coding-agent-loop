# Store Refactoring Plan

## Current State
The `useChatStore.ts` is ~1550 lines and handles multiple concerns:
- Tab management
- Event storage
- Session status
- Active sessions cache
- Chat history cache
- Toast notifications
- Workflow phase tracking
- Streaming text accumulation

## Proposed Split

### 1. useTabStore (Tab Management)
Extract tab-related state and actions:
- `chatTabs`, `activeTabId`
- `createChatTab`, `switchTab`, `closeTab`, `getTab`, `getActiveTab`
- `getTabsByMode`, `getTabsByPhaseId`
- `setTabStreaming`, `setTabCompleted`, `updateTabSessionId`
- `setTabEventMode`, `getTabConfig`, `setTabConfig`
- `getTabStreamingStatus`, `checkTabCompletion`
- `lastViewedEventCount` tracking

### 2. useEventStore (Event Storage)
Extract event-related state and actions:
- `tabEvents`, `tabEventIndices`, `tabHasMoreOlderEvents`
- `getTabEvents`, `addTabEvent`, `addTabEvents`, `setTabEvents`
- `clearTabEvents`, `cleanupTabEvents`
- `getTabLastEventIndex`, `setTabLastEventIndex`
- `getTabHasMoreOlderEvents`, `setTabHasMoreOlderEvents`
- Event deduplication logic
- Memory cleanup logic (`cleanupOldEvents`, `shouldRetainEvent`)

### 3. useSessionStore (Session Management)
Extract session-related state and actions:
- `tabSessionStatus`
- `fetchTabSessionStatus`, `fetchAllTabSessionStatuses`, `getTabSessionStatus`
- `activeSessionsCache`, `activeSessionsCacheTimestamp`
- `getActiveSessions`, `getActiveSessionIds`
- `startActiveSessionsPolling`, `stopActiveSessionsPolling`

### 4. Keep in useChatStore (Core Chat State)
Retain core chat functionality:
- Streaming state (`isStreaming`, `pollingInterval`)
- User message state (`currentUserMessage`, `showUserMessage`)
- Session state (`sessionId`, `hasActiveChat`)
- UI state (`autoScroll`, `lastScrollTop`)
- Response state (`finalResponse`, `isCompleted`)
- Workflow phase state
- Toast notifications
- Chat history cache

## Implementation Strategy

1. **Phase 1**: Create new stores with extracted functionality
2. **Phase 2**: Add re-exports from `useChatStore` for backward compatibility
3. **Phase 3**: Gradually migrate imports to new stores
4. **Phase 4**: Remove deprecated re-exports

## Dependencies Between Stores

```
useTabStore
    ↓ uses
useEventStore (for event counts in tabs)
    ↓ uses
useSessionStore (for session status indicators)
```

## Risks
- Breaking changes if not done carefully
- Complex inter-store dependencies
- State synchronization issues

## Recommendation
Implement incrementally, starting with `useEventStore` as it has the clearest boundaries.
The configurable logger has already been added to help debug any issues during migration.
