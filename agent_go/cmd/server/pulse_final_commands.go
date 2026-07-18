package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	pulseFinalCommandDashboard = "dashboard"
	pulseFinalCommandBackup    = "backup"
	pulseFinalCommandPublish   = "publish"
	pulseFinalCommandNotify    = "notify"
)

var pulseFinalCommandOrder = []string{
	pulseFinalCommandDashboard,
	pulseFinalCommandBackup,
	pulseFinalCommandPublish,
	pulseFinalCommandNotify,
}

var validPulseFinalCommands = map[string]bool{
	pulseFinalCommandDashboard: true,
	pulseFinalCommandBackup:    true,
	pulseFinalCommandPublish:   true,
	pulseFinalCommandNotify:    true,
}

const pulseFinalCommandStateSchema = `CREATE TABLE IF NOT EXISTS pulse_final_command_state (
	workspace_path TEXT NOT NULL,
	command TEXT NOT NULL,
	pulse_run_id TEXT NOT NULL DEFAULT '',
	status TEXT NOT NULL DEFAULT '',
	reason TEXT NOT NULL DEFAULT '',
	started_at TEXT NOT NULL DEFAULT '',
	finished_at TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL,
	PRIMARY KEY (workspace_path, command)
)`

type PulseFinalCommandState struct {
	WorkspacePath string `json:"workspace_path"`
	Command       string `json:"command"`
	PulseRunID    string `json:"pulse_run_id,omitempty"`
	Status        string `json:"status,omitempty"`
	Reason        string `json:"reason,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

func initializePulseFinalCommandStates(ctx context.Context, workspacePath, pulseRunID string) error {
	pulseRunID = strings.TrimSpace(pulseRunID)
	if pulseRunID == "" {
		return fmt.Errorf("pulse_run_id is required")
	}
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, command := range pulseFinalCommandOrder {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pulse_final_command_state (
				workspace_path, command, pulse_run_id, status, reason, started_at, finished_at, updated_at
			) VALUES (?, ?, ?, 'waiting', 'Waiting for Pulse finalization', '', '', ?)
			ON CONFLICT(workspace_path, command) DO UPDATE SET
				pulse_run_id=excluded.pulse_run_id,
				status=excluded.status,
				reason=excluded.reason,
				started_at='',
				finished_at='',
				updated_at=excluded.updated_at`,
			normalized, command, pulseRunID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func markPulseFinalCommandState(ctx context.Context, workspacePath, command, pulseRunID, status, reason string) (*PulseFinalCommandState, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return markPulseFinalCommandStateInDB(ctx, db, normalized, command, pulseRunID, status, reason)
}

func markPulseFinalCommandStateFromAgent(ctx context.Context, workspacePath, command, pulseRunID, status, reason string) (*PulseFinalCommandState, error) {
	command = strings.TrimSpace(strings.ToLower(command))
	if !validPulseFinalCommands[command] {
		return nil, fmt.Errorf("final command %q is not valid", command)
	}
	pulseRunID = strings.TrimSpace(pulseRunID)
	status = strings.TrimSpace(strings.ToLower(status))
	switch status {
	case "running", "done", "skipped", "blocked", "failed":
	default:
		return nil, fmt.Errorf("status must be one of running, done, skipped, blocked, failed")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("reason is required")
	}

	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	existing, err := getPulseFinalCommandStateByCommand(ctx, db, normalized, command)
	if err != nil {
		return nil, fmt.Errorf("final command %q was not initialized for this Pulse run: %w", command, err)
	}
	if existing.PulseRunID != pulseRunID {
		return nil, fmt.Errorf("final command %q belongs to Pulse run %q, not %q", command, existing.PulseRunID, pulseRunID)
	}
	if existing.Status == status {
		return existing, nil
	}
	if isTerminalPulseFinalCommandStatus(existing.Status) {
		return nil, fmt.Errorf("final command %q is already terminal with status %q", command, existing.Status)
	}
	if status == "done" && existing.Status != "running" {
		return nil, fmt.Errorf("final command %q must be marked running before done", command)
	}

	commandIndex := -1
	for index, candidate := range pulseFinalCommandOrder {
		if candidate == command {
			commandIndex = index
			break
		}
	}
	if commandIndex < 0 {
		return nil, fmt.Errorf("final command %q has no configured order", command)
	}
	for _, prior := range pulseFinalCommandOrder[:commandIndex] {
		priorState, err := getPulseFinalCommandStateByCommand(ctx, db, normalized, prior)
		if err != nil || priorState.PulseRunID != pulseRunID || !isTerminalPulseFinalCommandStatus(priorState.Status) {
			return nil, fmt.Errorf("final command %q cannot start before %q is terminal", command, prior)
		}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	startedAt := existing.StartedAt
	finishedAt := ""
	if startedAt == "" {
		startedAt = now
	}
	if status != "running" {
		finishedAt = now
	}
	result, err := db.ExecContext(ctx, `UPDATE pulse_final_command_state SET
			status = ?, reason = ?, started_at = ?, finished_at = ?, updated_at = ?
		WHERE workspace_path = ? AND command = ? AND pulse_run_id = ? AND status = ?`,
		status, reason, startedAt, finishedAt, now,
		normalized, command, pulseRunID, existing.Status)
	if err != nil {
		return nil, err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if changed != 1 {
		return nil, fmt.Errorf("final command %q changed concurrently; refresh its state before retrying", command)
	}
	return getPulseFinalCommandStateByCommand(ctx, db, normalized, command)
}

func isTerminalPulseFinalCommandStatus(status string) bool {
	switch strings.TrimSpace(strings.ToLower(status)) {
	case "done", "skipped", "blocked", "failed", "timed_out":
		return true
	default:
		return false
	}
}

func markPulseFinalCommandStateInDB(ctx context.Context, db *sql.DB, workspacePath, command, pulseRunID, status, reason string) (*PulseFinalCommandState, error) {
	command = strings.TrimSpace(strings.ToLower(command))
	if !validPulseFinalCommands[command] {
		return nil, fmt.Errorf("final command %q is not valid", command)
	}
	pulseRunID = strings.TrimSpace(pulseRunID)
	if pulseRunID == "" {
		return nil, fmt.Errorf("pulse_run_id is required")
	}
	status = strings.TrimSpace(strings.ToLower(status))
	switch status {
	case "waiting", "running", "done", "skipped", "blocked", "failed", "timed_out":
	default:
		return nil, fmt.Errorf("status must be one of waiting, running, done, skipped, blocked, failed, timed_out")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("reason is required")
	}

	var existingRunID, startedAt string
	err := db.QueryRowContext(ctx, `SELECT pulse_run_id, started_at FROM pulse_final_command_state
		WHERE workspace_path = ? AND command = ?`, workspacePath, command).Scan(&existingRunID, &startedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existingRunID != pulseRunID {
		startedAt = ""
	}

	now := time.Now().UTC().Format(time.RFC3339)
	finishedAt := ""
	switch status {
	case "waiting":
		startedAt = ""
	case "running":
		if startedAt == "" {
			startedAt = now
		}
	default:
		if startedAt == "" {
			startedAt = now
		}
		finishedAt = now
	}

	_, err = db.ExecContext(ctx, `INSERT INTO pulse_final_command_state (
			workspace_path, command, pulse_run_id, status, reason, started_at, finished_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_path, command) DO UPDATE SET
			pulse_run_id=excluded.pulse_run_id,
			status=excluded.status,
			reason=excluded.reason,
			started_at=excluded.started_at,
			finished_at=excluded.finished_at,
			updated_at=excluded.updated_at`,
		workspacePath, command, pulseRunID, status, reason, startedAt, finishedAt, now)
	if err != nil {
		return nil, err
	}
	return getPulseFinalCommandStateByCommand(ctx, db, workspacePath, command)
}

func getPulseFinalCommandStates(ctx context.Context, workspacePath string) ([]PulseFinalCommandState, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil {
		return nil, err
	}
	if db == nil {
		return []PulseFinalCommandState{}, nil
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT workspace_path, command, pulse_run_id, status, reason,
		started_at, finished_at, updated_at FROM pulse_final_command_state WHERE workspace_path = ?`, normalized)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byCommand := map[string]PulseFinalCommandState{}
	for rows.Next() {
		state, err := scanPulseFinalCommandState(rows)
		if err != nil {
			return nil, err
		}
		byCommand[state.Command] = *state
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	states := make([]PulseFinalCommandState, 0, len(byCommand))
	for _, command := range pulseFinalCommandOrder {
		if state, ok := byCommand[command]; ok {
			states = append(states, state)
		}
	}
	return states, nil
}

func getPulseFinalCommandStateByCommand(ctx context.Context, db *sql.DB, workspacePath, command string) (*PulseFinalCommandState, error) {
	row := db.QueryRowContext(ctx, `SELECT workspace_path, command, pulse_run_id, status, reason,
		started_at, finished_at, updated_at FROM pulse_final_command_state
		WHERE workspace_path = ? AND command = ?`, workspacePath, command)
	return scanPulseFinalCommandState(row)
}

type pulseFinalCommandScanner interface {
	Scan(dest ...interface{}) error
}

func scanPulseFinalCommandState(row pulseFinalCommandScanner) (*PulseFinalCommandState, error) {
	var state PulseFinalCommandState
	if err := row.Scan(&state.WorkspacePath, &state.Command, &state.PulseRunID, &state.Status, &state.Reason,
		&state.StartedAt, &state.FinishedAt, &state.UpdatedAt); err != nil {
		return nil, err
	}
	return &state, nil
}

func finalizeUnresolvedPulseFinalCommands(ctx context.Context, workspacePath, pulseRunID, status, reason string) error {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT command, status FROM pulse_final_command_state
		WHERE workspace_path = ? AND pulse_run_id = ?`, normalized, strings.TrimSpace(pulseRunID))
	if err != nil {
		return err
	}
	var unresolved []string
	for rows.Next() {
		var command, currentStatus string
		if err := rows.Scan(&command, &currentStatus); err != nil {
			rows.Close()
			return err
		}
		if currentStatus == "waiting" || currentStatus == "running" {
			unresolved = append(unresolved, command)
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, command := range unresolved {
		if _, err := markPulseFinalCommandStateInDB(ctx, db, normalized, command, pulseRunID, status, reason); err != nil {
			return err
		}
	}
	return nil
}

func finalizeAllUnresolvedPulseFinalCommands(ctx context.Context, workspacePath, status, reason string) (int64, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, false)
	if err != nil || db == nil {
		return 0, err
	}
	defer db.Close()
	if err := ensurePulseModuleStateSchema(ctx, db); err != nil {
		return 0, err
	}
	status = strings.TrimSpace(strings.ToLower(status))
	if !isTerminalPulseFinalCommandStatus(status) {
		return 0, fmt.Errorf("cleanup status %q is not terminal", status)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return 0, fmt.Errorf("reason is required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.ExecContext(ctx, `UPDATE pulse_final_command_state SET
			status = ?, reason = ?,
			started_at = CASE WHEN started_at = '' THEN ? ELSE started_at END,
			finished_at = ?, updated_at = ?
		WHERE workspace_path = ? AND status IN ('waiting', 'running')`,
		status, reason, now, now, now, normalized)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
