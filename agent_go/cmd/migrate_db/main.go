package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"
	"unicode/utf8"

	"mcp-agent-builder-go/agent_go/pkg/database"

	_ "github.com/lib/pq"
	_ "github.com/mattn/go-sqlite3"
)

// Data structures matching database schema
type PresetQuery struct {
	ID                   string          `json:"id"`
	Label                string          `json:"label"`
	Query                string          `json:"query"`
	SelectedServers      string          `json:"selected_servers"`
	SelectedTools        string          `json:"selected_tools"`
	SelectedFolder       sql.NullString  `json:"selected_folder"`
	AgentMode            string          `json:"agent_mode"`
	LLMConfig            sql.NullString  `json:"llm_config"` // Use sql.NullString for raw JSON access
	UseCodeExecutionMode interface{}     `json:"use_code_execution_mode"` // Can be int (sqlite) or bool (postgres)
	IsPredefined         interface{}     `json:"is_predefined"`           // Can be int (sqlite) or bool (postgres)
	CreatedAt            time.Time       `json:"created_at"`
	UpdatedAt            time.Time       `json:"updated_at"`
	CreatedBy            string          `json:"created_by"`
}

type ChatSession struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"session_id"`
	Title         string          `json:"title"`
	AgentMode     sql.NullString  `json:"agent_mode"`
	PresetQueryID sql.NullString  `json:"preset_query_id"`
	Config        sql.NullString  `json:"config"`
	CreatedAt     time.Time       `json:"created_at"`
	CompletedAt   sql.NullTime    `json:"completed_at"`
	Status        string          `json:"status"`
}

type Event struct {
	ID            string          `json:"id"`
	SessionID     string          `json:"session_id"`
	ChatSessionID sql.NullString  `json:"chat_session_id"`
	EventType     string          `json:"event_type"`
	Timestamp     time.Time       `json:"timestamp"`
	EventData     string          `json:"event_data"`
}

type Workflow struct {
	ID              string          `json:"id"`
	PresetQueryID   string          `json:"preset_query_id"`
	WorkflowStatus  string          `json:"workflow_status"`
	SelectedOptions sql.NullString  `json:"selected_options"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func main() {
	sqlitePath := flag.String("sqlite", "chat_history.db", "Path to SQLite database")
	postgresURL := flag.String("postgres", "", "PostgreSQL connection URL")
	flag.Parse()

	if *postgresURL == "" {
		*postgresURL = os.Getenv("DATABASE_URL")
		if *postgresURL == "" {
			log.Fatal("PostgreSQL URL is required. Use -postgres flag or DATABASE_URL env var.")
		}
	}

	log.Println("🔌 Connecting to SQLite...")
	sqliteDB, err := sql.Open("sqlite3", *sqlitePath)
	if err != nil {
		log.Fatalf("Failed to open SQLite DB: %v", err)
	}
	defer sqliteDB.Close()

	if err := sqliteDB.Ping(); err != nil {
		log.Fatalf("Failed to ping SQLite DB: %v", err)
	}

	log.Println("🔌 Connecting to PostgreSQL...")
	pgDB, err := sql.Open("postgres", *postgresURL)
	if err != nil {
		log.Fatalf("Failed to open PostgreSQL DB: %v", err)
	}
	defer pgDB.Close()

	if err := pgDB.Ping(); err != nil {
		log.Fatalf("Failed to ping PostgreSQL DB: %v", err)
	}

	// Run migrations on Postgres first
	log.Println("🔄 Running migrations on PostgreSQL...")
	migrationRunner := database.NewMigrationRunner(pgDB, "postgres")
	if err := migrationRunner.RunMigrations("pkg/database/migrations_postgres"); err != nil {
		log.Fatalf("Failed to run migrations on PostgreSQL: %v", err)
	}

	// Migrate Preset Queries
	if err := migratePresetQueries(sqliteDB, pgDB); err != nil {
		log.Fatalf("Failed to migrate preset queries: %v", err)
	}

	// Migrate Chat Sessions
	if err := migrateChatSessions(sqliteDB, pgDB); err != nil {
		log.Fatalf("Failed to migrate chat sessions: %v", err)
	}

	// Migrate Events
	if err := migrateEvents(sqliteDB, pgDB); err != nil {
		log.Fatalf("Failed to migrate events: %v", err)
	}

	// Migrate Workflows
	if err := migrateWorkflows(sqliteDB, pgDB); err != nil {
		log.Fatalf("Failed to migrate workflows: %v", err)
	}
	
	// Migrate Slack Config (Optional tables)
	if err := migrateSlackConfig(sqliteDB, pgDB); err != nil {
		log.Printf("Warning: Failed to migrate slack config (might not exist): %v", err)
	}

	log.Println("✅ Migration completed successfully!")
}

