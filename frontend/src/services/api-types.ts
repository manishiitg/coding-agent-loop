// API-specific types (separate from event types)
import type { PollingEventSchema } from '../generated/event-types'
import type { EventTypeString } from '../generated/event-types'
import type { ValidationSchema, AgentConfigs } from '../utils/stepConfigMatching'

// New LLM Configuration types (Tiered Fallback System)
export interface LLMModel {
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli' | 'minimax' | 'minimax-coding-plan'
  model_id: string

  // Auth per model (each model carries its own credentials)
  api_key?: string      // For OpenRouter, OpenAI, Anthropic, Vertex, Azure
  region?: string       // For Bedrock, Azure
  endpoint?: string     // For Azure (endpoint URL)

  // Model-specific options (reasoning_effort, thinking_level, thinking_budget, etc.)
  options?: Record<string, unknown>

  // Model-specific temperature (0.0 - 1.0)
  temperature?: number
}

// Saved/Published LLM Configuration (User library)
export interface SavedLLM extends LLMModel {
  id: string
  name: string
  model_name?: string // Display name from metadata (e.g., "Claude 3.5 Sonnet")
  auth_method?: 'api_key' | 'oauth' | 'none' // Auth method used
  created_at?: string
}

export interface AgentLLMConfiguration {
  // Primary LLM
  primary: LLMModel

  // Fallback LLMs (ordered array - fallback in this exact order)
  fallbacks: LLMModel[]
}

// Legacy LLM Configuration types (kept for backward compatibility)
export interface LLMConfiguration {
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli' | 'minimax' | 'minimax-coding-plan'
  model_id: string
  fallback_models: string[]
  cross_provider_fallback?: {
    provider: 'openai' | 'bedrock' | 'openrouter' | 'vertex' | 'anthropic' | 'azure' | 'claude-code'
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
    anthropic?: string
    vertex?: string
    azure?: {
      endpoint: string
      api_key: string
      api_version?: string
      region?: string
    }
  }
}

// ExtendedLLM Configuration for frontend (secrets/UI-only)
export type ExtendedLLMConfiguration = Omit<LLMConfiguration, 'api_keys'> & {
  api_key?: string
  region?: string
  endpoint?: string  // Azure endpoint URL
  options?: Record<string, unknown>
  temperature?: number
}

// Execution mode constants matching backend enum

// Agent streaming types
export interface AgentQueryRequest {
  query: string
  provider?: 'bedrock' | 'openai' | 'openrouter' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli'
  model_id?: string
  temperature?: number
  max_turns?: number
  enabled_tools?: string[]
  enabled_servers?: string[]
  selected_tools?: string[] // Array of "server:tool" strings
  agent_mode?: 'simple' | 'workflow' | 'workflow_phase'
  // Workflow phase chat: specifies which phase to run as a chat session
  phase_id?: string
  // Support both legacy and new config format
  llm_config?: LLMConfiguration | AgentLLMConfiguration
  preset_query_id?: string
  // Code execution mode: When enabled, only virtual tools are added to LLM
  // MCP tools are accessed via generated Go code using discover_code_files and write_code
  use_code_execution_mode?: boolean
  // Tool search mode: When enabled, agents discover tools on-demand via search_tools
  use_tool_search_mode?: boolean
  pre_discovered_tools?: string[] // Tools always available without searching
  // Execution options from frontend (for workflow execution phase)
  execution_options?: ExecutionOptions
  // Context summarization configuration
  enable_context_summarization?: boolean // Enable context summarization feature
  summarize_on_max_turns?: boolean // Automatically summarize when max turns is reached
  summary_keep_last_messages?: number // Number of recent messages to keep when summarizing (default: 8)
  // Workspace access configuration
  enable_workspace_access?: boolean // Enable/disable workspace file access tools (default: true)
  // Browser automation access configuration
  enable_browser_access?: boolean // Enable/disable browser automation tool (auto-enables workspace when true)
  // Google Workspace access configuration
  enable_gws_access?: boolean // Enable/disable Google Workspace CLI access
  // CDP port for connecting to an existing Chrome browser (local mode only)
  cdp_port?: number
  // Context editing configuration
  enable_context_editing?: boolean // Enable context editing (dynamic context reduction)
  // Selected skills to include in the chat context
  selected_skills?: string[] // Array of skill folder names
  // Selected sub-agent templates for delegation
  selected_subagents?: string[] // Array of sub-agent template folder names
  // Delegation mode: 'spawn' = simple delegate only, 'plan' = plan-driven + delegate, undefined/absent = disabled
  delegation_mode?: 'spawn' | 'plan'
  // Plan phase override: 'planning' = plan first (default), 'execution' = skip planning and execute directly
  plan_phase?: 'planning' | 'execution'
  // Delegation tier configuration: Maps reasoning levels to specific provider/model pairs
  delegation_tier_config?: DelegationTierConfig
  // Decrypted secrets to pass to backend (injected into agent system prompt, never in query text)
  decrypted_secrets?: Array<{ name: string; value: string }>
  // Selected global secret names to include (if omitted, all global secrets are included)
  selected_global_secrets?: string[]
  // Workspace paths of workflows to inject context for (via # selector in chat)
  workflow_context_paths?: string[]
  // Image generation configuration
  enable_image_generation?: boolean
  image_gen_config?: {
    provider: string
    model_id: string
    api_key?: string
  }
  // Existing plan folder to reuse (skips creating new folder in multi-agent mode)
  plan_folder?: string
}

