import React from 'react'
import type { FallbackModelUsedEvent } from '../../../generated/events'

interface FallbackModelUsedEventDisplayProps {
  event: FallbackModelUsedEvent
}

export const FallbackModelUsedEventDisplay: React.FC<FallbackModelUsedEventDisplayProps> = ({
  event
}) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return '';
    return new Date(timestamp).toLocaleTimeString();
  };

  return (
    <div className="p-2 bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-gray-700 dark:text-gray-300">
              Model Fallback{' '}
              <span className="text-xs font-normal text-gray-600 dark:text-gray-400">
                | Original: {event.original_model} → {event.fallback_model} | Provider: {event.provider}
                {event.turn !== undefined && ` | Turn: ${event.turn}`}
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
