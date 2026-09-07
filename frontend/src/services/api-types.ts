// API-specific types (separate from event types)
import type { PollingEventSchema } from '../generated/event-types'
import type { EventTypeString } from '../generated/event-types'
import type { PollingEvent, RuntimeSnapshot, TerminalSnapshot } from '../../shared/session/types'
export type { PollingEvent, RuntimePhase, RuntimeSnapshot, GetEventsResponse, TerminalEventsResponse, TerminalSnapshot, TerminalSnapshotRow, TerminalStatus, SSEEventMessage, SSEStatusMessage } from '../../shared/session/types'
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
    provider: 'openai' | 'bedrock' | 'vertex' | 'anthropic' | 'azure' | 'claude-code' | 'codex-cli' | 'cursor-cli' | 'agy-cli' | 'pi-cli'
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
  browser_mode?: 'none' | 'auto' | 'headless' | 'cdp'
  // CDP port for connecting to an existing Chrome browser (local mode only)
  cdp_port?: number
  // Specialized multi-profile CDP testing. Each port uses a distinct Chrome profile.
  cdp_ports?: number[]
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
  // Conversation JSON selected from a previous chat panel. The backend
  // can use its runtime metadata for native coding-agent resume.
  restored_conversation_path?: string
  // Auto-notification flag: when true, this is a background agent completion notification,
  // not a user-initiated message. Backend treats it as a synthetic turn (doesn't block user input).
  is_auto_notification?: boolean
  // Force a server-owned turn instead of injecting this message directly into
  // an idle retained coding CLI. Product surfaces use this when their clean UI
  // depends on structured progress and completion events for every message.
  disable_live_input_delivery?: boolean
  // Immutable generic Agent Profile binding used by product surfaces.
  agent_profile_id?: string
  agent_profile_version?: number
  agent_profile_context?: {
    project_title: string
    workspace_description?: string
    local_datetime?: string
  }
  selected_folder?: string
  // True after the user converts an observed schedule/bot run into an
  // interactive chat. Keeps the same session/native conversation identity.
  user_interactive_continuation?: boolean
}

// Delegation tier configuration for multi-LLM support
export interface DelegationTierConfig {
  schema_version: 2
  mode: 'provider_profile' | 'explicit'
  provider?: string
  main?: TierModel    // orchestrator/main agent model
  high?: TierModel
  medium?: TierModel
  low?: TierModel
  custom?: Record<string, CustomTierModel>  // slug → custom tier
}

export interface TierModel {
  provider: string
  model_id: string
  options?: Record<string, unknown>
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
  kind?: 'fix_bundle' | 'strategy_experiment' | string
  guardrails?: string[]
  rollback_condition?: string
  human_input_id?: string
  terminal_outcome?: string
  message?: string
  sse_endpoint?: string
  session_id?: string
  // Populated only when status === 'live_input_delivered'.
  delivery_status?: 'sent_to_cli' | 'queued_for_injection' | 'next_turn_started'
  provider?: string
}

// Minimal product-chat wire contract. Profile-owned model, prompt, tools,
// skills and workspace configuration are deliberately absent.
export interface AgentProfileChatRequest {
  message: string
  conversation_key?: string
  // One of the profile's declared runtime.provider_options[].id; the server
  // resolves it to that option's (provider, model_id) and rejects any other.
  engine?: string
  // A model within that engine's provider, from the platform's model catalog
  // (/api/llm-config/models/metadata); omitted keeps the option's own model.
  model_id?: string
  // A reasoning level from the engine's declared reasoning_efforts; omitted
  // keeps whatever the engine's own runtime options declare.
  reasoning_effort?: string
}

export interface AgentProfileConversationRequest {
  conversation_key?: string
  /** An earlier conversation of the slot to make live again (…/conversation/switch). */
  session_id?: string
}

