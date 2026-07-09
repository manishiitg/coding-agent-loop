package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/fsutil"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

const scheduledBackgroundNoPollingInstruction = "After launching background workflow or step work, do not babysit it with sleep/list_executions/query_step polling loops. Use at most one immediate query_step if you need to confirm the execution_id/status, then stop; [AUTO-NOTIFICATION] messages will resume the conversation when background work completes."

// ScheduleContext bundles everything needed to identify and execute a schedule.
type ScheduleContext struct {
	WorkspacePath string
	WorkflowID    string
	WorkflowLabel string
	Schedule      WorkflowSchedule
	Capabilities  WorkflowCapabilities
	UserID        string // Set for multi-agent schedules (derived from _users/{userID}/ path)
	SourceType    string // "workflow" or "multi-agent"
	TriggerSource string // "cron" (default) or "manual"; encoded into the session ID
}

// newScheduleSessionID mints the session ID for a scheduled run. Encoding the
// trigger source (cron vs. manual) and the schedule ID prefix makes it easy to
// tell, just from the builder/ filename, where a conversation originated.
func (s *SchedulerService) newScheduleSessionID(sctx *ScheduleContext) string {
	trigger := sctx.TriggerSource
	if trigger == "" {
		trigger = "cron"
	}
	idPrefix := sctx.Schedule.ID
	if len(idPrefix) > 8 {
		idPrefix = idPrefix[:8]
	}
	return fmt.Sprintf("schedule-%s--%s_%d", trigger, idPrefix, time.Now().UnixNano())
}

