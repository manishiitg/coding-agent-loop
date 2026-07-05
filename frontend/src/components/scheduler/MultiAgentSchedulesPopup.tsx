import React, { useState, useEffect, useCallback, useMemo } from 'react'
import cronstrue from 'cronstrue'
import { X, CalendarDays, Play, Trash2, Square, ToggleLeft, ToggleRight, RefreshCw, AlertCircle, CheckCircle2, Clock, ClockAlert } from 'lucide-react'
import { schedulerApi } from '../../api/scheduler'
import type { ScheduledJob } from '../../services/api-types'
import ModalPortal from '../ui/ModalPortal'

const MISSED_SCHEDULE_GRACE_MS = 60_000
const ORG_PULSE_JOB_ID = 'builtin-org-pulse'
type JobFilter = 'running' | 'enabled' | 'paused' | 'missed' | 'issues' | 'all'

function describeCron(expr: string): string {
  try {
    return cronstrue.toString(expr, { throwExceptionOnParseError: true })
  } catch {
    return expr
  }
}

function formatRelativeTime(dateStr: string): string {
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = date.getTime() - now.getTime()
  const absDiff = Math.abs(diffMs)
  const isPast = diffMs < 0

  if (absDiff < 60_000) return isPast ? 'just now' : 'in a moment'
  if (absDiff < 3600_000) {
    const mins = Math.round(absDiff / 60_000)
    return isPast ? `${mins}m ago` : `in ${mins}m`
  }
  if (absDiff < 86400_000) {
    const hrs = Math.round(absDiff / 3600_000)
    return isPast ? `${hrs}h ago` : `in ${hrs}h`
  }
  const days = Math.round(absDiff / 86400_000)
  return isPast ? `${days}d ago` : `in ${days}d`
}

function formatLastRunLabel(dateStr?: string): string {
  if (!dateStr) return 'never'
  return formatRelativeTime(dateStr)
}

function getMissedScheduleDelayMs(job: ScheduledJob): number | null {
  if (!job.enabled || job.last_status === 'running' || !job.next_run_at) return null

  const scheduledAtMs = new Date(job.next_run_at).getTime()
  if (Number.isNaN(scheduledAtMs)) return null

  const overdueMs = Date.now() - scheduledAtMs
  if (overdueMs < MISSED_SCHEDULE_GRACE_MS) return null

  return overdueMs
}

function formatDurationShort(durationMs: number): string {
  if (durationMs < 60_000) return `${Math.round(durationMs / 1000)}s`
  if (durationMs < 3_600_000) return `${Math.round(durationMs / 60_000)}m`
  if (durationMs < 86_400_000) return `${Math.round(durationMs / 3_600_000)}h`
  return `${Math.round(durationMs / 86_400_000)}d`
}

function sortJobs(a: ScheduledJob, b: ScheduledJob): number {
  const rank = (job: ScheduledJob) => {
    if (job.last_status === 'running') return 0
    if (getMissedScheduleDelayMs(job) != null) return 1
    if (job.enabled && job.next_run_at) return 2
    if (job.enabled) return 3
    if (job.last_status === 'error') return 4
    return 5
  }

  const rankDiff = rank(a) - rank(b)
  if (rankDiff !== 0) return rankDiff

  if (a.next_run_at && b.next_run_at) {
    return a.next_run_at.localeCompare(b.next_run_at)
  }

  return a.name.localeCompare(b.name)
}

function isOrgPulseJob(job: ScheduledJob): boolean {
  if (job.id === ORG_PULSE_JOB_ID) return true
  const haystack = `${job.name}\n${job.description || ''}\n${job.query || ''}`.toLowerCase()
  return haystack.includes('org pulse') || haystack.includes('org-pulse')
}

function getSlashManagedCommand(job: ScheduledJob): string {
  if (isOrgPulseJob(job)) return '/pulse-setup'
  if (job.managed_by === 'slash-command') return 'setup command'
  return ''
}

function isSlashManagedJob(job: ScheduledJob): boolean {
  return getSlashManagedCommand(job) !== ''
}

