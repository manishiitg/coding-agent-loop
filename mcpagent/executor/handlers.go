package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"mcpagent/agent/codeexec"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpcache"
	"mcpagent/mcpclient"
)

// --- REQUEST/RESPONSE TYPES ---

// MCPExecuteRequest represents a request to execute an MCP tool
type MCPExecuteRequest struct {
	Server string                 `json:"server"` // MCP server name (e.g., "aws", "gdrive")
	Tool   string                 `json:"tool"`   // Tool name (e.g., "list_buckets")
	Args   map[string]interface{} `json:"args"`   // Tool arguments
}

// MCPExecuteResponse represents the response from an MCP tool execution
type MCPExecuteResponse struct {
	Success bool   `json:"success"`
	Result  string `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

// CustomExecuteRequest represents a request to execute a custom tool
type CustomExecuteRequest struct {
	Tool string                 `json:"tool"` // Tool name (e.g., "read_workspace_file")
	Args map[string]interface{} `json:"args"` // Tool arguments
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
		json.NewEncoder(w).Encode(MCPExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	h.logger.Info("🔧 MCP Execute request",
		loggerv2.String("server", req.Server),
		loggerv2.String("tool", req.Tool))

	// Validate request
	if req.Server == "" {
		json.NewEncoder(w).Encode(MCPExecuteResponse{
			Success: false,
			Error:   "server parameter is required",
		})
		return
	}

	if req.Tool == "" {
		json.NewEncoder(w).Encode(MCPExecuteResponse{
			Success: false,
			Error:   "tool parameter is required",
		})
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	// Get or create MCP client for this server
	client, err := GetOrCreateMCPClient(ctx, req.Server, h.configPath, h.logger)
	if err != nil {
		h.logger.Error("Failed to get MCP client", err, loggerv2.String("server", req.Server))
		json.NewEncoder(w).Encode(MCPExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to connect to server %s: %v", req.Server, err),
		})
		return
	}

	// Execute tool
	h.logger.Info("🚀 Executing tool",
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
		json.NewEncoder(w).Encode(MCPExecuteResponse{
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
	json.NewEncoder(w).Encode(MCPExecuteResponse{
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
		json.NewEncoder(w).Encode(CustomExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	h.logger.Info("🔧 Custom Execute request", loggerv2.String("tool", req.Tool))

	// Validate request
	if req.Tool == "" {
		json.NewEncoder(w).Encode(CustomExecuteResponse{
			Success: false,
			Error:   "tool parameter is required",
		})
		return
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()

	// Execute custom tool using codeexec registry
	h.logger.Info("🚀 Executing custom tool", loggerv2.String("tool", req.Tool))
	result, err := codeexec.CallCustomTool(ctx, req.Tool, req.Args)
	if err != nil {
		h.logger.Error("Custom tool execution failed", err, loggerv2.String("tool", req.Tool))
		json.NewEncoder(w).Encode(CustomExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("Custom tool execution failed: %v", err),
		})
		return
	}

	h.logger.Info("✅ Custom tool executed successfully",
		loggerv2.String("tool", req.Tool),
		loggerv2.Int("result_length", len(result)))

	// Return success response
	json.NewEncoder(w).Encode(CustomExecuteResponse{
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
		json.NewEncoder(w).Encode(VirtualExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("Invalid request body: %v", err),
		})
		return
	}

	h.logger.Info("🔧 Virtual Execute request", loggerv2.String("tool", req.Tool))

	// Validate request
	if req.Tool == "" {
		json.NewEncoder(w).Encode(VirtualExecuteResponse{
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
		json.NewEncoder(w).Encode(VirtualExecuteResponse{
			Success: false,
			Error:   fmt.Sprintf("Virtual tool execution failed: %v", err),
		})
		return
	}

	h.logger.Info("✅ Virtual tool executed successfully",
		loggerv2.String("tool", req.Tool),
		loggerv2.Int("result_length", len(result)))

	// Return success response
	json.NewEncoder(w).Encode(VirtualExecuteResponse{
		Success: true,
		Result:  result,
	})
}
