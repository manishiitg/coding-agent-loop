import { useState, useCallback, useEffect, useMemo } from 'react'
import { X, Brain, Zap, Gauge, Cpu, Loader2, RefreshCw, SlidersHorizontal, Activity } from 'lucide-react'
import { Button } from './ui/Button'
import { useLLMStore, useChatStore } from '../stores'
import { useGlobalPresetStore } from '../stores/useGlobalPresetStore'
import { useWorkflowManifestStore } from '../stores/useWorkflowManifestStore'
import LLMSelectionDropdown from './LLMSelectionDropdown'
import LLMRoleSelector from './LLMRoleSelector'
import WorkflowLLMTierPreview from './WorkflowLLMTierPreview'
import ModalPortal from './ui/ModalPortal'
import type { AgentLLMConfig, AgentLLMFallback, PresetLLMConfig } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import { formatLLMOptions, formatLLMRef, formatLLMRefWithOptions, llmOptionsKey } from '../utils/llmConfigDisplay'
import type { CustomPreset } from '../types/preset'
import { getWorkflowLLMOptions, getWorkflowLLMTierDefaults, getWorkflowProviderOptions } from '../utils/workflowLLMTierDefaults'

interface WorkflowLLMConfigModalProps {
  isOpen: boolean
  onClose: () => void
}

