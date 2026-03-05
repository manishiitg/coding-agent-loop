package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"

	"mcp-agent-builder-go/agent_go/pkg/database"
)

// SchedulerService manages cron job execution using gocron
type SchedulerService struct {
	db        database.Database
	api       *StreamingAPI
	scheduler gocron.Scheduler
	mu        sync.Mutex
	jobIDs    map[string]uuid.UUID // schedJobID → gocron job UUID
}

// NewSchedulerService creates a new SchedulerService
func NewSchedulerService(db database.Database, api *StreamingAPI) *SchedulerService {
	return &SchedulerService{
		db:     db,
		api:    api,
		jobIDs: make(map[string]uuid.UUID),
	}
}

// Start loads all enabled jobs and starts the scheduler
func (s *SchedulerService) Start(ctx context.Context) error {
	scheduler, err := gocron.NewScheduler()
	if err != nil {
		return fmt.Errorf("failed to create scheduler: %w", err)
	}
	s.scheduler = scheduler

	// Clean up stale "running" statuses from previous server crash/restart
	allJobs, _, err := s.db.ListScheduledJobs(ctx, 1000, 0, nil, nil)
	if err != nil {
		log.Printf("[SCHEDULER] Failed to list jobs for stale cleanup: %v", err)
	} else {
		for i := range allJobs {
			if allJobs[i].LastStatus == "running" {
				log.Printf("[SCHEDULER] Resetting stale running status for job %s (%s)", allJobs[i].ID, allJobs[i].Name)
				lastRun := time.Now()
				if allJobs[i].LastRunAt != nil {
					lastRun = *allJobs[i].LastRunAt
				}
				if err := s.db.UpdateScheduledJobRunStatus(ctx, allJobs[i].ID, lastRun, allJobs[i].NextRunAt, allJobs[i].LastSessionID, "error", "interrupted by server restart", allJobs[i].LastDurationMs); err != nil {
					log.Printf("[SCHEDULER] Failed to reset stale status for job %s: %v", allJobs[i].ID, err)
				}
			}
			// Also clean up stale run history entries for this job
			runs, _, runErr := s.db.ListScheduledJobRuns(ctx, allJobs[i].ID, 100, 0)
			if runErr == nil {
				for _, run := range runs {
					if run.Status == "running" {
						log.Printf("[SCHEDULER] Resetting stale run entry %s for job %s", run.ID, allJobs[i].ID)
						dur := int64(0)
						if !run.StartedAt.IsZero() {
							dur = time.Since(run.StartedAt).Milliseconds()
						}
						_ = s.db.UpdateScheduledJobRun(ctx, run.ID, "error", "interrupted by server restart", &dur, run.RunFolder, run.SessionID)
					}
				}
			}
		}
	}

	// Load all enabled jobs from DB
	jobs, _, err := s.db.ListScheduledJobs(ctx, 1000, 0, nil, boolPtr(true))
	if err != nil {
		log.Printf("[SCHEDULER] Failed to load scheduled jobs: %v", err)
	} else {
		for i := range jobs {
			if err := s.LoadJob(&jobs[i]); err != nil {
				log.Printf("[SCHEDULER] Failed to load job %s (%s): %v", jobs[i].ID, jobs[i].Name, err)
			}
		}
		log.Printf("[SCHEDULER] Loaded %d scheduled jobs", len(jobs))
	}

	s.scheduler.Start()
	log.Printf("[SCHEDULER] Started")

	// Wait for context cancellation
	<-ctx.Done()
	return nil
}

// Stop shuts down the scheduler
func (s *SchedulerService) Stop() {
	if s.scheduler != nil {
		if err := s.scheduler.Shutdown(); err != nil {
			log.Printf("[SCHEDULER] Error shutting down: %v", err)
		}
	}
}

// LoadJob adds or updates a job in gocron
func (s *SchedulerService) LoadJob(job *database.ScheduledJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove existing gocron job if any
	if existingID, ok := s.jobIDs[job.ID]; ok {
		if err := s.scheduler.RemoveJob(existingID); err != nil {
			log.Printf("[SCHEDULER] Warning: failed to remove old gocron job for %s: %v", job.ID, err)
		}
		delete(s.jobIDs, job.ID)
	}

	if !job.Enabled {
		return nil
	}

	// Build cron expression with timezone prefix if non-UTC
	cronExpr := job.CronExpression
	if job.Timezone != "" && job.Timezone != "UTC" {
		cronExpr = fmt.Sprintf("CRON_TZ=%s %s", job.Timezone, job.CronExpression)
	}

	jobCopy := *job
	gocronJob, err := s.scheduler.NewJob(
		gocron.CronJob(cronExpr, false),
		gocron.NewTask(func() {
			s.triggerJob(&jobCopy)
		}),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
	)
	if err != nil {
		return fmt.Errorf("failed to create gocron job: %w", err)
	}

	s.jobIDs[job.ID] = gocronJob.ID()
	log.Printf("[SCHEDULER] Registered job %s (%s) with cron %q", job.ID, job.Name, job.CronExpression)
	return nil
}

