import React, { useState } from 'react';
import type { OrchestratorAgentStartEvent } from '../../../generated/events';
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer';

interface OrchestratorAgentStartEventDisplayProps {
  event: OrchestratorAgentStartEvent;
}

export const OrchestratorAgentStartEventDisplay: React.FC<OrchestratorAgentStartEventDisplayProps> = ({ event }) => {
  const [isExpanded, setIsExpanded] = useState(true);
  
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return '';
    return new Date(timestamp).toLocaleTimeString();
  };

  const hasInputData = event.input_data && Object.keys(event.input_data).length > 0;
  const hasExpandableContent = hasInputData || event.plan_id || event.step_index !== undefined || event.iteration !== undefined;

  const getLabel = () => {
    const t = (event as unknown as { agent_type?: string })?.agent_type
    if (t === 'planning') return 'Planning Agent'
    if (t === 'execution') return 'Execution Agent'
    if (t === 'validation') return 'Validation Agent'
    if (t === 'organizer') return 'Organizer Agent'
    if (t === 'plan_breakdown') return 'Plan Breakdown Agent'
    return 'Agent'
  }

  const getAgentIcon = () => {
    const t = (event as unknown as { agent_type?: string })?.agent_type
    if (t === 'plan_breakdown') return '🔍'
    if (t === 'planning') return '📋'
    if (t === 'execution') return '⚡'
    if (t === 'validation') return '✅'
    if (t === 'organizer') return '🗂️'
    return '🤖'
  }

  const getAgentColor = () => {
    const t = (event as unknown as { agent_type?: string })?.agent_type
    if (t === 'plan_breakdown') return 'green'
    if (t === 'planning') return 'blue'
    if (t === 'execution') return 'purple'
    if (t === 'validation') return 'emerald'
    if (t === 'organizer') return 'orange'
    return 'yellow'
  }

  const agentColor = getAgentColor();
  const agentIcon = getAgentIcon();
  
  const getColorClasses = (color: string) => {
    switch (color) {
      case 'green':
        return {
          bg: 'bg-green-50 dark:bg-green-900/20',
          border: 'border-green-200 dark:border-green-800',
          text: 'text-green-700 dark:text-green-300',
          textSecondary: 'text-green-600 dark:text-green-400',
          hover: 'hover:text-green-800 dark:hover:text-green-200'
        };
      case 'blue':
        return {
          bg: 'bg-blue-50 dark:bg-blue-900/20',
          border: 'border-blue-200 dark:border-blue-800',
          text: 'text-blue-700 dark:text-blue-300',
          textSecondary: 'text-blue-600 dark:text-blue-400',
          hover: 'hover:text-blue-800 dark:hover:text-blue-200'
        };
      case 'purple':
        return {
          bg: 'bg-purple-50 dark:bg-purple-900/20',
          border: 'border-purple-200 dark:border-purple-800',
          text: 'text-purple-700 dark:text-purple-300',
          textSecondary: 'text-purple-600 dark:text-purple-400',
          hover: 'hover:text-purple-800 dark:hover:text-purple-200'
        };
      case 'emerald':
        return {
          bg: 'bg-emerald-50 dark:bg-emerald-900/20',
          border: 'border-emerald-200 dark:border-emerald-800',
          text: 'text-emerald-700 dark:text-emerald-300',
          textSecondary: 'text-emerald-600 dark:text-emerald-400',
          hover: 'hover:text-emerald-800 dark:hover:text-emerald-200'
        };
      case 'orange':
        return {
          bg: 'bg-orange-50 dark:bg-orange-900/20',
          border: 'border-orange-200 dark:border-orange-800',
          text: 'text-orange-700 dark:text-orange-300',
          textSecondary: 'text-orange-600 dark:text-orange-400',
          hover: 'hover:text-orange-800 dark:hover:text-orange-200'
        };
      default:
        return {
          bg: 'bg-yellow-50 dark:bg-yellow-900/20',
          border: 'border-yellow-200 dark:border-yellow-800',
          text: 'text-yellow-700 dark:text-yellow-300',
          textSecondary: 'text-yellow-600 dark:text-yellow-400',
          hover: 'hover:text-yellow-800 dark:hover:text-yellow-200'
        };
    }
  };

  const colors = getColorClasses(agentColor);

  return (
    <div className={`p-2 ${colors.bg} border ${colors.border} rounded`}>
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <div className={`w-6 h-6 ${colors.bg} rounded-full flex items-center justify-center`}>
              <span className="text-sm">{agentIcon}</span>
            </div>
            <div className="min-w-0 flex-1">
              <div className={`text-sm font-medium ${colors.text}`}>
                {getLabel()} Started: {event.agent_name}{' '}
                <span className={`text-xs font-normal ${colors.textSecondary}`}>
                  | Model: {event.model_id} | Servers: {event.servers_count} | Max Turns: {event.max_turns}
                  {event.step_index !== undefined && ` | Step: ${event.step_index}`}
                </span>
              </div>
            </div>
          </div>
        </div>
        
        {/* Right side: Time and expand button */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <div className={`text-xs ${colors.textSecondary}`}>
              {formatTimestamp(event.timestamp)}
            </div>
          )}
          
          {hasExpandableContent && (
            <button 
              onClick={() => setIsExpanded(!isExpanded)}
              className={`${colors.textSecondary} ${colors.hover}`}
            >
              {isExpanded ? '▼' : '▶'}
            </button>
          )}
        </div>
      </div>

      {/* Objective content - always visible with markdown rendering */}
      {event.objective && (
        <div className="mt-3">
          <div className={`text-xs font-medium ${colors.textSecondary} mb-2`}>Objective:</div>
          <ConversationMarkdownRenderer content={event.objective} maxHeight="400px" />
        </div>
      )}

      {/* Expandable content */}
      {isExpanded && hasExpandableContent && (
        <div className="mt-3 space-y-3">
          {/* Input Data */}
          {hasInputData && (
            <div>
              <div className={`text-xs font-medium ${colors.textSecondary} mb-2`}>Input Data:</div>
              <div className={`${colors.bg} rounded p-3 text-sm`}>
                {/* Step Number - Highlighted */}
                {event.input_data?.step_number && (
                  <div className="mb-3 p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-700 rounded">
                    <div className="text-xs font-bold text-blue-700 dark:text-blue-300">
                      Step #{event.input_data.step_number}
                    </div>
                  </div>
                )}
                {Object.entries(event.input_data || {})
                  .filter(([key]) => key !== 'step_number')
                  .map(([key, value]) => (
                    <div key={key} className="mb-2 last:mb-0">
                      <div className={`font-medium ${colors.text} mb-1`}>{key}:</div>
                      <div className={colors.textSecondary}>
                        <ConversationMarkdownRenderer 
                          content={value} 
                          maxHeight="200px" 
                        />
                      </div>
                    </div>
                  ))}
              </div>
            </div>
          )}

          {/* Additional metadata */}
          <div className={`text-xs ${colors.textSecondary} space-y-1`}>
            {event.plan_id && (
              <div>Plan ID: {event.plan_id}</div>
            )}
            {event.step_index !== undefined && (
              <div>Step Index: {event.step_index}</div>
            )}
            {event.iteration !== undefined && (
              <div>Iteration: {event.iteration}</div>
            )}
          </div>
        </div>
      )}

    </div>
  );
};

