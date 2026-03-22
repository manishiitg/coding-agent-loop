package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/mcpagent/events"

	_ "github.com/mattn/go-sqlite3"
)

// pendingEvent holds an event waiting to be batched
type pendingEvent struct {
	sessionID string
	event     *events.AgentEvent
	timestamp time.Time
}

// SQLiteDB implements the Database interface using SQLite
type SQLiteDB struct {
	db *sql.DB

	// Batching infrastructure
	batchMux         sync.Mutex
	eventBuffer      map[string][]pendingEvent // sessionID -> []pendingEvent
	chatSessionCache map[string]string         // sessionID -> chat_session_id (cached)
	flushTicker      *time.Ticker
	stopFlusher      chan struct{}
	flushDone        chan struct{}
	batchSizeLimit   int           // Maximum events per batch before flushing
	flushInterval    time.Duration // Time interval for periodic flushing
}

// validateWhereClause ensures the WHERE clause only contains safe, parameterized conditions
// This helps prevent SQL injection when building dynamic queries
func validateWhereClause(whereClause string) error {
	// Only allow WHERE/AND/OR followed by column names and = ? or other safe operators
	// This is a basic check - the real protection is using parameterized queries
	if strings.Contains(whereClause, ";") || strings.Contains(whereClause, "--") {
		return fmt.Errorf("unsafe WHERE clause detected")
	}
	return nil
}

// validateUpdateFields ensures UPDATE field names are from a whitelist
var allowedUpdateFields = map[string]bool{
	"label":                        true,
	"query":                        true,
	"selected_servers":             true,
	"selected_tools":               true,
	"selected_folder":              true,
	"agent_mode":                   true,
	"llm_config":                   true,
	"use_code_execution_mode":      true,
	"use_tool_search_mode":         true,
	"pre_discovered_tools":         true,
	"selected_skills":              true,
	"selected_secrets":             true,
	"selected_global_secret_names": true,
	"enable_browser_access":        true,
	"browser_mode":                 true,
	"workflow_status":              true,
	"selected_options":             true,
	"updated_at":                   true,
}

func validateUpdateField(field string) bool {
	// Extract field name from "field_name = ?" pattern
	parts := strings.Split(field, "=")
	if len(parts) != 2 {
		return false
	}
	fieldName := strings.TrimSpace(parts[0])
	return allowedUpdateFields[fieldName]
}

// NewSQLiteDB creates a new SQLite database connection
func NewSQLiteDB(dbPath string) (*SQLiteDB, error) {
	// Default to 10 connections for multi-user support
	maxConns := 10
	if val := os.Getenv("SQLITE_MAX_CONNECTIONS"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			maxConns = n
		}
	}

	// Build DSN with connection options
	// _journal_mode=WAL: better concurrency and works on network shares
	// _busy_timeout=10000: wait up to 10s if DB is busy (increased for multi-user)
	// _foreign_keys=1: enable foreign key constraints
	// cache=shared: shared cache for better concurrency
	lockingMode := ""
	if os.Getenv("SQLITE_EXCLUSIVE_LOCKING") == "true" {
		// EXCLUSIVE mode for network storage (Azure Files, SMB/CIFS)
		// This avoids POSIX file locking which doesn't work on SMB
		lockingMode = "&_locking_mode=EXCLUSIVE"
		maxConns = 1 // Must use single connection with exclusive locking
		log.Printf("[SQLITE] Using exclusive locking mode (single connection) for network storage compatibility")
	}

	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_busy_timeout=10000&_foreign_keys=1&cache=shared%s", dbPath, lockingMode)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for multi-user support
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns / 2)
	db.SetConnMaxLifetime(5 * time.Minute)

	log.Printf("[SQLITE] Connection pool configured: maxOpen=%d, maxIdle=%d", maxConns, maxConns/2)

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Run migrations (includes initial schema creation)
	migrationRunner := NewMigrationRunner(db, "sqlite3")
	if err := migrationRunner.RunMigrations("pkg/database/migrations"); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	sqliteDB := &SQLiteDB{
		db:               db,
		eventBuffer:      make(map[string][]pendingEvent),
		chatSessionCache: make(map[string]string),
		batchSizeLimit:   50,                     // Flush when batch reaches 50 events
		flushInterval:    500 * time.Millisecond, // Flush every 500ms
		stopFlusher:      make(chan struct{}),
		flushDone:        make(chan struct{}),
	}

	// Start background flusher
	sqliteDB.startFlusher()

	return sqliteDB, nil
}

// GetDB returns the underlying *sql.DB connection
// This is needed for integrations that require direct database access
func (s *SQLiteDB) GetDB() *sql.DB {
	return s.db
}

// CreateChatSession creates a new chat session
func (s *SQLiteDB) CreateChatSession(ctx context.Context, req *CreateChatSessionRequest) (*ChatSession, error) {
	query := `
		INSERT INTO chat_sessions (session_id, title, agent_mode, preset_query_id, config, status)
		VALUES (?, ?, ?, ?, ?, ?)
		RETURNING id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
	`

	log.Printf("[CREATE_CHAT_SESSION DEBUG] Creating session with SessionID: %s, Title: '%s' (length: %d), AgentMode: '%s'", req.SessionID, req.Title, len(req.Title), req.AgentMode)

	// Handle empty preset_query_id by converting to NULL
	var presetQueryID interface{}
	if req.PresetQueryID == "" {
		presetQueryID = nil
	} else {
		presetQueryID = req.PresetQueryID
	}

	// Handle config - convert to JSON string or NULL
	var configValue interface{}
	if len(req.Config) == 0 {
		configValue = nil
	} else {
		configValue = string(req.Config)
	}

	var session ChatSession
	var agentModeStr *string
	var presetQueryIDStr *string
	var configStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, req.SessionID, req.Title, req.AgentMode, presetQueryID, configValue, "active").Scan(
		&session.ID, &session.SessionID, &session.Title, &agentModeStr, &presetQueryIDStr, &configStr, &session.CreatedAt, &session.CompletedAt, &session.Status,
	)
	if err != nil {
		log.Printf("[CREATE_CHAT_SESSION ERROR] Failed to create chat session: %v", err)
		return nil, fmt.Errorf("failed to create chat session: %w", err)
	}

	// Handle NULL agent_mode
	if agentModeStr != nil {
		session.AgentMode = *agentModeStr
	} else {
		session.AgentMode = "" // Default to empty string for NULL values
	}

	// Handle NULL preset_query_id
	if presetQueryIDStr != nil {
		session.PresetQueryID = presetQueryIDStr
	}

	// Handle NULL config
	if configStr.Valid {
		session.Config = json.RawMessage(configStr.String)
	} else {
		session.Config = nil
	}

	log.Printf("[CREATE_CHAT_SESSION DEBUG] Successfully created session ID: %s, SessionID: %s, Title: '%s' (length: %d)", session.ID, session.SessionID, session.Title, len(session.Title))

	return &session, nil
}

// GetChatSession retrieves a chat session by session ID
func (s *SQLiteDB) GetChatSession(ctx context.Context, sessionID string) (*ChatSession, error) {
	query := `
		SELECT id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
		FROM chat_sessions
		WHERE session_id = ?
	`

	var session ChatSession
	var agentModeStr *string
	var presetQueryIDStr *string
	var configStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, sessionID).Scan(
		&session.ID, &session.SessionID, &session.Title, &agentModeStr, &presetQueryIDStr, &configStr, &session.CreatedAt, &session.CompletedAt, &session.Status,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("chat session not found")
		}
		return nil, fmt.Errorf("failed to get chat session: %w", err)
	}

	// Handle NULL agent_mode
	if agentModeStr != nil {
		session.AgentMode = *agentModeStr
	} else {
		session.AgentMode = "" // Default to empty string for NULL values
	}

	// Handle NULL preset_query_id
	if presetQueryIDStr != nil {
		session.PresetQueryID = presetQueryIDStr
	}

	// Handle NULL config
	if configStr.Valid {
		session.Config = json.RawMessage(configStr.String)
	} else {
		session.Config = nil
	}

	return &session, nil
}

// UpdateChatSession updates a chat session
func (s *SQLiteDB) UpdateChatSession(ctx context.Context, sessionID string, req *UpdateChatSessionRequest) (*ChatSession, error) {
	query := `
		UPDATE chat_sessions
		SET title = CASE 
		        WHEN ? = '' THEN title 
		        ELSE ? 
		    END,
		    agent_mode = COALESCE(NULLIF(?, ''), agent_mode),
		    preset_query_id = CASE
		        WHEN ? = '' THEN preset_query_id
		        ELSE COALESCE(NULLIF(?, ''), preset_query_id)
		    END,
		    config = CASE
		        WHEN ? IS NULL THEN config
		        ELSE ?
		    END,
		    status = COALESCE(NULLIF(?, ''), status),
		    completed_at = COALESCE(?, completed_at)
		WHERE session_id = ?
		RETURNING id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
	`

	var session ChatSession
	var agentModeStr *string
	var presetQueryIDStr *string
	var configStr sql.NullString

	// Handle config - convert to string or NULL
	var configValue interface{}
	if len(req.Config) == 0 {
		configValue = nil
	} else {
		configValue = string(req.Config)
	}

	// For title: pass it twice - first for the WHEN check, second for the ELSE value
	// If empty string, the CASE will return the existing title
	// For config: pass it twice - first for the WHEN check, second for the ELSE value
	err := s.db.QueryRowContext(ctx, query, req.Title, req.Title, req.AgentMode, req.PresetQueryID, req.PresetQueryID, configValue, configValue, req.Status, req.CompletedAt, sessionID).Scan(
		&session.ID, &session.SessionID, &session.Title, &agentModeStr, &presetQueryIDStr, &configStr, &session.CreatedAt, &session.CompletedAt, &session.Status,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("chat session not found")
		}
		return nil, fmt.Errorf("failed to update chat session: %w", err)
	}

	// Handle NULL agent_mode
	if agentModeStr != nil {
		session.AgentMode = *agentModeStr
	} else {
		session.AgentMode = "" // Default to empty string for NULL values
	}

	// Handle NULL preset_query_id
	if presetQueryIDStr != nil {
		session.PresetQueryID = presetQueryIDStr
	} else {
		session.PresetQueryID = nil // Default to nil for NULL values
	}

	// Handle NULL config
	if configStr.Valid {
		session.Config = json.RawMessage(configStr.String)
	} else {
		session.Config = nil
	}

	// If session is being marked as completed, flush any pending events immediately
	if req.Status == "completed" {
		go s.FlushSessionEvents(sessionID)
	}

	return &session, nil
}

// DeleteChatSession deletes a chat session and all its events
func (s *SQLiteDB) DeleteChatSession(ctx context.Context, sessionID string) error {
	query := `DELETE FROM chat_sessions WHERE session_id = ?`

	result, err := s.db.ExecContext(ctx, query, sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete chat session: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("chat session not found")
	}

	return nil
}

