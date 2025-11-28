import React from 'react'
import type { TokenUsageEvent } from '../../../generated/events'
import { formatDuration } from '../../../utils/duration'

interface TokenUsageEventDisplayProps {
  event: TokenUsageEvent
}

export const TokenUsageEventDisplay: React.FC<TokenUsageEventDisplayProps> = ({ event }) => {
  const isTotalEvent = event.context === 'conversation_total'
  
  // Extract cumulative metrics from generation_info for total events
  const cumulativePromptTokens = isTotalEvent 
    ? (event.generation_info?.cumulative_prompt_tokens as number ?? event.prompt_tokens ?? 0)
    : (event.generation_info?.PromptTokens as number ?? event.prompt_tokens ?? 0)
  
  const cumulativeCompletionTokens = isTotalEvent
    ? (event.generation_info?.cumulative_completion_tokens as number ?? event.completion_tokens ?? 0)
    : (event.generation_info?.CompletionTokens as number ?? event.completion_tokens ?? 0)
  
  const cumulativeTotalTokens = isTotalEvent
    ? (event.generation_info?.cumulative_total_tokens as number ?? event.total_tokens ?? 0)
    : (event.generation_info?.TotalTokens as number ?? event.total_tokens ?? 0)
  
  const cumulativeCacheTokens = isTotalEvent
    ? (event.generation_info?.cumulative_cache_tokens as number ?? 0)
    : 0
  
  const cumulativeReasoningTokens = isTotalEvent
    ? (event.generation_info?.cumulative_reasoning_tokens as number ?? event.reasoning_tokens ?? 0)
    : (event.generation_info?.ReasoningTokens as number ?? event.reasoning_tokens ?? 0)
  
  const llmCallCount = isTotalEvent
    ? (event.generation_info?.llm_call_count as number ?? 0)
    : 0
  
  const cacheEnabledCallCount = isTotalEvent
    ? (event.generation_info?.cache_enabled_call_count as number ?? 0)
    : 0

  // Theme colors - use purple for total events, blue for per-call events
  const bgColor = isTotalEvent 
    ? 'bg-purple-50 dark:bg-purple-900/20'
    : 'bg-blue-50 dark:bg-blue-900/20'
  
  const borderColor = isTotalEvent
    ? 'border-purple-200 dark:border-purple-800'
    : 'border-blue-200 dark:border-blue-800'
  
  const textColor = isTotalEvent
    ? 'text-purple-700 dark:text-purple-300'
    : 'text-blue-700 dark:text-blue-300'
  
  const textSecondaryColor = isTotalEvent
    ? 'text-purple-600 dark:text-purple-400'
    : 'text-blue-600 dark:text-blue-400'

  return (
    <div className={`${bgColor} border ${borderColor} rounded-md p-3`}>
      {/* Header */}
      <div className="flex items-center justify-between gap-3 mb-2">
        <div className="flex items-center gap-2 flex-wrap">
          <span className={`text-sm font-semibold ${textColor}`}>
            {isTotalEvent ? '📊 Total Token Usage' : 'Token Usage'}
          </span>
          {event.context && (
            <span className={`text-xs ${textSecondaryColor}`}>
              • Context: {event.context}
            </span>
          )}
          {event.model_id && (
            <span className={`text-xs ${textSecondaryColor}`}>
              • Model: {event.model_id}
            </span>
          )}
          {event.provider && (
            <span className={`text-xs ${textSecondaryColor}`}>
              • Provider: {event.provider}
            </span>
          )}
        </div>
        {event.timestamp && (
          <span className="text-xs text-gray-500 dark:text-gray-400 flex-shrink-0">
            {new Date(event.timestamp).toLocaleTimeString()}
          </span>
        )}
      </div>
      
      {/* Main token metrics - all on same line */}
      <div className={`flex flex-wrap items-center gap-x-3 gap-y-1 ${isTotalEvent ? 'text-sm' : 'text-xs'} font-medium`}>
        <span className={textColor}>
          Input: <span className="font-semibold">{cumulativePromptTokens.toLocaleString()}</span>
        </span>
        <span className={textColor}>
          Output: <span className="font-semibold">{cumulativeCompletionTokens.toLocaleString()}</span>
        </span>
        <span className={textColor}>
          Total: <span className="font-semibold">{cumulativeTotalTokens.toLocaleString()}</span>
        </span>
        {cumulativeCacheTokens > 0 && (
          <span className="text-cyan-600 dark:text-cyan-400 font-medium">
            Cache: {cumulativeCacheTokens.toLocaleString()}
          </span>
        )}
        {cumulativeReasoningTokens > 0 && (
          <span className="text-purple-600 dark:text-purple-400 font-medium">
            Reasoning: {cumulativeReasoningTokens.toLocaleString()}
          </span>
        )}
        {event.duration && (
          <span className={textSecondaryColor}>
            Duration: {formatDuration(event.duration)}
          </span>
        )}
      </div>
      
      {/* Additional metrics for total events - separate section */}
      {isTotalEvent && (llmCallCount > 0 || cacheEnabledCallCount > 0) && (
        <div className="mt-3 pt-2 border-t border-purple-200 dark:border-purple-700">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-purple-600 dark:text-purple-400">
            {llmCallCount > 0 && (
              <span>
                LLM Calls: <span className="font-semibold">{llmCallCount}</span>
              </span>
            )}
            {cacheEnabledCallCount > 0 && (
              <span>
                Cache-Enabled Calls: <span className="font-semibold">{cacheEnabledCallCount}</span>
              </span>
            )}
            {llmCallCount > 0 && cumulativeTotalTokens > 0 && (
              <span>
                Avg per Call: <span className="font-semibold">{Math.round(cumulativeTotalTokens / llmCallCount).toLocaleString()}</span>
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
