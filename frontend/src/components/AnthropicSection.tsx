import { useState, useEffect } from 'react'
import { Key, CheckCircle, AlertCircle, Loader2, Plus, Save } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { useLLMStore } from '../stores'
import type { ExtendedLLMConfiguration, LLMProvider, FallbackModel, LLMOptions } from '../services/api-types'

// Provider badge colors
const getProviderColor = (provider: string) => {
  switch (provider) {
    case 'openrouter': return 'bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-200';
    case 'bedrock': return 'bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-200';
    case 'openai': return 'bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-200';
    case 'vertex': return 'bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-200';
    case 'anthropic': return 'bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-200';
    default: return 'bg-gray-100 text-gray-800 dark:bg-gray-900 dark:text-gray-200';
  }
};

interface AnthropicSectionProps {
  config: ExtendedLLMConfiguration
  onUpdate: (config: ExtendedLLMConfiguration) => void
  onTestAPIKey: (apiKey: string, modelId?: string) => void
  apiKeyStatus: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  apiKeyError: string | null
  isPrimary: boolean
  onSetPrimary: () => void
  getAvailableModelsForProvider: (provider: LLMProvider) => string[]
  currentProvider: LLMProvider
  onSaveAsConfig: (name: string, provider: LLMProvider, modelId: string, options?: LLMOptions) => void
}