// DeleteWorkflowSessions deletes all chat sessions with agent_mode = 'workflow' and all their events
// Returns the number of sessions deleted
func (s *SQLiteDB) DeleteWorkflowSessions(ctx context.Context) (int64, error) {
	query := `DELETE FROM chat_sessions WHERE agent_mode = ?`

	result, err := s.db.ExecContext(ctx, query, AgentModeWorkflow)
	if err != nil {
		return 0, fmt.Errorf("failed to delete workflow sessions: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}

// ListChatSessions lists chat sessions with pagination
func (s *SQLiteDB) ListChatSessions(ctx context.Context, limit, offset int, presetQueryID *string, agentMode *string) ([]ChatHistorySummary, int, error) {
	// Build WHERE clause for filtering
	var whereConditions []string
	var args []interface{}

	if presetQueryID != nil && *presetQueryID != "" {
		whereConditions = append(whereConditions, "cs.preset_query_id = ?")
		args = append(args, *presetQueryID)
	}

	if agentMode != nil && *agentMode != "" {
		whereConditions = append(whereConditions, "cs.agent_mode = ?")
		args = append(args, *agentMode)
	}

	var whereClause string
	if len(whereConditions) > 0 {
		whereClause = " WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Validate WHERE clause for safety
	if err := validateWhereClause(whereClause); err != nil {
		return nil, 0, fmt.Errorf("invalid WHERE clause: %w", err)
	}

	// Get total count
	//nolint:gosec // G202: whereClause is validated and uses parameterized queries (?)
	countQuery := `SELECT COUNT(*) FROM chat_sessions cs` + whereClause
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get sessions with summary data
	// Optimized query: No event aggregation needed for sidebar list view
	// Events are only loaded when user clicks on a specific chat session
	// This makes the query much faster - just a simple SELECT with ORDER BY and LIMIT
	//nolint:gosec // G202: whereClause is validated and uses parameterized queries (?)
	query := `
		SELECT 
			cs.id,
			cs.session_id,
			cs.title,
			cs.agent_mode,
			cs.status,
			cs.created_at,
			cs.completed_at,
			cs.preset_query_id,
			cs.config,
			0 as total_events,
			0 as total_turns,
			NULL as last_activity
		FROM chat_sessions cs` + whereClause + `
		ORDER BY cs.created_at DESC
		LIMIT ? OFFSET ?
	`

	// Add limit and offset to args (only once, used in CTE)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list chat sessions: %w", err)
	}
	defer rows.Close()

	var sessions []ChatHistorySummary
	for rows.Next() {
		var session ChatHistorySummary
		var lastActivityStr *string
		var agentModeStr *string
		var presetQueryIDStr *string
		var configStr sql.NullString
		err := rows.Scan(
			&session.ChatSessionID, &session.SessionID, &session.Title, &agentModeStr, &session.Status,
			&session.CreatedAt, &session.CompletedAt, &presetQueryIDStr, &configStr, &session.TotalEvents, &session.TotalTurns, &lastActivityStr,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan session: %w", err)
		}

		// Handle NULL agent_mode
		if agentModeStr != nil {
			session.AgentMode = *agentModeStr
		} else {
			session.AgentMode = "" // Default to empty string for NULL values
		}

		// Handle NULL preset_query_id
		if presetQueryIDStr != nil {
			session.PresetQueryID = *presetQueryIDStr
		} else {
			session.PresetQueryID = "" // Default to empty string for NULL values
		}

		// Handle NULL config
		if configStr.Valid {
			session.Config = json.RawMessage(configStr.String)
		} else {
			session.Config = nil
		}

		// Parse lastActivity string to time.Time (can be NULL since we don't load events for list view)
		if lastActivityStr != nil {
			if lastActivity, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", *lastActivityStr); err == nil {
				session.LastActivity = &lastActivity
			} else {
				// If parsing fails, leave as nil (not needed for sidebar)
				session.LastActivity = nil
			}
		} else {
			// No last activity (we don't load events for list view)
			session.LastActivity = nil
		}

		sessions = append(sessions, session)
	}

	return sessions, total, nil
}

// StoreEvent stores an event in the database (batched)
func (s *SQLiteDB) StoreEvent(ctx context.Context, sessionID string, event *events.AgentEvent) error {
	s.batchMux.Lock()
	defer s.batchMux.Unlock()

	// Add event to buffer
	if s.eventBuffer[sessionID] == nil {
		s.eventBuffer[sessionID] = make([]pendingEvent, 0, s.batchSizeLimit)
	}

	s.eventBuffer[sessionID] = append(s.eventBuffer[sessionID], pendingEvent{
		sessionID: sessionID,
		event:     event,
		timestamp: time.Now(),
	})

	// Check if we need to flush immediately due to batch size
	if len(s.eventBuffer[sessionID]) >= s.batchSizeLimit {
		// Flush this session's events asynchronously
		go s.flushSessionEvents(sessionID)
	}

	return nil
}

// getChatSessionID gets the chat_session_id for a session, using cache if available
// extractParentSessionID extracts the parent session ID from a sub-agent session ID.
// Sub-agent session IDs have format: {parent_session_id}-sub-{n}-{timestamp}
// Returns the original sessionID if it's not a sub-agent session.
func extractParentSessionIDSQLite(sessionID string) string {
	// Check if this is a sub-agent session ID
	if idx := strings.Index(sessionID, "-sub-"); idx != -1 {
		return sessionID[:idx]
	}
	return sessionID
}

func (s *SQLiteDB) getChatSessionID(ctx context.Context, sessionID string) (string, error) {
	// Extract parent session ID for sub-agents
	lookupSessionID := extractParentSessionIDSQLite(sessionID)

	// Check cache first
	s.batchMux.Lock()
	if cachedID, ok := s.chatSessionCache[lookupSessionID]; ok {
		s.batchMux.Unlock()
		// Also cache for the original sub-agent session ID
		if lookupSessionID != sessionID {
			s.batchMux.Lock()
			s.chatSessionCache[sessionID] = cachedID
			s.batchMux.Unlock()
		}
		return cachedID, nil
	}
	s.batchMux.Unlock()

	// Cache miss - fetch from database
	chatSession, err := s.GetChatSession(ctx, lookupSessionID)
	if err != nil {
		return "", fmt.Errorf("failed to get chat session: %w", err)
	}

	// Update cache
	s.batchMux.Lock()
	s.chatSessionCache[lookupSessionID] = chatSession.ID
	// Also cache for the original sub-agent session ID
	if lookupSessionID != sessionID {
		s.chatSessionCache[sessionID] = chatSession.ID
	}
	s.batchMux.Unlock()

	return chatSession.ID, nil
}

// flushSessionEvents flushes all pending events for a specific session
func (s *SQLiteDB) flushSessionEvents(sessionID string) {
	s.batchMux.Lock()
	events := s.eventBuffer[sessionID]
	if len(events) == 0 {
		s.batchMux.Unlock()
		return
	}
	// Copy events to avoid holding lock during DB operations
	// Don't delete from buffer yet - only delete after successful commit
	eventsCopy := make([]pendingEvent, len(events))
	copy(eventsCopy, events)
	s.batchMux.Unlock()

	// Flush events in a transaction
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[BATCH ERROR] Failed to begin transaction for session %s: %v", sessionID, err)
		return
	}

	// Get chat_session_id (with caching)
	chatSessionID, err := s.getChatSessionID(ctx, sessionID)
	if err != nil {
		tx.Rollback()
		log.Printf("[BATCH ERROR] Failed to get chat session ID for session %s: %v", sessionID, err)
		return
	}

	// Prepare batch insert statement
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO events (session_id, chat_session_id, event_type, timestamp, event_data)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		tx.Rollback()
		log.Printf("[BATCH ERROR] Failed to prepare statement for session %s: %v", sessionID, err)
		return
	}
	defer stmt.Close()

	// Insert all events in the batch
	for _, pending := range eventsCopy {
		eventData, err := json.Marshal(pending.event)
		if err != nil {
			log.Printf("[BATCH ERROR] Failed to marshal event for session %s: %v", sessionID, err)
			continue
		}

		_, err = stmt.ExecContext(ctx, pending.sessionID, chatSessionID, pending.event.Type, pending.event.Timestamp, string(eventData))
		if err != nil {
			log.Printf("[BATCH ERROR] Failed to insert event for session %s: %v", sessionID, err)
			// Continue with other events even if one fails
			continue
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		log.Printf("[BATCH ERROR] Failed to commit transaction for session %s: %v", sessionID, err)
		// Events remain in buffer for retry on next flush cycle
		return
	}

	// Only clear the buffer after successful commit
	s.batchMux.Lock()
	// Double-check that the events we flushed are still the same (in case new events were added)
	// If new events were added, we only remove the ones we successfully flushed
	if len(s.eventBuffer[sessionID]) >= len(eventsCopy) {
		// Remove the flushed events from the front of the buffer
		s.eventBuffer[sessionID] = s.eventBuffer[sessionID][len(eventsCopy):]
		// If buffer is now empty, remove the session entry
		if len(s.eventBuffer[sessionID]) == 0 {
			delete(s.eventBuffer, sessionID)
		}
	} else {
		// Buffer was modified - clear it entirely to be safe
		delete(s.eventBuffer, sessionID)
	}
	s.batchMux.Unlock()

	log.Printf("[BATCH] Flushed %d events for session %s", len(eventsCopy), sessionID)
}

// flushBatches flushes all pending event batches
func (s *SQLiteDB) flushBatches() {
	s.batchMux.Lock()
	// Copy all sessions that need flushing
	sessionsToFlush := make([]string, 0, len(s.eventBuffer))
	for sessionID := range s.eventBuffer {
		if len(s.eventBuffer[sessionID]) > 0 {
			sessionsToFlush = append(sessionsToFlush, sessionID)
		}
	}
	s.batchMux.Unlock()

	// Flush each session's events
	for _, sessionID := range sessionsToFlush {
		s.flushSessionEvents(sessionID)
	}
}

// FlushSessionEvents flushes all pending events for a specific session
// This can be called when a session completes to ensure all events are persisted immediately
func (s *SQLiteDB) FlushSessionEvents(sessionID string) {
	s.flushSessionEvents(sessionID)
}

// startFlusher starts the background goroutine that periodically flushes batches
func (s *SQLiteDB) startFlusher() {
	s.flushTicker = time.NewTicker(s.flushInterval)
	go func() {
		for {
			select {
			case <-s.flushTicker.C:
				s.flushBatches()
			case <-s.stopFlusher:
				// Final flush before stopping
				s.flushBatches()
				close(s.flushDone)
				return
			}
		}
	}()
}

// GetEvents retrieves events based on the request
func (s *SQLiteDB) GetEvents(ctx context.Context, req *GetChatHistoryRequest) (*GetEventsResponse, error) {
	// Build query
	whereClause := "WHERE 1=1"
	args := []interface{}{}

	if req.SessionID != "" {
		whereClause += " AND session_id = ?"
		args = append(args, req.SessionID)
	}

	if req.EventType != "" {
		whereClause += " AND event_type = ?"
		args = append(args, req.EventType)
	}

	if !req.FromDate.IsZero() {
		whereClause += " AND timestamp >= ?"
		args = append(args, req.FromDate)
	}

	if !req.ToDate.IsZero() {
		whereClause += " AND timestamp <= ?"
		args = append(args, req.ToDate)
	}

	// Validate WHERE clause for safety
	if err := validateWhereClause(whereClause); err != nil {
		return nil, fmt.Errorf("invalid WHERE clause: %w", err)
	}

	// Get total count
	//nolint:gosec // G201: whereClause is validated and uses parameterized queries (?)
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM events %s", whereClause)
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get events
	limit := req.Limit
	if limit <= 0 {
		limit = 100 // Default limit
	}

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	//nolint:gosec // G201: whereClause is validated and uses parameterized queries (?)
	query := fmt.Sprintf(`
		SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
		FROM events %s
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}
	defer rows.Close()

	var eventList []Event
	for rows.Next() {
		var event Event
		var eventDataJSON string
		err := rows.Scan(
			&event.ID, &event.SessionID, &event.ChatSessionID, &event.EventType, &event.Timestamp, &eventDataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Unmarshal event data
		err = json.Unmarshal([]byte(eventDataJSON), &event.EventData)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
		}

		eventList = append(eventList, event)
	}

	return &GetEventsResponse{
		Events: eventList,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

// GetEventsBySession retrieves events for a chat session
// The sessionID parameter is the chat_session_id (UUID from chat_sessions table),
// not the internal trace/session_id used during event emission
func (s *SQLiteDB) GetEventsBySession(ctx context.Context, sessionID string, limit, offset int) ([]Event, error) {
	// Resolve session_id UUID → internal hex id first, then query by chat_session_id.
	// Two-step approach avoids OR which prevents index usage on large tables.
	var internalID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM chat_sessions WHERE session_id = ?`, sessionID,
	).Scan(&internalID)
	if err != nil {
		// Fallback: try using sessionID directly as chat_session_id
		internalID = sessionID
	}

	query := `
		SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
		FROM events
		WHERE chat_session_id = ?
		ORDER BY timestamp ASC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, internalID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get events by session: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		var eventDataJSON string
		err := rows.Scan(
			&event.ID, &event.SessionID, &event.ChatSessionID, &event.EventType, &event.Timestamp, &eventDataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		// Unmarshal event data
		err = json.Unmarshal([]byte(eventDataJSON), &event.EventData)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
		}

		events = append(events, event)
	}

	return events, nil
}

// GetEventsByCorrelationID retrieves events for a session filtered by correlation_id (stored in event_data JSON)
func (s *SQLiteDB) GetEventsByCorrelationID(ctx context.Context, sessionID string, correlationID string, limit, offset int) ([]Event, error) {
	// Resolve session_id UUID → internal hex id
	var internalID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM chat_sessions WHERE session_id = ?`, sessionID,
	).Scan(&internalID)
	if err != nil {
		internalID = sessionID
	}

	query := `
		SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
		FROM events
		WHERE chat_session_id = ?
		  AND json_extract(event_data, '$.correlation_id') = ?
		ORDER BY timestamp ASC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, internalID, correlationID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to get events by correlation_id: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		var eventDataJSON string
		err := rows.Scan(
			&event.ID, &event.SessionID, &event.ChatSessionID, &event.EventType, &event.Timestamp, &eventDataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}

		err = json.Unmarshal([]byte(eventDataJSON), &event.EventData)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
		}

		events = append(events, event)
	}

	return events, nil
}

// CountEventsBySession returns the total number of events for a session (O(1) with index)
func (s *SQLiteDB) CountEventsBySession(ctx context.Context, sessionID string) (int, error) {
	var internalID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM chat_sessions WHERE session_id = ?`, sessionID,
	).Scan(&internalID)
	if err != nil {
		internalID = sessionID
	}

	var count int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE chat_session_id = ?`, internalID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count events: %w", err)
	}
	return count, nil
}

// Ping tests the database connection
func (s *SQLiteDB) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// CreatePresetQuery creates a new preset query
func (s *SQLiteDB) CreatePresetQuery(ctx context.Context, req *CreatePresetQueryRequest) (*PresetQuery, error) {
	// Validate the request
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Convert selected servers to JSON
	selectedServersJSON := "[]"
	if len(req.SelectedServers) > 0 {
		serversJSON, err := json.Marshal(req.SelectedServers)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected servers: %w", err)
		}
		selectedServersJSON = string(serversJSON)
	}

	// Convert selected tools to JSON
	selectedToolsJSON := "[]"
	if len(req.SelectedTools) > 0 {
		toolsJSON, err := json.Marshal(req.SelectedTools)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected tools: %w", err)
		}
		selectedToolsJSON = string(toolsJSON)
	}

	// Prepare LLM config for insert (NULL when absent)
	var llmConfigParam interface{}
	if req.LLMConfig != nil {
		llmConfigBytes, err := json.Marshal(req.LLMConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal LLM config: %w", err)
		}
		llmConfigParam = string(llmConfigBytes)
	} else {
		llmConfigParam = nil
	}

	// Set default agent mode if not provided
	agentMode := req.AgentMode
	if agentMode == "" {
		agentMode = AgentModeSimple // Use constant for default
	}

	// Convert use_code_execution_mode boolean to INTEGER (0/1) for SQLite
	useCodeExecutionModeInt := 0
	if req.UseCodeExecutionMode {
		useCodeExecutionModeInt = 1
	}

	// Convert use_tool_search_mode boolean to INTEGER (0/1) for SQLite
	useToolSearchModeInt := 0
	if req.UseToolSearchMode {
		useToolSearchModeInt = 1
	}

	// Convert pre_discovered_tools to JSON
	preDiscoveredToolsJSON := "[]"
	if len(req.PreDiscoveredTools) > 0 {
		toolsJSON, err := json.Marshal(req.PreDiscoveredTools)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal pre-discovered tools: %w", err)
		}
		preDiscoveredToolsJSON = string(toolsJSON)
	}

	// Convert selected_skills to JSON
	selectedSkillsJSON := "[]"
	if len(req.SelectedSkills) > 0 {
		skillsJSON, err := json.Marshal(req.SelectedSkills)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected skills: %w", err)
		}
		selectedSkillsJSON = string(skillsJSON)
	}

	// Convert enable_browser_access boolean to INTEGER (0/1) for SQLite
	enableBrowserAccessInt := 0
	if req.EnableBrowserAccess {
		enableBrowserAccessInt = 1
	}

	// Convert selected_secrets to JSON
	selectedSecretsJSON := "[]"
	if len(req.SelectedSecrets) > 0 {
		secretsJSON, err := json.Marshal(req.SelectedSecrets)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected secrets: %w", err)
		}
		selectedSecretsJSON = string(secretsJSON)
	}

	// Convert selected_global_secret_names to JSON (nil = all selected, stored as NULL)
	var selectedGlobalSecretNamesParam interface{}
	if req.SelectedGlobalSecretNames != nil {
		gsJSON, err := json.Marshal(*req.SelectedGlobalSecretNames)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected global secret names: %w", err)
		}
		selectedGlobalSecretNamesParam = string(gsJSON)
	}

	// Resolve browser_mode: prefer explicit BrowserMode, fall back from EnableBrowserAccess for backward compat
	browserMode := req.BrowserMode
	if browserMode == "" && req.EnableBrowserAccess {
		browserMode = "headless" // Legacy: enable_browser_access=true without browser_mode defaults to headless
	}

	query := `
		INSERT INTO preset_queries (label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, browser_mode, is_predefined, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, browser_mode, is_predefined, created_at, updated_at, created_by
	`

	var preset PresetQuery
	var selectedServersStr string
	var selectedToolsStr string
	var selectedFolderStr sql.NullString
	var llmConfigNullStr sql.NullString
	var returnedUseCodeExecutionModeInt int
	var returnedUseToolSearchModeInt int
	var preDiscoveredToolsStr string
	var selectedSkillsStr string
	var selectedSecretsStr string
	var selectedGlobalSecretNamesStr sql.NullString
	var returnedEnableBrowserAccessInt int
	err := s.db.QueryRowContext(ctx, query, req.Label, req.Query, selectedServersJSON, selectedToolsJSON, req.SelectedFolder, agentMode, llmConfigParam, useCodeExecutionModeInt, useToolSearchModeInt, preDiscoveredToolsJSON, selectedSkillsJSON, selectedSecretsJSON, selectedGlobalSecretNamesParam, enableBrowserAccessInt, browserMode, req.IsPredefined, "user").Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &returnedUseCodeExecutionModeInt, &returnedUseToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &returnedEnableBrowserAccessInt, &preset.BrowserMode, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create preset query: %w", err)
	}

	// Parse selected servers JSON
	preset.SelectedServers = selectedServersStr
	preset.SelectedTools = selectedToolsStr
	preset.SelectedFolder = selectedFolderStr
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = json.RawMessage("null")
	}
	// Convert INTEGER to boolean
	preset.UseCodeExecutionMode = returnedUseCodeExecutionModeInt != 0
	preset.UseToolSearchMode = returnedUseToolSearchModeInt != 0
	preset.PreDiscoveredTools = preDiscoveredToolsStr
	preset.SelectedSkills = selectedSkillsStr
	preset.SelectedSecrets = selectedSecretsStr
	if selectedGlobalSecretNamesStr.Valid {
		preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
	}
	preset.EnableBrowserAccess = returnedEnableBrowserAccessInt != 0

	return &preset, nil
}

// GetPresetQuery retrieves a preset query by ID
func (s *SQLiteDB) GetPresetQuery(ctx context.Context, id string) (*PresetQuery, error) {
	query := `
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, browser_mode, is_predefined, employee_id, created_at, updated_at, created_by
		FROM preset_queries
		WHERE id = ?
	`

	var preset PresetQuery
	var selectedServersStr string
	var selectedToolsStr string
	var selectedFolderStr sql.NullString
	var llmConfigNullStr sql.NullString
	var useCodeExecutionModeInt int
	var useToolSearchModeInt int
	var preDiscoveredToolsStr sql.NullString
	var selectedSkillsStr sql.NullString
	var selectedSecretsStr sql.NullString
	var selectedGlobalSecretNamesStr sql.NullString
	var enableBrowserAccessInt sql.NullInt64
	var browserModeStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &useCodeExecutionModeInt, &useToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &enableBrowserAccessInt, &browserModeStr, &preset.IsPredefined, &preset.EmployeeID, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("preset query not found")
		}
		return nil, fmt.Errorf("failed to get preset query: %w", err)
	}

	preset.SelectedServers = selectedServersStr
	preset.SelectedTools = selectedToolsStr
	preset.SelectedFolder = selectedFolderStr
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = nil
	}
	// Convert INTEGER to boolean
	preset.UseCodeExecutionMode = useCodeExecutionModeInt != 0
	preset.UseToolSearchMode = useToolSearchModeInt != 0
	if preDiscoveredToolsStr.Valid {
		preset.PreDiscoveredTools = preDiscoveredToolsStr.String
	} else {
		preset.PreDiscoveredTools = "[]"
	}
	if selectedSkillsStr.Valid {
		preset.SelectedSkills = selectedSkillsStr.String
	} else {
		preset.SelectedSkills = "[]"
	}
	if selectedSecretsStr.Valid {
		preset.SelectedSecrets = selectedSecretsStr.String
	} else {
		preset.SelectedSecrets = "[]"
	}
	if selectedGlobalSecretNamesStr.Valid {
		preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
	}
	preset.EnableBrowserAccess = enableBrowserAccessInt.Valid && enableBrowserAccessInt.Int64 != 0
	if browserModeStr.Valid {
		preset.BrowserMode = browserModeStr.String
	}

	return &preset, nil
}

// UpdatePresetQuery updates a preset query
func (s *SQLiteDB) UpdatePresetQuery(ctx context.Context, id string, req *UpdatePresetQueryRequest) (*PresetQuery, error) {
	// Validate the request
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Build dynamic update query
	updateFields := []string{}
	args := []interface{}{}
	if req.Label != "" {
		updateFields = append(updateFields, "label = ?")
		args = append(args, req.Label)
	}

	if req.Query != "" {
		updateFields = append(updateFields, "query = ?")
		args = append(args, req.Query)
	}

	if req.SelectedServers != nil {
		selectedServersJSON := "[]"
		if len(req.SelectedServers) > 0 {
			serversJSON, err := json.Marshal(req.SelectedServers)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal selected servers: %w", err)
			}
			selectedServersJSON = string(serversJSON)
		}
		updateFields = append(updateFields, "selected_servers = ?")
		args = append(args, selectedServersJSON)
	}

	if req.SelectedTools != nil {
		selectedToolsJSON := "[]"
		if len(req.SelectedTools) > 0 {
			toolsJSON, err := json.Marshal(req.SelectedTools)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal selected tools: %w", err)
			}
			selectedToolsJSON = string(toolsJSON)
		}
		updateFields = append(updateFields, "selected_tools = ?")
		args = append(args, selectedToolsJSON)
	}

	if req.SelectedFolder != "" {
		updateFields = append(updateFields, "selected_folder = ?")
		args = append(args, req.SelectedFolder)
	}

	if req.AgentMode != "" {
		updateFields = append(updateFields, "agent_mode = ?")
		args = append(args, req.AgentMode)
	}

	if req.LLMConfig != nil {
		llmConfigBytes, err := json.Marshal(req.LLMConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal LLM config: %w", err)
		}
		updateFields = append(updateFields, "llm_config = ?")
		args = append(args, string(llmConfigBytes))
	}

	if req.UseCodeExecutionMode != nil {
		// Convert boolean to INTEGER (0/1) for SQLite
		useCodeExecutionModeInt := 0
		if *req.UseCodeExecutionMode {
			useCodeExecutionModeInt = 1
		}
		updateFields = append(updateFields, "use_code_execution_mode = ?")
		args = append(args, useCodeExecutionModeInt)
	}

	if req.UseToolSearchMode != nil {
		// Convert boolean to INTEGER (0/1) for SQLite
		useToolSearchModeInt := 0
		if *req.UseToolSearchMode {
			useToolSearchModeInt = 1
		}
		updateFields = append(updateFields, "use_tool_search_mode = ?")
		args = append(args, useToolSearchModeInt)
	}

	if req.PreDiscoveredTools != nil {
		preDiscoveredToolsJSON := "[]"
		if len(req.PreDiscoveredTools) > 0 {
			toolsJSON, err := json.Marshal(req.PreDiscoveredTools)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal pre-discovered tools: %w", err)
			}
			preDiscoveredToolsJSON = string(toolsJSON)
		}
		updateFields = append(updateFields, "pre_discovered_tools = ?")
		args = append(args, preDiscoveredToolsJSON)
	}

	if req.SelectedSkills != nil {
		selectedSkillsJSON := "[]"
		if len(req.SelectedSkills) > 0 {
			skillsJSON, err := json.Marshal(req.SelectedSkills)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal selected skills: %w", err)
			}
			selectedSkillsJSON = string(skillsJSON)
		}
		updateFields = append(updateFields, "selected_skills = ?")
		args = append(args, selectedSkillsJSON)
	}

	if req.SelectedSecrets != nil {
		selectedSecretsJSON := "[]"
		if len(req.SelectedSecrets) > 0 {
			secretsJSON, err := json.Marshal(req.SelectedSecrets)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal selected secrets: %w", err)
			}
			selectedSecretsJSON = string(secretsJSON)
		}
		updateFields = append(updateFields, "selected_secrets = ?")
		args = append(args, selectedSecretsJSON)
	}

	if req.SelectedGlobalSecretNames != nil {
		gsJSON, err := json.Marshal(*req.SelectedGlobalSecretNames)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected global secret names: %w", err)
		}
		log.Printf("[PRESET_UPDATE] selected_global_secret_names is NOT nil, storing: %s", string(gsJSON))
		updateFields = append(updateFields, "selected_global_secret_names = ?")
		args = append(args, string(gsJSON))
	} else {
		log.Printf("[PRESET_UPDATE] selected_global_secret_names is nil, NOT updating")
	}

	if req.EnableBrowserAccess != nil {
		// Deprecated: use BrowserMode. Convert boolean to INTEGER (0/1) for SQLite
		enableBrowserAccessInt := 0
		if *req.EnableBrowserAccess {
			enableBrowserAccessInt = 1
		}
		updateFields = append(updateFields, "enable_browser_access = ?")
		args = append(args, enableBrowserAccessInt)
	}

	if req.BrowserMode != "" {
		updateFields = append(updateFields, "browser_mode = ?")
		args = append(args, req.BrowserMode)
	}

	if len(updateFields) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	// Validate all update fields are from whitelist
	for _, field := range updateFields {
		if !validateUpdateField(field) {
			return nil, fmt.Errorf("invalid update field: %s", field)
		}
	}

	updateFields = append(updateFields, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	//nolint:gosec // G201: updateFields are validated against whitelist and use parameterized queries (?)
	query := fmt.Sprintf(`
		UPDATE preset_queries
		SET %s
		WHERE id = ?
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, browser_mode, is_predefined, created_at, updated_at, created_by
	`, strings.Join(updateFields, ", "))

	var preset PresetQuery
	var selectedServersStr string
	var selectedToolsStr string
	var selectedFolderStr sql.NullString
	var llmConfigNullStr sql.NullString
	var useCodeExecutionModeInt int
	var useToolSearchModeInt int
	var preDiscoveredToolsStr sql.NullString
	var selectedSkillsStr sql.NullString
	var selectedSecretsStr sql.NullString
	var selectedGlobalSecretNamesStr sql.NullString
	var enableBrowserAccessInt sql.NullInt64
	var browserModeNullStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &useCodeExecutionModeInt, &useToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &enableBrowserAccessInt, &browserModeNullStr, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("preset query not found")
		}
		return nil, fmt.Errorf("failed to update preset query: %w", err)
	}

	preset.SelectedServers = selectedServersStr
	preset.SelectedTools = selectedToolsStr
	preset.SelectedFolder = selectedFolderStr
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = nil
	}
	// Convert INTEGER to boolean
	preset.UseCodeExecutionMode = useCodeExecutionModeInt != 0
	preset.UseToolSearchMode = useToolSearchModeInt != 0
	preset.EnableBrowserAccess = enableBrowserAccessInt.Valid && enableBrowserAccessInt.Int64 != 0
	if browserModeNullStr.Valid {
		preset.BrowserMode = browserModeNullStr.String
	}
	if preDiscoveredToolsStr.Valid {
		preset.PreDiscoveredTools = preDiscoveredToolsStr.String
	} else {
		preset.PreDiscoveredTools = "[]"
	}
	if selectedSkillsStr.Valid {
		preset.SelectedSkills = selectedSkillsStr.String
	} else {
		preset.SelectedSkills = "[]"
	}
	if selectedSecretsStr.Valid {
		preset.SelectedSecrets = selectedSecretsStr.String
	} else {
		preset.SelectedSecrets = "[]"
	}
	if selectedGlobalSecretNamesStr.Valid {
		preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
	}

	return &preset, nil
}

// DeletePresetQuery deletes a preset query
func (s *SQLiteDB) DeletePresetQuery(ctx context.Context, id string) error {
	query := `DELETE FROM preset_queries WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete preset query: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("preset query not found")
	}

	return nil
}

