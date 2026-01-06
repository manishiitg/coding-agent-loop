import { useState, useEffect, useCallback, useMemo } from 'react'
import { X, Settings, Layers } from 'lucide-react'
import { Button } from './ui/Button'
import { TooltipProvider } from './ui/tooltip'
import { useLLMStore } from '../stores'
import type { LLMConfiguration, ExtendedLLMConfiguration, AgentLLMConfiguration, SavedLLM } from '../services/api-types'
import { AnthropicSection } from './AnthropicSection'
import { OpenRouterSection } from './OpenRouterSection'
import { BedrockSection } from './BedrockSection'
import { OpenAISection } from './OpenAISection'
import { VertexSection } from './VertexSection'
import { llmConfigService, type ModelMetadata } from '../services/llm-config-api'
import { FallbacksTab } from './llm/FallbacksTab'
import { LibraryTab } from './llm/LibraryTab'

interface LLMConfigurationModalProps {
  isOpen: boolean
  onClose: () => void
}

// Provider type for reuse
type ProviderType = 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'

// Tab type for the modal
type TabType = 'fallbacks' | 'library' | ProviderType

type APIKeyStatusValue = 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'

type APIKeyStatus = Record<ProviderType, APIKeyStatusValue>

type APIKeyError = Record<ProviderType, string | null>

