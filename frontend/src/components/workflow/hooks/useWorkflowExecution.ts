import { useState, useCallback, useMemo } from 'react'
import { agentApi, getSessionId } from '../../../services/api'
import type { PollingEvent, AgentQueryRequest } from '../../../services/api-types'
import { useLLMStore, useMCPStore, useChatStore } from '../../../stores'
import { usePresetApplication } from '../../../stores/useGlobalPresetStore'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'
import { useModeStore } from '../../../stores/useModeStore'
import { EXECUTION_PHASE_ID } from '../../../constants/workflow'
import { buildLLMConfigWithApiKeys } from '../../../utils/chatSubmitHelpers'

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
  events: PollingEvent[]
  sessionId: string
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
 * - sessionId: session identifier
 * - events: event stream
 * - isStreaming: whether agent is running (source of truth for 'running' status)
 * - isCompleted: whether execution completed (source of truth for 'completed' status)
 */
export function useWorkflowExecution(): UseWorkflowExecutionReturn {
  // Get active tab (for multi-tab support - works for both chat and workflow)
  const activeTab = useChatStore(state => state.getActiveTab())
  const selectedModeCategory = useModeStore(state => state.selectedModeCategory)
  
  // CRITICAL: Always use tab's session ID - never fall back to global to prevent mixing
  const tabSessionId = activeTab?.sessionId
  
  // Get tab-specific events and status
  const getTabEvents = useChatStore(state => state.getTabEvents)
  const getTabStreamingStatus = useChatStore(state => state.getTabStreamingStatus)
  
  // Use tab-specific data - never fall back to global
  const sessionId = tabSessionId || null
  const tabIsStreaming = activeTab ? getTabStreamingStatus(activeTab.tabId) : false
  const isStreaming = tabIsStreaming
  const isCompleted = activeTab?.isCompleted || false
  const events = useMemo(() => {
    return sessionId ? getTabEvents(sessionId) : []
  }, [sessionId, getTabEvents])
  
  // In workflow mode, it's normal to not have an active tab until a phase is started
  // Only warn in chat mode where tabs should always exist
  if (!activeTab && selectedModeCategory === 'chat') {
    console.warn(`[useWorkflowExecution] No active tab - this should not happen in chat mode`)
  }
  
  // Chat store actions
  const setTabStreaming = useChatStore(state => state.setTabStreaming)
  const updateTabSessionId = useChatStore(state => state.updateTabSessionId)

  // Local state for workflow-specific tracking
  const [error, setError] = useState<string | null>(null)
  const [manualStatus, setManualStatus] = useState<WorkflowExecutionStatus | null>(null)

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

  // NOTE: Step status events (step_progress_updated, step_execution_end, step_execution_failed)
  // are now handled in ChatArea.tsx polling layer which updates useWorkflowStore directly.
  // This is more reliable than useEffect here because the events array from useMemo
  // doesn't update when new events arrive (the getTabEvents function reference doesn't change).

  // Start workflow - CRITICAL: Always use tab's observer ID, never fall back to global
  const startWorkflow = useCallback(async (presetQueryId: string) => {
    // Get active tab
    const activeTab = useChatStore.getState().getActiveTab()
    
    // CRITICAL: Always use tab's session ID - never fall back to global
    const currentSessionId = activeTab?.sessionId || null

    if (!currentSessionId) {
      console.error('[useWorkflowExecution] No session ID available. Active tab should have a session ID.')
      setError('No session ID available. Please ensure you have an active tab.')
      return
    }

    console.log(`[useWorkflowExecution] Starting workflow with sessionId: ${currentSessionId} (tab: ${activeTab?.tabId || 'unknown'})`)

    setError(null)
    setManualStatus(null) // Clear any manual status

    // Clear previous events from the tab — a re-run should start fresh
    const chatStore = useChatStore.getState()
    chatStore.clearTabEvents(currentSessionId)
    chatStore.clearStreamingText(currentSessionId)

    try {
      // Get active preset for LLM config
      const activePreset = getActivePreset('workflow')
      const filteredPresetTools = currentPresetTools?.filter(t => !t.endsWith(':*')) || []

      // Auto-start camofox-browser if stealth mode is selected
      if (effectiveServers.includes('camofox')) {
        try {
          const headed = activePreset?.camofoxHeaded !== false // default true
          await agentApi.startCamofox(headed)
        } catch (err) {
          console.warn('[useWorkflowExecution] Failed to start camofox-browser:', err)
          // Don't block workflow — camofox-mcp will retry connection
        }
      }

      // Build request payload
      // CDP port: read from active tab config (set via ChatInput CDP toggle)
      const tabConfig = activeTab?.config
      const cdpPort = (tabConfig?.enableBrowserAccess && tabConfig?.useCdp && tabConfig?.cdpPort)
        ? tabConfig.cdpPort
        : undefined

      const llmConfigWithApiKeys = buildLLMConfigWithApiKeys(llmConfig)
      const requestPayload: AgentQueryRequest = {
        query: `Execute workflow for preset: ${presetQueryId}`,
        agent_mode: 'workflow' as const,
        enabled_tools: enabledTools.map(tool => tool.name),
        enabled_servers: effectiveServers,
        selected_tools: filteredPresetTools.length > 0 ? filteredPresetTools : undefined,
        provider: llmConfig.provider as AgentQueryRequest['provider'],
        model_id: llmConfig.model_id,
        llm_config: llmConfigWithApiKeys,
        preset_query_id: presetQueryId,
        use_code_execution_mode: activePreset?.useCodeExecutionMode,
        cdp_port: cdpPort
      }

      // Start the query - agentApi.startQuery will use sessionId
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
  }, [getActivePreset, currentPresetTools, enabledTools, effectiveServers, llmConfig, setTabStreaming, updateTabSessionId])

  // Run a specific step
  const runStep = useCallback(async (stepId: string, presetQueryId: string) => {
    // CRITICAL: Always use tab's session ID - never fall back to global
    const activeTab = useChatStore.getState().getActiveTab()
    const currentSessionId = activeTab?.sessionId || null

    if (!currentSessionId) {
      console.error('[useWorkflowExecution] No session ID available. ChatArea should initialize it.')
      setError('No session ID available. Please wait for initialization.')
      return
    }

    console.log(`[useWorkflowExecution] Running step ${stepId} with sessionId: ${currentSessionId}`)

    setError(null)
    setManualStatus(null)

    try {
      // Get active preset for LLM config
      const activePreset = getActivePreset('workflow')
      const filteredPresetTools = currentPresetTools?.filter(t => !t.endsWith(':*')) || []

      // Auto-start camofox-browser if stealth mode is selected
      if (effectiveServers.includes('camofox')) {
        try {
          const headed = activePreset?.camofoxHeaded !== false
          await agentApi.startCamofox(headed)
        } catch (err) {
          console.warn('[useWorkflowExecution] Failed to start camofox-browser for step:', err)
        }
      }

      // CDP port: read from active tab config (set via ChatInput CDP toggle)
      const stepTabConfig = useChatStore.getState().getActiveTab()?.config
      const stepCdpPort = (stepTabConfig?.enableBrowserAccess && stepTabConfig?.useCdp && stepTabConfig?.cdpPort)
        ? stepTabConfig.cdpPort
        : undefined

      // Build request payload with step_id
      const llmConfigWithApiKeys = buildLLMConfigWithApiKeys(llmConfig)
      const requestPayload = {
        query: `Execute step ${stepId} for preset: ${presetQueryId}`,
        agent_mode: 'workflow' as const,
        enabled_tools: enabledTools.map(tool => tool.name),
        enabled_servers: effectiveServers,
        selected_tools: filteredPresetTools.length > 0 ? filteredPresetTools : undefined,
        provider: llmConfig.provider as AgentQueryRequest['provider'],
        model_id: llmConfig.model_id,
        llm_config: llmConfigWithApiKeys,
        preset_query_id: presetQueryId,
        step_id: stepId,
        use_code_execution_mode: activePreset?.useCodeExecutionMode,
        cdp_port: stepCdpPort
      } as AgentQueryRequest & { step_id: string }

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
  }, [getActivePreset, currentPresetTools, enabledTools, effectiveServers, llmConfig])

  // Pause workflow
  const pauseWorkflow = useCallback(async () => {
    setManualStatus('paused')
  }, [])

  // Stop workflow - finds execution phase tab or uses active tab
  const stopWorkflow = useCallback(async () => {
    const chatStore = useChatStore.getState()
    const getTabStreamingStatus = chatStore.getTabStreamingStatus
    
    // Find execution phase tab for the CURRENT preset (preferred) or use active tab
    const activePreset = getActivePreset('workflow')
    const allTabs = Object.values(chatStore.chatTabs)
    const executionTabs = allTabs.filter(tab =>
      tab.metadata?.mode === 'workflow' &&
      tab.metadata?.phaseId === EXECUTION_PHASE_ID &&
      tab.metadata?.presetQueryId === activePreset?.id
    )
    
    // Use computed streaming status (not stored property) to find running tab
    const runningExecutionTab = executionTabs.find(tab => getTabStreamingStatus(tab.tabId))
    // If no running tab, use most recent execution tab, otherwise first one
    const executionTab = runningExecutionTab || 
      (executionTabs.length > 0 
        ? executionTabs.sort((a, b) => b.createdAt - a.createdAt)[0] 
        : null)
    const activeTab = chatStore.getActiveTab()
    
    // Use execution tab if found, otherwise use active tab, otherwise fallback to global
    const targetTab = executionTab || activeTab
    
    console.log('[useWorkflowExecution] Stop workflow:', {
      executionTabsCount: executionTabs.length,
      runningExecutionTab: runningExecutionTab?.tabId,
      executionTab: executionTab?.tabId,
      activeTab: activeTab?.tabId,
      targetTab: targetTab?.tabId,
      targetSessionId: targetTab?.sessionId
    })
    
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
    // Deprecated: setLastEventCount removed

    // Clear current step tracking in store
    useWorkflowStore.getState().setCurrentStepId(null)

    // Call backend to stop the session using session ID
    let sessionId: string | null = null
    if (targetTab?.sessionId) {
      // Use target tab's session ID (CRITICAL: this is the correct sessionId for the execution phase)
      sessionId = targetTab.sessionId
      console.log(`[useWorkflowExecution] Using execution tab sessionId: ${sessionId}`)
    } else {
      // Fallback to global session ID (shouldn't happen in normal flow)
      sessionId = getSessionId()
      console.warn(`[useWorkflowExecution] No target tab sessionId, falling back to global: ${sessionId}`)
    }
    
    if (sessionId) {
      try {
        await agentApi.stopSession(sessionId)
        console.log(`[useWorkflowExecution] ✅ Session ${sessionId} stopped via API${targetTab ? ` (tab: ${targetTab.tabId}, phase: ${targetTab.metadata?.phaseId})` : ' (global)'}`)
      } catch (err) {
        console.error('[useWorkflowExecution] ❌ Failed to stop session:', err)
      }
    } else {
      console.warn('[useWorkflowExecution] ⚠️ No session ID available to stop session')
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
    if (activeTab?.sessionId) {
      const clearTabEvents = useChatStore.getState().clearTabEvents
      clearTabEvents(activeTab.sessionId)
    } else {
      console.warn(`[useWorkflowExecution] No active tab - cannot clear events`)
    }
    setManualStatus(null)
  }, [])

  return {
    status: derivedStatus,
    events,
    sessionId: sessionId || '',
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
