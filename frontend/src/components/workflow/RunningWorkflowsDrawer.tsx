import React, { useEffect, useState, useMemo, useRef } from 'react'
import { X, Loader2, RefreshCw, Layers, Eye, ChevronDown, ChevronUp } from 'lucide-react'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import { useRunningWorkflowsStore, useRunningWorkflows, useShowRunningDrawer, type RunningWorkflow } from '../../stores/useRunningWorkflowsStore'
import { useChatStore } from '../../stores/useChatStore'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { cn } from '@/lib/utils'
import { extractWorkflowInfo } from '../../utils/workflowEventProcessor'

// Unified workflow item for display
interface WorkflowItem {
  id: string
  presetId: string
  presetName: string
  phaseName?: string
  sessionId?: string
  status: 'running' | 'completed' | 'failed' | 'paused'
  progress?: {
    completed_step_indices?: number[]
    total_steps: number
  }
  currentStepTitle?: string
  currentStepId?: string      // From step_execution_start events
  currentStepIndex?: number    // From step_execution_start events
  currentAgentName?: string  // From orchestrator metadata
  orchestratorPhase?: string // From orchestrator metadata
  agentTurns?: number        // From agent_end events
  contextTokens?: number     // From agent_end events
  // Tool call info from tool_call_end events
  lastToolName?: string
  lastToolServerName?: string
  lastToolTurn?: number
  contextUsagePercent?: number
  inputTokens?: number     // From tool_call_end events
  totalTokens?: number      // From tool_call_end events
  modelId?: string          // From tool_call_end events
  finalResult?: string      // From last unified_completion event
  timestamp: number
  lastEventTime?: number    // Timestamp of the last event received (ms)
  source: 'tracked' | 'active-tab'
}

interface RunningWorkflowsDrawerProps {
  onRestoreWorkflow?: (workflow: RunningWorkflow) => void
}

// Format timestamp to relative time
const formatTimestamp = (timestamp: number): string => {
  const now = Date.now()
  const diff = now - timestamp
  const minutes = Math.floor(diff / 60000)
  const hours = Math.floor(minutes / 60)
  const days = Math.floor(hours / 24)

  if (days > 0) return `${days}d ago`
  if (hours > 0) return `${hours}h ago`
  if (minutes > 0) return `${minutes}m ago`
  return 'Just now'
}

/**
 * Drawer component showing all running workflows with status and actions.
 * Combines tracked workflows with active workflow tabs.
 */
