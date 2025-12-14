import React, { useState } from 'react'
import type { SystemPromptEvent } from '../../../generated/events'

interface SystemPromptEventDisplayProps {
  event: SystemPromptEvent
  mode?: 'compact' | 'detailed'
}

export const SystemPromptEventDisplay: React.FC<SystemPromptEventDisplayProps> = ({
  event,
  mode = 'detailed'
}) => {
  const [isExpanded, setIsExpanded] = useState(false)

  // Check if content is long enough to need expansion
  const shouldShowExpand = event.content && event.content.length > (mode === 'compact' ? 80 : 300)

  if (mode === 'compact') {
    return (
      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
        <div className="flex items-start gap-2">
          <span className="text-xs font-bold text-blue-700 dark:text-blue-300">System</span>
          <div className="flex-1 min-w-0">
            {event.content ? (
              <div className="text-xs text-blue-900 dark:text-blue-100 leading-tight">
                {isExpanded || event.content.length <= 80
                  ? event.content
                  : `${event.content.substring(0, 80)}...`
                }
              </div>
            ) : (
              <div className="text-xs text-red-600 dark:text-red-400 italic">
                No prompt content
              </div>
            )}
            {shouldShowExpand && (
              <button
                onClick={() => setIsExpanded(!isExpanded)}
                className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 mt-1"
              >
                {isExpanded ? '↑ Collapse' : '↓ Expand'}
              </button>
            )}
            {event.token_count !== undefined && event.token_count > 0 && (
              <div className="text-xs text-blue-600 dark:text-blue-400 mt-1 font-medium">
                📊 {event.token_count.toLocaleString()} tokens
              </div>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
      <div className="flex items-start gap-2">
        <span className="text-xs font-bold text-blue-700 dark:text-blue-300">System</span>
        <div className="flex-1 min-w-0">
          {event.content ? (
            <div className="text-xs text-blue-900 dark:text-blue-100 leading-tight whitespace-pre-wrap font-mono bg-blue-50 dark:bg-blue-900/30 rounded p-2 border border-blue-100 dark:border-blue-700/50">
              {isExpanded || !shouldShowExpand ? event.content : `${event.content.substring(0, 300)}...`}
            </div>
          ) : (
            <div className="text-xs text-red-600 dark:text-red-400 italic bg-red-50 dark:bg-red-900/30 rounded p-2 border border-red-200 dark:border-red-800">
              No prompt content
            </div>
          )}

          {shouldShowExpand && (
            <button
              onClick={() => setIsExpanded(!isExpanded)}
              className="text-xs text-blue-600 dark:text-blue-400 hover:text-blue-700 dark:hover:text-blue-300 mt-1"
            >
              {isExpanded ? '↑ Collapse' : '↓ Expand'}
            </button>
          )}

          <div className="flex items-center gap-3 mt-2">
            {event.token_count !== undefined && event.token_count > 0 && (
              <div className="text-xs text-blue-600 dark:text-blue-400 font-medium">
                📊 {event.token_count.toLocaleString()} tokens
              </div>
            )}
            {event.timestamp && (
              <div className="text-xs text-blue-600 dark:text-blue-400">
                {new Date(event.timestamp).toLocaleString()}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
