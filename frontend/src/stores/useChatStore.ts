import { create } from 'zustand'
import { devtools } from 'zustand/middleware'
import type { PollingEvent, ExtendedLLMConfiguration, SessionStatusResponse } from '../services/api-types'
import type { StoreActions } from './types'
import type { FileContextItem } from './types'
import type { WorkflowPhase } from '../constants/workflow'
import { useAppStore } from './useAppStore'
import { agentApi } from '../services/api'
import { useMCPStore } from './useMCPStore'
import { useLLMStore } from './useLLMStore'

// Event memory management constants
const MAX_EVENTS = 1000
const CLEANUP_THRESHOLD = 1200

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
  selectedServers: string[]  // Selected MCP servers
  llmConfig: ExtendedLLMConfiguration  // LLM configuration (provider, model, etc.)
  fileContext: FileContextItem[]  // Files/folders in context
  enableContextSummarization?: boolean  // Context summarization setting
}

// Generalized ChatTab interface (works for both chat and workflow modes)
export interface ChatTab {
  tabId: string  // Unique ID: `chat_${timestamp}` or `phase_${phaseId}_${timestamp}`
  name: string  // Display name (e.g., "Chat 1", "Planning", "Execution")
  observerId: string  // Unique observer ID for this tab
  sessionId: string | null  // Chat session ID if exists
  isStreaming: boolean  // Whether this tab's execution is currently running
  isCompleted: boolean  // Whether this tab's execution has completed
  eventMode: 'basic' | 'advanced'  // Event display mode for this tab
  config: ChatTabConfig  // Tab-specific configuration
  createdAt: number  // Timestamp for ordering
  // Mode-specific metadata
  metadata?: {
    phaseId?: string  // For workflow mode: phase ID
    phaseName?: string  // For workflow mode: phase name
    mode?: 'chat' | 'workflow'  // Which mode this tab belongs to
  }
}

