import React, { useState, useEffect, useCallback } from 'react'
import ReactDOM from 'react-dom'
import cronstrue from 'cronstrue'
import { X, CalendarDays, Play, Trash2, Square, ToggleLeft, ToggleRight, RefreshCw, AlertCircle, CheckCircle2 } from 'lucide-react'
import { schedulerApi } from '../../api/scheduler'
import type { ScheduledJob } from '../../services/api-types'

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

interface MultiAgentSchedulesPopupProps {
  onClose: () => void
}

const MultiAgentSchedulesPopup: React.FC<MultiAgentSchedulesPopupProps> = ({ onClose }) => {
  const [jobs, setJobs] = useState<ScheduledJob[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [actionInProgress, setActionInProgress] = useState<string | null>(null) // jobID being acted on

  const loadJobs = useCallback(async () => {
    try {
      setError(null)
      const resp = await schedulerApi.listJobs({ mode: 'multi-agent' })
      setJobs(resp.jobs || [])
    } catch (err) {
      setError('Failed to load schedules')
      console.error(err)
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

  const handleToggle = async (job: ScheduledJob) => {
    setActionInProgress(job.id)
    try {
      const updated = job.enabled
        ? await schedulerApi.disableJob(job.id)
        : await schedulerApi.enableJob(job.id)
      setJobs(prev => prev.map(j => j.id === job.id ? updated : j))
    } catch (err) {
      setError(`Failed to ${job.enabled ? 'disable' : 'enable'} schedule`)
    } finally {
      setActionInProgress(null)
    }
  }

  const handleTrigger = async (job: ScheduledJob) => {
    setActionInProgress(job.id)
    try {
      await schedulerApi.triggerJob(job.id)
      // Refresh to show running status
      setTimeout(loadJobs, 1000)
    } catch (err) {
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
    } catch (err) {
      setError('Failed to stop schedule')
    } finally {
      setActionInProgress(null)
    }
  }

  const handleDelete = async (job: ScheduledJob) => {
    if (!window.confirm(`Delete schedule "${job.name}"?`)) return
    setActionInProgress(job.id)
    try {
      await schedulerApi.deleteJob(job.id)
      setJobs(prev => prev.filter(j => j.id !== job.id))
    } catch (err) {
      setError('Failed to delete schedule')
    } finally {
      setActionInProgress(null)
    }
  }

  const statusBadge = (job: ScheduledJob) => {
    if (job.last_status === 'running') {
      return <span className="inline-flex items-center gap-1 text-[10px] font-medium px-1.5 py-0.5 rounded-full bg-blue-900/40 text-blue-400 border border-blue-700/50">
        <span className="w-1.5 h-1.5 rounded-full bg-blue-400 animate-pulse" /> Running
      </span>
    }
    if (job.last_status === 'error') {
      return <span className="inline-flex items-center gap-1 text-[10px] font-medium px-1.5 py-0.5 rounded-full bg-red-900/30 text-red-400 border border-red-700/50">
        <AlertCircle className="w-3 h-3" /> Error
      </span>
    }
    if (job.last_status === 'success') {
      return <span className="inline-flex items-center gap-1 text-[10px] font-medium px-1.5 py-0.5 rounded-full bg-green-900/30 text-green-400 border border-green-700/50">
        <CheckCircle2 className="w-3 h-3" /> Success
      </span>
    }
    return null
  }

  return ReactDOM.createPortal(
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/50 backdrop-blur-sm"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="w-[520px] max-h-[80vh] bg-gray-900 rounded-xl shadow-2xl border border-gray-700 flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-gray-700 flex-shrink-0">
          <div className="flex items-center gap-2">
            <CalendarDays className="w-5 h-5 text-amber-400" />
            <h3 className="text-base font-semibold text-white">Scheduled Tasks</h3>
            <span className="text-xs text-gray-500">
              {jobs.length} schedule{jobs.length !== 1 ? 's' : ''}
            </span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={loadJobs}
              className="p-1.5 rounded-md text-gray-400 hover:text-gray-200 hover:bg-gray-800 transition-colors"
              title="Refresh"
            >
              <RefreshCw className={`w-4 h-4 ${isLoading ? 'animate-spin' : ''}`} />
            </button>
            <button
              onClick={onClose}
              className="text-gray-400 hover:text-gray-200 transition-colors"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-5 py-3">
          {error && (
            <div className="mb-3 px-3 py-2 rounded-lg bg-red-900/20 border border-red-800/40 text-xs text-red-400">
              {error}
            </div>
          )}

          {isLoading && jobs.length === 0 ? (
            <div className="py-8 text-center text-sm text-gray-500">Loading schedules...</div>
          ) : jobs.length === 0 ? (
            <div className="py-8 text-center">
              <CalendarDays className="w-8 h-8 text-gray-600 mx-auto mb-2" />
              <p className="text-sm text-gray-400">No scheduled tasks yet</p>
              <p className="text-xs text-gray-500 mt-1">
                Ask the agent to schedule a task, e.g. "Run this report every morning at 9am"
              </p>
            </div>
          ) : (
            <div className="space-y-2">
              {jobs.map((job) => (
                <div
                  key={job.id}
                  className={`rounded-lg border p-3 transition-colors ${
                    job.enabled
                      ? 'border-gray-700 bg-gray-800/50'
                      : 'border-gray-800 bg-gray-900/50 opacity-60'
                  }`}
                >
                  {/* Top row: name + status */}
                  <div className="flex items-center justify-between mb-1.5">
                    <span className="text-sm font-medium text-gray-200 truncate max-w-[280px]">
                      {job.name}
                    </span>
                    <div className="flex items-center gap-2">
                      {statusBadge(job)}
                      <button
                        onClick={() => handleToggle(job)}
                        disabled={actionInProgress === job.id}
                        className="text-gray-400 hover:text-gray-200 transition-colors disabled:opacity-50"
                        title={job.enabled ? 'Disable' : 'Enable'}
                      >
                        {job.enabled
                          ? <ToggleRight className="w-5 h-5 text-green-400" />
                          : <ToggleLeft className="w-5 h-5" />
                        }
                      </button>
                    </div>
                  </div>

                  {/* Cron description */}
                  <p className="text-xs text-gray-400 mb-1">
                    {describeCron(job.cron_expression)}
                    {job.timezone && <span className="text-gray-600"> ({job.timezone})</span>}
                  </p>

                  {/* Query preview */}
                  {job.query && (
                    <p className="text-xs text-gray-500 truncate mb-1.5" title={job.query}>
                      {job.query}
                    </p>
                  )}

                  {/* Meta row: next run, last run, actions */}
                  <div className="flex items-center justify-between mt-2">
                    <div className="flex items-center gap-3 text-[10px] text-gray-500">
                      {job.next_run_at && job.enabled && (
                        <span>Next: {formatRelativeTime(job.next_run_at)}</span>
                      )}
                      {job.last_run_at && (
                        <span>Last: {formatRelativeTime(job.last_run_at)}</span>
                      )}
                      {job.run_count > 0 && (
                        <span>{job.run_count} run{job.run_count !== 1 ? 's' : ''}</span>
                      )}
                    </div>
                    <div className="flex items-center gap-1">
                      {job.last_status === 'running' ? (
                        <button
                          onClick={() => handleStop(job)}
                          disabled={actionInProgress === job.id}
                          className="p-1 rounded text-gray-400 hover:text-red-400 hover:bg-gray-700 transition-colors disabled:opacity-50"
                          title="Stop"
                        >
                          <Square className="w-3.5 h-3.5" />
                        </button>
                      ) : (
                        <button
                          onClick={() => handleTrigger(job)}
                          disabled={actionInProgress === job.id}
                          className="p-1 rounded text-gray-400 hover:text-green-400 hover:bg-gray-700 transition-colors disabled:opacity-50"
                          title="Run now"
                        >
                          <Play className="w-3.5 h-3.5" />
                        </button>
                      )}
                      <button
                        onClick={() => handleDelete(job)}
                        disabled={actionInProgress === job.id}
                        className="p-1 rounded text-gray-400 hover:text-red-400 hover:bg-gray-700 transition-colors disabled:opacity-50"
                        title="Delete"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </button>
                    </div>
                  </div>

                  {/* Error message */}
                  {job.last_error && job.last_status === 'error' && (
                    <p className="text-[10px] text-red-400/80 mt-1 truncate" title={job.last_error}>
                      {job.last_error}
                    </p>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>,
    document.body
  )
}

export default MultiAgentSchedulesPopup
