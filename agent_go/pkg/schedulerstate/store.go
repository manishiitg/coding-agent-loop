package schedulerstate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type State string

const (
	StateStarting         State = "starting"
	StateWorkflowRunning  State = "workflow_running"
	StateWorkflowFinished State = "workflow_finished"
	StatePulseGate        State = "pulse_gate"
	StatePulseModules     State = "pulse_modules"
	StatePulseFinalizing  State = "pulse_finalizing"
	StateCompleted        State = "completed"
	StatePartial          State = "partial"
	StateFailed           State = "failed"
	StateStopped          State = "stopped"
	StateInterrupted      State = "interrupted"
)

var (
	ErrRunAlreadyActive  = errors.New("schedule run already active for scope")
	ErrRunNotFound       = errors.New("schedule run not found")
	ErrInvalidTransition = errors.New("invalid schedule run transition")
)

var allowedTransitions = map[State]map[State]bool{
	StateStarting: {
		StateWorkflowRunning: true, StateFailed: true, StateStopped: true, StateInterrupted: true,
	},
	StateWorkflowRunning: {
		StateWorkflowFinished: true, StateFailed: true, StateStopped: true, StateInterrupted: true,
	},
	StateWorkflowFinished: {
		StatePulseGate: true, StateCompleted: true, StatePartial: true, StateFailed: true, StateStopped: true, StateInterrupted: true,
	},
	StatePulseGate: {
		StatePulseModules: true, StatePulseFinalizing: true, StateCompleted: true, StatePartial: true, StateFailed: true, StateStopped: true, StateInterrupted: true,
	},
	StatePulseModules: {
		StatePulseFinalizing: true, StateCompleted: true, StatePartial: true, StateFailed: true, StateStopped: true, StateInterrupted: true,
	},
	StatePulseFinalizing: {
		StateCompleted: true, StatePartial: true, StateFailed: true, StateStopped: true, StateInterrupted: true,
	},
}

func IsTerminal(state State) bool {
	switch state {
	case StateCompleted, StatePartial, StateFailed, StateStopped, StateInterrupted:
		return true
	default:
		return false
	}
}

type Run struct {
	RunID           string
	ScopeType       string
	ScopeID         string
	LockKey         string
	ScheduleID      string
	TriggerSource   string
	State           State
	StartedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
	ActiveSessionID string
	RunFolder       string
	ErrorMessage    string
}

type Transition struct {
	RunID        string
	To           State
	Reason       string
	SessionID    string
	SessionKind  string
	RunFolder    string
	ErrorMessage string
	At           time.Time
}

type FireDecision struct {
	DecisionID    string
	ScopeType     string
	ScopeID       string
	ScheduleID    string
	TriggerSource string
	Decision      string
	Reason        string
	RunID         string
	FiredAt       time.Time
}

type Event struct {
	Sequence  int64
	RunID     string
	FromState State
	ToState   State
	Reason    string
	CreatedAt time.Time
}

type Store struct {
	db *sql.DB
}

const fireDecisionRetentionPerSchedule = 500

