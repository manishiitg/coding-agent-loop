import React, { useState, useEffect, useCallback } from "react";
import { StepEditPanel } from "./StepEditPanel";
import { agentApi } from "../../../services/api";
import { usePresetApplication } from "../../../stores/useGlobalPresetStore";
import type { TodoStepsExtractedEvent, TodoStep, AgentConfigs } from "../../../generated/events-bridge";
import type { StepConfigFile } from "../../../utils/stepConfigMatching";

interface TodoStepsExtractedEventDisplayProps {
  event: TodoStepsExtractedEvent;
}

export const TodoStepsExtractedEventDisplay: React.FC<
  TodoStepsExtractedEventDisplayProps
> = ({ event }) => {
  // Get preset's selected servers to pass to step configs (for filtering available servers)
  const { currentPresetServers } = usePresetApplication();
  
  // Use local state to track steps with saved configs
  const [steps, setSteps] = useState<TodoStep[]>(() => event.extracted_steps || []);
  const [isSaving, setIsSaving] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  // Track which step's configuration panel is expanded (only one at a time)
  const [expandedStepIndex, setExpandedStepIndex] = useState<number | null>(null);
  // Track config source (run folder vs default)
  const [configSource, setConfigSource] = useState<'run-folder' | 'default' | 'unknown'>('unknown');

  // Get step config file path - workspace_path is critical, no fallback
  const getStepConfigFilePath = useCallback((useRunFolder = false): string => {
    if (!event.workspace_path) {
      throw new Error("workspace_path is required but not provided in TodoStepsExtractedEvent");
    }
    
    if (useRunFolder && event.run_folder) {
      // Run folder path: workspace_path is like "Workflow/Some Task"
      // Run folder is like "runs/2025-01-27-iteration-1"
      // Full path: "Workflow/Some Task/runs/2025-01-27-iteration-1/planning/step_config.json"
      const workspaceRoot = event.workspace_path.replace(/\/todo_creation_human$/, '');
      return `${workspaceRoot}/runs/${event.run_folder}/planning/step_config.json`;
    }
    
    // Default path: The workspace_path already includes /todo_creation_human, so we just need planning/step_config.json
    // workspace_path is like "Workflow/Some Task/todo_creation_human"
    return `${event.workspace_path}/planning/step_config.json`;
  }, [event.workspace_path, event.run_folder]);

  // Get run folder config file path
  const getRunFolderConfigFilePath = useCallback((): string => {
    return getStepConfigFilePath(true);
  }, [getStepConfigFilePath]);

  // Check config source and load configs on mount
  useEffect(() => {
    const checkConfigSource = async () => {
      if (!event.workspace_path) {
        setIsLoading(false);
        setConfigSource('unknown');
        return;
      }

      // Check run folder config first (if run_folder is present)
      if (event.run_folder) {
        try {
          const runFolderPath = getRunFolderConfigFilePath();
          const runFolderResponse = await agentApi.getPlannerFileContent(runFolderPath);
          if (runFolderResponse.success && runFolderResponse.data) {
            setConfigSource('run-folder');
            setIsLoading(false);
            return;
          }
        } catch {
          // Run folder config doesn't exist - check default
        }
      }

      // Check default config
      try {
        const defaultPath = getStepConfigFilePath(false);
        const defaultResponse = await agentApi.getPlannerFileContent(defaultPath);
        if (defaultResponse.success && defaultResponse.data) {
          setConfigSource('default');
        } else {
          setConfigSource('unknown');
        }
      } catch {
        setConfigSource('unknown');
      } finally {
        setIsLoading(false);
      }
    };

    checkConfigSource();
  }, [event.workspace_path, event.run_folder, getRunFolderConfigFilePath, getStepConfigFilePath]);

  // Handle migration from plan.json to step_config.json on mount (one-time)
  // Backend already merges configs before emitting the event, so we just use event.extracted_steps
  useEffect(() => {
    const handleMigration = async () => {
      if (!event.workspace_path) {
        return;
      }

      const stepConfigFilePath = getStepConfigFilePath(false);
      const planFilePath = `${event.workspace_path}/planning/plan.json`;

      try {
        // Check if step_config.json exists
        let stepConfigExists = false;
        try {
          const stepConfigResponse = await agentApi.getPlannerFileContent(stepConfigFilePath);
          if (stepConfigResponse.success && stepConfigResponse.data) {
            stepConfigExists = true;
          }
        } catch {
          // step_config.json doesn't exist yet
        }

        // Migration: If step_config.json doesn't exist, check plan.json for agent_configs
        if (!stepConfigExists) {
          try {
            const planResponse = await agentApi.getPlannerFileContent(planFilePath);
            if (planResponse.success && planResponse.data) {
              const plan = JSON.parse(planResponse.data.content);
              
              // Check if plan.json has agent_configs that need migration
              if (plan.steps && Array.isArray(plan.steps)) {
                // Type for plan step during migration (may have agent_configs from old format)
                type PlanStepWithConfigs = {
                  title?: string;
                  agent_configs?: AgentConfigs;
                  [key: string]: unknown;
                };
                
                const stepsWithConfigs = (plan.steps as PlanStepWithConfigs[]).filter(
                  (step) => step.agent_configs && Object.keys(step.agent_configs).length > 0
                );
                
                if (stepsWithConfigs.length > 0) {
                  console.log(`Migrating ${stepsWithConfigs.length} step configs from plan.json to step_config.json`);
                  
                  // Migrate configs to step_config.json using step index
                  const stepConfigFile: StepConfigFile = {
                    steps: (plan.steps as PlanStepWithConfigs[]).map((step, index) => ({
                      index: index,
                      title: step.title || '',
                      agent_configs: step.agent_configs,
                    })).filter((config) => config.agent_configs && Object.keys(config.agent_configs).length > 0),
                  };
                  
                  // Write step_config.json
                  const stepConfigContent = JSON.stringify(stepConfigFile, null, 2);
                  await agentApi.updatePlannerFile(
                    stepConfigFilePath,
                    stepConfigContent,
                    "Migrated agent_configs from plan.json to step_config.json"
                  );
                  
                  // Remove agent_configs from plan.json
                  const cleanedPlan = {
                    ...plan,
                    steps: (plan.steps as PlanStepWithConfigs[]).map((step) => {
                      // eslint-disable-next-line @typescript-eslint/no-unused-vars
                      const { agent_configs, ...stepWithoutConfigs } = step;
                      return stepWithoutConfigs;
                    }),
                  };
                  
                  const cleanedPlanContent = JSON.stringify(cleanedPlan, null, 2);
                  await agentApi.updatePlannerFile(
                    planFilePath,
                    cleanedPlanContent,
                    "Removed agent_configs from plan.json (migrated to step_config.json)"
                  );
                  
                  console.log("Migration completed successfully");
                }
              }
            }
          } catch (error) {
            console.error("Migration failed (continuing with defaults):", error);
            // Continue even if migration fails
          }
        }
      } catch (error) {
        console.log("Error during migration check:", error);
        // Continue even if migration check fails
      }
    };

    handleMigration();
  }, [event.workspace_path, getStepConfigFilePath]);

  // Use steps from event (backend already merged configs via convertPlanStepsToTodoSteps)
  // Only update local state if event steps change
  useEffect(() => {
    if (event.extracted_steps) {
      setSteps(event.extracted_steps);
    }
  }, [event.extracted_steps]);

  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return "";
    return new Date(timestamp).toLocaleTimeString();
  };

  // Handle save to run folder
  const handleSaveToRunFolder = async () => {
    if (!event.run_folder) {
      alert("Run folder information is not available");
      return;
    }

    setIsSaving(true);
    try {
      const runFolderPath = getRunFolderConfigFilePath();
      
      // Read current step configs from state, or copy from default if needed
      let stepConfigFile: StepConfigFile = { steps: [] };
      
      // First, try to read from run folder (if it exists)
      try {
        const runFolderResponse = await agentApi.getPlannerFileContent(runFolderPath);
        if (runFolderResponse.success && runFolderResponse.data) {
          stepConfigFile = JSON.parse(runFolderResponse.data.content);
        }
      } catch {
        // Run folder config doesn't exist - try to copy from default
        try {
          const defaultPath = getStepConfigFilePath(false);
          const defaultResponse = await agentApi.getPlannerFileContent(defaultPath);
          if (defaultResponse.success && defaultResponse.data) {
            stepConfigFile = JSON.parse(defaultResponse.data.content);
            console.log("Copied default config to run folder as starting point");
          }
        } catch {
          // No default config either - start with empty
          console.log("No existing config found - starting with empty config");
        }
      }

      // Update step configs from current state using index
      steps.forEach((step, index) => {
        const stepTitle = step.title || '';
        const existingConfigIndex = stepConfigFile.steps.findIndex(
          (s) => s.index === index
        );

        if (existingConfigIndex >= 0) {
          stepConfigFile.steps[existingConfigIndex] = {
            index: index,
            title: stepTitle,
            agent_configs: step.agent_configs,
          };
        } else {
          stepConfigFile.steps.push({
            index: index,
            title: stepTitle,
            agent_configs: step.agent_configs,
          });
        }
      });

      // Write to run folder
      const updatedContent = JSON.stringify(stepConfigFile, null, 2);
      const updateResponse = await agentApi.updatePlannerFile(
        runFolderPath,
        updatedContent,
        `Saved step configuration to run folder: ${event.run_folder}`
      );

      if (!updateResponse.success) {
        throw new Error(updateResponse.message || "Failed to save to run folder");
      }

      setConfigSource('run-folder');
      alert(`Configuration saved to run folder: ${event.run_folder}`);
    } catch (error) {
      console.error("Failed to save to run folder:", error);
      alert(`Failed to save to run folder: ${error instanceof Error ? error.message : "Unknown error"}`);
    } finally {
      setIsSaving(false);
    }
  };

  // Handle save step configuration
  // If run_folder is present, save to run folder; otherwise save to default location
  const handleSaveStep = async (updatedStep: TodoStep, stepIndex: number) => {
    setIsSaving(true);
    try {
      // If run_folder is present, save to run folder; otherwise save to default
      const useRunFolder = !!event.run_folder;
      const stepConfigFilePath = getStepConfigFilePath(useRunFolder);
      
      // Read current step_config.json (or create empty if doesn't exist)
      let stepConfigFile: StepConfigFile = { steps: [] };
      try {
        const response = await agentApi.getPlannerFileContent(stepConfigFilePath);
        if (response.success && response.data) {
          stepConfigFile = JSON.parse(response.data.content);
        }
      } catch {
        // File doesn't exist yet - use empty structure
        console.log("step_config.json doesn't exist yet, creating new file");
      }

      // Find existing step config by index match, or create new one
      const stepTitle = updatedStep.title || '';
      const existingConfigIndex = stepConfigFile.steps.findIndex(
        (step) => step.index === stepIndex
      );

      if (existingConfigIndex >= 0) {
        // Update existing step config
        stepConfigFile.steps[existingConfigIndex] = {
          index: stepIndex,
          title: stepTitle,
          agent_configs: updatedStep.agent_configs,
        };
      } else {
        // Add new step config
        stepConfigFile.steps.push({
          index: stepIndex,
          title: stepTitle,
          agent_configs: updatedStep.agent_configs,
        });
      }

      // Write back to step_config.json
      const updatedContent = JSON.stringify(stepConfigFile, null, 2);
      
      // Debug logging to verify what's being saved
      console.log('[TodoStepsExtractedEvent] Saving step_config.json:', {
        stepTitle,
        agent_configs: updatedStep.agent_configs,
        stepConfigFile,
        fileContent: updatedContent.substring(0, 500), // First 500 chars for preview
      });
      
      const updateResponse = await agentApi.updatePlannerFile(
        stepConfigFilePath,
        updatedContent,
        `Updated step "${stepTitle}" agent configuration`
      );

      if (!updateResponse.success) {
        throw new Error(updateResponse.message || "Failed to update step_config.json");
      }

      // Update local state to reflect saved changes
      setSteps((prevSteps) => {
        const updated = [...prevSteps];
        updated[stepIndex] = updatedStep;
        return updated;
      });

      // Update config source based on where we saved
      if (useRunFolder) {
        setConfigSource('run-folder');
      } else if (configSource !== 'run-folder') {
        setConfigSource('default');
      }
    } catch (error) {
      console.error("Failed to save step configuration:", error);
      alert(`Failed to save step configuration: ${error instanceof Error ? error.message : "Unknown error"}`);
    } finally {
      setIsSaving(false);
    }
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
          <div className="flex items-center gap-3">
            {/* Config Source Indicator */}
            {!isLoading && (
              <div className="text-xs px-2 py-1 rounded-md bg-gray-100 dark:bg-gray-800 text-gray-700 dark:text-gray-300">
                {configSource === 'run-folder' && (
                  <span>📁 Using run-specific config</span>
                )}
                {configSource === 'default' && (
                  <span>📄 Using default config</span>
                )}
                {configSource === 'unknown' && (
                  <span>⚪ No config found</span>
                )}
              </div>
            )}
            {event.timestamp && (
              <div className="text-xs text-green-600 dark:text-green-400">
                {formatTimestamp(event.timestamp)}
              </div>
            )}
          </div>
        </div>
        {/* Save to Run Folder Button */}
        {event.run_folder && (
          <div className="mt-2 flex items-center gap-2">
            <button
              onClick={handleSaveToRunFolder}
              disabled={isSaving}
              className="px-3 py-1.5 text-xs font-medium bg-blue-600 hover:bg-blue-700 disabled:bg-gray-400 text-white rounded-md transition-colors"
            >
              {isSaving ? "Saving..." : `Save Configuration to Run Folder (${event.run_folder})`}
            </button>
          </div>
        )}
      </div>

      {/* Steps List */}
      {steps && steps.length > 0 && (
        <div className="p-3">
          {isLoading && (
            <div className="text-sm text-gray-600 dark:text-gray-400 text-center py-2">
              Loading saved configurations...
            </div>
          )}
          <div className="space-y-2">
            {steps.map((step, index) => (
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
                      
                      {step.agent_configs?.disable_validation !== undefined && (
                        <div className="text-xs">
                          <span className="font-medium text-indigo-700 dark:text-indigo-400">Validation:</span>
                          <span className={`ml-1 ${step.agent_configs.disable_validation ? 'text-gray-500 dark:text-gray-500' : 'text-indigo-600 dark:text-indigo-400'}`}>
                            {step.agent_configs.disable_validation ? 'Disabled (Auto-approve)' : 'Enabled'}
                          </span>
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
                
                {/* Configuration Panel - Always visible */}
                <StepEditPanel
                  step={step}
                  stepIndex={index}
                  onSave={(updatedStep) => handleSaveStep(updatedStep, index)}
                  onCancel={() => {}} // No cancel needed since it's always visible
                  isSaving={isSaving}
                  presetServers={currentPresetServers}
                  isExpanded={expandedStepIndex === index}
                  onToggleExpanded={(expanded) => {
                    // If expanding, set this step as expanded (closes others)
                    // If collapsing, set to null (all closed)
                    setExpandedStepIndex(expanded ? index : null);
                  }}
                />
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
};
