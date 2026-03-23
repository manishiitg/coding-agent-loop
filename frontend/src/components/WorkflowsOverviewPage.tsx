import React, { useEffect, useState, useCallback } from 'react'
import { Loader2, ChevronLeft, ChevronRight, ChevronDown, FileText, BarChart3, DollarSign, Clock, AlertCircle, X, CheckCircle2, PlayCircle, Circle, Timer, Zap, MessageSquare } from 'lucide-react'
import { agentApi } from '../services/api'
import { usePresetApplication } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { useAppStore } from '../stores/useAppStore'
import { useChatStore } from '../stores/useChatStore'
import ExecutionLogsPopup from './workflow/ExecutionLogsPopup'
import EvaluationPopup from './workflow/EvaluationPopup'
import CostsPopup from './workflow/CostsPopup'
import { EmployeeDashboard } from './EmployeeDashboard'
import ChatArea from './ChatArea'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import type { RunFolderInfo, EvaluationReportsResponse, EvaluationReportEntry, StepProgress, RunMetadataModels } from '../services/api-types'

interface RunFolderDetail {
  folder: RunFolderInfo
  totalSteps: number
  completedSteps: number
  lastUpdated: string | null
  evalScore: number | null
  evalMaxScore: number | null
  costUsd: number | null
  startedAt: string | null
  completedAt: string | null
  triggeredBy: string | null
  status: string // "running", "completed", "unknown"
  models: RunMetadataModels | null
}

interface WorkflowOverviewRow {
  preset: CustomPreset | PredefinedPreset
  workspacePath: string | null
  loading: boolean
  error: string | null
  runFolders: RunFolderDetail[]
  evalData: EvaluationReportsResponse | null
  lastUpdated: string | null
  totalRunCount: number
}

const formatTimestamp = (ts: string): string => {
  try {
    const d = new Date(ts)
    const now = new Date()
    const diffMs = now.getTime() - d.getTime()
    const diffMin = Math.floor(diffMs / 60000)
    const diffHr = Math.floor(diffMs / 3600000)
    const diffDay = Math.floor(diffMs / 86400000)
    if (diffMin < 1) return 'just now'
    if (diffMin < 60) return `${diffMin}m ago`
    if (diffHr < 24) return `${diffHr}h ago`
    if (diffDay < 7) return `${diffDay}d ago`
    return d.toLocaleDateString()
  } catch {
    return ts
  }
}

const formatDateTime = (ts: string): string => {
  try {
    const d = new Date(ts)
    return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  } catch {
    return ts
  }
}

const formatCost = (cost: number): string => {
  if (cost < 0.01) return '<$0.01'
  return `$${cost.toFixed(2)}`
}

const getProgressColor = (completed: number, total: number): string => {
  if (total === 0) return 'bg-gray-300 dark:bg-gray-600'
  const pct = completed / total
  if (pct >= 1) return 'bg-green-500'
  if (pct >= 0.5) return 'bg-blue-500'
  return 'bg-amber-500'
}

const triggerLabel = (t: string | null): string => {
  switch (t) {
    case 'cron': return 'Cron'
    case 'manual': return 'Execute'
    case 'workflow_builder': return 'Builder'
    default: return ''
  }
}