// ListPresetQueries lists preset queries with pagination
func (s *SQLiteDB) ListPresetQueries(ctx context.Context, limit, offset int) ([]PresetQuery, int, error) {
	// Get total count
	countQuery := `SELECT COUNT(*) FROM preset_queries`
	var total int
	err := s.db.QueryRowContext(ctx, countQuery).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get presets
	query := `
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, browser_mode, is_predefined, employee_id, created_at, updated_at, created_by
		FROM preset_queries
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list preset queries: %w", err)
	}
	defer rows.Close()

	presets := make([]PresetQuery, 0) // Initialize as empty slice, not nil
	for rows.Next() {
		var preset PresetQuery
		var selectedServersStr string
		var selectedToolsStr string
		var selectedFolderStr sql.NullString
		var llmConfigNullStr sql.NullString
		var useCodeExecutionModeInt int
		var useToolSearchModeInt int
		var preDiscoveredToolsStr sql.NullString
		var selectedSkillsStr sql.NullString
		var selectedSecretsStr sql.NullString
		var selectedGlobalSecretNamesStr sql.NullString
		var enableBrowserAccessInt sql.NullInt64
		var browserModeNullStr sql.NullString
		var employeeIDStr sql.NullString
		err := rows.Scan(
			&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &useCodeExecutionModeInt, &useToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &enableBrowserAccessInt, &browserModeNullStr, &preset.IsPredefined, &employeeIDStr, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan preset query: %w", err)
		}

		preset.SelectedServers = selectedServersStr
		preset.SelectedTools = selectedToolsStr
		if llmConfigNullStr.Valid {
			preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
		} else {
			preset.LLMConfig = json.RawMessage("null")
		}
		preset.SelectedFolder = selectedFolderStr
		// Convert INTEGER to boolean
		preset.UseCodeExecutionMode = useCodeExecutionModeInt != 0
		preset.UseToolSearchMode = useToolSearchModeInt != 0
		preset.EnableBrowserAccess = enableBrowserAccessInt.Valid && enableBrowserAccessInt.Int64 != 0
		if browserModeNullStr.Valid {
			preset.BrowserMode = browserModeNullStr.String
		}
		if preDiscoveredToolsStr.Valid {
			preset.PreDiscoveredTools = preDiscoveredToolsStr.String
		} else {
			preset.PreDiscoveredTools = "[]"
		}
		if selectedSkillsStr.Valid {
			preset.SelectedSkills = selectedSkillsStr.String
		} else {
			preset.SelectedSkills = "[]"
		}
		if selectedSecretsStr.Valid {
			preset.SelectedSecrets = selectedSecretsStr.String
		} else {
			preset.SelectedSecrets = "[]"
		}
		if selectedGlobalSecretNamesStr.Valid {
			preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
		}
		preset.EmployeeID = employeeIDStr
		presets = append(presets, preset)
	}

	return presets, total, nil
}

// CreateWorkflow creates a new workflow
func (s *SQLiteDB) CreateWorkflow(ctx context.Context, req *CreateWorkflowRequest) (*Workflow, error) {
	// Set default status if not provided
	workflowStatus := req.WorkflowStatus
	if workflowStatus == "" {
		workflowStatus = WorkflowStatusPreVerification
	}

	// Prepare selected options JSON
	var selectedOptionsJSON sql.NullString
	if req.SelectedOptions != nil {
		jsonBytes, err := json.Marshal(*req.SelectedOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected_options: %w", err)
		}
		selectedOptionsJSON = sql.NullString{String: string(jsonBytes), Valid: true}
	}

	query := `
		INSERT INTO workflows (preset_query_id, workflow_status, selected_options)
		VALUES (?, ?, ?)
		RETURNING id, preset_query_id, workflow_status, selected_options, created_at, updated_at
	`

	var workflow Workflow
	var selectedOptionJSONResult sql.NullString
	err := s.db.QueryRowContext(ctx, query, req.PresetQueryID, workflowStatus, selectedOptionsJSON).Scan(
		&workflow.ID, &workflow.PresetQueryID, &workflow.WorkflowStatus,
		&selectedOptionJSONResult, &workflow.CreatedAt, &workflow.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow: %w", err)
	}

	// Parse selected options JSON if present
	if selectedOptionJSONResult.Valid && selectedOptionJSONResult.String != "" {
		var selectedOptions WorkflowSelectedOptions
		if err := json.Unmarshal([]byte(selectedOptionJSONResult.String), &selectedOptions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal selected_options: %w", err)
		}
		workflow.SelectedOptions = &selectedOptions
	}

	return &workflow, nil
}

// GetWorkflowByPresetQueryID retrieves a workflow by preset query ID
func (s *SQLiteDB) GetWorkflowByPresetQueryID(ctx context.Context, presetQueryID string) (*Workflow, error) {
	query := `
		SELECT id, preset_query_id, workflow_status, selected_options, created_at, updated_at
		FROM workflows
		WHERE preset_query_id = ?
	`

	var workflow Workflow
	var selectedOptionJSON sql.NullString
	err := s.db.QueryRowContext(ctx, query, presetQueryID).Scan(
		&workflow.ID, &workflow.PresetQueryID, &workflow.WorkflowStatus,
		&selectedOptionJSON, &workflow.CreatedAt, &workflow.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("workflow not found for preset query: %s", presetQueryID)
		}
		return nil, fmt.Errorf("failed to get workflow: %w", err)
	}

	// Parse selected options JSON if present
	if selectedOptionJSON.Valid && selectedOptionJSON.String != "" {
		var selectedOptions WorkflowSelectedOptions
		if err := json.Unmarshal([]byte(selectedOptionJSON.String), &selectedOptions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal selected_options: %w", err)
		}
		workflow.SelectedOptions = &selectedOptions
	}

	return &workflow, nil
}

// UpdateWorkflow updates a workflow, creating it if it doesn't exist
func (s *SQLiteDB) UpdateWorkflow(ctx context.Context, presetQueryID string, req *UpdateWorkflowRequest) (*Workflow, error) {
	// First, check if workflow exists
	existingWorkflow, err := s.GetWorkflowByPresetQueryID(ctx, presetQueryID)
	if err != nil && !strings.Contains(err.Error(), "workflow not found for preset query") {
		return nil, fmt.Errorf("failed to check existing workflow: %w", err)
	}

	// If workflow doesn't exist, create it
	if existingWorkflow == nil {
		// Determine default workflow status
		workflowStatus := "execution"
		if req.WorkflowStatus != nil {
			workflowStatus = *req.WorkflowStatus
		}

		// Create new workflow
		createReq := &CreateWorkflowRequest{
			PresetQueryID:   presetQueryID,
			WorkflowStatus:  workflowStatus,
			SelectedOptions: req.SelectedOptions,
		}

		workflow, err := s.CreateWorkflow(ctx, createReq)
		if err != nil {
			return nil, fmt.Errorf("failed to create workflow: %w", err)
		}

		return workflow, nil
	}

	// Workflow exists, proceed with update
	// Build dynamic update query
	updateFields := []string{}
	args := []interface{}{}

	if req.WorkflowStatus != nil {
		updateFields = append(updateFields, "workflow_status = ?")
		args = append(args, *req.WorkflowStatus)
	}

	if req.SelectedOptions != nil {
		updateFields = append(updateFields, "selected_options = ?")
		selectedOptionsJSON, err := json.Marshal(*req.SelectedOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected_options: %w", err)
		}
		args = append(args, string(selectedOptionsJSON))
	}

	if len(updateFields) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	// Validate all update fields are from whitelist
	for _, field := range updateFields {
		if !validateUpdateField(field) {
			return nil, fmt.Errorf("invalid update field: %s", field)
		}
	}

	updateFields = append(updateFields, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, presetQueryID)

	//nolint:gosec // G201: updateFields are validated against whitelist and use parameterized queries (?)
	query := fmt.Sprintf(`
		UPDATE workflows
		SET %s
		WHERE preset_query_id = ?
		RETURNING id, preset_query_id, workflow_status, selected_options, created_at, updated_at
	`, strings.Join(updateFields, ", "))

	var workflow Workflow
	var selectedOptionJSON sql.NullString
	err = s.db.QueryRowContext(ctx, query, args...).Scan(
		&workflow.ID, &workflow.PresetQueryID, &workflow.WorkflowStatus,
		&selectedOptionJSON, &workflow.CreatedAt, &workflow.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	// Parse selected options JSON if present
	if selectedOptionJSON.Valid && selectedOptionJSON.String != "" {
		var selectedOptions WorkflowSelectedOptions
		if err := json.Unmarshal([]byte(selectedOptionJSON.String), &selectedOptions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal selected_options: %w", err)
		}
		workflow.SelectedOptions = &selectedOptions
	}

	return &workflow, nil
}

// DeleteWorkflow deletes a workflow
func (s *SQLiteDB) DeleteWorkflow(ctx context.Context, presetQueryID string) error {
	query := `DELETE FROM workflows WHERE preset_query_id = ?`

	result, err := s.db.ExecContext(ctx, query, presetQueryID)
	if err != nil {
		return fmt.Errorf("failed to delete workflow: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("workflow not found for preset query: %s", presetQueryID)
	}

	return nil
}

// Close closes the database connection and flushes all pending events
func (s *SQLiteDB) Close() error {
	// Stop the flusher and wait for it to finish
	if s.flushTicker != nil {
		s.flushTicker.Stop()
		close(s.stopFlusher)
		<-s.flushDone
	}

	// Flush any remaining events
	s.flushBatches()

	return s.db.Close()
}

// ============================================================================
// User-aware methods for multi-user support
// ============================================================================

// CreateChatSessionWithUser creates a new chat session with user association
func (s *SQLiteDB) CreateChatSessionWithUser(ctx context.Context, req *CreateChatSessionRequest, userID string) (*ChatSession, error) {
	query := `
		INSERT INTO chat_sessions (session_id, title, agent_mode, preset_query_id, config, status, user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		RETURNING id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
	`

	log.Printf("[CREATE_CHAT_SESSION DEBUG] Creating session with SessionID: %s, Title: '%s', AgentMode: '%s', UserID: '%s'", req.SessionID, req.Title, req.AgentMode, userID)

	// Handle empty preset_query_id by converting to NULL
	var presetQueryID interface{}
	if req.PresetQueryID == "" {
		presetQueryID = nil
	} else {
		presetQueryID = req.PresetQueryID
	}

	// Handle config - convert to JSON string or NULL
	var configValue interface{}
	if len(req.Config) == 0 {
		configValue = nil
	} else {
		configValue = string(req.Config)
	}

	// Handle empty userID by converting to NULL
	var userIDValue interface{}
	if userID == "" {
		userIDValue = nil
	} else {
		userIDValue = userID
	}

	var session ChatSession
	var agentModeStr *string
	var presetQueryIDStr *string
	var configStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, req.SessionID, req.Title, req.AgentMode, presetQueryID, configValue, "active", userIDValue).Scan(
		&session.ID, &session.SessionID, &session.Title, &agentModeStr, &presetQueryIDStr, &configStr, &session.CreatedAt, &session.CompletedAt, &session.Status,
	)
	if err != nil {
		log.Printf("[CREATE_CHAT_SESSION ERROR] Failed to create chat session: %v", err)
		return nil, fmt.Errorf("failed to create chat session: %w", err)
	}

	// Handle NULL agent_mode
	if agentModeStr != nil {
		session.AgentMode = *agentModeStr
	} else {
		session.AgentMode = ""
	}

	// Handle NULL preset_query_id
	if presetQueryIDStr != nil {
		session.PresetQueryID = presetQueryIDStr
	}

	// Handle NULL config
	if configStr.Valid {
		session.Config = json.RawMessage(configStr.String)
	} else {
		session.Config = nil
	}

	log.Printf("[CREATE_CHAT_SESSION DEBUG] Successfully created session ID: %s, SessionID: %s, UserID: %s", session.ID, session.SessionID, userID)

	return &session, nil
}

// GetChatSessionWithUser retrieves a chat session by session ID with user verification
func (s *SQLiteDB) GetChatSessionWithUser(ctx context.Context, sessionID string, userID string) (*ChatSession, error) {
	var query string
	var args []interface{}

	if userID == "" {
		// No user filtering
		query = `
			SELECT id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
			FROM chat_sessions
			WHERE session_id = ?
		`
		args = []interface{}{sessionID}
	} else {
		// Filter by user_id (also allow sessions with NULL user_id for backwards compatibility)
		query = `
			SELECT id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
			FROM chat_sessions
			WHERE session_id = ? AND (user_id = ? OR user_id IS NULL)
		`
		args = []interface{}{sessionID, userID}
	}

	var session ChatSession
	var agentModeStr *string
	var presetQueryIDStr *string
	var configStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&session.ID, &session.SessionID, &session.Title, &agentModeStr, &presetQueryIDStr, &configStr, &session.CreatedAt, &session.CompletedAt, &session.Status,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("chat session not found")
		}
		return nil, fmt.Errorf("failed to get chat session: %w", err)
	}

	// Handle NULL agent_mode
	if agentModeStr != nil {
		session.AgentMode = *agentModeStr
	} else {
		session.AgentMode = ""
	}

	// Handle NULL preset_query_id
	if presetQueryIDStr != nil {
		session.PresetQueryID = presetQueryIDStr
	}

	// Handle NULL config
	if configStr.Valid {
		session.Config = json.RawMessage(configStr.String)
	} else {
		session.Config = nil
	}

	return &session, nil
}

// ListChatSessionsWithUser lists chat sessions with pagination and user filtering
func (s *SQLiteDB) ListChatSessionsWithUser(ctx context.Context, limit, offset int, presetQueryID *string, agentMode *string, userID string) ([]ChatHistorySummary, int, error) {
	// Build WHERE clause for filtering
	var whereConditions []string
	var args []interface{}

	if presetQueryID != nil && *presetQueryID != "" {
		whereConditions = append(whereConditions, "cs.preset_query_id = ?")
		args = append(args, *presetQueryID)
	}

	if agentMode != nil && *agentMode != "" {
		whereConditions = append(whereConditions, "cs.agent_mode = ?")
		args = append(args, *agentMode)
	}

	// Filter by user_id (also include sessions with NULL user_id for backwards compatibility)
	if userID != "" {
		whereConditions = append(whereConditions, "(cs.user_id = ? OR cs.user_id IS NULL)")
		args = append(args, userID)
	}

	var whereClause string
	if len(whereConditions) > 0 {
		whereClause = " WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Validate WHERE clause for safety
	if err := validateWhereClause(whereClause); err != nil {
		return nil, 0, fmt.Errorf("invalid WHERE clause: %w", err)
	}

	// Get total count
	//nolint:gosec // G202: whereClause is validated and uses parameterized queries (?)
	countQuery := `SELECT COUNT(*) FROM chat_sessions cs` + whereClause
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get sessions with summary data
	//nolint:gosec // G202: whereClause is validated and uses parameterized queries (?)
	query := `
		SELECT
			cs.id,
			cs.session_id,
			cs.title,
			cs.agent_mode,
			cs.status,
			cs.created_at,
			cs.completed_at,
			cs.preset_query_id,
			cs.config,
			0 as total_events,
			0 as total_turns,
			NULL as last_activity
		FROM chat_sessions cs` + whereClause + `
		ORDER BY cs.created_at DESC
		LIMIT ? OFFSET ?
	`

	// Add limit and offset to args
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list chat sessions: %w", err)
	}
	defer rows.Close()

	var sessions []ChatHistorySummary
	for rows.Next() {
		var session ChatHistorySummary
		var lastActivityStr *string
		var agentModeStr *string
		var presetQueryIDStr *string
		var configStr sql.NullString
		err := rows.Scan(
			&session.ChatSessionID, &session.SessionID, &session.Title, &agentModeStr, &session.Status,
			&session.CreatedAt, &session.CompletedAt, &presetQueryIDStr, &configStr, &session.TotalEvents, &session.TotalTurns, &lastActivityStr,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan session: %w", err)
		}

		// Handle NULL agent_mode
		if agentModeStr != nil {
			session.AgentMode = *agentModeStr
		} else {
			session.AgentMode = ""
		}

		// Handle NULL preset_query_id
		if presetQueryIDStr != nil {
			session.PresetQueryID = *presetQueryIDStr
		} else {
			session.PresetQueryID = ""
		}

		// Handle NULL config
		if configStr.Valid {
			session.Config = json.RawMessage(configStr.String)
		} else {
			session.Config = nil
		}

		// Parse lastActivity string to time.Time
		if lastActivityStr != nil {
			if lastActivity, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", *lastActivityStr); err == nil {
				session.LastActivity = &lastActivity
			} else {
				session.LastActivity = nil
			}
		} else {
			session.LastActivity = nil
		}

		sessions = append(sessions, session)
	}

	return sessions, total, nil
}

// CreatePresetQueryWithUser creates a new preset query with user association
func (s *SQLiteDB) CreatePresetQueryWithUser(ctx context.Context, req *CreatePresetQueryRequest, userID string) (*PresetQuery, error) {
	// Validate the request
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Convert selected servers to JSON
	selectedServersJSON := "[]"
	if len(req.SelectedServers) > 0 {
		serversJSON, err := json.Marshal(req.SelectedServers)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected servers: %w", err)
		}
		selectedServersJSON = string(serversJSON)
	}

	// Convert selected tools to JSON
	selectedToolsJSON := "[]"
	if len(req.SelectedTools) > 0 {
		toolsJSON, err := json.Marshal(req.SelectedTools)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected tools: %w", err)
		}
		selectedToolsJSON = string(toolsJSON)
	}

	// Prepare LLM config for insert (NULL when absent)
	var llmConfigParam interface{}
	if req.LLMConfig != nil {
		llmConfigBytes, err := json.Marshal(req.LLMConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal LLM config: %w", err)
		}
		llmConfigParam = string(llmConfigBytes)
	} else {
		llmConfigParam = nil
	}

	// Set default agent mode if not provided
	agentMode := req.AgentMode
	if agentMode == "" {
		agentMode = AgentModeSimple
	}

	// Convert boolean flags to INTEGER for SQLite
	useCodeExecutionModeInt := 0
	if req.UseCodeExecutionMode {
		useCodeExecutionModeInt = 1
	}
	useToolSearchModeInt := 0
	if req.UseToolSearchMode {
		useToolSearchModeInt = 1
	}
	enableBrowserAccessInt := 0
	if req.EnableBrowserAccess {
		enableBrowserAccessInt = 1
	}

	// Convert pre_discovered_tools to JSON
	preDiscoveredToolsJSON := "[]"
	if len(req.PreDiscoveredTools) > 0 {
		toolsJSON, err := json.Marshal(req.PreDiscoveredTools)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal pre-discovered tools: %w", err)
		}
		preDiscoveredToolsJSON = string(toolsJSON)
	}

	// Convert selected_skills to JSON
	selectedSkillsJSON := "[]"
	if len(req.SelectedSkills) > 0 {
		skillsJSON, err := json.Marshal(req.SelectedSkills)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected skills: %w", err)
		}
		selectedSkillsJSON = string(skillsJSON)
	}

	// Convert selected_secrets to JSON
	selectedSecretsJSON := "[]"
	if len(req.SelectedSecrets) > 0 {
		secretsJSON, err := json.Marshal(req.SelectedSecrets)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected secrets: %w", err)
		}
		selectedSecretsJSON = string(secretsJSON)
	}

	// Convert selected_global_secret_names to JSON (nil = all selected, stored as NULL)
	var selectedGlobalSecretNamesParam interface{}
	if req.SelectedGlobalSecretNames != nil {
		gsJSON, err := json.Marshal(*req.SelectedGlobalSecretNames)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected global secret names: %w", err)
		}
		selectedGlobalSecretNamesParam = string(gsJSON)
	}

	// Handle userID
	var userIDValue interface{}
	if userID == "" {
		userIDValue = nil
	} else {
		userIDValue = userID
	}

	query := `
		INSERT INTO preset_queries (label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, browser_mode, is_predefined, created_by, user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, browser_mode, is_predefined, created_at, updated_at, created_by
	`

	var preset PresetQuery
	var selectedServersStr string
	var selectedToolsStr string
	var selectedFolderStr sql.NullString
	var llmConfigNullStr sql.NullString
	var returnedUseCodeExecutionModeInt int
	var returnedUseToolSearchModeInt int
	var preDiscoveredToolsStr string
	var selectedSkillsStr string
	var selectedSecretsStr string
	var selectedGlobalSecretNamesStr sql.NullString
	var returnedEnableBrowserAccessInt int
	// Resolve browser_mode for CreatePresetQueryWithUser
	browserModeWithUser := req.BrowserMode
	if browserModeWithUser == "" && req.EnableBrowserAccess {
		browserModeWithUser = "headless"
	}
	err := s.db.QueryRowContext(ctx, query, req.Label, req.Query, selectedServersJSON, selectedToolsJSON, req.SelectedFolder, agentMode, llmConfigParam, useCodeExecutionModeInt, useToolSearchModeInt, preDiscoveredToolsJSON, selectedSkillsJSON, selectedSecretsJSON, selectedGlobalSecretNamesParam, enableBrowserAccessInt, browserModeWithUser, req.IsPredefined, "user", userIDValue).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &returnedUseCodeExecutionModeInt, &returnedUseToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &returnedEnableBrowserAccessInt, &preset.BrowserMode, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create preset query: %w", err)
	}

	// Parse fields
	preset.SelectedServers = selectedServersStr
	preset.SelectedTools = selectedToolsStr
	preset.SelectedFolder = selectedFolderStr
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = json.RawMessage("null")
	}
	preset.UseCodeExecutionMode = returnedUseCodeExecutionModeInt != 0
	preset.UseToolSearchMode = returnedUseToolSearchModeInt != 0
	preset.PreDiscoveredTools = preDiscoveredToolsStr
	preset.SelectedSkills = selectedSkillsStr
	preset.SelectedSecrets = selectedSecretsStr
	if selectedGlobalSecretNamesStr.Valid {
		preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
	}
	preset.EnableBrowserAccess = returnedEnableBrowserAccessInt != 0

	return &preset, nil
}

// ListPresetQueriesWithUser lists preset queries with pagination and user filtering
// --- Bot Connector Config CRUD ---

func (s *SQLiteDB) UpsertBotConnectorConfig(ctx context.Context, req *CreateBotConnectorConfigRequest) (*BotConnectorConfig, error) {
	query := `
		INSERT INTO bot_connector_config (id, enabled, bot_mode, config_json, default_preset_id, auto_confirm, allowed_channels, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			enabled = excluded.enabled,
			bot_mode = excluded.bot_mode,
			config_json = excluded.config_json,
			default_preset_id = excluded.default_preset_id,
			auto_confirm = excluded.auto_confirm,
			allowed_channels = excluded.allowed_channels,
			updated_at = CURRENT_TIMESTAMP
	`

	configJSON := req.ConfigJSON
	if configJSON == "" {
		configJSON = "{}"
	}
	allowedChannels := req.AllowedChannels
	if allowedChannels == "" {
		allowedChannels = "[]"
	}

	_, err := s.db.ExecContext(ctx, query,
		req.ID, req.Enabled, req.BotMode, configJSON,
		req.DefaultPresetID, req.AutoConfirm, allowedChannels,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert bot connector config: %w", err)
	}

	return s.GetBotConnectorConfig(ctx, req.ID)
}

func (s *SQLiteDB) GetBotConnectorConfig(ctx context.Context, id string) (*BotConnectorConfig, error) {
	query := `SELECT id, enabled, bot_mode, config_json, default_preset_id, auto_confirm, allowed_channels, created_at, updated_at FROM bot_connector_config WHERE id = ?`

	var cfg BotConnectorConfig
	var enabledInt, botModeInt, autoConfirmInt int
	var defaultPresetID, configJSON, allowedChannels sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&cfg.ID, &enabledInt, &botModeInt, &configJSON,
		&defaultPresetID, &autoConfirmInt, &allowedChannels,
		&cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("bot connector config not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get bot connector config: %w", err)
	}

	cfg.Enabled = enabledInt != 0
	cfg.BotMode = botModeInt != 0
	cfg.AutoConfirm = autoConfirmInt != 0
	if configJSON.Valid {
		cfg.ConfigJSON = configJSON.String
	} else {
		cfg.ConfigJSON = "{}"
	}
	if defaultPresetID.Valid {
		cfg.DefaultPresetID = defaultPresetID.String
	}
	if allowedChannels.Valid {
		cfg.AllowedChannels = allowedChannels.String
	} else {
		cfg.AllowedChannels = "[]"
	}

	return &cfg, nil
}

func (s *SQLiteDB) ListBotConnectorConfigs(ctx context.Context) ([]BotConnectorConfig, error) {
	query := `SELECT id, enabled, bot_mode, config_json, default_preset_id, auto_confirm, allowed_channels, created_at, updated_at FROM bot_connector_config ORDER BY id`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list bot connector configs: %w", err)
	}
	defer rows.Close()

	configs := make([]BotConnectorConfig, 0)
	for rows.Next() {
		var cfg BotConnectorConfig
		var enabledInt, botModeInt, autoConfirmInt int
		var defaultPresetID, configJSON, allowedChannels sql.NullString
		err := rows.Scan(
			&cfg.ID, &enabledInt, &botModeInt, &configJSON,
			&defaultPresetID, &autoConfirmInt, &allowedChannels,
			&cfg.CreatedAt, &cfg.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan bot connector config: %w", err)
		}
		cfg.Enabled = enabledInt != 0
		cfg.BotMode = botModeInt != 0
		cfg.AutoConfirm = autoConfirmInt != 0
		if configJSON.Valid {
			cfg.ConfigJSON = configJSON.String
		} else {
			cfg.ConfigJSON = "{}"
		}
		if defaultPresetID.Valid {
			cfg.DefaultPresetID = defaultPresetID.String
		}
		if allowedChannels.Valid {
			cfg.AllowedChannels = allowedChannels.String
		} else {
			cfg.AllowedChannels = "[]"
		}
		configs = append(configs, cfg)
	}

	return configs, nil
}

// --- Bot Session CRUD ---

func (s *SQLiteDB) CreateBotSession(ctx context.Context, req *CreateBotSessionRequest) (*BotSession, error) {
	id := uuid.New().String()
	query := `
		INSERT INTO bot_sessions (id, platform, channel_id, thread_ts, user_id, user_name, query, status, thread_context)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'analyzing', ?)
	`

	threadContext := req.ThreadContext
	if threadContext == "" {
		threadContext = "[]"
	}

	_, err := s.db.ExecContext(ctx, query,
		id, req.Platform, req.ChannelID, req.ThreadTS,
		req.UserID, req.UserName, req.Query, threadContext,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot session: %w", err)
	}

	return s.GetBotSession(ctx, id)
}

func (s *SQLiteDB) GetBotSession(ctx context.Context, id string) (*BotSession, error) {
	query := `
		SELECT id, platform, channel_id, thread_ts, session_id, user_id, user_name, query, status,
		       preset_id, config_json, thread_context, created_at, updated_at, completed_at
		FROM bot_sessions WHERE id = ?
	`

	var bs BotSession
	var sessionID, userName, presetID, configJSON, threadContext sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&bs.ID, &bs.Platform, &bs.ChannelID, &bs.ThreadTS,
		&sessionID, &bs.UserID, &userName, &bs.Query, &bs.Status,
		&presetID, &configJSON, &threadContext,
		&bs.CreatedAt, &bs.UpdatedAt, &bs.CompletedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("bot session not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get bot session: %w", err)
	}

	if sessionID.Valid {
		bs.SessionID = sessionID.String
	}
	if userName.Valid {
		bs.UserName = userName.String
	}
	if presetID.Valid {
		bs.PresetID = presetID.String
	}
	if configJSON.Valid {
		bs.ConfigJSON = configJSON.String
	}
	if threadContext.Valid {
		bs.ThreadContext = threadContext.String
	}

	return &bs, nil
}

func (s *SQLiteDB) GetBotSessionByThread(ctx context.Context, platform, channelID, threadTS string) (*BotSession, error) {
	query := `
		SELECT id FROM bot_sessions
		WHERE platform = ? AND channel_id = ? AND thread_ts = ?
		ORDER BY created_at DESC LIMIT 1
	`

	var id string
	err := s.db.QueryRowContext(ctx, query, platform, channelID, threadTS).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // not found is not an error for thread lookup
		}
		return nil, fmt.Errorf("failed to get bot session by thread: %w", err)
	}

	return s.GetBotSession(ctx, id)
}

func (s *SQLiteDB) GetBotSessionBySessionID(ctx context.Context, sessionID string) (*BotSession, error) {
	query := `SELECT id FROM bot_sessions WHERE session_id = ? LIMIT 1`

	var id string
	err := s.db.QueryRowContext(ctx, query, sessionID).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get bot session by session ID: %w", err)
	}

	return s.GetBotSession(ctx, id)
}

func (s *SQLiteDB) UpdateBotSession(ctx context.Context, id string, req *UpdateBotSessionRequest) (*BotSession, error) {
	var updateFields []string
	var args []interface{}

	if req.SessionID != "" {
		updateFields = append(updateFields, "session_id = ?")
		args = append(args, req.SessionID)
	}
	if req.Status != "" {
		updateFields = append(updateFields, "status = ?")
		args = append(args, req.Status)
	}
	if req.PresetID != "" {
		updateFields = append(updateFields, "preset_id = ?")
		args = append(args, req.PresetID)
	}
	if req.ConfigJSON != "" {
		updateFields = append(updateFields, "config_json = ?")
		args = append(args, req.ConfigJSON)
	}

	if len(updateFields) == 0 {
		return s.GetBotSession(ctx, id)
	}

	updateFields = append(updateFields, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE bot_sessions SET %s WHERE id = ?", strings.Join(updateFields, ", "))
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update bot session: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return nil, fmt.Errorf("bot session not found: %s", id)
	}

	return s.GetBotSession(ctx, id)
}

func (s *SQLiteDB) CompleteBotSession(ctx context.Context, id string, status string) error {
	query := `UPDATE bot_sessions SET status = ?, completed_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("failed to complete bot session: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("bot session not found: %s", id)
	}

	return nil
}

func (s *SQLiteDB) ListBotSessions(ctx context.Context, limit, offset int, status string) ([]BotSession, int, error) {
	var whereClause string
	var args []interface{}

	if status != "" {
		whereClause = " WHERE status = ?"
		args = append(args, status)
	}

	countQuery := "SELECT COUNT(*) FROM bot_sessions" + whereClause
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count bot sessions: %w", err)
	}

	query := `
		SELECT id, platform, channel_id, thread_ts, session_id, user_id, user_name, query, status,
		       preset_id, config_json, thread_context, created_at, updated_at, completed_at
		FROM bot_sessions` + whereClause + `
		ORDER BY created_at DESC LIMIT ? OFFSET ?
	`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list bot sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]BotSession, 0)
	for rows.Next() {
		var bs BotSession
		var sessionID, userName, presetID, configJSON, threadContext sql.NullString
		err := rows.Scan(
			&bs.ID, &bs.Platform, &bs.ChannelID, &bs.ThreadTS,
			&sessionID, &bs.UserID, &userName, &bs.Query, &bs.Status,
			&presetID, &configJSON, &threadContext,
			&bs.CreatedAt, &bs.UpdatedAt, &bs.CompletedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan bot session: %w", err)
		}
		if sessionID.Valid {
			bs.SessionID = sessionID.String
		}
		if userName.Valid {
			bs.UserName = userName.String
		}
		if presetID.Valid {
			bs.PresetID = presetID.String
		}
		if configJSON.Valid {
			bs.ConfigJSON = configJSON.String
		}
		if threadContext.Valid {
			bs.ThreadContext = threadContext.String
		}
		sessions = append(sessions, bs)
	}

	return sessions, total, nil
}

