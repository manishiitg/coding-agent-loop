import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { PollingEvent, ExtendedLLMConfiguration, SessionStatusResponse, ActiveSessionInfo, ChatHistorySummary, DelegationTierConfig, SSEEventMessage, SSEStatusMessage } from '../services/api-types'
import { SSEConnection } from '../services/sse'
import { shouldShowEventByMode } from '../components/events/eventModeUtils'
import type { EventMode } from '../components/events/EventContext'
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

// Per-mode event counts type - stores last viewed count for each event mode
export type PerModeEventCounts = Record<EventMode, number>

// Helper to compute per-mode event counts
const computePerModeCounts = (events: PollingEvent[]): PerModeEventCounts => {
  return {
    advanced: events.length, // Advanced shows all events
    micro: events.filter(e => e.type && shouldShowEventByMode(e.type, 'micro')).length,
  }
}
// Chat history cache TTL (5 minutes - chat history doesn't change as frequently)
const CHAT_HISTORY_CACHE_TTL = 300000

// Event memory management constants - use shared constants
const MAX_EVENTS = MAX_EVENTS_TO_PROCESS

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
    'phase_completed'
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
  enableBrowserAccess?: boolean  // Enable/disable browser automation tool (auto-enables workspace when true)
  useCdp?: boolean  // Whether CDP mode is enabled (connect to local Chrome)
  cdpPort?: number  // CDP port (default 9222)
  delegationTierConfig?: DelegationTierConfig  // Per-tab delegation tier config (multi-agent mode)
  workflowContext: Array<{
    presetId: string
    label: string
    workspacePath: string
  }>  // Workflow presets selected via # in chat input
  queuedMessages: string[]  // Queue of messages to send one by one when chat completes
  autoRun?: boolean  // Automatically run the chat when tab is loaded
  planPhaseOverride?: 'planning' | 'execution' | null  // User-selected plan phase override for multi-agent mode
}

// Generalized ChatTab interface (works for both chat and workflow modes)
export interface ChatTab {
  tabId: string  // Unique ID: `chat_${timestamp}` or `phase_${phaseId}_${timestamp}`
  name: string  // Display name (e.g., "Chat 1", "Planning", "Execution")
  sessionId: string | null  // Chat session ID if exists
  isStreaming: boolean  // Whether this tab's execution is currently running
  isCompleted: boolean  // Whether this tab's execution has completed
  hasRunningBgAgents: boolean  // Whether background agents are still running for this session
  eventMode: 'advanced' | 'micro'  // Event display mode for this tab
  hideToolCalls: boolean  // Whether to hide tool_call_start/end events in this tab
  config: ChatTabConfig  // Tab-specific configuration
  createdAt: number  // Timestamp for ordering
  lastViewedEventCount: number  // @deprecated - use lastViewedEventCounts instead
  lastViewedEventCounts: PerModeEventCounts  // Per-mode event counts for accurate badge calculation
  // Mode-specific metadata
  metadata?: {
    phaseId?: string  // For workflow mode: phase ID
    phaseName?: string  // For workflow mode: phase name
    mode?: 'chat' | 'workflow' | 'multi-agent'  // Which mode this tab belongs to
    presetQueryId?: string  // For workflow mode: preset query ID (workflow identifier)
    isRestored?: boolean  // True when restored from history (sidebar, resume dialog, page refresh)
    isViewOnly?: boolean // True when tab is in view-only mode (e.g. shared session or bot connector)
  }
}

