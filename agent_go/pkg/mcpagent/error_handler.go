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
	"strings"
	"time"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpclient"

	"mcp-agent/agent_go/internal/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"
)

// BrokenPipeHandler handles broken pipe errors by recreating connections and retrying operations
type BrokenPipeHandler struct {
	agent  *Agent
	logger utils.ExtendedLogger
}

// NewBrokenPipeHandler creates a new broken pipe handler
func NewBrokenPipeHandler(agent *Agent) *BrokenPipeHandler {
	return &BrokenPipeHandler{
		agent:  agent,
		logger: getLogger(agent),
	}
}

// IsBrokenPipeError checks if an error is a broken pipe error
func IsBrokenPipeError(err error) bool {
	if err == nil {
		return false
	}
	errorMessage := err.Error()
	return strings.Contains(errorMessage, "Broken pipe") ||
		strings.Contains(errorMessage, "broken pipe") ||
		strings.Contains(errorMessage, "[Errno 32]") ||
		strings.Contains(errorMessage, "EOF") ||
		strings.Contains(errorMessage, "connection reset")
}

// HandleBrokenPipeError handles broken pipe errors by recreating the connection and retrying
func (h *BrokenPipeHandler) HandleBrokenPipeError(
	ctx context.Context,
	toolCall *llmtypes.ToolCall,
	serverName string,
	originalErr error,
	startTime time.Time,
) (*mcp.CallToolResult, error, time.Duration) {

	h.logger.Infof("🔧 [BROKEN PIPE DETECTED] Tool: %s, Server: %s - Attempting immediate connection recreation",
		toolCall.FunctionCall.Name, serverName)

	// Emit broken pipe detection event
	h.emitBrokenPipeEvent(ctx, toolCall, serverName, originalErr)

	// Create a fresh connection immediately
	h.logger.Infof("🔧 [BROKEN PIPE] Creating fresh connection for server: %s", serverName)
	freshClient, freshErr := h.agent.createOnDemandConnection(ctx, serverName)
	if freshErr != nil {
		h.logger.Errorf("🔧 [BROKEN PIPE] Failed to create fresh connection: %v", freshErr)
		return nil, freshErr, time.Since(startTime)
	}

	h.logger.Infof("🔧 [BROKEN PIPE] Successfully created fresh connection for server: %s", serverName)

	// Retry the tool call once with the fresh connection
	return h.retryToolCall(ctx, toolCall, freshClient, serverName, startTime)
}

// retryToolCall retries a tool call with a fresh connection
func (h *BrokenPipeHandler) retryToolCall(
	ctx context.Context,
	toolCall *llmtypes.ToolCall,
	client mcpclient.ClientInterface,
	serverName string,
	startTime time.Time,
) (*mcp.CallToolResult, error, time.Duration) {

	h.logger.Infof("🔧 [BROKEN PIPE] Retrying tool call '%s' with fresh connection", toolCall.FunctionCall.Name)

	// Parse the tool arguments from JSON string to map
	retryArgs, parseErr := mcpclient.ParseToolArguments(toolCall.FunctionCall.Arguments)
	if parseErr != nil {
		h.logger.Errorf("🔧 [BROKEN PIPE] Failed to parse tool arguments: %v", parseErr)
		return nil, parseErr, time.Since(startTime)
	}

	// Create a timeout context for the retry
	retryCtx, retryCancel := context.WithTimeout(ctx, 30*time.Second)
	defer retryCancel()

	// Execute the retry
	retryResult, retryErr := client.CallTool(retryCtx, toolCall.FunctionCall.Name, retryArgs)
	retryDuration := time.Since(startTime)

	if retryErr == nil {
		h.logger.Infof("🔧 [BROKEN PIPE] Retry successful for tool '%s' after %v", toolCall.FunctionCall.Name, retryDuration)
		h.emitRetrySuccessEvent(ctx, toolCall, serverName, retryDuration)
		return retryResult, nil, retryDuration
	}

	h.logger.Errorf("🔧 [BROKEN PIPE] Retry failed for tool '%s': %v", toolCall.FunctionCall.Name, retryErr)
	h.emitRetryFailureEvent(ctx, toolCall, serverName, retryErr, retryDuration)
	return nil, retryErr, retryDuration
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
	logger            utils.ExtendedLogger
}

// NewErrorRecoveryHandler creates a new error recovery handler
func NewErrorRecoveryHandler(agent *Agent) *ErrorRecoveryHandler {
	return &ErrorRecoveryHandler{
		brokenPipeHandler: NewBrokenPipeHandler(agent),
		logger:            getLogger(agent),
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
) (*mcp.CallToolResult, error, time.Duration, bool) {

	// Only handle errors for regular MCP tools (not custom or virtual tools)
	if isCustomTool || isVirtualTool {
		return nil, originalErr, time.Since(startTime), false
	}

	// Handle broken pipe errors
	if IsBrokenPipeError(originalErr) {
		result, err, duration := h.brokenPipeHandler.HandleBrokenPipeError(ctx, toolCall, serverName, originalErr, startTime)
		return result, err, duration, true
	}

	// No recovery strategy available for this error type
	return nil, originalErr, time.Since(startTime), false
}