// Delegation tier configuration for multi-LLM support
export interface DelegationTierConfig {
  main?: TierModel    // orchestrator/main agent model
  high?: TierModel
  medium?: TierModel
  low?: TierModel
  custom?: Record<string, CustomTierModel>  // slug → custom tier
}

export interface TierModel {
  provider: string
  model_id: string
}

export interface CustomTierModel {
  description: string // LLM guidance, e.g. "low cost model for code reviews"
  provider: string
  model_id: string
}

export interface AgentQueryResponse {
  query_id: string
  status: string
  message?: string
  sse_endpoint?: string
  session_id?: string
}

// LLM Defaults Configuration Response
export interface LLMDefaultsResponse {
  primary_config: LLMConfiguration
  openrouter_config: ExtendedLLMConfiguration
  bedrock_config: ExtendedLLMConfiguration
  openai_config: ExtendedLLMConfiguration
  vertex_config?: ExtendedLLMConfiguration
  anthropic_config?: ExtendedLLMConfiguration
  azure_config?: ExtendedLLMConfiguration
  minimax_config?: ExtendedLLMConfiguration
  minimax_coding_plan_config?: ExtendedLLMConfiguration
  available_models: {
    bedrock: string[]
    openrouter: string[]
    openai: string[]
    vertex?: string[]
    anthropic?: string[]
    azure?: string[]
    minimax?: string[]
    'minimax-coding-plan'?: string[]
  }
  supported_providers?: ('openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli' | 'minimax' | 'minimax-coding-plan')[]
  /** When true, LLM config is locked by admin; do not show editable modal, use server env only */
  llm_config_locked?: boolean
  /** Default published LLMs from server (e.g. one "Gemini" entry); when locked, list is read-only */
  default_published_llms?: SavedLLM[]
  /** When true, default published LLMs list is locked (no add/delete/edit) */
  default_published_llms_locked?: boolean
  /** List of provider names that are locked (read-only) because they are fully configured via server env */
  locked_providers?: string[]
}

// API Key Validation Request/Response
export interface APIKeyValidationRequest {
  provider: 'openrouter' | 'openai' | 'bedrock' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli' | 'minimax' | 'minimax-coding-plan'
  api_key?: string // Optional for Bedrock (uses IAM credentials)
  model_id?: string // Optional model ID for Bedrock validation
  endpoint?: string // Azure endpoint URL
  options?: Record<string, unknown> // Model options like reasoning_effort, thinking_level, etc.
}

export interface APIKeyValidationResponse {
  valid: boolean
  message?: string
  error?: string
  corrected_options?: {
    endpoint?: string
    model_id?: string
    api_version?: string
    [key: string]: unknown
  }
}

