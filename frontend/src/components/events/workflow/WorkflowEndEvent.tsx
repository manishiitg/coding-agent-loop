import React from 'react'

interface WorkflowEndEventData {
  workflow_id?: string
  result?: string
  status?: string
  message?: string
  timestamp?: number
}

interface WorkflowEndEventProps {
  event: WorkflowEndEventData
}

export const WorkflowEndEvent: React.FC<WorkflowEndEventProps> = ({ event }) => {
  const isSuccess = event.status === 'completed' || event.status === 'success'
  const isError = event.status === 'error' || event.status === 'failed'
  
  const bgColor = isError 
    ? 'bg-red-50 dark:bg-red-900/20' 
    : isSuccess 
    ? 'bg-green-50 dark:bg-green-900/20' 
    : 'bg-purple-50 dark:bg-purple-900/20'
    
  const borderColor = isError 
    ? 'border-red-200 dark:border-red-800' 
    : isSuccess 
    ? 'border-green-200 dark:border-green-800' 
    : 'border-purple-200 dark:border-purple-800'
    
  const textColor = isError 
    ? 'text-red-800 dark:text-red-200' 
    : isSuccess 
    ? 'text-green-800 dark:text-green-200' 
    : 'text-purple-800 dark:text-purple-200'
    
  const subTextColor = isError 
    ? 'text-red-600 dark:text-red-400' 
    : isSuccess 
    ? 'text-green-600 dark:text-green-400' 
    : 'text-purple-600 dark:text-purple-400'
    
  const dotColor = isError 
    ? 'bg-red-500' 
    : isSuccess 
    ? 'bg-green-500' 
    : 'bg-purple-500'

  return (
    <div className={`${bgColor} border ${borderColor} rounded-md p-3`}>
      <div className="flex items-center">
        <div className="flex-shrink-0">
          <div className={`w-2 h-2 ${dotColor} rounded-full`}></div>
        </div>
        <div className="ml-3">
          <div className={`text-sm font-medium ${textColor}`}>
            Automation Completed: {event.status || 'Finished'}
          </div>
          <div className={`text-xs ${subTextColor} mt-1`}>
            {event.message || 'Automation execution completed'}
          </div>
          {event.workflow_id && (
            <div className={`text-xs ${subTextColor} mt-1`}>
              ID: {event.workflow_id}
            </div>
          )}
          {event.result && (
            <div className={`text-xs ${subTextColor} mt-1`}>
              Result: {event.result}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
