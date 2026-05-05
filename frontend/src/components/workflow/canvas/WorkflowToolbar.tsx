import React, { useEffect, useRef, useMemo, useCallback, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import {
  Square,
  BookOpen,
  Settings,
  FileText,
  BarChart3,
  DollarSign,
  Package,
  Database,
  Table2,
  Beaker,
} from 'lucide-react'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useWorkflowStore, type RunFolder } from '../../../stores/useWorkflowStore'
import { useChatStore } from '../../../stores/useChatStore'
import type { ActiveSessionInfo, VariablesManifest } from '../../../services/api-types'
import type { PlanningResponse } from '../../../utils/stepConfigMatching'
import type { WorkflowExecutionStatus } from '../hooks/useWorkflowExecution'
import type { ExecutionOptions } from '../../../services/api-types'
import { agentApi } from '../../../services/api'
import { useSessionExecutionTree } from '../../../hooks/useSessionExecutionTree'
import { useCommandDialogStore } from '../../../stores/useCommandDialogStore'
import LearningsPopup from '../LearningsPopup'
import KBPopup from '../KBPopup'
import DatabasePopup from '../DatabasePopup'
import ExecutionLogsPopup from '../ExecutionLogsPopup'
import EvaluationPopup from '../EvaluationPopup'
import CostsPopup from '../CostsPopup'
import WorkflowVersionsPopup from '../WorkflowVersionsPopup'
import AutoImprovementPopup from '../AutoImprovementPopup'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../ui/tooltip'
import {
  extractGroupNameFromFolder,
  resolveGroupFolderPath
} from '../../../utils/workflowUtils'

// Execution phase ID - special phase that should be displayed separately
const EXECUTION_PHASE_ID = 'execution'
const EVAL_EXECUTION_PHASE_ID = 'evaluation-execution'

const isActiveRuntimeSession = (session?: ActiveSessionInfo | null): boolean => {
  if (!session) return false
  const status = (session.status || '').toLowerCase()
  return status === 'running' ||
    status === 'paused' ||
    status === 'waiting' ||
    status === 'waiting_feedback' ||
    status === 'idle' ||
    session.has_running_background_agents === true ||
    (session.running_background_agent_count ?? 0) > 0
}

interface WorkflowToolbarProps {
  status: WorkflowExecutionStatus
  hasPlan: boolean
  plan?: PlanningResponse | null  // Plan data for identifying conditional steps and branches
  currentPhase?: string
  workspacePath?: string | null
  presetQueryId?: string | null  // Used to persist settings per workflow
  // API data passed as props (avoids store subscription issues)
  runFolders: RunFolder[]
  variablesManifest: VariablesManifest | null
  isLoadingWorkspaceState?: boolean  // Whether workspace state (iterations, manifest) is loading
  onStartPhase: (phaseId: string, executionOptions?: ExecutionOptions) => void
  onCreatePlan: () => void
  showChatArea?: boolean
  onToggleChatArea?: () => void
  onRefresh?: () => Promise<void>  // Refresh plan and variables
  className?: string
}

