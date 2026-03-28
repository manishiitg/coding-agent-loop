import React from 'react'
import type { BrokenPipeEvent } from '../../../generated/event-types'

interface BrokenPipeEventDisplayProps {
  event: BrokenPipeEvent
}

export const BrokenPipeEventDisplay: React.FC<BrokenPipeEventDisplayProps> = ({ event }) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return '';
    return new Date(timestamp).toLocaleTimeString();
  };

  const getOperationLabel = (operation?: string) => {
    switch (operation) {
      case 'broken_pipe_detected': return 'Broken Pipe';
      case 'retry_success': return 'Retry Success';
      case 'retry_failure': return 'Retry Failed';
      default: return operation || 'Broken Pipe';
    }
  };

  const getStatusColor = (operation?: string) => {
    if (operation === 'retry_success') return 'text-green-600 dark:text-green-400';
    if (operation === 'retry_failure' || operation === 'broken_pipe_detected') return 'text-red-600 dark:text-red-400';
    return 'text-yellow-600 dark:text-yellow-400';
  };

  return (
    <div className="p-2 bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className={`text-sm font-medium ${getStatusColor(event.operation)}`}>
              {getOperationLabel(event.operation)}{' '}
              <span className="text-xs font-normal text-gray-600 dark:text-gray-400">
                {event.tool_name && `Tool: ${event.tool_name}`}
                {event.server_name && ` | Server: ${event.server_name}`}
                {event.duration && ` | ${event.duration}`}
              </span>
            </div>
          </div>
        </div>

        {event.timestamp && (
          <div className="text-xs text-gray-600 dark:text-gray-400 flex-shrink-0">
            {formatTimestamp(event.timestamp)}
          </div>
        )}
      </div>

      {event.error && (
        <div className="mt-2">
          <div className="text-xs text-red-700 dark:text-red-300 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded p-2">
            {event.error}
          </div>
        </div>
      )}
    </div>
  )
}
