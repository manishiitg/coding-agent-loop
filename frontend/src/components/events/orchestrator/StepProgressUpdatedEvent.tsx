import React from 'react'

interface StepProgressUpdatedEventData {
  completed_step_indices?: number[]
  total_steps?: number
  last_completed_step?: number
  workspace_path?: string
  run_folder?: string
  metadata?: {
    orchestrator_agent_name?: string
    orchestrator_iteration?: number
    orchestrator_phase?: string
    orchestrator_step?: number
  }
}

interface StepProgressUpdatedEventDisplayProps {
  event: StepProgressUpdatedEventData
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
        {event.total_steps !== undefined && event.completed_step_indices && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-600 dark:text-green-400 mt-1`}>
            Progress: {event.completed_step_indices.length} / {event.total_steps} steps completed
          </div>
        )}
        {event.last_completed_step !== undefined && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-500 dark:text-green-500 mt-1`}>
            Last completed: Step {event.last_completed_step + 1}
          </div>
        )}
        {event.metadata?.orchestrator_phase && (
          <div className={`${compact ? 'text-[10px]' : 'text-xs'} text-green-500 dark:text-green-500 mt-1`}>
            Phase: {event.metadata.orchestrator_phase}
          </div>
        )}
      </div>
    </div>
  )
}

