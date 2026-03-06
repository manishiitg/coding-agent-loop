// error_handler.go
//
// This file contains error handling strategies for the Agent, including broken pipe recovery,
// connection error handling, and other error recovery mechanisms.
//
// Exported:
//   - BrokenPipeHandler
//   - NewBrokenPipeHandler
//   - IsBrokenPipeError

package mcpagent

import (
	"context"
	"fmt"
	"time"

	"github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpcache"
	"github.com/manishiitg/mcpagent/mcpclient"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"
)

// BrokenPipeHandler handles broken pipe errors by recreating connections and retrying operations
type BrokenPipeHandler struct {
	agent  *Agent
	logger loggerv2.Logger
}

// NewBrokenPipeHandler creates a new broken pipe handler
func NewBrokenPipeHandler(agent *Agent) *BrokenPipeHandler {
	return &BrokenPipeHandler{
		agent:  agent,
		logger: agent.Logger,
	}
}

// IsBrokenPipeError checks if an error is a broken pipe error
// Delegates to mcpclient.IsBrokenPipeError for shared implementation
func IsBrokenPipeError(err error) bool {
	return mcpclient.IsBrokenPipeError(err)
}

// HandleBrokenPipeError handles broken pipe errors by recreating the connection and retrying
func (h *BrokenPipeHandler) HandleBrokenPipeError(
	ctx context.Context,
	toolCall *llmtypes.ToolCall,
	serverName string,
	originalErr error,
	startTime time.Time,
) (*mcp.CallToolResult, time.Duration, error) {

	h.logger.Info("Broken pipe detected, attempting connection recreation",
		loggerv2.String("tool", toolCall.FunctionCall.Name),
		loggerv2.String("server", serverName))

	// Emit broken pipe detection event
	h.emitBrokenPipeEvent(ctx, toolCall, serverName, originalErr)

	var freshClient mcpclient.ClientInterface
	var freshErr error

	if h.agent.SessionID != "" {
		// Session-scoped mode: use the registry so the new connection is tracked
		// and will be cleaned up by CloseAllSessions at shutdown.
		freshClient, freshErr = h.recreateViaRegistry(ctx, serverName)
	} else {
		// Legacy mode: direct close + fresh connection (no registry)
		freshClient, freshErr = h.recreateDirect(ctx, serverName)
	}

	if freshErr != nil {
		h.logger.Error(fmt.Sprintf("🔧 [BROKEN PIPE] Failed to create fresh connection: %v", freshErr), freshErr)
		return nil, time.Since(startTime), freshErr
	}

	// Update the agent's client map with the new connection
	// This ensures future tool calls use the new connection
	h.agent.clientsMu.Lock()
	h.agent.Clients[serverName] = freshClient
	h.agent.clientsMu.Unlock()
	h.logger.Info(fmt.Sprintf("🔧 [BROKEN PIPE] Updated agent's client map with fresh connection for server: %s", serverName),
		loggerv2.String("server", serverName))

	// Retry the tool call once with the fresh connection
	return h.retryToolCall(ctx, toolCall, freshClient, serverName, startTime)
}

// recreateViaRegistry closes the stale connection in the session registry and
// creates a fresh one that the registry tracks. This prevents connection leaks
// because CloseAllSessions will close the replacement connection at shutdown.
func (h *BrokenPipeHandler) recreateViaRegistry(ctx context.Context, serverName string) (mcpclient.ClientInterface, error) {
	// Compute the connection session ID — stateless servers share "global",
	// matching the logic in connection_session.go:170-175.
	connSessionID := h.agent.SessionID
	if serverName != "playwright" && serverName != "agent-browser" {
		connSessionID = "global"
	}

	registry := mcpclient.GetSessionRegistry()

	// Atomically close AND remove the stale entry from the registry.
	h.logger.Info(fmt.Sprintf("🔧 [BROKEN PIPE] Closing stale registry entry for server: %s (session=%s)", serverName, connSessionID),
		loggerv2.String("server", serverName))
	registry.CloseSessionServer(connSessionID, serverName)

	// Load server config so we can pass it to GetOrCreateConnection.
	config, err := mcpclient.LoadMergedConfig(h.agent.configPath, h.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to load MCP config for broken pipe recovery: %w", err)
	}
	serverConfig, err := config.GetServer(serverName)
	if err != nil {
		return nil, fmt.Errorf("server %s not found in config: %w", serverName, err)
	}

	// Apply runtime overrides (matching connection_session.go:149-158)
	if h.agent.RuntimeOverrides != nil {
		if override, hasOverride := h.agent.RuntimeOverrides[serverName]; hasOverride {
			serverConfig = serverConfig.ApplyOverride(override)
		}
	}

	// Apply per-user OAuth token path (matching connection_session.go:161-168)
	if h.agent.UserID != "" && serverConfig.OAuth != nil {
		serverConfig.OAuth.TokenFile = fmt.Sprintf("~/.config/mcpagent/tokens/%s/%s.json", h.agent.UserID, serverName)
	}

	// Create a fresh connection tracked by the registry.
	// The per-key mutex inside the registry prevents concurrent broken pipe
	// handlers from spawning duplicate connections for the same server.
	client, _, err := registry.GetOrCreateConnection(ctx, connSessionID, serverName, serverConfig, h.logger)
	if err != nil {
		return nil, fmt.Errorf("registry GetOrCreateConnection failed: %w", err)
	}
	return client, nil
}

