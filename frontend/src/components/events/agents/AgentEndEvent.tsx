import { useState } from 'react'
import type { AgentEndEvent } from '@/generated/events'

interface AgentEndEventProps {
  event: AgentEndEvent
}

export function AgentEndEventComponent({ event }: AgentEndEventProps) {
  const [isExpanded, setIsExpanded] = useState(false)
  
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return 'Unknown time'
    return new Date(timestamp).toLocaleTimeString()
  }

  const isSuccess = event.success !== false // Default to true if not specified
  const hasExpandableContent = event.error || event.parent_id || event.trace_id
  
  // Type assertion for extended properties that may exist but aren't in the base type
  const extendedEvent = event as AgentEndEvent & {
    total_tokens?: number
    prompt_tokens?: number
    completion_tokens?: number
    cache_tokens?: number
    reasoning_tokens?: number
    llm_call_count?: number
    cache_enabled_call_count?: number
    average_cache_discount?: number
  }

  return (
    <div className={`border border-${isSuccess ? 'green' : 'red'}-200 dark:border-${isSuccess ? 'green' : 'red'}-800 rounded p-2 bg-${isSuccess ? 'green' : 'red'}-50 dark:bg-${isSuccess ? 'green' : 'red'}-900/20`}>
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className={`text-sm font-medium ${isSuccess ? 'text-green-700 dark:text-green-300' : 'text-red-700 dark:text-red-300'}`}>
              {isSuccess ? '✅' : '❌'} Agent {isSuccess ? 'Completed' : 'Failed'}{' '}
              <span className={`text-xs font-normal ${isSuccess ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
                | Type: {event.agent_type || 'Unknown'} | Status: {isSuccess ? 'Success' : 'Failed'}
                {/* Token usage summary */}
                {(extendedEvent.total_tokens !== undefined && extendedEvent.total_tokens > 0) && (
                  <>
                    {' • Tokens: '}
                    {extendedEvent.prompt_tokens !== undefined && <>Input: {extendedEvent.prompt_tokens.toLocaleString()}</>}
                    {extendedEvent.completion_tokens !== undefined && <> • Output: {extendedEvent.completion_tokens.toLocaleString()}</>}
                    {' • Total: '}
                    <span className="font-semibold">{extendedEvent.total_tokens.toLocaleString()}</span>
                    {extendedEvent.cache_tokens !== undefined && extendedEvent.cache_tokens > 0 && (
                      <span className="text-cyan-600 dark:text-cyan-400">
                        {' • Cache: '}{extendedEvent.cache_tokens.toLocaleString()}
                      </span>
                    )}
                    {extendedEvent.reasoning_tokens !== undefined && extendedEvent.reasoning_tokens > 0 && (
                      <span className="text-purple-600 dark:text-purple-400">
                        {' • Reasoning: '}{extendedEvent.reasoning_tokens.toLocaleString()}
                      </span>
                    )}
                    {extendedEvent.metadata?.context_usage_percent !== undefined && (extendedEvent.metadata.context_usage_percent as number) > 0 && (
                      <span className={(extendedEvent.metadata.context_usage_percent as number) > 80 ? 'text-red-600 dark:text-red-400' : (extendedEvent.metadata.context_usage_percent as number) > 50 ? 'text-yellow-600 dark:text-yellow-400' : 'text-green-600 dark:text-green-400'}>
                        {' • Context: '}{(extendedEvent.metadata.context_usage_percent as number).toFixed(1)}%
                        {extendedEvent.metadata?.fixed_threshold_percent !== undefined && (extendedEvent.metadata.fixed_threshold_percent as number) > 0 && extendedEvent.metadata?.fixed_threshold_tokens !== undefined && (
                          <span className="text-blue-600 dark:text-blue-400">
                            {' / '}{((extendedEvent.metadata.fixed_threshold_tokens as number) / 1000).toFixed(0)}k ({((extendedEvent.metadata.fixed_threshold_percent as number)).toFixed(1)}%)
                          </span>
                        )}
                      </span>
                    )}
                  </>
                )}
              </span>
            </div>
          </div>
        </div>

        {/* Right side: Time and expand button */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <div className={`text-xs ${isSuccess ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}`}>
              {formatTimestamp(event.timestamp)}
            </div>
          )}
          
          {hasExpandableContent && (
            <button 
              onClick={() => setIsExpanded(!isExpanded)}
              className={`${isSuccess ? 'text-green-600 dark:text-green-400 hover:text-green-800 dark:hover:text-green-200' : 'text-red-600 dark:text-red-400 hover:text-red-800 dark:hover:text-red-200'}`}
            >
              {isExpanded ? '▼' : '▶'}
            </button>
          )}
        </div>
      </div>

      {/* Expandable content */}
      {isExpanded && hasExpandableContent && (
        <div className="mt-3 space-y-2">
          {/* Error Information */}
          {event.error && (
            <div className="bg-red-50 dark:bg-red-950/20 border border-red-200 dark:border-red-800 rounded-md p-2">
              <div className="text-xs font-medium text-red-800 dark:text-red-200 mb-1">Error Details:</div>
              <div className="text-xs text-red-700 dark:text-red-300 font-mono">
                {event.error}
              </div>
            </div>
          )}

          {/* Hierarchy Information */}
          {(event.parent_id || event.hierarchy_level !== undefined) && (
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Hierarchy:</div>
              <div className="text-xs text-gray-600 dark:text-gray-400 space-y-1">
                {event.hierarchy_level !== undefined && (
                  <div>Level: <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded">L{event.hierarchy_level}</code></div>
                )}
                {event.parent_id && (
                  <div>Parent ID: <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded">{event.parent_id}</code></div>
                )}
              </div>
            </div>
          )}

          {/* Token Usage Details */}
          {(extendedEvent.total_tokens !== undefined && extendedEvent.total_tokens > 0) && (
            <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-md p-2">
              <div className="text-xs font-medium text-blue-700 dark:text-blue-300 mb-1">Token Usage:</div>
              <div className="text-xs text-blue-600 dark:text-blue-400 space-y-1">
                <div className="flex flex-wrap gap-x-3 gap-y-1">
                  {extendedEvent.prompt_tokens !== undefined && (
                    <span>Input: <span className="font-semibold">{extendedEvent.prompt_tokens.toLocaleString()}</span></span>
                  )}
                  {extendedEvent.completion_tokens !== undefined && (
                    <span>Output: <span className="font-semibold">{extendedEvent.completion_tokens.toLocaleString()}</span></span>
                  )}
                  <span>Total: <span className="font-semibold">{extendedEvent.total_tokens.toLocaleString()}</span></span>
                  {extendedEvent.cache_tokens !== undefined && extendedEvent.cache_tokens > 0 && (
                    <span className="text-cyan-600 dark:text-cyan-400">
                      Cache: {extendedEvent.cache_tokens.toLocaleString()}
                    </span>
                  )}
                  {extendedEvent.reasoning_tokens !== undefined && extendedEvent.reasoning_tokens > 0 && (
                    <span className="text-purple-600 dark:text-purple-400">
                      Reasoning: {extendedEvent.reasoning_tokens.toLocaleString()}
                    </span>
                  )}
                </div>
                {(extendedEvent.llm_call_count !== undefined && extendedEvent.llm_call_count > 0) && (
                  <div className="mt-1 pt-1 border-t border-blue-200 dark:border-blue-700">
                    <div className="flex flex-wrap gap-x-3 gap-y-1">
                      <span>LLM Calls: <span className="font-semibold">{extendedEvent.llm_call_count}</span></span>
                      {extendedEvent.cache_enabled_call_count !== undefined && extendedEvent.cache_enabled_call_count > 0 && (
                        <span>Cache-Enabled: <span className="font-semibold">{extendedEvent.cache_enabled_call_count}</span></span>
                      )}
                      {extendedEvent.average_cache_discount !== undefined && extendedEvent.average_cache_discount > 0 && (
                        <span>Avg Cache Discount: <span className="font-semibold">{(extendedEvent.average_cache_discount * 100).toFixed(1)}%</span></span>
                      )}
                    </div>
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Trace Information */}
          {(event.trace_id || event.span_id || event.event_id) && (
            <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
              <div className="text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">Trace Info:</div>
              <div className="text-xs text-gray-600 dark:text-gray-400 space-y-1">
                {event.trace_id && (
                  <div>Trace: <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded">{event.trace_id}</code></div>
                )}
                {event.span_id && (
                  <div>Span: <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded">{event.span_id}</code></div>
                )}
                {event.event_id && (
                  <div>Event: <code className="bg-gray-100 dark:bg-gray-800 px-1 rounded">{event.event_id}</code></div>
                )}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
