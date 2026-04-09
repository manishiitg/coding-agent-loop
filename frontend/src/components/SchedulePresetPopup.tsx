import React, { useState, useEffect } from 'react'
import ReactDOM from 'react-dom'
import cronstrue from 'cronstrue'
import { X, Clock, Play, Trash2, Save, ChevronDown, ChevronUp, ExternalLink, AlertTriangle } from 'lucide-react'
import { schedulerApi } from '../api/scheduler'
import { agentApi } from '../services/api'
import type { ScheduledJob, CreateScheduledJobRequest, VariableGroup, SchedulerConfig } from '../services/api-types'

const LOCAL_TIMEZONE = Intl.DateTimeFormat().resolvedOptions().timeZone

function describeCron(expr: string): string {
  try {
    return cronstrue.toString(expr, { throwExceptionOnParseError: true })
  } catch {
    return ''
  }
}

interface SchedulePresetPopupProps {
  presetQueryId: string | null
  presetLabel: string
  entityType?: 'workflow' | 'multi-agent'
  workspacePath?: string   // required to load variable groups
  onClose: () => void
}

const QUICK_PICKS = [
  { label: 'Hourly', value: '0 * * * *' },
  { label: 'Daily 2am', value: '0 2 * * *' },
  { label: 'Daily 9am', value: '0 9 * * *' },
  { label: 'Weekly Mon', value: '0 9 * * 1' },
  { label: 'Monthly', value: '0 9 1 * *' },
]

