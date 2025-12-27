import { useEffect, useRef, useCallback, forwardRef, useImperativeHandle, useMemo, useState } from 'react'
import debounce from 'lodash.debounce'
import { agentApi, resetSessionId, getSessionId } from '../services/api'
import type { PollingEvent, ActiveSessionInfo } from '../services/api-types'
import type { AgentMode } from '../stores/types'
import { ChatInput } from './ChatInput'
import { EventDisplay } from './EventDisplay'
import { WorkflowModeHandler, type WorkflowModeHandlerRef } from './workflow'
import { ToastContainer } from './ui/Toast'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { useWorkflowStore } from '../stores/useWorkflowStore'
import { WorkflowExplanation } from './WorkflowExplanation'
import { useAppStore, useLLMStore, useMCPStore, useChatStore } from '../stores'
import { useModeStore } from '../stores/useModeStore'
import { ModeEmptyState } from './ModeEmptyState'
import { PresetSelectionOverlay } from './PresetSelectionOverlay'
import { usePresetApplication } from '../stores/useGlobalPresetStore'
import { ModeSwitchDialog } from './ui/ModeSwitchDialog'
import { ChatHeader } from './ChatHeader'
import type { ChatTab } from '../stores/useChatStore'

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


// Inner component that can use the EventMode context
const ChatAreaInner = forwardRef<ChatAreaRef, ChatAreaProps>((props, ref) => {
  const { onNewChat, hideHeader = false, hideInput = false, compact = false, tabId } = props
  
  // Store subscriptions
  const { 
    agentMode, 
    setCurrentQuery,
    chatFileContext,
    clearFileContext,
    chatSessionId,
    chatSessionTitle
  } = useAppStore()
  
  const { selectedModeCategory, getAgentModeFromCategory } = useModeStore()
  const { getActivePreset, applyPreset, clearActivePreset, currentPresetServers, currentPresetTools } = usePresetApplication()
  
  // Derive correct agent mode from selectedModeCategory (source of truth)
  const correctAgentMode = useMemo(() => {
    if (selectedModeCategory) {
      return getAgentModeFromCategory(selectedModeCategory) as AgentMode
    }
    return agentMode // Fallback to agentMode if selectedModeCategory is null
  }, [selectedModeCategory, agentMode, getAgentModeFromCategory])
  
  const { 
    primaryConfig: llmConfig,
    openrouterConfig,
    openaiConfig,
    anthropicConfig,
    vertexConfig,
    bedrockConfig
  } = useLLMStore()
  
  const { 
    toolList: allTools,
    selectedServers,
    getAvailableServers
  } = useMCPStore()
  
  // Get active tab (works for both chat and workflow modes)
  const { getActiveTab, getTab, chatTabs } = useChatStore()
  const activeTab = tabId ? getTab(tabId) : getActiveTab()
  
  // Determine which servers to use based on mode category
  const effectiveServers = useMemo(() => {
    // For workflow mode, use preset servers
    if (selectedModeCategory === 'workflow') {
      return currentPresetServers.length > 0 ? currentPresetServers : selectedServers
    }
    // For chat mode, use tab's selected servers from config (if available), otherwise fall back to global
    const tabSelectedServers = (selectedModeCategory === 'chat' && activeTab?.config) 
      ? activeTab.config.selectedServers 
      : selectedServers
    
    // If no servers are selected (empty array), default to all available servers (matches backend behavior)
    // This ensures tools are available when user hasn't explicitly selected servers
    if (tabSelectedServers.length === 0) {
      const availableServers = getAvailableServers()
      return availableServers.length > 0 ? availableServers : []
    }
    // Return selected servers (including "NO_SERVERS" if explicitly selected)
    return tabSelectedServers
  }, [selectedModeCategory, currentPresetServers, selectedServers, getAvailableServers, activeTab?.config])
  
  // Filter tools to only include those from effective servers
  // If "NO_SERVERS" is selected, return empty tools (pure LLM mode)
  const enabledTools = useMemo(() => {
    if (effectiveServers.includes("NO_SERVERS")) {
      return []
    }
    return allTools.filter(tool => 
      tool.server && effectiveServers.includes(tool.server)
    )
  }, [allTools, effectiveServers])
  
  // Get all tabs to track changes for polling
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
    getTabLastEventIndex,
    setTabLastEventIndex,
    setSessionId,
    setHasActiveChat,
    autoScroll,
    setAutoScroll,
    lastScrollTop,
    setLastScrollTop,
    finalResponse,
    setFinalResponse: _setFinalResponse,
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
    isAtBottom
  } = useChatStore()

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
          console.warn(`[ChatArea] WARNING: Multiple tabs sharing session IDs:`, duplicateSessionIds)
        }
        
        // Debug: Log event counts per session
        console.log(`[ChatArea] Event filtering - Looking for sessionId: ${currentSessionId}`)
        const sessionDetails = sessionIdToTabs.map(item => ({
          sessionId: item.sessionId,
          eventCount: item.eventCount,
          tabs: item.tabsUsingThisId
        }))
        console.log(`[ChatArea] Available sessionIds in store:`, JSON.stringify(sessionDetails, null, 2))
        console.log(`[ChatArea] Found ${eventsForTab.length} events for sessionId: ${currentSessionId}`)
        console.log(`[ChatArea] Events array:`, eventsForTab.map(e => ({ id: e.id, type: e.type, timestamp: e.timestamp })))
        
        // Check if the current sessionId matches any in the store
        const matchingSession = sessionDetails.find(item => item.sessionId === currentSessionId)
        if (!matchingSession) {
          console.warn(`[ChatArea] ⚠️ SessionId ${currentSessionId} NOT FOUND in store! Available sessionIds:`, sessionDetails.map(item => item.sessionId))
        } else if (matchingSession.eventCount !== eventsForTab.length) {
          console.warn(`[ChatArea] ⚠️ Event count mismatch! Store says ${matchingSession.eventCount} events, but found ${eventsForTab.length} events`)
        }
      }
      return eventsForTab
    }
    
    // No session ID - return empty array (allows header to render for mode/preset selection)
    console.log(`[ChatArea] No sessionId provided, returning empty events array`)
    return []
  }, [activeTab?.sessionId, tabEventsStore])
  
  // Always use tab events - never fall back to global events to prevent cross-tab mixing
  // If there are no tabs, return empty array (tabs should always exist in multi-tab mode)
  const displayEvents = tabEvents
  
  // Debug: Log when displayEvents changes
  useEffect(() => {
    console.log(`[ChatArea] displayEvents changed - sessionId: ${activeTab?.sessionId}, eventCount: ${displayEvents.length}`)
  }, [displayEvents.length, activeTab?.sessionId])

  // Computed values
  const isRequiredFolderSelected = useMemo(() => {
    if (selectedModeCategory !== 'workflow') return true; // No validation needed for other modes

    // Workflow mode requires Workflow/ folder
    if (selectedModeCategory === 'workflow') {
      const hasWorkflowFolder = chatFileContext.some((file: { type: string; path: string }) => 
        file.type === 'folder' && file.path.startsWith('Workflow/')
      );
      return hasWorkflowFolder;
    }
    
    return true;
  }, [selectedModeCategory, chatFileContext])

  // Use currentPresetServers from props (passed from App.tsx when preset is selected)

  // State for preset selection overlay
  const [showPresetSelection, setShowPresetSelection] = useState(false)
  const [pendingModeCategory, setPendingModeCategory] = useState<'workflow' | null>(null)
  
  // State for mode switch dialog
  const [showModeSwitchDialog, setShowModeSwitchDialog] = useState(false)
  const [pendingModeSwitch, setPendingModeSwitch] = useState<'chat' | 'workflow' | null>(null)
  

  // Handle mode selection from dropdown
  // Handle mode switching with preset selection for Workflow
  const handleModeSwitchWithPreset = (category: 'chat' | 'workflow') => {
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
  const switchMode = (category: 'chat' | 'workflow') => {
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
          console.error('[MODE_SWITCH] Failed to apply preset:', result.error)
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
  const filteredToasts = toasts.filter((toast: { type: string }) => toast.type === 'success' || toast.type === 'info') as Array<{id: string, message: string, type: 'success' | 'info'}>
  
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

  // Observer initialization removed - no longer needed

  // Immediate scroll handler for better responsiveness
  const handleScroll = useCallback(() => {
    if (!chatContentRef.current) return;
    
    const element = chatContentRef.current;
    const currentScrollTop = element.scrollTop;
    const scrollDistance = Math.abs(currentScrollTop - lastScrollTop);
    const isScrollingUp = currentScrollTop < lastScrollTop;
    const isScrollingDown = currentScrollTop > lastScrollTop;
    
    // Check if user is at bottom
    const wasAtBottom = isAtBottom(element);
    
    // Only disable auto-scroll if user actively scrolls up significantly
    // Don't show toast - user can see the toggle in header and floating button
    if (isScrollingUp && scrollDistance > 50 && autoScroll) {
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
    return () => element.removeEventListener('scroll', handleScroll);
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
    
    // Use requestAnimationFrame for smoother scrolling
    requestAnimationFrame(() => {
      element.scrollTo({
        top: targetScrollTop,
        behavior
      });
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


  // Update refs when values change (for global observer)
  useEffect(() => {
    if (!activeTab) {
      lastEventIndexRef.current = lastEventIndex
    }
  }, [lastEventIndex, activeTab])
  
  // Update displayEvents when active tab changes
  useEffect(() => {
    if (activeTab?.sessionId) {
      const tabEvents = getTabEvents(activeTab.sessionId)
      console.log(`[ChatArea] Switched to tab ${activeTab.tabId}, loading ${tabEvents.length} events`)
    }
  }, [activeTab?.tabId, activeTab?.sessionId, getTabEvents])
  
  // Deprecated: totalEventsRef useEffect removed

  // Workflow preset handlers
  const handleWorkflowPresetSelected = useCallback(async (presetId: string, presetContent: string) => {
    // Clear previous file context when switching workflow presets
    clearFileContext()
    // Apply the preset using the global preset store
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
      console.error('[WORKFLOW] Error checking workflow status:', error)
      // Fallback to default phase on error
      const defaultPhase = useWorkflowStore.getState().getDefaultPhase()
      setCurrentWorkflowPhase(defaultPhase)
      setCurrentQuery(presetContent)
    }
  }, [setCurrentQuery, applyPreset, setCurrentWorkflowPhase, setCurrentWorkflowQueryId, clearFileContext])

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
      console.error('[WORKFLOW] No preset query ID available for workflow approval')
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
      console.error('[WORKFLOW] Failed to approve workflow:', error)
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
    console.error(`[flushEventBatch] ERROR: This should not be called in tab mode. Discarding ${batch.length} events. Events should be stored via addTabEvents in polling.`)
  }, [])
  
  // Create debounced flush function (100ms delay)
  const debouncedFlush = useMemo(
    () => debounce(flushEventBatch, 100),
    [flushEventBatch]
  )

  // Removed extractUserMessageContent - no longer needed since we removed duplicate detection


  // Get polling management actions from store (before pollEvents callback)
  const { startPolling, stopPolling } = useChatStore()

  // Track active session IDs from backend (source of truth for what's actually active)
  // Declare before pollEvents so it can be used in the callback
  const [activeSessionIds, setActiveSessionIds] = useState<Set<string>>(new Set())

  // Polling function to get events for ALL active sessions
  const pollEvents = useCallback(async () => {
    console.log('[PollEvents] pollEvents called')
    const chatStore = useChatStore.getState()
    
    // Get all tabs that should be polled (all tabs in current mode)
    const allTabs = Object.values(chatStore.chatTabs).filter(tab => {
      return tab.metadata?.mode === selectedModeCategory
    })
    
    console.log('[PollEvents] All tabs:', allTabs.map(t => ({ tabId: t.tabId, sessionId: t.sessionId, mode: t.metadata?.mode })))
    
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
      if (activeSessionIds.size > 0 && !activeSessionIds.has(currentTab.sessionId)) {
        console.log(`[PollEvents] Skipping tab ${currentTab.tabId} - session ${currentTab.sessionId} not in backend active sessions`)
        return false
      }
      
      // Skip if completed (definitely done)
      if (currentTab.isCompleted) {
        console.log(`[PollEvents] Skipping tab ${currentTab.tabId} - session ${currentTab.sessionId} is completed`)
        return false
      }
      
      // If backend status is unknown (empty), allow polling (backend check might be in progress)
      // Backend will mark it inactive if no events for 10 minutes
      
      return true
    })
    
    console.log('[PollEvents] Tabs to poll:', tabsToPoll.map(t => ({ tabId: t.tabId, sessionId: t.sessionId })))
    
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
      } else if (!sessionId) {
        console.error(`[PollEvents] Tab ${tab.tabId} has no session ID - skipping`)
      }
    })
    
    if (sessionsToPoll.length === 0) {
      console.log('[PollEvents] No sessions to poll')
      return
    }
    
    // Polling multiple sessions (one per unique session)
    console.log(`[PollEvents] Polling ${sessionsToPoll.length} sessions:`, sessionsToPoll.map(s => ({ sessionId: s.sessionId, tabId: s.tab?.tabId })))
    
    // Poll each session
    for (const { sessionId, tab } of sessionsToPoll) {
      let currentTab = tab
      
      if (tab) {
        // Re-fetch the tab from store to ensure we have the latest session ID
        const fetchedTab = chatStore.getTab(tab.tabId)
        if (!fetchedTab) {
          console.warn(`[PollEvents] Tab ${tab.tabId} no longer exists, skipping`)
          continue
        }
        currentTab = fetchedTab
        
        // Verify session ID matches
        if (currentTab.sessionId !== sessionId) {
          console.warn(`[PollEvents] Tab ${currentTab.tabId} session ID changed from ${sessionId} to ${currentTab.sessionId}, using new session ID`)
          // Use the new session ID
          if (!currentTab.sessionId) {
            console.log(`[PollEvents] Skipping tab ${currentTab.tabId} - no session ID`)
            continue
          }
        }
        
        console.log(`[PollEvents] Polling tab ${currentTab.tabId} with sessionId: ${sessionId}`)
        
        // Double-check: verify this tab should still be polled
        // Only check isCompleted and sessionId - isStreaming is UI-only, not used for polling decisions
        if (currentTab.isCompleted && !currentTab.sessionId) {
          console.log(`[PollEvents] Skipping tab ${currentTab.tabId} - completed and no sessionId`)
          continue
        }
      }
      
      // Get fresh tab from store to ensure we have latest session ID
      const freshTab = currentTab ? chatStore.getTab(currentTab.tabId) : null
      const effectiveSessionId = freshTab?.sessionId || currentTab?.sessionId || sessionId
      
      // Log tab state for debugging
      if (currentTab) {
        console.log(`[PollEvents] Tab state check for ${currentTab.tabId}:`, {
          isStreaming: freshTab?.isStreaming ?? currentTab.isStreaming,
          sessionId: freshTab?.sessionId ?? currentTab.sessionId,
          isCompleted: freshTab?.isCompleted ?? currentTab.isCompleted,
          effectiveSessionId
        })
      }
      
      const rawLastEventIndex = currentTab 
        ? getTabLastEventIndex(effectiveSessionId)
        : lastEventIndexRef.current
      
      // Ensure lastEventIndex is >= 0 (API requirement)
      // -1 means "no events yet", which should be treated as 0
      const currentLastEventIndex = Math.max(0, rawLastEventIndex === -1 ? 0 : rawLastEventIndex)
      
      console.log(`[PollEvents] About to call getSessionEvents for sessionId: ${effectiveSessionId}, rawLastEventIndex: ${rawLastEventIndex}, currentLastEventIndex: ${currentLastEventIndex}`)
      
      // Track which session is currently being polled (for derived isStreaming)

      try {
        // Get event mode from current tab (defaults to 'basic')
        const eventMode: 'basic' | 'advanced' = (currentTab?.eventMode || 'basic') as 'basic' | 'advanced'
        console.log(`[PollEvents] Calling agentApi.getSessionEvents(${effectiveSessionId}, ${currentLastEventIndex}, eventMode: ${eventMode})`)
        const response = await agentApi.getSessionEvents(effectiveSessionId, currentLastEventIndex, { eventMode })
        console.log(`[PollEvents] Received response from getSessionEvents:`, { eventCount: response.events.length, hasMore: response.has_more, sessionId: response.session_id, requestedSessionId: effectiveSessionId })
        
        // Use session ID from response if available (it's the source of truth)
        const actualSessionId = response.session_id || effectiveSessionId
        
        // If response has a different session ID, update the tab
        if (currentTab && response.session_id && response.session_id !== effectiveSessionId) {
          console.log(`[PollEvents] Session ID changed from ${effectiveSessionId} to ${response.session_id}, updating tab`)
          chatStore.updateTabSessionId(currentTab.tabId, response.session_id)
        }

        // Check session status from response (source of truth - replaces event parsing)
        // session_status is always present in the response (required field)
        // Update isStreaming for UI feedback only (not used for polling decisions)
        const sessionStatus = response.session_status
        console.log(`[PollEvents] Session ${actualSessionId} status: ${sessionStatus}`)
        
        if (currentTab) {
          const chatStore = useChatStore.getState()
          
          // Handle different session statuses - update UI state (isStreaming) for user feedback
          if (sessionStatus === 'completed' || sessionStatus === 'error') {
            // Session is done - update UI state
            chatStore.setTabCompleted(currentTab.tabId, true)
            chatStore.setTabStreaming(currentTab.tabId, false) // UI: Hide stop button, show send button
            console.log(`[PollEvents] Tab ${currentTab.tabId} marked as completed (status: ${sessionStatus})`)
          } else if (sessionStatus === 'running') {
            // Session is active - update UI state
            chatStore.setTabCompleted(currentTab.tabId, false)
            chatStore.setTabStreaming(currentTab.tabId, true) // UI: Show stop button, disable send button
            console.log(`[PollEvents] Tab ${currentTab.tabId} marked as running`)
          } else if (sessionStatus === 'stopped' || sessionStatus === 'inactive') {
            // Session stopped or inactive - update UI state
            chatStore.setTabCompleted(currentTab.tabId, false)
            chatStore.setTabStreaming(currentTab.tabId, false) // UI: Show send button, hide stop button
            console.log(`[PollEvents] Tab ${currentTab.tabId} marked as stopped/inactive (status: ${sessionStatus})`)
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
          console.log(`[PollEvents] Received ${response.events.length} events for sessionId: ${actualSessionId}, tab: ${currentTab?.tabId || 'none'}`)
          // Update last event index for this session
          // CRITICAL: Use last_processed_index from backend (tracks unfiltered array position)
          // This ensures correct tracking even when filtering reduces the number of events returned
          // Require last_processed_index from backend (new system - no fallback)
          if (response.last_processed_index === undefined) {
            console.error('[PollEvents] Backend did not provide last_processed_index - required field missing')
            return // Skip processing if backend doesn't provide required field
          }
          const newLastEventIndex = response.last_processed_index
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
          const newEvents = (response.events as PollingEvent[]).filter(event => {
            // Detect request human feedback event and stop streaming for this tab
            if (event.type === 'request_human_feedback' && currentTab) {
              const chatStore = useChatStore.getState()
              chatStore.setTabStreaming(currentTab.tabId, false)
              chatStore.setTabCompleted(currentTab.tabId, false) // Not completed, just paused
            }
            
            // Process workspace events using the centralized store
            if (event.type === 'workspace_file_operation') {
              console.log('[ChatArea] Received workspace_file_operation event:', {
                type: event.type,
                data: event.data,
                timestamp: event.timestamp
              })
            }
            const { processWorkspaceEvent } = useWorkspaceStore.getState()
            const processed = processWorkspaceEvent(event)
            if (event.type === 'workspace_file_operation') {
              console.log('[ChatArea] Event processed:', processed)
            }
            
            // Backend doesn't send user_message events - we add them manually on the frontend
            // So we don't need to filter user_message events from polling
            // If backend ever sends user_message events, we'll accept them (shouldn't happen)
            return true
          })
          
          // Process workflow-specific events (after filtering)
          if (selectedModeCategory === 'workflow' && currentTab?.metadata?.phaseId) {
            const phases = useWorkflowStore.getState().phases
            for (const event of response.events as PollingEvent[]) {
              // Handle todo list generation from workflow agent
              if (event.type === 'agent_end') {
                const agentEventData = event.data as { data?: { agent_type?: string; result?: string } }
                const agentEvent = agentEventData?.data
                if (agentEvent && agentEvent.agent_type === 'todo_planner') {
                  const result = agentEvent.result || ''
                  if (result) {
                    const planningPhase = phases.length > 1 ? phases[1].id : (phases.length > 0 ? phases[0].id : 'execution')
                    if (currentWorkflowPhase !== planningPhase) {
                      setCurrentWorkflowPhase(planningPhase)
                    }
                  }
                }
              }

              // Handle workflow completion events (for phase transitions, not completion detection)
              if (event.type === 'workflow_end') {
                const completionPhase = phases.length > 1 ? phases[1].id : (phases.length > 0 ? phases[0].id : 'execution')
                setCurrentWorkflowPhase(completionPhase)
              }
            }
          }
          
          // Add events to the correct tab's event list
          if (currentTab) {
            // CRITICAL: Re-verify the tab's session ID right before storing events
            // This prevents storing events under the wrong session ID if the tab's session ID changed
            const finalTab = chatStore.getTab(currentTab.tabId)
            if (!finalTab) {
              console.warn(`[PollEvents] Tab ${currentTab.tabId} no longer exists, skipping event storage`)
              continue
            }
            
            // Use the actual session ID from the response (source of truth)
            // If response has a different session ID, use that one
            const sessionIdForStorage = actualSessionId
            
            // Verify session ID matches
            if (finalTab.sessionId !== sessionIdForStorage) {
              console.warn(
                `[PollEvents] Session ID mismatch for tab ${currentTab.tabId}! ` +
                `Tab has: ${finalTab.sessionId}, but response has: ${sessionIdForStorage}. ` +
                `Using response session ID ${sessionIdForStorage} for event storage.`
              )
            }
            
            // Store events per session (by sessionId)
            // CRITICAL: Use the sessionId from the response (actualSessionId) as it's the source of truth
            // Multiple observers can view the same session, but events are stored per session
            console.log(`[PollEvents] Storing ${newEvents.length} events for sessionId: ${sessionIdForStorage}, tab: ${currentTab.tabId}`)
            addTabEvents(sessionIdForStorage, newEvents)
            
            // NOTE: Completion detection is now handled by session_status above
            // No need to parse events for completion anymore
          } else {
            // No tab - this should not happen in tab mode
            // Log error and discard events to prevent mixing
            console.error(`[PollEvents] ERROR: No currentTab but polling returned events. Discarding ${newEvents.length} events.`)
            
            // NOTE: Completion detection is now handled by session_status above
            // No need to parse events for completion anymore
          }
        }
      } catch (error) {
        console.error(`[PollEvents] Error polling session ${effectiveSessionId} (tab: ${currentTab?.tabId || 'global'}):`, error)
        // Continue polling other observers even if one fails
      }
    }
  }, [selectedModeCategory, getTabLastEventIndex, setTabLastEventIndex, setLastEventIndex, addTabEvents, pollingInterval, setIsStreaming, setIsCompleted, setHasActiveChat, currentWorkflowPhase, setCurrentWorkflowPhase, processedCompletionEventsRef, stopPolling, activeSessionIds])


  // Track if we're already processing to prevent infinite loops
  const processingRef = useRef<string | null>(null)
  
  // Periodically check active sessions from backend (every 30 seconds)
  useEffect(() => {
    const checkActiveSessions = async () => {
      try {
        const response = await agentApi.getActiveSessions()
        const activeIds = new Set(response.active_sessions.map(s => s.session_id))
        setActiveSessionIds(activeIds)
        console.log(`[ActiveSessions] Found ${activeIds.size} active session(s) from backend:`, Array.from(activeIds))
      } catch (error) {
        console.error('[ActiveSessions] Failed to fetch active sessions:', error)
      }
    }
    
    // Check immediately
    checkActiveSessions()
    
    // Then check every 30 seconds
    const interval = setInterval(checkActiveSessions, 30000)
    return () => clearInterval(interval)
  }, [])
  
  // Only poll tabs that have their session ID in the backend's active sessions list
  // Backend determines activity based on event activity (10 min timeout)
  // Don't filter by local isStreaming - backend is source of truth
  const tabsWithActiveSessions = useMemo(() => {
    const activeIds = activeSessionIds // Capture in closure
    return tabsWithSessions.filter(tab => {
      // Must have session ID
      if (!tab.sessionId) return false
      
      // Skip completed sessions (definitely done)
      if (tab.isCompleted) return false
      
      // Must be in backend's active sessions list (or we haven't checked yet - allow polling)
      // If backend says it's active, poll it even if local isStreaming is false
      // This ensures we catch events that come after stop is pressed
      if (activeIds.size > 0 && !activeIds.has(tab.sessionId)) {
        console.log(`[ActiveSessions] Tab ${tab.tabId} session ${tab.sessionId} not in backend active sessions, skipping`)
        return false
      }
      
      // If backend status is unknown (empty), allow polling (backend check might be in progress)
      // Backend will mark it inactive if no events for 10 minutes
      
      return true
    })
  }, [tabsWithSessions, activeSessionIds])
  
  // Start/stop polling based on active sessions using store actions
  // Use tabsWithActiveSessions (backend-driven) instead of updatePollingState (isStreaming-based)
  useEffect(() => {
    // If there are active sessions and no polling interval, start polling
    if (tabsWithActiveSessions.length > 0 && !pollingInterval) {
      console.log(`[ChatArea] Starting polling via store - found ${tabsWithActiveSessions.length} active tab(s):`, tabsWithActiveSessions.map(t => ({ tabId: t.tabId, sessionId: t.sessionId })))
      startPolling(pollEvents)
    }
    // If there are no active sessions but polling is running, stop it
    else if (tabsWithActiveSessions.length === 0 && pollingInterval) {
      console.log(`[ChatArea] Stopping polling via store - no active sessions`)
      stopPolling()
    }
  }, [pollingInterval, startPolling, stopPolling, pollEvents, tabsWithActiveSessions])

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
        // Check if session is currently active
        const activeSessions = await agentApi.getActiveSessions()
        const activeSession = activeSessions.active_sessions.find(
          (session: ActiveSessionInfo) => session.session_id === originalSessionId
        )
        
        if (activeSession) {
          setSessionState('active')
          
          // First, load historical events for the active session
          setIsLoadingHistory(true)
          
          try {
            // Get event mode from active tab (defaults to 'basic')
            const activeTab = getActiveTab()
            const eventMode: 'basic' | 'advanced' = (activeTab?.eventMode || 'basic') as 'basic' | 'advanced'
            const response = await agentApi.getSessionEvents(originalSessionId, 0, { eventMode })
            
            // Convert and set historical events
            // response.events is already PollingEvent[] from GetEventsResponse
            const pollingEvents: PollingEvent[] = response.events
            
            // CRITICAL: Always use tabEvents - store by sessionId
            const sessionIdForHistory = activeSession.session_id
            
            if (sessionIdForHistory) {
              // Store in tabEvents using the session ID
              addTabEvents(sessionIdForHistory, pollingEvents)
              setTabLastEventIndex(sessionIdForHistory, pollingEvents.length > 0 ? pollingEvents.length - 1 : -1)
            } else {
              console.error(`[History] No session ID in active session - cannot store events`)
            }
            // Deprecated: setTotalEvents removed
            setIsLoadingHistory(false)
            
            // Now reconnect to active session for live updates
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
            console.error('[SESSION_STATE] Failed to load historical events:', error)
            setIsLoadingHistory(false)
            addToast('Failed to load historical events', 'info')
          }
          
          processingRef.current = null
          return
        }
        
        // Check if session exists in database (completed)
        try {
          const sessionStatus = await agentApi.getSessionStatus(originalSessionId)
          if (sessionStatus.status === 'completed') {
            setSessionState('completed')
            
            // Load historical events
            setIsLoadingHistory(true)
            // Get event mode from active tab (defaults to 'basic')
            const activeTab = getActiveTab()
            const eventMode: 'basic' | 'advanced' = (activeTab?.eventMode || 'basic') as 'basic' | 'advanced'
            const response = await agentApi.getSessionEvents(originalSessionId, 0, { eventMode })
            
            // Convert and set events
            // response.events is already PollingEvent[] from GetEventsResponse
            const pollingEvents: PollingEvent[] = response.events
            
            // CRITICAL: Always use tabEvents - no backward compatibility fallback
            // For completed sessions, we need to find the observer ID from the session
            // Try to get it from the first event's session_id or look up the tab that has this session
            const chatStore = useChatStore.getState()
            const tabWithSession = Object.values(chatStore.chatTabs).find(tab => tab.sessionId === originalSessionId)
            const sessionIdForHistory = tabWithSession?.sessionId
            
            if (sessionIdForHistory) {
              // Store in tabEvents using the session ID
              addTabEvents(sessionIdForHistory, pollingEvents)
              setTabLastEventIndex(sessionIdForHistory, pollingEvents.length > 0 ? pollingEvents.length - 1 : -1)
            } else {
              console.error(`[History] No session ID found for completed session ${originalSessionId} - cannot store events`)
            }
            // Deprecated: setTotalEvents removed
            setIsCompleted(true)
            setIsStreaming(false)
            setHasActiveChat(false)
            setIsLoadingHistory(false)
            processingRef.current = null
            return
          }
        } catch {
          // Session not found in database
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
      console.log(`[STOP] Updated tab ${activeTab.tabId} streaming status to false (UI feedback only)`)
      
      // DO NOT reset event index when stopping - preserve it for multi-turn conversations
      // The event index should only be reset when starting a completely new chat
      // This ensures that when user sends a new message after stopping, we only get NEW events
      console.log(`[STOP] Preserving event index for tab ${activeTab.tabId} to continue multi-turn conversation`)
    }

    // Call backend to stop the agent execution (preserves conversation history)
    // Use tab's session ID
    const currentSessionId = getSessionId()
    const sessionIdToStop = activeTab?.sessionId || currentSessionId
    if (sessionIdToStop) {
      try {
        await agentApi.stopSession(sessionIdToStop)
      } catch (error) {
        console.error('[STOP] Failed to stop session:', error)
      }
    }

    // DO NOT reset global event polling index - preserve it for multi-turn conversations
    // Only reset when starting a completely new chat (via handleNewChat)
    console.log('[STOP] Preserving event polling index for multi-turn conversation')
    // Deprecated: setLastEventCount removed
  }, [setIsStreaming, activeTab])

  // Store execution options for use in the request
  const executionOptionsRef = useRef<ExecutionOptions | undefined>(undefined)

  // Wrapper function to submit query with the current local query
  const submitQueryWithQuery = useCallback(async (query: string, executionOptions?: ExecutionOptions) => {
    console.log('[ChatArea] submitQueryWithQuery called:', {
      query: query?.substring(0, 50),
      hasQuery: Boolean(query?.trim()),
      activeTab: activeTab?.tabId,
      tabSessionId: activeTab?.sessionId,
    })
    
    // Store execution options for inclusion in the request
    executionOptionsRef.current = executionOptions
    
    // Early validation
    if (!query?.trim()) {
      console.warn('[ChatArea] submitQueryWithQuery: Empty query, returning early')
      return
    }

    if (selectedModeCategory === 'workflow' && !isRequiredFolderSelected) {
      console.error('[SUBMIT] Validation failed - Workflow folder required for workflow mode')
      return
    }

    // Stop any ongoing streaming
    if (isStreaming) {
      await stopStreaming()
    }
    
    // Get or create current tab
    let currentTab = activeTab
    if (!currentTab && selectedModeCategory === 'chat') {
      const chatStore = useChatStore.getState()
      const chatTabs = Object.values(chatStore.chatTabs).filter(tab => 
        tab.metadata?.mode === 'chat'
      )
      
      if (chatTabs.length === 0) {
        console.log('[SUBMIT] No chat tab exists, creating one automatically...')
        try {
          const newTabId = await chatStore.createChatTab('Chat 1', { mode: 'chat' })
          currentTab = chatStore.getTab(newTabId)
          console.log(`[SUBMIT] Created new chat tab: ${newTabId}`)
        } catch (error) {
          console.error('[SUBMIT] Failed to create chat tab:', error)
          return
        }
      } else {
        currentTab = chatStore.getActiveTab() || chatTabs[0]
      }
    }
    
    // Validate tab exists
    if (!currentTab) {
      const chatStore = useChatStore.getState()
      const hasTabs = Object.keys(chatStore.chatTabs).length > 0
      console.error(`[SUBMIT] No currentTab - cannot submit query. Has tabs: ${hasTabs}`)
      return
    }
    
    // Ensure tab has session ID (generate if missing)
    let sessionId = currentTab.sessionId
    if (!sessionId) {
      sessionId = globalThis.crypto.randomUUID()
      const chatStore = useChatStore.getState()
      chatStore.updateTabSessionId(currentTab.tabId, sessionId)
      currentTab = { ...currentTab, sessionId }
      console.log(`[SUBMIT] Generated new session ID for tab ${currentTab.tabId}: ${sessionId}`)
    }
    
    // At this point, sessionId is guaranteed to be a string
    const tabSessionId: string = sessionId
    
    // Use tab's file context in chat mode, global file context otherwise
    const effectiveFileContext = (selectedModeCategory === 'chat' && currentTab.config) 
      ? currentTab.config.fileContext 
      : chatFileContext
    
    // Build query with file context
    const queryWithContext = effectiveFileContext.length > 0 
      ? `${query.trim()}\n\n📁 Files in context: ${effectiveFileContext.map((file: { path: string }) => file.path).join(', ')}`
      : query.trim()
    
    // Set current query for workflow mode
    if (selectedModeCategory === 'workflow') {
      setCurrentQuery(queryWithContext)
    }
    
    // Add user message event to the tab's events
    const userMessageEvent: PollingEvent = {
      id: `user-message-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
      type: 'user_message',
      timestamp: new Date().toISOString(),
      data: {
        type: 'user_message',
        timestamp: new Date().toISOString(),
        data: {
          content: query.trim(),
          timestamp: new Date().toISOString()
        }
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      } as any
    }
    
    console.log(`[SUBMIT] Adding user message event:`, {
      id: userMessageEvent.id,
      content: query.trim().substring(0, 50),
      sessionId: tabSessionId,
      tabId: currentTab.tabId
    })
    addTabEvents(tabSessionId, [userMessageEvent])
    
    // Enable auto-scroll and scroll to bottom when user submits a new message
    setAutoScroll(true)
    setTimeout(() => {
      scrollToBottom('smooth')
    }, 50)
    
    // Clear the query text after submission
    setCurrentQuery('')

    // Preserve final response by adding it as completion event if needed
    const eventsToCheck = getTabEvents(tabSessionId)
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
      addTabEvents(tabSessionId, [completionEvent])
    }

    // Clear the final response and completion state for the new query
    _setFinalResponse('')
    setIsCompleted(false)
    setIsStreaming(true)
    setHasActiveChat(true)
    
    // Reset tab's completion status and set streaming to true immediately (UI feedback only)
    // This gives instant UI feedback - buttons change immediately, not waiting for API response
    // Note: Polling decisions are based on backend activeSessionIds, not isStreaming
    if (currentTab) {
      const chatStore = useChatStore.getState()
      chatStore.setTabCompleted(currentTab.tabId, false)
      chatStore.setTabStreaming(currentTab.tabId, true) // UI: Show stop button immediately
      console.log(`[SUBMIT] Set tab ${currentTab.tabId} streaming to true immediately (UI feedback only)`)
    }

    // Reset event tracking for new query (preserve lastEventIndex for multi-turn chat)
    // Deprecated: setLastEventCount removed
    processedCompletionEventsRef.current.clear()

    try {
      // Filter out "*" markers from currentPresetTools before sending to backend
      // "*" markers indicate "all tools" mode, which is represented as an empty array
      const filteredPresetTools = currentPresetTools?.filter(t => !t.endsWith(':*')) || []
      
      // Use the correctAgentMode calculated at component level (derived from selectedModeCategory)
      console.log('[CHATAREA] Starting query with:', {
        selectedModeCategory,
        agentModeFromStore: agentMode,
        correctAgentMode,
        selectedWorkflowPreset,
        currentPresetServers,
        currentPresetTools,
        filteredPresetTools,
        effectiveServers,
        enabledToolsCount: enabledTools.length,
        'hasAllToolsMarkers': currentPresetTools?.some(t => t.endsWith(':*')) ? 'YES' : 'NO'
      });
      
      // Get code execution mode: 
      // - For chat mode: Use ChatInput selection if no preset, or preset value if preset exists
      // - For workflow mode: Use preset value if available
      // IMPORTANT: Only check for chat preset when in chat mode, workflow preset when in workflow mode
      const chatPreset = correctAgentMode === 'simple' ? getActivePreset('chat') : null
      const workflowPreset = correctAgentMode === 'workflow' ? (selectedWorkflowPreset ? getActivePreset('workflow') : null) : null
      const activePreset = workflowPreset || chatPreset
      const presetUseCodeExecutionMode = activePreset?.useCodeExecutionMode
      
      // Get code execution mode from store (only relevant for chat mode when no preset)
      const { useCodeExecutionMode: storeCodeExecutionMode } = useAppStore.getState()
      
      // Determine final code execution mode value
      // For chat mode: Use preset value if preset exists, otherwise use tab's config value
      // For workflow mode: Use preset value if available, otherwise undefined
      let useCodeExecutionMode: boolean | undefined
      if (correctAgentMode === 'simple') {
        // In chat mode: If preset exists, use preset value; otherwise use tab's config
        if (presetUseCodeExecutionMode !== undefined) {
          useCodeExecutionMode = presetUseCodeExecutionMode
        } else {
          // No preset, use tab's config value (user's manual control via ChatInput toggle)
          useCodeExecutionMode = (selectedModeCategory === 'chat' && currentTab?.config) 
            ? currentTab.config.useCodeExecutionMode 
            : storeCodeExecutionMode
        }
      } else if (correctAgentMode === 'workflow') {
        // For workflow mode, use preset value if available
        useCodeExecutionMode = presetUseCodeExecutionMode
      } else {
        useCodeExecutionMode = undefined
      }
      
      console.log('[code_execution] [ChatArea] Mode determination:', {
        activePreset: activePreset?.label,
        presetUseCodeExecutionMode,
        storeCodeExecutionMode,
        selectedModeCategory,
        correctAgentMode,
        finalUseCodeExecutionMode: useCodeExecutionMode,
        finalType: typeof useCodeExecutionMode
      })
      
      // Use tab's LLM config in chat mode, global config otherwise
      const effectiveLLMConfig = (selectedModeCategory === 'chat' && currentTab?.config) 
        ? currentTab.config.llmConfig 
        : llmConfig
      
      // Build llm_config with API keys from provider configs
      const llmConfigWithApiKeys = {
        ...effectiveLLMConfig,
        api_keys: {
          ...(openrouterConfig.api_key ? { openrouter: openrouterConfig.api_key } : {}),
          ...(openaiConfig.api_key ? { openai: openaiConfig.api_key } : {}),
          ...(anthropicConfig.api_key ? { anthropic: anthropicConfig.api_key } : {}),
          ...(vertexConfig.api_key ? { vertex: vertexConfig.api_key } : {}),
          ...(bedrockConfig.region ? { bedrock: { region: bedrockConfig.region } } : {}),
        }
      }
      
      // Prepare API request payload
      // CRITICAL: Log the actual query being sent to debug message confusion issues
      console.log('[SUBMIT] Preparing API request with query:', {
        query: query.trim(),
        queryLength: query.trim().length,
        queryWithContext: queryWithContext.substring(0, 100),
        queryWithContextLength: queryWithContext.length,
        sessionId: tabSessionId,
        tabId: currentTab.tabId
      })
      
      const requestPayload = {
        query: queryWithContext,
        agent_mode: correctAgentMode,
        enabled_tools: enabledTools.map((tool: { name: string }) => tool.name),
        enabled_servers: effectiveServers,
        selected_tools: (selectedWorkflowPreset || getActivePreset('chat')) ? filteredPresetTools : undefined, // Only send when preset is active
        provider: effectiveLLMConfig.provider,
        model_id: effectiveLLMConfig.model_id,
        llm_config: llmConfigWithApiKeys,
        preset_query_id: selectedWorkflowPreset || undefined,
        // Send boolean value directly - don't use || undefined as it converts false to undefined
        use_code_execution_mode: useCodeExecutionMode !== undefined ? useCodeExecutionMode : undefined,
        // Execution options from frontend (for workflow execution phase)
        execution_options: executionOptionsRef.current,
        // Context summarization: Enable by default for chat mode
        enable_context_summarization: selectedModeCategory === 'chat' ? true : undefined,
        summarize_on_max_turns: selectedModeCategory === 'chat' ? true : undefined,
        summary_keep_last_messages: selectedModeCategory === 'chat' ? 8 : undefined,
      }
      
      console.log('[code_execution] [ChatArea] API Request payload:', {
        use_code_execution_mode: requestPayload.use_code_execution_mode,
        type: typeof requestPayload.use_code_execution_mode,
        preset_query_id: requestPayload.preset_query_id,
        enabled_servers: requestPayload.enabled_servers,
        enabled_servers_count: requestPayload.enabled_servers?.length || 0
      })
      
      // Ensure API module uses the tab's session ID
      setSessionId(tabSessionId)
      
      // Submit query to backend
      const response = await agentApi.startQuery(requestPayload, tabSessionId)

      if (response.status === 'started' || response.status === 'workflow_started') {
        // Update session ID from response (matches backend storage)
        const sessionId = response.session_id || response.query_id
        if (!sessionId) {
          console.error('[SUBMIT] No sessionId in response, cannot start polling')
          setIsStreaming(false)
          setHasActiveChat(false)
          // Reset tab's streaming status on error
          if (currentTab) {
            const chatStore = useChatStore.getState()
            chatStore.setTabStreaming(currentTab.tabId, false)
          }
          return
        }
        
        setSessionId(sessionId)
        
        // Update tab's session ID and streaming status
        const chatStore = useChatStore.getState()
        chatStore.updateTabSessionId(currentTab.tabId, sessionId)
        chatStore.setTabStreaming(currentTab.tabId, true)
        chatStore.setTabCompleted(currentTab.tabId, false) // Ensure not marked as completed
        console.log(`[SUBMIT] Updated tab ${currentTab.tabId} with sessionId: ${sessionId}, streaming: true`)
        
        // Verify the update was applied by reading fresh from store
        const updatedTab = chatStore.getTab(currentTab.tabId)
        if (updatedTab) {
          console.log(`[SUBMIT] Verified tab state - streaming: ${updatedTab.isStreaming}, sessionId: ${updatedTab.sessionId}, completed: ${updatedTab.isCompleted}`)
        }
        
        // Start polling if not already running (read fresh from store to avoid stale closure)
        const currentPollingInterval = chatStore.pollingInterval
        if (!currentPollingInterval) {
          console.log('[SUBMIT] Starting polling interval for events')
          startPolling(pollEvents)
        } else {
          console.log('[SUBMIT] Polling already active, reusing existing interval')
        }
        
        // Call pollEvents immediately after a short delay to ensure store updates are applied
        // Use a slightly longer delay to ensure Zustand store updates have propagated
        setTimeout(() => {
          console.log('[SUBMIT] Calling pollEvents after store update delay')
          pollEvents()
        }, 150)
      } else {
        console.error('[SUBMIT] Backend error:', response)
        setIsStreaming(false)
        setHasActiveChat(false)
        // Reset tab's streaming status on error
        if (currentTab) {
          const chatStore = useChatStore.getState()
          chatStore.setTabStreaming(currentTab.tabId, false)
        }
      }
    } catch (error) {
      console.error('[SUBMIT] Failed to submit query:', error)
      setIsStreaming(false)
      setHasActiveChat(false)
      // Reset tab's streaming status on error
      if (currentTab) {
        const chatStore = useChatStore.getState()
        chatStore.setTabStreaming(currentTab.tabId, false)
      }
    }

  }, [correctAgentMode, selectedModeCategory, isRequiredFolderSelected, chatFileContext, isStreaming, stopStreaming, finalResponse, startPolling, setCurrentQuery, _setFinalResponse, setIsCompleted, setIsStreaming, setHasActiveChat, setSessionId, llmConfig, openrouterConfig, openaiConfig, anthropicConfig, vertexConfig, bedrockConfig, effectiveServers, enabledTools, currentPresetTools, getActivePreset, selectedWorkflowPreset, pollEvents, processedCompletionEventsRef, activeTab, agentMode, currentPresetServers, addTabEvents, getTabEvents, setAutoScroll, scrollToBottom])

  // Handle new chat - clear backend session and reset all chat state
  const handleNewChat = useCallback(async () => {
    // Clear conversation history from backend first (if sessionId is available)
    const currentSessionId = getSessionId()
    const sessionIdToClear = activeTab?.sessionId || currentSessionId
    if (sessionIdToClear) {
      try {
        await agentApi.clearSession(sessionIdToClear)
        console.log('[NEW_CHAT] Successfully cleared session:', sessionIdToClear)
      } catch (error) {
        console.error('[NEW_CHAT] Failed to clear session:', error)
        // Continue with frontend reset even if backend clear fails
      }
    } else {
      console.log('[NEW_CHAT] No sessionId available, skipping backend session clear')
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
  }, [clearWorkflowState, resetChatState, onNewChat, activeTab?.sessionId, selectedModeCategory, selectedWorkflowPreset, setCurrentWorkflowPhase, setLastEventIndex])

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
        
        {/* Floating "Scroll to latest" button - shown when auto-scroll is disabled (not in workflow mode) */}
        {!autoScroll && displayEvents.length > 0 && selectedModeCategory !== 'workflow' && (
          <button
            onClick={() => {
              setAutoScroll(true)
              scrollToBottom('smooth')
            }}
            className="sticky bottom-4 left-1/2 -translate-x-1/2 z-10 flex items-center gap-1.5 px-3 py-1.5 bg-gray-100 dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-xs font-medium rounded-full shadow-lg hover:bg-gray-200 dark:hover:bg-gray-600 transition-colors"
            title="Scroll to latest messages"
          >
            <svg className="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 14l-7 7m0 0l-7-7m7 7V3" />
            </svg>
            Scroll to latest
          </button>
        )}
        
        <div className={`min-w-0 ${compact ? 'p-2' : 'p-4'}`}>
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
              <EventDisplay events={displayEvents} onFeedbackSubmitted={handleFeedbackSubmitted} compact={compact} flatHierarchy={true} />
            )}
          </WorkflowModeHandler>
        ) : (
          <>
            {/* Empty State - Show when no events and not in historical session */}
            {!chatSessionId && displayEvents.length === 0 && !isStreaming && (
              <ModeEmptyState modeCategory={selectedModeCategory} />
            )}
            
            {activeTab?.sessionId && (
              <EventDisplay events={displayEvents} onFeedbackSubmitted={handleFeedbackSubmitted} compact={compact} />
            )}
          </>
        )}
        </div>
      </div>

      {/* Input Area - Completely isolated from event updates, hidden in workflow mode */}
      {!chatSessionId && !hideInput && (
        <ChatInput
          onSubmit={submitQueryWithQuery}
          onStopStreaming={stopStreaming}
        />
      )}
      
      {/* Historical Session Notice */}
      {chatSessionId && (
        <div className={`${compact ? 'px-2 py-2' : 'px-4 py-3'} border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800`}>
          <div className={`text-center ${compact ? 'text-xs' : 'text-sm'} text-gray-600 dark:text-gray-400`}>
            <p>Viewing historical chat session</p>
            <p className={`${compact ? 'text-[10px]' : 'text-xs'} mt-1`}>
              <button
                onClick={onNewChat}
                className="text-blue-600 dark:text-blue-400 hover:text-blue-800 dark:hover:text-blue-300 underline"
              >
                Start new chat
              </button>
              {' '}to continue the conversation
            </p>
          </div>
        </div>
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

export default ChatArea

