console.log('Cache bust: 2026-02-08-150000');
import axios from 'axios'
import type { AxiosInstance, InternalAxiosRequestConfig } from 'axios'
import { useChatStore } from '../stores/useChatStore'
import { useModeStore } from '../stores/useModeStore'
import { getActiveWorkspaceProfile, useWorkspaceConnectionStore } from '../stores/useWorkspaceConnectionStore'
import type {
  AgentQueryRequest,
  AgentQueryResponse,
  GetEventsResponse,
  MCPServerConfig,
  ChatSession,
  ChatHistoryConversation,
  ChatHistorySession,
  ChatHistoryCleanupResult,
  ListChatSessionsResponse,
  GetSessionEventsResponse,
  CreateChatSessionRequest,
  UpdateChatSessionRequest,
  WorkflowStatusResponse,
  WorkflowConstantsResponse,
  WorkflowSelectedOptions,
  GetActiveSessionsResponse,
  ReconnectSessionResponse,
  SessionStatusResponse,
  SessionExecutionTreeResponse,
  LLMGuidanceResponse,
  LLMGuidanceRequest,
  HumanFeedbackResponse,
  SummarizeConversationRequest,
  SummarizeConversationResponse,
  CompactContextRequest,
  CompactContextResponse,
  RunFoldersResponse,
  RunFolderInfo,
  RunMetadataModels,
  CreateRunFolderResponse,
  VariableGroupsResponse,
  VariablesManifest,
  SlackConfigRequest,
  SlackConfigResponse,
  SlackTestResponse,
  SlackTestReplyResponse,
  GmailConfigRequest,
  GmailConfigResponse,
  GmailAuthStatus,
  GmailTestResponse,
  ExecutionLogsResponse,
  EvaluationReportsResponse,
  WorkflowReviewDataResponse,
  TokenUsageFile,
  WorkflowCostsResponse,
  WorkspaceStateResponse,
  CapabilitiesResponse,
  UserCostsResponse,
  SessionCostDetail,
  DelegationLogsResponse,
  AllDelegationLogsResponse,
  PollingEvent,
  SimulatorMessage,
  SimulatorSendResponse,
  SimulatorThreadInfo,
  ListWorkflowManifestsResponse,
  GetWorkflowManifestResponse,
  CreateWorkflowManifestRequest,
  UpdateWorkflowManifestRequest,
  DuplicateWorkflowManifestRequest,
  MigrateWorkflowsResponse,
  RunningWorkflowInfo,
  UpdateRunningWorkflowRequest,
  CostSummary,
  NotificationPreference,
  WorkflowBuilderSessionResponse,
  ListTerminalsResponse,
  TerminalSnapshot,
  StartRestoredTerminalRequest,
  StartRestoredTerminalResponse,
  WorkflowCapabilities,
  WorkflowBackupInfoResponse,
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
  WorkflowStatusResponse,
  WorkflowConstantsResponse,
  GetActiveSessionsResponse,
  ReconnectSessionResponse,
  SessionStatusResponse,
  SessionExecutionTreeResponse,
  LLMGuidanceResponse,
  HumanFeedbackResponse,
  SummarizeConversationRequest,
  SummarizeConversationResponse,
  RunFoldersResponse,
  CreateRunFolderResponse,
  ExecutionLogsResponse,
  StepExecutionLogs,
  ValidationLog,
  ExecutionAttemptLog,
  EvaluationReportsResponse,
  EvaluationReport,
  EvaluationStepScore,
} from './api-types'

type RuntimeConfig = {
  apiBaseUrl?: string
  workspaceApiBaseUrl?: string
  desktopAppOnly?: boolean | string
}

export interface CdpCheckResult {
  connected: boolean
  error?: string
  browser?: string
  endpoint?: string
  source?: 'workspace' | 'agent'
}

export interface WorkflowOverviewRunFolderDetail {
  folder: RunFolderInfo
  total_steps: number
  completed_steps: number
  last_updated?: string
  metrics_summary?: WorkflowMetricRunSummary
  cost_usd?: number
  started_at?: string
  completed_at?: string
  triggered_by?: string
  status: string
  models?: RunMetadataModels | null
}

export interface MetricSnapshotRow {
  run_folder: string
  completed_at: string
  metric_id: string
  metric_version?: number
  value: number
  has_value: boolean
  resolve_error?: string
  threshold_kind?: string
  threshold_value?: number
  passed?: boolean
}

export interface WorkflowMetricRunSummary {
  total: number
  with_value: number
  passed: number
  failed: number
  unknown: number
  rows?: MetricSnapshotRow[]
}

export interface WorkflowOverviewBatchResponse {
  success: boolean
  workflows: Array<{
    workspace_path: string
    run_folders: WorkflowOverviewRunFolderDetail[]
    eval_data: EvaluationReportsResponse
    last_updated?: string
    total_run_count: number
    active_run_paths?: string[]
    error?: string
  }>
}

type AppWindow = Window & {
  __APP_RUNTIME_CONFIG__?: RuntimeConfig
  __logged_apiBaseUrl?: boolean
  __logged_workspaceApiBaseUrl?: boolean
  electronAPI?: {
    getApiBaseUrl?: () => string
    getWorkspaceApiBaseUrl?: () => string
  }
}

type RuntimeRetriableRequestConfig = InternalAxiosRequestConfig & {
  __runtimeConfigRetried?: boolean
}

// Resolve API base URL: use build-time env if set; otherwise fallback based on mode
function getRuntimeConfig(): RuntimeConfig {
  if (typeof window === 'undefined') return {}
  return (window as AppWindow).__APP_RUNTIME_CONFIG__ || {}
}

let runtimeConfigRefreshPromise: Promise<boolean> | null = null

