import React, { useEffect, useMemo, useCallback, useRef } from 'react'
import { useShallow } from 'zustand/react/shallow'
import {
  Square,
  BookOpen,
  Settings,
  FileText,
  DollarSign,
  Package,
  Database,
  Table2,
  Beaker,
  ShieldCheck,
  HelpCircle,
} from 'lucide-react'
import { useWorkspaceStore } from '../../../stores/useWorkspaceStore'
import { useWorkflowStore, type RunFolder } from '../../../stores/useWorkflowStore'
import { useChatStore } from '../../../stores/useChatStore'
import { useAuthStore } from '../../../stores/useAuthStore'
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
import CostsPopup from '../CostsPopup'
import WorkflowVersionsPopup from '../WorkflowVersionsPopup'
import WorkflowAccessPopup from '../WorkflowAccessPopup'
import AutoImprovementPopup from '../AutoImprovementPopup'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../ui/tooltip'
import {
  resolveGroupFolderPath
} from '../../../utils/workflowUtils'
import { hasWorkflowWriteAccess, hasWorkflowOwnerAccess } from '../../../utils/workflowPermissions'

// Execution phase ID - special phase that should be displayed separately
const EXECUTION_PHASE_ID = 'execution'
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
  workspacePath?: string | null
  presetQueryId?: string | null  // Used to persist settings per workflow
  variablesManifest: VariablesManifest | null
  isLoadingWorkspaceState?: boolean  // Whether workspace state (iterations, manifest) is loading
  onStartPhase: (phaseId: string, executionOptions?: ExecutionOptions) => void
  showChatArea?: boolean
  onOpenPopup: (popup: WorkflowToolbarPopup) => void
  className?: string
}

export type WorkflowToolbarPopup =
  | 'autoImprovement'
  | 'costs'
  | 'logs'
  | 'learnings'
  | 'kb'
  | 'database'
  | 'versions'
  | 'access'
  | null

interface WorkflowToolbarPopupsProps {
  activePopup: WorkflowToolbarPopup
  onClose: () => void
  workspacePath?: string | null
  plan?: PlanningResponse | null
  runFolders: RunFolder[]
  variablesManifest: VariablesManifest | null
  onRefresh?: () => Promise<void>
}

export const WorkflowToolbarPopups: React.FC<WorkflowToolbarPopupsProps> = ({
  activePopup,
  onClose,
  workspacePath,
  plan,
  runFolders,
  variablesManifest,
  onRefresh,
}) => {
  const fetchFiles = useWorkspaceStore(state => state.fetchFiles)
  const {
    selectedRunFolder,
    selectedGroupIds,
    currentRunningGroupId,
  } = useWorkflowStore(useShallow(state => ({
    selectedRunFolder: state.selectedRunFolder,
    selectedGroupIds: state.selectedGroupIds,
    currentRunningGroupId: state.currentRunningGroupId,
  })))

  const contextRunFolder = useMemo(() => {
    const resolved = resolveGroupFolderPath({
      currentRunningGroupId,
      selectedRunFolder,
      selectedGroupIds,
      manifest: variablesManifest
    })
    return resolved || selectedRunFolder
  }, [currentRunningGroupId, selectedRunFolder, selectedGroupIds, variablesManifest])

  const runFoldersNames = useMemo(() => {
    return (runFolders ?? []).map(rf => rf.name)
  }, [runFolders])

  return (
    <>
      <LearningsPopup
        isOpen={activePopup === 'learnings'}
        onClose={onClose}
        workspacePath={workspacePath || null}
        plan={plan || null}
      />

      <KBPopup
        isOpen={activePopup === 'kb'}
        onClose={onClose}
        workspacePath={workspacePath || null}
      />

      <DatabasePopup
        isOpen={activePopup === 'database'}
        onClose={onClose}
        workspacePath={workspacePath || null}
      />

      <CostsPopup
        isOpen={activePopup === 'costs'}
        onClose={onClose}
        workspacePath={workspacePath || null}
        runFolders={runFoldersNames}
        selectedRunFolder={contextRunFolder}
      />

      <ExecutionLogsPopup
        isOpen={activePopup === 'logs'}
        onClose={onClose}
        workspacePath={workspacePath || null}
        runFolder={contextRunFolder}
        runFolders={runFoldersNames}
      />

      <AutoImprovementPopup
        isOpen={activePopup === 'autoImprovement'}
        onClose={onClose}
        workspacePath={workspacePath || null}
        selectedRunFolder={contextRunFolder}
      />

      <WorkflowVersionsPopup
        isOpen={activePopup === 'versions'}
        onClose={onClose}
        workspacePath={workspacePath || null}
        onRefresh={async () => {
          if (onRefresh) await onRefresh()
          fetchFiles()
        }}
      />

      <WorkflowAccessPopup
        isOpen={activePopup === 'access'}
        onClose={onClose}
      />
    </>
  )
}

