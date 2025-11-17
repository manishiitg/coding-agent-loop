import React, { useState, useEffect, useCallback } from "react";
import { StepEditPanel } from "./StepEditPanel";
import { agentApi } from "../../../services/api";
import { usePresetApplication } from "../../../stores/useGlobalPresetStore";
import type { TodoStepsExtractedEvent } from "../../../generated/events-bridge";
import type { TodoStepWithConfigs, AgentConfigs } from "../../../utils/stepConfigMatching";
import type { StepConfigFile } from "../../../utils/stepConfigMatching";
import ConfirmationDialog from "../../ui/ConfirmationDialog";
import { Edit2, Trash2, Save, X } from "lucide-react";

// Helper to get workspace_path from event
// workspace_path may be on the event directly or in metadata
function getWorkspacePath(event: TodoStepsExtractedEvent): string | undefined {
  // Check if workspace_path is directly on the event
  const eventWithPath = event as TodoStepsExtractedEvent & { workspace_path?: string };
  if (eventWithPath.workspace_path && typeof eventWithPath.workspace_path === 'string') {
    return eventWithPath.workspace_path;
  }
  // Check if workspace_path is in metadata
  if (event.metadata && typeof event.metadata.workspace_path === 'string') {
    return event.metadata.workspace_path as string;
  }
  return undefined;
}

interface TodoStepsExtractedEventDisplayProps {
  event: TodoStepsExtractedEvent;
}

export const TodoStepsExtractedEventDisplay: React.FC<
  TodoStepsExtractedEventDisplayProps