// RemoveJob removes a job from gocron
func (s *SchedulerService) RemoveJob(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existingID, ok := s.jobIDs[id]; ok {
		if err := s.scheduler.RemoveJob(existingID); err != nil {
			return fmt.Errorf("failed to remove gocron job: %w", err)
		}
		delete(s.jobIDs, id)
	}
	return nil
}

// TriggerNow triggers a job immediately (for manual trigger API).
// It marks the job as running and launches execution in a goroutine so the API responds immediately.
func (s *SchedulerService) TriggerNow(id string) (string, error) {
	ctx := context.Background()
	job, err := s.db.GetScheduledJob(ctx, id)
	if err != nil {
		return "", fmt.Errorf("failed to get scheduled job: %w", err)
	}
	if job == nil {
		return "", fmt.Errorf("scheduled job not found: %s", id)
	}

	// Prevent concurrent runs
	if job.LastStatus == "running" {
		return "", fmt.Errorf("job is already running (session: %s)", job.LastSessionID)
	}

	// Mark as running immediately so the UI reflects it
	startTime := time.Now().UTC()
	if err := s.db.UpdateScheduledJobRunStatus(ctx, job.ID, startTime, job.NextRunAt, "", "running", "", nil); err != nil {
		log.Printf("[SCHEDULER] Failed to set running status for job %s: %v", job.ID, err)
	}

	// Run in background so the API responds immediately
	go func() {
		if _, err := s.runJob(context.Background(), job); err != nil {
			log.Printf("[SCHEDULER] Triggered job %s failed: %v", job.ID, err)
		}
	}()

	return "triggered", nil
}

// StopRunningJob stops a running scheduled job by cancelling its session
func (s *SchedulerService) StopRunningJob(job *database.ScheduledJob) {
	if job.LastSessionID == "" {
		return
	}

	// Cancel the agent execution via the StreamingAPI
	sessionID := job.LastSessionID
	log.Printf("[SCHEDULER] Stopping running job %s (session: %s)", job.ID, sessionID)

	// Cancel agent execution context
	s.api.agentCancelMux.Lock()
	if cancelFunc, exists := s.api.agentCancelFuncs[sessionID]; exists {
		cancelFunc()
		delete(s.api.agentCancelFuncs, sessionID)
	}
	s.api.agentCancelMux.Unlock()

	// Cancel workflow orchestrator contexts for this session
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

	log.Printf("[SCHEDULER] Stopped job %s (session: %s)", job.ID, sessionID)
}

// triggerJob is called by gocron when a cron fires
func (s *SchedulerService) triggerJob(job *database.ScheduledJob) {
	ctx := context.Background()
	log.Printf("[SCHEDULER] Triggering job %s (%s)", job.ID, job.Name)

	// Reload job from DB to get current config
	currentJob, err := s.db.GetScheduledJob(ctx, job.ID)
	if err != nil || currentJob == nil {
		log.Printf("[SCHEDULER] Job %s not found in DB, skipping", job.ID)
		return
	}
	if !currentJob.Enabled {
		log.Printf("[SCHEDULER] Job %s is disabled, skipping", job.ID)
		return
	}

	// Prevent concurrent runs — skip if already running
	if currentJob.LastStatus == "running" {
		log.Printf("[SCHEDULER] Job %s is already running (session: %s), skipping this trigger", job.ID, currentJob.LastSessionID)
		return
	}

	if _, err := s.runJob(ctx, currentJob); err != nil {
		log.Printf("[SCHEDULER] Job %s runJob error: %v", job.ID, err)
	}
}

