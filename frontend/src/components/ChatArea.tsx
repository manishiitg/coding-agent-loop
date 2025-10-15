import { useEffect, useRef, useCallback, forwardRef, useImperativeHandle, useMemo, useState } from 'react'
import { agentApi, type AgentQueryRequest } from '../services/api'
import type { PollingEvent, ActiveSessionInfo } from '../services/api-types'
import { EventModeProvider, EventModeToggle } from './events'
import { ChatInput } from './ChatInput'
import { EventDisplay } from './EventDisplay'
import { WorkflowModeHandler, type WorkflowModeHandlerRef } from './workflow'
import { getAgentModeDescription } from '../utils/agentModeDescriptions'
import { ToastContainer } from './ui/Toast'
import { useWorkspaceStore } from '../stores/useWorkspaceStore'
import { WORKFLOW_PHASES, type WorkflowPhase } from '../constants/workflow'
import { OrchestratorExplanation } from './OrchestratorExplanation'
import { WorkflowExplanation } from './WorkflowExplanation'
import { ReActExplanation } from './ReActExplanation'
import GuidanceFloatingIcon from './GuidanceFloatingIcon'
import { useAppStore, useLLMStore, useMCPStore, useChatStore } from '../stores'
import { useModeStore } from '../stores/useModeStore'
import { usePresetStore } from '../stores/usePresetStore'
import { MessageCircle, Search, Workflow, Settings } from 'lucide-react'
import { ModeEmptyState } from './ModeEmptyState'
import { PresetSelectionOverlay } from './PresetSelectionOverlay'

interface ChatAreaProps {
  // New chat handler
  onNewChat: () => void
  
  // Selected preset folder path
  selectedPresetFolder?: string | null
  
  // Current preset servers (from preset selection)
  currentPresetServers?: string[]
  
