import axios from 'axios'
import { getApiBaseUrl, getAuthToken } from './api'

export type CLISecurityMode = 'compatibility' | 'isolated' | 'verified'

export interface CLISecurityCapability {
  id: string
  label: string
  reason: string
  risk: string
  read_path_templates?: string[]
  write_path_templates?: string[]
  environment?: string[]
}

export interface CLISecurityProfile {
  provider: string
  display_name: string
  version: string
  executables: string[]
  supports_private_home: boolean
  certified: boolean
  capabilities: CLISecurityCapability[]
}

export interface CLISecurityConfig {
  version: number
  mode: CLISecurityMode
  approved_profiles: Record<string, {
    profile_version: string
    capabilities: string[]
  }>
}

export interface CLISecurityStatus {
  config: CLISecurityConfig
  profiles: CLISecurityProfile[]
}

const client = axios.create({
  baseURL: getApiBaseUrl(),
  timeout: 30000,
  headers: { 'Content-Type': 'application/json' },
})

client.interceptors.request.use((config) => {
  const token = getAuthToken()
  if (token && config.headers) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

export const cliSecurityService = {
  get: async (): Promise<CLISecurityStatus> => {
    const response = await client.get('/api/cli-security')
    return response.data
  },
  update: async (config: CLISecurityConfig): Promise<CLISecurityStatus> => {
    const response = await client.put('/api/cli-security', config)
    return response.data
  },
}
