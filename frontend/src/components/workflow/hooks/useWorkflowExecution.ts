import { useState, useCallback, useRef } from 'react'
import { agentApi } from '../../../services/api'
import type { PollingEvent } from '../../../services/api-types'
import { useAppStore, useLLMStore, useMCPStore } from '../../../stores'
import { usePresetApplication } from '../../../stores/useGlobalPresetStore'

export type WorkflowExecutionStatus = 
  | 'idle' 
  | 'running' 
  | 'paused' 
  | 'completed' 
  | 'failed'
  | 'waiting_feedback'

export interface UseWorkflowExecutionReturn {
  status: WorkflowExecutionStatus
  currentStepId: string | null
  events: PollingEvent[]
  observerId: string | null
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
 */
export function useWorkflowExecution(): UseWorkflowExecutionReturn {
  const [status, setStatus] = useState<WorkflowExecutionStatus>('idle')
  const [currentStepId, setCurrentStepId] = useState<string | null>(null)
  const [events, setEvents] = useState<PollingEvent[]>([])
  const [observerId, setObserverId] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  
  const pollingIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const lastEventIndexRef = useRef<number>(-1)

  // Store subscriptions - note: agentMode can be used if needed for validation
  const { } = useAppStore()
  const { primaryConfig: llmConfig } = useLLMStore()
  const { toolList: allTools, selectedServers } = useMCPStore()
  const { currentPresetServers, currentPresetTools, getActivePreset } = usePresetApplication()

  // Get effective servers
  const effectiveServers = currentPresetServers.length > 0 ? currentPresetServers : selectedServers

  // Filter tools to only include those from effective servers
  const enabledTools = allTools.filter(tool => 
    tool.server && effectiveServers.includes(tool.server)
  )

  // Stop polling
  const stopPolling = useCallback(() => {
    if (pollingIntervalRef.current) {
      clearInterval(pollingIntervalRef.current)
      pollingIntervalRef.current = null
    }
  }, [])

  // Poll for events
  const pollEvents = useCallback(async (obsId: string) => {
    try {
      const response = await agentApi.getEvents(obsId, lastEventIndexRef.current)
      
      if (response.events.length > 0) {
        lastEventIndexRef.current = response.last_event_index
        
        // Add new events
        setEvents(prev => [...prev, ...response.events])
        
        // Check for completion events
        const hasCompletion = response.events.some(event => 
          event.type === 'workflow_end' ||
          event.type === 'agent_end' ||
          event.type === 'conversation_end' ||
          event.type === 'conversation_error' ||
          event.type === 'agent_error'
        )
        
        if (hasCompletion) {
          stopPolling()
          setStatus('completed')
        }
        
        // Check for human feedback request
        const hasFeedbackRequest = response.events.some(event => 
          event.type === 'request_human_feedback'
        )
        
        if (hasFeedbackRequest) {
          stopPolling()
          setStatus('waiting_feedback')
        }
        
        // Track current step from events
        const stepEvents = response.events.filter(event => 
          event.type === 'orchestrator_step_start' ||
          event.type === 'step_start'
        )
        if (stepEvents.length > 0) {
          const lastStepEvent = stepEvents[stepEvents.length - 1]
          const stepId = (lastStepEvent.data as { step_id?: string })?.step_id
          if (stepId) {
            setCurrentStepId(stepId)
          }
        }
      }
    } catch (err) {
      console.error('[useWorkflowExecution] Polling error:', err)
    }
  }, [stopPolling])

  // Start polling
  const startPolling = useCallback((obsId: string) => {
    stopPolling() // Clear any existing interval
    pollingIntervalRef.current = setInterval(() => pollEvents(obsId), 1000)
  }, [stopPolling, pollEvents])

  // Register observer and start workflow
  const startWorkflow = useCallback(async (presetQueryId: string) => {
    setError(null)
    setStatus('running')
    setEvents([])
    lastEventIndexRef.current = -1

    try {
      // Register observer if not already registered
      let obsId = observerId
      if (!obsId) {
        const registerResponse = await agentApi.registerObserver()
        obsId = registerResponse.observer_id
        setObserverId(obsId)
      }

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

      // Start the query
      const response = await agentApi.startQuery(requestPayload)

      if (response.status === 'started' || response.status === 'workflow_started') {
        // Start polling for events
        if (obsId) {
          startPolling(obsId)
        }
      } else {
        throw new Error('Failed to start workflow')
      }
    } catch (err) {
      console.error('[useWorkflowExecution] Failed to start workflow:', err)
      setError(err instanceof Error ? err.message : 'Failed to start workflow')
      setStatus('failed')
    }
  }, [observerId, getActivePreset, currentPresetTools, enabledTools, effectiveServers, llmConfig, startPolling])

  // Run a specific step
  const runStep = useCallback(async (stepId: string, presetQueryId: string) => {
    setError(null)
    setStatus('running')
    setCurrentStepId(stepId)

    try {
      // Register observer if not already registered
      let obsId = observerId
      if (!obsId) {
        const registerResponse = await agentApi.registerObserver()
        obsId = registerResponse.observer_id
        setObserverId(obsId)
      }

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
        step_id: stepId, // Specific step to run
        use_code_execution_mode: activePreset?.useCodeExecutionMode
      }

      // Start the query
      const response = await agentApi.startQuery(requestPayload)

      if (response.status === 'started' || response.status === 'workflow_started') {
        // Start polling for events
        if (obsId) {
          startPolling(obsId)
        }
      } else {
        throw new Error('Failed to run step')
      }
    } catch (err) {
      console.error('[useWorkflowExecution] Failed to run step:', err)
      setError(err instanceof Error ? err.message : 'Failed to run step')
      setStatus('failed')
    }
  }, [observerId, getActivePreset, currentPresetTools, enabledTools, effectiveServers, llmConfig, startPolling])

  // Pause workflow
  const pauseWorkflow = useCallback(async () => {
    stopPolling()
    setStatus('paused')
    // TODO: Call backend pause API if available
  }, [stopPolling])

  // Stop workflow
  const stopWorkflow = useCallback(async () => {
    stopPolling()
    setStatus('idle')
    setCurrentStepId(null)
    
    // Call backend to stop the session
    if (observerId) {
      try {
        await agentApi.stopSession(observerId)
      } catch (err) {
        console.error('[useWorkflowExecution] Failed to stop session:', err)
      }
    }
  }, [stopPolling, observerId])

  // Resume workflow
  const resumeWorkflow = useCallback(async () => {
    if (observerId && status === 'paused') {
      setStatus('running')
      startPolling(observerId)
    }
  }, [observerId, status, startPolling])

  // Clear events
  const clearEvents = useCallback(() => {
    setEvents([])
    lastEventIndexRef.current = -1
  }, [])

  return {
    status,
    currentStepId,
    events,
    observerId,
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

