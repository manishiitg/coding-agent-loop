import { useState, useEffect, useCallback, useMemo } from 'react'
import { X, Settings, Lock, WandSparkles } from 'lucide-react'
import { Button } from './ui/Button'
import { TooltipProvider } from './ui/tooltip'
import { useLLMStore, useAppStore } from '../stores'
import type { LLMConfiguration, ExtendedLLMConfiguration, AgentLLMConfiguration, LLMProvider } from '../services/api-types'
import { CodingAgentSection } from './llm/CodingAgentSection'
import { APIProviderSection } from './llm/APIProviderSection'
import { APIKeyProviderSection } from './APIKeyProviderSection'
import { llmConfigService, type ModelMetadata, type ProviderManifestEntry } from '../services/llm-config-api'
import { LibraryTab } from './llm/LibraryTab'
import { getProviderDisplayInfo } from '../utils/llmDisplay'
import ModalPortal from './ui/ModalPortal'

interface LLMConfigurationModalProps {
  isOpen: boolean
  onClose: () => void
  onOpenDiscovery?: () => void
}

// Providers that use API keys in this modal (excludes local CLIs and hidden legacy chat providers)
type APIKeyProviderType = 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'minimax' | 'elevenlabs' | 'deepgram'

type APIKeyStatusValue = 'idle' | 'testing' | 'valid' | 'invalid' | 'timeout'

type APIKeyStatus = Record<APIKeyProviderType, APIKeyStatusValue>

type APIKeyError = Record<APIKeyProviderType, string | null>

type AudioProviderTab = 'audio-gemini' | 'audio-minimax'

type TabType = 'library' | LLMProvider | AudioProviderTab

const CHAT_CAPABILITIES = new Set(['chat', 'text'])
const AUDIO_CAPABILITIES = new Set(['text_to_speech', 'speech_to_text', 'generate_music', 'audio_generation', 'audio_transcription', 'music_generation'])
const HIDDEN_CHAT_PROVIDER_TABS = new Set<string>(['openrouter', 'z-ai', 'kimi', 'minimax', 'minimax-coding-plan'])
const isMiniMaxAudioModel = (modelId: string) => /^(speech|music|audio|voice)[-_]/i.test(modelId)
const API_KEY_PROVIDER_IDS = new Set<string>(['bedrock', 'openai', 'vertex', 'anthropic', 'azure', 'minimax', 'elevenlabs', 'deepgram'])

const FALLBACK_AUDIO_PROVIDER_ITEMS: Array<{
  tab: LLMProvider | AudioProviderTab
  provider: APIKeyProviderType
  name: string
  placeholder: string
}> = [
  {
    tab: 'audio-gemini',
    provider: 'vertex',
    name: 'Gemini',
    placeholder: 'Select a Gemini audio model',
  },
  {
    tab: 'audio-minimax',
    provider: 'minimax',
    name: 'MiniMax',
    placeholder: 'Select a MiniMax audio model',
  },
  {
    tab: 'elevenlabs',
    provider: 'elevenlabs',
    name: 'ElevenLabs',
    placeholder: 'Select an ElevenLabs media model',
  },
  {
    tab: 'deepgram',
    provider: 'deepgram',
    name: 'Deepgram',
    placeholder: 'Select a Deepgram media model',
  },
]

