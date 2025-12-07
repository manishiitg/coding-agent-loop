import React, { useEffect } from 'react'
import { Play, Layers } from 'lucide-react'
import type { BatchGroupStartEvent as BatchGroupStartEventData } from '../../../generated/event-types'
import { useWorkflowStore } from '../../../stores/useWorkflowStore'

interface BatchGroupStartEventProps {
  event: BatchGroupStartEventData
  compact?: boolean
}

export const BatchGroupStartEvent: React.FC<BatchGroupStartEventProps> = ({ event, compact = false }) => {
  const setCurrentRunningGroupId = useWorkflowStore(state => state.setCurrentRunningGroupId)

  // Update store when this event is displayed
  useEffect(() => {
    if (event.group_id) {
      setCurrentRunningGroupId(event.group_id)
    }
    // Clear when component unmounts (event is replaced)
    return () => {
      // Don't clear here - let batch_group_end handle it
    }
  }, [event.group_id, setCurrentRunningGroupId])
  if (compact) {
    return (
      <div className="p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded">
        <div className="flex items-center gap-2 text-xs text-blue-700 dark:text-blue-300">
          <Play className="w-3 h-3" />
          <span className="font-medium">Group {event.group_id?.toUpperCase() || 'N/A'}</span>
          <span className="text-blue-600 dark:text-blue-400">
            ({event.group_index !== undefined ? event.group_index + 1 : '?'}/{event.total_groups ?? '?'})
          </span>
        </div>
      </div>
    )
  }

  return (
    <div className="p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-700 rounded-lg">
      <div className="flex items-start gap-3">
        <div className="flex-shrink-0 mt-0.5">
          <div className="w-8 h-8 rounded-full bg-blue-100 dark:bg-blue-800/50 flex items-center justify-center">
            <Layers className="w-4 h-4 text-blue-600 dark:text-blue-400" />
          </div>
        </div>
        
        <div className="flex-1 min-w-0 space-y-2">
          {/* Header */}
          <div className="flex items-center gap-2">
            <Play className="w-4 h-4 text-blue-600 dark:text-blue-400" />
            <span className="text-sm font-semibold text-blue-700 dark:text-blue-300">
              Batch Group Started
            </span>
          </div>
          
          {/* Group Info */}
          <div className="space-y-1 text-sm">
            <div className="flex items-center gap-2">
              <span className="font-medium text-blue-700 dark:text-blue-300">Group:</span>
              <span className="font-mono text-blue-600 dark:text-blue-400 bg-blue-100 dark:bg-blue-800/50 px-2 py-0.5 rounded">
                {event.group_id?.toUpperCase() || 'N/A'}
              </span>
              <span className="text-blue-600 dark:text-blue-400">
                ({event.group_index !== undefined ? event.group_index + 1 : '?'} of {event.total_groups ?? '?'})
              </span>
            </div>
            
            {/* Variable Values */}
            {event.variable_values && Object.keys(event.variable_values).length > 0 && (
              <div className="mt-2 space-y-1">
                <div className="text-xs font-medium text-blue-600 dark:text-blue-400">Variable Values:</div>
                <div className="bg-white dark:bg-gray-800 border border-blue-200 dark:border-blue-700 rounded p-2 space-y-1">
                  {Object.entries(event.variable_values).map(([key, value]) => (
                    <div key={key} className="flex items-start gap-2 text-xs">
                      <span className="font-mono text-blue-700 dark:text-blue-300 font-medium">{key}:</span>
                      <span className="text-blue-600 dark:text-blue-400 break-words">{value || '(empty)'}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}
            
            {/* Run Folder */}
            {event.run_folder && (
              <div className="text-xs text-blue-600 dark:text-blue-400">
                <span className="font-medium">Run Folder:</span>{' '}
                <span className="font-mono">{event.run_folder}</span>
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

