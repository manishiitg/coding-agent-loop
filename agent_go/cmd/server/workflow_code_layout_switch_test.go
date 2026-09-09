package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeWorkspaceDocumentStore is a minimal in-memory stand-in for the
// workspace API's GET/PUT /api/documents/<path> routes, just enough for
// readFileFromWorkspace/writeFileToWorkspace to round-trip against it.
type fakeWorkspaceDocumentStore struct {
	mu    sync.Mutex
	files map[string]string
}

func newFakeWorkspaceServer(t *testing.T) (*httptest.Server, *fakeWorkspaceDocumentStore) {
	t.Helper()
	store := &fakeWorkspaceDocumentStore{files: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/documents/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/documents/")
		store.mu.Lock()
		defer store.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			content, ok := store.files[path]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"data":    map[string]any{"filepath": path, "content": content},
			})
		case http.MethodPut:
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			store.files[path] = body.Content
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, store
}

func TestSetWorkflowCodeLayoutVersionRejectsUnsupportedVersion(t *testing.T) {
	// No network setup needed: the version check runs before any workspace read.
	if err := SetWorkflowCodeLayoutVersion(context.Background(), "Workflow/whatever", 2); err == nil {
		t.Fatal("expected an error for an unsupported code_layout_version, got nil")
	}
}

func TestSetWorkflowCodeLayoutVersionSwitchesAndRollsBack(t *testing.T) {
	server, store := newFakeWorkspaceServer(t)
	t.Setenv("WORKSPACE_API_URL", server.URL)

	const workspacePath = "Workflow/test-code-layout-switch"
	seed, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"id":             "wf_test",
		"label":          "Test",
	})
	if err != nil {
		t.Fatalf("marshal seed manifest: %v", err)
	}
	store.mu.Lock()
	store.files[workspacePath+"/workflow.json"] = string(seed)
	store.mu.Unlock()

	ctx := context.Background()

	// Switching to a legacy manifest's already-current value (0) must be
	// rejected as a no-op, not silently accepted.
	if err := SetWorkflowCodeLayoutVersion(ctx, workspacePath, 0); err == nil {
		t.Fatal("expected an error switching to the already-current version 0, got nil")
	}

	if err := SetWorkflowCodeLayoutVersion(ctx, workspacePath, 1); err != nil {
		t.Fatalf("switch to version 1 failed: %v", err)
	}
	store.mu.Lock()
	afterSwitch := store.files[workspacePath+"/workflow.json"]
	store.mu.Unlock()
	var switched WorkflowManifest
	if err := json.Unmarshal([]byte(afterSwitch), &switched); err != nil {
		t.Fatalf("parse manifest after switch: %v", err)
	}
	if switched.CodeLayoutVersion != 1 {
		t.Fatalf("code_layout_version = %d after switch, want 1", switched.CodeLayoutVersion)
	}

	// Nothing under learnings/ is ever touched by this call, so rolling back
	// is just switching the field again -- verify that path too.
	if err := SetWorkflowCodeLayoutVersion(ctx, workspacePath, 0); err != nil {
		t.Fatalf("roll back to version 0 failed: %v", err)
	}
	store.mu.Lock()
	afterRollback := store.files[workspacePath+"/workflow.json"]
	store.mu.Unlock()
	var rolledBack WorkflowManifest
	if err := json.Unmarshal([]byte(afterRollback), &rolledBack); err != nil {
		t.Fatalf("parse manifest after rollback: %v", err)
	}
	if rolledBack.CodeLayoutVersion != 0 {
		t.Fatalf("code_layout_version = %d after rollback, want 0", rolledBack.CodeLayoutVersion)
	}
}

func TestSetWorkflowCodeLayoutVersionMissingManifest(t *testing.T) {
	server, _ := newFakeWorkspaceServer(t)
	t.Setenv("WORKSPACE_API_URL", server.URL)

	if err := SetWorkflowCodeLayoutVersion(context.Background(), "Workflow/does-not-exist", 1); err == nil {
		t.Fatal("expected an error for a workspace with no workflow.json, got nil")
	}
}
