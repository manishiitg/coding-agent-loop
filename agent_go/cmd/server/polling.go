package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"mcp-agent/agent_go/internal/events"

	"github.com/gorilla/mux"
)

// --- POLLING API TYPES ---

// RegisterObserverRequest represents a request to register a new observer
type RegisterObserverRequest struct {
	SessionID string `json:"session_id,omitempty"`
}

// RegisterObserverResponse represents the response for observer registration
type RegisterObserverResponse struct {
	ObserverID string `json:"observer_id"`
	Status     string `json:"status"`
	Message    string `json:"message"`
}

// GetEventsResponse represents the response for event polling
type GetEventsResponse struct {
	Events         []events.Event `json:"events"`
	LastEventIndex int            `json:"last_event_index"`
	HasMore        bool           `json:"has_more"`
	ObserverID     string         `json:"observer_id"`
}

// ObserverStatusResponse represents the response for observer status
type ObserverStatusResponse struct {
	ObserverID   string    `json:"observer_id"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	LastActivity time.Time `json:"last_activity"`
	TotalEvents  int       `json:"total_events"`
}

// --- POLLING API HANDLERS ---

// handleRegisterObserver handles observer registration
func (api *StreamingAPI) handleRegisterObserver(w http.ResponseWriter, r *http.Request) {
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var req RegisterObserverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	// Register new observer
	observer := api.observerManager.RegisterObserver(req.SessionID)

	response := RegisterObserverResponse{
		ObserverID: observer.ID,
		Status:     "created",
		Message:    "Observer registered successfully",
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleGetEvents handles event polling for an observer
func (api *StreamingAPI) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract observer ID from URL
	vars := mux.Vars(r)
	observerID := vars["observer_id"]

	if observerID == "" {
		http.Error(w, "Observer ID is required", http.StatusBadRequest)
		return
	}

	// Get since parameter (optional)
	sinceStr := r.URL.Query().Get("since")
	sinceIndex := 0
	if sinceStr != "" {
		if since, err := strconv.Atoi(sinceStr); err == nil {
			sinceIndex = since
		}
	}

	// Update observer activity
	api.observerManager.UpdateObserverActivity(observerID)

	// Get events for observer
	fmt.Printf("[POLLING DEBUG] Getting events for observerID='%s', sinceIndex=%d\n", observerID, sinceIndex)
	events, totalEvents, exists := api.eventStore.GetEvents(observerID, sinceIndex)
	fmt.Printf("[POLLING DEBUG] Found %d events, totalEvents=%d, exists=%t\n", len(events), totalEvents, exists)

	if !exists {
		fmt.Printf("[POLLING DEBUG] Observer '%s' not found in event store\n", observerID)
		http.Error(w, "Observer not found", http.StatusNotFound)
		return
	}

	fmt.Printf("[POLLING DEBUG] Returning %d events to frontend:\n", len(events))
	for i, event := range events {
		fmt.Printf("  [%d] %s\n", i, event.Type)
	}

	response := GetEventsResponse{
		Events:         events,
		LastEventIndex: totalEvents,
		HasMore:        len(events) > 0,
		ObserverID:     observerID,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleGetObserverStatus handles observer status requests
func (api *StreamingAPI) handleGetObserverStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract observer ID from URL
	vars := mux.Vars(r)
	observerID := vars["observer_id"]

	if observerID == "" {
		http.Error(w, "Observer ID is required", http.StatusBadRequest)
		return
	}

	// Get observer
	observer, exists := api.observerManager.GetObserver(observerID)
	if !exists {
		http.Error(w, "Observer not found", http.StatusNotFound)
		return
	}

	// Get total events for this observer
	totalEvents, _ := api.eventStore.GetObserverStatus(observerID)

	response := ObserverStatusResponse{
		ObserverID:   observer.ID,
		Status:       observer.Status,
		CreatedAt:    observer.CreatedAt,
		LastActivity: observer.LastActivity,
		TotalEvents:  totalEvents,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, fmt.Sprintf("Failed to encode response: %v", err), http.StatusInternalServerError)
		return
	}
}

// handleRemoveObserver handles observer removal
func (api *StreamingAPI) handleRemoveObserver(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Extract observer ID from URL
	vars := mux.Vars(r)
	observerID := vars["observer_id"]

	if observerID == "" {
		http.Error(w, "Observer ID is required", http.StatusBadRequest)
		return
	}

	// Remove observer
	removed := api.observerManager.RemoveObserver(observerID)

	if !removed {
		http.Error(w, "Observer not found", http.StatusNotFound)
		return
	}

	response := map[string]interface{}{
		"status":  "deleted",
		"message": "Observer removed successfully",
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
	ObserverID string `json:"observer_id"`
	SessionID  string `json:"session_id"`
	Status     string `json:"status"`
	AgentMode  string `json:"agent_mode"`
	Message    string `json:"message"`
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

	// Create new observer for reconnection
	observer := api.observerManager.RegisterObserver(sessionID)

	response := ReconnectSessionResponse{
		ObserverID: observer.ID,
		SessionID:  sessionID,
		Status:     "reconnected",
		AgentMode:  activeSession.AgentMode,
		Message:    "Successfully reconnected to active session",
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
		"observer_id":   activeSession.ObserverID,
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
