package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MultiAgentScheduleFile is the top-level structure for _users/{userID}/multiagent-schedules.json.
type MultiAgentScheduleFile struct {
	Schedules    []WorkflowSchedule   `json:"schedules"`
	Capabilities WorkflowCapabilities `json:"capabilities"`
}

func multiAgentSchedulesPath(userID string) string {
	return "_users/" + userID + "/multiagent-schedules.json"
}

func multiAgentScheduleRunsPath(userID string) string {
	return "_users/" + userID + "/multiagent-schedule-runs.json"
}

// ReadMultiAgentSchedules reads the schedule file for a user.
// Returns (file, true, nil) if found, (nil, false, nil) if not found, (nil, false, error) on error.
func ReadMultiAgentSchedules(ctx context.Context, userID string) (*MultiAgentScheduleFile, bool, error) {
	content, exists, err := readFileFromWorkspace(ctx, multiAgentSchedulesPath(userID))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read multiagent-schedules.json for user %s: %w", userID, err)
	}
	if !exists {
		return &MultiAgentScheduleFile{
			Schedules:    []WorkflowSchedule{},
			Capabilities: WorkflowCapabilities{},
		}, false, nil
	}

	var f MultiAgentScheduleFile
	if err := json.Unmarshal([]byte(content), &f); err != nil {
		return nil, false, fmt.Errorf("failed to parse multiagent-schedules.json for user %s: %w", userID, err)
	}

	// Auto-assign IDs to schedules without one
	for i := range f.Schedules {
		if f.Schedules[i].ID == "" {
			f.Schedules[i].ID = uuid.New().String()
		}
		// Ensure mode is set
		if f.Schedules[i].Mode == "" {
			f.Schedules[i].Mode = "multi-agent"
		}
	}

	if f.Schedules == nil {
		f.Schedules = []WorkflowSchedule{}
	}

	return &f, true, nil
}

// WriteMultiAgentSchedules writes the schedule file for a user.
func WriteMultiAgentSchedules(ctx context.Context, userID string, f *MultiAgentScheduleFile) error {
	if f.Schedules == nil {
		f.Schedules = []WorkflowSchedule{}
	}

	// Validate schedules
	for i, sched := range f.Schedules {
		if sched.ID == "" {
			return fmt.Errorf("schedules[%d].id is required", i)
		}
		if sched.CronExpression == "" {
			return fmt.Errorf("schedules[%d].cron_expression is required", i)
		}
		if strings.TrimSpace(sched.Query) == "" {
			return fmt.Errorf("schedules[%d].query is required for multi-agent schedules", i)
		}
	}

	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal multiagent-schedules.json: %w", err)
	}

	return writeFileToWorkspace(ctx, multiAgentSchedulesPath(userID), string(data))
}

// DiscoverMultiAgentSchedules scans all _users/*/multiagent-schedules.json files.
type DiscoveredMultiAgentSchedules struct {
	UserID       string
	ScheduleFile *MultiAgentScheduleFile
}

func DiscoverMultiAgentSchedules(ctx context.Context) ([]DiscoveredMultiAgentSchedules, error) {
	userIDs, err := listWorkspaceChildFolderNames(ctx, "_users")
	if err != nil {
		return nil, nil
	}

	var results []DiscoveredMultiAgentSchedules
	for _, userID := range userIDs {
		f, exists, err := ReadMultiAgentSchedules(ctx, userID)
		if err != nil {
			scheduleLogf("[SCHEDULER] Warning: failed to read multi-agent schedules for user %s: %v", userID, err)
			continue
		}
		if !exists || len(f.Schedules) == 0 {
			continue
		}
		results = append(results, DiscoveredMultiAgentSchedules{
			UserID:       userID,
			ScheduleFile: f,
		})
	}

	return results, nil
}

// ReadMultiAgentScheduleRuns reads run history for a user's multi-agent schedules.
func ReadMultiAgentScheduleRuns(ctx context.Context, userID string) ([]ScheduleRunEntry, error) {
	path := multiAgentScheduleRunsPath(userID)
	lock := scheduleRunFileLock(path)
	lock.Lock()
	defer lock.Unlock()
	return readMultiAgentScheduleRunsUnlocked(ctx, userID)
}

func readMultiAgentScheduleRunsUnlocked(ctx context.Context, userID string) ([]ScheduleRunEntry, error) {
	content, exists, err := readFileFromWorkspace(ctx, multiAgentScheduleRunsPath(userID))
	if err != nil {
		return nil, fmt.Errorf("failed to read multiagent-schedule-runs.json: %w", err)
	}
	if !exists {
		return []ScheduleRunEntry{}, nil
	}

	var runs []ScheduleRunEntry
	if err := json.Unmarshal([]byte(content), &runs); err != nil {
		return nil, fmt.Errorf("failed to parse multiagent-schedule-runs.json: %w", err)
	}
	return runs, nil
}

