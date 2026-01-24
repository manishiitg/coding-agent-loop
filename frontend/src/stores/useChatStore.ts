import { create } from 'zustand'
import { devtools, persist } from 'zustand/middleware'
import type { PollingEvent, ExtendedLLMConfiguration, SessionStatusResponse, ActiveSessionInfo, ChatHistorySummary } from '../services/api-types'
import type { StoreActions } from './types'
import type { FileContextItem } from './types'
import type { WorkflowPhase } from '../constants/workflow'
import { useAppStore } from './useAppStore'
import { agentApi } from '../services/api'
import { useMCPStore } from './useMCPStore'
import { useLLMStore } from './useLLMStore'
import { MAX_EVENTS_TO_PROCESS, CLEANUP_THRESHOLD } from '../constants/events'

// Active sessions cache TTL (30 seconds - shorter than polling interval to allow force refresh)
const ACTIVE_SESSIONS_CACHE_TTL = 30000
// Chat history cache TTL (5 minutes - chat history doesn't change as frequently)
const CHAT_HISTORY_CACHE_TTL = 300000

// Event memory management constants - use shared constants
const MAX_EVENTS = MAX_EVENTS_TO_PROCESS

// Helper function to identify important events that should always be retained
const shouldRetainEvent = (event: PollingEvent): boolean => {
  if (!event.type) return false
  
  const importantTypes = [
    'agent_error',
    'conversation_error',
    'orchestrator_error',
    'unified_completion',
    'conversation_end',
    'workflow_end',
    'request_human_feedback',
    'blocking_human_feedback',
    'orchestrator_end',
    'agent_end',
    'workflow_start'
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
  llmConfig: ExtendedLLMConfiguration  // LLM configuration (provider, model, etc.)
  fileContext: FileContextItem[]  // Files/folders in context
  enableContextSummarization?: boolean  // Context summarization setting
  enableWorkspaceAccess?: boolean  // Enable/disable workspace file access tools
  enableBrowserAccess?: boolean  // Enable/disable browser automation tool (auto-enables workspace when true)
  queuedMessages: string[]  // Queue of messages to send one by one when chat completes
  autoRun?: boolean  // Automatically run the chat when tab is loaded
}

// Generalized ChatTab interface (works for both chat and workflow modes)
export interface ChatTab {
  tabId: string  // Unique ID: `chat_${timestamp}` or `phase_${phaseId}_${timestamp}`
  name: string  // Display name (e.g., "Chat 1", "Planning", "Execution")
  sessionId: string | null  // Chat session ID if exists
  isStreaming: boolean  // Whether this tab's execution is currently running
  isCompleted: boolean  // Whether this tab's execution has completed
  eventMode: 'basic' | 'advanced' | 'tiny'  // Event display mode for this tab
  config: ChatTabConfig  // Tab-specific configuration
  createdAt: number  // Timestamp for ordering
  lastViewedEventCount: number  // Last event count when this tab was viewed (for badge)
  // Mode-specific metadata
  metadata?: {
    phaseId?: string  // For workflow mode: phase ID
    phaseName?: string  // For workflow mode: phase name
    mode?: 'chat' | 'workflow'  // Which mode this tab belongs to
    presetQueryId?: string  // For workflow mode: preset query ID (workflow identifier)
  }
}

// Helper function to get default tab config from current global state
// Uses mode-specific configs for LLM and server selections
const getDefaultTabConfig = (mode: 'chat' | 'workflow' = 'chat'): ChatTabConfig => {
  const mcpStore = useMCPStore?.getState?.()
  const llmStore = useLLMStore?.getState?.()

  // Get mode-specific server selection
  const selectedServers = mode === 'workflow'
    ? (mcpStore?.workflowSelectedServers || mcpStore?.selectedServers || [])
    : (mcpStore?.chatSelectedServers || mcpStore?.selectedServers || [])

  // Get mode-specific LLM config
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
  createChatTab: (name: string, metadata?: ChatTab['metadata'], existingObserverId?: string, eventMode?: 'basic' | 'advanced' | 'tiny') => Promise<string>  // Returns tabId
  switchTab: (tabId: string) => void
  closeTab: (tabId: string, stopSession?: boolean, keepEvents?: boolean) => Promise<void>
  getTab: (tabId: string) => ChatTab | undefined
  getActiveTab: () => ChatTab | undefined
  getTabsByMode: (mode: 'chat' | 'workflow') => ChatTab[]
  getTabsByPhaseId: (phaseId: string) => ChatTab[]  // Find workflow tabs by phaseId
  setTabStreaming: (tabId: string, isStreaming: boolean) => void
  setTabCompleted: (tabId: string, isCompleted: boolean) => void
  updateTabSessionId: (tabId: string, sessionId: string) => void
  setTabEventMode: (tabId: string, eventMode: 'basic' | 'advanced' | 'tiny') => void
  getTabConfig: (tabId: string) => ChatTabConfig | undefined
  setTabConfig: (tabId: string, configUpdate: Partial<ChatTabConfig>) => void
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
  
  // Helper methods
  resetChatState: () => void
  isAtBottom: (element: HTMLDivElement) => boolean
}

export const useChatStore = create<ChatState>()(
  devtools(
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
          console.log('[ChatStore] Polling already active, skipping start')
          return
        }
        
        console.log('[ChatStore] Starting polling interval')
        const interval = setInterval(() => {
          onPoll().catch(error => {
            console.error('[ChatStore] Error in polling callback:', error)
          })
        }, 2000)  // 2 seconds - reduced from 1s to improve performance
        
        set({ pollingInterval: interval })
      },
      
      // Stop polling
      stopPolling: () => {
        const state = get()
        if (state.pollingInterval) {
          console.log('[ChatStore] Stopping polling interval')
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
          console.log(`[ChatStore] Found ${activeTabs.length} active session(s), but polling not started yet. Call startPolling() with your poll callback.`)
          // Note: We can't start polling here because we need the poll callback
          // The component should call startPolling with the callback
        }
        // If there are no active sessions but polling is running, stop it
        else if (activeTabs.length === 0 && state.pollingInterval) {
          console.log(`[ChatStore] No active sessions, stopping polling`)
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
            console.log(`[MEMORY] Cleaning up events for session ${sessionId}: ${newEvents.length} -> ${MAX_EVENTS}`)
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
              console.warn(`[addTabEvents] Event without ID detected:`, event)
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
            console.log(`[addTabEvents] Deduplicated ${events.length - uniqueNewEvents.length} duplicate events for session ${sessionId}`)
          }

          const newEvents = [...currentEvents, ...uniqueNewEvents]
          
          // Trigger cleanup if threshold exceeded
          let finalEvents = newEvents
          if (newEvents.length >= CLEANUP_THRESHOLD) {
            console.log(`[MEMORY] Cleaning up events for session ${sessionId}: ${newEvents.length} -> ${MAX_EVENTS}`)
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
      
      setTabEvents: (sessionId: string, events: PollingEvent[]) => {
        set((state) => {
          // Trigger cleanup if threshold exceeded
          let finalEvents = events
          if (events.length >= CLEANUP_THRESHOLD) {
            console.log(`[MEMORY] Cleaning up events for session ${sessionId}: ${events.length} -> ${MAX_EVENTS}`)
            finalEvents = cleanupOldEvents(events)
          }
          
          return {
            tabEvents: {
              ...state.tabEvents,
              [sessionId]: finalEvents
            }
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

          console.log(`[ChatStore] Cleaned up events for ${sessionId}: ${events.length} -> ${cleaned.length}`)

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

      // Helper methods
      resetChatState: () => {
        const state = get()
        
        // Close all tabs and stop sessions
        Object.values(state.chatTabs).forEach(async (tab) => {
          try {
            if (tab.isStreaming && tab.sessionId) {
              await agentApi.stopSession(tab.sessionId)
            }
          } catch (error) {
            console.error(`[ChatStore] Error cleaning up tab ${tab.tabId}:`, error)
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
          activeTabId: null
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
      createChatTab: async (name: string, metadata?: ChatTab['metadata'], existingObserverId?: string, eventMode?: 'basic' | 'advanced' | 'tiny') => {
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
          console.log(`[ChatStore] Using existing session ID for tab ${tabId}: ${sessionIdForTab}`)
        } else {
          // Generate new session ID
          sessionIdForTab = globalThis.crypto.randomUUID()
          console.log(`[ChatStore] Generated new session ID for tab ${tabId}: ${sessionIdForTab}`)
        }
        
        // Get default config from current global state (mode-specific)
        const defaultConfig = getDefaultTabConfig(mode)
        
        // Validate session ID before creating tab
        if (!sessionIdForTab || sessionIdForTab.trim() === '') {
          console.error(`[ChatStore] ❌ Cannot create tab ${tabId} - sessionId is empty!`)
          throw new Error('Session ID is required but was not provided or is empty')
        }
        
        // Use provided eventMode, or default based on mode:
        // chat mode -> 'tiny', workflow mode -> 'basic'
        const finalEventMode = eventMode || (mode === 'chat' ? 'tiny' : 'basic')
        
        // Create tab with session ID
        const tab: ChatTab = {
          tabId,
          name,
          sessionId: sessionIdForTab,
          isStreaming: false,
          isCompleted: false,
          eventMode: finalEventMode,
          config: defaultConfig, // Initialize with default config from global state
          createdAt: timestamp,
          lastViewedEventCount: 0, // Initialize to 0 (no events viewed yet)
          metadata
        }
        
        console.log(`[ChatStore] Creating tab with session ID:`, {
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
          console.error(`[ChatStore] ❌ Tab ${tabId} was not stored in state!`)
          throw new Error('Failed to store tab in state')
        }
        if (!storedTab.sessionId) {
          console.error(`[ChatStore] ❌ Tab ${tabId} stored but has no sessionId!`, storedTab)
          throw new Error('Tab stored without session ID')
        }
        
        console.log(`[ChatStore] ✅ Tab ${tabId} created and stored successfully with session ID: ${storedTab.sessionId}`)
        return tabId
      },
      
      switchTab: (tabId: string) => {
        const state = get()
        if (!state.chatTabs[tabId]) {
          console.warn(`[ChatStore] Tab ${tabId} not found`)
          return
        }
        
        // Update previous active tab's lastViewedEventCount before switching
        if (state.activeTabId && state.activeTabId !== tabId) {
          const previousTabId = state.activeTabId
          const previousTab = state.chatTabs[previousTabId]
          if (previousTab?.sessionId) {
            const currentEventCount = state.tabEvents[previousTab.sessionId]?.length || 0
            set((s) => ({
              chatTabs: {
                ...s.chatTabs,
                [previousTabId]: {
                  ...previousTab,
                  lastViewedEventCount: currentEventCount
                }
              }
            }))
          }
        }
        
        // Switch to new tab and update its lastViewedEventCount
        const newTab = state.chatTabs[tabId]
        if (newTab?.sessionId) {
          const currentEventCount = state.tabEvents[newTab.sessionId]?.length || 0
          set((s) => ({
            activeTabId: tabId,
            chatTabs: {
              ...s.chatTabs,
              [tabId]: {
                ...newTab,
                lastViewedEventCount: currentEventCount
              }
            }
          }))
        } else {
          set({ activeTabId: tabId })
        }
      },
      
      closeTab: async (tabId: string, stopSession: boolean = true, keepEvents: boolean = false) => {
        const state = get()
        const tab = state.chatTabs[tabId]

        if (!tab) {
          console.warn(`[ChatStore] Tab ${tabId} not found`)
          return
        }

        // Stop session if streaming (unless explicitly disabled, e.g., when minimizing to background)
        if (stopSession && tab.isStreaming && tab.sessionId) {
          try {
            await agentApi.stopSession(tab.sessionId)
          } catch (error) {
            console.error(`[ChatStore] Failed to stop session ${tab.sessionId}:`, error)
          }
        }

        // Clear tab's events (by sessionId) unless keepEvents is true (e.g., for background workflows)
        let newTabEvents = state.tabEvents
        let newTabEventIndices = state.tabEventIndices
        if (!keepEvents && tab.sessionId) {
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

        set({
          chatTabs: newTabs,
          activeTabId: newActiveTabId,
          tabEvents: newTabEvents,
          tabEventIndices: newTabEventIndices
        })
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
      
      updateTabSessionId: (tabId: string, sessionId: string) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return
        
        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: {
              ...tab,
              sessionId
            }
          }
        }))
      },
      
      // updateTabObserverId removed - observers no longer used
      
      setTabEventMode: (tabId: string, eventMode: 'basic' | 'advanced' | 'tiny') => {
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
          // because the filtered view is completely different (basic vs advanced vs tiny)
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
          }
          
          return updates
        })
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
          console.warn(`[ChatStore] Cannot fetch session status - tab ${tabId} has no session ID`)
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
            console.log(`[ChatStore] Session ${tab.sessionId} not found (404) for tab ${tabId}`)
          } else {
            console.warn(`[ChatStore] Failed to fetch session status for tab ${tabId}:`, axiosError.message || 'Unknown error')
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
            console.log('[ChatStore] No active sessions found, skipping status fetch')
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
            console.log('[ChatStore] No tabs with active sessions to fetch status for')
            return
          }
          
          console.log(`[ChatStore] Fetching status for ${tabsToFetch.length} tabs (${activeSessionIds.size} active sessions)`)
          
          // Fetch status for tabs in parallel
          const { fetchTabSessionStatus } = get()
          const promises = tabsToFetch.map(tabId => fetchTabSessionStatus(tabId))
          await Promise.all(promises)
        } catch (error) {
          console.warn('[ChatStore] Failed to check active sessions, falling back to fetching all tab statuses:', error)
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
          console.error('[ChatStore] Failed to fetch active sessions:', error)
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
          console.error('[ChatStore] Failed to fetch active sessions on polling start:', error)
        })
        
        // Poll every 60 seconds
        const interval = setInterval(() => {
          const { getActiveSessions } = get()
          getActiveSessions(true).catch(error => {
            console.error('[ChatStore] Failed to fetch active sessions during polling:', error)
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
          console.log('[ChatStore] Returning cached chat history for mode:', modeCategory)
          return state.chatHistoryCache
        }
        
        // Fetch fresh data - load only 10 initially
        try {
          console.log('[ChatStore] Fetching fresh chat history for mode:', modeCategory, '(initial load: 10 sessions)')
          const response = await agentApi.getChatSessions(10, 0)
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
          console.error('[ChatStore] Failed to fetch chat history:', error)
          // Return cached data even if stale on error
          return state.chatHistoryCache
        }
      },
      
      loadMoreChatHistory: async (modeCategory: string): Promise<ChatHistorySummary[]> => {
        const state = get()
        
        // Check if we have more to load
        if (state.chatHistoryTotalCount === null || state.chatHistoryLoadedCount >= state.chatHistoryTotalCount) {
          console.log('[ChatStore] No more chat history to load')
          return state.chatHistoryCache
        }
        
        // Load next 10 sessions
        try {
          console.log('[ChatStore] Loading more chat history for mode:', modeCategory, `(offset: ${state.chatHistoryLoadedCount}, limit: 10)`)
          const response = await agentApi.getChatSessions(10, state.chatHistoryLoadedCount)
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
          console.error('[ChatStore] Failed to load more chat history:', error)
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
          // Only persist workflow tabs (for reconnection), not chat tabs (ephemeral)
          chatTabs: Object.fromEntries(
            Object.entries(state.chatTabs)
              .filter(([, tab]) => tab.metadata?.mode === 'workflow') // Only persist workflow tabs
              .map(([tabId, tab]) => [
              tabId,
              {
                tabId: tab.tabId,
                name: tab.name,
                sessionId: null, // Don't persist session IDs (sessions are ephemeral)
                isStreaming: false, // Reset streaming state on reload
                isCompleted: false, // Reset completion state on reload
                eventMode: tab.eventMode, // Persist user preference
                config: tab.config, // CRITICAL: Persist full config including:
                // - selectedServers (MCP server selections)
                // - llmConfig (LLM provider, model_id, fallback_models, etc.)
                // - useCodeExecutionMode (Simple vs Code Exec mode)
                // - fileContext (selected files/folders)
                // - inputText (chat input text)
                // - enableContextSummarization
                createdAt: tab.createdAt, // Persist for ordering
                lastViewedEventCount: 0, // Reset on reload
                metadata: tab.metadata // Persist mode and phase info
              }
            ])
          ),
          // Only persist activeTabId if it's a workflow tab
          activeTabId: (() => {
            const activeTab = state.activeTabId ? state.chatTabs[state.activeTabId] : null
            return activeTab?.metadata?.mode === 'workflow' ? state.activeTabId : null
          })()
          // Exclude all other state (isStreaming, pollingInterval, tabEvents, etc.)
        })
      }
    ),
    {
      name: 'chat-store'
    }
  )
)
