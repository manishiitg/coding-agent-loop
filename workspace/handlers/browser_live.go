package handlers

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

var browserLiveSessionName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

// BrowserLiveStream stays inside the workspace network namespace, where the
// managed headless daemon runs. No externally configured Chrome/CDP is needed.
// Only the agent service may call this route, after checking workflow ownership.
func BrowserLiveStream(c *gin.Context) {
	port, err := browserLivePort(c.Param("session"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Live browser unavailable. Start a browser session with a streaming-capable agent-browser version."})
		return
	}
	target := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)}
	proxy := httputil.NewSingleHostReverseProxy(target)
	original := proxy.Director
	proxy.Director = func(r *http.Request) {
		original(r)
		r.URL.Path = "/"
		r.URL.RawPath = ""
		r.URL.RawQuery = ""
		r.Host = target.Host
		r.Header.Del("Authorization")
		r.Header.Del("Cookie")
		r.Header.Del("X-Workspace-Token")
		r.Header.Set("Origin", "http://localhost")
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

func browserLivePort(session string) (int, error) {
	if !browserLiveSessionName.MatchString(session) {
		return 0, fmt.Errorf("invalid session")
	}
	home, _ := os.UserHomeDir()
	dirs := []string{filepath.Join(home, ".agent-browser"), filepath.Join(os.TempDir(), "agent-browser"), filepath.Join(os.TempDir(), ".agent-browser"), "/tmp/.agent-browser"}
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		dirs = append([]string{filepath.Join(runtimeDir, "agent-browser")}, dirs...)
	}
	if socketDir := os.Getenv("AGENT_BROWSER_SOCKET_DIR"); socketDir != "" {
		dirs = append([]string{socketDir}, dirs...)
	}
	for _, dir := range dirs {
		data, err := os.ReadFile(filepath.Join(dir, session+".stream"))
		if err != nil {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err == nil && port > 0 && port <= 65535 {
			return port, nil
		}
	}
	return 0, fmt.Errorf("stream metadata unavailable")
}