// LLM Guidance types
export interface LLMGuidanceRequest {
  session_id: string
  guidance: string
  memory_folder?: string  // Optional override for memory storage path
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

// Context Summarization types
export interface SummarizeConversationRequest {
  keep_last_messages?: number // Optional: number of recent messages to keep (default: 8)
}

export interface SummarizeConversationResponse {
  session_id: string
  status: string
  message?: string
  original_count?: number
  new_count?: number
  reduced_by?: number
  summary?: string
}

export interface CompactContextRequest {
  token_threshold?: number // Optional: token threshold (default: 1000)
  turn_threshold?: number  // Optional: turn age threshold (default: 10)
}

export interface CompactContextResponse {
  session_id: string
  status: string
  message?: string
  total_messages?: number
  compacted_count?: number
  total_tokens_saved?: number
}

// Slack Feedback Configuration types
export interface SlackConfig {
  enabled: boolean
  bot_token?: string  // Masked in GET response
  app_token?: string  // Masked in GET response (App-level token for Socket Mode)
  channel_id?: string
  bot_mode?: boolean  // Enable @mention bot mode
}

export interface SlackConfigRequest {
  enabled: boolean
  bot_token: string  // Bot User OAuth Token (xoxb-...)
  app_token: string  // App-level token (xapp-...) for Socket Mode
  channel_id: string
  bot_mode: boolean  // Enable @mention bot mode
}

export interface SlackConfigResponse {
  enabled: boolean
  bot_token?: string  // Masked in GET
  app_token?: string  // Masked in GET
  channel_id?: string
  bot_mode?: boolean
}

export interface SlackTestResponse {
  success: boolean
  message: string
  test_id?: string  // Unique ID for polling test replies
}

export interface SlackTestReplyResponse {
  test_id: string
  reply: string
  received: boolean
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

// Observer APIs removed - no longer needed

// Use the PollingEventSchema type from generated events
// Extends with additional runtime fields that may not be in schema
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

// Re-export the EventTypeString for convenience
export type { EventTypeString }

export interface GetEventsResponse {
  events: PollingEvent[]
  has_more: boolean
  session_id: string
  session_status: string // Session status: "running", "completed", "error", "stopped", "inactive" (required - source of truth)
  last_processed_index?: number // Last index processed in unfiltered array (for correct sinceIndex tracking when filtering)
  has_running_background_agents?: boolean // Whether background agents are still running for this session
}

// Observer APIs removed - no longer needed

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
  oauth?: {
    auto_discover?: boolean;
    client_id?: string;
    client_secret?: string;
    auth_url?: string;
    token_url?: string;
    redirect_url?: string;
    scopes?: string[];
    use_pkce?: boolean;
    token_file?: string;
  };
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
  // OAuth auto-detection
  requires_oauth?: boolean;
  oauth_endpoints?: {
    auth_url: string;
    token_url: string;
  };
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
  // Store original path when filepath is adjusted for display (e.g., in workflow mode)
  originalFilepath?: string;
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

export interface CopyFolderRequest {
  source_path: string;
  destination_path: string;
  commit_message?: string;
}

export interface CopyFolderResponse {
  source_path: string;
  destination_path: string;
  files_copied: number;
  dirs_created: number;
}

// Git Sync types
export interface GitSyncStatus {
  is_connected: boolean;
  last_sync?: string;
  pending_changes: number;
  unpushed_commits: number;
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


// Workflow metadata for background/minimized workflows
// Stored in session config to enable querying and restoring background workflows
export interface WorkflowMetadata {
  preset_id?: string;           // Preset ID for context restoration
  preset_name?: string;         // Display name
  workspace_path?: string;      // Workflow workspace path
  run_folder?: string;          // Current run folder (e.g., "iteration-1")
  phase_id?: string;            // Current phase ID (e.g., "execution")
  phase_name?: string;          // Phase display name
  is_minimized?: boolean;       // True when workflow is in background
  minimized_at?: number;        // Unix timestamp (ms) when minimized
  step_progress?: StepProgress; // Current step progress
  current_step_id?: string;     // Currently executing step ID
  current_step_title?: string;  // Currently executing step title
  last_polled?: number;         // Unix timestamp (ms) of last status check
}

// Chat Session Configuration
export interface ChatSessionConfig {
  selected_servers?: string[];
  enabled_servers?: string[];
  use_code_execution_mode?: boolean;
  use_tool_search_mode?: boolean;
  pre_discovered_tools?: string[];
  enable_context_summarization?: boolean;
  llm_config?: {
    provider?: string;
    model_id?: string;
    fallback_models?: string[];
    cross_provider_fallback?: {
      provider: string;
      models: string[];
    };
  };
  file_context?: Array<{
    name: string;
    path: string;
    type: 'file' | 'folder';
  }>;
  enable_workspace_access?: boolean;
  workflow_metadata?: WorkflowMetadata; // Workflow-specific metadata (for background workflows)
  selected_skills?: string[]; // Selected skill folder names
  selected_subagents?: string[]; // Selected sub-agent template folder names
  delegation_mode?: 'off' | 'spawn' | 'plan'; // Delegation mode for multi-agent sessions
  delegation_tier_config?: DelegationTierConfig; // Delegation tier model config
  plan_id?: string; // Active plan ID (for session resume)
  plan_folder?: string; // Active plan folder path
  plan_phase?: string; // Plan phase: "planning" or "execution"
}

// Chat History API types
export interface ChatSession {
  id: string;
  session_id: string;
  title: string;
  agent_mode?: string;
  preset_query_id?: string;
  config?: ChatSessionConfig; // Typed configuration
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
  preset_query_id?: string;
  config?: ChatSessionConfig; // Typed configuration
  status: string;
  created_at: string;
  completed_at?: string;
  total_events: number;
  total_turns: number;
  last_activity?: string;
}

export interface ListChatSessionsResponse {
  sessions: ChatHistorySummary[]; // Backend returns ChatHistorySummary with total_events
  total: number;
  limit: number;
  offset: number;
}

export interface GetSessionEventsResponse {
  events: PollingEvent[]; // Same structure as polling API
  total: number;
  limit: number;
  offset: number;
}

// SSE message types (match backend sseEventMessage / sseStatusMessage)
export interface SSEEventMessage {
  events: PollingEvent[]
  session_status?: string
  last_processed_index: number
  has_running_background_agents?: boolean
}

export interface SSEStatusMessage {
  session_status?: string
  has_running_background_agents?: boolean
}

export interface CreateChatSessionRequest {
  session_id: string;
  title?: string;
  preset_query_id?: string;
}

export interface UpdateChatSessionRequest {
  title?: string;
  agent_mode?: string;
  preset_query_id?: string;
  status?: string;
  completed_at?: string;
}

// Preset LLM Configuration types
export interface AgentLLMFallback {
  provider: string
  model_id: string
}

export interface AgentLLMConfig {
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli'
  model_id: string
  fallbacks?: AgentLLMFallback[]
}

export interface PresetLLMConfig {
  // Legacy: Single default model (for backward compatibility)
  provider?: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli'
  model_id?: string

