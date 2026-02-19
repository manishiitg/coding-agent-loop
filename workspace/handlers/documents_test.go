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

	// Create _users/default/ per-user folders with content
	for _, folder := range utils.PerUserFolders {
		dir := filepath.Join(docsDir, utils.UsersDirectory, utils.GetDefaultUserID(), folder)
		os.MkdirAll(dir, 0755)
	}
	// Add files to default user's Chats
	os.WriteFile(
		filepath.Join(docsDir, utils.UsersDirectory, utils.GetDefaultUserID(), "Chats", "session1.json"),
		[]byte(`{"id":"s1"}`), 0644,
	)

	// Create _users/user2/ per-user folders with content
	for _, folder := range utils.PerUserFolders {
		dir := filepath.Join(docsDir, utils.UsersDirectory, "user2", folder)
		os.MkdirAll(dir, 0755)
	}
	os.WriteFile(
		filepath.Join(docsDir, utils.UsersDirectory, "user2", "Chats", "user2-secret.json"),
		[]byte(`{"id":"u2"}`), 0644,
	)

	// Create shared folders
	os.MkdirAll(filepath.Join(docsDir, "skills"), 0755)
	os.WriteFile(filepath.Join(docsDir, "skills", "shared-skill.json"), []byte(`{}`), 0644)

	// Create per-user symlinks for default user
	for _, folder := range utils.PerUserFolders {
		target := filepath.Join(utils.UsersDirectory, utils.GetDefaultUserID(), folder)
		os.Symlink(target, filepath.Join(docsDir, folder))
	}

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
	router := setupRouter(docsDir)

	resp := listDocs(t, router, "", utils.GetDefaultUserID())
	paths := collectFilePaths(resp.Data)

	// _users/ should NOT appear in root listing
	for _, p := range paths {
		if p == utils.UsersDirectory || p == utils.UsersDirectory+"/" {
			t.Errorf("_users/ directory should not appear in root listing, found: %s", p)
		}
	}

	// Per-user folders SHOULD appear (injected from _users/default/)
	if !contains(paths, "Chats") {
		t.Error("Chats/ should appear in root listing (injected from user dir)")
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

func TestPerUserFolderIsolation(t *testing.T) {
	docsDir, cleanup := setupTestDocsDir(t)
	defer cleanup()
	router := setupRouter(docsDir)

	t.Run("DefaultUserSeesOwnChats", func(t *testing.T) {
		resp := listDocs(t, router, "Chats", utils.GetDefaultUserID())
		paths := collectFilePaths(resp.Data)
		t.Logf("Default user Chats/ paths: %v", paths)

		found := contains(paths, "session1.json") || contains(paths, "Chats/session1.json")
		if !found {
			t.Errorf("Default user should see session1.json in Chats/, got paths: %v", paths)
		}

		// Should NOT see user2's files
		if contains(paths, "user2-secret.json") || contains(paths, "Chats/user2-secret.json") {
			t.Error("Default user should NOT see user2's files in Chats/")
		}
	})

	t.Run("User2SeesOwnChats", func(t *testing.T) {
		resp := listDocs(t, router, "Chats", "user2")
		paths := collectFilePaths(resp.Data)
		t.Logf("User2 Chats/ paths: %v", paths)

		found := contains(paths, "user2-secret.json") || contains(paths, "Chats/user2-secret.json")
		if !found {
			t.Errorf("User2 should see user2-secret.json in Chats/, got paths: %v", paths)
		}

		// Should NOT see default user's files
		if contains(paths, "session1.json") || contains(paths, "Chats/session1.json") {
			t.Error("User2 should NOT see default user's session1.json")
		}
	})
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

	t.Logf("User1 skills paths: %v", paths1)
	t.Logf("User2 skills paths: %v", paths2)
	if !contains(paths1, "shared-skill.json") && !contains(paths1, "skills/shared-skill.json") {
		t.Errorf("skills/ should contain shared-skill.json, got: %v", paths1)
	}
}

func TestDirectUsersAccessBlocked(t *testing.T) {
	docsDir, cleanup := setupTestDocsDir(t)
	defer cleanup()
	router := setupRouter(docsDir)

	// Requesting _users/ directly should fail
	url := "/api/documents?folder=_users"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-User-ID", "user2")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return error (500 because ResolveUserPath returns error)
	if w.Code == http.StatusOK {
		var resp models.APIResponse[[]models.Document]
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Success {
			t.Error("Accessing _users/ directly should not succeed")
		}
	}
}

func TestCrossUserAccessViaUsersPath(t *testing.T) {
	docsDir, cleanup := setupTestDocsDir(t)
	defer cleanup()
	router := setupRouter(docsDir)

	// user2 trying to access _users/default/Chats should be blocked
	url := "/api/documents?folder=_users/default/Chats"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-User-ID", "user2")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		var resp models.APIResponse[[]models.Document]
		json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Success {
			paths := collectFilePaths(resp.Data)
			if contains(paths, "session1.json") {
				t.Error("User2 should NOT be able to access default user's files via _users/ path")
			}
		}
	}
}

func TestNoUserIDFallsToDefault(t *testing.T) {
	docsDir, cleanup := setupTestDocsDir(t)
	defer cleanup()
	router := setupRouter(docsDir)

	// No X-User-ID header → should fall back to "default"
	resp := listDocs(t, router, "Chats", "")
	paths := collectFilePaths(resp.Data)
	t.Logf("No-userID Chats/ paths: %v", paths)

	found := contains(paths, "session1.json") || contains(paths, "Chats/session1.json")
	if !found {
		t.Errorf("No user ID should fall back to default user and see session1.json, got: %v", paths)
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