// Helper function to get default tab config from current global state
// Uses mode-specific configs for LLM and server selections
const getDefaultTabConfig = (mode: 'chat' | 'workflow' | 'multi-agent' = 'chat'): ChatTabConfig => {
  const mcpStore = useMCPStore?.getState?.()
  const llmStore = useLLMStore?.getState?.()

  // Get mode-specific server selection (multi-agent uses chat settings)
  const selectedServers = mode === 'workflow'
    ? (mcpStore?.workflowSelectedServers || mcpStore?.selectedServers || [])
    : (mcpStore?.chatSelectedServers || mcpStore?.selectedServers || [])

  // Get mode-specific LLM config (multi-agent uses chat settings)
  const llmConfig = mode === 'workflow'
    ? (llmStore?.workflowPrimaryConfig || llmStore?.primaryConfig)
    : (llmStore?.chatPrimaryConfig || llmStore?.primaryConfig)

  return {
    inputText: '',
    // Default to false (simple mode) - user can toggle to true (code exec mode) via ChatInput
    useCodeExecutionMode: false,
    // Default to false - user can toggle to true (tool search mode) via ChatInput
    useToolSearchMode: false,
    selectedServers,
    selectedSkills: [],  // No skills selected by default
    selectedSecrets: [],  // No secrets selected by default
    selectedSubAgents: [],  // No sub-agent templates selected by default
    workflowContext: [],  // No workflow context selected by default
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
    enableWorkspaceAccess: true,  // Enable workspace access by default
    enableBrowserAccess: false,  // Disable browser access by default (user must enable via checkbox)
    delegationTierConfig: mode === 'multi-agent' ? (llmStore?.delegationTierConfig ?? undefined) : undefined,
    queuedMessages: [],  // No queued messages by default
    autoRun: false  // Do not auto-run by default
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
  lastScrollTop: number
  
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
  streamingText: Record<string, string>  // sessionId → accumulated streaming text
  lastStreamingChunkIndex: Record<string, number>  // sessionId → last processed chunk_index (dedup guard)

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
      setTabEvents: (sessionId: string, events: PollingEvent[]) => void
      clearTabEvents: (sessionId: string) => void
      cleanupTabEvents: (sessionId: string, keepCount: number) => void
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
  setLastScrollTop: (scrollTop: number) => void
  
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
  createChatTab: (name: string, metadata?: ChatTab['metadata'], existingObserverId?: string, eventMode?: 'advanced' | 'micro') => Promise<string>  // Returns tabId
  switchTab: (tabId: string) => void
  closeTab: (tabId: string, stopSession?: boolean, keepEvents?: boolean) => Promise<void>
  getTab: (tabId: string) => ChatTab | undefined
  getActiveTab: () => ChatTab | undefined
  getTabsByMode: (mode: 'chat' | 'workflow') => ChatTab[]
  getTabsByPhaseId: (phaseId: string) => ChatTab[]  // Find workflow tabs by phaseId
  setTabStreaming: (tabId: string, isStreaming: boolean) => void
  setTabCompleted: (tabId: string, isCompleted: boolean) => void
  setTabHasRunningBgAgents: (tabId: string, hasRunningBgAgents: boolean) => void
  updateTabSessionId: (tabId: string, sessionId: string) => void
  setTabEventMode: (tabId: string, eventMode: 'advanced' | 'micro') => void
  setTabHideToolCalls: (tabId: string, hideToolCalls: boolean) => void
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

  // Delegation streaming text actions
  appendDelegationStreamingChunk: (delegationId: string, chunkIndex: number, chunk: string) => void
  clearDelegationStreamingText: (delegationId: string) => void

  // SSE connection management
  sseConnections: Record<string, SSEConnection>  // sessionId -> SSEConnection
  connectSSE: (sessionId: string, eventMode: string, onMessage: (msg: SSEEventMessage) => void, onStatus: (msg: SSEStatusMessage) => void) => void
  disconnectSSE: (sessionId: string) => void
  disconnectAllSSE: () => void

  // Helper methods
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
      lastScrollTop: 0,
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
      lastStreamingChunkIndex: {},

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
      
      addTabEvents: (sessionId: string, events: PollingEvent[]) => {
        set((state) => {
          const currentEvents = state.tabEvents[sessionId] || []

          // Deduplicate events by ID to prevent React key warnings and performance issues
          // Create a Set of existing event IDs for fast lookup
          const existingEventIds = new Set(currentEvents.map(e => e.id))

          // Filter out events that already exist
          const uniqueNewEvents = events.filter(event => {
            if (!event.id) {
              // If event has no ID, allow it (shouldn't happen, but be safe)
              logger.warn('EventStore', 'Event without ID detected:', event)
              return true
            }
            if (existingEventIds.has(event.id)) {
              // Event already exists, skip it
              return false
            }
            // New event, add its ID to the set and include it
            existingEventIds.add(event.id)
            return true
          })

          // Only log if duplicates were found (helps debug)
          if (uniqueNewEvents.length < events.length) {
            logger.debug('EventStore', `Deduplicated ${events.length - uniqueNewEvents.length} duplicate events for session ${sessionId}`)
          }

          const newEvents = [...currentEvents, ...uniqueNewEvents]

          // Trigger cleanup if threshold exceeded
          let finalEvents = newEvents
          if (newEvents.length >= CLEANUP_THRESHOLD) {
            logger.debug('Memory', `Cleaning up events for session ${sessionId}: ${newEvents.length} -> ${MAX_EVENTS}`)
            finalEvents = cleanupOldEvents(newEvents)
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
            updates.chatTabs = {
              ...state.chatTabs,
              [activeTab.tabId]: {
                ...activeTab,
                lastViewedEventCount: finalEvents.length,
                lastViewedEventCounts: computePerModeCounts(finalEvents)
              }
            }
          }

          return updates
        })
      },
      
      setTabEvents: (sessionId: string, events: PollingEvent[]) => {
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

      getTabLastEventIndex: (sessionId: string) => {
        const state = get()
        return state.tabEventIndices[sessionId] ?? -1
      },
      
      setTabLastEventIndex: (sessionId: string, index: number) => {
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

      setLastScrollTop: (scrollTop) => {
        set({ lastScrollTop: scrollTop })
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
        set((state) => {
          let lastIndex = state.lastStreamingChunkIndex[sessionId] ?? -1
          let currentText = state.streamingText[sessionId] || ''

          // Auto-reset if we see chunk 0 (start of new generation)
          if (chunkIndex === 0) {
             lastIndex = -1
             currentText = ''
          }

          // Deduplicate: skip chunks already processed (handles concurrent poll overlap)
          if (chunkIndex >= 0 && chunkIndex <= lastIndex) {
            return state
          }

          return {
            streamingText: {
              ...state.streamingText,
              [sessionId]: currentText + chunk
            },
            lastStreamingChunkIndex: {
              ...state.lastStreamingChunkIndex,
              [sessionId]: chunkIndex
            }
          }
        })
      },

      clearStreamingText: (sessionId: string) => {
        set((state) => {
          const newStreamingText = { ...state.streamingText }
          delete newStreamingText[sessionId]
          const newLastIdx = { ...state.lastStreamingChunkIndex }
          delete newLastIdx[sessionId]
          return { streamingText: newStreamingText, lastStreamingChunkIndex: newLastIdx }
        })
      },

      // Delegation streaming text actions
      appendDelegationStreamingChunk: (delegationId: string, chunkIndex: number, chunk: string) => {
        if (typeof chunk !== 'string' || !chunk) return
        set((state) => {
          let lastIndex = state.lastDelegationChunkIndex[delegationId] ?? -1
          let currentText = state.delegationStreamingText[delegationId] || ''

          // Auto-reset if we see chunk 0 (start of new generation)
          if (chunkIndex === 0) {
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
      connectSSE: (sessionId, eventMode, onMessage, onStatus) => {
        const state = get()
        // Close existing connection for this session if any
        if (state.sseConnections[sessionId]) {
          state.sseConnections[sessionId].close()
        }
        const lastIndex = state.tabEventIndices[sessionId] ?? 0
        const conn = new SSEConnection(sessionId, lastIndex, eventMode, {
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
          lastScrollTop: 0,
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
          lastStreamingChunkIndex: {},
          delegationStreamingText: {},
          lastDelegationChunkIndex: {}
        })
        
        // Clear the requiresNewChat flag after successful chat reset
        useAppStore.getState().clearRequiresNewChat()
      },

      isAtBottom: (element) => {
        const threshold = 50 // Increased threshold for more lenient detection
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
      createChatTab: async (name: string, metadata?: ChatTab['metadata'], existingObserverId?: string, eventMode?: 'advanced' | 'micro') => {
        // Generate unique tab ID
        const timestamp = Date.now()
        const mode = metadata?.mode || 'chat'
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
        
        // Use provided eventMode, or default to 'micro' for both workflow and chat
        let finalEventMode = eventMode
        if (!finalEventMode) {
          finalEventMode = 'micro'
        }
        
        // Create tab with session ID
        const tab: ChatTab = {
          tabId,
          name,
          sessionId: sessionIdForTab,
          isStreaming: false,
          isCompleted: false,
          hasRunningBgAgents: false,
          eventMode: finalEventMode,
          hideToolCalls: false,
          config: defaultConfig, // Initialize with default config from global state
          createdAt: timestamp,
          lastViewedEventCount: 0, // @deprecated - kept for backwards compat
          lastViewedEventCounts: { advanced: 0, micro: 0 }, // Initialize all modes to 0
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

          return {
            activeTabId: tabId,
            chatTabs: { ...state.chatTabs, ...updates }
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
        if (tab.sessionId) {
          logger.info('SessionStore', `Dismissing session ${tab.sessionId} (stopSession=${stopSession})`)
          agentApi.dismissSession(tab.sessionId).catch(error => {
            logger.error('SessionStore', `Failed to dismiss session ${tab.sessionId}:`, error)
          })
        } else {
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
      
      getTabsByMode: (mode: 'chat' | 'workflow') => {
        const state = get()
        return Object.values(state.chatTabs).filter(tab => tab.metadata?.mode === mode)
      },
      
      getTabsByPhaseId: (phaseId: string) => {
        const state = get()
        return Object.values(state.chatTabs).filter(
          tab => tab.metadata?.mode === 'workflow' && tab.metadata?.phaseId === phaseId
        )
      },
      
      setTabStreaming: (tabId: string, isStreaming: boolean) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return
        
        set(() => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: {
              ...tab,
              isStreaming
            }
          }
        }))
      },
      
      setTabCompleted: (tabId: string, isCompleted: boolean) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return
        
        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: {
              ...tab,
              isCompleted,
              isStreaming: isCompleted ? false : tab.isStreaming
            }
          }
        }))
      },

      setTabHasRunningBgAgents: (tabId: string, hasRunningBgAgents: boolean) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return

        set(() => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: {
              ...tab,
              hasRunningBgAgents
            }
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

          // Migrate events from old sessionId to new sessionId (if old session had events)
          // This preserves conversation history for chat/multi-agent when session ID changes
          if (oldSessionId && state.tabEvents[oldSessionId]) {
            const oldEvents = state.tabEvents[oldSessionId]
            const oldEventIndex = state.tabEventIndices[oldSessionId]
            const oldHasMore = state.tabHasMoreOlderEvents[oldSessionId]

            // Copy events to new sessionId
            updates.tabEvents = {
              ...state.tabEvents,
              [newSessionId]: oldEvents
            }

            // Copy event index
            if (oldEventIndex !== undefined) {
              updates.tabEventIndices = {
                ...state.tabEventIndices,
                [newSessionId]: oldEventIndex
              }
            }

            // Copy hasMoreOlderEvents flag
            if (oldHasMore !== undefined) {
              updates.tabHasMoreOlderEvents = {
                ...state.tabHasMoreOlderEvents,
                [newSessionId]: oldHasMore
              }
            }

            // Clean up old sessionId entries
            const newTabEvents = { ...updates.tabEvents || state.tabEvents }
            delete newTabEvents[oldSessionId]
            updates.tabEvents = newTabEvents

            const newTabEventIndices = { ...updates.tabEventIndices || state.tabEventIndices }
            delete newTabEventIndices[oldSessionId]
            updates.tabEventIndices = newTabEventIndices

            const newTabHasMore = { ...updates.tabHasMoreOlderEvents || state.tabHasMoreOlderEvents }
            delete newTabHasMore[oldSessionId]
            updates.tabHasMoreOlderEvents = newTabHasMore
          }

          return updates
        })
      },
      
      // updateTabObserverId removed - observers no longer used
      
      setTabEventMode: (tabId: string, eventMode: 'advanced' | 'micro') => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return
        
        // Check if mode actually changed
        const modeChanged = tab.eventMode !== eventMode
        
        set((state) => {
          const updates: Partial<ChatState> = {
            chatTabs: {
              ...state.chatTabs,
              [tabId]: {
                ...tab,
                eventMode
              }
            }
          }
          
          // CRITICAL: When event mode changes, reset lastEventIndex and clear events IMMEDIATELY
          // This forces the frontend to fetch all events from the beginning
          // because the filtered view is completely different (micro vs advanced)
          // The old events in the store are from the previous filter mode and are invalid
          // Clear events FIRST to ensure UI shows empty state immediately
          if (modeChanged && tab.sessionId) {
            // Clear events IMMEDIATELY - this ensures the UI shows empty state right away
            updates.tabEvents = {
              ...state.tabEvents,
              [tab.sessionId]: [] // Always set to empty array, even if it doesn't exist
            }
            // Reset lastEventIndex to -1 to trigger full reload
            updates.tabEventIndices = {
              ...state.tabEventIndices,
              [tab.sessionId]: -1
            }
            // Reset hasMoreOlderEvents flag
            updates.tabHasMoreOlderEvents = {
              ...state.tabHasMoreOlderEvents,
              [tab.sessionId]: false
            }
            // NOTE: SSE reconnection is handled by ChatArea's event mode change effect
            // which calls sseConn.reconnectWithMode(). Do NOT disconnect SSE here —
            // sseConnections isn't in the SSE effect's deps, so it can't re-create the connection.
          }

          return updates
        })
      },

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

      getTabConfig: (tabId: string) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        return tab?.config
      },
      
      setTabConfig: (tabId: string, configUpdate: Partial<ChatTabConfig>) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return
        
        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: {
              ...tab,
              config: {
                ...tab.config,
                ...configUpdate
              }
            }
          }
        }))
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
        
        const mode = tab.metadata?.mode || 'chat'
        
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
                // For 'chat' and 'multi-agent', fetch all non-workflow sessions;
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
          // Persist workflow, multi-agent, and chat tabs (for reconnection)
          chatTabs: Object.fromEntries(
            Object.entries(state.chatTabs)
              .filter(([, tab]) => tab.metadata?.mode === 'workflow' || tab.metadata?.mode === 'multi-agent' || tab.metadata?.mode === 'chat')
              .map(([tabId, tab]) => [
              tabId,
              {
                tabId: tab.tabId,
                name: tab.name,
                sessionId: tab.sessionId, // Persist session ID for direct restore on page refresh
                isStreaming: false, // Reset streaming state on reload
                isCompleted: false,
                hasRunningBgAgents: false,
                eventMode: tab.eventMode, // Persist user preference
                hideToolCalls: tab.hideToolCalls ?? false, // Persist user preference
                config: tab.config, // CRITICAL: Persist full config including:
                // - selectedServers (MCP server selections)
                // - llmConfig (LLM provider, model_id, fallback_models, etc.)
                // - useCodeExecutionMode (Simple vs Code Exec mode)
                // - fileContext (selected files/folders)
                // - inputText (chat input text)
                // - enableContextSummarization
                createdAt: tab.createdAt, // Persist for ordering
                lastViewedEventCount: 0, // @deprecated - Reset on reload
                lastViewedEventCounts: { advanced: 0, micro: 0 }, // Reset on reload
                metadata: tab.metadata // Persist mode and phase info
              }
            ])
          ),
          // Persist activeTabId for workflow, multi-agent, and chat tabs
          activeTabId: (() => {
            const activeTab = state.activeTabId ? state.chatTabs[state.activeTabId] : null
            return (activeTab?.metadata?.mode === 'workflow' || activeTab?.metadata?.mode === 'multi-agent' || activeTab?.metadata?.mode === 'chat') ? state.activeTabId : null
          })()
          // Exclude all other state (isStreaming, pollingInterval, tabEvents, etc.)
        }),
        onRehydrateStorage: () => (state) => {
          if (!state) return
          // Auto-select first tab if activeTabId is null but tabs exist
          if (!state.activeTabId) {
            const tabs = Object.values(state.chatTabs)
            if (tabs.length > 0) {
              const sorted = [...tabs].sort((a, b) => (a.createdAt || 0) - (b.createdAt || 0))
              // Use setState to trigger re-render (direct mutation won't)
              useChatStore.setState({ activeTabId: sorted[0].tabId })
            }
          }
        }
      }
    )
)
