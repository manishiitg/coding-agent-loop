package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

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
