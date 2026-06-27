import React from 'react'

interface WorkflowStartEventData {
  workflow_id?: string
  objective?: string
  message?: string
  timestamp?: number
}

interface WorkflowStartEventProps {
  event: WorkflowStartEventData
}

export const WorkflowStartEvent: React.FC<WorkflowStartEventProps> = ({ event }) => {
  return (
    <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-md p-3">
      <div className="flex items-center">
        <div className="flex-shrink-0">
          <div className="w-2 h-2 bg-green-500 rounded-full"></div>
        </div>
        <div className="ml-3">
          <div className="text-sm font-medium text-green-800 dark:text-green-200">
            Automation Started
          </div>
          <div className="text-xs text-green-600 dark:text-green-400 mt-1">
            {event.objective || 'Starting automation execution'}
          </div>
          {event.workflow_id && (
            <div className="text-xs text-green-500 dark:text-green-300 mt-1">
              ID: {event.workflow_id}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
