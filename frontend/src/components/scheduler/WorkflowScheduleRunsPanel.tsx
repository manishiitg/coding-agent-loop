import React, { useState, useEffect, useCallback, useMemo, useRef } from 'react'
import ReactDOM from 'react-dom'
import cronstrue from 'cronstrue'
import {
  X, Play, Trash2, Clock, CheckCircle, XCircle, Minus, Loader,
  Terminal, Pause, Calendar, ClipboardCheck, AlertTriangle,
  ChevronDown, ChevronLeft, ChevronRight, RefreshCw, Square, Radio, Search, FileText, MessageSquare, Workflow
} from 'lucide-react'
import { schedulerApi } from '../../api/scheduler'
import { agentApi } from '../../services/api'
import { useGlobalPresetStore } from '../../stores/useGlobalPresetStore'
import { useChatStore } from '../../stores/useChatStore'
import { useWorkflowStore } from '../../stores/useWorkflowStore'
import type { ScheduledJob, ScheduledJobRun, SchedulerConfig, RunFolderInfo, RunMetadataModels, TokenUsageFile } from '../../services/api-types'
import CostsPopup from '../workflow/CostsPopup'
import ExecutionLogsPopup from '../workflow/ExecutionLogsPopup'
import EvaluationPopup from '../workflow/EvaluationPopup'
import { ReportViewer } from '../workflow/ReportViewer'
import SchedulePresetPopup from '../SchedulePresetPopup'
import ScheduleLiveEventsPopup from './ScheduleLiveEventsPopup'
import { Tooltip, TooltipContent, TooltipTrigger, TooltipProvider } from '../ui/tooltip'

interface WorkflowScheduleRunsPanelProps {
  onClose: () => void
}

type ActivePopup = 'costs' | 'logs' | 'eval' | 'report' | 'live' | null
type JobFilter = 'running' | 'enabled' | 'paused' | 'missed' | 'issues' | 'all'
type SchedulePanelView = 'overview' | 'calendar' | 'workflows'

interface JobPopupState {
  jobId: string
  jobName: string
  workspacePath: string
  runFolders: string[]
  popup: ActivePopup
  selectedRunFolder?: string
  sessionId?: string
  presetQueryId?: string
  startedAt?: string
}

type RunCostData = {
  cost: number
  tokens: number
  tierTokens?: Array<{ label: 'T1' | 'T2' | 'T3'; tokens: number }>
}

function getResolvedRunFolder(
  run: ScheduledJobRun,
  runIndex: number,
  inferredRunFolders: Record<string, string>,
  latestRunFolderForJob?: string
): string {
  if (run.run_folder) return run.run_folder
  if (inferredRunFolders[run.id]) return inferredRunFolders[run.id]
  if (runIndex === 0 && latestRunFolderForJob) return latestRunFolderForJob
  return ''
}

function getRunFolderIterationNumber(folderName: string): number {
  const match = folderName.match(/iteration-(\d+)/)
  return parseInt(match?.[1] ?? '-1', 10)
}

function getRunFolderActivityTime(folder: RunFolderInfo): number {
  const activityTimestamp =
    folder.metadata?.completed_at ||
    folder.metadata?.created_at ||
    ''

  const parsed = Date.parse(activityTimestamp)
  return Number.isNaN(parsed) ? 0 : parsed
}

function getMostRelevantRunFolder(folders: RunFolderInfo[]): string {
  if (folders.length === 0) return ''

  return [...folders]
    .sort((a, b) => {
      const activityDiff = getRunFolderActivityTime(b) - getRunFolderActivityTime(a)
      if (activityDiff !== 0) return activityDiff

      const iterationDiff = getRunFolderIterationNumber(b.name) - getRunFolderIterationNumber(a.name)
      if (iterationDiff !== 0) return iterationDiff

      return b.name.localeCompare(a.name)
    })[0]?.name ?? ''
}

function parseCronField(field: string, min: number, max: number, normalize?: (n: number) => number): number[] | null {
  const values = new Set<number>()
  const addValue = (n: number) => {
    const value = normalize ? normalize(n) : n
    if (value >= min && value <= max) values.add(value)
  }

  for (const rawPart of field.split(',')) {
    const part = rawPart.trim()
    if (!part) return null
    const [rangePart, stepPart] = part.split('/')
    const step = stepPart ? Number(stepPart) : 1
    if (!Number.isInteger(step) || step <= 0) return null

    let start: number
    let end: number
    if (rangePart === '*') {
      start = min
      end = max
    } else if (rangePart.includes('-')) {
      const [a, b] = rangePart.split('-').map(Number)
      if (!Number.isInteger(a) || !Number.isInteger(b)) return null
      start = a
      end = b
    } else {
      const single = Number(rangePart)
      if (!Number.isInteger(single)) return null
      start = single
      end = single
    }

    if (start > end) return null
    for (let n = start; n <= end; n += step) addValue(n)
  }

  return [...values].sort((a, b) => a - b)
}

function isWildcardCronField(field: string): boolean {
  return field.trim() === '*'
}