func Open(path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("schedule state path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create schedule state directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open schedule state: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) init(ctx context.Context) error {
	statements := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS schedule_runs (
			run_id TEXT PRIMARY KEY,
			scope_type TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			lock_key TEXT NOT NULL,
			schedule_id TEXT NOT NULL,
			trigger_source TEXT NOT NULL,
			state TEXT NOT NULL,
			started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			completed_at TEXT,
			active_session_id TEXT NOT NULL DEFAULT '',
			run_folder TEXT NOT NULL DEFAULT '',
			error_message TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_runs_active_lock
			ON schedule_runs(lock_key)
			WHERE state IN ('starting','workflow_running','workflow_finished','pulse_gate','pulse_modules','pulse_finalizing')`,
		`CREATE INDEX IF NOT EXISTS idx_schedule_runs_scope_started
			ON schedule_runs(scope_type, scope_id, schedule_id, started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS schedule_run_events (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			run_id TEXT NOT NULL,
			from_state TEXT NOT NULL,
			to_state TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			FOREIGN KEY(run_id) REFERENCES schedule_runs(run_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS schedule_run_sessions (
			run_id TEXT NOT NULL,
			session_id TEXT NOT NULL,
			session_kind TEXT NOT NULL,
			status TEXT NOT NULL,
			started_at TEXT NOT NULL,
			ended_at TEXT,
			PRIMARY KEY(run_id, session_id),
			FOREIGN KEY(run_id) REFERENCES schedule_runs(run_id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS schedule_fire_decisions (
			decision_id TEXT PRIMARY KEY,
			scope_type TEXT NOT NULL,
			scope_id TEXT NOT NULL,
			schedule_id TEXT NOT NULL,
			trigger_source TEXT NOT NULL,
			decision TEXT NOT NULL,
			reason TEXT NOT NULL DEFAULT '',
			run_id TEXT NOT NULL DEFAULT '',
			fired_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_schedule_fire_scope_time
			ON schedule_fire_decisions(scope_type, scope_id, schedule_id, fired_at DESC)`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize schedule state: %w", err)
		}
	}
	return nil
}

func (s *Store) BeginRun(ctx context.Context, run Run) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("schedule state store is unavailable")
	}
	if strings.TrimSpace(run.RunID) == "" || strings.TrimSpace(run.LockKey) == "" || strings.TrimSpace(run.ScheduleID) == "" {
		return fmt.Errorf("run_id, lock_key, and schedule_id are required")
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	run.StartedAt = run.StartedAt.UTC()
	if run.State == "" {
		run.State = StateStarting
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `INSERT INTO schedule_runs (
		run_id, scope_type, scope_id, lock_key, schedule_id, trigger_source,
		state, started_at, updated_at, active_session_id, run_folder, error_message
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunID, run.ScopeType, run.ScopeID, run.LockKey, run.ScheduleID, run.TriggerSource,
		run.State, formatTime(run.StartedAt), formatTime(run.StartedAt), run.ActiveSessionID, run.RunFolder, run.ErrorMessage)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return fmt.Errorf("%w: %s", ErrRunAlreadyActive, run.LockKey)
		}
		return fmt.Errorf("insert schedule run: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_run_events (run_id, from_state, to_state, reason, created_at)
		VALUES (?, '', ?, 'run claimed', ?)`, run.RunID, run.State, formatTime(run.StartedAt)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Transition(ctx context.Context, transition Transition) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("schedule state store is unavailable")
	}
	if transition.At.IsZero() {
		transition.At = time.Now().UTC()
	}
	transition.At = transition.At.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var current string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM schedule_runs WHERE run_id = ?`, transition.RunID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrRunNotFound, transition.RunID)
		}
		return err
	}
	from := State(current)
	if from == transition.To {
		return tx.Commit()
	}
	if IsTerminal(from) || !allowedTransitions[from][transition.To] {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, transition.To)
	}

	completedAt := ""
	if IsTerminal(transition.To) {
		completedAt = formatTime(transition.At)
	}
	_, err = tx.ExecContext(ctx, `UPDATE schedule_runs SET
		state = ?, updated_at = ?,
		completed_at = CASE WHEN ? <> '' THEN ? ELSE completed_at END,
		active_session_id = CASE WHEN ? <> '' THEN ? ELSE active_session_id END,
		run_folder = CASE WHEN ? <> '' THEN ? ELSE run_folder END,
		error_message = CASE WHEN ? <> '' THEN ? ELSE error_message END
		WHERE run_id = ?`,
		transition.To, formatTime(transition.At), completedAt, completedAt,
		transition.SessionID, transition.SessionID, transition.RunFolder, transition.RunFolder,
		transition.ErrorMessage, transition.ErrorMessage, transition.RunID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_run_events (run_id, from_state, to_state, reason, created_at)
		VALUES (?, ?, ?, ?, ?)`, transition.RunID, from, transition.To, transition.Reason, formatTime(transition.At)); err != nil {
		return err
	}
	if transition.SessionID != "" {
		sessionKind := transition.SessionKind
		if sessionKind == "" {
			sessionKind = "main"
		}
		sessionStatus := "running"
		var endedAt any
		if IsTerminal(transition.To) {
			sessionStatus = string(transition.To)
			endedAt = formatTime(transition.At)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO schedule_run_sessions (
			run_id, session_id, session_kind, status, started_at, ended_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, session_id) DO UPDATE SET
			session_kind = excluded.session_kind,
			status = excluded.status,
			ended_at = COALESCE(excluded.ended_at, schedule_run_sessions.ended_at)`,
			transition.RunID, transition.SessionID, sessionKind, sessionStatus, formatTime(transition.At), endedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ForceTerminal releases an active run lease after ordinary terminal
// transition retries fail. It never changes one terminal state into another.
func (s *Store) ForceTerminal(ctx context.Context, transition Transition) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("schedule state store is unavailable")
	}
	if !IsTerminal(transition.To) {
		return fmt.Errorf("forced transition must be terminal: %s", transition.To)
	}
	if transition.At.IsZero() {
		transition.At = time.Now().UTC()
	}
	transition.At = transition.At.UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var current string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM schedule_runs WHERE run_id = ?`, transition.RunID).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrRunNotFound, transition.RunID)
		}
		return err
	}
	from := State(current)
	if IsTerminal(from) {
		if from == transition.To {
			return tx.Commit()
		}
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, transition.To)
	}

	completedAt := formatTime(transition.At)
	if _, err := tx.ExecContext(ctx, `UPDATE schedule_runs SET
		state = ?, updated_at = ?, completed_at = ?,
		active_session_id = CASE WHEN ? <> '' THEN ? ELSE active_session_id END,
		run_folder = CASE WHEN ? <> '' THEN ? ELSE run_folder END,
		error_message = CASE WHEN ? <> '' THEN ? ELSE error_message END
		WHERE run_id = ?`,
		transition.To, completedAt, completedAt,
		transition.SessionID, transition.SessionID, transition.RunFolder, transition.RunFolder,
		transition.ErrorMessage, transition.ErrorMessage, transition.RunID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_run_events (run_id, from_state, to_state, reason, created_at)
		VALUES (?, ?, ?, ?, ?)`, transition.RunID, from, transition.To, "recovered terminal transition: "+transition.Reason, completedAt); err != nil {
		return err
	}
	if transition.SessionID != "" {
		sessionKind := transition.SessionKind
		if sessionKind == "" {
			sessionKind = "main"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_run_sessions (
			run_id, session_id, session_kind, status, started_at, ended_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, session_id) DO UPDATE SET
			session_kind = excluded.session_kind,
			status = excluded.status,
			ended_at = excluded.ended_at`, transition.RunID, transition.SessionID, sessionKind,
			transition.To, completedAt, completedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) RecordFireDecision(ctx context.Context, decision FireDecision) error {
	if decision.FiredAt.IsZero() {
		decision.FiredAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO schedule_fire_decisions (
		decision_id, scope_type, scope_id, schedule_id, trigger_source, decision, reason, run_id, fired_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, decision.DecisionID, decision.ScopeType, decision.ScopeID,
		decision.ScheduleID, decision.TriggerSource, decision.Decision, decision.Reason, decision.RunID, formatTime(decision.FiredAt)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM schedule_fire_decisions
		WHERE scope_type = ? AND scope_id = ? AND schedule_id = ?
		AND decision_id NOT IN (
			SELECT decision_id FROM schedule_fire_decisions
			WHERE scope_type = ? AND scope_id = ? AND schedule_id = ?
			ORDER BY fired_at DESC, decision_id DESC LIMIT ?
		)`, decision.ScopeType, decision.ScopeID, decision.ScheduleID,
		decision.ScopeType, decision.ScopeID, decision.ScheduleID, fireDecisionRetentionPerSchedule); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ListFireDecisions(ctx context.Context, scopeType, scopeID, scheduleID string, limit int) ([]FireDecision, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT decision_id, scope_type, scope_id, schedule_id,
		trigger_source, decision, reason, run_id, fired_at
		FROM schedule_fire_decisions
		WHERE scope_type = ? AND scope_id = ? AND schedule_id = ?
		ORDER BY fired_at DESC LIMIT ?`, scopeType, scopeID, scheduleID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var decisions []FireDecision
	for rows.Next() {
		var decision FireDecision
		var firedAt string
		if err := rows.Scan(&decision.DecisionID, &decision.ScopeType, &decision.ScopeID, &decision.ScheduleID,
			&decision.TriggerSource, &decision.Decision, &decision.Reason, &decision.RunID, &firedAt); err != nil {
			return nil, err
		}
		decision.FiredAt, err = parseTime(firedAt)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, decision)
	}
	return decisions, rows.Err()
}

func (s *Store) GetRun(ctx context.Context, runID string) (Run, error) {
	var run Run
	var startedAt, updatedAt, completedAt string
	err := s.db.QueryRowContext(ctx, `SELECT run_id, scope_type, scope_id, lock_key, schedule_id,
		trigger_source, state, started_at, updated_at, COALESCE(completed_at, ''), active_session_id,
		run_folder, error_message FROM schedule_runs WHERE run_id = ?`, runID).Scan(
		&run.RunID, &run.ScopeType, &run.ScopeID, &run.LockKey, &run.ScheduleID, &run.TriggerSource,
		&run.State, &startedAt, &updatedAt, &completedAt, &run.ActiveSessionID, &run.RunFolder, &run.ErrorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrRunNotFound
	}
	if err != nil {
		return Run{}, err
	}
	run.StartedAt, err = parseTime(startedAt)
	if err != nil {
		return Run{}, err
	}
	run.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Run{}, err
	}
	if completedAt != "" {
		parsed, parseErr := parseTime(completedAt)
		if parseErr != nil {
			return Run{}, parseErr
		}
		run.CompletedAt = &parsed
	}
	return run, nil
}

// ActiveRunByLockKey returns the run currently holding a schedule/workflow
// lease. The partial unique index guarantees that at most one row can match.
func (s *Store) ActiveRunByLockKey(ctx context.Context, lockKey string) (Run, error) {
	var runID string
	err := s.db.QueryRowContext(ctx, `SELECT run_id FROM schedule_runs
		WHERE lock_key = ?
		  AND state IN ('starting','workflow_running','workflow_finished','pulse_gate','pulse_modules','pulse_finalizing')
		LIMIT 1`, lockKey).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrRunNotFound
	}
	if err != nil {
		return Run{}, err
	}
	return s.GetRun(ctx, runID)
}

func (s *Store) ListEvents(ctx context.Context, runID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sequence, run_id, from_state, to_state, reason, created_at
		FROM schedule_run_events WHERE run_id = ? ORDER BY sequence`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var event Event
		var createdAt string
		if err := rows.Scan(&event.Sequence, &event.RunID, &event.FromState, &event.ToState, &event.Reason, &createdAt); err != nil {
			return nil, err
		}
		event.CreatedAt, err = parseTime(createdAt)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) InterruptActiveRuns(ctx context.Context, reason string, at time.Time) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id FROM schedule_runs
		WHERE state IN ('starting','workflow_running','workflow_finished','pulse_gate','pulse_modules','pulse_finalizing')`)
	if err != nil {
		return 0, err
	}
	var runIDs []string
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		runIDs = append(runIDs, runID)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, runID := range runIDs {
		if err := s.Transition(ctx, Transition{RunID: runID, To: StateInterrupted, Reason: reason, ErrorMessage: reason, At: at}); err != nil {
			return 0, err
		}
	}
	return len(runIDs), nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, value)
}
