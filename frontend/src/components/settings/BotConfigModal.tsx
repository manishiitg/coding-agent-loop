import { useState, useEffect, useCallback } from 'react'
import { X, Layers, Check, Settings } from 'lucide-react'
import { agentApi } from '../../services/api'
import { useLLMStore, useMCPStore } from '../../stores'
import ServerSelectionDropdown from '../ServerSelectionDropdown'
import SkillSelectionDropdown from '../skills/SkillSelectionDropdown'

interface BotConfigModalProps {
  isOpen: boolean
  onClose: () => void
}

export default function BotConfigModal({ isOpen, onClose }: BotConfigModalProps) {
  const { delegationTierConfig } = useLLMStore()
  const { getAvailableServers } = useMCPStore()

  const [selectedServers, setSelectedServers] = useState<string[]>([])
  const [selectedSkills, setSelectedSkills] = useState<string[]>([])
  const [allowedEmails, setAllowedEmails] = useState('')
  const [delegationMode, setDelegationMode] = useState<'plan' | 'spawn'>('plan')
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [serverTierConfig, setServerTierConfig] = useState<Record<string, { provider?: string; model_id?: string }> | null>(null)

  const loadData = useCallback(async () => {
    try {
      const cfg = await agentApi.getSimulatorConfig()
      if (cfg.default_servers) setSelectedServers(cfg.default_servers)
      if (cfg.default_skills) setSelectedSkills(cfg.default_skills)
      if ((cfg as Record<string, unknown>).allowed_emails) {
        setAllowedEmails(((cfg as Record<string, unknown>).allowed_emails as string[]).join(', '))
      }
      if (cfg.delegation_tier_config && typeof cfg.delegation_tier_config === 'object') {
        setServerTierConfig(cfg.delegation_tier_config as Record<string, { provider?: string; model_id?: string }>)
      } else {
        setServerTierConfig(null)
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

  const handleSave = async () => {
    setSaving(true)
    try {
      const emails = allowedEmails
        .split(',')
        .map(e => e.trim())
        .filter(e => e.length > 0)
      await agentApi.saveBotConfig({
        default_servers: selectedServers,
        default_skills: selectedSkills,
        allowed_emails: emails,
        delegation_mode: delegationMode,
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
  const tierCfg = serverTierConfig || (delegationTierConfig as Record<string, { provider?: string; model_id?: string }> | null)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-md flex flex-col">
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

        {/* Tier Config Bar */}
        <div className="px-4 py-1.5 border-b border-border bg-muted/30 flex items-center gap-2 text-[11px] text-muted-foreground flex-wrap">
          <Layers className="w-3.5 h-3.5 flex-shrink-0" />
          <span className="font-medium">Tiers:</span>
          {tierCfg ? (
            <>
              {['high', 'medium', 'low'].map(tier => {
                const cfg = tierCfg[tier]
                if (!cfg?.model_id) return null
                const label = cfg.model_id.split('/').pop() || cfg.model_id
                return (
                  <span key={tier} className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-secondary border border-border">
                    <span className="capitalize font-medium">{tier[0].toUpperCase()}:</span>
                    <span className="truncate max-w-[120px]">{label}</span>
                  </span>
                )
              })}
              {tierCfg.custom && typeof tierCfg.custom === 'object' &&
                Object.entries(tierCfg.custom as Record<string, { model_id?: string }>).map(([slug, cfg]) => {
                  if (!cfg?.model_id) return null
                  const label = cfg.model_id.split('/').pop() || cfg.model_id
                  return (
                    <span key={slug} className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-secondary border border-purple-500/30">
                      <span className="font-medium text-purple-400">{slug}:</span>
                      <span className="truncate max-w-[120px]">{label}</span>
                    </span>
                  )
                })
              }
            </>
          ) : (
            <span className="text-yellow-500">Not configured — set delegation tiers in LLM Settings</span>
          )}
        </div>

        {/* Content — dropdown selectors */}
        <div className="px-4 py-4 space-y-4">
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
