import axios from 'axios'
import type { 
  AgentQueryRequest, 
  AgentQueryResponse, 
  RegisterObserverResponse,
  GetEventsResponse,
  ObserverStatusResponse,
  MCPServerConfig,
  ChatSession,
  ListChatSessionsResponse,
  GetSessionEventsResponse,
  CreateChatSessionRequest,
  UpdateChatSessionRequest,
  PresetQuery,
  CreatePresetQueryRequest,
  UpdatePresetQueryRequest,
  ListPresetQueriesResponse,
  WorkflowStatusResponse,
  WorkflowConstantsResponse,
  WorkflowSelectedOptions,
  GetActiveSessionsResponse,
  ReconnectSessionResponse,
  SessionStatusResponse,
  LLMGuidanceResponse,
  HumanFeedbackResponse,
} from './api-types'

// Re-export types for other components to use
export type { 
  AgentQueryRequest, 
  AgentQueryResponse, 
  RegisterObserverResponse,
  GetEventsResponse,
  ObserverStatusResponse,
  MCPServerConfig,
  ToolDefinition,
  PollingEvent,
  AgentStreamEvent,
  RegisterObserverRequest,
  ChatSession,
  ChatEvent,
  ChatHistorySummary,
  ListChatSessionsResponse,
  GetSessionEventsResponse,
  CreateChatSessionRequest,
  UpdateChatSessionRequest,
  PresetQuery,
  CreatePresetQueryRequest,
  UpdatePresetQueryRequest,
  ListPresetQueriesResponse,
  WorkflowStatusResponse,
  WorkflowConstantsResponse
} from './api-types'

const API_BASE_URL = 'http://localhost:8000'
const PLANNER_API_BASE_URL = 'http://localhost:8081'

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

