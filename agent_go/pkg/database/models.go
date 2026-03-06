package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	llmproviders "github.com/manishiitg/multi-llm-provider-go"
)

// Workflow status constants
const (
	WorkflowStatusPreVerification  = "execution"
	WorkflowStatusPostVerification = "post-verification"
)

// Schedule entity type constants
const (
	ScheduleEntityWorkflow = "workflow"
	ScheduleEntityChat     = "chat"
)

// ScheduledJob represents a cron-scheduled job in the database
type ScheduledJob struct {
	ID                  string          `json:"id"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	EntityType          string          `json:"entity_type"`
	PresetQueryID       string          `json:"preset_query_id"`
	TriggerPayload      json.RawMessage `json:"trigger_payload,omitempty"`
	GroupIDs            []string        `json:"group_ids,omitempty"` // nil/empty = run all groups
	CronExpression      string          `json:"cron_expression"`
	Timezone            string          `json:"timezone"`
	Enabled             bool            `json:"enabled"`
	LastRunAt           *time.Time      `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time      `json:"next_run_at,omitempty"`
	LastSessionID       string          `json:"last_session_id,omitempty"`
	LastStatus          string          `json:"last_status,omitempty"`
	LastError           string          `json:"last_error,omitempty"`
	LastDurationMs      *int64          `json:"last_duration_ms,omitempty"`
	RunCount            int             `json:"run_count"`
	ConsecutiveFailures int             `json:"consecutive_failures"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// CreateScheduledJobRequest represents a request to create a new scheduled job
type CreateScheduledJobRequest struct {
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	EntityType     string          `json:"entity_type"`
	PresetQueryID  string          `json:"preset_query_id"`
	TriggerPayload json.RawMessage `json:"trigger_payload,omitempty"`
	GroupIDs       []string        `json:"group_ids,omitempty"` // nil/empty = all groups
	CronExpression string          `json:"cron_expression"`
	Timezone       string          `json:"timezone,omitempty"`
	Enabled        *bool           `json:"enabled,omitempty"`
}

// UpdateScheduledJobRequest represents a request to update a scheduled job
type UpdateScheduledJobRequest struct {
	Name           string          `json:"name,omitempty"`
	Description    string          `json:"description,omitempty"`
	TriggerPayload json.RawMessage `json:"trigger_payload,omitempty"`
	GroupIDs       []string        `json:"group_ids"`             // updated always (use SetGroupIDs flag)
	SetGroupIDs    bool            `json:"set_group_ids,omitempty"` // true = update group_ids (even if empty)
	CronExpression string          `json:"cron_expression,omitempty"`
	Timezone       string          `json:"timezone,omitempty"`
	Enabled        *bool           `json:"enabled,omitempty"`
}

// ScheduledJobRun represents a single execution of a scheduled job
type ScheduledJobRun struct {
	ID          string     `json:"id"`
	JobID       string     `json:"job_id"`
	RunFolder   string     `json:"run_folder,omitempty"`
	SessionID   string     `json:"session_id,omitempty"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	DurationMs  *int64     `json:"duration_ms,omitempty"`
	GroupIDs    []string   `json:"group_ids,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// ListScheduledJobsResponse represents the response for listing scheduled jobs
type ListScheduledJobsResponse struct {
	Jobs   []ScheduledJob `json:"jobs"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// Agent mode constants
const (
	AgentModeSimple       = "simple"
	AgentModeOrchestrator = "orchestrator"
	AgentModeWorkflow     = "workflow"
)

// WorkflowMetadata stores workflow-specific metadata for background/minimized workflows
// This is stored in the session config to enable querying and restoring background workflows
type WorkflowMetadata struct {
	PresetID         string          `json:"preset_id,omitempty"`           // Preset ID for context restoration
	PresetName       string          `json:"preset_name,omitempty"`         // Display name
	WorkspacePath    string          `json:"workspace_path,omitempty"`      // Workflow workspace path
	RunFolder        string          `json:"run_folder,omitempty"`          // Current run folder (e.g., "iteration-1")
	PhaseID          string          `json:"phase_id,omitempty"`            // Current phase ID (e.g., "execution")
	PhaseName        string          `json:"phase_name,omitempty"`          // Phase display name
	IsMinimized      bool            `json:"is_minimized,omitempty"`        // True when workflow is in background
	MinimizedAt      *int64          `json:"minimized_at,omitempty"`        // Unix timestamp (ms) when minimized
	StepProgress     json.RawMessage `json:"step_progress,omitempty"`       // Current step progress (StepProgress JSON)
	CurrentStepID    string          `json:"current_step_id,omitempty"`     // Currently executing step ID
	CurrentStepTitle string          `json:"current_step_title,omitempty"`  // Currently executing step title
	LastPolled       *int64          `json:"last_polled,omitempty"`         // Unix timestamp (ms) of last status check
}

// ChatSessionConfig represents the configuration stored for a chat session
type ChatSessionConfig struct {
	SelectedServers            []string             `json:"selected_servers,omitempty"`
	EnabledServers             []string             `json:"enabled_servers,omitempty"`
	UseCodeExecutionMode       bool                 `json:"use_code_execution_mode,omitempty"`
	EnableContextSummarization *bool                `json:"enable_context_summarization,omitempty"` // nil = inherit default, true/false = explicit override
	LLMConfig                  *LLMConfigForStorage `json:"llm_config,omitempty"`                   // LLM config (without API keys)
	FileContext                []FileContextItem    `json:"file_context,omitempty"`                 // Workspace files/folders
	EnableWorkspaceAccess      *bool                `json:"enable_workspace_access,omitempty"`      // Workspace access setting
	WorkflowMetadata           *WorkflowMetadata    `json:"workflow_metadata,omitempty"`            // Workflow-specific metadata (for background workflows)
	SelectedSkills             []string             `json:"selected_skills,omitempty"`              // Selected skill folder names
	SelectedSubAgents          []string             `json:"selected_subagents,omitempty"`           // Selected sub-agent template folder names
	DelegationMode             string               `json:"delegation_mode,omitempty"`              // Delegation mode: "spawn" or "plan"
	DelegationTierConfig       json.RawMessage      `json:"delegation_tier_config,omitempty"`       // Delegation tier config (stored as-is)
	PlanID                     string               `json:"plan_id,omitempty"`                      // Active plan ID (for session resume)
	PlanFolder                 string               `json:"plan_folder,omitempty"`                  // Active plan folder path
	PlanPhase                  string               `json:"plan_phase,omitempty"`                   // Plan phase: "planning" or "execution"
}

// LLMConfigForStorage stores LLM config without sensitive API keys
type LLMConfigForStorage struct {
	Provider       string                 `json:"provider,omitempty"`
	ModelID        string                 `json:"model_id,omitempty"`
	FallbackModels []string               `json:"fallback_models,omitempty"`
	CrossProvider  *CrossProviderFallback `json:"cross_provider_fallback,omitempty"`
}

// CrossProviderFallback represents cross-provider fallback configuration
type CrossProviderFallback struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}

