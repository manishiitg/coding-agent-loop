import React from 'react';
import type { OrchestratorAgentEndEvent } from '../../../generated/events';
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer';
import { formatDuration } from '../../../utils/duration';

interface OrchestratorAgentEndEventDisplayProps {
  event: OrchestratorAgentEndEvent;
}

export const OrchestratorAgentEndEventDisplay: React.FC<OrchestratorAgentEndEventDisplayProps> = ({ event }) => {
  // Hide workshop wrapper end events — these are signal-only events for auto-notification,
  // the actual agent completion is already shown by the inner agent's end event
  const agentType = (event as unknown as { agent_type?: string })?.agent_type
  if (agentType === 'workshop-step-execution' || agentType === 'workshop-step-debug' || agentType === 'workshop-step-learning') {
    return null
  }

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
          textSecondary: 'text-emerald-600 dark:text-emerald-400'
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
      case 'orange':
        return {
          bg: 'bg-orange-50 dark:bg-orange-900/20',
          border: 'border-orange-200 dark:border-orange-800',
          text: 'text-orange-700 dark:text-orange-300',
          textSecondary: 'text-orange-600 dark:text-orange-400'
        };
      case 'indigo':
        return {
          bg: 'bg-indigo-50 dark:bg-indigo-900/20',
          border: 'border-indigo-200 dark:border-indigo-800',
          text: 'text-indigo-700 dark:text-indigo-300',
          textSecondary: 'text-indigo-600 dark:text-indigo-400'
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
                  | Duration: {formatDuration(event.duration || 0)}
                  {event.step_index !== undefined && ` | Step: ${event.step_index}`}
                  {event.iteration !== undefined && ` | Iteration: ${event.iteration}`}
                  {/* Token usage summary - check if token fields exist */}
                  {(() => {
                    const eventWithTokens = event as OrchestratorAgentEndEvent & {
                      total_tokens?: number
                      prompt_tokens?: number
                      completion_tokens?: number
                      cache_tokens?: number
                      reasoning_tokens?: number
                    }
                    if (eventWithTokens.total_tokens !== undefined && eventWithTokens.total_tokens > 0) {
                      const contextUsagePercent = event.metadata?.context_usage_percent as number | undefined
                      const fixedThresholdPercent = event.metadata?.fixed_threshold_percent as number | undefined
                      const fixedThresholdTokens = event.metadata?.fixed_threshold_tokens as number | undefined
                      return (
                        <>
                          {' • Tokens: '}
                          {eventWithTokens.prompt_tokens !== undefined && <>Input: {eventWithTokens.prompt_tokens.toLocaleString()}</>}
                          {eventWithTokens.completion_tokens !== undefined && <> • Output: {eventWithTokens.completion_tokens.toLocaleString()}</>}
                          {' • Total: '}
                          <span className="font-semibold">{eventWithTokens.total_tokens.toLocaleString()}</span>
                          {eventWithTokens.cache_tokens !== undefined && eventWithTokens.cache_tokens > 0 && (
                            <span className="text-sky-600 dark:text-sky-400">
                              {' • Cache: '}{eventWithTokens.cache_tokens.toLocaleString()}
                            </span>
                          )}
                          {eventWithTokens.reasoning_tokens !== undefined && eventWithTokens.reasoning_tokens > 0 && (
                            <span className="text-purple-600 dark:text-purple-400">
                              {' • Reasoning: '}{eventWithTokens.reasoning_tokens.toLocaleString()}
                            </span>
                          )}
                          {contextUsagePercent !== undefined && contextUsagePercent > 0 && (
                            <span className={contextUsagePercent > 80 ? 'text-red-600 dark:text-red-400' : contextUsagePercent > 50 ? 'text-yellow-600 dark:text-yellow-400' : 'text-green-600 dark:text-green-400'}>
                              {' • Context: '}{contextUsagePercent.toFixed(1)}%
                              {fixedThresholdPercent !== undefined && fixedThresholdPercent > 0 && fixedThresholdTokens !== undefined && (
                                <span className="text-blue-600 dark:text-blue-400">
                                  {' / '}{(fixedThresholdTokens / 1000).toFixed(0)}k ({fixedThresholdPercent.toFixed(1)}%)
                                </span>
                              )}
                            </span>
                          )}
                        </>
                      )
                    }
                    return null
                  })()}
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
      {/* Commented out: Only show final result, not inputs */}
      {/* 
      {event.input_data && Object.keys(event.input_data).length > 0 && (
        <div className="mt-3">
          <div className={`text-xs font-medium ${colors.textSecondary} mb-2`}>Input Data:</div>
          <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-3">
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
                .filter(([, value]) => {
                  if (value === null || value === undefined || value === '') return false;
                  if (typeof value === 'string' && value.trim() === '') return false;
                  return true;
                })
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
      */}

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

