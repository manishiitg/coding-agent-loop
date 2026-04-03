import { useState, useEffect } from "react";
import { X, Loader2, CheckCircle2, AlertCircle } from "lucide-react";
import { Button } from "../ui/Button";
import { WORKSPACE_ADVANCED_TOOLS } from "../../utils/customToolNames";
import type { AgentConfigs } from "../../utils/stepConfigMatching";
import { useWorkflowStore } from "../../stores/useWorkflowStore";
import { useWorkflowManifestStore } from "../../stores/useWorkflowManifestStore";

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
  workspacePath: string | null;
}

export default function BulkStepConfigModal({
  isOpen,
  onClose,
  workspacePath,
}: BulkStepConfigModalProps) {
  const stepOverride = useWorkflowStore(state => state.stepOverride)
  const setStepOverrideInStore = useWorkflowStore(state => state.setStepOverride)

  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [applyingAction, setApplyingAction] = useState<string | null>(null);
  const [selectedMaxTurns, setSelectedMaxTurns] = useState<number>(100);

  // Reset form when modal opens/closes
  useEffect(() => {
    if (!isOpen) {
      setSaveError(null);
      setSaveSuccess(false);
      setApplyingAction(null);
    }
    if (isOpen && stepOverride?.execution_max_turns) {
      setSelectedMaxTurns(stepOverride.execution_max_turns)
    } else if (isOpen) {
      setSelectedMaxTurns(100)
    }
  }, [isOpen, stepOverride?.execution_max_turns]);

  // Handle Escape key to close modal
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && isOpen && applyingAction === null) {
        onClose();
      }
    };
    if (isOpen) document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [isOpen, onClose, applyingAction]);

  // Save overrides to workflow.json and update store
  const saveOverrides = async (newOverrides: AgentConfigs | null) => {
    if (!workspacePath) return

    // Read current execution_defaults from manifest to preserve stateless/stateful settings
    const currentWorkflow = useWorkflowManifestStore.getState().getWorkflowByPath(workspacePath)
    const currentDefaults = currentWorkflow?.manifest?.execution_defaults

    const updatedDefaults = {
      always_use_same_run: currentDefaults?.always_use_same_run ?? false,
      skip_execution_cleanup: currentDefaults?.skip_execution_cleanup ?? false,
      execution_mode: currentDefaults?.execution_mode,
      ...(newOverrides ? {
        disable_learning: newOverrides.disable_learning ?? undefined,
        use_global_learning: newOverrides.use_global_learning ?? undefined,
        global_skill_objective: newOverrides.global_skill_objective || undefined,
        disable_parallel_tool_execution: newOverrides.disable_parallel_tool_execution ?? undefined,
        execution_max_turns: newOverrides.execution_max_turns ?? undefined,
        enabled_custom_tools: newOverrides.enabled_custom_tools,
      } : {
        disable_learning: undefined,
        use_global_learning: undefined,
        global_skill_objective: undefined,
        disable_parallel_tool_execution: undefined,
        execution_max_turns: undefined,
        enabled_custom_tools: undefined,
      }),
    }

    await useWorkflowManifestStore.getState().updateWorkflow(workspacePath, {
      execution_defaults: updatedDefaults,
    })

    setStepOverrideInStore(newOverrides)
  }

  const ov = stepOverride || {};
  const overrideStatus = {
    learningDisabled: ov.disable_learning === true,
    hasLearningOverride: ov.disable_learning !== undefined,
    globalLearningEnabled: ov.use_global_learning === true,
    hasGlobalLearningOverride: ov.use_global_learning !== undefined,
    parallelToolExecDisabled: ov.disable_parallel_tool_execution === true,
    hasParallelToolExecOverride: ov.disable_parallel_tool_execution !== undefined,
    maxTurns: ov.execution_max_turns,
    hasMaxTurnsOverride: ov.execution_max_turns !== undefined,
    enabledCustomTools: ov.enabled_custom_tools,
    hasToolAccessOverride: ov.enabled_custom_tools !== undefined && ov.enabled_custom_tools.length > 0,
  };

  const learningEnabled = overrideStatus.hasLearningOverride ? !overrideStatus.learningDisabled : true;
  const globalLearningEnabled = overrideStatus.globalLearningEnabled;
  const parallelToolExecEnabled = !overrideStatus.parallelToolExecDisabled;

  const toolAccessSummary = (() => {
    const tools = overrideStatus.enabledCustomTools;
    if (!tools || tools.length === 0) return 'All tools enabled';
    const hasHumanTools = tools.some(e => e.startsWith("human_tools"));
    const hasImage = tools.some(e => e === "workspace_advanced:read_image" || e === "workspace_advanced:*");
    const hasPdf = tools.some(e => e === "workspace_advanced:read_pdf" || e === "workspace_advanced:*");
    const parts: string[] = [];
    if (!hasImage) parts.push('no images');
    if (!hasPdf) parts.push('no PDF');
    if (!hasHumanTools) parts.push('no human tools');
    return parts.length > 0 ? parts.join(', ') : 'All tools enabled';
  })();

  const overrideCount = stepOverride ? Object.keys(stepOverride).filter(k => (stepOverride as Record<string, unknown>)[k] !== undefined).length : 0;
  const isBusy = applyingAction !== null;

  const handleAction = async (
    action:
      | "disable_learning" | "enable_learning"
      | "enable_global_learning" | "disable_global_learning"
      | "set_global_skill_objective"
      | "disable_read_image_access" | "disable_read_pdf_access" | "disable_human_tools"
      | "set_execution_max_turns"
      | "enable_parallel_tool_exec" | "disable_parallel_tool_exec",
    maxTurns?: number,
    skillObjective?: string
  ) => {
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
        case "enable_global_learning":
          newOverrides.use_global_learning = true;
          break;
        case "disable_global_learning":
          newOverrides.use_global_learning = false;
          break;
        case "set_global_skill_objective":
          newOverrides.global_skill_objective = skillObjective || '';
          break;
        case "disable_read_image_access": {
          const cur = newOverrides.enabled_custom_tools || [];
          const without = WORKSPACE_ADVANCED_TOOLS.filter(t => t !== "read_image").map(t => `workspace_advanced:${t}`);
          const hasHuman = cur.length === 0 || cur.some(e => e.startsWith("human_tools"));
          const base = cur.length === 0 || cur.includes("workspace_advanced:*")
            ? without
            : cur.filter(e => e !== "workspace_advanced:read_image" && e !== "workspace_advanced:*").concat(without.filter(t => !cur.includes(t)));
          newOverrides.enabled_custom_tools = [...base, ...(hasHuman ? ["human_tools:*"] : [])];
          break;
        }
        case "disable_read_pdf_access": {
          const cur = newOverrides.enabled_custom_tools || [];
          const without = WORKSPACE_ADVANCED_TOOLS.filter(t => t !== "read_pdf").map(t => `workspace_advanced:${t}`);
          const hasHuman = cur.length === 0 || cur.some(e => e.startsWith("human_tools"));
          const base = cur.length === 0 || cur.includes("workspace_advanced:*")
            ? without
            : cur.filter(e => e !== "workspace_advanced:read_pdf" && e !== "workspace_advanced:*").concat(without.filter(t => !cur.includes(t)));
          newOverrides.enabled_custom_tools = [...base, ...(hasHuman ? ["human_tools:*"] : [])];
          break;
        }
        case "disable_human_tools": {
          const cur = newOverrides.enabled_custom_tools || [];
          newOverrides.enabled_custom_tools = cur.length === 0
            ? ["workspace_advanced:*"]
            : cur.filter(e => !e.startsWith("human_tools"));
          break;
        }
        case "set_execution_max_turns":
          if (maxTurns !== undefined) newOverrides.execution_max_turns = maxTurns;
          break;
        case "enable_parallel_tool_exec":
          delete newOverrides.disable_parallel_tool_execution;
          break;
        case "disable_parallel_tool_exec":
          newOverrides.disable_parallel_tool_execution = true;
          break;
      }

      await saveOverrides(newOverrides);
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 2000);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : `Failed to ${action.replace(/_/g, " ")}`);
    } finally {
      setApplyingAction(null);
    }
  };

  const handleReset = async () => {
    setApplyingAction("reset");
    setSaveError(null);
    setSaveSuccess(false);
    try {
      await saveOverrides(null);
      setSelectedMaxTurns(100);
      setSaveSuccess(true);
      setTimeout(() => setSaveSuccess(false), 2000);
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : "Failed to reset overrides");
    } finally {
      setApplyingAction(null);
    }
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-4" style={{ zIndex: 50 }}>
      <div className="bg-background border border-border rounded-xl shadow-2xl w-full max-w-lg max-h-[90vh] flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-3.5 border-b border-border flex-shrink-0">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold text-foreground">Global Overrides</h2>
            {overrideCount > 0 && (
              <span className="px-2 py-0.5 text-xs font-medium rounded-full bg-primary/10 text-primary">
                {overrideCount} {overrideCount === 1 ? 'override' : 'overrides'}
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            {overrideCount > 0 && (
              <button
                onClick={handleReset}
                disabled={isBusy}
                className="text-xs font-medium text-red-600 dark:text-red-400 hover:text-red-700 dark:hover:text-red-300 disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {applyingAction === "reset" && <Loader2 className="w-3 h-3 animate-spin inline mr-1" />}
                Reset
              </button>
            )}
            <Button variant="ghost" size="sm" onClick={onClose} className="h-8 w-8 p-0 hover:bg-secondary rounded-lg" disabled={isBusy}>
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
                {overrideStatus.hasLearningOverride && (
                  <span>Learning: <span className="text-muted-foreground">{overrideStatus.learningDisabled ? 'Off' : 'On'}</span></span>
                )}
                {overrideStatus.hasParallelToolExecOverride && (
                  <span>Parallel Exec: <span className="text-muted-foreground">{overrideStatus.parallelToolExecDisabled ? 'Off' : 'On'}</span></span>
                )}
                {overrideStatus.hasMaxTurnsOverride && (
                  <span>Max Turns: <span className="text-muted-foreground">{overrideStatus.maxTurns}</span></span>
                )}
                {overrideStatus.hasToolAccessOverride && (
                  <span>Tools: <span className="text-muted-foreground">{toolAccessSummary}</span></span>
                )}
              </div>
            </div>
          )}

          <SectionHeader label="Step Overrides" />
          <div className="divide-y divide-border/50">
            <ToggleRow
              label="Learning"
              enabled={learningEnabled}
              hasOverride={overrideStatus.hasLearningOverride}
              disabled={isBusy}
              onToggle={() => handleAction(learningEnabled ? "disable_learning" : "enable_learning")}
            />
            <ToggleRow
              label="Global Learning"
              enabled={globalLearningEnabled}
              hasOverride={overrideStatus.hasGlobalLearningOverride}
              disabled={isBusy}
              onToggle={() => handleAction(globalLearningEnabled ? "disable_global_learning" : "enable_global_learning")}
            />
            {globalLearningEnabled && (
              <div className="px-3 py-2">
                <label className="text-xs font-medium text-muted-foreground mb-1 block">Skill Objective</label>
                <textarea
                  className="w-full text-xs rounded-md border border-border bg-background px-2 py-1.5 resize-none focus:outline-none focus:ring-1 focus:ring-primary"
                  rows={3}
                  placeholder="What should this global skill capture? e.g., 'Understand this website's structure, auth flows, selectors, and common failure modes so any step can interact with it reliably'"
                  defaultValue={ov.global_skill_objective || ''}
                  disabled={isBusy}
                  onBlur={(e) => {
                    const val = e.target.value.trim()
                    if (val !== (ov.global_skill_objective || '')) {
                      handleAction("set_global_skill_objective", undefined, val)
                    }
                  }}
                />
              </div>
            )}
            <ToggleRow
              label="Parallel Tool Execution"
              enabled={parallelToolExecEnabled}
              hasOverride={overrideStatus.hasParallelToolExecOverride}
              disabled={isBusy}
              onToggle={() => handleAction(parallelToolExecEnabled ? "disable_parallel_tool_exec" : "enable_parallel_tool_exec")}
            />
          </div>

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
                onClick={() => handleAction(tool.action)}
                disabled={isBusy}
                className="px-3 py-1.5 text-xs rounded-full border border-border hover:bg-muted transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {applyingAction === tool.action && <Loader2 className="w-3 h-3 animate-spin inline mr-1" />}
                {tool.label}
              </button>
            ))}
          </div>

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
                <option key={value} value={value}>{value}</option>
              ))}
            </select>
            <Button
              size="sm"
              onClick={() => handleAction("set_execution_max_turns", selectedMaxTurns)}
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
                <CheckCircle2 className="w-3 h-3" /> Saved to workflow.json
              </span>
            ) : saveError ? (
              <span className="flex items-center gap-1 text-red-600 dark:text-red-400">
                <AlertCircle className="w-3 h-3" /> {saveError}
              </span>
            ) : (
              'Changes apply to all steps'
            )}
          </span>
          <Button variant="outline" size="sm" onClick={onClose} disabled={isBusy}>Close</Button>
        </div>
      </div>
    </div>
  );
}
