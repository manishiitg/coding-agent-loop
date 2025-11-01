import React from 'react'
import type { ThrottlingDetectedEvent } from '../../../generated/events'

interface ThrottlingDetectedEventDisplayProps {
  event: ThrottlingDetectedEvent
}

export const ThrottlingDetectedEventDisplay: React.FC<ThrottlingDetectedEventDisplayProps> = ({
  event
}) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return '';
    return new Date(timestamp).toLocaleTimeString();
  };

  // Determine the error type display text
  const getErrorDisplayText = () => {
    switch (event.error_type) {
      case 'empty_content':
        return '⚠️ Empty Content Error';
      case 'connection_error':
        return '⚠️ Connection Error';
      case 'throttling':
      default:
        return '⚠️ Throttling Detected';
    }
  };

  return (
    <div className="p-2 bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-gray-700 dark:text-gray-300">
              {getErrorDisplayText()}{' '}
              <span className="text-xs font-normal text-gray-600 dark:text-gray-400">
                | Model: {event.model_id} | Provider: {event.provider}
                {event.attempt && event.max_attempts && ` | Attempt: ${event.attempt}/${event.max_attempts}`}
                {event.turn !== undefined && ` | Turn: ${event.turn}`}
                {event.retry_delay && ` | Retry in: ${event.retry_delay}`}
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
    </div>
  )
}
