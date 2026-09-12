import React from 'react'
import {
  Clock, CheckCircle, XCircle, Minus, Loader,
  AlertTriangle, Square
} from 'lucide-react'
import { WebhookEndpoint } from './WebhookEndpoint'
import { describeCron } from './cron'
import {
  formatDuration,
  formatExactDateTime,
  formatLastRunLabel,
  formatLocalScheduleTime,
  formatMissedScheduleReason,
  formatOverdueDuration,
  getLocalizedJobName,
  getMissedScheduleDelayMs,
  getScheduleExecutionScope,
  isMissedSchedule,
  isScheduleIssueStatus,
  isSchedulePartialStatus,
  isScheduleWaitingStatus,
  timeAgo,
} from './helpers'
import type { ScheduleRunsPanelState } from './useScheduleRunsData'
import { ScheduleRowActions } from './ScheduleRowActions'

type ScheduleListViewProps = {
  panel: Pick<ScheduleRunsPanelState,
    | 'filteredJobs' | 'presetMap' | 'showWorkflowIdentityInScheduleRows' | 'isReadOnlyUser'
    | 'handleStopRun' | 'handleTrigger' | 'triggering' | 'handleToggle'
    | 'openActionMenuJobId' | 'setOpenActionMenuJobId' | 'handleDelete'
  >
}

