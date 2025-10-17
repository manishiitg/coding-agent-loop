package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent/agent_go/pkg/events"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteDB implements the Database interface using SQLite
type SQLiteDB struct {
	db *sql.DB
}

// NewSQLiteDB creates a new SQLite database connection
func NewSQLiteDB(dbPath string) (*SQLiteDB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Test the connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Run migrations (includes initial schema creation)
	migrationRunner := NewMigrationRunner(db)
	if err := migrationRunner.RunMigrations("pkg/database/migrations"); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &SQLiteDB{db: db}, nil
}

// CreateChatSession creates a new chat session
func (s *SQLiteDB) CreateChatSession(ctx context.Context, req *CreateChatSessionRequest) (*ChatSession, error) {
	query := `
		INSERT INTO chat_sessions (session_id, title, agent_mode, preset_query_id, status)
		VALUES (?, ?, ?, ?, ?)
		RETURNING id, session_id, title, agent_mode, preset_query_id, created_at, completed_at, status
	`

	// Handle empty preset_query_id by converting to NULL
	var presetQueryID interface{}
	if req.PresetQueryID == "" {
		presetQueryID = nil
	} else {
		presetQueryID = req.PresetQueryID
	}

	var session ChatSession
	var agentModeStr *string
	var presetQueryIDStr *string
	err := s.db.QueryRowContext(ctx, query, req.SessionID, req.Title, req.AgentMode, presetQueryID, "active").Scan(
		&session.ID, &session.SessionID, &session.Title, &agentModeStr, &presetQueryIDStr, &session.CreatedAt, &session.CompletedAt, &session.Status,
	)
	if err != nil {
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

	return &session, nil
}

// GetChatSession retrieves a chat session by session ID
func (s *SQLiteDB) GetChatSession(ctx context.Context, sessionID string) (*ChatSession, error) {
	query := `
		SELECT id, session_id, title, agent_mode, preset_query_id, created_at, completed_at, status
		FROM chat_sessions
		WHERE session_id = ?
	`

	var session ChatSession
	var agentModeStr *string
	var presetQueryIDStr *string
	err := s.db.QueryRowContext(ctx, query, sessionID).Scan(
		&session.ID, &session.SessionID, &session.Title, &agentModeStr, &presetQueryIDStr, &session.CreatedAt, &session.CompletedAt, &session.Status,
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

	return &session, nil
}

// UpdateChatSession updates a chat session
func (s *SQLiteDB) UpdateChatSession(ctx context.Context, sessionID string, req *UpdateChatSessionRequest) (*ChatSession, error) {
	query := `
		UPDATE chat_sessions
		SET title = COALESCE(?, title),
		    agent_mode = COALESCE(?, agent_mode),
		    preset_query_id = COALESCE(?, preset_query_id),
		    status = COALESCE(?, status),
		    completed_at = COALESCE(?, completed_at)
		WHERE session_id = ?
		RETURNING id, session_id, title, agent_mode, preset_query_id, created_at, completed_at, status
	`

	var session ChatSession
	var agentModeStr *string
	var presetQueryIDStr *string
	err := s.db.QueryRowContext(ctx, query, req.Title, req.AgentMode, req.PresetQueryID, req.Status, req.CompletedAt, sessionID).Scan(
		&session.ID, &session.SessionID, &session.Title, &agentModeStr, &presetQueryIDStr, &session.CreatedAt, &session.CompletedAt, &session.Status,
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

// ListChatSessions lists chat sessions with pagination
func (s *SQLiteDB) ListChatSessions(ctx context.Context, limit, offset int, presetQueryID *string) ([]ChatHistorySummary, int, error) {
	// Build WHERE clause for filtering
	var whereClause string
	var args []interface{}

	if presetQueryID != nil && *presetQueryID != "" {
		whereClause = " WHERE cs.preset_query_id = ?"
		args = append(args, *presetQueryID)
	}

	// Get total count
	countQuery := `SELECT COUNT(*) FROM chat_sessions cs` + whereClause
	var total int
	err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get total count: %w", err)
	}

	// Get sessions with summary data
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
			COUNT(e.id) as total_events,
			0 as total_turns,
			CASE 
				WHEN MAX(e.timestamp) IS NOT NULL THEN MAX(e.timestamp)
				ELSE NULL
			END as last_activity
		FROM chat_sessions cs
		LEFT JOIN events e ON cs.id = e.chat_session_id` + whereClause + `
		GROUP BY cs.id, cs.session_id, cs.title, cs.agent_mode, cs.status, cs.created_at, cs.completed_at, cs.preset_query_id
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
		err := rows.Scan(
			&session.ChatSessionID, &session.SessionID, &session.Title, &agentModeStr, &session.Status,
			&session.CreatedAt, &session.CompletedAt, &presetQueryIDStr, &session.TotalEvents, &session.TotalTurns, &lastActivityStr,
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

		// Parse lastActivity string to time.Time
		if lastActivityStr != nil {
			if lastActivity, err := time.Parse("2006-01-02 15:04:05.999999999-07:00", *lastActivityStr); err == nil {
				session.LastActivity = &lastActivity
			} else {
				// Fallback to CreatedAt if parsing fails
				session.LastActivity = &session.CreatedAt
			}
		} else {
			// Use CreatedAt as fallback if no last activity
			session.LastActivity = &session.CreatedAt
		}
		sessions = append(sessions, session)
	}

	return sessions, total, nil
}

// StoreEvent stores an event in the database
func (s *SQLiteDB) StoreEvent(ctx context.Context, sessionID string, event *events.AgentEvent) error {
	fmt.Printf("[DATABASE DEBUG] SQLiteDB.StoreEvent called - EventType: %s, SessionID: %s\n", event.Type, sessionID)

	// Get chat session ID
	chatSession, err := s.GetChatSession(ctx, sessionID)
	if err != nil {
		fmt.Printf("[DATABASE DEBUG] Failed to get chat session for %s: %v\n", sessionID, err)
		return fmt.Errorf("failed to get chat session: %w", err)
	}
	fmt.Printf("[DATABASE DEBUG] Found chat session ID: %s for session: %s\n", chatSession.ID, sessionID)

	// Convert event to JSON
	eventData, err := json.Marshal(event)
	if err != nil {
		fmt.Printf("[DATABASE DEBUG] Failed to marshal event data: %v\n", err)
		return fmt.Errorf("failed to marshal event data: %w", err)
	}
	fmt.Printf("[DATABASE DEBUG] Event data marshaled successfully, size: %d bytes\n", len(eventData))

	query := `
		INSERT INTO events (session_id, chat_session_id, event_type, timestamp, event_data)
		VALUES (?, ?, ?, ?, ?)
	`

	_, err = s.db.ExecContext(ctx, query, sessionID, chatSession.ID, event.Type, event.Timestamp, string(eventData))
	if err != nil {
		fmt.Printf("[DATABASE DEBUG] Failed to store event in database: %v\n", err)
		return fmt.Errorf("failed to store event: %w", err)
	}

	fmt.Printf("[DATABASE DEBUG] Successfully stored event %s in database for session %s\n", event.Type, sessionID)
	return nil
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

	// Get total count
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

// GetEventsBySession retrieves events for a specific session
func (s *SQLiteDB) GetEventsBySession(ctx context.Context, sessionID string, limit, offset int) ([]Event, error) {
	query := `
		SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
		FROM events
		WHERE session_id = ?
		ORDER BY timestamp ASC
		LIMIT ? OFFSET ?
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
		agentMode = AgentModeReAct // Use constant for default
	}

	query := `
		INSERT INTO preset_queries (label, query, selected_servers, selected_folder, agent_mode, llm_config, is_predefined, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		RETURNING id, label, query, selected_servers, selected_folder, agent_mode, llm_config, is_predefined, created_at, updated_at, created_by
	`

	var preset PresetQuery
	var selectedServersStr string
	var llmConfigNullStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, req.Label, req.Query, selectedServersJSON, req.SelectedFolder, agentMode, llmConfigParam, req.IsPredefined, "user").Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &preset.SelectedFolder, &preset.AgentMode, &llmConfigNullStr, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create preset query: %w", err)
	}

	// Parse selected servers JSON
	preset.SelectedServers = selectedServersStr
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = json.RawMessage("null")
	}

	return &preset, nil
}

// GetPresetQuery retrieves a preset query by ID
func (s *SQLiteDB) GetPresetQuery(ctx context.Context, id string) (*PresetQuery, error) {
	query := `
		SELECT id, label, query, selected_servers, selected_folder, agent_mode, llm_config, is_predefined, created_at, updated_at, created_by
		FROM preset_queries
		WHERE id = ?
	`

	var preset PresetQuery
	var selectedServersStr string
	var llmConfigNullStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &preset.SelectedFolder, &preset.AgentMode, &llmConfigNullStr, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("preset query not found")
		}
		return nil, fmt.Errorf("failed to get preset query: %w", err)
	}

	preset.SelectedServers = selectedServersStr
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = nil
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
	argIndex := 1

	if req.Label != "" {
		updateFields = append(updateFields, "label = ?")
		args = append(args, req.Label)
		argIndex++
	}

	if req.Query != "" {
		updateFields = append(updateFields, "query = ?")
		args = append(args, req.Query)
		argIndex++
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
		argIndex++
	}

	if req.SelectedFolder != "" {
		updateFields = append(updateFields, "selected_folder = ?")
		args = append(args, req.SelectedFolder)
		argIndex++
	}

	if req.AgentMode != "" {
		updateFields = append(updateFields, "agent_mode = ?")
		args = append(args, req.AgentMode)
		argIndex++
	}

	if req.LLMConfig != nil {
		llmConfigBytes, err := json.Marshal(req.LLMConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal LLM config: %w", err)
		}
		updateFields = append(updateFields, "llm_config = ?")
		args = append(args, string(llmConfigBytes))
		argIndex++
	}

	if len(updateFields) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	updateFields = append(updateFields, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, id)

	query := fmt.Sprintf(`
		UPDATE preset_queries
		SET %s
		WHERE id = ?
		RETURNING id, label, query, selected_servers, selected_folder, agent_mode, llm_config, is_predefined, created_at, updated_at, created_by
	`, strings.Join(updateFields, ", "))

	var preset PresetQuery
	var selectedServersStr string
	var llmConfigNullStr sql.NullString
	err := s.db.QueryRowContext(ctx, query, args...).Scan(
		&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &preset.SelectedFolder, &preset.AgentMode, &llmConfigNullStr, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("preset query not found")
		}
		return nil, fmt.Errorf("failed to update preset query: %w", err)
	}

	preset.SelectedServers = selectedServersStr
	if llmConfigNullStr.Valid {
		preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
	} else {
		preset.LLMConfig = nil
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
		SELECT id, label, query, selected_servers, selected_folder, agent_mode, llm_config, is_predefined, created_at, updated_at, created_by
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
		var selectedFolderStr sql.NullString
		var llmConfigNullStr sql.NullString
		err := rows.Scan(
			&preset.ID, &preset.Label, &preset.Query, &selectedServersStr, &selectedFolderStr, &preset.AgentMode, &llmConfigNullStr, &preset.IsPredefined, &preset.CreatedAt, &preset.UpdatedAt, &preset.CreatedBy,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan preset query: %w", err)
		}

		preset.SelectedServers = selectedServersStr
		if llmConfigNullStr.Valid {
			preset.LLMConfig = json.RawMessage(llmConfigNullStr.String)
		} else {
			preset.LLMConfig = json.RawMessage("null")
		}
		if selectedFolderStr.Valid {
			preset.SelectedFolder = selectedFolderStr.String
		} else {
			preset.SelectedFolder = ""
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
		workflowStatus := "pre-verification"
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

	updateFields = append(updateFields, "updated_at = CURRENT_TIMESTAMP")
	args = append(args, presetQueryID)

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

// Close closes the database connection
func (s *SQLiteDB) Close() error {
	return s.db.Close()
}
