package events

import (
	"encoding/json"
	"log"
	"strings"
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
	// Streaming chunks — ephemeral, only useful in real-time via subscriber.
	// Excluding from GetEvents/polling prevents them from consuming the InitialEventsLimit (300)
	// and pushing important events (request_human_feedback, tool calls) out of range.
	// Subscribers still receive these in real-time (see AddEvent override).
	"streaming_start": true,
	"streaming_chunk": true,
	// NOTE: streaming_end is NOT excluded — it's a critical lifecycle signal that the frontend
	// needs to clear the streaming card. Since SSE can miss it (race condition, reconnect),
	// it must be recoverable via polling backfill. It's only 1 event per LLM call.
	// "streaming_end": true,
}

// HIDDEN_EVENTS contains additional event types hidden from UI display
// Note: user_message is NOT filtered — essential for conversation display on session restore
// system_prompt is stored in DB but hidden
// NOTE: agent_end is NOT filtered — the frontend needs it to clear
// the "Generating..." state and streaming text. Without it, the UI stays stuck.
// NOTE: llm_generation_end is NOT filtered — ChatInput uses it as fallback for the
// context usage circle (token_usage with conversation_total is not emitted by all providers).
// The frontend hides it from the event list display via its own HIDDEN_EVENTS.
// step_progress_updated is NOT in this list because it's required for React Flow canvas
// node highlighting - it must always be sent to frontend for workflow mode to function correctly.
var HIDDEN_EVENTS = map[string]bool{
	"llm_generation_start":      true,
	"llm_generation_with_retry": true,
	"conversation_start":        true,
	"conversation_turn":         true,
	"system_prompt":             true,
	"agent_start":               true,
	"agent_error":               true,
	"batch_execution_canceled":  true,
}

// MaxPollingLimit is the maximum number of events returned in a single polling request
// This prevents fetching too many events at once, especially when switching event modes
const MaxPollingLimit = 1000 // Match frontend MAX_EVENTS limit

// InitialEventsLimit is the number of events returned when starting from the beginning (sinceIndex=0)
// This is used when switching event modes - show latest events first, then allow loading older events
// Set to 300 to accommodate parallel multi-agent workflows where hierarchy-defining events
// (orchestrator_agent_start, delegation_start) may be far back in the event stream
const InitialEventsLimit = 300

// ShouldShowEvent checks if an event should be shown in the UI
func ShouldShowEvent(eventType string) bool {
	if eventType == "" {
		return false
	}
	// First check: NEVER show these events
	if NEVER_SHOW_EVENTS[eventType] {
		return false
	}
	// Filter out hidden events
	return !HIDDEN_EVENTS[eventType]
}

func shouldReturnEvent(eventType string, includeStreaming bool) bool {
	if includeStreaming && (eventType == "streaming_start" || eventType == "streaming_chunk" || eventType == "streaming_end") {
		return true
	}
	return ShouldShowEvent(eventType)
}

// Event represents a generic event that can be stored and retrieved
// Both MCP agent and orchestrator events now use the same AgentEvent structure
type Event struct {
	ID                string             `json:"id"`
	Type              string             `json:"type"`
	Timestamp         time.Time          `json:"timestamp"`
	Data              *events.AgentEvent `json:"data,omitempty"` // Use AgentEvent directly - both systems compatible
	Error             string             `json:"error,omitempty"`
	SessionID         string             `json:"session_id,omitempty"`
	ExecutionID       string             `json:"execution_id,omitempty"`
	ParentExecutionID string             `json:"parent_execution_id,omitempty"`
	ExecutionKind     string             `json:"execution_kind,omitempty"`
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
	if e.ExecutionID != "" {
		result["execution_id"] = e.ExecutionID
	}
	if e.ParentExecutionID != "" {
		result["parent_execution_id"] = e.ParentExecutionID
	}
	if e.ExecutionKind != "" {
		result["execution_kind"] = e.ExecutionKind
	}

	// Add the original data field - this is the only data structure we use now
	if e.Data != nil {
		result["data"] = e.Data
	}

	return json.Marshal(result)
}

