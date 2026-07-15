package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	virtualtools "github.com/manishiitg/coding-agent-loop/agent_go/cmd/server/virtual-tools"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/fsutil"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/schedulerstate"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/workflowtypes"
	"github.com/robfig/cron/v3"
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
	CalendarItem  *CalendarScheduleItem
}

const scheduleScopeSeparator = "\x1f"

func workflowScheduleRuntimeKey(workspacePath, scheduleID string) string {
	return strings.Join([]string{"workflow", filepath.Clean(strings.TrimSpace(workspacePath)), strings.TrimSpace(scheduleID)}, scheduleScopeSeparator)
}

func multiAgentScheduleRuntimeKey(userID, scheduleID string) string {
	return strings.Join([]string{"multi-agent", strings.TrimSpace(userID), strings.TrimSpace(scheduleID)}, scheduleScopeSeparator)
}

func scheduleRuntimeKey(sctx *ScheduleContext) string {
	if sctx == nil {
		return ""
	}
	if sctx.SourceType == "multi-agent" {
		return multiAgentScheduleRuntimeKey(sctx.UserID, sctx.Schedule.ID)
	}
	return workflowScheduleRuntimeKey(sctx.WorkspacePath, sctx.Schedule.ID)
}

func scheduleRuntimeKeyHasID(key, scheduleID string) bool {
	return strings.HasSuffix(key, scheduleScopeSeparator+strings.TrimSpace(scheduleID))
}

func scheduleStateLockKeyFromRuntimeKey(runtimeKey string) string {
	parts := strings.Split(runtimeKey, scheduleScopeSeparator)
	if len(parts) < 3 || parts[0] != "workflow" {
		return runtimeKey
	}
	return strings.Join(parts[:2], scheduleScopeSeparator)
}

func scheduleStateScope(sctx *ScheduleContext) (scopeType, scopeID, lockKey string) {
	if sctx != nil && sctx.SourceType == "multi-agent" {
		scopeID = strings.TrimSpace(sctx.UserID)
		return "multi-agent", scopeID, strings.Join([]string{"multi-agent", scopeID, strings.TrimSpace(sctx.Schedule.ID)}, scheduleScopeSeparator)
	}
	if sctx != nil {
		scopeID = filepath.Clean(strings.TrimSpace(sctx.WorkspacePath))
	}
	return "workflow", scopeID, strings.Join([]string{"workflow", scopeID}, scheduleScopeSeparator)
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
	ActiveRunID         string     `json:"active_run_id,omitempty"`
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
	jobs map[string]*registeredJob // scoped schedule key → job
	// scheduleFingerprints tracks the persisted config loaded for each scoped
	// schedule, including disabled and calendar schedules with no future items.
	scheduleFingerprints map[string]string

	stateStoreMu sync.RWMutex
	stateStore   *schedulerstate.Store
	runCancelsMu sync.Mutex
	runCancels   map[string]context.CancelFunc

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
		api:                  api,
		jobs:                 make(map[string]*registeredJob),
		scheduleFingerprints: make(map[string]string),
		runCancels:           make(map[string]context.CancelFunc),
		runtimeStates:        make(map[string]*ScheduleRuntimeState),
		workspaceIndex:       make(map[string]string),
		userIndex:            make(map[string]string),
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
	s.stateStoreMu.Lock()
	if s.stateStore == nil {
		storePath := filepath.Join(fsutil.WorkspaceDocsRoot(), "_system", "schedule-state.sqlite")
		store, err := schedulerstate.Open(storePath)
		if err != nil {
			s.stateStoreMu.Unlock()
			return fmt.Errorf("initialize schedule state store: %w", err)
		}
		s.stateStore = store
	}
	if interrupted, err := s.stateStore.InterruptActiveRuns(ctx, "interrupted: server restarted", time.Now().UTC()); err != nil {
		s.stateStoreMu.Unlock()
		return fmt.Errorf("reconcile interrupted schedule runs: %w", err)
	} else if interrupted > 0 {
		scheduleLogf("[SCHEDULER] Marked %d durable schedule run(s) interrupted after restart", interrupted)
	}
	s.stateStoreMu.Unlock()

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
						job.lastFired = t.Truncate(time.Minute)
					}
					parts = append(parts, fmt.Sprintf("%s next=%s", sid, job.cronSched.Next(t).UTC().Format(time.RFC3339)))
				} else if job.runAt != nil {
					if !job.runAt.After(t) && job.lastFired.Before(*job.runAt) {
						toFire = append(toFire, job)
						job.lastFired = t.Truncate(time.Minute)
					}
					parts = append(parts, fmt.Sprintf("%s at=%s", sid, job.runAt.UTC().Format(time.RFC3339)))
				}
			}
			s.mu.Unlock()

			scheduleLogf("[SCHEDULER] ❤️ heartbeat now=%s gap=%s jobs=%d due=%d | %s",
				t.Format(time.RFC3339), gap.Round(time.Second), len(parts), len(toFire), strings.Join(parts, ", "))

			for _, job := range toFire {
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

	// Build set of all discovered scoped schedule keys.
	discovered := make(map[string]bool)

	for _, ma := range maScheds {
		for _, sched := range MergeBuiltinSchedules(ma.ScheduleFile.Schedules) {
			sctx := buildMultiAgentScheduleContext(ma.UserID, sched, ma.ScheduleFile.Capabilities)
			key := scheduleRuntimeKey(sctx)
			discovered[key] = true

			fingerprint := scheduleConfigFingerprint(sctx)
			s.mu.Lock()
			loadedFingerprint, isKnown := s.scheduleFingerprints[key]
			s.mu.Unlock()

			if sched.Enabled && (!isKnown || loadedFingerprint != fingerprint) {
				// New, re-enabled, or changed schedule.
				if err := s.LoadSchedule(sctx); err != nil {
					scheduleLogf("[SCHEDULER] Rescan: failed to load multi-agent schedule %s: %v", sched.ID, err)
				} else {
					scheduleLogf("[SCHEDULER] Rescan: loaded new or changed multi-agent schedule %s (%s) for user %s", sched.ID, sched.Name, ma.UserID)
				}
			} else if !sched.Enabled && (!isKnown || loadedFingerprint != fingerprint) {
				// Newly disabled or changed while disabled. LoadSchedule removes any
				// live registration and remembers this exact disabled config.
				if err := s.LoadSchedule(sctx); err != nil {
					scheduleLogf("[SCHEDULER] Rescan: failed to disable multi-agent schedule %s: %v", sched.ID, err)
				} else {
					scheduleLogf("[SCHEDULER] Rescan: removed disabled multi-agent schedule %s", sched.ID)
				}
			}
		}
	}

	// Remove schedules that were deleted from files
	s.userIndexMu.RLock()
	toRemove := []string{}
	for key := range s.userIndex {
		if !discovered[key] {
			toRemove = append(toRemove, key)
		}
	}
	s.userIndexMu.RUnlock()

	for _, key := range toRemove {
		_ = s.removeJobByKey(key)
		scheduleLogf("[SCHEDULER] Rescan: removed deleted multi-agent schedule %s", key)
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
	s.runCancelsMu.Lock()
	runCancels := s.runCancels
	s.runCancels = make(map[string]context.CancelFunc)
	s.runCancelsMu.Unlock()
	for _, cancel := range runCancels {
		cancel()
	}
	s.mu.Lock()
	s.jobs = make(map[string]*registeredJob)
	s.scheduleFingerprints = make(map[string]string)
	s.mu.Unlock()
	s.stateStoreMu.Lock()
	if s.stateStore != nil {
		if err := s.stateStore.Close(); err != nil {
			scheduleLogf("[SCHEDULER] Failed to close schedule state store: %v", err)
		}
		s.stateStore = nil
	}
	s.stateStoreMu.Unlock()
	scheduleLogf("[SCHEDULER] Stopped")
}

// LoadSchedule registers a schedule for wall-clock evaluation from a ScheduleContext.
func (s *SchedulerService) LoadSchedule(sctx *ScheduleContext) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sched := sctx.Schedule
	runtimeKey := scheduleRuntimeKey(sctx)
	if s.scheduleFingerprints == nil {
		s.scheduleFingerprints = make(map[string]string)
	}

	// Remove existing registration if any.
	delete(s.jobs, runtimeKey)
	calendarPrefix := runtimeKey + "__cal__"
	for key := range s.jobs {
		if strings.HasPrefix(key, calendarPrefix) {
			delete(s.jobs, key)
		}
	}

	// Update workspace index
	s.workspaceIndexMu.Lock()
	s.workspaceIndex[runtimeKey] = sctx.WorkspacePath
	s.workspaceIndexMu.Unlock()

	if sctx.SourceType == "workflow" {
		if err := EnsureWorkflowScheduleExecutionTracker(context.Background(), sctx.WorkspacePath, sched, time.Now().UTC()); err != nil {
			s.logf(sctx, "[SCHEDULER] Warning: failed to initialize execution history for %s: %v", sched.ID, err)
		}
	}

	// Update user index for multi-agent schedules
	if sctx.UserID != "" {
		s.userIndexMu.Lock()
		s.userIndex[runtimeKey] = sctx.UserID
		s.userIndexMu.Unlock()
	}

	if !sched.Enabled {
		s.scheduleFingerprints[runtimeKey] = scheduleConfigFingerprint(sctx)
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
			itemSctx.CalendarItem = &itemCopy
			calID := fmt.Sprintf("%s__cal__%s_%s", runtimeKey, item.Date, item.Time)
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

		s.jobs[runtimeKey] = &registeredJob{
			sctx:      &sctxCopy,
			cronSched: cronSched,
			lastFired: time.Now().Add(-30 * time.Second), // don't fire immediately on registration
		}
		nextRun = getNextRunTime(sched.CronExpression, sched.Timezone)
	}

	// Initialize runtime state with next run.
	s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		state.NextRunAt = nextRun
	})
	s.scheduleFingerprints[runtimeKey] = scheduleConfigFingerprint(sctx)

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
	return s.removeJobByKey(workflowScheduleRuntimeKey(workspacePath, scheduleID))
}

// ReloadMultiAgentSchedule reloads a multi-agent schedule after it's been updated.
func (s *SchedulerService) ReloadMultiAgentSchedule(ctx context.Context, userID string, scheduleID string) error {
	f, exists, err := ReadMultiAgentSchedules(ctx, userID)
	if err != nil || !exists {
		return s.removeJobByKey(multiAgentScheduleRuntimeKey(userID, scheduleID))
	}

	for _, sched := range MergeBuiltinSchedules(f.Schedules) {
		if sched.ID == scheduleID {
			return s.LoadSchedule(buildMultiAgentScheduleContext(userID, sched, f.Capabilities))
		}
	}

	return s.removeJobByKey(multiAgentScheduleRuntimeKey(userID, scheduleID))
}

func (s *SchedulerService) removeJobByKey(key string) error {
	s.mu.Lock()
	delete(s.jobs, key)
	delete(s.scheduleFingerprints, key)
	// Also remove calendar sub-jobs.
	prefix := key + "__cal__"
	for k := range s.jobs {
		if strings.HasPrefix(k, prefix) {
			delete(s.jobs, k)
		}
	}
	s.mu.Unlock()

	s.workspaceIndexMu.Lock()
	delete(s.workspaceIndex, key)
	s.workspaceIndexMu.Unlock()

	s.userIndexMu.Lock()
	delete(s.userIndex, key)
	s.userIndexMu.Unlock()

	s.runtimeStatesMu.Lock()
	if state := s.runtimeStates[key]; state == nil || state.ActiveRunID == "" {
		delete(s.runtimeStates, key)
	}
	s.runtimeStatesMu.Unlock()

	return nil
}

// RemoveJob removes a schedule only when its ID resolves to one loaded scope.
// Scoped callers should use ReloadSchedule/ReloadMultiAgentSchedule or the
// dedicated helpers below so a copied schedule cannot remove another scope.
func (s *SchedulerService) RemoveJob(scheduleID string) error {
	keys := s.loadedScheduleKeys(scheduleID)
	if len(keys) == 0 {
		return nil
	}
	if len(keys) > 1 {
		return fmt.Errorf("schedule ID %q is ambiguous across %d scopes", scheduleID, len(keys))
	}
	return s.removeJobByKey(keys[0])
}

func (s *SchedulerService) RemoveWorkflowJob(workspacePath, scheduleID string) error {
	return s.removeJobByKey(workflowScheduleRuntimeKey(workspacePath, scheduleID))
}

func (s *SchedulerService) RemoveMultiAgentJob(userID, scheduleID string) error {
	return s.removeJobByKey(multiAgentScheduleRuntimeKey(userID, scheduleID))
}

