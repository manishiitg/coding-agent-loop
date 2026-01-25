import axios from 'axios'
import type { 
  LLMDefaultsResponse,
  APIKeyValidationRequest,
  APIKeyValidationResponse
} from './api-types'

export interface ModelMetadata {
  model_id: string
  model_name: string
  context_window: number
  input_cost_per_1m: number
  output_cost_per_1m: number
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

// Create axios instance for LLM configuration API
const llmConfigApi = axios.create({
  baseURL: process.env.NODE_ENV === 'production' 
    ? 'https://api.mcp-agent.com' 
    : 'http://localhost:8000',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
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
}

export default llmConfigService
