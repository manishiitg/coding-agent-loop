// API-specific types (separate from event types)
import type { PollingEventSchema } from '../generated/event-types'
import type { EventTypeString } from '../generated/event-types'
import type { ValidationSchema, AgentConfigs } from '../utils/stepConfigMatching'

export type LLMProvider =
  | 'openrouter'
  | 'bedrock'
  | 'openai'
  | 'vertex'
  | 'anthropic'
  | 'azure'
  | 'z-ai'
  | 'kimi'
  | 'claude-code'
  | 'gemini-cli'
  | 'codex-cli'
  | 'cursor-cli'
  | 'agy-cli'
  | 'pi-cli'
  | 'minimax'
  | 'minimax-coding-plan'
  | 'elevenlabs'
  | 'deepgram'

// New LLM Configuration types (Tiered Fallback System)
export interface LLMModel {
  provider: LLMProvider
  model_id: string

  // Auth per model (each model carries its own credentials)
  api_key?: string      // For OpenAI, Anthropic, Vertex, Azure, and API-key based providers
  region?: string       // For Bedrock, Azure
  endpoint?: string     // For Azure (endpoint URL)

  // Model-specific options (reasoning_effort, thinking_level, thinking_budget, etc.)
  options?: Record<string, unknown>
}

// Saved/Published LLM Configuration (User library)
export interface SavedLLM extends LLMModel {
  id: string
  name: string
  model_name?: string // Display name from metadata (e.g., "Claude 3.5 Sonnet")
  auth_method?: 'api_key' | 'oauth' | 'none' // Auth method used
  context_window?: number
  input_cost_per_1m?: number
  output_cost_per_1m?: number
  reasoning_cost_per_1m?: number
  cached_input_cost_per_1m?: number
  cached_input_cost_write_per_1m?: number
  source?: 'auto_coding_agent' | string
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
  published_llm_id?: string
  provider: LLMProvider
  model_id: string
  options?: Record<string, unknown>
  fallback_models: string[]
  cross_provider_fallback?: {
    provider: 'openai' | 'bedrock' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'gemini-cli' | 'codex-cli' | 'cursor-cli' | 'agy-cli' | 'pi-cli'
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
    kimi?: string
    pi_cli?: string
    minimax?: string
    minimax_coding_plan?: string
    elevenlabs?: string
    deepgram?: string
    pi_provider_keys?: Record<string, string>
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
}

// Execution mode constants matching backend enum

// Agent streaming types
export interface AgentQueryRequest {
  query: string
  provider?: LLMProvider
  model_id?: string
  temperature?: number
  max_turns?: number
  enabled_tools?: string[]
  enabled_servers?: string[]
  selected_tools?: string[] // Array of "server:tool" strings
  // 'simple' is accepted as a legacy alias of 'multi-agent' on the
  // backend; new clients should send 'multi-agent'.
  agent_mode?: 'multi-agent' | 'simple' | 'workflow' | 'workflow_phase'
  // Workflow phase chat: specifies which phase to run as a chat session
  phase_id?: string
  // Support both legacy and new config format
  llm_config?: LLMConfiguration | AgentLLMConfiguration
  preset_query_id?: string
  // Code execution mode: When enabled, only virtual tools are added to LLM
  // MCP tools are accessed via generated Go code using discover_code_files and write_code
  use_code_execution_mode?: boolean
  // Execution options from frontend (for workflow execution phase)
  execution_options?: ExecutionOptions
  // Context summarization configuration
  enable_context_summarization?: boolean // Enable context summarization feature
  summarize_on_max_turns?: boolean // Automatically summarize when max turns is reached
  summary_keep_last_messages?: number // Number of recent messages to keep when summarizing (default: 8)
  // Browser automation access configuration
  enable_browser_access?: boolean // Enable/disable browser automation tool (auto-enables workspace when true)
  // Explicit browser mode for prompt/runtime selection
  browser_mode?: 'none' | 'headless' | 'cdp' | 'playwright'
  // CDP port for connecting to an existing Chrome browser (local mode only)
  cdp_port?: number
  // Context editing configuration
  enable_context_editing?: boolean // Enable context editing (dynamic context reduction)
  // Selected skills to include in the chat context
  selected_skills?: string[] // Array of skill folder names
  // Delegation tier configuration: Maps reasoning levels to specific provider/model pairs
  delegation_tier_config?: DelegationTierConfig
  // Decrypted secrets to pass to backend (injected into agent system prompt, never in query text)
  decrypted_secrets?: Array<{ name: string; value: string }>
  // Selected global secret names to include (if omitted, all global secrets are included)
  selected_global_secrets?: string[]
  // Workspace paths of workflows to inject context for (via # selector in chat)
  workflow_context_paths?: string[]
  // Conversation JSON selected from /resume or a previous chat panel. The backend
  // can use its runtime metadata for native coding-agent resume.
  restored_conversation_path?: string
  // Image generation configuration
  enable_image_generation?: boolean
  image_gen_config?: {
    provider: string
    model_id: string
    api_key?: string
  }
  // Auto-notification flag: when true, this is a background agent completion notification,
  // not a user-initiated message. Backend treats it as a synthetic turn (doesn't block user input).
  is_auto_notification?: boolean
}

// Delegation tier configuration for multi-LLM support
export interface DelegationTierConfig {
  main?: TierModel    // orchestrator/main agent model
  chief_of_staff?: TierModel // scheduled Chief of Staff / Org Pulse model
  high?: TierModel
  medium?: TierModel
  low?: TierModel
  custom?: Record<string, CustomTierModel>  // slug → custom tier
}

export interface TierModel {
  provider: string
  model_id: string
  fallbacks?: AgentLLMFallback[]
}

export interface CustomTierModel {
  description: string // LLM guidance, e.g. "low cost model for code reviews"
  provider: string
  model_id: string
}

export interface AgentQueryResponse {
  query_id: string
  // 'started' | 'workflow_started' | 'live_input_delivered' | error states.
  // 'live_input_delivered' means the backend steered this message into an
  // already-running coding-agent turn instead of starting a new one (single-entry
  // routing for tmux-transport CLIs).
  status: string
  message?: string
  sse_endpoint?: string
  session_id?: string
  // Populated only when status === 'live_input_delivered'.
  delivery_status?: 'sent_to_cli' | 'queued_for_injection' | 'next_turn_started'
  provider?: string
}

// LLM Defaults Configuration Response
export interface LLMDefaultsResponse {
  primary_config: LLMConfiguration
  openrouter_config?: ExtendedLLMConfiguration
  bedrock_config: ExtendedLLMConfiguration
  openai_config: ExtendedLLMConfiguration
  vertex_config?: ExtendedLLMConfiguration
  anthropic_config?: ExtendedLLMConfiguration
  azure_config?: ExtendedLLMConfiguration
  zai_config?: ExtendedLLMConfiguration
  kimi_config?: ExtendedLLMConfiguration
  pi_cli_config?: ExtendedLLMConfiguration
  minimax_config?: ExtendedLLMConfiguration
  minimax_coding_plan_config?: ExtendedLLMConfiguration
  elevenlabs_config?: ExtendedLLMConfiguration
  deepgram_config?: ExtendedLLMConfiguration
  available_models: {
    bedrock: string[]
    openrouter?: string[]
    openai: string[]
    vertex?: string[]
    anthropic?: string[]
    azure?: string[]
    'z-ai'?: string[]
    kimi?: string[]
    'pi-cli'?: string[]
    minimax?: string[]
    'minimax-coding-plan'?: string[]
    elevenlabs?: string[]
    deepgram?: string[]
  }
  provider_capabilities?: Partial<Record<LLMProvider, string[]>>
  supported_providers?: LLMProvider[]
  /** When true, LLM config is locked by admin; do not show editable modal, use server env only */
  llm_config_locked?: boolean
  /** Default published LLMs from server (e.g. one "Gemini" entry); when locked, list is read-only */
  default_published_llms?: SavedLLM[]
  /** When true, default published LLMs list is locked (no add/delete/edit) */
  default_published_llms_locked?: boolean
  /** List of provider names that are locked (read-only) because they are fully configured via server env */
  locked_providers?: string[]
}

export interface LLMDiscoveryCandidate {
  id: string
  provider: LLMProvider
  model_id: string
  model_name?: string
  label: string
  kind: 'local_cli' | 'api'
  detection_source: string
  auth_source?: string
  auth_configured: boolean
  runtime_command?: string
  runtime_available?: boolean
  usable: boolean
  recommended: boolean
  reason: string
  setup_hint?: string
  deprecated?: boolean
  deprecation_reason?: string
  replacement_provider?: string
  options?: string[]
}

export interface LLMDiscoveryResponse {
  candidates: LLMDiscoveryCandidate[]
  notes: string[]
}

// API Key Validation Request/Response
export interface APIKeyValidationRequest {
  provider: LLMProvider
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

// ChannelRoute maps a Slack channel ID to a specific workflow, including the workspace path
// so the bot can read the workflow manifest (e.g. workshop_mode) without scanning all workspaces.
export interface ChannelRoute {
  workflow_id: string
  workspace_path: string
  // Override the manifest's workshop_mode for this channel. Empty = use manifest.
  workshop_mode?: 'workshop' | 'run'
  // Opt in to detailed workflow runtime messages in the bot channel.
  send_full_details?: boolean
}

export interface SlackConfig {
  enabled: boolean
  bot_token?: string  // Masked in GET response
  app_token?: string  // Masked in GET response (App-level token for Socket Mode)
  channel_id?: string
  bot_mode?: boolean  // Enable @mention bot mode
  channel_routing?: Record<string, ChannelRoute>  // Maps Slack channel IDs to ChannelRoute
}

export interface SlackConfigRequest {
  enabled: boolean
  bot_token: string  // Bot User OAuth Token (xoxb-...)
  app_token: string  // App-level token (xapp-...) for Socket Mode
  channel_id: string
  bot_mode: boolean  // Enable @mention bot mode
  channel_routing?: Record<string, ChannelRoute>  // Maps Slack channel IDs to ChannelRoute
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

// Gmail is an outbound-only notification channel backed server-side by the
// `gws` CLI. Auth is handled on the host (gws auth login / service account),
// so the UI only sets an enable toggle + a default recipient and reads
// auto-detected connection status.
export interface GmailAuthStatus {
  gws_installed: boolean
  authenticated: boolean
  has_gmail_scope: boolean
  scopes?: string[]
  detail?: string
}

export interface GmailConfigRequest {
  enabled: boolean
  default_to: string
  allowed_recipients?: string[]
}

export interface GmailConfigResponse {
  enabled: boolean
  default_to?: string
  allowed_recipients?: string[]
  auth: GmailAuthStatus
  ready: boolean  // enabled + recipient + authenticated + gmail scope
}

export interface GmailTestResponse {
  success: boolean
  message: string
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
  execution_id?: string
  parent_execution_id?: string
  execution_kind?: string
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
  is_synthetic_turn?: boolean // True when running auto-notification turn (input remains locked as normal)
  can_steer?: boolean // True when a live foreground agent can accept steer injection
}

export interface TerminalSnapshot {
  terminal_id: string
  session_id: string
  owner_id?: string
  execution_id?: string
  execution_kind?: string
  label?: string
  scope?: string
  workflow_path?: string
  workflow_name?: string
  workflow_label?: string
  step_id?: string
  step_name?: string
  step_type?: string
  agent_name?: string
  display_title?: string
  display_meta?: string
  tmux_session?: string
  content_source?: 'tmux_pipe' | 'tmux_capture' | 'event_stream' | string
  // Rich step context — populated by the orchestrator's bridge for
  // workflow-step terminals. Used to render the transport-class chip
  // and the "step 3/7 · attempt 1 · triggered by X" meta row.
  step_index?: number
  step_total?: number
  parent_step_id?: string
  step_attempt?: number
  step_execution_mode?: string
  step_transport?: string
  step_triggered_by?: string
  content: string
  rows: TerminalSnapshotRow[]
  chunk_index: number
  active: boolean
  state?: 'running' | 'completed' | 'failed' | 'idle' | 'closing' | 'stale' | string
  closes_at?: string
  retention_seconds?: number
  status: TerminalStatus
  created_at: string
  updated_at: string
}

export interface TerminalSnapshotRow {
  kind: string
  text?: string
  name?: string
  args?: string
  result?: string
  result_prefix?: '✓' | '✗' | string
}

export interface TerminalStatus {
  provider_label?: string
  status_text?: string
  assistant_preview?: string
  tool_summary?: string
  tool_name?: string
  tool_count?: number
  input_tokens?: number
  output_tokens?: number
  cache_creation_input_tokens?: number
  cache_read_input_tokens?: number
  total_input_tokens?: number
  total_output_tokens?: number
  cost_usd?: number
  // Raw provider statusline extras with no first-class field (context window,
  // git branch, rate limits, …). Carried through so nothing is dropped.
  status_meta?: Record<string, unknown>
  duration_ms?: number
  pre_validation_status?: 'passed' | 'failed' | string
  pre_validation_summary?: string
  pre_validation_passed_checks?: number
  pre_validation_failed_checks?: number
  pre_validation_total_checks?: number
}

export interface ListTerminalsResponse {
  terminals: TerminalSnapshot[]
  total: number
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
  title?: string
  workflow_name?: string
  workflow_label?: string
  workspace_path?: string
  preset_name?: string
  preset_query_id?: string
  bot_platform?: string
  triggered_by?: string
  has_running_background_agents?: boolean
  running_background_agent_count?: number
  has_retained_tmux_session?: boolean
  current_execution_name?: string
  needs_user_input?: boolean
  waiting_event_type?: string
  waiting_message?: string
  waiting_since?: string
  runtime?: ChatHistoryAgentRuntime
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
  can_steer?: boolean
  has_retained_tmux_session?: boolean
}

export interface SessionExecutionTreeNode {
  execution_id: string
  parent_execution_id?: string
  session_id: string
  source?: string
  kind: string
  name: string
  status: string
  started_at: string
  completed_at?: string
  error?: string
  metadata?: Record<string, string>
  children?: SessionExecutionTreeNode[]
}

export interface SessionExecutionTreeSummary {
  session_id: string
  session_status: string
  display_status: 'busy' | 'idle' | 'stopped'
  is_session_busy: boolean
  running_count: number
  completed_count: number
  failed_count: number
  canceled_count: number
  has_running_main_agent: boolean
  has_running_background_agents: boolean
  has_running_tracked_executions: boolean
}

export interface SessionExecutionTreeResponse {
  session_id: string
  root: SessionExecutionTreeNode
  summary: SessionExecutionTreeSummary
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
  content?: string;
  last_modified?: string;
  folder?: string;
  type?: 'folder' | 'file';
  children?: PlannerFile[];
  depth?: number;
  is_image?: boolean;
  is_binary?: boolean;
  size?: number;
  mime_type?: string;
  encoding?: string;
  // Store original path when filepath is adjusted for display (e.g., in workflow mode)
  originalFilepath?: string;
}

export interface PlannerFileContent {
  filepath: string;
  content: string;
  last_modified?: string;
  folder?: string;
  is_image?: boolean;
  is_binary?: boolean;
  size?: number;
  mime_type?: string;
  encoding?: string;
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
  current_step_id?: string;     // Currently executing step ID
  current_step_title?: string;  // Currently executing step title
  last_polled?: number;         // Unix timestamp (ms) of last status check
}

// Chat Session Configuration
export interface ChatSessionConfig {
  selected_servers?: string[];
  enabled_servers?: string[];
  use_code_execution_mode?: boolean;
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
  workflow_metadata?: WorkflowMetadata; // Workflow-specific metadata (for background workflows)
  selected_skills?: string[]; // Selected skill folder names
  delegation_tier_config?: DelegationTierConfig; // Delegation tier model config
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

export interface ChatHistoryMessagePart {
  Text?: string;
  text?: string;
  Type?: string;
  type?: string;
  Content?: string;
  content?: string;
}

export interface ChatHistoryMessage {
  Role?: string;
  role?: string;
  Parts?: ChatHistoryMessagePart[];
  parts?: ChatHistoryMessagePart[];
}

export interface ChatHistoryConversation {
  session_id: string;
  agent_mode?: string;
  runtime?: ChatHistoryAgentRuntime;
  workshop_mode?: 'workshop' | 'run' | string;
  conversation_history: ChatHistoryMessage[];
  terminal_snapshots?: TerminalSnapshot[];
  ui_events?: PollingEventSchema[];
  updated_at?: string;
}

export interface ChatHistoryAgentRuntime {
  kind?: string;
  provider?: string;
  model_id?: string;
  transport?: string;
  external_session_id?: string;
  resume_supported: boolean;
  resume_flag?: string;
  project_dir_id?: string;
  workspace_path?: string;
  workshop_mode?: 'workshop' | 'run' | string;
  captured_at?: string;
  agent_session_handle?: ChatHistoryAgentSessionHandle;
}

export interface ChatHistoryAgentSessionHandle {
  agent_id?: string;
  session_id?: string;
  owner_id?: string;
  scope?: string;
  correlation_id?: string;
  provider?: ChatHistoryCodingProviderSessionHandle;
}

export interface ChatHistoryCodingProviderSessionHandle {
  provider?: string;
  transport?: string;
  native_session_id?: string;
  tmux_session?: string;
  working_dir?: string;
  project_dir_id?: string;
  model?: string;
  status?: string;
}

export interface StartRestoredTerminalRequest {
  session_id: string;
  restored_conversation_path?: string;
  restored_conversation_session_id?: string;
  workspace_path?: string;
}

export interface StartRestoredTerminalResponse {
  ok: boolean;
  started: boolean;
  reason?: string;
  terminal?: TerminalSnapshot;
}

export interface ChatHistoryPreviewMessage {
  role: string;
  text: string;
}

export interface ChatHistorySession {
  session_id: string;
  agent_mode?: string;
  runtime?: ChatHistoryAgentRuntime;
  workshop_mode?: 'workshop' | 'run' | string;
  status?: string;
  query?: string;
  user_id?: string;
  workspace_path?: string;
  conversation_path?: string;
  created_at?: string;
  updated_at?: string;
  message_count?: number;
  preview_messages?: ChatHistoryPreviewMessage[];
}

export interface ChatHistoryCleanupResult {
  deleted_count: number;
  deleted_paths: string[];
  cutoff: string;
  scope: string;
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

export interface WorkflowBuilderSessionResponse {
  success: boolean;
  source: 'live' | 'workspace' | 'none';
  session_id?: string;
  phase_id?: string;
  status: 'running' | 'completed' | 'idle' | 'error' | 'stopped' | string;
  display_status?: string;
  preset_query_id?: string;
  workspace_path?: string;
  workflow_name?: string;
  updated_at?: string;
  conversation_path?: string;
  events: PollingEvent[];
  total?: number;
  last_processed_index?: number;
}

// SSE message types (match backend sseEventMessage / sseStatusMessage)
export interface SSEEventMessage {
  events: PollingEvent[]
  session_status?: string
  last_processed_index: number
  has_running_background_agents?: boolean
  is_synthetic_turn?: boolean
  can_steer?: boolean
}

export interface SSEStatusMessage {
  session_status?: string
  has_running_background_agents?: boolean
  is_synthetic_turn?: boolean
  can_steer?: boolean
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

// Workflow running-session types — mirror ActiveWorkflowExecution in
// agent_go/cmd/server/workflow.go.
export interface RunningWorkflowInfo {
  query_id: string;
  session_id: string;
  preset_query_id?: string;
  preset_name?: string;
  workspace_path: string;
  run_folder?: string;
  phase_id?: string;
  phase_name?: string;
  status?: string;
  user_id?: string;
  title?: string;
  query?: string;
  triggered_by: string;
  started_at: string;
  is_minimized?: boolean;
  minimized_at?: number;
  current_step_id?: string;
  current_step_title?: string;
  needs_user_input?: boolean;
  waiting_message?: string;
  waiting_since?: string;
}

export interface UpdateRunningWorkflowRequest {
  status?: string;
  phase_id?: string;
  phase_name?: string;
  is_minimized?: boolean;
  minimized_at?: number;
  current_step_id?: string;
  current_step_title?: string;
}

// Global cost ledger summary — mirror of pkg/costledger.Summary.
export interface CostAggregate {
  prompt_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  total_cost_usd: number
  call_count: number
}

// CostDateAggregate is one row in the per-date rollup. It inherits all
// CostAggregate fields (so existing per-date totals still read flat),
// and adds an optional per-model breakdown for that date so clients
// can expand a row to see which models contributed.
export interface CostDateAggregate extends CostAggregate {
  by_model?: Record<string, CostAggregate>
}

export interface CostSummary {
  from?: string
  to?: string
  total: CostAggregate
  by_date: Record<string, CostDateAggregate>
  by_model: Record<string, CostAggregate>
}

// Preset LLM Configuration types
export interface AgentLLMFallback {
  published_llm_id?: string
  provider: string
  model_id: string
  options?: Record<string, unknown>
}

export interface AgentLLMConfig {
  published_llm_id?: string
  provider: LLMProvider
  model_id: string
  options?: Record<string, unknown>
  fallbacks?: AgentLLMFallback[]
}

export interface PresetLLMConfig {
  published_llm_id?: string

