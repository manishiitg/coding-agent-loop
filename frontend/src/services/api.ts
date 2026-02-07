import axios from 'axios'
import { useChatStore } from '../stores/useChatStore'
import { useModeStore } from '../stores/useModeStore'
import type {
  AgentQueryRequest,
  AgentQueryResponse,
  GetEventsResponse,
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
  SummarizeConversationRequest,
  SummarizeConversationResponse,
  CompactContextRequest,
  CompactContextResponse,
  RunFoldersResponse,
  CreateRunFolderResponse,
  ProgressResponse,
  VariableGroupsResponse,
  VariablesManifest,
  SlackConfigRequest,
  SlackConfigResponse,
  SlackTestResponse,
  SlackTestReplyResponse,
  ExecutionLogsResponse,
  EvaluationReportsResponse,
  TokenUsageFile,
  WorkspaceStateResponse,
  CapabilitiesResponse,
} from './api-types'
import type { PlanStep, AgentConfigs } from '../utils/stepConfigMatching'

// Re-export types for other components to use
export type {
  AgentQueryRequest,
  AgentQueryResponse,
  GetEventsResponse,
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
  SummarizeConversationRequest,
  SummarizeConversationResponse,
  RunFoldersResponse,
  CreateRunFolderResponse,
  ProgressResponse,
  ExecutionLogsResponse,
  StepExecutionLogs,
  ValidationLog,
  ExecutionAttemptLog,
  EvaluationReportsResponse,
  EvaluationReport,
  EvaluationStepScore,
} from './api-types'

// Resolve API base URL: use build-time env if set; in production (non-localhost) use same origin so it works even with cached builds
function getApiBaseUrl(): string {
  const env = import.meta.env.VITE_API_BASE_URL
  if (env) return env
  if (typeof window !== 'undefined' && window.location.hostname !== 'localhost') return ''
  return 'http://localhost:8000'
}

function getWorkspaceApiBaseUrl(): string {
  const env = import.meta.env.VITE_WORKSPACE_API_URL
  if (env) return env
  if (typeof window !== 'undefined' && window.location.hostname !== 'localhost') return `${window.location.origin}/workspace`
  return 'http://localhost:8081'
}

const API_BASE_URL = getApiBaseUrl()
export const WORKSPACE_API_BASE_URL = getWorkspaceApiBaseUrl()

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

