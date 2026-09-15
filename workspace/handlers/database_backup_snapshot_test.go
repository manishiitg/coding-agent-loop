package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/manishiitg/coding-agent-loop/workspace/models"
	"github.com/spf13/viper"
)

func TestWorkflowDatabaseBackupSnapshotIncludesWALAndReplacesPriorImage(t *testing.T) {
	rel, abs, router := setupWorkflowDBTest(t)
	db, err := sql.Open("sqlite", abs)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE facts(id INTEGER PRIMARY KEY, value TEXT); INSERT INTO facts(value) VALUES ('first');`); err != nil {
		t.Fatal(err)
	}

	request := models.WorkflowDatabaseBackupSnapshotRequest{DBPath: rel}
	first := postWorkflowDBTest(t, router, "/api/db/backup-snapshot", request)
	if first.Code != http.StatusOK {
		t.Fatalf("first snapshot failed: status=%d body=%s", first.Code, first.Body.String())
	}
	var response models.APIResponse[models.WorkflowDatabaseBackupSnapshotResult]
	if err := json.Unmarshal(first.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	wantRel := filepath.ToSlash(filepath.Join("Workflow", "wal-test", "backup", "database", "db.sqlite"))
	wantChecksumRel := wantRel + ".sha256"
	if response.Data.SnapshotPath != wantRel || response.Data.ChecksumPath != wantChecksumRel || response.Data.Integrity != "ok" || response.Data.SHA256 == "" || response.Data.SizeBytes == 0 {
		t.Fatalf("unexpected snapshot response: %+v", response.Data)
	}
	snapshotAbs := filepath.Join(filepath.Dir(filepath.Dir(abs)), "backup", "database", "db.sqlite")
	assertSnapshotFactCount(t, snapshotAbs, 1)
	checksum, err := os.ReadFile(snapshotAbs + ".sha256")
	if err != nil {
		t.Fatal(err)
	}
	if string(checksum) != response.Data.SHA256+"  db.sqlite\n" {
		t.Fatalf("checksum contents=%q, want hash sidecar", checksum)
	}

	if _, err := db.Exec(`INSERT INTO facts(value) VALUES ('second')`); err != nil {
		t.Fatal(err)
	}
	second := postWorkflowDBTest(t, router, "/api/db/backup-snapshot", request)
	if second.Code != http.StatusOK {
		t.Fatalf("replacement snapshot failed: status=%d body=%s", second.Code, second.Body.String())
	}
	assertSnapshotFactCount(t, snapshotAbs, 2)
	if info, err := os.Stat(snapshotAbs); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm()&0o222 != 0 {
		t.Fatalf("snapshot mode=%#o, want read-only", info.Mode().Perm())
	}
}

func TestWorkflowDatabaseBackupSnapshotRejectsArbitraryPaths(t *testing.T) {
	_, _, router := setupWorkflowDBTest(t)
	for _, path := range []string{"notes.sqlite", "Workflow/wal-test/other.sqlite", "/tmp/db/db.sqlite", "../Workflow/wal-test/db/db.sqlite"} {
		recorder := postWorkflowDBTest(t, router, "/api/db/backup-snapshot", models.WorkflowDatabaseBackupSnapshotRequest{DBPath: path})
		if recorder.Code == http.StatusOK {
			t.Fatalf("unsafe db_path %q was accepted: %s", path, recorder.Body.String())
		}
	}
}

func TestWorkDatabaseBackupSnapshotUsesProjectBackupFolder(t *testing.T) {
	docs := t.TempDir()
	old := viper.GetString("docs-dir")
	viper.Set("docs-dir", docs)
	t.Cleanup(func() { viper.Set("docs-dir", old) })
	rel := "Chats/Work/projects/demo/db/db.sqlite"
	abs := filepath.Join(docs, "_users", "alice", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", abs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE facts(id INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/db/backup-snapshot", CreateWorkflowDatabaseBackupSnapshot)
	body, _ := json.Marshal(models.WorkflowDatabaseBackupSnapshotRequest{DBPath: rel})
	req := httptest.NewRequest(http.MethodPost, "/api/db/backup-snapshot", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "alice")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("Work snapshot failed: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := os.Stat(filepath.Join(docs, "_users", "alice", "Chats", "Work", "projects", "demo", "backup", "database", "db.sqlite")); err != nil {
		t.Fatal(err)
	}
}

func assertSnapshotFactCount(t *testing.T, path string, want int) {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM facts").Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("snapshot fact count=%d, want %d", got, want)
	}
	var integrity string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil {
		t.Fatal(err)
	}
	if integrity != "ok" {
		t.Fatalf("snapshot integrity=%q", integrity)
	}
}