export interface AgentProfileConversationResponse {
  conversation_id: string
  conversation_key: string
  session_id: string
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

export interface PendingHumanFeedbackRequest {
  unique_id: string
  message_for_user: string
  context?: string
  session_id?: string
  options?: string[]
  allow_feedback: boolean
  created_at: string
  expires_at: string
}

export interface PendingHumanFeedbackResponse {
  requests: PendingHumanFeedbackRequest[]
}

export interface ReportHumanInputOption {
  id: string
  title: string
  description?: string
}

export interface ReportHumanInputApplyContract {
  mode?: 'no_change' | 'direct_apply' | 'targeted_fixer' | 'external_wait' | string
  issue_id?: string
  approved_scope?: string
  pre_run_checks?: string[]
  post_run_proof?: string
  failure_policy?: 'continue_unchanged' | 'block_run' | string
}

export interface ReportHumanInput {
  id: string
  workspace_path: string
  source: 'pulse' | 'technical_review' | 'strategic_review' | 'plan_drift_review' | 'engineering_review' | 'ops_review' | 'strategy_auditor' | 'goal_advisor' | string
  priority: 'low' | 'medium' | 'high' | string
  question: string
  context?: string
  options: ReportHumanInputOption[]
  allow_free_text: boolean
	status: 'pending' | 'answered' | 'claimed' | 'consumed' | 'dismissed' | string
  selected_option_id?: string
  note?: string
  run_id?: string
  evidence?: string
  created_by?: string
  answered_by?: string
  answered_by_kind?: string
  answered_via?: string
  answered_session_id?: string
  consumed_by?: string
  outcome_summary?: string
  created_at: string
  updated_at: string
  answered_at?: string
  consumed_at?: string
	dismissed_at?: string
	claim_token?: string
	claimed_at?: string
	claim_expires_at?: string
	apply_contract?: ReportHumanInputApplyContract
}

export interface ReportHumanInputsResponse {
  success: boolean
  inputs: ReportHumanInput[]
  error?: string
}

export interface ReportHumanInputResponse {
  success: boolean
  input: ReportHumanInput
  error?: string
}

export interface PulseModuleState {
  workspace_path: string
  module: string
  last_pulse_run_id?: string
  last_checked_at?: string
  last_ran_at?: string
  last_decision?: string
  last_reason?: string
  last_gate_decision?: string
  last_result?: string
  last_result_reason?: string
  next_check_at?: string
  next_check_after_run_id?: string
  cooldown_runs?: number
  evidence?: string[]
  updated_at?: string
}

export interface PulseFinalCommandState {
  workspace_path: string
  command: string
  pulse_run_id?: string
  status?: string
  reason?: string
  started_at?: string
  finished_at?: string
  updated_at?: string
}

export interface PulseRunMode {
  workspace_path: string
  pulse_run_id: string
  mode: 'backlog_drain' | 'discovery' | 'strategy' | 'observe' | string
  reason: string
  recorded_at: string
}

export interface PulseReviewFocus {
  workspace_path: string
  module: string
  focus_key: string
  last_pulse_run_id?: string
  last_reviewed_at?: string
  last_verdict?: string
  last_selection_reason?: string
  route_scope?: string
  next_check_at?: string
  next_check_reason?: string
  updated_at: string
  review_count?: number
  route_review_count?: number
  deferred_focuses?: string[]
  issue_ids?: string[]
}

export interface PulseLoopClosureFinding {
  kind: 'answer_not_applied' | 'decision_waiting_on_user' | 'concern_keeps_recurring' | string
  severity: 'high' | 'medium' | string
  subject: string
  detail: string
  evidence: string
  age_days: number
  id?: string
}

export interface PulseShadowSignalObservation {
  workspace_path: string
  pulse_run_id: string
  detector: string
  detector_version: string
  observed_at: string
  coverage_status: 'verified' | 'partial' | 'not_instrumented' | 'unavailable' | string
  coverage_reason?: string
  signals: PulseLoopClosureFinding[]
  recorded_at: string
}

export interface PulseModuleStateResponse {
  success: boolean
  modules: PulseModuleState[]
  commands: PulseFinalCommandState[]
  gate_mode?: PulseRunMode | null
  review_focus_history?: PulseReviewFocus[]
  review_focus_selections?: PulseReviewFocus[]
  shadow_signal_observations?: PulseShadowSignalObservation[]
  shadow_signal_coverage?: {
    status: string
    reason?: string
  }
  error?: string
}

export interface PulseInterventionSource {
  source_type: 'attempt' | 'experiment' | 'finding' | 'review' | string
  source_id: string
}

export interface PulseIntervention {
  intervention_id: string
  pulse_run_id?: string
  title: string
  criterion_id: string
  impact_type: 'direct_goal' | 'reliability' | 'measurement' | 'presentation_maintenance' | string
  metric: string
  expected_direction: 'increase' | 'decrease' | 'maintain' | string
  scope?: string[]
  provenance?: string
  baseline_window?: string
  checkpoint?: string
  minimum_evidence_runs: number
  status: string
  kind?: 'fix_bundle' | 'strategy_experiment' | string
  guardrails?: string[]
  rollback_condition?: string
  interference_domains?: string[]
  human_input_id?: string
  terminal_outcome?: string
  created_at?: string
  updated_at?: string
  sources?: PulseInterventionSource[]
}

export interface PulseGoalObservation {
  observation_id: string
  criterion_id: string
  metric: string
  run_id: string
  route?: string
  environment?: string
  value?: number
  status?: string
  unit?: string
  observed_at: string
  evidence?: string[]
  recorded_at?: string
}

export interface PulseImpactAssessment {
  assessment_id: string
  intervention_id: string
  verdict: 'improved' | 'unchanged' | 'regressed' | 'inconclusive' | 'confounded' | string
  before_window: string
  after_window: string
  before_value?: number
  after_value?: number
  absolute_change?: number
  relative_change?: number
  confidence: string
  confounders?: string[]
  evidence?: string[]
  next_checkpoint?: string
  assessed_at: string
}

export interface PulseImpactLedger {
  interventions: PulseIntervention[]
  observations: PulseGoalObservation[]
  assessments: PulseImpactAssessment[]
}

export interface PulseImpactResponse {
  success: boolean
  impact: PulseImpactLedger
  error?: string
}

export interface PulseContextRecord {
  context_id: string
  section: string
  context_text: string
  example_note?: string
  path: string
  created_at: string
}

export interface PulseContextResponse {
  success: boolean
  records: PulseContextRecord[]
  total: number
  error?: string
}

export interface EvalResultRecord {
  run_folder: string
  step_id: string
  title?: string
  description?: string
  score: number
  max_score: number
  score_captured: boolean
  reasoning: string
  evidence: string
  skipped: boolean
  generated_at: string
  historical?: boolean
}

export interface PulseEvalResultsResponse {
  success: boolean
  results: EvalResultRecord[]
  error?: string
}

export interface PulseFindingVerification {
  check: string
  verdict: 'passed' | 'failed' | 'inconclusive' | string
  expected?: string
  observed?: string
  evidence?: string[]
  verified_at?: string
}

export interface PulseFixFindingRef {
  finding_id: string
  disposition?: string
  summary?: string
}

export interface PulseFixAttempt {
  attempt_id: string
  module: string
  pulse_run_id: string
  summary: string
  status: string
  intended_files: string[]
  changed_files: string[]
  before_refs: string[]
  after_refs: string[]
  started_at: string
  completed_at?: string
  findings?: PulseFixFindingRef[]
}

export interface PulseFindingEvent {
  finding_id?: string
  event_type: string
  summary: string
  pulse_run_id?: string
  attempt_id?: string
  metadata?: Record<string, unknown>
  recorded_at: string
}

export interface PulseFindingReproduction {
  safe: boolean
  setup?: string
  action?: string
  expected?: string
  observed?: string
  limitations?: string
}

export interface PulseFindingDetails {
  finding_id?: string
  target_key?: string
  issue_kind?: 'harness_issue' | 'workflow_bug' | 'external_dependency' | 'evidence_gap' | string
  recommended_route?: 'decision_required' | 'evidence_wait' | 'fixer_handoff' | 'none' | string
  next_check?: string
  classification?: string
  severity?: string
  summary?: string
  impact?: string
  workaround?: string
  evidence?: string[]
  reproduction: PulseFindingReproduction
  platform?: {
    issue_key: string
    affected_workflows: string[]
    seen_count: number
    first_seen_at?: string
    last_seen_at?: string
  }
}

export interface PulseIssue {
  id: string
  title: string
  description?: string
  status: 'backlog' | 'in_progress' | 'in_review' | 'needs_input' | 'blocked' | 'done' | 'canceled' | 'external' | string
  priority: 'urgent' | 'high' | 'medium' | 'low' | 'none' | string
  module?: string
  created_at?: string
  updated_at?: string
  seen_count: number
}

export interface PulseFindingLifecycle {
  /** Compact user-facing issue. Remaining fields are lifecycle internals. */
  issue?: PulseIssue
  /**
   * `observation` is workflow evidence awaiting reviewer classification;
   * `issue` has been accepted into Pulse's repair lifecycle.
   */
  kind?: 'issue' | 'observation'
  finding_id?: string
  module?: string
  step_id: string
  phase: string
  group_name?: string
  text: string
  status: string
  first_seen_run?: string
  first_seen_at?: string
  last_seen_run?: string
  last_seen_at?: string
  seen_count: number
  resolution_note?: string
  external_owner?: string
  reason_code?: string
  reopen_condition?: string
  details?: PulseFindingDetails
  fix_attempts: PulseFixAttempt[]
  verifications: PulseFindingVerification[]
  events: PulseFindingEvent[]
}

export interface PulseFindingsResponse {
  success: boolean
  total?: number
  findings: PulseFindingLifecycle[]
  error?: string
}

export interface PulseReviewRecord {
  id: number
  module: string
  review_run_id: string
  pulse_run_id?: string
  verdict?: string
  status?: string
  finding_count: number
  verification_count: number
  recorded_at: string
  metrics?: PulseAgentMetricRecord
}

export interface PulseAgentMetricRecord {
  id: number
  execution_id: string
  agent_session_id?: string
  pulse_run_id?: string
  review_run_id?: string
  module: string
  role: 'reviewer' | 'fixer'
  status: string
  queued_at?: string
  started_at?: string
  completed_at?: string
  queue_duration_ms: number
  duration_ms: number
  llm_call_count: number
  prompt_tokens: number
  completion_tokens: number
  reasoning_tokens: number
  cache_read_tokens: number
  cache_write_tokens: number
  total_cost_usd: number
  models?: Record<string, {
    prompt_tokens: number
    completion_tokens: number
    reasoning_tokens: number
    cache_read_tokens: number
    cache_write_tokens: number
    total_cost_usd: number
    call_count: number
  }>
  usage_status: 'captured' | 'unavailable'
  usage_error?: string
  recorded_at: string
}

export interface PulseReviewsResponse {
  success: boolean
  total?: number
  reviews: PulseReviewRecord[]
  error?: string
}

export interface PulseAgentMetricsResponse {
  success: boolean
  total?: number
  metrics: PulseAgentMetricRecord[]
  error?: string
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

// Shape of GET/PUT /api/whatsapp/routing entries. Same idea as ChannelRoute
// but workshop_mode is an untyped string on this endpoint.
export interface WhatsAppRoute {
  workflow_id: string
  workspace_path?: string
  workshop_mode?: string
  send_full_details?: boolean
}

// Shape of GET /api/whatsapp/status. enabled = connector started at server
// startup; paired = device identity stored; connected = live WS.
export interface WhatsAppStatus {
  enabled: boolean
  paired: boolean
  connected: boolean
  own_jid: string
  qr_available: boolean
  qr_expires_at?: string
  link_code?: string
  link_code_expires_at?: string
  bound_chat_count?: number
  owner_user_id?: string
  owner_email?: string
  owner_username?: string
  owner_paired_at?: string
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
  /**
   * The authenticated sending address. `gws auth status` reports no identity,
   * so this comes from a separate profile lookup; empty when unauthenticated
   * or not yet resolved.
   */
  email?: string
  /**
   * No fresh result yet — a refresh is running in the background. Render a
   * pending state rather than a failure; unknown is not the same as broken.
   */
  checking?: boolean
}

export interface GmailConfigRequest {
  enabled: boolean
  default_to: string
  blocked_recipients?: string[]
}

export interface GmailConfigResponse {
  enabled: boolean
  default_to?: string
  blocked_recipients?: string[]
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

// Re-export the EventTypeString for convenience
export type { EventTypeString }








export interface ListTerminalsResponse {
  terminals: TerminalSnapshot[]
  total: number
  runtime_states?: Record<string, RuntimeSnapshot>
}

// Observer APIs removed - no longer needed

// Active Session Management Types
export interface ActiveSessionInfo {
  session_id: string
  parent_session_id?: string
  session_kind?: string
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
  phase_id?: string
  phase_name?: string
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
  display_status?: 'busy' | 'idle' | 'stopped'
  can_steer?: boolean
  runtime_state?: RuntimeSnapshot
}

export interface GetActiveSessionsResponse {
  active_sessions: ActiveSessionInfo[]
  total: number
}

export interface WorkflowScheduleSummary {
  scheduled_workflows: number
  running_workflows: number
  total_schedules: number
  running_schedules: number
}

export interface HeaderSummaryResponse {
  active_sessions: ActiveSessionInfo[]
  total: number
  schedule_summary: WorkflowScheduleSummary
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
  display_status?: 'busy' | 'idle' | 'stopped'
  agent_mode?: string
  created_at?: string
  last_activity?: string
  completed_at?: string
  query?: string
  can_steer?: boolean
  has_retained_tmux_session?: boolean
  runtime_state?: RuntimeSnapshot
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
  runtime_state?: RuntimeSnapshot
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

// Persisted-message shapes live in the shared session client.
export type { ChatHistoryMessagePart, ChatHistoryMessage } from '../../shared/session/types'
import type { ChatHistoryMessage } from '../../shared/session/types'

export interface ChatHistoryConversation {
  session_id: string;
  agent_mode?: string;
  runtime?: ChatHistoryAgentRuntime;
  workshop_mode?: 'workshop' | 'run' | string;
  conversation_history: ChatHistoryMessage[];
  terminal_snapshots?: TerminalSnapshot[];
  ui_events?: PollingEventSchema[];
  history_pagination?: {
    has_more: boolean;
    next_offset: number;
    start_turn: number;
    total_turns: number;
  };
  history_source_message_count?: number;
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
  /** Present on role === 'tool': which tool produced this result. */
  toolName?: string;
  /** Present on role === 'tool': the tool reported a failure. */
  isError?: boolean;
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

export type ChatHistorySessionKind = 'chat' | 'schedule' | 'bot'

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
  kind?: string;
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
  runtime_state?: RuntimeSnapshot;
  // Same collapsed busy/idle/stopped read ActiveSessionInfo.display_status
  // ships, computed from runtime_state so callers don't have to re-derive it
  // from runtime_state.phase themselves. See PLAT-095.
  display_status?: 'busy' | 'idle' | 'stopped';
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
  // Sum of time spent waiting for LLM generations. This deliberately excludes
  // tool execution and queue time, so it is not a workflow wall-clock duration.
  llm_generation_duration_ms?: number
}

// CostDateAggregate is one row in the per-date rollup. It inherits all
// CostAggregate fields (so existing per-date totals still read flat),
// and adds an optional per-model breakdown for that date so clients
// can expand a row to see which models contributed.
export interface CostDateAggregate extends CostAggregate {
  by_model?: Record<string, CostAggregate>
  by_scope?: Record<string, CostScopeAggregate>
  workflow_run_count?: number
}

// CostExecutionAggregate is one runtime execution's cost, with an optional
// phase breakdown (PLAT-166): a workflow step's own execution-turn cost vs
// its separate post-completion reflection-turn cost, when the ledger entries
// making up this execution carried a phase. Absent for executions that never
// distinguished a phase — the combined total above is unaffected either way.
export interface CostExecutionAggregate extends CostAggregate {
  by_phase?: Record<string, CostAggregate>
}

export interface CostScopeAggregate extends CostAggregate {
  by_execution?: Record<string, CostExecutionAggregate>
}

export interface CostSummary {
  from?: string
  to?: string
  total: CostAggregate
  by_date: Record<string, CostDateAggregate>
  by_model: Record<string, CostAggregate>
  by_scope?: Record<string, CostScopeAggregate>
}

export interface WorkflowActivityTimingAggregate {
  duration_ms: number
  llm_duration_ms: number
  tool_duration_ms: number
  by_execution?: Record<string, WorkflowActivityTimingAggregate>
}

export interface WorkflowActivityTimingSummary {
  by_scope: Record<string, WorkflowActivityTimingAggregate>
  by_date: Record<string, { by_scope: Record<string, WorkflowActivityTimingAggregate> }>
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
  schema_version: 2
  mode: 'provider_profile' | 'explicit'

