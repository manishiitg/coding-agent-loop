package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Workflow status constants
const (
	WorkflowStatusPreVerification  = "execution"
	WorkflowStatusPostVerification = "post-verification"
)

// Agent mode constants
const (
	AgentModeSimple       = "simple"
	AgentModeOrchestrator = "orchestrator"
	AgentModeWorkflow     = "workflow"
)

// ChatSessionConfig represents the configuration stored for a chat session
type ChatSessionConfig struct {
	SelectedServers            []string             `json:"selected_servers,omitempty"`
	EnabledServers             []string             `json:"enabled_servers,omitempty"`
	UseCodeExecutionMode       bool                 `json:"use_code_execution_mode,omitempty"`
	EnableContextSummarization bool                 `json:"enable_context_summarization,omitempty"`
	LLMConfig                  *LLMConfigForStorage `json:"llm_config,omitempty"`              // LLM config (without API keys)
	FileContext                []FileContextItem    `json:"file_context,omitempty"`            // Workspace files/folders
	EnableWorkspaceAccess      *bool                `json:"enable_workspace_access,omitempty"` // Workspace access setting
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
	Provider string `json:"provider,omitempty"` // openrouter, bedrock, openai, vertex, anthropic
	ModelID  string `json:"model_id,omitempty"`

	// New: Agent-specific default models (takes priority over legacy fields)
	ExecutionLLM  *AgentLLMConfig `json:"execution_llm,omitempty"`  // Default for execution agents
	ValidationLLM *AgentLLMConfig `json:"validation_llm,omitempty"` // Default for validation agents
	LearningLLM   *AgentLLMConfig `json:"learning_llm,omitempty"`   // Default for learning agents
	PhaseLLM      *AgentLLMConfig `json:"phase_llm,omitempty"`      // Default for all phase agents (planning, anonymization, plan improvement, etc.)
}

// AgentLLMConfig represents LLM configuration for a specific agent type
type AgentLLMConfig struct {
	Provider string `json:"provider"` // openrouter, bedrock, openai, vertex, anthropic
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
		IsPredefined:         p.IsPredefined,
		CreatedAt:            p.CreatedAt,
		UpdatedAt:            p.UpdatedAt,
		CreatedBy:            p.CreatedBy,
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
	IsPredefined         bool             `json:"is_predefined,omitempty"`
}

// validatePresetLLMConfig validates a PresetLLMConfig, accepting either legacy Provider+ModelID
// or at least one non-nil AgentLLMConfig with valid provider and model_id
func validatePresetLLMConfig(config *PresetLLMConfig) error {
	validProviders := []string{"openrouter", "bedrock", "openai", "vertex", "anthropic"}

	// Check if legacy config is provided
	hasLegacyConfig := config.Provider != "" && config.ModelID != ""

	// Validate legacy config if present
	if hasLegacyConfig {
		valid := false
		for _, provider := range validProviders {
			if config.Provider == provider {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid provider: %s, must be one of: %v", config.Provider, validProviders)
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
	}

	// Validate each non-nil AgentLLMConfig
	hasValidAgentConfig := false
	for _, agentConfig := range agentConfigs {
		if agentConfig.config != nil {
			// Validate model_id is non-empty
			if agentConfig.config.ModelID == "" {
				return fmt.Errorf("model_id is required for %s", agentConfig.name)
			}

			// Validate provider is in valid set
			valid := false
			for _, provider := range validProviders {
				if agentConfig.config.Provider == provider {
					valid = true
					break
				}
			}
			if !valid {
				return fmt.Errorf("invalid provider for %s: %s, must be one of: %v", agentConfig.name, agentConfig.config.Provider, validProviders)
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
	if r.Query == "" {
		return fmt.Errorf("query is required")
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
