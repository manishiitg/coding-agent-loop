import React from 'react'
import { MoreHorizontal, Pause, Play, Square, Trash2 } from 'lucide-react'
import type { ScheduledJob } from '../../../services/api-types'
import type { ScheduleRunsPanelState } from './useScheduleRunsData'

type ScheduleRowActionsProps = Pick<
  ScheduleRunsPanelState,
  'isReadOnlyUser' | 'triggering' | 'openActionMenuJobId' | 'setOpenActionMenuJobId' | 'handleStopRun' | 'handleTrigger' | 'handleToggle' | 'handleDelete'
> & {
  job: ScheduledJob
  isRunning: boolean
  isMissedJob: boolean
  /** The kebab button's classes -- the two hosting rows style it differently. */
  menuButtonClassName: string
}

// The Stop / Run now / Resume action plus the "More schedule actions" menu
// shared by the flat schedule list and the by-automation rows.
export const ScheduleRowActions: React.FC<ScheduleRowActionsProps> = ({
  job,
  isRunning,
  isMissedJob,
  isReadOnlyUser,
  triggering,
  openActionMenuJobId,
  setOpenActionMenuJobId,
  handleStopRun,
  handleTrigger,
  handleToggle,
  handleDelete,
  menuButtonClassName,
}) => (
  <>
    {!isReadOnlyUser && (
      job.enabled ? (
        isRunning ? (
          <button
            type="button"
            onClick={() => handleStopRun(job)}
            className="inline-flex items-center gap-1 rounded-md border border-red-200 bg-red-50 px-2 py-1 text-xs font-medium text-red-600 transition-colors hover:bg-red-100 dark:border-red-800 dark:bg-red-900/30 dark:text-red-400 dark:hover:bg-red-900/50"
          >
            <Square className="h-3 w-3" />
            Stop
          </button>
        ) : job.schedule_type !== 'webhook' ? (
          <button
            type="button"
            onClick={() => handleTrigger(job)}
            disabled={triggering === job.id}
            className={`inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs font-medium transition-colors disabled:opacity-40 ${
              isMissedJob
                ? 'border-amber-200 bg-amber-50 text-amber-700 hover:bg-amber-100 dark:border-amber-800 dark:bg-amber-900/30 dark:text-amber-300 dark:hover:bg-amber-900/50'
                : 'border-border bg-background text-muted-foreground hover:bg-muted hover:text-green-600'
            }`}
          >
            <Play className="h-3 w-3" />
            Run now
          </button>
        ) : null
      ) : (
        <button
          type="button"
          onClick={() => handleToggle(job)}
          className="inline-flex items-center gap-1 rounded-md border border-green-200 bg-green-50 px-2 py-1 text-xs font-medium text-green-600 transition-colors hover:bg-green-100 dark:border-green-800 dark:bg-green-900/30 dark:text-green-400 dark:hover:bg-green-900/50"
        >
          <Play className="h-3 w-3" />
          Resume
        </button>
      )
    )}
    <div className="relative">
      <button
        type="button"
        aria-label="More schedule actions"
        aria-expanded={openActionMenuJobId === job.id}
        onPointerDown={(event) => event.stopPropagation()}
        onClick={(event) => {
          event.stopPropagation()
          setOpenActionMenuJobId((openId) => openId === job.id ? null : job.id)
        }}
        className={menuButtonClassName}
      >
        <MoreHorizontal className="h-3.5 w-3.5" />
      </button>
      {openActionMenuJobId === job.id && (
        <div
          role="menu"
          onPointerDown={(event) => event.stopPropagation()}
          className="absolute right-0 top-8 z-30 w-36 rounded-md border border-border bg-popover p-1 shadow-lg"
        >
          {!isReadOnlyUser && job.enabled && !isRunning && (
            <button
              type="button"
              role="menuitem"
              onClick={() => { setOpenActionMenuJobId(null); handleToggle(job) }}
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs text-popover-foreground hover:bg-muted"
            >
              <Pause className="h-3.5 w-3.5" /> Pause schedule
            </button>
          )}
          {!isReadOnlyUser && (
            <button
              type="button"
              role="menuitem"
              onClick={() => { setOpenActionMenuJobId(null); handleDelete(job) }}
              className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs text-red-600 hover:bg-red-500/10 dark:text-red-400"
            >
              <Trash2 className="h-3.5 w-3.5" /> Delete schedule
            </button>
          )}
        </div>
      )}
    </div>
  </>
)
