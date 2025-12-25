import { useState, useEffect, useCallback, useMemo } from "react";
import { X, Settings, AlertCircle, CheckCircle2, Loader2 } from "lucide-react";
import { Button } from "../ui/Button";
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

  // Handle immediate action (disable/enable validation, learning, lock learnings, LLM updates, disable human tools, set max turns)
  const handleImmediateAction = async (
    action:
      | "disable_validation"
      | "enable_validation"
      | "disable_learning"
      | "enable_learning"
      | "lock_learnings"
      | "unlock_learnings"
      | "set_execution_llm"
      | "set_validation_llm"
      | "set_learning_llm"
      | "disable_human_tools"
      | "set_execution_max_turns",
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
        <div className="flex-1 p-6 space-y-6 min-h-[400px] overflow-y-auto">
          {/* Quick Actions */}
          <div className="space-y-4">
            <div className="text-sm font-medium text-foreground mb-3">
              Quick Actions
            </div>

            <div className="grid grid-cols-2 gap-3">
              {/* Set Execution LLM from Preset */}
              <Button
                variant="outline"
                onClick={() =>
                  handleImmediateAction("set_execution_llm", presetExecutionLLM)
                }
                disabled={applyingAction !== null || !presetExecutionLLM}
                className="w-full"
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
                  `Set Execution LLM${presetExecutionLLM ? ` (${presetExecutionLLM.label})` : ""}`
                )}
              </Button>

              {/* Set Validation LLM from Preset */}
              <Button
                variant="outline"
                onClick={() =>
                  handleImmediateAction(
                    "set_validation_llm",
                    presetValidationLLM
                  )
                }
                disabled={applyingAction !== null || !presetValidationLLM}
                className="w-full"
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
                  `Set Validation LLM${presetValidationLLM ? ` (${presetValidationLLM.label})` : ""}`
                )}
              </Button>

              {/* Set Learning LLM from Preset */}
              <Button
                variant="outline"
                onClick={() =>
                  handleImmediateAction("set_learning_llm", presetLearningLLM)
                }
                disabled={applyingAction !== null || !presetLearningLLM}
                className="w-full"
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
                  `Set Learning LLM${presetLearningLLM ? ` (${presetLearningLLM.label})` : ""}`
                )}
              </Button>
              {/* Disable Validation */}
              <Button
                variant="outline"
                onClick={() => handleImmediateAction("disable_validation")}
                disabled={applyingAction !== null}
                className="w-full"
              >
                {applyingAction === "disable_validation" ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Applying...
                  </>
                ) : (
                  "Disable Validation for All Steps"
                )}
              </Button>

              {/* Enable Validation */}
              <Button
                variant="outline"
                onClick={() => handleImmediateAction("enable_validation")}
                disabled={applyingAction !== null}
                className="w-full"
              >
                {applyingAction === "enable_validation" ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Applying...
                  </>
                ) : (
                  "Enable Validation for All Steps"
                )}
              </Button>

              {/* Disable Learning */}
              <Button
                variant="outline"
                onClick={() => handleImmediateAction("disable_learning")}
                disabled={applyingAction !== null}
                className="w-full"
              >
                {applyingAction === "disable_learning" ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Applying...
                  </>
                ) : (
                  "Disable Learning for All Steps"
                )}
              </Button>

              {/* Enable Learning */}
              <Button
                variant="outline"
                onClick={() => handleImmediateAction("enable_learning")}
                disabled={applyingAction !== null}
                className="w-full"
              >
                {applyingAction === "enable_learning" ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Applying...
                  </>
                ) : (
                  "Enable Learning for All Steps"
                )}
              </Button>

              {/* Lock Learnings */}
              <Button
                variant="outline"
                onClick={() => handleImmediateAction("lock_learnings")}
                disabled={applyingAction !== null}
                className="w-full"
              >
                {applyingAction === "lock_learnings" ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Applying...
                  </>
                ) : (
                  "Lock Learnings for All Steps"
                )}
              </Button>

              {/* Unlock Learnings */}
              <Button
                variant="outline"
                onClick={() => handleImmediateAction("unlock_learnings")}
                disabled={applyingAction !== null}
                className="w-full"
              >
                {applyingAction === "unlock_learnings" ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Applying...
                  </>
                ) : (
                  "Unlock Learnings for All Steps"
                )}
              </Button>

              {/* Disable Human Feedback Tools */}
              <Button
                variant="outline"
                onClick={() => handleImmediateAction("disable_human_tools")}
                disabled={applyingAction !== null}
                className="w-full"
              >
                {applyingAction === "disable_human_tools" ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                    Applying...
                  </>
                ) : (
                  "Disable Human Feedback Tools for All Steps"
                )}
              </Button>
            </div>

            {/* Execution Max Turns Section */}
            <div className="space-y-3 pt-4 border-t border-border">
              <div className="text-sm font-medium text-foreground">
                Execution Max Turns
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
                    "Apply to All Steps"
                  )}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                Set the maximum number of turns for execution agents across all steps.
              </p>
            </div>

            <p className="text-xs text-muted-foreground mt-2">
              These actions are applied immediately to all steps. No need to
              click "Apply to All Steps".
            </p>
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
