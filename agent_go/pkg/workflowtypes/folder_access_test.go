package workflowtypes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalizeFolderPath(t *testing.T) {
	dir := t.TempDir()
	canonical, err := CanonicalizeFolderPath(dir)
	if err != nil {
		t.Fatalf("CanonicalizeFolderPath failed: %v", err)
	}
	want, _ := filepath.EvalSymlinks(dir)
	if canonical != filepath.Clean(want) {
		t.Fatalf("canonical = %q, want %q", canonical, want)
	}
	if _, err := CanonicalizeFolderPath(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing path accepted")
	}
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalizeFolderPath(file); err == nil {
		t.Fatal("file accepted as directory")
	}
}

func TestFolderAliasEnvKey(t *testing.T) {
	if got := FolderAliasEnvKey("My Project!"); got != "MY_PROJECT" {
		t.Fatalf("key = %q", got)
	}
	if got := FolderAliasEnvKey("  "); got != "" {
		t.Fatalf("expected empty key, got %q", got)
	}
}

func TestNormalizeFolderGrantsPreservesTimestamps(t *testing.T) {
	dir := t.TempDir()
	previous := []WorkflowFolderGrant{{ID: "g1", CreatedAt: "2026-01-01T00:00:00Z"}}
	requested := []WorkflowFolderGrant{
		{ID: " g1 ", Alias: " site ", Path: dir, Access: "read_only"},
		{ID: "g2", Alias: "docs", Path: dir, Access: "read_write"},
	}
	got, err := NormalizeFolderGrants(requested, previous, "folder_access", "2026-09-13T00:00:00Z")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if got[0].CreatedAt != "2026-01-01T00:00:00Z" || got[0].Alias != "site" {
		t.Fatalf("existing grant = %+v", got[0])
	}
	if got[1].CreatedAt != "2026-09-13T00:00:00Z" || got[1].UpdatedAt != "2026-09-13T00:00:00Z" {
		t.Fatalf("new grant = %+v", got[1])
	}
	if _, err := NormalizeFolderGrants([]WorkflowFolderGrant{{ID: "x", Path: filepath.Join(dir, "missing")}}, nil, "folder_access", "now"); err == nil {
		t.Fatal("missing path accepted")
	} else if !strings.Contains(err.Error(), "folder_access[0]") {
		t.Fatalf("error lost its label: %v", err)
	}
}

func TestResolveFolderGrants(t *testing.T) {
	grants := []WorkflowFolderGrant{
		{ID: "1", Alias: "proj", Path: "/data/proj", Access: FolderAccessReadWrite},
		{ID: "2", Alias: "docs", Path: "/data/docs", Access: FolderAccessReadOnly},
		{ID: "3", Alias: "proj", Path: "/data/proj", Access: FolderAccessReadWrite},
	}
	read, write, readOnly, env := ResolveFolderGrants(grants, "WORK_FOLDER_")
	if len(read) != 2 || len(write) != 1 || len(readOnly) != 1 {
		t.Fatalf("read=%v write=%v readOnly=%v", read, write, readOnly)
	}
	if env["WORK_FOLDER_PROJ"] != "/data/proj" || env["WORK_FOLDER_DOCS"] != "/data/docs" {
		t.Fatalf("env=%v", env)
	}
}

func TestFolderGrantsPrompt(t *testing.T) {
	empty := FolderGrantsPrompt(nil, "WORK_FOLDER_", "Nothing attached.")
	if !strings.Contains(empty, "### Attached Folders") || !strings.Contains(empty, "Nothing attached.") {
		t.Fatalf("empty prompt = %q", empty)
	}
	withGrants := FolderGrantsPrompt([]WorkflowFolderGrant{
		{ID: "1", Alias: "proj", Access: FolderAccessReadWrite},
	}, "WORK_FOLDER_", "Nothing attached.")
	if !strings.Contains(withGrants, "**proj**") || !strings.Contains(withGrants, "$WORK_FOLDER_PROJ") {
		t.Fatalf("prompt = %q", withGrants)
	}
}

func TestFolderGrantAvailable(t *testing.T) {
	dir := t.TempDir()
	if !FolderGrantAvailable(dir) {
		t.Fatal("live dir reported unavailable")
	}
	if FolderGrantAvailable(filepath.Join(dir, "missing")) {
		t.Fatal("missing path reported available")
	}
}
