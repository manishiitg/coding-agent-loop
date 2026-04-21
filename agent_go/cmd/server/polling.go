package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"mcp-agent-builder-go/agent_go/internal/events"

	"github.com/gorilla/mux"
)

// getSessionStatusString returns the session status for a given session ID
// from the in-memory active sessions map. Returns "" if not found, and
// denyAccess=true when the session belongs to another user.
func (api *StreamingAPI) getSessionStatusString(r *http.Request, sessionID string) (status string, denyAccess bool) {
	currentUserID := GetUserIDFromContext(r.Context())
	activeSession, existsInActive := api.getActiveSession(sessionID)
	if !existsInActive {
		return "", false
	}
	if activeSession.UserID != "" && activeSession.UserID != currentUserID {
		return "", true
	}
	return activeSession.Status, false
}

func (api *StreamingAPI) canSteerSession(sessionID string) bool {
	api.runningAgentsMux.RLock()
	defer api.runningAgentsMux.RUnlock()

	runningAgent, exists := api.runningAgents[sessionID]
	return exists && runningAgent != nil
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
	IsSyntheticTurn            bool           `json:"is_synthetic_turn,omitempty"`             // True when running auto-notification turn (frontend should not block input)
	CanSteer                   bool           `json:"can_steer,omitempty"`                     // True when a live foreground agent can accept steer injection
}

// --- POLLING API HANDLERS ---
// Observer registration/status/removal handlers removed - no longer needed

// handleGetEvents handles event polling for a session (new session-based API)
// Supports both forward polling (since parameter) and backward pagination (limit/offset)
// Also supports event mode filtering (event_mode parameter: "advanced", "tiny", or "micro")
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

	// Build options for GetEvents
	opts := events.GetEventsOptions{
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

	// Session status comes from the in-memory active sessions map.
	var sessionStatus string
	activeSession, existsInActive := api.getActiveSession(sessionID)
	if existsInActive {
		if activeSession.UserID != "" && activeSession.UserID != currentUserID {
			http.Error(w, "Session not found or access denied", http.StatusNotFound)
			return
		}
		sessionStatus = activeSession.Status
	}

	// If the session doesn't exist in the in-memory event store, return an
	// empty events payload. This happens when polling starts before events
	// are generated, or after the process has restarted and dropped state.
	if !exists {
		response := GetEventsResponse{
			Events:                     []events.Event{},
			HasMore:                    false,
			SessionID:                  sessionID,
			SessionStatus:              sessionStatus,
			LastProcessedIndex:         -1,
			HasRunningBackgroundAgents: api.bgAgentRegistry.HasRunningAgents(sessionID),
			CanSteer:                   api.canSteerSession(sessionID),
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
		CanSteer:                   api.canSteerSession(sessionID),
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

	// Sessions are in-memory only now — if activeSessions is empty after a
	// server restart, there are no durable sessions to restore. Workflow
	// restarts are handled independently via the /api/workflow/running API.

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

	activeSession, exists := api.getActiveSession(sessionID)
	if !exists {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}
	if activeSession.UserID != "" && activeSession.UserID != currentUserID {
		http.Error(w, "Session not found", http.StatusNotFound)
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
		"can_steer":     api.canSteerSession(sessionID),
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}