// runJob marks the job as running, executes it, and updates the final status with duration.
func (s *SchedulerService) runJob(ctx context.Context, job *database.ScheduledJob) (string, error) {
	startTime := time.Now().UTC()

	// Mark as running before execution
	if err := s.db.UpdateScheduledJobRunStatus(ctx, job.ID, startTime, job.NextRunAt, "", "running", "", nil); err != nil {
		log.Printf("[SCHEDULER] Failed to set running status for job %s: %v", job.ID, err)
	}

	// Create a run history entry
	runID := uuid.New().String()
	run := &database.ScheduledJobRun{
		ID:        runID,
		JobID:     job.ID,
		SessionID: "",
		Status:    "running",
		GroupIDs:  job.GroupIDs,
		StartedAt: startTime,
	}
	if err := s.db.CreateScheduledJobRun(ctx, run); err != nil {
		log.Printf("[SCHEDULER] Failed to create run entry for job %s: %v", job.ID, err)
	}

	// Snapshot iteration folders before execution to detect the new one
	workspacePath := s.getJobWorkspacePath(ctx, job)
	foldersBefore := s.listIterationFolders(workspacePath)

	sessionID, execErr := s.executeJob(ctx, job)

	// Calculate duration and next run
	durationMs := time.Since(startTime).Milliseconds()
	nextRun := s.getNextRunTime(job)

	status := "success"
	errMsg := ""
	if execErr != nil {
		status = "error"
		errMsg = execErr.Error()
		log.Printf("[SCHEDULER] Job %s failed in %dms: %v", job.ID, durationMs, execErr)
	} else {
		log.Printf("[SCHEDULER] Job %s completed in %dms, session: %s", job.ID, durationMs, sessionID)
	}

	if err := s.db.UpdateScheduledJobRunStatus(ctx, job.ID, startTime, nextRun, sessionID, status, errMsg, &durationMs); err != nil {
		log.Printf("[SCHEDULER] Failed to update run status for job %s: %v", job.ID, err)
	}

	// Detect new iteration folder by comparing before/after
	runFolder := ""
	foldersAfter := s.listIterationFolders(workspacePath)
	for _, f := range foldersAfter {
		found := false
		for _, fb := range foldersBefore {
			if f == fb {
				found = true
				break
			}
		}
		if !found {
			runFolder = f
			break
		}
	}

	// Update the run history entry with results
	if err := s.db.UpdateScheduledJobRun(ctx, runID, status, errMsg, &durationMs, runFolder, sessionID); err != nil {
		log.Printf("[SCHEDULER] Failed to update run entry for job %s: %v", job.ID, err)
	}

	return sessionID, execErr
}

// executeJob creates a session and runs the job's preset
func (s *SchedulerService) executeJob(ctx context.Context, job *database.ScheduledJob) (string, error) {
	// Load preset from DB
	preset, err := s.db.GetPresetQuery(ctx, job.PresetQueryID)
	if err != nil {
		return "", fmt.Errorf("failed to get preset query %s: %w", job.PresetQueryID, err)
	}
	if preset == nil {
		return "", fmt.Errorf("preset query not found: %s", job.PresetQueryID)
	}

	// Build base request from preset.
	// handleQuery loads most config (servers, tools, skills, browser, code exec mode, preset LLM)
	// from the preset via preset_query_id, so we only need to pass what it can't load itself.
	query := preset.Query
	if query == "" {
		query = preset.Label
	}
	if query == "" {
		query = "Execute workflow"
	}
	reqMap := map[string]interface{}{
		"query":           query,
		"agent_mode":      preset.AgentMode,
		"preset_query_id": preset.ID,
	}

	// Pass workspace folder (required for workflow mode)
	if preset.SelectedFolder.Valid && preset.SelectedFolder.String != "" {
		reqMap["selected_folder"] = preset.SelectedFolder.String
	}

	// Pass LLM config with API keys from server environment.
	// handleQuery uses req.LLMConfig as the orchestrator's base/fallback LLM (with API keys);
	// the preset's agent-specific LLMs (execution/validation/learning) are loaded separately
	// by handleQuery from preset_query_id, but they still need the base LLM's API keys.
	if len(preset.LLMConfig) > 0 {
		var presetLLM database.PresetLLMConfig
		if err := json.Unmarshal(preset.LLMConfig, &presetLLM); err == nil && presetLLM.Provider != "" && presetLLM.ModelID != "" {
			llmConfig := map[string]interface{}{
				"primary": map[string]interface{}{
					"provider": presetLLM.Provider,
					"model_id": presetLLM.ModelID,
				},
			}
			// Include API keys from server environment (reuses buildProviderAPIKeysFromEnv
			// used by locked-mode UI requests) so LLM providers can authenticate
			apiKeys := buildSchedulerAPIKeys()
			if len(apiKeys) > 0 {
				llmConfig["api_keys"] = apiKeys
			}
			reqMap["llm_config"] = llmConfig
			log.Printf("[SCHEDULER] Using preset LLM config: %s/%s", presetLLM.Provider, presetLLM.ModelID)
		}
	}

	// Apply trigger_payload overrides
	if len(job.TriggerPayload) > 0 {
		var overrides map[string]interface{}
		if err := json.Unmarshal(job.TriggerPayload, &overrides); err == nil {
			for k, v := range overrides {
				reqMap[k] = v
			}
		}
	}

	// Build execution_options: always create a new run iteration for scheduled executions,
	// and skip interactive prompts (no UI to respond to them).
	execOpts := map[string]interface{}{
		"run_mode":           "create_new_runs_always",
		"execution_strategy": "start_from_beginning",
	}
	if len(job.GroupIDs) > 0 {
		execOpts["enabled_group_ids"] = job.GroupIDs
	}
	reqMap["execution_options"] = execOpts

	// Generate session ID
	sessionID := fmt.Sprintf("sched_%s_%d", job.ID[:8], time.Now().UnixNano())

	// Use startSessionInternal to run the query (same as bot connector pattern)
	runErr := s.api.startSessionInternal(ctx, reqMap, sessionID, "", nil)
	if runErr != nil {
		return sessionID, fmt.Errorf("session execution failed: %w", runErr)
	}

	return sessionID, nil
}

