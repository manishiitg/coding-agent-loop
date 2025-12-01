import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import { devtools } from 'zustand/middleware'
import type { LLMConfiguration, ExtendedLLMConfiguration, APIKeyValidationRequest, FallbackModel, LLMProvider } from '../services/api-types'
import type { LLMOption } from '../types/llm'
import type { StoreActions } from './types'
import { getAllAvailableLLMs, getAvailableModels } from '../utils/llmConfig'
import { llmConfigService } from '../services/llm-config-api'

interface LLMState extends StoreActions {
  // Primary LLM configuration (unified from sidebar and chat input)
  primaryConfig: LLMConfiguration
  
  // Provider-specific configurations with API keys
  openrouterConfig: ExtendedLLMConfiguration
  bedrockConfig: ExtendedLLMConfiguration
  openaiConfig: ExtendedLLMConfiguration
  vertexConfig: ExtendedLLMConfiguration
  anthropicConfig: ExtendedLLMConfiguration
  
  // Custom models for each provider
  customBedrockModels: string[]
  customOpenRouterModels: string[]
  customOpenAIModels: string[]
  customVertexModels: string[]
  
  // Available models from backend
  availableBedrockModels: string[]
  availableOpenRouterModels: string[]
  availableOpenAIModels: string[]
  availableVertexModels: string[]
  availableAnthropicModels: string[]
  
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
  setVertexConfig: (config: ExtendedLLMConfiguration) => void
  setAnthropicConfig: (config: ExtendedLLMConfiguration) => void
  setShowLLMModal: (show: boolean) => void
  loadDefaultsFromBackend: () => Promise<void>
  
  // Custom model management
  addCustomBedrockModel: (model: string) => void
  removeCustomBedrockModel: (model: string) => void
  addCustomOpenRouterModel: (model: string) => void
  removeCustomOpenRouterModel: (model: string) => void
  addCustomOpenAIModel: (model: string) => void
  removeCustomOpenAIModel: (model: string) => void
  addCustomVertexModel: (model: string) => void
  removeCustomVertexModel: (model: string) => void
  
  // Legacy actions (for backward compatibility)
  updateProvider: (provider: LLMProvider) => void
  updateModel: (modelId: string) => void
  updateFallbacks: (fallbacks: FallbackModel[]) => void
  // Fallback management actions
  addFallbackModel: (model: FallbackModel) => void
  removeFallbackModel: (modelId: string) => void
  reorderFallbackModels: (newOrder: FallbackModel[]) => void
  refreshAvailableLLMs: () => Promise<void>
  
