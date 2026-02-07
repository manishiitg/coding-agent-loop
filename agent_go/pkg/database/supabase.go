package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/mcpagent/events"

	_ "github.com/lib/pq"
)

// isLLMConfigLocked checks if LLM configuration is locked via environment variable
// When locked, the backend uses env vars for LLM config instead of stored session config
func isLLMConfigLocked() bool {
	return os.Getenv("LLM_CONFIG_LOCKED") == "true" || os.Getenv("LLM_CONFIG_LOCKED") == "1"
}

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
	query := `
		SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
		FROM events
		WHERE chat_session_id = $1
		ORDER BY timestamp ASC
		LIMIT $2 OFFSET $3
	`

	rows, err := s.db.QueryContext(ctx, query, sessionID, limit, offset)
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

	query := `
		INSERT INTO preset_queries (label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
	`

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
		req.EnableBrowserAccess,
		req.IsPredefined,
		"user",
	).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create preset query: %w", err)
	}

	preset.SelectedServers = selectedServersStr
	preset.SelectedTools = selectedToolsStr
	preset.SelectedFolder = selectedFolderStr
	preset.PreDiscoveredTools = preDiscoveredToolsStr.String
	preset.SelectedSkills = selectedSkillsStr.String
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
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
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

	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
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
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
	`, strings.Join(updateFields, ", "), argCount)

	var preset PresetQuery
	var selectedServersStr string
	var selectedToolsStr string
	var selectedFolderStr sql.NullString
	var llmConfigNullStr sql.NullString
	var preDiscoveredToolsStr sql.NullString
	var selectedSkillsStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
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
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
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

			err := rows.Scan(

				&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,

			)

			if err != nil {

				return nil, 0, fmt.Errorf("failed to scan preset query: %w", err)

			}

	

			preset.SelectedServers = selectedServersStr

			preset.SelectedTools = selectedToolsStr

			preset.PreDiscoveredTools = preDiscoveredToolsStr.String

			preset.SelectedSkills = selectedSkillsStr.String

	

			if llmConfigNullStr.Valid {

				preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)

			} else {

				preset.LLMConfig = json.RawMessage("null")

			}

	

			preset.SelectedFolder = selectedFolderStr

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

		// Filter out sessions without valid LLM config (old sessions before LLM config was stored)
		// Skip this filter when LLM_CONFIG_LOCKED=true because backend uses env vars for LLM config
		if !isLLMConfigLocked() && !hasValidLLMConfig(session.Config) {
			continue
		}

		sessions = append(sessions, session)
	}

	// Adjust total count to exclude filtered sessions
	// Note: This is approximate as we filter in-memory; for exact count would need SQL filtering
	validTotal := total
	if len(sessions) < limit {
		validTotal = offset + len(sessions)
	}

	return sessions, validTotal, nil
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

	var userIDValue interface{}
	if userID == "" {
		userIDValue = nil
	} else {
		userIDValue = userID
	}

	query := `
		INSERT INTO preset_queries (label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_by, user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
	`

	var preset PresetQuery
	var selectedServersStr string
	var selectedToolsStr string
	var selectedFolderStr sql.NullString
	var llmConfigNullStr sql.NullString
	var preDiscoveredToolsStr sql.NullString
	var selectedSkillsStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, req.Label, req.Query, selectedServersJSON, selectedToolsJSON, req.SelectedFolder, agentMode, llmConfigParam, req.UseCodeExecutionMode, req.UseToolSearchMode, preDiscoveredToolsJSON, selectedSkillsJSON, req.EnableBrowserAccess, req.IsPredefined, "user", userIDValue).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create preset query: %w", err)
	}

	preset.SelectedServers = selectedServersStr
	preset.SelectedTools = selectedToolsStr
	preset.SelectedFolder = selectedFolderStr
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
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, use_tool_search_mode, pre_discovered_tools, selected_skills, enable_browser_access, is_predefined, created_at, updated_at, created_by
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
		err := rows.Scan(
			&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedToolsStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.UseCodeExecutionMode, &preset.UseToolSearchMode, &preDiscoveredToolsStr, &selectedSkillsStr, &preset.EnableBrowserAccess, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan preset query: %w", err)
		}

		preset.SelectedServers = selectedServersStr
		preset.SelectedTools = selectedToolsStr
		preset.SelectedFolder = selectedFolderStr
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
