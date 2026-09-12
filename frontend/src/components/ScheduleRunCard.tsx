import { ArrowUpRight, ChevronRight, Loader2, RotateCcw, Trash2 } from 'lucide-react'
import type { ChatHistorySession, ScheduledJob, ScheduledJobRun } from '../services/api-types'
import { workflowTriggerLabel } from '../utils/workflowSessionKinds'
import { scheduleRunSlotLabel } from '../utils/scheduleRunSlot'
import {
  type ScheduleActivityItem,
  formatScheduleRunDuration,
  formatScheduleRunTime,
  scheduleRunExcerpt,
  scheduleRunLatestAgentMessage,
  scheduleRunStartMessage,
  scheduleStatusPresentation,
} from '../utils/scheduleRunPresentation'
import { ChatSessionIdCopyButton } from './ChatSessionIdCopyButton'

interface ScheduleRunCardProps {
  job: ScheduledJob
  run: ScheduledJobRun
  // Only a caller that already has resolved ChatHistorySession objects (today,
  // just PreviousChatHistoryPanel) can supply this to enrich "latest agent
  // update" and the started-with fallback with real conversation content.
  // Without it, the card still renders correctly: scheduleRunStartMessage
  // falls back to the job's configured message, and the outcome line falls
  // back to the status presentation's detail text.
  resolveSession?: (run: ScheduledJobRun) => ChatHistorySession | undefined
  onOpen: (run: ScheduledJobRun) => void
  onDelete?: (run: ScheduledJobRun) => void
  deletingRunIds: Set<string>
  compact?: boolean
  // Set by a flat, cross-schedule feed (PreviousChatHistoryPanel's Schedules
  // tab) where runs from different schedules are interleaved and the job
  // grouping that used to make this obvious is gone. Omitted by
  // ScheduleExecutionHistoryList, which still groups runs under a job header
  // that already names the schedule.
  showScheduleName?: boolean
  /** Show the durable chat/session identifier for debugging. */
  showCopySessionId?: boolean
}

export function ScheduleRunCard({
  job,
  run,
  resolveSession,
  onOpen,
  onDelete,
  deletingRunIds,
  compact = false,
  showScheduleName = false,
  showCopySessionId = false,
}: ScheduleRunCardProps) {
  const item: ScheduleActivityItem = { id: run.id, job, run, kind: 'run', occurredAt: run.started_at }
  const presentation = scheduleStatusPresentation(item)
  const Icon = presentation.Icon
  const session = resolveSession?.(run)
  const canResume = run.status === 'interrupted' && !!run.session_id
  const duration = formatScheduleRunDuration(run.duration_ms)
  const isDeleting = !!run.session_id && deletingRunIds.has(run.session_id)
  const startedWith = scheduleRunStartMessage(job, session)
  const latestAgentUpdate = scheduleRunLatestAgentMessage(session)
  const outcome = latestAgentUpdate || presentation.detail
  const triggerLabel = workflowTriggerLabel({ sessionId: run.session_id, triggeredBy: run.trigger_source || (job.schedule_type === 'webhook' ? 'webhook' : 'cron') })
  const slotLabel = scheduleRunSlotLabel(job, run)

  return (
    <div className="space-y-2.5">
      <div className="flex items-start gap-2">
        <div className={`mt-0.5 inline-flex shrink-0 items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] font-semibold ${presentation.className}`}>
          <Icon className={`h-3 w-3 ${run.status === 'running' ? 'animate-spin' : ''}`} />
          <span>{presentation.label}</span>
        </div>
        <div className="min-w-0 flex-1">
          {showScheduleName && (
            <div className="flex items-center gap-1 text-[11px] font-medium text-muted-foreground">
              <ChevronRight className="h-3 w-3 shrink-0" />
              <span className="truncate">{run.webhook?.trigger_name || job.name}</span>
            </div>
          )}
          <div className="text-sm font-medium text-foreground">
            {run.status === 'success'
              ? duration ? `Completed in ${duration}` : 'Completed'
              : run.status === 'running'
                ? 'Run in progress'
                : duration ? `Stopped after ${duration}` : 'Run stopped'}
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
            {triggerLabel && <span className="rounded border border-border px-1.5 py-0.5" aria-label={`Trigger: ${triggerLabel}`}>{triggerLabel}</span>}
            {run.run_folder && <span>{run.run_folder}</span>}
            {run.webhook?.event && <span>{run.webhook.event}</span>}
            {slotLabel && <span className="font-medium text-foreground/75">{slotLabel}</span>}
            <span>Started {formatScheduleRunTime(run.started_at)}</span>
            {run.completed_at && <span>ended {formatScheduleRunTime(run.completed_at)}</span>}
            {run.group_names?.length ? <span>{run.group_names.join(', ')}</span> : null}
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          {run.session_id && showCopySessionId && (
            <ChatSessionIdCopyButton sessionId={run.session_id} compact />
          )}
          {run.session_id && (
            <button
              type="button"
              onClick={() => onOpen(run)}
              className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground"
            >
              {canResume ? <RotateCcw className="h-3.5 w-3.5" /> : <ArrowUpRight className="h-3.5 w-3.5" />}
              {!compact && <span>{canResume ? 'Resume' : 'Open'}</span>}
            </button>
          )}
          {run.session_id && onDelete && (
            <button
              type="button"
              onClick={() => onDelete(run)}
              disabled={isDeleting}
              className="inline-flex items-center rounded border border-border bg-background p-1 text-destructive/75 transition-colors hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
              aria-label="Delete conversation record"
              title="Delete conversation record; the schedule execution remains"
            >
              {isDeleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
            </button>
          )}
        </div>
      </div>

      <div className="space-y-2 border-l-2 border-border/80 pl-3">
        <div>
          <div className="mb-0.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">Started with</div>
          <p className="line-clamp-2 text-xs leading-5 text-muted-foreground">{scheduleRunExcerpt(startedWith)}</p>
        </div>
        <div>
          <div className="mb-0.5 text-[10px] font-semibold uppercase tracking-wide text-muted-foreground">{latestAgentUpdate ? 'Latest agent update' : 'Outcome'}</div>
          <p className="line-clamp-3 text-xs leading-5 text-foreground/90">{scheduleRunExcerpt(outcome)}</p>
        </div>
      </div>

      {run.webhook && (
        <details className="text-[11px] text-muted-foreground">
          <summary className="cursor-pointer font-medium">Delivery details</summary>
          <dl className="mt-1 space-y-1 break-words rounded border border-border p-2">
            <dt>Trigger</dt><dd>{run.webhook.trigger_name}</dd>
            <dt>Delivery ID</dt><dd className="font-mono">{run.webhook.delivery_id}</dd>
            {run.webhook.event && <><dt>Event</dt><dd>{run.webhook.event}</dd></>}
            <dt>Received</dt><dd>{formatScheduleRunTime(run.webhook.received_at)}</dd>
          </dl>
        </details>
      )}

      {run.error && (
        <details className="text-[11px] text-muted-foreground">
          <summary className="cursor-pointer select-none font-medium hover:text-foreground">Technical details</summary>
          <pre className="mt-1 max-h-32 overflow-auto whitespace-pre-wrap break-words rounded border border-destructive/20 bg-destructive/5 px-2 py-1.5 font-mono text-[10px] leading-4 text-muted-foreground">{run.error}</pre>
        </details>
      )}
    </div>
  )
}
