import axios from 'axios'
import { getApiBaseUrl, getAuthToken } from '../services/api'
import type {
  ScheduledJob,
  CreateScheduledJobRequest,
  UpdateScheduledJobRequest,
  ListScheduledJobsResponse,
  ListScheduledJobRunsResponse,
  SchedulerConfig,
} from '../services/api-types'

const API_BASE_URL = getApiBaseUrl()

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: { 'Content-Type': 'application/json' },
})

api.interceptors.request.use((config) => {
  config.baseURL = getApiBaseUrl()
  const token = getAuthToken()
  if (token && config.headers) {
    config.headers['Authorization'] = `Bearer ${token}`
  }
  return config
})

export const schedulerApi = {
  getConfig: () =>
    api.get<SchedulerConfig>('/api/scheduler/config').then(r => r.data),

  updateConfig: (req: SchedulerConfig) =>
    api.put<SchedulerConfig>('/api/scheduler/config', req).then(r => r.data),

  listJobs: (params?: { entity_type?: string; enabled?: boolean; limit?: number; offset?: number; mode?: string }) =>
    api.get<ListScheduledJobsResponse>('/api/scheduler/jobs', { params }).then(r => r.data),

  getJob: (id: string) =>
    api.get<ScheduledJob>(`/api/scheduler/jobs/${id}`).then(r => r.data),

  createJob: (req: CreateScheduledJobRequest) =>
    api.post<ScheduledJob>('/api/scheduler/jobs', req).then(r => r.data),

  updateJob: (id: string, req: UpdateScheduledJobRequest) =>
    api.put<ScheduledJob>(`/api/scheduler/jobs/${id}`, req).then(r => r.data),

  deleteJob: (id: string) =>
    api.delete(`/api/scheduler/jobs/${id}`),

  enableJob: (id: string) =>
    api.post<ScheduledJob>(`/api/scheduler/jobs/${id}/enable`).then(r => r.data),

  disableJob: (id: string) =>
    api.post<ScheduledJob>(`/api/scheduler/jobs/${id}/disable`).then(r => r.data),

  triggerJob: (id: string) =>
    api.post<{ session_id: string }>(`/api/scheduler/jobs/${id}/trigger`).then(r => r.data),

  runWorkflowPulse: (workspacePath: string) =>
    api.post<{ run_id: string }>('/api/scheduler/workflows/pulse-run', {
      workspace_path: workspacePath,
    }).then(r => r.data),

  getJobRuns: (id: string, limit = 20) =>
    api.get<ListScheduledJobRunsResponse>(`/api/scheduler/jobs/${id}/runs`, { params: { limit } }).then(r => r.data),

  stopJob: (id: string) =>
    api.post<ScheduledJob>(`/api/scheduler/jobs/${id}/stop`).then(r => r.data),
}

// --- Provider API Keys (server-side encrypted storage) ---

export interface StoredProviderKeys {
  openrouter?: string
  openai?: string
  anthropic?: string
  zai?: string
  kimi?: string
  vertex?: string
  codex_cli?: string
  cursor_cli?: string
  pi_cli?: string
  minimax?: string
  minimax_coding_plan?: string
  elevenlabs?: string
  deepgram?: string
  pi_provider_keys?: Record<string, string>
  bedrock?: { region: string }
  azure?: { endpoint: string; api_key: string; api_version?: string; region?: string }
}

export const providerKeysApi = {
  save: (keys: StoredProviderKeys) =>
    api.put('/api/provider-keys', keys).then(r => r.data),

  load: () =>
    api.get<StoredProviderKeys>('/api/provider-keys').then(r => r.data),
}
