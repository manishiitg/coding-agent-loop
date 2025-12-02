import React from 'react'

interface StepExecutionEventData {
  step_id?: string
  step_index?: number
  step_title?: string
  step_path?: string
  is_branch_step?: boolean
  error?: string
}

interface StepExecutionEventDisplayProps {
  event: StepExecutionEventData
  eventType: 'step_execution_start' | 'step_execution_end' | 'step_execution_failed'
  compact?: boolean
}

export const StepExecutionEventDisplay: React.FC<StepExecutionEventDisplayProps> = ({ 
  event, 
  eventType,
  compact = false 
}) => {
  const getTitle = () => {
    switch (eventType) {
      case 'step_execution_start':
        return 'Step Started'
      case 'step_execution_end':
        return 'Step Completed'
      case 'step_execution_failed':
        return 'Step Failed'
      default:
        return 'Step Event'
    }
  }

  return (
    <div className={`bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md ${compact ? 'p-2' : 'p-3'}`}>
      <div className={`${compact ? 'text-xs' : 'text-sm'} text-blue-700 dark:text-blue-300`}>
        <div className="font-medium">{getTitle()}</div>
        {event.step_title && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-blue-600 dark:text-blue-400 mt-1`}>
            {event.step_title}
          </div>
        )}
        {event.step_id && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-blue-500 dark:text-blue-500 mt-1`}>
            ID: {event.step_id}
          </div>
        )}
        {event.error && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-red-600 dark:text-red-400 mt-1`}>
            Error: {event.error}
          </div>
        )}
      </div>
    </div>
  )
}

