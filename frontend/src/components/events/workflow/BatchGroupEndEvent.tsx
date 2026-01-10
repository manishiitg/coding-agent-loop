import React, { useEffect } from 'react'
import { CheckCircle, XCircle, Layers } from 'lucide-react'
import type { BatchGroupEndEvent as BatchGroupEndEventData } from '../../../generated/event-types'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'

interface BatchGroupEndEventProps {
  event: BatchGroupEndEventData
  compact?: boolean
}

export const BatchGroupEndEvent: React.FC<BatchGroupEndEventProps> = ({ event, compact = false }) => {
  // Use consolidated handler for batch group switching (single source of truth)
  const handleBatchGroupEnd = useWorkflowStore(state => state.handleBatchGroupEnd)

  // Clear running group when this event is displayed
  useEffect(() => {
    if (event.group_id) {
      // Use consolidated handler - safely clears currentRunningGroupId only if it matches
      handleBatchGroupEnd(event.group_id)
    }
  }, [event.group_id, handleBatchGroupEnd])

  if (compact) {
    return (
      <div className={`p-2 border rounded ${event.success ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800' : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'}`}>
        <div className={`flex items-center gap-2 text-xs ${event.success ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'}`}>
          {event.success ? (
            <CheckCircle className="w-3 h-3" />
          ) : (
            <XCircle className="w-3 h-3" />
          )}
          <span className="font-medium">Group {event.group_id?.toUpperCase() || 'N/A'}</span>
          <span className={event.success ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}>
            {event.success ? 'Completed' : 'Failed'}
          </span>
        </div>
      </div>
    )
  }

  return (
    <div className={`p-3 border rounded-lg ${event.success ? 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-700' : 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-700'}`}>
      <div className="flex items-start gap-3">
        <div className="flex-shrink-0 mt-0.5">
          <div className={`w-8 h-8 rounded-full flex items-center justify-center ${event.success ? 'bg-green-100 dark:bg-green-800/50' : 'bg-red-100 dark:bg-red-800/50'}`}>
            {event.success ? (
              <CheckCircle className="w-4 h-4 text-green-600 dark:text-green-400" />
            ) : (
              <XCircle className="w-4 h-4 text-red-600 dark:text-red-400" />
            )}
          </div>
        </div>
        
        <div className="flex-1 min-w-0 space-y-2">
          {/* Header */}
          <div className="flex items-center gap-2">
            <Layers className={`w-4 h-4 ${event.success ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`} />
            <span className={`text-sm font-semibold ${event.success ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'}`}>
              Batch Group {event.success ? 'Completed' : 'Failed'}
            </span>
          </div>
          
          {/* Group Info */}
          <div className="space-y-1 text-sm">
            <div className="flex items-center gap-2">
              <span className={`font-medium ${event.success ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'}`}>Group:</span>
              <span className={`font-mono px-2 py-0.5 rounded ${event.success ? 'bg-green-100 dark:bg-green-800/50 text-green-600 dark:text-green-400' : 'bg-red-100 dark:bg-red-800/50 text-red-600 dark:text-red-400'}`}>
                {event.group_id?.toUpperCase() || 'N/A'}
              </span>
              <span className={event.success ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}>
                ({event.group_index !== undefined ? event.group_index + 1 : '?'} of {event.total_groups ?? '?'})
              </span>
            </div>
            
            {/* Progress */}
            {event.completed_steps !== undefined && event.total_steps !== undefined && (
              <div className={`text-xs ${event.success ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
                <span className="font-medium">Steps:</span>{' '}
                {event.completed_steps}/{event.total_steps} completed
              </div>
            )}
            
            {/* Error message */}
            {!event.success && event.error && (
              <div className="text-xs text-red-600 dark:text-red-400 bg-red-100 dark:bg-red-800/50 rounded p-2 mt-2">
                {event.error}
              </div>
            )}
            
            {/* Run Folder */}
            {event.run_folder && (
              <div className={`text-xs ${event.success ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
                <span className="font-medium">Run Folder:</span>{' '}
                <span className="font-mono">{event.run_folder}</span>
              </div>
            )}
            
            {/* Duration */}
            {event.duration && (
              <div className={`text-xs ${event.success ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
                <span className="font-medium">Duration:</span>{' '}
                {typeof event.duration === 'number' 
                  ? `${(event.duration / 1000).toFixed(1)}s`
                  : event.duration}
              </div>
            )}
            
            {/* Remaining groups */}
            {event.remaining_groups !== undefined && event.remaining_groups > 0 && (
              <div className={`text-xs ${event.success ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
                <span className="font-medium">Remaining:</span>{' '}
                {event.remaining_groups} group{event.remaining_groups !== 1 ? 's' : ''}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