  // New: Agent-specific default models (takes priority over legacy fields)
  execution_llm?: AgentLLMConfig        // Default for execution agents
  learning_llm?: AgentLLMConfig         // Default for learning agents
  phase_llm?: AgentLLMConfig            // Default for all phase agents (planning, anonymization, plan improvement, etc.)

  // Feature toggles
  use_knowledgebase?: boolean           // nil/true = enabled (default), false = disabled
  enable_context_summarization?: boolean // nil/true = enabled (default), false = disabled
  enable_context_editing?: boolean       // nil/false = disabled (default), true = enabled

  // Tiered LLM allocation mode
  llm_allocation_mode?: 'manual' | 'tiered'
  tiered_config?: {
    tier_1: AgentLLMConfig
    tier_2: AgentLLMConfig
    tier_3: AgentLLMConfig
  }
}

// Preset Query API types
export interface PresetQuery {
  id: string;
  label: string;
  query?: string;
  selected_servers: string; // JSON string
  selected_tools: string; // JSON string of "server:tool" array
  selected_folder?: string; // Single folder path (nullable)
  agent_mode: string;
  llm_config: string; // JSON string of PresetLLMConfig
  use_code_execution_mode?: boolean;
  use_tool_search_mode?: boolean;
  pre_discovered_tools?: string; // JSON string of tools array
  selected_skills?: string; // JSON string of skill folder names
  selected_secrets?: string; // JSON string of secret names
  selected_global_secret_names?: string; // JSON string of global secret names (null=all)
  enable_context_summarization?: boolean; // Enable context summarization
  enable_context_editing?: boolean; // Enable context editing (dynamic context reduction)
  enable_browser_access?: boolean; // Browser automation access
  is_predefined: boolean;
  created_at: string;
  updated_at: string;
  created_by: string;
}

export interface CreatePresetQueryRequest {
  label: string;
  query?: string;
  selected_servers?: string[];
  selected_tools?: string[]; // NEW
  selected_folder?: string; // Single folder path
  agent_mode?: string;
  llm_config?: PresetLLMConfig; // LLM configuration for this preset
  use_code_execution_mode?: boolean; // MCP code execution mode
  use_tool_search_mode?: boolean; // Tool search mode
  pre_discovered_tools?: string[]; // Tools always available without searching
  selected_skills?: string[]; // Skill folder names for workflow
  selected_secrets?: string[]; // Secret names for workflow
  selected_global_secret_names?: string[]; // Global secret names (null=all, []=none)
  enable_context_summarization?: boolean; // Enable context summarization
  enable_context_editing?: boolean; // Enable context editing (dynamic context reduction)
  enable_browser_access?: boolean; // Browser automation access
  is_predefined?: boolean;
}

export interface UpdatePresetQueryRequest {
  label?: string;
  query?: string;
  selected_servers?: string[];
  selected_tools?: string[]; // NEW
  selected_folder?: string; // Single folder path
  agent_mode?: string;
  llm_config?: PresetLLMConfig; // LLM configuration for this preset
  use_code_execution_mode?: boolean; // MCP code execution mode
  use_tool_search_mode?: boolean; // Tool search mode
  pre_discovered_tools?: string[]; // Tools always available without searching
  selected_skills?: string[]; // Skill folder names for workflow
  selected_secrets?: string[]; // Secret names for workflow
  selected_global_secret_names?: string[]; // Global secret names (null=all, []=none)
  enable_context_summarization?: boolean; // Enable context summarization
  enable_context_editing?: boolean; // Enable context editing (dynamic context reduction)
  enable_browser_access?: boolean; // Browser automation access
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

export interface CapabilitiesResponse {
  providers: string[];
  streaming: boolean;
  sse: boolean;
  agent_modes: string[];
  tracing: {
    enabled: boolean;
    provider: string;
  };
  workspace: {
    semantic_search_enabled: boolean;
    github_sync_enabled: boolean;
  };
  servers: string[];
  local_mode?: boolean;
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

// Workflow Run Folders API types
export interface RunFolderInfo {
  name: string;
  progress?: StepProgress; // Progress info if available
}

export interface RunFoldersResponse {
  folders: RunFolderInfo[]; // Changed from string[] to RunFolderInfo[]
  total_count: number;
  showing_count: number;
}

export interface CreateRunFolderResponse {
  success: boolean;
  folder_name: string;
  message: string;
}

// Branch step progress for conditional steps
export interface BranchStepProgress {
  branch_executed: string;  // "if_true" or "if_false"
  completed_steps: string[];
}

// Execution progress for a run folder
export interface StepProgress {
  completed_step_indices: number[];  // 0-based indices
  total_steps: number;
  last_updated: string;  // ISO timestamp
  branch_steps?: Record<number, BranchStepProgress>;  // key is step index (0-based)
  last_completed_step_id?: string;  // Step ID for direct node updates (from step_progress_updated event)
}

export interface ProgressResponse {
  exists: boolean;
  progress: StepProgress | null;
}

// Execution options for frontend-controlled execution
// Note: AgentLLMConfig is already defined above (line ~462)
export interface ExecutionOptions {
  run_mode: 'use_same_run' | 'create_new_runs_always';
  selected_run_folder?: string;
  execution_strategy: string;
  resume_from_step?: number;  // 1-based step number (for top-level steps)
  resume_from_branch_step?: {  // For resuming from branch steps
    parent_step_index: number;  // 0-based index of conditional step
    branch_type: 'if_true' | 'if_false';  // Which branch
    branch_step_index: number;  // 0-based index within the branch
  };
  fast_execute_end_step?: number;  // 0-based last step for fast execute range
  plan_change_action?: 'keep_old_progress' | 'delete_old_progress';
  all_steps_completed_action?: 'fast_execute_again' | 'skip_execution';
  
