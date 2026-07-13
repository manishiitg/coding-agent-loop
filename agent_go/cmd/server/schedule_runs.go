package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
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
	Status      string     `json:"status"` // running, success, error, stopped, partial, interrupted
	Error       string     `json:"error,omitempty"`
	DurationMs  *int64     `json:"duration_ms,omitempty"`
	GroupNames  []string   `json:"group_names,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

const maxScheduleRuns = 200

var scheduleRunFileLocks sync.Map

func scheduleRunFileLock(path string) *sync.Mutex {
	lock, _ := scheduleRunFileLocks.LoadOrStore(path, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func scheduleRunsPath(workspacePath string) string {
	return workspacePath + "/schedule-runs.json"
}

// ReadScheduleRuns reads all run entries from <workspace>/schedule-runs.json.
func ReadScheduleRuns(ctx context.Context, workspacePath string) ([]ScheduleRunEntry, error) {
	path := scheduleRunsPath(workspacePath)
	lock := scheduleRunFileLock(path)
	lock.Lock()
	defer lock.Unlock()
	return readScheduleRunsUnlocked(ctx, workspacePath)
}

func readScheduleRunsUnlocked(ctx context.Context, workspacePath string) ([]ScheduleRunEntry, error) {
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
	path := scheduleRunsPath(workspacePath)
	lock := scheduleRunFileLock(path)
	lock.Lock()
	defer lock.Unlock()
	return writeScheduleRunsUnlocked(ctx, workspacePath, runs)
}

func writeScheduleRunsUnlocked(ctx context.Context, workspacePath string, runs []ScheduleRunEntry) error {
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal schedule runs: %w", err)
	}
	return writeFileToWorkspace(ctx, scheduleRunsPath(workspacePath), string(data))
}

// AppendScheduleRun adds a run entry, keeping at most maxScheduleRuns entries (trimming oldest).
func AppendScheduleRun(ctx context.Context, workspacePath string, run *ScheduleRunEntry) error {
	if run == nil {
		return fmt.Errorf("schedule run is required")
	}
	path := scheduleRunsPath(workspacePath)
	lock := scheduleRunFileLock(path)
	lock.Lock()
	defer lock.Unlock()

	runs, err := readScheduleRunsUnlocked(ctx, workspacePath)
	if err != nil {
		return err
	}

	runs = append([]ScheduleRunEntry{*run}, runs...) // prepend (newest first)

	if len(runs) > maxScheduleRuns {
		runs = runs[:maxScheduleRuns]
	}

	return writeScheduleRunsUnlocked(ctx, workspacePath, runs)
}

// UpdateScheduleRun finds a run by ID and updates its fields.
func UpdateScheduleRun(ctx context.Context, workspacePath string, runID string, status string, errMsg string, durationMs *int64, runFolder string, sessionID string) error {
	path := scheduleRunsPath(workspacePath)
	lock := scheduleRunFileLock(path)
	lock.Lock()
	defer lock.Unlock()

	runs, err := readScheduleRunsUnlocked(ctx, workspacePath)
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
			if isTerminalScheduleRunStatus(status) {
				now := time.Now().UTC()
				runs[i].CompletedAt = &now
			}
			return writeScheduleRunsUnlocked(ctx, workspacePath, runs)
		}
	}

	return fmt.Errorf("schedule run %q not found in %s", runID, path)
}

func isTerminalScheduleRunStatus(status string) bool {
	switch status {
	case "success", "error", "stopped", "partial", "failed", "interrupted":
		return true
	default:
		return false
	}
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
