import React from 'react'
import { XCircle } from 'lucide-react'
import type { ContextSummarizationErrorEvent } from '../../../generated/events'

interface ContextSummarizationErrorEventDisplayProps {
  event: ContextSummarizationErrorEvent
  compact?: boolean
}

export const ContextSummarizationErrorEventDisplay: React.FC<ContextSummarizationErrorEventDisplayProps> = ({
  event,
  compact = false
}) => {
  if (compact) {
    return (
      <div className="p-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md">
        <div className="text-xs text-red-700 dark:text-red-300 flex items-center gap-2">
          <XCircle className="w-3 h-3 text-red-600" />
          <span className="font-medium">Summarization Error</span>
          {event.error && (
            <span className="text-red-600 dark:text-red-400">
              • {event.error.substring(0, 40)}...
            </span>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-700 rounded-lg">
      <div className="text-xs text-red-700 dark:text-red-300 space-y-2">
        {/* Header */}
        <div className="flex items-center gap-2">
          <XCircle className="w-4 h-4 text-red-600" />
          <span className="font-medium">Context Summarization Error</span>
        </div>

        {/* Error Message */}
        {event.error && (
          <div className="space-y-1">
            <div className="font-medium">Error:</div>
            <div className="bg-white dark:bg-gray-800 border border-red-200 dark:border-red-800 rounded-md p-2">
              <pre className="text-xs whitespace-pre-wrap text-red-700 dark:text-red-300">
                {event.error}
              </pre>
            </div>
          </div>
        )}

        {/* Statistics */}
        <div className="grid grid-cols-2 gap-2">
          {event.original_message_count !== undefined && (
            <div>
              <span className="font-medium">Original Messages:</span>
              <span className="ml-2">{event.original_message_count}</span>
            </div>
          )}
          {event.keep_last_messages !== undefined && (
            <div>
              <span className="font-medium">Keep Last:</span>
              <span className="ml-2">{event.keep_last_messages} messages</span>
            </div>
          )}
        </div>

        {/* Optional metadata */}
        {event.timestamp && (
          <div>
            <span className="font-medium">Time:</span>
            <span className="ml-2">{new Date(event.timestamp).toLocaleString()}</span>
          </div>
        )}
        {event.trace_id && (
          <div>
            <span className="font-medium">Trace ID:</span>
            <code className="text-xs bg-red-100 dark:bg-red-800 px-1 rounded ml-2">
              {event.trace_id}
            </code>
          </div>
        )}
        {event.correlation_id && (
          <div>
            <span className="font-medium">Correlation ID:</span>
            <code className="text-xs bg-red-100 dark:bg-red-800 px-1 rounded ml-2">
              {event.correlation_id}
            </code>
          </div>
        )}
      </div>
    </div>
  )
}

