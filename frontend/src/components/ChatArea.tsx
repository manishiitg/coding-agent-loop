import { useEffect, useRef, useCallback, forwardRef, useImperativeHandle, useMemo, useState } from 'react'
import debounce from 'lodash.debounce'
import { agentApi } from '../services/api'
import type { PollingEvent, ActiveSessionInfo, OrchestratorExecutionMode } from '../services/api-types'
import { EXECUTION_MODES } from '../services/api-types'
import { EventModeProvider } from './events'
import { ChatInput } from './ChatInput'
import { EventDisplay } from './EventDisplay'
import { WorkflowModeHandler, type WorkflowModeHandlerRef } from './workflow'
import { OrchestratorModeHandler, type OrchestratorModeHandlerRef } from './orchestrator/OrchestratorModeHandler'
import { ToastContainer } from './ui/Toast'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { WORKFLOW_PHASES } from '../constants/workflow'
import { OrchestratorExplanation } from './OrchestratorExplanation'
import { WorkflowExplanation } from './WorkflowExplanation'
import { ReActExplanation } from './ReActExplanation'
import GuidanceFloatingIcon from './GuidanceFloatingIcon'
import { useAppStore, useLLMStore, useMCPStore, useChatStore } from '../stores'
import { useModeStore } from '../stores/useModeStore'
import { ModeEmptyState } from './ModeEmptyState'
import { PresetSelectionOverlay } from './PresetSelectionOverlay'
import { usePresetApplication } from '../stores/useGlobalPresetStore'
import { ModeSwitchDialog } from './ui/ModeSwitchDialog'
import { ChatHeader } from './ChatHeader'

interface ChatAreaProps {
  // New chat handler
  onNewChat: () => void
}

// Ref interface for ChatArea component
export interface ChatAreaRef {
  handleNewChat: () => void
  resetChatState: () => void
  refreshWorkflowPresets: () => Promise<void>
}


