import React from "react";

// Local type definition for IndependentStepsSelectedEvent (not in generated schema)
interface IndependentStepsSelectedEvent {
  timestamp?: string;
  trace_id?: string;
  span_id?: string;
  event_id?: string;
  parent_id?: string;
  session_id?: string;
  component?: string;
  steps_count?: number;
  execution_mode?: string;
  plan_id?: string;
  selected_steps?: Array<string | { id?: string; description?: string }>;
}

interface IndependentStepsSelectedEventDisplayProps {
  event: IndependentStepsSelectedEvent;
}

export const IndependentStepsSelectedEventDisplay: React.FC<
  IndependentStepsSelectedEventDisplayProps
> = ({ event }) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return "";
    return new Date(timestamp).toLocaleTimeString();
  };

  return (
    <div className="p-2 bg-indigo-50 dark:bg-indigo-900/20 border border-indigo-200 dark:border-indigo-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-indigo-700 dark:text-indigo-300">
              🔀 Independent Steps Selected{" "}
              <span className="text-xs font-normal text-indigo-600 dark:text-indigo-400">
                | Steps: {event.steps_count || 0}
                {event.execution_mode && ` | Mode: ${event.execution_mode === 'parallel_execution' ? 'Parallel' : 'Sequential'}`}
                {event.plan_id && ` | Plan: ${event.plan_id.slice(0, 8)}...`}
              </span>
            </div>
          </div>
        </div>

        {/* Right side: Time */}
        {event.timestamp && (
          <div className="text-xs text-indigo-600 dark:text-indigo-400 flex-shrink-0">
            {formatTimestamp(event.timestamp)}
          </div>
        )}
      </div>

      {/* Selected steps details */}
      {event.selected_steps && event.selected_steps.length > 0 && (
        <div className="mt-2">
          <div className="text-xs text-indigo-600 dark:text-indigo-400 mb-1">
            Selected Steps:
          </div>
          <div className="flex flex-wrap gap-1">
            {event.selected_steps.map((step: string | { id?: string; description?: string }, index: number) => {
              // Handle both string and object step formats
              const stepId = typeof step === 'string' ? step : step.id || `Step ${index + 1}`
              const stepDescription = typeof step === 'object' && step.description ? step.description : null
              
              return (
                <div
                  key={index}
                  className="text-xs bg-indigo-100 dark:bg-indigo-800 text-indigo-700 dark:text-indigo-300 px-2 py-1 rounded"
                  title={stepDescription || undefined}
                >
                  {stepId}
                  {stepDescription && (
                    <div className="text-xs text-indigo-600 dark:text-indigo-400 mt-1">
                      {stepDescription}
                    </div>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      )}
    </div>
  );
};