// --- Bot Message CRUD ---

func (s *SQLiteDB) CreateBotMessage(ctx context.Context, req *CreateBotMessageRequest) (*BotMessage, error) {
	id := uuid.New().String()
	query := `
		INSERT INTO bot_messages (id, bot_session_id, direction, message_type, content, platform_message_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.ExecContext(ctx, query,
		id, req.BotSessionID, req.Direction, req.MessageType,
		req.Content, req.PlatformMessageID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot message: %w", err)
	}

	var msg BotMessage
	getQuery := `SELECT id, bot_session_id, direction, message_type, content, platform_message_id, created_at FROM bot_messages WHERE id = ?`
	var content, platformMsgID sql.NullString
	err = s.db.QueryRowContext(ctx, getQuery, id).Scan(
		&msg.ID, &msg.BotSessionID, &msg.Direction, &msg.MessageType,
		&content, &platformMsgID, &msg.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get created bot message: %w", err)
	}
	if content.Valid {
		msg.Content = content.String
	}
	if platformMsgID.Valid {
		msg.PlatformMessageID = platformMsgID.String
	}

	return &msg, nil
}

func (s *SQLiteDB) ListBotMessages(ctx context.Context, botSessionID string, limit, offset int) ([]BotMessage, int, error) {
	countQuery := `SELECT COUNT(*) FROM bot_messages WHERE bot_session_id = ?`
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, botSessionID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count bot messages: %w", err)
	}

	query := `
		SELECT id, bot_session_id, direction, message_type, content, platform_message_id, created_at
		FROM bot_messages WHERE bot_session_id = ?
		ORDER BY created_at ASC LIMIT ? OFFSET ?
	`

	rows, err := s.db.QueryContext(ctx, query, botSessionID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list bot messages: %w", err)
	}
	defer rows.Close()

	messages := make([]BotMessage, 0)
	for rows.Next() {
		var msg BotMessage
		var content, platformMsgID sql.NullString
		err := rows.Scan(
			&msg.ID, &msg.BotSessionID, &msg.Direction, &msg.MessageType,
			&content, &platformMsgID, &msg.CreatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan bot message: %w", err)
		}
		if content.Valid {
			msg.Content = content.String
		}
		if platformMsgID.Valid {
			msg.PlatformMessageID = platformMsgID.String
		}
		messages = append(messages, msg)
	}

	return messages, total, nil
}

func (s *SQLiteDB) ListPresetQueriesWithUser(ctx context.Context, limit, offset int, userID string) ([]PresetQuery, int, error) {
	var whereClause string
	var args []interface{}

	// Filter by user_id (also include presets with NULL user_id for backwards compatibility and system presets)
	if userID != "" {
		whereClause = " WHERE (user_id = ? OR user_id IS NULL OR is_predefined = 1)"
		args = append(args, userID)
	}

	// Get total count
	countQuery := `SELECT COUNT(*) FROM preset_queries` + whereClause
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get presets
	query := `
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, browser_mode, is_predefined, employee_id, created_at, updated_at, created_by
		FROM preset_queries` + whereClause + `
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`

	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list preset queries: %w", err)
	}
	defer rows.Close()

	presets := make([]PresetQuery, 0)
	for rows.Next() {
		var preset PresetQuery
		var selectedServersStr string
		var selectedToolsStr string
		var selectedFolderStr sql.NullString
		var llmConfigNullStr sql.NullString
		var useCodeExecutionModeInt int
		var useToolSearchModeInt int
		var preDiscoveredToolsStr sql.NullString
		var selectedSkillsStr sql.NullString
		var selectedSecretsStr sql.NullString
		var selectedGlobalSecretNamesStr sql.NullString
		var enableBrowserAccessInt sql.NullInt64
		var browserModeNullStr sql.NullString
		var employeeIDStr sql.NullString
		err := rows.Scan(
			&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &useCodeExecutionModeInt, &useToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &enableBrowserAccessInt, &browserModeNullStr, &preset.IsPredefined, &employeeIDStr, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan preset query: %w", err)
		}

		preset.SelectedServers = selectedServersStr
		preset.SelectedTools = selectedToolsStr
		if llmConfigNullStr.Valid {
			preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
		} else {
			preset.LLMConfig = json.RawMessage("null")
		}
		preset.SelectedFolder = selectedFolderStr
		preset.UseCodeExecutionMode = useCodeExecutionModeInt != 0
		preset.UseToolSearchMode = useToolSearchModeInt != 0
		preset.EnableBrowserAccess = enableBrowserAccessInt.Valid && enableBrowserAccessInt.Int64 != 0
		if browserModeNullStr.Valid {
			preset.BrowserMode = browserModeNullStr.String
		}
		if preDiscoveredToolsStr.Valid {
			preset.PreDiscoveredTools = preDiscoveredToolsStr.String
		} else {
			preset.PreDiscoveredTools = "[]"
		}
		if selectedSkillsStr.Valid {
			preset.SelectedSkills = selectedSkillsStr.String
		} else {
			preset.SelectedSkills = "[]"
		}
		if selectedSecretsStr.Valid {
			preset.SelectedSecrets = selectedSecretsStr.String
		} else {
			preset.SelectedSecrets = "[]"
		}
		if selectedGlobalSecretNamesStr.Valid {
			preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
		}
		preset.EmployeeID = employeeIDStr
		presets = append(presets, preset)
	}

	return presets, total, nil
}

// --- App Users CRUD ---

func (s *SQLiteDB) UpsertAppUser(ctx context.Context, userID, email, username, provider string) error {
	query := `
		INSERT INTO app_users (user_id, email, username, provider, created_at, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id) DO UPDATE SET
			email = excluded.email,
			username = excluded.username,
			provider = excluded.provider,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err := s.db.ExecContext(ctx, query, userID, email, username, provider)
	if err != nil {
		return fmt.Errorf("failed to upsert app user: %w", err)
	}
	return nil
}

