import { useState, useEffect } from 'react'
import { Key, CheckCircle, AlertCircle, Loader2 } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import type { ExtendedLLMConfiguration } from '../services/api-types'

interface AnthropicSectionProps {
  config: ExtendedLLMConfiguration
  onUpdate: (config: ExtendedLLMConfiguration) => void
  onTestAPIKey: (apiKey: string, modelId?: string) => void
  apiKeyStatus: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  apiKeyError: string | null
  isPrimary: boolean
  onSetPrimary: () => void
  getAvailableModelsForProvider: (provider: 'openai' | 'bedrock' | 'openrouter' | 'vertex' | 'anthropic') => string[]
  currentProvider: 'openai' | 'bedrock' | 'openrouter' | 'vertex' | 'anthropic'
}

export function AnthropicSection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyError, isPrimary, onSetPrimary, getAvailableModelsForProvider, currentProvider }: AnthropicSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  const { availableAnthropicModels } = useLLMStore()

  useEffect(() => {
    if (config.api_key) {
      setApiKey(config.api_key)
    }
  }, [config.api_key])

  const handleAPIKeyChange = (newApiKey: string) => {
    setApiKey(newApiKey)
    onUpdate({ ...config, api_key: newApiKey })
  }

  const allModels = availableAnthropicModels.length > 0 ? availableAnthropicModels : ['claude-sonnet-4-5-20250929', 'claude-haiku-4-5-20251001']

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">Anthropic Configuration</h3>
        {!isPrimary && (
          <Button onClick={onSetPrimary} size="sm">Set as Primary</Button>
        )}
      </div>
      <Card className="p-4">
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Key className="w-4 h-4 text-muted-foreground" />
            <h4 className="font-medium text-foreground">API Key</h4>
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
              <input
                type="password"
                value={apiKey}
                onChange={(e) => handleAPIKeyChange(e.target.value)}
                placeholder="Enter your Anthropic API key"
                className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
              />
              <Button
                onClick={() => onTestAPIKey(apiKey)}
                disabled={!apiKey.trim() || apiKeyStatus === 'testing'}
                size="sm"
                variant="outline"
              >
                {apiKeyStatus === 'testing' ? (
                  <Loader2 className="w-4 h-4 animate-spin" />
                ) : apiKeyStatus === 'valid' ? (
                  <CheckCircle className="w-4 h-4 text-green-500" />
                ) : apiKeyStatus === 'invalid' ? (
                  <AlertCircle className="w-4 h-4 text-red-500" />
                ) : (
                  'Test'
                )}
              </Button>
            </div>
            {apiKey && (
              <div className="text-xs text-muted-foreground">
                <button onClick={() => handleAPIKeyChange('')} className="text-primary hover:underline">Clear and enter new key</button>
              </div>
            )}
            {apiKeyStatus === 'valid' && <div className="text-sm text-green-600 dark:text-green-400 flex items-center gap-1"><CheckCircle className="w-4 h-4" />API key is valid</div>}
            {apiKeyStatus === 'invalid' && <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1"><AlertCircle className="w-4 h-4" />{apiKeyError || 'API key is invalid'}</div>}
            {apiKeyStatus === 'timeout' && <div className="text-sm text-yellow-600 dark:text-yellow-400 flex items-center gap-1"><AlertCircle className="w-4 h-4" />{apiKeyError || 'Validation timeout - check your connection'}</div>}
          </div>
        </div>
      </Card>
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Model Selection</h4>
        <div className="space-y-3">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">Primary Model</label>
            <select value={config.model_id} onChange={(e) => onUpdate({ ...config, model_id: e.target.value })} className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary">
              {allModels.map((model) => <option key={model} value={model}>{model}</option>)}
            </select>
          </div>
        </div>
      </Card>
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Fallback Models</h4>
        <div className="space-y-2 max-h-32 overflow-y-auto">
          {allModels.filter(model => model !== config.model_id).map((model) => (
            <label key={model} className="flex items-center gap-2">
              <input type="checkbox" checked={config.fallback_models.includes(model)} onChange={(e) => {
                const newFallbacks = e.target.checked ? [...config.fallback_models, model] : config.fallback_models.filter(m => m !== model)
                onUpdate({ ...config, fallback_models: newFallbacks })
              }} className="rounded border-border text-primary focus:ring-primary" />
              <span className="text-sm text-foreground">{model}</span>
            </label>
          ))}
        </div>
      </Card>
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Cross-Provider Fallback</h4>
        <div className="space-y-3">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">Fallback Provider</label>
            <select value={config.cross_provider_fallback?.provider || ''} onChange={(e) => {
              const fallbackProvider = e.target.value as 'openai' | 'bedrock' | 'openrouter' | 'vertex' | 'anthropic'
              if (fallbackProvider) {
                const fallbackModels = getAvailableModelsForProvider(fallbackProvider)
                onUpdate({ ...config, cross_provider_fallback: { provider: fallbackProvider, models: fallbackModels.length > 0 ? [fallbackModels[0]] : [] } })
              } else {
                onUpdate({ ...config, cross_provider_fallback: undefined })
              }
            }} className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary">
              <option value="">No cross-provider fallback</option>
              {currentProvider !== 'openai' && <option value="openai">OpenAI</option>}
              {currentProvider !== 'bedrock' && <option value="bedrock">AWS Bedrock</option>}
              {currentProvider !== 'openrouter' && <option value="openrouter">OpenRouter</option>}
              {currentProvider !== 'vertex' && <option value="vertex">Vertex AI</option>}
              {currentProvider !== 'anthropic' && <option value="anthropic">Anthropic</option>}
            </select>
          </div>
          {config.cross_provider_fallback && (
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-2">Fallback Models</label>
              <div className="space-y-2 max-h-32 overflow-y-auto">
                {getAvailableModelsForProvider(config.cross_provider_fallback.provider).map((model) => (
                  <label key={model} className="flex items-center gap-2">
                    <input type="checkbox" checked={config.cross_provider_fallback?.models.includes(model) || false} onChange={(e) => {
                      const currentModels = config.cross_provider_fallback?.models || []
                      const newModels = e.target.checked ? [...currentModels, model] : currentModels.filter(m => m !== model)
                      onUpdate({ ...config, cross_provider_fallback: { ...config.cross_provider_fallback!, models: newModels } })
                    }} className="rounded border-border text-primary focus:ring-primary" />
                    <span className="text-sm text-foreground">{model}</span>
                  </label>
                ))}
              </div>
            </div>
          )}
        </div>
      </Card>
    </div>
  )
}

