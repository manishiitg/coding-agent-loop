package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// ScheduledJobResponse is the API response for a scheduled job.
// Designed to be backward-compatible with the old DB-based ScheduledJob shape.
type ScheduledJobResponse struct {
	ID                  string          `json:"id"`
	Name                string          `json:"name"`
	Description         string          `json:"description"`
	EntityType          string          `json:"entity_type"`
	WorkspacePath       string          `json:"workspace_path"`
	WorkflowID          string          `json:"workflow_id"`
	WorkflowLabel       string          `json:"workflow_label"`
	PresetQueryID       string          `json:"preset_query_id"` // empty — kept for frontend compat
	TriggerPayload      json.RawMessage `json:"trigger_payload,omitempty"`
	GroupIDs            []string        `json:"group_ids,omitempty"`
	Mode                string          `json:"mode,omitempty"`          // "workflow" or "workshop"
	Messages            []string        `json:"messages,omitempty"`      // Predefined messages for workshop mode
	WorkshopMode        string          `json:"workshop_mode,omitempty"` // builder, optimizer, runner (default), debugger
	CronExpression      string          `json:"cron_expression"`
	Timezone            string          `json:"timezone"`
	Enabled             bool            `json:"enabled"`
	LastRunAt           *time.Time      `json:"last_run_at,omitempty"`
	NextRunAt           *time.Time      `json:"next_run_at,omitempty"`
	LastSessionID       string          `json:"last_session_id,omitempty"`
	LastStatus          string          `json:"last_status,omitempty"`
	LastError           string          `json:"last_error,omitempty"`
	LastDurationMs      *int64          `json:"last_duration_ms,omitempty"`
	RunCount            int             `json:"run_count"`
	ConsecutiveFailures int             `json:"consecutive_failures"`
	CreatedAt           string          `json:"created_at,omitempty"`
	UpdatedAt           string          `json:"updated_at,omitempty"`
}

// CreateScheduleRequest is the request body for creating a schedule.
type CreateScheduleRequest struct {
	WorkspacePath  string          `json:"workspace_path"`
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	CronExpression string          `json:"cron_expression"`
	Timezone       string          `json:"timezone"`
	Enabled        bool            `json:"enabled"`
	TriggerPayload json.RawMessage `json:"trigger_payload,omitempty"`
	GroupIDs       []string        `json:"group_ids,omitempty"`
	Mode           string          `json:"mode,omitempty"`          // "workflow" (default) or "workshop"
	Messages       []string        `json:"messages,omitempty"`      // Predefined messages for workshop mode
	WorkshopMode   string          `json:"workshop_mode,omitempty"` // builder, optimizer, runner (default), debugger
}

// UpdateScheduleRequest is the request body for updating a schedule.
type UpdateScheduleRequest struct {
	Name           string          `json:"name,omitempty"`
	Description    string          `json:"description,omitempty"`
	CronExpression string          `json:"cron_expression,omitempty"`
	Timezone       string          `json:"timezone,omitempty"`
	Enabled        *bool           `json:"enabled,omitempty"`
	TriggerPayload json.RawMessage `json:"trigger_payload,omitempty"`
	GroupIDs       []string        `json:"group_ids,omitempty"`
	Mode           string          `json:"mode,omitempty"`          // "workflow" or "workshop"
	Messages       []string        `json:"messages,omitempty"`      // Predefined messages for workshop mode
	WorkshopMode   string          `json:"workshop_mode,omitempty"` // builder, optimizer, runner, debugger
}

