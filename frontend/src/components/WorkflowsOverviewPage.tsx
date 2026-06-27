import React, { useEffect, useState, useCallback } from 'react'
import { Loader2, ChevronRight, ChevronDown, FileText, BarChart3, DollarSign, Clock, AlertCircle, X, CheckCircle2, PlayCircle, Circle, Timer, Zap, Building2, Target, Workflow } from 'lucide-react'
import { agentApi, type WorkflowMetricRunSummary } from '../services/api'
import { usePresetApplication } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import ExecutionLogsPopup from './workflow/ExecutionLogsPopup'
import EvaluationPopup from './workflow/EvaluationPopup'
import CostsPopup from './workflow/CostsPopup'
import { OrgGoalsPanel } from './org/OrgHtmlPanels'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import type { RunFolderInfo, EvaluationReportsResponse, RunMetadataModels } from '../services/api-types'

interface RunFolderDetail {
  folder: RunFolderInfo
  totalSteps: number
  completedSteps: number
  lastUpdated: string | null
  metricsSummary: WorkflowMetricRunSummary | null
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

const metricHealthText = (summary: WorkflowMetricRunSummary | null): string | null => {
  if (!summary || summary.total <= 0) return null
  if (summary.failed > 0) return `${summary.failed} failing`
  if (summary.with_value < summary.total) return `${summary.with_value}/${summary.total} values`
  if (summary.passed > 0) return `${summary.passed}/${summary.total} passing`
  return `${summary.total} metrics`
}

const metricHealthClass = (summary: WorkflowMetricRunSummary | null): string => {
  if (!summary || summary.total <= 0) return 'text-gray-400 dark:text-gray-500'
  if (summary.failed > 0) return 'text-red-600 dark:text-red-400'
  if (summary.with_value < summary.total) return 'text-amber-600 dark:text-amber-400'
  if (summary.passed > 0) return 'text-green-600 dark:text-green-400'
  return 'text-gray-600 dark:text-gray-300'
}

const MetricsSummaryBadge: React.FC<{ summary: WorkflowMetricRunSummary | null; compact?: boolean }> = ({ summary, compact = false }) => {
  const text = metricHealthText(summary)
  if (!text) return <span className="text-xs text-gray-400 dark:text-gray-500">-</span>
  return (
    <div
      className="flex items-center gap-1.5"
      title={`${summary?.passed || 0} passing, ${summary?.failed || 0} failing, ${summary?.unknown || 0} unknown`}
    >
      <BarChart3 className="w-3 h-3 text-cyan-500 dark:text-cyan-400" />
      <span className={`text-xs font-medium ${metricHealthClass(summary)}`}>{text}</span>
      {!compact && summary && summary.unknown > 0 && (
        <span className="text-xs text-gray-400 dark:text-gray-500">{summary.unknown} unknown</span>
      )}
    </div>
  )
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

    const workspacePaths = initialRows
      .map(row => row.workspacePath)
      .filter((path): path is string => Boolean(path))

    let overviewByPath = new Map<string, Awaited<ReturnType<typeof agentApi.getWorkflowsOverview>>['workflows'][number]>()
    try {
      const overviewResp = workspacePaths.length > 0
        ? await agentApi.getWorkflowsOverview(workspacePaths)
        : { success: true, workflows: [] }
      overviewByPath = new Map((overviewResp.workflows || []).map(row => [row.workspace_path, row]))
    } catch {
      overviewByPath = new Map()
    }

    const updated = initialRows.map((row) => {
      if (!row.workspacePath) {
        return { ...row, loading: false, error: 'No workspace path' }
      }

      const overview = overviewByPath.get(row.workspacePath)
      if (!overview) {
        return { ...row, loading: false, error: 'Failed to load' }
      }

      const runFolderDetails: RunFolderDetail[] = (overview.run_folders || []).map(detail => ({
        folder: detail.folder,
        totalSteps: detail.total_steps || 0,
        completedSteps: detail.completed_steps || 0,
        lastUpdated: detail.last_updated || null,
        metricsSummary: detail.metrics_summary || null,
        costUsd: detail.cost_usd ?? null,
        startedAt: detail.started_at || null,
        completedAt: detail.completed_at || null,
        triggeredBy: detail.triggered_by || null,
        status: detail.status || 'unknown',
        models: detail.models || null,
      }))

      return {
        ...row,
        loading: false,
        error: overview.error || null,
        runFolders: runFolderDetails,
        evalData: overview.eval_data || null,
        lastUpdated: overview.last_updated || null,
        totalRunCount: overview.total_run_count || runFolderDetails.length,
      }
    })

    updated.sort((a, b) => {
      if (!a.lastUpdated && !b.lastUpdated) return 0
      if (!a.lastUpdated) return 1
      if (!b.lastUpdated) return -1
      return b.lastUpdated.localeCompare(a.lastUpdated)
    })

    setRows(updated)
    setLoading(false)
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

    {/* Metric health */}
    <td className="px-5 py-2.5">
      {detail.metricsSummary ? (
        <button
          onClick={(e) => { e.stopPropagation(); onOpenEval(workspacePath, detail.folder.name) }}
          className="flex items-center gap-1.5 hover:opacity-80 transition-opacity"
          title="View evaluation evidence"
        >
          <MetricsSummaryBadge summary={detail.metricsSummary} compact />
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
        <span className="text-sm text-gray-500 dark:text-gray-400">Loading automations...</span>
      </div>
    )
  }

  if (rows.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center p-16 text-gray-500 dark:text-gray-400">
        <AlertCircle className="w-10 h-10 mb-3 text-gray-300 dark:text-gray-600" />
        <span className="text-sm">No automations found</span>
        <span className="text-xs text-gray-400 dark:text-gray-500 mt-1">Create an automation to get started</span>
      </div>
    )
  }

