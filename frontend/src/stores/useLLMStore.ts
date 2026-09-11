import { stripRetiredLLMFallbacks } from '../utils/retiredLLMFallbacks'
import { create } from 'zustand'
import { persist } from 'zustand/middleware'
import type { LLMConfiguration, ExtendedLLMConfiguration, APIKeyValidationRequest, AgentLLMConfiguration, SavedLLM, LLMModel, DelegationTierConfig, LLMProvider } from '../services/api-types'
import type { DelegationTierDefaultsStatus } from '../utils/llmOnboarding'
import type { LLMOption } from '../types/llm'
import type { StoreActions } from './types'
import { llmConfigService, type ModelMetadata, type ProviderManifestEntry, type DynamicModelsResponse } from '../services/llm-config-api'
import { agentApi } from '../services/api'
import { providerKeysApi, type StoredProviderKeys } from '../api/scheduler'

type PublishedLLMMetadataSnapshot = {
  context_window?: number
  input_cost_per_1m?: number
  output_cost_per_1m?: number
  reasoning_cost_per_1m?: number
  cached_input_cost_per_1m?: number
  cached_input_cost_write_per_1m?: number
}

const DEFAULT_CHAT_PROVIDER: LLMProvider = 'codex-cli'
const DEFAULT_CHAT_MODEL = 'codex-cli'
// Direct API transport is limited to text LLM providers. MiniMax remains
// available only through Pi's text-model sub-provider routing; no separate
// MiniMax, ElevenLabs, or Deepgram frontend provider configuration remains.
const FRONTEND_DEPRECATED_PROVIDER_IDS = new Set<string>([
  'agy-cli',
  'openai',
  'anthropic',
  'vertex',
  'bedrock',
  'azure',
])
const MASKED_PROVIDER_KEY_PREFIX = '********'
const SUPPORTED_PROVIDERS_FALLBACK: LLMProvider[] = [
  'bedrock',
  'openai',
  'vertex',
  'anthropic',
  'azure',
  'claude-code',
  'codex-cli',
  'cursor-cli',
  'pi-cli',
  'muse-cli',
]

function isFrontendDeprecatedProvider(provider?: string): boolean {
  return !!provider && FRONTEND_DEPRECATED_PROVIDER_IDS.has(provider)
}

function isMaskedProviderKey(value?: string): boolean {
  return !!value?.trim().startsWith(MASKED_PROVIDER_KEY_PREFIX)
}

function unmaskedProviderKey(value?: string): string | undefined {
  const trimmed = value?.trim()
  if (!trimmed || isMaskedProviderKey(trimmed)) return undefined
  return trimmed
}

function hasUsableLLMIdentity(model?: { provider?: string; model_id?: string }): model is { provider: LLMProvider; model_id: string } {
  return !!model?.provider?.trim() && !!model?.model_id?.trim() && !isFrontendDeprecatedProvider(model.provider)
}

function defaultLLMConfiguration(): LLMConfiguration {
  return {
    provider: DEFAULT_CHAT_PROVIDER,
    model_id: DEFAULT_CHAT_MODEL,
  }
}

function normalizePrimaryConfig(config?: LLMConfiguration): LLMConfiguration {
  if (!config || !hasUsableLLMIdentity(config)) {
    return defaultLLMConfiguration()
  }
  return {
    published_llm_id: config.published_llm_id,
    provider: config.provider,
    model_id: config.model_id,
    options: config.options,
    api_keys: config.api_keys,
  }
}

function sanitizeLLMModel(model: LLMModel): LLMModel {
  return {
    provider: model.provider,
    model_id: model.model_id,
    region: model.region,
    options: model.options,
  }
}

function normalizeLLMModel(model?: LLMModel): LLMModel {
  if (!model || !hasUsableLLMIdentity(model)) {
    return {
      provider: DEFAULT_CHAT_PROVIDER,
      model_id: DEFAULT_CHAT_MODEL,
    }
  }
  return sanitizeLLMModel(model)
}

function sanitizeSavedLLM(llm: SavedLLM): SavedLLM {
  return {
    ...sanitizeLLMModel(llm),
    id: llm.id,
    name: llm.name,
    source: llm.source,
    created_at: llm.created_at,
  }
}

function isAutoPublishedLLM(llm: SavedLLM): boolean {
  return llm.source === 'auto_coding_agent' || llm.id?.startsWith('auto:')
}

function persistablePublishedLLMs(llms: SavedLLM[]): SavedLLM[] {
  return filterPublishedLLMs(llms).filter(llm => !isAutoPublishedLLM(llm))
}

function filterPublishedLLMs(llms: SavedLLM[]): SavedLLM[] {
  return llms.filter(llm =>
    hasUsableLLMIdentity(llm) &&
    !FRONTEND_DEPRECATED_PROVIDER_IDS.has(llm.provider)
  )
}

