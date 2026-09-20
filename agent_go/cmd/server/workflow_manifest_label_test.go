package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Renaming a workflow edits one manifest field: the folder, the id, and
// every reference stay exactly where they were.
func TestUpdateWorkflowManifestLabelKeepsFolder(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WORKSPACE_DOCS_PATH", root)
	manifest := NewWorkflowManifest("Old name")
	manifest.ID = "wf_demo"
	raw, _ := json.Marshal(manifest)
	dir := filepath.Join(root, "Workflow", "demo")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "workflow.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	mock := &mockWorkspaceAPI{files: map[string]string{"Workflow/demo/workflow.json": string(raw)}}
	host := httptest.NewServer(mock)
	defer host.Close()
	t.Setenv("WORKSPACE_API_URL", host.URL)
	api := &StreamingAPI{}
	update := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		api.handleUpdateWorkflowManifest(rec, httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body)))
		return rec
	}

	rec := update(`{"workspace_path":"Workflow/demo","label":"New name"}`)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var response struct {
		Manifest WorkflowManifest `json:"manifest"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Manifest.Label != "New name" {
		t.Fatalf("label not updated: %q", response.Manifest.Label)
	}
	if response.Manifest.ID != "wf_demo" {
		t.Fatalf("id changed on rename: %q", response.Manifest.ID)
	}

	mock.mu.Lock()
	persisted := mock.files["Workflow/demo/workflow.json"]
	var touched []string
	for key := range mock.files {
		touched = append(touched, key)
	}
	mock.mu.Unlock()
	if !strings.Contains(persisted, `"label": "New name"`) {
		t.Fatalf("renamed label not persisted: %s", persisted)
	}
	// The handler also appends a plan-changelog audit entry; everything must
	// stay inside the same workflow folder.
	for _, key := range touched {
		if !strings.HasPrefix(key, "Workflow/demo/") {
			t.Fatalf("rename escaped the workflow folder: %q", key)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "Workflow"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "demo" {
		t.Fatalf("rename moved the folder: %v", entries)
	}

	rec = update(`{"workspace_path":"Workflow/demo","label":"  Padded  "}`)
	if rec.Code != 200 {
		t.Fatal(rec.Code, rec.Body.String())
	}
	var padded struct {
		Manifest WorkflowManifest `json:"manifest"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &padded); err != nil {
		t.Fatal(err)
	}
	if padded.Manifest.Label != "Padded" {
		t.Fatalf("label not trimmed: %q", padded.Manifest.Label)
	}

	rec = update(`{"workspace_path":"Workflow/demo","label":""}`)
	if rec.Code == 200 || !strings.Contains(strings.ToLower(rec.Body.String()), "label") {
		t.Fatalf("empty label accepted: %d %s", rec.Code, rec.Body.String())
	}
}
