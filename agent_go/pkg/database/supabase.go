package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/manishiitg/mcpagent/events"

	_ "github.com/lib/pq"
)

// SupabaseDB implements the Database interface using PostgreSQL (Supabase)
type SupabaseDB struct {
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

// NewSupabaseDB creates a new Supabase (Postgres) database connection
func NewSupabaseDB(connStr string) (*SupabaseDB, error) {
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Run migrations
	migrationRunner := NewMigrationRunner(db, "postgres")
	if err := migrationRunner.RunMigrations("pkg/database/migrations_postgres"); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	supabaseDB := &SupabaseDB{
		db:               db,
		eventBuffer:      make(map[string][]pendingEvent),
		chatSessionCache: make(map[string]string),
		batchSizeLimit:   50,                     // Flush when batch reaches 50 events
		flushInterval:    500 * time.Millisecond, // Flush every 500ms
		stopFlusher:      make(chan struct{}),
		flushDone:        make(chan struct{}),
	}

	// Start background flusher
	supabaseDB.startFlusher()

	return supabaseDB, nil
}

// GetDB returns the underlying *sql.DB connection
func (s *SupabaseDB) GetDB() *sql.DB {
	return s.db
}

// CreateChatSession creates a new chat session
func (s *SupabaseDB) CreateChatSession(ctx context.Context, req *CreateChatSessionRequest) (*ChatSession, error) {
	query := `
		INSERT INTO chat_sessions (session_id, title, agent_mode, preset_query_id, config, status)
		VALUES ($1, $2, $3, $4, $5, $6)
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

	log.Printf("[CREATE_CHAT_SESSION DEBUG] Successfully created session ID: %s, SessionID: %s, Title: '%s' (length: %d)", session.ID, session.SessionID, session.Title, len(session.Title))

	return &session, nil
}

// GetChatSession retrieves a chat session by session ID
func (s *SupabaseDB) GetChatSession(ctx context.Context, sessionID string) (*ChatSession, error) {
	query := `
		SELECT id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
		FROM chat_sessions
		WHERE session_id = $1
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

// UpdateChatSession updates a chat session
func (s *SupabaseDB) UpdateChatSession(ctx context.Context, sessionID string, req *UpdateChatSessionRequest) (*ChatSession, error) {
	// Postgres CASE WHEN syntax is standard SQL, same as SQLite
	query := `
		UPDATE chat_sessions
		SET title = CASE 
		        WHEN $1 = '' THEN title 
		        ELSE $2 
		    END,
		    agent_mode = COALESCE(NULLIF($3, ''), agent_mode),
		    preset_query_id = CASE 
		        WHEN $4 = '' THEN NULL 
		        ELSE COALESCE(NULLIF($5, ''), preset_query_id) 
		    END,
		    config = CASE
		        WHEN $6::text IS NULL THEN config
		        ELSE $7::text
		    END,
		    status = COALESCE(NULLIF($8, ''), status),
		    completed_at = COALESCE($9, completed_at)
		WHERE session_id = $10
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
		session.AgentMode = ""
	}

	// Handle NULL preset_query_id
	if presetQueryIDStr != nil {
		session.PresetQueryID = presetQueryIDStr
	} else {
		session.PresetQueryID = nil
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
func (s *SupabaseDB) DeleteChatSession(ctx context.Context, sessionID string) error {
	query := `DELETE FROM chat_sessions WHERE session_id = $1`

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

// DeleteWorkflowSessions deletes all chat sessions with agent_mode = 'workflow'
func (s *SupabaseDB) DeleteWorkflowSessions(ctx context.Context) (int64, error) {
	query := `DELETE FROM chat_sessions WHERE agent_mode = $1`

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
func (s *SupabaseDB) ListChatSessions(ctx context.Context, limit, offset int, presetQueryID *string, agentMode *string) ([]ChatHistorySummary, int, error) {
	var whereConditions []string
	var args []interface{}
	argCount := 0

	if presetQueryID != nil && *presetQueryID != "" {
		argCount++
		whereConditions = append(whereConditions, fmt.Sprintf("cs.preset_query_id = $%d", argCount))
		args = append(args, *presetQueryID)
	}

	if agentMode != nil && *agentMode != "" {
		argCount++
		whereConditions = append(whereConditions, fmt.Sprintf("cs.agent_mode = $%d", argCount))
		args = append(args, *agentMode)
	}

	var whereClause string
	if len(whereConditions) > 0 {
		whereClause = " WHERE " + strings.Join(whereConditions, " AND ")
	}

	if err := validateWhereClause(whereClause); err != nil {
		return nil, 0, fmt.Errorf("invalid WHERE clause: %w", err)
	}

	countQuery := `SELECT COUNT(*) FROM chat_sessions cs` + whereClause
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

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
		FROM chat_sessions cs` + whereClause + fmt.Sprintf(`
		ORDER BY cs.created_at DESC
		LIMIT $%d OFFSET $%d
	`, argCount+1, argCount+2)

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

		if agentModeStr != nil {
			session.AgentMode = *agentModeStr
		} else {
			session.AgentMode = ""
		}

		if presetQueryIDStr != nil {
			session.PresetQueryID = *presetQueryIDStr
		} else {
			session.PresetQueryID = ""
		}

		if configStr.Valid {
			session.Config = json.RawMessage(configStr.String)
		} else {
			session.Config = nil
		}

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

// StoreEvent stores an event in the database (batched)
func (s *SupabaseDB) StoreEvent(ctx context.Context, sessionID string, event *events.AgentEvent) error {
	s.batchMux.Lock()
	defer s.batchMux.Unlock()

	if s.eventBuffer[sessionID] == nil {
		s.eventBuffer[sessionID] = make([]pendingEvent, 0, s.batchSizeLimit)
	}

	s.eventBuffer[sessionID] = append(s.eventBuffer[sessionID], pendingEvent{
		sessionID: sessionID,
		event:     event,
		timestamp: time.Now(),
	})

	if len(s.eventBuffer[sessionID]) >= s.batchSizeLimit {
		go s.flushSessionEvents(sessionID)
	}

	return nil
}

// extractParentSessionID extracts the parent session ID from a sub-agent session ID.
// Sub-agent session IDs have format: {parent_session_id}-sub-{n}-{timestamp}
// Returns the original sessionID if it's not a sub-agent session.
func extractParentSessionID(sessionID string) string {
	// Check if this is a sub-agent session ID
	if idx := strings.Index(sessionID, "-sub-"); idx != -1 {
		return sessionID[:idx]
	}
	return sessionID
}

// getChatSessionID gets the chat_session_id for a session
// For sub-agent sessions (format: {parent}-sub-{n}-{ts}), it returns the parent's chat_session_id
func (s *SupabaseDB) getChatSessionID(ctx context.Context, sessionID string) (string, error) {
	// Extract parent session ID for sub-agents
	lookupSessionID := extractParentSessionID(sessionID)

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

	chatSession, err := s.GetChatSession(ctx, lookupSessionID)
	if err != nil {
		return "", fmt.Errorf("failed to get chat session: %w", err)
	}

	s.batchMux.Lock()
	s.chatSessionCache[lookupSessionID] = chatSession.ID
	// Also cache for the original sub-agent session ID
	if lookupSessionID != sessionID {
		s.chatSessionCache[sessionID] = chatSession.ID
	}
	s.batchMux.Unlock()

	return chatSession.ID, nil
}

// flushSessionEvents flushes pending events for a session
func (s *SupabaseDB) flushSessionEvents(sessionID string) {
	s.batchMux.Lock()
	events := s.eventBuffer[sessionID]
	if len(events) == 0 {
		s.batchMux.Unlock()
		return
	}
	eventsCopy := make([]pendingEvent, len(events))
	copy(eventsCopy, events)
	s.batchMux.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("[BATCH ERROR] Failed to begin transaction for session %s: %v", sessionID, err)
		return
	}

	chatSessionID, err := s.getChatSessionID(ctx, sessionID)
	if err != nil {
		tx.Rollback()
		log.Printf("[BATCH ERROR] Failed to get chat session ID for session %s: %v", sessionID, err)
		return
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO events (session_id, chat_session_id, event_type, timestamp, event_data)
		VALUES ($1, $2, $3, $4, $5)
	`)
	if err != nil {
		tx.Rollback()
		log.Printf("[BATCH ERROR] Failed to prepare statement for session %s: %v", sessionID, err)
		return
	}
	defer stmt.Close()

	for _, pending := range eventsCopy {
		eventData, err := json.Marshal(pending.event)
		if err != nil {
			log.Printf("[BATCH ERROR] Failed to marshal event for session %s: %v", sessionID, err)
			continue
		}

		_, err = stmt.ExecContext(ctx, pending.sessionID, chatSessionID, pending.event.Type, pending.event.Timestamp, string(eventData))
		if err != nil {
			log.Printf("[BATCH ERROR] Failed to insert event for session %s: %v", sessionID, err)
			continue
		}
	}

	if err := tx.Commit(); err != nil {
		log.Printf("[BATCH ERROR] Failed to commit transaction for session %s: %v", sessionID, err)
		return
	}

	s.batchMux.Lock()
	if len(s.eventBuffer[sessionID]) >= len(eventsCopy) {
		s.eventBuffer[sessionID] = s.eventBuffer[sessionID][len(eventsCopy):]
		if len(s.eventBuffer[sessionID]) == 0 {
			delete(s.eventBuffer, sessionID)
		}
	} else {
		delete(s.eventBuffer, sessionID)
	}
	s.batchMux.Unlock()

	log.Printf("[BATCH] Flushed %d events for session %s", len(eventsCopy), sessionID)
}

// flushBatches flushes all pending event batches
func (s *SupabaseDB) flushBatches() {
	s.batchMux.Lock()
	sessionsToFlush := make([]string, 0, len(s.eventBuffer))
	for sessionID := range s.eventBuffer {
		if len(s.eventBuffer[sessionID]) > 0 {
			sessionsToFlush = append(sessionsToFlush, sessionID)
		}
	}
	s.batchMux.Unlock()

	for _, sessionID := range sessionsToFlush {
		s.flushSessionEvents(sessionID)
	}
}

// FlushSessionEvents flushes pending events for a specific session
func (s *SupabaseDB) FlushSessionEvents(sessionID string) {
	s.flushSessionEvents(sessionID)
}

// startFlusher starts the background flusher
func (s *SupabaseDB) startFlusher() {
	s.flushTicker = time.NewTicker(s.flushInterval)
	go func() {
		for {
			select {
			case <-s.flushTicker.C:
				s.flushBatches()
			case <-s.stopFlusher:
				s.flushBatches()
				close(s.flushDone)
				return
			}
		}
	}()
}

// GetEvents retrieves events
func (s *SupabaseDB) GetEvents(ctx context.Context, req *GetChatHistoryRequest) (*GetEventsResponse, error) {
	whereClause := "WHERE 1=1"
	args := []interface{}{}
	argCount := 0

	if req.SessionID != "" {
		argCount++
		whereClause += fmt.Sprintf(" AND session_id = $%d", argCount)
		args = append(args, req.SessionID)
	}

	if req.EventType != "" {
		argCount++
		whereClause += fmt.Sprintf(" AND event_type = $%d", argCount)
		args = append(args, req.EventType)
	}

	if !req.FromDate.IsZero() {
		argCount++
		whereClause += fmt.Sprintf(" AND timestamp >= $%d", argCount)
		args = append(args, req.FromDate)
	}

	if !req.ToDate.IsZero() {
		argCount++
		whereClause += fmt.Sprintf(" AND timestamp <= $%d", argCount)
		args = append(args, req.ToDate)
	}

	if err := validateWhereClause(whereClause); err != nil {
		return nil, fmt.Errorf("invalid WHERE clause: %w", err)
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM events %s", whereClause)
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("failed to get total count: %w", err)
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100
	}

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`
		SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
		FROM events %s
		ORDER BY timestamp DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argCount+1, argCount+2)

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
func (s *SupabaseDB) GetEventsBySession(ctx context.Context, sessionID string, limit, offset int) ([]Event, error) {
	// Resolve session_id UUID → internal hex id first, then query by chat_session_id.
	// Two-step approach avoids OR which prevents index usage on large tables.
	var internalID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM chat_sessions WHERE session_id = $1`, sessionID,
	).Scan(&internalID)
	if err != nil {
		// Fallback: try using sessionID directly as chat_session_id
		internalID = sessionID
	}

	query := `
		SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
		FROM events
		WHERE chat_session_id = $1
		ORDER BY timestamp ASC
		LIMIT $2 OFFSET $3
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

		err = json.Unmarshal([]byte(eventDataJSON), &event.EventData)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
		}

		events = append(events, event)
	}

	return events, nil
}

// GetEventsByCorrelationID retrieves events for a session filtered by correlation_id (stored in event_data JSON)
func (s *SupabaseDB) GetEventsByCorrelationID(ctx context.Context, sessionID string, correlationID string, limit, offset int) ([]Event, error) {
	var internalID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM chat_sessions WHERE session_id = $1`, sessionID,
	).Scan(&internalID)
	if err != nil {
		internalID = sessionID
	}

	query := `
		SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
		FROM events
		WHERE chat_session_id = $1
		  AND event_data->>'correlation_id' = $2
		ORDER BY timestamp ASC
		LIMIT $3 OFFSET $4
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
func (s *SupabaseDB) CountEventsBySession(ctx context.Context, sessionID string) (int, error) {
	var internalID string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM chat_sessions WHERE session_id = $1`, sessionID,
	).Scan(&internalID)
	if err != nil {
		internalID = sessionID
	}

	var count int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM events WHERE chat_session_id = $1`, internalID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count events: %w", err)
	}
	return count, nil
}

// Ping tests the database connection
func (s *SupabaseDB) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// CreatePresetQuery creates a new preset query
func (s *SupabaseDB) CreatePresetQuery(ctx context.Context, req *CreatePresetQueryRequest) (*PresetQuery, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	selectedServersJSON := "[]"
	if len(req.SelectedServers) > 0 {
		serversJSON, err := json.Marshal(req.SelectedServers)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected servers: %w", err)
		}
		selectedServersJSON = string(serversJSON)
	}

	selectedToolsJSON := "[]"
	if len(req.SelectedTools) > 0 {
		toolsJSON, err := json.Marshal(req.SelectedTools)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected tools: %w", err)
		}
		selectedToolsJSON = string(toolsJSON)
	}

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

	agentMode := req.AgentMode
	if agentMode == "" {
		agentMode = AgentModeSimple
	}

	var preset PresetQuery
	var selectedServersStr string
	var selectedToolsStr string
	var selectedFolderStr sql.NullString
	var llmConfigNullStr sql.NullString
	var preDiscoveredToolsStr sql.NullString
	var selectedSkillsStr sql.NullString

	preDiscoveredToolsJSON := "[]"
	if len(req.PreDiscoveredTools) > 0 {
		toolsJSON, err := json.Marshal(req.PreDiscoveredTools)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal pre-discovered tools: %w", err)
		}
		preDiscoveredToolsJSON = string(toolsJSON)
	}

	selectedSkillsJSON := "[]"
	if len(req.SelectedSkills) > 0 {
		skillsJSON, err := json.Marshal(req.SelectedSkills)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected skills: %w", err)
		}
		selectedSkillsJSON = string(skillsJSON)
	}

	selectedSecretsJSON := "[]"
	if len(req.SelectedSecrets) > 0 {
		secretsJSON, err := json.Marshal(req.SelectedSecrets)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected secrets: %w", err)
		}
		selectedSecretsJSON = string(secretsJSON)
	}

	var selectedGlobalSecretNamesParam interface{}
	if req.SelectedGlobalSecretNames != nil {
		gsJSON, err := json.Marshal(*req.SelectedGlobalSecretNames)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected global secret names: %w", err)
		}
		selectedGlobalSecretNamesParam = string(gsJSON)
	} else {
		selectedGlobalSecretNamesParam = nil
	}

	query := `
		INSERT INTO preset_queries (label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, is_predefined, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, is_predefined, created_at, updated_at, created_by
	`

	var selectedSecretsStr sql.NullString
	var selectedGlobalSecretNamesStr sql.NullString
	err := s.db.QueryRowContext(ctx, query,
		req.Label,
		req.Query,
		selectedServersJSON,
		selectedToolsJSON,
		req.SelectedFolder,
		agentMode,
		llmConfigParam,
		req.UseCodeExecutionMode,
		req.UseToolSearchMode,
		preDiscoveredToolsJSON,
		selectedSkillsJSON,
		selectedSecretsJSON,
		selectedGlobalSecretNamesParam,
		req.EnableBrowserAccess,
		req.IsPredefined,
		"user",
	).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create preset query: %w", err)
	}

	preset.SelectedServers = selectedServersStr
	preset.SelectedTools = selectedToolsStr
	preset.SelectedFolder = selectedFolderStr
	preset.PreDiscoveredTools = preDiscoveredToolsStr.String
	preset.SelectedSkills = selectedSkillsStr.String
	if selectedSecretsStr.Valid {
		preset.SelectedSecrets = selectedSecretsStr.String
	} else {
		preset.SelectedSecrets = "[]"
	}
	if selectedGlobalSecretNamesStr.Valid {
		preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
	}
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = json.RawMessage("null")
	}

	return &preset, nil
}