export default function LLMConfigurationModal({ isOpen, onClose }: LLMConfigurationModalProps) {
  const {
    primaryConfig,
    agentConfig,
    setAgentConfig,
    openrouterConfig,
    bedrockConfig,
    openaiConfig,
    vertexConfig,
    anthropicConfig,
    setPrimaryConfig,
    setOpenrouterConfig,
    setBedrockConfig,
    setOpenaiConfig,
    setVertexConfig,
    setAnthropicConfig,
    testAPIKey,
    defaultsLoaded,
    loadDefaultsFromBackend,
    refreshAvailableLLMs
  } = useLLMStore()

  // Provider config map for reducing duplication
  const providerConfigMap = useMemo(() => ({
    openrouter: { config: openrouterConfig, setConfig: setOpenrouterConfig },
    bedrock: { config: bedrockConfig, setConfig: setBedrockConfig },
    openai: { config: openaiConfig, setConfig: setOpenaiConfig },
    vertex: { config: vertexConfig, setConfig: setVertexConfig },
    anthropic: { config: anthropicConfig, setConfig: setAnthropicConfig }
  }), [openrouterConfig, bedrockConfig, openaiConfig, vertexConfig, anthropicConfig,
      setOpenrouterConfig, setBedrockConfig, setOpenaiConfig, setVertexConfig, setAnthropicConfig])

  // Metadata state - Driven purely by backend
  const [metadata, setMetadata] = useState<ModelMetadata[]>([])
  const [isLoadingMetadata, setIsLoadingMetadata] = useState(false)

  // Fetch metadata on mount
  useEffect(() => {
    if (isOpen) {
      const fetchMetadata = async () => {
        setIsLoadingMetadata(true)
        try {
          const response = await llmConfigService.getModelMetadata()
          if (response.models && response.models.length > 0) {
            setMetadata(response.models)
          }
        } catch (err) {
          console.error('Failed to fetch model metadata', err)
        } finally {
          setIsLoadingMetadata(false)
        }
      }
      fetchMetadata()
    }
  }, [isOpen])

  // Initialize/Migrate agentConfig
  useEffect(() => {
    if (isOpen && !agentConfig && primaryConfig.provider && primaryConfig.model_id) {
      const newConfig: AgentLLMConfiguration = {
        primary: {
          provider: primaryConfig.provider,
          model_id: primaryConfig.model_id,
        },
        fallbacks: []
      }

      // Migrate legacy fallbacks
      if (primaryConfig.fallback_models) {
        primaryConfig.fallback_models.forEach(modelId => {
          newConfig.fallbacks.push({
            provider: primaryConfig.provider,
            model_id: modelId
          })
        })
      }

      if (primaryConfig.cross_provider_fallback) {
        primaryConfig.cross_provider_fallback.models.forEach(modelId => {
          newConfig.fallbacks.push({
            provider: primaryConfig.cross_provider_fallback!.provider,
            model_id: modelId
          })
        })
      }
      
      setAgentConfig(newConfig)
    }
  }, [isOpen, agentConfig, primaryConfig, setAgentConfig])

  // Models are now accessed directly from metadata or store in each provider section

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

  const [activeTab, setActiveTab] = useState<TabType>('library')

  // Load defaults when modal opens
  useEffect(() => {
    if (isOpen && !defaultsLoaded) {
      loadDefaultsFromBackend()
    }
  }, [isOpen, defaultsLoaded, loadDefaultsFromBackend])

  // Handle API key testing
  const handleTestAPIKey = useCallback(async (provider: 'openrouter' | 'openai' | 'bedrock' | 'vertex' | 'anthropic', apiKey: string, modelId?: string, options?: Record<string, unknown>, temperature?: number) => {
    // Allow testing without API key for Bedrock and Vertex (they support OAuth/credentials)
    if (provider !== 'bedrock' && provider !== 'vertex' && !apiKey.trim()) {
      return
    }

    setApiKeyStatus(prev => ({ ...prev, [provider]: 'testing' }))
    setApiKeyErrors(prev => ({ ...prev, [provider]: null }))
    
    // Merge temperature into options if provided
    const optionsWithTemp = temperature !== undefined 
      ? { ...options, temperature }
      : options
    
    try {
      const result = await testAPIKey(provider, apiKey, modelId, optionsWithTemp)
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

  // Handle Escape key
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && isOpen) {
        onClose()
      }
    }
    if (isOpen) document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [isOpen, onClose])

  // Sync primary config when provider config changes
  const syncPrimaryConfig = useCallback((provider: ProviderType, config: ExtendedLLMConfiguration) => {
    // Also sync agentConfig primary
    if (agentConfig && agentConfig.primary.provider === provider) {
      setAgentConfig({
        ...agentConfig,
        primary: {
          ...agentConfig.primary,
          model_id: config.model_id,
          options: config.options
        }
      })
    }

    if (primaryConfig.provider === provider) {
      const updatedPrimaryConfig: LLMConfiguration = {
        provider: provider,
        model_id: config.model_id,
        fallback_models: config.fallback_models,
        cross_provider_fallback: config.cross_provider_fallback
      }
      setPrimaryConfig(updatedPrimaryConfig)
    }
  }, [agentConfig, primaryConfig.provider, setAgentConfig, setPrimaryConfig])

  // Generic handler for provider config updates
  const handleProviderConfigUpdate = useCallback((provider: ProviderType, config: ExtendedLLMConfiguration) => {
    providerConfigMap[provider].setConfig(config)
    syncPrimaryConfig(provider, config)
  }, [providerConfigMap, syncPrimaryConfig])

  // Handle setting primary provider
  const handleSetPrimaryProvider = useCallback((provider: ProviderType) => {
    const configToUse = providerConfigMap[provider].config

    // Update Legacy Config
    const newPrimaryConfig: LLMConfiguration = {
      provider: provider,
      model_id: configToUse.model_id,
      fallback_models: configToUse.fallback_models,
      cross_provider_fallback: configToUse.cross_provider_fallback
    }
    setPrimaryConfig(newPrimaryConfig)

    // Update New Config
    if (agentConfig) {
      setAgentConfig({
        ...agentConfig,
        primary: {
          provider: provider,
          model_id: configToUse.model_id,
          options: configToUse.options
        }
      })
    }

    refreshAvailableLLMs()
  }, [providerConfigMap, agentConfig, setAgentConfig, setPrimaryConfig, refreshAvailableLLMs])

  // Handle library selection
  const handleLibrarySelect = useCallback((llm: SavedLLM) => {
    const provider = llm.provider
    const { setConfig } = providerConfigMap[provider]

    setConfig({
      ...llm,
      api_key: llm.api_key || '',
      region: llm.region,
      fallback_models: [],
      cross_provider_fallback: undefined
    })
    handleSetPrimaryProvider(provider)
  }, [providerConfigMap, handleSetPrimaryProvider])

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
            <Button variant="ghost" size="sm" onClick={onClose} className="h-8 w-8 p-0 hover:bg-secondary">
              <X className="w-4 h-4" />
            </Button>
          </div>

          {/* Content */}
          <div className="flex flex-1 min-h-0">
            {/* Left Sidebar */}
            <div className="w-48 sm:w-64 border-r border-border bg-muted/30 p-3 sm:p-4 flex-shrink-0 overflow-y-auto">
              <div className="space-y-2">
                <h3 className="text-sm font-medium text-muted-foreground mb-3">General</h3>

                <button
                  onClick={() => setActiveTab('library')}
                  className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                    activeTab === 'library' ? 'bg-primary text-primary-foreground' : 'hover:bg-secondary'
                  }`}
                >
                  <div className="flex-1">
                    <div className="font-medium">Published LLM</div>
                    <div className="text-xs opacity-75">Saved configurations</div>
                  </div>
                  <Settings className="w-4 h-4" />
                </button>

                <button
                  onClick={() => setActiveTab('fallbacks')}
                  className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                    activeTab === 'fallbacks' ? 'bg-primary text-primary-foreground' : 'hover:bg-secondary'
                  }`}
                >
                  <div className="flex-1">
                    <div className="font-medium">Global Fallbacks</div>
                    <div className="text-xs opacity-75">Chain configuration</div>
                  </div>
                  <Layers className="w-4 h-4" />
                </button>

                <h3 className="text-sm font-medium text-muted-foreground mb-3 mt-6">Providers</h3>
                {['openrouter', 'bedrock', 'openai', 'vertex', 'anthropic'].map((provider) => (
                  <button
                    key={provider}
                    onClick={() => setActiveTab(provider as typeof activeTab)}
                    className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                      activeTab === provider ? 'bg-primary text-primary-foreground' : 'hover:bg-secondary'
                    }`}
                  >
                    <div className="flex-1">
                      <div className="font-medium capitalize">{provider === 'openrouter' ? 'OpenRouter' : provider === 'openai' ? 'OpenAI' : provider}</div>
                      <div className="text-xs opacity-75">
                        {provider === 'bedrock' ? 'AWS IAM' : 'API Key'}
                      </div>
                    </div>
                  </button>
                ))}
              </div>
            </div>

            {/* Right Content */}
            <div className="flex-1 p-3 sm:p-6 overflow-y-auto min-h-0">
              {activeTab === 'fallbacks' && agentConfig && (
                <FallbacksTab
                  config={agentConfig}
                  onUpdate={setAgentConfig}
                  metadata={metadata}
                  isLoadingMetadata={isLoadingMetadata}
                />
              )}

              {activeTab === 'library' && (
                <LibraryTab onSelect={handleLibrarySelect} />
              )}

              {activeTab === 'openrouter' && (
                <OpenRouterSection
                  config={openrouterConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('openrouter', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('openrouter', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.openrouter}
                  apiKeyError={apiKeyErrors.openrouter}
                  isPrimary={primaryConfig.provider === 'openrouter'}
                  onSetPrimary={() => handleSetPrimaryProvider('openrouter')}
                  metadata={metadata}
                />
              )}

              {activeTab === 'bedrock' && (
                <BedrockSection
                  config={bedrockConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('bedrock', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('bedrock', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.bedrock}
                  apiKeyError={apiKeyErrors.bedrock}
                  isPrimary={primaryConfig.provider === 'bedrock'}
                  onSetPrimary={() => handleSetPrimaryProvider('bedrock')}
                  metadata={metadata}
                />
              )}

              {activeTab === 'openai' && (
                <OpenAISection
                  config={openaiConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('openai', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('openai', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.openai}
                  apiKeyError={apiKeyErrors.openai}
                  isPrimary={primaryConfig.provider === 'openai'}
                  onSetPrimary={() => handleSetPrimaryProvider('openai')}
                  metadata={metadata}
                />
              )}

              {activeTab === 'vertex' && (
                <VertexSection
                  config={vertexConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('vertex', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('vertex', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.vertex}
                  apiKeyError={apiKeyErrors.vertex}
                  isPrimary={primaryConfig.provider === 'vertex'}
                  onSetPrimary={() => handleSetPrimaryProvider('vertex')}
                  metadata={metadata}
                />
              )}

              {activeTab === 'anthropic' && (
                <AnthropicSection
                  config={anthropicConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('anthropic', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('anthropic', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.anthropic}
                  apiKeyError={apiKeyErrors.anthropic}
                  isPrimary={primaryConfig.provider === 'anthropic'}
                  onSetPrimary={() => handleSetPrimaryProvider('anthropic')}
                  metadata={metadata}
                />
              )}
            </div>
          </div>

          {/* Footer */}
          <div className="flex items-center justify-between p-3 sm:p-6 border-t border-border bg-muted/30 flex-shrink-0">
            <div className="text-sm text-muted-foreground">
              Changes are saved automatically.
            </div>
            <Button variant="outline" onClick={onClose}>Close</Button>
          </div>
        </div>
      </div>
    </TooltipProvider>
  )
}
