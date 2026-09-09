package handlers

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserLivePortValidatesMetadata(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_BROWSER_SOCKET_DIR", dir)
	for _, input := range []string{"../escape", "", "a/b", "--cdp"} {
		// --cdp is a valid filename but must still have runtime metadata.
		if _, err := browserLivePort(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	for _, value := range []string{"0", "65536", "http://example.com", "123\n456"} {
		os.WriteFile(filepath.Join(dir, "live-test.stream"), []byte(value), 0600)
		if _, err := browserLivePort("live-test"); err == nil {
			t.Fatalf("accepted port %q", value)
		}
	}
	os.WriteFile(filepath.Join(dir, "live-test.stream"), []byte("12345\n"), 0600)
	if port, err := browserLivePort("live-test"); err != nil || port != 12345 {
		t.Fatalf("port=%d err=%v", port, err)
	}
}

func TestBrowserLiveUnavailableDoesNotStartBrowser(t *testing.T) {
	t.Setenv("AGENT_BROWSER_CDP_ENABLED", "false")
	router := gin.New()
	router.GET("/stream/:session", BrowserLiveStream)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/stream/nonexistent-live-test", nil))
	if response.Code != http.StatusNotFound {
		t.Fatal(response.Code)
	}
}