function useWorkflowRows() {
  const [rows, setRows] = useState<WorkflowOverviewRow[]>([])
  const [loading, setLoading] = useState(false)
  const { getPresetsForMode } = usePresetApplication()

  const loadData = useCallback(async () => {
    const presets = getPresetsForMode('workflow')
    if (presets.length === 0) {
      setRows([])
      return
    }

    setLoading(true)

    const initialRows: WorkflowOverviewRow[] = presets.map(preset => ({
      preset,
      workspacePath: preset.selectedFolder?.filepath || null,
      loading: true,
      error: null,
      runFolders: [],
      evalData: null,
      lastUpdated: null,
      totalRunCount: 0,
    }))
    setRows(initialRows)

    const updated = await Promise.all(
      initialRows.map(async (row) => {
        if (!row.workspacePath) {
          return { ...row, loading: false, error: 'No workspace path' }
        }

        try {
          const [wsState, evalResp] = await Promise.all([
            agentApi.loadWorkspaceState(row.workspacePath).catch(() => null),
            agentApi.getEvaluationReports(row.workspacePath).catch(() => null),
          ])

          const folders = wsState?.data?.run_folders || []

          const evalByFolder: Record<string, EvaluationReportEntry> = {}
          if (evalResp?.success && evalResp.reports) {
            for (const entry of evalResp.reports) {
              evalByFolder[entry.run_folder] = entry
            }
          }

          // Build set of truly running run folders from in-memory registry
          const activeRunFolders = new Set<string>()
          if (wsState?.data?.active_executions) {
            for (const exec of wsState.data.active_executions) {
              if (exec.run_folder) activeRunFolders.add(exec.run_folder)
            }
          }

          let latestTime = ''
          const runFolderDetails: RunFolderDetail[] = folders.map(f => {
            const progress: StepProgress | undefined = f.progress
            const totalSteps = progress?.total_steps || 0
            const completedSteps = progress?.completed_step_indices?.length || 0
            const lastUp = progress?.last_updated || null
            if (lastUp && lastUp > latestTime) latestTime = lastUp

            const meta = f.metadata
            let status = meta?.status || (totalSteps > 0 && completedSteps >= totalSteps ? 'completed' : totalSteps > 0 ? 'running' : 'unknown')
            // Reconcile: if metadata says "running" but not in active executions, it's stale
            if (status === 'running' && !activeRunFolders.has(f.name)) {
              // Check if actually completed
              if (totalSteps > 0 && completedSteps >= totalSteps) {
                status = 'completed'
              } else {
                status = 'failed'
              }
            }

            const evalEntry = evalByFolder[f.name]
            let evalScore: number | null = null
            let evalMaxScore: number | null = null
            if (evalEntry) {
              evalScore = evalEntry.report.total_score
              evalMaxScore = evalEntry.report.max_possible_score
            }

            return {
              folder: f,
              totalSteps,
              completedSteps,
              lastUpdated: lastUp,
              evalScore,
              evalMaxScore,
              costUsd: null,
              startedAt: meta?.created_at || null,
              completedAt: meta?.completed_at || null,
              triggeredBy: meta?.triggered_by || null,
              status,
              models: meta?.models || null,
            }
          })

          // Sort by created_at (metadata) first, then lastUpdated as fallback — most recent first
          runFolderDetails.sort((a, b) => {
            const aTime = a.startedAt || a.lastUpdated || ''
            const bTime = b.startedAt || b.lastUpdated || ''
            if (!aTime && !bTime) return 0
            if (!aTime) return 1
            if (!bTime) return -1
            return bTime.localeCompare(aTime)
          })

          return {
            ...row,
            loading: false,
            runFolders: runFolderDetails,
            evalData: evalResp,
            lastUpdated: latestTime || null,
            totalRunCount: folders.length,
          }
        } catch {
          return { ...row, loading: false, error: 'Failed to load' }
        }
      })
    )

    updated.sort((a, b) => {
      if (!a.lastUpdated && !b.lastUpdated) return 0
      if (!a.lastUpdated) return 1
      if (!b.lastUpdated) return -1
      return b.lastUpdated.localeCompare(a.lastUpdated)
    })

    setRows(updated)
    setLoading(false)

    // Fetch costs lazily per run folder
    for (const row of updated) {
      if (!row.workspacePath || row.runFolders.length === 0) continue
      const wp = row.workspacePath
      for (const rf of row.runFolders) {
        agentApi.getCosts(wp, rf.folder.name)
          .then(costResp => {
            if (costResp.success && costResp.token_usage) {
              let sum = 0
              for (const model of Object.values(costResp.token_usage.by_model)) {
                sum += model.total_cost_usd || 0
              }
              rf.costUsd = sum > 0 ? sum : null
              // Use token_usage timestamps as fallback if no metadata
              if (!rf.startedAt && costResp.token_usage.created_at) {
                rf.startedAt = costResp.token_usage.created_at
              }
              if (!rf.completedAt && rf.status === 'completed' && costResp.token_usage.updated_at) {
                rf.completedAt = costResp.token_usage.updated_at
              }
              setRows(prev => [...prev])
            }
          })
          .catch(() => {})
      }
    }
  }, [getPresetsForMode])

  return { rows, loading, loadData }
}

