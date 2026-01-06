import { useState, useEffect } from 'react'
import { Key, CheckCircle, AlertCircle, Loader2, BookOpen, X } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import type { ExtendedLLMConfiguration } from '../services/api-types'
import type { ModelMetadata } from '../services/llm-config-api'
import { ModelSelector } from './ui/ModelSelector'
import { ModelOptionsConfig } from './llm/ModelOptionsConfig'

interface OpenRouterSectionProps {
  config: ExtendedLLMConfiguration
  onUpdate: (config: ExtendedLLMConfiguration) => void
  onTestAPIKey: (apiKey: string, modelId?: string, options?: Record<string, unknown>, temperature?: number) => void
  apiKeyStatus: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  apiKeyError: string | null
  isPrimary: boolean
  onSetPrimary: () => void
  metadata?: ModelMetadata[]
}

export function OpenRouterSection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyError, isPrimary, onSetPrimary, metadata }: OpenRouterSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  const [customModelInput, setCustomModelInput] = useState('')
  const [customModels, setCustomModels] = useState<string[]>(() => {
    const saved = localStorage.getItem('openrouter_custom_models')
    return saved ? JSON.parse(saved) : []
  })
  const [isPublishing, setIsPublishing] = useState(false)
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [publishName, setPublishName] = useState('')
  const [publishError, setPublishError] = useState<string | null>(null)
  
  const { availableOpenRouterModels, refreshAvailableLLMs, saveLLM, testAPIKey: testAPIKeyFromStore } = useLLMStore()

  useEffect(() => {
    if (config.api_key) setApiKey(config.api_key)
  }, [config.api_key])

  const handleAPIKeyChange = (newApiKey: string) => {
    setApiKey(newApiKey)
    onUpdate({ ...config, api_key: newApiKey })
    setPublishError(null)
  }

  const handleAddCustomModel = () => {
    const model = customModelInput.trim()
    if (!model || customModels.includes(model)) return
    if (!model.includes('/')) { alert('Model should be in format "provider/model-name"'); return }
    const newCustomModels = [...customModels, model]
    setCustomModels(newCustomModels)
    localStorage.setItem('openrouter_custom_models', JSON.stringify(newCustomModels))
    setCustomModelInput('')
    refreshAvailableLLMs()
  }

  const handleRemoveCustomModel = (model: string) => {
    const newCustomModels = customModels.filter(m => m !== model)
    setCustomModels(newCustomModels)
    localStorage.setItem('openrouter_custom_models', JSON.stringify(newCustomModels))
    refreshAvailableLLMs()
  }

  const handleOptionsChange = (newOptions: Record<string, unknown>, newTemp?: number) => {
    onUpdate({ ...config, options: newOptions, temperature: newTemp })
  }

  const generateDefaultName = (): string => {
    if (!config.model_id) return ''
    
    const parts: string[] = []
    const currentModelMetadata = metadata?.find(m => m.model_id === config.model_id)
    
    // Add model ID (simplified - remove version dates if present, handle provider/model format)
    let modelId = config.model_id
    if (modelId.includes('/')) {
      // For OpenRouter format "provider/model", use just the model part
      modelId = modelId.split('/')[1]
    }
    modelId = modelId.replace(/-20\d{6}/g, '').replace(/-20\d{2}-\d{2}-\d{2}/g, '')
    parts.push(modelId)
    
    // Add temperature (use default 0 if not set, same as UI)
    const temp = config.temperature !== undefined ? config.temperature : 0
    parts.push(`temp${temp.toFixed(1)}`)
    
    // Add key options with defaults (same logic as ModelOptionsConfig)
    if (currentModelMetadata?.supports_thinking_level) {
      const thinkingLevel = (config.options?.thinking_level as string) || 'high'
      parts.push(`thinking-${thinkingLevel}`)
    }
    if (currentModelMetadata?.supports_reasoning_effort) {
      const reasoningEffort = (config.options?.reasoning_effort as string) || 'medium'
      parts.push(`reasoning-${reasoningEffort}`)
    }
    if (currentModelMetadata?.supports_thinking_budget) {
      const thinkingBudget = (config.options?.thinking_budget as number) || 1024
      parts.push(`budget-${thinkingBudget}`)
    }
    
    return parts.join('-')
  }

  const handlePublishToLibrary = async () => {
    if (!publishName.trim() || !config.model_id) return
    
    // Validate API key is required for OpenRouter
    if (!apiKey.trim()) {
      setPublishError('API key is required to publish OpenRouter models')
      return
    }

    setIsSubmitting(true)
    setPublishError(null)

    try {
      // Test API key before saving - merge temperature into options
      const optionsWithTemp = config.temperature !== undefined 
        ? { ...config.options, temperature: config.temperature }
        : config.options
      const testResult = await testAPIKeyFromStore('openrouter', apiKey, config.model_id, optionsWithTemp)
      
      if (testResult.valid) {
        const llmModel = {
          provider: 'openrouter' as const,
          model_id: config.model_id,
          api_key: config.api_key,
          options: config.options,
          temperature: config.temperature
        }
        
        const currentModelMetadata = metadata?.find(m => m.model_id === config.model_id)
        saveLLM(llmModel, publishName.trim(), currentModelMetadata?.model_name, 'api_key')
        setPublishName('')
        setIsPublishing(false)
        setIsSubmitting(false)
        // Also update the status in parent via callback
        await onTestAPIKey(apiKey, config.model_id, config.options, config.temperature)
      } else {
        setPublishError(testResult.error || 'API key validation failed. Please check your API key and try again.')
      }
    } catch (err) {
      setPublishError(err instanceof Error ? err.message : 'Failed to publish. Please try again.')
    } finally {
      setIsSubmitting(false)
    }
  }

  // Drive all models from metadata if available, otherwise fallback to store (from backend defaults)
  const modelsFromMetadata = metadata?.filter(m => m.provider === 'openrouter').map(m => m.model_id) || []
  const allModels = Array.from(new Set([...modelsFromMetadata, ...availableOpenRouterModels, ...customModels]))

  // Get current model metadata
  const currentModelMetadata = metadata?.find(m => m.model_id === config.model_id)

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">OpenRouter Configuration</h3>
      </div>
      
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Model Selection</h4>
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">Primary Model</label>
            <ModelSelector
              value={config.model_id}
              onChange={(val) => onUpdate({ ...config, model_id: val })}
              models={allModels}
              metadata={metadata || []}
              placeholder="Select an OpenRouter model"
            />
            <ModelOptionsConfig 
              metadata={currentModelMetadata} 
              options={config.options || {}} 
              temperature={config.temperature}
              onChange={handleOptionsChange}
            />
          </div>

          <div className="border-t border-border pt-4 space-y-3">
            <div className="flex items-center gap-2">
              <Key className="w-4 h-4 text-muted-foreground" />
              <h5 className="text-sm font-medium text-foreground">API Key</h5>
            </div>
            {apiKey && (
              <div className="text-sm text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 p-2 rounded-md">
                <div className="flex items-center gap-2">
                  <CheckCircle className="w-4 h-4" />
                  <span>API key loaded from environment variables</span>
                </div>
              </div>
            )}
            <div className="space-y-2">
              <div className="flex gap-2">
                <input type="password" value={apiKey} onChange={(e) => handleAPIKeyChange(e.target.value)} placeholder="Enter your OpenRouter API key" className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary" />
                <Button onClick={() => onTestAPIKey(apiKey, config.model_id, config.options, config.temperature)} disabled={!apiKey.trim() || apiKeyStatus === 'testing'} size="sm" variant="outline">
                  {apiKeyStatus === 'testing' ? <Loader2 className="w-4 h-4 animate-spin" /> : apiKeyStatus === 'valid' ? <CheckCircle className="w-4 h-4 text-green-500" /> : apiKeyStatus === 'invalid' ? <AlertCircle className="w-4 h-4 text-red-500" /> : 'Test'}
                </Button>
              </div>
              {apiKey && <div className="text-xs text-muted-foreground"><button onClick={() => handleAPIKeyChange('')} className="text-primary hover:underline">Clear and enter new key</button></div>}
              {apiKeyStatus === 'valid' && <div className="text-sm text-green-600 dark:text-green-400 flex items-center gap-1"><CheckCircle className="w-4 h-4" />API key is valid</div>}
              {apiKeyStatus === 'invalid' && <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1"><AlertCircle className="w-4 h-4" />{apiKeyError || 'API key is invalid'}</div>}
            </div>
          </div>

          <div className="border-t border-border pt-4">
            {isPublishing ? (
              <div className="space-y-3">
                <div className="flex gap-2">
                  <input
                    type="text"
                    value={publishName}
                    onChange={(e) => { setPublishName(e.target.value); setPublishError(null) }}
                    placeholder="Enter configuration name..."
                    className="flex-1 px-3 py-2 text-sm border border-border rounded-md bg-background text-foreground focus:ring-1 focus:ring-primary focus:border-primary"
                    autoFocus
                    onKeyPress={(e) => e.key === 'Enter' && handlePublishToLibrary()}
                  />
                  <Button onClick={handlePublishToLibrary} size="sm" disabled={!publishName.trim() || isSubmitting}>
                    {isSubmitting ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Save'}
                  </Button>
                  <Button onClick={() => { setIsPublishing(false); setPublishName(''); setPublishError(null) }} size="sm" variant="ghost" disabled={isSubmitting}>Cancel</Button>
                </div>
                {publishError && (
                  <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1">
                    <AlertCircle className="w-4 h-4" />
                    {publishError}
                  </div>
                )}
              </div>
            ) : (
              <Button 
                onClick={() => {
                  setPublishName(generateDefaultName())
                  setIsPublishing(true)
                }} 
                size="sm" 
                variant="outline"
                disabled={!config.model_id}
                className="w-full"
              >
                <BookOpen className="w-4 h-4 mr-2" />
                Publish
              </Button>
            )}
          </div>
        </div>
      </Card>

      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Custom Models</h4>
        <div className="space-y-3">
          <div className="flex gap-2">
            <input type="text" value={customModelInput} onChange={(e) => setCustomModelInput(e.target.value)} placeholder="provider/model-name" className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary" onKeyPress={(e) => e.key === 'Enter' && handleAddCustomModel()} />
            <Button onClick={handleAddCustomModel} size="sm">Add</Button>
          </div>
          {customModels.length > 0 && (
            <div>
              <div className="text-sm font-medium text-muted-foreground mb-2">Custom Models:</div>
              <div className="space-y-1 max-h-32 overflow-y-auto">
                {customModels.map((model) => (
                  <div key={model} className="flex items-center justify-between bg-muted rounded-md px-3 py-2">
                    <span className="text-sm text-foreground truncate flex-1">{model}</span>
                    <Button onClick={() => handleRemoveCustomModel(model)} size="sm" variant="ghost" className="h-6 w-6 p-0 text-destructive hover:text-destructive"><X className="w-3 h-3" /></Button>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </Card>
    </div>
  )
}
