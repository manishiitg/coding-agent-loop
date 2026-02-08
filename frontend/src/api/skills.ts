import axios from 'axios';
import { getApiBaseUrl, getAuthToken } from '../services/api';
import type {
  Skill,
  ImportSkillRequest,
  ImportSkillResponse,
  ValidateSkillRequest,
  ValidateSkillResponse,
  UpdateSkillRequest,
  ListSkillsResponse,
} from '../types/skills';

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

export const skillsApi = {
  // List all skills
  listSkills: async (): Promise<ListSkillsResponse> => {
    const response = await api.get('/api/skills');
    return response.data;
  },

  // Get a specific skill by name
  getSkill: async (name: string): Promise<Skill> => {
    const response = await api.get(`/api/skills/${encodeURIComponent(name)}`);
    return response.data;
  },

  // Import a skill from GitHub
  importSkill: async (request: ImportSkillRequest): Promise<ImportSkillResponse> => {
    const response = await api.post('/api/skills/import', request);
    return response.data;
  },

  // Validate a GitHub URL before importing
  validateSkill: async (request: ValidateSkillRequest): Promise<ValidateSkillResponse> => {
    const response = await api.post('/api/skills/validate', request);
    return response.data;
  },

  // Validate a skill from uploaded zip file
  validateSkillZip: async (file: File): Promise<ValidateSkillResponse> => {
    const formData = new FormData();
    formData.append('file', file);
    const response = await api.post('/api/skills/validate-zip', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    });
    return response.data;
  },

  // Import a skill from uploaded zip file
  importSkillZip: async (file: File): Promise<ImportSkillResponse> => {
    const formData = new FormData();
    formData.append('file', file);
    const response = await api.post('/api/skills/import-zip', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    });
    return response.data;
  },

  // Update a skill's content
  updateSkill: async (name: string, request: UpdateSkillRequest): Promise<Skill> => {
    const response = await api.put(`/api/skills/${encodeURIComponent(name)}`, request);
    return response.data;
  },

  // Delete a skill
  deleteSkill: async (name: string): Promise<void> => {
    await api.delete(`/api/skills/${encodeURIComponent(name)}`);
  },
};

export default skillsApi;
