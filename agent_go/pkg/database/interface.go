package database

import (
	"context"
	"time"

	"mcpagent/events"
)

// Database interface for chat history storage
type Database interface {
	// Chat session management
	CreateChatSession(ctx context.Context, req *CreateChatSessionRequest) (*ChatSession, error)
	GetChatSession(ctx context.Context, sessionID string) (*ChatSession, error)
	UpdateChatSession(ctx context.Context, sessionID string, req *UpdateChatSessionRequest) (*ChatSession, error)
	DeleteChatSession(ctx context.Context, sessionID string) error
	ListChatSessions(ctx context.Context, limit, offset int, presetQueryID *string) ([]ChatHistorySummary, int, error)

	// Event storage
	StoreEvent(ctx context.Context, sessionID string, event *events.AgentEvent) error
	GetEvents(ctx context.Context, req *GetChatHistoryRequest) (*GetEventsResponse, error)
	GetEventsBySession(ctx context.Context, sessionID string, limit, offset int) ([]Event, error)

	// Preset query management
	CreatePresetQuery(ctx context.Context, req *CreatePresetQueryRequest) (*PresetQuery, error)
	GetPresetQuery(ctx context.Context, id string) (*PresetQuery, error)
	UpdatePresetQuery(ctx context.Context, id string, req *UpdatePresetQueryRequest) (*PresetQuery, error)
	DeletePresetQuery(ctx context.Context, id string) error
	ListPresetQueries(ctx context.Context, limit, offset int) ([]PresetQuery, int, error)

	// Workflow management
	CreateWorkflow(ctx context.Context, req *CreateWorkflowRequest) (*Workflow, error)
	GetWorkflowByPresetQueryID(ctx context.Context, presetQueryID string) (*Workflow, error)
	UpdateWorkflow(ctx context.Context, presetQueryID string, req *UpdateWorkflowRequest) (*Workflow, error)
	DeleteWorkflow(ctx context.Context, presetQueryID string) error

	// Health check
	Ping(ctx context.Context) error
	Close() error
}

// EventStore interface for integrating with existing event system
type EventStore interface {
	// Store events from the unified events system
	StoreAgentEvent(ctx context.Context, sessionID string, event *events.AgentEvent) error

	// Get events for a session
	GetSessionEvents(ctx context.Context, sessionID string) ([]*events.AgentEvent, error)

	// Get events by type
	GetEventsByType(ctx context.Context, sessionID string, eventType events.EventType) ([]*events.AgentEvent, error)
}

// EventFilter represents filtering options for events
type EventFilter struct {
	SessionID      string           `json:"session_id,omitempty"`
	EventType      events.EventType `json:"event_type,omitempty"`
	Component      string           `json:"component,omitempty"`
	FromDate       time.Time        `json:"from_date,omitempty"`
	ToDate         time.Time        `json:"to_date,omitempty"`
	HierarchyLevel int              `json:"hierarchy_level,omitempty"`
	Limit          int              `json:"limit,omitempty"`
	Offset         int              `json:"offset,omitempty"`
}

// EventSearchResult represents the result of an event search
type EventSearchResult struct {
	Events []Event `json:"events"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

// ChatHistoryServiceInterface provides high-level chat history operations
type ChatHistoryServiceInterface interface {
	// Start a new chat session
	StartChatSession(ctx context.Context, sessionID, title string) (*ChatSession, error)

	// End a chat session
	EndChatSession(ctx context.Context, sessionID string, status string) error

	// List all chat sessions
	ListChatSessions(ctx context.Context, limit, offset int, presetQueryID *string) ([]ChatHistorySummary, int, error)

	// Search events
	SearchEvents(ctx context.Context, filter *EventFilter) (*EventSearchResult, error)
}