export const WorkflowToolbar: React.FC<WorkflowToolbarProps> = ({
  status,
  plan,
  workspacePath,
  presetQueryId,
  runFolders,
  variablesManifest,
  isLoadingWorkspaceState = false,
  onStartPhase,
  showChatArea = false,
  onRefresh,
  className = ''
}) => {
  // Normalize runFolders to avoid repeated null checks throughout the component
  const folders = useMemo(() => runFolders ?? [], [runFolders])

  // Workspace store for opening folders
  const fetchFiles = useWorkspaceStore(state => state.fetchFiles)

  // Workflow store - use useShallow to prevent unnecessary re-renders
  // Note: runFolders, variablesManifest come from props (passed from WorkflowCanvas)
  const {
    selectedRunFolder,
    selectedGroupIds,
    currentRunningGroupId,
    buildExecutionOptions,
    loadSavedSettings,
    setSelectedGroupIds,
    restoreSelectionFromLocalStorage,
    showWorkspacePane,
    workflowWorkspaceView,
    canvasViewMode,
  } = useWorkflowStore(useShallow(state => ({
    selectedRunFolder: state.selectedRunFolder,
    selectedGroupIds: state.selectedGroupIds,
    currentRunningGroupId: state.currentRunningGroupId,
    buildExecutionOptions: state.buildExecutionOptions,
    loadSavedSettings: state.loadSavedSettings,
    setSelectedGroupIds: state.setSelectedGroupIds,
    restoreSelectionFromLocalStorage: state.restoreSelectionFromLocalStorage,
    showWorkspacePane: state.showWorkspacePane,
    workflowWorkspaceView: state.workflowWorkspaceView,
    canvasViewMode: state.canvasViewMode,
  })))

  // Reset start point when switching away from plan mode
  // Calculate the best run folder to use for popups (context-aware)
  // Priority: currentRunningGroupId > selectedRunFolder (if group path) > first selectedGroupIds
  const contextRunFolder = useMemo(() => {
    const resolved = resolveGroupFolderPath({
      currentRunningGroupId,
      selectedRunFolder,
      selectedGroupIds,
      manifest: variablesManifest
    })
    return resolved || selectedRunFolder
  }, [currentRunningGroupId, selectedRunFolder, selectedGroupIds, variablesManifest])
  
  // Memoize runFolders array to prevent unnecessary re-renders in popups
  const runFoldersNames = useMemo(() => {
    return folders.map(rf => rf.name)
  }, [folders])
  
  
  // Learnings popup state
  const [showLearningsPopup, setShowLearningsPopup] = useState(false)
  const [showKBPopup, setShowKBPopup] = useState(false)
  const [showDatabasePopup, setShowDatabasePopup] = useState(false)

  // Execution logs popup state
  const [showExecutionLogsPopup, setShowExecutionLogsPopup] = useState(false)

  // Costs popup state
  const [showCostsPopup, setShowCostsPopup] = useState(false)

  // Evaluation popup state
  const [showEvaluationPopup, setShowEvaluationPopup] = useState(false)
  const [showAutoImprovementPopup, setShowAutoImprovementPopup] = useState(false)

  // Versions popup state
  const [showVersionsPopup, setShowVersionsPopup] = useState(false)

  const closeAllPopups = useCallback(() => {
    setShowLearningsPopup(false)
    setShowKBPopup(false)
    setShowDatabasePopup(false)
    setShowExecutionLogsPopup(false)
    setShowCostsPopup(false)
    setShowEvaluationPopup(false)
    setShowVersionsPopup(false)
    setShowAutoImprovementPopup(false)
  }, [])
  
  // Close popups only when switching between two concrete workflows.
  // Preset refreshes can briefly unset workspacePath; treating that as a switch
  // closes every toolbar popup even though the user is still on the same workflow.
  const prevWorkspacePathRef = useRef<string | null>(workspacePath ?? null)
  useEffect(() => {
    if (!workspacePath) {
      return
    }
    if (prevWorkspacePathRef.current && prevWorkspacePathRef.current !== workspacePath) {
      closeAllPopups()
    }
    prevWorkspacePathRef.current = workspacePath
  }, [workspacePath, closeAllPopups])
  
  // Main workflow execution phase for the canvas toolbar
  const targetExecutionPhaseId = EXECUTION_PHASE_ID
  
  // Check if execution phase specifically is running (not just any phase)
  // Use a selector that only recalculates when chatTabs, pollingInterval, or sseConnections change
  const isExecutionRunning = useChatStore(state => {
    const chatTabs = state.chatTabs
    const pollingInterval = state.pollingInterval
    const sseConnections = state.sseConnections
    const allTabs = Object.values(chatTabs)

    try {
      // Filter for execution phase tabs belonging to the current preset
      const executionTabs = allTabs.filter(tab =>
        tab.metadata?.mode === 'workflow' &&
        tab.metadata?.phaseId === targetExecutionPhaseId &&
        tab.metadata?.presetQueryId === presetQueryId
      )

      // Check if any execution tab is streaming
      return executionTabs.some(tab => {
        // If tab is completed, it's not streaming
        if (tab.isCompleted) return false

        // Tab is streaming if there's an active connection (SSE or polling) and tab is not manually paused
        const hasActiveConnection = pollingInterval !== null
          || (tab.sessionId != null && sseConnections[tab.sessionId] != null)
        if (hasActiveConnection) {
          return tab.isStreaming !== false // Respect manual pause
        }

        // Also show Stop if tab.isStreaming is explicitly true (set immediately on query submit,
        // before SSE/polling connects)
        return tab.isStreaming === true
      })
    } catch (error) {
      console.error('[WorkflowToolbar] Error checking execution phase status:', error)
      return false
    }
  }) // Zustand will handle memoization - only re-render if result changes

  const {
    activeWorkflowTab,
    activeWorkflowRuntimeSession,
    setTabStreaming,
    setTabHasRunningBgAgents,
  } = useChatStore(useShallow(state => {
    const activeTab = state.activeTabId ? state.chatTabs[state.activeTabId] : null
    const activeWorkflowTab =
      activeTab?.metadata?.mode === 'workflow' &&
      activeTab.metadata?.presetQueryId === presetQueryId
        ? activeTab
        : null
    const activeWorkflowRuntimeSession = state.activeSessionsCache.find(session => {
      if (presetQueryId && session.preset_query_id === presetQueryId) return true
      if (workspacePath && session.workspace_path === workspacePath) return true
      return false
    })

    return {
      activeWorkflowTab,
      activeWorkflowRuntimeSession,
      setTabStreaming: state.setTabStreaming,
      setTabHasRunningBgAgents: state.setTabHasRunningBgAgents,
    }
  }))
  const activeWorkflowSessionId = activeWorkflowTab?.sessionId ?? activeWorkflowRuntimeSession?.session_id ?? null
  const { data: activeWorkflowExecutionTree } = useSessionExecutionTree(activeWorkflowSessionId, !!activeWorkflowSessionId)
  const runtimeDisplayStatus = isActiveRuntimeSession(activeWorkflowRuntimeSession) ? 'busy' : null
  const backendWorkflowDisplayStatus = runtimeDisplayStatus ?? activeWorkflowExecutionTree?.summary.display_status ?? null

  // Load saved settings when preset changes
  useEffect(() => {
    if (presetQueryId) {
      loadSavedSettings(presetQueryId)
    }
  }, [presetQueryId, loadSavedSettings])

  // Restore selection from localStorage after workspace state finishes loading
  // This ensures localStorage values are restored AFTER all API data is loaded
  const hasRestoredRef = useRef(false)
  useEffect(() => {
    // Only restore once when workspace loading completes and manifest is available
    if (!isLoadingWorkspaceState && variablesManifest && !hasRestoredRef.current) {
      restoreSelectionFromLocalStorage()
      hasRestoredRef.current = true
    }
    // Reset the flag when workspace starts loading (preset change)
    if (isLoadingWorkspaceState) {
      hasRestoredRef.current = false
    }
  }, [isLoadingWorkspaceState, variablesManifest, restoreSelectionFromLocalStorage])

  // Restore selectedGroupIds from execution state when page refreshes during execution
  // This handles the case where execution is running but selectedGroupIds was lost on page refresh
  useEffect(() => {
    if (isExecutionRunning && selectedGroupIds.length === 0 && currentRunningGroupId) {
      // If execution is running but no groups are selected, restore from currentRunningGroupId
      console.log('[WorkflowToolbar] Restoring selectedGroupIds from currentRunningGroupId:', currentRunningGroupId)
      setSelectedGroupIds([currentRunningGroupId])
    } else if (isExecutionRunning && selectedGroupIds.length === 0 && variablesManifest?.groups) {
      // If we have groups in manifest but none selected, try to infer from selectedRunFolder
      // Extract group ID from selectedRunFolder if it's a group path
      if (selectedRunFolder && selectedRunFolder.includes('/')) {
        const parts = selectedRunFolder.split('/')
        if (parts.length === 2) {
          const groupFolderName = parts[1]
          // Try to find matching group in manifest
          const matchingGroup = variablesManifest.groups.find(g => {
            if (g.name === groupFolderName) return true
            const sanitized = groupFolderName.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
            const groupSanitized = g.name.toLowerCase().replace(/[^a-z0-9-]/g, '-').replace(/-+/g, '-').trim()
            return sanitized === groupSanitized
          })
          if (matchingGroup) {
            console.log('[WorkflowToolbar] Restoring selectedGroupIds from selectedRunFolder:', matchingGroup.name)
            setSelectedGroupIds([matchingGroup.name])
          }
        }
      }
    }
  }, [isExecutionRunning, selectedGroupIds.length, currentRunningGroupId, variablesManifest, selectedRunFolder, setSelectedGroupIds])

  // selectedGroupIds is already included in the batched selector above
  
  // Settings are no longer persisted to localStorage - removed save logic

  // NOTE: loadRunFolders is NOT called here anymore.
  // useWorkspaceState in WorkflowCanvas handles initial load of:
  // - run_folders (via setRunFolders)
  // - variables_manifest (via setVariablesManifest)
  // This eliminates duplicate API calls on initial page load.

  // View selection should follow the actual canvas/report renderer, not the
  // higher-level workspace mode.
  const isBuilderPaneVisible = showChatArea === true && !showWorkspacePane
  const isBuilderModeActive = workflowWorkspaceView === 'builder' || isBuilderPaneVisible
  const isReportWorkspace = showWorkspacePane && canvasViewMode === 'report'
  const isFlowWorkspace = showWorkspacePane && canvasViewMode === 'flow'
  const canStopActiveWorkflowSession = !!(
    activeWorkflowTab?.sessionId &&
    activeWorkflowTab.metadata?.phaseId !== EXECUTION_PHASE_ID &&
    backendWorkflowDisplayStatus === 'busy'
  )
  const workflowActivityStatus = useMemo(() => {
    if (backendWorkflowDisplayStatus === 'busy') {
      return {
        label: 'Busy',
        className: 'border-[hsl(var(--info)/0.22)] bg-[hsl(var(--info)/0.1)] text-[hsl(var(--info))]',
        dotClassName: 'bg-[hsl(var(--info))]',
      }
    }
    if (backendWorkflowDisplayStatus === 'stopped') {
      return {
        label: 'Stopped',
        className: 'border-border bg-muted/60 text-muted-foreground',
        dotClassName: 'bg-muted-foreground/70',
      }
    }
    return {
      label: 'Idle',
      className: 'border-[hsl(var(--success)/0.22)] bg-[hsl(var(--success)/0.1)] text-[hsl(var(--success))]',
      dotClassName: 'bg-[hsl(var(--success))]',
    }
  }, [backendWorkflowDisplayStatus])

  const handleStopActiveWorkflowSession = useCallback(async () => {
    if (!activeWorkflowTab?.sessionId) return

    try {
      await agentApi.stopSession(activeWorkflowTab.sessionId, true)
      setTabStreaming(activeWorkflowTab.tabId, false)
      setTabHasRunningBgAgents(activeWorkflowTab.tabId, false)
    } catch (error) {
      console.error('[WorkflowToolbar] Failed to stop active workflow session:', error)
    }
  }, [activeWorkflowTab, setTabHasRunningBgAgents, setTabStreaming])

  const handleRunEvaluation = useCallback(async (runFolder: string) => {
    if (!runFolder || !runFolder.includes('/')) {
      throw new Error('Select a group-scoped run folder like iteration-2/manish before running evaluation.')
    }

    const options = buildExecutionOptions()
    const inferredGroupName = extractGroupNameFromFolder(runFolder, variablesManifest)
    const enabledGroupNames = inferredGroupName
      ? [inferredGroupName]
      : (options.enabled_group_names && options.enabled_group_names.length > 0 ? options.enabled_group_names : undefined)

    onStartPhase(EVAL_EXECUTION_PHASE_ID, {
      ...options,
      selected_run_folder: runFolder,
      enabled_group_names: enabledGroupNames,
    })
  }, [buildExecutionOptions, onStartPhase, variablesManifest])

  return (
    <>
    <div className={`
      flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 px-3 py-1.5
      bg-background border-b border-border
      relative z-10
      ${className}
    `}>
      {/* Left side - workflow context */}
      <div className="flex min-w-0 flex-1 items-center gap-x-3 gap-y-1.5 flex-wrap">
        <div className="flex min-w-0 items-center gap-x-3 gap-y-1.5 flex-wrap">
        <div className="flex shrink-0 items-center gap-2">
          <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            Mode
          </span>
          <div className="inline-flex items-center gap-0.5 rounded-lg border border-border bg-muted/60 p-0.5 shadow-sm">
              <button
                onClick={() => {
                  const store = useWorkflowStore.getState()
                  if (isBuilderPaneVisible) {
                    store.setShowChatArea(false)
                    return
                  }
                  store.setWorkflowWorkspaceView('builder')
                  store.setShowWorkspacePane(false)
                  store.setShowChatArea(true)
                  onStartPhase('workflow-builder')
                }}
                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                  isBuilderModeActive
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                }`}
              >
                Builder
              </button>
          </div>
        </div>
        </div>

        <div className="flex shrink-0 items-center gap-2">
          <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            View
          </span>
          <div className="inline-flex items-center gap-0.5 rounded-lg border border-border bg-muted/60 p-0.5 shadow-sm">
              <button
                onClick={() => {
                  const store = useWorkflowStore.getState()
                  if (canvasViewMode === 'flow' && showWorkspacePane && showChatArea) {
                    store.setShowWorkspacePane(false)
                    return
                  }
                  store.setWorkflowWorkspaceView('flow')
                  store.setShowWorkspacePane(true)
                  store.setCanvasViewMode('flow')
                }}
                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                  isFlowWorkspace
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                }`}
              >
                Flow
              </button>
              <button
                onClick={() => {
                  const store = useWorkflowStore.getState()
                  if (canvasViewMode === 'report' && showWorkspacePane && showChatArea) {
                    store.setShowWorkspacePane(false)
                    return
                  }
                  store.setWorkflowWorkspaceView('report')
                  store.setShowWorkspacePane(true)
                  store.setCanvasViewMode('report')
                }}
                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                  isReportWorkspace
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                }`}
              >
                Report
              </button>
          </div>
        </div>

        <div className="hidden h-5 w-px shrink-0 bg-border sm:block" />

        <div className="flex shrink-0 items-center gap-2">
          <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            Status
          </span>
          <div
            className={`inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-semibold ${workflowActivityStatus.className}`}
          >
            <span className={`h-1.5 w-1.5 rounded-full ${workflowActivityStatus.dotClassName}`} />
            {workflowActivityStatus.label}
          </div>
          {canStopActiveWorkflowSession && (
            <button
              onClick={handleStopActiveWorkflowSession}
              className="inline-flex items-center gap-1 rounded-md border border-[hsl(var(--destructive)/0.22)] bg-[hsl(var(--destructive)/0.1)] px-2.5 py-1 text-xs font-semibold text-[hsl(var(--destructive))] transition-colors hover:bg-[hsl(var(--destructive)/0.16)]"
              title="Stop current workflow chat session"
            >
              <Square className="w-3 h-3" fill="currentColor" />
              <span>Stop</span>
            </button>
          )}
        </div>
      </div>

      {/* Center - Status indicator */}
      <div className="flex shrink-0 items-center gap-1.5">
        {status === 'waiting_feedback' && (
          <div className="flex items-center gap-1.5 px-2 py-1 bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300 rounded-md text-xs">
            <div className="w-1.5 h-1.5 bg-amber-500 rounded-full animate-pulse" />
            Waiting for feedback
          </div>
        )}
        {status === 'failed' && (
          <div className="flex items-center gap-1.5 px-2 py-1 bg-red-100 dark:bg-red-900/30 text-red-700 dark:text-red-300 rounded-md text-xs">
            <div className="w-1.5 h-1.5 bg-red-500 rounded-full" />
            Failed
          </div>
        )}
      </div>

      {/* Right side - View controls */}
      <div className="ml-auto flex shrink-0 items-center gap-1">
        <TooltipProvider delayDuration={150}>
        {/* Auto-improvement framework — metrics, trajectory, decisions */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowAutoImprovementPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <Beaker className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Auto-improvement (metrics, trajectory, decisions)</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Costs - opens popup with cost analysis across all iterations */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowCostsPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <DollarSign className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Costs</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Execution Logs - opens popup with detailed execution logs */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowExecutionLogsPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <FileText className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Execution logs</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Learnings - opens popup with learning metadata */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowLearningsPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <BookOpen className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Learnings</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Knowledgebase — entities/relationships accumulated by the KB update agent */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowKBPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <Database className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Knowledgebase</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Database - durable db/*.json sources used by report widgets and steps */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowDatabasePopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <Table2 className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Database</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Evaluation Reports - opens popup with evaluation scores */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowEvaluationPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <BarChart3 className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Evaluation reports</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Versions - opens popup with version publish/revert */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowVersionsPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <Package className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Versions</p></TooltipContent>
          </Tooltip>
        )}

        {/* Workflow Settings Button — opens the preset settings modal from the top header */}
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={() => useCommandDialogStore.getState().openDialog('presetSettings')}
              className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
            >
              <Settings className="w-3.5 h-3.5" />
            </button>
          </TooltipTrigger>
          <TooltipContent side="bottom"><p>Settings</p></TooltipContent>
        </Tooltip>

        </TooltipProvider>
      </div>
    </div>
    {/* Learnings Popup */}
    <LearningsPopup
      isOpen={showLearningsPopup}
      onClose={() => setShowLearningsPopup(false)}
      workspacePath={workspacePath || null}
      plan={plan || null}
    />

    {/* Knowledgebase Popup */}
    <KBPopup
      isOpen={showKBPopup}
      onClose={() => setShowKBPopup(false)}
      workspacePath={workspacePath || null}
    />

    {/* Database Popup */}
    <DatabasePopup
      isOpen={showDatabasePopup}
      onClose={() => setShowDatabasePopup(false)}
      workspacePath={workspacePath || null}
    />

    {/* Costs Popup */}
    <CostsPopup
      isOpen={showCostsPopup}
      onClose={() => setShowCostsPopup(false)}
      workspacePath={workspacePath || null}
      runFolders={runFoldersNames}
      selectedRunFolder={contextRunFolder}
    />

    {/* Execution Logs Popup */}
    <ExecutionLogsPopup
      isOpen={showExecutionLogsPopup}
      onClose={() => setShowExecutionLogsPopup(false)}
      workspacePath={workspacePath || null}
      runFolder={contextRunFolder}
      runFolders={runFoldersNames}
    />

    {/* Evaluation Reports Popup */}
    <EvaluationPopup
      isOpen={showEvaluationPopup}
      onClose={() => setShowEvaluationPopup(false)}
      workspacePath={workspacePath || null}
      selectedRunFolder={contextRunFolder}
      runFolders={runFoldersNames}
      onRunEvaluation={handleRunEvaluation}
    />

    {/* Auto-improvement framework popup */}
    <AutoImprovementPopup
      isOpen={showAutoImprovementPopup}
      onClose={() => setShowAutoImprovementPopup(false)}
      workspacePath={workspacePath || null}
    />

    {/* Workflow Versions Popup */}
    <WorkflowVersionsPopup
      isOpen={showVersionsPopup}
      onClose={() => setShowVersionsPopup(false)}
      workspacePath={workspacePath || null}
      onRefresh={async () => {
        if (onRefresh) await onRefresh()
        fetchFiles()
      }}
    />
    </>
  )
}

WorkflowToolbar.whyDidYouRender = true

export default WorkflowToolbar
