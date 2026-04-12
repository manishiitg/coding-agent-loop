import React, { useEffect, useState, useCallback, useMemo } from 'react'
import {
  X, UserCircle2, Workflow,
  PlayCircle, Clock, DollarSign, Loader2, Calendar, FileText, BarChart3, Activity, ChevronDown, ChevronRight
} from 'lucide-react'
import { agentApi } from '../services/api'
import { schedulerApi } from '../api/scheduler'
import type { DiscoveredWorkflow, Employee, EvaluationReportEntry, TokenUsageFile, WorkflowFinalOutputResponse } from '../services/api-types'
import WorkflowScheduleRunsPanel from './scheduler/WorkflowScheduleRunsPanel'
import ExecutionLogsPopup from './workflow/ExecutionLogsPopup'
import { MarkdownRenderer } from './ui/MarkdownRenderer'
import { useAppStore } from '../stores/useAppStore'

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
  report: WorkflowFinalOutputResponse | null
  reportError: string | null
  evaluation: EvaluationReportEntry | null
  evaluationError: string | null
  tokenUsage: TokenUsageFile | null
  evaluationTokenUsage: TokenUsageFile | null
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
  report: null,
  reportError: null,
  evaluation: null,
  evaluationError: null,
  tokenUsage: null,
  evaluationTokenUsage: null,
  costError: null,
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