  // Simple mode: resolve the provider's current role defaults at runtime.
  provider?: LLMProvider

  // Advanced mode: pin every workflow role directly.
  builder_llm?: AgentLLMConfig
  maintenance_llm?: AgentLLMConfig
  pulse_llm?: AgentLLMConfig
  // Feature toggles
  use_knowledgebase?: boolean           // nil/true = enabled (default), false = disabled
  enable_context_summarization?: boolean // nil/true = enabled (default), false = disabled
  enable_context_editing?: boolean       // nil/false = disabled (default), true = enabled

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
  runtime_debug?: boolean;
  terminal_live_attach?: boolean;
  /** Streaming microphone dictation (agent_go/pkg/voicestt, voicestt.Status).
   * `available` is a build-time fact (false in a CGO_ENABLED=0 build);
   * `installed` means the model is on disk; `ready` flips once it has loaded. */
  voice?: VoiceEngineStatus;
}

/** Mirrors voicestt.Status — also what GET /api/voice/status returns. */
export interface VoiceEngineStatus {
  available: boolean;
  installed: boolean;
  downloading: boolean;
  got_bytes: number;
  total_bytes: number;
  loading: boolean;
  ready: boolean;
  active_streams: number;
  error?: string;
  model_dir: string;
  size_mb: number;
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
  selected_run_folder?: string;
  execution_strategy: string;
  resume_from_step?: number;  // 1-based step number (for top-level steps)
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
  // How the eval runs (PLAT-287): "scripted" replays learnings/<id>/main.py,
  // "agentic" (the default when absent) lets the agent judge each run. Lives
  // on the eval step itself; the old agent_configs.declared_execution_mode is
  // legacy and stripped by the v1.0.39 migration.
  execution_mode?: 'scripted' | 'agentic'
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
  // "pre_validation" records are automatic structural checks that can trigger
  // a message-sequence repair turn; ordinary "validation" is the legacy step
  // validation format.
  kind?: 'validation' | 'pre_validation' | string;
  phase?: string;
  execution_attempt?: number;
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
  source?: 'routing_evaluation' | string;
  file_path?: string;
  routing_evaluation?: {
    routing_question?: string;
    selected_route_id?: string;
    routing_reasoning?: string;
    route_selection?: {
      source_kind?: string;
      source_path?: string;
      raw_value?: string;
    };
    route_next_steps?: Record<string, string>;
  };
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
  parent_step_id?: string;
  parent_step_title?: string;
  route_id?: string;
  // Present when route_kind === 'routing': which routing/branch step (route
  // major-fork concept, PLAT-259) selected this route for this run, and the
  // route's own display name. Distinct from parent_step_id/parent_step_title
  // above, which cover the unrelated todo_task orchestrator sub-agent-route
  // case -- kept visually separate in the UI since they're different
  // mechanisms.
  route_name?: string;
  route_kind?: 'orchestrator' | 'routing';
  route_step_id?: string;
  route_step_title?: string;
  success_criteria?: string;
  execution_tier?: string; // Configured tier pin from step_config.json ("high"|"medium"|"low"); empty when the step uses adaptive tiering
  context_output?: string;  // Expected output filename
  learning_objective?: string;
  learnings_access?: string;
  knowledgebase_access?: string;
  knowledgebase_write_method?: string;
  knowledgebase_contribution?: string;
  message_sequence_status?: 'running' | 'completed' | 'failed';
  message_sequence?: {
    session_path: string;
    status?: 'running' | 'completed' | 'failed' | string;
    entries?: MessageSequenceLogEntry[];
  };
  // The message text authored in the workflow plan, distinct from the larger
  // runtime prompt assembled for the agent.
  planned_messages?: PlannedMessageSequenceItem[];
  output_content?: StepOutputContent;  // Actual output file content
  artifacts?: { file_name: string; file_path: string }[]; // Other output files
  validations: ValidationLog[];
  executions: ExecutionAttemptLog[];
  orchestration?: OrchestrationLog[];
  todo_task?: TodoTaskLog[];
  learnings?: LearningLog[];
  archived_logs?: ArchivedLogEntry[];  // Logs from previous runs
  archived_executions?: ArchivedExecutionEntry[];  // Archived execution outputs from previous routing
}

export interface MessageSequenceLogEntry {
  entry_id: string;
  item_id?: string;
  item_type?: string;
  source?: string;
  status: 'running' | 'completed' | 'failed' | string;
  summary?: string;
  started_at?: string;
  ended_at?: string;
}

export interface PlannedMessageSequenceItem {
  id: string;
  type?: string;
  kind?: string;
  message: string;
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
  scoped_costs?: CostSummary;
  phase_token_usage?: PhaseTokenUsageFile;
  phase_daily_costs: WorkflowPhaseDailyCostsEntry[];
  run_daily_costs?: WorkflowRunDailyCostsEntry[];
  runs: WorkflowRunCostsEntry[];
  activity_timing?: WorkflowActivityTimingSummary;
  history?: {
    days: number;
    window_from: string;
    window_to: string;
    has_more: boolean;
    next_before?: string;
  };
}

export interface ExecutionLogsResponse {
  success: boolean;
  steps: Record<string, StepExecutionLogs>; // key is step ID or name (e.g. "step-1")
  token_usage?: TokenUsageFile;
  pulse_reviews?: PulseReviewRunLog[];
}

export interface PulseReviewTranscriptEvent {
  timestamp: string;
  type: 'user_message' | 'assistant_message' | 'tool_call' | string;
  role?: string;
  text?: string;
  tool_call?: Record<string, unknown>;
}

export interface PulseBackgroundAgentLog {
  agent_id: string;
  name: string;
  kind?: string;
  parent_execution_id?: string;
  status: string;
  result?: string;
  error?: string;
  duration?: string;
  started_at?: string;
  completed_at?: string;
  transcript_path?: string;
  transcript_status?: string;
  provider?: string;
  model_id?: string;
  events?: PulseReviewTranscriptEvent[];
}

export interface PulseReviewRunLog {
  run_id: string;
  session_id: string;
  schedule_id?: string;
  trigger_source?: string;
  status?: string;
  started_at: string;
  completed_at?: string;
  agents: PulseBackgroundAgentLog[];
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
  score_captured?: boolean;
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
export type ReportWidgetKind = 'file' | 'file-list' | 'interaction';
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
// The workflow-owned HTML report document renders stored artifacts and reads db/db.sqlite live via
// window.report. Native interaction widgets have no `source`; they persist
// configured user responses to the workflow database.
export type ReportWidget = ReportPlanDocumentWidget & {
  source?: string;
  db?: string;
  sql?: string;
  path: string;
};

export interface ReportWidgetResponse {
  workspace_path: string;
  widget_id: string;
  instance_key: string;
  question: string;
  response_kind: 'choice' | 'text' | 'choice-with-text' | string;
  options: ReportHumanInputOption[];
  allow_free_text: boolean;
  subject_id?: string;
  subject_version?: string;
  subject_hash?: string;
	status: 'pending' | 'answered' | 'executing' | 'completed' | 'failed' | 'expired' | 'cancelled' | string;
  selected_option_id?: string;
  note?: string;
  answered_by?: string;
  consumed_by?: string;
	outcome_summary?: string;
	execution_key?: string;
	execution_revision?: number;
	claimed_by?: string;
	claim_started_at?: string;
	completed_at?: string;
	failed_at?: string;
	failure_summary?: string;
  revision: number;
  answered_at?: string;
  consumed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface ReportWidgetResponsesResponse {
  success: boolean;
  responses: ReportWidgetResponse[];
  error?: string;
}

export interface ReportWidgetResponseResponse {
  success: boolean;
  response: ReportWidgetResponse;
  error?: string;
}

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
  builder_llm?: RunMetadataLLM;
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
  state: 'not_configured' | 'configured_not_verified' | 'running' | 'healthy' | 'partial' | 'failed' | string
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
  effective_state: 'not_configured' | 'local_only' | 'configured_not_verified' | 'running' | 'healthy' | 'partial' | 'failed' | string
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
  state: 'not_configured' | 'configured_not_verified' | 'publishing' | 'published' | 'failed' | string
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
  effective_state: 'not_configured' | 'configured_not_verified' | 'publishing' | 'published' | 'failed' | string
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

export type WorkflowNotificationState = 'not_configured' | 'missing_secret' | 'invalid_secret' | 'ready'

export interface WorkflowNotificationDestinationInfo {
  id: string
  type: string
  label: string
  state: WorkflowNotificationState
  secret_name?: string
  summary?: string
}

/** One configured Gmail sending account. Identifiers and labels only —
 *  the API never returns tokens, secrets, or credential file contents. */
export interface GmailConnection {
  id: string
  display_name: string
  email?: string
  config_home?: string
  has_credentials_file?: boolean
  /** The named OAuth client (see GmailOAuthClient) this connection
   *  authorizes under. Empty only for a connection that predates the
   *  named-client registry and has not yet been migrated. */
  client_name?: string
  status?: string
  enabled: boolean
  is_default: boolean
  auth: GmailAuthStatus
  ready: boolean
  created_at?: string
  updated_at?: string
}

export interface GmailConnectionsResponse {
  connections: GmailConnection[]
  /** Empty means no default is set — sends that name no connection will fail. */
  default_connection_id?: string
}

export interface GmailConnectionRequest {
  display_name?: string
  config_home?: string
  credentials_file?: string
  /** Required on create — which named OAuth client this connection
   *  authorizes under. */
  client_name?: string
  enabled?: boolean
}

/** One named Google Cloud OAuth app registration. Each Gmail connection
 *  authorizes under exactly one of these — naming them prevents a second
 *  upload from silently replacing the credentials an existing connection
 *  depends on. The client secret itself is never returned. */
export interface GmailOAuthClient {
  name: string
  client_id?: string
  created_at?: string
  updated_at?: string
}

export interface GmailOAuthClientsResponse {
  clients: GmailOAuthClient[]
}

export interface WorkflowNotificationAccountChannelInfo {
  id: string
  label: string
  state: 'ready' | 'not_ready' | 'checking' | string
  default_recipient?: string
  summary?: string
  blocked_recipients?: string[]
  /** Gmail authorization is still resolving in the background. */
  checking?: boolean
  /**
   * The address mail goes OUT from — the counterpart to default_recipient,
   * which is where it goes TO. Empty while auth is still resolving.
   */
  default_sender?: string
  /** Which configured connection default_sender belongs to. */
  default_sender_connection_id?: string
  /**
   * Connections a workflow may send from. Fewer than two entries means there
   * is nothing to choose and the picker stays hidden.
   */
  sender_choices?: WorkflowNotificationSenderChoice[]
}

/** One selectable Gmail sending account. Identifiers and labels only. */
export interface WorkflowNotificationSenderChoice {
  id: string
  display_name: string
  email?: string
  ready: boolean
  is_default?: boolean
}

export interface WorkflowNotificationInfoResponse {
  success: boolean
  agentic: boolean
  scope_label: string
  workflow_label?: string
  effective_state: WorkflowNotificationState
  destinations: WorkflowNotificationDestinationInfo[]
  account_channels: WorkflowNotificationAccountChannelInfo[]
  // Per-workflow content preferences from workflow.json notifications.
  run_summary_instructions?: string
  pulse_summary_instructions?: string
  run_summary_channels?: string[]
  pulse_summary_channels?: string[]
  // Who each summary is emailed to. Empty means the account default recipient.
  run_summary_recipients?: string[]
  /** Which Gmail account(s) each summary sends FROM. Several entries deliver
   *  the summary once per account; absent means inherit the default. */
  run_summary_gmail_connection_ids?: string[]
  pulse_summary_gmail_connection_ids?: string[]
  pulse_summary_recipients?: string[]
  // Slack channels per summary, as webhook secret names (one webhook = one channel).
  run_summary_slack_webhooks?: string[]
  pulse_summary_slack_webhooks?: string[]
  exclude_channels?: string[]
  block_recipients?: string[]
}

// Scheduled Jobs
export interface ScheduledJob {
  pulse_mode?: 'off' | 'basic' | 'full'
  pulse_mode_reason?: string
  id: string
  name: string
  description: string
  entity_type: 'workflow' | 'chat' | 'multi-agent' | 'product'
  preset_query_id?: string
  workspace_path?: string
  workflow_id?: string
  workflow_label?: string
  trigger_payload?: Record<string, unknown>
  group_names?: string[]  // undefined/empty = all groups
  // Deterministic route choices for a workflow schedule. The scheduler passes
  // these to the canonical full-workflow run; they are useful schedule context
  // in the history UI too.
  route_selections?: Record<string, string>
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
  last_status?: 'success' | 'error' | 'running' | 'partial' | 'stopped' | 'interrupted' | 'waiting_for_capacity' | 'waiting_for_workflow'
  last_error?: string
  last_duration_ms?: number
  run_count: number
  consecutive_failures: number
  execution_mode?: 'close_only'
  collision_policy?: 'skip' | 'queue_latest' | 'retry' | 'coalesce'
  max_start_delay_minutes?: number
  after_schedule_id?: string
  after_terminal_status?: 'completed' | 'any_terminal'
  after_delay_minutes?: number
  dependency_deadline?: string
  waiting_since?: string
  waiting_until?: string
  waiting_reason?: string
  queued_occurrences?: number
  missed_run_count?: number
  latest_missed_run_at?: string
  missed_run_reason?: string
  // Pulse schedules synthesize their run-specific messages from current
  // evidence; they intentionally do not persist ordinary messages[].
  pulse_review_only?: boolean
  created_at?: string
  updated_at?: string
  built_in?: boolean
  managed_by?: 'built-in' | 'slash-command' | string
}

export interface CreateScheduledJobRequest {
  pulse_mode?: 'off' | 'basic' | 'full'
  pulse_mode_reason?: string
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
  execution_mode?: 'close_only'
  collision_policy?: 'skip' | 'queue_latest' | 'retry' | 'coalesce'
  max_start_delay_minutes?: number
  after_schedule_id?: string
  after_terminal_status?: 'completed' | 'any_terminal'
  after_delay_minutes?: number
  dependency_deadline?: string
  schedule_type?: 'cron' | 'calendar'
  calendar_items?: CalendarScheduleItem[]
  cron_expression?: string
  timezone?: string
  enabled?: boolean
  pulse_review_only?: boolean
}

export interface UpdateScheduledJobRequest {
  pulse_mode?: 'off' | 'basic' | 'full'
  pulse_mode_reason?: string
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
  execution_mode?: 'close_only' | ''
  collision_policy?: 'skip' | 'queue_latest' | 'retry' | 'coalesce'
  max_start_delay_minutes?: number
  after_schedule_id?: string
  after_terminal_status?: 'completed' | 'any_terminal'
  after_delay_minutes?: number
  dependency_deadline?: string
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
  trigger_source?: 'manual' | 'cron' | 'calendar' | string
  scheduled_for?: string
  run_folder?: string
  session_id?: string
  status: 'running' | 'success' | 'error' | 'failed' | 'partial' | 'stopped' | 'interrupted' | 'waiting_for_capacity' | 'waiting_for_workflow'
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
  /** Who owns and who may read this workflow (workflow_access.go). */
  access?: { owners: string[]; readers: string[] }
  created_at?: string
  updated_at?: string
  run_retention_count?: number
  pulse?: WorkflowPulseConfig
  backup?: WorkflowBackupConfig
  folder_access?: WorkflowFolderGrant[]
  folder_access_requests?: WorkflowFolderAccessRequest[]
}

export interface WorkflowFolderGrant {
  id: string
  alias: string
  path: string
  access: 'read_only' | 'read_write'
  reason?: string
  created_at?: string
  updated_at?: string
}

export interface WorkflowFolderAccessRequest {
  id: string
  alias: string
  requested_path?: string
  access: 'read_only' | 'read_write'
  reason: string
  requested_at: string
}

export interface WorkflowPulseConfig {
  enabled?: boolean
  advisor_specialization?: WorkflowAdvisorSpecialization
}

export interface WorkflowAdvisorSpecialization {
  version: number
  strategy_auditor: string
  goal_advisor: string
  approved_input_id?: string
  updated_at?: string
}

export interface WorkflowCapabilities {
  selected_servers: string[]
  selected_tools: string[]
  selected_skills: string[]
  selected_secrets: string[]
  selected_global_secret_names: string[] | null // null = all, [] = none
  browser_mode: string
  cdp_ports?: number[]
  use_code_execution_mode: boolean
  llm_config?: PresetLLMConfig
  notifications?: WorkflowNotificationConfig
}

export interface WorkflowNotificationConfig {
  // Name of an encrypted workflow/user/global secret; never the webhook URL.
  slack_webhook_secret_name?: string
  // Legacy shared guidance; new saves use the scoped fields below.
  instructions?: string
  run_summary_instructions?: string
  pulse_summary_instructions?: string
  run_summary_channels?: string[]
  pulse_summary_channels?: string[]
}

export interface WorkflowExecutionDefaults {
  // Global step overrides (replaces step_override.json)
  disable_learning?: boolean
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
  pulse_review_only?: boolean
}

export interface DiscoveredWorkflow {
  workspace_path: string
  manifest: WorkflowManifest
  /** The signed-in user's level on this workflow: owner/write may edit and
   * share, read gets the read-only session. Absent on older servers. */
  my_access?: 'owner' | 'write' | 'read'
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
  pulse_enabled?: boolean
  run_notification_instructions?: string
  pulse_notification_instructions?: string
  run_notification_channels?: string[]
  pulse_notification_channels?: string[]
  // Send an empty array to clear a recipient list back to the account default;
  // omit the field to leave it unchanged.
  run_notification_recipients?: string[]
  pulse_notification_recipients?: string[]
  // Which Gmail account(s) each summary is sent FROM. Several entries fan the
  // summary out, one delivery per account. Send an empty array to clear back to
  // the account default; omit the field to leave it unchanged.
  run_notification_gmail_connection_ids?: string[]
  pulse_notification_gmail_connection_ids?: string[]
  notification_instructions?: string
  folder_access?: WorkflowFolderGrant[]
  folder_access_requests?: WorkflowFolderAccessRequest[]
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

export interface OrgDashboardNotificationField {
  label: string
  value: string
}

export interface OrgDashboardNotificationSection {
  heading: string
  body: string
}

export interface NotificationRouteSummary {
  routing_step_id: string
  route_id: string
  label?: string
  title: string
  status: OrgDashboardNotification['status']
  message: string
  fields?: OrgDashboardNotificationField[]
  sections?: OrgDashboardNotificationSection[]
}

export interface OrgDashboardRouteNotifications {
  routing_step_id?: string
  route_id: string
  label: string
  legacy?: boolean
  run_summary?: OrgDashboardNotification
  pulse_summary?: OrgDashboardNotification
}

export interface OrgDashboardNotification {
  id: string
  workspace_path: string
  kind: 'run_summary' | 'pulse_summary'
  title?: string
  status: 'completed' | 'failed' | 'blocked' | 'waiting_for_user' | 'waiting_for_platform' | 'monitoring' | 'informational' | 'no_run'
  route?: string
  message: string
  routes?: NotificationRouteSummary[]
  fields?: OrgDashboardNotificationField[]
  sections?: OrgDashboardNotificationSection[]
  created_at: string
}
