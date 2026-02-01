import React from 'react'
import type { SmartRoutingEndEvent } from '../../../generated/events'
import { formatDuration } from '../../../utils/duration'
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer'
import { useExpandable } from '../useExpandable'
import { Plus, Minus } from 'lucide-react'

interface SmartRoutingEndEventDisplayProps {
  event: SmartRoutingEndEvent
}

export const SmartRoutingEndEventDisplay: React.FC<SmartRoutingEndEventDisplayProps> = ({
  event
}) => {
  const { 
    total_tools, 
    filtered_tools, 
    total_servers, 
    relevant_servers, 
    routing_duration, 
    success, 
    error 
  } = event
  const { isExpanded, toggle } = useExpandable()

  const hasExpandableContent = event.llm_response || event.selected_servers || event.routing_reasoning || event.llm_model_id || event.llm_provider

  const theme = success ? 'indigo' : 'red'
  const bgColor = theme === 'indigo' ? 'bg-indigo-50 dark:bg-indigo-900/20' : 'bg-red-50 dark:bg-red-900/20'
  const borderColor = theme === 'indigo' ? 'border-indigo-200 dark:border-indigo-800' : 'border-red-200 dark:border-red-800'
  const textColor = theme === 'indigo' ? 'text-indigo-700 dark:text-indigo-300' : 'text-red-700 dark:text-red-300'
  const textSecondaryColor = theme === 'indigo' ? 'text-indigo-600 dark:text-indigo-400' : 'text-red-600 dark:text-red-400'

  return (
    <div className={`${bgColor} border ${borderColor} rounded p-2`}>
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className={`text-sm font-medium ${textColor}`}>
              Smart Routing {success ? 'Completed' : 'Failed'}{' '}
              <span className={`text-xs font-normal ${textSecondaryColor}`}>
                | Tools: {total_tools || 0} → {filtered_tools || 0} | Servers: {total_servers || 0} → {relevant_servers?.length || 0}
                {routing_duration && ` | Duration: ${formatDuration(routing_duration)}`}
                {event.llm_model_id && ` | LLM: ${event.llm_model_id}`}
                {event.llm_provider && ` (${event.llm_provider})`}
              </span>
            </div>
          </div>
        </div>

        {/* Right side: Time and expand button */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <div className={`text-xs ${textSecondaryColor}`}>
              {new Date(event.timestamp).toLocaleTimeString()}
            </div>
          )}
          
          {hasExpandableContent && (
            <button
              onClick={toggle}
              className={`p-0.5 hover:bg-${theme}-200 dark:hover:bg-${theme}-800 rounded ${textColor} transition-colors`}
              title={isExpanded ? "Collapse details (Alt+Click for all)" : "Expand details (Alt+Click for all)"}
            >
              {isExpanded ? <Minus className="w-4 h-4" /> : <Plus className="w-4 h-4" />}
            </button>
          )}
        </div>
      </div>
      
      {/* Error display */}
      {error && (
        <div className="mt-2 text-xs text-red-600 dark:text-red-400">
          Error: {error.length > 80 ? `${error.substring(0, 80)}...` : error}
        </div>
      )}
      
      {/* Expanded LLM Details */}
      {isExpanded && hasExpandableContent && (
        <div className="mt-3 space-y-3 border-t border-gray-200 dark:border-gray-700 pt-3">
          {/* LLM Configuration */}
          {(event.llm_model_id || event.llm_provider || event.llm_temperature || event.llm_max_tokens) && (
            <div>
              <div className={`text-xs font-medium ${textColor} mb-1`}>⚙️ LLM Configuration:</div>
              <div className={`text-xs ${textSecondaryColor} space-y-1`}>
                {event.llm_model_id && <div>Model: <span className="font-mono">{event.llm_model_id}</span></div>}
                {event.llm_provider && <div>Provider: <span className="font-mono">{event.llm_provider}</span></div>}
                {event.llm_temperature !== undefined && <div>Temperature: <span className="font-mono">{event.llm_temperature}</span></div>}
                {event.llm_max_tokens && <div>Max Tokens: <span className="font-mono">{event.llm_max_tokens}</span></div>}
              </div>
            </div>
          )}
          
          {event.routing_reasoning && (
            <div>
              <div className={`text-xs font-medium ${textColor} mb-1`}>🧠 Routing Reasoning:</div>
              <ConversationMarkdownRenderer content={event.routing_reasoning} />
            </div>
          )}
          
          {event.llm_response && (
            <div>
              <div className={`text-xs font-medium ${textColor} mb-1`}>🤖 LLM Response:</div>
              <ConversationMarkdownRenderer content={event.llm_response} />
            </div>
          )}
          
          {event.selected_servers && (
            <div>
              <div className={`text-xs font-medium ${textColor} mb-1`}>Server Selection:</div>
              <ConversationMarkdownRenderer content={event.selected_servers} />
            </div>
          )}
        </div>
      )}
    </div>
  )
}