package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/manishiitg/mcpagent/events"
)

// EventDatabaseObserver implements the EventObserver interface to store events in the database
type EventDatabaseObserver struct {
	db          Database
	ShouldStore func(eventType string) bool
}

// NewEventDatabaseObserver creates a new database observer
func NewEventDatabaseObserver(db Database, shouldStore func(eventType string) bool) *EventDatabaseObserver {
	if shouldStore == nil {
		shouldStore = func(eventType string) bool { return true }
	}
	return &EventDatabaseObserver{
		db:          db,
		ShouldStore: shouldStore,
	}
}

// OnEvent handles incoming events and stores them in the database
func (e *EventDatabaseObserver) OnEvent(event *events.Event) {
	// Skip streaming events - they are ephemeral and handled in real-time via polling
	if event.Type == events.StreamingStart || event.Type == events.StreamingChunk || event.Type == events.StreamingEnd {
		return
	}

	if !e.ShouldStore(string(event.Type)) {
		return
	}

	ctx := context.Background()

	// Convert unified Event to AgentEvent for storage
	agentEvent := &events.AgentEvent{
		Type:           event.Type,
		Timestamp:      event.Timestamp,
		EventIndex:     0, // Will be set by the event store
		TraceID:        event.TraceID,
		SpanID:         event.SpanID,
		ParentID:       event.ParentID,
		HierarchyLevel: event.HierarchyLevel,
		SessionID:      event.SessionID,
		Component:      event.Component,
		Data:           event.Data,
	}

	// Store the event
	if err := e.db.StoreEvent(ctx, event.SessionID, agentEvent); err != nil {
		fmt.Printf("Failed to store event: %v\n", err)
	}
}

// HandleEvent implements the AgentEventListener interface for direct agent event handling
func (e *EventDatabaseObserver) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	// Note: We can't use logger here as EventDatabaseObserver doesn't have one
	// This is called from the agent event system

	// Skip streaming events - they are ephemeral and handled in real-time via polling
	// Persisting them would cause "Unknown Event Type" on page reload
	if event.Type == events.StreamingStart || event.Type == events.StreamingChunk || event.Type == events.StreamingEnd {
		return nil
	}

	if !e.ShouldStore(string(event.Type)) {
		return nil
	}

	// Extract original session ID from modified session ID
	// The agent modifies session ID to: agent-init-{originalSessionID}-{timestamp}
	originalSessionID := event.SessionID
	if strings.HasPrefix(event.SessionID, "agent-init-") {
		// Remove "agent-init-" prefix
		withoutPrefix := strings.TrimPrefix(event.SessionID, "agent-init-")
		// Find the last dash and remove everything after it (timestamp)
		if lastDash := strings.LastIndex(withoutPrefix, "-"); lastDash != -1 {
			originalSessionID = withoutPrefix[:lastDash]
		}
	}

	// Store the event using the original session ID
	if err := e.db.StoreEvent(ctx, originalSessionID, event); err != nil {
		return err
	}
	return nil
}

// Name implements the AgentEventListener interface
func (e *EventDatabaseObserver) Name() string {
	return "EventDatabaseObserver"
}

// ChatHistoryService provides high-level chat history operations
type ChatHistoryService struct {
	db Database
}

// NewChatHistoryService creates a new chat history service
func NewChatHistoryService(db Database) *ChatHistoryService {
	return &ChatHistoryService{db: db}
}

// StartChatSession starts a new chat session
func (s *ChatHistoryService) StartChatSession(ctx context.Context, sessionID, title string) (*ChatSession, error) {
	req := &CreateChatSessionRequest{
		SessionID: sessionID,
		Title:     title,
	}
	return s.db.CreateChatSession(ctx, req)
}

// EndChatSession ends a chat session
func (s *ChatHistoryService) EndChatSession(ctx context.Context, sessionID string, status string) error {
	req := &UpdateChatSessionRequest{
		Status: status,
	}
	_, err := s.db.UpdateChatSession(ctx, sessionID, req)
	return err
}
