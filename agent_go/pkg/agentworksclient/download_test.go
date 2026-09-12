package agentworksclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadDoesNotPublishPartialOrOverwrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.Write([]byte("partial"))
	}))
	defer server.Close()
	client, _ := New(server.URL, "pat")
	dir := t.TempDir()
	output := filepath.Join(dir, "output")
	if _, err := client.Download(context.Background(), "wf", "asset", output); err == nil {
		t.Fatal("truncated body accepted")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("partial file published")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatal("temporary file leaked")
	}
	os.WriteFile(output, []byte("keep"), 0600)
	if _, err := client.Download(context.Background(), "wf", "asset", output); err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatal("existing file not protected", err)
	}
}
