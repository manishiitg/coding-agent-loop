import React, { useMemo, useState } from 'react';
import { Key } from 'lucide-react';
import { useChatStore } from '../../stores';
import type { PollingEvent } from '../../services/api-types';
import { VariablesModal } from './VariablesModal';

interface Variable {
  name: string;
  value: string;
  description: string;
}

interface VariablesExtractedEventData {
  variables?: Variable[];
  templated_objective?: string;
  workspace_path?: string;
  run_folder?: string;
}

interface VariablesIconProps {
  onSubmitQuery?: (query: string) => void;
}

export const VariablesIcon: React.FC<VariablesIconProps> = ({ onSubmitQuery }) => {
  const { events } = useChatStore();
  const [isModalOpen, setIsModalOpen] = useState(false);

  // Find the latest variables_extracted event
  const latestVariablesEvent = useMemo(() => {
    const variablesEvents = events.filter(
      (e: PollingEvent) => e.type === 'variables_extracted'
    );
    if (variablesEvents.length === 0) return null;
    
    // Get the most recent event (last in array since events are chronological)
    const latest = variablesEvents[variablesEvents.length - 1];
    // Event data might be nested: event.data.data or event.data
    const eventData = latest.data as VariablesExtractedEventData | { data?: VariablesExtractedEventData };
    return ('data' in eventData && eventData.data) ? eventData.data : (eventData as VariablesExtractedEventData);
  }, [events]);

  const variableCount = latestVariablesEvent?.variables?.length || 0;

  // Don't show icon if no variables
  if (variableCount === 0) {
    return null;
  }

  return (
    <>
      <div className="absolute top-4 right-4 z-50">
        <button
          onClick={() => setIsModalOpen(true)}
          className="flex items-center gap-2 px-3 py-2 bg-blue-100 dark:bg-blue-900/40 hover:bg-blue-200 dark:hover:bg-blue-900/60 border border-blue-300 dark:border-blue-700 rounded-md shadow-sm transition-colors"
          title={`${variableCount} variable${variableCount !== 1 ? 's' : ''} extracted - Click to view/edit`}
        >
          <Key className="w-4 h-4 text-blue-700 dark:text-blue-300" />
          <span className="text-sm font-medium text-blue-700 dark:text-blue-300">
            {variableCount}
          </span>
        </button>
      </div>
      
      {isModalOpen && latestVariablesEvent && (
        <VariablesModal
          isOpen={isModalOpen}
          onClose={() => setIsModalOpen(false)}
          variables={latestVariablesEvent.variables || []}
          templatedObjective={latestVariablesEvent.templated_objective || ''}
          workspacePath={latestVariablesEvent.workspace_path || ''}
          onExtractAgain={onSubmitQuery ? () => {
            // Submit query to extract new variables
            onSubmitQuery('Extract new variables from the objective');
          } : undefined}
          onUpdateVariables={onSubmitQuery ? () => {
            // Submit query to update variables with current values
            const currentVars = latestVariablesEvent.variables || [];
            const feedback = `Update variables with the following values:\n\n${currentVars.map(v => `${v.name}=${v.value}`).join('\n')}\n\nObjective: ${latestVariablesEvent.templated_objective || ''}`;
            onSubmitQuery(feedback);
          } : undefined}
        />
      )}
    </>
  );
};

