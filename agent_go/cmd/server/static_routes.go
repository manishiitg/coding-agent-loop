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
	"time"

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

	// Use uid from query param (owner's ID for cross-user sharing), fall back to auth context
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		uid = GetUserIDFromContext(r.Context())
	}
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

	resp, err := workspaceHTTPClient.Do(req)
	if err != nil {
		log.Printf("[PUBLIC-FILE] Workspace request failed: %v", err)
		http.Error(w, "workspace unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[PUBLIC-FILE] Workspace returned %d: %s", resp.StatusCode, string(body))
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	// Proxy content-type and body — force inline display (no download)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Content-Disposition", "inline")
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

	// Use uid from query param (owner's ID for cross-user sharing), fall back to auth context
	uid := r.URL.Query().Get("uid")
	if uid == "" {
		uid = GetUserIDFromContext(r.Context())
	}
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

	uid := r.URL.Query().Get("uid")
	if uid == "" {
		uid = GetUserIDFromContext(r.Context())
	}
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
		"workspace":  map[string]interface{}{},
		"servers":    []string{},
		"local_mode": IsLocalMode(),
		// Live-attach (control-mode) terminal WebSocket transport. True when tmux
		// is new enough for control mode (the manager is constructed). Lets the
		// frontend render the selected live tmux terminal over
		// /api/terminals/{id}/stream instead of the snapshot/replay polling.
		// See docs/refactor/terminal_live_attach_transport.md.
		"terminal_live_attach": api.liveAttach != nil,
	})
}

// handleCdpCheck checks if Chrome DevTools is reachable on localhost.
func (api *StreamingAPI) handleCdpCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

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
