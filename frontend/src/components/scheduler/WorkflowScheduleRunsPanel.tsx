import React, { useState, useEffect, useCallback, useMemo } from 'react'
import ReactDOM from 'react-dom'
import cronstrue from 'cronstrue'
import {
  X, Play, Trash2, Clock, CheckCircle, XCircle, Minus, Loader,
  Terminal, Pause, Calendar, ClipboardCheck, AlertTriangle,
  ChevronDown, ChevronRight, RefreshCw, Square, Radio, Search, FileText
} from 'lucide-react'
import { schedulerApi } from '../../api/scheduler'
import { agentApi } from '../../services/api'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import type { ScheduledJob, ScheduledJobRun, SchedulerConfig } from '../../services/api-types'
import CostsPopup from '../workflow/CostsPopup'
import ExecutionLogsPopup from '../workflow/ExecutionLogsPopup'
import EvaluationPopup from '../workflow/EvaluationPopup'
import FinalOutputPopup from '../workflow/FinalOutputPopup'
import SchedulePresetPopup from '../SchedulePresetPopup'
import ScheduleLiveEventsPopup from './ScheduleLiveEventsPopup'
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from '../ui/tooltip'

interface WorkflowScheduleRunsPanelProps {
  onClose: () => void
}

type ActivePopup = 'costs' | 'logs' | 'eval' | 'report' | 'live' | null
type JobFilter = 'running' | 'enabled' | 'paused' | 'issues' | 'all'

interface JobPopupState {
  jobId: string
  jobName: string
  workspacePath: string
  runFolders: string[]
  popup: ActivePopup
  selectedRunFolder?: string
  sessionId?: string
}

function describeCron(expr: string): string {
  try {
    return cronstrue.toString(expr, { throwExceptionOnParseError: true })
  } catch {
    return expr
  }
}

function timeAgo(dateStr?: string): string {
  if (!dateStr) return '—'
  const diff = Date.now() - new Date(dateStr).getTime()
  const mins = Math.floor(diff / 60000)
  if (mins < 60) return `${mins}m ago`
  const hrs = Math.floor(mins / 60)
  if (hrs < 24) return `${hrs}h ago`
  const days = Math.floor(hrs / 24)
  return `${days}d ago`
}

function formatDuration(ms?: number): string {
  if (!ms) return ''
  if (ms < 1000) return `${ms}ms`
  const secs = Math.floor(ms / 1000)
  if (secs < 60) return `${secs}s`
  const mins = Math.floor(secs / 60)
  const remSecs = secs % 60
  if (mins < 60) return `${mins}m ${remSecs}s`
  const hrs = Math.floor(mins / 60)
  const remMins = mins % 60
  return `${hrs}h ${remMins}m`
}

function formatCost(usd: number): string {
  if (usd === 0) return '$0'
  if (usd < 0.01) return `$${usd.toFixed(4)}`
  if (usd < 1) return `$${usd.toFixed(3)}`
  return `$${usd.toFixed(2)}`
}