> = ({ event }) => {
  // Get preset's selected servers to pass to step configs (for filtering available servers)
  const { currentPresetServers } = usePresetApplication();
  
  // Use local state to track steps with saved configs
  const [steps, setSteps] = useState<TodoStepWithConfigs[]>(() => {
    // Cast to include agent_configs if present
    return (event.extracted_steps || []) as TodoStepWithConfigs[];
  });
  const [isSaving, setIsSaving] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  // Track which step's configuration panel is expanded (only one at a time)
  const [expandedStepIndex, setExpandedStepIndex] = useState<number | null>(null);
  // Track config source (default)
  const [configSource, setConfigSource] = useState<'default' | 'unknown'>('unknown');
  // Run mode state
  const [runMode, setRunMode] = useState<string>('use_same_run');
  const [isLoadingRunMode, setIsLoadingRunMode] = useState(true);
  const [isSavingRunMode, setIsSavingRunMode] = useState(false);
  
  // Edit state management
  const [editingStepIndex, setEditingStepIndex] = useState<number | null>(null);
  const [editedStepDescription, setEditedStepDescription] = useState<string>("");
  const [editedSuccessCriteria, setEditedSuccessCriteria] = useState<string>("");
  const [editedLoopCondition, setEditedLoopCondition] = useState<string>("");
  const [editedMaxIterations, setEditedMaxIterations] = useState<string>("");
  const [editedLoopDescription, setEditedLoopDescription] = useState<string>("");
  const [isSavingStepEdit, setIsSavingStepEdit] = useState(false);
  
  // Delete state management
  const [stepToDelete, setStepToDelete] = useState<number | null>(null);
  const [isDeletingStep, setIsDeletingStep] = useState(false);

  // Get step config file path - workspace_path is critical, no fallback
  const getStepConfigFilePath = useCallback((): string => {
    const workspacePath = getWorkspacePath(event);
    if (!workspacePath) {
      throw new Error("workspace_path is required but not provided in TodoStepsExtractedEvent");
    }
    
    // Default path: workspace_path is like "Workflow/Some Task"
    // Full path: "Workflow/Some Task/planning/step_config.json"
    return `${workspacePath}/planning/step_config.json`;
  }, [event]);

  // Check config source and load configs on mount
  useEffect(() => {
    const checkConfigSource = async () => {
      const workspacePath = getWorkspacePath(event);
      if (!workspacePath) {
        setIsLoading(false);
        setConfigSource('unknown');
        return;
      }

      // Check default config
      try {
        const defaultPath = getStepConfigFilePath();
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
  }, [event, getStepConfigFilePath]);

  // Handle migration from plan.json to step_config.json on mount (one-time)
  // Backend already merges configs before emitting the event, so we just use event.extracted_steps
  useEffect(() => {
    const handleMigration = async () => {
      const workspacePath = getWorkspacePath(event);
      if (!workspacePath) {
        return;
      }
      
      const stepConfigFilePath = getStepConfigFilePath();
      const planFilePath = `${workspacePath}/planning/plan.json`;

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
  }, [event, getStepConfigFilePath]);

  // Load run_mode from plan.json on mount
  useEffect(() => {
    const loadRunMode = async () => {
      const workspacePath = getWorkspacePath(event);
      if (!workspacePath) {
        setIsLoadingRunMode(false);
        return;
      }

      try {
        const planFilePath = `${workspacePath}/planning/plan.json`;
        const planResponse = await agentApi.getPlannerFileContent(planFilePath);
        if (planResponse.success && planResponse.data) {
          const plan = JSON.parse(planResponse.data.content);
          if (plan.run_mode && typeof plan.run_mode === 'string') {
            setRunMode(plan.run_mode);
          } else {
            // Default to 'use_same_run' if not present
            setRunMode('use_same_run');
          }
        } else {
          setRunMode('use_same_run');
        }
      } catch (error) {
        console.error("Failed to load run_mode from plan.json:", error);
        setRunMode('use_same_run');
      } finally {
        setIsLoadingRunMode(false);
      }
    };

    loadRunMode();
  }, [event]);

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

  // Get plan.json file path
  const getPlanFilePath = useCallback((): string => {
    const workspacePath = getWorkspacePath(event);
    if (!workspacePath) {
      throw new Error("workspace_path is required but not provided in TodoStepsExtractedEvent");
    }
    return `${workspacePath}/planning/plan.json`;
  }, [event]);

  // Handle edit step (description and loop_description)
  const handleEditStep = async (stepIndex: number) => {
    setIsSavingStepEdit(true);
    try {
      const workspacePath = getWorkspacePath(event);
      if (!workspacePath) {
        throw new Error("Workspace path is not available");
      }

      const planFilePath = getPlanFilePath();
      
      // Read current plan.json
      const planResponse = await agentApi.getPlannerFileContent(planFilePath);
      if (!planResponse.success || !planResponse.data) {
        throw new Error("Failed to read plan.json");
      }

      const plan = JSON.parse(planResponse.data.content);
      
      // Validate plan structure
      if (!plan.steps || !Array.isArray(plan.steps) || stepIndex >= plan.steps.length) {
        throw new Error("Invalid plan structure or step index");
      }

      // Update step description
      plan.steps[stepIndex].description = editedStepDescription;
      
      // Update success criteria
      plan.steps[stepIndex].success_criteria = editedSuccessCriteria;
      
      // Update loop fields if step has loop
      if (plan.steps[stepIndex].has_loop) {
        plan.steps[stepIndex].loop_condition = editedLoopCondition;
        // Parse max_iterations as integer (default to 10 if invalid)
        const maxIterations = parseInt(editedMaxIterations, 10);
        if (!isNaN(maxIterations) && maxIterations > 0) {
          plan.steps[stepIndex].max_iterations = maxIterations;
        } else if (editedMaxIterations.trim() === "") {
          // If empty, use default of 10
          plan.steps[stepIndex].max_iterations = 10;
        }
        plan.steps[stepIndex].loop_description = editedLoopDescription;
      }

      // Save updated plan.json
      const updatedContent = JSON.stringify(plan, null, 2);
      const updateParts = ['description', 'success criteria'];
      if (plan.steps[stepIndex].has_loop) {
        updateParts.push('loop condition', 'max iterations', 'loop description');
      }
      const saveResponse = await agentApi.updatePlannerFile(
        planFilePath,
        updatedContent,
        `Updated step ${stepIndex + 1} ${updateParts.join(', ')}`
      );

      if (!saveResponse.success) {
        throw new Error(saveResponse.message || "Failed to save plan.json");
      }

      // Update local state to reflect changes
      setSteps((prevSteps) => {
        const updated = [...prevSteps];
        updated[stepIndex] = {
          ...updated[stepIndex],
          description: editedStepDescription,
          success_criteria: editedSuccessCriteria,
          loop_condition: plan.steps[stepIndex].has_loop ? editedLoopCondition : updated[stepIndex].loop_condition,
          max_iterations: plan.steps[stepIndex].has_loop ? plan.steps[stepIndex].max_iterations : updated[stepIndex].max_iterations,
          loop_description: plan.steps[stepIndex].has_loop ? editedLoopDescription : updated[stepIndex].loop_description,
        };
        return updated;
      });

      // Exit edit mode
      setEditingStepIndex(null);
      setEditedStepDescription("");
      setEditedSuccessCriteria("");
      setEditedLoopCondition("");
      setEditedMaxIterations("");
      setEditedLoopDescription("");
    } catch (error) {
      console.error("Failed to edit step:", error);
      alert(`Failed to edit step: ${error instanceof Error ? error.message : "Unknown error"}`);
    } finally {
      setIsSavingStepEdit(false);
    }
  };

  // Handle delete step
  const handleDeleteStep = async (stepIndex: number) => {
    setIsDeletingStep(true);
    try {
      const workspacePath = getWorkspacePath(event);
      if (!workspacePath) {
        throw new Error("Workspace path is not available");
      }

      const planFilePath = getPlanFilePath();
      
      // Read current plan.json
      const planResponse = await agentApi.getPlannerFileContent(planFilePath);
      if (!planResponse.success || !planResponse.data) {
        throw new Error("Failed to read plan.json");
      }

      const plan = JSON.parse(planResponse.data.content);
      
      // Validate plan structure
      if (!plan.steps || !Array.isArray(plan.steps) || stepIndex >= plan.steps.length) {
        throw new Error("Invalid plan structure or step index");
      }

      // Get deleted step's context_output for cleanup
      const deletedStep = plan.steps[stepIndex];
      const deletedContextOutput = deletedStep.context_output;

      // Remove step from array
      plan.steps.splice(stepIndex, 1);

      // Clean up context_dependencies in remaining steps
      if (deletedContextOutput) {
        plan.steps.forEach((step: { context_dependencies?: string[] }) => {
          if (step.context_dependencies && Array.isArray(step.context_dependencies)) {
            step.context_dependencies = step.context_dependencies.filter(
              (dep: string) => dep !== deletedContextOutput
            );
          }
        });
      }

      // Save updated plan.json
      const updatedContent = JSON.stringify(plan, null, 2);
      const saveResponse = await agentApi.updatePlannerFile(
        planFilePath,
        updatedContent,
        `Deleted step ${stepIndex + 1}`
      );

      if (!saveResponse.success) {
        throw new Error(saveResponse.message || "Failed to save plan.json");
      }

      // Update local state to remove deleted step
      setSteps((prevSteps) => {
        const updated = [...prevSteps];
        updated.splice(stepIndex, 1);
        return updated;
      });

      // Close delete confirmation dialog
      setStepToDelete(null);
    } catch (error) {
      console.error("Failed to delete step:", error);
      alert(`Failed to delete step: ${error instanceof Error ? error.message : "Unknown error"}`);
    } finally {
      setIsDeletingStep(false);
    }
  };

  // Start editing a step
  const startEditingStep = (stepIndex: number) => {
    const step = steps[stepIndex];
    setEditingStepIndex(stepIndex);
    setEditedStepDescription(step.description || "");
    setEditedSuccessCriteria(step.success_criteria || "");
    setEditedLoopCondition(step.loop_condition || "");
    setEditedMaxIterations(step.max_iterations ? step.max_iterations.toString() : "10");
    setEditedLoopDescription(step.loop_description || "");
  };

  // Cancel editing
  const cancelEditingStep = () => {
    setEditingStepIndex(null);
    setEditedStepDescription("");
    setEditedSuccessCriteria("");
    setEditedLoopCondition("");
    setEditedMaxIterations("");
    setEditedLoopDescription("");
  };

  // Handle save step configuration
  const handleSaveStep = async (updatedStep: TodoStepWithConfigs, stepIndex: number) => {
    setIsSaving(true);
    try {
      const stepConfigFilePath = getStepConfigFilePath();
      
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

      // Update config source
      setConfigSource('default');
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
        {/* Run Mode Selector */}
        <div className="mt-2 flex items-center gap-2 flex-wrap">
          <label className="text-xs font-medium text-gray-700 dark:text-gray-300">
            Run Mode:
          </label>
          <select
            value={runMode}
            onChange={(e) => setRunMode(e.target.value)}
            disabled={isLoadingRunMode || isSavingRunMode}
            className="px-2 py-1 text-xs border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-800 text-gray-900 dark:text-gray-100 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            <option value="use_same_run">Use Same Run</option>
            <option value="create_new_runs_always">Create New Runs Always</option>
            <option value="create_new_run_once_daily">Create New Run Once Daily</option>
          </select>
          <button
            onClick={async () => {
              const workspacePath = getWorkspacePath(event);
              if (!workspacePath) {
                alert("Workspace path is not available");
                return;
              }

              setIsSavingRunMode(true);
              try {
                const planFilePath = `${workspacePath}/planning/plan.json`;
                
                // Read current plan.json
                const planResponse = await agentApi.getPlannerFileContent(planFilePath);
                if (!planResponse.success || !planResponse.data) {
                  throw new Error("Failed to read plan.json");
                }

                const plan = JSON.parse(planResponse.data.content);
                
                // Update run_mode
                const updatedPlan = {
                  ...plan,
                  run_mode: runMode,
                };

                const updatedContent = JSON.stringify(updatedPlan, null, 2);
                const saveResponse = await agentApi.updatePlannerFile(
                  planFilePath,
                  updatedContent,
                  `Updated run_mode to ${runMode}`
                );

                if (!saveResponse.success) {
                  throw new Error(saveResponse.message || "Failed to save run_mode");
                }

                console.log(`Run mode updated to: ${runMode}`);
              } catch (error) {
                console.error("Failed to save run_mode:", error);
                alert(`Failed to save run mode: ${error instanceof Error ? error.message : "Unknown error"}`);
              } finally {
                setIsSavingRunMode(false);
              }
            }}
            disabled={isLoadingRunMode || isSavingRunMode}
            className="px-2 py-1 text-xs font-medium bg-green-600 hover:bg-green-700 disabled:bg-gray-400 text-white rounded-md transition-colors"
          >
            {isSavingRunMode ? "Saving..." : "Save"}
          </button>
        </div>
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
            {steps.map((step, index) => {
              // Create unique key using event identifier + step index to prevent React from reusing component instances
              const eventId = event.event_id || event.trace_id || event.timestamp || 'event';
              const stepKey = `${eventId}-step-${index}`;
              return (
              <div
                key={stepKey}
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
                    <div className="flex items-center justify-between gap-2 mb-1 flex-wrap">
                      <div className="flex items-center gap-2 flex-wrap">
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
                      {/* Edit and Delete buttons */}
                      {editingStepIndex !== index && (
                        <div className="flex items-center gap-1">
                          <button
                            onClick={() => startEditingStep(index)}
                            disabled={isSavingStepEdit || isDeletingStep}
                            className="p-1.5 text-gray-500 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                            title="Edit step description"
                          >
                            <Edit2 className="w-4 h-4" />
                          </button>
                          <button
                            onClick={() => setStepToDelete(index)}
                            disabled={isSavingStepEdit || isDeletingStep}
                            className="p-1.5 text-gray-500 hover:text-red-600 dark:text-gray-400 dark:hover:text-red-400 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                            title="Delete step"
                          >
                            <Trash2 className="w-4 h-4" />
                          </button>
                        </div>
                      )}
                    </div>
                    
                    {/* Edit Mode */}
                    {editingStepIndex === index ? (
                      <div className="mb-3 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-700 rounded-md">
                        <div className="space-y-3">
                          {/* Step Description Editor */}
                          <div>
                            <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                              Step Description
                            </label>
                            <textarea
                              value={editedStepDescription}
                              onChange={(e) => setEditedStepDescription(e.target.value)}
                              rows={4}
                              className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-y"
                              placeholder="Enter step description..."
                            />
                          </div>
                          
                          {/* Success Criteria Editor */}
                          <div>
                            <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                              Success Criteria
                            </label>
                            <textarea
                              value={editedSuccessCriteria}
                              onChange={(e) => setEditedSuccessCriteria(e.target.value)}
                              rows={3}
                              className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-y"
                              placeholder="Enter success criteria..."
                            />
                          </div>
                          
                          {/* Loop Condition Editor (only for loop steps) */}
                          {step.has_loop && (
                            <div>
                              <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                                Loop Condition
                              </label>
                              <textarea
                                value={editedLoopCondition}
                                onChange={(e) => setEditedLoopCondition(e.target.value)}
                                rows={2}
                                className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-y"
                                placeholder="Enter loop condition (when to exit the loop)..."
                              />
                            </div>
                          )}
                          
                          {/* Max Iterations Editor (only for loop steps) */}
                          {step.has_loop && (
                            <div>
                              <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                                Max Iterations
                              </label>
                              <input
                                type="number"
                                value={editedMaxIterations}
                                onChange={(e) => setEditedMaxIterations(e.target.value)}
                                min="1"
                                max="100"
                                className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                                placeholder="Enter max iterations (default: 10)..."
                              />
                              <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                                Maximum number of loop iterations allowed (default: 10)
                              </p>
                            </div>
                          )}
                          
                          {/* Loop Description Editor (only for loop steps) */}
                          {step.has_loop && (
                            <div>
                              <label className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1">
                                Loop Description
                              </label>
                              <textarea
                                value={editedLoopDescription}
                                onChange={(e) => setEditedLoopDescription(e.target.value)}
                                rows={3}
                                className="w-full px-3 py-2 text-xs border border-gray-300 dark:border-gray-600 rounded-md bg-white dark:bg-gray-700 text-gray-900 dark:text-gray-100 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500 resize-y"
                                placeholder="Enter loop description..."
                              />
                            </div>
                          )}
                          
                          {/* Edit Action Buttons */}
                          <div className="flex items-center justify-end gap-2">
                            <button
                              onClick={cancelEditingStep}
                              disabled={isSavingStepEdit}
                              className="px-3 py-1.5 text-xs font-medium text-gray-700 dark:text-gray-300 bg-gray-100 dark:bg-gray-700 hover:bg-gray-200 dark:hover:bg-gray-600 rounded-md transition-colors disabled:opacity-50 disabled:cursor-not-allowed flex items-center gap-1"
                            >
                              <X className="w-3.5 h-3.5" />
                              Cancel
                            </button>
                            <button
                              onClick={() => handleEditStep(index)}
                              disabled={
                                isSavingStepEdit || 
                                !editedStepDescription.trim() || 
                                !editedSuccessCriteria.trim() ||
                                (step.has_loop && !editedLoopCondition.trim())
                              }
                              className="px-3 py-1.5 text-xs font-medium bg-blue-600 hover:bg-blue-700 disabled:bg-gray-400 text-white rounded-md transition-colors disabled:cursor-not-allowed flex items-center gap-1"
                            >
                              {isSavingStepEdit ? (
                                <>
                                  <div className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin"></div>
                                  Saving...
                                </>
                              ) : (
                                <>
                                  <Save className="w-3.5 h-3.5" />
                                  Save
                                </>
                              )}
                            </button>
                          </div>
                        </div>
                      </div>
                    ) : (
                      /* View Mode */
                      step.description && (
                        <div className="text-xs text-gray-600 dark:text-gray-400 leading-relaxed mb-2">
                          {step.description}
                        </div>
                      )
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
              );
            })}
          </div>
        </div>
      )}

      {/* Delete Confirmation Dialog */}
      <ConfirmationDialog
        isOpen={stepToDelete !== null}
        onClose={() => setStepToDelete(null)}
        onConfirm={() => {
          if (stepToDelete !== null) {
            handleDeleteStep(stepToDelete);
          }
        }}
        title="Delete Step"
        message={
          stepToDelete !== null
            ? `Are you sure you want to delete "${steps[stepToDelete]?.title || `Step ${stepToDelete + 1}`}"? This action cannot be undone. Any context dependencies referencing this step's output will be automatically removed.`
            : ""
        }
        confirmText="Delete"
        cancelText="Cancel"
        type="danger"
        isLoading={isDeletingStep}
      />
    </div>
  );
};
