import React from 'react'
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer'

interface UnifiedCompletionEvent {
  timestamp?: string
  trace_id?: string
  span_id?: string
  event_id?: string
  parent_id?: string
  is_end_event?: boolean
  correlation_id?: string
  agent_type?: string
  agent_mode?: string
  question?: string
  final_result?: string
  status?: string
  duration?: number
  turns?: number
  error?: string
  metadata?: Record<string, unknown>
}

interface UnifiedCompletionEventDisplayProps {
  event: UnifiedCompletionEvent
}

export const UnifiedCompletionEventDisplay: React.FC<UnifiedCompletionEventDisplayProps> = ({ event }) => {

  // Note: event.duration is in nanoseconds from Go time.Duration
  const formatDuration = (durationNs: number) => {
    if (!durationNs || durationNs <= 0) {
      return '0ms'
    }

    // Convert nanoseconds to milliseconds
    const durationMs = durationNs / 1000000

    if (durationMs < 1) {
      // Less than 1ms, show in microseconds
      const durationUs = durationNs / 1000
      return `${Math.round(durationUs)}μs`
    } else if (durationMs < 1000) {
      // Less than 1ms, show in milliseconds
      return `${Math.round(durationMs)}ms`
    } else if (durationMs < 60000) {
      // Less than 1 minute, show in seconds
      return `${(durationMs / 1000).toFixed(1)}s`
    } else {
      // 1 minute or more, show in minutes
      return `${(durationMs / 60000).toFixed(1)}m`
    }
  }

  // Render final result with JSON detection
  const renderFinalResult = (result: string) => {
    try {
      // Try to parse as JSON
      const parsed = JSON.parse(result)
      
      // If successful, render as formatted JSON
      return (
        <div className="bg-gray-100 dark:bg-gray-700 rounded-md p-2">
          <div className="text-xs text-gray-600 dark:text-gray-400 mb-1 font-medium">
            📄 JSON Result
          </div>
          <pre className="text-xs text-gray-800 dark:text-gray-200 overflow-x-auto whitespace-pre-wrap">
            {JSON.stringify(parsed, null, 2)}
          </pre>
        </div>
      )
    } catch {
      // If not valid JSON, render as markdown
      return (
        <div className="bg-white dark:bg-gray-800 rounded-md p-2">
          <ConversationMarkdownRenderer content={result} />
        </div>
      )
    }
  }

  // Determine if this is an error event
  const isError = event.status === 'error' || event.error

  // Single-line layout following design guidelines
  return (
    <div className={`${isError ? 'bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800' : 'bg-gray-50 dark:bg-gray-900/20 border-gray-200 dark:border-gray-800'} border rounded p-2`}>
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className={`text-sm font-medium ${isError ? 'text-red-700 dark:text-red-300' : 'text-gray-700 dark:text-gray-300'}`}>
              {isError ? '❌' : '✅'} Unified Completion{' '}
              <span className={`text-xs font-normal ${isError ? 'text-red-600 dark:text-red-400' : 'text-gray-600 dark:text-gray-400'}`}>
                {event.agent_mode && `• Mode: ${event.agent_mode}`}
                {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
                {event.turns && ` • Turn: ${event.turns}`}
                {event.status && ` • ${event.status}`}
              </span>
            </div>
          </div>
        </div>

        {/* Right side: Time */}
        {event.timestamp && (
          <div className={`text-xs flex-shrink-0 ${isError ? 'text-red-600 dark:text-red-400' : 'text-gray-600 dark:text-gray-400'}`}>
            {new Date(event.timestamp).toLocaleTimeString()}
          </div>
        )}
      </div>

      {/* Error message - always visible for error status */}
      {isError && event.error && (
        <div className="mt-2">
          <div className={`${isError ? 'bg-red-100 dark:bg-red-800' : 'bg-gray-100 dark:bg-gray-700'} border ${isError ? 'border-red-200 dark:border-red-700' : 'border-gray-200 dark:border-gray-600'} rounded-md p-2`}>
            <div className={`text-xs font-medium mb-1 ${isError ? 'text-red-800 dark:text-red-200' : 'text-gray-800 dark:text-gray-200'}`}>
              Error:
            </div>
            <div className={`text-sm ${isError ? 'text-red-900 dark:text-red-100' : 'text-gray-900 dark:text-gray-100'} whitespace-pre-wrap break-words`}>
              {event.error}
            </div>
          </div>
        </div>
      )}

      {/* Result always visible below (for non-error cases) */}
      {!isError && event.final_result && (
        <div className="mt-2">
          {renderFinalResult(event.final_result)}
        </div>
      )}
    </div>
  )
}