  // API key management
  testAPIKey: (provider: 'openrouter' | 'openai' | 'bedrock' | 'vertex' | 'anthropic', apiKey: string, modelId?: string) => Promise<{valid: boolean, error: string | null}>
  
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
          provider: 'openrouter',
          model_id: '',
          fallback_models: []  // Unified FallbackModel array
        },
        
        // Provider-specific configurations - will be loaded from backend
        openrouterConfig: {
          provider: 'openrouter',
          model_id: '',
          fallback_models: [],
          api_key: ''
        },
        bedrockConfig: {
          provider: 'bedrock',
          model_id: '',
          fallback_models: [],
          region: 'us-east-1'
        },
        openaiConfig: {
          provider: 'openai',
          model_id: '',
          fallback_models: [],
          api_key: ''
        },
        vertexConfig: {
          provider: 'vertex',
          model_id: '',
          fallback_models: [],
          api_key: ''
        },
        anthropicConfig: {
          provider: 'anthropic',
          model_id: '',
          fallback_models: [],
          api_key: ''
        },
        
        // Custom models for each provider
        customBedrockModels: [],
        customOpenRouterModels: [],
        customOpenAIModels: [],
        customVertexModels: [],
        
        // Available models from backend
        availableBedrockModels: [],
        availableOpenRouterModels: [],
        availableOpenAIModels: [],
        availableVertexModels: [],
        availableAnthropicModels: [],
        
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

        setVertexConfig: (config) => {
          set({ vertexConfig: config, error: null })
        },

        setAnthropicConfig: (config) => {
          set({ anthropicConfig: config, error: null })
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
                // Preserve fallback_models from saved config if they exist (now FallbackModel[])
                fallback_models: hasSavedFallbacks ? savedConfig.fallback_models : (defaultConfig?.fallback_models || []),
                // Preserve API key if it exists in saved config
                api_key: savedConfig?.api_key || defaultConfig?.api_key || '',
                // Preserve region for Bedrock
                region: savedConfig?.region || defaultConfig?.region
              }
            }
            
            set({
              // Only overwrite primaryConfig if user hasn't selected a model yet
              // This preserves user's LLM selection across app reloads and modal opens
              primaryConfig: hasUserSelection 
                ? currentState.primaryConfig 
                : defaults.primary_config,
              openrouterConfig: preserveUserConfig(currentState.openrouterConfig, defaults.openrouter_config),
              bedrockConfig: preserveUserConfig(currentState.bedrockConfig, defaults.bedrock_config),
              openaiConfig: preserveUserConfig(currentState.openaiConfig, defaults.openai_config),
              vertexConfig: preserveUserConfig(
                currentState.vertexConfig,
                defaults.vertex_config || {
                provider: 'vertex',
                model_id: '',
                fallback_models: [],
                api_key: ''
                }
              ),
              anthropicConfig: preserveUserConfig(
                currentState.anthropicConfig,
                defaults.anthropic_config || {
                provider: 'anthropic',
                model_id: '',
                fallback_models: [],
                api_key: ''
                }
              ),
              availableBedrockModels: defaults.available_models.bedrock,
              availableOpenRouterModels: defaults.available_models.openrouter,
              availableOpenAIModels: defaults.available_models.openai,
              availableVertexModels: defaults.available_models.vertex || [],
              availableAnthropicModels: defaults.available_models.anthropic || [],
              defaultsLoaded: true,
              error: null,
              isLoadingLLMs: false
            })
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
        testAPIKey: async (provider, apiKey, modelId?: string) => {
          try {
            // Only check for empty API key for providers that require it (not bedrock, not vertex)
            // Vertex supports OAuth fallback, so API key is optional
            if (provider !== 'bedrock' && provider !== 'vertex' && !apiKey.trim()) {
              return { valid: false, error: 'API key is empty' }
            }
            
            const request: APIKeyValidationRequest = {
              provider
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
          
          // Set appropriate fallback models based on provider (unified FallbackModel format)
          let fallbackModels: FallbackModel[] = []
          
          if (provider === 'openrouter') {
            fallbackModels = [
              { model_id: 'x-ai/grok-code-fast-1', provider: 'openrouter', priority: 1 },
              { model_id: 'openai/gpt-4o-mini', provider: 'openrouter', priority: 2 },
              { model_id: 'gpt-4o-mini', provider: 'openai', priority: 3 }  // Cross-provider fallback
            ]
          } else if (provider === 'bedrock') {
            fallbackModels = [
              { model_id: 'us.anthropic.claude-sonnet-4-20250514-v1:0', provider: 'bedrock', priority: 1 },
              { model_id: 'us.anthropic.claude-3-7-sonnet-20250219-v1:0', provider: 'bedrock', priority: 2 },
              { model_id: 'x-ai/grok-code-fast-1', provider: 'openrouter', priority: 3 }  // Cross-provider fallback
            ]
          } else if (provider === 'openai') {
            fallbackModels = [
              { model_id: 'gpt-4o-mini', provider: 'openai', priority: 1 },
              { model_id: 'gpt-4o', provider: 'openai', priority: 2 }
            ]
          } else if (provider === 'vertex') {
            fallbackModels = [
              { model_id: 'gemini-2.0-flash-001', provider: 'vertex', priority: 1 },
              { model_id: 'gemini-1.5-flash', provider: 'vertex', priority: 2 }
            ]
          } else if (provider === 'anthropic') {
            fallbackModels = [
              { model_id: 'claude-3-5-sonnet-20241022', provider: 'anthropic', priority: 1 },
              { model_id: 'claude-3-haiku-20240307', provider: 'anthropic', priority: 2 }
            ]
          }

          set({
            primaryConfig: {
              ...state.primaryConfig,
              provider,
              model_id: availableModels[0] || '',
              fallback_models: fallbackModels
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

        // Add a single fallback model
        addFallbackModel: (model) => {
          set((state) => {
            const existingModels = state.primaryConfig.fallback_models
            // Auto-assign priority based on position
            const newPriority = existingModels.length + 1
            const newModel = { ...model, priority: newPriority }
            return {
              primaryConfig: {
                ...state.primaryConfig,
                fallback_models: [...existingModels, newModel]
              },
              error: null
            }
          })
        },

        // Remove a fallback model and re-order priorities
        removeFallbackModel: (modelId) => {
          set((state) => {
            const filteredModels = state.primaryConfig.fallback_models
              .filter(m => m.model_id !== modelId)
              .map((m, idx) => ({ ...m, priority: idx + 1 }))  // Re-assign priorities
            return {
              primaryConfig: {
                ...state.primaryConfig,
                fallback_models: filteredModels
              },
              error: null
            }
          })
        },

        // Reorder fallback models (for drag-and-drop)
        reorderFallbackModels: (newOrder) => {
          set((state) => ({
            primaryConfig: {
              ...state.primaryConfig,
              fallback_models: newOrder.map((m, idx) => ({ ...m, priority: idx + 1 }))
            },
            error: null
          }))
        },

        refreshAvailableLLMs: async () => {
          set({ isLoadingLLMs: true, error: null })
          
          try {
            const state = get()
            const baseLLMs = getAllAvailableLLMs()
            
            // Add custom models from store
            const customBedrockLLMs = state.customBedrockModels.map(model => ({
              provider: 'bedrock' as const,
              model,
              label: `Bedrock - ${model}`,
              description: 'Custom Bedrock model'
            }))
            
            const customVertexLLMs = state.customVertexModels.map(model => ({
              provider: 'vertex' as const,
              model,
              label: `Vertex - ${model}`,
              description: 'Custom Vertex AI model'
            }))
            
            const customOpenAILLMs = state.customOpenAIModels.map(model => ({
              provider: 'openai' as const,
              model,
              label: `OpenAI - ${model}`,
              description: 'Custom OpenAI model'
            }))
            
            const availableLLMs = [
              ...baseLLMs,
              ...customBedrockLLMs,
              ...customVertexLLMs,
              ...customOpenAILLMs
            ]
            
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
              provider: 'openrouter',
              model_id: '',
              fallback_models: []
            },
            openrouterConfig: {
              provider: 'openrouter',
              model_id: '',
              fallback_models: [],
              api_key: ''
            },
            bedrockConfig: {
              provider: 'bedrock',
              model_id: '',
              fallback_models: [],
              region: 'us-east-1'
            },
            openaiConfig: {
              provider: 'openai',
              model_id: '',
              fallback_models: [],
              api_key: ''
            },
            vertexConfig: {
              provider: 'vertex',
              model_id: '',
              fallback_models: [],
              api_key: ''
            },
            anthropicConfig: {
              provider: 'anthropic',
              model_id: '',
              fallback_models: [],
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
          vertexConfig: state.vertexConfig,
          anthropicConfig: state.anthropicConfig,
          customBedrockModels: state.customBedrockModels,
          customOpenRouterModels: state.customOpenRouterModels,
          customOpenAIModels: state.customOpenAIModels,
          customVertexModels: state.customVertexModels,
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
