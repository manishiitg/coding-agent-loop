package reportinteraction

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestEnsureSchemaCreatesLifecycleAndRejectsUnsafeCompletion(t *testing.T) {
	ctx := context.Background()
	db, err := OpenDatabase(ctx, filepath.Join(t.TempDir(), "db", "db.sqlite"))
	if err != nil {
		t.Fatalf("open report interaction db: %v", err)
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `INSERT INTO report_widget_responses
		(workspace_path, widget_id, instance_key, question, response_kind, status, revision, created_at, updated_at)
		VALUES ('Workflow/test', 'approval', 'v1', 'Approve?', 'choice', 'answered', 1, 'now', 'now')`)
	if err != nil {
		t.Fatalf("insert answered response: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE report_widget_responses SET status='completed'
		WHERE workspace_path='Workflow/test' AND widget_id='approval' AND instance_key='v1'`); err == nil {
		t.Fatal("unsafe answered-to-completed transition should be rejected")
	}
	if _, err := db.ExecContext(ctx, `UPDATE report_widget_responses
		SET status='executing', execution_key='approval:v1:1:publisher', execution_revision=1
		WHERE workspace_path='Workflow/test' AND widget_id='approval' AND instance_key='v1'`); err != nil {
		t.Fatalf("claim answered response: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE report_widget_responses SET status='completed'
		WHERE workspace_path='Workflow/test' AND widget_id='approval' AND instance_key='v1'`); err != nil {
		t.Fatalf("complete claimed response: %v", err)
	}
	var eventCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM report_widget_response_events
		WHERE workspace_path='Workflow/test' AND widget_id='approval' AND instance_key='v1'`).Scan(&eventCount); err != nil {
		t.Fatalf("count direct SQL audit events: %v", err)
	}
	if eventCount != 3 {
		t.Fatalf("direct SQL audit event count = %d, want answer + claim + completion", eventCount)
	}
}

func TestOpenDatabaseMigratesExistingResponseTable(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "db", "db.sqlite")
	db, err := OpenDatabase(ctx, dbPath)
	if err != nil {
		t.Fatalf("create report interaction db: %v", err)
	}
	if _, err := db.ExecContext(ctx, "DROP TABLE report_widget_responses"); err != nil {
		t.Fatalf("drop current response table: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE report_widget_responses (
		workspace_path TEXT NOT NULL,
		widget_id TEXT NOT NULL,
		instance_key TEXT NOT NULL,
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
		revision INTEGER NOT NULL DEFAULT 0,
		answered_at TEXT NOT NULL DEFAULT '',
		consumed_at TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (workspace_path, widget_id, instance_key)
	)`); err != nil {
		t.Fatalf("create legacy response table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	db, err = OpenDatabase(ctx, dbPath)
	if err != nil {
		t.Fatalf("migrate legacy response table: %v", err)
	}
	defer db.Close()
	for _, column := range []string{"execution_key", "execution_revision", "claimed_by", "completed_at", "failure_summary"} {
		if !schemaColumnExists(t, db, column) {
			t.Fatalf("migrated response table is missing %q", column)
		}
	}
}

func schemaColumnExists(t *testing.T, db *sql.DB, want string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(report_widget_responses)")
	if err != nil {
		t.Fatalf("read response table columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan response table column: %v", err)
		}
		if name == want {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate response table columns: %v", err)
	}
	return false
}
