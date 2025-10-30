import React from 'react';
import type { OrchestratorAgentErrorEvent } from '../../../generated/events';
import { ConversationMarkdownRenderer } from '../../ui/MarkdownRenderer';

interface OrchestratorAgentErrorEventDisplayProps {
  event: OrchestratorAgentErrorEvent;
}

export const OrchestratorAgentErrorEventDisplay: React.FC<OrchestratorAgentErrorEventDisplayProps> = ({ event }) => {
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
    return 'Agent'
  }

  return (
    <div className="p-2 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-yellow-700 dark:text-yellow-300">
              {getLabel()} Error: {event.agent_name}{' '}
              <span className="text-xs font-normal text-yellow-600 dark:text-yellow-400">
                | Duration: {formatDuration(event.duration)}
                {event.execution_mode && ` | Mode: ${event.execution_mode === 'parallel_execution' ? 'Parallel' : 'Sequential'}`}
                {event.step_index !== undefined && ` | Step: ${event.step_index + 1}`}
                {event.iteration !== undefined && ` | Iteration: ${event.iteration + 1}`}
              </span>
            </div>
          </div>
        </div>
        
        {/* Right side: Time */}
        {event.timestamp && (
          <div className="text-xs text-yellow-600 dark:text-yellow-400 flex-shrink-0">
            {formatTimestamp(event.timestamp)}
          </div>
        )}
      </div>

      {/* Objective content - always visible with markdown rendering */}
      {event.objective && (
        <div className="mt-3">
          <div className="text-xs font-medium text-yellow-600 dark:text-yellow-400 mb-2">Objective:</div>
          <ConversationMarkdownRenderer content={event.objective} maxHeight="400px" />
        </div>
      )}

      {/* Error content - always visible with markdown rendering */}
      {event.error && (
        <div className="mt-3">
          <div className="text-xs font-medium text-yellow-600 dark:text-yellow-400 mb-2">Error Details:</div>
          <ConversationMarkdownRenderer content={event.error} maxHeight="400px" />
        </div>
      )}

    </div>
  );
};

