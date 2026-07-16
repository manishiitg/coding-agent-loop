import axios from 'axios';
import { getApiBaseUrl, getAuthToken } from '../services/api';

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

export const secretsApi = {
  encrypt: async (value: string): Promise<{ encrypted: string }> => {
    const response = await api.post('/api/secrets/encrypt', { value });
    return response.data;
  },

  decrypt: async (encrypted: string): Promise<{ value: string }> => {
    const response = await api.post('/api/secrets/decrypt', { encrypted });
    return response.data;
  },

  getGlobalSecrets: async (): Promise<{ name: string }[]> => {
    const response = await api.get('/api/secrets/global');
    return response.data;
  },

  storeSecret: async (name: string, encryptedValue: string): Promise<{ success: boolean }> => {
    const response = await api.put('/api/secrets/store', { name, encrypted_value: encryptedValue });
    return response.data;
  },

  storeWorkflowSecret: async (workspacePath: string, name: string, encryptedValue: string): Promise<{ success: boolean }> => {
    const response = await api.put('/api/secrets/workflow/store', {
      workspace_path: workspacePath,
      name,
      encrypted_value: encryptedValue,
    });
    return response.data;
  },

  deleteStoredSecret: async (name: string): Promise<{ success: boolean }> => {
    const response = await api.delete(`/api/secrets/store/${encodeURIComponent(name)}`);
    return response.data;
  },

  deleteWorkflowSecret: async (workspacePath: string, name: string): Promise<{ success: boolean }> => {
    const response = await api.delete(`/api/secrets/workflow/store/${encodeURIComponent(name)}`, {
      params: { workspace_path: workspacePath },
    });
    return response.data;
  },

  listStoredSecrets: async (): Promise<{ name: string }[]> => {
    const response = await api.get('/api/secrets/stored');
    return response.data;
  },

  listWorkflowSecrets: async (workspacePath: string): Promise<{ name: string }[]> => {
    const response = await api.get('/api/secrets/workflow/stored', {
      params: { workspace_path: workspacePath },
    });
    return response.data;
  },

  getWorkflowClaudeCodeCredentialStatus: async (workspacePath: string): Promise<{ configured: boolean; updated_at?: string }> => {
    const response = await api.get('/api/workflow-provider-credentials/claude-code', {
      params: { workspace_path: workspacePath },
    });
    return response.data;
  },

  storeWorkflowClaudeCodeCredential: async (workspacePath: string, token: string): Promise<{ success: boolean }> => {
    const encrypted = await secretsApi.encrypt(token);
    const response = await api.put('/api/workflow-provider-credentials/claude-code', {
      workspace_path: workspacePath,
      encrypted_value: encrypted.encrypted,
    }, { timeout: 20_000 });
    return response.data;
  },

  deleteWorkflowClaudeCodeCredential: async (workspacePath: string): Promise<{ success: boolean }> => {
    const response = await api.delete('/api/workflow-provider-credentials/claude-code', {
      params: { workspace_path: workspacePath },
    });
    return response.data;
  },
};

export default secretsApi;
