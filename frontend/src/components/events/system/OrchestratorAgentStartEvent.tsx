import React from 'react';
import type { OrchestratorAgentStartEvent } from '../../../generated/events';
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer';
import { useExpandable } from '../useExpandable';
import { Plus, Minus } from 'lucide-react';

interface OrchestratorAgentStartEventDisplayProps {
  event: OrchestratorAgentStartEvent;
  isCollapsed?: boolean;
  eventCount?: number;
  onToggleCollapse?: () => void;
}

export const OrchestratorAgentStartEventDisplay: React.FC<OrchestratorAgentStartEventDisplayProps> = ({ event, isCollapsed, eventCount, onToggleCollapse }) => {
  const { isExpanded: isInputsExpanded, toggle } = useExpandable(true)

  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return '';
    return new Date(timestamp).toLocaleTimeString();
  };

  const hasInputData = event.input_data && Object.keys(event.input_data).length > 0;

  const getLabel = () => {
    const t = (event as unknown as { agent_type?: string })?.agent_type
    if (t === 'planning') return 'Planning Agent'
    if (t === 'execution') return 'Execution Agent'
    if (t === 'validation') return 'Validation Agent'
    if (t === 'organizer') return 'Organizer Agent'
    if (t === 'plan_breakdown') return 'Plan Breakdown Agent'
    if (t === 'conditional') return 'Conditional LLM'
    return 'Agent'
  }

  const getAgentIcon = () => {
    const t = (event as unknown as { agent_type?: string })?.agent_type
    if (t === 'plan_breakdown') return '🔍'
    if (t === 'planning') return '📋'
    if (t === 'execution') return '⚡'
    if (t === 'validation') return '✅'
    if (t === 'organizer') return '🗂️'
    if (t === 'conditional') return '🔀'
    return '🤖'
  }

  const getAgentColor = () => {
    const t = (event as unknown as { agent_type?: string })?.agent_type
    if (t === 'plan_breakdown') return 'emerald'
    if (t === 'planning') return 'blue'
    if (t === 'execution') return 'purple'
    if (t === 'validation') return 'emerald'
    if (t === 'organizer') return 'orange'
    if (t === 'conditional') return 'indigo'
    return 'yellow'
  }

  const agentColor = getAgentColor();
  const agentIcon = getAgentIcon();
  
  const getColorClasses = (color: string) => {
    switch (color) {
      case 'emerald':
        return {
          bg: 'bg-emerald-50 dark:bg-emerald-900/20',
          border: 'border-emerald-200 dark:border-emerald-800',
          text: 'text-emerald-700 dark:text-emerald-300',
          textSecondary: 'text-emerald-600 dark:text-emerald-400',
          hover: 'hover:text-emerald-800 dark:hover:text-emerald-200'
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
      case 'orange':
        return {
          bg: 'bg-orange-50 dark:bg-orange-900/20',
          border: 'border-orange-200 dark:border-orange-800',
          text: 'text-orange-700 dark:text-orange-300',
          textSecondary: 'text-orange-600 dark:text-orange-400',
          hover: 'hover:text-orange-800 dark:hover:text-orange-200'
        };
      case 'indigo':
        return {
          bg: 'bg-indigo-50 dark:bg-indigo-900/20',
          border: 'border-indigo-200 dark:border-indigo-800',
          text: 'text-indigo-700 dark:text-indigo-300',
          textSecondary: 'text-indigo-600 dark:text-indigo-400',
          hover: 'hover:text-indigo-800 dark:hover:text-indigo-200'
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
  const hasExpandableContent = event.objective || (event.input_data && event.input_data.context) || hasInputData;

  return (
    <div className={`p-2 ${colors.bg} border ${colors.border} rounded transition-all duration-200`}>
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <div className={`w-6 h-6 ${colors.bg} rounded-full flex items-center justify-center flex-shrink-0`}>
              <span className="text-sm">{agentIcon}</span>
            </div>
            <div className="min-w-0 flex-1">
              <div className={`text-sm font-medium ${colors.text}`}>
                {getLabel()} Started: {event.agent_name}
                <span className={`text-xs font-normal ${colors.textSecondary}`}>
                  {event.use_code_execution_mode && ' | Code Exec'}
                  {event.use_tool_search_mode && ' | Tool Search'}
                  {' '}| Model: {event.model_id} | Servers: {event.servers_count} | Max Turns: {event.max_turns}
                  {event.step_index !== undefined && ` | Step: ${event.step_index}`}
                </span>
                {isCollapsed && eventCount !== undefined && (
                  <span className={`text-xs font-normal ${colors.textSecondary}`}> | {eventCount} events collapsed</span>
                )}
              </div>
            </div>
          </div>
        </div>
        
        {/* Right side: Time and expand buttons */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <div className={`text-xs ${colors.textSecondary}`}>
              {formatTimestamp(event.timestamp)}
            </div>
          )}

          {/* Toggle for inputs/details */}
          {!isCollapsed && hasExpandableContent && (
            <button
              onClick={toggle}
              className={`p-0.5 ${colors.hover} rounded ${colors.text} transition-colors flex items-center gap-1`}
              title={isInputsExpanded ? "Collapse inputs (Alt+Click for all)" : "Expand inputs (Alt+Click for all)"}
            >
              <span className="text-[10px] uppercase font-bold">Inputs</span>
              {isInputsExpanded ? <Minus className="w-3 h-3" /> : <Plus className="w-3 h-3" />}
            </button>
          )}
          
          {/* Session collapse/expand button */}
          {onToggleCollapse && (
            <button 
              onClick={onToggleCollapse}
              className={`${colors.textSecondary} ${colors.hover} px-1`}
              aria-label={isCollapsed ? 'Expand session' : 'Collapse session'}
              title={isCollapsed ? 'Expand session' : 'Collapse session'}
            >
              {isCollapsed ? <Plus className="w-4 h-4" /> : <Minus className="w-4 h-4" />}
            </button>
          )}
        </div>
      </div>

      {/* Objective - always visible when not collapsed */}
      {!isCollapsed && event.objective && (
        <div className={`mt-2 text-xs ${colors.textSecondary}`}>
          <ConversationMarkdownRenderer content={event.objective} maxHeight="200px" />
        </div>
      )}

      {/* Expandable content - only show when not collapsed AND inputs expanded */}
      {!isCollapsed && isInputsExpanded && (
        <div className="mt-3 space-y-3">
          {/* Objective is always shown above; only show here if inputs are expanded for full height */}

          {/* Context for conditional agents - show prominently after objective */}
          {(event as unknown as { agent_type?: string })?.agent_type === 'conditional' && event.input_data?.context && (
            <div>
              <div className={`text-xs font-medium ${colors.textSecondary} mb-2`}>Context:</div>
              <div className={`${colors.bg} rounded p-3 text-sm border ${colors.border}`}>
                <ConversationMarkdownRenderer 
                  content={event.input_data.context} 
                  maxHeight="400px" 
                />
              </div>
            </div>
          )}

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
                  .filter(([key]) => key !== 'step_number' && key !== 'context') // Exclude context from general input data for conditional agents
                  .filter(([, value]) => {
                    // Filter out empty values: null, undefined, empty string
                    if (value === null || value === undefined || value === '') return false;
                    // For string values, check if trimmed string is empty
                    if (typeof value === 'string' && value.trim() === '') return false;
                    return true;
                  })
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

