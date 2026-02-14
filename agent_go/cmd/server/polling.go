package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"mcp-agent-builder-go/agent_go/internal/events"
	"mcp-agent-builder-go/agent_go/pkg/database"

	pkgevents "github.com/manishiitg/mcpagent/events"

	"github.com/gorilla/mux"
)

// flatEventData is a custom EventData type that serializes event-specific fields directly
// without BaseEventData fields or nested "data" field, matching what the frontend expects
type flatEventData struct {
	eventData map[string]interface{}
	eventType pkgevents.EventType
}

func (f *flatEventData) GetEventType() pkgevents.EventType {
	return f.eventType
}

// MarshalJSON serializes only the event-specific fields (no BaseEventData, no nested "data")
func (f *flatEventData) MarshalJSON() ([]byte, error) {
	return json.Marshal(f.eventData)
}

// --- POLLING API TYPES ---
// Observer APIs removed - events are now stored by sessionID

// GetEventsResponse represents the response for event polling
type GetEventsResponse struct {
	Events                     []events.Event `json:"events"`
	HasMore                    bool           `json:"has_more"`
	SessionID                  string         `json:"session_id"`
	SessionStatus              string         `json:"session_status,omitempty"`                // Session status: "running", "completed", "error", "stopped", "inactive"
	LastProcessedIndex         int            `json:"last_processed_index"`                    // Last index processed in unfiltered array (for correct sinceIndex tracking)
	HasRunningBackgroundAgents bool           `json:"has_running_background_agents,omitempty"` // Whether background agents are still running for this session
}

// --- POLLING API HANDLERS ---
// Observer registration/status/removal handlers removed - no longer needed

