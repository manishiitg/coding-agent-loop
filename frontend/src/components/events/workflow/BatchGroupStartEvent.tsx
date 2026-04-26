import React from 'react'
import { Play, Layers } from 'lucide-react'
import type { BatchGroupStartEvent as BatchGroupStartEventData } from '../../../generated/event-types'

type BatchGroupStartDisplayEvent = BatchGroupStartEventData & {
  group_name?: string
  group_id?: string
}

interface BatchGroupStartEventProps {
  event: BatchGroupStartEventData
  compact?: boolean
}

// NOTE: Batch progress updates are handled in the event polling layer (ChatArea.tsx)
// to ensure reliable updates even when events are filtered or not visible in UI.
// This component is purely for display purposes.
export const BatchGroupStartEvent: React.FC<BatchGroupStartEventProps> = ({ event, compact = false }) => {
  const display = event as BatchGroupStartDisplayEvent
  const groupLabel = (display.group_name ?? display.group_id ?? 'N/A').toUpperCase()

  if (compact) {
    return (
      <div className="p-2 bg-white dark:bg-gray-800/40 border border-blue-200 dark:border-blue-900/30 rounded">
        <div className="flex items-center gap-2 text-xs text-gray-700 dark:text-gray-300">
          <Play className="w-3 h-3 text-blue-600 dark:text-blue-400" />
          <span className="font-medium">Group {groupLabel}</span>
          <span className="text-gray-500 dark:text-gray-400">
            ({event.group_index !== undefined ? event.group_index + 1 : '?'}/{event.total_groups ?? '?'})
          </span>
        </div>
      </div>
    )
  }

  return (
    <div className="border-l-2 border-blue-500 pl-3 py-3 bg-white dark:bg-gray-800/40 rounded-r-md text-sm">
      <div className="flex items-center gap-2 mb-2">
        <div className="p-1 bg-blue-100 dark:bg-blue-900/20 rounded-md">
          <Play className="w-3.5 h-3.5 text-blue-600 dark:text-blue-400" />
        </div>
        <div className="flex-1">
          <div className="font-bold text-gray-900 dark:text-gray-100 flex items-center gap-2">
            Batch Group Started
            <span className="text-[10px] font-normal bg-blue-100 dark:bg-blue-900/20 px-1.5 py-0.5 rounded text-blue-700 dark:text-blue-300 border border-blue-200 dark:border-blue-900/30 font-mono">
              {groupLabel}
            </span>
          </div>
          <div className="text-[10px] text-gray-500 dark:text-gray-400 mt-0.5">
            Group {event.group_index !== undefined ? event.group_index + 1 : '?'} of {event.total_groups ?? '?'}
          </div>
        </div>
      </div>
            
      {/* Variable Values */}
      {event.variable_values && Object.keys(event.variable_values).length > 0 && (
        <div className="mt-2 text-xs text-gray-600 dark:text-gray-300 bg-gray-50 dark:bg-gray-900/50 p-2.5 rounded border border-gray-200 dark:border-gray-700/50">
          <div className="font-medium mb-1.5 text-[10px] uppercase tracking-wider opacity-70 flex items-center gap-1.5">
            <span className="w-1 h-1 rounded-full bg-blue-400 dark:bg-blue-500"></span>
            Variable Values
          </div>
          <div className="space-y-1">
            {Object.entries(event.variable_values).map(([key, value]) => (
              <div key={key} className="flex items-start gap-2 text-[11px]">
                <span className="font-mono text-gray-700 dark:text-gray-300 font-semibold min-w-[80px]">{key}:</span>
                <span className="text-gray-600 dark:text-gray-400 break-words flex-1 bg-white dark:bg-gray-950/30 px-1 rounded">
                  {value || '(empty)'}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
      
      {/* Run Folder */}
      {event.run_folder && (
        <div className="mt-2 flex items-center gap-1.5 text-[10px] text-gray-500 dark:text-gray-400 opacity-80 pl-1">
          <Layers className="w-3 h-3" />
          <span className="font-mono">{event.run_folder}</span>
        </div>
      )}
    </div>
  )
}
