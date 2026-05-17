import React, { useState } from 'react'
import type { UserMessageEvent } from '../../../generated/events'

interface UserMessageEventDisplayProps {
  event: UserMessageEvent
  mode?: 'compact' | 'detailed'
}

export const UserMessageEventDisplay: React.FC<UserMessageEventDisplayProps> = ({ 
  event, 
  mode = 'detailed' 
}) => {
  const [isExpanded, setIsExpanded] = useState(false)
  const CHAR_LIMIT = 300
  const isLiveCodingAgentInput = event.metadata?.source === 'coding_agent_live_input'

  // Check if content is long enough to need expansion
  const shouldShowExpand = event.content && event.content.length > CHAR_LIMIT

  if (isLiveCodingAgentInput) {
    return (
      <div className="ml-6 flex items-baseline gap-2 py-1 text-xs text-slate-500 dark:text-slate-400">
        <span className="text-slate-400 dark:text-slate-500">↳</span>
        <span className="max-w-full whitespace-pre-wrap break-words text-slate-700 dark:text-slate-200">
          {event.content || 'No message content'}
        </span>
      </div>
    )
  }

  if (mode === 'compact') {
    return (
      <div className="bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700 rounded p-2">
        <div className="flex items-start gap-2">
          <span className="text-xs font-bold text-slate-700 dark:text-slate-300">👤</span>
          <div className="flex-1 min-w-0">
            {event.content ? (
              <>
                <div className="text-xs text-slate-900 dark:text-slate-100 leading-tight">
                  {isExpanded || event.content.length <= CHAR_LIMIT
                    ? event.content
                    : `${event.content.substring(0, CHAR_LIMIT)}...`
                  }
                </div>
                {shouldShowExpand && (
                  <button
                    onClick={() => setIsExpanded(!isExpanded)}
                    className="text-xs text-slate-600 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-300 mt-1"
                  >
                    {isExpanded ? '↑ Collapse' : '↓ Expand'}
                  </button>
                )}
              </>
            ) : (
              <div className="text-xs text-red-600 dark:text-red-400 italic">
                No message content
              </div>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700 rounded p-2">
      <div className="flex items-start gap-2">
        <span className="text-xs font-bold text-slate-700 dark:text-slate-300">👤</span>
        <div className="flex-1 min-w-0">
          {event.content ? (
            <>
              <div className="text-xs text-slate-900 dark:text-slate-100 leading-tight whitespace-pre-wrap bg-white dark:bg-slate-700/50 rounded p-2 border border-slate-100 dark:border-slate-600">
                {isExpanded || !shouldShowExpand ? event.content : `${event.content.substring(0, CHAR_LIMIT)}...`}
              </div>
              {shouldShowExpand && (
                <button
                  onClick={() => setIsExpanded(!isExpanded)}
                  className="text-xs text-slate-600 dark:text-slate-400 hover:text-slate-700 dark:hover:text-slate-300 mt-1"
                >
                  {isExpanded ? '↑ Collapse' : '↓ Expand'}
                </button>
              )}
            </>
          ) : (
            <div className="text-xs text-red-600 dark:text-red-400 italic bg-red-50 dark:bg-red-900/30 rounded p-2 border border-red-200 dark:border-red-800">
              No message content
            </div>
          )}

          {event.timestamp && (
            <div className="text-xs text-slate-600 dark:text-slate-400 mt-1">
              {new Date(event.timestamp).toLocaleString()}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
