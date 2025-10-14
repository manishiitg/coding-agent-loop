import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { devtools } from 'zustand/middleware'
import type { LLMConfiguration, ExtendedLLMConfiguration, APIKeyValidationRequest } from '../services/api-types'
import type { LLMOption, StoreActions } from './types'
import { getAllAvailableLLMs, getAvailableModels } from '../utils/llmConfig'
import { llmConfigService } from '../services/llm-config-api'

interface LLMState extends StoreActions {
  // Primary LLM configuration (unified from sidebar and chat input)
  primaryConfig: LLMConfiguration
  
  // Provider-specific configurations with API keys
  openrouterConfig: ExtendedLLMConfiguration
  bedrockConfig: ExtendedLLMConfiguration
  openaiConfig: ExtendedLLMConfiguration
  
  // Custom models for each provider
  customBedrockModels: string[]
  customOpenRouterModels: string[]
  customOpenAIModels: string[]
  
  // Available models from backend
  availableBedrockModels: string[]
  availableOpenRouterModels: string[]
  availableOpenAIModels: string[]
  
  // Modal state
  showLLMModal: boolean
  
  // Available LLMs for selection
  availableLLMs: LLMOption[]
  
  // Loading and error states
  isLoadingLLMs: boolean
  error: string | null
  defaultsLoaded: boolean
  
  // Actions
  setPrimaryConfig: (config: LLMConfiguration) => void
  setOpenrouterConfig: (config: ExtendedLLMConfiguration) => void
  setBedrockConfig: (config: ExtendedLLMConfiguration) => void
  setOpenaiConfig: (config: ExtendedLLMConfiguration) => void
  setShowLLMModal: (show: boolean) => void
  loadDefaultsFromBackend: () => Promise<void>
  
  // Custom model management
  addCustomBedrockModel: (model: string) => void
  removeCustomBedrockModel: (model: string) => void
  addCustomOpenRouterModel: (model: string) => void
  removeCustomOpenRouterModel: (model: string) => void
  addCustomOpenAIModel: (model: string) => void
  removeCustomOpenAIModel: (model: string) => void
  
  // Legacy actions (for backward compatibility)
  updateProvider: (provider: 'openrouter' | 'bedrock') => void
  updateModel: (modelId: string) => void
  updateFallbacks: (fallbacks: string[]) => void
  updateCrossProviderFallback: (fallback: LLMConfiguration['cross_provider_fallback']) => void
  refreshAvailableLLMs: () => Promise<void>
  
  // API key management
  testAPIKey: (provider: 'openrouter' | 'openai' | 'bedrock', apiKey: string, modelId?: string) => Promise<{valid: boolean, error: string | null}>
  
  // Helper methods
  getCurrentLLMOption: () => LLMOption | null
  isConfigValid: () => boolean
}

