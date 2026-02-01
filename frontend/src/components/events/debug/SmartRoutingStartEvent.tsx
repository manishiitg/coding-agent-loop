import React from 'react'
import type { SmartRoutingStartEvent } from '../../../generated/events'
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer'
import { useExpandable } from '../useExpandable'
import { Plus, Minus } from 'lucide-react'

interface SmartRoutingStartEventDisplayProps {
  event: SmartRoutingStartEvent
}

export const SmartRoutingStartEventDisplay: React.FC<SmartRoutingStartEventDisplayProps> = ({
  event
}) => {
  const { total_tools, total_servers, thresholds } = event
  const { isExpanded, toggle } = useExpandable()

  const hasExpandableContent = event.llm_prompt || event.user_query || event.conversation_context || event.llm_model_id || event.llm_provider

  return (
    <div className="bg-indigo-50 dark:bg-indigo-900/20 border border-indigo-200 dark:border-indigo-800 rounded p-2">
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-indigo-700 dark:text-indigo-300">
              Smart Routing Started{' '}
              <span className="text-xs font-normal text-indigo-600 dark:text-indigo-400">
                | Tools: {total_tools || 0} | Servers: {total_servers || 0} | Thresholds: tools&gt;{thresholds?.max_tools || 0}, servers&gt;{thresholds?.max_servers || 0}
                {event.llm_model_id && ` | LLM: ${event.llm_model_id}`}
                {event.llm_provider && ` (${event.llm_provider})`}
              </span>
            </div>
          </div>
        </div>

        {/* Right side: Time and expand button */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <div className="text-xs text-indigo-600 dark:text-indigo-400">
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
          
          {hasExpandableContent && (
            <button
              onClick={toggle}
              className="p-0.5 hover:bg-indigo-200 dark:hover:bg-indigo-800 rounded text-indigo-600 dark:text-indigo-400 transition-colors"
              title={isExpanded ? "Collapse details (Alt+Click for all)" : "Expand details (Alt+Click for all)"}
            >
              {isExpanded ? <Minus className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
            </button>
          )}
        </div>
      </div>
      
      {/* Expanded LLM Details */}
      {isExpanded && hasExpandableContent && (
        <div className="mt-3 space-y-3 border-t border-gray-200 dark:border-gray-700 pt-3">
          {/* LLM Configuration */}
          {(event.llm_model_id || event.llm_provider || event.llm_temperature || event.llm_max_tokens) && (
            <div>
              <div className="text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">⚙️ LLM Configuration:</div>
              <div className="text-xs text-gray-600 dark:text-gray-400 space-y-1">
                {event.llm_model_id && <div>Model: <span className="font-mono">{event.llm_model_id}</span></div>}
                {event.llm_provider && <div>Provider: <span className="font-mono">{event.llm_provider}</span></div>}
                {event.llm_temperature !== undefined && <div>Temperature: <span className="font-mono">{event.llm_temperature}</span></div>}
                {event.llm_max_tokens && <div>Max Tokens: <span className="font-mono">{event.llm_max_tokens}</span></div>}
              </div>
            </div>
          )}
          
          {event.llm_prompt && (
            <div>
              <div className="text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">🤖 LLM Input:</div>
              <ConversationMarkdownRenderer content={event.llm_prompt} />
            </div>
          )}
          
          {event.user_query && (
            <div>
              <div className="text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">👤 User Query:</div>
              <ConversationMarkdownRenderer content={event.user_query} />
            </div>
          )}
          
          {event.conversation_context && (
            <div>
              <div className="text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">💬 Context:</div>
              <ConversationMarkdownRenderer content={event.conversation_context} />
            </div>
          )}
        </div>
      )}
    </div>
  )
}
