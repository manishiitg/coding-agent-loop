import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { PollingEvent, ExtendedLLMConfiguration, SessionStatusResponse, ActiveSessionInfo, ChatHistorySummary, DelegationTierConfig, SSEEventMessage, SSEStatusMessage } from '../services/api-types'
import { SSEConnection } from '../services/sse'
import { shouldShowEventByMode } from '../components/events/eventModeUtils'
import type { StoreActions } from './types'
import type { FileContextItem } from './types'
import type { WorkflowPhase } from '../constants/workflow'
import { useAppStore } from './useAppStore'
import { agentApi } from '../services/api'
import { useMCPStore } from './useMCPStore'
import { useLLMStore } from './useLLMStore'
import { MAX_EVENTS_TO_PROCESS, CLEANUP_THRESHOLD } from '../constants/events'
import { logger } from '../utils/logger'

// Active sessions cache TTL (30 seconds - shorter than polling interval to allow force refresh)
const ACTIVE_SESSIONS_CACHE_TTL = 30000

// Streaming inactivity auto-clear timers (per sessionId)
// When no new chunk arrives for 3s, streaming text is auto-cleared
const _streamingInactivityTimers: Record<string, ReturnType<typeof setTimeout>> = {}
const STREAMING_INACTIVITY_MS = 60000

// Per-mode event counts type — kept for backwards compat with persisted state
export type PerModeEventCounts = { micro: number }

// Helper to compute visible event counts (full recomputation — use sparingly)
const computePerModeCounts = (events: PollingEvent[]): PerModeEventCounts => {
  return {
    micro: events.filter(e => e.type && shouldShowEventByMode(e.type)).length,
  }
}

// Incremental helper: count only new events and add to existing counts
const incrementPerModeCounts = (
  existing: PerModeEventCounts,
  newEvents: PollingEvent[]
): PerModeEventCounts => {
  let microDelta = 0
  for (const e of newEvents) {
    if (e.type && shouldShowEventByMode(e.type)) microDelta++
  }
  return {
    micro: existing.micro + microDelta,
  }
}
// Chat history cache TTL (5 minutes - chat history doesn't change as frequently)
const CHAT_HISTORY_CACHE_TTL = 300000

// Event memory management constants - use shared constants
const MAX_EVENTS = MAX_EVENTS_TO_PROCESS

// Persistent event ID index — avoids O(n) Set rebuild on every addTabEvents call.
// Lives outside zustand state so mutating it doesn't trigger re-renders.
const tabEventIdSets = new Map<string, Set<string>>()

// --- Micro-batching for addTabEvents ---
// Instead of updating zustand state on every SSE event (10-50/sec),
// buffer events and flush at most every BATCH_INTERVAL_MS.
// This reduces render cascades (ChatArea → EventHierarchy) by ~10-50x.
const BATCH_INTERVAL_MS = 200 // 200ms batching — balances render frequency vs responsiveness
const _eventBatchBuffers = new Map<string, PollingEvent[]>()
const _eventBatchTimers = new Map<string, ReturnType<typeof setTimeout>>()

function _flushEventBatch(sessionId: string) {
  _eventBatchTimers.delete(sessionId)
  const buffer = _eventBatchBuffers.get(sessionId)
  if (!buffer || buffer.length === 0) return
  _eventBatchBuffers.delete(sessionId)
  useChatStore.getState()._addTabEventsImmediate(sessionId, buffer)
}

function addTabEventsBatched(sessionId: string, events: PollingEvent[]) {
  // Check if any event is "important" (completion, error, human feedback) — flush immediately
  const hasImportant = events.some(e => {
    const t = e.type
    return t === 'unified_completion' || t === 'conversation_end' || t === 'workflow_end' ||
      t === 'agent_error' || t === 'conversation_error' || t === 'orchestrator_error' ||
      t === 'request_human_feedback' || t === 'blocking_human_feedback' ||
      t === 'orchestrator_end' || t === 'agent_end' ||
      t === 'user_message' || t === 'conversation_resumed'
  })

  const existing = _eventBatchBuffers.get(sessionId)
  if (existing) {
    existing.push(...events)
  } else {
    _eventBatchBuffers.set(sessionId, [...events])
  }

  if (hasImportant) {
    // Flush immediately for important events
    const timer = _eventBatchTimers.get(sessionId)
    if (timer) clearTimeout(timer)
    _flushEventBatch(sessionId)
  } else if (!_eventBatchTimers.has(sessionId)) {
    // Schedule flush
    _eventBatchTimers.set(sessionId, setTimeout(() => _flushEventBatch(sessionId), BATCH_INTERVAL_MS))
  }
}

// Helper function to identify important events that should always be retained
// These events provide critical context and should not be removed during cleanup
const shouldRetainEvent = (event: PollingEvent): boolean => {
  if (!event.type) return false

  const importantTypes = [
    // Error events - always keep for debugging
    'agent_error',
    'conversation_error',
    'orchestrator_error',
    // Completion/end events - always keep
    'unified_completion',
    'conversation_end',
    'workflow_end',
    'orchestrator_end',
    'agent_end',
    // Start events - keep for context
    'workflow_start',
    'conversation_start',
    // Human feedback events - critical for workflow
    'request_human_feedback',
    'blocking_human_feedback',
    // User input - keep for conversation context
    'user_message',
    // Tool events - keep for understanding what happened
    'tool_call',
    'tool_result',
    'tool_output',
    // LLM output - keep final generation results
    'llm_generation_end',
    // Workflow execution events
    'step_progress_updated',
    'phase_started',
    'phase_completed',
    // Delegation structural events - must survive for sub-agent cards
    'delegation_start',
    'delegation_end',
    // Orchestrator agent boundaries - must survive for step collapse/expand
    'orchestrator_agent_start',
    'orchestrator_agent_end'
  ]
  return importantTypes.includes(event.type)
}

// Tab session status type
export interface TabSessionStatus {
  status: string | null
  agentMode: string | null
  lastActivity: string | null
}

// Tab-specific configuration (all settings that should be per-tab)
export interface ChatTabConfig {
  inputText: string  // Chat input text
  useCodeExecutionMode: boolean  // Code execution mode toggle
  useToolSearchMode: boolean  // Tool search mode toggle (discover tools on-demand)
  selectedServers: string[]  // Selected MCP servers
  selectedSkills: string[]  // Selected skills to include in chat
  selectedSecrets: string[]  // Selected secret IDs to inject into chat
  selectedSubAgents: string[]  // Selected sub-agent templates for delegation
  llmConfig: ExtendedLLMConfiguration  // LLM configuration (provider, model, etc.)
  fileContext: FileContextItem[]  // Files/folders in context
  enableContextSummarization?: boolean  // Context summarization setting
  enableWorkspaceAccess?: boolean  // Enable/disable workspace file access tools
  browserMode?: 'none' | 'headless' | 'cdp' | 'playwright' | 'stealth'  // Browser access mode (default: 'none')
  enableBrowserAccess?: boolean  // Enable/disable browser automation tool (auto-enables workspace when true)
  enableGWSAccess?: boolean  // Enable/disable Google Workspace CLI access
  useCdp?: boolean  // Whether CDP mode is enabled (connect to local Chrome)
  cdpPort?: number  // CDP port (default 9222)
  camofoxHeaded?: boolean  // Camofox headed mode — visible browser window (default: true)
  delegationTierConfig?: DelegationTierConfig  // Per-tab delegation tier config (multi-agent mode)
  workflowContext: Array<{
    presetId: string
    label: string
    workspacePath: string
  }>  // Workflow presets selected via # in chat input
  queuedMessages: string[]  // Queue of messages to send one by one when chat completes
  isQueueProcessing?: boolean  // Lock to prevent multiple ChatArea instances from double-processing the queue
  autoRun?: boolean  // Automatically run the chat when tab is loaded
  planPhaseOverride?: 'planning' | 'execution' | null  // User-selected plan phase override for multi-agent mode
  defaultReasoningLevel?: 'high' | 'medium' | 'low' | null  // Preferred reasoning level for delegated tasks in multi-agent mode
  enableImageGeneration?: boolean  // Enable/disable image generation virtual tool
  selectedPlanFolder?: string  // Existing plan folder to reuse (multi-agent mode; cleared after submit)
}

// Generalized ChatTab interface (works for multi-agent and workflow modes)
export interface ChatTab {
  tabId: string  // Unique ID: `chat_${timestamp}` or `phase_${phaseId}_${timestamp}`
  name: string  // Display name (e.g., "Chat 1", "Planning", "Execution")
  sessionId: string | null  // Chat session ID if exists
  isStreaming: boolean  // Whether this tab's execution is currently running
  isCompleted: boolean  // Whether this tab's execution has completed
  hasRunningBgAgents: boolean  // Whether background agents are still running for this session
  isSyntheticTurn: boolean  // Whether current streaming turn is an auto-notification (input remains locked as normal)
  hideToolCalls: boolean  // Whether to hide tool_call_start/end events in this tab
  // View mode controls how much detail is rendered in the event list.
  // 'detailed' — full event stream (tool calls, LLM events, delegation internals, etc.)
  // 'summary'  — lightweight view showing only high-level agent activity:
  //   agent start/end, step progress, todo items, errors, user messages, delegation cards.
  //   Drops tool calls, LLM generation, streaming, MCP internals — reducing event count
  //   from ~500 to ~20-30 for a typical workflow. Ideal for background workflows where
  //   the user only cares about progress, not execution details.
  viewMode: 'detailed' | 'summary'
  config: ChatTabConfig  // Tab-specific configuration
  createdAt: number  // Timestamp for ordering
  lastViewedEventCount: number  // @deprecated - use lastViewedEventCounts instead
  lastViewedEventCounts: PerModeEventCounts  // Per-mode event counts for accurate badge calculation
  // Mode-specific metadata
  metadata?: {
    phaseId?: string  // For workflow mode: phase ID
    phaseName?: string  // For workflow mode: phase name
    mode?: 'workflow' | 'multi-agent' | 'organization'  // Which mode this tab belongs to
    presetQueryId?: string  // For workflow mode: preset query ID (workflow identifier)
    isOrganizationAssistant?: boolean // True when tab is reserved for Organization panel
    isRestored?: boolean  // True when restored from history (sidebar, resume dialog, page refresh)
    isRestoringSession?: boolean  // True while session events are being loaded from backend
    isViewOnly?: boolean // True when tab is in view-only mode (e.g. shared session or bot connector)
  }
}