// FileContextItem represents a file or folder in workspace context
type FileContextItem struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"` // "file" or "folder"
}

// ToJSON converts ChatSessionConfig to json.RawMessage for database storage
func (c *ChatSessionConfig) ToJSON() (json.RawMessage, error) {
	if c == nil {
		return nil, nil
	}
	data, err := json.Marshal(c)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}
	return json.RawMessage(data), nil
}

// ChatSessionConfigFromJSON converts json.RawMessage to ChatSessionConfig
func ChatSessionConfigFromJSON(data json.RawMessage) (*ChatSessionConfig, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var config ChatSessionConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return &config, nil
}

// GetConfig returns the ChatSessionConfig from a ChatSession, or nil if not set
func (s *ChatSession) GetConfig() (*ChatSessionConfig, error) {
	return ChatSessionConfigFromJSON(s.Config)
}

// ChatSession represents a chat session in the database
type ChatSession struct {
	ID            string          `json:"id" db:"id"`
	SessionID     string          `json:"session_id" db:"session_id"`
	Title         string          `json:"title" db:"title"`
	AgentMode     string          `json:"agent_mode" db:"agent_mode"`
	PresetQueryID *string         `json:"preset_query_id" db:"preset_query_id"`
	Config        json.RawMessage `json:"config" db:"config"` // JSON configuration
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	CompletedAt   *time.Time      `json:"completed_at" db:"completed_at"`
	Status        string          `json:"status" db:"status"`
	LastActivity  *time.Time      `json:"last_activity" db:"last_activity"`
}

