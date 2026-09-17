// Package pulsestore owns the compact, versioned Pulse persistence model.
//
// Version 1 collapses the legacy Pulse lifecycle into three product-facing
// tables: reviews, issues, and decisions. Version 2 restricts review history
// to the four current reviewers. Migrations are safe to run at deployment and
// again whenever an older/offline workflow database is opened.
package pulsestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/manishiitg/coding-agent-loop/workspace/sqliteopen"
	_ "modernc.org/sqlite"
)

const CurrentSchemaVersion = 2

const schema = `
CREATE TABLE IF NOT EXISTS pulse_schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL,
    backup_path TEXT NOT NULL DEFAULT '',
    legacy_tables_retained INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS pulse_reviews (
    id TEXT PRIMARY KEY,
    pulse_run_id TEXT NOT NULL,
    reviewer_type TEXT NOT NULL,
    status TEXT NOT NULL,
    summary TEXT NOT NULL DEFAULT '',
    evidence_json TEXT NOT NULL DEFAULT '[]',
    next_check_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(pulse_run_id, reviewer_type)
);
CREATE INDEX IF NOT EXISTS idx_pulse_reviews_type_created
    ON pulse_reviews(reviewer_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_pulse_reviews_status
    ON pulse_reviews(status, updated_at DESC);
CREATE TABLE IF NOT EXISTS pulse_issues (
    id TEXT PRIMARY KEY COLLATE NOCASE,
    opened_by_review_id TEXT,
    issue_type TEXT NOT NULL DEFAULT 'workflow_issue',
    description TEXT NOT NULL,
    evidence_json TEXT NOT NULL DEFAULT '[]',
    dedupe_key TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL CHECK(status IN ('open','closed')),
    action_taken TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    CHECK(status <> 'closed' OR trim(action_taken) <> '')
);
CREATE INDEX IF NOT EXISTS idx_pulse_issues_status_updated
    ON pulse_issues(status, updated_at DESC);
CREATE TABLE IF NOT EXISTS pulse_decisions (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL,
    question TEXT NOT NULL,
    options_json TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL CHECK(status IN ('pending','answered')),
    answer TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    answered_at TEXT NOT NULL DEFAULT '',
    FOREIGN KEY(issue_id) REFERENCES pulse_issues(id)
);
CREATE INDEX IF NOT EXISTS idx_pulse_decisions_status_created
    ON pulse_decisions(status, created_at DESC);
`

type Result struct {
	Path           string `json:"path"`
	Applied        bool   `json:"applied"`
	AlreadyCurrent bool   `json:"already_current"`
	BackupPath     string `json:"backup_path,omitempty"`
	Reviews        int    `json:"reviews"`
	Issues         int    `json:"issues"`
	Decisions      int    `json:"decisions"`
}

type BatchResult struct {
	Scanned  int            `json:"scanned"`
	Migrated int            `json:"migrated"`
	Current  int            `json:"current"`
	Skipped  int            `json:"skipped"`
	Results  []Result       `json:"results"`
	Failures []BatchFailure `json:"failures,omitempty"`
}

type BatchFailure struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

var migrationMu sync.Mutex

// MigratePath upgrades one workflow db.sqlite. Databases with no Pulse-owned
// legacy or current tables are skipped so unrelated product databases remain
// untouched during a deployment-wide scan.
func MigratePath(ctx context.Context, dbPath string) (Result, error) {
	result := Result{Path: dbPath}
	info, err := os.Stat(dbPath)
	if err != nil {
		return result, err
	}
	if info.IsDir() {
		return result, fmt.Errorf("Pulse database path is a directory: %s", dbPath)
	}
	db, err := sql.Open("sqlite", sqliteopen.DSN(dbPath))
	if err != nil {
		return result, err
	}
	defer db.Close()
	return migrate(ctx, db, dbPath, false)
}

// Ensure upgrades an already-open workflow database. It is the lazy safety net
// for laptops or deployments that were offline during the rollout.
func Ensure(ctx context.Context, db *sql.DB, dbPath string) (Result, error) {
	return migrate(ctx, db, dbPath, true)
}

