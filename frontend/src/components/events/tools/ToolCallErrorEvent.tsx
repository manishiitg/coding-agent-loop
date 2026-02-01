import React from 'react'
import type { ToolCallErrorEvent } from '../../../generated/events'
import { formatDuration } from '../../../utils/duration'
import { useExpandable } from '../useExpandable'
import { Plus, Minus } from 'lucide-react'

interface ToolCallErrorEventDisplayProps {
  event: ToolCallErrorEvent
}

export const ToolCallErrorEventDisplay: React.FC<ToolCallErrorEventDisplayProps> = ({ event }) => {
  const { isExpanded, toggle } = useExpandable()

  return (
    <div className="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-2">
      <div className="flex items-center justify-between gap-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-red-700 dark:text-red-300">
              ❌ Tool Call Error
            </span>
            {event.tool_name && (
              <span className="text-xs text-red-600 dark:text-red-400">
                • {event.tool_name}
              </span>
            )}
            {event.turn && (
              <span className="text-xs text-red-600 dark:text-red-400">
                • Turn {event.turn}
              </span>
            )}
            {event.duration && (
              <span className="text-xs text-red-600 dark:text-red-400">
                • {formatDuration(event.duration)}
              </span>
            )}
          </div>
        </div>

        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <div className="text-xs text-red-600 dark:text-red-400">
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
          <button
            onClick={toggle}
            className="p-0.5 hover:bg-red-200 dark:hover:bg-red-800 rounded text-red-700 dark:text-red-300 transition-colors"
            title={isExpanded ? "Collapse error details (Alt+Click for all)" : "Expand error details (Alt+Click for all)"}
          >
            {isExpanded ? <Minus className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
          </button>
        </div>
      </div>
      
      {isExpanded && (
        <div className="mt-2 space-y-2">
          {event.server_name && (
            <div className="text-xs text-red-600 dark:text-red-400">
              <span className="font-medium">Server:</span> {event.server_name}
            </div>
          )}
          
          {/* Error details */}
          {event.error && (
            <div className="mt-1 p-2 bg-white dark:bg-red-950/30 border border-red-200 dark:border-red-800 rounded text-xs text-red-800 dark:text-red-200 font-mono whitespace-pre-wrap break-words">
              {event.error}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
