import axios from 'axios'
import type { 
  LLMDefaultsResponse,
  APIKeyValidationRequest,
  APIKeyValidationResponse
} from './api-types'

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
}

export default llmConfigService
