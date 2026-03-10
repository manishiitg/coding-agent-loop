import { useState, useEffect, useCallback, useMemo } from "react";
import { X, Loader2, CheckCircle2, AlertCircle } from "lucide-react";
import { Button } from "../ui/Button";
import { useLLMStore, useGlobalPresetStore } from "../../stores";
import { useCapabilitiesStore } from "../../stores/useCapabilitiesStore";
import {
  WORKSPACE_ADVANCED_TOOLS,
} from "../../utils/customToolNames";
import type {
  AgentLLMConfig,
  AgentConfigs,
} from "../../utils/stepConfigMatching";
import type { LLMOption } from "../../types/llm";
import type {
  PlanningResponse,
  PlanStep,
} from "../../utils/stepConfigMatching";
import { isConditionalStep, isDecisionStep, isOrchestrationStep, isTodoTaskStep } from "../../utils/stepConfigMatching";
import LLMSelectionDropdown from "../LLMSelectionDropdown";

// --- Reusable sub-components ---

function SectionHeader({ label }: { label: string }) {
  return (
    <div className="text-xs font-medium text-muted-foreground uppercase tracking-wider mb-3 mt-6 first:mt-0">
      {label}
    </div>
  );
}

function ToggleRow({ label, enabled, hasOverride, disabled, onToggle }: {
  label: string;
  enabled: boolean;
  hasOverride: boolean;
  disabled: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="flex items-center justify-between py-2.5">
      <div className="flex items-center gap-2">
        {hasOverride && <span className="w-1.5 h-1.5 rounded-full bg-primary shrink-0" />}
        <span className="text-sm">{label}</span>
      </div>
      <button
        onClick={onToggle}
        disabled={disabled}
        className={`relative inline-flex h-5 w-9 items-center rounded-full transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
          enabled ? 'bg-primary' : 'bg-muted-foreground/30'
        }`}
      >
        <span className={`inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform ${
          enabled ? 'translate-x-[18px]' : 'translate-x-[3px]'
        }`} />
      </button>
    </div>
  );
}

// --- Main component ---

interface BulkStepConfigModalProps {
  isOpen: boolean;
  onClose: () => void;
  plan: PlanningResponse | null;
  stepOverride: AgentConfigs | null;
  onSaveStepOverride: (agentConfigs: AgentConfigs | null) => Promise<void>;
}

