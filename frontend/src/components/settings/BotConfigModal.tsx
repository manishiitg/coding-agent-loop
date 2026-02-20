import { useState, useEffect, useCallback } from 'react'
import { X, Layers, Check, Settings, Brain, Gauge, Zap, Plus, Trash2, Tag } from 'lucide-react'
import { agentApi } from '../../services/api'
import { useLLMStore, useMCPStore } from '../../stores'
import ServerSelectionDropdown from '../ServerSelectionDropdown'
import SkillSelectionDropdown from '../skills/SkillSelectionDropdown'
import LLMSelectionDropdown from '../LLMSelectionDropdown'
import type { LLMOption } from '../../types/llm'
import type { CustomTierModel } from '../../services/api-types'

interface BotConfigModalProps {
  isOpen: boolean
  onClose: () => void
}

const TIERS = [
  { key: 'high' as const, label: 'High', desc: 'Complex / planning', icon: Brain, color: 'text-purple-500' },
  { key: 'medium' as const, label: 'Medium', desc: 'Standard tasks', icon: Gauge, color: 'text-blue-500' },
  { key: 'low' as const, label: 'Low', desc: 'Simple / validation', icon: Zap, color: 'text-green-500' },
]

export default function BotConfigModal({ isOpen, onClose }: BotConfigModalProps) {
  const { availableLLMs } = useLLMStore()
  const { getAvailableServers } = useMCPStore()

  const [selectedServers, setSelectedServers] = useState<string[]>([])
  const [selectedSkills, setSelectedSkills] = useState<string[]>([])
  const [allowedEmails, setAllowedEmails] = useState('')
  const [delegationMode, setDelegationMode] = useState<'plan' | 'spawn'>('plan')
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [tierConfig, setTierConfig] = useState<Record<string, { provider?: string; model_id?: string }>>({})

  const loadData = useCallback(async () => {
    try {
      const cfg = await agentApi.getSimulatorConfig()
      if (cfg.default_servers) setSelectedServers(cfg.default_servers)
      if (cfg.default_skills) setSelectedSkills(cfg.default_skills)
      if ((cfg as Record<string, unknown>).allowed_emails) {
        setAllowedEmails(((cfg as Record<string, unknown>).allowed_emails as string[]).join(', '))
      }
      if (cfg.delegation_tier_config && typeof cfg.delegation_tier_config === 'object') {
        const dtc = cfg.delegation_tier_config as Record<string, unknown>
        setTierConfig(dtc as Record<string, { provider?: string; model_id?: string }>)
        // Extract custom tiers
        if (dtc.custom && typeof dtc.custom === 'object') {
          setCustomTiers(dtc.custom as Record<string, CustomTierModel>)
        } else {
          setCustomTiers({})
        }
      } else {
        setTierConfig({})
        setCustomTiers({})
      }
      if (cfg.delegation_mode === 'plan' || cfg.delegation_mode === 'spawn') {
        setDelegationMode(cfg.delegation_mode)
      }
    } catch {
      // ignore
    }
  }, [])

  useEffect(() => {
    if (!isOpen) return
    setDirty(false)
    setSaved(false)
    loadData()
  }, [isOpen, loadData])

  const handleServerToggle = (server: string) => {
    setDirty(true)
    setSaved(false)
    setSelectedServers(prev =>
      prev.includes(server) ? prev.filter(s => s !== server) : [...prev, server]
    )
  }

  const handleSelectAllServers = () => {
    setDirty(true)
    setSaved(false)
    const all = getAvailableServers().filter(s => s !== 'mcp')
    setSelectedServers([...all])
  }

  const handleClearAllServers = () => {
    setDirty(true)
    setSaved(false)
    setSelectedServers([])
  }

  const handleSkillToggle = (skillFolderName: string) => {
    setDirty(true)
    setSaved(false)
    setSelectedSkills(prev =>
      prev.includes(skillFolderName) ? prev.filter(s => s !== skillFolderName) : [...prev, skillFolderName]
    )
  }

  const handleSelectAllSkills = (allSkillNames: string[]) => {
    setDirty(true)
    setSaved(false)
    setSelectedSkills([...allSkillNames])
  }

  const handleClearAllSkills = () => {
    setDirty(true)
    setSaved(false)
    setSelectedSkills([])
  }

  const handleTierSelect = (tier: string, llm: LLMOption) => {
    setDirty(true)
    setSaved(false)
    setTierConfig(prev => ({
      ...prev,
      [tier]: { provider: llm.provider, model_id: llm.model },
    }))
  }

  const handleTierClear = (tier: string) => {
    setDirty(true)
    setSaved(false)
    setTierConfig(prev => {
      const next = { ...prev }
      delete next[tier]
      return next
    })
  }

  // Custom tier state
  const [customTiers, setCustomTiers] = useState<Record<string, CustomTierModel>>({})

  const handleAddCustomTier = () => {
    setDirty(true)
    setSaved(false)
    const base = 'custom-tier'
    let slug = base
    let i = 1
    while (customTiers[slug]) {
      slug = `${base}-${i}`
      i++
    }
    setCustomTiers(prev => ({
      ...prev,
      [slug]: { description: '', provider: '', model_id: '' },
    }))
  }

  const handleRemoveCustomTier = (slug: string) => {
    setDirty(true)
    setSaved(false)
    setCustomTiers(prev => {
      const next = { ...prev }
      delete next[slug]
      return next
    })
  }

  const handleCustomTierDescChange = (slug: string, value: string) => {
    setDirty(true)
    setSaved(false)
    setCustomTiers(prev => ({
      ...prev,
      [slug]: { ...prev[slug], description: value },
    }))
  }

  const descToSlug = (desc: string) =>
    desc.toLowerCase().split(/\s+/).slice(0, 4).join('-').replace(/[^a-z0-9-]/g, '') || 'custom-tier'

  const handleCustomTierDescBlur = (slug: string) => {
    const tier = customTiers[slug]
    if (!tier?.description) return
    const candidate = descToSlug(tier.description)
    if (candidate === slug || customTiers[candidate]) return
    setCustomTiers(prev => {
      const next = { ...prev }
      delete next[slug]
      next[candidate] = tier
      return next
    })
  }

  const handleCustomTierLLMSelect = (slug: string, llm: LLMOption) => {
    setDirty(true)
    setSaved(false)
    setCustomTiers(prev => ({
      ...prev,
      [slug]: { ...prev[slug], provider: llm.provider, model_id: llm.model },
    }))
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      const emails = allowedEmails
        .split(',')
        .map(e => e.trim())
        .filter(e => e.length > 0)

      // Build tier config for save (only non-empty tiers)
      const tierConfigToSave: Record<string, unknown> = {}
      for (const { key } of TIERS) {
        if (tierConfig[key]?.provider && tierConfig[key]?.model_id) {
          tierConfigToSave[key] = tierConfig[key]
        }
      }
      // Include custom tiers (only ones with provider+model set)
      const validCustom: Record<string, unknown> = {}
      for (const [slug, ct] of Object.entries(customTiers)) {
        if (ct.provider && ct.model_id) {
          validCustom[slug] = ct
        }
      }
      if (Object.keys(validCustom).length > 0) {
        tierConfigToSave.custom = validCustom
      }

      await agentApi.saveBotConfig({
        default_servers: selectedServers,
        default_skills: selectedSkills,
        allowed_emails: emails,
        delegation_mode: delegationMode,
        delegation_tier_config: Object.keys(tierConfigToSave).length > 0 ? tierConfigToSave : undefined,
      })
      setDirty(false)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch {
      // ignore
    } finally {
      setSaving(false)
    }
  }

  if (!isOpen) return null

  const availableServers = getAvailableServers().filter(s => s !== 'mcp')

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-md max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <h2 className="text-base font-semibold flex items-center gap-2">
            <Settings className="w-5 h-5 text-primary" />
            Bot Configuration
          </h2>
          <button
            onClick={onClose}
            className="p-1.5 text-muted-foreground hover:text-foreground transition-colors rounded"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Content — scrollable */}
        <div className="px-4 py-4 space-y-4 overflow-y-auto">
          {/* Delegation Tier Config */}
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              <Layers className="w-4 h-4 text-muted-foreground" />
              <span className="text-xs font-medium text-muted-foreground">LLM Tiers</span>
            </div>
            <div className="space-y-2">
              {TIERS.map(({ key, label, desc, icon: Icon, color }) => {
                const tier = tierConfig[key]
                const selectedLLM: LLMOption | null = tier?.provider && tier?.model_id
                  ? { provider: tier.provider, model: tier.model_id, label: `${tier.provider} - ${tier.model_id}`, description: `${key} tier` }
                  : null

                return (
                  <div key={key} className="flex items-center gap-2">
                    <div className="flex items-center gap-1.5 w-20 shrink-0">
                      <Icon className={`w-3.5 h-3.5 ${color}`} />
                      <div className="leading-tight">
                        <span className="text-xs font-medium text-foreground">{label}</span>
                        <span className="text-[9px] text-muted-foreground block">{desc}</span>
                      </div>
                    </div>
                    <div className="flex-1 min-w-0">
                      <LLMSelectionDropdown
                        availableLLMs={availableLLMs}
                        selectedLLM={selectedLLM}
                        onLLMSelect={(llm) => handleTierSelect(key, llm)}
                        inModal={true}
                        openDirection="down"
                        title={`Select ${key} tier model`}
                      />
                    </div>
                    {selectedLLM && (
                      <button
                        onClick={() => handleTierClear(key)}
                        className="text-[10px] text-red-400 hover:text-red-600 shrink-0"
                      >
                        Clear
                      </button>
                    )}
                  </div>
                )
              })}
            </div>
            {!tierConfig.high?.provider && (
              <span className="text-[10px] text-yellow-500">Set at least the High tier — it's used as the bot's main model.</span>
            )}

            {/* Custom tiers */}
            <div className="mt-2">
              <div className="flex items-center justify-between">
                <span className="text-[10px] text-muted-foreground">Custom Tiers</span>
                <button
                  onClick={handleAddCustomTier}
                  className="flex items-center gap-0.5 text-[10px] text-blue-500 hover:text-blue-700"
                >
                  <Plus className="w-3 h-3" />
                  Add
                </button>
              </div>
              {Object.entries(customTiers).map(([slug, tier]) => {
                const selectedLLM: LLMOption | null = tier.provider && tier.model_id
                  ? { provider: tier.provider, model: tier.model_id, label: `${tier.provider} - ${tier.model_id}`, description: slug }
                  : null

                return (
                  <div key={slug} className="mt-1.5 border border-dashed border-border rounded p-2 space-y-1.5">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-1">
                        <Tag className="w-3 h-3 text-orange-500" />
                        <span className="text-[9px] font-mono text-muted-foreground">{slug}</span>
                      </div>
                      <button
                        onClick={() => handleRemoveCustomTier(slug)}
                        className="text-[10px] text-red-400 hover:text-red-600 flex items-center gap-0.5"
                      >
                        <Trash2 className="w-2.5 h-2.5" />
                      </button>
                    </div>
                    <input
                      type="text"
                      placeholder="Description (e.g. fast model for simple tasks)"
                      value={tier.description}
                      onChange={(e) => handleCustomTierDescChange(slug, e.target.value)}
                      onBlur={() => handleCustomTierDescBlur(slug)}
                      className="w-full px-2 py-1 text-[11px] bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
                    />
                    <LLMSelectionDropdown
                      availableLLMs={availableLLMs}
                      selectedLLM={selectedLLM}
                      onLLMSelect={(llm) => handleCustomTierLLMSelect(slug, llm)}
                      inModal={true}
                      openDirection="down"
                      title={`Select model for ${slug}`}
                    />
                  </div>
                )
              })}
            </div>
          </div>

          <hr className="border-border" />

          <p className="text-xs text-muted-foreground">
            Select which MCP servers and skills are available to all bot interfaces (Slack, Web Simulator, etc.)
          </p>

          <div className="flex items-center gap-3">
            <div className="flex flex-col gap-1">
              <span className="text-xs font-medium text-muted-foreground">MCP Servers</span>
              <ServerSelectionDropdown
                availableServers={availableServers}
                selectedServers={selectedServers}
                onServerToggle={handleServerToggle}
                onSelectAll={handleSelectAllServers}
                onClearAll={handleClearAllServers}
              />
            </div>

            <div className="flex flex-col gap-1">
              <span className="text-xs font-medium text-muted-foreground">Skills</span>
              <SkillSelectionDropdown
                selectedSkills={selectedSkills}
                onSkillToggle={handleSkillToggle}
                onSelectAll={handleSelectAllSkills}
                onClearAll={handleClearAllSkills}
              />
            </div>
          </div>

          {/* Allowed Emails */}
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium text-muted-foreground">Allowed Emails (Slack)</span>
            <input
              type="text"
              value={allowedEmails}
              onChange={e => { setAllowedEmails(e.target.value); setDirty(true); setSaved(false) }}
              placeholder="user@example.com, user2@example.com"
              className="w-full px-2.5 py-1.5 text-xs bg-secondary border border-border rounded focus:outline-none focus:ring-1 focus:ring-primary"
            />
            <span className="text-[10px] text-muted-foreground">Comma-separated. Leave empty to allow everyone.</span>
          </div>

          {/* Delegation Mode */}
          <div className="flex flex-col gap-1">
            <span className="text-xs font-medium text-muted-foreground">Delegation Mode</span>
            <div className="flex items-center gap-2">
              {(['plan', 'spawn'] as const).map(mode => (
                <button
                  key={mode}
                  onClick={() => { setDelegationMode(mode); setDirty(true); setSaved(false) }}
                  className={`px-3 py-1 text-xs rounded-md border transition-colors ${
                    delegationMode === mode
                      ? 'bg-primary text-primary-foreground border-primary'
                      : 'bg-secondary text-muted-foreground border-border hover:text-foreground'
                  }`}
                >
                  {mode === 'plan' ? 'Plan' : 'Spawn'}
                </button>
              ))}
            </div>
            <span className="text-[10px] text-muted-foreground">
              Plan: agent creates a plan then delegates tasks. Spawn: agent directly spawns sub-agents.
            </span>
          </div>

          {/* Summary */}
          <div className="text-xs text-muted-foreground">
            {selectedServers.length} server{selectedServers.length !== 1 ? 's' : ''}, {selectedSkills.length} skill{selectedSkills.length !== 1 ? 's' : ''} selected
            {dirty && <span className="text-yellow-500 ml-2">(unsaved)</span>}
          </div>
        </div>

        {/* Footer */}
        <div className="px-4 py-3 border-t border-border flex items-center justify-end gap-2">
          <button
            onClick={handleSave}
            disabled={saving || (!dirty && !saved)}
            className={`px-4 py-1.5 text-sm rounded-md transition-colors flex items-center gap-1.5 ${
              saved
                ? 'bg-green-600 text-white'
                : 'bg-primary text-primary-foreground hover:bg-primary/90 disabled:opacity-50'
            }`}
          >
            {saving ? 'Saving...' : saved ? (
              <><Check className="w-3.5 h-3.5" /> Saved</>
            ) : 'Save'}
          </button>
        </div>
      </div>
    </div>
  )
}
