// Static / public HTTP route handlers: health check, public file and folder
// serving (browse + download), capabilities, and CDP availability check.
// Relocated verbatim from server.go.
package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/browser"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// Health check endpoint
func (api *StreamingAPI) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Get current tracing provider
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"time":    time.Now(),
		"version": llmtypes.VERSION,
		// Read by deploy-rootless.sh before it swaps releases: a restart while
		// a turn is running returns 502 to the user mid-message (RTS,
		// 2026-09-03, twice in one afternoon). active_sessions counts turns
		// the tracker still considers running; in_flight_requests counts
		// non-GET API requests currently being served.
		"drain": api.drainStatus(),
		"config": map[string]interface{}{"provider": api.config.Provider,
			"model":            api.config.ModelID,
			"temperature":      api.config.Temperature,
			"max_turns":        api.config.MaxTurns,
			"tracing_provider": tracingProvider,
		},
	})
}

// handlePublicFile serves workspace files via a shareable URL.
// GET /api/public/file?path=<base64-encoded-filepath>
func (api *StreamingAPI) handlePublicFile(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("path")
	if encoded == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	// Decode base64 filepath
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			log.Printf("[PUBLIC-FILE] Failed to decode base64 path: %s, error: %v", encoded, err)
			http.Error(w, "invalid file path encoding", http.StatusBadRequest)
			return
		}
	}
	filePath := string(decoded)

	uid := publicWorkspaceUserID(r)
	log.Printf("[PUBLIC-FILE] Serving file: %s for user: %s", filePath, uid)

	// URL-encode each path segment for the workspace API
	segments := strings.Split(filePath, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	encodedPath := strings.Join(segments, "/")

	wsURL := getWorkspaceAPIURL() + "/api/documents/" + encodedPath + "/raw"
	log.Printf("[PUBLIC-FILE] Proxying to workspace: %s", wsURL)

	req, err := http.NewRequestWithContext(r.Context(), "GET", wsURL, nil)
	if err != nil {
		log.Printf("[PUBLIC-FILE] Failed to create request: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("X-User-ID", uid)
	if rangeHeader := r.Header.Get("Range"); rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		log.Printf("[PUBLIC-FILE] Workspace request failed: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[PUBLIC-FILE] Workspace returned %d: %s", resp.StatusCode, string(body))
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Proxy streaming/range headers so media players can seek through the same
	// generic workspace surface used by every product.
	for _, header := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Last-Modified"} {
		if value := resp.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	w.Header().Set("Content-Disposition", "inline")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handlePublicFolder lists workspace folder contents via a shareable URL.
// GET /api/public/folder?path=<base64-encoded-folderpath>
func (api *StreamingAPI) handlePublicFolder(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("path")
	if encoded == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			log.Printf("[PUBLIC-FOLDER] Failed to decode base64 path: %s, error: %v", encoded, err)
			http.Error(w, "invalid path encoding", http.StatusBadRequest)
			return
		}
	}
	folderPath := string(decoded)

	uid := publicWorkspaceUserID(r)
	log.Printf("[PUBLIC-FOLDER] Listing folder: %s for user: %s", folderPath, uid)

	wsURL := getWorkspaceAPIURL() + "/api/documents"
	req, err := http.NewRequestWithContext(r.Context(), "GET", wsURL, nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	q := req.URL.Query()
	q.Set("folder", folderPath)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-User-ID", uid)

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		log.Printf("[PUBLIC-FOLDER] Workspace request failed: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// handlePublicFolderDownload exports a shared folder as a ZIP download.
// GET /api/public/folder/download?path=<base64-encoded-folderpath>&uid=<owner-id>
func (api *StreamingAPI) handlePublicFolderDownload(w http.ResponseWriter, r *http.Request) {
	encoded := r.URL.Query().Get("path")
	if encoded == "" {
		http.Error(w, "path query parameter required", http.StatusBadRequest)
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(encoded)
		if err != nil {
			http.Error(w, "invalid path encoding", http.StatusBadRequest)
			return
		}
	}
	folderPath := string(decoded)

	uid := publicWorkspaceUserID(r)
	log.Printf("[PUBLIC-FOLDER-DOWNLOAD] Exporting folder: %s for user: %s", folderPath, uid)

	// Proxy to workspace export endpoint
	wsURL := getWorkspaceAPIURL() + "/api/workspace/export"
	body, _ := json.Marshal(map[string]string{"workspace_path": folderPath})
	req, err := http.NewRequestWithContext(r.Context(), "POST", wsURL, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", uid)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PUBLIC-FOLDER-DOWNLOAD] Workspace request failed: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[PUBLIC-FOLDER-DOWNLOAD] Workspace returned %d: %s", resp.StatusCode, string(respBody))
		http.Error(w, "export failed", resp.StatusCode)
		return
	}

	// Proxy ZIP response headers and body
	for _, h := range []string{"Content-Type", "Content-Disposition", "Content-Length"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	io.Copy(w, resp.Body)
}

// publicWorkspaceUserID selects the durable workspace owner for public file
// routes. In a single-user deployment, the gateway's JWT subject is an
// internal product label (for example "video-studio"), not another workspace
// owner. Every public asset must therefore resolve to DEFAULT_USER_ID. The
// explicit uid query parameter remains available only in multi-user mode for
// intentional cross-user sharing.
func publicWorkspaceUserID(r *http.Request) string {
	if !IsMultiUserMode() {
		return GetDefaultUserID()
	}
	if uid := r.URL.Query().Get("uid"); uid != "" {
		return uid
	}
	return GetUserIDFromContext(r.Context())
}

// API Key Validation endpoint - validates API keys for supported providers.
// Capabilities endpoint
func (api *StreamingAPI) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	// Get current tracing provider
	tracingProvider := os.Getenv("TRACING_PROVIDER")
	if tracingProvider == "" {
		tracingProvider = "noop"
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"providers":   []string{"bedrock", "openai", "anthropic"},
		"streaming":   true,
		"sse":         true,
		"agent_modes": []string{"multi-agent", "workflow"},
		"tracing": map[string]interface{}{
			"enabled":  tracingProvider != "noop",
			"provider": tracingProvider,
		},
		"workspace":     map[string]interface{}{},
		"servers":       []string{},
		"local_mode":    IsLocalMode(),
		"runtime_debug": runtimeDiagnosticsEnabled(),
		// Live-attach (control-mode) terminal WebSocket transport. True when tmux
		// is new enough for control mode (the manager is constructed). Lets the
		// frontend render the selected live tmux terminal over
		// /api/terminals/{id}/stream instead of the snapshot/replay polling.
		// See docs/refactor/terminal_live_attach_transport.md.
		"terminal_live_attach": runtimeDiagnosticsEnabled() && api.liveAttach != nil,
		// Streaming microphone dictation (pkg/voicestt, voicestt.Status).
		// `available` is a build-time fact: false in a CGO_ENABLED=0 build,
		// where the mic must not be offered at all. `ready` flips once the
		// model has loaded, which on a first run means a ~690MB download after
		// startup. AgentWorks' own composer gates its mic on `available`;
		// product composers keep gating on their profile's
		// runtime.capabilities.voice instead.
		"voice": voiceManager.Status(),
	})
}

// handleCdpCheck checks if Chrome DevTools is reachable on localhost.
func (api *StreamingAPI) handleCdpCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !browser.CDPEnabled() {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
			"supported": false,
			"error":     "CDP is disabled for this server deployment",
		})
		return
	}

	portStr := r.URL.Query().Get("port")
	if portStr == "" {
		portStr = "9222"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
			"error":     "invalid port number",
		})
		return
	}

	result, err := checkLocalChromeCdpVersion(port)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"connected": false,
			"error":     fmt.Sprintf("Cannot reach Chrome CDP /json/version on port %d: %v", port, err),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(result)
}

