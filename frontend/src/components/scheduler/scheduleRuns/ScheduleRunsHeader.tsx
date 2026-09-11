import React from 'react'
import { X, Play, Loader, Pause, Calendar, RefreshCw } from 'lucide-react'
import { Tooltip, TooltipContent, TooltipTrigger } from '../../ui/tooltip'
import { formatExactDateTime, formatLastRunLabel } from './helpers'
import type { ScheduleRunsPanelState } from './useScheduleRunsData'

type ScheduleRunsHeaderProps = {
  panel: Pick<ScheduleRunsPanelState,
    | 'panelTitle' | 'isLoading' | 'isWorkflowScoped' | 'workflowScheduleSummary' | 'summary'
    | 'isSchedulerPaused' | 'isReadOnlyUser' | 'handleToggleGlobalPause' | 'isUpdatingSchedulerPause' | 'loadJobs'
  >
  onClose: () => void
  /** Embedded workspace views are closed by their parent layout, not here. */
  showClose?: boolean
  headerAction?: React.ReactNode
}

export const ScheduleRunsHeader: React.FC<ScheduleRunsHeaderProps> = ({ panel, onClose, showClose = true, headerAction }) => {
  const {
    panelTitle,
    isLoading,
    isWorkflowScoped,
    workflowScheduleSummary,
    summary,
    isSchedulerPaused,
    isReadOnlyUser,
    handleToggleGlobalPause,
    isUpdatingSchedulerPause,
    loadJobs,
  } = panel

  return (
    <div className="flex items-start justify-between gap-4 px-5 py-4 border-b border-border flex-shrink-0">
      <div className="min-w-0 space-y-2">
        <div className="flex items-center gap-2">
          <Calendar className="w-5 h-5 text-amber-500" />
          <h2 className="text-base font-semibold text-foreground">
            {panelTitle}
          </h2>
        </div>
        {!isLoading && (
          <div className="flex flex-wrap gap-1.5">
            {!isWorkflowScoped && (
              <span className="rounded-full border border-border bg-background px-2 py-0.5 text-xs text-muted-foreground">
                {workflowScheduleSummary.workflows} automation{workflowScheduleSummary.workflows === 1 ? '' : 's'}
              </span>
            )}
            {isWorkflowScoped && (
              <span className="rounded-full border border-border bg-background px-2 py-0.5 text-xs text-muted-foreground">
                {summary.total} schedule{summary.total === 1 ? '' : 's'}
              </span>
            )}
            {isWorkflowScoped && summary.total > 0 && (
              <span
                className="rounded-full border border-border bg-background px-2 py-0.5 text-xs text-muted-foreground"
                title={formatExactDateTime(summary.lastRunAt)}
              >
                Last ran {formatLastRunLabel(summary.lastRunAt)}
              </span>
            )}
            {!isWorkflowScoped && workflowScheduleSummary.running > 0 && (
              <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300">
                {workflowScheduleSummary.running} running
              </span>
            )}
            {!isWorkflowScoped && workflowScheduleSummary.attention > 0 && (
              <span className="rounded-full border border-red-500/30 bg-red-500/10 px-2 py-0.5 text-xs font-medium text-red-600 dark:text-red-300">
                {workflowScheduleSummary.attention} need attention
              </span>
            )}
            {!isWorkflowScoped && workflowScheduleSummary.fullyPaused > 0 && (
              <span className="rounded-full border border-border bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                {workflowScheduleSummary.fullyPaused} fully paused
              </span>
            )}
            {!isWorkflowScoped && workflowScheduleSummary.partlyPaused > 0 && (
              <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-xs text-amber-700 dark:text-amber-300">
                {workflowScheduleSummary.partlyPaused} partly paused
              </span>
            )}
            {isWorkflowScoped && summary.running > 0 && (
              <span className="rounded-full border border-amber-500/30 bg-amber-500/10 px-2 py-0.5 text-xs font-medium text-amber-700 dark:text-amber-300">
                {summary.running} running
              </span>
            )}
            {isWorkflowScoped && (
              <span className={`rounded-full border px-2 py-0.5 text-xs ${
                isSchedulerPaused
                  ? 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300'
                  : 'border-border bg-background text-muted-foreground'
              }`}
              >
                {isSchedulerPaused ? 'globally paused' : `${summary.enabled} active`}
              </span>
            )}
          </div>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {!isWorkflowScoped && !isReadOnlyUser && (
          <button
            onClick={handleToggleGlobalPause}
            disabled={isUpdatingSchedulerPause}
            className={`inline-flex items-center gap-2 rounded-md border px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-60 ${
              isSchedulerPaused
                ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300 hover:bg-emerald-500/20'
                : 'border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-300 hover:bg-amber-500/20'
            }`}
          >
            {isUpdatingSchedulerPause ? (
              <Loader className="w-3.5 h-3.5 animate-spin" />
            ) : isSchedulerPaused ? (
              <Play className="w-3.5 h-3.5" />
            ) : (
              <Pause className="w-3.5 h-3.5" />
            )}
            {isSchedulerPaused ? 'Resume schedules' : 'Pause all schedules'}
          </button>
        )}
        <Tooltip>
          <TooltipTrigger asChild>
            <button
              onClick={() => loadJobs(true)}
              disabled={isLoading}
              className="rounded-md p-1.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:cursor-wait disabled:opacity-60"
              aria-label={isLoading ? 'Refreshing schedule status' : 'Refresh schedule status'}
            >
              <RefreshCw className={`w-4 h-4 ${isLoading ? 'animate-spin' : ''}`} />
            </button>
          </TooltipTrigger>
          <TooltipContent side="bottom">{isLoading ? 'Refreshing…' : 'Refresh'}</TooltipContent>
        </Tooltip>
        {headerAction}
        {showClose && (
          <button onClick={onClose} className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors" aria-label="Close schedules">
            <X className="w-4 h-4" />
          </button>
        )}
      </div>
    </div>
  )
}