// Inner component that can use the EventMode context
const ChatAreaInner = forwardRef<ChatAreaRef, ChatAreaProps>(({
  onNewChat
}, ref) => {
  // Store subscriptions
  const { 
    agentMode, 
    setCurrentQuery,
    chatFileContext,
    clearFileContext,
    chatSessionId,
    chatSessionTitle,
    requiresNewChat
  } = useAppStore()
  
  const { selectedModeCategory } = useModeStore()
  const { getActivePreset, applyPreset, clearActivePreset, currentPresetServers } = usePresetApplication()
  
  const { 
    primaryConfig: llmConfig
  } = useLLMStore()
  
  const { 
    toolList: allTools,
    selectedServers
  } = useMCPStore()
  
  // Determine which servers to use based on agent mode
  const effectiveServers = useMemo(() => {
    // For workflow/deep-research modes, use preset servers
    if (agentMode === 'workflow' || agentMode === 'orchestrator') {
      return currentPresetServers.length > 0 ? currentPresetServers : selectedServers
    }
    // For simple/ReAct modes, use manually selected servers
    return selectedServers
  }, [agentMode, currentPresetServers, selectedServers])
  
  // Filter tools to only include those from effective servers
  const enabledTools = allTools.filter(tool => 
    tool.server && effectiveServers.includes(tool.server)
  )
  
  const {
    // Chat state
    isStreaming,
    setIsStreaming,
    observerId,
    setObserverId,
    lastEventIndex,
    setLastEventIndex,
    pollingInterval,
    setPollingInterval,
    totalEvents,
    setTotalEvents,
    lastEventCount,
    setLastEventCount,
    events,
    setEvents,
    sessionId,
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

  // Get active preset for workflow mode
  const activeWorkflowPreset = getActivePreset('workflow')
  const selectedWorkflowPreset = activeWorkflowPreset?.id || null

  // Computed values
  const isRequiredFolderSelected = useMemo(() => {
    if (agentMode !== 'orchestrator' && agentMode !== 'workflow') return true; // No validation needed for other modes
    
    if (agentMode === 'orchestrator') {
      // Deep Search mode requires Tasks/ folder
      const hasTasksFolder = chatFileContext.some((file: { type: string; path: string }) => 
        file.type === 'folder' && file.path.startsWith('Tasks/')
      );
      return hasTasksFolder;
    } else if (agentMode === 'workflow') {
      // Workflow mode requires Workflow/ folder
      const hasWorkflowFolder = chatFileContext.some((file: { type: string; path: string }) => 
        file.type === 'folder' && file.path.startsWith('Workflow/')
      );
      return hasWorkflowFolder;
    }
    
    return true;
  }, [agentMode, chatFileContext])

  // Use currentPresetServers from props (passed from App.tsx when preset is selected)

  // State for preset selection overlay
  const [showPresetSelection, setShowPresetSelection] = useState(false)
  const [pendingModeCategory, setPendingModeCategory] = useState<'deep-research' | 'workflow' | null>(null)
  
  // State for mode switch dialog
  const [showModeSwitchDialog, setShowModeSwitchDialog] = useState(false)
  const [pendingModeSwitch, setPendingModeSwitch] = useState<'chat' | 'deep-research' | 'workflow' | null>(null)
  

  // Handle mode selection from dropdown
  const handleModeSelect = (category: 'chat' | 'deep-research' | 'workflow') => {
    if (category === selectedModeCategory) {
      return
    }

    // Check if there's an active chat session
    const hasActiveChat = events.length > 0 || isStreaming
    
    if (hasActiveChat) {
      // Show mode switch dialog for confirmation
      setPendingModeSwitch(category)
      setShowModeSwitchDialog(true)
    } else {
      // Switch mode directly
      handleModeSwitchWithPreset(category)
      // Clear backend session and reset UI after mode switch
      handleNewChat()
    }
  }

  // Handle mode switching with preset selection for Deep Research/Workflow
  const handleModeSwitchWithPreset = (category: 'chat' | 'deep-research' | 'workflow') => {
    if (category === 'chat') {
      // Chat mode doesn't need preset selection
      // Clear any active presets when switching to chat mode
      clearActivePreset('deep-research')
      clearActivePreset('workflow')
      switchMode(category)
    } else {
      // Deep Research or Workflow mode - always show preset selection when switching between modes
      // Clear the current mode's preset first
      if (selectedModeCategory === 'deep-research') {
        clearActivePreset('deep-research')
      } else if (selectedModeCategory === 'workflow') {
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
  const switchMode = (category: 'chat' | 'deep-research' | 'workflow') => {
    const { setModeCategory, getAgentModeFromCategory } = useModeStore.getState()
    const { setAgentMode } = useAppStore.getState()
    
    setModeCategory(category)
    
    // Set the corresponding agent mode using centralized mapping
    const agentModeToSet = getAgentModeFromCategory(category) as 'simple' | 'ReAct' | 'orchestrator' | 'workflow'
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
  
  // Handle guidance change (simplified - just log for now)
  const handleGuidanceChange = useCallback(() => {
    // Guidance updated
  }, [])
  
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
  
  // Add ref for orchestrator mode handler
  const orchestratorModeHandlerRef = useRef<OrchestratorModeHandlerRef>(null)
  
  
  // Orchestrator execution mode state
  const [orchestratorExecutionMode, setOrchestratorExecutionMode] = useState<OrchestratorExecutionMode>(EXECUTION_MODES.PARALLEL)
  
  // Performance metrics tracking (dev mode only)
  const [performanceMetrics, setPerformanceMetrics] = useState({
    renderCount: 0,
    lastRenderTime: 0,
    memoryEstimate: 0
  })
  
  // Handle orchestrator execution mode change
  const handleOrchestratorExecutionModeChange = useCallback((mode: OrchestratorExecutionMode) => {
    setOrchestratorExecutionMode(mode)
  }, [])
  
  // Track performance metrics when events change (dev mode only)
  useEffect(() => {
    if (process.env.NODE_ENV === 'development') {
      const startTime = performance.now()
      
      // Calculate memory estimate (rough approximation)
      const memoryEstimate = events.reduce((total, event) => {
        return total + JSON.stringify(event).length * 2 // Rough estimate
      }, 0)

      const endTime = performance.now()
      
      setPerformanceMetrics(prev => ({
        renderCount: prev.renderCount + 1,
        lastRenderTime: endTime - startTime,
        memoryEstimate: Math.round(memoryEstimate / 1024) // KB
      }))
    }
  }, [events])
  
  // Track processed completion events to avoid stopping on old ones
  const processedCompletionEventsRef = useRef<Set<string>>(new Set())

  // Selected preset folder state
  const lastEventIndexRef = useRef<number>(0)
  const totalEventsRef = useRef<number>(0)

  // Toast wrapper for components that only support limited types
  const addToastLimited = useCallback((message: string, type: 'success' | 'info') => {
    addToast(message, type)
  }, [addToast])

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
    if (isScrollingUp && scrollDistance > 50 && autoScroll) {
      setAutoScroll(false);
      addToast('Auto-scroll disabled', 'info');
    }
    // Re-enable auto-scroll when user scrolls back to bottom
    else if (wasAtBottom && !autoScroll) {
      setAutoScroll(true);
      addToast('Auto-scroll enabled', 'success');
    }
    // Re-enable auto-scroll if user scrolled down significantly and is near bottom
    else if (isScrollingDown && scrollDistance > 30 && !wasAtBottom && !autoScroll) {
      // Check if user is close to bottom (within 100px)
      const distanceFromBottom = element.scrollHeight - element.scrollTop - element.clientHeight;
      if (distanceFromBottom < 100) {
        setAutoScroll(true);
        addToast('Auto-scroll enabled', 'success');
      }
    }
    
    // Update last scroll position immediately
    setLastScrollTop(currentScrollTop);
  }, [autoScroll, isAtBottom, lastScrollTop, setAutoScroll, setLastScrollTop, addToast]);


  // Set up scroll event listener
  useEffect(() => {
    const element = chatContentRef.current;
    if (!element) return;

    // Initialize lastScrollTop with current position
    setLastScrollTop(element.scrollTop);

    element.addEventListener('scroll', handleScroll);
    return () => element.removeEventListener('scroll', handleScroll);
  }, [handleScroll, setLastScrollTop]);

  // Reset auto-scroll when starting new conversation
  useEffect(() => {
    if (events.length > 0 && !isStreaming) {
      setAutoScroll(true);
    }
  }, [events.length, isStreaming, setAutoScroll]);

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
  }, []);

  // Auto-scroll to bottom when new events arrive (only if autoScroll is enabled)
  useEffect(() => {
    if (autoScroll && chatContentRef.current && events.length > 0) {
      // Use requestAnimationFrame to ensure DOM has updated with new content
      requestAnimationFrame(() => {
        const element = chatContentRef.current;
        if (!element) return;
        
        // Always scroll to bottom when auto-scroll is enabled and new events arrive
        // The scroll handler will disable auto-scroll if user manually scrolls away
        scrollToBottom('smooth');
      });
    }
  }, [events.length, autoScroll, scrollToBottom])

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


  // Update refs when values change
  useEffect(() => {
    lastEventIndexRef.current = lastEventIndex
  }, [lastEventIndex])
  
  useEffect(() => {
    totalEventsRef.current = totalEvents
  }, [totalEvents])

  // Workflow preset handlers
  const handleWorkflowPresetSelected = useCallback(async (presetId: string, presetContent: string) => {
    // Clear previous file context when switching workflow presets
    clearFileContext()
    // Apply the preset using the global preset store
    applyPreset(presetId, 'workflow')
    setCurrentWorkflowQueryId(presetId) // Store the preset query ID for workflow approval
    
    try {
      // Check if workflow already exists for this preset
      const workflowStatus = await agentApi.getWorkflowStatus(presetId)
      
      if (workflowStatus.success && workflowStatus.workflow) {
        const workflow = workflowStatus.workflow
        const status = workflow.workflow_status
        
        // Set the workflow phase based on the database status
        if (status === WORKFLOW_PHASES.POST_VERIFICATION) {
          setCurrentWorkflowPhase(WORKFLOW_PHASES.POST_VERIFICATION)
        } else if (status === WORKFLOW_PHASES.POST_VERIFICATION_TODO_REFINEMENT) {
          setCurrentWorkflowPhase(WORKFLOW_PHASES.POST_VERIFICATION_TODO_REFINEMENT)
        } else {
          setCurrentWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
        }
        
        // Use presetContent directly (this is the objective from preset query)
        setCurrentQuery(presetContent)
      } else {
        // No workflow exists, proceed with normal flow
        setCurrentWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
        setCurrentQuery(presetContent)
      }
    } catch (error) {
      console.error('[WORKFLOW] Error checking workflow status:', error)
      // Fallback to normal flow on error
      setCurrentWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
      setCurrentQuery(presetContent)
    }
  }, [setCurrentQuery, applyPreset, setCurrentWorkflowPhase, setCurrentWorkflowQueryId, clearFileContext])

  const handleWorkflowPresetCleared = useCallback(() => {
    clearActivePreset('workflow')
    setCurrentWorkflowQueryId(null) // Clear the stored preset query ID
    setCurrentWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION) // Reset to preset selection phase
    setCurrentQuery('')
  }, [clearActivePreset, setCurrentWorkflowQueryId, setCurrentWorkflowPhase, setCurrentQuery])
  
  // Clear workflow state when starting a new chat
  const clearWorkflowState = useCallback(() => {
    clearActivePreset('workflow')
    setCurrentWorkflowQueryId(null)
    setCurrentWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
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
      const nextPhase = eventData?.next_phase || WORKFLOW_PHASES.POST_VERIFICATION
      
      // Update workflow status to the determined next phase
      await agentApi.updateWorkflow(presetQueryId, nextPhase)
      
      // Stop any ongoing polling to prevent events from coming back
      if (pollingInterval) {
        clearInterval(pollingInterval)
        setPollingInterval(null)
      }
      
      // Clear all events to show clean slate for execution phase
      setEvents([])
      setTotalEvents(0)
      setLastEventCount(0)
      setLastEventIndex(0)
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
  }, [currentWorkflowQueryId, pollingInterval, setIsApprovingWorkflow, setEvents, setTotalEvents, setLastEventCount, setLastEventIndex, setFinalResponse, setIsCompleted, setCurrentUserMessage, setShowUserMessage, setCurrentWorkflowPhase, setPollingInterval])
  */

  // Initialize observer on mount (only if not loading from chat session)
  useEffect(() => {
    // If we have a chatSessionId and don't require a new chat, don't initialize observer
    if (chatSessionId && !requiresNewChat) {
      console.log('[INIT] Skipping observer initialization - chatSessionId exists and requiresNewChat is false')
      return
    }
    
    // If requiresNewChat is true, clear the chatSessionId to force fresh initialization
    if (requiresNewChat) {
      console.log('[INIT] requiresNewChat is true - clearing chatSessionId for fresh initialization')
      useAppStore.getState().setChatSessionId('')
    }
    
    // Check if we need to initialize observer (no observerId or requiresNewChat)
    if (observerId && !requiresNewChat) {
      console.log('[INIT] Skipping observer initialization - already have working observer:', observerId)
      return
    }
    
    console.log('[INIT] Starting observer initialization...')
    
    // Clear any existing observer ID to ensure fresh start
    setObserverId('')
    
    let retryCount = 0
    const maxRetries = 3
    const retryDelay = 1000 // 1 second
    let retryTimeout: ReturnType<typeof setTimeout> | null = null
    
    const initializeObserver = async () => {
      try {
        console.log(`[INIT] Attempting to register observer (attempt ${retryCount + 1}/${maxRetries + 1})`)
        const response = await agentApi.registerObserver()
        
        if (response.observer_id) {
          console.log(`[INIT] Observer registered successfully: ${response.observer_id}`)
          setObserverId(response.observer_id)
          
          // Clear the requiresNewChat flag after successful initialization
          useAppStore.getState().clearRequiresNewChat()
          console.log('[INIT] Cleared requiresNewChat flag after successful observer registration')
        } else {
          console.error('[INIT] No observer_id received from server')
          // Retry if we haven't exceeded max retries
          if (retryCount < maxRetries) {
            retryCount++
            console.log(`[INIT] Retrying observer registration in ${retryDelay}ms...`)
            retryTimeout = setTimeout(() => initializeObserver(), retryDelay)
          } else {
            console.error('[INIT] Max retries exceeded, giving up on observer registration')
          }
        }
      } catch (error) {
        console.error(`[INIT] Failed to register observer (attempt ${retryCount + 1}):`, error)
        if (error instanceof Error) {
          console.error('[INIT] Error details:', {
            name: error.name,
            message: error.message,
            stack: error.stack
          })
        } else {
          console.error('[INIT] Unknown error type:', typeof error, error)
        }
        
        // Retry if we haven't exceeded max retries
        if (retryCount < maxRetries) {
          retryCount++
          console.log(`[INIT] Retrying observer registration in ${retryDelay}ms...`)
          retryTimeout = setTimeout(() => initializeObserver(), retryDelay)
        } else {
          console.error('[INIT] Max retries exceeded, giving up on observer registration')
        }
      }
    }

    initializeObserver()
    
    // Cleanup function to clear any pending retry timeout
    return () => {
      if (retryTimeout) {
        clearTimeout(retryTimeout)
        console.log('[INIT] Cleaned up observer registration retry timeout')
      }
    }
  }, [chatSessionId, setObserverId, requiresNewChat, observerId])

  // Event batching for performance
  const eventBatchRef = useRef<PollingEvent[]>([])
  const batchTimeoutRef = useRef<NodeJS.Timeout | null>(null)
  
  // Debounced function to flush event batch
  const flushEventBatch = useCallback(() => {
    if (eventBatchRef.current.length === 0) return
    
    const batch = [...eventBatchRef.current]
    eventBatchRef.current = []
    
    // Process the batch of events
    setEvents((prevEvents: PollingEvent[]) => {
      const updatedEvents = [...prevEvents, ...batch]
      
      // Trigger cleanup if threshold exceeded (handled by setEvents)
      return updatedEvents
    })
    
    // Update counters
    setTotalEvents(totalEventsRef.current + batch.length)
    setLastEventCount(batch.length)
  }, [setEvents, setTotalEvents, setLastEventCount])
  
  // Create debounced flush function (100ms delay)
  const debouncedFlush = useMemo(
    () => debounce(flushEventBatch, 100),
    [flushEventBatch]
  )

  // Polling function to get events
  const pollEvents = useCallback(async () => {
    const currentLastEventIndex = lastEventIndexRef.current
    
    if (!observerId) {
      return
    }

    try {
      const response = await agentApi.getEvents(observerId, currentLastEventIndex)
      
      if (response.events.length > 0) {
        
        // Update last event index immediately
        setLastEventIndex(response.last_event_index)
        
        // Add new events to batch for debounced processing
        const newEvents = response.events.filter(event => {
            // Debug: Log all tool_call_start events to see what we're getting
            if (event.type === 'tool_call_start') {
              // const eventData = event.data as Record<string, unknown> 
            }
            
            // Detect request human feedback event and stop streaming
            if (event.type === 'request_human_feedback') {
              setIsStreaming(false)
              setIsCompleted(false) // Not completed, just paused for human input
              
              // Stop polling when human feedback is requested
              if (pollingInterval) {
                clearInterval(pollingInterval)
                setPollingInterval(null)
              }
            }
            
            
            // Process workspace events using the centralized store
            const { processWorkspaceEvent } = useWorkspaceStore.getState()
            processWorkspaceEvent(event)

            // Process workflow-specific events
            if (agentMode === 'workflow') {
              // Handle todo list generation from orchestrator agent
              if (event.type === 'orchestrator_agent_end') {
                const agentEvent = event.data?.orchestrator_agent_end
                if (agentEvent?.agent_type === 'todo_planner') {

                  const result = agentEvent.result || ''
                  if (result) {
                    // Only reset to PRE_VERIFICATION if workflow hasn't been approved yet
                    // This prevents resetting the phase after user approval
                    if (currentWorkflowPhase === WORKFLOW_PHASES.POST_VERIFICATION) {
                      // Workflow already approved, keeping POST_VERIFICATION phase
                    } else {
                      setCurrentWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
                    }
                  }
                }
              }

              // Handle workflow completion events
              if (event.type === 'workflow_end') {
                setCurrentWorkflowPhase(WORKFLOW_PHASES.POST_VERIFICATION)
              }

            }
            
            // Only filter out user_message events from backend since we add them immediately in submitQuery
            return event.type !== 'user_message'
          })
          
    // Add events to batch instead of immediately processing
    eventBatchRef.current.push(...newEvents)
          
          // Trigger debounced flush
          debouncedFlush()
        
        // Check for completion events and stop polling if detected
        const completionEvents = response.events.filter((event: PollingEvent) => {
          // Skip events we've already processed to avoid stopping on old completion events
          if (processedCompletionEventsRef.current.has(event.id)) {
            return false
          }
          
          // Completion detection based on agent mode
          if (agentMode === 'orchestrator') {
            // For Deep Search mode, only check Deep Search-specific events
            // Don't check unified_completion as orchestrator uses multiple agents
            return event.type === 'orchestrator_end' ||
                   event.type === 'orchestrator_error'
          } else if (agentMode === 'workflow') {
            // For workflow mode, check workflow-specific events
            return event.type === 'workflow_end' ||
                   event.type === 'request_human_feedback'
          } else {
            // For simple and ReAct modes, check standard completion events
            return event.type === 'unified_completion' ||
                   event.type === 'agent_end' ||
                   event.type === 'conversation_end' || 
                   event.type === 'conversation_error' ||
                   event.type === 'agent_error'
          }
        })
        
        if (completionEvents.length > 0) {
          // Mark these completion events as processed to avoid reprocessing them
          completionEvents.forEach(event => {
            processedCompletionEventsRef.current.add(event.id)
          })
          
          // Force flush any pending events before stopping polling
          if (eventBatchRef.current.length > 0) {
            flushEventBatch()
          }
          
          if (pollingInterval) {
            clearInterval(pollingInterval)
            setPollingInterval(null)
          }
          setIsStreaming(false)
          setIsCompleted(true)
          setHasActiveChat(false)
          
          // Check for unified_completion event first - it takes precedence (only for non-Deep Search modes)
          let hasError = false
          let finalResult = ''
          
          if (agentMode !== 'orchestrator') {
            const unifiedCompletionEvent = completionEvents.find((event: PollingEvent) => 
              event.type === 'unified_completion'
            )
            
            if (unifiedCompletionEvent && unifiedCompletionEvent.data && typeof unifiedCompletionEvent.data === 'object') {
              const data = unifiedCompletionEvent.data as Record<string, unknown>
              
              // Check status from unified_completion event
              if (data.status === 'error' || data.status === 'failed') {
                hasError = true
                finalResult = (data.error as string) || (data.final_result as string) || 'Agent encountered an error.'
              } else if (data.status === 'completed' && data.final_result) {
                // Success case - use the final_result from unified_completion
                hasError = false
                finalResult = data.final_result as string
              }
            }
          }
          
          // If no unified_completion event or no final result, check for individual error events
          if (!finalResult) {
            if (agentMode === 'orchestrator') {
              // For Deep Search mode, check Deep Search-specific errors
              hasError = completionEvents.some((event: PollingEvent) => 
                event.type === 'orchestrator_error'
              )
            } else if (agentMode === 'workflow') {
              // For workflow mode, check workflow-specific errors
              hasError = completionEvents.some((event: PollingEvent) => 
                event.type === 'agent_error'
              )
            } else {
              // For simple and ReAct modes, check standard errors
              hasError = completionEvents.some((event: PollingEvent) => 
                event.type === 'conversation_error' || 
                event.type === 'agent_error'
              )
            }
            
            if (hasError) {
              finalResult = 'Agent encountered an error.'
            }
          }
          
          if (hasError) {
            // Error case - no final response set
          } else if (finalResult) {
            // We already have a final result from unified_completion
            // Skip processing other completion events since we already have the result
            return
          } else {
            // Extract final response from completion events - AGENT MODE SPECIFIC LOGIC
            let foundFinalResponse = false
            for (const event of completionEvents) {
              let result: string | undefined
              
              if (agentMode === 'orchestrator') {
                // For Deep Search mode, only check orchestrator_end events
                if (event.type === 'orchestrator_end' && event.data && typeof event.data === 'object') {
                  if ('result' in event.data) {
                    result = (event.data as { result?: string }).result
                  } else if ('orchestrator_end' in event.data) {
                    const orchData = (event.data as { orchestrator_end?: { result?: string } }).orchestrator_end
                    if (orchData && orchData.result) {
                      result = orchData.result
                    }
                  }
                }
              } else if (agentMode === 'workflow') {
                // For workflow mode, check workflow_end events
                if (event.type === 'workflow_end' && event.data && typeof event.data === 'object') {
                  if ('result' in event.data) {
                    result = (event.data as { result?: string }).result
                  } else if ('workflow_end' in event.data) {
                    const workflowData = (event.data as { workflow_end?: { result?: string } }).workflow_end
                    if (workflowData && workflowData.result) {
                      result = workflowData.result
                    }
                  }
                }
                // Also check conversation_end events for workflow mode
                if (!result && event.type === 'conversation_end' && event.data && typeof event.data === 'object') {
                  if ('result' in event.data) {
                    result = (event.data as { result?: string }).result
                  } else if ('conversation_end' in event.data) {
                    const convData = (event.data as { conversation_end?: { result?: string } }).conversation_end
                    if (convData && convData.result) {
                      result = convData.result
                    }
                  }
                }
              } else {
                // For simple and ReAct modes, check standard events
                // Skip unified_completion events since we already handled them above
                if (event.type === 'unified_completion') {
                  continue
                }
                
                // Check legacy conversation_end events
                if (!result && event.type === 'conversation_end' && event.data && typeof event.data === 'object') {
                  if ('result' in event.data) {
                    result = (event.data as { result?: string }).result
                  } else if ('conversation_end' in event.data) {
                    const convData = (event.data as { conversation_end?: { result?: string } }).conversation_end
                    if (convData && convData.result) {
                      result = convData.result
                    }
                  }
                }
              }
              
              
              if (result && typeof result === 'string' && result.trim()) {
                foundFinalResponse = true
                break
              }
            }
            
            // If no final response found in completion events, check if we have a preserved one
            if (!foundFinalResponse && finalResponse) {
              // No new final response found, keeping existing one
            }
            
            // Fallback: Check for ReAct reasoning events with final answers
            if (!foundFinalResponse) {
              for (const event of completionEvents) {
                if (event.type === 'react_reasoning_final' || event.type === 'react_reasoning_end') {
                  if (event.data && typeof event.data === 'object') {
                    let result: string | undefined
                    
                    // Check for final_answer field in ReAct events
                    if ('final_answer' in event.data) {
                      result = (event.data as { final_answer?: string }).final_answer
                    }
                    
                    // Check for content field as fallback
                    if (!result && 'content' in event.data) {
                      result = (event.data as { content?: string }).content
                    }
                    
                    if (result && typeof result === 'string' && result.trim()) {
                      foundFinalResponse = true
                      break
                    }
                  }
                }
              }
            }
          }
        }
      }
    } catch (error) {
      console.error('[POLL] Error polling events:', error)
      if (error instanceof Error) {
        console.error('[POLL] Error details:', error.message)
      }
      if (error && typeof error === 'object' && 'response' in error) {
        const axiosError = error as { response?: { status?: number; data?: unknown } }
        console.error('[POLL] HTTP status:', axiosError.response?.status)
        console.error('[POLL] HTTP data:', axiosError.response?.data)
      }
    }
  }, [observerId, pollingInterval, setPollingInterval, setIsStreaming, setIsCompleted, setHasActiveChat, setLastEventIndex, finalResponse, agentMode, setCurrentWorkflowPhase, currentWorkflowPhase, debouncedFlush, flushEventBatch])


  // Track if we're already processing to prevent infinite loops
  const processingRef = useRef<string | null>(null)
  
  // Cleanup polling on unmount
  useEffect(() => {
    const timeout = batchTimeoutRef.current
    return () => {
      if (pollingInterval) {
        clearInterval(pollingInterval)
      }
      // Cleanup debounced function
      debouncedFlush.cancel()
      // Cleanup batch timeout if it exists
      if (timeout) {
        clearTimeout(timeout)
      }
    }
  }, [pollingInterval, debouncedFlush])
  
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
            const response = await agentApi.getSessionEvents(originalSessionId, 1000, 0)
            
            // Convert and set historical events
            const pollingEvents: PollingEvent[] = response.events.map(event => ({
              id: event.id,
              type: event.event_type,
              timestamp: event.timestamp,
              data: event.event_data,
              session_id: event.session_id,
              parent_id: undefined,
              hierarchy_level: 0,
              span_id: undefined,
              trace_id: undefined,
              correlation_id: undefined,
              component: undefined,
              event_index: 0
            }))
            
            setEvents(pollingEvents)
            setTotalEvents(pollingEvents.length)
            setIsLoadingHistory(false)
            
            // Now reconnect to active session for live updates
            const reconnectResponse = await agentApi.reconnectSession(activeSession.session_id)
            
            if (reconnectResponse.observer_id) {
              setObserverId(reconnectResponse.observer_id)
              setIsStreaming(true)
              setIsCompleted(false)
              
              // Use the ORIGINAL observer ID from the active session, not the new one from reconnection
              const originalObserverId = activeSession.observer_id
              
              // Start polling for new events with the ORIGINAL observer ID
              const interval = setInterval(async () => {
                // Use the original observer ID that has the events
                const currentLastEventIndex = lastEventIndexRef.current
                
                try {
                  const response = await agentApi.getEvents(originalObserverId, currentLastEventIndex)
                  
                  if (response.events.length > 0) {
                    
                    // Process events
                    setLastEventIndex(response.last_event_index)
                    setTotalEvents(totalEventsRef.current + response.events.length)
                    setLastEventCount(response.events.length)
                    
                    // Add new events to the events array
                    const newEvents = response.events.filter(event => {
                        // Detect request human feedback event and stop streaming
                        if (event.type === 'request_human_feedback') {
                          setIsStreaming(false)
                          setIsCompleted(false)
                          
                          // Stop polling when human feedback is requested
                          if (pollingInterval) {
                            clearInterval(pollingInterval)
                            setPollingInterval(null)
                          }
                        }
                        
                        // Avoid duplicating user messages we inject on submit
                        if (event.type === 'user_message') {
                          return false
                        }
                        return true
                      })
                      
                      setEvents((prevEvents: PollingEvent[]) => [...prevEvents, ...newEvents])
                  }
                } catch (error) {
                  console.error('[POLL] Error polling events:', error)
                  if (error && typeof error === 'object' && 'response' in error) {
                    const axiosError = error as { response?: { status?: number; data?: unknown } }
                    console.error('[POLL] HTTP status:', axiosError.response?.status)
                    console.error('[POLL] HTTP data:', axiosError.response?.data)
                  }
                }
              }, 1000)
              setPollingInterval(interval)
              addToast('Reconnected to active session', 'success')
              
            } else {
              console.error('[SESSION_STATE] No observer_id in reconnect response:', reconnectResponse)
              addToast('Failed to reconnect - no observer ID', 'info')
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
            const response = await agentApi.getSessionEvents(originalSessionId, 1000, 0)
            
            // Convert and set events
            const pollingEvents: PollingEvent[] = response.events.map(event => ({
              id: event.id,
              type: event.event_type,
              timestamp: event.timestamp,
              data: event.event_data,
              session_id: event.session_id,
              parent_id: undefined,
              hierarchy_level: 0,
              span_id: undefined,
              trace_id: undefined,
              correlation_id: undefined,
              component: undefined,
              event_index: 0
            }))
            
            setEvents(pollingEvents)
            setTotalEvents(pollingEvents.length)
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
    if (pollingInterval) {
      clearInterval(pollingInterval)
      setPollingInterval(null)
    }
    setIsStreaming(false)
    
    // Call backend to stop the agent execution (preserves conversation history)
    if (observerId) {
      try {
        await agentApi.stopSession(observerId)
      } catch (error) {
        console.error('[STOP] Failed to stop session:', error)
      }
    }
  }, [pollingInterval, setPollingInterval, observerId, setIsStreaming])

  // Wrapper function to submit query with the current local query
  const submitQueryWithQuery = useCallback(async (query: string) => {
    if (!query?.trim()) {
      return
    }

    // Add validation check for Tasks folder requirement in Deep Search and Workflow modes
    if ((agentMode === 'orchestrator' || agentMode === 'workflow') && !isRequiredFolderSelected) {
      console.error(
        '[SUBMIT] Validation failed -',
        agentMode === 'workflow' ? 'Workflow' : 'Tasks',
        'folder required for',
        agentMode,
        'mode'
      )
      return
    }

    // Add file context to the query for ALL agent types
    const queryWithContext = chatFileContext.length > 0 
      ? `${query.trim()}\n\n📁 Files in context: ${chatFileContext.map((file: { path: string }) => file.path).join(', ')}`
      : query.trim()
    
    // Handle workflow mode - submit query directly to backend
    if (agentMode === 'workflow') {
      // For all workflow phases, submit the query directly to the backend
      // The backend will handle the appropriate workflow logic based on the current phase
      setCurrentQuery(queryWithContext)
      // Continue with normal agent execution below
    }

    // If currently streaming, stop it first
    if (isStreaming) {
      await stopStreaming()
    }
    
    // Ensure observer is ready
    if (!observerId) {
      console.error('[SUBMIT] No observer ID available, cannot submit query')
      return
    }

    // Use the query with file context that we prepared earlier
    const enhancedQuery = queryWithContext
    
    // Add user message as an event instead of floating popup
    const userMessageEvent: PollingEvent = {
      id: `user-message-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
      type: 'user_message',
      timestamp: new Date().toISOString(),
      data: {
        user_message: {
          content: query.trim(),
          timestamp: new Date().toISOString()
        }
      }
    }
    
    // Add user message event to the events array
    setEvents((prevEvents: PollingEvent[]) => [...prevEvents, userMessageEvent])
    
    setCurrentQuery('') // Clear the query text after submission

    // Clear any existing polling interval before starting a new one
    if (pollingInterval) {
      clearInterval(pollingInterval)
      setPollingInterval(null)
    }

    // Preserve the Final Result by adding it to events before clearing
    // Check both the current finalResponse state and any completion events in the events array
    const hasCompletionEvent = events.some(event => 
      event.type === 'unified_completion' || event.type === 'agent_end'
    )
    
    if (finalResponse && !hasCompletionEvent) {
      // Add the final response as a completion event if it's not already there
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
      setEvents((prevEvents: PollingEvent[]) => [...prevEvents, completionEvent])
    }

    // Clear the final response and completion state for the new query
    _setFinalResponse('')
    setIsCompleted(false)
    setIsStreaming(true)
    setHasActiveChat(true)

    // Reset event tracking for new query (preserve lastEventIndex for multi-turn chat)
    setLastEventCount(0)
    processedCompletionEventsRef.current.clear()

    try {
      // Submit query to backend
      const response = await agentApi.startQuery({
        query: enhancedQuery,
        agent_mode: agentMode,
        enabled_tools: enabledTools.map((tool: { name: string }) => tool.name),
        enabled_servers: effectiveServers,
        provider: llmConfig.provider,
        model_id: llmConfig.model_id,
        llm_config: llmConfig,
        preset_query_id: selectedWorkflowPreset || undefined,
        orchestrator_execution_mode: agentMode === 'orchestrator' ? orchestratorExecutionMode : undefined,
      })

      if (response.status === 'started' || response.status === 'workflow_started') {
        // Update session ID for subsequent requests
        if (response.query_id) {
          setSessionId(response.query_id)
        }
        
        // Start polling for events
        const interval = setInterval(pollEvents, 1000)
        setPollingInterval(interval)
      } else {
        console.error('[SUBMIT] Backend error:', response)
        setIsStreaming(false)
        setHasActiveChat(false)
      }
    } catch (error) {
      console.error('[SUBMIT] Failed to submit query:', error)
      setIsStreaming(false)
      setHasActiveChat(false)
    }

    // Reset orchestrator mode selection after submission
    if (agentMode === 'orchestrator') {
      orchestratorModeHandlerRef.current?.resetSelection?.()
    }
  }, [agentMode, isRequiredFolderSelected, chatFileContext, isStreaming, stopStreaming, observerId, events, finalResponse, pollingInterval, setPollingInterval, setEvents, setCurrentQuery, _setFinalResponse, setIsCompleted, setIsStreaming, setHasActiveChat, setLastEventCount, setLastEventIndex, setSessionId, llmConfig, effectiveServers, enabledTools, orchestratorExecutionMode, selectedWorkflowPreset, pollEvents, processedCompletionEventsRef])

  // Handle new chat - clear backend session and reset all chat state
  const handleNewChat = useCallback(async () => {
    // Clear conversation history from backend first (if observerId is available)
    if (observerId) {
      try {
        await agentApi.clearSession(observerId)
        console.log('[NEW_CHAT] Successfully cleared session:', observerId)
      } catch (error) {
        console.error('[NEW_CHAT] Failed to clear session:', error)
        // Continue with frontend reset even if backend clear fails
      }
    } else {
      console.log('[NEW_CHAT] No observerId available, skipping backend session clear')
    }
    
    // For workflow mode, preserve the selected preset but reset workflow phase
    if (agentMode === 'workflow' && selectedWorkflowPreset) {
      // Keep the preset selected, just reset the workflow phase
      setCurrentWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
      // Don't clear selectedWorkflowPreset or currentWorkflowQueryId
    } else {
      // For other modes, clear workflow state completely
      clearWorkflowState()
    }
    
    // Reset frontend state
    resetChatState()
    
    // Explicitly reset events and tracking for new chat
    setEvents([])
    setTotalEvents(0)
    setLastEventCount(0)
    setLastEventIndex(0)
    processedCompletionEventsRef.current.clear()
    
    // Clear guidance state
    setSessionId(null)
    
    // Call the parent's new chat handler
    onNewChat()
  }, [clearWorkflowState, resetChatState, onNewChat, observerId, setSessionId, setOrchestratorExecutionMode, agentMode, selectedWorkflowPreset, setCurrentWorkflowPhase])

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
    refreshWorkflowPresets
  }), [handleNewChat, resetChatState, refreshWorkflowPresets])

  return (
    <div className="flex flex-col h-full min-w-0">
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

      {/* Header */}
      <ChatHeader
        chatSessionTitle={chatSessionTitle}
        chatSessionId={chatSessionId}
        sessionState={sessionState === 'not_found' ? 'not-found' : sessionState}
        onModeSelect={handleModeSelect}
      />

      {/* Chat Content - Separated to prevent input re-renders */}
      <div ref={chatContentRef} className="flex-1 overflow-y-auto overflow-x-hidden min-w-0 relative">
        {/* Auto-scroll indicator */}
        {!autoScroll && (
          <div className="absolute top-4 right-4 z-10">
            <div className="bg-blue-100 dark:bg-blue-900 text-blue-800 dark:text-blue-200 px-3 py-1 rounded-full text-xs font-medium shadow-sm border border-blue-200 dark:border-blue-700">
              Auto-scroll disabled
            </div>
          </div>
        )}
        
        <div className="min-w-0 p-4">
          {/* Loading indicator for historical events */}
          {isLoadingHistory && (
            <div className="flex items-center justify-center py-8">
              <div className="flex items-center gap-3 text-gray-600 dark:text-gray-400">
                <div className="w-5 h-5 border-2 border-gray-300 dark:border-gray-600 border-t-blue-600 dark:border-t-blue-400 rounded-full animate-spin"></div>
                <span className="text-sm">Loading chat history...</span>
              </div>
            </div>
          )}

          {/* Loading indicator for active session checking */}
          {isCheckingActiveSessions && (
            <div className="flex items-center justify-center py-8">
              <div className="flex items-center gap-3 text-gray-600 dark:text-gray-400">
                <div className="w-5 h-5 border-2 border-gray-300 dark:border-gray-600 border-t-green-600 dark:border-t-green-400 rounded-full animate-spin"></div>
                <span className="text-sm">Checking for active session...</span>
              </div>
            </div>
          )}

          {/* Active session indicator */}
          {sessionState === 'active' && (
            <div className="flex items-center justify-center py-4">
              <div className="flex items-center gap-2 px-3 py-2 bg-green-100 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg">
                <div className="w-2 h-2 bg-green-500 rounded-full animate-pulse"></div>
                <span className="text-sm text-green-700 dark:text-green-300 font-medium">Live Session - Reconnected</span>
              </div>
            </div>
          )}

          {/* Session error indicator */}
          {sessionState === 'error' && (
            <div className="flex items-center justify-center py-4">
              <div className="flex items-center gap-2 px-3 py-2 bg-red-100 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
                <svg className="w-4 h-4 text-red-600 dark:text-red-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                  <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span className="text-sm text-red-700 dark:text-red-300 font-medium">Session Error - Unable to reconnect</span>
              </div>
            </div>
          )}
          

          {/* Show workflow explanation when in workflow mode but no preset selected */}
          <WorkflowExplanation agentMode={agentMode} selectedWorkflowPreset={selectedWorkflowPreset} />

          {/* Show preset selection message when in workflow mode with preset selected but no workflow started */}
          {agentMode === 'workflow' && selectedWorkflowPreset && !events.length && (
            <div className="flex items-center justify-center py-12">
              <div className="text-center max-w-md">
                <div className="w-16 h-16 mx-auto mb-4 bg-blue-100 dark:bg-blue-900/20 rounded-full flex items-center justify-center">
                  <svg className="w-8 h-8 text-blue-600 dark:text-blue-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                  </svg>
                </div>
                <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100 mb-2">
                  Preset Selected - Ready to Start Workflow
                </h3>
                <p className="text-sm text-gray-600 dark:text-gray-400 mb-4">
                  Your preset has been loaded. Enter your query below to begin the workflow execution with the selected tools and context.
                </p>
                <div className="flex items-center justify-center gap-2 text-xs text-gray-500 dark:text-gray-400">
                  <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                  <span>Type your query in the input field below to start</span>
                </div>
              </div>
            </div>
          )}

          {/* Show Deep Search explanation when in Deep Search mode */}
          <OrchestratorExplanation agentMode={agentMode} />

          {/* Show ReAct explanation when in ReAct mode */}
          <ReActExplanation agentMode={agentMode} />

        {agentMode === 'workflow' ? (
          <WorkflowModeHandler
            ref={workflowModeHandlerRef}
            onPresetSelected={handleWorkflowPresetSelected}
            onPresetCleared={handleWorkflowPresetCleared}
            onWorkflowPhaseChange={setCurrentWorkflowPhase}
          >
            {/* Empty State - Show when no events and not in historical session */}
            {!chatSessionId && events.length === 0 && !isStreaming && (
              <ModeEmptyState modeCategory={selectedModeCategory} />
            )}
            
            <EventDisplay />
          </WorkflowModeHandler>
        ) : agentMode === 'orchestrator' ? (
          <OrchestratorModeHandler
            ref={orchestratorModeHandlerRef}
            onExecutionModeChange={handleOrchestratorExecutionModeChange}
          >
            {/* Empty State - Show when no events and not in historical session */}
            {!chatSessionId && events.length === 0 && !isStreaming && (
              <ModeEmptyState modeCategory={selectedModeCategory} />
            )}
            
            <EventDisplay />
          </OrchestratorModeHandler>
        ) : (
          <>
            {/* Empty State - Show when no events and not in historical session */}
            {!chatSessionId && events.length === 0 && !isStreaming && (
              <ModeEmptyState modeCategory={selectedModeCategory} />
            )}
            
            <EventDisplay />
          </>
        )}
        </div>
      </div>

      {/* Input Area - Completely isolated from event updates */}
      {!chatSessionId && (
        <ChatInput
          onSubmit={submitQueryWithQuery}
          onStopStreaming={stopStreaming}
          onNewChat={handleNewChat}
        />
      )}
      
      {/* Streaming Status - Show at bottom when streaming */}
      {isStreaming && !chatSessionId && (
        <div className="px-3 py-1 border-t border-gray-200 dark:border-gray-700 bg-blue-50 dark:bg-blue-900/20">
          <div className="flex items-center justify-center gap-1 text-xs text-blue-600 dark:text-blue-400">
            <div className="w-1.5 h-1.5 bg-blue-500 rounded-full animate-pulse"></div>
            <span>Streaming</span>
            <span>📊 {totalEvents} ({lastEventCount})</span>
            
            {/* Performance Metrics (Dev Mode Only) */}
            {process.env.NODE_ENV === 'development' && (
              <>
                <span className="text-gray-500 dark:text-gray-400">|</span>
                <span>Renders: {performanceMetrics.renderCount}</span>
                <span>Memory: ~{performanceMetrics.memoryEstimate}KB</span>
                <span>Render: {performanceMetrics.lastRenderTime.toFixed(1)}ms</span>
              </>
            )}
          </div>
        </div>
      )}
      
      {/* Historical Session Notice */}
      {chatSessionId && (
        <div className="px-4 py-3 border-t border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800">
          <div className="text-center text-sm text-gray-600 dark:text-gray-400">
            <p>Viewing historical chat session</p>
            <p className="text-xs mt-1">
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
      
      {/* Floating Guidance Icon - Only show for Deep Search/workflow modes when streaming */}
      {(agentMode === 'orchestrator' || agentMode === 'workflow') && !chatSessionId && isStreaming && (
        <GuidanceFloatingIcon 
          sessionId={sessionId}
          onGuidanceChange={handleGuidanceChange}
          onAddToast={addToastLimited}
        />
      )}
    </div>
  )
})

ChatAreaInner.displayName = 'ChatAreaInner'

// Main ChatArea component that provides the EventMode context
const ChatArea = forwardRef<ChatAreaRef, ChatAreaProps>((props, ref) => {
  return (
    <EventModeProvider>
      <ChatAreaInner ref={ref} {...props} />
    </EventModeProvider>
  )
})

ChatArea.displayName = 'ChatArea'

export default ChatArea

