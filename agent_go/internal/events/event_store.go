package events

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/manishiitg/mcpagent/events"
)

// NEVER_SHOW_EVENTS contains event types that should NEVER be shown in any mode
// These are filtered out at the polling layer, regardless of source (memory or database)
// This catches events that were stored before SKIP_EVENTS was added to the event bridge
var NEVER_SHOW_EVENTS = map[string]bool{
	// Tool extras - no UI component
	"tool_execution":     true,
	"tool_output":        true,
	"tool_response":      true,
	"tool_call_progress": true,
	// Cache events - all 9 (no UI needed)
	"cache_event":               true,
	"comprehensive_cache_event": true,
	"cache_hit":                 true,
	"cache_miss":                true,
	"cache_write":               true,
	"cache_expired":             true,
	"cache_cleanup":             true,
	"cache_error":               true,
	"cache_operation_start":     true,
}

// ADVANCED_MODE_EVENTS contains event types that are hidden in basic mode
// These events are only shown in advanced mode
// Note: step_progress_updated is NOT in this list because it's required for React Flow canvas
// node highlighting - it must always be sent to frontend for workflow mode to function correctly.
var ADVANCED_MODE_EVENTS = map[string]bool{
	"llm_generation_start":      true,
	"llm_generation_with_retry": true,
	"conversation_start":        true,
	"conversation_turn":         true,
}

// TINY_MODE_ADDITIONAL_EVENTS contains additional event types hidden in tiny mode (beyond basic mode)
// Tiny mode hides everything basic mode hides PLUS user messages, system prompts, and agent lifecycle events
var TINY_MODE_ADDITIONAL_EVENTS = map[string]bool{
	"user_message":             true,
	"system_prompt":            true,
	"agent_error":              true,
	"llm_generation_end":       true,
	"batch_execution_canceled": true,
}

// MaxPollingLimit is the maximum number of events returned in a single polling request
// This prevents fetching too many events at once, especially when switching event modes
const MaxPollingLimit = 1000 // Match frontend MAX_EVENTS limit

// InitialEventsLimit is the number of events returned when starting from the beginning (sinceIndex=0)
// This is used when switching event modes - show latest events first, then allow loading older events
const InitialEventsLimit = 50

// ShouldShowEventByMode checks if an event should be shown based on event mode
func ShouldShowEventByMode(eventType string, eventMode string) bool {
	if eventType == "" {
		return false
	}
	// First check: NEVER show these events in any mode
	if NEVER_SHOW_EVENTS[eventType] {
		return false
	}
	if eventMode == "advanced" {
		return true // Show all events in advanced mode
	}
	if eventMode == "tiny" || eventMode == "micro" {
		// In tiny/micro mode, hide everything basic mode hides PLUS user_message and system_prompt
		// So hide if it's in ADVANCED_MODE_EVENTS OR in TINY_MODE_ADDITIONAL_EVENTS
		return !ADVANCED_MODE_EVENTS[eventType] && !TINY_MODE_ADDITIONAL_EVENTS[eventType]
	}
	// In basic mode, show all events EXCEPT the ones in ADVANCED_MODE_EVENTS
	return !ADVANCED_MODE_EVENTS[eventType]
}

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

// ActivityCallback is called when an event is added to update session activity
type ActivityCallback func(sessionID string)

// EventStore manages in-memory event storage for sessions
// Events are stored by sessionID, allowing multiple observers to view the same session
type EventStore struct {
	events              map[string][]Event // sessionID -> events
	sessionStartIndices map[string]int     // sessionID -> startIndex (offset for events in memory)
	mu                  sync.RWMutex
	maxEvents           int // Maximum events per session
	cleanupTicker       *time.Ticker
	stopCh              chan struct{}
	activityCallback    ActivityCallback // Optional callback to update session activity
}

// NewEventStore creates a new event store with configurable limits
func NewEventStore(maxEvents int) *EventStore {
	return NewEventStoreWithActivityCallback(maxEvents, nil)
}

