import React from 'react'
import type { ToolCallStartEvent } from '../../../../generated/event-types'
import { useExpandable } from '../../useExpandable'
import { Plus, Minus } from 'lucide-react'
import { MarkdownRenderer } from '../../../ui/MarkdownRenderer'

interface DelegationToolCallDisplayProps {
  event: ToolCallStartEvent
}

export const DelegationToolCallDisplay: React.FC<DelegationToolCallDisplayProps> = ({ event }) => {
  const { isExpanded, toggle } = useExpandable(false)
  const toolName = event.tool_name || ''

  let parsedArgs: Record<string, unknown> = {}
  try {
    if (event.tool_params?.arguments) {
      parsedArgs = JSON.parse(event.tool_params.arguments)
    }
  } catch { /* ignore */ }

  const reasoningLevel = parsedArgs.reasoning_level as string | undefined
  const reasoningColors: Record<string, string> = {
    high: 'bg-red-100 dark:bg-red-900/40 text-red-700 dark:text-red-300',
    medium: 'bg-yellow-100 dark:bg-yellow-900/40 text-yellow-700 dark:text-yellow-300',
    low: 'bg-green-100 dark:bg-green-900/40 text-green-700 dark:text-green-300',
  }

  // create_delegation_plan tool
  if (toolName === 'create_delegation_plan') {
    const planName = (parsedArgs.plan_name as string) || ''
    const objective = (parsedArgs.objective as string) || ''
    const context = (parsedArgs.context as string) || ''

    return (
      <div className="bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-800 rounded p-2">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0 flex-1">
            <span className="text-sm flex-shrink-0">📋</span>
            <div className="text-xs font-medium text-purple-700 dark:text-purple-300 truncate">
              Creating plan: <span className="font-mono">{planName}</span>
            </div>
            {reasoningLevel && (
              <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium flex-shrink-0 ${reasoningColors[reasoningLevel] || 'bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400'}`}>
                {reasoningLevel}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            {event.timestamp && (
              <span className="text-[10px] text-purple-500 dark:text-purple-400">
                {new Date(event.timestamp).toLocaleTimeString()}
              </span>
            )}
            <button
              onClick={toggle}
              className="p-0.5 hover:bg-purple-200 dark:hover:bg-purple-800 rounded text-purple-700 dark:text-purple-300 transition-colors"
              title={isExpanded ? "Collapse" : "Expand"}
            >
              {isExpanded ? <Minus className="w-3.5 h-3.5" /> : <Plus className="w-3.5 h-3.5" />}
            </button>
          </div>
        </div>

        {isExpanded && (
          <div className="mt-2 pt-2 border-t border-purple-200 dark:border-purple-700 space-y-1.5">
            <div>
              <div className="text-[10px] font-medium text-purple-500 dark:text-purple-400 mb-0.5">Objective</div>
              <MarkdownRenderer content={objective} className="text-xs" />
            </div>
            {context && (
              <div>
                <div className="text-[10px] font-medium text-purple-500 dark:text-purple-400 mb-0.5">Context</div>
                <MarkdownRenderer content={context} className="text-xs" />
              </div>
            )}
            <div className="flex items-center gap-3 text-[10px] text-purple-500 dark:text-purple-400">
              <span>Folder: Plans/{planName}/</span>
              {reasoningLevel && <span>Reasoning: {reasoningLevel}</span>}
            </div>
          </div>
        )}
      </div>
    )
  }

  // confirm_plan_execution tool
  if (toolName === 'confirm_plan_execution') {
    const planSummary = (parsedArgs.plan_summary as string) || ''

    return (
      <div className="bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded p-2">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 min-w-0 flex-1">
            <span className="text-sm flex-shrink-0">✋</span>
            <div className="text-xs font-medium text-amber-700 dark:text-amber-300">
              Requesting plan approval
            </div>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            {event.timestamp && (
              <span className="text-[10px] text-amber-500 dark:text-amber-400">
                {new Date(event.timestamp).toLocaleTimeString()}
              </span>
            )}
            {planSummary && (
              <button
                onClick={toggle}
                className="p-0.5 hover:bg-amber-200 dark:hover:bg-amber-800 rounded text-amber-700 dark:text-amber-300 transition-colors"
                title={isExpanded ? "Collapse" : "Expand"}
              >
                {isExpanded ? <Minus className="w-3.5 h-3.5" /> : <Plus className="w-3.5 h-3.5" />}
              </button>
            )}
          </div>
        </div>

        {isExpanded && planSummary && (
          <div className="mt-2 pt-2 border-t border-amber-200 dark:border-amber-700">
            <div className="text-[10px] font-medium text-amber-500 dark:text-amber-400 mb-0.5">Plan Summary</div>
            <MarkdownRenderer content={planSummary} className="text-xs" />
          </div>
        )}
      </div>
    )
  }

  return null
}
