import { ArrowUpRight, ChevronDown, ChevronRight, Loader2, RotateCcw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { workflowWebhooksApi } from '../api/workflowWebhooks'
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
  onOpen?: (run: ScheduledJobRun) => void
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
  loadWebhookPayload?: (job: ScheduledJob, run: ScheduledJobRun) => Promise<{ raw_payload: string }>
  openLabel?: string
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
  loadWebhookPayload: loadWebhookPayloadProp,
  openLabel,
}: ScheduleRunCardProps) {
  const [expanded, setExpanded] = useState(false)
  const [webhookPayload, setWebhookPayload] = useState<string>()
  const [webhookPayloadError, setWebhookPayloadError] = useState<string>()
  const [isLoadingWebhookPayload, setIsLoadingWebhookPayload] = useState(false)
  const item: ScheduleActivityItem = { id: run.id, job, run, kind: 'run', occurredAt: run.started_at }
  const presentation = scheduleStatusPresentation(item)
  const Icon = presentation.Icon
  const session = resolveSession?.(run)
  const canResume = run.status === 'interrupted' && !!run.session_id
  const duration = formatScheduleRunDuration(run.duration_ms)
  const isDeleting = !!run.session_id && deletingRunIds.has(run.session_id)
  const startedWith = scheduleRunStartMessage(job, session)
  const latestAgentUpdate = run.final_response || scheduleRunLatestAgentMessage(session)
  const outcome = latestAgentUpdate || presentation.detail
  const outcomeLabel = run.final_response || (job.schedule_type === 'webhook' && latestAgentUpdate)
    ? 'Final response'
    : latestAgentUpdate ? 'Latest agent update' : 'Outcome'
  const triggerLabel = workflowTriggerLabel({ sessionId: run.session_id, triggeredBy: run.trigger_source || (job.schedule_type === 'webhook' ? 'webhook' : 'cron') })
  const slotLabel = scheduleRunSlotLabel(job, run)
  const isWebhookRun = run.trigger_source === 'webhook' || job.schedule_type === 'webhook'
  const showOutcome = !isWebhookRun || !!latestAgentUpdate
  const headline = run.status === 'success'
    ? duration ? `Completed in ${duration}` : 'Completed'
    : run.status === 'running'
      ? 'Run in progress'
      : duration ? `Stopped after ${duration}` : 'Run stopped'
  const title = showScheduleName ? (run.webhook?.trigger_name || job.name) : headline
  const summary = [
    showScheduleName ? duration : undefined,
    slotLabel || undefined,
    `Started ${formatScheduleRunTime(run.started_at)}`,
    showOutcome ? scheduleRunExcerpt(outcome, 140) : undefined,
  ].filter((part): part is string => Boolean(part)).join(' · ')

  const loadPayload = async () => {
    if (!run.webhook || webhookPayload !== undefined || isLoadingWebhookPayload) return
    setIsLoadingWebhookPayload(true)
    setWebhookPayloadError(undefined)
    try {
      const result = await (loadWebhookPayloadProp
        ? loadWebhookPayloadProp(job, run)
        : workflowWebhooksApi.getPayload(job.id, run.id))
      let formatted = result.raw_payload
      try {
        formatted = JSON.stringify(JSON.parse(result.raw_payload), null, 2)
      } catch {
        // Keep the stored body visible if an older retained delivery cannot be
        // reformatted for any reason.
      }
      setWebhookPayload(formatted)
    } catch {
      setWebhookPayloadError(run.artifacts_expired ? 'Payload expired with this run’s retained artifacts.' : 'Payload could not be loaded.')
    } finally {
      setIsLoadingWebhookPayload(false)
    }
  }

  return (
    <div className="space-y-2.5">
      <button
        type="button"
        onClick={() => setExpanded(current => !current)}
        aria-expanded={expanded}
        className="flex w-full items-start gap-2 text-left"
      >
        <div className={`mt-0.5 inline-flex shrink-0 items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] font-semibold ${presentation.className}`}>
          <Icon className={`h-3 w-3 ${run.status === 'running' ? 'animate-spin' : ''}`} />
          <span>{presentation.label}</span>
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm font-medium text-foreground">{title}</div>
          <div className="mt-0.5 truncate text-[11px] text-muted-foreground">{summary}</div>
        </div>
        {expanded
          ? <ChevronDown className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
          : <ChevronRight className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />}
      </button>

      {expanded && (
        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
            {triggerLabel && <span className="rounded border border-border px-1.5 py-0.5" aria-label={`Trigger: ${triggerLabel}`}>{triggerLabel}</span>}
            {run.run_folder && <span>{run.run_folder}</span>}
            {run.webhook?.event && <span>{run.webhook.event}</span>}
            {run.artifacts_expired && <span className="rounded border border-border px-1.5 py-0.5">Artifacts expired</span>}
            {run.completed_at && <span>Ended {formatScheduleRunTime(run.completed_at)}</span>}
            {run.group_names?.length ? <span>{run.group_names.join(', ')}</span> : null}
          </div>

          {!isWebhookRun && (
            <div>
              <div className="text-xs text-muted-foreground">Started with</div>
              <p className="mt-0.5 whitespace-pre-wrap break-words text-xs leading-5 text-foreground/90">{startedWith}</p>
            </div>
          )}
          {showOutcome && (
            <div>
              <div className="text-xs text-muted-foreground">{outcomeLabel}</div>
              <p className="mt-0.5 whitespace-pre-wrap break-words text-xs leading-5 text-foreground/90">{outcome}</p>
            </div>
          )}

          {isWebhookRun && (
            <details
              className="text-[11px] text-muted-foreground"
              onToggle={event => {
                if (event.currentTarget.open && run.webhook) void loadPayload()
              }}
            >
              <summary className="cursor-pointer font-medium">Delivery details</summary>
              {run.webhook ? (
                <dl className="mt-1 space-y-1 break-words rounded border border-border p-2">
                  <dt>Trigger</dt><dd>{run.webhook.trigger_name}</dd>
                  <dt>Delivery ID</dt><dd className="font-mono">{run.webhook.delivery_id}</dd>
                  {run.webhook.event && <><dt>Event</dt><dd>{run.webhook.event}</dd></>}
                  <dt>Received</dt><dd>{formatScheduleRunTime(run.webhook.received_at)}</dd>
                  <dt>Payload (JSON body)</dt>
                  <dd>
                    {isLoadingWebhookPayload && <span>Loading payload…</span>}
                    {webhookPayloadError && <span>{webhookPayloadError}</span>}
                    {webhookPayload !== undefined && (
                      <pre className="mt-1 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded bg-muted/50 p-2 font-mono text-[10px] leading-4 text-foreground">{webhookPayload}</pre>
                    )}
                  </dd>
                </dl>
              ) : (
                <p className="mt-1 rounded border border-border p-2">
                  Payload and final response were not retained for this delivery because it ran before delivery history capture was enabled. Send a new delivery to record both.
                </p>
              )}
            </details>
          )}

          {run.error && (
            <details className="text-[11px] text-muted-foreground">
              <summary className="cursor-pointer select-none font-medium hover:text-foreground">Technical details</summary>
              <pre className="mt-1 max-h-32 overflow-auto whitespace-pre-wrap break-words rounded border border-destructive/20 bg-destructive/5 px-2 py-1.5 font-mono text-[10px] leading-4 text-muted-foreground">{run.error}</pre>
            </details>
          )}

          {run.session_id && (showCopySessionId || onOpen || onDelete) && (
            <div className="flex items-center gap-1.5">
              {run.session_id && showCopySessionId && (
                <ChatSessionIdCopyButton sessionId={run.session_id} compact />
              )}
              {run.session_id && onOpen && (
                <button
                  type="button"
                  onClick={() => onOpen(run)}
                  className="inline-flex items-center gap-1 rounded border border-border bg-background px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:border-primary/40 hover:text-foreground"
                >
                  {canResume ? <RotateCcw className="h-3.5 w-3.5" /> : <ArrowUpRight className="h-3.5 w-3.5" />}
                  {!compact && <span>{canResume ? 'Resume' : (openLabel || 'View chat')}</span>}
                </button>
              )}
              {run.session_id && onDelete && (
                <button
                  type="button"
                  onClick={() => onDelete(run)}
                  disabled={isDeleting}
                  className="inline-flex items-center rounded border border-border bg-background p-1 text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground disabled:opacity-50"
                  aria-label="Delete conversation record"
                  title="Delete conversation record; the schedule execution remains"
                >
                  {isDeleting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Trash2 className="h-3.5 w-3.5" />}
                </button>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
