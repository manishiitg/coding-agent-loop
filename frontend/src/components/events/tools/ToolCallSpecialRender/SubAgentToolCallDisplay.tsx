import React from 'react'
import type { ToolCallStartEvent } from '../../../../generated/event-types'
import { useExpandable } from '../../useExpandable'
import { Plus, Minus, Bot, Zap } from 'lucide-react'
import { MarkdownRenderer } from '../../../ui/MarkdownRenderer'

interface SubAgentToolCallDisplayProps {
  event: ToolCallStartEvent
}

export const SubAgentToolCallDisplay: React.FC<SubAgentToolCallDisplayProps> = ({ event }) => {
  const { isExpanded, toggle } = useExpandable(false)
  const isGeneric = event.tool_name === 'call_generic_agent'

  let parsedArgs: Record<string, unknown> = {}
  try {
    if (event.tool_params?.arguments) {
      parsedArgs = JSON.parse(event.tool_params.arguments)
    }
  } catch { /* ignore */ }

  const routeId = parsedArgs.route_id as string | undefined
  const todoId = parsedArgs.todo_id as string | undefined
  const instructions = parsedArgs.instructions as string | undefined
  const successCriteria = parsedArgs.success_criteria as string | undefined
  const preferredTier = parsedArgs.preferred_tier as number | undefined

  const tierColors: Record<number, string> = {
    1: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-400',
    2: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-400',
    3: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-400',
  }
  const tierLabels: Record<number, string> = { 1: 'High', 2: 'Medium', 3: 'Low' }

  const hasExpandable = !!(instructions || successCriteria)

  return (
    <div className="bg-gray-50 dark:bg-gray-800/60 border border-gray-200 dark:border-gray-700 rounded p-2">
      {/* Header row */}
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-start gap-2 min-w-0 flex-1">
          <span className="flex-shrink-0 mt-0.5 text-indigo-500 dark:text-indigo-400">
            {isGeneric ? <Zap className="w-4 h-4" /> : <Bot className="w-4 h-4" />}
          </span>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-xs font-semibold text-gray-800 dark:text-gray-100">
                {isGeneric ? 'Generic Agent' : (routeId || 'Sub-agent')}
              </span>
              {preferredTier && (
                <span className={`text-[10px] px-1.5 py-0.5 rounded font-medium ${tierColors[preferredTier] || 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-400'}`}>
                  Tier {preferredTier} · {tierLabels[preferredTier]}
                </span>
              )}
            </div>
            {todoId && (
              <div className="text-[10px] text-gray-500 dark:text-gray-400 mt-0.5">
                Task: <span className="font-mono">{todoId}</span>
              </div>
            )}
          </div>
        </div>

        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <span className="text-[10px] text-gray-400 dark:text-gray-500">
              {new Date(event.timestamp).toLocaleTimeString()}
            </span>
          )}
          {hasExpandable && (
            <button
              onClick={toggle}
              className="p-0.5 rounded hover:bg-gray-200 dark:hover:bg-gray-700 text-gray-500 dark:text-gray-400 transition-colors"
              title={isExpanded ? 'Collapse' : 'Expand'}
            >
              {isExpanded ? <Minus className="w-3.5 h-3.5" /> : <Plus className="w-3.5 h-3.5" />}
            </button>
          )}
        </div>
      </div>

      {/* Expanded: instructions + success criteria */}
      {hasExpandable && isExpanded && (
        <div className="mt-2 pt-2 border-t border-gray-200 dark:border-gray-700 space-y-3">
          {instructions && (
            <div>
              <div className="text-[10px] font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500 mb-1">
                Instructions
              </div>
              <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded p-2">
                <MarkdownRenderer content={instructions} className="text-xs text-gray-700 dark:text-gray-300" />
              </div>
            </div>
          )}
          {successCriteria && (
            <div>
              <div className="text-[10px] font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500 mb-1">
                Success Criteria
              </div>
              <div className="bg-white dark:bg-gray-900 border border-gray-200 dark:border-gray-700 rounded p-2">
                <MarkdownRenderer content={successCriteria} className="text-xs text-gray-700 dark:text-gray-300" />
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