  // Legacy: Single default model (for backward compatibility)
  provider?: LLMProvider
  model_id?: string
  options?: Record<string, unknown>

  // Agent-specific defaults.
  phase_llm?: AgentLLMConfig            // Default for all phase agents (planning, anonymization, plan improvement, etc.)
  auto_improve_llm?: AgentLLMConfig     // Optional scheduled Auto Improve override
  pulse_llm?: AgentLLMConfig            // Optional scheduled Pulse override
  chief_of_staff_llm?: AgentLLMConfig   // Optional scheduled Chief of Staff override

  // Feature toggles
  use_knowledgebase?: boolean           // nil/true = enabled (default), false = disabled
  enable_context_summarization?: boolean // nil/true = enabled (default), false = disabled
  enable_context_editing?: boolean       // nil/false = disabled (default), true = enabled

  // Tiered LLM allocation mode
  llm_allocation_mode?: 'manual' | 'tiered' | 'coding_agent' | 'coding_plan'
  tiered_config?: {
    tier_1: AgentLLMConfig
    tier_2: AgentLLMConfig
    tier_3: AgentLLMConfig
  }
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
  workspace: Record<string, never>;
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
// RunFolderInfo is defined below (near WorkspaceState) with metadata field

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
  plan_change_action?: 'keep_old_progress' | 'delete_old_progress';
  
