import React from 'react'
import type { PrerequisiteNavigationEvent } from '../../../generated/event-types'

interface PrerequisiteNavigationEventDisplayProps {
  event: PrerequisiteNavigationEvent
  compact?: boolean
}

export const PrerequisiteNavigationEventDisplay: React.FC<PrerequisiteNavigationEventDisplayProps> = ({ 
  event, 
  compact = false 
}) => {
  const fromStepId = event.from_step_id || `step-${(event.from_step_index ?? 0) + 1}`
  const toStepId = event.to_step_id || `step-${(event.to_step_index ?? 0) + 1}`
  
  return (
    <div className={`bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded-md ${compact ? 'p-2' : 'p-3'}`}>
      <div className={`${compact ? 'text-xs' : 'text-sm'} text-yellow-700 dark:text-yellow-300`}>
        <div className="font-semibold text-yellow-800 dark:text-yellow-200">🔄 Prerequisite Navigation</div>
        <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-yellow-600 dark:text-yellow-400 mt-1`}>
          Navigating from step <code className="px-1 py-0.5 bg-yellow-100 dark:bg-yellow-900/40 rounded text-xs font-mono">{fromStepId}</code> to step <code className="px-1 py-0.5 bg-yellow-100 dark:bg-yellow-900/40 rounded text-xs font-mono">{toStepId}</code>
        </div>
        {event.reason && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-yellow-600 dark:text-yellow-400 mt-1`}>
            <span className="font-medium">Reason:</span> {event.reason}
          </div>
        )}
        {event.failure_type && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-yellow-500 dark:text-yellow-500 mt-1`}>
            Failure type: {event.failure_type}
          </div>
        )}
      </div>
    </div>
  )
}

