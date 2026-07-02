import type { ProviderManifestEntry } from '../services/llm-config-api'
import type { LLMOption } from '../types/llm'
import { getWorkflowLLMTierDefaults } from '../utils/workflowLLMTierDefaults'

type WorkflowLLMTierPreviewProps = {
  selectedLLM: LLMOption | null
  providerManifest: ProviderManifestEntry[]
}

const tierRows = [
  { key: 'tier1', label: 'High', usage: 'first run, complex execution' },
  { key: 'tier2', label: 'Medium', usage: 'execution with learnings' },
  { key: 'tier3', label: 'Low', usage: 'validation, mature learnings' },
  { key: 'phase', label: 'Workshop', usage: 'planning, eval, debugging' },
  { key: 'autoImprove', label: 'Auto Improve', usage: 'scheduled optimizer' },
] as const

function formatModel(provider: string, modelID: string) {
  return `${provider}/${modelID}`
}

export default function WorkflowLLMTierPreview({ selectedLLM, providerManifest }: WorkflowLLMTierPreviewProps) {
  if (!selectedLLM) return null

  const defaults = getWorkflowLLMTierDefaults(selectedLLM, providerManifest)
  const values = {
    tier1: defaults.tier1,
    tier2: defaults.tier2,
    tier3: defaults.tier3,
    phase: defaults.phase,
    autoImprove: defaults.autoImprove,
  }

  return (
    <div className="mt-3 rounded-md border border-gray-200 bg-white p-2.5 text-xs dark:border-slate-700 dark:bg-slate-900/60">
      <div className="mb-1 font-medium text-gray-700 dark:text-gray-200">Automatically managed tiers</div>
      <div className="mb-2 text-[11px] leading-snug text-gray-500 dark:text-gray-400">
        The automation picks the tier for each job. These are the models that will be used right now.
      </div>
      <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
        {tierRows.map(row => {
          const value = values[row.key]
          return (
            <div key={row.key} className="min-w-0 rounded border border-gray-100 bg-gray-50 px-2 py-1.5 dark:border-slate-700 dark:bg-slate-800">
              <div className="text-[10px] font-semibold uppercase tracking-wide text-gray-500 dark:text-gray-400">
                {row.label}
              </div>
              <div className="truncate text-[10px] text-gray-500 dark:text-gray-400" title={row.usage}>
                {row.usage}
              </div>
              <div className="truncate font-mono text-[11px] text-gray-800 dark:text-gray-100" title={formatModel(value.provider, value.model_id)}>
                {formatModel(value.provider, value.model_id)}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
