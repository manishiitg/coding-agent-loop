import { X, Brain, Zap, Gauge, Server, Shield, FolderOpen, Sparkles, Tag, Plus, Trash2, Crown } from 'lucide-react'
import { Button } from './ui/Button'
import { useLLMStore } from '../stores'
import { useChatStore } from '../stores'
import type { DelegationTierConfig, TierModel, CustomTierModel } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import LLMSelectionDropdown from './LLMSelectionDropdown'

// Helper: sync delegation tier config to the active multi-agent tab
const syncTierConfigToActiveTab = (newConfig: DelegationTierConfig | null) => {
  const chatStore = useChatStore.getState()
  const activeTab = chatStore.getActiveTab()
  if (activeTab?.metadata?.mode === 'multi-agent') {
    chatStore.setTabConfig(activeTab.tabId, { delegationTierConfig: newConfig ?? undefined })
  }
}

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
  return !!(config.main || config.high || config.medium || config.low ||
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

export default function DelegationTierConfigModal({ isOpen, onClose }: DelegationTierConfigModalProps) {
  const { availableLLMs, delegationTierConfig, setDelegationTierConfig } = useLLMStore()

  if (!isOpen) return null

  const customTiers = delegationTierConfig?.custom ?? {}
  const customEntries = Object.entries(customTiers)

  const updateConfig = (next: DelegationTierConfig | null) => {
    setDelegationTierConfig(next)
    syncTierConfigToActiveTab(next)
  }

  const updateBuiltInTier = (key: BuiltInTierKey, updater: (current?: TierModel) => TierModel | undefined) => {
    const current = delegationTierConfig?.[key]
    const nextTier = updater(current)
    const nextConfig: DelegationTierConfig = { ...(delegationTierConfig ?? {}) }
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
    return availableLLMs.filter(llm => {
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
      custom: { ...customTiers, [slug]: newCustom },
    }
    updateConfig(newConfig)
  }

  const handleRemoveCustomTier = (slug: string) => {
    const newCustom = { ...customTiers }
    delete newCustom[slug]
    const newConfig: DelegationTierConfig = {
      ...delegationTierConfig,
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
      custom: newCustom,
    }
    updateConfig(newConfig)
  }

  const handleCustomTierLLMSelect = (slug: string, llm: LLMOption) => {
    const tier = customTiers[slug]
    if (!tier) return
    const updatedTier: CustomTierModel = { ...tier, provider: llm.provider, model_id: llm.model }
    const newConfig: DelegationTierConfig = {
      ...delegationTierConfig,
      custom: { ...customTiers, [slug]: updatedTier },
    }
    updateConfig(newConfig)
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        className="bg-white dark:bg-slate-800 rounded-lg shadow-xl w-full max-w-5xl mx-4 max-h-[90vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-slate-700 shrink-0">
          <div>
            <h2 className="text-xl font-semibold text-gray-900 dark:text-gray-100">Multi-Agent Mode</h2>
            <p className="text-sm text-gray-500 dark:text-gray-400 mt-0.5">Configure how your AI team works together</p>
          </div>
          <button
            onClick={onClose}
            className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Two-column body — scrollable */}
        <div className="flex divide-x divide-gray-200 dark:divide-slate-700 overflow-y-auto min-h-0">
          {/* Left: How it works + features */}
          <div className="w-2/5 p-5 space-y-4 shrink-0">
            <div className="bg-slate-50 dark:bg-slate-700/50 rounded-lg p-4">
              <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-2">How it works</h3>
              <div className="text-sm text-gray-600 dark:text-gray-300 space-y-1">
                <p><span className="font-medium">1. Describe a task</span> — clarifying questions if needed</p>
                <p><span className="font-medium">2. Plan is created</span> — broken into phased steps</p>
                <p><span className="font-medium">3. You approve</span> — review and approve or give feedback</p>
                <p><span className="font-medium">4. Parallel execution</span> — background agents work simultaneously</p>
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
          <div className="w-3/5 p-5 overflow-y-auto">

            {/* Main Agent section */}
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
                      const newConfig: DelegationTierConfig = { ...delegationTierConfig }
                      delete newConfig.main
                      const finalConfig = hasAnyConfig(newConfig) ? newConfig : null
                      setDelegationTierConfig(finalConfig)
                      syncTierConfigToActiveTab(finalConfig)
                    }}
                    className="text-xs text-red-400 hover:text-red-600"
                  >
                    Clear
                  </button>
                )}
              </div>
              <LLMSelectionDropdown
                availableLLMs={availableLLMs}
                selectedLLM={delegationTierConfig?.main
                  ? {
                      provider: delegationTierConfig.main.provider,
                      model: delegationTierConfig.main.model_id,
                      label: `${delegationTierConfig.main.provider} - ${delegationTierConfig.main.model_id}`,
                      description: 'main agent model'
                    }
                  : null}
                onLLMSelect={(llm: LLMOption) => {
                  const newTier: TierModel = { provider: llm.provider, model_id: llm.model }
                  const newConfig: DelegationTierConfig = { ...delegationTierConfig, main: newTier }
                  updateConfig(newConfig)
                }}
                inModal={true}
                openDirection="down"
                title="Select main agent model"
              />
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
                      const nextFallbacks = [...(current.fallbacks || []), { provider: llm.provider, model_id: llm.model }]
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

            <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-1">Sub-Agent Models</h3>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
              Assign models by complexity tier. Leave empty to use the parent model.
            </p>

            {/* Built-in tiers */}
            <div className="space-y-3">
              {TIERS.map(({ key, label, desc, icon: Icon, color }) => {
                const tierModel = delegationTierConfig?.[key]
                const selectedLLM: LLMOption | null = tierModel
                  ? {
                      provider: tierModel.provider,
                      model: tierModel.model_id,
                      label: `${tierModel.provider} - ${tierModel.model_id}`,
                      description: `${key} reasoning tier`
                    }
                  : null

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
                    <LLMSelectionDropdown
                      availableLLMs={availableLLMs}
                      selectedLLM={selectedLLM}
                      onLLMSelect={(llm: LLMOption) => {
                        const newTier: TierModel = { provider: llm.provider, model_id: llm.model, fallbacks: tierModel?.fallbacks }
                        updateBuiltInTier(key, () => newTier)
                      }}
                      inModal={true}
                      openDirection="down"
                      title={`Select ${key} tier model`}
                    />
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
                            const nextFallbacks = [...(current.fallbacks || []), { provider: llm.provider, model_id: llm.model }]
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
                  const selectedLLM: LLMOption | null = tier.provider && tier.model_id
                    ? {
                        provider: tier.provider,
                        model: tier.model_id,
                        label: `${tier.provider} - ${tier.model_id}`,
                        description: slug,
                      }
                    : null

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
                        availableLLMs={availableLLMs}
                        selectedLLM={selectedLLM}
                        onLLMSelect={(llm: LLMOption) => handleCustomTierLLMSelect(slug, llm)}
                        inModal={true}
                        openDirection="up"
                        title={`Select model for ${slug}`}
                      />
                    </div>
                  )
                })}
              </div>
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex justify-end px-6 py-3 border-t border-gray-200 dark:border-slate-700 shrink-0">
          <Button onClick={onClose} size="sm">Done</Button>
        </div>
      </div>
    </div>
  )
}
