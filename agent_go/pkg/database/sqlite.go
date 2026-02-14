package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
	"label":                   true,
	"query":                   true,
	"selected_servers":        true,
	"selected_tools":          true,
	"selected_folder":         true,
	"agent_mode":              true,
	"llm_config":              true,
	"use_code_execution_mode": true,
	"use_tool_search_mode":    true,
	"pre_discovered_tools":    true,
	"selected_skills":         true,
	"enable_browser_access":   true,
	"workflow_status":         true,
	"selected_options":        true,
	"updated_at":              true,
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

// hasValidLLMConfig checks if a session config has valid LLM configuration
// Returns false for old sessions without LLM config (null config or missing/empty provider)
func hasValidLLMConfig(config json.RawMessage) bool {
	if config == nil || len(config) == 0 {
		return false
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal(config, &configMap); err != nil {
		return false
	}

	llmConfig, ok := configMap["llm_config"]
	if !ok || llmConfig == nil {
		return false
	}

	llmConfigMap, ok := llmConfig.(map[string]interface{})
	if !ok {
		return false
	}

	provider, ok := llmConfigMap["provider"]
	if !ok || provider == nil {
		return false
	}

	providerStr, ok := provider.(string)
	if !ok || providerStr == "" || providerStr == "." {
		return false
	}

	return true
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
		        WHEN ? = '' THEN NULL 
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

	query := `
		INSERT INTO preset_queries (label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
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
	var returnedEnableBrowserAccessInt int
	err := s.db.QueryRowContext(ctx, query, req.Label, req.Query, selectedServersJSON, selectedToolsJSON, req.SelectedFolder, agentMode, llmConfigParam, useCodeExecutionModeInt, useToolSearchModeInt, preDiscoveredToolsJSON, selectedSkillsJSON, enableBrowserAccessInt, req.IsPredefined, "user").Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &returnedUseCodeExecutionModeInt, &returnedUseToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &returnedEnableBrowserAccessInt, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
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
	preset.EnableBrowserAccess = returnedEnableBrowserAccessInt != 0

	return &preset, nil
}

// GetPresetQuery retrieves a preset query by ID
func (s *SQLiteDB) GetPresetQuery(ctx context.Context, id string) (*PresetQuery, error) {
	query := `
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
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
	var enableBrowserAccessInt sql.NullInt64
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &useCodeExecutionModeInt, &useToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &enableBrowserAccessInt, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
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
	preset.EnableBrowserAccess = enableBrowserAccessInt.Valid && enableBrowserAccessInt.Int64 != 0

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

	if req.EnableBrowserAccess != nil {
		// Convert boolean to INTEGER (0/1) for SQLite
		enableBrowserAccessInt := 0
		if *req.EnableBrowserAccess {
			enableBrowserAccessInt = 1
		}
		updateFields = append(updateFields, "enable_browser_access = ?")
		args = append(args, enableBrowserAccessInt)
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
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
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
	var enableBrowserAccessInt sql.NullInt64
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &useCodeExecutionModeInt, &useToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &enableBrowserAccessInt, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
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
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
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
		var enableBrowserAccessInt sql.NullInt64
		err := rows.Scan(
			&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &useCodeExecutionModeInt, &useToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &enableBrowserAccessInt, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
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

	// Handle userID
	var userIDValue interface{}
	if userID == "" {
		userIDValue = nil
	} else {
		userIDValue = userID
	}

	query := `
		INSERT INTO preset_queries (label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_by, user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
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
	var returnedEnableBrowserAccessInt int
	err := s.db.QueryRowContext(ctx, query, req.Label, req.Query, selectedServersJSON, selectedToolsJSON, req.SelectedFolder, agentMode, llmConfigParam, useCodeExecutionModeInt, useToolSearchModeInt, preDiscoveredToolsJSON, selectedSkillsJSON, enableBrowserAccessInt, req.IsPredefined, "user", userIDValue).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &returnedUseCodeExecutionModeInt, &returnedUseToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &returnedEnableBrowserAccessInt, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
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
	preset.EnableBrowserAccess = returnedEnableBrowserAccessInt != 0

	return &preset, nil
}

// ListPresetQueriesWithUser lists preset queries with pagination and user filtering
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
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
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
		var enableBrowserAccessInt sql.NullInt64
		err := rows.Scan(
			&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &useCodeExecutionModeInt, &useToolSearchModeInt, &preDiscoveredToolsStr, &selectedSkillsStr, &enableBrowserAccessInt, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
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
		presets = append(presets, preset)
	}

	return presets, total, nil
}