// handleGetEvents handles event polling for a session (new session-based API)
// Supports both forward polling (since parameter) and backward pagination (limit/offset)
// Also supports event mode filtering (event_mode parameter: "basic" or "advanced")
func (api *StreamingAPI) handleGetSessionEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract session ID from URL
	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Parse query parameters
	sinceStr := r.URL.Query().Get("since")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	eventMode := r.URL.Query().Get("event_mode")
	if eventMode == "" {
		eventMode = "basic" // Default to basic mode
	}
	if eventMode != "basic" && eventMode != "advanced" && eventMode != "tiny" && eventMode != "micro" {
		http.Error(w, "event_mode must be 'basic', 'advanced', 'tiny', or 'micro'", http.StatusBadRequest)
		return
	}

	// Build options for GetEvents
	opts := events.GetEventsOptions{
		EventMode:  eventMode,
		SinceIndex: -1, // Default: not using sinceIndex
		Limit:      0,  // Default: no limit
		Offset:     0,  // Default: no offset
	}

	// Determine mode: forward polling (since) or backward pagination (limit/offset)
	if sinceStr != "" {
		// Forward polling mode
		sinceIndex, err := strconv.Atoi(sinceStr)
		if err != nil {
			http.Error(w, "since parameter must be a valid integer", http.StatusBadRequest)
			return
		}
		opts.SinceIndex = sinceIndex
	} else if limitStr != "" || offsetStr != "" {
		// Backward pagination mode
		if limitStr != "" {
			limit, err := strconv.Atoi(limitStr)
			if err != nil || limit <= 0 {
				http.Error(w, "limit parameter must be a positive integer", http.StatusBadRequest)
				return
			}
			opts.Limit = limit
		} else {
			opts.Limit = 50 // Default limit for pagination
		}

		if offsetStr != "" {
			offset, err := strconv.Atoi(offsetStr)
			if err != nil || offset < 0 {
				http.Error(w, "offset parameter must be a non-negative integer", http.StatusBadRequest)
				return
			}
			opts.Offset = offset
		}
	} else {
		// Neither since nor limit/offset specified - require at least one
		http.Error(w, "either 'since' parameter (for polling) or 'limit' parameter (for pagination) is required", http.StatusBadRequest)
		return
	}

	// Get events for session with options
	getEventsResult := api.eventStore.GetEvents(sessionID, opts)
	sessionEvents := getEventsResult.Events
	exists := getEventsResult.Exists

	lastProcessedIndex := getEventsResult.LastProcessedIndex
	hasMoreFromStore := getEventsResult.HasMore

	// Get current user ID for session isolation
	currentUserID := GetUserIDFromContext(r.Context())

	// Get session status (from active sessions or database)
	var sessionStatus string
	var chatSession *database.ChatSession
	activeSession, existsInActive := api.getActiveSession(sessionID)
	if existsInActive {
		// Verify user ownership for active session
		if activeSession.UserID != "" && activeSession.UserID != currentUserID {
			http.Error(w, "Session not found or access denied", http.StatusNotFound)
			return
		}
		sessionStatus = activeSession.Status
	} else {
		// Check database for completed/error sessions - filter by user for isolation
		var err error
		chatSession, err = api.chatDB.GetChatSessionWithUser(r.Context(), sessionID, currentUserID)
		if err == nil && chatSession != nil {
			sessionStatus = chatSession.Status
		}
		// If not found, leave empty (session might not exist yet or belongs to another user)
	}

	// Check if we need to fallback to database:
	// 1. Session doesn't exist in memory (!exists), OR
	// 2. Session exists in memory but has 0 events AND session is completed/stopped
	shouldFallbackToDB := !exists || (exists && len(sessionEvents) == 0 && chatSession != nil && (chatSession.Status == "completed" || chatSession.Status == "error" || chatSession.Status == "stopped"))

	if shouldFallbackToDB {
		// Session doesn't exist in memory or has no events - check if it's a completed/stopped session in database
		// For non-active sessions (completed, stopped, error), fetch events from database
		if chatSession != nil && (chatSession.Status == "completed" || chatSession.Status == "error" || chatSession.Status == "stopped") {
			// Fallback to database for non-active sessions
			dbEvents, err := api.chatDB.GetEventsBySession(r.Context(), sessionID, 10000, 0)
			if err != nil {
				api.logger.Warn(fmt.Sprintf("[POLLING] Failed to fetch events from database for session %s: %v", sessionID, err))
			}
			if err == nil && len(dbEvents) > 0 {
				// Convert database events to polling events format
				convertedEvents := make([]events.Event, 0, len(dbEvents))
				parseErrors := 0
				filteredOut := 0
				for i, dbEvent := range dbEvents {
					// Parse only the Type field from the stored AgentEvent JSON for filtering
					// The full AgentEvent will be unmarshaled properly by the event store's MarshalJSON
					type eventTypeOnly struct {
						Type pkgevents.EventType `json:"type"`
					}

					var typeOnly eventTypeOnly
					if err := json.Unmarshal(dbEvent.EventData, &typeOnly); err != nil {
						parseErrors++
						if i < 3 { // Log first 3 parse errors
							log.Printf("[POLLING ERROR] Failed to parse event type %d for session %s: %v, event_type=%s", i, sessionID, err, dbEvent.EventType)
						}
						continue
					}

					// Apply event mode filtering
					shouldShow := opts.EventMode == "" || events.ShouldShowEventByMode(string(typeOnly.Type), opts.EventMode)
					if !shouldShow {
						filteredOut++
						continue
					}

					// Unmarshal the full AgentEvent - use helper struct to handle EventData interface
					// Since EventData is an interface, we need to unmarshal Data as json.RawMessage first
					type agentEventWithRawData struct {
						Type           pkgevents.EventType `json:"type"`
						Timestamp      time.Time           `json:"timestamp"`
						EventIndex     int                 `json:"event_index"`
						TraceID        string              `json:"trace_id,omitempty"`
						SpanID         string              `json:"span_id,omitempty"`
						ParentID       string              `json:"parent_id,omitempty"`
						CorrelationID  string              `json:"correlation_id,omitempty"`
						HierarchyLevel int                 `json:"hierarchy_level"`
						SessionID      string              `json:"session_id,omitempty"`
						Component      string              `json:"component,omitempty"`
						Data           json.RawMessage     `json:"data"`
					}

					var helper agentEventWithRawData
					if err := json.Unmarshal(dbEvent.EventData, &helper); err != nil {
						parseErrors++
						if i < 3 {
							log.Printf("[POLLING ERROR] Failed to parse event %d for session %s: %v, event_type=%s", i, sessionID, err, dbEvent.EventType)
						}
						continue
					}

					// Unmarshal Data field into a map to preserve structure
					var dataMap map[string]interface{}
					if err := json.Unmarshal(helper.Data, &dataMap); err != nil {
						parseErrors++
						if i < 3 {
							log.Printf("[POLLING ERROR] Failed to parse event data %d for session %s: %v", i, sessionID, err)
						}
						continue
					}

					// Extract event-specific fields, excluding BaseEventData fields
					// BaseEventData fields are: timestamp, trace_id, span_id, event_id, parent_id,
					// is_end_event, correlation_id, hierarchy_level, session_id, component, metadata
					baseEventDataFields := map[string]bool{
						"timestamp":       true,
						"trace_id":        true,
						"span_id":         true,
						"event_id":        true,
						"parent_id":       true,
						"is_end_event":    true,
						"correlation_id":  true,
						"hierarchy_level": true,
						"session_id":      true,
						"component":       true,
						"metadata":        true,
					}

					actualEventData := make(map[string]interface{})
					for k, v := range dataMap {
						// Skip BaseEventData fields - they're already in AgentEvent
						if !baseEventDataFields[k] {
							actualEventData[k] = v
						}
					}

					// Use convertDBEventToPollingEvent helper for consistency
					// But we need to call it from server.go, so we'll duplicate the logic here
					// Create AgentEvent with flatEventData that serializes directly
					agentEvent := pkgevents.AgentEvent{
						Type:           helper.Type,
						Timestamp:      helper.Timestamp,
						EventIndex:     helper.EventIndex,
						TraceID:        helper.TraceID,
						SpanID:         helper.SpanID,
						ParentID:       helper.ParentID,
						CorrelationID:  helper.CorrelationID,
						HierarchyLevel: helper.HierarchyLevel,
						SessionID:      helper.SessionID,
						Component:      helper.Component,
						Data: &flatEventData{
							eventData: actualEventData,
							eventType: helper.Type,
						},
					}

					convertedEvents = append(convertedEvents, events.Event{
						ID:        dbEvent.ID,
						Type:      dbEvent.EventType,
						Timestamp: dbEvent.Timestamp,
						SessionID: sessionID,
						Data:      &agentEvent,
					})
				}

				// Fix EventIndex for DB-loaded events: they're stored with EventIndex=0
				// Assign sequential indices so sinceIndex filtering works correctly
				for i := range convertedEvents {
					if convertedEvents[i].Data != nil && convertedEvents[i].Data.EventIndex == 0 {
						convertedEvents[i].Data.EventIndex = i
					}
				}

				// Apply sinceIndex filtering if specified
				var filteredBySinceIndex []events.Event
				maxEventIndex := -1
				if opts.SinceIndex >= 0 {
					for _, event := range convertedEvents {
						eventIndex := -1
						if event.Data != nil {
							eventIndex = event.Data.EventIndex
						}
						if eventIndex > maxEventIndex {
							maxEventIndex = eventIndex
						}
						if eventIndex > opts.SinceIndex {
							filteredBySinceIndex = append(filteredBySinceIndex, event)
						}
					}
					if maxEventIndex < 0 && len(convertedEvents) > 0 {
						maxEventIndex = len(convertedEvents) - 1
					}
				} else {
					// No sinceIndex filtering, return all
					filteredBySinceIndex = convertedEvents
					for _, event := range convertedEvents {
						if event.Data != nil && event.Data.EventIndex > maxEventIndex {
							maxEventIndex = event.Data.EventIndex
						}
					}
					if maxEventIndex < 0 && len(convertedEvents) > 0 {
						maxEventIndex = len(convertedEvents) - 1
					}
				}

				response := GetEventsResponse{
					Events:                     filteredBySinceIndex,
					HasMore:                    false, // Completed sessions don't have more events
					SessionID:                  sessionID,
					SessionStatus:              sessionStatus,
					LastProcessedIndex:         maxEventIndex, // Use actual max EventIndex, not array length
					HasRunningBackgroundAgents: api.bgAgentRegistry.HasRunningAgents(sessionID),
				}

				if err := json.NewEncoder(w).Encode(response); err != nil {
					http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
					return
				}
				return
			}
		}

		// Session doesn't exist yet (no events have been added)
		// Return empty events array instead of 404 - this is expected when polling starts before events are generated
		response := GetEventsResponse{
			Events:                     []events.Event{},
			HasMore:                    false,
			SessionID:                  sessionID,
			SessionStatus:              sessionStatus,
			LastProcessedIndex:         -1, // No events processed
			HasRunningBackgroundAgents: api.bgAgentRegistry.HasRunningAgents(sessionID),
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
			return
		}
		return
	}

	for i, event := range sessionEvents {
		api.logger.Debug(fmt.Sprintf("  [%d] %s", i, event.Type))
	}

	// Determine has_more based on mode
	// Use hasMoreFromStore which is calculated correctly by the event store:
	// - For sinceIndex=0: hasMore is true if there are older events beyond InitialEventsLimit
	// - For sinceIndex>0 (forward polling): hasMore is false (frontend continues polling anyway)
	// - For limit/offset (backward pagination): hasMore is true if more events exist
	hasMore := hasMoreFromStore
	if !hasMoreFromStore && opts.Limit > 0 {
		// Backward pagination: has more if there are more filtered events after current offset
		// totalCount is the total UNFILTERED events, but we need to check filtered count
		// Since we filter first then paginate, we can check if we got a full page
		// If we got fewer events than requested limit, we've reached the end
		hasMore = len(sessionEvents) >= opts.Limit
	}
	// Note: For forward polling (sinceIndex >= 0), hasMore from store is correct:
	// - It's true only when we limited results due to InitialEventsLimit (sinceIndex=0 case)
	// - It's false for normal polling (sinceIndex > 0) which is correct behavior
	// Frontend doesn't need hasMore for streaming - it keeps polling until session completes

	response := GetEventsResponse{
		Events:                     sessionEvents,
		HasMore:                    hasMore,
		SessionID:                  sessionID,
		SessionStatus:              sessionStatus,
		LastProcessedIndex:         lastProcessedIndex,
		HasRunningBackgroundAgents: api.bgAgentRegistry.HasRunningAgents(sessionID),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// --- In-Memory Session/Agent State Management ---
//
// StreamingAPI maintains a map of sessionID -> *LLMAgentWrapper, allowing each frontend session
// (identified by X-Session-ID header, cookie, or fallback to queryID) to have its own persistent agent instance.
// This enables the frontend to interrupt (stop) and resume conversations with the same agent, preserving
// conversation state in memory for the session's lifetime. No external database or disk persistence is used.
//
// - All /api/query and /api/stream/{query_id} requests use the same agent instance for a given sessionID.
// - The /api/session/stop endpoint (POST) allows explicit interruption/clearing of a session's agent state.
// - When a session is stopped, its agent is removed from memory and a new one will be created on the next request.
// - If the server process is restarted, all in-memory session state is lost (by design).
//
// This design provides efficient, scalable, and stateless (from a persistence perspective) session management
// for interactive, interruptible agent conversations in the frontend.

// --- ACTIVE SESSION API ENDPOINTS ---

// GetActiveSessionsResponse represents the response for getting active sessions
type GetActiveSessionsResponse struct {
	ActiveSessions []*ActiveSessionInfo `json:"active_sessions"`
	Total          int                  `json:"total"`
}

// ReconnectSessionResponse represents the response for reconnecting to a session
type ReconnectSessionResponse struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
	AgentMode string `json:"agent_mode"`
	Message   string `json:"message"`
}

