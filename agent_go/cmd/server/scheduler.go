package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"mcp-agent-builder-go/agent_go/pkg/workflowtypes"
)

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

	envFilter := loadSchedulerWorkflowFilter()
	if len(envFilter.rawAllow) > 0 {
		scheduleLogf("[SCHEDULER] Workflow allowlist active (SCHEDULER_ALLOWED_WORKFLOWS): %s", strings.Join(envFilter.rawAllow, ", "))
	}
	if len(envFilter.rawBlock) > 0 {
		scheduleLogf("[SCHEDULER] Workflow blocklist active (SCHEDULER_BLOCKED_WORKFLOWS): %s", strings.Join(envFilter.rawBlock, ", "))
	}
	if len(envFilter.rawAllowUsers) > 0 {
		scheduleLogf("[SCHEDULER] Multi-agent user allowlist active (SCHEDULER_ALLOWED_USERS): %s", strings.Join(envFilter.rawAllowUsers, ", "))
	}
	if len(envFilter.rawBlockUsers) > 0 {
		scheduleLogf("[SCHEDULER] Multi-agent user blocklist active (SCHEDULER_BLOCKED_USERS): %s", strings.Join(envFilter.rawBlockUsers, ", "))
	}

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
			if !sched.Enabled {
				continue
			}
			sctx := buildScheduleContext(wf.WorkspacePath, wf.Manifest, sched)
			if err := s.LoadSchedule(sctx); err != nil {
				scheduleLogf("[SCHEDULER] Failed to load schedule %s (%s): %v", sched.ID, sched.Name, err)
			} else {
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
				if !sched.Enabled {
					continue
				}
				sctx := buildMultiAgentScheduleContext(ma.UserID, sched, ma.ScheduleFile.Capabilities)
				if err := s.LoadSchedule(sctx); err != nil {
					scheduleLogf("[SCHEDULER] Failed to load multi-agent schedule %s (%s) for user %s: %v", sched.ID, sched.Name, ma.UserID, err)
				} else {
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
	envFilter := loadSchedulerWorkflowFilter()

	for _, ma := range maScheds {
		// Skip users excluded by env filter to avoid log spam every rescan tick.
		if !envFilter.IsUserAllowed(ma.UserID) {
			for _, sched := range MergeBuiltinSchedules(ma.ScheduleFile.Schedules) {
				discovered[sched.ID] = true
			}
			continue
		}

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

	if !sched.Enabled {
		return nil
	}

	// Env filter: skip registration for workflows or users excluded on this
	// machine. Manual TriggerNow still works (mirrors SCHEDULER_ENABLED=false).
	envFilter := loadSchedulerWorkflowFilter()
	if sctx.SourceType == "workflow" {
		if !envFilter.IsWorkflowAllowed(sctx.WorkflowID, sctx.WorkflowLabel, sctx.WorkspacePath) {
			s.logf(sctx, "[SCHEDULER] Skipping schedule %s (%s) — workflow %q filtered by env (SCHEDULER_ALLOWED_WORKFLOWS / SCHEDULER_BLOCKED_WORKFLOWS)",
				sched.ID, sched.Name, sctx.WorkflowLabel)
			return nil
		}
	} else if sctx.SourceType == "multi-agent" {
		if !envFilter.IsUserAllowed(sctx.UserID) {
			s.logf(sctx, "[SCHEDULER] Skipping multi-agent schedule %s (%s) — user %q filtered by env (SCHEDULER_ALLOWED_USERS / SCHEDULER_BLOCKED_USERS)",
				sched.ID, sched.Name, sctx.UserID)
			return nil
		}
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

	for _, sched := range f.Schedules {
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
	for i := range f.Schedules {
		if f.Schedules[i].ID == scheduleID {
			sched = &f.Schedules[i]
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
	if sctx.SourceType == "multi-agent" {
		f, exists, err := ReadMultiAgentSchedules(context.Background(), sctx.UserID)
		if err != nil || !exists {
			s.logf(sctx, "[SCHEDULER] ❌ Failed to reload multi-agent schedules for user %s: %v", sctx.UserID, err)
			return
		}
		var currentSched *WorkflowSchedule
		for i := range f.Schedules {
			if f.Schedules[i].ID == schedID {
				currentSched = &f.Schedules[i]
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

	// Update runtime state
	state.LastStatus = status
	state.LastSessionID = sessionID
	state.LastError = errMsg
	state.LastDurationMs = &durationMs
	state.NextRunAt = nextRun
	state.RunCount++
	if status == "error" {
		state.ConsecutiveFailures++
	} else {
		state.ConsecutiveFailures = 0
	}

	// Update run history entry
	if sctx.SourceType == "multi-agent" {
		if err := UpdateMultiAgentScheduleRun(ctx, sctx.UserID, runID, status, errMsg, &durationMs, sessionID); err != nil {
			s.sessionLogf(sctx, sessionID, "[SCHEDULER] Failed to update multi-agent run entry for %s: %v", schedID, err)
		}
	} else {
		if err := UpdateScheduleRun(ctx, sctx.WorkspacePath, runID, status, errMsg, &durationMs, runFolder, sessionID); err != nil {
			s.sessionLogf(sctx, sessionID, "[SCHEDULER] Failed to update run entry for %s: %v", schedID, err)
		}
	}

	return sessionID, execErr
}

// executeJob builds a session request from the manifest and runs it.
// Returns (sessionID, runFolder, error).
func (s *SchedulerService) executeJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, string, error) {
	// Dispatch to workshop or multi-agent mode if configured
	if sctx.Schedule.Mode == "workshop" {
		return s.executeWorkshopJob(ctx, sctx, runID)
	}
	if sctx.Schedule.Mode == "multi-agent" || sctx.SourceType == "multi-agent" {
		return s.executeMultiAgentJob(ctx, sctx, runID)
	}

	// Deprecated: headless workflow execution (running a workflow without the
	// Workflow Builder chat). The supported path is the Workflow Builder chat
	// (workshop mode → agent_mode "workflow_phase"); this orchestrator-only
	// run-without-chat branch is kept for backward compatibility.
	query := sctx.WorkflowLabel
	if query == "" {
		query = "Execute workflow"
	}

	reqMap := map[string]interface{}{
		"query":                   query,
		"agent_mode":              "workflow", // deprecated: headless run; use "workflow_phase" (builder chat)
		"selected_folder":         sctx.WorkspacePath,
		"triggered_by":            "cron",
		"servers":                 sctx.Capabilities.SelectedServers,
		"selected_tools":          sctx.Capabilities.SelectedTools,
		"selected_skills":         sctx.Capabilities.SelectedSkills,
		"browser_mode":            sctx.Capabilities.BrowserMode,
		"use_code_execution_mode": sctx.Capabilities.UseCodeExecutionMode,
	}

	s.applyLLMAndSecretsToReqMap(ctx, reqMap, sctx)

	// Execution options — always use iteration-0 (controller backs up previous run automatically)
	execOpts := map[string]interface{}{
		"run_mode":            "use_same_run",
		"selected_run_folder": "iteration-0",
		"execution_strategy":  "start_from_beginning",
	}
	if len(sctx.Schedule.GroupNames) > 0 {
		execOpts["enabled_group_names"] = sctx.Schedule.GroupNames
	}
	reqMap["execution_options"] = execOpts

	// Generate session ID
	sessionID := s.newScheduleSessionID(sctx)

	// Update runtime state with session
	state := s.getOrCreateRuntimeState(sctx.Schedule.ID)
	state.LastSessionID = sessionID

	// Update run entry with session_id immediately
	if runID != "" {
		_ = UpdateScheduleRun(ctx, sctx.WorkspacePath, runID, "running", "", nil, "", sessionID)
	}

	s.sessionLogf(sctx, sessionID, "[SCHEDULER] executeJob for %s (%s): session=%s workspace=%s",
		sctx.Schedule.ID, sctx.Schedule.Name, sessionID, sctx.WorkspacePath)

	runErr := s.api.startSessionInternal(ctx, reqMap, sessionID, "", nil)
	if runErr != nil {
		return sessionID, "", fmt.Errorf("session execution failed: %w", runErr)
	}

	// Stamp the schedule's display name onto the tracked session so the
	// frontend can label the reconnect tab "<Schedule Name>" instead of
	// the generic "Workflow" fallback. QueryRequest doesn't carry a
	// title field, so we update activeSessions directly here after the
	// session has been tracked by handleQuery.
	s.stampScheduleNameOnSession(sessionID, sctx)

	// Wait for workflow orchestrator to finish (background goroutines)
	detectedFolder, waitErr := s.waitForWorkflowComplete(ctx, sessionID, runID, sctx.WorkspacePath)
	if waitErr != nil {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] ⚠️ Workflow wait interrupted for %s: %v", sctx.Schedule.ID, waitErr)
	}

	return sessionID, detectedFolder, nil
}

// executeWorkshopJob runs a workflow via the workshop builder path (workflow_phase mode).
func (s *SchedulerService) executeWorkshopJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, string, error) {
	messages := sctx.Schedule.Messages
	if len(messages) == 0 {
		messages = []string{"Run the full workflow using run_full_workflow tool."}
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

	// Auto-backup: after the run's work turns complete, run one more turn that backs
	// up the workflow's state per the backup-strategy skill. This fires on every
	// workshop scheduled run so unattended runs persist their state off-box without
	// the builder having to author a backup message. Runs in the same session so the
	// agent has full context of what it just did (e.g. for a meaningful commit message).
	//
	// Critical: a backup failure must NOT fail the run — the actual work already
	// completed, so backup errors are logged and swallowed, never returned.
	backupMsg := "The scheduled run is complete. Now back up this workflow per the backup-strategy skill: " +
		"call get_reference_doc(kind=\"backup-strategy\") and follow it to commit and push the workflow's state " +
		"(workflow.json, planning/, db/, knowledgebase/, learnings/, reports) to its git backup remote, routing " +
		"any large/binary artifacts to the configured large-file backend. This run is UNATTENDED: do NOT ask for " +
		"confirmation and make every decision autonomously. If no backup remote is configured yet, set one up per " +
		"the playbook where possible; if a step is genuinely impossible without an operator, log what you skipped " +
		"and finish without erroring."
	s.sessionLogf(sctx, sessionID, "[SCHEDULER] Workshop auto-backup turn for %s", sctx.Schedule.ID)
	backupReq := make(map[string]interface{})
	for k, v := range baseReqMap {
		backupReq[k] = v
	}
	backupReq["query"] = backupMsg
	if err := s.api.startSessionInternal(ctx, backupReq, sessionID, "", nil); err != nil {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] ⚠️ auto-backup turn failed to start (run still complete): %v", err)
	} else if err := s.waitForWorkshopIdle(ctx, sessionID); err != nil {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] ⚠️ auto-backup turn idle wait failed (run still complete): %v", err)
	} else {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] ✅ Workshop auto-backup turn completed for %s", sctx.Schedule.ID)
	}

	// Previously auto-generated a static markdown report here via the report agent.
	// The dynamic report (design doc §2) is a live frontend view over db/ + graph.json;
	// there is no post-run artifact to produce, so scheduled runs now finish without a
	// report side-effect. Users open the report in the UI whenever they want.

	s.sessionLogf(sctx, sessionID, "[SCHEDULER] ✅ Workshop execution completed for %s, session=%s, folder=%s", sctx.Schedule.ID, sessionID, runFolder)
	return sessionID, runFolder, nil
}

// executeMultiAgentJob runs a multi-agent chat session with the configured query.
func (s *SchedulerService) executeMultiAgentJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, string, error) {
	query := strings.TrimSpace(sctx.Schedule.Query)
	if query == "" {
		return "", "", fmt.Errorf("multi-agent schedule %s has no query", sctx.Schedule.ID)
	}

	sessionID := s.newScheduleSessionID(sctx)

	state := s.getOrCreateRuntimeState(sctx.Schedule.ID)
	state.LastSessionID = sessionID

	if runID != "" {
		_ = UpdateMultiAgentScheduleRun(ctx, sctx.UserID, runID, "running", "", nil, sessionID)
	}

	reqMap := map[string]interface{}{
		"query":        query,
		"agent_mode":   "simple",
		"triggered_by": "cron",
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

	// Load user-level secrets if configured
	if len(sctx.Capabilities.SelectedSecrets) > 0 && sctx.UserID != "" {
		userSecrets := s.api.loadSelectedSecrets(context.Background(), sctx.UserID, sctx.WorkspacePath, sctx.Capabilities.SelectedSecrets)
		if len(userSecrets) > 0 {
			reqMap["decrypted_secrets"] = userSecrets
			s.sessionLogf(sctx, sessionID, "[SCHEDULER] Loaded %d user secrets for multi-agent schedule %s", len(userSecrets), sctx.Schedule.ID)
		}
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
			hasRunning := s.api.bgAgentRegistry.HasRunningAgents(sessionID)
			isBusy := s.api.isSessionBusy(sessionID)
			if !hasRunning && !isBusy {
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
		if llmCfg.Provider != "" && llmCfg.ModelID != "" {
			llmConfig := map[string]interface{}{
				"primary": map[string]interface{}{
					"provider": llmCfg.Provider,
					"model_id": llmCfg.ModelID,
				},
			}
			reqMap["llm_config"] = llmConfig
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

// buildWorkshopRequest creates the base request map for workshop mode execution.
func (s *SchedulerService) buildWorkshopRequest(ctx context.Context, sctx *ScheduleContext) map[string]interface{} {
	reqMap := map[string]interface{}{
		"agent_mode":              "workflow_phase",
		"phase_id":                workflowtypes.WorkflowStatusWorkflowBuilder,
		"preset_query_id":         sctx.WorkflowID,
		"selected_folder":         sctx.WorkspacePath,
		"triggered_by":            "cron",
		"servers":                 sctx.Capabilities.SelectedServers,
		"selected_tools":          sctx.Capabilities.SelectedTools,
		"selected_skills":         sctx.Capabilities.SelectedSkills,
		"browser_mode":            sctx.Capabilities.BrowserMode,
		"use_code_execution_mode": sctx.Capabilities.UseCodeExecutionMode,
	}

	s.applyLLMAndSecretsToReqMap(ctx, reqMap, sctx)

	execOpts := map[string]interface{}{
		"run_mode":            "use_same_run",
		"selected_run_folder": "iteration-0",
		"execution_strategy":  "start_from_beginning_no_human",
		"workshop_mode": func() string {
			if sctx.Schedule.WorkshopMode != "" {
				return sctx.Schedule.WorkshopMode
			}
			return "run"
		}(),
	}
	if len(sctx.Schedule.GroupNames) > 0 {
		execOpts["enabled_group_names"] = sctx.Schedule.GroupNames
	}
	reqMap["execution_options"] = execOpts

	return reqMap
}

// waitForWorkshopIdle polls until all background agents and synthetic turns have completed.
func (s *SchedulerService) waitForWorkshopIdle(ctx context.Context, sessionID string) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			hasRunning := s.api.bgAgentRegistry.HasRunningAgents(sessionID)
			isBusy := s.api.isSessionBusy(sessionID)
			if !hasRunning && !isBusy {
				return nil
			}
		}
	}
}

// waitForWorkflowComplete polls until all workflow orchestrator queries have finished.
func (s *SchedulerService) waitForWorkflowComplete(ctx context.Context, sessionID string, runID string, workspacePath string) (string, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	runFolderWritten := false
	detectedFolder := ""

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			// Try to capture the run folder from the unified execution tracker.
			if !runFolderWritten {
				if folder := s.api.latestTrackedRunFolderForSession(sessionID); folder != "" {
					detectedFolder = folder
					runFolderWritten = true
					if runID != "" {
						_ = UpdateScheduleRun(ctx, workspacePath, runID, "running", "", nil, folder, "")
						scheduleLogf("[SCHEDULER] 📁 Detected run folder: %s (session %s)", folder, sessionID)
					}
				}
			}

			// Check if all queries are done
			s.api.sessionQueryIDMux.RLock()
			queryIDs := s.api.sessionQueryIDs[sessionID]
			s.api.sessionQueryIDMux.RUnlock()

			if len(queryIDs) == 0 {
				// All orchestrator queries finished
				if detectedFolder == "" {
					detectedFolder = s.api.latestTrackedRunFolderForSession(sessionID)
				}
				return detectedFolder, nil
			}

			if session, exists := s.api.getActiveSession(sessionID); exists {
				switch session.Status {
				case "completed":
					if detectedFolder == "" {
						detectedFolder = s.api.latestTrackedRunFolderForSession(sessionID)
					}
					return detectedFolder, nil
				case "error", "stopped", "inactive", "dismissed":
					if detectedFolder == "" {
						detectedFolder = s.api.latestTrackedRunFolderForSession(sessionID)
					}
					return detectedFolder, fmt.Errorf("session ended with status %s", session.Status)
				}
			}
		}
	}
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