// Status badge component
const StatusBadge: React.FC<{ status: string; completedSteps: number; totalSteps: number }> = ({ status, completedSteps, totalSteps }) => {
  if (status === 'failed') {
    return (
      <div className="flex items-center gap-1.5">
        <AlertCircle className="w-3.5 h-3.5 text-red-500 dark:text-red-400" />
        <span className="text-xs font-medium text-red-600 dark:text-red-400">Failed</span>
      </div>
    )
  }
  if (status === 'completed' || (totalSteps > 0 && completedSteps >= totalSteps)) {
    return (
      <div className="flex items-center gap-1.5">
        <CheckCircle2 className="w-3.5 h-3.5 text-green-500 dark:text-green-400" />
        <span className="text-xs font-medium text-green-600 dark:text-green-400">Completed</span>
      </div>
    )
  }
  if (status === 'running' || (totalSteps > 0 && completedSteps > 0)) {
    return (
      <div className="flex items-center gap-2">
        <PlayCircle className="w-3.5 h-3.5 text-blue-500 dark:text-blue-400" />
        <div className="flex items-center gap-1.5">
          <div className="w-12 h-1.5 bg-gray-200 dark:bg-gray-600 rounded-full overflow-hidden">
            <div
              className={`h-full rounded-full ${getProgressColor(completedSteps, totalSteps)}`}
              style={{ width: `${totalSteps > 0 ? (completedSteps / totalSteps) * 100 : 0}%` }}
            />
          </div>
          <span className="text-xs text-gray-500 dark:text-gray-400">{completedSteps}/{totalSteps}</span>
        </div>
      </div>
    )
  }
  return (
    <div className="flex items-center gap-1.5">
      <Circle className="w-3 h-3 text-gray-400 dark:text-gray-500" />
      <span className="text-xs text-gray-400 dark:text-gray-500">Not started</span>
    </div>
  )
}

// Model summary - shows compact model info
const ModelSummary: React.FC<{ models: RunMetadataModels | null }> = ({ models }) => {
  if (!models) return null
  const parts: string[] = []
  if (models.allocation_mode === 'tiered') {
    if (models.tier_1?.model_id) parts.push(`T1: ${models.tier_1.model_id.split('/').pop()}`)
    if (models.tier_2?.model_id) parts.push(`T2: ${models.tier_2.model_id.split('/').pop()}`)
    if (models.tier_3?.model_id) parts.push(`T3: ${models.tier_3.model_id.split('/').pop()}`)
  } else {
    if (models.execution_llm?.model_id) parts.push(models.execution_llm.model_id.split('/').pop() || '')
    else if (models.tier_1?.model_id) parts.push(models.tier_1.model_id.split('/').pop() || '')
  }
  if (models.temp_override?.model_id) parts.push(`override: ${models.temp_override.model_id.split('/').pop()}`)
  if (parts.length === 0) return null
  return (
    <span className="text-[10px] text-gray-400 dark:text-gray-500 truncate max-w-[200px] block" title={parts.join(', ')}>
      {parts.join(' · ')}
    </span>
  )
}

// Trigger badge
const TriggerBadge: React.FC<{ triggeredBy: string | null }> = ({ triggeredBy }) => {
  if (!triggeredBy) return null
  const label = triggerLabel(triggeredBy)
  if (!label) return null
  return (
    <span className="inline-flex items-center gap-1 px-1.5 py-0.5 text-[10px] font-medium rounded bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400">
      <Zap className="w-2.5 h-2.5" />
      {label}
    </span>
  )
}

