package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRecentSuccessfulRunAveragesUsesNewestCompletedSuccesses(t *testing.T) {
	now := time.Now()
	ms := func(value int64) *int64 { return &value }
	runs := []ScheduleRunEntry{
		{ScheduleID: "daily", Status: "success", DurationMs: ms(1000), StartedAt: now.Add(-3 * time.Hour)},
		{ScheduleID: "daily", Status: "error", DurationMs: ms(9000), StartedAt: now.Add(-2 * time.Hour)},
		{ScheduleID: "daily", Status: "success", DurationMs: ms(3000), StartedAt: now.Add(-time.Hour)},
		{ScheduleID: "daily", Status: "running", StartedAt: now},
		{ScheduleID: "daily", TriggerSource: "manual", Status: "success", DurationMs: ms(12000), StartedAt: now},
		{ScheduleID: "other", Status: "success", DurationMs: ms(5000), StartedAt: now},
	}

	averages := recentSuccessfulRunAverages(runs, 2)
	if got := averages["daily"]; got.DurationMs != 2000 || got.Samples != 2 {
		t.Fatalf("daily average = %+v, want 2000ms from two successes", got)
	}
	if got := averages["other"]; got.DurationMs != 5000 || got.Samples != 1 {
		t.Fatalf("other average = %+v, want 5000ms from one success", got)
	}
	if got := recentSuccessfulRunAverages(runs, 1)["daily"]; got.DurationMs != 3000 || got.Samples != 1 {
		t.Fatalf("newest daily average = %+v, want the latest success only", got)
	}
}

type scheduleRunWorkspaceStub struct {
	mu    sync.Mutex
	files map[string]string
}

func newScheduleRunWorkspaceStub(t *testing.T) (*scheduleRunWorkspaceStub, *httptest.Server) {
	t.Helper()
	stub := &scheduleRunWorkspaceStub{files: make(map[string]string)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/documents/")
		stub.mu.Lock()
		defer stub.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			content, ok := stub.files[path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data":    map[string]any{"content": content},
			})
		case http.MethodPut:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var payload struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			stub.files[path] = payload.Content
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv("WORKSPACE_API_URL", server.URL)
	return stub, server
}

func TestAppendScheduleRunPreservesConcurrentEntries(t *testing.T) {
	_, _ = newScheduleRunWorkspaceStub(t)
	const count = 40

	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			run := &ScheduleRunEntry{
				ID:         string(rune('A' + index)),
				ScheduleID: "schedule-1",
				Status:     "running",
				StartedAt:  time.Now().UTC(),
			}
			if err := AppendScheduleRun(context.Background(), "Workflow/test", run); err != nil {
				t.Errorf("AppendScheduleRun(%d): %v", index, err)
			}
		}(i)
	}
	wg.Wait()

	runs, err := ReadScheduleRuns(context.Background(), "Workflow/test")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != count {
		t.Fatalf("len(runs) = %d, want %d", len(runs), count)
	}
}

func TestWorkflowScheduleRunHistoryPagesBeyondFormerLimit(t *testing.T) {
	_, _ = newScheduleRunWorkspaceStub(t)
	ctx := context.Background()
	const totalRuns = maxScheduleRuns + 5
	for index := 0; index < totalRuns; index++ {
		run := &ScheduleRunEntry{
			ID: fmt.Sprintf("run-%03d", index), ScheduleID: "deploy-hook", Status: "success",
			StartedAt: time.Now().UTC().Add(-time.Duration(totalRuns-index) * time.Minute),
		}
		if err := AppendScheduleRun(ctx, "Workflow/test", run); err != nil {
			t.Fatal(err)
		}
	}
	page, count, err := ListScheduleRuns(ctx, "Workflow/test", "deploy-hook", 10, maxScheduleRuns)
	if err != nil || count != totalRuns || len(page) != 5 || page[0].ID != "run-004" {
		t.Fatalf("older page = %+v, total=%d, err=%v", page, count, err)
	}
}

