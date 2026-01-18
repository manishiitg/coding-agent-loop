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
  return (
    <div className={`bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-md ${compact ? 'p-2' : 'p-3'}`}>
      <div className={`${compact ? 'text-xs' : 'text-sm'} text-green-700 dark:text-green-300`}>
        <div className="font-medium">Step Progress Updated</div>
        {event.status && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-600 dark:text-green-400 mt-1`}>
            Status: {event.status === 'start' ? 'Step Started' : event.status === 'end' ? 'Step Ended' : event.status === 'stop' ? 'Step Stopped' : event.status}
          </div>
        )}
        {event.current_step_id && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-600 dark:text-green-400 mt-1`}>
            Current Step: {event.current_step_id}
          </div>
        )}
        {event.status && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-500 dark:text-green-500 mt-1`}>
            Status: {event.status === 'start' ? 'Started' : event.status === 'end' ? 'Ended' : event.status}
          </div>
        )}
        {event.run_folder && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-500 dark:text-green-500 mt-1`}>
            Run Folder: {event.run_folder}
          </div>
        )}
        {event.metadata && typeof event.metadata === 'object' && 'orchestrator_phase' in event.metadata && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-500 dark:text-green-500 mt-1`}>
            Phase: {String(event.metadata.orchestrator_phase)}
          </div>
        )}
      </div>
    </div>
  )
}

