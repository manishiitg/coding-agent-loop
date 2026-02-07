import { X } from 'lucide-react'
import { Button } from './ui/Button'
import { useLLMStore } from '../stores'
import type { DelegationTierConfig, TierModel } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import LLMSelectionDropdown from './LLMSelectionDropdown'

interface DelegationTierConfigModalProps {
  isOpen: boolean
  onClose: () => void
}

const TIERS = [
  { key: 'high' as const, label: 'High (Complex)', desc: 'Architecture, planning, complex reasoning' },
  { key: 'medium' as const, label: 'Medium (Standard)', desc: 'Standard implementation tasks' },
  { key: 'low' as const, label: 'Low (Simple)', desc: 'Formatting, validation, simple tasks' },
]

export default function DelegationTierConfigModal({ isOpen, onClose }: DelegationTierConfigModalProps) {
  const { availableLLMs, delegationTierConfig, setDelegationTierConfig } = useLLMStore()

  if (!isOpen) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        className="bg-white dark:bg-slate-800 rounded-lg shadow-xl w-full max-w-md mx-4 max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-gray-200 dark:border-slate-700">
          <h2 className="text-lg font-semibold text-gray-900 dark:text-gray-100">Delegation Tier Models</h2>
          <button
            onClick={onClose}
            className="p-1 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
          >
            <X className="w-5 h-5" />
          </button>
        </div>

        {/* Content */}
        <div className="p-4 space-y-4">
          <p className="text-sm text-gray-500 dark:text-gray-400">
            Assign models to reasoning tiers for sub-agent delegation. Leave empty to use the parent model.
          </p>

          {TIERS.map(({ key, label, desc }) => {
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
              <div key={key}>
                <div className="flex items-center justify-between mb-1">
                  <div>
                    <span className="text-sm font-medium text-gray-700 dark:text-gray-300">{label}</span>
                    <p className="text-xs text-gray-400 dark:text-gray-500">{desc}</p>
                  </div>
                  {tierModel && (
                    <button
                      onClick={() => {
                        const newConfig: DelegationTierConfig = { ...delegationTierConfig }
                        delete newConfig[key]
                        const hasAny = newConfig.high || newConfig.medium || newConfig.low
                        setDelegationTierConfig(hasAny ? newConfig : null)
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
                  }}
                  inModal={true}
                  openDirection="down"
                  title={`Select ${key} tier model`}
                />
              </div>
            )
          })}
        </div>

        {/* Footer */}
        <div className="flex justify-end p-4 border-t border-gray-200 dark:border-slate-700">
          <Button onClick={onClose} size="sm">Done</Button>
        </div>
      </div>
    </div>
  )
}
