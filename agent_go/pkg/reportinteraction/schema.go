package reportinteraction

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// OpenDatabase opens the workflow database and installs the framework-owned
// report interaction tables. Callers own the returned handle.
func OpenDatabase(ctx context.Context, dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := EnsureSchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// EnsureSchema is deliberately additive so existing workflow databases can be
// upgraded without rebuilding user-owned tables.
func EnsureSchema(ctx context.Context, db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS report_widget_responses (
			workspace_path TEXT NOT NULL,
			widget_id TEXT NOT NULL,
			instance_key TEXT NOT NULL DEFAULT 'default',
			question TEXT NOT NULL,
			response_kind TEXT NOT NULL,
			options_json TEXT NOT NULL DEFAULT '[]',
			allow_free_text INTEGER NOT NULL DEFAULT 0,
			subject_id TEXT NOT NULL DEFAULT '',
			subject_version TEXT NOT NULL DEFAULT '',
			subject_hash TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			selected_option_id TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			answered_by TEXT NOT NULL DEFAULT '',
			consumed_by TEXT NOT NULL DEFAULT '',
			outcome_summary TEXT NOT NULL DEFAULT '',
			execution_key TEXT NOT NULL DEFAULT '',
			execution_revision INTEGER NOT NULL DEFAULT 0,
			claimed_by TEXT NOT NULL DEFAULT '',
			claim_started_at TEXT NOT NULL DEFAULT '',
			completed_at TEXT NOT NULL DEFAULT '',
			failed_at TEXT NOT NULL DEFAULT '',
			failure_summary TEXT NOT NULL DEFAULT '',
			revision INTEGER NOT NULL DEFAULT 0,
			answered_at TEXT NOT NULL DEFAULT '',
			consumed_at TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (workspace_path, widget_id, instance_key)
		)`,
		`CREATE TABLE IF NOT EXISTS report_widget_response_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_path TEXT NOT NULL,
			widget_id TEXT NOT NULL,
			instance_key TEXT NOT NULL,
			revision INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			status TEXT NOT NULL,
			subject_id TEXT NOT NULL DEFAULT '',
			subject_version TEXT NOT NULL DEFAULT '',
			subject_hash TEXT NOT NULL DEFAULT '',
			selected_option_id TEXT NOT NULL DEFAULT '',
			note TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			execution_key TEXT NOT NULL DEFAULT '',
			details TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	columns := map[string]string{
		"execution_key":      "TEXT NOT NULL DEFAULT ''",
		"execution_revision": "INTEGER NOT NULL DEFAULT 0",
		"claimed_by":         "TEXT NOT NULL DEFAULT ''",
		"claim_started_at":   "TEXT NOT NULL DEFAULT ''",
		"completed_at":       "TEXT NOT NULL DEFAULT ''",
		"failed_at":          "TEXT NOT NULL DEFAULT ''",
		"failure_summary":    "TEXT NOT NULL DEFAULT ''",
	}
	for name, definition := range columns {
		if err := ensureColumn(ctx, db, "report_widget_responses", name, definition); err != nil {
			return err
		}
	}

	transitions := []string{
		`CREATE TRIGGER IF NOT EXISTS trg_report_widget_response_claim_transition
			BEFORE UPDATE OF status ON report_widget_responses
			WHEN NEW.status = 'executing' AND OLD.status <> 'answered'
			BEGIN SELECT RAISE(ABORT, 'report response must be answered before execution'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_report_widget_response_claim_fields
			BEFORE UPDATE OF status ON report_widget_responses
			WHEN NEW.status = 'executing' AND (NEW.execution_key = '' OR NEW.execution_revision <> OLD.revision)
			BEGIN SELECT RAISE(ABORT, 'report response execution requires a key for the current revision'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_report_widget_response_terminal_transition
			BEFORE UPDATE OF status ON report_widget_responses
			WHEN NEW.status IN ('completed', 'failed') AND OLD.status <> 'executing'
			BEGIN SELECT RAISE(ABORT, 'report response must be executing before completion or failure'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_report_widget_response_active_claim
			BEFORE UPDATE OF status ON report_widget_responses
			WHEN OLD.status = 'executing' AND NEW.status NOT IN ('executing', 'completed', 'failed', 'cancelled')
			BEGIN SELECT RAISE(ABORT, 'report response has an active execution claim'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_report_widget_response_terminal_claim
			BEFORE UPDATE OF status ON report_widget_responses
			WHEN NEW.status IN ('completed', 'failed') AND
				(NEW.execution_key = '' OR NEW.execution_key <> OLD.execution_key OR NEW.execution_revision <> OLD.revision)
			BEGIN SELECT RAISE(ABORT, 'report response terminal state must retain its execution claim'); END`,
		`CREATE TRIGGER IF NOT EXISTS trg_report_widget_response_answer_insert_event
			AFTER INSERT ON report_widget_responses
			WHEN NEW.status = 'answered'
			BEGIN
				INSERT INTO report_widget_response_events
					(workspace_path, widget_id, instance_key, revision, event_type, status, subject_id,
					 subject_version, subject_hash, selected_option_id, note, actor, execution_key, details, created_at)
				VALUES
					(NEW.workspace_path, NEW.widget_id, NEW.instance_key, NEW.revision, 'answered', NEW.status,
					 NEW.subject_id, NEW.subject_version, NEW.subject_hash, NEW.selected_option_id, NEW.note,
					 NEW.answered_by, NEW.execution_key, '', NEW.updated_at);
			END`,
		`CREATE TRIGGER IF NOT EXISTS trg_report_widget_response_answer_update_event
			AFTER UPDATE OF revision ON report_widget_responses
			WHEN NEW.status = 'answered' AND NEW.revision <> OLD.revision
			BEGIN
				INSERT INTO report_widget_response_events
					(workspace_path, widget_id, instance_key, revision, event_type, status, subject_id,
					 subject_version, subject_hash, selected_option_id, note, actor, execution_key, details, created_at)
				VALUES
					(NEW.workspace_path, NEW.widget_id, NEW.instance_key, NEW.revision, 'answered', NEW.status,
					 NEW.subject_id, NEW.subject_version, NEW.subject_hash, NEW.selected_option_id, NEW.note,
					 NEW.answered_by, NEW.execution_key, '', NEW.updated_at);
			END`,
		`CREATE TRIGGER IF NOT EXISTS trg_report_widget_response_execution_event
			AFTER UPDATE OF status ON report_widget_responses
			WHEN NEW.status <> OLD.status AND NEW.status IN ('executing', 'completed', 'failed', 'cancelled')
			BEGIN
				INSERT INTO report_widget_response_events
					(workspace_path, widget_id, instance_key, revision, event_type, status, subject_id,
					 subject_version, subject_hash, selected_option_id, note, actor, execution_key, details, created_at)
				VALUES
					(NEW.workspace_path, NEW.widget_id, NEW.instance_key, NEW.revision,
					 CASE NEW.status WHEN 'executing' THEN 'claimed' ELSE NEW.status END, NEW.status,
					 NEW.subject_id, NEW.subject_version, NEW.subject_hash, NEW.selected_option_id, NEW.note,
					 CASE NEW.status
						WHEN 'executing' THEN NEW.claimed_by
						WHEN 'completed' THEN NEW.consumed_by
						ELSE NEW.claimed_by
					 END,
					 NEW.execution_key,
					 CASE NEW.status
						WHEN 'completed' THEN NEW.outcome_summary
						WHEN 'failed' THEN NEW.failure_summary
						ELSE ''
					 END,
					 NEW.updated_at);
			END`,
	}
	for _, stmt := range transitions {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_report_widget_responses_status
			ON report_widget_responses(status, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_report_widget_responses_subject
			ON report_widget_responses(subject_id, subject_version, updated_at)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_report_widget_responses_execution_key
			ON report_widget_responses(workspace_path, widget_id, instance_key, execution_key)
			WHERE execution_key <> ''`,
		`CREATE INDEX IF NOT EXISTS idx_report_widget_response_events_lookup
			ON report_widget_response_events(workspace_path, widget_id, instance_key, revision, id)`,
	}
	for _, stmt := range indexes {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func ensureColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			_ = rows.Close()
			return err
		}
		if strings.EqualFold(name, column) {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition))
	return err
}
