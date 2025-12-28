import React from 'react'
import type { LargeToolOutputDetectedEvent } from '../../../generated/events'

interface LargeToolOutputDetectedEventDisplayProps {
  event: LargeToolOutputDetectedEvent
}

export const LargeToolOutputDetectedEventDisplay: React.FC<LargeToolOutputDetectedEventDisplayProps> = ({
  event
}) => {
  return (
    <div className="bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded p-2">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
            Context Offloading Detected
          </span>
          {event.tool_name && (
            <span className="text-xs text-gray-600 dark:text-gray-400">
              • {event.tool_name}
            </span>
          )}
          {event.output_size && event.threshold && (
            <span className="text-xs text-gray-600 dark:text-gray-400">
              • {event.output_size.toLocaleString()} characters (cutoff: {event.threshold.toLocaleString()} characters, {Math.round((event.output_size / event.threshold) * 100)}% of limit)
            </span>
          )}
          {event.server_available !== undefined && (
            <span className={`text-xs ${event.server_available ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'}`}>
              • Server Available: {event.server_available ? 'Yes' : 'No'}
            </span>
          )}
        </div>
        {event.timestamp && (
          <span className="text-xs text-gray-500 dark:text-gray-400">
            {new Date(event.timestamp).toLocaleTimeString()}
          </span>
        )}
      </div>
      
      {(event.output_folder || (event.output_size && event.threshold)) && (
        <div className="mt-2">
          {event.output_size && event.threshold && (
            <div className="mb-2">
              <div className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Size Details:</div>
              <div className="p-2 bg-gray-100 dark:bg-gray-800/30 rounded text-xs text-gray-700 dark:text-gray-300">
                <div>Total characters: <span className="font-mono font-semibold">{event.output_size.toLocaleString()}</span></div>
                <div>Cutoff threshold: <span className="font-mono font-semibold">{event.threshold.toLocaleString()}</span></div>
                <div>Excess: <span className="font-mono font-semibold text-orange-600 dark:text-orange-400">{(event.output_size - event.threshold).toLocaleString()}</span> characters</div>
                <div>Percentage: <span className="font-mono font-semibold text-blue-600 dark:text-blue-400">{Math.round((event.output_size / event.threshold) * 100)}%</span> of limit</div>
              </div>
            </div>
          )}
          {event.output_folder && (
            <div>
              <div className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Output Folder:</div>
              <div className="p-2 bg-gray-100 dark:bg-gray-800/30 rounded text-xs font-mono text-gray-700 dark:text-gray-300">
                {event.output_folder}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
