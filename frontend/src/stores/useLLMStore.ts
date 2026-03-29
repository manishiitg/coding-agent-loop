import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { LLMConfiguration, ExtendedLLMConfiguration, APIKeyValidationRequest, AgentLLMConfiguration, SavedLLM, LLMModel, DelegationTierConfig } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import type { StoreActions } from './types'
import { llmConfigService } from '../services/llm-config-api'
import { agentApi } from '../services/api'

interface LLMState extends StoreActions {
  // Primary LLM configuration (unified from sidebar and chat input)
  // LEGACY: kept for backward compatibility, use mode-specific configs instead
  primaryConfig: LLMConfiguration

  // New unified configuration (Tiered Fallback System)
  // LEGACY: kept for backward compatibility, use mode-specific configs instead
  agentConfig: AgentLLMConfiguration | null

  // Mode-specific LLM configurations (multi-agent vs workflow)
  chatPrimaryConfig: LLMConfiguration
  chatAgentConfig: AgentLLMConfiguration | null
  workflowPrimaryConfig: LLMConfiguration
  workflowAgentConfig: AgentLLMConfiguration | null

  // Saved/Published LLM Library
  savedLLMs: SavedLLM[]
  
  // Provider-specific configurations with API keys
  openrouterConfig: ExtendedLLMConfiguration
  bedrockConfig: ExtendedLLMConfiguration
  openaiConfig: ExtendedLLMConfiguration
  vertexConfig: ExtendedLLMConfiguration
  anthropicConfig: ExtendedLLMConfiguration
  azureConfig: ExtendedLLMConfiguration
  minimaxConfig: ExtendedLLMConfiguration
  minimaxCodingPlanConfig: ExtendedLLMConfiguration

  // CLI provider API keys
  geminiCliApiKey: string
  setGeminiCliApiKey: (key: string) => void
  geminiCliModel: string
  setGeminiCliModel: (model: string) => void

  // Custom models for each provider
  customBedrockModels: string[]
  customOpenRouterModels: string[]
  customOpenAIModels: string[]
  customVertexModels: string[]
  customAzureModels: string[]
  customMinimaxModels: string[]
  customMinimaxCodingPlanModels: string[]

  // Available models from backend
  availableBedrockModels: string[]
  availableOpenRouterModels: string[]
  availableOpenAIModels: string[]
  availableVertexModels: string[]
  availableAnthropicModels: string[]
  availableAzureModels: string[]
  availableMinimaxModels: string[]
  availableMinimaxCodingPlanModels: string[]

  // Modal state
  showLLMModal: boolean
  
  // Available LLMs for selection
  availableLLMs: LLMOption[]
  
  // Loading and error states
  isLoadingLLMs: boolean
  error: string | null
  defaultsLoaded: boolean

  // Supported providers (from backend, not persisted)
  supportedProviders: ('openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli' | 'codex-cli' | 'minimax' | 'minimax-coding-plan')[]
  isProviderSupported: (provider: string) => boolean

  // Delegation tier configuration
  delegationTierConfig: DelegationTierConfig | null
  setDelegationTierConfig: (config: DelegationTierConfig | null) => void
  loadDelegationTierDefaults: () => Promise<void>

  // Lock state from backend (not persisted; re-read on each load)
  llmConfigLocked: boolean
  lockedProviders: string[]
  defaultPublishedLLMsLocked: boolean

  // Actions
  setPrimaryConfig: (config: LLMConfiguration) => void
  setAgentConfig: (config: AgentLLMConfiguration | null) => void

  // Mode-specific config actions
  setChatPrimaryConfig: (config: LLMConfiguration) => void
  setChatAgentConfig: (config: AgentLLMConfiguration | null) => void
  setWorkflowPrimaryConfig: (config: LLMConfiguration) => void
  setWorkflowAgentConfig: (config: AgentLLMConfiguration | null) => void
  getConfigForMode: (mode: 'multi-agent' | 'workflow') => { primaryConfig: LLMConfiguration; agentConfig: AgentLLMConfiguration | null }
  setOpenrouterConfig: (config: ExtendedLLMConfiguration) => void
  setBedrockConfig: (config: ExtendedLLMConfiguration) => void
  setOpenaiConfig: (config: ExtendedLLMConfiguration) => void
  setVertexConfig: (config: ExtendedLLMConfiguration) => void
  setAnthropicConfig: (config: ExtendedLLMConfiguration) => void
  setAzureConfig: (config: ExtendedLLMConfiguration) => void
  setMinimaxConfig: (config: ExtendedLLMConfiguration) => void
  setMinimaxCodingPlanConfig: (config: ExtendedLLMConfiguration) => void
  setShowLLMModal: (show: boolean) => void
  loadDefaultsFromBackend: () => Promise<void>
  
  // Library management
  saveLLM: (llm: LLMModel, name: string, modelName?: string, authMethod?: 'api_key' | 'oauth' | 'none') => void
  deleteSavedLLM: (id: string) => void

