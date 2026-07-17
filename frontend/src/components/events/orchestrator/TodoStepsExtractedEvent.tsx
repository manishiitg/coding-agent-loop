import React, { useState, useEffect, useCallback } from "react";
import { StepEditPanel } from "./StepEditPanel";
import { agentApi } from "../../../services/api";
import { usePresetApplication } from "../../../stores/useGlobalPresetStore";
import { useModeStore } from "../../../stores/useModeStore";
import type { TodoStepsExtractedEvent } from "../../../generated/events-bridge";
import type { TodoStepWithConfigs, AgentConfigs, StepConfig } from "../../../utils/stepConfigMatching";
import ConfirmationDialog from "../../ui/ConfirmationDialog";
import { Edit2, Trash2, Save, X, ChevronDown, ChevronRight, RefreshCw, CheckCircle2 } from "lucide-react";

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


  // Load and apply step_config.json configs after the event loads.
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

        const applyConfigsToStep = (step: TodoStepWithConfigs): TodoStepWithConfigs => {
          if (!step.id) {
            throw new Error(`Step is missing required ID field. Step title: "${step.title || 'unknown'}"`);
          }
          return {
            ...step,
            agent_configs: idConfigMap.get(step.id) || step.agent_configs,
          };
        };

        // Apply configs to all steps
        const stepsWithConfigs = (event.extracted_steps as TodoStepWithConfigs[]).map((step) =>
          applyConfigsToStep(step)
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
  const handleSaveStep = async (updatedStep: TodoStepWithConfigs, stepIndex: number) => {
    setIsSaving(true);
    try {
      const workspacePath = getWorkspacePath(event);
      if (!workspacePath) throw new Error('Workspace path not found in event');
      if (!updatedStep.id) {
        throw new Error(`Cannot save step config: step "${updatedStep.title || 'unknown'}" is missing its required ID`);
      }

      const updateResponse = await agentApi.updateStepConfig(
        workspacePath,
        updatedStep.id,
        updatedStep.agent_configs
      );
      if (!updateResponse.success) {
        throw new Error(updateResponse.message || 'Failed to update step config');
      }

      setSteps((previousSteps) => previousSteps.map((step, index) =>
        index === stepIndex ? updatedStep : step
      ));
      setConfigSource('default');
    } catch (error) {
      console.error('Failed to save step configuration:', error);
      alert(`Failed to save step configuration: ${error instanceof Error ? error.message : 'Unknown error'}`);
    } finally {
      setIsSaving(false);
    }
  };

  // Count step types for summary
  const stepSummary = React.useMemo(() => ({
    loop: steps.filter(step => step.has_loop).length,
    total: steps.length,
  }), [steps]);

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