  return (
    <table className="w-full text-sm">
      <thead className="sticky top-0 bg-gray-50 dark:bg-gray-700/50 z-10">
        <tr className="text-left text-xs font-medium text-gray-500 dark:text-gray-400 uppercase tracking-wider">
          <th className="px-6 py-3">Automation / Run</th>
          <th className="px-5 py-3">Status</th>
          <th className="px-5 py-3">Metrics</th>
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
                  {!row.loading && latestRun?.metricsSummary ? (
                    <button
                      onClick={(e) => {
                        e.stopPropagation()
                        if (row.workspacePath) onOpenEval(row.workspacePath, '')
                      }}
                      className="flex items-center gap-1.5 hover:opacity-80 transition-opacity"
                      title="View evaluation evidence"
                    >
                      <MetricsSummaryBadge summary={latestRun.metricsSummary} />
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
                    title="Open automation"
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

// Full page view — goals-first organization overview
export const WorkflowsOverviewPage: React.FC = () => {
  const { rows, loading, loadData } = useWorkflowRows()
  const { applyPreset } = usePresetApplication()
  const { setModeCategory, selectedModeCategory } = useModeStore()
  const popups = usePopupState()

  useEffect(() => { loadData() }, [loadData])

  const handleOpenWorkflow = useCallback((preset: CustomPreset | PredefinedPreset) => {
    if (selectedModeCategory !== 'workflow') setModeCategory('workflow')
    applyPreset(preset, 'workflow')
  }, [applyPreset, selectedModeCategory, setModeCategory])

  const automationCount = rows.length

  return (
    <>
      <div className="h-full flex flex-col bg-white dark:bg-gray-900">
        <div className="flex-1 min-h-0 overflow-auto">
          <div className="mx-auto w-full max-w-[1400px] p-6 space-y-8">
            {/* Header */}
            <header className="flex items-center gap-3">
              <div className="flex h-10 w-10 flex-none items-center justify-center rounded-xl bg-purple-100 text-purple-600 dark:bg-purple-900/40 dark:text-purple-300">
                <Building2 className="h-5 w-5" />
              </div>
              <div className="min-w-0">
                <h1 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Organization</h1>
                <p className="text-sm text-gray-500 dark:text-gray-400">
                  Goals, automations, and the Chief of Staff that runs them.
                </p>
              </div>
            </header>

            {/* Goals section */}
            <section className="space-y-3">
              <div className="flex items-center gap-2">
                <Target className="h-4 w-4 text-primary" />
                <h2 className="text-sm font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Org Goals
                </h2>
              </div>
              <div className="h-[420px] overflow-hidden rounded-xl border border-gray-200 dark:border-gray-700">
                <OrgGoalsPanel />
              </div>
            </section>

            {/* Automations section */}
            <section className="space-y-3">
              <div className="flex items-center gap-2">
                <Workflow className="h-4 w-4 text-primary" />
                <h2 className="text-sm font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
                  Automations
                </h2>
                {automationCount > 0 && (
                  <span className="text-xs text-gray-400 dark:text-gray-500">
                    {automationCount} total
                  </span>
                )}
              </div>
              <div className="overflow-hidden rounded-xl border border-gray-200 dark:border-gray-700">
                <WorkflowTable
                  rows={rows}
                  loading={loading}
                  onOpenWorkflow={handleOpenWorkflow}
                  onOpenLogs={popups.handleOpenLogs}
                  onOpenEval={popups.handleOpenEval}
                  onOpenCost={popups.handleOpenCost}
                />
              </div>
            </section>
          </div>
        </div>
      </div>
      <PopupGroup p={popups} />
    </>
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
      <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-2 sm:p-4" onClick={onClose}>
        <div
          className="bg-white dark:bg-gray-800 rounded-xl shadow-2xl overflow-hidden text-gray-900 dark:text-gray-100"
          style={{ width: 'min(1400px, calc(100vw - 1rem))', height: 'min(900px, calc(100dvh - 1rem))' }}
          onClick={e => e.stopPropagation()}
        >
          <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-gray-700">
            <h3 className="text-lg font-semibold">All Automations</h3>
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
