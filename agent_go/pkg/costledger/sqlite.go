package costledger

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS cost_events (
    event_id TEXT PRIMARY KEY,
    idempotency_key TEXT NOT NULL UNIQUE,
    occurred_at TEXT NOT NULL,
    user_id TEXT NOT NULL DEFAULT '',
    workflow_id TEXT NOT NULL DEFAULT '',
    session_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    execution_id TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL DEFAULT 'unknown',
    agent_mode TEXT NOT NULL DEFAULT '',
    component TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    requested_provider TEXT NOT NULL DEFAULT '',
    requested_model_id TEXT NOT NULL DEFAULT '',
    effective_provider TEXT NOT NULL DEFAULT '',
    effective_model_id TEXT NOT NULL DEFAULT '',
    turn_count INTEGER NOT NULL DEFAULT 0,
    llm_call_count INTEGER NOT NULL DEFAULT 0,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens INTEGER NOT NULL DEFAULT 0,
    cache_write_tokens INTEGER NOT NULL DEFAULT 0,
    total_cost_usd REAL NOT NULL DEFAULT 0,
    currency TEXT NOT NULL DEFAULT 'USD',
    billing_basis TEXT NOT NULL DEFAULT 'unpriced',
    pricing_source TEXT NOT NULL DEFAULT '',
    pricing_version TEXT NOT NULL DEFAULT '',
    tool_name TEXT NOT NULL DEFAULT '',
    operation_metadata_json TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_cost_events_occurred_at ON cost_events(occurred_at);
CREATE INDEX IF NOT EXISTS idx_cost_events_effective_model ON cost_events(effective_model_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_cost_events_workflow_scope ON cost_events(workflow_id, scope, occurred_at);
CREATE TABLE IF NOT EXISTS cost_event_quarantine (
    source_hash TEXT PRIMARY KEY,
    source_path TEXT NOT NULL,
    line_number INTEGER NOT NULL,
    raw_record TEXT NOT NULL,
    parse_error TEXT NOT NULL,
    quarantined_at TEXT NOT NULL
);`

type sqliteLedger struct {
	db *sql.DB
}

// MigrationReport describes one idempotent legacy JSONL import.
type MigrationReport struct {
	Imported    int `json:"imported"`
	Duplicates  int `json:"duplicates"`
	Quarantined int `json:"quarantined"`
}

// NewSQLiteLedger opens the authoritative local cost event database.
func NewSQLiteLedger(dbPath string) (*Ledger, error) {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return nil, fmt.Errorf("costledger: SQLite path is required")
	}
	absPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("costledger: resolve SQLite path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("costledger: create SQLite directory: %w", err)
	}
	u := &url.URL{Scheme: "file", Path: absPath}
	dsn := u.String() + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("costledger: open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("costledger: ping SQLite database: %w", err)
	}
	if _, err := db.Exec(sqliteSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("costledger: initialize SQLite schema: %w", err)
	}
	return &Ledger{db: &sqliteLedger{db: db}}, nil
}

func (s *sqliteLedger) append(e Entry) error {
	normalizeEntry(&e)
	metadata, err := json.Marshal(e.OperationMetadata)
	if err != nil {
		return fmt.Errorf("costledger: marshal operation metadata: %w", err)
	}
	const insertEvent = `
INSERT OR IGNORE INTO cost_events (
    event_id, idempotency_key, occurred_at, user_id, workflow_id, session_id,
    run_id, execution_id, scope, agent_mode, component, correlation_id,
    requested_provider, requested_model_id, effective_provider, effective_model_id,
    turn_count, llm_call_count, prompt_tokens, completion_tokens, reasoning_tokens,
    cache_read_tokens, cache_write_tokens, total_cost_usd, currency, billing_basis,
    pricing_source, pricing_version, tool_name, operation_metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	args := []interface{}{
		e.EventID, e.IdempotencyKey, e.Timestamp.UTC().Format(time.RFC3339Nano),
		e.UserID, e.WorkflowID, e.SessionID, e.RunID, e.ExecutionID, e.Scope,
		e.AgentMode, e.Component, e.CorrelationID, e.Provider, e.ModelID,
		e.EffectiveProvider, e.EffectiveModelID, e.TurnCount, e.LLMCallCount,
		e.PromptTokens, e.CompletionTokens, e.ReasoningTokens, e.CacheReadTokens,
		e.CacheWriteTokens, e.TotalCostUSD, e.Currency, e.BillingBasis,
		e.PricingSource, e.PricingVersion, e.ToolName, string(metadata),
	}
	for attempt := 0; ; attempt++ {
		_, err = s.db.Exec(insertEvent, args...)
		if err == nil {
			return nil
		}
		if attempt >= 2 || !isSQLiteBusy(err) {
			return fmt.Errorf("costledger: insert SQLite event: %w", err)
		}
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
	}
}

func (s *sqliteLedger) summarize(from, to string) (*Summary, error) {
	fromInclusive, toExclusive, err := costDateBounds(from, to)
	if err != nil {
		return nil, err
	}
	summary := &Summary{
		From: from, To: to,
		ByDate: make(map[string]*DateAggregate), ByModel: make(map[string]*Aggregate),
		Coverage: Coverage{Source: "sqlite"},
	}
	query := `
SELECT event_id, idempotency_key, occurred_at, user_id, workflow_id, session_id,
       run_id, execution_id, scope, agent_mode, component, correlation_id,
       requested_provider, requested_model_id, effective_provider, effective_model_id,
       turn_count, llm_call_count, prompt_tokens, completion_tokens, reasoning_tokens,
       cache_read_tokens, cache_write_tokens, total_cost_usd, currency, billing_basis,
       pricing_source, pricing_version, tool_name, operation_metadata_json
FROM cost_events`
	where := make([]string, 0, 2)
	args := make([]interface{}, 0, 2)
	if fromInclusive != "" {
		where = append(where, "occurred_at >= ?")
		args = append(args, fromInclusive)
	}
	if toExclusive != "" {
		where = append(where, "occurred_at < ?")
		args = append(args, toExclusive)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY occurred_at"
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("costledger: query SQLite events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e Entry
		var occurredAt, metadataJSON string
		if err := rows.Scan(
			&e.EventID, &e.IdempotencyKey, &occurredAt, &e.UserID, &e.WorkflowID,
			&e.SessionID, &e.RunID, &e.ExecutionID, &e.Scope, &e.AgentMode,
			&e.Component, &e.CorrelationID, &e.Provider, &e.ModelID,
			&e.EffectiveProvider, &e.EffectiveModelID, &e.TurnCount, &e.LLMCallCount,
			&e.PromptTokens, &e.CompletionTokens, &e.ReasoningTokens,
			&e.CacheReadTokens, &e.CacheWriteTokens, &e.TotalCostUSD, &e.Currency,
			&e.BillingBasis, &e.PricingSource, &e.PricingVersion, &e.ToolName,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("costledger: scan SQLite event: %w", err)
		}
		ts, err := time.Parse(time.RFC3339Nano, occurredAt)
		if err != nil {
			return nil, fmt.Errorf("costledger: parse stored timestamp %q: %w", occurredAt, err)
		}
		e.Timestamp = ts
		date := ts.UTC().Format("2006-01-02")
		addEntryToSummary(summary, date, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("costledger: iterate SQLite events: %w", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM cost_event_quarantine`).Scan(&summary.Coverage.QuarantinedEventCount); err != nil {
		return nil, fmt.Errorf("costledger: count quarantined events: %w", err)
	}
	return summary, nil
}

func costDateBounds(from, to string) (string, string, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	parse := func(name, value string) (time.Time, error) {
		parsed, err := time.Parse("2006-01-02", value)
		if err != nil {
			return time.Time{}, fmt.Errorf("costledger: invalid %s date %q (expected YYYY-MM-DD): %w", name, value, err)
		}
		return parsed.UTC(), nil
	}
	fromInclusive := ""
	if from != "" {
		parsed, err := parse("from", from)
		if err != nil {
			return "", "", err
		}
		fromInclusive = parsed.Format(time.RFC3339Nano)
	}
	toExclusive := ""
	if to != "" {
		parsed, err := parse("to", to)
		if err != nil {
			return "", "", err
		}
		toExclusive = parsed.AddDate(0, 0, 1).Format(time.RFC3339Nano)
	}
	if fromInclusive != "" && toExclusive != "" && fromInclusive >= toExclusive {
		return "", "", fmt.Errorf("costledger: from date %q must not be after to date %q", from, to)
	}
	return fromInclusive, toExclusive, nil
}

func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "sqlite_busy") || strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked")
}

func (s *sqliteLedger) migrateLegacyJSONL(path string) (MigrationReport, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return MigrationReport{}, nil
	}
	if err != nil {
		return MigrationReport{}, fmt.Errorf("costledger: open legacy JSONL: %w", err)
	}
	defer file.Close()

	tx, err := s.db.Begin()
	if err != nil {
		return MigrationReport{}, fmt.Errorf("costledger: begin legacy migration: %w", err)
	}
	defer tx.Rollback()
	report := MigrationReport{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		raw := append([]byte(nil), scanner.Bytes()...)
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(raw))
		var e Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			result, qErr := tx.Exec(`INSERT OR IGNORE INTO cost_event_quarantine
                (source_hash, source_path, line_number, raw_record, parse_error, quarantined_at)
                VALUES (?, ?, ?, ?, ?, ?)`, hash, path, lineNumber, string(raw), err.Error(), time.Now().UTC().Format(time.RFC3339Nano))
			if qErr != nil {
				return MigrationReport{}, fmt.Errorf("costledger: quarantine malformed row %d: %w", lineNumber, qErr)
			}
			if affected, _ := result.RowsAffected(); affected > 0 {
				report.Quarantined++
			}
			continue
		}
		e.EventID = "legacy-" + hash
		e.IdempotencyKey = "legacy-jsonl:" + hash
		normalizeEntry(&e)
		metadata, err := json.Marshal(e.OperationMetadata)
		if err != nil {
			return MigrationReport{}, fmt.Errorf("costledger: marshal legacy metadata row %d: %w", lineNumber, err)
		}
		result, err := tx.Exec(`
INSERT OR IGNORE INTO cost_events (
    event_id, idempotency_key, occurred_at, user_id, workflow_id, session_id,
    run_id, execution_id, scope, agent_mode, component, correlation_id,
    requested_provider, requested_model_id, effective_provider, effective_model_id,
    turn_count, llm_call_count, prompt_tokens, completion_tokens, reasoning_tokens,
    cache_read_tokens, cache_write_tokens, total_cost_usd, currency, billing_basis,
    pricing_source, pricing_version, tool_name, operation_metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.EventID, e.IdempotencyKey, e.Timestamp.UTC().Format(time.RFC3339Nano),
			e.UserID, e.WorkflowID, e.SessionID, e.RunID, e.ExecutionID, e.Scope,
			e.AgentMode, e.Component, e.CorrelationID, e.Provider, e.ModelID,
			e.EffectiveProvider, e.EffectiveModelID, e.TurnCount, e.LLMCallCount,
			e.PromptTokens, e.CompletionTokens, e.ReasoningTokens, e.CacheReadTokens,
			e.CacheWriteTokens, e.TotalCostUSD, e.Currency, e.BillingBasis,
			e.PricingSource, e.PricingVersion, e.ToolName, string(metadata),
		)
		if err != nil {
			return MigrationReport{}, fmt.Errorf("costledger: migrate legacy row %d: %w", lineNumber, err)
		}
		if affected, _ := result.RowsAffected(); affected > 0 {
			report.Imported++
		} else {
			report.Duplicates++
		}
	}
	if err := scanner.Err(); err != nil {
		return MigrationReport{}, fmt.Errorf("costledger: scan legacy JSONL: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return MigrationReport{}, fmt.Errorf("costledger: commit legacy migration: %w", err)
	}
	return report, nil
}

func (s *sqliteLedger) close() error {
	return s.db.Close()
}

func normalizeEntry(e *Entry) {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	} else {
		e.Timestamp = e.Timestamp.UTC()
	}
	if e.Currency == "" {
		e.Currency = "USD"
	}
	if e.Scope == "" {
		e.Scope = "unknown"
	}
	if e.BillingBasis == "" {
		switch e.CostUSDSource {
		case "provider":
			e.BillingBasis = "provider_actual"
		case "estimated":
			e.BillingBasis = "token_estimate"
		default:
			if e.TotalCostUSD > 0 {
				e.BillingBasis = "token_estimate"
			} else {
				e.BillingBasis = "unpriced"
			}
		}
	}
	if e.LLMCallCount == 0 && !strings.HasPrefix(e.Component, "tool:") &&
		(e.Provider != "" || e.ModelID != "" || e.PromptTokens > 0 || e.CompletionTokens > 0) {
		e.LLMCallCount = 1
	}
	if e.IdempotencyKey == "" {
		copy := *e
		copy.EventID = ""
		copy.IdempotencyKey = ""
		data, _ := json.Marshal(copy)
		e.IdempotencyKey = fmt.Sprintf("event:%x", sha256.Sum256(data))
	}
	if e.EventID == "" {
		e.EventID = e.IdempotencyKey
	}
}
