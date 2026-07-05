package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestListScheduledJobsIncludesDefaultBuiltinsWithoutScheduleFile(t *testing.T) {
	api := &mockWorkspaceAPI{files: map[string]string{}}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/scheduler/jobs?mode=multi-agent", nil)
	rec := httptest.NewRecorder()
	listScheduledJobsHandler(NewSchedulerService(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Jobs []ScheduledJobResponse `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v\n%s", err, rec.Body.String())
	}

	var foundOrgPulse bool
	for _, job := range resp.Jobs {
		if job.ID == deprecatedAutoEnrichMemoryID {
			t.Fatalf("deprecated auto-enrich memory should not be listed: %+v", resp.Jobs)
		}
		if job.ID == builtinOrgPulseID {
			foundOrgPulse = true
			if job.Enabled {
				t.Fatal("builtin org pulse should be listed as disabled by default")
			}
			if job.EntityType != "multi-agent" || job.UserID != GetDefaultUserID() {
				t.Fatalf("unexpected org pulse job metadata: %+v", job)
			}
			if !job.BuiltIn || job.ManagedBy != "slash-command" {
				t.Fatalf("unexpected org pulse management metadata: %+v", job)
			}
		}
	}
	if !foundOrgPulse {
		t.Fatalf("builtin org pulse was not listed: %+v", resp.Jobs)
	}
}

func TestListScheduledJobsReflectsOrgPulseEnabledViaDuplicate(t *testing.T) {
	// /pulse-setup can leave Org Pulse enabled under a user-created duplicate id
	// (via create_multiagent_schedule) instead of a same-id builtin override.
	// The canonical builtin-org-pulse job must still report Enabled=true so the
	// Org Pulse pill matches what the scheduler actually runs.
	scheduleFile := `{
  "schedules": [
    {
      "id": "c8261d3e-f999-4260-a58b-98ff28aa5d1e",
      "name": "Daily Org Pulse",
      "description": "Daily sweep of all workflows.",
      "schedule_type": "cron",
      "cron_expression": "0 9 * * *",
      "timezone": "Asia/Kolkata",
      "enabled": true,
      "mode": "multi-agent",
      "query": "Perform the Daily Org Pulse sweep."
    }
  ]
}`
	api := &mockWorkspaceAPI{files: map[string]string{
		multiAgentSchedulesPath(GetDefaultUserID()): scheduleFile,
	}}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	req := httptest.NewRequest(http.MethodGet, "/api/scheduler/jobs?mode=multi-agent", nil)
	rec := httptest.NewRecorder()
	listScheduledJobsHandler(NewSchedulerService(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Jobs []ScheduledJobResponse `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v\n%s", err, rec.Body.String())
	}

	var found bool
	for _, job := range resp.Jobs {
		if job.ID == builtinOrgPulseID {
			found = true
			if !job.Enabled {
				t.Fatal("builtin-org-pulse should report Enabled=true when an enabled Org Pulse duplicate exists")
			}
		}
	}
	if !found {
		t.Fatalf("builtin-org-pulse was not listed: %+v", resp.Jobs)
	}
}

func TestEnableBuiltinOrgPulseRequiresPersistedOverride(t *testing.T) {
	api := &mockWorkspaceAPI{files: map[string]string{}}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/scheduler/jobs/"+builtinOrgPulseID+"/enable", nil)
	req = mux.SetURLVars(req, map[string]string{"id": builtinOrgPulseID})
	rec := httptest.NewRecorder()

	enableScheduledJobHandler(NewSchedulerService(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s; want 404 for slash-managed built-in", rec.Code, rec.Body.String())
	}

	api.mu.Lock()
	written := api.files[multiAgentSchedulesPath(GetDefaultUserID())]
	api.mu.Unlock()
	if written != "" {
		t.Fatalf("generic enable should not materialize slash-managed org pulse; wrote:\n%s", written)
	}
}

func TestDeprecatedAutoEnrichScheduleIsFiltered(t *testing.T) {
	schedules := MergeBuiltinSchedules([]WorkflowSchedule{
		{
			ID:             deprecatedAutoEnrichMemoryID,
			Name:           "Auto-enrich memory",
			Enabled:        true,
			Mode:           "multi-agent",
			CronExpression: "0 */3 * * *",
			Query:          "Run enrich_memory.",
		},
	})
	for _, sched := range schedules {
		if sched.ID == deprecatedAutoEnrichMemoryID {
			t.Fatalf("deprecated auto-enrich memory schedule should be filtered: %+v", schedules)
		}
	}
}