// Helper function to get default tab config from current global state
// Uses mode-specific configs for LLM and server selections
const getDefaultTabConfig = (mode: 'workflow' | 'multi-agent' | 'organization' = 'multi-agent'): ChatTabConfig => {
  const mcpStore = useMCPStore?.getState?.()
  const llmStore = useLLMStore?.getState?.()
  const appStore = useAppStore?.getState?.()

  // Get mode-specific server selection (multi-agent uses chat settings)
  const isWorkflowMode = mode === 'workflow'
  const isMultiAgentMode = mode === 'multi-agent'

  const selectedServers = isWorkflowMode
    ? (mcpStore?.workflowSelectedServers || mcpStore?.selectedServers || [])
    : (mcpStore?.chatSelectedServers || mcpStore?.selectedServers || [])

  // Get mode-specific LLM config (multi-agent uses chat settings)
  const llmConfig = isWorkflowMode
    ? (llmStore?.workflowPrimaryConfig || llmStore?.primaryConfig)
    : (llmStore?.chatPrimaryConfig || llmStore?.primaryConfig)

  return {
    inputText: '',
    // Default to false (simple mode) - user can toggle to true (code exec mode) via ChatInput
    useCodeExecutionMode: false,
    // Default to false - user can toggle to true (tool search mode) via ChatInput
    useToolSearchMode: false,
    selectedServers,
    selectedSecrets: [],
    workflowContext: [],
    llmConfig: llmConfig || {
      provider: 'openrouter',
      model_id: '',
      fallback_models: [],
      cross_provider_fallback: undefined
    },
    // CRITICAL: Don't copy global chatFileContext - chat tabs should have independent file context
    // Workflow mode uses global chatFileContext, but chat mode uses tab-specific fileContext
    fileContext: [],
    enableContextSummarization: false,
    enableWorkspaceAccess: true,
    browserMode: appStore?.lastBrowserMode ?? 'none',
    enableBrowserAccess: (appStore?.lastBrowserMode === 'headless' || appStore?.lastBrowserMode === 'cdp') ?? false,
    enableImageGeneration: appStore?.lastEnableImageGeneration ?? false,
    enableGWSAccess: appStore?.lastGWSAccess ?? false,
    selectedSkills: appStore?.lastSelectedSkills ?? [],
    selectedSubAgents: appStore?.lastSelectedSubAgents ?? [],
    delegationTierConfig: isMultiAgentMode ? (llmStore?.delegationTierConfig ?? undefined) : undefined,
    queuedMessages: [],
    autoRun: false,
    planPhaseOverride: isMultiAgentMode ? (appStore?.lastMultiAgentPlanPhase ?? 'planning') : undefined,
  }
}

// Helper function to cleanup old events while retaining important ones
const cleanupOldEvents = (events: PollingEvent[]): PollingEvent[] => {
  if (events.length <= MAX_EVENTS) return events
  
  // Separate important and regular events
  const important = events.filter(shouldRetainEvent)
  const regular = events.filter(e => !shouldRetainEvent(e))
  
  // Trim important events if they exceed MAX_EVENTS
  let trimmedImportant = important
  if (important.length > MAX_EVENTS) {
    // Keep only the newest MAX_EVENTS important events
    trimmedImportant = important
      .sort((a, b) => {
        const aTime = a.timestamp ? new Date(a.timestamp).getTime() : 0
        const bTime = b.timestamp ? new Date(b.timestamp).getTime() : 0
        return bTime - aTime // Sort newest first
      })
      .slice(0, MAX_EVENTS)
  }
  
  // Calculate budget for regular events (clamped to 0)
  const budget = Math.max(0, MAX_EVENTS - trimmedImportant.length)
  
  // Keep latest regular events within budget
  const keepRegular = budget > 0 ? regular.slice(-budget) : []
  
  // Combine and sort by timestamp
  return [...trimmedImportant, ...keepRegular].sort((a, b) => {
    const aTime = a.timestamp ? new Date(a.timestamp).getTime() : 0
    const bTime = b.timestamp ? new Date(b.timestamp).getTime() : 0
    return aTime - bTime
  })
}

interface ChatState extends StoreActions {
  // Chat streaming state
  isStreaming: boolean
  lastEventIndex: number
  pollingInterval: NodeJS.Timeout | null
  
  // Event tracking removed - using tabEvents only (keyed by sessionId)
  // Per-tab event storage (keyed by session ID)
  tabEvents: Record<string, PollingEvent[]>  // sessionId -> events
  tabEventIndices: Record<string, number>  // sessionId -> lastEventIndex
  tabHasMoreOlderEvents: Record<string, boolean>  // sessionId -> hasMoreOlderEvents (from initial fetch)
  
  // User message state
  currentUserMessage: string
  showUserMessage: boolean
  
  // Session state
  sessionId: string | null
  hasActiveChat: boolean
  
  // Chat UI state
  autoScroll: boolean
  
  // Response state
  finalResponse: string
  isCompleted: boolean
  
  // Loading states
  isLoadingHistory: boolean
  isApprovingWorkflow: boolean
  
  // Session management
  sessionState: 'loading' | 'active' | 'completed' | 'not_found' | 'error'
  isCheckingActiveSessions: boolean
  
  // Workflow execution state (not preset management)
  currentWorkflowPhase: WorkflowPhase
  currentWorkflowQueryId: string | null
  
  // Toast notifications
  toasts: Array<{ id: string; message: string; type: 'success' | 'info' | 'error' | 'warning' }>
  
  // Multi-tab chat state (generalized for both chat and workflow modes)
  chatTabs: Record<string, ChatTab>  // tabId -> tab
  activeTabId: string | null  // Currently selected tab
  
  // Tab session status (fetched from backend)
  tabSessionStatus: Record<string, TabSessionStatus>  // tabId -> status
  
  // Active sessions cache (shared across all components)
  activeSessionsCache: ActiveSessionInfo[]
  activeSessionsCacheTimestamp: number | null
  activeSessionsPollingInterval: NodeJS.Timeout | null
  
  // Chat history cache (shared across all components, persists across sidebar mount/unmount)
  chatHistoryCache: ChatHistorySummary[]
  chatHistoryCacheTimestamp: number | null
  chatHistoryLastLoadedMode: string | null  // Track which mode category was last loaded
  chatHistoryTotalCount: number | null  // Total count of sessions available
  chatHistoryLoadedCount: number  // Number of sessions currently loaded

  // Streaming text accumulation (per session)
  // Only tracks parent agent streaming - sub-agent streaming routed to delegationStreamingText
  streamingText: Record<string, string>  // sessionId → accumulated streaming text (response content only)
  streamingStatus: Record<string, string>  // sessionId → latest status/heartbeat message (⏳/⚠️ messages)
  lastStreamingChunkIndex: Record<string, number>  // sessionId → last processed chunk_index (dedup guard)
  completedStreamingText: Record<string, string>  // sessionId → preserved streaming text after generation completes

  // Sub-agent streaming text accumulation (per delegation)
  delegationStreamingText: Record<string, string>  // delegationId → accumulated streaming text
  lastDelegationChunkIndex: Record<string, number>  // delegationId → last processed chunk_index (dedup guard)

  // Actions
  setIsStreaming: (streaming: boolean) => void
  // Computed: Derive isStreaming from polling status
  // Use this selector: useChatStore(state => state.getIsStreaming())
  getIsStreaming: () => boolean
  setLastEventIndex: (index: number) => void
  setPollingInterval: (interval: NodeJS.Timeout | null) => void
  
  // Polling management actions
  startPolling: (onPoll: () => Promise<void>) => void
  stopPolling: () => void
  updatePollingState: () => void  // Auto-start/stop based on active sessions
  
  // Event actions removed - using tabEvents only
      // Per-tab event actions (now keyed by sessionId instead of observerId)
      getTabEvents: (sessionId: string) => PollingEvent[]
      addTabEvent: (sessionId: string, event: PollingEvent) => void
      addTabEvents: (sessionId: string, events: PollingEvent[]) => void
      _addTabEventsImmediate: (sessionId: string, events: PollingEvent[]) => void
      setTabEvents: (sessionId: string, events: PollingEvent[]) => void
      clearTabEvents: (sessionId: string) => void
      cleanupTabEvents: (sessionId: string, keepCount: number) => void
      cleanupOrphanedTabEvents: () => void
      getTabLastEventIndex: (sessionId: string) => number
      setTabLastEventIndex: (sessionId: string, index: number) => void
      getTabHasMoreOlderEvents: (sessionId: string) => boolean
      setTabHasMoreOlderEvents: (sessionId: string, hasMore: boolean) => void
  
  // User message actions
  setCurrentUserMessage: (message: string) => void
  setShowUserMessage: (show: boolean) => void
  
  // Session actions
  setSessionId: (id: string | null) => void
  setHasActiveChat: (active: boolean) => void
  
  // UI actions
  setAutoScroll: (autoScroll: boolean) => void
  
  // Response actions
  setFinalResponse: (response: string) => void
  setIsCompleted: (completed: boolean) => void
  
  // Loading actions
  setIsLoadingHistory: (loading: boolean) => void
  setIsApprovingWorkflow: (loading: boolean) => void
  
  // Session management actions
  setSessionState: (state: 'loading' | 'active' | 'completed' | 'not_found' | 'error') => void
  setIsCheckingActiveSessions: (checking: boolean) => void
  
  // Workflow execution actions
  setCurrentWorkflowPhase: (phase: WorkflowPhase) => void
  setCurrentWorkflowQueryId: (id: string | null) => void
  
  // Toast actions
  addToast: (message: string, type: 'success' | 'info' | 'error' | 'warning') => void
  removeToast: (id: string) => void
  clearToasts: () => void
  
