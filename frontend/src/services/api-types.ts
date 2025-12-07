// API-specific types (separate from event types)
import type { PollingEventSchema } from '../generated/events-bridge'
import type { EventTypeString } from '../generated/event-types'

// LLM Configuration types
export interface LLMConfiguration {
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'
  model_id: string
  fallback_models: string[]
  cross_provider_fallback?: {
    provider: 'openai' | 'bedrock' | 'openrouter' | 'vertex' | 'anthropic'
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
  }
}

// Extended LLM Configuration for frontend (secrets/UI-only)
export type ExtendedLLMConfiguration = Omit<LLMConfiguration, 'api_keys'> & {
  api_key?: string
  region?: string
}

// Execution mode constants matching backend enum

// Agent streaming types
export interface AgentQueryRequest {
  query: string
  provider?: 'bedrock' | 'openai' | 'openrouter' | 'vertex' | 'anthropic'
  model_id?: string
  temperature?: number
  max_turns?: number
  enabled_tools?: string[]
  enabled_servers?: string[]
  selected_tools?: string[] // Array of "server:tool" strings
  agent_mode?: 'simple' | 'workflow'
  llm_config?: LLMConfiguration
  preset_query_id?: string
  // Code execution mode: When enabled, only virtual tools are added to LLM
  // MCP tools are accessed via generated Go code using discover_code_files and write_code
  use_code_execution_mode?: boolean
  // Execution options from frontend (for workflow execution phase)
  execution_options?: ExecutionOptions
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
  vertex_config?: ExtendedLLMConfiguration
  anthropic_config?: ExtendedLLMConfiguration
  available_models: {
    bedrock: string[]
    openrouter: string[]
    openai: string[]
    vertex?: string[]
    anthropic?: string[]
  }
}

// API Key Validation Request/Response
export interface APIKeyValidationRequest {
  provider: 'openrouter' | 'openai' | 'bedrock' | 'vertex' | 'anthropic'
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

// Slack Feedback Configuration types
export interface SlackConfig {
  enabled: boolean
  bot_token?: string  // Masked in GET response
  app_token?: string  // Masked in GET response (App-level token for Socket Mode)
  channel_id?: string
}

export interface SlackConfigRequest {
  enabled: boolean
  bot_token: string  // Bot User OAuth Token (xoxb-...)
  app_token: string  // App-level token (xapp-...) for Socket Mode
  channel_id: string
}

export interface SlackConfigResponse {
  enabled: boolean
  bot_token?: string  // Masked in GET
  app_token?: string  // Masked in GET
  channel_id?: string
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

export interface RegisterObserverResponse {
  observer_id: string
  status: string
  message: string
}

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

// Preset LLM Configuration types
export interface AgentLLMConfig {
  provider: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'
  model_id: string
}

export interface PresetLLMConfig {
  // Legacy: Single default model (for backward compatibility)
  provider?: 'openrouter' | 'bedrock' | 'openai' | 'vertex' | 'anthropic'
  model_id?: string

  // New: Agent-specific default models (takes priority over legacy fields)
  execution_llm?: AgentLLMConfig        // Default for execution agents
  validation_llm?: AgentLLMConfig       // Default for validation agents
  learning_llm?: AgentLLMConfig         // Default for learning agents
  learning_reading_llm?: AgentLLMConfig // Default for learning reading agent
  planning_llm?: AgentLLMConfig         // Default for planning agent
  variable_extraction_llm?: AgentLLMConfig // Default for variable extraction agent
  anonymization_llm?: AgentLLMConfig    // Default for anonymization agent
  plan_improvement_llm?: AgentLLMConfig // Default for plan improvement agent
  plan_tool_optimization_llm?: AgentLLMConfig // Default for plan tool optimization agent
  plan_learnings_alignment_llm?: AgentLLMConfig // Default for plan learnings alignment agent
  learning_consolidation_llm?: AgentLLMConfig // Default for learning consolidation agent
}

// Preset Query API types
export interface PresetQuery {
  id: string;
  label: string;
  query: string;
  selected_servers: string; // JSON string
  selected_tools: string; // JSON string of "server:tool" array
  selected_folder?: string; // Single folder path (nullable)
  agent_mode: string;
  llm_config: string; // JSON string of PresetLLMConfig
  use_code_execution_mode?: boolean;
  is_predefined: boolean;
  created_at: string;
  updated_at: string;
  created_by: string;
}

export interface CreatePresetQueryRequest {
  label: string;
  query: string;
  selected_servers?: string[];
  selected_tools?: string[]; // NEW
  selected_folder?: string; // Single folder path
  agent_mode?: string;
  llm_config?: PresetLLMConfig; // LLM configuration for this preset
  use_code_execution_mode?: boolean; // MCP code execution mode
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
  resume_from_step?: number;  // 1-based step number
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
  
  // Variable group execution options (for batch execution with multiple groups)
  enabled_group_ids?: string[];  // Group IDs to execute (if empty, uses groups' enabled flags)
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

export type ExecutionStrategyType = typeof ExecutionStrategy[keyof typeof ExecutionStrategy];

// Variable Groups API types
export interface Variable {
  name: string;
  value?: string;  // Used in single-group mode
  description: string;
}

export interface VariableGroup {
  group_id: string;  // e.g., "group-1", "group-2"
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