import React, { useState, useEffect, useMemo } from 'react'
import cronstrue from 'cronstrue'
import { X, Clock, Play, Trash2, Save, ChevronDown, ChevronUp, ExternalLink, AlertTriangle, CalendarDays, Plus, Minus } from 'lucide-react'
import { schedulerApi } from '../api/scheduler'
import { agentApi } from '../services/api'
import type { CalendarScheduleItem, ScheduledJob, CreateScheduledJobRequest, VariableGroup, SchedulerConfig } from '../services/api-types'
import ModalPortal from './ui/ModalPortal'

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

type ScheduleType = 'cron' | 'calendar'

function defaultCalendarItem(): CalendarScheduleItem {
  const d = new Date()
  d.setDate(d.getDate() + 1)
  return {
    date: d.toISOString().slice(0, 10),
    time: '09:00',
    description: '',
  }
}

function monthLabel(dateKey: string): string {
  const d = dateKey ? new Date(`${dateKey}T00:00:00`) : new Date()
  return d.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
}

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
  const [scheduleType, setScheduleType] = useState<ScheduleType>('cron')
  const [calendarItems, setCalendarItems] = useState<CalendarScheduleItem[]>([defaultCalendarItem()])
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
  const hasValidCalendarItems = calendarItems.length > 0 && calendarItems.every((item) => item.date && item.time)
  const canSaveSchedule = jobName.trim() && hasGroupSelection && (
    scheduleType === 'cron' ? isValidCron : hasValidCalendarItems
  )
  const previewMonthKey = calendarItems.find((item) => item.date)?.date || new Date().toISOString().slice(0, 10)
  const calendarPreview = useMemo(() => {
    const base = new Date(`${previewMonthKey}T00:00:00`)
    const year = base.getFullYear()
    const month = base.getMonth()
    const first = new Date(year, month, 1)
    const daysInMonth = new Date(year, month + 1, 0).getDate()
    const offset = first.getDay()
    const byDate = calendarItems.reduce<Record<string, CalendarScheduleItem[]>>((acc, item) => {
      if (!item.date) return acc
      acc[item.date] = [...(acc[item.date] || []), item].sort((a, b) => (a.time || '').localeCompare(b.time || ''))
      return acc
    }, {})
    const cells: Array<{ key: string; day?: number; date?: string; items: CalendarScheduleItem[] }> = []
    for (let i = 0; i < offset; i += 1) cells.push({ key: `empty-${i}`, items: [] })
    for (let day = 1; day <= daysInMonth; day += 1) {
      const date = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`
      cells.push({ key: date, day, date, items: byDate[date] || [] })
    }
    return cells
  }, [calendarItems, previewMonthKey])

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
          const loadedType = job.schedule_type === 'calendar' ? 'calendar' : 'cron'
          setScheduleType(loadedType)
          setCronExpr(job.cron_expression || '0 9 * * 1')
          setCalendarItems(job.calendar_items?.length ? job.calendar_items : [defaultCalendarItem()])
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

  const updateCalendarItem = (index: number, patch: Partial<CalendarScheduleItem>) => {
    setCalendarItems((items) => items.map((item, i) => i === index ? { ...item, ...patch } : item))
  }

  const addCalendarItem = () => {
    setCalendarItems((items) => [...items, defaultCalendarItem()])
  }

  const removeCalendarItem = (index: number) => {
    setCalendarItems((items) => items.length <= 1 ? items : items.filter((_, i) => i !== index))
  }

  const handleSave = async () => {
    if (!presetQueryId || !canSaveSchedule) return
    setIsSaving(true)
    setError(null)
    const groupIds = selectedGroupIds
    try {
      if (existingJob) {
        const updated = await schedulerApi.updateJob(existingJob.id, {
          schedule_type: scheduleType,
          cron_expression: scheduleType === 'cron' ? cronExpr : undefined,
          calendar_items: scheduleType === 'calendar' ? calendarItems : undefined,
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
          workspace_path: workspacePath,
          schedule_type: scheduleType,
          cron_expression: scheduleType === 'cron' ? cronExpr : undefined,
          calendar_items: scheduleType === 'calendar' ? calendarItems : undefined,
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

  return (
    <ModalPortal>
    <div
      className="fixed inset-0 z-[9999] flex items-center justify-center bg-black/40 p-2 sm:p-4"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="w-full max-w-96 bg-white dark:bg-gray-800 rounded-xl shadow-xl border border-gray-200 dark:border-gray-700 flex flex-col max-h-[calc(100dvh-1rem)] sm:max-h-[85vh]">
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

              {/* Schedule type */}
              {entityType === 'workflow' && (
                <div className="grid grid-cols-2 gap-1 rounded-lg bg-gray-100 p-1 dark:bg-gray-700/70">
                  <button
                    type="button"
                    onClick={() => setScheduleType('cron')}
                    className={`flex items-center justify-center gap-1 rounded-md px-2 py-1.5 text-xs font-medium transition-colors ${
                      scheduleType === 'cron'
                        ? 'bg-white text-gray-900 shadow-sm dark:bg-gray-800 dark:text-gray-100'
                        : 'text-gray-500 hover:text-gray-700 dark:text-gray-300 dark:hover:text-gray-100'
                    }`}
                  >
                    <Clock className="h-3.5 w-3.5" />
                    Repeating
                  </button>
                  <button
                    type="button"
                    onClick={() => setScheduleType('calendar')}
                    className={`flex items-center justify-center gap-1 rounded-md px-2 py-1.5 text-xs font-medium transition-colors ${
                      scheduleType === 'calendar'
                        ? 'bg-white text-gray-900 shadow-sm dark:bg-gray-800 dark:text-gray-100'
                        : 'text-gray-500 hover:text-gray-700 dark:text-gray-300 dark:hover:text-gray-100'
                    }`}
                  >
                    <CalendarDays className="h-3.5 w-3.5" />
                    Calendar
                  </button>
                </div>
              )}

              {scheduleType === 'cron' ? (
                <>
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
                        {isValidCron ? `Calendar: ${cronDescription}` : 'Invalid cron expression'}
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
                          type="button"
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
                </>
              ) : (
                <div>
                  <div className="mb-1.5 flex items-center justify-between">
                    <label className="text-xs font-medium text-gray-700 dark:text-gray-300">
                      Calendar items
                    </label>
                    <button
                      type="button"
                      onClick={addCalendarItem}
                      className="flex items-center gap-1 rounded border border-gray-300 px-2 py-0.5 text-xs text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700"
                    >
                      <Plus className="h-3 w-3" />
                      Add
                    </button>
                  </div>
                  <div className="space-y-2">
                    {calendarItems.map((item, index) => (
                      <div key={item.id || index} className="rounded-lg border border-gray-200 p-2 dark:border-gray-700">
                        <div className="grid grid-cols-[1fr_88px_28px] gap-1.5">
                          <input
                            type="date"
                            value={item.date}
                            onChange={(e) => updateCalendarItem(index, { date: e.target.value })}
                            className="min-w-0 rounded-md border border-gray-300 bg-white px-2 py-1.5 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
                          />
                          <input
                            type="time"
                            value={item.time}
                            onChange={(e) => updateCalendarItem(index, { time: e.target.value })}
                            className="min-w-0 rounded-md border border-gray-300 bg-white px-2 py-1.5 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
                          />
                          <button
                            type="button"
                            onClick={() => removeCalendarItem(index)}
                            disabled={calendarItems.length <= 1}
                            className="flex items-center justify-center rounded-md text-gray-400 hover:bg-gray-100 hover:text-red-500 disabled:opacity-40 dark:hover:bg-gray-700"
                            title="Remove item"
                          >
                            <Minus className="h-3.5 w-3.5" />
                          </button>
                        </div>
                        <input
                          type="text"
                          value={item.description || ''}
                          onChange={(e) => updateCalendarItem(index, { description: e.target.value })}
                          placeholder="Optional note"
                          className="mt-1.5 w-full rounded-md border border-gray-300 bg-white px-2 py-1.5 text-xs text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
                        />
                      </div>
                    ))}
                  </div>
                  {!hasValidCalendarItems && (
                    <p className="mt-1 text-xs text-red-500">Add at least one item with date and time.</p>
                  )}

                  <div className="rounded-lg border border-gray-200 p-2 dark:border-gray-700">
                    <div className="mb-2 flex items-center justify-between">
                      <span className="text-xs font-medium text-gray-700 dark:text-gray-300">
                        {monthLabel(previewMonthKey)}
                      </span>
                      <span className="text-xs text-gray-400 dark:text-gray-500">
                        {calendarItems.length} item{calendarItems.length === 1 ? '' : 's'}
                      </span>
                    </div>
                    <div className="grid grid-cols-7 gap-1 text-center text-[10px] font-medium text-gray-400 dark:text-gray-500">
                      {['S', 'M', 'T', 'W', 'T', 'F', 'S'].map((day, index) => (
                        <div key={`${day}-${index}`}>{day}</div>
                      ))}
                    </div>
                    <div className="mt-1 grid grid-cols-7 gap-1">
                      {calendarPreview.map((cell) => (
                        <div
                          key={cell.key}
                          className={`min-h-[46px] rounded-md border p-1 text-left ${
                            cell.day
                              ? cell.items.length
                                ? 'border-amber-300 bg-amber-50 dark:border-amber-700 dark:bg-amber-950/30'
                                : 'border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800'
                              : 'border-transparent'
                          }`}
                        >
                          {cell.day && (
                            <>
                              <div className="text-[10px] font-medium text-gray-500 dark:text-gray-400">
                                {cell.day}
                              </div>
                              <div className="mt-0.5 space-y-0.5">
                                {cell.items.slice(0, 2).map((item, itemIndex) => (
                                  <div
                                    key={`${cell.date}-${item.time}-${itemIndex}`}
                                    className="truncate rounded bg-amber-200/80 px-1 py-0.5 text-[9px] leading-none text-amber-900 dark:bg-amber-800/70 dark:text-amber-100"
                                    title={`${item.time}${item.description ? ` - ${item.description}` : ''}`}
                                  >
                                    {item.time}
                                  </div>
                                ))}
                                {cell.items.length > 2 && (
                                  <div className="text-[9px] leading-none text-amber-700 dark:text-amber-300">
                                    +{cell.items.length - 2}
                                  </div>
                                )}
                              </div>
                            </>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              )}

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
              disabled={isSaving || !canSaveSchedule}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium bg-amber-500 hover:bg-amber-600 text-white rounded-md disabled:opacity-50 transition-colors"
            >
              <Save className="w-3 h-3" />
              {existingJob ? 'Update' : 'Save Schedule'}
            </button>
          </div>
        )}
      </div>
    </div>
    </ModalPortal>
  )
}

export default SchedulePresetPopup