// handleGetActiveSessions handles requests to get all active sessions
// Returns running sessions and recently completed sessions (within 30 minutes)
// This allows the frontend to restore sessions on page refresh
// Also queries database for recent sessions if in-memory map is empty (e.g., after backend restart)
func (api *StreamingAPI) handleGetActiveSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get current user ID for session isolation
	currentUserID := GetUserIDFromContext(r.Context())

	// getAllActiveSessions returns running + recently completed sessions from in-memory map
	allActiveSessions := api.getAllActiveSessions()

	// Filter sessions by user ID for isolation
	activeSessions := make([]*ActiveSessionInfo, 0, len(allActiveSessions))
	for _, session := range allActiveSessions {
		// Include session if it belongs to this user (or if UserID is empty for backwards compat)
		if session.UserID == "" || session.UserID == currentUserID {
			activeSessions = append(activeSessions, session)
		}
	}

	// If no in-memory sessions, check database for recent sessions (handles backend restart case)
	// Query for sessions with status 'active' or 'running', or recently completed (within 30 minutes)
	if len(activeSessions) == 0 && api.chatDB != nil {
		thirtyMinutesAgo := time.Now().Add(-30 * time.Minute)

		// Get recent sessions from database - filter by user for isolation
		dbSessions, _, err := api.chatDB.ListChatSessionsWithUser(r.Context(), 20, 0, nil, nil, currentUserID)
		if err != nil {
			log.Printf("[ACTIVE_SESSION] Failed to query database for recent sessions: %v", err)
		} else {
			for _, dbSession := range dbSessions {
				// Include sessions that are active/running or recently completed
				isActive := dbSession.Status == "active" || dbSession.Status == "running"
				isRecentlyCompleted := dbSession.Status == "completed" && dbSession.LastActivity != nil && dbSession.LastActivity.After(thirtyMinutesAgo)

				if isActive || isRecentlyCompleted {
					// Convert to ActiveSessionInfo
					sessionInfo := &ActiveSessionInfo{
						SessionID:    dbSession.SessionID,
						AgentMode:    dbSession.AgentMode,
						Status:       dbSession.Status,
						CreatedAt:    dbSession.CreatedAt,
						Query:        dbSession.Title, // Use title as query summary
					}
					if dbSession.LastActivity != nil {
						sessionInfo.LastActivity = *dbSession.LastActivity
					} else {
						sessionInfo.LastActivity = dbSession.CreatedAt
					}

					// Map 'active' status to 'completed' for frontend consistency
					// (frontend expects 'running' or 'completed')
					if sessionInfo.Status == "active" {
						sessionInfo.Status = "completed"
					}

					activeSessions = append(activeSessions, sessionInfo)
					log.Printf("[ACTIVE_SESSION] Restored session from DB: %s (status: %s, last_activity: %v)", dbSession.SessionID, dbSession.Status, dbSession.LastActivity)
				}
			}
		}
	}

	response := GetActiveSessionsResponse{
		ActiveSessions: activeSessions,
		Total:          len(activeSessions),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleReconnectSession handles requests to reconnect to an active session
func (api *StreamingAPI) handleReconnectSession(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract session ID from URL
	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Check if session is active
	activeSession, exists := api.getActiveSession(sessionID)
	if !exists || activeSession.Status != "running" {
		http.Error(w, "Session not active or not found", http.StatusNotFound)
		return
	}

	// No observer needed - just return session info
	response := ReconnectSessionResponse{
		SessionID: sessionID,
		Status:    "reconnected",
		AgentMode: activeSession.AgentMode,
		Message:   "Successfully reconnected to active session",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleGetSessionStatus handles requests to get the status of a specific session
func (api *StreamingAPI) handleGetSessionStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract session ID from URL
	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	if sessionID == "" {
		http.Error(w, "Session ID is required", http.StatusBadRequest)
		return
	}

	// Get current user ID for session isolation
	currentUserID := GetUserIDFromContext(r.Context())

	// Check if session is active
	activeSession, exists := api.getActiveSession(sessionID)
	if !exists {
		// Check if session exists in database (completed) - filter by user for isolation
		chatSession, err := api.chatDB.GetChatSessionWithUser(r.Context(), sessionID, currentUserID)
		if err != nil {
			http.Error(w, "Session not found", http.StatusNotFound)
			return
		}

		// Return completed session info
		response := map[string]interface{}{
			"session_id":   sessionID,
			"status":       "completed",
			"agent_mode":   chatSession.AgentMode,
			"created_at":   chatSession.CreatedAt,
			"completed_at": chatSession.CompletedAt,
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
			return
		}
		return
	}

	// Return active session info
	response := map[string]interface{}{
		"session_id":    activeSession.SessionID,
		"status":        activeSession.Status,
		"agent_mode":    activeSession.AgentMode,
		"created_at":    activeSession.CreatedAt,
		"last_activity": activeSession.LastActivity,
		"query":         activeSession.Query,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}
