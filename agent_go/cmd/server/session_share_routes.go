package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/database"

	"github.com/gorilla/mux"
)

// isPostgresDB returns true if the underlying database is Postgres (SupabaseDB)
func isPostgresDB(db database.Database) bool {
	_, ok := db.(*database.SupabaseDB)
	return ok
}

// SessionShare represents a shared session link
type SessionShare struct {
	ID          string     `json:"id"`
	SessionID   string     `json:"session_id"`
	ShareToken  string     `json:"share_token"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	AccessLevel string     `json:"access_level"`
}

// CreateShareRequest represents a request to create a share link
type CreateShareRequest struct {
	ExpiresInHours *int `json:"expires_in_hours,omitempty"` // Optional expiration in hours
}

// ShareResponse represents the response when creating a share
type ShareResponse struct {
	ShareID   string     `json:"share_id"`
	ShareURL  string     `json:"share_url"`
	Token     string     `json:"token"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// SharedSessionResponse represents shared session data (read-only)
type SharedSessionResponse struct {
	SessionID   string          `json:"session_id"`
	Title       string          `json:"title"`
	AgentMode   string          `json:"agent_mode"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	Events      json.RawMessage `json:"events,omitempty"`
	IsShared    bool            `json:"is_shared"`
}

// handleCreateShare creates a share link for a session
func (api *StreamingAPI) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["session_id"]
	if sessionID == "" {
		http.Error(w, `{"error": "Session ID is required"}`, http.StatusBadRequest)
		return
	}

	user := GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error": "Not authenticated"}`, http.StatusUnauthorized)
		return
	}

	// Verify user owns this session (in multi-user mode)
	if IsMultiUserMode() {
		session, err := api.chatDB.GetChatSession(r.Context(), sessionID)
		if err != nil {
			http.Error(w, `{"error": "Session not found"}`, http.StatusNotFound)
			return
		}

		// Check if user owns this session by checking user_id in config or via database
		sessionUserID := api.getSessionUserID(r.Context(), session.SessionID)
		if sessionUserID != user.UserID {
			http.Error(w, `{"error": "You don't have permission to share this session"}`, http.StatusForbidden)
			return
		}
	}

	var req CreateShareRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			// Ignore decode errors - request body is optional
		}
	}

	// Generate share token
	shareToken := generateShareToken()
	shareID := generateShareToken()

	// Calculate expiration time if provided
	var expiresAt *time.Time
	if req.ExpiresInHours != nil && *req.ExpiresInHours > 0 {
		t := time.Now().Add(time.Duration(*req.ExpiresInHours) * time.Hour)
		expiresAt = &t
	}

	// Create share in database
	ctx := r.Context()
	err := api.createSessionShare(ctx, shareID, sessionID, shareToken, user.UserID, expiresAt)
	if err != nil {
		log.Printf("[SHARE] Failed to create share: %v", err)
		http.Error(w, `{"error": "Failed to create share link"}`, http.StatusInternalServerError)
		return
	}

	// Build share URL
	shareURL := "/shared/" + shareToken

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(ShareResponse{
		ShareID:   shareID,
		ShareURL:  shareURL,
		Token:     shareToken,
		ExpiresAt: expiresAt,
	})
}

