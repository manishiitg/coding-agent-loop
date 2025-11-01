import { useState, useEffect, useCallback } from 'react'
import { X, Settings, Key, CheckCircle, AlertCircle, Loader2, XCircle, Clock, Plus, Trash2 } from 'lucide-react'
import { Button } from './ui/Button'
import { Card } from './ui/Card'
import { TooltipProvider } from './ui/tooltip'
import { useLLMStore } from '../stores'
import type { LLMConfiguration, ExtendedLLMConfiguration } from '../services/api-types'
import { AnthropicSection } from './AnthropicSection'

interface LLMConfigurationModalProps {
  isOpen: boolean
  onClose: () => void
}

interface APIKeyStatus {
  openrouter: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  openai: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  bedrock: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  vertex: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
  anthropic: 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'
}

interface APIKeyError {
  openrouter: string | null
  openai: string | null
  bedrock: string | null
  vertex: string | null
  anthropic: string | null
}

export default function LLMConfigurationModal({ isOpen, onClose }: LLMConfigurationModalProps) {
  const {
    primaryConfig,
    openrouterConfig,
    bedrockConfig,
    openaiConfig,
    vertexConfig,
    anthropicConfig,
    availableBedrockModels,
    availableOpenRouterModels,
    availableOpenAIModels,
    availableVertexModels,
    availableAnthropicModels,
    setPrimaryConfig,
    setOpenrouterConfig,
    setBedrockConfig,
    setOpenaiConfig,
    setVertexConfig,
    setAnthropicConfig,
    testAPIKey,
    defaultsLoaded,
    loadDefaultsFromBackend
  } = useLLMStore()

  // Helper function to get available models for any provider
  const getAvailableModelsForProvider = (provider: 'openai' | 'bedrock' | 'openrouter' | 'vertex' | 'anthropic'): string[] => {
    switch (provider) {
      case 'openai':
        return availableOpenAIModels
      case 'bedrock':
        return availableBedrockModels
      case 'openrouter':
        return availableOpenRouterModels
      case 'vertex':
        return availableVertexModels
      case 'anthropic':
        return availableAnthropicModels
      default:
        return []
    }
  }

  const [apiKeyStatus, setApiKeyStatus] = useState<APIKeyStatus>({
    openrouter: 'idle',
    openai: 'idle',
    bedrock: 'idle',
    vertex: 'idle',
    anthropic: 'idle'
  })
  
  const [apiKeyErrors, setApiKeyErrors] = useState<APIKeyError>({
    openrouter: null,
    openai: null,
    bedrock: null,
    vertex: null,
    anthropic: null
  })

  const [activeTab, setActiveTab] = useState<'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'>('openrouter')
  const [isSaving, setIsSaving] = useState(false)
  const [saveStatus, setSaveStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')

  // Load defaults when modal opens
  useEffect(() => {
    if (isOpen && !defaultsLoaded) {
      loadDefaultsFromBackend()
    }
  }, [isOpen, defaultsLoaded, loadDefaultsFromBackend])

  // Handle API key testing
  const handleTestAPIKey = useCallback(async (provider: 'openrouter' | 'openai' | 'bedrock' | 'vertex' | 'anthropic', apiKey: string, modelId?: string) => {
    if (!apiKey.trim()) return

    setApiKeyStatus(prev => ({ ...prev, [provider]: 'testing' }))
    setApiKeyErrors(prev => ({ ...prev, [provider]: null }))
    
    try {
      const result = await testAPIKey(provider, apiKey, modelId)
      if (result.valid) {
        setApiKeyStatus(prev => ({ ...prev, [provider]: 'valid' }))
        setApiKeyErrors(prev => ({ ...prev, [provider]: null }))
      } else {
        setApiKeyStatus(prev => ({ ...prev, [provider]: 'invalid' }))
        setApiKeyErrors(prev => ({ ...prev, [provider]: result.error || 'API key validation failed' }))
      }
    } catch (err) {
      // Check if it's a timeout error
      if (err instanceof Error && err.message.includes('timeout')) {
        setApiKeyStatus(prev => ({ ...prev, [provider]: 'timeout' }))
        setApiKeyErrors(prev => ({ ...prev, [provider]: 'Request timed out. Please check your connection.' }))
      } else {
        setApiKeyStatus(prev => ({ ...prev, [provider]: 'invalid' }))
        setApiKeyErrors(prev => ({ ...prev, [provider]: err instanceof Error ? err.message : 'Unknown error occurred' }))
      }
    }
  }, [testAPIKey])

  // Auto-save indicator - show when changes are made (with debounce)
  useEffect(() => {
    if (saveStatus === 'idle') {
      // Debounce the auto-save indicator to prevent excessive messages
      const timeoutId = setTimeout(() => {
        setSaveStatus('saved')
        setTimeout(() => setSaveStatus('idle'), 2000) // Show for 2 seconds
      }, 500) // Wait 500ms before showing "saved"
      
      return () => clearTimeout(timeoutId)
    }
  }, [openrouterConfig, bedrockConfig, openaiConfig, vertexConfig, anthropicConfig, primaryConfig, saveStatus])

  // Handle Escape key to close modal
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        onClose()
      }
    }

    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown)
    }

    return () => {
      document.removeEventListener('keydown', handleKeyDown)
    }
  }, [isOpen, onClose])

  // Handle primary provider selection
  const handleSetPrimaryProvider = (provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic') => {
    let configToUse: ExtendedLLMConfiguration
    
    switch (provider) {
      case 'openrouter':
        configToUse = openrouterConfig
        break
      case 'bedrock':
        configToUse = bedrockConfig
        break
      case 'openai':
        configToUse = openaiConfig
        break
      case 'vertex':
        configToUse = vertexConfig
        break
      case 'anthropic':
        configToUse = anthropicConfig
        break
    }
    
    // Convert to LLMConfiguration for primary config
    const primaryConfig: LLMConfiguration = {
      provider: provider,
      model_id: configToUse.model_id,
      fallback_models: configToUse.fallback_models,
      cross_provider_fallback: configToUse.cross_provider_fallback
    }
    
    setPrimaryConfig(primaryConfig)
    
    // Refresh available LLMs to sync with ChatInput
    const { refreshAvailableLLMs } = useLLMStore.getState()
    refreshAvailableLLMs()
  }

  // Handle save configuration
  const handleSave = useCallback(async () => {
    setIsSaving(true)
    setSaveStatus('saving')
    
    try {
      // The Zustand store automatically persists to localStorage
      // We just need to simulate a save operation for user feedback
      await new Promise(resolve => setTimeout(resolve, 500)) // Simulate save delay
      
      setSaveStatus('saved')
      
      // Reset save status after 2 seconds
      setTimeout(() => {
        setSaveStatus('idle')
      }, 2000)
    } catch {
      setSaveStatus('error')
      setTimeout(() => {
        setSaveStatus('idle')
      }, 3000)
    } finally {
      setIsSaving(false)
    }
  }, [])

  if (!isOpen) return null

  return (
    <TooltipProvider>
      <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-2 sm:p-4">
        <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] flex flex-col">
          {/* Header */}
          <div className="flex items-center justify-between p-6 border-b border-border flex-shrink-0">
            <div className="flex items-center gap-3">
              <Settings className="w-6 h-6 text-primary" />
              <h2 className="text-xl font-semibold text-foreground">LLM Configuration</h2>
            </div>
            <Button
              variant="ghost"
              size="sm"
              onClick={onClose}
              className="h-8 w-8 p-0 hover:bg-secondary"
            >
              <X className="w-4 h-4" />
            </Button>
          </div>

          {/* Content */}
          <div className="flex flex-1 min-h-0">
            {/* Left Sidebar - Provider Tabs */}
            <div className="w-48 sm:w-64 border-r border-border bg-muted/30 p-3 sm:p-4 flex-shrink-0">
              <div className="space-y-2">
                <h3 className="text-sm font-medium text-muted-foreground mb-3">Providers</h3>
                
                {/* OpenRouter Tab */}
                <button
                  onClick={() => setActiveTab('openrouter')}
                  className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                    activeTab === 'openrouter'
                      ? 'bg-primary text-primary-foreground'
                      : 'hover:bg-secondary'
                  }`}
                >
                  <div className="flex-1">
                    <div className="font-medium">OpenRouter</div>
                    <div className="text-xs opacity-75">Multiple models</div>
                  </div>
                  {primaryConfig.provider === 'openrouter' && (
                    <CheckCircle className="w-4 h-4" />
                  )}
                </button>

                {/* Bedrock Tab */}
                <button
                  onClick={() => setActiveTab('bedrock')}
                  className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                    activeTab === 'bedrock'
                      ? 'bg-primary text-primary-foreground'
                      : 'hover:bg-secondary'
                  }`}
                >
                  <div className="flex-1">
                    <div className="font-medium">AWS Bedrock</div>
                    <div className="text-xs opacity-75">Claude models</div>
                  </div>
                  {primaryConfig.provider === 'bedrock' && (
                    <CheckCircle className="w-4 h-4" />
                  )}
                </button>

                {/* OpenAI Tab */}
                <button
                  onClick={() => setActiveTab('openai')}
                  className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                    activeTab === 'openai'
                      ? 'bg-primary text-primary-foreground'
                      : 'hover:bg-secondary'
                  }`}
                >
                  <div className="flex-1">
                    <div className="font-medium">OpenAI</div>
                    <div className="text-xs opacity-75">GPT models</div>
                  </div>
                  {primaryConfig.provider === 'openai' && (
                    <CheckCircle className="w-4 h-4" />
                  )}
                </button>

                {/* Vertex Tab */}
                <button
                  onClick={() => setActiveTab('vertex')}
                  className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                    activeTab === 'vertex'
                      ? 'bg-primary text-primary-foreground'
                      : 'hover:bg-secondary'
                  }`}
                >
                  <div className="flex-1">
                    <div className="font-medium">Vertex AI</div>
                    <div className="text-xs opacity-75">Gemini models</div>
                  </div>
                  {primaryConfig.provider === 'vertex' && (
                    <CheckCircle className="w-4 h-4" />
                  )}
                </button>

                {/* Anthropic Tab */}
                <button
                  onClick={() => setActiveTab('anthropic')}
                  className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                    activeTab === 'anthropic'
                      ? 'bg-primary text-primary-foreground'
                      : 'hover:bg-secondary'
                  }`}
                >
                  <div className="flex-1">
                    <div className="font-medium">Anthropic</div>
                    <div className="text-xs opacity-75">Claude models</div>
                  </div>
                  {primaryConfig.provider === 'anthropic' && (
                    <CheckCircle className="w-4 h-4" />
                  )}
                </button>
              </div>

              {/* Primary Provider Selection */}
              <div className="mt-6 pt-4 border-t border-border">
                <h3 className="text-sm font-medium text-muted-foreground mb-3">Primary Provider</h3>
                <div className="text-xs text-muted-foreground">
                  Current: <span className="font-mono">{primaryConfig.provider}</span>
                </div>
                <div className="text-xs text-muted-foreground">
                  Model: <span className="font-mono truncate">{primaryConfig.model_id}</span>
                </div>
              </div>
            </div>

            {/* Right Content - Provider Configuration */}
            <div className="flex-1 p-3 sm:p-6 overflow-y-auto min-h-0">
              {activeTab === 'openrouter' && (
            <OpenRouterSection
              config={openrouterConfig}
              onUpdate={setOpenrouterConfig}
              onTestAPIKey={(apiKey) => handleTestAPIKey('openrouter', apiKey)}
              apiKeyStatus={apiKeyStatus.openrouter}
              apiKeyError={apiKeyErrors.openrouter}
              isPrimary={primaryConfig.provider === 'openrouter'}
              onSetPrimary={() => handleSetPrimaryProvider('openrouter')}
              getAvailableModelsForProvider={getAvailableModelsForProvider}
              currentProvider="openrouter"
            />
              )}

              {activeTab === 'bedrock' && (
                <BedrockSection
                  config={bedrockConfig}
                  onUpdate={setBedrockConfig}
                  onTestAPIKey={(apiKey, modelId) => handleTestAPIKey('bedrock', apiKey, modelId)}
                  apiKeyStatus={apiKeyStatus}
                  apiKeyErrors={apiKeyErrors}
                  isPrimary={primaryConfig.provider === 'bedrock'}
                  onSetPrimary={() => handleSetPrimaryProvider('bedrock')}
                  getAvailableModelsForProvider={getAvailableModelsForProvider}
                  currentProvider="bedrock"
                />
              )}

              {activeTab === 'openai' && (
                <OpenAISection
                  config={openaiConfig}
                  onUpdate={setOpenaiConfig}
                  onTestAPIKey={(apiKey) => handleTestAPIKey('openai', apiKey)}
                  apiKeyStatus={apiKeyStatus.openai}
                  apiKeyError={apiKeyErrors.openai}
                  isPrimary={primaryConfig.provider === 'openai'}
                  onSetPrimary={() => handleSetPrimaryProvider('openai')}
                  getAvailableModelsForProvider={getAvailableModelsForProvider}
                  currentProvider="openai"
                />
              )}

              {activeTab === 'vertex' && (
                <VertexSection
                  config={vertexConfig}
                  onUpdate={setVertexConfig}
                  onTestAPIKey={(apiKey: string) => handleTestAPIKey('vertex', apiKey)}
                  apiKeyStatus={apiKeyStatus.vertex}
                  apiKeyError={apiKeyErrors.vertex}
                  isPrimary={primaryConfig.provider === 'vertex'}
                  onSetPrimary={() => handleSetPrimaryProvider('vertex')}
                  getAvailableModelsForProvider={getAvailableModelsForProvider}
                  currentProvider="vertex"
                />
              )}

              {activeTab === 'anthropic' && (
                <AnthropicSection
                  config={anthropicConfig}
                  onUpdate={setAnthropicConfig}
                  onTestAPIKey={(apiKey: string) => handleTestAPIKey('anthropic', apiKey)}
                  apiKeyStatus={apiKeyStatus.anthropic}
                  apiKeyError={apiKeyErrors.anthropic}
                  isPrimary={primaryConfig.provider === 'anthropic'}
                  onSetPrimary={() => handleSetPrimaryProvider('anthropic')}
                  getAvailableModelsForProvider={getAvailableModelsForProvider}
                  currentProvider="anthropic"
                />
              )}
            </div>
          </div>

          {/* Footer */}
          <div className="flex items-center justify-between p-3 sm:p-6 border-t border-border bg-muted/30 flex-shrink-0">
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              {saveStatus === 'saving' && (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  <span>Saving configuration...</span>
                </>
              )}
              {saveStatus === 'saved' && (
                <>
                  <CheckCircle className="w-4 h-4 text-green-600" />
                  <span className="text-green-600">Configuration saved!</span>
                </>
              )}
              {saveStatus === 'error' && (
                <>
                  <XCircle className="w-4 h-4 text-red-600" />
                  <span className="text-red-600">Save failed</span>
                </>
              )}
              {saveStatus === 'idle' && (
                <span>Changes are saved automatically. API keys are stored securely in your browser.</span>
              )}
            </div>
            <div className="flex items-center gap-2">
              <Button 
                variant="outline" 
                onClick={handleSave}
                disabled={isSaving}
                className="flex items-center gap-2"
              >
                {isSaving ? (
                  <>
                    <Loader2 className="w-4 h-4 animate-spin" />
                    Saving...
                  </>
                ) : saveStatus === 'saved' ? (
                  <>
                    <CheckCircle className="w-4 h-4" />
                    Saved
                  </>
                ) : (
                  <>
                    <Settings className="w-4 h-4" />
                    Save Configuration
                  </>
                )}
              </Button>
              <Button variant="outline" onClick={onClose}>
                Close
              </Button>
            </div>
          </div>
        </div>
      </div>
    </TooltipProvider>
  )
}

// Provider Section Component Props
interface ProviderSectionProps {
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

interface BedrockSectionProps {
  config: ExtendedLLMConfiguration
  onUpdate: (config: ExtendedLLMConfiguration) => void
  onTestAPIKey: (apiKey: string, modelId?: string) => void
  apiKeyStatus: APIKeyStatus
  apiKeyErrors: APIKeyError
  isPrimary: boolean
  onSetPrimary: () => void
  getAvailableModelsForProvider: (provider: 'openai' | 'bedrock' | 'openrouter' | 'vertex' | 'anthropic') => string[]
  currentProvider: 'openai' | 'bedrock' | 'openrouter' | 'vertex' | 'anthropic'
}

function OpenRouterSection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyError, isPrimary, onSetPrimary, getAvailableModelsForProvider, currentProvider }: ProviderSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  const [customModelInput, setCustomModelInput] = useState('')
  const [customModels, setCustomModels] = useState<string[]>(() => {
    const saved = localStorage.getItem('openrouter_custom_models')
    return saved ? JSON.parse(saved) : []
  })
  
  const { availableOpenRouterModels } = useLLMStore()

  // Update local state when config changes (e.g., when loaded from backend)
  useEffect(() => {
    if (config.api_key) {
      setApiKey(config.api_key)
    }
  }, [config.api_key])

  const handleAPIKeyChange = (newApiKey: string) => {
    setApiKey(newApiKey)
    onUpdate({ ...config, api_key: newApiKey })
  }

  const handleAddCustomModel = () => {
    const model = customModelInput.trim()
    if (!model || customModels.includes(model)) return

    if (!model.includes('/')) {
      alert('Model should be in format "provider/model-name"')
      return
    }

    const newCustomModels = [...customModels, model]
    setCustomModels(newCustomModels)
    localStorage.setItem('openrouter_custom_models', JSON.stringify(newCustomModels))
    setCustomModelInput('')
    
    // Refresh available LLMs in the store to sync with ChatInput
    const { refreshAvailableLLMs } = useLLMStore.getState()
    refreshAvailableLLMs()
  }

  const handleRemoveCustomModel = (model: string) => {
    const newCustomModels = customModels.filter(m => m !== model)
    setCustomModels(newCustomModels)
    localStorage.setItem('openrouter_custom_models', JSON.stringify(newCustomModels))
    
    // Refresh available LLMs in the store to sync with ChatInput
    const { refreshAvailableLLMs } = useLLMStore.getState()
    refreshAvailableLLMs()
  }

  const allModels = [
    ...availableOpenRouterModels,
    ...customModels
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">OpenRouter Configuration</h3>
        {!isPrimary && (
          <Button onClick={onSetPrimary} size="sm">
            Set as Primary
          </Button>
        )}
      </div>

      {/* API Key Section */}
      <Card className="p-4">
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Key className="w-4 h-4 text-muted-foreground" />
            <h4 className="font-medium text-foreground">API Key</h4>
          </div>
          
          {/* Show if API key is prefilled from environment */}
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
                placeholder="Enter your OpenRouter API key"
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
            
            {/* Show option to change key if prefilled */}
            {apiKey && (
              <div className="text-xs text-muted-foreground">
                <button
                  onClick={() => handleAPIKeyChange('')}
                  className="text-primary hover:underline"
                >
                  Clear and enter new key
                </button>
              </div>
            )}
            
            {apiKeyStatus === 'valid' && (
              <div className="text-sm text-green-600 dark:text-green-400 flex items-center gap-1">
                <CheckCircle className="w-4 h-4" />
                API key is valid
              </div>
            )}
            {apiKeyStatus === 'invalid' && (
              <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1">
                <AlertCircle className="w-4 h-4" />
                {apiKeyError || 'API key is invalid'}
              </div>
            )}
            {apiKeyStatus === 'timeout' && (
              <div className="text-sm text-yellow-600 dark:text-yellow-400 flex items-center gap-1">
                <AlertCircle className="w-4 h-4" />
                {apiKeyError || 'Validation timeout - check your connection'}
              </div>
            )}
          </div>
        </div>
      </Card>

      {/* Model Selection */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Model Selection</h4>
        
        <div className="space-y-3">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">
              Primary Model
            </label>
            <select
              value={config.model_id}
              onChange={(e) => onUpdate({ ...config, model_id: e.target.value })}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
            >
              {allModels.map((model) => (
                <option key={model} value={model}>
                  {model}
                </option>
              ))}
            </select>
          </div>

          {/* Custom Model Input */}
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">
              Add Custom Model
            </label>
            <div className="flex gap-2">
              <input
                type="text"
                value={customModelInput}
                onChange={(e) => setCustomModelInput(e.target.value)}
                placeholder="provider/model-name"
                className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
                onKeyPress={(e) => e.key === 'Enter' && handleAddCustomModel()}
              />
              <Button onClick={handleAddCustomModel} size="sm">
                Add
              </Button>
            </div>
            
            {/* Custom Models List */}
            {customModels.length > 0 && (
              <div className="mt-3">
                <div className="text-sm font-medium text-muted-foreground mb-2">Custom Models:</div>
                <div className="space-y-1 max-h-32 overflow-y-auto">
                  {customModels.map((model) => (
                    <div key={model} className="flex items-center justify-between bg-muted rounded-md px-3 py-2">
                      <span className="text-sm text-foreground truncate flex-1">{model}</span>
                      <Button
                        onClick={() => handleRemoveCustomModel(model)}
                        size="sm"
                        variant="ghost"
                        className="h-6 w-6 p-0 text-destructive hover:text-destructive"
                      >
                        <X className="w-3 h-3" />
                      </Button>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
      </Card>

      {/* Fallback Models */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Fallback Models</h4>
        <div className="space-y-2 max-h-32 overflow-y-auto">
          {allModels
            .filter(model => model !== config.model_id)
            .map((model) => (
              <label key={model} className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={config.fallback_models.includes(model)}
                  onChange={(e) => {
                    const newFallbacks = e.target.checked
                      ? [...config.fallback_models, model]
                      : config.fallback_models.filter(m => m !== model)
                    onUpdate({ ...config, fallback_models: newFallbacks })
                  }}
                  className="rounded border-border text-primary focus:ring-primary"
                />
                <span className="text-sm text-foreground">{model}</span>
              </label>
            ))}
        </div>
      </Card>

      {/* Cross-Provider Fallback */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Cross-Provider Fallback</h4>
        <div className="space-y-3">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">
              Fallback Provider
            </label>
            <select
              value={config.cross_provider_fallback?.provider || ''}
              onChange={(e) => {
                const fallbackProvider = e.target.value as 'openai' | 'bedrock' | 'openrouter' | 'vertex'
                if (fallbackProvider) {
                  // Get available models for the fallback provider
                  const fallbackModels = getAvailableModelsForProvider(fallbackProvider)
                  onUpdate({
                    ...config,
                    cross_provider_fallback: {
                      provider: fallbackProvider,
                      models: fallbackModels.length > 0 ? [fallbackModels[0]] : []
                    }
                  })
                } else {
                  onUpdate({ ...config, cross_provider_fallback: undefined })
                }
              }}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
            >
              <option value="">No cross-provider fallback</option>
              {currentProvider !== 'openai' && <option value="openai">OpenAI</option>}
              {currentProvider !== 'bedrock' && <option value="bedrock">AWS Bedrock</option>}
              {currentProvider !== 'openrouter' && <option value="openrouter">OpenRouter</option>}
              {currentProvider !== 'vertex' && <option value="vertex">Vertex AI</option>}
            </select>
          </div>

          {config.cross_provider_fallback && (
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-2">
                Fallback Models
              </label>
              <div className="space-y-2 max-h-32 overflow-y-auto">
                {getAvailableModelsForProvider(config.cross_provider_fallback.provider)
                  .map((model) => (
                    <label key={model} className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={config.cross_provider_fallback?.models.includes(model) || false}
                        onChange={(e) => {
                          const currentModels = config.cross_provider_fallback?.models || []
                          const newModels = e.target.checked
                            ? [...currentModels, model]
                            : currentModels.filter(m => m !== model)
                          onUpdate({
                            ...config,
                            cross_provider_fallback: {
                              ...config.cross_provider_fallback!,
                              models: newModels
                            }
                          })
                        }}
                        className="rounded border-border text-primary focus:ring-primary"
                      />
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

// Bedrock Section Component
function BedrockSection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyErrors, isPrimary, onSetPrimary, getAvailableModelsForProvider, currentProvider }: BedrockSectionProps) {
  const [region, setRegion] = useState(config.region || 'us-east-1')
  const [newCustomModel, setNewCustomModel] = useState('')
  
  const { customBedrockModels, addCustomBedrockModel, removeCustomBedrockModel, availableBedrockModels } = useLLMStore()

  const handleRegionChange = (newRegion: string) => {
    setRegion(newRegion)
    onUpdate({ ...config, region: newRegion })
  }

  const allModels = [...availableBedrockModels, ...customBedrockModels]
  
  const handleAddCustomModel = () => {
    if (newCustomModel.trim() && !allModels.includes(newCustomModel.trim())) {
      addCustomBedrockModel(newCustomModel.trim())
      setNewCustomModel('')
      
      // Refresh available LLMs in the store to sync with ChatInput
      const { refreshAvailableLLMs } = useLLMStore.getState()
      refreshAvailableLLMs()
    }
  }
  
  const handleRemoveCustomModel = (model: string) => {
    removeCustomBedrockModel(model)
    
    // Refresh available LLMs in the store to sync with ChatInput
    const { refreshAvailableLLMs } = useLLMStore.getState()
    refreshAvailableLLMs()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">AWS Bedrock Configuration</h3>
        {!isPrimary && (
          <Button onClick={onSetPrimary} size="sm">
            Set as Primary
          </Button>
        )}
      </div>

      {/* AWS Region */}
      <Card className="p-4">
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Key className="w-4 h-4 text-muted-foreground" />
            <h4 className="font-medium text-foreground">AWS Configuration</h4>
          </div>
          
          {/* Show if region is prefilled from environment */}
          {region && (
            <div className="text-sm text-blue-600 dark:text-blue-400 bg-blue-50 dark:bg-blue-900/20 p-2 rounded-md">
              <div className="flex items-center gap-2">
                <CheckCircle className="w-4 h-4" />
                <span>AWS region loaded from environment variables</span>
              </div>
            </div>
          )}
          
          <div className="space-y-2">
            <label className="block text-sm font-medium text-muted-foreground">
              AWS Region
            </label>
            <select
              value={region}
              onChange={(e) => handleRegionChange(e.target.value)}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
            >
              <option value="us-east-1">US East (N. Virginia)</option>
              <option value="us-west-2">US West (Oregon)</option>
              <option value="eu-west-1">Europe (Ireland)</option>
              <option value="ap-southeast-1">Asia Pacific (Singapore)</option>
            </select>
            <div className="text-xs text-muted-foreground">
              Uses AWS IAM roles for authentication. Make sure your AWS credentials are configured.
            </div>
          </div>
        </div>
      </Card>

      {/* Test AWS Credentials */}
      <Card className="p-4">
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Key className="w-4 h-4 text-muted-foreground" />
            <h4 className="font-medium text-foreground">Test AWS Credentials</h4>
          </div>
          
          <div className="flex items-center gap-3">
            <Button
              onClick={() => onTestAPIKey('test', config.model_id)}
              disabled={apiKeyStatus.bedrock === 'testing'}
              variant="outline"
              size="sm"
            >
              {apiKeyStatus.bedrock === 'testing' ? (
                <>
                  <Loader2 className="w-4 h-4 mr-2 animate-spin" />
                  Testing...
                </>
              ) : (
                <>
                  <CheckCircle className="w-4 h-4 mr-2" />
                  Test Credentials
                </>
              )}
            </Button>
            
            {apiKeyStatus.bedrock === 'valid' && (
              <div className="flex items-center gap-2 text-green-600 dark:text-green-400">
                <CheckCircle className="w-4 h-4" />
                <span className="text-sm">AWS credentials are valid</span>
              </div>
            )}
            
            {apiKeyStatus.bedrock === 'invalid' && (
              <div className="flex items-center gap-2 text-red-600 dark:text-red-400">
                <XCircle className="w-4 h-4" />
                <span className="text-sm">{apiKeyErrors.bedrock || 'AWS credentials are invalid'}</span>
              </div>
            )}
            
            {apiKeyStatus.bedrock === 'timeout' && (
              <div className="flex items-center gap-2 text-yellow-600 dark:text-yellow-400">
                <Clock className="w-4 h-4" />
                <span className="text-sm">{apiKeyErrors.bedrock || 'Test timed out'}</span>
              </div>
            )}
          </div>
          
          <div className="text-xs text-muted-foreground">
            Tests AWS credentials and Bedrock access using the selected model.
          </div>
        </div>
      </Card>

      {/* Model Selection */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Model Selection</h4>
        
        <div className="space-y-3">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">
              Primary Model
            </label>
            <select
              value={config.model_id}
              onChange={(e) => onUpdate({ ...config, model_id: e.target.value })}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
            >
              {allModels.map((model) => (
                <option key={model} value={model}>
                  {model}
                </option>
              ))}
            </select>
          </div>
        </div>
      </Card>

      {/* Fallback Models */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Fallback Models</h4>
        <div className="space-y-2 max-h-32 overflow-y-auto">
          {allModels
            .filter(model => model !== config.model_id)
            .map((model) => (
              <label key={model} className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={config.fallback_models.includes(model)}
                  onChange={(e) => {
                    const newFallbacks = e.target.checked
                      ? [...config.fallback_models, model]
                      : config.fallback_models.filter(m => m !== model)
                    onUpdate({ ...config, fallback_models: newFallbacks })
                  }}
                  className="rounded border-border text-primary focus:ring-primary"
                />
                <span className="text-sm text-foreground">{model}</span>
              </label>
            ))}
        </div>
      </Card>

      {/* Custom Models */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Custom Models</h4>
        
        {/* Add Custom Model */}
        <div className="space-y-3">
          <div className="flex gap-2">
            <input
              type="text"
              value={newCustomModel}
              onChange={(e) => setNewCustomModel(e.target.value)}
              placeholder="Enter custom model ID (e.g., claude-sonnet-4-5-20250929)"
              className="flex-1 px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
              onKeyPress={(e) => e.key === 'Enter' && handleAddCustomModel()}
            />
            <Button
              onClick={handleAddCustomModel}
              disabled={!newCustomModel.trim() || allModels.includes(newCustomModel.trim())}
              size="sm"
              variant="outline"
            >
              <Plus className="w-4 h-4" />
            </Button>
          </div>
          
          {/* Custom Models List */}
          {customBedrockModels.length > 0 && (
            <div className="space-y-2">
              <h5 className="text-sm font-medium text-muted-foreground">Custom Models:</h5>
              <div className="space-y-1">
                {customBedrockModels.map((model) => (
                  <div key={model} className="flex items-center justify-between bg-muted/50 p-2 rounded-md">
                    <span className="text-sm text-foreground font-mono">{model}</span>
                    <Button
                      onClick={() => handleRemoveCustomModel(model)}
                      size="sm"
                      variant="ghost"
                      className="text-red-600 hover:text-red-700 hover:bg-red-50 dark:text-red-400 dark:hover:text-red-300 dark:hover:bg-red-900/20"
                    >
                      <Trash2 className="w-4 h-4" />
                    </Button>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      </Card>

      {/* Cross-Provider Fallback */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Cross-Provider Fallback</h4>
        <div className="space-y-3">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">
              Fallback Provider
            </label>
            <select
              value={config.cross_provider_fallback?.provider || ''}
              onChange={(e) => {
                const fallbackProvider = e.target.value as 'openai' | 'bedrock' | 'openrouter' | 'vertex'
                if (fallbackProvider) {
                  // Get available models for the fallback provider
                  const fallbackModels = getAvailableModelsForProvider(fallbackProvider)
                  onUpdate({
                    ...config,
                    cross_provider_fallback: {
                      provider: fallbackProvider,
                      models: fallbackModels.length > 0 ? [fallbackModels[0]] : []
                    }
                  })
                } else {
                  onUpdate({ ...config, cross_provider_fallback: undefined })
                }
              }}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
            >
              <option value="">No cross-provider fallback</option>
              {currentProvider !== 'openai' && <option value="openai">OpenAI</option>}
              {currentProvider !== 'bedrock' && <option value="bedrock">AWS Bedrock</option>}
              {currentProvider !== 'openrouter' && <option value="openrouter">OpenRouter</option>}
              {currentProvider !== 'vertex' && <option value="vertex">Vertex AI</option>}
            </select>
          </div>

          {config.cross_provider_fallback && (
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-2">
                Fallback Models
              </label>
              <div className="space-y-2 max-h-32 overflow-y-auto">
                {getAvailableModelsForProvider(config.cross_provider_fallback.provider)
                  .map((model) => (
                    <label key={model} className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={config.cross_provider_fallback?.models.includes(model) || false}
                        onChange={(e) => {
                          const currentModels = config.cross_provider_fallback?.models || []
                          const newModels = e.target.checked
                            ? [...currentModels, model]
                            : currentModels.filter(m => m !== model)
                          onUpdate({
                            ...config,
                            cross_provider_fallback: {
                              ...config.cross_provider_fallback!,
                              models: newModels
                            }
                          })
                        }}
                        className="rounded border-border text-primary focus:ring-primary"
                      />
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

// OpenAI Section Component
function OpenAISection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyError, isPrimary, onSetPrimary, getAvailableModelsForProvider, currentProvider }: ProviderSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  
  const { availableOpenAIModels } = useLLMStore()

  // Update local state when config changes (e.g., when loaded from backend)
  useEffect(() => {
    if (config.api_key) {
      setApiKey(config.api_key)
    }
  }, [config.api_key])

  const handleAPIKeyChange = (newApiKey: string) => {
    setApiKey(newApiKey)
    onUpdate({ ...config, api_key: newApiKey })
  }

  const allModels = availableOpenAIModels

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">OpenAI Configuration</h3>
        {!isPrimary && (
          <Button onClick={onSetPrimary} size="sm">
            Set as Primary
          </Button>
        )}
      </div>

      {/* API Key Section */}
      <Card className="p-4">
        <div className="space-y-4">
          <div className="flex items-center gap-2">
            <Key className="w-4 h-4 text-muted-foreground" />
            <h4 className="font-medium text-foreground">API Key</h4>
          </div>
          
          {/* Show if API key is prefilled from environment */}
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
                placeholder="Enter your OpenAI API key"
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
            
            {/* Show option to change key if prefilled */}
            {apiKey && (
              <div className="text-xs text-muted-foreground">
                <button
                  onClick={() => handleAPIKeyChange('')}
                  className="text-primary hover:underline"
                >
                  Clear and enter new key
                </button>
              </div>
            )}
            
            {apiKeyStatus === 'valid' && (
              <div className="text-sm text-green-600 dark:text-green-400 flex items-center gap-1">
                <CheckCircle className="w-4 h-4" />
                API key is valid
              </div>
            )}
            {apiKeyStatus === 'invalid' && (
              <div className="text-sm text-red-600 dark:text-red-400 flex items-center gap-1">
                <AlertCircle className="w-4 h-4" />
                {apiKeyError || 'API key is invalid'}
              </div>
            )}
            {apiKeyStatus === 'timeout' && (
              <div className="text-sm text-yellow-600 dark:text-yellow-400 flex items-center gap-1">
                <AlertCircle className="w-4 h-4" />
                {apiKeyError || 'Validation timeout - check your connection'}
              </div>
            )}
          </div>
        </div>
      </Card>

      {/* Model Selection */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Model Selection</h4>
        
        <div className="space-y-3">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">
              Primary Model
            </label>
            <select
              value={config.model_id}
              onChange={(e) => onUpdate({ ...config, model_id: e.target.value })}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
            >
              {allModels.map((model) => (
                <option key={model} value={model}>
                  {model}
                </option>
              ))}
            </select>
          </div>
        </div>
      </Card>

      {/* Fallback Models */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Fallback Models</h4>
        <div className="space-y-2 max-h-32 overflow-y-auto">
          {allModels
            .filter(model => model !== config.model_id)
            .map((model) => (
              <label key={model} className="flex items-center gap-2">
                <input
                  type="checkbox"
                  checked={config.fallback_models.includes(model)}
                  onChange={(e) => {
                    const newFallbacks = e.target.checked
                      ? [...config.fallback_models, model]
                      : config.fallback_models.filter(m => m !== model)
                    onUpdate({ ...config, fallback_models: newFallbacks })
                  }}
                  className="rounded border-border text-primary focus:ring-primary"
                />
                <span className="text-sm text-foreground">{model}</span>
              </label>
            ))}
        </div>
      </Card>

      {/* Cross-Provider Fallback */}
      <Card className="p-4">
        <h4 className="font-medium text-foreground mb-4">Cross-Provider Fallback</h4>
        <div className="space-y-3">
          <div>
            <label className="block text-sm font-medium text-muted-foreground mb-2">
              Fallback Provider
            </label>
            <select
              value={config.cross_provider_fallback?.provider || ''}
              onChange={(e) => {
                const fallbackProvider = e.target.value as 'openai' | 'bedrock' | 'openrouter' | 'vertex'
                if (fallbackProvider) {
                  // Get available models for the fallback provider
                  const fallbackModels = getAvailableModelsForProvider(fallbackProvider)
                  onUpdate({
                    ...config,
                    cross_provider_fallback: {
                      provider: fallbackProvider,
                      models: fallbackModels.length > 0 ? [fallbackModels[0]] : []
                    }
                  })
                } else {
                  onUpdate({ ...config, cross_provider_fallback: undefined })
                }
              }}
              className="w-full px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
            >
              <option value="">No cross-provider fallback</option>
              {currentProvider !== 'openai' && <option value="openai">OpenAI</option>}
              {currentProvider !== 'bedrock' && <option value="bedrock">AWS Bedrock</option>}
              {currentProvider !== 'openrouter' && <option value="openrouter">OpenRouter</option>}
              {currentProvider !== 'vertex' && <option value="vertex">Vertex AI</option>}
            </select>
          </div>

          {config.cross_provider_fallback && (
            <div>
              <label className="block text-sm font-medium text-muted-foreground mb-2">
                Fallback Models
              </label>
              <div className="space-y-2 max-h-32 overflow-y-auto">
                {getAvailableModelsForProvider(config.cross_provider_fallback.provider)
                  .map((model) => (
                    <label key={model} className="flex items-center gap-2">
                      <input
                        type="checkbox"
                        checked={config.cross_provider_fallback?.models.includes(model) || false}
                        onChange={(e) => {
                          const currentModels = config.cross_provider_fallback?.models || []
                          const newModels = e.target.checked
                            ? [...currentModels, model]
                            : currentModels.filter(m => m !== model)
                          onUpdate({
                            ...config,
                            cross_provider_fallback: {
                              ...config.cross_provider_fallback!,
                              models: newModels
                            }
                          })
                        }}
                        className="rounded border-border text-primary focus:ring-primary"
                      />
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

// Vertex Section Component
function VertexSection({ config, onUpdate, onTestAPIKey, apiKeyStatus, apiKeyError, isPrimary, onSetPrimary, getAvailableModelsForProvider, currentProvider }: ProviderSectionProps) {
  const [apiKey, setApiKey] = useState(config.api_key || '')
  const { availableVertexModels } = useLLMStore()

  useEffect(() => {
    if (config.api_key) {
      setApiKey(config.api_key)
    }
  }, [config.api_key])

  const handleAPIKeyChange = (newApiKey: string) => {
    setApiKey(newApiKey)
    onUpdate({ ...config, api_key: newApiKey })
  }

  const allModels = availableVertexModels.length > 0 ? availableVertexModels : ['gemini-2.5-flash', 'gemini-2.5-pro']

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h3 className="text-lg font-semibold text-foreground">Vertex AI Configuration</h3>
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
                placeholder="Enter your Vertex AI API key"
                className="flex-1❇ px-3 py-2 border border-border rounded-md bg-background text-foreground focus:ring-2 focus:ring-primary focus:border-primary"
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
