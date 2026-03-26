package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// generateScheduleID creates a new UUID for a schedule entry.
func generateScheduleID() string {
	return uuid.New().String()
}

// ScheduleRunEntry represents a single scheduled job execution record.
// Stored in <workspace>/schedule-runs.json.
type ScheduleRunEntry struct {
	ID          string     `json:"id"`
	ScheduleID  string     `json:"schedule_id"`
	RunFolder   string     `json:"run_folder,omitempty"`
	SessionID   string     `json:"session_id,omitempty"`
	Status      string     `json:"status"` // running, success, error
	Error       string     `json:"error,omitempty"`
	DurationMs  *int64     `json:"duration_ms,omitempty"`
	GroupIDs    []string   `json:"group_ids,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

const maxScheduleRuns = 200

func scheduleRunsPath(workspacePath string) string {
	return workspacePath + "/schedule-runs.json"
}

// ReadScheduleRuns reads all run entries from <workspace>/schedule-runs.json.
func ReadScheduleRuns(ctx context.Context, workspacePath string) ([]ScheduleRunEntry, error) {
	content, exists, err := readFileFromWorkspace(ctx, scheduleRunsPath(workspacePath))
	if err != nil {
		return nil, fmt.Errorf("failed to read schedule-runs.json: %w", err)
	}
	if !exists {
		return []ScheduleRunEntry{}, nil
	}

	var runs []ScheduleRunEntry
	if err := json.Unmarshal([]byte(content), &runs); err != nil {
		return nil, fmt.Errorf("failed to parse schedule-runs.json: %w", err)
	}
	return runs, nil
}

// WriteScheduleRuns writes run entries to <workspace>/schedule-runs.json.
func WriteScheduleRuns(ctx context.Context, workspacePath string, runs []ScheduleRunEntry) error {
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schedule runs: %w", err)
	}
	return writeFileToWorkspace(ctx, scheduleRunsPath(workspacePath), string(data))
}

// AppendScheduleRun adds a run entry, keeping at most maxScheduleRuns entries (trimming oldest).
func AppendScheduleRun(ctx context.Context, workspacePath string, run *ScheduleRunEntry) error {
	runs, err := ReadScheduleRuns(ctx, workspacePath)
	if err != nil {
		// If file is corrupt, start fresh
		runs = []ScheduleRunEntry{}
	}

	runs = append([]ScheduleRunEntry{*run}, runs...) // prepend (newest first)

	if len(runs) > maxScheduleRuns {
		runs = runs[:maxScheduleRuns]
	}

	return WriteScheduleRuns(ctx, workspacePath, runs)
}

// UpdateScheduleRun finds a run by ID and updates its fields.
func UpdateScheduleRun(ctx context.Context, workspacePath string, runID string, status string, errMsg string, durationMs *int64, runFolder string, sessionID string) error {
	runs, err := ReadScheduleRuns(ctx, workspacePath)
	if err != nil {
		return err
	}

	for i := range runs {
		if runs[i].ID == runID {
			runs[i].Status = status
			runs[i].Error = errMsg
			runs[i].DurationMs = durationMs
			if runFolder != "" {
				runs[i].RunFolder = runFolder
			}
			if sessionID != "" {
				runs[i].SessionID = sessionID
			}
			if status == "success" || status == "error" {
				now := time.Now().UTC()
				runs[i].CompletedAt = &now
			}
			return WriteScheduleRuns(ctx, workspacePath, runs)
		}
	}

	return nil // run not found, ignore
}

// ListScheduleRuns returns runs for a specific schedule ID with pagination.
// Runs are returned newest-first.
func ListScheduleRuns(ctx context.Context, workspacePath string, scheduleID string, limit, offset int) ([]ScheduleRunEntry, int, error) {
	allRuns, err := ReadScheduleRuns(ctx, workspacePath)
	if err != nil {
		return nil, 0, err
	}

	// Filter by schedule ID
	var filtered []ScheduleRunEntry
	for _, r := range allRuns {
		if r.ScheduleID == scheduleID {
			filtered = append(filtered, r)
		}
	}

	// Sort newest first (should already be, but ensure)
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].StartedAt.After(filtered[j].StartedAt)
	})

	total := len(filtered)

	// Pagination
	if offset >= total {
		return []ScheduleRunEntry{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return filtered[offset:end], total, nil
}

// cleanupStaleRuns marks any "running" entries as "error" (interrupted by server restart).
func cleanupStaleRuns(ctx context.Context, workspacePath string) {
	runs, err := ReadScheduleRuns(ctx, workspacePath)
	if err != nil || len(runs) == 0 {
		return
	}

	changed := false
	for i := range runs {
		if runs[i].Status == "running" {
			runs[i].Status = "error"
			runs[i].Error = "interrupted by server restart"
			now := time.Now().UTC()
			runs[i].CompletedAt = &now
			changed = true
			scheduleLogf("[SCHEDULER] Cleaned up stale run %s (schedule %s) in %s", runs[i].ID, runs[i].ScheduleID, workspacePath)
		}
	}

	if changed {
		_ = WriteScheduleRuns(ctx, workspacePath, runs)
	}
}
