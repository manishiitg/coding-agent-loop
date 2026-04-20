package server

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
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

// SchedulerService manages cron job execution using gocron.
// All schedule configuration comes from workflow.json manifests — no database dependency.
type SchedulerService struct {
	api       *StreamingAPI
	scheduler gocron.Scheduler
	mu        sync.Mutex
	jobIDs    map[string]uuid.UUID // scheduleID → gocron job UUID

	// In-memory runtime state per schedule (survives within server lifetime, reset on restart)
	runtimeStates   map[string]*ScheduleRuntimeState
	runtimeStatesMu sync.RWMutex

	// Schedule-to-workspace index for quick lookups
	workspaceIndex   map[string]string // scheduleID → workspacePath
	workspaceIndexMu sync.RWMutex

	// Schedule-to-user index for multi-agent schedules
	userIndex   map[string]string // scheduleID → userID
	userIndexMu sync.RWMutex
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
		jobIDs:         make(map[string]uuid.UUID),
		runtimeStates:  make(map[string]*ScheduleRuntimeState),
		workspaceIndex: make(map[string]string),
		userIndex:      make(map[string]string),
	}
}

// Start scans all workspace folders for workflow.json manifests, loads enabled schedules,
// and starts the gocron scheduler.
func (s *SchedulerService) Start(ctx context.Context) error {
	scheduleLogf("[SCHEDULER] Starting manifest-based scheduler service...")
	scheduler, err := gocron.NewScheduler()
	if err != nil {
		return fmt.Errorf("failed to create scheduler: %w", err)
	}
	s.scheduler = scheduler

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

	s.scheduler.Start()
	scheduleLogf("[SCHEDULER] ✅ Started with %d schedules. Server time: %s, timezone: %s",
		loaded, time.Now().Format(time.RFC3339), time.Now().Location().String())

	// Periodically rescan multi-agent schedule files for changes (written by agents via shell)
	go s.multiAgentRescanLoop(ctx)

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
			_, isLoaded := s.jobIDs[sched.ID]
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
	if s.scheduler != nil {
		if err := s.scheduler.Shutdown(); err != nil {
			scheduleLogf("[SCHEDULER] Error shutting down: %v", err)
		}
	}
}

// LoadSchedule registers a schedule in gocron from a ScheduleContext.
func (s *SchedulerService) LoadSchedule(sctx *ScheduleContext) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sched := sctx.Schedule

	// Remove existing gocron job if any
	if existingID, ok := s.jobIDs[sched.ID]; ok {
		if err := s.scheduler.RemoveJob(existingID); err != nil {
			s.logf(sctx, "[SCHEDULER] Warning: failed to remove old gocron job for %s: %v", sched.ID, err)
		}
		delete(s.jobIDs, sched.ID)
	}

	if !sched.Enabled {
		return nil
	}

	// Build cron expression with timezone prefix
	cronExpr := sched.CronExpression
	if sched.Timezone != "" && sched.Timezone != "UTC" {
		cronExpr = fmt.Sprintf("CRON_TZ=%s %s", sched.Timezone, sched.CronExpression)
	}

	sctxCopy := *sctx
	gocronJob, err := s.scheduler.NewJob(
		gocron.CronJob(cronExpr, false),
		gocron.NewTask(func() {
			s.triggerSchedule(&sctxCopy)
		}),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	)
	if err != nil {
		return fmt.Errorf("failed to create gocron job: %w", err)
	}

	s.jobIDs[sched.ID] = gocronJob.ID()

	// Update workspace index
	s.workspaceIndexMu.Lock()
	s.workspaceIndex[sched.ID] = sctx.WorkspacePath
	s.workspaceIndexMu.Unlock()

	// Update user index for multi-agent schedules
	if sctx.UserID != "" {
		s.userIndexMu.Lock()
		s.userIndex[sched.ID] = sctx.UserID
		s.userIndexMu.Unlock()
	}

	// Initialize runtime state with next run
	nextRun := getNextRunTime(sched.CronExpression, sched.Timezone)
	state := s.getOrCreateRuntimeState(sched.ID)
	state.NextRunAt = nextRun

	nextRunStr := "unknown"
	if nextRun != nil {
		nextRunStr = nextRun.Format(time.RFC3339)
	}
	s.logf(sctx, "[SCHEDULER] Registered schedule %s (%s) cron=%q timezone=%s next_run=%s",
		sched.ID, sched.Name, sched.CronExpression, sched.Timezone, nextRunStr)
	return nil
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

	// Schedule not found — remove it from gocron
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

