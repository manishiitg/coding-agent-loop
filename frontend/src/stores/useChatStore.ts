import { create } from 'zustand'
import { devtools } from 'zustand/middleware'
import type { PollingEvent } from '../services/api-types'
import type { StoreActions } from './types'
import type { WorkflowPhase } from '../constants/workflow'
import { useAppStore } from './useAppStore'

interface ChatState extends StoreActions {
  // Chat streaming state
  isStreaming: boolean
  observerId: string
  lastEventIndex: number
  pollingInterval: NodeJS.Timeout | null
  
  // Event tracking
  totalEvents: number
  lastEventCount: number
  events: PollingEvent[]
  
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
  
  // Actions
  setIsStreaming: (streaming: boolean) => void
  setObserverId: (id: string) => void
  setLastEventIndex: (index: number) => void
  setPollingInterval: (interval: NodeJS.Timeout | null) => void
  
  // Event actions
  setTotalEvents: (count: number) => void
  setLastEventCount: (count: number) => void
  setEvents: (events: PollingEvent[] | ((prevEvents: PollingEvent[]) => PollingEvent[])) => void
  addEvent: (event: PollingEvent) => void
  clearEvents: () => void
  
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
      lastEventIndex: 0,
      pollingInterval: null,
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
      currentWorkflowPhase: 'pre-verification' as WorkflowPhase,
      currentWorkflowQueryId: null,
      toasts: [],

      // Actions
      setIsStreaming: (streaming) => {
        set({ isStreaming: streaming })
      },

      setObserverId: (id) => {
        set({ observerId: id })
      },

      setLastEventIndex: (index) => {
        set({ lastEventIndex: index })
      },

      setPollingInterval: (interval) => {
        set({ pollingInterval: interval })
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
            const newEvents = events(state.events)
            return { events: newEvents }
          })
        } else {
          set({ events })
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
        set({
          isStreaming: false,
          observerId: '',
          lastEventIndex: 0,
          pollingInterval: null,
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
          currentWorkflowPhase: 'pre-verification' as WorkflowPhase,
          currentWorkflowQueryId: null,
          toasts: []
        })
        
        // Clear the requiresNewChat flag after successful chat reset
        useAppStore.getState().clearRequiresNewChat()
      },

      isAtBottom: (element) => {
        const threshold = 50 // Increased threshold for more lenient detection
        const isAtBottom = element.scrollTop + element.clientHeight >= element.scrollHeight - threshold
        
        // Debug logging to help troubleshoot
        if (process.env.NODE_ENV === 'development') {
          console.log('[AUTO_SCROLL] Bottom detection:', {
            scrollTop: element.scrollTop,
            clientHeight: element.clientHeight,
            scrollHeight: element.scrollHeight,
            threshold,
            isAtBottom,
            distanceFromBottom: element.scrollHeight - element.scrollTop - element.clientHeight
          });
        }
        
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
      }
    }),
    {
      name: 'chat-store'
    }
  )
)