export const workspaceApi = axios.create({
  baseURL: WORKSPACE_API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

// --- Session ID Management ---
// Session IDs are now stored per-tab in useChatStore, not globally
// This function gets the session ID from the active tab
export function getSessionId(): string {
  const activeTab = useChatStore.getState().getActiveTab()
  
  if (activeTab?.sessionId) {
    return activeTab.sessionId
  }
  
  // If no active tab or tab has no session ID, generate a new one for the tab
  if (activeTab) {
    const newSessionId = crypto.randomUUID()
    useChatStore.getState().updateTabSessionId(activeTab.tabId, newSessionId)
    console.log(`[API] Generated new session ID for tab ${activeTab.tabId}: ${newSessionId}`)
    return newSessionId
  }
  
  // Fallback: generate a temporary session ID
  // In workflow mode, it's normal to not have an active tab until a phase is started
  // Only warn in chat mode where tabs should always exist
  const selectedModeCategory = useModeStore.getState().selectedModeCategory
  if (selectedModeCategory === 'chat') {
    console.warn('[API] No active tab - generating temporary session ID')
  }
  return crypto.randomUUID()
}

export function resetSessionId(): void {
  // Reset session ID for the active tab by setting it to empty string
  // Note: The tab's sessionId field is string | null, but updateTabSessionId may expect string
  // We'll clear it by setting to empty string or handle it differently
  const activeTab = useChatStore.getState().getActiveTab()
  if (activeTab) {
    // Generate a new session ID instead of null to avoid type issues
    const newSessionId = crypto.randomUUID()
    useChatStore.getState().updateTabSessionId(activeTab.tabId, newSessionId)
    console.log(`[API] Reset session ID for tab ${activeTab.tabId} - generated new: ${newSessionId}`)
  }
}

export function setSessionId(sessionId: string): void {
  // Set session ID for the active tab
  const activeTab = useChatStore.getState().getActiveTab()
  if (activeTab) {
    useChatStore.getState().updateTabSessionId(activeTab.tabId, sessionId)
    console.log(`[API] Set session ID for tab ${activeTab.tabId}: ${sessionId}`)
  } else {
    console.warn('[API] No active tab - cannot set session ID')
  }
}

// Observer ID management removed - no longer needed

// --- Auth token management ---
const AUTH_TOKEN_KEY = 'auth_token'

export function getAuthToken(): string | null {
  return localStorage.getItem(AUTH_TOKEN_KEY)
}

export function setAuthToken(token: string): void {
  localStorage.setItem(AUTH_TOKEN_KEY, token)
}

export function clearAuthToken(): void {
  localStorage.removeItem(AUTH_TOKEN_KEY)
}

// --- Axios request interceptor to inject session ID and auth token ---
// Only adds session ID if not already provided in headers
api.interceptors.request.use((config) => {
  config.headers = config.headers || {}

  // Only add session ID if not already provided
  if (!config.headers['X-Session-ID']) {
    config.headers['X-Session-ID'] = getSessionId()
  }

  // Add auth token if available
  const authToken = getAuthToken()
  if (authToken && !config.headers['Authorization']) {
    config.headers['Authorization'] = `Bearer ${authToken}`
  }

  return config
})

// --- Axios response interceptor to handle 401 errors ---
api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      // Clear auth token on 401
      clearAuthToken()
      // Don't redirect automatically - let the app handle it
      // window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

// Helper to extract user ID from JWT token
function getUserIdFromToken(token: string): string | null {
  try {
    // JWT format: header.payload.signature
    const parts = token.split('.')
    if (parts.length !== 3) return null

    // Decode payload (base64url)
    const payload = JSON.parse(atob(parts[1].replace(/-/g, '+').replace(/_/g, '/')))
    return payload.user_id || payload.sub || null
  } catch {
    return null
  }
}

// --- Workspace API interceptors for auth ---
workspaceApi.interceptors.request.use((config) => {
  config.headers = config.headers || {}

  // Add auth token if available
  const authToken = getAuthToken()
  if (authToken && !config.headers['Authorization']) {
    config.headers['Authorization'] = `Bearer ${authToken}`

    // Extract user ID from JWT and add X-User-ID header for workspace API
    // Workspace API doesn't parse JWT - it needs X-User-ID header for per-user folder isolation
    const userId = getUserIdFromToken(authToken)
    if (userId && !config.headers['X-User-ID']) {
      config.headers['X-User-ID'] = userId
    }
  }

  return config
})

workspaceApi.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      clearAuthToken()
    }
    return Promise.reject(error)
  }
)