// RemoveJob removes a schedule from gocron.
func (s *SchedulerService) RemoveJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existingID, ok := s.jobIDs[id]; ok {
		if err := s.scheduler.RemoveJob(existingID); err != nil {
			return fmt.Errorf("failed to remove gocron job: %w", err)
		}
		delete(s.jobIDs, id)
	}

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
	defer s.runtimeStatesMu.RUnlock()
	if state, ok := s.runtimeStates[scheduleID]; ok {
		return *state
	}
	return ScheduleRuntimeState{}
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
	s.runtimeStatesMu.Unlock()

	sctx := buildScheduleContext(workspacePath, manifest, *sched)

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
	s.runtimeStatesMu.Unlock()

	sctx := buildMultiAgentScheduleContext(userID, *sched, f.Capabilities)

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

// triggerSchedule is called by gocron when a cron fires.
func (s *SchedulerService) triggerSchedule(sctx *ScheduleContext) {
	schedID := sctx.Schedule.ID
	s.logf(sctx, "[SCHEDULER] ⏰ Cron fired for %s (%s) at %s", schedID, sctx.Schedule.Name, time.Now().Format(time.RFC3339))

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
	s.runtimeStatesMu.Unlock()

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

	query := sctx.WorkflowLabel
	if query == "" {
		query = "Execute workflow"
	}

	reqMap := map[string]interface{}{
		"query":                   query,
		"agent_mode":              "workflow",
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
	idPrefix := sctx.Schedule.ID
	if len(idPrefix) > 8 {
		idPrefix = idPrefix[:8]
	}
	sessionID := fmt.Sprintf("sched_%s_%d", idPrefix, time.Now().UnixNano())

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

	idPrefix := sctx.Schedule.ID
	if len(idPrefix) > 8 {
		idPrefix = idPrefix[:8]
	}
	sessionID := fmt.Sprintf("sched_%s_%d", idPrefix, time.Now().UnixNano())

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

		if err := s.waitForWorkshopIdle(ctx, sessionID); err != nil {
			return sessionID, runFolder, fmt.Errorf("workshop idle wait failed after message %d: %w", i+1, err)
		}

		s.sessionLogf(sctx, sessionID, "[SCHEDULER] Workshop message %d/%d completed", i+1, len(messages))
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

	idPrefix := sctx.Schedule.ID
	if len(idPrefix) > 8 {
		idPrefix = idPrefix[:8]
	}
	sessionID := fmt.Sprintf("sched_%s_%d", idPrefix, time.Now().UnixNano())

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
		userSecrets := s.api.loadSelectedUserSecrets(context.Background(), sctx.UserID, sctx.Capabilities.SelectedSecrets)
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

	// Wait for session to complete (no workflow orchestrator, just wait for agent to finish)
	if err := s.waitForSessionComplete(ctx, sessionID); err != nil {
		s.sessionLogf(sctx, sessionID, "[SCHEDULER] ⚠️ Multi-agent session wait interrupted for %s: %v", sctx.Schedule.ID, err)
	}

	return sessionID, "", nil
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

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			// Try to capture run folder from active execution registry
			if !runFolderWritten {
				s.api.activeWorkflowExecutionsMux.RLock()
				for _, exec := range s.api.activeWorkflowExecutions {
					if exec.SessionID == sessionID && exec.RunFolder != "" {
						runFolderWritten = true
						if runID != "" {
							_ = UpdateScheduleRun(ctx, workspacePath, runID, "running", "", nil, exec.RunFolder, "")
							scheduleLogf("[SCHEDULER] 📁 Detected run folder: %s (session %s)", exec.RunFolder, sessionID)
						}
						break
					}
				}
				s.api.activeWorkflowExecutionsMux.RUnlock()
			}

			// Check if all queries are done
			s.api.sessionQueryIDMux.RLock()
			queryIDs := s.api.sessionQueryIDs[sessionID]
			s.api.sessionQueryIDMux.RUnlock()

			if len(queryIDs) == 0 {
				// All orchestrator queries finished
				detectedFolder := ""
				if runFolderWritten {
					s.api.activeWorkflowExecutionsMux.RLock()
					for _, exec := range s.api.activeWorkflowExecutions {
						if exec.SessionID == sessionID && exec.RunFolder != "" {
							detectedFolder = exec.RunFolder
							break
						}
					}
					s.api.activeWorkflowExecutionsMux.RUnlock()
				}
				return detectedFolder, nil
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

	normalizedWorkspace := filepath.Clean(workspacePath)

	s.api.activeWorkflowExecutionsMux.RLock()
	defer s.api.activeWorkflowExecutionsMux.RUnlock()

	for _, exec := range s.api.activeWorkflowExecutions {
		if exec == nil || strings.TrimSpace(exec.WorkspacePath) == "" {
			continue
		}
		if filepath.Clean(exec.WorkspacePath) == normalizedWorkspace {
			execCopy := *exec
			return &execCopy
		}
	}

	return nil
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
	loc, err := time.LoadLocation(timezone)
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

// ValidateCronExpression validates a 5-field cron expression.
func ValidateCronExpression(expr string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}
