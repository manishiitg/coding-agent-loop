import React from 'react'
import type { ToolCallStartEvent } from '../../../../generated/event-types'
import { useExpandable } from '../../useExpandable'
import { Plus, Minus } from 'lucide-react'

interface ToolSearchToolCallDisplayProps {
  event: ToolCallStartEvent
}

export const ToolSearchToolCallDisplay: React.FC<ToolSearchToolCallDisplayProps> = ({ event }) => {
  const { isExpanded, toggle } = useExpandable(false)

  if (!event.tool_params?.arguments) {
    return null
  }

  let parsedArgs: Record<string, unknown> = {}
  try {
    parsedArgs = JSON.parse(event.tool_params.arguments)
  } catch {
    return null
  }

  // Handle search_tools
  if (toolName === 'search_tools') {
    const query = (parsedArgs.query as string) || ''
    
    return (
      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-blue-700 dark:text-blue-300 flex items-center">
                🔍 Tool Search{parallelBadge}{' '}
                <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                  {event.turn && `• Turn: ${event.turn}`}
                </span>
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            {event.timestamp && (
              <div className="text-xs text-blue-600 dark:text-blue-400">
                {new Date(event.timestamp).toLocaleTimeString()}
              </div>
            )}
            <button
              onClick={toggle}
              className="p-0.5 hover:bg-blue-200 dark:hover:bg-blue-800 rounded text-blue-700 dark:text-blue-300 transition-colors"
              title={isExpanded ? "Collapse arguments (Alt+Click for all)" : "Expand arguments (Alt+Click for all)"}
            >
              {isExpanded ? <Minus className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
            </button>
          </div>
        </div>

        {isExpanded && (
          <div className="mt-2">
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
               <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">Query:</div>
               <div className="text-sm font-mono text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900 px-2 py-1 rounded">
                 {query}
               </div>
            </div>
          </div>
        )}
      </div>
    )
  }

  // Handle add_tool
  if (toolName === 'add_tool') {
    const toolNames = (parsedArgs.tool_names as string[]) || (parsedArgs.tool_name ? [parsedArgs.tool_name as string] : [])

    return (
      <div className="bg-purple-50 dark:bg-violet-900/20 border border-purple-200 dark:border-violet-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
           <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-purple-700 dark:text-violet-300 flex items-center">
                ➕ Add Tool{parallelBadge}{' '}
                <span className="text-xs font-normal text-purple-600 dark:text-violet-400">
                  {event.turn && `• Turn: ${event.turn}`}
                </span>
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            {event.timestamp && (
              <div className="text-xs text-purple-600 dark:text-violet-400">
                {new Date(event.timestamp).toLocaleTimeString()}
              </div>
            )}
            <button
              onClick={toggle}
              className="p-0.5 hover:bg-purple-200 dark:hover:bg-violet-800 rounded text-purple-700 dark:text-violet-300 transition-colors"
              title={isExpanded ? "Collapse arguments (Alt+Click for all)" : "Expand arguments (Alt+Click for all)"}
            >
              {isExpanded ? <Minus className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
            </button>
          </div>
        </div>

        {isExpanded && (
          <div className="mt-2">
             <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
                <div className="text-xs font-medium text-purple-700 dark:text-violet-300 mb-1">
                   Adding {toolNames.length} tool{toolNames.length !== 1 ? 's' : ''}:
                </div>
                <div className="flex flex-wrap gap-1">
                  {toolNames.map((tool, index) => (
                    <span
                      key={index}
                      className="text-xs font-mono text-gray-800 dark:text-gray-200 bg-gray-50 dark:bg-gray-900 px-2 py-1 rounded border border-gray-200 dark:border-gray-700"
                    >
                      {tool}
                    </span>
                  ))}
                </div>
             </div>
          </div>
        )}
      </div>
    )
  }

  return null
}
