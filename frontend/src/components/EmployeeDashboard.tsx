import React, { useEffect, useState, useCallback, useMemo } from 'react'
import {
  X, UserCircle2,
  PlayCircle, Clock, DollarSign, Loader2, Calendar, FileText, BarChart3, ChevronDown, ChevronRight,
  UserCog, UserPlus
} from 'lucide-react'
import {
  Bar,
  BarChart,
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { agentApi } from '../services/api'
import { schedulerApi } from '../api/scheduler'
import type { Employee, EvaluationAggregate, EvaluationReportEntry, PhaseTokenUsageFile, TokenUsageFile, WorkflowPhaseDailyCostsEntry, WorkflowReviewDataResponse, WorkflowRunCostsEntry } from '../services/api-types'
import ExecutionLogsPopup from './workflow/ExecutionLogsPopup'
import { ReportView } from './workflow/ReportViewer'
import { useAppStore } from '../stores/useAppStore'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'

interface WorkflowSummary {
  id: string
  label: string
  latestStatus: string
  totalRuns: number
  lastActive: string | null
  totalCost: number | null
  evalPercent: number | null
  workspacePath: string
  latestRunFolder: string | null
  scheduleCount: number
  nextScheduleAt: string | null
}

interface EmployeeWithWorkflows {
  employee: Employee
  workflows: WorkflowSummary[]
  totalCost: number
  completedToday: number
  runningNow: number
}

interface EmployeeApiRecord extends Employee {
  workflow_count?: number
  workflows?: string[]
}

type ReviewTab = 'report' | 'evaluation' | 'cost'

interface SelectedWorkflowEntry {
  employee: Employee
  workflow: WorkflowSummary
}

interface WorkflowReviewState {
  loading: boolean
  reviewData: WorkflowReviewDataResponse | null
  evaluation: EvaluationReportEntry | null
  evaluations: EvaluationReportEntry[]
  evaluationAggregate: EvaluationAggregate | null
  evaluationError: string | null
  tokenUsage: TokenUsageFile | null
  evaluationTokenUsage: TokenUsageFile | null
  costRuns: WorkflowRunCostsEntry[]
  phaseDailyCosts: WorkflowPhaseDailyCostsEntry[]
  costError: string | null
}

type WorkflowsSummaryResponse = Awaited<ReturnType<typeof agentApi.getWorkflowsSummary>>
type WorkflowApiSummary = WorkflowsSummaryResponse['workflows'][number]

// Avatar component
const EmployeeAvatar: React.FC<{ name: string; color: string; size?: 'sm' | 'md' | 'lg' }> = ({ name, color, size = 'md' }) => {
  const initials = name.split(' ').map(w => w[0]).join('').slice(0, 2).toUpperCase()
  const sizeClasses = { sm: 'w-8 h-8 text-[11px]', md: 'w-11 h-11 text-sm', lg: 'w-14 h-14 text-lg' }
  return (
    <div
      className={`${sizeClasses[size]} flex items-center justify-center rounded-xl font-bold text-white shadow-sm ring-1 ring-black/5 dark:ring-white/10`}
      style={{
        background: `linear-gradient(145deg, ${color}, rgba(71, 85, 105, 0.95))`,
      }}
    >
      {initials}
    </div>
  )
}

// Mini status indicator
const StatusDot: React.FC<{ status: string }> = ({ status }) => {
  if (status === 'completed') return <div className="h-2.5 w-2.5 rounded-full bg-emerald-500" />
  if (status === 'running') return <div className="h-2.5 w-2.5 rounded-full bg-sky-500 animate-pulse" />
  if (status === 'failed') return <div className="h-2.5 w-2.5 rounded-full bg-rose-500" />
  return <div className="w-2.5 h-2.5 rounded-full bg-gray-300 dark:bg-slate-500" />
}

const EMPTY_REVIEW_STATE: WorkflowReviewState = {
  loading: false,
  reviewData: null,
  evaluation: null,
  evaluations: [],
  evaluationAggregate: null,
  evaluationError: null,
  tokenUsage: null,
  evaluationTokenUsage: null,
  costRuns: [],
  phaseDailyCosts: [],
  costError: null,
}

const runFolderMatches = (candidate: string | null | undefined, requested: string | null | undefined): boolean => {
  if (!candidate || !requested) return false
  return candidate === requested || candidate.startsWith(`${requested}/`) || requested.startsWith(`${candidate}/`)
}

// Merge N TokenUsageFile entries into one by summing per-model numeric fields.
// Mirrors what the workflows CostsPopup does client-side.
const mergeTokenUsageFiles = (files: Array<TokenUsageFile | null | undefined>): TokenUsageFile | null => {
  const nonNull = files.filter((f): f is TokenUsageFile => !!f)
  if (nonNull.length === 0) return null

  const merged: TokenUsageFile = {
    created_at: nonNull[0].created_at,
    updated_at: nonNull[0].updated_at,
    by_model: {},
  }

  for (const file of nonNull) {
    for (const [model, stats] of Object.entries(file.by_model || {})) {
      const existing = merged.by_model[model]
      if (!existing) {
        merged.by_model[model] = { ...stats }
        continue
      }
      existing.input_tokens = (existing.input_tokens || 0) + (stats.input_tokens || 0)
      existing.output_tokens = (existing.output_tokens || 0) + (stats.output_tokens || 0)
      existing.cache_tokens = (existing.cache_tokens || 0) + (stats.cache_tokens || 0)
      existing.reasoning_tokens = (existing.reasoning_tokens || 0) + (stats.reasoning_tokens || 0)
      existing.llm_call_count = (existing.llm_call_count || 0) + (stats.llm_call_count || 0)
      existing.total_cost_usd = (existing.total_cost_usd || 0) + (stats.total_cost_usd || 0)
      existing.input_cost_usd = (existing.input_cost_usd || 0) + (stats.input_cost_usd || 0)
      existing.output_cost_usd = (existing.output_cost_usd || 0) + (stats.output_cost_usd || 0)
    }
  }

  return merged
}

const getTokenUsageTotal = (usage: TokenUsageFile | null | undefined): number | null => {
  if (!usage) return null
  let total = 0
  let found = false
  Object.values(usage.by_model || {}).forEach(model => {
    const cost = model.total_cost_usd || 0
    total += cost
    if (cost > 0) found = true
  })
  return found || total > 0 ? total : null
}

const formatUsd = (value: number | null): string => {
  if (value === null) return '-'
  if (value < 0.01) return '<$0.01'
  return `$${value.toFixed(2)}`
}

const ReviewTabButton: React.FC<{
  active: boolean
  label: string
  onClick: () => void
}> = ({ active, label, onClick }) => (
  <button
    onClick={onClick}
    className={`px-3 py-1.5 rounded-md text-xs font-medium transition-colors ${
      active
        ? 'border border-border bg-background text-foreground shadow-sm'
        : 'text-muted-foreground hover:text-foreground'
    }`}
  >
    {label}
  </button>
)

const getEvalBadgeClasses = (_evalPercent: number): string => {
  return 'bg-muted text-muted-foreground ring-1 ring-inset ring-border'
}

const formatPercent = (value: number): string => `${value.toFixed(1)}%`

const getScoreTextColor = (_percentage: number): string => 'text-foreground'

const getScoreBarColor = (_percentage: number): string => 'bg-muted-foreground/40'

// Line colors for multi-group trend chart. Cycled by group index.
const GROUP_LINE_COLORS = ['#6366f1', '#10b981', '#f59e0b', '#ec4899', '#06b6d4', '#a855f7', '#ef4444', '#84cc16']

// run_folder looks like "iteration-0/default-group". Split into iteration + group.
// If there's no "/", treat the whole thing as the iteration and use "default" as group.
interface ParsedRun {
  iteration: string
  group: string
  iterationIndex: number
}
const parseRunFolder = (runFolder: string): ParsedRun => {
  const slash = runFolder.indexOf('/')
  const iteration = slash >= 0 ? runFolder.slice(0, slash) : runFolder
  const group = slash >= 0 ? runFolder.slice(slash + 1) : 'default'
  const match = iteration.match(/(\d+)/)
  const iterationIndex = match ? parseInt(match[1], 10) : Number.MAX_SAFE_INTEGER
  return { iteration, group, iterationIndex }
}

export const EmployeeDashboard: React.FC = () => {
  const showWorkflowsOverview = useAppStore(state => state.showWorkflowsOverview)
  const workflowPresets = useGlobalPresetStore(state => state.workflowPresets)
  const workflowPresetsLoaded = useGlobalPresetStore(state => state.workflowPresetsLoaded)
  const presetsLoading = useGlobalPresetStore(state => state.loading)
  const refreshPresets = useGlobalPresetStore(state => state.refreshPresets)
  const [employees, setEmployees] = useState<Employee[]>([])
  const [employeeWorkflows, setEmployeeWorkflows] = useState<EmployeeWithWorkflows[]>([])
  const [loading, setLoading] = useState(true)
  const [assigningWorkflow, setAssigningWorkflow] = useState<string | null>(null) // workspace path being assigned
  const [logsState, setLogsState] = useState<{ workspacePath: string; runFolder: string } | null>(null)
  const [selectedWorkflowId, setSelectedWorkflowId] = useState<string | null>(null)
  const [collapsedEmployeeIds, setCollapsedEmployeeIds] = useState<Set<string>>(new Set())
  const [reviewTab, setReviewTab] = useState<ReviewTab>('report')
  const [reviewState, setReviewState] = useState<WorkflowReviewState>(EMPTY_REVIEW_STATE)
  const [selectedEvalRunFolder, setSelectedEvalRunFolder] = useState<string | null>(null)
  const [expandedEvalSteps, setExpandedEvalSteps] = useState<Set<string>>(new Set())

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [empResp] = await Promise.all([
        agentApi.listEmployees(),
      ])
      const schedulesResp = await schedulerApi.listJobs({ entity_type: 'workflow' }).catch(() => ({ jobs: [], total: 0, limit: 0, offset: 0 }))

      const discoveredWorkflows = workflowPresets
      const scheduleByWorkspace = new Map<string, { count: number; nextRunAt: string | null }>()
      for (const job of schedulesResp.jobs || []) {
        const workspacePath = job.workspace_path
        if (!workspacePath) continue
        const prev = scheduleByWorkspace.get(workspacePath) || { count: 0, nextRunAt: null }
        let nextRunAt = prev.nextRunAt
        if (job.enabled && job.next_run_at) {
          if (!nextRunAt || job.next_run_at < nextRunAt) nextRunAt = job.next_run_at
        }
        scheduleByWorkspace.set(workspacePath, { count: prev.count + 1, nextRunAt })
      }

      const emps = (empResp.employees || []) as EmployeeApiRecord[]
      setEmployees(emps)

      const workflowAssignments = new Map<string, string>()
      for (const emp of emps) {
        for (const workflowPath of emp.workflows || []) {
          workflowAssignments.set(workflowPath, emp.id)
        }
      }

      // Build workflow summaries using the batch summary endpoint (single API call)
      const summaries: Map<string, WorkflowSummary> = new Map()
      const allWorkspacePaths = discoveredWorkflows.map(wf => wf.selectedFolder?.filepath).filter((path): path is string => Boolean(path))

      // Fetch only lightweight workflow summaries for the dashboard list. Detailed
      // evaluation data is loaded for the selected workflow via review-data below.
      const summaryResp = await (
        allWorkspacePaths.length > 0
          ? agentApi.getWorkflowsSummary(allWorkspacePaths).catch(() => null)
          : Promise.resolve(null)
      )

      // Index summary results by workspace path
      const summaryByPath = new Map<string, WorkflowApiSummary>()
      if (summaryResp?.success && summaryResp.workflows) {
        for (const ws of summaryResp.workflows) {
          summaryByPath.set(ws.workspace_path, ws)
        }
      }

      for (const workflow of discoveredWorkflows) {
        const wp = workflow.selectedFolder?.filepath
        if (!wp) continue
        const ws = summaryByPath.get(wp)
        const sched = scheduleByWorkspace.get(wp)

        let latestStatus = ws?.latest_run?.status || 'unknown'
        const lastActive = ws?.latest_run?.created_at || null
        const latestRunFolder = ws?.latest_run?.folder || null

        if (ws?.is_running) {
          latestStatus = 'running'
        }

        summaries.set(wp, {
          id: workflow.id || wp,
          label: workflow.label || wp.split('/').pop() || wp,
          latestStatus,
          totalRuns: ws?.total_runs || 0,
          lastActive,
          totalCost: null,
          evalPercent: null,
          workspacePath: wp,
          latestRunFolder,
          scheduleCount: sched?.count || 0,
          nextScheduleAt: sched?.nextRunAt || null,
        })
      }

      const empMap: Map<string, EmployeeWithWorkflows> = new Map()
      for (const emp of emps) {
        empMap.set(emp.id, { employee: emp, workflows: [], totalCost: 0, completedToday: 0, runningNow: 0 })
      }

      const unassigned: WorkflowSummary[] = []
      for (const [, summary] of summaries) {
        const empId = workflowAssignments.get(summary.workspacePath)
        if (empId && empMap.has(empId)) {
          const empData = empMap.get(empId)!
          empData.workflows.push(summary)
          if (summary.latestStatus === 'running') empData.runningNow++
          if (summary.latestStatus === 'completed') empData.completedToday++
        } else {
          unassigned.push(summary)
        }
      }

      // Add unassigned as a virtual "Unassigned" employee at the end
      const allEmployeeWorkflows = Array.from(empMap.values())
      if (unassigned.length > 0) {
        let unassignedRunning = 0
        let unassignedCompleted = 0
        for (const wf of unassigned) {
          if (wf.latestStatus === 'running') unassignedRunning++
          if (wf.latestStatus === 'completed') unassignedCompleted++
        }
        allEmployeeWorkflows.push({
          employee: {
            id: '__unassigned__',
            name: 'Unassigned',
            avatar_color: '#9ca3af',
            description: 'Workflows not assigned to any employee',
            created_at: '',
            updated_at: '',
          },
          workflows: unassigned,
          totalCost: 0,
          completedToday: unassignedCompleted,
          runningNow: unassignedRunning,
        })
      }
      setEmployeeWorkflows(allEmployeeWorkflows)
    } catch (err) {
      console.error('Failed to load employee dashboard:', err)
    }
    setLoading(false)
  }, [workflowPresets])

  useEffect(() => {
    if (!workflowPresetsLoaded && workflowPresets.length === 0) {
      if (!presetsLoading) {
        refreshPresets().catch(error => {
          console.error('[EmployeeDashboard] Failed to refresh workflow presets:', error)
        })
      }
      return
    }
    if (presetsLoading && workflowPresets.length === 0) return
    loadData()
  }, [showWorkflowsOverview, workflowPresetsLoaded, presetsLoading, workflowPresets.length, refreshPresets, loadData])

  const workflowEntries = useMemo<SelectedWorkflowEntry[]>(() => {
    return employeeWorkflows.flatMap(({ employee, workflows }) =>
      workflows.map(workflow => ({ employee, workflow }))
    )
  }, [employeeWorkflows])

  const selectedWorkflowEntry = useMemo<SelectedWorkflowEntry | null>(() => {
    if (workflowEntries.length === 0) return null
    if (!selectedWorkflowId) return workflowEntries[0]
    return workflowEntries.find(entry => entry.workflow.workspacePath === selectedWorkflowId) || workflowEntries[0]
  }, [workflowEntries, selectedWorkflowId])

  useEffect(() => {
    if (workflowEntries.length === 0) {
      if (selectedWorkflowId !== null) setSelectedWorkflowId(null)
      return
    }

    if (!selectedWorkflowId || !workflowEntries.some(entry => entry.workflow.workspacePath === selectedWorkflowId)) {
      setSelectedWorkflowId(workflowEntries[0].workflow.workspacePath)
    }
  }, [selectedWorkflowId, workflowEntries])

  const loadWorkflowReview = useCallback(async (entry: SelectedWorkflowEntry | null) => {
    if (!entry || !entry.workflow.workspacePath || !entry.workflow.latestRunFolder) {
      setReviewState(EMPTY_REVIEW_STATE)
      return
    }

    const workspacePath = entry.workflow.workspacePath
    const runFolder = entry.workflow.latestRunFolder

    setReviewState({
      ...EMPTY_REVIEW_STATE,
      loading: true,
    })

    try {
      const reviewData = await agentApi.getWorkflowReviewData(workspacePath, runFolder)
      const evaluationResponse = reviewData.evaluations
      const costsResponse = reviewData.costs

      let evaluation: EvaluationReportEntry | null = null
      let evaluations: EvaluationReportEntry[] = []
      let evaluationAggregate: EvaluationAggregate | null = null
      let evaluationError: string | null = null
      if (evaluationResponse?.success) {
        evaluations = Array.isArray(evaluationResponse.reports) ? evaluationResponse.reports : []
        evaluationAggregate = evaluationResponse.aggregate || null
        evaluation = evaluations.find(item => runFolderMatches(item.run_folder, runFolder)) || evaluations[0] || null
      } else if (evaluationResponse?.error) {
        evaluationError = evaluationResponse.error
      } else {
        evaluationError = 'Failed to load evaluation'
      }

      let tokenUsage: TokenUsageFile | null = null
      let evaluationTokenUsage: TokenUsageFile | null = null
      let costRuns: WorkflowRunCostsEntry[] = []
      let phaseDailyCosts: WorkflowPhaseDailyCostsEntry[] = []
      let costError: string | null = null
      if (costsResponse?.success) {
        costRuns = costsResponse.runs || []
        phaseDailyCosts = costsResponse.phase_daily_costs || []
        tokenUsage = mergeTokenUsageFiles(costRuns.map(r => r.token_usage))
        evaluationTokenUsage = mergeTokenUsageFiles(costRuns.map(r => r.evaluation_token_usage))
      } else {
        costError = 'Failed to load cost data'
      }

      setReviewState({
        loading: false,
        reviewData,
        evaluation,
        evaluations,
        evaluationAggregate,
        evaluationError,
        tokenUsage,
        evaluationTokenUsage,
        costRuns,
        phaseDailyCosts,
        costError,
      })
    } catch (err) {
      setReviewState({
        loading: false,
        reviewData: null,
        evaluation: null,
        evaluations: [],
        evaluationAggregate: null,
        evaluationError: err instanceof Error ? err.message : 'Failed to load evaluation',
        tokenUsage: null,
        evaluationTokenUsage: null,
        costRuns: [],
        phaseDailyCosts: [],
        costError: err instanceof Error ? err.message : 'Failed to load cost data',
      })
    }
  }, [])

  useEffect(() => {
    loadWorkflowReview(selectedWorkflowEntry)
  }, [loadWorkflowReview, selectedWorkflowEntry])

  // Default the selected iteration to the matched/latest one when evaluations load
  useEffect(() => {
    if (reviewState.evaluations.length === 0) {
      setSelectedEvalRunFolder(null)
      return
    }
    const exists = reviewState.evaluations.some(e => e.run_folder === selectedEvalRunFolder)
    if (!exists) {
      setSelectedEvalRunFolder(reviewState.evaluation?.run_folder || reviewState.evaluations[0].run_folder)
    }
  }, [reviewState.evaluations, reviewState.evaluation, selectedEvalRunFolder])

  const handleAssign = useCallback(async (workspacePath: string | null, employeeId: string | null) => {
    if (!workspacePath) return
    await agentApi.assignWorkflowEmployee(workspacePath, employeeId)
    setAssigningWorkflow(null)
    loadData()
  }, [loadData])

  const handleSelectWorkflow = useCallback((workflowPath: string, nextTab?: ReviewTab) => {
    setSelectedWorkflowId(workflowPath)
    if (nextTab) setReviewTab(nextTab)
  }, [])

  const toggleEmployeeCollapsed = useCallback((employeeId: string) => {
    setCollapsedEmployeeIds(prev => {
      const next = new Set(prev)
      if (next.has(employeeId)) next.delete(employeeId)
      else next.add(employeeId)
      return next
    })
  }, [])

  const selectedWorkflow = selectedWorkflowEntry?.workflow || null
  const selectedEmployee = selectedWorkflowEntry?.employee || null
  const executionCost = getTokenUsageTotal(reviewState.tokenUsage)
  const evaluationCost = getTokenUsageTotal(reviewState.evaluationTokenUsage)
  const totalKnownCost = (executionCost || 0) + (evaluationCost || 0)

  // Latest-run figures for the header chips. "Latest" = the run_folder the
  // workspace reports as most recent, with a fallback to the run whose cost
  // file has the newest updated_at if that folder isn't present in costs.
  const latestRunFolder = selectedWorkflow?.latestRunFolder || null
  const latestEvalPercent = useMemo(() => {
    if (reviewState.evaluations.length === 0) return null
    const matched = latestRunFolder
      ? reviewState.evaluations.find(e => runFolderMatches(e.run_folder, latestRunFolder))
      : null
    const entry = matched || [...reviewState.evaluations].sort((a, b) =>
      (Date.parse(b.report.generated_at) || 0) - (Date.parse(a.report.generated_at) || 0)
    )[0]
    return entry ? Math.round(entry.report.score_percentage) : null
  }, [reviewState.evaluations, latestRunFolder])

  const latestRunCost = useMemo(() => {
    if (reviewState.costRuns.length === 0) return null
    const sumCost = (usage: TokenUsageFile | null | undefined): number => {
      if (!usage) return 0
      let total = 0
      for (const m of Object.values(usage.by_model || {})) total += m.total_cost_usd || 0
      return total
    }
    const pickTs = (run: WorkflowRunCostsEntry): number => {
      const stamps = [
        run.token_usage?.updated_at,
        run.evaluation_token_usage?.updated_at,
        run.token_usage?.created_at,
        run.evaluation_token_usage?.created_at,
      ].map(s => (s ? Date.parse(s) : 0))
      return Math.max(0, ...stamps)
    }
    const matched = latestRunFolder
      ? reviewState.costRuns.find(r => runFolderMatches(r.run_folder, latestRunFolder))
      : null
    const run = matched || [...reviewState.costRuns].sort((a, b) => pickTs(b) - pickTs(a))[0]
    if (!run) return null
    return sumCost(run.token_usage) + sumCost(run.evaluation_token_usage)
  }, [reviewState.costRuns, latestRunFolder])
  // Evaluation trend data: one point per report. X is a numeric timestamp so
  // points land at their real time (including multiple same-day runs).
  // recharts needs one row per distinct x; same-timestamp rows are merged.
  const evalTrend = useMemo(() => {
    type Row = { ts: number; [group: string]: number | null }
    const rowByTs = new Map<number, Row>()
    const groupSet = new Set<string>()

    for (const entry of reviewState.evaluations) {
      const parsed = parseRunFolder(entry.run_folder)
      groupSet.add(parsed.group)

      const parsedDate = new Date(entry.report.generated_at)
      if (Number.isNaN(parsedDate.getTime())) continue
      const ts = parsedDate.getTime()
      const row = rowByTs.get(ts) || { ts }
      row[parsed.group] = entry.report.score_percentage
      rowByTs.set(ts, row)
    }

    const rows = Array.from(rowByTs.values()).sort((a, b) => a.ts - b.ts)
    const groups = Array.from(groupSet).sort()
    return { rows, groups }
  }, [reviewState.evaluations])

  // Cost trend: daily totals per day, split into execution / evaluation / phase
  // (phase = workflow-builder/planning/etc, not tied to a specific run).
  const costTrend = useMemo(() => {
    type Row = { date: string; dateLabel: string; ts: number; total: number; execution: number; evaluation: number; phase: number }
    const rowByDate = new Map<string, Row>()

    const sumCost = (usage: TokenUsageFile | PhaseTokenUsageFile | null | undefined): number => {
      if (!usage) return 0
      let total = 0
      for (const m of Object.values(usage.by_model || {})) {
        total += m.total_cost_usd || 0
      }
      return total
    }

    const pickTimestamp = (usage: TokenUsageFile | PhaseTokenUsageFile | null | undefined): number | null => {
      const ts = usage?.updated_at || usage?.created_at
      if (!ts) return null
      const parsed = new Date(ts)
      return Number.isNaN(parsed.getTime()) ? null : parsed.getTime()
    }

    const parseDateKey = (key: string): number | null => {
      const parsed = new Date(`${key}T00:00:00Z`)
      return Number.isNaN(parsed.getTime()) ? null : parsed.getTime()
    }

    const bump = (ts: number | null, field: 'execution' | 'evaluation' | 'phase', amount: number) => {
      if (ts === null || amount <= 0) return
      const d = new Date(ts)
      const date = d.toISOString().slice(0, 10)
      const dateLabel = d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
      const row = rowByDate.get(date) || { date, dateLabel, ts: d.getTime(), total: 0, execution: 0, evaluation: 0, phase: 0 }
      row[field] += amount
      row.total += amount
      if (d.getTime() > row.ts) row.ts = d.getTime()
      rowByDate.set(date, row)
    }

    for (const run of reviewState.costRuns) {
      bump(pickTimestamp(run.token_usage), 'execution', sumCost(run.token_usage))
      bump(pickTimestamp(run.evaluation_token_usage), 'evaluation', sumCost(run.evaluation_token_usage))
    }

    for (const entry of reviewState.phaseDailyCosts) {
      // The phase daily file's own date key is authoritative (e.g. "2026-04-21");
      // its token_usage timestamp may drift if the file was rewritten later.
      const ts = parseDateKey(entry.date) ?? pickTimestamp(entry.token_usage)
      bump(ts, 'phase', sumCost(entry.token_usage))
    }

    const rows = Array.from(rowByDate.values()).sort((a, b) => a.date.localeCompare(b.date))
    return { rows }
  }, [reviewState.costRuns, reviewState.phaseDailyCosts])

  const currentEvalEntry = useMemo(() => {
    if (!selectedEvalRunFolder) return reviewState.evaluation
    return reviewState.evaluations.find(e => e.run_folder === selectedEvalRunFolder) || reviewState.evaluation
  }, [reviewState.evaluation, reviewState.evaluations, selectedEvalRunFolder])

  // Per-group summary: one row per group with runs, latest, best, avg. Sorted
  // by latest score descending (worst-performing groups sink so they're easy
  // to scan for issues; ties fall back to group name).
  const evalGroupsSummary = useMemo(() => {
    type GroupStats = {
      group: string
      runs: number
      latest: number
      latestRunFolder: string
      latestGeneratedAt: string
      best: number
      avg: number
    }
    const byGroup = new Map<string, EvaluationReportEntry[]>()
    for (const entry of reviewState.evaluations) {
      const { group } = parseRunFolder(entry.run_folder)
      const list = byGroup.get(group) || []
      list.push(entry)
      byGroup.set(group, list)
    }

    const rows: GroupStats[] = []
    for (const [group, entries] of byGroup.entries()) {
      const sorted = [...entries].sort((a, b) => {
        const ta = Date.parse(a.report.generated_at) || 0
        const tb = Date.parse(b.report.generated_at) || 0
        return tb - ta
      })
      const latest = sorted[0]
      const scores = entries.map(e => e.report.score_percentage)
      rows.push({
        group,
        runs: entries.length,
        latest: latest.report.score_percentage,
        latestRunFolder: latest.run_folder,
        latestGeneratedAt: latest.report.generated_at,
        best: Math.max(...scores),
        avg: scores.reduce((s, v) => s + v, 0) / scores.length,
      })
    }

    rows.sort((a, b) => b.latest - a.latest || a.group.localeCompare(b.group))
    return rows
  }, [reviewState.evaluations])

  const toggleEvalStep = useCallback((stepKey: string) => {
    setExpandedEvalSteps(prev => {
      const next = new Set(prev)
      if (next.has(stepKey)) next.delete(stepKey)
      else next.add(stepKey)
      return next
    })
  }, [])

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="w-6 h-6 animate-spin text-indigo-500" />
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="grid gap-5 lg:grid-cols-[minmax(320px,4fr)_minmax(0,6fr)]">
          <div className="space-y-4">
            {employeeWorkflows.length === 0 && (
              <div className="rounded-2xl border border-dashed border-border bg-card px-6 py-16 text-center">
                <UserCircle2 className="mx-auto mb-4 h-14 w-14 text-muted-foreground/60" />
                <p className="text-base font-medium text-foreground">No employees found.</p>
                <p className="mt-1 text-sm text-muted-foreground">The page is waiting for employee records from the filesystem config.</p>
              </div>
            )}

            {employeeWorkflows.map(({ employee, workflows, runningNow }) => {
              const isCollapsed = collapsedEmployeeIds.has(employee.id)

              return (
                <div
                  key={employee.id}
                  className={`overflow-hidden rounded-2xl border ${employee.id === '__unassigned__' ? 'border-dashed border-border' : 'border-border'} bg-card shadow-sm`}
                >
                <div className="bg-muted/30 px-5 py-4">
                  <div className="flex items-center justify-between gap-4">
                    <div className="flex min-w-0 items-center gap-3">
                      <div className="min-w-0">
                        <h4 className="font-semibold text-foreground">{employee.name}</h4>
                        {employee.description && (
                          <p className="mt-1 text-sm text-muted-foreground">{employee.description}</p>
                        )}
                      </div>
                    </div>
                    <div className="flex flex-wrap items-center gap-2 text-xs">
                      {runningNow > 0 && (
                        <span className="inline-flex items-center gap-1 rounded-full bg-info/10 px-2.5 py-1 font-medium text-info">
                          <PlayCircle className="h-3.5 w-3.5" />
                          {runningNow} running
                        </span>
                      )}
                      <button
                        type="button"
                        onClick={() => toggleEmployeeCollapsed(employee.id)}
                        className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-2.5 py-1 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                        aria-expanded={!isCollapsed}
                        aria-label={`${isCollapsed ? 'Expand' : 'Collapse'} ${employee.name}`}
                      >
                        {isCollapsed ? <ChevronRight className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                        <span>{isCollapsed ? 'Expand' : 'Collapse'}</span>
                      </button>
                    </div>
                  </div>
                </div>

                {!isCollapsed && (workflows.length > 0 ? (
                  <div className={`border-t ${employee.id === '__unassigned__' ? 'border-dashed' : ''} border-border divide-y divide-border`}>
                    {workflows.map(wf => {
                      const isSelected = selectedWorkflow?.workspacePath === wf.workspacePath
                      return (
                        <div
                          key={wf.workspacePath}
                          className={`px-5 py-3 cursor-pointer transition-colors ${
                            isSelected
                              ? 'border-l-2 border-l-primary bg-primary/10'
                              : 'border-l-2 border-l-transparent hover:bg-muted/40'
                          }`}
                          onClick={() => handleSelectWorkflow(wf.workspacePath)}
                        >
                          <div className="flex items-start justify-between gap-3">
                            <div className="min-w-0 flex-1">
                              <div className="flex items-center gap-2 min-w-0">
                                <StatusDot status={wf.latestStatus} />
                                <span className="truncate text-sm font-medium text-foreground">{wf.label}</span>
                                {isSelected && (
                                  <span className="rounded-full bg-primary/15 px-1.5 py-0.5 text-[10px] font-medium text-primary">
                                    Selected
                                  </span>
                                )}
                              </div>
                              <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
                                {wf.latestRunFolder ? (
                                  <span className="inline-flex items-center gap-1">
                                    <FileText className="w-3 h-3" />
                                    {wf.latestRunFolder}
                                  </span>
                                ) : (
                                  <span>No run yet</span>
                                )}
                                {wf.totalRuns > 0 && <span>{wf.totalRuns} runs</span>}
                                {wf.nextScheduleAt ? (
                                  <span className="inline-flex items-center gap-1 text-warning">
                                    <Calendar className="w-3 h-3" />
                                    {formatScheduleTime(wf.nextScheduleAt)}
                                  </span>
                                ) : wf.scheduleCount > 0 ? (
                                  <span>{wf.scheduleCount} schedules</span>
                                ) : null}
                                {wf.lastActive && <span>{formatTimestamp(wf.lastActive)}</span>}
                              </div>
                            </div>

                            <div className="flex items-center gap-2 flex-wrap justify-end" onClick={e => e.stopPropagation()}>
                              {employees.length > 0 && (
                                <div className="relative">
                                  <button
                                    onClick={() => setAssigningWorkflow(assigningWorkflow === wf.workspacePath ? null : wf.workspacePath)}
                                    className="inline-flex h-7 w-7 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                                    aria-label={employee.id === '__unassigned__' ? 'Assign workflow' : 'Reassign workflow'}
                                    title={employee.id === '__unassigned__' ? 'Assign workflow' : 'Reassign workflow'}
                                  >
                                    {employee.id === '__unassigned__' ? (
                                      <UserPlus className="h-3.5 w-3.5" />
                                    ) : (
                                      <UserCog className="h-3.5 w-3.5" />
                                    )}
                                  </button>
                                  {assigningWorkflow === wf.workspacePath && (
                                    <div className="absolute right-0 top-full z-10 mt-1 w-48 rounded-xl border border-border bg-popover py-1 shadow-lg">
                                      {employee.id !== '__unassigned__' && (
                                        <button
                                          onClick={() => handleAssign(wf.workspacePath, null)}
                                          className="w-full flex items-center gap-2 px-3 py-1.5 text-left text-sm text-popover-foreground transition-colors hover:bg-muted"
                                        >
                                          <X className="w-3.5 h-3.5" />
                                          <span>Unassign</span>
                                        </button>
                                      )}
                                      {employees.filter(e => e.id !== '__unassigned__' && e.id !== employee.id).map(emp => (
                                        <button
                                          key={emp.id}
                                          onClick={() => handleAssign(wf.workspacePath, emp.id)}
                                          className="w-full flex items-center gap-2 px-3 py-1.5 text-left text-sm transition-colors hover:bg-muted"
                                        >
                                          <EmployeeAvatar name={emp.name} color={emp.avatar_color} size="sm" />
                                          <span className="text-popover-foreground">{emp.name}</span>
                                        </button>
                                      ))}
                                    </div>
                                  )}
                                </div>
                              )}
                            </div>
                          </div>
                        </div>
                      )
                    })}
                  </div>
                ) : (
                  <div className="border-t border-border px-5 py-3 text-xs text-muted-foreground">
                    No workflows assigned yet
                  </div>
                ))}
              </div>
              )
            })}
          </div>

          <div className="lg:sticky lg:top-6 self-start">
            <div className="overflow-hidden rounded-2xl border border-border bg-card shadow-sm">
              <div className="border-b border-border bg-muted/30 px-5 py-4">
                {selectedWorkflow && selectedEmployee ? (
                  <>
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex items-center gap-3">
                          <div className="min-w-0">
                            <h4 className="truncate text-base font-semibold text-foreground">
                              {selectedWorkflow.label}
                            </h4>
                            <p className="mt-0.5 text-xs text-muted-foreground">
                              {selectedEmployee.name}
                            </p>
                          </div>
                        </div>
                      </div>
                      <div className="flex items-center gap-2">
                        {selectedWorkflow.latestRunFolder && (
                          <button
                            onClick={() => setLogsState({ workspacePath: selectedWorkflow.workspacePath, runFolder: selectedWorkflow.latestRunFolder! })}
                            className="rounded-lg px-2.5 py-1 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                          >
                            Logs
                          </button>
                        )}
                      </div>
                    </div>

                    <div className="mt-3 flex flex-wrap gap-2 text-[11px]">
                      {selectedWorkflow.latestRunFolder ? (
                        <span className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-2 py-1 text-muted-foreground">
                          <FileText className="w-3 h-3" />
                          {selectedWorkflow.latestRunFolder}
                        </span>
                      ) : (
                        <span className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-2 py-1 text-muted-foreground">
                          No runs yet
                        </span>
                      )}
                      {latestEvalPercent !== null && (
                        <span
                          className={`inline-flex items-center gap-1 rounded-full px-2 py-1 ${getEvalBadgeClasses(latestEvalPercent)}`}
                          title="Evaluation score of the latest run"
                        >
                          <BarChart3 className="w-3 h-3" />
                          Eval {latestEvalPercent}%
                        </span>
                      )}
                      {latestRunCost !== null && latestRunCost > 0 && (
                        <span
                          className="inline-flex items-center gap-1 rounded-full bg-warning/15 px-2 py-1 text-warning"
                          title="Cost of the latest run (execution + evaluation)"
                        >
                          <DollarSign className="w-3 h-3" />
                          {formatUsd(latestRunCost)}
                        </span>
                      )}
                      {selectedWorkflow.nextScheduleAt && (
                        <span className="inline-flex items-center gap-1 rounded-full bg-primary/15 px-2 py-1 text-primary">
                          <Calendar className="w-3 h-3" />
                          {formatScheduleTime(selectedWorkflow.nextScheduleAt)}
                        </span>
                      )}
                      {selectedWorkflow.lastActive && (
                        <span className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-2 py-1 text-muted-foreground">
                          <Clock className="w-3 h-3" />
                          {formatTimestamp(selectedWorkflow.lastActive)}
                        </span>
                      )}
                    </div>
                  </>
                ) : (
                  <div>
                    <h4 className="text-base font-semibold text-foreground">Latest report</h4>
                    <p className="mt-1 text-xs text-muted-foreground">
                      Select a workflow to review its report, evaluation, and cost.
                    </p>
                  </div>
                )}
              </div>

              <div className="border-b border-border px-5 py-3">
                <div className="inline-flex items-center gap-1 rounded-xl bg-muted/60 p-1">
                  <ReviewTabButton active={reviewTab === 'report'} label="Report" onClick={() => setReviewTab('report')} />
                  <ReviewTabButton active={reviewTab === 'evaluation'} label="Evaluation" onClick={() => setReviewTab('evaluation')} />
                  <ReviewTabButton active={reviewTab === 'cost'} label="Cost" onClick={() => setReviewTab('cost')} />
                </div>
              </div>

              <div className="max-h-[calc(100vh-240px)] overflow-y-auto p-5">
                {!selectedWorkflow ? (
                  <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                    Select a workflow from the left to review what this employee produced.
                  </div>
                ) : !selectedWorkflow.latestRunFolder ? (
                  <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                    This workflow has not produced a run yet, so there is no report, evaluation, or cost data to review.
                  </div>
                ) : reviewState.loading ? (
                  <div className="flex items-center justify-center py-16 text-sm text-muted-foreground">
                    <Loader2 className="mr-2 h-5 w-5 animate-spin text-cyan-500" />
                    Loading latest workflow review data...
                  </div>
                ) : reviewTab === 'report' ? (
                  <div className="h-[calc(100vh-320px)] min-h-[400px]">
                    <ReportView workspacePath={selectedWorkflow.workspacePath} selectedRunFolder={selectedWorkflow.latestRunFolder} reviewData={reviewState.reviewData} />
                  </div>
                ) : reviewTab === 'evaluation' ? (
                  currentEvalEntry ? (
                    (() => {
                      const current = currentEvalEntry
                      const currentParsed = parseRunFolder(current.run_folder)
                      const pct = current.report.score_percentage
                      const agg = reviewState.evaluationAggregate
                      return (
                        <div className="space-y-4">
                          {/* Selected iteration/group header */}
                          <div className="grid gap-3 sm:grid-cols-2">
                            <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                              <div className="flex items-center justify-between">
                                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Overall Score</div>
                                <span className={`rounded-full px-2 py-0.5 text-[10px] font-semibold ${getEvalBadgeClasses(pct)}`}>
                                  {formatPercent(pct)}
                                </span>
                              </div>
                              <div className={`mt-2 text-2xl font-semibold ${getScoreTextColor(pct)}`}>
                                {formatPercent(pct)}
                              </div>
                              <div className="mt-2 h-2 w-full overflow-hidden rounded-full bg-muted">
                                <div
                                  className={`h-full transition-all ${getScoreBarColor(pct)}`}
                                  style={{ width: `${Math.min(100, Math.max(0, pct))}%` }}
                                />
                              </div>
                              <div className="mt-2 text-xs text-muted-foreground">
                                {current.report.total_score} / {current.report.max_possible_score}
                              </div>
                            </div>
                            <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                              <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Selected run</div>
                              <div className="mt-2 flex flex-wrap items-center gap-1.5">
                                <span className="rounded bg-background px-1.5 py-0.5 font-mono text-xs text-foreground ring-1 ring-border">
                                  {currentParsed.iteration}
                                </span>
                                <span className="text-muted-foreground">/</span>
                                <span className="rounded bg-background px-1.5 py-0.5 font-mono text-xs text-foreground ring-1 ring-border">
                                  {currentParsed.group}
                                </span>
                              </div>
                              <div className="mt-2 text-xs text-muted-foreground">
                                {formatScheduleTime(current.report.generated_at)}
                              </div>
                              {agg && agg.total_runs > 1 && (
                                <div className="mt-1 text-xs text-muted-foreground">
                                  {agg.total_runs} runs · avg {formatPercent(agg.average_percentage)}
                                </div>
                              )}
                            </div>
                          </div>

                          {/* Trend over time (line per group) */}
                          {evalTrend.rows.length >= 1 && (
                            <div className="rounded-xl border border-border bg-card px-4 py-3">
                              <div className="mb-2 flex items-center justify-between">
                                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Scores over time</div>
                                <div className="text-[11px] text-muted-foreground">
                                  {evalTrend.rows.length} run{evalTrend.rows.length !== 1 ? 's' : ''} · {evalTrend.groups.length} group{evalTrend.groups.length !== 1 ? 's' : ''}
                                </div>
                              </div>
                              <div className="h-52 w-full">
                                <ResponsiveContainer width="100%" height="100%">
                                  <LineChart data={evalTrend.rows} margin={{ top: 8, right: 12, left: -12, bottom: 0 }}>
                                    <CartesianGrid strokeDasharray="3 3" stroke="currentColor" className="text-border" opacity={0.3} />
                                    <XAxis
                                      dataKey="ts"
                                      type="number"
                                      scale="time"
                                      domain={['dataMin', 'dataMax']}
                                      tickFormatter={v => new Date(v).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}
                                      fontSize={11}
                                      tick={{ fill: 'currentColor' }}
                                      className="text-muted-foreground"
                                    />
                                    <YAxis domain={[0, 100]} fontSize={11} tick={{ fill: 'currentColor' }} className="text-muted-foreground" tickFormatter={v => `${v}%`} />
                                    <Tooltip
                                      labelFormatter={(v: unknown) => typeof v === 'number' ? new Date(v).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : String(v)}
                                      formatter={(value: unknown, name: unknown) => [
                                        typeof value === 'number' ? `${value.toFixed(1)}%` : String(value),
                                        String(name),
                                      ]}
                                      contentStyle={{ fontSize: 12, borderRadius: 6 }}
                                    />
                                    {evalTrend.groups.length > 1 && <Legend wrapperStyle={{ fontSize: 11 }} />}
                                    {evalTrend.groups.map((g, idx) => (
                                      <Line
                                        key={g}
                                        type="monotone"
                                        dataKey={g}
                                        stroke={GROUP_LINE_COLORS[idx % GROUP_LINE_COLORS.length]}
                                        strokeWidth={2}
                                        dot={{ r: 3 }}
                                        activeDot={{ r: 5 }}
                                        connectNulls
                                      />
                                    ))}
                                  </LineChart>
                                </ResponsiveContainer>
                              </div>
                            </div>
                          )}

                          {/* Per-group summary */}
                          {evalGroupsSummary.length > 0 && (
                            <div className="overflow-hidden rounded-xl border border-border bg-card">
                              <div className="flex items-center justify-between border-b border-border px-4 py-2">
                                <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                                  Groups
                                </div>
                                <div className="text-[11px] text-muted-foreground">
                                  {evalGroupsSummary.length} group{evalGroupsSummary.length !== 1 ? 's' : ''}
                                </div>
                              </div>
                              <div className="grid grid-cols-[1fr_auto_auto_auto_auto] items-center gap-x-6 border-b border-border bg-muted/20 px-4 py-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                                <div>Group</div>
                                <div className="text-right">Runs</div>
                                <div className="text-right">Latest</div>
                                <div className="text-right">Best</div>
                                <div className="text-right">Avg</div>
                              </div>
                              <div className="divide-y divide-border">
                                {evalGroupsSummary.map(row => {
                                  const isCurrent = row.latestRunFolder === current.run_folder
                                  return (
                                    <button
                                      key={row.group}
                                      type="button"
                                      onClick={() => setSelectedEvalRunFolder(row.latestRunFolder)}
                                      className={`grid w-full grid-cols-[1fr_auto_auto_auto_auto] items-center gap-x-6 px-4 py-2 text-left text-sm transition-colors hover:bg-accent/50 ${
                                        isCurrent ? 'bg-primary/5' : ''
                                      }`}
                                      title={`Latest: ${row.latestRunFolder} (${new Date(row.latestGeneratedAt).toLocaleString()})`}
                                    >
                                      <div className="flex items-center gap-2">
                                        <span className="font-mono text-foreground">{row.group}</span>
                                        {isCurrent && (
                                          <span className="rounded-full bg-primary/15 px-1.5 py-0.5 text-[10px] font-medium text-primary">Selected</span>
                                        )}
                                      </div>
                                      <div className="text-right text-muted-foreground">{row.runs}</div>
                                      <div className="text-right font-medium text-foreground">{formatPercent(row.latest)}</div>
                                      <div className="text-right text-muted-foreground">{formatPercent(row.best)}</div>
                                      <div className="text-right text-muted-foreground">{formatPercent(row.avg)}</div>
                                    </button>
                                  )
                                })}
                              </div>
                            </div>
                          )}

                          {/* Step breakdown */}
                          <div className="space-y-2">
                            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
                              Step scores ({current.report.step_scores.length})
                            </div>
                            {current.report.step_scores.map((step, idx) => {
                              const stepPct = step.max_score > 0 ? (step.score / step.max_score) * 100 : 0
                              const stepKey = `${current.run_folder}-${step.step_id}-${idx}`
                              const isExpanded = expandedEvalSteps.has(stepKey)
                              return (
                                <div key={stepKey} className="overflow-hidden rounded-xl border border-border bg-card">
                                  <button
                                    type="button"
                                    onClick={() => toggleEvalStep(stepKey)}
                                    className="flex w-full items-center gap-3 px-4 py-2.5 text-left transition-colors hover:bg-accent/50"
                                  >
                                    {isExpanded ? (
                                      <ChevronDown className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
                                    ) : (
                                      <ChevronRight className="h-4 w-4 flex-shrink-0 text-muted-foreground" />
                                    )}
                                    <div className="min-w-0 flex-1">
                                      <div className="flex items-center gap-2">
                                        <span className="rounded bg-muted px-1 py-0.5 font-mono text-[10px] text-muted-foreground">#{idx + 1}</span>
                                        <span className="truncate text-sm font-medium text-foreground">{step.step_id}</span>
                                      </div>
                                      <div className="mt-1.5 flex items-center gap-2">
                                        <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                                          <div
                                            className={`h-full transition-all ${getScoreBarColor(stepPct)}`}
                                            style={{ width: `${Math.min(100, Math.max(0, stepPct))}%` }}
                                          />
                                        </div>
                                        <span className={`text-[11px] font-medium ${getScoreTextColor(stepPct)}`}>
                                          {step.score}/{step.max_score}
                                        </span>
                                      </div>
                                    </div>
                                  </button>
                                  {isExpanded && (step.reasoning || step.evidence) && (
                                    <div className="space-y-3 border-t border-border bg-muted/20 px-4 py-3">
                                      {step.reasoning && (
                                        <div>
                                          <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Reasoning</div>
                                          <p className="whitespace-pre-wrap text-xs text-foreground">{step.reasoning}</p>
                                        </div>
                                      )}
                                      {step.evidence && (
                                        <div>
                                          <div className="mb-1 text-[11px] font-semibold uppercase tracking-wide text-muted-foreground">Evidence</div>
                                          <pre className="max-h-40 overflow-auto whitespace-pre-wrap rounded border border-border bg-background p-2 text-[11px]">
                                            {step.evidence}
                                          </pre>
                                        </div>
                                      )}
                                    </div>
                                  )}
                                </div>
                              )
                            })}
                          </div>
                        </div>
                      )
                    })()
                  ) : (
                    <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                      {reviewState.evaluationError || 'No evaluation report exists for the latest run yet.'}
                    </div>
                  )
                ) : (
                  <div className="space-y-4">
                    {(() => {
                      const phaseCostTotal = costTrend.rows.reduce((sum, r) => sum + r.phase, 0)
                      const grandTotal = (totalKnownCost || 0) + phaseCostTotal
                      return (
                        <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Total cost</div>
                          <div className="mt-2 text-2xl font-semibold text-foreground">{formatUsd(grandTotal > 0 ? grandTotal : null)}</div>
                          <div className="mt-1 text-xs text-muted-foreground">
                            {formatUsd(executionCost)} execution · {formatUsd(evaluationCost)} evaluation · {formatUsd(phaseCostTotal > 0 ? phaseCostTotal : null)} builder
                          </div>
                        </div>
                      )
                    })()}

                    {reviewState.costError && !reviewState.tokenUsage && !reviewState.evaluationTokenUsage && (
                      <div className="rounded-2xl border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
                        {reviewState.costError}
                      </div>
                    )}

                    {!reviewState.costError && !reviewState.tokenUsage && !reviewState.evaluationTokenUsage && (
                      <div className="rounded-2xl border border-dashed border-border p-6 text-center text-sm text-muted-foreground">
                        No cost data has been recorded for the latest run yet.
                      </div>
                    )}

                    {costTrend.rows.length >= 1 && (
                      <div className="rounded-xl border border-border bg-card px-4 py-3">
                        <div className="mb-2 flex items-center justify-between">
                          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Cost over time</div>
                          <div className="text-[11px] text-muted-foreground">
                            {costTrend.rows.length} day{costTrend.rows.length !== 1 ? 's' : ''}
                          </div>
                        </div>
                        <div className="h-52 w-full">
                          <ResponsiveContainer width="100%" height="100%">
                            <BarChart data={costTrend.rows} margin={{ top: 8, right: 12, left: -8, bottom: 0 }}>
                              <CartesianGrid strokeDasharray="3 3" stroke="currentColor" className="text-border" opacity={0.3} />
                              <XAxis dataKey="dateLabel" fontSize={11} tick={{ fill: 'currentColor' }} className="text-muted-foreground" />
                              <YAxis fontSize={11} tick={{ fill: 'currentColor' }} className="text-muted-foreground" tickFormatter={v => `$${v}`} />
                              <Tooltip
                                formatter={(value: unknown, name: unknown) => [
                                  typeof value === 'number' ? `$${value.toFixed(2)}` : String(value),
                                  String(name),
                                ]}
                                contentStyle={{ fontSize: 12, borderRadius: 6 }}
                              />
                              <Bar dataKey="total" name="Total" fill="#6366f1" />
                            </BarChart>
                          </ResponsiveContainer>
                        </div>
                      </div>
                    )}

                    {costTrend.rows.length >= 1 && (
                      <div className="overflow-hidden rounded-xl border border-border">
                        <div className="border-b border-border bg-muted/30 px-4 py-3">
                          <div className="text-sm font-medium text-foreground">Cost by day</div>
                        </div>
                        <div className="grid grid-cols-[1fr_auto_auto_auto_auto] items-center gap-x-6 border-b border-border bg-muted/20 px-4 py-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                          <div>Date</div>
                          <div className="text-right">Execution</div>
                          <div className="text-right">Evaluation</div>
                          <div className="text-right">Builder</div>
                          <div className="text-right">Total</div>
                        </div>
                        <div className="divide-y divide-border">
                          {[...costTrend.rows].reverse().map(row => (
                            <div key={row.date} className="grid grid-cols-[1fr_auto_auto_auto_auto] items-center gap-x-6 px-4 py-2.5 text-sm">
                              <div className="text-foreground">{row.dateLabel}</div>
                              <div className="text-right text-muted-foreground">{row.execution > 0 ? formatUsd(row.execution) : '-'}</div>
                              <div className="text-right text-muted-foreground">{row.evaluation > 0 ? formatUsd(row.evaluation) : '-'}</div>
                              <div className="text-right text-muted-foreground">{row.phase > 0 ? formatUsd(row.phase) : '-'}</div>
                              <div className="text-right font-medium text-foreground">{formatUsd(row.total)}</div>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                )}
              </div>
            </div>
          </div>
      </div>

      {logsState && (
        <ExecutionLogsPopup
          isOpen
          onClose={() => setLogsState(null)}
          workspacePath={logsState.workspacePath}
          runFolder={logsState.runFolder}
          runFolders={[logsState.runFolder]}
        />
      )}
    </div>
  )
}

function formatTimestamp(ts: string): string {
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

function formatScheduleTime(ts: string): string {
  try {
    const d = new Date(ts)
    return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
  } catch {
    return ts
  }
}
