import axios from 'axios';
import type {
  Skill,
  ImportSkillRequest,
  ImportSkillResponse,
  ValidateSkillRequest,
  ValidateSkillResponse,
  UpdateSkillRequest,
  ListSkillsResponse,
} from '../types/skills';

const API_BASE_URL = 'http://localhost:8000';

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
});

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