function sanitizeAgentConfig(config: AgentLLMConfiguration | null): AgentLLMConfiguration | null {
  if (!config) return null
  return {
    primary: normalizeLLMModel(config.primary),
  }
}

function sanitizeProviderConfigForPersistence(config: ExtendedLLMConfiguration): ExtendedLLMConfiguration {
  const sanitized = stripRetiredLLMFallbacks({ ...config }) as ExtendedLLMConfiguration & { temperature?: number }
  delete sanitized.api_key
  delete sanitized.endpoint
  delete sanitized.temperature
  return sanitized
}

function hasStoredProviderKeys(keys?: StoredProviderKeys | null): boolean {
  return !!(
    keys?.openai ||
    keys?.anthropic ||
    keys?.zai ||
    keys?.kimi ||
    keys?.vertex ||
    keys?.codex_cli ||
    keys?.cursor_cli ||
    keys?.pi_cli ||
    (keys?.pi_provider_keys && Object.values(keys.pi_provider_keys).some(key => !!key?.trim())) ||
    keys?.bedrock?.region ||
    (keys?.azure?.endpoint && keys?.azure?.api_key)
  )
}

function extractStoredProviderKeysFromState(state: {
  openrouterConfig: ExtendedLLMConfiguration
  openaiConfig: ExtendedLLMConfiguration
  anthropicConfig: ExtendedLLMConfiguration
  zaiConfig: ExtendedLLMConfiguration
  kimiConfig: ExtendedLLMConfiguration
  vertexConfig: ExtendedLLMConfiguration
  bedrockConfig: ExtendedLLMConfiguration
  azureConfig: ExtendedLLMConfiguration
  savedLLMs: SavedLLM[]
}): StoredProviderKeys {
  const keys: StoredProviderKeys = {
    openai: unmaskedProviderKey(state.openaiConfig?.api_key),
    anthropic: unmaskedProviderKey(state.anthropicConfig?.api_key),
    zai: unmaskedProviderKey(state.zaiConfig?.api_key),
    kimi: unmaskedProviderKey(state.kimiConfig?.api_key),
    vertex: unmaskedProviderKey(state.vertexConfig?.api_key),
    bedrock: state.bedrockConfig?.region ? { region: state.bedrockConfig.region } : undefined,
    azure: state.azureConfig?.endpoint && unmaskedProviderKey(state.azureConfig?.api_key)
      ? {
          endpoint: state.azureConfig.endpoint,
          api_key: unmaskedProviderKey(state.azureConfig.api_key)!,
          api_version: (state.azureConfig.options?.api_version as string) || undefined,
          region: state.azureConfig.region || undefined,
        }
      : undefined,
  }

  for (const llm of state.savedLLMs || []) {
    const apiKey = unmaskedProviderKey(llm.api_key)
    if (llm.provider === 'openai' && apiKey && !keys.openai) keys.openai = apiKey
    if (llm.provider === 'anthropic' && apiKey && !keys.anthropic) keys.anthropic = apiKey
    if (llm.provider === 'z-ai' && apiKey && !keys.zai) keys.zai = apiKey
    if (llm.provider === 'kimi' && apiKey && !keys.kimi) keys.kimi = apiKey
    if (llm.provider === 'vertex' && apiKey && !keys.vertex) keys.vertex = apiKey
    if (llm.provider === 'codex-cli' && apiKey && !keys.codex_cli) keys.codex_cli = apiKey
    if (llm.provider === 'pi-cli' && apiKey && !keys.pi_cli) keys.pi_cli = apiKey
    if (llm.provider === 'bedrock' && llm.region && !keys.bedrock) keys.bedrock = { region: llm.region }
    if (llm.provider === 'azure' && llm.endpoint && apiKey && !keys.azure) {
      keys.azure = {
        endpoint: llm.endpoint,
        api_key: apiKey,
        api_version: (llm.options?.api_version as string) || undefined,
        region: llm.region || undefined,
      }
    }
  }

  return keys
}

interface LLMState extends StoreActions {
  // Primary LLM configuration (unified from sidebar and chat input)
  // LEGACY: kept for backward compatibility, use mode-specific configs instead
  primaryConfig: LLMConfiguration

  // New unified configuration (Tiered Model Selection)
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
  zaiConfig: ExtendedLLMConfiguration
  kimiConfig: ExtendedLLMConfiguration

  // Custom models for each provider
  customBedrockModels: string[]
  customOpenRouterModels: string[]
  customOpenAIModels: string[]
  customVertexModels: string[]
  customAzureModels: string[]