function expandCronForMonth(job: ScheduledJob, year: number, month: number): Array<{ date: string; time: string }> {
  if (!job.cron_expression) return []
  const parts = job.cron_expression.trim().split(/\s+/)
  if (parts.length !== 5) return []

  const minutes = parseCronField(parts[0], 0, 59)
  const hours = parseCronField(parts[1], 0, 23)
  const dom = parseCronField(parts[2], 1, 31)
  const months = parseCronField(parts[3], 1, 12)
  const dow = parseCronField(parts[4], 0, 6, n => n === 7 ? 0 : n)
  if (!minutes || !hours || !dom || !months || !dow) return []

  const monthNumber = month + 1
  if (!months.includes(monthNumber)) return []

  const domWildcard = isWildcardCronField(parts[2])
  const dowWildcard = isWildcardCronField(parts[4])
  const daysInMonth = new Date(year, month + 1, 0).getDate()
  const out: Array<{ date: string; time: string }> = []

  for (let day = 1; day <= daysInMonth; day += 1) {
    const d = new Date(year, month, day)
    const domMatches = dom.includes(day)
    const dowMatches = dow.includes(d.getDay())
    const dayMatches = domWildcard && dowWildcard
      ? true
      : domWildcard
        ? dowMatches
        : dowWildcard
          ? domMatches
          : domMatches || dowMatches
    if (!dayMatches) continue

    const date = `${year}-${String(monthNumber).padStart(2, '0')}-${String(day).padStart(2, '0')}`
    for (const hour of hours) {
      for (const minute of minutes) {
        out.push({ date, time: `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}` })
      }
    }
  }

  return out.slice(0, 250)
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

function normalizeModelIdentifier(value?: string): string {
  return (value || '').trim().toLowerCase()
}

function buildModelVariants(value?: string): Set<string> {
  const variants = new Set<string>()
  const normalized = normalizeModelIdentifier(value)
  if (!normalized) return variants

  variants.add(normalized)

  const slashPart = normalized.split('/').pop()
  if (slashPart) variants.add(slashPart)

  const colonPart = normalized.split(':').pop()
  if (colonPart) variants.add(colonPart)

  return variants
}

function getTierLabelForUsage(modelKey: string, usageProvider: string | undefined, models?: RunMetadataModels): 'T1' | 'T2' | 'T3' | null {
  if (models?.allocation_mode !== 'tiered') return null

  const usageVariants = buildModelVariants(modelKey)
  const normalizedProvider = normalizeModelIdentifier(usageProvider)

  const tierEntries: Array<{ label: 'T1' | 'T2' | 'T3'; model?: { provider?: string; model_id?: string } }> = [
    { label: 'T1', model: models.tier_1 },
    { label: 'T2', model: models.tier_2 },
    { label: 'T3', model: models.tier_3 },
  ]

  for (const entry of tierEntries) {
    const tierModel = entry.model
    if (!tierModel?.model_id) continue

    const tierProvider = normalizeModelIdentifier(tierModel.provider)
    if (tierProvider && normalizedProvider && tierProvider !== normalizedProvider) continue

    const tierVariants = buildModelVariants(tierModel.model_id)
    for (const variant of usageVariants) {
      if (tierVariants.has(variant)) {
        return entry.label
      }
    }
  }

  return null
}

function calculateTierTokenBreakdown(tokenUsage: TokenUsageFile | undefined, models?: RunMetadataModels): Array<{ label: 'T1' | 'T2' | 'T3'; tokens: number }> {
  if (!tokenUsage?.by_model || models?.allocation_mode !== 'tiered') return []

  const totals: Record<'T1' | 'T2' | 'T3', number> = { T1: 0, T2: 0, T3: 0 }

  for (const [modelKey, usage] of Object.entries(tokenUsage.by_model)) {
    const tierLabel = getTierLabelForUsage(modelKey, usage.provider, models)
    if (!tierLabel) continue
    totals[tierLabel] += (usage.input_tokens ?? 0) + (usage.output_tokens ?? 0)
  }

  return (['T1', 'T2', 'T3'] as const)
    .map(label => ({ label, tokens: totals[label] }))
    .filter(entry => entry.tokens > 0)
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

function getMissedScheduleDelayMs(job: ScheduledJob): number | null {
  if (!job.enabled || job.last_status === 'running' || !job.latest_missed_run_at) return null

  const missedAtMs = new Date(job.latest_missed_run_at).getTime()
  if (Number.isNaN(missedAtMs)) return null

  return Math.max(0, Date.now() - missedAtMs)
}

function isMissedSchedule(job: ScheduledJob): boolean {
  return !!job.enabled && (job.missed_run_count ?? 0) > 0
}

function formatOverdueDuration(durationMs: number): string {
  if (durationMs < 60_000) return `${Math.round(durationMs / 1000)}s`
  if (durationMs < 3_600_000) return `${Math.round(durationMs / 60_000)}m`
  if (durationMs < 86_400_000) return `${Math.round(durationMs / 3_600_000)}h`
  return `${Math.round(durationMs / 86_400_000)}d`
}

function formatLocalScheduleTime(dateStr?: string): string {
  if (!dateStr) return '—'

  try {
    const d = new Date(dateStr)
    const now = new Date()
    const diffDays = Math.floor((d.getTime() - now.getTime()) / 86400000)

    if (diffDays < 1) {
      return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: true })
    }

    if (diffDays < 7) {
      return d.toLocaleDateString([], { weekday: 'short', hour: '2-digit', minute: '2-digit', hour12: true })
    }

    return d.toLocaleDateString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: true })
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
      hour12: true,
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

function getWorkflowFilterMeta(
  job: ScheduledJob,
  presetMap: Map<string, { label: string; workspacePath: string | null }>
): { value: string; label: string; workflowLabel: string } {
  const workflowLabel = presetMap.get(job.preset_query_id ?? '')?.label || job.workflow_label || job.name
  const value = job.workflow_id || job.preset_query_id || job.workspace_path || workflowLabel

  return {
    value,
    label: workflowLabel,
    workflowLabel,
  }
}