function hasAdvancedWorkflowLLMConfig(config?: PresetLLMConfig | null): boolean {
  return config?.mode === 'explicit'
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
  const providerProfileOptions = useMemo(
    () => getWorkflowProviderOptions(providerManifest),
    [providerManifest]
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

  const initialProvider = (() => {
    const config = manifestLLM ?? existing
    return config?.mode === 'provider_profile' ? config.provider ?? null : config?.builder_llm?.provider ?? null
  })()

  const [selectedProvider, setSelectedProvider] = useState<string | null>(initialProvider)
  const [tier1, setTier1] = useState<AgentLLMConfig | null>(manifestLLM?.tiered_config?.tier_1 ?? existing?.tiered_config?.tier_1 ?? null)
  const [tier2, setTier2] = useState<AgentLLMConfig | null>(manifestLLM?.tiered_config?.tier_2 ?? existing?.tiered_config?.tier_2 ?? null)
  const [tier3, setTier3] = useState<AgentLLMConfig | null>(manifestLLM?.tiered_config?.tier_3 ?? existing?.tiered_config?.tier_3 ?? null)
  const [builderLLM, setBuilderLLM] = useState<AgentLLMConfig | null>(manifestLLM?.builder_llm ?? existing?.builder_llm ?? null)
  const [maintenanceLLM, setMaintenanceLLM] = useState<AgentLLMConfig | null>(manifestLLM?.maintenance_llm ?? existing?.maintenance_llm ?? null)
  const [pulseLLM, setPulseLLM] = useState<AgentLLMConfig | null>(manifestLLM?.pulse_llm ?? existing?.pulse_llm ?? null)
  const [showAdvanced, setShowAdvanced] = useState(() => hasAdvancedWorkflowLLMConfig(manifestLLM ?? existing))

  if (!activePreset) return null

  function hasOptions(options?: Record<string, unknown>) {
    return Boolean(options && Object.keys(options).length > 0)
  }
  const llmKey = (llm: { provider?: string; model_id?: string; published_llm_id?: string; options?: Record<string, unknown> }) =>
    llm.published_llm_id ? `id:${llm.published_llm_id}` : `model:${llm.provider}/${llm.model_id}/${llmOptionsKey(llm.options)}`
  const optionKey = (opt: LLMOption) =>
    opt.id ? `id:${opt.id}` : `model:${opt.provider}/${opt.model}/${llmOptionsKey(opt.options)}`

  const selectedProviderOption = providerProfileOptions.find(option => option.provider === selectedProvider) ?? null
  const defaultTierLLMs = (() => {
    const selected = selectedProviderOption
    return selected ? getWorkflowLLMTierDefaults(selected, providerManifest) : null
  })()

  const effectiveTier1 = tier1 ?? defaultTierLLMs?.tier1 ?? null
  const effectiveTier2 = tier2 ?? defaultTierLLMs?.tier2 ?? null
  const effectiveTier3 = tier3 ?? defaultTierLLMs?.tier3 ?? null
  const effectiveBuilderLLM = builderLLM ?? defaultTierLLMs?.builder ?? effectiveTier1
  const effectiveMaintenanceLLM = maintenanceLLM ?? defaultTierLLMs?.maintenance ?? effectiveTier1
  const effectivePulseLLM = pulseLLM ?? defaultTierLLMs?.pulse ?? effectiveTier1

  const ResolvedModelLine = ({ value, inherited }: { value: AgentLLMConfig | null; inherited?: boolean }) => (
    <div className="mt-2 rounded-md border border-gray-100 bg-gray-50 px-2.5 py-2 dark:border-slate-700 dark:bg-slate-900/50">
      <div className="text-[10px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
        {inherited ? 'Resolved default' : 'Resolved model'}
      </div>
      <div className="truncate font-mono text-[12px] text-gray-900 dark:text-gray-100" title={formatLLMRefWithOptions(value)}>
        {formatLLMRef(value)}
      </div>
      {formatLLMOptions(value?.options) && (
        <div className="truncate text-[11px] text-primary/75" title={formatLLMOptions(value?.options)}>
          {formatLLMOptions(value?.options)}
        </div>
      )}
    </div>
  )

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

  const sharedWorkflowLLM = effectiveBuilderLLM ?? effectiveTier1 ?? effectiveTier2 ?? effectiveTier3

  const handleSharedWorkflowLLMSelect = (opt: LLMOption) => {
    const defaults = getWorkflowLLMTierDefaults(opt, providerManifest)
    setSelectedProvider(opt.provider)
    setBuilderLLM(defaults.builder)
    setTier1(defaults.tier1)
    setTier2(defaults.tier2)
    setTier3(defaults.tier3)
    setMaintenanceLLM(defaults.maintenance)
    setPulseLLM(defaults.pulse)
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
          setSelectedProvider(refreshed.mode === 'provider_profile' ? refreshed.provider ?? null : refreshed.builder_llm?.provider ?? null)
          if (refreshed.tiered_config?.tier_1) setTier1(refreshed.tiered_config.tier_1)
          if (refreshed.tiered_config?.tier_2) setTier2(refreshed.tiered_config.tier_2)
          if (refreshed.tiered_config?.tier_3) setTier3(refreshed.tiered_config.tier_3)
          setBuilderLLM(refreshed.builder_llm ?? null)
          setMaintenanceLLM(refreshed.maintenance_llm ?? null)
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
      let newLLMConfig: PresetLLMConfig
      if (!showAdvanced) {
        if (!selectedProvider) throw new Error('Choose a coding agent provider')
        newLLMConfig = {
          ...existingWithoutExecution,
          schema_version: 2,
          mode: 'provider_profile',
          provider: selectedProvider as PresetLLMConfig['provider'],
          builder_llm: undefined,
          maintenance_llm: undefined,
          pulse_llm: undefined,
          chief_of_staff_llm: undefined,
          tiered_config: undefined,
        }
      } else {
        if (!effectiveBuilderLLM || !effectiveMaintenanceLLM || !effectivePulseLLM || !tieredConfig) {
          throw new Error('Builder, Maintenance, Pulse, and all three execution tiers are required')
        }
        newLLMConfig = {
          ...existingWithoutExecution,
          schema_version: 2,
          mode: 'explicit',
          provider: undefined,
          builder_llm: effectiveBuilderLLM,
          maintenance_llm: effectiveMaintenanceLLM,
          pulse_llm: effectivePulseLLM,
          tiered_config: tieredConfig,
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
                    <p><span className="font-medium">Builder</span> — chat, planning, eval design, and workflow coordination.</p>
                    <p><span className="font-medium">Maintenance</span> — Harden, Goal Advisor, and deeper report/eval/KB/DB reviews.</p>
                    <p><span className="font-medium">Pulse</span> — routine Pulse Gate and daily QA coordination.</p>
                  </div>
                </>
              ) : (
                <>
                  <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-2">How it works</h3>
                  <div className="text-sm text-gray-600 dark:text-gray-300 space-y-1.5">
                    <p>Pick one coding agent or model for this automation.</p>
                    <p>Coding agents save high, medium, and low tiers automatically.</p>
                    <p>The provider supplies Builder, Maintenance, Pulse, and execution-tier defaults.</p>
                    <p>Provider defaults update with the app when new models are introduced.</p>
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
                  <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-1">Choose one automation provider</h3>
                    <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
                    Choose a coding agent profile. Its current role defaults stay managed by the app.
                  </p>
                  <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-4">
                    <LLMSelectionDropdown
                      availableLLMs={providerProfileOptions}
                      selectedLLM={selectedProviderOption}
                      onLLMSelect={handleSharedWorkflowLLMSelect}
                      inModal={true}
                      openDirection="down"
                      title="Select automation provider"
                      placeholder="Select a coding agent"
                    />
                    <WorkflowLLMTierPreview selectedLLM={selectedProviderOption} providerManifest={providerManifest} />
                    {sharedWorkflowLLM && (
                      <p className="text-xs text-gray-500 dark:text-gray-400 mt-3">
                        The provider profile is saved; its current role defaults resolve at runtime.
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
                    setMaintenanceLLM(null)
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
                <LLMRoleSelector
                  availableLLMs={workflowLLMOptions}
                  value={value}
                  onLLMSelect={opt => setter(toAgentLLM(opt, value?.fallbacks))}
                />
                <ResolvedModelLine value={value} inherited={!canClear} />
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

            {/* Builder LLM */}
            <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-3">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <Cpu className="w-4 h-4 text-orange-500" />
                  <div>
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Builder LLM</span>
                    <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">chat, planning, coordination</span>
                  </div>
                </div>
                {builderLLM && (
                  <button onClick={() => setBuilderLLM(null)} className="text-xs text-red-400 hover:text-red-600">Clear</button>
                )}
              </div>
              <LLMRoleSelector
                availableLLMs={workflowLLMOptions}
                value={effectiveBuilderLLM}
                onLLMSelect={opt => setBuilderLLM(toAgentLLM(opt))}
              />
              <ResolvedModelLine value={effectiveBuilderLLM} inherited={!builderLLM} />
            </div>

            {/* Maintenance LLM */}
            <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-3">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <RefreshCw className="w-4 h-4 text-cyan-500" />
                  <div>
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Maintenance LLM</span>
                    <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">Harden, Goal Advisor, report and eval review</span>
                  </div>
                </div>
                {maintenanceLLM && (
                  <button onClick={() => setMaintenanceLLM(null)} className="text-xs text-red-400 hover:text-red-600">Clear</button>
                )}
              </div>
              <LLMRoleSelector
                availableLLMs={workflowLLMOptions}
                value={effectiveMaintenanceLLM}
                onLLMSelect={opt => setMaintenanceLLM(toAgentLLM(opt))}
              />
              <ResolvedModelLine value={effectiveMaintenanceLLM} inherited={!maintenanceLLM} />
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
              <LLMRoleSelector
                availableLLMs={workflowLLMOptions}
                value={effectivePulseLLM}
                onLLMSelect={opt => setPulseLLM(toAgentLLM(opt))}
              />
              <ResolvedModelLine value={effectivePulseLLM} inherited={!pulseLLM} />
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
