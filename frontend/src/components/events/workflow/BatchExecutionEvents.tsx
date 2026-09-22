import React from 'react'
import type { BatchExecutionCanceledEvent } from '../../../generated/event-types'
import { Ban } from 'lucide-react'

interface BatchExecutionEventProps<T> {
  event: T
  compact?: boolean
}

export const BatchExecutionCanceledEventDisplay: React.FC<BatchExecutionEventProps<BatchExecutionCanceledEvent>> = ({ event, compact }) => {
  return (
    <div className={`border-l-2 border-blue-500 pl-3 py-3 bg-white dark:bg-gray-800/40 rounded-r-md ${compact ? 'text-xs' : 'text-sm'}`}>
      <div className="flex items-center gap-2 mb-2">
        <div className="p-1 bg-blue-100 dark:bg-blue-900/20 rounded-md">
          <Ban className="w-4 h-4 text-blue-600 dark:text-blue-400" />
        </div>
        <span className="font-bold text-gray-900 dark:text-gray-100">
          Batch Execution Canceled
        </span>
      </div>
      
      <div className="p-2.5 bg-gray-50 dark:bg-gray-900/50 rounded border border-gray-200 dark:border-gray-700/50 mb-2">
        <div className="text-xs text-gray-700 dark:text-gray-300 font-medium mb-1">
          Stopped after {event.completed_groups} of {event.total_groups} groups
        </div>
        {event.canceled_group_name && (
          <div className="text-[10px] text-gray-500 dark:text-gray-400 font-mono">
            Interrupted Group: {event.canceled_group_name}
          </div>
        )}
      </div>
      
      {event.reason && (
        <div className="text-xs text-gray-600 dark:text-gray-400 italic pl-1 border-l-2 border-blue-300 dark:border-blue-600 ml-1 py-0.5">
          "{event.reason}"
        </div>
      )}
    </div>
  )
}
