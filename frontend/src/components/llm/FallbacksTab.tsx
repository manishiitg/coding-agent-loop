import { useState } from 'react'
import { Plus, X, ArrowUp, ArrowDown } from 'lucide-react'
import { Button } from '../ui/Button'
import { Card } from '../ui/Card'
import type { AgentLLMConfiguration } from '../../services/api-types'
import type { ModelMetadata } from '../../services/llm-config-api'
import type { LLMOption } from '../../types/llm'
import { useLLMStore } from '../../stores'
import LLMSelectionDropdown from '../LLMSelectionDropdown'

interface FallbacksTabProps {
  config: AgentLLMConfiguration
  onUpdate: (config: AgentLLMConfiguration) => void
  metadata: ModelMetadata[]
  isLoadingMetadata: boolean
}

export function FallbacksTab({ config, onUpdate, metadata }: FallbacksTabProps) {
  const { savedLLMs } = useLLMStore()
  const [isAdding, setIsAdding] = useState(false)

  // Convert savedLLMs to LLMOptions, filtering out the primary and existing fallbacks
  const getAvailableFallbackOptions = (): LLMOption[] => {
    return savedLLMs
      .filter(llm => {
        // Filter out the primary model
        if (llm.provider === config.primary.provider && llm.model_id === config.primary.model_id) {
          return false
        }
        // Filter out models already in fallback chain
        const isAlreadyInFallbacks = config.fallbacks.some(
          fb => fb.provider === llm.provider && fb.model_id === llm.model_id
        )
        return !isAlreadyInFallbacks
      })
      .map(llm => {
        const meta = metadata.find(m => m.model_id === llm.model_id)
        return {
          provider: llm.provider,
          model: llm.model_id,
          label: llm.name,
          description: llm.model_id,
          temperature: llm.temperature,
          contextWindow: meta?.context_window,
          inputCostPer1M: meta?.input_cost_per_1m,
          outputCostPer1M: meta?.output_cost_per_1m,
          options: llm.options
        }
      })
  }

  const handleAddFallback = (llmOption: LLMOption) => {
    // Find the original savedLLM to get all its data
    const saved = savedLLMs.find(l => l.provider === llmOption.provider && l.model_id === llmOption.model)
    if (saved) {
      // eslint-disable-next-line @typescript-eslint/no-unused-vars
      const { id: _id, name: _name, created_at: _created_at, ...modelData } = saved
      const updatedFallbacks = [...config.fallbacks, modelData]
      onUpdate({ ...config, fallbacks: updatedFallbacks })
    }
    setIsAdding(false)
  }

  const handleRemoveFallback = (index: number) => {
    const updatedFallbacks = config.fallbacks.filter((_, i) => i !== index)
    onUpdate({ ...config, fallbacks: updatedFallbacks })
  }

  const handleMoveFallback = (index: number, direction: 'up' | 'down') => {
    if (direction === 'up' && index === 0) return
    if (direction === 'down' && index === config.fallbacks.length - 1) return

    const updatedFallbacks = [...config.fallbacks]
    const swapIndex = direction === 'up' ? index - 1 : index + 1

    const temp = updatedFallbacks[index]
    updatedFallbacks[index] = updatedFallbacks[swapIndex]
    updatedFallbacks[swapIndex] = temp

    onUpdate({ ...config, fallbacks: updatedFallbacks })
  }

  const getModelMetadata = (modelId: string) => {
    return metadata.find(m => m.model_id === modelId)
  }

  const availableFallbackOptions = getAvailableFallbackOptions()

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">Global Fallback Chain</h3>
      </div>

      <div className="text-sm text-muted-foreground mb-4">
        Define an ordered list of models to try if the primary model fails. The system will attempt each model in sequence.
      </div>

      {/* Primary Model Display */}
      <Card className="p-4 border-primary/20 bg-primary/5">
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <div className="text-xs font-medium text-primary uppercase tracking-wider mb-1">Primary Model</div>
            <div className="font-medium text-lg">{config.primary.model_id}</div>
            <div className="text-sm text-muted-foreground capitalize flex items-center gap-2">
              {config.primary.provider}
              {config.primary.temperature !== undefined && <span>• Temp: {config.primary.temperature}</span>}
            </div>
          </div>
          <div className="text-right text-xs text-muted-foreground">
            {getModelMetadata(config.primary.model_id) && (
              <>
                <div>Context: {(getModelMetadata(config.primary.model_id)!.context_window / 1000).toFixed(0)}k</div>
                <div>Input: ${getModelMetadata(config.primary.model_id)!.input_cost_per_1m.toFixed(2)}/1M</div>
              </>
            )}
          </div>
        </div>
      </Card>

      <div className="relative">
        <div className="absolute left-4 top-0 bottom-0 w-0.5 bg-border -z-10" />

        <div className="space-y-3">
          {config.fallbacks.map((fallback, index) => {
            const meta = getModelMetadata(fallback.model_id)
            // Find the name from savedLLMs if available
            const savedLLM = savedLLMs.find(l => l.provider === fallback.provider && l.model_id === fallback.model_id)

            return (
              <Card key={`${fallback.provider}-${fallback.model_id}-${index}`} className="p-3 ml-8 relative">
                <div className="absolute -left-4 top-1/2 w-4 h-0.5 bg-border" />
                <div className="absolute -left-[29px] top-1/2 -translate-y-1/2 w-6 h-6 rounded-full bg-background border border-border flex items-center justify-center text-xs font-medium text-muted-foreground">
                  {index + 1}
                </div>

                <div className="flex items-center gap-3">
                  <div className="flex-1">
                    <div className="font-medium">{savedLLM?.name || fallback.model_id}</div>
                    <div className="text-xs text-muted-foreground flex items-center gap-2">
                      <span className="capitalize">{fallback.provider}</span>
                      {savedLLM && savedLLM.name !== fallback.model_id && (
                        <span className="text-muted-foreground/70">• {fallback.model_id}</span>
                      )}
                      {fallback.temperature !== undefined && <span>• T: {fallback.temperature}</span>}
                      {meta && (
                        <>
                          <span>•</span>
                          <span>{meta.context_window >= 1000000 ? `${(meta.context_window / 1000000).toFixed(1)}M` : `${(meta.context_window / 1000).toFixed(0)}k`} ctx</span>
                          <span>•</span>
                          <span>${meta.input_cost_per_1m.toFixed(2)}/1M in</span>
                        </>
                      )}
                    </div>
                  </div>

                  <div className="flex items-center gap-1">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleMoveFallback(index, 'up')}
                      disabled={index === 0}
                      className="h-8 w-8 p-0"
                    >
                      <ArrowUp className="w-4 h-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleMoveFallback(index, 'down')}
                      disabled={index === config.fallbacks.length - 1}
                      className="h-8 w-8 p-0"
                    >
                      <ArrowDown className="w-4 h-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => handleRemoveFallback(index)}
                      className="h-8 w-8 p-0 text-destructive hover:text-destructive"
                    >
                      <X className="w-4 h-4" />
                    </Button>
                  </div>
                </div>
              </Card>
            )
          })}

          {config.fallbacks.length === 0 && (
            <div className="ml-8 p-4 text-center border border-dashed border-border rounded-lg text-sm text-muted-foreground">
              No fallback models configured. If the primary model fails, the request will fail immediately.
            </div>
          )}
        </div>
      </div>

      {/* Add Fallback Section */}
      {isAdding ? (
        <Card className="p-4 border-dashed">
          <div className="flex items-center justify-between mb-3">
            <h4 className="font-medium">Add Fallback from Published LLM</h4>
            <Button variant="ghost" size="sm" onClick={() => setIsAdding(false)} className="h-6 w-6 p-0">
              <X className="w-4 h-4" />
            </Button>
          </div>

          {availableFallbackOptions.length === 0 ? (
            <div className="text-sm text-muted-foreground text-center py-4 bg-muted/30 rounded-md">
              {savedLLMs.length === 0
                ? 'No published LLMs available. Configure and publish models in the Published LLM tab first.'
                : 'All published LLMs are already in use (as primary or fallbacks).'}
            </div>
          ) : (
            <div className="flex items-center gap-2">
              <LLMSelectionDropdown
                availableLLMs={availableFallbackOptions}
                selectedLLM={null}
                onLLMSelect={handleAddFallback}
                inModal={true}
                title="Select Fallback LLM"
              />
              <span className="text-sm text-muted-foreground">
                {availableFallbackOptions.length} available
              </span>
            </div>
          )}
        </Card>
      ) : (
        <Button
          variant="outline"
          className="w-full border-dashed flex items-center justify-center gap-2 py-6"
          onClick={() => setIsAdding(true)}
          disabled={availableFallbackOptions.length === 0}
        >
          <Plus className="w-4 h-4" />
          Add Fallback Model
          {availableFallbackOptions.length === 0 && savedLLMs.length > 0 && (
            <span className="text-xs text-muted-foreground ml-2">(all LLMs in use)</span>
          )}
        </Button>
      )}
    </div>
  )
}
