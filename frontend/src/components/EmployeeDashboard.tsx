import React, { useEffect, useState, useCallback } from 'react'
import {
  Plus, X, Pencil, Trash2, UserCircle2, Workflow, CheckCircle2,
  PlayCircle, AlertCircle, Clock, DollarSign, ChevronRight, Loader2
} from 'lucide-react'
import { agentApi } from '../services/api'
import { usePresetApplication } from '../stores/useGlobalPresetStore'
import { useModeStore } from '../stores/useModeStore'
import { useAppStore } from '../stores/useAppStore'
import type { Employee, RunFolderInfo, EvaluationReportsResponse, EvaluationReportEntry, StepProgress } from '../services/api-types'
import type { CustomPreset, PredefinedPreset } from '../types/preset'

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
}

interface EmployeeWithWorkflows {
  employee: Employee
  workflows: WorkflowSummary[]
  totalCost: number
  completedToday: number
  runningNow: number
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

export const EmployeeDashboard: React.FC = () => {
  const [employees, setEmployees] = useState<Employee[]>([])
  const [employeeWorkflows, setEmployeeWorkflows] = useState<EmployeeWithWorkflows[]>([])
  const [unassignedWorkflows, setUnassignedWorkflows] = useState<WorkflowSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [modalOpen, setModalOpen] = useState(false)
  const [editingEmployee, setEditingEmployee] = useState<Employee | null>(null)
  const [assigningWorkflow, setAssigningWorkflow] = useState<string | null>(null) // preset ID being assigned

  const { getPresetsForMode, applyPreset } = usePresetApplication()
  const { setModeCategory, selectedModeCategory } = useModeStore()
  const setShowWorkflowsOverview = useAppStore(s => s.setShowWorkflowsOverview)

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [empResp, presets] = await Promise.all([
        agentApi.listEmployees(),
        Promise.resolve(getPresetsForMode('workflow')),
      ])

      const emps = empResp.employees || []
      setEmployees(emps)

      // Build workflow summaries for each preset
      const summaries: Map<string, WorkflowSummary> = new Map()
      await Promise.all(
        presets.map(async (preset) => {
          const wp = preset.selectedFolder?.filepath
          if (!wp) {
            summaries.set(preset.id, { preset, latestStatus: 'unknown', totalRuns: 0, lastActive: null, totalCost: null, evalPercent: null })
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
            for (const f of folders) {
              const t = f.progress?.last_updated || null
              if (t && (!lastActive || t > lastActive)) lastActive = t
              const meta = f.metadata
              if (meta?.status === 'running') latestStatus = 'running'
              else if (meta?.status === 'completed' && latestStatus !== 'running') latestStatus = 'completed'
            }
            let evalPercent: number | null = null
            if (evalResp?.success && evalResp.aggregate && evalResp.aggregate.max_possible_score > 0) {
              evalPercent = Math.round((evalResp.aggregate.average_score / evalResp.aggregate.max_possible_score) * 100)
            }
            summaries.set(preset.id, { preset, latestStatus, totalRuns: folders.length, lastActive, totalCost: null, evalPercent })
          } catch {
            summaries.set(preset.id, { preset, latestStatus: 'unknown', totalRuns: 0, lastActive: null, totalCost: null, evalPercent: null })
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
        // Check if preset has employee_id (it comes from backend JSON)
        const presetAny = summary.preset as Record<string, unknown>
        const empId = presetAny.employee_id as string | undefined
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
      setUnassignedWorkflows(unassigned)
    } catch (err) {
      console.error('Failed to load employee dashboard:', err)
    }
    setLoading(false)
  }, [getPresetsForMode])

  useEffect(() => { loadData() }, [loadData])

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

  if (loading) {
    return (
      <div className="flex items-center justify-center h-64">
        <Loader2 className="w-6 h-6 animate-spin text-indigo-500" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-base font-semibold text-gray-900 dark:text-gray-100">Employees</h3>
          <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">Assign workflows to employees to organize your automation team</p>
        </div>
        <button
          onClick={() => { setEditingEmployee(null); setModalOpen(true) }}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg bg-indigo-600 text-white hover:bg-indigo-700 transition-colors"
        >
          <Plus className="w-3.5 h-3.5" />
          Add Employee
        </button>
      </div>

      {/* Employee Cards */}
      {employeeWorkflows.length === 0 && (
        <div className="text-center py-12 text-gray-400 dark:text-gray-500">
          <UserCircle2 className="w-12 h-12 mx-auto mb-3 opacity-50" />
          <p className="text-sm">No employees yet. Add one to get started.</p>
        </div>
      )}

      <div className="grid gap-4">
        {employeeWorkflows.map(({ employee, workflows, runningNow, completedToday }) => (
          <div
            key={employee.id}
            className={`rounded-xl border ${employee.id === '__unassigned__' ? 'border-dashed border-gray-300 dark:border-gray-600' : 'border-gray-200 dark:border-gray-700'} bg-white dark:bg-gray-800/50 overflow-hidden`}
          >
            {/* Employee header with gradient accent */}
            <div className="relative px-5 py-4">
              <div
                className="absolute inset-0 opacity-[0.04] dark:opacity-[0.08]"
                style={{ background: `linear-gradient(135deg, ${employee.avatar_color} 0%, transparent 60%)` }}
              />
              <div className="relative flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <EmployeeAvatar name={employee.name} color={employee.avatar_color} />
                  <div>
                    <h4 className="font-semibold text-gray-900 dark:text-gray-100">{employee.name}</h4>
                    {employee.description && (
                      <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{employee.description}</p>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-4">
                  {/* Stats */}
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
                  {/* Actions — hide for virtual unassigned employee */}
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

            {/* Workflow list */}
            {workflows.length > 0 ? (
              <div className={`border-t ${employee.id === '__unassigned__' ? 'border-dashed' : ''} border-gray-100 dark:border-gray-700/50 divide-y divide-gray-50 dark:divide-gray-700/30`}>
                {workflows.map(wf => (
                  <div
                    key={wf.preset.id}
                    className="flex items-center justify-between px-5 py-2.5 cursor-pointer hover:bg-gray-50/50 dark:hover:bg-gray-700/20 transition-colors"
                    onClick={() => handleOpenWorkflow(wf.preset)}
                  >
                    <div className="flex items-center gap-3">
                      <StatusDot status={wf.latestStatus} />
                      <span className="text-sm text-gray-700 dark:text-gray-300">{wf.preset.label}</span>
                      {wf.totalRuns > 0 && (
                        <span className="text-[10px] text-gray-400 dark:text-gray-500">{wf.totalRuns} runs</span>
                      )}
                    </div>
                    <div className="flex items-center gap-3">
                      {wf.evalPercent !== null && (
                        <span className={`text-xs font-medium ${wf.evalPercent >= 80 ? 'text-green-600 dark:text-green-400' : wf.evalPercent >= 50 ? 'text-amber-600 dark:text-amber-400' : 'text-red-600 dark:text-red-400'}`}>
                          {wf.evalPercent}%
                        </span>
                      )}
                      {wf.lastActive && (
                        <span className="text-[11px] text-gray-400 dark:text-gray-500">
                          {formatTimestamp(wf.lastActive)}
                        </span>
                      )}
                      {/* Show assign dropdown for unassigned workflows */}
                      {employee.id === '__unassigned__' && employees.length > 0 && (
                        <div className="relative" onClick={e => e.stopPropagation()}>
                          <button
                            onClick={() => setAssigningWorkflow(assigningWorkflow === wf.preset.id ? null : wf.preset.id)}
                            className="text-[11px] text-indigo-600 dark:text-indigo-400 hover:underline px-1.5 py-0.5"
                          >
                            Assign
                          </button>
                          {assigningWorkflow === wf.preset.id && (
                            <div className="absolute right-0 top-full mt-1 w-48 bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg z-10 py-1">
                              {employees.filter(e => e.id !== '__unassigned__').map(emp => (
                                <button
                                  key={emp.id}
                                  onClick={() => handleAssign(wf.preset.id, emp.id)}
                                  className="w-full flex items-center gap-2 px-3 py-1.5 text-sm text-left hover:bg-gray-50 dark:hover:bg-gray-700"
                                >
                                  <EmployeeAvatar name={emp.name} color={emp.avatar_color} size="sm" />
                                  <span className="text-gray-700 dark:text-gray-300">{emp.name}</span>
                                </button>
                              ))}
                            </div>
                          )}
                        </div>
                      )}
                      <ChevronRight className="w-3.5 h-3.5 text-gray-300 dark:text-gray-600" />
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="border-t border-gray-100 dark:border-gray-700/50 px-5 py-3 text-xs text-gray-400 dark:text-gray-500">
                No workflows assigned yet
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Employee Modal */}
      <EmployeeModal
        isOpen={modalOpen}
        onClose={() => { setModalOpen(false); setEditingEmployee(null) }}
        onSave={editingEmployee ? handleUpdateEmployee : handleCreateEmployee}
        initial={editingEmployee ? { name: editingEmployee.name, avatar_color: editingEmployee.avatar_color, description: editingEmployee.description } : undefined}
      />
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