func (s *SQLiteDB) GetAppUserByEmail(ctx context.Context, email string) (*AppUser, error) {
	query := `SELECT user_id, email, username, provider, created_at, updated_at FROM app_users WHERE email = ?`
	row := s.db.QueryRowContext(ctx, query, email)
	var user AppUser
	if err := row.Scan(&user.UserID, &user.Email, &user.Username, &user.Provider, &user.CreatedAt, &user.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get app user by email: %w", err)
	}
	return &user, nil
}

// --- User Secrets CRUD ---

func (s *SQLiteDB) UpsertUserSecret(ctx context.Context, userID, name, encryptedValue string) error {
	id := uuid.New().String()
	query := `
		INSERT INTO user_secrets (id, user_id, name, encrypted_value, created_at, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT(user_id, name) DO UPDATE SET
			encrypted_value = excluded.encrypted_value,
			updated_at = CURRENT_TIMESTAMP
	`
	_, err := s.db.ExecContext(ctx, query, id, userID, name, encryptedValue)
	if err != nil {
		return fmt.Errorf("failed to upsert user secret: %w", err)
	}
	return nil
}

func (s *SQLiteDB) DeleteUserSecret(ctx context.Context, userID, name string) error {
	query := `DELETE FROM user_secrets WHERE user_id = ? AND name = ?`
	_, err := s.db.ExecContext(ctx, query, userID, name)
	if err != nil {
		return fmt.Errorf("failed to delete user secret: %w", err)
	}
	return nil
}

