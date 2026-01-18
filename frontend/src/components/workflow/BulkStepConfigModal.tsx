import { useState, useEffect, useCallback, useMemo } from "react";
import { X, Settings, AlertCircle, CheckCircle2, Loader2, Code2, Sparkles, Brain, Shield, BookOpen, Wrench, Info, Book, FileStack } from "lucide-react";
import { Button } from "../ui/Button";
import { Accordion, AccordionItem, AccordionTrigger, AccordionContent } from "../ui/accordion";
import { useLLMStore } from "../../stores";
import { useGlobalPresetStore } from "../../stores/useGlobalPresetStore";
import type {
  AgentLLMConfig,
  AgentConfigs,
} from "../../utils/stepConfigMatching";
import type { LLMOption } from "../../types/llm";
import type {
  PlanningResponse,
  PlanStep,
} from "../../utils/stepConfigMatching";
import { isConditionalStep, isDecisionStep, isOrchestrationStep } from "../../utils/stepConfigMatching";
import { getToolsByCategory } from "../../utils/customToolNames";

interface BulkStepConfigModalProps {
  isOpen: boolean;
  onClose: () => void;
  plan: PlanningResponse | null;
  onBulkUpdate: (
    updates: Array<{ stepId: string; updates: Partial<PlanStep> }>
  ) => Promise<void>;
}