// WriteMultiAgentScheduleRuns writes run history for a user's multi-agent schedules.
func WriteMultiAgentScheduleRuns(ctx context.Context, userID string, runs []ScheduleRunEntry) error {
	path := multiAgentScheduleRunsPath(userID)
	lock := scheduleRunFileLock(path)
	lock.Lock()
	defer lock.Unlock()
	return writeMultiAgentScheduleRunsUnlocked(ctx, userID, runs)
}

func writeMultiAgentScheduleRunsUnlocked(ctx context.Context, userID string, runs []ScheduleRunEntry) error {
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal multiagent-schedule-runs.json: %w", err)
	}
	return writeFileToWorkspace(ctx, multiAgentScheduleRunsPath(userID), string(data))
}

// AppendMultiAgentScheduleRun adds a run entry for a user's multi-agent schedule.
func AppendMultiAgentScheduleRun(ctx context.Context, userID string, run *ScheduleRunEntry) error {
	if run == nil {
		return fmt.Errorf("multi-agent schedule run is required")
	}
	path := multiAgentScheduleRunsPath(userID)
	lock := scheduleRunFileLock(path)
	lock.Lock()
	defer lock.Unlock()

	runs, err := readMultiAgentScheduleRunsUnlocked(ctx, userID)
	if err != nil {
		return err
	}

	runs = append([]ScheduleRunEntry{*run}, runs...) // prepend (newest first)

	if len(runs) > maxScheduleRuns {
		runs = runs[:maxScheduleRuns]
	}

	return writeMultiAgentScheduleRunsUnlocked(ctx, userID, runs)
}

// UpdateMultiAgentScheduleRun updates a run entry for a user's multi-agent schedule.
func UpdateMultiAgentScheduleRun(ctx context.Context, userID string, runID string, status string, errMsg string, durationMs *int64, sessionID string) error {
	path := multiAgentScheduleRunsPath(userID)
	lock := scheduleRunFileLock(path)
	lock.Lock()
	defer lock.Unlock()

	runs, err := readMultiAgentScheduleRunsUnlocked(ctx, userID)
	if err != nil {
		return err
	}

	for i := range runs {
		if runs[i].ID == runID {
			runs[i].Status = status
			runs[i].Error = errMsg
			runs[i].DurationMs = durationMs
			if sessionID != "" {
				runs[i].SessionID = sessionID
			}
			if isTerminalScheduleRunStatus(status) {
				now := time.Now().UTC()
				runs[i].CompletedAt = &now
			}
			return writeMultiAgentScheduleRunsUnlocked(ctx, userID, runs)
		}
	}

	return fmt.Errorf("multi-agent schedule run %q not found in %s", runID, path)
}

// ListMultiAgentScheduleRuns returns runs for a specific schedule ID with pagination.
func ListMultiAgentScheduleRuns(ctx context.Context, userID string, scheduleID string, limit, offset int) ([]ScheduleRunEntry, int, error) {
	allRuns, err := ReadMultiAgentScheduleRuns(ctx, userID)
	if err != nil {
		return nil, 0, err
	}

	var filtered []ScheduleRunEntry
	for _, r := range allRuns {
		if r.ScheduleID == scheduleID {
			filtered = append(filtered, r)
		}
	}

	total := len(filtered)
	if offset >= total {
		return []ScheduleRunEntry{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return filtered[offset:end], total, nil
}

// findMultiAgentScheduleByID scans all user directories for a multi-agent schedule.
// Returns (userID, scheduleFile, scheduleIndex, error).
func findMultiAgentScheduleByID(ctx context.Context, scheduleID string) (string, *MultiAgentScheduleFile, int, error) {
	userIDs, err := listWorkspaceChildFolderNames(ctx, "_users")
	if err != nil {
		return "", nil, 0, fmt.Errorf("cannot scan _users directory: %w", err)
	}

	for _, userID := range userIDs {
		f, exists, err := ReadMultiAgentSchedules(ctx, userID)
		if err != nil || !exists {
			continue
		}
		for i, sched := range f.Schedules {
			if sched.ID == scheduleID {
				return userID, f, i, nil
			}
		}
	}

	return "", nil, 0, fmt.Errorf("multi-agent schedule %s not found", scheduleID)
}