  // Preset selection callbacks
  onPresetSelect?: (servers: string[], agentMode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow') => void
  onPresetFolderSelect?: (folderPath?: string) => void
}

// Ref interface for ChatArea component
export interface ChatAreaRef {
  handleNewChat: () => void
  resetChatState: () => void
  refreshWorkflowPresets: () => Promise<void>
}


// Inner component that can use the EventMode context
const ChatAreaInner = forwardRef<ChatAreaRef, ChatAreaProps>(({
  onNewChat,
  selectedPresetFolder,
  currentPresetServers = [],
  onPresetSelect,
  onPresetFolderSelect
}, ref) => {
  // Store subscriptions
  const { 
    agentMode, 
    currentQuery,
    setCurrentQuery,
    chatFileContext,
    clearFileContext,
    chatSessionId,
    chatSessionTitle
  } = useAppStore()
  
  const { selectedModeCategory } = useModeStore()
  const { getActivePreset } = usePresetStore()
  
  const { 
    primaryConfig: llmConfig,
    getCurrentLLMOption
  } = useLLMStore()
  
  const { 
    toolList: enabledTools,
    enabledServers,
    selectedServers: manualSelectedServers
  } = useMCPStore()
  
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
    currentUserMessage,
    setCurrentUserMessage,
    setShowUserMessage,
    sessionId,
    setSessionId,
    autoScroll,
    setAutoScroll,
    lastScrollTop,
    setLastScrollTop,
    finalResponse,
    setFinalResponse,
    setIsCompleted,
    isLoadingHistory,
    setIsLoadingHistory,
    setIsApprovingWorkflow,
    sessionState,
    setSessionState,
    isCheckingActiveSessions,
    setIsCheckingActiveSessions,
    selectedWorkflowPreset,
    setSelectedWorkflowPreset,
    workflowPhase,
    setWorkflowPhase,
    workflowPresetQueryId,
    setWorkflowPresetQueryId,
    toasts,
    addToast,
    removeToast,
    resetChatState,
    isAtBottom
  } = useChatStore()

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
  const primaryLLM = getCurrentLLMOption()
  
  // Mode indicator helpers
  const getModeIcon = (category: typeof selectedModeCategory) => {
    switch (category) {
      case 'chat':
        return <MessageCircle className="w-4 h-4 text-blue-600" />
      case 'deep-research':
        return <Search className="w-4 h-4 text-blue-600" />
      case 'workflow':
        return <Workflow className="w-4 h-4 text-blue-600" />
      default:
        return <MessageCircle className="w-4 h-4 text-gray-400" />
    }
  }

  const getModeName = (category: typeof selectedModeCategory) => {
    switch (category) {
      case 'chat':
        return 'Chat Mode'
      case 'deep-research':
        return 'Deep Research Mode'
      case 'workflow':
        return 'Workflow Mode'
      default:
        return 'Unknown Mode'
    }
  }

  // State for mode switching
  const [showModeSwitch, setShowModeSwitch] = useState(false)
  
  // State for preset selection overlay
  const [showPresetSelection, setShowPresetSelection] = useState(false)
  const [pendingModeCategory, setPendingModeCategory] = useState<'deep-research' | 'workflow' | null>(null)
  
  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (showModeSwitch) {
        const target = event.target as Element
        if (!target.closest('.mode-switch-dropdown')) {
          setShowModeSwitch(false)
        }
      }
    }
    
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [showModeSwitch])

  // Handle mode selection from dropdown
  const handleModeSelect = (category: 'chat' | 'deep-research' | 'workflow') => {
    if (category === selectedModeCategory) {
      setShowModeSwitch(false)
      return
    }

    // Check if there's an active chat session
    const hasActiveChat = events.length > 0 || isStreaming
    
    if (hasActiveChat) {
      // Show confirmation dialog
      // For now, we'll use a simple confirm dialog
      const confirmed = window.confirm(
        `Switching to ${category === 'chat' ? 'Chat Mode' : category === 'deep-research' ? 'Deep Research Mode' : 'Workflow Mode'} will start a new chat session and clear your current conversation. Continue?`
      )
      
      if (confirmed) {
        handleModeSwitchWithPreset(category)
      }
    } else {
      // Switch mode directly
      handleModeSwitchWithPreset(category)
    }
    
    setShowModeSwitch(false)
  }

  // Handle mode switching with preset selection for Deep Research/Workflow
  const handleModeSwitchWithPreset = (category: 'chat' | 'deep-research' | 'workflow') => {
    if (category === 'chat') {
      // Chat mode doesn't need preset selection
      switchMode(category)
    } else {
      // Deep Research or Workflow mode - check if preset is needed
      const { getActivePreset } = usePresetStore.getState()
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
    const { setModeCategory } = useModeStore.getState()
    const { setAgentMode } = useAppStore.getState()
    
    setModeCategory(category)
    
    // Set the corresponding agent mode
    let agentModeToSet: 'simple' | 'ReAct' | 'orchestrator' | 'workflow'
    switch (category) {
      case 'chat':
        agentModeToSet = 'ReAct' // Default to ReAct for chat
        break
      case 'deep-research':
        agentModeToSet = 'orchestrator'
        break
      case 'workflow':
        agentModeToSet = 'workflow'
        break
      default:
        agentModeToSet = 'ReAct'
    }
    
    setAgentMode(agentModeToSet)
    
    // Start a new chat when switching modes
    // Note: startNewChat will be handled by the parent component
  }

  // Handle preset selection from overlay
  const handlePresetSelected = (presetId: string) => {
    if (pendingModeCategory) {
      const { setActivePreset } = usePresetStore.getState()
      setActivePreset(pendingModeCategory, presetId)
      
      // Now switch to the mode
      switchMode(pendingModeCategory)
      
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

  const getActivePresetName = () => {
    if (!selectedModeCategory || (selectedModeCategory !== 'deep-research' && selectedModeCategory !== 'workflow')) {
      return null
    }
    const activePreset = getActivePreset(selectedModeCategory)
    return activePreset?.name || null
  }
  
  // Filter toasts to only include types supported by ToastContainer
  const filteredToasts = toasts.filter((toast: { type: string }) => toast.type === 'success' || toast.type === 'info') as Array<{id: string, message: string, type: 'success' | 'info'}>
  
  // Handle guidance change (simplified - just log for now)
  const handleGuidanceChange = useCallback(() => {
    // Guidance updated
  }, [])
  
  // Add ref for auto-scrolling
  const chatContentRef = useRef<HTMLDivElement>(null)
  
  // Add ref for workflow mode handler
  const workflowModeHandlerRef = useRef<WorkflowModeHandlerRef>(null)
  
  // Selected preset folder state
  const lastEventIndexRef = useRef<number>(0)

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
    if (currentUserMessage && !isStreaming) {
      setAutoScroll(true);
    }
  }, [currentUserMessage, isStreaming, setAutoScroll]);

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


  // Update ref when lastEventIndex changes
  useEffect(() => {
    lastEventIndexRef.current = lastEventIndex
  }, [lastEventIndex])

  // Workflow preset handlers
  const handleWorkflowPresetSelected = useCallback(async (presetId: string, presetContent: string) => {
    // Clear previous file context when switching workflow presets
    clearFileContext()
    setSelectedWorkflowPreset(presetId)
    setWorkflowPresetQueryId(presetId) // Store the preset query ID for workflow approval
    
    try {
      // Check if workflow already exists for this preset
      const workflowStatus = await agentApi.getWorkflowStatus(presetId)
      
      if (workflowStatus.success && workflowStatus.workflow) {
        const workflow = workflowStatus.workflow
        const status = workflow.workflow_status
        
        // Set the workflow phase based on the database status
        if (status === WORKFLOW_PHASES.POST_VERIFICATION) {
          setWorkflowPhase(WORKFLOW_PHASES.POST_VERIFICATION)
        } else if (status === WORKFLOW_PHASES.POST_VERIFICATION_TODO_REFINEMENT) {
          setWorkflowPhase(WORKFLOW_PHASES.POST_VERIFICATION_TODO_REFINEMENT)
        } else {
          setWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
        }
        
        // Use presetContent directly (this is the objective from preset query)
        setCurrentQuery(presetContent)
      } else {
        // No workflow exists, proceed with normal flow
        setWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
        setCurrentQuery(presetContent)
      }
    } catch (error) {
      console.error('[WORKFLOW] Error checking workflow status:', error)
      // Fallback to normal flow on error
      setWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
      setCurrentQuery(presetContent)
    }
  }, [setCurrentQuery, setSelectedWorkflowPreset, setWorkflowPhase, setWorkflowPresetQueryId, clearFileContext])

  const handleWorkflowPresetCleared = useCallback(() => {
    setSelectedWorkflowPreset(null)
    setWorkflowPresetQueryId(null) // Clear the stored preset query ID
    setWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION) // Reset to preset selection phase
    setCurrentQuery('')
  }, [setCurrentQuery, setSelectedWorkflowPreset, setWorkflowPresetQueryId, setWorkflowPhase])
  
  // Clear workflow state when starting a new chat
  const clearWorkflowState = useCallback(() => {
    setSelectedWorkflowPreset(null)
    setWorkflowPresetQueryId(null)
    setWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
  }, [setSelectedWorkflowPreset, setWorkflowPresetQueryId, setWorkflowPhase])

  // Handle human verification actions
  const handleApproveWorkflow = useCallback(async (_requestId: string, eventData?: { next_phase?: string }) => {
    
    setIsApprovingWorkflow(true)  // Set loading state
    
    // Use the stored preset query ID instead of the request ID
    const presetQueryId = workflowPresetQueryId
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
      setWorkflowPhase(nextPhase as WorkflowPhase)
      
    } catch (error) {
      console.error('[WORKFLOW] Failed to approve workflow:', error)
      // TODO: Show error message to user
    } finally {
      setIsApprovingWorkflow(false)  // Clear loading state
    }
  }, [workflowPresetQueryId, pollingInterval, setIsApprovingWorkflow, setEvents, setTotalEvents, setLastEventCount, setLastEventIndex, setFinalResponse, setIsCompleted, setCurrentUserMessage, setShowUserMessage, setWorkflowPhase, setPollingInterval])

  // Initialize observer on mount (only if not loading from chat session)
  useEffect(() => {
    if (chatSessionId) {
      return
    }
    
    // Clear any existing observer ID to ensure fresh start
    setObserverId('')
    
    const initializeObserver = async () => {
      try {
        const response = await agentApi.registerObserver()
        
        if (response.observer_id) {
          setObserverId(response.observer_id)
        } else {
          console.error('[INIT] No observer_id received from server')
        }
      } catch (error) {
        console.error('[INIT] Failed to register observer:', error)
        if (error instanceof Error) {
          console.error('[INIT] Error details:', {
            name: error.name,
            message: error.message,
            stack: error.stack
          })
        } else {
          console.error('[INIT] Unknown error type:', typeof error, error)
        }
      }
    }

    initializeObserver()
  }, [chatSessionId, setObserverId])

  // Cleanup polling on unmount
  useEffect(() => {
    return () => {
      if (pollingInterval) {
        clearInterval(pollingInterval)
      }
    }
  }, [pollingInterval])

  // Polling function to get events
  const pollEvents = useCallback(async () => {
    const currentLastEventIndex = lastEventIndexRef.current
    if (!observerId) {
      return
    }

    try {
      const response = await agentApi.getEvents(observerId, currentLastEventIndex)
      
      if (response.events.length > 0) {
        
        // Process events if we have any events (remove the lastEventIndex condition)
        // The condition was preventing early events from being displayed
        setLastEventIndex(response.last_event_index)
        setTotalEvents(totalEvents + response.events.length)
        setLastEventCount(response.events.length)
        
        // Add new events to the events array for rich UI display
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
                    if (workflowPhase === WORKFLOW_PHASES.POST_VERIFICATION) {
                      // Workflow already approved, keeping POST_VERIFICATION phase
                    } else {
                      setWorkflowPhase(WORKFLOW_PHASES.PRE_VERIFICATION)
                    }
                  }
                }
              }

              // Handle workflow completion events
              if (event.type === 'workflow_end') {
                setWorkflowPhase(WORKFLOW_PHASES.POST_VERIFICATION)
              }

            }
            
            // Only filter out user_message events from backend since we add them immediately in submitQuery
            return event.type !== 'user_message'
          })
          
          setEvents((prevEvents: PollingEvent[]) => {
            const updatedEvents = [...prevEvents, ...newEvents]
            return updatedEvents
          })
        
        // Check for completion events and stop polling if detected
        const completionEvents = response.events.filter((event: PollingEvent) => {
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
                   event.type === 'conversation_end' || 
                   event.type === 'conversation_error' ||
                   event.type === 'agent_error'
          }
        })
        
        if (completionEvents.length > 0) {
          
          if (pollingInterval) {
            clearInterval(pollingInterval)
            setPollingInterval(null)
          }
          setIsStreaming(false)
          setIsCompleted(true)
          
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
  }, [observerId, pollingInterval, setPollingInterval, setIsStreaming, setIsCompleted, setLastEventIndex, setTotalEvents, setLastEventCount, setEvents, finalResponse, agentMode, setWorkflowPhase, totalEvents, workflowPhase])


  // Track if we're already processing to prevent infinite loops
  const processingRef = useRef<string | null>(null)

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
                    setTotalEvents(totalEvents + response.events.length)
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

  const submitQuery = useCallback(async () => {
    if (!currentQuery?.trim()) {
      return
    }

    // Add validation check for Tasks folder requirement in Deep Search and Workflow modes
    if ((agentMode === 'orchestrator' || agentMode === 'workflow') && !isRequiredFolderSelected) {
      console.error('[SUBMIT] Validation failed - Tasks folder required for', agentMode, 'mode')
      return
    }

    // Add file context to the query for ALL agent types
    const queryWithContext = chatFileContext.length > 0 
      ? `${currentQuery?.trim()}\n\n📁 Files in context: ${chatFileContext.map((file: { path: string }) => file.path).join(', ')}`
      : currentQuery?.trim()
    
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
    const query = queryWithContext
    const enhancedQuery = query
    
    // Store user message for display (show original query without file context)
    setCurrentUserMessage(currentQuery?.trim() || '')
    
    // Show user message when new query is submitted
    setShowUserMessage(true)
    
    setCurrentQuery('') // Clear the query text after submission

    // Clear any existing polling interval before starting a new one
    if (pollingInterval) {
      clearInterval(pollingInterval)
      setPollingInterval(null)
    }

    // Preserve the Final Result by adding it to events before clearing
    // Check both the current finalResponse state and any completion events in the events array
    let resultToPreserve = finalResponse
    
    // If no finalResponse in state, check if there are completion events in the events array
    if (!resultToPreserve) {
      // Get current events from store
      const currentEvents = useChatStore.getState().events
      const lastCompletionEvent = currentEvents
        .filter((event: PollingEvent) => event.type === 'conversation_end')
        .pop()
      
      if (lastCompletionEvent && lastCompletionEvent.data && typeof lastCompletionEvent.data === 'object' && 'result' in lastCompletionEvent.data) {
        const result = (lastCompletionEvent.data as { result?: string }).result
        if (result && typeof result === 'string') {
          resultToPreserve = result
        }
      }
    }
    
    if (resultToPreserve) {
      const finalResultEvent: PollingEvent = {
        id: `final-result-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
        type: 'conversation_end',
        timestamp: new Date().toISOString(),
        data: {
          conversation_end: {
            result: resultToPreserve,
            duration: 0,
            turns: 1,
            status: 'completed'
          }
        }
      }
      
      // Add the final result to events so it's preserved in conversation history
      setEvents((prevEvents: PollingEvent[]) => [...prevEvents, finalResultEvent])
    }

    // Determine which servers to use for this query
    // Determine which servers to use based on mode and selection
    let serversToUse: string[]
    
    if (agentMode === 'simple' || agentMode === 'ReAct') {
      // For Simple/ReAct modes, use manual selection if available, otherwise use all enabled servers
      if (manualSelectedServers.length > 0) {
        serversToUse = manualSelectedServers
      } else {
        serversToUse = enabledServers
      }
    } else {
      // For Deep Search/Workflow modes, use preset servers if available, otherwise use all enabled servers
      serversToUse = currentPresetServers.length > 0 ? currentPresetServers : enabledServers
    }

    // Determine which LLM config to use for this query
    let llmConfigToUse = llmConfig // Default to sidebar's llmConfig
    
    if ((agentMode === 'simple' || agentMode === 'ReAct') && primaryLLM) {
      // For Simple/ReAct modes, use primaryLLM selection but preserve complete configuration
      llmConfigToUse = {
        ...llmConfig, // ✅ Preserve all existing configuration (fallbacks, cross-provider, etc.)
        provider: primaryLLM.provider as "openrouter" | "bedrock" | "openai",
        model_id: primaryLLM.model
      }
    }

    try {

      const request: AgentQueryRequest = {
        query: enhancedQuery,
        agent_mode: agentMode,
        enabled_tools: enabledTools.map((tool: { name: string }) => tool.name),
        enabled_servers: serversToUse,
        provider: llmConfigToUse.provider,
        model_id: llmConfigToUse.model_id,
        llm_config: llmConfigToUse,
        preset_query_id: selectedWorkflowPreset || undefined,
      }


      // Start polling for events IMMEDIATELY when query is submitted
      // This ensures we capture all events including early tool_call_start/end events
      const interval = setInterval(pollEvents, 1000) // Poll every 1000ms (1 second) during streaming
      setPollingInterval(interval)
      
      const response = await agentApi.startQuery(request)

      // Handle observer ID from response (for workflow mode)
      if (response.observer_id && response.observer_id !== observerId) {
        setObserverId(response.observer_id)
        // Update localStorage as well
        localStorage.setItem('agent_observer_id', response.observer_id)
      }

      // Set session ID for guidance functionality
      if (response.query_id) {
        setSessionId(response.query_id)
      }

      if (response.status === 'started' || response.status === 'workflow_started') {
        // Set streaming to true immediately when query starts successfully
        setIsStreaming(true)
        setIsCompleted(false)
    } else {
        console.error('[SUBMIT] Query failed:', response)
        // Only reset streaming state if query fails to start
        setIsStreaming(false)
        setIsCompleted(false)
        // Stop polling if query failed
        if (interval) {
          clearInterval(interval)
          setPollingInterval(null)
        }
      }
    } catch (error) {
      console.error('[SUBMIT] Error during query submission:', error)
      // Only reset streaming state if query fails
      setIsStreaming(false)
      setIsCompleted(false)
    }
  }, [currentQuery, isStreaming, observerId, currentPresetServers, enabledServers, enabledTools, agentMode, chatFileContext, finalResponse, pollEvents, pollingInterval, setCurrentQuery, llmConfig, stopStreaming, isRequiredFolderSelected, selectedWorkflowPreset, manualSelectedServers, primaryLLM, setCurrentUserMessage, setEvents, setIsCompleted, setIsStreaming, setObserverId, setPollingInterval, setSessionId, setShowUserMessage])


  // Handle new chat - clear backend session and reset all chat state
  const handleNewChat = useCallback(async () => {
    // Clear conversation history from backend first
    if (observerId) {
      try {
        await agentApi.clearSession(observerId)
      } catch (error) {
        console.error('[NEW_CHAT] Failed to clear session:', error)
      }
    }
    
    // Clear workflow state
    clearWorkflowState()
    
    // Reset frontend state
    resetChatState()
    
    // Clear guidance state
    setSessionId(null)
    
    // Call the parent's new chat handler
    onNewChat()
  }, [clearWorkflowState, resetChatState, onNewChat, observerId, setSessionId])

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
          onPresetSelect={onPresetSelect}
          onPresetFolderSelect={onPresetFolderSelect}
          setCurrentQuery={setCurrentQuery}
        />
      )}

      {/* Header */}
      <div className="px-4 py-3 border-b border-gray-200 dark:border-gray-700 flex-shrink-0 h-16">
        <div className="flex items-center justify-between h-full">
          <div className="min-w-0 flex-1">
            {/* Mode Indicator and Title */}
            <div className="flex items-center gap-2 mb-1">
              {selectedModeCategory && (
                <div className="relative">
                  <button
                    onClick={() => setShowModeSwitch(!showModeSwitch)}
                    className="flex items-center gap-1 px-2 py-1 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md hover:bg-blue-100 dark:hover:bg-blue-900/30 transition-colors cursor-pointer"
                    title="Click to change mode"
                  >
                    {getModeIcon(selectedModeCategory)}
                    <span className="text-xs font-medium text-blue-700 dark:text-blue-300">
                      {getModeName(selectedModeCategory)}
                    </span>
                    <Settings className="w-3 h-3 text-blue-600" />
                  </button>
                  
                  {/* Direct Mode Selection Dropdown */}
                  {showModeSwitch && (
                    <div className="mode-switch-dropdown absolute top-full left-0 mt-1 w-64 bg-white dark:bg-slate-800 border border-gray-200 dark:border-slate-700 rounded-lg shadow-lg z-50">
                      <div className="p-2 space-y-1">
                        {/* Chat Mode */}
                        <button
                          onClick={() => handleModeSelect('chat')}
                          className={`w-full text-left p-3 rounded-md text-sm transition-colors ${
                            selectedModeCategory === 'chat'
                              ? 'bg-blue-100 dark:bg-blue-900/30 text-blue-900 dark:text-blue-100'
                              : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                          }`}
                        >
                          <div className="flex items-center gap-3">
                            <MessageCircle className="w-4 h-4 text-blue-600" />
                            <div>
                              <div className="font-medium">Chat Mode</div>
                              <div className="text-xs text-gray-500 dark:text-gray-400">
                                Quick conversations and questions
                              </div>
                            </div>
                          </div>
                        </button>
                        
                        {/* Deep Research Mode */}
                        <button
                          onClick={() => handleModeSelect('deep-research')}
                          className={`w-full text-left p-3 rounded-md text-sm transition-colors ${
                            selectedModeCategory === 'deep-research'
                              ? 'bg-green-100 dark:bg-green-900/30 text-green-900 dark:text-green-100'
                              : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                          }`}
                        >
                          <div className="flex items-center gap-3">
                            <Search className="w-4 h-4 text-green-600" />
                            <div>
                              <div className="font-medium">Deep Research Mode</div>
                              <div className="text-xs text-gray-500 dark:text-gray-400">
                                Multi-step analysis and research
                              </div>
                            </div>
                          </div>
                        </button>
                        
                        {/* Workflow Mode */}
                        <button
                          onClick={() => handleModeSelect('workflow')}
                          className={`w-full text-left p-3 rounded-md text-sm transition-colors ${
                            selectedModeCategory === 'workflow'
                              ? 'bg-purple-100 dark:bg-purple-900/30 text-purple-900 dark:text-purple-100'
                              : 'hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-700 dark:text-gray-300'
                          }`}
                        >
                          <div className="flex items-center gap-3">
                            <Workflow className="w-4 h-4 text-purple-600" />
                            <div>
                              <div className="font-medium">Workflow Mode</div>
                              <div className="text-xs text-gray-500 dark:text-gray-400">
                                Todo-based task execution
                              </div>
                            </div>
                          </div>
                        </button>
                      </div>
                    </div>
                  )}
                </div>
              )}
              <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100 truncate">
                {chatSessionTitle ? `Chat: ${chatSessionTitle}` : 'Chat'}
              </h2>
            </div>
            
            {/* Preset Name (for Deep Research/Workflow modes) */}
            {(() => {
              const activePresetName = getActivePresetName()
              if (activePresetName) {
                return (
                  <div className="flex items-center gap-1 mb-1">
                    <div className="w-1.5 h-1.5 bg-blue-500 rounded-full"></div>
                    <span className="text-xs text-blue-600 dark:text-blue-400 font-medium">
                      {activePresetName}
                    </span>
                  </div>
                )
              }
              return null
            })()}
            
            {/* Session Status or Mode Description */}
            <p className="text-xs text-gray-600 dark:text-gray-400 truncate">
              {chatSessionId ? (
                sessionState === 'active' ? 'Live Session' : 
                sessionState === 'completed' ? 'Historical Session' :
                sessionState === 'loading' ? 'Checking session...' :
                sessionState === 'error' ? 'Session Error' :
                'Session Not Found'
              ) : getAgentModeDescription(agentMode)}
            </p>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            <EventModeToggle />
            {isStreaming && (
              <div className="text-xs text-gray-500 dark:text-gray-400">
                <span>
                  Streaming... 
                  <span className="ml-2">
                    📊 Events: {totalEvents} (Last: {lastEventCount})
                  </span>
                </span>
              </div>
            )}
          </div>
        </div>
      </div>

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

        <WorkflowModeHandler
          ref={workflowModeHandlerRef}
          onPresetSelected={handleWorkflowPresetSelected}
          onPresetCleared={handleWorkflowPresetCleared}
          onWorkflowPhaseChange={setWorkflowPhase}
        >
          {/* Empty State - Show when no events and not in historical session */}
          {!chatSessionId && events.length === 0 && !isStreaming && (
            <ModeEmptyState modeCategory={selectedModeCategory} />
          )}
          
          <EventDisplay 
            onDismissUserMessage={() => setShowUserMessage(false)}
            onApproveWorkflow={handleApproveWorkflow}
          />
        </WorkflowModeHandler>
        </div>
      </div>

      {/* Input Area - Completely isolated from event updates */}
      {!chatSessionId && (
        <ChatInput
          onSubmit={submitQuery}
          onStopStreaming={stopStreaming}
          onNewChat={handleNewChat}
          selectedPresetFolder={selectedPresetFolder}
        />
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