func (s *SchedulerService) loadedScheduleKeys(scheduleID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, 1)
	for key, job := range s.jobs {
		if strings.Contains(key, "__cal__") || job == nil || job.sctx == nil || job.sctx.Schedule.ID != scheduleID {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// GetRuntimeState is the legacy unscoped lookup. It returns state only when the
// schedule ID resolves to one loaded scope; scoped callers must use the methods
// below so copied workflows and per-user built-ins cannot collide.
func (s *SchedulerService) GetRuntimeState(scheduleID string) ScheduleRuntimeState {
	keys := s.runtimeKeysForScheduleID(scheduleID)
	if len(keys) == 1 {
		return s.getRuntimeStateByKey(keys[0])
	}
	// Preserve tests and pre-migration in-memory state that used a bare key.
	return s.getRuntimeStateByKey(scheduleID)
}

func (s *SchedulerService) GetRuntimeStateForWorkflow(workspacePath, scheduleID string) ScheduleRuntimeState {
	key := workflowScheduleRuntimeKey(workspacePath, scheduleID)
	merged := s.getRuntimeStateByKey(key)
	_ = s.reconcileWorkflowScheduleRuns(context.Background(), workspacePath, scheduleID)
	runs, err := ReadScheduleRuns(context.Background(), workspacePath)
	if err == nil {
		return mergeRuntimeStateWithRuns(merged, scheduleID, runs)
	}
	return merged
}

func (s *SchedulerService) GetRuntimeStateForUser(userID, scheduleID string) ScheduleRuntimeState {
	merged := s.getRuntimeStateByKey(multiAgentScheduleRuntimeKey(userID, scheduleID))
	_ = s.reconcileMultiAgentScheduleRuns(context.Background(), userID, scheduleID)
	runs, err := ReadMultiAgentScheduleRuns(context.Background(), userID)
	if err == nil {
		return mergeRuntimeStateWithRuns(merged, scheduleID, runs)
	}
	return merged
}

func (s *SchedulerService) getRuntimeStateByKey(key string) ScheduleRuntimeState {
	s.runtimeStatesMu.RLock()
	var merged ScheduleRuntimeState
	if state, ok := s.runtimeStates[key]; ok {
		merged = cloneScheduleRuntimeState(state)
	}
	s.runtimeStatesMu.RUnlock()
	return merged
}

func cloneScheduleRuntimeState(state *ScheduleRuntimeState) ScheduleRuntimeState {
	if state == nil {
		return ScheduleRuntimeState{}
	}
	copy := *state
	if state.LastRunAt != nil {
		value := *state.LastRunAt
		copy.LastRunAt = &value
	}
	if state.NextRunAt != nil {
		value := *state.NextRunAt
		copy.NextRunAt = &value
	}
	if state.LastDurationMs != nil {
		value := *state.LastDurationMs
		copy.LastDurationMs = &value
	}
	return copy
}

func (s *SchedulerService) runtimeKeysForScheduleID(scheduleID string) []string {
	s.runtimeStatesMu.RLock()
	defer s.runtimeStatesMu.RUnlock()
	keys := make([]string, 0, 1)
	for key := range s.runtimeStates {
		if scheduleRuntimeKeyHasID(key, scheduleID) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
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

func (s *SchedulerService) reconcileMultiAgentScheduleRuns(ctx context.Context, userID, scheduleID string) error {
	if s == nil || s.api == nil || strings.TrimSpace(userID) == "" {
		return nil
	}

	runs, err := ReadMultiAgentScheduleRuns(ctx, userID)
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
	return WriteMultiAgentScheduleRuns(ctx, userID, runs)
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
	case "stopped", "dismissed":
		return "stopped", fmt.Sprintf("session ended with status %s", session.Status), true
	case "error", "failed", "inactive":
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
	// File history records the workflow result before Pulse finishes. Preserve
	// the live in-memory state for that same run so the UI cannot report success
	// and admit another trigger while Pulse still owns the session.
	if state.LastStatus == "running" &&
		(state.LastSessionID == "" || latest.SessionID == "" || latest.SessionID == state.LastSessionID) {
		return state
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
	match := ""
	for key, workspacePath := range s.workspaceIndex {
		if !scheduleRuntimeKeyHasID(key, scheduleID) {
			continue
		}
		if match != "" && match != workspacePath {
			return ""
		}
		match = workspacePath
	}
	return match
}

// GetUserForSchedule returns the user ID for a multi-agent schedule ID.
func (s *SchedulerService) GetUserForSchedule(scheduleID string) string {
	s.userIndexMu.RLock()
	defer s.userIndexMu.RUnlock()
	match := ""
	for key, userID := range s.userIndex {
		if !scheduleRuntimeKeyHasID(key, scheduleID) {
			continue
		}
		if match != "" && match != userID {
			return ""
		}
		match = userID
	}
	return match
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
	sctx := buildScheduleContext(workspacePath, manifest, *sched)
	sctx.TriggerSource = "manual"
	startTime := time.Now().UTC()

	// Match the cron-fired path: do not start a manual trigger when this workflow
	// workspace already has an active execution, regardless of whether that run was
	// started manually, by workflow builder, or by another schedule.
	if activeExec := s.findActiveExecutionForWorkspace(workspacePath); activeExec != nil {
		triggeredBy := activeExec.TriggeredBy
		if strings.TrimSpace(triggeredBy) == "" {
			triggeredBy = "unknown"
		}
		err := fmt.Errorf(
			"workflow already has an active %s run (session: %s)",
			triggeredBy,
			activeExec.SessionID,
		)
		s.recordScheduleFireDecision(ctx, sctx, "skipped_busy", err.Error(), "", startTime)
		return "", err
	}

	// Reserve the in-memory run and cancellation handle atomically, then claim
	// the durable lease without holding the global runtime-state mutex.
	runtimeKey := workflowScheduleRuntimeKey(workspacePath, scheduleID)
	runID := uuid.NewString()
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(runtimeKey)
	if state.LastStatus == "running" {
		s.runtimeStatesMu.Unlock()
		s.recordScheduleFireDecision(ctx, sctx, "skipped_busy", "schedule is already running", "", startTime)
		return "", fmt.Errorf("job is already running (session: %s)", state.LastSessionID)
	}
	previousState := *state
	runCtx := s.activateScheduleRunLocked(state, runID, startTime)
	s.runtimeStatesMu.Unlock()
	if err := s.claimScheduleRun(ctx, sctx, runID, startTime); err != nil {
		s.rollbackScheduleRunActivation(runtimeKey, runID, previousState)
		s.recordScheduleFireDecision(ctx, sctx, "skipped_busy", err.Error(), "", startTime)
		return "", err
	}
	if s.abortCanceledScheduleRunBeforeStart(runCtx, sctx, runtimeKey, runID) {
		return "", context.Canceled
	}
	s.recordScheduleFireDecision(ctx, sctx, "started", "manual trigger accepted", runID, startTime)

	if err := RecordWorkflowScheduleExecution(context.Background(), workspacePath, *sched, startTime); err != nil {
		s.logf(sctx, "[SCHEDULER] Warning: failed to record manual schedule execution for %s: %v", scheduleID, err)
	}

	go func() {
		if _, err := s.runJob(runCtx, sctx, runID); err != nil {
			scheduleLogf("[SCHEDULER] Triggered job %s failed: %v", scheduleID, err)
		}
	}()

	return "triggered", nil
}

// TriggerMultiAgentNow triggers a multi-agent schedule immediately.
func (s *SchedulerService) TriggerMultiAgentNow(userID string, scheduleID string) (string, error) {
	ctx := context.Background()

	f, _, err := ReadMultiAgentSchedules(ctx, userID)
	if err != nil {
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
	sctx := buildMultiAgentScheduleContext(userID, *sched, f.Capabilities)
	sctx.TriggerSource = "manual"
	runtimeKey := multiAgentScheduleRuntimeKey(userID, scheduleID)
	runID := uuid.NewString()
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(runtimeKey)
	if state.LastStatus == "running" {
		s.runtimeStatesMu.Unlock()
		s.recordScheduleFireDecision(ctx, sctx, "skipped_busy", "schedule is already running", "", startTime)
		return "", fmt.Errorf("job is already running (session: %s)", state.LastSessionID)
	}
	previousState := *state
	runCtx := s.activateScheduleRunLocked(state, runID, startTime)
	s.runtimeStatesMu.Unlock()
	if err := s.claimScheduleRun(ctx, sctx, runID, startTime); err != nil {
		s.rollbackScheduleRunActivation(runtimeKey, runID, previousState)
		s.recordScheduleFireDecision(ctx, sctx, "skipped_busy", err.Error(), "", startTime)
		return "", err
	}
	if s.abortCanceledScheduleRunBeforeStart(runCtx, sctx, runtimeKey, runID) {
		return "", context.Canceled
	}

	s.recordScheduleFireDecision(ctx, sctx, "started", "manual trigger accepted", runID, startTime)

	go func() {
		if _, err := s.runJob(runCtx, sctx, runID); err != nil {
			scheduleLogf("[SCHEDULER] Triggered multi-agent job %s failed: %v", scheduleID, err)
		}
	}()

	return "triggered", nil
}

// StopRunningJob stops a running scheduled job by canceling its session.
func (s *SchedulerService) StopRunningJobForWorkflow(workspacePath, scheduleID string) {
	s.stopRunningJob(workflowScheduleRuntimeKey(workspacePath, scheduleID), scheduleID)
}

func (s *SchedulerService) StopRunningJobForUser(userID, scheduleID string) {
	s.stopRunningJob(multiAgentScheduleRuntimeKey(userID, scheduleID), scheduleID)
}

func (s *SchedulerService) StopRunningJob(scheduleID string) {
	keys := s.runtimeKeysForScheduleID(scheduleID)
	if len(keys) != 1 {
		return
	}
	s.stopRunningJob(keys[0], scheduleID)
}

func (s *SchedulerService) stopRunningJob(runtimeKey, scheduleID string) {
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(runtimeKey)
	sessionID := state.LastSessionID
	runID := state.ActiveRunID
	state.LastStatus = "stopped"
	state.LastError = "stopped by user"
	state.ActiveRunID = ""
	s.runtimeStatesMu.Unlock()
	if runID == "" {
		lockKey := scheduleStateLockKeyFromRuntimeKey(runtimeKey)
		s.stateStoreMu.RLock()
		store := s.stateStore
		if store != nil {
			if active, err := store.ActiveRunByLockKey(context.Background(), lockKey); err == nil {
				runID = active.RunID
			}
		}
		s.stateStoreMu.RUnlock()
	}
	if runID != "" {
		s.cancelScheduleRunContext(runID)
		s.transitionScheduleRun(context.Background(), nil, schedulerstate.Transition{
			RunID: runID, To: schedulerstate.StateStopped, Reason: "stopped by user", SessionID: sessionID,
			ErrorMessage: "stopped by user", At: time.Now().UTC(),
		})
	}
	if sessionID == "" {
		return
	}

	scheduleLogf("[SCHEDULER] Stopping running job %s (session: %s)", scheduleID, sessionID)
	if isScheduledSession(sessionID) {
		s.api.markSessionTurnInterrupted(sessionID)
	}
	s.cancelScheduledSessionWork(sessionID, "scheduled job stopped by user")

	scheduleLogf("[SCHEDULER] Stopped job %s (session: %s)", scheduleID, sessionID)
}

// cancelScheduledSessionWork stops agent, workflow, background, and tmux work
// owned by a scheduled session without changing the schedule's recorded run
// result. Pulse timeout recovery uses this before continuing finalization in a
// fresh session.
func (s *SchedulerService) cancelScheduledSessionWork(sessionID, closeReason string) {
	if s == nil || s.api == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	s.api.cancelSessionRuntimeWork(sessionID, closeReason)
}

// triggerSchedule is called by the tick loop when a schedule is due.
func (s *SchedulerService) triggerSchedule(sctx *ScheduleContext) {
	schedID := sctx.Schedule.ID
	runtimeKey := scheduleRuntimeKey(sctx)
	now := time.Now()
	ctx := context.Background()
	s.logf(sctx, "[SCHEDULER] ⏰ Cron fired for %s (%s) at %s", schedID, sctx.Schedule.Name, now.Format(time.RFC3339))

	// Late-fire detection: compare to the next_run we recorded last time. Drift > 60s
	// usually means a missed-fire catch-up after macOS sleep/wake, or scheduler stall.
	s.runtimeStatesMu.RLock()
	if st, ok := s.runtimeStates[runtimeKey]; ok && st.NextRunAt != nil {
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
		s.recordScheduleFireDecision(ctx, sctx, "skipped_paused", "global scheduler pause is active", "", now.UTC())
		return
	}

	// Reload schedule for latest config — different paths for workflow vs multi-agent
	var freshCtx *ScheduleContext
	var workflowScheduleIDs []string
	if sctx.SourceType == "multi-agent" {
		f, exists, err := ReadMultiAgentSchedules(context.Background(), sctx.UserID)
		if err != nil || !exists {
			s.logf(sctx, "[SCHEDULER] ❌ Failed to reload multi-agent schedules for user %s: %v", sctx.UserID, err)
			s.recordScheduleFireDecision(ctx, sctx, "failed_to_start", "failed to reload multi-agent schedule", "", now.UTC())
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
			s.recordScheduleFireDecision(ctx, sctx, "failed_to_start", "schedule no longer exists", "", now.UTC())
			return
		}
		if !currentSched.Enabled {
			s.logf(sctx, "[SCHEDULER] ⏭️ Multi-agent schedule %s is disabled, skipping", schedID)
			s.recordScheduleFireDecision(ctx, sctx, "skipped_disabled", "schedule is disabled", "", now.UTC())
			return
		}
		resolvedSchedule, calendarItem, ok := scheduleWithReloadedCalendarItem(*currentSched, sctx.CalendarItem)
		if !ok {
			s.logf(sctx, "[SCHEDULER] Calendar item for %s no longer exists, skipping", schedID)
			s.recordScheduleFireDecision(ctx, sctx, "failed_to_start", "calendar item no longer exists", "", now.UTC())
			return
		}
		freshCtx = buildMultiAgentScheduleContext(sctx.UserID, resolvedSchedule, f.Capabilities)
		freshCtx.CalendarItem = calendarItem
	} else {
		// Reload manifest for latest config
		manifest, found, err := ReadWorkflowManifest(context.Background(), sctx.WorkspacePath)
		if err != nil || !found {
			s.logf(sctx, "[SCHEDULER] ❌ Failed to reload manifest for %s: %v", schedID, err)
			s.recordScheduleFireDecision(ctx, sctx, "failed_to_start", "failed to reload workflow manifest", "", now.UTC())
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
			s.recordScheduleFireDecision(ctx, sctx, "failed_to_start", "schedule no longer exists", "", now.UTC())
			return
		}
		if !currentSched.Enabled {
			if err := EnsureWorkflowScheduleExecutionTracker(context.Background(), sctx.WorkspacePath, *currentSched, time.Now().UTC()); err != nil {
				s.logf(sctx, "[SCHEDULER] Warning: failed to sync disabled execution history for %s: %v", schedID, err)
			}
			s.logf(sctx, "[SCHEDULER] ⏭️ Schedule %s is disabled, skipping", schedID)
			s.recordScheduleFireDecision(ctx, sctx, "skipped_disabled", "schedule is disabled", "", now.UTC())
			return
		}

		if activeExec := s.findActiveExecutionForWorkspace(sctx.WorkspacePath); activeExec != nil {
			triggeredBy := activeExec.TriggeredBy
			if strings.TrimSpace(triggeredBy) == "" {
				triggeredBy = "unknown"
			}
			s.logf(sctx, "[SCHEDULER] ⏭️ Workflow %s already has an active %s run (session: %s), skipping schedule %s",
				sctx.WorkspacePath, triggeredBy, activeExec.SessionID, schedID)
			s.recordScheduleFireDecision(ctx, sctx, "skipped_busy", "workflow already has an active execution", "", now.UTC())
			return
		}

		resolvedSchedule, calendarItem, ok := scheduleWithReloadedCalendarItem(*currentSched, sctx.CalendarItem)
		if !ok {
			s.logf(sctx, "[SCHEDULER] Calendar item for %s no longer exists, skipping", schedID)
			s.recordScheduleFireDecision(ctx, sctx, "failed_to_start", "calendar item no longer exists", "", now.UTC())
			return
		}
		freshCtx = buildScheduleContext(sctx.WorkspacePath, manifest, resolvedSchedule)
		freshCtx.CalendarItem = calendarItem
	}
	freshCtx.TriggerSource = "cron"

	// Built-in pre-fire check: if the built-in registered a gating function and
	// it returns false, skip this tick entirely. No LLM session is spawned.
	if check, ok := PreFireChecks[freshCtx.Schedule.ID]; ok {
		if !check(freshCtx.UserID) {
			s.logf(freshCtx, "[SCHEDULER] ⏭️ Pre-fire check returned false for %s (user %s) — skipping", freshCtx.Schedule.ID, freshCtx.UserID)
			s.recordScheduleFireDecision(ctx, freshCtx, "skipped_prefire", "pre-fire check returned false", "", now.UTC())
			return
		}
	}

	// Reserve in memory before the durable claim so Stop can cancel even while
	// SQLite is claiming the lease. The database call itself runs without the
	// global runtime-state mutex.
	startTime := time.Now().UTC()
	runID := uuid.NewString()
	runtimeKey = scheduleRuntimeKey(freshCtx)
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(runtimeKey)
	if state.LastStatus == "running" {
		s.runtimeStatesMu.Unlock()
		s.sessionLogf(freshCtx, state.LastSessionID, "[SCHEDULER] ⏭️ Schedule %s is already running (session: %s), skipping", schedID, state.LastSessionID)
		s.recordScheduleFireDecision(ctx, freshCtx, "skipped_busy", "schedule is already running", "", startTime)
		return
	}
	if freshCtx.SourceType == "workflow" {
		workflowRuntimeKeys := make([]string, 0, len(workflowScheduleIDs))
		for _, workflowScheduleID := range workflowScheduleIDs {
			workflowRuntimeKeys = append(workflowRuntimeKeys, workflowScheduleRuntimeKey(freshCtx.WorkspacePath, workflowScheduleID))
		}
		if otherKey, otherSession := runningWorkflowScheduleInSetLocked(s.runtimeStates, workflowRuntimeKeys, scheduleRuntimeKey(freshCtx)); otherKey != "" {
			s.runtimeStatesMu.Unlock()
			s.logf(freshCtx, "[SCHEDULER] ⏭️ Workflow %s already has running schedule %s (session: %s), skipping schedule %s",
				freshCtx.WorkspacePath, otherKey, otherSession, schedID)
			s.recordScheduleFireDecision(ctx, freshCtx, "skipped_busy", "another schedule owns the workflow", "", startTime)
			return
		}
	}
	previousState := *state
	runCtx := s.activateScheduleRunLocked(state, runID, startTime)
	s.runtimeStatesMu.Unlock()
	if err := s.claimScheduleRun(ctx, freshCtx, runID, startTime); err != nil {
		s.rollbackScheduleRunActivation(runtimeKey, runID, previousState)
		s.recordScheduleFireDecision(ctx, freshCtx, "skipped_busy", err.Error(), "", startTime)
		s.logf(freshCtx, "[SCHEDULER] ⏭️ Durable run lease rejected schedule %s: %v", schedID, err)
		return
	}
	if s.abortCanceledScheduleRunBeforeStart(runCtx, freshCtx, runtimeKey, runID) {
		return
	}
	s.recordScheduleFireDecision(ctx, freshCtx, "started", "cron fire accepted", runID, startTime)

	if freshCtx.SourceType == "workflow" {
		if err := RecordWorkflowScheduleExecution(context.Background(), freshCtx.WorkspacePath, freshCtx.Schedule, startTime); err != nil {
			s.logf(freshCtx, "[SCHEDULER] Warning: failed to record scheduled execution for %s: %v", schedID, err)
		}
	}

	s.logf(freshCtx, "[SCHEDULER] 🚀 Starting %s (%s)", schedID, freshCtx.Schedule.Name)
	if _, err := s.runJob(runCtx, freshCtx, runID); err != nil {
		s.logf(freshCtx, "[SCHEDULER] ❌ %s failed: %v", schedID, err)
	} else {
		s.logf(freshCtx, "[SCHEDULER] ✅ %s completed", schedID)
	}
}

// runJob executes a scheduled job: updates runtime state, creates run history, executes, updates results.
func (s *SchedulerService) runJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, error) {
	defer s.releaseScheduleRunContext(runID)
	schedID := sctx.Schedule.ID
	runtimeKey := scheduleRuntimeKey(sctx)
	startTime := time.Now().UTC()
	s.logf(sctx, "[SCHEDULER] runJob starting for %s (%s) at %s, groups=%v",
		schedID, sctx.Schedule.Name, startTime.Format(time.RFC3339), sctx.Schedule.GroupNames)
	if err := ctx.Err(); err != nil {
		s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
			state.ActiveRunID = ""
			state.LastStatus = "stopped"
			state.LastError = "stopped by user before execution started"
		})
		s.transitionScheduleRun(context.Background(), sctx, schedulerstate.Transition{
			RunID: runID, To: schedulerstate.StateStopped, Reason: "stopped before workflow execution started",
			ErrorMessage: "stopped by user", At: time.Now().UTC(),
		})
		return "", errors.Join(errWorkshopSequenceInterrupted, err)
	}
	if strings.TrimSpace(runID) == "" {
		return "", errors.Join(errWorkshopSequenceInterrupted, errors.New("scheduled run is missing its run id"))
	}
	s.runtimeStatesMu.RLock()
	activeState := s.runtimeStates[runtimeKey]
	ownsActiveRun := activeState != nil && activeState.LastStatus == "running" && activeState.ActiveRunID == runID
	s.runtimeStatesMu.RUnlock()
	if !ownsActiveRun {
		return "", errors.Join(errWorkshopSequenceInterrupted, fmt.Errorf("scheduled run %s no longer owns %s", runID, runtimeKey))
	}

	// Clear session/error fields — status is already "running" (set atomically by caller)
	s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		state.LastSessionID = ""
		state.LastError = ""
	})

	// Create run history entry (file-based)
	if strings.TrimSpace(runID) == "" {
		runID = uuid.New().String()
	}
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
	s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
		RunID:  runID,
		To:     schedulerstate.StateWorkflowRunning,
		Reason: "workflow execution starting",
		At:     time.Now().UTC(),
	})

	// Execute
	sessionID, runFolder, execErr := s.executeJob(ctx, sctx, runID)

	// Calculate results
	durationMs := time.Since(startTime).Milliseconds()
	nextRun := getNextRunTime(sctx.Schedule.CronExpression, sctx.Schedule.Timezone)

	status := "success"
	errMsg := ""
	userInterrupted := errors.Is(execErr, errWorkshopSequenceInterrupted) || errors.Is(execErr, context.Canceled)
	if userInterrupted {
		status = "stopped"
		if execErr != nil {
			errMsg = execErr.Error()
		} else {
			errMsg = "stopped by user"
			execErr = errWorkshopSequenceInterrupted
		}
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] %s stopped by user after %dms", schedID, durationMs)
	} else if execErr != nil {
		status = "error"
		errMsg = execErr.Error()
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] %s failed in %dms: %v", schedID, durationMs, execErr)
	} else {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] %s completed in %dms, session: %s, folder: %s", schedID, durationMs, sessionID, runFolder)
	}
	s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
		RunID:        runID,
		To:           schedulerstate.StateWorkflowFinished,
		Reason:       "workflow execution finished with status " + status,
		SessionID:    sessionID,
		SessionKind:  "workflow",
		RunFolder:    runFolder,
		ErrorMessage: errMsg,
		At:           time.Now().UTC(),
	})

	// Keep the runtime state as "running" until all post-run side effects finish.
	// Pulse runs as several resumed builder-chat turns after the workflow result
	// is recorded; if we mark the schedule successful before Pulse finishes, a
	// frequent cron can start the next workflow run while Pulse is between steps.
	// That makes the next Pulse turn fail with workflow_busy (commonly after the
	// LLM/cost/time report), so cadence/backup/publish/notify never run.
	s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		state.LastSessionID = sessionID
	})

	// Update run history entry for the actual workflow/task run. Post-run Pulse
	// may continue after this, but it does not change the recorded run result.
	pulseResult := postRunMonitorNotRun
	if sctx.SourceType == "multi-agent" {
		if err := UpdateMultiAgentScheduleRun(ctx, sctx.UserID, runID, status, errMsg, &durationMs, sessionID); err != nil {
			s.sessionLogf(sctx, sessionID, "[SCHEDULER] Failed to update multi-agent run entry for %s: %v", schedID, err)
		}
		if !userInterrupted && shouldUpdateChiefTaskReport(sctx) {
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
		// only the selected modules (bug review, artifact/report/eval/learning/KB/DB
		// health, cost/time, or Goal Advisor), backs up the final state, publishes, and sends
		// a run summary notification — see runPostRunMonitor.
		// Opt-in per workflow (post_run_monitor in workflow.json) — runs only when
		// the user / builder enabled Pulse. Only after an actual workflow RUN, not an
		// optimizer/improvement pass (there's no fresh run output to scan there).
		// Never affects the run's recorded result.
		if !userInterrupted && runFolder != "" && sctx.Schedule.WorkshopMode != "optimizer" {
			if manifest, found, mErr := ReadWorkflowManifest(ctx, sctx.WorkspacePath); mErr == nil && found && manifest.MonitorEnabled() {
				// Pass the run's sessionID so Pulse resumes the SAME chat (not a fresh one).
				s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
					RunID: runID, To: schedulerstate.StatePulseGate, Reason: "Pulse enabled for workflow", SessionID: sessionID, SessionKind: "pulse", At: time.Now().UTC(),
				})
				pulseResult = s.runPostRunMonitor(ctx, sctx, manifest, status, runFolder, sessionID, runID)
			}
		}
	}

	// Now the whole scheduled job, including post-run side effects, is done.
	terminalState := schedulerstate.StateCompleted
	if userInterrupted {
		terminalState = schedulerstate.StateStopped
	} else if status == "error" {
		terminalState = schedulerstate.StateFailed
	} else if pulseResult == postRunMonitorPartial {
		terminalState = schedulerstate.StatePartial
	} else if pulseResult == postRunMonitorStopped {
		terminalState = schedulerstate.StateStopped
	}
	overallStatus := status
	overallError := errMsg
	if terminalState == schedulerstate.StateStopped {
		overallStatus = "stopped"
		if overallError == "" {
			overallError = "stopped by user"
		}
	} else if terminalState == schedulerstate.StatePartial {
		overallStatus = "partial"
		if overallError == "" {
			overallError = "Pulse completed partially"
		}
	}
	s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		state.ActiveRunID = ""
		state.LastStatus = overallStatus
		state.LastError = overallError
		state.LastDurationMs = &durationMs
		state.NextRunAt = nextRun
		state.RunCount++
		if overallStatus == "error" {
			state.ConsecutiveFailures++
		} else {
			state.ConsecutiveFailures = 0
		}
	})
	s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
		RunID: runID, To: terminalState, Reason: "scheduled run finished with status " + status,
		SessionID: sessionID, ErrorMessage: errMsg, At: time.Now().UTC(),
	})
	s.cleanupRemovedScheduleRuntimeState(runtimeKey)

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
	if len(sctx.Capabilities.CDPPorts) > 0 {
		reqMap["cdp_ports"] = append([]int(nil), sctx.Capabilities.CDPPorts...)
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

If progress requires a non-blocking user decision, clarification, or approval, do not guess and do not wait in real time. Call create_human_input_request(source="chief_of_staff", workspace_path="pulse" for an org-wide question or the affected Workflow/<name> path for a workflow-specific question). Continue any independent work that remains safe. A future Chief of Staff or workflow Pulse run will receive the saved answer and must record what it did with mark_human_input_consumed.

Scheduled task:
%s`, sctx.Schedule.ID, query)
}

// runPostRunMonitor fires the Pulse pass after a scheduled workflow run. Pulse
// reads the run evidence, plan changelog, and eval files to form a Bug
// verdict and a Goal verdict (recorded into builder/improve.html — the Pulse
// log, the single source of truth), applies the full Pulse Fixer pass for Bug
// findings (recording the Goal finding + evidence for the scheduled Goal
// Advisor loop, which applies evidence-backed replans), runs a separate report-only artifact drift
// review, records the report-only LLM/cost/time readout, backs up the final state
// before publish, and notifies on a transition. It
// never changes the run's recorded status — failures here are logged and
// swallowed. Pulse's behavior is defined by the post-run-monitor reference doc;
// this just hands it the run context.
type postRunMonitorResult string

const (
	postRunMonitorNotRun    postRunMonitorResult = "not_run"
	postRunMonitorCompleted postRunMonitorResult = "completed"
	postRunMonitorPartial   postRunMonitorResult = "partial"
	postRunMonitorStopped   postRunMonitorResult = "stopped"
)

func (s *SchedulerService) runPostRunMonitor(ctx context.Context, sctx *ScheduleContext, manifest *WorkflowManifest, runStatus, runFolder, runSessionID, scheduleRunID string) (pulseResult postRunMonitorResult) {
	pulseResult = postRunMonitorPartial
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
	pulseRunID := sessionID
	baseReqMap := s.buildWorkshopRequest(ctx, sctx)
	if err := initializePulseFinalCommandStates(ctx, sctx.WorkspacePath, pulseRunID); err != nil {
		s.sessionLogf(sctx, sessionID, "[PULSE] failed to initialize final command state: %v", err)
	}

	// Run Pulse as a sequence: lightweight Gate first, then only the modules Gate
	// selected, then one ordered dashboard/backup/publish/notify finalizer. Each
	// expensive maintenance turn stays focused while fixed final work reuses one
	// context instead of paying for four repeated context loads.
	answeredHumanInputs := formatAnsweredReportHumanInputsForAgent(ctx, sctx.WorkspacePath)
	humanInputNote := ""
	if answeredHumanInputs != "" {
		humanInputNote = "\n\n" + answeredHumanInputs
	}
	intro := fmt.Sprintf(
		"You are Pulse, the post-run steward. A scheduled run of this workflow just finished: workspace_path=%q, status=%q, run_folder=%q. "+
			"Call get_reference_doc(kind=\"post-run-monitor\") and follow it. Write builder/improve.html in simple user-facing language and priority order: Needs your decision first in Runloop, then active Assumptions challenged, Today's outcome, goal progress, recent activity, and collapsed technical detail. Keep the first screen compact, do not duplicate the latest-run narrative, put the takeaway first, use short labeled detail, and put raw evidence paths last. "+
			"If you need user input, call create_human_input_request(workspace_path=%q, source=\"pulse\", ...) instead of hand-editing request state in HTML. "+
			"For strategic plan-change approval, Goal Advisor must use that same existing report interaction flow: create_human_input_request(source=\"goal_advisor\", input_id=\"plan-proposal-...\", options approve/reject/defer, context=\"proposal + exact plan edits + evidence\"). "+
			"We'll go one step at a time — do ONLY the step in each message, finish it, then stop and wait for the next.%s",
		sctx.WorkspacePath, runStatus, runFolder, sctx.WorkspacePath, humanInputNote)

	upgradeSteps := postRunMonitorUpgradeStepsForManifest(manifest)
	s.sessionLogf(sctx, sessionID, "[PULSE] starting pulse for %s (run_folder=%s status=%s, upgrades=%d)", sctx.Schedule.ID, runFolder, runStatus, len(upgradeSteps))
	introSent := false
	recoveryNotes := []string{}
	runStep := func(st postRunMonitorStep) postRunMonitorStepRunResult {
		if st.label == "finalize" {
			s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
				RunID: scheduleRunID, To: schedulerstate.StatePulseFinalizing, Reason: "Pulse finalizer starting",
				SessionID: sessionID, SessionKind: "pulse", At: time.Now().UTC(),
			})
			if _, err := markPulseFinalCommandState(ctx, sctx.WorkspacePath, pulseFinalCommandDashboard, pulseRunID, "running", "Preparing the Pulse dashboard and user questions"); err != nil {
				s.sessionLogf(sctx, sessionID, "[PULSE] failed to mark dashboard running: %v", err)
			}
		}
		reqMap := cloneStringInterfaceMap(baseReqMap)
		s.applyPulseLLMToReqMap(reqMap, sctx, sessionID)
		query := st.query
		if !introSent {
			recoveryContext := ""
			if len(recoveryNotes) > 0 {
				recoveryContext = "\n\nPULSE RECOVERY CONTEXT. Earlier Pulse work did not finish cleanly. Do not rerun timed-out maintenance in this recovery session. Continue the requested finalization step, preserve partial/failed status honestly, and do not claim skipped work succeeded:\n- " + strings.Join(recoveryNotes, "\n- ")
			}
			query = intro + recoveryContext + "\n\n" + query
			introSent = true
		}
		reqMap["query"] = query
		if err := s.api.startSessionInternal(ctx, reqMap, sessionID, "", nil); err != nil {
			s.sessionLogf(sctx, sessionID, "[PULSE] step %q failed to start: %v", st.label, err)
			if st.label == "finalize" {
				_ = finalizeUnresolvedPulseFinalCommands(ctx, sctx.WorkspacePath, pulseRunID, "failed", "Pulse finalizer failed to start")
			}
			return postRunMonitorStepRunResult{outcome: postRunMonitorStepStartFailed, err: err}
		}
		if err := s.waitForWorkshopIdleWithInactivityTimeout(ctx, sessionID, st.idleMaxInactivity()); err != nil {
			s.sessionLogf(sctx, sessionID, "[PULSE] step %q idle wait failed: %v", st.label, err)
			outcome := postRunMonitorStepWaitFailed
			if errors.Is(err, errWorkshopSequenceInterrupted) || errors.Is(err, context.Canceled) {
				outcome = postRunMonitorStepInterrupted
			} else if errors.Is(err, errWorkshopIdleWaitTimeout) {
				outcome = postRunMonitorStepTimedOut
			}
			if st.label == "finalize" {
				status := "failed"
				if outcome == postRunMonitorStepTimedOut {
					status = "timed_out"
				}
				_ = finalizeUnresolvedPulseFinalCommands(ctx, sctx.WorkspacePath, pulseRunID, status, "Pulse finalizer did not finish cleanly")
			}
			return postRunMonitorStepRunResult{outcome: outcome, err: err}
		}
		if st.label == "finalize" {
			if err := finalizeUnresolvedPulseFinalCommands(ctx, sctx.WorkspacePath, pulseRunID, "failed", "Finalizer ended without recording this command's outcome"); err != nil {
				s.sessionLogf(sctx, sessionID, "[PULSE] failed to reconcile final command state: %v", err)
			}
		}
		s.sessionLogf(sctx, sessionID, "[PULSE] step %q done for %s", st.label, sctx.Schedule.ID)
		return postRunMonitorStepRunResult{outcome: postRunMonitorStepCompleted}
	}
	handleStepFailure := func(st postRunMonitorStep, result postRunMonitorStepRunResult, needsFollowup bool) string {
		oldSessionID := sessionID
		resultName := "failed"
		failureLabel := "failed"
		if result.outcome == postRunMonitorStepTimedOut {
			resultName = "timed_out"
			failureLabel = fmt.Sprintf("made no observable progress for %s", st.idleMaxInactivity())
		}
		reason := fmt.Sprintf("Pulse step %s %s", st.label, failureLabel)
		if result.err != nil && result.outcome != postRunMonitorStepTimedOut {
			reason += ": " + result.err.Error()
		}
		if module := pulseModuleForPostRunMonitorStep(st.label); module != "" {
			if _, err := markPulseModuleResult(ctx, sctx.WorkspacePath, module, pulseRunID, resultName, reason, []string{"scheduler timeout/failure handling"}); err != nil {
				s.sessionLogf(sctx, oldSessionID, "[PULSE] failed to record %s result for module %s: %v", resultName, module, err)
			}
		}
		if result.outcome != postRunMonitorStepStartFailed {
			s.cancelScheduledSessionWork(oldSessionID, reason)
		}
		recoveryNotes = append(recoveryNotes, reason)
		if needsFollowup {
			sessionID = s.newScheduleSessionID(sctx)
			introSent = false
			s.sessionLogf(sctx, sessionID, "[PULSE] continuing finalization in recovery session after %s", reason)
		}
		return reason
	}
	abortIfInterrupted := func(st postRunMonitorStep, result postRunMonitorStepRunResult) bool {
		if result.outcome != postRunMonitorStepInterrupted {
			return false
		}
		reason := fmt.Sprintf("Pulse stopped by user during %s", st.label)
		pulseResult = postRunMonitorStopped
		s.sessionLogf(sctx, sessionID, "[PULSE] %s; no later module, recovery, finalizer, publish, or notification turn will run", reason)
		_ = finalizeUnresolvedPulseFinalCommands(ctx, sctx.WorkspacePath, pulseRunID, "skipped", reason)
		return true
	}

	gateReady := true
	for _, st := range upgradeSteps {
		result := runStep(st)
		if abortIfInterrupted(st, result) {
			return
		}
		if result.outcome != postRunMonitorStepCompleted {
			handleStepFailure(st, result, true)
			gateReady = false
			break
		}
	}

	// Archive is a conditional preflight, not a mandatory Pulse module. The
	// scheduler only detects that the active HTML crossed a size/card threshold;
	// the agent owns the semantic choice of which resolved history is safe to
	// move. Archive failure is fail-open so Gate and final reporting still run.
	if gateReady {
		assessment, err := pulseImproveArchiveAssessmentForWorkspace(ctx, sctx.WorkspacePath)
		if err != nil {
			s.sessionLogf(sctx, sessionID, "[PULSE] could not assess improve.html archive threshold: %v", err)
		} else if assessment.Due {
			s.sessionLogf(sctx, sessionID, "[PULSE] improve.html archive due: %s", assessment.triggerSummary())
			archiveStep := postRunMonitorArchiveStep(assessment)
			if result := runStep(archiveStep); abortIfInterrupted(archiveStep, result) {
				return
			} else if result.outcome != postRunMonitorStepCompleted {
				handleStepFailure(archiveStep, result, true)
			}
		}
	}

	var steps []postRunMonitorStep
	if gateReady {
		gateStep := postRunMonitorGateStep(pulseRunID, runFolder, runStatus)
		result := runStep(gateStep)
		if abortIfInterrupted(gateStep, result) {
			return
		}
		if result.outcome == postRunMonitorStepCompleted {
			steps = s.selectedPostRunMonitorModuleSteps(ctx, sctx, pulseRunID)
			if len(steps) > 0 && !isPostRunMonitorFinalStep(steps[0].label) {
				s.transitionScheduleRun(ctx, sctx, schedulerstate.Transition{
					RunID: scheduleRunID, To: schedulerstate.StatePulseModules, Reason: "Pulse Gate recorded due modules",
					SessionID: sessionID, SessionKind: "pulse", At: time.Now().UTC(),
				})
			}
			s.sessionLogf(sctx, sessionID, "[PULSE] selected %d post-gate steps for %s", len(steps), sctx.Schedule.ID)
		} else {
			handleStepFailure(gateStep, result, true)
			steps = postRunMonitorFinalSteps(pulseRunID)
		}
	} else {
		steps = postRunMonitorFinalSteps(pulseRunID)
	}

	skipMaintenanceReason := ""
	for i, st := range steps {
		if skipMaintenanceReason != "" && !isPostRunMonitorFinalStep(st.label) {
			if module := pulseModuleForPostRunMonitorStep(st.label); module != "" {
				reason := "Not run because earlier Pulse maintenance did not finish safely: " + skipMaintenanceReason
				if _, err := markPulseModuleResult(ctx, sctx.WorkspacePath, module, pulseRunID, "skipped", reason, []string{"scheduler timeout/failure handling"}); err != nil {
					s.sessionLogf(sctx, sessionID, "[PULSE] failed to record skipped result for module %s: %v", module, err)
				}
			}
			continue
		}
		result := runStep(st)
		if abortIfInterrupted(st, result) {
			return
		}
		if result.outcome != postRunMonitorStepCompleted {
			reason := handleStepFailure(st, result, i < len(steps)-1)
			if !isPostRunMonitorFinalStep(st.label) {
				skipMaintenanceReason = reason
			}
		}
	}
	if len(recoveryNotes) > 0 {
		pulseResult = postRunMonitorPartial
		s.sessionLogf(sctx, sessionID, "[PULSE] pulse finalized partially for %s after %d failed/timed-out step(s)", sctx.Schedule.ID, len(recoveryNotes))
	} else {
		pulseResult = postRunMonitorCompleted
		s.sessionLogf(sctx, sessionID, "[PULSE] pulse completed for %s", sctx.Schedule.ID)
	}
	// Pulse owns its own notification: per its reference doc it calls notify_user
	// once with a compact run summary, highlighting state transitions it reads from
	// the durable Pulse log. The scheduler no longer pushes a templated message —
	// that avoids a double-send and lets the agent author the exact, nuanced sentence.
	// The Bug/Goal verdict lives in builder/improve.html (pills + headline) — the
	// single source of truth, no separate file.
	return pulseResult
}

type postRunMonitorStep struct{ label, query string }

type postRunMonitorStepOutcome string

const (
	postRunMonitorStepCompleted   postRunMonitorStepOutcome = "completed"
	postRunMonitorStepStartFailed postRunMonitorStepOutcome = "start_failed"
	postRunMonitorStepWaitFailed  postRunMonitorStepOutcome = "wait_failed"
	postRunMonitorStepTimedOut    postRunMonitorStepOutcome = "timed_out"
	postRunMonitorStepInterrupted postRunMonitorStepOutcome = "interrupted"
)

type postRunMonitorStepRunResult struct {
	outcome postRunMonitorStepOutcome
	err     error
}

func pulseModuleForPostRunMonitorStep(label string) string {
	switch label {
	case "bug-review":
		return pulseModuleBugReview
	case "artifact":
		return pulseModuleArtifactReview
	case "report-health":
		return pulseModuleReportHealth
	case "eval-health":
		return pulseModuleEvalHealth
	case "learning-health":
		return pulseModuleLearningHealth
	case "knowledgebase-health":
		return pulseModuleKnowledgebaseHealth
	case "db-health":
		return pulseModuleDBHealth
	case "cost-llm-time":
		return pulseModuleCostLLMTime
	case "llm-ops-review":
		return pulseModuleLLMOpsReview
	case "goal-advisor":
		return pulseModuleGoalAdvisor
	default:
		return ""
	}
}

func isPostRunMonitorFinalStep(label string) bool {
	return label == "finalize"
}

func (st postRunMonitorStep) idleMaxInactivity() time.Duration {
	if st.label == "goal-advisor" {
		return schedulerGoalAdvisorMaxInactivity
	}
	return schedulerWorkshopMaxInactivity
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
			"Use a progressive evidence scan, not a full audit. First read builder/improve.html (authoritative history/current cadence), including any .advisor-experiment card and its data-status/data-review-after, get_pulse_module_state(workspace_path=\"<current workflow>\"), latest run metadata/summary, changelog filenames and review flags, existence/freshness metadata for plan/eval/report artifacts, the workflow.json version plus a compact resolved LLM/tier/fallback signature, backup/publish/notification readiness metadata, db/README or a compact schema summary, per-step learning metadata, a compact KB note index, open/answered report_human_inputs, and pending Chief of Staff recommendation cards. For the current run_folder, explicitly inspect every executed step/item's compact final result for the literal `CONCERNS:` marker: for regular/todo-task steps prefer runs/<run_folder>/logs/<step>/execution/execution-final-summary.json execution_result; when that file is absent for a failed, incomplete, or legacy run, use the latest applicable runs/<run_folder>/logs/<step>/execution/execution-attempt-*.json execution_result. For message-sequence steps use runs/<run_folder>/execution/<step>/session.json entries[].summary. Use the latest successful/final retry only for the fallback attempt files, and do not load *-conversation.json, prompts, tool-call logs, or other long logs just to find concerns. A completed run or step does not resolve a concern. Deduplicate each current concern against builder/improve.html, preserve its step/item and evidence path, classify it into the matching due module (or reviewed/no-action), and keep unresolved concerns visible in the Gate timeline/handoff until a module verifies resolution, records a blocker, or creates a durable human-input request. `CONCERNS:` is evidence to classify, not an automatic run failure or automatic Bug verdict. Gate must not create the human-input request itself; select the responsible module so its Pulse Fixer can do so. Do not load full report HTML, full KB/learnings, broad DB rows, long logs, or every cost file unless a compact signal makes that module plausibly due or you need one targeted fact to justify its decision. The selected module performs the deep audit later; Gate only chooses the evidence-backed worklist and cadence. If the scan finds a plausible bug signal, mark Bug Review due so its read-only reviewer can investigate and the Pulse Fixer can repair and verify it. Do not treat the SQLite module cache as historical truth.\n\n"+
			"Update builder/improve.html first with a compact Pulse Gate / Worklist entry in the timeline area. Do not place full Gate details in the first-screen dashboard. Refresh Today's outcome (Outcome, Goal progress, Issues & fixes, Next Pulse) without duplicating the latest-run row. Preserve pending Needs your decision requests at the top through create_human_input_request/report_human_inputs. Keep at most three consequential Assumptions challenged items near the top; add one only when a non-user-confirmed assumption in soul/plan/steps/eval/KB/learnings/DB/report may materially cap the goal, and remove it when resolved. Architecture and implementation choices are revisable unless explicitly user-approved constraints. Refresh the bottom #pulse-agent-handoff in place with compact current Pulse/run ids, one row per module decision and next-check condition, cursor/open/pending ids, and evidence pointers. Never append Agent log copies or repeat the user-facing narrative there. The Gate entry should be plain English: what is healthy, what needs attention, which modules are due/skipped, and why. Update #pulse-bug-verdict and #pulse-goal-verdict in place; if either is missing from an otherwise current-format page, insert the standard two-element verdict block beside the workflow title without rewriting history, and never create a duplicate verdict block. Keep older trustworthy metrics when the latest route did not measure them, but visibly label every verdict, goal criterion, brief metric, and important tile with `as of run/date` or `not measured this run · last measured run/date`. Then call record_pulse_worklist exactly once with one decision for each module: bug_review, artifact_review, report_health, eval_health, learning_health, knowledgebase_health, db_health, cost_llm_time, llm_ops_review, goal_advisor.\n\n"+
			"Be agentic: decide what is due from workflow evidence, not a hardcoded cadence. Every skipped module must include a reason, evidence, and at least one concrete next-check condition: next_check_at, a positive cooldown_runs, or next_check_after_run_id. Write that next check visibly in the Gate/Worklist entry so the user can understand it; SQLite only mirrors it for scheduler/popup state. Evidence can override an earlier cooldown, but if you check a module earlier than previously planned, state what new evidence caused the override. High-frequency workflows should normally roll up cost/time checks across several runs instead of running cost_llm_time every time; mark it due immediately for missing/unpriced telemetry, a material cost or latency change, a model/tier change, or when its planned next check arrives. Treat llm_ops_review as a low-frequency coaching pass: run it when it has never completed, its planned checkpoint arrives, the resolved model/tier/fallback config changes, cost evidence suggests avoidable overkill, or publish/notify/backup/version state materially changes. Otherwise schedule a meaningful later checkpoint instead of running it every Pulse. Final dashboard, backup, publish, and notify happen later in one ordered finalizer turn.\n\n"+
			"Module meanings:\n"+
			"- bug_review: due for real Bug signals, failed producing steps, stale guards, selector/API/runtime breakage, workflow code that reuses an older receipt/artifact, broken evidence contracts, hallucinated success, or CoS recommendations that are operational bugs. Also mark it due for targeted observable trace review when compact output/eval/validation/report/DB/artifact/CONCERNS evidence suggests a successful step chose the wrong tool/source/route, used stale inputs, ignored or misinterpreted returned evidence, stopped too early, or made an unsupported decision; do not audit every conversation. Run a bounded exploratory QA checkpoint when QA has never completed, a material plan/step/contract/tool/model change landed, enough new outcome-bearing runs accumulated, or a previously recorded risk checkpoint arrived. Do not run exploratory QA on every high-frequency Pulse; when skipped, set a concrete next check based on risk, meaningful runs, elapsed business time, or a material change. New failure or suspicious evidence overrides that cadence. Exploratory QA derives the behavioral contract and safely probes critical, negative, boundary, stale-run-isolation, and failure/recovery paths without producing external side effects. The reviewer is read-only; Pulse Fixer writes.\n"+
			"- artifact_review: due when planning/changelog has unreviewed material plan/config changes, or when a plan/config change may have drifted learnings, KB, DB, report, eval, or step configs.\n"+
			"- report_health: due when report HTML, report_plan, SQL/window.report usage, dashboard tabs, goal visibility, or published/report claims are stale, broken, misleading, or not aligned with plan/eval/db.\n"+
			"- eval_health: due when evaluation_plan, eval rubric, eval run wiring, target run path, success criteria mapping, scoring thresholds, or eval reports are missing, stale, too lenient, gamed, misleading, or not aligned with soul/plan/report/db. If an eval accepts an older receipt/artifact for the current run, route it here as a correctness repair; do not route it to Goal Advisor or ask the user.\n"+
			"- learning_health: due when plan/step behavior changed, learning locks look stale, learning_objective no longer matches, reusable execution HOW was discovered, stale selectors/API quirks may exist, or learnings need lock/unlock decisions. If a plan change makes locked learnings stale, mark this due and name the step ids.\n"+
			"- knowledgebase_health: due when KB notes/context are missing, stale, contradictory, duplicated, in the wrong place, or step KB config no longer matches the plan. Do not rewrite knowledgebase/context; it is user-owned.\n"+
			"- db_health: due when DB schema/contracts/assets/upsert rules do not match current writers, reports, evals, or plan outputs.\n"+
			"- cost_llm_time: report run cost/eval cost/builder+Pulse overhead, missing cost buckets, timing, model/tier evidence; report-only unless a real config bug belongs to another module.\n"+
			"- llm_ops_review: low-frequency workflow coaching for execution tier usage, resolved provider/model/fallback routing, Pulse and Maintenance models, publish/notify/backup/version readiness, and deduplicated efficiency_or_coaching trace findings retained by an earlier Bug Review. It proposes exact evidence-backed changes through the existing Needs your decision cards and never silently rewrites configuration.\n"+
			"- goal_advisor: due when Goal drift persists, current strategy seems capped even if execution is clean, user answered a strategy question, CoS rec is strategic, enough new evidence exists for an expert out-of-plan critique, or a healthy workflow has accumulated enough meaningful outcome evidence for an optimization-headroom review. Also make it due for conditional plan-design review after a material plan structure/step-boundary/routing/store/validation/orchestration change, when repeated goal misses or recurring Bugs or material cost/latency evidence suggest the plan shape is limiting outcomes, or when its planned plan-design checkpoint arrives. Meeting a target is not a permanent skip: treat targets as minimums unless explicitly defined as caps, and periodically compare the successful baseline against credible ways to improve outcome rate, quality, cost, time, reach, or risk. Do not run this expensive review every Pulse; when a healthy workflow is skipped, set a concrete next check tied to meaningful outcome-bearing runs, a material outcome milestone, elapsed business time, a market/platform change, a plan-design risk, or new Chief of Staff evidence -- not merely raw high-frequency run count. Once that headroom or plan-design checkpoint arrives, Goal Advisor is due and cannot be rolled forward solely because the workflow remains healthy. If builder/improve.html has one active .advisor-experiment (status proposed, deferred, approved, running, measuring, or blocked), do not ask for another bold idea. The active experiment does not block plan-design monitoring of whether structure, instrumentation, or implementation prevents a fair test; select Goal Advisor for that monitoring when the plan-design trigger arrives, but do not create a competing experiment. Also select it when an answer, its data-review-after checkpoint, sufficient measurement evidence, an unblock signal, or decisive contradictory evidence requires advancing that same experiment. Never allow more than one active advisor experiment. A correct abstention or green eval is execution evidence, not goal progress: repeated no_job/no_match/no_candidate/stand_aside or zero downstream outcomes must make Goal Advisor due when they materially miss the soul success criteria. A trustworthy flat lagging metric (for example subscribers, responses, revenue, wins, or conversions) is enough to run strategic review even when the latest producing run has operational Bugs; mark both bug_review and goal_advisor due when appropriate. Do not use 'wait for a clean run' as an indefinite strategy cooldown, and do not restrict Goal Advisor to evidence from the latest route when retained cross-run goal evidence is already sufficient. Preserve explicit user exclusions, but challenge agent-chosen search breadth, source/channel mix, positioning, offer, thresholds, cadence, and brittle plan architecture. Goal Advisor does not do routine Bug Review/KB/learnings/DB cleanup.\n\n"+
			"Do not launch reviewers or call mutation tools, plan modification tools, publish, backup, or notify in this Gate step. Only update the compact log entry and record_pulse_worklist. Then stop.",
			pulseRunID, runFolder, runStatus),
	}
}

type postRunMonitorModuleStep struct {
	module string
	step   postRunMonitorStep
}

func postRunMonitorModuleSteps(pulseRunID string) []postRunMonitorModuleStep {
	steps := []postRunMonitorModuleStep{
		{pulseModuleBugReview, postRunMonitorStep{"bug-review", fmt.Sprintf("PULSE MODULE — BUG REVIEW. pulse_run_id=%q. This is a read-only reliability and exploratory QA review selected by Pulse Gate. Inspect the compact Gate worklist, retained run/eval evidence, execution logs, validation, prompts/config, stale artifacts, selector/API/runtime failures, hallucinated success, and report/eval evidence-chain breakage. First derive a concise behavioral contract from soul.md, the current plan and step descriptions/config, and applicable eval/report/DB contracts: state what must happen, what must never happen, and which evidence proves each claim. Build a small risk-ranked exploratory QA matrix covering the critical path, one negative path, one boundary or edge case, stale/current-run isolation, and failure/recovery behavior when applicable. Execute only tests proven side-effect-free, using existing artifacts, fixtures, validation scripts, temporary copies, scratch directories, or a scratch DB. Never send email/messages, post content, trade, publish, mutate production DB/data, or rerun an externally producing workflow action without explicit user approval. When a path cannot be tested safely, return an exact reproducible test case with setup, action, expected versus observed assertion, required evidence, and risk; do not simulate success. Treat semantic execution defects as Bugs too: for each suspect step named by Gate evidence, follow the post-run-monitor Observable execution-trace review contract and inspect only its latest applicable *-conversation.json (conversation_history, tool_calls, llm_calls), or message-sequence session.json, rather than auditing every conversation. Judge observable behavior: wrong tool/source, wrong workspace/run/group/table/endpoint/ids/filters/time window/destination, ignored or misinterpreted tool results, stale dependencies, invalid route/fallback/retry/stop choices, insufficient evidence, unsupported conclusions, and unverified recovery. Do not request or infer hidden chain-of-thought. Return every trace finding with classification, step/item, attempt, exact observable decision/tool call and result, impact, bounded fix, and verification, using exactly correctness_bug, efficiency_or_coaching, no_issue, or insufficient_evidence. Only correctness_bug belongs to the Pulse Fixer. Route efficiency_or_coaching to current llm_ops_review when due, otherwise preserve one deduplicated evidence pointer and next-check trigger for a future LLM/Ops pass; never change a correct step merely because a different tool might be faster. Return verdict, behavioral contract, QA coverage, ordered findings, expected versus observed behavior, exact evidence, confidence, untested risk, bounded recommended fixes, regression verification steps, and whether user judgment is required. Do not edit files, call mutation tools, update builder/improve.html, ask the user, or mark module state from the reviewer. The parent Pulse Fixer consolidates this review with all other due modules, applies safe fixes sequentially, runs targeted regression verification only in a temporary or otherwise proven side-effect-free environment, records one `Bug fix` outcome when needed, and calls mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"bug_review\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]).", pulseRunID, pulseRunID)}},
		{pulseModuleArtifactReview, postRunMonitorStep{"artifact", fmt.Sprintf("PULSE MODULE — ARTIFACT REVIEW. pulse_run_id=%q. Run only the artifact drift module selected by Pulse Gate. This is a read-only review separate from Bug Review. Read planning/changelog/ and the Artifact Sync Cursor in builder/improve.html, then follow get_workflow_command_guidance(kind=\"review-artifact-drift\", focus=\"Pulse artifact review after this run; report-only; do not fix\") as the audit checklist. Return exact drift findings and changelog entries that are fully inspected. Do not edit artifacts, write builder/improve.html, or mark changelog/module state from the reviewer. The parent Pulse Fixer records the Artifact Review outcome, calls mark_changelog_artifact_reviewed where justified, and finishes with mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"artifact_review\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]).", pulseRunID, pulseRunID)}},
		{pulseModuleReportHealth, postRunMonitorStep{"report-health", fmt.Sprintf("PULSE MODULE — REPORT HEALTH. pulse_run_id=%q. Use the consolidated protocol: pass the improve-report checklist and Gate evidence to a generic READ-ONLY REVIEW agent. Inspect reports/report_plan.json, db/reports/*.html, builder/improve.html, current plan/eval/db evidence, and latest run outputs. The reviewer returns exact stale, broken, misleading, text-heavy, goal-visibility, SQL/window.report, responsive-layout, and evidence-alignment findings with bounded recommended edits and verification steps. It must not edit files, call report mutation tools, write builder/improve.html, ask the user, or mark state. The parent Pulse Fixer applies and verifies safe report-only fixes sequentially, records one consolidated `Report fix` outcome when needed, and calls mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"report_health\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]).", pulseRunID, pulseRunID)}},
		{pulseModuleEvalHealth, postRunMonitorStep{"eval-health", fmt.Sprintf("PULSE MODULE — EVAL HEALTH. pulse_run_id=%q. Use the consolidated protocol: pass the improve-evaluation checklist and Gate evidence to a generic READ-ONLY REVIEW agent. Inspect evaluation/evaluation_plan.json, matching evaluation outputs, soul/soul.md success criteria, planning/plan.json, planning/step_config.json, report/db consumers, and latest run evidence. The reviewer classifies findings as correctness repair, operational, or goal-semantic and returns exact evidence, bounded recommended edits, score-continuity impact, and verification steps. It must not edit files, run evals, write builder/improve.html, create questions, or mark state. The parent Pulse Fixer may apply correctness-preserving current-run/group binding, stale-evidence rejection, TARGET_RUN_PATH/path/parser/schema wiring, and fail-closed repairs without asking. It must use the existing human-input flow before changing goal meaning, thresholds, weights, rubric semantics, or business policy. After safe sequential fixes and targeted validation when useful, the parent records one consolidated `Eval fix` outcome and calls mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"eval_health\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]).", pulseRunID, pulseRunID)}},
		{pulseModuleLearningHealth, postRunMonitorStep{"learning-health", fmt.Sprintf("PULSE MODULE — LEARNING HEALTH. pulse_run_id=%q. Use the consolidated protocol: load get_reference_doc(kind=\"assumption-audit\") and pass it with the improve-learnings, optimize-playbook, and step-config checklists to a generic READ-ONLY REVIEW agent. Preserve consequential unresolved restrictions as Assumptions challenged. Inspect planning/plan.json, planning/step_config.json, planning/changelog, learnings/_global/SKILL.md, relevant references, per-step .learning_metadata.json, and latest run evidence. The reviewer returns stale HOW, policy/architecture leakage, missing learning coverage, and lock/unlock recommendations. It must not edit or mark state. The parent Pulse Fixer applies bounded learning and step-config changes, verifies them, updates builder/improve.html once, and calls mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"learning_health\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]).", pulseRunID, pulseRunID)}},
		{pulseModuleKnowledgebaseHealth, postRunMonitorStep{"knowledgebase-health", fmt.Sprintf("PULSE MODULE — KNOWLEDGEBASE HEALTH. pulse_run_id=%q. Use the consolidated protocol: load get_reference_doc(kind=\"assumption-audit\") and pass it with the improve-knowledge, stores, and step-config checklists to a generic READ-ONLY REVIEW agent. Preserve consequential unresolved restrictions as Assumptions challenged. Inspect knowledgebase/notes, knowledgebase/context only as read-only user-owned context, KB access/contribution settings, latest run evidence, and report/eval consumers. The reviewer returns stale, duplicated, missing, contradictory, or tactic-bound notes and bounded config recommendations. It must never rewrite knowledgebase/context, edit files, or mark state. The parent Pulse Fixer applies bounded note/index/config changes, verifies them, updates builder/improve.html once, and calls mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"knowledgebase_health\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]).", pulseRunID, pulseRunID)}},
		{pulseModuleDBHealth, postRunMonitorStep{"db-health", fmt.Sprintf("PULSE MODULE — DB HEALTH. pulse_run_id=%q. Use the consolidated protocol: load get_reference_doc(kind=\"assumption-audit\") and pass it with the improve-database and stores checklists to a generic READ-ONLY REVIEW agent. Preserve consequential unresolved restrictions as Assumptions challenged. Inspect db/db.sqlite schema/table contracts, db/README.md, db/assets, current plan writers, report SQL/window.report consumers, eval consumers, and latest run evidence. The reviewer returns integrity, contract, upsert, index, compatibility, and over-constrained-schema findings with exact verification commands. It must not execute DDL/DML, edit files, or mark state. The parent Pulse Fixer applies only bounded non-speculative contract/schema repairs, verifies integrity and consumers, updates builder/improve.html once, and calls mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"db_health\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]).", pulseRunID, pulseRunID)}},
		{pulseModuleCostLLMTime, postRunMonitorStep{"cost-llm-time", fmt.Sprintf("PULSE MODULE — COST / LLM / TIME. pulse_run_id=%q. Use the consolidated protocol: send a generic READ-ONLY REVIEW agent the Gate evidence plus workflow.json capabilities.llm_config, step execution tiers, get_cost_summary(run_folder) when available, costs/execution, costs/evaluation, costs/phase/token_usage.json, and matching timing summaries. The reviewer returns a compact telemetry finding set with separately labeled workflow execution, evaluation, and builder/Pulse overhead buckets, including missing or unpriced evidence instead of estimates. It must not edit files, change model/config/schedules, write builder/improve.html or cards, ask the user, or mark state. The parent Pulse Fixer updates the cost/time tiles and builder/card.cost.html from verified evidence, without changing model allocation, then calls mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"cost_llm_time\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]).", pulseRunID, pulseRunID)}},
		{pulseModuleLLMOpsReview, postRunMonitorStep{"llm-ops-review", fmt.Sprintf("PULSE MODULE — LLM + OPERATIONS REVIEW. pulse_run_id=%q. Use the consolidated protocol for this low-frequency coaching pass. Give a generic READ-ONLY REVIEW agent get_reference_doc(kind=\"llm-selection\"), resolved LLM config, plan/step/eval tiers, latest cost/time evidence, any deduplicated efficiency_or_coaching execution-trace findings retained by Bug Review, notification preferences, backup/publish/report readiness, and workflow version. It returns evidence-backed recommendations for tier coverage/use, avoidable model/tool overkill, unnecessary retries or brittle execution shape, useful provider diversity, Pulse/Maintenance model fit, fallbacks, publish/password, notify safety, backup readiness, and version upgrades. It must not reclassify a correctness defect as coaching. It must not edit configuration/files, process human answers, write builder/improve.html, create questions, publish, notify, run the workflow, or mark state. The parent Pulse Fixer processes existing answered `llm-ops-` requests first; it applies only an exact approved bounded edit, verifies it, records and consumes the answer, and never invents providers, models, recipients, destinations, passwords, secrets, or credentials. It then refreshes one deduplicated `LLM & operations recommendations` area and may create at most two material structured decision requests through the existing human-input flow. Informational advice stays in HTML; no configuration changes without exact approval. Finally the parent calls mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"llm_ops_review\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]).", pulseRunID, pulseRunID)}},
		{pulseModuleGoalAdvisor, postRunMonitorStep{"goal-advisor", fmt.Sprintf("PULSE MODULE — GOAL ADVISOR. pulse_run_id=%q. Run the read-only strategy advisor and separate read-only critic defined by the consolidated Pulse protocol. Use Gate evidence and the active .advisor-experiment to choose recovery, healthy 10x/headroom, active-experiment measurement, approved-answer review, or conditional plan-design review. When Gate evidence names a plan-design trigger, the strategy reviewer must load get_workflow_command_guidance(kind=\"design-plan\") as a read-only checklist; its normal write instruction is overridden, so it must not edit builder/improve.html or any workspace file. Use actual run, goal, eval, cost, latency, and failure evidence to return exactly one plan-design disposition: keep, simplify, restructure, or experiment. For any change disposition, compare the current plan with at most two credible alternatives and state expected benefit, affected goal criterion, evidence, risk, migration/rollback, and measurement. The separate critic must challenge whether the recommendation is materially better than the current plan. Preserve one active advisor experiment: it blocks a competing experiment but not plan-design monitoring of whether structure, instrumentation, or implementation prevents a fair test. During measurement, recommend only keep or a repair to the approved experiment unless decisive evidence supports retiring it; do not create an unrelated bold idea. Challenge consequential agent-inferred assumptions, and never turn operational correctness issues such as stale receipts, wrong paths, parsing/schema wiring, or fail-closed behavior into strategic questions; route those findings to Bug Review or Eval Health. Reviewers must not edit files, update builder/improve.html, create/consume questions, or mark module state. The parent Pulse Fixer consolidates advisor and critic results, including any plan-design disposition, records a proposal or advances/applies an exact approved experiment, then calls mark_pulse_module_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, module=\"goal_advisor\", result=\"done|changed|blocked|failed|skipped\", reason=\"...\", evidence=[...]).", pulseRunID, pulseRunID)}},
	}
	const consolidatedReviewProtocol = "PULSE CONSOLIDATED REVIEW PROTOCOL. First load get_reference_doc(kind=\"post-run-monitor\") and follow its Parallel Review Team And Single Fixer section. This fixed module message is only an entry point. Read the current Gate/worklist and get_pulse_module_state. If every due module already has a result for this pulse_run_id, stop. Otherwise this parent turn owns ALL unresolved due modules: launch one READ-ONLY REVIEW call_generic_agent per due module in one parallel tool-call batch, wait naturally for those calls to return, and deduplicate their findings. call_generic_agent is synchronous: its direct tool result is authoritative and no auto-notification is expected. The tool rejects partial provider snapshots and retries one incomplete result once. If a reviewer still fails, do not perform that deep review in the parent; mark only that module failed or blocked with the exact error and continue the safe modules. Use review-artifact-drift, improve-learnings, improve-knowledge, improve-database, improve-report, and improve-evaluation guidance only as read-only reviewer checklists. Then this parent becomes the Pulse Fixer: the only writer, applying bounded fixes sequentially and marking every due module result. Do not use run_in_background, polling, or nested mutating maintenance agents. Treat the later module brief only as domain/evidence guidance; it must not launch another agent. Only the Pulse Fixer updates files, DB/contracts, plan/config, reports/evals, human-input state, changelog review state, module state, and builder/improve.html. Update builder/improve.html once after the consolidated pass."
	for i := range steps {
		steps[i].step.query = consolidatedReviewProtocol + "\n\n" + steps[i].step.query
	}
	byModule := make(map[string]postRunMonitorModuleStep, len(steps))
	for _, step := range steps {
		byModule[step.module] = step
	}
	ordered := make([]postRunMonitorModuleStep, 0, len(steps))
	for _, module := range pulseModuleOrder {
		if step, ok := byModule[module]; ok {
			ordered = append(ordered, step)
		}
	}
	return ordered
}

func postRunMonitorPreBackupStep(pulseRunID string) postRunMonitorStep {
	return postRunMonitorStep{"pre-backup", fmt.Sprintf("PULSE PRE-CHANGE BACKUP. pulse_run_id=%q. The Gate selected one or more mutating/reporting modules. Do not ask for confirmation. Read workflow.json.backup and call get_reference_doc(kind=\"backup-strategy\"). Perform the backup directly in THIS parent workflow turn. Never delegate backup or Git commands through run_in_background, call_generic_agent, a reviewer, or another sub-agent: those agents intentionally cannot write the workflow .git directory. Ensure there is a checkpoint before selected Pulse modules mutate workflow artifacts. If backup is disabled, set up the zero-config local-git default and write workflow.json.backup plus backup/status.json. If backup/status.json says the current source hash is already backed up, record that and skip the actual push. Report the backup result, then stop.", pulseRunID)}
}

func postRunMonitorFinalSteps(pulseRunID string) []postRunMonitorStep {
	return []postRunMonitorStep{
		{"finalize", fmt.Sprintf("PULSE FINALIZER. pulse_run_id=%q. Before final commands, read get_pulse_module_state and confirm every module marked due for this pulse_run_id has a terminal result. If any due result is missing, do not publish or notify a complete Pulse: load get_reference_doc(kind=\"post-run-monitor\"), run the consolidated READ-ONLY REVIEW plus single-fixer protocol for only the unresolved due modules, mark their results, and then continue. Never treat a missing result as skipped or successful. Complete the four final commands below in order in this ONE turn. Do not split them into more chat turns. Before each command call mark_pulse_final_command_result(workspace_path=\"<current workflow>\", pulse_run_id=%q, command=\"dashboard|backup|publish|notify\", status=\"running\", reason=\"...\"); immediately after it finishes call the same tool with done, skipped, blocked, or failed and a factual one-sentence reason. Use the actual single command name, not the pipe-separated example. Continue to Notify even when an earlier command fails, and report the failure honestly.\n\n"+
			"1. DASHBOARD + QUESTIONS. Prepare the final user-facing Pulse/org state. Overwrite builder/card.health.html with one compact org-dashboard card fragment (inline content only, single-quoted attributes). In builder/improve.html refresh Today's outcome and keep technical/module detail collapsed; preserve active Assumptions challenged. Summarize selected modules ran/skipped, Bug/Goal state, user input requests, module outcomes, backup/publish intent, and next action. If a real user/business decision is needed, call create_human_input_request(workspace_path=\"<current workflow>\", source=\"pulse\", ...); Runloop renders it first as Needs your decision, so keep any matching HTML timeline marker compact. Mark dashboard done or failed.\n\n"+
			"2. BACKUP. Mark backup running. Read workflow.json.backup, backup/status.json, and get_reference_doc(kind=\"backup-strategy\"). Perform backup and all Git commands directly in THIS parent finalizer turn. Never delegate them through run_in_background, call_generic_agent, a reviewer, or another sub-agent: delegated agents intentionally cannot write the workflow .git directory. Back up the final Gate + selected-module + dashboard state. If backup is disabled, set up the zero-config local-git default. Skip the actual backup only when the status/source-hash check proves this exact current state is already backed up; mark skipped with that reason. Otherwise perform it, always update backup/status.json, update the latest run row with the result, and mark done or failed.\n\n"+
			"3. PUBLISH. Mark publish running. Read workflow.json.publish and publish/status.json. Mark skipped without deploying when publishing is disabled, has never been verified, or the current publish source hash matches last_source_hash. Only re-publish a verified destination when the source is stale, following get_reference_doc(kind=\"publish-strategy\"). Never perform the first/verifying publish unattended. If backup failed, do not publish unbacked changes; mark publish blocked. Always keep publish/status.json truthful, then mark done, skipped, blocked, or failed.\n\n"+
			"4. NOTIFY. Mark notify running. Read builder/improve.html, builder/card.health.html, db/db.sqlite report_human_inputs, backup/status.json, and publish/status.json. Honor soul/soul.md ## Notifications; otherwise call notify_user once every run with the in-depth run summary. Include modules ran/skipped, Bug/Goal state, user requests, important outcomes, backup/publish status, dashboard URL when live, cost/time evidence or its planned next check, and next action. Gmail/email remains the default rich rendering when available. Mark notify done, skipped only when notifications are explicitly disabled, or failed. Then stop.", pulseRunID, pulseRunID)},
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
	// An incomplete worklist is not permission to spend on every expensive
	// maintenance lane. Preserve only decisions Gate actually recorded as due,
	// plus the deterministic changelog review below.
	wanted := map[string]bool{}
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

	s.updateRuntimeState(scheduleRuntimeKey(sctx), func(state *ScheduleRuntimeState) {
		state.LastSessionID = sessionID
	})

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
	// interactive-run completion directive for interactive runs (and as the fallback
	// when Pulse is off). The shared source-hash
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
	if llmConfig := sctx.Capabilities.LLMConfig; llmConfig != nil {
		if llmConfig.BuilderLLM != nil {
			currentProvider = strings.TrimSpace(llmConfig.BuilderLLM.Provider)
		}
		if currentProvider == "" {
			currentProvider = strings.TrimSpace(llmConfig.Provider)
		}
		if currentProvider == "" {
			if builder, _, ok := workflowtypes.ResolveProviderProfileConfig(llmConfig); ok && builder != nil {
				currentProvider = strings.TrimSpace(builder.Provider)
			}
		}
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
	sessionID := s.newScheduleSessionID(sctx)
	chiefInputWorkspaces := []string{"pulse"}
	if workflows, err := s.DiscoverWorkflowManifestsCached(ctx, 5*time.Second); err != nil {
		s.logf(sctx, "[CHIEF_INPUT] Could not discover workflow scopes for answered questions: %v", err)
	} else {
		for _, workflow := range workflows {
			chiefInputWorkspaces = append(chiefInputWorkspaces, workflow.WorkspacePath)
		}
	}
	answeredChiefInputs := claimAnsweredChiefOfStaffInputsForAgent(ctx, chiefInputWorkspaces, sessionID)
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		releaseChiefOfStaffInputClaims(releaseCtx, chiefInputWorkspaces, sessionID)
	}()
	chiefInputNote := ""
	if answeredChiefInputs != "" {
		chiefInputNote = "\n\n" + answeredChiefInputs
	}
	if query != "" {
		query = withChiefTaskRunContext(sctx, query) + chiefInputNote
	}

	s.updateRuntimeState(scheduleRuntimeKey(sctx), func(state *ScheduleRuntimeState) {
		state.LastSessionID = sessionID
	})

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
	if len(sctx.Capabilities.CDPPorts) > 0 {
		reqMap["cdp_ports"] = append([]int(nil), sctx.Capabilities.CDPPorts...)
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
				msg = withChiefTaskRunContext(sctx, msg) + chiefInputNote
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

	// Single-query path. Use the same tmux/background-aware bounded wait as
	// sequence turns so an abandoned coding CLI cannot hold a schedule forever.
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

	if err := s.waitForWorkshopIdle(ctx, sessionID); err != nil {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Multi-agent session wait failed for %s: %v", sctx.Schedule.ID, err)
		return sessionID, "", fmt.Errorf("multi-agent session idle wait failed: %w", err)
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

// applyLLMAndSecretsToReqMap adds LLM config, API keys, secrets, and trigger payload to a request map.
func (s *SchedulerService) applyLLMAndSecretsToReqMap(ctx context.Context, reqMap map[string]interface{}, sctx *ScheduleContext) {
	if sctx.Capabilities.SelectedGlobalSecretNames != nil {
		reqMap["selected_global_secrets"] = sctx.Capabilities.SelectedGlobalSecretNames
	}

	if sctx.Capabilities.LLMConfig != nil {
		llmCfg := sctx.Capabilities.LLMConfig
		builderLLM := llmCfg.BuilderLLM
		if builderLLM == nil {
			if resolved, _, ok := workflowtypes.ResolveProviderProfileConfig(llmCfg); ok {
				builderLLM = resolved
			}
		}
		provider := ""
		modelID := ""
		var options map[string]interface{}
		if builderLLM != nil {
			provider = strings.TrimSpace(builderLLM.Provider)
			modelID = strings.TrimSpace(builderLLM.ModelID)
			options = builderLLM.Options
		}
		llmConfigSource := ""
		if strings.EqualFold(strings.TrimSpace(sctx.Schedule.WorkshopMode), "optimizer") {
			maintenanceLLM := llmCfg.MaintenanceLLM
			if maintenanceLLM == nil {
				if resolved, ok := workflowtypes.ResolveProviderProfileMaintenanceConfig(llmCfg); ok {
					maintenanceLLM = resolved
				}
			}
			if maintenanceLLM != nil {
				maintenanceProvider := strings.TrimSpace(maintenanceLLM.Provider)
				maintenanceModelID := strings.TrimSpace(maintenanceLLM.ModelID)
				if maintenanceProvider != "" && maintenanceModelID != "" {
					provider = maintenanceProvider
					modelID = maintenanceModelID
					options = maintenanceLLM.Options
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
		if resolved, ok := workflowtypes.ResolveProviderProfilePulseConfig(sctx.Capabilities.LLMConfig); ok {
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
	goalAdvisorLLM := sctx.Capabilities.LLMConfig.MaintenanceLLM
	if goalAdvisorLLM == nil {
		if resolved, ok := workflowtypes.ResolveProviderProfileMaintenanceConfig(sctx.Capabilities.LLMConfig); ok {
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
		if resolved, ok := workflowtypes.ResolveProviderProfileChiefOfStaffConfig(llmCfg); ok {
			return resolved
		}
		return nil
	}

	tierConfig := LoadAndResolveTierConfig(ctx, nil)
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
			if resolved, ok := workflowtypes.ResolveProviderProfileChiefOfStaffConfig(&workflowtypes.PresetLLMConfig{
				SchemaVersion: workflowtypes.LLMConfigSchemaVersion,
				Mode:          workflowtypes.LLMConfigModeProviderProfile,
				Provider:      cfg.Provider,
			}); ok {
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
		Options:  tier.Options,
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
				Options:  fallback.Options,
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
	if len(sctx.Capabilities.CDPPorts) > 0 {
		reqMap["cdp_ports"] = append([]int(nil), sctx.Capabilities.CDPPorts...)
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
var schedulerWorkshopMaxInactivity = 10 * time.Minute
var schedulerGoalAdvisorMaxInactivity = 30 * time.Minute
var errWorkshopIdleWaitTimeout = errors.New("workshop idle wait timed out")
var errWorkshopSequenceInterrupted = errors.New("workshop sequence interrupted by user")
var errWorkshopSessionFailed = errors.New("workshop session failed")

const schedulerWorkshopIdleConsecutiveChecks = 2

// waitForWorkshopIdle polls until all background agents, tracked executions, and
// tmux-backed turns have completed.
func (s *SchedulerService) waitForWorkshopIdle(ctx context.Context, sessionID string) error {
	return s.waitForWorkshopIdleWithInactivityTimeout(ctx, sessionID, schedulerWorkshopMaxInactivity)
}

func (s *SchedulerService) waitForWorkshopIdleWithInactivityTimeout(ctx context.Context, sessionID string, maxInactivity time.Duration) error {
	ticker := time.NewTicker(schedulerWorkshopIdlePollInterval)
	defer ticker.Stop()

	consecutiveIdleChecks := 0
	lastObservedProgress := s.workshopLastProgressAt(sessionID)
	lastProgressAt := time.Now()
	checkUserInterruption := func() error {
		if activeSession, exists := s.api.getActiveSession(sessionID); exists &&
			normalizeSessionLifecycleStatus(activeSession.Status) == sessionLifecycleFailed {
			return fmt.Errorf("%w: session %s status is %s", errWorkshopSessionFailed, sessionID, activeSession.Status)
		}
		if s.api.isSessionMarkedStopped(sessionID) {
			return fmt.Errorf("%w: session %s was stopped", errWorkshopSequenceInterrupted, sessionID)
		}
		if s.api.consumeSessionTurnInterrupted(sessionID) {
			return fmt.Errorf("%w: current response in session %s was canceled", errWorkshopSequenceInterrupted, sessionID)
		}
		return nil
	}
	if err := checkUserInterruption(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := checkUserInterruption(); err != nil {
				return err
			}
			refreshErr := s.refreshSessionTmuxSnapshotsForIdleCheck(ctx, sessionID)
			now := time.Now()
			if observedProgress := s.workshopLastProgressAt(sessionID); observedProgress.After(lastObservedProgress) {
				lastObservedProgress = observedProgress
				lastProgressAt = now
			}
			// A transient tmux capture failure is not proof that the agent failed.
			// Keep observing other progress signals and only cancel after the same
			// inactivity window has elapsed. Do not count a failed refresh as an
			// idle-completion check because the pane state is not fresh.
			if refreshErr != nil {
				consecutiveIdleChecks = 0
				if maxInactivity > 0 && now.Sub(lastProgressAt) >= maxInactivity {
					return fmt.Errorf(
						"%w: no tmux, tool, execution, or session progress for %s in session %s; last tmux refresh error: %w",
						errWorkshopIdleWaitTimeout,
						maxInactivity,
						sessionID,
						refreshErr,
					)
				}
				continue
			}
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
			if maxInactivity > 0 && now.Sub(lastProgressAt) >= maxInactivity {
				return fmt.Errorf(
					"%w: no tmux, tool, execution, or session progress for %s in session %s",
					errWorkshopIdleWaitTimeout,
					maxInactivity,
					sessionID,
				)
			}
		}
	}
}

// workshopLastProgressAt returns the latest observable activity timestamp for a
// scheduled workshop turn. The inactivity timeout is deliberately sliding: a
// long-running maintenance agent remains healthy while its tmux pane, tool calls,
// tracked execution, or parent session continues to advance.
func (s *SchedulerService) workshopLastProgressAt(sessionID string) time.Time {
	if s == nil || s.api == nil || strings.TrimSpace(sessionID) == "" {
		return time.Time{}
	}
	api := s.api
	latest := time.Time{}
	record := func(candidate time.Time) {
		if candidate.After(latest) {
			latest = candidate
		}
	}

	api.activeSessionsMux.RLock()
	if session := api.activeSessions[sessionID]; session != nil {
		record(session.CreatedAt)
		record(session.LastActivity)
	}
	api.activeSessionsMux.RUnlock()

	if api.terminalStore != nil {
		for _, snapshot := range api.terminalStore.ListMetadata(sessionID) {
			record(snapshot.CreatedAt)
			record(snapshot.UpdatedAt)
		}
	}

	if api.bgAgentRegistry != nil {
		for _, agent := range api.bgAgentRegistry.GetAll(sessionID) {
			if agent == nil {
				continue
			}
			snapshot := agent.GetSnapshot()
			record(snapshot.CreatedAt)
			if snapshot.CompletedAt != nil {
				record(*snapshot.CompletedAt)
			}
			for _, call := range agent.GetRecentToolCalls(1) {
				record(call.StartedAt)
				if call.Duration > 0 {
					record(call.StartedAt.Add(call.Duration))
				}
			}
		}
	}

	for _, execution := range api.trackedExecutionsForSession(sessionID) {
		if execution == nil {
			continue
		}
		record(execution.StartedAt)
		if execution.CompletedAt != nil {
			record(*execution.CompletedAt)
		}
	}

	return latest
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

// updateRuntimeState owns the complete read-modify-write operation. Callers must
// never retain a runtime-state pointer after the mutex is released.
func (s *SchedulerService) updateRuntimeState(scheduleID string, update func(*ScheduleRuntimeState)) ScheduleRuntimeState {
	s.runtimeStatesMu.Lock()
	defer s.runtimeStatesMu.Unlock()
	state, ok := s.runtimeStates[scheduleID]
	if !ok {
		state = &ScheduleRuntimeState{}
		s.runtimeStates[scheduleID] = state
	}
	if update != nil {
		update(state)
	}
	return *state
}

// activateScheduleRunLocked publishes the active run and its cancellation
// handle as one runtime-state operation. Caller must hold runtimeStatesMu.
func (s *SchedulerService) activateScheduleRunLocked(state *ScheduleRuntimeState, runID string, startedAt time.Time) context.Context {
	state.LastStatus = "running"
	state.ActiveRunID = runID
	state.LastRunAt = &startedAt
	state.LastError = ""
	return s.registerScheduleRunContext(runID)
}

func (s *SchedulerService) rollbackScheduleRunActivation(runtimeKey, runID string, previous ScheduleRuntimeState) {
	s.runtimeStatesMu.Lock()
	if current := s.runtimeStates[runtimeKey]; current != nil && current.ActiveRunID == runID {
		*current = previous
	}
	s.runtimeStatesMu.Unlock()
	s.releaseScheduleRunContext(runID)
}

func (s *SchedulerService) abortCanceledScheduleRunBeforeStart(ctx context.Context, sctx *ScheduleContext, runtimeKey, runID string) bool {
	if ctx.Err() == nil {
		return false
	}
	s.updateRuntimeState(runtimeKey, func(state *ScheduleRuntimeState) {
		if state.ActiveRunID != runID {
			return
		}
		state.ActiveRunID = ""
		state.LastStatus = "stopped"
		state.LastError = "stopped before execution started"
	})
	s.transitionScheduleRun(context.Background(), sctx, schedulerstate.Transition{
		RunID: runID, To: schedulerstate.StateStopped, Reason: "stopped before execution started",
		ErrorMessage: "stopped before execution started", At: time.Now().UTC(),
	})
	s.releaseScheduleRunContext(runID)
	s.cleanupRemovedScheduleRuntimeState(runtimeKey)
	return true
}

func (s *SchedulerService) cleanupRemovedScheduleRuntimeState(runtimeKey string) {
	s.mu.Lock()
	_, known := s.scheduleFingerprints[runtimeKey]
	s.mu.Unlock()
	if known {
		return
	}
	s.runtimeStatesMu.Lock()
	if state := s.runtimeStates[runtimeKey]; state == nil || state.ActiveRunID == "" {
		delete(s.runtimeStates, runtimeKey)
	}
	s.runtimeStatesMu.Unlock()
}

func (s *SchedulerService) registerScheduleRunContext(runID string) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	s.runCancelsMu.Lock()
	if s.runCancels == nil {
		s.runCancels = make(map[string]context.CancelFunc)
	}
	if previous := s.runCancels[runID]; previous != nil {
		previous()
	}
	s.runCancels[runID] = cancel
	s.runCancelsMu.Unlock()
	return ctx
}

func (s *SchedulerService) cancelScheduleRunContext(runID string) {
	s.runCancelsMu.Lock()
	cancel := s.runCancels[runID]
	s.runCancelsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *SchedulerService) releaseScheduleRunContext(runID string) {
	s.runCancelsMu.Lock()
	cancel := s.runCancels[runID]
	delete(s.runCancels, runID)
	s.runCancelsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *SchedulerService) claimScheduleRun(ctx context.Context, sctx *ScheduleContext, runID string, startedAt time.Time) error {
	s.stateStoreMu.RLock()
	defer s.stateStoreMu.RUnlock()
	if s.stateStore == nil {
		return nil
	}
	scopeType, scopeID, lockKey := scheduleStateScope(sctx)
	triggerSource := strings.TrimSpace(sctx.TriggerSource)
	if triggerSource == "" {
		triggerSource = "cron"
	}
	return s.stateStore.BeginRun(ctx, schedulerstate.Run{
		RunID:         runID,
		ScopeType:     scopeType,
		ScopeID:       scopeID,
		LockKey:       lockKey,
		ScheduleID:    sctx.Schedule.ID,
		TriggerSource: triggerSource,
		StartedAt:     startedAt,
	})
}

func (s *SchedulerService) transitionScheduleRun(ctx context.Context, sctx *ScheduleContext, transition schedulerstate.Transition) {
	if strings.TrimSpace(transition.RunID) == "" {
		return
	}
	s.stateStoreMu.RLock()
	defer s.stateStoreMu.RUnlock()
	if s.stateStore == nil {
		return
	}
	transitionCtx := ctx
	if transitionCtx == nil {
		transitionCtx = context.Background()
	}
	attempts := 1
	if schedulerstate.IsTerminal(transition.To) {
		transitionCtx = context.WithoutCancel(transitionCtx)
		attempts = 3
	}
	var transitionErr error
	for attempt := 0; attempt < attempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(transitionCtx, 5*time.Second)
		transitionErr = s.stateStore.Transition(attemptCtx, transition)
		cancel()
		if transitionErr == nil {
			return
		}
		if attempt+1 < attempts {
			time.Sleep(time.Duration(attempt+1) * 50 * time.Millisecond)
		}
	}
	if schedulerstate.IsTerminal(transition.To) {
		recoveryCtx, cancel := context.WithTimeout(transitionCtx, 5*time.Second)
		recoveryErr := s.stateStore.ForceTerminal(recoveryCtx, transition)
		cancel()
		if recoveryErr == nil {
			if sctx != nil {
				s.logf(sctx, "[SCHEDULER_STATE] recovered terminal transition run=%s to=%s after error: %v", transition.RunID, transition.To, transitionErr)
			} else {
				scheduleLogf("[SCHEDULER_STATE] recovered terminal transition run=%s to=%s after error: %v", transition.RunID, transition.To, transitionErr)
			}
			return
		}
		transitionErr = errors.Join(transitionErr, recoveryErr)
	}
	if transitionErr != nil {
		if sctx != nil {
			s.logf(sctx, "[SCHEDULER_STATE] transition run=%s to=%s failed: %v", transition.RunID, transition.To, transitionErr)
		} else {
			scheduleLogf("[SCHEDULER_STATE] transition run=%s to=%s failed: %v", transition.RunID, transition.To, transitionErr)
		}
	}
}

func (s *SchedulerService) recordScheduleFireDecision(ctx context.Context, sctx *ScheduleContext, decision, reason, runID string, firedAt time.Time) {
	if sctx == nil {
		return
	}
	s.stateStoreMu.RLock()
	defer s.stateStoreMu.RUnlock()
	if s.stateStore == nil {
		return
	}
	scopeType, scopeID, _ := scheduleStateScope(sctx)
	triggerSource := strings.TrimSpace(sctx.TriggerSource)
	if triggerSource == "" {
		triggerSource = "cron"
	}
	if err := s.stateStore.RecordFireDecision(ctx, schedulerstate.FireDecision{
		DecisionID:    uuid.NewString(),
		ScopeType:     scopeType,
		ScopeID:       scopeID,
		ScheduleID:    sctx.Schedule.ID,
		TriggerSource: triggerSource,
		Decision:      decision,
		Reason:        reason,
		RunID:         runID,
		FiredAt:       firedAt,
	}); err != nil {
		s.logf(sctx, "[SCHEDULER_STATE] record fire decision=%s failed: %v", decision, err)
	}
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

func scheduleWithReloadedCalendarItem(sched WorkflowSchedule, requested *CalendarScheduleItem) (WorkflowSchedule, *CalendarScheduleItem, bool) {
	if requested == nil {
		return sched, nil, true
	}
	for i := range sched.CalendarItems {
		item := sched.CalendarItems[i]
		matches := requested.ID != "" && item.ID == requested.ID
		if requested.ID == "" {
			matches = item.Date == requested.Date && item.Time == requested.Time
		}
		if !matches {
			continue
		}
		itemCopy := item
		return scheduleWithCalendarItem(sched, itemCopy), &itemCopy, true
	}
	return sched, nil, false
}

func scheduleConfigFingerprint(sctx *ScheduleContext) string {
	if sctx == nil {
		return ""
	}
	payload, err := json.Marshal(struct {
		Schedule     WorkflowSchedule     `json:"schedule"`
		Capabilities WorkflowCapabilities `json:"capabilities"`
	}{Schedule: sctx.Schedule, Capabilities: sctx.Capabilities})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(payload))
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
