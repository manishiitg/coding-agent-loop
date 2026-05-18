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
