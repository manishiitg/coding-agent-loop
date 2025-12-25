import { useState, useCallback, useMemo, useEffect, useRef } from 'react'
import { agentApi, getSessionId, setCurrentObserverId } from '../../../services/api'
import type { PollingEvent } from '../../../services/api-types'
import { useLLMStore, useMCPStore, useChatStore } from '../../../stores'
import { usePresetApplication } from '../../../stores/useGlobalPresetStore'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { EXECUTION_PHASE_ID } from '../../../constants/workflow'

export type WorkflowExecutionStatus =
  | 'idle'
  | 'running'
  | 'paused'
  | 'completed'
  | 'failed'
  | 'waiting_feedback'

export type StepStatus = 'pending' | 'running' | 'completed' | 'failed'

export interface UseWorkflowExecutionReturn {
  status: WorkflowExecutionStatus
  currentStepId: string | null
  stepStatusMap: Map<string, StepStatus> // Map of stepId -> status
  events: PollingEvent[]
  observerId: string
  error: string | null

  // Actions
  startWorkflow: (presetQueryId: string) => Promise<void>
  runStep: (stepId: string, presetQueryId: string) => Promise<void>
  pauseWorkflow: () => Promise<void>
  stopWorkflow: () => Promise<void>
  resumeWorkflow: () => Promise<void>
  clearEvents: () => void
}

/**
 * Hook to manage workflow execution from the canvas
 * Uses useChatStore as single source of truth for:
 * - observerId: session identifier
 * - events: event stream
 * - isStreaming: whether agent is running (source of truth for 'running' status)
 * - isCompleted: whether execution completed (source of truth for 'completed' status)
 */