export const useLLMStore = create<LLMState>()(
  devtools(
    persist(
      (set, get) => ({
        // Initial state - will be loaded from backend
        primaryConfig: {
          provider: 'openrouter' as 'openrouter' | 'bedrock' | 'openai',
          model_id: '',
          fallback_models: [],
          cross_provider_fallback: undefined
        },
        
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
        
        // Custom models for each provider
        customBedrockModels: [],
        customOpenRouterModels: [],
        customOpenAIModels: [],
        
        // Available models from backend
        availableBedrockModels: [],
        availableOpenRouterModels: [],
        availableOpenAIModels: [],
        
        // Modal state
        showLLMModal: false,
        
        availableLLMs: [],
        isLoadingLLMs: false,
        error: null,
        defaultsLoaded: false,

        // Actions
        setPrimaryConfig: (config) => {
          set({ primaryConfig: config, error: null })
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

        setShowLLMModal: (show) => {
          set({ showLLMModal: show })
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

        // Load defaults from backend
        loadDefaultsFromBackend: async () => {
          try {
            const defaults = await llmConfigService.getLLMDefaults()
            
            set({
              primaryConfig: defaults.primary_config,
              openrouterConfig: defaults.openrouter_config,
              bedrockConfig: defaults.bedrock_config,
              openaiConfig: defaults.openai_config,
              availableBedrockModels: defaults.available_models.bedrock,
              availableOpenRouterModels: defaults.available_models.openrouter,
              availableOpenAIModels: defaults.available_models.openai,
              defaultsLoaded: true,
              error: null
            })

            // Automatically test prefilled API keys after state is updated
            const { testAPIKey } = get()
            
            // Test OpenRouter API key if present
            if (defaults.openrouter_config.api_key) {
              try {
                const isValid = await testAPIKey('openrouter', defaults.openrouter_config.api_key)
                console.log('OpenRouter API key validation:', isValid ? 'valid' : 'invalid')
              } catch (error) {
                console.log('OpenRouter API key validation failed:', error)
              }
            }

            // Test OpenAI API key if present
            if (defaults.openai_config.api_key) {
              try {
                const isValid = await testAPIKey('openai', defaults.openai_config.api_key)
                console.log('OpenAI API key validation:', isValid ? 'valid' : 'invalid')
              } catch (error) {
                console.log('OpenAI API key validation failed:', error)
              }
            }
          } catch (error) {
            console.error('Failed to load LLM defaults from backend:', error)
            set({ 
              error: 'Failed to load LLM defaults from backend',
              defaultsLoaded: false 
            })
          }
        },

        // API key testing
        testAPIKey: async (provider, apiKey, modelId?: string) => {
          try {
            if (!apiKey.trim()) return { valid: false, error: 'API key is empty' }
            
            const request: APIKeyValidationRequest = {
              provider,
              api_key: apiKey
            }
            
            // Add model ID for Bedrock validation
            if (provider === 'bedrock' && modelId) {
              request.model_id = modelId
            }
            
            const response = await llmConfigService.validateAPIKey(request)
            
            return { 
              valid: response.valid, 
              error: response.valid ? null : (response.message || response.error || 'Validation failed')
            }
          } catch (error) {
            console.error('API key validation failed:', error)
            return { 
              valid: false, 
              error: error instanceof Error ? error.message : 'Unknown error occurred'
            }
          }
        },

        updateProvider: (provider) => {
          const state = get()
          const availableModels = getAvailableModels(provider)
          
          // Set appropriate fallback models based on provider
          let fallbackModels: string[] = []
          let crossProviderFallback: LLMConfiguration['cross_provider_fallback']
          
          if (provider === 'openrouter') {
            fallbackModels = ['z-ai/glm-4.5', 'openai/gpt-4o-mini']
            crossProviderFallback = {
              provider: 'openai',
              models: ['gpt-4o-mini']
            }
          } else if (provider === 'bedrock') {
            fallbackModels = [
              'us.anthropic.claude-sonnet-4-20250514-v1:0',
              'us.anthropic.claude-3-7-sonnet-20250219-v1:0'
            ]
            crossProviderFallback = {
              provider: 'openrouter',
              models: ['x-ai/grok-code-fast-1', 'openai/gpt-4o-mini']
            }
          }

          set({
            primaryConfig: {
              ...state.primaryConfig,
              provider,
              model_id: availableModels[0] || '',
              fallback_models: fallbackModels,
              cross_provider_fallback: crossProviderFallback
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
          set({ isLoadingLLMs: true, error: null })
          
          try {
            const availableLLMs = getAllAvailableLLMs()
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

        // Generic actions
        reset: () => {
          set({
            primaryConfig: {
              provider: 'openrouter' as 'openrouter' | 'bedrock' | 'openai',
              model_id: '',
              fallback_models: [],
              cross_provider_fallback: undefined
            },
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
          primaryConfig: state.primaryConfig,
          openrouterConfig: state.openrouterConfig,
          bedrockConfig: state.bedrockConfig,
          openaiConfig: state.openaiConfig,
          customBedrockModels: state.customBedrockModels,
          customOpenRouterModels: state.customOpenRouterModels,
          customOpenAIModels: state.customOpenAIModels,
          showLLMModal: state.showLLMModal,
          // DO NOT persist availableBedrockModels, availableOpenRouterModels, availableOpenAIModels
          // These should always be loaded fresh from backend
          // DO NOT persist defaultsLoaded - this should be reset on each app load
        })
      }
    ),
    {
      name: 'llm-store'
    }
  )
)
