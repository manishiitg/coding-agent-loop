package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
			if job.Enabled {
				t.Fatal("builtin auto-enrich memory should be listed as disabled by default")
			}
			if !job.BuiltIn || job.ManagedBy != "slash-command" {
				t.Fatalf("unexpected auto-enrich metadata: %+v", job)
			}
			if strings.Contains(job.Query, "_users/default") {
				t.Fatalf("auto-enrich query should not hardcode default user paths: %s", job.Query)
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

func TestDeleteBuiltinAutoEnrichRequiresMemorySetup(t *testing.T) {
	api := &mockWorkspaceAPI{files: map[string]string{}}
	server := httptest.NewServer(api)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	req := httptest.NewRequest(http.MethodDelete, "/api/scheduler/jobs/"+builtinAutoEnrichMemoryID, nil)
	req = mux.SetURLVars(req, map[string]string{"id": builtinAutoEnrichMemoryID})
	rec := httptest.NewRecorder()

	deleteScheduledJobHandler(NewSchedulerService(nil)).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s; want 404 for slash-managed built-in", rec.Code, rec.Body.String())
	}

	api.mu.Lock()
	written := api.files[multiAgentSchedulesPath(GetDefaultUserID())]
	api.mu.Unlock()
	if written != "" {
		t.Fatalf("generic delete should not materialize slash-managed memory schedule; wrote:\n%s", written)
	}
}

func TestHasChatsNeedingEnrichmentSkipsScheduleDateBucketSessions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)

	chatDir := filepath.Join(root, "_users", GetDefaultUserID(), "chat_history", "2026-07-01")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatalf("mkdir chat dir: %v", err)
	}

	writeConversation := func(name string, modTime time.Time) string {
		t.Helper()
		path := filepath.Join(chatDir, name)
		data := []byte(`{"session_id":"x","conversation_history":[{"Role":"human","Parts":[{"Text":"remember preference"}]}]}`)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("chtimes %s: %v", name, err)
		}
		return path
	}

	now := time.Now()
	writeConversation("session-schedule-cron--abc123_100-conversation.json", now)
	writeConversation("session-sched_legacy123_100-conversation.json", now)
	if hasChatsNeedingEnrichment(GetDefaultUserID()) {
		t.Fatal("scheduled conversations should not wake memory enrichment")
	}

	normalConv := writeConversation("session-chat-1-conversation.json", now)
	if !hasChatsNeedingEnrichment(GetDefaultUserID()) {
		t.Fatal("normal date-bucket conversation should wake memory enrichment")
	}

	marker := normalConv + ".enriched"
	if err := os.WriteFile(marker, []byte{}, 0o600); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	later := now.Add(time.Minute)
	if err := os.Chtimes(marker, later, later); err != nil {
		t.Fatalf("chtimes marker: %v", err)
	}
	if hasChatsNeedingEnrichment(GetDefaultUserID()) {
		t.Fatal("fresh enrichment marker should suppress memory enrichment")
	}
}
