import { X, Brain, Zap, Gauge, Server, Shield, FolderOpen, Sparkles } from 'lucide-react'
import { Button } from './ui/Button'
import { useLLMStore } from '../stores'
import { useChatStore } from '../stores'
import type { DelegationTierConfig, TierModel } from '../services/api-types'
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

export default function DelegationTierConfigModal({ isOpen, onClose }: DelegationTierConfigModalProps) {
  const { availableLLMs, delegationTierConfig, setDelegationTierConfig } = useLLMStore()

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        className="bg-white dark:bg-slate-800 rounded-lg shadow-xl w-full max-w-4xl mx-4"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 dark:border-slate-700">
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

        {/* Two-column body */}
        <div className="flex divide-x divide-gray-200 dark:divide-slate-700">
          {/* Left: How it works + features */}
          <div className="w-1/2 p-5 space-y-4">
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
          <div className="w-1/2 p-5">
            <h3 className="text-sm font-semibold text-gray-800 dark:text-gray-200 mb-1">Sub-Agent Models</h3>
            <p className="text-xs text-gray-500 dark:text-gray-400 mb-3">
              Assign models by complexity tier. Leave empty to use the parent model.
            </p>

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
                            const newConfig: DelegationTierConfig = { ...delegationTierConfig }
                            delete newConfig[key]
                            const hasAny = newConfig.high || newConfig.medium || newConfig.low
                            const finalConfig = hasAny ? newConfig : null
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
                      selectedLLM={selectedLLM}
                      onLLMSelect={(llm: LLMOption) => {
                        const newTier: TierModel = { provider: llm.provider, model_id: llm.model }
                        const newConfig: DelegationTierConfig = { ...delegationTierConfig, [key]: newTier }
                        setDelegationTierConfig(newConfig)
                        syncTierConfigToActiveTab(newConfig)
                      }}
                      inModal={true}
                      openDirection="down"
                      title={`Select ${key} tier model`}
                    />
                  </div>
                )
              })}
            </div>
          </div>
        </div>

        {/* Footer */}
        <div className="flex justify-end px-6 py-3 border-t border-gray-200 dark:border-slate-700">
          <Button onClick={onClose} size="sm">Done</Button>
        </div>
      </div>
    </div>
  )
}
