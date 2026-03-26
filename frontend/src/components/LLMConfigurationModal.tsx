import { useState, useEffect, useCallback, useMemo } from 'react'
import { X, Settings, Layers, Lock, Upload } from 'lucide-react'
import { Button } from './ui/Button'
import { TooltipProvider } from './ui/tooltip'
import { useLLMStore, useAppStore, useChatStore } from '../stores'
import type { LLMConfiguration, ExtendedLLMConfiguration, AgentLLMConfiguration, SavedLLM } from '../services/api-types'
import { AnthropicSection } from './AnthropicSection'
import { OpenRouterSection } from './OpenRouterSection'
import { BedrockSection } from './BedrockSection'
import { OpenAISection } from './OpenAISection'
import { VertexSection } from './VertexSection'
import { AzureSection } from './AzureSection'
import { ClaudeCodeSection } from './ClaudeCodeSection'
import { GeminiCLISection } from './GeminiCLISection'
import { CodexCLISection } from './CodexCLISection'
import { MiniMaxSection } from './MiniMaxSection'
import { MiniMaxCodingPlanSection } from './MiniMaxCodingPlanSection'
import { llmConfigService, type ModelMetadata } from '../services/llm-config-api'
import { FallbacksTab } from './llm/FallbacksTab'
import { LibraryTab } from './llm/LibraryTab'

interface LLMConfigurationModalProps {
  isOpen: boolean
  onClose: () => void
}

// Provider type for reuse
type ProviderType = 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli' | 'codex-cli' | 'minimax' | 'minimax-coding-plan'

// Providers that use API keys (excludes claude-code which uses local CLI)
type APIKeyProviderType = 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'minimax' | 'minimax-coding-plan'

// Tab type for the modal
type TabType = 'fallbacks' | 'library' | ProviderType

type APIKeyStatusValue = 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'

type APIKeyStatus = Record<APIKeyProviderType, APIKeyStatusValue>

type APIKeyError = Record<APIKeyProviderType, string | null>

/** Small button that saves all configured API keys to the server for scheduled runs. */
function SaveKeysToServerButton() {
  const [status, setStatus] = useState<'idle' | 'saving' | 'saved' | 'error'>('idle')

  const handleSave = async () => {
    setStatus('saving')
    try {
      const store = useLLMStore.getState()
      const { providerKeysApi } = await import('../api/scheduler')
      await providerKeysApi.save({
        openrouter: store.openrouterConfig?.api_key || undefined,
        openai: store.openaiConfig?.api_key || undefined,
        anthropic: store.anthropicConfig?.api_key || undefined,
        vertex: store.vertexConfig?.api_key || undefined,
        gemini_cli: store.geminiCliApiKey || undefined,
        minimax: store.minimaxConfig?.api_key || undefined,
        minimax_coding_plan: store.minimaxCodingPlanConfig?.api_key || undefined,
        bedrock: store.bedrockConfig?.region ? { region: store.bedrockConfig.region } : undefined,
        azure: store.azureConfig?.endpoint && store.azureConfig?.api_key
          ? {
              endpoint: store.azureConfig.endpoint,
              api_key: store.azureConfig.api_key,
              api_version: (store.azureConfig.options?.api_version as string) || undefined,
              region: store.azureConfig.region || undefined,
            }
          : undefined,
      })
      setStatus('saved')
      setTimeout(() => setStatus('idle'), 2000)
    } catch {
      setStatus('error')
      setTimeout(() => setStatus('idle'), 3000)
    }
  }

  return (
    <Button
      variant="outline"
      size="sm"
      onClick={handleSave}
      disabled={status === 'saving'}
      className="text-xs gap-1"
    >
      <Upload className="w-3 h-3" />
      {status === 'idle' && 'Save keys to server'}
      {status === 'saving' && 'Saving...'}
      {status === 'saved' && 'Saved!'}
      {status === 'error' && 'Failed'}
    </Button>
  )
}

