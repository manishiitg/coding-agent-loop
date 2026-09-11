import React from 'react'
import type { FallbackAttemptEvent } from '../../../generated/events'

interface FallbackAttemptEventDisplayProps {
  event: FallbackAttemptEvent
}

export const FallbackAttemptEventDisplay: React.FC<FallbackAttemptEventDisplayProps> = ({
  event
}) => {
  const isRetry = event.phase === 'retry'
  const failedAttempt = event.attempt_index ?? 0
  const nextAttempt = failedAttempt + 1
  const retryAttempt = event.total_attempts
    ? Math.min(nextAttempt, event.total_attempts)
    : nextAttempt

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

  const attemptText = event.total_attempts
    ? `${isRetry ? retryAttempt : (event.attempt_index ?? '?')} of ${event.total_attempts}`
    : `${isRetry ? retryAttempt : (event.attempt_index ?? '?')}`
  const retryReason = event.error?.replace(/\s*-\s*retrying original model\s*$/i, '')

  return (
    <div className={`p-2 rounded border ${isRetry
      ? 'bg-amber-50 dark:bg-amber-950/20 border-amber-200 dark:border-amber-800'
      : 'bg-gray-50 dark:bg-gray-900/20 border-gray-200 dark:border-gray-800'
    }`}>
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-gray-700 dark:text-gray-300">
              {isRetry ? 'Retrying Model' : `${getSuccessIcon(event.success)} Fallback Attempt`}{' '}
              <span className="text-xs font-normal text-gray-600 dark:text-gray-400">
                {isRetry
                  ? `| ${event.model_id || event.provider || 'Unknown'} | Attempt ${attemptText}${event.duration ? ` | Waiting ${event.duration}` : ''}`
                  : `#${event.attempt_index ?? '?'} | Model: ${event.model_id || event.provider || 'Unknown'} | Phase: ${event.phase || 'Unknown'}${event.success !== undefined ? ` | Success: ${getSuccessText(event.success)}` : ''}`}
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

      {/* A retry is scheduled, not failed. Present its cause as a warning;
          reserve the red failure treatment for an actual fallback failure. */}
      {(isRetry ? retryReason : event.error) && (
        <div className="mt-2">
          <div className={`text-xs font-medium mb-1 ${isRetry ? 'text-amber-700 dark:text-amber-400' : 'text-red-600 dark:text-red-400'}`}>
            {isRetry ? 'Reason:' : 'Error:'}
          </div>
          <div className={`text-xs rounded p-2 border ${isRetry
            ? 'text-amber-800 dark:text-amber-300 bg-amber-100/60 dark:bg-amber-900/20 border-amber-200 dark:border-amber-800'
            : 'text-red-700 dark:text-red-300 bg-red-50 dark:bg-red-900/20 border-red-200 dark:border-red-800'
          }`}>
            {isRetry ? retryReason : event.error}
          </div>
        </div>
      )}
    </div>
  )
}