  // Available models from backend
  availableBedrockModels: string[]
  availableOpenRouterModels: string[]
  availableOpenAIModels: string[]
  availableVertexModels: string[]
  availableAnthropicModels: string[]
  availableAzureModels: string[]
  availableZAIModels: string[]
  availableKimiModels: string[]

  // Modal state
  showLLMModal: boolean

  // Available LLMs for selection
  availableLLMs: LLMOption[]
  modelMetadataCatalog: ModelMetadata[]
  
  // Loading and error states
  isLoadingLLMs: boolean
  error: string | null
  defaultsLoaded: boolean

  // Supported providers (from backend, not persisted)
  supportedProviders: LLMProvider[]
  providerCapabilities: Partial<Record<LLMProvider, string[]>>
  isProviderSupported: (provider: string) => boolean

  // Provider manifest (API-driven provider discovery)
  providerManifest: ProviderManifestEntry[]
  providerManifestLoaded: boolean
  providerManifestLoading: boolean
  loadProviderManifest: () => Promise<void>
  getProviderInfo: (id: string) => ProviderManifestEntry | undefined
  getProviderDynamicModels: (provider: string, full?: boolean) => Promise<DynamicModelsResponse | null>

  // Delegation tier configuration
  delegationTierConfig: DelegationTierConfig | null
  delegationTierDefaultsStatus: DelegationTierDefaultsStatus
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
  setZaiConfig: (config: ExtendedLLMConfiguration) => void
  setKimiConfig: (config: ExtendedLLMConfiguration) => void
  setShowLLMModal: (show: boolean) => void
  loadDefaultsFromBackend: () => Promise<void>
  
  // Library management
  saveLLM: (llm: LLMModel, name: string, modelName?: string, authMethod?: 'api_key' | 'oauth' | 'none', metadata?: PublishedLLMMetadataSnapshot) => Promise<void>
  deleteSavedLLM: (id: string) => Promise<void>

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

  // Legacy actions (for backward compatibility)
  updateProvider: (provider: 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure') => void
  updateModel: (modelId: string) => void
  refreshAvailableLLMs: () => Promise<void>
  
  // API key management
  testAPIKey: (provider: 'openrouter' | 'openai' | 'bedrock' | 'vertex' | 'anthropic' | 'azure' | 'z-ai' | 'kimi', apiKey: string, modelId?: string, options?: Record<string, unknown>) => Promise<{valid: boolean, error: string | null, correctedOptions?: Record<string, unknown>}>
  
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
        primaryConfig: defaultLLMConfiguration(),

        agentConfig: null,

        // Mode-specific configs (initialized empty, will be migrated from legacy on first load)
        chatPrimaryConfig: defaultLLMConfiguration(),
        chatAgentConfig: null,
        workflowPrimaryConfig: defaultLLMConfiguration(),
        workflowAgentConfig: null,

        // Saved/Published LLM Library
        savedLLMs: [],
        
        // Provider-specific configurations - will be loaded from backend
        openrouterConfig: {
          provider: 'openrouter',
          model_id: '',
          api_key: ''
        },
        bedrockConfig: {
          provider: 'bedrock',
          model_id: '',
          region: 'us-east-1'
        },
        openaiConfig: {
          provider: 'openai',
          model_id: '',
          api_key: ''
        },
        vertexConfig: {
          provider: 'vertex',
          model_id: '',
          api_key: ''
        },
        anthropicConfig: {
          provider: 'anthropic',
          model_id: '',
          api_key: ''
        },
        azureConfig: {
          provider: 'azure',
          model_id: '',
          api_key: '',
          endpoint: ''
        },
        zaiConfig: {
          provider: 'z-ai',
          model_id: '',
          api_key: ''
        },
        kimiConfig: {
          provider: 'kimi',
          model_id: '',
          api_key: ''
        },
        // Custom models for each provider
        customBedrockModels: [],
        customOpenRouterModels: [],
        customOpenAIModels: [],
        customVertexModels: [],
        customAzureModels: [],

        // Available models from backend
        availableBedrockModels: [],
        availableOpenRouterModels: [],
        availableOpenAIModels: [],
        availableVertexModels: [],
        availableAnthropicModels: [],
        availableAzureModels: [],
        availableZAIModels: [],
        availableKimiModels: [],

        // Modal state
        showLLMModal: false,

        availableLLMs: [],
        modelMetadataCatalog: [],
        isLoadingLLMs: false,
        error: null,
        defaultsLoaded: false,

        // Provider manifest (API-driven)
        providerManifest: [],
        providerManifestLoaded: false,
        providerManifestLoading: false,

        loadProviderManifest: async () => {
          if (get().providerManifestLoading) return
          set({ providerManifestLoading: true })
          try {
            const manifest = await llmConfigService.getProviderManifest()
            set({
              // A server that answers without a providers list (an older
              // build, a product-only deployment) must not leave the store
              // holding undefined: every consumer indexes this array.
              providerManifest: Array.isArray(manifest?.providers) ? manifest.providers : [],
              providerManifestLoaded: true,
              providerManifestLoading: false,
            })
          } catch (error) {
            console.warn('Failed to load provider manifest:', error)
            set({ providerManifestLoading: false })
          }
        },

        getProviderInfo: (id: string) => {
          return get().providerManifest.find(p => p.id === id)
        },

        getProviderDynamicModels: async (provider: string, full?: boolean) => {
          try {
            return await llmConfigService.getProviderModels(provider, full)
          } catch (error) {
            console.warn(`Failed to load dynamic models for ${provider}:`, error)
            return null
          }
        },

        // Delegation tier config
        delegationTierConfig: null,
        delegationTierDefaultsStatus: 'idle',

        // Supported providers (always load fresh from backend, default to all)
        supportedProviders: SUPPORTED_PROVIDERS_FALLBACK,
        providerCapabilities: {},
        llmConfigLocked: false,
        lockedProviders: [],
        defaultPublishedLLMsLocked: false,
        isProviderSupported: (provider) => {
          const supported = get().supportedProviders
          return supported.includes(provider as typeof supported[number])
        },

        // Actions
        setPrimaryConfig: (config) => {
          set({ primaryConfig: stripRetiredLLMFallbacks(config), error: null })
        },

        setAgentConfig: (config) => {
          set({ agentConfig: stripRetiredLLMFallbacks(config), error: null })
        },

        // Mode-specific config actions
        setChatPrimaryConfig: (config) => {
          set({ chatPrimaryConfig: stripRetiredLLMFallbacks(config), error: null })
        },

        setChatAgentConfig: (config) => {
          set({ chatAgentConfig: stripRetiredLLMFallbacks(config), error: null })
        },

        setWorkflowPrimaryConfig: (config) => {
          set({ workflowPrimaryConfig: stripRetiredLLMFallbacks(config), error: null })
        },

        setWorkflowAgentConfig: (config) => {
          set({ workflowAgentConfig: stripRetiredLLMFallbacks(config), error: null })
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

        setZaiConfig: (config) => {
          set({ zaiConfig: config, error: null })
        },

        setKimiConfig: (config) => {
          set({ kimiConfig: config, error: null })
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
              openai: state.openaiConfig,
              anthropic: state.anthropicConfig,
              vertex: state.vertexConfig,
              bedrock: state.bedrockConfig,
              azure: state.azureConfig,
            }
            const tierConfig = config as unknown as Record<string, { provider?: string }>
            for (const tier of ['main', 'high', 'medium', 'low']) {
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
              config as unknown as Record<string, unknown>,
              Object.keys(providerKeys).length > 0 ? providerKeys : undefined
            ).catch(() => {})
          }
        },

