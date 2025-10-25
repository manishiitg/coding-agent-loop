import React from 'react';
import type { OrchestratorAgentEndEvent } from '../../../generated/events';
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer';

interface OrchestratorAgentEndEventDisplayProps {
  event: OrchestratorAgentEndEvent;
}

export const OrchestratorAgentEndEventDisplay: React.FC<OrchestratorAgentEndEventDisplayProps> = ({ event }) => {
  const formatDuration = (duration?: number) => {
    if (!duration) return '0ms';
    if (duration < 1000) return `${duration}ms`;
    return `${(duration / 1000).toFixed(1)}s`;
  };

  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return '';
    return new Date(timestamp).toLocaleTimeString();
  };

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
          textSecondary: 'text-green-600 dark:text-green-400'
        };
      case 'blue':
        return {
          bg: 'bg-blue-50 dark:bg-blue-900/20',
          border: 'border-blue-200 dark:border-blue-800',
          text: 'text-blue-700 dark:text-blue-300',
          textSecondary: 'text-blue-600 dark:text-blue-400'
        };
      case 'purple':
        return {
          bg: 'bg-purple-50 dark:bg-purple-900/20',
          border: 'border-purple-200 dark:border-purple-800',
          text: 'text-purple-700 dark:text-purple-300',
          textSecondary: 'text-purple-600 dark:text-purple-400'
        };
      case 'emerald':
        return {
          bg: 'bg-emerald-50 dark:bg-emerald-900/20',
          border: 'border-emerald-200 dark:border-emerald-800',
          text: 'text-emerald-700 dark:text-emerald-300',
          textSecondary: 'text-emerald-600 dark:text-emerald-400'
        };
      case 'orange':
        return {
          bg: 'bg-orange-50 dark:bg-orange-900/20',
          border: 'border-orange-200 dark:border-orange-800',
          text: 'text-orange-700 dark:text-orange-300',
          textSecondary: 'text-orange-600 dark:text-orange-400'
        };
      default:
        return {
          bg: 'bg-yellow-50 dark:bg-yellow-900/20',
          border: 'border-yellow-200 dark:border-yellow-800',
          text: 'text-yellow-700 dark:text-yellow-300',
          textSecondary: 'text-yellow-600 dark:text-yellow-400'
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
                {getLabel()} Completed: {event.agent_name}{' '}
                <span className={`text-xs font-normal ${colors.textSecondary}`}>
                  | Duration: {formatDuration(event.duration)}
                  {event.step_index !== undefined && ` | Step: ${event.step_index}`}
                  {event.iteration !== undefined && ` | Iteration: ${event.iteration}`}
                </span>
              </div>
            </div>
          </div>
        </div>
        
        {/* Right side: Time */}
        {event.timestamp && (
          <div className={`text-xs ${colors.textSecondary} flex-shrink-0`}>
            {formatTimestamp(event.timestamp)}
          </div>
        )}
      </div>

      {/* Objective content - always visible with markdown rendering */}
      {event.objective && (
        <div className="mt-3">
          <div className={`text-xs font-medium ${colors.textSecondary} mb-2`}>Objective:</div>
          <ConversationMarkdownRenderer content={event.objective} maxHeight="400px" />
        </div>
      )}

      {/* Input Data content - template variables passed to agent */}
      {event.input_data && Object.keys(event.input_data).length > 0 && (
        <div className="mt-3">
          <div className={`text-xs font-medium ${colors.textSecondary} mb-2`}>Input Data:</div>
          <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-3">
            {/* Step Number - Highlighted */}
            {event.input_data.step_number && (
              <div className="mb-3 p-2 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-700 rounded">
                <div className="text-xs font-bold text-blue-700 dark:text-blue-300">
                  Step #{event.input_data.step_number}
                </div>
              </div>
            )}
            <div className="space-y-2">
              {Object.entries(event.input_data)
                .filter(([key]) => key !== 'step_number')
                .map(([key, value]) => (
                  <div key={key} className="flex flex-col gap-1">
                    <div className="text-xs font-medium text-gray-700 dark:text-gray-300">
                      {key}:
                    </div>
                    <div className="text-xs text-gray-600 dark:text-gray-400 bg-gray-50 dark:bg-gray-900 rounded p-2 max-h-32 overflow-y-auto">
                      <ConversationMarkdownRenderer content={String(value)} maxHeight="120px" />
                    </div>
                  </div>
                ))}
            </div>
          </div>
        </div>
      )}

      {/* Result content - always visible with markdown rendering */}
      {event.result && (
        <div className="mt-3">
          <div className="text-xs font-medium text-yellow-600 dark:text-yellow-400 mb-2">Result:</div>
          <ConversationMarkdownRenderer content={event.result} maxHeight="400px" />
        </div>
      )}
    </div>
  );
};