func migrate(ctx context.Context, db *sql.DB, dbPath string, createForEmpty bool) (Result, error) {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	result := Result{Path: dbPath}
	current, err := schemaVersion(ctx, db)
	if err != nil {
		return result, err
	}
	if current >= CurrentSchemaVersion {
		result.AlreadyCurrent = true
		return withCounts(ctx, db, result)
	}
	legacy, err := hasLegacyPulseData(ctx, db)
	if err != nil {
		return result, err
	}
	if !legacy && !createForEmpty && current == 0 {
		return result, nil
	}

	backupPath := ""
	if legacy || current > 0 {
		backupPath, err = createMigrationBackup(ctx, db, dbPath)
		if err != nil {
			return result, fmt.Errorf("back up Pulse database before schema v%d: %w", CurrentSchemaVersion, err)
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, schema); err != nil {
		return result, fmt.Errorf("create compact Pulse schema: %w", err)
	}
	if err := backfillReviews(ctx, tx); err != nil {
		return result, err
	}
	if err := backfillIssues(ctx, tx); err != nil {
		return result, err
	}
	if err := backfillDecisions(ctx, tx); err != nil {
		return result, err
	}
	if err := cleanupRetiredReviewers(ctx, tx); err != nil {
		return result, err
	}
	if err := validateCompactStore(ctx, tx); err != nil {
		return result, fmt.Errorf("validate compact Pulse migration: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_schema_migrations(version,applied_at,backup_path,legacy_tables_retained)
		VALUES (?,?,?,1)`, CurrentSchemaVersion, now, backupPath); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	if err := installCompatibilityTriggers(ctx, db); err != nil {
		return result, err
	}
	result.Applied = true
	result.BackupPath = backupPath
	return withCounts(ctx, db, result)
}

func schemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	exists, err := tableExists(ctx, db, "pulse_schema_migrations")
	if err != nil || !exists {
		return 0, err
	}
	var version int
	err = db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM pulse_schema_migrations`).Scan(&version)
	return version, err
}

func hasLegacyPulseData(ctx context.Context, db *sql.DB) (bool, error) {
	for _, table := range []string{"pulse_module_state", "pulse_module_audit", "run_concerns", "pulse_review_log", "pulse_review_notes"} {
		exists, err := tableExists(ctx, db, table)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	if exists, err := tableExists(ctx, db, "report_human_inputs"); err != nil {
		return false, err
	} else if exists {
		ok, err := columnsExist(ctx, db, "report_human_inputs", []string{"source"})
		if err != nil || !ok {
			return false, err
		}
		var found int
		err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM report_human_inputs
			WHERE source IN ('pulse','plan_drift_review','technical_review','architecture_review','strategic_review','strategy_auditor','goal_advisor'))`).Scan(&found)
		return found != 0, err
	}
	return false, nil
}

func createMigrationBackup(ctx context.Context, db *sql.DB, dbPath string) (string, error) {
	backupDir := filepath.Join(filepath.Dir(dbPath), "migrations", ".backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	backupPath := filepath.Join(backupDir, fmt.Sprintf("pulse-schema-v%d.sqlite", CurrentSchemaVersion))
	if _, err := os.Stat(backupPath); err == nil {
		return backupPath, validateSQLiteFile(ctx, backupPath)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	temp, err := os.CreateTemp(backupDir, ".pulse-schema-backup-*.sqlite")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	if err := temp.Close(); err != nil {
		return "", err
	}
	_ = os.Remove(tempPath)
	defer os.Remove(tempPath)
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", tempPath); err != nil {
		return "", err
	}
	if err := validateSQLiteFile(ctx, tempPath); err != nil {
		return "", err
	}
	if err := os.Chmod(tempPath, 0o444); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}

func validateSQLiteFile(ctx context.Context, path string) error {
	dsn := (&url.URL{Scheme: "file", Path: path}).String() + "?mode=ro&_pragma=query_only(true)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(integrity), "ok") {
		return fmt.Errorf("backup integrity_check returned %q", integrity)
	}
	return nil
}

func backfillReviews(ctx context.Context, tx *sql.Tx) error {
	auditReady, err := columnsExistTx(ctx, tx, "pulse_module_audit", []string{"module", "pulse_run_id", "result", "reason", "evidence_json", "recorded_at"})
	if err != nil {
		return err
	}
	if auditReady {
		_, err = tx.ExecContext(ctx, `INSERT INTO pulse_reviews
			(id,pulse_run_id,reviewer_type,status,summary,evidence_json,next_check_at,created_at,updated_at)
			SELECT 'review:' || pulse_run_id || ':' || CASE module
			         WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
			         WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE module END,
			       pulse_run_id, CASE module
			         WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
			         WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE module END,
			       CASE lower(result) WHEN 'done' THEN 'done' WHEN 'changed' THEN 'changed'
			            WHEN 'blocked' THEN 'blocked' WHEN 'failed' THEN 'failed'
			            WHEN 'skipped' THEN 'skipped' WHEN 'timed_out' THEN 'failed' ELSE 'done' END,
			       reason, CASE WHEN json_valid(evidence_json) THEN evidence_json ELSE '[]' END,
			       '', recorded_at, recorded_at
			FROM pulse_module_audit
			WHERE trim(pulse_run_id)<>'' AND module IN
			('plan_drift_review','technical_review','architecture_review','strategic_review',
			 'workflow_review','llmops_review','strategy_auditor','goal_advisor')
			ON CONFLICT(pulse_run_id,reviewer_type) DO UPDATE SET
			status=excluded.status, summary=excluded.summary, evidence_json=excluded.evidence_json,
			updated_at=excluded.updated_at`)
		if err != nil {
			return fmt.Errorf("backfill Pulse reviews from module audit: %w", err)
		}
	}
	if exists, err := tableExistsTx(ctx, tx, "pulse_module_state"); err != nil {
		return err
	} else if exists {
		ok, err := columnsExistTx(ctx, tx, "pulse_module_state", []string{"module", "last_pulse_run_id", "last_decision", "last_reason", "last_result", "last_result_reason", "next_check_at", "evidence_json", "updated_at"})
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO pulse_reviews
			(id,pulse_run_id,reviewer_type,status,summary,evidence_json,next_check_at,created_at,updated_at)
			SELECT 'review:' || last_pulse_run_id || ':' || CASE module
			         WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
			         WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE module END,
			       last_pulse_run_id, CASE module
			         WHEN 'workflow_review' THEN 'technical_review' WHEN 'llmops_review' THEN 'technical_review'
			         WHEN 'strategy_auditor' THEN 'strategic_review' WHEN 'goal_advisor' THEN 'strategic_review' ELSE module END,
			       CASE WHEN lower(last_result) IN ('done','changed','blocked','failed','skipped') THEN lower(last_result)
			            WHEN lower(last_decision)='due' THEN 'scheduled'
			            WHEN lower(last_decision)='skipped' THEN 'skipped' ELSE 'skipped' END,
			       CASE WHEN trim(last_result_reason)<>'' THEN last_result_reason ELSE last_reason END,
			       CASE WHEN json_valid(evidence_json) THEN evidence_json ELSE '[]' END,
			       next_check_at, updated_at, updated_at
			FROM pulse_module_state
			WHERE trim(last_pulse_run_id)<>'' AND module IN
			('plan_drift_review','technical_review','architecture_review','strategic_review',
			 'workflow_review','llmops_review','strategy_auditor','goal_advisor')
			ON CONFLICT(pulse_run_id,reviewer_type) DO NOTHING`)
		if err != nil {
			return fmt.Errorf("backfill latest Pulse worklist state: %w", err)
		}
	}
	return nil
}

type legacyIssueDetail struct {
	IssueKind    string   `json:"issue_kind"`
	Evidence     []string `json:"evidence"`
	HumanInputID string   `json:"human_input_id"`
	Module       string   `json:"module"`
	MergedIntoID string   `json:"merged_into_issue_id"`
}

func backfillIssues(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExistsTx(ctx, tx, "run_concerns")
	if err != nil || !exists {
		return err
	}
	ok, err := columnsExistTx(ctx, tx, "run_concerns", []string{"fingerprint", "issue_id", "step_id", "phase", "text", "first_seen_run", "first_seen_at", "last_seen_run", "last_seen_at", "status", "resolution_note"})
	if err != nil || !ok {
		return err
	}
	query := `SELECT c.fingerprint,c.issue_id,c.step_id,c.phase,c.text,c.first_seen_run,c.first_seen_at,
		c.last_seen_run,c.last_seen_at,c.status,c.resolution_note`
	if details, _ := columnsExistTx(ctx, tx, "pulse_finding_details", []string{"fingerprint", "detail_json", "source_run_id"}); details {
		query += `,COALESCE(d.detail_json,'{}'),COALESCE(d.source_run_id,'') FROM run_concerns c
			LEFT JOIN pulse_finding_details d ON d.fingerprint=c.fingerprint`
	} else {
		query += `,'{}','' FROM run_concerns c`
	}
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("read legacy Pulse issues: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var fingerprint, issueID, stepID, phase, description, firstRun, firstAt, lastRun, lastAt, legacyStatus, resolution, detailJSON, sourceRun string
		if err := rows.Scan(&fingerprint, &issueID, &stepID, &phase, &description, &firstRun, &firstAt, &lastRun, &lastAt, &legacyStatus, &resolution, &detailJSON, &sourceRun); err != nil {
			return err
		}
		var detail legacyIssueDetail
		_ = json.Unmarshal([]byte(detailJSON), &detail)
		if strings.TrimSpace(detail.MergedIntoID) != "" {
			continue
		}
		if strings.TrimSpace(issueID) == "" {
			issueID = issueIDForKey(fingerprint)
		}
		status := "open"
		action := ""
		if legacyStatus == "resolved" || legacyStatus == "rejected" {
			status = "closed"
			action = strings.TrimSpace(resolution)
			if action == "" {
				if legacyStatus == "rejected" {
					action = "Rejected in the legacy Pulse lifecycle; no additional action was recorded."
				} else {
					action = "Resolved in the legacy Pulse lifecycle; no action detail was recorded."
				}
			}
		}
		issueType := strings.TrimSpace(detail.IssueKind)
		if issueType == "" {
			issueType = "workflow_issue"
		}
		evidence, _ := json.Marshal(detail.Evidence)
		createdAt := firstNonEmpty(firstAt, lastAt, time.Now().UTC().Format(time.RFC3339Nano))
		updatedAt := firstNonEmpty(lastAt, firstAt, createdAt)
		openedBy := legacyReviewID(firstNonEmpty(sourceRun, firstRun), firstNonEmpty(detail.Module, reviewModule(phase, stepID)))
		if openedBy != "" {
			var present int
			_ = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pulse_reviews WHERE id=?)`, openedBy).Scan(&present)
			if present == 0 {
				openedBy = ""
			}
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM pulse_issues WHERE dedupe_key=? AND id<>?`, fingerprint, issueID); err != nil {
			return fmt.Errorf("reconcile backfilled Pulse issue %s: %w", issueID, err)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO pulse_issues
			(id,opened_by_review_id,issue_type,description,evidence_json,dedupe_key,status,action_taken,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET opened_by_review_id=COALESCE(excluded.opened_by_review_id,pulse_issues.opened_by_review_id),
			issue_type=excluded.issue_type,description=excluded.description,evidence_json=excluded.evidence_json,
			dedupe_key=excluded.dedupe_key,status=excluded.status,action_taken=excluded.action_taken,updated_at=excluded.updated_at`,
			issueID, nullableString(openedBy), issueType, firstNonEmpty(description, "Legacy Pulse issue"), string(evidence), fingerprint, status, action, createdAt, updatedAt)
		if err != nil {
			return fmt.Errorf("backfill Pulse issue %s: %w", issueID, err)
		}
	}
	return rows.Err()
}

func backfillDecisions(ctx context.Context, tx *sql.Tx) error {
	exists, err := tableExistsTx(ctx, tx, "report_human_inputs")
	if err != nil || !exists {
		return err
	}
	ok, err := columnsExistTx(ctx, tx, "report_human_inputs", []string{"id", "source", "question", "context", "options_json", "status", "selected_option_id", "note", "created_at", "answered_at", "apply_contract_json"})
	if err != nil || !ok {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,source,question,context,options_json,status,selected_option_id,note,
		created_at,answered_at,apply_contract_json FROM report_human_inputs
		WHERE source IN ('pulse','plan_drift_review','technical_review','architecture_review','strategic_review','strategy_auditor','goal_advisor')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, source, question, decisionContext, optionsJSON, legacyStatus, selected, note, createdAt, answeredAt, contractJSON string
		if err := rows.Scan(&id, &source, &question, &decisionContext, &optionsJSON, &legacyStatus, &selected, &note, &createdAt, &answeredAt, &contractJSON); err != nil {
			return err
		}
		issueID := issueIDFromContract(contractJSON)
		if issueID == "" {
			issueID, _ = issueForHumanInput(ctx, tx, id)
		}
		if issueID == "" {
			issueID = issueIDForKey("decision:" + id)
			dedupe := "decision:" + id
			description := firstNonEmpty(decisionContext, "Decision required: "+question)
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO pulse_issues
				(id,issue_type,description,evidence_json,dedupe_key,status,action_taken,created_at,updated_at)
				VALUES (?,'workflow_issue',?,'[]',?,'open','',?,?)`, issueID, description, dedupe, createdAt, createdAt); err != nil {
				return err
			}
		}
		var issuePresent int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pulse_issues WHERE id=?)`, issueID).Scan(&issuePresent); err != nil {
			return err
		}
		if issuePresent == 0 {
			continue
		}
		status := "pending"
		answer := ""
		if strings.TrimSpace(answeredAt) != "" || legacyStatus == "answered" || legacyStatus == "consumed" {
			status = "answered"
			answer = strings.TrimSpace(strings.Join(nonEmptyStrings(selected, note), ": "))
		}
		if !json.Valid([]byte(optionsJSON)) {
			optionsJSON = "[]"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO pulse_decisions
			(id,issue_id,question,options_json,status,answer,created_at,answered_at)
			VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET
			issue_id=excluded.issue_id,question=excluded.question,options_json=excluded.options_json,
			status=excluded.status,answer=excluded.answer,answered_at=excluded.answered_at`,
			id, issueID, question, optionsJSON, status, answer, createdAt, answeredAt)
		if err != nil {
			return fmt.Errorf("backfill Pulse decision %s (%s): %w", id, source, err)
		}
	}
	return rows.Err()
}

