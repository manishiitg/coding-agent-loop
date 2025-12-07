import React from "react";
import type { 
  VariablesExtractedEvent as GeneratedVariablesExtractedEvent,
  Variable 
} from "../../../generated/events-bridge";

interface VariablesExtractedEventDisplayProps {
  event: GeneratedVariablesExtractedEvent;
}

export const VariablesExtractedEventDisplay: React.FC<
  VariablesExtractedEventDisplayProps
> = ({ event }) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return "";
    return new Date(timestamp).toLocaleTimeString();
  };

  const variables: Variable[] = event.variables || [];

  return (
    <div className="bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
      {/* Header */}
      <div className="p-3 border-b border-blue-200 dark:border-blue-800">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <div className="w-7 h-7 bg-blue-100 dark:bg-blue-800/50 rounded-full flex items-center justify-center">
              <span className="text-blue-600 dark:text-blue-400 text-sm">🔑</span>
            </div>
            <div className="text-sm font-medium text-blue-700 dark:text-blue-300">
              Variables Extracted
            </div>
          </div>
          {event.timestamp && (
            <div className="text-xs text-blue-600 dark:text-blue-400">
              {formatTimestamp(event.timestamp)}
            </div>
          )}
        </div>
      </div>

      {/* Content */}
      <div className="p-3">
        {/* Templated Objective */}
        {event.templated_objective && (
          <div className="mb-4">
            <div className="text-xs font-medium text-blue-700 dark:text-blue-400 mb-2">
              Templated Objective:
            </div>
            <div className="bg-white dark:bg-gray-800 border border-blue-200 dark:border-blue-700 rounded-md p-3 text-sm text-gray-700 dark:text-gray-300 font-mono">
              {event.templated_objective}
            </div>
          </div>
        )}

        {/* Variables List */}
        {variables.length > 0 && (
          <div>
            <div className="text-xs font-medium text-blue-700 dark:text-blue-400 mb-2">
              Variables ({variables.length}):
            </div>
            <div className="space-y-2">
              {variables.map((variable, index) => (
                <div
                  key={index}
                  className="bg-white dark:bg-gray-800 border border-blue-200 dark:border-blue-700 rounded-md p-3"
                >
                  <div className="flex items-start gap-2">
                    <div className="w-5 h-5 bg-blue-100 dark:bg-blue-800/50 rounded-full flex items-center justify-center flex-shrink-0 mt-0.5">
                      <span className="text-xs font-medium text-blue-700 dark:text-blue-300">
                        {index + 1}
                      </span>
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="text-sm font-medium text-gray-900 dark:text-gray-100 mb-1">
                        {`{{${variable.name || 'UNNAMED'}}}`}
                      </div>
                      {variable.description && (
                        <div className="text-xs text-gray-600 dark:text-gray-400 mb-1">
                          {variable.description}
                        </div>
                      )}
                      <div className="text-xs">
                        <span className="font-medium text-gray-700 dark:text-gray-300">Value: </span>
                        <span className="text-gray-600 dark:text-gray-400 font-mono">{variable.value || ''}</span>
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {variables.length === 0 && (
          <div className="text-sm text-gray-600 dark:text-gray-400 text-center py-2">
            No variables extracted
          </div>
        )}
      </div>
    </div>
  );
};