func (e *Event) ensureExecutionOwnership(sessionID string, previous []Event) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(e.SessionID)
	}
	if sessionID == "" {
		return
	}
	if e.SessionID == "" {
		e.SessionID = sessionID
	}

	if e.ExecutionID == "" {
		if inherited := findParentExecutionOwnership(e, previous); inherited != nil {
			e.ExecutionID = inherited.ExecutionID
			e.ParentExecutionID = inherited.ParentExecutionID
			e.ExecutionKind = inherited.ExecutionKind
			return
		}
	}

	payload := eventPayloadMap(e)
	metadata := mapField(payload, "metadata")
	correlationID := firstNonEmptyString(agentEventCorrelationID(e), stringField(payload, "correlation_id"))
	delegationID := firstNonEmptyString(stringField(payload, "delegation_id"), correlationID)
	backgroundAgentID := firstNonEmptyString(
		stringField(payload, "background_agent_id"),
		stringField(payload, "agent_id"),
	)
	parentExecutionID := firstNonEmptyString(
		stringField(metadata, "parent_execution_id"),
		stringField(payload, "parent_execution_id"),
	)
	autoNotificationExecutionID := autoNotificationExecutionID(e)
	workflowID := firstNonEmptyString(
		backgroundAgentID,
		autoNotificationExecutionID,
		normalizeWorkshopExecutionID(stringField(metadata, "workshop_step_id")),
		normalizeWorkshopExecutionID(stringField(payload, "workshop_step_id")),
		stringField(metadata, "workflow_run_id"),
		stringField(payload, "workflow_run_id"),
		stringField(metadata, "workflow_id"),
		stringField(payload, "workflow_id"),
		normalizeWorkshopExecutionID(correlationID),
	)
	stepID := firstNonEmptyString(
		stringField(metadata, "current_step_id"),
		stringField(metadata, "orchestrator_step_id"),
		stringField(metadata, "workflow_step_id"),
		stringField(metadata, "step_id"),
		stringField(metadata, "route_id"),
		stringField(payload, "current_step_id"),
		stringField(payload, "orchestrator_step_id"),
		stringField(payload, "workflow_step_id"),
		stringField(payload, "step_id"),
		stringField(payload, "route_id"),
	)

	if e.ExecutionID == "" {
		switch {
		case e.Type == "delegation_start" || e.Type == "delegation_end" || strings.HasPrefix(delegationID, "delegation-"):
			if delegationID != "" {
				e.ExecutionID = "delegation:" + delegationID
				e.ParentExecutionID = firstNonEmptyString(parentExecutionID, backgroundAgentID, "main:"+sessionID)
				e.ExecutionKind = "delegation"
			}
		case strings.HasPrefix(e.Type, "background_agent_") && backgroundAgentID != "":
			e.ExecutionID = backgroundAgentID
			e.ParentExecutionID = firstNonEmptyString(parentExecutionID, "main:"+sessionID)
			e.ExecutionKind = "background_agent"
		case autoNotificationExecutionID != "":
			e.ExecutionID = autoNotificationExecutionID
			e.ParentExecutionID = "main:" + sessionID
			e.ExecutionKind = inferExecutionKind(autoNotificationExecutionID, sessionID)
		case stepID != "":
			if parentExecutionID != "" {
				e.ExecutionID = "workflow-step:" + parentExecutionID + ":" + stepID
				e.ParentExecutionID = parentExecutionID
			} else if workflowID != "" {
				e.ExecutionID = "workflow-step:" + workflowID + ":" + stepID
				e.ParentExecutionID = workflowID
			} else {
				e.ExecutionID = "workflow-step:" + stepID
				e.ParentExecutionID = "main:" + sessionID
			}
			e.ExecutionKind = "workflow_step"
		case e.Type == "orchestrator_agent_start" || e.Type == "orchestrator_agent_end" || e.Type == "orchestrator_agent_error":
			switch {
			case parentExecutionID != "":
				e.ExecutionID = parentExecutionID
				e.ParentExecutionID = "main:" + sessionID
				e.ExecutionKind = inferExecutionKind(parentExecutionID, sessionID)
			case workflowID != "":
				e.ExecutionID = workflowID
				e.ParentExecutionID = "main:" + sessionID
				e.ExecutionKind = "workflow"
			case correlationID != "":
				e.ExecutionID = "agent:" + correlationID
				e.ParentExecutionID = "main:" + sessionID
				e.ExecutionKind = "agent"
			}
		case workflowID != "":
			e.ExecutionID = workflowID
			e.ParentExecutionID = "main:" + sessionID
			e.ExecutionKind = "workflow"
		default:
			e.ExecutionID = "main:" + sessionID
			e.ParentExecutionID = "session:" + sessionID
			e.ExecutionKind = "main_agent"
		}
	}

	if e.ParentExecutionID == "" {
		if e.ExecutionID == "main:"+sessionID {
			e.ParentExecutionID = "session:" + sessionID
		} else {
			e.ParentExecutionID = "main:" + sessionID
		}
	}
	if e.ExecutionKind == "" {
		e.ExecutionKind = inferExecutionKind(e.ExecutionID, sessionID)
	}
}