func validateCompactStore(ctx context.Context, tx *sql.Tx) error {
	checks := []struct {
		query string
		name  string
	}{
		{`SELECT COUNT(*) FROM pulse_issues WHERE status='closed' AND trim(action_taken)=''`, "closed issue without action_taken"},
		{`SELECT COUNT(*) FROM pulse_decisions d LEFT JOIN pulse_issues i ON i.id=d.issue_id WHERE i.id IS NULL`, "decision without issue"},
		{`SELECT COUNT(*) FROM (SELECT id,COUNT(*) n FROM pulse_issues GROUP BY lower(id) HAVING n>1)`, "duplicate public issue id"},
		{`SELECT COUNT(*) FROM pulse_reviews WHERE reviewer_type NOT IN
		  ('plan_drift_review','technical_review','architecture_review','strategic_review')`, "retired reviewer row"},
	}
	for _, check := range checks {
		var count int
		if err := tx.QueryRowContext(ctx, check.query).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return fmt.Errorf("%s: %d row(s)", check.name, count)
		}
	}
	return nil
}

func cleanupRetiredReviewers(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM pulse_reviews WHERE reviewer_type NOT IN
		('plan_drift_review','technical_review','architecture_review','strategic_review')`); err != nil {
		return fmt.Errorf("remove retired Pulse reviewer rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE pulse_issues SET opened_by_review_id=NULL
		WHERE opened_by_review_id IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM pulse_reviews r WHERE r.id=pulse_issues.opened_by_review_id)`); err != nil {
		return fmt.Errorf("remove retired Pulse review links: %w", err)
	}
	return nil
}

