import React from 'react'
import type { 
  BatchExecutionStartEvent, 
  BatchExecutionEndEvent,
  BatchExecutionCanceledEvent
} from '../../../generated/event-types'
import { Layers, CheckCircle, XCircle, Clock, Play, AlertCircle, Ban } from 'lucide-react'

interface BatchExecutionEventProps<T> {
  event: T
  compact?: boolean
}

export const BatchExecutionStartEventDisplay: React.FC<BatchExecutionEventProps<BatchExecutionStartEvent>> = ({ event, compact }) => {
  return (
    <div className={`border-l-2 border-blue-500 pl-3 py-3 bg-white dark:bg-gray-800/40 rounded-r-md ${compact ? 'text-xs' : 'text-sm'}`}>
      <div className="flex items-center gap-2 mb-2">
        <div className="p-1 bg-blue-100 dark:bg-blue-900/20 rounded-md">
          <Layers className="w-4 h-4 text-blue-600 dark:text-blue-400" />
        </div>
        <div className="flex-1">
          <div className="font-bold text-gray-900 dark:text-gray-100">
            Batch Execution Started
          </div>
          <div className="text-[10px] text-gray-500 dark:text-gray-400 mt-0.5 font-mono">
            Iteration {event.iteration_number}
          </div>
        </div>
      </div>
      
      <div className="flex flex-col gap-2 mt-2">
        <div className="flex items-center gap-2 p-2 bg-gray-50 dark:bg-gray-900/50 rounded border border-gray-200 dark:border-gray-700/50">
          <Play className="w-3.5 h-3.5 text-blue-600 dark:text-blue-400" />
          <div className="flex-1 text-gray-700 dark:text-gray-300 font-medium">
            Running {event.total_groups} {event.total_groups === 1 ? 'group' : 'groups'}
          </div>
        </div>
        
        {event.execution_options && !compact && Object.keys(event.execution_options).length > 0 && (
          <div className="mt-1 text-xs text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-gray-900/50 p-2.5 rounded border border-gray-200 dark:border-gray-700/50">
            <div className="font-medium mb-1.5 text-[10px] uppercase tracking-wider opacity-70 flex items-center gap-1.5">
              <span className="w-1 h-1 rounded-full bg-blue-400 dark:bg-blue-500"></span>
              Execution Options
            </div>
            <pre className="whitespace-pre-wrap font-mono text-[10px] bg-white dark:bg-gray-950/50 p-1.5 rounded text-gray-600 dark:text-gray-300 border border-gray-100 dark:border-gray-800">
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
  const hasFailures = (event.failed_groups ?? 0) > 0
  const hasCancellations = (event.canceled_groups ?? 0) > 0
  
  // Determine color scheme based on outcome
  let colorClass = 'border-green-500 bg-white dark:bg-gray-800/40'
  let iconColor = 'text-green-600 dark:text-green-400'
  let bgIconColor = 'bg-green-100 dark:bg-green-900/20'
  let titleColor = 'text-gray-900 dark:text-gray-100'
  
  if (hasFailures) {
    colorClass = 'border-red-500 bg-white dark:bg-gray-800/40'
    iconColor = 'text-red-600 dark:text-red-400'
    bgIconColor = 'bg-red-100 dark:bg-red-900/20'
    titleColor = 'text-gray-900 dark:text-gray-100'
  } else if (hasCancellations && !isSuccess) {
    colorClass = 'border-blue-500 bg-white dark:bg-gray-800/40'
    iconColor = 'text-blue-600 dark:text-blue-400'
    bgIconColor = 'bg-blue-100 dark:bg-blue-900/20'
    titleColor = 'text-gray-900 dark:text-gray-100'
  }
  
  return (
    <div className={`border-l-2 ${colorClass} pl-3 py-3 rounded-r-md ${compact ? 'text-xs' : 'text-sm'}`}>
      <div className="flex items-center gap-2 mb-3">
        <div className={`p-1 ${bgIconColor} rounded-md`}>
          {isSuccess ? (
            <CheckCircle className={`w-4 h-4 ${iconColor}`} />
          ) : hasFailures ? (
            <XCircle className={`w-4 h-4 ${iconColor}`} />
          ) : (
            <Ban className={`w-4 h-4 ${iconColor}`} />
          )}
        </div>
        <div className="flex-1">
          <span className={`font-bold ${titleColor}`}>
            Batch Execution {isSuccess ? 'Completed' : hasFailures ? 'Failed' : 'Canceled'}
          </span>
          <div className="text-[10px] text-gray-500 dark:text-gray-400 mt-0.5 font-mono">
            Iteration {event.iteration_number}
          </div>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-2 mt-2">
        <div className={`p-2.5 rounded bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700/50`}>
          <div className="text-[10px] uppercase tracking-wider opacity-60 mb-1 font-semibold dark:text-gray-400">Progress</div>
          <div className="font-medium flex items-center gap-2 dark:text-gray-200">
            <span className="text-lg">{event.completed_groups}</span>
            <span className="text-xs opacity-70">/ {event.total_groups} groups done</span>
          </div>
        </div>
        
        <div className={`p-2.5 rounded bg-gray-50 dark:bg-gray-900/50 border border-gray-200 dark:border-gray-700/50`}>
          <div className="text-[10px] uppercase tracking-wider opacity-60 mb-1 font-semibold dark:text-gray-400">Outcome</div>
          <div className="text-xs space-y-1">
            {(event.failed_groups ?? 0) > 0 && (
              <div className="text-red-600 dark:text-red-400 font-bold flex items-center gap-1.5">
                <XCircle className="w-3 h-3" />
                {event.failed_groups} failed
              </div>
            )}
            {(event.canceled_groups ?? 0) > 0 && (
              <div className="text-blue-600 dark:text-blue-400 font-bold flex items-center gap-1.5">
                <Ban className="w-3 h-3" />
                {event.canceled_groups} canceled
              </div>
            )}
            {(event.failed_groups ?? 0) === 0 && (event.canceled_groups ?? 0) === 0 && (
              <div className="text-green-600 dark:text-green-400 font-bold flex items-center gap-1.5">
                <CheckCircle className="w-3 h-3" />
                All successful
              </div>
            )}
          </div>
        </div>
      </div>
      
      {event.error && (
        <div className="mt-3 text-xs text-red-800 dark:text-red-300 bg-red-50 dark:bg-red-900/10 p-2.5 rounded border border-red-200 dark:border-red-900/30">
          <div className="font-bold mb-1 flex items-center gap-1.5">
            <AlertCircle className="w-3.5 h-3.5" />
            Error Details
          </div>
          <div className="font-mono whitespace-pre-wrap opacity-90 text-[11px] leading-relaxed">
            {event.error}
          </div>
        </div>
      )}
      
      {event.duration && (
        <div className="mt-2.5 flex items-center gap-1.5 text-xs opacity-60 font-mono pl-1 dark:text-gray-400">
          <Clock className="w-3 h-3" />
          <span>Duration: {(event.duration / 1000000000).toFixed(2)}s</span>
        </div>
      )}
    </div>
  )
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