function managedScheduleError(job: ScheduledJob, action: string): string {
  const command = getSlashManagedCommand(job)
  if (command && command !== 'setup command') {
    return `Use ${command} in Chief of Staff to ${action}.`
  }
  return `Use the setup command in Chief of Staff to ${action}.`
}

function isBuiltInJob(job: ScheduledJob): boolean {
  return !!job.built_in || job.id.startsWith('builtin-')
}

interface MultiAgentSchedulesPopupProps {
  onClose: () => void
}

const MultiAgentSchedulesPopup: React.FC<MultiAgentSchedulesPopupProps> = ({ onClose }) => {
  const [jobs, setJobs] = useState<ScheduledJob[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionInProgress, setActionInProgress] = useState<string | null>(null) // jobID being acted on
  const [activeFilter, setActiveFilter] = useState<JobFilter>('all')

  const loadJobs = useCallback(async () => {
    try {
      setError(null)
      const resp = await schedulerApi.listJobs({ mode: 'multi-agent' })
      setJobs(resp.jobs || [])
    } catch {
      setError('Failed to load schedules')
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    loadJobs()
    // Refresh every 30s while open
    const interval = setInterval(loadJobs, 30_000)
    return () => clearInterval(interval)
  }, [loadJobs])

  const summary = useMemo(() => {
    let running = 0
    let enabled = 0
    let paused = 0
    let missed = 0
    let issues = 0
    let lastRunAt = ''

    for (const job of jobs) {
      if (job.last_status === 'running') running += 1
      if (job.enabled) enabled += 1
      else paused += 1
      if (getMissedScheduleDelayMs(job) != null) missed += 1
      if (job.last_status === 'error') issues += 1
      if (job.last_run_at && (!lastRunAt || job.last_run_at > lastRunAt)) {
        lastRunAt = job.last_run_at
      }
    }

    return {
      total: jobs.length,
      running,
      enabled,
      paused,
      missed,
      issues,
      lastRunAt
    }
  }, [jobs])

  const sortedJobs = useMemo(
    () => [...jobs].sort(sortJobs),
    [jobs]
  )

  const filteredJobs = useMemo(() => {
    return sortedJobs.filter(job => {
      switch (activeFilter) {
        case 'running':
          return job.last_status === 'running'
        case 'enabled':
          return job.enabled
        case 'paused':
          return !job.enabled
        case 'missed':
          return getMissedScheduleDelayMs(job) != null
        case 'issues':
          return job.last_status === 'error'
        case 'all':
        default:
          return true
      }
    })
  }, [activeFilter, sortedJobs])

  const handleToggle = async (job: ScheduledJob) => {
    if (isSlashManagedJob(job)) {
      setError(managedScheduleError(job, 'change this schedule'))
      return
    }
    setActionInProgress(job.id)
    try {
      const updated = job.enabled
        ? await schedulerApi.disableJob(job.id)
        : await schedulerApi.enableJob(job.id)
      setJobs(prev => prev.map(j => j.id === job.id ? updated : j))
    } catch {
      setError(`Failed to ${job.enabled ? 'disable' : 'enable'} schedule`)
    } finally {
      setActionInProgress(null)
    }
  }

  const handleTrigger = async (job: ScheduledJob) => {
    if (isSlashManagedJob(job)) {
      setError(managedScheduleError(job, 'run this schedule'))
      return
    }
    setActionInProgress(job.id)
    try {
      await schedulerApi.triggerJob(job.id)
      // Refresh to show running status
      setTimeout(loadJobs, 1000)
    } catch {
      setError('Failed to trigger schedule')
    } finally {
      setActionInProgress(null)
    }
  }

  const handleStop = async (job: ScheduledJob) => {
    setActionInProgress(job.id)
    try {
      const updated = await schedulerApi.stopJob(job.id)
      setJobs(prev => prev.map(j => j.id === job.id ? updated : j))
    } catch {
      setError('Failed to stop schedule')
    } finally {
      setActionInProgress(null)
    }
  }

  const handleDelete = async (job: ScheduledJob) => {
    if (isSlashManagedJob(job)) {
      setError(managedScheduleError(job, 'disable or change this schedule'))
      return
    }
    if (!window.confirm(`Delete schedule "${job.name}"?`)) return
    setActionInProgress(job.id)
    try {
      await schedulerApi.deleteJob(job.id)
      setJobs(prev => prev.filter(j => j.id !== job.id))
    } catch {
      setError('Failed to delete schedule')
    } finally {
      setActionInProgress(null)
    }
  }

  const statusBadge = (job: ScheduledJob) => {
    const missedDelayMs = getMissedScheduleDelayMs(job)
    if (job.last_status === 'running') {
      return <span className="inline-flex items-center gap-1 rounded-full border border-amber-500/30 bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:text-amber-300">
        <span className="h-1.5 w-1.5 rounded-full bg-amber-500 animate-pulse" /> Running
      </span>
    }
    if (missedDelayMs != null) {
      return <span className="inline-flex items-center gap-1 rounded-full border border-amber-500/30 bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:text-amber-300">
        <ClockAlert className="h-3 w-3" /> Missed
      </span>
    }
    if (job.last_status === 'error') {
      return <span className="inline-flex items-center gap-1 rounded-full border border-red-500/30 bg-red-500/10 px-1.5 py-0.5 text-[11px] font-medium text-red-600 dark:text-red-300">
        <AlertCircle className="h-3 w-3" /> Issue
      </span>
    }
    if (job.last_status === 'success') {
      return <span className="inline-flex items-center gap-1 rounded-full border border-green-500/30 bg-green-500/10 px-1.5 py-0.5 text-[11px] font-medium text-green-700 dark:text-green-300">
        <CheckCircle2 className="h-3 w-3" /> Success
      </span>
    }
    return null
  }

  const filterPills: Array<{ key: JobFilter; label: string; count: number }> = [
    { key: 'running', label: 'Running', count: summary.running },
    { key: 'enabled', label: 'Enabled', count: summary.enabled },
    { key: 'paused', label: 'Paused', count: summary.paused },
    { key: 'missed', label: 'Missed', count: summary.missed },
    { key: 'issues', label: 'Issues', count: summary.issues },
    { key: 'all', label: 'All', count: summary.total },
  ]

  return (
    <ModalPortal>
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 p-2 sm:p-4"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="flex max-h-[calc(100dvh-1rem)] w-full max-w-4xl flex-col overflow-hidden rounded-xl border border-border bg-card text-card-foreground shadow-2xl sm:max-h-[85vh]">
        {/* Header */}
        <div className="flex flex-shrink-0 items-start justify-between gap-4 border-b border-border px-5 py-4">
          <div className="min-w-0 space-y-2">
            <div className="flex items-center gap-2">
              <CalendarDays className="h-5 w-5 text-amber-500" />
              <h3 className="truncate text-base font-semibold text-foreground">Scheduled Chief of Staff Tasks</h3>
            </div>
            {!isLoading && (
              <div className="flex flex-wrap gap-1.5">
                <span className="rounded-full border border-border bg-background px-2 py-0.5 text-xs text-muted-foreground">
                  {summary.total} schedule{summary.total === 1 ? '' : 's'}
                </span>
                <span className="rounded-full border border-border bg-background px-2 py-0.5 text-xs text-muted-foreground">
                  {summary.enabled} active
                </span>
                {summary.total > 0 && (
                  <span className="rounded-full border border-border bg-background px-2 py-0.5 text-xs text-muted-foreground">
                    Last ran {formatLastRunLabel(summary.lastRunAt)}
                  </span>
                )}
                {summary.running > 0 && (
                  <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300">
                    {summary.running} running
                  </span>
                )}
                {summary.missed > 0 && (
                  <span className="inline-flex items-center gap-1 rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300">
                    <ClockAlert className="h-3 w-3" />
                    {summary.missed} missed
                  </span>
                )}
                {summary.issues > 0 && (
                  <span className="rounded-full border border-red-500/30 bg-red-500/10 px-2 py-0.5 text-xs font-medium text-red-600 dark:text-red-300">
                    {summary.issues} issue{summary.issues === 1 ? '' : 's'}
                  </span>
                )}
              </div>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <button
              onClick={loadJobs}
              className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              title="Refresh"
              aria-label="Refresh schedules"
            >
              <RefreshCw className={`h-4 w-4 ${isLoading ? 'animate-spin' : ''}`} />
            </button>
            <button
              onClick={onClose}
              className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
              aria-label="Close schedules"
            >
              <X className="h-4 w-4" />
            </button>
          </div>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto">
          {!isLoading && jobs.length > 0 && (
            <div className="sticky top-0 z-10 border-b border-border bg-card/95 px-5 py-3 backdrop-blur">
              <div className="-mx-1 flex items-center gap-2 overflow-x-auto px-1 pb-1">
                {filterPills.map(pill => (
                  <button
                    key={pill.key}
                    onClick={() => setActiveFilter(pill.key)}
                    className={`shrink-0 rounded-full px-3 py-1.5 text-xs font-medium transition-colors ${
                      activeFilter === pill.key
                        ? 'bg-foreground text-background'
                        : 'border border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
                    }`}
                  >
                    {pill.label}
                    <span className="ml-1 opacity-70">{pill.count}</span>
                  </button>
                ))}
              </div>
              <div className="pt-1 text-xs text-muted-foreground">
                {filteredJobs.length} schedule{filteredJobs.length === 1 ? '' : 's'} shown
              </div>
            </div>
          )}

          <div className="px-5 py-4">
            {error && (
              <div className="mb-3 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2 text-xs text-red-600 dark:text-red-300">
                {error}
              </div>
            )}

            {isLoading && jobs.length === 0 ? (
              <div className="flex h-40 items-center justify-center text-sm text-muted-foreground">Loading schedules...</div>
            ) : jobs.length === 0 ? (
              <div className="flex h-48 flex-col items-center justify-center gap-3 px-6 text-center text-sm text-muted-foreground">
                <CalendarDays className="h-8 w-8 opacity-30" />
                <div>
                  <p>No scheduled Chief of Staff tasks yet.</p>
                  <p className="mt-1 text-xs">Ask Chief of Staff to schedule a recurring task when you are ready.</p>
                </div>
              </div>
            ) : filteredJobs.length === 0 ? (
              <div className="flex h-40 flex-col items-center justify-center gap-3 px-6 text-center text-sm text-muted-foreground">
                <Clock className="h-8 w-8 opacity-30" />
                <div>
                  <p>No schedules match this filter.</p>
                  <button
                    onClick={() => setActiveFilter('all')}
                    className="mt-2 rounded-md border border-border px-2.5 py-1 text-xs font-medium text-foreground hover:bg-muted"
                  >
                    Show all schedules
                  </button>
                </div>
              </div>
            ) : (
              <div className="overflow-hidden rounded-xl border border-border bg-background/50">
                <div className="divide-y divide-border">
                  {filteredJobs.map((job) => {
                    const missedDelayMs = getMissedScheduleDelayMs(job)
                    const slashManagedCommand = getSlashManagedCommand(job)
                    const slashManagedJob = slashManagedCommand !== ''
                    const builtInJob = isBuiltInJob(job)
                    const isRunning = job.last_status === 'running'

                    return (
                      <div key={job.id} className={`px-4 py-3 ${!job.enabled ? 'bg-muted/20' : ''}`}>
                        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                          <div className="min-w-0 flex-1">
                            <div className="flex items-start gap-3">
                              <div className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${
                                isRunning ? 'bg-amber-500 animate-pulse' :
                                missedDelayMs != null ? 'bg-amber-500' :
                                job.last_status === 'error' ? 'bg-red-500' :
                                job.enabled ? 'bg-green-500' : 'bg-gray-300 dark:bg-gray-600'
                              }`} />
                              <div className="min-w-0 flex-1">
                                <div className="flex min-w-0 flex-wrap items-center gap-2">
                                  <span className="truncate text-sm font-medium text-foreground" title={job.name}>
                                    {job.name}
                                  </span>
                                  {statusBadge(job)}
                                  {!job.enabled && (
                                    <span className="rounded-full border border-border bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">
                                      Paused
                                    </span>
                                  )}
                                  {slashManagedCommand && slashManagedCommand !== 'setup command' && (
                                    <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:text-amber-300">
                                      {slashManagedCommand}
                                    </span>
                                  )}
                                  {builtInJob && !slashManagedJob && (
                                    <span className="rounded-full border border-border bg-muted px-1.5 py-0.5 text-[11px] font-medium text-muted-foreground">
                                      Built-in
                                    </span>
                                  )}
                                </div>

                                <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                                  <span className="flex items-center gap-1">
                                    <Clock className="h-3 w-3" />
                                    {describeCron(job.cron_expression)}
                                    {job.timezone && <span>({job.timezone})</span>}
                                  </span>
                                  <span>
                                    {job.enabled
                                      ? missedDelayMs != null
                                        ? `Missed by ${formatDurationShort(missedDelayMs)}`
                                        : job.next_run_at
                                          ? `Next ${formatRelativeTime(job.next_run_at)}`
                                          : 'No next run scheduled'
                                      : 'Paused'}
                                  </span>
                                  <span>Last ran {formatLastRunLabel(job.last_run_at)}</span>
                                  <span>{job.run_count} run{job.run_count !== 1 ? 's' : ''}</span>
                                  {slashManagedJob && <span>Managed in chat</span>}
                                  {builtInJob && !slashManagedJob && <span>System schedule</span>}
                                </div>

                                {job.query && (
                                  <p className="mt-1 truncate text-xs text-muted-foreground" title={job.query}>
                                    {job.query}
                                  </p>
                                )}

                                {job.last_error && job.last_status === 'error' && (
                                  <p className="mt-1 truncate text-xs text-red-500" title={job.last_error}>
                                    {job.last_error}
                                  </p>
                                )}
                              </div>
                            </div>
                          </div>

                          <div className="flex shrink-0 flex-wrap items-center gap-1 sm:justify-end">
                            {isRunning ? (
                              <button
                                onClick={() => handleStop(job)}
                                disabled={actionInProgress === job.id}
                                className="inline-flex items-center gap-1 rounded-md border border-red-500/30 bg-red-500/10 px-2 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-500/20 disabled:opacity-50 dark:text-red-300"
                                title="Stop"
                              >
                                <Square className="h-3 w-3" />
                                Stop
                              </button>
                            ) : slashManagedJob ? (
                              <span className="rounded-md border border-border bg-muted px-2 py-1 text-xs font-medium text-muted-foreground">
                                Managed in chat
                              </span>
                            ) : (
                              <>
                                <button
                                  onClick={() => handleToggle(job)}
                                  disabled={actionInProgress === job.id}
                                  className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
                                  title={job.enabled ? 'Disable schedule' : 'Enable schedule'}
                                >
                                  {job.enabled
                                    ? <ToggleRight className="h-4 w-4 text-green-600 dark:text-green-300" />
                                    : <ToggleLeft className="h-4 w-4" />
                                  }
                                </button>
                                <button
                                  onClick={() => handleTrigger(job)}
                                  disabled={actionInProgress === job.id}
                                  className={`rounded-md transition-colors disabled:opacity-50 ${
                                    missedDelayMs != null
                                      ? 'inline-flex items-center gap-1 border border-amber-500/30 bg-amber-500/10 px-2 py-1 text-xs font-medium text-amber-700 hover:bg-amber-500/20 dark:text-amber-300'
                                      : 'p-1.5 text-muted-foreground hover:bg-muted hover:text-green-600'
                                  }`}
                                  title={missedDelayMs != null ? 'Run missed schedule now' : 'Run now'}
                                >
                                  <Play className="h-3.5 w-3.5" />
                                  {missedDelayMs != null && <span>Run now</span>}
                                </button>
                                {!builtInJob && (
                                  <button
                                    onClick={() => handleDelete(job)}
                                    disabled={actionInProgress === job.id}
                                    className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-red-600 disabled:opacity-50"
                                    title="Delete schedule"
                                  >
                                    <Trash2 className="h-3.5 w-3.5" />
                                  </button>
                                )}
                              </>
                            )}
                          </div>
                        </div>
                      </div>
                    )
                  })}
                </div>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
    </ModalPortal>
  )
}

export default MultiAgentSchedulesPopup
