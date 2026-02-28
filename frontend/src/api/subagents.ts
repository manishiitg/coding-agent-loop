import axios from 'axios';
import { getApiBaseUrl, getAuthToken } from '../services/api';
import type {
  SubAgent,
  UpdateSubAgentRequest,
  ListSubAgentsResponse,
  ValidateSubAgentRequest,
  ValidateSubAgentResponse,
  ImportSubAgentRequest,
  ImportSubAgentResponse
} from '../types/subagents';

const API_BASE_URL = getApiBaseUrl();

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Add auth token interceptor
api.interceptors.request.use((config) => {
  const authToken = getAuthToken()
  if (authToken && config.headers) {
    config.headers['Authorization'] = `Bearer ${authToken}`
  }
  return config
})

export const subagentsApi = {
  // List all sub-agent templates
  listSubAgents: async (): Promise<ListSubAgentsResponse> => {
    const response = await api.get('/api/subagents');
    return response.data;
  },

  // Get a specific sub-agent template by name
  getSubAgent: async (name: string): Promise<SubAgent> => {
    const response = await api.get(`/api/subagents/${encodeURIComponent(name)}`);
    return response.data;
  },

  // Update a sub-agent template's content
  updateSubAgent: async (name: string, request: UpdateSubAgentRequest): Promise<SubAgent> => {
    const response = await api.put(`/api/subagents/${encodeURIComponent(name)}`, request);
    return response.data;
  },

  // Delete a sub-agent template
  deleteSubAgent: async (name: string): Promise<void> => {
    await api.delete(`/api/subagents/${encodeURIComponent(name)}`);
  },

  // Validate a sub-agent from GitHub URL
  validateSubAgent: async (request: ValidateSubAgentRequest): Promise<ValidateSubAgentResponse> => {
    const response = await api.post('/api/subagents/validate', request);
    return response.data;
  },

  // Import a sub-agent from GitHub URL
  importSubAgent: async (request: ImportSubAgentRequest): Promise<ImportSubAgentResponse> => {
    const response = await api.post('/api/subagents/import', request);
    return response.data;
  }
};

export default subagentsApi;
