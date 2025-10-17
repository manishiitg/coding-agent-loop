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

  return (
    <div className="p-2 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-yellow-700 dark:text-yellow-300">
              Deep Search Agent Started: {event.agent_name}{' '}
              <span className="text-xs font-normal text-yellow-600 dark:text-yellow-400">
                | Model: {event.model_id} | Servers: {event.servers_count} | Max Turns: {event.max_turns}
                {event.execution_mode && ` | Mode: ${event.execution_mode === 'parallel_execution' ? 'Parallel' : 'Sequential'}`}
                {event.step_index !== undefined && ` | Step: ${event.step_index}`}
              </span>
            </div>
          </div>
        </div>
        
        {/* Right side: Time and expand button */}
        <div className="flex items-center gap-2 flex-shrink-0">
          {event.timestamp && (
            <div className="text-xs text-yellow-600 dark:text-yellow-400">
              {formatTimestamp(event.timestamp)}
            </div>
          )}
          
          {hasExpandableContent && (
            <button 
              onClick={() => setIsExpanded(!isExpanded)}
              className="text-yellow-600 dark:text-yellow-400 hover:text-yellow-800 dark:hover:text-yellow-200"
            >
              {isExpanded ? '▼' : '▶'}
            </button>
          )}
        </div>
      </div>

      {/* Objective content - always visible with markdown rendering */}
      {event.objective && (
        <div className="mt-3">
          <div className="text-xs font-medium text-yellow-600 dark:text-yellow-400 mb-2">Objective:</div>
          <ConversationMarkdownRenderer content={event.objective} maxHeight="400px" />
        </div>
      )}

      {/* Expandable content */}
      {isExpanded && hasExpandableContent && (
        <div className="mt-3 space-y-3">
          {/* Input Data */}
          {hasInputData && (
            <div>
              <div className="text-xs font-medium text-yellow-600 dark:text-yellow-400 mb-2">Input Data:</div>
              <div className="bg-yellow-100 dark:bg-yellow-800/30 rounded p-3 text-sm">
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
                      <div className="font-medium text-yellow-700 dark:text-yellow-300 mb-1">{key}:</div>
                      <div className="text-yellow-600 dark:text-yellow-400">
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
          <div className="text-xs text-yellow-600 dark:text-yellow-400 space-y-1">
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

