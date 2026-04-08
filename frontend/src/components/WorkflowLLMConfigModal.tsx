import { useState, useCallback } from 'react'
import { X, Brain, Zap, Gauge, Cpu, Loader2, RefreshCw } from 'lucide-react'
import { Button } from './ui/Button'
import { useLLMStore } from '../stores'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useWorkflowManifestStore } from '../stores/useWorkflowManifestStore'
import LLMSelectionDropdown from './LLMSelectionDropdown'
import type { AgentLLMConfig, AgentLLMFallback, PresetLLMConfig } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import type { CustomPreset } from '../types/preset'

interface WorkflowLLMConfigModalProps {
  isOpen: boolean
  onClose: () => void
}

// Inner component mounts fresh each open, so state always reflects the current preset
function WorkflowLLMConfigModalContent({ onClose }: { onClose: () => void }) {
  const { availableLLMs, loadDefaultsFromBackend, isLoadingLLMs } = useLLMStore()
  const { getActivePreset, savePreset } = useGlobalPresetStore()
  const [isSaving, setIsSaving] = useState(false)
  const [isRefreshing, setIsRefreshing] = useState(false)

  const activePreset = getActivePreset('workflow') as CustomPreset | null
  const existing = activePreset?.llmConfig

  // Read from workflow manifest (source of truth) with preset as fallback
  const manifestLLM = (() => {
    const workspacePath = activePreset?.selectedFolder?.filepath
    if (!workspacePath) return null
    return useWorkflowManifestStore.getState().getWorkflowByPath(workspacePath)?.manifest?.capabilities?.llm_config ?? null
  })()

  const [tier1, setTier1] = useState<AgentLLMConfig | null>(manifestLLM?.tiered_config?.tier_1 ?? existing?.tiered_config?.tier_1 ?? null)
  const [tier2, setTier2] = useState<AgentLLMConfig | null>(manifestLLM?.tiered_config?.tier_2 ?? existing?.tiered_config?.tier_2 ?? null)
  const [tier3, setTier3] = useState<AgentLLMConfig | null>(manifestLLM?.tiered_config?.tier_3 ?? existing?.tiered_config?.tier_3 ?? null)
  const [phaseLLM, setPhaseLLM] = useState<AgentLLMConfig | null>(manifestLLM?.phase_llm ?? existing?.phase_llm ?? null)

  if (!activePreset) return null

  const findOption = (cfg: AgentLLMConfig | null): LLMOption | null => {
    if (!cfg) return null
    return availableLLMs.find(o => o.provider === cfg.provider && o.model === cfg.model_id) ?? null
  }

  const getFallbackOptions = (primary: AgentLLMConfig | null): LLMOption[] => {
    if (!primary) return availableLLMs
    const excluded = new Set([
      `${primary.provider}/${primary.model_id}`,
      ...(primary.fallbacks ?? []).map(f => `${f.provider}/${f.model_id}`),
    ])
    return availableLLMs.filter(o => !excluded.has(`${o.provider}/${o.model}`))
  }

  const toAgentLLM = (opt: LLMOption, fallbacks?: AgentLLMFallback[]): AgentLLMConfig => ({
    provider: opt.provider as AgentLLMConfig['provider'],
    model_id: opt.model,
    ...(fallbacks && fallbacks.length > 0 ? { fallbacks } : {}),
  })

  const addFallback = (setter: React.Dispatch<React.SetStateAction<AgentLLMConfig | null>>, opt: LLMOption) => {
    setter(prev => {
      if (!prev) return prev
      const fb: AgentLLMFallback = { provider: opt.provider, model_id: opt.model }
      return { ...prev, fallbacks: [...(prev.fallbacks ?? []), fb] }
    })
  }

  const removeFallback = (setter: React.Dispatch<React.SetStateAction<AgentLLMConfig | null>>, idx: number) => {
    setter(prev => {
      if (!prev) return prev
      const next = (prev.fallbacks ?? []).filter((_, i) => i !== idx)
      return { ...prev, fallbacks: next.length > 0 ? next : undefined }
    })
  }

  const handleRefresh = useCallback(async () => {
    setIsRefreshing(true)
    try {
      // Refresh both available LLMs and workflow manifest
      await Promise.all([
        loadDefaultsFromBackend(),
        useWorkflowManifestStore.getState().refreshWorkflows(),
      ])
      // Re-read manifest and update form state
      const workspacePath = activePreset?.selectedFolder?.filepath
      if (workspacePath) {
        const refreshed = useWorkflowManifestStore.getState().getWorkflowByPath(workspacePath)?.manifest?.capabilities?.llm_config
        if (refreshed) {
          if (refreshed.tiered_config?.tier_1) setTier1(refreshed.tiered_config.tier_1)
          if (refreshed.tiered_config?.tier_2) setTier2(refreshed.tiered_config.tier_2)
          if (refreshed.tiered_config?.tier_3) setTier3(refreshed.tiered_config.tier_3)
          if (refreshed.phase_llm) setPhaseLLM(refreshed.phase_llm)
        }
      }
    } finally {
      setIsRefreshing(false)
    }
  }, [activePreset?.selectedFolder?.filepath, loadDefaultsFromBackend])

  const handleSave = async () => {
    setIsSaving(true)
    try {
      const tieredConfig = tier1 && tier2 && tier3
        ? { tier_1: tier1, tier_2: tier2, tier_3: tier3 }
        : undefined

      const newLLMConfig: PresetLLMConfig = {
        ...(existing ?? {}),
        phase_llm: phaseLLM ?? undefined,
        llm_allocation_mode: tieredConfig ? 'tiered' : 'manual',
        ...(tieredConfig ? { tiered_config: tieredConfig } : {}),
      }

      await savePreset(
        activePreset.label,
        activePreset.query,
        activePreset.selectedServers,
        activePreset.selectedTools,
        activePreset.selectedSkills,
        'workflow',
        activePreset.selectedFolder,
        newLLMConfig,
        activePreset.useCodeExecutionMode,
        activePreset.id, // existing preset id
        activePreset.enableContextSummarization,
        activePreset.enableBrowserAccess,
        activePreset.enableContextEditing,
        activePreset.selectedSecrets,
      )
      onClose()
    } catch (err) {
      console.error('[WorkflowLLMConfig] Save failed:', err)
    } finally {
      setIsSaving(false)
    }
  }

  const TIERS: Array<{
    label: string
    desc: string
    icon: typeof Brain
    color: string
    value: AgentLLMConfig | null
    setter: React.Dispatch<React.SetStateAction<AgentLLMConfig | null>>
  }> = [
    { label: 'Tier 1 — High Reasoning', desc: 'First-time execution & initial learning extraction', icon: Brain, color: 'text-purple-500', value: tier1, setter: setTier1 },
    { label: 'Tier 2 — Medium Reasoning', desc: 'Execution with learnings & learning refinement', icon: Gauge, color: 'text-blue-500', value: tier2, setter: setTier2 },
    { label: 'Tier 3 — Low Reasoning', desc: 'Validation & mature learning refinement (2+ runs)', icon: Zap, color: 'text-green-500', value: tier3, setter: setTier3 },
  ]

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        className="bg-white dark:bg-slate-800 rounded-lg shadow-xl w-full max-w-4xl mx-4 max-h-[90vh] flex flex-col"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-slate-700 shrink-0">
          <div>
            <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Workflow LLM Configuration</h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">
              <span className="font-medium text-purple-500">{activePreset.label}</span> — tiered models &amp; fallbacks
            </p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleRefresh}
              disabled={isRefreshing || isLoadingLLMs}
              className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors disabled:opacity-50"
              title="Reload from workflow.json"
            >
              <RefreshCw className={`w-5 h-5 ${(isRefreshing || isLoadingLLMs) ? 'animate-spin' : ''}`} />
            </button>
            <button
              onClick={onClose}
              className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {/* Body */}
        <div className="flex divide-x divide-gray-200 dark:divide-slate-700 overflow-y-auto min-h-0">
          {/* Left — how tiers are used */}
          <div className="w-2/5 p-5 space-y-4 shrink-0">
            <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4">
              <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-2">Auto-selection rules</h3>
              <div className="text-sm text-gray-600 dark:text-gray-300 space-y-1.5">
                <p><span className="font-medium">Tier 1</span> — first execution (no learnings yet) + initial extraction</p>
                <p><span className="font-medium">Tier 2</span> — execution once learnings exist + refinement</p>
                <p><span className="font-medium">Tier 3</span> — validation (always) + mature refinement (2+ runs)</p>
                <p><span className="font-medium">Phase LLM</span> — planning, eval design, debugging, anonymization. Independent of tier assignment.</p>
              </div>
            </div>
            <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4">
              <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-2">Fallbacks</h3>
              <p className="text-sm text-gray-600 dark:text-gray-300">
                Each tier can have ordered fallback models. If the primary model fails, the system tries fallbacks in order before returning an error.
              </p>
            </div>
          </div>

          {/* Right — tier config */}
          <div className="w-3/5 p-5 space-y-4 overflow-y-auto">

            {/* Tier 1 / 2 / 3 */}
            {TIERS.map(({ label, desc, icon: Icon, color, value, setter }) => (
              <div key={label} className="border border-gray-200 dark:border-slate-600 rounded-lg p-3">
                <div className="flex items-center justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <Icon className={`w-4 h-4 ${color}`} />
                    <div>
                      <span className="text-sm font-medium text-gray-700 dark:text-gray-300">{label}</span>
                      <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">{desc}</span>
                    </div>
                  </div>
                  {value && (
                    <button onClick={() => setter(null)} className="text-xs text-red-400 hover:text-red-600">Clear</button>
                  )}
                </div>
                <LLMSelectionDropdown
                  availableLLMs={availableLLMs}
                  selectedLLM={findOption(value)}
                  onLLMSelect={opt => setter(toAgentLLM(opt, value?.fallbacks))}
                  inModal={true}
                  openDirection="down"
                />
                {value && (
                  <div className="mt-2">
                    <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">Fallbacks</div>
                    {(value.fallbacks ?? []).length > 0 && (
                      <div className="flex flex-wrap gap-1.5 mb-2">
                        {(value.fallbacks ?? []).map((fb, idx) => (
                          <span key={`${fb.provider}-${fb.model_id}-${idx}`} className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded bg-gray-100 dark:bg-slate-700 text-gray-700 dark:text-gray-300">
                            <span className="font-mono">{fb.provider}/{fb.model_id}</span>
                            <button type="button" onClick={() => removeFallback(setter, idx)} className="text-gray-400 hover:text-red-500">
                              <X className="w-3 h-3" />
                            </button>
                          </span>
                        ))}
                      </div>
                    )}
                    <LLMSelectionDropdown
                      availableLLMs={getFallbackOptions(value)}
                      selectedLLM={null}
                      onLLMSelect={opt => addFallback(setter, opt)}
                      inModal={true}
                      openDirection="down"
                      placeholder="+ Add fallback"
                      disabled={getFallbackOptions(value).length === 0}
                    />
                  </div>
                )}
              </div>
            ))}

            {/* Phase LLM */}
            <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-3">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <Cpu className="w-4 h-4 text-orange-500" />
                  <div>
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Phase Agent</span>
                    <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">planning, eval, debugging</span>
                  </div>
                </div>
                {phaseLLM && (
                  <button onClick={() => setPhaseLLM(null)} className="text-xs text-red-400 hover:text-red-600">Clear</button>
                )}
              </div>
              <LLMSelectionDropdown
                availableLLMs={availableLLMs}
                selectedLLM={findOption(phaseLLM)}
                onLLMSelect={opt => setPhaseLLM(toAgentLLM(opt))}
                inModal={true}
                openDirection="down"
                placeholder={tier1 ? `Defaults to Tier 1 (${tier1.model_id})` : 'Select phase agent model'}
              />
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex justify-end gap-2 px-6 py-3 border-t border-gray-200 dark:border-slate-700 shrink-0">
          <Button variant="outline" size="sm" onClick={onClose} disabled={isSaving}>Cancel</Button>
          <Button size="sm" onClick={handleSave} disabled={isSaving}>
            {isSaving && <Loader2 className="w-3.5 h-3.5 mr-1.5 animate-spin" />}
            Save
          </Button>
        </div>
      </div>
    </div>
  )
}

export default function WorkflowLLMConfigModal({ isOpen, onClose }: WorkflowLLMConfigModalProps) {
  if (!isOpen) return null
  return <WorkflowLLMConfigModalContent onClose={onClose} />
}
