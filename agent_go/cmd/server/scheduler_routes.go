package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
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
	EntityType          string          `json:"entity_type"` // "workflow" or "multi-agent"
	WorkspacePath       string          `json:"workspace_path"`
	WorkflowID          string          `json:"workflow_id,omitempty"`
	WorkflowLabel       string          `json:"workflow_label,omitempty"`
	PresetQueryID       string          `json:"preset_query_id,omitempty"` // empty — kept for frontend compat
	TriggerPayload      json.RawMessage `json:"trigger_payload,omitempty"`
	GroupNames          []string        `json:"group_names,omitempty"`
	Mode                string          `json:"mode,omitempty"`          // "workflow", "workshop", or "multi-agent"
	Messages            []string        `json:"messages,omitempty"`      // Predefined messages for workshop mode
	WorkshopMode        string          `json:"workshop_mode,omitempty"` // builder, optimizer, runner (default), debugger
	Query               string          `json:"query,omitempty"`         // Message to execute (multi-agent mode)
	UserID              string          `json:"user_id,omitempty"`       // User context (multi-agent mode)
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
	MissedRunCount      int             `json:"missed_run_count,omitempty"`
	LatestMissedRunAt   *time.Time      `json:"latest_missed_run_at,omitempty"`
	CreatedAt           string          `json:"created_at,omitempty"`
	UpdatedAt           string          `json:"updated_at,omitempty"`
}

// CreateScheduleRequest is the request body for creating a schedule.
type CreateScheduleRequest struct {
	WorkspacePath  string          `json:"workspace_path"` // Required for workflow/workshop mode
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	CronExpression string          `json:"cron_expression"`
	Timezone       string          `json:"timezone"`
	Enabled        bool            `json:"enabled"`
	TriggerPayload json.RawMessage `json:"trigger_payload,omitempty"`
	GroupNames     []string        `json:"group_names,omitempty"`
	Mode           string          `json:"mode,omitempty"`          // "workflow" (default), "workshop", or "multi-agent"
	Messages       []string        `json:"messages,omitempty"`      // Predefined messages for workshop mode
	WorkshopMode   string          `json:"workshop_mode,omitempty"` // builder, optimizer, runner (default), debugger
	Query          string          `json:"query,omitempty"`         // Message to execute (multi-agent mode)
}

// UpdateScheduleRequest is the request body for updating a schedule.
type UpdateScheduleRequest struct {
	Name           string          `json:"name,omitempty"`
	Description    string          `json:"description,omitempty"`
	CronExpression string          `json:"cron_expression,omitempty"`
	Timezone       string          `json:"timezone,omitempty"`
	Enabled        *bool           `json:"enabled,omitempty"`
	TriggerPayload json.RawMessage `json:"trigger_payload,omitempty"`
	GroupNames     []string        `json:"group_names,omitempty"`
	Mode           string          `json:"mode,omitempty"`          // "workflow", "workshop", or "multi-agent"
	Messages       []string        `json:"messages,omitempty"`      // Predefined messages for workshop mode
	WorkshopMode   string          `json:"workshop_mode,omitempty"` // builder, optimizer, runner, debugger
	Query          string          `json:"query,omitempty"`         // Message to execute (multi-agent mode)
}

// buildJobResponse combines manifest schedule + runtime state into an API response.
func buildJobResponse(workspacePath string, manifest *WorkflowManifest, sched WorkflowSchedule, state ScheduleRuntimeState, missed WorkflowScheduleMissedStatus) ScheduledJobResponse {
	return ScheduledJobResponse{
		ID:                  sched.ID,
		Name:                sched.Name,
		Description:         sched.Description,
		EntityType:          "workflow",
		WorkspacePath:       workspacePath,
		WorkflowID:          manifest.ID,
		WorkflowLabel:       manifest.Label,
		PresetQueryID:       manifest.ID,
		TriggerPayload:      sched.TriggerPayload,
		GroupNames:          sched.GroupNames,
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
		MissedRunCount:      missed.MissedRunCount,
		LatestMissedRunAt:   missed.LatestMissedRunAt,
		CreatedAt:           manifest.CreatedAt,
		UpdatedAt:           manifest.UpdatedAt,
	}
}

