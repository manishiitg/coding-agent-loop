import { memo } from 'react'
import { Handle, Position, type NodeProps } from '@xyflow/react'
import { CalendarClock, RefreshCw, Settings, Webhook } from 'lucide-react'
import type { WorkflowTriggerNodeData } from '../hooks/usePlanToFlow'
import { describeCron } from '../../scheduler/scheduleRuns/cron'
import { formatLocalScheduleTime } from '../../scheduler/scheduleRuns/helpers'
import { WebhookEndpoint } from '../../scheduler/scheduleRuns/WebhookEndpoint'

export const WorkflowTriggerNode = memo(({ data }: NodeProps) => {
  const { job, routeSummary, active, onSelect, onSettings } = data as WorkflowTriggerNodeData
  if (!job) return null
  const webhook = job.schedule_type === 'webhook'
  const Icon = webhook ? Webhook : CalendarClock
  const cadence = webhook ? 'On request' : job.schedule_type === 'calendar' ? `${job.calendar_items?.length || 0} calendar dates` : describeCron(job.cron_expression)
  return <article className={`nodrag nopan flex h-[320px] w-[320px] flex-col gap-2 rounded-xl border bg-card p-3 text-card-foreground shadow-sm ${active ? 'border-primary ring-2 ring-primary/30' : 'border-border'}`} aria-label={`${webhook ? 'Webhook' : 'Schedule'}: ${job.name}`}>
    <div className="flex items-center gap-2 text-[11px] text-muted-foreground"><Icon className="h-4 w-4" /><span>{webhook ? 'Webhook' : 'Schedule'}</span><span className="ml-auto rounded border border-border px-1.5 py-0.5">{job.enabled ? 'Enabled' : 'Paused'}</span></div>
    <h3 className="line-clamp-2 text-sm font-semibold" title={job.name}>{job.name}</h3>
    <p className="line-clamp-2 text-xs text-muted-foreground" title={cadence}>{cadence}</p>
    {!webhook && <p className="text-[11px] text-muted-foreground">{job.enabled ? `Next: ${formatLocalScheduleTime(job.next_run_at)}` : 'Schedule paused'} · {job.timezone || 'UTC'}</p>}
    <p className="line-clamp-2 text-xs" title={routeSummary?.label}>{routeSummary?.label}</p>
    {!!job.group_names?.length && <p className="truncate text-[11px] text-muted-foreground" title={job.group_names.join(', ')}>Groups: {job.group_names.join(', ')}</p>}
    {webhook && <div className="max-h-28 overflow-auto"><WebhookEndpoint id={job.id} name={job.name} /></div>}
    <div className="mt-auto flex items-center gap-2 border-t border-border pt-2">
      <button type="button" onClick={onSelect} disabled={!routeSummary?.canTrace} aria-pressed={!!active} className="rounded border border-border px-2 py-1 text-xs hover:bg-muted disabled:opacity-40">{active ? 'Clear highlight' : 'Highlight path'}</button>
      <button type="button" onClick={() => onSettings?.(webhook ? 'api-triggers' : 'schedules')} className="ml-auto rounded p-1.5 text-muted-foreground hover:bg-muted" aria-label={`Open settings for ${job.name}`} title="Open settings"><Settings className="h-4 w-4" /></button>
    </div>
    {routeSummary?.canTrace && <Handle type="source" position={Position.Bottom} className="!h-2 !w-2 !bg-muted-foreground" />}
  </article>
})
WorkflowTriggerNode.displayName = 'WorkflowTriggerNode'

export const WorkflowTriggerHeading = memo(({ data }: NodeProps) => {
  const { loading, error, count, onSettings, onRefresh } = data as WorkflowTriggerNodeData
  return <div className="nodrag nopan h-16 space-y-1 text-foreground">
    <div className="flex items-center gap-2 text-sm font-semibold"><span>Triggers</span><button type="button" onClick={onRefresh} aria-label="Refresh triggers" className="rounded p-1 hover:bg-muted"><RefreshCw className={`h-3 w-3 ${loading ? 'animate-spin' : ''}`} /></button></div>
    <p className="text-xs text-muted-foreground">{error || (loading ? 'Loading schedules and webhooks…' : count ? 'Select a trigger to highlight its path from Start.' : 'No automatic triggers. This workflow can be started manually.')}</p>
    <div className="flex gap-3 text-xs"><button type="button" className="hover:underline" onClick={() => onSettings?.('schedules')}>Schedules</button><button type="button" className="hover:underline" onClick={() => onSettings?.('api-triggers')}>Webhooks</button></div>
  </div>
})
WorkflowTriggerHeading.displayName = 'WorkflowTriggerHeading'
