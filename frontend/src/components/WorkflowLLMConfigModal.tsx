import { useState, useCallback, useEffect, useMemo } from 'react'
import { X, Brain, Zap, Gauge, Cpu, Loader2, RefreshCw, SlidersHorizontal, Activity } from 'lucide-react'
import { Button } from './ui/Button'
import { useLLMStore, useChatStore } from '../stores'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useWorkflowManifestStore } from '../stores/useWorkflowManifestStore'
import LLMSelectionDropdown from './LLMSelectionDropdown'
import WorkflowLLMTierPreview from './WorkflowLLMTierPreview'
import ModalPortal from './ui/ModalPortal'
import type { AgentLLMConfig, AgentLLMFallback, PresetLLMConfig } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import type { CustomPreset } from '../types/preset'
import { agentLLMToPresetBase, getWorkflowLLMOptions, getWorkflowLLMTierDefaults, hasWorkflowLLMTierDefaults } from '../utils/workflowLLMTierDefaults'

interface WorkflowLLMConfigModalProps {
  isOpen: boolean
  onClose: () => void
}

function hasAdvancedWorkflowLLMConfig(config?: PresetLLMConfig | null): boolean {
  if (!config) return false
  if (config.auto_improve_llm) return true
  if (config.pulse_llm) return true
  if (config.llm_allocation_mode === 'tiered') return true
  if (config.llm_allocation_mode === 'coding_agent' || config.llm_allocation_mode === 'coding_plan') {
    return false
  }

  const t1 = config.tiered_config?.tier_1
  const t2 = config.tiered_config?.tier_2
  const t3 = config.tiered_config?.tier_3
  const phase = config.phase_llm
  const configured = [t1, t2, t3, phase].filter(Boolean)
  const key = (cfg?: AgentLLMConfig | null) => cfg?.provider && cfg?.model_id ? `${cfg.provider}/${cfg.model_id}` : ''

  if (config.llm_allocation_mode === 'manual' && configured.length > 0) return true
  if (configured.some(cfg => (cfg?.fallbacks ?? []).length > 0)) return true
  return new Set(configured.map(key).filter(Boolean)).size > 1
}