// GetPresetQuery retrieves a preset query by ID
func (s *SupabaseDB) GetPresetQuery(ctx context.Context, id string) (*PresetQuery, error) {
	query := `
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, is_predefined, created_at, updated_at, created_by
		FROM preset_queries
		WHERE id = $1
	`

	var preset PresetQuery
	var selectedServersStr string
	var selectedToolsStr string
	var selectedFolderStr sql.NullString
	var llmConfigNullStr sql.NullString
	var preDiscoveredToolsStr sql.NullString
	var selectedSkillsStr sql.NullString
	var selectedSecretsStr sql.NullString
	var selectedGlobalSecretNamesStr sql.NullString

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
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
	preset.PreDiscoveredTools = preDiscoveredToolsStr.String
	preset.SelectedSkills = selectedSkillsStr.String
	if selectedSecretsStr.Valid {
		preset.SelectedSecrets = selectedSecretsStr.String
	} else {
		preset.SelectedSecrets = "[]"
	}
	if selectedGlobalSecretNamesStr.Valid {
		preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
	}
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = nil
	}

	return &preset, nil
}

// UpdatePresetQuery updates a preset query
func (s *SupabaseDB) UpdatePresetQuery(ctx context.Context, id string, req *UpdatePresetQueryRequest) (*PresetQuery, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	updateFields := []string{}
	args := []interface{}{}
	argCount := 0

	if req.Label != "" {
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("label = $%d", argCount))
		args = append(args, req.Label)
	}

	if req.Query != "" {
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("query = $%d", argCount))
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
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("selected_servers = $%d", argCount))
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
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("selected_tools = $%d", argCount))
		args = append(args, selectedToolsJSON)
	}

	if req.SelectedFolder != "" {
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("selected_folder = $%d", argCount))
		args = append(args, req.SelectedFolder)
	}

	if req.AgentMode != "" {
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("agent_mode = $%d", argCount))
		args = append(args, req.AgentMode)
	}

	if req.LLMConfig != nil {
		llmConfigBytes, err := json.Marshal(req.LLMConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal LLM config: %w", err)
		}
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("llm_config = $%d", argCount))
		args = append(args, string(llmConfigBytes))
	}

	if req.UseCodeExecutionMode != nil {
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("use_code_execution_mode = $%d", argCount))
		args = append(args, *req.UseCodeExecutionMode)
	}

	if req.UseToolSearchMode != nil {
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("use_tool_search_mode = $%d", argCount))
		args = append(args, *req.UseToolSearchMode)
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
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("pre_discovered_tools = $%d", argCount))
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
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("selected_skills = $%d", argCount))
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
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("selected_secrets = $%d", argCount))
		args = append(args, selectedSecretsJSON)
	}

	if req.SelectedGlobalSecretNames != nil {
		gsJSON, err := json.Marshal(*req.SelectedGlobalSecretNames)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected global secret names: %w", err)
		}
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("selected_global_secret_names = $%d", argCount))
		args = append(args, string(gsJSON))
	}

	if req.EnableBrowserAccess != nil {
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("enable_browser_access = $%d", argCount))
		args = append(args, *req.EnableBrowserAccess)
	}

	if len(updateFields) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	updateFields = append(updateFields, "updated_at = CURRENT_TIMESTAMP")

	argCount++
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE preset_queries
		SET %s
		WHERE id = $%d
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, is_predefined, created_at, updated_at, created_by
	`, strings.Join(updateFields, ", "), argCount)

	var preset PresetQuery
	var selectedServersStr string
	var selectedToolsStr string
	var selectedFolderStr sql.NullString
	var llmConfigNullStr sql.NullString
	var preDiscoveredToolsStr sql.NullString
	var selectedSkillsStr sql.NullString
	var selectedSecretsStr sql.NullString
	var selectedGlobalSecretNamesStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
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
	preset.PreDiscoveredTools = preDiscoveredToolsStr.String
	preset.SelectedSkills = selectedSkillsStr.String
	if selectedSecretsStr.Valid {
		preset.SelectedSecrets = selectedSecretsStr.String
	} else {
		preset.SelectedSecrets = "[]"
	}
	if selectedGlobalSecretNamesStr.Valid {
		preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
	}
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = nil
	}

	return &preset, nil
}

// DeletePresetQuery deletes a preset query
func (s *SupabaseDB) DeletePresetQuery(ctx context.Context, id string) error {
	query := `DELETE FROM preset_queries WHERE id = $1`

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
func (s *SupabaseDB) ListPresetQueries(ctx context.Context, limit, offset int) ([]PresetQuery, int, error) {
	countQuery := `SELECT COUNT(*) FROM preset_queries`
	var total int
	err := s.db.QueryRowContext(ctx, countQuery).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	query := `
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, is_predefined, created_at, updated_at, created_by
		FROM preset_queries
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`

	rows, err := s.db.QueryContext(ctx, query, limit, offset)
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
		var preDiscoveredToolsStr sql.NullString
		var selectedSkillsStr sql.NullString
		var selectedSecretsStr sql.NullString
		var selectedGlobalSecretNamesStr sql.NullString

		err := rows.Scan(
			&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan preset query: %w", err)
		}

		preset.SelectedServers = selectedServersStr
		preset.SelectedTools = selectedToolsStr
		preset.SelectedFolder = selectedFolderStr
		preset.PreDiscoveredTools = preDiscoveredToolsStr.String
		preset.SelectedSkills = selectedSkillsStr.String
		if selectedSecretsStr.Valid {
			preset.SelectedSecrets = selectedSecretsStr.String
		} else {
			preset.SelectedSecrets = "[]"
		}
		if selectedGlobalSecretNamesStr.Valid {
			preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
		}
		if llmConfigNullStr.Valid {
			preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
		} else {
			preset.LLMConfig = json.RawMessage("null")
		}

		presets = append(presets, preset)
	}

	return presets, total, nil
}

// CreateWorkflow creates a new workflow
func (s *SupabaseDB) CreateWorkflow(ctx context.Context, req *CreateWorkflowRequest) (*Workflow, error) {
	workflowStatus := req.WorkflowStatus
	if workflowStatus == "" {
		workflowStatus = WorkflowStatusPreVerification
	}

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
		VALUES ($1, $2, $3)
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
func (s *SupabaseDB) GetWorkflowByPresetQueryID(ctx context.Context, presetQueryID string) (*Workflow, error) {
	query := `
		SELECT id, preset_query_id, workflow_status, selected_options, created_at, updated_at
		FROM workflows
		WHERE preset_query_id = $1
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

	if selectedOptionJSON.Valid && selectedOptionJSON.String != "" {
		var selectedOptions WorkflowSelectedOptions
		if err := json.Unmarshal([]byte(selectedOptionJSON.String), &selectedOptions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal selected_options: %w", err)
		}
		workflow.SelectedOptions = &selectedOptions
	}

	return &workflow, nil
}

