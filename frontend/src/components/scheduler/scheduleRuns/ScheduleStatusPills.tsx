import React from 'react'
import { formatExactDateTime, formatLastRunLabel } from './helpers'
import type { ScheduleRunsPanelState } from './useScheduleRunsData'

export type ScheduleStatusSnapshot = Pick<ScheduleRunsPanelState,
  | 'summary' | 'workflowScheduleSummary' | 'isLoading' | 'isSchedulerPaused' | 'isWorkflowScoped'
>

/**
 * Status pills shared by the schedules modal header and the Automation hub
 * header: counts, last run, and pause state. Rendered in the header's
 * stats row, never beside a second action pair.
 */
export const ScheduleStatusPills: React.FC<{ status: ScheduleStatusSnapshot }> = ({ status }) => {
  const { summary, workflowScheduleSummary, isLoading, isSchedulerPaused, isWorkflowScoped } = status
  if (isLoading) return null
  return (
    <div className="flex flex-wrap items-center gap-2">
      {!isWorkflowScoped && (
        <span className="rounded-full border border-border bg-background px-2.5 py-0.5 text-xs text-muted-foreground">
          {workflowScheduleSummary.workflows} automation{workflowScheduleSummary.workflows === 1 ? '' : 's'}
        </span>
      )}
      {isWorkflowScoped && (
        <span className="rounded-full border border-border bg-background px-2.5 py-0.5 text-xs text-muted-foreground">
          {summary.total} schedule{summary.total === 1 ? '' : 's'}
        </span>
      )}
      {isWorkflowScoped && summary.total > 0 && (
        <span
          className="rounded-full border border-border bg-background px-2.5 py-0.5 text-xs text-muted-foreground"
          title={formatExactDateTime(summary.lastRunAt)}
        >
          {summary.lastRunAt ? `Last ran ${formatLastRunLabel(summary.lastRunAt)}` : 'Never run'}
        </span>
      )}
      {!isWorkflowScoped && workflowScheduleSummary.running > 0 && (
        <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2.5 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300">
          {workflowScheduleSummary.running} running
        </span>
      )}
      {!isWorkflowScoped && workflowScheduleSummary.fullyPaused > 0 && (
        <span className="rounded-full border border-border bg-muted px-2.5 py-0.5 text-xs text-muted-foreground">
          {workflowScheduleSummary.fullyPaused} fully paused
        </span>
      )}
      {!isWorkflowScoped && workflowScheduleSummary.partlyPaused > 0 && (
        <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2.5 py-0.5 text-xs text-amber-700 dark:text-amber-300">
          {workflowScheduleSummary.partlyPaused} partly paused
        </span>
      )}
      {isWorkflowScoped && summary.running > 0 && (
        <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2.5 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300">
          {summary.running} running
        </span>
      )}
      {isWorkflowScoped && (
        <span className={`rounded-full border px-2.5 py-0.5 text-xs ${
          isSchedulerPaused
            ? 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300'
            : 'border-border bg-background text-muted-foreground'
        }`}
        >
          {isSchedulerPaused ? 'globally paused' : `${summary.enabled} active`}
        </span>
      )}
    </div>
  )
}