  // Tab management actions
  createChatTab: (name: string, metadata?: ChatTab['metadata'], existingObserverId?: string) => Promise<string>  // Returns tabId
  switchTab: (tabId: string) => void
  closeTab: (tabId: string, stopSession?: boolean, keepEvents?: boolean) => Promise<void>
  getTab: (tabId: string) => ChatTab | undefined
  getActiveTab: () => ChatTab | undefined
  getTabsByMode: (mode: 'multi-agent' | 'workflow' | 'organization') => ChatTab[]
  getTabsByPhaseId: (phaseId: string, presetQueryId?: string) => ChatTab[]  // Find workflow tabs by phaseId (optionally scoped to preset)
  setTabStreaming: (tabId: string, isStreaming: boolean) => void
  setTabCompleted: (tabId: string, isCompleted: boolean) => void
  setTabHasRunningBgAgents: (tabId: string, hasRunningBgAgents: boolean) => void
  setTabSyntheticTurn: (tabId: string, isSyntheticTurn: boolean) => void
  updateTabSessionId: (tabId: string, sessionId: string) => void
  setTabHideToolCalls: (tabId: string, hideToolCalls: boolean) => void
  setTabViewMode: (tabId: string, viewMode: 'detailed' | 'summary') => void
  getTabConfig: (tabId: string) => ChatTabConfig | undefined
  setTabConfig: (tabId: string, configUpdate: Partial<ChatTabConfig>) => void
  setTabMetadata: (tabId: string, metadataUpdate: Partial<NonNullable<ChatTab['metadata']>>) => void
  getTabStreamingStatus: (tabId: string) => boolean
  checkTabCompletion: (tabId: string, events: Array<{ type: string }>) => boolean
  
  // Tab session status actions
  fetchTabSessionStatus: (tabId: string) => Promise<void>
  fetchAllTabSessionStatuses: (tabIds: string[]) => Promise<void>
  getTabSessionStatus: (tabId: string) => TabSessionStatus | undefined
  
  // Active sessions cache actions
  getActiveSessions: (forceRefresh?: boolean) => Promise<ActiveSessionInfo[]>
  getActiveSessionIds: () => Set<string>
  startActiveSessionsPolling: () => void
  stopActiveSessionsPolling: () => void
  
  // Chat history cache actions
  getChatHistory: (modeCategory: string, forceRefresh?: boolean) => Promise<ChatHistorySummary[]>
  loadMoreChatHistory: (modeCategory: string) => Promise<ChatHistorySummary[]>
  setChatHistory: (sessions: ChatHistorySummary[], modeCategory: string) => void
  getChatHistoryHasMore: () => boolean
  
  // Streaming text actions
  appendStreamingChunk: (sessionId: string, chunkIndex: number, chunk: string) => void
  clearStreamingText: (sessionId: string) => void
  clearStreamingStatus: (sessionId: string) => void

  // Delegation streaming text actions
  appendDelegationStreamingChunk: (delegationId: string, chunkIndex: number, chunk: string) => void
  clearDelegationStreamingText: (delegationId: string) => void

  // SSE connection management
  sseConnections: Record<string, SSEConnection>  // sessionId -> SSEConnection
  connectSSE: (sessionId: string, onMessage: (msg: SSEEventMessage) => void, onStatus: (msg: SSEStatusMessage) => void) => void
  disconnectSSE: (sessionId: string) => void
  disconnectAllSSE: () => void

  // Helper methods
  resetTabChat: (tabId: string) => void
  resetChatState: () => void
  isAtBottom: (element: HTMLDivElement) => boolean
}

