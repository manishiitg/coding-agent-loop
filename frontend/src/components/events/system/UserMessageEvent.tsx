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
  const content = event.content || ''
  const isAutoNotification = content.trim().startsWith('[AUTO-NOTIFICATION]')

  // Check if content is long enough to need expansion
  const shouldShowExpand = content.length > CHAR_LIMIT

  if (isAutoNotification) {
    const firstLine = content
      .trim()
      .split('\n')
      .find(line => line.trim() && !line.includes('Do NOT call tools')) || content.trim()
    const summary = firstLine
      .replace(/^\[AUTO-NOTIFICATION\]\s*/i, '')
      .replace(/\s*Ack briefly;.*$/i, '')
      .trim()

    return (
      <div className="ml-6 border-l border-cyan-300/70 dark:border-cyan-700/70 pl-3 py-1">
        <div className="flex items-start gap-2 text-xs">
          <span className="mt-0.5 rounded-sm border border-cyan-200 bg-cyan-50 px-1.5 py-0.5 font-medium text-cyan-700 dark:border-cyan-800 dark:bg-cyan-950/40 dark:text-cyan-300">
            Auto
          </span>
          <div className="min-w-0 flex-1">
            <div className="whitespace-pre-wrap break-words text-slate-700 dark:text-slate-300">
              {isExpanded || !shouldShowExpand ? summary || 'Notification sent' : `${summary.substring(0, CHAR_LIMIT)}...`}
            </div>
            {shouldShowExpand && (
              <button
                onClick={() => setIsExpanded(!isExpanded)}
                className="mt-1 text-xs text-cyan-700 hover:text-cyan-800 dark:text-cyan-300 dark:hover:text-cyan-200"
              >
                {isExpanded ? 'Hide details' : 'Show details'}
              </button>
            )}
            {isExpanded && shouldShowExpand && (
              <pre className="mt-2 max-h-40 overflow-auto whitespace-pre-wrap rounded border border-slate-200 bg-white p-2 text-[11px] text-slate-600 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-300">
                {content}
              </pre>
            )}
          </div>
        </div>
      </div>
    )
  }

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