// Sub-row for a single run folder
const RunFolderRow: React.FC<{
  detail: RunFolderDetail
  workspacePath: string
  allFolderNames: string[]
  onOpenLogs: (workspacePath: string, runFolder: string, allFolders: string[]) => void
  onOpenEval: (workspacePath: string, runFolder: string) => void
  onOpenCost: (workspacePath: string, runFolder: string, allFolders: string[]) => void
}> = ({ detail, workspacePath, allFolderNames, onOpenLogs, onOpenEval, onOpenCost }) => (
  <tr>
    {/* Run name + times */}
    <td className="pl-14 pr-5 py-2.5">
      <div className="flex items-center gap-2">
        <span className="text-xs font-mono text-gray-600 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 px-2 py-0.5 rounded">
          {detail.folder.name}
        </span>
        <TriggerBadge triggeredBy={detail.triggeredBy} />
      </div>
      <div className="flex items-center gap-3 mt-1 text-[11px] text-gray-400 dark:text-gray-500">
        {detail.startedAt && (
          <span className="flex items-center gap-1">
            <Timer className="w-2.5 h-2.5" />
            {formatDateTime(detail.startedAt)}
          </span>
        )}
        {detail.completedAt && (
          <span>
            → {formatDateTime(detail.completedAt)}
          </span>
        )}
      </div>
      <ModelSummary models={detail.models} />
    </td>

    {/* Status */}
    <td className="px-5 py-2.5">
      <StatusBadge status={detail.status} completedSteps={detail.completedSteps} totalSteps={detail.totalSteps} />
    </td>

    {/* Eval score - clickable */}
    <td className="px-5 py-2.5">
      {detail.evalScore !== null && detail.evalMaxScore !== null && detail.evalMaxScore > 0 ? (
        <button
          onClick={(e) => { e.stopPropagation(); onOpenEval(workspacePath, detail.folder.name) }}
          className="flex items-center gap-1.5 hover:opacity-80 transition-opacity"
          title="View evaluation details"
        >
          <BarChart3 className="w-3 h-3 text-purple-500 dark:text-purple-400" />
          <span className={`text-xs font-medium ${
            (detail.evalScore / detail.evalMaxScore) >= 0.8
              ? 'text-green-600 dark:text-green-400'
              : (detail.evalScore / detail.evalMaxScore) >= 0.5
                ? 'text-amber-600 dark:text-amber-400'
                : 'text-red-600 dark:text-red-400'
          }`}>
            {Math.round((detail.evalScore / detail.evalMaxScore) * 100)}%
          </span>
        </button>
      ) : (
        <span className="text-xs text-gray-400 dark:text-gray-500">-</span>
      )}
    </td>

    {/* Cost - clickable */}
    <td className="px-5 py-2.5">
      {detail.costUsd !== null ? (
        <button
          onClick={(e) => { e.stopPropagation(); onOpenCost(workspacePath, detail.folder.name, allFolderNames) }}
          className="flex items-center gap-1.5 hover:opacity-80 transition-opacity"
          title="View cost breakdown"
        >
          <DollarSign className="w-3 h-3 text-emerald-500 dark:text-emerald-400" />
          <span className="text-xs font-medium text-gray-700 dark:text-gray-300">
            {formatCost(detail.costUsd)}
          </span>
        </button>
      ) : (
        <span className="text-xs text-gray-400 dark:text-gray-500">-</span>
      )}
    </td>

    {/* Last active */}
    <td className="px-5 py-2.5">
      {detail.lastUpdated ? (
        <div className="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
          <Clock className="w-3 h-3" />
          {formatTimestamp(detail.lastUpdated)}
        </div>
      ) : (
        <span className="text-xs text-gray-400 dark:text-gray-500">-</span>
      )}
    </td>

    {/* Actions */}
    <td className="px-5 py-2.5 text-right">
      <button
        onClick={(e) => {
          e.stopPropagation()
          onOpenLogs(workspacePath, detail.folder.name, allFolderNames)
        }}
        className="flex items-center gap-1 px-2 py-1 text-xs rounded-md text-gray-600 dark:text-gray-400 hover:bg-gray-200/70 dark:hover:bg-gray-600/40 transition-colors ml-auto"
        title="View logs"
      >
        <FileText className="w-3 h-3" />
        Logs
      </button>
    </td>
  </tr>
)