export default function BulkStepConfigModal({
  isOpen,
  onClose,
  plan,
  stepOverride,
  onSaveStepOverride,
}: BulkStepConfigModalProps) {
  const { availableLLMs, refreshAvailableLLMs } = useLLMStore();
  const { capabilities } = useCapabilitiesStore();
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
  const enableContextEditing = presetLLMConfig?.enable_context_editing === true;

  // Function to update feature toggles in the preset
  const handleToggleFeature = useCallback(async (
    feature: 'use_knowledgebase' | 'enable_context_summarization' | 'enable_context_editing',
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
        activePreset.selectedSkills,
        activePreset.agentMode,
        activePreset.selectedFolder,
        updatedLLMConfig,
        activePreset.useCodeExecutionMode,
        feature === 'enable_context_summarization' ? enabled : activePreset.enableContextSummarization,
        undefined, // useToolSearchMode
        undefined, // enableBrowserAccess
        feature === 'enable_context_editing' ? enabled : activePreset.enableContextEditing
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

  // Local state for selected LLMs
  const [selectedExecutionLLM, setSelectedExecutionLLM] = useState<LLMOption | null>(null);
  const [selectedLearningLLM, setSelectedLearningLLM] = useState<LLMOption | null>(null);

  // Initialize selections from preset defaults
  useEffect(() => {
    if (presetExecutionLLM) {
      setSelectedExecutionLLM(presetExecutionLLM);
    }
    if (presetLearningLLM) {
      setSelectedLearningLLM(presetLearningLLM);
    }
  }, [presetExecutionLLM, presetLearningLLM, isOpen]);

  // Reset form when modal opens/closes
  useEffect(() => {
    if (!isOpen) {
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

        if (isConditionalStep(step)) {
          if (step.if_true_steps && step.if_true_steps.length > 0) {
            collectSteps(step.if_true_steps, `${stepPath}.if_true_steps`);
          }
          if (step.if_false_steps && step.if_false_steps.length > 0) {
            collectSteps(step.if_false_steps, `${stepPath}.if_false_steps`);
          }
        }

        if (isDecisionStep(step) && step.decision_step) {
          allSteps.push({
            stepId: step.decision_step.id,
            step: step.decision_step,
            path: `${stepPath}.decision_step`,
          });
        }

        if (isOrchestrationStep(step)) {
          if (step.orchestration_step) {
            allSteps.push({
              stepId: step.orchestration_step.id,
              step: step.orchestration_step,
              path: `${stepPath}.orchestration_step`,
            });
          }

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

        if (isTodoTaskStep(step)) {
          if (step.todo_task_step) {
            allSteps.push({
              stepId: step.todo_task_step.id,
              step: step.todo_task_step,
              path: `${stepPath}.todo_task_step`,
            });
          }

          if (step.predefined_routes && step.predefined_routes.length > 0) {
            step.predefined_routes.forEach((route, routeIndex) => {
              if (route.sub_agent_step) {
                allSteps.push({
                  stepId: route.sub_agent_step.id,
                  step: route.sub_agent_step,
                  path: `${stepPath}.predefined_routes[${routeIndex}].sub_agent_step`,
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

  // Derive current override status from stepOverride
  const overrideStatus = useMemo(() => {
    const ov = stepOverride || {};
    return {
      learningDisabled: ov.disable_learning === true,
      learningEnabled: ov.disable_learning === false,
      hasLearningOverride: ov.disable_learning !== undefined,
      executionLLM: ov.execution_llm,
      hasExecutionLLMOverride: ov.execution_llm !== undefined,
      learningLLM: ov.learning_llm,
      hasLearningLLMOverride: ov.learning_llm !== undefined,
      codeExecutionMode: ov.use_code_execution_mode === true,
      toolSearchMode: ov.use_tool_search_mode === true,
      simpleMode: ov.use_code_execution_mode === false && ov.use_tool_search_mode === false,
      hasAgentModeOverride: ov.use_code_execution_mode !== undefined || ov.use_tool_search_mode !== undefined,
      contextOffloading: ov.enable_context_offloading,
      hasContextOffloadingOverride: ov.enable_context_offloading !== undefined,
      parallelToolExecDisabled: ov.disable_parallel_tool_execution === true,
      hasParallelToolExecOverride: ov.disable_parallel_tool_execution !== undefined,
      maxTurns: ov.execution_max_turns,
      hasMaxTurnsOverride: ov.execution_max_turns !== undefined,
      enabledCustomTools: ov.enabled_custom_tools,
      hasToolAccessOverride: ov.enabled_custom_tools !== undefined && ov.enabled_custom_tools.length > 0,
      disableTierOptimization: ov.disable_tier_optimization === true,
      hasDisableTierOptimizationOverride: ov.disable_tier_optimization !== undefined,
    };
  }, [stepOverride]);

  // Check context offloading state - prefer override, fallback to per-step check
  const contextOffloadingState = useMemo(() => {
    if (overrideStatus.hasContextOffloadingOverride) {
      return { enabled: overrideStatus.contextOffloading === true };
    }
    if (!plan) return { enabled: true };
    const allSteps = getAllSteps();
    const disabledCount = allSteps.filter(({ step }) =>
      step.agent_configs?.enable_context_offloading === false
    ).length;
    return { enabled: allSteps.length === 0 || disabledCount === 0 };
  }, [plan, getAllSteps, overrideStatus]);

  // Check parallel tool execution state - prefer override, fallback to true (backend default)
  const parallelToolExecState = useMemo(() => {
    if (overrideStatus.hasParallelToolExecOverride) {
      return { enabled: !overrideStatus.parallelToolExecDisabled };
    }
    return { enabled: true };
  }, [overrideStatus]);

  // Handle immediate action via step_override.json
  const handleImmediateAction = async (
    action:
      | "disable_learning"
      | "enable_learning"
      | "set_execution_llm"
      | "set_learning_llm"
      | "enable_only_advanced_tools"
      | "disable_read_image_access"
      | "disable_read_pdf_access"
      | "disable_human_tools"
      | "set_execution_max_turns"
      | "set_code_execution_mode"
      | "set_simple_mode"
      | "set_tool_search_mode"
      | "enable_context_offloading"
      | "disable_context_offloading"
      | "enable_parallel_tool_exec"
      | "disable_parallel_tool_exec"
      | "enable_disable_tier_optimization"
      | "disable_disable_tier_optimization",
    llm?: LLMOption | null,
    maxTurns?: number
  ) => {
    if (!plan) return;

    setApplyingAction(action);
    setSaveError(null);
    setSaveSuccess(false);

    try {
      const newOverrides: AgentConfigs = { ...(stepOverride || {}) };

      switch (action) {
        case "disable_learning":
          newOverrides.disable_learning = true;
          break;
        case "enable_learning":
          newOverrides.disable_learning = false;
          break;
        case "set_execution_llm":
          if (llm) {
            newOverrides.execution_llm = {
              provider: llm.provider as AgentLLMConfig["provider"],
              model_id: llm.model,
            };
          }
          break;
        case "set_learning_llm":
          if (llm) {
            newOverrides.learning_llm = {
              provider: llm.provider as AgentLLMConfig["provider"],
              model_id: llm.model,
            };
          }
          break;
        case "disable_read_image_access": {
          const currentEnabledTools = newOverrides.enabled_custom_tools || [];
          // Expand workspace_advanced:* → specific tools minus read_image
          const advancedWithoutImage = WORKSPACE_ADVANCED_TOOLS
            .filter(t => t !== "read_image")
            .map(t => `workspace_advanced:${t}`);
          const hasHumanTools = currentEnabledTools.length === 0 ||
            currentEnabledTools.some(e => e.startsWith("human_tools"));
          const baseTools = currentEnabledTools.length === 0 || currentEnabledTools.includes("workspace_advanced:*")
            ? advancedWithoutImage
            : currentEnabledTools.filter(e => e !== "workspace_advanced:read_image" && e !== "workspace_advanced:*")
                .concat(advancedWithoutImage.filter(t => !currentEnabledTools.includes(t)));
          newOverrides.enabled_custom_tools = [
            ...baseTools,
            ...(hasHumanTools ? ["human_tools:*"] : []),
          ];
          break;
        }
        case "disable_read_pdf_access": {
          const currentEnabledTools = newOverrides.enabled_custom_tools || [];
          const advancedWithoutPdf = WORKSPACE_ADVANCED_TOOLS
            .filter(t => t !== "read_pdf")
            .map(t => `workspace_advanced:${t}`);
          const hasHumanTools = currentEnabledTools.length === 0 ||
            currentEnabledTools.some(e => e.startsWith("human_tools"));
          const baseTools = currentEnabledTools.length === 0 || currentEnabledTools.includes("workspace_advanced:*")
            ? advancedWithoutPdf
            : currentEnabledTools.filter(e => e !== "workspace_advanced:read_pdf" && e !== "workspace_advanced:*")
                .concat(advancedWithoutPdf.filter(t => !currentEnabledTools.includes(t)));
          newOverrides.enabled_custom_tools = [
            ...baseTools,
            ...(hasHumanTools ? ["human_tools:*"] : []),
          ];
          break;
        }
        case "disable_human_tools": {
          const currentEnabledTools = newOverrides.enabled_custom_tools || [];
          if (currentEnabledTools.length === 0) {
            // Default is workspace_advanced:* + human_tools:*, just drop human_tools
            newOverrides.enabled_custom_tools = ["workspace_advanced:*"];
          } else {
            newOverrides.enabled_custom_tools = currentEnabledTools.filter(
              entry => !entry.startsWith("human_tools")
            );
          }
          break;
        }
        case "set_execution_max_turns":
          if (maxTurns !== undefined) {
            newOverrides.execution_max_turns = maxTurns;
          }
          break;
        case "set_code_execution_mode":
          newOverrides.use_code_execution_mode = true;
          newOverrides.use_tool_search_mode = false;
          newOverrides.disable_learning = false;
          newOverrides.learning_detail_level = "exact";
          break;
        case "set_tool_search_mode":
          newOverrides.use_tool_search_mode = true;
          newOverrides.use_code_execution_mode = false;
          break;
        case "set_simple_mode":
          newOverrides.use_code_execution_mode = false;
          newOverrides.use_tool_search_mode = false;
          break;
        case "enable_context_offloading":
          newOverrides.enable_context_offloading = true;
          break;
        case "disable_context_offloading":
          newOverrides.enable_context_offloading = false;
          break;
        case "enable_parallel_tool_exec":
          delete newOverrides.disable_parallel_tool_execution;
          break;
        case "disable_parallel_tool_exec":
          newOverrides.disable_parallel_tool_execution = true;
          break;
        case "enable_disable_tier_optimization":
          newOverrides.disable_tier_optimization = true;
          break;
        case "disable_disable_tier_optimization":
          delete newOverrides.disable_tier_optimization;
          break;
      }

      await onSaveStepOverride(newOverrides);

      setSaveSuccess(true);
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

  // Handle resetting all overrides
  const handleResetOverrides = async () => {
    setApplyingAction("reset");
    setSaveError(null);
    setSaveSuccess(false);

    try {
      await onSaveStepOverride(null);

      // Reset local LLM selections back to preset defaults
      setSelectedExecutionLLM(presetExecutionLLM);
      setSelectedLearningLLM(presetLearningLLM);
      setSelectedMaxTurns(100);

      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 2000);
    } catch (error) {
      console.error("[BulkStepConfigModal] Error resetting overrides:", error);
      setSaveError(
        error instanceof Error ? error.message : "Failed to reset overrides"
      );
    } finally {
      setApplyingAction(null);
    }
  };

  const overrideCount = stepOverride ? Object.keys(stepOverride).length : 0;
  const isBusy = applyingAction !== null;

  // Derived toggle states
  const learningEnabled = overrideStatus.hasLearningOverride ? !overrideStatus.learningDisabled : true;

  // Active execution mode
  const activeExecutionMode = useMemo(() => {
    if (overrideStatus.hasAgentModeOverride) {
      if (overrideStatus.codeExecutionMode) return 'code_exec';
      if (overrideStatus.toolSearchMode) return 'tool_search';
      return 'simple';
    }
    if (activePreset?.useCodeExecutionMode) return 'code_exec';
    return 'simple';
  }, [overrideStatus, activePreset]);

  const isTieredMode = presetLLMConfig?.llm_allocation_mode === 'tiered';

  // Compute a readable summary of the current tool access state
  const toolAccessSummary = useMemo(() => {
    const tools = overrideStatus.enabledCustomTools;
    if (!tools || tools.length === 0) return 'All tools enabled';

    const wsTools = tools.filter(t => t.startsWith('workspace_tools:')).map(t => t.split(':')[1]);
    const humanTools = tools.filter(t => t.startsWith('human_tools:'));

    const parts: string[] = [];
    parts.push(`${wsTools.length} workspace tools`);

    if (humanTools.length === 0) parts.push('no human tools');

    if (!wsTools.includes('read_image')) {
      parts.push('no images');
    }

    return parts.join(', ');
  }, [overrideStatus.enabledCustomTools]);

  if (!isOpen) return null;

  return (
    <div
      className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4"
      style={{ zIndex: 50 }}
    >
      <div className="bg-background border border-border rounded-xl shadow-2xl w-full max-w-2xl max-h-[90vh] flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold text-foreground">
              Global Overrides
            </h2>
            {overrideCount > 0 && (
              <span className="px-2 py-0.5 text-xs font-medium rounded-full bg-primary/10 text-primary">
                {overrideCount} {overrideCount === 1 ? 'override' : 'overrides'}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            {overrideCount > 0 && (
              <button
                onClick={handleResetOverrides}
                disabled={isBusy}
                className="text-xs font-medium text-red-600 dark:text-red-400 hover:text-red-700 dark:hover:text-red-300 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {applyingAction === "reset" ? (
                  <Loader2 className="w-3 h-3 animate-spin inline mr-1" />
                ) : null}
                Reset
              </button>
            )}
            <Button
              variant="ghost"
              size="sm"
              onClick={onClose}
              className="h-8 w-8 p-0 hover:bg-secondary rounded-lg"
              disabled={isBusy}
            >
              <X className="w-4 h-4" />
            </Button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 px-5 py-4 overflow-y-auto">

          {/* Active overrides summary */}
          {overrideCount > 0 && (
            <div className="mb-4 px-3 py-2.5 rounded-lg bg-muted/50 border border-border/50">
              <div className="text-xs font-medium text-muted-foreground mb-1.5">Active overrides</div>
              <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-foreground">
                {overrideStatus.hasExecutionLLMOverride && (
                  <span>Execution LLM: <span className="text-muted-foreground">{overrideStatus.executionLLM?.model_id}</span></span>
                )}
                {overrideStatus.hasLearningLLMOverride && (
                  <span>Learning LLM: <span className="text-muted-foreground">{overrideStatus.learningLLM?.model_id}</span></span>
                )}
                {overrideStatus.hasAgentModeOverride && (
                  <span>Mode: <span className="text-muted-foreground">{overrideStatus.codeExecutionMode ? 'Code Exec' : overrideStatus.toolSearchMode ? 'Tool Search' : 'Simple'}</span></span>
                )}
                {overrideStatus.hasLearningOverride && (
                  <span>Learning: <span className="text-muted-foreground">{overrideStatus.learningDisabled ? 'Off' : 'On'}</span></span>
                )}
                {overrideStatus.hasContextOffloadingOverride && (
                  <span>Offloading: <span className="text-muted-foreground">{overrideStatus.contextOffloading ? 'On' : 'Off'}</span></span>
                )}
                {overrideStatus.hasParallelToolExecOverride && (
                  <span>Parallel Exec: <span className="text-muted-foreground">{overrideStatus.parallelToolExecDisabled ? 'Off' : 'On'}</span></span>
                )}
                {overrideStatus.hasMaxTurnsOverride && (
                  <span>Max Turns: <span className="text-muted-foreground">{overrideStatus.maxTurns}</span></span>
                )}
                {overrideStatus.hasToolAccessOverride && (
                  <span>Tools: <span className="text-muted-foreground">{overrideStatus.enabledCustomTools?.length} entries</span></span>
                )}
              </div>
            </div>
          )}

          {/* ── Models ── */}
          {!isTieredMode && (
            <>
              <SectionHeader label="Models" />

              <div className="space-y-3">
                {/* Execution LLM */}
                <div>
                    <div className="flex items-center gap-3">
                      <div className="flex items-center gap-1.5 w-28 shrink-0">
                        {overrideStatus.hasExecutionLLMOverride && <span className="w-1.5 h-1.5 rounded-full bg-primary shrink-0" />}
                        <span className="text-sm font-medium">Execution LLM</span>
                      </div>
                      <div className="flex-1">
                        <LLMSelectionDropdown
                          availableLLMs={availableLLMs}
                          selectedLLM={selectedExecutionLLM}
                          onLLMSelect={setSelectedExecutionLLM}
                          onRefresh={refreshAvailableLLMs}
                          inModal={true}
                          openDirection="down"
                          title="Select Execution LLM"
                        />
                      </div>
                      <Button
                        size="sm"
                        onClick={() => handleImmediateAction("set_execution_llm", selectedExecutionLLM)}
                        disabled={!selectedExecutionLLM || isBusy}
                        className="px-3 h-8"
                      >
                        {applyingAction === "set_execution_llm" ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : "Apply"}
                      </Button>
                    </div>
                    <div className="text-[10px] text-muted-foreground mt-1 ml-[calc(7rem+0.375rem)]">
                      {overrideStatus.hasExecutionLLMOverride
                        ? <span className="text-primary">Override: {overrideStatus.executionLLM?.provider}/{overrideStatus.executionLLM?.model_id}</span>
                        : presetExecutionLLM
                          ? `Preset: ${presetExecutionLLM.provider}/${presetExecutionLLM.model}`
                          : 'Using preset default'
                      }
                    </div>
                  </div>

                  {/* Learning LLM */}
                  <div>
                    <div className="flex items-center gap-3">
                      <div className="flex items-center gap-1.5 w-28 shrink-0">
                        {overrideStatus.hasLearningLLMOverride && <span className="w-1.5 h-1.5 rounded-full bg-primary shrink-0" />}
                        <span className="text-sm font-medium">Learning LLM</span>
                      </div>
                      <div className="flex-1">
                        <LLMSelectionDropdown
                          availableLLMs={availableLLMs}
                          selectedLLM={selectedLearningLLM}
                          onLLMSelect={setSelectedLearningLLM}
                          onRefresh={refreshAvailableLLMs}
                          inModal={true}
                          openDirection="down"
                          title="Select Learning LLM"
                        />
                      </div>
                      <Button
                        size="sm"
                        onClick={() => handleImmediateAction("set_learning_llm", selectedLearningLLM)}
                        disabled={!selectedLearningLLM || isBusy}
                        className="px-3 h-8"
                      >
                        {applyingAction === "set_learning_llm" ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : "Apply"}
                      </Button>
                    </div>
                    <div className="text-[10px] text-muted-foreground mt-1 ml-[calc(7rem+0.375rem)]">
                      {overrideStatus.hasLearningLLMOverride
                        ? <span className="text-primary">Override: {overrideStatus.learningLLM?.provider}/{overrideStatus.learningLLM?.model_id}</span>
                        : presetLearningLLM
                          ? `Preset: ${presetLearningLLM.provider}/${presetLearningLLM.model}`
                          : 'Using preset default'
                      }
                    </div>
                  </div>
              </div>
            </>
          )}

          {/* ── Execution Mode ── */}
          {!isTieredMode && (
            <>
              <SectionHeader label="Execution Mode" />

              <div className="flex gap-2">
                {([
                  { key: 'code_exec', label: 'Code Execution', action: 'set_code_execution_mode' as const },
                  { key: 'tool_search', label: 'Tool Search', action: 'set_tool_search_mode' as const },
                  { key: 'simple', label: 'Simple', action: 'set_simple_mode' as const },
                ]).map(mode => (
                  <button
                    key={mode.key}
                    onClick={() => handleImmediateAction(mode.action)}
                    disabled={isBusy}
                    className={`flex-1 py-2 px-3 text-sm rounded-lg border transition-colors disabled:opacity-50 disabled:cursor-not-allowed ${
                      activeExecutionMode === mode.key
                        ? 'bg-primary text-primary-foreground border-primary'
                        : 'bg-background border-border hover:bg-muted'
                    }`}
                  >
                    {applyingAction === mode.action ? (
                      <Loader2 className="w-3.5 h-3.5 animate-spin mx-auto" />
                    ) : (
                      mode.label
                    )}
                  </button>
                ))}
              </div>
            </>
          )}

          {/* ── Step Overrides ── */}
          <SectionHeader label="Step Overrides" />

          <div className="divide-y divide-border/50">
            <ToggleRow
              label="Learning"
              enabled={learningEnabled}
              hasOverride={overrideStatus.hasLearningOverride}
              disabled={isBusy}
              onToggle={() => handleImmediateAction(learningEnabled ? "disable_learning" : "enable_learning")}
            />
            <ToggleRow
              label="Context Offloading"
              enabled={contextOffloadingState.enabled}
              hasOverride={overrideStatus.hasContextOffloadingOverride}
              disabled={isBusy}
              onToggle={() => handleImmediateAction(
                contextOffloadingState.enabled ? "disable_context_offloading" : "enable_context_offloading"
              )}
            />
            <ToggleRow
              label="Parallel Tool Execution"
              enabled={parallelToolExecState.enabled}
              hasOverride={overrideStatus.hasParallelToolExecOverride}
              disabled={isBusy}
              onToggle={() => handleImmediateAction(
                parallelToolExecState.enabled ? "disable_parallel_tool_exec" : "enable_parallel_tool_exec"
              )}
            />
            {isTieredMode && (
              <ToggleRow
                label="Don't Optimize Tier For Learnings"
                enabled={overrideStatus.disableTierOptimization}
                hasOverride={overrideStatus.hasDisableTierOptimizationOverride}
                disabled={isBusy}
                onToggle={() => handleImmediateAction(
                  overrideStatus.disableTierOptimization ? "disable_disable_tier_optimization" : "enable_disable_tier_optimization"
                )}
              />
            )}
          </div>

          {/* ── Preset Settings ── */}
          <SectionHeader label="Preset Settings" />

          <div className="divide-y divide-border/50">
            <ToggleRow
              label="Knowledgebase"
              enabled={enableKnowledgebase}
              hasOverride={false}
              disabled={isBusy || !activePreset}
              onToggle={() => handleToggleFeature('use_knowledgebase', !enableKnowledgebase)}
            />
            <ToggleRow
              label="Context Summarization"
              enabled={enableContextSummarization}
              hasOverride={false}
              disabled={isBusy || !activePreset}
              onToggle={() => handleToggleFeature('enable_context_summarization', !enableContextSummarization)}
            />
          </div>

          {/* ── Tools ── */}
          <SectionHeader label="Tools" />

          <div className="text-xs text-muted-foreground mb-2">
            {overrideStatus.hasToolAccessOverride && <span className="w-1.5 h-1.5 rounded-full bg-primary inline-block mr-1.5 align-middle" />}
            {toolAccessSummary}
          </div>

          <div className="flex flex-wrap gap-2">
            {([
              { label: 'No Read Image', action: 'disable_read_image_access' as const },
              { label: 'No PDF', action: 'disable_read_pdf_access' as const },
              { label: 'No Human Tools', action: 'disable_human_tools' as const },
            ]).map(tool => (
              <button
                key={tool.action}
                onClick={() => handleImmediateAction(tool.action)}
                disabled={isBusy}
                className="px-3 py-1.5 text-xs rounded-full border border-border hover:bg-muted transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {applyingAction === tool.action ? (
                  <Loader2 className="w-3 h-3 animate-spin inline mr-1" />
                ) : null}
                {tool.label}
              </button>
            ))}
          </div>

          {/* ── Max Turns ── */}
          <SectionHeader label="Max Turns" />

          <div className="flex items-center gap-3">
            <div className="flex items-center gap-1.5">
              {overrideStatus.hasMaxTurnsOverride && <span className="w-1.5 h-1.5 rounded-full bg-primary shrink-0" />}
              <span className="text-sm font-medium">Max turns per step</span>
            </div>
            <select
              value={selectedMaxTurns}
              onChange={(e) => setSelectedMaxTurns(parseInt(e.target.value))}
              disabled={isBusy}
              className="px-2 py-1.5 text-sm border border-border rounded-md bg-background text-foreground focus:outline-none focus:ring-1 focus:ring-primary disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {[10, 25, 50, 75, 100, 200, 500].map((value) => (
                <option key={value} value={value}>
                  {value}
                </option>
              ))}
            </select>
            <Button
              size="sm"
              onClick={() => handleImmediateAction("set_execution_max_turns", null, selectedMaxTurns)}
              disabled={isBusy}
              className="px-3 h-8"
            >
              {applyingAction === "set_execution_max_turns" ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : "Apply"}
            </Button>
          </div>
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between px-5 py-3 border-t border-border flex-shrink-0">
          <span className="text-xs text-muted-foreground">
            {saveSuccess ? (
              <span className="flex items-center gap-1 text-green-600 dark:text-green-400">
                <CheckCircle2 className="w-3 h-3" /> Saved
              </span>
            ) : saveError ? (
              <span className="flex items-center gap-1 text-red-600 dark:text-red-400">
                <AlertCircle className="w-3 h-3" /> {saveError}
              </span>
            ) : (
              'Changes apply to all steps'
            )}
          </span>
          <Button variant="outline" size="sm" onClick={onClose} disabled={isBusy}>
            Close
          </Button>
        </div>
      </div>
    </div>
  );
}
