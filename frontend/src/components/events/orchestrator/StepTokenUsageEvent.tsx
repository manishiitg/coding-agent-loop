import React from 'react'
// TODO: Replace with generated type when backend schema is updated
interface StepTokenUsageEvent {
  timestamp?: string
  phase: string
  step: number
  step_title?: string
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
  cache_tokens: number
  reasoning_tokens: number
  llm_call_count: number
  cache_enabled_call_count: number
  average_cache_discount: number
}

interface StepTokenUsageEventDisplayProps {
  event: StepTokenUsageEvent
}

export const StepTokenUsageEventDisplay: React.FC<StepTokenUsageEventDisplayProps> = ({ event }) => {
  const stepLabel = event.step_title || `Step ${event.step + 1}`
  
  return (
    <div className="bg-orange-50 dark:bg-orange-900/20 border border-orange-200 dark:border-orange-800 rounded-md p-3">
      {/* Header */}
      <div className="flex items-center justify-between gap-3 mb-2">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="text-sm font-semibold text-orange-700 dark:text-orange-300">
            📊 Step Token Usage: {stepLabel}
          </span>
          <span className="text-xs text-orange-600 dark:text-orange-400">
            • Phase: {event.phase} • Step: {event.step + 1}
          </span>
        </div>
        {event.timestamp && (
          <span className="text-xs text-gray-500 dark:text-gray-400 flex-shrink-0">
            {new Date(event.timestamp).toLocaleTimeString()}
          </span>
        )}
      </div>
      
      {/* Main token metrics */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm font-medium">
        <span className="text-orange-700 dark:text-orange-300">
          Input: <span className="font-semibold">{event.prompt_tokens.toLocaleString()}</span>
        </span>
        <span className="text-orange-700 dark:text-orange-300">
          Output: <span className="font-semibold">{event.completion_tokens.toLocaleString()}</span>
        </span>
        <span className="text-orange-700 dark:text-orange-300">
          Total: <span className="font-semibold">{event.total_tokens.toLocaleString()}</span>
        </span>
        {event.cache_tokens > 0 && (
          <span className="text-cyan-600 dark:text-cyan-400 font-medium">
            Cache: {event.cache_tokens.toLocaleString()}
          </span>
        )}
        {event.reasoning_tokens > 0 && (
          <span className="text-purple-600 dark:text-purple-400 font-medium">
            Reasoning: {event.reasoning_tokens.toLocaleString()}
          </span>
        )}
      </div>
      
      {/* Additional metrics */}
      {(event.llm_call_count > 0 || event.cache_enabled_call_count > 0) && (
        <div className="mt-3 pt-2 border-t border-orange-200 dark:border-orange-700">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-orange-600 dark:text-orange-400">
            {event.llm_call_count > 0 && (
              <span>
                LLM Calls: <span className="font-semibold">{event.llm_call_count}</span>
              </span>
            )}
            {event.cache_enabled_call_count > 0 && (
              <span>
                Cache-Enabled Calls: <span className="font-semibold">{event.cache_enabled_call_count}</span>
              </span>
            )}
            {event.llm_call_count > 0 && event.total_tokens > 0 && (
              <span>
                Avg per Call: <span className="font-semibold">{Math.round(event.total_tokens / event.llm_call_count).toLocaleString()}</span>
              </span>
            )}
            {event.average_cache_discount > 0 && (
              <span>
                Avg Cache Discount: <span className="font-semibold">{(event.average_cache_discount * 100).toFixed(1)}%</span>
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