  // Validation response persistence
  save_validation_responses?: boolean;  // If true, save validation responses to workspace validation folder (default: true)

  // Tool access control (global configuration)

  // Variable group execution options (for batch execution with multiple groups)
  enabled_group_names?: string[];  // Group names to execute (if empty, uses groups' enabled flags)

  // Feature toggles (runtime configuration)
  enable_knowledgebase?: boolean;  // Enable knowledgebase (default: true)
  enable_context_summarization?: boolean;  // Enable context summarization (default: true)

  // Workshop mode override. Reporting remains accepted for backend compatibility,
  // but the visible UI maps report authoring to builder.
  workshop_mode?: 'workshop' | 'run';
}

// Execution strategy constants (matching backend)
// All strategies use learning enabled, no human feedback.
export const ExecutionStrategy = {
  // Fresh start
  START_FROM_BEGINNING_NO_HUMAN: 'start_from_beginning_no_human',
  // Resume
  RESUME_FROM_STEP_NO_HUMAN: 'resume_from_step_no_human',
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
  applies_to_routes?: Array<{
    routing_step_id: string
    route_ids: string[]
  }>
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
  name: string;  // Unique identifier and display label
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
  timing_path?: string;
  // Optional parsed execution-attempt timing sidecar, or null when unavailable.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  timing?: any;
  // True when this entry came from a learn-code fast-path run (saved main.py
  // executed directly, no LLM involved). attempt=0 + fast_path=true signal this.
  // Content shape then follows ScriptedFastPathLog (success/exit_code/output/...).
  fast_path?: boolean;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  content?: any; // Full JSON content of execution result
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

// Archived execution outputs (when step routes back to earlier step)
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
  learning_objective?: string;
  learnings_access?: string;
  knowledgebase_access?: string;
  knowledgebase_write_method?: string;
  knowledgebase_contribution?: string;
  output_content?: StepOutputContent;  // Actual output file content
  artifacts?: { file_name: string; file_path: string }[]; // Other output files
  validations: ValidationLog[];
  executions: ExecutionAttemptLog[];
  orchestration?: OrchestrationLog[];
  todo_task?: TodoTaskLog[];
  conditionals?: ConditionalLog[];
  learnings?: LearningLog[];
  archived_logs?: ArchivedLogEntry[];  // Logs from previous runs
  archived_executions?: ArchivedExecutionEntry[];  // Archived execution outputs from previous routing
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

export interface ToolCostUsage {
  tool_name: string;
  capability?: string;
  provider?: string;
  model_id?: string;
  unit?: string;
  quantity?: number;
  count?: number;
  total_cost_usd?: number;
  estimated?: boolean;
  output_paths?: string[];
  metadata?: Record<string, unknown>;
  created_at?: string;
  updated_at?: string;
}

export interface TokenUsageFile {
  created_at: string;
  updated_at: string;
  by_model: Record<string, ModelTokenUsage>;
  by_step_and_model?: Record<string, Record<string, ModelTokenUsage>>;
  by_tool?: Record<string, ToolCostUsage>;
  by_step_and_tool?: Record<string, Record<string, ToolCostUsage>>;
}

export interface PhaseTokenUsageFile {
  created_at: string;
  updated_at: string;
  by_model: Record<string, ModelTokenUsage>;
  by_phase_and_model?: Record<string, Record<string, ModelTokenUsage>>;
}

export interface WorkflowRunCostsEntry {
  run_folder: string;
  token_usage?: TokenUsageFile;
  evaluation_token_usage?: TokenUsageFile;
}

export interface WorkflowPhaseDailyCostsEntry {
  date: string;
  token_usage?: PhaseTokenUsageFile;
}

export interface WorkflowRunDailyCostsEntry {
  date: string;
  scope: 'execution' | 'evaluation' | string;
  group_folder: string;
  run_folder: string;
  token_usage?: TokenUsageFile;
}

export interface WorkflowCostsResponse {
  success: boolean;
  phase_token_usage?: PhaseTokenUsageFile;
  phase_daily_costs: WorkflowPhaseDailyCostsEntry[];
  run_daily_costs?: WorkflowRunDailyCostsEntry[];
  runs: WorkflowRunCostsEntry[];
}

export interface ExecutionLogsResponse {
  success: boolean;
  steps: Record<string, StepExecutionLogs>; // key is step ID or name (e.g. "step-1")
  token_usage?: TokenUsageFile;
}


// Batch execution event types
export interface BatchExecutionStartEvent {
  total_groups: number;
  enabled_group_names: string[];
  iteration_number: number;
  workspace_path: string;
}

export interface BatchGroupStartEvent {
  group_name: string;
  group_index: number;
  total_groups: number;
  variable_values: Record<string, string>;
  run_folder: string;
  iteration_number: number;
  workspace_path: string;
}

export interface BatchGroupEndEvent {
  group_name: string;
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
  completed_group_names: string[];
  failed_group_names: string[];
}

export interface BatchExecutionCanceledEvent {
  total_groups: number;
  completed_groups: number;
  canceled_group_name: string;
  remaining_group_names: string[];
  reason: string;
}

// Evaluation Report types
// step_title and success_criteria are intentionally absent — UI consumers look
// them up by step_id from the evaluation_plan returned alongside reports.
// summary is also absent — per-step reasoning + evidence is the entire output.
export interface EvaluationStepScore {
  step_id: string;
  score?: number;
  max_score?: number;
  reasoning?: string | null;
  evidence?: string | null;
  skipped?: boolean;
  context_output?: string | null;
  output_content?: StepOutputContent | null;
}

export interface EvaluationReport {
  target_run_folder: string;
  generated_at: string;
  step_scores?: EvaluationStepScore[] | null;
}

// Evaluation reports response for aggregate view
export interface EvaluationReportsResponse {
  success: boolean;
  reports: EvaluationReportEntry[];
  aggregate?: EvaluationAggregate;
  evaluation_plan?: string;
  error?: string;
}

export interface WorkflowReviewDataResponse {
  success: boolean;
  costs: WorkflowCostsResponse;
  evaluations: EvaluationReportsResponse;
}

export interface EvaluationReportEntry {
  run_folder: string;
  report: EvaluationReport | null;
}

export interface EvaluationAggregate {
  total_runs: number;
}

// ---------------------------------------------------------------------------
// Dynamic report system (docs/workflow/persistent_stores_design.md section 2)
// Replaces the static final_output.go agent. The report is a live frontend
// view over db/, defined by reports/report_plan.json.
//
// The Go reportPlanDocument types in agent_go are the canonical source of
// truth — the validator runs in the builder loop. The interfaces below are
// re-exports of the auto-generated types under their public names, narrowed
// where the parser provides stronger guarantees than the schema alone (e.g.
// source/path always set post-parse, entries always discriminated).
//
// To add or rename a field: edit the Go struct, then run
// `cd frontend && npm run types:generate`. The pre-commit drift check fails
// commits that forget the regen step.
// ---------------------------------------------------------------------------

import type {
  ReportPlanDocumentSection,
  ReportPlanDocumentSectionLayout,
  ReportPlanDocumentWidget,
  ReportPlanDocumentWidgetLayout,
} from '../generated/report-plan'

// Enum aliases — the codegen embeds the same literal unions inline on each
// consuming field, so these are independently useful where callsites need
// to name an enum (function args, switch exhaustiveness checks, etc.). They
// must mirror the Go enum tags; if these drift, callsites will break.
export type ReportWidgetKind = 'file' | 'file-list';
export type ReportFileRenderFormat = 'auto' | 'html' | 'text' | 'code' | 'json' | 'image' | 'video' | 'audio' | 'pdf' | 'link';
export type ReportFileListFormat = 'list' | 'cards' | 'table' | 'gallery';
export type ReportFormatterName =
  | 'currency-inr'
  | 'currency-usd'
  | 'percent'
  | 'percent-1dp'
  | 'short-date'
  | 'long-date'
  | 'datetime'
  | 'number'
  | 'number-1dp'
  | 'number-2dp'
  | 'bytes'
  | 'boolean-icon';
// Direct re-exports of the generated types under their public names. Adding
// a field to any of these = edit the Go struct and regenerate.
export type ReportWidgetLayout = ReportPlanDocumentWidgetLayout;
export type ReportSectionLayout = ReportPlanDocumentSectionLayout;

// Narrowed widget type. The parser always sets `path` to a string, so callsites
// can rely on it being defined — the schema makes it optional because Go's
// omitempty allows it to be absent on the wire.
//
// Reports render stored artifacts only. HTML reports read db/db.sqlite live via
// the injected window.report API; `source` points at a file or folder under db/,
// knowledgebase/, or docs/.
export type ReportWidget = ReportPlanDocumentWidget & {
  source?: string;
  db?: string;
  sql?: string;
  path: string;
};

// Narrowed row — widgets is a ReportWidget[] (with source/path defined),
// not the looser ReportPlanDocumentWidget[] from the raw schema.
export interface ReportWidgetRow {
  widgets: ReportWidget[];
}

// One entry under a section. The schema models this as a flat record with
// optional widget/row fields; the parser narrows to a discriminated union
// keyed on `kind` so callsites get the matching variant.
export type ReportEntry =
  | { kind: 'single'; widget: ReportWidget; tab?: string }
  | { kind: 'row'; row: ReportWidgetRow; tab?: string };

// Narrowed section — entries is the discriminated ReportEntry[] (parser
// output) rather than the loose generated array.
export interface ReportSection extends Omit<ReportPlanDocumentSection, 'entries'> {
  entries: ReportEntry[];
}

// Inline custom palette. Hex strings; the renderer converts them to HSL and
// injects them as CSS variables on the report root, overriding the named
// theme. Authors think in colors; the conversion stays inside the renderer.
export interface ParsedReportThemeColors {
  primary?: string;
  accent?: string;
  card?: string;
  muted?: string;
  border?: string;
  chart?: string[]; // Up to 5; mapped to --chart-1 .. --chart-5.
}

export interface ParsedReportPlan {
  sections: ReportSection[];
  // Optional theme name applied to the report root via data-report-theme.
  // Maps to a CSS block in index.css that overrides --chart-* / --primary
  // and friends. Unknown / unset themes fall back to the workspace defaults.
  theme?: string;
  // Optional inline custom palette. Layers on top of the named theme — any
  // field set here wins over the named theme's value for that variable.
  themeColors?: ParsedReportThemeColors;
}

// Consolidated workspace state (NEW - single API call for all workspace data)
export interface WorkspaceStateResponse {
  success: boolean;
  data?: WorkspaceState;
  error?: string;
}

export interface WorkspaceState {
  run_folders: RunFolderInfo[];
  variables_manifest?: VariablesManifest;
  phases: WorkflowPhase[];
  active_executions?: ActiveWorkflowExecution[];
}

export interface ActiveWorkflowExecution {
  query_id: string;
  session_id: string;
  preset_query_id?: string;
  workspace_path: string;
  run_folder?: string;
  triggered_by: string;
  started_at: string;
}

export interface RunMetadataLLM {
  provider?: string;
  model_id?: string;
}

export interface RunMetadataModels {
  allocation_mode?: string; // "manual" or "tiered"
  execution_llm?: RunMetadataLLM;
  phase_llm?: RunMetadataLLM;
  tier_1?: RunMetadataLLM;
  tier_2?: RunMetadataLLM;
  tier_3?: RunMetadataLLM;
  temp_override?: RunMetadataLLM;
  temp_override_2?: RunMetadataLLM;
}

export interface RunMetadata {
  created_at: string;
  started_at?: string;
  completed_at?: string;
  duration_ms?: number;
  status: string; // "running", "completed", "failed", "canceled"
  triggered_by?: string; // "manual", "cron", "workflow_builder"
  models?: RunMetadataModels;
}

export interface RunFolderInfo {
  name: string;
  metadata?: RunMetadata;
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

// Workflow plan changelog (History view "Plan edits" feed)
export interface PlanChangelogFieldChange {
  step_id: string
  field: string
  old_value: unknown
  new_value: unknown
}

export interface PlanChangelogEntry {
  timestamp: string
  tool: string
  reason: string
  step_ids?: string[]
  changes?: PlanChangelogFieldChange[]
  file?: string
}

export interface PlanChangelogResponse {
  success: boolean
  entries: PlanChangelogEntry[]
  count: number
  error?: string
}

// Workflow Backup
export interface WorkflowBackupConfig {
  enabled: boolean
  mode?: string
  triggers?: WorkflowBackupTriggers
  destinations?: WorkflowBackupDestination[]
  notes?: string
}

export interface WorkflowBackupTriggers {
  after_scheduled_run?: boolean
  after_manual_run?: boolean
}

export interface WorkflowBackupDestination {
  id: string
  type: 'git' | 'object_store' | 'huggingface' | 'local_zip' | string
  provider: 'github' | 'git' | 'r2' | 's3' | 'b2' | 'huggingface' | 'local' | string
  repo?: string
  branch?: string
  bucket?: string
  prefix?: string
  covers?: string[]
  secret_refs?: string[]
  notes?: string
}

export interface WorkflowBackupStatus {
  version: number
  state: 'not_configured' | 'configured_not_verified' | 'running' | 'healthy' | 'stale' | 'partial' | 'failed' | string
  last_attempt_at?: string
  last_success_at?: string
  last_agent_session_id?: string
  last_source_hash?: string
  summary?: string
  destinations?: WorkflowBackupDestinationStatus[]
  last_error?: string
  updated_at?: string
}

export interface WorkflowBackupDestinationStatus {
  id: string
  type?: string
  provider?: string
  state: 'healthy' | 'failed' | 'skipped' | 'running' | string
  last_success_at?: string
  commit?: string
  objects_synced?: number
  summary?: string
  error?: string
}

export interface WorkflowBackupStrategyInfo {
  id: string
  label: string
  description: string
  best_for: string[]
}

export interface WorkflowBackupInfoResponse {
  success: boolean
  config?: WorkflowBackupConfig
  status?: WorkflowBackupStatus
  effective_state: 'not_configured' | 'configured_not_verified' | 'running' | 'healthy' | 'stale' | 'partial' | 'failed' | string
  current_source_hash?: string
  tracked_files_count?: number
  supported: WorkflowBackupStrategyInfo[]
  status_path: string
}

// Workflow Publish (share HTML artifacts to a public URL)
export interface WorkflowPublishConfig {
  enabled: boolean
  mode?: string
  // Agent-authored: may be plain strings ("pulse"/"report") or rich objects.
  targets?: Array<string | { id?: string; artifact?: string; [k: string]: unknown }>
  dashboard_mode?: string     // "snapshot"
  url?: string
  triggers?: WorkflowBackupTriggers
  destinations?: WorkflowPublishDestination[]
  notes?: string
}

export interface WorkflowPublishDestination {
  id: string
  provider: string            // free-form: netlify, vercel, cloudflare-pages, github-pages, s3, ...
  method?: 'cli' | 'git' | 'sync' | string
  site?: string
  secret_name?: string
  visibility?: string         // public | private | unguessable-link
  public_base_url?: string
  url?: string
  covers?: string[]
  notes?: string
}

export interface WorkflowPublishStatus {
  version: number
  state: 'not_configured' | 'configured_not_verified' | 'publishing' | 'published' | 'stale' | 'failed' | string
  url?: string
  last_published_at?: string
  last_attempt_at?: string
  last_agent_session_id?: string
  last_source_hash?: string
  visibility?: string
  secret_name?: string
  targets?: Array<string | { id?: string; artifact?: string; [k: string]: unknown }>
  summary?: string
  destinations?: WorkflowPublishDestinationStatus[]
  last_error?: string
  updated_at?: string
}

export interface WorkflowPublishDestinationStatus {
  id: string
  provider?: string
  method?: string
  state: 'published' | 'failed' | 'skipped' | 'publishing' | string
  url?: string
  last_success_at?: string
  summary?: string
  error?: string
}

export interface WorkflowPublishStrategyInfo {
  id: string
  label: string
  method: string
  description: string
}

export interface WorkflowPublishInfoResponse {
  success: boolean
  config?: WorkflowPublishConfig
  status?: WorkflowPublishStatus
  effective_state: 'not_configured' | 'configured_not_verified' | 'publishing' | 'published' | 'stale' | 'failed' | string
  url?: string
  current_source_hash?: string
  supported: WorkflowPublishStrategyInfo[]
  status_path: string
}

export interface WorkflowPublishSecretResponse {
  success: boolean
  name: string
  value: string
}

// Scheduled Jobs
export interface ScheduledJob {
  id: string
  name: string
  description: string
  entity_type: 'workflow' | 'chat' | 'multi-agent'
  preset_query_id?: string
  workspace_path?: string
  workflow_id?: string
  workflow_label?: string
  trigger_payload?: Record<string, unknown>
  group_names?: string[]  // undefined/empty = all groups
  mode?: 'workshop' | 'multi-agent'
  messages?: string[]  // predefined messages for workflow workshop schedules
  workshop_mode?: 'run' | 'optimizer'  // workflow workshop schedule mode (default: run)
  query?: string  // message to execute (multi-agent mode)
  resume_previous?: boolean  // coding-agent CLI only: explicit true resumes latest prior thread; omitted/false starts fresh
  user_id?: string  // user context (multi-agent mode)
  schedule_type?: 'cron' | 'calendar'
  calendar_items?: CalendarScheduleItem[]
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
  missed_run_count?: number
  latest_missed_run_at?: string
  missed_run_reason?: string
  created_at?: string
  updated_at?: string
  built_in?: boolean
  managed_by?: 'built-in' | 'slash-command' | string
}

export interface CreateScheduledJobRequest {
  name: string
  description?: string
  entity_type: 'workflow' | 'chat' | 'multi-agent'
  preset_query_id?: string
  workspace_path?: string
  trigger_payload?: Record<string, unknown>
  group_names?: string[]  // undefined/empty = all groups
  mode?: 'workshop' | 'multi-agent'
  messages?: string[]
  workshop_mode?: 'run' | 'optimizer'
  query?: string
  resume_previous?: boolean
  schedule_type?: 'cron' | 'calendar'
  calendar_items?: CalendarScheduleItem[]
  cron_expression?: string
  timezone?: string
  enabled?: boolean
}

export interface UpdateScheduledJobRequest {
  name?: string
  description?: string
  trigger_payload?: Record<string, unknown>
  group_names?: string[]       // undefined = don't change; [] = run all groups; [...] = specific groups
  set_group_names?: boolean    // must be true to actually update group_names
  mode?: 'workshop' | 'multi-agent'
  messages?: string[]
  workshop_mode?: 'run' | 'optimizer'
  query?: string
  resume_previous?: boolean
  schedule_type?: 'cron' | 'calendar'
  calendar_items?: CalendarScheduleItem[]
  cron_expression?: string
  timezone?: string
  enabled?: boolean
}

export interface CalendarScheduleItem {
  id?: string
  date: string
  time: string
  description?: string
  trigger_payload?: Record<string, unknown>
  messages?: string[]
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
  group_names?: string[]
  started_at: string
  completed_at?: string
}

export interface ListScheduledJobRunsResponse {
  runs: ScheduledJobRun[]
  total: number
  limit: number
  offset: number
}

export interface SchedulerConfig {
  globally_paused: boolean
  paused_at?: string
  paused_by?: string
  updated_at?: string
}

// --- Workflow Manifest Types (file-backed workflow definitions) ---

export interface WorkflowManifest {
  schema_version: number
  id: string
  version?: string
  label: string
  capabilities: WorkflowCapabilities
  execution_defaults: WorkflowExecutionDefaults
  ownership: WorkflowOwnership
  schedules: WorkflowScheduleEntry[]
  created_at?: string
  updated_at?: string
  run_retention_count?: number
  post_run_monitor?: boolean
  backup?: WorkflowBackupConfig
}

export interface WorkflowCapabilities {
  selected_servers: string[]
  selected_tools: string[]
  selected_skills: string[]
  selected_secrets: string[]
  selected_global_secret_names: string[] | null // null = all, [] = none
  browser_mode: string
  use_code_execution_mode: boolean
  llm_config?: PresetLLMConfig
}

export interface WorkflowExecutionDefaults {
  always_use_same_run: boolean
  // Global step overrides (replaces step_override.json)
  disable_learning?: boolean
  global_skill_objective?: string
  disable_parallel_tool_execution?: boolean
  execution_max_turns?: number
  enabled_custom_tools?: string[]
  workshop_mode?: string // Workshop builder mode: "builder", "optimizer", or "run"
}

export interface WorkflowOwnership {
  employee_id: string | null
}

export interface WorkflowScheduleEntry {
  id: string
  name: string
  description?: string
  schedule_type?: 'cron' | 'calendar'
  cron_expression: string
  timezone: string
  enabled: boolean
  trigger_payload?: Record<string, unknown>
  calendar_items?: CalendarScheduleItem[]
  group_names?: string[]
  mode?: 'workshop' | 'multi-agent' | string
  messages?: string[]
  workshop_mode?: 'run' | 'optimizer' | string
  query?: string
  resume_previous?: boolean
}

export interface DiscoveredWorkflow {
  workspace_path: string
  manifest: WorkflowManifest
}

export interface ListWorkflowManifestsResponse {
  success: boolean
  workflows: DiscoveredWorkflow[]
  total: number
}

export interface GetWorkflowManifestResponse {
  success: boolean
  manifest: WorkflowManifest
  workspace_path: string
}

export interface CreateWorkflowManifestRequest {
  label: string
  workspace_path: string
  capabilities?: Partial<WorkflowCapabilities>
  execution_defaults?: Partial<WorkflowExecutionDefaults>
  human_verification_required?: boolean
}

export interface UpdateWorkflowManifestRequest {
  workspace_path: string
  label?: string
  capabilities?: WorkflowCapabilities
  execution_defaults?: WorkflowExecutionDefaults
  ownership?: WorkflowOwnership
  schedules?: WorkflowScheduleEntry[]
  workshop_mode?: string // Standalone patch — avoids zeroing out other execution_defaults fields
  run_retention_count?: number
  post_run_monitor?: boolean
}

export interface DuplicateWorkflowManifestRequest {
  source_workspace_path: string
  target_workspace_path: string
  new_label?: string
}

export interface MigrateWorkflowsResponse {
  success: boolean
  results: Array<{
    preset_id: string
    label: string
    workspace_path: string
    status: 'migrated' | 'skipped' | 'error'
    error?: string
  }>
  migrated: number
  skipped: number
  errors: number
  total: number
}

// --- Output plan (planning/output_plan.json) ---

export interface WorkflowOutputPlanStep {
  id?: string
  title?: string
  description?: string
  instructions?: string
  output_filename?: string
  enabled?: boolean
  context_dependencies?: string[]
  context_output?: string
}

export interface WorkflowOutputPlan {
  step: WorkflowOutputPlanStep | null
}

// Per-user override for where workflow questions should land. Empty fields
// fall back to the connector's workspace-wide default. The Disabled flags let
// a user opt out of one connector entirely (e.g. only Slack, never WhatsApp).
export interface NotificationPreference {
  slack_channel_id?: string
  slack_disabled?: boolean
  whatsapp_phone?: string
  whatsapp_disabled?: boolean
}