// NewEventStoreWithActivityCallback creates a new event store with an activity callback
func NewEventStoreWithActivityCallback(maxEvents int, activityCallback ActivityCallback) *EventStore {
	store := &EventStore{
		events:              make(map[string][]Event),
		sessionStartIndices: make(map[string]int),
		maxEvents:           maxEvents,
		cleanupTicker:       time.NewTicker(5 * time.Minute), // Cleanup every 5 minutes
		stopCh:              make(chan struct{}),
		activityCallback:    activityCallback,
	}

	// Start background cleanup
	go store.cleanupRoutine()

	return store
}

// SetActivityCallback sets the activity callback (can be called after creation)
func (es *EventStore) SetActivityCallback(callback ActivityCallback) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.activityCallback = callback
}

// AddEvent adds an event for a specific session
func (es *EventStore) AddEvent(sessionID string, event Event) {
	es.mu.Lock()

	// Initialize session if not exists
	if _, exists := es.events[sessionID]; !exists {
		es.events[sessionID] = make([]Event, 0)
		es.sessionStartIndices[sessionID] = 0
	}

	// Add event
	es.events[sessionID] = append(es.events[sessionID], event)

	// Remove old events if over limit
	if len(es.events[sessionID]) > es.maxEvents {
		droppedCount := len(es.events[sessionID]) - es.maxEvents
		es.events[sessionID] = es.events[sessionID][droppedCount:]
		// Update start index to reflect dropped events
		es.sessionStartIndices[sessionID] += droppedCount
	}

	// Call activity callback if set (call outside of lock to avoid deadlock)
	activityCallback := es.activityCallback
	es.mu.Unlock()

	// Update session activity (call outside lock to avoid potential deadlock)
	if activityCallback != nil && sessionID != "" {
		activityCallback(sessionID)
	}
}

// InitializeSession creates an empty event list for a session
func (es *EventStore) InitializeSession(sessionID string, baseIndex int) {
	es.mu.Lock()
	defer es.mu.Unlock()

	es.sessionStartIndices[sessionID] = baseIndex
	// Initialize session if not exists
	if _, exists := es.events[sessionID]; !exists {
		es.events[sessionID] = make([]Event, 0)
	}
}

// GetEventsOptions contains options for retrieving events
type GetEventsOptions struct {
	SinceIndex int    // For forward polling: get events after this index
	Limit      int    // For pagination: maximum number of events to return (0 = no limit)
	Offset     int    // For pagination: skip this many events (used for backward pagination)
	EventMode  string // "basic" or "advanced" - filters events by mode
}

// GetEventsResult contains the result of GetEvents call
type GetEventsResult struct {
	Events             []Event
	Exists             bool
	TotalCount         int
	LastProcessedIndex int  // Last index processed in unfiltered array (for forward polling with filtering)
	HasMore            bool // Whether there are more events available (for initial fetch with sinceIndex=0)
}

