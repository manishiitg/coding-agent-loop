package events

import (
	"context"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/logger"

	"github.com/sirupsen/logrus"
)

// EventObserver implements AgentEventListener to capture agent events
type EventObserver struct {
	store      *EventStore
	observerID string
	sessionID  string
	logger     utils.ExtendedLogger
}

// NewEventObserver creates a new event observer
func NewEventObserver(store *EventStore, observerID, sessionID string) *EventObserver {
	return &EventObserver{
		store:      store,
		observerID: observerID,
		sessionID:  sessionID,
		logger:     createDefaultLogger(),
	}
}

// NewEventObserverWithLogger creates a new event observer with an injected logger
func NewEventObserverWithLogger(store *EventStore, observerID, sessionID string, logger utils.ExtendedLogger) *EventObserver {
	return &EventObserver{
		store:      store,
		observerID: observerID,
		sessionID:  sessionID,
		logger:     logger,
	}
}

// HandleEvent processes agent events and stores them in the event store
func (eo *EventObserver) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	// Get the next event counter from the store (persistent across messages)
	eventCounter := eo.store.GetNextEventCounter(eo.observerID)

	// Create the store event with only the original AgentEvent data
	// Add a random suffix to ensure uniqueness even when multiple tracers send the same event
	randomSuffix := fmt.Sprintf("%d", time.Now().UnixNano()%1000000)
	storeEvent := Event{
		ID:        fmt.Sprintf("%s_event_%d_%d_%s", eo.observerID, eventCounter, event.Timestamp.UnixNano(), randomSuffix),
		Type:      string(event.Type),
		Timestamp: event.Timestamp,
		SessionID: eo.sessionID,
		Data:      event, // Use only the original AgentEvent
	}

	// No special handling - pass event data directly to frontend
	// The frontend will handle content extraction from the original event data
	// This follows the unified event system principle from types-sync-design.md

	// Content and error are already set on storeEvent if needed

	// Store the event
	eo.store.AddEvent(eo.observerID, storeEvent)

	return nil
}

// Name returns the observer name
func (eo *EventObserver) Name() string {
	return fmt.Sprintf("event_observer_%s", eo.observerID)
}

// createDefaultLogger creates a default logger for the event observer
func createDefaultLogger() utils.ExtendedLogger {
	loggerInstance, err := logger.CreateLogger("", "info", "text", true)
	if err != nil {
		// If we can't create a logger, create a minimal one that won't panic
		return &minimalLogger{}
	}
	return loggerInstance
}

// minimalLogger is a fallback logger that implements ExtendedLogger
type minimalLogger struct{}

func (m *minimalLogger) Infof(format string, v ...any)                         {}
func (m *minimalLogger) Errorf(format string, v ...any)                        {}
func (m *minimalLogger) Info(args ...interface{})                              {}
func (m *minimalLogger) Error(args ...interface{})                             {}
func (m *minimalLogger) Debug(args ...interface{})                             {}
func (m *minimalLogger) Debugf(format string, args ...interface{})             {}
func (m *minimalLogger) Warn(args ...interface{})                              {}
func (m *minimalLogger) Warnf(format string, args ...interface{})              {}
func (m *minimalLogger) Fatal(args ...interface{})                             {}
func (m *minimalLogger) Fatalf(format string, args ...interface{})             {}
func (m *minimalLogger) WithField(key string, value interface{}) *logrus.Entry { return nil }
func (m *minimalLogger) WithFields(fields logrus.Fields) *logrus.Entry         { return nil }
func (m *minimalLogger) WithError(err error) *logrus.Entry                     { return nil }
func (m *minimalLogger) Close() error                                          { return nil }
