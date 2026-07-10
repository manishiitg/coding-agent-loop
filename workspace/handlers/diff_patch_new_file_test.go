package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

func TestApplyDiffPatchDirectCreatesContentFromEmptyFile(t *testing.T) {
	diff := "--- a/references/unfollow-cleanup.md\n" +
		"+++ b/references/unfollow-cleanup.md\n" +
		"@@ -0,0 +1,3 @@\n" +
		"+# X Unfollow Cleanup\n" +
		"+\n" +
		"+Use the shared browser and confirm each unfollow dialog.\n"

	got, err := ApplyDiffPatchDirect("", diff)
	if err != nil {
		t.Fatalf("ApplyDiffPatchDirect returned error: %v", err)
	}
	want := "# X Unfollow Cleanup\n\nUse the shared browser and confirm each unfollow dialog.\n"
	if got != want {
		t.Fatalf("patched content = %q, want %q", got, want)
	}
}

func TestDiffPatchErrorPreviewTruncatesLargeDiff(t *testing.T) {
	diff := "--- a/file\n+++ b/file\n@@ -0,0 +1,1 @@\n+" + strings.Repeat("x", 5000)

	got := diffPatchErrorPreview(diff)
	if len(got) >= len(diff) {
		t.Fatalf("expected preview to be shorter than original diff: preview=%d original=%d", len(got), len(diff))
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("expected truncation marker in preview, got %q", got[len(got)-80:])
	}
}

func TestDiffPatchDocumentErrorDoesNotEchoFullDiff(t *testing.T) {
	docsDir, cleanup := setupTestDocsDir(t)
	defer cleanup()
	gin.SetMode(gin.TestMode)
	viper.Set("docs-dir", docsDir)

	router := gin.New()
	router.PATCH("/api/documents/*filepath", HandleDocumentRequest)

	diff := "this is not a diff\n" + strings.Repeat("x", 50000)
	body, err := json.Marshal(map[string]string{"diff": diff})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/documents/_tmp_codex_probe/bad%20diff.md/diff", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	responseBody := w.Body.String()
	if len(responseBody) > 12000 {
		t.Fatalf("error response too large: %d bytes", len(responseBody))
	}
	if strings.Contains(responseBody, strings.Repeat("x", 20000)) {
		t.Fatalf("error response echoed the full diff")
	}
	if !strings.Contains(responseBody, "truncated") {
		t.Fatalf("error response should contain truncation marker: %s", responseBody)
	}
}