function formatTokens(n: number): string {
  if (n < 1000) return `${n}`
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`
  return `${(n / 1_000_000).toFixed(2)}M`
}

function formatRunTime(dateStr: string): string {
  const d = new Date(dateStr)
  const now = new Date()
  const diffDays = Math.floor((now.getTime() - d.getTime()) / 86400000)
  if (diffDays === 0) return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  if (diffDays < 7) return d.toLocaleDateString([], { weekday: 'short', hour: '2-digit', minute: '2-digit' })
  return d.toLocaleDateString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

function formatTimeUntil(dateStr?: string): string {
  if (!dateStr) return '—'

  const diffMs = new Date(dateStr).getTime() - Date.now()
  if (Number.isNaN(diffMs)) return '—'
  if (diffMs <= 0) return 'due now'

  const totalMinutes = Math.ceil(diffMs / 60000)
  if (totalMinutes < 60) return `in ${totalMinutes}m`

  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60
  if (hours < 24) return minutes > 0 ? `in ${hours}h ${minutes}m` : `in ${hours}h`

  const days = Math.floor(hours / 24)
  const remHours = hours % 24
  if (days < 7) return remHours > 0 ? `in ${days}d ${remHours}h` : `in ${days}d`

  return new Date(dateStr).toLocaleDateString([], { month: 'short', day: 'numeric' })
}

function formatLocalScheduleTime(dateStr?: string): string {
  if (!dateStr) return '—'

  try {
    const d = new Date(dateStr)
    const now = new Date()
    const diffDays = Math.floor((d.getTime() - now.getTime()) / 86400000)

    if (diffDays < 1) {
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', timeZoneName: 'short' })
    }

    if (diffDays < 7) {
      return d.toLocaleDateString([], { weekday: 'short', hour: '2-digit', minute: '2-digit', timeZoneName: 'short' })
    }

    return d.toLocaleDateString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', timeZoneName: 'short' })
  } catch {
    return dateStr
  }
}

function formatLocalScheduleTimeShort(dateStr?: string): string {
  if (!dateStr) return ''

  try {
    return new Date(dateStr).toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      timeZoneName: 'short',
    })
  } catch {
    return ''
  }
}

function stripTimezoneSuffix(label: string): string {
  return label.replace(/\s*\([^()]*\b(?:[A-Z]{2,5}|UTC|GMT)\b[^()]*\)\s*$/, '').trim()
}

function localizeTimezoneLabel(label: string, dateStr?: string): string {
  const baseLabel = stripTimezoneSuffix(label)
  if (!dateStr) return baseLabel || label

  const localizedTime = formatLocalScheduleTimeShort(dateStr)
  if (!localizedTime) return baseLabel || label

  const hasTimezoneSuffix = /\([^()]*\b(?:[A-Z]{2,5}|UTC|GMT)\b[^()]*\)\s*$/.test(label)
  if (!hasTimezoneSuffix) return label

  return `${baseLabel} (${localizedTime})`
}

function getLocalizedJobName(job: ScheduledJob): string {
  return localizeTimezoneLabel(job.name, job.next_run_at)
}

function sortJobs(a: ScheduledJob, b: ScheduledJob): number {
  const rank = (job: ScheduledJob) => {
    if (job.last_status === 'running') return 0
    if (job.enabled && job.next_run_at) return 1
    if (job.enabled) return 2
    if (job.last_status === 'error') return 3
    return 4
  }

  const rankDiff = rank(a) - rank(b)
  if (rankDiff !== 0) return rankDiff

  if (a.enabled && b.enabled) {
    if (a.next_run_at && b.next_run_at) {
      return a.next_run_at.localeCompare(b.next_run_at)
    }
    if (a.next_run_at) return -1
    if (b.next_run_at) return 1
  }

  const aTime = a.updated_at || a.last_run_at || a.created_at || ''
  const bTime = b.updated_at || b.last_run_at || b.created_at || ''
  return bTime.localeCompare(aTime)
}

const WorkflowScheduleRunsPanel: React.FC<WorkflowScheduleRunsPanelProps> = ({ onClose }) => {
  const [jobs, setJobs] = useState<ScheduledJob[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [expandedJobId, setExpandedJobId] = useState<string | null>(null)
  const [activeFilter, setActiveFilter] = useState<JobFilter>('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedWorkflowFilter, setSelectedWorkflowFilter] = useState('all')
  const [schedulerConfig, setSchedulerConfig] = useState<SchedulerConfig | null>(null)
  const [isUpdatingSchedulerPause, setIsUpdatingSchedulerPause] = useState(false)
  const [showSchedulerDisabledNotice, setShowSchedulerDisabledNotice] = useState(false)

  // Auto-expand running jobs
  useEffect(() => {
    if (expandedJobId) return // don't override user's choice
    const runningJob = jobs.find(j => j.last_status === 'running')
    if (runningJob) {
      setExpandedJobId(runningJob.id)
    }
  }, [jobs, expandedJobId])
  const [triggering, setTriggering] = useState<string | null>(null)
  const [popupState, setPopupState] = useState<JobPopupState | null>(null)
  const [editingJob, setEditingJob] = useState<ScheduledJob | null>(null)
  // Run history per job
  const [jobRuns, setJobRuns] = useState<Record<string, ScheduledJobRun[]>>({})
  const [loadingRuns, setLoadingRuns] = useState<string | null>(null)
  // Cost summary per run_folder: { totalCost, totalTokens }
  const [runCosts, setRunCosts] = useState<Record<string, { cost: number; tokens: number } | null>>({})
  // For running runs without run_folder, we detect the latest iteration folder
  const [runningRunFolders, setRunningRunFolders] = useState<Record<string, string>>({})

  const { customPresets, predefinedPresets, refreshPresets } = useGlobalPresetStore()

  // Build presetId → {label, workspacePath} map
  const presetMap = React.useMemo(() => {
    const map = new Map<string, { label: string; workspacePath: string | null }>()
    ;[...customPresets, ...predefinedPresets].forEach((p) => {
      map.set(p.id, {
        label: p.label,
        workspacePath: p.selectedFolder?.filepath ?? null,
      })
    })
    return map
  }, [customPresets, predefinedPresets])

  const loadJobs = useCallback(async (showLoading = false) => {
    if (showLoading) setIsLoading(true)
    setError(null)
    try {
      const [resp, config] = await Promise.all([
        schedulerApi.listJobs({ entity_type: 'workflow' }),
        schedulerApi.getConfig().catch(() => null),
      ])
      setJobs(resp.jobs)
      setSchedulerConfig(config)
    } catch {
      setError('Failed to load scheduled workflows')
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadJobs(true)
    refreshPresets()
  }, [loadJobs, refreshPresets])

  const loadRunsForJob = useCallback(async (jobId: string) => {
    setLoadingRuns(jobId)
    try {
      const resp = await schedulerApi.getJobRuns(jobId, 20)
      setJobRuns(prev => ({ ...prev, [jobId]: resp.runs ?? [] }))
    } catch {
      setJobRuns(prev => ({ ...prev, [jobId]: [] }))
    } finally {
      setLoadingRuns(null)
    }
  }, [])

  // Load cost summary for runs that have a run_folder (or detected running folder)
  const loadCostsForRuns = useCallback(async (jobId: string, runs: ScheduledJobRun[]) => {
    const job = jobs.find(j => j.id === jobId)
    const preset = presetMap.get(job?.preset_query_id ?? '')
    const workspacePath = job?.workspace_path || preset?.workspacePath
    if (!workspacePath) return

    const foldersToLoad = runs
      .map(r => r.run_folder || runningRunFolders[r.id])
      .filter((f): f is string => !!f && !(f in runCosts))

    if (foldersToLoad.length === 0) return

    // Fetch in parallel (limit to 5 to avoid overload)
    const batch = foldersToLoad.slice(0, 5)
    const results = await Promise.allSettled(
      batch.map(folder => agentApi.getCosts(workspacePath, folder))
    )

    const newCosts: Record<string, { cost: number; tokens: number } | null> = {}
    batch.forEach((folder, i) => {
      const result = results[i]
      if (result.status === 'fulfilled' && result.value.token_usage?.by_model) {
        let totalCost = 0
        let totalTokens = 0
        for (const model of Object.values(result.value.token_usage.by_model)) {
          totalCost += model.total_cost_usd ?? 0
          totalTokens += (model.input_tokens ?? 0) + (model.output_tokens ?? 0)
        }
        // Also add evaluation costs if present
        if (result.value.evaluation_token_usage?.by_model) {
          for (const model of Object.values(result.value.evaluation_token_usage.by_model)) {
            totalCost += model.total_cost_usd ?? 0
            totalTokens += (model.input_tokens ?? 0) + (model.output_tokens ?? 0)
          }
        }
        newCosts[folder] = { cost: totalCost, tokens: totalTokens }
      } else {
        newCosts[folder] = null
      }
    })
    setRunCosts(prev => ({ ...prev, ...newCosts }))
  }, [presetMap, jobs, runCosts, runningRunFolders])

  // Auto-load runs when a job is expanded
  useEffect(() => {
    if (!expandedJobId) return
    loadRunsForJob(expandedJobId)
  }, [expandedJobId, loadRunsForJob])

  // Detect latest iteration folder for running runs that don't have run_folder yet
  const detectRunningFolders = useCallback(async (jobId: string, runs: ScheduledJobRun[]) => {
    const runningWithoutFolder = runs.filter(r => r.status === 'running' && !r.run_folder)
    if (runningWithoutFolder.length === 0) return

    const djob = jobs.find(j => j.id === jobId)
    const preset = presetMap.get(djob?.preset_query_id ?? '')
    const workspacePath = djob?.workspace_path || preset?.workspacePath
    if (!workspacePath) return

    try {
      const resp = await agentApi.getRunFolders(workspacePath)
      const folders = resp.folders?.map(f => f.name) ?? []
      if (folders.length > 0) {
        // Find the highest iteration number (API sorts alphabetically, not numerically)
        const latestFolder = [...folders].sort((a, b) => {
          const numA = parseInt(a.match(/iteration-(\d+)/)?.[1] ?? '0')
          const numB = parseInt(b.match(/iteration-(\d+)/)?.[1] ?? '0')
          return numB - numA // descending
        })[0]
        const newMap: Record<string, string> = {}
        runningWithoutFolder.forEach(r => { newMap[r.id] = latestFolder })
        setRunningRunFolders(prev => ({ ...prev, ...newMap }))
      }
    } catch { /* ignore */ }
  }, [presetMap, jobs])

  // Auto-refresh while any job is running: jobs list + runs + costs (every 5s)
  const hasRunningJob = jobs.some(j => j.last_status === 'running')
  const activeJobs = jobs.filter(j => j.enabled).length
  const isSchedulerPaused = !!schedulerConfig?.globally_paused
  const isSchedulerExecutionEnabled = schedulerConfig?.execution_enabled !== false
  const schedulerDisabledReason = schedulerConfig?.disabled_reason

  useEffect(() => {
    if (schedulerConfig?.execution_enabled === false) {
      setShowSchedulerDisabledNotice(true)
    }
  }, [schedulerConfig?.execution_enabled])

  const summary = useMemo(() => {
    const running = jobs.filter(j => j.last_status === 'running').length
    const issues = jobs.filter(j => j.last_status === 'error').length
    const paused = jobs.filter(j => !j.enabled).length
    return {
      running,
      issues,
      paused,
      enabled: activeJobs,
      total: jobs.length,
    }
  }, [jobs, activeJobs])

  const normalizedSearch = searchQuery.trim().toLowerCase()
  const workflowOptions = useMemo(() => {
    const seen = new Map<string, string>()

    jobs.forEach((job) => {
      const preset = presetMap.get(job.preset_query_id)
      const label = localizeTimezoneLabel(
        preset?.label || job.workflow_label || job.name,
        job.next_run_at
      )
      if (!label) return
      if (!seen.has(label)) seen.set(label, label)
    })

    return Array.from(seen.entries())
      .map(([value, label]) => ({ value, label }))
      .sort((a, b) => a.label.localeCompare(b.label))
  }, [jobs, presetMap])

  const upcomingJobs = useMemo(() => {
    return [...jobs]
      .filter(job => {
        if (!job.enabled || !job.next_run_at) return false
        if (selectedWorkflowFilter === 'all') return true
        const preset = presetMap.get(job.preset_query_id)
        const workflowLabel = preset?.label || job.workflow_label || job.name
        return workflowLabel === selectedWorkflowFilter
      })
      .sort((a, b) => (a.next_run_at || '').localeCompare(b.next_run_at || ''))
      .slice(0, 4)
  }, [jobs, presetMap, selectedWorkflowFilter])

  const filteredJobs = useMemo(() => {
    return [...jobs]
      .sort(sortJobs)
      .filter((job) => {
        switch (activeFilter) {
          case 'running':
            if (job.last_status !== 'running') return false
            break
          case 'enabled':
            if (!job.enabled) return false
            break
          case 'paused':
            if (job.enabled) return false
            break
          case 'issues':
            if (job.last_status !== 'error') return false
            break
          case 'all':
          default:
            break
        }

        if (!normalizedSearch) return true

        const preset = presetMap.get(job.preset_query_id)
        const workflowLabel = preset?.label || job.workflow_label || job.name

        if (selectedWorkflowFilter !== 'all' && workflowLabel !== selectedWorkflowFilter) {
          return false
        }

        const haystack = [
          job.name,
          job.description,
          workflowLabel,
          preset?.label,
          job.workspace_path,
          job.cron_expression,
        ]
          .filter(Boolean)
          .join(' ')
          .toLowerCase()

        return haystack.includes(normalizedSearch)
      })
  }, [jobs, activeFilter, normalizedSearch, presetMap, selectedWorkflowFilter])

  useEffect(() => {
    if (!hasRunningJob) return
    const interval = setInterval(async () => {
      loadJobs()
      if (expandedJobId) {
        await loadRunsForJob(expandedJobId)
        const runs = jobRuns[expandedJobId] ?? []
        // Clear cached costs for running runs so they re-fetch
        const runningFolders = runs
          .filter(r => r.status === 'running')
          .map(r => r.run_folder || runningRunFolders[r.id])
          .filter((f): f is string => !!f)
        if (runningFolders.length > 0) {
          setRunCosts(prev => {
            const next = { ...prev }
            runningFolders.forEach(f => delete next[f])
            return next
          })
        }
        detectRunningFolders(expandedJobId, runs)
      }
    }, 5000)
    return () => clearInterval(interval)
  }, [hasRunningJob, loadJobs, expandedJobId, jobRuns, runningRunFolders, detectRunningFolders, loadRunsForJob])

  // Auto-load costs when runs are loaded
  useEffect(() => {
    if (!expandedJobId) return
    const runs = jobRuns[expandedJobId]
    if (!runs || runs.length === 0) return
    loadCostsForRuns(expandedJobId, runs)
    detectRunningFolders(expandedJobId, runs)
  }, [expandedJobId, jobRuns, loadCostsForRuns, detectRunningFolders])

  // Auto-refresh costs for running jobs (every 10s)
  useEffect(() => {
    if (!expandedJobId) return
    const runs = jobRuns[expandedJobId]
    const runningFolders = (runs ?? [])
      .filter(r => r.status === 'running')
      .map(r => r.run_folder || runningRunFolders[r.id])
      .filter((f): f is string => !!f)
    if (runningFolders.length === 0) return
    const interval = setInterval(() => {
      // Clear cached costs for running runs to force re-fetch
      setRunCosts(prev => {
        const next = { ...prev }
        runningFolders.forEach(f => delete next[f])
        return next
      })
      // Also re-detect folders for runs that still don't have one
      detectRunningFolders(expandedJobId, runs!)
    }, 10000)
    return () => clearInterval(interval)
  }, [expandedJobId, jobRuns, runningRunFolders, detectRunningFolders])

  const handleToggle = async (job: ScheduledJob) => {
    try {
      const updated = job.enabled
        ? await schedulerApi.disableJob(job.id)
        : await schedulerApi.enableJob(job.id)
      setJobs(prev => prev.map(j => j.id === job.id ? updated : j))
    } catch { /* ignore */ }
  }

  const handleDelete = async (job: ScheduledJob) => {
    if (!window.confirm(`Remove schedule for "${job.name}"?`)) return
    try {
      await schedulerApi.deleteJob(job.id)
      setJobs(prev => prev.filter(j => j.id !== job.id))
    } catch { /* ignore */ }
  }

  const handleTrigger = async (job: ScheduledJob) => {
    setTriggering(job.id)
    try {
      await schedulerApi.triggerJob(job.id)
      setTimeout(loadJobs, 1500)
    } catch { /* ignore */ }
    finally { setTriggering(null) }
  }

  const handleStopRun = async (job: ScheduledJob) => {
    if (!window.confirm('Stop this running execution?')) return
    try {
      await schedulerApi.stopJob(job.id)
      // Refresh after a brief delay to let status propagate
      setTimeout(() => {
        loadJobs()
        loadRunsForJob(job.id)
      }, 1500)
    } catch (e) {
      console.error('Failed to stop run:', e)
    }
  }

  const handleToggleGlobalPause = async () => {
    if (!isSchedulerExecutionEnabled) {
      setShowSchedulerDisabledNotice(true)
      return
    }

    setIsUpdatingSchedulerPause(true)
    try {
      const updated = await schedulerApi.updateConfig({
        globally_paused: !isSchedulerPaused,
        paused_by: !isSchedulerPaused ? 'frontend-user' : '',
      })
      setSchedulerConfig(updated)
    } catch (e) {
      console.error('Failed to update scheduler config:', e)
    } finally {
      setIsUpdatingSchedulerPause(false)
    }
  }

  const openPopup = async (job: ScheduledJob, popup: ActivePopup, selectedRunFolder?: string) => {
    const preset = presetMap.get(job.preset_query_id)
    const workspacePath = job.workspace_path || preset?.workspacePath || null
    if (!workspacePath) return

    // Load run folders and filter to the specific iteration
    try {
      const resp = await agentApi.getRunFolders(workspacePath)
      const allFolders = resp.folders?.map(f => f.name) ?? []
      // If selectedRunFolder is an iteration (e.g. "iteration-10"), filter to only its group sub-folders
      // e.g. "iteration-10/group-1", "iteration-10/group-2"
      let runFolders = allFolders
      if (selectedRunFolder && !selectedRunFolder.includes('/')) {
        runFolders = allFolders.filter(f =>
          f === selectedRunFolder || f.startsWith(selectedRunFolder + '/')
        )
        // If we have group sub-folders, use the first group as the selected folder for logs
        const groupFolders = runFolders.filter(f => f.startsWith(selectedRunFolder + '/'))
        if (groupFolders.length > 0 && popup === 'logs') {
          selectedRunFolder = groupFolders[0]
        }
      }
      setPopupState({ jobId: job.id, jobName: job.name, workspacePath, runFolders, popup, selectedRunFolder })
    } catch {
      setPopupState({ jobId: job.id, jobName: job.name, workspacePath, runFolders: [], popup, selectedRunFolder })
    }
  }

  const filterPills: Array<{ key: JobFilter; label: string; count: number }> = [
    { key: 'running', label: 'Running', count: summary.running },
    { key: 'enabled', label: 'Enabled', count: summary.enabled },
    { key: 'paused', label: 'Paused', count: summary.paused },
    { key: 'issues', label: 'Issues', count: summary.issues },
    { key: 'all', label: 'All', count: summary.total },
  ]

  return ReactDOM.createPortal(
    <TooltipProvider delayDuration={300}>
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="w-full max-w-6xl mx-4 bg-card text-card-foreground rounded-xl shadow-2xl border border-border flex flex-col max-h-[85vh]">

        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-2">
            <Calendar className="w-5 h-5 text-amber-500" />
            <h2 className="text-base font-semibold text-foreground">
              Scheduled Workflows
            </h2>
            {!isLoading && (
              <span className="text-xs text-muted-foreground ml-1">
                {summary.running} running · {!isSchedulerExecutionEnabled ? 'server scheduler disabled' : isSchedulerPaused ? 'globally paused' : `${activeJobs} active`} · {jobs.length} total
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleToggleGlobalPause}
              disabled={isUpdatingSchedulerPause || !isSchedulerExecutionEnabled}
              className={`inline-flex items-center gap-2 rounded-md border px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-60 ${
                !isSchedulerExecutionEnabled
                  ? 'border-red-500/40 bg-red-500/10 text-red-700 dark:text-red-300'
                  : isSchedulerPaused
                  ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 hover:bg-emerald-500/20'
                  : 'border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-300 hover:bg-amber-500/20'
              }`}
              title={!isSchedulerExecutionEnabled ? 'Automatic schedules are disabled on this server' : undefined}
            >
              {isUpdatingSchedulerPause ? (
                <Loader className="w-3.5 h-3.5 animate-spin" />
              ) : !isSchedulerExecutionEnabled ? (
                <AlertTriangle className="w-3.5 h-3.5" />
              ) : isSchedulerPaused ? (
                <Play className="w-3.5 h-3.5" />
              ) : (
                <Pause className="w-3.5 h-3.5" />
              )}
              {!isSchedulerExecutionEnabled ? 'Server disabled' : isSchedulerPaused ? 'Resume schedules' : 'Pause all schedules'}
            </button>
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={() => loadJobs()}
                  className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                >
                  <RefreshCw className="w-4 h-4" />
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom">Refresh</TooltipContent>
            </Tooltip>
            <button onClick={onClose} className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors">
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto">
          {!isLoading && jobs.length > 0 && (
            <div className="sticky top-0 z-10 border-b border-border bg-card/95 backdrop-blur px-5 py-4 space-y-4">
              {!isSchedulerExecutionEnabled && (
                <div className="rounded-xl border border-red-500/30 bg-red-500/10 px-4 py-3">
                  <div className="flex items-start justify-between gap-3">
                    <div className="flex items-start gap-3">
                      <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0 text-red-500" />
                      <div>
                        <div className="text-sm font-medium text-foreground">Automatic schedules are disabled on this server</div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {schedulerDisabledReason || 'Timed cron executions will not start until the server scheduler is re-enabled. Manual runs still work.'}
                        </div>
                      </div>
                    </div>
                    <button
                      onClick={() => setShowSchedulerDisabledNotice(true)}
                      className="text-xs font-medium text-red-700 hover:text-red-800 dark:text-red-300 dark:hover:text-red-200 whitespace-nowrap"
                    >
                      View details
                    </button>
                  </div>
                </div>
              )}

              {isSchedulerPaused && (
                <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-4 py-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <div className="text-sm font-medium text-foreground">All scheduled workflow triggers are paused</div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        Existing manual runs still work. Cron-triggered executions will not start until you resume schedules.
                      </div>
                    </div>
                    {schedulerConfig?.paused_at && (
                      <div className="text-xs text-muted-foreground whitespace-nowrap">
                        Paused {timeAgo(schedulerConfig.paused_at)}
                      </div>
                    )}
                  </div>
                </div>
              )}

              <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
                <button
                  onClick={() => setActiveFilter('running')}
                  className={`text-left rounded-xl border px-3 py-2 transition-colors ${
                    activeFilter === 'running'
                      ? 'border-amber-400/70 bg-muted text-foreground shadow-sm'
                      : 'border-border bg-background text-foreground shadow-sm hover:bg-muted'
                  }`}
                >
                  <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Running now</div>
                  <div className="mt-1 flex items-center gap-2">
                    <Radio className="w-3.5 h-3.5 text-amber-500" />
                    <span className="text-lg font-semibold text-foreground">{summary.running}</span>
                  </div>
                </button>
                <button
                  onClick={() => setActiveFilter('enabled')}
                  className={`text-left rounded-xl border px-3 py-2 transition-colors ${
                    activeFilter === 'enabled'
                      ? 'border-emerald-400/70 bg-muted text-foreground shadow-sm'
                      : 'border-border bg-background text-foreground shadow-sm hover:bg-muted'
                  }`}
                >
                  <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Enabled</div>
                  <div className="mt-1 text-lg font-semibold text-foreground">{summary.enabled}</div>
                </button>
                <button
                  onClick={() => setActiveFilter('paused')}
                  className={`text-left rounded-xl border px-3 py-2 transition-colors ${
                    activeFilter === 'paused'
                      ? 'border-border bg-muted text-foreground shadow-sm'
                      : 'border-border bg-background text-foreground shadow-sm hover:bg-muted'
                  }`}
                >
                  <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Paused</div>
                  <div className="mt-1 text-lg font-semibold text-foreground">{summary.paused}</div>
                </button>
                <button
                  onClick={() => setActiveFilter('issues')}
                  className={`text-left rounded-xl border px-3 py-2 transition-colors ${
                    activeFilter === 'issues'
                      ? 'border-red-400/70 bg-muted text-foreground shadow-sm'
                      : 'border-border bg-background text-foreground shadow-sm hover:bg-muted'
                  }`}
                >
                  <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Issues</div>
                  <div className="mt-1 text-lg font-semibold text-foreground">{summary.issues}</div>
                </button>
              </div>

              <div className="rounded-xl border border-border bg-background px-3 py-3">
                <div className="flex items-center justify-between gap-3 mb-2">
                  <div>
                    <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Next scheduled</div>
                    <div className="text-sm font-medium text-foreground">Which workflows will run soonest</div>
                  </div>
                  <div className="text-xs text-muted-foreground whitespace-nowrap">
                    {upcomingJobs.length} upcoming
                  </div>
                </div>

                {upcomingJobs.length === 0 ? (
                  <div className="text-sm text-muted-foreground">No upcoming enabled schedules.</div>
                ) : (
                  <div className="grid gap-2 md:grid-cols-2">
                    {upcomingJobs.map((job) => {
                      const preset = presetMap.get(job.preset_query_id)
                      const label = localizeTimezoneLabel(
                        preset?.label || job.workflow_label || job.name,
                        job.next_run_at
                      )
                      const localizedJobName = getLocalizedJobName(job)
                      return (
                        <button
                          key={`upcoming-${job.id}`}
                          onClick={() => setExpandedJobId(job.id)}
                          className="rounded-lg border border-border bg-card px-3 py-2 text-left hover:bg-muted transition-colors"
                        >
                          <div className="flex items-center justify-between gap-3">
                            <span className="truncate text-sm font-medium text-foreground" title={label}>
                              {label}
                            </span>
                            <span className="text-xs font-medium text-amber-600 dark:text-amber-400 whitespace-nowrap">
                              {formatTimeUntil(job.next_run_at)}
                            </span>
                          </div>
                          <div className="mt-1 flex items-center justify-between gap-3 text-xs text-muted-foreground">
                            <span className="truncate" title={job.name}>{localizedJobName}</span>
                            <span className="whitespace-nowrap">{formatLocalScheduleTime(job.next_run_at)}</span>
                          </div>
                        </button>
                      )
                    })}
                  </div>
                )}
              </div>

              <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                <div className="flex flex-1 max-w-3xl flex-col gap-3 md:flex-row">
                  <div className="relative flex-1">
                    <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-muted-foreground" />
                    <input
                      type="text"
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      placeholder="Search by workflow, preset, cron, workspace..."
                      className="w-full rounded-lg border border-border bg-background px-9 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-amber-400/50"
                    />
                  </div>
                  <select
                    value={selectedWorkflowFilter}
                    onChange={(e) => setSelectedWorkflowFilter(e.target.value)}
                    className="rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-2 focus:ring-amber-400/50 md:min-w-[220px]"
                  >
                    <option value="all">All workflows</option>
                    {workflowOptions.map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="flex flex-wrap gap-2">
                  {filterPills.map((pill) => (
                    <button
                      key={pill.key}
                      onClick={() => setActiveFilter(pill.key)}
                      className={`rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                        activeFilter === pill.key
                          ? 'bg-foreground text-background'
                          : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                      }`}
                    >
                      {pill.label} ({pill.count})
                    </button>
                  ))}
                </div>
              </div>
            </div>
          )}

          {isLoading && jobs.length === 0 ? (
            <div className="flex items-center justify-center h-40 text-sm text-muted-foreground">Loading...</div>
          ) : error ? (
            <div className="flex items-center justify-center h-40 text-sm text-red-500">{error}</div>
          ) : jobs.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-40 gap-2 text-sm text-muted-foreground">
              <Clock className="w-8 h-8 opacity-30" />
              <p>No scheduled workflows yet.</p>
              <p className="text-xs">Click the clock icon on a workflow preset to add one.</p>
            </div>
          ) : filteredJobs.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-40 gap-2 text-sm text-muted-foreground px-6 text-center">
              <Search className="w-8 h-8 opacity-30" />
              <p>No workflows match the current filter.</p>
              <button
                onClick={() => {
                  setSearchQuery('')
                  setActiveFilter('all')
                  setSelectedWorkflowFilter('all')
                }}
                className="text-xs text-amber-600 dark:text-amber-400 hover:underline"
              >
                Clear search and show all schedules
              </button>
            </div>
          ) : (
            <div className="divide-y divide-gray-100 dark:divide-gray-700">
              {filteredJobs.map((job, index, jobsList) => {
                const preset = presetMap.get(job.preset_query_id)
                const cronDesc = describeCron(job.cron_expression)
                const localizedJobName = getLocalizedJobName(job)
                const workflowDisplayLabel = preset?.label || job.workflow_label || job.name
                const isExpanded = expandedJobId === job.id
                const hasWorkspace = !!job.workspace_path || !!preset?.workspacePath
                const runs = jobRuns[job.id] ?? []
                const isLoadingThis = loadingRuns === job.id
                const previousJob = index > 0 ? jobsList[index - 1] : null
                const isRunningJob = job.last_status === 'running'
                const showRunningHeader = isRunningJob && (!previousJob || previousJob.last_status !== 'running')
                const showScheduledHeader = !isRunningJob && (!previousJob || previousJob.last_status === 'running')

                return (
                  <React.Fragment key={job.id}>
                    {showRunningHeader && (
                      <div className="px-5 py-3 bg-amber-500/5 border-b border-amber-500/10">
                        <div className="flex items-center justify-between gap-3">
                          <div>
                            <div className="text-[11px] uppercase tracking-wide text-amber-600 dark:text-amber-400">Running workflows</div>
                            <div className="text-sm font-medium text-foreground">Workflows with an active execution right now</div>
                          </div>
                          <div className="text-xs text-muted-foreground whitespace-nowrap">
                            {summary.running} active
                          </div>
                        </div>
                      </div>
                    )}

                    {showScheduledHeader && (
                      <div className="px-5 py-3 bg-muted/30 border-b border-border">
                        <div className="flex items-center justify-between gap-3">
                          <div>
                            <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Scheduled workflows</div>
                            <div className="text-sm font-medium text-foreground">Saved schedules that are idle, paused, or waiting for their next run</div>
                          </div>
                          <div className="text-xs text-muted-foreground whitespace-nowrap">
                            {Math.max(filteredJobs.length - summary.running, 0)} listed
                          </div>
                        </div>
                      </div>
                    )}

                    <div className={`px-5 py-4 ${!job.enabled ? 'opacity-60' : ''}`}>
                    {/* Row top */}
                    <div className="flex items-start gap-3">
                      {/* Status dot */}
                      <div className={`mt-1 w-2 h-2 rounded-full flex-shrink-0 ${
                        job.last_status === 'running' ? 'bg-amber-500 animate-pulse' :
                        job.enabled ? 'bg-green-500' : 'bg-gray-300 dark:bg-gray-600'
                      }`} />

                      {/* Main content */}
                      <div className="flex-1 min-w-0">
                        <div className="min-w-0">
                          <div className="text-[11px] uppercase tracking-wide text-gray-400 dark:text-gray-500">
                            Workflow
                          </div>
                          <div className="mt-0.5 flex items-center gap-2 flex-wrap">
                            <span className="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate" title={workflowDisplayLabel}>
                              {workflowDisplayLabel}
                            </span>
                          </div>
                        </div>

                        <div className="mt-1 flex items-center gap-2 flex-wrap">
                          <span className="text-[11px] uppercase tracking-wide text-gray-400 dark:text-gray-500">
                            Schedule
                          </span>
                          <span className="text-xs font-medium text-gray-700 dark:text-gray-300 truncate" title={job.name}>
                            {localizedJobName}
                          </span>
                          {!job.enabled && (
                            <span className="text-xs px-1.5 py-0.5 rounded-full bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400">
                              Paused
                            </span>
                          )}
                        </div>

                        {/* Cron + groups */}
                        <div className="mt-0.5 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-gray-500 dark:text-gray-400">
                          <span className="flex items-center gap-1">
                            <Clock className="w-3 h-3" />
                            {cronDesc}
                          </span>
                          {job.group_ids && job.group_ids.length > 0 && (
                            <span>Groups: {job.group_ids.join(', ')}</span>
                          )}
                          {job.mode === 'workshop' && (
                            <span className="px-1.5 py-0.5 rounded-full bg-purple-100 dark:bg-purple-900/30 text-purple-600 dark:text-purple-400 font-medium">
                              Workshop{job.workshop_mode ? ` · ${job.workshop_mode}` : ''}
                            </span>
                          )}
                        </div>
                        {job.mode === 'workshop' && job.messages && job.messages.length > 0 && (
                          <div className="mt-1 space-y-0.5">
                            {job.messages.map((m, i) => (
                              <div key={i} className="text-xs text-gray-500 dark:text-gray-400 flex items-start gap-1">
                                <span className="text-gray-400 dark:text-gray-500 shrink-0">{i + 1}.</span>
                                <span>{m}</span>
                              </div>
                            ))}
                          </div>
                        )}

                        {/* Run stats */}
                        <div className="mt-1.5 flex flex-wrap gap-x-4 gap-y-0.5 text-xs text-gray-500 dark:text-gray-400">
                          <span className="flex items-center gap-1">
                            {job.last_status === 'running' ? (
                              <Loader className="w-3 h-3 text-amber-500 animate-spin" />
                            ) : job.last_status === 'success' ? (
                              <CheckCircle className="w-3 h-3 text-green-500" />
                            ) : job.last_status === 'error' ? (
                              <XCircle className="w-3 h-3 text-red-500" />
                            ) : (
                              <Minus className="w-3 h-3" />
                            )}
                            {job.last_status === 'running' ? 'Running...' : `Last: ${timeAgo(job.last_run_at)}`}
                          </span>
                          <span>Next: {job.enabled ? formatLocalScheduleTime(job.next_run_at) : 'paused'}</span>
                          <span>{job.run_count} run{job.run_count !== 1 ? 's' : ''}</span>
                          {job.last_duration_ms != null && job.last_status !== 'running' && (
                            <span>Duration: {formatDuration(job.last_duration_ms)}</span>
                          )}
                        </div>

                        {/* Error message */}
                        {job.last_status === 'error' && job.last_error && (
                          <div className="mt-1 text-xs text-red-500 truncate max-w-lg" title={job.last_error}>
                            ✗ {job.last_error}
                          </div>
                        )}
                      </div>

                      {/* Actions */}
                      <div className="flex items-center gap-1 flex-shrink-0">
                        {job.enabled ? (
                          <>
                            {job.last_status === 'running' ? (
                              /* Stop button when running */
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <button
                                    onClick={() => handleStopRun(job)}
                                    className="flex items-center gap-1 px-2 py-1 rounded-md text-xs font-medium text-red-600 dark:text-red-400 bg-red-50 dark:bg-red-900/30 hover:bg-red-100 dark:hover:bg-red-900/50 border border-red-200 dark:border-red-800 transition-colors"
                                  >
                                    <Square className="w-3 h-3" />
                                    Stop
                                  </button>
                                </TooltipTrigger>
                                <TooltipContent side="bottom">Stop the running execution</TooltipContent>
                              </Tooltip>
                            ) : (
                              <>
                                {/* Run now */}
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <button
                                      onClick={() => handleTrigger(job)}
                                      disabled={triggering === job.id}
                                      className="p-1.5 rounded-md text-gray-400 hover:text-green-600 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors disabled:opacity-40"
                                    >
                                      <Play className="w-3.5 h-3.5" />
                                    </button>
                                  </TooltipTrigger>
                                  <TooltipContent side="bottom">Trigger a manual run now</TooltipContent>
                                </Tooltip>
                                {/* Pause */}
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <button
                                      onClick={() => handleToggle(job)}
                                      className="p-1.5 rounded-md text-gray-400 hover:text-amber-500 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
                                    >
                                      <Pause className="w-3.5 h-3.5" />
                                    </button>
                                  </TooltipTrigger>
                                  <TooltipContent side="bottom">Pause — stops future cron runs</TooltipContent>
                                </Tooltip>
                              </>
                            )}
                          </>
                        ) : (
                          /* Resume - prominent green button when paused */
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <button
                                onClick={() => handleToggle(job)}
                                className="flex items-center gap-1 px-2 py-1 rounded-md text-xs font-medium text-green-600 dark:text-green-400 bg-green-50 dark:bg-green-900/30 hover:bg-green-100 dark:hover:bg-green-900/50 border border-green-200 dark:border-green-800 transition-colors"
                              >
                                <Play className="w-3 h-3" />
                                Resume
                              </button>
                            </TooltipTrigger>
                            <TooltipContent side="bottom">Resume — re-enable cron schedule</TooltipContent>
                          </Tooltip>
                        )}
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              onClick={() => setEditingJob(job)}
                              className="p-1.5 rounded-md text-gray-400 hover:text-blue-500 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
                            >
                              <Clock className="w-3.5 h-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="bottom">Edit schedule</TooltipContent>
                        </Tooltip>
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <button
                              onClick={() => handleDelete(job)}
                              className="p-1.5 rounded-md text-gray-400 hover:text-red-500 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
                            >
                              <Trash2 className="w-3.5 h-3.5" />
                            </button>
                          </TooltipTrigger>
                          <TooltipContent side="bottom">Delete schedule</TooltipContent>
                        </Tooltip>
                        <button
                          onClick={() => setExpandedJobId(isExpanded ? null : job.id)}
                          className="p-1.5 rounded-md text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
                        >
                          {isExpanded ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}
                        </button>
                      </div>
                    </div>

                    {/* Expanded: run history */}
                    {isExpanded && (
                      <div className="mt-3 ml-5">
                        {!hasWorkspace ? (
                          <span className="text-xs text-gray-400 italic">Workspace path not available — re-save the preset to fix.</span>
                        ) : isLoadingThis && runs.length === 0 ? (
                          <span className="text-xs text-gray-400">Loading run history...</span>
                        ) : runs.length === 0 ? (
                          <span className="text-xs text-gray-400 italic">No runs recorded yet. Trigger a run to see history.</span>
                        ) : (
                          <div className="space-y-1">
                            <div className="text-xs text-gray-400 dark:text-gray-500 mb-1.5">
                              Run history ({runs.length} runs):
                            </div>
                            <div className="space-y-1 max-h-48 overflow-y-auto">
                              {runs.map((run) => {
                                // For running runs without run_folder, use detected folder
                                const effectiveFolder = run.run_folder || runningRunFolders[run.id] || ''
                                return (
                                <div
                                  key={run.id}
                                  className="flex items-center gap-2 text-xs py-1 px-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700/50 cursor-pointer"
                                  onClick={(e) => {
                                    // Only open logs if the click wasn't on one of the action buttons
                                    if ((e.target as HTMLElement).closest('button')) return
                                    if (effectiveFolder) openPopup(job, 'logs', effectiveFolder)
                                  }}
                                >
                                  {/* Status icon */}
                                  {run.status === 'running' ? (
                                    <Loader className="w-3 h-3 text-amber-500 animate-spin flex-shrink-0" />
                                  ) : run.status === 'success' ? (
                                    <CheckCircle className="w-3 h-3 text-green-500 flex-shrink-0" />
                                  ) : (
                                    <XCircle className="w-3 h-3 text-red-500 flex-shrink-0" />
                                  )}

                                  {/* Time */}
                                  <span className="text-gray-500 dark:text-gray-400 w-28 flex-shrink-0">
                                    {formatRunTime(run.started_at)}
                                  </span>

                                  {/* Iteration / group folder */}
                                  {effectiveFolder ? (
                                    <span className="font-mono text-gray-600 dark:text-gray-400 min-w-[6rem] max-w-[12rem] flex-shrink-0 truncate" title={effectiveFolder}>
                                      {effectiveFolder}
                                    </span>
                                  ) : (
                                    <span className="text-gray-300 dark:text-gray-600 min-w-[6rem] flex-shrink-0">
                                      {run.status === 'running' ? '...' : '—'}
                                    </span>
                                  )}

                                  {/* Duration */}
                                  <span className="text-gray-400 w-16 flex-shrink-0">
                                    {run.status === 'running' ? '' : formatDuration(run.duration_ms)}
                                  </span>

                                  {/* Cost & tokens inline */}
                                  {effectiveFolder && (() => {
                                    const costData = runCosts[effectiveFolder]
                                    if (costData === undefined) return <span className="text-gray-300 dark:text-gray-600 w-24 flex-shrink-0">...</span>
                                    if (costData === null) return <span className="text-gray-300 dark:text-gray-600 w-24 flex-shrink-0">—</span>
                                    return (
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            onClick={() => openPopup(job, 'costs', effectiveFolder)}
                                            className="flex items-center gap-1 text-amber-600 dark:text-amber-400 hover:text-amber-700 dark:hover:text-amber-300 flex-shrink-0 transition-colors"
                                          >
                                            <span>{formatCost(costData.cost)}</span>
                                            <span className="text-gray-400 dark:text-gray-500">{formatTokens(costData.tokens)}t</span>
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">Click for full cost breakdown</TooltipContent>
                                      </Tooltip>
                                    )
                                  })()}

                                  {/* Groups */}
                                  {run.group_ids && run.group_ids.length > 0 && (
                                    <span className="text-gray-400 truncate" title={`Groups: ${run.group_ids.join(', ')}`}>
                                      [{run.group_ids.length}g]
                                    </span>
                                  )}

                                  {/* Error (truncated) */}
                                  {run.status === 'error' && run.error && (
                                    <span className="text-red-400 truncate flex-1" title={run.error}>
                                      {run.error.length > 50 ? run.error.slice(0, 50) + '...' : run.error}
                                    </span>
                                  )}

                                  {/* Action buttons */}
                                  <div className="flex items-center gap-2 ml-auto flex-shrink-0">
                                    {/* Live view button for running jobs with session_id */}
                                    {run.status === 'running' && run.session_id && (
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            onClick={() => setPopupState({
                                              jobId: job.id,
                                              jobName: job.name,
                                              workspacePath: job.workspace_path || '',
                                              runFolders: [],
                                              popup: 'live',
                                              sessionId: run.session_id,
                                            })}
                                            className="p-1 text-green-500 hover:text-green-400 animate-pulse transition-colors"
                                          >
                                            <Radio className="w-3 h-3" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">Live execution view</TooltipContent>
                                      </Tooltip>
                                    )}
                                    {/* Stop button for running jobs */}
                                    {run.status === 'running' && (
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            onClick={() => handleStopRun(job)}
                                            className="p-1 text-gray-400 hover:text-red-500 transition-colors"
                                          >
                                            <Square className="w-3 h-3" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">Stop this run</TooltipContent>
                                      </Tooltip>
                                    )}
                                    {/* Logs button */}
                                    {effectiveFolder && (
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            onClick={() => openPopup(job, 'logs', effectiveFolder)}
                                            className="p-1 text-gray-300 dark:text-gray-600 hover:text-blue-500 transition-colors"
                                          >
                                            <Terminal className="w-3 h-3" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">Execution logs</TooltipContent>
                                      </Tooltip>
                                    )}
                                    {/* Evaluation button */}
                                    {effectiveFolder && (
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            onClick={() => openPopup(job, 'eval', effectiveFolder)}
                                            className="p-1 text-gray-300 dark:text-gray-600 hover:text-emerald-500 transition-colors"
                                          >
                                            <ClipboardCheck className="w-3 h-3" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">Evaluation scores</TooltipContent>
                                      </Tooltip>
                                    )}
                                    {/* Report button */}
                                    {effectiveFolder && (
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            onClick={() => openPopup(job, 'report', effectiveFolder)}
                                            className="p-1 text-gray-300 dark:text-gray-600 hover:text-purple-500 transition-colors"
                                          >
                                            <FileText className="w-3 h-3" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">Latest archived report</TooltipContent>
                                      </Tooltip>
                                    )}
                                  </div>
                                </div>
                                )
                              })}
                            </div>
                          </div>
                        )}
                      </div>
                    )}
                    </div>
                  </React.Fragment>
                )
              })}
            </div>
          )}
        </div>
      </div>

      {/* Cost popup */}
      {popupState?.popup === 'costs' && (
        <CostsPopup
          isOpen
          onClose={() => setPopupState(null)}
          workspacePath={popupState.workspacePath}
          runFolders={popupState.runFolders}
          selectedRunFolder={popupState.selectedRunFolder ?? popupState.runFolders[popupState.runFolders.length - 1] ?? null}
        />
      )}

      {/* Execution logs popup */}
      {popupState?.popup === 'logs' && (
        <ExecutionLogsPopup
          isOpen
          onClose={() => setPopupState(null)}
          workspacePath={popupState.workspacePath}
          runFolder={popupState.selectedRunFolder ?? popupState.runFolders[popupState.runFolders.length - 1] ?? null}
          runFolders={popupState.runFolders}
        />
      )}

      {/* Live events popup */}
      {popupState?.popup === 'live' && popupState.sessionId && (
        <ScheduleLiveEventsPopup
          sessionId={popupState.sessionId}
          jobName={popupState.jobName}
          onClose={() => setPopupState(null)}
        />
      )}

      {/* Evaluation popup */}
      {popupState?.popup === 'eval' && (
        <EvaluationPopup
          isOpen
          onClose={() => setPopupState(null)}
          workspacePath={popupState.workspacePath}
          selectedRunFolder={popupState.selectedRunFolder ?? popupState.runFolders[popupState.runFolders.length - 1] ?? null}
          runFolders={popupState.runFolders}
        />
      )}

      {/* Final report popup */}
      {popupState?.popup === 'report' && (
        <FinalOutputPopup
          isOpen
          onClose={() => setPopupState(null)}
          workspacePath={popupState.workspacePath}
          selectedRunFolder={popupState.selectedRunFolder ?? popupState.runFolders[popupState.runFolders.length - 1] ?? null}
          runFolders={popupState.runFolders}
          variablesManifest={null}
        />
      )}

      {/* Edit schedule popup */}
      {editingJob && (() => {
        const preset = presetMap.get(editingJob.preset_query_id)
        return (
          <SchedulePresetPopup
            presetQueryId={editingJob.preset_query_id}
            presetLabel={preset?.label ?? editingJob.name}
            entityType="workflow"
            workspacePath={editingJob.workspace_path || preset?.workspacePath || undefined}
            onClose={() => { setEditingJob(null); loadJobs() }}
          />
        )
      })()}

      {showSchedulerDisabledNotice && !isSchedulerExecutionEnabled && (
        <div
          className="fixed inset-0 z-[10000] flex items-center justify-center bg-black/50 px-4"
          onClick={(e) => { if (e.target === e.currentTarget) setShowSchedulerDisabledNotice(false) }}
        >
          <div className="w-full max-w-md rounded-xl border border-red-500/30 bg-card p-5 shadow-2xl">
            <div className="flex items-start gap-3">
              <AlertTriangle className="mt-0.5 h-5 w-5 flex-shrink-0 text-red-500" />
              <div className="min-w-0">
                <h3 className="text-base font-semibold text-foreground">Automatic schedules are disabled</h3>
                <p className="mt-2 text-sm text-muted-foreground">
                  {schedulerDisabledReason || 'Timed cron executions are turned off on this server.'}
                </p>
                <p className="mt-2 text-sm text-muted-foreground">
                  Users can still edit schedules and trigger workflows manually, but scheduled runs will not start until the server setting is re-enabled and the app is restarted.
                </p>
                <div className="mt-4 flex justify-end">
                  <button
                    onClick={() => setShowSchedulerDisabledNotice(false)}
                    className="rounded-md bg-red-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-red-700 transition-colors"
                  >
                    Got it
                  </button>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
    </TooltipProvider>,
    document.body
  )
}

export default WorkflowScheduleRunsPanel