func (s *SQLiteDB) ListUserSecrets(ctx context.Context, userID string) ([]UserSecret, error) {
	query := `SELECT id, user_id, name, encrypted_value, created_at, updated_at FROM user_secrets WHERE user_id = ? ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list user secrets: %w", err)
	}
	defer rows.Close()

	var secrets []UserSecret
	for rows.Next() {
		var secret UserSecret
		if err := rows.Scan(&secret.ID, &secret.UserID, &secret.Name, &secret.EncryptedValue, &secret.CreatedAt, &secret.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan user secret: %w", err)
		}
		secrets = append(secrets, secret)
	}
	return secrets, nil
}

// --- Scheduled Jobs CRUD ---

func scanScheduledJob(row interface {
	Scan(dest ...interface{}) error
}) (*ScheduledJob, error) {
	var job ScheduledJob
	var enabledInt int
	var triggerPayloadStr sql.NullString
	var groupIDsStr sql.NullString
	var lastRunAtStr sql.NullTime
	var nextRunAtStr sql.NullTime
	var lastSessionIDStr sql.NullString
	var lastStatusStr sql.NullString
	var lastErrorStr sql.NullString
	var lastDurationMs sql.NullInt64

	err := row.Scan(
		&job.ID, &job.Name, &job.Description, &job.EntityType, &job.PresetQueryID,
		&triggerPayloadStr, &groupIDsStr, &job.CronExpression, &job.Timezone,
		&enabledInt, &lastRunAtStr, &nextRunAtStr,
		&lastSessionIDStr, &lastStatusStr, &lastErrorStr, &lastDurationMs,
		&job.RunCount, &job.ConsecutiveFailures, &job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	job.Enabled = enabledInt != 0
	if triggerPayloadStr.Valid {
		job.TriggerPayload = json.RawMessage(triggerPayloadStr.String)
	}
	if groupIDsStr.Valid && groupIDsStr.String != "" {
		_ = json.Unmarshal([]byte(groupIDsStr.String), &job.GroupIDs)
	}
	if lastRunAtStr.Valid {
		t := lastRunAtStr.Time
		job.LastRunAt = &t
	}
	if nextRunAtStr.Valid {
		t := nextRunAtStr.Time
		job.NextRunAt = &t
	}
	if lastSessionIDStr.Valid {
		job.LastSessionID = lastSessionIDStr.String
	}
	if lastStatusStr.Valid {
		job.LastStatus = lastStatusStr.String
	}
	if lastErrorStr.Valid {
		job.LastError = lastErrorStr.String
	}
	if lastDurationMs.Valid {
		v := lastDurationMs.Int64
		job.LastDurationMs = &v
	}
	return &job, nil
}

func (s *SQLiteDB) CreateScheduledJob(ctx context.Context, req *CreateScheduledJobRequest) (*ScheduledJob, error) {
	id := uuid.New().String()
	enabled := 1
	if req.Enabled != nil && !*req.Enabled {
		enabled = 0
	}
	tz := req.Timezone
	if tz == "" {
		tz = "UTC"
	}
	var triggerPayload interface{}
	if len(req.TriggerPayload) > 0 {
		triggerPayload = string(req.TriggerPayload)
	}
	var groupIDs interface{}
	if len(req.GroupIDs) > 0 {
		b, _ := json.Marshal(req.GroupIDs)
		groupIDs = string(b)
	}

	query := `
		INSERT INTO scheduled_jobs (id, name, description, entity_type, preset_query_id, trigger_payload,
			group_ids, cron_expression, timezone, enabled, run_count, consecutive_failures, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
	`
	_, err := s.db.ExecContext(ctx, query, id, req.Name, req.Description, req.EntityType, req.PresetQueryID,
		triggerPayload, groupIDs, req.CronExpression, tz, enabled)
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduled job: %w", err)
	}
	return s.GetScheduledJob(ctx, id)
}

func (s *SQLiteDB) GetScheduledJob(ctx context.Context, id string) (*ScheduledJob, error) {
	query := `SELECT id, name, description, entity_type, preset_query_id, trigger_payload,
		group_ids, cron_expression, timezone, enabled, last_run_at, next_run_at,
		last_session_id, last_status, last_error, last_duration_ms, run_count, consecutive_failures, created_at, updated_at
		FROM scheduled_jobs WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)
	job, err := scanScheduledJob(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get scheduled job: %w", err)
	}
	return job, nil
}

