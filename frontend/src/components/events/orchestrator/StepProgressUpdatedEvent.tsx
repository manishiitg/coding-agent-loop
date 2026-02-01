import React from 'react'
import type { StepProgressUpdatedEvent } from '../../../generated/events-bridge'

interface StepProgressUpdatedEventDisplayProps {
  event: StepProgressUpdatedEvent
  compact?: boolean
}

export const StepProgressUpdatedEventDisplay: React.FC<StepProgressUpdatedEventDisplayProps> = ({
  event,
  compact = false
}) => {
  const isFailed = event.status === 'failed'
  const bgColor = isFailed ? 'bg-red-50 dark:bg-red-900/20' : 'bg-green-50 dark:bg-green-900/20'
  const borderColor = isFailed ? 'border-red-200 dark:border-red-800' : 'border-green-200 dark:border-green-800'
  const textColor = isFailed ? 'text-red-700 dark:text-red-300' : 'text-green-700 dark:text-green-300'
  const subTextColor = isFailed ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'
  const mutedTextColor = isFailed ? 'text-red-500 dark:text-red-500' : 'text-green-500 dark:text-green-500'

  return (
    <div className={`${bgColor} border ${borderColor} rounded-md ${compact ? 'p-2' : 'p-3'}`}>
      <div className={`${compact ? 'text-xs' : 'text-sm'} ${textColor}`}>
        <div className="font-medium">Step Progress Updated</div>
        {event.status && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} ${subTextColor} mt-1`}>
            Status: {event.status === 'start' ? 'Step Started' : event.status === 'end' ? 'Step Ended' : event.status === 'stop' ? 'Step Stopped' : event.status === 'failed' ? 'Step Failed' : event.status}
          </div>
        )}
        {event.current_step_id && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} ${subTextColor} mt-1`}>
            Current Step: {event.current_step_id}
          </div>
        )}
        {event.error && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-red-600 dark:text-red-400 mt-1`}>
            Error: {event.error}
          </div>
        )}
        {event.run_folder && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} ${mutedTextColor} mt-1`}>
            Run Folder: {event.run_folder}
          </div>
        )}
        {event.metadata && typeof event.metadata === 'object' && 'orchestrator_phase' in event.metadata && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} ${mutedTextColor} mt-1`}>
            Phase: {String(event.metadata.orchestrator_phase)}
          </div>
        )}
      </div>
    </div>
  )
}
