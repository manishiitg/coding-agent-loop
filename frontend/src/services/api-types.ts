// API-specific types (separate from event types)
import type { PollingEventSchema } from '../generated/events-bridge'

// LLM Configuration types
export interface LLMConfiguration {
  provider: 'openrouter' | 'bedrock' | 'openai'
  model_id: string
  fallback_models: string[]
  cross_provider_fallback?: {
    provider: 'openai' | 'bedrock' | 'openrouter'
    models: string[]
  }
  // API keys for each provider
  api_keys?: {
    openrouter?: string
    openai?: string
    bedrock?: {
      region: string
      // AWS credentials handled via IAM roles
    }
  }
}

// Extended LLM Configuration with API keys for frontend use
export interface ExtendedLLMConfiguration extends LLMConfiguration {
  api_key?: string
  region?: string
}

// Agent streaming types
export interface AgentQueryRequest {
  query: string
  servers?: string[]
  provider?: 'bedrock' | 'openai' | 'openrouter'
  model_id?: string
  temperature?: number
  max_turns?: number
  enabled_tools?: string[]
  enabled_servers?: string[]
  agent_mode?: 'simple' | 'ReAct' | 'orchestrator' | 'workflow'
  llm_config?: LLMConfiguration
  preset_query_id?: string
}

export interface AgentQueryResponse {
  query_id: string
  status: string
  message?: string
  sse_endpoint?: string
  observer_id?: string
  session_id?: string
}

// LLM Defaults Configuration Response
export interface LLMDefaultsResponse {
  primary_config: LLMConfiguration
  openrouter_config: ExtendedLLMConfiguration
  bedrock_config: ExtendedLLMConfiguration
  openai_config: ExtendedLLMConfiguration
  available_models: {
    bedrock: string[]
    openrouter: string[]
    openai: string[]
  }
}

// API Key Validation Request/Response
export interface APIKeyValidationRequest {
  provider: 'openrouter' | 'openai' | 'bedrock'
  api_key?: string // Optional for Bedrock (uses IAM credentials)
  model_id?: string // Optional model ID for Bedrock validation
}

export interface APIKeyValidationResponse {
  valid: boolean
  message?: string
  error?: string
}

// LLM Guidance types
export interface LLMGuidanceRequest {
  session_id: string
  guidance: string
}

export interface LLMGuidanceResponse {
  session_id: string
  status: string
  message?: string
  guidance?: string
}

// Human Feedback types
export interface HumanFeedbackRequest {
  unique_id: string
  response: string
}

export interface HumanFeedbackResponse {
  unique_id: string
  status: string
  message?: string
}

export interface AgentStreamEvent {
  type: string
  query_id: string
  timestamp: string
  data?: Record<string, unknown>
  content?: string
  error?: string
}

// Polling API types
export interface RegisterObserverRequest {
  session_id?: string
}

export interface RegisterObserverResponse {
  observer_id: string
  status: string
  message: string
}

// Use the PollingEventSchema type from generated events
export type PollingEvent = PollingEventSchema & {
  id: string
  parent_id?: string
  hierarchy_level?: number
  span_id?: string
  trace_id?: string
  correlation_id?: string
  session_id?: string
  component?: string
  event_index?: number
}

export interface GetEventsResponse {
  events: PollingEvent[]
  last_event_index: number
  has_more: boolean
  observer_id: string
}

export interface ObserverStatusResponse {
  observer_id: string
  status: string
  created_at: string
  last_activity: string
  total_events: number
}

// Active Session Management Types
export interface ActiveSessionInfo {
  session_id: string
  observer_id: string
  agent_mode: string
  status: string // "running", "paused", "completed"
  last_activity: string
  created_at: string
  query?: string
}

export interface GetActiveSessionsResponse {
  active_sessions: ActiveSessionInfo[]
  total: number
}

export interface ReconnectSessionResponse {
  observer_id: string
  session_id: string
  status: string
  agent_mode: string
  message: string
}

export interface SessionStatusResponse {
  session_id: string
  status: string // "active", "completed", "not_found"
  agent_mode?: string
  observer_id?: string
  created_at?: string
  last_activity?: string
  completed_at?: string
  query?: string
}

// Define MCPServerConfig type to match backend
export type MCPServerConfig = {
  command: string;
  args: string[];
  env?: Record<string, string>;
  description?: string;
};

export type ToolDetail = {
  name: string;
  description: string;
  parameters?: Record<string, {
    description?: string;
    type?: string;
  }>;
  required?: string[];
};

export type ToolDefinition = {
  name: string;
  description: string;
  parameters: Record<string, unknown>;
  status?: string;
  error?: string;
  server?: string;
  toolsEnabled?: number;
  function_names?: string[];
  tools?: ToolDetail[];
};

// Planner API types
export interface PlannerFile {
  filepath: string;
  content: string;
  last_modified: string;
  folder?: string;
  type?: 'file' | 'folder';
  children?: PlannerFile[];
  depth?: number;
  is_image?: boolean;
}

export interface PlannerFileContent {
  filepath: string;
  content: string;
  last_modified: string;
  folder?: string;
  is_image?: boolean;
}

export interface PlannerFilesResponse {
  success: boolean;
  message: string;
  data: PlannerFile[];
}

export interface PlannerFolderChildrenResponse {
  success: boolean;
  message: string;
  data: {
    children: PlannerFile[];
    total: number;
    folderPath: string;
  };
}

export interface CreateFolderRequest {
  folder_path: string;
  commit_message?: string;
}

export interface CreateFolderResponse {
  folder_path: string;
  last_modified: string;
  created: boolean;
}

// Git Sync types
export interface GitSyncStatus {
  is_connected: boolean;
  last_sync?: string;
  pending_changes: number;
  pending_files: string[];
  file_statuses: FileStatus[];
  conflicts: GitConflict[];
  repository: string;
  branch: string;
}

