package executor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
)

// HandlePerToolMCPRequest is the public entry point for per-tool MCP requests.
// It handles requests to /tools/mcp/{server}/{tool}, extracting args from the body
// and delegating to the standard MCP execution logic.
func (h *ExecutorHandlers) HandlePerToolMCPRequest(w http.ResponseWriter, r *http.Request, server, tool string) {
	h.handlePerToolMCP(w, r, server, tool)
}

// HandlePerToolCustomRequest is the public entry point for per-tool custom requests.
// It handles requests to /tools/custom/{tool}, extracting args from the body
// and delegating to the standard custom tool execution logic.
func (h *ExecutorHandlers) HandlePerToolCustomRequest(w http.ResponseWriter, r *http.Request, tool string) {
	h.handlePerToolCustom(w, r, tool)
}

// handlePerToolMCP handles requests to /tools/mcp/{server}/{tool}.
// It extracts the tool arguments from the request body and delegates to the standard MCP execution logic.
func (h *ExecutorHandlers) handlePerToolMCP(w http.ResponseWriter, r *http.Request, server, tool string) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse request body as tool arguments
	var args map[string]interface{}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			h.logger.Warn("Failed to decode per-tool MCP request body", loggerv2.Error(err))
			_ = json.NewEncoder(w).Encode(MCPExecuteResponse{ //nolint:gosec
				Success: false,
				Error:   fmt.Sprintf("Invalid request body: %v", err),
			})
			return
		}
	}
	if args == nil {
		args = make(map[string]interface{})
	}

	// Extract optional session_id from args (and remove it so it's not passed as tool arg)
	sessionID := ""
	if sid, ok := args["session_id"].(string); ok {
		sessionID = sid
		delete(args, "session_id")
	}

	// Server-side fallback: if session_id is empty in body, check X-Session-ID header.
	// This header is set automatically when requests come through session-scoped routes
	// (/s/{session_id}/tools/...), so session_id is enforced without agent cooperation.
	if sessionID == "" {
		if hdr := r.Header.Get("X-Session-ID"); hdr != "" {
			sessionID = hdr
		}
	}

	h.logger.Info("Per-tool MCP request",
		loggerv2.String("server", server),
		loggerv2.String("tool", tool),
		loggerv2.String("session_id", sessionID))

	// Build the standard MCPExecuteRequest and reuse the full handler logic.
	// URL path segments are sanitized (hyphens→underscores via SanitizePathSegment).
	// The original server/tool names may have hyphens, so we try both forms.
	wrappedBody := MCPExecuteRequest{
		Server:    server,
		Tool:      tool,
		Args:      args,
		SessionID: sessionID,
	}

	bodyBytes, err := json.Marshal(wrappedBody)
	if err != nil {
		_ = json.NewEncoder(w).Encode(MCPExecuteResponse{ //nolint:gosec
			Success: false,
			Error:   fmt.Sprintf("Internal error: %v", err),
		})
		return
	}

	// Create a new request with the wrapped body
	newReq, err := http.NewRequestWithContext(r.Context(), "POST", r.URL.String(), strings.NewReader(string(bodyBytes)))
	if err != nil {
		_ = json.NewEncoder(w).Encode(MCPExecuteResponse{ //nolint:gosec
			Success: false,
			Error:   fmt.Sprintf("Internal error: %v", err),
		})
		return
	}
	newReq.Header.Set("Content-Type", "application/json")

	// Try the sanitized name first. If it fails with a server connection error
	// and the name contains underscores, retry with hyphens (reverse of sanitization).
	desanitizedServer := strings.ReplaceAll(server, "_", "-")
	if desanitizedServer == server {
		// No underscores to desanitize — just delegate directly
		h.HandleMCPExecute(w, newReq)
		return
	}

	// Name has underscores that might be sanitized hyphens — try with recorder first
	rec := httptest.NewRecorder()
	h.HandleMCPExecute(rec, newReq)

	// Check if the response indicates a server-not-found error
	var resp MCPExecuteResponse
	shouldRetry := false
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err == nil {
		if !resp.Success && strings.Contains(resp.Error, "Failed to connect to server") {
			shouldRetry = true
		}
	}

	if shouldRetry {
		h.logger.Info("Retrying per-tool MCP request with desanitized server name",
			loggerv2.String("original", server),
			loggerv2.String("desanitized", desanitizedServer))

		wrappedBody.Server = desanitizedServer
		retryBytes, _ := json.Marshal(wrappedBody)
		retryReq, retryErr := http.NewRequestWithContext(r.Context(), "POST", r.URL.String(), strings.NewReader(string(retryBytes)))
		if retryErr == nil {
			retryReq.Header.Set("Content-Type", "application/json")
			h.HandleMCPExecute(w, retryReq)
			return
		}
	}

	// Write the original (first attempt) response
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes()) //nolint:gosec
}