func findParentExecutionOwnership(event *Event, previous []Event) *Event {
	if event == nil || event.Data == nil || event.Data.ParentID == "" {
		return nil
	}
	parentID := event.Data.ParentID
	for i := len(previous) - 1; i >= 0; i-- {
		prev := previous[i]
		if prev.ExecutionID == "" {
			continue
		}
		if prev.ID == parentID || (prev.Data != nil && prev.Data.SpanID == parentID) {
			return &prev
		}
	}
	return nil
}

func eventPayloadMap(event *Event) map[string]interface{} {
	if event == nil || event.Data == nil || event.Data.Data == nil {
		return nil
	}
	raw, err := json.Marshal(event.Data.Data)
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	if nested := mapField(out, "data"); nested != nil {
		for key, value := range nested {
			if _, exists := out[key]; !exists {
				out[key] = value
			}
		}
	}
	return out
}

func agentEventCorrelationID(event *Event) string {
	if event == nil || event.Data == nil {
		return ""
	}
	return strings.TrimSpace(event.Data.CorrelationID)
}

func autoNotificationExecutionID(event *Event) string {
	if event == nil || event.Type != "user_message" {
		return ""
	}
	payload := eventPayloadMap(event)
	content := firstNonEmptyString(
		stringField(payload, "content"),
		stringField(payload, "message"),
	)
	if !strings.HasPrefix(content, "[AUTO-NOTIFICATION]") {
		return ""
	}
	const marker = "(ID:"
	start := strings.Index(content, marker)
	if start < 0 {
		return ""
	}
	start += len(marker)
	end := strings.Index(content[start:], ")")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(content[start : start+end])
}