// GetEvents retrieves events for a session with various options
// Supports both forward polling (sinceIndex) and backward pagination (limit/offset)
func (es *EventStore) GetEvents(sessionID string, opts GetEventsOptions) GetEventsResult {
	es.mu.RLock()
	defer es.mu.RUnlock()

	events, exists := es.events[sessionID]
	if !exists {
		return GetEventsResult{
			Events:             []Event{},
			Exists:             false,
			TotalCount:         0,
			LastProcessedIndex: -1,
			HasMore:            false,
		}
	}

	// Get the base index offset for this session (accounts for events stored in DB before reactivation)
	// This offset is set when a session is reactivated to track where in-memory events start
	// relative to the full event history (DB events + in-memory events)
	baseIndex := es.sessionStartIndices[sessionID]

	var result []Event
	var lastProcessedIndex int
	hasMore := false

	// Adjust sinceIndex to be relative to in-memory array if a base index is set
	// Frontend sends absolute index (including DB events), we need relative index for in-memory array
	adjustedSinceIndex := opts.SinceIndex
	if baseIndex > 0 && opts.SinceIndex >= 0 {
		adjustedSinceIndex = opts.SinceIndex - baseIndex
		// If sinceIndex is before our in-memory events start, return all in-memory events
		if adjustedSinceIndex < 0 {
			adjustedSinceIndex = -1 // Will trigger "get all events" mode below
		}
	}

	// Determine which events to retrieve based on options
	if adjustedSinceIndex >= 0 || (opts.SinceIndex >= 0 && adjustedSinceIndex == -1) {
		// Forward polling mode: get events after sinceIndex (adjusted for base index)
		// CRITICAL: Filter FIRST, then apply index logic
		// This ensures consistency with backward pagination and correct filtering behavior

		// If adjustedSinceIndex is -1 but we're in forward polling mode (sinceIndex >= 0),
		// it means sinceIndex was before our in-memory events - return all in-memory events
		effectiveSinceIndex := adjustedSinceIndex
		if effectiveSinceIndex < 0 {
			effectiveSinceIndex = -1 // Will return all events
		}

		// Step 1: Filter the entire array first
		var filteredEvents []Event
		if opts.EventMode != "" {
			filteredEvents = make([]Event, 0, len(events))
			for _, event := range events {
				if ShouldShowEventByMode(event.Type, opts.EventMode) {
					filteredEvents = append(filteredEvents, event)
				}
			}
		} else {
			filteredEvents = events
		}

		// Step 2: Find the position in filtered array that corresponds to "after sinceIndex" in unfiltered array
		// Use effectiveSinceIndex (relative to in-memory array)
		filteredCountUpToSinceIndex := 0
		for i := 0; i <= effectiveSinceIndex && i < len(events); i++ {
			if opts.EventMode == "" || ShouldShowEventByMode(events[i].Type, opts.EventMode) {
				filteredCountUpToSinceIndex++
			}
		}

		// Step 3: Slice from the next position in filtered array
		if effectiveSinceIndex >= len(events) {
			result = []Event{}
			lastProcessedIndex = baseIndex + len(events) - 1
		} else if effectiveSinceIndex <= 0 && len(filteredEvents) > 0 {
			// Special case: Starting from beginning or before (sinceIndex=0 or adjusted to -1)
			// Return all filtered events (no InitialEventsLimit when continuing a conversation)
			// The limit only applies to initial page load, not to polling after reactivation
			if effectiveSinceIndex < 0 {
				// Return all in-memory events (sinceIndex was before our in-memory events)
				result = filteredEvents
				lastProcessedIndex = baseIndex + len(events) - 1
				hasMore = false
			} else {
				// sinceIndex=0: Apply initial limit
				startPos := len(filteredEvents) - InitialEventsLimit
				if startPos < 0 {
					startPos = 0
				}
				result = filteredEvents[startPos:]
				lastProcessedIndex = baseIndex + len(events) - 1
				hasMore = startPos > 0
			}
		} else {
			nextFilteredPos := filteredCountUpToSinceIndex
			if nextFilteredPos >= len(filteredEvents) {
				result = []Event{}
				lastProcessedIndex = baseIndex + len(events) - 1
			} else {
				// Apply maximum limit to prevent fetching too many events at once
				remainingEvents := filteredEvents[nextFilteredPos:]
				if len(remainingEvents) > MaxPollingLimit {
					result = remainingEvents[:MaxPollingLimit]
					// Calculate actual last processed index (relative to in-memory array)
					filteredCount := 0
					actualLastIndex := -1
					for i := 0; i < len(events); i++ {
						if opts.EventMode == "" || ShouldShowEventByMode(events[i].Type, opts.EventMode) {
							filteredCount++
							if filteredCount == nextFilteredPos+MaxPollingLimit {
								actualLastIndex = i
								break
							}
						}
					}
					lastProcessedIndex = baseIndex + actualLastIndex
				} else {
					result = remainingEvents
					lastProcessedIndex = baseIndex + len(events) - 1
				}
			}
		}
	} else if opts.Limit > 0 {
		// Backward pagination mode: get older events using limit/offset
		// Events are stored chronologically (oldest to newest: index 0 = oldest)
		// For "load more", we want older events from the START of the array
		// Offset counts from the START (0 = oldest events)

		// CRITICAL: Filter FIRST, then paginate, to ensure correct offset calculation
		// Otherwise, offset would be wrong if some events are filtered out
		var eventsToPaginate []Event
		if opts.EventMode != "" {
			// Filter first
			eventsToPaginate = make([]Event, 0, len(events))
			for _, event := range events {
				if ShouldShowEventByMode(event.Type, opts.EventMode) {
					eventsToPaginate = append(eventsToPaginate, event)
				}
			}
		} else {
			// No filtering needed
			eventsToPaginate = events
		}

		// Now paginate the filtered events
		start := opts.Offset
		end := opts.Offset + opts.Limit

		if start < 0 {
			start = 0
		}
		if end > len(eventsToPaginate) {
			end = len(eventsToPaginate)
		}

		if start < end {
			// Get events from start (oldest first)
			result = eventsToPaginate[start:end]
		} else {
			result = []Event{}
		}
		// For pagination, lastProcessedIndex is not relevant (offset-based, not index-based)
		lastProcessedIndex = -1
	} else {
		// No specific mode: return all events (with filtering if needed)
		if opts.EventMode != "" {
			filtered := make([]Event, 0, len(events))
			for _, event := range events {
				if ShouldShowEventByMode(event.Type, opts.EventMode) {
					filtered = append(filtered, event)
				}
			}
			result = filtered
		} else {
			result = events
		}
		// For "all events" mode, we processed all events
		// Add baseIndex to return absolute index
		lastProcessedIndex = baseIndex + len(events) - 1
	}

	return GetEventsResult{
		Events:             result,
		Exists:             true,
		TotalCount:         len(events),
		LastProcessedIndex: lastProcessedIndex,
		HasMore:            hasMore,
	}
}

