import axios from 'axios'
import { getApiBaseUrl, getAuthToken } from '../services/api'
import type { PrototypeProject, PrototypeProjectConfig, DeploymentRecord } from '../stores/useCodePrototypeStore'

const api = axios.create({ baseURL: getApiBaseUrl(), headers: { 'Content-Type': 'application/json' } })

api.interceptors.request.use((config) => {
  const token = getAuthToken()
  if (token && config.headers) config.headers['Authorization'] = `Bearer ${token}`
  return config
})

export interface DeployRequest {
  project_name: string
  provider: 'k8s' | 'vercel' | 'railway'
}

export interface DeployResponse {
  url: string
  logs: string
  deployment_id: string
}

export const codePrototypeApi = {
  listProjects: async (): Promise<PrototypeProject[]> => {
    console.log('[CODE-PROTOTYPE] listProjects()')
    const r = await api.get('/api/code-prototype/projects')
    console.log('[CODE-PROTOTYPE] listProjects result:', r.data)
    return r.data
  },

  createProject: async (payload: {
    name: string
    type: string
    description?: string
    config: PrototypeProjectConfig
  }): Promise<PrototypeProject> => {
    console.log('[CODE-PROTOTYPE] createProject payload:', payload)
    const r = await api.post('/api/code-prototype/projects', payload)
    console.log('[CODE-PROTOTYPE] createProject result:', r.data)
    return r.data
  },

  getProject: async (name: string): Promise<PrototypeProject> => {
    console.log('[CODE-PROTOTYPE] getProject:', name)
    const r = await api.get(`/api/code-prototype/projects/${encodeURIComponent(name)}`)
    console.log('[CODE-PROTOTYPE] getProject result:', r.data)
    return r.data
  },

  updateConfig: async (name: string, config: PrototypeProjectConfig): Promise<void> => {
    console.log('[CODE-PROTOTYPE] updateConfig:', name, config)
    await api.patch(`/api/code-prototype/projects/${encodeURIComponent(name)}/config`, config)
    console.log('[CODE-PROTOTYPE] updateConfig done')
  },

  deleteProject: async (name: string): Promise<void> => {
    console.log('[CODE-PROTOTYPE] deleteProject:', name)
    await api.delete(`/api/code-prototype/projects/${encodeURIComponent(name)}`)
    console.log('[CODE-PROTOTYPE] deleteProject done')
  },

  deploy: async (req: DeployRequest): Promise<DeployResponse> => {
    console.log('[CODE-PROTOTYPE] deploy request:', req)
    const r = await api.post('/api/code-prototype/deploy', req, { timeout: 120_000 })
    console.log('[CODE-PROTOTYPE] deploy result:', r.data)
    return r.data
  },

  undeploy: async (projectName: string): Promise<void> => {
    console.log('[CODE-PROTOTYPE] undeploy:', projectName)
    await api.delete(`/api/code-prototype/deploy/${encodeURIComponent(projectName)}`)
    console.log('[CODE-PROTOTYPE] undeploy done')
  },
}

export type { PrototypeProject, PrototypeProjectConfig, DeploymentRecord }