async function refreshRuntimeConfigFromScript(): Promise<boolean> {
  if (typeof window === 'undefined') return false
  if (runtimeConfigRefreshPromise) return runtimeConfigRefreshPromise

  runtimeConfigRefreshPromise = (async () => {
    try {
      const response = await fetch(`/runtime-config.js?t=${Date.now()}`, { cache: 'no-store' })
      if (!response.ok) return false

      const text = await response.text()
      const apiBaseUrl = text.match(/apiBaseUrl:\s*["']([^"']+)["']/)?.[1]
      const workspaceApiBaseUrl = text.match(/workspaceApiBaseUrl:\s*["']([^"']+)["']/)?.[1]
      const desktopAppOnlyMatch = text.match(/desktopAppOnly:\s*(true|false|["'][^"']+["'])/)
      const desktopAppOnlyRaw = desktopAppOnlyMatch?.[1]
      const desktopAppOnly = desktopAppOnlyRaw
        ? desktopAppOnlyRaw.replace(/^["']|["']$/g, '')
        : undefined
      if (!apiBaseUrl && !workspaceApiBaseUrl && desktopAppOnly === undefined) return false

      const previous = getRuntimeConfig()
      const next: RuntimeConfig = {
        ...previous,
        ...(apiBaseUrl ? { apiBaseUrl } : {}),
        ...(workspaceApiBaseUrl ? { workspaceApiBaseUrl } : {}),
        ...(desktopAppOnly !== undefined ? { desktopAppOnly: desktopAppOnly === 'true' } : {}),
      }
      const changed =
        next.apiBaseUrl !== previous.apiBaseUrl ||
        next.workspaceApiBaseUrl !== previous.workspaceApiBaseUrl ||
        next.desktopAppOnly !== previous.desktopAppOnly

      ;(window as AppWindow).__APP_RUNTIME_CONFIG__ = next
      if (changed) {
        ;(window as AppWindow).__logged_apiBaseUrl = false
        ;(window as AppWindow).__logged_workspaceApiBaseUrl = false
        console.info('[api-config] runtime-config refreshed', { previous, next })
      }
      return changed
    } catch {
      return false
    } finally {
      runtimeConfigRefreshPromise = null
    }
  })()

  return runtimeConfigRefreshPromise
}

function isTruthyRuntimeFlag(value: boolean | string | undefined): boolean {
  if (typeof value === 'boolean') return value
  if (typeof value === 'string') return value.toLowerCase() === 'true'
  return false
}

export function isDesktopAppOnlyMode(): boolean {
  const runtime = getRuntimeConfig()
  if (runtime.desktopAppOnly !== undefined) {
    return isTruthyRuntimeFlag(runtime.desktopAppOnly)
  }
  return isTruthyRuntimeFlag(import.meta.env.VITE_DESKTOP_APP_ONLY_UI)
}

function logResolvedUrlOnce(key: string, payload: Record<string, unknown>) {
  if (typeof window === 'undefined') return
  const appWindow = window as AppWindow
  if (key === 'workspaceApiBaseUrl') {
    if (appWindow.__logged_workspaceApiBaseUrl) return
    appWindow.__logged_workspaceApiBaseUrl = true
  } else {
    if (appWindow.__logged_apiBaseUrl) return
    appWindow.__logged_apiBaseUrl = true
  }
  console.info(`[api-config] ${key}`, payload)
}

export function getApiBaseUrl(): string {
  const activeWorkspace = getActiveWorkspaceProfile()
  if (activeWorkspace.type === 'remote' && activeWorkspace.apiBaseUrl) {
    logResolvedUrlOnce('apiBaseUrl', {
      source: 'workspace-profile',
      workspaceId: activeWorkspace.id,
      resolved: activeWorkspace.apiBaseUrl,
    })
    return activeWorkspace.apiBaseUrl
  }

  const runtime = getRuntimeConfig()
  if (runtime.apiBaseUrl) {
    logResolvedUrlOnce('apiBaseUrl', { source: 'runtime-config', resolved: runtime.apiBaseUrl, runtime })
    return runtime.apiBaseUrl
  }

  // Use Electron API if available
  if (typeof window !== 'undefined' && (window as AppWindow).electronAPI?.getApiBaseUrl) {
    const resolved = (window as AppWindow).electronAPI!.getApiBaseUrl!()
    logResolvedUrlOnce('apiBaseUrl', { source: 'electron', resolved, runtime })
    return resolved
  }

  const env = import.meta.env.VITE_API_BASE_URL
  if (env) {
    logResolvedUrlOnce('apiBaseUrl', { source: 'vite-env', resolved: env, runtime })
    return env
  }
  // Only fallback to localhost:8000 in DEV mode
  if (import.meta.env.DEV) {
    const resolved = 'http://localhost:8000'
    logResolvedUrlOnce('apiBaseUrl', { source: 'dev-fallback', resolved, runtime })
    return resolved
  }
  // In production (including preview/docker), use relative path (same origin)
  logResolvedUrlOnce('apiBaseUrl', { source: 'relative-origin', resolved: '', runtime })
  return ''
}

function getWorkspaceApiBaseUrl(): string {
  const activeWorkspace = getActiveWorkspaceProfile()
  if (activeWorkspace.type === 'remote' && activeWorkspace.workspaceApiBaseUrl) {
    logResolvedUrlOnce('workspaceApiBaseUrl', {
      source: 'workspace-profile',
      workspaceId: activeWorkspace.id,
      resolved: activeWorkspace.workspaceApiBaseUrl,
    })
    return activeWorkspace.workspaceApiBaseUrl
  }

  const runtime = getRuntimeConfig()
  if (runtime.workspaceApiBaseUrl) {
    logResolvedUrlOnce('workspaceApiBaseUrl', { source: 'runtime-config', resolved: runtime.workspaceApiBaseUrl, runtime })
    return runtime.workspaceApiBaseUrl
  }

  // Use Electron API if available
  if (typeof window !== 'undefined' && (window as AppWindow).electronAPI?.getWorkspaceApiBaseUrl) {
    const resolved = (window as AppWindow).electronAPI!.getWorkspaceApiBaseUrl!()
    logResolvedUrlOnce('workspaceApiBaseUrl', { source: 'electron', resolved, runtime })
    return resolved
  }

  const env = import.meta.env.VITE_WORKSPACE_API_URL
  if (env) {
    logResolvedUrlOnce('workspaceApiBaseUrl', { source: 'vite-env', resolved: env, runtime })
    return env
  }
  if (typeof window !== 'undefined' && window.location.hostname !== 'localhost') {
    const resolved = `${window.location.origin}/api/wp`
    logResolvedUrlOnce('workspaceApiBaseUrl', { source: 'origin-proxy', resolved, runtime })
    return resolved
  }
  const resolved = 'http://127.0.0.1:8081'
  logResolvedUrlOnce('workspaceApiBaseUrl', { source: 'dev-fallback', resolved, runtime })
  return resolved
}

const API_BASE_URL = getApiBaseUrl()
export { API_BASE_URL }
export const WORKSPACE_API_BASE_URL = getWorkspaceApiBaseUrl()

const api = axios.create({
  baseURL: API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

const DEDUPED_GET_REUSE_MS = 1000
const dedupedGetRequests = new Map<string, { promise: Promise<unknown>; expiresAt: number }>()

function dedupedGet<T>(key: string, request: () => Promise<T>): Promise<T> {
  const now = Date.now()
  const existing = dedupedGetRequests.get(key)
  if (existing && (existing.expiresAt === 0 || existing.expiresAt > now)) {
    return existing.promise as Promise<T>
  }

  let succeeded = false
  const promise = request()
    .then(result => {
      succeeded = true
      return result
    })
    .finally(() => {
      const current = dedupedGetRequests.get(key)
      if (!current || current.promise !== promise) return

      if (!succeeded) {
        dedupedGetRequests.delete(key)
        return
      }

      current.expiresAt = Date.now() + DEDUPED_GET_REUSE_MS
      window.setTimeout(() => {
        const latest = dedupedGetRequests.get(key)
        if (latest?.promise === promise && latest.expiresAt <= Date.now()) {
          dedupedGetRequests.delete(key)
        }
      }, DEDUPED_GET_REUSE_MS + 50)
    })

  dedupedGetRequests.set(key, { promise, expiresAt: 0 })
  return promise
}

export const workspaceApi = axios.create({
  baseURL: WORKSPACE_API_BASE_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

function shouldRefreshRuntimeConfig(error: unknown): boolean {
  if (!axios.isAxiosError(error)) return false
  if (error.response) return false
  return error.code === 'ERR_NETWORK' || error.code === 'ECONNABORTED' || error.request
}

async function retryWithFreshRuntimeConfig(
  instance: AxiosInstance,
  error: unknown,
  resolveBaseUrl: () => string
) {
  if (!shouldRefreshRuntimeConfig(error) || !axios.isAxiosError(error) || !error.config) {
    return Promise.reject(error)
  }

  const config = error.config as RuntimeRetriableRequestConfig
  if (config.__runtimeConfigRetried) {
    return Promise.reject(error)
  }

  const oldBaseUrl = String(config.baseURL || '')
  const changed = await refreshRuntimeConfigFromScript()
  const nextBaseUrl = resolveBaseUrl()
  if (!changed || !nextBaseUrl || nextBaseUrl === oldBaseUrl) {
    return Promise.reject(error)
  }

  config.__runtimeConfigRetried = true
  config.baseURL = nextBaseUrl
  return instance(config)
}

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
  // Only warn in multi-agent mode where tabs should always exist
  const selectedModeCategory = useModeStore.getState().selectedModeCategory
  if (selectedModeCategory === 'multi-agent') {
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
  const activeWorkspace = getActiveWorkspaceProfile()
  if (activeWorkspace.token) return activeWorkspace.token

  // One-time compatibility fallback for pre-workspace local installs.
  if (activeWorkspace.id === 'local') {
    return localStorage.getItem(AUTH_TOKEN_KEY)
  }
  return null
}

export function setAuthToken(token: string): void {
  useWorkspaceConnectionStore.getState().setActiveWorkspaceToken(token)
}

export function clearAuthToken(): void {
  useWorkspaceConnectionStore.getState().setActiveWorkspaceToken(undefined)
  if (getActiveWorkspaceProfile().id === 'local') {
    localStorage.removeItem(AUTH_TOKEN_KEY)
  }
}

// --- Axios request interceptor to inject session ID and auth token ---
// Only adds session ID if not already provided in headers
api.interceptors.request.use((config) => {
  config.baseURL = getApiBaseUrl()
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
// Only clear the auth token when the *token itself* is rejected as expired/invalid.
// Clearing on every 401 is a footgun: a single transient 401 from any endpoint (e.g.
// a race where a request fires before the token is attached) wipes localStorage, and
// every subsequent request goes out with no Authorization header → infinite 401 loop
// until the user hard-refreshes and re-logs in.
function is401DueToBadToken(error: unknown): boolean {
  if (!error || typeof error !== 'object') return false
  const e = error as { response?: { status?: number; data?: { error?: string } } }
  if (e.response?.status !== 401) return false
  const msg = (e.response.data?.error || '').toLowerCase()
  return msg.includes('expired') || msg.includes('invalid')
}

api.interceptors.response.use(
  (response) => response,
  async (error) => {
    if (is401DueToBadToken(error)) {
      clearAuthToken()
    }
    try {
      return await retryWithFreshRuntimeConfig(api, error, getApiBaseUrl)
    } catch {
      // Fall through to the original rejection so callers keep the real error.
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
  config.baseURL = getWorkspaceApiBaseUrl()
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
  async (error) => {
    if (is401DueToBadToken(error)) {
      clearAuthToken()
    }
    try {
      return await retryWithFreshRuntimeConfig(workspaceApi, error, getWorkspaceApiBaseUrl)
    } catch {
      // Fall through to the original rejection so callers keep the real error.
    }
    return Promise.reject(error)
  }
)


export const agentApi = {
  // Observer APIs removed - no longer needed

  // Get events for a session
  // Supports both forward polling (sinceIndex) and backward pagination (limit/offset)
  getSessionEvents: async (
    sessionId: string,
    sinceIndex?: number,
    options?: {
      limit?: number
      offset?: number
    }
  ): Promise<GetEventsResponse> => {
    const params: Record<string, string | number> = {}

    // Forward polling mode: use sinceIndex
    if (sinceIndex !== undefined && sinceIndex >= -1) {
      params.since = sinceIndex
    }
    // Backward pagination mode: use limit/offset
    else if (options?.limit !== undefined || options?.offset !== undefined) {
      if (options.limit !== undefined) {
        params.limit = options.limit
      }
      if (options.offset !== undefined) {
        params.offset = options.offset
      }
    } else {
      throw new Error('Either sinceIndex (for polling) or limit (for pagination) must be provided')
    }

    const response = await api.get(`/api/sessions/${sessionId}/events`, { params })
    return response.data
  },

  // Initial restores should not use since=-1. That requests the entire
  // in-memory event buffer before the frontend trims it, which can spike both
  // backend and Electron memory on large workflow runs. since=0 uses the
  // backend's bounded initial page and still returns last_processed_index.
  getRecentSessionEvents: async (sessionId: string): Promise<GetEventsResponse> => {
    return agentApi.getSessionEvents(sessionId, 0)
  },

  listTerminals: async (sessionId?: string, content: 'none' | 'tail' | 'full' = 'tail'): Promise<ListTerminalsResponse> => {
    const params = sessionId ? { session_id: sessionId, content } : { content }
    const response = await api.get('/api/terminals', { params, timeout: 5000 })
    return response.data
  },

  getTerminal: async (
    terminalId: string,
    options?: { content?: 'stored' | 'screen' | 'history' | 'tmux' | 'deep'; lines?: number; debug?: boolean; debugSource?: string },
  ): Promise<TerminalSnapshot> => {
    const params: Record<string, string | number> = {}
    if (options?.content && options.content !== 'stored') params.content = options.content
    if (options?.lines) params.lines = options.lines
    if (options?.debug) params.debug = 1
    if (options?.debugSource) params.debug_source = options.debugSource
    const response = await api.get(`/api/terminals/${encodeURIComponent(terminalId)}`, { params })
    if (options?.debug) {
      const headers = response.headers || {}
      const debugHeaders = {
        terminal_id: terminalId,
        source: options.debugSource,
        request_content: options.content || 'stored',
        request_lines: options.lines,
        debug_should_capture: headers['x-runloop-terminal-debug-should-capture'],
        debug_skip_reason: headers['x-runloop-terminal-debug-skip-reason'],
        debug_tmux_session: headers['x-runloop-terminal-debug-tmux-session'],
        debug_step_transport: headers['x-runloop-terminal-debug-step-transport'],
        debug_active: headers['x-runloop-terminal-debug-active'],
        debug_state: headers['x-runloop-terminal-debug-state'],
        debug_chunk_index: headers['x-runloop-terminal-debug-chunk-index'],
        debug_content_mode: headers['x-runloop-terminal-debug-content-mode'],
        debug_lines_param: headers['x-runloop-terminal-debug-lines-param'],
        debug_stored_lines: headers['x-runloop-terminal-debug-stored-lines'],
        debug_stored_bytes: headers['x-runloop-terminal-debug-stored-bytes'],
        content_source: headers['x-runloop-terminal-content-source'],
        requested_lines: headers['x-runloop-terminal-requested-lines'],
        capture_lines: headers['x-runloop-terminal-capture-lines'],
        raw_lines: headers['x-runloop-terminal-raw-lines'],
        raw_bytes: headers['x-runloop-terminal-raw-bytes'],
        collapsed_lines: headers['x-runloop-terminal-collapsed-lines'],
        collapsed_bytes: headers['x-runloop-terminal-collapsed-bytes'],
        pipe_lines: headers['x-runloop-terminal-pipe-lines'],
        pipe_bytes: headers['x-runloop-terminal-pipe-bytes'],
        preserve_scrollback: headers['x-runloop-terminal-preserve-scrollback'],
        tmux_history_limit: headers['x-runloop-terminal-tmux-history-limit'],
        tmux_history_size: headers['x-runloop-terminal-tmux-history-size'],
        tmux_alternate_on: headers['x-runloop-terminal-tmux-alternate-on'],
        tmux_pane_height: headers['x-runloop-terminal-tmux-pane-height'],
        tmux_pane_width: headers['x-runloop-terminal-tmux-pane-width'],
        tmux_pane_in_mode: headers['x-runloop-terminal-tmux-pane-in-mode'],
        tmux_scroll_position: headers['x-runloop-terminal-tmux-scroll-position'],
        response_tmux_session: response.data?.tmux_session,
        response_step_transport: response.data?.step_transport,
        response_active: response.data?.active,
        response_state: response.data?.state,
        response_content_source: response.data?.content_source,
        response_chunk_index: response.data?.chunk_index,
        response_content_lines: typeof response.data?.content === 'string' ? response.data.content.split(/\n/).length : undefined,
        response_content_bytes: typeof response.data?.content === 'string' ? response.data.content.length : undefined,
        response_rows: Array.isArray(response.data?.rows) ? response.data.rows.length : undefined,
      }
      if (Object.values(debugHeaders).some(value => value !== undefined && value !== '')) {
        console.info('[TERMINAL_DEBUG] response headers', debugHeaders)
      }
    }
    return response.data
  },

  dismissTerminal: async (terminalId: string): Promise<void> => {
    await api.delete(`/api/terminals/${encodeURIComponent(terminalId)}`)
  },

  completeTerminal: async (terminalId: string): Promise<TerminalSnapshot> => {
    const response = await api.post(`/api/terminals/${encodeURIComponent(terminalId)}/complete`)
    return response.data.terminal
  },

  failTerminal: async (terminalId: string): Promise<TerminalSnapshot> => {
    const response = await api.post(`/api/terminals/${encodeURIComponent(terminalId)}/fail`)
    return response.data.terminal
  },

  refreshTerminal: async (terminalId: string, options?: { lines?: number }): Promise<TerminalSnapshot> => {
    const params: Record<string, number> = {}
    if (options?.lines) params.lines = options.lines
    const response = await api.post(`/api/terminals/${encodeURIComponent(terminalId)}/refresh`, undefined, { params })
    return response.data.terminal
  },

  killTerminal: async (terminalId: string): Promise<TerminalSnapshot> => {
    const response = await api.post(`/api/terminals/${encodeURIComponent(terminalId)}/kill`)
    return response.data.terminal
  },

  sendTerminalInput: async (terminalId: string, text: string, submit: boolean = false): Promise<void> => {
    await api.post(`/api/terminals/${encodeURIComponent(terminalId)}/input`, { text, submit })
  },

  sendTerminalKey: async (terminalId: string, key: string): Promise<void> => {
    await api.post(`/api/terminals/${encodeURIComponent(terminalId)}/key`, { key })
  },

  resizeTerminal: async (terminalId: string, cols: number, rows: number): Promise<void> => {
    await api.post(`/api/terminals/${encodeURIComponent(terminalId)}/resize`, { cols, rows })
  },

  reportTerminalSizeHint: async (cols: number, rows: number): Promise<void> => {
    await api.post('/api/terminals/size-hint', { cols, rows })
  },

  // Observer APIs removed - no longer needed

  // Stop session/agent execution (preserves conversation history)
  stopSession: async (sessionId: string, cancelAgents: boolean = false): Promise<void> => {
    await api.post(`/api/session/stop${cancelAgents ? '?cancelAgents=true' : ''}`, {}, {
      headers: { 'X-Session-ID': sessionId }
    })
  },

  // Cancel only the currently running LLM turn for a session.
  cancelCurrentTurn: async (sessionId: string): Promise<void> => {
    await api.post('/api/session/cancel-turn', {}, {
      headers: { 'X-Session-ID': sessionId }
    })
  },

  // Dismiss session so it won't be auto-restored on page refresh
  dismissSession: async (sessionId: string): Promise<void> => {
    await api.post(`/api/sessions/${sessionId}/dismiss`)
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

  getChatHistoryConversation: async (sessionId: string, workspacePath?: string): Promise<ChatHistoryConversation> => {
    const params: Record<string, string> = {}
    if (workspacePath) params.workspace_path = workspacePath
    const response = await api.get(`/api/chat-history/sessions/${sessionId}`, { params })
    return response.data
  },

  listChatHistorySessions: async (limit = 80, offset = 0, workspacePath?: string): Promise<{ sessions: ChatHistorySession[] }> => {
    const params: Record<string, string | number> = { limit, offset }
    if (workspacePath) params.workspace_path = workspacePath
    const response = await api.get('/api/chat-history/sessions', { params })
    return response.data
  },

  cleanupChatHistorySessions: async (olderThanDays = 14, workspacePath?: string): Promise<{ success: boolean; result: ChatHistoryCleanupResult }> => {
    const params: Record<string, string | number> = { older_than_days: olderThanDays }
    if (workspacePath) params.workspace_path = workspacePath
    const response = await api.delete('/api/chat-history/sessions/cleanup', { params })
    return response.data
  },

  deleteChatHistorySession: async (sessionId: string, workspacePath?: string): Promise<{ success: boolean; result: ChatHistoryCleanupResult }> => {
    const params: Record<string, string> = {}
    if (workspacePath) params.workspace_path = workspacePath
    const response = await api.delete(`/api/chat-history/sessions/${sessionId}`, { params })
    return response.data
  },

  startRestoredTerminal: async (request: StartRestoredTerminalRequest): Promise<StartRestoredTerminalResponse> => {
    const response = await api.post('/api/chat-history/restored-terminal', request, { timeout: 95000 })
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

  getSessionExecutionTree: async (sessionId: string): Promise<SessionExecutionTreeResponse> => {
    const response = await api.get(`/api/sessions/${sessionId}/execution-tree`)
    return response.data
  },

  getWorkflowBuilderSession: async (
    presetQueryId?: string,
    workspacePath?: string
  ): Promise<WorkflowBuilderSessionResponse> => {
    const params: Record<string, string> = {}
    if (presetQueryId) params.preset_query_id = presetQueryId
    if (workspacePath) params.workspace_path = workspacePath
    return dedupedGet(
      `workflow-builder-session:${presetQueryId || ''}:${workspacePath || ''}`,
      async () => {
        const response = await api.get('/api/workflow/builder-session', { params })
        return response.data
      }
    )
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

  getMultiAgentChatCapabilities: async (): Promise<{ capabilities: WorkflowCapabilities; updated_at?: string }> => {
    const response = await api.get('/api/multiagent/chat-capabilities')
    return response.data
  },

  saveMultiAgentChatCapabilities: async (capabilities: WorkflowCapabilities): Promise<{ success: boolean; user_id: string }> => {
    const response = await api.post('/api/multiagent/chat-capabilities', capabilities)
    return response.data
  },

  // CDP Port Check — checks from the workspace container (where agent-browser runs)
  // if Chrome's remote debugging port is reachable via host.docker.internal.
  // Falls back to agent server check (host localhost) if workspace is unavailable.
  checkCdpPort: async (port: number): Promise<CdpCheckResult> => {
    const normalize = (data: unknown, source: 'workspace' | 'agent'): CdpCheckResult => {
      const value = (data || {}) as Record<string, unknown>
      return {
        connected: value.connected === true,
        error: typeof value.error === 'string' ? value.error : undefined,
        browser: typeof value.browser === 'string' ? value.browser : undefined,
        endpoint: typeof value.endpoint === 'string' ? value.endpoint : undefined,
        source,
      }
    }

    try {
      // Primary: check from workspace container (matches actual agent-browser runtime)
      const response = await workspaceApi.get(`/api/cdp-check?port=${port}`, { timeout: 5000 })
      const workspaceResult = normalize(response.data, 'workspace')
      if (!workspaceResult.connected) {
        try {
          const hostResponse = await api.get(`/api/cdp-check?port=${port}`, { timeout: 5000 })
          const hostResult = normalize(hostResponse.data, 'agent')
          if (hostResult.connected) {
            return {
              ...workspaceResult,
              error: workspaceResult.error
                ? `${workspaceResult.error}. Chrome CDP is reachable from the app host, but not from the workspace where browser tools run.`
                : 'Chrome CDP is reachable from the app host, but not from the workspace where browser tools run.',
            }
          }
        } catch {
          // Keep the workspace failure, because the workspace is the runtime that matters.
        }
      }
      return workspaceResult
    } catch {
      // Fallback: check from agent server (host machine)
      const response = await api.get(`/api/cdp-check?port=${port}`, { timeout: 5000 })
      return normalize(response.data, 'agent')
    }
  },

  // Browser process management — list and cleanup stale chromium instances in workspace container
  getBrowserProcesses: async (): Promise<{
    success: boolean;
    processes: Array<{
      pid: number;
      cpu: number;
      mem_mb: number;
      started_at: string;
      user_data_dir: string;
      type: string;
    }>;
    count: number;
  }> => {
    const response = await workspaceApi.get('/api/browser/processes', { timeout: 10000 });
    return response.data;
  },

  // Get tracked browser sessions from agent_go (includes session IDs, age, idle time)
  getBrowserSessionTracking: async (): Promise<{
    sessions: Array<{
      browser_session: string;
      agent_session: string;
      workflow_session: string;
      age: string;
      idle: string;
    }>;
    count: number;
  }> => {
    const response = await api.get('/api/browser/sessions', { timeout: 5000 });
    return response.data;
  },

  cleanupBrowserProcesses: async (pids?: number[]): Promise<{
    success: boolean;
    killed: number;
    message: string;
    remaining?: number;
  }> => {
    const body = pids ? { pids } : { all: true };
    const response = await workspaceApi.post('/api/browser/cleanup', body, { timeout: 10000 });
    return response.data;
  },

  // LLM Guidance Management
  // Set LLM guidance for a session
  setLLMGuidance: async (sessionId: string, guidance: string, memoryFolder?: string): Promise<LLMGuidanceResponse> => {
    const body: LLMGuidanceRequest = { session_id: sessionId, guidance }
    if (memoryFolder) body.memory_folder = memoryFolder
    const response = await api.post(`/api/sessions/${sessionId}/llm-guidance`, body, {
      headers: { 'X-Session-ID': sessionId }
    })
    return response.data
  },

  // Steer message - inject user message into running agent mid-execution
  steerMessage: async (sessionId: string, message: string): Promise<{
    success: boolean
    message?: string
    delivery_status?: 'sent_to_cli' | 'queued_for_injection'
    provider?: string
    message_id?: string
  }> => {
    const response = await api.post(`/api/sessions/${sessionId}/steer`, { message }, {
      headers: { 'X-Session-ID': sessionId }
    })
    return response.data
  },

  // Send a tmux control key (e.g. "Escape") to a running coding-agent session.
  // Only valid when the provider transport supports live input (claude-code,
  // gemini-cli, codex-cli, cursor-cli). Used to route ESC keystrokes to the
  // foreground CLI pane instead of cancelling the agent context.
  sendControlKey: async (sessionId: string, key: string): Promise<{
    success: boolean
    message?: string
    provider?: string
    key?: string
  }> => {
    const response = await api.post(`/api/sessions/${sessionId}/control`, { key }, {
      headers: { 'X-Session-ID': sessionId }
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

  // Per-user notification preferences (where workflow questions should land
  // when this user is the recipient). Falls back to the workspace default
  // when fields are blank.
  getNotificationPreferences: async (): Promise<NotificationPreference> => {
    const apiResponse = await api.get('/api/notification-preferences')
    return apiResponse.data
  },

  updateNotificationPreferences: async (pref: NotificationPreference): Promise<{ status: string }> => {
    const apiResponse = await api.post('/api/notification-preferences', pref)
    return apiResponse.data
  },

  // Test Slack connection. If config is provided, test that config without saving.
  // If called with no arg, the server tests the saved workspace config — in that
  // case we must send an empty body (not {}), otherwise the server parses {} as
  // a disabled config and refuses.
  testSlackConnection: async (config?: SlackConfigRequest): Promise<SlackTestResponse> => {
    const apiResponse = config
      ? await api.post('/api/human-feedback/slack/test', config)
      : await api.post('/api/human-feedback/slack/test')
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

  // --- Gmail (outbound-only) feedback channel ---

  getGmailFeedbackConfig: async (): Promise<GmailConfigResponse> => {
    const apiResponse = await api.get('/api/human-feedback/gmail/config')
    return apiResponse.data
  },

  updateGmailFeedbackConfig: async (config: GmailConfigRequest): Promise<GmailConfigResponse> => {
    const apiResponse = await api.post('/api/human-feedback/gmail/config', config)
    return apiResponse.data
  },

  // Auto-detected connection status (runs `gws auth status` server-side)
  getGmailStatus: async (): Promise<GmailAuthStatus> => {
    const apiResponse = await api.get('/api/human-feedback/gmail/status')
    return apiResponse.data
  },

  // Send a test email; optional config tests a recipient before saving
  testGmailConnection: async (config?: GmailConfigRequest): Promise<GmailTestResponse> => {
    const apiResponse = config
      ? await api.post('/api/human-feedback/gmail/test', config)
      : await api.post('/api/human-feedback/gmail/test')
    return apiResponse.data
  },

  // --- Bot Simulator API ---

  // Send a message to the bot simulator (synchronous — returns analysis result or conversational reply)
  // Pass thread_id to route follow-up messages into an existing thread/session
  simulateBotMessage: async (message: string, threadId?: string): Promise<SimulatorSendResponse> => {
    const response = await api.post('/api/bot/simulate/send', { message, thread_id: threadId })
    return response.data
  },

  // Get messages from a simulator thread
  getSimulatorMessages: async (threadId: string, since: number = 0): Promise<{ messages: SimulatorMessage[]; total: number }> => {
    const response = await api.get(`/api/bot/simulate/${threadId}/messages`, { params: { since } })
    return response.data
  },

  // Send a button interaction to the simulator
  simulateBotInteract: async (threadId: string, actionId: string, value: string): Promise<{ success: boolean }> => {
    const response = await api.post(`/api/bot/simulate/${threadId}/interact`, { action_id: actionId, value })
    return response.data
  },

  // Cleanup a simulator thread
  clearSimulatorThread: async (threadId: string): Promise<{ success: boolean }> => {
    const response = await api.delete(`/api/bot/simulate/${threadId}`)
    return response.data
  },

  // Get bot simulator config
  getSimulatorConfig: async (): Promise<{ delegation_tier_config?: Record<string, unknown>; default_servers?: string[]; default_skills?: string[] }> => {
    const response = await api.get('/api/bot/simulate/config')
    return response.data
  },

  // Save bot simulator config (delegation tier config + default servers/skills)
  saveBotConfig: async (config: {
    allowed_emails?: string[];
  }): Promise<{ success: boolean }> => {
    const response = await api.post('/api/bot/simulate/config', config)
    return response.data
  },

  // ── WhatsApp bot connector ────────────────────────────────────────────────
  // Status: is the connector enabled, paired, connected? When a pairing flow
  // is active, returns the QR expiration timestamp so the UI can auto-refresh.
  getWhatsAppStatus: async (): Promise<{
    enabled: boolean;
    paired: boolean;
    connected: boolean;
    own_jid: string;
    qr_available: boolean;
    qr_expires_at?: string;
    link_code?: string;
    link_code_expires_at?: string;
    bound_chat_count?: number;
    owner_user_id?: string;
    owner_email?: string;
    owner_username?: string;
    owner_paired_at?: string;
  }> => {
    const response = await api.get('/api/whatsapp/status')
    return response.data
  },

  // Returns the URL to the PNG QR. Kept for callers that need a direct URL.
  getWhatsAppPairURL: (size = 384, bust?: number): string => {
    const b = bust ?? Date.now()
    const token = getAuthToken()
    const tokenParam = token ? `&token=${encodeURIComponent(token)}` : ''
    return `${API_BASE_URL}/api/whatsapp/pair?size=${size}&_=${b}${tokenParam}`
  },

  // Fetches the QR PNG and preserves backend error text. This avoids the
  // <img>-tag failure mode where 409/503 responses only render as a broken image.
  getWhatsAppPairQR: async (size = 384, bust?: number): Promise<Blob> => {
    const response = await fetch(agentApi.getWhatsAppPairURL(size, bust), {
      method: 'GET',
      cache: 'no-store',
    })
    if (!response.ok) {
      const text = (await response.text()).trim()
      throw new Error(text || `WhatsApp pairing QR failed (${response.status})`)
    }
    return response.blob()
  },

  // Drops the paired account and restarts the connector with a fresh QR.
  unpairWhatsApp: async (): Promise<{ ok: boolean }> => {
    const response = await api.delete('/api/whatsapp/session')
    return response.data
  },

  // Slug → workflow routing for incoming WhatsApp messages. A message that
  // starts with "@<slug> " routes to the workflow mapped for that slug.
  getWhatsAppRouting: async (): Promise<{
    routing: Record<string, { workflow_id: string; workspace_path?: string; workshop_mode?: string; send_full_details?: boolean }>;
  }> => {
    const response = await api.get('/api/whatsapp/routing')
    return response.data
  },

  updateWhatsAppRouting: async (
    routing: Record<string, { workflow_id: string; workspace_path?: string; workshop_mode?: string; send_full_details?: boolean }>
  ): Promise<{
    routing: Record<string, { workflow_id: string; workspace_path?: string; workshop_mode?: string; send_full_details?: boolean }>;
  }> => {
    const response = await api.put('/api/whatsapp/routing', { routing })
    return response.data
  },

  // Save delegation tier config to workspace filesystem (shared by chat and bot connector)
  saveDelegationTierConfig: async (config: Record<string, unknown>, providerApiKeys?: Record<string, string>): Promise<{ success: boolean }> => {
    await api.put('/api/delegation-tier-config', config)
    // Save provider API keys to encrypted workspace file if provided
    if (providerApiKeys && Object.keys(providerApiKeys).length > 0) {
      await api.put('/api/provider-keys', providerApiKeys).catch(() => {})
    }
    return { success: true }
  },

  // Load delegation tier config from workspace filesystem
  getDelegationTierConfig: async (): Promise<Record<string, unknown>> => {
    const response = await api.get('/api/delegation-tier-config')
    return response.data
  },

  // Get available MCP servers and skills for bot config
  getAvailableCapabilities: async (): Promise<{ servers: string[]; skills: { name: string; description?: string }[] }> => {
    const response = await api.get('/api/bot/simulate/available-capabilities')
    return response.data
  },

  // List all simulator threads
  listSimulatorThreads: async (): Promise<{ threads: SimulatorThreadInfo[] }> => {
    const response = await api.get('/api/bot/simulate/threads')
    return response.data
  },

  // Get current simulator mode (threaded / non-threaded)
  getSimulatorMode: async (): Promise<{ threaded: boolean }> => {
    const response = await api.get('/api/bot/simulate/mode')
    return response.data
  },

  // Set simulator mode (threaded / non-threaded)
  setSimulatorMode: async (threaded: boolean): Promise<{ success: boolean; threaded: boolean }> => {
    const response = await api.post('/api/bot/simulate/mode', { threaded })
    return response.data
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
  getPlannerFiles: async (folder?: string, limit: number = -1, maxDepth?: number) => {
    const params: Record<string, string | number> = {}
    if (limit >= 0) params.limit = limit
    if (folder) params.folder = folder
    if (maxDepth !== undefined) params.max_depth = maxDepth
    const response = await workspaceApi.get('/api/documents', { params })
    return response.data
  },

  getPlannerFileContent: async (filepath: string) => {
    // API handles path conversion internally
    const response = await workspaceApi.get(`/api/documents/${encodeURIComponent(filepath)}`)
    return response.data
  },

  // Run a read-only SQL query against a workflow's db/db.sqlite. The workspace
  // service opens the connection mode=ro + query_only, so only reads succeed.
  // Returns { success, data: { columns, rows } } — rows are objects keyed by column.
  queryWorkflowDB: async (dbPath: string, sql: string) => {
    const response = await workspaceApi.post('/api/query', { db_path: dbPath, sql })
    return response.data as {
      success: boolean
      error?: string
      data?: { columns: string[]; rows: Record<string, unknown>[] }
    }
  },

  // List tables (schema + row count + sample) in a workflow's db/db.sqlite for
  // the read-only DatabasePopup inspector.
  getWorkflowDBTables: async (dbPath: string) => {
    const response = await workspaceApi.get('/api/db/tables', { params: { db_path: dbPath } })
    return response.data as {
      success: boolean
      error?: string
      data?: {
        tables: Array<{
          name: string
          columns: Array<{ name: string; type: string; primary_key: boolean }>
          row_count: number
          sample: Record<string, unknown>[]
        }>
      }
    }
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

  importWorkflowBackup: async (workspacePath: string, file: File, overwrite: boolean = false, onProgress?: (progress: number) => void): Promise<{ success: boolean; message: string; data?: { workspace_path: string; files_extracted: number; extracted_files: string[] } }> => {
    const formData = new FormData()
    formData.append('file', file)
    formData.append('workspace_path', workspacePath)
    formData.append('overwrite', overwrite.toString())

    const response = await workspaceApi.post('/api/workspace/import', formData, {
      headers: {
        'Content-Type': 'multipart/form-data',
      },
      onUploadProgress: (progressEvent) => {
        if (onProgress && progressEvent.total) {
          const progress = Math.round((progressEvent.loaded * 100) / progressEvent.total)
          onProgress(progress)
        }
      },
    })
    return response.data
  },

  searchDocuments: async (params: { query: string; search_type?: string; folder?: string; limit?: number }) => {
    const response = await workspaceApi.get('/api/search', { params })
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

  // Workflow running-session API (decoupled from chat session storage).
  getRunningWorkflow: async (sessionId: string): Promise<RunningWorkflowInfo> => {
    const response = await api.get(`/api/workflow/running/${sessionId}`)
    return response.data
  },

  listRunningWorkflows: async (): Promise<{ running: RunningWorkflowInfo[] }> => {
    const response = await api.get('/api/workflow/running')
    return response.data
  },

  updateRunningWorkflow: async (sessionId: string, patch: UpdateRunningWorkflowRequest): Promise<RunningWorkflowInfo> => {
    const response = await api.patch(`/api/workflow/running/${sessionId}`, patch)
    return response.data
  },

  // Global cost ledger summary (date + model aggregation).
  getCostSummary: async (from?: string, to?: string, signal?: AbortSignal): Promise<CostSummary> => {
    const params: Record<string, string> = {}
    if (from) params.from = from
    if (to) params.to = to
    const response = await api.get('/api/cost/summary', { params, signal })
    return response.data
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
    return dedupedGet(`workflow-status:${presetQueryId}`, async () => {
      const response = await api.get(`/api/workflow/status?preset_query_id=${encodeURIComponent(presetQueryId)}`)
      return response.data
    })
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
    return dedupedGet(`workflow-run-folders:${workspacePath}`, async () => {
      const response = await api.get('/api/workflow/run-folders', {
        params: { workspace_path: workspacePath }
      })
      return response.data
    })
  },

  // Create a new run folder (iteration)
  createRunFolder: async (workspacePath: string, triggeredBy?: string): Promise<CreateRunFolderResponse> => {
    const params: Record<string, string> = { workspace_path: workspacePath }
    if (triggeredBy) params.triggered_by = triggeredBy
    const response = await api.post('/api/workflow/run-folder', null, { params })
    return response.data
  },

  // Get active workflow executions (from backend in-memory registry)
  getActiveExecutions: async (workspacePath?: string): Promise<{ executions: import('./api-types').ActiveWorkflowExecution[] }> => {
    const params: Record<string, string> = {}
    if (workspacePath) params.workspace_path = workspacePath
    const response = await api.get('/api/workflow/active-executions', { params })
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

  // Get workspace-scoped cost data (phase + all run costs) for a workflow
  getCosts: async (workspacePath: string): Promise<WorkflowCostsResponse> => {
    const response = await api.get('/api/workflow/costs', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  getWorkflowReviewData: async (workspacePath: string, runFolder?: string): Promise<WorkflowReviewDataResponse> => {
    return dedupedGet(`workflow-review-data:${workspacePath}:${runFolder || ''}`, async () => {
      const response = await api.get('/api/workflow/review-data', {
        params: { workspace_path: workspacePath, run_folder: runFolder || '' }
      })
      return {
        ...response.data,
        costs: {
          ...response.data?.costs,
          runs: Array.isArray(response.data?.costs?.runs) ? response.data.costs.runs : [],
          phase_daily_costs: Array.isArray(response.data?.costs?.phase_daily_costs) ? response.data.costs.phase_daily_costs : [],
          run_daily_costs: Array.isArray(response.data?.costs?.run_daily_costs) ? response.data.costs.run_daily_costs : [],
        },
        evaluations: {
          ...response.data?.evaluations,
          reports: Array.isArray(response.data?.evaluations?.reports) ? response.data.evaluations.reports : [],
        },
      }
    })
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
    return {
      ...response.data,
      reports: Array.isArray(response.data?.reports) ? response.data.reports : [],
    }
  },

  // Auto-improvement framework API.
  // See docs/workflow/auto_improvement_framework.md.
  getAutoImprovementMetrics: async (workspacePath: string): Promise<{ success: boolean; file?: { metrics: any[] }; error?: string }> => {
    const response = await api.get('/api/workflow/metrics', { params: { workspace_path: workspacePath } })
    return response.data
  },
  getMetricsHistory: async (workspacePath: string): Promise<{ success: boolean; rows: any[]; error?: string }> => {
    const response = await api.get('/api/workflow/metrics-history', { params: { workspace_path: workspacePath } })
    return { ...response.data, rows: Array.isArray(response.data?.rows) ? response.data.rows : [] }
  },
  getBuilderDoc: async (workspacePath: string, doc: 'improve' | 'review' | 'soul', filePath?: string): Promise<{ success: boolean; doc: string; path: string; exists: boolean; content: string; error?: string }> => {
    const response = await api.get('/api/workflow/builder-doc', { params: { workspace_path: workspacePath, doc, path: filePath || '' } })
    return response.data
  },
  getBuilderDocsStatus: async (workspacePath: string): Promise<{
    success: boolean
    improve: { exists: boolean; last_modified?: string; path: string }
    review: { exists: boolean; last_modified?: string; path: string }
    error?: string
  }> => {
    const response = await api.get('/api/workflow/builder-docs-status', { params: { workspace_path: workspacePath } })
    return response.data
  },
  getBuilderDocArchives: async (workspacePath: string, doc: 'improve' | 'review' = 'improve'): Promise<{ success: boolean; files: Array<{ path: string; label: string }>; error?: string }> => {
    const response = await api.get('/api/workflow/builder-doc-archives', { params: { workspace_path: workspacePath, doc } })
    return { ...response.data, files: Array.isArray(response.data?.files) ? response.data.files : [] }
  },
  getPlanChangelog: async (workspacePath: string): Promise<import('./api-types').PlanChangelogResponse> => {
    const response = await api.get('/api/workflow/plan-changelog', { params: { workspace_path: workspacePath } })
    return { success: !!response.data?.success, entries: Array.isArray(response.data?.entries) ? response.data.entries : [], count: response.data?.count ?? 0, error: response.data?.error }
  },
  getFrameworkHealth: async (workspacePath: string): Promise<{
    success: boolean
    soul_exists: boolean
    objective_ok: boolean
    success_criteria_ok: boolean
    objective?: string
    success_criteria?: string
    declared_criteria: string[]
    uncovered_criteria: string[]
    unanchored_metrics: string[]
    telemetry_metrics: string[]
    error?: string
  }> => {
    const response = await api.get('/api/workflow/framework-health', { params: { workspace_path: workspacePath } })
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


  // Lightweight workflow summaries for dashboard pages (single call for all workflows)
  getWorkflowsSummary: async (workspacePaths: string[]): Promise<{
    success: boolean
    workflows: Array<{
      workspace_path: string
      total_runs: number
      latest_run: {
        folder: string
        status: string
        created_at?: string
        completed_at?: string
        completed_steps: number
        total_steps: number
        metrics_summary?: WorkflowMetricRunSummary
      } | null
      is_running: boolean
      active_run_folder?: string
    }>
  }> => {
    const keyPaths = workspacePaths.join(',')
    return dedupedGet(`workflows-summary:${keyPaths}`, async () => {
      const response = await api.get('/api/workflows/summary', {
        params: { workspace_paths: keyPaths }
      })
      return response.data
    })
  },

  // Rich overview rows for multiple workflows in one backend call.
  getWorkflowsOverview: async (workspacePaths: string[]): Promise<WorkflowOverviewBatchResponse> => {
    const response = await api.get('/api/workflows/overview', {
      params: { workspace_paths: workspacePaths.join(',') }
    })
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

  // Get step override (global config that overrides all steps)
  getStepOverride: async (
    workspacePath: string
  ): Promise<{ success: boolean; data: { agent_configs: AgentConfigs | null } }> => {
    const response = await api.get('/api/workflow/plan/step-override', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  // Update step override (global config that overrides all steps)
  // Pass null to clear all overrides
  updateStepOverride: async (
    workspacePath: string,
    agentConfigs: AgentConfigs | null
  ): Promise<{ success: boolean; message: string }> => {
    const response = await api.post('/api/workflow/plan/step-override', {
      workspace_path: workspacePath,
      agent_configs: agentConfigs
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

  getWorkflowBackup: async (workspacePath: string): Promise<WorkflowBackupInfoResponse> => {
    const response = await api.get('/api/workflow/backup', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  getWorkflowPublish: async (workspacePath: string): Promise<import('./api-types').WorkflowPublishInfoResponse> => {
    const response = await api.get('/api/workflow/publish', { params: { workspace_path: workspacePath } })
    return response.data
  },

  // Test image generation config by attempting to generate a sample image
  testImageGen: async (config: { provider: string; model_id: string; api_key?: string }): Promise<{ valid: boolean; message?: string; error?: string; image_url?: string; image_data?: string }> => {
    const response = await api.post('/api/image-gen/test', config)
    return response.data
  },

  // Employee API
  listEmployees: async (): Promise<{ employees: import('./api-types').Employee[] }> => {
    return dedupedGet('employees', async () => {
      const response = await api.get('/api/employees')
      return response.data
    })
  },

  createEmployee: async (employee: { name: string; avatar_color?: string; description?: string }): Promise<import('./api-types').Employee> => {
    const response = await api.post('/api/employees', employee)
    return response.data
  },

  updateEmployee: async (id: string, employee: { name?: string; avatar_color?: string; description?: string }): Promise<import('./api-types').Employee> => {
    const response = await api.put(`/api/employees/${id}`, employee)
    return response.data
  },

  deleteEmployee: async (id: string): Promise<void> => {
    await api.delete(`/api/employees/${id}`)
  },

  assignWorkflowEmployee: async (workspacePath: string, employeeId: string | null): Promise<void> => {
    await api.post('/api/employees/assign-workflow', {
      workspace_path: workspacePath,
      employee_id: employeeId,
    })
  },

  // --- Workflow Manifest API (file-backed workflow definitions) ---

  listWorkflowManifests: async (): Promise<ListWorkflowManifestsResponse> => {
    return dedupedGet('workflow-manifests', async () => {
      const response = await api.get('/api/workflows/manifests')
      return response.data
    })
  },

  getWorkflowManifest: async (workspacePath: string): Promise<GetWorkflowManifestResponse> => {
    const response = await api.get('/api/workflows/manifest', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  createWorkflowManifest: async (request: CreateWorkflowManifestRequest) => {
    const response = await api.post('/api/workflows/manifest', request)
    return response.data
  },

  updateWorkflowManifest: async (request: UpdateWorkflowManifestRequest) => {
    const response = await api.put('/api/workflows/manifest', request)
    return response.data
  },

  deleteWorkflowManifest: async (workspacePath: string) => {
    const response = await api.delete('/api/workflows/manifest', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  deleteWorkflowFolder: async (workspacePath: string) => {
    const response = await api.delete('/api/workflows/folder', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  duplicateWorkflowManifest: async (request: DuplicateWorkflowManifestRequest) => {
    const response = await api.post('/api/workflows/manifest/duplicate', request)
    return response.data
  },

  migrateWorkflowsToManifests: async (overwrite: boolean = false): Promise<MigrateWorkflowsResponse> => {
    const response = await api.post(`/api/workflows/migrate?overwrite=${overwrite}`)
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
  is_bot_manager?: boolean
  workflow_access?: 'read' | 'write' | 'owner'
  can_run_workflows?: boolean
  can_write_workflows?: boolean
  can_manage_workflow_access?: boolean
  workflow_permissions_enabled?: boolean
}

export interface AuthResponse {
  token: string
  user: AuthUser
}

export interface AuthUsersResponse {
  users: AuthUser[]
  total: number
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

export interface DesktopConnectResponse {
  connect_url: string
  server_url: string
  expires_at: string
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

  createDesktopConnect: async (): Promise<DesktopConnectResponse> => {
    const response = await api.post('/api/auth/desktop/connect')
    return response.data
  },

  exchangeDesktopConnect: async (serverUrl: string, code: string): Promise<AuthResponse> => {
    const response = await fetch(`${serverUrl.replace(/\/+$/, '')}/api/auth/desktop/exchange`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ code }),
    })
    if (!response.ok) {
      const text = await response.text()
      throw new Error(text || `Desktop connect failed (${response.status})`)
    }
    return response.json()
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

  listUsers: async (): Promise<AuthUsersResponse> => {
    const response = await api.get('/api/auth/users')
    return response.data
  },

  listWorkflowUserPermissions: async (): Promise<WorkflowUserPermissionsResponse> => {
    const response = await api.get('/api/workflow/user-permissions')
    return response.data
  },

  upsertWorkflowUserPermission: async (
    userKey: string,
    workflowAccess: 'read' | 'write' | 'owner'
  ): Promise<WorkflowUserPermission> => {
    const response = await api.put('/api/workflow/user-permissions', {
      user_key: userKey,
      workflow_access: workflowAccess,
    })
    return response.data
  },

  deleteWorkflowUserPermission: async (userKey: string): Promise<void> => {
    await api.delete(`/api/workflow/user-permissions?user_key=${encodeURIComponent(userKey)}`)
  },
}

export interface WorkflowUserPermission {
  user_key: string
  workflow_access: 'read' | 'write' | 'owner'
}

export interface WorkflowUserPermissionsResponse {
  permissions: WorkflowUserPermission[]
  total: number
}

// --- Workflow manifest API ---
export const workflowManifestApi = {
  listWorkflowManifests: async (): Promise<ListWorkflowManifestsResponse> => {
    const response = await api.get('/api/workflows/manifests')
    return response.data
  },

  getWorkflowManifest: async (workspacePath: string): Promise<GetWorkflowManifestResponse> => {
    const response = await api.get('/api/workflows/manifest', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  createWorkflowManifest: async (request: CreateWorkflowManifestRequest) => {
    const response = await api.post('/api/workflows/manifest', request)
    return response.data
  },

  updateWorkflowManifest: async (request: UpdateWorkflowManifestRequest) => {
    const response = await api.put('/api/workflows/manifest', request)
    return response.data
  },

  deleteWorkflowManifest: async (workspacePath: string) => {
    const response = await api.delete('/api/workflows/manifest', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  deleteWorkflowFolder: async (workspacePath: string) => {
    const response = await api.delete('/api/workflows/folder', {
      params: { workspace_path: workspacePath }
    })
    return response.data
  },

  duplicateWorkflowManifest: async (request: DuplicateWorkflowManifestRequest) => {
    const response = await api.post('/api/workflows/manifest/duplicate', request)
    return response.data
  },
}

export default api
