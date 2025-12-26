package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"mcp-agent-builder-go/agent_go/internal/events"

	"github.com/gorilla/mux"
)

// --- POLLING API TYPES ---
// Observer APIs removed - events are now stored by sessionID

// GetEventsResponse represents the response for event polling
type GetEventsResponse struct {
	Events             []events.Event `json:"events"`
	HasMore            bool           `json:"has_more"`
	SessionID          string         `json:"session_id"`
	SessionStatus      string         `json:"session_status,omitempty"`       // Session status: "running", "completed", "error", "stopped", "inactive"
	LastProcessedIndex int            `json:"last_processed_index,omitempty"` // Last index processed in unfiltered array (for correct sinceIndex tracking)
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
	if eventMode != "basic" && eventMode != "advanced" {
		http.Error(w, "event_mode must be 'basic' or 'advanced'", http.StatusBadRequest)
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

	// Get session status (from active sessions or database)
	var sessionStatus string
	activeSession, existsInActive := api.getActiveSession(sessionID)
	if existsInActive {
		sessionStatus = activeSession.Status
	} else {
		// Check database for completed/error sessions
		chatSession, err := api.chatDB.GetChatSession(r.Context(), sessionID)
		if err == nil && chatSession != nil {
			sessionStatus = chatSession.Status
		}
		// If not found, leave empty (session might not exist yet)
	}

	if !exists {
		// Session doesn't exist yet (no events have been added)
		// Return empty events array instead of 404 - this is expected when polling starts before events are generated
		response := GetEventsResponse{
			Events:        []events.Event{},
			HasMore:       false,
			SessionID:     sessionID,
			SessionStatus: sessionStatus,
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
	// Use hasMoreFromStore if available (for sinceIndex=0 case), otherwise calculate
	hasMore := hasMoreFromStore
	if !hasMoreFromStore {
		if opts.SinceIndex >= 0 {
			// Forward polling: has more if we got events (for normal polling, not initial fetch)
			// For sinceIndex=0, hasMoreFromStore is already set correctly
			if opts.SinceIndex > 0 {
				hasMore = len(sessionEvents) > 0
			}
		} else if opts.Limit > 0 {
			// Backward pagination: has more if there are more filtered events after current offset
			// totalCount is the total UNFILTERED events, but we need to check filtered count
			// Since we filter first then paginate, we can check if we got a full page
			// If we got fewer events than requested limit, we've reached the end
			hasMore = len(sessionEvents) >= opts.Limit
		}
	}

	response := GetEventsResponse{
		Events:             sessionEvents,
		HasMore:            hasMore,
		SessionID:          sessionID,
		SessionStatus:      sessionStatus,
		LastProcessedIndex: lastProcessedIndex,
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
func (api *StreamingAPI) handleGetActiveSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	activeSessions := api.getAllActiveSessions()

	// Filter only running sessions
	runningSessions := make([]*ActiveSessionInfo, 0)
	for _, session := range activeSessions {
		if session.Status == "running" {
			runningSessions = append(runningSessions, session)
		}
	}

	response := GetActiveSessionsResponse{
		ActiveSessions: runningSessions,
		Total:          len(runningSessions),
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

	// Check if session is active
	activeSession, exists := api.getActiveSession(sessionID)
	if !exists {
		// Check if session exists in database (completed)
		chatSession, err := api.chatDB.GetChatSession(r.Context(), sessionID)
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
