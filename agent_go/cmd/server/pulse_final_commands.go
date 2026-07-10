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
	for _, command := range pulseFinalCommandOrder {
		if _, err := markPulseFinalCommandStateInDB(ctx, db, normalized, command, pulseRunID, "waiting", "Waiting for Pulse finalization"); err != nil {
			return err
		}
	}
	return nil
}

func markPulseFinalCommandState(ctx context.Context, workspacePath, command, pulseRunID, status, reason string) (*PulseFinalCommandState, error) {
	normalized, db, err := openPulseModuleStateDB(ctx, workspacePath, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return markPulseFinalCommandStateInDB(ctx, db, normalized, command, pulseRunID, status, reason)
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
