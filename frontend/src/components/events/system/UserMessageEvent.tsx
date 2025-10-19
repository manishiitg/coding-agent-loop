import React from 'react'
import type { UserMessageEvent } from '../../../generated/events'

interface UserMessageEventDisplayProps {
  event: UserMessageEvent
  mode?: 'compact' | 'detailed'
}

export const UserMessageEventDisplay: React.FC<UserMessageEventDisplayProps> = ({ 
  event, 
  mode = 'detailed' 
}) => {
  if (mode === 'compact') {
    return (
      <div className="bg-slate-50 dark:bg-slate-800/30 border border-slate-200 dark:border-slate-700 rounded p-2">
        <div className="flex items-start gap-2">
          <span className="text-xs font-bold text-slate-700 dark:text-slate-300">👤</span>
          <div className="flex-1 min-w-0">
            {event.content ? (
              <div className="text-xs text-slate-900 dark:text-slate-100 leading-tight">
                {event.content.length > 80
                  ? `${event.content.substring(0, 80)}...`
                  : event.content
                }
              </div>
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
            <div className="text-xs text-slate-900 dark:text-slate-100 leading-tight whitespace-pre-wrap bg-white dark:bg-slate-700/50 rounded p-2 border border-slate-100 dark:border-slate-600">
              {event.content}
            </div>
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