export const ScheduleListView: React.FC<ScheduleListViewProps> = ({ panel }) => {
  const {
    filteredJobs,
    presetMap,
    showWorkflowIdentityInScheduleRows,
    isReadOnlyUser,
    handleStopRun,
    handleTrigger,
    triggering,
    handleToggle,
    openActionMenuJobId,
    setOpenActionMenuJobId,
    handleDelete,
  } = panel

  return (
    <div className="divide-y divide-gray-100 dark:divide-gray-700">
      {filteredJobs.map((job, index, jobsList) => {
        const preset = presetMap.get(job.preset_query_id ?? '')
        const cronDesc = job.schedule_type === 'webhook' ? 'API trigger · on request' : describeCron(job.cron_expression)
        const localizedJobName = getLocalizedJobName(job)
        const workflowDisplayLabel = preset?.label || job.workflow_label || job.name
        const executionScope = getScheduleExecutionScope(job)
        const previousJob = index > 0 ? jobsList[index - 1] : null
        const isRunningJob = job.last_status === 'running'
        const isWaitingJob = isScheduleWaitingStatus(job.last_status)
        const isMissedJob = isMissedSchedule(job)
        const missedDelayMs = getMissedScheduleDelayMs(job)
        const missedReason = isMissedJob ? formatMissedScheduleReason(job) : ''
        const showRunningHeader = isRunningJob && (!previousJob || previousJob.last_status !== 'running')
        const previousJobWasMissed = previousJob ? isMissedSchedule(previousJob) : false
        const showMissedHeader = isMissedJob && (!previousJob || previousJob.last_status === 'running' || !previousJobWasMissed)
        const showScheduledHeader = !isRunningJob && !isMissedJob && (!previousJob || previousJob.last_status === 'running' || previousJobWasMissed)

        return (
          <React.Fragment key={job.id}>
            {showRunningHeader && (
              <div className="px-5 py-3 bg-amber-500/5 border-b border-amber-500/10">
                <div>
                  <div className="text-[11px] uppercase tracking-wide text-amber-600 dark:text-amber-400">Running schedules</div>
                  <div className="text-sm font-medium text-foreground">Schedules with an active execution right now</div>
                </div>
              </div>
            )}

            {showScheduledHeader && (
              <div className="px-5 py-3 bg-muted/30 border-b border-border">
                <div>
                  <div className="text-[11px] uppercase tracking-wide text-muted-foreground">Automation schedules</div>
                  <div className="text-sm font-medium text-foreground">Saved schedules that are idle, paused, or waiting for their next run</div>
                </div>
              </div>
            )}

            {showMissedHeader && (
              <div className="px-5 py-3 bg-amber-500/5 border-b border-amber-500/10">
                <div>
                  <div className="text-[11px] uppercase tracking-wide text-amber-600 dark:text-amber-400">Missed schedules</div>
                  <div className="text-sm font-medium text-foreground">Schedules that were due, but never started at the scheduled time</div>
                </div>
              </div>
            )}

            <div className={`px-5 py-4 ${!job.enabled ? 'opacity-60' : ''}`}>
            {/* Row top */}
            <div className="relative flex items-start gap-3">
              {/* Status dot */}
              <div className={`mt-1 w-2 h-2 rounded-full flex-shrink-0 ${
                job.last_status === 'running' ? 'bg-amber-500 animate-pulse' :
                isWaitingJob ? 'bg-sky-500 animate-pulse' :
                isMissedJob ? 'bg-amber-500' :
                job.enabled ? 'bg-green-500' : 'bg-gray-300 dark:bg-gray-600'
              }`} />

              {/* Main content */}
              <div className="flex-1 min-w-0">
                {showWorkflowIdentityInScheduleRows && (
                  <div className="min-w-0 pr-28">
                    <div className="text-[11px] uppercase tracking-wide text-gray-400 dark:text-gray-500">
                      Automation
                    </div>
                    <div className="mt-0.5 flex items-center gap-2 flex-wrap">
                      <span className="text-sm font-semibold text-gray-900 dark:text-gray-100 truncate" title={workflowDisplayLabel}>
                        {workflowDisplayLabel}
                      </span>
                    </div>
                  </div>
                )}

                <div className={`${showWorkflowIdentityInScheduleRows ? 'mt-1' : ''} flex items-center gap-2 flex-wrap pr-28`}>
                  {showWorkflowIdentityInScheduleRows && (
                    <span className="text-[11px] uppercase tracking-wide text-gray-400 dark:text-gray-500">
                      {job.schedule_type === 'webhook' ? 'Webhook' : 'Schedule'}
                    </span>
                  )}
                  <span className={`${showWorkflowIdentityInScheduleRows ? 'text-xs font-medium' : 'text-sm font-semibold'} text-gray-700 dark:text-gray-300 truncate`} title={job.name}>
                    {localizedJobName}
                  </span>
                  {isMissedJob && (
                    <span className="text-xs px-1.5 py-0.5 rounded-full bg-amber-100 dark:bg-amber-900/30 text-amber-700 dark:text-amber-300">
                      Missed
                    </span>
                  )}
                  {isWaitingJob && (
                    <span className="text-xs px-1.5 py-0.5 rounded-full bg-sky-100 dark:bg-sky-900/30 text-sky-700 dark:text-sky-300">
                      {job.last_status === 'waiting_for_capacity'
                        ? 'Waiting for capacity'
                        : `Queued${job.queued_occurrences && job.queued_occurrences > 1 ? ` · ${job.queued_occurrences} combined` : ''}`}
                    </span>
                  )}
                  {!job.enabled && (
                    <span className="text-xs px-1.5 py-0.5 rounded-full bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400">
                      Paused
                    </span>
                  )}
                </div>

                {/* Cron + groups */}
                <div className="mt-0.5 flex flex-wrap gap-x-3 gap-y-0.5 pr-28 text-xs text-gray-500 dark:text-gray-400">
                  <span className="flex items-center gap-1">
                    <Clock className="w-3 h-3" />
                    {cronDesc}
                  </span>
                  {job.group_names && job.group_names.length > 0 && (
                    <span>Groups: {job.group_names.join(', ')}</span>
                  )}
                  {executionScope && (
                    <span className="px-1.5 py-0.5 rounded-full bg-purple-100 dark:bg-purple-900/30 text-purple-600 dark:text-purple-400 font-medium" title={executionScope.title}>
                      {executionScope.label}
                    </span>
                  )}
                </div>
                {job.mode === 'workshop' && job.messages && job.messages.length > 0 && (
                  <div className="mt-1 pr-28">
                    <div className="space-y-0.5">
                      {job.messages.map((m, i) => (
                        <div key={i} className="flex items-start gap-1 text-xs text-gray-500 dark:text-gray-400">
                          <span className="shrink-0 text-gray-400 dark:text-gray-500">{i + 1}.</span>
                          <span>{m}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {job.schedule_type === 'webhook' && <WebhookEndpoint id={job.id} name={job.name} />}

                {/* Run stats */}
                <div className="mt-1.5 flex flex-wrap gap-x-4 gap-y-0.5 text-xs text-gray-500 dark:text-gray-400">
                  <span className="flex items-center gap-1" title={formatExactDateTime(job.last_run_at)}>
                    {job.last_status === 'running' ? (
                      <Loader className="w-3 h-3 text-amber-500 animate-spin" />
                    ) : isWaitingJob ? (
                      <Clock className="w-3 h-3 text-sky-500" />
                    ) : job.last_status === 'success' ? (
                      <CheckCircle className="w-3 h-3 text-green-500" />
                    ) : job.last_status === 'error' ? (
                      <XCircle className="w-3 h-3 text-red-500" />
                    ) : isSchedulePartialStatus(job.last_status) ? (
                      <AlertTriangle className="w-3 h-3 text-amber-500" />
                    ) : job.last_status === 'stopped' ? (
                      <Square className="w-3 h-3 text-gray-500" />
                    ) : (
                      <Minus className="w-3 h-3" />
                    )}
                    {isWaitingJob
                      ? job.waiting_until
                        ? `Waiting until ${formatLocalScheduleTime(job.waiting_until)}`
                        : 'Waiting to start'
                      : job.last_status === 'running'
                      ? job.last_run_at
                        ? `Running since ${formatLocalScheduleTime(job.last_run_at)} (${timeAgo(job.last_run_at)})`
                        : 'Running...'
                      : `Last ran: ${formatLastRunLabel(job.last_run_at)}`}
                  </span>
                  <span>
                    {job.enabled
                      ? isMissedJob && missedDelayMs != null
                        ? job.missed_run_count && job.missed_run_count > 1
                          ? `${job.missed_run_count} missed · latest ${formatOverdueDuration(missedDelayMs)} ago`
                          : `Missed by ${formatOverdueDuration(missedDelayMs)}`
                        : `Next: ${formatLocalScheduleTime(job.next_run_at)}`
                      : 'paused'}
                  </span>
                  {isMissedJob && (
                    <span className="text-amber-700 dark:text-amber-300" title={missedReason}>
                      {missedReason}
                    </span>
                  )}
                  <span>{job.run_count} run{job.run_count !== 1 ? 's' : ''}</span>
                  {job.last_duration_ms != null && job.last_status !== 'running' && (
                    <span>Duration: {formatDuration(job.last_duration_ms)}</span>
                  )}
                </div>

                {/* Error message */}
                {(isScheduleIssueStatus(job.last_status) || job.last_status === 'stopped') && job.last_error && (
                  <div className={`mt-1 text-xs truncate max-w-lg ${job.last_status === 'error' ? 'text-red-500' : isSchedulePartialStatus(job.last_status) ? 'text-amber-600 dark:text-amber-400' : 'text-gray-500'}`} title={job.last_error}>
                    ✗ {job.last_error}
                  </div>
                )}
                {isWaitingJob && job.waiting_reason && (
                  <div className="mt-1 text-xs truncate max-w-lg text-sky-700 dark:text-sky-300" title={job.waiting_reason}>
                    {job.waiting_reason}
                  </div>
                )}
              </div>

              {/* Keep the immediate operational action visible; secondary actions live in one menu. */}
              <div className="absolute right-0 top-0 flex items-center gap-1">
                <ScheduleRowActions
                  job={job}
                  isRunning={job.last_status === 'running'}
                  isMissedJob={isMissedJob}
                  isReadOnlyUser={isReadOnlyUser}
                  triggering={triggering}
                  openActionMenuJobId={openActionMenuJobId}
                  setOpenActionMenuJobId={setOpenActionMenuJobId}
                  handleStopRun={handleStopRun}
                  handleTrigger={handleTrigger}
                  handleToggle={handleToggle}
                  handleDelete={handleDelete}
                  menuButtonClassName="rounded-md p-1.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-gray-700 dark:hover:text-gray-200"
                />
              </div>
            </div>
            </div>
          </React.Fragment>
        )
      })}
    </div>
  )
}
