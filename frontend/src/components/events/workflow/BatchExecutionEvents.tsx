import React from 'react'
import type { 
  BatchExecutionStartEvent, 
  BatchExecutionEndEvent 
} from '../../../generated/event-types'
import { Layers, CheckCircle, XCircle, Clock } from 'lucide-react'

interface BatchExecutionEventProps<T> {
  event: T
  compact?: boolean
}

export const BatchExecutionStartEventDisplay: React.FC<BatchExecutionEventProps<BatchExecutionStartEvent>> = ({ event, compact }) => {
  return (
    <div className={`border-l-2 border-indigo-500 pl-3 py-2 bg-indigo-50/30 dark:bg-indigo-900/10 rounded-r-md ${compact ? 'text-xs' : 'text-sm'}`}>
      <div className="flex items-center gap-2 mb-1">
        <Layers className="w-4 h-4 text-indigo-600 dark:text-indigo-400" />
        <span className="font-bold text-indigo-800 dark:text-indigo-200">
          Batch Execution Started
        </span>
        <span className="text-[10px] bg-indigo-100 dark:bg-indigo-900/50 text-indigo-700 dark:text-indigo-300 px-1.5 py-0.5 rounded-full border border-indigo-200 dark:border-indigo-800">
          Iteration {event.iteration_number}
        </span>
      </div>
      
      <div className="flex flex-col gap-1 mt-2">
        <div className="flex items-center gap-2 text-indigo-700 dark:text-indigo-300">
          <span className="font-semibold">{event.total_groups}</span>
          <span>groups scheduled for execution</span>
        </div>
        
        {event.execution_options && !compact && Object.keys(event.execution_options).length > 0 && (
          <div className="mt-1 text-xs text-gray-500 dark:text-gray-400 bg-white/50 dark:bg-black/20 p-2 rounded border border-indigo-100 dark:border-indigo-900/30">
            <div className="font-medium mb-1 text-[10px] uppercase tracking-wider opacity-70">Execution Options</div>
            <pre className="whitespace-pre-wrap font-mono text-[10px]">
              {JSON.stringify(event.execution_options, null, 2)}
            </pre>
          </div>
        )}
      </div>
    </div>
  )
}

export const BatchExecutionEndEventDisplay: React.FC<BatchExecutionEventProps<BatchExecutionEndEvent>> = ({ event, compact }) => {
  const isSuccess = event.success
  
  return (
    <div className={`border-l-2 ${isSuccess ? 'border-green-500 bg-green-50/30 dark:bg-green-900/10' : 'border-red-500 bg-red-50/30 dark:bg-red-900/10'} pl-3 py-2 rounded-r-md ${compact ? 'text-xs' : 'text-sm'}`}>
      <div className="flex items-center gap-2 mb-2">
        {isSuccess ? (
          <CheckCircle className="w-4 h-4 text-green-600 dark:text-green-400" />
        ) : (
          <XCircle className="w-4 h-4 text-red-600 dark:text-red-400" />
        )}
        <span className={`font-bold ${isSuccess ? 'text-green-800 dark:text-green-200' : 'text-red-800 dark:text-red-200'}`}>
          Batch Execution {isSuccess ? 'Completed' : 'Failed'}
        </span>
        <span className="text-[10px] px-1.5 py-0.5 rounded-full border opacity-80">
          Iteration {event.iteration_number}
        </span>
      </div>

      <div className="grid grid-cols-2 gap-2 mt-2">
        <div className={`p-2 rounded border ${isSuccess ? 'bg-green-100/50 border-green-200 dark:bg-green-900/20 dark:border-green-800' : 'bg-red-100/50 border-red-200 dark:bg-red-900/20 dark:border-red-800'}`}>
          <div className="text-[10px] uppercase tracking-wider opacity-70 mb-1">Status</div>
          <div className="font-medium flex items-center gap-2">
            <span>{event.completed_groups} / {event.total_groups}</span>
            <span className="text-xs opacity-80">groups completed</span>
          </div>
        </div>
        
        <div className={`p-2 rounded border ${isSuccess ? 'bg-green-100/50 border-green-200 dark:bg-green-900/20 dark:border-green-800' : 'bg-red-100/50 border-red-200 dark:bg-red-900/20 dark:border-red-800'}`}>
          <div className="text-[10px] uppercase tracking-wider opacity-70 mb-1">Results</div>
          <div className="text-xs space-y-0.5">
            {event.failed_groups > 0 && (
              <div className="text-red-600 dark:text-red-400 font-medium">
                {event.failed_groups} failed
              </div>
            )}
            {event.canceled_groups > 0 && (
              <div className="text-amber-600 dark:text-amber-400 font-medium">
                {event.canceled_groups} canceled
              </div>
            )}
            {event.failed_groups === 0 && event.canceled_groups === 0 && (
              <div className="text-green-600 dark:text-green-400 font-medium">
                All successful
              </div>
            )}
          </div>
        </div>
      </div>
      
      {event.error && (
        <div className="mt-2 text-xs text-red-600 dark:text-red-400 bg-red-100/50 dark:bg-red-900/20 p-2 rounded border border-red-200 dark:border-red-800 font-mono whitespace-pre-wrap">
          Error: {event.error}
        </div>
      )}
      
      {event.duration && (
        <div className="mt-2 flex items-center gap-1.5 text-xs opacity-60">
          <Clock className="w-3 h-3" />
          <span>Duration: {(event.duration / 1000000000).toFixed(2)}s</span>
        </div>
      )}
    </div>
  )
}