func TestWorkflowScheduleRunHistoryPrunesOldTerminalRuns(t *testing.T) {
	stub, _ := newScheduleRunWorkspaceStub(t)
	ctx := context.Background()
	old := time.Now().UTC().AddDate(0, 0, -workflowScheduleRunRetentionDays-1)
	seed, err := json.Marshal([]ScheduleRunEntry{
		{ID: "old-completed", ScheduleID: "deploy-hook", Status: "success", StartedAt: old},
		{ID: "old-running", ScheduleID: "deploy-hook", Status: "running", StartedAt: old},
	})
	if err != nil {
		t.Fatal(err)
	}
	stub.files[scheduleRunsPath("Workflow/test")] = string(seed)
	if err := AppendScheduleRun(ctx, "Workflow/test", &ScheduleRunEntry{ID: "new", ScheduleID: "deploy-hook", Status: "success", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	runs, err := ReadScheduleRuns(ctx, "Workflow/test")
	if err != nil || len(runs) != 2 || runs[0].ID != "new" || runs[1].ID != "old-running" {
		t.Fatalf("retained runs = %+v, err=%v", runs, err)
	}
}

func TestAppendScheduleRunDoesNotEraseCorruptHistory(t *testing.T) {
	stub, _ := newScheduleRunWorkspaceStub(t)
	stub.files["Workflow/test/schedule-runs.json"] = "{not valid json"

	err := AppendScheduleRun(context.Background(), "Workflow/test", &ScheduleRunEntry{
		ID:         "run-2",
		ScheduleID: "schedule-1",
		Status:     "running",
		StartedAt:  time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("AppendScheduleRun() error = nil, want corrupt-history error")
	}
	if got := stub.files["Workflow/test/schedule-runs.json"]; got != "{not valid json" {
		t.Fatalf("corrupt history was overwritten: %q", got)
	}
}

func TestUpdateScheduleRunRejectsMissingRunAndCompletesStoppedRun(t *testing.T) {
	_, _ = newScheduleRunWorkspaceStub(t)
	ctx := context.Background()
	run := &ScheduleRunEntry{
		ID:         "run-1",
		ScheduleID: "schedule-1",
		Status:     "running",
		StartedAt:  time.Now().UTC(),
	}
	if err := AppendScheduleRun(ctx, "Workflow/test", run); err != nil {
		t.Fatal(err)
	}
	if err := UpdateScheduleRun(ctx, "Workflow/test", "missing", "success", "", nil, "", ""); err == nil {
		t.Fatal("UpdateScheduleRun(missing) error = nil")
	}
	if err := UpdateScheduleRun(ctx, "Workflow/test", "run-1", "stopped", "stopped by user", nil, "", "session-1"); err != nil {
		t.Fatal(err)
	}

	runs, err := ReadScheduleRuns(ctx, "Workflow/test")
	if err != nil {
		t.Fatal(err)
	}
	if runs[0].Status != "stopped" || runs[0].CompletedAt == nil {
		t.Fatalf("updated run = %+v, want stopped with completed_at", runs[0])
	}
}

func TestScheduleRunPreservesOccurrenceIdentity(t *testing.T) {
	_, _ = newScheduleRunWorkspaceStub(t)
	ctx := context.Background()
	scheduledFor := time.Date(2026, time.August, 21, 9, 30, 0, 0, time.UTC)
	run := &ScheduleRunEntry{
		ID:            "run-cron-1",
		ScheduleID:    "schedule-1",
		TriggerSource: "cron",
		ScheduledFor:  &scheduledFor,
		Status:        "running",
		StartedAt:     scheduledFor.Add(45 * time.Second),
	}
	if err := AppendScheduleRun(ctx, "Workflow/test", run); err != nil {
		t.Fatal(err)
	}

	runs, err := ReadScheduleRuns(ctx, "Workflow/test")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].TriggerSource != "cron" || runs[0].ScheduledFor == nil || !runs[0].ScheduledFor.Equal(scheduledFor) {
		t.Fatalf("persisted run = %+v, want cron occurrence at %s", runs, scheduledFor)
	}
}

func TestClaimScheduleRunDedupesByID(t *testing.T) {
	_, _ = newScheduleRunWorkspaceStub(t)
	ctx := context.Background()
	ws := "Chats/test-user/project-claim"
	run := &ScheduleRunEntry{ID: "run-1", ScheduleID: "sched-1", TriggerSource: "webhook", Status: "queued", StartedAt: time.Now().UTC()}
	if _, claimed, err := ClaimScheduleRun(ctx, ws, run); err != nil || !claimed {
		t.Fatalf("first claim = claimed=%v err=%v", claimed, err)
	}
	existing, claimed, err := ClaimScheduleRun(ctx, ws, &ScheduleRunEntry{ID: "run-1", ScheduleID: "sched-1", Status: "queued", StartedAt: time.Now().UTC()})
	if err != nil || claimed {
		t.Fatalf("second claim = claimed=%v err=%v", claimed, err)
	}
	if existing == nil || existing.ID != "run-1" || existing.Status != "queued" {
		t.Fatalf("existing = %+v", existing)
	}
	runs, err := ReadScheduleRuns(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1 (redelivery must not fork history)", len(runs))
	}
}

func TestClaimScheduleRunRejectsEmptyID(t *testing.T) {
	_, _ = newScheduleRunWorkspaceStub(t)
	ctx := context.Background()
	if _, _, err := ClaimScheduleRun(ctx, "Chats/test-user/project-claim", &ScheduleRunEntry{}); err == nil {
		t.Fatal("expected an error for an empty run id")
	}
	if _, _, err := ClaimScheduleRun(ctx, "Chats/test-user/project-claim", nil); err == nil {
		t.Fatal("expected an error for a nil run")
	}
}

func TestFindScheduleRun(t *testing.T) {
	_, _ = newScheduleRunWorkspaceStub(t)
	ctx := context.Background()
	ws := "Chats/test-user/project-find"
	if _, claimed, err := ClaimScheduleRun(ctx, ws, &ScheduleRunEntry{ID: "run-9", ScheduleID: "sched-1", Status: "running", StartedAt: time.Now().UTC()}); err != nil || !claimed {
		t.Fatalf("claim = claimed=%v err=%v", claimed, err)
	}
	found, err := FindScheduleRun(ctx, ws, "run-9")
	if err != nil || found.ID != "run-9" || found.Status != "running" {
		t.Fatalf("found = %+v err=%v", found, err)
	}
	if _, err := FindScheduleRun(ctx, ws, "missing"); err == nil {
		t.Fatal("expected an error for an unknown run id")
	}
}

func seedScheduleRunEntries(t *testing.T, stub *scheduleRunWorkspaceStub, workspacePath string, runs []ScheduleRunEntry) {
	t.Helper()
	data, err := json.Marshal(runs)
	if err != nil {
		t.Fatal(err)
	}
	stub.files[workspacePath+"/schedule-runs.json"] = string(data)
}

func remainingScheduleRunIDs(t *testing.T, ctx context.Context, workspacePath string) []string {
	t.Helper()
	runs, err := ReadScheduleRuns(ctx, workspacePath)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(runs))
	for _, run := range runs {
		ids = append(ids, run.ID)
	}
	return ids
}

func TestDeleteScheduleRunsOlderThan(t *testing.T) {
	stub, _ := newScheduleRunWorkspaceStub(t)
	ctx := context.Background()
	ws := "Workflow/cleanup-test"
	old := time.Now().UTC().AddDate(0, 0, -60)
	recent := time.Now().UTC().AddDate(0, 0, -2)
	seedScheduleRunEntries(t, stub, ws, []ScheduleRunEntry{
		{ID: "old-terminal", ScheduleID: "sched-1", Status: "success", StartedAt: old},
		{ID: "old-running", ScheduleID: "sched-1", Status: "running", StartedAt: old},
		{ID: "old-queued", ScheduleID: "sched-1", Status: "queued", StartedAt: old},
		{ID: "recent-terminal", ScheduleID: "sched-1", Status: "error", StartedAt: recent},
		{ID: "old-other-schedule", ScheduleID: "sched-2", Status: "success", StartedAt: old},
		{ID: "zero-time", ScheduleID: "sched-1", Status: "success"},
	})

	deleted, err := DeleteScheduleRunsOlderThan(ctx, ws, []string{"sched-1"}, 30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	want := map[string]bool{"old-running": true, "old-queued": true, "recent-terminal": true, "old-other-schedule": true, "zero-time": true}
	for _, id := range remainingScheduleRunIDs(t, ctx, ws) {
		if !want[id] {
			t.Fatalf("remaining run %q should have been deleted", id)
		}
		delete(want, id)
	}
	if len(want) != 0 {
		t.Fatalf("missing remaining runs: %v", want)
	}

	deleted, err = DeleteScheduleRunsOlderThan(ctx, ws, nil, 30)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1 (other schedule, no filter)", deleted)
	}
	if got := remainingScheduleRunIDs(t, ctx, ws); len(got) != 4 {
		t.Fatalf("len(remaining) = %d, want 4", len(got))
	}
}

func TestDeleteScheduleRunsOlderThanLeavesCorruptHistoryAlone(t *testing.T) {
	stub, _ := newScheduleRunWorkspaceStub(t)
	stub.files["Workflow/corrupt/schedule-runs.json"] = "{not valid json"
	if _, err := DeleteScheduleRunsOlderThan(context.Background(), "Workflow/corrupt", nil, 30); err == nil {
		t.Fatal("DeleteScheduleRunsOlderThan() error = nil, want corrupt-history error")
	}
	if got := stub.files["Workflow/corrupt/schedule-runs.json"]; got != "{not valid json" {
		t.Fatalf("corrupt history was overwritten: %q", got)
	}
}