func stringField(record map[string]interface{}, key string) string {
	if record == nil {
		return ""
	}
	value, ok := record[key]
	if !ok {
		return ""
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func mapField(record map[string]interface{}, key string) map[string]interface{} {
	if record == nil {
		return nil
	}
	value, ok := record[key]
	if !ok {
		return nil
	}
	if nested, ok := value.(map[string]interface{}); ok {
		return nested
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeWorkshopExecutionID(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "workshop-workflow-") {
		return strings.TrimPrefix(value, "workshop-")
	}
	if strings.HasPrefix(value, "workflow-full-") || strings.HasPrefix(value, "workflow-step-") {
		return value
	}
	return ""
}

func inferExecutionKind(executionID, sessionID string) string {
	switch {
	case executionID == "main:"+sessionID:
		return "main_agent"
	case strings.HasPrefix(executionID, "delegation:"):
		return "delegation"
	case strings.HasPrefix(executionID, "agent:"):
		return "agent"
	case strings.HasPrefix(executionID, "workflow-step:"):
		return "workflow_step"
	case strings.HasPrefix(executionID, "workflow:"):
		return "workflow"
	case strings.HasPrefix(executionID, "workflow-"):
		return "workflow"
	default:
		return "execution"
	}
}

// ActivityCallback is called when an event is added to update session activity
type ActivityCallback func(sessionID string)

// Subscriber represents a client subscribed to real-time events for a session via SSE.
type Subscriber struct {
	Ch        chan Event
	SessionID string
}

// EventStore manages in-memory event storage for sessions
// Events are stored by sessionID, allowing multiple observers to view the same session
type EventStore struct {
	events              map[string][]Event // sessionID -> events
	sessionStartIndices map[string]int     // sessionID -> startIndex (offset for events in memory)
	sessionOwners       map[string]string  // sessionID -> userID
	mu                  sync.RWMutex
	maxEvents           int // Maximum events per session
	cleanupTicker       *time.Ticker
	stopCh              chan struct{}
	activityCallback    ActivityCallback // Optional callback to update session activity

	// SSE subscriber registry: sessionID -> list of subscribers
	subscribers   map[string][]*Subscriber
	subscribersMu sync.RWMutex
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
		sessionOwners:       make(map[string]string),
		maxEvents:           maxEvents,
		cleanupTicker:       time.NewTicker(5 * time.Minute), // Cleanup every 5 minutes
		stopCh:              make(chan struct{}),
		activityCallback:    activityCallback,
		subscribers:         make(map[string][]*Subscriber),
	}

	// Start background cleanup
	go store.cleanupRoutine()

	return store
}

// SetSessionOwner records the user that owns a session's in-memory events.
func (es *EventStore) SetSessionOwner(sessionID, userID string) {
	sessionID = strings.TrimSpace(sessionID)
	userID = strings.TrimSpace(userID)
	if sessionID == "" || userID == "" {
		return
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	es.sessionOwners[sessionID] = userID
}

// GetSessionOwner returns the user that owns a session's in-memory events.
func (es *EventStore) GetSessionOwner(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}

	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.sessionOwners[sessionID]
}

// SetActivityCallback sets the activity callback (can be called after creation)
func (es *EventStore) SetActivityCallback(callback ActivityCallback) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.activityCallback = callback
}

// Subscribe creates a new subscriber for real-time events on a session.
// The returned Subscriber's Ch channel receives events as they are added.
// Buffer size is 256 to absorb bursts without blocking AddEvent.
func (es *EventStore) Subscribe(sessionID string) *Subscriber {
	sub := &Subscriber{
		Ch:        make(chan Event, 256),
		SessionID: sessionID,
	}
	es.subscribersMu.Lock()
	es.subscribers[sessionID] = append(es.subscribers[sessionID], sub)
	es.subscribersMu.Unlock()
	return sub
}

// Unsubscribe removes a subscriber and closes its channel.
func (es *EventStore) Unsubscribe(sessionID string, sub *Subscriber) {
	es.subscribersMu.Lock()
	defer es.subscribersMu.Unlock()
	subs := es.subscribers[sessionID]
	for i, s := range subs {
		if s == sub {
			es.subscribers[sessionID] = append(subs[:i], subs[i+1:]...)
			close(sub.Ch)
			return
		}
	}
}

// AddEvent adds an event for a specific session
func (es *EventStore) AddEvent(sessionID string, event Event) {
	es.mu.Lock()
	if event.Data != nil {
		if cloned := events.CloneAgentEvent(event.Data); cloned != nil {
			event.Data = cloned
		}
	}

	// Initialize session if not exists
	if _, exists := es.events[sessionID]; !exists {
		es.events[sessionID] = make([]Event, 0)
		es.sessionStartIndices[sessionID] = 0
	}
	event.ensureExecutionOwnership(sessionID, es.events[sessionID])

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

	// Notify SSE subscribers (non-blocking send; drop if buffer full)
	es.subscribersMu.RLock()
	subs := es.subscribers[sessionID]
	for _, sub := range subs {
		// Apply event filtering for subscriber.
		// Exception: streaming events are always delivered to subscribers for real-time display,
		// even though they're in NEVER_SHOW_EVENTS (excluded from GetEvents backfill/polling).
		isStreamingEvent := event.Type == "streaming_start" || event.Type == "streaming_chunk" || event.Type == "streaming_end"
		if !isStreamingEvent && !ShouldShowEvent(event.Type) {
			continue
		}
		select {
		case sub.Ch <- event:
		default:
			// Channel full, drop event (subscriber will catch up via backfill)
			log.Printf("[EventStore] WARNING: channel full, dropping %s for session %s", event.Type, sessionID)
		}
	}
	es.subscribersMu.RUnlock()
}

// ToolCallSummary holds a compact representation of a tool call event.
type ToolCallSummary struct {
	ToolCallID string        `json:"tool_call_id"`
	ToolName   string        `json:"tool_name"`
	Status     string        `json:"status"` // "running", "done", "error"
	Duration   time.Duration `json:"duration,omitempty"`
	StartedAt  time.Time     `json:"started_at"`
	Args       string        `json:"args,omitempty"`   // full arguments from ToolCallStartEvent
	Result     string        `json:"result,omitempty"` // full result from ToolCallEndEvent (or error message)
}

// GetToolCallsByCorrelation returns tool call summaries for events matching the given correlationID.
// Used by query_step to show which tools a running step is calling.
func (es *EventStore) GetToolCallsByCorrelation(sessionID, correlationID string) []ToolCallSummary {
	es.mu.RLock()
	defer es.mu.RUnlock()

	evts, exists := es.events[sessionID]
	if !exists {
		return nil
	}

	// Track tool calls: toolCallID -> summary
	toolCalls := make(map[string]*ToolCallSummary)
	var order []string // preserve insertion order

	for _, evt := range evts {
		if evt.Data == nil || evt.Data.CorrelationID != correlationID {
			continue
		}
		switch d := evt.Data.Data.(type) {
		case *events.ToolCallStartEvent:
			if _, exists := toolCalls[d.ToolCallID]; !exists {
				order = append(order, d.ToolCallID)
			}
			toolCalls[d.ToolCallID] = &ToolCallSummary{
				ToolCallID: d.ToolCallID,
				ToolName:   d.ToolName,
				Status:     "running",
				StartedAt:  evt.Timestamp,
				Args:       d.ToolParams.Arguments,
			}
		case *events.ToolCallEndEvent:
			if tc, ok := toolCalls[d.ToolCallID]; ok {
				tc.Status = "done"
				tc.Duration = d.Duration
				tc.Result = d.Result
			} else {
				order = append(order, d.ToolCallID)
				toolCalls[d.ToolCallID] = &ToolCallSummary{
					ToolCallID: d.ToolCallID,
					ToolName:   d.ToolName,
					Status:     "done",
					Duration:   d.Duration,
					StartedAt:  evt.Timestamp,
					Result:     d.Result,
				}
			}
		case *events.ToolCallErrorEvent:
			if tc, ok := toolCalls[d.ToolCallID]; ok {
				tc.Status = "error"
				tc.Duration = d.Duration
				tc.Result = d.Error
			} else {
				order = append(order, d.ToolCallID)
				toolCalls[d.ToolCallID] = &ToolCallSummary{
					ToolCallID: d.ToolCallID,
					ToolName:   d.ToolName,
					Status:     "error",
					Duration:   d.Duration,
					StartedAt:  evt.Timestamp,
					Result:     d.Error,
				}
			}
		}
	}

	result := make([]ToolCallSummary, 0, len(order))
	for _, id := range order {
		result = append(result, *toolCalls[id])
	}
	return result
}

// GetAllEventsRaw returns a copy of all in-memory events for a session with no
// filtering and no limit. Used for persistence on session completion.
func (es *EventStore) GetAllEventsRaw(sessionID string) []Event {
	es.mu.RLock()
	defer es.mu.RUnlock()
	src, ok := es.events[sessionID]
	if !ok {
		return nil
	}
	out := make([]Event, len(src))
	copy(out, src)
	return out
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
	SinceIndex       int  // For forward polling: get events after this index
	Limit            int  // For pagination: maximum number of events to return (0 = no limit)
	Offset           int  // For pagination: skip this many events (used for backward pagination)
	IncludeStreaming bool // Include streaming_start/chunk for SSE reconnect backfill only
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
		filteredEvents := make([]Event, 0, len(events))
		for _, event := range events {
			if shouldReturnEvent(event.Type, opts.IncludeStreaming) {
				filteredEvents = append(filteredEvents, event)
			}
		}

		// Step 2: Find the position in filtered array that corresponds to "after sinceIndex" in unfiltered array
		// Use effectiveSinceIndex (relative to in-memory array)
		filteredCountUpToSinceIndex := 0
		for i := 0; i <= effectiveSinceIndex && i < len(events); i++ {
			if shouldReturnEvent(events[i].Type, opts.IncludeStreaming) {
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
						if shouldReturnEvent(events[i].Type, opts.IncludeStreaming) {
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
		eventsToPaginate := make([]Event, 0, len(events))
		for _, event := range events {
			if shouldReturnEvent(event.Type, opts.IncludeStreaming) {
				eventsToPaginate = append(eventsToPaginate, event)
			}
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
		// No specific mode: return all filtered events
		filtered := make([]Event, 0, len(events))
		for _, event := range events {
			if shouldReturnEvent(event.Type, opts.IncludeStreaming) {
				filtered = append(filtered, event)
			}
		}
		result = filtered
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
	DelegationID      string   `json:"delegation_id"`
	Depth             int      `json:"depth"`
	Instruction       string   `json:"instruction"`
	ReasoningLevel    string   `json:"reasoning_level,omitempty"`
	ModelID           string   `json:"model_id,omitempty"`
	Servers           []string `json:"servers,omitempty"`
	BackgroundAgentID string   `json:"background_agent_id,omitempty"`
	AgentTemplate     string   `json:"agent_template,omitempty"`
	Timestamp         string   `json:"timestamp"`
}

func (d *DelegationStartEventData) GetEventType() events.EventType {
	return events.EventType("delegation_start")
}

// DelegationEndEventData implements events.EventData for delegation_end
type DelegationEndEventData struct {
	DelegationID string  `json:"delegation_id"`
	Depth        int     `json:"depth"`
	Result       string  `json:"result"`
	Error        string  `json:"error,omitempty"`
	Success      bool    `json:"success"`
	Timestamp    string  `json:"timestamp"`
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
	ToolCalls    int64   `json:"tool_calls,omitempty"`
	Duration     string  `json:"duration,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
}

func (d *DelegationEndEventData) GetEventType() events.EventType {
	return events.EventType("delegation_end")
}

// GenericEventData is a generic event data type that wraps a map
// Used for events that don't need a specific struct (e.g. background agent events)
type GenericEventData struct {
	EventType string                 `json:"event_type"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

func (d *GenericEventData) GetEventType() events.EventType {
	return events.EventType(d.EventType)
}

// MarshalJSON flattens Fields into the top-level JSON
func (d *GenericEventData) MarshalJSON() ([]byte, error) {
	result := make(map[string]interface{})
	for k, v := range d.Fields {
		result[k] = v
	}
	result["event_type"] = d.EventType
	return json.Marshal(result)
}

// NewGenericEventData creates a GenericEventData with the given type and fields
func NewGenericEventData(eventType string, fields map[string]interface{}) *GenericEventData {
	return &GenericEventData{
		EventType: eventType,
		Fields:    fields,
	}
}
