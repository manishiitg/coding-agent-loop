import React from 'react'
import { CheckCircle2 } from 'lucide-react'
import type { ContextSummarizationCompletedEvent } from '../../../generated/events'

interface ContextSummarizationCompletedEventDisplayProps {
  event: ContextSummarizationCompletedEvent
  compact?: boolean
}

export const ContextSummarizationCompletedEventDisplay: React.FC<ContextSummarizationCompletedEventDisplayProps> = ({
  event
}) => {
  // Extract token information
  const promptTokens = event.prompt_tokens ?? 0
  const completionTokens = event.completion_tokens ?? 0
  const cacheTokens = event.cache_tokens ?? 0
  const reasoningTokens = event.reasoning_tokens ?? 0
  const totalTokens = event.total_tokens ?? 0

  // Extract context usage from metadata
  const contextUsagePercent = event.metadata?.context_usage_percent as number | undefined

  const hasTokens = promptTokens > 0 || completionTokens > 0 || totalTokens > 0 || cacheTokens > 0 || reasoningTokens > 0

  return (
    <div className="p-2 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-md">
      <div className="text-xs text-green-700 dark:text-green-300 space-y-2">
        {/* Header */}
        <div className="flex items-center gap-2">
          <CheckCircle2 className="w-3 h-3 text-green-600" />
          <span className="font-medium">Context Summarization Completed</span>
        </div>

        {/* Token Usage - Single line format */}
        {hasTokens && (
          <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
            <span className="font-semibold">Tokens:</span>
            {promptTokens > 0 && (
              <span>
                In:{promptTokens.toLocaleString()}
              </span>
            )}
            {cacheTokens > 0 && (
              <span className="text-cyan-600 dark:text-cyan-400">
                Cache:{cacheTokens.toLocaleString()}
              </span>
            )}
            {reasoningTokens > 0 && (
              <span className="text-purple-600 dark:text-purple-400">
                Reason:{reasoningTokens.toLocaleString()}
              </span>
            )}
            {completionTokens > 0 && (
              <span>
                Out:{completionTokens.toLocaleString()}
              </span>
            )}
            {contextUsagePercent !== undefined && contextUsagePercent > 0 && (
              <span className={contextUsagePercent > 80 ? 'text-red-600 dark:text-red-400' : contextUsagePercent > 50 ? 'text-yellow-600 dark:text-yellow-400' : 'text-green-600 dark:text-green-400'}>
                Context:{contextUsagePercent.toFixed(1)}%
              </span>
            )}
          </div>
        )}

        {/* Summary Content */}
        {event.summary && (
          <div className="space-y-1">
            <div className="font-medium">Summary:</div>
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2 max-h-48 overflow-y-auto">
              <pre className="text-xs whitespace-pre-wrap text-gray-700 dark:text-gray-300">
                {event.summary}
              </pre>
            </div>
          </div>
        )}

        {/* Statistics */}
        {(event.original_message_count !== undefined || event.new_message_count !== undefined || event.old_messages_count !== undefined || event.recent_messages_count !== undefined) && (
          <div className="grid grid-cols-2 gap-2 text-xs">
            {event.original_message_count !== undefined && (
              <div>
                <span className="font-medium">Original Messages:</span>
                <span className="ml-2">{event.original_message_count}</span>
              </div>
            )}
            {event.new_message_count !== undefined && (
              <div>
                <span className="font-medium">New Messages:</span>
                <span className="ml-2">{event.new_message_count}</span>
              </div>
            )}
            {event.old_messages_count !== undefined && (
              <div>
                <span className="font-medium">Old Messages:</span>
                <span className="ml-2">{event.old_messages_count}</span>
              </div>
            )}
            {event.recent_messages_count !== undefined && (
              <div>
                <span className="font-medium">Recent Messages:</span>
                <span className="ml-2">{event.recent_messages_count}</span>
              </div>
            )}
          </div>
        )}

        {/* Optional metadata */}
        {event.timestamp && (
          <div className="text-xs">
            <span className="font-medium">Time:</span>
            <span className="ml-2">{new Date(event.timestamp).toLocaleString()}</span>
          </div>
        )}
      </div>
    </div>
  )
}

