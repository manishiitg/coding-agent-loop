import React from 'react'
import { Loader2 } from 'lucide-react'
import type { ContextSummarizationStartedEvent } from '../../../generated/events'

interface ContextSummarizationStartedEventDisplayProps {
  event: ContextSummarizationStartedEvent
  compact?: boolean
}

export const ContextSummarizationStartedEventDisplay: React.FC<ContextSummarizationStartedEventDisplayProps> = ({
  event,
  compact = false
}) => {
  if (compact) {
    return (
      <div className="p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md">
        <div className="text-xs text-blue-700 dark:text-blue-300 flex items-center gap-2">
          <Loader2 className="w-3 h-3 text-blue-600 animate-spin" />
          <span className="font-medium">Summarization Started</span>
          {event.original_message_count !== undefined && (
            <span className="text-blue-600 dark:text-blue-400">
              • {event.original_message_count} messages
            </span>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md">
      <div className="text-xs text-blue-700 dark:text-blue-300 space-y-2">
        {/* Header */}
        <div className="flex items-center gap-2">
          <Loader2 className="w-3 h-3 text-blue-600 animate-spin" />
          <span className="font-medium">Context Summarization Started</span>
        </div>

        {/* Statistics */}
        {(event.original_message_count !== undefined || event.keep_last_messages !== undefined) && (
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
        )}

        {/* Optional metadata */}
        {event.timestamp && (
          <div>
            <span className="font-medium">Time:</span>
            <span className="ml-2">{new Date(event.timestamp).toLocaleString()}</span>
          </div>
        )}
        {event.component && (
          <div>
            <span className="font-medium">Component:</span>
            <span className="ml-2">{event.component}</span>
          </div>
        )}
      </div>
    </div>
  )
}