const getModelUsageRows = (usage: TokenUsageFile | null | undefined): Array<{
  model: string
  totalCostUsd: number
  inputTokens: number
  outputTokens: number
}> => {
  if (!usage) return []

  return Object.entries(usage.by_model || {})
    .map(([model, stats]) => ({
      model,
      totalCostUsd: stats.total_cost_usd || 0,
      inputTokens: stats.input_tokens || 0,
      outputTokens: stats.output_tokens || 0,
    }))
    .sort((a, b) => b.totalCostUsd - a.totalCostUsd)
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

const getEvalBadgeClasses = (evalPercent: number): string => {
  if (evalPercent >= 80) {
    return 'bg-emerald-100 text-emerald-800 dark:bg-emerald-500/15 dark:text-emerald-300 dark:ring-1 dark:ring-emerald-500/20'
  }
  if (evalPercent >= 50) {
    return 'bg-amber-100 text-amber-800 dark:bg-amber-500/15 dark:text-amber-300 dark:ring-1 dark:ring-amber-500/20'
  }
  return 'bg-rose-100 text-rose-800 dark:bg-rose-500/15 dark:text-rose-300 dark:ring-1 dark:ring-rose-500/20'
}

export const EmployeeDashboard: React.FC = () => {
  const showWorkflowsOverview = useAppStore(state => state.showWorkflowsOverview)
  const [employees, setEmployees] = useState<Employee[]>([])
  const [employeeWorkflows, setEmployeeWorkflows] = useState<EmployeeWithWorkflows[]>([])
  const [loading, setLoading] = useState(true)
  const [assigningWorkflow, setAssigningWorkflow] = useState<string | null>(null) // workspace path being assigned
  const [showAllSchedules, setShowAllSchedules] = useState(false)
  const [logsState, setLogsState] = useState<{ workspacePath: string; runFolder: string } | null>(null)
  const [selectedWorkflowId, setSelectedWorkflowId] = useState<string | null>(null)
  const [collapsedEmployeeIds, setCollapsedEmployeeIds] = useState<Set<string>>(new Set())
  const [reviewTab, setReviewTab] = useState<ReviewTab>('report')
  const [reviewState, setReviewState] = useState<WorkflowReviewState>(EMPTY_REVIEW_STATE)

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [empResp, workflowResp] = await Promise.all([
        agentApi.listEmployees(),
        agentApi.listWorkflowManifests(),
      ])
      const schedulesResp = await schedulerApi.listJobs({ entity_type: 'workflow' }).catch(() => ({ jobs: [], total: 0, limit: 0, offset: 0 }))

      const discoveredWorkflows = workflowResp.workflows || []
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
      const allWorkspacePaths = discoveredWorkflows.map((wf: DiscoveredWorkflow) => wf.workspace_path)

      // Fetch summaries + eval reports in parallel (2 calls instead of N*2)
      const [summaryResp, evalResults] = await Promise.all([
        agentApi.getWorkflowsSummary(allWorkspacePaths).catch(() => null),
        Promise.all(
          discoveredWorkflows.map((wf: DiscoveredWorkflow) =>
            agentApi.getEvaluationReports(wf.workspace_path).catch(() => null).then(r => ({ wp: wf.workspace_path, data: r }))
          )
        ),
      ])

      // Index summary results by workspace path
      const summaryByPath = new Map<string, WorkflowApiSummary>()
      if (summaryResp?.success && summaryResp.workflows) {
        for (const ws of summaryResp.workflows) {
          summaryByPath.set(ws.workspace_path, ws)
        }
      }

      // Index eval results by workspace path
      const evalByPath = new Map<string, typeof evalResults[number]['data']>()
      for (const er of evalResults) {
        evalByPath.set(er.wp, er.data)
      }

      for (const workflow of discoveredWorkflows as DiscoveredWorkflow[]) {
        const wp = workflow.workspace_path
        const ws = summaryByPath.get(wp)
        const evalResp = evalByPath.get(wp)
        const sched = scheduleByWorkspace.get(wp)

        let latestStatus = ws?.latest_run?.status || 'unknown'
        const lastActive = ws?.latest_run?.created_at || null
        const latestRunFolder = ws?.latest_run?.folder || null

        if (ws?.is_running) {
          latestStatus = 'running'
        }

        let evalPercent: number | null = null
        if (evalResp?.success && evalResp.aggregate && evalResp.aggregate.max_possible_score > 0) {
          evalPercent = Math.round((evalResp.aggregate.average_score / evalResp.aggregate.max_possible_score) * 100)
        }

        summaries.set(wp, {
          id: workflow.manifest.id || wp,
          label: workflow.manifest.label || wp.split('/').pop() || wp,
          latestStatus,
          totalRuns: ws?.total_runs || 0,
          lastActive,
          totalCost: null,
          evalPercent,
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
  }, [])

  useEffect(() => { loadData() }, [loadData])

  useEffect(() => {
    if (showWorkflowsOverview) {
      loadData()
    }
  }, [showWorkflowsOverview, loadData])

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

    setReviewState(prev => ({
      ...EMPTY_REVIEW_STATE,
      loading: true,
      report: prev.report?.run_folder === runFolder ? prev.report : null,
    }))

    const [reportResult, evaluationResult, costResult] = await Promise.allSettled([
      agentApi.getFinalOutput(workspacePath, runFolder),
      agentApi.getEvaluationReports(workspacePath, runFolder),
      agentApi.getCosts(workspacePath),
    ])

    let report: WorkflowFinalOutputResponse | null = null
    let reportError: string | null = null
    if (reportResult.status === 'fulfilled') {
      report = reportResult.value
      if (report && report.success === false && report.error) {
        reportError = report.error
      }
    } else {
      reportError = reportResult.reason instanceof Error ? reportResult.reason.message : 'Failed to load report'
    }

    let evaluation: EvaluationReportEntry | null = null
    let evaluationError: string | null = null
    if (evaluationResult.status === 'fulfilled') {
      const response = evaluationResult.value
      if (response.success) {
        const reports = Array.isArray(response.reports) ? response.reports : []
        evaluation = reports.find(item => item.run_folder === runFolder) || reports[0] || null
      } else if (response.error) {
        evaluationError = response.error
      }
    } else {
      evaluationError = evaluationResult.reason instanceof Error ? evaluationResult.reason.message : 'Failed to load evaluation'
    }

    let tokenUsage: TokenUsageFile | null = null
    let evaluationTokenUsage: TokenUsageFile | null = null
    let costError: string | null = null
    if (costResult.status === 'fulfilled') {
      if (costResult.value.success) {
        const runCosts = (costResult.value.runs || []).find(item => item.run_folder === runFolder) || null
        tokenUsage = runCosts?.token_usage || null
        evaluationTokenUsage = runCosts?.evaluation_token_usage || null
      } else {
        costError = 'Failed to load cost data'
      }
    } else {
      costError = costResult.reason instanceof Error ? costResult.reason.message : 'Failed to load cost data'
    }

    setReviewState({
      loading: false,
      report,
      reportError,
      evaluation,
      evaluationError,
      tokenUsage,
      evaluationTokenUsage,
      costError,
    })
  }, [])

  useEffect(() => {
    loadWorkflowReview(selectedWorkflowEntry)
  }, [loadWorkflowReview, selectedWorkflowEntry])

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
  const executionModelRows = getModelUsageRows(reviewState.tokenUsage)
  const evaluationModelRows = getModelUsageRows(reviewState.evaluationTokenUsage)
  const realEmployees = useMemo(
    () => employeeWorkflows.filter(({ employee }) => employee.id !== '__unassigned__'),
    [employeeWorkflows]
  )
  const totalWorkflowCount = useMemo(
    () => employeeWorkflows.reduce((sum, entry) => sum + entry.workflows.length, 0),
    [employeeWorkflows]
  )
  const runningWorkflowCount = useMemo(
    () => employeeWorkflows.reduce((sum, entry) => sum + entry.runningNow, 0),
    [employeeWorkflows]
  )
  const unassignedWorkflowCount = useMemo(
    () => employeeWorkflows.find(({ employee }) => employee.id === '__unassigned__')?.workflows.length || 0,
    [employeeWorkflows]
  )

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="w-6 h-6 animate-spin text-indigo-500" />
      </div>
    )
  }

  return (
    <div className="space-y-5">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <h3 className="text-2xl font-semibold tracking-tight text-foreground">Employees</h3>
          <p className="mt-1 max-w-2xl text-sm text-muted-foreground">
            Review each employee through their latest workflow report, evaluation, and cost.
          </p>
        </div>

        <button
          onClick={() => setShowAllSchedules(true)}
          className="inline-flex items-center gap-2 self-start rounded-xl border border-border bg-background px-4 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted"
        >
          <Calendar className="h-4 w-4" />
          View Schedules
        </button>
      </div>

      <div className="grid gap-3 sm:grid-cols-3">
        <div className="rounded-xl border border-border bg-card px-4 py-3 shadow-sm">
          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Employees
          </div>
          <div className="mt-2 text-2xl font-semibold text-foreground">{realEmployees.length}</div>
        </div>
        <div className="rounded-xl border border-border bg-card px-4 py-3 shadow-sm">
          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Workflows
          </div>
          <div className="mt-2 text-2xl font-semibold text-foreground">{totalWorkflowCount}</div>
        </div>
        <div className="rounded-xl border border-border bg-card px-4 py-3 shadow-sm">
          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
            Activity
          </div>
          <div className="mt-2 flex items-baseline gap-2">
            <span className="text-2xl font-semibold text-foreground">{runningWorkflowCount}</span>
            <span className="text-sm text-muted-foreground">{unassignedWorkflowCount} unassigned</span>
          </div>
        </div>
      </div>

      <div className="grid gap-5 xl:grid-cols-[minmax(0,1.08fr)_minmax(360px,0.92fr)]">
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
                      <EmployeeAvatar name={employee.name} color={employee.avatar_color} />
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
                      <span className="inline-flex items-center gap-1 rounded-full border border-border bg-background px-2.5 py-1 text-muted-foreground">
                        <Workflow className="h-3.5 w-3.5" />
                        {workflows.length} workflow{workflows.length !== 1 ? 's' : ''}
                      </span>
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
                                {wf.evalPercent !== null && (
                                  <span className={`rounded-full px-1.5 py-0.5 text-[10px] ${getEvalBadgeClasses(wf.evalPercent)}`}>
                                    Eval {wf.evalPercent}%
                                  </span>
                                )}
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
                              <button
                                onClick={() => handleSelectWorkflow(wf.workspacePath, 'report')}
                                className="rounded-lg border border-border bg-background px-2.5 py-1 text-[11px] font-medium text-foreground transition-colors hover:bg-muted"
                              >
                                Report
                              </button>
                              <button
                                onClick={() => handleSelectWorkflow(wf.workspacePath, 'evaluation')}
                                className="rounded-lg border border-border bg-background px-2.5 py-1 text-[11px] font-medium text-foreground transition-colors hover:bg-muted"
                              >
                                Eval
                              </button>
                              <button
                                onClick={() => handleSelectWorkflow(wf.workspacePath, 'cost')}
                                className="rounded-lg border border-border bg-background px-2.5 py-1 text-[11px] font-medium text-foreground transition-colors hover:bg-muted"
                              >
                                Cost
                              </button>
                              {employees.length > 0 && (
                                <div className="relative">
                                  <button
                                    onClick={() => setAssigningWorkflow(assigningWorkflow === wf.workspacePath ? null : wf.workspacePath)}
                                    className="rounded-lg px-2.5 py-1 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
                                  >
                                    {employee.id === '__unassigned__' ? 'Assign' : 'Reassign'}
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

          <div className="xl:sticky xl:top-6 self-start">
            <div className="overflow-hidden rounded-2xl border border-border bg-card shadow-sm">
              <div className="border-b border-border bg-muted/30 px-5 py-4">
                {selectedWorkflow && selectedEmployee ? (
                  <>
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <div className="flex items-center gap-3">
                          <EmployeeAvatar name={selectedEmployee.name} color={selectedEmployee.avatar_color} size="sm" />
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
                      {selectedWorkflow.evalPercent !== null && (
                        <span className={`inline-flex items-center gap-1 rounded-full px-2 py-1 ${getEvalBadgeClasses(selectedWorkflow.evalPercent)}`}>
                          <BarChart3 className="w-3 h-3" />
                          Eval {selectedWorkflow.evalPercent}%
                        </span>
                      )}
                      {totalKnownCost > 0 && (
                        <span className="inline-flex items-center gap-1 rounded-full bg-warning/15 px-2 py-1 text-warning">
                          <DollarSign className="w-3 h-3" />
                          {formatUsd(totalKnownCost)}
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
                  reviewState.report?.exists && reviewState.report.content ? (
                    <div className="space-y-4">
                      <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Latest Archived Report</div>
                        <div className="mt-1 text-sm text-foreground">
                          {reviewState.report.output_path || `reports/${selectedWorkflow.latestRunFolder.split('/').pop() || 'group'}/<timestamp>.md`}
                        </div>
                      </div>
                      <MarkdownRenderer content={reviewState.report.content} className="max-w-none" showScrollbar={true} />
                    </div>
                  ) : (
                    <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                      {reviewState.reportError || 'No report has been generated for the latest run yet.'}
                    </div>
                  )
                ) : reviewTab === 'evaluation' ? (
                  reviewState.evaluation ? (
                    <div className="space-y-4">
                      <div className="grid gap-3 sm:grid-cols-2">
                        <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Overall Score</div>
                          <div className="mt-2 text-2xl font-semibold text-foreground">
                            {reviewState.evaluation.report.score_percentage}%
                          </div>
                          <div className="mt-1 text-xs text-muted-foreground">
                            {reviewState.evaluation.report.total_score} / {reviewState.evaluation.report.max_possible_score}
                          </div>
                        </div>
                        <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                          <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Generated</div>
                          <div className="mt-2 text-sm font-medium text-foreground">
                            {formatScheduleTime(reviewState.evaluation.report.generated_at)}
                          </div>
                          <div className="mt-1 text-xs text-muted-foreground">
                            Run {reviewState.evaluation.run_folder}
                          </div>
                        </div>
                      </div>
                      <div className="rounded-xl border border-border px-4 py-3">
                        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Summary</div>
                        <p className="mt-2 whitespace-pre-wrap text-sm text-foreground">
                          {reviewState.evaluation.report.summary}
                        </p>
                      </div>
                      <div className="space-y-3">
                        {reviewState.evaluation.report.step_scores.map(step => (
                          <div key={step.step_id} className="rounded-xl border border-border px-4 py-3">
                            <div className="flex items-center justify-between gap-3">
                              <div className="text-sm font-medium text-foreground">{step.step_title}</div>
                              <div className="text-xs font-semibold text-muted-foreground">
                                {step.max_score > 0 ? Math.round((step.score / step.max_score) * 100) : 0}%
                              </div>
                            </div>
                            <p className="mt-2 whitespace-pre-wrap text-xs text-muted-foreground">{step.reasoning}</p>
                          </div>
                        ))}
                      </div>
                    </div>
                  ) : (
                    <div className="rounded-2xl border border-dashed border-border p-8 text-center text-sm text-muted-foreground">
                      {reviewState.evaluationError || 'No evaluation report exists for the latest run yet.'}
                    </div>
                  )
                ) : (
                  <div className="space-y-4">
                    <div className="grid gap-3 sm:grid-cols-3">
                      <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Execution</div>
                        <div className="mt-2 text-xl font-semibold text-foreground">{formatUsd(executionCost)}</div>
                      </div>
                      <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Evaluation</div>
                        <div className="mt-2 text-xl font-semibold text-foreground">{formatUsd(evaluationCost)}</div>
                      </div>
                      <div className="rounded-xl border border-border bg-muted/30 px-4 py-3">
                        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">Known Total</div>
                        <div className="mt-2 text-xl font-semibold text-foreground">{formatUsd(totalKnownCost > 0 ? totalKnownCost : null)}</div>
                      </div>
                    </div>

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

                    {reviewState.tokenUsage && (
                      <div className="overflow-hidden rounded-xl border border-border">
                        <div className="border-b border-border bg-muted/30 px-4 py-3">
                          <div className="text-sm font-medium text-foreground">Execution cost by model</div>
                        </div>
                        <div className="divide-y divide-border">
                          {executionModelRows.map(model => (
                            <div key={model.model} className="flex items-center justify-between gap-3 px-4 py-3">
                              <div className="min-w-0">
                                <div className="truncate text-sm text-foreground">{model.model}</div>
                                <div className="mt-1 text-xs text-muted-foreground">
                                  {model.inputTokens.toLocaleString()} in · {model.outputTokens.toLocaleString()} out
                                </div>
                              </div>
                              <div className="text-sm font-medium text-foreground">{formatUsd(model.totalCostUsd)}</div>
                            </div>
                          ))}
                        </div>
                      </div>
                    )}

                    {reviewState.evaluationTokenUsage && (
                      <div className="overflow-hidden rounded-xl border border-border">
                        <div className="border-b border-border bg-muted/30 px-4 py-3">
                          <div className="text-sm font-medium text-foreground">Evaluation cost by model</div>
                        </div>
                        <div className="divide-y divide-border">
                          {evaluationModelRows.map(model => (
                            <div key={model.model} className="flex items-center justify-between gap-3 px-4 py-3">
                              <div className="min-w-0">
                                <div className="truncate text-sm text-foreground">{model.model}</div>
                                <div className="mt-1 text-xs text-muted-foreground">
                                  {model.inputTokens.toLocaleString()} in · {model.outputTokens.toLocaleString()} out
                                </div>
                              </div>
                              <div className="text-sm font-medium text-foreground">{formatUsd(model.totalCostUsd)}</div>
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

      {showAllSchedules && (
        <WorkflowScheduleRunsPanel onClose={() => setShowAllSchedules(false)} />
      )}

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
