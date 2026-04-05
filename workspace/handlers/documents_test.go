package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/manishiitg/mcp-agent-builder-go/workspace/models"
	"github.com/manishiitg/mcp-agent-builder-go/workspace/utils"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// setupTestDocsDir creates a realistic workspace directory structure for testing
func setupTestDocsDir(t *testing.T) (string, func()) {
	t.Helper()
	docsDir, err := os.MkdirTemp("", "docs-handler-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Create shared folders at root level
	for _, folder := range []string{"Chats", "Downloads", "skills"} {
		os.MkdirAll(filepath.Join(docsDir, folder), 0755)
	}

	// Add files to Chats (shared by all users)
	os.WriteFile(
		filepath.Join(docsDir, "Chats", "session1.json"),
		[]byte(`{"id":"s1"}`), 0644,
	)

	// Add files to skills
	os.WriteFile(filepath.Join(docsDir, "skills", "shared-skill.json"), []byte(`{}`), 0644)

	return docsDir, func() { os.RemoveAll(docsDir) }
}

// setupRouter creates a gin router with the documents endpoint and viper config
func setupRouter(docsDir string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	viper.Set("docs-dir", docsDir)

	r := gin.New()
	r.GET("/api/documents", ListDocuments)
	return r
}

// listDocs makes a GET /api/documents request and returns the parsed response
func listDocs(t *testing.T, router *gin.Engine, folder string, userID string) models.APIResponse[[]models.Document] {
	t.Helper()
	url := "/api/documents"
	if folder != "" {
		url += "?folder=" + folder
	}

	req, _ := http.NewRequest("GET", url, nil)
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp models.APIResponse[[]models.Document]
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v\nBody: %s", err, w.Body.String())
	}
	return resp
}

// collectFilePaths flattens a hierarchical document tree into a list of file paths
func collectFilePaths(docs []models.Document) []string {
	var paths []string
	for _, doc := range docs {
		paths = append(paths, doc.FilePath)
		if len(doc.Children) > 0 {
			paths = append(paths, collectFilePaths(doc.Children)...)
		}
	}
	return paths
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// --- Tests ---

func TestRootListingFiltersUsersDirectory(t *testing.T) {
	docsDir, cleanup := setupTestDocsDir(t)
	defer cleanup()

	// Create a legacy _users/ directory to verify it gets filtered
	os.MkdirAll(filepath.Join(docsDir, utils.UsersDirectory, "default", "Chats"), 0755)

	router := setupRouter(docsDir)

	resp := listDocs(t, router, "", utils.GetDefaultUserID())
	paths := collectFilePaths(resp.Data)

	// _users/ should NOT appear in root listing
	for _, p := range paths {
		if p == utils.UsersDirectory || p == utils.UsersDirectory+"/" {
			t.Errorf("_users/ directory should not appear in root listing, found: %s", p)
		}
	}

	// Chats/ should appear at root level
	if !contains(paths, "Chats") {
		t.Error("Chats/ should appear in root listing")
	}

	// Shared folders should appear
	if !contains(paths, "skills") {
		t.Error("skills/ should appear in root listing")
	}
}

func TestRootListingWithDotFolder(t *testing.T) {
	docsDir, cleanup := setupTestDocsDir(t)
	defer cleanup()
	router := setupRouter(docsDir)

	resp := listDocs(t, router, ".", utils.GetDefaultUserID())
	paths := collectFilePaths(resp.Data)

	// folder=. should behave the same as empty folder (root listing)
	for _, p := range paths {
		if p == utils.UsersDirectory || p == utils.UsersDirectory+"/" {
			t.Errorf("_users/ should not appear when folder=., found: %s", p)
		}
	}
}

func TestAllUsersShareFilesystem(t *testing.T) {
	docsDir, cleanup := setupTestDocsDir(t)
	defer cleanup()
	router := setupRouter(docsDir)

	// Both users should see the same Chats/ content
	resp1 := listDocs(t, router, "Chats", "user1")
	resp2 := listDocs(t, router, "Chats", "user2")

	paths1 := collectFilePaths(resp1.Data)
	paths2 := collectFilePaths(resp2.Data)

	if len(paths1) != len(paths2) {
		t.Errorf("All users should see same Chats/ content: user1=%v, user2=%v", paths1, paths2)
	}

	found1 := contains(paths1, "session1.json") || contains(paths1, "Chats/session1.json")
	found2 := contains(paths2, "session1.json") || contains(paths2, "Chats/session1.json")
	if !found1 || !found2 {
		t.Errorf("Both users should see session1.json: user1=%v, user2=%v", paths1, paths2)
	}
}

func TestSharedFoldersSameForAllUsers(t *testing.T) {
	docsDir, cleanup := setupTestDocsDir(t)
	defer cleanup()
	router := setupRouter(docsDir)

	resp1 := listDocs(t, router, "skills", utils.GetDefaultUserID())
	resp2 := listDocs(t, router, "skills", "user2")

	paths1 := collectFilePaths(resp1.Data)
	paths2 := collectFilePaths(resp2.Data)

	if len(paths1) != len(paths2) {
		t.Errorf("Shared folder should return same content for all users: default=%v, user2=%v", paths1, paths2)
	}

	if !contains(paths1, "shared-skill.json") && !contains(paths1, "skills/shared-skill.json") {
		t.Errorf("skills/ should contain shared-skill.json, got: %v", paths1)
	}
}

func TestNoUserIDFallsToDefault(t *testing.T) {
	docsDir, cleanup := setupTestDocsDir(t)
	defer cleanup()
	router := setupRouter(docsDir)

	// No X-User-ID header — should still work and show shared content
	resp := listDocs(t, router, "Chats", "")
	paths := collectFilePaths(resp.Data)

	found := contains(paths, "session1.json") || contains(paths, "Chats/session1.json")
	if !found {
		t.Errorf("No user ID should still see shared session1.json, got: %v", paths)
	}
}

func TestNormalizeFolderPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"empty", "", ""},
		{"root-slash", "/", "/"},
		{"trailing-slash", "Chats/", "Chats"},
		{"multiple-trailing", "Chats///", "Chats"},
		{"no-trailing", "Chats", "Chats"},
		{"nested-trailing", "Chats/sub/", "Chats/sub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeFolderPath(tt.input)
			if got != tt.expect {
				t.Errorf("normalizeFolderPath(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}