export default function LLMConfigurationModal({ isOpen, onClose }: LLMConfigurationModalProps) {
  const activeTabId = useChatStore(state => state.activeTabId)
  const setTabConfig = useChatStore(state => state.setTabConfig)
  const getTabConfig = useChatStore(state => state.getTabConfig)

  // Get current mode from app store
  const agentMode = useAppStore(state => state.agentMode)
  // Map 'simple' to 'multi-agent' for our mode-specific configs
  const currentMode: 'multi-agent' | 'workflow' = agentMode === 'workflow' ? 'workflow' : 'multi-agent'

  const {
    // Legacy configs (kept for backward compatibility)
    setAgentConfig,
    setPrimaryConfig,
    // Mode-specific configs
    getConfigForMode,
    setChatPrimaryConfig,
    setChatAgentConfig,
    setWorkflowPrimaryConfig,
    setWorkflowAgentConfig,
    // Provider configs (shared across modes)
    openrouterConfig,
    bedrockConfig,
    openaiConfig,
    vertexConfig,
    anthropicConfig,
    azureConfig,
    minimaxConfig,
    minimaxCodingPlanConfig,
    setOpenrouterConfig,
    setBedrockConfig,
    setOpenaiConfig,
    setVertexConfig,
    setAnthropicConfig,
    setAzureConfig,
    setMinimaxConfig,
    setMinimaxCodingPlanConfig,
    testAPIKey,
    defaultsLoaded,
    loadDefaultsFromBackend,
    refreshAvailableLLMs,
    // Supported providers filter
    isProviderSupported,
    llmConfigLocked,
    lockedProviders
  } = useLLMStore()

  const isProviderLocked = (provider: ProviderType) =>
    lockedProviders.includes('all') || lockedProviders.includes(provider)

  // Get mode-specific configs
  const modeConfig = getConfigForMode(currentMode)
  const modePrimaryConfig = modeConfig.primaryConfig
  const modeAgentConfig = modeConfig.agentConfig

  // Mode-specific setters
  const setModePrimaryConfig = useCallback((config: LLMConfiguration) => {
    if (currentMode === 'workflow') {
      setWorkflowPrimaryConfig(config)
    } else {
      setChatPrimaryConfig(config)
    }
    // Also update legacy config for backward compatibility
    setPrimaryConfig(config)
  }, [currentMode, setChatPrimaryConfig, setWorkflowPrimaryConfig, setPrimaryConfig])

  const setModeAgentConfig = useCallback((config: AgentLLMConfiguration | null) => {
    if (currentMode === 'workflow') {
      setWorkflowAgentConfig(config)
    } else {
      setChatAgentConfig(config)
    }
    // Also update legacy config for backward compatibility
    setAgentConfig(config)
  }, [currentMode, setChatAgentConfig, setWorkflowAgentConfig, setAgentConfig])

  // Provider config map for reducing duplication
  const providerConfigMap = useMemo(() => ({
    openrouter: { config: openrouterConfig, setConfig: setOpenrouterConfig },
    bedrock: { config: bedrockConfig, setConfig: setBedrockConfig },
    openai: { config: openaiConfig, setConfig: setOpenaiConfig },
    vertex: { config: vertexConfig, setConfig: setVertexConfig },
    anthropic: { config: anthropicConfig, setConfig: setAnthropicConfig },
    azure: { config: azureConfig, setConfig: setAzureConfig },
    minimax: { config: minimaxConfig, setConfig: setMinimaxConfig },
    'minimax-coding-plan': { config: minimaxCodingPlanConfig, setConfig: setMinimaxCodingPlanConfig }
  }), [openrouterConfig, bedrockConfig, openaiConfig, vertexConfig, anthropicConfig, azureConfig, minimaxConfig, minimaxCodingPlanConfig,
      setOpenrouterConfig, setBedrockConfig, setOpenaiConfig, setVertexConfig, setAnthropicConfig, setAzureConfig, setMinimaxConfig, setMinimaxCodingPlanConfig])

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

  // Initialize/Migrate agentConfig for current mode
  useEffect(() => {
    if (isOpen && !modeAgentConfig && modePrimaryConfig.provider && modePrimaryConfig.model_id) {
      const newConfig: AgentLLMConfiguration = {
        primary: {
          provider: modePrimaryConfig.provider,
          model_id: modePrimaryConfig.model_id,
        },
        fallbacks: []
      }

      // Migrate legacy fallbacks
      if (modePrimaryConfig.fallback_models) {
        modePrimaryConfig.fallback_models.forEach(modelId => {
          newConfig.fallbacks.push({
            provider: modePrimaryConfig.provider,
            model_id: modelId
          })
        })
      }

      if (modePrimaryConfig.cross_provider_fallback) {
        modePrimaryConfig.cross_provider_fallback.models.forEach(modelId => {
          newConfig.fallbacks.push({
            provider: modePrimaryConfig.cross_provider_fallback!.provider,
            model_id: modelId
          })
        })
      }

      setModeAgentConfig(newConfig)
    }
  }, [isOpen, modeAgentConfig, modePrimaryConfig, setModeAgentConfig])

  // Models are now accessed directly from metadata or store in each provider section

  const [apiKeyStatus, setApiKeyStatus] = useState<APIKeyStatus>({
    openrouter: 'idle',
    openai: 'idle',
    bedrock: 'idle',
    vertex: 'idle',
    anthropic: 'idle',
    azure: 'idle',
    minimax: 'idle',
    'minimax-coding-plan': 'idle'
  })

  const [apiKeyErrors, setApiKeyErrors] = useState<APIKeyError>({
    openrouter: null,
    openai: null,
    bedrock: null,
    vertex: null,
    anthropic: null,
    azure: null,
    minimax: null,
    'minimax-coding-plan': null
  })

  const [activeTab, setActiveTab] = useState<TabType>('library')

  // Load defaults when modal opens
  useEffect(() => {
    if (isOpen && !defaultsLoaded) {
      loadDefaultsFromBackend()
    }
  }, [isOpen, defaultsLoaded, loadDefaultsFromBackend])

  // Handle API key testing
  const handleTestAPIKey = useCallback(async (provider: 'openrouter' | 'openai' | 'bedrock' | 'vertex' | 'anthropic' | 'azure' | 'minimax' | 'minimax-coding-plan', apiKey: string, modelId?: string, options?: Record<string, unknown>, temperature?: number) => {
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

  // Sync primary config when provider config changes (mode-specific)
  const syncPrimaryConfig = useCallback((provider: ProviderType, config: ExtendedLLMConfiguration) => {
    // Also sync agentConfig primary (mode-specific)
    if (modeAgentConfig && modeAgentConfig.primary.provider === provider) {
      setModeAgentConfig({
        ...modeAgentConfig,
        primary: {
          ...modeAgentConfig.primary,
          model_id: config.model_id,
          options: config.options
        }
      })
    }

    if (modePrimaryConfig.provider === provider) {
      const updatedPrimaryConfig: LLMConfiguration = {
        provider: provider,
        model_id: config.model_id,
        fallback_models: config.fallback_models,
        cross_provider_fallback: config.cross_provider_fallback
      }
      setModePrimaryConfig(updatedPrimaryConfig)
    }
  }, [modeAgentConfig, modePrimaryConfig.provider, setModeAgentConfig, setModePrimaryConfig])

  // Generic handler for provider config updates
  const handleProviderConfigUpdate = useCallback((provider: APIKeyProviderType, config: ExtendedLLMConfiguration) => {
    providerConfigMap[provider].setConfig(config)
    syncPrimaryConfig(provider, config)
  }, [providerConfigMap, syncPrimaryConfig])

  // Handle setting primary provider (mode-specific)
  const handleSetPrimaryProvider = useCallback((provider: APIKeyProviderType) => {
    const configToUse = providerConfigMap[provider].config

    // Update mode-specific primary config
    const newPrimaryConfig: LLMConfiguration = {
      provider: provider,
      model_id: configToUse.model_id,
      fallback_models: configToUse.fallback_models,
      cross_provider_fallback: configToUse.cross_provider_fallback
    }
    setModePrimaryConfig(newPrimaryConfig)

    // Update mode-specific agent config
    if (modeAgentConfig) {
      setModeAgentConfig({
        ...modeAgentConfig,
        primary: {
          provider: provider,
          model_id: configToUse.model_id,
          options: configToUse.options
        }
      })
    }

    refreshAvailableLLMs()
  }, [providerConfigMap, modeAgentConfig, setModeAgentConfig, setModePrimaryConfig, refreshAvailableLLMs])

  // Handle library selection
  // Sync the active tab's llmConfig so ChatInput + sidebar pick up the change immediately
  const syncActiveTabLLM = useCallback((provider: string, model_id: string) => {
    if (!activeTabId) return
    const existing = getTabConfig(activeTabId)
    setTabConfig(activeTabId, {
      llmConfig: {
        ...(existing?.llmConfig ?? {}),
        provider,
        model_id,
      } as ExtendedLLMConfiguration,
    })
  }, [activeTabId, getTabConfig, setTabConfig])

  const handleLibrarySelect = useCallback((llm: SavedLLM) => {
    const provider = llm.provider

    // CLI-based providers don't use provider config map — set primary directly
    if (provider === 'claude-code' || provider === 'gemini-cli' || provider === 'codex-cli') {
      const newPrimaryConfig: LLMConfiguration = {
        provider,
        model_id: llm.model_id,
        fallback_models: [],
        cross_provider_fallback: undefined
      }
      setModePrimaryConfig(newPrimaryConfig)
      if (modeAgentConfig) {
        setModeAgentConfig({
          ...modeAgentConfig,
          primary: { provider, model_id: llm.model_id }
        })
      }
      syncActiveTabLLM(provider, llm.model_id)
      refreshAvailableLLMs()
      return
    }

    const { setConfig } = providerConfigMap[provider]

    setConfig({
      ...llm,
      api_key: llm.api_key || '',
      region: llm.region,
      fallback_models: [],
      cross_provider_fallback: undefined
    })
    handleSetPrimaryProvider(provider)
    syncActiveTabLLM(provider, llm.model_id)
  }, [providerConfigMap, handleSetPrimaryProvider, syncActiveTabLLM])

  if (!isOpen) return null

  if (llmConfigLocked) {
    return (
      <TooltipProvider>
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-2 sm:p-4">
          <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-md flex flex-col p-6">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-semibold text-foreground">LLM Configuration</h2>
              <Button variant="ghost" size="sm" onClick={onClose} className="h-8 w-8 p-0 hover:bg-secondary">
                <X className="w-4 h-4" />
              </Button>
            </div>
            <p className="text-muted-foreground">
              LLM settings are locked by admin. Contact your administrator to enable new LLMs or models.
            </p>
            {modePrimaryConfig?.provider && modePrimaryConfig?.model_id && (
              <p className="text-sm text-muted-foreground mt-3">
                Current: {modePrimaryConfig.provider} — {modePrimaryConfig.model_id}
              </p>
            )}
          </div>
        </div>
      </TooltipProvider>
    )
  }

  return (
    <TooltipProvider>
      <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-2 sm:p-4">
        <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl max-h-[90vh] flex flex-col">
          {/* Header */}
          <div className="flex items-center justify-between p-6 border-b border-border flex-shrink-0">
            <div className="flex items-center gap-3">
              <Settings className="w-6 h-6 text-primary" />
              <h2 className="text-xl font-semibold text-foreground">LLM Configuration</h2>
              <span className={`text-xs px-2 py-0.5 rounded-full ${
                currentMode === 'workflow'
                  ? 'bg-purple-500/20 text-purple-400'
                  : 'bg-blue-500/20 text-blue-400'
              }`}>
                {currentMode === 'workflow' ? 'Workflow' : 'Chat'}
              </span>
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
                {(['openrouter', 'bedrock', 'openai', 'vertex', 'anthropic', 'azure', 'minimax', 'minimax-coding-plan', 'claude-code', 'gemini-cli', 'codex-cli'] as const)
                  .filter(provider => {
                    const supported = isProviderSupported(provider)
                    console.log('[LLMModal] provider', provider, 'supported:', supported)
                    return supported
                  })
                  .map((provider) => (
                  <button
                    key={provider}
                    onClick={() => setActiveTab(provider as typeof activeTab)}
                    className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                      activeTab === provider ? 'bg-primary text-primary-foreground' : 'hover:bg-secondary'
                    }`}
                  >
                    <div className="flex-1">
                      <div className="font-medium capitalize">{provider === 'openrouter' ? 'OpenRouter' : provider === 'openai' ? 'OpenAI' : provider === 'azure' ? 'Azure AI' : provider === 'claude-code' ? 'Claude Code' : provider === 'gemini-cli' ? 'Gemini CLI' : provider === 'codex-cli' ? 'Codex CLI' : provider === 'minimax' ? 'MiniMax' : provider === 'minimax-coding-plan' ? 'MiniMax Coding Plan' : provider}</div>
                      <div className="text-xs opacity-75">
                        {isProviderLocked(provider) ? 'Configured by admin' : provider === 'bedrock' ? 'AWS IAM' : provider === 'azure' ? 'Endpoint + API Key' : provider === 'claude-code' ? 'Local CLI (no API key)' : provider === 'gemini-cli' ? 'Local CLI (no API key)' : provider === 'codex-cli' ? 'Local CLI (API key optional)' : provider === 'minimax-coding-plan' ? 'Coding Plan Key (sk-cp-)' : 'API Key'}
                      </div>
                    </div>
                    {isProviderLocked(provider) && <Lock className="w-4 h-4 opacity-60" />}
                  </button>
                ))}
              </div>
            </div>

            {/* Right Content */}
            <div className="flex-1 p-3 sm:p-6 overflow-y-auto min-h-0">
              {activeTab === 'fallbacks' && modeAgentConfig && (
                <FallbacksTab
                  config={modeAgentConfig}
                  onUpdate={setModeAgentConfig}
                  metadata={metadata}
                  isLoadingMetadata={isLoadingMetadata}
                />
              )}

              {activeTab === 'library' && (
                <LibraryTab onSelect={handleLibrarySelect} />
              )}

              {/* Locked provider read-only banner */}
              {activeTab !== 'fallbacks' && activeTab !== 'library' && activeTab !== 'claude-code' && activeTab !== 'gemini-cli' && activeTab !== 'codex-cli' && (activeTab as string) in providerConfigMap && isProviderLocked(activeTab) && (
                <div className="flex flex-col items-center justify-center h-full min-h-[300px] text-center px-6">
                  <Lock className="w-12 h-12 text-muted-foreground/50 mb-4" />
                  <h3 className="text-lg font-semibold text-foreground mb-2">Configured by admin</h3>
                  <p className="text-sm text-muted-foreground max-w-sm">
                    The API key for this provider is set server-side. Contact your administrator to change it.
                  </p>
                  {providerConfigMap[activeTab]?.config.model_id && (
                    <p className="text-sm text-muted-foreground mt-4">
                      Current model: <span className="font-mono text-foreground">{providerConfigMap[activeTab].config.model_id}</span>
                    </p>
                  )}
                </div>
              )}

              {/* Editable provider sections (only when not locked) */}
              {activeTab === 'openrouter' && !isProviderLocked('openrouter') && (
                <OpenRouterSection
                  config={openrouterConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('openrouter', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('openrouter', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.openrouter}
                  apiKeyError={apiKeyErrors.openrouter}
                  metadata={metadata}
                />
              )}

              {activeTab === 'bedrock' && !isProviderLocked('bedrock') && (
                <BedrockSection
                  config={bedrockConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('bedrock', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('bedrock', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.bedrock}
                  apiKeyError={apiKeyErrors.bedrock}
                  metadata={metadata}
                />
              )}

              {activeTab === 'openai' && !isProviderLocked('openai') && (
                <OpenAISection
                  config={openaiConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('openai', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('openai', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.openai}
                  apiKeyError={apiKeyErrors.openai}
                  metadata={metadata}
                />
              )}

              {activeTab === 'vertex' && !isProviderLocked('vertex') && (
                <VertexSection
                  config={vertexConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('vertex', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('vertex', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.vertex}
                  apiKeyError={apiKeyErrors.vertex}
                  metadata={metadata}
                />
              )}

              {activeTab === 'anthropic' && !isProviderLocked('anthropic') && (
                <AnthropicSection
                  config={anthropicConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('anthropic', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('anthropic', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.anthropic}
                  apiKeyError={apiKeyErrors.anthropic}
                  metadata={metadata}
                />
              )}

              {activeTab === 'azure' && !isProviderLocked('azure') && (
                <AzureSection
                  config={azureConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('azure', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('azure', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.azure}
                  apiKeyError={apiKeyErrors.azure}
                  metadata={metadata}
                />
              )}

              {activeTab === 'minimax' && !isProviderLocked('minimax') && (
                <MiniMaxSection
                  config={minimaxConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('minimax', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('minimax', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus.minimax}
                  apiKeyError={apiKeyErrors.minimax}
                  metadata={metadata}
                />
              )}

              {activeTab === 'minimax-coding-plan' && !isProviderLocked('minimax-coding-plan') && (
                <MiniMaxCodingPlanSection
                  config={minimaxCodingPlanConfig}
                  onUpdate={(config) => handleProviderConfigUpdate('minimax-coding-plan', config)}
                  onTestAPIKey={(apiKey, modelId, options, temperature) => handleTestAPIKey('minimax-coding-plan', apiKey, modelId, options, temperature)}
                  apiKeyStatus={apiKeyStatus['minimax-coding-plan']}
                  apiKeyError={apiKeyErrors['minimax-coding-plan']}
                  metadata={metadata}
                />
              )}

              {activeTab === 'claude-code' && (
                <ClaudeCodeSection />
              )}

              {activeTab === 'gemini-cli' && (
                <GeminiCLISection
                  onModelChange={(modelId) => {
                    if (modePrimaryConfig.provider === 'gemini-cli') {
                      const newPrimaryConfig: LLMConfiguration = {
                        provider: 'gemini-cli',
                        model_id: modelId,
                        fallback_models: modePrimaryConfig.fallback_models || [],
                        cross_provider_fallback: modePrimaryConfig.cross_provider_fallback
                      }
                      setModePrimaryConfig(newPrimaryConfig)
                      if (modeAgentConfig) {
                        setModeAgentConfig({
                          ...modeAgentConfig,
                          primary: { provider: 'gemini-cli', model_id: modelId, options: modeAgentConfig.primary.options }
                        })
                      }
                    }
                  }}
                />
              )}

              {activeTab === 'codex-cli' && (
                <CodexCLISection />
              )}
            </div>
          </div>

          {/* Footer */}
          <div className="flex items-center justify-between p-3 sm:p-6 border-t border-border bg-muted/30 flex-shrink-0">
            <div className="flex items-center gap-3">
              <div className="text-sm text-muted-foreground">
                Changes are saved automatically.
              </div>
            </div>
            <div className="flex items-center gap-2">
              <SaveKeysToServerButton />
              <Button variant="outline" onClick={onClose}>Close</Button>
            </div>
          </div>
        </div>
      </div>
    </TooltipProvider>
  )
}