// getNextRunTime calculates the next scheduled run time for a job
func (s *SchedulerService) getNextRunTime(job *database.ScheduledJob) *time.Time {
	loc, err := time.LoadLocation(job.Timezone)
	if err != nil {
		loc = time.UTC
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	schedule, err := parser.Parse(job.CronExpression)
	if err != nil {
		return nil
	}

	next := schedule.Next(time.Now().In(loc)).UTC()
	return &next
}

// ValidateCronExpression validates a 5-field cron expression
func ValidateCronExpression(expr string) error {
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("invalid cron expression: %w", err)
	}
	return nil
}

func boolPtr(b bool) *bool {
	return &b
}

// getJobWorkspacePath returns the workspace path for a scheduled job's preset
func (s *SchedulerService) getJobWorkspacePath(ctx context.Context, job *database.ScheduledJob) string {
	preset, err := s.db.GetPresetQuery(ctx, job.PresetQueryID)
	if err != nil || preset == nil {
		return ""
	}
	if preset.SelectedFolder.Valid {
		return preset.SelectedFolder.String
	}
	return ""
}

// listIterationFolders returns sorted iteration folder names from a workspace's runs directory.
// Uses the workspace API (same as handleGetRunFolders) to list folders.
func (s *SchedulerService) listIterationFolders(workspacePath string) []string {
	if workspacePath == "" {
		return nil
	}

	apiURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil
	}
	q := req.URL.Query()
	q.Add("folder", workspacePath+"/runs")
	q.Add("max_depth", "1")
	req.URL.RawQuery = q.Encode()

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	// Parse workspace API response (same format as handleGetRunFolders)
	var apiResp struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil || !apiResp.Success {
		return nil
	}

	// Data is an array of folder items with children
	var items []struct {
		FilePath string `json:"filepath"`
		Type     string `json:"type"`
		Children []struct {
			FilePath string `json:"filepath"`
			Type     string `json:"type"`
		} `json:"children"`
	}
	if err := json.Unmarshal(apiResp.Data, &items); err != nil {
		return nil
	}

	iterRe := regexp.MustCompile(`iteration-(\d+)$`)
	var folders []string
	// Check top-level items first (the runs folder itself), then its children
	for _, item := range items {
		if item.Type == "folder" && iterRe.MatchString(item.FilePath) {
			matches := iterRe.FindStringSubmatch(item.FilePath)
			if len(matches) > 0 {
				folders = append(folders, "iteration-"+matches[1])
			}
		}
		for _, child := range item.Children {
			if child.Type == "folder" && iterRe.MatchString(child.FilePath) {
				matches := iterRe.FindStringSubmatch(child.FilePath)
				if len(matches) > 0 {
					folders = append(folders, "iteration-"+matches[1])
				}
			}
		}
	}
	sort.Strings(folders)
	return folders
}

// buildSchedulerAPIKeys builds API keys map from environment variables for scheduled job execution.
// Reuses buildProviderAPIKeysFromEnv() (used by locked-mode UI requests) and converts to JSON-compatible map.
func buildSchedulerAPIKeys() map[string]interface{} {
	envKeys := buildProviderAPIKeysFromEnv()
	keys := map[string]interface{}{}
	if envKeys.OpenRouter != nil {
		keys["openrouter"] = *envKeys.OpenRouter
	}
	if envKeys.OpenAI != nil {
		keys["openai"] = *envKeys.OpenAI
	}
	if envKeys.Anthropic != nil {
		keys["anthropic"] = *envKeys.Anthropic
	}
	if envKeys.Vertex != nil {
		keys["vertex"] = *envKeys.Vertex
	}
	if envKeys.GeminiCLI != nil {
		keys["gemini_cli"] = *envKeys.GeminiCLI
	}
	if envKeys.Bedrock != nil {
		keys["bedrock"] = map[string]interface{}{"region": envKeys.Bedrock.Region}
	}
	if envKeys.Azure != nil {
		keys["azure"] = map[string]interface{}{
			"endpoint":    envKeys.Azure.Endpoint,
			"api_key":     envKeys.Azure.APIKey,
			"api_version": envKeys.Azure.APIVersion,
			"region":      envKeys.Azure.Region,
		}
	}
	return keys
}
