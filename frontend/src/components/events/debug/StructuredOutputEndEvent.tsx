import React from 'react'
import type { StructuredOutputEndEvent } from '../../../generated/events'
import { formatDuration } from '../../../utils/duration'

interface StructuredOutputEndEventDisplayProps {
  event: StructuredOutputEndEvent
}

export const StructuredOutputEndEventDisplay: React.FC<StructuredOutputEndEventDisplayProps> = ({ event }) => {
  const hasError = event.error && event.error.length > 0
  
  return (
    <div className={`${hasError ? 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800' : 'bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800'} border rounded p-2`}>
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <span className={`text-sm font-medium ${hasError ? 'text-red-700 dark:text-red-300' : 'text-green-700 dark:text-green-300'}`}>
            {hasError ? '❌ Structured Output End (Error)' : '✅ Structured Output End'}
          </span>
          {event.operation && (
            <span className={`text-xs ${hasError ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'}`}>
              • Operation: {event.operation}
            </span>
          )}
          {event.event_type && (
            <span className={`text-xs ${hasError ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'}`}>
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
        {event.duration && (
          <span className={`text-xs ${hasError ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'}`}>
            Duration: {formatDuration(event.duration)}
          </span>
        )}
        {event.component && (
          <span className={`text-xs ${hasError ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'}`}>
            • Component: {event.component}
          </span>
        )}
        {event.session_id && (
          <span className={`text-xs ${hasError ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'}`}>
            • Session: {event.session_id.slice(0, 8)}...
          </span>
        )}
      </div>
      
      {hasError && (
        <div className="mt-2 p-2 bg-red-100 dark:bg-red-800/30 rounded text-xs text-red-700 dark:text-red-300">
          <strong>Error:</strong> {event.error}
        </div>
      )}
    </div>
  )
}
