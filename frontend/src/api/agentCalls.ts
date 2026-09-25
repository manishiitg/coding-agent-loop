import axios from 'axios'

export interface AgentCallResult {
  status: 'queued' | 'working' | 'completed' | 'failed' | 'interrupted' | 'refused'
  call_id?: string
  code?: string
  reason?: string
  problems?: string[]
  result?: unknown
  answer?: string
  error?: string
  progress?: { at: string; message: string; percent?: number }[]
  run_id?: string
  late?: boolean
  timed_out?: boolean
}

async function config() {
  const { getApiBaseUrl, getAuthToken } = await import('../services/api')
  const token = getAuthToken()
  return { baseURL: getApiBaseUrl(), headers: token ? { Authorization: `Bearer ${token}` } : {} }
}

async function call(name: string, args: Record<string, unknown>): Promise<AgentCallResult> {
  const response = await axios.post<AgentCallResult>('/api/external/v1/call', { name, arguments: args }, await config())
  return response.data
}

export const agentCallsApi = {
  run: (target: string, functionName: string, args: Record<string, unknown>) => call('call_function', { target, function: functionName, args, wait_seconds: 0 }),
  ask: (target: string, message: string) => call('ask', { target, message, wait_seconds: 0 }),
  get: (callId: string) => call('get_call', { call_id: callId, wait_seconds: 0 }),
}