// ScheduleRuntimeState holds in-memory runtime state for a schedule (not persisted in manifest).
type ScheduleRuntimeState struct {
	LastStatus          string     `json:"last_status,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time `json:"next_run_at,omitempty"`
	LastSessionID       string     `json:"last_session_id,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	LastDurationMs      *int64     `json:"last_duration_ms,omitempty"`
	RunCount            int        `json:"run_count"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
}

// registeredJob is a schedule registered for wall-clock evaluation.
type registeredJob struct {
	sctx      *ScheduleContext
	cronSched cron.Schedule // nil for calendar (one-time) jobs
	runAt     *time.Time    // non-nil for calendar (one-time) jobs
	lastFired time.Time     // truncated to the minute — prevents double-fire in the same minute
}

// SchedulerService manages cron job execution using wall-clock polling.
// Every 60 seconds it evaluates each registered schedule's cron expression against
// the current wall-clock time. This approach is immune to macOS App Nap and sleep/wake
// issues that wedge monotonic-clock-based timers (like gocron).
type SchedulerService struct {
	api  *StreamingAPI
	mu   sync.Mutex
	jobs map[string]*registeredJob // scheduleID → job

	// In-memory runtime state per schedule (survives within server lifetime, reset on restart)
	runtimeStates   map[string]*ScheduleRuntimeState
	runtimeStatesMu sync.RWMutex

	// Schedule-to-workspace index for quick lookups
	workspaceIndex   map[string]string // scheduleID → workspacePath
	workspaceIndexMu sync.RWMutex

	// Schedule-to-user index for multi-agent schedules
	userIndex   map[string]string // scheduleID → userID
	userIndexMu sync.RWMutex

	workflowManifestCacheMu        sync.Mutex
	workflowManifestCacheExpiresAt time.Time
	workflowManifestCache          []DiscoveredWorkflow
}

func (s *SchedulerService) logf(sctx *ScheduleContext, format string, args ...interface{}) {
	scheduleLogfWithContext(scheduleLogContext(sctx), format, args...)
}

func (s *SchedulerService) sessionLogf(sctx *ScheduleContext, sessionID string, format string, args ...interface{}) {
	scheduleLogfWithContext(scheduleLogContext(sctx).WithSession(sessionID), format, args...)
}

// NewSchedulerService creates a new manifest-based SchedulerService.
func NewSchedulerService(api *StreamingAPI) *SchedulerService {
	return &SchedulerService{
		api:            api,
		jobs:           make(map[string]*registeredJob),
		runtimeStates:  make(map[string]*ScheduleRuntimeState),
		workspaceIndex: make(map[string]string),
		userIndex:      make(map[string]string),
	}
}

func (s *SchedulerService) DiscoverWorkflowManifestsCached(ctx context.Context, ttl time.Duration) ([]DiscoveredWorkflow, error) {
	now := time.Now()

	s.workflowManifestCacheMu.Lock()
	if ttl > 0 && now.Before(s.workflowManifestCacheExpiresAt) && s.workflowManifestCache != nil {
		cached := append([]DiscoveredWorkflow(nil), s.workflowManifestCache...)
		s.workflowManifestCacheMu.Unlock()
		return cached, nil
	}
	s.workflowManifestCacheMu.Unlock()

	discovered, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		return nil, err
	}

	s.workflowManifestCacheMu.Lock()
	s.workflowManifestCache = append([]DiscoveredWorkflow(nil), discovered...)
	if ttl > 0 {
		s.workflowManifestCacheExpiresAt = now.Add(ttl)
	} else {
		s.workflowManifestCacheExpiresAt = time.Time{}
	}
	s.workflowManifestCacheMu.Unlock()

	return discovered, nil
}

func (s *SchedulerService) InvalidateWorkflowManifestCache() {
	s.workflowManifestCacheMu.Lock()
	s.workflowManifestCache = nil
	s.workflowManifestCacheExpiresAt = time.Time{}
	s.workflowManifestCacheMu.Unlock()
}

// Start scans all workspace folders for workflow.json manifests, loads enabled schedules,
// and starts the wall-clock tick loop.
func (s *SchedulerService) Start(ctx context.Context) error {
	scheduleLogf("[SCHEDULER] Starting manifest-based scheduler service...")

	// Discover all workflows by scanning workspace-docs/Workflow/*/workflow.json
	workflows := s.discoverWorkflows(ctx)
	scheduleLogf("[SCHEDULER] Discovered %d workflows with manifests", len(workflows))

	// Mark any stale "running" runs as "error" — they were interrupted by a server restart
	for _, wf := range workflows {
		runs, err := ReadScheduleRuns(ctx, wf.WorkspacePath)
		if err != nil {
			continue
		}
		fixed := 0
		for i := range runs {
			if runs[i].Status == "running" {
				runs[i].Status = "error"
				runs[i].Error = "interrupted: server restarted"
				fixed++
			}
		}
		if fixed > 0 {
			_ = WriteScheduleRuns(ctx, wf.WorkspacePath, runs)
			scheduleLogf("[SCHEDULER] Marked %d stale running run(s) as error in %s", fixed, wf.WorkspacePath)
		}
	}

	loaded := 0
	for _, wf := range workflows {
		for _, sched := range wf.Manifest.Schedules {
			sctx := buildScheduleContext(wf.WorkspacePath, wf.Manifest, sched)
			if err := s.LoadSchedule(sctx); err != nil {
				scheduleLogf("[SCHEDULER] Failed to load schedule %s (%s): %v", sched.ID, sched.Name, err)
			} else if sched.Enabled {
				loaded++
			}
		}
	}

	// Discover multi-agent schedules from _users/*/multiagent-schedules.json
	maScheds, err := DiscoverMultiAgentSchedules(ctx)
	if err != nil {
		scheduleLogf("[SCHEDULER] Warning: failed to discover multi-agent schedules: %v", err)
	} else {
		scheduleLogf("[SCHEDULER] Discovered %d users with multi-agent schedules", len(maScheds))

		// Mark stale runs
		for _, ma := range maScheds {
			runs, err := ReadMultiAgentScheduleRuns(ctx, ma.UserID)
			if err != nil {
				continue
			}
			fixed := 0
			for i := range runs {
				if runs[i].Status == "running" {
					runs[i].Status = "error"
					runs[i].Error = "interrupted: server restarted"
					fixed++
				}
			}
			if fixed > 0 {
				_ = WriteMultiAgentScheduleRuns(ctx, ma.UserID, runs)
				scheduleLogf("[SCHEDULER] Marked %d stale multi-agent run(s) as error for user %s", fixed, ma.UserID)
			}
		}

		for _, ma := range maScheds {
			for _, sched := range MergeBuiltinSchedules(ma.ScheduleFile.Schedules) {
				sctx := buildMultiAgentScheduleContext(ma.UserID, sched, ma.ScheduleFile.Capabilities)
				if err := s.LoadSchedule(sctx); err != nil {
					scheduleLogf("[SCHEDULER] Failed to load multi-agent schedule %s (%s) for user %s: %v", sched.ID, sched.Name, ma.UserID, err)
				} else if sched.Enabled {
					loaded++
				}
			}
		}
	}

	scheduleLogf("[SCHEDULER] ✅ Started with %d schedules. Server time: %s, timezone: %s",
		loaded, time.Now().Format(time.RFC3339), time.Now().Location().String())

	// Periodically rescan multi-agent schedule files for changes (written by agents via shell)
	go s.multiAgentRescanLoop(ctx)

	// Wall-clock tick loop: every 60s, evaluate all registered schedules against current time.
	go s.tickLoop(ctx)

	// Wait for context cancellation
	<-ctx.Done()
	scheduleLogf("[SCHEDULER] Shutting down (context canceled)")
	return nil
}

// multiAgentRescanLoop periodically checks for new/changed multi-agent schedule files.
// Agents write these files directly via shell commands, so we need to rescan to pick up changes.
func (s *SchedulerService) multiAgentRescanLoop(ctx context.Context) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.rescanMultiAgentSchedules(ctx)
		}
	}
}

// tickLoop is the wall-clock scheduler. Every 60 seconds it evaluates each
// registered schedule against the current wall-clock time and fires any that
// are due. Unlike timer-based schedulers (gocron), this approach is immune to
// macOS App Nap and sleep/wake monotonic clock drift — if a job was missed
// during sleep, it fires on the first tick after wake.
func (s *SchedulerService) tickLoop(ctx context.Context) {
	const interval = 60 * time.Second
	const wakeThreshold = 90 * time.Second

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	lastTick := time.Now()

	for {
		select {
		case <-ctx.Done():
			return
		case t := <-ticker.C:
			gap := t.Sub(lastTick)
			if gap > wakeThreshold {
				scheduleLogf("[SCHEDULER] 💤 WAKE_DETECTED gap=%s now=%s prev_tick=%s",
					gap.Round(time.Second), t.Format(time.RFC3339), lastTick.Format(time.RFC3339))
			}

			s.mu.Lock()
			parts := make([]string, 0, len(s.jobs))
			var toFire []*registeredJob
			for sid, job := range s.jobs {
				if job.cronSched != nil {
					next := job.cronSched.Next(job.lastFired)
					if !next.After(t) {
						toFire = append(toFire, job)
					}
					parts = append(parts, fmt.Sprintf("%s next=%s", sid, job.cronSched.Next(t).UTC().Format(time.RFC3339)))
				} else if job.runAt != nil {
					if !job.runAt.After(t) && job.lastFired.Before(*job.runAt) {
						toFire = append(toFire, job)
					}
					parts = append(parts, fmt.Sprintf("%s at=%s", sid, job.runAt.UTC().Format(time.RFC3339)))
				}
			}
			s.mu.Unlock()

			scheduleLogf("[SCHEDULER] ❤️ heartbeat now=%s gap=%s jobs=%d due=%d | %s",
				t.Format(time.RFC3339), gap.Round(time.Second), len(parts), len(toFire), strings.Join(parts, ", "))

			for _, job := range toFire {
				job.lastFired = t.Truncate(time.Minute)
				go s.triggerSchedule(job.sctx)
			}

			lastTick = t
		}
	}
}

// rescanMultiAgentSchedules discovers multi-agent schedules and loads/unloads as needed.
func (s *SchedulerService) rescanMultiAgentSchedules(ctx context.Context) {
	maScheds, err := DiscoverMultiAgentSchedules(ctx)
	if err != nil {
		return
	}

	// Build set of all discovered schedule IDs
	discovered := make(map[string]bool)

	for _, ma := range maScheds {
		for _, sched := range MergeBuiltinSchedules(ma.ScheduleFile.Schedules) {
			discovered[sched.ID] = true

			// Check if already loaded with same enabled state
			s.mu.Lock()
			_, isLoaded := s.jobs[sched.ID]
			s.mu.Unlock()

			if sched.Enabled && !isLoaded {
				// New or re-enabled schedule
				sctx := buildMultiAgentScheduleContext(ma.UserID, sched, ma.ScheduleFile.Capabilities)
				if err := s.LoadSchedule(sctx); err != nil {
					scheduleLogf("[SCHEDULER] Rescan: failed to load multi-agent schedule %s: %v", sched.ID, err)
				} else {
					scheduleLogf("[SCHEDULER] Rescan: loaded new multi-agent schedule %s (%s) for user %s", sched.ID, sched.Name, ma.UserID)
				}
			} else if !sched.Enabled && isLoaded {
				// Disabled — remove
				_ = s.RemoveJob(sched.ID)
				scheduleLogf("[SCHEDULER] Rescan: removed disabled multi-agent schedule %s", sched.ID)
			}
		}
	}

	// Remove schedules that were deleted from files
	s.userIndexMu.RLock()
	toRemove := []string{}
	for schedID := range s.userIndex {
		if !discovered[schedID] {
			toRemove = append(toRemove, schedID)
		}
	}
	s.userIndexMu.RUnlock()

	for _, schedID := range toRemove {
		_ = s.RemoveJob(schedID)
		scheduleLogf("[SCHEDULER] Rescan: removed deleted multi-agent schedule %s", schedID)
	}
}

// discoveredWorkflow holds a manifest + its workspace path.
type discoveredWorkflow struct {
	WorkspacePath string
	Manifest      *WorkflowManifest
}

// discoverWorkflows scans workspace-docs/Workflow/ for workflow.json files.
func (s *SchedulerService) discoverWorkflows(ctx context.Context) []discoveredWorkflow {
	var results []discoveredWorkflow

	discovered, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		scheduleLogf("[SCHEDULER] Cannot scan workflow directory: %v", err)
		return nil
	}

	for _, item := range discovered {
		if len(item.Manifest.Schedules) > 0 {
			results = append(results, discoveredWorkflow{
				WorkspacePath: item.WorkspacePath,
				Manifest:      item.Manifest,
			})
		}
	}

	return results
}

// buildScheduleContext creates a ScheduleContext from a manifest and schedule.
func buildScheduleContext(workspacePath string, manifest *WorkflowManifest, sched WorkflowSchedule) *ScheduleContext {
	return &ScheduleContext{
		WorkspacePath: workspacePath,
		WorkflowID:    manifest.ID,
		WorkflowLabel: manifest.Label,
		Schedule:      sched,
		Capabilities:  manifest.Capabilities,
		SourceType:    "workflow",
	}
}

// buildMultiAgentScheduleContext creates a ScheduleContext for a multi-agent schedule.
func buildMultiAgentScheduleContext(userID string, sched WorkflowSchedule, caps WorkflowCapabilities) *ScheduleContext {
	return &ScheduleContext{
		WorkspacePath: "_users/" + userID,
		UserID:        userID,
		Schedule:      sched,
		Capabilities:  caps,
		SourceType:    "multi-agent",
	}
}

// Stop shuts down the scheduler.
func (s *SchedulerService) Stop() {
	s.mu.Lock()
	s.jobs = make(map[string]*registeredJob)
	s.mu.Unlock()
	scheduleLogf("[SCHEDULER] Stopped")
}

// LoadSchedule registers a schedule for wall-clock evaluation from a ScheduleContext.
func (s *SchedulerService) LoadSchedule(sctx *ScheduleContext) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sched := sctx.Schedule

	// Remove existing registration if any.
	delete(s.jobs, sched.ID)

	// Update workspace index
	s.workspaceIndexMu.Lock()
	s.workspaceIndex[sched.ID] = sctx.WorkspacePath
	s.workspaceIndexMu.Unlock()

	if sctx.SourceType == "workflow" {
		if err := EnsureWorkflowScheduleExecutionTracker(context.Background(), sctx.WorkspacePath, sched, time.Now().UTC()); err != nil {
			s.logf(sctx, "[SCHEDULER] Warning: failed to initialize execution history for %s: %v", sched.ID, err)
		}
	}

	// Update user index for multi-agent schedules
	if sctx.UserID != "" {
		s.userIndexMu.Lock()
		s.userIndex[sched.ID] = sctx.UserID
		s.userIndexMu.Unlock()
	}

	if !sched.Enabled {
		return nil
	}

	scheduleType := scheduleTypeOrDefault(sched.ScheduleType)
	var nextRun *time.Time
	sctxCopy := *sctx

	if scheduleType == "calendar" {
		// Calendar schedules: register one job per future calendar item.
		for _, item := range sched.CalendarItems {
			runAt, err := calendarItemRunTime(sched, item)
			if err != nil || !runAt.After(time.Now().UTC()) {
				continue
			}
			if nextRun == nil || runAt.Before(*nextRun) {
				runAtCopy := runAt
				nextRun = &runAtCopy
			}
			itemCopy := item
			itemSctx := sctxCopy
			itemSctx.Schedule = scheduleWithCalendarItem(sched, itemCopy)
			calID := fmt.Sprintf("%s__cal__%s_%s", sched.ID, item.Date, item.Time)
			s.jobs[calID] = &registeredJob{
				sctx:  &itemSctx,
				runAt: &runAt,
			}
		}
		if nextRun == nil {
			s.logf(sctx, "[SCHEDULER] Calendar schedule %s (%s) has no future items; not registering", sched.ID, sched.Name)
		}
	} else {
		// Cron schedules: parse the expression and register for wall-clock eval.
		loc, err := time.LoadLocation(scheduleTimezoneOrDefault(sched.Timezone))
		if err != nil {
			loc = time.UTC
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		cronSched, err := parser.Parse(sched.CronExpression)
		if err != nil {
			return fmt.Errorf("failed to parse cron expression %q: %w", sched.CronExpression, err)
		}
		// Wrap with timezone-aware location
		cronSched = &tzSchedule{inner: cronSched, loc: loc}

		s.jobs[sched.ID] = &registeredJob{
			sctx:      &sctxCopy,
			cronSched: cronSched,
			lastFired: time.Now().Add(-30 * time.Second), // don't fire immediately on registration
		}
		nextRun = getNextRunTime(sched.CronExpression, sched.Timezone)
	}

	// Initialize runtime state with next run
	state := s.getOrCreateRuntimeState(sched.ID)
	state.NextRunAt = nextRun

	nextRunStr := "unknown"
	if nextRun != nil {
		nextRunStr = nextRun.Format(time.RFC3339)
	}
	s.logf(sctx, "[SCHEDULER] Registered schedule %s (%s) type=%s cron=%q timezone=%s next_run=%s",
		sched.ID, sched.Name, scheduleType, sched.CronExpression, sched.Timezone, nextRunStr)
	return nil
}

// tzSchedule wraps a cron.Schedule to evaluate in a specific timezone.
type tzSchedule struct {
	inner cron.Schedule
	loc   *time.Location
}

func (tz *tzSchedule) Next(t time.Time) time.Time {
	return tz.inner.Next(t.In(tz.loc))
}

// ReloadSchedule reloads a schedule from its manifest after it's been updated.
func (s *SchedulerService) ReloadSchedule(ctx context.Context, workspacePath string, scheduleID string) error {
	manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil || !found {
		return fmt.Errorf("failed to read manifest from %s: %w", workspacePath, err)
	}

	// Find the schedule
	for _, sched := range manifest.Schedules {
		if sched.ID == scheduleID {
			return s.LoadSchedule(buildScheduleContext(workspacePath, manifest, sched))
		}
	}

	// Schedule not found — remove it
	return s.RemoveJob(scheduleID)
}

// ReloadMultiAgentSchedule reloads a multi-agent schedule after it's been updated.
func (s *SchedulerService) ReloadMultiAgentSchedule(ctx context.Context, userID string, scheduleID string) error {
	f, exists, err := ReadMultiAgentSchedules(ctx, userID)
	if err != nil || !exists {
		return s.RemoveJob(scheduleID)
	}

	for _, sched := range MergeBuiltinSchedules(f.Schedules) {
		if sched.ID == scheduleID {
			return s.LoadSchedule(buildMultiAgentScheduleContext(userID, sched, f.Capabilities))
		}
	}

	return s.RemoveJob(scheduleID)
}

// RemoveJob removes a schedule from the tick loop.
func (s *SchedulerService) RemoveJob(id string) error {
	s.mu.Lock()
	delete(s.jobs, id)
	// Also remove calendar sub-jobs (keyed as id__cal__date_time)
	prefix := id + "__cal__"
	for k := range s.jobs {
		if strings.HasPrefix(k, prefix) {
			delete(s.jobs, k)
		}
	}
	s.mu.Unlock()

	s.workspaceIndexMu.Lock()
	delete(s.workspaceIndex, id)
	s.workspaceIndexMu.Unlock()

	s.userIndexMu.Lock()
	delete(s.userIndex, id)
	s.userIndexMu.Unlock()

	return nil
}

// GetRuntimeState returns the in-memory runtime state for a schedule.
func (s *SchedulerService) GetRuntimeState(scheduleID string) ScheduleRuntimeState {
	s.runtimeStatesMu.RLock()
	var merged ScheduleRuntimeState
	if state, ok := s.runtimeStates[scheduleID]; ok {
		merged = *state
	}
	s.runtimeStatesMu.RUnlock()

	if userID := s.GetUserForSchedule(scheduleID); userID != "" {
		runs, err := ReadMultiAgentScheduleRuns(context.Background(), userID)
		if err == nil {
			return mergeRuntimeStateWithRuns(merged, scheduleID, runs)
		}
		return merged
	}

	if workspacePath := s.GetWorkspaceForSchedule(scheduleID); workspacePath != "" {
		_ = s.reconcileWorkflowScheduleRuns(context.Background(), workspacePath, scheduleID)
		runs, err := ReadScheduleRuns(context.Background(), workspacePath)
		if err == nil {
			return mergeRuntimeStateWithRuns(merged, scheduleID, runs)
		}
	}

	return merged
}

func (s *SchedulerService) reconcileWorkflowScheduleRuns(ctx context.Context, workspacePath, scheduleID string) error {
	if s == nil || s.api == nil || strings.TrimSpace(workspacePath) == "" {
		return nil
	}

	runs, err := ReadScheduleRuns(ctx, workspacePath)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	changed := false
	for i := range runs {
		if runs[i].Status != "running" {
			continue
		}
		if scheduleID != "" && runs[i].ScheduleID != scheduleID {
			continue
		}

		status, errMsg, shouldFinalize := s.reconciledScheduleRunStatus(&runs[i], now)
		if !shouldFinalize {
			continue
		}

		runs[i].Status = status
		runs[i].Error = errMsg
		durationMs := now.Sub(runs[i].StartedAt).Milliseconds()
		if durationMs < 0 {
			durationMs = 0
		}
		runs[i].DurationMs = &durationMs
		runs[i].CompletedAt = &now
		changed = true
	}

	if !changed {
		return nil
	}
	return WriteScheduleRuns(ctx, workspacePath, runs)
}

func (s *SchedulerService) reconciledScheduleRunStatus(run *ScheduleRunEntry, now time.Time) (string, string, bool) {
	if run == nil {
		return "", "", false
	}

	if strings.TrimSpace(run.SessionID) == "" {
		if now.Sub(run.StartedAt) > 10*time.Minute {
			return "error", "interrupted: no session id recorded", true
		}
		return "", "", false
	}

	session, exists := s.api.getActiveSession(run.SessionID)
	if !exists {
		return "error", "interrupted: session not active", true
	}

	switch session.Status {
	case "running":
		return "", "", false
	case "completed":
		return "success", "", true
	case "error", "stopped", "inactive", "dismissed":
		return "error", fmt.Sprintf("session ended with status %s", session.Status), true
	default:
		return "", "", false
	}
}

func mergeRuntimeStateWithRuns(state ScheduleRuntimeState, scheduleID string, runs []ScheduleRunEntry) ScheduleRuntimeState {
	var filtered []ScheduleRunEntry
	for _, run := range runs {
		if run.ScheduleID == scheduleID {
			filtered = append(filtered, run)
		}
	}
	if len(filtered) == 0 {
		return state
	}

	latest := filtered[0]
	if state.RunCount < len(filtered) {
		state.RunCount = len(filtered)
	}

	shouldAdoptLatest := state.LastRunAt == nil || latest.StartedAt.After(*state.LastRunAt)
	sameRun := state.LastRunAt != nil && latest.StartedAt.Equal(*state.LastRunAt)

	if shouldAdoptLatest {
		startedAt := latest.StartedAt
		state.LastRunAt = &startedAt
		state.LastStatus = latest.Status
		state.LastSessionID = latest.SessionID
		state.LastError = latest.Error
		state.LastDurationMs = latest.DurationMs
		return state
	}

	if sameRun {
		if state.LastStatus == "" {
			state.LastStatus = latest.Status
		}
		if state.LastSessionID == "" {
			state.LastSessionID = latest.SessionID
		}
		if state.LastError == "" {
			state.LastError = latest.Error
		}
		if state.LastDurationMs == nil {
			state.LastDurationMs = latest.DurationMs
		}
	}

	return state
}

// GetWorkspaceForSchedule returns the workspace path for a schedule ID.
func (s *SchedulerService) GetWorkspaceForSchedule(scheduleID string) string {
	s.workspaceIndexMu.RLock()
	defer s.workspaceIndexMu.RUnlock()
	return s.workspaceIndex[scheduleID]
}

// GetUserForSchedule returns the user ID for a multi-agent schedule ID.
func (s *SchedulerService) GetUserForSchedule(scheduleID string) string {
	s.userIndexMu.RLock()
	defer s.userIndexMu.RUnlock()
	return s.userIndex[scheduleID]
}

// TriggerNow triggers a schedule immediately (for manual trigger API).
func (s *SchedulerService) TriggerNow(workspacePath string, scheduleID string) (string, error) {
	ctx := context.Background()

	manifest, found, err := ReadWorkflowManifest(ctx, workspacePath)
	if err != nil || !found {
		return "", fmt.Errorf("failed to read manifest from %s: %w", workspacePath, err)
	}

	var sched *WorkflowSchedule
	for i := range manifest.Schedules {
		if manifest.Schedules[i].ID == scheduleID {
			sched = &manifest.Schedules[i]
			break
		}
	}
	if sched == nil {
		return "", fmt.Errorf("schedule %s not found in manifest at %s", scheduleID, workspacePath)
	}

	// Match the cron-fired path: do not start a manual trigger when this workflow
	// workspace already has an active execution, regardless of whether that run was
	// started manually, by workflow builder, or by another schedule.
	if activeExec := s.findActiveExecutionForWorkspace(workspacePath); activeExec != nil {
		triggeredBy := activeExec.TriggeredBy
		if strings.TrimSpace(triggeredBy) == "" {
			triggeredBy = "unknown"
		}
		return "", fmt.Errorf(
			"workflow already has an active %s run (session: %s)",
			triggeredBy,
			activeExec.SessionID,
		)
	}

	// Prevent concurrent runs — check and mark atomically under the write lock
	startTime := time.Now().UTC()
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(scheduleID)
	if state.LastStatus == "running" {
		s.runtimeStatesMu.Unlock()
		return "", fmt.Errorf("job is already running (session: %s)", state.LastSessionID)
	}
	state.LastStatus = "running"
	state.LastRunAt = &startTime
	state.LastError = ""
	s.runtimeStatesMu.Unlock()

	sctx := buildScheduleContext(workspacePath, manifest, *sched)
	sctx.TriggerSource = "manual"

	if err := RecordWorkflowScheduleExecution(context.Background(), workspacePath, *sched, startTime); err != nil {
		s.logf(sctx, "[SCHEDULER] Warning: failed to record manual schedule execution for %s: %v", scheduleID, err)
	}

	go func() {
		if _, err := s.runJob(context.Background(), sctx); err != nil {
			scheduleLogf("[SCHEDULER] Triggered job %s failed: %v", scheduleID, err)
		}
	}()

	return "triggered", nil
}

// TriggerMultiAgentNow triggers a multi-agent schedule immediately.
func (s *SchedulerService) TriggerMultiAgentNow(userID string, scheduleID string) (string, error) {
	ctx := context.Background()

	f, exists, err := ReadMultiAgentSchedules(ctx, userID)
	if err != nil || !exists {
		return "", fmt.Errorf("failed to read multi-agent schedules for user %s: %w", userID, err)
	}

	var sched *WorkflowSchedule
	schedules := MergeBuiltinSchedules(f.Schedules)
	for i := range schedules {
		if schedules[i].ID == scheduleID {
			sched = &schedules[i]
			break
		}
	}
	if sched == nil {
		return "", fmt.Errorf("multi-agent schedule %s not found for user %s", scheduleID, userID)
	}

	startTime := time.Now().UTC()
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(scheduleID)
	if state.LastStatus == "running" {
		s.runtimeStatesMu.Unlock()
		return "", fmt.Errorf("job is already running (session: %s)", state.LastSessionID)
	}
	state.LastStatus = "running"
	state.LastRunAt = &startTime
	state.LastError = ""
	s.runtimeStatesMu.Unlock()

	sctx := buildMultiAgentScheduleContext(userID, *sched, f.Capabilities)
	sctx.TriggerSource = "manual"

	go func() {
		if _, err := s.runJob(context.Background(), sctx); err != nil {
			scheduleLogf("[SCHEDULER] Triggered multi-agent job %s failed: %v", scheduleID, err)
		}
	}()

	return "triggered", nil
}

// StopRunningJob stops a running scheduled job by canceling its session.
func (s *SchedulerService) StopRunningJob(scheduleID string) {
	state := s.GetRuntimeState(scheduleID)
	sessionID := state.LastSessionID
	if sessionID == "" {
		return
	}

	scheduleLogf("[SCHEDULER] Stopping running job %s (session: %s)", scheduleID, sessionID)

	// Cancel agent execution context
	s.api.agentCancelMux.Lock()
	if cancelFunc, exists := s.api.agentCancelFuncs[sessionID]; exists {
		cancelFunc()
		delete(s.api.agentCancelFuncs, sessionID)
	}
	s.api.agentCancelMux.Unlock()

	// Cancel workflow orchestrator contexts
	s.api.sessionQueryIDMux.Lock()
	queryIDs := s.api.sessionQueryIDs[sessionID]
	delete(s.api.sessionQueryIDs, sessionID)
	s.api.sessionQueryIDMux.Unlock()

	if len(queryIDs) > 0 {
		s.api.workflowOrchestratorContextMux.Lock()
		for _, qid := range queryIDs {
			if cancelFunc, exists := s.api.workflowOrchestratorContexts[qid]; exists {
				cancelFunc()
				delete(s.api.workflowOrchestratorContexts, qid)
			}
		}
		s.api.workflowOrchestratorContextMux.Unlock()
	}

	// Cancel background agents
	s.api.bgAgentRegistry.CancelAll(sessionID)

	// Close tmux-backed coding-CLI sessions. Canceling the Go contexts above tears
	// down the streaming/orchestration server-side, but the CLI processes inside
	// the tmux panes (the scheduled run's main agent + any step sub-agents) keep
	// running their current turn until they finish naturally — so the job showed
	// "stopped" in the UI while the main agent kept going. Mirror handleStopSession:
	// gracefully close the main-agent session by owner, then enumerate this
	// session's terminals and tear down each sub-agent pane by name (a scheduled
	// stop is a hard, full stop — always cancel sub-agents).
	const closeReason = "scheduled job stopped by user"
	closeAllCodingCLIInteractiveSessionsForOwner(sessionID, closeReason)
	if s.api.terminalStore != nil {
		mainOwner := "main:" + sessionID
		for _, snap := range s.api.terminalStore.List(sessionID) {
			owner := strings.TrimSpace(snap.OwnerID)
			tmux := strings.TrimSpace(snap.TmuxSession)
			if tmux == "" || owner == sessionID || owner == mainOwner {
				continue // no pane, or main agent already handled above
			}
			if handled := gracefulCloseCodingCLITmuxByName(tmux, closeReason); !handled {
				killCtx, killCancel := context.WithTimeout(context.Background(), terminalTmuxActionTimeout)
				if err := runTerminalTmuxCommand(killCtx, "", "kill-session", "-t", tmux); err != nil {
					scheduleLogf("[SCHEDULER] kill-session %q (owner %s) failed (may already be gone): %v", tmux, owner, err)
				}
				killCancel()
			}
			if snap.Active {
				s.api.terminalStore.MarkFailed(snap.TerminalID)
			}
		}
	}

	// Update runtime state
	s.runtimeStatesMu.Lock()
	if st, ok := s.runtimeStates[scheduleID]; ok {
		st.LastStatus = "stopped"
	}
	s.runtimeStatesMu.Unlock()

	scheduleLogf("[SCHEDULER] Stopped job %s (session: %s)", scheduleID, sessionID)
}

// triggerSchedule is called by the tick loop when a schedule is due.
func (s *SchedulerService) triggerSchedule(sctx *ScheduleContext) {
	schedID := sctx.Schedule.ID
	now := time.Now()
	s.logf(sctx, "[SCHEDULER] ⏰ Cron fired for %s (%s) at %s", schedID, sctx.Schedule.Name, now.Format(time.RFC3339))

	// Late-fire detection: compare to the next_run we recorded last time. Drift > 60s
	// usually means a missed-fire catch-up after macOS sleep/wake, or scheduler stall.
	s.runtimeStatesMu.RLock()
	if st, ok := s.runtimeStates[schedID]; ok && st.NextRunAt != nil {
		expected := *st.NextRunAt
		drift := now.Sub(expected)
		if drift > 60*time.Second {
			s.logf(sctx, "[SCHEDULER] ⚠️ LATE_FIRE schedule=%s expected=%s actual=%s drift=%s",
				schedID, expected.Format(time.RFC3339), now.Format(time.RFC3339), drift.Round(time.Second))
		}
	}
	s.runtimeStatesMu.RUnlock()

	paused, cfg, err := s.IsGloballyPaused(context.Background())
	if err != nil {
		s.logf(sctx, "[SCHEDULER] ⚠️ Failed to read scheduler config before trigger %s: %v", schedID, err)
	} else if paused {
		pausedAt := ""
		if cfg != nil && cfg.PausedAt != nil {
			pausedAt = cfg.PausedAt.Format(time.RFC3339)
		}
		if pausedAt != "" {
			s.logf(sctx, "[SCHEDULER] ⏸️ Global scheduler pause active since %s, skipping %s", pausedAt, schedID)
		} else {
			s.logf(sctx, "[SCHEDULER] ⏸️ Global scheduler pause active, skipping %s", schedID)
		}
		return
	}

	// Reload schedule for latest config — different paths for workflow vs multi-agent
	var freshCtx *ScheduleContext
	var workflowScheduleIDs []string
	if sctx.SourceType == "multi-agent" {
		f, exists, err := ReadMultiAgentSchedules(context.Background(), sctx.UserID)
		if err != nil || !exists {
			s.logf(sctx, "[SCHEDULER] ❌ Failed to reload multi-agent schedules for user %s: %v", sctx.UserID, err)
			return
		}
		var currentSched *WorkflowSchedule
		schedules := MergeBuiltinSchedules(f.Schedules)
		for i := range schedules {
			if schedules[i].ID == schedID {
				currentSched = &schedules[i]
				break
			}
		}
		if currentSched == nil {
			s.logf(sctx, "[SCHEDULER] ❌ Multi-agent schedule %s not found for user %s, skipping", schedID, sctx.UserID)
			return
		}
		if !currentSched.Enabled {
			s.logf(sctx, "[SCHEDULER] ⏭️ Multi-agent schedule %s is disabled, skipping", schedID)
			return
		}
		freshCtx = buildMultiAgentScheduleContext(sctx.UserID, *currentSched, f.Capabilities)
	} else {
		// Reload manifest for latest config
		manifest, found, err := ReadWorkflowManifest(context.Background(), sctx.WorkspacePath)
		if err != nil || !found {
			s.logf(sctx, "[SCHEDULER] ❌ Failed to reload manifest for %s: %v", schedID, err)
			return
		}

		workflowScheduleIDs = make([]string, 0, len(manifest.Schedules))
		for i := range manifest.Schedules {
			workflowScheduleIDs = append(workflowScheduleIDs, manifest.Schedules[i].ID)
		}

		// Find current schedule in manifest (may have been updated)
		var currentSched *WorkflowSchedule
		for i := range manifest.Schedules {
			if manifest.Schedules[i].ID == schedID {
				currentSched = &manifest.Schedules[i]
				break
			}
		}
		if currentSched == nil {
			s.logf(sctx, "[SCHEDULER] ❌ Schedule %s not found in manifest, skipping", schedID)
			return
		}
		if !currentSched.Enabled {
			if err := EnsureWorkflowScheduleExecutionTracker(context.Background(), sctx.WorkspacePath, *currentSched, time.Now().UTC()); err != nil {
				s.logf(sctx, "[SCHEDULER] Warning: failed to sync disabled execution history for %s: %v", schedID, err)
			}
			s.logf(sctx, "[SCHEDULER] ⏭️ Schedule %s is disabled, skipping", schedID)
			return
		}

		if activeExec := s.findActiveExecutionForWorkspace(sctx.WorkspacePath); activeExec != nil {
			triggeredBy := activeExec.TriggeredBy
			if strings.TrimSpace(triggeredBy) == "" {
				triggeredBy = "unknown"
			}
			s.logf(sctx, "[SCHEDULER] ⏭️ Workflow %s already has an active %s run (session: %s), skipping schedule %s",
				sctx.WorkspacePath, triggeredBy, activeExec.SessionID, schedID)
			return
		}

		freshCtx = buildScheduleContext(sctx.WorkspacePath, manifest, *currentSched)
	}

	// Built-in pre-fire check: if the built-in registered a gating function and
	// it returns false, skip this tick entirely. No LLM session is spawned.
	if check, ok := PreFireChecks[freshCtx.Schedule.ID]; ok {
		if !check(freshCtx.UserID) {
			s.logf(freshCtx, "[SCHEDULER] ⏭️ Pre-fire check returned false for %s (user %s) — skipping", freshCtx.Schedule.ID, freshCtx.UserID)
			return
		}
	}

	// Prevent concurrent runs — check and mark atomically under the write lock
	startTime := time.Now().UTC()
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(schedID)
	if state.LastStatus == "running" {
		s.runtimeStatesMu.Unlock()
		s.sessionLogf(freshCtx, state.LastSessionID, "[SCHEDULER] ⏭️ Schedule %s is already running (session: %s), skipping", schedID, state.LastSessionID)
		return
	}
	if freshCtx.SourceType == "workflow" {
		if otherID, otherSession := runningWorkflowScheduleInSetLocked(s.runtimeStates, workflowScheduleIDs, schedID); otherID != "" {
			s.runtimeStatesMu.Unlock()
			s.logf(freshCtx, "[SCHEDULER] ⏭️ Workflow %s already has running schedule %s (session: %s), skipping schedule %s",
				freshCtx.WorkspacePath, otherID, otherSession, schedID)
			return
		}
	}
	state.LastStatus = "running"
	state.LastRunAt = &startTime
	state.LastError = ""
	s.runtimeStatesMu.Unlock()

	if freshCtx.SourceType == "workflow" {
		if err := RecordWorkflowScheduleExecution(context.Background(), freshCtx.WorkspacePath, freshCtx.Schedule, startTime); err != nil {
			s.logf(freshCtx, "[SCHEDULER] Warning: failed to record scheduled execution for %s: %v", schedID, err)
		}
	}

	s.logf(freshCtx, "[SCHEDULER] 🚀 Starting %s (%s)", schedID, freshCtx.Schedule.Name)
	if _, err := s.runJob(context.Background(), freshCtx); err != nil {
		s.logf(freshCtx, "[SCHEDULER] ❌ %s failed: %v", schedID, err)
	} else {
		s.logf(freshCtx, "[SCHEDULER] ✅ %s completed", schedID)
	}
}

// runJob executes a scheduled job: updates runtime state, creates run history, executes, updates results.
func (s *SchedulerService) runJob(ctx context.Context, sctx *ScheduleContext) (string, error) {
	schedID := sctx.Schedule.ID
	startTime := time.Now().UTC()
	s.logf(sctx, "[SCHEDULER] runJob starting for %s (%s) at %s, groups=%v",
		schedID, sctx.Schedule.Name, startTime.Format(time.RFC3339), sctx.Schedule.GroupNames)

	// Clear session/error fields — status is already "running" (set atomically by caller)
	state := s.getOrCreateRuntimeState(schedID)
	state.LastSessionID = ""
	state.LastError = ""

	// Create run history entry (file-based)
	runID := uuid.New().String()
	run := &ScheduleRunEntry{
		ID:         runID,
		ScheduleID: schedID,
		Status:     "running",
		GroupNames: sctx.Schedule.GroupNames,
		StartedAt:  startTime,
	}
	if sctx.SourceType == "multi-agent" {
		if err := AppendMultiAgentScheduleRun(ctx, sctx.UserID, run); err != nil {
			s.logf(sctx, "[SCHEDULER] Failed to create multi-agent run entry for %s: %v", schedID, err)
		}
	} else {
		if err := AppendScheduleRun(ctx, sctx.WorkspacePath, run); err != nil {
			s.logf(sctx, "[SCHEDULER] Failed to create run entry for %s: %v", schedID, err)
		}
	}

	// Execute
	sessionID, runFolder, execErr := s.executeJob(ctx, sctx, runID)

	// Calculate results
	durationMs := time.Since(startTime).Milliseconds()
	nextRun := getNextRunTime(sctx.Schedule.CronExpression, sctx.Schedule.Timezone)

	status := "success"
	errMsg := ""
	if execErr != nil {
		status = "error"
		errMsg = execErr.Error()
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] %s failed in %dms: %v", schedID, durationMs, execErr)
	} else {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] %s completed in %dms, session: %s, folder: %s", schedID, durationMs, sessionID, runFolder)
	}

	// Keep the runtime state as "running" until all post-run side effects finish.
	// Pulse runs as several resumed builder-chat turns after the workflow result
	// is recorded; if we mark the schedule successful before Pulse finishes, a
	// frequent cron can start the next workflow run while Pulse is between steps.
	// That makes the next Pulse turn fail with workflow_busy (commonly after the
	// LLM/cost/time report), so cadence/backup/publish/notify never run.
	state.LastSessionID = sessionID

	// Update run history entry for the actual workflow/task run. Post-run Pulse
	// may continue after this, but it does not change the recorded run result.
	if sctx.SourceType == "multi-agent" {
		if err := UpdateMultiAgentScheduleRun(ctx, sctx.UserID, runID, status, errMsg, &durationMs, sessionID); err != nil {
			s.sessionLogf(sctx, sessionID, "[SCHEDULER] Failed to update multi-agent run entry for %s: %v", schedID, err)
		}
		if shouldUpdateChiefTaskReport(sctx) {
			if err := s.runChiefTaskReportUpdate(ctx, sctx, runID, status, errMsg, durationMs, startTime, time.Now().UTC(), sessionID); err != nil {
				s.sessionLogf(sctx, sessionID, "[TASK_REPORT] Failed to update pulse/task.html for %s: %v", schedID, err)
			}
		}
	} else {
		if err := UpdateScheduleRun(ctx, sctx.WorkspacePath, runID, status, errMsg, &durationMs, runFolder, sessionID); err != nil {
			s.sessionLogf(sctx, sessionID, "[SCHEDULER] Failed to update run entry for %s: %v", schedID, err)
		}

		// Pulse: the post-run steward. When enabled it runs a Gate turn that reads
		// run evidence and records a module worklist in db/db.sqlite, then executes
		// only the selected modules (harden, artifact/report/eval/learning/KB/DB
		// health, cost/time, or Goal Advisor), backs up the final state, publishes, and sends
		// a run summary notification — see runPostRunMonitor.
		// Opt-in per workflow (post_run_monitor in workflow.json) — runs only when
		// the user / builder enabled Pulse. Only after an actual workflow RUN, not an
		// optimizer/improvement pass (there's no fresh run output to triage there).
		// Never affects the run's recorded result.
		if runFolder != "" && sctx.Schedule.WorkshopMode != "optimizer" {
			if manifest, found, mErr := ReadWorkflowManifest(ctx, sctx.WorkspacePath); mErr == nil && found && manifest.MonitorEnabled() {
				// Pass the run's sessionID so Pulse resumes the SAME chat (not a fresh one).
				s.runPostRunMonitor(ctx, sctx, manifest, status, runFolder, sessionID)
			}
		}
	}

	// Now the whole scheduled job, including post-run side effects, is done.
	state.LastStatus = status
	state.LastError = errMsg
	state.LastDurationMs = &durationMs
	state.NextRunAt = nextRun
	state.RunCount++
	if status == "error" {
		state.ConsecutiveFailures++
	} else {
		state.ConsecutiveFailures = 0
	}

	return sessionID, execErr
}

func shouldUpdateChiefTaskReport(sctx *ScheduleContext) bool {
	if sctx == nil || sctx.SourceType != "multi-agent" {
		return false
	}
	if IsDefaultBuiltinSchedule(sctx.Schedule.ID) || IsOrgPulseSchedule(sctx.Schedule) {
		return false
	}
	hay := strings.ToLower(strings.Join([]string{
		sctx.Schedule.ID,
		sctx.Schedule.Name,
		sctx.Schedule.Description,
		sctx.Schedule.Query,
		strings.Join(sctx.Schedule.Messages, "\n"),
	}, "\n"))
	return !strings.Contains(hay, "enrich_memory") &&
		!strings.Contains(hay, "memory enrichment") &&
		!strings.Contains(hay, "auto-enrich memory") &&
		sctx.Schedule.ID != deprecatedAutoEnrichMemoryID
}

func (s *SchedulerService) runChiefTaskReportUpdate(ctx context.Context, sctx *ScheduleContext, runID, status, errMsg string, durationMs int64, startedAt, completedAt time.Time, sessionID string) error {
	if s == nil || s.api == nil {
		return fmt.Errorf("scheduler API is not configured")
	}
	if sessionID == "" {
		return fmt.Errorf("missing session id")
	}

	reqMap := map[string]interface{}{
		"agent_mode":                  "simple",
		"triggered_by":                "cron",
		"query":                       buildChiefTaskReportUpdateMessage(sctx, runID, status, errMsg, durationMs, startedAt, completedAt, sessionID),
		"task_report_update_turn":     true,
		"disable_live_input_delivery": true,
	}
	if len(sctx.Capabilities.SelectedServers) > 0 {
		reqMap["servers"] = sctx.Capabilities.SelectedServers
	}
	if len(sctx.Capabilities.SelectedSkills) > 0 {
		reqMap["selected_skills"] = sctx.Capabilities.SelectedSkills
	}
	if sctx.Capabilities.BrowserMode != "" && sctx.Capabilities.BrowserMode != "none" {
		reqMap["browser_mode"] = sctx.Capabilities.BrowserMode
	}
	if sctx.Capabilities.UseCodeExecutionMode {
		reqMap["use_code_execution_mode"] = true
	}
	s.applyChiefOfStaffLLMToReqMap(ctx, reqMap, sctx, sessionID)

	s.sessionLogf(sctx, sessionID, "[TASK_REPORT] updating pulse/task.html for schedule %s run %s", sctx.Schedule.ID, runID)
	if err := s.api.startSessionInternal(ctx, reqMap, sessionID, sctx.UserID, nil); err != nil {
		return fmt.Errorf("task report update turn failed: %w", err)
	}
	if err := s.waitForWorkshopIdle(ctx, sessionID); err != nil {
		return fmt.Errorf("task report update idle wait failed: %w", err)
	}
	return nil
}

func buildChiefTaskReportUpdateMessage(sctx *ScheduleContext, runID, status, errMsg string, durationMs int64, startedAt, completedAt time.Time, sessionID string) string {
	if sctx == nil {
		sctx = &ScheduleContext{}
	}
	taskText := strings.TrimSpace(sctx.Schedule.Query)
	if taskText == "" && len(sctx.Schedule.Messages) > 0 {
		taskText = strings.TrimSpace(strings.Join(sctx.Schedule.Messages, "\n\n"))
	}
	if taskText == "" {
		taskText = "(no query recorded)"
	}
	errLine := ""
	if strings.TrimSpace(errMsg) != "" {
		errLine = "\n- error: " + strings.TrimSpace(errMsg)
	}
	return fmt.Sprintf(`TASK REPORT UPDATE - normal Chief of Staff schedule completed.

Call get_reference_doc(kind="chief-task-report") and follow it exactly.

Update the single shared Tasks page at pulse/task.html. This is separate from Org Pulse.
Do not create per-task files. Do not edit pulse/org-pulse.html, pulse/goals.html, workflow files, schedules, memory tools/files, or secrets.
Do not redo the task; summarize the just-completed scheduled task run from this current conversation.
Do not call notify_user from this report-update turn unless the original task explicitly required a notification.

Run metadata:
- schedule_id: %s
- schedule_name: %s
- schedule_description: %s
- run_id: %s
- session_id: %s
- status: %s%s
- started_at: %s
- completed_at: %s
- duration_ms: %d
- cron_expression: %s
- timezone: %s

Original scheduled task:
%s

	What to write:
	- Create pulse/task.html if missing using the chief-task-report skeleton.
	- Prepend one .task-entry after <!-- CHIEF TASK ENTRIES: newest first -->.
	- Update the top summary tiles/counts and latest update timestamp.
	- Capture result summary, decisions/recommendations/findings, key findings to reuse on the next run, affected workflows/entities, evidence paths, and next action.
	- If the task failed, record the failure clearly with the error and suggested next action.
	- Keep the page concise; this is a durable task ledger, not a transcript dump.
	`, sctx.Schedule.ID, sctx.Schedule.Name, sctx.Schedule.Description, runID, sessionID, status, errLine, startedAt.Format(time.RFC3339), completedAt.Format(time.RFC3339), durationMs, sctx.Schedule.CronExpression, sctx.Schedule.Timezone, taskText)
}

func withChiefTaskRunContext(sctx *ScheduleContext, query string) string {
	if !shouldUpdateChiefTaskReport(sctx) {
		return query
	}
	return fmt.Sprintf(`NORMAL CHIEF OF STAFF TASK RUN.

Before doing the task, read pulse/task.html if it exists. Use only prior .task-entry items with data-schedule-id=%q as reusable context for this same scheduled task: key findings, open next actions, prior decisions, recurring entities/workflows, and evidence paths. Treat that page as the task's durable context. Do not use or update Chief of Staff memory tools/files.

After the task finishes, stop normally. The scheduler will send a separate report-update turn to write this run's summary and key findings back into pulse/task.html.

Scheduled task:
%s`, sctx.Schedule.ID, query)
}

// runPostRunMonitor fires the Pulse pass after a scheduled workflow run. Pulse
// reads the run evidence, plan changelog, and eval files to form a Bug
// verdict and a Goal verdict (recorded into builder/improve.html — the Pulse
// log, the single source of truth), applies the full plan-step harden for Bug
// findings (recording the Goal finding + evidence for the scheduled Goal
// Advisor loop, which applies evidence-backed replans), runs a separate report-only artifact drift
// review, records the report-only LLM/cost/time readout, backs up the final state
// before publish, and notifies on a transition. It
// never changes the run's recorded status — failures here are logged and
// swallowed. Pulse's behavior is defined by the post-run-monitor reference doc;
// this just hands it the run context.
func (s *SchedulerService) runPostRunMonitor(ctx context.Context, sctx *ScheduleContext, manifest *WorkflowManifest, runStatus, runFolder, runSessionID string) {
	defer func() {
		if r := recover(); r != nil {
			s.logf(sctx, "[PULSE] post-run pulse panic (recovered): %v", r)
		}
	}()

	// Resume the SAME session the workflow run just used, so Pulse continues in the
	// run's chat thread — the user sees the run and its post-run steward as one
	// conversation, not a fresh session spun up out of nowhere. Fall back to a new id
	// only if the run somehow didn't record one.
	sessionID := strings.TrimSpace(runSessionID)
	if sessionID == "" {
		sessionID = s.newScheduleSessionID(sctx)
	}
	baseReqMap := s.buildWorkshopRequest(ctx, sctx)

	// Run Pulse as a SEQUENCE of smaller turns: Gate first, then only the modules
	// Gate selected, then final backup/publish/notify. Each turn does one focused
	// job and builds on the prior turns' context in the same resumed session.
	answeredHumanInputs := formatAnsweredReportHumanInputsForAgent(ctx, sctx.WorkspacePath)
	humanInputNote := ""
	if answeredHumanInputs != "" {
		humanInputNote = "\n\n" + answeredHumanInputs
	}
	intro := fmt.Sprintf(
		"You are Pulse, the post-run steward. A scheduled run of this workflow just finished: workspace_path=%q, status=%q, run_folder=%q. "+
			"Call get_reference_doc(kind=\"post-run-monitor\") and follow it. Write builder/improve.html in simple user-facing language: takeaway first, short labeled detail after, raw evidence paths last. "+
			"If you need user input, call create_human_input_request(workspace_path=%q, source=\"pulse\", ...) instead of hand-editing request state in HTML. "+
			"For strategic plan-change approval, Goal Advisor must use that same existing report interaction flow: create_human_input_request(source=\"goal_advisor\", input_id=\"plan-proposal-...\", options approve/reject/defer, context=\"proposal + exact plan edits + evidence\"). "+
			"We'll go one step at a time — do ONLY the step in each message, finish it, then stop and wait for the next.%s",
		sctx.WorkspacePath, runStatus, runFolder, sctx.WorkspacePath, humanInputNote)

	upgradeSteps := postRunMonitorUpgradeStepsForManifest(manifest)
	s.sessionLogf(sctx, sessionID, "[PULSE] starting pulse for %s (run_folder=%s status=%s, upgrades=%d)", sctx.Schedule.ID, runFolder, runStatus, len(upgradeSteps))
	introSent := false
	runStep := func(st postRunMonitorStep) bool {
		reqMap := cloneStringInterfaceMap(baseReqMap)
		s.applyPulseLLMToReqMap(reqMap, sctx, sessionID)
		query := st.query
		if !introSent {
			query = intro + "\n\n" + query
			introSent = true
		}
		reqMap["query"] = query
		if err := s.api.startSessionInternal(ctx, reqMap, sessionID, "", nil); err != nil {
			s.sessionLogf(sctx, sessionID, "[PULSE] step %q failed to start: %v", st.label, err)
			return false
		}
		if err := s.waitForWorkshopIdleWithMaxWait(ctx, sessionID, st.idleMaxWait()); err != nil {
			s.sessionLogf(sctx, sessionID, "[PULSE] step %q idle wait failed: %v", st.label, err)
			return false
		}
		s.sessionLogf(sctx, sessionID, "[PULSE] step %q done for %s", st.label, sctx.Schedule.ID)
		return true
	}

	for _, st := range upgradeSteps {
		if !runStep(st) {
			return
		}
	}

	if !runStep(postRunMonitorGateStep(sessionID, runFolder, runStatus)) {
		return
	}

	steps := s.selectedPostRunMonitorModuleSteps(ctx, sctx, sessionID)
	s.sessionLogf(sctx, sessionID, "[PULSE] selected %d post-gate steps for %s", len(steps), sctx.Schedule.ID)
	for _, st := range steps {
		if !runStep(st) {
			return
		}
	}
	s.sessionLogf(sctx, sessionID, "[PULSE] pulse completed for %s", sctx.Schedule.ID)
	// Pulse owns its own notification: per its reference doc it calls notify_user
	// once with a compact run summary, highlighting state transitions it reads from
	// the durable Pulse log. The scheduler no longer pushes a templated message —
	// that avoids a double-send and lets the agent author the exact, nuanced sentence.
	// The Bug/Goal verdict lives in builder/improve.html (pills + headline) — the
	// single source of truth, no separate file.
}

type postRunMonitorStep struct{ label, query string }

func (st postRunMonitorStep) idleMaxWait() time.Duration {
	if st.label == "goal-advisor" {
		return schedulerGoalAdvisorIdleMaxWait
	}
	return schedulerWorkshopIdleMaxWait
}

func workflowHasPendingPlanChangelogArtifactReview(ctx context.Context, workspacePath string) (bool, error) {
	workspacePath = strings.Trim(strings.TrimSpace(workspacePath), "/")
	if workspacePath == "" {
		return false, nil
	}

	folder := workspacePath + "/planning/changelog"
	listing, exists, err := listWorkspaceFolder(ctx, folder, 1)
	if err != nil {
		return true, err
	}
	if !exists {
		return false, nil
	}

	var filePaths []string
	collectWorkspaceFilePaths(listing, &filePaths)
	for _, filePath := range filePaths {
		base := filepath.Base(filePath)
		if !strings.HasPrefix(base, "changelog-") || !strings.HasSuffix(strings.ToLower(base), ".json") {
			continue
		}

		content, exists, err := readFileFromWorkspace(ctx, filePath)
		if err != nil {
			return true, err
		}
		if !exists || strings.TrimSpace(content) == "" {
			continue
		}

		var changelog planChangelogFile
		if err := json.Unmarshal([]byte(content), &changelog); err != nil {
			// A malformed changelog still needs human/agent attention; keep the
			// Pulse Artifact Review turn rather than treating it as clean.
			return true, nil
		}
		for _, entry := range changelog.Entries {
			if entry.ArtifactReview == nil || !entry.ArtifactReview.Done {
				return true, nil
			}
		}
	}

	return false, nil
}

func postRunMonitorSteps() []postRunMonitorStep {
	steps := []postRunMonitorStep{postRunMonitorGateStep("<pulse_run_id>", "<run_folder>", "<run_status>")}
	for _, moduleStep := range postRunMonitorModuleSteps("<pulse_run_id>") {
		steps = append(steps, moduleStep.step)
	}
	steps = append(steps, postRunMonitorFinalSteps("<pulse_run_id>")...)
	return steps
}

func postRunMonitorGateStep(pulseRunID, runFolder, runStatus string) postRunMonitorStep {
	return postRunMonitorStep{
		label: "gate",
		query: fmt.Sprintf("STEP 1 — PULSE GATE / WORKLIST. This is the only mandatory intelligence step for this Pulse run. pulse_run_id=%q, run_folder=%q, run_status=%q.\n\n"+
			"Read the latest run evidence, builder/improve.html, planning/changelog, evaluation files, report_plan + db/reports HTML, db/README + db schema hints, knowledgebase notes/context, learnings/_global and per-step learning metadata, open/answered report_human_inputs, and pending Chief of Staff recommendation cards. Call get_pulse_module_state(workspace_path=\"<current workflow>\") before deciding.\n\n"+
			"Update builder/improve.html first with a simple user-facing Gate card: Bug verdict, Goal verdict, Maintenance Radar, and a Pulse Worklist summary with ran/skipped/due modules and plain reasons. Then call record_pulse_worklist exactly once with one decision for each module: harden, artifact_review, report_health, eval_health, learning_health, knowledgebase_health, db_health, cost_llm_time, goal_advisor.\n\n"+
			"Be agentic: decide what is due from workflow evidence, not a hardcoded cadence. You may skip expensive modules when the state and evidence justify it, but every skip needs a reason, evidence, and next_check_at / cooldown_runs / next_check_after_run_id when useful. Evidence can override cooldown. cost_llm_time normally runs every Pulse unless there is genuinely no telemetry to read; notify/final backup/publish happen later outside the worklist.\n\n"+
			"Module meanings:\n"+
			"- harden: due for real Bug findings, broken eval/report contracts that block truth, failed steps, stale guards, selector/API/runtime breakage, or CoS recs that are operational bugs.\n"+
			"- artifact_review: due when planning/changelog has unreviewed material plan/config changes, or when a plan/config change may have drifted learnings, KB, DB, report, eval, or step configs.\n"+
			"- report_health: due when report HTML, report_plan, SQL/window.report usage, dashboard tabs, goal visibility, or published/report claims are stale, broken, misleading, or not aligned with plan/eval/db.\n"+
			"- eval_health: due when evaluation_plan, eval rubric, eval run wiring, target run path, success criteria mapping, scoring thresholds, or eval reports are missing, stale, too lenient, gamed, misleading, or not aligned with soul/plan/report/db.\n"+
			"- learning_health: due when plan/step behavior changed, learning locks look stale, learning_objective no longer matches, reusable execution HOW was discovered, stale selectors/API quirks may exist, or learnings need lock/unlock decisions. If a plan change makes locked learnings stale, mark this due and name the step ids.\n"+
			"- knowledgebase_health: due when KB notes/context are missing, stale, contradictory, duplicated, in the wrong place, or step KB config no longer matches the plan. Do not rewrite knowledgebase/context; it is user-owned.\n"+
			"- db_health: due when DB schema/contracts/assets/upsert rules do not match current writers, reports, evals, or plan outputs.\n"+
			"- cost_llm_time: report run cost/eval cost/builder+Pulse overhead, missing cost buckets, timing, model/tier evidence; report-only unless a real config bug belongs to another module.\n"+
			"- goal_advisor: due when Goal drift persists, current strategy seems capped even if execution is clean, user answered a strategy question, CoS rec is strategic, or enough new evidence exists for an expert out-of-plan critique. Goal Advisor does not do routine harden/KB/learnings/db cleanup.\n\n"+
			"Do not call harden_workflow, improve_learnings, improve_kb, improve_db, plan modification tools, publish, backup, or notify in this Gate step. Only update the log/card and record_pulse_worklist. Then stop.",
			pulseRunID, runFolder, runStatus),
	}
}

type postRunMonitorModuleStep struct {
	module string
	step   postRunMonitorStep
}

func postRunMonitorModuleSteps(pulseRunID string) []postRunMonitorModuleStep {
	return []postRunMonitorModuleStep{
		{pulseModuleHarden, postRunMonitorStep{"harden", fmt.Sprintf("PULSE MODULE — HARDEN. pulse_run_id=%q. Run only the harden module selected by Pulse Gate. Read the Gate card/worklist in builder/improve.html and the evidence named for module harden. If there is a real Bug, call get_reference_doc(kind=\"optimize-playbook\"), then call harden_workflow(focus=\"<concise Bug finding + evidence paths from Pulse Gate>\") as the canonical repair path; include group_name only when the completed run was scoped to a single group. Capture the returned execution_id. Do not wait by polling: no sleep loops, no repeated list_executions, and no repeated query_step. Use query_step(step_id=\"harden-workflow\", execution_id=\"<returned execution_id>\") at most once for an immediate status/result check; if still running, stop and rely on [AUTO-NOTIFICATION] to resume when harden completes. When the result is available, record/refresh a `Decision - Pulse harden` card with the `Bug` verdict chip and work label (`Bug fix`, `Report fix`, or `Eval fix` as appropriate). If no Bug remains after re-reading evidence, record why no harden was needed. Finish by calling mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"harden\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]). Then stop.", pulseRunID, pulseRunID)}},
		{pulseModuleArtifactReview, postRunMonitorStep{"artifact", fmt.Sprintf("PULSE MODULE — ARTIFACT REVIEW. pulse_run_id=%q. Run only the artifact drift module selected by Pulse Gate. This is not part of harden and is report-only. Read planning/changelog/ and the Artifact Sync Cursor in builder/improve.html. Call get_workflow_command_guidance(kind=\"review-artifact-drift\", focus=\"Pulse artifact review after this run; report-only; do not fix\") and follow it: call review_artifact_sync(focus=\"Pulse artifact review after this run; report-only; do not fix\"), capture the returned execution_id. Do not wait by polling: no sleep loops, no repeated list_executions, and no repeated query_step. Use query_step(step_id=\"review-artifact-sync\", execution_id=\"<returned execution_id>\") at most once for an immediate status/result check; if still running, stop and rely on [AUTO-NOTIFICATION] to resume when it completes. review_artifact_sync must append/update the report-only Artifact Review item in builder/improve.html with the `Artifact drift` action label and call mark_changelog_artifact_reviewed for entries fully inspected. Do not fix artifacts here. Finish by calling mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"artifact_review\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]). Then stop.", pulseRunID, pulseRunID)}},
		{pulseModuleReportHealth, postRunMonitorStep{"report-health", fmt.Sprintf("PULSE MODULE — REPORT HEALTH. pulse_run_id=%q. Run only the report/dashboard health module selected by Pulse Gate. Read reports/report_plan.json, db/reports/*.html, builder/improve.html, current plan/eval/db evidence, and latest run outputs. If the report is stale, broken, misleading, too text-heavy, missing goal/progress visibility, using dead SQL/window.report paths, or not aligned with DB/eval/plan, call get_workflow_command_guidance(kind=\"improve-report\", focus=\"Pulse report health repair from Gate evidence\") and follow it. Prefer bounded HTML/report_plan fixes; do not replan the workflow. Record a `Report fix` entry in builder/improve.html when you change report artifacts. Finish by calling mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"report_health\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]). Then stop.", pulseRunID, pulseRunID)}},
		{pulseModuleEvalHealth, postRunMonitorStep{"eval-health", fmt.Sprintf("PULSE MODULE — EVAL HEALTH. pulse_run_id=%q. Run only the evaluation health module selected by Pulse Gate. Read evaluation/evaluation_plan.json, evaluation/runs or scores/evaluation outputs, soul/soul.md success criteria, planning/plan.json, planning/step_config.json, report/db consumers, and latest run evidence. If evals are missing, stale, too lenient, gamed, scoped to the wrong run/group, not mapped to success criteria, duplicating operational checks, broken by TARGET_RUN_PATH wiring, or producing misleading reports, call get_workflow_command_guidance(kind=\"improve-evaluation\", focus=\"Pulse eval health repair from Gate evidence\") and follow it. Prefer bounded evaluation_plan/eval config fixes; do not replan the workflow. Validate the evaluation plan when the validation tool is available and run a targeted eval only when materially useful. Record an `Eval fix` entry in builder/improve.html when you change eval artifacts. Finish by calling mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"eval_health\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]). Then stop.", pulseRunID, pulseRunID)}},
		{pulseModuleLearningHealth, postRunMonitorStep{"learning-health", fmt.Sprintf("PULSE MODULE — LEARNING HEALTH. pulse_run_id=%q. Run only the learning health module selected by Pulse Gate. Load get_reference_doc(kind=\"optimize-playbook\") and get_reference_doc(kind=\"step-config\"). Read planning/plan.json, planning/step_config.json, planning/changelog, learnings/_global/SKILL.md, per-step .learning_metadata.json, and latest run evidence. If a plan/step behavior change makes locked learnings stale, use update_step_config on the specific step ids to clear stale lock_learnings/description_reviewed with review_notes. If mature stable learnings should be frozen, set lock_learnings=true only with a concrete review_notes rationale and evidence. If learning content itself needs cleanup/consolidation, call improve_learnings(mode=\"auto\", instruction=\"<specific evidence-backed instruction>\", focus=\"<step ids or topic>\"); if it returns an execution_id, do not wait by polling: no sleep loops, no repeated list_executions, and no repeated query_step. Use query_step(step_id=\"Skill Update\", execution_id=\"<returned execution_id>\") at most once for an immediate status/result check; if still running, stop and rely on [AUTO-NOTIFICATION] to resume when complete. Finish by calling mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"learning_health\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]). Then stop.", pulseRunID, pulseRunID)}},
		{pulseModuleKnowledgebaseHealth, postRunMonitorStep{"knowledgebase-health", fmt.Sprintf("PULSE MODULE — KNOWLEDGEBASE HEALTH. pulse_run_id=%q. Run only the KB health module selected by Pulse Gate. Read get_reference_doc(kind=\"stores\") and, when changing step config, get_reference_doc(kind=\"step-config\"). Inspect knowledgebase/notes, knowledgebase/context only as read-only user-owned context, planning/step_config KB access/contribution settings, latest run evidence, and report/eval consumers. If notes are stale/duplicated/missing/contradictory or step KB config no longer matches the plan, call improve_kb(mode=\"auto\", instruction=\"<specific KB cleanup/consolidation instruction>\", focus=\"<topic/step/evidence>\") and continue from the returned summary; use update_step_config only for concrete KB access/contribution fixes. Never rewrite knowledgebase/context. Finish by calling mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"knowledgebase_health\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]). Then stop.", pulseRunID, pulseRunID)}},
		{pulseModuleDBHealth, postRunMonitorStep{"db-health", fmt.Sprintf("PULSE MODULE — DB HEALTH. pulse_run_id=%q. Run only the DB health module selected by Pulse Gate. Load get_reference_doc(kind=\"stores\"). Read db/db.sqlite schema/table contracts, db/README.md, db/assets, current plan writer steps, report SQL/window.report consumers, eval consumers, and latest run evidence. If schema/contracts/assets/upserts are stale, missing, undocumented, or incompatible with reports/evals/steps, call improve_db(mode=\"auto\", instruction=\"<specific DB contract/schema/report-compatibility instruction>\", focus=\"<table/report/step>\"), capture the returned execution_id. Do not wait by polling: no sleep loops, no repeated list_executions, and no repeated query_step. Use query_step(step_id=\"improve-db\", execution_id=\"<returned execution_id>\") at most once for an immediate status/result check; if still running, stop and rely on [AUTO-NOTIFICATION] to resume when complete. Do not do speculative row migrations. Finish by calling mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"db_health\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]). Then stop.", pulseRunID, pulseRunID)}},
		{pulseModuleCostLLMTime, postRunMonitorStep{"cost-llm-time", fmt.Sprintf("PULSE MODULE — COST / LLM / TIME. pulse_run_id=%q. This is report-only. Read workflow.json capabilities.llm_config / step execution tiers, get_cost_summary(run_folder) when available, costs/execution + costs/evaluation + costs/phase/token_usage.json, and timing summaries under runs/<run_folder>/logs/<step-id>/execution. Create a compact telemetry report with labeled buckets: workflow execution cost, evaluation cost, and builder/Pulse overhead. Name missing evidence instead of estimating. Update builder/improve.html cost/time tiles and overwrite builder/card.cost.html with a compact org-dashboard cost card. do NOT change model tiers, LLM config, prompts, schedules, or agent allocation. Finish by calling mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"cost_llm_time\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]). Then stop.", pulseRunID, pulseRunID)}},
		{pulseModuleGoalAdvisor, postRunMonitorStep{"goal-advisor", fmt.Sprintf("PULSE MODULE — GOAL ADVISOR. pulse_run_id=%q. Run only the strategy advisor module selected by Pulse Gate. Do not perform the strategic review inline. Read only the Gate/worklist enough to pass a concise focus, then call run_goal_advisor_review(pulse_run_id=%q, focus=\"Pulse-selected Goal Advisor module; use Gate evidence; expert strategy advisor, not routine hardening\"). Capture the returned execution_id. Do not wait by polling: no sleep loops, no repeated list_executions, and no repeated query_step. Use query_step(step_id=\"goal-advisor\", execution_id=\"<returned execution_id>\") at most once for an immediate status/result check; if still running, stop and rely on [AUTO-NOTIFICATION] to resume when the background Goal Advisor completes. Do not call harden_workflow, improve_kb, improve_learnings, or improve_db from this parent Pulse turn. When the result is available from [AUTO-NOTIFICATION] or a one-off query_step, update/confirm the visible Goal Advisor outcome in builder/improve.html if needed and finish by calling mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"goal_advisor\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]). Then stop.", pulseRunID, pulseRunID, pulseRunID)}},
	}
}

func postRunMonitorPreBackupStep(pulseRunID string) postRunMonitorStep {
	return postRunMonitorStep{"pre-backup", fmt.Sprintf("PULSE PRE-CHANGE BACKUP. pulse_run_id=%q. The Gate selected one or more mutating/reporting modules. Do not ask for confirmation. Read workflow.json.backup and call get_reference_doc(kind=\"backup-strategy\"). Ensure there is a checkpoint before selected Pulse modules mutate workflow artifacts. If backup is disabled, set up the zero-config local-git default and write workflow.json.backup plus backup/status.json. If backup/status.json says the current source hash is already backed up, record that and skip the actual push. Report the backup result, then stop.", pulseRunID)}
}

func postRunMonitorFinalSteps(pulseRunID string) []postRunMonitorStep {
	return []postRunMonitorStep{
		{"dashboard", "PULSE DASHBOARD / QUESTIONS. Prepare the final user-facing Pulse/org state before backup and publish. Overwrite builder/card.health.html with one compact org-dashboard card fragment (inline content only, single-quoted attributes). This dashboard write happens every run even if notifications are disabled. Summarize the final post-Pulse health and final post-Pulse state: selected modules ran/skipped, Bug/Goal state, user input requests, harden/artifact/report/eval/learning/KB/DB/cost/Goal Advisor outcomes, backup/publish intent, and next action. If this run needs user input or the later notification would ask a question, call create_human_input_request(workspace_path=\"<current workflow>\", source=\"pulse\", ...) now and show the request in builder/improve.html. Do not call notify_user, backup, or publish in this step. Then stop."},
		{"backup", fmt.Sprintf("PULSE FINAL BACKUP. pulse_run_id=%q. Read workflow.json.backup and back up per get_reference_doc(kind=\"backup-strategy\"). Back up the final state produced by Gate + selected Pulse modules + dashboard/questions, including builder/improve.html, builder/card.*.html, db/db.sqlite pulse_module_state/report_human_inputs, report/eval/KB/learnings/db changes, backup/status.json, and publish/status.json when changed. If backup is disabled, set it up with the zero-config local-git default and back up. Skip actual push only when backup/status.json shows the current source is already backed up. Always write backup/status.json and update the latest run row with the backup result. Report the backup result, then stop.", pulseRunID)},
		{"publish", "PULSE PUBLISH. If workflow.json.publish is enabled, re-publish the updated HTML per get_reference_doc(kind=\"publish-strategy\") — but ONLY when the destination is already VERIFIED (publish/status.json shows a prior successful publish). Every run changes published artifacts — new db data plus a fresh Pulse entry in builder/improve.html — so always re-publish to a verified destination. Never do the first/verifying publish here unattended. Always write publish/status.json. Report the result, then stop."},
		{"notify", "PULSE NOTIFY. Read builder/improve.html, builder/card.health.html, db/db.sqlite report_human_inputs, backup/status.json, and publish/status.json. Do not create new user-input requests here unless the dashboard step clearly failed to record an already-needed request; this step is primarily delivery. Honor soul/soul.md ## Notifications; otherwise call notify_user once every run with a compact summary. Include selected modules ran/skipped, Bug/Goal state, user input requests, harden/artifact/report/eval/learning/KB/DB/cost/Goal Advisor outcomes, backup/publish status, dashboard URL when live, and next action. When Gmail/email fields are available, email is the default rich rendering: set email_subject, email_html, and plain email_body on the same notify_user call unless the preference explicitly says not to email; set email_to/email_cc only when the preference asks. Then stop."},
	}
}

func (s *SchedulerService) selectedPostRunMonitorModuleSteps(ctx context.Context, sctx *ScheduleContext, pulseRunID string) []postRunMonitorStep {
	worklist, ok, err := getPulseWorklistForRun(ctx, sctx.WorkspacePath, pulseRunID)
	if err != nil {
		s.sessionLogf(sctx, pulseRunID, "[PULSE] worklist read failed; using conservative fallback: %v", err)
	}
	var selected []postRunMonitorStep
	if !ok || err != nil {
		selected = s.fallbackPostRunMonitorModuleSteps(ctx, sctx, pulseRunID, worklist)
	} else if !pulseWorklistIsComplete(worklist) {
		s.sessionLogf(sctx, pulseRunID, "[PULSE] worklist incomplete (%d/%d modules); using conservative fallback", len(worklist), len(pulseModuleOrder))
		selected = s.fallbackPostRunMonitorModuleSteps(ctx, sctx, pulseRunID, worklist)
	} else {
		for _, moduleStep := range postRunMonitorModuleSteps(pulseRunID) {
			state, exists := worklist[moduleStep.module]
			if !exists || strings.TrimSpace(strings.ToLower(state.LastDecision)) != "due" {
				continue
			}
			selected = append(selected, moduleStep.step)
		}
	}
	if len(selected) > 0 {
		selected = append([]postRunMonitorStep{postRunMonitorPreBackupStep(pulseRunID)}, selected...)
	}
	selected = append(selected, postRunMonitorFinalSteps(pulseRunID)...)
	return selected
}

func (s *SchedulerService) fallbackPostRunMonitorModuleSteps(ctx context.Context, sctx *ScheduleContext, pulseRunID string, worklist map[string]PulseModuleState) []postRunMonitorStep {
	wanted := map[string]bool{
		pulseModuleHarden:       true,
		pulseModuleReportHealth: true,
		pulseModuleEvalHealth:   true,
		pulseModuleCostLLMTime:  true,
	}
	for _, module := range pulseModuleOrder {
		if pulseWorklistModuleWasDue(worklist, module) {
			wanted[module] = true
		}
	}
	pendingArtifactReview, err := workflowHasPendingPlanChangelogArtifactReview(ctx, sctx.WorkspacePath)
	if err != nil {
		s.sessionLogf(sctx, pulseRunID, "[PULSE] fallback artifact changelog check failed; including artifact review: %v", err)
		pendingArtifactReview = true
	}
	if pendingArtifactReview {
		wanted[pulseModuleArtifactReview] = true
	}
	var selected []postRunMonitorStep
	for _, moduleStep := range postRunMonitorModuleSteps(pulseRunID) {
		if wanted[moduleStep.module] {
			selected = append(selected, moduleStep.step)
		}
	}
	return selected
}

func pulseWorklistModuleWasDue(worklist map[string]PulseModuleState, module string) bool {
	if len(worklist) == 0 {
		return false
	}
	state, ok := worklist[module]
	if !ok {
		return false
	}
	decision := strings.TrimSpace(strings.ToLower(state.LastGateDecision))
	if decision == "" {
		decision = strings.TrimSpace(strings.ToLower(state.LastDecision))
	}
	return decision == "due"
}

func optimizerScheduleMessages(_ context.Context, _ string, stored []string, _ []string) []string {
	messages := compactScheduleMessages(stored)
	if len(messages) > 0 && !isLegacyGoalAdvisorMessageQueue(messages) {
		return messages
	}
	return []string{
		"Do not ask for confirmation. This optimizer schedule is no longer the product Goal Advisor loop. Goal Advisor now runs as a Pulse-selected module after normal scheduled workflow runs. Do not modify workflow files. Report that this legacy optimizer schedule should be disabled or converted to an explicit custom optimizer job.",
	}
}

func isLegacyOrEmptyOptimizerSchedule(messages []string) bool {
	messages = compactScheduleMessages(messages)
	return len(messages) == 0 || isLegacyGoalAdvisorMessageQueue(messages)
}

func isLegacyGoalAdvisorMessageQueue(messages []string) bool {
	joined := normalizeLegacyScheduleText(strings.Join(messages, "\n"))
	if !strings.Contains(joined, "step 1/5") || !strings.Contains(joined, "pre backup") {
		return false
	}
	if !strings.Contains(joined, "step 2/5") {
		return false
	}
	return strings.Contains(joined, "goal advisor") || strings.Contains(joined, "improve")
}

func normalizeLegacyScheduleText(text string) string {
	text = strings.ToLower(text)
	replacer := strings.NewReplacer(
		"—", " ",
		"–", " ",
		"-", " ",
		"_", " ",
		"\n", " ",
		"\t", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

func compactScheduleMessages(messages []string) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		if strings.TrimSpace(msg) == "" {
			continue
		}
		out = append(out, msg)
	}
	return out
}

// executeJob builds a session request from the manifest and runs it.
// Returns (sessionID, runFolder, error).
func (s *SchedulerService) executeJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, string, error) {
	// Multi-agent schedules live in a separate user-level schedule store. All
	// workflow-manifest schedules execute through the workshop builder path.
	if sctx.SourceType == "multi-agent" {
		return s.executeMultiAgentJob(ctx, sctx, runID)
	}

	if mode := strings.TrimSpace(sctx.Schedule.Mode); mode != "" && mode != "workshop" {
		s.logf(sctx, "[SCHEDULER] Schedule %s uses legacy mode=%s; executing through workshop mode", sctx.Schedule.ID, mode)
	}
	return s.executeWorkshopJob(ctx, sctx, runID)
}

// executeWorkshopJob runs a workflow via the workshop builder path (workflow_phase mode).
func (s *SchedulerService) executeWorkshopJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, string, error) {
	messages := compactScheduleMessages(sctx.Schedule.Messages)
	isOptimizer := strings.EqualFold(strings.TrimSpace(sctx.Schedule.WorkshopMode), "optimizer")
	if isOptimizer {
		if isLegacyOrEmptyOptimizerSchedule(messages) {
			runFolder := "iteration-0"
			sessionID := s.newScheduleSessionID(sctx)
			if runID != "" {
				_ = UpdateScheduleRun(ctx, sctx.WorkspacePath, runID, "running", "", nil, runFolder, sessionID)
			}
			if err := s.disableLegacyOptimizerSchedule(ctx, sctx, sessionID); err != nil {
				return sessionID, runFolder, err
			}
			return sessionID, runFolder, nil
		}
	} else if len(messages) == 0 {
		messages = []string{"Run the full workflow using run_full_workflow tool. " + scheduledBackgroundNoPollingInstruction}
	}
	runFolder := "iteration-0"

	sessionID := s.newScheduleSessionID(sctx)

	state := s.getOrCreateRuntimeState(sctx.Schedule.ID)
	state.LastSessionID = sessionID

	if runID != "" {
		_ = UpdateScheduleRun(ctx, sctx.WorkspacePath, runID, "running", "", nil, runFolder, sessionID)
	}

	s.sessionLogf(sctx, sessionID, "[SCHEDULER] Workshop mode: executing %d messages for %s (session=%s workspace=%s run_folder=%s)",
		len(messages), sctx.Schedule.ID, sessionID, sctx.WorkspacePath, runFolder)

	baseReqMap := s.buildWorkshopRequest(ctx, sctx)

	for i, msg := range messages {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Workshop message %d/%d: %q", i+1, len(messages), msg)

		reqMap := make(map[string]interface{})
		for k, v := range baseReqMap {
			reqMap[k] = v
		}
		reqMap["query"] = msg

		// Resume the workflow's latest thread (same CLI) on the first message
		// only — later messages already share this run's live session.
		if i == 0 {
			if resumed := s.maybeResumeLatestWorkflowThread(ctx, sctx, reqMap, sessionID); resumed != "" {
				s.sessionLogf(sctx, sessionID, "[SCHEDULER] Resuming latest workflow thread %s for schedule %s", resumed, sctx.Schedule.ID)
			}
		}

		if err := s.api.startSessionInternal(ctx, reqMap, sessionID, "", nil); err != nil {
			return sessionID, runFolder, fmt.Errorf("workshop message %d/%d failed: %w", i+1, len(messages), err)
		}

		// First message of the workshop sequence — stamp schedule name on
		// the session for frontend tab labeling. Subsequent calls are
		// no-ops (helper guards against overwriting an existing Title).
		s.stampScheduleNameOnSession(sessionID, sctx)

		if err := s.waitForWorkshopIdle(ctx, sessionID); err != nil {
			return sessionID, runFolder, fmt.Errorf("workshop idle wait failed after message %d: %w", i+1, err)
		}

		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Workshop message %d/%d completed", i+1, len(messages))
	}

	// Note: backup-on-completion is not appended here as a message turn. Backup is
	// owned by two arms that share one source-hash-gated contract: the Pulse pass
	// (runPostRunMonitor, step 4) for scheduled runs when Pulse is enabled, and the
	// run_workflow completion directive (workflowRunCompletionDirective) for
	// interactive runs (and as the fallback when Pulse is off). The shared source-hash
	// gate means whichever arm runs second sees the state already backed up and skips
	// the push — so the overlap can't double-back up.

	// Previously auto-generated a static markdown report here via the report agent.
	// The dynamic report (design doc §2) is a live frontend view over db/ + graph.json;
	// there is no post-run artifact to produce, so scheduled runs now finish without a
	// report side-effect. Users open the report in the UI whenever they want.

	s.sessionLogf(sctx, sessionID, "[SCHEDULER] ✅ Workshop execution completed for %s, session=%s, folder=%s", sctx.Schedule.ID, sessionID, runFolder)
	return sessionID, runFolder, nil
}

func (s *SchedulerService) disableLegacyOptimizerSchedule(ctx context.Context, sctx *ScheduleContext, sessionID string) error {
	if sctx == nil {
		return nil
	}
	if sctx.SourceType != "" && sctx.SourceType != "workflow" {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] legacy optimizer schedule %s is non-workflow source %q; skipping manifest disable", sctx.Schedule.ID, sctx.SourceType)
		return nil
	}
	manifest, found, err := ReadWorkflowManifest(ctx, sctx.WorkspacePath)
	if err != nil {
		return fmt.Errorf("disable legacy optimizer schedule: %w", err)
	}
	if !found {
		return fmt.Errorf("disable legacy optimizer schedule: workflow manifest not found at %s", sctx.WorkspacePath)
	}
	for i := range manifest.Schedules {
		if manifest.Schedules[i].ID != sctx.Schedule.ID {
			continue
		}
		if !manifest.Schedules[i].Enabled {
			s.sessionLogf(sctx, sessionID, "[SCHEDULER] legacy optimizer schedule %s already disabled; no LLM session started", sctx.Schedule.ID)
			return nil
		}
		manifest.Schedules[i].Enabled = false
		enabled := true
		// Deliberately migrate the old standalone optimizer loop into Pulse.
		// Disabling the legacy schedule while leaving Pulse off would silently
		// remove Goal Advisor from the workflow.
		manifest.PostRunMonitor = &enabled
		if err := WriteWorkflowManifest(ctx, sctx.WorkspacePath, manifest); err != nil {
			return fmt.Errorf("disable legacy optimizer schedule: write workflow.json: %w", err)
		}
		if err := s.ReloadSchedule(ctx, sctx.WorkspacePath, sctx.Schedule.ID); err != nil {
			s.sessionLogf(sctx, sessionID, "[SCHEDULER] legacy optimizer schedule %s disabled but reload failed: %v", sctx.Schedule.ID, err)
		}
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] legacy optimizer schedule %s disabled without starting an LLM session; post_run_monitor enabled so Goal Advisor can run inside Pulse", sctx.Schedule.ID)
		return nil
	}
	return fmt.Errorf("disable legacy optimizer schedule: schedule %s not found in %s", sctx.Schedule.ID, sctx.WorkspacePath)
}

// executeMultiAgentJob runs a multi-agent chat session with the configured query.
// maxWorkflowResumeScan bounds how many of this schedule's most recent runs the
// scheduler inspects when resume_previous=true: if a same-CLI resumable scheduled
// chat is among the latest few, resume it; otherwise start a fresh session.
const maxWorkflowResumeScan = 5

// maybeResumeLatestWorkflowThread wires restored_conversation_session_id into reqMap
// so an opt-in scheduled workflow run continues the schedule's most recent
// scheduled chat instead of starting fresh.
//
// We look at schedule-runs.json for this exact schedule_id first, then validate
// the referenced chat runtime. This deliberately excludes normal user/builder
// chats in the same workflow workspace. A different CLI's external session ID is
// meaningless to the new one. Prior run status (success/error) is intentionally
// ignored: resume happens regardless so the agent can recover from a failed run.
// Returns the resumed thread's session ID, or "" when the run should start fresh.
func (s *SchedulerService) maybeResumeLatestWorkflowThread(ctx context.Context, sctx *ScheduleContext, reqMap map[string]interface{}, currentSessionID string) string {
	if !sctx.Schedule.ShouldResumePrevious() {
		return ""
	}

	runs, err := listScheduleRunsForResume(ctx, sctx.WorkspacePath, sctx.Schedule.ID)
	if err != nil || len(runs) == 0 {
		return ""
	}
	return s.maybeResumeLatestScheduledThread(sctx, reqMap, currentSessionID, runs, sctx.WorkspacePath)
}

func (s *SchedulerService) maybeResumeLatestMultiAgentThread(ctx context.Context, sctx *ScheduleContext, reqMap map[string]interface{}, currentSessionID string) string {
	if !sctx.Schedule.ShouldResumePrevious() {
		return ""
	}

	runs, err := listMultiAgentScheduleRunsForResume(ctx, sctx.UserID, sctx.Schedule.ID)
	if err != nil || len(runs) == 0 {
		return ""
	}
	return s.maybeResumeLatestScheduledThread(sctx, reqMap, currentSessionID, runs, "")
}

func (s *SchedulerService) maybeResumeLatestScheduledThread(sctx *ScheduleContext, reqMap map[string]interface{}, currentSessionID string, runs []ScheduleRunEntry, workspacePath string) string {
	currentProvider := ""
	if sctx.Capabilities.LLMConfig != nil {
		currentProvider = strings.TrimSpace(sctx.Capabilities.LLMConfig.Provider)
	}
	if currentProvider == "" {
		return ""
	}

	// Runs are newest-first. Within the latest maxWorkflowResumeScan scheduled
	// chats, resume the most recent one that is a resumable coding-agent thread
	// on the same CLI; skip any that don't qualify (e.g. an API-model thread).
	// If none qualify, start fresh.
	//
	// Validate via ReadChatHistoryRuntimeForSession(sessionID, workspace) — the
	// SAME resolver handleQuery uses when it later honors
	// restored_conversation_session_id — so what we match here is provably what
	// gets resumed.
	checked := 0
	for _, run := range runs {
		sessionID := strings.TrimSpace(run.SessionID)
		if sessionID == "" || sessionID == currentSessionID {
			continue
		}
		checked++
		if checked > maxWorkflowResumeScan {
			break
		}

		rt, ok, rErr := ReadChatHistoryRuntimeForSession(sctx.UserID, sessionID, workspacePath)
		if rErr != nil || !ok || rt == nil {
			continue
		}
		// A coding-agent thread the CLI can resume: kind, matching CLI provider,
		// and a captured external session ID to hand to the CLI's --resume.
		if rt.Kind != "coding_agent" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(rt.Provider), currentProvider) {
			continue
		}
		if !rt.ResumeSupported || strings.TrimSpace(rt.ExternalSessionID) == "" {
			continue
		}
		reqMap["restored_conversation_session_id"] = sessionID
		return sessionID
	}
	return ""
}

func listScheduleRunsForResume(ctx context.Context, workspacePath, scheduleID string) ([]ScheduleRunEntry, error) {
	if localRuns, ok, err := readLocalScheduleRuns(workspacePath); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return filterScheduleRunsNewestFirst(localRuns, scheduleID), nil
	}
	runs, _, err := ListScheduleRuns(ctx, workspacePath, scheduleID, maxScheduleRuns, 0)
	return runs, err
}

func readLocalScheduleRuns(workspacePath string) ([]ScheduleRunEntry, bool, error) {
	localPath := filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(scheduleRunsPath(workspacePath)))
	data, err := os.ReadFile(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	var runs []ScheduleRunEntry
	if err := json.Unmarshal(data, &runs); err != nil {
		return nil, true, err
	}
	return runs, true, nil
}

func readLocalMultiAgentScheduleRuns(userID string) ([]ScheduleRunEntry, bool, error) {
	userID = sanitizeUserIDForPath(userID)
	localPath := filepath.Join(fsutil.WorkspaceDocsRoot(), filepath.FromSlash(multiAgentScheduleRunsPath(userID)))
	data, err := os.ReadFile(localPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	var runs []ScheduleRunEntry
	if err := json.Unmarshal(data, &runs); err != nil {
		return nil, true, err
	}
	return runs, true, nil
}

func listMultiAgentScheduleRunsForResume(ctx context.Context, userID, scheduleID string) ([]ScheduleRunEntry, error) {
	if localRuns, ok, err := readLocalMultiAgentScheduleRuns(userID); ok || err != nil {
		if err != nil {
			return nil, err
		}
		return filterScheduleRunsNewestFirst(localRuns, scheduleID), nil
	}
	runs, _, err := ListMultiAgentScheduleRuns(ctx, userID, scheduleID, maxScheduleRuns, 0)
	return runs, err
}

func filterScheduleRunsNewestFirst(runs []ScheduleRunEntry, scheduleID string) []ScheduleRunEntry {
	filtered := make([]ScheduleRunEntry, 0, len(runs))
	for _, run := range runs {
		if run.ScheduleID == scheduleID {
			filtered = append(filtered, run)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].StartedAt.After(filtered[j].StartedAt)
	})
	if len(filtered) > maxScheduleRuns {
		filtered = filtered[:maxScheduleRuns]
	}
	return filtered
}

func (s *SchedulerService) executeMultiAgentJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, string, error) {
	// A multi-agent schedule runs either a Messages SEQUENCE (one focused turn per
	// message in one resumed session, the way workflow Pulse / runPostRunMonitor
	// does — this is how Org Pulse runs) or a single Query (legacy/fallback).
	// Messages wins when present; Query stays the single-turn fallback so anything
	// that still sets only Query keeps working unchanged.
	messages := sctx.Schedule.Messages
	query := strings.TrimSpace(sctx.Schedule.Query)
	if len(messages) == 0 && query == "" {
		return "", "", fmt.Errorf("multi-agent schedule %s has no messages or query", sctx.Schedule.ID)
	}
	if query != "" {
		query = withChiefTaskRunContext(sctx, query)
	}

	sessionID := s.newScheduleSessionID(sctx)

	state := s.getOrCreateRuntimeState(sctx.Schedule.ID)
	state.LastSessionID = sessionID

	if runID != "" {
		_ = UpdateMultiAgentScheduleRun(ctx, sctx.UserID, runID, "running", "", nil, sessionID)
	}

	// Build the base request once. The per-turn query is set per message below
	// (sequence) or kept as-is (single Query). Everything else — capabilities,
	// servers, skills, browser/code-exec, LLM config, and secrets — is shared by
	// every turn of the sequence.
	reqMap := map[string]interface{}{
		"agent_mode":                  "simple",
		"triggered_by":                "cron",
		"disable_live_input_delivery": true,
	}
	if query != "" {
		reqMap["query"] = query
	}

	// Apply capabilities if set
	if len(sctx.Capabilities.SelectedServers) > 0 {
		reqMap["servers"] = sctx.Capabilities.SelectedServers
	}
	if len(sctx.Capabilities.SelectedSkills) > 0 {
		reqMap["selected_skills"] = sctx.Capabilities.SelectedSkills
	}
	if sctx.Capabilities.BrowserMode != "" && sctx.Capabilities.BrowserMode != "none" {
		reqMap["browser_mode"] = sctx.Capabilities.BrowserMode
	}
	if sctx.Capabilities.UseCodeExecutionMode {
		reqMap["use_code_execution_mode"] = true
	}

	// Apply LLM config and secrets
	s.applyLLMAndSecretsToReqMap(ctx, reqMap, sctx)
	s.applyChiefOfStaffLLMToReqMap(ctx, reqMap, sctx, sessionID)

	// Load user-level secrets if configured
	if len(sctx.Capabilities.SelectedSecrets) > 0 && sctx.UserID != "" {
		userSecrets := s.api.loadSelectedSecrets(context.Background(), sctx.UserID, sctx.WorkspacePath, sctx.Capabilities.SelectedSecrets)
		if len(userSecrets) > 0 {
			reqMap["decrypted_secrets"] = userSecrets
			s.sessionLogf(sctx, sessionID, "[SCHEDULER] Loaded %d user secrets for multi-agent schedule %s", len(userSecrets), sctx.Schedule.ID)
		}
	}

	// Sequence path: run each message as its own focused turn in the same resumed
	// session, mirroring executeWorkshopJob / runPostRunMonitor. The agent builds on
	// the prior turns' context, and the user watches it progress step by step.
	if len(messages) > 0 {
		s.sessionLogf(sctx, sessionID, "[ORG_PULSE] executeMultiAgentJob for %s (%s): session=%s user=%s running %d-step sequence",
			sctx.Schedule.ID, sctx.Schedule.Name, sessionID, sctx.UserID, len(messages))
		for i, msg := range messages {
			s.sessionLogf(sctx, sessionID, "[ORG_PULSE] step %d/%d: %q", i+1, len(messages), msg)

			stepReq := make(map[string]interface{}, len(reqMap))
			for k, v := range reqMap {
				stepReq[k] = v
			}
			if i == 0 {
				msg = withChiefTaskRunContext(sctx, msg)
			}
			stepReq["query"] = msg

			// Resume the latest prior scheduled thread on the first turn only — later
			// turns already share this run's live session.
			if i == 0 {
				if resumed := s.maybeResumeLatestMultiAgentThread(ctx, sctx, stepReq, sessionID); resumed != "" {
					s.sessionLogf(sctx, sessionID, "[ORG_PULSE] Resuming previous multi-agent schedule thread %s for %s", resumed, sctx.Schedule.ID)
				}
			}

			if err := s.api.startSessionInternal(ctx, stepReq, sessionID, sctx.UserID, nil); err != nil {
				return sessionID, "", fmt.Errorf("multi-agent step %d/%d failed: %w", i+1, len(messages), err)
			}

			// Stamp the schedule name on the first turn for frontend tab labeling;
			// later calls are no-ops (the helper guards an existing Title).
			if i == 0 {
				s.stampScheduleNameOnSession(sessionID, sctx)
			}

			if err := s.waitForWorkshopIdle(ctx, sessionID); err != nil {
				s.sessionLogf(sctx, sessionID, "[ORG_PULSE] step %d/%d idle wait failed: %v", i+1, len(messages), err)
				return sessionID, "", fmt.Errorf("multi-agent step %d/%d idle wait failed: %w", i+1, len(messages), err)
			}
			s.sessionLogf(sctx, sessionID, "[ORG_PULSE] step %d/%d done for %s", i+1, len(messages), sctx.Schedule.ID)
		}
		s.sessionLogf(sctx, sessionID, "[ORG_PULSE] sequence completed for %s", sctx.Schedule.ID)
		return sessionID, "", nil
	}

	// Single-query path (legacy/fallback) — unchanged behavior.
	if resumed := s.maybeResumeLatestMultiAgentThread(ctx, sctx, reqMap, sessionID); resumed != "" {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Resuming previous multi-agent schedule thread %s for %s", resumed, sctx.Schedule.ID)
	}

	s.sessionLogf(sctx, sessionID, "[SCHEDULER] executeMultiAgentJob for %s (%s): session=%s user=%s query=%q",
		sctx.Schedule.ID, sctx.Schedule.Name, sessionID, sctx.UserID, query)

	// Start the session with the user's identity
	runErr := s.api.startSessionInternal(ctx, reqMap, sessionID, sctx.UserID, nil)
	if runErr != nil {
		return sessionID, "", fmt.Errorf("multi-agent session execution failed: %w", runErr)
	}

	s.stampScheduleNameOnSession(sessionID, sctx)

	// Wait for session to complete (no workflow orchestrator, just wait for agent to finish)
	if err := s.waitForSessionComplete(ctx, sessionID); err != nil {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] ⚠️ Multi-agent session wait interrupted for %s: %v", sctx.Schedule.ID, err)
	}

	return sessionID, "", nil
}

// stampScheduleNameOnSession updates the tracked session with the
// schedule's display name + triggered_by=cron so the frontend reconnect
// path can identify this as a scheduled run and label the tab using the
// schedule name instead of falling back to the literal "Workflow".
// Safe to call after startSessionInternal returns — the session is
// already tracked by then.
func (s *SchedulerService) stampScheduleNameOnSession(sessionID string, sctx *ScheduleContext) {
	if sctx == nil || strings.TrimSpace(sctx.Schedule.Name) == "" {
		return
	}
	s.api.activeSessionsMux.Lock()
	defer s.api.activeSessionsMux.Unlock()
	if sess, ok := s.api.activeSessions[sessionID]; ok && sess != nil {
		if sess.Title == "" {
			sess.Title = sctx.Schedule.Name
		}
		if sess.TriggeredBy == "" {
			sess.TriggeredBy = "cron"
		}
	}
}

// waitForSessionComplete polls until a simple/multi-agent session is no longer busy.
func (s *SchedulerService) waitForSessionComplete(ctx context.Context, sessionID string) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if !s.api.sessionIsBusy(sessionID) {
				return nil
			}
		}
	}
}

// applyLLMAndSecretsToReqMap adds LLM config, API keys, secrets, and trigger payload to a request map.
func (s *SchedulerService) applyLLMAndSecretsToReqMap(ctx context.Context, reqMap map[string]interface{}, sctx *ScheduleContext) {
	if sctx.Capabilities.SelectedGlobalSecretNames != nil {
		reqMap["selected_global_secrets"] = sctx.Capabilities.SelectedGlobalSecretNames
	}

	if sctx.Capabilities.LLMConfig != nil {
		llmCfg := sctx.Capabilities.LLMConfig
		provider := strings.TrimSpace(llmCfg.Provider)
		modelID := strings.TrimSpace(llmCfg.ModelID)
		var options map[string]interface{}
		llmConfigSource := ""
		if strings.EqualFold(strings.TrimSpace(sctx.Schedule.WorkshopMode), "optimizer") {
			autoImproveLLM := llmCfg.AutoImproveLLM
			if autoImproveLLM == nil {
				if resolved, ok := workflowtypes.ResolveCodingAgentAutoImproveConfig(llmCfg); ok {
					autoImproveLLM = resolved
				}
			}
			if autoImproveLLM != nil {
				autoImproveProvider := strings.TrimSpace(autoImproveLLM.Provider)
				autoImproveModelID := strings.TrimSpace(autoImproveLLM.ModelID)
				if autoImproveProvider != "" && autoImproveModelID != "" {
					provider = autoImproveProvider
					modelID = autoImproveModelID
					options = autoImproveLLM.Options
					llmConfigSource = llmConfigSourceScheduledAutoImprove
				}
			}
		}
		if provider != "" && modelID != "" {
			primary := map[string]interface{}{
				"provider": provider,
				"model_id": modelID,
			}
			if len(options) > 0 {
				primary["options"] = options
			}
			llmConfig := map[string]interface{}{
				"primary": primary,
			}
			reqMap["llm_config"] = llmConfig
			if llmConfigSource != "" {
				reqMap["llm_config_source"] = llmConfigSource
			}
		}
	}
	// API keys are now handled by MergedProviderAPIKeys in buildWorkshopConfig

	if len(sctx.Schedule.TriggerPayload) > 0 {
		var overrides map[string]interface{}
		if err := json.Unmarshal(sctx.Schedule.TriggerPayload, &overrides); err == nil {
			for k, v := range overrides {
				reqMap[k] = v
			}
		}
	}
}

func applyPrimaryLLMConfigToReqMap(reqMap map[string]interface{}, cfg *workflowtypes.AgentLLMConfig) bool {
	if reqMap == nil || cfg == nil {
		return false
	}
	provider := strings.TrimSpace(cfg.Provider)
	modelID := strings.TrimSpace(cfg.ModelID)
	if provider == "" || modelID == "" {
		return false
	}

	primary := map[string]interface{}{
		"provider": provider,
		"model_id": modelID,
	}
	if len(cfg.Options) > 0 {
		primary["options"] = cfg.Options
	}
	reqMap["llm_config"] = map[string]interface{}{
		"primary": primary,
	}
	return true
}

func cloneStringInterfaceMap(in map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *SchedulerService) applyPulseLLMToReqMap(reqMap map[string]interface{}, sctx *ScheduleContext, sessionID string) {
	if sctx == nil || sctx.Capabilities.LLMConfig == nil {
		return
	}
	pulseLLM := sctx.Capabilities.LLMConfig.PulseLLM
	if pulseLLM == nil {
		if resolved, ok := workflowtypes.ResolveCodingAgentPulseConfig(sctx.Capabilities.LLMConfig); ok {
			pulseLLM = resolved
		}
	}
	if !applyPrimaryLLMConfigToReqMap(reqMap, pulseLLM) {
		return
	}
	reqMap["llm_config_source"] = llmConfigSourceScheduledPulse
	s.sessionLogf(sctx, sessionID, "[PULSE] using configured pulse LLM %s/%s", strings.TrimSpace(pulseLLM.Provider), strings.TrimSpace(pulseLLM.ModelID))
}

func (s *SchedulerService) applyGoalAdvisorLLMToReqMap(reqMap map[string]interface{}, sctx *ScheduleContext, sessionID string) {
	if sctx == nil || sctx.Capabilities.LLMConfig == nil {
		return
	}
	goalAdvisorLLM := sctx.Capabilities.LLMConfig.AutoImproveLLM
	if goalAdvisorLLM == nil {
		if resolved, ok := workflowtypes.ResolveCodingAgentAutoImproveConfig(sctx.Capabilities.LLMConfig); ok {
			goalAdvisorLLM = resolved
		}
	}
	if !applyPrimaryLLMConfigToReqMap(reqMap, goalAdvisorLLM) {
		return
	}
	reqMap["llm_config_source"] = llmConfigSourceScheduledAutoImprove
	s.sessionLogf(sctx, sessionID, "[PULSE] using configured Goal Advisor LLM %s/%s", strings.TrimSpace(goalAdvisorLLM.Provider), strings.TrimSpace(goalAdvisorLLM.ModelID))
}

func (s *SchedulerService) applyChiefOfStaffLLMToReqMap(ctx context.Context, reqMap map[string]interface{}, sctx *ScheduleContext, sessionID string) {
	if sctx == nil || sctx.SourceType != "multi-agent" {
		return
	}
	chiefOfStaffLLM := resolveChiefOfStaffLLMForSchedule(ctx, sctx)
	if !applyPrimaryLLMConfigToReqMap(reqMap, chiefOfStaffLLM) {
		return
	}
	reqMap["llm_config_source"] = llmConfigSourceScheduledChiefOfStaff
	s.sessionLogf(sctx, sessionID, "[SCHEDULER] using configured Chief of Staff LLM %s/%s", strings.TrimSpace(chiefOfStaffLLM.Provider), strings.TrimSpace(chiefOfStaffLLM.ModelID))
}

func resolveChiefOfStaffLLMForSchedule(ctx context.Context, sctx *ScheduleContext) *workflowtypes.AgentLLMConfig {
	if sctx == nil {
		return nil
	}
	if llmCfg := sctx.Capabilities.LLMConfig; llmCfg != nil {
		if llmCfg.ChiefOfStaffLLM != nil {
			return llmCfg.ChiefOfStaffLLM
		}
		if resolved, ok := workflowtypes.ResolveCodingAgentChiefOfStaffConfig(llmCfg); ok {
			return resolved
		}
		return nil
	}

	tierConfig, err := LoadDelegationTierConfig(ctx)
	if err != nil {
		return nil
	}
	return resolveChiefOfStaffLLMFromDelegationConfig(tierConfig)
}

func resolveChiefOfStaffLLMFromDelegationConfig(tierConfig *virtualtools.DelegationTierConfig) *workflowtypes.AgentLLMConfig {
	if tierConfig == nil {
		return nil
	}
	if tierConfig.ChiefOfStaff != nil {
		return agentLLMConfigFromTierModel(tierConfig.ChiefOfStaff)
	}
	for _, candidate := range []*virtualtools.TierModel{tierConfig.Main, tierConfig.High} {
		if cfg := agentLLMConfigFromTierModel(candidate); cfg != nil {
			if resolved, ok := workflowtypes.ResolveCodingAgentChiefOfStaffConfig(&workflowtypes.PresetLLMConfig{Provider: cfg.Provider}); ok {
				return resolved
			}
			return cfg
		}
	}
	return nil
}

func agentLLMConfigFromTierModel(tier *virtualtools.TierModel) *workflowtypes.AgentLLMConfig {
	if tier == nil || strings.TrimSpace(tier.Provider) == "" || strings.TrimSpace(tier.ModelID) == "" {
		return nil
	}
	cfg := &workflowtypes.AgentLLMConfig{
		Provider: strings.TrimSpace(tier.Provider),
		ModelID:  strings.TrimSpace(tier.ModelID),
	}
	if len(tier.Fallbacks) > 0 {
		cfg.Fallbacks = make([]workflowtypes.AgentLLMFallback, 0, len(tier.Fallbacks))
		for _, fallback := range tier.Fallbacks {
			provider := strings.TrimSpace(fallback.Provider)
			modelID := strings.TrimSpace(fallback.ModelID)
			if provider == "" || modelID == "" {
				continue
			}
			cfg.Fallbacks = append(cfg.Fallbacks, workflowtypes.AgentLLMFallback{
				Provider: provider,
				ModelID:  modelID,
			})
		}
	}
	return cfg
}

// buildWorkshopRequest creates the base request map for workshop mode execution.
func (s *SchedulerService) buildWorkshopRequest(ctx context.Context, sctx *ScheduleContext) map[string]interface{} {
	reqMap := map[string]interface{}{
		"agent_mode":                  "workflow_phase",
		"phase_id":                    workflowtypes.WorkflowStatusWorkflowBuilder,
		"preset_query_id":             sctx.WorkflowID,
		"selected_folder":             sctx.WorkspacePath,
		"triggered_by":                "cron",
		"servers":                     sctx.Capabilities.SelectedServers,
		"selected_tools":              sctx.Capabilities.SelectedTools,
		"selected_skills":             sctx.Capabilities.SelectedSkills,
		"browser_mode":                sctx.Capabilities.BrowserMode,
		"use_code_execution_mode":     sctx.Capabilities.UseCodeExecutionMode,
		"disable_live_input_delivery": true,
	}

	s.applyLLMAndSecretsToReqMap(ctx, reqMap, sctx)

	execOpts := map[string]interface{}{
		"run_mode":            "use_same_run",
		"selected_run_folder": "iteration-0",
		"execution_strategy":  "start_from_beginning_no_human",
		// Scheduled runs execute the workflow builder exactly like a normal
		// interactive chat — workshop mode. This keeps the scheduled run on the
		// same mode as the user's interactive sessions, so it natively resumes
		// the workflow's latest thread (same-mode) with no special handling.
		"workshop_mode": "workshop",
	}
	if len(sctx.Schedule.GroupNames) > 0 {
		execOpts["enabled_group_names"] = sctx.Schedule.GroupNames
	}
	reqMap["execution_options"] = execOpts

	return reqMap
}

var schedulerWorkshopIdlePollInterval = 3 * time.Second
var schedulerWorkshopIdleMaxWait = 10 * time.Minute
var schedulerGoalAdvisorIdleMaxWait = 30 * time.Minute

const schedulerWorkshopIdleConsecutiveChecks = 2
const schedulerWorkshopIdleMaxRefreshErrors = 3

// waitForWorkshopIdle polls until all background agents, tracked executions, and
// tmux-backed turns have completed.
func (s *SchedulerService) waitForWorkshopIdle(ctx context.Context, sessionID string) error {
	return s.waitForWorkshopIdleWithMaxWait(ctx, sessionID, schedulerWorkshopIdleMaxWait)
}

func (s *SchedulerService) waitForWorkshopIdleWithMaxWait(ctx context.Context, sessionID string, maxWait time.Duration) error {
	ticker := time.NewTicker(schedulerWorkshopIdlePollInterval)
	defer ticker.Stop()
	var timeout <-chan time.Time
	if maxWait > 0 {
		timer := time.NewTimer(maxWait)
		defer timer.Stop()
		timeout = timer.C
	}

	consecutiveIdleChecks := 0
	consecutiveRefreshErrors := 0
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("workshop idle wait timed out after %s for session %s", maxWait, sessionID)
		case <-ticker.C:
			if err := s.refreshSessionTmuxSnapshotsForIdleCheck(ctx, sessionID); err != nil {
				consecutiveIdleChecks = 0
				consecutiveRefreshErrors++
				if consecutiveRefreshErrors >= schedulerWorkshopIdleMaxRefreshErrors {
					return err
				}
				continue
			}
			consecutiveRefreshErrors = 0
			// Consolidated status — same busy/idle/stopped the UI sees, so the
			// scheduler doesn't fire the next message while the (possibly tmux-
			// backed) agent is still working.
			if !s.api.sessionIsBusy(sessionID) {
				consecutiveIdleChecks++
				if consecutiveIdleChecks >= schedulerWorkshopIdleConsecutiveChecks {
					return nil
				}
				continue
			}
			consecutiveIdleChecks = 0
		}
	}
}

func (s *SchedulerService) refreshSessionTmuxSnapshotsForIdleCheck(ctx context.Context, sessionID string) error {
	if s == nil || s.api == nil {
		return nil
	}
	return s.api.refreshSessionTmuxSnapshotsForIdleCheck(ctx, sessionID)
}

func (api *StreamingAPI) refreshSessionTmuxSnapshotsForIdleCheck(ctx context.Context, sessionID string) error {
	if api == nil || api.terminalStore == nil || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	seenTmuxSessions := map[string]struct{}{}
	for _, snapshot := range api.terminalStore.ListMetadata(sessionID) {
		tmuxSession := strings.TrimSpace(snapshot.TmuxSession)
		if tmuxSession == "" {
			continue
		}
		if _, seen := seenTmuxSessions[tmuxSession]; seen {
			continue
		}
		seenTmuxSessions[tmuxSession] = struct{}{}

		captureCtx, cancel := context.WithTimeout(ctx, terminalTmuxActionTimeout)
		content, err := captureTerminalPaneLines(captureCtx, tmuxSession, terminalDefaultRefreshLines)
		cancel()
		if err != nil {
			if isMissingTmuxTargetError(err) {
				api.terminalStore.MarkStale(snapshot.TerminalID)
				continue
			}
			return fmt.Errorf("refresh tmux snapshot %q: %w", tmuxSession, err)
		}
		api.terminalStore.ReplaceContentWithSource(snapshot.TerminalID, content, "tmux_capture")
	}
	return nil
}

// getOrCreateRuntimeState returns (or creates) the in-memory runtime state for a schedule.
func (s *SchedulerService) getOrCreateRuntimeState(scheduleID string) *ScheduleRuntimeState {
	s.runtimeStatesMu.Lock()
	defer s.runtimeStatesMu.Unlock()
	if state, ok := s.runtimeStates[scheduleID]; ok {
		return state
	}
	state := &ScheduleRuntimeState{}
	s.runtimeStates[scheduleID] = state
	return state
}

// getRuntimeStateLocked returns or creates runtime state. Caller MUST hold runtimeStatesMu write lock.
func (s *SchedulerService) getRuntimeStateLocked(scheduleID string) *ScheduleRuntimeState {
	if state, ok := s.runtimeStates[scheduleID]; ok {
		return state
	}
	state := &ScheduleRuntimeState{}
	s.runtimeStates[scheduleID] = state
	return state
}

func runningWorkflowScheduleInSetLocked(runtimeStates map[string]*ScheduleRuntimeState, scheduleIDs []string, ignoreScheduleID string) (string, string) {
	for _, scheduleID := range scheduleIDs {
		if scheduleID == "" || scheduleID == ignoreScheduleID {
			continue
		}
		state := runtimeStates[scheduleID]
		if state == nil || state.LastStatus != "running" {
			continue
		}
		return scheduleID, state.LastSessionID
	}
	return "", ""
}

func (s *SchedulerService) findActiveExecutionForWorkspace(workspacePath string) *ActiveWorkflowExecution {
	if s == nil || s.api == nil || strings.TrimSpace(workspacePath) == "" {
		return nil
	}

	tracked := s.api.findRunningTrackedExecutionForWorkspace(workspacePath)
	if tracked == nil {
		return nil
	}
	active := trackedExecutionToActive(tracked)
	return &active
}

// ScheduleSearchResult holds the result of finding a schedule by ID.
type ScheduleSearchResult struct {
	SourceType    string // "workflow" or "multi-agent"
	WorkspacePath string // For workflow schedules
	Manifest      *WorkflowManifest
	UserID        string // For multi-agent schedules
	ScheduleFile  *MultiAgentScheduleFile
	Index         int
}

// findScheduleByIDAny scans both workflow manifests and multi-agent schedule files.
func findScheduleByIDAny(ctx context.Context, scheduleID string) (*ScheduleSearchResult, error) {
	// Try workflow manifests first
	wsPath, manifest, idx, err := findScheduleByID(ctx, scheduleID)
	if err == nil {
		return &ScheduleSearchResult{
			SourceType:    "workflow",
			WorkspacePath: wsPath,
			Manifest:      manifest,
			Index:         idx,
		}, nil
	}

	// Try multi-agent schedules
	userID, f, idx, err := findMultiAgentScheduleByID(ctx, scheduleID)
	if err == nil {
		return &ScheduleSearchResult{
			SourceType:   "multi-agent",
			UserID:       userID,
			ScheduleFile: f,
			Index:        idx,
		}, nil
	}

	return nil, fmt.Errorf("schedule %s not found", scheduleID)
}

func findBuiltinMultiAgentScheduleForUser(ctx context.Context, userID, scheduleID string) (*ScheduleSearchResult, error) {
	if strings.TrimSpace(userID) == "" {
		userID = GetDefaultUserID()
	}

	sched, ok := FindDefaultBuiltinSchedule(scheduleID)
	if !ok {
		return nil, fmt.Errorf("built-in schedule %s not found", scheduleID)
	}

	f, _, err := ReadMultiAgentSchedules(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to read multi-agent schedules for user %s: %w", userID, err)
	}

	f.Schedules = append(f.Schedules, sched)
	return &ScheduleSearchResult{
		SourceType:   "multi-agent",
		UserID:       userID,
		ScheduleFile: f,
		Index:        len(f.Schedules) - 1,
	}, nil
}

// findScheduleByID scans all workspace manifests to find a schedule by ID.
// Returns (workspacePath, manifest, scheduleIndex, error).
func findScheduleByID(ctx context.Context, scheduleID string) (string, *WorkflowManifest, int, error) {
	discovered, err := DiscoverWorkflowManifests(ctx)
	if err != nil {
		return "", nil, 0, fmt.Errorf("cannot scan workflow directory: %w", err)
	}

	for _, item := range discovered {
		for i, sched := range item.Manifest.Schedules {
			if sched.ID == scheduleID {
				return item.WorkspacePath, item.Manifest, i, nil
			}
		}
	}

	return "", nil, 0, fmt.Errorf("schedule %s not found in any manifest", scheduleID)
}

// getNextRunTime calculates the next scheduled run time.
func getNextRunTime(cronExpr string, timezone string) *time.Time {
	loc, err := time.LoadLocation(scheduleTimezoneOrDefault(timezone))
	if err != nil {
		loc = time.UTC
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(cronExpr)
	if err != nil {
		return nil
	}

	next := schedule.Next(time.Now().In(loc)).UTC()
	return &next
}

func buildScheduleCronExpression(cronExpr string, timezone string) string {
	return fmt.Sprintf("CRON_TZ=%s %s", scheduleTimezoneOrDefault(timezone), cronExpr)
}

func scheduleTypeOrDefault(scheduleType string) string {
	if scheduleType == "" {
		return "cron"
	}
	return scheduleType
}

func calendarItemRunTime(sched WorkflowSchedule, item CalendarScheduleItem) (time.Time, error) {
	loc, err := time.LoadLocation(scheduleTimezoneOrDefault(sched.Timezone))
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone %q: %w", sched.Timezone, err)
	}
	if item.Date == "" || item.Time == "" {
		return time.Time{}, fmt.Errorf("calendar item date and time are required")
	}
	local, err := time.ParseInLocation("2006-01-02 15:04", item.Date+" "+item.Time, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid calendar item %q %q: expected date YYYY-MM-DD and time HH:MM", item.Date, item.Time)
	}
	return local.UTC(), nil
}

func getNextRunTimeForCalendar(sched WorkflowSchedule) *time.Time {
	now := time.Now().UTC()
	var next *time.Time
	for _, item := range sched.CalendarItems {
		runAt, err := calendarItemRunTime(sched, item)
		if err != nil || !runAt.After(now) {
			continue
		}
		if next == nil || runAt.Before(*next) {
			runAtCopy := runAt
			next = &runAtCopy
		}
	}
	return next
}

func scheduleWithCalendarItem(sched WorkflowSchedule, item CalendarScheduleItem) WorkflowSchedule {
	sched.TriggerPayload = item.TriggerPayload
	if len(item.Messages) > 0 {
		sched.Messages = item.Messages
	}
	return sched
}

func ValidateScheduleTimezone(timezone string) error {
	if timezone == "" {
		return fmt.Errorf("timezone is required; use an IANA timezone like UTC, Asia/Kolkata, or America/New_York")
	}
	if timezone != "UTC" && !strings.Contains(timezone, "/") {
		return fmt.Errorf("invalid timezone %q: use an IANA timezone like UTC, Asia/Kolkata, or America/New_York; abbreviations like EST, PST, or IST are not accepted", timezone)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("invalid timezone %q: use an IANA timezone like UTC, Asia/Kolkata, or America/New_York", timezone)
	}
	return nil
}

// ValidateCronExpression validates a 5-field cron expression.
func ValidateCronExpression(expr string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}