// UpdateWorkflow updates a workflow
func (s *SupabaseDB) UpdateWorkflow(ctx context.Context, presetQueryID string, req *UpdateWorkflowRequest) (*Workflow, error) {
	existingWorkflow, err := s.GetWorkflowByPresetQueryID(ctx, presetQueryID)
	if err != nil && !strings.Contains(err.Error(), "workflow not found for preset query") {
		return nil, fmt.Errorf("failed to check existing workflow: %w", err)
	}

	if existingWorkflow == nil {
		workflowStatus := "execution"
		if req.WorkflowStatus != nil {
			workflowStatus = *req.WorkflowStatus
		}

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

	updateFields := []string{}
	args := []interface{}{}
	argCount := 0

	if req.WorkflowStatus != nil {
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("workflow_status = $%d", argCount))
		args = append(args, *req.WorkflowStatus)
	}

	if req.SelectedOptions != nil {
		selectedOptionsJSON, err := json.Marshal(*req.SelectedOptions)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected_options: %w", err)
		}
		argCount++
		updateFields = append(updateFields, fmt.Sprintf("selected_options = $%d", argCount))
		args = append(args, string(selectedOptionsJSON))
	}

	if len(updateFields) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	updateFields = append(updateFields, "updated_at = CURRENT_TIMESTAMP")
	argCount++
	args = append(args, presetQueryID)

	query := fmt.Sprintf(`
		UPDATE workflows
		SET %s
		WHERE preset_query_id = $%d
		RETURNING id, preset_query_id, workflow_status, selected_options, created_at, updated_at
	`, strings.Join(updateFields, ", "), argCount)

	var workflow Workflow
	var selectedOptionJSON sql.NullString
	err = s.db.QueryRowContext(ctx, query, args...).Scan(
		&workflow.ID, &workflow.PresetQueryID, &workflow.WorkflowStatus,
		&selectedOptionJSON, &workflow.CreatedAt, &workflow.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

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
func (s *SupabaseDB) DeleteWorkflow(ctx context.Context, presetQueryID string) error {
	query := `DELETE FROM workflows WHERE preset_query_id = $1`

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

// Close closes the database connection
func (s *SupabaseDB) Close() error {
	if s.flushTicker != nil {
		s.flushTicker.Stop()
		close(s.stopFlusher)
		<-s.flushDone
	}

	s.flushBatches()

	return s.db.Close()
}

// ============================================================================
// User-aware methods for multi-user support
// ============================================================================

// CreateChatSessionWithUser creates a new chat session with user association
func (s *SupabaseDB) CreateChatSessionWithUser(ctx context.Context, req *CreateChatSessionRequest, userID string) (*ChatSession, error) {
	query := `
		INSERT INTO chat_sessions (session_id, title, agent_mode, preset_query_id, config, status, user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
	`

	log.Printf("[CREATE_CHAT_SESSION DEBUG] Creating session with SessionID: %s, Title: '%s', AgentMode: '%s', UserID: '%s'", req.SessionID, req.Title, req.AgentMode, userID)

	var presetQueryID interface{}
	if req.PresetQueryID == "" {
		presetQueryID = nil
	} else {
		presetQueryID = req.PresetQueryID
	}

	var configValue interface{}
	if len(req.Config) == 0 {
		configValue = nil
	} else {
		configValue = string(req.Config)
	}

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

	if agentModeStr != nil {
		session.AgentMode = *agentModeStr
	} else {
		session.AgentMode = ""
	}

	if presetQueryIDStr != nil {
		session.PresetQueryID = presetQueryIDStr
	}

	if configStr.Valid {
		session.Config = json.RawMessage(configStr.String)
	} else {
		session.Config = nil
	}

	log.Printf("[CREATE_CHAT_SESSION DEBUG] Successfully created session ID: %s, SessionID: %s, UserID: %s", session.ID, session.SessionID, userID)

	return &session, nil
}

// GetChatSessionWithUser retrieves a chat session by session ID with user verification
func (s *SupabaseDB) GetChatSessionWithUser(ctx context.Context, sessionID string, userID string) (*ChatSession, error) {
	var query string
	var args []interface{}

	if userID == "" {
		query = `
			SELECT id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
			FROM chat_sessions
			WHERE session_id = $1
		`
		args = []interface{}{sessionID}
	} else {
		query = `
			SELECT id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
			FROM chat_sessions
			WHERE session_id = $1 AND (user_id = $2 OR user_id IS NULL)
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

	if agentModeStr != nil {
		session.AgentMode = *agentModeStr
	} else {
		session.AgentMode = ""
	}

	if presetQueryIDStr != nil {
		session.PresetQueryID = presetQueryIDStr
	}

	if configStr.Valid {
		session.Config = json.RawMessage(configStr.String)
	} else {
		session.Config = nil
	}

	return &session, nil
}

// ListChatSessionsWithUser lists chat sessions with pagination and user filtering
func (s *SupabaseDB) ListChatSessionsWithUser(ctx context.Context, limit, offset int, presetQueryID *string, agentMode *string, userID string) ([]ChatHistorySummary, int, error) {
	var whereConditions []string
	var args []interface{}
	argCount := 0

	if presetQueryID != nil && *presetQueryID != "" {
		argCount++
		whereConditions = append(whereConditions, fmt.Sprintf("cs.preset_query_id = $%d", argCount))
		args = append(args, *presetQueryID)
	}

	if agentMode != nil && *agentMode != "" {
		argCount++
		whereConditions = append(whereConditions, fmt.Sprintf("cs.agent_mode = $%d", argCount))
		args = append(args, *agentMode)
	}

	if userID != "" {
		argCount++
		whereConditions = append(whereConditions, fmt.Sprintf("(cs.user_id = $%d OR cs.user_id IS NULL)", argCount))
		args = append(args, userID)
	}

	var whereClause string
	if len(whereConditions) > 0 {
		whereClause = " WHERE " + strings.Join(whereConditions, " AND ")
	}

	if err := validateWhereClause(whereClause); err != nil {
		return nil, 0, fmt.Errorf("invalid WHERE clause: %w", err)
	}

	countQuery := `SELECT COUNT(*) FROM chat_sessions cs` + whereClause
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}
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
		FROM chat_sessions cs` + whereClause + fmt.Sprintf(`
		ORDER BY cs.created_at DESC
		LIMIT $%d OFFSET $%d
	`, argCount+1, argCount+2)

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

		if agentModeStr != nil {
			session.AgentMode = *agentModeStr
		} else {
			session.AgentMode = ""
		}

		if presetQueryIDStr != nil {
			session.PresetQueryID = *presetQueryIDStr
		} else {
			session.PresetQueryID = ""
		}

		if configStr.Valid {
			session.Config = json.RawMessage(configStr.String)
		} else {
			session.Config = nil
		}

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
func (s *SupabaseDB) CreatePresetQueryWithUser(ctx context.Context, req *CreatePresetQueryRequest, userID string) (*PresetQuery, error) {
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	selectedServersJSON := "[]"
	if len(req.SelectedServers) > 0 {
		serversJSON, err := json.Marshal(req.SelectedServers)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected servers: %w", err)
		}
		selectedServersJSON = string(serversJSON)
	}

	selectedToolsJSON := "[]"
	if len(req.SelectedTools) > 0 {
		toolsJSON, err := json.Marshal(req.SelectedTools)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected tools: %w", err)
		}
		selectedToolsJSON = string(toolsJSON)
	}

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

	agentMode := req.AgentMode
	if agentMode == "" {
		agentMode = AgentModeSimple
	}

	preDiscoveredToolsJSON := "[]"
	if len(req.PreDiscoveredTools) > 0 {
		toolsJSON, err := json.Marshal(req.PreDiscoveredTools)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal pre-discovered tools: %w", err)
		}
		preDiscoveredToolsJSON = string(toolsJSON)
	}

	selectedSkillsJSON := "[]"
	if len(req.SelectedSkills) > 0 {
		skillsJSON, err := json.Marshal(req.SelectedSkills)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected skills: %w", err)
		}
		selectedSkillsJSON = string(skillsJSON)
	}

	selectedSecretsJSON := "[]"
	if len(req.SelectedSecrets) > 0 {
		secretsJSON, err := json.Marshal(req.SelectedSecrets)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected secrets: %w", err)
		}
		selectedSecretsJSON = string(secretsJSON)
	}

	var selectedGlobalSecretNamesParam interface{}
	if req.SelectedGlobalSecretNames != nil {
		gsJSON, err := json.Marshal(*req.SelectedGlobalSecretNames)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal selected global secret names: %w", err)
		}
		selectedGlobalSecretNamesParam = string(gsJSON)
	} else {
		selectedGlobalSecretNamesParam = nil
	}

	var userIDValue interface{}
	if userID == "" {
		userIDValue = nil
	} else {
		userIDValue = userID
	}

	query := `
		INSERT INTO preset_queries (label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, is_predefined, created_by, user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, is_predefined, created_at, updated_at, created_by
	`

	var preset PresetQuery
	var selectedServersStr string
	var selectedToolsStr string
	var selectedFolderStr sql.NullString
	var llmConfigNullStr sql.NullString
	var preDiscoveredToolsStr sql.NullString
	var selectedSkillsStr sql.NullString
	var selectedSecretsStr sql.NullString
	var selectedGlobalSecretNamesStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, req.Label, req.Query, selectedServersJSON, selectedToolsJSON, req.SelectedFolder, agentMode, llmConfigParam, req.UseCodeExecutionMode, req.UseToolSearchMode, preDiscoveredToolsJSON, selectedSkillsJSON, selectedSecretsJSON, selectedGlobalSecretNamesParam, req.EnableBrowserAccess, req.IsPredefined, "user", userIDValue).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create preset query: %w", err)
	}

	preset.SelectedServers = selectedServersStr
	preset.SelectedTools = selectedToolsStr
	preset.SelectedFolder = selectedFolderStr
	if selectedSecretsStr.Valid {
		preset.SelectedSecrets = selectedSecretsStr.String
	} else {
		preset.SelectedSecrets = "[]"
	}
	if selectedGlobalSecretNamesStr.Valid {
		preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
	}
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = json.RawMessage("null")
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

	return &preset, nil
}

