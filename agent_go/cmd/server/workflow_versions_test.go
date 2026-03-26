package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
)

type mockWorkspaceAPI struct {
	mu    sync.Mutex
	files map[string]string
}

func (m *mockWorkspaceAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/documents":
		m.handleListDocuments(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/documents/"):
		m.handleDocument(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/folders/") && r.Method == http.MethodDelete:
		m.handleDeleteFolder(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (m *mockWorkspaceAPI) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	folder := r.URL.Query().Get("folder")
	maxDepth := 1
	if raw := r.URL.Query().Get("max_depth"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			maxDepth = parsed
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	root := buildFolderTree(folder, maxDepth, m.files)
	if root == nil {
		http.NotFound(w, r)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"data":    virtualtools.WorkspaceFolderListing{*root},
	})
}

func (m *mockWorkspaceAPI) handleDocument(w http.ResponseWriter, r *http.Request) {
	path := decodeWorkspacePath(strings.TrimPrefix(r.URL.Path, "/api/documents/"))

	switch r.Method {
	case http.MethodGet:
		m.mu.Lock()
		content, ok := m.files[path]
		m.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"data": map[string]interface{}{
				"content": content,
			},
		})
	case http.MethodPut:
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.files[path] = body.Content
		m.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"success": true,
		})
	case http.MethodDelete:
		m.mu.Lock()
		_, ok := m.files[path]
		if ok {
			delete(m.files, path)
		}
		m.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (m *mockWorkspaceAPI) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	folderPath := decodeWorkspacePath(strings.TrimPrefix(r.URL.Path, "/api/folders/"))
	prefix := folderPath + "/"

	m.mu.Lock()
	defer m.mu.Unlock()

	var deleted bool
	for path := range m.files {
		if strings.HasPrefix(path, prefix) {
			delete(m.files, path)
			deleted = true
		}
	}

	if !deleted {
		http.NotFound(w, r)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
	})
}

func decodeWorkspacePath(raw string) string {
	if raw == "" {
		return ""
	}

	parts := strings.Split(raw, "/")
	decoded := make([]string, 0, len(parts))
	for _, part := range parts {
		value, err := url.PathUnescape(part)
		if err != nil {
			decoded = append(decoded, part)
			continue
		}
		decoded = append(decoded, value)
	}
	return strings.Join(decoded, "/")
}

func buildFolderTree(folder string, maxDepth int, files map[string]string) *virtualtools.WorkspaceFolderItem {
	rootNode := &folderNode{children: map[string]*folderNode{}}
	found := false
	prefix := folder + "/"

	for filePath := range files {
		if !strings.HasPrefix(filePath, prefix) {
			continue
		}
		found = true
		remainder := strings.TrimPrefix(filePath, prefix)
		if remainder == "" {
			continue
		}

		segments := strings.Split(remainder, "/")
		current := rootNode
		currentPath := folder
		for i, segment := range segments {
			child := current.children[segment]
			if child == nil {
				child = &folderNode{children: map[string]*folderNode{}}
				current.children[segment] = child
			}
			currentPath = currentPath + "/" + segment
			child.path = currentPath
			child.name = segment
			if i == len(segments)-1 {
				child.isFile = true
			}
			current = child
		}
	}

	if !found {
		return nil
	}

	return &virtualtools.WorkspaceFolderItem{
		FilePath: folder,
		Type:     "folder",
		Children: convertFolderChildren(rootNode, 1, maxDepth),
	}
}

type folderNode struct {
	name     string
	path     string
	isFile   bool
	children map[string]*folderNode
}