const plannerApi = axios.create({
  baseURL: PLANNER_API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

// --- Session ID Management ---
let sessionIdRef: string | null = null

export function getSessionId(): string {
  if (!sessionIdRef) {
    // Create a new session ID
    sessionIdRef = crypto.randomUUID()
  }
  return sessionIdRef
}

export function resetSessionId(): void {
  sessionIdRef = null
}

// --- Observer ID Management ---
function getObserverId(): string {
  const observerId = localStorage.getItem('agent_observer_id')
  if (!observerId) {
    // We'll get this from the server when we register
    return ''
  }
  return observerId
}

// --- Axios request interceptor to inject session ID ---
api.interceptors.request.use((config) => {
  config.headers = config.headers || {}
  config.headers['X-Session-ID'] = getSessionId()
  
  // Add observer ID if available
  const observerId = getObserverId()
  if (observerId) {
    config.headers['X-Observer-ID'] = observerId
  }
  
  return config
})

export const agentApi = {
  // Register a new observer
  registerObserver: async (sessionId?: string): Promise<RegisterObserverResponse> => {
    const response = await api.post('/api/observer/register', {
      session_id: sessionId || getSessionId()
    })
    const data = response.data
    
    // Store observer ID for future requests
    if (data.observer_id) {
      localStorage.setItem('agent_observer_id', data.observer_id)
      // Observer registered successfully
    } else {
      console.error('[API] No observer_id received from server')
    }
    
    return data
  },

  // Get events for an observer
  getEvents: async (observerId: string, sinceIndex?: number): Promise<GetEventsResponse> => {
    const params = sinceIndex !== undefined ? { since: sinceIndex } : {}
    const response = await api.get(`/api/observer/${observerId}/events`, { params })
    return response.data
  },

  // Get observer status
  getObserverStatus: async (observerId: string): Promise<ObserverStatusResponse> => {
    const response = await api.get(`/api/observer/${observerId}/status`)
    return response.data
  },

  // Remove observer
  removeObserver: async (observerId: string): Promise<void> => {
    await api.delete(`/api/observer/${observerId}`)
    localStorage.removeItem('agent_observer_id')
  },

  // Stop session/agent execution (preserves conversation history)
  stopSession: async (sessionId: string): Promise<void> => {
    await api.post('/api/session/stop', {}, {
      headers: { 'X-Session-ID': sessionId }
    })
  },

  // Clear session/conversation history (for new chat)
  clearSession: async (sessionId: string): Promise<void> => {
    await api.post('/api/session/clear', {}, {
      headers: { 'X-Session-ID': sessionId }
    })
  },

  // Active Session Management
  // Get all active sessions
  getActiveSessions: async (): Promise<GetActiveSessionsResponse> => {
    const response = await api.get('/api/sessions/active')
    return response.data
  },

  // Reconnect to an active session
  reconnectSession: async (sessionId: string): Promise<ReconnectSessionResponse> => {
    const response = await api.post(`/api/sessions/${sessionId}/reconnect`)
    return response.data
  },

  // Get session status (active, completed, or not found)
  getSessionStatus: async (sessionId: string): Promise<SessionStatusResponse> => {
    const response = await api.get(`/api/sessions/${sessionId}/status`)
    return response.data
  },

  // Start a new agent query
  startQuery: async (request: AgentQueryRequest): Promise<AgentQueryResponse> => {
    // Get the current observer ID from localStorage
    const observerId = localStorage.getItem('agent_observer_id')
    
    // Create headers with observer ID if available
    const headers: Record<string, string> = {}
    if (observerId) {
      headers['X-Observer-ID'] = observerId
      // Starting query with observer ID
    } else {
      console.warn('[API] No observer ID available for query')
    }
    
    const response = await api.post('/api/query', request, { headers })
    return response.data
  },

  // Get server health
  getHealth: async () => {
    const response = await api.get('/api/health')
    return response.data
  },

  // Get server capabilities
  getCapabilities: async () => {
    const response = await api.get('/api/capabilities')
    return response.data
  },


  // LLM Guidance Management
  // Set LLM guidance for a session
  setLLMGuidance: async (sessionId: string, guidance: string): Promise<LLMGuidanceResponse> => {
    const response = await api.post(`/api/sessions/${sessionId}/llm-guidance`, {
      session_id: sessionId,
      guidance: guidance
    }, {
      headers: {
        'X-Session-ID': sessionId
      }
    })
    return response.data
  },

  // Human Feedback Management
  // Submit human feedback response
  submitHumanFeedback: async (uniqueId: string, response: string): Promise<HumanFeedbackResponse> => {
    const apiResponse = await api.post('/api/human-feedback/submit', {
      unique_id: uniqueId,
      response: response
    })
    return apiResponse.data
  },

  // Get tool list and status
  getTools: async () => {
    const response = await api.get('/api/tools')
    return response.data
  },

  // Set enabled tools for a query/session
  setEnabledTools: async (queryId: string, enabledTools: string[]) => {
    const response = await api.post('/api/tools/enabled', {
      query_id: queryId,
      enabled_tools: enabledTools,
    })
    return response.data
  },

  // Add a new server/tool
  addServer: async (name: string, server: MCPServerConfig) => {
    const response = await api.post('/api/tools/add', { name, server })
    return response.data
  },

  // Edit an existing server/tool
  editServer: async (name: string, server: MCPServerConfig) => {
    const response = await api.post('/api/tools/edit', { name, server })
    return response.data
  },

  // Remove a server/tool
  removeServer: async (name: string) => {
    const response = await api.post('/api/tools/remove', { name })
    return response.data
  },

  getToolDetail: async (serverName: string) => {
    const response = await api.get(`/api/tools/detail?server_name=${encodeURIComponent(serverName)}`)
    return response.data
  },

  // Planner API - File Management
  getPlannerFiles: async (folder?: string, limit: number = 100) => {
    const params: Record<string, string | number> = { limit }
    if (folder) {
      params.folder = folder
    }
    const response = await plannerApi.get('/api/documents', { params })
    return response.data
  },

  getPlannerFileContent: async (filepath: string) => {
    // API handles path conversion internally
    const response = await plannerApi.get(`/api/documents/${encodeURIComponent(filepath)}`)
    return response.data
  },

  deletePlannerFile: async (filepath: string, commitMessage?: string) => {
    const params: Record<string, string> = { confirm: 'true' }
    if (commitMessage) {
      params.commit_message = commitMessage
    }
    // API handles path conversion internally
    const response = await plannerApi.delete(`/api/documents/${encodeURIComponent(filepath)}`, { params })
    return response.data
  },

  deletePlannerFolder: async (folderPath: string, commitMessage?: string) => {
    const params: Record<string, string> = { confirm: 'true' }
    if (commitMessage) {
      params.commit_message = commitMessage
    }
    const response = await plannerApi.delete(`/api/folders/${encodeURIComponent(folderPath)}`, { params })
    return response.data
  },

  deleteAllFilesInFolder: async (folderPath: string, commitMessage?: string) => {
    const params: Record<string, string> = { confirm: 'true' }
    if (commitMessage) {
      params.commit_message = commitMessage
    }
    const response = await plannerApi.delete(`/api/folders/${encodeURIComponent(folderPath)}/files`, { params })
    return response.data
  },

  uploadPlannerFile: async (file: File, folderPath: string, commitMessage?: string) => {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('folder_path', folderPath)
    if (commitMessage) {
      formData.append('commit_message', commitMessage)
    }
    
    const response = await plannerApi.post('/api/upload', formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    })
    return response.data
  },

  createPlannerFolder: async (folderPath: string, commitMessage?: string) => {
    const response = await plannerApi.post('/api/folders', {
      folder_path: folderPath,
      commit_message: commitMessage
    })
    return response.data
  },

  // Git Sync API
  getGitSyncStatus: async () => {
    const response = await plannerApi.get('/api/sync/status')
    return response.data
  },

  syncWithGitHub: async (force: boolean = false, commitMessage?: string) => {
    const response = await plannerApi.post('/api/sync/github', {
      force,
      commit_message: commitMessage,
      operation: 'sync'
    })
    return response.data
  },

  forcePushLocal: async (commitMessage?: string) => {
    const response = await plannerApi.post('/api/sync/github', {
      force: true,
      commit_message: commitMessage,
      operation: 'force_push_local'
    })
    return response.data
  },

  forcePullRemote: async () => {
    const response = await plannerApi.post('/api/sync/github', {
      force: true,
      operation: 'force_pull_remote'
    })
    return response.data
  },

  // Semantic Search Sync API
  getSemanticSearchStatus: async () => {
    const response = await plannerApi.get('/api/semantic/stats')
    return response.data
  },

  getSemanticJobStatus: async () => {
    const response = await plannerApi.get('/api/semantic/jobs')
    return response.data
  },

  triggerSemanticResync: async (dryRun: boolean = false, force: boolean = false) => {
    const response = await plannerApi.post('/api/semantic/resync', {
      dry_run: dryRun,
      force: force
    })
    return response.data
  },

  searchDocuments: async (params: { query: string; search_type?: string; folder?: string; limit?: number }) => {
    const response = await plannerApi.get('/api/search', { params })
    return response.data
  },

  searchSemanticDocuments: async (params: { 
    query: string; 
    folder?: string; 
    limit?: number; 
    similarity_threshold?: number; 
    include_regex?: boolean; 
    regex_limit?: number 
  }) => {
    const response = await plannerApi.get('/api/search/semantic', { params })
    return response.data
  },

  // File Version History API
  getFileVersions: async (filepath: string, limit: number = 10) => {
    const response = await plannerApi.get(`/api/versions/${encodeURIComponent(filepath)}`, {
      params: { limit }
    })
    return response.data
  },

  // Chat History API
  getChatSessions: async (limit: number = 20, offset: number = 0, presetQueryId?: string): Promise<ListChatSessionsResponse> => {
    const params: Record<string, string | number> = { limit, offset }
    if (presetQueryId) {
      params.preset_query_id = presetQueryId
    }
    const response = await api.get('/api/chat-history/sessions', { params })
    return response.data
  },

  getChatSession: async (sessionId: string): Promise<ChatSession> => {
    const response = await api.get(`/api/chat-history/sessions/${sessionId}`)
    return response.data
  },

  getSessionEvents: async (sessionId: string, limit: number = 100, offset: number = 0): Promise<GetSessionEventsResponse> => {
    const response = await api.get(`/api/chat-history/sessions/${sessionId}/events`, {
      params: { limit, offset }
    })
    return response.data
  },

  createChatSession: async (request: CreateChatSessionRequest): Promise<ChatSession> => {
    const response = await api.post('/api/chat-history/sessions', request)
    return response.data
  },

  updateChatSession: async (sessionId: string, request: UpdateChatSessionRequest): Promise<ChatSession> => {
    const response = await api.put(`/api/chat-history/sessions/${sessionId}`, request)
    return response.data
  },

  deleteChatSession: async (sessionId: string): Promise<void> => {
    await api.delete(`/api/chat-history/sessions/${sessionId}`)
  },

  // Preset Query API
  getPresetQueries: async (limit: number = 50, offset: number = 0): Promise<ListPresetQueriesResponse> => {
    const response = await api.get('/api/chat-history/presets', {
      params: { limit, offset }
    })
    return response.data
  },

  getPresetQuery: async (id: string): Promise<PresetQuery> => {
    const response = await api.get(`/api/chat-history/presets/${id}`)
    return response.data
  },

  createPresetQuery: async (request: CreatePresetQueryRequest): Promise<PresetQuery> => {
    const response = await api.post('/api/chat-history/presets', request)
    return response.data
  },

  updatePresetQuery: async (id: string, request: UpdatePresetQueryRequest): Promise<PresetQuery> => {
    const response = await api.put(`/api/chat-history/presets/${id}`, request)
    return response.data
  },

  deletePresetQuery: async (id: string): Promise<void> => {
    await api.delete(`/api/chat-history/presets/${id}`)
  },

  // Workflow API
  createWorkflow: async (presetQueryId: string, humanVerificationRequired: boolean = true) => {
    const response = await api.post('/api/workflow/create', {
      preset_query_id: presetQueryId,
      human_verification_required: humanVerificationRequired
    })
    return response.data
  },

  // executeWorkflow removed - now using normal agent execution flow

  getWorkflowStatus: async (presetQueryId: string): Promise<WorkflowStatusResponse> => {
    const response = await api.get(`/api/workflow/status?preset_query_id=${encodeURIComponent(presetQueryId)}`)
    return response.data
  },

  updateWorkflow: async (presetQueryId: string, workflowStatus?: string, selectedOptions?: WorkflowSelectedOptions | null) => {
    const body: { preset_query_id: string; workflow_status?: string; selected_options?: WorkflowSelectedOptions | null } = {
      preset_query_id: presetQueryId
    }
    
    if (workflowStatus !== undefined) {
      body.workflow_status = workflowStatus
    }
    
    if (selectedOptions !== undefined) {
      body.selected_options = selectedOptions
    }
    
    const response = await api.post('/api/workflow/update', body)
    return response.data
  },

  getWorkflowConstants: async (): Promise<WorkflowConstantsResponse> => {
    const response = await api.get('/api/workflow/constants')
    return response.data
  },

}

export const healthApi = {
  // Health check
  healthCheck: async () => {
    const response = await api.get('/health')
    return response.data
  },
}

export default api 