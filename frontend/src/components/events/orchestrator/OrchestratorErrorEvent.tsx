import React from 'react'
import type { OrchestratorErrorEvent } from '../../../generated/events'
import { formatDuration } from '../../../utils/duration'
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer'

interface OrchestratorErrorEventDisplayProps {
  event: OrchestratorErrorEvent
}

export const OrchestratorErrorEventDisplay: React.FC<OrchestratorErrorEventDisplayProps> = ({
  event
}) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return '';
    return new Date(timestamp).toLocaleTimeString();
  };

  return (
    <div className="p-2 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-yellow-700 dark:text-yellow-300">
              ❌ Deep Search Error{' '}
              <span className="text-xs font-normal text-yellow-600 dark:text-yellow-400">
                | Duration: {formatDuration(event.duration || 0)}
                {event.execution_mode && ` | Mode: ${event.execution_mode === 'parallel_execution' ? 'Parallel' : 'Sequential'}`}
                {event.context && ` | Context: ${event.context.length > 20 ? `${event.context.substring(0, 20)}...` : event.context}`}
              </span>
            </div>
          </div>
        </div>
        
        {/* Right side: Time */}
        {event.timestamp && (
          <div className="text-xs text-yellow-600 dark:text-yellow-400 flex-shrink-0">
            {formatTimestamp(event.timestamp)}
          </div>
        )}
      </div>

      {/* Error content - always visible with markdown rendering */}
      {event.error && (
        <div className="mt-3">
          <div className="text-xs font-medium text-yellow-600 dark:text-yellow-400 mb-2">Error Details:</div>
          <ConversationMarkdownRenderer content={event.error} maxHeight="400px" />
        </div>
      )}

      {/* Context info - always visible with markdown rendering */}
      {event.context && (
        <div className="mt-3">
          <div className="text-xs font-medium text-yellow-600 dark:text-yellow-400 mb-2">Context:</div>
          <ConversationMarkdownRenderer content={event.context} maxHeight="400px" />
        </div>
      )}

      {/* Trace information */}
      {(event.trace_id || event.span_id) && (
        <div className="mt-3">
          <div className="text-xs font-medium text-yellow-600 dark:text-yellow-400 mb-2">Trace Information:</div>
          <div className="text-xs text-yellow-600 dark:text-yellow-400 bg-yellow-100 dark:bg-yellow-800/30 rounded p-2">
            {event.trace_id && `Trace: ${event.trace_id.substring(0, 8)}...`}
            {event.span_id && event.trace_id && ' | '}
            {event.span_id && `Span: ${event.span_id.substring(0, 8)}...`}
          </div>
        </div>
      )}
    </div>
  )
}