func (s *SQLiteDB) UpdateScheduledJob(ctx context.Context, id string, req *UpdateScheduledJobRequest) (*ScheduledJob, error) {
	setClauses := []string{"updated_at = CURRENT_TIMESTAMP"}
	args := []interface{}{}

	if req.Name != "" {
		setClauses = append(setClauses, "name = ?")
		args = append(args, req.Name)
	}
	if req.Description != "" {
		setClauses = append(setClauses, "description = ?")
		args = append(args, req.Description)
	}
	if len(req.TriggerPayload) > 0 {
		setClauses = append(setClauses, "trigger_payload = ?")
		args = append(args, string(req.TriggerPayload))
	}
	if req.SetGroupIDs {
		if len(req.GroupIDs) > 0 {
			b, _ := json.Marshal(req.GroupIDs)
			setClauses = append(setClauses, "group_ids = ?")
			args = append(args, string(b))
		} else {
			setClauses = append(setClauses, "group_ids = NULL")
		}
	}
	if req.CronExpression != "" {
		setClauses = append(setClauses, "cron_expression = ?")
		args = append(args, req.CronExpression)
	}
	if req.Timezone != "" {
		setClauses = append(setClauses, "timezone = ?")
		args = append(args, req.Timezone)
	}
	if req.Enabled != nil {
		enabledInt := 0
		if *req.Enabled {
			enabledInt = 1
		}
		setClauses = append(setClauses, "enabled = ?")
		args = append(args, enabledInt)
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE scheduled_jobs SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update scheduled job: %w", err)
	}
	return s.GetScheduledJob(ctx, id)
}