  // Custom model management
  addCustomBedrockModel: (model: string) => void
  removeCustomBedrockModel: (model: string) => void
  addCustomOpenRouterModel: (model: string) => void
  removeCustomOpenRouterModel: (model: string) => void
  addCustomOpenAIModel: (model: string) => void
  removeCustomOpenAIModel: (model: string) => void
  addCustomVertexModel: (model: string) => void
  removeCustomVertexModel: (model: string) => void
  addCustomAzureModel: (model: string) => void
  removeCustomAzureModel: (model: string) => void
  addCustomMinimaxModel: (model: string) => void
  removeCustomMinimaxModel: (model: string) => void
  addCustomMinimaxCodingPlanModel: (model: string) => void
  removeCustomMinimaxCodingPlanModel: (model: string) => void

  // Legacy actions (for backward compatibility)
  updateProvider: (provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'minimax' | 'minimax-coding-plan') => void
  updateModel: (modelId: string) => void
  updateFallbacks: (fallbacks: string[]) => void
  updateCrossProviderFallback: (fallback: LLMConfiguration['cross_provider_fallback']) => void
  refreshAvailableLLMs: () => Promise<void>
  
  // API key management
  testAPIKey: (provider: 'openrouter' | 'openai' | 'bedrock' | 'vertex' | 'anthropic' | 'azure' | 'minimax' | 'minimax-coding-plan', apiKey: string, modelId?: string, options?: Record<string, unknown>) => Promise<{valid: boolean, error: string | null, correctedOptions?: Record<string, unknown>}>
  
  // Helper methods
  getCurrentLLMOption: () => LLMOption | null
  isConfigValid: () => boolean
  checkModelExists: (modelId: string) => Promise<boolean>
}