// Shared table content
const WorkflowTable: React.FC<{
  rows: WorkflowOverviewRow[]
  loading: boolean
  onOpenWorkflow: (preset: CustomPreset | PredefinedPreset) => void
  onOpenLogs: (workspacePath: string, runFolder: string, allFolders: string[]) => void
  onOpenEval: (workspacePath: string, runFolder: string) => void
  onOpenCost: (workspacePath: string, runFolder: string, allFolders: string[]) => void
}> = ({ rows, loading, onOpenWorkflow, onOpenLogs, onOpenEval, onOpenCost }) => {
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())

  const toggleExpanded = useCallback((id: string) => {
    setExpandedIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }, [])

  if (loading && rows.length === 0) {
    return (
      <div className="flex items-center justify-center p-16">
        <Loader2 className="w-5 h-5 animate-spin text-gray-400 dark:text-gray-500 mr-2" />
        <span className="text-sm text-gray-500 dark:text-gray-400">Loading workflows...</span>
      </div>
    )
  }

  if (rows.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-16 text-gray-500 dark:text-gray-400">
        <AlertCircle className="w-10 h-10 mb-3 text-gray-300 dark:text-gray-600" />
        <span className="text-sm">No workflows found</span>
        <span className="text-xs text-gray-400 dark:text-gray-500 mt-1">Create a workflow to get started</span>
      </div>
    )
  }

  return (
    <table className="w-full text-sm">
      <thead className="sticky top-0 bg-gray-50 dark:bg-gray-700/50 z-10">
        <tr className="text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
          <th className="px-6 py-3">Workflow / Run</th>
          <th className="px-5 py-3">Status</th>
          <th className="px-5 py-3">Eval</th>
          <th className="px-5 py-3">Cost</th>
          <th className="px-5 py-3">Last Active</th>
          <th className="px-5 py-3 text-right">Actions</th>
        </tr>
      </thead>
      <tbody>
        {rows.map((row) => {
          const isExpanded = expandedIds.has(row.preset.id)
          const hasRuns = row.runFolders.length > 0
          const allFolderNames = row.runFolders.map(rf => rf.folder.name)

          // Aggregate eval
          let aggEvalScore: number | null = null
          let aggEvalMax: number | null = null
          if (row.evalData?.success && row.evalData.aggregate) {
            aggEvalScore = row.evalData.aggregate.average_score
            aggEvalMax = row.evalData.aggregate.max_possible_score
          }

          // Aggregate cost
          let aggCost: number | null = null
          for (const rf of row.runFolders) {
            if (rf.costUsd !== null) aggCost = (aggCost || 0) + rf.costUsd
          }

          const latestRun = hasRuns ? row.runFolders[0] : null

          return (
            <React.Fragment key={row.preset.id}>
              <tr
                className="border-t border-gray-100 dark:border-gray-700/50 cursor-pointer"
                onClick={() => hasRuns ? toggleExpanded(row.preset.id) : onOpenWorkflow(row.preset)}
              >
                <td className="px-6 py-3.5">
                  <div className="flex items-center gap-2">
                    {hasRuns ? (
                      isExpanded
                        ? <ChevronDown className="w-4 h-4 text-gray-400 dark:text-gray-500 flex-shrink-0" />
                        : <ChevronRight className="w-4 h-4 text-gray-400 dark:text-gray-500 flex-shrink-0" />
                    ) : (
                      <div className="w-4" />
                    )}
                    <div>
                      <div className="font-medium text-gray-900 dark:text-gray-100 truncate max-w-[280px]">
                        {row.preset.label}
                      </div>
                      {row.loading ? (
                        <div className="flex items-center gap-1 mt-0.5">
                          <Loader2 className="w-3 h-3 animate-spin text-gray-400" />
                          <span className="text-xs text-gray-400">Loading...</span>
                        </div>
                      ) : row.error ? (
                        <span className="text-xs text-red-400">{row.error}</span>
                      ) : hasRuns ? (
                        <span className="text-xs text-gray-400 dark:text-gray-500 mt-0.5">
                          {row.totalRunCount} run{row.totalRunCount !== 1 ? 's' : ''}
                        </span>
                      ) : (
                        <span className="text-xs text-gray-400 dark:text-gray-500 mt-0.5">No runs yet</span>
                      )}
                    </div>
                  </div>
                </td>

                <td className="px-5 py-3.5">
                  {!row.loading && latestRun && (
                    <StatusBadge status={latestRun.status} completedSteps={latestRun.completedSteps} totalSteps={latestRun.totalSteps} />
                  )}
                </td>

                <td className="px-5 py-3.5">
                  {!row.loading && aggEvalScore !== null && aggEvalMax !== null && aggEvalMax > 0 ? (
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        if (row.workspacePath) onOpenEval(row.workspacePath, '')
                      }}
                      className="flex items-center gap-1.5 hover:opacity-80 transition-opacity"
                      title="View evaluation details"
                    >
                      <BarChart3 className="w-3 h-3 text-purple-500 dark:text-purple-400" />
                      <span className={`text-xs font-medium ${
                        (aggEvalScore / aggEvalMax) >= 0.8
                          ? 'text-green-600 dark:text-green-400'
                          : (aggEvalScore / aggEvalMax) >= 0.5
                            ? 'text-amber-600 dark:text-amber-400'
                            : 'text-red-600 dark:text-red-400'
                      }`}>
                        {Math.round((aggEvalScore / aggEvalMax) * 100)}%
                      </span>
                      <span className="text-xs text-gray-400 dark:text-gray-500">avg</span>
                    </button>
                  ) : (
                    !row.loading && <span className="text-xs text-gray-400 dark:text-gray-500">-</span>
                  )}
                </td>

                <td className="px-5 py-3.5">
                  {!row.loading && aggCost !== null ? (
                    <div className="flex items-center gap-1.5">
                      <DollarSign className="w-3 h-3 text-emerald-500 dark:text-emerald-400" />
                      <span className="text-xs font-medium text-gray-700 dark:text-gray-300">{formatCost(aggCost)}</span>
                      <span className="text-xs text-gray-400 dark:text-gray-500">total</span>
                    </div>
                  ) : (
                    !row.loading && <span className="text-xs text-gray-400 dark:text-gray-500">-</span>
                  )}
                </td>

                <td className="px-5 py-3.5">
                  {!row.loading && row.lastUpdated ? (
                    <div className="flex items-center gap-1.5 text-xs text-gray-500 dark:text-gray-400">
                      <Clock className="w-3 h-3" />
                      {formatTimestamp(row.lastUpdated)}
                    </div>
                  ) : (
                    !row.loading && <span className="text-xs text-gray-400 dark:text-gray-500">-</span>
                  )}
                </td>

                <td className="px-5 py-3.5 text-right">
                  <button
                    onClick={(e) => { e.stopPropagation(); onOpenWorkflow(row.preset) }}
                    className="flex items-center gap-1 px-2.5 py-1 text-xs rounded-md text-purple-600 dark:text-purple-400 hover:bg-purple-100 dark:hover:bg-purple-900/30 transition-colors ml-auto"
                    title="Open workflow"
                  >
                    Open
                  </button>
                </td>
              </tr>

              {isExpanded && row.runFolders.map((detail) => (
                <RunFolderRow
                  key={detail.folder.name}
                  detail={detail}
                  workspacePath={row.workspacePath!}
                  allFolderNames={allFolderNames}
                  onOpenLogs={onOpenLogs}
                  onOpenEval={onOpenEval}
                  onOpenCost={onOpenCost}
                />
              ))}
            </React.Fragment>
          )
        })}
      </tbody>
    </table>
  )
}