export default function LLMConfigurationModal({ isOpen, onClose, onOpenDiscovery }: LLMConfigurationModalProps) {
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
    bedrockConfig,
    openaiConfig,
    vertexConfig,
    anthropicConfig,
    azureConfig,
    minimaxConfig,
    elevenlabsConfig,
    deepgramConfig,
    availableVertexModels,
    availableMinimaxModels,
    availableElevenLabsModels,
    availableDeepgramModels,
    setBedrockConfig,
    setOpenaiConfig,
    setVertexConfig,
    setAnthropicConfig,
    setAzureConfig,
    setMinimaxConfig,
    setElevenlabsConfig,
    setDeepgramConfig,
    testAPIKey,
    defaultsLoaded,
    loadDefaultsFromBackend,
    // Supported providers filter
    isProviderSupported,
    llmConfigLocked,
    lockedProviders,
    providerManifest,
    providerManifestLoaded,
    loadProviderManifest,
  } = useLLMStore()

  const isProviderLocked = (provider: string) =>
    lockedProviders.includes('all') || lockedProviders.includes(provider)

  const getProviderForTab = (tab: TabType): APIKeyProviderType | null => {
    if (tab === 'audio-gemini') return 'vertex'
    if (tab === 'audio-minimax') return 'minimax'
    if (tab === 'library') return null
    const entry = providerManifest.find(provider => provider.id === tab)
    if (entry?.integration_kind === 'coding_agent') return null
    if (HIDDEN_CHAT_PROVIDER_TABS.has(tab)) return null
    if (!API_KEY_PROVIDER_IDS.has(tab)) return null
    return tab as APIKeyProviderType
  }

  const entryHasCapability = useCallback((entry: ProviderManifestEntry, capabilities: Set<string>) => {
    return (entry.capabilities || []).some(capability => capabilities.has(capability))
  }, [])

  const manifestProviderEntries = useMemo(() => (
    providerManifest.filter(entry => {
      if (HIDDEN_CHAT_PROVIDER_TABS.has(entry.id)) return false
      return isProviderSupported(entry.id as LLMProvider)
    })
  ), [isProviderSupported, providerManifest])

  const apiProviderEntries = useMemo(
    () => manifestProviderEntries.filter(entry =>
      entry.integration_kind === 'api_model' &&
      API_KEY_PROVIDER_IDS.has(entry.id) &&
      entryHasCapability(entry, CHAT_CAPABILITIES)
    ),
    [entryHasCapability, manifestProviderEntries]
  )

  const codingAgentProviderEntries = useMemo(
    () => manifestProviderEntries.filter(entry => entry.integration_kind === 'coding_agent'),
    [manifestProviderEntries]
  )

  const audioProviderItems = useMemo(() => {
    if (providerManifest.length === 0) {
      return FALLBACK_AUDIO_PROVIDER_ITEMS.filter(item => isProviderSupported(item.provider))
    }
    const items = manifestProviderEntries
      .filter(entry => API_KEY_PROVIDER_IDS.has(entry.id) && entryHasCapability(entry, AUDIO_CAPABILITIES))
      .map(entry => ({
        tab: entry.id === 'vertex' ? 'audio-gemini' as const : entry.id === 'minimax' ? 'audio-minimax' as const : entry.id as LLMProvider,
        provider: entry.id as APIKeyProviderType,
        name: entry.id === 'vertex' ? 'Gemini' : entry.display_name,
        placeholder: entry.id === 'vertex'
          ? 'Select a Gemini audio model'
          : entry.id === 'minimax'
            ? 'Select a MiniMax audio model'
            : `Select a ${entry.display_name} media model`,
      }))
    return items.length > 0 ? items : FALLBACK_AUDIO_PROVIDER_ITEMS.filter(item => isProviderSupported(item.provider))
  }, [entryHasCapability, isProviderSupported, manifestProviderEntries, providerManifest.length])

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
    bedrock: { config: bedrockConfig, setConfig: setBedrockConfig },
    openai: { config: openaiConfig, setConfig: setOpenaiConfig },
    vertex: { config: vertexConfig, setConfig: setVertexConfig },
    anthropic: { config: anthropicConfig, setConfig: setAnthropicConfig },
    azure: { config: azureConfig, setConfig: setAzureConfig },
    minimax: { config: minimaxConfig, setConfig: setMinimaxConfig },
    elevenlabs: { config: elevenlabsConfig, setConfig: setElevenlabsConfig },
    deepgram: { config: deepgramConfig, setConfig: setDeepgramConfig }
  }), [bedrockConfig, openaiConfig, vertexConfig, anthropicConfig, azureConfig, minimaxConfig, elevenlabsConfig, deepgramConfig,
      setBedrockConfig, setOpenaiConfig, setVertexConfig, setAnthropicConfig, setAzureConfig, setMinimaxConfig, setElevenlabsConfig, setDeepgramConfig])

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
    openai: 'idle',
    bedrock: 'idle',
    vertex: 'idle',
    anthropic: 'idle',
    azure: 'idle',
    minimax: 'idle',
    elevenlabs: 'idle',
    deepgram: 'idle'
  })

  const [apiKeyErrors, setApiKeyErrors] = useState<APIKeyError>({
    openai: null,
    bedrock: null,
    vertex: null,
    anthropic: null,
    azure: null,
    minimax: null,
    elevenlabs: null,
    deepgram: null
  })

  const [activeTab, setActiveTab] = useState<TabType>('library')

  // Load defaults and manifest when modal opens
  useEffect(() => {
    if (isOpen && !defaultsLoaded) {
      loadDefaultsFromBackend()
    }
    if (isOpen && !providerManifestLoaded) {
      loadProviderManifest()
    }
  }, [isOpen, defaultsLoaded, loadDefaultsFromBackend, providerManifestLoaded, loadProviderManifest])

  const getManifestEntry = useCallback((providerId: string): ProviderManifestEntry | undefined => {
    return providerManifest.find(p => p.id === providerId)
  }, [providerManifest])

  // Handle API key testing
  const handleTestAPIKey = useCallback(async (provider: APIKeyProviderType, apiKey: string, modelId?: string, options?: Record<string, unknown>) => {
    // Allow testing without API key for Bedrock and Vertex (they support OAuth/credentials)
    if (provider !== 'bedrock' && provider !== 'vertex' && !apiKey.trim()) {
      return
    }

    setApiKeyStatus(prev => ({ ...prev, [provider]: 'testing' }))
    setApiKeyErrors(prev => ({ ...prev, [provider]: null }))

    try {
      const result = await testAPIKey(provider, apiKey, modelId, options)
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
  const syncPrimaryConfig = useCallback((provider: LLMProvider, config: ExtendedLLMConfiguration) => {
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
      <ModalPortal>
      <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50 p-2 sm:p-4">
        <div className="bg-background border border-border rounded-lg shadow-xl w-full max-w-4xl max-h-[calc(100vh-2rem)] flex flex-col">
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
            <div className="flex items-center gap-2">
              {onOpenDiscovery && (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    onClose()
                    onOpenDiscovery()
                  }}
                  className="dark:border-slate-600 dark:bg-slate-800 dark:text-slate-100 dark:hover:bg-slate-700 dark:hover:text-white"
                >
                  <WandSparkles className="w-4 h-4 mr-2" />
                  Discover setup
                </Button>
              )}
              <Button variant="ghost" size="sm" onClick={onClose} className="h-8 w-8 p-0 hover:bg-secondary">
                <X className="w-4 h-4" />
              </Button>
            </div>
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

                {codingAgentProviderEntries.length > 0 && (
                  <>
                    <h3 className="text-sm font-medium text-muted-foreground mb-3 mt-6">Coding Agents</h3>
                    {codingAgentProviderEntries.map((entry) => (
                      <button
                        key={entry.id}
                        onClick={() => setActiveTab(entry.id as typeof activeTab)}
                        className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                          activeTab === entry.id ? 'bg-primary text-primary-foreground' : 'hover:bg-secondary'
                        }`}
                      >
                        <div className="flex-1">
                          <div className="font-medium">{entry.display_name}</div>
                          <div className="text-xs opacity-75">
                            {isProviderLocked(entry.id) ? 'Configured by admin' : entry.auth_description}
                          </div>
                        </div>
                        {isProviderLocked(entry.id) && <Lock className="w-4 h-4 opacity-60" />}
                      </button>
                    ))}
                  </>
                )}

                {apiProviderEntries.length > 0 && (
                  <>
                    <h3 className="text-sm font-medium text-muted-foreground mb-3 mt-6">API Providers</h3>
                    {apiProviderEntries.map((entry) => (
                      <button
                        key={entry.id}
                        onClick={() => setActiveTab(entry.id as typeof activeTab)}
                        className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                          activeTab === entry.id ? 'bg-primary text-primary-foreground' : 'hover:bg-secondary'
                        }`}
                      >
                        <div className="flex-1">
                          <div className="font-medium">{entry.display_name}</div>
                          <div className="text-xs opacity-75">
                            {isProviderLocked(entry.id) ? 'Configured by admin' : entry.auth_description}
                          </div>
                        </div>
                        {isProviderLocked(entry.id) && <Lock className="w-4 h-4 opacity-60" />}
                      </button>
                    ))}
                  </>
                )}

                {audioProviderItems.length > 0 && (
                  <>
                    <h3 className="text-sm font-medium text-muted-foreground mb-3 mt-6">Audio Providers</h3>
                    {audioProviderItems.map((item) => (
                      (() => {
                        const providerInfo = getProviderDisplayInfo(item.provider)
                        return (
                    <button
                      key={item.tab}
                      onClick={() => setActiveTab(item.tab)}
                      className={`w-full flex items-center gap-3 p-3 rounded-md text-left transition-colors ${
                        activeTab === item.tab ? 'bg-primary text-primary-foreground' : 'hover:bg-secondary'
                      }`}
                    >
                      <div className="flex-1">
                        <div className="font-medium">{item.name}</div>
                        <div className="text-xs opacity-75">
                          {isProviderLocked(item.provider) ? 'Configured by admin' : providerInfo.authDescription}
                        </div>
                      </div>
                      {isProviderLocked(item.provider) && <Lock className="w-4 h-4 opacity-60" />}
                    </button>
                        )
                      })()
                    ))}
                  </>
                )}
              </div>
            </div>

            {/* Right Content */}
            <div className="flex-1 p-3 sm:p-6 overflow-y-auto min-h-0">
              {activeTab === 'library' && (
                <LibraryTab />
              )}

              {/* Locked provider read-only banner */}
              {(() => {
                const lockedProvider = getProviderForTab(activeTab)
                if (!lockedProvider || !(lockedProvider in providerConfigMap) || !isProviderLocked(lockedProvider)) return null
                return (
                <div className="flex flex-col items-center justify-center h-full min-h-[300px] text-center px-6">
                  <Lock className="w-12 h-12 text-muted-foreground/50 mb-4" />
                  <h3 className="text-lg font-semibold text-foreground mb-2">Configured by admin</h3>
                  <p className="text-sm text-muted-foreground max-w-sm">
                    The API key for this provider is set server-side. Contact your administrator to change it.
                  </p>
                  {providerConfigMap[lockedProvider]?.config.model_id && (
                    <p className="text-sm text-muted-foreground mt-4">
                      Current model: <span className="font-mono text-foreground">{providerConfigMap[lockedProvider].config.model_id}</span>
                    </p>
                  )}
                </div>
                )
              })()}

              {/* Editable provider sections (only when not locked) */}
              {/* API provider sections — unified component driven by manifest */}
              {apiProviderEntries.some(entry => entry.id === activeTab) && !isProviderLocked(activeTab) && (() => {
                const entry = getManifestEntry(activeTab as string)
                const providerKey = activeTab as APIKeyProviderType
                const configEntry = providerConfigMap[providerKey]
                if (!entry || !configEntry) return <div className="text-sm text-muted-foreground py-8 text-center">Loading provider info...</div>
                return (
                  <APIProviderSection
                    provider={entry}
                    config={configEntry.config}
                    onUpdate={(config) => handleProviderConfigUpdate(providerKey, config)}
                    onTestAPIKey={(apiKey, modelId, options) => handleTestAPIKey(providerKey, apiKey, modelId, options)}
                    apiKeyStatus={apiKeyStatus[providerKey]}
                    apiKeyError={apiKeyErrors[providerKey]}
                    metadata={metadata}
                  />
                )
              })()}

              {activeTab === 'audio-gemini' && !isProviderLocked('vertex') && (
                <APIKeyProviderSection
                  provider="vertex"
                  providerLabel="Gemini Audio"
                  modelPlaceholder="Select a Gemini audio model"
                  publishErrorLabel="Gemini"
                  config={{
                    ...vertexConfig,
                    model_id: vertexConfig.model_id || 'gemini-3.1-flash-tts-preview'
                  }}
                  models={Array.from(new Set([
                    'gemini-3.1-flash-tts-preview',
                    ...(metadata?.filter(m => m.provider === 'vertex').map(m => m.model_id) || []),
                    ...availableVertexModels
                  ]))}
                  onUpdate={(config) => setVertexConfig(config)}
                  onTestAPIKey={(apiKey, modelId, options) => handleTestAPIKey('vertex', apiKey, modelId, options)}
                  apiKeyStatus={apiKeyStatus.vertex}
                  apiKeyError={apiKeyErrors.vertex}
                  metadata={metadata}
                  allowPublish={false}
                />
              )}

              {activeTab === 'audio-minimax' && !isProviderLocked('minimax') && (
                <APIKeyProviderSection
                  provider="minimax"
                  providerLabel="MiniMax Audio"
                  modelPlaceholder="Select a MiniMax audio model"
                  publishErrorLabel="MiniMax"
                  config={{
                    ...minimaxConfig,
                    model_id: minimaxConfig.model_id || 'speech-2.8-turbo'
                  }}
                  models={Array.from(new Set([
                    'speech-2.8-turbo',
                    'speech-2.8-hd',
                    'music-2.6',
                    'music-2.6-free',
                    ...(metadata?.filter(m => m.provider === 'minimax' && isMiniMaxAudioModel(m.model_id)).map(m => m.model_id) || []),
                    ...availableMinimaxModels.filter(isMiniMaxAudioModel)
                  ]))}
                  onUpdate={(config) => setMinimaxConfig(config)}
                  onTestAPIKey={(apiKey, modelId, options) => handleTestAPIKey('minimax', apiKey, modelId, options)}
                  apiKeyStatus={apiKeyStatus.minimax}
                  apiKeyError={apiKeyErrors.minimax}
                  metadata={metadata}
                  allowPublish={false}
                />
              )}

              {activeTab === 'elevenlabs' && !isProviderLocked('elevenlabs') && (
                <APIKeyProviderSection
                  provider="elevenlabs"
                  providerLabel="ElevenLabs"
                  modelPlaceholder="Select an ElevenLabs media model"
                  publishErrorLabel="ElevenLabs"
                  config={elevenlabsConfig}
                  models={Array.from(new Set([
                    ...(metadata?.filter(m => m.provider === 'elevenlabs').map(m => m.model_id) || []),
                    ...availableElevenLabsModels
                  ]))}
                  onUpdate={(config) => handleProviderConfigUpdate('elevenlabs', config)}
                  onTestAPIKey={(apiKey, modelId, options) => handleTestAPIKey('elevenlabs', apiKey, modelId, options)}
                  apiKeyStatus={apiKeyStatus.elevenlabs}
                  apiKeyError={apiKeyErrors.elevenlabs}
                  metadata={metadata}
                  allowPublish={false}
                />
              )}

              {activeTab === 'deepgram' && !isProviderLocked('deepgram') && (
                <APIKeyProviderSection
                  provider="deepgram"
                  providerLabel="Deepgram"
                  modelPlaceholder="Select a Deepgram media model"
                  publishErrorLabel="Deepgram"
                  config={deepgramConfig}
                  models={Array.from(new Set([
                    ...(metadata?.filter(m => m.provider === 'deepgram').map(m => m.model_id) || []),
                    ...availableDeepgramModels
                  ]))}
                  onUpdate={(config) => handleProviderConfigUpdate('deepgram', config)}
                  onTestAPIKey={(apiKey, modelId, options) => handleTestAPIKey('deepgram', apiKey, modelId, options)}
                  apiKeyStatus={apiKeyStatus.deepgram}
                  apiKeyError={apiKeyErrors.deepgram}
                  metadata={metadata}
                  allowPublish={false}
                />
              )}

              {/* Coding agent sections — unified component driven by manifest */}
              {codingAgentProviderEntries.some(entry => entry.id === activeTab) && (() => {
                const entry = getManifestEntry(activeTab as string)
                if (!entry) return <div className="text-sm text-muted-foreground py-8 text-center">Loading provider info...</div>
                return <CodingAgentSection provider={entry} />
              })()}
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
              <Button variant="outline" onClick={onClose}>Close</Button>
            </div>
          </div>
        </div>
      </div>
      </ModalPortal>
    </TooltipProvider>
  )
}
