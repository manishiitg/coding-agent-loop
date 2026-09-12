import React, { useEffect, useState } from 'react'
import { ChevronDown, ChevronRight, Square } from 'lucide-react'
import { WebhookEndpoint } from './WebhookEndpoint'
import { describeCron } from './cron'
import { formatExactDateTime, formatLastRunLabel, formatLocalScheduleTime, getLocalizedJobName, getScheduleExecutionScope, isMissedSchedule, isScheduleWaitingStatus } from './helpers'
import { ScheduleRowActions } from './ScheduleRowActions'
import type { ScheduleRunsPanelState } from './useScheduleRunsData'

type ScheduleTableViewProps = {
  panel: Pick<ScheduleRunsPanelState,
    | 'focusedScheduleId' | 'filteredJobs' | 'presetMap' | 'isSchedulerPaused' | 'isReadOnlyUser' | 'triggering'
    | 'handleStopRun' | 'handleTrigger' | 'handleToggle' | 'handleDelete'
    | 'openActionMenuJobId' | 'setOpenActionMenuJobId'
  >
}

/** Global schedules show timing first; instructions and run diagnostics are opt-in. */
export function ScheduleTableView({ panel }: ScheduleTableViewProps) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  useEffect(() => {
    if (panel.focusedScheduleId) {
      setExpanded(previous => new Set(previous).add(panel.focusedScheduleId!))
    }
  }, [panel.focusedScheduleId])
  return <div className="px-4 py-3 sm:px-6">
    <div className="overflow-x-auto rounded-lg border border-border">
      <table className="w-full min-w-[720px] text-left text-sm">
        <thead className="border-b border-border bg-muted/30 text-xs text-muted-foreground">
          <tr>
            <th className="px-4 py-2 font-medium">Schedule / automation</th>
            <th className="px-3 py-2 font-medium">Frequency</th>
            <th className="px-3 py-2 font-medium">State</th>
            <th className="px-3 py-2 font-medium">Next run</th>
            <th className="px-3 py-2 font-medium">Last run</th>
            <th className="px-4 py-2 text-right font-medium">Details</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {panel.filteredJobs.map(job => {
            const name = getLocalizedJobName(job)
            const isWebhook = job.schedule_type === 'webhook'
            const frequency = isWebhook ? 'Webhook · on request' : describeCron(job.cron_expression)
            const workflow = panel.presetMap.get(job.preset_query_id ?? '')?.label || job.workflow_label || job.name
            const isRunning = job.last_status === 'running'
            const state = isRunning ? 'Running' : isScheduleWaitingStatus(job.last_status) ? 'Queued' : job.enabled ? 'Enabled' : 'Paused'
            const open = expanded.has(job.id)
            const detailsId = `schedule-details-${job.id}`
            const toggle = () => setExpanded(previous => {
              const next = new Set(previous)
              if (next.has(job.id)) next.delete(job.id)
              else next.add(job.id)
              return next
            })
            const scope = getScheduleExecutionScope(job)
            return <React.Fragment key={job.id}>
              <tr className="transition-colors hover:bg-muted/20">
                <td className="max-w-[340px] px-4 py-3">
                  <button type="button" onClick={toggle} aria-expanded={open} aria-controls={open ? detailsId : undefined} className="block max-w-full truncate text-left font-medium text-foreground hover:text-primary" title={name}>{name}</button>
                  <div className="mt-0.5 truncate text-xs text-muted-foreground" title={workflow}>{workflow}</div>
                </td>
                <td className="max-w-56 px-3 py-3 text-xs text-muted-foreground"><span className="line-clamp-2" title={frequency}>{frequency}</span></td>
                <td className="whitespace-nowrap px-3 py-3 text-xs text-muted-foreground"><span className="inline-flex items-center gap-1.5"><span className={`h-1.5 w-1.5 rounded-full ${isRunning ? 'animate-pulse bg-primary' : job.enabled ? 'bg-emerald-500' : 'bg-muted-foreground/50'}`} />{state}</span></td>
                <td className="whitespace-nowrap px-3 py-3 text-xs">{!job.enabled ? '—' : panel.isSchedulerPaused ? <span className="text-muted-foreground">Paused globally</span> : isWebhook ? 'On request' : formatLocalScheduleTime(job.next_run_at)}</td>
                <td className="whitespace-nowrap px-3 py-3 text-xs text-muted-foreground" title={formatExactDateTime(job.last_run_at)}>{formatLastRunLabel(job.last_run_at)}</td>
                <td className="px-4 py-3"><div className="flex items-center justify-end gap-2">
                  {isRunning && !panel.isReadOnlyUser && <button type="button" onClick={() => panel.handleStopRun(job)} className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs text-red-500 hover:bg-red-500/10"><Square className="h-3 w-3" />Stop</button>}
                  <button type="button" aria-label={`${open ? 'Hide' : 'Show'} ${name} details`} aria-expanded={open} aria-controls={open ? detailsId : undefined} onClick={toggle} className="rounded-md p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground">{open ? <ChevronDown className="h-4 w-4" /> : <ChevronRight className="h-4 w-4" />}</button>
                </div></td>
              </tr>
              {open && <tr><td colSpan={6} className="bg-muted/15 px-4 py-4">
                <div id={detailsId} role="region" aria-label={`${name} details`} className="min-h-36 space-y-3">
                  <div className="flex items-center justify-between gap-4">
                    <p className="text-xs text-muted-foreground">{job.run_count} runs · Last result: {job.last_status || 'Not run yet'}{scope ? ` · ${scope.label}` : ''}</p>
                    {!panel.isReadOnlyUser && <div className="flex items-center gap-1">
                      <ScheduleRowActions {...panel} job={job} isRunning={isRunning} isMissedJob={false} menuButtonClassName="rounded-md p-1.5 text-muted-foreground hover:bg-muted hover:text-foreground" />
                    </div>}
                  </div>
                  {isWebhook && <WebhookEndpoint id={job.id} name={job.name} />}
                  {job.group_names?.length ? <p className="text-xs text-muted-foreground">Groups: {job.group_names.join(', ')}</p> : null}
                  {job.messages?.length ? <div className="max-w-4xl space-y-2"><h4 className="text-xs font-medium text-foreground">Instructions</h4>{job.messages.map((message, i) => <p key={i} className="whitespace-pre-wrap text-sm leading-6 text-muted-foreground">{message}</p>)}</div> : null}
                  {job.last_error && <p className="max-w-4xl whitespace-pre-wrap break-words text-xs leading-5 text-muted-foreground">Last run: {job.last_error}</p>}
                  {isMissedSchedule(job) && <p className="text-xs text-muted-foreground">{job.missed_run_count} missed occurrence{job.missed_run_count === 1 ? '' : 's'} recorded.</p>}
                  {job.waiting_reason && <p className="text-xs text-muted-foreground">{job.waiting_reason}</p>}
                  <p className="text-xs text-muted-foreground">{isWebhook ? 'Configure this webhook in Setup → API triggers.' : 'Ask this automation in Chat to change its schedule.'}</p>
                </div>
              </td></tr>}
            </React.Fragment>
          })}
        </tbody>
      </table>
    </div>
  </div>
}
