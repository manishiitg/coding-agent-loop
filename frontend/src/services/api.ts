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
  RunFoldersResponse,
  CreateRunFolderResponse,
  ProgressResponse,
  VariableGroupsResponse,
  VariablesManifest,
  SlackConfigRequest,
  SlackConfigResponse,
  SlackTestResponse,
  SlackTestReplyResponse,
} from './api-types'

// Re-export types for other components to use
export type {
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
  GetActiveSessionsResponse,
  ReconnectSessionResponse,
  SessionStatusResponse,
  LLMGuidanceResponse,
  HumanFeedbackResponse,
  RunFoldersResponse,
  CreateRunFolderResponse,
  ProgressResponse,
} from './api-types'

const API_BASE_URL = 'http://localhost:8000'
const WORKSPACE_API_BASE_URL = 'http://localhost:8081'

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

const workspaceApi = axios.create({
  baseURL: WORKSPACE_API_BASE_URL,
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

export function setSessionId(sessionId: string): void {
  sessionIdRef = sessionId
}

// --- Observer ID Management ---
// Module-level observer ID (synced from useChatStore via setCurrentObserverId)
let currentObserverIdRef = ''

// Called by ChatArea to sync observer ID from useChatStore
export function setCurrentObserverId(observerId: string): void {
  currentObserverIdRef = observerId
  console.log(`[API] Observer ID set: ${observerId}`)
}

function getObserverId(): string {
  return currentObserverIdRef
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

    // Note: Observer ID is now managed by useChatStore, not localStorage
    // The caller (ChatArea) should call setCurrentObserverId after this
    if (!data.observer_id) {
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
    // Note: Observer ID is managed by useChatStore, caller should clear it there
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
    // Get the current observer ID (managed via setCurrentObserverId)
    const observerId = getObserverId()

    // Create headers with observer ID if available
    const headers: Record<string, string> = {}
    if (observerId) {
      headers['X-Observer-ID'] = observerId
      console.log(`[API] Starting query with observer ID: ${observerId}`)
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

  // Slack Feedback Configuration
  // Get Slack configuration
  getSlackFeedbackConfig: async (): Promise<SlackConfigResponse> => {
    const apiResponse = await api.get('/api/human-feedback/slack/config')
    return apiResponse.data
  },

  // Update Slack configuration
  updateSlackFeedbackConfig: async (config: SlackConfigRequest): Promise<SlackConfigResponse> => {
    const apiResponse = await api.post('/api/human-feedback/slack/config', config)
    return apiResponse.data
  },

  // Test Slack connection (with optional config to test without saving)
  testSlackConnection: async (config?: SlackConfigRequest): Promise<SlackTestResponse> => {
    const apiResponse = await api.post('/api/human-feedback/slack/test', config || {})
    return apiResponse.data
  },

  // Get test connection reply (polling)
  getTestConnectionReply: async (testId: string): Promise<SlackTestReplyResponse | null> => {
    try {
      const apiResponse = await api.get(`/api/human-feedback/slack/test/reply?test_id=${testId}`)
      return apiResponse.data
    } catch (err: unknown) {
      // 204 No Content means no reply yet
      if (err && typeof err === 'object' && 'response' in err) {
        const axiosError = err as { response?: { status?: number } }
        if (axiosError.response?.status === 204) {
          return null
        }
      }
      throw err
    }
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
    const response = await workspaceApi.get('/api/documents', { params })
    return response.data
  },

  getPlannerFileContent: async (filepath: string) => {
    // API handles path conversion internally
    const response = await workspaceApi.get(`/api/documents/${encodeURIComponent(filepath)}`)
    return response.data
  },

  updatePlannerFile: async (filepath: string, content: string, commitMessage?: string) => {
    const requestBody: { content: string; commit_message?: string } = { content }
    if (commitMessage) {
      requestBody.commit_message = commitMessage
    }
    // API handles path conversion internally
    const response = await workspaceApi.put(`/api/documents/${encodeURIComponent(filepath)}`, requestBody)
    return response.data
  },

  deletePlannerFile: async (filepath: string, commitMessage?: string) => {
    const params: Record<string, string> = { confirm: 'true' }
    if (commitMessage) {
      params.commit_message = commitMessage
    }
    // API handles path conversion internally
    const response = await workspaceApi.delete(`/api/documents/${encodeURIComponent(filepath)}`, { params })
    return response.data
  },

  deletePlannerFolder: async (folderPath: string, commitMessage?: string) => {
    const params: Record<string, string> = { confirm: 'true' }
    if (commitMessage) {
      params.commit_message = commitMessage
    }
    const response = await workspaceApi.delete(`/api/folders/${encodeURIComponent(folderPath)}`, { params })
    return response.data
  },

  deleteAllFilesInFolder: async (folderPath: string, commitMessage?: string) => {
    const params: Record<string, string> = { confirm: 'true' }
    if (commitMessage) {
      params.commit_message = commitMessage
    }
    const response = await workspaceApi.delete(`/api/folders/${encodeURIComponent(folderPath)}/files`, { params })
    return response.data
  },

  movePlannerFile: async (filepath: string, destinationPath: string, commitMessage?: string) => {
    const requestBody: { destination_path: string; commit_message?: string } = { destination_path: destinationPath }
    if (commitMessage) {
      requestBody.commit_message = commitMessage
    }
    // API handles path conversion internally
    const response = await workspaceApi.post(`/api/documents/${encodeURIComponent(filepath)}/move`, requestBody)
    return response.data
  },

  uploadPlannerFile: async (file: File, folderPath: string, commitMessage?: string) => {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('folder_path', folderPath)
    if (commitMessage) {
      formData.append('commit_message', commitMessage)
    }

    const response = await workspaceApi.post('/api/upload', formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
    })
    return response.data
  },

  createPlannerFolder: async (folderPath: string, commitMessage?: string) => {
    const response = await workspaceApi.post('/api/folders', {
      folder_path: folderPath,
      commit_message: commitMessage
    })
    return response.data
  },

  // Git Sync API
  getGitSyncStatus: async () => {
    const response = await workspaceApi.get('/api/sync/status')
    return response.data
  },

  syncWithGitHub: async (force: boolean = false, commitMessage?: string) => {
    const response = await workspaceApi.post('/api/sync/github', {
      force,
      commit_message: commitMessage,
      operation: 'sync'
    })
    return response.data
  },

  forcePushLocal: async (commitMessage?: string) => {
    const response = await workspaceApi.post('/api/sync/github', {
      force: true,
      commit_message: commitMessage,
      operation: 'force_push_local'
    })
    return response.data
  },

  forcePullRemote: async () => {
    const response = await workspaceApi.post('/api/sync/github', {
      force: true,
      operation: 'force_pull_remote'
    })
    return response.data
  },

  // Semantic Search Sync API
  getSemanticSearchStatus: async () => {
    const response = await workspaceApi.get('/api/semantic/stats')
    return response.data
  },

  getSemanticJobStatus: async () => {
    const response = await workspaceApi.get('/api/semantic/jobs')
    return response.data
  },

  triggerSemanticResync: async (dryRun: boolean = false, force: boolean = false) => {
    const response = await workspaceApi.post('/api/semantic/resync', {
      dry_run: dryRun,
      force: force
    })
    return response.data
  },

  searchDocuments: async (params: { query: string; search_type?: string; folder?: string; limit?: number }) => {
    const response = await workspaceApi.get('/api/search', { params })
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
    const response = await workspaceApi.get('/api/search/semantic', { params })
    return response.data
  },

  // File Version History API
  getFileVersions: async (filepath: string, limit: number = 10) => {
    const response = await workspaceApi.get(`/api/versions/${encodeURIComponent(filepath)}`, {
      params: { limit }
    })
    return response.data
  },

  restoreFileVersion: async (filepath: string, commitHash: string, commitMessage?: string) => {
    const response = await workspaceApi.post(`/api/restore/${encodeURIComponent(filepath)}`, {
      commit_hash: commitHash,
      commit_message: commitMessage
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

  updateWorkflow: async (presetQueryId: string, workflowStatus?: string, selectedOptions?: WorkflowSelectedOptions | null, stepId?: string) => {
    const body: { preset_query_id: string; workflow_status?: string; selected_options?: WorkflowSelectedOptions | null; step_id?: string } = {
      preset_query_id: presetQueryId
    }

    if (workflowStatus !== undefined) {
      body.workflow_status = workflowStatus
    }

    if (selectedOptions !== undefined) {
      body.selected_options = selectedOptions
    }

    if (stepId !== undefined) {
      body.step_id = stepId
    }

    const response = await api.post('/api/workflow/update', body)
    return response.data
  },

  getWorkflowConstants: async (): Promise<WorkflowConstantsResponse> => {
    const response = await api.get('/api/workflow/constants')
    return response.data
  },

  // Get available run folders for a workspace
  getRunFolders: async (workspacePath: string): Promise<RunFoldersResponse> => {
    const response = await api.get('/api/workflow/run-folders', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  // Create a new run folder (iteration)
  createRunFolder: async (workspacePath: string): Promise<CreateRunFolderResponse> => {
    const response = await api.post('/api/workflow/run-folder', null, {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  // Get execution progress for a run folder
  getProgress: async (workspacePath: string, runFolder: string): Promise<ProgressResponse> => {
    const response = await api.get('/api/workflow/progress', {
      params: { workspace_path: workspacePath, run_folder: runFolder }
    })
    return response.data
  },

  // Delete a run folder (iteration)
  deleteRunFolder: async (workspacePath: string, runFolder: string): Promise<{ success: boolean; message: string }> => {
    const response = await api.delete('/api/workflow/run-folder', {
      params: { workspace_path: workspacePath, run_folder: runFolder }
    })
    return response.data
  },

  // Delete learnings for a specific step
  deleteStepLearnings: async (workspacePath: string, stepNumber: number): Promise<{ success: boolean; message: string }> => {
    const response = await api.delete('/api/workflow/learnings', {
      params: { workspace_path: workspacePath, step_number: stepNumber }
    })
    return response.data
  },

  // Get variable groups from variables.json
  getVariableGroups: async (workspacePath: string): Promise<VariableGroupsResponse> => {
    const response = await api.get('/api/workflow/variable-groups', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  // Update variable groups in variables.json
  updateVariableGroups: async (workspacePath: string, manifest: VariablesManifest): Promise<{ success: boolean; message: string }> => {
    const response = await api.put('/api/workflow/variable-groups', manifest, {
      params: { workspace_path: workspacePath }
    })
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