  // Temporary LLM overrides (optional, overrides step-level configs for this execution only)
  // Only applies to execution agents (not validation or learning agents)
  // Cascading fallback: tempLLM1 → tempLLM2 → step LLM (on validation failures)
  temp_override_llm?: AgentLLMConfig;  // First override LLM (used on first attempt)
  temp_override_llm2?: AgentLLMConfig;  // Second override LLM (used on second attempt if tempLLM1 fails)
  
  // Fallback behavior when validation fails
  fallback_to_original_llm_on_failure?: boolean;  // If true, use original LLM instead of temp override when validation fails
  
  // Learning behavior when tempLLM is active (per-model control)
  skip_learning_when_temp_llm1?: boolean;  // If true, skip learning phases when tempLLM1 is used (default: false, learning runs)
  skip_learning_when_temp_llm2?: boolean;  // If true, skip learning phases when tempLLM2 is used (default: false, learning runs)
  
  // Temporary LLM for learning agents (optional, used when learnings already exist for a step)
  // If learnings exist for a step_id, use temp_learning_llm if configured
  // If no learnings exist (new learning), always use default LLM (step config → preset)
  temp_learning_llm?: AgentLLMConfig;
  
  // Validation response persistence
  save_validation_responses?: boolean;  // If true, save validation responses to workspace validation folder (default: true)

  // Tool access control (global configuration)

  // Variable group execution options (for batch execution with multiple groups)
  enabled_group_ids?: string[];  // Group IDs to execute (if empty, uses groups' enabled flags)

  // Feature toggles (runtime configuration)
  enable_knowledgebase?: boolean;  // Enable knowledgebase (default: true)
  enable_context_summarization?: boolean;  // Enable context summarization (default: true)