        loadDelegationTierDefaults: async () => {
          if (get().delegationTierDefaultsStatus === 'loading') return
          set({ delegationTierDefaultsStatus: 'loading' })
          try {
            // Try to load saved config from workspace file first
            try {
              const saved = await agentApi.getDelegationTierConfig()
              const hasSaved = saved && (saved.provider || saved.main || saved.high || saved.medium || saved.low ||
                (saved.custom && Object.keys(saved.custom as object).length > 0))
              if (hasSaved) {
                set({
                  delegationTierConfig: saved as unknown as DelegationTierConfig,
                  delegationTierDefaultsStatus: 'loaded',
                })
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
            set({ delegationTierDefaultsStatus: 'loaded' })
          } catch (error) {
            console.warn('Failed to load delegation tier defaults:', error)
            set({ delegationTierDefaultsStatus: 'error' })
          }
        },

        // Library management
        saveLLM: async (llm, name, _modelName, _authMethod, _metadata) => {
          const { refreshAvailableLLMs, supportedProviders, providerManifest } = get()
          const knownProvider =
            !FRONTEND_DEPRECATED_PROVIDER_IDS.has(llm.provider) && (
              supportedProviders.includes(llm.provider) ||
              providerManifest.some(provider => provider.id === llm.provider && !provider.deprecated)
            )
          if (!knownProvider) {
            throw new Error(`Provider ${llm.provider} is not available as a published chat LLM`)
          }
          // Always fetch the current list from backend to avoid overwriting
          // previously published LLMs when frontend state is stale/empty
          const existingLLMs = filterPublishedLLMs(await llmConfigService.getPublishedLLMs().catch(() => get().savedLLMs))
          const newSavedLLM = sanitizeSavedLLM({
            ...llm,
            id: crypto.randomUUID(),
            name,
            created_at: new Date().toISOString()
          })
          const nextSavedLLMs = filterPublishedLLMs([...(existingLLMs || []), newSavedLLM])

          await llmConfigService.savePublishedLLMs(persistablePublishedLLMs(nextSavedLLMs))
          set({ savedLLMs: nextSavedLLMs })
          await refreshAvailableLLMs()
        },

        deleteSavedLLM: async (id) => {
          const { refreshAvailableLLMs } = get()
          // Always fetch the current list from backend to avoid overwriting
          const existingLLMs = filterPublishedLLMs(await llmConfigService.getPublishedLLMs().catch(() => get().savedLLMs))
          const nextSavedLLMs = (existingLLMs || []).filter(llm => llm.id !== id)

          await llmConfigService.savePublishedLLMs(persistablePublishedLLMs(nextSavedLLMs))
          set({ savedLLMs: nextSavedLLMs })
          await refreshAvailableLLMs()
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


        // Load defaults from backend
        loadDefaultsFromBackend: async () => {
          try {
            set({ isLoadingLLMs: true })
            const [defaults, loadedProviderKeys, loadedPublishedLLMs] = await Promise.all([
              llmConfigService.getLLMDefaults(),
              providerKeysApi.load().catch(() => undefined),
              llmConfigService.getPublishedLLMs().catch(() => undefined),
            ])

            // Get current state to check if user has already selected a model
            const currentState = get()
            // Check if user has made a selection (both provider and model_id should be set)
            const hasUserSelection = currentState.primaryConfig.provider && 
                                     currentState.primaryConfig.model_id && 
                                     currentState.primaryConfig.model_id.trim() !== ''
            
            // Preserve user configurations from current state (loaded from localStorage)
            // Merge backend defaults with saved config, prioritizing saved values
            const preserveUserConfig = (savedConfig: ExtendedLLMConfiguration, defaultConfig?: ExtendedLLMConfiguration): ExtendedLLMConfiguration => {
              // Use saved config as base, only fill in missing fields from defaults
              // Check if savedConfig has meaningful values (not just initial empty state)
              const hasSavedModel = savedConfig?.model_id && savedConfig.model_id.trim() !== ''

              return {
                provider: savedConfig?.provider || defaultConfig?.provider || DEFAULT_CHAT_PROVIDER,
                // Preserve model_id from saved config (including custom models) if it exists
                // Otherwise use default
                model_id: hasSavedModel ? savedConfig.model_id : (defaultConfig?.model_id || ''),
                // Preserve API key if it exists in saved config
                api_key: savedConfig?.api_key || defaultConfig?.api_key || '',
                // Preserve region for Bedrock and Azure
                region: savedConfig?.region || defaultConfig?.region,
	                // Preserve endpoint for Azure
	                endpoint: savedConfig?.endpoint || defaultConfig?.endpoint,
	                // Preserve options (includes api_version for Azure, reasoning settings, etc.)
	                options: savedConfig?.options || defaultConfig?.options
              }
            }

            const localProviderKeys = extractStoredProviderKeysFromState(currentState)
            let workspaceProviderKeys = loadedProviderKeys
            if (!hasStoredProviderKeys(workspaceProviderKeys) && hasStoredProviderKeys(localProviderKeys)) {
              try {
                await providerKeysApi.save(localProviderKeys)
                workspaceProviderKeys = localProviderKeys
              } catch (error) {
                console.warn('Failed to migrate provider keys from legacy local storage:', error)
                workspaceProviderKeys = localProviderKeys
              }
            }

            const localPublishedLLMs = filterPublishedLLMs((currentState.savedLLMs || []).map(sanitizeSavedLLM))
            const localPersistedPublishedLLMs = persistablePublishedLLMs(localPublishedLLMs)
            const loadedSanitizedPublishedLLMs = Array.isArray(loadedPublishedLLMs)
              ? loadedPublishedLLMs.map(sanitizeSavedLLM)
              : []
            let workspacePublishedLLMs = Array.isArray(loadedPublishedLLMs)
              ? filterPublishedLLMs(loadedSanitizedPublishedLLMs)
              : []
            let workspacePersistedPublishedLLMs = persistablePublishedLLMs(workspacePublishedLLMs)
            const loadedPersistedPublishedLLMs = loadedSanitizedPublishedLLMs.filter(llm => !isAutoPublishedLLM(llm))
            if (Array.isArray(loadedPublishedLLMs) && loadedPersistedPublishedLLMs.length !== workspacePersistedPublishedLLMs.length) {
              try {
                await llmConfigService.savePublishedLLMs(workspacePersistedPublishedLLMs)
              } catch (error) {
                console.warn('Failed to remove deprecated published LLM providers from workspace storage:', error)
              }
            }
            if (workspacePersistedPublishedLLMs.length === 0 && localPersistedPublishedLLMs.length > 0) {
              try {
                await llmConfigService.savePublishedLLMs(localPersistedPublishedLLMs)
                workspacePublishedLLMs = [
                  ...workspacePublishedLLMs.filter(isAutoPublishedLLM),
                  ...localPersistedPublishedLLMs,
                ]
                workspacePersistedPublishedLLMs = localPersistedPublishedLLMs
              } catch (error) {
                console.warn('Failed to migrate published LLMs from legacy local storage:', error)
                workspacePublishedLLMs = [
                  ...workspacePublishedLLMs.filter(isAutoPublishedLLM),
                  ...localPersistedPublishedLLMs,
                ]
                workspacePersistedPublishedLLMs = localPersistedPublishedLLMs
              }
            }

            const locked = !!defaults.llm_config_locked
            const defaultPublishedLocked = !!defaults.default_published_llms_locked
            const defaultList = Array.isArray(defaults.default_published_llms)
              ? filterPublishedLLMs((defaults.default_published_llms as SavedLLM[]).map(sanitizeSavedLLM))
              : []

            let newSavedLLMs = workspacePublishedLLMs
            if (defaultList.length > 0) {
              if (defaultPublishedLocked) {
                newSavedLLMs = defaultList
              } else {
                const byId = new Map(newSavedLLMs.map((llm) => [llm.id, llm]))
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

            const openrouterConfig = preserveUserConfig(currentState.openrouterConfig, defaults.openrouter_config)
            const bedrockConfig = preserveUserConfig(currentState.bedrockConfig, defaults.bedrock_config)
            const openaiConfig = preserveUserConfig(currentState.openaiConfig, defaults.openai_config)
            const vertexConfig = preserveUserConfig(
              currentState.vertexConfig,
              defaults.vertex_config || {
                provider: 'vertex',
                model_id: '',
                api_key: ''
              }
            )
            const anthropicConfig = preserveUserConfig(
              currentState.anthropicConfig,
              defaults.anthropic_config || {
                provider: 'anthropic',
                model_id: '',
                api_key: ''
              }
            )
            const azureConfig = preserveUserConfig(
              currentState.azureConfig,
              defaults.azure_config || {
                provider: 'azure',
                model_id: '',
                api_key: '',
                endpoint: ''
              }
            )
            const zaiConfig = preserveUserConfig(
              currentState.zaiConfig,
              defaults.zai_config || {
                provider: 'z-ai',
                model_id: '',
                api_key: ''
              }
            )
            const kimiConfig = preserveUserConfig(
              currentState.kimiConfig,
              defaults.kimi_config || {
                provider: 'kimi',
                model_id: '',
                api_key: ''
              }
            )
            if (workspaceProviderKeys?.openai) openaiConfig.api_key = workspaceProviderKeys.openai
            if (workspaceProviderKeys?.anthropic) anthropicConfig.api_key = workspaceProviderKeys.anthropic
            if (workspaceProviderKeys?.zai) zaiConfig.api_key = workspaceProviderKeys.zai
            if (workspaceProviderKeys?.kimi) kimiConfig.api_key = workspaceProviderKeys.kimi
            if (workspaceProviderKeys?.vertex) vertexConfig.api_key = workspaceProviderKeys.vertex
            if (workspaceProviderKeys?.bedrock?.region) {
              bedrockConfig.region = workspaceProviderKeys.bedrock.region
            }
            if (workspaceProviderKeys?.azure) {
              azureConfig.api_key = workspaceProviderKeys.azure.api_key
              azureConfig.endpoint = workspaceProviderKeys.azure.endpoint
              azureConfig.region = workspaceProviderKeys.azure.region || azureConfig.region
              azureConfig.options = {
                ...(azureConfig.options || {}),
                ...(workspaceProviderKeys.azure.api_version ? { api_version: workspaceProviderKeys.azure.api_version } : {}),
              }
            }

            let newPrimaryConfig = normalizePrimaryConfig(hasUserSelection ? currentState.primaryConfig : defaults.primary_config)
            if (locked && defaultList.length > 0) {
              const first = defaultList[0]
              newPrimaryConfig = {
                provider: first.provider,
                model_id: first.model_id,
              }
            }

            set({
              primaryConfig: newPrimaryConfig,
              openrouterConfig,
              bedrockConfig,
              openaiConfig,
              vertexConfig,
              anthropicConfig,
              azureConfig,
              zaiConfig,
              kimiConfig,
              savedLLMs: newSavedLLMs,
              availableBedrockModels: defaults.available_models.bedrock,
              availableOpenRouterModels: defaults.available_models.openrouter || [],
              availableOpenAIModels: defaults.available_models.openai,
              availableVertexModels: defaults.available_models.vertex || [],
              availableAnthropicModels: defaults.available_models.anthropic || [],
              availableAzureModels: defaults.available_models.azure || [],
              availableZAIModels: defaults.available_models['z-ai'] || [],
              availableKimiModels: defaults.available_models.kimi || [],
              supportedProviders: (() => {
                const sp = (defaults.supported_providers || SUPPORTED_PROVIDERS_FALLBACK).filter(provider =>
                  provider !== 'openrouter' && provider !== 'z-ai' && provider !== 'kimi' &&
                  provider !== 'minimax' && provider !== 'minimax-coding-plan' &&
                  provider !== 'elevenlabs' && provider !== 'deepgram'
                )
                console.log('[useLLMStore] supported_providers from backend:', defaults.supported_providers, '→ using:', sp)
                return sp
              })(),
              providerCapabilities: defaults.provider_capabilities || {},
              llmConfigLocked: locked,
              lockedProviders: defaults.locked_providers || [],
              defaultPublishedLLMsLocked: defaultPublishedLocked,
              defaultsLoaded: true,
              error: null,
              isLoadingLLMs: false
            })

            if (locked && defaultList.length > 0) {
              const first = defaultList[0]
              get().setChatPrimaryConfig({ provider: first.provider, model_id: first.model_id })
              get().setWorkflowPrimaryConfig({ provider: first.provider, model_id: first.model_id })
              get().setAgentConfig({ primary: first })
            }

            // Load provider manifest in parallel (non-blocking)
            get().loadProviderManifest()

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
          }
          
          // Set the first model for the explicitly selected provider
          set({
            primaryConfig: {
              ...state.primaryConfig,
              provider,
              model_id: availableModels[0] || '',
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

        refreshAvailableLLMs: async () => {
          const state = get()
          // Don't build list until backend defaults are loaded (so supported_providers is set)
          if (!state.defaultsLoaded) {
            set({ availableLLMs: [], modelMetadataCatalog: [], isLoadingLLMs: false })
            return
          }

          set({ isLoadingLLMs: true, error: null })

          try {
            const currentState = get()
            const availableLLMs: LLMOption[] = []
            let modelMetadataCatalog: ModelMetadata[] = []

            // Fetch model metadata for cost/context info
            const metadataMap: Record<string, { contextWindow: number; inputCost: number; outputCost: number }> = {}
            try {
              const metadataResponse = await llmConfigService.getModelMetadata()
              modelMetadataCatalog = metadataResponse.models
              metadataResponse.models.forEach(m => {
                metadataMap[`${m.provider}:${m.model_id}`] = {
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

              const metadata = metadataMap[`${savedLLM.provider}:${savedLLM.model_id}`]
              availableLLMs.push({
                id: savedLLM.id,
                provider: savedLLM.provider,
                model: savedLLM.model_id,
                label: savedLLM.name || `${savedLLM.provider} - ${savedLLM.model_id}`,
                description: savedLLM.model_name || `Published ${savedLLM.provider} model`,
                options: savedLLM.options,
                contextWindow: metadata?.contextWindow,
                inputCostPer1M: metadata?.inputCost,
                outputCostPer1M: metadata?.outputCost
              })
            })

            set({ availableLLMs, modelMetadataCatalog, isLoadingLLMs: false })
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
            return get().modelMetadataCatalog.some(model => model.model_id === modelId)
          }
        },

        // Generic actions
        reset: () => {
          set({
            primaryConfig: defaultLLMConfiguration(),
            agentConfig: null,
            openrouterConfig: {
              provider: 'openrouter',
              model_id: '',
              api_key: ''
            },
            bedrockConfig: {
              provider: 'bedrock',
              model_id: '',
              region: 'us-east-1'
            },
            openaiConfig: {
              provider: 'openai',
              model_id: '',
              api_key: ''
            },
            vertexConfig: {
              provider: 'vertex',
              model_id: '',
              api_key: ''
            },
            azureConfig: {
              provider: 'azure',
              model_id: '',
              api_key: '',
              endpoint: ''
            },
            zaiConfig: {
              provider: 'z-ai',
              model_id: '',
              api_key: ''
            },
            kimiConfig: {
              provider: 'kimi',
              model_id: '',
              api_key: ''
            },
            savedLLMs: [],
            showLLMModal: false,
            availableLLMs: [],
            modelMetadataCatalog: [],
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
          // Persist user configurations and custom models, but keep secrets/workspace-backed
          // LLM library data out of localStorage.
          // Legacy configs (kept for backward compatibility)
          primaryConfig: stripRetiredLLMFallbacks(state.primaryConfig),
          agentConfig: sanitizeAgentConfig(state.agentConfig),
          // Mode-specific configs
          chatPrimaryConfig: stripRetiredLLMFallbacks(state.chatPrimaryConfig),
          chatAgentConfig: sanitizeAgentConfig(state.chatAgentConfig),
          workflowPrimaryConfig: stripRetiredLLMFallbacks(state.workflowPrimaryConfig),
          workflowAgentConfig: sanitizeAgentConfig(state.workflowAgentConfig),
          // Other persisted state
          bedrockConfig: sanitizeProviderConfigForPersistence(state.bedrockConfig),
          openaiConfig: sanitizeProviderConfigForPersistence(state.openaiConfig),
          vertexConfig: sanitizeProviderConfigForPersistence(state.vertexConfig),
          anthropicConfig: sanitizeProviderConfigForPersistence(state.anthropicConfig),
          azureConfig: sanitizeProviderConfigForPersistence(state.azureConfig),
          zaiConfig: sanitizeProviderConfigForPersistence(state.zaiConfig),
          kimiConfig: sanitizeProviderConfigForPersistence(state.kimiConfig),
          customBedrockModels: state.customBedrockModels,
          customOpenAIModels: state.customOpenAIModels,
          customVertexModels: state.customVertexModels,
          customAzureModels: state.customAzureModels,
          showLLMModal: state.showLLMModal,
          delegationTierConfig: stripRetiredLLMFallbacks(state.delegationTierConfig),
          // DO NOT persist availableBedrockModels, availableOpenRouterModels, availableOpenAIModels
          // These should always be loaded fresh from backend
          // DO NOT persist defaultsLoaded - this should be reset on each app load
        }),
        // Migration: copy legacy config to mode-specific configs on first load
        onRehydrateStorage: () => (state) => {
          if (state) {
            state.delegationTierConfig = stripRetiredLLMFallbacks(state.delegationTierConfig)
            state.primaryConfig = normalizePrimaryConfig(state.primaryConfig)
            state.chatPrimaryConfig = normalizePrimaryConfig(state.chatPrimaryConfig)
            state.workflowPrimaryConfig = normalizePrimaryConfig(state.workflowPrimaryConfig)
            state.agentConfig = sanitizeAgentConfig(state.agentConfig)
            state.chatAgentConfig = sanitizeAgentConfig(state.chatAgentConfig)
            state.workflowAgentConfig = sanitizeAgentConfig(state.workflowAgentConfig)
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

// --- Auto-sync provider API keys to server (workspace-backed storage) ---
// Debounced: saves 2 seconds after the last key change to avoid spamming on every keystroke.
let _syncTimer: ReturnType<typeof setTimeout> | null = null

function syncProviderKeysToServer() {
  if (_syncTimer) clearTimeout(_syncTimer)
  _syncTimer = setTimeout(async () => {
    try {
      const s = useLLMStore.getState()
      const piSavedKey = unmaskedProviderKey(s.savedLLMs.find(llm => llm.provider === 'pi-cli' && unmaskedProviderKey(llm.api_key))?.api_key)
      const azureApiKey = unmaskedProviderKey(s.azureConfig?.api_key)
      await providerKeysApi.save({
        openai: unmaskedProviderKey(s.openaiConfig?.api_key),
        anthropic: unmaskedProviderKey(s.anthropicConfig?.api_key),
        zai: unmaskedProviderKey(s.zaiConfig?.api_key),
        kimi: unmaskedProviderKey(s.kimiConfig?.api_key),
        vertex: unmaskedProviderKey(s.vertexConfig?.api_key),
        pi_cli: piSavedKey,
        bedrock: s.bedrockConfig?.region ? { region: s.bedrockConfig.region } : undefined,
        azure: s.azureConfig?.endpoint && azureApiKey
          ? {
              endpoint: s.azureConfig.endpoint,
              api_key: azureApiKey,
              api_version: (s.azureConfig.options?.api_version as string) || undefined,
              region: s.azureConfig.region || undefined,
            }
          : undefined,
      })
    } catch (error) {
      console.warn('Failed to sync provider keys to workspace config:', error)
    }
  }, 2000)
}

const getProviderKeySnapshot = (state: LLMState) => ([
  state.openaiConfig?.api_key,
  state.anthropicConfig?.api_key,
  state.zaiConfig?.api_key,
  state.kimiConfig?.api_key,
  state.vertexConfig?.api_key,
  state.azureConfig?.api_key,
  state.azureConfig?.endpoint,
  state.bedrockConfig?.region,
])

// Watch for changes to any provider config or API key
useLLMStore.subscribe((state, prevState) => {
  const nextSnapshot = getProviderKeySnapshot(state)
  const prevSnapshot = getProviderKeySnapshot(prevState)
  if (JSON.stringify(nextSnapshot) !== JSON.stringify(prevSnapshot)) {
    syncProviderKeysToServer()
  }
})
