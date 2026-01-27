import React from 'react'
import { CheckCircle, XCircle, Layers, Clock, AlertCircle } from 'lucide-react'
import type { BatchGroupEndEvent as BatchGroupEndEventData } from '../../../generated/event-types'

interface BatchGroupEndEventProps {
  event: BatchGroupEndEventData
  compact?: boolean
}

// NOTE: Batch progress updates are handled in the event polling layer (ChatArea.tsx)
// to ensure reliable updates even when events are filtered or not visible in UI.
// This component is purely for display purposes.
export const BatchGroupEndEvent: React.FC<BatchGroupEndEventProps> = ({ event, compact = false }) => {
  const isSuccess = event.success

  if (compact) {
    return (
      <div className={`p-2 border rounded ${isSuccess ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800' : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'}`}>
        <div className={`flex items-center gap-2 text-xs ${isSuccess ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'}`}>
          {isSuccess ? (
            <CheckCircle className="w-3 h-3" />
          ) : (
            <XCircle className="w-3 h-3" />
          )}
          <span className="font-medium">Group {event.group_id?.toUpperCase() || 'N/A'}</span>
          <span className={isSuccess ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}>
            {isSuccess ? 'Completed' : 'Failed'}
          </span>
        </div>
      </div>
    )
  }

  const colorClass = isSuccess ? 'border-green-500 bg-green-50/30 dark:bg-green-900/20' : 'border-red-500 bg-red-50/30 dark:bg-red-900/20'
  const iconColor = isSuccess ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'
  const bgIconColor = isSuccess ? 'bg-green-100 dark:bg-green-800/60' : 'bg-red-100 dark:bg-red-800/60'
  const titleColor = isSuccess ? 'text-green-900 dark:text-green-100' : 'text-red-900 dark:text-red-100'
  const borderColor = isSuccess ? 'border-green-100 dark:border-green-800/30' : 'border-red-100 dark:border-red-800/30'

  return (
    <div className={`border-l-2 ${colorClass} pl-3 py-3 rounded-r-md text-sm`}>
      <div className="flex items-center gap-2 mb-2">
        <div className={`p-1 ${bgIconColor} rounded-md`}>
          {isSuccess ? (
            <CheckCircle className={`w-4 h-4 ${iconColor}`} />
          ) : (
            <XCircle className={`w-4 h-4 ${iconColor}`} />
          )}
        </div>
        <div className="flex-1">
          <div className={`font-bold ${titleColor} flex items-center gap-2`}>
            Batch Group {isSuccess ? 'Completed' : 'Failed'}
            <span className={`text-[10px] font-normal ${bgIconColor} px-1.5 py-0.5 rounded ${titleColor} opacity-80 border border-transparent font-mono`}>
              {event.group_id?.toUpperCase() || 'N/A'}
            </span>
          </div>
          <div className={`text-[10px] ${titleColor} opacity-70 mt-0.5`}>
            Group {event.group_index !== undefined ? event.group_index + 1 : '?'} of {event.total_groups ?? '?'}
          </div>
        </div>
      </div>
      
      {/* Progress */}
      {event.completed_steps !== undefined && event.total_steps !== undefined && (
        <div className={`mt-2 p-2 rounded bg-white/60 dark:bg-gray-800/40 border ${borderColor} flex items-center justify-between`}>
          <div className="text-xs font-medium opacity-80 dark:text-gray-300">Steps Completed</div>
          <div className="text-sm font-bold font-mono dark:text-gray-200">
            {event.completed_steps} <span className="opacity-60 text-xs font-normal">/ {event.total_steps}</span>
          </div>
        </div>
      )}
      
      {/* Error message */}
      {!isSuccess && event.error && (
        <div className="mt-3 text-xs text-red-700 dark:text-red-300 bg-red-100/50 dark:bg-red-900/30 p-2.5 rounded border border-red-200 dark:border-red-800/50">
          <div className="font-bold mb-1 flex items-center gap-1.5">
            <AlertCircle className="w-3.5 h-3.5" />
            Error Details
          </div>
          <div className="font-mono whitespace-pre-wrap opacity-90 text-[11px] leading-relaxed">
            {event.error}
          </div>
        </div>
      )}
      
      {/* Metadata Footer */}
      <div className="mt-3 flex items-center gap-4 text-[10px] opacity-60 pl-1 dark:text-gray-400">
        {event.duration && (
          <div className="flex items-center gap-1.5">
            <Clock className="w-3 h-3" />
            <span className="font-mono">
              {typeof event.duration === 'number' 
                ? `${(event.duration / 1000).toFixed(1)}s`
                : event.duration}
            </span>
          </div>
        )}
        
        {event.remaining_groups !== undefined && event.remaining_groups > 0 && (
          <div className="flex items-center gap-1.5">
            <Layers className="w-3 h-3" />
            <span>{event.remaining_groups} more pending</span>
          </div>
        )}
      </div>
    </div>
  )
}
