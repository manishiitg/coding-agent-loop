package events

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// EventEmitter handles event creation and emission with hierarchy support
type EventEmitter struct {
	mu             sync.RWMutex
	activeEvents   map[string]*Event // eventID -> event
	completedTrees map[string]bool   // sessionID -> completed
	observers      []EventObserver
}

// EventObserver interface for event consumers
type EventObserver interface {
	OnEvent(event *Event)
}

// NewEventEmitter creates a new event emitter
func NewEventEmitter() *EventEmitter {
	return &EventEmitter{
		activeEvents:   make(map[string]*Event),
		completedTrees: make(map[string]bool),
		observers:      make([]EventObserver, 0),
	}
}

// CreateQueryRootEvent creates the root event for a query
func (e *EventEmitter) CreateQueryRootEvent(ctx context.Context, query string, sessionID string) *Event {
	eventID := GenerateEventID()

	event := &Event{
		Type:           ConversationStart,
		Timestamp:      time.Now(),
		TraceID:        getTraceID(ctx),
		SpanID:         eventID,
		ParentID:       "", // No parent for query events
		CorrelationID:  eventID,
		Data:           &ConversationStartEvent{Question: query},
		HierarchyLevel: 0,
		ParentType:     "",
		SessionID:      sessionID,
		Component:      "query",
		Query:          query,
	}

	// Track as active root event
	e.mu.Lock()
	e.activeEvents[eventID] = event
	e.mu.Unlock()

	return event
}

// CreateChildEvent creates a child event with parent relationship
func (e *EventEmitter) CreateChildEvent(ctx context.Context, eventType EventType, data EventData, parentEvent *Event) *Event {
	eventID := GenerateEventID()

	event := &Event{
		Type:           eventType,
		Timestamp:      time.Now(),
		TraceID:        parentEvent.TraceID, // Inherit trace ID
		SpanID:         eventID,
		ParentID:       parentEvent.SpanID,
		CorrelationID:  eventID,
		Data:           data,
		HierarchyLevel: parentEvent.HierarchyLevel + 1,
		ParentType:     parentEvent.Type,
		SessionID:      parentEvent.SessionID, // Inherit session ID
		Component:      GetComponentFromEventType(eventType),
		Query:          parentEvent.Query, // Inherit query
	}

	// Track as active if it's a start event
	if IsStartEvent(eventType) {
		e.mu.Lock()
		e.activeEvents[eventID] = event
		e.mu.Unlock()
	}

	// Mark tree as complete if it's an end event
	if IsEndEvent(eventType) {
		e.mu.Lock()
		delete(e.activeEvents, parentEvent.SpanID)
		e.completedTrees[event.SessionID] = true
		e.mu.Unlock()
	}

	return event
}

// Emit sends an event to all observers
func (e *EventEmitter) Emit(event *Event) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, observer := range e.observers {
		observer.OnEvent(event)
	}
}

// AddObserver adds an event observer
func (e *EventEmitter) AddObserver(observer EventObserver) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.observers = append(e.observers, observer)
}

// GetActiveEvents returns all currently active events
func (e *EventEmitter) GetActiveEvents() map[string]*Event {
	e.mu.RLock()
	defer e.mu.RUnlock()

	active := make(map[string]*Event)
	for k, v := range e.activeEvents {
		active[k] = v
	}
	return active
}

// IsTreeCompleted checks if a session tree is completed
func (e *EventEmitter) IsTreeCompleted(sessionID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.completedTrees[sessionID]
}

// Helper function
func getTraceID(ctx context.Context) string {
	if traceID := ctx.Value("trace_id"); traceID != nil {
		return traceID.(string)
	}
	return fmt.Sprintf("trace_%d", time.Now().UnixNano())
}
