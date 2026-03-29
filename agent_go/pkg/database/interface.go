package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/manishiitg/mcpagent/events"
)

// Database interface for chat history storage
type Database interface {
	// Chat session management
	// userID parameter is used for session isolation in multi-user mode
	// Pass empty string to skip user filtering (for single-user mode)
	CreateChatSession(ctx context.Context, req *CreateChatSessionRequest) (*ChatSession, error)
	CreateChatSessionWithUser(ctx context.Context, req *CreateChatSessionRequest, userID string) (*ChatSession, error)
	GetChatSession(ctx context.Context, sessionID string) (*ChatSession, error)
	GetChatSessionWithUser(ctx context.Context, sessionID string, userID string) (*ChatSession, error)
	UpdateChatSession(ctx context.Context, sessionID string, req *UpdateChatSessionRequest) (*ChatSession, error)
	DeleteChatSession(ctx context.Context, sessionID string) error
	ListChatSessions(ctx context.Context, limit, offset int, presetQueryID *string, agentMode *string) ([]ChatHistorySummary, int, error)
	ListChatSessionsWithUser(ctx context.Context, limit, offset int, presetQueryID *string, agentMode *string, userID string) ([]ChatHistorySummary, int, error)

	// Event storage
	StoreEvent(ctx context.Context, sessionID string, event *events.AgentEvent) error
	GetEvents(ctx context.Context, req *GetChatHistoryRequest) (*GetEventsResponse, error)
	GetEventsBySession(ctx context.Context, sessionID string, limit, offset int) ([]Event, error)
	GetEventsByCorrelationID(ctx context.Context, sessionID string, correlationID string, limit, offset int) ([]Event, error)
	CountEventsBySession(ctx context.Context, sessionID string) (int, error)

	// Preset query management
	// userID parameter is used for isolation in multi-user mode
	CreatePresetQuery(ctx context.Context, req *CreatePresetQueryRequest) (*PresetQuery, error)
	CreatePresetQueryWithUser(ctx context.Context, req *CreatePresetQueryRequest, userID string) (*PresetQuery, error)
	GetPresetQuery(ctx context.Context, id string) (*PresetQuery, error)
	UpdatePresetQuery(ctx context.Context, id string, req *UpdatePresetQueryRequest) (*PresetQuery, error)
	DeletePresetQuery(ctx context.Context, id string) error
	ListPresetQueries(ctx context.Context, limit, offset int) ([]PresetQuery, int, error)
	ListPresetQueriesWithUser(ctx context.Context, limit, offset int, userID string) ([]PresetQuery, int, error)

	// Bot connector config management
	UpsertBotConnectorConfig(ctx context.Context, req *CreateBotConnectorConfigRequest) (*BotConnectorConfig, error)
	GetBotConnectorConfig(ctx context.Context, id string) (*BotConnectorConfig, error)
	ListBotConnectorConfigs(ctx context.Context) ([]BotConnectorConfig, error)

	// Bot session management
	CreateBotSession(ctx context.Context, req *CreateBotSessionRequest) (*BotSession, error)
	GetBotSession(ctx context.Context, id string) (*BotSession, error)
	GetBotSessionByThread(ctx context.Context, platform, channelID, threadTS string) (*BotSession, error)
	GetBotSessionBySessionID(ctx context.Context, sessionID string) (*BotSession, error)
	UpdateBotSession(ctx context.Context, id string, req *UpdateBotSessionRequest) (*BotSession, error)
	CompleteBotSession(ctx context.Context, id string, status string) error
	ListBotSessions(ctx context.Context, limit, offset int, status string) ([]BotSession, int, error)

	// Bot message management
	CreateBotMessage(ctx context.Context, req *CreateBotMessageRequest) (*BotMessage, error)
	ListBotMessages(ctx context.Context, botSessionID string, limit, offset int) ([]BotMessage, int, error)

	// App user management (email→userID lookup for bot sessions)
	UpsertAppUser(ctx context.Context, userID, email, username, provider string) error
	GetAppUserByEmail(ctx context.Context, email string) (*AppUser, error)

	// User secrets management
	UpsertUserSecret(ctx context.Context, userID, name, encryptedValue string) error
	DeleteUserSecret(ctx context.Context, userID, name string) error
	ListUserSecrets(ctx context.Context, userID string) ([]UserSecret, error)

	// Employee Management
	CreateEmployee(ctx context.Context, employee *Employee) (*Employee, error)
	GetEmployee(ctx context.Context, id string) (*Employee, error)
	UpdateEmployee(ctx context.Context, id string, employee *Employee) (*Employee, error)
	DeleteEmployee(ctx context.Context, id string) error
	ListEmployees(ctx context.Context) ([]Employee, error)

	// Health check
	Ping(ctx context.Context) error
	Close() error

	// Access underlying DB connection (for integrations like Slack)
	GetDB() *sql.DB
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
	ListChatSessions(ctx context.Context, limit, offset int, presetQueryID *string, agentMode *string) ([]ChatHistorySummary, int, error)

	// Search events
	SearchEvents(ctx context.Context, filter *EventFilter) (*EventSearchResult, error)
}