func checkLocalChromeCdpVersion(port int) (map[string]interface{}, error) {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/json/version", port)
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", endpoint, resp.StatusCode)
	}

	var payload struct {
		Browser              string `json:"Browser"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload.Browser) == "" && strings.TrimSpace(payload.WebSocketDebuggerURL) == "" {
		return nil, fmt.Errorf("%s did not return Chrome DevTools metadata", endpoint)
	}

	return map[string]interface{}{
		"connected": true,
		"browser":   payload.Browser,
		"endpoint":  endpoint,
	}, nil
}

// drainStatus reports what a deploy should wait for before restarting.
func (api *StreamingAPI) drainStatus() map[string]interface{} {
	// The tracker retains finished sessions for 24h; only ones still in the
	// running lifecycle state hold a turn a restart would cut.
	api.activeSessionsMux.RLock()
	active := 0
	for _, session := range api.activeSessions {
		if session != nil && normalizeSessionLifecycleStatus(session.Status) == sessionLifecycleRunning {
			active++
		}
	}
	api.activeSessionsMux.RUnlock()
	// A steered coding-agent turn returns its HTTP request immediately and
	// runs the model in the background, so neither the tracker nor the
	// request count sees it; the runtime coordinator does.
	generating := api.runtimeCoordinator.BusyCount()
	if generating > active {
		active = generating
	}
	inFlight := atomic.LoadInt64(&apiRequestsInFlight)
	return map[string]interface{}{
		"active_sessions":    active,
		"in_flight_requests": inFlight,
		"idle":               active == 0 && inFlight == 0,
	}
}
