import React, { useEffect, useRef, useMemo, useCallback, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import {
  BookOpen,
  Settings,
  FileText,
  DollarSign,
  Cloud,
  Globe,
  Database,
  Table2,
  ShieldCheck,
  Activity,
  CalendarClock,
  Sparkles,
  GitCommitVertical,
  RefreshCw,
  X,
} from 'lucide-react'
import ModalPortal from '../../ui/ModalPortal'
import PlanChangelogFeed from '../PlanChangelogFeed'
import { useWorkflowStore, type RunFolder } from '../../../stores/useWorkflowStore'
import { useWorkflowManifestStore } from '../../../stores/useWorkflowManifestStore'
import { useChatStore } from '../../../stores/useChatStore'
import { useAuthStore } from '../../../stores/useAuthStore'
import type { PulseModuleState, VariablesManifest, WorkflowScheduleEntry } from '../../../services/api-types'
import type { PlanningResponse } from '../../../utils/stepConfigMatching'
import type { WorkflowExecutionStatus } from '../hooks/useWorkflowExecution'
import type { ExecutionOptions } from '../../../services/api-types'
import { useCommandDialogStore } from '../../../stores/useCommandDialogStore'
import { agentApi } from '../../../services/api'
import { schedulerApi } from '../../../api/scheduler'
import LearningsPopup from '../LearningsPopup'
import KBPopup from '../KBPopup'
import DatabasePopup from '../DatabasePopup'
import ExecutionLogsPopup from '../ExecutionLogsPopup'
import CostsPopup from '../CostsPopup'
import WorkflowBackupPopup from '../WorkflowBackupPopup'
import { getBackupDotClass, formatBackupStateLabel } from '../backupStatus'
import WorkflowPublishPopup from '../WorkflowPublishPopup'
import { getPublishDotClass, formatPublishStateLabel } from '../publishStatus'
import WorkflowAccessPopup from '../WorkflowAccessPopup'
import WorkflowScheduleRunsPanel from '../../scheduler/WorkflowScheduleRunsPanel'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../ui/tooltip'
import {
  resolveGroupFolderPath
} from '../../../utils/workflowUtils'
import { hasWorkflowWriteAccess, hasWorkflowOwnerAccess } from '../../../utils/workflowPermissions'

// Execution phase ID - special phase that should be displayed separately
const EXECUTION_PHASE_ID = 'execution'
const WORKFLOW_SCHEDULE_TOOLBAR_LIMIT = 10_000

type WorkflowScheduleStats = {
  total: number
  running: number
  enabled: number
  paused: number
  missed: number
  issues: number
}

const EMPTY_WORKFLOW_SCHEDULE_STATS: WorkflowScheduleStats = {
  total: 0,
  running: 0,
  enabled: 0,
  paused: 0,
  missed: 0,
  issues: 0,
}

function normalizeWorkspacePath(path?: string | null): string {
  return (path || '').replace(/\/+$/, '')
}

function formatWorkflowNameFromPath(path?: string | null): string {
  const name = normalizeWorkspacePath(path).split('/').filter(Boolean).pop()
  return name || 'Workflow'
}

function isAutoImproveSchedule(schedule: WorkflowScheduleEntry): boolean {
  if ((schedule.workshop_mode || '').toLowerCase() !== 'optimizer') return false
  const nameAndDescription = `${schedule.name || ''} ${schedule.description || ''}`.toLowerCase()
  if (nameAndDescription.includes('retired') || nameAndDescription.includes('duplicate')) return false
  if (/\bharden\b/.test(nameAndDescription) && !/\bimprove\b/.test(nameAndDescription)) return false
  return true
}

const PULSE_MODULE_COMMANDS: Array<{ id: string; label: string; description: string }> = [
  { id: 'harden', label: 'Harden', description: 'Bug checks and low-risk fixes' },
  { id: 'artifact_review', label: 'Artifact review', description: 'Plan-change artifact drift' },
  { id: 'report_health', label: 'Report health', description: 'Dashboard/report accuracy' },
  { id: 'learning_health', label: 'Learning health', description: 'Learning freshness and quality' },
  { id: 'knowledgebase_health', label: 'Knowledge base', description: 'KB freshness and contradictions' },
  { id: 'db_health', label: 'Database health', description: 'DB/schema/data quality checks' },
  { id: 'cost_llm_time', label: 'Cost + LLM + time', description: 'Cost, model, and runtime review' },
  { id: 'goal_advisor', label: 'Goal Advisor', description: 'Strategic review when goal evidence is weak' },
]

const PULSE_FIXED_COMMANDS: Array<{ id: string; label: string; description: string; status: string }> = [
  { id: 'dashboard', label: 'Dashboard + questions', description: 'Updates Pulse UI and asks for input if needed', status: 'FINAL' },
  { id: 'backup', label: 'Backup', description: 'Saves current workflow artifacts', status: 'FINAL' },
  { id: 'publish', label: 'Publish', description: 'Refreshes public report when publishing is configured', status: 'IF SET' },
  { id: 'notify', label: 'Notify', description: 'Sends the run summary after report updates', status: 'FINAL' },
]

function pulseStatusToneClass(status: string): string {
  const normalized = status.toLowerCase()
  if (normalized === 'failed' || normalized === 'blocked') {
    return 'border-red-500/30 bg-red-500/10 text-red-600 dark:text-red-300'
  }
  if (normalized === 'changed' || normalized === 'due') {
    return 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300'
  }
  if (normalized === 'done') {
    return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
  }
  if (normalized === 'skipped' || normalized === 'no data' || normalized === 'waiting' || normalized === 'if set') {
    return 'border-border bg-muted text-muted-foreground'
  }
  return 'border-primary/20 bg-primary/10 text-primary'
}

function formatPulseTimestamp(value?: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function getPulseModuleStatus(state?: PulseModuleState): { label: string; detail: string; time: string } {
  if (!state) {
    return { label: 'NO DATA', detail: 'No Pulse run recorded yet', time: '' }
  }
  const result = (state.last_result || '').trim()
  if (result) {
    return {
      label: result.toUpperCase(),
      detail: state.last_result_reason || state.last_reason || 'Completed in the latest recorded Pulse run',
      time: formatPulseTimestamp(state.last_ran_at || state.updated_at),
    }
  }
  const decision = (state.last_gate_decision || state.last_decision || '').trim()
  if (decision) {
    return {
      label: decision.toUpperCase(),
      detail: state.last_reason || 'Gate decision recorded',
      time: formatPulseTimestamp(state.last_checked_at || state.updated_at),
    }
  }
  return {
    label: 'WAITING',
    detail: 'State exists, but no decision has been recorded yet',
    time: formatPulseTimestamp(state.updated_at),
  }
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
  // Chat tab strip (WorkflowChatTabs) rendered inline on the left of this bar so the
  // workflow chat tabs + new-chat share one row with the status/tools instead of
  // sitting in a separate bar below.
  chatTabsSlot?: React.ReactNode
  className?: string
}

export const WorkflowToolbar: React.FC<WorkflowToolbarProps> = ({
  status,
  hasPlan,
  plan,
  workspacePath,
  presetQueryId,
  runFolders,
  variablesManifest,
  isLoadingWorkspaceState = false,
  onStartPhase,
  showChatArea = false,
  chatTabsSlot,
  className = ''
}) => {
  // Normalize runFolders to avoid repeated null checks throughout the component
  const folders = useMemo(() => runFolders ?? [], [runFolders])
  const canWriteWorkflow = useAuthStore(state => hasWorkflowWriteAccess(state.user, state.isMultiUserMode))
  const canManageAccess = useAuthStore(state => state.isMultiUserMode && hasWorkflowOwnerAccess(state.user, state.isMultiUserMode))

  // Workspace store for opening folders

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

  // Post-run monitor opt-in (workflow.json::post_run_monitor). When on, Pulse
  // Gate runs after each scheduled run, records Bug + Goal verdicts, and selects
  // any deeper maintenance/Goal Advisor modules that are due.
  const monitorOn = useWorkflowManifestStore((s) => {
    const wf = s.workflows.find((w) => w.workspace_path === workspacePath)
    return !!wf?.manifest.post_run_monitor
  })
  const workflowSchedules = useWorkflowManifestStore((s) => {
    const wf = s.workflows.find((w) => w.workspace_path === workspacePath)
    return wf?.manifest.schedules || []
  })
  const updateWorkflowManifest = useWorkflowManifestStore((s) => s.updateWorkflow)
  const [monitorSaving, setMonitorSaving] = useState(false)
  const toggleMonitor = useCallback(async () => {
    if (!workspacePath || monitorSaving) return
    setMonitorSaving(true)
    try {
      await updateWorkflowManifest(workspacePath, { post_run_monitor: !monitorOn })
    } catch (err) {
      console.error('[WorkflowToolbar] Failed to toggle post-run monitor:', err)
    } finally {
      setMonitorSaving(false)
    }
  }, [workspacePath, monitorOn, monitorSaving, updateWorkflowManifest])
  const [showMonitorHelp, setShowMonitorHelp] = useState(false)
  const [pulseModuleStates, setPulseModuleStates] = useState<PulseModuleState[]>([])
  const [pulseStatusLoading, setPulseStatusLoading] = useState(false)
  const [pulseStatusError, setPulseStatusError] = useState<string | null>(null)
  const autoImproveSchedules = useMemo(
    () => workflowSchedules.filter(isAutoImproveSchedule),
    [workflowSchedules]
  )
  const [showAutoImproveHelp, setShowAutoImproveHelp] = useState(false)

  // Backup popup state
  const [showBackupPopup, setShowBackupPopup] = useState(false)
  const [backupState, setBackupState] = useState<string>('not_configured')
  const [showPublishPopup, setShowPublishPopup] = useState(false)
  const [publishState, setPublishState] = useState<string>('not_configured')
  const [showPlanEditsPopup, setShowPlanEditsPopup] = useState(false)
  const [showAccessPopup, setShowAccessPopup] = useState(false)
  const [showWorkflowSchedulesPanel, setShowWorkflowSchedulesPanel] = useState(false)
  const [workflowScheduleStats, setWorkflowScheduleStats] = useState<WorkflowScheduleStats>(EMPTY_WORKFLOW_SCHEDULE_STATS)

  const workflowScheduleLabel = useMemo(
    () => formatWorkflowNameFromPath(workspacePath),
    [workspacePath]
  )

  const refreshPulseModuleStates = useCallback(async () => {
    if (!workspacePath) {
      setPulseModuleStates([])
      setPulseStatusError(null)
      return
    }
    setPulseStatusLoading(true)
    setPulseStatusError(null)
    try {
      const resp = await agentApi.getPulseModuleState(workspacePath)
      if (!resp.success) {
        throw new Error(resp.error || 'Failed to load Pulse status')
      }
      setPulseModuleStates(resp.modules || [])
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to load Pulse status'
      setPulseStatusError(message)
    } finally {
      setPulseStatusLoading(false)
    }
  }, [workspacePath])

  useEffect(() => {
    if (!showMonitorHelp || !monitorOn) return
    refreshPulseModuleStates()
  }, [showMonitorHelp, monitorOn, refreshPulseModuleStates])

  const pulseModuleStateByModule = useMemo(() => {
    return new Map(pulseModuleStates.map(state => [state.module, state]))
  }, [pulseModuleStates])

  const refreshWorkflowScheduleStats = useCallback(async () => {
    if (!workspacePath && !presetQueryId) {
      setWorkflowScheduleStats(EMPTY_WORKFLOW_SCHEDULE_STATS)
      return
    }

    const normalizedWorkspacePath = normalizeWorkspacePath(workspacePath)
    try {
      const resp = await schedulerApi.listJobs({
        entity_type: 'workflow',
        limit: WORKFLOW_SCHEDULE_TOOLBAR_LIMIT,
      })
      const matchingJobs = (resp.jobs || []).filter((job) => {
        if (presetQueryId && job.preset_query_id === presetQueryId) return true
        if (!normalizedWorkspacePath) return false
        return normalizeWorkspacePath(job.workspace_path) === normalizedWorkspacePath
      })
      setWorkflowScheduleStats({
        total: matchingJobs.length,
        running: matchingJobs.filter(job => job.last_status === 'running').length,
        enabled: matchingJobs.filter(job => job.enabled).length,
        paused: matchingJobs.filter(job => !job.enabled).length,
        missed: matchingJobs.filter(job => job.enabled && (job.missed_run_count ?? 0) > 0).length,
        issues: matchingJobs.filter(job => job.last_status === 'error').length,
      })
    } catch {
      setWorkflowScheduleStats(EMPTY_WORKFLOW_SCHEDULE_STATS)
    }
  }, [workspacePath, presetQueryId])

  useEffect(() => {
    refreshWorkflowScheduleStats()
  }, [refreshWorkflowScheduleStats])

  // Lightweight backup-status poll so the toolbar dot reflects health at a glance.
  const refreshBackupState = useCallback(async () => {
    if (!workspacePath) {
      setBackupState('not_configured')
      return
    }
    try {
      const resp = await agentApi.getWorkflowBackup(workspacePath)
      setBackupState(resp.effective_state || 'not_configured')
    } catch {
      // Leave the last known state; a transient fetch failure shouldn't flip the dot.
    }
  }, [workspacePath])

  useEffect(() => {
    refreshBackupState()
  }, [refreshBackupState])

  const refreshPublishState = useCallback(async () => {
    if (!workspacePath) {
      setPublishState('not_configured')
      return
    }
    try {
      const resp = await agentApi.getWorkflowPublish(workspacePath)
      setPublishState(resp.effective_state || 'not_configured')
    } catch {
      // Leave the last known state.
    }
  }, [workspacePath])

  useEffect(() => {
    refreshPublishState()
  }, [refreshPublishState])

  const closeAllPopups = useCallback(() => {
    setShowLearningsPopup(false)
    setShowKBPopup(false)
    setShowDatabasePopup(false)
    setShowExecutionLogsPopup(false)
    setShowCostsPopup(false)
    setShowBackupPopup(false)
    setShowPublishPopup(false)
    setShowPlanEditsPopup(false)
    setShowWorkflowSchedulesPanel(false)
    setShowMonitorHelp(false)
    setShowAutoImproveHelp(false)
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

  // Per-tab live status (busy/idle/stopped) + Stop now live inside each chat tab
  // pill (see WorkflowChatTabs), so the toolbar no longer renders a status badge.

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
  const scheduleTooltip = useMemo(() => {
    if (workflowScheduleStats.total === 0) return 'No schedules for this workflow'
    if (workflowScheduleStats.running > 0) {
      return `${workflowScheduleStats.running} active schedule${workflowScheduleStats.running === 1 ? '' : 's'}`
    }
    if (workflowScheduleStats.enabled > 0) {
      return `${workflowScheduleStats.enabled} active of ${workflowScheduleStats.total} schedule${workflowScheduleStats.total === 1 ? '' : 's'}`
    }
    return `${workflowScheduleStats.total} paused schedule${workflowScheduleStats.total === 1 ? '' : 's'}`
  }, [workflowScheduleStats])
  const scheduleStatusDotClass = workflowScheduleStats.issues > 0 || workflowScheduleStats.missed > 0
    ? 'bg-red-500'
    : workflowScheduleStats.running > 0
      ? 'bg-green-500'
      : workflowScheduleStats.enabled > 0
        ? 'bg-amber-500'
        : 'bg-muted-foreground/50'

  return (
    <>
    <div className={`
      flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 px-3 py-1.5
      bg-background border-b border-border
      relative z-10
      ${className}
    `}>
      {/* Left side - chat tab strip (grows). Per-tab status dot + Stop live
          inside each tab pill (WorkflowChatTabs), not as a separate badge here. */}
      <div className="flex min-w-0 flex-1 items-center gap-3">
        {chatTabsSlot}
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
          {/* Pulse — opens the Pulse popup. Goal Advisor is now a Pulse-selected
              module, so keep it in the same compact control. */}
          {workspacePath && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => setShowMonitorHelp(true)}
                  className="inline-flex items-center gap-1.5 rounded-lg border border-border bg-background/90 px-2 py-1 text-[11px] font-medium text-muted-foreground shadow-sm backdrop-blur-sm transition-colors hover:bg-muted"
                >
                  <Activity className={`w-3.5 h-3.5 ${monitorOn ? 'text-primary' : ''}`} />
                  <span className={monitorOn ? 'text-foreground' : ''}>Pulse</span>
                  <span className={`text-[10px] font-semibold tracking-wide ${monitorOn ? 'text-primary' : 'text-muted-foreground/60'}`}>{monitorOn ? 'ON' : 'OFF'}</span>
                  <span className="mx-0.5 h-3.5 w-px bg-border" />
                  <Sparkles className={`w-3.5 h-3.5 ${monitorOn ? 'text-primary' : ''}`} />
                  <span className="hidden sm:inline">Advisor</span>
                  <span className={`text-[10px] font-semibold tracking-wide ${monitorOn ? 'text-primary' : 'text-muted-foreground/60'}`}>
                    {monitorOn ? 'GATED' : 'OFF'}
                  </span>
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom"><p>Pulse — includes Goal Advisor when Pulse Gate decides strategy review is due</p></TooltipContent>
            </Tooltip>
          )}

          {workspacePath && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => setShowWorkflowSchedulesPanel(true)}
                  className={`relative p-1.5 rounded-md transition-colors ${
                    workflowScheduleStats.issues > 0 || workflowScheduleStats.missed > 0
                      ? 'bg-muted text-red-600 hover:bg-accent dark:text-red-400'
                      : workflowScheduleStats.running > 0
                        ? 'bg-muted text-green-600 hover:bg-accent dark:text-green-400'
                        : 'bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                  }`}
                  aria-label={scheduleTooltip}
                >
                  <CalendarClock className="w-3.5 h-3.5" />
                  {workflowScheduleStats.total > 0 && (
                    <span className={`absolute right-1 top-1 h-1.5 w-1.5 rounded-full ${scheduleStatusDotClass}`} />
                  )}
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom"><p>{scheduleTooltip}</p></TooltipContent>
            </Tooltip>
          )}

          {/* Backup - dedicated remote backup status + strategy; status dot reflects health */}
          {workspacePath && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={() => setShowBackupPopup(true)}
                  className="relative p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
                >
                  <Cloud className="w-3.5 h-3.5" />
                  <span
                    className={`absolute -top-0.5 -right-0.5 h-2 w-2 rounded-full border border-background ${getBackupDotClass(backupState)}`}
                  />
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom"><p>Backup &middot; {formatBackupStateLabel(backupState)}</p></TooltipContent>
            </Tooltip>
          )}

          {/* Publish - share the workflow's HTML (Pulse + report) to a public URL; dot reflects publish state */}
          {workspacePath && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={() => setShowPublishPopup(true)}
                  className="relative p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
                >
                  <Globe className="w-3.5 h-3.5" />
                  <span
                    className={`absolute -top-0.5 -right-0.5 h-2 w-2 rounded-full border border-background ${getPublishDotClass(publishState)}`}
                  />
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom"><p>Publish &middot; {formatPublishStateLabel(publishState)}</p></TooltipContent>
            </Tooltip>
          )}

          {/* Plan edits - the per-change audit trail (planning/changelog): tool · reason · field diffs */}
          {workspacePath && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={() => setShowPlanEditsPopup(true)}
                  className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
                >
                  <GitCommitVertical className="w-3.5 h-3.5" />
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom"><p>Plan edits</p></TooltipContent>
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

        {/* Show Learnings - opens popup with learning metadata (read-only safe) */}
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

        {/* Show Database - durable db/db.sqlite tables used by report widgets and steps */}
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

        {/* Workflow Access (multi-user mode only, owners only) */}
        {canManageAccess && (
          <Tooltip>
            <TooltipTrigger asChild>
              <button
                onClick={() => setShowAccessPopup(true)}
                className="p-1.5 rounded-md bg-muted text-muted-foreground hover:bg-accent hover:text-accent-foreground transition-colors"
              >
                <ShieldCheck className="w-3.5 h-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent side="bottom"><p>Automation Access</p></TooltipContent>
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

    {showWorkflowSchedulesPanel && (
      <WorkflowScheduleRunsPanel
        workflowScope={{
          presetQueryId,
          workspacePath,
          label: workflowScheduleLabel,
        }}
        onClose={() => {
          setShowWorkflowSchedulesPanel(false)
          refreshWorkflowScheduleStats()
        }}
      />
    )}

    {/* Pulse help */}
    {showMonitorHelp && (
      <ModalPortal>
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={() => setShowMonitorHelp(false)}>
          <div className="max-h-[calc(100vh-2rem)] w-full max-w-2xl overflow-y-auto rounded-lg border bg-background shadow-xl" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between border-b px-5 py-3.5">
              <div className="flex items-center gap-2">
                <Activity className="h-4 w-4 text-primary" />
                <h2 className="text-sm font-semibold">Pulse</h2>
              </div>
              <button onClick={() => setShowMonitorHelp(false)} className="rounded-md p-1 hover:bg-accent" aria-label="Close">
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="space-y-3 px-5 py-4 text-sm text-muted-foreground">
              <p>When <span className="font-medium text-foreground">on</span>, your workflow looks after itself after <span className="font-medium text-foreground">every run</span> and records what it does in the <span className="font-medium text-foreground">Pulse</span> log — so you catch problems the moment they happen, not days later.</p>
              <p className="text-foreground font-medium">Each run it:</p>
              <ul className="space-y-1.5 pl-1">
                <li><span className="font-medium text-foreground">Backs up</span> — saves the workflow first, so any change is reversible.</li>
                <li><span className="font-medium text-foreground">Reviews</span> — <span className="font-medium text-red-600 dark:text-red-400">Bug</span> (did it run correctly?) and <span className="font-medium text-purple-600 dark:text-purple-400">Goal</span> (is it hitting its success criteria?).</li>
                <li><span className="font-medium text-foreground">Fixes the small stuff</span> — applies low-risk repairs for Bugs; flags bigger plan changes as proposals.</li>
                <li><span className="font-medium text-foreground">Notifies</span> — sends a compact run summary, with stronger wording when something broke or recovered.</li>
              </ul>
              <p>If Publish is set up, it also re-publishes your report so the shared link stays current. Bigger strategy questions are handled by the Goal Advisor module when Pulse Gate decides enough evidence exists.</p>
            </div>
            {monitorOn && (
              <div className="border-t px-5 py-4">
                <div className="mb-3 flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="text-sm font-medium text-foreground">Command status</div>
                    <div className="text-xs text-muted-foreground">
                      Latest Pulse Gate state from this workflow&apos;s <code className="rounded bg-muted px-1 py-0.5">db/db.sqlite</code>
                    </div>
                  </div>
                  <button
                    type="button"
                    onClick={() => { void refreshPulseModuleStates() }}
                    disabled={pulseStatusLoading}
                    className="inline-flex items-center gap-1.5 rounded-md border px-2 py-1 text-xs text-muted-foreground hover:bg-muted disabled:opacity-60"
                  >
                    <RefreshCw className={`h-3.5 w-3.5 ${pulseStatusLoading ? 'animate-spin' : ''}`} />
                    Refresh
                  </button>
                </div>
                {pulseStatusError && (
                  <div className="mb-3 rounded-md border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-300">
                    {pulseStatusError}
                  </div>
                )}
                <div className="max-h-[320px] overflow-y-auto rounded-lg border bg-muted/20">
                  {PULSE_MODULE_COMMANDS.map((command) => {
                    const state = pulseModuleStateByModule.get(command.id)
                    const status = getPulseModuleStatus(state)
                    return (
                      <div key={command.id} className="grid grid-cols-[minmax(0,1fr)_auto] gap-3 border-b px-3 py-2.5 last:border-b-0">
                        <div className="min-w-0">
                          <div className="flex min-w-0 items-center gap-2">
                            <span className="truncate text-sm font-medium text-foreground">{command.label}</span>
                            {state?.last_pulse_run_id && (
                              <span className="shrink-0 rounded bg-background px-1.5 py-0.5 text-[10px] text-muted-foreground">
                                {state.last_pulse_run_id}
                              </span>
                            )}
                          </div>
                          <div className="mt-0.5 truncate text-xs text-muted-foreground">
                            {status.detail || command.description}
                          </div>
                        </div>
                        <div className="flex shrink-0 flex-col items-end gap-1">
                          <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold tracking-wide ${pulseStatusToneClass(status.label)}`}>
                            {status.label}
                          </span>
                          <span className="text-[10px] text-muted-foreground">{status.time}</span>
                        </div>
                      </div>
                    )
                  })}
                  <div className="border-t bg-background/40 px-3 py-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                    Final commands
                  </div>
                  {PULSE_FIXED_COMMANDS.map((command) => (
                    <div key={command.id} className="grid grid-cols-[minmax(0,1fr)_auto] gap-3 border-t px-3 py-2.5 first:border-t-0">
                      <div className="min-w-0">
                        <div className="truncate text-sm font-medium text-foreground">{command.label}</div>
                        <div className="mt-0.5 truncate text-xs text-muted-foreground">{command.description}</div>
                      </div>
                      <div className="flex shrink-0 items-start">
                        <span className={`rounded-full border px-2 py-0.5 text-[10px] font-semibold tracking-wide ${pulseStatusToneClass(command.status)}`}>
                          {command.status}
                        </span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
            {/* enable / disable */}
            <div className="flex items-center justify-between border-t px-5 py-3.5">
              <div>
                <div className="text-sm font-medium text-foreground">Pulse</div>
                <div className="text-xs text-muted-foreground">{monitorOn ? 'On — reviewing every run' : 'Off — not reviewing runs'}</div>
              </div>
              <button
                type="button"
                role="switch"
                aria-checked={monitorOn}
                onClick={() => { void toggleMonitor() }}
                disabled={monitorSaving}
                className={`relative inline-flex h-5 w-9 flex-none items-center rounded-full p-0 transition-colors disabled:opacity-50 ${monitorOn ? 'bg-primary' : 'bg-muted-foreground/30'}`}
                aria-label="Toggle Pulse"
              >
                <span className={`inline-block h-4 w-4 rounded-full bg-white shadow-sm transition-transform ${monitorOn ? 'translate-x-[18px]' : 'translate-x-[2px]'}`} />
              </button>
            </div>
            {/* scheduling note */}
            <div className="border-t px-5 py-4">
              <p className="rounded-md bg-muted/60 px-3 py-2.5 text-xs text-muted-foreground">
                <span className="font-medium text-foreground">To run on a schedule:</span> Use <code className="rounded bg-background px-1 py-0.5 font-medium text-foreground">/goal-advisor</code> to enable Pulse and set the recurring run schedule. Pulse Gate decides which deeper modules run after each run.
              </p>
            </div>
          </div>
        </div>
      </ModalPortal>
    )}

    {/* Goal Advisor help */}
    {showAutoImproveHelp && (
      <ModalPortal>
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={() => setShowAutoImproveHelp(false)}>
          <div className="w-full max-w-md rounded-lg border bg-background shadow-xl" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between border-b px-5 py-3.5">
              <div className="flex items-center gap-2">
                <Sparkles className="h-4 w-4 text-primary" />
                <h2 className="text-sm font-semibold">Goal Advisor</h2>
              </div>
              <button onClick={() => setShowAutoImproveHelp(false)} className="rounded-md p-1 hover:bg-accent" aria-label="Close">
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="space-y-3 px-5 py-4 text-sm text-muted-foreground">
              <p>Goal Advisor is the strategic review module inside Pulse. Pulse Gate runs it when real run/eval evidence suggests the goal is not moving, even when the workflow itself runs cleanly.</p>
              <p className="text-foreground font-medium">When selected, it can:</p>
              <ul className="space-y-1.5 pl-1">
                <li><span className="font-medium text-foreground">Challenge strategy</span> — compare the current plan against the workflow's success criteria.</li>
                <li><span className="font-medium text-foreground">Find blind spots</span> — propose out-of-plan ideas an expert operator would consider.</li>
                <li><span className="font-medium text-foreground">Apply only when proven</span> — replan only with strong cross-run evidence; otherwise log proposals or ask you.</li>
                <li><span className="font-medium text-foreground">Publish the decision</span> — update the Pulse log and org progress card, then let Pulse backup/publish/notify run as separate turns.</li>
              </ul>
            </div>
            <div className="flex items-center justify-between border-t px-5 py-3.5">
              <div>
                <div className="text-sm font-medium text-foreground">Goal Advisor</div>
                <div className="text-xs text-muted-foreground">
                  {monitorOn
                    ? 'Pulse Gate will run it when strategy review is due'
                    : 'Off — enable Pulse to use the Goal Advisor module'}
                </div>
              </div>
              <span className={`rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide ${monitorOn ? 'bg-primary/10 text-primary' : 'bg-muted text-muted-foreground'}`}>
                {monitorOn ? 'GATED' : 'OFF'}
              </span>
            </div>
            {autoImproveSchedules.length > 0 && (
              <div className="border-t px-5 py-4 text-xs text-muted-foreground">
                <div className="mb-2 font-medium text-foreground">Legacy optimizer schedule{autoImproveSchedules.length === 1 ? '' : 's'}</div>
                <ul className="space-y-1">
                  {autoImproveSchedules.slice(0, 3).map((schedule) => (
                    <li key={schedule.id} className="flex items-center justify-between gap-3">
                      <span className="truncate">{schedule.name || 'Unnamed schedule'}</span>
                      <span className={schedule.enabled ? 'text-primary' : 'text-muted-foreground'}>{schedule.enabled ? 'enabled' : 'paused'}</span>
                    </li>
                  ))}
                </ul>
                {autoImproveSchedules.length > 3 && (
                  <div className="mt-1 text-muted-foreground/80">+{autoImproveSchedules.length - 3} more</div>
                )}
              </div>
            )}
            <div className="border-t px-5 py-4">
              <p className="rounded-md bg-muted/60 px-3 py-2.5 text-xs text-muted-foreground">
                Goal Advisor now runs as a Pulse-selected module. Use <code className="rounded bg-background px-1 py-0.5 font-medium text-foreground">/goal-advisor</code> in workflow chat to enable Pulse and set the recurring run schedule.
              </p>
            </div>
          </div>
        </div>
      </ModalPortal>
    )}

    {/* Backup Popup (dedicated remote backup status + strategy) */}
    <WorkflowBackupPopup
      isOpen={showBackupPopup}
      onClose={() => { setShowBackupPopup(false); refreshBackupState() }}
      workspacePath={workspacePath || null}
      onStateLoaded={setBackupState}
    />

    {/* Publish Popup (share HTML to a public URL) */}
    <WorkflowPublishPopup
      isOpen={showPublishPopup}
      onClose={() => { setShowPublishPopup(false); refreshPublishState() }}
      workspacePath={workspacePath || null}
      onStateLoaded={setPublishState}
    />

    {/* Plan edits Popup (planning/changelog audit trail) */}
    {showPlanEditsPopup && workspacePath && (
      <ModalPortal>
        <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 p-2 backdrop-blur-sm sm:p-4" onClick={() => setShowPlanEditsPopup(false)}>
          <div className="relative flex max-h-[calc(100dvh-1rem)] w-full max-w-2xl flex-col rounded-lg border border-border bg-background shadow-xl sm:max-h-[86vh]" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
              <div className="flex items-center gap-2">
                <GitCommitVertical className="h-4.5 w-4.5 text-primary" />
                <h2 className="text-sm font-semibold text-foreground">Plan edits</h2>
              </div>
              <button onClick={() => setShowPlanEditsPopup(false)} className="rounded-md p-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground" aria-label="Close">
                <X className="h-4 w-4" />
              </button>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto">
              <PlanChangelogFeed workspacePath={workspacePath} />
            </div>
          </div>
        </div>
      </ModalPortal>
    )}

    {/* Workflow Access Popup (multi-user owners only) */}
    <WorkflowAccessPopup
      isOpen={showAccessPopup}
      onClose={() => setShowAccessPopup(false)}
    />
    </>
  )
}

WorkflowToolbar.whyDidYouRender = true

export default WorkflowToolbar
