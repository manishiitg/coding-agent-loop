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

  return (
    <div className="p-2 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-yellow-700 dark:text-yellow-300">
              Deep Search Agent Completed: {event.agent_name}{' '}
              <span className="text-xs font-normal text-yellow-600 dark:text-yellow-400">
                | Duration: {formatDuration(event.duration)}
                {event.execution_mode && ` | Mode: ${event.execution_mode === 'parallel_execution' ? 'Parallel' : 'Sequential'}`}
                {event.step_index !== undefined && ` | Step: ${event.step_index}`}
                {event.iteration !== undefined && ` | Iteration: ${event.iteration}`}
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

      {/* Input Data content - template variables passed to agent */}
      {event.input_data && Object.keys(event.input_data).length > 0 && (
        <div className="mt-3">
          <div className="text-xs font-medium text-yellow-600 dark:text-yellow-400 mb-2">Input Data:</div>
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

