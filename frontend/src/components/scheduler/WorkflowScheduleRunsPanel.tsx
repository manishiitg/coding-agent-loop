import React, { useState, useEffect, useCallback } from 'react'
import ReactDOM from 'react-dom'
import cronstrue from 'cronstrue'
import {
  X, Play, Trash2, Clock, CheckCircle, XCircle, Minus, Loader,
  Terminal, Pause, Calendar, ClipboardCheck,
  ChevronDown, ChevronRight, RefreshCw, Square
} from 'lucide-react'
import { schedulerApi } from '../../api/scheduler'
import { agentApi } from '../../services/api'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import type { ScheduledJob, ScheduledJobRun } from '../../services/api-types'
import CostsPopup from '../workflow/CostsPopup'
import ExecutionLogsPopup from '../workflow/ExecutionLogsPopup'
import EvaluationPopup from '../workflow/EvaluationPopup'
import SchedulePresetPopup from '../SchedulePresetPopup'
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from '../ui/tooltip'

interface WorkflowScheduleRunsPanelProps {
  onClose: () => void
}

type ActivePopup = 'costs' | 'logs' | 'eval' | null

interface JobPopupState {
  jobId: string
  workspacePath: string
  runFolders: string[]
  popup: ActivePopup
  selectedRunFolder?: string
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

function formatNext(dateStr?: string): string {
  if (!dateStr) return '—'
  const d = new Date(dateStr)
  const now = new Date()
  const diff = d.getTime() - now.getTime()
  const hrs = Math.floor(diff / 3600000)
  if (hrs < 24) return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return d.toLocaleDateString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
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

const WorkflowScheduleRunsPanel: React.FC<WorkflowScheduleRunsPanelProps> = ({ onClose }) => {
  const [jobs, setJobs] = useState<ScheduledJob[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [expandedJobId, setExpandedJobId] = useState<string | null>(null)
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

  const loadJobs = useCallback(async () => {
    setIsLoading(true)
    setError(null)
    try {
      const resp = await schedulerApi.listJobs({ entity_type: 'workflow' })
      setJobs(resp.jobs)
    } catch {
      setError('Failed to load scheduled workflows')
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadJobs()
    refreshPresets()
  }, [loadJobs, refreshPresets])

  // Auto-refresh while any job is running
  const hasRunningJob = jobs.some(j => j.last_status === 'running')
  useEffect(() => {
    if (!hasRunningJob) return
    const interval = setInterval(() => {
      loadJobs()
      // Also refresh runs for expanded job
      if (expandedJobId) {
        loadRunsForJob(expandedJobId)
      }
    }, 5000)
    return () => clearInterval(interval)
  }, [hasRunningJob, loadJobs, expandedJobId])

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
    const preset = presetMap.get(jobs.find(j => j.id === jobId)?.preset_query_id ?? '')
    const workspacePath = preset?.workspacePath
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
  }, [presetMap, jobs, runCosts])

  // Auto-load runs when a job is expanded
  useEffect(() => {
    if (!expandedJobId) return
    loadRunsForJob(expandedJobId)
  }, [expandedJobId, loadRunsForJob])

  // Detect latest iteration folder for running runs that don't have run_folder yet
  const detectRunningFolders = useCallback(async (jobId: string, runs: ScheduledJobRun[]) => {
    const runningWithoutFolder = runs.filter(r => r.status === 'running' && !r.run_folder)
    if (runningWithoutFolder.length === 0) return

    const preset = presetMap.get(jobs.find(j => j.id === jobId)?.preset_query_id ?? '')
    const workspacePath = preset?.workspacePath
    if (!workspacePath) return

    try {
      const resp = await agentApi.getRunFolders(workspacePath)
      const folders = resp.folders?.map(f => f.name) ?? []
      if (folders.length > 0) {
        // Use the latest folder (highest iteration number)
        const latestFolder = folders[folders.length - 1]
        const newMap: Record<string, string> = {}
        runningWithoutFolder.forEach(r => { newMap[r.id] = latestFolder })
        setRunningRunFolders(prev => ({ ...prev, ...newMap }))
      }
    } catch { /* ignore */ }
  }, [presetMap, jobs])

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

  const openPopup = async (job: ScheduledJob, popup: ActivePopup, selectedRunFolder?: string) => {
    const preset = presetMap.get(job.preset_query_id)
    const workspacePath = preset?.workspacePath ?? null
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
      setPopupState({ jobId: job.id, workspacePath, runFolders, popup, selectedRunFolder })
    } catch {
      setPopupState({ jobId: job.id, workspacePath, runFolders: [], popup, selectedRunFolder })
    }
  }

  const activeJobs = jobs.filter(j => j.enabled).length

  return ReactDOM.createPortal(
    <TooltipProvider delayDuration={300}>
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="w-full max-w-3xl mx-4 bg-white dark:bg-gray-800 rounded-xl shadow-2xl border border-gray-200 dark:border-gray-700 flex flex-col max-h-[85vh]">

        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
          <div className="flex items-center gap-2">
            <Calendar className="w-5 h-5 text-amber-500" />
            <h2 className="text-base font-semibold text-gray-900 dark:text-gray-100">
              Scheduled Workflows
            </h2>
            {!isLoading && (
              <span className="text-xs text-gray-400 dark:text-gray-500 ml-1">
                {activeJobs} active · {jobs.length} total
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={loadJobs}
                  className="p-1.5 rounded-md text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors"
                >
                  <RefreshCw className="w-4 h-4" />
                </button>
              </TooltipTrigger>
              <TooltipContent side="bottom">Refresh</TooltipContent>
            </Tooltip>
            <button onClick={onClose} className="p-1.5 rounded-md text-gray-400 hover:text-gray-600 dark:hover:text-gray-200">
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto">
          {isLoading ? (
            <div className="flex items-center justify-center h-40 text-sm text-gray-400">Loading...</div>
          ) : error ? (
            <div className="flex items-center justify-center h-40 text-sm text-red-500">{error}</div>
          ) : jobs.length === 0 ? (
            <div className="flex flex-col items-center justify-center h-40 gap-2 text-sm text-gray-400 dark:text-gray-500">
              <Clock className="w-8 h-8 opacity-30" />
              <p>No scheduled workflows yet.</p>
              <p className="text-xs">Click the clock icon on a workflow preset to add one.</p>
            </div>
          ) : (
            <div className="divide-y divide-gray-100 dark:divide-gray-700">
              {jobs.map((job) => {
                const preset = presetMap.get(job.preset_query_id)
                const cronDesc = describeCron(job.cron_expression)
                const isExpanded = expandedJobId === job.id
                const hasWorkspace = !!preset?.workspacePath
                const runs = jobRuns[job.id] ?? []
                const isLoadingThis = loadingRuns === job.id

                return (
                  <div key={job.id} className={`px-5 py-4 ${!job.enabled ? 'opacity-60' : ''}`}>
                    {/* Row top */}
                    <div className="flex items-start gap-3">
                      {/* Status dot */}
                      <div className={`mt-1 w-2 h-2 rounded-full flex-shrink-0 ${
                        job.last_status === 'running' ? 'bg-amber-500 animate-pulse' :
                        job.enabled ? 'bg-green-500' : 'bg-gray-300 dark:bg-gray-600'
                      }`} />

                      {/* Main content */}
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className="text-sm font-medium text-gray-900 dark:text-gray-100 truncate">
                            {job.name}
                          </span>
                          {preset && (
                            <span className="text-xs text-gray-400 dark:text-gray-500 truncate">
                              {preset.label}
                            </span>
                          )}
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
                        </div>

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
                          <span>Next: {job.enabled ? formatNext(job.next_run_at) : 'paused'}</span>
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
                                  className="flex items-center gap-2 text-xs py-1 px-2 rounded hover:bg-gray-100 dark:hover:bg-gray-700/50"
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
                                  <div className="flex items-center gap-0.5 ml-auto flex-shrink-0">
                                    {/* Stop button for running jobs */}
                                    {run.status === 'running' && (
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            onClick={() => handleStopRun(job)}
                                            className="p-0.5 text-gray-400 hover:text-red-500 transition-colors"
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
                                            className="p-0.5 text-gray-300 dark:text-gray-600 hover:text-blue-500 transition-colors"
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
                                            className="p-0.5 text-gray-300 dark:text-gray-600 hover:text-emerald-500 transition-colors"
                                          >
                                            <ClipboardCheck className="w-3 h-3" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">Evaluation scores</TooltipContent>
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

      {/* Edit schedule popup */}
      {editingJob && (() => {
        const preset = presetMap.get(editingJob.preset_query_id)
        return (
          <SchedulePresetPopup
            presetQueryId={editingJob.preset_query_id}
            presetLabel={preset?.label ?? editingJob.name}
            entityType="workflow"
            workspacePath={preset?.workspacePath ?? undefined}
            onClose={() => { setEditingJob(null); loadJobs() }}
          />
        )
      })()}
    </div>
    </TooltipProvider>,
    document.body
  )
}

export default WorkflowScheduleRunsPanel
