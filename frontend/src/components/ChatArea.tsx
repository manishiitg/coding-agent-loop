import { useEffect, useRef, useCallback, forwardRef, useImperativeHandle, useMemo, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import debounce from 'lodash.debounce'
import { agentApi, resetSessionId, getSessionId } from '../services/api'
import type { PollingEvent, ActiveSessionInfo, ExtendedLLMConfiguration } from '../services/api-types'
import { shouldShowEventByMode } from './events/eventModeUtils'
import type { AgentMode } from '../stores/types'
import { ChatInput } from './ChatInput'
import { EventDisplay } from './EventDisplay'
import { WorkflowModeHandler, type WorkflowModeHandlerRef } from './workflow'
import { ToastContainer } from './ui/Toast'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { WorkflowExplanation } from './WorkflowExplanation'
import { useAppStore, useLLMStore, useMCPStore, useChatStore, useGlobalPresetStore } from '../stores'
import { useModeStore, type ModeCategory } from '../stores/useModeStore'
import { ModeEmptyState } from './ModeEmptyState'
import { PresetSelectionOverlay } from './PresetSelectionOverlay'
import { ModeSwitchDialog } from './ui/ModeSwitchDialog'
import { ChatHeader } from './ChatHeader'
import type { ChatTab, ChatTabConfig } from '../stores/useChatStore'
import { WORKSPACE_TOOLS } from '../utils/customToolNames'
import { truncateTabTitle } from '../utils/textUtils'
import { logger } from '../utils/logger'
import {
  determineModeFlag,
  buildLLMConfigWithApiKeys,
  buildQueryRequestPayload,
  resolveOrCreateTab,
  createUserMessageEvent,
  validateExecutionGroups,
} from '../utils/chatSubmitHelpers'

interface ChatAreaProps {
  // New chat handler
  onNewChat: () => void
  // Hide header when used inside another layout (like WorkflowLayout)
  hideHeader?: boolean
  // Hide input area when used inside workflow mode
  hideInput?: boolean
  // Compact mode for smaller font sizes (used in workflow layout)
  compact?: boolean
  // Tab ID - if provided, use this tab's session ID (works for both chat and workflow modes)
  tabId?: string
}

import type { ExecutionOptions } from '../services/api-types'

// Ref interface for ChatArea component
export interface ChatAreaRef {
  handleNewChat: () => void
  resetChatState: () => void
  refreshWorkflowPresets: () => Promise<void>
  submitQuery: (query: string, executionOptions?: ExecutionOptions) => Promise<void>
  getEvents: () => PollingEvent[]
  isStreaming: boolean
  currentWorkflowPhase: string
}


// Global flag to ensure auto-restore only happens once per page load
let globalHasRestored = false

// Global Set to track session IDs currently being restored to prevent race conditions
// between auto-restore and manual session detection creating duplicate tabs
const sessionsBeingRestored = new Set<string>()

// Inner component that can use the EventMode context
const ChatAreaInner = forwardRef<ChatAreaRef, ChatAreaProps>((props, ref) => {
  const { onNewChat, hideHeader = false, hideInput = false, compact = false, tabId } = props
  
  // Store subscriptions
  const { 
    agentMode, 
    setCurrentQuery,
    chatSessionId,
    chatSessionTitle
  } = useAppStore(useShallow(state => ({
    agentMode: state.agentMode,
    setCurrentQuery: state.setCurrentQuery,
    chatSessionId: state.chatSessionId,
    chatSessionTitle: state.chatSessionTitle
  })))
  
  const { selectedModeCategory, getAgentModeFromCategory } = useModeStore(useShallow(state => ({
    selectedModeCategory: state.selectedModeCategory,
    getAgentModeFromCategory: state.getAgentModeFromCategory
  })))
  const { getActivePreset, applyPreset, clearActivePreset, currentPresetServers } = useGlobalPresetStore(useShallow(state => ({
    getActivePreset: state.getActivePreset,
    applyPreset: state.applyPreset,
    clearActivePreset: state.clearActivePreset,
    currentPresetServers: state.currentPresetServers
  })))
  
  // Derive correct agent mode from selectedModeCategory (source of truth)
  const correctAgentMode = useMemo(() => {
    if (selectedModeCategory) {
      return getAgentModeFromCategory(selectedModeCategory) as AgentMode
    }
    return agentMode // Fallback to agentMode if selectedModeCategory is null
  }, [selectedModeCategory, agentMode, getAgentModeFromCategory])
  
  // LLM provider configs are read via useLLMStore.getState() in helpers
  
  const {
    toolList: allTools,
    selectedServers,
    enabledServers,
  } = useMCPStore(useShallow(state => ({
    toolList: state.toolList,
    selectedServers: state.selectedServers,
    enabledServers: state.enabledServers,
  })))
  
  // Get active tab reactively (works for both chat and workflow modes)
  // Use selector to ensure reactivity when tab config changes
  const activeTabIdFromStore = useChatStore(state => state.activeTabId)
  const targetTabId = tabId || activeTabIdFromStore
  const activeTab = useChatStore(state => 
    targetTabId ? state.chatTabs[targetTabId] : undefined
  )
  
  // Get all tabs reactively for polling and other operations
  const chatTabs = useChatStore(state => state.chatTabs)
  
  // Determine which servers to use based on mode category
  // CRITICAL: Workflow preset servers should ONLY be used in workflow mode, never leak into chat mode
  const effectiveServers = useMemo(() => {
    // For workflow mode, use preset servers
    if (selectedModeCategory === 'workflow') {
      return currentPresetServers.length > 0 ? currentPresetServers : selectedServers
    }
    // For chat mode, ALWAYS use tab's selected servers from config (if available), otherwise fall back to global
    // NEVER use currentPresetServers in chat mode - workflow preset state is isolated to workflow mode only
    const isChatLike = selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent'
    const tabSelectedServers = (isChatLike && activeTab?.config)
      ? activeTab.config.selectedServers 
      : selectedServers
    
    // If no servers are selected (empty array), default to no servers (pure LLM mode)
    // User must explicitly select servers to enable tools
    if (tabSelectedServers.length === 0) {
      return ["NO_SERVERS"]
    }
    // Filter out servers that aren't currently enabled (connected).
    // Stale servers from localStorage could block queries if sent to backend.
    const filtered = tabSelectedServers.filter(s => s === "NO_SERVERS" || enabledServers.includes(s))
    return filtered
  }, [
    selectedModeCategory,
    // Include currentPresetServers only for workflow mode reactivity
    // In chat mode, this value is ignored but included to satisfy exhaustive-deps
    // The logic above ensures chat mode never uses currentPresetServers
    currentPresetServers,
    selectedServers,
    enabledServers,
    activeTab?.config  // ✅ Now reactive - will update when tab config changes
  ])
  
  // Filter tools to only include those from effective servers
  // If "NO_SERVERS" is selected, return empty tools (pure LLM mode)
  // Also filter out workspace tools if workspace access is disabled
  const enabledTools = useMemo(() => {
    if (effectiveServers.includes("NO_SERVERS")) {
      return []
    }
    
    // Get workspace access setting from tab config (default: true)
    const enableWorkspaceAccess = ((selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent') && activeTab?.config)
      ? (activeTab.config.enableWorkspaceAccess ?? true)
      : true // Default to enabled for workflow mode or if no tab config
    
    let filteredTools = allTools.filter(tool => 
      tool.server && effectiveServers.includes(tool.server)
    )
    
    // Filter out workspace tools if workspace access is disabled
    // Use category-based filtering: check if tool name is in WORKSPACE_TOOLS list
    // This matches the backend category system where workspace tools have category "workspace"
    if (!enableWorkspaceAccess) {
      const workspaceToolSet = new Set<string>(WORKSPACE_TOOLS as readonly string[])
      filteredTools = filteredTools.filter(tool => {
        const toolName = tool.name || ''
        return !workspaceToolSet.has(toolName)
      })
    }
    
    return filteredTools
  }, [allTools, effectiveServers, selectedModeCategory, activeTab?.config])
  
  // Get all tabs to track changes for polling
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const allTabs = useMemo(() => Object.values(chatTabs), [chatTabs])
  const tabsWithSessions = useMemo(() => allTabs.filter(tab => tab.sessionId), [allTabs])
  
  // No observer ID syncing needed - sessions are used directly

  const {
    // Chat state
    isStreaming,
    setIsStreaming,
    lastEventIndex,
    setLastEventIndex,
    pollingInterval,
    // Deprecated: totalEvents, setTotalEvents, setLastEventCount, events, setEvents removed
    getTabEvents,
    addTabEvents,
    setTabEvents,
    getTabLastEventIndex,
    setTabLastEventIndex,
    setHasActiveChat,
    autoScroll,
    setAutoScroll,
    lastScrollTop,
    setLastScrollTop,
    finalResponse,
    setIsCompleted,
    isLoadingHistory,
    setIsLoadingHistory,
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    setIsApprovingWorkflow: _setIsApprovingWorkflow,
    sessionState,
    setSessionState,
    isCheckingActiveSessions,
    setIsCheckingActiveSessions,
    currentWorkflowPhase,
    setCurrentWorkflowPhase,
    setCurrentWorkflowQueryId,
    toasts,
    addToast,
    removeToast,
    resetChatState,
    isAtBottom,
    createChatTab,
    switchTab
  } = useChatStore(useShallow(state => ({
    isStreaming: state.isStreaming,
    setIsStreaming: state.setIsStreaming,
    lastEventIndex: state.lastEventIndex,
    setLastEventIndex: state.setLastEventIndex,
    pollingInterval: state.pollingInterval,
    getTabEvents: state.getTabEvents,
    addTabEvents: state.addTabEvents,
    setTabEvents: state.setTabEvents,
    getTabLastEventIndex: state.getTabLastEventIndex,
    setTabLastEventIndex: state.setTabLastEventIndex,
    setHasActiveChat: state.setHasActiveChat,
    autoScroll: state.autoScroll,
    setAutoScroll: state.setAutoScroll,
    lastScrollTop: state.lastScrollTop,
    setLastScrollTop: state.setLastScrollTop,
    finalResponse: state.finalResponse,
    setIsCompleted: state.setIsCompleted,
    isLoadingHistory: state.isLoadingHistory,
    setIsLoadingHistory: state.setIsLoadingHistory,
    setIsApprovingWorkflow: state.setIsApprovingWorkflow,
    sessionState: state.sessionState,
    setSessionState: state.setSessionState,
    isCheckingActiveSessions: state.isCheckingActiveSessions,
    setIsCheckingActiveSessions: state.setIsCheckingActiveSessions,
    currentWorkflowPhase: state.currentWorkflowPhase,
    setCurrentWorkflowPhase: state.setCurrentWorkflowPhase,
    setCurrentWorkflowQueryId: state.setCurrentWorkflowQueryId,
    toasts: state.toasts,
    addToast: state.addToast,
    removeToast: state.removeToast,
    resetChatState: state.resetChatState,
    isAtBottom: state.isAtBottom,
    createChatTab: state.createChatTab,
    switchTab: state.switchTab
  })))

  // Subscribe to tabEvents directly from store to ensure reactivity
  const tabEventsStore = useChatStore((state) => state.tabEvents)

  // Get active preset for workflow mode
  const activeWorkflowPreset = getActivePreset('workflow')
  const selectedWorkflowPreset = activeWorkflowPreset?.id || null
  
  // Get events for the active tab (per-tab event storage)
  // Subscribe directly to tabEvents from store to ensure reactivity
  // CRITICAL: Only show events for the active tab's session ID to prevent cross-tab event mixing
  // NEVER fall back to global events - always use tab-specific events to prevent mixing
  // If activeTab has no sessionId, return empty array (allows header to render for mode/preset selection)
  
  const tabEvents = useMemo(() => {
    // Get sessionId from activeTab
    const effectiveSessionId = activeTab?.sessionId
    
    if (effectiveSessionId) {
      // CRITICAL: Use the session ID from activeTab, ensuring correct isolation
      // Multiple observers can view the same session, but events are stored per session
      const currentSessionId = effectiveSessionId
      
      // Only get events for this specific session ID
      const eventsForTab = tabEventsStore[currentSessionId] || []
      
      // Debug logging to help diagnose event filtering issues
      const allSessionIds = Object.keys(tabEventsStore)
      if (allSessionIds.length > 0) {
        const chatStore = useChatStore.getState()
        const sessionIdToTabs = allSessionIds.map(id => {
          const tabsUsingThisId = Object.values(chatStore.chatTabs)
            .filter((t: ChatTab) => t.sessionId === id)
            .map((t: ChatTab) => t.tabId)
          return { 
            sessionId: id, 
            eventCount: tabEventsStore[id]?.length || 0,
            tabsUsingThisId
          }
        })
        
        const duplicateSessionIds = sessionIdToTabs.filter(item => item.tabsUsingThisId.length > 1)
        if (duplicateSessionIds.length > 0) {
          logger.warn('ChatArea', 'Multiple tabs sharing session IDs:', duplicateSessionIds)
        }
        
        // Check if the current sessionId matches any in the store (only warn on issues)
        const sessionDetails = sessionIdToTabs.map(item => ({
          sessionId: item.sessionId,
          eventCount: item.eventCount,
          tabs: item.tabsUsingThisId
        }))
        const matchingSession = sessionDetails.find(item => item.sessionId === currentSessionId)
        if (!matchingSession) {
          logger.warn('ChatArea', `SessionId ${currentSessionId} not found in store`, sessionDetails.map(item => item.sessionId))
        } else if (matchingSession.eventCount !== eventsForTab.length) {
          logger.warn('ChatArea', `Event count mismatch: store=${matchingSession.eventCount}, found=${eventsForTab.length}`)
        }
      }
      return eventsForTab
    }
    
    // No session ID - return empty array (allows header to render for mode/preset selection)
    return []
  }, [activeTab?.sessionId, tabEventsStore])
  
  // Always use tab events - never fall back to global events to prevent cross-tab mixing
  // If there are no tabs, return empty array (tabs should always exist in multi-tab mode)
  // Filter out workspace_file_operation events from display in basic/tiny mode
  // (These events are still sent to frontend for workspace store processing, but hidden from chat UI)
  const displayEvents = useMemo(() => {
    const eventMode = activeTab?.eventMode || 'basic'
    // In basic/tiny/micro mode, hide workspace_file_operation events from display
    // (they're still processed by useWorkspaceStore for file highlighting)
    if (eventMode === 'basic' || eventMode === 'tiny' || eventMode === 'micro') {
      return tabEvents.filter(event => {
        if (event.type === 'workspace_file_operation') return false
        
        // In tiny/micro mode, also hide Total Token Usage and Context Offloading events
        if (eventMode === 'tiny' || eventMode === 'micro') {
          if (event.type === 'token_usage') {
            const agentEvent = event.data as { data?: Record<string, unknown> } | undefined
            const payload = agentEvent?.data || event.data as Record<string, unknown> | undefined
            
            if (payload?.context === 'conversation_total') {
              return false
            }
          }
          
          if (event.type === 'large_tool_output_detected' || event.type === 'large_tool_output_file_written') {
            return false
          }
        }
        
        return true
      })
    }
    // In advanced mode, show all events
    return tabEvents
  }, [tabEvents, activeTab?.eventMode])
  
  // Debug: Log when displayEvents changes

  // Computed values
  const isRequiredFolderSelected = useMemo(() => {
    if (selectedModeCategory !== 'workflow') return true; // No validation needed for other modes

    // Workflow mode requires Workflow/ folder from preset
    if (selectedModeCategory === 'workflow') {
      const workflowFolder = activeWorkflowPreset?.selectedFolder?.filepath
      return workflowFolder ? workflowFolder.startsWith('Workflow/') : false
    }
    
    return true;
  }, [selectedModeCategory, activeWorkflowPreset])

  // Use currentPresetServers from props (passed from App.tsx when preset is selected)

  // State for preset selection overlay
  const [showPresetSelection, setShowPresetSelection] = useState(false)
  const [pendingModeCategory, setPendingModeCategory] = useState<Exclude<ModeCategory, 'chat' | null> | null>(null)
  
  // State for mode switch dialog
  const [showModeSwitchDialog, setShowModeSwitchDialog] = useState(false)
  const [pendingModeSwitch, setPendingModeSwitch] = useState<Exclude<ModeCategory, null> | null>(null)
  

  // Handle mode selection from dropdown
  // Handle mode switching with preset selection for Workflow
  const handleModeSwitchWithPreset = (category: Exclude<ModeCategory, null>) => {
    if (category === 'chat') {
      // Chat mode doesn't need preset selection
      // Clear any active presets when switching to chat mode
      clearActivePreset('workflow')
      switchMode(category)
    } else {
      // Workflow mode - always show preset selection when switching between modes
      // Clear the current mode's preset first
      if (selectedModeCategory === 'workflow') {
        clearActivePreset('workflow')
      }
      
      // Check if target mode already has a preset
      const activePreset = getActivePreset(category)
      
      if (activePreset) {
        // Preset already selected, switch mode directly
        switchMode(category)
      } else {
        // No preset selected, show preset selection overlay
        setPendingModeCategory(category)
        setShowPresetSelection(true)
      }
    }
  }

  // Switch mode function
  const switchMode = (category: Exclude<ModeCategory, null>) => {
    const { setModeCategory, getAgentModeFromCategory } = useModeStore.getState()
    const { setAgentMode } = useAppStore.getState()
    
    setModeCategory(category)
    
    // Set the corresponding agent mode using centralized mapping
    const agentModeToSet = getAgentModeFromCategory(category) as AgentMode
    setAgentMode(agentModeToSet)
  }

  // Handle preset selection from overlay
  const handlePresetSelected = (presetId: string) => {
    if (pendingModeCategory) {
      // Now switch to the mode
      switchMode(pendingModeCategory)
      
      // Apply the preset after mode switch (this will also set the active preset ID)
      setTimeout(() => {
        const result = applyPreset(presetId, pendingModeCategory)
        if (!result.success) {
          logger.error('ChatArea', 'Failed to apply preset:', result.error)
        }
      }, 100)
      
      // Close overlay
      setShowPresetSelection(false)
      setPendingModeCategory(null)
    }
  }

  // Handle preset selection overlay close
  const handlePresetSelectionClose = () => {
    setShowPresetSelection(false)
    setPendingModeCategory(null)
  }

  
  // Filter toasts to only include types supported by ToastContainer
  const filteredToasts = toasts.filter((toast: { type: string }) => toast.type === 'success' || toast.type === 'info' || toast.type === 'error') as Array<{id: string, message: string, type: 'success' | 'info' | 'error'}>
  
  // Handle mode switch dialog confirmation
  const handleModeSwitchConfirm = () => {
    if (pendingModeSwitch) {
      handleModeSwitchWithPreset(pendingModeSwitch)
      // Clear backend session and reset UI after mode switch
      handleNewChat()
    }
    setShowModeSwitchDialog(false)
    setPendingModeSwitch(null)
  }
  
  // Handle mode switch dialog cancellation
  const handleModeSwitchCancel = () => {
    setShowModeSwitchDialog(false)
    setPendingModeSwitch(null)
  }
  
  // Add ref for auto-scrolling
  const chatContentRef = useRef<HTMLDivElement>(null)
  
  // Add ref for workflow mode handler
  const workflowModeHandlerRef = useRef<WorkflowModeHandlerRef>(null)
  
  
  // Track processed completion events to avoid stopping on old ones
  const processedCompletionEventsRef = useRef<Set<string>>(new Set())
  

  // Selected preset folder state
  const lastEventIndexRef = useRef<number>(-1)
  // Deprecated: totalEventsRef removed
  const previousEventCountRef = useRef<number>(0)

  // Ref to track if we're currently performing programmatic scrolling
  const isProgrammaticScrollRef = useRef<boolean>(false)
  const programmaticScrollTimeoutRef = useRef<NodeJS.Timeout | null>(null)

  // Ref to track currentWorkflowPhase without causing callback re-renders
  const currentWorkflowPhaseRef = useRef<string>(currentWorkflowPhase)
  useEffect(() => {
    currentWorkflowPhaseRef.current = currentWorkflowPhase
  }, [currentWorkflowPhase])

  // Observer initialization removed - no longer needed

  // Immediate scroll handler for better responsiveness
  const handleScroll = useCallback(() => {
    if (!chatContentRef.current) return;
    
    // If this is a programmatic scroll, ignore it (don't disable auto-scroll)
    if (isProgrammaticScrollRef.current) {
      // Still update last scroll position to track movement
      const element = chatContentRef.current;
      setLastScrollTop(element.scrollTop);
      return;
    }
    
    const element = chatContentRef.current;
    const currentScrollTop = element.scrollTop;
    const scrollDistance = Math.abs(currentScrollTop - lastScrollTop);
    const isScrollingUp = currentScrollTop < lastScrollTop;
    const isScrollingDown = currentScrollTop > lastScrollTop;
    
    // Check if user is at bottom
    const wasAtBottom = isAtBottom(element);
    
    // Only disable auto-scroll if user actively scrolls up significantly
    // Don't show toast - user can see the toggle in header and floating button
    if (isScrollingUp && scrollDistance > 150 && autoScroll) {
      setAutoScroll(false);
    }
    // Re-enable auto-scroll when user scrolls back to bottom
    // Don't show toast - user can see the toggle in header
    else if (wasAtBottom && !autoScroll) {
      setAutoScroll(true);
    }
    // Re-enable auto-scroll if user scrolled down significantly and is near bottom
    else if (isScrollingDown && scrollDistance > 30 && !wasAtBottom && !autoScroll) {
      // Check if user is close to bottom (within 100px)
      const distanceFromBottom = element.scrollHeight - element.scrollTop - element.clientHeight;
      if (distanceFromBottom < 100) {
        setAutoScroll(true);
      }
    }
    
    // Update last scroll position immediately
    setLastScrollTop(currentScrollTop);
  }, [autoScroll, isAtBottom, lastScrollTop, setAutoScroll, setLastScrollTop]);


  // Set up scroll event listener
  useEffect(() => {
    const element = chatContentRef.current;
    if (!element) return;

    // Initialize lastScrollTop with current position
    setLastScrollTop(element.scrollTop);

    element.addEventListener('scroll', handleScroll);
    return () => {
      element.removeEventListener('scroll', handleScroll);
      // Cleanup programmatic scroll timeout on unmount
      if (programmaticScrollTimeoutRef.current) {
        clearTimeout(programmaticScrollTimeoutRef.current);
        programmaticScrollTimeoutRef.current = null;
      }
    };
  }, [handleScroll, setLastScrollTop]);

  // Reset auto-scroll when starting new conversation (events go from 0 to > 0)
  // Use displayEvents (tabEvents) instead of events to track the actual displayed events
  useEffect(() => {
    const currentEventCount = displayEvents.length
    const previousEventCount = previousEventCountRef.current
    
    // Only reset auto-scroll when starting a new conversation (0 -> > 0)
    // Don't reset if user has manually disabled it or if events are just updating
    if (previousEventCount === 0 && currentEventCount > 0 && !isStreaming) {
      setAutoScroll(true);
    }
    
    previousEventCountRef.current = currentEventCount
  }, [displayEvents.length, isStreaming, setAutoScroll]);

  // Improved auto-scroll for new events
  const scrollToBottom = useCallback((behavior: ScrollBehavior = 'smooth') => {
    if (!chatContentRef.current) return;
    
    const element = chatContentRef.current;
    const targetScrollTop = element.scrollHeight - element.clientHeight;
    
    // Mark that we're performing programmatic scrolling
    isProgrammaticScrollRef.current = true
    
    // Clear any existing timeout
    if (programmaticScrollTimeoutRef.current) {
      clearTimeout(programmaticScrollTimeoutRef.current)
    }
    
    // Use requestAnimationFrame for smoother scrolling
    requestAnimationFrame(() => {
      element.scrollTo({
        top: targetScrollTop,
        behavior
      });
      
      // Clear the programmatic scroll flag after scroll completes
      // For smooth scroll, wait longer; for instant, clear immediately
      const timeoutDuration = behavior === 'smooth' ? 600 : 100
      programmaticScrollTimeoutRef.current = setTimeout(() => {
        isProgrammaticScrollRef.current = false
        programmaticScrollTimeoutRef.current = null
      }, timeoutDuration)
    });
  }, [])

  // Callback to re-enable auto-scroll and scroll to bottom after feedback submission
  const handleFeedbackSubmitted = useCallback(() => {
    setAutoScroll(true)
    scrollToBottom('smooth')
  }, [setAutoScroll, scrollToBottom])

  // Auto-scroll to bottom when new events arrive (only if autoScroll is enabled)
  // Use displayEvents (tabEvents) instead of events to track the actual displayed events
  useEffect(() => {
    if (autoScroll && chatContentRef.current && displayEvents.length > 0) {
      // Use requestAnimationFrame to ensure DOM has updated with new content
      requestAnimationFrame(() => {
        const element = chatContentRef.current;
        if (!element) return;
        
        // Always scroll to bottom when auto-scroll is enabled and new events arrive
        // The scroll handler will disable auto-scroll if user manually scrolls away
        scrollToBottom('smooth');
      });
    }
  }, [displayEvents.length, autoScroll, scrollToBottom])

  // Auto-scroll to bottom when final response is updated (only if autoScroll is enabled)
  useEffect(() => {
    if (autoScroll && chatContentRef.current && finalResponse) {
      // Use requestAnimationFrame to ensure DOM has updated with new content
      requestAnimationFrame(() => {
        const element = chatContentRef.current;
        if (!element) return;

        // Always scroll to bottom when auto-scroll is enabled and final response updates
        // The scroll handler will disable auto-scroll if user manually scrolls away
        scrollToBottom('smooth');
      });
    }
  }, [finalResponse, autoScroll, scrollToBottom])

  // Auto-scroll when streaming text first appears (brings the "Generating..." card into view)
  const activeSessionId = activeTab?.sessionId
  const hasStreamingText = useChatStore(state =>
    activeSessionId ? !!state.streamingText[activeSessionId] : false
  )
  const prevHasStreamingTextRef = useRef(false)
  useEffect(() => {
    if (hasStreamingText && !prevHasStreamingTextRef.current && autoScroll && chatContentRef.current) {
      requestAnimationFrame(() => {
        scrollToBottom('smooth')
      })
    }
    prevHasStreamingTextRef.current = hasStreamingText
  }, [hasStreamingText, autoScroll, scrollToBottom])


  // Update refs when values change (for global observer)
  useEffect(() => {
    if (!activeTab) {
      lastEventIndexRef.current = lastEventIndex
    }
  }, [lastEventIndex, activeTab])
  
  // Update displayEvents when active tab changes
  // Tab events are automatically loaded via tabEvents useMemo
  
  // Deprecated: totalEventsRef useEffect removed

  // Workflow preset handlers
  const handleWorkflowPresetSelected = useCallback(async (presetId: string, presetContent: string) => {
    // Apply the preset using the global preset store
    // File context is now preset-specific (from preset.selectedFolder), no need to clear
    applyPreset(presetId, 'workflow')
    setCurrentWorkflowQueryId(presetId) // Store the preset query ID for workflow approval
    
    try {
      // Ensure phases are loaded and get them from store
      const workflowStore = useWorkflowStore.getState()
      if (!workflowStore.phasesInitialized) {
        await workflowStore.loadPhases()
      }
      const phases = workflowStore.phases
      const phaseIds = phases.map(p => p.id)
      const defaultPhase = workflowStore.getDefaultPhase()
      
      // Check if workflow already exists for this preset
      const workflowStatus = await agentApi.getWorkflowStatus(presetId)
      
      if (workflowStatus.success && workflowStatus.workflow) {
        const workflow = workflowStatus.workflow
        const status = workflow.workflow_status
        
        // Set the workflow phase based on the database status
        // Use the status if it's a valid phase ID, otherwise use default (first phase)
        if (status && phaseIds.includes(status)) {
          setCurrentWorkflowPhase(status)
        } else {
          // Default to first phase if status is invalid or not found
          setCurrentWorkflowPhase(defaultPhase)
        }
        
        // Use presetContent directly (this is the objective from preset query)
        setCurrentQuery(presetContent)
      } else {
        // No workflow exists, proceed with default phase
        setCurrentWorkflowPhase(defaultPhase)
        setCurrentQuery(presetContent)
      }
    } catch (error) {
      logger.error('ChatArea', 'Error checking workflow status:', error)
      // Fallback to default phase on error
      const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
      setCurrentWorkflowPhase(defaultPhase)
      setCurrentQuery(presetContent)
    }
  }, [setCurrentQuery, applyPreset, setCurrentWorkflowPhase, setCurrentWorkflowQueryId])

  const handleWorkflowPresetCleared = useCallback(() => {
    clearActivePreset('workflow')
    setCurrentWorkflowQueryId(null) // Clear the stored preset query ID
    const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
    setCurrentWorkflowPhase(defaultPhase) // Reset to default phase
    setCurrentQuery('')
  }, [clearActivePreset, setCurrentWorkflowQueryId, setCurrentWorkflowPhase, setCurrentQuery])
  
  // Clear workflow state when starting a new chat
  const clearWorkflowState = useCallback(() => {
    clearActivePreset('workflow')
    setCurrentWorkflowQueryId(null)
    const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
    setCurrentWorkflowPhase(defaultPhase)
  }, [clearActivePreset, setCurrentWorkflowQueryId, setCurrentWorkflowPhase])

  // Handle human verification actions
  // TODO: Re-enable when RequestHumanFeedbackEvent is available
  /*
  const handleApproveWorkflow = useCallback(async (_requestId: string, eventData?: { next_phase?: string }) => {
    
    setIsApprovingWorkflow(true)  // Set loading state
    
    // Use the stored preset query ID instead of the request ID
    const presetQueryId = currentWorkflowQueryId
    if (!presetQueryId) {
      logger.error('ChatArea', 'No preset query ID available for workflow approval')
      setIsApprovingWorkflow(false)
      return
    }
    
    try {
      // Determine next phase based on event data
      // If next_phase is provided, use it; otherwise get the second phase (planning) as default
      let nextPhase = eventData?.next_phase
      if (!nextPhase) {
        const phases = useWorkflowStore.getState().phases
        // Use second phase (planning) if available, otherwise first phase
        nextPhase = phases.length > 1 ? phases[1].id : (phases.length > 0 ? phases[0].id : 'execution')
      }
      
      // Update workflow status to the determined next phase
      await agentApi.updateWorkflow(presetQueryId, nextPhase)
      
      // Stop any ongoing polling to prevent events from coming back
      if (pollingInterval) {
        stopPolling()
      }
      
      // Clear all events to show clean slate for execution phase
      // Note: Using tabEvents now, not global events
      if (currentTab?.sessionId) {
        chatStore.clearTabEvents(currentTab.sessionId)
      }
      // Deprecated: setLastEventCount removed
      setLastEventIndex(-1)
      setFinalResponse('')
      setIsCompleted(false)
      setCurrentUserMessage('')
      setShowUserMessage(false)
      
      // Update phase to the determined next phase
      setCurrentWorkflowPhase(nextPhase as WorkflowPhase)
      
    } catch (error) {
      logger.error('ChatArea', 'Failed to approve workflow:', error)
      // TODO: Show error message to user
    } finally {
      setIsApprovingWorkflow(false)  // Clear loading state
    }
  }, [currentWorkflowQueryId, pollingInterval, setIsApprovingWorkflow, setLastEventIndex, setFinalResponse, setIsCompleted, setCurrentUserMessage, setShowUserMessage, setCurrentWorkflowPhase, setPollingInterval])
  */

  // Observer initialization removed - no longer needed

  // Event batching for performance
  const eventBatchRef = useRef<PollingEvent[]>([])
  const batchTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  
  // Debounced function to flush event batch
  // CRITICAL: This should NOT be used - all events should be stored via addTabEvents in polling
  // This function is kept for backward compatibility but should never be called in tab mode
  const flushEventBatch = useCallback(() => {
    if (eventBatchRef.current.length === 0) return
    
    const batch = [...eventBatchRef.current]
    eventBatchRef.current = []
    
    // Tabs should always exist - this should never be called
    // Log error and discard to prevent mixing
    logger.error('ChatArea', `flushEventBatch should not be called in tab mode. Discarding ${batch.length} events.`)
  }, [])
  
  // Create debounced flush function (100ms delay)
  const debouncedFlush = useMemo(
    () => debounce(flushEventBatch, 100),
    [flushEventBatch]
  )

  // Removed extractUserMessageContent - no longer needed since we removed duplicate detection


  // Get polling management actions from store (before pollEvents callback)
  const { startPolling, stopPolling, getActiveSessions } = useChatStore(useShallow(state => ({
    startPolling: state.startPolling,
    stopPolling: state.stopPolling,
    getActiveSessions: state.getActiveSessions
  })))

  // Get active sessions from cache (shared across all components)
  const startActiveSessionsPolling = useChatStore(state => state.startActiveSessionsPolling)
  
  // Subscribe to active sessions cache updates
  // Get the array first, then memoize the Set to avoid infinite loops
  const activeSessionsCache = useChatStore((state) => state.activeSessionsCache)
  const activeSessionIds = useMemo(() => {
    return new Set(activeSessionsCache.map(s => s.session_id))
  }, [activeSessionsCache])

  // Polling function to get events for ALL active sessions
  const pollEvents = useCallback(async () => {
    const chatStore = useChatStore.getState()

    // Read mode from store directly to avoid stale closure from setInterval capture
    const currentModeCategory = useModeStore.getState().selectedModeCategory

    // Get all tabs that should be polled (all tabs in current mode)
    const allTabs = Object.values(chatStore.chatTabs).filter(tab => {
      // If mode category is null (not yet selected), poll all non-workflow tabs
      if (!currentModeCategory) {
        return tab.metadata?.mode !== 'workflow'
      }
      return tab.metadata?.mode === currentModeCategory
    })
    
    // CRITICAL: Only poll tabs that are:
    // 1. Actively streaming (query in progress)
    // 2. Have session ID in backend's active sessions list (backend determines activity based on events)
    // We don't poll completed sessions - they're done and won't have new events
    // We also don't poll uninitialized sessions (no query submitted yet)
    const tabsToPoll = allTabs.filter(tab => {
      const currentTab = chatStore.getTab(tab.tabId)
      if (!currentTab?.sessionId) {
        return false
      }
      
      // Check if session is in backend's active sessions list (source of truth)
      // Backend determines activity based on event activity (10 min timeout)
      // If session is in backend's active list, poll it regardless of local isStreaming state
      // This ensures we catch events that come after stop is pressed
      // CRITICAL: Also allow polling if tab is streaming (user just submitted a query)
      // This handles the case where a restored session is being replied to
      const isStreaming = currentTab.isStreaming
      const isInActiveSessions = activeSessionIds.size === 0 || activeSessionIds.has(currentTab.sessionId)
      
      // Allow polling if:
      // 1. Session is in backend's active sessions list, OR
      // 2. Tab is currently streaming (query just submitted)
      if (!isInActiveSessions && !isStreaming) {
        return false
      }
      
      // Skip if completed (definitely done)
      if (currentTab.isCompleted) {
        return false
      }
      
      return true
    })
    
    // CRITICAL: Poll by sessionId, not observerId
    // Multiple observers can view the same session, but events are stored per session
    const sessionsToPoll: Array<{ sessionId: string; tab: ChatTab | null }> = []
    
    // Add all tab sessions (deduplicate by sessionId)
    const seenSessionIds = new Set<string>()
    tabsToPoll.forEach(tab => {
      const currentTab = chatStore.getTab(tab.tabId)
      const sessionId = currentTab?.sessionId || tab.sessionId
      if (sessionId && !seenSessionIds.has(sessionId)) {
        seenSessionIds.add(sessionId)
        sessionsToPoll.push({ sessionId, tab: currentTab || tab })
      }
    })
    
    if (sessionsToPoll.length === 0) {
      return
    }
    
    // Poll each session
    for (const { sessionId, tab } of sessionsToPoll) {
      let currentTab = tab
      
      if (tab) {
        // Re-fetch the tab from store to ensure we have the latest session ID
        const fetchedTab = chatStore.getTab(tab.tabId)
        if (!fetchedTab) {
          continue
        }
        currentTab = fetchedTab
        
        // Verify session ID matches
        if (currentTab.sessionId !== sessionId) {
          // Use the new session ID
          if (!currentTab.sessionId) {
            continue
          }
        }
        
        // Double-check: verify this tab should still be polled
        // Only check isCompleted and sessionId - isStreaming is UI-only, not used for polling decisions
        if (currentTab.isCompleted && !currentTab.sessionId) {
          continue
        }
      }
      
      // Get fresh tab from store to ensure we have latest session ID
      const freshTab = currentTab ? chatStore.getTab(currentTab.tabId) : null
      const effectiveSessionId = freshTab?.sessionId || currentTab?.sessionId || sessionId
      
      let rawLastEventIndex = currentTab 
        ? getTabLastEventIndex(effectiveSessionId)
        : lastEventIndexRef.current
      
      // CRITICAL: Detect sentinel value (9999) which means "all events processed" but not an actual index
      // If lastEventIndex is 9999 or higher, check stored events to get the actual last index
      if (rawLastEventIndex >= 9999) {
        const storedEvents = getTabEvents(effectiveSessionId)
        if (storedEvents && storedEvents.length > 0) {
          const actualLastIndex = storedEvents.length - 1
          rawLastEventIndex = actualLastIndex
          // Update the stored index to the correct value
          if (currentTab) {
            setTabLastEventIndex(effectiveSessionId, actualLastIndex)
          } else {
            setLastEventIndex(actualLastIndex)
          }
        } else {
          // No stored events, but sentinel value - reset to 0 to start fresh
          rawLastEventIndex = 0
          if (currentTab) {
            setTabLastEventIndex(effectiveSessionId, 0)
          } else {
            setLastEventIndex(0)
          }
        }
      } else if (rawLastEventIndex === -1) {
        // Safety check: if index is -1 but we have events, use the event count
        // This prevents re-fetching from 0 if index state was lost but events exist
        const storedEvents = getTabEvents(effectiveSessionId)
        if (storedEvents && storedEvents.length > 0) {
          const actualLastIndex = storedEvents.length - 1
          rawLastEventIndex = actualLastIndex
          logger.debug('ChatArea', `Recovered lastEventIndex ${actualLastIndex} for session ${effectiveSessionId}`)
          
          if (currentTab) {
            setTabLastEventIndex(effectiveSessionId, actualLastIndex)
          }
        }
      }
      
      // Ensure lastEventIndex is >= 0 (API requirement)
      // -1 means "no events yet", which should be treated as 0
      const currentLastEventIndex = Math.max(0, rawLastEventIndex === -1 ? 0 : rawLastEventIndex)
      
      // Track which session is currently being polled (for derived isStreaming)

      try {
        // Get event mode from current tab (defaults to 'basic')
        const eventMode: 'basic' | 'advanced' | 'tiny' | 'micro' = (currentTab?.eventMode || 'basic') as 'basic' | 'advanced' | 'tiny' | 'micro'
        
        const response = await agentApi.getSessionEvents(effectiveSessionId, currentLastEventIndex, { eventMode })

        // Use session ID from response if available (it's the source of truth)
        const actualSessionId = response.session_id || effectiveSessionId
        
        // If response has a different session ID, update the tab
        if (currentTab && response.session_id && response.session_id !== effectiveSessionId) {
          chatStore.updateTabSessionId(currentTab.tabId, response.session_id)
        }

        // Check session status from response (source of truth - replaces event parsing)
        // session_status is always present in the response (required field)
        // Update isStreaming for UI feedback only (not used for polling decisions)
        const sessionStatus = response.session_status
        
        if (currentTab) {
          const chatStore = useChatStore.getState()
          
          // Handle different session statuses - update UI state (isStreaming) for user feedback
          if (sessionStatus === 'completed' || sessionStatus === 'error') {
            // Session is done - update UI state
            chatStore.setTabCompleted(currentTab.tabId, true)
            chatStore.setTabStreaming(currentTab.tabId, false) // UI: Hide stop button, show send button
          } else if (sessionStatus === 'running') {
            // Session is active - update UI state
            chatStore.setTabCompleted(currentTab.tabId, false)
            chatStore.setTabStreaming(currentTab.tabId, true) // UI: Show stop button, disable send button
          } else if (sessionStatus === 'stopped' || sessionStatus === 'inactive') {
            // Session stopped or inactive - update UI state
            chatStore.setTabCompleted(currentTab.tabId, false)
            chatStore.setTabStreaming(currentTab.tabId, false) // UI: Show send button, hide stop button
          }
        } else {
          // No tab - update global UI state
          if (sessionStatus === 'completed' || sessionStatus === 'error') {
            setIsStreaming(false) // UI: Hide stop button
            setIsCompleted(true)
            setHasActiveChat(false)
          } else if (sessionStatus === 'running') {
            setIsStreaming(true) // UI: Show stop button
            setIsCompleted(false)
          } else if (sessionStatus === 'stopped' || sessionStatus === 'inactive') {
            setIsStreaming(false) // UI: Hide stop button
            setIsCompleted(false)
          }
        }

        if (response.events.length > 0) {
          // Update last event index for this session
          // CRITICAL: Use last_processed_index from backend (tracks unfiltered array position)
          // This ensures correct tracking even when filtering reduces the number of events returned
          // Require last_processed_index from backend (new system - no fallback)
          if (response.last_processed_index === undefined) {
            logger.warn('ChatArea', `Backend didn't provide last_processed_index for session ${effectiveSessionId}`)
            return // Skip processing if backend doesn't provide required field
          }
          
          // CRITICAL: Detect sentinel value (9999) which means "all events processed" but not an actual index
          // If API returns 0 events with last_processed_index=9999, use the actual last index from stored events
          let newLastEventIndex = response.last_processed_index
          if (response.events.length === 0 && response.last_processed_index >= 9999) {
            // This is likely a sentinel value - check stored events to get the actual last index
            const storedEvents = getTabEvents(actualSessionId)
            if (storedEvents && storedEvents.length > 0) {
              // Use the actual last event index from stored events (0-indexed)
              const actualLastIndex = storedEvents.length - 1
              newLastEventIndex = actualLastIndex
            } else {
              // No stored events, but sentinel value - keep current index to avoid going backwards
              newLastEventIndex = currentLastEventIndex
            }
          }
          
          if (currentTab) {
            setTabLastEventIndex(actualSessionId, newLastEventIndex)
            // If this is an initial fetch (sinceIndex=0), store has_more flag for older events
            if (currentLastEventIndex === 0 && response.has_more !== undefined) {
              useChatStore.getState().setTabHasMoreOlderEvents(actualSessionId, response.has_more)
            }
          } else {
            setLastEventIndex(newLastEventIndex)
          }
          
          // Filter events first (synchronous)
          // Type assertion: response.events is PollingEvent[] from GetEventsResponse
          const eventsBeforeFilter = response.events as PollingEvent[]

          const newEvents: PollingEvent[] = []
          let hasCompletionEvent = false
          for (const event of eventsBeforeFilter) {
            // Extract component from event (used to identify sub-agent events)
            // Sub-agent events have component like "delegation-0", "delegation-1", etc.
            // Check multiple locations: event top-level, event.data (AgentEvent), event.data.data (inner)
            const agentEvent = event.data as Record<string, unknown> | undefined
            const innerData = agentEvent?.data as Record<string, unknown> | undefined
            const rawComponent = (event as unknown as Record<string, unknown>).component ?? innerData?.component ?? agentEvent?.component
            const rawCorrelationId = (event as unknown as Record<string, unknown>).correlation_id ?? innerData?.correlation_id ?? agentEvent?.correlation_id
            const rawHierarchyLevel = (event as unknown as Record<string, unknown>).hierarchy_level ?? innerData?.hierarchy_level ?? agentEvent?.hierarchy_level
            const isSubAgentEvent = (typeof rawComponent === 'string' && rawComponent.startsWith('delegation-'))
              || (typeof rawCorrelationId === 'string' && rawCorrelationId.startsWith('delegation-'))
              || (typeof rawHierarchyLevel === 'number' && (rawHierarchyLevel as number) > 0 && typeof rawComponent === 'string' && rawComponent !== '')

            // Intercept streaming events - accumulate text in store, don't add to event list
            // Parent agent streaming → streamingText[sessionId]
            // Sub-agent streaming → delegationStreamingText[delegationId]
            if (event.type === 'streaming_start') {
              if (isSubAgentEvent) {
                const correlationId = innerData?.correlation_id ?? agentEvent?.correlation_id
                if (correlationId && typeof correlationId === 'string') {
                  chatStore.clearDelegationStreamingText(correlationId)
                }
              } else {
                chatStore.clearStreamingText(actualSessionId)
              }
              continue
            }
            if (event.type === 'streaming_chunk') {
              if (isSubAgentEvent) {
                // Route sub-agent streaming to delegation-specific slot
                const correlationId = innerData?.correlation_id ?? agentEvent?.correlation_id
                if (correlationId && typeof correlationId === 'string') {
                  const rawContent = innerData?.content ?? agentEvent?.content
                  const content = typeof rawContent === 'string' ? rawContent : ''
                  const rawIndex = innerData?.chunk_index ?? agentEvent?.chunk_index
                  const chunkIndex = typeof rawIndex === 'number' ? rawIndex : -1
                  if (content) {
                    if (chunkIndex === 0) chatStore.clearDelegationStreamingText(correlationId)
                    chatStore.appendDelegationStreamingChunk(correlationId, chunkIndex, content)
                  }
                }
                continue
              }
              // Parent agent streaming chunk
              const rawContent = innerData?.content ?? agentEvent?.content
              const content = typeof rawContent === 'string' ? rawContent : ''

              const rawIndex = innerData?.chunk_index ?? agentEvent?.chunk_index
              const chunkIndex = typeof rawIndex === 'number' ? rawIndex : -1

              // Debug: Log if this chunk might actually be from a sub-agent (has hierarchy metadata)
              if (typeof rawHierarchyLevel === 'number' && rawHierarchyLevel > 0) {
                logger.warn('ChatArea', `streaming_chunk routed to parent but has hierarchy_level=${rawHierarchyLevel}, component=${rawComponent}, correlation_id=${rawCorrelationId}`)
              }

              if (content) {
                if (chunkIndex === 0) {
                  chatStore.clearStreamingText(actualSessionId)
                }
                chatStore.appendStreamingChunk(actualSessionId, chunkIndex, content)
              }
              continue
            }
            if (event.type === 'streaming_end') {
              continue
            }

            // Track completion events but don't clear streaming text synchronously.
            // Clearing synchronously in the same loop as appendStreamingChunk prevents
            // React from ever rendering the streaming card (state is set then cleared
            // before the next render).
            // Only track parent agent completion events for clearing streaming text
            if (!isSubAgentEvent && (event.type === 'llm_generation_end' || event.type === 'unified_completion' || event.type === 'agent_end' || event.type === 'conversation_end')) {
              hasCompletionEvent = true
            }

            // Clear delegation streaming text when delegation ends
            if (event.type === 'delegation_end') {
              const correlationId = innerData?.correlation_id ?? innerData?.delegation_id ?? agentEvent?.correlation_id ?? agentEvent?.delegation_id
              if (correlationId && typeof correlationId === 'string') {
                chatStore.clearDelegationStreamingText(correlationId)
              }
            }

            if (event.type === 'request_human_feedback' && currentTab) {
              chatStore.setTabStreaming(currentTab.tabId, false)
              chatStore.setTabCompleted(currentTab.tabId, false)
            }

            const { processWorkspaceEvent } = useWorkspaceStore.getState()
            processWorkspaceEvent(event)

            newEvents.push(event)
          }

          // Defer streaming text clear so React renders the accumulated text first.
          // Use requestAnimationFrame to ensure at least one paint with the streaming card
          // before it's replaced by the full response from the event list.
          if (hasCompletionEvent) {
            const sid = actualSessionId
            const textBeforeClear = useChatStore.getState().streamingText[sid]
            requestAnimationFrame(() => {
              // Only clear if no new streaming generation started since the defer
              const currentText = useChatStore.getState().streamingText[sid]
              if (currentText === textBeforeClear) {
                useChatStore.getState().clearStreamingText(sid)
              }
            })
          }
          
          // Process workflow-specific events (after filtering)
          if (selectedModeCategory === 'workflow') {
            const workflowStore = useWorkflowStore.getState()
            // phases removed

            for (const event of response.events as PollingEvent[]) {
              // Handle batch progress updates from polling layer (more reliable than component useEffect)
              // Note: Batch events have data directly in event.data, not event.data.data
              if (event.type === 'batch_group_start') {
                // Extract data - try event.data.data first (standard format), then event.data (direct format)
                const eventData = event.data as Record<string, unknown> | undefined
                const batchGroupStartData = (eventData?.data as Record<string, unknown>) || eventData

                const groupId = batchGroupStartData?.group_id as string | undefined
                const runFolder = batchGroupStartData?.run_folder as string | undefined
                const workspacePath = batchGroupStartData?.workspace_path as string | undefined
                const groupIndex = batchGroupStartData?.group_index as number | undefined
                const totalGroups = batchGroupStartData?.total_groups as number | undefined

                logger.debug('ChatArea', 'batch_group_start', { groupId, groupIndex, totalGroups })

                if (groupId && runFolder) {
                  workflowStore.handleBatchGroupStart(
                    groupId,
                    runFolder,
                    workspacePath,
                    groupIndex,
                    totalGroups
                  )
                }
              }

              if (event.type === 'batch_group_end') {
                // Extract data - try event.data.data first (standard format), then event.data (direct format)
                const eventData = event.data as Record<string, unknown> | undefined
                const batchGroupEndData = (eventData?.data as Record<string, unknown>) || eventData

                const groupId = batchGroupEndData?.group_id as string | undefined
                const success = batchGroupEndData?.success as boolean | undefined
                const remainingGroups = batchGroupEndData?.remaining_groups as number | undefined

                if (groupId) {
                  workflowStore.handleBatchGroupEnd(
                    groupId,
                    success,
                    remainingGroups
                  )
                }
              }

              // Handle step progress updates for auto-focus and status on canvas
              if (event.type === 'step_progress_updated') {
                // Extract data - try event.data.data first (standard format), then event.data (direct format)
                const eventData = event.data as Record<string, unknown> | undefined
                const stepProgressData = (eventData?.data as Record<string, unknown>) || eventData

                const stepId = stepProgressData?.current_step_id as string | undefined
                const status = stepProgressData?.status as string | undefined

                if (stepId && status) {
                  // Update step status in store
                  if (status === 'start') {
                    workflowStore.setCurrentStepId(stepId)
                    workflowStore.setStepStatus(stepId, 'running')
                  } else if (status === 'end') {
                    workflowStore.setStepStatus(stepId, 'completed')
                  } else if (status === 'failed') {
                    workflowStore.setStepStatus(stepId, 'failed')
                  }
                }

                // Update batch progress from step_progress_updated event (batch info is always included)
                const groupId = stepProgressData?.group_id as string | undefined
                const groupIndex = stepProgressData?.group_index as number | undefined
                const totalGroups = stepProgressData?.total_groups as number | undefined
                const runFolder = stepProgressData?.run_folder as string | undefined

                if (groupId && totalGroups !== undefined && totalGroups > 0) {
                  logger.debug('ChatArea', 'batch progress update', { groupId, groupIndex, totalGroups })
                  workflowStore.handleBatchGroupStart(
                    groupId,
                    runFolder || '',
                    undefined,
                    groupIndex,
                    totalGroups
                  )
                }
              }
            }
          }
          
          // Add events to the correct tab's event list
          if (currentTab) {
            // CRITICAL: Re-verify the tab's session ID right before storing events
            // This prevents storing events under the wrong session ID if the tab's session ID changed
            const finalTab = chatStore.getTab(currentTab.tabId)
            if (!finalTab) {
              continue
            }
            
            // Use the actual session ID from the response (source of truth)
            // If response has a different session ID, use that one
            const sessionIdForStorage = actualSessionId
            
            // Store events per session (by sessionId)
            // CRITICAL: Use the sessionId from the response (actualSessionId) as it's the source of truth
            // Multiple observers can view the same session, but events are stored per session
            addTabEvents(sessionIdForStorage, newEvents)
            
            // NOTE: Completion detection is now handled by session_status above
            // No need to parse events for completion anymore
          } else {
            // No tab - this should not happen in tab mode
            // Discard events to prevent mixing
            // NOTE: Completion detection is now handled by session_status above
            // No need to parse events for completion anymore
          }
        }
      } catch {
        // Continue polling other observers even if one fails
      }
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps -- selectedModeCategory read from store directly inside callback to avoid stale setInterval closure
  }, [getTabLastEventIndex, setTabLastEventIndex, setLastEventIndex, addTabEvents, getTabEvents, setIsStreaming, setIsCompleted, setHasActiveChat, activeSessionIds])


  // Track if we're already processing to prevent infinite loops
  const processingRef = useRef<string | null>(null)
  
  // Start centralized active sessions polling when component mounts
  useEffect(() => {
    startActiveSessionsPolling()
    return () => {
      // Note: We don't stop polling here because other components might be using it
      // The polling will be managed globally and cleaned up when app unmounts
    }
  }, [startActiveSessionsPolling])

  // Auto-restore active sessions on page load
  // This ensures that if the user refreshes the page, active chats are automatically restored
  useEffect(() => {
    // Only restore once per page load (using global flag)
    if (globalHasRestored) {
      return
    }
    
    const restoreActiveSessions = async () => {
      // Mark as restored immediately to prevent concurrent executions
      globalHasRestored = true
      
      try {
        // Wait a bit for active sessions polling to start and fetch initial data
        await new Promise(resolve => setTimeout(resolve, 500))
        
        const chatStore = useChatStore.getState()
        const activeSessions = await getActiveSessions(true) // Force refresh to get latest
        
        if (activeSessions.length === 0) {
          console.log('[AutoRestore] No active sessions found, skipping restore')
          return
        }
        
        console.log(`[AutoRestore] Found ${activeSessions.length} active session(s), checking if tabs need to be created`)
        
        // Filter to only running sessions (active ones)
        // CRITICAL: Filter based on current mode category to ensure sessions appear in correct mode
        // Workflow sessions are handled by the Running Workflows drawer in Workflow mode
        const runningSessions = activeSessions.filter(s => {
          const isRunning = s.status === 'running'
          const agentMode = s.agent_mode?.toLowerCase()
          const isWorkflow = agentMode === 'workflow'

          // Filter by current mode (chat and multi-agent both use simple agent mode)
          if (selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent') {
            // In chat/multi-agent mode, skip workflow sessions
            if (isWorkflow) {
              console.log(`[AutoRestore] Skipping ${agentMode} session in ${selectedModeCategory} mode: ${s.session_id}`)
              return false
            }
          } else {
            // Workflow mode handles its own session restore
            return false
          }
          
          if (isRunning) return true

          // Also include recently completed sessions (within last 30 minutes)
          if (s.status === 'completed' && s.last_activity) {
            const lastActivityTime = new Date(s.last_activity).getTime()
            const thirtyMinutesAgo = Date.now() - (30 * 60 * 1000)
            
            if (lastActivityTime > thirtyMinutesAgo) {
              console.log(`[AutoRestore] Including recently completed session: ${s.session_id} (completed at ${s.last_activity})`)
              return true
            }
          }

          return false
        })
        
        if (runningSessions.length === 0) {
          console.log('[AutoRestore] No running sessions found (after filtering), skipping restore')
          return
        }
        
        // Determine tab mode — match current mode so tabs appear in correct tab bar
        const tabMode = (selectedModeCategory === 'multi-agent' ? 'multi-agent' : 'chat') as 'chat' | 'multi-agent'

        // Collect persisted tabs without sessionId (restored from localStorage with sessionId: null)
        const orphanedTabs = Object.values(chatStore.chatTabs).filter(tab =>
          tab.metadata?.mode === tabMode && !tab.sessionId
        ).sort((a, b) => (a.createdAt || 0) - (b.createdAt || 0))
        let orphanIndex = 0

        // For each active session, create a tab if one doesn't exist
        for (const activeSession of runningSessions) {
          const sessionId = activeSession.session_id

          // Check if a tab already exists for this session (by sessionId match)
          const existingTab = Object.values(chatStore.chatTabs).find(tab => tab.sessionId === sessionId)

          if (existingTab) {
            console.log(`[AutoRestore] Tab already exists for active session ${sessionId}, skipping`)
            // Still switch to first session's tab
            if (runningSessions.indexOf(activeSession) === 0) {
              switchTab(existingTab.tabId)
            }
            continue
          }

          // CRITICAL: Check if this session is already being restored by another code path
          // This prevents race conditions between auto-restore and manual session detection
          if (sessionsBeingRestored.has(sessionId)) {
            console.log(`[AutoRestore] Session ${sessionId} is already being restored, skipping`)
            continue
          }

          // Mark as being restored to prevent duplicate tab creation
          sessionsBeingRestored.add(sessionId)

          // Try to re-use a persisted tab (from localStorage) instead of creating a new one
          const orphanedTab = orphanedTabs[orphanIndex]
          let targetTabId: string

          if (orphanedTab) {
            orphanIndex++
            console.log(`[AutoRestore] Re-associating persisted tab ${orphanedTab.tabId} with session ${sessionId}`)
            chatStore.updateTabSessionId(orphanedTab.tabId, sessionId)
            targetTabId = orphanedTab.tabId
          } else {
            // Create a new tab for this active session
            const sessionTitle = truncateTabTitle(activeSession.query || 'Active Chat')
            const agentMode = activeSession.agent_mode?.toLowerCase() || ''
            const defaultEventMode: 'basic' | 'advanced' | 'tiny' =
              agentMode === 'orchestrator' ? 'advanced' : 'tiny'

            try {
              console.log(`[AutoRestore] Creating tab for active session ${sessionId}: ${sessionTitle} (mode: ${tabMode})`)
              targetTabId = await createChatTab(sessionTitle, { mode: tabMode }, sessionId, defaultEventMode)
            } catch (error) {
              console.error(`[AutoRestore] Failed to create tab for active session ${sessionId}:`, error)
              sessionsBeingRestored.delete(sessionId)
              continue
            }
          }

          const targetTab = chatStore.getTab(targetTabId)

          if (!targetTab) {
            console.error(`[AutoRestore] Tab ${targetTabId} not found in store`)
            sessionsBeingRestored.delete(sessionId)
            continue
          }

          // Load session config and events
          try {
            // Get full chat session details including config
            const chatSession = await agentApi.getChatSession(sessionId)

            // Restore config from stored session
            if (chatSession.config) {
              const config = chatSession.config
              console.log(`[AutoRestore] Session config from API:`, JSON.stringify(config, null, 2))
              const configUpdate: Partial<ChatTabConfig> = {}

              // Restore selected servers (prefer enabled_servers over selected_servers)
              if (config.enabled_servers && config.enabled_servers.length > 0) {
                configUpdate.selectedServers = config.enabled_servers
              } else if (config.selected_servers && config.selected_servers.length > 0) {
                configUpdate.selectedServers = config.selected_servers
              }

              // Restore code execution mode
              if (config.use_code_execution_mode !== undefined) {
                configUpdate.useCodeExecutionMode = config.use_code_execution_mode
              }

              // Restore context summarization
              if (config.enable_context_summarization !== undefined) {
                configUpdate.enableContextSummarization = config.enable_context_summarization
              }

              // Restore LLM config
              if (config.llm_config) {
                let provider = config.llm_config.provider as string
                // Fix for invalid provider (e.g. "." or empty) - fallback to global default
                if (!provider || provider === '.' || provider.trim() === '') {
                  console.warn(`[AutoRestore] Invalid provider "${provider}" found in session config, falling back to default`)
                  provider = useLLMStore.getState().primaryConfig.provider || 'openai'
                }

                let modelId = config.llm_config.model_id || ''
                // Fix for invalid/missing model_id - fallback to global default
                if (!modelId || modelId.trim() === '') {
                  console.warn(`[AutoRestore] Invalid model_id "${modelId}" found in session config, falling back to default`)
                  modelId = useLLMStore.getState().primaryConfig.model_id || ''
                }

                const llmConfig: ExtendedLLMConfiguration = {
                  provider: provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                  model_id: modelId,
                  fallback_models: config.llm_config.fallback_models || [],
                }
                if (config.llm_config.cross_provider_fallback) {
                  llmConfig.cross_provider_fallback = {
                    provider: config.llm_config.cross_provider_fallback.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                    models: config.llm_config.cross_provider_fallback.models || [],
                  }
                }
                configUpdate.llmConfig = llmConfig
              }

              // Restore workspace file context
              if (config.file_context && Array.isArray(config.file_context)) {
                configUpdate.fileContext = config.file_context.map((item: { name?: string; path: string; type?: 'file' | 'folder' }) => ({
                  name: item.name || item.path || '',
                  path: item.path || '',
                  type: (item.type === 'folder' ? 'folder' : 'file') as 'file' | 'folder',
                }))
              }

              // Restore workspace access setting
              if (config.enable_workspace_access !== undefined) {
                configUpdate.enableWorkspaceAccess = config.enable_workspace_access
              }

              // Restore selected skills
              if (config.selected_skills && Array.isArray(config.selected_skills)) {
                configUpdate.selectedSkills = config.selected_skills
              }

              // Restore selected sub-agent templates
              if (config.selected_subagents && Array.isArray(config.selected_subagents)) {
                configUpdate.selectedSubAgents = config.selected_subagents
              }

              // Restore delegation tier config (for multi-agent sessions)
              if (config.delegation_tier_config) {
                configUpdate.delegationTierConfig = config.delegation_tier_config
              }

              // Update tab config
              if (Object.keys(configUpdate).length > 0) {
                chatStore.setTabConfig(targetTabId, configUpdate)
                console.log(`[AutoRestore] Restored config for active session ${sessionId}:`, JSON.stringify(configUpdate, null, 2))
              } else {
                console.log(`[AutoRestore] No config to restore for session ${sessionId}`)
              }
            } else {
              console.log(`[AutoRestore] No config in chatSession for session ${sessionId}`)
            }

            // Load events for this session
            const eventMode: 'basic' | 'advanced' | 'tiny' | 'micro' = (targetTab.eventMode || 'basic') as 'basic' | 'advanced' | 'tiny' | 'micro'
            const response = await agentApi.getSessionEvents(sessionId, 0, { eventMode })
            const pollingEvents: PollingEvent[] = response.events

            // CRITICAL: Use setTabEvents instead of addTabEvents to ensure correct chronological order
            // When restoring historical events, we want to replace any existing events to maintain order
            setTabEvents(sessionId, pollingEvents)
            // CRITICAL: Use last_processed_index from backend (not events.length - 1)
            // Backend tracks the actual event index which may be higher due to filtering/cleanup
            const lastIndex = response.last_processed_index ?? (pollingEvents.length > 0 ? pollingEvents.length - 1 : -1)
            setTabLastEventIndex(sessionId, lastIndex)
            console.log(`[AutoRestore] Loaded ${pollingEvents.length} events for session ${sessionId}, last_processed_index=${lastIndex}`)

            // Set streaming/completed status based on actual session status
            if (activeSession.status === 'completed') {
              chatStore.setTabStreaming(targetTabId, false)
              chatStore.setTabCompleted(targetTabId, true)
              console.log(`[AutoRestore] Restored completed session ${sessionId}`)
            } else {
              chatStore.setTabStreaming(targetTabId, true)
              chatStore.setTabCompleted(targetTabId, false)
            }

            // If this is the first active session, switch to it
            if (runningSessions.indexOf(activeSession) === 0) {
              switchTab(targetTabId)
              console.log(`[AutoRestore] Switched to first active session tab: ${targetTabId}`)
            }
          } catch (error) {
            console.error(`[AutoRestore] Failed to load events/config for session ${sessionId}:`, error)
          } finally {
            // Remove from sessionsBeingRestored after processing is complete
            sessionsBeingRestored.delete(sessionId)
          }
        }

        console.log(`[AutoRestore] Completed restoring ${runningSessions.length} active session(s)`)
      } catch (error) {
        console.error('[AutoRestore] Failed to restore active sessions:', error)
      }
    }
    
    // Restore in chat and multi-agent modes (workflow handles its own restore)
    if (selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent') {
      restoreActiveSessions()
    }
  }, [getActiveSessions, createChatTab, switchTab, setTabEvents, setTabLastEventIndex, selectedModeCategory])
  
  // Only poll tabs that have their session ID in the backend's active sessions list
  // Backend determines activity based on event activity (10 min timeout)
  // CRITICAL: Also include tabs that are streaming (user just submitted a query)
  // This ensures restored sessions start polling immediately when replying
  const tabsWithActiveSessions = useMemo(() => {
    const activeIds = activeSessionIds // Capture in closure
    const chatStore = useChatStore.getState() // Get fresh store state to check streaming status
    
    const filtered = tabsWithSessions.filter(tab => {
      // Must have session ID
      if (!tab.sessionId) {
        return false
      }
      
      // Skip completed sessions (definitely done)
      if (tab.isCompleted) {
        return false
      }
      
      // CRITICAL: Check streaming status directly from store (not from tab object)
      // This ensures we get the latest streaming status even if tabsWithSessions is stale
      const currentTab = chatStore.getTab(tab.tabId)
      const isStreaming = currentTab?.isStreaming ?? tab.isStreaming
      
      // CRITICAL: Include tabs that are streaming (user just submitted a query)
      // This handles the case where a restored session is being replied to
      // The backend might not have added it to active sessions yet, but we should poll it
      if (isStreaming) {
        return true
      }
      
      // Must be in backend's active sessions list (or we haven't checked yet - allow polling)
      // If backend says it's active, poll it even if local isStreaming is false
      // This ensures we catch events that come after stop is pressed
      if (activeIds.size > 0 && !activeIds.has(tab.sessionId)) {
        return false
      }
      
      // If backend status is unknown (empty), allow polling (backend check might be in progress)
      // Backend will mark it inactive if no events for 10 minutes
      return true
    })
    
    return filtered
  }, [tabsWithSessions, activeSessionIds, chatTabs])
  
  // Start/stop polling based on active sessions using store actions
  // Use tabsWithActiveSessions (backend-driven) instead of updatePollingState (isStreaming-based)
  useEffect(() => {
    // If there are active sessions and no polling interval, start polling
    if (tabsWithActiveSessions.length > 0 && !pollingInterval) {
      startPolling(pollEvents)
    }
    // If there are no active sessions but polling is running, stop it
    else if (tabsWithActiveSessions.length === 0 && pollingInterval) {
      stopPolling()
    }
  }, [pollingInterval, startPolling, stopPolling, pollEvents, tabsWithActiveSessions, activeSessionIds, activeTab])

  // Trigger immediate reload when event mode changes
  // This ensures events are reloaded with the new filter when user switches modes
  // Events are cleared immediately in the store, so UI will show empty state right away
  const prevEventModeRef = useRef<string | undefined>(activeTab?.eventMode)
  useEffect(() => {
    const currentEventMode = activeTab?.eventMode
    const prevEventMode = prevEventModeRef.current
    
    // Only trigger if event mode actually changed and we have a session
    if (currentEventMode && currentEventMode !== prevEventMode && activeTab?.sessionId) {
      // Update ref to current value
      prevEventModeRef.current = currentEventMode
      
      // When event mode changes, fetch events from the beginning (sinceIndex=0)
      // The store already cleared events and reset index to -1
      // We need to explicitly fetch from beginning to get all events with new filter
      const fetchFromBeginning = async () => {
        const chatStore = useChatStore.getState()
        const eventMode = currentEventMode
        const sessionId = activeTab.sessionId!
        
        try {
          // Explicitly fetch from beginning (sinceIndex=0) with new event mode
          const response = await agentApi.getSessionEvents(sessionId, 0, { eventMode })
          
          if (response.events.length > 0) {
            // Add events to store
            chatStore.addTabEvents(sessionId, response.events)
            // Update last event index
            if (response.last_processed_index !== undefined) {
              chatStore.setTabLastEventIndex(sessionId, response.last_processed_index)
            }
            // Update hasMoreOlderEvents flag
            if (response.has_more !== undefined) {
              chatStore.setTabHasMoreOlderEvents(sessionId, response.has_more)
            }
          }
        } catch (error) {
          logger.error('ChatArea', 'Failed to fetch events from beginning:', error)
        }
      }
      
      fetchFromBeginning()
    } else if (currentEventMode !== prevEventMode) {
      // Update ref even if we don't trigger reload (e.g., no sessionId)
      prevEventModeRef.current = currentEventMode
    }
  }, [activeTab?.eventMode, activeTab?.sessionId])

  // Cleanup polling on unmount
  useEffect(() => {
    const timeout = batchTimeoutRef.current
    return () => {
      // Use store's stopPolling to clean up
      if (pollingInterval) {
        stopPolling()
      }
      // Cleanup debounced function
      debouncedFlush.cancel()
      // Cleanup batch timeout if it exists
      if (timeout) {
        clearTimeout(timeout)
      }
    }
  }, [pollingInterval, stopPolling, debouncedFlush])
  
  // Simple session state detection
  useEffect(() => {
    if (!chatSessionId) {
      setSessionState('not_found')
      return
    }

    // Extract original session ID (remove timestamp if present)
    const originalSessionId = chatSessionId.includes('_') ? chatSessionId.split('_')[0] : chatSessionId
    
    // Prevent infinite loops
    if (processingRef.current === originalSessionId) {
      return
    }
    
    const handleSession = async () => {
      processingRef.current = originalSessionId
      setIsCheckingActiveSessions(true)
      setSessionState('loading')
      
      try {
        // Check if session is currently active using cached active sessions
        const { getActiveSessions } = useChatStore.getState()
        const activeSessions = await getActiveSessions()
        const activeSession = activeSessions.find(
          (session: ActiveSessionInfo) => session.session_id === originalSessionId
        )
        
        if (activeSession) {
          setSessionState('active')
          
          // CRITICAL: Create a tab for this active session if one doesn't exist
          // This ensures events can be displayed (displayEvents depends on activeTab?.sessionId)
          const chatStore = useChatStore.getState()
          let tabWithSession = Object.values(chatStore.chatTabs).find(tab => tab.sessionId === originalSessionId)
          
          // Fetch full chat session details to restore config
          let chatSession: Awaited<ReturnType<typeof agentApi.getChatSession>> | null = null
          try {
            chatSession = await agentApi.getChatSession(originalSessionId)
            console.log(`[History] Retrieved chat session for active session ${originalSessionId}:`, { 
              status: chatSession.status, 
              title: chatSession.title,
              hasConfig: !!chatSession.config 
            })
          } catch (error) {
            console.error(`[History] Failed to fetch chat session for active session ${originalSessionId}:`, error)
            // Continue without config restoration
          }
          
          if (!tabWithSession) {
            // CRITICAL: Check if this session is already being restored by auto-restore
            // This prevents race conditions creating duplicate tabs
            if (sessionsBeingRestored.has(originalSessionId)) {
              console.log(`[History] Session ${originalSessionId} is already being restored by auto-restore, waiting...`)
              // Wait a bit and check again - auto-restore should finish soon
              await new Promise(resolve => setTimeout(resolve, 1000))
              // Re-check if tab was created
              tabWithSession = Object.values(chatStore.chatTabs).find(tab => tab.sessionId === originalSessionId)
              if (tabWithSession) {
                console.log(`[History] Tab was created by auto-restore for session ${originalSessionId}, using it`)
                switchTab(tabWithSession.tabId)
                setIsCheckingActiveSessions(false)
                processingRef.current = null
                return
              }
              // Still no tab and still being restored - bail out to avoid conflict
              if (sessionsBeingRestored.has(originalSessionId)) {
                console.log(`[History] Session ${originalSessionId} still being restored, bailing out`)
                setIsCheckingActiveSessions(false)
                processingRef.current = null
                return
              }
            }

            // Mark as being restored to prevent duplicate creation
            sessionsBeingRestored.add(originalSessionId)

            // Create a tab for this active session (truncate to 3 words for display)
            const sessionTitle = truncateTabTitle(chatSessionTitle || chatSession?.title || activeSession.query || 'Active Chat')

            // Determine default event mode based on agent mode
            // orchestrator -> advanced (more complex, needs detailed view)
            // simple -> tiny (minimal view for restored sessions)
            // workflow -> tiny (minimal view for restored sessions)
            const agentMode = activeSession.agent_mode?.toLowerCase() || ''
            const defaultEventMode: 'basic' | 'advanced' | 'tiny' =
              agentMode === 'orchestrator' ? 'advanced' : 'tiny'

            try {
              const newTabId = await createChatTab(sessionTitle, { mode: 'chat' }, originalSessionId, defaultEventMode)
              tabWithSession = chatStore.getTab(newTabId)
              if (!tabWithSession) {
                console.error(`[History] Tab ${newTabId} was created but not found in store`)
                setIsLoadingHistory(false)
                processingRef.current = null
                return
              }
              
              // Restore config from stored session
              if (chatSession?.config) {
                const config = chatSession.config
                const configUpdate: Partial<ChatTabConfig> = {}
                
                // Restore selected servers (prefer enabled_servers over selected_servers)
                if (config.enabled_servers && config.enabled_servers.length > 0) {
                  configUpdate.selectedServers = config.enabled_servers
                } else if (config.selected_servers && config.selected_servers.length > 0) {
                  configUpdate.selectedServers = config.selected_servers
                }
                
                // Restore code execution mode
                if (config.use_code_execution_mode !== undefined) {
                  configUpdate.useCodeExecutionMode = config.use_code_execution_mode
                }
                
                // Restore context summarization
                if (config.enable_context_summarization !== undefined) {
                  configUpdate.enableContextSummarization = config.enable_context_summarization
                }
                
                                  // Restore LLM config
                                  if (config.llm_config) {
                                    let provider = config.llm_config.provider as string
                                    // Fix for invalid provider (e.g. "." or empty) - fallback to global default
                                    if (!provider || provider === '.' || provider.trim() === '') {
                                      console.warn(`[History] Invalid provider "${provider}" found in session config, falling back to default`)
                                      provider = useLLMStore.getState().primaryConfig.provider || 'openai'
                                    }
                
                                    let modelId = config.llm_config.model_id || ''
                                    // Fix for invalid/missing model_id - fallback to global default
                                    if (!modelId || modelId.trim() === '') {
                                      console.warn(`[History] Invalid model_id "${modelId}" found in session config, falling back to default`)
                                      modelId = useLLMStore.getState().primaryConfig.model_id || ''
                                    }
                
                                    const llmConfig: ExtendedLLMConfiguration = {
                                      provider: provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                                      model_id: modelId,
                                      fallback_models: config.llm_config.fallback_models || [],
                                    }
                                    if (config.llm_config.cross_provider_fallback) {
                                      llmConfig.cross_provider_fallback = {
                                        provider: config.llm_config.cross_provider_fallback.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                                        models: config.llm_config.cross_provider_fallback.models || [],
                                      }
                                    }
                                    configUpdate.llmConfig = llmConfig
                                  }                
                // Restore workspace file context
                if (config.file_context && Array.isArray(config.file_context)) {
                  configUpdate.fileContext = config.file_context.map((item: { name?: string; path?: string; type?: string }) => ({
                    name: item.name || item.path || '',
                    path: item.path || '',
                    type: (item.type === 'folder' ? 'folder' : 'file') as 'file' | 'folder',
                  }))
                }
                
                // Restore workspace access setting
                if (config.enable_workspace_access !== undefined) {
                  configUpdate.enableWorkspaceAccess = config.enable_workspace_access
                }

                // Restore selected skills
                if (config.selected_skills && Array.isArray(config.selected_skills)) {
                  configUpdate.selectedSkills = config.selected_skills
                }

                // Restore selected sub-agent templates
                if (config.selected_subagents && Array.isArray(config.selected_subagents)) {
                  configUpdate.selectedSubAgents = config.selected_subagents
                }

                // Restore delegation tier config (for multi-agent sessions)
                if (config.delegation_tier_config) {
                  configUpdate.delegationTierConfig = config.delegation_tier_config
                }

                // Update tab config
                if (Object.keys(configUpdate).length > 0) {
                  chatStore.setTabConfig(newTabId, configUpdate)
                  console.log(`[History] Restored config for active session ${originalSessionId}:`, configUpdate)
                }
              }

              // Switch to the new tab to display events
              switchTab(newTabId)
              console.log(`[History] Created tab ${newTabId} for active session ${originalSessionId}`)
            } catch (error) {
              console.error(`[History] Failed to create tab for active session:`, error)
              setIsLoadingHistory(false)
              processingRef.current = null
              // Remove from sessionsBeingRestored on error
              sessionsBeingRestored.delete(originalSessionId)
              return
            } finally {
              // Remove from sessionsBeingRestored after processing is complete
              sessionsBeingRestored.delete(originalSessionId)
            }
          } else {
            // Tab exists, restore config if not already restored
            if (chatSession?.config) {
              const config = chatSession.config
              const configUpdate: Partial<ChatTabConfig> = {}
              
              // Restore selected servers (prefer enabled_servers over selected_servers)
              if (config.enabled_servers && config.enabled_servers.length > 0) {
                configUpdate.selectedServers = config.enabled_servers
              } else if (config.selected_servers && config.selected_servers.length > 0) {
                configUpdate.selectedServers = config.selected_servers
              }
              
              // Restore code execution mode
              if (config.use_code_execution_mode !== undefined) {
                configUpdate.useCodeExecutionMode = config.use_code_execution_mode
              }
              
              // Restore context summarization
              if (config.enable_context_summarization !== undefined) {
                configUpdate.enableContextSummarization = config.enable_context_summarization
              }
              
              // Restore LLM config
              if (config.llm_config) {
                let provider = config.llm_config.provider as string
                // Fix for invalid provider (e.g. "." or empty) - fallback to global default
                if (!provider || provider === '.' || provider.trim() === '') {
                  console.warn(`[History] Invalid provider "${provider}" found in session config, falling back to default`)
                  provider = useLLMStore.getState().primaryConfig.provider || 'openai'
                }

                let modelId = config.llm_config.model_id || ''
                // Fix for invalid/missing model_id - fallback to global default
                if (!modelId || modelId.trim() === '') {
                  console.warn(`[History] Invalid model_id "${modelId}" found in session config, falling back to default`)
                  modelId = useLLMStore.getState().primaryConfig.model_id || ''
                }

                const llmConfig: ExtendedLLMConfiguration = {
                  provider: provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                  model_id: modelId,
                  fallback_models: config.llm_config.fallback_models || [],
                }
                if (config.llm_config.cross_provider_fallback) {
                  llmConfig.cross_provider_fallback = {
                    provider: config.llm_config.cross_provider_fallback.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                    models: config.llm_config.cross_provider_fallback.models || [],
                  }
                }
                configUpdate.llmConfig = llmConfig
              }
              
              // Restore workspace file context
              if (config.file_context && Array.isArray(config.file_context)) {
                configUpdate.fileContext = config.file_context.map((item: { name?: string; path?: string; type?: string }) => ({
                  name: item.name || item.path || '',
                  path: item.path || '',
                  type: (item.type === 'folder' ? 'folder' : 'file') as 'file' | 'folder',
                }))
              }
              
              // Restore workspace access setting
              if (config.enable_workspace_access !== undefined) {
                configUpdate.enableWorkspaceAccess = config.enable_workspace_access
              }

              // Restore selected skills
              if (config.selected_skills && Array.isArray(config.selected_skills)) {
                configUpdate.selectedSkills = config.selected_skills
              }

              // Restore selected sub-agent templates
              if (config.selected_subagents && Array.isArray(config.selected_subagents)) {
                configUpdate.selectedSubAgents = config.selected_subagents
              }

              // Restore delegation tier config (for multi-agent sessions)
              if (config.delegation_tier_config) {
                configUpdate.delegationTierConfig = config.delegation_tier_config
              }

              // Update tab config
              if (Object.keys(configUpdate).length > 0) {
                chatStore.setTabConfig(tabWithSession.tabId, configUpdate)
                console.log(`[History] Restored config for existing active session tab ${tabWithSession.tabId}:`, configUpdate)
              }
            }

            // Switch to it (events should already be loaded)
            switchTab(tabWithSession.tabId)
            console.log(`[History] Tab ${tabWithSession.tabId} already exists for active session ${originalSessionId}, switching to it`)
          }
          
          // Ensure we have a valid tab with sessionId
          if (!tabWithSession || !tabWithSession.sessionId) {
            console.error(`[History] No valid tab or sessionId found for active session ${originalSessionId}`)
            setIsLoadingHistory(false)
            processingRef.current = null
            return
          }
          
          // CRITICAL: Always use tabEvents - store by sessionId
          const sessionIdForHistory = tabWithSession.sessionId
          
          // Check if events are already loaded for this session
          const existingEvents = getTabEvents(sessionIdForHistory)
          if (existingEvents.length > 0) {
            // Events already loaded, skip reloading
            console.log(`[History] Events already loaded for active session ${sessionIdForHistory} (${existingEvents.length} events), skipping reload`)
            setIsLoadingHistory(false)
          } else {
            // First, load historical events for the active session
            setIsLoadingHistory(true)
            
            try {
              // Get event mode from the tab we just created/switched to
              const eventMode: 'basic' | 'advanced' | 'tiny' | 'micro' = (tabWithSession.eventMode || 'basic') as 'basic' | 'advanced' | 'tiny' | 'micro'
              const response = await agentApi.getSessionEvents(originalSessionId, 0, { eventMode })
              
              // Convert and set historical events
              // response.events is already PollingEvent[] from GetEventsResponse
              const pollingEvents: PollingEvent[] = response.events

              // CRITICAL: Use setTabEvents instead of addTabEvents to ensure correct chronological order
              // When restoring historical events, we want to replace any existing events to maintain order
              setTabEvents(sessionIdForHistory, pollingEvents)
              // CRITICAL: Use last_processed_index from backend (not events.length - 1)
              // Backend tracks the actual event index which may be higher due to filtering/cleanup
              const lastIndex = response.last_processed_index ?? (pollingEvents.length > 0 ? pollingEvents.length - 1 : -1)
              setTabLastEventIndex(sessionIdForHistory, lastIndex)
              console.log(`[History] Loaded ${pollingEvents.length} events for session ${sessionIdForHistory}, last_processed_index=${lastIndex}`)
              setIsLoadingHistory(false)
            } catch (error) {
              console.error('[SESSION_STATE] Failed to load historical events:', error)
              setIsLoadingHistory(false)
              addToast('Failed to load historical events', 'info')
            }
          }
          
          // Now reconnect to active session for live updates
          try {
            const reconnectResponse = await agentApi.reconnectSession(activeSession.session_id)
            
            if (reconnectResponse.session_id) {
              setIsStreaming(true)
              setIsCompleted(false)
              
              // Start polling for new events - use the store's polling management
              if (!pollingInterval) {
                console.log('[SESSION_STATE] Starting polling interval after reconnecting to active session')
                startPolling(pollEvents)
              } else {
                console.log('[SESSION_STATE] Polling interval already exists, reusing it')
              }
              
              // Note: The global pollEvents function will poll all tabs with sessionIds,
              // including this reconnected session, so we don't need separate polling logic here
              addToast('Reconnected to active session', 'success')
              
            } else {
              console.error('[SESSION_STATE] No session_id in reconnect response:', reconnectResponse)
              addToast('Failed to reconnect - no session ID', 'info')
            }
          } catch (error) {
            console.error('[SESSION_STATE] Failed to reconnect to active session:', error)
            addToast('Failed to reconnect to active session', 'info')
          }
          
          processingRef.current = null
          return
        }
        
        // Check if session exists in database (completed or active)
        try {
          // Get full chat session details including config
          const chatSession = await agentApi.getChatSession(originalSessionId)
          console.log(`[History] Retrieved chat session for ${originalSessionId}:`, { 
            status: chatSession.status, 
            title: chatSession.title,
            hasConfig: !!chatSession.config 
          })
          
          // Handle 'completed', 'active', 'stopped', and 'error' status
          // These are all non-active sessions that should load events from database
          if (chatSession.status === 'completed' || chatSession.status === 'active' || chatSession.status === 'stopped' || chatSession.status === 'error') {
            setSessionState('completed')
            
            // CRITICAL: Create a tab for this historical session if one doesn't exist
            // This ensures events can be displayed (displayEvents depends on activeTab?.sessionId)
            const chatStore = useChatStore.getState()
            let tabWithSession = Object.values(chatStore.chatTabs).find(tab => tab.sessionId === originalSessionId)
            
            if (!tabWithSession) {
              // Create a tab for this historical session (truncate to 3 words for display)
              const sessionTitle = truncateTabTitle(chatSessionTitle || chatSession.title || 'Historical Chat')
              
              // Determine default event mode based on agent mode
              // orchestrator -> advanced (more complex, needs detailed view)
              // simple -> tiny (minimal view for restored sessions)
              // workflow -> tiny (minimal view for restored sessions)
              const agentMode = chatSession.agent_mode?.toLowerCase() || ''
              const defaultEventMode: 'basic' | 'advanced' | 'tiny' = 
                agentMode === 'orchestrator' ? 'advanced' : 'tiny'
              
              try {
                const newTabId = await createChatTab(sessionTitle, { mode: 'chat' }, originalSessionId, defaultEventMode)
                tabWithSession = chatStore.getTab(newTabId)
                if (!tabWithSession) {
                  console.error(`[History] Tab ${newTabId} was created but not found in store`)
                  setIsLoadingHistory(false)
                  processingRef.current = null
                  return
                }
                
                // Restore config from stored session
                if (chatSession.config) {
                  const config = chatSession.config
                  const configUpdate: Partial<ChatTabConfig> = {}
                  
                  // Restore selected servers (prefer enabled_servers over selected_servers)
                  if (config.enabled_servers && config.enabled_servers.length > 0) {
                    configUpdate.selectedServers = config.enabled_servers
                  } else if (config.selected_servers && config.selected_servers.length > 0) {
                    configUpdate.selectedServers = config.selected_servers
                  }
                  
                  // Restore code execution mode
                  if (config.use_code_execution_mode !== undefined) {
                    configUpdate.useCodeExecutionMode = config.use_code_execution_mode
                  }
                  
                  // Restore context summarization
                  if (config.enable_context_summarization !== undefined) {
                    configUpdate.enableContextSummarization = config.enable_context_summarization
                  }
                  
                  // Restore LLM config
                  if (config.llm_config) {
                    let provider = config.llm_config.provider as string
                    // Fix for invalid provider (e.g. "." or empty) - fallback to global default
                    if (!provider || provider === '.' || provider.trim() === '') {
                      console.warn(`[History] Invalid provider "${provider}" found in session config, falling back to default`)
                      provider = useLLMStore.getState().primaryConfig.provider || 'openai'
                    }

                    let modelId = config.llm_config.model_id || ''
                    // Fix for invalid/missing model_id - fallback to global default
                    if (!modelId || modelId.trim() === '') {
                      console.warn(`[History] Invalid model_id "${modelId}" found in session config, falling back to default`)
                      modelId = useLLMStore.getState().primaryConfig.model_id || ''
                    }

                    const llmConfig: ExtendedLLMConfiguration = {
                      provider: provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                      model_id: modelId,
                      fallback_models: config.llm_config.fallback_models || [],
                    }
                    if (config.llm_config.cross_provider_fallback) {
                      llmConfig.cross_provider_fallback = {
                        provider: config.llm_config.cross_provider_fallback.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                        models: config.llm_config.cross_provider_fallback.models || [],
                      }
                    }
                    configUpdate.llmConfig = llmConfig
                  }
                  
                  // Restore workspace file context
                  if (config.file_context && Array.isArray(config.file_context)) {
                    configUpdate.fileContext = config.file_context.map((item: { name?: string; path?: string; type?: string }) => ({
                      name: item.name || item.path || '',
                      path: item.path || '',
                      type: (item.type === 'folder' ? 'folder' : 'file') as 'file' | 'folder',
                    }))
                  }
                  
                  // Restore workspace access setting
                  if (config.enable_workspace_access !== undefined) {
                    configUpdate.enableWorkspaceAccess = config.enable_workspace_access
                  }

                  // Restore selected skills
                  if (config.selected_skills && Array.isArray(config.selected_skills)) {
                    configUpdate.selectedSkills = config.selected_skills
                  }

                  // Restore selected sub-agent templates
                  if (config.selected_subagents && Array.isArray(config.selected_subagents)) {
                    configUpdate.selectedSubAgents = config.selected_subagents
                  }

                  // Restore delegation tier config (for multi-agent sessions)
                  if (config.delegation_tier_config) {
                    configUpdate.delegationTierConfig = config.delegation_tier_config
                  }

                  // Update tab config
                  if (Object.keys(configUpdate).length > 0) {
                    chatStore.setTabConfig(newTabId, configUpdate)
                    console.log(`[History] Restored config for session ${originalSessionId}:`, configUpdate)
                  }
                }

                // Restore agent mode (stored separately in agent_mode field)
                if (chatSession.agent_mode) {
                  // Agent mode is already used when creating the tab, but we can log it
                  console.log(`[History] Session ${originalSessionId} agent_mode: ${chatSession.agent_mode}`)
                }
                
                // Switch to the new tab to display events
                switchTab(newTabId)
                console.log(`[History] Created tab ${newTabId} for historical session ${originalSessionId}`)
              } catch (error) {
                console.error(`[History] Failed to create tab for historical session:`, error)
                setIsLoadingHistory(false)
                processingRef.current = null
                return
              }
            } else {
              // Tab exists, restore config if available
              if (chatSession.config) {
                const config = chatSession.config
                const configUpdate: Partial<ChatTabConfig> = {}
                
                // Restore selected servers (prefer enabled_servers over selected_servers)
                if (config.enabled_servers && config.enabled_servers.length > 0) {
                  configUpdate.selectedServers = config.enabled_servers
                } else if (config.selected_servers && config.selected_servers.length > 0) {
                  configUpdate.selectedServers = config.selected_servers
                }
                
                // Restore code execution mode
                if (config.use_code_execution_mode !== undefined) {
                  configUpdate.useCodeExecutionMode = config.use_code_execution_mode
                }
                
                // Restore context summarization
                if (config.enable_context_summarization !== undefined) {
                  configUpdate.enableContextSummarization = config.enable_context_summarization
                }
                
                                  // Restore LLM config
                                  if (config.llm_config) {
                                    let provider = config.llm_config.provider as string
                                    // Fix for invalid provider (e.g. "." or empty) - fallback to global default
                                    if (!provider || provider === '.' || provider.trim() === '') {
                                      console.warn(`[History] Invalid provider "${provider}" found in session config, falling back to default`)
                                      provider = useLLMStore.getState().primaryConfig.provider || 'openai'
                                    }
                
                                    let modelId = config.llm_config.model_id || ''
                                    // Fix for invalid/missing model_id - fallback to global default
                                    if (!modelId || modelId.trim() === '') {
                                      console.warn(`[History] Invalid model_id "${modelId}" found in session config, falling back to default`)
                                      modelId = useLLMStore.getState().primaryConfig.model_id || ''
                                    }
                
                                    const llmConfig: ExtendedLLMConfiguration = {
                                      provider: provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                                      model_id: modelId,
                                      fallback_models: config.llm_config.fallback_models || [],
                                    }
                                    if (config.llm_config.cross_provider_fallback) {
                                      llmConfig.cross_provider_fallback = {
                                        provider: config.llm_config.cross_provider_fallback.provider as 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure',
                                        models: config.llm_config.cross_provider_fallback.models || [],
                                      }
                                    }
                                    configUpdate.llmConfig = llmConfig
                                  }                
                // Restore workspace file context
                if (config.file_context && Array.isArray(config.file_context)) {
                  configUpdate.fileContext = config.file_context.map((item: { name?: string; path?: string; type?: string }) => ({
                    name: item.name || item.path || '',
                    path: item.path || '',
                    type: (item.type === 'folder' ? 'folder' : 'file') as 'file' | 'folder',
                  }))
                }
                
                // Restore workspace access setting
                if (config.enable_workspace_access !== undefined) {
                  configUpdate.enableWorkspaceAccess = config.enable_workspace_access
                }

                // Restore selected skills
                if (config.selected_skills && Array.isArray(config.selected_skills)) {
                  configUpdate.selectedSkills = config.selected_skills
                }

                // Restore selected sub-agent templates
                if (config.selected_subagents && Array.isArray(config.selected_subagents)) {
                  configUpdate.selectedSubAgents = config.selected_subagents
                }

                // Restore delegation tier config (for multi-agent sessions)
                if (config.delegation_tier_config) {
                  configUpdate.delegationTierConfig = config.delegation_tier_config
                }

                // Update tab config
                if (Object.keys(configUpdate).length > 0) {
                  chatStore.setTabConfig(tabWithSession.tabId, configUpdate)
                  console.log(`[History] Restored config for existing tab ${tabWithSession.tabId}:`, configUpdate)
                }
              }

              // Switch to it (events should already be loaded)
              switchTab(tabWithSession.tabId)
              
              // Check/Update event mode if needed (e.g. if created as tiny but needs advanced)
              const agentMode = chatSession.agent_mode?.toLowerCase() || ''
              const targetEventMode: 'basic' | 'advanced' | 'tiny' = 
                agentMode === 'orchestrator' ? 'advanced' : 'tiny'
              
              if (tabWithSession.eventMode !== targetEventMode) {
                 const chatStore = useChatStore.getState()
                 chatStore.setTabEventMode(tabWithSession.tabId, targetEventMode)
                 console.log(`[History] Updated event mode for tab ${tabWithSession.tabId} to ${targetEventMode}`)
              }
              
              console.log(`[History] Tab ${tabWithSession.tabId} already exists for session ${originalSessionId}, switching to it`)
            }
            
            // Ensure we have a valid tab with sessionId
            if (!tabWithSession || !tabWithSession.sessionId) {
              console.error(`[History] No valid tab or sessionId found for session ${originalSessionId}`)
              setIsLoadingHistory(false)
              processingRef.current = null
              return
            }
            
            const sessionIdForHistory = tabWithSession.sessionId
            
            // Check if events are already loaded for this session
            const existingEvents = getTabEvents(sessionIdForHistory)
            if (existingEvents.length > 0) {
              // Events already loaded, skip reloading
              console.log(`[History] Events already loaded for session ${sessionIdForHistory} (${existingEvents.length} events), skipping reload`)
              setIsCompleted(true)
              setIsStreaming(false)
              setHasActiveChat(false)
              setIsLoadingHistory(false)
              processingRef.current = null
              return
            }
            
            // Load historical events from database (special code path for sidebar clicks - no polling)
            setIsLoadingHistory(true)
            try {
            // Get event mode from the tab we just created/switched to
            const eventMode: 'basic' | 'advanced' | 'tiny' | 'micro' = (tabWithSession.eventMode || 'basic') as 'basic' | 'advanced' | 'tiny' | 'micro'
              console.log(`[History] Loading events from database for completed session ${originalSessionId} with eventMode: ${eventMode}`)
              
              // Use database endpoint (not polling endpoint) for completed sessions
              // Backend now returns the same structure as polling API (events.Event[])
              const dbResponse = await agentApi.getChatSessionEvents(originalSessionId, 1000, 0)
              
              console.log(`[History] Database API response for session ${originalSessionId}:`, {
                eventCount: dbResponse.events?.length || 0,
                total: dbResponse.total,
                limit: dbResponse.limit,
                offset: dbResponse.offset
              })
            
              // Backend now returns PollingEvent[] directly (same structure as polling API)
              // No conversion needed - use events directly
              const allPollingEvents: PollingEvent[] = (dbResponse.events || []) as PollingEvent[]
              
              // Apply event mode filtering (frontend-side filtering)
              const filteredEvents = allPollingEvents.filter(event => {
                if (!event.type) return false
                return shouldShowEventByMode(event.type, eventMode)
              })
            
            // CRITICAL: Use setTabEvents instead of addTabEvents to ensure correct chronological order
            // When restoring historical events, we want to replace any existing events to maintain order
            setTabEvents(sessionIdForHistory, filteredEvents)
            setTabLastEventIndex(sessionIdForHistory, filteredEvents.length > 0 ? filteredEvents.length - 1 : -1)
              console.log(`[History] Loaded ${filteredEvents.length} events (${allPollingEvents.length} total) for completed session ${sessionIdForHistory}`)
              
              if (filteredEvents.length === 0 && allPollingEvents.length === 0) {
                console.warn(`[History] No events found for session ${originalSessionId}. This might mean:`)
                console.warn(`  - Events were not stored in the backend`)
                console.warn(`  - Session ID mismatch between chat_sessions and events tables`)
                console.warn(`  - Events were deleted`)
              }
            } catch (error) {
              console.error(`[History] Failed to load events for session ${originalSessionId}:`, error)
              addToast('Failed to load chat history events', 'error')
            } finally {
              setIsLoadingHistory(false)
            }
            // Deprecated: setTotalEvents removed
            setIsCompleted(true)
            setIsStreaming(false)
            setHasActiveChat(false)
            setIsLoadingHistory(false)
            processingRef.current = null
            return
          } else {
            console.log(`[History] Session ${originalSessionId} found but has unexpected status: ${chatSession.status}`)
          }
        } catch (error: unknown) {
          // Session not found in database or other error
          const axiosError = error as { response?: { status?: number }; message?: string }
          if (axiosError?.response?.status === 404) {
            console.log(`[History] Session ${originalSessionId} not found in database (404)`)
          } else {
            console.error(`[History] Error loading chat session ${originalSessionId}:`, error)
          }
        }
        
        // Session not found
        setSessionState('not_found')
        
      } catch (error) {
        console.error('[SESSION_STATE] Error:', error)
        setSessionState('error')
      } finally {
        setIsCheckingActiveSessions(false)
        processingRef.current = null
      }
    }

    handleSession()
  }, [chatSessionId, addToast]) // eslint-disable-line react-hooks/exhaustive-deps

  const stopStreaming = useCallback(async () => {
    const chatStore = useChatStore.getState()
    
    // DO NOT stop polling - let backend determine activity based on events
    // Backend will mark session as inactive after 10 minutes of no events
    // This ensures we catch any pending events after stop is pressed
    
    // Update UI state only (isStreaming is UI-only, not used for polling decisions)
    setIsStreaming(false) // UI: Hide stop button, show send button
    
    // Update active tab's streaming status (UI feedback only)
    if (activeTab) {
      chatStore.setTabStreaming(activeTab.tabId, false) // UI: Hide stop button, show send button
    }

    // Call backend to stop the agent execution (preserves conversation history)
    // CRITICAL: Only use the active tab's session ID - never fall back to global sessionId
    // Falling back to global sessionId could stop a different tab's session
    const sessionIdToStop = activeTab?.sessionId
    if (!sessionIdToStop) {
      logger.warn('ChatArea', 'No session ID available for active tab')
      return
    }

    try {
      await agentApi.stopSession(sessionIdToStop)
    } catch (error) {
      logger.error('ChatArea', 'Failed to stop session:', error)
    }

    // Mark tab as completed so queued messages get auto-sent
    if (activeTab) {
      chatStore.setTabCompleted(activeTab.tabId, true)
    }

    // Deprecated: setLastEventCount removed
  }, [setIsStreaming, activeTab])

  // Store execution options for use in the request
  const executionOptionsRef = useRef<ExecutionOptions | undefined>(undefined)

  // Helper: reset streaming state (replaces 4 duplicated blocks)
  const resetStreamingState = useCallback((tabId?: string) => {
    const store = useChatStore.getState()
    store.setIsStreaming(false)
    store.setHasActiveChat(false)
    if (tabId) store.setTabStreaming(tabId, false)
  }, [])

  // Wrapper function to submit query with the current local query
  const submitQueryWithQuery = useCallback(async (query: string, executionOptions?: ExecutionOptions) => {
    // Get fresh tab state from store to avoid stale closure issues
    const chatStore = useChatStore.getState()
    const freshActiveTab = activeTab?.tabId ? chatStore.chatTabs[activeTab.tabId] : activeTab

    executionOptionsRef.current = executionOptions

    // Early validation
    if (!query?.trim()) {
      logger.warn('ChatArea', 'Empty query, returning early')
      return
    }

    if (selectedModeCategory === 'workflow' && !isRequiredFolderSelected) {
      logger.error('ChatArea', 'Workflow folder required for workflow mode')
      return
    }

    // Stop any ongoing streaming
    if (isStreaming) {
      await stopStreaming()
    }

    // Resolve or create tab
    const resolved = await resolveOrCreateTab({ freshActiveTab, selectedModeCategory })
    if (!resolved) return
    const { tab: currentTab, sessionId: tabSessionId } = resolved

    // Build file context
    let effectiveFileContext: Array<{ name: string; path: string; type: 'file' | 'folder' }> = []
    if ((selectedModeCategory === 'chat' || selectedModeCategory === 'multi-agent') && currentTab?.config) {
      effectiveFileContext = currentTab.config.fileContext
    } else if (selectedModeCategory === 'workflow' && activeWorkflowPreset?.selectedFolder) {
      const folderPath = activeWorkflowPreset.selectedFolder.filepath
      effectiveFileContext = [{
        name: folderPath.split('/').pop() || folderPath,
        path: folderPath,
        type: (activeWorkflowPreset.selectedFolder.type || 'folder') as 'file' | 'folder'
      }]
    }

    const queryWithContext = effectiveFileContext.length > 0
      ? `${query.trim()}\n\n📁 Files in context: ${effectiveFileContext.map((file: { path: string }) => file.path).join(', ')}`
      : query.trim()

    if (selectedModeCategory === 'workflow') {
      useAppStore.getState().setCurrentQuery(queryWithContext)
    }

    // Add user message event
    chatStore.addTabEvents(tabSessionId, [createUserMessageEvent(query.trim())])

    // Enable auto-scroll and scroll to bottom
    chatStore.setAutoScroll(true)
    setTimeout(() => { scrollToBottom('smooth') }, 50)

    // Clear query text
    useAppStore.getState().setCurrentQuery('')

    // Preserve final response as completion event if needed
    const eventsToCheck = chatStore.getTabEvents(tabSessionId)
    const hasCompletionEvent = eventsToCheck.some(event =>
      event.type === 'unified_completion' || event.type === 'agent_end'
    )
    if (finalResponse && !hasCompletionEvent) {
      const completionEvent: PollingEvent = {
        id: `completion-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
        type: 'unified_completion',
        timestamp: new Date().toISOString(),
        data: {
          unified_completion: {
            content: finalResponse,
            timestamp: new Date().toISOString()
          }
        } as PollingEvent['data']
      }
      chatStore.addTabEvents(tabSessionId, [completionEvent])
    }

    // Reset UI state for new query
    chatStore.setFinalResponse('')
    chatStore.setIsCompleted(false)
    chatStore.setIsStreaming(true)
    chatStore.setHasActiveChat(true)
    chatStore.setTabCompleted(currentTab.tabId, false)
    chatStore.setTabStreaming(currentTab.tabId, true)

    // Start polling immediately
    if (!chatStore.pollingInterval) {
      startPolling(pollEvents)
    }

    processedCompletionEventsRef.current.clear()

    try {
      // Get active presets for the current mode
      const presetStore = useGlobalPresetStore.getState()
      const chatPreset = correctAgentMode === 'simple' ? presetStore.getActivePreset('chat') : null
      const workflowPreset = correctAgentMode === 'workflow' ? (selectedWorkflowPreset ? presetStore.getActivePreset('workflow') : null) : null
      const activePreset = workflowPreset || chatPreset

      const presetTools = activePreset?.selectedTools || []
      const filteredPresetTools = presetTools.filter(t => !t.endsWith(':*'))

      const chatPresetId = chatPreset?.id || null
      const workflowPresetId = workflowPreset?.id || null

      // Determine mode flags using helper
      const useCodeExecutionMode = determineModeFlag({
        correctAgentMode,
        selectedModeCategory: selectedModeCategory || '',
        presetValue: activePreset?.useCodeExecutionMode,
        tabConfigValue: currentTab?.config?.useCodeExecutionMode,
      })
      const useToolSearchMode = determineModeFlag({
        correctAgentMode,
        selectedModeCategory: selectedModeCategory || '',
        presetValue: activePreset?.useToolSearchMode,
        tabConfigValue: currentTab?.config?.useToolSearchMode,
      })

      // Build LLM config
      const isMultiAgentMode = selectedModeCategory === 'multi-agent'
      const llmStore = useLLMStore.getState()
      const baseLLMConfig = ((selectedModeCategory === 'chat' || isMultiAgentMode) && currentTab?.config)
        ? currentTab.config.llmConfig
        : llmStore.primaryConfig
      const tierConfig = llmStore.delegationTierConfig
      const effectiveLLMConfig: ExtendedLLMConfiguration = (isMultiAgentMode && tierConfig?.high?.provider && tierConfig?.high?.model_id)
        ? { ...baseLLMConfig, provider: tierConfig.high.provider as ExtendedLLMConfiguration['provider'], model_id: tierConfig.high.model_id }
        : baseLLMConfig

      const llmConfigWithApiKeys = buildLLMConfigWithApiKeys(effectiveLLMConfig)

      // Build request payload
      const requestPayload = buildQueryRequestPayload({
        queryWithContext,
        correctAgentMode,
        selectedModeCategory,
        enabledTools,
        effectiveServers,
        currentTab,
        effectiveLLMConfig,
        llmConfigWithApiKeys,
        useCodeExecutionMode,
        useToolSearchMode,
        executionOptions: executionOptionsRef.current,
        workflowPresetId,
        chatPresetId,
        filteredPresetTools,
        hasActivePreset: !!activePreset,
      })

      // Validate execution groups for workflow mode
      if (correctAgentMode === 'workflow' && requestPayload.execution_options) {
        const validationError = validateExecutionGroups(requestPayload.execution_options)
        if (validationError) {
          chatStore.addToast(validationError, 'warning')
          resetStreamingState(currentTab.tabId)
          return
        }
      }

      // Set session ID and submit
      chatStore.setSessionId(tabSessionId)
      const response = await agentApi.startQuery(requestPayload, tabSessionId)

      if (response.status === 'started' || response.status === 'workflow_started') {
        const responseSessionId = response.session_id || response.query_id
        if (!responseSessionId) {
          logger.error('ChatArea', 'No sessionId in response')
          resetStreamingState(currentTab.tabId)
          return
        }

        chatStore.setSessionId(responseSessionId)
        chatStore.updateTabSessionId(currentTab.tabId, responseSessionId)
        chatStore.setTabStreaming(currentTab.tabId, true)
        chatStore.setTabCompleted(currentTab.tabId, false)

        // Reactivate historical session if needed
        const currentSessionState = useChatStore.getState().sessionState
        if (currentSessionState === 'completed' || currentSessionState === 'error') {
          chatStore.setSessionState('active')
        }

        // Refresh active sessions cache then start polling
        const startPollingAfterRefresh = () => {
          const currentPollingInterval = useChatStore.getState().pollingInterval
          if (!currentPollingInterval) {
            startPolling(pollEvents)
          }
          setTimeout(() => { pollEvents() }, 150)
        }

        getActiveSessions(true)
          .then(startPollingAfterRefresh)
          .catch(error => {
            logger.error('ChatArea', 'Failed to refresh active sessions cache:', error)
            startPollingAfterRefresh()
          })
      } else {
        logger.error('ChatArea', 'Backend error:', response)
        resetStreamingState(currentTab.tabId)
      }
    } catch (error) {
      logger.error('ChatArea', 'Failed to submit query:', error)
      resetStreamingState(currentTab.tabId)
    }

  }, [correctAgentMode, selectedModeCategory, isRequiredFolderSelected, isStreaming, stopStreaming, finalResponse, startPolling, effectiveServers, enabledTools, selectedWorkflowPreset, activeWorkflowPreset, pollEvents, processedCompletionEventsRef, activeTab, scrollToBottom, getActiveSessions, resetStreamingState])

  // Auto-send queued messages one by one when chat completes
  const prevIsCompletedRef = useRef<boolean>(false)
  const isProcessingQueueRef = useRef<boolean>(false)
  
  useEffect(() => {
    const currentIsCompleted = activeTab?.isCompleted ?? false
    const prevIsCompleted = prevIsCompletedRef.current
    
    // Log when execution completes or stops
    if (prevIsCompleted !== currentIsCompleted) {
      logger.debug('ChatArea', 'Completion state changed', {
        prev: prevIsCompleted, current: currentIsCompleted, tabId: activeTab?.tabId
      })
    }
    
    // Check if chat just completed (transitioned from false to true)
    if (!prevIsCompleted && currentIsCompleted && activeTab && !isProcessingQueueRef.current) {
      const queuedMessages = activeTab.config?.queuedMessages || []

      if (queuedMessages.length > 0) {
        logger.debug('ChatArea', 'Processing queued messages:', queuedMessages.length)
        isProcessingQueueRef.current = true

        // Get the first message from queue
        const messageToSend = queuedMessages[0]
        const remainingMessages = queuedMessages.slice(1)

        // Update queue to remove the message we're about to send
        const chatStore = useChatStore.getState()
        chatStore.setTabConfig(activeTab.tabId, { queuedMessages: remainingMessages })

        // Small delay to ensure completion state is fully processed
        setTimeout(async () => {
          logger.debug('ChatArea', 'Auto-sending queued message')
          try {
            await submitQueryWithQuery(messageToSend.trim())
          } catch (error) {
            logger.error('ChatArea', 'Failed to send queued message:', error)
            // Re-add the failed message back to the front of the queue
            const currentChatStore = useChatStore.getState()
            const currentQueue = currentChatStore.getTabConfig(activeTab.tabId)?.queuedMessages || []
            currentChatStore.setTabConfig(activeTab.tabId, {
              queuedMessages: [messageToSend, ...currentQueue]
            })
            // Show error toast
            addToast('Failed to send queued message. It has been re-queued.', 'error')
          } finally {
            // Reset processing flag after a delay to allow the new chat to start
            setTimeout(() => {
              isProcessingQueueRef.current = false
            }, 500)
          }
        }, 100)
      }
    }
    
    // Update ref
    prevIsCompletedRef.current = currentIsCompleted
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activeTab?.isCompleted, activeTab?.config?.queuedMessages, activeTab?.tabId, submitQueryWithQuery])

  // Handle new chat - clear backend session and reset all chat state
  const handleNewChat = useCallback(async () => {
    // Clear conversation history from backend first (if sessionId is available)
    const currentSessionId = getSessionId()
    const sessionIdToClear = activeTab?.sessionId || currentSessionId
    if (sessionIdToClear) {
      try {
        await agentApi.clearSession(sessionIdToClear)
      } catch (error) {
        logger.error('ChatArea', 'Failed to clear session:', error)
        // Continue with frontend reset even if backend clear fails
      }
    }
    
    // For workflow mode, preserve the selected preset but reset workflow phase
    if (selectedModeCategory === 'workflow' && selectedWorkflowPreset) {
      // Keep the preset selected, just reset the workflow phase to default
      const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
      setCurrentWorkflowPhase(defaultPhase)
      // Don't clear selectedWorkflowPreset or currentWorkflowQueryId
    } else {
      // For other modes, clear workflow state completely
      clearWorkflowState()
    }
    
    // Reset frontend state
    resetChatState()
    
    // Clear queued messages if any
    if (activeTab) {
      const chatStore = useChatStore.getState()
      chatStore.setTabConfig(activeTab.tabId, { queuedMessages: [] })
      isProcessingQueueRef.current = false
    }
    
    // Explicitly reset events and tracking for new chat
    // Note: Using tabEvents now, not global events
    // Events are cleared when tab is removed/cleared
    setLastEventIndex(-1)
    processedCompletionEventsRef.current.clear()
    
    // Clear guidance state
    // Reset session ID for the active tab (will generate a new one on next query)
    resetSessionId()
    
    // Call the parent's new chat handler
    onNewChat()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [clearWorkflowState, resetChatState, onNewChat, activeTab?.sessionId, activeTab?.tabId, selectedModeCategory, selectedWorkflowPreset, setCurrentWorkflowPhase, setLastEventIndex])

  // Refresh workflow presets function
  const refreshWorkflowPresets = useCallback(async () => {
    if (workflowModeHandlerRef.current) {
      await workflowModeHandlerRef.current.refreshPresets()
    }
  }, [])

  // Expose methods to parent component
  useImperativeHandle(ref, () => ({
    handleNewChat,
    resetChatState,
    refreshWorkflowPresets,
    submitQuery: submitQueryWithQuery,
    getEvents: () => displayEvents,
    isStreaming,
    currentWorkflowPhase
  }), [handleNewChat, resetChatState, refreshWorkflowPresets, submitQueryWithQuery, displayEvents, isStreaming, currentWorkflowPhase])

  return (
    <div className="flex flex-col h-full min-w-0" data-testid="chat-area-container">
      {/* Preset Selection Overlay */}
      {showPresetSelection && pendingModeCategory && (
        <PresetSelectionOverlay
          isOpen={showPresetSelection}
          onClose={handlePresetSelectionClose}
          onPresetSelected={handlePresetSelected}
          modeCategory={pendingModeCategory}
          setCurrentQuery={setCurrentQuery}
        />
      )}

      {/* Mode Switch Dialog */}
      {showModeSwitchDialog && pendingModeSwitch && (
        <ModeSwitchDialog
          isOpen={showModeSwitchDialog}
          onCancel={handleModeSwitchCancel}
          onConfirm={handleModeSwitchConfirm}
          currentModeCategory={selectedModeCategory}
          newModeCategory={pendingModeSwitch}
        />
      )}

      {/* Header - hidden when used inside WorkflowLayout */}
      {!hideHeader && (
      <ChatHeader
        chatSessionTitle={chatSessionTitle}
        chatSessionId={chatSessionId}
        sessionState={sessionState === 'not_found' ? 'not-found' : sessionState}
      />
      )}


      {/* Chat Content - Separated to prevent input re-renders */}
      <div ref={chatContentRef} className={`flex-1 overflow-y-auto overflow-x-hidden min-w-0 relative ${compact ? 'text-sm' : ''}`}>
        
        <div className={`min-w-0 ${compact ? 'px-2 pb-2' : 'px-4 pb-4'}`}>
          {/* Loading indicator for historical events */}
          {isLoadingHistory && (
            <div className={`flex items-center justify-center ${compact ? 'py-4' : 'py-8'}`}>
              <div className="flex items-center gap-3 text-gray-600 dark:text-gray-400">
                <div className={`${compact ? 'w-4 h-4' : 'w-5 h-5'} border-2 border-gray-300 dark:border-gray-600 border-t-blue-600 dark:border-t-blue-400 rounded-full animate-spin`}></div>
                <span className={compact ? 'text-xs' : 'text-sm'}>Loading chat history...</span>
              </div>
            </div>
          )}

          {/* Loading indicator for active session checking */}
          {isCheckingActiveSessions && (
            <div className={`flex items-center justify-center ${compact ? 'py-4' : 'py-8'}`}>
              <div className="flex items-center gap-3 text-gray-600 dark:text-gray-400">
                <div className={`${compact ? 'w-4 h-4' : 'w-5 h-5'} border-2 border-gray-300 dark:border-gray-600 border-t-green-600 dark:border-t-green-400 rounded-full animate-spin`}></div>
                <span className={compact ? 'text-xs' : 'text-sm'}>Checking for active session...</span>
              </div>
            </div>
          )}

          {/* Active session indicator */}
          {sessionState === 'active' && (
            <div className={`flex items-center justify-center ${compact ? 'py-2' : 'py-4'}`}>
              <div className={`flex items-center gap-2 ${compact ? 'px-2 py-1' : 'px-3 py-2'} bg-green-100 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg`}>
                <div className={`${compact ? 'w-1.5 h-1.5' : 'w-2 h-2'} bg-green-500 rounded-full animate-pulse`}></div>
                <span className={`${compact ? 'text-xs' : 'text-sm'} text-green-700 dark:text-green-300 font-medium`}>Live Session - Reconnected</span>
              </div>
            </div>
          )}

          {/* Session error indicator */}
          {sessionState === 'error' && (
            <div className={`flex items-center justify-center ${compact ? 'py-2' : 'py-4'}`}>
              <div className={`flex items-center gap-2 ${compact ? 'px-2 py-1' : 'px-3 py-2'} bg-red-100 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg`}>
                <svg className={`${compact ? 'w-3 h-3' : 'w-4 h-4'} text-red-600 dark:text-red-400`} fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span className={`${compact ? 'text-xs' : 'text-sm'} text-red-700 dark:text-red-300 font-medium`}>Session Error - Unable to reconnect</span>
              </div>
            </div>
          )}

          {/* Show workflow explanation when in workflow mode but no preset selected */}
          {selectedModeCategory === 'workflow' && (
            <WorkflowExplanation agentMode={correctAgentMode} selectedWorkflowPreset={selectedWorkflowPreset} />
          )}


          {/* Show Deep Search explanation when in Deep Search mode */}


        {selectedModeCategory === 'workflow' ? (
          <WorkflowModeHandler
            ref={workflowModeHandlerRef}
            onPresetSelected={handleWorkflowPresetSelected}
            onPresetCleared={handleWorkflowPresetCleared}
            onWorkflowPhaseChange={setCurrentWorkflowPhase}
          >
            {/* Empty State - Show when no events and not in historical session */}
            {!chatSessionId && displayEvents.length === 0 && !isStreaming && (
              <ModeEmptyState modeCategory={selectedModeCategory} />
            )}
            
            {activeTab?.sessionId && (
              <EventDisplay events={displayEvents} onFeedbackSubmitted={handleFeedbackSubmitted} compact={compact} flatHierarchy={true} sessionId={activeTab.sessionId} />
            )}
          </WorkflowModeHandler>
        ) : (
          <>
            {/* Empty State - Show when no events and not in historical session */}
            {!chatSessionId && displayEvents.length === 0 && !isStreaming && (
              <ModeEmptyState modeCategory={selectedModeCategory} />
            )}

            {activeTab?.sessionId && (
              <EventDisplay events={displayEvents} onFeedbackSubmitted={handleFeedbackSubmitted} compact={compact} sessionId={activeTab.sessionId} />
            )}
          </>
        )}
        </div>
      </div>

      {/* Input Area - Completely isolated from event updates, hidden in workflow mode */}
      {/* Enable input for historical sessions so users can continue conversations */}
      {!hideInput && (
        <ChatInput
          onSubmit={submitQueryWithQuery}
          onStopStreaming={stopStreaming}
        />
      )}
      
      {/* Toast notifications */}
      <ToastContainer 
        toasts={filteredToasts} 
        onRemoveToast={removeToast} 
      />
    </div>
  )
})

ChatAreaInner.displayName = 'ChatAreaInner'

// Main ChatArea component (EventModeProvider should be provided by parent)
const ChatArea = ChatAreaInner

ChatArea.displayName = 'ChatArea'
ChatArea.whyDidYouRender = true

export default ChatArea

