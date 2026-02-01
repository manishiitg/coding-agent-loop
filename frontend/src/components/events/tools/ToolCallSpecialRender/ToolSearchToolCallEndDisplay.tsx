import React from 'react'
import type { ToolCallEndEvent } from '../../../../generated/events'
import { CircularProgress, type ContextOnlyTokenUsage } from '../../../ui/CircularProgress'
import { TooltipProvider } from '../../../ui/tooltip'
import { useExpandable } from '../../useExpandable'
import { Plus, Minus } from 'lucide-react'

interface ToolSearchToolCallEndDisplayProps {
  event: ToolCallEndEvent
}

// Format duration from nanoseconds
const formatDuration = (durationNs: number) => {
  if (!durationNs || durationNs <= 0) return '0ms'
  const durationMs = durationNs / 1000000
  if (durationMs < 1) {
    const durationUs = durationNs / 1000
    return `${Math.round(durationUs)}μs`
  }
  if (durationMs < 1000) return `${Math.round(durationMs)}ms`
  if (durationMs < 60000) return `${(durationMs / 1000).toFixed(1)}s`
  return `${(durationMs / 60000).toFixed(1)}m`
}

export const ToolSearchToolCallEndDisplay: React.FC<ToolSearchToolCallEndDisplayProps> = ({ event }) => {
  const { isExpanded: isOutputExpanded, toggle } = useExpandable()

  const contextUsagePercent = event.context_usage_percent
  const modelContextWindow = event.model_context_window
  const contextWindowUsage = event.context_window_usage
  const modelId = event.model_id

  const tokenUsageForTooltip: ContextOnlyTokenUsage | undefined =
    contextUsagePercent !== undefined && contextUsagePercent > 0 ? {
      context_usage_percent: contextUsagePercent,
      model_context_window: modelContextWindow,
      context_window_usage: contextWindowUsage,
      model_id: modelId,
    } : undefined

  if (!event.result) return null

  const toolName = event.tool_name || ''

  if (toolName === 'search_tools') {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let resultData: any = null
    try {
      resultData = JSON.parse(event.result)
    } catch {
      // Not valid JSON
    }

    const foundTools = resultData?.tools || []
    const message = resultData?.message || event.result

    return (
      <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-blue-700 dark:text-blue-300 flex items-center gap-2">
                🔍 Search Complete{' '}
                <span className="text-xs font-normal text-blue-600 dark:text-blue-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
                </span>
                {contextUsagePercent !== undefined && contextUsagePercent > 0 && (
                  <TooltipProvider>
                    <CircularProgress
                      percentage={contextUsagePercent}
                      size={18}
                      strokeWidth={3}
                      tokenUsage={tokenUsageForTooltip}
                    />
                  </TooltipProvider>
                )}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            {event.timestamp && (
              <div className="text-xs text-blue-600 dark:text-blue-400 flex-shrink-0">
                {new Date(event.timestamp).toLocaleTimeString()}
              </div>
            )}
            <button
              onClick={toggle}
              className="p-0.5 hover:bg-blue-200 dark:hover:bg-blue-800 rounded text-blue-700 dark:text-blue-300 transition-colors"
              title={isOutputExpanded ? "Collapse output (Alt+Click for all)" : "Expand output (Alt+Click for all)"}
            >
              {isOutputExpanded ? <Minus className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
            </button>
          </div>
        </div>

        {isOutputExpanded && (
          <div className="mt-2 space-y-2">
             <div className="text-xs text-blue-800 dark:text-blue-200">{message}</div>
             
             {foundTools.length > 0 && (
               <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
                 <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-2 px-1">
                   Found Tools ({foundTools.length}):
                 </div>
                 <div className="grid grid-cols-1 gap-2 max-h-80 overflow-y-auto px-1">
                   {/* eslint-disable-next-line @typescript-eslint/no-explicit-any */}
                   {foundTools.map((tool: any, idx: number) => (
                     <div key={idx} className="bg-white dark:bg-gray-900 border border-blue-200 dark:border-blue-800 rounded-md p-3 hover:border-blue-400 dark:hover:border-blue-500 transition-colors group shadow-sm">
                       <div className="flex items-center justify-between gap-2 mb-2">
                         <div 
                           className="font-mono text-xs font-bold text-blue-700 dark:text-blue-300 bg-blue-50 dark:bg-blue-900/40 px-2 py-1 rounded cursor-pointer hover:bg-blue-100 dark:hover:bg-blue-900/60 transition-colors flex items-center gap-1.5 border border-blue-100 dark:border-blue-800"
                           title="Click to copy tool name"
                           onClick={(e) => {
                             e.stopPropagation()
                             navigator.clipboard.writeText(tool.name)
                             // Visual feedback could be added here
                           }}
                         >
                           {tool.name}
                           <span className="opacity-0 group-hover:opacity-100 text-[10px] transition-opacity">📋</span>
                         </div>
                       </div>
                       <div className="text-xs text-gray-600 dark:text-gray-400 leading-relaxed">
                         {tool.description}
                       </div>
                     </div>
                   ))}
                 </div>
               </div>
             )}
          </div>
        )}
      </div>
    )
  }

  if (toolName === 'add_tool') {
    return (
      <div className="bg-violet-50 dark:bg-violet-900/20 border border-violet-200 dark:border-violet-800 rounded p-2">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3 min-w-0 flex-1">
            <div className="min-w-0 flex-1">
              <div className="text-sm font-medium text-violet-700 dark:text-violet-300 flex items-center gap-2">
                ➕ Tool Added{' '}
                <span className="text-xs font-normal text-violet-600 dark:text-violet-400">
                  {event.turn && `• Turn: ${event.turn}`}
                  {event.duration && ` • Duration: ${formatDuration(event.duration)}`}
                </span>
                 {contextUsagePercent !== undefined && contextUsagePercent > 0 && (
                  <TooltipProvider>
                    <CircularProgress
                      percentage={contextUsagePercent}
                      size={18}
                      strokeWidth={3}
                      tokenUsage={tokenUsageForTooltip}
                    />
                  </TooltipProvider>
                )}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-2 flex-shrink-0">
            {event.timestamp && (
              <div className="text-xs text-violet-600 dark:text-violet-400 flex-shrink-0">
                {new Date(event.timestamp).toLocaleTimeString()}
              </div>
            )}
            <button
              onClick={toggle}
              className="p-0.5 hover:bg-violet-200 dark:hover:bg-violet-800 rounded text-violet-700 dark:text-violet-300 transition-colors"
              title={isOutputExpanded ? "Collapse output (Alt+Click for all)" : "Expand output (Alt+Click for all)"}
            >
              {isOutputExpanded ? <Minus className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
            </button>
          </div>
        </div>

        {isOutputExpanded && (
          <div className="mt-2">
             <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
                <pre className="text-xs text-gray-800 dark:text-gray-200 font-mono whitespace-pre-wrap overflow-x-auto">
                  {event.result}
                </pre>
             </div>
          </div>
        )}
      </div>
    )
  }

  return null
}