export const RunningWorkflowsDrawer: React.FC<RunningWorkflowsDrawerProps> = ({
  onRestoreWorkflow
}) => {
  const runningWorkflows = useRunningWorkflows()
  const showRunningDrawer = useShowRunningDrawer()
  const {
    setShowRunningDrawer,
    refreshRunningWorkflowStatuses,
    validateRunningWorkflows
  } = useRunningWorkflowsStore()
  const stepProgress = useWorkflowStore(state => state.stepProgress)

  const chatTabs = useChatStore(state => state.chatTabs)
  const tabEvents = useChatStore(state => state.tabEvents)
  const getTabStreamingStatus = useChatStore(state => state.getTabStreamingStatus)
  const activePresetId = useGlobalPresetStore(state => state.activePresetIds.workflow)
  const { customPresets, predefinedPresets } = useGlobalPresetStore()

  const [isRefreshing, setIsRefreshing] = useState(false)
  const [expandedResults, setExpandedResults] = useState<Set<string>>(new Set())
  const isSwitchingRef = useRef(false)

  // Helper to extract progress and orchestrator info from events for a session
  const getInfoFromEvents = (sessionId: string | undefined) => {
    if (!sessionId) {
      return {
        progress: undefined,
        stepTitle: undefined,
        agentName: undefined,
        orchestratorPhase: undefined,
        agentTurns: undefined,
        contextTokens: undefined,
        lastToolName: undefined,
        lastToolServerName: undefined,
        lastToolTurn: undefined,
        contextUsagePercent: undefined,
        inputTokens: undefined,
        totalTokens: undefined,
        modelId: undefined,
        lastEventTime: undefined
      }
    }

    const events = tabEvents[sessionId] || []
    const info = extractWorkflowInfo(events)
    
    // Get the last event timestamp
    let lastEventTime: number | undefined = undefined
    if (events.length > 0) {
      // Events are typically sorted, so get the last one
      const lastEvent = events[events.length - 1]
      // PollingEvent has timestamp at top level (from PollingEventSchema)
      const eventTimestamp = lastEvent?.timestamp
      if (eventTimestamp) {
        // Convert ISO string to timestamp
        if (typeof eventTimestamp === 'string') {
          lastEventTime = new Date(eventTimestamp).getTime()
        } else if (typeof eventTimestamp === 'number') {
          lastEventTime = eventTimestamp
        }
      }
    }
    
    return {
      ...info,
      lastEventTime
    }
  }

  // Combine tracked workflows with active workflow tabs
  const allWorkflows = useMemo(() => {
    const items: WorkflowItem[] = []
    const seenSessionIds = new Set<string>()

    // First, add tracked workflows
    runningWorkflows.forEach(wf => {
      // For tracked workflows, also check events for latest info
      let progress = wf.progress
      let stepTitle = wf.currentStepTitle
      let currentStepId: string | undefined = undefined
      let currentStepIndex: number | undefined = undefined
      let agentName: string | undefined = undefined
      let orchestratorPhase: string | undefined = undefined
      let agentTurns: number | undefined = undefined
      let contextTokens: number | undefined = undefined
      let lastToolName: string | undefined = undefined
      let lastToolServerName: string | undefined = undefined
      let lastToolTurn: number | undefined = undefined
      let contextUsagePercent: number | undefined = undefined
      let inputTokens: number | undefined = undefined
      let totalTokens: number | undefined = undefined
      let modelId: string | undefined = undefined
      let finalResult: string | undefined = undefined

      if (wf.sessionId) {
        const fromEvents = getInfoFromEvents(wf.sessionId)
        if (!progress && fromEvents.progress) {
          progress = {
            completed_step_indices: fromEvents.progress.completed_step_indices || [],
            total_steps: fromEvents.progress.total_steps,
            last_updated: new Date().toISOString()
          }
        }
        if (!stepTitle && fromEvents.stepTitle) stepTitle = fromEvents.stepTitle
        if (fromEvents.currentStepId) currentStepId = fromEvents.currentStepId
        if (fromEvents.currentStepIndex !== undefined) currentStepIndex = fromEvents.currentStepIndex
        agentName = fromEvents.agentName
        orchestratorPhase = fromEvents.orchestratorPhase
        agentTurns = fromEvents.agentTurns
        contextTokens = fromEvents.contextTokens
        lastToolName = fromEvents.lastToolName
        lastToolServerName = fromEvents.lastToolServerName
        lastToolTurn = fromEvents.lastToolTurn
        contextUsagePercent = fromEvents.contextUsagePercent
        inputTokens = fromEvents.inputTokens
        totalTokens = fromEvents.totalTokens
        modelId = fromEvents.modelId
        finalResult = fromEvents.finalResult
      }
      
      // Get last event time
      let lastEventTime: number | undefined = undefined
      if (wf.sessionId) {
        const fromEvents = getInfoFromEvents(wf.sessionId)
        lastEventTime = fromEvents.lastEventTime
      }

      items.push({
        id: wf.id,
        presetId: wf.presetId,
        presetName: wf.presetName,
        phaseName: wf.phaseName,
        sessionId: wf.sessionId,
        status: wf.status,
        progress,
        currentStepTitle: stepTitle,
        currentStepId,
        currentStepIndex,
        currentAgentName: agentName,
        orchestratorPhase: orchestratorPhase,
        agentTurns,
        contextTokens,
        lastToolName,
        lastToolServerName,
        lastToolTurn,
        contextUsagePercent,
        inputTokens,
        totalTokens,
        modelId,
        finalResult,
        timestamp: wf.minimizedAt,
        lastEventTime,
        source: 'tracked'
      })
      if (wf.sessionId) seenSessionIds.add(wf.sessionId)
    })

    // Then, add active workflow tabs that aren't already in tracked list
    Object.values(chatTabs).forEach(tab => {
      if (tab.metadata?.mode !== 'workflow') return
      if (tab.sessionId && seenSessionIds.has(tab.sessionId)) return

      const presetId = tab.metadata?.presetQueryId
      if (!presetId) return

      const isStreaming = getTabStreamingStatus(tab.tabId)
      if (!isStreaming) return // Only show actively running tabs

      const preset = customPresets.find(p => p.id === presetId) || predefinedPresets.find(p => p.id === presetId)
      const presetName = preset?.label || tab.name || 'Workflow'

      // Get progress and agent info from events or global stepProgress for current preset
      let progress: WorkflowItem['progress'] = undefined
      let stepTitle: string | undefined = undefined
      let currentStepId: string | undefined = undefined
      let currentStepIndex: number | undefined = undefined
      let agentName: string | undefined = undefined
      let orchestratorPhase: string | undefined = undefined
      let agentTurns: number | undefined = undefined
      let contextTokens: number | undefined = undefined
      let lastToolName: string | undefined = undefined
      let lastToolServerName: string | undefined = undefined
      let lastToolTurn: number | undefined = undefined
      let contextUsagePercent: number | undefined = undefined
      let inputTokens: number | undefined = undefined
      let totalTokens: number | undefined = undefined
      let modelId: string | undefined = undefined
      let finalResult: string | undefined = undefined

      if (presetId === activePresetId && stepProgress) {
        // Use global stepProgress for current preset
        progress = stepProgress
        stepTitle = `Step ${(stepProgress.completed_step_indices?.length || 0) + 1}`
      }

      // Always try to get agent info from events
      if (tab.sessionId) {
        const fromEvents = getInfoFromEvents(tab.sessionId)
        if (!progress && fromEvents.progress) progress = fromEvents.progress
        if (!stepTitle && fromEvents.stepTitle) stepTitle = fromEvents.stepTitle
        if (fromEvents.currentStepId) currentStepId = fromEvents.currentStepId
        if (fromEvents.currentStepIndex !== undefined) currentStepIndex = fromEvents.currentStepIndex
        agentName = fromEvents.agentName
        orchestratorPhase = fromEvents.orchestratorPhase
        agentTurns = fromEvents.agentTurns
        contextTokens = fromEvents.contextTokens
        lastToolName = fromEvents.lastToolName
        lastToolServerName = fromEvents.lastToolServerName
        lastToolTurn = fromEvents.lastToolTurn
        contextUsagePercent = fromEvents.contextUsagePercent
        inputTokens = fromEvents.inputTokens
        totalTokens = fromEvents.totalTokens
        modelId = fromEvents.modelId
        finalResult = fromEvents.finalResult
      }
      
      // Get last event time
      let lastEventTime: number | undefined = undefined
      if (tab.sessionId) {
        const fromEvents = getInfoFromEvents(tab.sessionId)
        lastEventTime = fromEvents.lastEventTime
      }

      items.push({
        id: `tab-${tab.tabId}`,
        presetId: presetId,
        presetName: presetName,
        phaseName: tab.metadata?.phaseName || tab.name,
        sessionId: tab.sessionId || undefined,
        status: 'running',
        progress,
        currentStepTitle: stepTitle,
        currentStepId,
        currentStepIndex,
        currentAgentName: agentName,
        orchestratorPhase: orchestratorPhase,
        agentTurns,
        contextTokens,
        lastToolName,
        lastToolServerName,
        lastToolTurn,
        contextUsagePercent,
        inputTokens,
        totalTokens,
        modelId,
        finalResult,
        timestamp: tab.createdAt,
        lastEventTime,
        source: 'active-tab'
      })
    })

    // Sort workflows: stale ones (no event for >10 minutes) go to bottom, then by timestamp (newest first)
    const STALE_THRESHOLD_MS = 10 * 60 * 1000 // 10 minutes
    const now = Date.now()
    
    return items.sort((a, b) => {
      const aIsStale = a.lastEventTime ? (now - a.lastEventTime) > STALE_THRESHOLD_MS : true
      const bIsStale = b.lastEventTime ? (now - b.lastEventTime) > STALE_THRESHOLD_MS : true
      
      // If both are stale or both are active, sort by timestamp (newest first)
      if (aIsStale === bIsStale) {
        return b.timestamp - a.timestamp
      }
      
      // Stale workflows go to bottom
      return aIsStale ? 1 : -1
    })
  }, [runningWorkflows, chatTabs, tabEvents, getTabStreamingStatus, customPresets, predefinedPresets, activePresetId, stepProgress])

  // Get set of session IDs that are already active in tabs
  const activeSessionIds = useMemo(() => {
    const ids = new Set<string>()
    Object.values(chatTabs).forEach(tab => {
      if (tab.sessionId && tab.metadata?.mode === 'workflow') {
        ids.add(tab.sessionId)
      }
    })
    return ids
  }, [chatTabs])

  const handleRefresh = async () => {
    setIsRefreshing(true)
    try {
      await validateRunningWorkflows()
      refreshRunningWorkflowStatuses()
    } finally {
      setTimeout(() => setIsRefreshing(false), 500)
    }
  }

  // Validate and refresh statuses when drawer opens
  useEffect(() => {
    if (showRunningDrawer) {
      validateRunningWorkflows().then(() => {
        refreshRunningWorkflowStatuses()
      })
    }
  }, [showRunningDrawer, validateRunningWorkflows, refreshRunningWorkflowStatuses])

  // Switch to view a workflow
  const handleSwitchTo = async (workflow: WorkflowItem) => {
    // Prevent multiple simultaneous switches
    if (isSwitchingRef.current) {
      console.log('[WorkflowsDrawer] Switch already in progress, ignoring')
      return
    }

    // Close the drawer immediately (non-blocking UI update)
    setShowRunningDrawer(false)

    // Mark as switching
    isSwitchingRef.current = true

    // Defer all heavy operations to avoid blocking the UI
    // Use requestIdleCallback if available, otherwise setTimeout with 0
    const defer = (fn: () => void | Promise<void>) => {
      const wrappedFn = async () => {
        try {
          await fn()
        } catch (error) {
          console.error('[WorkflowsDrawer] Error in deferred operation:', error)
        } finally {
          // Clear switching flag when done
          isSwitchingRef.current = false
        }
      }

      if (typeof requestIdleCallback !== 'undefined') {
        requestIdleCallback(() => {
          wrappedFn().catch(error => {
            console.error('[WorkflowsDrawer] Error in deferred callback:', error)
            isSwitchingRef.current = false
          })
        }, { timeout: 100 })
      } else {
        setTimeout(() => {
          wrappedFn().catch(error => {
            console.error('[WorkflowsDrawer] Error in deferred callback:', error)
            isSwitchingRef.current = false
          })
        }, 0)
      }
    }

    // Capture values at call time to avoid stale closures
    const workflowPresetId = workflow.presetId
    const workflowSessionId = workflow.sessionId
    const workflowId = workflow.id
    const workflowSource = workflow.source
    const currentActivePresetId = activePresetId
    const currentCustomPresets = customPresets
    const currentPredefinedPresets = predefinedPresets
    const currentRunningWorkflows = runningWorkflows
    const currentOnRestoreWorkflow = onRestoreWorkflow

    // For tracked workflows, call the restore handler which will load latest iteration and restore groups
    if (currentOnRestoreWorkflow && workflowSource === 'tracked') {
      const trackedWorkflow = currentRunningWorkflows.find(wf => wf.id === workflowId)
      if (trackedWorkflow) {
        defer(async () => {
          // Switch to the preset first (deferred to avoid blocking)
          if (workflowPresetId !== currentActivePresetId) {
            useGlobalPresetStore.getState().applyPreset(workflowPresetId, 'workflow')
            // Small delay to let preset switch settle
            await new Promise(resolve => setTimeout(resolve, 50))
          }

          // Set the restoring flag to prevent auto-minimize
          useRunningWorkflowsStore.getState().setIsRestoringWorkflow(true)

          try {
            await currentOnRestoreWorkflow(trackedWorkflow)
          } finally {
            // Clear the restoring flag after restore completes
            useRunningWorkflowsStore.getState().setIsRestoringWorkflow(false)
          }
        })
        return
      }
    }

    // For active tabs, defer all operations
    defer(async () => {
      // Switch to the preset (deferred to avoid blocking)
      if (workflowPresetId !== currentActivePresetId) {
        useGlobalPresetStore.getState().applyPreset(workflowPresetId, 'workflow')
        // Small delay to let preset switch settle
        await new Promise(resolve => setTimeout(resolve, 50))
      }

      // Get workspace path from preset (use captured values)
      const preset = currentCustomPresets.find(p => p.id === workflowPresetId) || currentPredefinedPresets.find(p => p.id === workflowPresetId)
      const workspacePath = preset?.selectedFolder?.filepath

      // Load run folders if workspace path is available (non-blocking)
      if (workspacePath) {
        const { loadRunFolders, setSelectedRunFolder } = useWorkflowStore.getState()

        // Don't await - let it run in background
        loadRunFolders(workspacePath).then(() => {
          const folders = useWorkflowStore.getState().runFolders
          if (folders.length > 0) {
            // Extract just the iteration folder (without group) from the latest folder
            // API returns folders like "iteration-3/xspaces", we need just "iteration-3"
            const latestFolder = folders[0]
            const iterationFolder = latestFolder.name.includes('/')
              ? latestFolder.name.split('/')[0]
              : latestFolder.name

            setSelectedRunFolder(iterationFolder)
          }
        }).catch(error => {
          console.error('[WorkflowsDrawer] Failed to load run folders:', error)
        })
      }

      // Switch to tab and show chat area (deferred)
      const currentChatTabs = useChatStore.getState().chatTabs
      const existingTab = Object.values(currentChatTabs).find(
        tab => (workflowSessionId && tab.sessionId === workflowSessionId) ||
               (tab.metadata?.presetQueryId === workflowPresetId && tab.metadata?.mode === 'workflow')
      )

      if (existingTab) {
        useChatStore.getState().switchTab(existingTab.tabId)
      }

      // Show the chat area so user can see the logs
      useWorkflowStore.getState().setShowChatArea(true)
    })
  }

  if (!showRunningDrawer) return null

  const runningCount = allWorkflows.length

  return (
    <>
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-background/80 backdrop-blur-sm z-[50]"
        onClick={() => setShowRunningDrawer(false)}
      />

      {/* Drawer */}
      <div className="absolute right-0 top-0 bottom-0 w-[450px] max-w-[90vw] bg-background border-l border-border shadow-2xl z-[51] flex flex-col animate-in slide-in-from-right duration-200">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border bg-muted/50 flex-shrink-0">
          <div className="flex items-center gap-2">
            <Layers className="w-5 h-5 text-primary" />
            <span className="font-semibold text-foreground">
              Workflows
            </span>
            {runningCount > 0 && (
              <span className="px-2 py-0.5 text-xs font-medium bg-blue-500/10 text-blue-500 rounded-full">
                {runningCount}
              </span>
            )}
          </div>

          <div className="flex items-center gap-1">
            <button
              onClick={handleRefresh}
              disabled={isRefreshing}
              className="p-2 rounded-md hover:bg-accent transition-colors disabled:opacity-50"
              title="Refresh statuses"
            >
              <RefreshCw className={cn("w-4 h-4 text-muted-foreground", isRefreshing && "animate-spin")} />
            </button>

            <button
              onClick={() => setShowRunningDrawer(false)}
              className="p-2 rounded-md hover:bg-accent transition-colors"
              title="Close"
            >
              <X className="w-5 h-5 text-muted-foreground" />
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto p-3">
          {allWorkflows.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-full text-center px-6">
              <div className="w-16 h-16 rounded-full bg-muted flex items-center justify-center mb-4">
                <Layers className="w-8 h-8 text-muted-foreground" />
              </div>
              <p className="text-sm font-medium text-foreground">
                No workflows
              </p>
              <p className="text-xs text-muted-foreground mt-1 max-w-[240px]">
                Start a workflow execution to see it here.
              </p>
            </div>
          ) : (
            <div className="space-y-2">
              {allWorkflows.map((workflow) => {
                const isAlreadyActive = workflow.sessionId ? activeSessionIds.has(workflow.sessionId) : false
                const isCurrentPreset = workflow.presetId === activePresetId

                // Calculate progress info
                const completedSteps = workflow.progress?.completed_step_indices?.length || 0
                const totalSteps = workflow.progress?.total_steps || 0
                const stepsRemaining = totalSteps - completedSteps
                const currentStep = completedSteps + 1
                const progressPercent = totalSteps > 0 ? Math.round((completedSteps / totalSteps) * 100) : 0

                // Check if workflow is stale (no event for >10 minutes)
                const STALE_THRESHOLD_MS = 10 * 60 * 1000 // 10 minutes
                const now = Date.now()
                const isStale = workflow.lastEventTime ? (now - workflow.lastEventTime) > STALE_THRESHOLD_MS : true

                return (
                <div
                  key={workflow.id}
                  onClick={() => handleSwitchTo(workflow)}
                  className={cn(
                    "p-3 rounded-lg border cursor-pointer transition-all",
                    "hover:shadow-md hover:border-primary/50",
                    isStale
                      ? 'border-red-500/50 bg-red-500/5'
                      : isCurrentPreset
                        ? 'border-primary/50 bg-primary/5'
                        : isAlreadyActive
                          ? 'border-green-500/30 bg-green-500/5'
                          : 'border-border bg-card hover:bg-accent/50'
                  )}
                >
                  {/* Header: Name + Status */}
                  <div className="flex items-start gap-2">
                    <div className="flex-1 min-w-0">
                      <h3 className="font-medium text-sm text-card-foreground truncate">
                        {workflow.presetName}
                      </h3>
                      {workflow.phaseName && (
                        <p className="text-xs text-muted-foreground truncate mt-0.5">
                          {workflow.phaseName}
                        </p>
                      )}
                    </div>

                    {/* Status badge removed - just show events */}
                  </div>

                  {/* Current Step Name - prominently displayed */}
                  {(workflow.currentStepTitle || workflow.currentStepId || workflow.currentStepIndex !== undefined) && (
                    <div className="mt-2 px-2 py-1.5 bg-blue-500/5 border border-blue-500/20 rounded-md">
                      <div className="flex items-center gap-2">
                        <Loader2 className="w-3 h-3 text-blue-500 animate-spin flex-shrink-0" />
                        <div className="flex-1 min-w-0">
                          {workflow.currentStepTitle && (
                            <span className="text-xs font-medium text-blue-600 dark:text-blue-400 break-words">
                              {workflow.currentStepTitle}
                            </span>
                          )}
                          {(workflow.currentStepId || workflow.currentStepIndex !== undefined || workflow.currentAgentName) && (
                            <div className="text-xs text-muted-foreground/70 mt-0.5">
                              {workflow.currentAgentName && (
                                <div className="font-mono text-muted-foreground/80 break-words mb-0.5" title="Agent Name">
                                  {workflow.currentAgentName}
                                </div>
                              )}
                              {workflow.currentStepId && (
                                <span className="font-mono" title="Step ID">
                                  ID: {workflow.currentStepId}
                                </span>
                              )}
                              {workflow.currentStepId && workflow.currentStepIndex !== undefined && (
                                <span className="mx-1">•</span>
                              )}
                              {workflow.currentStepIndex !== undefined && (
                                <span className="font-mono" title="Step Index">
                                  Index: {workflow.currentStepIndex}
                                </span>
                              )}
                            </div>
                          )}
                        </div>
                      </div>
                      {/* Tool call info - organized in multiple lines */}
                      {(workflow.lastToolName || workflow.lastToolServerName || workflow.lastToolTurn !== undefined || workflow.contextUsagePercent !== undefined || (workflow.inputTokens !== undefined && workflow.totalTokens !== undefined) || workflow.modelId) && (
                        <div className="mt-1.5 space-y-1.5">
                          {/* Line 1: Tool name and server */}
                          {(workflow.lastToolName || workflow.lastToolServerName) && (
                            <div className="flex items-center gap-2 text-xs text-muted-foreground">
                              {workflow.lastToolName && (
                                <span className="font-mono text-muted-foreground/80 break-words" title={workflow.lastToolName}>
                                  {workflow.lastToolName}
                                </span>
                              )}
                              {workflow.lastToolServerName && (
                                <span className="text-muted-foreground/60" title={`Server: ${workflow.lastToolServerName}`}>
                                  @{workflow.lastToolServerName}
                                </span>
                              )}
                            </div>
                          )}
                          {/* Line 2: Turn, context usage, and tokens on same line */}
                          {(workflow.lastToolTurn !== undefined || workflow.contextUsagePercent !== undefined || (workflow.inputTokens !== undefined && workflow.totalTokens !== undefined)) && (
                            <div className="flex items-center gap-3 text-xs text-muted-foreground">
                              {workflow.lastToolTurn !== undefined && (
                                <span title="Turn" className="text-muted-foreground/70">
                                  T{workflow.lastToolTurn}
                                </span>
                              )}
                              {workflow.contextUsagePercent !== undefined && (
                                <span title="Context usage" className="text-muted-foreground/70">
                                  {Math.round(workflow.contextUsagePercent)}%
                                </span>
                              )}
                              {workflow.inputTokens !== undefined && workflow.totalTokens !== undefined && (
                                <span title="Input tokens / Total context window" className="text-muted-foreground/70">
                                  {workflow.inputTokens.toLocaleString()} / {workflow.totalTokens.toLocaleString()}
                                </span>
                              )}
                            </div>
                          )}
                          {/* Line 4: Model name */}
                          {workflow.modelId && (
                            <div className="text-xs text-muted-foreground/70">
                              <span title="Model" className="font-mono">
                                {workflow.modelId}
                              </span>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  )}

                  {/* Step Progress */}
                  {workflow.progress && totalSteps > 0 && (
                    <div className="mt-2">
                      {/* Progress bar */}
                      <div className="h-1.5 bg-muted rounded-full overflow-hidden">
                        <div
                          className="h-full rounded-full transition-all bg-blue-500"
                          style={{ width: `${progressPercent}%` }}
                        />
                      </div>

                      {/* Progress text - show step info and remaining */}
                      <div className="flex items-center justify-between text-xs text-muted-foreground mt-1.5">
                        <span className="font-medium">
                          Step {currentStep} of {totalSteps}
                        </span>
                        <span className="font-medium text-blue-600 dark:text-blue-400">
                          {stepsRemaining > 0 ? `${stepsRemaining} remaining` : `${progressPercent}%`}
                        </span>
                      </div>
                    </div>
                  )}

                  {/* Final Result - Collapsible */}
                  {workflow.finalResult && (
                    <div className="mt-2">
                      <button
                        onClick={(e) => {
                          e.stopPropagation()
                          setExpandedResults(prev => {
                            const newSet = new Set(prev)
                            if (newSet.has(workflow.id)) {
                              newSet.delete(workflow.id)
                            } else {
                              newSet.add(workflow.id)
                            }
                            return newSet
                          })
                        }}
                        className="flex items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground transition-colors w-full"
                      >
                        {expandedResults.has(workflow.id) ? (
                          <ChevronUp className="w-3 h-3" />
                        ) : (
                          <ChevronDown className="w-3 h-3" />
                        )}
                        <span>Final Result</span>
                      </button>
                      {expandedResults.has(workflow.id) && (
                        <div className="mt-1.5 p-2 bg-muted/50 rounded-md border border-border/50 text-xs text-muted-foreground whitespace-pre-wrap break-words max-h-48 overflow-y-auto">
                          {workflow.finalResult}
                        </div>
                      )}
                    </div>
                  )}

                  {/* Footer: Time + Action hint */}
                  <div className="flex items-center justify-between mt-3 pt-2 border-t border-border/50 text-xs">
                    <span className="text-muted-foreground">
                      {formatTimestamp(workflow.timestamp)}
                    </span>
                    <div className="flex items-center gap-1 text-primary font-medium">
                      <Eye className="w-3 h-3" />
                      <span>{isCurrentPreset ? 'Current' : isAlreadyActive ? 'Viewing' : 'Switch'}</span>
                    </div>
                  </div>
                </div>
                )
              })}
            </div>
          )}
        </div>
      </div>
    </>
  )
}

export default RunningWorkflowsDrawer
