import React, { useEffect, useRef, useMemo, useCallback, useState } from 'react'
import { useShallow } from 'zustand/react/shallow'
import {
  Cloud,
  Globe,
  ShieldCheck,
  Activity,
  BellRing,
  CalendarClock,
  ChevronDown,
  ChevronRight,
  Gauge,
  LayoutDashboard,
} from 'lucide-react'
import { useWorkflowStore, type RunFolder } from '../../../stores/useWorkflowStore'
import { PRIMARY_WORKSPACE_TOOLBAR_VIEWS, WORKSPACE_VIEWS, type WorkspaceViewId } from '../workspaceViews'
import { useChatStore } from '../../../stores/useChatStore'
import { useAuthStore } from '../../../stores/useAuthStore'
import type { ScheduledJob, VariablesManifest } from '../../../services/api-types'
import type { PlanningResponse } from '../../../utils/stepConfigMatching'
import type { WorkflowExecutionStatus } from '../hooks/useWorkflowExecution'
import type { ExecutionOptions } from '../../../services/api-types'
import { agentApi } from '../../../services/api'
import { schedulerApi } from '../../../api/scheduler'
import { getBackupDotClass } from '../backupStatus'
import { getPublishDotClass } from '../publishStatus'
import { getNotificationDotClass } from '../notificationStatus'
import { loadWorkflowNotificationInfo, type WorkflowNotificationState } from '../../../services/workflow-notifications'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '../../ui/tooltip'
import { hasWorkflowOwnerAccess } from '../../../utils/workflowPermissions'
import { usePendingDecisionCount } from '../hooks/usePendingDecisionCount'
import { useCanWriteWorkflow } from '../../../hooks/useCanWriteWorkflow'

// Execution phase ID - special phase that should be displayed separately
const EXECUTION_PHASE_ID = 'execution'
const WORKFLOW_SCHEDULE_TOOLBAR_LIMIT = 10_000

// Product-tour / test hooks on specific toolbar buttons. Kept here rather
// than in the view registry because they describe this toolbar's buttons,
// not the views themselves.
// Report and Pulse are always visible. Other workspace tools are grouped
// under Views; configuration stays under Setup. Which expandable
// group is open is a per-browser preference shared by all workflows.
const TOOLBAR_OPEN_GROUPS_KEY = 'workflow-toolbar-open-groups'
type ToolbarGroupId = 'views' | 'setup'
const readOpenGroups = (): Record<ToolbarGroupId, boolean> => {
  const fallback: Record<ToolbarGroupId, boolean> = { views: false, setup: false }
  try {
    const raw = localStorage.getItem(TOOLBAR_OPEN_GROUPS_KEY)
    if (!raw) return fallback
    const stored = JSON.parse(raw) as Partial<Record<ToolbarGroupId | 'pulse', boolean>>
    // Preserve the expanded state of the former Pulse group as Views.
    // Setup wins if an older malformed
    // preference has more than one group open.
    if (stored.setup) return { views: false, setup: true }
    if (stored.pulse || stored.views) return { views: true, setup: false }
    return fallback
  } catch {
    return fallback
  }
}

function ToolbarGroup({ label, open, onToggle, title, children, ...rest }: {
  label: string
  open: boolean
  onToggle: () => void
  title: string
  children: React.ReactNode
} & Record<`data-${string}`, string | undefined>) {
  return (
    <div {...rest} className="inline-flex h-full items-center gap-0.5 px-1 first:pl-0.5 last:pr-0.5">
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={open}
        title={title}
        className={`inline-flex h-6 items-center gap-1 rounded px-2 text-[11px] font-medium outline-none transition-colors hover:bg-background/70 ${open ? 'text-foreground' : 'text-muted-foreground hover:text-foreground'}`}
      >
        <span>{label}</span>
        {open ? <ChevronDown className="h-3 w-3" aria-hidden="true" /> : <ChevronRight className="h-3 w-3" aria-hidden="true" />}
      </button>
      {open && children}
    </div>
  )
}

const CAPABILITY_BUTTON_ATTRS: Partial<Record<WorkspaceViewId, { 'data-tour': string; 'data-testid': string }>> = {
  bots: { 'data-tour': 'bot-connector', 'data-testid': 'tour-bot-connector' },
}

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