  // Cleanup control
  skip_execution_cleanup?: boolean;  // If true, skip deleting execution folders before running steps
}

// Execution strategy constants (matching backend)
export const ExecutionStrategy = {
  // Fresh start strategies
  START_FROM_BEGINNING: 'start_from_beginning',
  FAST_EXECUTE_ALL: 'fast_execute_all',
  START_FROM_BEGINNING_NO_HUMAN: 'start_from_beginning_no_human',
  // Resume strategies
  RESUME_FROM_STEP: 'resume_from_step',
  FAST_RESUME_FROM_STEP: 'fast_resume_from_step',
  RESUME_FROM_STEP_NO_HUMAN: 'resume_from_step_no_human',
  FAST_EXECUTE_RANGE: 'fast_execute_range',
  // Single step execution
  RUN_SINGLE_STEP: 'run_single_step',
} as const;

// Execution strategies
export type ExecutionStrategyType = typeof ExecutionStrategy[keyof typeof ExecutionStrategy];

// Evaluation types
export interface EvaluationStep {
  id: string
  title: string
  description: string
  pre_validation?: ValidationSchema
  success_criteria: string
  agent_configs?: AgentConfigs
}

export interface EvaluationPlan {
  steps: EvaluationStep[]
}

export interface EvaluationStepConfig {
  id: string
  agent_configs: AgentConfigs
}

// Variable Groups API types
export interface Variable {
  name: string;
  value?: string;  // Used in single-group mode
  description: string;
}

export interface VariableGroup {
  group_id: string;  // e.g., "group-1", "group-2" (used as fallback for folder names)
  display_name?: string;  // Optional user-friendly name (e.g., "Production", "Staging")
  values: Record<string, string>;  // Variable name -> value mapping
  enabled: boolean;
}

export interface VariablesManifest {
  objective: string;  // Templated objective with {{VARS}}
  variables: Variable[];  // Variable definitions
  groups?: VariableGroup[];  // Array of variable groups (multi-group mode)
  extraction_date: string;
}

export interface VariableGroupsResponse {
  success: boolean;
  manifest?: VariablesManifest;
  error?: string;
}

// Execution Logs API types
export interface ValidationLog {
  attempt: number;
  file_path: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  content: any; // Full JSON content of validation log
}

export interface ExecutionAttemptLog {
  attempt: number;
  iteration: number;
  file_path: string;
  conversation_path: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  content?: any; // Full JSON content of execution result
}

export interface DecisionLog {
  decision_result: boolean;
  decision_reasoning: string;
  timestamp: string;
  execution_result?: string;
}

export interface OrchestrationLog {
  type: string;
  timestamp: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  orchestration_response?: any;
  selected_route_id?: string;
  success_criteria_met?: boolean;
}

export interface TodoTaskLog {
  type: string;  // 'routing', 'evaluation'
  timestamp: string;
  iteration?: number;
  model?: string;
  todo_task_response?: {
    next_action?: string;  // 'delegate', 'complete', 'continue'
    selected_route_id?: string;
    selected_route_name?: string;
    use_generic_agent?: boolean;
    todo_id_to_execute?: string;
    todo_title?: string;
    instructions_to_sub_agent?: string;
    success_criteria_for_sub_agent?: string;
    selection_reasoning?: string;
    all_tasks_complete?: boolean;
    progress_summary?: string;
  };
  all_tasks_complete?: boolean;
}

export interface ConditionalLog {
  condition_result: boolean;
  condition_reason: string;
  condition_question: string;
  timestamp: string;
  branch_executed: string;
}

export interface LearningLog {
  type: string;
  step_path: string;
  learning_type: string;
  learning_path_id?: string;
  detail_level?: string;
  result?: string;
  conversation_path?: string;
  error?: string;
  timestamp: string;
}

// Archived logs from previous runs (when resuming from a step)
export interface ArchivedLogEntry {
  timestamp: string;  // Archive timestamp (e.g., "20260106-115300")
  validations: ValidationLog[];
  executions: ExecutionAttemptLog[];
  decisions?: DecisionLog[];
  orchestration?: OrchestrationLog[];
  todo_task?: TodoTaskLog[];
  conditionals?: ConditionalLog[];
  learnings?: LearningLog[];
}

export interface StepOutputContent {
  file_path: string;
  content: unknown;
  is_json: boolean;
}

// Archived execution outputs from decision step routing (when step routes back to earlier step)
export interface ArchivedExecutionEntry {
  run_number: string;
  artifacts?: { file_name: string; file_path: string }[];
  output_content?: StepOutputContent;
}

export interface StepExecutionLogs {
  step_id: string;
  original_id?: string;
  type: string;
  title: string;
  description: string;
  success_criteria?: string;
  context_output?: string;  // Expected output filename
  is_completed?: boolean;   // Explicit completion marker
  output_content?: StepOutputContent;  // Actual output file content
  artifacts?: { file_name: string; file_path: string }[]; // Other output files
  validations: ValidationLog[];
  executions: ExecutionAttemptLog[];
  decisions?: DecisionLog[];
  orchestration?: OrchestrationLog[];
  todo_task?: TodoTaskLog[];
  conditionals?: ConditionalLog[];
  learnings?: LearningLog[];
  archived_logs?: ArchivedLogEntry[];  // Logs from previous runs
  archived_executions?: ArchivedExecutionEntry[];  // Archived execution outputs from decision step routing
}

export interface ModelTokenUsage {
  provider: string;
  input_tokens: number;
  output_tokens: number;
  input_tokens_m: string;
  output_tokens_m: string;
  cache_tokens: number;
  cache_tokens_m: string;
  cache_read_tokens?: number;
  cache_read_tokens_m?: string;
  cache_write_tokens?: number;
  cache_write_tokens_m?: string;
  reasoning_tokens: number;
  reasoning_tokens_m: string;
  llm_call_count: number;
  input_cost_usd?: number;
  output_cost_usd?: number;
  reasoning_cost_usd?: number;
  cache_cost_usd?: number;
  cache_read_cost_usd?: number;
  cache_write_cost_usd?: number;
  total_cost_usd?: number;
  context_window_usage?: number;
  model_context_window?: number;
  context_usage_percent?: number;
}

export interface TokenUsageFile {
  created_at: string;
  updated_at: string;
  by_model: Record<string, ModelTokenUsage>;
  by_step_and_model?: Record<string, Record<string, ModelTokenUsage>>;
}

export interface ExecutionLogsResponse {
  success: boolean;
  steps: Record<string, StepExecutionLogs>; // key is step ID or name (e.g. "step-1")
  token_usage?: TokenUsageFile;
}


// Batch execution progress for multiple variable groups
export interface BatchExecutionProgress {
  total_groups: number;
  enabled_groups: string[];  // Group IDs to execute
  completed_groups: string[];  // Group IDs that finished
  current_group: string;  // Currently executing group ID
  group_progress: Record<string, StepProgress>;  // Per-group step progress
  iteration_number: number;
}

// Batch execution event types
export interface BatchExecutionStartEvent {
  total_groups: number;
  enabled_group_ids: string[];
  iteration_number: number;
  workspace_path: string;
}

export interface BatchGroupStartEvent {
  group_id: string;
  group_index: number;
  total_groups: number;
  variable_values: Record<string, string>;
  run_folder: string;
  iteration_number: number;
  workspace_path: string;
}

export interface BatchGroupEndEvent {
  group_id: string;
  group_index: number;
  total_groups: number;
  success: boolean;
  error?: string;
  duration: number;  // Duration in nanoseconds
  completed_steps: number;
  total_steps: number;
  run_folder: string;
  remaining_groups: number;
}

export interface BatchExecutionEndEvent {
  total_groups: number;
  completed_groups: number;
  failed_groups: number;
  canceled_groups: number;
  duration: number;  // Duration in nanoseconds
  success: boolean;
  error?: string;
  iteration_number: number;
  completed_group_ids: string[];
  failed_group_ids: string[];
}

export interface BatchExecutionCanceledEvent {
  total_groups: number;
  completed_groups: number;
  canceled_group_id: string;
  remaining_group_ids: string[];
  reason: string;
}

// Evaluation Report types
export interface EvaluationStepScore {
  step_id: string;
  step_title: string;
  score: number;
  max_score: number;
  reasoning: string;
  evidence: string;
  success_criteria: string;
  context_output?: string;
  output_content?: StepOutputContent;
}

export interface EvaluationReport {
  target_run_folder: string;
  generated_at: string;
  total_score: number;
  max_possible_score: number;
  score_percentage: number;
  step_scores: EvaluationStepScore[];
  summary: string;
}

// Evaluation reports response for aggregate view
export interface EvaluationReportsResponse {
  success: boolean;
  reports: EvaluationReportEntry[];
  aggregate?: EvaluationAggregate;
  evaluation_plan?: string;
  error?: string;
}

export interface EvaluationReportEntry {
  run_folder: string;
  report: EvaluationReport;
}

export interface EvaluationAggregate {
  total_runs: number;
  average_score: number;
  average_percentage: number;
  highest_score: number;
  lowest_score: number;
  max_possible_score: number;
}

// Consolidated workspace state (NEW - single API call for all workspace data)
export interface WorkspaceStateResponse {
  success: boolean;
  data?: WorkspaceState;
  error?: string;
}

export interface WorkspaceState {
  run_folders: RunFolderInfo[];
  selected_progress?: StepProgress;
  variables_manifest?: VariablesManifest;
  phases: WorkflowPhase[];
}

export interface RunFolderInfo {
  name: string;
  progress?: StepProgress;
}

export interface StepProgress {
  completed_step_indices: number[];
  total_steps: number;
  last_updated: string;
  branch_steps?: Record<number, BranchStepProgress>;
}

export interface BranchStepProgress {
  branch_executed: string;
  completed_steps: string[];
}

export interface WorkflowPhase {
  id: string;
  title: string;
  description: string;
  options?: WorkflowPhaseOption[];
}

// ============================================================================
// Chat Cost Analysis Types
// ============================================================================

export interface ChatModelUsage {
  provider: string;
  input_tokens: number;
  output_tokens: number;
  reasoning_tokens: number;
  cache_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  llm_call_count: number;
  input_cost_usd: number;
  output_cost_usd: number;
  reasoning_cost_usd: number;
  cache_cost_usd: number;
  total_cost_usd: number;
  context_window_usage: number;
  model_context_window: number;
  context_usage_percent: number;
}

export interface SessionCostSummary {
  session_id: string;
  title: string;
  agent_mode: string;
  created_at: string;
  status: string;
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_llm_calls: number;
  by_model: Record<string, ChatModelUsage>;
  by_agent?: Record<string, ChatModelUsage>;
}

export interface AggregateCosts {
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_llm_calls: number;
  total_sessions: number;
  by_model: Record<string, ChatModelUsage>;
  by_agent?: Record<string, ChatModelUsage>;
}

export interface UserCostsResponse {
  sessions: SessionCostSummary[];
  aggregate: AggregateCosts;
}

export interface SessionCostDetail {
  session_id: string;
  title: string;
  created_at: string;
  by_model: Record<string, ChatModelUsage>;
  by_turn_and_model?: Record<string, Record<string, ChatModelUsage>>;
  by_agent_and_model?: Record<string, Record<string, ChatModelUsage>>;
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_llm_calls: number;
}

// ============================================================================
// Delegation Logs Types (Multi-Agent Mode)
// ============================================================================

export interface DelegationLogEntry {
  delegation_id: string;
  session_id?: string;
  instruction: string;
  reasoning_level?: string;
  model_id?: string;
  tool_mode?: string;
  servers?: string[];
  background_agent_id?: string;
  depth: number;
  status: 'running' | 'completed' | 'failed';
  start_time: string;
  end_time?: string;
  duration?: string;
  result?: string;
  error?: string;
  input_tokens: number;
  output_tokens: number;
  tool_calls: number;
  token_usage?: Record<string, ChatModelUsage>;
  total_cost_usd: number;
}

export interface DelegationLogsResponse {
  delegations: DelegationLogEntry[];
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_llm_calls: number;
  by_model: Record<string, ChatModelUsage>;
}

export interface AgentCostSummary {
  name: string;
  input_tokens: number;
  output_tokens: number;
  total_cost_usd: number;
  llm_calls: number;
  by_model: Record<string, ChatModelUsage>;
}

export interface SessionDelegationLogs {
  session_id: string;
  title: string;
  created_at: string;
  status: string;
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_llm_calls: number;
  main_agent: AgentCostSummary;
  delegations: DelegationLogEntry[];
  by_model: Record<string, ChatModelUsage>;
}

export interface AllDelegationLogsResponse {
  sessions: SessionDelegationLogs[];
  total_cost_usd: number;
  total_input_tokens: number;
  total_output_tokens: number;
  total_llm_calls: number;
  by_model: Record<string, ChatModelUsage>;
}

// ============================================================================
// Bot Simulator Types
// ============================================================================

export interface SimulatorMessageBlock {
  type: string;
  text?: string;
  buttons?: SimulatorMessageButton[];
}

export interface SimulatorMessageButton {
  text: string;
  value: string;
  style?: string;
  action_id: string;
}

export interface SimulatorMessage {
  id: string;
  text: string;
  blocks?: SimulatorMessageBlock[];
  is_bot: boolean;
  timestamp: string;
}

export interface SimulatorThreadInfo {
  thread_id: string
  preview: string
  created_at: string
  message_count: number
}

export interface SimulatorSendResponse {
  type: 'conversation' | 'follow_up';
  response?: string;          // text reply for conversation
  thread_id: string;
  session_id?: string;        // internal chat session ID (for follow_up)
  bot_session_id?: string;    // set when awaiting user confirmation
  thread_offset?: number;     // current thread message count (for polling init)
}

export interface SimulatorMessagesResponse {
  messages: SimulatorMessage[];
  total: number;
}

export interface SimulatorInteractResponse {
  success: boolean;
}

// Workflow Versions
export interface WorkflowVersionMeta {
  version: number
  label: string
  created_at: string
  files_count: number
}

// Scheduled Jobs
export interface ScheduledJob {
  id: string
  name: string
  description: string
  entity_type: 'workflow' | 'chat'
  preset_query_id: string
  trigger_payload?: Record<string, unknown>
  group_ids?: string[]  // undefined/empty = all groups
  cron_expression: string
  timezone: string
  enabled: boolean
  last_run_at?: string
  next_run_at?: string
  last_session_id?: string
  last_status?: 'success' | 'error' | 'running'
  last_error?: string
  last_duration_ms?: number
  run_count: number
  consecutive_failures: number
  created_at: string
  updated_at: string
}

export interface CreateScheduledJobRequest {
  name: string
  description?: string
  entity_type: 'workflow' | 'chat'
  preset_query_id: string
  trigger_payload?: Record<string, unknown>
  group_ids?: string[]  // undefined/empty = all groups
  cron_expression: string
  timezone?: string
  enabled?: boolean
}

export interface UpdateScheduledJobRequest {
  name?: string
  description?: string
  trigger_payload?: Record<string, unknown>
  group_ids?: string[]       // undefined = don't change; [] = run all groups; [...] = specific groups
  set_group_ids?: boolean    // must be true to actually update group_ids
  cron_expression?: string
  timezone?: string
  enabled?: boolean
}

export interface ListScheduledJobsResponse {
  jobs: ScheduledJob[]
  total: number
  limit: number
  offset: number
}

export interface ScheduledJobRun {
  id: string
  job_id: string
  run_folder?: string
  session_id?: string
  status: 'running' | 'success' | 'error'
  error?: string
  duration_ms?: number
  group_ids?: string[]
  started_at: string
  completed_at?: string
}

export interface ListScheduledJobRunsResponse {
  runs: ScheduledJobRun[]
  total: number
  limit: number
  offset: number
}