export const useChatStore = create<ChatState>()(
    persist(
      (set, get) => ({
      // Initial state
      isStreaming: false,
      lastEventIndex: -1,
      pollingInterval: null,
      tabEvents: {},
      tabEventIndices: {},
      tabHasMoreOlderEvents: {},
      currentUserMessage: '',
      showUserMessage: true,
      sessionId: null,
      hasActiveChat: false,
      autoScroll: true,
      finalResponse: '',
      isCompleted: false,
      isLoadingHistory: false,
      isApprovingWorkflow: false,
      sessionState: 'loading',
      isCheckingActiveSessions: false,
      currentWorkflowPhase: 'planning' as WorkflowPhase,
      currentWorkflowQueryId: null,
      toasts: [],
      chatTabs: {},
      activeTabId: null,
      tabSessionStatus: {},
      
      // Active sessions cache (shared across all components)
      activeSessionsCache: [],
      activeSessionsCacheTimestamp: null,
      activeSessionsPollingInterval: null,
      
      // Chat history cache (shared across all components)
      chatHistoryCache: [],
      chatHistoryCacheTimestamp: null,
      chatHistoryLastLoadedMode: null,
      chatHistoryTotalCount: null,
      chatHistoryLoadedCount: 0,

      // Streaming text accumulation (per session)
      streamingText: {},
      streamingStatus: {},
      lastStreamingChunkIndex: {},
      completedStreamingText: {},

      // Sub-agent streaming text accumulation (per delegation)
      delegationStreamingText: {},
      lastDelegationChunkIndex: {},

      // SSE connections (not persisted)
      sseConnections: {},

      // Actions
      setIsStreaming: (streaming) => {
        set({ isStreaming: streaming })
      },
      
      // Computed getter for isStreaming (derived from polling status)
      // This ensures isStreaming reflects actual polling state
      getIsStreaming: () => {
        const state = get()
        // Derive from polling status: if polling is active and not completed, we're streaming
        // Exception: if manually set to false (e.g., human feedback pause), respect that
        return state.pollingInterval !== null && !state.isCompleted && state.isStreaming !== false
      },

      setLastEventIndex: (index) => {
        set({ lastEventIndex: index })
      },

      setPollingInterval: (interval) => {
        set({ pollingInterval: interval })
        // Auto-update isStreaming based on polling status
        const state = get()
        const derivedStreaming = interval !== null && !state.isCompleted
        if (state.isStreaming !== derivedStreaming && state.isStreaming !== false) {
          // Only auto-update if not manually paused (isStreaming === false means paused)
          set({ isStreaming: derivedStreaming })
        }
      },
      
      // Start polling with a callback function
      startPolling: (onPoll) => {
        const state = get()
        // Don't start if already polling
        if (state.pollingInterval) {
          logger.debug('ChatStore', 'Polling already active, skipping start')
          return
        }
        
        logger.debug('ChatStore', 'Starting polling interval')
        const interval = setInterval(() => {
          onPoll().catch(error => {
            logger.error('ChatStore', 'Error in polling callback:', error)
          })
        }, 500)  // 500ms for streaming responsiveness
        
        set({ pollingInterval: interval })
      },
      
      // Stop polling
      stopPolling: () => {
        const state = get()
        if (state.pollingInterval) {
          logger.debug('ChatStore', 'Stopping polling interval')
          clearInterval(state.pollingInterval)
          set({ pollingInterval: null })
        }
      },
      
      // Update polling state based on active (streaming) sessions
      // This should be called when tab streaming status changes
      updatePollingState: () => {
        const state = get()
        const activeTabs = Object.values(state.chatTabs).filter(tab => tab.isStreaming)
        
        // If there are active sessions and no polling, start it
        if (activeTabs.length > 0 && !state.pollingInterval) {
          logger.debug('ChatStore', `Found ${activeTabs.length} active session(s), but polling not started yet. Call startPolling() with your poll callback.`)
          // Note: We can't start polling here because we need the poll callback
          // The component should call startPolling with the callback
        }
        // If there are no active sessions but polling is running, stop it
        else if (activeTabs.length === 0 && state.pollingInterval) {
          logger.debug('ChatStore', 'No active sessions, stopping polling')
          state.stopPolling()
        }
      },
      

      // Event actions
      // Deprecated: setTotalEvents and setLastEventCount removed - use tabEvents instead

      // Deprecated: setEvents, addEvent, clearEvents removed - use tabEvents instead
      
      // Per-tab event actions (now keyed by sessionId)
      getTabEvents: (sessionId: string) => {
        const state = get()
        return state.tabEvents[sessionId] || []
      },
      
      addTabEvent: (sessionId: string, event: PollingEvent) => {
        set((state) => {
          const currentEvents = state.tabEvents[sessionId] || []
          const newEvents = [...currentEvents, event]
          
          // Trigger cleanup if threshold exceeded
          let finalEvents = newEvents
          if (newEvents.length >= CLEANUP_THRESHOLD) {
            logger.debug('Memory', `Cleaning up events for session ${sessionId}: ${newEvents.length} -> ${MAX_EVENTS}`)
            finalEvents = cleanupOldEvents(newEvents)
          }
          
          return {
            tabEvents: {
              ...state.tabEvents,
              [sessionId]: finalEvents
            },
            // Deprecated: totalEvents removed
          }
        })
      },
      
      // Public API: micro-batched — buffers events and flushes every 100ms
      // to reduce render cascades from 10-50/sec to ~10/sec
      addTabEvents: (sessionId: string, events: PollingEvent[]) => {
        addTabEventsBatched(sessionId, events)
      },

      // Internal: immediate store update (called by the batch flush)
      _addTabEventsImmediate: (sessionId: string, events: PollingEvent[]) => {
        set((state) => {
          const currentEvents = state.tabEvents[sessionId] || []

          // Use persistent event ID index (O(1) lookup instead of rebuilding Set each call)
          let idSet = tabEventIdSets.get(sessionId)
          if (!idSet) {
            // First call for this session — build from existing events
            idSet = new Set(currentEvents.map(e => e.id).filter(Boolean) as string[])
            tabEventIdSets.set(sessionId, idSet)
          }

          // Filter out events that already exist
          const uniqueNewEvents = events.filter(event => {
            if (!event.id) {
              logger.warn('EventStore', 'Event without ID detected:', event)
              return true
            }
            if (idSet!.has(event.id)) {
              return false
            }
            idSet!.add(event.id)
            return true
          })

          // PERF: Skip state update entirely when no new events — avoids creating a new
          // array reference which would cascade re-renders through ChatArea → EventHierarchy.
          if (uniqueNewEvents.length === 0) {
            return state
          }

          const newEvents = [...currentEvents, ...uniqueNewEvents]

          // Trigger cleanup if threshold exceeded
          let finalEvents = newEvents
          let didCleanup = false
          if (newEvents.length >= CLEANUP_THRESHOLD) {
            logger.debug('Memory', `Cleaning up events for session ${sessionId}: ${newEvents.length} -> ${MAX_EVENTS}`)
            finalEvents = cleanupOldEvents(newEvents)
            didCleanup = true
            // Rebuild ID index after cleanup discards events
            tabEventIdSets.set(sessionId, new Set(finalEvents.map(e => e.id).filter(Boolean) as string[]))
          }

          // Update lastViewedEventCounts for the ACTIVE tab if it owns this session.
          // Events arriving on the active tab are visible to the user in real-time,
          // so they shouldn't show as "new" in the badge.
          const updates: Partial<ChatState> = {
            tabEvents: {
              ...state.tabEvents,
              [sessionId]: finalEvents
            },
          }

          const activeTab = state.activeTabId ? state.chatTabs[state.activeTabId] : null
          if (activeTab && activeTab.sessionId === sessionId) {
            // Incremental count: only scan new events instead of the entire array.
            // Fall back to full recomputation when cleanup discards old events (counts become stale).
            const newCounts = didCleanup
              ? computePerModeCounts(finalEvents)
              : incrementPerModeCounts(activeTab.lastViewedEventCounts, uniqueNewEvents)
            updates.chatTabs = {
              ...state.chatTabs,
              [activeTab.tabId]: {
                ...activeTab,
                lastViewedEventCount: finalEvents.length,
                lastViewedEventCounts: newCounts
              }
            }
          }

          return updates
        })
      },

      setTabEvents: (sessionId: string, events: PollingEvent[]) => {
        // Rebuild the persistent ID index for this session
        tabEventIdSets.set(sessionId, new Set(events.map(e => e.id).filter(Boolean) as string[]))

        set((state) => {
          // Trigger cleanup if threshold exceeded
          let finalEvents = events
          if (events.length >= CLEANUP_THRESHOLD) {
            logger.debug('Memory', `Cleaning up events for session ${sessionId}: ${events.length} -> ${MAX_EVENTS}`)
            finalEvents = cleanupOldEvents(events)
          }

          // Also update lastViewedEventCounts for the tab owning this session
          // so restored/hydrated events don't show as "new" in the badge
          const updatedTabs = { ...state.chatTabs }
          for (const [tabId, tab] of Object.entries(updatedTabs)) {
            if (tab.sessionId === sessionId) {
              updatedTabs[tabId] = {
                ...tab,
                lastViewedEventCount: finalEvents.length,
                lastViewedEventCounts: computePerModeCounts(finalEvents)
              }
            }
          }

          return {
            tabEvents: {
              ...state.tabEvents,
              [sessionId]: finalEvents
            },
            chatTabs: updatedTabs
          }
        })
      },
      
      clearTabEvents: (sessionId: string) => {
        tabEventIdSets.delete(sessionId)
        set((state) => {
          const newTabEvents = { ...state.tabEvents }
          delete newTabEvents[sessionId]
          const newTabEventIndices = { ...state.tabEventIndices }
          delete newTabEventIndices[sessionId]
          const newTabHasMoreOlderEvents = { ...state.tabHasMoreOlderEvents }
          delete newTabHasMoreOlderEvents[sessionId]

          return {
            tabEvents: newTabEvents,
            tabEventIndices: newTabEventIndices,
            tabHasMoreOlderEvents: newTabHasMoreOlderEvents
          }
        })
      },

      cleanupTabEvents: (sessionId: string, keepCount: number) => {
        set((state) => {
          const events = state.tabEvents[sessionId]

          if (!events || events.length <= keepCount) {
            return state // No cleanup needed
          }

          // Keep only recent events and important events using the utility
          const cleaned = cleanupOldEvents(events)

          logger.debug('ChatStore', `Cleaned up events for ${sessionId}: ${events.length} -> ${cleaned.length}`)

          return {
            tabEvents: {
              ...state.tabEvents,
              [sessionId]: cleaned
            }
          }
        })
      },

      // Remove tabEvents/tabEventIndices entries for session IDs that no tab references
      // This prevents memory leaks when tabs are reused with new session IDs
      cleanupOrphanedTabEvents: () => {
        const state = get()
        const activeSessionIds = new Set(
          Object.values(state.chatTabs)
            .map(tab => tab.sessionId)
            .filter(Boolean)
        )

        let orphanCount = 0
        const newTabEvents = { ...state.tabEvents }
        const newTabEventIndices = { ...state.tabEventIndices }
        const newTabHasMore = { ...state.tabHasMoreOlderEvents }

        for (const sessionId of Object.keys(state.tabEvents)) {
          if (!activeSessionIds.has(sessionId)) {
            delete newTabEvents[sessionId]
            delete newTabEventIndices[sessionId]
            delete newTabHasMore[sessionId]
            tabEventIdSets.delete(sessionId)
            orphanCount++
          }
        }

        if (orphanCount > 0) {
          logger.debug('ChatStore', `Cleaned up ${orphanCount} orphaned tabEvent entries`)
          set({
            tabEvents: newTabEvents,
            tabEventIndices: newTabEventIndices,
            tabHasMoreOlderEvents: newTabHasMore
          })
        }
      },

      getTabLastEventIndex: (sessionId: string) => {
        const state = get()
        return state.tabEventIndices[sessionId] ?? -1
      },
      
      setTabLastEventIndex: (sessionId: string, index: number) => {
        // PERF: skip state update when index hasn't changed (avoids unnecessary re-renders)
        if (get().tabEventIndices[sessionId] === index) return
        set((state) => ({
          tabEventIndices: {
            ...state.tabEventIndices,
            [sessionId]: index
          }
        }))
      },
      
      getTabHasMoreOlderEvents: (sessionId: string) => {
        const state = get()
        return state.tabHasMoreOlderEvents[sessionId] ?? false
      },
      
      setTabHasMoreOlderEvents: (sessionId: string, hasMore: boolean) => {
        if (get().tabHasMoreOlderEvents[sessionId] === hasMore) return
        set((state) => ({
          tabHasMoreOlderEvents: {
            ...state.tabHasMoreOlderEvents,
            [sessionId]: hasMore
          }
        }))
      },

      // User message actions
      setCurrentUserMessage: (message) => {
        set({ currentUserMessage: message })
      },

      setShowUserMessage: (show) => {
        set({ showUserMessage: show })
      },

      // Session actions
      setSessionId: (id) => {
        set({ sessionId: id })
      },

      setHasActiveChat: (active) => {
        set({ hasActiveChat: active })
      },

      // UI actions
      setAutoScroll: (autoScroll) => {
        set({ autoScroll })
      },

      // Response actions
      setFinalResponse: (response) => {
        set({ finalResponse: response })
      },

      setIsCompleted: (completed) => {
        set({ isCompleted: completed })
        // Auto-update isStreaming: if completed, stop streaming
        if (completed) {
          const state = get()
          if (state.isStreaming) {
            set({ isStreaming: false })
          }
        }
      },

      // Loading actions
      setIsLoadingHistory: (loading) => {
        set({ isLoadingHistory: loading })
      },

      setIsApprovingWorkflow: (loading) => {
        set({ isApprovingWorkflow: loading })
      },

      // Session management actions
      setSessionState: (state) => {
        set({ sessionState: state })
      },

      setIsCheckingActiveSessions: (checking) => {
        set({ isCheckingActiveSessions: checking })
      },

      // Workflow execution actions
      setCurrentWorkflowPhase: (phase) => {
        set({ currentWorkflowPhase: phase })
      },

      setCurrentWorkflowQueryId: (id) => {
        set({ currentWorkflowQueryId: id })
      },

      // Toast actions
      addToast: (message, type) => {
        const id = Date.now().toString()
        set((state) => ({
          toasts: [...state.toasts, { id, message, type }]
        }))
      },

      removeToast: (id) => {
        set((state) => ({
          toasts: state.toasts.filter(toast => toast.id !== id)
        }))
      },

      clearToasts: () => {
        set({ toasts: [] })
      },

      // Streaming text actions
      // Only parent agent streaming is processed - sub-agent streaming is filtered out in ChatArea
      appendStreamingChunk: (sessionId: string, chunkIndex: number, chunk: string) => {
        if (typeof chunk !== 'string' || !chunk) return

        // Reset inactivity auto-clear timer — if no new chunk arrives in 3s, clear streaming text
        if (_streamingInactivityTimers[sessionId]) {
          clearTimeout(_streamingInactivityTimers[sessionId])
        }
        _streamingInactivityTimers[sessionId] = setTimeout(() => {
          const currentText = useChatStore.getState().streamingText[sessionId]
          if (currentText) {
            useChatStore.getState().clearStreamingText(sessionId)
          }
          delete _streamingInactivityTimers[sessionId]
        }, STREAMING_INACTIVITY_MS)

        set((state) => {
          let lastIndex = state.lastStreamingChunkIndex[sessionId] ?? -1
          let currentText = state.streamingText[sessionId] || ''
          let clearCompleted = false

          // Auto-reset if we see chunk 0 or 1 (start of new generation)
          if (chunkIndex === 0 || chunkIndex === 1) {
             lastIndex = -1
             currentText = ''
             clearCompleted = true
          }

          // Deduplicate: skip chunks already processed (handles concurrent poll overlap)
          if (chunkIndex >= 0 && chunkIndex <= lastIndex) {
            return state
          }

          // Build completedStreamingText update if needed
          const completedUpdate = clearCompleted
            ? (() => { const c = { ...state.completedStreamingText }; delete c[sessionId]; return c })()
            : state.completedStreamingText

          // Route heartbeat/status messages (⏳/⚠️ Gemini) to streamingStatus instead of streamingText
          const isStatusMessage = chunk.includes('⏳') || chunk.includes('⚠️ Gemini')
          if (isStatusMessage) {
            return {
              streamingStatus: {
                ...state.streamingStatus,
                [sessionId]: chunk.trim()
              },
              lastStreamingChunkIndex: {
                ...state.lastStreamingChunkIndex,
                [sessionId]: chunkIndex
              },
              ...(clearCompleted ? { completedStreamingText: completedUpdate } : {})
            }
          }

          // Clear status once real content arrives
          const newStreamingStatus = { ...state.streamingStatus }
          delete newStreamingStatus[sessionId]

          return {
            streamingText: {
              ...state.streamingText,
              [sessionId]: currentText + chunk
            },
            streamingStatus: newStreamingStatus,
            lastStreamingChunkIndex: {
              ...state.lastStreamingChunkIndex,
              [sessionId]: chunkIndex
            },
            ...(clearCompleted ? { completedStreamingText: completedUpdate } : {})
          }
        })
      },

      clearStreamingText: (sessionId: string) => {
        // Cancel any pending inactivity timer
        if (_streamingInactivityTimers[sessionId]) {
          clearTimeout(_streamingInactivityTimers[sessionId])
          delete _streamingInactivityTimers[sessionId]
        }
        set((state) => {
          const newStreamingText = { ...state.streamingText }
          const currentText = newStreamingText[sessionId]
          delete newStreamingText[sessionId]
          const newStreamingStatus = { ...state.streamingStatus }
          delete newStreamingStatus[sessionId]
          const newLastIdx = { ...state.lastStreamingChunkIndex }
          delete newLastIdx[sessionId]
          // Preserve completed streaming text so it stays visible after generation ends.
          // APPEND to existing (don't replace) so multiple thinking rounds accumulate.
          const newCompletedStreamingText = { ...state.completedStreamingText }
          if (currentText) {
            const existing = newCompletedStreamingText[sessionId]
            newCompletedStreamingText[sessionId] = existing
              ? existing + '\n\n---\n\n' + currentText
              : currentText
          }
          return { streamingText: newStreamingText, streamingStatus: newStreamingStatus, lastStreamingChunkIndex: newLastIdx, completedStreamingText: newCompletedStreamingText }
        })
      },

      clearStreamingStatus: (sessionId: string) => {
        set((state) => {
          const newStreamingStatus = { ...state.streamingStatus }
          delete newStreamingStatus[sessionId]
          return { streamingStatus: newStreamingStatus }
        })
      },

      // Delegation streaming text actions
      appendDelegationStreamingChunk: (delegationId: string, chunkIndex: number, chunk: string) => {
        if (typeof chunk !== 'string' || !chunk) return
        set((state) => {
          let lastIndex = state.lastDelegationChunkIndex[delegationId] ?? -1
          let currentText = state.delegationStreamingText[delegationId] || ''

          // Auto-reset if we see chunk 0 or 1 (start of new generation)
          if (chunkIndex === 0 || chunkIndex === 1) {
            lastIndex = -1
            currentText = ''
          }

          // Deduplicate: skip chunks already processed
          if (chunkIndex >= 0 && chunkIndex <= lastIndex) {
            return state
          }

          return {
            delegationStreamingText: {
              ...state.delegationStreamingText,
              [delegationId]: currentText + chunk
            },
            lastDelegationChunkIndex: {
              ...state.lastDelegationChunkIndex,
              [delegationId]: chunkIndex
            }
          }
        })
      },

      clearDelegationStreamingText: (delegationId: string) => {
        set((state) => {
          const newText = { ...state.delegationStreamingText }
          delete newText[delegationId]
          const newIdx = { ...state.lastDelegationChunkIndex }
          delete newIdx[delegationId]
          return { delegationStreamingText: newText, lastDelegationChunkIndex: newIdx }
        })
      },

      // SSE connection management
      connectSSE: (sessionId, onMessage, onStatus) => {
        const state = get()
        // Close existing connection for this session if any
        if (state.sseConnections[sessionId]) {
          state.sseConnections[sessionId].close()
        }
        const lastIndex = state.tabEventIndices[sessionId] ?? 0
        const conn = new SSEConnection(sessionId, lastIndex, {
          onMessage,
          onStatusUpdate: onStatus,
          onError: () => {
            // Fallback to polling on persistent SSE errors
            logger.warn('ChatStore', `SSE fallback triggered for session ${sessionId}, falling back to polling`)
            // Remove the failed SSE connection
            set((s) => {
              const conns = { ...s.sseConnections }
              delete conns[sessionId]
              return { sseConnections: conns }
            })
          },
          onOpen: () => {
            logger.debug('ChatStore', `SSE connected for session ${sessionId}`)
          },
        })
        set((s) => ({
          sseConnections: { ...s.sseConnections, [sessionId]: conn },
        }))
      },

      disconnectSSE: (sessionId) => {
        const state = get()
        const conn = state.sseConnections[sessionId]
        if (conn) {
          conn.close()
          set((s) => {
            const conns = { ...s.sseConnections }
            delete conns[sessionId]
            return { sseConnections: conns }
          })
        }
      },

      disconnectAllSSE: () => {
        const state = get()
        Object.values(state.sseConnections).forEach((conn) => conn.close())
        set({ sseConnections: {} })
      },

      // Reset a single tab's chat session without touching other tabs (used by prototype "New Chat")
      resetTabChat: (tabId: string) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return

        const oldSessionId = tab.sessionId
        const newSessionId = globalThis.crypto.randomUUID()

        // Stop any streaming + close SSE for this tab
        if (oldSessionId && state.sseConnections[oldSessionId]) {
          state.sseConnections[oldSessionId].close()
        }

        // Clear per-session data and assign a new session ID
        set((s) => {
          const newTabEvents = { ...s.tabEvents }
          const newTabEventIndices = { ...s.tabEventIndices }
          const newTabHasMore = { ...s.tabHasMoreOlderEvents }
          const newStreamingText = { ...s.streamingText }
          const newSSE = { ...s.sseConnections }
          if (oldSessionId) {
            delete newTabEvents[oldSessionId]
            delete newTabEventIndices[oldSessionId]
            delete newTabHasMore[oldSessionId]
            delete newStreamingText[oldSessionId]
            delete newSSE[oldSessionId]
          }
          return {
            chatTabs: {
              ...s.chatTabs,
              [tabId]: { ...tab, sessionId: newSessionId, isStreaming: false },
            },
            tabEvents: newTabEvents,
            tabEventIndices: newTabEventIndices,
            tabHasMoreOlderEvents: newTabHasMore,
            streamingText: newStreamingText,
            sseConnections: newSSE,
          }
        })
      },

      // Helper methods
      resetChatState: () => {
        const state = get()

        // Close all SSE connections
        Object.values(state.sseConnections).forEach((conn) => conn.close())

        // Close all tabs and stop sessions
        Object.values(state.chatTabs).forEach(async (tab) => {
          try {
            if (tab.isStreaming && tab.sessionId) {
              await agentApi.stopSession(tab.sessionId)
            }
          } catch (error) {
            logger.error('ChatStore', `Error cleaning up tab ${tab.tabId}:`, error)
          }
        })
        
        set({
          isStreaming: false,
          lastEventIndex: -1,
          pollingInterval: null,
          // Deprecated fields removed
          currentUserMessage: '',
          showUserMessage: true,
          sessionId: null,
          hasActiveChat: false,
          autoScroll: true,
          finalResponse: '',
          isCompleted: false,
          isLoadingHistory: false,
          isApprovingWorkflow: false,
          sessionState: 'loading',
          isCheckingActiveSessions: false,
          currentWorkflowPhase: 'planning' as WorkflowPhase,
          currentWorkflowQueryId: null,
          toasts: [],
          chatTabs: {},
          activeTabId: null,
          streamingText: {},
          streamingStatus: {},
          lastStreamingChunkIndex: {},
          completedStreamingText: {},
          delegationStreamingText: {},
          lastDelegationChunkIndex: {}
        })

        // Clear the requiresNewChat flag after successful chat reset
        useAppStore.getState().clearRequiresNewChat()
      },

      isAtBottom: (element) => {
        const threshold = 150 // Generous threshold so scrolling back down re-enables auto-scroll easily
        const isAtBottom = element.scrollTop + element.clientHeight >= element.scrollHeight - threshold

        return isAtBottom;
      },

      // Generic actions
      reset: () => {
        get().resetChatState()
      },

      setLoading: (loading) => {
        set({ isLoadingHistory: loading })
      },

      setError: (error) => {
        if (error) {
          get().addToast(error, 'error')
        }
      },
      
      // Tab management actions
      createChatTab: async (name: string, metadata?: ChatTab['metadata'], existingObserverId?: string) => {
        // Generate unique tab ID
        const timestamp = Date.now()
        const mode = metadata?.mode || 'multi-agent'
        const tabId = mode === 'workflow' && metadata?.phaseId
          ? `phase_${metadata.phaseId}_${timestamp}`
          : `chat_${timestamp}`
        
        // Generate session ID for the new tab if not provided
        let sessionIdForTab: string | null = null
        
        if (existingObserverId) {
          // If existingObserverId is provided, treat it as sessionId
          sessionIdForTab = existingObserverId
          logger.debug('TabStore', `Using existing session ID for tab ${tabId}: ${sessionIdForTab}`)
        } else {
          // Generate new session ID
          sessionIdForTab = globalThis.crypto.randomUUID()
          logger.debug('TabStore', `Generated new session ID for tab ${tabId}: ${sessionIdForTab}`)
        }
        
        // Get default config from current global state (mode-specific)
        const defaultConfig = getDefaultTabConfig(mode)
        
        // Validate session ID before creating tab
        if (!sessionIdForTab || sessionIdForTab.trim() === '') {
          logger.error('TabStore', `Cannot create tab ${tabId} - sessionId is empty!`)
          throw new Error('Session ID is required but was not provided or is empty')
        }
        
        // Create tab with session ID
        const tab: ChatTab = {
          tabId,
          name,
          sessionId: sessionIdForTab,
          isStreaming: false,
          isCompleted: false,
          hasRunningBgAgents: false,
          isSyntheticTurn: false,
          hideToolCalls: true,
          viewMode: 'detailed', // Default to full detail — user or system can switch to 'summary' for background workflows
          config: defaultConfig, // Initialize with default config from global state
          createdAt: timestamp,
          lastViewedEventCount: 0, // @deprecated - kept for backwards compat
          lastViewedEventCounts: { micro: 0 }, // Initialize all modes to 0
          metadata
        }
        
        logger.debug('TabStore', `Creating tab with session ID:`, {
          tabId,
          name,
          sessionId: sessionIdForTab,
          hasSessionId: !!sessionIdForTab
        })
        
        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: tab
          },
          activeTabId: tabId
        }))
        
        // Verify the tab was stored correctly
        const storedTab = get().chatTabs[tabId]
        if (!storedTab) {
          logger.error('TabStore', `Tab ${tabId} was not stored in state!`)
          throw new Error('Failed to store tab in state')
        }
        if (!storedTab.sessionId) {
          logger.error('TabStore', `Tab ${tabId} stored but has no sessionId!`, storedTab)
          throw new Error('Tab stored without session ID')
        }
        
        logger.debug('TabStore', `Tab ${tabId} created and stored successfully with session ID: ${storedTab.sessionId}`)
        return tabId
      },
      
      switchTab: (tabId: string) => {
        // Use single atomic set() to avoid race conditions with stale state
        set((state) => {
          if (!state.chatTabs[tabId]) {
            logger.warn('TabStore', `Tab ${tabId} not found`)
            return state
          }

          const updates: Record<string, ChatTab> = {}
          let newTabEvents = state.tabEvents
          let newTabEventIndices = state.tabEventIndices
          let newTabHasMore = state.tabHasMoreOlderEvents

          // Update previous active tab's lastViewedEventCounts before switching
          if (state.activeTabId && state.activeTabId !== tabId) {
            const previousTab = state.chatTabs[state.activeTabId]
            if (previousTab?.sessionId) {
              const events = state.tabEvents[previousTab.sessionId] || []
              updates[state.activeTabId] = {
                ...previousTab,
                lastViewedEventCount: events.length, // @deprecated - kept for compat
                lastViewedEventCounts: computePerModeCounts(events)
              }
            }
          }

          // Update new tab's lastViewedEventCounts
          const newTab = state.chatTabs[tabId]
          if (newTab?.sessionId) {
            const events = state.tabEvents[newTab.sessionId] || []
            updates[tabId] = {
              ...newTab,
              lastViewedEventCount: events.length, // @deprecated - kept for compat
              lastViewedEventCounts: computePerModeCounts(events)
            }
          }

          // Clean up orphaned tab events (sessions no longer referenced by any tab)
          const referencedSessionIds = new Set(
            Object.values({ ...state.chatTabs, ...updates })
              .map(tab => tab.sessionId)
              .filter(Boolean)
          )
          for (const sessionId of Object.keys(newTabEvents)) {
            if (!referencedSessionIds.has(sessionId)) {
              if (newTabEvents === state.tabEvents) {
                newTabEvents = { ...newTabEvents }
                newTabEventIndices = { ...newTabEventIndices }
                newTabHasMore = { ...newTabHasMore }
              }
              delete newTabEvents[sessionId]
              delete newTabEventIndices[sessionId]
              delete newTabHasMore[sessionId]
              logger.debug('TabStore', `Cleaned up orphaned events for session ${sessionId}`)
            }
          }

          return {
            activeTabId: tabId,
            chatTabs: { ...state.chatTabs, ...updates },
            tabEvents: newTabEvents,
            tabEventIndices: newTabEventIndices,
            tabHasMoreOlderEvents: newTabHasMore
          }
        })
      },
      
      closeTab: async (tabId: string, stopSession: boolean = true, keepEvents: boolean = false) => {
        const state = get()
        const tab = state.chatTabs[tabId]

        if (!tab) {
          logger.warn('TabStore', `Tab ${tabId} not found`)
          return
        }

        // Stop session if streaming (unless explicitly disabled, e.g., when minimizing to background)
        if (stopSession && tab.isStreaming && tab.sessionId) {
          try {
            await agentApi.stopSession(tab.sessionId)
          } catch (error) {
            logger.error('SessionStore', `Failed to stop session ${tab.sessionId}:`, error)
          }
        }

        // Dismiss session so it won't be auto-restored on page refresh (fire-and-forget)
        // Skip dismiss for workflow tabs — their sessions should remain restorable via DB
        if (tab.sessionId && tab.metadata?.mode !== 'workflow') {
          logger.info('SessionStore', `Dismissing session ${tab.sessionId} (stopSession=${stopSession})`)
          agentApi.dismissSession(tab.sessionId).catch(error => {
            logger.error('SessionStore', `Failed to dismiss session ${tab.sessionId}:`, error)
          })
        } else if (!tab.sessionId) {
          logger.info('SessionStore', `Tab ${tabId} has no sessionId, skipping dismiss`)
        }

        // Check if any OTHER tab shares this sessionId (e.g., duplicate workflow tabs)
        // If so, don't disconnect SSE or clean up session-keyed resources — the other tab needs them
        const otherTabUsesSession = tab.sessionId && Object.values(state.chatTabs).some(
          t => t.tabId !== tabId && t.sessionId === tab.sessionId
        )

        // Disconnect SSE connection for this session (only if no other tab shares it)
        if (tab.sessionId && !otherTabUsesSession && state.sseConnections[tab.sessionId]) {
          state.sseConnections[tab.sessionId].close()
        }

        // Clear tab's events (by sessionId) unless keepEvents is true (e.g., for background workflows)
        let newTabEvents = state.tabEvents
        let newTabEventIndices = state.tabEventIndices
        if (!keepEvents && tab.sessionId && !otherTabUsesSession) {
          newTabEvents = { ...state.tabEvents }
          delete newTabEvents[tab.sessionId]
          newTabEventIndices = { ...state.tabEventIndices }
          delete newTabEventIndices[tab.sessionId]
        }

        // Remove tab
        const newTabs = { ...state.chatTabs }
        delete newTabs[tabId]

        // Switch to another tab if this was active
        let newActiveTabId = state.activeTabId
        if (state.activeTabId === tabId) {
          const remainingTabs = Object.values(newTabs).sort((a, b) => b.createdAt - a.createdAt)
          newActiveTabId = remainingTabs.length > 0 ? remainingTabs[0].tabId : null
        }

        // Clean up all resources associated with this tab
        const updates: Partial<ChatState> = {
          chatTabs: newTabs,
          activeTabId: newActiveTabId,
          tabEvents: newTabEvents,
          tabEventIndices: newTabEventIndices
        }

        // Clean up SSE connection entry (only if no other tab shares the session)
        if (tab.sessionId && !otherTabUsesSession && state.sseConnections[tab.sessionId]) {
          const newConns = { ...state.sseConnections }
          delete newConns[tab.sessionId]
          updates.sseConnections = newConns
        }

        // Clean up session-keyed resources (only if no other tab shares the session)
        if (tab.sessionId && !otherTabUsesSession) {
          const newStreamingText = { ...state.streamingText }
          delete newStreamingText[tab.sessionId]
          updates.streamingText = newStreamingText

          const newLastChunkIndex = { ...state.lastStreamingChunkIndex }
          delete newLastChunkIndex[tab.sessionId]
          updates.lastStreamingChunkIndex = newLastChunkIndex

          const newHasMore = { ...state.tabHasMoreOlderEvents }
          delete newHasMore[tab.sessionId]
          updates.tabHasMoreOlderEvents = newHasMore
        }

        // Clean up tab session status (always — this is keyed by tabId, not sessionId)
        const newTabSessionStatus = { ...state.tabSessionStatus }
        delete newTabSessionStatus[tabId]
        updates.tabSessionStatus = newTabSessionStatus

        set(updates)
      },
      
      getTab: (tabId: string) => {
        return get().chatTabs[tabId]
      },
      
      getActiveTab: () => {
        const state = get()
        if (!state.activeTabId) return undefined
        return state.chatTabs[state.activeTabId]
      },
      
      getTabsByMode: (mode: 'multi-agent' | 'workflow' | 'organization') => {
        const state = get()
        return Object.values(state.chatTabs).filter(tab => tab.metadata?.mode === mode)
      },
      
      getTabsByPhaseId: (phaseId: string, presetQueryId?: string) => {
        const state = get()
        return Object.values(state.chatTabs).filter(
          tab => tab.metadata?.mode === 'workflow' &&
            tab.metadata?.phaseId === phaseId &&
            (!presetQueryId || tab.metadata?.presetQueryId === presetQueryId)
        )
      },
      
      setTabStreaming: (tabId: string, isStreaming: boolean) => {
        const tab = get().chatTabs[tabId]
        if (!tab || tab.isStreaming === isStreaming) return

        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: { ...state.chatTabs[tabId], isStreaming }
          }
        }))
      },

      setTabCompleted: (tabId: string, isCompleted: boolean) => {
        const tab = get().chatTabs[tabId]
        if (!tab) return
        const newStreaming = isCompleted ? false : tab.isStreaming
        if (tab.isCompleted === isCompleted && tab.isStreaming === newStreaming) return

        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: {
              ...state.chatTabs[tabId],
              isCompleted,
              isStreaming: newStreaming
            }
          }
        }))
      },

      setTabHasRunningBgAgents: (tabId: string, hasRunningBgAgents: boolean) => {
        const tab = get().chatTabs[tabId]
        if (!tab || tab.hasRunningBgAgents === hasRunningBgAgents) return

        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: { ...state.chatTabs[tabId], hasRunningBgAgents }
          }
        }))
      },

      setTabSyntheticTurn: (tabId: string, isSyntheticTurn: boolean) => {
        const tab = get().chatTabs[tabId]
        if (!tab || tab.isSyntheticTurn === isSyntheticTurn) return

        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: { ...state.chatTabs[tabId], isSyntheticTurn }
          }
        }))
      },

      updateTabSessionId: (tabId: string, newSessionId: string) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return

        const oldSessionId = tab.sessionId

        // If sessionId hasn't changed, nothing to do
        if (oldSessionId === newSessionId) return

        set((state) => {
          const updates: Partial<ChatState> = {
            chatTabs: {
              ...state.chatTabs,
              [tabId]: {
                ...tab,
                sessionId: newSessionId
              }
            }
          }

          // For non-workflow tabs: migrate events from old to new sessionId
          // For workflow tabs: discard old events (re-runs should start fresh)
          if (oldSessionId && state.tabEvents[oldSessionId]) {
            const isWorkflowTab = tab.metadata?.mode === 'workflow'

            if (!isWorkflowTab) {
              // Migrate events to preserve conversation history for chat/multi-agent
              const oldEvents = state.tabEvents[oldSessionId]
              const oldEventIndex = state.tabEventIndices[oldSessionId]
              const oldHasMore = state.tabHasMoreOlderEvents[oldSessionId]

              updates.tabEvents = {
                ...state.tabEvents,
                [newSessionId]: oldEvents
              }
              if (oldEventIndex !== undefined) {
                updates.tabEventIndices = {
                  ...state.tabEventIndices,
                  [newSessionId]: oldEventIndex
                }
              }
              if (oldHasMore !== undefined) {
                updates.tabHasMoreOlderEvents = {
                  ...state.tabHasMoreOlderEvents,
                  [newSessionId]: oldHasMore
                }
              }
            }

            // Clean up old sessionId entries (always, for both modes)
            const newTabEvents = { ...(updates.tabEvents || state.tabEvents) }
            delete newTabEvents[oldSessionId]
            updates.tabEvents = newTabEvents

            const newTabEventIndices = { ...(updates.tabEventIndices || state.tabEventIndices) }
            delete newTabEventIndices[oldSessionId]
            updates.tabEventIndices = newTabEventIndices

            const newTabHasMore = { ...(updates.tabHasMoreOlderEvents || state.tabHasMoreOlderEvents) }
            delete newTabHasMore[oldSessionId]
            updates.tabHasMoreOlderEvents = newTabHasMore
          }

          return updates
        })
      },
      
      // updateTabObserverId removed - observers no longer used
      

      setTabHideToolCalls: (tabId: string, hideToolCalls: boolean) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return

        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: {
              ...tab,
              hideToolCalls
            }
          }
        }))
      },

      setTabViewMode: (tabId: string, viewMode: 'detailed' | 'summary') => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return

        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: {
              ...state.chatTabs[tabId],
              viewMode
            }
          }
        }))
      },

      getTabConfig: (tabId: string) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        return tab?.config
      },
      
      setTabConfig: (tabId: string, configUpdate: Partial<ChatTabConfig>) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return

        set((state) => {
          const freshTab = state.chatTabs[tabId]
          if (!freshTab) return state
          return {
            chatTabs: {
              ...state.chatTabs,
              [tabId]: {
                ...freshTab,
                config: {
                  ...freshTab.config,
                  ...configUpdate
                }
              }
            }
          }
        })

        // Sync last-used settings to AppStore so new tabs inherit them.
        // Only sync for multi-agent tabs — workflow tabs have different settings.
        const tabMode = tab.metadata?.mode
        if (tabMode === 'multi-agent') {
          type SyncUpdate = {
            lastSelectedSkills?: string[]
            lastSelectedSubAgents?: string[]
            lastBrowserMode?: 'none' | 'headless' | 'cdp' | 'playwright' | 'stealth'
            lastEnableImageGeneration?: boolean
            lastGWSAccess?: boolean
          }
          const sync: SyncUpdate = {}
          if (configUpdate.selectedSkills !== undefined) sync.lastSelectedSkills = configUpdate.selectedSkills
          if (configUpdate.selectedSubAgents !== undefined) sync.lastSelectedSubAgents = configUpdate.selectedSubAgents
          if (configUpdate.browserMode !== undefined) sync.lastBrowserMode = configUpdate.browserMode
          if (configUpdate.enableImageGeneration !== undefined) sync.lastEnableImageGeneration = configUpdate.enableImageGeneration
          if (configUpdate.enableGWSAccess !== undefined) sync.lastGWSAccess = configUpdate.enableGWSAccess
          if (Object.keys(sync).length > 0) {
            console.log('[TabSettings] Syncing to AppStore:', sync)
            useAppStore.getState().syncLastTabSettings(sync)
          }
        }
      },
      
      setTabMetadata: (tabId: string, metadataUpdate: Partial<NonNullable<ChatTab['metadata']>>) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return
        
        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: {
              ...tab,
              metadata: {
                ...tab.metadata,
                ...metadataUpdate
              }
            }
          }
        }))
      },
      
      getTabStreamingStatus: (tabId: string) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return false
        
        // If tab is marked as completed, it's not streaming
        if (tab.isCompleted) return false
        
        // Tab is streaming if:
        // 1. Polling is active
        // 2. Not manually paused (stored isStreaming !== false)
        const isPolling = state.pollingInterval !== null
        
        if (isPolling) {
          return tab.isStreaming !== false // Respect manual pause
        }
        
        return false
      },
      
      checkTabCompletion: (tabId: string, events: Array<{ type: string }>) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return false
        
        const mode = tab.metadata?.mode || 'multi-agent'
        
        // Check if any events are completion events
        const completionEventTypes = mode === 'workflow'
          ? ['workflow_end', 'request_human_feedback']
          : ['unified_completion', 'agent_end', 'conversation_end', 'conversation_error', 'agent_error']
        
        return events.some(event => completionEventTypes.includes(event.type))
      },
      
      // Tab session status actions
      fetchTabSessionStatus: async (tabId: string) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        
        if (!tab || !tab.sessionId) {
          logger.warn('SessionStore', `Cannot fetch session status - tab ${tabId} has no session ID`)
          return
        }
        
        try {
          let status: TabSessionStatus = {
            status: null,
            agentMode: null,
            lastActivity: null
          }
          
          if (tab.sessionId) {
            // Check if this session is in the active sessions list before calling getSessionStatus
            // Use cached active sessions to avoid unnecessary API calls
            try {
              const { getActiveSessions } = get()
              const activeSessions = await getActiveSessions()
              const activeSessionIds = new Set(activeSessions.map(s => s.session_id))
              
              // Only call getSessionStatus if the session is active
              if (activeSessionIds.has(tab.sessionId)) {
                try {
                  // Get session status
                  const sessionStatus: SessionStatusResponse = await agentApi.getSessionStatus(tab.sessionId)
                  status = {
                    status: sessionStatus.status || null,
                    agentMode: sessionStatus.agent_mode || null,
                    lastActivity: sessionStatus.last_activity || null
                  }
                } catch (sessionError: unknown) {
                  // Handle 404 or other session status errors gracefully
                  const axiosError = sessionError as { response?: { status?: number }; message?: string }
                  if (axiosError?.response?.status === 404) {
                    // Session not found
                    status = {
                      status: null,
                      agentMode: null,
                      lastActivity: null
                    }
                  }
                }
              }
            } catch {
              // If active sessions check fails, fall back to calling getSessionStatus
              try {
                const sessionStatus: SessionStatusResponse = await agentApi.getSessionStatus(tab.sessionId)
                status = {
                  status: sessionStatus.status || null,
                  agentMode: sessionStatus.agent_mode || null,
                  lastActivity: sessionStatus.last_activity || null
                }
              } catch (sessionError: unknown) {
                // Handle 404 or other session status errors gracefully
                const axiosError = sessionError as { response?: { status?: number }; message?: string }
                if (axiosError?.response?.status === 404) {
                  // Session not found
                  status = {
                    status: null,
                    agentMode: null,
                    lastActivity: null
                  }
                }
              }
            }
          }
          
          // Update status in store
          set((state) => ({
            tabSessionStatus: {
              ...state.tabSessionStatus,
              [tabId]: status
            }
          }))
        } catch (error: unknown) {
          // Handle session status errors gracefully
          const axiosError = error as { response?: { status?: number }; message?: string }
          if (axiosError?.response?.status === 404) {
            // Session not found - it may have been cleaned up
            logger.debug('SessionStore', `Session ${tab.sessionId} not found (404) for tab ${tabId}`)
          } else {
            logger.warn('SessionStore', `Failed to fetch session status for tab ${tabId}:`, axiosError.message || 'Unknown error')
          }
          
          // Set status to null on error
          set((state) => ({
            tabSessionStatus: {
              ...state.tabSessionStatus,
              [tabId]: {
                status: null,
                agentMode: null,
                lastActivity: null
              }
            }
          }))
        }
      },
      
      fetchAllTabSessionStatuses: async (tabIds: string[]) => {
        // First check if there are any active sessions using cache
        try {
          const { getActiveSessions } = get()
          const activeSessions = await getActiveSessions()
          const activeSessionIds = new Set(activeSessions.map(s => s.session_id))
          
          // If no active sessions, skip all status calls (no need to poll)
          if (activeSessionIds.size === 0) {
            logger.debug('SessionStore', 'No active sessions found, skipping status fetch')
            return
          }
          
          // Filter tabs: only fetch status for tabs that either:
          // 1. Have a sessionId that matches an active session, OR
          // 2. Don't have a sessionId yet (need to check observer to get it)
          const state = get()
          const tabsToFetch = tabIds.filter(tabId => {
            const tab = state.chatTabs[tabId]
            if (!tab) return false
            
            // If tab has sessionId, only fetch if it's in active sessions
            if (tab.sessionId) {
              return activeSessionIds.has(tab.sessionId)
            }
            
            // If tab doesn't have sessionId yet, we still need to check observer
            // to see if it has a session (it might be newly created)
            return true
          })
          
          if (tabsToFetch.length === 0) {
            logger.debug('SessionStore', 'No tabs with active sessions to fetch status for')
            return
          }

          logger.debug('SessionStore', `Fetching status for ${tabsToFetch.length} tabs (${activeSessionIds.size} active sessions)`)
          
          // Fetch status for tabs in parallel
          const { fetchTabSessionStatus } = get()
          const promises = tabsToFetch.map(tabId => fetchTabSessionStatus(tabId))
          await Promise.all(promises)
        } catch (error) {
          logger.warn('SessionStore', 'Failed to check active sessions, falling back to fetching all tab statuses:', error)
          // Fallback: fetch status for all tabs if active sessions check fails
          const { fetchTabSessionStatus } = get()
          const promises = tabIds.map(tabId => fetchTabSessionStatus(tabId))
          await Promise.all(promises)
        }
      },
      
      getTabSessionStatus: (tabId: string) => {
        return get().tabSessionStatus[tabId]
      },
      
      // Active sessions cache actions
      getActiveSessions: async (forceRefresh = false): Promise<ActiveSessionInfo[]> => {
        const state = get()
        const now = Date.now()
        
        // Return cached data if it's still fresh and not forcing refresh
        if (!forceRefresh && 
            state.activeSessionsCacheTimestamp !== null && 
            (now - state.activeSessionsCacheTimestamp) < ACTIVE_SESSIONS_CACHE_TTL) {
          return state.activeSessionsCache
        }
        
        // Fetch fresh data
        try {
          const response = await agentApi.getActiveSessions()
          const activeSessions = response.active_sessions || []
          
          set({
            activeSessionsCache: activeSessions,
            activeSessionsCacheTimestamp: now
          })
          
          return activeSessions
        } catch (error) {
          logger.error('SessionStore', 'Failed to fetch active sessions:', error)
          // Return cached data even if stale on error
          return state.activeSessionsCache
        }
      },
      
      getActiveSessionIds: (): Set<string> => {
        const state = get()
        return new Set(state.activeSessionsCache.map(s => s.session_id))
      },
      
      startActiveSessionsPolling: () => {
        const state = get()
        
        // Don't start if already polling
        if (state.activeSessionsPollingInterval !== null) {
          return
        }
        
        // Initial fetch
        const { getActiveSessions } = get()
        getActiveSessions(true).catch(error => {
          logger.error('Polling', 'Failed to fetch active sessions on polling start:', error)
        })
        
        // Poll every 60 seconds
        const interval = setInterval(() => {
          const { getActiveSessions } = get()
          getActiveSessions(true).catch(error => {
            logger.error('Polling', 'Failed to fetch active sessions during polling:', error)
          })
        }, 60000)
        
        set({ activeSessionsPollingInterval: interval })
      },
      
      stopActiveSessionsPolling: () => {
        const state = get()
        if (state.activeSessionsPollingInterval !== null) {
          clearInterval(state.activeSessionsPollingInterval)
          set({ activeSessionsPollingInterval: null })
        }
      },
      
      // Chat history cache actions
      getChatHistory: async (modeCategory: string, forceRefresh = false): Promise<ChatHistorySummary[]> => {
        const state = get()
        const now = Date.now()
        
        // Return cached data if:
        // 1. Not forcing refresh
        // 2. Cache exists and is still fresh
        // 3. Cache was loaded for the same mode category
        if (!forceRefresh && 
            state.chatHistoryCacheTimestamp !== null && 
            state.chatHistoryLastLoadedMode === modeCategory &&
            (now - state.chatHistoryCacheTimestamp) < CHAT_HISTORY_CACHE_TTL) {
          logger.debug('ChatStore', 'Returning cached chat history for mode:', modeCategory)
          return state.chatHistoryCache
        }
        
        // Fetch fresh data — fetch 50 to give client-side mode filtering enough to work with
        try {
                // Map mode category to agent mode for filtering
                let agentMode: string | undefined
                if (modeCategory === 'workflow') {
                  agentMode = 'workflow'
                }
                // For multi-agent mode, fetch all non-workflow sessions;
                // client-side filtering splits them by delegation_mode

                logger.debug('ChatStore', `Fetching fresh chat history for mode: ${modeCategory} (agentMode: ${agentMode})`)
                const response = await agentApi.getChatSessions(50, 0, undefined, agentMode)
                const sessions = response.sessions || []
          
          set({
            chatHistoryCache: sessions,
            chatHistoryCacheTimestamp: now,
            chatHistoryLastLoadedMode: modeCategory,
            chatHistoryTotalCount: response.total || 0,
            chatHistoryLoadedCount: sessions.length
          })
          
          return sessions
        } catch (error) {
          logger.error('ChatStore', 'Failed to fetch chat history:', error)
          // Return cached data even if stale on error
          return state.chatHistoryCache
        }
      },
      
      loadMoreChatHistory: async (modeCategory: string): Promise<ChatHistorySummary[]> => {
        const state = get()
        
        // Check if we have more to load
        if (state.chatHistoryTotalCount === null || state.chatHistoryLoadedCount >= state.chatHistoryTotalCount) {
          logger.debug('ChatStore', 'No more chat history to load')
          return state.chatHistoryCache
        }
        
        // Load next batch
        try {
                // Map mode category to agent mode for filtering
                let agentMode: string | undefined
                if (modeCategory === 'workflow') {
                  agentMode = 'workflow'
                }

                logger.debug('ChatStore', `Loading more chat history for mode: ${modeCategory} (agentMode: ${agentMode}) (offset: ${state.chatHistoryLoadedCount}, limit: 50)`)
                const response = await agentApi.getChatSessions(50, state.chatHistoryLoadedCount, undefined, agentMode)
                const newSessions = response.sessions || []
          
          // Append new sessions to existing cache
          const updatedSessions = [...state.chatHistoryCache, ...newSessions]
          
          set({
            chatHistoryCache: updatedSessions,
            chatHistoryTotalCount: response.total || state.chatHistoryTotalCount,
            chatHistoryLoadedCount: updatedSessions.length
          })
          
          return updatedSessions
        } catch (error) {
          logger.error('ChatStore', 'Failed to load more chat history:', error)
          return state.chatHistoryCache
        }
      },
      
      getChatHistoryHasMore: (): boolean => {
        const state = get()
        if (state.chatHistoryTotalCount === null) return false
        return state.chatHistoryLoadedCount < state.chatHistoryTotalCount
      },
      
      setChatHistory: (sessions: ChatHistorySummary[], modeCategory: string) => {
        set({
          chatHistoryCache: sessions,
          chatHistoryCacheTimestamp: Date.now(),
          chatHistoryLastLoadedMode: modeCategory
        })
      }
      }),
      {
        name: 'chat-store',
        partialize: (state) => ({
          // Persist workflow and multi-agent tabs (for reconnection)
          // Only persist tabs created within the last 24 hours to prevent stale tabs
          // from accumulating in localStorage and causing page hangs on reload
          chatTabs: Object.fromEntries(
            Object.entries(state.chatTabs)
              .filter(([, tab]) => {
                const isRelevantMode = tab.metadata?.mode === 'workflow' || tab.metadata?.mode === 'multi-agent' || tab.metadata?.mode === 'organization'
                if (!isRelevantMode) return false
                // Drop tabs older than 24 hours - they won't have active sessions
                const MAX_TAB_AGE = 24 * 60 * 60 * 1000
                const age = Date.now() - (tab.createdAt || 0)
                return age < MAX_TAB_AGE
              })
              .map(([tabId, tab]) => [
              tabId,
              {
                tabId: tab.tabId,
                name: tab.name,
                sessionId: tab.sessionId, // Persist session ID for direct restore on page refresh
                isStreaming: false, // Reset streaming state on reload
                isCompleted: false,
                hasRunningBgAgents: false,
                isSyntheticTurn: false,
                
                hideToolCalls: tab.hideToolCalls ?? true, // Default true — collapse tool calls by default
                viewMode: tab.viewMode ?? 'detailed', // Persist view mode across reload
                config: { ...tab.config, selectedPlanFolder: undefined }, // CRITICAL: Persist full config including:
                // - selectedServers (MCP server selections)
                // - llmConfig (LLM provider, model_id, fallback_models, etc.)
                // - useCodeExecutionMode (Simple vs Code Exec mode)
                // - fileContext (selected files/folders)
                // - inputText (chat input text)
                // - enableContextSummarization
                createdAt: tab.createdAt, // Persist for ordering
                lastViewedEventCount: 0, // @deprecated - Reset on reload
                lastViewedEventCounts: { micro: 0 }, // Reset on reload
                metadata: tab.metadata // Persist mode and phase info
              }
            ])
          ),
          // Persist activeTabId for workflow and multi-agent tabs
          activeTabId: (() => {
            const activeTab = state.activeTabId ? state.chatTabs[state.activeTabId] : null
            return (activeTab?.metadata?.mode === 'workflow' || activeTab?.metadata?.mode === 'multi-agent' || activeTab?.metadata?.mode === 'organization') ? state.activeTabId : null
          })()
          // Exclude all other state (isStreaming, pollingInterval, tabEvents, etc.)
        }),
        onRehydrateStorage: () => (state) => {
          if (!state) return
          // Signal that localStorage rehydration is complete
          markChatStoreHydrated()

          // Clean up stale tabs that survived partialize (edge case: clock skew, etc.)
          const MAX_TAB_AGE = 24 * 60 * 60 * 1000
          const now = Date.now()
          const freshTabs: Record<string, typeof state.chatTabs[string]> = {}
          let removedCount = 0
          for (const [tabId, tab] of Object.entries(state.chatTabs)) {
            const age = now - (tab.createdAt || 0)
            if (age >= MAX_TAB_AGE) {
              removedCount++
              continue
            }
            // Note: we previously dropped workflow tabs without phaseId here,
            // but this was too aggressive — it removed valid builder tabs from older sessions.
            // Stale tabs are handled by the 24-hour age check above.
            freshTabs[tabId] = tab
          }
          if (removedCount > 0) {
            logger.debug('ChatStore', `Cleaned up ${removedCount} stale tabs on rehydrate`)
            useChatStore.setState({ chatTabs: freshTabs })
          }

          // Migrate tabs: compute browserMode from enableBrowserAccess/useCdp if missing
          for (const tab of Object.values(freshTabs)) {
            if (tab.config && tab.config.browserMode === undefined) {
              if (tab.config.enableBrowserAccess) {
                tab.config.browserMode = tab.config.useCdp ? 'cdp' : 'headless'
              } else {
                tab.config.browserMode = 'none'
              }
            }
          }

          // Auto-select first tab if activeTabId is null or points to a removed tab
          if (!state.activeTabId || !freshTabs[state.activeTabId]) {
            const tabs = Object.values(freshTabs)
            if (tabs.length > 0) {
              const sorted = [...tabs].sort((a, b) => (a.createdAt || 0) - (b.createdAt || 0))
              useChatStore.setState({ activeTabId: sorted[0].tabId })
            } else {
              useChatStore.setState({ activeTabId: null })
            }
          }
        }
      }
    )
)

// Hydration flag — set to true once zustand rehydrates persisted tabs from localStorage.
// Uses a module-level variable (NOT a property on useChatStore) to survive Vite HMR,
// which re-executes the module and would reset a store property to false after rehydration already fired.
let chatStoreHydrated = false

/** Returns a promise that resolves once useChatStore has rehydrated from localStorage. */
export function waitForChatStoreHydration(): Promise<void> {
  if (chatStoreHydrated) {
    return Promise.resolve()
  }
  return new Promise(resolve => {
    let elapsed = 0
    const check = () => {
      if (chatStoreHydrated) {
        resolve()
      } else if (elapsed >= 3000) {
        // Safety timeout: don't hang forever if hydration never fires
        console.warn('[waitForChatStoreHydration] Timeout after 3s, proceeding anyway')
        resolve()
      } else {
        elapsed += 50
        setTimeout(check, 50)
      }
    }
    check()
  })
}

/** Called by onRehydrateStorage to signal hydration is complete */
export function markChatStoreHydrated() {
  chatStoreHydrated = true
}