function sortJobs(a: ScheduledJob, b: ScheduledJob): number {
  const rank = (job: ScheduledJob) => {
    if (job.last_status === 'running') return 0
    if (isMissedSchedule(job)) return 1
    if (job.enabled && job.next_run_at) return 2
    if (job.enabled) return 3
    if (job.last_status === 'error') return 4
    return 5
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
  const [expandedJobIds, setExpandedJobIds] = useState<string[]>([])
  const [activeView, setActiveView] = useState<SchedulePanelView>('workflows')
  const [calendarMonth, setCalendarMonth] = useState(() => new Date())
  const [activeFilter, setActiveFilter] = useState<JobFilter>('all')
  const [searchQuery, setSearchQuery] = useState('')
  const [selectedWorkflowFilter, setSelectedWorkflowFilter] = useState('all')
  const [schedulerConfig, setSchedulerConfig] = useState<SchedulerConfig | null>(null)
  const [isUpdatingSchedulerPause, setIsUpdatingSchedulerPause] = useState(false)
  const [showSchedulerDisabledNotice, setShowSchedulerDisabledNotice] = useState(false)

  // Keep a running workflow expanded so its live run history stays visible.
  useEffect(() => {
    const runningJobIds = jobs
      .filter(j => j.last_status === 'running')
      .map(j => j.id)

    if (runningJobIds.length === 0) return

    setExpandedJobIds(prev => {
      const next = [...prev]
      let changed = false
      runningJobIds.forEach((jobId) => {
        if (!next.includes(jobId)) {
          next.push(jobId)
          changed = true
        }
      })
      return changed ? next : prev
    })
  }, [jobs])
  const [triggering, setTriggering] = useState<string | null>(null)
  const [popupState, setPopupState] = useState<JobPopupState | null>(null)
  const [editingJob, setEditingJob] = useState<ScheduledJob | null>(null)
  // Run history per job
  const [jobRuns, setJobRuns] = useState<Record<string, ScheduledJobRun[]>>({})
  const [loadingRunIds, setLoadingRunIds] = useState<Record<string, boolean>>({})
  // Cost summary per run_folder: { totalCost, totalTokens }
  const [runCosts, setRunCosts] = useState<Record<string, RunCostData | null>>({})
  // For running runs without run_folder, we detect the latest iteration folder
  const [runningRunFolders, setRunningRunFolders] = useState<Record<string, string>>({})
  const [latestRunFoldersByJob, setLatestRunFoldersByJob] = useState<Record<string, string>>({})

  const { workflowPresets, refreshPresets } = useGlobalPresetStore()

  // Build presetId → {label, workspacePath} map
  const presetMap = React.useMemo(() => {
    const map = new Map<string, { label: string; workspacePath: string | null }>()
    workflowPresets.forEach((p) => {
      map.set(p.id, {
        label: p.label,
        workspacePath: p.selectedFolder?.filepath ?? null,
      })
    })
    return map
  }, [workflowPresets])

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
    setLoadingRunIds(prev => ({ ...prev, [jobId]: true }))
    try {
      const resp = await schedulerApi.getJobRuns(jobId, 20)
      setJobRuns(prev => ({ ...prev, [jobId]: resp.runs ?? [] }))
    } catch {
      setJobRuns(prev => ({ ...prev, [jobId]: [] }))
    } finally {
      setLoadingRunIds(prev => {
        const next = { ...prev }
        delete next[jobId]
        return next
      })
    }
  }, [])

  // Load cost summary for runs that have a run_folder (or detected running folder)
  const loadCostsForRuns = useCallback(async (jobId: string, runs: ScheduledJobRun[]) => {
    const job = jobs.find(j => j.id === jobId)
    const preset = presetMap.get(job?.preset_query_id ?? '')
    const workspacePath = job?.workspace_path || preset?.workspacePath
    if (!workspacePath) return

    const latestRunFolderForJob = latestRunFoldersByJob[jobId]
    const foldersToLoad = runs
      .map((run, index) => getResolvedRunFolder(run, index, runningRunFolders, latestRunFolderForJob))
      .filter((f): f is string => !!f && !(f in runCosts))

    if (foldersToLoad.length === 0) return

    // Fetch in parallel (limit to 5 to avoid overload)
    const batch = foldersToLoad.slice(0, 5)
    const runFoldersResp = await agentApi.getRunFolders(workspacePath).catch(() => null)
    const runFolderInfoMap = new Map<string, RunFolderInfo>(
      (runFoldersResp?.folders || []).map(folder => [folder.name, folder])
    )
    const workspaceCosts = await agentApi.getCosts(workspacePath).catch(() => null)
    const costByRunFolder = new Map(
      (workspaceCosts?.runs || []).map(entry => [entry.run_folder, entry])
    )

    const newCosts: Record<string, RunCostData | null> = {}
    batch.forEach((folder) => {
      const result = costByRunFolder.get(folder)
      if (result?.token_usage?.by_model) {
        let totalCost = 0
        let totalTokens = 0
        for (const model of Object.values(result.token_usage.by_model)) {
          totalCost += model.total_cost_usd ?? 0
          totalTokens += (model.input_tokens ?? 0) + (model.output_tokens ?? 0)
        }
        // Also add evaluation costs if present
        if (result.evaluation_token_usage?.by_model) {
          for (const model of Object.values(result.evaluation_token_usage.by_model)) {
            totalCost += model.total_cost_usd ?? 0
            totalTokens += (model.input_tokens ?? 0) + (model.output_tokens ?? 0)
          }
        }
        const tierTokens = calculateTierTokenBreakdown(
          result.token_usage,
          runFolderInfoMap.get(folder)?.metadata?.models
        )
        newCosts[folder] = {
          cost: totalCost,
          tokens: totalTokens,
          tierTokens: tierTokens.length > 0 ? tierTokens : undefined,
        }
      } else {
        newCosts[folder] = null
      }
    })
    setRunCosts(prev => ({ ...prev, ...newCosts }))
  }, [presetMap, jobs, runCosts, runningRunFolders, latestRunFoldersByJob])

  // Auto-load runs when a job is expanded
  useEffect(() => {
    if (expandedJobIds.length === 0) return
    expandedJobIds.forEach((jobId) => {
      loadRunsForJob(jobId)
    })
  }, [expandedJobIds, loadRunsForJob])

  // Detect the latest run folder so the newest completed run keeps its artifacts
  // even if the explicit run_folder arrives slightly after the run status update.
  const detectRunFolders = useCallback(async (jobId: string, runs: ScheduledJobRun[]) => {
    const djob = jobs.find(j => j.id === jobId)
    const preset = presetMap.get(djob?.preset_query_id ?? '')
    const workspacePath = djob?.workspace_path || preset?.workspacePath
    if (!workspacePath) return

    try {
      const resp = await agentApi.getRunFolders(workspacePath)
      const folders = resp.folders ?? []
      if (folders.length > 0) {
        const latestFolder = getMostRelevantRunFolder(folders)
        setLatestRunFoldersByJob(prev => (
          prev[jobId] === latestFolder ? prev : { ...prev, [jobId]: latestFolder }
        ))

        const newMap: Record<string, string> = {}
        runs
          .filter((run, index) => !run.run_folder && (run.status === 'running' || index === 0))
          .forEach((run) => { newMap[run.id] = latestFolder })

        if (Object.keys(newMap).length > 0) {
          setRunningRunFolders(prev => ({ ...prev, ...newMap }))
        }
      }
    } catch { /* ignore */ }
  }, [presetMap, jobs])

  // Auto-refresh while any job is running: jobs list + runs + costs (every 5s)
  const hasRunningJob = jobs.some(j => j.last_status === 'running')
  const activeJobs = jobs.filter(j => j.enabled).length
  const isSchedulerPaused = !!schedulerConfig?.globally_paused
  const isSchedulerExecutionEnabled = schedulerConfig?.execution_enabled !== false
  const schedulerDisabledReason = schedulerConfig?.disabled_reason

  const previousHasRunningJobRef = useRef(hasRunningJob)

  useEffect(() => {
    if (previousHasRunningJobRef.current && !hasRunningJob && expandedJobIds.length > 0) {
      expandedJobIds.forEach((jobId) => {
        loadRunsForJob(jobId)
      })
    }
    previousHasRunningJobRef.current = hasRunningJob
  }, [hasRunningJob, expandedJobIds, loadRunsForJob])

  useEffect(() => {
    if (schedulerConfig?.execution_enabled === false) {
      setShowSchedulerDisabledNotice(true)
    }
  }, [schedulerConfig?.execution_enabled])

  const summary = useMemo(() => {
    const running = jobs.filter(j => j.last_status === 'running').length
    const missed = jobs.filter(isMissedSchedule).length
    const issues = jobs.filter(j => j.last_status === 'error').length
    const paused = jobs.filter(j => !j.enabled).length
    return {
      running,
      missed,
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
      const meta = getWorkflowFilterMeta(job, presetMap)
      if (!meta.label) return
      if (!seen.has(meta.value)) seen.set(meta.value, meta.label)
    })

    return Array.from(seen.entries())
      .map(([value, label]) => ({ value, label }))
      .sort((a, b) => a.label.localeCompare(b.label))
  }, [jobs, presetMap])

  const upcomingJobs = useMemo(() => {
    return [...jobs]
      .filter(job => {
        if (!job.enabled || !job.next_run_at) return false
        if (isMissedSchedule(job)) return false
        if (selectedWorkflowFilter === 'all') return true
        return getWorkflowFilterMeta(job, presetMap).value === selectedWorkflowFilter
      })
      .sort((a, b) => (a.next_run_at || '').localeCompare(b.next_run_at || ''))
      .slice(0, 4)
  }, [jobs, presetMap, selectedWorkflowFilter])

  const missedJobs = useMemo(() => {
    return [...jobs]
      .filter(job => {
        if (!isMissedSchedule(job)) return false
        if (selectedWorkflowFilter === 'all') return true
        return getWorkflowFilterMeta(job, presetMap).value === selectedWorkflowFilter
      })
      .sort((a, b) => {
        const aDelay = getMissedScheduleDelayMs(a) ?? 0
        const bDelay = getMissedScheduleDelayMs(b) ?? 0
        return bDelay - aDelay
      })
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
          case 'missed':
            if (!isMissedSchedule(job)) return false
            break
          case 'issues':
            if (job.last_status !== 'error') return false
            break
          case 'all':
          default:
            break
        }

        const preset = presetMap.get(job.preset_query_id ?? '')
        const workflowMeta = getWorkflowFilterMeta(job, presetMap)
        const workflowLabel = workflowMeta.workflowLabel

        if (selectedWorkflowFilter !== 'all' && workflowMeta.value !== selectedWorkflowFilter) {
          return false
        }

        if (!normalizedSearch) return true

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

  const monthlyCalendar = useMemo(() => {
    const year = calendarMonth.getFullYear()
    const month = calendarMonth.getMonth()
    const monthKey = `${year}-${String(month + 1).padStart(2, '0')}`
    const byDate: Record<string, Array<{ job: ScheduledJob; time: string; label: string; note?: string }>> = {}

    jobs.forEach((job) => {
      const workflowMeta = getWorkflowFilterMeta(job, presetMap)
      const label = workflowMeta.workflowLabel || job.name

      if (job.schedule_type === 'calendar' && job.calendar_items?.length) {
        job.calendar_items.forEach((item) => {
          if (!item.date?.startsWith(monthKey)) return
          byDate[item.date] = [
            ...(byDate[item.date] || []),
            { job, time: item.time, label, note: item.description || job.name },
          ]
        })
        return
      }

      expandCronForMonth(job, year, month).forEach((occurrence) => {
        byDate[occurrence.date] = [
          ...(byDate[occurrence.date] || []),
          {
            job,
            time: occurrence.time,
            label,
            note: `${job.name}${job.timezone ? ` (${job.timezone})` : ''}`,
          },
        ]
      })
    })

    Object.values(byDate).forEach(items => items.sort((a, b) => a.time.localeCompare(b.time)))

    const first = new Date(year, month, 1)
    const daysInMonth = new Date(year, month + 1, 0).getDate()
    const cells: Array<{ key: string; day?: number; date?: string; items: Array<{ job: ScheduledJob; time: string; label: string; note?: string }> }> = []
    for (let i = 0; i < first.getDay(); i += 1) cells.push({ key: `empty-${i}`, items: [] })
    for (let day = 1; day <= daysInMonth; day += 1) {
      const date = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`
      cells.push({ key: date, day, date, items: byDate[date] || [] })
    }

    return {
      label: calendarMonth.toLocaleDateString([], { month: 'long', year: 'numeric' }),
      cells,
      total: Object.values(byDate).reduce((sum, items) => sum + items.length, 0),
    }
  }, [calendarMonth, jobs, presetMap])

  useEffect(() => {
    if (!hasRunningJob) return
    const interval = setInterval(async () => {
      loadJobs()
      await Promise.all(expandedJobIds.map(async (jobId) => {
        await loadRunsForJob(jobId)
        const runs = jobRuns[jobId] ?? []
        const latestRunFolderForJob = latestRunFoldersByJob[jobId]
        const runningFolders = runs
          .filter(r => r.status === 'running')
          .map((run, index) => getResolvedRunFolder(run, index, runningRunFolders, latestRunFolderForJob))
          .filter((f): f is string => !!f)
        if (runningFolders.length > 0) {
          setRunCosts(prev => {
            const next = { ...prev }
            runningFolders.forEach(f => delete next[f])
            return next
          })
        }
        detectRunFolders(jobId, runs)
      }))
    }, 5000)
    return () => clearInterval(interval)
  }, [hasRunningJob, loadJobs, expandedJobIds, jobRuns, runningRunFolders, latestRunFoldersByJob, detectRunFolders, loadRunsForJob])

  // Auto-load costs when runs are loaded
  useEffect(() => {
    expandedJobIds.forEach((jobId) => {
      const runs = jobRuns[jobId]
      if (!runs || runs.length === 0) return
      loadCostsForRuns(jobId, runs)
      detectRunFolders(jobId, runs)
    })
  }, [expandedJobIds, jobRuns, loadCostsForRuns, detectRunFolders])

  // Auto-refresh costs for running jobs (every 10s)
  useEffect(() => {
    const runningFoldersByJob = expandedJobIds
      .map((jobId) => {
        const runs = jobRuns[jobId]
        const latestRunFolderForJob = latestRunFoldersByJob[jobId]
        const runningFolders = (runs ?? [])
          .filter(r => r.status === 'running')
          .map((run, index) => getResolvedRunFolder(run, index, runningRunFolders, latestRunFolderForJob))
          .filter((f): f is string => !!f)
        return { jobId, runs, runningFolders }
      })
      .filter(({ runningFolders }) => runningFolders.length > 0)
    if (runningFoldersByJob.length === 0) return
    const interval = setInterval(() => {
      // Clear cached costs for running runs to force re-fetch
      setRunCosts(prev => {
        const next = { ...prev }
        runningFoldersByJob.forEach(({ runningFolders }) => {
          runningFolders.forEach(f => delete next[f])
        })
        return next
      })
      // Also re-detect folders for runs that still don't have one
      runningFoldersByJob.forEach(({ jobId, runs }) => {
        if (runs) detectRunFolders(jobId, runs)
      })
    }, 10000)
    return () => clearInterval(interval)
  }, [expandedJobIds, jobRuns, runningRunFolders, latestRunFoldersByJob, detectRunFolders])

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

  const openScheduledRunInChat = useCallback(async (
    sessionId: string,
    jobName: string,
    presetQueryId?: string,
  ) => {
    if (!sessionId) return

    const chatStore = useChatStore.getState()
    const existingTab = Object.values(chatStore.chatTabs).find(t => t.sessionId === sessionId)

    let effectivePresetQueryId = presetQueryId || existingTab?.metadata?.presetQueryId
    if (!effectivePresetQueryId) {
      try {
        const running = await agentApi.getRunningWorkflow(sessionId)
        effectivePresetQueryId = running.preset_query_id || undefined
      } catch {
        // Leave undefined rather than rebinding the scheduled run to whichever
        // workflow is currently open.
      }
    }

    if (effectivePresetQueryId) {
      useGlobalPresetStore.getState().setActivePreset('workflow', effectivePresetQueryId)
    }
    useWorkflowStore.getState().setShowChatArea(true)

    const desiredName = 'Schedule'
    const metadata = {
      mode: 'workflow' as const,
      phaseId: undefined,
      phaseName: undefined,
      ...(effectivePresetQueryId ? { presetQueryId: effectivePresetQueryId } : {}),
      isViewOnly: true,
      isScheduledRun: true,
      scheduledJobName: jobName,
    }

    if (existingTab) {
      // Rebind the tab to this preset and surface the schedule badge.
      chatStore.setTabMetadata(existingTab.tabId, metadata)
      if (existingTab.name !== desiredName) {
        useChatStore.setState((state) => {
          const t = state.chatTabs[existingTab.tabId]
          if (!t) return state
          return { chatTabs: { ...state.chatTabs, [existingTab.tabId]: { ...t, name: desiredName } } }
        })
      }
      try {
        const existingEvents = chatStore.getTabEvents(sessionId)
        const response = await agentApi.getSessionEvents(sessionId, existingEvents.length === 0 ? -1 : chatStore.getTabLastEventIndex(sessionId))
        if (response.events.length > 0) {
          if (existingEvents.length === 0) {
            chatStore.setTabEvents(sessionId, response.events)
          } else {
            chatStore.addTabEvents(sessionId, response.events)
          }
        }
        if (response.last_processed_index !== undefined) {
          chatStore.setTabLastEventIndex(sessionId, response.last_processed_index)
        }
        if (response.has_more !== undefined) {
          chatStore.setTabHasMoreOlderEvents(sessionId, response.has_more)
        }
        const isDone = response.session_status === 'completed' || response.session_status === 'stopped'
        const isError = response.session_status === 'error'
        chatStore.setTabCompleted(existingTab.tabId, isDone)
        chatStore.setTabStreaming(existingTab.tabId, !isDone && !isError && response.session_status === 'running')
        chatStore.setTabHasRunningBgAgents(existingTab.tabId, !!response.has_running_background_agents)
        chatStore.setTabSyntheticTurn(existingTab.tabId, !!response.is_synthetic_turn)
        chatStore.setTabCanSteer(existingTab.tabId, !!response.can_steer)
      } catch {
        // Leave the tab attached even if the ephemeral session buffer is gone.
      }
      chatStore.switchTab(existingTab.tabId)
      onClose()
      return
    }

    const tabId = await chatStore.createChatTab(
      desiredName,
      metadata,
      sessionId,
    )
    try {
      const response = await agentApi.getSessionEvents(sessionId, -1)
      if (response.events.length > 0) {
        chatStore.setTabEvents(sessionId, response.events)
      }
      if (response.last_processed_index !== undefined) {
        chatStore.setTabLastEventIndex(sessionId, response.last_processed_index)
      }
      if (response.has_more !== undefined) {
        chatStore.setTabHasMoreOlderEvents(sessionId, response.has_more)
      }
      const isDone = response.session_status === 'completed' || response.session_status === 'stopped'
      const isError = response.session_status === 'error'
      chatStore.setTabCompleted(tabId, isDone)
      chatStore.setTabStreaming(tabId, !isDone && !isError && response.session_status === 'running')
      chatStore.setTabHasRunningBgAgents(tabId, !!response.has_running_background_agents)
      chatStore.setTabSyntheticTurn(tabId, !!response.is_synthetic_turn)
      chatStore.setTabCanSteer(tabId, !!response.can_steer)
    } catch {
      // Scheduled run sessions are in-memory only; after restart there may be nothing to hydrate.
    }
    chatStore.switchTab(tabId)
    onClose()
  }, [onClose])

  const openWorkflowBuilderForSchedule = useCallback(async (
    presetQueryId?: string,
  ) => {
    if (!presetQueryId) return

    useGlobalPresetStore.getState().setActivePreset('workflow', presetQueryId)
    useWorkflowStore.getState().setShowChatArea(true)

    const chatStore = useChatStore.getState()
    const existingBuilderTab = Object.values(chatStore.chatTabs).find(tab =>
      tab.metadata?.mode === 'workflow' &&
      tab.metadata?.presetQueryId === presetQueryId &&
      tab.metadata?.phaseId === 'workflow-builder'
    )

    const builderMetadata = {
      mode: 'workflow' as const,
      phaseId: 'workflow-builder',
      phaseName: 'Workflow Builder',
      presetQueryId,
      isViewOnly: false,
      isScheduledRun: false,
      scheduledJobName: undefined,
    }

    if (existingBuilderTab) {
      chatStore.setTabMetadata(existingBuilderTab.tabId, builderMetadata)
      if (existingBuilderTab.name !== 'Workflow Builder') {
        useChatStore.setState((state) => {
          const t = state.chatTabs[existingBuilderTab.tabId]
          if (!t) return state
          return { chatTabs: { ...state.chatTabs, [existingBuilderTab.tabId]: { ...t, name: 'Workflow Builder' } } }
        })
      }
      chatStore.switchTab(existingBuilderTab.tabId)
      onClose()
      return
    }

    const tabId = await chatStore.createChatTab('Workflow Builder', builderMetadata)
    chatStore.switchTab(tabId)
    onClose()
  }, [onClose])

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
    const preset = presetMap.get(job.preset_query_id ?? '')
    const workspacePath = job.workspace_path || preset?.workspacePath || null
    if (!workspacePath) return

    const resolveStartedAt = (folder?: string): string | undefined => {
      const runs = jobRuns[job.id] ?? []
      if (folder) {
        const iterationKey = folder.includes('/') ? folder.split('/')[0] : folder
        const match = runs.find(r => r.run_folder === folder || r.run_folder === iterationKey)
        if (match?.started_at) return match.started_at
      }
      if (job.last_status === 'running' && runs[0]?.started_at) return runs[0].started_at
      return job.last_run_at
    }

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
      setPopupState({ jobId: job.id, jobName: job.name, workspacePath, runFolders, popup, selectedRunFolder, startedAt: resolveStartedAt(selectedRunFolder) })
    } catch {
      setPopupState({ jobId: job.id, jobName: job.name, workspacePath, runFolders: [], popup, selectedRunFolder, startedAt: resolveStartedAt(selectedRunFolder) })
    }
  }

  const filterPills: Array<{ key: JobFilter; label: string; count: number }> = [
    { key: 'running', label: 'Running', count: summary.running },
    { key: 'enabled', label: 'Enabled', count: summary.enabled },
    { key: 'paused', label: 'Paused', count: summary.paused },
    { key: 'missed', label: 'Missed', count: summary.missed },
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
                {summary.running} running · {summary.missed} missed · {!isSchedulerExecutionEnabled ? 'server scheduler disabled' : isSchedulerPaused ? 'globally paused' : `${activeJobs} active`} · {jobs.length} total
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
            <div className="sticky top-0 z-10 border-b border-border bg-card/95 backdrop-blur px-5 py-3">
              <div className="space-y-2">
                <div className="-mx-1 flex items-center gap-2 overflow-x-auto px-1 pb-1">
                  <button
                    onClick={() => setActiveView('overview')}
                    className={`shrink-0 rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                      activeView === 'overview'
                        ? 'bg-foreground text-background'
                        : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                    }`}
                  >
                    Overview
                  </button>
                  <button
                    onClick={() => {
                      setActiveView('workflows')
                      setSelectedWorkflowFilter('all')
                    }}
                    className={`shrink-0 rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                      activeView === 'workflows' && selectedWorkflowFilter === 'all'
                        ? 'bg-foreground text-background'
                        : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                    }`}
                  >
                    All Workflows
                  </button>
                  <button
                    onClick={() => setActiveView('calendar')}
                    className={`shrink-0 rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                      activeView === 'calendar'
                        ? 'bg-foreground text-background'
                        : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                    }`}
                  >
                    Month Calendar
                  </button>
                  {workflowOptions.map((option) => (
                    <button
                      key={option.value}
                      onClick={() => {
                        setActiveView('workflows')
                        setSelectedWorkflowFilter(option.value)
                      }}
                      className={`shrink-0 rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                        activeView === 'workflows' && selectedWorkflowFilter === option.value
                          ? 'bg-foreground text-background'
                          : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                      }`}
                      title={option.label}
                    >
                      {option.label}
                    </button>
                  ))}
                </div>

                <div className="text-xs text-muted-foreground">
                  {activeView === 'overview'
                    ? 'Summary and schedule health'
                    : activeView === 'calendar'
                      ? `${monthlyCalendar.total} scheduled item${monthlyCalendar.total === 1 ? '' : 's'} this month`
                      : `${filteredJobs.length} workflow${filteredJobs.length !== 1 ? 's' : ''} shown`}
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
          ) : activeView === 'overview' ? (
            <div className="px-5 py-4 space-y-4">
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
                  onClick={() => {
                    setActiveFilter('running')
                    setActiveView('workflows')
                  }}
                  className="text-left rounded-xl border border-border bg-background px-3 py-2 text-foreground shadow-sm hover:bg-muted transition-colors"
                >
                  <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Running now</div>
                  <div className="mt-1 flex items-center gap-2">
                    <Radio className="w-3.5 h-3.5 text-amber-500" />
                    <span className="text-lg font-semibold text-foreground">{summary.running}</span>
                  </div>
                </button>
                <button
                  onClick={() => {
                    setActiveFilter('enabled')
                    setActiveView('workflows')
                  }}
                  className="text-left rounded-xl border border-border bg-background px-3 py-2 text-foreground shadow-sm hover:bg-muted transition-colors"
                >
                  <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Enabled</div>
                  <div className="mt-1 text-lg font-semibold text-foreground">{summary.enabled}</div>
                </button>
                <button
                  onClick={() => {
                    setActiveFilter('paused')
                    setActiveView('workflows')
                  }}
                  className="text-left rounded-xl border border-border bg-background px-3 py-2 text-foreground shadow-sm hover:bg-muted transition-colors"
                >
                  <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Paused</div>
                  <div className="mt-1 text-lg font-semibold text-foreground">{summary.paused}</div>
                </button>
                <button
                  onClick={() => {
                    setActiveFilter('issues')
                    setActiveView('workflows')
                  }}
                  className="text-left rounded-xl border border-border bg-background px-3 py-2 text-foreground shadow-sm hover:bg-muted transition-colors"
                >
                  <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Issues</div>
                  <div className="mt-1 text-lg font-semibold text-foreground">{summary.issues}</div>
                </button>
              </div>

              {missedJobs.length > 0 && (
                <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 px-3 py-3">
                  <div className="flex items-center justify-between gap-3 mb-2">
                    <div>
                      <div className="text-[11px] uppercase tracking-wide text-amber-600 dark:text-amber-400">Missed schedules</div>
                      <div className="text-sm font-medium text-foreground">Schedules that were due but have not run yet</div>
                    </div>
                    <div className="text-xs text-muted-foreground whitespace-nowrap">
                      {summary.missed} missed
                    </div>
                  </div>

                  <div className="grid gap-2 md:grid-cols-2">
                    {missedJobs.map((job) => {
                      const preset = presetMap.get(job.preset_query_id ?? '')
                      const label = localizeTimezoneLabel(
                        preset?.label || job.workflow_label || job.name,
                        job.next_run_at
                      )
                      const overdueMs = getMissedScheduleDelayMs(job) ?? 0

                      return (
                        <button
                          key={`missed-${job.id}`}
                          onClick={() => {
                            setExpandedJobIds(prev => prev.includes(job.id) ? prev : [...prev, job.id])
                            setActiveView('workflows')
                          }}
                          className="rounded-lg border border-amber-400/30 bg-card px-3 py-2 text-left hover:bg-muted transition-colors"
                        >
                          <div className="flex items-center justify-between gap-3">
                            <span className="truncate text-sm font-medium text-foreground" title={label}>
                              {label}
                            </span>
                            <span className="text-xs font-medium text-amber-600 dark:text-amber-400 whitespace-nowrap">
                              {job.missed_run_count && job.missed_run_count > 1
                                ? `${job.missed_run_count} missed`
                                : `Missed by ${formatOverdueDuration(overdueMs)}`}
                            </span>
                          </div>
                          <div className="mt-1 flex items-center justify-between gap-3 text-xs text-muted-foreground">
                            <span className="truncate" title={job.name}>{getLocalizedJobName(job)}</span>
                            <span className="whitespace-nowrap">{formatLocalScheduleTime(job.latest_missed_run_at || job.next_run_at)}</span>
                          </div>
                        </button>
                      )
                    })}
                  </div>
                </div>
              )}

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
                      const preset = presetMap.get(job.preset_query_id ?? '')
                      const label = localizeTimezoneLabel(
                        preset?.label || job.workflow_label || job.name,
                        job.next_run_at
                      )
                      const localizedJobName = getLocalizedJobName(job)
                      return (
                        <button
                          key={`upcoming-${job.id}`}
                          onClick={() => {
                            setExpandedJobIds(prev => prev.includes(job.id) ? prev : [...prev, job.id])
                            setActiveView('workflows')
                          }}
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
            </div>
          ) : activeView === 'calendar' ? (
            <div className="px-5 py-4 space-y-4">
              <div className="flex items-center justify-between gap-3">
                <button
                  onClick={() => setCalendarMonth(prev => new Date(prev.getFullYear(), prev.getMonth() - 1, 1))}
                  className="rounded-md border border-border p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                >
                  <ChevronLeft className="h-4 w-4" />
                </button>
                <div className="text-center">
                  <div className="text-sm font-semibold text-foreground">{monthlyCalendar.label}</div>
                  <div className="text-xs text-muted-foreground">
                    {monthlyCalendar.total} scheduled item{monthlyCalendar.total === 1 ? '' : 's'}
                  </div>
                </div>
                <button
                  onClick={() => setCalendarMonth(prev => new Date(prev.getFullYear(), prev.getMonth() + 1, 1))}
                  className="rounded-md border border-border p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground"
                >
                  <ChevronRight className="h-4 w-4" />
                </button>
              </div>

              <div className="rounded-xl border border-border bg-background p-3">
                <div className="grid grid-cols-7 gap-1 text-center text-[11px] font-medium text-muted-foreground">
                  {['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].map(day => (
                    <div key={day} className="rounded-md bg-muted/40 py-1">{day}</div>
                  ))}
                </div>
                <div className="mt-1 grid grid-cols-7 gap-1">
                  {monthlyCalendar.cells.map((cell) => (
                    <div
                      key={cell.key}
                      className={`min-h-[112px] rounded-lg border p-2 ${
                        cell.day
                          ? cell.items.length
                            ? 'border-amber-500/30 bg-card shadow-[inset_0_0_0_1px_rgba(245,158,11,0.08)]'
                            : 'border-border bg-card/70'
                          : 'border-transparent'
                      }`}
                    >
                      {cell.day && (
                        <>
                          <div className="mb-1 flex items-center justify-between">
                            <span className="text-xs font-medium text-foreground">{cell.day}</span>
                            {cell.items.length > 0 && (
                              <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-1.5 py-0.5 text-[10px] font-medium text-amber-600 dark:text-amber-300">
                                {cell.items.length}
                              </span>
                            )}
                          </div>
                          <div className="space-y-1">
                            {cell.items.slice(0, 4).map((item, index) => (
                              <button
                                key={`${cell.date}-${item.job.id}-${index}`}
                                onClick={() => {
                                  setExpandedJobIds(prev => prev.includes(item.job.id) ? prev : [...prev, item.job.id])
                                  setActiveView('workflows')
                                }}
                                className="block w-full truncate rounded-md border border-border bg-muted/40 px-1.5 py-1 text-left text-[11px] leading-tight text-foreground hover:border-amber-500/30 hover:bg-muted"
                                title={`${item.time} ${item.label} - ${item.note || ''}`}
                              >
                                <span className="font-medium text-amber-600 dark:text-amber-300">{item.time}</span>
                                <span className="ml-1">{item.label}</span>
                              </button>
                            ))}
                            {cell.items.length > 4 && (
                              <div className="text-[11px] text-muted-foreground">+{cell.items.length - 4} more</div>
                            )}
                          </div>
                        </>
                      )}
                    </div>
                  ))}
                </div>
              </div>
            </div>
          ) : (
            <>
              <div className="border-b border-border px-5 py-4">
                <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
                  <div className="flex flex-1 max-w-3xl flex-col gap-3">
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
            </>
          )}
          {jobs.length > 0 && activeView === 'workflows' && filteredJobs.length === 0 && (
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
          )}
          {jobs.length > 0 && activeView === 'workflows' && filteredJobs.length > 0 && (
            <div className="divide-y divide-gray-100 dark:divide-gray-700">
              {filteredJobs.map((job, index, jobsList) => {
                const preset = presetMap.get(job.preset_query_id ?? '')
                const cronDesc = describeCron(job.cron_expression)
                const localizedJobName = getLocalizedJobName(job)
                const workflowDisplayLabel = preset?.label || job.workflow_label || job.name
                const isExpanded = expandedJobIds.includes(job.id)
                const hasWorkspace = !!job.workspace_path || !!preset?.workspacePath
                const runs = jobRuns[job.id] ?? []
                const isLoadingThis = !!loadingRunIds[job.id]
                const previousJob = index > 0 ? jobsList[index - 1] : null
                const isRunningJob = job.last_status === 'running'
                const isMissedJob = isMissedSchedule(job)
                const missedDelayMs = getMissedScheduleDelayMs(job)
                const showRunningHeader = isRunningJob && (!previousJob || previousJob.last_status !== 'running')
                const previousJobWasMissed = previousJob ? isMissedSchedule(previousJob) : false
                const showMissedHeader = isMissedJob && (!previousJob || previousJob.last_status === 'running' || !previousJobWasMissed)
                const showScheduledHeader = !isRunningJob && !isMissedJob && (!previousJob || previousJob.last_status === 'running' || previousJobWasMissed)

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

                    {showMissedHeader && (
                      <div className="px-5 py-3 bg-amber-500/5 border-b border-amber-500/10">
                        <div className="flex items-center justify-between gap-3">
                          <div>
                            <div className="text-[11px] uppercase tracking-wide text-amber-600 dark:text-amber-400">Missed schedules</div>
                            <div className="text-sm font-medium text-foreground">Schedules that were due, but never started at the scheduled time</div>
                          </div>
                          <div className="text-xs text-muted-foreground whitespace-nowrap">
                            {summary.missed} missed
                          </div>
                        </div>
                      </div>
                    )}

                    <div className={`px-5 py-4 ${!job.enabled || job.env_filtered ? 'opacity-60' : ''}`}>
                    {/* Row top */}
                    <div className="flex items-start gap-3">
                      {/* Status dot */}
                      <div className={`mt-1 w-2 h-2 rounded-full flex-shrink-0 ${
                        job.last_status === 'running' ? 'bg-amber-500 animate-pulse' :
                        isMissedJob ? 'bg-amber-500' :
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
                          {isMissedJob && (
                            <span className="text-xs px-1.5 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300">
                              Missed
                            </span>
                          )}
                          {!job.enabled && (
                            <span className="text-xs px-1.5 py-0.5 rounded-full bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400">
                              Paused
                            </span>
                          )}
                          {job.env_filtered && (
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <span className="text-xs px-1.5 py-0.5 rounded-full bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-300 border border-slate-200 dark:border-slate-700 cursor-help">
                                  Env-filtered
                                </span>
                              </TooltipTrigger>
                              <TooltipContent side="bottom" className="max-w-xs">
                                The cron is not registered on this machine — gated by SCHEDULER_ALLOWED_WORKFLOWS / SCHEDULER_BLOCKED_WORKFLOWS in agent_go/.env. Manual "Run now" still works; only automatic firing is suppressed.
                              </TooltipContent>
                            </Tooltip>
                          )}
                        </div>

                        {/* Cron + groups */}
                        <div className="mt-0.5 flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-gray-500 dark:text-gray-400">
                          <span className="flex items-center gap-1">
                            <Clock className="w-3 h-3" />
                            {cronDesc}
                          </span>
                          {job.group_names && job.group_names.length > 0 && (
                            <span>Groups: {job.group_names.join(', ')}</span>
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
                            {job.last_status === 'running'
                              ? job.last_run_at
                                ? `Running since ${formatLocalScheduleTime(job.last_run_at)} (${timeAgo(job.last_run_at)})`
                                : 'Running...'
                              : `Last: ${timeAgo(job.last_run_at)}`}
                          </span>
                          <span>
                            {job.env_filtered
                              ? "won't fire on this machine (env-filtered)"
                              : job.enabled
                              ? isMissedJob && missedDelayMs != null
                                ? job.missed_run_count && job.missed_run_count > 1
                                  ? `${job.missed_run_count} missed · latest ${formatOverdueDuration(missedDelayMs)} ago`
                                  : `Missed by ${formatOverdueDuration(missedDelayMs)}`
                                : `Next: ${formatLocalScheduleTime(job.next_run_at)}`
                              : 'paused'}
                          </span>
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
                                    className={`rounded-md transition-colors disabled:opacity-40 ${
                                      isMissedJob
                                        ? 'flex items-center gap-1 px-2 py-1 text-xs font-medium text-amber-700 dark:text-amber-300 bg-amber-50 dark:bg-amber-900/30 hover:bg-amber-100 dark:hover:bg-amber-900/50 border border-amber-200 dark:border-amber-800'
                                        : 'p-1.5 text-gray-400 hover:text-green-600 hover:bg-gray-100 dark:hover:bg-gray-700'
                                    }`}
                                  >
                                    <Play className="w-3.5 h-3.5" />
                                    {isMissedJob && <span>Run now</span>}
                                  </button>
                                </TooltipTrigger>
                                  <TooltipContent side="bottom">{isMissedJob ? 'Run this missed schedule now' : 'Trigger a manual run now'}</TooltipContent>
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
                          onClick={() => {
                            if (job.last_status === 'running') return
                            setExpandedJobIds(prev => (
                              isExpanded
                                ? prev.filter(id => id !== job.id)
                                : [...prev, job.id]
                            ))
                          }}
                          disabled={job.last_status === 'running'}
                          className="p-1.5 rounded-md text-gray-400 hover:text-gray-600 dark:hover:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-700 transition-colors disabled:opacity-50 disabled:hover:bg-transparent disabled:hover:text-gray-400"
                          title={job.last_status === 'running' ? 'Running workflows stay expanded' : undefined}
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
                              {runs.map((run, runIndex) => {
                                const effectiveFolder = getResolvedRunFolder(
                                  run,
                                  runIndex,
                                  runningRunFolders,
                                  latestRunFoldersByJob[job.id]
                                )
                                const currentSessionId =
                                  run.session_id || (run.status === 'running' ? job.last_session_id : undefined)
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
                                            className="flex items-center gap-1.5 text-amber-600 dark:text-amber-400 hover:text-amber-700 dark:hover:text-amber-300 flex-shrink-0 transition-colors"
                                          >
                                            <span>{formatCost(costData.cost)}</span>
                                            <span className="text-gray-400 dark:text-gray-500">{formatTokens(costData.tokens)}t</span>
                                            {costData.tierTokens && costData.tierTokens.length > 0 && (
                                              <span className="flex items-center gap-1 text-[10px] text-gray-400 dark:text-gray-500">
                                                {costData.tierTokens.map((tier) => (
                                                  <span
                                                    key={`${effectiveFolder}-${tier.label}`}
                                                    className="rounded border border-amber-200/60 bg-amber-50 px-1 py-0.5 text-[10px] font-medium text-amber-700 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-300"
                                                  >
                                                    {tier.label} {formatTokens(tier.tokens)}
                                                  </span>
                                                ))}
                                              </span>
                                            )}
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">
                                          {costData.tierTokens && costData.tierTokens.length > 0
                                            ? `Click for full cost breakdown. Tier tokens: ${costData.tierTokens.map(tier => `${tier.label} ${formatTokens(tier.tokens)}`).join(' · ')}`
                                            : 'Click for full cost breakdown'}
                                        </TooltipContent>
                                      </Tooltip>
                                    )
                                  })()}

                                  {/* Groups */}
                                  {run.group_names && run.group_names.length > 0 && (
                                    <span className="text-gray-400 truncate" title={`Groups: ${run.group_names.join(', ')}`}>
                                      [{run.group_names.length}g]
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
                                    {run.status === 'running' && currentSessionId && (
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            onClick={() => setPopupState({
                                              jobId: job.id,
                                              jobName: job.name,
                                              workspacePath: job.workspace_path || '',
                                              runFolders: [],
                                              popup: 'live',
                                              sessionId: currentSessionId,
                                              presetQueryId: job.preset_query_id,
                                              startedAt: run.started_at,
                                            })}
                                            className="p-1 text-green-500 hover:text-green-400 animate-pulse transition-colors"
                                          >
                                            <Radio className="w-3 h-3" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">Live execution view</TooltipContent>
                                      </Tooltip>
                                    )}
                                    {/* Open the scheduled run itself as a read-only chat tab. */}
                                    {currentSessionId && (
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            onClick={() => openScheduledRunInChat(currentSessionId, job.name, job.preset_query_id)}
                                            className="p-1 text-blue-500 hover:text-blue-400 transition-colors"
                                          >
                                            <MessageSquare className="w-3 h-3" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">
                                          {run.status === 'running'
                                            ? 'Open run in chat (read-only)'
                                            : 'Restore to chat (read-only)'}
                                        </TooltipContent>
                                      </Tooltip>
                                    )}
                                    {run.status === 'running' && job.preset_query_id && (
                                      <Tooltip>
                                        <TooltipTrigger asChild>
                                          <button
                                            onClick={() => openWorkflowBuilderForSchedule(job.preset_query_id)}
                                            className="p-1 text-violet-500 hover:text-violet-400 transition-colors"
                                          >
                                            <Workflow className="w-3 h-3" />
                                          </button>
                                        </TooltipTrigger>
                                        <TooltipContent side="left">Open workflow builder</TooltipContent>
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
                                    {effectiveFolder && run.status !== 'running' && (
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
                                    {effectiveFolder && run.status !== 'running' && (
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
                                    {effectiveFolder && run.status !== 'running' && (
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
          startedAt={popupState.startedAt}
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
          startedAt={popupState.startedAt}
        />
      )}

      {/* Live events popup */}
      {popupState?.popup === 'live' && popupState.sessionId && (
        <ScheduleLiveEventsPopup
          sessionId={popupState.sessionId}
          jobName={popupState.jobName}
          onClose={() => setPopupState(null)}
          onOpenInChat={() => {
            const sid = popupState.sessionId!
            const name = popupState.jobName
            const pid = popupState.presetQueryId
            setPopupState(null)
            openScheduledRunInChat(sid, name, pid)
          }}
          startedAt={popupState.startedAt}
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
          startedAt={popupState.startedAt}
        />
      )}

      {/* Dynamic report viewer (replaces the deleted static FinalOutputPopup).
          The report is workspace-scoped now — runFolders/selectedRunFolder are ignored. */}
      {popupState?.popup === 'report' && (
        <ReportViewer
          isOpen
          onClose={() => setPopupState(null)}
          workspacePath={popupState.workspacePath}
        />
      )}

      {/* Edit schedule popup */}
      {editingJob && (() => {
        const preset = presetMap.get(editingJob.preset_query_id ?? '')
        return (
          <SchedulePresetPopup
            presetQueryId={editingJob.preset_query_id ?? null}
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