export function AnthropicSection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyError, isPrimary, onSetPrimary, getAvailableModelsForProvider, onSaveAsConfig }: AnthropicSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  const [showAddFallback, setShowAddFallback] = useState(false)
  const [newFallbackProvider, setNewFallbackProvider] = useState<LLMProvider>('anthropic')
  const [newFallbackModel, setNewFallbackModel] = useState('')
  
  // Save as config state
  const [showSaveConfigInput, setShowSaveConfigInput] = useState(false)
  const [saveConfigName, setSaveConfigName] = useState('')
  
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
  
  // Handle save as config
  const handleSaveAsConfig = () => {
    if (!saveConfigName.trim() || !config.model_id) return
    onSaveAsConfig(saveConfigName.trim(), 'anthropic', config.model_id, config.options)
    setSaveConfigName('')
    setShowSaveConfigInput(false)
  }

  const allModels = availableAnthropicModels.length > 0 ? availableAnthropicModels : ['claude-sonnet-4-5-20250929', 'claude-haiku-4-5-20251001']
  const allProviders: LLMProvider[] = ['openrouter', 'bedrock', 'openai', 'vertex', 'anthropic']

  // Handle adding a new fallback model
  const handleAddFallback = () => {
    if (!newFallbackModel) return
    
    // Check if model already exists in fallbacks
    const exists = config.fallback_models.some(
      m => m.model_id === newFallbackModel && m.provider === newFallbackProvider
    )
    if (exists) {
      alert("This model is already in the fallback list!")
      return
    }
    
    const newFallback: FallbackModel = {
      model_id: newFallbackModel,
      provider: newFallbackProvider,
      priority: config.fallback_models.length + 1
    }
    
    onUpdate({
      ...config,
      fallback_models: [...config.fallback_models, newFallback]
    })
    
    setNewFallbackModel('')
    setShowAddFallback(false)
  }

  // Handle removing a fallback model
  const handleRemoveFallback = (modelId: string, provider: string) => {
    const newFallbacks = config.fallback_models
      .filter(m => !(m.model_id === modelId && m.provider === provider))
      .map((m, idx) => ({ ...m, priority: idx + 1 })) // Re-assign priorities
    
    onUpdate({ ...config, fallback_models: newFallbacks })
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">Anthropic Configuration</h3>
        {!isPrimary && (
          <Button onClick={onSetPrimary} size="sm">Set as Primary</Button>
        )}
      </div>

      {/* API Key Section */}
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
                onClick={() => onTestAPIKey(apiKey, config.model_id)}
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

      {/* Save as Configuration - Only show when API key is valid */}
      {apiKeyStatus === 'valid' && config.model_id && (
        <Card className="p-4 bg-green-50 dark:bg-green-900/20 border-green-200 dark:border-green-800">
          <div className="space-y-3">
            <div className="flex items-center gap-2">
              <Save className="w-4 h-4 text-green-600 dark:text-green-400" />
              <h4 className="font-medium text-green-800 dark:text-green-200">Save as Configuration</h4>
            </div>
            <p className="text-sm text-green-700 dark:text-green-300">
              API key validated! Save this configuration for use as primary or fallback.
            </p>
            {!showSaveConfigInput ? (
              <Button
                onClick={() => setShowSaveConfigInput(true)}
                size="sm"
                className="bg-green-600 hover:bg-green-700 text-white"
              >
                <Save className="w-4 h-4 mr-2" />
                Save Configuration
              </Button>
            ) : (
              <div className="flex items-center gap-2">
                <input
                  type="text"
                  value={saveConfigName}
                  onChange={(e) => setSaveConfigName(e.target.value)}
                  placeholder="Enter config name (e.g., 'Production Claude')"
                  className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
                  autoFocus
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') handleSaveAsConfig()
                    if (e.key === 'Escape') {
                      setShowSaveConfigInput(false)
                      setSaveConfigName('')
                    }
                  }}
                />
                <Button onClick={handleSaveAsConfig} disabled={!saveConfigName.trim()} size="sm">
                  Save
                </Button>
                <Button variant="outline" onClick={() => { setShowSaveConfigInput(false); setSaveConfigName('') }} size="sm">
                  Cancel
                </Button>
              </div>
            )}
            <p className="text-xs text-green-600 dark:text-green-400">
              Model: <span className="font-mono">{config.model_id}</span>
            </p>
          </div>
        </Card>
      )}

      {/* Model Selection */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Model Selection</h4>
        <div className="space-y-3">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">Primary Model</label>
            <select 
              value={config.model_id} 
              onChange={(e) => onUpdate({ ...config, model_id: e.target.value })} 
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
            >
              {allModels.map((model) => <option key={model} value={model}>{model}</option>)}
            </select>
          </div>
        </div>
      </Card>

      {/* Unified Fallback Models */}
      <Card className="p-4">
        <div className="flex items-center justify-between mb-4">
          <h4 className="font-medium text-foreground">Fallback Models (Priority Order)</h4>
          <Button
            onClick={() => setShowAddFallback(!showAddFallback)}
            size="sm"
            variant="outline"
          >
            <Plus className="w-4 h-4 mr-1" />
            {showAddFallback ? "Cancel" : "Add"}
          </Button>
        </div>
        
        {/* Add Fallback Form */}
        {showAddFallback && (
          <div className="mb-4 p-3 bg-muted rounded-md space-y-2">
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-1">Provider</label>
              <select
                value={newFallbackProvider}
                onChange={(e) => {
                  setNewFallbackProvider(e.target.value as LLMProvider)
                  setNewFallbackModel('')
                }}
                className="w-full px-2 py-1 border border-border rounded text-sm bg-background text-foreground"
              >
                {allProviders.map((provider) => (
                  <option key={provider} value={provider}>
                    {provider.charAt(0).toUpperCase() + provider.slice(1)}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-1">Model</label>
              <select
                value={newFallbackModel}
                onChange={(e) => setNewFallbackModel(e.target.value)}
                className="w-full px-2 py-1 border border-border rounded text-sm bg-background text-foreground"
              >
                <option value="">Select model...</option>
                {getAvailableModelsForProvider(newFallbackProvider).map((model) => (
                  <option key={model} value={model}>{model}</option>
                ))}
              </select>
            </div>
            <Button
              onClick={handleAddFallback}
              disabled={!newFallbackModel}
              size="sm"
              className="w-full"
            >
              Add Fallback
            </Button>
          </div>
        )}
        
        {/* Fallback Models List */}
        <div className="space-y-2 max-h-48 overflow-y-auto">
          {config.fallback_models.length === 0 ? (
            <p className="text-sm text-muted-foreground italic">No fallback models configured</p>
          ) : (
            config.fallback_models
              .sort((a, b) => a.priority - b.priority)
              .map((fallback, index) => (
                <div 
                  key={`${fallback.provider}-${fallback.model_id}`}
                  className="flex items-center justify-between bg-muted rounded-md px-3 py-2"
                >
                  <div className="flex items-center gap-2 flex-1 min-w-0">
                    <span className="text-xs font-medium text-muted-foreground w-4">{index + 1}.</span>
                    <span className={`text-xs px-1.5 py-0.5 rounded ${getProviderColor(fallback.provider)}`}>
                      {fallback.provider}
                    </span>
                    <span className="text-sm text-foreground truncate">{fallback.model_id}</span>
                  </div>
                  <Button
                    onClick={() => handleRemoveFallback(fallback.model_id, fallback.provider)}
                    size="sm"
                    variant="ghost"
                    className="h-6 w-6 p-0 text-destructive hover:text-destructive flex-shrink-0"
                  >
                    ×
                  </Button>
                </div>
              ))
          )}
        </div>
        <p className="text-xs text-muted-foreground mt-2">
          Fallback models from any provider. Priority 1 is tried first.
        </p>
      </Card>
    </div>
  )
}