// Helper function to get default tab config from current global state
const getDefaultTabConfig = (): ChatTabConfig => {
  const appStore = useAppStore.getState()
  const mcpStore = useMCPStore?.getState?.()
  const llmStore = useLLMStore?.getState?.()
  
  return {
    inputText: '',
    useCodeExecutionMode: appStore.useCodeExecutionMode || false,
    selectedServers: mcpStore?.selectedServers || [],
    llmConfig: llmStore?.primaryConfig || {
      provider: 'openrouter',
      model_id: '',
      fallback_models: [],
      cross_provider_fallback: undefined
    },
    fileContext: appStore.chatFileContext || [],
    enableContextSummarization: false
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
  observerId: string
  lastEventIndex: number
  pollingInterval: NodeJS.Timeout | null
  currentlyPolledObserverId: string | null  // Track which observer ID is currently being polled
  
  // Event tracking (global - for backward compatibility)
  totalEvents: number
  lastEventCount: number
  events: PollingEvent[]  // Deprecated: use tabEvents instead
  // Per-tab event storage (keyed by observer ID)
  tabEvents: Record<string, PollingEvent[]>  // observerId -> events
  tabEventIndices: Record<string, number>  // observerId -> lastEventIndex
  
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
  
  // Actions
  setIsStreaming: (streaming: boolean) => void
  // Computed: Derive isStreaming from polling status
  // Use this selector: useChatStore(state => state.getIsStreaming())
  getIsStreaming: () => boolean
  setObserverId: (id: string) => void
  setLastEventIndex: (index: number) => void
  setPollingInterval: (interval: NodeJS.Timeout | null) => void
  setCurrentlyPolledObserverId: (observerId: string | null) => void
  
  // Event actions
  setTotalEvents: (count: number) => void
  setLastEventCount: (count: number) => void
  setEvents: (events: PollingEvent[] | ((prevEvents: PollingEvent[]) => PollingEvent[])) => void
  addEvent: (event: PollingEvent) => void
  clearEvents: () => void
  // Per-tab event actions
  getTabEvents: (observerId: string) => PollingEvent[]
  addTabEvent: (observerId: string, event: PollingEvent) => void
  addTabEvents: (observerId: string, events: PollingEvent[]) => void
  setTabEvents: (observerId: string, events: PollingEvent[]) => void
  clearTabEvents: (observerId: string) => void
  getTabLastEventIndex: (observerId: string) => number
  setTabLastEventIndex: (observerId: string, index: number) => void
  
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
  createChatTab: (name: string, metadata?: ChatTab['metadata'], existingObserverId?: string) => Promise<string>  // Returns tabId
  switchTab: (tabId: string) => void
  closeTab: (tabId: string) => Promise<void>
  getTab: (tabId: string) => ChatTab | undefined
  getActiveTab: () => ChatTab | undefined
  getTabsByMode: (mode: 'chat' | 'workflow') => ChatTab[]
  setTabStreaming: (tabId: string, isStreaming: boolean) => void
  setTabCompleted: (tabId: string, isCompleted: boolean) => void
  updateTabSessionId: (tabId: string, sessionId: string) => void
  updateTabObserverId: (tabId: string, observerId: string) => void
  setTabEventMode: (tabId: string, eventMode: 'basic' | 'advanced') => void
  getTabConfig: (tabId: string) => ChatTabConfig | undefined
  setTabConfig: (tabId: string, configUpdate: Partial<ChatTabConfig>) => void
  getTabStreamingStatus: (tabId: string) => boolean
  checkTabCompletion: (tabId: string, events: Array<{ type: string }>) => boolean
  
  // Tab session status actions
  fetchTabSessionStatus: (tabId: string) => Promise<void>
  fetchAllTabSessionStatuses: (tabIds: string[]) => Promise<void>
  getTabSessionStatus: (tabId: string) => TabSessionStatus | undefined
  
  // Helper methods
  resetChatState: () => void
  isAtBottom: (element: HTMLDivElement) => boolean
}

export const useChatStore = create<ChatState>()(
  devtools(
    (set, get) => ({
      // Initial state
      isStreaming: false,
      observerId: '',
      lastEventIndex: -1,
      pollingInterval: null,
      currentlyPolledObserverId: null,
      totalEvents: 0,
      lastEventCount: 0,
      events: [],
      tabEvents: {},
      tabEventIndices: {},
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

      setObserverId: (id) => {
        set({ observerId: id })
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
      
      setCurrentlyPolledObserverId: (observerId) => {
        set({ currentlyPolledObserverId: observerId })
      },

      // Event actions
      setTotalEvents: (count) => {
        set({ totalEvents: count })
      },

      setLastEventCount: (count) => {
        set({ lastEventCount: count })
      },

      setEvents: (events) => {
        if (typeof events === 'function') {
          set((state) => {
            let newEvents = events(state.events)
            
            // Trigger cleanup if threshold exceeded
            if (newEvents.length >= CLEANUP_THRESHOLD) {
              console.log(`[MEMORY] Cleaning up events: ${newEvents.length} -> ${MAX_EVENTS}`)
              newEvents = cleanupOldEvents(newEvents)
            }
            
            return { events: newEvents }
          })
        } else {
          // Trigger cleanup if threshold exceeded
          let finalEvents = events
          if (events.length >= CLEANUP_THRESHOLD) {
            console.log(`[MEMORY] Cleaning up events: ${events.length} -> ${MAX_EVENTS}`)
            finalEvents = cleanupOldEvents(events)
          }
          set({ events: finalEvents })
        }
      },

      addEvent: (event) => {
        set((state) => ({
          events: [...state.events, event],
          totalEvents: state.totalEvents + 1
        }))
      },

      clearEvents: () => {
        set({ events: [], totalEvents: 0, lastEventCount: 0 })
      },
      
      // Per-tab event actions
      getTabEvents: (observerId: string) => {
        const state = get()
        return state.tabEvents[observerId] || []
      },
      
      addTabEvent: (observerId: string, event: PollingEvent) => {
        set((state) => {
          const currentEvents = state.tabEvents[observerId] || []
          const newEvents = [...currentEvents, event]
          
          // Trigger cleanup if threshold exceeded
          let finalEvents = newEvents
          if (newEvents.length >= CLEANUP_THRESHOLD) {
            console.log(`[MEMORY] Cleaning up events for observer ${observerId}: ${newEvents.length} -> ${MAX_EVENTS}`)
            finalEvents = cleanupOldEvents(newEvents)
          }
          
          return {
            tabEvents: {
              ...state.tabEvents,
              [observerId]: finalEvents
            },
            totalEvents: state.totalEvents + 1
          }
        })
      },
      
      addTabEvents: (observerId: string, events: PollingEvent[]) => {
        set((state) => {
          const currentEvents = state.tabEvents[observerId] || []
          const newEvents = [...currentEvents, ...events]
          
          // Trigger cleanup if threshold exceeded
          let finalEvents = newEvents
          if (newEvents.length >= CLEANUP_THRESHOLD) {
            console.log(`[MEMORY] Cleaning up events for observer ${observerId}: ${newEvents.length} -> ${MAX_EVENTS}`)
            finalEvents = cleanupOldEvents(newEvents)
          }
          
          return {
            tabEvents: {
              ...state.tabEvents,
              [observerId]: finalEvents
            },
            totalEvents: state.totalEvents + events.length
          }
        })
      },
      
      setTabEvents: (observerId: string, events: PollingEvent[]) => {
        set((state) => {
          // Trigger cleanup if threshold exceeded
          let finalEvents = events
          if (events.length >= CLEANUP_THRESHOLD) {
            console.log(`[MEMORY] Cleaning up events for observer ${observerId}: ${events.length} -> ${MAX_EVENTS}`)
            finalEvents = cleanupOldEvents(events)
          }
          
          return {
            tabEvents: {
              ...state.tabEvents,
              [observerId]: finalEvents
            }
          }
        })
      },
      
      clearTabEvents: (observerId: string) => {
        set((state) => {
          const newTabEvents = { ...state.tabEvents }
          delete newTabEvents[observerId]
          const newTabEventIndices = { ...state.tabEventIndices }
          delete newTabEventIndices[observerId]
          
          return {
            tabEvents: newTabEvents,
            tabEventIndices: newTabEventIndices
          }
        })
      },
      
      getTabLastEventIndex: (observerId: string) => {
        const state = get()
        return state.tabEventIndices[observerId] ?? -1
      },
      
      setTabLastEventIndex: (observerId: string, index: number) => {
        set((state) => ({
          tabEventIndices: {
            ...state.tabEventIndices,
            [observerId]: index
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
        
        // Close all tabs and remove observers
        Object.values(state.chatTabs).forEach(async (tab) => {
          try {
            await agentApi.removeObserver(tab.observerId)
            if (tab.isStreaming && tab.sessionId) {
              await agentApi.stopSession(tab.sessionId)
            }
          } catch (error) {
            console.error(`[ChatStore] Error cleaning up tab ${tab.tabId}:`, error)
          }
        })
        
        set({
          isStreaming: false,
          observerId: '',
          lastEventIndex: -1,
          pollingInterval: null,
          currentlyPolledObserverId: null,
          totalEvents: 0,
          lastEventCount: 0,
          events: [],
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
      createChatTab: async (name: string, metadata?: ChatTab['metadata'], existingObserverId?: string) => {
        // Generate unique tab ID
        const timestamp = Date.now()
        const mode = metadata?.mode || 'chat'
        const tabId = mode === 'workflow' && metadata?.phaseId
          ? `phase_${metadata.phaseId}_${timestamp}`
          : `chat_${timestamp}`
        
        // Use existing observer ID if provided, otherwise register a new one
        let observerId: string
        let sessionIdForTab: string | null = null
        
        if (existingObserverId) {
          observerId = existingObserverId
          console.log(`[ChatStore] Using existing observer ID for tab ${tabId}: ${observerId}`)
          // When using existing observer ID, session ID will be set separately via updateTabSessionId
        } else {
          try {
            console.log(`[ChatStore] Registering new observer for tab ${tabId}...`)
            // Generate session ID for the new tab
            sessionIdForTab = globalThis.crypto.randomUUID()
            const observerResponse = await agentApi.registerObserver(sessionIdForTab)
            
            console.log(`[ChatStore] Observer response received:`, {
              hasResponse: !!observerResponse,
              hasObserverId: !!observerResponse?.observer_id,
              observerId: observerResponse?.observer_id,
              fullResponse: observerResponse
            })
            
            if (!observerResponse?.observer_id) {
              console.error(`[ChatStore] ❌ Invalid observer response:`, observerResponse)
              throw new Error('No observer_id received from registerObserver response')
            }
            
            observerId = observerResponse.observer_id
            console.log(`[ChatStore] ✅ Registered new observer for tab ${tabId}: ${observerId}`)
          } catch (error) {
            console.error(`[ChatStore] ❌ Failed to register observer for tab ${tabId}:`, error)
            if (error instanceof Error) {
              console.error(`[ChatStore] Error details:`, {
                name: error.name,
                message: error.message,
                stack: error.stack
              })
            }
            throw new Error(`Failed to create observer for tab: ${error instanceof Error ? error.message : String(error)}`)
          }
        }
        
        // Get default config from current global state
        const defaultConfig = getDefaultTabConfig()
        
        // Validate observer ID before creating tab
        if (!observerId || observerId.trim() === '') {
          console.error(`[ChatStore] ❌ Cannot create tab ${tabId} - observerId is empty!`)
          throw new Error('Observer ID is required but was not provided or is empty')
        }
        
        // Create tab with session ID if we generated one
        const tab: ChatTab = {
          tabId,
          name,
          observerId,
          sessionId: sessionIdForTab,
          isStreaming: false,
          isCompleted: false,
          eventMode: 'basic', // Default to basic mode
          config: defaultConfig, // Initialize with default config from global state
          createdAt: timestamp,
          metadata
        }
        
        console.log(`[ChatStore] Creating tab with observer ID:`, {
          tabId,
          name,
          observerId,
          hasObserverId: !!observerId
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
        if (!storedTab.observerId) {
          console.error(`[ChatStore] ❌ Tab ${tabId} stored but has no observerId!`, storedTab)
          throw new Error('Tab stored without observer ID')
        }
        
        console.log(`[ChatStore] ✅ Tab ${tabId} created and stored successfully with observer ID: ${storedTab.observerId}`)
        return tabId
      },
      
      switchTab: (tabId: string) => {
        const state = get()
        if (!state.chatTabs[tabId]) {
          console.warn(`[ChatStore] Tab ${tabId} not found`)
          return
        }
        set({ activeTabId: tabId })
      },
      
      closeTab: async (tabId: string) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        
        if (!tab) {
          console.warn(`[ChatStore] Tab ${tabId} not found`)
          return
        }
        
        // Remove observer from backend
        try {
          await agentApi.removeObserver(tab.observerId)
        } catch (error) {
          console.error(`[ChatStore] Failed to remove observer ${tab.observerId}:`, error)
        }
        
        // Stop session if streaming
        if (tab.isStreaming && tab.sessionId) {
          try {
            await agentApi.stopSession(tab.sessionId)
          } catch (error) {
            console.error(`[ChatStore] Failed to stop session ${tab.sessionId}:`, error)
          }
        }
        
        // Clear tab's events
        const newTabEvents = { ...state.tabEvents }
        delete newTabEvents[tab.observerId]
        const newTabEventIndices = { ...state.tabEventIndices }
        delete newTabEventIndices[tab.observerId]
        
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
      
      updateTabObserverId: (tabId: string, observerId: string) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return
        
        const oldObserverId = tab.observerId
        
        // CRITICAL: Check if another tab is already using this observer ID
        // If so, this is a bug - each tab should have a unique observer ID
        const tabsUsingNewObserverId = Object.values(state.chatTabs)
          .filter(t => t.observerId === observerId && t.tabId !== tabId)
        
        if (tabsUsingNewObserverId.length > 0) {
          console.error(
            `[ChatStore] WARNING: Observer ID ${observerId} is already used by other tabs:`,
            tabsUsingNewObserverId.map(t => t.tabId),
            `. This will cause events to mix between tabs!`
          )
        }
        
        // If observer ID is changing, migrate events from old ID to new ID
        if (oldObserverId !== observerId) {
          console.log(`[ChatStore] Migrating events for tab ${tabId} from observer ${oldObserverId} to ${observerId}`)
          const oldEvents = state.tabEvents[oldObserverId] || []
          const oldEventIndex = state.tabEventIndices[oldObserverId] ?? -1
          
          // CRITICAL: Don't merge with existing events if another tab is using the new observer ID
          // This prevents events from mixing between tabs
          const existingEvents = state.tabEvents[observerId] || []
          
          // Only merge if no other tab is using this observer ID
          // Otherwise, overwrite to prevent cross-tab event mixing
          let finalEvents: PollingEvent[]
          let finalEventIndex: number
          
          if (tabsUsingNewObserverId.length > 0) {
            // Another tab is using this observer ID - don't merge, just use old events
            // This prevents mixing events from different tabs
            console.warn(
              `[ChatStore] Another tab is using observer ID ${observerId}, not merging events to prevent cross-tab mixing`
            )
            finalEvents = oldEvents
            finalEventIndex = oldEventIndex
          } else {
            // No other tab using this ID - safe to merge
            finalEvents = [...oldEvents, ...existingEvents]
            finalEventIndex = Math.max(oldEventIndex, state.tabEventIndices[observerId] ?? -1)
          }
          
          set((state) => {
            const newTabEvents = { ...state.tabEvents }
            const newTabEventIndices = { ...state.tabEventIndices }
            
            // Move events to new observer ID
            if (finalEvents.length > 0) {
              newTabEvents[observerId] = finalEvents
              newTabEventIndices[observerId] = finalEventIndex
            }
            
            // Check if old observer ID is still used by other tabs before cleaning up
            const otherTabsUsingOldId = Object.values(state.chatTabs)
              .filter(t => t.observerId === oldObserverId && t.tabId !== tabId)
            
            // Only remove old observer ID events if no other tabs are using it
            if (otherTabsUsingOldId.length === 0 && oldObserverId !== observerId) {
              delete newTabEvents[oldObserverId]
              delete newTabEventIndices[oldObserverId]
            }
            
            return {
              chatTabs: {
                ...state.chatTabs,
                [tabId]: {
                  ...tab,
                  observerId
                }
              },
              tabEvents: newTabEvents,
              tabEventIndices: newTabEventIndices
            }
          })
        } else {
          // No change, just update the tab
          set((state) => ({
            chatTabs: {
              ...state.chatTabs,
              [tabId]: {
                ...tab,
                observerId
              }
            }
          }))
        }
      },
      
      setTabEventMode: (tabId: string, eventMode: 'basic' | 'advanced') => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return
        
        set((state) => ({
          chatTabs: {
            ...state.chatTabs,
            [tabId]: {
              ...tab,
              eventMode
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
      
      getTabStreamingStatus: (tabId: string) => {
        const state = get()
        const tab = state.chatTabs[tabId]
        if (!tab) return false
        
        // If tab is marked as completed, it's not streaming
        if (tab.isCompleted) return false
        
        // Tab is streaming if:
        // 1. Polling is active
        // 2. This tab's observer ID matches the currently polled observer
        // 3. Not manually paused (stored isStreaming !== false)
        const isPolling = state.pollingInterval !== null
        const isThisTabPolled = state.currentlyPolledObserverId === tab.observerId
        
        if (isPolling && isThisTabPolled) {
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
        
        if (!tab || !tab.observerId) {
          console.warn(`[ChatStore] Cannot fetch session status - tab ${tabId} has no observer ID`)
          return
        }
        
        try {
          // First get observer status to get session_id
          const observerStatus = await agentApi.getObserverStatus(tab.observerId)
          
          let status: TabSessionStatus = {
            status: null,
            agentMode: null,
            lastActivity: null
          }
          
          if (observerStatus.session_id) {
            // Check if this session is in the active sessions list before calling getSessionStatus
            // This avoids unnecessary API calls when there are no active sessions
            try {
              const activeSessionsResponse = await agentApi.getActiveSessions()
              const activeSessionIds = new Set(activeSessionsResponse.active_sessions.map(s => s.session_id))
              
              // Only call getSessionStatus if the session is active
              if (activeSessionIds.has(observerStatus.session_id)) {
                try {
                  // Then get session status
                  const sessionStatus: SessionStatusResponse = await agentApi.getSessionStatus(observerStatus.session_id)
                  status = {
                    status: sessionStatus.status || null,
                    agentMode: sessionStatus.agent_mode || observerStatus.agent_mode || null,
                    lastActivity: sessionStatus.last_activity || observerStatus.last_activity || null
                  }
                } catch (sessionError: unknown) {
                  // Handle 404 or other session status errors gracefully
                  const axiosError = sessionError as { response?: { status?: number }; message?: string }
                  if (axiosError?.response?.status === 404) {
                    // Session not found - use observer status only
                    status = {
                      status: observerStatus.status || null,
                      agentMode: observerStatus.agent_mode || null,
                      lastActivity: observerStatus.last_activity || null
                    }
                  } else {
                    // Other error, use observer status as fallback
                    status = {
                      status: observerStatus.status || null,
                      agentMode: observerStatus.agent_mode || null,
                      lastActivity: observerStatus.last_activity || null
                    }
                  }
                }
              } else {
                // Session is not active, use observer status only (don't call getSessionStatus)
                status = {
                  status: observerStatus.status || null,
                  agentMode: observerStatus.agent_mode || null,
                  lastActivity: observerStatus.last_activity || null
                }
              }
            } catch {
              // If active sessions check fails, fall back to calling getSessionStatus
              try {
                const sessionStatus: SessionStatusResponse = await agentApi.getSessionStatus(observerStatus.session_id)
                status = {
                  status: sessionStatus.status || null,
                  agentMode: sessionStatus.agent_mode || observerStatus.agent_mode || null,
                  lastActivity: sessionStatus.last_activity || observerStatus.last_activity || null
                }
              } catch (sessionError: unknown) {
                // Handle 404 or other session status errors gracefully
                const axiosError = sessionError as { response?: { status?: number }; message?: string }
                if (axiosError?.response?.status === 404) {
                  // Session not found - use observer status only
                  status = {
                    status: observerStatus.status || null,
                    agentMode: observerStatus.agent_mode || null,
                    lastActivity: observerStatus.last_activity || null
                  }
                } else {
                  // Other error, use observer status as fallback
                  status = {
                    status: observerStatus.status || null,
                    agentMode: observerStatus.agent_mode || null,
                    lastActivity: observerStatus.last_activity || null
                  }
                }
              }
            }
          } else {
            // No session yet, but we have agent_mode from observer
            status = {
              status: observerStatus.status || null,
              agentMode: observerStatus.agent_mode || null,
              lastActivity: observerStatus.last_activity || null
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
          // Handle observer status errors gracefully
          const axiosError = error as { response?: { status?: number }; message?: string }
          if (axiosError?.response?.status === 404) {
            // Observer not found - it may have been cleaned up
            console.log(`[ChatStore] Observer ${tab.observerId} not found (404) for tab ${tabId}`)
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
        // First check if there are any active sessions
        try {
          const activeSessionsResponse = await agentApi.getActiveSessions()
          const activeSessionIds = new Set(activeSessionsResponse.active_sessions.map(s => s.session_id))
          
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
      }
    }),
    {
      name: 'chat-store'
    }
  )
)
