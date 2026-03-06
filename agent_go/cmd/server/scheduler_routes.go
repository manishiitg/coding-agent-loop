package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/database"

	"github.com/gorilla/mux"
)

// SchedulerRoutes registers the scheduler API routes
func SchedulerRoutes(router *mux.Router, db database.Database, svc *SchedulerService) {
	apiRouter := router.PathPrefix("/api/scheduler").Subrouter()

	apiRouter.HandleFunc("/jobs", listScheduledJobsHandler(db)).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/jobs", createScheduledJobHandler(db, svc)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}", getScheduledJobHandler(db)).Methods("GET", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}", updateScheduledJobHandler(db, svc)).Methods("PUT", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}", deleteScheduledJobHandler(db, svc)).Methods("DELETE", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}/enable", enableScheduledJobHandler(db, svc)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}/disable", disableScheduledJobHandler(db, svc)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}/trigger", triggerScheduledJobHandler(svc)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}/stop", stopScheduledJobHandler(db, svc)).Methods("POST", "OPTIONS")
	apiRouter.HandleFunc("/jobs/{id}/runs", getScheduledJobRunsHandler(db)).Methods("GET", "OPTIONS")
}

func listScheduledJobsHandler(db database.Database) http.HandlerFunc {
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

		var entityType *string
		if v := r.URL.Query().Get("entity_type"); v != "" {
			entityType = &v
		}

		var enabled *bool
		if v := r.URL.Query().Get("enabled"); v != "" {
			b := v == "true" || v == "1"
			enabled = &b
		}

		jobs, total, err := db.ListScheduledJobs(r.Context(), limit, offset, entityType, enabled)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(database.ListScheduledJobsResponse{
			Jobs:   jobs,
			Total:  total,
			Limit:  limit,
			Offset: offset,
		})
	}
}

func createScheduledJobHandler(db database.Database, svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		var req database.CreateScheduledJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validate entity type
		if req.EntityType != database.ScheduleEntityWorkflow && req.EntityType != database.ScheduleEntityChat {
			http.Error(w, "entity_type must be 'workflow' or 'chat'", http.StatusBadRequest)
			return
		}

		// Validate preset exists
		preset, err := db.GetPresetQuery(r.Context(), req.PresetQueryID)
		if err != nil || preset == nil {
			http.Error(w, "preset_query_id not found", http.StatusBadRequest)
			return
		}

		// Validate cron expression
		if err := ValidateCronExpression(req.CronExpression); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validate timezone
		if req.Timezone != "" {
			if _, err := time.LoadLocation(req.Timezone); err != nil {
				http.Error(w, "invalid timezone", http.StatusBadRequest)
				return
			}
		}

		job, err := db.CreateScheduledJob(r.Context(), &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Register in scheduler if enabled
		if job.Enabled && svc != nil {
			if err := svc.LoadJob(job); err != nil {
				// Log but don't fail the request
				_ = err
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(job)
	}
}

func getScheduledJobHandler(db database.Database) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]
		job, err := db.GetScheduledJob(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if job == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
	}
}

func updateScheduledJobHandler(db database.Database, svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]

		var req database.UpdateScheduledJobRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Validate cron if provided
		if req.CronExpression != "" {
			if err := ValidateCronExpression(req.CronExpression); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}

		// Validate timezone if provided
		if req.Timezone != "" {
			if _, err := time.LoadLocation(req.Timezone); err != nil {
				http.Error(w, "invalid timezone", http.StatusBadRequest)
				return
			}
		}

		job, err := db.UpdateScheduledJob(r.Context(), id, &req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if job == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Reload in scheduler (handles enable/disable/cron change)
		if svc != nil {
			if err := svc.LoadJob(job); err != nil {
				_ = err
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
	}
}

func deleteScheduledJobHandler(db database.Database, svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]

		// Remove from scheduler first
		if svc != nil {
			if err := svc.RemoveJob(id); err != nil {
				_ = err
			}
		}

		if err := db.DeleteScheduledJob(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func enableScheduledJobHandler(db database.Database, svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]
		enabled := true
		job, err := db.UpdateScheduledJob(r.Context(), id, &database.UpdateScheduledJobRequest{Enabled: &enabled})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if job == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if svc != nil {
			if err := svc.LoadJob(job); err != nil {
				_ = err
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
	}
}

func disableScheduledJobHandler(db database.Database, svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]
		enabled := false
		job, err := db.UpdateScheduledJob(r.Context(), id, &database.UpdateScheduledJobRequest{Enabled: &enabled})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if job == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if svc != nil {
			if err := svc.RemoveJob(id); err != nil {
				_ = err
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(job)
	}
}

func triggerScheduledJobHandler(svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		if svc == nil {
			http.Error(w, "scheduler not available", http.StatusServiceUnavailable)
			return
		}

		id := mux.Vars(r)["id"]
		sessionID, err := svc.TriggerNow(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"session_id": sessionID})
	}
}

func stopScheduledJobHandler(db database.Database, svc *SchedulerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		id := mux.Vars(r)["id"]
		job, err := db.GetScheduledJob(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if job == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if job.LastStatus != "running" {
			http.Error(w, "job is not running", http.StatusBadRequest)
			return
		}

		// Stop the running session via the scheduler service
		if svc != nil {
			svc.StopRunningJob(job)
		}

		// Update job status to error/stopped
		durationMs := int64(0)
		if job.LastRunAt != nil {
			durationMs = time.Since(*job.LastRunAt).Milliseconds()
		}
		if err := db.UpdateScheduledJobRunStatus(r.Context(), id, func() time.Time {
			if job.LastRunAt != nil {
				return *job.LastRunAt
			}
			return time.Now()
		}(), job.NextRunAt, job.LastSessionID, "error", "stopped by user", &durationMs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Also update the latest run history entry — detect iteration folder if missing
		runs, _, _ := db.ListScheduledJobRuns(r.Context(), id, 1, 0)
		if len(runs) > 0 && runs[0].Status == "running" {
			runFolder := runs[0].RunFolder
			if runFolder == "" && svc != nil {
				// Detect the iteration folder by looking at workspace
				workspacePath := svc.getJobWorkspacePath(r.Context(), job)
				folders := svc.listIterationFolders(workspacePath)
				if len(folders) > 0 {
					runFolder = folders[len(folders)-1] // latest iteration
				}
			}
			_ = db.UpdateScheduledJobRun(r.Context(), runs[0].ID, "error", "stopped by user", &durationMs, runFolder, runs[0].SessionID)
		}

		// Re-fetch updated job
		updatedJob, _ := db.GetScheduledJob(r.Context(), id)
		if updatedJob == nil {
			updatedJob = job
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(updatedJob)
	}
}

func getScheduledJobRunsHandler(db database.Database) http.HandlerFunc {
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

		runs, total, err := db.ListScheduledJobRuns(r.Context(), id, limit, offset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"runs":   runs,
			"total":  total,
			"limit":  limit,
			"offset": offset,
		})
	}
}
