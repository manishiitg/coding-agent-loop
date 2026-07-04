import { useState, useEffect, useMemo } from 'react'
import { Trash2, Box, DollarSign, RefreshCw, Terminal, KeyRound, AudioLines } from 'lucide-react'
import { Button } from '../ui/Button'
import { useLLMStore } from '../../stores'
import { llmConfigService, type ModelMetadata } from '../../services/llm-config-api'
import type { SavedLLM } from '../../services/api-types'
import {
  getProviderDisplayInfo,
  getProviderIntegrationInfo,
  getProviderIntegrationKind,
  LLM_INTEGRATION_ORDER,
  PROVIDER_ORDER,
  shouldShowLLMPricing,
  type LLMIntegrationKind,
} from '../../utils/llmDisplay'

const formatContextWindow = (tokens?: number): string => {
  if (!tokens) return ''
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(0)}k`
  return `${tokens}`
}

const formatCost = (cost?: number): string => {
  if (cost === undefined || cost === null) return ''
  return `$${cost.toFixed(2)}`
}

const getOptionsSummary = (options?: Record<string, unknown>): string => {
  if (!options || Object.keys(options).length === 0) return ''
  const parts: string[] = []
  if (options.reasoning_effort) parts.push(`Reasoning: ${options.reasoning_effort}`)
  if (options.thinking_level) parts.push(`Thinking: ${options.thinking_level}`)
  if (options.thinking_budget) parts.push(`Budget: ${options.thinking_budget}`)
  return parts.join(' • ')
}

const isAutoPublishedLLM = (llm: SavedLLM): boolean =>
  llm.source === 'auto_coding_agent' || llm.id?.startsWith('auto:')

const providerOrderRank = (provider: string): number => {
  const index = PROVIDER_ORDER.indexOf(provider as typeof PROVIDER_ORDER[number])
  return index === -1 ? Number.MAX_SAFE_INTEGER : index
}

const groupSavedLLMsByProvider = (llms: SavedLLM[]): Array<{ provider: string; llms: SavedLLM[] }> => {
  const grouped = llms.reduce((acc, llm) => {
    const provider = llm.provider
    if (!acc[provider]) {
      acc[provider] = []
    }
    acc[provider].push(llm)
    return acc
  }, {} as Record<string, SavedLLM[]>)

  return Object.entries(grouped)
    .sort(([left], [right]) => {
      const rankDelta = providerOrderRank(left) - providerOrderRank(right)
      if (rankDelta !== 0) return rankDelta
      return left.localeCompare(right)
    })
    .map(([provider, providerLLMs]) => ({
      provider,
      llms: [...providerLLMs].sort((left, right) => {
        const nameDelta = left.name.localeCompare(right.name)
        if (nameDelta !== 0) return nameDelta
        return left.model_id.localeCompare(right.model_id)
      }),
    }))
}

export function LibraryTab() {
  const { savedLLMs, deleteSavedLLM, defaultPublishedLLMsLocked, loadDefaultsFromBackend } = useLLMStore()
  const [metadataMap, setMetadataMap] = useState<Record<string, ModelMetadata>>({})
  const [isRefreshing, setIsRefreshing] = useState(false)
  const integrationIcons: Record<LLMIntegrationKind, typeof Terminal> = {
    coding_agent: Terminal,
    api_model: KeyRound,
    audio_provider: AudioLines,
  }

  useEffect(() => {
    const fetchMetadata = async () => {
      try {
        const response = await llmConfigService.getModelMetadata()
        const map: Record<string, ModelMetadata> = {}
        response.models.forEach(m => {
          map[`${m.provider}:${m.model_id}`] = m
          map[m.model_id] = m
        })
        setMetadataMap(map)
      } catch (e) {
        console.warn('Failed to fetch model metadata:', e)
      }
    }
    fetchMetadata()
  }, [])

  const handleRefreshLibrary = async () => {
    setIsRefreshing(true)
    try {
      await loadDefaultsFromBackend()
    } finally {
      setIsRefreshing(false)
    }
  }

  const groupedSavedLLMs = useMemo(() => {
    return LLM_INTEGRATION_ORDER.map(kind => ({
      kind,
      llms: savedLLMs.filter(llm => getProviderIntegrationKind(llm.provider, llm.model_id) === kind),
    })).filter(group => group.llms.length > 0)
  }, [savedLLMs])

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-lg font-semibold text-foreground">Published LLM</h3>
          <p className="mt-0.5 text-xs text-muted-foreground">
            Saved model configurations grouped by coding agent or API provider.
          </p>
        </div>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => { void handleRefreshLibrary() }}
          disabled={isRefreshing}
          className="h-8 px-2"
          title="Refresh published LLMs from workspace"
        >
          <RefreshCw className={`w-4 h-4 ${isRefreshing ? 'animate-spin' : ''}`} />
        </Button>
      </div>

      {savedLLMs.length === 0 ? (
        <div className="rounded-md border border-dashed border-border bg-muted/20 p-8 text-center text-sm text-muted-foreground">
          No published LLMs yet. Configure a model in Provider tabs and publish it here.
        </div>
      ) : (
        <div className="space-y-3">
          {groupedSavedLLMs.map(group => {
            const integrationInfo = getProviderIntegrationInfo(group.llms[0]?.provider, group.llms[0]?.model_id)
            const IntegrationIcon = integrationIcons[group.kind]
            const providerGroups = groupSavedLLMsByProvider(group.llms)

            return (
              <section key={group.kind} className="overflow-hidden rounded-md border border-border bg-background">
                <div className="flex items-center justify-between border-b border-border bg-muted/40 px-3 py-1.5">
                  <div className={`flex items-center gap-2 text-xs font-semibold uppercase tracking-wide ${integrationInfo.toneClass}`}>
                    <IntegrationIcon className="w-3.5 h-3.5" />
                    {integrationInfo.label}
                  </div>
                  <span className="rounded bg-background px-1.5 py-0.5 text-[11px] text-muted-foreground">
                    {group.llms.length}
                  </span>
                </div>

                <div className="grid gap-2.5 p-2.5 2xl:grid-cols-2">
                  {providerGroups.map(providerGroup => {
                    const providerInfo = getProviderDisplayInfo(providerGroup.provider)

                    return (
                      <div key={providerGroup.provider} className="overflow-hidden rounded-md border border-border bg-card">
                        <div className="flex items-center justify-between border-b border-border bg-muted/30 px-2.5 py-1.5">
                          <div className="min-w-0">
                            <div className="truncate text-sm font-semibold text-foreground">{providerInfo.name}</div>
                          </div>
                          <span className="ml-2 shrink-0 rounded bg-background px-1.5 py-0.5 text-[11px] text-muted-foreground">
                            {providerGroup.llms.length}
                          </span>
                        </div>

                        <div className="grid gap-2 p-2.5 [grid-template-columns:repeat(auto-fill,minmax(230px,1fr))]">
                          {providerGroup.llms.map((llm) => {
                            const metadata = metadataMap[`${llm.provider}:${llm.model_id}`] || metadataMap[llm.model_id]
                            const optionsSummary = getOptionsSummary(llm.options)
                            const showPricing = shouldShowLLMPricing(llm.provider, llm.model_id)
                            const autoPublished = isAutoPublishedLLM(llm)
                            const apiKeyLast4 = llm.api_key && llm.api_key.length >= 4
                              ? `...${llm.api_key.slice(-4)}`
                              : null

                            return (
                              <div key={llm.id} className="min-h-[104px] rounded-md border border-border bg-background p-3">
                                <div className="min-w-0">
                                  <div className="flex items-start justify-between gap-1.5">
                                    <div className="min-w-0">
                                      <div className="truncate text-sm font-semibold text-foreground">{llm.name}</div>
                                      <div className="mt-0.5 truncate text-xs text-muted-foreground">
                                        {llm.model_id}
                                      </div>
                                    </div>
                                    {!defaultPublishedLLMsLocked && !autoPublished && (
                                      <Button
                                        size="sm"
                                        variant="ghost"
                                        onClick={() => { void deleteSavedLLM(llm.id) }}
                                        className="h-6 w-6 shrink-0 p-0 text-destructive hover:bg-destructive/10 hover:text-destructive"
                                        title="Delete published LLM"
                                      >
                                        <Trash2 className="w-3.5 h-3.5" />
                                      </Button>
                                    )}
                                  </div>

                                  {apiKeyLast4 && (
                                  <div className="mt-1 flex flex-wrap items-center gap-1.5">
                                      <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground" title="API Key (last 4 chars)">
                                        Key {apiKeyLast4}
                                      </span>
                                  </div>
                                  )}

                                  {autoPublished && (
                                    <div className="mt-1 flex flex-wrap items-center gap-1.5">
                                      <span className="shrink-0 rounded bg-primary/10 px-1.5 py-0.5 text-[11px] font-medium text-primary">
                                        Auto
                                      </span>
                                    </div>
                                  )}

                                  <div className="mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] text-muted-foreground">
                                    {metadata?.context_window && (
                                      <span className="flex items-center gap-0.5" title="Context window">
                                        <Box className="w-3 h-3" />
                                        {formatContextWindow(metadata.context_window)}
                                      </span>
                                    )}
                                    {showPricing && metadata?.input_cost_per_1m !== undefined && (
                                      <span className="flex items-center gap-0.5" title="Input cost per 1M tokens">
                                        <DollarSign className="w-3 h-3" />
                                        {formatCost(metadata.input_cost_per_1m)} in
                                      </span>
                                    )}
                                    {showPricing && metadata?.output_cost_per_1m !== undefined && (
                                      <span title="Output cost per 1M tokens">
                                        {formatCost(metadata.output_cost_per_1m)} out
                                      </span>
                                    )}
                                  </div>

                                  {optionsSummary && (
                                    <div className="mt-1 truncate text-[11px] text-primary/70">
                                      {optionsSummary}
                                    </div>
                                  )}
                                </div>
                              </div>
                            )
                          })}
                        </div>
                      </div>
                    )
                  })}
                </div>
              </section>
            )
          })}
        </div>
      )}
    </div>
  )
}