func withCounts(ctx context.Context, db *sql.DB, result Result) (Result, error) {
	for table, target := range map[string]*int{"pulse_reviews": &result.Reviews, "pulse_issues": &result.Issues, "pulse_decisions": &result.Decisions} {
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(target); err != nil {
			return result, err
		}
	}
	return result, nil
}

func tableExists(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var found int
	err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, name).Scan(&found)
	return found != 0, err
}

func tableExistsTx(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var found int
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type='table' AND name=?)`, name).Scan(&found)
	return found != 0, err
}

func columnsExistTx(ctx context.Context, tx *sql.Tx, table string, required []string) (bool, error) {
	exists, err := tableExistsTx(ctx, tx, table)
	if err != nil || !exists {
		return false, err
	}
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return false, err
		}
		found[name] = true
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, name := range required {
		if !found[name] {
			return false, nil
		}
	}
	return true, nil
}

func issueIDForKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return "PUL-" + strings.ToUpper(hex.EncodeToString(sum[:4]))
}

func issueIDFromContract(raw string) string {
	var contract struct {
		IssueID string `json:"issue_id"`
	}
	_ = json.Unmarshal([]byte(raw), &contract)
	return strings.TrimSpace(contract.IssueID)
}

func issueForHumanInput(ctx context.Context, tx *sql.Tx, inputID string) (string, error) {
	exists, err := tableExistsTx(ctx, tx, "pulse_finding_events")
	if err != nil || !exists {
		return "", err
	}
	var issueID string
	err = tx.QueryRowContext(ctx, `SELECT c.issue_id FROM pulse_finding_events e
		JOIN run_concerns c ON c.fingerprint=e.fingerprint
		WHERE COALESCE(json_extract(e.metadata_json,'$.human_input_id'),'')=?
		ORDER BY e._id DESC LIMIT 1`, inputID).Scan(&issueID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return strings.TrimSpace(issueID), err
}

func legacyReviewID(runID, module string) string {
	runID, module = strings.TrimSpace(runID), normalizeReviewerType(module)
	if runID == "" || module == "" {
		return ""
	}
	return "review:" + runID + ":" + module
}

func normalizeReviewerType(module string) string {
	switch strings.TrimSpace(module) {
	case "workflow_review", "llmops_review", "technical_review":
		return "technical_review"
	case "strategy_auditor", "goal_advisor", "strategic_review":
		return "strategic_review"
	case "plan_drift_review":
		return "plan_drift_review"
	case "architecture_review":
		return "architecture_review"
	default:
		return ""
	}
}

func reviewModule(phase, stepID string) string {
	if phase == "review" {
		return stepID
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}