// GetSessionStatus returns the status of a session
func (es *EventStore) GetSessionStatus(sessionID string) (int, bool) {
	es.mu.RLock()
	defer es.mu.RUnlock()

	events, exists := es.events[sessionID]
	if !exists {
		return 0, false
	}

	return len(events), true
}

// RemoveSession removes a session and its events
func (es *EventStore) RemoveSession(sessionID string) {
	es.mu.Lock()
	defer es.mu.Unlock()

	delete(es.events, sessionID)
}

// GetActiveSessions returns all active session IDs
func (es *EventStore) GetActiveSessions() []string {
	es.mu.RLock()
	defer es.mu.RUnlock()

	sessions := make([]string, 0, len(es.events))
	for sessionID := range es.events {
		sessions = append(sessions, sessionID)
	}

	return sessions
}

// cleanupRoutine periodically cleans up inactive observers
func (es *EventStore) cleanupRoutine() {
	for {
		select {
		case <-es.cleanupTicker.C:
			es.cleanupInactiveSessions()
		case <-es.stopCh:
			es.cleanupTicker.Stop()
			return
		}
	}
}

// cleanupInactiveSessions removes sessions that haven't been active recently
func (es *EventStore) cleanupInactiveSessions() {
	// For now, we'll implement a simple cleanup based on event count
	// In a real implementation, you might track last activity time
	es.mu.Lock()
	defer es.mu.Unlock()

	for sessionID, events := range es.events {
		// Remove sessions with no events (inactive)
		if len(events) == 0 {
			delete(es.events, sessionID)
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
		"total_sessions": len(es.events),
		"total_events":   totalEvents,
		"max_events":     es.maxEvents,
	}
}

// SummarizationStartedEventData implements events.EventData for context_summarization_started
type SummarizationStartedEventData struct {
	OriginalMessageCount int    `json:"original_message_count"`
	KeepLastMessages     int    `json:"keep_last_messages"`
	Timestamp            string `json:"timestamp"`
}

func (s *SummarizationStartedEventData) GetEventType() events.EventType {
	return events.EventType("context_summarization_started")
}

// SummarizationCompletedEventData implements events.EventData for context_summarization_completed
type SummarizationCompletedEventData struct {
	OriginalMessageCount int    `json:"original_message_count"`
	NewMessageCount      int    `json:"new_message_count"`
	Summary              string `json:"summary"`
	Timestamp            string `json:"timestamp"`
}

func (s *SummarizationCompletedEventData) GetEventType() events.EventType {
	return events.EventType("context_summarization_completed")
}

// SummarizationErrorEventData implements events.EventData for context_summarization_error
type SummarizationErrorEventData struct {
	Error     string `json:"error"`
	Timestamp string `json:"timestamp"`
}

func (s *SummarizationErrorEventData) GetEventType() events.EventType {
	return events.EventType("context_summarization_error")
}

// AddSummarizationStartedEvent adds a context_summarization_started event
func (es *EventStore) AddSummarizationStartedEvent(sessionID string, originalMessageCount int, keepLastMessages int) {
	now := time.Now()
	eventData := &SummarizationStartedEventData{
		OriginalMessageCount: originalMessageCount,
		KeepLastMessages:     keepLastMessages,
		Timestamp:            now.Format(time.RFC3339),
	}
	event := Event{
		ID:        sessionID + "_context_summarization_started_" + now.Format("20060102150405.000"),
		Type:      "context_summarization_started",
		Timestamp: now,
		SessionID: sessionID,
		Data: &events.AgentEvent{
			Type:      events.EventType("context_summarization_started"),
			Timestamp: now,
			Data:      eventData,
		},
	}
	es.AddEvent(sessionID, event)
}

// AddSummarizationCompletedEvent adds a context_summarization_completed event
func (es *EventStore) AddSummarizationCompletedEvent(sessionID string, originalCount int, newCount int, summary string) {
	now := time.Now()
	eventData := &SummarizationCompletedEventData{
		OriginalMessageCount: originalCount,
		NewMessageCount:      newCount,
		Summary:              summary,
		Timestamp:            now.Format(time.RFC3339),
	}
	event := Event{
		ID:        sessionID + "_context_summarization_completed_" + now.Format("20060102150405.000"),
		Type:      "context_summarization_completed",
		Timestamp: now,
		SessionID: sessionID,
		Data: &events.AgentEvent{
			Type:      events.EventType("context_summarization_completed"),
			Timestamp: now,
			Data:      eventData,
		},
	}
	es.AddEvent(sessionID, event)
}

// AddSummarizationErrorEvent adds a context_summarization_error event
func (es *EventStore) AddSummarizationErrorEvent(sessionID string, errorMessage string) {
	now := time.Now()
	eventData := &SummarizationErrorEventData{
		Error:     errorMessage,
		Timestamp: now.Format(time.RFC3339),
	}
	event := Event{
		ID:        sessionID + "_context_summarization_error_" + now.Format("20060102150405.000"),
		Type:      "context_summarization_error",
		Timestamp: now,
		SessionID: sessionID,
		Data: &events.AgentEvent{
			Type:      events.EventType("context_summarization_error"),
			Timestamp: now,
			Data:      eventData,
		},
	}
	es.AddEvent(sessionID, event)
}

// DelegationStartEventData implements events.EventData for delegation_start
type DelegationStartEventData struct {
	DelegationID   string `json:"delegation_id"`
	Depth          int    `json:"depth"`
	Instruction    string `json:"instruction"`
	ReasoningLevel string `json:"reasoning_level,omitempty"`
	ModelID        string `json:"model_id,omitempty"`
	Timestamp      string `json:"timestamp"`
}

func (d *DelegationStartEventData) GetEventType() events.EventType {
	return events.EventType("delegation_start")
}

// DelegationEndEventData implements events.EventData for delegation_end
type DelegationEndEventData struct {
	DelegationID string `json:"delegation_id"`
	Depth        int    `json:"depth"`
	Result       string `json:"result"`
	Error        string `json:"error,omitempty"`
	Success      bool   `json:"success"`
	Timestamp    string `json:"timestamp"`
	InputTokens  int64  `json:"input_tokens,omitempty"`
	OutputTokens int64  `json:"output_tokens,omitempty"`
	ToolCalls    int64  `json:"tool_calls,omitempty"`
	Duration     string `json:"duration,omitempty"`
}

func (d *DelegationEndEventData) GetEventType() events.EventType {
	return events.EventType("delegation_end")
}

// DelegationPlanCreatedEventData implements events.EventData for delegation_plan_created
type DelegationPlanCreatedEventData struct {
	PlanID    string `json:"plan_id"`
	Objective string `json:"objective"`
	TaskCount int    `json:"task_count"`
	Timestamp string `json:"timestamp"`
}

func (d *DelegationPlanCreatedEventData) GetEventType() events.EventType {
	return events.EventType("delegation_plan_created")
}

// DelegationPlanUpdatedEventData implements events.EventData for delegation_plan_updated
type DelegationPlanUpdatedEventData struct {
	PlanID    string `json:"plan_id"`
	Status    string `json:"status"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
	Failed    int    `json:"failed"`
	Timestamp string `json:"timestamp"`
}

func (d *DelegationPlanUpdatedEventData) GetEventType() events.EventType {
	return events.EventType("delegation_plan_updated")
}