// recreateDirect closes the old connection directly and creates a fresh one
// via mcpcache. Used when there is no session registry (legacy mode).
func (h *BrokenPipeHandler) recreateDirect(ctx context.Context, serverName string) (mcpclient.ClientInterface, error) {
	h.agent.clientsMu.Lock()
	if oldClient, exists := h.agent.Clients[serverName]; exists && oldClient != nil {
		h.logger.Info(fmt.Sprintf("🔧 [BROKEN PIPE] Closing old broken connection for server: %s", serverName),
			loggerv2.String("server", serverName))
		_ = oldClient.Close()
	}
	h.agent.clientsMu.Unlock()

	return mcpcache.GetFreshConnection(ctx, serverName, h.agent.configPath, h.logger)
}

// retryToolCall retries a tool call with a fresh connection
func (h *BrokenPipeHandler) retryToolCall(
	ctx context.Context,
	toolCall *llmtypes.ToolCall,
	client mcpclient.ClientInterface,
	serverName string,
	startTime time.Time,
) (*mcp.CallToolResult, time.Duration, error) {

	h.logger.Info("Retrying tool call with fresh connection",
		loggerv2.String("tool", toolCall.FunctionCall.Name))

	// Parse the tool arguments from JSON string to map
	retryArgs, parseErr := mcpclient.ParseToolArguments(toolCall.FunctionCall.Arguments)
	if parseErr != nil {
		h.logger.Error(fmt.Sprintf("🔧 [BROKEN PIPE] Failed to parse tool arguments: %v", parseErr), parseErr)
		return nil, time.Since(startTime), parseErr
	}

	// Create a timeout context for the retry
	retryCtx, retryCancel := context.WithTimeout(ctx, 30*time.Second)
	defer retryCancel()

	// Execute the retry
	retryResult, retryErr := client.CallTool(retryCtx, toolCall.FunctionCall.Name, retryArgs)
	retryDuration := time.Since(startTime)

	if retryErr == nil {
		h.logger.Info("Retry successful",
			loggerv2.String("tool", toolCall.FunctionCall.Name),
			loggerv2.String("duration", retryDuration.String()))
		h.emitRetrySuccessEvent(ctx, toolCall, serverName, retryDuration)
		return retryResult, retryDuration, nil
	}

	h.logger.Error("Retry failed", retryErr,
		loggerv2.String("tool", toolCall.FunctionCall.Name))
	h.emitRetryFailureEvent(ctx, toolCall, serverName, retryErr, retryDuration)
	return nil, retryDuration, retryErr
}

// emitBrokenPipeEvent emits a broken pipe detection event
func (h *BrokenPipeHandler) emitBrokenPipeEvent(ctx context.Context, toolCall *llmtypes.ToolCall, serverName string, originalErr error) {
	brokenPipeEvent := &events.GenericEventData{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Data: map[string]interface{}{
			"error_type":    "broken_pipe_detected",
			"tool_name":     toolCall.FunctionCall.Name,
			"server_name":   serverName,
			"tool_call_id":  toolCall.ID,
			"error_message": originalErr.Error(),
			"operation":     "broken_pipe_connection_recreation",
		},
	}
	h.agent.EmitTypedEvent(ctx, brokenPipeEvent)
}

// emitRetrySuccessEvent emits a successful retry event
func (h *BrokenPipeHandler) emitRetrySuccessEvent(ctx context.Context, toolCall *llmtypes.ToolCall, serverName string, duration time.Duration) {
	retrySuccessEvent := &events.GenericEventData{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Data: map[string]interface{}{
			"error_type":     "broken_pipe_retry_success",
			"tool_name":      toolCall.FunctionCall.Name,
			"server_name":    serverName,
			"tool_call_id":   toolCall.ID,
			"retry_duration": duration.String(),
			"operation":      "broken_pipe_retry_success",
		},
	}
	h.agent.EmitTypedEvent(ctx, retrySuccessEvent)
}

// emitRetryFailureEvent emits a failed retry event
func (h *BrokenPipeHandler) emitRetryFailureEvent(ctx context.Context, toolCall *llmtypes.ToolCall, serverName string, retryErr error, duration time.Duration) {
	retryFailureEvent := &events.GenericEventData{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Data: map[string]interface{}{
			"error_type":     "broken_pipe_retry_failure",
			"tool_name":      toolCall.FunctionCall.Name,
			"server_name":    serverName,
			"tool_call_id":   toolCall.ID,
			"retry_duration": duration.String(),
			"retry_error":    retryErr.Error(),
			"operation":      "broken_pipe_retry_failure",
		},
	}
	h.agent.EmitTypedEvent(ctx, retryFailureEvent)
}

// ErrorRecoveryHandler provides a unified interface for different error recovery strategies
type ErrorRecoveryHandler struct {
	brokenPipeHandler *BrokenPipeHandler
	logger            loggerv2.Logger
}

// NewErrorRecoveryHandler creates a new error recovery handler
func NewErrorRecoveryHandler(agent *Agent) *ErrorRecoveryHandler {
	return &ErrorRecoveryHandler{
		brokenPipeHandler: NewBrokenPipeHandler(agent),
		logger:            agent.Logger,
	}
}

// HandleError attempts to recover from various types of errors
func (h *ErrorRecoveryHandler) HandleError(
	ctx context.Context,
	toolCall *llmtypes.ToolCall,
	serverName string,
	originalErr error,
	startTime time.Time,
	isCustomTool bool,
	isVirtualTool bool,
) (*mcp.CallToolResult, time.Duration, bool, error) {

	// Only handle errors for regular MCP tools (not custom or virtual tools)
	if isCustomTool || isVirtualTool {
		return nil, time.Since(startTime), false, originalErr
	}

	// Handle broken pipe errors
	if IsBrokenPipeError(originalErr) {
		result, duration, err := h.brokenPipeHandler.HandleBrokenPipeError(ctx, toolCall, serverName, originalErr, startTime)
		return result, duration, true, err
	}

	// No recovery strategy available for this error type
	return nil, time.Since(startTime), false, originalErr
}