// buildMultiAgentJobResponse combines a multi-agent schedule + runtime state into an API response.
func buildMultiAgentJobResponse(userID string, sched WorkflowSchedule, state ScheduleRuntimeState) ScheduledJobResponse {
	return ScheduledJobResponse{
		ID:                  sched.ID,
		Name:                sched.Name,
		Description:         sched.Description,
		EntityType:          "multi-agent",
		WorkspacePath:       "_users/" + userID,
		Mode:                "multi-agent",
		Query:               sched.Query,
		UserID:              userID,
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
	}
}

type workflowMissedStatusResolver struct {
	ctx     context.Context
	history map[string]*WorkflowScheduleExecutionHistoryFile
}

func newWorkflowMissedStatusResolver(ctx context.Context) *workflowMissedStatusResolver {
	return &workflowMissedStatusResolver{
		ctx:     ctx,
		history: make(map[string]*WorkflowScheduleExecutionHistoryFile),
	}
}

func (r *workflowMissedStatusResolver) get(workspacePath string, sched WorkflowSchedule) WorkflowScheduleMissedStatus {
	if !sched.Enabled {
		return WorkflowScheduleMissedStatus{}
	}

	history, ok := r.history[workspacePath]
	if !ok {
		loaded, err := ReadWorkflowScheduleExecutionHistory(r.ctx, workspacePath)
		if err != nil {
			loaded = &WorkflowScheduleExecutionHistoryFile{
				Version:   workflowScheduleExecutionHistoryVersion,
				Schedules: map[string]WorkflowScheduleExecutionTrack{},
			}
		}
		history = loaded
		r.history[workspacePath] = history
	}
	if history == nil || history.Schedules == nil {
		return WorkflowScheduleMissedStatus{}
	}

	tracker, ok := history.Schedules[sched.ID]
	if !ok {
		return WorkflowScheduleMissedStatus{}
	}
	return ComputeWorkflowScheduleMissedStatus(sched, &tracker, time.Now().UTC())
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

		modeFilter := r.URL.Query().Get("mode") // "workflow", "multi-agent", or "" for all

		// Discover all workflows and collect schedules
		workflows, err := DiscoverWorkflowManifests(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		var allJobs []ScheduledJobResponse
		missedResolver := newWorkflowMissedStatusResolver(r.Context())

		if modeFilter == "" || modeFilter == "workflow" || modeFilter == "workshop" {
			for _, dw := range workflows {
				for _, sched := range dw.Manifest.Schedules {
					if enabledFilter != "" {
						wantEnabled := enabledFilter == "true" || enabledFilter == "1"
						if sched.Enabled != wantEnabled {
							continue
						}
					}
					if modeFilter != "" && sched.Mode != modeFilter {
						continue
					}

					state := svc.GetRuntimeState(sched.ID)
					missed := missedResolver.get(dw.WorkspacePath, sched)
					allJobs = append(allJobs, buildJobResponse(dw.WorkspacePath, dw.Manifest, sched, state, missed))
				}
			}
		}

		// Discover multi-agent schedules — filtered by current user
		if modeFilter == "" || modeFilter == "multi-agent" {
			currentUserID := GetUserIDFromContext(r.Context())
			userIDFilter := r.URL.Query().Get("user_id")
			if userIDFilter == "" {
				userIDFilter = currentUserID
			}

			// If a specific user is requested, read just their file; otherwise scan all
			if userIDFilter != "" {
				f, exists, fErr := ReadMultiAgentSchedules(r.Context(), userIDFilter)
				if fErr == nil && exists {
					for _, sched := range f.Schedules {
						if enabledFilter != "" {
							wantEnabled := enabledFilter == "true" || enabledFilter == "1"
							if sched.Enabled != wantEnabled {
								continue
							}
						}
						state := svc.GetRuntimeState(sched.ID)
						allJobs = append(allJobs, buildMultiAgentJobResponse(userIDFilter, sched, state))
					}
				}
			} else {
				maScheds, maErr := DiscoverMultiAgentSchedules(r.Context())
				if maErr == nil {
					for _, ma := range maScheds {
						for _, sched := range ma.ScheduleFile.Schedules {
							if enabledFilter != "" {
								wantEnabled := enabledFilter == "true" || enabledFilter == "1"
								if sched.Enabled != wantEnabled {
									continue
								}
							}
							state := svc.GetRuntimeState(sched.ID)
							allJobs = append(allJobs, buildMultiAgentJobResponse(ma.UserID, sched, state))
						}
					}
				}
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

		// Multi-agent schedule creation
		if req.Mode == "multi-agent" {
			userID := GetUserIDFromContext(r.Context())
			if strings.TrimSpace(req.Query) == "" {
				http.Error(w, "query is required for multi-agent schedules", http.StatusBadRequest)
				return
			}

			f, _, err := ReadMultiAgentSchedules(r.Context(), userID)
			if err != nil {
				http.Error(w, "failed to read multi-agent schedules: "+err.Error(), http.StatusInternalServerError)
				return
			}

			newSched := WorkflowSchedule{
				ID:             uuid.New().String(),
				Name:           req.Name,
				Description:    req.Description,
				CronExpression: req.CronExpression,
				Timezone:       req.Timezone,
				Enabled:        req.Enabled,
				Mode:           "multi-agent",
				Query:          req.Query,
			}

			f.Schedules = append(f.Schedules, newSched)

			if err := WriteMultiAgentSchedules(r.Context(), userID, f); err != nil {
				http.Error(w, "failed to write multi-agent schedules: "+err.Error(), http.StatusInternalServerError)
				return
			}

			if newSched.Enabled {
				sctx := buildMultiAgentScheduleContext(userID, newSched, f.Capabilities)
				if err := svc.LoadSchedule(sctx); err != nil {
					scheduleLogf("[SCHEDULER] Failed to load new multi-agent schedule %s: %v", newSched.ID, err)
				}
			}

			state := svc.GetRuntimeState(newSched.ID)
			resp := buildMultiAgentJobResponse(userID, newSched, state)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Workflow/workshop schedule creation
		if req.WorkspacePath == "" {
			http.Error(w, "workspace_path is required", http.StatusBadRequest)
			return
		}

		// Read manifest
		manifest, found, err := ReadWorkflowManifest(r.Context(), req.WorkspacePath)
		if err != nil || !found {
			http.Error(w, "workflow manifest not found at "+req.WorkspacePath, http.StatusBadRequest)
			return
		}
		req.GroupNames, err = validateScheduleGroupNamesForWorkspace(r.Context(), req.WorkspacePath, req.GroupNames)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
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
			GroupNames:     req.GroupNames,
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
		missedResolver := newWorkflowMissedStatusResolver(r.Context())
		resp := buildJobResponse(req.WorkspacePath, manifest, newSched, state, missedResolver.get(req.WorkspacePath, newSched))

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
		result, err := findScheduleByIDAny(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		state := svc.GetRuntimeState(id)
		var resp ScheduledJobResponse
		if result.SourceType == "multi-agent" {
			resp = buildMultiAgentJobResponse(result.UserID, result.ScheduleFile.Schedules[result.Index], state)
		} else {
			missedResolver := newWorkflowMissedStatusResolver(r.Context())
			sched := result.Manifest.Schedules[result.Index]
			resp = buildJobResponse(result.WorkspacePath, result.Manifest, sched, state, missedResolver.get(result.WorkspacePath, sched))
		}

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

		result, err := findScheduleByIDAny(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if result.SourceType == "multi-agent" {
			sched := &result.ScheduleFile.Schedules[result.Index]
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
			if req.Query != "" {
				sched.Query = req.Query
			}

			if err := WriteMultiAgentSchedules(r.Context(), result.UserID, result.ScheduleFile); err != nil {
				http.Error(w, "failed to write multi-agent schedules: "+err.Error(), http.StatusInternalServerError)
				return
			}

			if err := svc.ReloadMultiAgentSchedule(r.Context(), result.UserID, id); err != nil {
				scheduleLogf("[SCHEDULER] Failed to reload multi-agent schedule %s after update: %v", id, err)
			}

			state := svc.GetRuntimeState(id)
			resp := buildMultiAgentJobResponse(result.UserID, *sched, state)

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}

		// Workflow schedule update
		workspacePath := result.WorkspacePath
		manifest := result.Manifest
		idx := result.Index

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
		if req.GroupNames != nil {
			validGroupNames, err := validateScheduleGroupNamesForWorkspace(r.Context(), workspacePath, req.GroupNames)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			sched.GroupNames = validGroupNames
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
		validGroupNames, err := validateScheduleGroupNamesForWorkspace(r.Context(), workspacePath, sched.GroupNames)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sched.GroupNames = validGroupNames

		if err := WriteWorkflowManifest(r.Context(), workspacePath, manifest); err != nil {
			http.Error(w, "failed to write manifest: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if err := svc.ReloadSchedule(r.Context(), workspacePath, id); err != nil {
			scheduleLogf("[SCHEDULER] Failed to reload schedule %s after update: %v", id, err)
		}

		state := svc.GetRuntimeState(id)
		missedResolver := newWorkflowMissedStatusResolver(r.Context())
		resp := buildJobResponse(workspacePath, manifest, *sched, state, missedResolver.get(workspacePath, *sched))

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

		result, err := findScheduleByIDAny(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Remove from gocron
		_ = svc.RemoveJob(id)

		if result.SourceType == "multi-agent" {
			result.ScheduleFile.Schedules = append(result.ScheduleFile.Schedules[:result.Index], result.ScheduleFile.Schedules[result.Index+1:]...)
			if err := WriteMultiAgentSchedules(r.Context(), result.UserID, result.ScheduleFile); err != nil {
				http.Error(w, "failed to write multi-agent schedules: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			manifest := result.Manifest
			manifest.Schedules = append(manifest.Schedules[:result.Index], manifest.Schedules[result.Index+1:]...)
			if err := WriteWorkflowManifest(r.Context(), result.WorkspacePath, manifest); err != nil {
				http.Error(w, "failed to write manifest: "+err.Error(), http.StatusInternalServerError)
				return
			}
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

		result, err := findScheduleByIDAny(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		var resp ScheduledJobResponse

		if result.SourceType == "multi-agent" {
			result.ScheduleFile.Schedules[result.Index].Enabled = true
			if err := WriteMultiAgentSchedules(r.Context(), result.UserID, result.ScheduleFile); err != nil {
				http.Error(w, "failed to write multi-agent schedules: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if err := svc.ReloadMultiAgentSchedule(r.Context(), result.UserID, id); err != nil {
				scheduleLogf("[SCHEDULER] Failed to reload multi-agent schedule %s after enable: %v", id, err)
			}
			state := svc.GetRuntimeState(id)
			resp = buildMultiAgentJobResponse(result.UserID, result.ScheduleFile.Schedules[result.Index], state)
		} else {
			result.Manifest.Schedules[result.Index].Enabled = true
			if err := WriteWorkflowManifest(r.Context(), result.WorkspacePath, result.Manifest); err != nil {
				http.Error(w, "failed to write manifest: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if err := svc.ReloadSchedule(r.Context(), result.WorkspacePath, id); err != nil {
				scheduleLogf("[SCHEDULER] Failed to reload schedule %s after enable: %v", id, err)
			}
			state := svc.GetRuntimeState(id)
			missedResolver := newWorkflowMissedStatusResolver(r.Context())
			sched := result.Manifest.Schedules[result.Index]
			resp = buildJobResponse(result.WorkspacePath, result.Manifest, sched, state, missedResolver.get(result.WorkspacePath, sched))
		}

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

		result, err := findScheduleByIDAny(r.Context(), id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		_ = svc.RemoveJob(id)

		state := svc.GetRuntimeState(id)
		var resp ScheduledJobResponse

		if result.SourceType == "multi-agent" {
			result.ScheduleFile.Schedules[result.Index].Enabled = false
			if err := WriteMultiAgentSchedules(r.Context(), result.UserID, result.ScheduleFile); err != nil {
				http.Error(w, "failed to write multi-agent schedules: "+err.Error(), http.StatusInternalServerError)
				return
			}
			resp = buildMultiAgentJobResponse(result.UserID, result.ScheduleFile.Schedules[result.Index], state)
		} else {
			result.Manifest.Schedules[result.Index].Enabled = false
			if err := WriteWorkflowManifest(r.Context(), result.WorkspacePath, result.Manifest); err != nil {
				http.Error(w, "failed to write manifest: "+err.Error(), http.StatusInternalServerError)
				return
			}
			missedResolver := newWorkflowMissedStatusResolver(r.Context())
			sched := result.Manifest.Schedules[result.Index]
			resp = buildJobResponse(result.WorkspacePath, result.Manifest, sched, state, missedResolver.get(result.WorkspacePath, sched))
		}

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

		// Check if it's a multi-agent schedule first
		userID := svc.GetUserForSchedule(id)
		if userID != "" {
			result, err := svc.TriggerMultiAgentNow(userID, id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"session_id": result})
			return
		}

		// Find workspace path — first check in-memory index, then scan manifests
		workspacePath := svc.GetWorkspaceForSchedule(id)
		if workspacePath == "" {
			// Try to find it — could be workflow or unloaded multi-agent
			result, err := findScheduleByIDAny(r.Context(), id)
			if err != nil {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			if result.SourceType == "multi-agent" {
				trigResult, err := svc.TriggerMultiAgentNow(result.UserID, id)
				if err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]string{"session_id": trigResult})
				return
			}
			workspacePath = result.WorkspacePath
		}

		trigResult, err := svc.TriggerNow(workspacePath, id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"session_id": trigResult})
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

		// Update latest run entry — check multi-agent first, then workflow
		userID := svc.GetUserForSchedule(id)
		if userID != "" {
			runs, err := ReadMultiAgentScheduleRuns(r.Context(), userID)
			if err == nil {
				for i := range runs {
					if runs[i].ScheduleID == id && runs[i].Status == "running" {
						_ = UpdateMultiAgentScheduleRun(r.Context(), userID, runs[i].ID, "error", "stopped by user", &durationMs, "")
						break
					}
				}
			}
		} else {
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
		}

		// Return updated state
		result, err := findScheduleByIDAny(r.Context(), id)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{"status": "stopped"})
			return
		}

		updatedState := svc.GetRuntimeState(id)
		var resp ScheduledJobResponse
		if result.SourceType == "multi-agent" {
			resp = buildMultiAgentJobResponse(result.UserID, result.ScheduleFile.Schedules[result.Index], updatedState)
		} else {
			missedResolver := newWorkflowMissedStatusResolver(r.Context())
			sched := result.Manifest.Schedules[result.Index]
			resp = buildJobResponse(result.WorkspacePath, result.Manifest, sched, updatedState, missedResolver.get(result.WorkspacePath, sched))
		}

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

		var runs []ScheduleRunEntry
		var total int
		var err error

		// Check if it's a multi-agent schedule
		userID := svc.GetUserForSchedule(id)
		if userID != "" {
			runs, total, err = ListMultiAgentScheduleRuns(r.Context(), userID, id, limit, offset)
		} else {
			// Find workspace path for workflow schedule
			workspacePath := svc.GetWorkspaceForSchedule(id)
			if workspacePath == "" {
				result, findErr := findScheduleByIDAny(r.Context(), id)
				if findErr != nil {
					http.Error(w, "not found", http.StatusNotFound)
					return
				}
				if result.SourceType == "multi-agent" {
					runs, total, err = ListMultiAgentScheduleRuns(r.Context(), result.UserID, id, limit, offset)
				} else {
					workspacePath = result.WorkspacePath
					runs, total, err = ListScheduleRuns(r.Context(), workspacePath, id, limit, offset)
				}
			} else {
				runs, total, err = ListScheduleRuns(r.Context(), workspacePath, id, limit, offset)
			}
		}

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
			GroupNames  []string   `json:"group_names,omitempty"`
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
				GroupNames:  run.GroupNames,
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