func (s *SQLiteDB) DeleteScheduledJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM scheduled_jobs WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete scheduled job: %w", err)
	}
	return nil
}

func (s *SQLiteDB) ListScheduledJobs(ctx context.Context, limit, offset int, entityType *string, enabled *bool) ([]ScheduledJob, int, error) {
	whereClauses := []string{}
	args := []interface{}{}

	if entityType != nil {
		whereClauses = append(whereClauses, "entity_type = ?")
		args = append(args, *entityType)
	}
	if enabled != nil {
		enabledInt := 0
		if *enabled {
			enabledInt = 1
		}
		whereClauses = append(whereClauses, "enabled = ?")
		args = append(args, enabledInt)
	}

	where := ""
	if len(whereClauses) > 0 {
		where = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM scheduled_jobs %s", where)
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count scheduled jobs: %w", err)
	}

	listArgs := append(args, limit, offset)
	query := fmt.Sprintf(`SELECT id, name, description, entity_type, preset_query_id, trigger_payload,
		group_ids, cron_expression, timezone, enabled, last_run_at, next_run_at,
		last_session_id, last_status, last_error, last_duration_ms, run_count, consecutive_failures, created_at, updated_at
		FROM scheduled_jobs %s ORDER BY created_at DESC LIMIT ? OFFSET ?`, where)

	rows, err := s.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list scheduled jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]ScheduledJob, 0)
	for rows.Next() {
		job, err := scanScheduledJob(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan scheduled job: %w", err)
		}
		jobs = append(jobs, *job)
	}
	return jobs, total, nil
}

func (s *SQLiteDB) UpdateScheduledJobRunStatus(ctx context.Context, id string, lastRunAt time.Time, nextRunAt *time.Time, sessionID, status, errMsg string, durationMs *int64) error {
	query := `UPDATE scheduled_jobs SET
		last_run_at = ?,
		next_run_at = ?,
		last_session_id = ?,
		last_status = ?,
		last_error = ?,
		last_duration_ms = ?,
		run_count = CASE WHEN ? = 'running' THEN run_count ELSE run_count + 1 END,
		consecutive_failures = CASE WHEN ? = 'error' THEN consecutive_failures + 1 WHEN ? = 'running' THEN consecutive_failures ELSE 0 END,
		updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, lastRunAt, nextRunAt, sessionID, status, errMsg, durationMs, status, status, status, id)
	if err != nil {
		return fmt.Errorf("failed to update scheduled job run status: %w", err)
	}
	return nil
}

func (s *SQLiteDB) CreateScheduledJobRun(ctx context.Context, run *ScheduledJobRun) error {
	groupIDsJSON := "[]"
	if len(run.GroupIDs) > 0 {
		b, _ := json.Marshal(run.GroupIDs)
		groupIDsJSON = string(b)
	}
	query := `INSERT INTO scheduled_job_runs (id, job_id, run_folder, session_id, status, error, duration_ms, group_ids, started_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, run.ID, run.JobID, run.RunFolder, run.SessionID, run.Status, run.Error, run.DurationMs, groupIDsJSON, run.StartedAt, run.CompletedAt)
	if err != nil {
		return fmt.Errorf("failed to create scheduled job run: %w", err)
	}
	return nil
}

func (s *SQLiteDB) UpdateScheduledJobRun(ctx context.Context, id string, status string, errMsg string, durationMs *int64, runFolder string, sessionID string) error {
	query := `UPDATE scheduled_job_runs SET status = ?, error = ?, duration_ms = ?, run_folder = ?, session_id = ?, completed_at = CURRENT_TIMESTAMP WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, status, errMsg, durationMs, runFolder, sessionID, id)
	if err != nil {
		return fmt.Errorf("failed to update scheduled job run: %w", err)
	}
	return nil
}

func (s *SQLiteDB) ListScheduledJobRuns(ctx context.Context, jobID string, limit, offset int) ([]ScheduledJobRun, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduled_job_runs WHERE job_id = ?", jobID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count scheduled job runs: %w", err)
	}

	query := `SELECT id, job_id, run_folder, session_id, status, error, duration_ms, group_ids, started_at, completed_at
		FROM scheduled_job_runs WHERE job_id = ? ORDER BY started_at DESC LIMIT ? OFFSET ?`
	rows, err := s.db.QueryContext(ctx, query, jobID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list scheduled job runs: %w", err)
	}
	defer rows.Close()

	runs := make([]ScheduledJobRun, 0)
	for rows.Next() {
		var run ScheduledJobRun
		var runFolder, sessionID, errMsg, groupIDsJSON sql.NullString
		var durationMs sql.NullInt64
		var completedAt sql.NullTime
		if err := rows.Scan(&run.ID, &run.JobID, &runFolder, &sessionID, &run.Status, &errMsg, &durationMs, &groupIDsJSON, &run.StartedAt, &completedAt); err != nil {
			return nil, 0, fmt.Errorf("failed to scan scheduled job run: %w", err)
		}
		run.RunFolder = runFolder.String
		run.SessionID = sessionID.String
		run.Error = errMsg.String
		if durationMs.Valid {
			run.DurationMs = &durationMs.Int64
		}
		if completedAt.Valid {
			run.CompletedAt = &completedAt.Time
		}
		if groupIDsJSON.Valid && groupIDsJSON.String != "" && groupIDsJSON.String != "[]" {
			json.Unmarshal([]byte(groupIDsJSON.String), &run.GroupIDs)
		}
		runs = append(runs, run)
	}
	return runs, total, nil
}

// Employee CRUD operations

func (db *SQLiteDB) CreateEmployee(ctx context.Context, employee *Employee) (*Employee, error) {
	if employee.ID == "" {
		employee.ID = uuid.New().String()
	}
	if employee.AvatarColor == "" {
		employee.AvatarColor = "#6366f1"
	}
	now := time.Now()
	employee.CreatedAt = now
	employee.UpdatedAt = now

	_, err := db.db.ExecContext(ctx,
		`INSERT INTO employees (id, name, avatar_color, description, created_at, updated_at, user_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		employee.ID, employee.Name, employee.AvatarColor, employee.Description, employee.CreatedAt, employee.UpdatedAt, employee.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to create employee: %w", err)
	}
	return employee, nil
}

func (db *SQLiteDB) GetEmployee(ctx context.Context, id string) (*Employee, error) {
	var emp Employee
	err := db.db.QueryRowContext(ctx,
		`SELECT id, name, avatar_color, description, created_at, updated_at, COALESCE(user_id, '') FROM employees WHERE id = ?`, id).
		Scan(&emp.ID, &emp.Name, &emp.AvatarColor, &emp.Description, &emp.CreatedAt, &emp.UpdatedAt, &emp.UserID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get employee: %w", err)
	}
	return &emp, nil
}

func (db *SQLiteDB) UpdateEmployee(ctx context.Context, id string, employee *Employee) (*Employee, error) {
	employee.UpdatedAt = time.Now()
	_, err := db.db.ExecContext(ctx,
		`UPDATE employees SET name = ?, avatar_color = ?, description = ?, updated_at = ? WHERE id = ?`,
		employee.Name, employee.AvatarColor, employee.Description, employee.UpdatedAt, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update employee: %w", err)
	}
	return db.GetEmployee(ctx, id)
}

func (db *SQLiteDB) DeleteEmployee(ctx context.Context, id string) error {
	_, err := db.db.ExecContext(ctx, `DELETE FROM employees WHERE id = ?`, id)
	return err
}

func (db *SQLiteDB) ListEmployees(ctx context.Context) ([]Employee, error) {
	rows, err := db.db.QueryContext(ctx,
		`SELECT id, name, avatar_color, description, created_at, updated_at, COALESCE(user_id, '') FROM employees ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("failed to list employees: %w", err)
	}
	defer rows.Close()

	var employees []Employee
	for rows.Next() {
		var emp Employee
		if err := rows.Scan(&emp.ID, &emp.Name, &emp.AvatarColor, &emp.Description, &emp.CreatedAt, &emp.UpdatedAt, &emp.UserID); err != nil {
			return nil, fmt.Errorf("failed to scan employee: %w", err)
		}
		employees = append(employees, emp)
	}
	if employees == nil {
		employees = []Employee{}
	}
	return employees, nil
}
