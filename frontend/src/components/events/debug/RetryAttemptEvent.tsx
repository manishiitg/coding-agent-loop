import React from 'react'
import type { RetryAttemptEvent } from '../../../generated/events'

interface RetryAttemptEventDisplayProps {
  event: RetryAttemptEvent
}

export const RetryAttemptEventDisplay: React.FC<RetryAttemptEventDisplayProps> = ({ event }) => {
  const nextAttempt = (event.attempt_index ?? 0) + 1
  const attempt = event.total_attempts ? Math.min(nextAttempt, event.total_attempts) : nextAttempt
  const attemptText = event.total_attempts ? `${attempt} of ${event.total_attempts}` : `${attempt}`
  const reason = event.error?.replace(/\s*-\s*retrying original model\s*$/i, '')

  return (
    <div className="p-2 rounded border bg-amber-50 dark:bg-amber-950/20 border-amber-200 dark:border-amber-800">
      <div className="flex items-center justify-between gap-3">
        <div className="text-sm font-medium text-gray-700 dark:text-gray-300">
          Retrying Model{' '}
          <span className="text-xs font-normal text-gray-600 dark:text-gray-400">
            | {event.model_id || event.provider || 'Unknown'} | Attempt {attemptText}
            {event.duration ? ` | Waiting ${event.duration}` : ''}
          </span>
        </div>
        {event.timestamp && (
          <div className="text-xs text-gray-600 dark:text-gray-400 flex-shrink-0">
            {new Date(event.timestamp).toLocaleTimeString()}
          </div>
        )}
      </div>
      {reason && (
        <div className="mt-2">
          <div className="text-xs font-medium mb-1 text-amber-700 dark:text-amber-400">Reason:</div>
          <div className="text-xs rounded p-2 border text-amber-800 dark:text-amber-300 bg-amber-100/60 dark:bg-amber-900/20 border-amber-200 dark:border-amber-800">
            {reason}
          </div>
        </div>
      )}
    </div>
  )
}
