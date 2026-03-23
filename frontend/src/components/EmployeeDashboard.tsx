import React, { useEffect, useState, useCallback, useMemo } from 'react'
import {
  Plus, X, Pencil, Trash2, UserCircle2, Workflow, CheckCircle2,
  PlayCircle, AlertCircle, Clock, DollarSign, ChevronRight, Loader2, Calendar, FileText, BarChart3
} from 'lucide-react'
import { agentApi } from '../services/api'
import { schedulerApi } from '../api/scheduler'
import { usePresetApplication, useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { useAppStore } from '../stores/useAppStore'
import type { Employee, EvaluationReportEntry, TokenUsageFile, WorkflowFinalOutputResponse } from '../services/api-types'
import type { CustomPreset, PredefinedPreset } from '../types/preset'
import SchedulePresetPopup from './SchedulePresetPopup'
import WorkflowScheduleRunsPanel from './scheduler/WorkflowScheduleRunsPanel'
import ExecutionLogsPopup from './workflow/ExecutionLogsPopup'
import { MarkdownRenderer } from './ui/MarkdownRenderer'

// Color palette for employees
const AVATAR_COLORS = [
  '#6366f1', '#8b5cf6', '#a855f7', '#d946ef',
  '#ec4899', '#f43f5e', '#ef4444', '#f97316',
  '#eab308', '#22c55e', '#14b8a6', '#06b6d4',
  '#3b82f6', '#6366f1',
]

interface WorkflowSummary {
  preset: CustomPreset | PredefinedPreset
  latestStatus: string
  totalRuns: number
  lastActive: string | null
  totalCost: number | null
  evalPercent: number | null
  workspacePath: string | null
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

// Employee creation/edit modal
const EmployeeModal: React.FC<{
  isOpen: boolean
  onClose: () => void
  onSave: (name: string, color: string, description: string) => Promise<void>
  initial?: { name: string; avatar_color: string; description: string }
}> = ({ isOpen, onClose, onSave, initial }) => {
  const [name, setName] = useState(initial?.name || '')
  const [color, setColor] = useState(initial?.avatar_color || AVATAR_COLORS[0])
  const [description, setDescription] = useState(initial?.description || '')
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (isOpen) {
      setName(initial?.name || '')
      setColor(initial?.avatar_color || AVATAR_COLORS[0])
      setDescription(initial?.description || '')
    }
  }, [isOpen, initial])

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-[60]" onClick={onClose}>
      <div className="bg-white dark:bg-gray-800 rounded-2xl shadow-2xl w-full max-w-md mx-4 overflow-hidden" onClick={e => e.stopPropagation()}>
        <div className="px-6 py-5 border-b border-gray-100 dark:border-gray-700">
          <h3 className="text-lg font-semibold text-gray-900 dark:text-gray-100">
            {initial ? 'Edit Employee' : 'Add Employee'}
          </h3>
        </div>
        <div className="px-6 py-5 space-y-4">
          <div>
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Name</label>
            <input
              type="text"
              value={name}
              onChange={e => setName(e.target.value)}
              placeholder="e.g. Sales Bot, Content Writer..."
              className="w-full px-3 py-2 rounded-lg border border-gray-200 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-indigo-500 focus:border-transparent outline-none"
              autoFocus
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-1.5">Description</label>
            <input
              type="text"
              value={description}
              onChange={e => setDescription(e.target.value)}
              placeholder="What does this employee do?"
              className="w-full px-3 py-2 rounded-lg border border-gray-200 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 text-sm focus:ring-2 focus:ring-indigo-500 focus:border-transparent outline-none"
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-500 dark:text-gray-400 mb-2">Color</label>
            <div className="flex flex-wrap gap-2">
              {AVATAR_COLORS.map(c => (
                <button
                  key={c}
                  onClick={() => setColor(c)}
                  className={`w-7 h-7 rounded-full transition-all ${color === c ? 'ring-2 ring-offset-2 ring-offset-white dark:ring-offset-gray-800 ring-indigo-500 scale-110' : 'hover:scale-105'}`}
                  style={{ backgroundColor: c }}
                />
              ))}
            </div>
          </div>
        </div>
        <div className="px-6 py-4 bg-gray-50 dark:bg-gray-800/50 border-t border-gray-100 dark:border-gray-700 flex justify-end gap-2">
          <button onClick={onClose} className="px-4 py-2 text-sm rounded-lg text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700">
            Cancel
          </button>
          <button
            onClick={async () => {
              if (!name.trim()) return
              setSaving(true)
              await onSave(name.trim(), color, description.trim())
              setSaving(false)
              onClose()
            }}
            disabled={!name.trim() || saving}
            className="px-4 py-2 text-sm rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 disabled:opacity-50 font-medium"
          >
            {saving ? 'Saving...' : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}

// Avatar component
const EmployeeAvatar: React.FC<{ name: string; color: string; size?: 'sm' | 'md' | 'lg' }> = ({ name, color, size = 'md' }) => {
  const initials = name.split(' ').map(w => w[0]).join('').slice(0, 2).toUpperCase()
  const sizeClasses = { sm: 'w-8 h-8 text-xs', md: 'w-10 h-10 text-sm', lg: 'w-14 h-14 text-lg' }
  return (
    <div
      className={`${sizeClasses[size]} rounded-xl flex items-center justify-center font-bold text-white shadow-lg`}
      style={{ backgroundColor: color }}
    >
      {initials}
    </div>
  )
}

// Mini status indicator
const StatusDot: React.FC<{ status: string }> = ({ status }) => {
  if (status === 'completed') return <div className="w-2 h-2 rounded-full bg-green-500" />
  if (status === 'running') return <div className="w-2 h-2 rounded-full bg-blue-500 animate-pulse" />
  if (status === 'failed') return <div className="w-2 h-2 rounded-full bg-red-500" />
  return <div className="w-2 h-2 rounded-full bg-gray-300 dark:bg-gray-600" />
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
        ? 'bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 shadow-sm'
        : 'text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200'
    }`}
  >
    {label}
  </button>
)

export const EmployeeDashboard: React.FC = () => {
  const [employees, setEmployees] = useState<Employee[]>([])
  const [employeeWorkflows, setEmployeeWorkflows] = useState<EmployeeWithWorkflows[]>([])
  const [loading, setLoading] = useState(true)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingEmployee, setEditingEmployee] = useState<Employee | null>(null)
  const [assigningWorkflow, setAssigningWorkflow] = useState<string | null>(null) // preset ID being assigned
  const [schedulePreset, setSchedulePreset] = useState<WorkflowSummary | null>(null)
  const [showAllSchedules, setShowAllSchedules] = useState(false)
  const [logsState, setLogsState] = useState<{ workspacePath: string; runFolder: string } | null>(null)
  const [selectedWorkflowId, setSelectedWorkflowId] = useState<string | null>(null)
  const [reviewTab, setReviewTab] = useState<ReviewTab>('report')
  const [reviewState, setReviewState] = useState<WorkflowReviewState>(EMPTY_REVIEW_STATE)

  const { applyPreset } = usePresetApplication()
  const refreshPresets = useGlobalPresetStore(s => s.refreshPresets)
  const { setModeCategory, selectedModeCategory } = useModeStore()
  const setShowWorkflowsOverview = useAppStore(s => s.setShowWorkflowsOverview)

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [empResp] = await Promise.all([
        agentApi.listEmployees(),
        refreshPresets(),
      ])
      const schedulesResp = await schedulerApi.listJobs({ entity_type: 'workflow' }).catch(() => ({ jobs: [], total: 0, limit: 0, offset: 0 }))

      const presets = useGlobalPresetStore.getState().getPresetsForMode('workflow')
      const scheduleByPreset = new Map<string, { count: number; nextRunAt: string | null }>()
      for (const job of schedulesResp.jobs || []) {
        const prev = scheduleByPreset.get(job.preset_query_id) || { count: 0, nextRunAt: null }
        let nextRunAt = prev.nextRunAt
        if (job.enabled && job.next_run_at) {
          if (!nextRunAt || job.next_run_at < nextRunAt) nextRunAt = job.next_run_at
        }
        scheduleByPreset.set(job.preset_query_id, { count: prev.count + 1, nextRunAt })
      }

      const emps = empResp.employees || []
      setEmployees(emps)

      // Build workflow summaries for each preset
      const summaries: Map<string, WorkflowSummary> = new Map()
      await Promise.all(
        presets.map(async (preset) => {
          const wp = preset.selectedFolder?.filepath
          if (!wp) {
            const sched = scheduleByPreset.get(preset.id)
            summaries.set(preset.id, {
              preset,
              latestStatus: 'unknown',
              totalRuns: 0,
              lastActive: null,
              totalCost: null,
              evalPercent: null,
              workspacePath: null,
              latestRunFolder: null,
              scheduleCount: sched?.count || 0,
              nextScheduleAt: sched?.nextRunAt || null,
            })
            return
          }
          try {
            const [wsState, evalResp] = await Promise.all([
              agentApi.loadWorkspaceState(wp).catch(() => null),
              agentApi.getEvaluationReports(wp).catch(() => null),
            ])
            const folders = wsState?.data?.run_folders || []
            let latestStatus = 'unknown'
            let lastActive: string | null = null
            let latestRunFolder: string | null = null
            let latestRunTime: string | null = null
            for (const f of folders) {
              const t = f.metadata?.created_at || f.progress?.last_updated || null
              if (t && (!lastActive || t > lastActive)) lastActive = t
              if (t && (!latestRunTime || t > latestRunTime)) {
                latestRunTime = t
                latestRunFolder = f.name
                latestStatus = f.metadata?.status || 'unknown'
              }
              const meta = f.metadata
              if (meta?.status === 'running') latestStatus = 'running'
              else if (meta?.status === 'completed' && latestStatus !== 'running') latestStatus = 'completed'
            }
            let evalPercent: number | null = null
            if (evalResp?.success && evalResp.aggregate && evalResp.aggregate.max_possible_score > 0) {
              evalPercent = Math.round((evalResp.aggregate.average_score / evalResp.aggregate.max_possible_score) * 100)
            }
            const sched = scheduleByPreset.get(preset.id)
            summaries.set(preset.id, {
              preset,
              latestStatus,
              totalRuns: folders.length,
              lastActive,
              totalCost: null,
              evalPercent,
              workspacePath: wp,
              latestRunFolder,
              scheduleCount: sched?.count || 0,
              nextScheduleAt: sched?.nextRunAt || null,
            })
          } catch {
            const sched = scheduleByPreset.get(preset.id)
            summaries.set(preset.id, {
              preset,
              latestStatus: 'unknown',
              totalRuns: 0,
              lastActive: null,
              totalCost: null,
              evalPercent: null,
              workspacePath: wp,
              latestRunFolder: null,
              scheduleCount: sched?.count || 0,
              nextScheduleAt: sched?.nextRunAt || null,
            })
          }
        })
      )

      // Group by employee
      // We need employee_id from presets — check if it's in the preset data
      // For now, we'll use the assign-workflow endpoint and track via a separate query
      // Actually, employee_id is on preset_queries but the frontend CustomPreset type may not have it
      // We'll fetch the mapping via a direct DB query approach — for now, use the raw preset data

      // Build employee -> workflows mapping
      // Since the frontend preset type doesn't have employee_id yet, we need to get it from the API
      // The preset list returns from the backend which includes employee_id via MarshalJSON
      // But the frontend type doesn't include it. Let's work with what we have and fetch separately.

      // For now, group all as unassigned and let users assign via the UI
      const empMap: Map<string, EmployeeWithWorkflows> = new Map()
      for (const emp of emps) {
        empMap.set(emp.id, { employee: emp, workflows: [], totalCost: 0, completedToday: 0, runningNow: 0 })
      }

      const unassigned: WorkflowSummary[] = []
      for (const [, summary] of summaries) {
        // Check if preset has employee_id (mapped from backend)
        const empId = (summary.preset as { employee_id?: string }).employee_id
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
  }, [refreshPresets])

  useEffect(() => { loadData() }, [loadData])

  const workflowEntries = useMemo<SelectedWorkflowEntry[]>(() => {
    return employeeWorkflows.flatMap(({ employee, workflows }) =>
      workflows.map(workflow => ({ employee, workflow }))
    )
  }, [employeeWorkflows])

  const selectedWorkflowEntry = useMemo<SelectedWorkflowEntry | null>(() => {
    if (workflowEntries.length === 0) return null
    if (!selectedWorkflowId) return workflowEntries[0]
    return workflowEntries.find(entry => entry.workflow.preset.id === selectedWorkflowId) || workflowEntries[0]
  }, [workflowEntries, selectedWorkflowId])

  useEffect(() => {
    if (workflowEntries.length === 0) {
      if (selectedWorkflowId !== null) setSelectedWorkflowId(null)
      return
    }

    if (!selectedWorkflowId || !workflowEntries.some(entry => entry.workflow.preset.id === selectedWorkflowId)) {
      setSelectedWorkflowId(workflowEntries[0].workflow.preset.id)
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
      agentApi.getCosts(workspacePath, runFolder),
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
        evaluation = response.reports.find(item => item.run_folder === runFolder) || response.reports[0] || null
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
        tokenUsage = costResult.value.token_usage || null
        evaluationTokenUsage = costResult.value.evaluation_token_usage || null
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

  const handleCreateEmployee = useCallback(async (name: string, color: string, description: string) => {
    await agentApi.createEmployee({ name, avatar_color: color, description })
    loadData()
  }, [loadData])

  const handleUpdateEmployee = useCallback(async (name: string, color: string, description: string) => {
    if (!editingEmployee) return
    await agentApi.updateEmployee(editingEmployee.id, { name, avatar_color: color, description })
    setEditingEmployee(null)
    loadData()
  }, [editingEmployee, loadData])

  const handleDeleteEmployee = useCallback(async (id: string) => {
    if (!confirm('Delete this employee? Workflows will be unassigned.')) return
    await agentApi.deleteEmployee(id)
    loadData()
  }, [loadData])

  const handleAssign = useCallback(async (presetId: string, employeeId: string | null) => {
    await agentApi.assignWorkflowEmployee(presetId, employeeId)
    setAssigningWorkflow(null)
    loadData()
  }, [loadData])

  const handleOpenWorkflow = useCallback((preset: CustomPreset | PredefinedPreset) => {
    if (selectedModeCategory !== 'workflow') setModeCategory('workflow')
    applyPreset(preset, 'workflow')
    setShowWorkflowsOverview(false)
  }, [applyPreset, selectedModeCategory, setModeCategory, setShowWorkflowsOverview])

  const handleSelectWorkflow = useCallback((presetId: string, nextTab?: ReviewTab) => {
    setSelectedWorkflowId(presetId)
    if (nextTab) setReviewTab(nextTab)
  }, [])

  const selectedWorkflow = selectedWorkflowEntry?.workflow || null
  const selectedEmployee = selectedWorkflowEntry?.employee || null
  const executionCost = getTokenUsageTotal(reviewState.tokenUsage)
  const evaluationCost = getTokenUsageTotal(reviewState.evaluationTokenUsage)
  const totalKnownCost = (executionCost || 0) + (evaluationCost || 0)
  const executionModelRows = getModelUsageRows(reviewState.tokenUsage)
  const evaluationModelRows = getModelUsageRows(reviewState.evaluationTokenUsage)

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="w-6 h-6 animate-spin text-indigo-500" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h3 className="text-base font-semibold text-gray-900 dark:text-gray-100">Employees</h3>
          <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
            Review each employee through their latest workflow report, evaluation, and cost.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowAllSchedules(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg border border-gray-200 dark:border-gray-700 text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700/60 transition-colors"
          >
            <Calendar className="w-3.5 h-3.5" />
            View Schedules
          </button>
          <button
            onClick={() => { setEditingEmployee(null); setModalOpen(true) }}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 transition-colors"
          >
            <Plus className="w-3.5 h-3.5" />
            Add Employee
          </button>
        </div>
      </div>

      {employeeWorkflows.length === 0 && (
        <div className="text-center py-12 text-gray-400 dark:text-gray-500">
          <UserCircle2 className="w-12 h-12 mx-auto mb-3 opacity-50" />
          <p className="text-sm">No employees yet. Add one to get started.</p>
        </div>
      )}

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.15fr)_minmax(360px,0.85fr)]">
        <div className="space-y-4">
          {employeeWorkflows.map(({ employee, workflows, runningNow }) => (
            <div
              key={employee.id}
              className={`rounded-xl border ${employee.id === '__unassigned__' ? 'border-dashed border-gray-300 dark:border-gray-600' : 'border-gray-200 dark:border-gray-700'} bg-white dark:bg-gray-800/50 overflow-hidden`}
            >
              <div className="relative px-5 py-4">
                <div
                  className="absolute inset-0 opacity-[0.04] dark:opacity-[0.08]"
                  style={{ background: `linear-gradient(135deg, ${employee.avatar_color} 0%, transparent 60%)` }}
                />
                <div className="relative flex items-center justify-between gap-3">
                  <div className="flex items-center gap-3 min-w-0">
                    <EmployeeAvatar name={employee.name} color={employee.avatar_color} />
                    <div className="min-w-0">
                      <h4 className="font-semibold text-gray-900 dark:text-gray-100">{employee.name}</h4>
                      {employee.description && (
                        <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5 truncate">{employee.description}</p>
                      )}
                    </div>
                  </div>
                  <div className="flex items-center gap-4">
                    <div className="flex items-center gap-3 text-xs">
                      {runningNow > 0 && (
                        <span className="flex items-center gap-1 text-blue-600 dark:text-blue-400">
                          <PlayCircle className="w-3.5 h-3.5" />
                          {runningNow} running
                        </span>
                      )}
                      <span className="flex items-center gap-1 text-gray-500 dark:text-gray-400">
                        <Workflow className="w-3.5 h-3.5" />
                        {workflows.length} workflow{workflows.length !== 1 ? 's' : ''}
                      </span>
                    </div>
                    {employee.id !== '__unassigned__' && (
                      <div className="flex items-center gap-1">
                        <button
                          onClick={() => { setEditingEmployee(employee); setModalOpen(true) }}
                          className="p-1.5 rounded-lg text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700"
                        >
                          <Pencil className="w-3.5 h-3.5" />
                        </button>
                        <button
                          onClick={() => handleDeleteEmployee(employee.id)}
                          className="p-1.5 rounded-lg text-gray-400 hover:text-red-500 hover:bg-red-50 dark:hover:bg-red-900/20"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </button>
                      </div>
                    )}
                  </div>
                </div>
              </div>

              {workflows.length > 0 ? (
                <div className={`border-t ${employee.id === '__unassigned__' ? 'border-dashed' : ''} border-gray-100 dark:border-gray-700/50 divide-y divide-gray-50 dark:divide-gray-700/30`}>
                  {workflows.map(wf => {
                    const isSelected = selectedWorkflow?.preset.id === wf.preset.id
                    return (
                      <div
                        key={wf.preset.id}
                        className={`px-5 py-3 cursor-pointer transition-colors ${
                          isSelected
                            ? 'bg-indigo-50/90 dark:bg-indigo-900/20'
                            : 'hover:bg-gray-100 dark:hover:bg-gray-700/50'
                        }`}
                        onClick={() => handleSelectWorkflow(wf.preset.id)}
                      >
                        <div className="flex items-start justify-between gap-3">
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2 min-w-0">
                              <StatusDot status={wf.latestStatus} />
                              <span className="text-sm font-medium text-gray-700 dark:text-gray-300 truncate">{wf.preset.label}</span>
                              {wf.evalPercent !== null && (
                                <span className={`text-[10px] px-1.5 py-0.5 rounded-full ${
                                  wf.evalPercent >= 80
                                    ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
                                    : wf.evalPercent >= 50
                                      ? 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
                                      : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
                                }`}>
                                  Eval {wf.evalPercent}%
                                </span>
                              )}
                              {isSelected && (
                                <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300">
                                  Selected
                                </span>
                              )}
                            </div>
                            <div className="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-gray-500 dark:text-gray-400">
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
                                <span className="inline-flex items-center gap-1 text-amber-600 dark:text-amber-400">
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
                              onClick={() => handleSelectWorkflow(wf.preset.id, 'report')}
                              className="px-2.5 py-1 text-[11px] rounded-md bg-indigo-100 text-indigo-700 dark:bg-indigo-900/30 dark:text-indigo-300 hover:bg-indigo-200/80 dark:hover:bg-indigo-900/50"
                            >
                              Report
                            </button>
                            <button
                              onClick={() => handleSelectWorkflow(wf.preset.id, 'evaluation')}
                              className="px-2.5 py-1 text-[11px] rounded-md bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300 hover:bg-emerald-200/80 dark:hover:bg-emerald-900/50"
                            >
                              Eval
                            </button>
                            <button
                              onClick={() => handleSelectWorkflow(wf.preset.id, 'cost')}
                              className="px-2.5 py-1 text-[11px] rounded-md bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300 hover:bg-amber-200/80 dark:hover:bg-amber-900/50"
                            >
                              Cost
                            </button>
                            <button
                              onClick={() => handleOpenWorkflow(wf.preset)}
                              className="px-2.5 py-1 text-[11px] rounded-md border border-gray-200 dark:border-gray-700 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700/60"
                            >
                              Open
                            </button>
                            <button
                              onClick={() => setSchedulePreset(wf)}
                              className="text-[11px] text-amber-600 dark:text-amber-400 hover:underline px-1.5 py-0.5"
                              title={wf.nextScheduleAt ? `Next run: ${formatScheduleTime(wf.nextScheduleAt)}` : 'Set schedule'}
                            >
                              Schedule
                            </button>
                            {employees.length > 0 && (
                              <div className="relative">
                                <button
                                  onClick={() => setAssigningWorkflow(assigningWorkflow === wf.preset.id ? null : wf.preset.id)}
                                  className="text-[11px] text-indigo-600 dark:text-indigo-400 hover:underline px-1.5 py-0.5"
                                >
                                  {employee.id === '__unassigned__' ? 'Assign' : 'Reassign'}
                                </button>
                                {assigningWorkflow === wf.preset.id && (
                                  <div className="absolute right-0 top-full mt-1 w-48 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-10 py-1">
                                    {employee.id !== '__unassigned__' && (
                                      <button
                                        onClick={() => handleAssign(wf.preset.id, null)}
                                        className="w-full flex items-center gap-2 px-3 py-1.5 text-sm text-left hover:bg-gray-100 dark:hover:bg-gray-700/70 text-gray-600 dark:text-gray-300"
                                      >
                                        <X className="w-3.5 h-3.5" />
                                        <span>Unassign</span>
                                      </button>
                                    )}
                                    {employees.filter(e => e.id !== '__unassigned__' && e.id !== employee.id).map(emp => (
                                      <button
                                        key={emp.id}
                                        onClick={() => handleAssign(wf.preset.id, emp.id)}
                                        className="w-full flex items-center gap-2 px-3 py-1.5 text-sm text-left hover:bg-gray-100 dark:hover:bg-gray-700/70"
                                      >
                                        <EmployeeAvatar name={emp.name} color={emp.avatar_color} size="sm" />
                                        <span className="text-gray-700 dark:text-gray-300">{emp.name}</span>
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
                <div className="border-t border-gray-100 dark:border-gray-700/50 px-5 py-3 text-xs text-gray-400 dark:text-gray-500">
                  No workflows assigned yet
                </div>
              )}
            </div>
          ))}
        </div>

        <div className="xl:sticky xl:top-6 self-start">
          <div className="rounded-xl border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800/50 overflow-hidden">
            <div className="px-5 py-4 border-b border-gray-100 dark:border-gray-700/50">
              {selectedWorkflow && selectedEmployee ? (
                <>
                  <div className="flex items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex items-center gap-3">
                        <EmployeeAvatar name={selectedEmployee.name} color={selectedEmployee.avatar_color} size="sm" />
                        <div className="min-w-0">
                          <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">
                            Employee report
                          </div>
                          <h4 className="text-base font-semibold text-gray-900 dark:text-gray-100 truncate">
                            {selectedWorkflow.preset.label}
                          </h4>
                          <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">
                            {selectedEmployee.name}
                          </p>
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => handleOpenWorkflow(selectedWorkflow.preset)}
                        className="px-2.5 py-1 text-[11px] rounded-md border border-gray-200 dark:border-gray-700 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700/60"
                      >
                        Open Workflow
                      </button>
                      {selectedWorkflow.workspacePath && selectedWorkflow.latestRunFolder && (
                        <button
                          onClick={() => setLogsState({ workspacePath: selectedWorkflow.workspacePath!, runFolder: selectedWorkflow.latestRunFolder! })}
                          className="px-2.5 py-1 text-[11px] rounded-md text-gray-500 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-700/60"
                        >
                          Logs
                        </button>
                      )}
                    </div>
                  </div>

                  <div className="mt-3 flex flex-wrap gap-2 text-[11px]">
                    {selectedWorkflow.latestRunFolder ? (
                      <span className="inline-flex items-center gap-1 rounded-full bg-gray-100 dark:bg-gray-700 px-2 py-1 text-gray-600 dark:text-gray-300">
                        <FileText className="w-3 h-3" />
                        {selectedWorkflow.latestRunFolder}
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1 rounded-full bg-gray-100 dark:bg-gray-700 px-2 py-1 text-gray-600 dark:text-gray-300">
                        No runs yet
                      </span>
                    )}
                    {selectedWorkflow.evalPercent !== null && (
                      <span className="inline-flex items-center gap-1 rounded-full bg-emerald-100 dark:bg-emerald-900/30 px-2 py-1 text-emerald-700 dark:text-emerald-300">
                        <BarChart3 className="w-3 h-3" />
                        Eval {selectedWorkflow.evalPercent}%
                      </span>
                    )}
                    {totalKnownCost > 0 && (
                      <span className="inline-flex items-center gap-1 rounded-full bg-amber-100 dark:bg-amber-900/30 px-2 py-1 text-amber-700 dark:text-amber-300">
                        <DollarSign className="w-3 h-3" />
                        {formatUsd(totalKnownCost)}
                      </span>
                    )}
                    {selectedWorkflow.nextScheduleAt && (
                      <span className="inline-flex items-center gap-1 rounded-full bg-indigo-100 dark:bg-indigo-900/30 px-2 py-1 text-indigo-700 dark:text-indigo-300">
                        <Calendar className="w-3 h-3" />
                        {formatScheduleTime(selectedWorkflow.nextScheduleAt)}
                      </span>
                    )}
                    {selectedWorkflow.lastActive && (
                      <span className="inline-flex items-center gap-1 rounded-full bg-gray-100 dark:bg-gray-700 px-2 py-1 text-gray-600 dark:text-gray-300">
                        <Clock className="w-3 h-3" />
                        {formatTimestamp(selectedWorkflow.lastActive)}
                      </span>
                    )}
                  </div>
                </>
              ) : (
                <div>
                  <h4 className="text-base font-semibold text-gray-900 dark:text-gray-100">Latest report</h4>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                    Select a workflow to review its report, evaluation, and cost.
                  </p>
                </div>
              )}
            </div>

            <div className="px-5 py-3 border-b border-gray-100 dark:border-gray-700/50">
              <div className="inline-flex items-center gap-1 rounded-lg bg-gray-100 dark:bg-gray-800 p-1">
                <ReviewTabButton active={reviewTab === 'report'} label="Report" onClick={() => setReviewTab('report')} />
                <ReviewTabButton active={reviewTab === 'evaluation'} label="Evaluation" onClick={() => setReviewTab('evaluation')} />
                <ReviewTabButton active={reviewTab === 'cost'} label="Cost" onClick={() => setReviewTab('cost')} />
              </div>
            </div>

            <div className="p-5 max-h-[calc(100vh-240px)] overflow-y-auto">
              {!selectedWorkflow ? (
                <div className="rounded-xl border border-dashed border-gray-200 dark:border-gray-700 p-8 text-center text-sm text-gray-500 dark:text-gray-400">
                  Select a workflow from the left to review what this employee produced.
                </div>
              ) : !selectedWorkflow.latestRunFolder ? (
                <div className="rounded-xl border border-dashed border-gray-200 dark:border-gray-700 p-8 text-center text-sm text-gray-500 dark:text-gray-400">
                  This workflow has not produced a run yet, so there is no report, evaluation, or cost data to review.
                </div>
              ) : reviewState.loading ? (
                <div className="flex items-center justify-center py-16 text-sm text-gray-500 dark:text-gray-400">
                  <Loader2 className="w-5 h-5 animate-spin mr-2 text-indigo-500" />
                  Loading latest workflow review data...
                </div>
              ) : reviewTab === 'report' ? (
                reviewState.report?.exists && reviewState.report.content ? (
                  <div className="space-y-4">
                    <div className="rounded-lg border border-gray-200 dark:border-gray-700 px-4 py-3 bg-gray-50 dark:bg-gray-900/40">
                      <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">Latest Report</div>
                      <div className="text-sm text-gray-700 dark:text-gray-300 mt-1">
                        {reviewState.report.output_path || `runs/${selectedWorkflow.latestRunFolder}/final_output.md`}
                      </div>
                    </div>
                    <MarkdownRenderer content={reviewState.report.content} className="max-w-none" showScrollbar={true} />
                  </div>
                ) : (
                  <div className="rounded-xl border border-dashed border-gray-200 dark:border-gray-700 p-8 text-center text-sm text-gray-500 dark:text-gray-400">
                    {reviewState.reportError || 'No report has been generated for the latest run yet.'}
                  </div>
                )
              ) : reviewTab === 'evaluation' ? (
                reviewState.evaluation ? (
                  <div className="space-y-4">
                    <div className="grid gap-3 sm:grid-cols-2">
                      <div className="rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/40 px-4 py-3">
                        <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">Overall Score</div>
                        <div className="mt-2 text-2xl font-semibold text-gray-900 dark:text-gray-100">
                          {reviewState.evaluation.report.score_percentage}%
                        </div>
                        <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          {reviewState.evaluation.report.total_score} / {reviewState.evaluation.report.max_possible_score}
                        </div>
                      </div>
                      <div className="rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/40 px-4 py-3">
                        <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">Generated</div>
                        <div className="mt-2 text-sm font-medium text-gray-900 dark:text-gray-100">
                          {formatScheduleTime(reviewState.evaluation.report.generated_at)}
                        </div>
                        <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                          Run {reviewState.evaluation.run_folder}
                        </div>
                      </div>
                    </div>
                    <div className="rounded-lg border border-gray-200 dark:border-gray-700 px-4 py-3">
                      <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">Summary</div>
                      <p className="mt-2 text-sm text-gray-700 dark:text-gray-300 whitespace-pre-wrap">
                        {reviewState.evaluation.report.summary}
                      </p>
                    </div>
                    <div className="space-y-3">
                      {reviewState.evaluation.report.step_scores.map(step => (
                        <div key={step.step_id} className="rounded-lg border border-gray-200 dark:border-gray-700 px-4 py-3">
                          <div className="flex items-center justify-between gap-3">
                            <div className="text-sm font-medium text-gray-900 dark:text-gray-100">{step.step_title}</div>
                            <div className="text-xs font-semibold text-gray-600 dark:text-gray-300">
                              {step.max_score > 0 ? Math.round((step.score / step.max_score) * 100) : 0}%
                            </div>
                          </div>
                          <p className="mt-2 text-xs text-gray-500 dark:text-gray-400 whitespace-pre-wrap">{step.reasoning}</p>
                        </div>
                      ))}
                    </div>
                  </div>
                ) : (
                  <div className="rounded-xl border border-dashed border-gray-200 dark:border-gray-700 p-8 text-center text-sm text-gray-500 dark:text-gray-400">
                    {reviewState.evaluationError || 'No evaluation report exists for the latest run yet.'}
                  </div>
                )
              ) : (
                <div className="space-y-4">
                  <div className="grid gap-3 sm:grid-cols-3">
                    <div className="rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/40 px-4 py-3">
                      <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">Execution</div>
                      <div className="mt-2 text-xl font-semibold text-gray-900 dark:text-gray-100">{formatUsd(executionCost)}</div>
                    </div>
                    <div className="rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/40 px-4 py-3">
                      <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">Evaluation</div>
                      <div className="mt-2 text-xl font-semibold text-gray-900 dark:text-gray-100">{formatUsd(evaluationCost)}</div>
                    </div>
                    <div className="rounded-lg border border-gray-200 dark:border-gray-700 bg-gray-50 dark:bg-gray-900/40 px-4 py-3">
                      <div className="text-xs uppercase tracking-wide text-gray-500 dark:text-gray-400">Known Total</div>
                      <div className="mt-2 text-xl font-semibold text-gray-900 dark:text-gray-100">{formatUsd(totalKnownCost > 0 ? totalKnownCost : null)}</div>
                    </div>
                  </div>

                  {reviewState.costError && !reviewState.tokenUsage && !reviewState.evaluationTokenUsage && (
                    <div className="rounded-xl border border-dashed border-gray-200 dark:border-gray-700 p-6 text-center text-sm text-gray-500 dark:text-gray-400">
                      {reviewState.costError}
                    </div>
                  )}

                  {!reviewState.costError && !reviewState.tokenUsage && !reviewState.evaluationTokenUsage && (
                    <div className="rounded-xl border border-dashed border-gray-200 dark:border-gray-700 p-6 text-center text-sm text-gray-500 dark:text-gray-400">
                      No cost data has been recorded for the latest run yet.
                    </div>
                  )}

                  {reviewState.tokenUsage && (
                    <div className="rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
                      <div className="px-4 py-3 border-b border-gray-100 dark:border-gray-700/50 bg-gray-50 dark:bg-gray-900/40">
                        <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Execution cost by model</div>
                      </div>
                      <div className="divide-y divide-gray-100 dark:divide-gray-700/50">
                        {executionModelRows.map(model => (
                          <div key={model.model} className="px-4 py-3 flex items-center justify-between gap-3">
                            <div className="min-w-0">
                              <div className="text-sm text-gray-800 dark:text-gray-200 truncate">{model.model}</div>
                              <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                                {model.inputTokens.toLocaleString()} in · {model.outputTokens.toLocaleString()} out
                              </div>
                            </div>
                            <div className="text-sm font-medium text-gray-700 dark:text-gray-300">{formatUsd(model.totalCostUsd)}</div>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {reviewState.evaluationTokenUsage && (
                    <div className="rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
                      <div className="px-4 py-3 border-b border-gray-100 dark:border-gray-700/50 bg-gray-50 dark:bg-gray-900/40">
                        <div className="text-sm font-medium text-gray-900 dark:text-gray-100">Evaluation cost by model</div>
                      </div>
                      <div className="divide-y divide-gray-100 dark:divide-gray-700/50">
                        {evaluationModelRows.map(model => (
                          <div key={model.model} className="px-4 py-3 flex items-center justify-between gap-3">
                            <div className="min-w-0">
                              <div className="text-sm text-gray-800 dark:text-gray-200 truncate">{model.model}</div>
                              <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                                {model.inputTokens.toLocaleString()} in · {model.outputTokens.toLocaleString()} out
                              </div>
                            </div>
                            <div className="text-sm font-medium text-gray-700 dark:text-gray-300">{formatUsd(model.totalCostUsd)}</div>
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

      {/* Employee Modal */}
      <EmployeeModal
        isOpen={modalOpen}
        onClose={() => { setModalOpen(false); setEditingEmployee(null) }}
        onSave={editingEmployee ? handleUpdateEmployee : handleCreateEmployee}
        initial={editingEmployee ? { name: editingEmployee.name, avatar_color: editingEmployee.avatar_color, description: editingEmployee.description } : undefined}
      />

      {schedulePreset && (
        <SchedulePresetPopup
          presetQueryId={schedulePreset.preset.id}
          presetLabel={schedulePreset.preset.label}
          entityType="workflow"
          workspacePath={schedulePreset.workspacePath || undefined}
          onClose={() => setSchedulePreset(null)}
        />
      )}

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