export const useLLMStore = create<LLMState>()(
    persist(
      (set, get) => ({
        // Initial state - will be loaded from backend
        // LEGACY: kept for backward compatibility
        primaryConfig: {
          provider: 'openrouter',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined
        },

        agentConfig: null,

        // Mode-specific configs (initialized empty, will be migrated from legacy on first load)
        chatPrimaryConfig: {
          provider: 'openrouter',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined
        },
        chatAgentConfig: null,
        workflowPrimaryConfig: {
          provider: 'openrouter',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined
        },
        workflowAgentConfig: null,

        // Saved/Published LLM Library
        savedLLMs: [],
        
        // Provider-specific configurations - will be loaded from backend
        openrouterConfig: {
          provider: 'openrouter',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined,
          api_key: ''
        },
        bedrockConfig: {
          provider: 'bedrock',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined,
          region: 'us-east-1'
        },
        openaiConfig: {
          provider: 'openai',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined,
          api_key: ''
        },
        vertexConfig: {
          provider: 'vertex',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined,
          api_key: ''
        },
        anthropicConfig: {
          provider: 'anthropic',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined,
          api_key: ''
        },
        azureConfig: {
          provider: 'azure',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined,
          api_key: '',
          endpoint: ''
        },
        minimaxConfig: {
          provider: 'minimax',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined,
          api_key: ''
        },
        minimaxCodingPlanConfig: {
          provider: 'minimax-coding-plan',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined,
          api_key: ''
        },

        // CLI provider API keys
        geminiCliApiKey: '',
        setGeminiCliApiKey: (key) => {
          set({ geminiCliApiKey: key })
        },
        geminiCliModel: 'auto',
        setGeminiCliModel: (model) => {
          set({ geminiCliModel: model })
        },

        // Custom models for each provider
        customBedrockModels: [],
        customOpenRouterModels: [],
        customOpenAIModels: [],
        customVertexModels: [],
        customAzureModels: [],
        customMinimaxModels: [],
        customMinimaxCodingPlanModels: [],

        // Available models from backend
        availableBedrockModels: [],
        availableOpenRouterModels: [],
        availableOpenAIModels: [],
        availableVertexModels: [],
        availableAnthropicModels: [],
        availableAzureModels: [],
        availableMinimaxModels: [],
        availableMinimaxCodingPlanModels: [],

        // Modal state
        showLLMModal: false,
        
        availableLLMs: [],
        isLoadingLLMs: false,
        error: null,
        defaultsLoaded: false,

        // Delegation tier config
        delegationTierConfig: null,

        // Supported providers (always load fresh from backend, default to all)
        supportedProviders: ['openrouter', 'bedrock', 'openai', 'vertex', 'anthropic', 'azure', 'claude-code', 'gemini-cli', 'codex-cli', 'minimax', 'minimax-coding-plan'],
        llmConfigLocked: false,
        lockedProviders: [],
        defaultPublishedLLMsLocked: false,
        isProviderSupported: (provider) => {
          const supported = get().supportedProviders
          return supported.includes(provider as typeof supported[number])
        },

        // Actions
        setPrimaryConfig: (config) => {
          set({ primaryConfig: config, error: null })
        },

        setAgentConfig: (config) => {
          set({ agentConfig: config, error: null })
        },

        // Mode-specific config actions
        setChatPrimaryConfig: (config) => {
          set({ chatPrimaryConfig: config, error: null })
        },

        setChatAgentConfig: (config) => {
          set({ chatAgentConfig: config, error: null })
        },

        setWorkflowPrimaryConfig: (config) => {
          set({ workflowPrimaryConfig: config, error: null })
        },

        setWorkflowAgentConfig: (config) => {
          set({ workflowAgentConfig: config, error: null })
        },

        getConfigForMode: (mode) => {
          const state = get()
          if (mode === 'workflow') {
            return {
              primaryConfig: state.workflowPrimaryConfig,
              agentConfig: state.workflowAgentConfig
            }
          }
          return {
            primaryConfig: state.chatPrimaryConfig,
            agentConfig: state.chatAgentConfig
          }
        },

        setOpenrouterConfig: (config) => {
          set({ openrouterConfig: config, error: null })
        },

        setBedrockConfig: (config) => {
          set({ bedrockConfig: config, error: null })
        },

        setOpenaiConfig: (config) => {
          set({ openaiConfig: config, error: null })
        },

        setVertexConfig: (config) => {
          set({ vertexConfig: config, error: null })
        },

        setAnthropicConfig: (config) => {
          set({ anthropicConfig: config, error: null })
        },

        setAzureConfig: (config) => {
          set({ azureConfig: config, error: null })
        },

        setMinimaxConfig: (config) => {
          set({ minimaxConfig: config, error: null })
        },

        setMinimaxCodingPlanConfig: (config) => {
          set({ minimaxCodingPlanConfig: config, error: null })
        },

        setShowLLMModal: (show) => {
          set({ showLLMModal: show })
        },

        setDelegationTierConfig: (config) => {
          set({ delegationTierConfig: config })
          // Fire-and-forget sync to server so bot sessions can use it
          if (config) {
            // Collect API keys for each tier's provider so the server can use them
            const state = get()
            const providerKeys: Record<string, string> = {}
            const providerConfigs: Record<string, { api_key?: string }> = {
              openrouter: state.openrouterConfig,
              openai: state.openaiConfig,
              anthropic: state.anthropicConfig,
              vertex: state.vertexConfig,
              bedrock: state.bedrockConfig,
              azure: state.azureConfig,
              minimax: state.minimaxConfig,
              'minimax-coding-plan': state.minimaxCodingPlanConfig,
            }
            const tierConfig = config as Record<string, { provider?: string }>
            for (const tier of ['high', 'medium', 'low']) {
              const provider = tierConfig[tier]?.provider
              if (provider && providerConfigs[provider]?.api_key && !providerKeys[provider]) {
                providerKeys[provider] = providerConfigs[provider].api_key!
              }
            }
            // Also collect keys from custom tiers
            if (config.custom) {
              for (const slug of Object.keys(config.custom)) {
                const provider = config.custom[slug]?.provider
                if (provider && providerConfigs[provider]?.api_key && !providerKeys[provider]) {
                  providerKeys[provider] = providerConfigs[provider].api_key!
                }
              }
            }
            agentApi.saveDelegationTierConfig(
              config as Record<string, unknown>,
              Object.keys(providerKeys).length > 0 ? providerKeys : undefined
            ).catch(() => {})
          }
        },

        loadDelegationTierDefaults: async () => {
          try {
            // Try to load saved config from workspace file first
            try {
              const saved = await agentApi.getDelegationTierConfig()
              const hasSaved = saved && (saved.main || saved.high || saved.medium || saved.low ||
                (saved.custom && Object.keys(saved.custom as object).length > 0))
              if (hasSaved) {
                set({ delegationTierConfig: saved as DelegationTierConfig })
                return
              }
            } catch {
              // file not saved yet, fall through to env var defaults
            }
            // Fall back to env var defaults
            const defaults = await llmConfigService.getDelegationTierDefaults()
            const hasDefaults = defaults.main || defaults.high || defaults.medium || defaults.low ||
              (defaults.custom && Object.keys(defaults.custom).length > 0)
            if (hasDefaults) {
              set({ delegationTierConfig: defaults })
            }
          } catch (error) {
            console.warn('Failed to load delegation tier defaults:', error)
          }
        },

        // Library management
        saveLLM: (llm, name, modelName, authMethod) => {
          const { savedLLMs, refreshAvailableLLMs } = get()
          const newSavedLLM: SavedLLM = {
            ...llm,
            id: crypto.randomUUID(),
            name,
            model_name: modelName,
            auth_method: authMethod,
            created_at: new Date().toISOString()
          }
          set({ savedLLMs: [...savedLLMs, newSavedLLM] })
          // Auto-refresh availableLLMs when a new LLM is published
          refreshAvailableLLMs()
        },

        deleteSavedLLM: (id) => {
          const { savedLLMs, refreshAvailableLLMs } = get()
          set({ savedLLMs: savedLLMs.filter(llm => llm.id !== id) })
          // Auto-refresh availableLLMs when an LLM is deleted
          refreshAvailableLLMs()
        },

        // Custom model management
        addCustomBedrockModel: (model) => {
          const { customBedrockModels } = get()
          if (!customBedrockModels.includes(model)) {
            set({ customBedrockModels: [...customBedrockModels, model] })
          }
        }, 
        
        removeCustomBedrockModel: (model) => {
          const { customBedrockModels } = get()
          set({ customBedrockModels: customBedrockModels.filter(m => m !== model) })
        },
        
        addCustomOpenRouterModel: (model) => {
          const { customOpenRouterModels } = get()
          if (!customOpenRouterModels.includes(model)) {
            set({ customOpenRouterModels: [...customOpenRouterModels, model] })
          }
        },
        
        removeCustomOpenRouterModel: (model) => {
          const { customOpenRouterModels } = get()
          set({ customOpenRouterModels: customOpenRouterModels.filter(m => m !== model) })
        },
        
        addCustomOpenAIModel: (model) => {
          const { customOpenAIModels } = get()
          if (!customOpenAIModels.includes(model)) {
            set({ customOpenAIModels: [...customOpenAIModels, model] })
          }
        },
        
        removeCustomOpenAIModel: (model) => {
          const { customOpenAIModels } = get()
          set({ customOpenAIModels: customOpenAIModels.filter(m => m !== model) })
        },
        
        addCustomVertexModel: (model) => {
          const { customVertexModels } = get()
          if (!customVertexModels.includes(model)) {
            set({ customVertexModels: [...customVertexModels, model] })
          }
        },
        
        removeCustomVertexModel: (model) => {
          const { customVertexModels } = get()
          set({ customVertexModels: customVertexModels.filter(m => m !== model) })
        },

        addCustomAzureModel: (model) => {
          const { customAzureModels } = get()
          if (!customAzureModels.includes(model)) {
            set({ customAzureModels: [...customAzureModels, model] })
          }
        },

        removeCustomAzureModel: (model) => {
          const { customAzureModels } = get()
          set({ customAzureModels: customAzureModels.filter(m => m !== model) })
        },

        addCustomMinimaxModel: (model) => {
          const { customMinimaxModels } = get()
          if (!customMinimaxModels.includes(model)) {
            set({ customMinimaxModels: [...customMinimaxModels, model] })
          }
        },

        removeCustomMinimaxModel: (model) => {
          const { customMinimaxModels } = get()
          set({ customMinimaxModels: customMinimaxModels.filter(m => m !== model) })
        },

        addCustomMinimaxCodingPlanModel: (model) => {
          const { customMinimaxCodingPlanModels } = get()
          if (!customMinimaxCodingPlanModels.includes(model)) {
            set({ customMinimaxCodingPlanModels: [...customMinimaxCodingPlanModels, model] })
          }
        },

        removeCustomMinimaxCodingPlanModel: (model) => {
          const { customMinimaxCodingPlanModels } = get()
          set({ customMinimaxCodingPlanModels: customMinimaxCodingPlanModels.filter(m => m !== model) })
        },

        // Load defaults from backend
        loadDefaultsFromBackend: async () => {
          try {
            set({ isLoadingLLMs: true })
            const defaults = await llmConfigService.getLLMDefaults()
            
            // Get current state to check if user has already selected a model
            const currentState = get()
            // Check if user has made a selection (both provider and model_id should be set)
            const hasUserSelection = currentState.primaryConfig.provider && 
                                     currentState.primaryConfig.model_id && 
                                     currentState.primaryConfig.model_id.trim() !== ''
            
            // Preserve user configurations from current state (loaded from localStorage)
            // Merge backend defaults with saved config, prioritizing saved values
            const preserveUserConfig = (savedConfig: ExtendedLLMConfiguration, defaultConfig: ExtendedLLMConfiguration): ExtendedLLMConfiguration => {
              // Use saved config as base, only fill in missing fields from defaults
              // Check if savedConfig has meaningful values (not just initial empty state)
              const hasSavedModel = savedConfig?.model_id && savedConfig.model_id.trim() !== ''
              const hasSavedFallbacks = savedConfig?.fallback_models && savedConfig.fallback_models.length > 0

              return {
                provider: savedConfig?.provider || defaultConfig?.provider || 'openrouter',
                // Preserve model_id from saved config (including custom models) if it exists
                // Otherwise use default
                model_id: hasSavedModel ? savedConfig.model_id : (defaultConfig?.model_id || ''),
                // Preserve fallback_models from saved config if they exist
                fallback_models: hasSavedFallbacks ? savedConfig.fallback_models : (defaultConfig?.fallback_models || []),
                // Preserve cross_provider_fallback from saved config if it exists
                cross_provider_fallback: savedConfig?.cross_provider_fallback || defaultConfig?.cross_provider_fallback,
                // Preserve API key if it exists in saved config
                api_key: savedConfig?.api_key || defaultConfig?.api_key || '',
                // Preserve region for Bedrock and Azure
                region: savedConfig?.region || defaultConfig?.region,
                // Preserve endpoint for Azure
                endpoint: savedConfig?.endpoint || defaultConfig?.endpoint,
                // Preserve options (includes api_version for Azure, reasoning settings, etc.)
                options: savedConfig?.options || defaultConfig?.options,
                // Preserve temperature
                temperature: savedConfig?.temperature ?? defaultConfig?.temperature
              }
            }
            
            const locked = !!defaults.llm_config_locked
            const defaultPublishedLocked = !!defaults.default_published_llms_locked
            const defaultList = Array.isArray(defaults.default_published_llms) ? defaults.default_published_llms as SavedLLM[] : []

            let newSavedLLMs = currentState.savedLLMs
            if (defaultList.length > 0) {
              if (defaultPublishedLocked) {
                newSavedLLMs = defaultList
              } else {
                const byId = new Map(currentState.savedLLMs.map((llm) => [llm.id, llm]))
                for (const d of defaultList) {
                  if (d.id && !byId.has(d.id)) {
                    byId.set(d.id, d)
                  } else if (d.provider && d.model_id) {
                    const key = `${d.provider}:${d.model_id}`
                    if (!Array.from(byId.values()).some((llm) => llm.provider === d.provider && llm.model_id === d.model_id)) {
                      byId.set(d.id || key, { ...d, id: d.id || key })
                    }
                  }
                }
                newSavedLLMs = Array.from(byId.values())
              }
            }

            let newPrimaryConfig = hasUserSelection ? currentState.primaryConfig : defaults.primary_config
            if (locked && defaultList.length > 0) {
              const first = defaultList[0]
              newPrimaryConfig = {
                provider: first.provider,
                model_id: first.model_id,
                fallback_models: [],
                cross_provider_fallback: undefined
              }
            }

            set({
              primaryConfig: newPrimaryConfig,
              openrouterConfig: preserveUserConfig(currentState.openrouterConfig, defaults.openrouter_config),
              bedrockConfig: preserveUserConfig(currentState.bedrockConfig, defaults.bedrock_config),
              openaiConfig: preserveUserConfig(currentState.openaiConfig, defaults.openai_config),
              vertexConfig: preserveUserConfig(
                currentState.vertexConfig,
                defaults.vertex_config || {
                provider: 'vertex',
                model_id: '',
                fallback_models: [],
                cross_provider_fallback: undefined,
                api_key: ''
                }
              ),
              anthropicConfig: preserveUserConfig(
                currentState.anthropicConfig,
                defaults.anthropic_config || {
                provider: 'anthropic',
                model_id: '',
                fallback_models: [],
                cross_provider_fallback: undefined,
                api_key: ''
                }
              ),
              azureConfig: preserveUserConfig(
                currentState.azureConfig,
                defaults.azure_config || {
                provider: 'azure',
                model_id: '',
                fallback_models: [],
                cross_provider_fallback: undefined,
                api_key: '',
                endpoint: ''
                }
              ),
              minimaxConfig: preserveUserConfig(
                currentState.minimaxConfig,
                defaults.minimax_config || {
                provider: 'minimax',
                model_id: '',
                fallback_models: [],
                cross_provider_fallback: undefined,
                api_key: ''
                }
              ),
              minimaxCodingPlanConfig: preserveUserConfig(
                currentState.minimaxCodingPlanConfig,
                defaults.minimax_coding_plan_config || {
                provider: 'minimax-coding-plan',
                model_id: '',
                fallback_models: [],
                cross_provider_fallback: undefined,
                api_key: ''
                }
              ),
              savedLLMs: newSavedLLMs,
              availableBedrockModels: defaults.available_models.bedrock,
              availableOpenRouterModels: defaults.available_models.openrouter,
              availableOpenAIModels: defaults.available_models.openai,
              availableVertexModels: defaults.available_models.vertex || [],
              availableAnthropicModels: defaults.available_models.anthropic || [],
              availableAzureModels: defaults.available_models.azure || [],
              availableMinimaxModels: defaults.available_models.minimax || [],
              availableMinimaxCodingPlanModels: defaults.available_models['minimax-coding-plan'] || [],
              supportedProviders: (() => {
                const sp = defaults.supported_providers || ['openrouter', 'bedrock', 'openai', 'vertex', 'anthropic', 'azure', 'claude-code', 'gemini-cli', 'minimax', 'minimax-coding-plan']
                console.log('[useLLMStore] supported_providers from backend:', defaults.supported_providers, '→ using:', sp)
                return sp
              })(),
              llmConfigLocked: locked,
              lockedProviders: defaults.locked_providers || [],
              defaultPublishedLLMsLocked: defaultPublishedLocked,
              defaultsLoaded: true,
              error: null,
              isLoadingLLMs: false
            })

            if (locked && defaultList.length > 0) {
              const first = defaultList[0]
              get().setChatPrimaryConfig({ provider: first.provider, model_id: first.model_id, fallback_models: [], cross_provider_fallback: undefined })
              get().setWorkflowPrimaryConfig({ provider: first.provider, model_id: first.model_id, fallback_models: [], cross_provider_fallback: undefined })
              get().setAgentConfig({ primary: first, fallbacks: [] })
            }

            // Refresh availableLLMs from savedLLMs (Published LLMs)
            get().refreshAvailableLLMs()
          } catch (error) {
            console.error('Failed to load LLM defaults from backend:', error)
            set({
              error: 'Failed to load LLM defaults from backend',
              defaultsLoaded: false,
              isLoadingLLMs: false
            })
          }
        },

        // API key testing
        testAPIKey: async (provider, apiKey, modelId?: string, options?: Record<string, unknown>) => {
          try {
            // Only check for empty API key for providers that require it (not bedrock, not vertex)
            // Vertex supports OAuth fallback, so API key is optional
            if (provider !== 'bedrock' && provider !== 'vertex' && !apiKey.trim()) {
              return { valid: false, error: 'API key is empty', correctedOptions: undefined }
            }

            const request: APIKeyValidationRequest = {
              provider,
              options
            }

            // Only include api_key for providers that need it (not bedrock, optional for vertex)
            if (provider !== 'bedrock') {
              // For vertex, only include api_key if provided (OAuth fallback will be used if not)
              if (provider === 'vertex' && apiKey.trim()) {
                request.api_key = apiKey
              } else if (provider !== 'vertex') {
              request.api_key = apiKey
              }
            }

            // Add model ID for all providers when validating
            if (modelId) {
              request.model_id = modelId
            }

            const response = await llmConfigService.validateAPIKey(request)

            return {
              valid: response.valid,
              error: response.valid ? null : (response.message || response.error || 'Validation failed'),
              correctedOptions: response.corrected_options,
              message: response.message
            }
          } catch (error) {
            console.error('API key validation failed:', error)
            return {
              valid: false,
              error: error instanceof Error ? error.message : 'Unknown error occurred',
              correctedOptions: undefined
            }
          }
        },

        updateProvider: (provider) => {
          const state = get()
          let availableModels: string[] = []
          
          switch(provider) {
            case 'openrouter':
              availableModels = [...state.availableOpenRouterModels, ...state.customOpenRouterModels];
              break;
            case 'bedrock':
              availableModels = [...state.availableBedrockModels, ...state.customBedrockModels];
              break;
            case 'openai':
              availableModels = [...state.availableOpenAIModels, ...state.customOpenAIModels];
              break;
            case 'vertex':
              availableModels = [...state.availableVertexModels, ...state.customVertexModels];
              break;
            case 'anthropic':
              availableModels = state.availableAnthropicModels;
              break;
            case 'azure':
              availableModels = [...state.availableAzureModels, ...state.customAzureModels];
              break;
            case 'minimax':
              availableModels = [...state.availableMinimaxModels, ...state.customMinimaxModels];
              break;
          }
          
          // Set appropriate fallback models based on provider
          set({
            primaryConfig: {
              ...state.primaryConfig,
              provider,
              model_id: availableModels[0] || '',
              fallback_models: [],
              cross_provider_fallback: undefined
            },
            error: null
          })
        },

        updateModel: (modelId) => {
          set((state) => ({
            primaryConfig: {
              ...state.primaryConfig,
              model_id: modelId
            },
            error: null
          }))
        },

        updateFallbacks: (fallbacks) => {
          set((state) => ({
            primaryConfig: {
              ...state.primaryConfig,
              fallback_models: fallbacks
            },
            error: null
          }))
        },

        updateCrossProviderFallback: (fallback) => {
          set((state) => ({
            primaryConfig: {
              ...state.primaryConfig,
              cross_provider_fallback: fallback
            },
            error: null
          }))
        },

        refreshAvailableLLMs: async () => {
          const state = get()
          // Don't build list until backend defaults are loaded (so supported_providers is set)
          if (!state.defaultsLoaded) {
            set({ availableLLMs: [], isLoadingLLMs: false })
            return
          }

          set({ isLoadingLLMs: true, error: null })

          try {
            const currentState = get()
            const availableLLMs: LLMOption[] = []

            // Fetch model metadata for cost/context info
            const metadataMap: Record<string, { contextWindow: number; inputCost: number; outputCost: number }> = {}
            try {
              const metadataResponse = await llmConfigService.getModelMetadata()
              metadataResponse.models.forEach(m => {
                metadataMap[m.model_id] = {
                  contextWindow: m.context_window,
                  inputCost: m.input_cost_per_1m,
                  outputCost: m.output_cost_per_1m
                }
              })
            } catch (e) {
              console.warn('Failed to fetch model metadata for dropdown:', e)
            }

            // Build availableLLMs from Published LLMs (savedLLMs)
            // This replaces the old provider-specific model lists
            const supportedProviders = currentState.supportedProviders || []
            currentState.savedLLMs.forEach(savedLLM => {
              // Skip if provider is not supported
              if (supportedProviders.length > 0 && !supportedProviders.includes(savedLLM.provider)) {
                return
              }

              const metadata = metadataMap[savedLLM.model_id]
              availableLLMs.push({
                provider: savedLLM.provider,
                model: savedLLM.model_id,
                label: savedLLM.name || `${savedLLM.provider} - ${savedLLM.model_id}`,
                description: savedLLM.model_name || `Published ${savedLLM.provider} model`,
                temperature: savedLLM.temperature,
                options: savedLLM.options,
                contextWindow: metadata?.contextWindow,
                inputCostPer1M: metadata?.inputCost,
                outputCostPer1M: metadata?.outputCost
              })
            })

            set({ availableLLMs, isLoadingLLMs: false })
          } catch (error) {
            set({
              error: error instanceof Error ? error.message : 'Failed to load LLMs',
              isLoadingLLMs: false
            })
          }
        },

        getCurrentLLMOption: () => {
          const state = get()

          // Use agentConfig primary if available (new tiered system)
          if (state.agentConfig?.primary) {
            const primary = state.agentConfig.primary
            // Try to find matching published LLM for better label
            const publishedLLM = state.savedLLMs.find(
              llm => llm.provider === primary.provider && llm.model_id === primary.model_id
            )

            return {
              provider: primary.provider,
              model: primary.model_id,
              label: publishedLLM?.name || `${primary.provider} - ${primary.model_id}`,
              description: publishedLLM?.model_name || 'Primary LLM'
            }
          }

          // Fallback to legacy primaryConfig
          const currentConfig = state.primaryConfig

          return {
            provider: currentConfig.provider,
            model: currentConfig.model_id,
            label: `${currentConfig.provider} - ${currentConfig.model_id}`,
            description: 'Current LLM configuration'
          }
        },

        isConfigValid: () => {
          const state = get()
          return !!(state.primaryConfig.provider && state.primaryConfig.model_id)
        },

        checkModelExists: async (modelId: string) => {
          // Fetch latest metadata from backend to ensure we have the most current info
          // and specifically to get costs/tokens for the new model
          try {
            const metadataResponse = await llmConfigService.getModelMetadata()
            const exists = metadataResponse.models.some(m => m.model_id === modelId)
            
            // Also refresh available LLMs to ensure the new model (if added) will have metadata
            if (exists) {
               await get().refreshAvailableLLMs()
            }
            
            return exists
          } catch (error) {
            console.error('Failed to validate model existence:', error)
            // Fallback to local state check if API call fails
            return get().availableOpenRouterModels.includes(modelId)
          }
        },

        // Generic actions
        reset: () => {
          set({
            primaryConfig: {
              provider: 'openrouter',
              model_id: '',
              fallback_models: [],
              cross_provider_fallback: undefined
            },
            agentConfig: null,
            openrouterConfig: {
              provider: 'openrouter',
              model_id: '',
              fallback_models: [],
              cross_provider_fallback: undefined,
              api_key: ''
            },
            bedrockConfig: {
              provider: 'bedrock',
              model_id: '',
              fallback_models: [],
              cross_provider_fallback: undefined,
              region: 'us-east-1'
            },
            openaiConfig: {
              provider: 'openai',
              model_id: '',
              fallback_models: [],
              cross_provider_fallback: undefined,
              api_key: ''
            },
            vertexConfig: {
              provider: 'vertex',
              model_id: '',
              fallback_models: [],
              cross_provider_fallback: undefined,
              api_key: ''
            },
            azureConfig: {
              provider: 'azure',
              model_id: '',
              fallback_models: [],
              cross_provider_fallback: undefined,
              api_key: '',
              endpoint: ''
            },
            minimaxConfig: {
              provider: 'minimax',
              model_id: '',
              fallback_models: [],
              cross_provider_fallback: undefined,
              api_key: ''
            },
            showLLMModal: false,
            availableLLMs: [],
            isLoadingLLMs: false,
            error: null
          })
        },

        setLoading: (loading) => {
          set({ isLoadingLLMs: loading })
        },

        setError: (error) => {
          set({ error })
        }
      }),
      {
        name: 'llm-store',
        partialize: (state) => ({
          // Persist user configurations and custom models, but NOT default models from backend
          // Legacy configs (kept for backward compatibility)
          primaryConfig: state.primaryConfig,
          agentConfig: state.agentConfig,
          // Mode-specific configs
          chatPrimaryConfig: state.chatPrimaryConfig,
          chatAgentConfig: state.chatAgentConfig,
          workflowPrimaryConfig: state.workflowPrimaryConfig,
          workflowAgentConfig: state.workflowAgentConfig,
          // Other persisted state
          savedLLMs: state.savedLLMs,
          openrouterConfig: state.openrouterConfig,
          bedrockConfig: state.bedrockConfig,
          openaiConfig: state.openaiConfig,
          vertexConfig: state.vertexConfig,
          anthropicConfig: state.anthropicConfig,
          azureConfig: state.azureConfig,
          minimaxConfig: state.minimaxConfig,
          minimaxCodingPlanConfig: state.minimaxCodingPlanConfig,
          customBedrockModels: state.customBedrockModels,
          customOpenRouterModels: state.customOpenRouterModels,
          customOpenAIModels: state.customOpenAIModels,
          customVertexModels: state.customVertexModels,
          customAzureModels: state.customAzureModels,
          customMinimaxModels: state.customMinimaxModels,
          customMinimaxCodingPlanModels: state.customMinimaxCodingPlanModels,
          geminiCliApiKey: state.geminiCliApiKey,
          geminiCliModel: state.geminiCliModel,
          showLLMModal: state.showLLMModal,
          delegationTierConfig: state.delegationTierConfig,
          // DO NOT persist availableBedrockModels, availableOpenRouterModels, availableOpenAIModels
          // These should always be loaded fresh from backend
          // DO NOT persist defaultsLoaded - this should be reset on each app load
        }),
        // Migration: copy legacy config to mode-specific configs on first load
        onRehydrateStorage: () => (state) => {
          if (state) {
            const hasLegacyConfig = state.primaryConfig?.provider && state.primaryConfig?.model_id
            const hasChatConfig = state.chatPrimaryConfig?.model_id && state.chatPrimaryConfig.model_id !== ''
            const hasWorkflowConfig = state.workflowPrimaryConfig?.model_id && state.workflowPrimaryConfig.model_id !== ''

            // Migrate legacy config to mode-specific configs if not already set
            if (hasLegacyConfig && !hasChatConfig) {
              state.chatPrimaryConfig = { ...state.primaryConfig }
              state.chatAgentConfig = state.agentConfig ? { ...state.agentConfig } : null
            }
            if (hasLegacyConfig && !hasWorkflowConfig) {
              state.workflowPrimaryConfig = { ...state.primaryConfig }
              state.workflowAgentConfig = state.agentConfig ? { ...state.agentConfig } : null
            }
          }
        }
      }
    )
)

