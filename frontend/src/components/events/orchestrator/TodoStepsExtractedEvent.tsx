import React from "react";
import type { TodoStepsExtractedEvent } from "../../../generated/events";

interface TodoStepsExtractedEventDisplayProps {
  event: TodoStepsExtractedEvent;
}

export const TodoStepsExtractedEventDisplay: React.FC<
  TodoStepsExtractedEventDisplayProps
> = ({ event }) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return "";
    return new Date(timestamp).toLocaleTimeString();
  };

  return (
    <div className="p-2 bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-green-700 dark:text-green-300">
              📋 Todo Steps Extracted{" "}
              <span className="text-xs font-normal text-green-600 dark:text-green-400">
                | Steps: {event.total_steps_extracted || 0}
                {event.extraction_method && ` | Method: ${event.extraction_method}`}
              </span>
            </div>
          </div>
        </div>

        {/* Right side: Timestamp */}
        {event.timestamp && (
          <div className="text-xs text-green-500 dark:text-green-400 whitespace-nowrap">
            {formatTimestamp(event.timestamp)}
          </div>
        )}
      </div>

      {/* Steps List */}
      {event.extracted_steps && event.extracted_steps.length > 0 && (
        <div className="mt-2 space-y-1">
          {event.extracted_steps.map((step, index) => (
            <div
              key={index}
              className="text-xs text-green-600 dark:text-green-400 bg-green-100 dark:bg-green-800/30 px-2 py-1 rounded"
            >
              <div className="font-medium">{step.title || `Step ${index + 1}`}</div>
              {step.description && (
                <div className="text-green-500 dark:text-green-500 mt-0.5">
                  {step.description}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
};
