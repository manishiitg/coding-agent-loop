package events

import (
	"context"
	"fmt"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/logger"
	"mcpagent/events"
	loggerv2 "mcpagent/logger/v2"
)

// EventObserver implements AgentEventListener to capture agent events
type EventObserver struct {
	store     *EventStore
	sessionID string
	logger    loggerv2.Logger
}

// NewEventObserver creates a new event observer
func NewEventObserver(store *EventStore, sessionID string) *EventObserver {
	return &EventObserver{
		store:     store,
		sessionID: sessionID,
		logger:    createDefaultLogger(),
	}
}

// NewEventObserverWithLogger creates a new event observer with an injected logger
func NewEventObserverWithLogger(store *EventStore, sessionID string, logger loggerv2.Logger) *EventObserver {
	return &EventObserver{
		store:     store,
		sessionID: sessionID,
		logger:    logger,
	}
}

// HandleEvent processes agent events and stores them in the event store
func (eo *EventObserver) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	// Create the store event with only the original AgentEvent data
	// Add a random suffix to ensure uniqueness even when multiple tracers send the same event
	randomSuffix := fmt.Sprintf("%d", time.Now().UnixNano()%1000000)
	storeEvent := Event{
		ID:        fmt.Sprintf("%s_%s_%d_%s", eo.sessionID, event.Type, event.Timestamp.UnixNano(), randomSuffix),
		Type:      string(event.Type),
		Timestamp: event.Timestamp,
		SessionID: eo.sessionID,
		Data:      event, // Use only the original AgentEvent
	}

	// No special handling - pass event data directly to frontend
	// The frontend will handle content extraction from the original event data
	// This follows the unified event system principle from types-sync-design.md

	// Content and error are already set on storeEvent if needed

	// Store the event by sessionID
	eo.store.AddEvent(eo.sessionID, storeEvent)

	return nil
}

// Name returns the observer name
func (eo *EventObserver) Name() string {
	return fmt.Sprintf("event_observer_%s", eo.sessionID)
}

// createDefaultLogger creates a default logger for the event observer
func createDefaultLogger() loggerv2.Logger {
	loggerInstance, err := logger.CreateLogger("", "info", "text", true)
	if err != nil {
		// If we can't create a logger, create a minimal one that won't panic
		return &minimalLogger{}
	}
	return loggerInstance
}

// minimalLogger is a fallback logger that implements loggerv2.Logger
type minimalLogger struct{}

func (m *minimalLogger) Debug(msg string, fields ...loggerv2.Field)            {}
func (m *minimalLogger) Info(msg string, fields ...loggerv2.Field)             {}
func (m *minimalLogger) Warn(msg string, fields ...loggerv2.Field)             {}
func (m *minimalLogger) Error(msg string, err error, fields ...loggerv2.Field) {}
func (m *minimalLogger) Fatal(msg string, err error, fields ...loggerv2.Field) {}
func (m *minimalLogger) With(fields ...loggerv2.Field) loggerv2.Logger         { return m }
func (m *minimalLogger) Close() error                                          { return nil }
