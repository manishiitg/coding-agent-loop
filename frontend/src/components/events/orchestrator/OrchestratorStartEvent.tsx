import React from "react";
import type { OrchestratorStartEvent } from "../../../generated/events";

interface OrchestratorStartEventDisplayProps {
  event: OrchestratorStartEvent;
}

export const OrchestratorStartEventDisplay: React.FC<
  OrchestratorStartEventDisplayProps
> = ({ event }) => {
  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return "";
    return new Date(timestamp).toLocaleTimeString();
  };

  const getLabel = () => {
    const t = event.orchestrator_type
    if (t === 'planner') return 'Planner Orchestrator'
    if (t === 'workflow') return 'Workflow Orchestrator'
    return 'Orchestrator'
  }

  return (
    <div className="p-2 bg-yellow-50 dark:bg-yellow-900/20 border border-yellow-200 dark:border-yellow-800 rounded">
      {/* Header with single-line layout */}
      <div className="flex items-center justify-between gap-3">
        {/* Left side: Icon and main content */}
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className="min-w-0 flex-1">
            <div className="text-sm font-medium text-yellow-700 dark:text-yellow-300">
              {getLabel()} Started{" "}
              <span className="text-xs font-normal text-yellow-600 dark:text-yellow-400">
                | Agents: {event.agents_count} | Servers: {event.servers_count}
                {event.execution_mode && ` | Mode: ${event.execution_mode === 'parallel_execution' ? 'Parallel' : 'Sequential'}`}
                {event.configuration &&
                  ` | Config: ${event.configuration.length > 20 ? `${event.configuration.substring(0, 20)}...` : event.configuration}`}
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
    </div>
  );
};
