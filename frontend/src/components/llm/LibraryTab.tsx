import { useState, useEffect } from 'react'
import { Trash2, Box, DollarSign, Thermometer, CheckCircle } from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import { useLLMStore } from '../../stores'
import { llmConfigService, type ModelMetadata } from '../../services/llm-config-api'
import type { SavedLLM } from '../../services/api-types'

// Helper to format context window size
const formatContextWindow = (tokens?: number): string => {
  if (!tokens) return ''
  if (tokens >= 1000000) return `${(tokens / 1000000).toFixed(1)}M`
  if (tokens >= 1000) return `${(tokens / 1000).toFixed(0)}k`
  return `${tokens}`
}

// Helper to format cost
const formatCost = (cost?: number): string => {
  if (cost === undefined || cost === null) return ''
  return `$${cost.toFixed(2)}`
}

// Helper to get options summary
const getOptionsSummary = (options?: Record<string, unknown>): string => {
  if (!options || Object.keys(options).length === 0) return ''
  const parts: string[] = []
  if (options.reasoning_effort) parts.push(`Reasoning: ${options.reasoning_effort}`)
  if (options.thinking_level) parts.push(`Thinking: ${options.thinking_level}`)
  if (options.thinking_budget) parts.push(`Budget: ${options.thinking_budget}`)
  return parts.join(' • ')
}

interface LibraryTabProps {
  onSelect?: (llm: SavedLLM) => void
}

export function LibraryTab({ onSelect }: LibraryTabProps) {
  const { savedLLMs, deleteSavedLLM, agentConfig } = useLLMStore()
  const [metadataMap, setMetadataMap] = useState<Record<string, ModelMetadata>>({})

  // Check if a saved LLM matches the current primary
  const isPrimary = (llm: SavedLLM): boolean => {
    if (!agentConfig?.primary) return false
    return agentConfig.primary.provider === llm.provider &&
           agentConfig.primary.model_id === llm.model_id
  }

  // Fetch model metadata for costs and context window
  useEffect(() => {
    const fetchMetadata = async () => {
      try {
        const response = await llmConfigService.getModelMetadata()
        const map: Record<string, ModelMetadata> = {}
        response.models.forEach(m => {
          map[m.model_id] = m
        })
        setMetadataMap(map)
      } catch (e) {
        console.warn('Failed to fetch model metadata:', e)
      }
    }
    fetchMetadata()
  }, [])

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">Published LLM</h3>
      </div>

      <div className="text-sm text-muted-foreground mb-4">
        Use Publish in Provider tabs to save configurations here. Select one as Primary LLM.
      </div>

      {/* List */}
      <div className="space-y-3">
        {savedLLMs.length === 0 ? (
          <div className="text-center p-8 text-muted-foreground bg-muted/30 rounded-lg">
            No published LLMs yet. Configure a model in Provider tabs and publish it here.
          </div>
        ) : (
          savedLLMs.map((llm) => {
            const metadata = metadataMap[llm.model_id]
            const optionsSummary = getOptionsSummary(llm.options)
            const apiKeyLast4 = llm.api_key && llm.api_key.length >= 4
              ? `...${llm.api_key.slice(-4)}`
              : null
            const isCurrentPrimary = isPrimary(llm)

            return (
              <Card key={llm.id} className={`p-3 ${isCurrentPrimary ? 'ring-2 ring-primary' : ''}`}>
                <div className="flex items-start justify-between gap-3">
                  <div className="flex-1 min-w-0">
                    {/* Name and Provider */}
                    <div className="font-medium flex items-center gap-2 flex-wrap">
                      <span className="truncate">{llm.name}</span>
                      {isCurrentPrimary && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-primary text-primary-foreground font-medium flex-shrink-0 flex items-center gap-1">
                          <CheckCircle className="w-3 h-3" />
                          Primary
                        </span>
                      )}
                      <span className="text-xs px-1.5 py-0.5 rounded bg-secondary text-secondary-foreground font-normal capitalize flex-shrink-0">
                        {llm.provider}
                      </span>
                      {apiKeyLast4 && (
                        <span className="text-xs px-1.5 py-0.5 rounded bg-muted text-muted-foreground font-mono flex-shrink-0" title="API Key (last 4 chars)">
                          🔑 {apiKeyLast4}
                        </span>
                      )}
                    </div>

                    {/* Model ID */}
                    <div className="text-xs text-muted-foreground truncate mt-0.5">
                      {llm.model_id}
                    </div>

                    {/* Metadata row: context, cost, temperature */}
                    <div className="flex flex-wrap items-center gap-2 mt-1.5 text-[11px] text-muted-foreground">
                      {metadata?.context_window && (
                        <span className="flex items-center gap-0.5" title="Context window">
                          <Box className="w-3 h-3" />
                          {formatContextWindow(metadata.context_window)}
                        </span>
                      )}
                      {metadata?.input_cost_per_1m !== undefined && (
                        <span className="flex items-center gap-0.5" title="Input cost per 1M tokens">
                          <DollarSign className="w-3 h-3" />
                          {formatCost(metadata.input_cost_per_1m)}/1M in
                        </span>
                      )}
                      {metadata?.output_cost_per_1m !== undefined && (
                        <span title="Output cost per 1M tokens">
                          {formatCost(metadata.output_cost_per_1m)}/1M out
                        </span>
                      )}
                      {llm.temperature !== undefined && (
                        <span className="flex items-center gap-0.5" title="Temperature">
                          <Thermometer className="w-3 h-3" />
                          {llm.temperature.toFixed(1)}
                        </span>
                      )}
                    </div>

                    {/* Options row: reasoning, thinking, etc. */}
                    {optionsSummary && (
                      <div className="text-[11px] text-primary/70 mt-1">
                        {optionsSummary}
                      </div>
                    )}
                  </div>

                  {/* Action buttons */}
                  <div className="flex flex-col gap-2 flex-shrink-0">
                    {onSelect && !isCurrentPrimary && (
                      <Button size="sm" variant="default" onClick={() => onSelect(llm)} className="whitespace-nowrap">
                        Set as Primary
                      </Button>
                    )}
                    <Button size="sm" variant="ghost" onClick={() => deleteSavedLLM(llm.id)} className="text-destructive hover:text-destructive hover:bg-destructive/10">
                      <Trash2 className="w-4 h-4" />
                    </Button>
                  </div>
                </div>
              </Card>
            )
          })
        )}
      </div>
    </div>
  )
}
