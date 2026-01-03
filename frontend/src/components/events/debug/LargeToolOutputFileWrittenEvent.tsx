import React from 'react'
import type { LargeToolOutputFileWrittenEvent } from '../../../generated/events'

interface LargeToolOutputFileWrittenEventDisplayProps {
  event: LargeToolOutputFileWrittenEvent
}

export const LargeToolOutputFileWrittenEventDisplay: React.FC<LargeToolOutputFileWrittenEventDisplayProps> = ({
  event
}) => {
  return (
    <div className="bg-gray-50 dark:bg-gray-900/20 border border-gray-200 dark:border-gray-800 rounded p-2">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
            Context Offloading: File Saved
          </span>
          {event.file_path && (
            <span className="text-xs text-gray-600 dark:text-gray-400">
              • {event.file_path.split('/').pop()}
            </span>
          )}
          {event.tool_name && (
            <span className="text-xs text-gray-600 dark:text-gray-400">
              • Tool: {event.tool_name}
            </span>
          )}
          {(event.output_size !== undefined || event.file_size !== undefined) && (
            <span className="text-xs text-gray-600 dark:text-gray-400">
              • {event.output_size && `${event.output_size} bytes`}
              {event.output_size && event.file_size && ' | '}
              {event.file_size && `${event.file_size} bytes`}
            </span>
          )}
        </div>
        {event.timestamp && (
          <span className="text-xs text-gray-500 dark:text-gray-400">
            {new Date(event.timestamp).toLocaleTimeString()}
          </span>
        )}
      </div>
      
      <div className="space-y-2 mt-2">
        {event.file_path && (
          <div>
            <div className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">File Path:</div>
            <div className="p-2 bg-gray-100 dark:bg-gray-800/30 rounded text-xs font-mono text-gray-700 dark:text-gray-300">
              {event.file_path}
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

        {event.preview && (
          <div>
            <div className="text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Preview:</div>
            <div className="p-2 bg-gray-100 dark:bg-gray-800/30 rounded text-xs text-gray-700 dark:text-gray-300 max-h-20 overflow-y-auto">
              {event.preview}
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