export const agentApi = {
  // Observer APIs removed - no longer needed

  // Get events for a session
  // Supports both forward polling (sinceIndex) and backward pagination (limit/offset)
  // eventMode: 'basic' | 'advanced' | 'tiny' - filters events by mode (defaults to 'basic')
  getSessionEvents: async (
    sessionId: string, 
    sinceIndex?: number,
    options?: {
      limit?: number
      offset?: number
      eventMode?: 'basic' | 'advanced' | 'tiny' | 'micro'
    }
  ): Promise<GetEventsResponse> => {
    const params: Record<string, string | number> = {}
    
    // Forward polling mode: use sinceIndex
    if (sinceIndex !== undefined && sinceIndex >= 0) {
      params.since = sinceIndex
      // Add event mode if specified (for polling mode)
      if (options?.eventMode) {
        params.event_mode = options.eventMode
      }
    }
    // Backward pagination mode: use limit/offset
    else if (options?.limit !== undefined || options?.offset !== undefined) {
      if (options.limit !== undefined) {
        params.limit = options.limit
      }
      if (options.offset !== undefined) {
        params.offset = options.offset
      }
      // Add event mode if specified (for pagination mode)
      if (options?.eventMode) {
        params.event_mode = options.eventMode
      }
    } else {
      throw new Error('Either sinceIndex (for polling) or limit (for pagination) must be provided')
    }
    
    const response = await api.get(`/api/sessions/${sessionId}/events`, { params })
    return response.data
  },

  // Observer APIs removed - no longer needed

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
  startQuery: async (request: AgentQueryRequest, sessionId?: string): Promise<AgentQueryResponse> => {
    // Create headers with session ID if provided
    const headers: Record<string, string> = {}
    if (sessionId) {
      headers['X-Session-ID'] = sessionId
      console.log(`[API] Starting query with session ID: ${sessionId}`)
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
  getCapabilities: async (): Promise<CapabilitiesResponse> => {
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

  // Context Summarization Management
  // Summarize conversation history for a session
  summarizeConversation: async (sessionId: string, request?: SummarizeConversationRequest): Promise<SummarizeConversationResponse> => {
    const response = await api.post(`/api/sessions/${sessionId}/summarize`, request || {}, {
      headers: {
        'X-Session-ID': sessionId
      }
    })
    return response.data
  },

  // Compact context (edit stale tool responses) for a session
  compactContext: async (sessionId: string, request?: CompactContextRequest): Promise<CompactContextResponse> => {
    const response = await api.post(`/api/sessions/${sessionId}/compact`, request || {}, {
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

  copyFolder: async (sourcePath: string, destinationPath: string, commitMessage?: string) => {
    const response = await workspaceApi.post('/api/folders/copy', {
      source_path: sourcePath,
      destination_path: destinationPath,
      commit_message: commitMessage
    })
    return response.data
  },

  // Workspace Backup API
  exportWorkflowBackup: async (workspacePath: string): Promise<Blob> => {
    const response = await workspaceApi.post('/api/workspace/export', {
      workspace_path: workspacePath
    }, {
      responseType: 'blob'
    })
    return response.data
  },

  importWorkflowBackup: async (workspacePath: string, file: File, overwrite: boolean = false): Promise<{ success: boolean; message: string; data?: { workspace_path: string; files_extracted: number; extracted_files: string[] } }> => {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('workspace_path', workspacePath)
    formData.append('overwrite', overwrite.toString())

    const response = await workspaceApi.post('/api/workspace/import', formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
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
  getChatSessions: async (limit: number = 20, offset: number = 0, presetQueryId?: string, agentMode?: string): Promise<ListChatSessionsResponse> => {
    const params: Record<string, string | number> = { limit, offset }
    if (presetQueryId) {
      params.preset_query_id = presetQueryId
    }
    if (agentMode) {
      params.agent_mode = agentMode
    }
    const response = await api.get('/api/chat-history/sessions', { params })
    return response.data
  },

  getChatSession: async (sessionId: string): Promise<ChatSession> => {
    const response = await api.get(`/api/chat-history/sessions/${sessionId}`)
    return response.data
  },

  // Get events from database for a chat session (for completed sessions)
  getChatSessionEvents: async (sessionId: string, limit: number = 1000, offset: number = 0): Promise<GetSessionEventsResponse> => {
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
  deleteStepLearnings: async (workspacePath: string, stepId: string): Promise<{ success: boolean; message: string }> => {
    const response = await api.delete('/api/workflow/learnings', {
      params: { workspace_path: workspacePath, step_id: stepId }
    })
    return response.data
  },

  // Get learning metadata for all steps
  getAllStepLearnings: async (workspacePath: string): Promise<{ success: boolean; learnings: Record<string, Record<string, unknown> | null> }> => {
    const response = await api.get('/api/workflow/learnings/all', {
      params: { workspace_path: workspacePath }
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

  // Get execution logs for a workflow run (steps, validations, etc.)
  getExecutionLogs: async (workspacePath: string, runFolder: string): Promise<ExecutionLogsResponse> => {
    const response = await api.get('/api/workflow/logs', {
      params: { workspace_path: workspacePath, run_folder: runFolder }
    })
    return response.data
  },

  // Get cost data (token usage) for a workflow run
  getCosts: async (workspacePath: string, runFolder: string): Promise<{ success: boolean; token_usage?: TokenUsageFile; evaluation_token_usage?: TokenUsageFile }> => {
    const response = await api.get('/api/workflow/costs', {
      params: { workspace_path: workspacePath, run_folder: runFolder }
    })
    return response.data
  },

  // Get content of a specific log file
  // Returns string content (may be JSON that needs parsing)
  getLogFile: async (filePath: string): Promise<string | Record<string, unknown>> => {
    const response = await api.get('/api/workflow/logs/file', {
      params: { file_path: filePath }
    })
    return response.data
  },

  // Get evaluation reports for a workflow
  // If runFolder is empty, returns aggregate across all evaluation runs
  // If runFolder is specified, returns report for that specific run
  getEvaluationReports: async (workspacePath: string, runFolder?: string): Promise<EvaluationReportsResponse> => {
    const response = await api.get('/api/workflow/evaluation-reports', {
      params: { workspace_path: workspacePath, run_folder: runFolder || '' }
    })
    return response.data
  },

  // *** NEW CONSOLIDATED API ***
  // Load all workspace state in a single API call (run folders, variables, phases, progress)
  // This replaces multiple individual API calls (getRunFolders, getVariableGroups, constants, progress)
  // Reduces network overhead, eliminates race conditions, and ensures consistent state
  loadWorkspaceState: async (workspacePath: string, selectedFolder?: string | null): Promise<WorkspaceStateResponse> => {
    const params: Record<string, string> = { workspace_path: workspacePath }
    if (selectedFolder && selectedFolder !== 'new') {
      params.selected_folder = selectedFolder
    }
    const response = await api.get('/api/workspace/state', { params })
    return response.data
  },


  // Plan and Step Config API
  // Update a plan step (plan.json fields only, no agent_configs)
  updatePlanStep: async (
    workspacePath: string,
    stepId: string,
    updates: Partial<Omit<PlanStep, 'agent_configs'>>
  ): Promise<{ success: boolean; message: string; data?: { step: PlanStep } }> => {
    const response = await api.post('/api/workflow/plan/update-step', {
      workspace_path: workspacePath,
      step_id: stepId,
      updates: updates
    })
    return response.data
  },

  // Update step config (agent_configs in step_config.json)
  updateStepConfig: async (
    workspacePath: string,
    stepId: string,
    agentConfigs: AgentConfigs | undefined
  ): Promise<{ success: boolean; message: string; data?: { step_id: string; agent_configs?: AgentConfigs } }> => {
    const response = await api.post('/api/workflow/plan/update-step-config', {
      workspace_path: workspacePath,
      step_id: stepId,
      agent_configs: agentConfigs
    })
    return response.data
  },

  // Batch update multiple steps
  batchUpdateSteps: async (
    workspacePath: string,
    updates: Array<{
      stepId: string
      planUpdates?: Partial<Omit<PlanStep, 'agent_configs'>>
      configUpdates?: Partial<AgentConfigs>
    }>
  ): Promise<{
    success: boolean
    message: string
    data?: {
      updated_steps: number
      updated_configs: number
      errors?: Array<{ step_id: string; error: string }>
    }
  }> => {
    const response = await api.post('/api/workflow/plan/batch-update-steps', {
      workspace_path: workspacePath,
      updates: updates.map(u => ({
        step_id: u.stepId,
        plan_updates: u.planUpdates,
        config_updates: u.configUpdates
      }))
    })
    return response.data
  },

  // Delete a step from plan and config
  deleteStep: async (
    workspacePath: string,
    stepId: string
  ): Promise<{ success: boolean; message: string; data?: { deleted_step_id: string; deleted_config: boolean } }> => {
    const response = await api.post('/api/workflow/plan/delete-step', {
      workspace_path: workspacePath,
      step_id: stepId
    })
    return response.data
  },

  // Add a new step to the plan
  addStep: async (
    workspacePath: string,
    step: Omit<PlanStep, 'agent_configs'>,
    options?: {
      insertAfterStepId?: string
      parentStepId?: string
      branchType?: 'if_true' | 'if_false'
    }
  ): Promise<{ success: boolean; message: string; data?: { step: PlanStep } }> => {
    const response = await api.post('/api/workflow/plan/add-step', {
      workspace_path: workspacePath,
      step: step,
      insert_after_step_id: options?.insertAfterStepId,
      parent_step_id: options?.parentStepId,
      branch_type: options?.branchType
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

// --- Auth API ---
export interface AuthUser {
  id: string
  username: string
  email?: string
  provider?: string
}

export interface AuthResponse {
  token: string
  user: AuthUser
}

export interface AuthProvider {
  name: string
  type: 'credentials' | 'oauth'
  auth_url?: string
}

export interface AuthModeResponse {
  multi_user_mode: boolean
  registration_enabled?: boolean
  providers: AuthProvider[]
}

export interface OAuthStartResponse {
  auth_url: string
  state: string
}

export const authApi = {
  // Get authentication mode and available providers
  getAuthMode: async (): Promise<AuthModeResponse> => {
    const response = await api.get('/api/auth/mode')
    return response.data
  },

  // Register a new user (only available in multi-user mode)
  register: async (username: string, password: string, email?: string): Promise<AuthResponse> => {
    const response = await api.post('/api/auth/register', { username, password, email })
    return response.data
  },

  // Login with credentials (for "simple" and "supabase" providers)
  login: async (username: string, password: string, provider?: string): Promise<AuthResponse> => {
    const response = await api.post('/api/auth/login', { username, password, provider })
    return response.data
  },

  // Start OAuth flow for a provider (for "cognito" and "supabase" OAuth)
  startOAuth: async (provider: string, redirectUri: string): Promise<OAuthStartResponse> => {
    const response = await api.post('/api/auth/start', { provider, redirect_uri: redirectUri })
    return response.data
  },

  // Handle OAuth callback - exchange code for app JWT
  handleOAuthCallback: async (code: string, state: string): Promise<AuthResponse> => {
    const response = await api.get('/api/auth/callback', {
      params: { code, state }
    })
    return response.data
  },

  // Logout
  logout: async (): Promise<void> => {
    await api.post('/api/auth/logout')
  },

  // Get current user info
  getCurrentUser: async (): Promise<AuthUser> => {
    const response = await api.get('/api/auth/me')
    return response.data
  },
}

// --- Session Sharing API ---
export interface ShareResponse {
  share_id: string
  share_url: string
  token: string
  expires_at?: string
}

export interface SessionShare {
  id: string
  session_id: string
  share_token: string
  created_by: string
  created_at: string
  expires_at?: string
  access_level: string
}

export interface SharedSessionResponse {
  session_id: string
  title: string
  agent_mode: string
  status: string
  created_at: string
  completed_at?: string
  events?: unknown[]
  is_shared: boolean
}

export const sessionShareApi = {
  // Create a share link for a session
  createShare: async (sessionId: string, expiresInHours?: number): Promise<ShareResponse> => {
    const response = await api.post(`/api/sessions/${sessionId}/share`, { expires_in_hours: expiresInHours })
    return response.data
  },

  // List all shares for a session
  listShares: async (sessionId: string): Promise<{ shares: SessionShare[] }> => {
    const response = await api.get(`/api/sessions/${sessionId}/shares`)
    return response.data
  },

  // Revoke a share link
  revokeShare: async (sessionId: string, shareId: string): Promise<void> => {
    await api.delete(`/api/sessions/${sessionId}/share/${shareId}`)
  },

  // Get a shared session (no auth required)
  getSharedSession: async (shareToken: string): Promise<SharedSessionResponse> => {
    const response = await api.get(`/api/shared/${shareToken}`)
    return response.data
  },
}

export default api