export default function BulkStepConfigModal({
  isOpen,
  onClose,
  plan,
  onBulkUpdate,
}: BulkStepConfigModalProps) {
  const { availableLLMs } = useLLMStore();
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);

  // Get preset for default LLMs and feature toggles
  const activePresetId = useGlobalPresetStore(
    (state) => state.activePresetIds.workflow
  );
  const customPresets = useGlobalPresetStore((state) => state.customPresets);
  const predefinedPresets = useGlobalPresetStore(
    (state) => state.predefinedPresets
  );
  const updatePreset = useGlobalPresetStore((state) => state.updatePreset);

  const activePreset = useMemo(() => {
    return activePresetId
      ? customPresets.find((p) => p.id === activePresetId) ||
          predefinedPresets.find((p) => p.id === activePresetId)
      : null;
  }, [activePresetId, customPresets, predefinedPresets]);

  // Get preset LLM configs
  const presetLLMConfig = activePreset?.llmConfig;

  // Feature toggles from preset (default to true if not set)
  const enableKnowledgebase = presetLLMConfig?.use_knowledgebase !== false;
  const enableContextSummarization = presetLLMConfig?.enable_context_summarization !== false;

  // Function to update feature toggles in the preset
  const handleToggleFeature = useCallback(async (
    feature: 'use_knowledgebase' | 'enable_context_summarization',
    enabled: boolean
  ) => {
    if (!activePreset || !activePresetId) return;

    const updatedLLMConfig = {
      ...presetLLMConfig,
      [feature]: enabled,
    };

    try {
      await updatePreset(
        activePresetId,
        activePreset.label,
        activePreset.query,
        activePreset.selectedServers,
        activePreset.selectedTools,
        activePreset.agentMode,
        activePreset.selectedFolder,
        updatedLLMConfig,
        activePreset.useCodeExecutionMode,
        feature === 'enable_context_summarization' ? enabled : activePreset.enableContextSummarization
      );
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 2000);
    } catch (error) {
      console.error('[BulkStepConfigModal] Error updating feature toggle:', error);
      setSaveError(error instanceof Error ? error.message : 'Failed to update setting');
    }
  }, [activePreset, activePresetId, presetLLMConfig, updatePreset]);

  const presetExecutionLLM = useMemo(() => {
    const llmConfig =
      presetLLMConfig?.execution_llm ||
      (presetLLMConfig?.provider && presetLLMConfig?.model_id
        ? {
            provider: presetLLMConfig.provider,
            model_id: presetLLMConfig.model_id,
          }
        : null);
    if (!llmConfig?.provider || !llmConfig?.model_id) return null;
    return (
      availableLLMs.find(
        (l) =>
          l.provider === llmConfig.provider && l.model === llmConfig.model_id
      ) || null
    );
  }, [presetLLMConfig, availableLLMs]);

  const presetValidationLLM = useMemo(() => {
    const llmConfig =
      presetLLMConfig?.validation_llm ||
      (presetLLMConfig?.provider && presetLLMConfig?.model_id
        ? {
            provider: presetLLMConfig.provider,
            model_id: presetLLMConfig.model_id,
          }
        : null);
    if (!llmConfig?.provider || !llmConfig?.model_id) return null;
    return (
      availableLLMs.find(
        (l) =>
          l.provider === llmConfig.provider && l.model === llmConfig.model_id
      ) || null
    );
  }, [presetLLMConfig, availableLLMs]);

  const presetLearningLLM = useMemo(() => {
    const llmConfig =
      presetLLMConfig?.learning_llm ||
      (presetLLMConfig?.provider && presetLLMConfig?.model_id
        ? {
            provider: presetLLMConfig.provider,
            model_id: presetLLMConfig.model_id,
          }
        : null);
    if (!llmConfig?.provider || !llmConfig?.model_id) return null;
    return (
      availableLLMs.find(
        (l) =>
          l.provider === llmConfig.provider && l.model === llmConfig.model_id
      ) || null
    );
  }, [presetLLMConfig, availableLLMs]);

  // Individual action states (for immediate application)
  const [applyingAction, setApplyingAction] = useState<string | null>(null);
  const [selectedMaxTurns, setSelectedMaxTurns] = useState<number>(100);

  // Reset form when modal opens/closes
  useEffect(() => {
    if (!isOpen) {
      // Reset all state when closing
      setSaveError(null);
      setSaveSuccess(false);
      setApplyingAction(null);
      setSelectedMaxTurns(100);
    }
  }, [isOpen]);

  // Handle Escape key to close modal
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && isOpen && applyingAction === null) {
        onClose();
      }
    };

    if (isOpen) {
      document.addEventListener("keydown", handleKeyDown);
    }

    return () => {
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [isOpen, onClose, applyingAction]);

  // Collect all steps including branch steps
  const getAllSteps = useCallback((): Array<{
    stepId: string;
    step: PlanStep;
    path: string;
  }> => {
    if (!plan || !plan.steps) return [];

    const allSteps: Array<{ stepId: string; step: PlanStep; path: string }> =
      [];

    const collectSteps = (steps: PlanStep[], path: string = "") => {
      steps.forEach((step, index) => {
        const stepPath = path ? `${path}.steps[${index}]` : `steps[${index}]`;
        allSteps.push({ stepId: step.id, step, path: stepPath });

        // Collect branch steps from conditional steps
        if (isConditionalStep(step)) {
          if (step.if_true_steps && step.if_true_steps.length > 0) {
            collectSteps(step.if_true_steps, `${stepPath}.if_true_steps`);
          }
          if (step.if_false_steps && step.if_false_steps.length > 0) {
            collectSteps(step.if_false_steps, `${stepPath}.if_false_steps`);
          }
        }

        // Collect decision step's inner step
        if (isDecisionStep(step) && step.decision_step) {
          allSteps.push({
            stepId: step.decision_step.id,
            step: step.decision_step,
            path: `${stepPath}.decision_step`,
          });
        }

        // Collect orchestration step's inner step and sub-agents
        if (isOrchestrationStep(step)) {
          // Collect main orchestration step
          if (step.orchestration_step) {
            allSteps.push({
              stepId: step.orchestration_step.id,
              step: step.orchestration_step,
              path: `${stepPath}.orchestration_step`,
            });
          }

          // Collect sub-agents from orchestration routes
          if (step.orchestration_routes && step.orchestration_routes.length > 0) {
            step.orchestration_routes.forEach((route, routeIndex) => {
              if (route.sub_agent_step) {
                allSteps.push({
                  stepId: route.sub_agent_step.id,
                  step: route.sub_agent_step,
                  path: `${stepPath}.orchestration_routes[${routeIndex}].sub_agent_step`,
                });
              }
            });
          }
        }
      });
    };

    collectSteps(plan.steps);
    return allSteps;
  }, [plan]);

  // Helper function to check if a tool is enabled in enabled_custom_tools
  const isToolEnabled = (enabledTools: string[], toolName: string, category: "workspace_tools" | "human_tools"): boolean => {
    // Empty array means all tools are enabled (default)
    if (enabledTools.length === 0) {
      return true;
    }
    
    // Check for wildcard (all tools in category enabled)
    if (enabledTools.includes(`${category}:*`)) {
      return true;
    }
    
    // Check for specific tool
    return enabledTools.includes(`${category}:${toolName}`);
  };

  // Check if tools are currently disabled by examining step configs
  const toolAccessState = useMemo(() => {
    if (!plan) {
      return { shellExecDisabled: false, readImageDisabled: false };
    }
    
    const allSteps = getAllSteps();
    let shellExecDisabledCount = 0;
    let readImageDisabledCount = 0;
    let totalSteps = 0;
    
    allSteps.forEach(({ step }) => {
      const enabledTools = step.agent_configs?.enabled_custom_tools || [];
      totalSteps++;
      
      // Check if tools are disabled (not enabled)
      if (!isToolEnabled(enabledTools, "execute_shell_command", "workspace_tools")) {
        shellExecDisabledCount++;
      }
      if (!isToolEnabled(enabledTools, "read_image", "workspace_tools")) {
        readImageDisabledCount++;
      }
    });
    
    // Tool is considered disabled if it's disabled in all steps
    return {
      shellExecDisabled: totalSteps > 0 && shellExecDisabledCount === totalSteps,
      readImageDisabled: totalSteps > 0 && readImageDisabledCount === totalSteps,
    };
  }, [plan, getAllSteps]);

  // Handle immediate action (disable/enable validation, learning, lock learnings, LLM updates, disable human tools, set max turns, learning detail level, agent mode)
  const handleImmediateAction = async (
    action:
      | "disable_validation"
      | "enable_validation"
      | "set_validation_mode_auto"
      | "set_validation_mode_always"
      | "set_validation_mode_skip"
      | "disable_learning"
      | "enable_learning"
      | "lock_learnings"
      | "unlock_learnings"
      | "set_execution_llm"
      | "set_validation_llm"
      | "set_learning_llm"
      | "disable_human_tools"
      | "enable_human_tools"
      | "disable_shell_exec_access"
      | "enable_shell_exec_access"
      | "disable_read_image_access"
      | "enable_read_image_access"
      | "set_execution_max_turns"
      | "set_learning_detail_level_exact"
      | "set_learning_detail_level_general"
      | "set_code_execution_mode"
      | "set_simple_mode",
    llm?: LLMOption | null,
    maxTurns?: number
  ) => {
    if (!plan) return;

    setApplyingAction(action);
    setSaveError(null);
    setSaveSuccess(false);

    try {
      const allSteps = getAllSteps();
      const stepConfigUpdates: Array<{
        stepId: string;
        agentConfigs: AgentConfigs | undefined;
      }> = [];

      allSteps.forEach(({ stepId, step }) => {
        const agentConfigs = step.agent_configs || {};
        const newAgentConfigs: typeof agentConfigs = { ...agentConfigs };

        // Apply the specific action
        switch (action) {
          case "disable_validation":
            newAgentConfigs.disable_validation = true;
            break;
          case "enable_validation":
            newAgentConfigs.disable_validation = false;
            break;
          case "set_validation_mode_auto":
            newAgentConfigs.llm_validation_mode = "auto";
            break;
          case "set_validation_mode_always":
            newAgentConfigs.llm_validation_mode = "always";
            break;
          case "set_validation_mode_skip":
            newAgentConfigs.llm_validation_mode = "skip";
            break;
          case "disable_learning":
            newAgentConfigs.disable_learning = true;
            break;
          case "enable_learning":
            newAgentConfigs.disable_learning = false;
            break;
          case "lock_learnings":
            newAgentConfigs.lock_learnings = true;
            break;
          case "unlock_learnings":
            newAgentConfigs.lock_learnings = false;
            break;
          case "set_execution_llm":
            if (llm) {
              newAgentConfigs.execution_llm = {
                provider: llm.provider as AgentLLMConfig["provider"],
                model_id: llm.model,
              };
            }
            break;
          case "set_validation_llm":
            if (llm) {
              newAgentConfigs.validation_llm = {
                provider: llm.provider as AgentLLMConfig["provider"],
                model_id: llm.model,
              };
            }
            break;
          case "set_learning_llm":
            if (llm) {
              newAgentConfigs.learning_llm = {
                provider: llm.provider as AgentLLMConfig["provider"],
                model_id: llm.model,
              };
            }
            break;
           case "disable_human_tools": {
             const currentEnabledTools = newAgentConfigs.enabled_custom_tools || [];
             
             // If human tools are already disabled, no change needed
             if (!isToolEnabled(currentEnabledTools, "human_feedback", "human_tools")) {
               break;
             }
             
             // If empty or has wildcard, replace with explicit list excluding human tools
             if (currentEnabledTools.length === 0 || currentEnabledTools.includes("human_tools:*")) {
               // Get all workspace tools and add them explicitly (exclude human tools)
               const workspaceTools = getToolsByCategory("workspace_tools");
               newAgentConfigs.enabled_custom_tools = workspaceTools.map(
                 (tool) => `workspace_tools:${tool}`
               );
             } else {
               // Remove all human_tools entries (both category:* and specific tools)
               newAgentConfigs.enabled_custom_tools = currentEnabledTools.filter(
                 (entry) => {
                   const parts = entry.split(":");
                   return parts.length !== 2 || parts[0] !== "human_tools";
                 }
               );
             }
             break;
           }
           case "enable_human_tools": {
             const currentEnabledTools = newAgentConfigs.enabled_custom_tools || [];
             
             // If human tools are already enabled, no change needed
             if (isToolEnabled(currentEnabledTools, "human_feedback", "human_tools")) {
               break;
             }
             
             // If empty, it's already enabled (default), but we'll keep it empty
             if (currentEnabledTools.length === 0) {
               break;
             }
             
             // Add human_feedback to the list (or add wildcard if we want all human tools)
             // For now, just add the specific tool
             const humanToolEntry = "human_tools:human_feedback";
             if (!currentEnabledTools.includes(humanToolEntry) && !currentEnabledTools.includes("human_tools:*")) {
               newAgentConfigs.enabled_custom_tools = [...currentEnabledTools, humanToolEntry];
             }
             break;
           }
           case "disable_shell_exec_access": {
             const currentEnabledTools = newAgentConfigs.enabled_custom_tools || [];
             const toolEntry = "workspace_tools:execute_shell_command";
             
             // If tool is already disabled, no change needed
             if (!isToolEnabled(currentEnabledTools, "execute_shell_command", "workspace_tools")) {
               break;
             }
             
             // If empty or has wildcard, replace with explicit list excluding the tool
             if (currentEnabledTools.length === 0 || currentEnabledTools.includes("workspace_tools:*")) {
               const allWorkspaceTools = getToolsByCategory("workspace_tools");
               newAgentConfigs.enabled_custom_tools = allWorkspaceTools
                 .filter((tool) => tool !== "execute_shell_command")
                 .map((tool) => `workspace_tools:${tool}`);
             } else {
               // Remove the tool from existing list (and remove wildcard if present)
               newAgentConfigs.enabled_custom_tools = currentEnabledTools
                 .filter((entry) => entry !== toolEntry && entry !== "workspace_tools:*");
             }
             break;
           }
           case "enable_shell_exec_access": {
             const currentEnabledTools = newAgentConfigs.enabled_custom_tools || [];
             const toolEntry = "workspace_tools:execute_shell_command";
             
             // If tool is already enabled, no change needed
             if (isToolEnabled(currentEnabledTools, "execute_shell_command", "workspace_tools")) {
               break;
             }
             
             // If empty, it's already enabled (default), but we'll keep it empty
             if (currentEnabledTools.length === 0) {
               break;
             }
             
             // Add the tool to the list
             if (!currentEnabledTools.includes(toolEntry)) {
               newAgentConfigs.enabled_custom_tools = [...currentEnabledTools, toolEntry];
             }
             
             // If we now have all workspace tools, we could clear to use defaults
             // But keeping explicit is safer - user can clear manually if needed
             // However, if we had wildcard before, we should restore it
             // Actually, if we're adding back, we should check if all tools are now present
             // For simplicity, just add the tool - user can optimize later
             break;
           }
           case "disable_read_image_access": {
             const currentEnabledTools = newAgentConfigs.enabled_custom_tools || [];
             const toolEntry = "workspace_tools:read_image";
             
             // If tool is already disabled, no change needed
             if (!isToolEnabled(currentEnabledTools, "read_image", "workspace_tools")) {
               break;
             }
             
             // If empty or has wildcard, replace with explicit list excluding the tool
             if (currentEnabledTools.length === 0 || currentEnabledTools.includes("workspace_tools:*")) {
               const allWorkspaceTools = getToolsByCategory("workspace_tools");
               newAgentConfigs.enabled_custom_tools = allWorkspaceTools
                 .filter((tool) => tool !== "read_image")
                 .map((tool) => `workspace_tools:${tool}`);
             } else {
               // Remove the tool from existing list (and remove wildcard if present)
               newAgentConfigs.enabled_custom_tools = currentEnabledTools
                 .filter((entry) => entry !== toolEntry && entry !== "workspace_tools:*");
             }
             break;
           }
           case "enable_read_image_access": {
             const currentEnabledTools = newAgentConfigs.enabled_custom_tools || [];
             const toolEntry = "workspace_tools:read_image";
             
             // If tool is already enabled, no change needed
             if (isToolEnabled(currentEnabledTools, "read_image", "workspace_tools")) {
               break;
             }
             
             // If empty, it's already enabled (default), but we'll keep it empty
             if (currentEnabledTools.length === 0) {
               break;
             }
             
             // Add the tool to the list
             if (!currentEnabledTools.includes(toolEntry)) {
               newAgentConfigs.enabled_custom_tools = [...currentEnabledTools, toolEntry];
             }
             
             // If we now have all workspace tools, we could clear to use defaults
             // But keeping explicit is safer - user can clear manually if needed
             break;
           }
           case "set_execution_max_turns":
             if (maxTurns !== undefined) {
               newAgentConfigs.execution_max_turns = maxTurns;
             }
             break;
          case "set_learning_detail_level_exact":
            newAgentConfigs.learning_detail_level = "exact";
            break;
          case "set_learning_detail_level_general":
            newAgentConfigs.learning_detail_level = "general";
            break;
          case "set_code_execution_mode":
            // Set code execution mode and auto-enable learning/validation
            newAgentConfigs.use_code_execution_mode = true;
            newAgentConfigs.disable_learning = false;
            newAgentConfigs.disable_validation = false;
            newAgentConfigs.learning_detail_level = "exact";
            break;
          case "set_simple_mode":
            // Set simple mode (disable code execution)
            newAgentConfigs.use_code_execution_mode = false;
            break;
        }

        stepConfigUpdates.push({
          stepId,
          agentConfigs: newAgentConfigs,
        });
      });

      // Use batch update API (handles both plan and config updates)
      const updates = stepConfigUpdates.map(({ stepId, agentConfigs }) => ({
        stepId,
        updates: { agent_configs: agentConfigs } as Partial<PlanStep>,
      }));
      await onBulkUpdate(updates);

      setSaveSuccess(true);

      // Reset success message after a delay
      setTimeout(() => {
        setSaveSuccess(false);
      }, 2000);
    } catch (error) {
      console.error("[BulkStepConfigModal] Error applying action:", error);
      setSaveError(
        error instanceof Error
          ? error.message
          : `Failed to ${action.replace(/_/g, " ")}`
      );
    } finally {
      setApplyingAction(null);
    }
  };

  const allSteps = getAllSteps();
  const stepCount = allSteps.length;

  if (!isOpen) return null;

  return (
    <div
      className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4"
      style={{ zIndex: 50 }}
    >
      <div className="bg-background border border-border rounded-xl shadow-2xl w-full max-w-4xl max-h-[90vh] flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-border bg-gradient-to-r from-background to-muted/20 flex-shrink-0">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-primary/10">
              <Settings className="w-5 h-5 text-primary" />
            </div>
            <div>
              <h2 className="text-xl font-bold text-foreground">
                Bulk Step Configuration
              </h2>
              {stepCount > 0 && (
                <p className="text-xs text-muted-foreground mt-0.5">
                  {stepCount} {stepCount === 1 ? "step" : "steps"} will be updated
                </p>
              )}
            </div>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={onClose}
            className="h-9 w-9 p-0 hover:bg-secondary rounded-lg"
            disabled={applyingAction !== null}
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        {/* Description */}
        <div className="px-6 py-3 border-b border-border bg-muted/40">
          <div className="flex items-start gap-2.5 text-sm text-muted-foreground">
            <AlertCircle className="w-4 h-4 mt-0.5 flex-shrink-0 text-blue-500" />
            <div>
              <p className="leading-relaxed">
                Apply configuration changes to <strong className="text-foreground">all steps</strong> in the
                workflow, including branch steps. Only fields you configure
                below will be updated.
              </p>
            </div>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 p-6 space-y-3 min-h-[400px] overflow-y-auto">
          <Accordion type="multiple" defaultValue={["llm", "validation", "learning", "agent-mode", "runtime-config", "tools-advanced"]} className="w-full space-y-3">
            {/* LLM Configuration Section */}
            <AccordionItem value="llm" className="border border-border rounded-xl bg-muted/10 hover:bg-muted/20 transition-colors shadow-sm">
              <AccordionTrigger className="hover:no-underline px-5 py-4">
                <div className="flex items-center gap-3">
                  <div className="p-1.5 rounded-md bg-purple-500/10">
                    <Brain className="w-4 h-4 text-purple-600 dark:text-purple-400" />
                  </div>
                  <span className="font-semibold text-base">LLM Configuration</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-5 pt-2 pb-6">
                <p className="text-sm text-muted-foreground mb-5 leading-relaxed">
                  Configure which LLM models to use for execution, validation, and learning phases across all steps.
                </p>
                <div className="grid grid-cols-1 gap-3">
                  {/* Set Execution LLM from Preset */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() =>
                        handleImmediateAction("set_execution_llm", presetExecutionLLM)
                      }
                      disabled={applyingAction !== null || !presetExecutionLLM}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-primary/5 hover:border-primary/50 transition-all"
                      title={
                        presetExecutionLLM
                          ? `Set to preset default: ${presetExecutionLLM.label}`
                          : "No preset execution LLM configured"
                      }
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "set_execution_llm" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Brain className="w-4 h-4 text-purple-600 dark:text-purple-400" />
                            <div className="flex-1 text-left">
                              <div className="font-medium text-sm">Set Execution LLM</div>
                              {presetExecutionLLM && (
                                <div className="text-xs text-muted-foreground mt-0.5">{presetExecutionLLM.label}</div>
                              )}
                            </div>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Sets the LLM model used by execution agents to perform step tasks. This is the primary model that will attempt to complete each step's objectives.
                    </p>
                  </div>

                  {/* Set Validation LLM from Preset */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() =>
                        handleImmediateAction(
                          "set_validation_llm",
                          presetValidationLLM
                        )
                      }
                      disabled={applyingAction !== null || !presetValidationLLM}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-primary/5 hover:border-primary/50 transition-all"
                      title={
                        presetValidationLLM
                          ? `Set to preset default: ${presetValidationLLM.label}`
                          : "No preset validation LLM configured"
                      }
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "set_validation_llm" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Shield className="w-4 h-4 text-blue-600 dark:text-blue-400" />
                            <div className="flex-1 text-left">
                              <div className="font-medium text-sm">Set Validation LLM</div>
                              {presetValidationLLM && (
                                <div className="text-xs text-muted-foreground mt-0.5">{presetValidationLLM.label}</div>
                              )}
                            </div>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Sets the LLM model used by validation agents to verify step outputs meet success criteria. This model checks if the execution results are correct and complete.
                    </p>
                  </div>

                  {/* Set Learning LLM from Preset */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() =>
                        handleImmediateAction("set_learning_llm", presetLearningLLM)
                      }
                      disabled={applyingAction !== null || !presetLearningLLM}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-primary/5 hover:border-primary/50 transition-all"
                      title={
                        presetLearningLLM
                          ? `Set to preset default: ${presetLearningLLM.label}`
                          : "No preset learning LLM configured"
                      }
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "set_learning_llm" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <BookOpen className="w-4 h-4 text-green-600 dark:text-green-400" />
                            <div className="flex-1 text-left">
                              <div className="font-medium text-sm">Set Learning LLM</div>
                              {presetLearningLLM && (
                                <div className="text-xs text-muted-foreground mt-0.5">{presetLearningLLM.label}</div>
                              )}
                            </div>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Sets the LLM model used by learning agents to extract insights and patterns from step execution. These insights help improve future step performance.
                    </p>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Validation Settings Section */}
            <AccordionItem value="validation" className="border border-border rounded-xl bg-muted/10 hover:bg-muted/20 transition-colors shadow-sm">
              <AccordionTrigger className="hover:no-underline px-5 py-4">
                <div className="flex items-center gap-3">
                  <div className="p-1.5 rounded-md bg-blue-500/10">
                    <Shield className="w-4 h-4 text-blue-600 dark:text-blue-400" />
                  </div>
                  <span className="font-semibold text-base">Validation Settings</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-5 pt-2 pb-6">
                <p className="text-sm text-muted-foreground mb-5 leading-relaxed">
                  Control validation behavior for all steps. Validation ensures step outputs meet quality standards.
                </p>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {/* Disable Validation */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("disable_validation")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-red-50 dark:hover:bg-red-950/20 hover:border-red-300 dark:hover:border-red-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "disable_validation" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <X className="w-4 h-4 text-red-500" />
                            <span className="font-medium text-sm">Disable Validation</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Turns off the validation phase entirely. Steps will proceed without quality checks, which is faster but may allow incorrect outputs.
                    </p>
                  </div>

                  {/* Enable Validation */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("enable_validation")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-green-50 dark:hover:bg-green-950/20 hover:border-green-300 dark:hover:border-green-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "enable_validation" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <CheckCircle2 className="w-4 h-4 text-green-500" />
                            <span className="font-medium text-sm">Enable Validation</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Enables the validation phase to verify step outputs meet success criteria. This ensures quality but adds execution time.
                    </p>
                  </div>

                  {/* Set Validation Mode: Auto */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_validation_mode_auto")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-blue-50 dark:hover:bg-blue-950/20 hover:border-blue-300 dark:hover:border-blue-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "set_validation_mode_auto" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Settings className="w-4 h-4 text-blue-500" />
                            <span className="font-medium text-sm">Set Mode: Auto</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Runs LLM validation for the first 3 successful executions, then skips (assuming stability).
                    </p>
                  </div>

                  {/* Set Validation Mode: Always */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_validation_mode_always")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-blue-50 dark:hover:bg-blue-950/20 hover:border-blue-300 dark:hover:border-blue-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "set_validation_mode_always" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Shield className="w-4 h-4 text-blue-500" />
                            <span className="font-medium text-sm">Set Mode: Always</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Ensures LLM validation always runs, even when pre-validation passes.
                    </p>
                  </div>

                  {/* Set Validation Mode: Skip */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_validation_mode_skip")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-orange-50 dark:hover:bg-orange-950/20 hover:border-orange-300 dark:hover:border-orange-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "set_validation_mode_skip" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Shield className="w-4 h-4 text-orange-500" />
                            <span className="font-medium text-sm">Set Mode: Skip</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Skips LLM validation if code-based pre-validation passes.
                    </p>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Learning Settings Section */}
            <AccordionItem value="learning" className="border border-border rounded-xl bg-muted/10 hover:bg-muted/20 transition-colors shadow-sm">
              <AccordionTrigger className="hover:no-underline px-5 py-4">
                <div className="flex items-center gap-3">
                  <div className="p-1.5 rounded-md bg-green-500/10">
                    <BookOpen className="w-4 h-4 text-green-600 dark:text-green-400" />
                  </div>
                  <span className="font-semibold text-base">Learning Settings</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-5 pt-2 pb-6">
                <p className="text-sm text-muted-foreground mb-5 leading-relaxed">
                  Control learning behavior. Learning agents extract insights and patterns from step execution to improve future performance.
                </p>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {/* Disable Learning */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("disable_learning")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-red-50 dark:hover:bg-red-950/20 hover:border-red-300 dark:hover:border-red-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "disable_learning" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <X className="w-4 h-4 text-red-500" />
                            <span className="font-medium text-sm">Disable Learning</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Turns off the learning phase entirely. No insights will be extracted from step execution, which speeds up execution but prevents knowledge accumulation.
                    </p>
                  </div>

                  {/* Enable Learning */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("enable_learning")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-green-50 dark:hover:bg-green-950/20 hover:border-green-300 dark:hover:border-green-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "enable_learning" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <CheckCircle2 className="w-4 h-4 text-green-500" />
                            <span className="font-medium text-sm">Enable Learning</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Enables the learning phase to extract insights and patterns from step execution. These insights help improve future step performance.
                    </p>
                  </div>

                  {/* Lock Learnings */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("lock_learnings")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-yellow-50 dark:hover:bg-yellow-950/20 hover:border-yellow-300 dark:hover:border-yellow-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "lock_learnings" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Settings className="w-4 h-4 text-yellow-500" />
                            <span className="font-medium text-sm">Lock Learnings</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Prevents new learning from being generated but still uses existing learnings. Useful when you want to preserve current knowledge without modification.
                    </p>
                  </div>

                  {/* Unlock Learnings */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("unlock_learnings")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-blue-50 dark:hover:bg-blue-950/20 hover:border-blue-300 dark:hover:border-blue-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "unlock_learnings" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Settings className="w-4 h-4 text-blue-500" />
                            <span className="font-medium text-sm">Unlock Learnings</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Allows learning agents to generate new insights from step execution. Enables continuous improvement of workflow knowledge.
                    </p>
                  </div>

                  {/* Set Learning Detail Level to Exact */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_learning_detail_level_exact")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-primary/5 hover:border-primary/50 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "set_learning_detail_level_exact" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Info className="w-4 h-4 text-primary" />
                            <span className="font-medium text-sm">Set Detail: Exact</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Learning agents will extract precise, step-specific insights. More detailed and specific to each step, useful for complex workflows.
                    </p>
                  </div>

                  {/* Set Learning Detail Level to General */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_learning_detail_level_general")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-primary/5 hover:border-primary/50 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "set_learning_detail_level_general" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Info className="w-4 h-4 text-primary" />
                            <span className="font-medium text-sm">Set Detail: General</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Learning agents will extract high-level, reusable patterns. Broader insights that can be applied across multiple steps.
                    </p>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Agent Mode Section */}
            <AccordionItem value="agent-mode" className="border border-border rounded-xl bg-muted/10 hover:bg-muted/20 transition-colors shadow-sm">
              <AccordionTrigger className="hover:no-underline px-5 py-4">
                <div className="flex items-center gap-3">
                  <div className="p-1.5 rounded-md bg-indigo-500/10">
                    <Code2 className="w-4 h-4 text-indigo-600 dark:text-indigo-400" />
                  </div>
                  <span className="font-semibold text-base">Agent Mode</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-5 pt-2 pb-6">
                <p className="text-sm text-muted-foreground mb-5 leading-relaxed">
                  Choose the execution mode for agents. Code execution mode enables code generation and execution capabilities.
                </p>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {/* Set Agent Mode to Code Exec */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_code_execution_mode")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-indigo-50 dark:hover:bg-indigo-950/20 hover:border-indigo-300 dark:hover:border-indigo-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "set_code_execution_mode" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Code2 className="w-4 h-4 text-indigo-600 dark:text-indigo-400" />
                            <span className="font-medium text-sm">Code Execution Mode</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Enables code generation and execution capabilities. Agents can write and run code to complete tasks. Automatically enables learning and validation.
                    </p>
                  </div>

                  {/* Set Agent Mode to Simple */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_simple_mode")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-purple-50 dark:hover:bg-purple-950/20 hover:border-purple-300 dark:hover:border-purple-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "set_simple_mode" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <Sparkles className="w-4 h-4 text-purple-600 dark:text-purple-400" />
                            <span className="font-medium text-sm">Simple Mode</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Disables code execution. Agents use only tools and natural language. Faster and simpler execution, suitable for straightforward tasks.
                    </p>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Runtime Configuration Section */}
            <AccordionItem value="runtime-config" className="border border-border rounded-xl bg-muted/10 hover:bg-muted/20 transition-colors shadow-sm">
              <AccordionTrigger className="hover:no-underline px-5 py-4">
                <div className="flex items-center gap-3">
                  <div className="p-1.5 rounded-md bg-teal-500/10">
                    <Settings className="w-4 h-4 text-teal-600 dark:text-teal-400" />
                  </div>
                  <span className="font-semibold text-base">Runtime Configuration</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-5 pt-2 pb-6">
                <p className="text-sm text-muted-foreground mb-5 leading-relaxed">
                  Configure runtime features that affect execution behavior. These settings persist across sessions.
                </p>
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {/* Enable Knowledgebase Toggle */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleToggleFeature('use_knowledgebase', !enableKnowledgebase)}
                      disabled={applyingAction !== null || !activePreset}
                      className={`w-full justify-start h-auto py-3 px-4 transition-all ${
                        enableKnowledgebase
                          ? "bg-green-50 dark:bg-green-950/20 border-green-300 dark:border-green-800 hover:bg-green-100 dark:hover:bg-green-950/30"
                          : "hover:bg-red-50 dark:hover:bg-red-950/20 hover:border-red-300 dark:hover:border-red-800"
                      }`}
                    >
                      <div className="flex items-center gap-3 w-full">
                        <Book className={`w-4 h-4 ${enableKnowledgebase ? 'text-green-600 dark:text-green-400' : 'text-muted-foreground'}`} />
                        <div className="flex-1 text-left">
                          <div className="font-medium text-sm">Knowledgebase</div>
                          <div className={`text-xs mt-0.5 ${enableKnowledgebase ? 'text-green-600 dark:text-green-400' : 'text-muted-foreground'}`}>
                            {enableKnowledgebase ? 'Enabled' : 'Disabled'}
                          </div>
                        </div>
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Enables access to learnings and knowledge during execution. When disabled, agents won't use stored learnings from previous runs.
                    </p>
                  </div>

                  {/* Enable Context Summarization Toggle */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleToggleFeature('enable_context_summarization', !enableContextSummarization)}
                      disabled={applyingAction !== null || !activePreset}
                      className={`w-full justify-start h-auto py-3 px-4 transition-all ${
                        enableContextSummarization
                          ? "bg-blue-50 dark:bg-blue-950/20 border-blue-300 dark:border-blue-800 hover:bg-blue-100 dark:hover:bg-blue-950/30"
                          : "hover:bg-red-50 dark:hover:bg-red-950/20 hover:border-red-300 dark:hover:border-red-800"
                      }`}
                    >
                      <div className="flex items-center gap-3 w-full">
                        <FileStack className={`w-4 h-4 ${enableContextSummarization ? 'text-blue-600 dark:text-blue-400' : 'text-muted-foreground'}`} />
                        <div className="flex-1 text-left">
                          <div className="font-medium text-sm">Context Summarization</div>
                          <div className={`text-xs mt-0.5 ${enableContextSummarization ? 'text-blue-600 dark:text-blue-400' : 'text-muted-foreground'}`}>
                            {enableContextSummarization ? 'Enabled' : 'Disabled'}
                          </div>
                        </div>
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Enables automatic context summarization during execution. When disabled, full conversation context is maintained without compression.
                    </p>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Tools & Advanced Section */}
            <AccordionItem value="tools-advanced" className="border border-border rounded-xl bg-muted/10 hover:bg-muted/20 transition-colors shadow-sm">
              <AccordionTrigger className="hover:no-underline px-5 py-4">
                <div className="flex items-center gap-3">
                  <div className="p-1.5 rounded-md bg-amber-500/10">
                    <Wrench className="w-4 h-4 text-amber-600 dark:text-amber-400" />
                  </div>
                  <span className="font-semibold text-base">Tools & Advanced</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="px-5 pt-2 pb-6">
                <p className="text-sm text-muted-foreground mb-5 leading-relaxed">
                  Configure tool access and execution parameters for all steps.
                </p>
                <div className="space-y-5">
                  {/* Disable Human Feedback Tools */}
                  <div className="space-y-2">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("disable_human_tools")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start h-auto py-3 px-4 hover:bg-red-50 dark:hover:bg-red-950/20 hover:border-red-300 dark:hover:border-red-800 transition-all"
                    >
                      <div className="flex items-center gap-3 w-full">
                        {applyingAction === "disable_human_tools" ? (
                          <>
                            <Loader2 className="w-4 h-4 animate-spin text-primary" />
                            <span className="font-medium">Applying...</span>
                          </>
                        ) : (
                          <>
                            <X className="w-4 h-4 text-red-500" />
                            <span className="font-medium text-sm">Disable Human Feedback Tools</span>
                          </>
                        )}
                      </div>
                    </Button>
                    <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                      Removes human feedback tools from available tools. Agents cannot request human input during execution, enabling fully automated workflows.
                    </p>
                  </div>

                  {/* Tool Access Control Section */}
                  <div className="space-y-4 pt-4 border-t border-border">
                    <div className="flex items-center gap-2.5 mb-3">
                      <div className="p-1 rounded-md bg-blue-500/10">
                        <Shield className="w-4 h-4 text-blue-600 dark:text-blue-400" />
                      </div>
                      <div className="text-sm font-semibold text-foreground">
                        Tool Access Control
                      </div>
                    </div>
                    
                    {/* Disable/Enable Shell Exec Access */}
                    <div className="space-y-2">
                      <Button
                        variant="outline"
                        onClick={() => handleImmediateAction(
                          toolAccessState.shellExecDisabled 
                            ? "enable_shell_exec_access" 
                            : "disable_shell_exec_access"
                        )}
                        disabled={applyingAction !== null}
                        className={`w-full justify-start h-auto py-3 px-4 transition-all ${
                          toolAccessState.shellExecDisabled
                            ? "hover:bg-green-50 dark:hover:bg-green-950/20 hover:border-green-300 dark:hover:border-green-800"
                            : "hover:bg-red-50 dark:hover:bg-red-950/20 hover:border-red-300 dark:hover:border-red-800"
                        }`}
                      >
                        <div className="flex items-center gap-3 w-full">
                          {applyingAction === "disable_shell_exec_access" || applyingAction === "enable_shell_exec_access" ? (
                            <Loader2 className="w-4 h-4 animate-spin" />
                          ) : toolAccessState.shellExecDisabled ? (
                            <>
                              <CheckCircle2 className="w-4 h-4 text-green-500" />
                              <span className="font-medium text-sm">Enable Shell Exec Access</span>
                            </>
                          ) : (
                            <>
                              <X className="w-4 h-4 text-red-500" />
                              <span className="font-medium text-sm">Disable Shell Exec Access</span>
                            </>
                          )}
                        </div>
                      </Button>
                      <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                        {toolAccessState.shellExecDisabled ? (
                          <>Currently disabled. Click to enable the <code className="text-xs bg-background px-1.5 py-0.5 rounded border border-border">execute_shell_command</code> tool for all steps.</>
                        ) : (
                          <>Disables the <code className="text-xs bg-background px-1.5 py-0.5 rounded border border-border">execute_shell_command</code> tool for all steps. This prevents agents from executing shell commands, providing an additional security layer for sensitive environments.</>
                        )}
                      </p>
                    </div>

                    {/* Disable/Enable Read Image Access */}
                    <div className="space-y-2">
                      <Button
                        variant="outline"
                        onClick={() => handleImmediateAction(
                          toolAccessState.readImageDisabled 
                            ? "enable_read_image_access" 
                            : "disable_read_image_access"
                        )}
                        disabled={applyingAction !== null}
                        className={`w-full justify-start h-auto py-3 px-4 transition-all ${
                          toolAccessState.readImageDisabled
                            ? "hover:bg-green-50 dark:hover:bg-green-950/20 hover:border-green-300 dark:hover:border-green-800"
                            : "hover:bg-red-50 dark:hover:bg-red-950/20 hover:border-red-300 dark:hover:border-red-800"
                        }`}
                      >
                        <div className="flex items-center gap-3 w-full">
                          {applyingAction === "disable_read_image_access" || applyingAction === "enable_read_image_access" ? (
                            <Loader2 className="w-4 h-4 animate-spin" />
                          ) : toolAccessState.readImageDisabled ? (
                            <>
                              <CheckCircle2 className="w-4 h-4 text-green-500" />
                              <span className="font-medium text-sm">Enable Read Image Access</span>
                            </>
                          ) : (
                            <>
                              <X className="w-4 h-4 text-red-500" />
                              <span className="font-medium text-sm">Disable Read Image Access</span>
                            </>
                          )}
                        </div>
                      </Button>
                      <p className="text-xs text-muted-foreground ml-7 leading-relaxed">
                        {toolAccessState.readImageDisabled ? (
                          <>Currently disabled. Click to enable the <code className="text-xs bg-background px-1.5 py-0.5 rounded border border-border">read_image</code> tool for all steps.</>
                        ) : (
                          <>Disables the <code className="text-xs bg-background px-1.5 py-0.5 rounded border border-border">read_image</code> tool for all steps. This prevents agents from reading and analyzing image files, useful for workflows that don't require image processing.</>
                        )}
                      </p>
                    </div>
                  </div>

                  {/* Execution Max Turns Section */}
                  <div className="space-y-3 pt-4 border-t border-border">
                    <div className="flex items-center gap-2.5">
                      <div className="p-1 rounded-md bg-gray-500/10">
                        <Settings className="w-4 h-4 text-gray-600 dark:text-gray-400" />
                      </div>
                      <div className="text-sm font-semibold text-foreground">
                        Execution Max Turns
                      </div>
                    </div>
                    <div className="bg-muted/30 rounded-lg p-4 border border-border/50">
                      <div className="flex items-center gap-3 flex-wrap">
                        <label className="text-sm font-medium text-foreground whitespace-nowrap">
                          Max Turns:
                        </label>
                        <select
                          value={selectedMaxTurns}
                          onChange={(e) => setSelectedMaxTurns(parseInt(e.target.value))}
                          disabled={applyingAction !== null}
                          className="flex-1 min-w-[120px] px-3 py-2 text-sm border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent disabled:opacity-50 disabled:cursor-not-allowed"
                        >
                          {[10, 25, 50, 75, 100].map((value) => (
                            <option key={value} value={value}>
                              {value} turns
                            </option>
                          ))}
                        </select>
                        <Button
                          variant="outline"
                          onClick={() =>
                            handleImmediateAction("set_execution_max_turns", null, selectedMaxTurns)
                          }
                          disabled={applyingAction !== null}
                          className="whitespace-nowrap hover:bg-primary hover:text-primary-foreground transition-colors"
                        >
                          {applyingAction === "set_execution_max_turns" ? (
                            <>
                              <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                              Applying...
                            </>
                          ) : (
                            "Apply"
                          )}
                        </Button>
                      </div>
                      <p className="text-xs text-muted-foreground mt-3 leading-relaxed">
                        Maximum number of conversation turns allowed for execution agents per step. Prevents infinite loops and controls execution time. Higher values allow more complex reasoning but may increase costs.
                      </p>
                    </div>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>
          </Accordion>

          <div className="mt-6 p-4 bg-blue-50 dark:bg-blue-950/20 border border-blue-200 dark:border-blue-800 rounded-lg">
            <div className="flex items-start gap-3 text-sm text-blue-700 dark:text-blue-300">
              <Info className="w-5 h-5 mt-0.5 flex-shrink-0" />
              <p className="leading-relaxed">
                <strong className="font-semibold">Note:</strong> All actions are applied immediately to all steps in the workflow, including branch steps. Changes take effect right away - no need to click a separate "Apply" button.
              </p>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-6 py-4 border-t border-border bg-muted/40 flex-shrink-0">
          <div className="flex items-center gap-2">
            {saveSuccess && (
              <div className="flex items-center gap-2 text-sm text-green-600 dark:text-green-400">
                <CheckCircle2 className="w-4 h-4" />
                <span>Successfully updated all steps</span>
              </div>
            )}
            {saveError && (
              <div className="flex items-center gap-2 text-sm text-red-600 dark:text-red-400">
                <AlertCircle className="w-4 h-4" />
                <span>{saveError}</span>
              </div>
            )}
            {!saveSuccess && !saveError && stepCount > 0 && (
              <span className="text-xs text-muted-foreground">
                Click buttons above to apply changes to all steps
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              onClick={onClose}
              disabled={applyingAction !== null}
            >
              Close
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
