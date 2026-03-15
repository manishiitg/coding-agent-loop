import React, { useState, useEffect, useCallback } from "react";
import { StepEditPanel } from "./StepEditPanel";
import { agentApi } from "../../../services/api";
import { usePresetApplication } from "../../../stores/useGlobalPresetStore";
import { useModeStore } from "../../../stores/useModeStore";
import type { TodoStepsExtractedEvent } from "../../../generated/events-bridge";
import type { TodoStepWithConfigs, AgentConfigs, StepConfig } from "../../../utils/stepConfigMatching";
import type { PresetLLMConfig } from "../../../services/api-types";
import ConfirmationDialog from "../../ui/ConfirmationDialog";
import { Edit2, Trash2, Save, X, ChevronDown, ChevronRight, GitBranch, RefreshCw, CheckCircle2 } from "lucide-react";

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
  // Check if we're in workflow mode - if so, show minimized view
  const { selectedModeCategory } = useModeStore();
  const isWorkflowMode = selectedModeCategory === 'workflow';
  
  // Get preset's selected servers and LLM config to pass to step configs
  const { currentPresetServers, getActivePreset } = usePresetApplication();
  const activePreset = getActivePreset('workflow');
  const presetLLMConfig = activePreset?.llmConfig;
  // Get code execution mode from preset (both CustomPreset and PredefinedPreset have this field)
  const presetUseCodeExecutionMode = activePreset && 'useCodeExecutionMode' in activePreset 
    ? (activePreset as { useCodeExecutionMode?: boolean }).useCodeExecutionMode || false
    : false;
  
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
  // Collapsed state for workflow mode (default: collapsed in workflow mode)
  const [isCollapsed, setIsCollapsed] = useState(isWorkflowMode);
  
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


  // Load and apply step_config.json configs (including branch steps) after event loads
  useEffect(() => {
    const loadAndApplyStepConfigs = async () => {
      if (!event.extracted_steps) return;

      const workspacePath = getWorkspacePath(event);
      if (!workspacePath) return;

      try {
        const stepConfigFilePath = getStepConfigFilePath();
        const response = await agentApi.getPlannerFileContent(stepConfigFilePath);
        
        if (!response.success || !response.data) {
          // No step_config.json, use event steps as-is
          setSteps(event.extracted_steps as TodoStepWithConfigs[]);
          return;
        }

        // Parse step_config.json in object format: { "steps": [...] }
        const rawContent = JSON.parse(response.data.content);
        const stepConfigs: StepConfig[] = Array.isArray(rawContent) 
          ? rawContent  // Legacy array format support
          : (rawContent?.steps || []);  // Object format with "steps" field
        
        // Create map for ID-based matching
        const idConfigMap = new Map<string, AgentConfigs>();      // ID -> config
        
        for (const stepConfig of stepConfigs) {
          if (stepConfig.agent_configs && stepConfig.id) {
            idConfigMap.set(stepConfig.id, stepConfig.agent_configs);
          }
        }
        
        // Debug: Log all IDs in the map (for debugging matching issues)
        console.log('[TodoStepsExtractedEvent] All IDs loaded from step_config.json:', {
          totalConfigs: stepConfigs.length,
          idsWithConfigs: Array.from(idConfigMap.keys()),
          idDetails: stepConfigs
            .filter(s => s.id && s.agent_configs)
            .map(s => ({
              id: s.id,
              title: s.title,
              executionLLM: s.agent_configs?.execution_llm?.model_id,
            })),
        });

        // Recursively apply configs to steps
        // parentStep: parent step for branch steps (for context)
        const applyConfigsToStep = (step: TodoStepWithConfigs, stepIndex: number, parentStep?: TodoStepWithConfigs, branchType?: 'true' | 'false'): TodoStepWithConfigs => {
          // Steps always have IDs from backend - throw error if missing
          if (!step.id) {
            throw new Error(
              `Step is missing required ID field. Step title: "${step.title || 'unknown'}", ` +
              `stepIndex: ${stepIndex}, parentStep: ${parentStep?.title || 'none'}, ` +
              `branchType: ${branchType || 'none'}`
            );
          }
          
          const stepId = step.id;
          const config = idConfigMap.get(stepId);
          
          // Debug logging for step index 1
          if (stepIndex === 1 && !parentStep) {
            console.log('[TodoStepsExtractedEvent] applyConfigsToStep - Top-level step 1:', {
              stepIndex,
              stepTitle: step.title,
              stepId,
              foundInMap: idConfigMap.has(stepId),
              foundConfig: !!config,
              executionLLM: config?.execution_llm?.model_id,
              existingConfig: step.agent_configs?.execution_llm?.model_id,
              allAvailableIds: Array.from(idConfigMap.keys()),
            });
          }

          // Create updated step with config
          const updatedStep: TodoStepWithConfigs = {
            ...step,
            agent_configs: config || step.agent_configs,
          };

          // Debug logging for step index 1 - final config
          if (stepIndex === 1 && !parentStep) {
            console.log('[TodoStepsExtractedEvent] applyConfigsToStep - Final config for step 1:', {
              stepIndex,
              finalConfig: updatedStep.agent_configs?.execution_llm,
              hasTrueSteps: !!step.if_true_steps?.length,
              hasFalseSteps: !!step.if_false_steps?.length,
            });
          }

          // Recursively apply configs to branch steps
          if (step.if_true_steps && step.if_true_steps.length > 0) {
            updatedStep.if_true_steps = step.if_true_steps.map((branchStep, idx) => {
              // Debug logging for step index 1
              if (stepIndex === 1 && !parentStep) {
                console.log('[TodoStepsExtractedEvent] applyConfigsToStep - Processing if_true branch for step 1:', {
                  stepIndex,
                  branchIndex: idx,
                  parentTitle: step.title,
                  branchStepTitle: branchStep.title,
                  branchStepHasConfig: !!branchStep.agent_configs,
                });
              }
              return applyConfigsToStep(branchStep, stepIndex, step, 'true');
            });
          }

          if (step.if_false_steps && step.if_false_steps.length > 0) {
            updatedStep.if_false_steps = step.if_false_steps.map((branchStep, idx) => {
              // Debug logging for step index 1
              if (stepIndex === 1 && !parentStep) {
                console.log('[TodoStepsExtractedEvent] applyConfigsToStep - Processing if_false branch for step 1:', {
                  stepIndex,
                  branchIndex: idx,
                  parentTitle: step.title,
                  branchStepTitle: branchStep.title,
                  branchStepHasConfig: !!branchStep.agent_configs,
                });
              }
              return applyConfigsToStep(branchStep, stepIndex, step, 'false');
            });
          }

          return updatedStep;
        };

        // Apply configs to all steps
        const stepsWithConfigs = (event.extracted_steps as TodoStepWithConfigs[]).map((step, index) => 
          applyConfigsToStep(step, index)
        );

        setSteps(stepsWithConfigs);
      } catch (error) {
        console.error('[TodoStepsExtractedEvent] Failed to load step_config.json, using event steps:', error);
        // Fallback to event steps if loading fails
        setSteps(event.extracted_steps as TodoStepWithConfigs[]);
      }
    };

    loadAndApplyStepConfigs();
  }, [event.extracted_steps, event, getStepConfigFilePath]);

  const formatTimestamp = (timestamp?: string) => {
    if (!timestamp) return "";
    return new Date(timestamp).toLocaleTimeString();
  };

  // Handle edit step (description and loop_description)
  const handleEditStep = async (stepIndex: number) => {
    setIsSavingStepEdit(true);
    try {
      const workspacePath = getWorkspacePath(event);
      if (!workspacePath) {
        throw new Error("Workspace path is not available");
      }

      // Validate step index
      if (stepIndex < 0 || stepIndex >= steps.length) {
        throw new Error("Invalid step index");
      }

      const currentStep = steps[stepIndex];
      if (!currentStep.id) {
        throw new Error("Step is missing required ID field");
      }

      // Build updates object
      const updates: Partial<{
        description: string;
        success_criteria: string;
        loop_condition: string;
        max_iterations: number;
        loop_description: string;
      }> = {
        description: editedStepDescription,
        success_criteria: editedSuccessCriteria,
      };

      // Update loop fields if step has loop
      // Note: TodoStepWithConfigs uses boolean flags (has_loop) instead of type field
      if (currentStep.has_loop) {
        updates.loop_condition = editedLoopCondition;
        // Parse max_iterations as integer (default to 10 if invalid)
        const maxIterations = parseInt(editedMaxIterations, 10);
        if (!isNaN(maxIterations) && maxIterations > 0) {
          updates.max_iterations = maxIterations;
        } else if (editedMaxIterations.trim() === "") {
          // If empty, use default of 10
          updates.max_iterations = 10;
        }
        updates.loop_description = editedLoopDescription;
      }

      // Use new backend API to update step
      const updateResponse = await agentApi.updatePlanStep(
        workspacePath,
        currentStep.id,
        updates
      );

      if (!updateResponse.success) {
        throw new Error(updateResponse.message || "Failed to update step");
      }

      // Update local state to reflect changes
      setSteps((prevSteps) => {
        const updated = [...prevSteps];
        updated[stepIndex] = {
          ...updated[stepIndex],
          description: editedStepDescription,
          success_criteria: editedSuccessCriteria,
          loop_condition: currentStep.has_loop ? editedLoopCondition : updated[stepIndex].loop_condition,
          max_iterations: currentStep.has_loop ? (updates.max_iterations ?? updated[stepIndex].max_iterations) : updated[stepIndex].max_iterations,
          loop_description: currentStep.has_loop ? editedLoopDescription : updated[stepIndex].loop_description,
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

      // Validate step index
      if (stepIndex < 0 || stepIndex >= steps.length) {
        throw new Error("Invalid step index");
      }

      const stepToDeleteObj = steps[stepIndex];
      if (!stepToDeleteObj.id) {
        throw new Error("Step is missing required ID field");
      }

      // Use new backend API to delete step
      // Backend will handle context_dependencies cleanup automatically
      const deleteResponse = await agentApi.deleteStep(
        workspacePath,
        stepToDeleteObj.id
      );

      if (!deleteResponse.success) {
        throw new Error(deleteResponse.message || "Failed to delete step");
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
  // stepPath: optional path for branch steps (e.g., "step-2-if-true-0") - only used temporarily to extract parent step info, not saved
  const handleSaveStep = async (updatedStep: TodoStepWithConfigs, stepIndex: number, stepPath?: string) => {
    setIsSaving(true);
    try {
      // Get workspace path from event
      const workspacePath = getWorkspacePath(event);
      if (!workspacePath) {
        throw new Error('Workspace path not found in event');
      }

      const stepTitle = updatedStep.title || '';
      
      console.log('[TodoStepsExtractedEvent] handleSaveStep - Input:', {
        stepTitle,
        stepIndex,
        stepPath,
        updatedStepId: updatedStep.id,
        updatedStepTitle: updatedStep.title,
        hasTitle: !!updatedStep.title,
        hasId: !!updatedStep.id,
        workspacePath,
      });
      
      // Steps always have IDs from backend - throw error if missing
      if (!updatedStep.id) {
        throw new Error(
          `Cannot save step config: step is missing required ID field. ` +
          `Step title: "${stepTitle}", stepIndex: ${stepIndex}, stepPath: ${stepPath || 'none'}`
        );
      }
      
      const stepId = updatedStep.id;
      
      // Debug logging to verify what's being saved
      const stepType = stepPath ? 'branch step' : 'top-level step';
      console.log(`[TodoStepsExtractedEvent] Saving step config via API (${stepType}):`, {
        stepTitle,
        stepIndex,
        stepId,
        stepType,
        agent_configs: updatedStep.agent_configs,
        disable_learning: updatedStep.agent_configs?.disable_learning,
        use_code_execution_mode: updatedStep.agent_configs?.use_code_execution_mode,
      });
      
      // Use new backend API to update step config
      const updateResponse = await agentApi.updateStepConfig(
        workspacePath,
        stepId,
        updatedStep.agent_configs
      );

      if (!updateResponse.success) {
        throw new Error(updateResponse.message || "Failed to update step config");
      }


      // Update local state to reflect saved changes
      setSteps((prevSteps) => {
        const updated = [...prevSteps];
        
        if (stepPath) {
          console.log('[TodoStepsExtractedEvent] handleSaveStep - Updating branch step in local state:', {
            stepPath,
            stepIndex,
            stepTitle: updatedStep.title,
          });
          
          // Branch step: update the nested step within the parent's branch arrays
          // Parse path recursively (e.g., "step-2-if-true-0" or "step-2-if-true-0-if-false-1")
          const updateNestedStep = (steps: TodoStepWithConfigs[], pathParts: string[], stepToUpdate: TodoStepWithConfigs): TodoStepWithConfigs[] => {
            if (pathParts.length === 0) {
              return steps;
            }
            
            // Parse next part: "if-true-0" or "if-false-1"
            const partMatch = pathParts[0].match(/if-(true|false)-(\d+)/);
            if (!partMatch) {
              return steps;
            }
            
            const branchType = partMatch[1] as 'true' | 'false';
            const branchIndex = parseInt(partMatch[2]);
            
            const updatedSteps = [...steps];
            if (branchIndex < updatedSteps.length) {
              const currentStep = { ...updatedSteps[branchIndex] };
              
              if (pathParts.length === 1) {
                // This is the final step to update
                updatedSteps[branchIndex] = stepToUpdate;
              } else {
                // Recurse into nested branches
                const branchSteps = branchType === 'true' ? currentStep.if_true_steps : currentStep.if_false_steps;
                if (branchSteps) {
                  const updatedBranchSteps = updateNestedStep(branchSteps, pathParts.slice(1), stepToUpdate);
                  if (branchType === 'true') {
                    currentStep.if_true_steps = updatedBranchSteps;
                  } else {
                    currentStep.if_false_steps = updatedBranchSteps;
                  }
                }
                updatedSteps[branchIndex] = currentStep;
              }
            }
            
            return updatedSteps;
          };
          
          // Parse path: "step-2-if-true-0-if-false-1" -> ["if-true-0", "if-false-1"]
          const pathParts = stepPath.split('-');
          const stepIndexMatch = stepPath.match(/^step-(\d+)/);
          console.log('[TodoStepsExtractedEvent] handleSaveStep - Parsing path:', {
            stepPath,
            pathParts,
            stepIndexMatch,
          });
          
          if (stepIndexMatch) {
            const parentStepIndex = parseInt(stepIndexMatch[1]) - 1; // Convert to 0-based index
            console.log('[TodoStepsExtractedEvent] handleSaveStep - Parent step index:', {
              parentStepIndex,
              totalSteps: updated.length,
              parentStepExists: parentStepIndex >= 0 && parentStepIndex < updated.length,
            });
            
            if (parentStepIndex >= 0 && parentStepIndex < updated.length) {
              const parentStep = { ...updated[parentStepIndex] };
              
              // Extract branch parts (everything after "step-N-")
              const branchParts = pathParts.slice(2); // Skip "step" and step number
              const branchPathParts: string[] = [];
              for (let i = 0; i < branchParts.length; i += 3) {
                if (branchParts[i] === 'if' && (branchParts[i + 1] === 'true' || branchParts[i + 1] === 'false')) {
                  branchPathParts.push(`${branchParts[i]}-${branchParts[i + 1]}-${branchParts[i + 2]}`);
                }
              }
              
              console.log('[TodoStepsExtractedEvent] handleSaveStep - Extracted branch parts:', {
                branchParts,
                branchPathParts,
              });
              
              // Get the appropriate branch array
              if (branchPathParts.length > 0) {
                const firstBranch = branchPathParts[0].match(/if-(true|false)-(\d+)/);
                console.log('[TodoStepsExtractedEvent] handleSaveStep - First branch match:', {
                  firstBranch,
                  branchPathParts,
                });
                
                if (firstBranch) {
                  const branchType = firstBranch[1] as 'true' | 'false';
                  const branchSteps = branchType === 'true' ? parentStep.if_true_steps : parentStep.if_false_steps;
                  
                  console.log('[TodoStepsExtractedEvent] handleSaveStep - Branch steps found:', {
                    branchType,
                    branchStepsLength: branchSteps?.length || 0,
                    branchStepsExists: !!branchSteps,
                  });
                  
                  if (branchSteps) {
                    const updatedBranchSteps = updateNestedStep(branchSteps, branchPathParts, updatedStep);
                    console.log('[TodoStepsExtractedEvent] handleSaveStep - Updated branch steps:', {
                      originalLength: branchSteps.length,
                      updatedLength: updatedBranchSteps.length,
                    });
                    
                    if (branchType === 'true') {
                      parentStep.if_true_steps = updatedBranchSteps;
                    } else {
                      parentStep.if_false_steps = updatedBranchSteps;
                    }
                    updated[parentStepIndex] = parentStep;
                    console.log('[TodoStepsExtractedEvent] handleSaveStep - Successfully updated parent step');
                  } else {
                    console.warn('[TodoStepsExtractedEvent] handleSaveStep - Branch steps array not found');
                  }
                } else {
                  console.warn('[TodoStepsExtractedEvent] handleSaveStep - First branch match failed');
                }
              } else {
                console.warn('[TodoStepsExtractedEvent] handleSaveStep - No branch path parts extracted');
              }
            } else {
              console.warn('[TodoStepsExtractedEvent] handleSaveStep - Parent step index out of bounds');
            }
          } else {
            console.warn('[TodoStepsExtractedEvent] handleSaveStep - Step index match failed for path:', stepPath);
          }
        } else {
          // Top-level step: update directly
          updated[stepIndex] = updatedStep;
        }
        
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

  // Count step types for summary
  const stepSummary = React.useMemo(() => {
    let regular = 0;
    let conditional = 0;
    let loop = 0;
    
    steps.forEach(step => {
      // Note: steps here are TodoStepWithConfigs, not PlanStep, so we check boolean flags
      // This is fine since TodoStepWithConfigs still uses boolean flags for API compatibility
      if (step.has_condition) conditional++;
      else if (step.has_loop) loop++;
      else regular++;
    });
    
    return { regular, conditional, loop, total: steps.length };
  }, [steps]);

  return (
    <div className="bg-green-50 dark:bg-green-900/20 border border-green-200 dark:border-green-800 rounded-lg">
      {/* Header - clickable in workflow mode to expand/collapse */}
      <div 
        className={`p-3 ${!isCollapsed ? 'border-b border-green-200 dark:border-green-800' : ''} ${isWorkflowMode ? 'cursor-pointer hover:bg-green-100 dark:hover:bg-green-900/30 transition-colors' : ''}`}
        onClick={isWorkflowMode ? () => setIsCollapsed(!isCollapsed) : undefined}
      >
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            {/* Expand/Collapse icon in workflow mode */}
            {isWorkflowMode && (
              <div className="w-5 h-5 flex items-center justify-center text-green-600 dark:text-green-400">
                {isCollapsed ? <ChevronRight className="w-4 h-4" /> : <ChevronDown className="w-4 h-4" />}
              </div>
            )}
            <div className="w-7 h-7 bg-green-100 dark:bg-green-800/50 rounded-full flex items-center justify-center">
              <CheckCircle2 className="w-4 h-4 text-green-600 dark:text-green-400" />
            </div>
            <div className="text-sm font-medium text-green-700 dark:text-green-300">
              Plan Updated
            </div>
            {/* Step count summary */}
            <div className="flex items-center gap-2 ml-2">
              <span className="text-xs px-2 py-0.5 rounded-full bg-green-200 dark:bg-green-800 text-green-700 dark:text-green-300 font-medium">
                {stepSummary.total} steps
              </span>
              {stepSummary.conditional > 0 && (
                <span className="flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-purple-100 dark:bg-purple-900/30 text-purple-700 dark:text-purple-300">
                  <GitBranch className="w-3 h-3" />
                  {stepSummary.conditional}
                </span>
              )}
              {stepSummary.loop > 0 && (
                <span className="flex items-center gap-1 text-xs px-2 py-0.5 rounded-full bg-indigo-100 dark:bg-indigo-900/30 text-indigo-700 dark:text-indigo-300">
                  <RefreshCw className="w-3 h-3" />
                  {stepSummary.loop}
                </span>
              )}
            </div>
            {/* Workflow mode hint */}
            {isWorkflowMode && isCollapsed && (
              <span className="text-xs text-green-600 dark:text-green-400 italic ml-2">
                (view in React Flow canvas)
              </span>
            )}
          </div>
          <div className="flex items-center gap-3">
            {/* Config Source Indicator */}
            {!isLoading && !isCollapsed && (
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
      </div>

      {/* Steps List - hidden when collapsed */}
      {!isCollapsed && steps && steps.length > 0 && (
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
              
              // Render conditional step with nested branches
              if (step.has_condition) {
                return (
                  <ConditionalStepCard
                    key={stepKey}
                    step={step}
                    stepIndex={index}
                    eventId={eventId}
                    currentPresetServers={currentPresetServers}
                    presetLLMConfig={presetLLMConfig}
                    presetUseCodeExecutionMode={presetUseCodeExecutionMode}
                    onEditStep={handleEditStep}
                    onDeleteStep={setStepToDelete}
                    onSaveStep={handleSaveStep}
                    isSavingStepEdit={isSavingStepEdit}
                    isDeletingStep={isDeletingStep}
                    editingStepIndex={editingStepIndex}
                    startEditingStep={startEditingStep}
                    editedStepDescription={editedStepDescription}
                    setEditedStepDescription={setEditedStepDescription}
                    editedSuccessCriteria={editedSuccessCriteria}
                    setEditedSuccessCriteria={setEditedSuccessCriteria}
                    editedLoopCondition={editedLoopCondition}
                    setEditedLoopCondition={setEditedLoopCondition}
                    editedMaxIterations={editedMaxIterations}
                    setEditedMaxIterations={setEditedMaxIterations}
                    editedLoopDescription={editedLoopDescription}
                    setEditedLoopDescription={setEditedLoopDescription}
                    cancelEditingStep={cancelEditingStep}
                    expandedStepIndex={expandedStepIndex}
                    setExpandedStepIndex={setExpandedStepIndex}
                    isSaving={isSaving}
                  />
                );
              }
              
              // Render regular step
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
                      
                      {step.has_loop && step.loop_condition && (
                        <div className="text-xs">
                          <span className="font-medium text-indigo-700 dark:text-indigo-400">Loop Condition:</span>
                          <span className="text-gray-600 dark:text-gray-400 ml-1">{step.loop_condition}</span>
                        </div>
                      )}
                      
                      {step.has_loop && step.max_iterations && (
                        <div className="text-xs">
                          <span className="font-medium text-indigo-700 dark:text-indigo-400">Max Iterations:</span>
                          <span className="text-gray-600 dark:text-gray-400 ml-1">{step.max_iterations}</span>
                        </div>
                      )}
                      
                      {step.has_loop && step.loop_description && (
                        <div className="text-xs">
                          <span className="font-medium text-indigo-700 dark:text-indigo-400">Loop Description:</span>
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
                  presetLLMConfig={presetLLMConfig}
                  presetUseCodeExecutionMode={presetUseCodeExecutionMode}
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

// ConditionalStepCard component for rendering conditional steps with nested branches
interface ConditionalStepCardProps {
  step: TodoStepWithConfigs;
  stepIndex: number;
  eventId: string;
  currentPresetServers: string[];
  presetLLMConfig?: PresetLLMConfig | null;
  presetUseCodeExecutionMode?: boolean;
  onEditStep: (index: number) => Promise<void>;
  onDeleteStep: (index: number) => void;
  onSaveStep: (updatedStep: TodoStepWithConfigs, index: number, path?: string) => Promise<void>;
  isSavingStepEdit: boolean;
  isDeletingStep: boolean;
  editingStepIndex: number | null;
  startEditingStep: (index: number) => void;
  editedStepDescription: string;
  setEditedStepDescription: (desc: string) => void;
  editedSuccessCriteria: string;
  setEditedSuccessCriteria: (criteria: string) => void;
  editedLoopCondition: string;
  setEditedLoopCondition: (condition: string) => void;
  editedMaxIterations: string;
  setEditedMaxIterations: (iterations: string) => void;
  editedLoopDescription: string;
  setEditedLoopDescription: (desc: string) => void;
  cancelEditingStep: () => void;
  expandedStepIndex: number | null;
  setExpandedStepIndex: (index: number | null) => void;
  isSaving: boolean;
}

const ConditionalStepCard: React.FC<ConditionalStepCardProps> = ({
  step,
  stepIndex,
  eventId,
  currentPresetServers,
  presetLLMConfig,
  presetUseCodeExecutionMode = false,
  onEditStep,
  onDeleteStep,
  onSaveStep,
  isSavingStepEdit,
  isDeletingStep,
  editingStepIndex,
  startEditingStep,
  editedStepDescription,
  setEditedStepDescription,
  editedSuccessCriteria,
  setEditedSuccessCriteria,
  editedLoopCondition,
  setEditedLoopCondition,
  editedMaxIterations,
  setEditedMaxIterations,
  editedLoopDescription,
  setEditedLoopDescription,
  cancelEditingStep,
  expandedStepIndex,
  setExpandedStepIndex,
  isSaving,
}) => {
  const [expandedBranches, setExpandedBranches] = useState<{ true: boolean; false: boolean }>({ true: true, false: true });
  const [expandedNestedSteps, setExpandedNestedSteps] = useState<Set<string>>(new Set());
  // Track which branch step's config panel is expanded (by stepKey)
  const [expandedBranchStepConfigs, setExpandedBranchStepConfigs] = useState<Set<string>>(new Set());

  // Expand all nested steps by default when component mounts or step changes
  useEffect(() => {
    const allStepKeys = new Set<string>();
    
    // Collect all step keys from if_true_steps and if_false_steps
    const collectStepKeys = (steps: TodoStepWithConfigs[] | undefined, branchType: 'true' | 'false', depth: number) => {
      if (!steps) return;
      steps.forEach((nestedStep, idx) => {
        const stepKey = `${eventId}-${branchType}-${idx}-depth-${depth}`;
        allStepKeys.add(stepKey);
        
        // Recursively collect nested branch steps
        if (nestedStep.if_true_steps && nestedStep.if_true_steps.length > 0) {
          collectStepKeys(nestedStep.if_true_steps, 'true', depth + 1);
        }
        if (nestedStep.if_false_steps && nestedStep.if_false_steps.length > 0) {
          collectStepKeys(nestedStep.if_false_steps, 'false', depth + 1);
        }
      });
    };
    
    if (step.if_true_steps && step.if_true_steps.length > 0) {
      collectStepKeys(step.if_true_steps, 'true', 1);
    }
    if (step.if_false_steps && step.if_false_steps.length > 0) {
      collectStepKeys(step.if_false_steps, 'false', 1);
    }
    
    // Expand all collected step keys
    if (allStepKeys.size > 0) {
      setExpandedNestedSteps(allStepKeys);
    }
  }, [step.if_true_steps, step.if_false_steps, eventId]);

  const toggleBranch = useCallback((branch: 'true' | 'false') => {
    setExpandedBranches(prev => ({ ...prev, [branch]: !prev[branch] }));
  }, []);

  const toggleNestedStep = useCallback((stepKey: string) => {
    setExpandedNestedSteps(prev => {
      const next = new Set(prev);
      if (next.has(stepKey)) {
        next.delete(stepKey);
      } else {
        next.add(stepKey);
      }
      return next;
    });
  }, []);

  // Recursive function to render nested steps with full features
  // pathPrefix: path prefix for this branch (e.g., "step-2-if-true" for first-level branch steps)
  const renderNestedStep = useCallback((nestedStep: TodoStepWithConfigs, nestedIndex: number, depth: number, branchType: 'true' | 'false', pathPrefix?: string): React.ReactNode => {
    const maxDepth = 2;
    if (depth > maxDepth) {
      return (
        <div className="text-xs text-red-600 dark:text-red-400 p-2 bg-red-50 dark:bg-red-900/20 rounded">
          ⚠️ Maximum nesting depth ({maxDepth}) exceeded
        </div>
      );
    }

    // Generate path for this branch step
    // For first-level branch steps, pathPrefix is undefined, so we generate it from stepIndex
    const branchStepPath = pathPrefix 
      ? `${pathPrefix}-${nestedIndex}` 
      : `step-${stepIndex + 1}-if-${branchType}-${nestedIndex}`;

    const stepKey = `${eventId}-${branchType}-${nestedIndex}-depth-${depth}`;
    const isExpanded = expandedNestedSteps.has(stepKey);
    const isConfigExpanded = expandedBranchStepConfigs.has(stepKey);

    return (
      <div key={stepKey} className="ml-4 border-l-2 border-gray-300 dark:border-gray-600 pl-3 mt-2">
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-md p-2">
          <div className="flex items-center gap-2 mb-1">
            <button
              onClick={() => toggleNestedStep(stepKey)}
              className="p-0.5 hover:bg-gray-100 dark:hover:bg-gray-700 rounded"
            >
              {isExpanded ? (
                <ChevronDown className="w-3 h-3 text-gray-500" />
              ) : (
                <ChevronRight className="w-3 h-3 text-gray-500" />
              )}
            </button>
            <div className="text-xs font-medium text-gray-700 dark:text-gray-300 flex-1">
              {nestedStep.title || `Branch Step ${nestedIndex + 1}`}
            </div>
            {nestedStep.has_loop && (
              <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 bg-blue-100 dark:bg-blue-900/40 text-blue-700 dark:text-blue-300 rounded-md font-medium">
                <span>🔄</span>
                <span>Loop</span>
              </span>
            )}
            {nestedStep.has_condition && (
              <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-300 rounded-md font-medium">
                <span>🔀</span>
                <span>Conditional</span>
              </span>
            )}
          </div>
          
          {isExpanded && (
            <>
              {/* Step Description */}
              {nestedStep.description && (
                <div className="text-xs text-gray-600 dark:text-gray-400 leading-relaxed mb-2">
                  {nestedStep.description}
                </div>
              )}

              {/* Additional Step Information */}
              <div className="space-y-1 mb-2">
                {nestedStep.success_criteria && (
                  <div className="text-xs">
                    <span className="font-medium text-green-700 dark:text-green-400">Success Criteria:</span>
                    <span className="text-gray-600 dark:text-gray-400 ml-1">{nestedStep.success_criteria}</span>
                  </div>
                )}
                
                {nestedStep.has_loop && nestedStep.loop_condition && (
                  <div className="text-xs">
                    <span className="font-medium text-indigo-700 dark:text-indigo-400">Loop Condition:</span>
                    <span className="text-gray-600 dark:text-gray-400 ml-1">{nestedStep.loop_condition}</span>
                  </div>
                )}
                
                {nestedStep.has_loop && nestedStep.max_iterations && (
                  <div className="text-xs">
                    <span className="font-medium text-indigo-700 dark:text-indigo-400">Max Iterations:</span>
                    <span className="text-gray-600 dark:text-gray-400 ml-1">{nestedStep.max_iterations}</span>
                  </div>
                )}
                
                {nestedStep.has_loop && nestedStep.loop_description && (
                  <div className="text-xs">
                    <span className="font-medium text-indigo-700 dark:text-indigo-400">Loop Description:</span>
                    <span className="text-gray-600 dark:text-gray-400 ml-1 italic">{nestedStep.loop_description}</span>
                  </div>
                )}
                
                {nestedStep.context_dependencies && nestedStep.context_dependencies.length > 0 && (
                  <div className="text-xs">
                    <span className="font-medium text-purple-700 dark:text-purple-400">Context Dependencies:</span>
                    <div className="text-gray-600 dark:text-gray-400 ml-1">
                      {nestedStep.context_dependencies.map((dep, depIndex) => (
                        <div key={depIndex} className="text-xs text-gray-500 dark:text-gray-500 font-mono">
                          • {dep}
                        </div>
                      ))}
                    </div>
                  </div>
                )}
                
                {nestedStep.context_output && (
                  <div className="text-xs">
                    <span className="font-medium text-orange-700 dark:text-orange-400">Context Output:</span>
                    <span className="text-gray-600 dark:text-gray-400 ml-1 font-mono">{nestedStep.context_output}</span>
                  </div>
                )}
              </div>

              {/* Nested Conditional Display */}
              {nestedStep.has_condition && (
                <div className="mt-2 mb-2">
                  {nestedStep.condition_question && (
                    <div className="p-2 bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-700 rounded-md">
                      <div className="text-xs">
                        <span className="font-medium text-purple-700 dark:text-purple-400">Condition:</span>
                        <span className="text-gray-700 dark:text-gray-300 ml-1">{nestedStep.condition_question}</span>
                      </div>
                      {nestedStep.condition_context && (
                        <div className="text-xs mt-1">
                          <span className="font-medium text-purple-700 dark:text-purple-400">Context:</span>
                          <span className="text-gray-600 dark:text-gray-400 ml-1">{nestedStep.condition_context}</span>
                        </div>
                      )}
                      {nestedStep.condition_result !== undefined && (
                        <div className={`text-xs mt-1 p-1 rounded ${nestedStep.condition_result ? 'bg-green-50 dark:bg-green-900/20' : 'bg-red-50 dark:bg-red-900/20'}`}>
                          <span className="font-medium">{nestedStep.condition_result ? '✅ Decision: TRUE' : '❌ Decision: FALSE'}</span>
                          {nestedStep.condition_reason && (
                            <div className="text-gray-600 dark:text-gray-400 mt-1 italic">{nestedStep.condition_reason}</div>
                          )}
                        </div>
                      )}
                    </div>
                  )}

                  {nestedStep.if_true_steps && nestedStep.if_true_steps.length > 0 && (
                    <div className="mb-2 mt-2">
                      <div className="text-xs font-medium text-green-700 dark:text-green-400 mb-1">
                        ✅ If True ({nestedStep.if_true_steps.length} steps):
                      </div>
                      {nestedStep.if_true_steps.map((nestedBranchStep, idx) => 
                        renderNestedStep(nestedBranchStep, idx, depth + 1, 'true', `${branchStepPath}-if-true`)
                      )}
                    </div>
                  )}
                  {nestedStep.if_false_steps && nestedStep.if_false_steps.length > 0 && (
                    <div className="mt-2">
                      <div className="text-xs font-medium text-red-700 dark:text-red-400 mb-1">
                        ❌ If False ({nestedStep.if_false_steps.length} steps):
                      </div>
                      {nestedStep.if_false_steps.map((nestedBranchStep, idx) => 
                        renderNestedStep(nestedBranchStep, idx, depth + 1, 'false', `${branchStepPath}-if-false`)
                      )}
                    </div>
                  )}
                </div>
              )}

              {/* Configuration Panel for nested step */}
              <div className="mt-2">
                <StepEditPanel
                  step={nestedStep}
                  stepIndex={stepIndex} // Use parent step index for now
                  onSave={async (updatedStep) => {
                    // Save branch step config using path-based identification
                    // Capture branchStepPath in a const to ensure it's in scope
                    const currentBranchStepPath = branchStepPath;
                    console.log('[TodoStepsExtractedEvent] ConditionalStepCard - Saving branch step:', {
                      branchStepPath: currentBranchStepPath,
                      stepIndex,
                      stepTitle: updatedStep.title,
                      agentConfigs: updatedStep.agent_configs,
                      hasPath: !!currentBranchStepPath,
                    });
                    if (!currentBranchStepPath) {
                      console.error('[TodoStepsExtractedEvent] ConditionalStepCard - ERROR: branchStepPath is undefined!');
                    }
                    await onSaveStep(updatedStep, stepIndex, currentBranchStepPath);
                  }}
                  onCancel={() => {}}
                  isSaving={isSaving}
                  presetServers={currentPresetServers}
                  presetLLMConfig={presetLLMConfig}
                  presetUseCodeExecutionMode={presetUseCodeExecutionMode}
                  isExpanded={isConfigExpanded}
                  onToggleExpanded={(expanded) => {
                    setExpandedBranchStepConfigs(prev => {
                      const next = new Set(prev);
                      if (expanded) {
                        next.add(stepKey);
                      } else {
                        next.delete(stepKey);
                      }
                      return next;
                    });
                  }}
                />
              </div>
            </>
          )}
        </div>
      </div>
    );
  }, [stepIndex, eventId, expandedNestedSteps, expandedBranchStepConfigs, toggleNestedStep, onSaveStep, currentPresetServers, presetLLMConfig, presetUseCodeExecutionMode, isSaving]);

  return (
    <div className="bg-white dark:bg-gray-800 border-l-4 border-purple-500 border border-purple-200 dark:border-purple-700 rounded-md p-3">
      <div className="flex items-start gap-3">
        {/* Step Number */}
        <div className="w-5 h-5 bg-purple-100 dark:bg-purple-800/50 rounded-full flex items-center justify-center flex-shrink-0 mt-0.5">
          <span className="text-xs font-medium text-purple-700 dark:text-purple-300">
            {stepIndex + 1}
          </span>
        </div>
        
        {/* Step Content */}
        <div className="min-w-0 flex-1">
          <div className="flex items-center justify-between gap-2 mb-1 flex-wrap">
            <div className="flex items-center gap-2 flex-wrap">
              <div className="text-sm font-medium text-gray-900 dark:text-gray-100">
                {step.title || `Step ${stepIndex + 1}`}
              </div>
              <span className="inline-flex items-center gap-1 text-xs px-2 py-0.5 bg-purple-100 dark:bg-purple-900/40 text-purple-700 dark:text-purple-300 rounded-md font-medium">
                <span>🔀</span>
                <span>Conditional</span>
              </span>
            </div>
            {/* Edit and Delete buttons */}
            {editingStepIndex !== stepIndex && (
              <div className="flex items-center gap-1">
                <button
                  onClick={() => startEditingStep(stepIndex)}
                  disabled={isSavingStepEdit || isDeletingStep}
                  className="p-1.5 text-gray-500 hover:text-blue-600 dark:text-gray-400 dark:hover:text-blue-400 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
                  title="Edit step"
                >
                  <Edit2 className="w-4 h-4" />
                </button>
                <button
                  onClick={() => onDeleteStep(stepIndex)}
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
          {editingStepIndex === stepIndex ? (
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
                  <>
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
                    
                    {/* Max Iterations Editor */}
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
                    
                    {/* Loop Description Editor */}
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
                  </>
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
                    onClick={() => onEditStep(stepIndex)}
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
            <>
              {/* Step Description */}
              {step.description && (
                <div className="text-xs text-gray-600 dark:text-gray-400 leading-relaxed mb-2">
                  {step.description}
                </div>
              )}

              {/* Condition Display */}
              {step.condition_question && (
                <div className="mb-2 p-2 bg-purple-50 dark:bg-purple-900/20 border border-purple-200 dark:border-purple-700 rounded-md">
                  <div className="text-xs">
                    <span className="font-medium text-purple-700 dark:text-purple-400">Condition:</span>
                    <span className="text-gray-700 dark:text-gray-300 ml-1">{step.condition_question}</span>
                  </div>
                  {step.condition_context && (
                    <div className="text-xs mt-1">
                      <span className="font-medium text-purple-700 dark:text-purple-400">Context:</span>
                      <span className="text-gray-600 dark:text-gray-400 ml-1">{step.condition_context}</span>
                    </div>
                  )}
                  {step.condition_result !== undefined && (
                    <div className={`text-xs mt-1 p-1 rounded ${step.condition_result ? 'bg-green-50 dark:bg-green-900/20' : 'bg-red-50 dark:bg-red-900/20'}`}>
                      <span className="font-medium">{step.condition_result ? '✅ Decision: TRUE' : '❌ Decision: FALSE'}</span>
                      {step.condition_reason && (
                        <div className="text-gray-600 dark:text-gray-400 mt-1 italic">{step.condition_reason}</div>
                      )}
                    </div>
                  )}
                </div>
              )}

              {/* Additional Step Information */}
              <div className="space-y-1 mb-2">
                {step.success_criteria && (
                  <div className="text-xs">
                    <span className="font-medium text-green-700 dark:text-green-400">Success Criteria:</span>
                    <span className="text-gray-600 dark:text-gray-400 ml-1">{step.success_criteria}</span>
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
              </div>
            </>
          )}

          {/* Branches */}
          <div className="space-y-2 mt-2">
            {/* True Branch */}
            {step.if_true_steps && step.if_true_steps.length > 0 && (
              <div className="border-l-2 border-green-500 pl-3">
                <button
                  onClick={() => toggleBranch('true')}
                  className="flex items-center gap-2 w-full text-left hover:bg-gray-50 dark:hover:bg-gray-700/50 p-1 rounded"
                >
                  {expandedBranches.true ? (
                    <ChevronDown className="w-4 h-4 text-green-600" />
                  ) : (
                    <ChevronRight className="w-4 h-4 text-green-600" />
                  )}
                  <span className="text-xs font-medium text-green-700 dark:text-green-400">
                    ✅ If True ({step.if_true_steps.length} steps)
                  </span>
                </button>
                {expandedBranches.true && (
                  <div className="mt-1 space-y-1">
                    {step.if_true_steps.map((branchStep, idx) => 
                      renderNestedStep(branchStep, idx, 1, 'true')
                    )}
                  </div>
                )}
              </div>
            )}

            {/* False Branch */}
            {step.if_false_steps && step.if_false_steps.length > 0 && (
              <div className="border-l-2 border-red-500 pl-3">
                <button
                  onClick={() => toggleBranch('false')}
                  className="flex items-center gap-2 w-full text-left hover:bg-gray-50 dark:hover:bg-gray-700/50 p-1 rounded"
                >
                  {expandedBranches.false ? (
                    <ChevronDown className="w-4 h-4 text-red-600" />
                  ) : (
                    <ChevronRight className="w-4 h-4 text-red-600" />
                  )}
                  <span className="text-xs font-medium text-red-700 dark:text-red-400">
                    ❌ If False ({step.if_false_steps.length} steps)
                  </span>
                </button>
                {expandedBranches.false && (
                  <div className="mt-1 space-y-1">
                    {step.if_false_steps.map((branchStep, idx) => 
                      renderNestedStep(branchStep, idx, 1, 'false')
                    )}
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      </div>
      
      {/* Configuration Panel */}
      <StepEditPanel
        step={step}
        stepIndex={stepIndex}
        onSave={async (updatedStep) => {
          console.log('[TodoStepsExtractedEvent] ConditionalStepCard - Saving main conditional step (top-level, identified by index):', {
            stepIndex,
            stepTitle: updatedStep.title,
            isConditional: step.has_condition,
            stepPath: 'undefined (expected for top-level steps)',
          });
          await onSaveStep(updatedStep, stepIndex); // Main conditional step - no path needed (identified by index)
        }}
        onCancel={() => {}}
        isSaving={isSaving}
        presetServers={currentPresetServers}
        presetLLMConfig={presetLLMConfig}
        presetUseCodeExecutionMode={presetUseCodeExecutionMode}
        isExpanded={expandedStepIndex === stepIndex}
        onToggleExpanded={(expanded) => {
          setExpandedStepIndex(expanded ? stepIndex : null);
        }}
      />
    </div>
  );
};