// Shared popup state hook
function usePopupState() {
  const [logsOpen, setLogsOpen] = useState(false)
  const [logsWorkspace, setLogsWorkspace] = useState<string | null>(null)
  const [logsRunFolder, setLogsRunFolder] = useState<string | null>(null)
  const [logsRunFolders, setLogsRunFolders] = useState<string[]>([])

  const [evalOpen, setEvalOpen] = useState(false)
  const [evalWorkspace, setEvalWorkspace] = useState<string | null>(null)
  const [evalRunFolder, setEvalRunFolder] = useState<string | null>(null)

  const [costOpen, setCostOpen] = useState(false)
  const [costWorkspace, setCostWorkspace] = useState<string | null>(null)
  const [costRunFolder, setCostRunFolder] = useState<string | null>(null)
  const [costRunFolders, setCostRunFolders] = useState<string[]>([])

  const handleOpenLogs = useCallback((workspacePath: string, runFolder: string, allFolders: string[]) => {
    setLogsWorkspace(workspacePath)
    setLogsRunFolder(runFolder)
    setLogsRunFolders(allFolders)
    setLogsOpen(true)
  }, [])

  const handleOpenEval = useCallback((workspacePath: string, runFolder: string) => {
    setEvalWorkspace(workspacePath)
    setEvalRunFolder(runFolder || null)
    setEvalOpen(true)
  }, [])

  const handleOpenCost = useCallback((workspacePath: string, runFolder: string, allFolders: string[]) => {
    setCostWorkspace(workspacePath)
    setCostRunFolder(runFolder)
    setCostRunFolders(allFolders)
    setCostOpen(true)
  }, [])

  return {
    logsOpen, setLogsOpen, logsWorkspace, logsRunFolder, logsRunFolders, handleOpenLogs,
    evalOpen, setEvalOpen, evalWorkspace, evalRunFolder, handleOpenEval,
    costOpen, setCostOpen, costWorkspace, costRunFolder, costRunFolders, handleOpenCost,
  }
}

// Popup rendering shared between page and dialog
const PopupGroup: React.FC<{ p: ReturnType<typeof usePopupState> }> = ({ p }) => (
  <>
    <ExecutionLogsPopup isOpen={p.logsOpen} onClose={() => p.setLogsOpen(false)} workspacePath={p.logsWorkspace} runFolder={p.logsRunFolder} runFolders={p.logsRunFolders} />
    <EvaluationPopup isOpen={p.evalOpen} onClose={() => p.setEvalOpen(false)} workspacePath={p.evalWorkspace} selectedRunFolder={p.evalRunFolder} runFolders={p.evalRunFolder ? [p.evalRunFolder] : []} />
    <CostsPopup isOpen={p.costOpen} onClose={() => p.setCostOpen(false)} workspacePath={p.costWorkspace} selectedRunFolder={p.costRunFolder} runFolders={p.costRunFolders} />
  </>
)

const ORG_CHAT_STARTER_PROMPT = `Help me manage employees and workflow ownership.

Use available tools/APIs to:
1. List employees
2. Create, edit, and delete employees
3. Assign and unassign workflows
4. Create, edit, enable/disable, and delete workflow schedules
5. Show latest workflow outputs and where to inspect them
6. Explain what changed and suggest next cleanup actions.`

const ORG_TOOL_GUIDE = [
  { title: 'List Employees', desc: 'Show current employees and assignment coverage.' },
  { title: 'Create Employee', desc: 'Add employee profile with name, color, and description.' },
  { title: 'Update Employee', desc: 'Rename or edit role/description for existing employee.' },
  { title: 'Delete Employee', desc: 'Remove employee; workflows become unassigned.' },
  { title: 'Assign Workflow', desc: 'Attach workflow preset to a specific employee.' },
  { title: 'Unassign Workflow', desc: 'Clear employee assignment from a workflow preset.' },
  { title: 'Manage Schedules', desc: 'Create/update cron schedules for workflow presets.' },
  { title: 'Inspect Outputs', desc: 'Surface the latest report, evaluation, cost, and run health.' },
]

