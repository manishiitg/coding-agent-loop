import { useEffect, useMemo, useRef, useState } from 'react'
import { X, Brain, Zap, Gauge, Server, Shield, FolderOpen, Sparkles, Tag, Plus, Trash2, Crown, RefreshCw, SlidersHorizontal } from 'lucide-react'
import { Button } from './ui/Button'
import { useLLMStore } from '../stores'
import type { DelegationTierConfig, TierModel, CustomTierModel } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import LLMSelectionDropdown from './LLMSelectionDropdown'
import LLMRoleSelector from './LLMRoleSelector'
import ModalPortal from './ui/ModalPortal'
import { formatLLMOptions, formatLLMRef, formatLLMRefWithOptions, llmOptionMatchesRef } from '../utils/llmConfigDisplay'
import { getWorkflowLLMOptions, getWorkflowLLMTierDefaults, getWorkflowProviderOptions } from '../utils/workflowLLMTierDefaults'

// Generate a slug from the first 4 words of a description, guarding against reserved names
const descToSlug = (desc: string): string => {
  const slug = desc
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, '')
    .trim()
    .split(/\s+/)
    .slice(0, 4)
    .join('-')
    .replace(/-+/g, '-')
    .slice(0, 30) || 'custom'
  if (slug === 'high' || slug === 'medium' || slug === 'low' || slug === 'main') {
    return `${slug}-tier`
  }
  return slug
}

// Check if config has any values (built-in or custom)
const hasAnyConfig = (config: DelegationTierConfig | null): boolean => {
  if (!config) return false
  return !!(config.provider || config.main || config.chief_of_staff || config.high || config.medium || config.low ||
    (config.custom && Object.keys(config.custom).length > 0))
}

interface DelegationTierConfigModalProps {
  isOpen: boolean
  onClose: () => void
}

const TIERS = [
  { key: 'high' as const, label: 'High (Complex)', desc: 'Architecture, planning', icon: Brain, color: 'text-purple-500' },
  { key: 'medium' as const, label: 'Medium (Standard)', desc: 'Implementation tasks', icon: Gauge, color: 'text-blue-500' },
  { key: 'low' as const, label: 'Low (Simple)', desc: 'Formatting, validation', icon: Zap, color: 'text-green-500' },
]

const FEATURES = [
  { icon: FolderOpen, label: 'Workspace', desc: 'Read/write files' },
  { icon: Server, label: 'MCP Servers', desc: 'Connected tool servers' },
  { icon: Sparkles, label: 'Skills', desc: 'Guided agent behavior' },
  { icon: Shield, label: 'Secrets', desc: 'Secure credentials' },
]

type BuiltInTierKey = 'main' | 'high' | 'medium' | 'low'
const BUILT_IN_TIERS: BuiltInTierKey[] = ['main', 'high', 'medium', 'low']

const tierKey = (tier?: TierModel): string | null => {
  if (!tier?.provider || !tier?.model_id) return null
  return `${tier.provider}/${tier.model_id}`
}

const hasAdvancedTierConfig = (config: DelegationTierConfig | null): boolean => {
  if (!config || config.mode === 'provider_profile') return false
  if (config.mode === 'explicit') return true
  if (config.custom && Object.keys(config.custom).length > 0) return true
  if (BUILT_IN_TIERS.some(key => (config[key]?.fallbacks || []).length > 0)) return true
  const configuredModels = new Set(BUILT_IN_TIERS.map(key => tierKey(config[key])).filter(Boolean))
  return configuredModels.size > 1
}

