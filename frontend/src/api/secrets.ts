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

  deleteStoredSecret: async (name: string): Promise<{ success: boolean }> => {
    const response = await api.delete(`/api/secrets/store/${encodeURIComponent(name)}`);
    return response.data;
  },

  listStoredSecrets: async (): Promise<{ name: string }[]> => {
    const response = await api.get('/api/secrets/stored');
    return response.data;
  },
};

export default secretsApi;
