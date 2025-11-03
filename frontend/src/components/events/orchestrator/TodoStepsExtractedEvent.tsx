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
    <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg">
      {/* Header */}
      <div className="p-3 border-b border-green-200 dark:border-green-800">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <div className="w-7 h-7 bg-green-100 dark:bg-green-800/50 rounded-full flex items-center justify-center">
              <span className="text-green-600 dark:text-green-400 text-sm">📋</span>
            </div>
            <div className="text-sm font-medium text-green-700 dark:text-green-300">
              Plan Breakdown Complete
            </div>
          </div>
          {event.timestamp && (
            <div className="text-xs text-green-600 dark:text-green-400">
              {formatTimestamp(event.timestamp)}
            </div>
          )}
        </div>
      </div>

      {/* Steps List */}
      {event.extracted_steps && event.extracted_steps.length > 0 && (
        <div className="p-3">
          <div className="space-y-2">
            {event.extracted_steps.map((step, index) => (
              <div
                key={index}
                className="bg-white dark:bg-gray-800 border border-green-200 dark:border-green-700 rounded-md p-3"
              >
                <div className="flex items-start gap-3">
                  {/* Step Number */}
                  <div className="w-5 h-5 bg-green-100 dark:bg-green-800/50 rounded-full flex items-center justify-center flex-shrink-0 mt-0.5">
                    <span className="text-xs font-medium text-green-700 dark:text-green-300">
                      {index + 1}
                    </span>
                  </div>
                  
                  {/* Step Content */}
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2 mb-1 flex-wrap">
                      <div className="text-sm font-medium text-gray-900 dark:text-gray-100">
                        {step.title || `Step ${index + 1}`}
                      </div>
                      {step.has_loop && (
                        <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 rounded-md font-medium">
                          <span>🔄</span>
                          <span>Loop</span>
                        </span>
                      )}
                    </div>
                    {step.description && (
                      <div className="text-xs text-gray-600 dark:text-gray-400 leading-relaxed mb-2">
                        {step.description}
                      </div>
                    )}
                    
                    {/* Additional Step Information */}
                    <div className="space-y-1">
                      {step.success_criteria && (
                        <div className="text-xs">
                          <span className="font-medium text-green-700 dark:text-green-400">Success Criteria:</span>
                          <span className="text-gray-600 dark:text-gray-400 ml-1">{step.success_criteria}</span>
                        </div>
                      )}
                      
                      {step.requires_validation !== undefined && (
                        <div className="text-xs">
                          <span className="font-medium text-indigo-700 dark:text-indigo-400">Requires Validation:</span>
                          <span className={`ml-1 ${step.requires_validation ? 'text-indigo-600 dark:text-indigo-400' : 'text-gray-500 dark:text-gray-500'}`}>
                            {step.requires_validation ? 'Yes' : 'No'}
                          </span>
                        </div>
                      )}
                      
                      {step.requires_validation && step.reason_for_validation && (
                        <div className="text-xs">
                          <span className="font-medium text-indigo-700 dark:text-indigo-400">Reason for Validation:</span>
                          <span className="text-gray-600 dark:text-gray-400 ml-1">{step.reason_for_validation}</span>
                        </div>
                      )}
                      
                      {step.has_loop && step.loop_condition && (
                        <div className="text-xs">
                          <span className="font-medium text-cyan-700 dark:text-cyan-400">Loop Condition:</span>
                          <span className="text-gray-600 dark:text-gray-400 ml-1">{step.loop_condition}</span>
                        </div>
                      )}
                      
                      {step.has_loop && step.max_iterations && (
                        <div className="text-xs">
                          <span className="font-medium text-cyan-700 dark:text-cyan-400">Max Iterations:</span>
                          <span className="text-gray-600 dark:text-gray-400 ml-1">{step.max_iterations}</span>
                        </div>
                      )}
                      
                      {step.has_loop && step.loop_description && (
                        <div className="text-xs">
                          <span className="font-medium text-cyan-700 dark:text-cyan-400">Loop Description:</span>
                          <span className="text-gray-600 dark:text-gray-400 ml-1 italic">{step.loop_description}</span>
                        </div>
                      )}
                      
                      {step.context_dependencies && step.context_dependencies.length > 0 && (
                        <div className="text-xs">
                          <span className="font-medium text-purple-700 dark:text-purple-400">Context Dependencies:</span>
                          <div className="text-gray-600 dark:text-gray-400 ml-1">
                            {step.context_dependencies.map((dep, depIndex) => (
                              <div key={depIndex} className="text-xs text-gray-500 dark:text-gray-500 font-mono">
                                • {dep}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                      
                      {step.context_output && (
                        <div className="text-xs">
                          <span className="font-medium text-orange-700 dark:text-orange-400">Context Output:</span>
                          <span className="text-gray-600 dark:text-gray-400 ml-1 font-mono">{step.context_output}</span>
                        </div>
                      )}
                      
                      {/* Success Patterns */}
                      {step.success_patterns && step.success_patterns.length > 0 && (
                        <div className="text-xs mt-2">
                          <span className="font-medium text-green-700 dark:text-green-400">✅ Success Patterns:</span>
                          <div className="text-gray-600 dark:text-gray-400 ml-1 mt-1">
                            {step.success_patterns.map((pattern, patternIndex) => (
                              <div key={patternIndex} className="text-xs text-green-600 dark:text-green-400 mb-1">
                                • {pattern}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                      
                      {/* Failure Patterns */}
                      {step.failure_patterns && step.failure_patterns.length > 0 && (
                        <div className="text-xs mt-2">
                          <span className="font-medium text-red-700 dark:text-red-400">❌ Failure Patterns:</span>
                          <div className="text-gray-600 dark:text-gray-400 ml-1 mt-1">
                            {step.failure_patterns.map((pattern, patternIndex) => (
                              <div key={patternIndex} className="text-xs text-red-600 dark:text-red-400 mb-1">
                                • {pattern}
                              </div>
                            ))}
                          </div>
                        </div>
                      )}
                      
                      {/* Show when step is independent (no context dependencies or output) */}
                      {(!step.context_dependencies || step.context_dependencies.length === 0) && !step.context_output && 
                       (!step.success_patterns || step.success_patterns.length === 0) && 
                       (!step.failure_patterns || step.failure_patterns.length === 0) && (
                        <div className="text-xs text-gray-500 dark:text-gray-500 italic">
                          Independent step - no context dependencies or outputs
                        </div>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};