// Event represents a stored event in the database
type Event struct {
	ID            string          `json:"id" db:"id"`
	SessionID     string          `json:"session_id" db:"session_id"`
	ChatSessionID string          `json:"chat_session_id" db:"chat_session_id"`
	EventType     string          `json:"event_type" db:"event_type"`
	Timestamp     time.Time       `json:"timestamp" db:"timestamp"`
	EventData     json.RawMessage `json:"event_data" db:"event_data"`
}

// ChatHistorySummary represents a summary view of chat history
type ChatHistorySummary struct {
	ChatSessionID string          `json:"chat_session_id" db:"chat_session_id"`
	SessionID     string          `json:"session_id" db:"session_id"`
	Title         string          `json:"title" db:"title"`
	AgentMode     string          `json:"agent_mode" db:"agent_mode"`
	PresetQueryID string          `json:"preset_query_id" db:"preset_query_id"`
	Config        json.RawMessage `json:"config" db:"config"` // JSON configuration
	Status        string          `json:"status" db:"status"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	CompletedAt   *time.Time      `json:"completed_at" db:"completed_at"`
	TotalEvents   int             `json:"total_events" db:"total_events"`
	TotalTurns    int             `json:"total_turns" db:"total_turns"`
	LastActivity  *time.Time      `json:"last_activity" db:"last_activity"`
}

// CreateChatSessionRequest represents a request to create a new chat session
type CreateChatSessionRequest struct {
	SessionID     string          `json:"session_id"`
	Title         string          `json:"title,omitempty"`
	AgentMode     string          `json:"agent_mode,omitempty"`
	PresetQueryID string          `json:"preset_query_id,omitempty"`
	Config        json.RawMessage `json:"config,omitempty"` // JSON configuration
}

// UpdateChatSessionRequest represents a request to update a chat session
type UpdateChatSessionRequest struct {
	Title         string          `json:"title,omitempty"`
	AgentMode     string          `json:"agent_mode,omitempty"`
	PresetQueryID string          `json:"preset_query_id,omitempty"`
	Config        json.RawMessage `json:"config,omitempty"` // JSON configuration
	Status        string          `json:"status,omitempty"`
	CompletedAt   *time.Time      `json:"completed_at,omitempty"`
}

// GetChatHistoryRequest represents a request to get chat history
type GetChatHistoryRequest struct {
	SessionID     string    `json:"session_id,omitempty"`
	ChatSessionID uuid.UUID `json:"chat_session_id,omitempty"`
	Limit         int       `json:"limit,omitempty"`
	Offset        int       `json:"offset,omitempty"`
	EventType     string    `json:"event_type,omitempty"`
	FromDate      time.Time `json:"from_date,omitempty"`
	ToDate        time.Time `json:"to_date,omitempty"`
}

// GetChatHistoryResponse represents the response for getting chat history
type GetChatHistoryResponse struct {
	Sessions []ChatHistorySummary `json:"sessions"`
	Total    int                  `json:"total"`
	Limit    int                  `json:"limit"`
	Offset   int                  `json:"offset"`
}

// GetEventsResponse represents the response for getting events
type GetEventsResponse struct {
	Events []Event `json:"events"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

// PresetLLMConfig represents LLM configuration stored with presets
// Supports both legacy single default model and new agent-specific defaults
type PresetLLMConfig struct {
	// Legacy: Single default model (for backward compatibility)
	Provider string `json:"provider,omitempty"` // openrouter, bedrock, openai, vertex, anthropic, azure
	ModelID  string `json:"model_id,omitempty"`

	// New: Agent-specific default models (takes priority over legacy fields)
	ExecutionLLM  *AgentLLMConfig `json:"execution_llm,omitempty"`  // Default for execution agents
	ValidationLLM *AgentLLMConfig `json:"validation_llm,omitempty"` // Default for validation agents
	LearningLLM   *AgentLLMConfig `json:"learning_llm,omitempty"`   // Default for learning agents
	PhaseLLM      *AgentLLMConfig `json:"phase_llm,omitempty"`      // Default for all phase agents (planning, anonymization, plan improvement, etc.)

	// Feature toggles
	UseKnowledgebase          *bool `json:"use_knowledgebase,omitempty"`           // nil/true = enabled (default), false = disabled - controls knowledgebase folder creation and prompt references
	EnableContextSummarization *bool `json:"enable_context_summarization,omitempty"` // nil/true = enabled (default), false = disabled
	EnableContextEditing       *bool `json:"enable_context_editing,omitempty"`       // nil/false = disabled (default), true = enabled

	// Tiered LLM allocation mode
	LLMAllocationMode string              `json:"llm_allocation_mode,omitempty"` // "manual" (default) or "tiered"
	TieredConfig      *TieredLLMConfig    `json:"tiered_config,omitempty"`
}

// TieredLLMConfig represents the 3-tier LLM configuration for tiered allocation mode
type TieredLLMConfig struct {
	Tier1 *AgentLLMConfig `json:"tier_1"` // High reasoning
	Tier2 *AgentLLMConfig `json:"tier_2"` // Medium reasoning
	Tier3 *AgentLLMConfig `json:"tier_3"` // Low reasoning
}

// AgentLLMConfig represents LLM configuration for a specific agent type
type AgentLLMConfig struct {
	Provider string `json:"provider"` // openrouter, bedrock, openai, vertex, anthropic, azure
	ModelID  string `json:"model_id"`
}

// PresetQuery represents a preset query in the database
type PresetQuery struct {
	ID                   string          `json:"id" db:"id"`
	Label                string          `json:"label" db:"label"`
	Query                string          `json:"query" db:"query"`
	SelectedServers      string          `json:"selected_servers" db:"selected_servers"`               // JSON array
	SelectedTools        string          `json:"selected_tools" db:"selected_tools"`                   // JSON array of "server:tool" format
	SelectedFolder       sql.NullString  `json:"selected_folder" db:"selected_folder"`                 // Single folder path
	AgentMode            string          `json:"agent_mode" db:"agent_mode"`                           // Agent mode: simple, ReAct, orchestrator, workflow
	LLMConfig            json.RawMessage `json:"llm_config" db:"llm_config"`                           // JSON configuration for LLM settings
	UseCodeExecutionMode bool            `json:"use_code_execution_mode" db:"use_code_execution_mode"` // MCP code execution mode
	UseToolSearchMode    bool            `json:"use_tool_search_mode" db:"use_tool_search_mode"`       // Tool search mode
	PreDiscoveredTools   string          `json:"pre_discovered_tools" db:"pre_discovered_tools"`       // JSON array of pre-discovered tools
	SelectedSkills       string          `json:"selected_skills" db:"selected_skills"`                 // JSON array of skill folder names
	SelectedSecrets              string          `json:"selected_secrets" db:"selected_secrets"`                                       // JSON array of secret IDs
	SelectedGlobalSecretNames    string          `json:"selected_global_secret_names" db:"selected_global_secret_names"`               // JSON array of global secret names (NULL=all)
	EnableBrowserAccess          bool            `json:"enable_browser_access" db:"enable_browser_access"`                             // Browser automation access
	IsPredefined         bool            `json:"is_predefined" db:"is_predefined"`
	CreatedAt            time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at" db:"updated_at"`
	CreatedBy            string          `json:"created_by" db:"created_by"`
}

// MarshalJSON implements json.Marshaler for PresetQuery to handle sql.NullString properly
func (p PresetQuery) MarshalJSON() ([]byte, error) {
	result := struct {
		ID                   string          `json:"id"`
		Label                string          `json:"label"`
		Query                string          `json:"query"`
		SelectedServers      string          `json:"selected_servers"`
		SelectedTools        string          `json:"selected_tools"`
		SelectedFolder       *string         `json:"selected_folder,omitempty"`
		AgentMode            string          `json:"agent_mode"`
		LLMConfig            json.RawMessage `json:"llm_config"`
		UseCodeExecutionMode bool            `json:"use_code_execution_mode"`
		UseToolSearchMode    bool            `json:"use_tool_search_mode"`
		PreDiscoveredTools   string          `json:"pre_discovered_tools"`
		SelectedSkills       string          `json:"selected_skills"`
		SelectedSecrets           string          `json:"selected_secrets"`
		SelectedGlobalSecretNames *string         `json:"selected_global_secret_names,omitempty"`
		EnableBrowserAccess       bool            `json:"enable_browser_access"`
		IsPredefined         bool            `json:"is_predefined"`
		CreatedAt            time.Time       `json:"created_at"`
		UpdatedAt            time.Time       `json:"updated_at"`
		CreatedBy            string          `json:"created_by"`
	}{
		ID:                   p.ID,
		Label:                p.Label,
		Query:                p.Query,
		SelectedServers:      p.SelectedServers,
		SelectedTools:        p.SelectedTools,
		AgentMode:            p.AgentMode,
		LLMConfig:            p.LLMConfig,
		UseCodeExecutionMode: p.UseCodeExecutionMode,
		UseToolSearchMode:    p.UseToolSearchMode,
		PreDiscoveredTools:   p.PreDiscoveredTools,
		SelectedSkills:       p.SelectedSkills,
		SelectedSecrets:      p.SelectedSecrets,
		EnableBrowserAccess:  p.EnableBrowserAccess,
		IsPredefined:         p.IsPredefined,
		CreatedAt:            p.CreatedAt,
		UpdatedAt:            p.UpdatedAt,
		CreatedBy:            p.CreatedBy,
	}

	// Convert SelectedGlobalSecretNames: empty string or "null" means nil (all selected)
	if p.SelectedGlobalSecretNames != "" && p.SelectedGlobalSecretNames != "null" {
		result.SelectedGlobalSecretNames = &p.SelectedGlobalSecretNames
	}

	// Convert sql.NullString to *string
	if p.SelectedFolder.Valid {
		result.SelectedFolder = &p.SelectedFolder.String
	}

	return json.Marshal(result)
}

// CreatePresetQueryRequest represents a request to create a new preset query
type CreatePresetQueryRequest struct {
	Label                string           `json:"label"`
	Query                string           `json:"query"`
	SelectedServers      []string         `json:"selected_servers,omitempty"`
	SelectedTools        []string         `json:"selected_tools,omitempty"`          // Array of "server:tool" strings
	SelectedFolder       string           `json:"selected_folder,omitempty"`         // Single folder path - required for orchestrator/workflow
	AgentMode            string           `json:"agent_mode,omitempty"`              // Agent mode: simple, ReAct, orchestrator, workflow
	LLMConfig            *PresetLLMConfig `json:"llm_config,omitempty"`              // LLM configuration for this preset
	UseCodeExecutionMode bool             `json:"use_code_execution_mode,omitempty"` // MCP code execution mode
	UseToolSearchMode    bool             `json:"use_tool_search_mode,omitempty"`    // Tool search mode
	PreDiscoveredTools   []string         `json:"pre_discovered_tools,omitempty"`    // Tools always available without searching
	SelectedSkills       []string         `json:"selected_skills,omitempty"`         // Skill folder names for workflow
	SelectedSecrets           []string         `json:"selected_secrets,omitempty"`                  // Secret IDs for workflow
	SelectedGlobalSecretNames *[]string        `json:"selected_global_secret_names,omitempty"`      // Global secret names (nil=all, []=none)
	EnableBrowserAccess       bool             `json:"enable_browser_access,omitempty"`              // Browser automation access
	IsPredefined              bool             `json:"is_predefined,omitempty"`
}

// validatePresetLLMConfig validates a PresetLLMConfig, accepting either legacy Provider+ModelID
// or at least one non-nil AgentLLMConfig with valid provider and model_id
func validatePresetLLMConfig(config *PresetLLMConfig) error {
	// Tiered mode validation: validate tier configs instead of agent-specific configs
	if config.LLMAllocationMode == "tiered" {
		if config.TieredConfig == nil {
			return fmt.Errorf("tiered_config is required when llm_allocation_mode is 'tiered'")
		}
		tierConfigs := []struct {
			config *AgentLLMConfig
			name   string
		}{
			{config.TieredConfig.Tier1, "tier_1"},
			{config.TieredConfig.Tier2, "tier_2"},
			{config.TieredConfig.Tier3, "tier_3"},
		}
		for _, tierConfig := range tierConfigs {
			if tierConfig.config == nil {
				return fmt.Errorf("%s is required in tiered_config", tierConfig.name)
			}
			if tierConfig.config.ModelID == "" {
				return fmt.Errorf("model_id is required for %s", tierConfig.name)
			}
			if tierConfig.config.Provider == "" {
				return fmt.Errorf("provider is required for %s", tierConfig.name)
			}
			if _, err := llmproviders.ValidateProvider(tierConfig.config.Provider); err != nil {
				return fmt.Errorf("invalid provider for %s: %w", tierConfig.name, err)
			}
		}
		return nil
	}

	// Manual mode validation (default)
	// Check if legacy config is provided
	hasLegacyConfig := config.Provider != "" && config.ModelID != ""

	// Validate legacy config if present
	if hasLegacyConfig {
		if _, err := llmproviders.ValidateProvider(config.Provider); err != nil {
			return fmt.Errorf("invalid provider: %w", err)
		}
	}

	// Collect all AgentLLMConfig fields
	agentConfigs := []struct {
		config *AgentLLMConfig
		name   string
	}{
		{config.ExecutionLLM, "execution_llm"},
		{config.ValidationLLM, "validation_llm"},
		{config.LearningLLM, "learning_llm"},
		{config.PhaseLLM, "phase_llm"},
	}

	// Validate each non-nil AgentLLMConfig
	hasValidAgentConfig := false
	for _, agentConfig := range agentConfigs {
		if agentConfig.config != nil {
			// Validate model_id is non-empty
			if agentConfig.config.ModelID == "" {
				return fmt.Errorf("model_id is required for %s", agentConfig.name)
			}

			// Validate provider using centralized validation
			if _, err := llmproviders.ValidateProvider(agentConfig.config.Provider); err != nil {
				return fmt.Errorf("invalid provider for %s: %w", agentConfig.name, err)
			}

			hasValidAgentConfig = true
		}
	}

	// Ensure either legacy config OR at least one valid agent config is present
	if !hasLegacyConfig && !hasValidAgentConfig {
		return fmt.Errorf("llm_config must have either legacy provider+model_id or at least one non-nil agent-specific config with valid provider and model_id")
	}

	return nil
}

// Validate validates the CreatePresetQueryRequest
func (r *CreatePresetQueryRequest) Validate() error {
	// Validate required fields
	if r.Label == "" {
		return fmt.Errorf("label is required")
	}
	// Query is only required for non-workflow presets
	if r.Query == "" && r.AgentMode != AgentModeWorkflow {
		return fmt.Errorf("query is required for non-workflow presets")
	}

	// Validate agent mode
	if r.AgentMode != "" {
		validModes := []string{AgentModeSimple, AgentModeOrchestrator, AgentModeWorkflow}
		valid := false
		for _, mode := range validModes {
			if r.AgentMode == mode {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid agent mode: %s, must be one of: %v", r.AgentMode, validModes)
		}
	}

	// Validate selected folder is required for orchestrator/workflow modes
	if r.AgentMode == AgentModeOrchestrator || r.AgentMode == AgentModeWorkflow {
		if r.SelectedFolder == "" {
			return fmt.Errorf("selected_folder is required for agent mode: %s", r.AgentMode)
		}
	}

	// Validate LLM config
	if r.LLMConfig != nil {
		if err := validatePresetLLMConfig(r.LLMConfig); err != nil {
			return err
		}
	}

	return nil
}

// UpdatePresetQueryRequest represents a request to update a preset query
type UpdatePresetQueryRequest struct {
	Label                string           `json:"label,omitempty"`
	Query                string           `json:"query,omitempty"`
	SelectedServers      []string         `json:"selected_servers,omitempty"`
	SelectedTools        []string         `json:"selected_tools,omitempty"`          // Array of "server:tool" strings
	SelectedFolder       string           `json:"selected_folder,omitempty"`         // Single folder path - required for orchestrator/workflow
	AgentMode            string           `json:"agent_mode,omitempty"`              // Agent mode: simple, ReAct, orchestrator, workflow
	LLMConfig            *PresetLLMConfig `json:"llm_config,omitempty"`              // LLM configuration for this preset
	UseCodeExecutionMode *bool            `json:"use_code_execution_mode,omitempty"` // MCP code execution mode (pointer to allow false value)
	UseToolSearchMode    *bool            `json:"use_tool_search_mode,omitempty"`    // Tool search mode (pointer to allow false value)
	PreDiscoveredTools   []string         `json:"pre_discovered_tools,omitempty"`    // Tools always available without searching
	SelectedSkills       []string         `json:"selected_skills,omitempty"`         // Skill folder names for workflow
	SelectedSecrets           []string         `json:"selected_secrets,omitempty"`                  // Secret names for workflow
	SelectedGlobalSecretNames *[]string        `json:"selected_global_secret_names,omitempty"`      // Global secret names (nil=all, []=none)
	EnableBrowserAccess       *bool            `json:"enable_browser_access,omitempty"`              // Browser automation access (pointer to allow false value)
}

// Validate validates the UpdatePresetQueryRequest
func (r *UpdatePresetQueryRequest) Validate() error {
	// Validate agent mode if provided
	if r.AgentMode != "" {
		validModes := []string{AgentModeSimple, AgentModeOrchestrator, AgentModeWorkflow}
		valid := false
		for _, mode := range validModes {
			if r.AgentMode == mode {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid agent mode: %s, must be one of: %v", r.AgentMode, validModes)
		}
	}

	// Validate selected folder is required for orchestrator/workflow modes
	if r.AgentMode == AgentModeOrchestrator || r.AgentMode == AgentModeWorkflow {
		if r.SelectedFolder == "" {
			return fmt.Errorf("selected_folder is required for agent mode: %s", r.AgentMode)
		}
	}

	// Validate LLM config if provided
	if r.LLMConfig != nil {
		if err := validatePresetLLMConfig(r.LLMConfig); err != nil {
			return err
		}
	}

	return nil
}

// ListPresetQueriesResponse represents the response for listing preset queries
type ListPresetQueriesResponse struct {
	Presets []PresetQuery `json:"presets"`
	Total   int           `json:"total"`
	Limit   int           `json:"limit"`
	Offset  int           `json:"offset"`
}

// WorkflowSelectedOption represents a selected option for a workflow phase
type WorkflowSelectedOption struct {
	OptionID    string `json:"option_id"`    // The option ID (e.g., "use_same_run")
	OptionLabel string `json:"option_label"` // Human-readable label (e.g., "Use Same Run")
	OptionValue string `json:"option_value"` // The actual value to use
	Group       string `json:"group"`        // The group this option belongs to (e.g., "run_management")
	PhaseID     string `json:"phase_id"`     // Which phase this option belongs to
}

// WorkflowSelectedOptions represents all selected options for a workflow phase (multiple groups)
type WorkflowSelectedOptions struct {
	PhaseID    string                   `json:"phase_id"`   // Which phase these options belong to
	Selections []WorkflowSelectedOption `json:"selections"` // All selected options across groups
}

// Workflow represents a workflow state for todo-list-based execution
type Workflow struct {
	ID              string                   `json:"id" db:"id"`
	PresetQueryID   string                   `json:"preset_query_id" db:"preset_query_id"`
	WorkflowStatus  string                   `json:"workflow_status" db:"workflow_status"`
	SelectedOptions *WorkflowSelectedOptions `json:"selected_options" db:"selected_options"` // Store selected options as JSON
	CreatedAt       time.Time                `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at" db:"updated_at"`
}

// CreateWorkflowRequest represents a request to create a new workflow
type CreateWorkflowRequest struct {
	PresetQueryID   string                   `json:"preset_query_id"`
	WorkflowStatus  string                   `json:"workflow_status,omitempty"`  // Optional, defaults to 'execution'
	SelectedOptions *WorkflowSelectedOptions `json:"selected_options,omitempty"` // Optional, selected options for the phase
}

// UpdateWorkflowRequest represents a request to update a workflow
type UpdateWorkflowRequest struct {
	WorkflowStatus  *string                  `json:"workflow_status,omitempty"`
	SelectedOptions *WorkflowSelectedOptions `json:"selected_options,omitempty"`
}

// AppUser represents a workspace user (populated on login for email→userID lookup)
type AppUser struct {
	UserID    string    `json:"user_id" db:"user_id"`
	Email     string    `json:"email" db:"email"`
	Username  string    `json:"username" db:"username"`
	Provider  string    `json:"provider" db:"provider"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// UserSecret represents a user-owned secret stored server-side for bot session access
type UserSecret struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	Name           string    `json:"name"`
	EncryptedValue string    `json:"encrypted_value"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Bot session status constants
const (
	BotSessionStatusAwaitingPlanApproval = "awaiting_plan_approval"
	BotSessionStatusRunning              = "running"
	BotSessionStatusCompleted            = "completed"
	BotSessionStatusFailed               = "failed"
)

// BotConnectorConfig represents configuration for a bot connector platform
type BotConnectorConfig struct {
	ID              string    `json:"id" db:"id"`                             // "slack", "discord", etc.
	Enabled         bool      `json:"enabled" db:"enabled"`
	BotMode         bool      `json:"bot_mode" db:"bot_mode"`                // full bot vs notification-only
	ConfigJSON      string    `json:"config_json" db:"config_json"`          // platform-specific config
	DefaultPresetID string    `json:"default_preset_id" db:"default_preset_id"`
	AutoConfirm     bool      `json:"auto_confirm" db:"auto_confirm"`
	AllowedChannels string    `json:"allowed_channels" db:"allowed_channels"` // JSON array
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// BotSession represents a bot conversation session tied to a platform thread
type BotSession struct {
	ID            string     `json:"id" db:"id"`
	Platform      string     `json:"platform" db:"platform"`
	ChannelID     string     `json:"channel_id" db:"channel_id"`
	ThreadTS      string     `json:"thread_ts" db:"thread_ts"`
	SessionID     string     `json:"session_id" db:"session_id"`         // internal chat session ID
	UserID        string     `json:"user_id" db:"user_id"`               // platform user ID
	UserName      string     `json:"user_name" db:"user_name"`
	Query         string     `json:"query" db:"query"`
	Status        string     `json:"status" db:"status"`
	PresetID      string     `json:"preset_id" db:"preset_id"`
	ConfigJSON    string     `json:"config_json" db:"config_json"`       // final QueryRequest config
	ThreadContext string     `json:"thread_context" db:"thread_context"` // JSON of thread history
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at" db:"updated_at"`
	CompletedAt   *time.Time `json:"completed_at" db:"completed_at"`
}

// BotMessage represents a message in a bot session (audit trail)
type BotMessage struct {
	ID                string    `json:"id" db:"id"`
	BotSessionID      string    `json:"bot_session_id" db:"bot_session_id"`
	Direction         string    `json:"direction" db:"direction"`                   // "incoming" / "outgoing"
	MessageType       string    `json:"message_type" db:"message_type"`             // "user_request", "analysis", "confirmation", "progress", "result", "human_feedback"
	Content           string    `json:"content" db:"content"`
	PlatformMessageID string    `json:"platform_message_id" db:"platform_message_id"` // for message updates
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
}

// CreateBotConnectorConfigRequest for creating/updating a bot connector config
type CreateBotConnectorConfigRequest struct {
	ID              string `json:"id"`
	Enabled         bool   `json:"enabled"`
	BotMode         bool   `json:"bot_mode"`
	ConfigJSON      string `json:"config_json,omitempty"`
	DefaultPresetID string `json:"default_preset_id,omitempty"`
	AutoConfirm     bool   `json:"auto_confirm"`
	AllowedChannels string `json:"allowed_channels,omitempty"`
}

// CreateBotSessionRequest for creating a new bot session
type CreateBotSessionRequest struct {
	Platform      string `json:"platform"`
	ChannelID     string `json:"channel_id"`
	ThreadTS      string `json:"thread_ts"`
	UserID        string `json:"user_id"`
	UserName      string `json:"user_name,omitempty"`
	Query         string `json:"query"`
	ThreadContext string `json:"thread_context,omitempty"`
}

// UpdateBotSessionRequest for updating a bot session
type UpdateBotSessionRequest struct {
	SessionID    string `json:"session_id,omitempty"`
	Status       string `json:"status,omitempty"`
	PresetID   string `json:"preset_id,omitempty"`
	ConfigJSON   string `json:"config_json,omitempty"`
}

// CreateBotMessageRequest for creating a bot message
type CreateBotMessageRequest struct {
	BotSessionID      string `json:"bot_session_id"`
	Direction         string `json:"direction"`
	MessageType       string `json:"message_type"`
	Content           string `json:"content,omitempty"`
	PlatformMessageID string `json:"platform_message_id,omitempty"`
}