func migratePresetQueries(src, dst *sql.DB) error {
	log.Println("📦 Migrating Preset Queries...")
	
	// Count records
	var count int
	if err := src.QueryRow("SELECT COUNT(*) FROM preset_queries").Scan(&count); err != nil {
		return fmt.Errorf("failed to count preset queries: %w", err)
	}
	log.Printf("Found %d preset queries to migrate", count)

	rows, err := src.Query(`
		SELECT id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, is_predefined, created_at, updated_at, created_by
		FROM preset_queries
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	tx, err := dst.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO preset_queries (id, label, query, selected_servers, selected_tools, selected_folder, agent_mode, llm_config, use_code_execution_mode, is_predefined, created_at, updated_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET
			label = EXCLUDED.label,
			query = EXCLUDED.query,
			selected_servers = EXCLUDED.selected_servers,
			selected_tools = EXCLUDED.selected_tools,
			selected_folder = EXCLUDED.selected_folder,
			agent_mode = EXCLUDED.agent_mode,
			llm_config = EXCLUDED.llm_config,
			use_code_execution_mode = EXCLUDED.use_code_execution_mode,
			is_predefined = EXCLUDED.is_predefined,
			updated_at = EXCLUDED.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for rows.Next() {
		var p PresetQuery
		var useCodeExec interface{}
		var isPredefined interface{}
		
		err := rows.Scan(
			&p.ID, &p.Label, &p.Query, &p.SelectedServers, &p.SelectedTools, &p.SelectedFolder,
			&p.AgentMode, &p.LLMConfig, &useCodeExec, &isPredefined,
			&p.CreatedAt, &p.UpdatedAt, &p.CreatedBy,
		)
		if err != nil {
			return err
		}

		// Convert SQLite INTEGER (0/1) to Boolean if necessary
		p.UseCodeExecutionMode = intToBool(useCodeExec)
		p.IsPredefined = intToBool(isPredefined)

		// Handle NULL llm_config
		var llmConfig interface{}
		if p.LLMConfig.Valid {
			llmConfig = p.LLMConfig.String
		} else {
			llmConfig = nil
		}

		var selectedFolder interface{}
		if p.SelectedFolder.Valid {
			selectedFolder = p.SelectedFolder.String
		} else {
			selectedFolder = nil
		}

		_, err = stmt.Exec(
			p.ID, cleanString(p.Label), cleanString(p.Query), p.SelectedServers, p.SelectedTools, selectedFolder,
			p.AgentMode, llmConfig, p.UseCodeExecutionMode, p.IsPredefined,
			p.CreatedAt, p.UpdatedAt, p.CreatedBy,
		)
		if err != nil {
			return fmt.Errorf("failed to insert preset %s: %w", p.ID, err)
		}
	}

	return tx.Commit()
}

func migrateChatSessions(src, dst *sql.DB) error {
	log.Println("📦 Migrating Chat Sessions (Last 1 Day)...")

	// Calculate cutoff date (1 day ago)
	cutoffDate := time.Now().AddDate(0, 0, -1).Format("2006-01-02 15:04:05")
	log.Printf("Filtering sessions created after: %s", cutoffDate)

	// Count records to migrate
	var count int
	if err := src.QueryRow("SELECT COUNT(*) FROM chat_sessions WHERE created_at >= ?", cutoffDate).Scan(&count); err != nil {
		return fmt.Errorf("failed to count chat sessions: %w", err)
	}
	log.Printf("Found %d chat sessions to migrate", count)

	rows, err := src.Query(`
		SELECT id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status
		FROM chat_sessions
		WHERE created_at >= ?
	`, cutoffDate)
	if err != nil {
		return err
	}
	defer rows.Close()

	tx, err := dst.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO chat_sessions (id, session_id, title, agent_mode, preset_query_id, config, created_at, completed_at, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for rows.Next() {
		var s ChatSession
		err := rows.Scan(
			&s.ID, &s.SessionID, &s.Title, &s.AgentMode, &s.PresetQueryID, &s.Config,
			&s.CreatedAt, &s.CompletedAt, &s.Status,
		)
		if err != nil {
			return err
		}

		// Check if referenced preset exists
		if s.PresetQueryID.Valid {
			var exists bool
			err := dst.QueryRow("SELECT EXISTS(SELECT 1 FROM preset_queries WHERE id = $1)", s.PresetQueryID.String).Scan(&exists)
			if err != nil {
				return err
			}
			if !exists {
				log.Printf("⚠️  Skipping preset_query_id %s for session %s (not found in destination)", s.PresetQueryID.String, s.SessionID)
				s.PresetQueryID.Valid = false
			}
		}

		var agentMode interface{}
		if s.AgentMode.Valid {
			agentMode = s.AgentMode.String
		} else {
			agentMode = nil
		}

		var presetQueryID interface{}
		if s.PresetQueryID.Valid {
			presetQueryID = s.PresetQueryID.String
		} else {
			presetQueryID = nil
		}

		var config interface{}
		if s.Config.Valid {
			config = s.Config.String
		} else {
			config = nil
		}

		var completedAt interface{}
		if s.CompletedAt.Valid {
			completedAt = s.CompletedAt.Time
		} else {
			completedAt = nil
		}

		_, err = stmt.Exec(
			s.ID, s.SessionID, cleanString(s.Title), agentMode, presetQueryID, config,
			s.CreatedAt, completedAt, s.Status,
		)
		if err != nil {
			return fmt.Errorf("failed to insert session %s: %w", s.SessionID, err)
		}
	}

	return tx.Commit()
}

func migrateEvents(src, dst *sql.DB) error {
	log.Println("📦 Migrating Events (Last 1 Day)...")

	// Calculate cutoff date (1 day ago)
	cutoffDate := time.Now().AddDate(0, 0, -1).Format("2006-01-02 15:04:05")
	log.Printf("Filtering events created after: %s", cutoffDate)

	// Pre-fetch all valid chat_session_ids from destination to avoid per-row checks
	log.Println("🔍 Pre-fetching valid chat session IDs from destination...")
	validSessions := make(map[string]bool)
	sessionMap := make(map[string]string) // session_id -> id
	
	rowsSess, err := dst.Query("SELECT id, session_id FROM chat_sessions")
	if err != nil {
		return fmt.Errorf("failed to fetch sessions from destination: %w", err)
	}
	for rowsSess.Next() {
		var id, sid string
		if err := rowsSess.Scan(&id, &sid); err != nil {
			rowsSess.Close()
			return err
		}
		validSessions[id] = true
		sessionMap[sid] = id
	}
	rowsSess.Close()
	log.Printf("Loaded %d sessions for validation", len(validSessions))

	// Count records
	var count int
	if err := src.QueryRow("SELECT COUNT(*) FROM events WHERE timestamp >= ?", cutoffDate).Scan(&count); err != nil {
		return fmt.Errorf("failed to count events: %w", err)
	}
	log.Printf("Found %d events to migrate", count)
	
	// Process in batches
	batchSize := 500 // Reduced batch size for more frequent logging
	offset := 0

	for offset < count {
		log.Printf("Processing events %d to %d...", offset, offset+batchSize)
		
		rows, err := src.Query(`
			SELECT id, session_id, chat_session_id, event_type, timestamp, event_data
			FROM events
			WHERE timestamp >= ?
			LIMIT ? OFFSET ?
		`, cutoffDate, batchSize, offset)
		if err != nil {
			return err
		}
		
		var batchEvents []Event
		for rows.Next() {
			var e Event
			err := rows.Scan(
				&e.ID, &e.SessionID, &e.ChatSessionID, &e.EventType, &e.Timestamp, &e.EventData,
			)
			if err != nil {
				rows.Close()
				return err
			}

			// Use memory maps instead of DB queries for validation
			if e.ChatSessionID.Valid {
				if !validSessions[e.ChatSessionID.String] {
					if id, found := sessionMap[e.SessionID]; found {
						e.ChatSessionID.String = id
						e.ChatSessionID.Valid = true
					} else {
						e.ChatSessionID.Valid = false
					}
				}
			} else {
				if id, found := sessionMap[e.SessionID]; found {
					e.ChatSessionID.String = id
					e.ChatSessionID.Valid = true
				}
			}
			batchEvents = append(batchEvents, e)
		}
		rows.Close()

		if len(batchEvents) == 0 {
			break
		}

		// Build multi-row INSERT
		query := "INSERT INTO events (id, session_id, chat_session_id, event_type, timestamp, event_data) VALUES "
		vals := []interface{}{}
		for i, e := range batchEvents {
			p := i * 6
			query += fmt.Sprintf("($%d, $%d, $%d, $%d, $%d, $%d),", p+1, p+2, p+3, p+4, p+5, p+6)
			
			var chatSessionID interface{}
			if e.ChatSessionID.Valid {
				chatSessionID = e.ChatSessionID.String
			} else {
				chatSessionID = nil
			}
			vals = append(vals, e.ID, e.SessionID, chatSessionID, e.EventType, e.Timestamp, cleanString(e.EventData))
		}
		query = query[0:len(query)-1] // Remove trailing comma
		query += " ON CONFLICT (id) DO NOTHING"

		_, err = dst.Exec(query, vals...)
		if err != nil {
			return fmt.Errorf("failed to insert batch: %w", err)
		}
		
		offset += batchSize
	}

	return nil
}

func migrateWorkflows(src, dst *sql.DB) error {
	log.Println("📦 Migrating Workflows...")
	
	rows, err := src.Query(`
		SELECT id, preset_query_id, workflow_status, selected_options, created_at, updated_at
		FROM workflows
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	tx, err := dst.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO workflows (id, preset_query_id, workflow_status, selected_options, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for rows.Next() {
		var w Workflow
		err := rows.Scan(
			&w.ID, &w.PresetQueryID, &w.WorkflowStatus, &w.SelectedOptions,
			&w.CreatedAt, &w.UpdatedAt,
		)
		if err != nil {
			return err
		}

		var exists bool
		err = dst.QueryRow("SELECT EXISTS(SELECT 1 FROM preset_queries WHERE id = $1)", w.PresetQueryID).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			log.Printf("⚠️  Skipping workflow %s (preset %s not found)", w.ID, w.PresetQueryID)
			continue
		}

		var selectedOptions interface{}
		if w.SelectedOptions.Valid {
			selectedOptions = w.SelectedOptions.String
		} else {
			selectedOptions = nil
		}

		_, err = stmt.Exec(
			w.ID, w.PresetQueryID, w.WorkflowStatus, selectedOptions,
			w.CreatedAt, w.UpdatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert workflow %s: %w", w.ID, err)
		}
	}

	return tx.Commit()
}

func migrateSlackConfig(src, dst *sql.DB) error {
	log.Println("📦 Migrating Slack Configuration...")

	var exists bool
	src.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='slack_feedback_config'").Scan(&exists)
	if !exists {
		log.Println("Skipping slack_feedback_config (table not found in SQLite)")
		return nil
	}

	rows, err := src.Query(`SELECT id, enabled, bot_token, app_token, channel_id, created_at, updated_at FROM slack_feedback_config`)
	if err != nil {
		return err
	}
	defer rows.Close()

	tx, err := dst.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO slack_feedback_config (id, enabled, bot_token, app_token, channel_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			bot_token = EXCLUDED.bot_token,
			app_token = EXCLUDED.app_token,
			channel_id = EXCLUDED.channel_id,
			updated_at = EXCLUDED.updated_at
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for rows.Next() {
		var id, botToken, appToken, channelId string
		var enabledRaw interface{}
		var createdAt, updatedAt time.Time

		err := rows.Scan(&id, &enabledRaw, &botToken, &appToken, &channelId, &createdAt, &updatedAt)
		if err != nil {
			return err
		}

		enabled := intToBool(enabledRaw)

		_, err = stmt.Exec(id, enabled, botToken, appToken, channelId, createdAt, updatedAt)
		if err != nil {
			return fmt.Errorf("failed to insert slack config: %w", err)
		}
	}
	
	src.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='slack_feedback_messages'").Scan(&exists)
	if !exists {
		log.Println("Skipping slack_feedback_messages (table not found in SQLite)")
		return tx.Commit()
	}

	msgRows, err := src.Query(`SELECT id, unique_id, slack_message_ts, slack_channel_id, slack_thread_ts, created_at FROM slack_feedback_messages`)
	if err != nil {
		return err
	}
	defer msgRows.Close()

	msgStmt, err := tx.Prepare(`
		INSERT INTO slack_feedback_messages (id, unique_id, slack_message_ts, slack_channel_id, slack_thread_ts, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer msgStmt.Close()

	for msgRows.Next() {
		var id, uniqueId, msgTs, chanId string
		var threadTs sql.NullString
		var createdAt time.Time

		err := msgRows.Scan(&id, &uniqueId, &msgTs, &chanId, &threadTs, &createdAt)
		if err != nil {
			return err
		}

		var threadTsVal interface{}
		if threadTs.Valid {
			threadTsVal = threadTs.String
		} else {
			threadTsVal = nil
		}

		_, err = msgStmt.Exec(id, uniqueId, msgTs, chanId, threadTsVal, createdAt)
		if err != nil {
			return fmt.Errorf("failed to insert slack message: %w", err)
		}
	}

	return tx.Commit()
}

// Helper to clean invalid UTF-8 sequences
func cleanString(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	v := make([]rune, 0, len(s))
	for i, r := range s {
		if r == utf8.RuneError {
			_, size := utf8.DecodeRuneInString(s[i:])
			if size == 1 {
				continue
			}
		}
		v = append(v, r)
	}
	return string(v)
}

func intToBool(v interface{}) bool {
	switch val := v.(type) {
	case int64:
		return val != 0
	case int:
		return val != 0
	case float64:
		return val != 0
	case []uint8:
		return string(val) == "1" || string(val) == "true"
	case string:
		return val == "1" || val == "true"
	case bool:
		return val
	default:
		return false
	}
}