func convertFolderChildren(node *folderNode, depth, maxDepth int) []virtualtools.WorkspaceFolderItem {
	names := make([]string, 0, len(node.children))
	for name := range node.children {
		names = append(names, name)
	}
	sort.Strings(names)

	items := make([]virtualtools.WorkspaceFolderItem, 0, len(names))
	for _, name := range names {
		child := node.children[name]
		item := virtualtools.WorkspaceFolderItem{
			FilePath: child.path,
			Type:     "folder",
		}
		if child.isFile {
			item.Type = "file"
		} else if depth < maxDepth {
			item.Children = convertFolderChildren(child, depth+1, maxDepth)
		}
		items = append(items, item)
	}
	return items
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func TestWorkflowVersionPublishAndRestoreIncludesLearnings(t *testing.T) {
	const workspacePath = "Workflow/Test Workspace"

	mockAPI := &mockWorkspaceAPI{
		files: map[string]string{
			workspacePath + "/planning/plan.json":                     `{"version":"original-plan"}`,
			workspacePath + "/planning/step_config.json":              `{"steps":["original-config"]}`,
			workspacePath + "/variables/variables.json":               `{"customer":"original"}`,
			workspacePath + "/learnings/step-1/SKILL.md":              "original learning",
			workspacePath + "/evaluation/learnings/eval-step/SKILL.md": "original eval learning",
		},
	}

	server := httptest.NewServer(mockAPI)
	defer server.Close()
	t.Setenv("WORKSPACE_API_URL", server.URL)

	api := &StreamingAPI{}

	publishReq := httptest.NewRequest(http.MethodPost, "/api/workflow/versions/publish", strings.NewReader(
		`{"workspace_path":"`+workspacePath+`","label":"baseline"}`,
	))
	publishRec := httptest.NewRecorder()
	api.handlePublishVersion(publishRec, publishReq)

	if publishRec.Code != http.StatusOK {
		t.Fatalf("publish returned status %d: %s", publishRec.Code, publishRec.Body.String())
	}

	mockAPI.mu.Lock()
	if got := mockAPI.files[workspacePath+"/versions/v1/learnings/step-1/SKILL.md"]; got != "original learning" {
		t.Fatalf("published snapshot missing learning, got %q", got)
	}
	if got := mockAPI.files[workspacePath+"/versions/v1/evaluation/learnings/eval-step/SKILL.md"]; got != "original eval learning" {
		t.Fatalf("published snapshot missing evaluation learning, got %q", got)
	}
	metaJSON := mockAPI.files[workspacePath+"/versions/v1/version_meta.json"]
	mockAPI.mu.Unlock()

	var meta struct {
		ManagedFolders []string `json:"managed_folders"`
	}
	if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
		t.Fatalf("failed to parse version metadata: %v", err)
	}
	if len(meta.ManagedFolders) != 2 || meta.ManagedFolders[0] != "learnings" || meta.ManagedFolders[1] != "evaluation/learnings" {
		t.Fatalf("unexpected managed_folders: %#v", meta.ManagedFolders)
	}

	mockAPI.mu.Lock()
	mockAPI.files[workspacePath+"/planning/plan.json"] = `{"version":"mutated-plan"}`
	mockAPI.files[workspacePath+"/planning/output_plan.json"] = `{"added":"later"}`
	mockAPI.files[workspacePath+"/learnings/step-1/SKILL.md"] = "mutated learning"
	mockAPI.files[workspacePath+"/learnings/step-2/SKILL.md"] = "stale new learning"
	delete(mockAPI.files, workspacePath+"/evaluation/learnings/eval-step/SKILL.md")
	mockAPI.files[workspacePath+"/evaluation/learnings/eval-step-2/SKILL.md"] = "stale eval learning"
	mockAPI.mu.Unlock()

	revertReq := httptest.NewRequest(http.MethodPost, "/api/workflow/versions/revert", strings.NewReader(
		`{"workspace_path":"`+workspacePath+`","version":1}`,
	))
	revertRec := httptest.NewRecorder()
	api.handleRevertVersion(revertRec, revertReq)

	if revertRec.Code != http.StatusOK {
		t.Fatalf("revert returned status %d: %s", revertRec.Code, revertRec.Body.String())
	}

	mockAPI.mu.Lock()
	defer mockAPI.mu.Unlock()

	if got := mockAPI.files[workspacePath+"/planning/plan.json"]; got != `{"version":"original-plan"}` {
		t.Fatalf("plan.json not restored, got %q", got)
	}
	if _, exists := mockAPI.files[workspacePath+"/planning/output_plan.json"]; exists {
		t.Fatalf("output_plan.json should have been removed during restore")
	}
	if got := mockAPI.files[workspacePath+"/learnings/step-1/SKILL.md"]; got != "original learning" {
		t.Fatalf("learning not restored, got %q", got)
	}
	if _, exists := mockAPI.files[workspacePath+"/learnings/step-2/SKILL.md"]; exists {
		t.Fatalf("stale learning should have been removed during restore")
	}
	if got := mockAPI.files[workspacePath+"/evaluation/learnings/eval-step/SKILL.md"]; got != "original eval learning" {
		t.Fatalf("evaluation learning not restored, got %q", got)
	}
	if _, exists := mockAPI.files[workspacePath+"/evaluation/learnings/eval-step-2/SKILL.md"]; exists {
		t.Fatalf("stale evaluation learning should have been removed during restore")
	}
}

func TestWorkflowVersionPublishRejectsEmptyLabel(t *testing.T) {
	api := &StreamingAPI{}

	req := httptest.NewRequest(http.MethodPost, "/api/workflow/versions/publish", strings.NewReader(
		`{"workspace_path":"Workflow/Test","label":"   "}`,
	))
	rec := httptest.NewRecorder()
	api.handlePublishVersion(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty label, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "label is required") {
		t.Fatalf("expected label validation error, got %q", rec.Body.String())
	}
}
