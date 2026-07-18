package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageBrowserUploadCopiesAuthorizedWorkspaceFile(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "inputs"), 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(base, "inputs", "report.txt")
	if err := os.WriteFile(source, []byte("upload-body"), 0600); err != nil {
		t.Fatal(err)
	}
	staged := browserUploadTestStagedPath(t, "report.txt")

	if err := StageBrowserUpload("inputs/report.txt", staged, base, base, []string{"inputs"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(staged)
	if err != nil || string(got) != "upload-body" {
		t.Fatalf("staged upload = %q, err=%v", got, err)
	}
	CleanupBrowserUploadStaging(staged)
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("staged upload was not cleaned: %v", err)
	}
}

func TestStageBrowserUploadUsesWorkingDirectoryFallback(t *testing.T) {
	base := t.TempDir()
	working := filepath.Join(base, "Downloads")
	if err := os.MkdirAll(working, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(working, "local.txt"), []byte("local"), 0600); err != nil {
		t.Fatal(err)
	}
	staged := browserUploadTestStagedPath(t, "local.txt")

	if err := StageBrowserUpload("local.txt", staged, base, working, []string{"Downloads"}, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestStageBrowserUploadRejectsUnauthorizedAndBlockedSources(t *testing.T) {
	base := t.TempDir()
	for _, dir := range []string{"allowed", "other", "blocked"} {
		if err := os.MkdirAll(filepath.Join(base, dir), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(base, dir, "file.txt"), []byte(dir), 0600); err != nil {
			t.Fatal(err)
		}
	}

	unauthorized := browserUploadTestStagedPath(t, "unauthorized.txt")
	err := StageBrowserUpload("other/file.txt", unauthorized, base, base, []string{"allowed"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "not covered") {
		t.Fatalf("unauthorized source error = %v", err)
	}

	blocked := browserUploadTestStagedPath(t, "blocked.txt")
	err = StageBrowserUpload("blocked/file.txt", blocked, base, base, []string{"blocked"}, nil, []string{"blocked"})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("blocked source error = %v", err)
	}
}

func TestStageBrowserUploadRejectsSymlinkSource(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "inputs"), 0755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(base, "inputs", "real.txt")
	if err := os.WriteFile(real, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "inputs", "link.txt")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	err := StageBrowserUpload("inputs/link.txt", browserUploadTestStagedPath(t, "link.txt"), base, base, []string{"inputs"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("symlink source error = %v", err)
	}
}

func browserUploadTestStagedPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp(BrowserUploadStagingDir(), "test-")
	if err != nil {
		if err := os.MkdirAll(BrowserUploadStagingDir(), 0700); err != nil {
			t.Fatal(err)
		}
		dir, err = os.MkdirTemp(BrowserUploadStagingDir(), "test-")
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}
