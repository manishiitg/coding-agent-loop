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
	"time"

	"mcpagent/events"
	"mcpagent/logger"
	"mcpagent/mcpcache"
	"mcpagent/mcpclient"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"
)

// BrokenPipeHandler handles broken pipe errors by recreating connections and retrying operations
type BrokenPipeHandler struct {
	agent  *Agent
	logger logger.ExtendedLogger
}

// NewBrokenPipeHandler creates a new broken pipe handler
func NewBrokenPipeHandler(agent *Agent) *BrokenPipeHandler {
	return &BrokenPipeHandler{
		agent:  agent,
		logger: getLogger(agent),
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

	h.logger.Infof("🔧 [BROKEN PIPE DETECTED] Tool: %s, Server: %s - Attempting immediate connection recreation",
		toolCall.FunctionCall.Name, serverName)

	// Emit broken pipe detection event
	h.emitBrokenPipeEvent(ctx, toolCall, serverName, originalErr)

	// Create a fresh connection immediately using shared function
	freshClient, freshErr := mcpcache.GetFreshConnection(ctx, serverName, h.agent.configPath, h.logger)
	if freshErr != nil {
		h.logger.Errorf("🔧 [BROKEN PIPE] Failed to create fresh connection: %v", freshErr)
		return nil, time.Since(startTime), freshErr
	}

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
) (*mcp.CallToolResult, time.Duration, error) {

	h.logger.Infof("🔧 [BROKEN PIPE] Retrying tool call '%s' with fresh connection", toolCall.FunctionCall.Name)

	// Parse the tool arguments from JSON string to map
	retryArgs, parseErr := mcpclient.ParseToolArguments(toolCall.FunctionCall.Arguments)
	if parseErr != nil {
		h.logger.Errorf("🔧 [BROKEN PIPE] Failed to parse tool arguments: %v", parseErr)
		return nil, time.Since(startTime), parseErr
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
		return retryResult, retryDuration, nil
	}

	h.logger.Errorf("🔧 [BROKEN PIPE] Retry failed for tool '%s': %v", toolCall.FunctionCall.Name, retryErr)
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
	logger            logger.ExtendedLogger
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
