import axios from 'axios'
import { getApiBaseUrl, getAuthToken } from '../services/api'
import type {
  UserCommand,
  CreateCommandRequest,
  UpdateCommandRequest,
  ListCommandsResponse,
} from '../types/commands'

const API_BASE_URL = getApiBaseUrl()

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

api.interceptors.request.use((config) => {
  const authToken = getAuthToken()
  if (authToken && config.headers) {
    config.headers['Authorization'] = `Bearer ${authToken}`
  }
  return config
})

export const commandsApi = {
  listCommands: async (): Promise<ListCommandsResponse> => {
    const response = await api.get('/api/commands')
    return response.data
  },

  getCommand: async (name: string): Promise<UserCommand> => {
    const response = await api.get(`/api/commands/${encodeURIComponent(name)}`)
    return response.data
  },

  createCommand: async (request: CreateCommandRequest): Promise<UserCommand> => {
    const response = await api.post('/api/commands', request)
    return response.data
  },

  updateCommand: async (name: string, request: UpdateCommandRequest): Promise<UserCommand> => {
    const response = await api.put(`/api/commands/${encodeURIComponent(name)}`, request)
    return response.data
  },

  deleteCommand: async (name: string): Promise<void> => {
    await api.delete(`/api/commands/${encodeURIComponent(name)}`)
  },
}

export default commandsApi
