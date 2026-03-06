import React, { useState } from 'react'
import type { TodoTaskStatusUpdateEvent } from '../../../generated/event-types'
import { Plus, Minus, ListTodo } from 'lucide-react'
import { MarkdownRenderer } from '../../ui/MarkdownRenderer'

interface TodoTaskStatusUpdateEventDisplayProps {
  event: TodoTaskStatusUpdateEvent
}

export const TodoTaskStatusUpdateEventDisplay: React.FC<TodoTaskStatusUpdateEventDisplayProps> = ({ event }) => {
  const [isExpanded, setIsExpanded] = useState(true)

  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return ''
    return new Date(timestamp).toLocaleTimeString()
  }

  if (!event.tasks_content) return null

  return (
    <div className="bg-gray-50 dark:bg-gray-800/50 border border-gray-200 dark:border-gray-700 rounded p-2">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0 flex-1">
          <ListTodo className="w-4 h-4 text-indigo-500 dark:text-indigo-400 flex-shrink-0" />
          <div className="text-xs font-semibold text-gray-700 dark:text-gray-200">
            Task Status
            {event.todo_id && (
              <span className="ml-2 text-[10px] font-normal text-gray-500 dark:text-gray-400">
                after: <span className="font-mono">{event.todo_id}</span>
              </span>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <span className="text-[10px] text-gray-400 dark:text-gray-500">
              {formatTimestamp(event.timestamp)}
            </span>
          )}
          <button
            onClick={() => setIsExpanded(!isExpanded)}
            className="p-0.5 rounded hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400 transition-colors"
            title={isExpanded ? 'Collapse' : 'Expand'}
          >
            {isExpanded ? <Minus className="w-3.5 h-3.5" /> : <Plus className="w-3.5 h-3.5" />}
          </button>
        </div>
      </div>

      {isExpanded && (
        <div className="mt-2 pt-2 border-t border-gray-200 dark:border-gray-700">
          <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded p-2 max-h-[32rem] overflow-y-auto">
            <MarkdownRenderer content={event.tasks_content} className="text-xs text-gray-700 dark:text-gray-300" />
          </div>
        </div>
      )}
    </div>
  )
}
