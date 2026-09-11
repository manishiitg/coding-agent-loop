import React from 'react'
import type { ModelChangeEvent } from '../../../generated/events'

interface ModelChangeEventDisplayProps {
  event: ModelChangeEvent
}

export const ModelChangeEventDisplay: React.FC<ModelChangeEventDisplayProps> = ({
  event
}) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return '';
    return new Date(timestamp).toLocaleTimeString();
  };

  const getStatusIcon = (reason?: string) => {
    if (!reason) return 'Model Change';
    if (reason.includes('success')) return 'Success';
    if (reason.includes('error') || reason.includes('failed')) return 'Error';
    return 'Model Change';
  };

  return (
    <div className="p-2 bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-gray-700 dark:text-gray-300">
              {getStatusIcon(event.reason)} Model Change{' '}
              <span className="text-xs font-normal text-gray-600 dark:text-gray-400">
                {event.new_model_id || 'Unknown'}
                {event.reason && ` | ${event.reason}`}
                {event.provider && ` | Provider: ${event.provider}`}
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

      {/* Old model info - always visible if present */}
      {event.old_model_id && event.new_model_id && (
        <div className="mt-2">
          <div className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">
            Changed from: <span className="text-gray-700 dark:text-gray-300">{event.old_model_id}</span>
          </div>
        </div>
      )}
    </div>
  )
}