export function useWorkflowExecution(): UseWorkflowExecutionReturn {
  // Get active tab (for multi-tab support - works for both chat and workflow)
  const activeTab = useChatStore(state => state.getActiveTab())
  
  // CRITICAL: Always use tab's observer ID - never fall back to global to prevent mixing
  const tabObserverId = activeTab?.observerId
  
  // Get tab-specific events and status
  const getTabEvents = useChatStore(state => state.getTabEvents)
  const getTabStreamingStatus = useChatStore(state => state.getTabStreamingStatus)
  
  // Use tab-specific data - never fall back to global
  const observerId = tabObserverId || null
  const tabIsStreaming = activeTab ? getTabStreamingStatus(activeTab.tabId) : false
  const isStreaming = tabIsStreaming
  const isCompleted = activeTab?.isCompleted || false
  const events = useMemo(() => {
    return observerId ? getTabEvents(observerId) : []
  }, [observerId, getTabEvents])
  
  // Warn if no active tab (tabs should always exist)
  if (!activeTab) {
    console.warn(`[useWorkflowExecution] No active tab - this should not happen in tab mode`)
  }
  
  // Workflow store actions
  const setSelectedRunFolder = useWorkflowStore(state => state.setSelectedRunFolder)
  const loadRunFolders = useWorkflowStore(state => state.loadRunFolders)
  const loadProgress = useWorkflowStore(state => state.loadProgress)
  const setTabStreaming = useChatStore(state => state.setTabStreaming)
  const updateTabSessionId = useChatStore(state => state.updateTabSessionId)

  // Local state for workflow-specific tracking
  const [currentStepId, setCurrentStepId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [manualStatus, setManualStatus] = useState<WorkflowExecutionStatus | null>(null)
  const [stepStatusMap, setStepStatusMap] = useState<Map<string, StepStatus>>(new Map())

  // Ref for tracking processed events (for step tracking only)
  const lastProcessedEventIndexRef = useRef<number>(-1)

  // Store subscriptions
  const { primaryConfig: llmConfig } = useLLMStore()
  const { toolList: allTools, selectedServers } = useMCPStore()
  const { currentPresetServers, currentPresetTools, getActivePreset } = usePresetApplication()

  // Get effective servers
  const effectiveServers = currentPresetServers.length > 0 ? currentPresetServers : selectedServers

  // Filter tools to only include those from effective servers
  const enabledTools = allTools.filter(tool =>
    tool.server && effectiveServers.includes(tool.server)
  )

  // Derive status from store states (ChatArea is the source of truth)
  // This eliminates redundant completion event scanning - ChatArea already does this
  const derivedStatus = useMemo((): WorkflowExecutionStatus => {
    // isStreaming is the source of truth for 'running' status
    // This MUST be checked first - if ChatArea is streaming, we're running
    // regardless of any previous manual status (e.g., 'idle' from a previous stop)
    if (isStreaming) {
      return 'running'
    }

    // Manual status takes priority for non-running states (e.g., 'paused', 'failed')
    // But NOT for 'idle' - we want natural state to take over when not streaming
    if (manualStatus && manualStatus !== 'idle') {
      return manualStatus
    }

    // Check for human feedback (ChatArea sets isStreaming=false, isCompleted=false for this)
    // Only check recent events to minimize scanning
    if (!isCompleted && events.length > 0) {
      const recentEvents = events.slice(-10)
      if (recentEvents.some(e => e.type === 'request_human_feedback')) {
        return 'waiting_feedback'
      }
    }

    // isCompleted is the source of truth for 'completed' status
    if (isCompleted) {
      return 'completed'
    }

    return 'idle'
  }, [isStreaming, isCompleted, events, manualStatus])

  // Track current step and step status from events
  useEffect(() => {
    if (events.length === 0) return

    // Only process new events for step tracking and status updates
    for (let i = lastProcessedEventIndexRef.current + 1; i < events.length; i++) {
      const event = events[i]

      // Debug: Log all step execution events (only in development)
      if (process.env.NODE_ENV === 'development' && (event.type?.includes('step_execution') || event.type?.includes('step_'))) {
        console.log('[useWorkflowExecution] Received step event:', {
          type: event.type,
          data: event.data,
          dataType: typeof event.data,
          dataKeys: event.data && typeof event.data === 'object' ? Object.keys(event.data) : 'not an object',
          hasStepId: event.data && typeof event.data === 'object' ? 'step_id' in (event.data as Record<string, unknown>) : false,
          stepIdValue: event.data && typeof event.data === 'object' ? (event.data as Record<string, unknown>).step_id : undefined
        })
      }

      // Handle step_started event
      if (event.type === 'step_execution_start') {
        // Access step_id - event.data might be the actual event data object directly
        // or it might be wrapped in EventData type (which doesn't include step execution events)
        const rawData = event.data as Record<string, unknown> | undefined
        let stepId: string | undefined
        let runFolder: string | undefined
        let workspacePath: string | undefined

        if (rawData && typeof rawData === 'object') {
          // Try direct access first (most common case - step_id is directly in event.data)
          stepId = rawData.step_id as string | undefined
          runFolder = rawData.run_folder as string | undefined
          workspacePath = rawData.workspace_path as string | undefined

          // If not found, try accessing through 'data' property (nested structure)
          if (!stepId && rawData.data && typeof rawData.data === 'object') {
            const nestedData = rawData.data as Record<string, unknown>
            stepId = nestedData.step_id as string | undefined
            if (!runFolder) {
              runFolder = nestedData.run_folder as string | undefined
            }
            if (!workspacePath) {
              workspacePath = nestedData.workspace_path as string | undefined
            }
          }
        }

        if (stepId) {
          setCurrentStepId(stepId)
          setStepStatusMap(prev => {
            const newMap = new Map(prev)
            newMap.set(stepId!, 'running')
            return newMap
          })
        } else {
          console.warn('[useWorkflowExecution] step_execution_start event missing step_id:', {
            rawData,
            dataType: typeof rawData,
            keys: rawData && typeof rawData === 'object' ? Object.keys(rawData) : []
          })
        }

        // Update selected run folder if provided in event
        if (runFolder && runFolder !== 'new') {
          setSelectedRunFolder(runFolder)
          
          // Reload run folders to ensure the folder is in the list
          if (workspacePath) {
            loadRunFolders(workspacePath).catch(err => {
              console.warn('[useWorkflowExecution] Failed to reload run folders:', err)
            })
            
            // Load progress for the selected folder
            loadProgress(workspacePath, runFolder).catch(err => {
              console.warn('[useWorkflowExecution] Failed to load progress:', err)
            })
          }
        }
      }

      // Handle step_finished event
      if (event.type === 'step_execution_end') {
        // Access step_id - event.data is the actual event data object
        const rawData = event.data as Record<string, unknown> | undefined
        let stepId: string | undefined

        if (rawData && typeof rawData === 'object') {
          // Try direct access first
          stepId = rawData.step_id as string | undefined

          // If not found, try accessing through 'data' property
          if (!stepId && rawData.data && typeof rawData.data === 'object') {
            const nestedData = rawData.data as Record<string, unknown>
            stepId = nestedData.step_id as string | undefined
          }
        }

        if (stepId) {
          setStepStatusMap(prev => {
            const newMap = new Map(prev)
            newMap.set(stepId!, 'completed')
            return newMap
          })
        } else {
          console.warn('[useWorkflowExecution] step_execution_end event missing step_id:', rawData)
        }
      }

      // Handle step_failed event
      if (event.type === 'step_execution_failed') {
        // Access step_id - event.data is the actual event data object
        const rawData = event.data as Record<string, unknown> | undefined
        let stepId: string | undefined
        let errorMessage: string | undefined

        if (rawData && typeof rawData === 'object') {
          // Try direct access first
          stepId = rawData.step_id as string | undefined
          errorMessage = rawData.error as string | undefined

          // If not found, try accessing through 'data' property
          if (!stepId && rawData.data && typeof rawData.data === 'object') {
            const nestedData = rawData.data as Record<string, unknown>
            stepId = nestedData.step_id as string | undefined
            errorMessage = nestedData.error as string | undefined
          }
        }

        if (stepId) {
          setStepStatusMap(prev => {
            const newMap = new Map(prev)
            newMap.set(stepId!, 'failed')
            return newMap
          })
          // Log error message for debugging
          if (errorMessage) {
            console.error(`[useWorkflowExecution] Step ${stepId} failed:`, errorMessage)
          }
        } else {
          console.warn('[useWorkflowExecution] step_execution_failed event missing step_id:', rawData)
        }
      }

      // Handle prerequisite_navigation event
      if (event.type === 'prerequisite_navigation') {
        const rawData = event.data as Record<string, unknown> | undefined
        let fromStepIndex: number | undefined
        let toStepIndex: number | undefined
        let reason: string | undefined
        let failureType: string | undefined

        if (rawData && typeof rawData === 'object') {
          // Try direct access first
          fromStepIndex = rawData.from_step_index as number | undefined
          toStepIndex = rawData.to_step_index as number | undefined
          reason = rawData.reason as string | undefined
          failureType = rawData.failure_type as string | undefined

          // If not found, try accessing through 'data' property
          if (fromStepIndex === undefined && rawData.data && typeof rawData.data === 'object') {
            const nestedData = rawData.data as Record<string, unknown>
            fromStepIndex = nestedData.from_step_index as number | undefined
            toStepIndex = nestedData.to_step_index as number | undefined
            if (!reason) {
              reason = nestedData.reason as string | undefined
            }
            if (!failureType) {
              failureType = nestedData.failure_type as string | undefined
            }
          }
        }

        if (fromStepIndex !== undefined && toStepIndex !== undefined) {
          console.log('[useWorkflowExecution] Prerequisite navigation detected:', {
            fromStep: fromStepIndex + 1,
            toStep: toStepIndex + 1,
            reason,
            failureType
          })
          
          // Update step status to show navigation
          // Mark from step as having prerequisite failure
          // The navigation will restart execution from toStepIndex
          // Note: The backend handles the actual navigation, this is just for UI feedback
        }
      }

    }

    lastProcessedEventIndexRef.current = events.length - 1
  }, [events, setSelectedRunFolder, loadRunFolders, loadProgress])

  // Start workflow - CRITICAL: Always use tab's observer ID, never fall back to global
  const startWorkflow = useCallback(async (presetQueryId: string) => {
    // Get active tab
    const activeTab = useChatStore.getState().getActiveTab()
    
    // CRITICAL: Always use tab's observer ID - never fall back to global
    const currentObserverId = activeTab?.observerId || null

    if (!currentObserverId) {
      console.error('[useWorkflowExecution] No observer ID available. Active tab should have an observer ID.')
      setError('No observer ID available. Please ensure you have an active tab.')
      return
    }

    console.log(`[useWorkflowExecution] Starting workflow with observerId: ${currentObserverId} (tab: ${activeTab?.tabId || 'unknown'})`)

    // Sync observer ID to API module
    if (activeTab) {
      setCurrentObserverId(currentObserverId)
    }

    setError(null)
    setManualStatus(null) // Clear any manual status
    lastProcessedEventIndexRef.current = events.length - 1 // Start processing from current position

    try {
      // Get active preset for LLM config
      const activePreset = getActivePreset('workflow')
      const filteredPresetTools = currentPresetTools?.filter(t => !t.endsWith(':*')) || []

      // Build request payload
      const requestPayload = {
        query: `Execute workflow for preset: ${presetQueryId}`,
        agent_mode: 'workflow' as const,
        enabled_tools: enabledTools.map(tool => tool.name),
        enabled_servers: effectiveServers,
        selected_tools: filteredPresetTools.length > 0 ? filteredPresetTools : undefined,
        provider: llmConfig.provider,
        model_id: llmConfig.model_id,
        llm_config: llmConfig,
        preset_query_id: presetQueryId,
        use_code_execution_mode: activePreset?.useCodeExecutionMode
      }

      // Start the query - agentApi.startQuery will use observerId from setCurrentObserverId
      const response = await agentApi.startQuery(requestPayload)

      if (response.status !== 'started' && response.status !== 'workflow_started') {
        throw new Error('Failed to start workflow')
      }

      // Update tab's session ID and streaming status if active tab exists
      if (activeTab && response.session_id) {
        updateTabSessionId(activeTab.tabId, response.session_id)
        setTabStreaming(activeTab.tabId, true)
        console.log(`[useWorkflowExecution] Updated tab ${activeTab.tabId} with sessionId: ${response.session_id}`)
      }

      console.log('[useWorkflowExecution] Workflow started successfully')
    } catch (err) {
      console.error('[useWorkflowExecution] Failed to start workflow:', err)
      setError(err instanceof Error ? err.message : 'Failed to start workflow')
      setManualStatus('failed')
      
      // Clear streaming status on error
      if (activeTab) {
        setTabStreaming(activeTab.tabId, false)
      }
    }
  }, [getActivePreset, currentPresetTools, enabledTools, effectiveServers, llmConfig, events.length, setTabStreaming, updateTabSessionId])

  // Run a specific step
  const runStep = useCallback(async (stepId: string, presetQueryId: string) => {
    // CRITICAL: Always use tab's observer ID - never fall back to global
    const activeTab = useChatStore.getState().getActiveTab()
    const currentObserverId = activeTab?.observerId || null

    if (!currentObserverId) {
      console.error('[useWorkflowExecution] No observer ID available. ChatArea should initialize it.')
      setError('No observer ID available. Please wait for initialization.')
      return
    }

    console.log(`[useWorkflowExecution] Running step ${stepId} with observerId: ${currentObserverId}`)

    setError(null)
    setManualStatus(null)
    setCurrentStepId(stepId)
    lastProcessedEventIndexRef.current = events.length - 1

    try {
      // Get active preset for LLM config
      const activePreset = getActivePreset('workflow')
      const filteredPresetTools = currentPresetTools?.filter(t => !t.endsWith(':*')) || []

      // Build request payload with step_id
      const requestPayload = {
        query: `Execute step ${stepId} for preset: ${presetQueryId}`,
        agent_mode: 'workflow' as const,
        enabled_tools: enabledTools.map(tool => tool.name),
        enabled_servers: effectiveServers,
        selected_tools: filteredPresetTools.length > 0 ? filteredPresetTools : undefined,
        provider: llmConfig.provider,
        model_id: llmConfig.model_id,
        llm_config: llmConfig,
        preset_query_id: presetQueryId,
        step_id: stepId,
        use_code_execution_mode: activePreset?.useCodeExecutionMode
      }

      // Start the query
      const response = await agentApi.startQuery(requestPayload)

      if (response.status !== 'started' && response.status !== 'workflow_started') {
        throw new Error('Failed to run step')
      }

      console.log('[useWorkflowExecution] Step started successfully')
    } catch (err) {
      console.error('[useWorkflowExecution] Failed to run step:', err)
      setError(err instanceof Error ? err.message : 'Failed to run step')
      setManualStatus('failed')
    }
  }, [getActivePreset, currentPresetTools, enabledTools, effectiveServers, llmConfig, events.length])

  // Pause workflow
  const pauseWorkflow = useCallback(async () => {
    setManualStatus('paused')
  }, [])

  // Stop workflow - finds execution phase tab or uses active tab
  const stopWorkflow = useCallback(async () => {
    const chatStore = useChatStore.getState()
    
    // Find execution phase tab (preferred) or use active tab
    // Filter workflow tabs by phase ID
    const allTabs = Object.values(chatStore.chatTabs)
    const executionTabs = allTabs.filter(tab => 
      tab.metadata?.mode === 'workflow' && tab.metadata?.phaseId === EXECUTION_PHASE_ID
    )
    const runningExecutionTab = executionTabs.find(tab => tab.isStreaming)
    const executionTab = runningExecutionTab || executionTabs[0]
    const activeTab = chatStore.getActiveTab()
    
    // Use execution tab if found, otherwise use active tab, otherwise fallback to global
    const targetTab = executionTab || activeTab
    
    // Stop ChatArea's polling (same logic as ChatArea.stopStreaming)
    const storeState = useChatStore.getState()
    const pollingInterval = storeState.pollingInterval
    if (pollingInterval) {
      clearInterval(pollingInterval)
      useChatStore.getState().setPollingInterval(null)
    }

    // Set streaming to false (this will update the button back to "Execute")
    useChatStore.getState().setIsStreaming(false)
    
    // Update tab's streaming status if target tab exists
    if (targetTab) {
      setTabStreaming(targetTab.tabId, false)
      console.log(`[useWorkflowExecution] Stopped streaming for tab ${targetTab.tabId}`)
    }

    // Reset event polling index so next workflow/chat starts fresh
    useChatStore.getState().setLastEventIndex(-1)
    useChatStore.getState().setLastEventCount(0)

    // Clear current step tracking
    setCurrentStepId(null)

    // Call backend to stop the session using session ID
    let sessionId: string | null = null
    if (targetTab?.sessionId) {
      // Use target tab's session ID
      sessionId = targetTab.sessionId
    } else {
      // Fallback to global session ID
      sessionId = getSessionId()
    }
    
    if (sessionId) {
      try {
        await agentApi.stopSession(sessionId)
        console.log(`[useWorkflowExecution] Session ${sessionId} stopped${targetTab ? ` (tab: ${targetTab.tabId})` : ' (global)'}`)
      } catch (err) {
        console.error('[useWorkflowExecution] Failed to stop session:', err)
      }
    } else {
      console.warn('[useWorkflowExecution] No session ID available to stop session')
    }
  }, [setTabStreaming])

  // Resume workflow
  const resumeWorkflow = useCallback(async () => {
    if (manualStatus === 'paused') {
      setManualStatus(null) // Clear manual status to let derived status take over
    }
  }, [manualStatus])

  // Clear events - delegates to useChatStore
  // CRITICAL: Clear tab-specific events, not global events
  const clearEvents = useCallback(() => {
    const activeTab = useChatStore.getState().getActiveTab()
    if (activeTab?.observerId) {
      const clearTabEvents = useChatStore.getState().clearTabEvents
      clearTabEvents(activeTab.observerId)
    } else {
      console.warn(`[useWorkflowExecution] No active tab - cannot clear events`)
    }
    lastProcessedEventIndexRef.current = -1
    setManualStatus(null)
  }, [])

  return {
    status: derivedStatus,
    currentStepId,
    stepStatusMap,
    events,
    observerId: observerId || '',
    error,
    startWorkflow,
    runStep,
    pauseWorkflow,
    stopWorkflow,
    resumeWorkflow,
    clearEvents
  }
}

export default useWorkflowExecution
