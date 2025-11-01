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
	WorkflowStatusPreVerification  = "pre-verification"
	WorkflowStatusPostVerification = "post-verification"
)

// Agent mode constants
const (
	AgentModeSimple       = "simple"
	AgentModeOrchestrator = "orchestrator"
	AgentModeWorkflow     = "workflow"
)

// ChatSession represents a chat session in the database
type ChatSession struct {
	ID            string     `json:"id" db:"id"`
	SessionID     string     `json:"session_id" db:"session_id"`
	Title         string     `json:"title" db:"title"`
	AgentMode     string     `json:"agent_mode" db:"agent_mode"`
	PresetQueryID *string    `json:"preset_query_id" db:"preset_query_id"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	CompletedAt   *time.Time `json:"completed_at" db:"completed_at"`
	Status        string     `json:"status" db:"status"`
	LastActivity  *time.Time `json:"last_activity" db:"last_activity"`
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
	ChatSessionID string     `json:"chat_session_id" db:"chat_session_id"`
	SessionID     string     `json:"session_id" db:"session_id"`
	Title         string     `json:"title" db:"title"`
	AgentMode     string     `json:"agent_mode" db:"agent_mode"`
	PresetQueryID string     `json:"preset_query_id" db:"preset_query_id"`
	Status        string     `json:"status" db:"status"`
	CreatedAt     time.Time  `json:"created_at" db:"created_at"`
	CompletedAt   *time.Time `json:"completed_at" db:"completed_at"`
	TotalEvents   int        `json:"total_events" db:"total_events"`
	TotalTurns    int        `json:"total_turns" db:"total_turns"`
	LastActivity  *time.Time `json:"last_activity" db:"last_activity"`
}

// CreateChatSessionRequest represents a request to create a new chat session
type CreateChatSessionRequest struct {
	SessionID     string `json:"session_id"`
	Title         string `json:"title,omitempty"`
	AgentMode     string `json:"agent_mode,omitempty"`
	PresetQueryID string `json:"preset_query_id,omitempty"`
}

// UpdateChatSessionRequest represents a request to update a chat session
type UpdateChatSessionRequest struct {
	Title         string     `json:"title,omitempty"`
	AgentMode     string     `json:"agent_mode,omitempty"`
	PresetQueryID string     `json:"preset_query_id,omitempty"`
	Status        string     `json:"status,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
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
type PresetLLMConfig struct {
	Provider string `json:"provider"` // openrouter, bedrock, openai, vertex
	ModelID  string `json:"model_id"`
}

// PresetQuery represents a preset query in the database
type PresetQuery struct {
	ID              string          `json:"id" db:"id"`
	Label           string          `json:"label" db:"label"`
	Query           string          `json:"query" db:"query"`
	SelectedServers string          `json:"selected_servers" db:"selected_servers"` // JSON array
	SelectedTools   string          `json:"selected_tools" db:"selected_tools"`     // JSON array of "server:tool" format
	SelectedFolder  sql.NullString  `json:"selected_folder" db:"selected_folder"`   // Single folder path
	AgentMode       string          `json:"agent_mode" db:"agent_mode"`             // Agent mode: simple, ReAct, orchestrator, workflow
	LLMConfig       json.RawMessage `json:"llm_config" db:"llm_config"`             // JSON configuration for LLM settings
	IsPredefined    bool            `json:"is_predefined" db:"is_predefined"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
	CreatedBy       string          `json:"created_by" db:"created_by"`
}

// MarshalJSON implements json.Marshaler for PresetQuery to handle sql.NullString properly
func (p PresetQuery) MarshalJSON() ([]byte, error) {
	result := struct {
		ID              string          `json:"id"`
		Label           string          `json:"label"`
		Query           string          `json:"query"`
		SelectedServers string          `json:"selected_servers"`
		SelectedTools   string          `json:"selected_tools"`
		SelectedFolder  *string         `json:"selected_folder,omitempty"`
		AgentMode       string          `json:"agent_mode"`
		LLMConfig       json.RawMessage `json:"llm_config"`
		IsPredefined    bool            `json:"is_predefined"`
		CreatedAt       time.Time       `json:"created_at"`
		UpdatedAt       time.Time       `json:"updated_at"`
		CreatedBy       string          `json:"created_by"`
	}{
		ID:              p.ID,
		Label:           p.Label,
		Query:           p.Query,
		SelectedServers: p.SelectedServers,
		SelectedTools:   p.SelectedTools,
		AgentMode:       p.AgentMode,
		LLMConfig:       p.LLMConfig,
		IsPredefined:    p.IsPredefined,
		CreatedAt:       p.CreatedAt,
		UpdatedAt:       p.UpdatedAt,
		CreatedBy:       p.CreatedBy,
	}

	// Convert sql.NullString to *string
	if p.SelectedFolder.Valid {
		result.SelectedFolder = &p.SelectedFolder.String
	}

	return json.Marshal(result)
}

// CreatePresetQueryRequest represents a request to create a new preset query
type CreatePresetQueryRequest struct {
	Label           string           `json:"label"`
	Query           string           `json:"query"`
	SelectedServers []string         `json:"selected_servers,omitempty"`
	SelectedTools   []string         `json:"selected_tools,omitempty"`  // Array of "server:tool" strings
	SelectedFolder  string           `json:"selected_folder,omitempty"` // Single folder path - required for orchestrator/workflow
	AgentMode       string           `json:"agent_mode,omitempty"`      // Agent mode: simple, ReAct, orchestrator, workflow
	LLMConfig       *PresetLLMConfig `json:"llm_config,omitempty"`      // LLM configuration for this preset
	IsPredefined    bool             `json:"is_predefined,omitempty"`
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
		if r.LLMConfig.ModelID == "" {
			return fmt.Errorf("model_id is required when llm_config is provided")
		}
		validProviders := []string{"openrouter", "bedrock", "openai", "vertex"}
		valid := false
		for _, provider := range validProviders {
			if r.LLMConfig.Provider == provider {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid provider: %s, must be one of: %v", r.LLMConfig.Provider, validProviders)
		}
	}

	return nil
}

// UpdatePresetQueryRequest represents a request to update a preset query
type UpdatePresetQueryRequest struct {
	Label           string           `json:"label,omitempty"`
	Query           string           `json:"query,omitempty"`
	SelectedServers []string         `json:"selected_servers,omitempty"`
	SelectedTools   []string         `json:"selected_tools,omitempty"`  // Array of "server:tool" strings
	SelectedFolder  string           `json:"selected_folder,omitempty"` // Single folder path - required for orchestrator/workflow
	AgentMode       string           `json:"agent_mode,omitempty"`      // Agent mode: simple, ReAct, orchestrator, workflow
	LLMConfig       *PresetLLMConfig `json:"llm_config,omitempty"`      // LLM configuration for this preset
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
		if r.LLMConfig.ModelID == "" {
			return fmt.Errorf("model_id is required when llm_config is provided")
		}
		validProviders := []string{"openrouter", "bedrock", "openai", "vertex"}
		valid := false
		for _, provider := range validProviders {
			if r.LLMConfig.Provider == provider {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid provider: %s, must be one of: %v", r.LLMConfig.Provider, validProviders)
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
	WorkflowStatus  string                   `json:"workflow_status,omitempty"`  // Optional, defaults to 'pre-verification'
	SelectedOptions *WorkflowSelectedOptions `json:"selected_options,omitempty"` // Optional, selected options for the phase
}

// UpdateWorkflowRequest represents a request to update a workflow
type UpdateWorkflowRequest struct {
	WorkflowStatus  *string                  `json:"workflow_status,omitempty"`
	SelectedOptions *WorkflowSelectedOptions `json:"selected_options,omitempty"`
}