// Inner component mounts fresh each open, so state always reflects the current preset
function WorkflowLLMConfigModalContent({ onClose }: { onClose: () => void }) {
  const { availableLLMs, loadDefaultsFromBackend, isLoadingLLMs, providerManifest, providerManifestLoaded, loadProviderManifest } = useLLMStore()
  const { getActivePreset, savePreset } = useGlobalPresetStore()
  const [isSaving, setIsSaving] = useState(false)
  const [isRefreshing, setIsRefreshing] = useState(false)

  const activePreset = getActivePreset('workflow') as CustomPreset | null
  const existing = activePreset?.llmConfig
  const workflowLLMOptions = useMemo(
    () => getWorkflowLLMOptions(availableLLMs, providerManifest),
    [availableLLMs, providerManifest]
  )

  useEffect(() => {
    if (!providerManifestLoaded) {
      loadProviderManifest()
    }
  }, [loadProviderManifest, providerManifestLoaded])

  // Read from workflow manifest (source of truth) with preset as fallback
  const manifestLLM = (() => {
    const workspacePath = activePreset?.selectedFolder?.filepath
    if (!workspacePath) return null
    return useWorkflowManifestStore.getState().getWorkflowByPath(workspacePath)?.manifest?.capabilities?.llm_config ?? null
  })()

  const initialCodingAgentLLM = (() => {
    const config = manifestLLM ?? existing
    if (!config?.provider || !config?.model_id) return null
    return {
      ...(config.published_llm_id ? { published_llm_id: config.published_llm_id } : {}),
      provider: config.provider,
      model_id: config.model_id,
      ...(hasOptions(config.options) ? { options: config.options } : {}),
    } as AgentLLMConfig
  })()

  const [selectedCodingAgentLLM, setSelectedCodingAgentLLM] = useState<AgentLLMConfig | null>(initialCodingAgentLLM)
  const [tier1, setTier1] = useState<AgentLLMConfig | null>(manifestLLM?.tiered_config?.tier_1 ?? existing?.tiered_config?.tier_1 ?? null)
  const [tier2, setTier2] = useState<AgentLLMConfig | null>(manifestLLM?.tiered_config?.tier_2 ?? existing?.tiered_config?.tier_2 ?? null)
  const [tier3, setTier3] = useState<AgentLLMConfig | null>(manifestLLM?.tiered_config?.tier_3 ?? existing?.tiered_config?.tier_3 ?? null)
  const [phaseLLM, setPhaseLLM] = useState<AgentLLMConfig | null>(manifestLLM?.phase_llm ?? existing?.phase_llm ?? null)
  const [autoImproveLLM, setAutoImproveLLM] = useState<AgentLLMConfig | null>(manifestLLM?.auto_improve_llm ?? existing?.auto_improve_llm ?? null)
  const [pulseLLM, setPulseLLM] = useState<AgentLLMConfig | null>(manifestLLM?.pulse_llm ?? existing?.pulse_llm ?? null)
  const [showAdvanced, setShowAdvanced] = useState(() => hasAdvancedWorkflowLLMConfig(manifestLLM ?? existing))

  if (!activePreset) return null

  function hasOptions(options?: Record<string, unknown>) {
    return Boolean(options && Object.keys(options).length > 0)
  }
  const llmKey = (llm: { provider?: string; model_id?: string; published_llm_id?: string }) =>
    llm.published_llm_id ? `id:${llm.published_llm_id}` : `model:${llm.provider}/${llm.model_id}`
  const optionKey = (opt: LLMOption) =>
    opt.id ? `id:${opt.id}` : `model:${opt.provider}/${opt.model}`

  const findOption = (cfg: AgentLLMConfig | null): LLMOption | null => {
    if (!cfg) return null
    if (cfg.published_llm_id) {
      const byID = workflowLLMOptions.find(o => o.id === cfg.published_llm_id)
      if (byID) return byID
    }
    return workflowLLMOptions.find(o => o.provider === cfg.provider && o.model === cfg.model_id) ?? null
  }

  const defaultTierLLMs = (() => {
    const selected = findOption(selectedCodingAgentLLM)
    return selected ? getWorkflowLLMTierDefaults(selected, providerManifest) : null
  })()

  const effectiveTier1 = tier1 ?? defaultTierLLMs?.tier1 ?? null
  const effectiveTier2 = tier2 ?? defaultTierLLMs?.tier2 ?? null
  const effectiveTier3 = tier3 ?? defaultTierLLMs?.tier3 ?? null
  const effectivePhaseLLM = phaseLLM ?? defaultTierLLMs?.phase ?? effectiveTier1
  const effectiveAutoImproveLLM = autoImproveLLM ?? defaultTierLLMs?.autoImprove ?? selectedCodingAgentLLM
  const effectivePulseLLM = pulseLLM ?? defaultTierLLMs?.pulse ?? selectedCodingAgentLLM

  const formatAgentLLMConfig = (cfg?: AgentLLMConfig | null) => {
    if (!cfg?.provider || !cfg?.model_id) return 'Not resolved'
    return `${cfg.provider}/${cfg.model_id}`
  }

  const getFallbackOptions = (primary: AgentLLMConfig | null): LLMOption[] => {
    if (!primary) return workflowLLMOptions
    const excluded = new Set([
      llmKey(primary),
      ...(primary.fallbacks ?? []).map(f => llmKey(f)),
    ])
    return workflowLLMOptions.filter(o => !excluded.has(optionKey(o)))
  }

  const toAgentLLM = (opt: LLMOption, fallbacks?: AgentLLMFallback[]): AgentLLMConfig => ({
    ...(opt.id ? { published_llm_id: opt.id } : {}),
    provider: opt.provider as AgentLLMConfig['provider'],
    model_id: opt.model,
    ...(hasOptions(opt.options) ? { options: opt.options } : {}),
    ...(fallbacks && fallbacks.length > 0 ? { fallbacks } : {}),
  })

  const sharedWorkflowLLM = selectedCodingAgentLLM ?? effectivePhaseLLM ?? effectiveTier1 ?? effectiveTier2 ?? effectiveTier3
  const sharedSelectedLLM = findOption(sharedWorkflowLLM)

  const handleSharedWorkflowLLMSelect = (opt: LLMOption) => {
    const defaults = getWorkflowLLMTierDefaults(opt, providerManifest)
    setSelectedCodingAgentLLM(defaults.base)
    setTier1(defaults.tier1)
    setTier2(defaults.tier2)
    setTier3(defaults.tier3)
    setPhaseLLM(defaults.phase)
  }

  const addFallback = (setter: React.Dispatch<React.SetStateAction<AgentLLMConfig | null>>, opt: LLMOption) => {
    setter(prev => {
      if (!prev) return prev
      const fb: AgentLLMFallback = {
        ...(opt.id ? { published_llm_id: opt.id } : {}),
        provider: opt.provider,
        model_id: opt.model,
        ...(hasOptions(opt.options) ? { options: opt.options } : {}),
      }
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
          if (refreshed.provider && refreshed.model_id) {
            setSelectedCodingAgentLLM({
              ...(refreshed.published_llm_id ? { published_llm_id: refreshed.published_llm_id } : {}),
              provider: refreshed.provider,
              model_id: refreshed.model_id,
              ...(hasOptions(refreshed.options) ? { options: refreshed.options } : {}),
            } as AgentLLMConfig)
          }
          if (refreshed.tiered_config?.tier_1) setTier1(refreshed.tiered_config.tier_1)
          if (refreshed.tiered_config?.tier_2) setTier2(refreshed.tiered_config.tier_2)
          if (refreshed.tiered_config?.tier_3) setTier3(refreshed.tiered_config.tier_3)
          if (refreshed.phase_llm) setPhaseLLM(refreshed.phase_llm)
          setAutoImproveLLM(refreshed.auto_improve_llm ?? null)
          setPulseLLM(refreshed.pulse_llm ?? null)
          setShowAdvanced(hasAdvancedWorkflowLLMConfig(refreshed))
        }
      }
    } finally {
      setIsRefreshing(false)
    }
  }, [activePreset?.selectedFolder?.filepath, loadDefaultsFromBackend])

  const handleSave = async () => {
    setIsSaving(true)
    try {
      const tieredConfig = effectiveTier1 && effectiveTier2 && effectiveTier3
        ? { tier_1: effectiveTier1, tier_2: effectiveTier2, tier_3: effectiveTier3 }
        : undefined
      const existingWithoutExecution = { ...((existing ?? {}) as PresetLLMConfig & { execution_llm?: unknown; learning_llm?: unknown }) }
      delete existingWithoutExecution.execution_llm
      delete existingWithoutExecution.learning_llm
      const nextBaseLLMConfig = {
        ...existingWithoutExecution,
        ...(selectedCodingAgentLLM ? agentLLMToPresetBase(selectedCodingAgentLLM) : {}),
      }
      if (!showAdvanced) {
        delete nextBaseLLMConfig.auto_improve_llm
        delete nextBaseLLMConfig.pulse_llm
      }

      let newLLMConfig: PresetLLMConfig
      if (!showAdvanced && !nextBaseLLMConfig.published_llm_id && nextBaseLLMConfig.provider && hasWorkflowLLMTierDefaults(nextBaseLLMConfig.provider, providerManifest)) {
        delete nextBaseLLMConfig.phase_llm
        delete nextBaseLLMConfig.tiered_config
        delete nextBaseLLMConfig.auto_improve_llm
        delete nextBaseLLMConfig.pulse_llm
        newLLMConfig = {
          ...nextBaseLLMConfig,
          llm_allocation_mode: 'coding_agent',
        }
      } else {
        newLLMConfig = {
          ...nextBaseLLMConfig,
          phase_llm: effectivePhaseLLM ?? undefined,
          auto_improve_llm: showAdvanced ? (autoImproveLLM ?? undefined) : undefined,
          pulse_llm: showAdvanced ? (pulseLLM ?? undefined) : undefined,
          llm_allocation_mode: tieredConfig ? 'tiered' : 'manual',
          ...(tieredConfig ? { tiered_config: tieredConfig } : {}),
        }
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
      // Surface the failure instead of silently swallowing it (previously the
      // modal just stayed open with no feedback — looked like "didn't save").
      // The server returns the validation reason as the response body.
      const serverDetail = (err as { response?: { data?: unknown } })?.response?.data
      const detail =
        typeof serverDetail === 'string' && serverDetail.trim() !== ''
          ? serverDetail.trim()
          : err instanceof Error
            ? err.message
            : 'Unknown error'
      useChatStore.getState().addToast(`Failed to save LLM config: ${detail}`, 'error')
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
    canClear: boolean
    setter: React.Dispatch<React.SetStateAction<AgentLLMConfig | null>>
  }> = [
    { label: 'Tier 1 — High Reasoning', desc: 'First-time execution & initial learning extraction', icon: Brain, color: 'text-purple-500', value: effectiveTier1, canClear: Boolean(tier1), setter: setTier1 },
    { label: 'Tier 2 — Medium Reasoning', desc: 'Execution with learnings & learning refinement', icon: Gauge, color: 'text-blue-500', value: effectiveTier2, canClear: Boolean(tier2), setter: setTier2 },
    { label: 'Tier 3 — Low Reasoning', desc: 'Validation & mature learning refinement (2+ runs)', icon: Zap, color: 'text-green-500', value: effectiveTier3, canClear: Boolean(tier3), setter: setTier3 },
  ]

  return (
    <ModalPortal>
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div
        className="bg-white dark:bg-slate-800 rounded-lg shadow-xl w-full max-w-4xl max-h-[calc(100vh-2rem)] flex flex-col"
        onClick={e => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-slate-700 shrink-0">
          <div>
            <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Automation LLM Configuration</h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">
              <span className="font-medium text-purple-500">{activePreset.label}</span> — model selection
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
        <div className="flex flex-col md:flex-row divide-y md:divide-y-0 md:divide-x divide-gray-200 dark:divide-slate-700 overflow-y-auto min-h-0">
          {/* Left — how tiers are used */}
          <div className="w-full md:w-2/5 p-5 space-y-4 shrink-0">
            <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4">
              {showAdvanced ? (
                <>
                  <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-2">Auto-selection rules</h3>
                  <div className="text-sm text-gray-600 dark:text-gray-300 space-y-1.5">
                    <p><span className="font-medium">Tier 1</span> — first execution (no learnings yet) + initial extraction</p>
                    <p><span className="font-medium">Tier 2</span> — execution once learnings exist + refinement</p>
                    <p><span className="font-medium">Tier 3</span> — validation (always) + mature refinement (2+ runs)</p>
                    <p><span className="font-medium">Workshop LLM</span> — planning, eval design, debugging, anonymization. Independent of tier assignment.</p>
                    <p><span className="font-medium">Goal Advisor LLM</span> — optional strategy-module override. Empty uses the provider default when available.</p>
                    <p><span className="font-medium">Pulse LLM</span> — optional post-run QA/harden override. Empty uses the provider Pulse default when available.</p>
                  </div>
                </>
              ) : (
                <>
                  <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-2">How it works</h3>
                  <div className="text-sm text-gray-600 dark:text-gray-300 space-y-1.5">
                    <p>Pick one coding agent or model for this automation.</p>
                    <p>Coding agents save high, medium, and low tiers automatically.</p>
                    <p>Workshop work uses the high tier.</p>
                    <p>Goal Advisor uses the provider default when Pulse Gate selects it unless an advanced override is set.</p>
                    <p>Pulse uses the provider Pulse default when available unless an advanced override is set.</p>
                  </div>
                </>
              )}
            </div>
            {showAdvanced && <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4">
              <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-2">Fallbacks</h3>
              <p className="text-sm text-gray-600 dark:text-gray-300">
                Each tier can have ordered fallback models. If the primary model fails, the system tries fallbacks in order before returning an error.
              </p>
            </div>}
          </div>

          {/* Right — model config */}
          <div className="w-full md:w-3/5 p-5 space-y-4 overflow-y-auto">
            {!showAdvanced && (
              <div className="space-y-5">
                <div>
                  <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-1">Choose one automation model</h3>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
                    Coding agents expand into high, medium, and low defaults. Workshop work uses high.
                  </p>
                  <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-4">
                    <LLMSelectionDropdown
                      availableLLMs={workflowLLMOptions}
                      selectedLLM={sharedSelectedLLM}
                      onLLMSelect={handleSharedWorkflowLLMSelect}
                      inModal={true}
                      openDirection="down"
                      title="Select model for automation"
                    />
                    <WorkflowLLMTierPreview selectedLLM={sharedSelectedLLM} providerManifest={providerManifest} />
                    {sharedWorkflowLLM && (
                      <p className="text-xs text-gray-500 dark:text-gray-400 mt-3">
                        Saved as automation tier defaults.
                      </p>
                    )}
                  </div>
                </div>

                <button
                  type="button"
                  onClick={() => setShowAdvanced(true)}
                  className="inline-flex items-center gap-2 text-sm text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  <SlidersHorizontal className="w-4 h-4" />
                  Advanced automation LLM setup
                </button>
              </div>
            )}

            {/* Tier 1 / 2 / 3 */}
            {showAdvanced && (
              <>
              <div className="flex justify-end">
                <button
                  type="button"
                  onClick={() => {
                    setShowAdvanced(false)
                    setAutoImproveLLM(null)
                    setPulseLLM(null)
                  }}
                  className="text-sm text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  Use simple setup
                </button>
              </div>
            {TIERS.map(({ label, desc, icon: Icon, color, value, canClear, setter }) => (
              <div key={label} className="border border-gray-200 dark:border-slate-600 rounded-lg p-3">
                <div className="flex items-center justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <Icon className={`w-4 h-4 ${color}`} />
                    <div>
                      <span className="text-sm font-medium text-gray-700 dark:text-gray-300">{label}</span>
                      <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">{desc}</span>
                    </div>
                  </div>
                  {canClear && (
                    <button onClick={() => setter(null)} className="text-xs text-red-400 hover:text-red-600">Clear</button>
                  )}
                </div>
                <LLMSelectionDropdown
                  availableLLMs={workflowLLMOptions}
                  selectedLLM={findOption(value)}
                  onLLMSelect={opt => setter(toAgentLLM(opt, value?.fallbacks))}
                  inModal={true}
                  openDirection="down"
                />
                <div className="mt-1 font-mono text-[11px] text-gray-700 dark:text-gray-300" title={formatAgentLLMConfig(value)}>
                  {formatAgentLLMConfig(value)}
                </div>
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

            {/* Workshop LLM */}
            <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-3">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <Cpu className="w-4 h-4 text-orange-500" />
                  <div>
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Workshop LLM</span>
                    <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">planning, eval, debugging</span>
                  </div>
                </div>
                {phaseLLM && (
                  <button onClick={() => setPhaseLLM(null)} className="text-xs text-red-400 hover:text-red-600">Clear</button>
                )}
              </div>
              <LLMSelectionDropdown
                availableLLMs={workflowLLMOptions}
                selectedLLM={findOption(effectivePhaseLLM)}
                onLLMSelect={opt => setPhaseLLM(toAgentLLM(opt))}
                inModal={true}
                openDirection="down"
                placeholder={effectiveTier1 ? `Defaults to Tier 1 (${effectiveTier1.model_id})` : 'Select workshop LLM'}
              />
              <div className="mt-1 font-mono text-[11px] text-gray-700 dark:text-gray-300" title={formatAgentLLMConfig(effectivePhaseLLM)}>
                {formatAgentLLMConfig(effectivePhaseLLM)}
              </div>
            </div>

            {/* Goal Advisor LLM */}
            <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-3">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <RefreshCw className="w-4 h-4 text-cyan-500" />
                  <div>
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Goal Advisor LLM</span>
                    <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">strategy module</span>
                  </div>
                </div>
                {autoImproveLLM && (
                  <button onClick={() => setAutoImproveLLM(null)} className="text-xs text-red-400 hover:text-red-600">Clear</button>
                )}
              </div>
              <LLMSelectionDropdown
                availableLLMs={workflowLLMOptions}
                selectedLLM={findOption(effectiveAutoImproveLLM)}
                onLLMSelect={opt => setAutoImproveLLM(toAgentLLM(opt))}
                inModal={true}
                openDirection="down"
                placeholder={effectiveAutoImproveLLM ? `Defaults to ${effectiveAutoImproveLLM.model_id}` : 'Defaults to provider default'}
              />
              <div className="mt-1 font-mono text-[11px] text-gray-700 dark:text-gray-300" title={formatAgentLLMConfig(effectiveAutoImproveLLM)}>
                {formatAgentLLMConfig(effectiveAutoImproveLLM)}
              </div>
            </div>

            {/* Pulse LLM */}
            <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-3">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <Activity className="w-4 h-4 text-sky-500" />
                  <div>
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Pulse LLM</span>
                    <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">post-run QA loop only</span>
                  </div>
                </div>
                {pulseLLM && (
                  <button onClick={() => setPulseLLM(null)} className="text-xs text-red-400 hover:text-red-600">Clear</button>
                )}
              </div>
              <LLMSelectionDropdown
                availableLLMs={workflowLLMOptions}
                selectedLLM={findOption(effectivePulseLLM)}
                onLLMSelect={opt => setPulseLLM(toAgentLLM(opt))}
                inModal={true}
                openDirection="down"
                placeholder={effectivePulseLLM ? `Defaults to ${effectivePulseLLM.model_id}` : 'Defaults to provider pulse default'}
              />
              <div className="mt-1 font-mono text-[11px] text-gray-700 dark:text-gray-300" title={formatAgentLLMConfig(effectivePulseLLM)}>
                {formatAgentLLMConfig(effectivePulseLLM)}
              </div>
            </div>
              </>
            )}
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
    </ModalPortal>
  )
}

export default function WorkflowLLMConfigModal({ isOpen, onClose }: WorkflowLLMConfigModalProps) {
  if (!isOpen) return null
  return <WorkflowLLMConfigModalContent onClose={onClose} />
}