// handlePerToolCustom handles requests to /tools/custom/{tool}.
func (h *ExecutorHandlers) handlePerToolCustom(w http.ResponseWriter, r *http.Request, tool string) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse request body as tool arguments
	var args map[string]interface{}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			h.logger.Warn("Failed to decode per-tool custom request body", loggerv2.Error(err))
			_ = json.NewEncoder(w).Encode(CustomExecuteResponse{ //nolint:gosec
				Success: false,
				Error:   fmt.Sprintf("Invalid request body: %v", err),
			})
			return
		}
	}
	if args == nil {
		args = make(map[string]interface{})
	}

	// Extract optional session_id
	sessionID := ""
	if sid, ok := args["session_id"].(string); ok {
		sessionID = sid
		delete(args, "session_id")
	}

	// Server-side fallback: check X-Session-ID header if body doesn't have session_id
	if sessionID == "" {
		if hdr := r.Header.Get("X-Session-ID"); hdr != "" {
			sessionID = hdr
		}
	}

	h.logger.Info("Per-tool custom request",
		loggerv2.String("tool", tool),
		loggerv2.String("session_id", sessionID))

	// Build the standard CustomExecuteRequest and reuse the full handler logic
	wrappedBody := CustomExecuteRequest{
		Tool:      tool,
		Args:      args,
		SessionID: sessionID,
	}

	bodyBytes, err := json.Marshal(wrappedBody)
	if err != nil {
		_ = json.NewEncoder(w).Encode(CustomExecuteResponse{ //nolint:gosec
			Success: false,
			Error:   fmt.Sprintf("Internal error: %v", err),
		})
		return
	}

	newReq, err := http.NewRequestWithContext(r.Context(), "POST", r.URL.String(), strings.NewReader(string(bodyBytes)))
	if err != nil {
		_ = json.NewEncoder(w).Encode(CustomExecuteResponse{ //nolint:gosec
			Success: false,
			Error:   fmt.Sprintf("Internal error: %v", err),
		})
		return
	}
	newReq.Header.Set("Content-Type", "application/json")

	h.HandleCustomExecute(w, newReq)
}

// HandlePerToolVirtualRequest is the public entry point for per-tool virtual requests.
// It handles requests to /tools/virtual/{tool}, extracting args from the body
// and delegating to the standard virtual tool execution logic.
func (h *ExecutorHandlers) HandlePerToolVirtualRequest(w http.ResponseWriter, r *http.Request, tool string) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse request body as tool arguments
	var args map[string]interface{}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			h.logger.Warn("Failed to decode per-tool virtual request body", loggerv2.Error(err))
			_ = json.NewEncoder(w).Encode(VirtualExecuteResponse{ //nolint:gosec
				Success: false,
				Error:   fmt.Sprintf("Invalid request body: %v", err),
			})
			return
		}
	}
	if args == nil {
		args = make(map[string]interface{})
	}

	h.logger.Info("Per-tool virtual request", loggerv2.String("tool", tool))

	// Build the standard VirtualExecuteRequest and reuse the full handler logic
	wrappedBody := VirtualExecuteRequest{
		Tool: tool,
		Args: args,
	}

	bodyBytes, err := json.Marshal(wrappedBody)
	if err != nil {
		_ = json.NewEncoder(w).Encode(VirtualExecuteResponse{ //nolint:gosec
			Success: false,
			Error:   fmt.Sprintf("Internal error: %v", err),
		})
		return
	}

	newReq, err := http.NewRequestWithContext(r.Context(), "POST", r.URL.String(), strings.NewReader(string(bodyBytes)))
	if err != nil {
		_ = json.NewEncoder(w).Encode(VirtualExecuteResponse{ //nolint:gosec
			Success: false,
			Error:   fmt.Sprintf("Internal error: %v", err),
		})
		return
	}
	newReq.Header.Set("Content-Type", "application/json")

	h.HandleVirtualExecute(w, newReq)
}
