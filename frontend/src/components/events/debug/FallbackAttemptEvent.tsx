import React from 'react'
import type { FallbackAttemptEvent } from '../../../generated/events'

interface FallbackAttemptEventDisplayProps {
  event: FallbackAttemptEvent
}

export const FallbackAttemptEventDisplay: React.FC<FallbackAttemptEventDisplayProps> = ({
  event
}) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return '';
    return new Date(timestamp).toLocaleTimeString();
  };

  const getSuccessIcon = (success?: boolean) => {
    if (success === undefined) return 'Fallback Attempt';
    return success ? 'Success' : 'Failed';
  };

  const getSuccessText = (success?: boolean) => {
    if (success === undefined) return '';
    return success ? 'Yes' : 'No';
  };

  return (
    <div className="p-2 bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-gray-700 dark:text-gray-300">
              {getSuccessIcon(event.success)} Fallback Attempt{' '}
              <span className="text-xs font-normal text-gray-600 dark:text-gray-400">
                #{event.attempt_index ?? '?'} | Model: {event.model_id || event.provider || 'Unknown'} | Phase: {event.phase || 'Unknown'}
                {event.success !== undefined && ` | Success: ${getSuccessText(event.success)}`}
              </span>
            </div>
          </div>
        </div>
        
        {/* Right side: Time */}
        {event.timestamp && (
          <div className="text-xs text-gray-600 dark:text-gray-400 flex-shrink-0">
            {formatTimestamp(event.timestamp)}
          </div>
        )}
      </div>

      {/* Error content - always visible if present */}
      {event.error && (
        <div className="mt-2">
          <div className="text-xs font-medium text-red-600 dark:text-red-400 mb-1">Error:</div>
          <div className="text-xs text-red-700 dark:text-red-300 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-2">
            {event.error}
          </div>
        </div>
      )}
    </div>
  )
}
