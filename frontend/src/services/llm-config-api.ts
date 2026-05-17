import axios from 'axios'
import { getApiBaseUrl, getAuthToken } from './api'
import type {
  LLMDefaultsResponse,
  APIKeyValidationRequest,
  APIKeyValidationResponse,
  DelegationTierConfig,
  SavedLLM,
  LLMDiscoveryResponse,
} from './api-types'

export interface ModelMetadata {
  model_id: string
  model_name: string
  context_window: number
  input_cost_per_1m: number
  output_cost_per_1m: number
  reasoning_cost_per_1m?: number
  cached_input_cost_per_1m?: number
  cached_input_cost_write_per_1m?: number
  provider: string
  supports_reasoning_effort?: boolean
  reasoning_effort_levels?: string[]
  supports_thinking_level?: boolean
  thinking_levels?: string[]
  supports_thinking_budget?: boolean
  model_selection_mode?: 'fixed_tier' | 'dynamic'
}

// --- Provider Manifest types (API-driven provider discovery) ---

export interface ProviderManifestEntry {
  id: string
  display_name: string
  description: string
  kind: 'local_cli' | 'api'
  integration_kind: 'coding_agent' | 'api_model' | 'audio_provider'
  model_selection_mode: 'fixed_tier' | 'dynamic'
  auth_description: string
  runtime_command?: string
  runtime_available?: boolean
  auth_configured: boolean
  auth_source?: string
  usable: boolean
  setup_hint?: string
  requires_api_key: boolean
  supports_dynamic_models: boolean
  default_model_id: string
  models: ModelMetadata[]
  capabilities: string[]
  api_key_env?: string
  api_key_url?: string
}

export interface IntegrationKindInfo {
  label: string
  description: string
}

export interface ProviderManifestResponse {
  providers: ProviderManifestEntry[]
  integration_kinds: Record<string, IntegrationKindInfo>
  provider_order: string[]
}

export interface DynamicModelEntry {
  model_id: string
  model_name: string
  group?: string
  is_default?: boolean
  context_window?: number
  cost_input?: number
  cost_output?: number
}

export interface DynamicModelsResponse {
  provider: string
  model_selection_mode: string
  models: DynamicModelEntry[]
  groups?: string[]
  supports_custom_model?: boolean
  custom_model_hint?: string
  source: string
  cached_at?: string
  cache_ttl_seconds?: number
}

export interface GetModelMetadataResponse {
  models: ModelMetadata[]
}

// Create axios instance for LLM configuration API (use Vite env so deploy URL works)
const llmConfigApi = axios.create({
  baseURL: getApiBaseUrl(),
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Add auth token interceptor
llmConfigApi.interceptors.request.use((config) => {
  const authToken = getAuthToken()
  if (authToken && config.headers) {
    config.headers['Authorization'] = `Bearer ${authToken}`
  }
  return config
})

// LLM Configuration API service
export const llmConfigService = {
  // Get LLM configuration defaults from backend
  getLLMDefaults: async (): Promise<LLMDefaultsResponse> => {
    const response = await llmConfigApi.get('/api/llm-config/defaults')
    return response.data
  },

  // Discover local CLI providers and server/workspace auth without making model calls
  discoverLLMSetup: async (): Promise<LLMDiscoveryResponse> => {
    const response = await llmConfigApi.get('/api/llm-config/discovery')
    return response.data
  },

  // Validate API key with backend
  validateAPIKey: async (request: APIKeyValidationRequest): Promise<APIKeyValidationResponse> => {
    const response = await llmConfigApi.post('/api/llm-config/validate-key', request, { timeout: 120000 })
    return response.data
  },

  // Get metadata for all available models
  getModelMetadata: async (): Promise<GetModelMetadataResponse> => {
    const response = await llmConfigApi.get('/api/llm-config/models/metadata')
    return response.data
  },

  // Get deployed models from Azure (requires endpoint and API key)
  getAzureDeployedModels: async (endpoint: string, apiKey: string): Promise<GetModelMetadataResponse & { error?: string }> => {
    const response = await llmConfigApi.post('/api/llm-config/azure/deployments', {
      endpoint,
      api_key: apiKey
    })
    return response.data
  },

  // Get comprehensive provider manifest (replaces hardcoded provider info)
  getProviderManifest: async (): Promise<ProviderManifestResponse> => {
    const response = await llmConfigApi.get('/api/llm-config/providers')
    return response.data
  },

  // Get dynamic model list for a provider (cursor-cli, opencode-cli, etc.)
  getProviderModels: async (provider: string): Promise<DynamicModelsResponse> => {
    const response = await llmConfigApi.get(`/api/llm-config/providers/${provider}/models`)
    return response.data
  },

  // Get delegation tier defaults from environment variables
  getDelegationTierDefaults: async (): Promise<DelegationTierConfig> => {
    const response = await llmConfigApi.get('/api/llm-config/delegation-tiers')
    return response.data
  },

  // Load published LLMs from the workspace config folder
  getPublishedLLMs: async (): Promise<SavedLLM[]> => {
    const response = await llmConfigApi.get('/api/published-llms')
    return response.data
  },

  // Save published LLMs to the workspace config folder
  savePublishedLLMs: async (llms: SavedLLM[]): Promise<{ status: string }> => {
    const response = await llmConfigApi.put('/api/published-llms', llms)
    return response.data
  },
}

export default llmConfigService
