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
	var foundAutoEnrich bool
	for _, job := range resp.Jobs {
		if job.ID == builtinAutoEnrichMemoryID {
			foundAutoEnrich = true
			if !job.Enabled {
				t.Fatal("builtin auto-enrich memory should be listed as enabled by default")
			}
			if !job.BuiltIn || job.ManagedBy != "built-in" {
				t.Fatalf("unexpected auto-enrich metadata: %+v", job)
			}
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
	if !foundAutoEnrich {
		t.Fatalf("builtin auto-enrich memory was not listed: %+v", resp.Jobs)
	}
	if !foundOrgPulse {
		t.Fatalf("builtin org pulse was not listed: %+v", resp.Jobs)
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

func TestDeleteBuiltinAutoEnrichWritesDisabledOverride(t *testing.T) {
	api := &mockWorkspaceAPI{files: map[string]string{}}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	req := httptest.NewRequest(http.MethodDelete, "/api/scheduler/jobs/"+builtinAutoEnrichMemoryID, nil)
	req = mux.SetURLVars(req, map[string]string{"id": builtinAutoEnrichMemoryID})
	rec := httptest.NewRecorder()

	deleteScheduledJobHandler(NewSchedulerService(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s; want 204", rec.Code, rec.Body.String())
	}

	api.mu.Lock()
	written := api.files[multiAgentSchedulesPath(GetDefaultUserID())]
	api.mu.Unlock()
	if written == "" {
		t.Fatal("delete should write a disabled override for the built-in auto-enrich schedule")
	}

	var f MultiAgentScheduleFile
	if err := json.Unmarshal([]byte(written), &f); err != nil {
		t.Fatalf("failed to decode written schedule file: %v\n%s", err, written)
	}
	if len(f.Schedules) != 1 {
		t.Fatalf("expected one override schedule, got %d: %+v", len(f.Schedules), f.Schedules)
	}
	if f.Schedules[0].ID != builtinAutoEnrichMemoryID {
		t.Fatalf("override id = %q, want %q", f.Schedules[0].ID, builtinAutoEnrichMemoryID)
	}
	if f.Schedules[0].Enabled {
		t.Fatal("delete should disable the built-in auto-enrich schedule")
	}
}
