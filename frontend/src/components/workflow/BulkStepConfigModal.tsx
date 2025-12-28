import { useState, useEffect, useCallback, useMemo } from "react";
import { X, Settings, AlertCircle, CheckCircle2, Loader2, Code2, Sparkles, Brain, Shield, BookOpen, Wrench, Info } from "lucide-react";
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

  // Get preset for default LLMs
  const activePresetId = useGlobalPresetStore(
    (state) => state.activePresetIds.workflow
  );
  const customPresets = useGlobalPresetStore((state) => state.customPresets);
  const predefinedPresets = useGlobalPresetStore(
    (state) => state.predefinedPresets
  );

  const activePreset = useMemo(() => {
    return activePresetId
      ? customPresets.find((p) => p.id === activePresetId) ||
          predefinedPresets.find((p) => p.id === activePresetId)
      : null;
  }, [activePresetId, customPresets, predefinedPresets]);

  // Get preset LLM configs
  const presetLLMConfig = activePreset?.llmConfig;
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

  // Handle immediate action (disable/enable validation, learning, lock learnings, LLM updates, disable human tools, set max turns, learning detail level, agent mode)
  const handleImmediateAction = async (
    action:
      | "disable_validation"
      | "enable_validation"
      | "skip_llm_validation"
      | "enable_llm_validation"
      | "disable_learning"
      | "enable_learning"
      | "lock_learnings"
      | "unlock_learnings"
      | "set_execution_llm"
      | "set_validation_llm"
      | "set_learning_llm"
      | "disable_human_tools"
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
          case "skip_llm_validation":
            newAgentConfigs.skip_llm_validation_if_pre_validation_passes = true;
            break;
          case "enable_llm_validation":
            newAgentConfigs.skip_llm_validation_if_pre_validation_passes = false;
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
             // Remove all human_tools entries from enabled_custom_tools
             const currentEnabledTools =
               newAgentConfigs.enabled_custom_tools || [];
             // If array is empty (default = all enabled), explicitly enable workspace_tools only
             if (currentEnabledTools.length === 0) {
               // Get all workspace tools and add them explicitly
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
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-3xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-2">
            <Settings className="w-5 h-5 text-primary" />
            <h2 className="text-lg font-semibold text-foreground">
              Bulk Step Configuration
            </h2>
            {stepCount > 0 && (
              <span className="text-sm text-muted-foreground">
                ({stepCount} {stepCount === 1 ? "step" : "steps"})
              </span>
            )}
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={onClose}
            className="h-8 w-8 p-0 hover:bg-secondary"
            disabled={applyingAction !== null}
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        {/* Description */}
        <div className="px-4 py-3 border-b border-border bg-muted/30">
          <div className="flex items-start gap-2 text-sm text-muted-foreground">
            <AlertCircle className="w-4 h-4 mt-0.5 flex-shrink-0" />
            <div>
              <p>
                Apply configuration changes to <strong>all steps</strong> in the
                workflow, including branch steps. Only fields you configure
                below will be updated.
              </p>
            </div>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 p-6 space-y-4 min-h-[400px] overflow-y-auto">
          <Accordion type="multiple" defaultValue={["llm", "validation", "learning"]} className="w-full space-y-2">
            {/* LLM Configuration Section */}
            <AccordionItem value="llm" className="border border-border rounded-lg px-4 bg-muted/20">
              <AccordionTrigger className="hover:no-underline">
                <div className="flex items-center gap-2">
                  <Brain className="w-4 h-4 text-primary" />
                  <span className="font-semibold">LLM Configuration</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="pt-4 pb-6">
                <p className="text-xs text-muted-foreground mb-4">
                  Configure which LLM models to use for execution, validation, and learning phases across all steps.
                </p>
                <div className="grid grid-cols-1 gap-3">
                  {/* Set Execution LLM from Preset */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() =>
                        handleImmediateAction("set_execution_llm", presetExecutionLLM)
                      }
                      disabled={applyingAction !== null || !presetExecutionLLM}
                      className="w-full justify-start"
                      title={
                        presetExecutionLLM
                          ? `Set to preset default: ${presetExecutionLLM.label}`
                          : "No preset execution LLM configured"
                      }
                    >
                      {applyingAction === "set_execution_llm" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <Brain className="w-4 h-4 mr-2" />
                          Set Execution LLM{presetExecutionLLM ? ` (${presetExecutionLLM.label})` : ""}
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Sets the LLM used by execution agents to perform step tasks
                    </p>
                  </div>

                  {/* Set Validation LLM from Preset */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() =>
                        handleImmediateAction(
                          "set_validation_llm",
                          presetValidationLLM
                        )
                      }
                      disabled={applyingAction !== null || !presetValidationLLM}
                      className="w-full justify-start"
                      title={
                        presetValidationLLM
                          ? `Set to preset default: ${presetValidationLLM.label}`
                          : "No preset validation LLM configured"
                      }
                    >
                      {applyingAction === "set_validation_llm" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <Shield className="w-4 h-4 mr-2" />
                          Set Validation LLM{presetValidationLLM ? ` (${presetValidationLLM.label})` : ""}
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Sets the LLM used by validation agents to verify step outputs
                    </p>
                  </div>

                  {/* Set Learning LLM from Preset */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() =>
                        handleImmediateAction("set_learning_llm", presetLearningLLM)
                      }
                      disabled={applyingAction !== null || !presetLearningLLM}
                      className="w-full justify-start"
                      title={
                        presetLearningLLM
                          ? `Set to preset default: ${presetLearningLLM.label}`
                          : "No preset learning LLM configured"
                      }
                    >
                      {applyingAction === "set_learning_llm" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <BookOpen className="w-4 h-4 mr-2" />
                          Set Learning LLM{presetLearningLLM ? ` (${presetLearningLLM.label})` : ""}
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Sets the LLM used by learning agents to extract insights from step execution
                    </p>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Validation Settings Section */}
            <AccordionItem value="validation" className="border border-border rounded-lg px-4 bg-muted/20">
              <AccordionTrigger className="hover:no-underline">
                <div className="flex items-center gap-2">
                  <Shield className="w-4 h-4 text-primary" />
                  <span className="font-semibold">Validation Settings</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="pt-4 pb-6">
                <p className="text-xs text-muted-foreground mb-4">
                  Control validation behavior for all steps. Validation ensures step outputs meet quality standards.
                </p>
                <div className="grid grid-cols-1 gap-3">
                  {/* Disable Validation */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("disable_validation")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "disable_validation" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <X className="w-4 h-4 mr-2 text-red-500" />
                          Disable Validation
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Turns off validation phase entirely - steps will proceed without quality checks
                    </p>
                  </div>

                  {/* Enable Validation */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("enable_validation")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "enable_validation" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <CheckCircle2 className="w-4 h-4 mr-2 text-green-500" />
                          Enable Validation
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Enables validation phase to verify step outputs meet success criteria
                    </p>
                  </div>

                  {/* Skip LLM Validation */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("skip_llm_validation")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "skip_llm_validation" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <Shield className="w-4 h-4 mr-2 text-orange-500" />
                          Skip LLM Validation
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Skips LLM-based validation when pre-validation (schema/pattern checks) passes - faster execution
                    </p>
                  </div>

                  {/* Enable LLM Validation */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("enable_llm_validation")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "enable_llm_validation" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <Shield className="w-4 h-4 mr-2 text-blue-500" />
                          Enable LLM Validation
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Ensures LLM validation always runs, even when pre-validation passes - more thorough checks
                    </p>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Learning Settings Section */}
            <AccordionItem value="learning" className="border border-border rounded-lg px-4 bg-muted/20">
              <AccordionTrigger className="hover:no-underline">
                <div className="flex items-center gap-2">
                  <BookOpen className="w-4 h-4 text-primary" />
                  <span className="font-semibold">Learning Settings</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="pt-4 pb-6">
                <p className="text-xs text-muted-foreground mb-4">
                  Control learning behavior. Learning agents extract insights and patterns from step execution to improve future performance.
                </p>
                <div className="grid grid-cols-1 gap-3">
                  {/* Disable Learning */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("disable_learning")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "disable_learning" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <X className="w-4 h-4 mr-2 text-red-500" />
                          Disable Learning
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Turns off learning phase - no insights will be extracted from step execution
                    </p>
                  </div>

                  {/* Enable Learning */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("enable_learning")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "enable_learning" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <CheckCircle2 className="w-4 h-4 mr-2 text-green-500" />
                          Enable Learning
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Enables learning phase to extract insights and improve future step execution
                    </p>
                  </div>

                  {/* Lock Learnings */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("lock_learnings")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "lock_learnings" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <Settings className="w-4 h-4 mr-2 text-yellow-500" />
                          Lock Learnings
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Prevents new learning from being generated but still uses existing learnings
                    </p>
                  </div>

                  {/* Unlock Learnings */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("unlock_learnings")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "unlock_learnings" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <Settings className="w-4 h-4 mr-2 text-blue-500" />
                          Unlock Learnings
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Allows learning agents to generate new insights from step execution
                    </p>
                  </div>

                  {/* Set Learning Detail Level to Exact */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_learning_detail_level_exact")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "set_learning_detail_level_exact" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <Info className="w-4 h-4 mr-2" />
                          Set Learning Detail: Exact
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Learning agents will extract precise, step-specific insights (more detailed, step-specific)
                    </p>
                  </div>

                  {/* Set Learning Detail Level to General */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_learning_detail_level_general")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "set_learning_detail_level_general" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <Info className="w-4 h-4 mr-2" />
                          Set Learning Detail: General
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Learning agents will extract high-level, reusable patterns (broader insights, less step-specific)
                    </p>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Agent Mode Section */}
            <AccordionItem value="agent-mode" className="border border-border rounded-lg px-4 bg-muted/20">
              <AccordionTrigger className="hover:no-underline">
                <div className="flex items-center gap-2">
                  <Code2 className="w-4 h-4 text-primary" />
                  <span className="font-semibold">Agent Mode</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="pt-4 pb-6">
                <p className="text-xs text-muted-foreground mb-4">
                  Choose the execution mode for agents. Code execution mode enables code generation and execution capabilities.
                </p>
                <div className="grid grid-cols-1 gap-3">
                  {/* Set Agent Mode to Code Exec */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_code_execution_mode")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "set_code_execution_mode" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <Code2 className="w-4 h-4 mr-2" />
                          Set Agent Mode: Code Execution
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Enables code generation and execution - agents can write and run code to complete tasks (auto-enables learning & validation)
                    </p>
                  </div>

                  {/* Set Agent Mode to Simple */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("set_simple_mode")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "set_simple_mode" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <Sparkles className="w-4 h-4 mr-2" />
                          Set Agent Mode: Simple
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Disables code execution - agents use only tools and natural language (faster, simpler execution)
                    </p>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>

            {/* Tools & Advanced Section */}
            <AccordionItem value="tools-advanced" className="border border-border rounded-lg px-4 bg-muted/20">
              <AccordionTrigger className="hover:no-underline">
                <div className="flex items-center gap-2">
                  <Wrench className="w-4 h-4 text-primary" />
                  <span className="font-semibold">Tools & Advanced</span>
                </div>
              </AccordionTrigger>
              <AccordionContent className="pt-4 pb-6">
                <p className="text-xs text-muted-foreground mb-4">
                  Configure tool access and execution parameters for all steps.
                </p>
                <div className="space-y-4">
                  {/* Disable Human Feedback Tools */}
                  <div className="space-y-1">
                    <Button
                      variant="outline"
                      onClick={() => handleImmediateAction("disable_human_tools")}
                      disabled={applyingAction !== null}
                      className="w-full justify-start"
                    >
                      {applyingAction === "disable_human_tools" ? (
                        <>
                          <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                          Applying...
                        </>
                      ) : (
                        <>
                          <X className="w-4 h-4 mr-2 text-red-500" />
                          Disable Human Feedback Tools
                        </>
                      )}
                    </Button>
                    <p className="text-xs text-muted-foreground ml-6">
                      Removes human feedback tools from available tools - agents cannot request human input
                    </p>
                  </div>

                  {/* Execution Max Turns Section */}
                  <div className="space-y-3 pt-2 border-t border-border">
                    <div className="flex items-center gap-2">
                      <Settings className="w-4 h-4 text-muted-foreground" />
                      <div className="text-sm font-medium text-foreground">
                        Execution Max Turns
                      </div>
                    </div>
                    <div className="flex items-center gap-3">
                      <label className="text-sm text-muted-foreground whitespace-nowrap">
                        Max Turns:
                      </label>
                      <select
                        value={selectedMaxTurns}
                        onChange={(e) => setSelectedMaxTurns(parseInt(e.target.value))}
                        disabled={applyingAction !== null}
                        className="flex-1 px-3 py-2 text-sm border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-2 focus:ring-primary focus:border-transparent disabled:opacity-50 disabled:cursor-not-allowed"
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
                        className="whitespace-nowrap"
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
                    <p className="text-xs text-muted-foreground ml-6">
                      Maximum number of conversation turns allowed for execution agents per step (prevents infinite loops)
                    </p>
                  </div>
                </div>
              </AccordionContent>
            </AccordionItem>
          </Accordion>

          <div className="mt-4 p-3 bg-blue-50 dark:bg-blue-950/20 border border-blue-200 dark:border-blue-800 rounded-md">
            <div className="flex items-start gap-2 text-xs text-blue-700 dark:text-blue-300">
              <Info className="w-4 h-4 mt-0.5 flex-shrink-0" />
              <p>
                <strong>Note:</strong> All actions are applied immediately to all steps in the workflow, including branch steps. Changes take effect right away - no need to click a separate "Apply" button.
              </p>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between p-4 border-t border-border bg-muted/30 flex-shrink-0">
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