export const WorkflowToolbar: React.FC<WorkflowToolbarProps> = ({
  status,
  hasPlan,
  workspacePath,
  presetQueryId,
  variablesManifest,
  isLoadingWorkspaceState = false,
  onStartPhase,
  showChatArea = false,
  onOpenPopup,
  className = ''
}) => {
  const canWriteWorkflow = useAuthStore(state => hasWorkflowWriteAccess(state.user, state.isMultiUserMode))
  const canManageAccess = useAuthStore(state => state.isMultiUserMode && hasWorkflowOwnerAccess(state.user, state.isMultiUserMode))

  // Workflow store - use useShallow to prevent unnecessary re-renders
  // Note: variablesManifest comes from props (passed from WorkflowLayout)
  const {
    selectedRunFolder,
    selectedGroupIds,
    currentRunningGroupId,
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
    loadSavedSettings: state.loadSavedSettings,
    setSelectedGroupIds: state.setSelectedGroupIds,
    restoreSelectionFromLocalStorage: state.restoreSelectionFromLocalStorage,
    showWorkspacePane: state.showWorkspacePane,
    workflowWorkspaceView: state.workflowWorkspaceView,
    canvasViewMode: state.canvasViewMode,
  })))
  
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

  return (
    <>
    <div className={`
      flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 px-3 py-1.5
      bg-background border-b border-border
      relative z-10
      ${className}
    `}>
      {/* Left side - primary workflow views */}
      <div className="flex min-w-0 flex-1 items-center gap-x-3 gap-y-1.5 flex-wrap">
        <div className="flex min-w-full items-center gap-2 sm:min-w-0">
          <div
            data-tour="workflow-view-switcher"
            data-testid="tour-workflow-view-switcher"
            className="inline-flex w-full items-center gap-0.5 rounded-lg border border-border bg-muted/60 p-0.5 shadow-sm sm:w-auto"
          >
            <button
              onClick={() => {
                const store = useWorkflowStore.getState()
                store.setWorkflowWorkspaceView('builder')
                store.setShowWorkspacePane(false)
                store.setShowChatArea(true)
                onStartPhase('workflow-builder')
              }}
              className={`min-w-0 flex-1 rounded-md px-3 py-1 text-xs font-medium transition-all sm:flex-none ${
                isBuilderModeActive
                  ? 'bg-background text-foreground shadow-sm'
                  : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
              }`}
            >
              Chat
            </button>
            {hasPlan && (
              <button
                onClick={() => {
                  const store = useWorkflowStore.getState()
                  store.setWorkflowWorkspaceView('flow')
                  store.setShowWorkspacePane(true)
                  store.setCanvasViewMode('flow')
                }}
                className={`min-w-0 flex-1 rounded-md px-3 py-1 text-xs font-medium transition-all sm:flex-none ${
                  isFlowWorkspace
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                }`}
              >
                Plan
              </button>
            )}
            {workspacePath && (
              <button
                onClick={() => {
                  const store = useWorkflowStore.getState()
                  store.setWorkflowWorkspaceView('report')
                  store.setShowWorkspacePane(true)
                  store.setCanvasViewMode('report')
                }}
                className={`min-w-0 flex-1 rounded-md px-3 py-1 text-xs font-medium transition-all sm:flex-none ${
                  isReportWorkspace
                    ? 'bg-background text-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'
                }`}
              >
                Report
              </button>
            )}
          </div>
        </div>

        <div className="hidden h-5 w-px shrink-0 bg-border sm:block" />

        <div className="flex shrink-0 items-center gap-2">
          <span className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            Status
          </span>
          <div
            data-tour="workflow-status"
            data-testid="tour-workflow-status"
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
      <div data-tour="workflow-tools" data-testid="tour-workflow-tools" className="ml-auto flex shrink-0 items-center gap-1">
        <TooltipProvider delayDuration={150}>
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={() => window.dispatchEvent(new Event('open-workflow-walkthrough'))}
              className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
            >
              <HelpCircle className="w-3.5 h-3.5" />
            </button>
          </TooltipTrigger>
          <TooltipContent side="bottom"><p>Walkthrough</p></TooltipContent>
        </Tooltip>

        {/* Auto-improvement framework — metrics, trajectory, decisions (read-only safe for run users) */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => onOpenPopup('autoImprovement')}
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
                onClick={() => onOpenPopup('costs')}
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
                onClick={() => onOpenPopup('logs')}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <FileText className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Execution logs</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Learnings - opens popup with learning metadata (read-only safe) */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => onOpenPopup('learnings')}
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
                onClick={() => onOpenPopup('kb')}
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
                onClick={() => onOpenPopup('database')}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <Table2 className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Database</p></TooltipContent>
          </Tooltip>
        )}

        {/* Show Versions - read-only list view safe; publish/revert backend-gated */}
        {workspacePath && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => onOpenPopup('versions')}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <Package className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Versions</p></TooltipContent>
          </Tooltip>
        )}

        {/* Workflow Access (multi-user mode only, owners only) */}
        {canManageAccess && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => onOpenPopup('access')}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <ShieldCheck className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Workflow Access</p></TooltipContent>
          </Tooltip>
        )}

        {/* Workflow Settings — write-only (read users don't see this) */}
        {canWriteWorkflow && (
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
        )}

        </TooltipProvider>
      </div>
    </div>
    </>
  )
}

WorkflowToolbar.whyDidYouRender = true

export default WorkflowToolbar
