import React from 'react'
import type { StructuredOutputStartEvent } from '../../../generated/events'

interface StructuredOutputStartEventDisplayProps {
  event: StructuredOutputStartEvent
}

export const StructuredOutputStartEventDisplay: React.FC<StructuredOutputStartEventDisplayProps> = ({ event }) => {
  return (
    <div className="bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 rounded p-2">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-purple-700 dark:text-purple-300">
            🔧 Structured Output Start
          </span>
          {event.operation && (
            <span className="text-xs text-purple-600 dark:text-purple-400">
              • Operation: {event.operation}
            </span>
          )}
          {event.event_type && (
            <span className="text-xs text-purple-600 dark:text-purple-400">
              • Type: {event.event_type}
            </span>
          )}
        </div>
        {event.timestamp && (
          <span className="text-xs text-gray-500 dark:text-gray-400">
            {new Date(event.timestamp).toLocaleTimeString()}
          </span>
        )}
      </div>
      
      <div className="flex items-center gap-2 mt-1">
        {event.component && (
          <span className="text-xs text-purple-600 dark:text-purple-400">
            Component: {event.component}
          </span>
        )}
        {event.session_id && (
          <span className="text-xs text-purple-600 dark:text-purple-400">
            • Session: {event.session_id.slice(0, 8)}...
          </span>
        )}
        {event.trace_id && (
          <span className="text-xs text-purple-600 dark:text-purple-400">
            • Trace: {event.trace_id.slice(0, 8)}...
          </span>
        )}
      </div>
    </div>
  )
}
