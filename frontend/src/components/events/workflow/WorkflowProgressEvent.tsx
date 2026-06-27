import React from 'react'

interface WorkflowProgressEventData {
  phase?: string
  message?: string
  timestamp?: number
}

interface WorkflowProgressEventProps {
  event: WorkflowProgressEventData
}

export const WorkflowProgressEvent: React.FC<WorkflowProgressEventProps> = ({ event }) => {
  return (
    <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md p-3">
      <div className="flex items-center">
        <div className="flex-shrink-0">
          <div className="w-2 h-2 bg-blue-500 rounded-full animate-pulse"></div>
        </div>
        <div className="ml-3">
          <div className="text-sm font-medium text-blue-800 dark:text-blue-200">
            Automation Progress: {event.phase || 'Processing'}
          </div>
          <div className="text-xs text-blue-600 dark:text-blue-400 mt-1">
            {event.message || 'Automation is in progress'}
          </div>
        </div>
      </div>
    </div>
  )
}
