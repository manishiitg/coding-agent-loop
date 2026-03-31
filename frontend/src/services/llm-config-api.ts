import axios from 'axios'
import { getAuthToken } from './api'
import type {
  LLMDefaultsResponse,
  APIKeyValidationRequest,
  APIKeyValidationResponse,
  DelegationTierConfig,
  SavedLLM,
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
}

export interface GetModelMetadataResponse {
  models: ModelMetadata[]
}

// Create axios instance for LLM configuration API (use Vite env so deploy URL works)
const llmConfigApi = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || (import.meta.env.DEV ? 'http://localhost:8000' : ''),
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

  // Validate API key with backend
  validateAPIKey: async (request: APIKeyValidationRequest): Promise<APIKeyValidationResponse> => {
    const response = await llmConfigApi.post('/api/llm-config/validate-key', request)
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