// ListPresetQueriesWithUser lists preset queries with pagination and user filtering
func (s *SupabaseDB) ListPresetQueriesWithUser(ctx context.Context, limit, offset int, userID string) ([]PresetQuery, int, error) {
	var whereClause string
	var args []interface{}
	argCount := 0

	if userID != "" {
		argCount++
		whereClause = fmt.Sprintf(" WHERE (user_id = $%d OR user_id IS NULL OR is_predefined = true)", argCount)
		args = append(args, userID)
	}

	countQuery := `SELECT COUNT(*) FROM preset_queries` + whereClause
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	query := `
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, selected_secrets, selected_global_secret_names, enable_browser_access, is_predefined, created_at, updated_at, created_by
		FROM preset_queries` + whereClause + fmt.Sprintf(`
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, argCount+1, argCount+2)

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
		var preDiscoveredToolsStr sql.NullString
		var selectedSkillsStr sql.NullString
		var selectedSecretsStr sql.NullString
		var selectedGlobalSecretNamesStr sql.NullString
		err := rows.Scan(
			&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &selectedSecretsStr, &selectedGlobalSecretNamesStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan preset query: %w", err)
		}

		preset.SelectedServers = selectedServersStr
		preset.SelectedTools = selectedToolsStr
		preset.SelectedFolder = selectedFolderStr
		if selectedSecretsStr.Valid {
			preset.SelectedSecrets = selectedSecretsStr.String
		} else {
			preset.SelectedSecrets = "[]"
		}
		if selectedGlobalSecretNamesStr.Valid {
			preset.SelectedGlobalSecretNames = selectedGlobalSecretNamesStr.String
		}
		if llmConfigNullStr.Valid {
			preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
		} else {
			preset.LLMConfig = json.RawMessage("null")
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
		presets = append(presets, preset)
	}

	return presets, total, nil
}

// --- Bot Connector Config CRUD (PostgreSQL) ---

func (s *SupabaseDB) UpsertBotConnectorConfig(ctx context.Context, req *CreateBotConnectorConfigRequest) (*BotConnectorConfig, error) {
	query := `
		INSERT INTO bot_connector_config (id, enabled, bot_mode, config_json, default_preset_id, auto_confirm, allowed_channels, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT(id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			bot_mode = EXCLUDED.bot_mode,
			config_json = EXCLUDED.config_json,
			default_preset_id = EXCLUDED.default_preset_id,
			auto_confirm = EXCLUDED.auto_confirm,
			allowed_channels = EXCLUDED.allowed_channels,
			updated_at = NOW()
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

func (s *SupabaseDB) GetBotConnectorConfig(ctx context.Context, id string) (*BotConnectorConfig, error) {
	query := `SELECT id, enabled, bot_mode, config_json, default_preset_id, auto_confirm, allowed_channels, created_at, updated_at FROM bot_connector_config WHERE id = $1`

	var cfg BotConnectorConfig
	var defaultPresetID, configJSON, allowedChannels sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&cfg.ID, &cfg.Enabled, &cfg.BotMode, &configJSON,
		&defaultPresetID, &cfg.AutoConfirm, &allowedChannels,
		&cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("bot connector config not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get bot connector config: %w", err)
	}

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

func (s *SupabaseDB) ListBotConnectorConfigs(ctx context.Context) ([]BotConnectorConfig, error) {
	query := `SELECT id, enabled, bot_mode, config_json, default_preset_id, auto_confirm, allowed_channels, created_at, updated_at FROM bot_connector_config ORDER BY id`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list bot connector configs: %w", err)
	}
	defer rows.Close()

	configs := make([]BotConnectorConfig, 0)
	for rows.Next() {
		var cfg BotConnectorConfig
		var defaultPresetID, configJSON, allowedChannels sql.NullString
		err := rows.Scan(
			&cfg.ID, &cfg.Enabled, &cfg.BotMode, &configJSON,
			&defaultPresetID, &cfg.AutoConfirm, &allowedChannels,
			&cfg.CreatedAt, &cfg.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan bot connector config: %w", err)
		}
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

func (s *SupabaseDB) CreateBotSession(ctx context.Context, req *CreateBotSessionRequest) (*BotSession, error) {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	query := `
		INSERT INTO bot_sessions (id, platform, channel_id, thread_ts, user_id, user_name, query, status, thread_context)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'analyzing', $8)
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

func (s *SupabaseDB) GetBotSession(ctx context.Context, id string) (*BotSession, error) {
	query := `
		SELECT id, platform, channel_id, thread_ts, session_id, user_id, user_name, query, status,
		       preset_id, config_json, thread_context, created_at, updated_at, completed_at
		FROM bot_sessions WHERE id = $1
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

func (s *SupabaseDB) GetBotSessionByThread(ctx context.Context, platform, channelID, threadTS string) (*BotSession, error) {
	query := `
		SELECT id FROM bot_sessions
		WHERE platform = $1 AND channel_id = $2 AND thread_ts = $3
		ORDER BY created_at DESC LIMIT 1
	`

	var id string
	err := s.db.QueryRowContext(ctx, query, platform, channelID, threadTS).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get bot session by thread: %w", err)
	}

	return s.GetBotSession(ctx, id)
}

func (s *SupabaseDB) GetBotSessionBySessionID(ctx context.Context, sessionID string) (*BotSession, error) {
	query := `SELECT id FROM bot_sessions WHERE session_id = $1 LIMIT 1`

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

func (s *SupabaseDB) UpdateBotSession(ctx context.Context, id string, req *UpdateBotSessionRequest) (*BotSession, error) {
	var updateFields []string
	var args []interface{}
	paramIdx := 1

	if req.SessionID != "" {
		updateFields = append(updateFields, fmt.Sprintf("session_id = $%d", paramIdx))
		args = append(args, req.SessionID)
		paramIdx++
	}
	if req.Status != "" {
		updateFields = append(updateFields, fmt.Sprintf("status = $%d", paramIdx))
		args = append(args, req.Status)
		paramIdx++
	}
	if req.PresetID != "" {
		updateFields = append(updateFields, fmt.Sprintf("preset_id = $%d", paramIdx))
		args = append(args, req.PresetID)
		paramIdx++
	}
	if req.ConfigJSON != "" {
		updateFields = append(updateFields, fmt.Sprintf("config_json = $%d", paramIdx))
		args = append(args, req.ConfigJSON)
		paramIdx++
	}

	if len(updateFields) == 0 {
		return s.GetBotSession(ctx, id)
	}

	updateFields = append(updateFields, "updated_at = NOW()")
	args = append(args, id)

	query := fmt.Sprintf("UPDATE bot_sessions SET %s WHERE id = $%d", strings.Join(updateFields, ", "), paramIdx)
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

func (s *SupabaseDB) CompleteBotSession(ctx context.Context, id string, status string) error {
	query := `UPDATE bot_sessions SET status = $1, completed_at = NOW(), updated_at = NOW() WHERE id = $2`

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

func (s *SupabaseDB) ListBotSessions(ctx context.Context, limit, offset int, status string) ([]BotSession, int, error) {
	var whereClause string
	var args []interface{}
	paramIdx := 1

	if status != "" {
		whereClause = fmt.Sprintf(" WHERE status = $%d", paramIdx)
		args = append(args, status)
		paramIdx++
	}

	countQuery := "SELECT COUNT(*) FROM bot_sessions" + whereClause
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count bot sessions: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, platform, channel_id, thread_ts, session_id, user_id, user_name, query, status,
		       preset_id, config_json, thread_context, created_at, updated_at, completed_at
		FROM bot_sessions%s
		ORDER BY created_at DESC LIMIT $%d OFFSET $%d
	`, whereClause, paramIdx, paramIdx+1)
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

func (s *SupabaseDB) CreateBotMessage(ctx context.Context, req *CreateBotMessageRequest) (*BotMessage, error) {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	query := `
		INSERT INTO bot_messages (id, bot_session_id, direction, message_type, content, platform_message_id)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := s.db.ExecContext(ctx, query,
		id, req.BotSessionID, req.Direction, req.MessageType,
		req.Content, req.PlatformMessageID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot message: %w", err)
	}

	var msg BotMessage
	getQuery := `SELECT id, bot_session_id, direction, message_type, content, platform_message_id, created_at FROM bot_messages WHERE id = $1`
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

func (s *SupabaseDB) ListBotMessages(ctx context.Context, botSessionID string, limit, offset int) ([]BotMessage, int, error) {
	countQuery := `SELECT COUNT(*) FROM bot_messages WHERE bot_session_id = $1`
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, botSessionID).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count bot messages: %w", err)
	}

	query := `
		SELECT id, bot_session_id, direction, message_type, content, platform_message_id, created_at
		FROM bot_messages WHERE bot_session_id = $1
		ORDER BY created_at ASC LIMIT $2 OFFSET $3
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

// --- App Users CRUD ---

func (s *SupabaseDB) UpsertAppUser(ctx context.Context, userID, email, username, provider string) error {
	query := `
		INSERT INTO app_users (user_id, email, username, provider, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT(user_id) DO UPDATE SET
			email = EXCLUDED.email,
			username = EXCLUDED.username,
			provider = EXCLUDED.provider,
			updated_at = NOW()
	`
	_, err := s.db.ExecContext(ctx, query, userID, email, username, provider)
	if err != nil {
		return fmt.Errorf("failed to upsert app user: %w", err)
	}
	return nil
}

func (s *SupabaseDB) GetAppUserByEmail(ctx context.Context, email string) (*AppUser, error) {
	query := `SELECT user_id, email, username, provider, created_at, updated_at FROM app_users WHERE email = $1`
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

func (s *SupabaseDB) UpsertUserSecret(ctx context.Context, userID, name, encryptedValue string) error {
	id := uuid.New().String()
	query := `
		INSERT INTO user_secrets (id, user_id, name, encrypted_value, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT(user_id, name) DO UPDATE SET
			encrypted_value = EXCLUDED.encrypted_value,
			updated_at = NOW()
	`
	_, err := s.db.ExecContext(ctx, query, id, userID, name, encryptedValue)
	if err != nil {
		return fmt.Errorf("failed to upsert user secret: %w", err)
	}
	return nil
}

func (s *SupabaseDB) DeleteUserSecret(ctx context.Context, userID, name string) error {
	query := `DELETE FROM user_secrets WHERE user_id = $1 AND name = $2`
	_, err := s.db.ExecContext(ctx, query, userID, name)
	if err != nil {
		return fmt.Errorf("failed to delete user secret: %w", err)
	}
	return nil
}

func (s *SupabaseDB) ListUserSecrets(ctx context.Context, userID string) ([]UserSecret, error) {
	query := `SELECT id, user_id, name, encrypted_value, created_at, updated_at FROM user_secrets WHERE user_id = $1 ORDER BY name`
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

func scanScheduledJobPG(row interface {
	Scan(dest ...interface{}) error
}) (*ScheduledJob, error) {
	var job ScheduledJob
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
		&job.Enabled, &lastRunAtStr, &nextRunAtStr,
		&lastSessionIDStr, &lastStatusStr, &lastErrorStr, &lastDurationMs,
		&job.RunCount, &job.ConsecutiveFailures, &job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
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

func (s *SupabaseDB) CreateScheduledJob(ctx context.Context, req *CreateScheduledJobRequest) (*ScheduledJob, error) {
	id := uuid.New().String()
	enabled := true
	if req.Enabled != nil && !*req.Enabled {
		enabled = false
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
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 0, 0, NOW(), NOW())
	`
	_, err := s.db.ExecContext(ctx, query, id, req.Name, req.Description, req.EntityType, req.PresetQueryID,
		triggerPayload, groupIDs, req.CronExpression, tz, enabled)
	if err != nil {
		return nil, fmt.Errorf("failed to create scheduled job: %w", err)
	}
	return s.GetScheduledJob(ctx, id)
}

func (s *SupabaseDB) GetScheduledJob(ctx context.Context, id string) (*ScheduledJob, error) {
	query := `SELECT id, name, description, entity_type, preset_query_id, trigger_payload,
		group_ids, cron_expression, timezone, enabled, last_run_at, next_run_at,
		last_session_id, last_status, last_error, last_duration_ms, run_count, consecutive_failures, created_at, updated_at
		FROM scheduled_jobs WHERE id = $1`
	row := s.db.QueryRowContext(ctx, query, id)
	job, err := scanScheduledJobPG(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get scheduled job: %w", err)
	}
	return job, nil
}

func (s *SupabaseDB) UpdateScheduledJob(ctx context.Context, id string, req *UpdateScheduledJobRequest) (*ScheduledJob, error) {
	setClauses := []string{"updated_at = NOW()"}
	args := []interface{}{}
	argIdx := 0

	if req.Name != "" {
		argIdx++
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, req.Name)
	}
	if req.Description != "" {
		argIdx++
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, req.Description)
	}
	if len(req.TriggerPayload) > 0 {
		argIdx++
		setClauses = append(setClauses, fmt.Sprintf("trigger_payload = $%d", argIdx))
		args = append(args, string(req.TriggerPayload))
	}
	if req.SetGroupIDs {
		if len(req.GroupIDs) > 0 {
			b, _ := json.Marshal(req.GroupIDs)
			argIdx++
			setClauses = append(setClauses, fmt.Sprintf("group_ids = $%d", argIdx))
			args = append(args, string(b))
		} else {
			setClauses = append(setClauses, "group_ids = NULL")
		}
	}
	if req.CronExpression != "" {
		argIdx++
		setClauses = append(setClauses, fmt.Sprintf("cron_expression = $%d", argIdx))
		args = append(args, req.CronExpression)
	}
	if req.Timezone != "" {
		argIdx++
		setClauses = append(setClauses, fmt.Sprintf("timezone = $%d", argIdx))
		args = append(args, req.Timezone)
	}
	if req.Enabled != nil {
		argIdx++
		setClauses = append(setClauses, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *req.Enabled)
	}

	argIdx++
	args = append(args, id)
	query := fmt.Sprintf("UPDATE scheduled_jobs SET %s WHERE id = $%d", strings.Join(setClauses, ", "), argIdx)
	_, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to update scheduled job: %w", err)
	}
	return s.GetScheduledJob(ctx, id)
}

func (s *SupabaseDB) DeleteScheduledJob(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM scheduled_jobs WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("failed to delete scheduled job: %w", err)
	}
	return nil
}

func (s *SupabaseDB) ListScheduledJobs(ctx context.Context, limit, offset int, entityType *string, enabled *bool) ([]ScheduledJob, int, error) {
	whereClauses := []string{}
	args := []interface{}{}
	argIdx := 0

	if entityType != nil {
		argIdx++
		whereClauses = append(whereClauses, fmt.Sprintf("entity_type = $%d", argIdx))
		args = append(args, *entityType)
	}
	if enabled != nil {
		argIdx++
		whereClauses = append(whereClauses, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *enabled)
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

	argIdx++
	limitArg := argIdx
	argIdx++
	offsetArg := argIdx
	listArgs := append(args, limit, offset)
	query := fmt.Sprintf(`SELECT id, name, description, entity_type, preset_query_id, trigger_payload,
		group_ids, cron_expression, timezone, enabled, last_run_at, next_run_at,
		last_session_id, last_status, last_error, last_duration_ms, run_count, consecutive_failures, created_at, updated_at
		FROM scheduled_jobs %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, limitArg, offsetArg)

	rows, err := s.db.QueryContext(ctx, query, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list scheduled jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]ScheduledJob, 0)
	for rows.Next() {
		job, err := scanScheduledJobPG(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan scheduled job: %w", err)
		}
		jobs = append(jobs, *job)
	}
	return jobs, total, nil
}

func (s *SupabaseDB) UpdateScheduledJobRunStatus(ctx context.Context, id string, lastRunAt time.Time, nextRunAt *time.Time, sessionID, status, errMsg string, durationMs *int64) error {
	query := `UPDATE scheduled_jobs SET
		last_run_at = $1,
		next_run_at = $2,
		last_session_id = $3,
		last_status = $4,
		last_error = $5,
		last_duration_ms = $6,
		run_count = CASE WHEN $7 = 'running' THEN run_count ELSE run_count + 1 END,
		consecutive_failures = CASE WHEN $8 = 'error' THEN consecutive_failures + 1 WHEN $9 = 'running' THEN consecutive_failures ELSE 0 END,
		updated_at = NOW()
		WHERE id = $10`
	_, err := s.db.ExecContext(ctx, query, lastRunAt, nextRunAt, sessionID, status, errMsg, durationMs, status, status, status, id)
	if err != nil {
		return fmt.Errorf("failed to update scheduled job run status: %w", err)
	}
	return nil
}

func (s *SupabaseDB) CreateScheduledJobRun(ctx context.Context, run *ScheduledJobRun) error {
	groupIDsJSON := "[]"
	if len(run.GroupIDs) > 0 {
		b, _ := json.Marshal(run.GroupIDs)
		groupIDsJSON = string(b)
	}
	query := `INSERT INTO scheduled_job_runs (id, job_id, run_folder, session_id, status, error, duration_ms, group_ids, started_at, completed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	_, err := s.db.ExecContext(ctx, query, run.ID, run.JobID, run.RunFolder, run.SessionID, run.Status, run.Error, run.DurationMs, groupIDsJSON, run.StartedAt, run.CompletedAt)
	if err != nil {
		return fmt.Errorf("failed to create scheduled job run: %w", err)
	}
	return nil
}

func (s *SupabaseDB) UpdateScheduledJobRun(ctx context.Context, id string, status string, errMsg string, durationMs *int64, runFolder string, sessionID string) error {
	query := `UPDATE scheduled_job_runs SET status = $1, error = $2, duration_ms = $3, run_folder = $4, session_id = $5, completed_at = NOW() WHERE id = $6`
	_, err := s.db.ExecContext(ctx, query, status, errMsg, durationMs, runFolder, sessionID, id)
	if err != nil {
		return fmt.Errorf("failed to update scheduled job run: %w", err)
	}
	return nil
}

func (s *SupabaseDB) ListScheduledJobRuns(ctx context.Context, jobID string, limit, offset int) ([]ScheduledJobRun, int, error) {
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM scheduled_job_runs WHERE job_id = $1", jobID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count scheduled job runs: %w", err)
	}

	query := `SELECT id, job_id, run_folder, session_id, status, error, duration_ms, group_ids, started_at, completed_at
		FROM scheduled_job_runs WHERE job_id = $1 ORDER BY started_at DESC LIMIT $2 OFFSET $3`
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
