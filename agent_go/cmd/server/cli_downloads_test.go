package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
)

func serveCliDownload(t *testing.T, dir, file string) *httptest.ResponseRecorder {
	t.Helper()
	old := cliDownloadsDirOverride
	cliDownloadsDirOverride = dir
	t.Cleanup(func() { cliDownloadsDirOverride = old })
	req := httptest.NewRequest(http.MethodGet, "/api/downloads/cli/"+file, nil)
	req = mux.SetURLVars(req, map[string]string{"file": file})
	w := httptest.NewRecorder()
	(&StreamingAPI{}).handleCliDownload(w, req)
	return w
}

func TestCliDownloadServesAllowlistedFiles(t *testing.T) {
	dir := t.TempDir()
	for name := range cliDownloadFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("payload:"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, contentType := range cliDownloadFiles {
		w := serveCliDownload(t, dir, name)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: got %d", name, w.Code)
		}
		if got := w.Header().Get("Content-Type"); got != contentType {
			t.Fatalf("%s: content-type %q, want %q", name, got, contentType)
		}
		if w.Body.String() != "payload:"+name {
			t.Fatalf("%s: wrong body %q", name, w.Body.String())
		}
	}
}

func TestCliDownloadRejectsUnknownAndTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agentworks-darwin-arm64"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"agentworks-windows-amd64", "install-agentworks.sh.bak", "../server.go", "..%2Fserver.go", "", "agentworks-darwin-arm64/extra"} {
		if w := serveCliDownload(t, dir, file); w.Code != http.StatusNotFound {
			t.Fatalf("%q: got %d, want 404", file, w.Code)
		}
	}
}

func TestCliDownloadMissingFileOrDirIs404(t *testing.T) {
	if w := serveCliDownload(t, t.TempDir(), "agentworks-linux-amd64"); w.Code != http.StatusNotFound {
		t.Fatalf("missing file: got %d, want 404", w.Code)
	}
	if w := serveCliDownload(t, filepath.Join(t.TempDir(), "absent"), "install-agentworks.sh"); w.Code != http.StatusNotFound {
		t.Fatalf("missing dir: got %d, want 404", w.Code)
	}
}
