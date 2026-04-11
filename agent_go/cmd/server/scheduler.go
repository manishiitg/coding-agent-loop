package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	todo_creation_human "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow"
)

// ScheduleContext bundles everything needed to identify and execute a schedule.
type ScheduleContext struct {
	WorkspacePath string
	WorkflowID    string
	WorkflowLabel string
	Schedule      WorkflowSchedule
	Capabilities  WorkflowCapabilities
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
}

// NewSchedulerService creates a new manifest-based SchedulerService.
func NewSchedulerService(api *StreamingAPI) *SchedulerService {
	return &SchedulerService{
		api:            api,
		jobIDs:         make(map[string]uuid.UUID),
		runtimeStates:  make(map[string]*ScheduleRuntimeState),
		workspaceIndex: make(map[string]string),
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

	s.scheduler.Start()
	scheduleLogf("[SCHEDULER] ✅ Started with %d schedules. Server time: %s, timezone: %s",
		loaded, time.Now().Format(time.RFC3339), time.Now().Location().String())

	// Wait for context cancellation
	<-ctx.Done()
	scheduleLogf("[SCHEDULER] Shutting down (context canceled)")
	return nil
}

// discoveredWorkflow holds a manifest + its workspace path.
type discoveredWorkflow struct {
	WorkspacePath string
	Manifest      *WorkflowManifest
}

// discoverWorkflows scans workspace-docs/Workflow/ for workflow.json files.
func (s *SchedulerService) discoverWorkflows(ctx context.Context) []discoveredWorkflow {
	var results []discoveredWorkflow

	// The workspace root is workspace-docs/ relative to the working directory
	workflowRoot := filepath.Join("../workspace-docs", "Workflow")
	entries, err := os.ReadDir(workflowRoot)
	if err != nil {
		// Try without ../ prefix (server may already be in root)
		workflowRoot = filepath.Join("workspace-docs", "Workflow")
		entries, err = os.ReadDir(workflowRoot)
		if err != nil {
			scheduleLogf("[SCHEDULER] Cannot scan workflow directory: %v", err)
			return nil
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Workspace path is relative: "Workflow/<name>"
		wsPath := "Workflow/" + entry.Name()
		manifest, found, mErr := ReadWorkflowManifest(ctx, wsPath)
		if mErr != nil || !found {
			continue
		}
		if len(manifest.Schedules) > 0 {
			results = append(results, discoveredWorkflow{
				WorkspacePath: wsPath,
				Manifest:      manifest,
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
			scheduleLogf("[SCHEDULER] Warning: failed to remove old gocron job for %s: %v", sched.ID, err)
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

	// Initialize runtime state with next run
	nextRun := getNextRunTime(sched.CronExpression, sched.Timezone)
	state := s.getOrCreateRuntimeState(sched.ID)
	state.NextRunAt = nextRun

	nextRunStr := "unknown"
	if nextRun != nil {
		nextRunStr = nextRun.Format(time.RFC3339)
	}
	scheduleLogf("[SCHEDULER] Registered schedule %s (%s) cron=%q timezone=%s next_run=%s",
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
	scheduleLogf("[SCHEDULER] ⏰ Cron fired for %s (%s) at %s", schedID, sctx.Schedule.Name, time.Now().Format(time.RFC3339))

	paused, cfg, err := s.IsGloballyPaused(context.Background())
	if err != nil {
		scheduleLogf("[SCHEDULER] ⚠️ Failed to read scheduler config before trigger %s: %v", schedID, err)
	} else if paused {
		pausedAt := ""
		if cfg != nil && cfg.PausedAt != nil {
			pausedAt = cfg.PausedAt.Format(time.RFC3339)
		}
		if pausedAt != "" {
			scheduleLogf("[SCHEDULER] ⏸️ Global scheduler pause active since %s, skipping %s", pausedAt, schedID)
		} else {
			scheduleLogf("[SCHEDULER] ⏸️ Global scheduler pause active, skipping %s", schedID)
		}
		return
	}

	// Reload manifest for latest config
	manifest, found, err := ReadWorkflowManifest(context.Background(), sctx.WorkspacePath)
	if err != nil || !found {
		scheduleLogf("[SCHEDULER] ❌ Failed to reload manifest for %s: %v", schedID, err)
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
		scheduleLogf("[SCHEDULER] ❌ Schedule %s not found in manifest, skipping", schedID)
		return
	}
	if !currentSched.Enabled {
		scheduleLogf("[SCHEDULER] ⏭️ Schedule %s is disabled, skipping", schedID)
		return
	}

	if activeExec := s.findActiveExecutionForWorkspace(sctx.WorkspacePath); activeExec != nil {
		triggeredBy := activeExec.TriggeredBy
		if strings.TrimSpace(triggeredBy) == "" {
			triggeredBy = "unknown"
		}
		scheduleLogf("[SCHEDULER] ⏭️ Workflow %s already has an active %s run (session: %s), skipping schedule %s",
			sctx.WorkspacePath, triggeredBy, activeExec.SessionID, schedID)
		return
	}

	// Prevent concurrent runs — check and mark atomically under the write lock
	startTime := time.Now().UTC()
	s.runtimeStatesMu.Lock()
	state := s.getRuntimeStateLocked(schedID)
	if state.LastStatus == "running" {
		s.runtimeStatesMu.Unlock()
		scheduleLogf("[SCHEDULER] ⏭️ Schedule %s is already running (session: %s), skipping", schedID, state.LastSessionID)
		return
	}
	state.LastStatus = "running"
	state.LastRunAt = &startTime
	s.runtimeStatesMu.Unlock()

	// Update context with fresh manifest data
	freshCtx := buildScheduleContext(sctx.WorkspacePath, manifest, *currentSched)

	scheduleLogf("[SCHEDULER] 🚀 Starting %s (%s)", schedID, currentSched.Name)
	if _, err := s.runJob(context.Background(), freshCtx); err != nil {
		scheduleLogf("[SCHEDULER] ❌ %s failed: %v", schedID, err)
	} else {
		scheduleLogf("[SCHEDULER] ✅ %s completed", schedID)
	}
}

// runJob executes a scheduled job: updates runtime state, creates run history, executes, updates results.
func (s *SchedulerService) runJob(ctx context.Context, sctx *ScheduleContext) (string, error) {
	schedID := sctx.Schedule.ID
	startTime := time.Now().UTC()
	scheduleLogf("[SCHEDULER] runJob starting for %s (%s) at %s, groups=%v",
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
	if err := AppendScheduleRun(ctx, sctx.WorkspacePath, run); err != nil {
		scheduleLogf("[SCHEDULER] Failed to create run entry for %s: %v", schedID, err)
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
		scheduleLogf("[SCHEDULER] %s failed in %dms: %v", schedID, durationMs, execErr)
	} else {
		scheduleLogf("[SCHEDULER] %s completed in %dms, session: %s, folder: %s", schedID, durationMs, sessionID, runFolder)
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
	if err := UpdateScheduleRun(ctx, sctx.WorkspacePath, runID, status, errMsg, &durationMs, runFolder, sessionID); err != nil {
		scheduleLogf("[SCHEDULER] Failed to update run entry for %s: %v", schedID, err)
	}

	return sessionID, execErr
}

// executeJob builds a session request from the manifest and runs it.
// Returns (sessionID, runFolder, error).
func (s *SchedulerService) executeJob(ctx context.Context, sctx *ScheduleContext, runID string) (string, string, error) {
	// Dispatch to workshop mode if configured
	if sctx.Schedule.Mode == "workshop" {
		return s.executeWorkshopJob(ctx, sctx, runID)
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

	scheduleLogf("[SCHEDULER] executeJob for %s (%s): session=%s workspace=%s",
		sctx.Schedule.ID, sctx.Schedule.Name, sessionID, sctx.WorkspacePath)

	runErr := s.api.startSessionInternal(ctx, reqMap, sessionID, "", nil)
	if runErr != nil {
		return sessionID, "", fmt.Errorf("session execution failed: %w", runErr)
	}

	// Wait for workflow orchestrator to finish (background goroutines)
	detectedFolder, waitErr := s.waitForWorkflowComplete(ctx, sessionID, runID, sctx.WorkspacePath)
	if waitErr != nil {
		scheduleLogf("[SCHEDULER] ⚠️ Workflow wait interrupted for %s: %v", sctx.Schedule.ID, waitErr)
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

	scheduleLogf("[SCHEDULER] Workshop mode: executing %d messages for %s (session=%s workspace=%s run_folder=%s)",
		len(messages), sctx.Schedule.ID, sessionID, sctx.WorkspacePath, runFolder)

	baseReqMap := s.buildWorkshopRequest(ctx, sctx)

	for i, msg := range messages {
		scheduleLogf("[SCHEDULER] Workshop message %d/%d: %q", i+1, len(messages), msg)

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

		scheduleLogf("[SCHEDULER] Workshop message %d/%d completed", i+1, len(messages))
	}

	if shouldAutoGenerateWorkshopReport(sctx.Schedule, messages) {
		reportPath, reportErr := s.generateWorkshopScheduleReport(ctx, sctx, sessionID, runFolder)
		if reportErr != nil {
			scheduleLogf("[SCHEDULER] ⚠️ Auto-report generation failed for %s run %s: %v", sctx.Schedule.ID, runFolder, reportErr)
		} else if reportPath != "" {
			scheduleLogf("[SCHEDULER] 📝 Auto-report generated for %s run %s → %s", sctx.Schedule.ID, runFolder, reportPath)
		} else {
			scheduleLogf("[SCHEDULER] 📝 Auto-report generated for %s run %s", sctx.Schedule.ID, runFolder)
		}
	}

	scheduleLogf("[SCHEDULER] ✅ Workshop execution completed for %s, session=%s, folder=%s", sctx.Schedule.ID, sessionID, runFolder)
	return sessionID, runFolder, nil
}

func shouldAutoGenerateWorkshopReport(schedule WorkflowSchedule, messages []string) bool {
	workshopMode := strings.TrimSpace(schedule.WorkshopMode)
	if workshopMode == "" {
		workshopMode = "runner"
	}
	if workshopMode != "runner" {
		return false
	}
	for _, message := range messages {
		if strings.Contains(message, "run_full_report") {
			return false
		}
	}
	return true
}

func (s *SchedulerService) generateWorkshopScheduleReport(ctx context.Context, sctx *ScheduleContext, sessionID, runFolder string) (string, error) {
	runFolder = strings.TrimSpace(runFolder)
	if runFolder == "" {
		return "", fmt.Errorf("run folder is required for report generation")
	}
	if !strings.Contains(runFolder, "/") {
		return "", fmt.Errorf("run folder must be group-scoped for report generation: %s", runFolder)
	}

	reqMap := s.buildWorkshopRequest(ctx, sctx)
	reqMap["preset_query_id"] = sctx.WorkflowID
	reqMap["selected_folder"] = sctx.WorkspacePath

	reqJSON, err := json.Marshal(reqMap)
	if err != nil {
		return "", fmt.Errorf("failed to marshal workshop request for report generation: %w", err)
	}

	var req QueryRequest
	if err := json.Unmarshal(reqJSON, &req); err != nil {
		return "", fmt.Errorf("failed to parse workshop request for report generation: %w", err)
	}

	workshopCfg, err := s.api.buildWorkshopConfig(ctx, req, "", sctx.WorkspacePath, runFolder, sctx.Capabilities.SelectedServers, sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to build workshop config for report generation: %w", err)
	}

	controller, err := todo_creation_human.NewStepBasedWorkflowOrchestrator(
		ctx,
		"", "", 0.7, "simple",
		workshopCfg.SelectedServers,
		workshopCfg.SelectedTools,
		workshopCfg.UseCodeExecutionMode,
		workshopCfg.MCPConfigPath,
		workshopCfg.LLMConfig,
		100,
		workshopCfg.Logger,
		nil,
		workshopCfg.EventBridge,
		workshopCfg.CustomTools,
		workshopCfg.CustomToolExecutors,
		workshopCfg.ToolCategories,
		workshopCfg.PresetPhaseLLM,
		workshopCfg.UseKnowledgebase,
		workshopCfg.TieredConfig,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create report controller: %w", err)
	}

	controller.SetWorkspacePath(workshopCfg.WorkspacePath)
	controller.SetSelectedRunFolder(runFolder)
	if workshopCfg.SessionID != "" {
		controller.SetHTTPSessionID(workshopCfg.SessionID)
	}
	if len(workshopCfg.Secrets) > 0 {
		controller.SetSecrets(workshopCfg.Secrets)
	}
	if len(workshopCfg.SelectedSkills) > 0 {
		controller.SetSelectedSkills(workshopCfg.SelectedSkills)
	}
	if workshopCfg.WorkspaceEnvRef != nil {
		controller.SetWorkspaceEnvRef(workshopCfg.WorkspaceEnvRef)
	}

	result, err := controller.ExecuteFinalOutputOnly(ctx, sctx.WorkflowLabel, workshopCfg.WorkspacePath, runFolder)
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "output_path:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "output_path:")), nil
		}
	}

	return "", nil
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
		"phase_id":                "workflow-builder",
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
			return "runner"
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

// findScheduleByID scans all workspace manifests to find a schedule by ID.
// Returns (workspacePath, manifest, scheduleIndex, error).
func findScheduleByID(ctx context.Context, scheduleID string) (string, *WorkflowManifest, int, error) {
	workflowRoot := filepath.Join("../workspace-docs", "Workflow")
	entries, err := os.ReadDir(workflowRoot)
	if err != nil {
		workflowRoot = filepath.Join("workspace-docs", "Workflow")
		entries, err = os.ReadDir(workflowRoot)
		if err != nil {
			return "", nil, 0, fmt.Errorf("cannot scan workflow directory: %w", err)
		}
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		wsPath := "Workflow/" + entry.Name()
		manifest, found, mErr := ReadWorkflowManifest(ctx, wsPath)
		if mErr != nil || !found {
			continue
		}
		for i, sched := range manifest.Schedules {
			if sched.ID == scheduleID {
				return wsPath, manifest, i, nil
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
