package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/manishiitg/mcpagent/agent/codeexec"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpcache"
	"github.com/manishiitg/mcpagent/mcpclient"
)

// --- REQUEST/RESPONSE TYPES ---

// MCPExecuteRequest represents a request to execute an MCP tool
type MCPExecuteRequest struct {
	Server    string                 `json:"server"`               // MCP server name (e.g., "aws", "gdrive")
	Tool      string                 `json:"tool"`                 // Tool name (e.g., "list_buckets")
	Args      map[string]interface{} `json:"args"`                 // Tool arguments
	SessionID string                 `json:"session_id,omitempty"` // Optional: MCP session ID for connection reuse
}

// MCPExecuteResponse represents the response from an MCP tool execution
type MCPExecuteResponse struct {
	Success bool   `json:"success"`
	Result  string `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

// CustomExecuteRequest represents a request to execute a custom tool
type CustomExecuteRequest struct {
	Tool      string                 `json:"tool"`                 // Tool name (e.g., "read_workspace_file")
	Args      map[string]interface{} `json:"args"`                 // Tool arguments
	SessionID string                 `json:"session_id,omitempty"` // Optional: Session ID for scoping custom tools (prevents cross-workflow contamination)
}

// CustomExecuteResponse represents the response from a custom tool execution
type CustomExecuteResponse struct {
	Success bool   `json:"success"`
	Result  string `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

// VirtualExecuteRequest represents a request to execute a virtual tool
type VirtualExecuteRequest struct {
	Tool string                 `json:"tool"` // Tool name (e.g., "discover_code_structure")
	Args map[string]interface{} `json:"args"` // Tool arguments
}

// VirtualExecuteResponse represents the response from a virtual tool execution
type VirtualExecuteResponse struct {
	Success bool   `json:"success"`
	Result  string `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

// --- EXECUTOR HANDLERS ---

// ExecutorHandlers provides HTTP handlers for tool execution endpoints.
// Use NewExecutorHandlers to create and attach to your HTTP mux.
type ExecutorHandlers struct {
	configPath string
	logger     loggerv2.Logger
}

// NewExecutorHandlers creates a new ExecutorHandlers instance.
// configPath: path to the MCP servers configuration file
// logger: logger instance for logging
func NewExecutorHandlers(configPath string, logger loggerv2.Logger) *ExecutorHandlers {
	if logger == nil {
		logger = loggerv2.NewNoop()
	}
	return &ExecutorHandlers{
		configPath: configPath,
		logger:     logger,
	}
}

// HandleMCPExecute handles the /api/mcp/execute endpoint
// POST /api/mcp/execute
// Body: {"server": "aws", "tool": "list_buckets", "args": {...}}
// Response: {"success": true, "result": "..."}
func (h *ExecutorHandlers) HandleMCPExecute(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse request
	var req MCPExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("Failed to decode MCP execute request", loggerv2.Error(err))
		_ = json.NewEncoder(w).Encode(MCPExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
			Success: false,
			Error:   fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	h.logger.Info("🔧 MCP Execute request",
		loggerv2.String("server", req.Server),
		loggerv2.String("tool", req.Tool),
		loggerv2.String("session_id", req.SessionID))

	// Validate request
	if req.Server == "" {
		_ = json.NewEncoder(w).Encode(MCPExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
			Success: false,
			Error:   "server parameter is required",
		})
		return
	}

	if req.Tool == "" {
		_ = json.NewEncoder(w).Encode(MCPExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
			Success: false,
			Error:   "tool parameter is required",
		})
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	// 🔧 STRATEGY: Try multiple connection sources in priority order
	// 1. Session registry (if session_id provided) - enables Playwright browser reuse
	// 2. Codeexec global registry - has session-aware connections from agent initialization
	// 3. mcpcache - creates new connection as fallback

	var client mcpclient.ClientInterface
	var err error

	// PRIORITY 1: If session_id is provided, try session registry first
	// This is the primary mechanism for connection reuse (e.g., Playwright browser sharing)
	if req.SessionID != "" {
		registry := mcpclient.GetSessionRegistry()

		// Debug: List all available sessions
		allSessions := registry.ListSessions()
		h.logger.Info("🔍 [SESSION DEBUG] Available sessions in registry",
			loggerv2.String("requested_session_id", req.SessionID),
			loggerv2.Any("all_sessions", allSessions),
			loggerv2.Int("session_count", len(allSessions)))

		sessionConns := registry.GetSessionConnections(req.SessionID)

		// Debug: List all connections for this session
		var connServers []string
		for serverName := range sessionConns {
			connServers = append(connServers, serverName)
		}
		h.logger.Info("🔍 [SESSION DEBUG] Connections for session",
			loggerv2.String("session_id", req.SessionID),
			loggerv2.Any("available_servers", connServers),
			loggerv2.String("requested_server", req.Server))

		if existingClient, exists := sessionConns[req.Server]; exists {
			h.logger.Info("✅ Using session registry connection (session-aware)",
				loggerv2.String("session_id", req.SessionID),
				loggerv2.String("server", req.Server))
			client = existingClient
		} else {
			h.logger.Info("🔄 Session registry miss, trying codeexec registry",
				loggerv2.String("session_id", req.SessionID),
				loggerv2.String("server", req.Server),
				loggerv2.Any("available_servers", connServers))
		}
	} else {
		h.logger.Info("⚠️ No session_id provided in request, skipping session registry")
	}

	// PRIORITY 2: Try codeexec global registry if no session connection found
	if client == nil {
		resultStr, callErr := codeexec.CallMCPTool(ctx, req.Tool, req.Args)
		if callErr == nil {
			h.logger.Info("✅ Tool executed via codeexec registry",
				loggerv2.String("tool", req.Tool),
				loggerv2.Int("result_length", len(resultStr)))
			_ = json.NewEncoder(w).Encode(MCPExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
				Success: true,
				Result:  resultStr,
			})
			return
		}
		h.logger.Info("🔄 Codeexec registry miss, falling back to mcpcache",
			loggerv2.String("tool", req.Tool),
			loggerv2.String("server", req.Server),
			loggerv2.String("registry_error", callErr.Error()))
	}

	// PRIORITY 3: Fall back to mcpcache (creates new connection)
	// ⚠️ WARNING: This creates a NEW connection which opens a NEW browser for Playwright!
	if client == nil {
		h.logger.Warn("⚠️ [SESSION MISS] Falling back to mcpcache - will create NEW connection (new browser for Playwright!)",
			loggerv2.String("server", req.Server),
			loggerv2.String("session_id", req.SessionID))
		client, err = GetOrCreateMCPClient(ctx, req.Server, h.configPath, h.logger)
		if err != nil {
			h.logger.Error("Failed to get MCP client", err, loggerv2.String("server", req.Server))
			_ = json.NewEncoder(w).Encode(MCPExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
				Success: false,
				Error:   fmt.Sprintf("Failed to connect to server %s: %v", req.Server, err),
			})
			return
		}
		h.logger.Warn("⚠️ [SESSION MISS] Created new connection via mcpcache",
			loggerv2.String("server", req.Server))
	}

	// Execute tool
	h.logger.Info("🚀 Executing tool via direct connection",
		loggerv2.String("tool", req.Tool),
		loggerv2.String("server", req.Server))
	result, err := client.CallTool(ctx, req.Tool, req.Args)

	// 🔧 BROKEN PIPE DETECTION AND RETRY
	if err != nil && mcpclient.IsBrokenPipeError(err) {
		h.logger.Info("🔧 [BROKEN PIPE] Detected, getting fresh connection...",
			loggerv2.String("tool", req.Tool),
			loggerv2.String("server", req.Server))

		// Get fresh connection using shared function (bypasses cache by invalidating)
		freshClient, freshErr := mcpcache.GetFreshConnection(ctx, req.Server, h.configPath, h.logger)
		if freshErr == nil {
			h.logger.Info("🔧 [BROKEN PIPE] Retrying with fresh connection...",
				loggerv2.String("tool", req.Tool))
			result, err = freshClient.CallTool(ctx, req.Tool, req.Args)
			if err == nil {
				h.logger.Info("🔧 [BROKEN PIPE] Retry successful",
					loggerv2.String("tool", req.Tool))
			} else {
				h.logger.Error("🔧 [BROKEN PIPE] Retry failed", err,
					loggerv2.String("tool", req.Tool))
			}
		} else {
			h.logger.Error("🔧 [BROKEN PIPE] Failed to get fresh connection", freshErr,
				loggerv2.String("server", req.Server))
		}
	}

	if err != nil {
		h.logger.Error("Tool execution failed", err,
			loggerv2.String("tool", req.Tool),
			loggerv2.String("server", req.Server))
		_ = json.NewEncoder(w).Encode(MCPExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
			Success: false,
			Error:   fmt.Sprintf("Tool execution failed: %v", err),
		})
		return
	}

	// Convert result to string
	resultStr := ConvertMCPResultToString(result)

	h.logger.Info("✅ Tool executed successfully",
		loggerv2.String("tool", req.Tool),
		loggerv2.Int("result_length", len(resultStr)))

	// Return success response
	_ = json.NewEncoder(w).Encode(MCPExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
		Success: true,
		Result:  resultStr,
	})
}

// HandleCustomExecute handles the /api/custom/execute endpoint
// POST /api/custom/execute
// Body: {"tool": "read_workspace_file", "args": {...}}
// Response: {"success": true, "result": "..."}
func (h *ExecutorHandlers) HandleCustomExecute(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse request
	var req CustomExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("Failed to decode custom execute request", loggerv2.Error(err))
		_ = json.NewEncoder(w).Encode(CustomExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
			Success: false,
			Error:   fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	h.logger.Info("🔧 Custom Execute request",
		loggerv2.String("tool", req.Tool),
		loggerv2.String("session_id", req.SessionID))

	// Validate request
	if req.Tool == "" {
		_ = json.NewEncoder(w).Encode(CustomExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
			Success: false,
			Error:   "tool parameter is required",
		})
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	// Execute custom tool using codeexec registry (session-scoped to prevent cross-workflow contamination)
	h.logger.Info("🚀 Executing custom tool",
		loggerv2.String("tool", req.Tool),
		loggerv2.String("session_id", req.SessionID))
	result, err := codeexec.CallCustomToolWithSession(ctx, req.SessionID, req.Tool, req.Args)
	if err != nil {
		h.logger.Error("Custom tool execution failed", err, loggerv2.String("tool", req.Tool))
		_ = json.NewEncoder(w).Encode(CustomExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
			Success: false,
			Error:   fmt.Sprintf("Custom tool execution failed: %v", err),
		})
		return
	}

	h.logger.Info("✅ Custom tool executed successfully",
		loggerv2.String("tool", req.Tool),
		loggerv2.Int("result_length", len(result)))

	// Return success response
	_ = json.NewEncoder(w).Encode(CustomExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
		Success: true,
		Result:  result,
	})
}

// HandleVirtualExecute handles the /api/virtual/execute endpoint
// POST /api/virtual/execute
// Body: {"tool": "discover_code_structure", "args": {...}}
// Response: {"success": true, "result": "..."}
func (h *ExecutorHandlers) HandleVirtualExecute(w http.ResponseWriter, r *http.Request) {
	// Enable CORS
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse request
	var req VirtualExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("Failed to decode virtual execute request", loggerv2.Error(err))
		_ = json.NewEncoder(w).Encode(VirtualExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
			Success: false,
			Error:   fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	h.logger.Info("🔧 Virtual Execute request", loggerv2.String("tool", req.Tool))

	// Validate request
	if req.Tool == "" {
		_ = json.NewEncoder(w).Encode(VirtualExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
			Success: false,
			Error:   "tool parameter is required",
		})
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	// Execute virtual tool using codeexec registry
	h.logger.Info("🚀 Executing virtual tool", loggerv2.String("tool", req.Tool))
	result, err := codeexec.CallVirtualTool(ctx, req.Tool, req.Args)
	if err != nil {
		h.logger.Error("Virtual tool execution failed", err, loggerv2.String("tool", req.Tool))
		_ = json.NewEncoder(w).Encode(VirtualExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
			Success: false,
			Error:   fmt.Sprintf("Virtual tool execution failed: %v", err),
		})
		return
	}

	h.logger.Info("✅ Virtual tool executed successfully",
		loggerv2.String("tool", req.Tool),
		loggerv2.Int("result_length", len(result)))

	// Return success response
	_ = json.NewEncoder(w).Encode(VirtualExecuteResponse{ //nolint:gosec // JSON encoding errors are non-critical in HTTP handlers
		Success: true,
		Result:  result,
	})
}
