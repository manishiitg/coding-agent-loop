import React from 'react'
import { AlertCircle, Hash } from 'lucide-react'
import type { ConversationErrorEvent } from '../../../generated/events'
import { formatDuration } from '../../../utils/duration'

interface ConversationErrorEventProps {
  event: ConversationErrorEvent
  compact?: boolean
}

export const ConversationErrorEventDisplay: React.FC<ConversationErrorEventProps> = ({ event, compact = false }) => {
  if (compact) {
    return (
      <div className="p-2 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-md">
        <div className="text-xs text-red-700 dark:text-red-300 space-y-1">
          <div className="flex items-center gap-2">
            <AlertCircle className="w-3 h-3 text-red-600" />
            <span className="font-medium">Conversation Error</span>
            {event.turn && <span className="text-red-600 dark:text-red-400">• Turn {event.turn}</span>}
            {event.duration && <span className="text-red-600 dark:text-red-400">• {formatDuration(event.duration)}</span>}
          </div>
          {event.error && (
            <div className="text-red-600 dark:text-red-400 mt-1 break-words">
              {event.error.length > 200 ? `${event.error.substring(0, 200)}...` : event.error}
            </div>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg">
      <div className="text-xs text-red-700 dark:text-red-300 space-y-1">
        {/* Header */}
        <div className="flex items-center gap-2">
          <AlertCircle className="w-4 h-4 text-red-600" />
          <span className="font-medium">Conversation Error</span>
        </div>
        
        {/* Question */}
        {event.question && (
          <div className="space-y-2">
            <div className="font-medium">User Message:</div>
            <div className="text-gray-600 dark:text-gray-400">
              {event.question.length > 80 ? `${event.question.substring(0, 80)}...` : event.question}
            </div>
          </div>
        )}
        
        {/* Error message */}
        {event.error && (
          <div className="bg-red-100 dark:bg-red-800 border border-red-200 dark:border-red-700 rounded-md p-2">
            <div className="font-medium">Error:</div>
            <div className="mt-1 text-red-800 dark:text-red-200">{event.error}</div>
          </div>
        )}
        
        {/* Context */}
        {event.context && (
          <div className="bg-red-100 dark:bg-red-800 border border-red-200 dark:border-red-700 rounded-md p-2">
            <div className="font-medium">Context:</div>
            <div className="mt-1 text-red-800 dark:text-red-200">{event.context}</div>
          </div>
        )}
        
        {/* Turn information */}
        {event.turn && (
          <div className="flex items-center gap-2">
            <Hash className="w-3 h-3 text-red-600" />
            <span>Turn: {event.turn}</span>
          </div>
        )}
        
        {/* Duration */}
        {event.duration && (
          <div><strong>Duration:</strong> {formatDuration(event.duration)}</div>
        )}
        
        {/* Optional metadata fields */}
        {event.timestamp && <div><strong>Timestamp:</strong> {new Date(event.timestamp).toLocaleString()}</div>}
        {event.trace_id && <div><strong>Trace ID:</strong> <code className="text-xs bg-red-100 dark:bg-red-800 px-1 rounded">{event.trace_id}</code></div>}
        {event.correlation_id && <div><strong>Correlation ID:</strong> <code className="text-xs bg-red-100 dark:bg-red-800 px-1 rounded">{event.correlation_id}</code></div>}
      </div>
    </div>
  )
}