const OrganizationChatPanel: React.FC<{
  minimized: boolean
  onToggleMinimized: () => void
}> = ({ minimized, onToggleMinimized }) => {
  const [orgTabId, setOrgTabId] = useState<string | null>(null)
  const hasConversationStarted = useChatStore(state => {
    if (!orgTabId) return false
    const tab = state.chatTabs[orgTabId]
    if (!tab) return false
    if (tab.isStreaming) return true
    const sessionId = tab.sessionId
    return !!sessionId && (state.tabEvents[sessionId]?.length ?? 0) > 0
  })

  useEffect(() => {
    let cancelled = false

    const ensureOrgTab = async () => {
      const chatStore = useChatStore.getState()
      const orgTabs = Object.values(chatStore.chatTabs).filter(
        tab => tab.metadata?.mode === 'multi-agent' && (
          tab.metadata?.isOrganizationAssistant ||
          tab.name.toLowerCase() === 'organization assistant' ||
          tab.name.toLowerCase().startsWith('org chat ')
        )
      )

      const primaryOrgTab = orgTabs.sort((a, b) => (a.createdAt || 0) - (b.createdAt || 0))[0]
        const tabId = primaryOrgTab
        ? primaryOrgTab.tabId
        : await chatStore.createChatTab('Organization Assistant', {
          mode: 'multi-agent',
          isOrganizationAssistant: true
        })

      if (!primaryOrgTab) {
        chatStore.setTabConfig(tabId, { inputText: ORG_CHAT_STARTER_PROMPT })
      } else if (
        !primaryOrgTab.metadata?.isOrganizationAssistant ||
        primaryOrgTab.metadata?.mode !== 'multi-agent'
      ) {
        chatStore.setTabMetadata(tabId, {
          isOrganizationAssistant: true,
          mode: 'multi-agent'
        })
      }

      // Keep exactly one Organization tab.
      const extraOrgTabs = orgTabs.filter(tab => tab.tabId !== tabId)
      await Promise.all(extraOrgTabs.map(tab => chatStore.closeTab(tab.tabId, false, true)))

      chatStore.switchTab(tabId)
      if (!cancelled) setOrgTabId(tabId)
    }

    ensureOrgTab().catch((err) => {
      console.error('[OrganizationChatPanel] Failed to initialize tab:', err)
    })

    return () => {
      cancelled = true
    }
  }, [])

  const handleOrgNewChat = useCallback(() => {
    // Single-thread organization assistant by design.
  }, [])

  if (minimized) {
    return (
      <div className="w-full xl:w-16 xl:min-w-16 xl:max-w-16 h-14 xl:h-full shrink-0 border-b xl:border-b-0 xl:border-r border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 flex items-center justify-between xl:justify-start xl:flex-col xl:py-4 px-4 xl:px-2">
        <div className="flex items-center gap-2 xl:flex-col xl:gap-3 text-gray-600 dark:text-gray-300">
          <MessageSquare className="w-4 h-4" />
          <span className="text-xs font-medium xl:hidden">Organization Chat</span>
        </div>
        <button
          onClick={onToggleMinimized}
          className="p-1.5 rounded-md text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
          title="Expand organization chat"
        >
          <ChevronRight className="w-4 h-4 xl:rotate-180" />
        </button>
      </div>
    )
  }

  return (
    <div className="w-full xl:w-[46%] xl:min-w-[420px] h-full flex flex-col min-h-0 border-b xl:border-b-0 xl:border-r border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900">
      <div className="px-4 py-3 border-b border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 flex items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-semibold text-gray-900 dark:text-gray-100">Organization Assistant</h3>
          <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">Single assistant thread for employee and workflow management.</p>
        </div>
        <button
          onClick={onToggleMinimized}
          className="p-1.5 rounded-md text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800 transition-colors"
          title="Minimize organization chat"
        >
          <ChevronLeft className="w-4 h-4" />
        </button>
      </div>

      {!hasConversationStarted && (
        <div className="px-4 py-3 border-b border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-800/50">
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            {ORG_TOOL_GUIDE.map(tool => (
              <div key={tool.title} className="rounded-md border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-900 px-2.5 py-2">
                <div className="text-xs font-semibold text-gray-800 dark:text-gray-200">{tool.title}</div>
                <div className="text-[11px] text-gray-500 dark:text-gray-400 mt-0.5">{tool.desc}</div>
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="flex-1 min-h-0 bg-white dark:bg-gray-900">
        {orgTabId ? (
          <ChatArea onNewChat={handleOrgNewChat} tabId={orgTabId} hideHeader compact />
        ) : (
          <div className="h-full flex items-center justify-center">
            <Loader2 className="w-5 h-5 animate-spin text-indigo-500" />
          </div>
        )}
      </div>
    </div>
  )
}

// Full page view
export const WorkflowsOverviewPage: React.FC = () => {
  const { rows, loading, loadData } = useWorkflowRows()
  const { applyPreset } = usePresetApplication()
  const { setModeCategory, selectedModeCategory } = useModeStore()
  const setShowWorkflowsOverview = useAppStore(s => s.setShowWorkflowsOverview)
  const popups = usePopupState()
  const [activeTab, setActiveTab] = useState<'workflows' | 'employees'>('employees')
  const [orgChatMinimized, setOrgChatMinimized] = useState(false)

  useEffect(() => { loadData() }, [loadData])

  const handleOpenWorkflow = useCallback((preset: CustomPreset | PredefinedPreset) => {
    if (selectedModeCategory !== 'workflow') setModeCategory('workflow')
    applyPreset(preset, 'workflow')
    setShowWorkflowsOverview(false)
  }, [applyPreset, selectedModeCategory, setModeCategory, setShowWorkflowsOverview])

  return (
    <div className="h-full flex flex-col bg-white dark:bg-gray-900">
      <div className="flex-1 min-h-0 flex flex-col xl:flex-row">
        <OrganizationChatPanel
          minimized={orgChatMinimized}
          onToggleMinimized={() => setOrgChatMinimized(prev => !prev)}
        />

        <div className="h-full min-h-0 flex-1 min-w-0 flex flex-col">
          <div className="px-6 py-3 border-b border-gray-200 dark:border-gray-700">
            <div className="flex items-center bg-gray-100 dark:bg-gray-800 rounded-lg p-0.5 w-fit">
              <button
                onClick={() => setActiveTab('workflows')}
                className={`px-3 py-1 text-xs font-medium rounded-md transition-all ${
                  activeTab === 'workflows'
                    ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm'
                    : 'text-gray-500 dark:text-gray-400'
                }`}
              >
                Runs
              </button>
              <button
                onClick={() => setActiveTab('employees')}
                className={`px-3 py-1 text-xs font-medium rounded-md transition-all ${
                  activeTab === 'employees'
                    ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm'
                    : 'text-gray-500 dark:text-gray-400'
                }`}
              >
                Employees
              </button>
            </div>
          </div>

          <div className="flex-1 min-h-0 overflow-auto">
            {activeTab === 'workflows' ? (
              <WorkflowTable rows={rows} loading={loading} onOpenWorkflow={handleOpenWorkflow} onOpenLogs={popups.handleOpenLogs} onOpenEval={popups.handleOpenEval} onOpenCost={popups.handleOpenCost} />
            ) : (
              <div className="p-6">
                <EmployeeDashboard />
              </div>
            )}
          </div>
        </div>
      </div>
      <PopupGroup p={popups} />
    </div>
  )
}

// Popup/dialog version
export const WorkflowsOverviewPopup: React.FC<{ isOpen: boolean; onClose: () => void }> = ({ isOpen, onClose }) => {
  const { rows, loading, loadData } = useWorkflowRows()
  const { applyPreset } = usePresetApplication()
  const { setModeCategory, selectedModeCategory } = useModeStore()
  const popups = usePopupState()

  useEffect(() => { if (isOpen) loadData() }, [isOpen, loadData])

  const handleOpenWorkflow = useCallback((preset: CustomPreset | PredefinedPreset) => {
    if (selectedModeCategory !== 'workflow') setModeCategory('workflow')
    applyPreset(preset, 'workflow')
    onClose()
  }, [applyPreset, onClose, selectedModeCategory, setModeCategory])

  if (!isOpen) return null

  return (
    <>
      <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onClick={onClose}>
        <div
          className="bg-white dark:bg-gray-800 rounded-xl shadow-2xl overflow-hidden text-gray-900 dark:text-gray-100"
          style={{ width: 'calc(100vw - 80px)', height: 'calc(100vh - 80px)', maxWidth: '1400px', maxHeight: '900px' }}
          onClick={e => e.stopPropagation()}
        >
          <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold">All Workflows</h3>
            <button onClick={onClose} className="p-1.5 rounded-md text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors">
              <X className="w-4 h-4" />
            </button>
          </div>
          <div className="overflow-auto" style={{ height: 'calc(100% - 57px)' }}>
            <WorkflowTable rows={rows} loading={loading} onOpenWorkflow={handleOpenWorkflow} onOpenLogs={popups.handleOpenLogs} onOpenEval={popups.handleOpenEval} onOpenCost={popups.handleOpenCost} />
          </div>
        </div>
      </div>
      <PopupGroup p={popups} />
    </>
  )
}
