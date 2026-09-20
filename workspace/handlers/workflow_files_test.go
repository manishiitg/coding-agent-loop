package handlers

import (
	"bytes"
	"encoding/json"
	"github.com/gin-gonic/gin"
	wf "github.com/manishiitg/coding-agent-loop/workspace/workflowfiles"
	"github.com/spf13/viper"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func workflowFilesHarness(t *testing.T) (string, func(wf.Request) (int, wf.Result, string)) {
	t.Helper()
	dir := t.TempDir()
	old := viper.Get("docs-dir")
	viper.Set("docs-dir", dir)
	t.Cleanup(func() { viper.Set("docs-dir", old) })
	root := filepath.Join(dir, "Workflow", "sample")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/api/workflow-files", WorkflowFiles)
	return root, func(req wf.Request) (int, wf.Result, string) {
		req.Root = "Workflow/sample"
		data, _ := json.Marshal(req)
		r := httptest.NewRequest("POST", "/api/workflow-files", bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		var result wf.Result
		_ = json.Unmarshal(rec.Body.Bytes(), &result)
		return rec.Code, result, rec.Body.String()
	}
}
func TestWorkflowFilesRevisionPatchAndProtectedPaths(t *testing.T) {
	root, call := workflowFilesHarness(t)
	status, r, body := call(wf.Request{Operation: "write", Path: "docs/notes.md", Content: "hello\n", ExpectedRevision: "missing"})
	if status != 200 || r.Revision != wf.Revision([]byte("hello\n")) {
		t.Fatalf("create: %d %s", status, body)
	}
	revision := r.Revision
	status, _, body = call(wf.Request{Operation: "write", Path: "docs/notes.md", Content: "lost", ExpectedRevision: "missing"})
	if status != 409 {
		t.Fatalf("stale write: %d %s", status, body)
	}
	status, r, body = call(wf.Request{Operation: "patch", Path: "docs/notes.md", Diff: "--- a/notes.md\n+++ b/notes.md\n@@ -1 +1 @@\n-hello\n+world\n", ExpectedRevision: revision})
	if status != 200 {
		t.Fatalf("patch: %d %s", status, body)
	}
	data, _ := os.ReadFile(filepath.Join(root, "docs", "notes.md"))
	if string(data) != "world\n" {
		t.Fatalf("patch content %q", data)
	}
	for _, p := range []string{"../other/file", "/etc/passwd", "docs/../../workflow.json", "planning/plan.json", "evaluation/step_config.json", "workflow.json", "runs/iteration-0/x", "builder/conversation/x", "secrets/key", "keys/key", "docs/.env.local"} {
		status, _, body = call(wf.Request{Operation: "write", Path: p, Content: "bad", ExpectedRevision: "missing"})
		if status != 400 && status != 403 {
			t.Errorf("path %q admitted: %d %s", p, status, body)
		}
	}
}
func TestWorkflowFilesSymlinksPrivateAndPagination(t *testing.T) {
	root, call := workflowFilesHarness(t)
	for p, content := range map[string]string{"a.md": "needle\nneedle again\n", "z.md": "needle z\n", "builder/conversation/private.json": "needle private", "planning/plan.json": "{}"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("planning/plan.json", filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"alias", "escape/secret", "builder/conversation/private.json"} {
		status, _, body := call(wf.Request{Operation: "read", Path: p})
		if status != 403 {
			t.Errorf("private/symlink %s: %d %s", p, status, body)
		}
	}
	status, r, body := call(wf.Request{Operation: "search", Query: "needle", Limit: 2})
	if status != 200 || len(r.Entries) != 2 || r.NextOffset != 2 || !r.Truncated {
		t.Fatalf("page1: %d %s", status, body)
	}
	status, r, body = call(wf.Request{Operation: "search", Query: "needle", Limit: 2, Offset: 2})
	if status != 200 || len(r.Entries) != 1 || r.Entries[0].Path != "z.md" || r.NextOffset != 0 {
		t.Fatalf("page2: %d %s", status, body)
	}
	status, r, body = call(wf.Request{Operation: "list", Limit: 200})
	if status != 200 {
		t.Fatal(body)
	}
	for _, e := range r.Entries {
		if strings.Contains(e.Path, "builder") || e.Path == "alias" || e.Path == "escape" {
			t.Errorf("private listing: %+v", e)
		}
	}
}
func TestWorkflowFilesManagedTransactionConflictAndCommit(t *testing.T) {
	root, call := workflowFilesHarness(t)
	if err := os.MkdirAll(filepath.Join(root, "planning"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "planning", "plan.json"), []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	req := wf.Request{Operation: "commit", Managed: true, Checks: map[string]string{"planning/plan.json": "stale", "planning/changelog/log.json": "missing"}, Writes: map[string]string{"planning/plan.json": "new", "planning/changelog/log.json": "log"}}
	status, _, body := call(req)
	if status != 409 {
		t.Fatalf("conflict: %d %s", status, body)
	}
	if _, err := os.Stat(filepath.Join(root, "planning", "changelog", "log.json")); !os.IsNotExist(err) {
		t.Fatal("conflicted transaction wrote changelog")
	}
	req.Checks["planning/plan.json"] = wf.Revision([]byte("old"))
	status, _, body = call(req)
	if status != 200 {
		t.Fatalf("commit: %d %s", status, body)
	}
	for p, want := range req.Writes {
		data, err := os.ReadFile(filepath.Join(root, p))
		if err != nil || string(data) != want {
			t.Errorf("%s: %q %v", p, data, err)
		}
	}
	req.Managed = false
	status, _, _ = call(req)
	if status != 403 {
		t.Fatal("unmanaged commit accepted")
	}
}
func TestWorkflowFilesBinaryLimitsAndExecutableMode(t *testing.T) {
	root, call := workflowFilesHarness(t)
	_ = os.WriteFile(filepath.Join(root, "binary.bin"), []byte{0, 1, 255}, 0644)
	status, r, body := call(wf.Request{Operation: "read", Path: "binary.bin"})
	if status != 200 || r.Encoding != "base64" || r.Content != "AAH/" {
		t.Fatalf("binary: %d %s", status, body)
	}
	_ = os.WriteFile(filepath.Join(root, "huge.txt"), bytes.Repeat([]byte("a"), wf.MaxFileBytes+1), 0644)
	status, _, _ = call(wf.Request{Operation: "read", Path: "huge.txt"})
	if status != 413 {
		t.Fatal("oversized file accepted")
	}
	_ = os.WriteFile(filepath.Join(root, "many.txt"), []byte(strings.Repeat("needle\n", 10000)), 0644)
	status, r, body = call(wf.Request{Operation: "search", Query: "needle", Limit: 1})
	if status != 200 || len(r.Entries) != 1 {
		t.Fatalf("bounded match: %d %s", status, body)
	}
	_ = os.WriteFile(filepath.Join(root, "script.sh"), []byte("old"), 0755)
	status, _, body = call(wf.Request{Operation: "write", Path: "script.sh", Content: "new", ExpectedRevision: wf.Revision([]byte("old"))})
	if status != 200 {
		t.Fatal(body)
	}
	st, _ := os.Stat(filepath.Join(root, "script.sh"))
	if st.Mode().Perm() != 0755 {
		t.Fatal("executable mode was lost")
	}
}

func TestWorkflowFilesConcurrentCASHasOneWinner(t *testing.T) {
	_, call := workflowFilesHarness(t)
	const original = "original"
	status, _, body := call(wf.Request{Operation: "write", Path: "notes.md", Content: original, ExpectedRevision: "missing"})
	if status != 200 {
		t.Fatal(body)
	}
	codes := make(chan int, 2)
	for _, content := range []string{"first", "second"} {
		go func(value string) {
			code, _, _ := call(wf.Request{Operation: "write", Path: "notes.md", Content: value, ExpectedRevision: wf.Revision([]byte(original))})
			codes <- code
		}(content)
	}
	a, b := <-codes, <-codes
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatalf("CAS statuses %d %d", a, b)
	}
}

func TestWorkflowFilesHidesAgentAndSkillPaths(t *testing.T) {
	root, call := workflowFilesHarness(t)
	for _, p := range []string{".claude/skills/demo/SKILL.md", ".agents/skills/demo/SKILL.md", "AGENTS.md", "docs/notes.md"} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	status, r, body := call(wf.Request{Operation: "list", Path: "", Depth: 8, Limit: 200})
	if status != 200 {
		t.Fatalf("list: %d %s", status, body)
	}
	for _, e := range r.Entries {
		if strings.Contains(e.Path, ".claude") || strings.Contains(e.Path, ".agents") || strings.HasSuffix(e.Path, "AGENTS.md") {
			t.Fatalf("agent path listed: %s", e.Path)
		}
	}
	found := false
	for _, e := range r.Entries {
		if e.Path == "docs/notes.md" {
			found = true
		}
	}
	if !found {
		t.Fatal("ordinary file missing from listing")
	}
	status, _, body = call(wf.Request{Operation: "search", Path: "", Query: "x", Depth: 8, Limit: 200})
	if status != 200 {
		t.Fatalf("search: %d %s", status, body)
	}
	for _, e := range r.Entries {
		if strings.Contains(e.Path, ".claude") || strings.Contains(e.Path, ".agents") {
			t.Fatalf("agent path in search: %s", e.Path)
		}
	}
	for _, p := range []string{".claude/skills/demo/SKILL.md", "AGENTS.md"} {
		if status, _, body := call(wf.Request{Operation: "read", Path: p}); status != 403 {
			t.Fatalf("read %s: got %d, want 403 (%s)", p, status, body)
		}
	}
	if status, _, body := call(wf.Request{Operation: "read", Path: "docs/notes.md"}); status != 200 {
		t.Fatalf("read ordinary file: %d %s", status, body)
	}
}

func TestWorkflowFilesMissingPathIsClean404(t *testing.T) {
	_, call := workflowFilesHarness(t)
	for _, req := range []wf.Request{
		{Operation: "list", Path: "totally/bogus/path"},
		{Operation: "search", Path: "totally/bogus", Query: "x"},
	} {
		status, _, body := call(req)
		if status != 404 {
			t.Fatalf("%s: got %d, want 404 (%s)", req.Operation, status, body)
		}
		if strings.Contains(body, "statat") {
			t.Fatalf("%s leaked raw error: %s", req.Operation, body)
		}
		if !strings.Contains(body, "path does not exist") {
			t.Fatalf("%s body wrong: %s", req.Operation, body)
		}
	}
}