const SchedulePresetPopup: React.FC<SchedulePresetPopupProps> = ({
  presetQueryId,
  presetLabel,
  entityType = 'multi-agent',
  workspacePath,
  onClose,
}) => {
  const [existingJob, setExistingJob] = useState<ScheduledJob | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [isSaving, setIsSaving] = useState(false)
  const [isTriggering, setIsTriggering] = useState(false)
  const [cronExpr, setCronExpr] = useState('0 9 * * 1')
  const [jobName, setJobName] = useState('')
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [successMsg, setSuccessMsg] = useState<string | null>(null)
  const [schedulerConfig, setSchedulerConfig] = useState<SchedulerConfig | null>(null)

  // Group selection — at least one group must be selected
  const [availableGroups, setAvailableGroups] = useState<VariableGroup[]>([])
  const [selectedGroupIds, setSelectedGroupIds] = useState<string[]>([])

  const cronDescription = describeCron(cronExpr)
  const isValidCron = cronDescription !== ''
  const schedulerEntityType = entityType === 'workflow' ? 'workflow' : 'chat'
  const isSchedulerExecutionEnabled = schedulerConfig?.execution_enabled !== false

  const allGroupsSelected = availableGroups.length > 0 && selectedGroupIds.length === availableGroups.length
  const hasGroupSelection = availableGroups.length === 0 || selectedGroupIds.length > 0

  // Load existing schedule and variable groups
  useEffect(() => {
    if (!presetQueryId) {
      setIsLoading(false)
      return
    }
    setIsLoading(true)

    const loadJobs = schedulerApi.listJobs({ entity_type: schedulerEntityType })
      .then((resp) => {
        const job = resp.jobs.find((j) => j.preset_query_id === presetQueryId) ?? null
        setExistingJob(job)
        if (job) {
          setCronExpr(job.cron_expression)
          setJobName(job.name)
          setSelectedGroupIds(job.group_names?.length ? job.group_names : [])
        } else {
          setJobName(presetLabel ? `${presetLabel} (scheduled)` : 'Scheduled job')
        }
      })

    const loadGroups = workspacePath
      ? agentApi.getVariableGroups(workspacePath)
          .then((resp) => {
            if (resp.manifest?.groups?.length) {
              setAvailableGroups(resp.manifest.groups)
              // Default: select all groups if no prior selection
              setSelectedGroupIds(prev =>
                prev.length === 0 ? resp.manifest!.groups!.map((g: VariableGroup) => g.name) : prev
              )
            }
          })
          .catch(() => {})
      : Promise.resolve()

    const loadSchedulerConfig = schedulerApi.getConfig()
      .then((config) => setSchedulerConfig(config))
      .catch(() => setSchedulerConfig(null))

    Promise.all([loadJobs, loadGroups, loadSchedulerConfig])
      .catch(() => {})
      .finally(() => setIsLoading(false))
  }, [presetQueryId, schedulerEntityType, presetLabel, workspacePath])

  const toggleGroup = (groupId: string) => {
    setSelectedGroupIds((prev) => {
      if (prev.includes(groupId)) {
        return prev.filter(id => id !== groupId)
      } else {
        return [...prev, groupId]
      }
    })
  }

  const handleSave = async () => {
    if (!presetQueryId || !isValidCron || !hasGroupSelection) return
    setIsSaving(true)
    setError(null)
    const groupIds = selectedGroupIds
    try {
      if (existingJob) {
        const updated = await schedulerApi.updateJob(existingJob.id, {
          cron_expression: cronExpr,
          timezone: LOCAL_TIMEZONE,
          name: jobName,
          enabled: true,
          group_names: groupIds,
          set_group_names: true,
        })
        setExistingJob(updated)
      } else {
        const req: CreateScheduledJobRequest = {
          name: jobName,
          entity_type: schedulerEntityType,
          preset_query_id: presetQueryId,
          cron_expression: cronExpr,
          timezone: LOCAL_TIMEZONE,
          enabled: true,
          group_names: groupIds,
        }
        const created = await schedulerApi.createJob(req)
        setExistingJob(created)
      }
      setSuccessMsg('Schedule saved')
      setTimeout(() => setSuccessMsg(null), 3000)
    } catch (err) {
      setError(String(err))
    } finally {
      setIsSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!existingJob) return
    if (!window.confirm('Remove this schedule?')) return
    try {
      await schedulerApi.deleteJob(existingJob.id)
      setExistingJob(null)
      setSuccessMsg('Schedule removed')
      setTimeout(() => setSuccessMsg(null), 3000)
    } catch (err) {
      setError(String(err))
    }
  }

  const handleToggle = async () => {
    if (!existingJob) return
    try {
      const updated = existingJob.enabled
        ? await schedulerApi.disableJob(existingJob.id)
        : await schedulerApi.enableJob(existingJob.id)
      setExistingJob(updated)
    } catch (err) {
      setError(String(err))
    }
  }

  const handleTrigger = async () => {
    if (!existingJob) return
    setIsTriggering(true)
    try {
      await schedulerApi.triggerJob(existingJob.id)
      setSuccessMsg('Job triggered — check chat history for the run')
      setTimeout(() => setSuccessMsg(null), 5000)
    } catch (err) {
      setError(String(err))
    } finally {
      setIsTriggering(false)
    }
  }

  return ReactDOM.createPortal(
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/40"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="w-96 bg-white dark:bg-gray-800 rounded-xl shadow-xl border border-gray-200 dark:border-gray-700 flex flex-col max-h-[85vh]">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
          <div className="flex items-center gap-2">
            <Clock className="w-4 h-4 text-amber-500" />
            <span className="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate max-w-[220px]">
              {presetLabel ? `Schedule: ${presetLabel}` : 'Schedule Preset'}
            </span>
          </div>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-4 py-3 space-y-4">
          {!presetQueryId ? (
            <p className="text-sm text-gray-500 dark:text-gray-400">
              Select a preset first to schedule it.
            </p>
          ) : isLoading ? (
            <p className="text-sm text-gray-500 dark:text-gray-400">Loading...</p>
          ) : (
            <>
              {!isSchedulerExecutionEnabled && (
                <div className="rounded-lg border border-red-200 bg-red-50 px-3 py-2 dark:border-red-900/40 dark:bg-red-950/30">
                  <div className="flex items-start gap-2">
                    <AlertTriangle className="mt-0.5 h-4 w-4 flex-shrink-0 text-red-500" />
                    <div>
                      <div className="text-xs font-semibold text-red-700 dark:text-red-300">
                        Automatic schedules are disabled on this server
                      </div>
                      <p className="mt-1 text-xs text-red-600 dark:text-red-200/90">
                        {schedulerConfig?.disabled_reason || 'Timed cron executions are currently turned off. You can still save this schedule and run it manually.'}
                      </p>
                    </div>
                  </div>
                </div>
              )}

              {/* Status badge */}
              {existingJob && (
                <div className="flex items-center justify-between">
                  <span className="text-xs text-gray-500 dark:text-gray-400">
                    {existingJob.run_count > 0
                      ? `${existingJob.run_count} run${existingJob.run_count !== 1 ? 's' : ''}`
                      : 'Never run'}
                  </span>
                  <button
                    onClick={handleToggle}
                    className={`text-xs font-medium px-2 py-0.5 rounded-full ${
                      existingJob.enabled
                        ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'
                        : 'bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-400'
                    }`}
                  >
                    {existingJob.enabled ? '● Active' : '○ Paused'}
                  </button>
                </div>
              )}

              {/* Name */}
              <div>
                <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Name
                </label>
                <input
                  type="text"
                  value={jobName}
                  onChange={(e) => setJobName(e.target.value)}
                  className="w-full text-sm px-2 py-1.5 border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100"
                />
              </div>

              {/* Groups selector (workflow only, when groups exist) */}
              {entityType === 'workflow' && availableGroups.length > 0 && (
                <div>
                  <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1.5">
                    Groups to run
                  </label>
                  <div className="space-y-1.5">
                    {/* Select all / deselect all toggle */}
                    <label className="flex items-center gap-2 cursor-pointer">
                      <input
                        type="checkbox"
                        checked={allGroupsSelected}
                        onChange={() => setSelectedGroupIds(
                          allGroupsSelected ? [] : availableGroups.map(g => g.name)
                        )}
                        className="rounded border-gray-300 text-amber-500 focus:ring-amber-400"
                      />
                      <span className="text-xs font-medium text-gray-700 dark:text-gray-300">
                        All groups ({availableGroups.length})
                      </span>
                    </label>
                    {/* Individual groups */}
                    {availableGroups.map((g) => (
                      <label key={g.name} className="flex items-center gap-2 cursor-pointer pl-4">
                        <input
                          type="checkbox"
                          checked={selectedGroupIds.includes(g.name)}
                          onChange={() => toggleGroup(g.name)}
                          className="rounded border-gray-300 text-amber-500 focus:ring-amber-400"
                        />
                        <span className="text-xs text-gray-600 dark:text-gray-400">
                          {g.name}
                        </span>
                      </label>
                    ))}
                    {/* Validation message */}
                    {selectedGroupIds.length === 0 && (
                      <p className="text-xs text-red-500 mt-1">Select at least one group to schedule</p>
                    )}
                  </div>
                </div>
              )}

              {/* Cron expression */}
              <div>
                <div className="flex items-center justify-between mb-1">
                  <label className="text-xs font-medium text-gray-700 dark:text-gray-300">
                    Schedule (cron)
                  </label>
                  <a
                    href="https://crontab.guru/"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center gap-0.5 text-xs text-amber-500 hover:text-amber-600 dark:text-amber-400"
                  >
                    crontab.guru
                    <ExternalLink className="w-3 h-3" />
                  </a>
                </div>
                <input
                  type="text"
                  value={cronExpr}
                  onChange={(e) => setCronExpr(e.target.value)}
                  placeholder="0 2 * * *"
                  className={`w-full text-sm font-mono px-2 py-1.5 border rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 ${
                    isValidCron
                      ? 'border-gray-300 dark:border-gray-600'
                      : 'border-red-400 dark:border-red-500'
                  }`}
                />
                {cronExpr && (
                  <p className={`mt-1 text-xs ${isValidCron ? 'text-gray-500 dark:text-gray-400' : 'text-red-500'}`}>
                    {isValidCron ? `📅 ${cronDescription}` : 'Invalid cron expression'}
                  </p>
                )}
              </div>

              {/* Quick picks */}
              <div>
                <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                  Quick picks
                </label>
                <div className="flex flex-wrap gap-1">
                  {QUICK_PICKS.map((p) => (
                    <button
                      key={p.value}
                      onClick={() => setCronExpr(p.value)}
                      className={`text-xs px-2 py-0.5 rounded border transition-colors ${
                        cronExpr === p.value
                          ? 'bg-amber-100 border-amber-400 text-amber-700 dark:bg-amber-900/30 dark:border-amber-500 dark:text-amber-300'
                          : 'border-gray-300 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-50 dark:hover:bg-gray-700'
                      }`}
                    >
                      {p.label}
                    </button>
                  ))}
                </div>
              </div>

              {/* Timezone info */}
              <p className="text-xs text-gray-400 dark:text-gray-500">
                Timezone: {LOCAL_TIMEZONE}
              </p>

              {/* Advanced: next run, last run */}
              {existingJob && (
                <div>
                  <button
                    onClick={() => setShowAdvanced(!showAdvanced)}
                    className="flex items-center gap-1 text-xs text-gray-500 dark:text-gray-400 hover:text-gray-700 dark:hover:text-gray-200"
                  >
                    {showAdvanced ? <ChevronUp className="w-3 h-3" /> : <ChevronDown className="w-3 h-3" />}
                    Details
                  </button>
                  {showAdvanced && (
                    <div className="mt-2 space-y-1 text-xs text-gray-500 dark:text-gray-400">
                      {existingJob.last_run_at && (
                        <div>Last run: {new Date(existingJob.last_run_at).toLocaleString()}</div>
                      )}
                      {existingJob.next_run_at && (
                        <div>Next run: {new Date(existingJob.next_run_at).toLocaleString()}</div>
                      )}
                      {existingJob.last_status && (
                        <div className={existingJob.last_status === 'error' ? 'text-red-500' : 'text-green-600 dark:text-green-400'}>
                          Last status: {existingJob.last_status}
                          {existingJob.last_error && ` — ${existingJob.last_error}`}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )}

              {error && (
                <p className="text-xs text-red-500">{error}</p>
              )}
              {successMsg && (
                <p className="text-xs text-green-600 dark:text-green-400">{successMsg}</p>
              )}
            </>
          )}
        </div>

        {/* Footer actions */}
        {presetQueryId && !isLoading && (
          <div className="flex items-center gap-2 px-4 py-3 border-t border-gray-200 dark:border-gray-700 flex-shrink-0">
            {existingJob && (
              <>
                <button
                  onClick={handleTrigger}
                  disabled={isTriggering}
                  title="Run now"
                  className="p-1.5 rounded-md text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-green-600 transition-colors disabled:opacity-50"
                >
                  <Play className="w-4 h-4" />
                </button>
                <button
                  onClick={handleDelete}
                  title="Remove schedule"
                  className="p-1.5 rounded-md text-gray-500 hover:bg-gray-100 dark:hover:bg-gray-700 hover:text-red-500 transition-colors"
                >
                  <Trash2 className="w-4 h-4" />
                </button>
              </>
            )}
            <div className="flex-1" />
            <button
              onClick={handleSave}
              disabled={isSaving || !isValidCron || !jobName.trim() || !hasGroupSelection}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-amber-500 hover:bg-amber-600 text-white rounded-md disabled:opacity-50 transition-colors"
            >
              <Save className="w-3 h-3" />
              {existingJob ? 'Update' : 'Save Schedule'}
            </button>
          </div>
        )}
      </div>
    </div>,
    document.body
  )
}

export default SchedulePresetPopup