export interface FileStatus {
  file: string;
  status: string;
  staged: boolean;
}

// File Version types
export interface FileVersion {
  commit_hash: string;
  commit_message: string;
  author: string;
  date: string;
  content?: string;
  diff?: string;
}

export interface FileVersionHistoryRequest {
  limit?: number;
}

export interface GitConflict {
  file: string;
  message: string;
  type: 'merge' | 'push' | 'pull';
}

// Semantic Search Sync types
export interface SemanticSearchStatus {
  enabled?: boolean; // Optional property for disabled state
  services: {
    qdrant: {
      available: boolean;
    };
    embedding: {
      available: boolean;
      model: {
        available: boolean;
        enabled: boolean;
        model: string;
        provider: string;
      };
    };
  };
  jobs: {
    job_stats: {
      completed: number;
      pending: number;
      processing: number;
      failed?: number;
    };
    running: boolean;
    worker_count: number;
  };
  timestamp: number;
}

export interface SemanticJobStatus {
  job_stats: {
    completed: number;
    pending: number;
    processing: number;
    failed?: number;
  };
  running: boolean;
  worker_count: number;
}

export interface SemanticResyncRequest {
  dry_run?: boolean;
  force?: boolean;
}

export interface SemanticResyncResponse {
  success: boolean;
  message: string;
  data: {
    docs_dir: string;
    qdrant_url: string;
    dry_run: boolean;
    force: boolean;
    status: string;
    note: string;
  };
}

export interface GitSyncRequest {
  force: boolean;
  resolve_conflicts: boolean;
}

export interface GitSyncResponse {
  success: boolean;
  message: string;
  data: {
    status: 'synced' | 'up_to_date' | 'error';
    commit_message?: string;
    repository?: string;
    branch?: string;
    timestamp?: string;
  };
}


// Chat History API types
export interface ChatSession {
  id: string;
  session_id: string;
  title: string;
  agent_mode?: string;
  preset_query_id?: string;
  created_at: string;
  completed_at?: string;
  status: string;
  last_activity?: string;
}

export interface ChatEvent {
  id: string;
  session_id: string;
  chat_session_id: string;
  event_type: string;
  timestamp: string;
  event_data: Record<string, unknown>;
}

export interface ChatHistorySummary {
  chat_session_id: string;
  session_id: string;
  title: string;
  agent_mode?: string;
  status: string;
  created_at: string;
  completed_at?: string;
  total_events: number;
  total_turns: number;
  last_activity?: string;
}

export interface ListChatSessionsResponse {
  sessions: ChatSession[];
  total: number;
  limit: number;
  offset: number;
}

export interface GetSessionEventsResponse {
  events: ChatEvent[];
  total: number;
  limit: number;
  offset: number;
}

export interface CreateChatSessionRequest {
  session_id: string;
  title?: string;
  preset_query_id?: string;
}

export interface UpdateChatSessionRequest {
  title?: string;
  status?: string;
  completed_at?: string;
}

// Preset Query API types
export interface PresetQuery {
  id: string;
  label: string;
  query: string;
  selected_servers: string; // JSON string
  selected_folder: string; // Single folder path
  agent_mode: string;
  is_predefined: boolean;
  created_at: string;
  updated_at: string;
  created_by: string;
}

export interface CreatePresetQueryRequest {
  label: string;
  query: string;
  selected_servers?: string[];
  selected_folder?: string; // Single folder path
  agent_mode?: string;
  is_predefined?: boolean;
}

export interface UpdatePresetQueryRequest {
  label?: string;
  query?: string;
  selected_servers?: string[];
  selected_folder?: string; // Single folder path
  agent_mode?: string;
}

export interface ListPresetQueriesResponse {
  presets: PresetQuery[];
  total: number;
  limit: number;
  offset: number;
}

// Workflow types
export interface WorkflowSelectedOption {
  option_id: string;
  option_label: string;
  option_value: string;
  group: string;
  phase_id: string;
}

export interface WorkflowSelectedOptions {
  phase_id: string;
  selections: WorkflowSelectedOption[];
}

export interface Workflow {
  id: string;
  preset_query_id: string;
  workflow_status: string;
  selected_options: WorkflowSelectedOptions | null;
  created_at: string;
  updated_at: string;
}

export interface WorkflowStatusResponse {
  success: boolean;
  workflow?: Workflow;
  status?: {
    is_ready: boolean;
    requires_verification: boolean;
    can_execute: boolean;
  };
  message?: string;
}

// Workflow Constants API types
export interface WorkflowPhaseOption {
  id: string;
  label: string;
  description: string;
  group: string;
  default: boolean;
}

export interface WorkflowPhase {
  id: string;
  title: string;
  description: string;
  options?: WorkflowPhaseOption[];
}

export interface WorkflowStatus {
  id: string;
  title: string;
  description: string;
}

export interface WorkflowConstants {
  phases: WorkflowPhase[];
}

export interface WorkflowConstantsResponse {
  success: boolean;
  constants: WorkflowConstants;
  message: string;
}

// MCP Registry types
export interface MCPRegistryServer {
  id: string;
  name: string;
  description: string;
  version: string;
  author: string;
  repository: string;
  tags: string[];
  category: string;
  installation: {
    command: string;
    args: string[];
    env?: Record<string, string>;
    dependencies?: string[];
  };
  documentation: string;
  examples: string[];
}

export interface MCPRegistrySearchParams {
  query?: string;
  category?: string;
  tags?: string[];
  limit?: number;
  offset?: number;
}

export interface MCPRegistryResponse {
  servers: MCPRegistryServer[];
  total: number;
  limit: number;
  offset: number;
} 