export default function DelegationTierConfigModal({ isOpen, onClose }: DelegationTierConfigModalProps) {
  const {
    availableLLMs,
    delegationTierConfig,
    setDelegationTierConfig,
    loadDelegationTierDefaults,
    providerManifest,
    providerManifestLoaded,
    loadProviderManifest,
  } = useLLMStore()
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const initializedAdvancedForOpen = useRef(false)
  const delegationLLMOptions = useMemo(
    () => getWorkflowLLMOptions(availableLLMs, providerManifest),
    [availableLLMs, providerManifest]
  )
  const providerProfileOptions = useMemo(
    () => getWorkflowProviderOptions(providerManifest),
    [providerManifest]
  )

  useEffect(() => {
    if (!isOpen) {
      initializedAdvancedForOpen.current = false
      return
    }
    if (!providerManifestLoaded) {
      loadProviderManifest()
    }
    if (!initializedAdvancedForOpen.current) {
      initializedAdvancedForOpen.current = true
      setShowAdvanced(hasAdvancedTierConfig(delegationTierConfig))
    }
  }, [isOpen, delegationTierConfig, loadProviderManifest, providerManifestLoaded])

  if (!isOpen) return null

  const customTiers = delegationTierConfig?.custom ?? {}
  const customEntries = Object.entries(customTiers)

  const updateConfig = (next: DelegationTierConfig | null) => {
    setDelegationTierConfig(next)
  }

  const selectedProviderOption = providerProfileOptions.find(option => option.provider === delegationTierConfig?.provider) ?? null
  const profileDefaults = selectedProviderOption ? getWorkflowLLMTierDefaults(selectedProviderOption, providerManifest) : null
  const sharedTierModel = delegationTierConfig?.main || delegationTierConfig?.high || delegationTierConfig?.medium || delegationTierConfig?.low
  const findOptionForTier = (tier?: TierModel, fallbackDescription = 'delegation tier model'): LLMOption | null => {
    if (!tier?.provider || !tier?.model_id) return null
    const matched = delegationLLMOptions.find(llm => llmOptionMatchesRef(llm, tier))
      || delegationLLMOptions.find(llm => llm.provider === tier.provider && llm.model === tier.model_id)
    if (matched) return matched
    return {
      provider: tier.provider,
      model: tier.model_id,
      label: `${tier.provider} - ${tier.model_id}`,
      description: fallbackDescription,
    }
  }

  const toTierModel = (config: { provider: string; model_id: string; options?: Record<string, unknown> }, fallbacks?: TierModel['fallbacks']): TierModel => ({
    provider: config.provider,
    model_id: config.model_id,
    ...(config.options ? { options: config.options } : {}),
    ...(fallbacks && fallbacks.length > 0 ? { fallbacks } : {}),
  })

  const tierModelForOption = (llm: LLMOption, _key: BuiltInTierKey, fallbacks?: TierModel['fallbacks']): TierModel => {
    return toTierModel({
      provider: llm.provider,
      model_id: llm.model,
      options: llm.options,
    }, fallbacks)
  }

  const sharedSelectedLLM = selectedProviderOption || findOptionForTier(sharedTierModel, 'model for all agents')

  const handleSharedLLMSelect = (llm: LLMOption) => {
    updateConfig({
      schema_version: 2,
      mode: 'provider_profile',
      provider: llm.provider,
    })
  }

  const chiefOfStaffTierModelForOption = (llm: LLMOption): TierModel => {
    return toTierModel({ provider: llm.provider, model_id: llm.model, options: llm.options })
  }

  const defaultChiefOfStaffTierModel = (() => {
    const sourceOption = sharedSelectedLLM
      || findOptionForTier(delegationTierConfig?.main, 'main agent model')
      || findOptionForTier(delegationTierConfig?.high, 'high reasoning tier')
    if (!sourceOption) return undefined
    return chiefOfStaffTierModelForOption(sourceOption)
  })()

  const effectiveChiefOfStaffTierModel = delegationTierConfig?.chief_of_staff
    || (profileDefaults ? toTierModel(profileDefaults.chiefOfStaff) : defaultChiefOfStaffTierModel)

  const enterAdvancedMode = () => {
    const defaults = profileDefaults
    updateConfig({
      schema_version: 2,
      mode: 'explicit',
      main: delegationTierConfig?.main || (defaults ? toTierModel(defaults.builder) : undefined),
      chief_of_staff: delegationTierConfig?.chief_of_staff || (defaults ? toTierModel(defaults.chiefOfStaff) : undefined),
      high: delegationTierConfig?.high || (defaults ? toTierModel(defaults.tier1) : undefined),
      medium: delegationTierConfig?.medium || (defaults ? toTierModel(defaults.tier2) : undefined),
      low: delegationTierConfig?.low || (defaults ? toTierModel(defaults.tier3) : undefined),
      custom: delegationTierConfig?.custom,
    })
    setShowAdvanced(true)
  }

  const useSimpleMode = () => {
    const provider = delegationTierConfig?.provider || delegationTierConfig?.main?.provider
    if (provider) {
      updateConfig({ schema_version: 2, mode: 'provider_profile', provider })
    }
    setShowAdvanced(false)
  }

  const ResolvedTierLine = ({ value, inherited }: { value?: TierModel | CustomTierModel; inherited?: boolean }) => {
    if (!value?.provider || !value?.model_id) return null
    const label = inherited ? 'Resolved default' : 'Resolved model'
    const resolved = formatLLMRef(value)
    const options = formatLLMOptions('options' in value ? value.options : undefined)
    return (
      <div className="mt-2 rounded-md border border-gray-100 bg-gray-50 px-2.5 py-2 dark:border-slate-700 dark:bg-slate-900/50">
        <div className="text-[10px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
          {label}
        </div>
        <div className="truncate font-mono text-[12px] text-gray-900 dark:text-gray-100" title={formatLLMRefWithOptions(value)}>
          {resolved}
        </div>
        {options && <div className="truncate text-[11px] text-primary/75">{options}</div>}
      </div>
    )
  }

  const updateChiefOfStaffTier = (tier?: TierModel) => {
    const nextConfig: DelegationTierConfig = { ...(delegationTierConfig ?? {}), schema_version: 2, mode: 'explicit', provider: undefined }
    if (tier) {
      nextConfig.chief_of_staff = tier
    } else {
      delete nextConfig.chief_of_staff
    }
    updateConfig(hasAnyConfig(nextConfig) ? nextConfig : null)
  }

  const handleRefresh = async () => {
    setIsRefreshing(true)
    try {
      await loadDelegationTierDefaults()
    } finally {
      setIsRefreshing(false)
    }
  }

  const updateBuiltInTier = (key: BuiltInTierKey, updater: (current?: TierModel) => TierModel | undefined) => {
    const current = delegationTierConfig?.[key]
    const nextTier = updater(current)
    const nextConfig: DelegationTierConfig = { ...(delegationTierConfig ?? {}), schema_version: 2, mode: 'explicit', provider: undefined }
    if (!nextTier) {
      delete nextConfig[key]
    } else {
      nextConfig[key] = nextTier
    }
    const finalConfig = hasAnyConfig(nextConfig) ? nextConfig : null
    updateConfig(finalConfig)
  }

  const getAvailableFallbackOptions = (tierModel?: TierModel): LLMOption[] => {
    if (!tierModel) return []
    const existing = new Set((tierModel.fallbacks || []).map(f => `${f.provider}/${f.model_id}`))
    return delegationLLMOptions.filter(llm => {
      if (llm.provider === tierModel.provider && llm.model === tierModel.model_id) return false
      return !existing.has(`${llm.provider}/${llm.model}`)
    })
  }

  const handleAddCustomTier = () => {
    const base = 'custom-tier'
    let slug = base
    let i = 1
    while (customTiers[slug]) {
      slug = `${base}-${i}`
      i++
    }
    const newCustom: CustomTierModel = {
      description: '',
      provider: '',
      model_id: '',
    }
    const newConfig: DelegationTierConfig = {
      ...delegationTierConfig,
      schema_version: 2,
      mode: 'explicit',
      provider: undefined,
      custom: { ...customTiers, [slug]: newCustom },
    }
    updateConfig(newConfig)
  }

  const handleRemoveCustomTier = (slug: string) => {
    const newCustom = { ...customTiers }
    delete newCustom[slug]
    const newConfig: DelegationTierConfig = {
      ...delegationTierConfig,
      schema_version: 2,
      mode: 'explicit',
      provider: undefined,
      custom: Object.keys(newCustom).length > 0 ? newCustom : undefined,
    }
    const finalConfig = hasAnyConfig(newConfig) ? newConfig : null
    updateConfig(finalConfig)
  }

  const handleDescriptionChange = (slug: string, value: string) => {
    const tier = customTiers[slug]
    if (!tier) return
    const updatedTier = { ...tier, description: value }
    const newCustom = { ...customTiers, [slug]: updatedTier }
    const newConfig: DelegationTierConfig = {
      ...delegationTierConfig,
      schema_version: 2,
      mode: 'explicit',
      provider: undefined,
      custom: newCustom,
    }
    updateConfig(newConfig)
  }

  // Regenerate slug from description on blur (first 4 words)
  const handleDescriptionBlur = (slug: string) => {
    const tier = customTiers[slug]
    if (!tier || !tier.description) return

    const candidate = descToSlug(tier.description)
    if (candidate === slug || customTiers[candidate]) return // no change or collision

    const newCustom = { ...customTiers }
    delete newCustom[slug]
    newCustom[candidate] = tier

    const newConfig: DelegationTierConfig = {
      ...delegationTierConfig,
      schema_version: 2,
      mode: 'explicit',
      provider: undefined,
      custom: newCustom,
    }
    updateConfig(newConfig)
  }

  const handleCustomTierLLMSelect = (slug: string, llm: LLMOption) => {
    const tier = customTiers[slug]
    if (!tier) return
    const selected = tierModelForOption(llm, 'main')
    const updatedTier: CustomTierModel = { ...tier, provider: selected.provider, model_id: selected.model_id }
    const newConfig: DelegationTierConfig = {
      ...delegationTierConfig,
      schema_version: 2,
      mode: 'explicit',
      provider: undefined,
      custom: { ...customTiers, [slug]: updatedTier },
    }
    updateConfig(newConfig)
  }

  return (
    <ModalPortal>
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div
        className="bg-white dark:bg-slate-800 rounded-lg shadow-xl w-full max-w-5xl max-h-[calc(100vh-2rem)] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-slate-700 shrink-0">
          <div>
            <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Chief of Staff</h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">Configure how your AI team works together</p>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleRefresh}
              disabled={isRefreshing}
              className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              title="Refresh tier configuration"
            >
              <RefreshCw className={`w-5 h-5 ${isRefreshing ? 'animate-spin' : ''}`} />
            </button>
            <button
              onClick={onClose}
              className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
            >
              <X className="w-5 h-5" />
            </button>
          </div>
        </div>

        {/* Two-column body — scrollable */}
        <div className="flex flex-col md:flex-row divide-y md:divide-y-0 md:divide-x divide-gray-200 dark:divide-slate-700 overflow-y-auto min-h-0">
          {/* Left: How it works + features */}
          <div className="w-full md:w-2/5 p-5 space-y-4 shrink-0">
            <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4">
              <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-2">How model routing works</h3>
              <div className="text-sm text-gray-600 dark:text-gray-300 space-y-1">
                <p><span className="font-medium">Main</span> — runs the Chief of Staff chat and coordinates work</p>
                <p><span className="font-medium">High</span> — handles hard reasoning, architecture, and reviews</p>
                <p><span className="font-medium">Medium</span> — handles normal workflow, reporting, and implementation work</p>
                <p><span className="font-medium">Low</span> — handles cheap checks, formatting, and simple validation</p>
                <p><span className="font-medium">Fallbacks</span> — used when a selected model fails; empty tiers inherit the parent/default model</p>
              </div>
            </div>

            <div>
              <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-2">Agents have access to</h3>
              <div className="grid grid-cols-2 gap-2">
                {FEATURES.map(({ icon: Icon, label, desc }) => (
                  <div key={label} className="flex items-center gap-2 p-2 rounded-md bg-gray-50 dark:bg-slate-700/30">
                    <Icon className="w-4 h-4 text-gray-400 dark:text-gray-500 shrink-0" />
                    <div>
                      <span className="text-xs font-medium text-gray-700 dark:text-gray-300">{label}</span>
                      <p className="text-xs text-gray-500 dark:text-gray-400 leading-tight">{desc}</p>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          </div>

          {/* Right: Tier config */}
          <div className="w-full md:w-3/5 p-5 overflow-y-auto">
            {!showAdvanced && (
              <div className="space-y-5">
                <div>
                  <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-1">Choose one coding agent</h3>
                  <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
                    The provider manages Main, scheduled Chief of Staff, and reasoning-tier defaults.
                  </p>
                  <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-4">
                    <LLMSelectionDropdown
                      availableLLMs={providerProfileOptions}
                      selectedLLM={selectedProviderOption}
                      onLLMSelect={handleSharedLLMSelect}
                      inModal={true}
                      openDirection="down"
                      title="Select coding agent profile"
                    />
                    {profileDefaults && (
                      <div className="mt-3 grid grid-cols-1 gap-2 sm:grid-cols-2">
                        {([
                          [profileDefaults.builder, 'Main'],
                          [profileDefaults.chiefOfStaff, 'Scheduled'],
                          [profileDefaults.tier1, 'High'],
                          [profileDefaults.tier2, 'Medium'],
                          [profileDefaults.tier3, 'Low'],
                        ] as const).map(([value, label]) => {
                          return (
                            <div key={label} className="rounded-md border border-gray-100 bg-gray-50 px-2.5 py-2 dark:border-slate-700 dark:bg-slate-900/50">
                              <div className="text-[10px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">{label}</div>
                              <div className="truncate font-mono text-[12px] text-gray-900 dark:text-gray-100" title={formatLLMRef(value)}>
                                {formatLLMRef(value)}
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    )}
                  </div>
                </div>

                <button
                  type="button"
                  onClick={enterAdvancedMode}
                  className="inline-flex items-center gap-2 text-sm text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  <SlidersHorizontal className="w-4 h-4" />
                  Advanced tier setup
                </button>
              </div>
            )}

            {/* Main Agent section */}
            {showAdvanced && (
              <>
            <div className="flex justify-end mb-4">
              <button
                type="button"
                onClick={useSimpleMode}
                className="text-sm text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
              >
                Use simple setup
              </button>
            </div>

            <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-1">Main Agent</h3>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
              The orchestrator that creates the plan and coordinates sub-agents. Defaults to the global/tab LLM if not set.
            </p>
            <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-3 mb-5">
              <div className="flex items-center justify-between mb-1.5">
                <div className="flex items-center gap-2">
                  <Crown className="w-4 h-4 text-amber-500" />
                  <div>
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Main Agent (Orchestrator)</span>
                    <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">Plan creation &amp; coordination</span>
                  </div>
                </div>
                {delegationTierConfig?.main && (
                  <button
                    onClick={() => {
                      const newConfig: DelegationTierConfig = { ...delegationTierConfig, schema_version: 2, mode: 'explicit', provider: undefined }
                      delete newConfig.main
                      const finalConfig = hasAnyConfig(newConfig) ? newConfig : null
                      updateConfig(finalConfig)
                    }}
                    className="text-xs text-red-400 hover:text-red-600"
                  >
                    Clear
                  </button>
                )}
              </div>
              <LLMRoleSelector
                availableLLMs={delegationLLMOptions}
                value={delegationTierConfig?.main ?? null}
                onLLMSelect={(llm: LLMOption) => {
                  const newTier = tierModelForOption(llm, 'main')
                  const newConfig: DelegationTierConfig = { ...delegationTierConfig, schema_version: 2, mode: 'explicit', provider: undefined, main: newTier }
                  updateConfig(newConfig)
                }}
              />
              <ResolvedTierLine value={delegationTierConfig?.main} />
              {delegationTierConfig?.main && (
                <div className="mt-2">
                  <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">Fallbacks</div>
                  {(delegationTierConfig.main.fallbacks || []).length > 0 && (
                    <div className="flex flex-wrap gap-1.5 mb-2">
                      {(delegationTierConfig.main.fallbacks || []).map((fb, idx) => (
                        <span key={`${fb.provider}-${fb.model_id}-${idx}`} className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded bg-gray-100 dark:bg-slate-700 text-gray-700 dark:text-gray-300">
                          <span className="font-mono">{fb.provider}/{fb.model_id}</span>
                          <button
                            type="button"
                            onClick={() => updateBuiltInTier('main', (current) => {
                              if (!current) return current
                              const nextFallbacks = (current.fallbacks || []).filter((_, i) => i !== idx)
                              return { ...current, fallbacks: nextFallbacks.length > 0 ? nextFallbacks : undefined }
                            })}
                            className="text-gray-400 hover:text-red-500"
                          >
                            <X className="w-3 h-3" />
                          </button>
                        </span>
                      ))}
                    </div>
                  )}
                  <LLMSelectionDropdown
                    availableLLMs={getAvailableFallbackOptions(delegationTierConfig.main)}
                    selectedLLM={null}
                    onLLMSelect={(llm: LLMOption) => updateBuiltInTier('main', (current) => {
                      if (!current) return current
                      const fallback = tierModelForOption(llm, 'main')
                      const nextFallbacks = [...(current.fallbacks || []), { provider: fallback.provider, model_id: fallback.model_id, options: fallback.options }]
                      return { ...current, fallbacks: nextFallbacks }
                    })}
                    inModal={true}
                    openDirection="down"
                    title="Add main tier fallback"
                    placeholder="+ Add fallback"
                    disabled={getAvailableFallbackOptions(delegationTierConfig.main).length === 0}
                  />
                </div>
              )}
            </div>

            <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-1">Scheduled Chief of Staff</h3>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
              Optional override for scheduled Chief of Staff work such as Org Pulse. Defaults to the provider Chief of Staff model.
            </p>
            <div className="border border-gray-200 dark:border-slate-600 rounded-lg p-3 mb-5">
              <div className="flex items-center justify-between mb-1.5">
                <div className="flex items-center gap-2">
                  <Sparkles className="w-4 h-4 text-blue-500" />
                  <div>
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Scheduled Chief of Staff LLM</span>
                    <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">Org Pulse &amp; recurring CoS tasks</span>
                  </div>
                </div>
                {delegationTierConfig?.chief_of_staff && (
                  <button
                    type="button"
                    onClick={() => updateChiefOfStaffTier(undefined)}
                    className="text-xs text-red-400 hover:text-red-600"
                  >
                    Clear
                  </button>
                )}
              </div>
              <LLMRoleSelector
                availableLLMs={delegationLLMOptions}
                value={effectiveChiefOfStaffTierModel ?? null}
                onLLMSelect={(llm: LLMOption) => updateChiefOfStaffTier(chiefOfStaffTierModelForOption(llm))}
              />
              <ResolvedTierLine value={effectiveChiefOfStaffTierModel} inherited={!delegationTierConfig?.chief_of_staff} />
            </div>

            <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-1">Sub-Agent Models</h3>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
              Assign models by complexity tier. Leave empty to use the parent model.
            </p>

            {/* Built-in tiers */}
            <div className="space-y-3">
              {TIERS.map(({ key, label, desc, icon: Icon, color }) => {
                const tierModel = delegationTierConfig?.[key]

                return (
                  <div key={key} className="border border-gray-200 dark:border-slate-600 rounded-lg p-3">
                    <div className="flex items-center justify-between mb-1.5">
                      <div className="flex items-center gap-2">
                        <Icon className={`w-4 h-4 ${color}`} />
                        <div>
                          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">{label}</span>
                          <span className="text-xs text-gray-400 dark:text-gray-500 ml-1.5">{desc}</span>
                        </div>
                      </div>
                      {tierModel && (
                        <button
                          onClick={() => {
                            updateBuiltInTier(key, () => undefined)
                          }}
                          className="text-xs text-red-400 hover:text-red-600"
                        >
                          Clear
                        </button>
                      )}
                    </div>
                    <LLMRoleSelector
                      availableLLMs={delegationLLMOptions}
                      value={tierModel ?? null}
                      onLLMSelect={(llm: LLMOption) => {
                        const newTier = tierModelForOption(llm, key, tierModel?.fallbacks)
                        updateBuiltInTier(key, () => newTier)
                      }}
                    />
                    <ResolvedTierLine value={tierModel} />
                    {tierModel && (
                      <div className="mt-2">
                        <div className="text-xs text-gray-500 dark:text-gray-400 mb-1">Fallbacks</div>
                        {(tierModel.fallbacks || []).length > 0 && (
                          <div className="flex flex-wrap gap-1.5 mb-2">
                            {(tierModel.fallbacks || []).map((fb, idx) => (
                              <span key={`${fb.provider}-${fb.model_id}-${idx}`} className="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded bg-gray-100 dark:bg-slate-700 text-gray-700 dark:text-gray-300">
                                <span className="font-mono">{fb.provider}/{fb.model_id}</span>
                                <button
                                  type="button"
                                  onClick={() => updateBuiltInTier(key, (current) => {
                                    if (!current) return current
                                    const nextFallbacks = (current.fallbacks || []).filter((_, i) => i !== idx)
                                    return { ...current, fallbacks: nextFallbacks.length > 0 ? nextFallbacks : undefined }
                                  })}
                                  className="text-gray-400 hover:text-red-500"
                                >
                                  <X className="w-3 h-3" />
                                </button>
                              </span>
                            ))}
                          </div>
                        )}
                        <LLMSelectionDropdown
                          availableLLMs={getAvailableFallbackOptions(tierModel)}
                          selectedLLM={null}
                          onLLMSelect={(llm: LLMOption) => updateBuiltInTier(key, (current) => {
                            if (!current) return current
                            const fallback = tierModelForOption(llm, key)
                            const nextFallbacks = [...(current.fallbacks || []), { provider: fallback.provider, model_id: fallback.model_id, options: fallback.options }]
                            return { ...current, fallbacks: nextFallbacks }
                          })}
                          inModal={true}
                          openDirection="down"
                          title={`Add ${key} tier fallback`}
                          placeholder="+ Add fallback"
                          disabled={getAvailableFallbackOptions(tierModel).length === 0}
                        />
                      </div>
                    )}
                  </div>
                )
              })}
            </div>

            {/* Custom tiers */}
            <div className="mt-4">
              <div className="flex items-center justify-between mb-2">
                <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200">Custom Tiers</h3>
                <button
                  onClick={handleAddCustomTier}
                  className="flex items-center gap-1 text-xs text-blue-500 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  <Plus className="w-3.5 h-3.5" />
                  Add Custom Tier
                </button>
              </div>
              <p className="text-xs text-gray-500 dark:text-gray-400 mb-2">
                Define named tiers so the AI picks the right model per task type. The tag is auto-generated from the description.
              </p>

              {customEntries.length === 0 && (
                <p className="text-xs text-gray-400 dark:text-gray-500 italic py-2">
                  No custom tiers yet.
                </p>
              )}

              <div className="space-y-3">
                {customEntries.map(([slug, tier]) => {
                  const selectedLLM = findOptionForTier(tier.provider && tier.model_id
                    ? { provider: tier.provider, model_id: tier.model_id }
                    : undefined, slug)

                  return (
                    <div key={slug} className="border border-dashed border-gray-300 dark:border-slate-500 rounded-lg p-3">
                      <div className="flex items-center justify-between mb-2">
                        <div className="flex items-center gap-2">
                          <Tag className="w-4 h-4 text-orange-500" />
                          <span className="text-xs font-mono text-gray-400 dark:text-gray-500">{slug}</span>
                        </div>
                        <button
                          onClick={() => handleRemoveCustomTier(slug)}
                          className="text-xs text-red-400 hover:text-red-600 flex items-center gap-0.5"
                        >
                          <Trash2 className="w-3 h-3" />
                          Remove
                        </button>
                      </div>

                      <input
                        type="text"
                        placeholder="Description (e.g. low cost model for code reviews)"
                        value={tier.description}
                        onChange={(e) => handleDescriptionChange(slug, e.target.value)}
                        onBlur={() => handleDescriptionBlur(slug)}
                        className="w-full text-sm px-2 py-1 mb-2 rounded border border-gray-200 dark:border-slate-600 bg-white dark:bg-slate-700 text-gray-800 dark:text-gray-200 placeholder-gray-400 dark:placeholder-gray-500"
                      />

                      <LLMSelectionDropdown
                        availableLLMs={delegationLLMOptions}
                        selectedLLM={selectedLLM}
                        onLLMSelect={(llm: LLMOption) => handleCustomTierLLMSelect(slug, llm)}
                        inModal={true}
                        openDirection="up"
                        title={`Select model for ${slug}`}
                      />
                      <ResolvedTierLine value={tier} />
                    </div>
                  )
                })}
              </div>
            </div>
              </>
            )}
          </div>
        </div>

        {/* Footer */}
        <div className="flex justify-end px-6 py-3 border-t border-gray-200 dark:border-slate-700 shrink-0">
          <Button onClick={onClose} size="sm">Done</Button>
        </div>
      </div>
    </div>
    </ModalPortal>
  )
}