// handleGetSharedSession retrieves a shared session by token (no auth required)
func (api *StreamingAPI) handleGetSharedSession(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	shareToken := vars["share_token"]
	if shareToken == "" {
		http.Error(w, `{"error": "Share token is required"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get share by token
	share, err := api.getSessionShareByToken(ctx, shareToken)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, `{"error": "Share link not found or expired"}`, http.StatusNotFound)
			return
		}
		log.Printf("[SHARE] Failed to get share: %v", err)
		http.Error(w, `{"error": "Failed to retrieve shared session"}`, http.StatusInternalServerError)
		return
	}

	// Check if share has expired
	if share.ExpiresAt != nil && share.ExpiresAt.Before(time.Now()) {
		http.Error(w, `{"error": "Share link has expired"}`, http.StatusGone)
		return
	}

	// Get session data
	session, err := api.chatDB.GetChatSession(ctx, share.SessionID)
	if err != nil {
		http.Error(w, `{"error": "Session not found"}`, http.StatusNotFound)
		return
	}

	// Determine event mode for filtering (default: micro to reduce payload)
	eventMode := r.URL.Query().Get("event_mode")
	if eventMode == "" {
		eventMode = "micro"
	}

	// Use a detached context for DB query (client disconnect shouldn't cancel mid-query)
	dbCtx, dbCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer dbCancel()

	// Get filtered events using SQL-level filtering for performance
	// session.ID is the internal hex ID used as chat_session_id in the events table
	filteredEvents, err := api.getFilteredEventsForShare(dbCtx, session.ID, eventMode)
	if err != nil {
		log.Printf("[SHARE] Failed to get events: %v", err)
		// Continue without events
	}
	log.Printf("[SHARE] Session %s: %d events after %s SQL filtering", share.SessionID, len(filteredEvents), eventMode)

	eventsJSON, _ := json.Marshal(filteredEvents)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SharedSessionResponse{
		SessionID:   session.SessionID,
		Title:       session.Title,
		AgentMode:   session.AgentMode,
		Status:      session.Status,
		CreatedAt:   session.CreatedAt,
		CompletedAt: session.CompletedAt,
		Events:      eventsJSON,
		IsShared:    true,
	})
}

// handleRevokeShare revokes a share link
func (api *StreamingAPI) handleRevokeShare(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["session_id"]
	shareID := vars["share_id"]

	if sessionID == "" || shareID == "" {
		http.Error(w, `{"error": "Session ID and share ID are required"}`, http.StatusBadRequest)
		return
	}

	user := GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error": "Not authenticated"}`, http.StatusUnauthorized)
		return
	}

	// Verify user owns the share
	ctx := r.Context()
	share, err := api.getSessionShareByID(ctx, shareID)
	if err != nil {
		http.Error(w, `{"error": "Share not found"}`, http.StatusNotFound)
		return
	}

	if share.CreatedBy != user.UserID && IsMultiUserMode() {
		http.Error(w, `{"error": "You don't have permission to revoke this share"}`, http.StatusForbidden)
		return
	}

	// Delete the share
	err = api.deleteSessionShare(ctx, shareID)
	if err != nil {
		log.Printf("[SHARE] Failed to delete share: %v", err)
		http.Error(w, `{"error": "Failed to revoke share link"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Share link revoked",
	})
}

// handleListShares lists all active shares for a session
func (api *StreamingAPI) handleListShares(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	sessionID := vars["session_id"]

	if sessionID == "" {
		http.Error(w, `{"error": "Session ID is required"}`, http.StatusBadRequest)
		return
	}

	user := GetUserFromContext(r.Context())
	if user == nil {
		http.Error(w, `{"error": "Not authenticated"}`, http.StatusUnauthorized)
		return
	}

	// Verify user owns this session
	if IsMultiUserMode() {
		sessionUserID := api.getSessionUserID(r.Context(), sessionID)
		if sessionUserID != user.UserID {
			http.Error(w, `{"error": "You don't have permission to view shares for this session"}`, http.StatusForbidden)
			return
		}
	}

	ctx := r.Context()
	shares, err := api.getSessionShares(ctx, sessionID)
	if err != nil {
		log.Printf("[SHARE] Failed to list shares: %v", err)
		http.Error(w, `{"error": "Failed to list shares"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"shares": shares,
	})
}

// Database helper functions

func (api *StreamingAPI) createSessionShare(ctx context.Context, id, sessionID, shareToken, createdBy string, expiresAt *time.Time) error {
	db := api.chatDB.GetDB()

	var query string
	if isPostgresDB(api.chatDB) {
		query = `INSERT INTO session_shares (id, session_id, share_token, created_by, expires_at, access_level)
			VALUES ($1, $2, $3, $4, $5, 'read')`
	} else {
		query = `INSERT INTO session_shares (id, session_id, share_token, created_by, expires_at, access_level)
			VALUES (?, ?, ?, ?, ?, 'read')`
	}

	_, err := db.ExecContext(ctx, query, id, sessionID, shareToken, createdBy, expiresAt)
	return err
}

func (api *StreamingAPI) getSessionShareByToken(ctx context.Context, token string) (*SessionShare, error) {
	db := api.chatDB.GetDB()

	var query string
	if isPostgresDB(api.chatDB) {
		query = `SELECT id, session_id, share_token, created_by, created_at, expires_at, access_level
			FROM session_shares WHERE share_token = $1`
	} else {
		query = `SELECT id, session_id, share_token, created_by, created_at, expires_at, access_level
			FROM session_shares WHERE share_token = ?`
	}

	var share SessionShare
	err := db.QueryRowContext(ctx, query, token).Scan(
		&share.ID, &share.SessionID, &share.ShareToken, &share.CreatedBy,
		&share.CreatedAt, &share.ExpiresAt, &share.AccessLevel,
	)
	if err != nil {
		return nil, err
	}

	return &share, nil
}

func (api *StreamingAPI) getSessionShareByID(ctx context.Context, id string) (*SessionShare, error) {
	db := api.chatDB.GetDB()

	var query string
	if isPostgresDB(api.chatDB) {
		query = `SELECT id, session_id, share_token, created_by, created_at, expires_at, access_level
			FROM session_shares WHERE id = $1`
	} else {
		query = `SELECT id, session_id, share_token, created_by, created_at, expires_at, access_level
			FROM session_shares WHERE id = ?`
	}

	var share SessionShare
	err := db.QueryRowContext(ctx, query, id).Scan(
		&share.ID, &share.SessionID, &share.ShareToken, &share.CreatedBy,
		&share.CreatedAt, &share.ExpiresAt, &share.AccessLevel,
	)
	if err != nil {
		return nil, err
	}

	return &share, nil
}

func (api *StreamingAPI) getSessionShares(ctx context.Context, sessionID string) ([]SessionShare, error) {
	db := api.chatDB.GetDB()

	var query string
	if isPostgresDB(api.chatDB) {
		query = `SELECT id, session_id, share_token, created_by, created_at, expires_at, access_level
			FROM session_shares WHERE session_id = $1 ORDER BY created_at DESC`
	} else {
		query = `SELECT id, session_id, share_token, created_by, created_at, expires_at, access_level
			FROM session_shares WHERE session_id = ? ORDER BY created_at DESC`
	}

	rows, err := db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var shares []SessionShare
	for rows.Next() {
		var share SessionShare
		err := rows.Scan(
			&share.ID, &share.SessionID, &share.ShareToken, &share.CreatedBy,
			&share.CreatedAt, &share.ExpiresAt, &share.AccessLevel,
		)
		if err != nil {
			return nil, err
		}
		shares = append(shares, share)
	}

	return shares, nil
}

func (api *StreamingAPI) deleteSessionShare(ctx context.Context, id string) error {
	db := api.chatDB.GetDB()

	var query string
	if isPostgresDB(api.chatDB) {
		query = `DELETE FROM session_shares WHERE id = $1`
	} else {
		query = `DELETE FROM session_shares WHERE id = ?`
	}
	_, err := db.ExecContext(ctx, query, id)
	return err
}

func (api *StreamingAPI) getSessionUserID(ctx context.Context, sessionID string) string {
	db := api.chatDB.GetDB()

	var query string
	if isPostgresDB(api.chatDB) {
		query = `SELECT user_id FROM chat_sessions WHERE session_id = $1`
	} else {
		query = `SELECT user_id FROM chat_sessions WHERE session_id = ?`
	}
	var userID sql.NullString
	err := db.QueryRowContext(ctx, query, sessionID).Scan(&userID)
	if err != nil || !userID.Valid {
		return ""
	}
	return userID.String
}

func generateShareToken() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// getFilteredEventsForShare retrieves events for a shared session with SQL-level event type filtering.
// This avoids fetching and unmarshaling events that will be discarded, improving performance significantly.
func (api *StreamingAPI) getFilteredEventsForShare(ctx context.Context, chatSessionID, eventMode string) ([]database.Event, error) {
	db := api.chatDB.GetDB()
	isPostgres := isPostgresDB(api.chatDB)

	// Build excluded event types based on mode
	var excludedTypes []string
	if eventMode != "advanced" {
		// NEVER_SHOW_EVENTS + ADVANCED_MODE_EVENTS (hidden in tiny/micro)
		excludedTypes = append(excludedTypes,
			"tool_execution", "tool_output", "tool_response", "tool_call_progress",
			"cache_event", "comprehensive_cache_event", "cache_hit", "cache_miss",
			"cache_write", "cache_expired", "cache_cleanup", "cache_error", "cache_operation_start",
			"llm_generation_start", "llm_generation_with_retry",
			"conversation_start", "conversation_turn",
		)
		if eventMode == "tiny" || eventMode == "micro" {
			// TINY_MODE_ADDITIONAL_EVENTS
			excludedTypes = append(excludedTypes,
				"user_message", "system_prompt", "agent_error",
				"llm_generation_end", "batch_execution_canceled",
			)
		}
	}

	// Build query with optional NOT IN filter
	var query string
	var args []interface{}

	if isPostgres {
		if len(excludedTypes) > 0 {
			placeholders := make([]string, len(excludedTypes))
			args = append(args, chatSessionID)
			for i, et := range excludedTypes {
				placeholders[i] = fmt.Sprintf("$%d", i+2)
				args = append(args, et)
			}
			query = fmt.Sprintf(`
				SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
				FROM events
				WHERE chat_session_id = $1 AND event_type NOT IN (%s)
				ORDER BY timestamp ASC
				LIMIT $%d`, strings.Join(placeholders, ", "), len(excludedTypes)+2)
			args = append(args, 5000)
		} else {
			query = `SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
				FROM events WHERE chat_session_id = $1 ORDER BY timestamp ASC LIMIT $2`
			args = []interface{}{chatSessionID, 5000}
		}
	} else {
		if len(excludedTypes) > 0 {
			placeholders := make([]string, len(excludedTypes))
			args = append(args, chatSessionID)
			for i, et := range excludedTypes {
				placeholders[i] = "?"
				args = append(args, et)
			}
			query = fmt.Sprintf(`
				SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
				FROM events
				WHERE chat_session_id = ? AND event_type NOT IN (%s)
				ORDER BY timestamp ASC
				LIMIT ?`, strings.Join(placeholders, ", "))
			args = append(args, 5000)
		} else {
			query = `SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
				FROM events WHERE chat_session_id = ? ORDER BY timestamp ASC LIMIT ?`
			args = []interface{}{chatSessionID, 5000}
		}
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}
	defer rows.Close()

	var dbEvents []database.Event
	for rows.Next() {
		var event database.Event
		var eventDataJSON string
		err := rows.Scan(
			&event.ID, &event.SessionID, &event.ChatSessionID,
			&event.EventType, &event.Timestamp, &eventDataJSON,
		)
		if err != nil {
			log.Printf("[SHARE] Skipping event with scan error: %v", err)
			continue
		}
		err = json.Unmarshal([]byte(eventDataJSON), &event.EventData)
		if err != nil {
			log.Printf("[SHARE] Skipping event with bad JSON: %v", err)
			continue
		}
		dbEvents = append(dbEvents, event)
	}

	return dbEvents, nil
}
