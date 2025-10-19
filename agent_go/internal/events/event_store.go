package events

import (
	"encoding/json"
	"sync"
	"time"

	"mcp-agent/agent_go/pkg/events"
)

// Event represents a generic event that can be stored and retrieved
// Both MCP agent and orchestrator events now use the same AgentEvent structure
type Event struct {
	ID        string             `json:"id"`
	Type      string             `json:"type"`
	Timestamp time.Time          `json:"timestamp"`
	Data      *events.AgentEvent `json:"data,omitempty"` // Use AgentEvent directly - both systems compatible
	Error     string             `json:"error,omitempty"`
	SessionID string             `json:"session_id,omitempty"`
}

// MarshalJSON customizes JSON serialization to flatten the event structure for frontend
func (e Event) MarshalJSON() ([]byte, error) {
	// Create a map with all the base fields
	result := map[string]interface{}{
		"id":         e.ID,
		"type":       e.Type,
		"timestamp":  e.Timestamp,
		"session_id": e.SessionID,
	}

	// Add error if it exists
	if e.Error != "" {
		result["error"] = e.Error
	}

	// Add the original data field - this is the only data structure we use now
	if e.Data != nil {
		result["data"] = e.Data
	}

	return json.Marshal(result)
}

// EventStore manages in-memory event storage for multiple observers
type EventStore struct {
	events        map[string][]Event // observerID -> events
	lastIndex     map[string]int     // observerID -> last event index
	eventCounters map[string]int     // observerID -> event counter (persistent across messages)
	mu            sync.RWMutex
	maxEvents     int // Maximum events per observer
	cleanupTicker *time.Ticker
	stopCh        chan struct{}
}

// NewEventStore creates a new event store with configurable limits
func NewEventStore(maxEvents int) *EventStore {
	store := &EventStore{
		events:        make(map[string][]Event),
		lastIndex:     make(map[string]int),
		eventCounters: make(map[string]int),
		maxEvents:     maxEvents,
		cleanupTicker: time.NewTicker(5 * time.Minute), // Cleanup every 5 minutes
		stopCh:        make(chan struct{}),
	}

	// Start background cleanup
	go store.cleanupRoutine()

	return store
}

// AddEvent adds an event for a specific observer
func (es *EventStore) AddEvent(observerID string, event Event) {
	es.mu.Lock()
	defer es.mu.Unlock()

	// Initialize observer if not exists
	if _, exists := es.events[observerID]; !exists {
		es.events[observerID] = make([]Event, 0)
		es.lastIndex[observerID] = 0
	}

	// Add event
	es.events[observerID] = append(es.events[observerID], event)

	// Remove old events if over limit
	if len(es.events[observerID]) > es.maxEvents {
		es.events[observerID] = es.events[observerID][len(es.events[observerID])-es.maxEvents:]
	}
}

// InitializeObserver creates an empty event list for an observer
func (es *EventStore) InitializeObserver(observerID string) {
	es.mu.Lock()
	defer es.mu.Unlock()

	// Initialize observer if not exists
	if _, exists := es.events[observerID]; !exists {
		es.events[observerID] = make([]Event, 0)
		es.lastIndex[observerID] = 0
		es.eventCounters[observerID] = 0
	}
}

// GetNextEventCounter gets and increments the event counter for an observer
func (es *EventStore) GetNextEventCounter(observerID string) int {
	es.mu.Lock()
	defer es.mu.Unlock()

	// Initialize counter if not exists
	if _, exists := es.eventCounters[observerID]; !exists {
		es.eventCounters[observerID] = 0
	}

	// Increment and return the counter
	es.eventCounters[observerID]++
	return es.eventCounters[observerID]
}

// GetEvents retrieves events for an observer since a specific index
func (es *EventStore) GetEvents(observerID string, sinceIndex int) ([]Event, int, bool) {
	es.mu.RLock()
	defer es.mu.RUnlock()

	events, exists := es.events[observerID]
	if !exists {
		return []Event{}, 0, false
	}

	// If sinceIndex is beyond our events, return empty but with correct last index
	if sinceIndex >= len(events) {
		// Return the actual last event index (len(events) - 1) instead of len(events)
		// This prevents the frontend from getting stuck in an infinite polling loop
		lastIndex := len(events) - 1
		if lastIndex < 0 {
			lastIndex = 0
		}
		return []Event{}, lastIndex, true
	}

	// Return events AFTER the specified index (excluding it)
	// This ensures only new events are returned, preventing infinite loops
	nextIndex := sinceIndex + 1
	var newEvents []Event
	if nextIndex >= len(events) {
		// No new events after the sinceIndex
		newEvents = []Event{}
	} else {
		newEvents = events[nextIndex:]
	}

	// Return the actual last event index (len(events) - 1) instead of len(events)
	// This prevents the frontend from getting stuck in an infinite polling loop
	lastIndex := len(events) - 1
	if lastIndex < 0 {
		lastIndex = 0
	}
	return newEvents, lastIndex, true
}

// GetObserverStatus returns the status of an observer
func (es *EventStore) GetObserverStatus(observerID string) (int, bool) {
	es.mu.RLock()
	defer es.mu.RUnlock()

	events, exists := es.events[observerID]
	if !exists {
		return 0, false
	}

	return len(events), true
}

// RemoveObserver removes an observer and its events
func (es *EventStore) RemoveObserver(observerID string) {
	es.mu.Lock()
	defer es.mu.Unlock()

	delete(es.events, observerID)
	delete(es.lastIndex, observerID)
	delete(es.eventCounters, observerID) // Clean up event counter to prevent memory leak
}

// GetActiveObservers returns all active observer IDs
func (es *EventStore) GetActiveObservers() []string {
	es.mu.RLock()
	defer es.mu.RUnlock()

	observers := make([]string, 0, len(es.events))
	for observerID := range es.events {
		observers = append(observers, observerID)
	}

	return observers
}

// cleanupRoutine periodically cleans up inactive observers
func (es *EventStore) cleanupRoutine() {
	for {
		select {
		case <-es.cleanupTicker.C:
			es.cleanupInactiveObservers()
		case <-es.stopCh:
			es.cleanupTicker.Stop()
			return
		}
	}
}

// cleanupInactiveObservers removes observers that haven't been active recently
func (es *EventStore) cleanupInactiveObservers() {
	// For now, we'll implement a simple cleanup based on event count
	// In a real implementation, you might track last activity time
	es.mu.Lock()
	defer es.mu.Unlock()

	for observerID, events := range es.events {
		// Remove observers with no events (inactive)
		if len(events) == 0 {
			delete(es.events, observerID)
			delete(es.lastIndex, observerID)
			delete(es.eventCounters, observerID) // Clean up event counter to prevent memory leak
		}
	}
}

// Stop stops the event store and cleanup routine
func (es *EventStore) Stop() {
	close(es.stopCh)
}

// GetStats returns statistics about the event store
func (es *EventStore) GetStats() map[string]interface{} {
	es.mu.RLock()
	defer es.mu.RUnlock()

	totalEvents := 0
	for _, events := range es.events {
		totalEvents += len(events)
	}

	return map[string]interface{}{
		"total_observers": len(es.events),
		"total_events":    totalEvents,
		"max_events":      es.maxEvents,
	}
}
