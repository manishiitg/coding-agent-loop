import { CheckCircle2, CircleAlert, CircleDashed, Clock3, XCircle, type LucideIcon } from 'lucide-react'
import type { ChatHistorySession, ScheduledJob, ScheduledJobRun } from '../services/api-types'

export type ScheduleActivityItem = {
  id: string
  job: ScheduledJob
  run?: ScheduledJobRun
  kind: 'run' | 'missed'
  occurredAt: string
}

export const scheduleStatusPresentation = (item: ScheduleActivityItem): {
  label: string
  className: string
  Icon: LucideIcon
  detail: string
} => {
  if (item.kind === 'missed') {
    return {
      label: 'Missed slot',
      className: 'border-amber-500/35 bg-amber-500/10 text-amber-700 dark:text-amber-300',
      Icon: CircleAlert,
      detail: item.job.missed_run_reason || 'No execution was created for this scheduled time.',
    }
  }

  const run = item.run!
  switch (run.status) {
    case 'success':
      return { label: 'Completed', className: 'border-emerald-500/35 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300', Icon: CheckCircle2, detail: 'Finished successfully.' }
    case 'running':
      return { label: 'Running', className: 'border-sky-500/35 bg-sky-500/10 text-sky-700 dark:text-sky-300', Icon: CircleDashed, detail: 'This occurrence is still running.' }
    case 'waiting_for_capacity':
    case 'waiting_for_workflow':
      return { label: 'Waiting', className: 'border-sky-500/35 bg-sky-500/10 text-sky-700 dark:text-sky-300', Icon: Clock3, detail: run.error || 'Waiting for its schedule policy to permit execution.' }
    case 'partial':
      return { label: 'Partial', className: 'border-amber-500/35 bg-amber-500/10 text-amber-700 dark:text-amber-300', Icon: CircleAlert, detail: run.error || 'The occurrence completed only partially.' }
    case 'interrupted':
    case 'stopped':
      return { label: 'Interrupted', className: 'border-amber-500/35 bg-amber-500/10 text-amber-700 dark:text-amber-300', Icon: CircleAlert, detail: run.error || 'The occurrence stopped before completion.' }
    default:
      return { label: 'Failed run', className: 'border-destructive/45 bg-destructive/10 text-destructive', Icon: XCircle, detail: run.error || 'This recorded execution failed.' }
  }
}

// Schedule history is a record of a conversation, not just a server job. Keep
// the compact card useful without dumping the scheduler's raw error into the
// primary line: show the instruction that started the run and its latest human
// readable agent update. The complete transcript remains available through
// Open, while the raw failure stays in a small disclosure for diagnosis.
export const scheduleRunStartMessage = (job: ScheduledJob, session?: ChatHistorySession): string => {
  const configuredMessage = (job.messages || []).find(message => message.trim()) || job.query || ''
  return (session?.query || configuredMessage || (job.schedule_type === 'webhook' ? 'An incoming webhook started the configured route.' : 'This scheduled run started without a saved instruction.')).trim()
}

export const scheduleRunLatestAgentMessage = (session?: ChatHistorySession): string | undefined => {
  return [...(session?.preview_messages || [])]
    .reverse()
    .find(message => ['assistant', 'ai'].includes(message.role.trim().toLowerCase()) && message.text.trim())
    ?.text
    .trim()
}

export const scheduleRunExcerpt = (text: string, maxLength = 240): string => {
  const normalized = text.replace(/\s+/g, ' ').trim()
  return normalized.length > maxLength ? `${normalized.slice(0, maxLength).trimEnd()}…` : normalized
}

export const formatScheduleRunTime = (value?: string): string => {
  if (!value) return 'Unknown time'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'Unknown time'
  return date.toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export const formatScheduleRunDuration = (durationMs?: number): string | undefined => {
  if (typeof durationMs !== 'number' || durationMs < 0) return undefined
  if (durationMs < 60_000) return `${Math.round(durationMs / 1000)}s`
  if (durationMs < 3_600_000) return `${Math.round(durationMs / 60_000)}m`
  const hours = Math.floor(durationMs / 3_600_000)
  const minutes = Math.round((durationMs % 3_600_000) / 60_000)
  return minutes ? `${hours}h ${minutes}m` : `${hours}h`
}