// buildJobResponse combines manifest schedule + runtime state into an API response.
func buildJobResponse(workspacePath string, manifest *WorkflowManifest, sched WorkflowSchedule, state ScheduleRuntimeState) ScheduledJobResponse {
	return ScheduledJobResponse{
		ID:                  sched.ID,
		Name:                sched.Name,
		Description:         sched.Description,
		EntityType:          "workflow",
		WorkspacePath:       workspacePath,
		WorkflowID:          manifest.ID,
		WorkflowLabel:       manifest.Label,
		PresetQueryID:       "", // no longer used
		TriggerPayload:      sched.TriggerPayload,
		GroupIDs:            sched.GroupIDs,
		Mode:                sched.Mode,
		Messages:            sched.Messages,
		WorkshopMode:        sched.WorkshopMode,
		CronExpression:      sched.CronExpression,
		Timezone:            sched.Timezone,
		Enabled:             sched.Enabled,
		LastRunAt:           state.LastRunAt,
		NextRunAt:           state.NextRunAt,
		LastSessionID:       state.LastSessionID,
		LastStatus:          state.LastStatus,
		LastError:           state.LastError,
		LastDurationMs:      state.LastDurationMs,
		RunCount:            state.RunCount,
		ConsecutiveFailures: state.ConsecutiveFailures,
		CreatedAt:           manifest.CreatedAt,
		UpdatedAt:           manifest.UpdatedAt,
	}
}

// SchedulerRoutes registers the scheduler API routes.
func SchedulerRoutes(router *mux.Router, svc *SchedulerService) {
	apiRouter := router.PathPrefix("/api/scheduler").Subrouter()

	apiRouter.HandleFunc("/config", getSchedulerConfigHandler(svc)).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/config", updateSchedulerConfigHandler(svc)).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/jobs", listScheduledJobsHandler(svc)).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/jobs", createScheduledJobHandler(svc)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}", getScheduledJobHandler(svc)).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}", updateScheduledJobHandler(svc)).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}", deleteScheduledJobHandler(svc)).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}/enable", enableScheduledJobHandler(svc)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}/disable", disableScheduledJobHandler(svc)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}/trigger", triggerScheduledJobHandler(svc)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}/stop", stopScheduledJobHandler(svc)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}/runs", getScheduledJobRunsHandler(svc)).Methods("GET", "OPTIONS")
}

func listScheduledJobsHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		enabledFilter := r.URL.Query().Get("enabled")

		// Discover all workflows and collect schedules
		workflows, err := DiscoverWorkflowManifests(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var allJobs []ScheduledJobResponse
		for _, dw := range workflows {
			for _, sched := range dw.Manifest.Schedules {
				// Apply enabled filter
				if enabledFilter != "" {
					wantEnabled := enabledFilter == "true" || enabledFilter == "1"
					if sched.Enabled != wantEnabled {
						continue
					}
				}

				state := svc.GetRuntimeState(sched.ID)
				allJobs = append(allJobs, buildJobResponse(dw.WorkspacePath, dw.Manifest, sched, state))
			}
		}

		total := len(allJobs)

		// Pagination
		if offset >= total {
			allJobs = []ScheduledJobResponse{}
		} else {
			end := offset + limit
			if end > total {
				end = total
			}
			allJobs = allJobs[offset:end]
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jobs":   allJobs,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}

func createScheduledJobHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req CreateScheduleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.WorkspacePath == "" {
			http.Error(w, "workspace_path is required", http.StatusBadRequest)
			return
		}
		if err := ValidateCronExpression(req.CronExpression); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.Timezone != "" {
			if _, err := time.LoadLocation(req.Timezone); err != nil {
				http.Error(w, "invalid timezone", http.StatusBadRequest)
				return
			}
		}

		// Read manifest
		manifest, found, err := ReadWorkflowManifest(r.Context(), req.WorkspacePath)
		if err != nil || !found {
			http.Error(w, "workflow manifest not found at "+req.WorkspacePath, http.StatusBadRequest)
			return
		}

		// Create new schedule
		newSched := WorkflowSchedule{
			ID:             uuid.New().String(),
			Name:           req.Name,
			Description:    req.Description,
			CronExpression: req.CronExpression,
			Timezone:       req.Timezone,
			Enabled:        req.Enabled,
			TriggerPayload: req.TriggerPayload,
			GroupIDs:       req.GroupIDs,
			Mode:           req.Mode,
			Messages:       req.Messages,
			WorkshopMode:   req.WorkshopMode,
		}

		manifest.Schedules = append(manifest.Schedules, newSched)

		if err := WriteWorkflowManifest(r.Context(), req.WorkspacePath, manifest); err != nil {
			http.Error(w, "failed to write manifest: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Register in scheduler if enabled
		if newSched.Enabled {
			sctx := buildScheduleContext(req.WorkspacePath, manifest, newSched)
			if err := svc.LoadSchedule(sctx); err != nil {
				scheduleLogf("[SCHEDULER] Failed to load new schedule %s: %v", newSched.ID, err)
			}
		}

		state := svc.GetRuntimeState(newSched.ID)
		resp := buildJobResponse(req.WorkspacePath, manifest, newSched, state)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	}
}

func getScheduledJobHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]
		workspacePath, manifest, idx, err := findScheduleByID(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		state := svc.GetRuntimeState(id)
		resp := buildJobResponse(workspacePath, manifest, manifest.Schedules[idx], state)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func updateScheduledJobHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]

		var req UpdateScheduleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.CronExpression != "" {
			if err := ValidateCronExpression(req.CronExpression); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if req.Timezone != "" {
			if _, err := time.LoadLocation(req.Timezone); err != nil {
				http.Error(w, "invalid timezone", http.StatusBadRequest)
				return
			}
		}

		workspacePath, manifest, idx, err := findScheduleByID(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		sched := &manifest.Schedules[idx]
		if req.Name != "" {
			sched.Name = req.Name
		}
		if req.Description != "" {
			sched.Description = req.Description
		}
		if req.CronExpression != "" {
			sched.CronExpression = req.CronExpression
		}
		if req.Timezone != "" {
			sched.Timezone = req.Timezone
		}
		if req.Enabled != nil {
			sched.Enabled = *req.Enabled
		}
		if req.TriggerPayload != nil {
			sched.TriggerPayload = req.TriggerPayload
		}
		if req.GroupIDs != nil {
			sched.GroupIDs = req.GroupIDs
		}
		if req.Mode != "" {
			sched.Mode = req.Mode
		}
		if req.Messages != nil {
			sched.Messages = req.Messages
		}
		if req.WorkshopMode != "" {
			sched.WorkshopMode = req.WorkshopMode
		}

		if err := WriteWorkflowManifest(r.Context(), workspacePath, manifest); err != nil {
			http.Error(w, "failed to write manifest: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Reload in scheduler
		if err := svc.ReloadSchedule(r.Context(), workspacePath, id); err != nil {
			scheduleLogf("[SCHEDULER] Failed to reload schedule %s after update: %v", id, err)
		}

		state := svc.GetRuntimeState(id)
		resp := buildJobResponse(workspacePath, manifest, *sched, state)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func deleteScheduledJobHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]

		workspacePath, manifest, idx, err := findScheduleByID(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Remove from gocron
		_ = svc.RemoveJob(id)

		// Remove from manifest
		manifest.Schedules = append(manifest.Schedules[:idx], manifest.Schedules[idx+1:]...)

		if err := WriteWorkflowManifest(r.Context(), workspacePath, manifest); err != nil {
			http.Error(w, "failed to write manifest: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func enableScheduledJobHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]

		workspacePath, manifest, idx, err := findScheduleByID(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		manifest.Schedules[idx].Enabled = true

		if err := WriteWorkflowManifest(r.Context(), workspacePath, manifest); err != nil {
			http.Error(w, "failed to write manifest: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if err := svc.ReloadSchedule(r.Context(), workspacePath, id); err != nil {
			scheduleLogf("[SCHEDULER] Failed to reload schedule %s after enable: %v", id, err)
		}

		state := svc.GetRuntimeState(id)
		resp := buildJobResponse(workspacePath, manifest, manifest.Schedules[idx], state)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func disableScheduledJobHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]

		workspacePath, manifest, idx, err := findScheduleByID(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		manifest.Schedules[idx].Enabled = false

		if err := WriteWorkflowManifest(r.Context(), workspacePath, manifest); err != nil {
			http.Error(w, "failed to write manifest: "+err.Error(), http.StatusInternalServerError)
			return
		}

		_ = svc.RemoveJob(id)

		state := svc.GetRuntimeState(id)
		resp := buildJobResponse(workspacePath, manifest, manifest.Schedules[idx], state)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func triggerScheduledJobHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]

		// Find workspace path — first check in-memory index, then scan manifests
		workspacePath := svc.GetWorkspaceForSchedule(id)
		if workspacePath == "" {
			wp, _, _, err := findScheduleByID(r.Context(), id)
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			workspacePath = wp
		}

		result, err := svc.TriggerNow(workspacePath, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"session_id": result})
	}
}

func stopScheduledJobHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]

		state := svc.GetRuntimeState(id)
		if state.LastStatus != "running" {
			http.Error(w, "job is not running", http.StatusBadRequest)
			return
		}

		svc.StopRunningJob(id)

		// Update runtime state
		svc.runtimeStatesMu.Lock()
		s := svc.getRuntimeStateLocked(id)
		durationMs := int64(0)
		if s.LastRunAt != nil {
			durationMs = time.Since(*s.LastRunAt).Milliseconds()
		}
		s.LastStatus = "error"
		s.LastError = "stopped by user"
		s.LastDurationMs = &durationMs
		svc.runtimeStatesMu.Unlock()

		// Update latest run entry
		workspacePath := svc.GetWorkspaceForSchedule(id)
		if workspacePath != "" {
			runs, err := ReadScheduleRuns(r.Context(), workspacePath)
			if err == nil && len(runs) > 0 {
				for i := range runs {
					if runs[i].ScheduleID == id && runs[i].Status == "running" {
						_ = UpdateScheduleRun(r.Context(), workspacePath, runs[i].ID, "error", "stopped by user", &durationMs, "", "")
						break
					}
				}
			}
		}

		// Return updated state
		wp, manifest, idx, err := findScheduleByID(r.Context(), id)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
			return
		}

		updatedState := svc.GetRuntimeState(id)
		resp := buildJobResponse(wp, manifest, manifest.Schedules[idx], updatedState)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func getScheduledJobRunsHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]

		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}

		// Find workspace path
		workspacePath := svc.GetWorkspaceForSchedule(id)
		if workspacePath == "" {
			wp, _, _, err := findScheduleByID(r.Context(), id)
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			workspacePath = wp
		}

		runs, total, err := ListScheduleRuns(r.Context(), workspacePath, id, limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Map to response format compatible with frontend ScheduledJobRun
		type RunResponse struct {
			ID          string     `json:"id"`
			JobID       string     `json:"job_id"`
			RunFolder   string     `json:"run_folder,omitempty"`
			SessionID   string     `json:"session_id,omitempty"`
			Status      string     `json:"status"`
			Error       string     `json:"error,omitempty"`
			DurationMs  *int64     `json:"duration_ms,omitempty"`
			GroupIDs    []string   `json:"group_ids,omitempty"`
			StartedAt   time.Time  `json:"started_at"`
			CompletedAt *time.Time `json:"completed_at,omitempty"`
		}

		var respRuns []RunResponse
		for _, run := range runs {
			respRuns = append(respRuns, RunResponse{
				ID:          run.ID,
				JobID:       id,
				RunFolder:   run.RunFolder,
				SessionID:   run.SessionID,
				Status:      run.Status,
				Error:       run.Error,
				DurationMs:  run.DurationMs,
				GroupIDs:    run.GroupIDs,
				StartedAt:   run.StartedAt,
				CompletedAt: run.CompletedAt,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"runs":   respRuns,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}