// --- Auto-sync provider API keys to server (for scheduled runs) ---
// Debounced: saves 2 seconds after the last key change to avoid spamming on every keystroke.
let _syncTimer: ReturnType<typeof setTimeout> | null = null

function syncProviderKeysToServer() {
  if (_syncTimer) clearTimeout(_syncTimer)
  _syncTimer = setTimeout(async () => {
    try {
      const s = useLLMStore.getState()
      const { providerKeysApi } = await import('../api/scheduler')
      await providerKeysApi.save({
        openrouter: s.openrouterConfig?.api_key || undefined,
        openai: s.openaiConfig?.api_key || undefined,
        anthropic: s.anthropicConfig?.api_key || undefined,
        vertex: s.vertexConfig?.api_key || undefined,
        gemini_cli: s.geminiCliApiKey || undefined,
        minimax: s.minimaxConfig?.api_key || undefined,
        minimax_coding_plan: s.minimaxCodingPlanConfig?.api_key || undefined,
        bedrock: s.bedrockConfig?.region ? { region: s.bedrockConfig.region } : undefined,
        azure: s.azureConfig?.endpoint && s.azureConfig?.api_key
          ? {
              endpoint: s.azureConfig.endpoint,
              api_key: s.azureConfig.api_key,
              api_version: (s.azureConfig.options?.api_version as string) || undefined,
              region: s.azureConfig.region || undefined,
            }
          : undefined,
      })
    } catch {
      // Silent fail — keys are still in localStorage as fallback
    }
  }, 2000)
}

const getProviderKeySnapshot = (state: LLMState) => ([
  state.openrouterConfig?.api_key,
  state.openaiConfig?.api_key,
  state.anthropicConfig?.api_key,
  state.vertexConfig?.api_key,
  state.azureConfig?.api_key,
  state.azureConfig?.endpoint,
  state.minimaxConfig?.api_key,
  state.minimaxCodingPlanConfig?.api_key,
  state.bedrockConfig?.region,
  state.geminiCliApiKey,
])

// Watch for changes to any provider config or API key
useLLMStore.subscribe((state, prevState) => {
  const nextSnapshot = getProviderKeySnapshot(state)
  const prevSnapshot = getProviderKeySnapshot(prevState)
  if (JSON.stringify(nextSnapshot) !== JSON.stringify(prevSnapshot)) {
    syncProviderKeysToServer()
  }
})