interface WorkflowToolbarProps {
  status: WorkflowExecutionStatus
  plan?: PlanningResponse | null  // Plan data used by toolbar actions
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
  onExport?: () => void
  // Chat tab strip (WorkflowChatTabs) rendered inline on the left of this bar so the
  // workflow chat tabs + new-chat share one row with the status/tools instead of
  // sitting in a separate bar below.
  chatTabsSlot?: React.ReactNode
  // Whether Pulse review is enabled for this workflow -- owned by the host
  // (shared with the pane's PulseView), just read here for the badge.
  monitorOn: boolean
  className?: string
}

export const WorkflowToolbar: React.FC<WorkflowToolbarProps> = ({
  status,
  workspacePath,
  presetQueryId,
  variablesManifest,
  isLoadingWorkspaceState = false,
  chatTabsSlot,
  monitorOn,
  className = ''
}) => {
  const pendingDecisionCount = usePendingDecisionCount(workspacePath)
  const canWriteWorkflow = useCanWriteWorkflow(workspacePath)
  const canManageAccess = useAuthStore(state => state.isMultiUserMode && (state.user?.is_admin === true || hasWorkflowOwnerAccess(state.user, state.isMultiUserMode)))

  // Workspace store for opening folders

  // Workflow store - use useShallow to prevent unnecessary re-renders
  // Note: runFolders, variablesManifest come from props (passed from WorkflowCanvas)
  const {
    selectedRunFolder,
    selectedGroupIds,
    currentRunningGroupId,
    loadSavedSettings,
    setSelectedGroupIds,
    restoreSelectionFromLocalStorage,
  } = useWorkflowStore(useShallow(state => ({
    selectedRunFolder: state.selectedRunFolder,
    selectedGroupIds: state.selectedGroupIds,
    currentRunningGroupId: state.currentRunningGroupId,
    loadSavedSettings: state.loadSavedSettings,
    setSelectedGroupIds: state.setSelectedGroupIds,
    restoreSelectionFromLocalStorage: state.restoreSelectionFromLocalStorage,
  })))

  const workflowWorkspaceView = useWorkflowStore(state => state.workflowWorkspaceView)
  const lastCanvasView = useWorkflowStore(state => state.lastCanvasView)
  const openWorkspaceView = useWorkflowStore(state => state.openWorkspaceView)

  // No explicit view means the pane is on whichever canvas view was last open.
  const activeWorkspaceView: WorkspaceViewId = workflowWorkspaceView ?? lastCanvasView

  // Button clusters come from the view registry, in registry order. Plan is
  // always present, including for a new workflow with no steps yet.
  const workspaceViewDefinitions = PRIMARY_WORKSPACE_TOOLBAR_VIEWS.filter(view => view.id !== 'report')
  const capabilityViewDefinitions = useMemo(
    () => WORKSPACE_VIEWS.filter(view => view.toolbarGroup === 'capabilities'),
    [],
  )

  // Backup/publish/notify status dots -- lightweight polls independent of
  // whether the pane is showing that view.
  const [backupState, setBackupState] = useState<string>('loading')
  const [publishState, setPublishState] = useState<string>('not_configured')
  const [notificationState, setNotificationState] = useState<WorkflowNotificationState | 'loading'>('loading')
  // Share is for this workflow's owners (or an admin), multi-user mode only.
  const isMultiUser = useAuthStore(state => state.isMultiUserMode)
  const [workflowScheduleStats, setWorkflowScheduleStats] = useState<WorkflowScheduleStats>(EMPTY_WORKFLOW_SCHEDULE_STATS)
  const [openGroups, setOpenGroups] = useState<Record<ToolbarGroupId, boolean>>(() => readOpenGroups())
  // One group open at a time. Opening one closes the other; clicking the open
  // one closes it, so both collapsed is a valid state.
  const toggleGroup = useCallback((group: ToolbarGroupId) => {
    setOpenGroups(current => {
      const opening = !current[group]
      const next = { views: false, setup: false } as Record<ToolbarGroupId, boolean>
      if (opening) next[group] = true
      try { localStorage.setItem(TOOLBAR_OPEN_GROUPS_KEY, JSON.stringify(next)) } catch { /* preference only */ }
      return next
    })
  }, [])

  const updateWorkflowScheduleStats = useCallback((jobs: ScheduledJob[]) => {
    const normalizedWorkspacePath = normalizeWorkspacePath(workspacePath)
    const matchingJobs = jobs.filter((job) => {
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
  }, [workspacePath, presetQueryId])

  const refreshWorkflowScheduleStats = useCallback(async () => {
    if (!workspacePath && !presetQueryId) {
      setWorkflowScheduleStats(EMPTY_WORKFLOW_SCHEDULE_STATS)
      return
    }

    try {
      const resp = await schedulerApi.listJobs({
        entity_type: 'workflow',
        limit: WORKFLOW_SCHEDULE_TOOLBAR_LIMIT,
      })
      updateWorkflowScheduleStats(resp.jobs || [])
    } catch {
      setWorkflowScheduleStats(EMPTY_WORKFLOW_SCHEDULE_STATS)
    }
  }, [workspacePath, presetQueryId, updateWorkflowScheduleStats])

  useEffect(() => {
    void refreshWorkflowScheduleStats()
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
    setBackupState('loading')
    void refreshBackupState()
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

  const refreshNotificationState = useCallback(async () => {
    if (!workspacePath) {
      setNotificationState('not_configured')
      return
    }
    try {
      const info = await loadWorkflowNotificationInfo(workspacePath)
      setNotificationState(info.effectiveState)
    } catch {
      setNotificationState('not_configured')
    }
  }, [workspacePath])

  useEffect(() => {
    void refreshNotificationState()
  }, [refreshNotificationState])

  // Backup/publish/notify each load richer status than this toolbar's own
  // lightweight poll while their pane view is open; catch the dot up once the
  // user navigates away, rather than leaving it stale until workspacePath
  // next changes.
  const prevWorkspaceViewRef = useRef(workflowWorkspaceView)
  useEffect(() => {
    const prev = prevWorkspaceViewRef.current
    if (prev !== workflowWorkspaceView) {
      if (prev === 'backup') void refreshBackupState()
      if (prev === 'publish') void refreshPublishState()
      if (prev === 'notify') void refreshNotificationState()
    }
    prevWorkspaceViewRef.current = workflowWorkspaceView
  }, [workflowWorkspaceView, refreshBackupState, refreshPublishState, refreshNotificationState])

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

  const scheduleTooltip = useMemo(() => {
    if (workflowScheduleStats.total === 0) return 'Schedules · None configured'
    if (workflowScheduleStats.issues > 0 || workflowScheduleStats.missed > 0) {
      const parts: string[] = []
      if (workflowScheduleStats.issues > 0) {
        parts.push(`${workflowScheduleStats.issues} failed`)
      }
      if (workflowScheduleStats.missed > 0) {
        parts.push(`${workflowScheduleStats.missed} missed`)
      }
      if (workflowScheduleStats.running > 0) {
        parts.push(`${workflowScheduleStats.running} running`)
      }
      return `Schedules · ${parts.join(' · ')}`
    }
    if (workflowScheduleStats.running > 0) {
      return `Schedules · ${workflowScheduleStats.running} running`
    }
    if (workflowScheduleStats.enabled > 0) {
      return `Schedules · ${workflowScheduleStats.enabled} enabled`
    }
    return `Schedules · ${workflowScheduleStats.total === 1 ? 'Paused' : `All ${workflowScheduleStats.total} paused`}`
  }, [workflowScheduleStats])
  const scheduleStatusDotClass = workflowScheduleStats.issues > 0
    ? 'bg-red-500'
    : workflowScheduleStats.missed > 0
        ? 'bg-amber-500'
        : workflowScheduleStats.running > 0
          ? 'bg-sky-500 animate-pulse'
          : workflowScheduleStats.enabled > 0
            ? 'bg-emerald-500'
            : 'bg-muted-foreground/40'

  return (
    <>
    <div className={`
      flex min-h-10 min-w-0 flex-nowrap items-center gap-3 overflow-visible px-3 py-1.5
      bg-background border-b border-border
      relative z-10
      ${className}
    `}>
      {/* Left side - chat tab strip (grows). Per-tab status dot + Stop live
          inside each tab pill (WorkflowChatTabs), not as a separate badge here.
          No separate "New Chat" action here: the Builder tab (WorkflowChatTabs)
          is a permanent, always-first, never-closed tab -- it IS that action. */}
      <div className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
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
          {/* Report and Pulse are primary views, always visible outside the groups. */}
          {workspacePath && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => openWorkspaceView('report')}
                  className={`flex h-8 w-8 items-center justify-center rounded-lg border border-border transition-colors ${activeWorkspaceView === 'report' ? 'bg-muted text-foreground shadow-sm' : 'bg-muted/60 text-muted-foreground hover:bg-muted hover:text-foreground'}`}
                  aria-label="Report"
                  aria-pressed={activeWorkspaceView === 'report'}
                >
                  <LayoutDashboard className="h-3.5 w-3.5" />
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom"><p>Report</p></TooltipContent>
            </Tooltip>
          )}

          {workspacePath && (
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  type="button"
                  onClick={() => openWorkspaceView('pulse')}
                  className={`relative flex h-8 w-8 items-center justify-center rounded-lg border border-border transition-colors ${activeWorkspaceView === 'pulse' ? 'bg-muted text-foreground shadow-sm' : 'bg-muted/60 text-muted-foreground hover:bg-muted hover:text-foreground'}`}
                  aria-label={pendingDecisionCount > 0 ? `Pulse, ${pendingDecisionCount} pending ${pendingDecisionCount === 1 ? 'decision' : 'decisions'}` : 'Pulse'}
                  aria-pressed={activeWorkspaceView === 'pulse'}
                >
                  <Activity
                    aria-hidden="true"
                    className={`h-3.5 w-3.5 ${pendingDecisionCount > 0 ? 'pulse-decision-heartbeat text-amber-500' : monitorOn ? 'text-primary' : ''}`}
                  />
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom"><p>{pendingDecisionCount > 0 ? `Pulse · ${pendingDecisionCount} ${pendingDecisionCount === 1 ? 'decision needs' : 'decisions need'} your input` : 'Pulse'}</p></TooltipContent>
            </Tooltip>
          )}

          {/* One continuous pill: Views | Setup, separated by a divider. */}
          {(workspacePath || canWriteWorkflow) && (
          <div className="inline-flex h-8 items-center divide-x divide-border rounded-lg border border-border bg-muted/60 py-0.5 shadow-sm">
          {/* Supporting workspace views and operations. */}
          {workspacePath && (
            <ToolbarGroup
              label="Views"
              open={openGroups.views}
              onToggle={() => toggleGroup('views')}
              title={openGroups.views ? 'Hide views' : 'Show views: plan, evidence, data, browser, schedules, files and operations'}
            >
              <div className="inline-flex items-center gap-0.5">
                {workspaceViewDefinitions.map(({ id: view, icon: Icon, label }) => {
                  const active = view === activeWorkspaceView
                  const viewButton = (
                    <button
                      type="button"
                      onClick={() => openWorkspaceView(view)}
                      className={`flex h-6 w-7 items-center justify-center rounded transition-colors ${active ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'}`}
                      aria-label={label}
                      aria-pressed={active}
                    >
                      <Icon className="h-3.5 w-3.5" />
                    </button>
                  )
                  if (active) return <React.Fragment key={view}>{viewButton}</React.Fragment>
                  return (
                    <Tooltip key={view}>
                      <TooltipTrigger asChild>{viewButton}</TooltipTrigger>
                      <TooltipContent side="bottom"><p>{label}</p></TooltipContent>
                    </Tooltip>
                  )
                })}
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      onClick={() => openWorkspaceView('evaluation')}
                      className={`flex h-6 w-7 items-center justify-center rounded transition-colors ${activeWorkspaceView === 'evaluation' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'}`}
                      aria-label="Evaluation"
                      aria-pressed={activeWorkspaceView === 'evaluation'}
                    >
                      <Gauge className="h-3.5 w-3.5" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom"><p>Evaluation results</p></TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      onClick={() => openWorkspaceView('schedules')}
                      className={`relative flex h-6 w-7 items-center justify-center rounded transition-colors ${activeWorkspaceView === 'schedules' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'}`}
                      aria-label="Schedules"
                      aria-pressed={activeWorkspaceView === 'schedules'}
                    >
                      <CalendarClock className="h-3.5 w-3.5" />
                      <span className={`absolute right-1.5 top-1.5 h-1.5 w-1.5 rounded-full border border-background ${scheduleStatusDotClass}`} />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom"><p>{scheduleTooltip}</p></TooltipContent>
                </Tooltip>
              </div>
              <span className="mx-0.5 h-4 w-px bg-border" aria-hidden="true" />
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    onClick={() => openWorkspaceView('backup')}
                    className="relative flex h-6 w-7 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-background/70 hover:text-foreground"
                    aria-label="Backup"
                  >
                    <Cloud className="h-3.5 w-3.5" />
                    <span className={`absolute right-1.5 top-1.5 h-1.5 w-1.5 rounded-full border border-background ${getBackupDotClass(backupState)}`} />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom"><p>Backup</p></TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    onClick={() => openWorkspaceView('publish')}
                    className="relative flex h-6 w-7 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-background/70 hover:text-foreground"
                    aria-label="Publish"
                  >
                    <Globe className="h-3.5 w-3.5" />
                    <span className={`absolute right-1.5 top-1.5 h-1.5 w-1.5 rounded-full border border-background ${getPublishDotClass(publishState)}`} />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom"><p>Publish</p></TooltipContent>
              </Tooltip>
              <Tooltip>
                <TooltipTrigger asChild>
                  <button
                    type="button"
                    data-testid="workflow-notification-settings-button"
                    onClick={() => openWorkspaceView('notify')}
                    className="relative flex h-6 w-7 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-background/70 hover:text-foreground"
                    aria-label="Notify"
                  >
                    <BellRing className="h-3.5 w-3.5" />
                    <span className={`absolute right-1.5 top-1.5 h-1.5 w-1.5 rounded-full border border-background ${getNotificationDotClass(notificationState)}`} />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="bottom"><p>Notify</p></TooltipContent>
              </Tooltip>
            </ToolbarGroup>
          )}

        {/* Workflow capabilities remain discoverable for readers. Each panel
            owns its read-only state and disables mutations in place. */}
        {workspacePath && (
          <ToolbarGroup
            label="Setup"
            open={openGroups.setup}
            onToggle={() => toggleGroup('setup')}
            title={openGroups.setup ? 'Hide setup' : 'Show setup: skills, secrets, MCP servers, LLM, bots, API triggers, folders, sharing, users'}
          >
          <div className="inline-flex items-center gap-0.5">
            {capabilityViewDefinitions.map(({ id, icon: Icon, label }) => {
              const active = workflowWorkspaceView === id
              return (
                <Tooltip key={id}>
                  <TooltipTrigger asChild>
                    <button onClick={() => openWorkspaceView(id)} {...CAPABILITY_BUTTON_ATTRS[id]} className={`flex h-6 w-7 items-center justify-center rounded transition-colors ${active ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'}`} aria-label={label} aria-pressed={active}>
                      <Icon className="w-3.5 h-3.5" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom"><p>{label}</p></TooltipContent>
                </Tooltip>
              )
            })}
            {/* Access lives with the rest of setup: one button, one panel
                with two tabs -- who may see or edit THIS workflow, and (for
                admins) the deployment's accounts, roles and products. */}
            {isMultiUser && workspacePath && (
              <>
                <span className="mx-0.5 h-4 w-px bg-border" aria-hidden="true" />
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      onClick={() => openWorkspaceView('access')}
                      className={`flex h-6 w-7 items-center justify-center rounded transition-colors ${workflowWorkspaceView === 'access' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:bg-background/70 hover:text-foreground'}`}
                      aria-label="Access"
                      aria-pressed={workflowWorkspaceView === 'access'}
                    >
                      <ShieldCheck className="w-3.5 h-3.5" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="bottom"><p>Access: share this workflow{canManageAccess ? ', manage users' : ''}</p></TooltipContent>
                </Tooltip>
              </>
            )}
          </div>
          </ToolbarGroup>
        )}
          </div>
          )}

        </TooltipProvider>
      </div>
    </div>
    {/* Access: this workflow's sharing + (admins) the deployment's users */}
    </>
  )
}

WorkflowToolbar.whyDidYouRender = true

export default WorkflowToolbar
