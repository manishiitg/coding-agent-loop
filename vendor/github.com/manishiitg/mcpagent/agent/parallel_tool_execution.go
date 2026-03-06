// parallel_tool_execution.go
//
// This file implements parallel (concurrent) execution of multiple tool calls
// returned by the LLM in a single response. When EnableParallelToolExecution is true
// and the LLM returns >1 tool calls, they execute concurrently using a fork-join pattern.
// Results are collected in pre-allocated indexed slots and assembled in deterministic order.
//
// Exported:
//   - executeToolCallsParallel

package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpcache"
	"github.com/manishiitg/mcpagent/mcpclient"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/mark3labs/mcp-go/mcp"
)

// toolExecutionPlan contains all pre-processed data needed to execute a single tool call.
// Built sequentially in Phase 1 before goroutines are launched.
type toolExecutionPlan struct {
	index      int
	toolCall   llmtypes.ToolCall
	args       map[string]interface{}
	client     mcpclient.ClientInterface
	serverName string

	isCustomTool bool
	isVirtual    bool

	toolTimeout  time.Duration
	hasNoTimeout bool
	toolType     string // "MCP", "virtual", "custom"

	// If true, skip execution — a pre-error message is already set
	skipExecution   bool
	preErrorMessage *llmtypes.MessageContent
}

// toolExecutionResult holds the output of a single tool execution goroutine.
// Written by exactly one goroutine into its indexed slot.
type toolExecutionResult struct {
	// messages to append (tool response)
	messages []llmtypes.MessageContent

	// Raw result from tool execution (for event emission and loop detection)
	result     *mcp.CallToolResult
	resultText string
	duration   time.Duration
	toolErr    error

	// If set, the entire conversation should return this error
	fatalError error
}

// executeToolCallsParallel orchestrates concurrent execution of multiple tool calls.
//
// Phase 1 (sequential): Prepare all tool calls — parse args, resolve clients, emit start events.
// Phase 2 (parallel):   Launch goroutines, each writes result to its indexed slot.
// Phase 3 (sequential): Assemble messages in order, emit end/error events, run loop detection.
func executeToolCallsParallel(
	ctx context.Context,
	a *Agent,
	toolCalls []llmtypes.ToolCall,
	messages []llmtypes.MessageContent,
	turn int,
	traceID string,
	conversationStartTime time.Time,
	lastUserMessage string,
	loopDetector *ToolLoopDetector,
	agentCtx context.Context,
) ([]llmtypes.MessageContent, error) {

	v2Logger := a.Logger

	// ─── Phase 1: Sequential preparation ───────────────────────────────────

	plans := make([]toolExecutionPlan, len(toolCalls))
	for i, tc := range toolCalls {
		plan := prepareToolExecution(ctx, a, tc, i, turn, traceID, conversationStartTime, agentCtx)
		plans[i] = plan

		// Emit tool call start event (sequential to keep hierarchy sane)
		if !plan.skipExecution {
			toolStartEvent := events.NewToolCallStartEventWithCorrelation(turn+1, tc.FunctionCall.Name, events.ToolParams{
				Arguments: tc.FunctionCall.Arguments,
			}, plan.serverName, traceID, traceID)
			toolStartEvent.IsParallel = true
			toolStartEvent.ToolCallID = tc.ID
			a.EmitTypedEvent(ctx, toolStartEvent)
		}
	}

	// Check for context cancellation before launching goroutines
	if agentCtx.Err() != nil {
		v2Logger.Debug("Context cancelled before parallel tool execution",
			loggerv2.Int("turn", turn+1),
			loggerv2.Error(agentCtx.Err()))
		cancellationEvent := events.NewContextCancelledEvent(
			turn+1,
			"cancelled before parallel tool execution",
			time.Since(conversationStartTime),
		)
		a.EmitTypedEvent(ctx, cancellationEvent)
		return messages, fmt.Errorf("conversation cancelled before parallel tool execution: %w", agentCtx.Err())
	}

	// ─── Phase 2: Parallel execution ───────────────────────────────────────

	results := make([]toolExecutionResult, len(plans))
	var wg sync.WaitGroup

	for i, plan := range plans {
		if plan.skipExecution {
			// Pre-error: already have the message, no goroutine needed
			if plan.preErrorMessage != nil {
				results[i] = toolExecutionResult{
					messages: []llmtypes.MessageContent{*plan.preErrorMessage},
				}
			}
			continue
		}

		wg.Add(1)
		go func(idx int, p toolExecutionPlan) {
			defer wg.Done()
			results[idx] = executeToolCall(ctx, a, p, turn, conversationStartTime, agentCtx)
		}(i, plan)
	}

	wg.Wait()

	// ─── Phase 3: Sequential assembly ──────────────────────────────────────

	needToolRefresh := false

	for i, plan := range plans {
		res := results[i]
		tc := plan.toolCall

		// Check for fatal errors (should abort entire conversation)
		if res.fatalError != nil {
			return messages, res.fatalError
		}

		// Append messages in order
		messages = append(messages, res.messages...)

		// Emit end/error events
		if plan.skipExecution {
			// Pre-error plans already have their error events emitted in prepareToolExecution
			// or the error message was built without needing an end event
			continue
		}

		if res.toolErr != nil {
			// Tool execution error — emit error event
			toolErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, res.toolErr.Error(), plan.serverName, res.duration)
			toolErrorEvent.ToolCallID = tc.ID
			a.EmitTypedEvent(ctx, toolErrorEvent)
		} else if res.result == nil || !res.result.IsError {
			// Success — emit tool call end event
			_, _, _, _, _, _, _, _, _, _, _, _, contextUsagePercent := a.GetTokenUsageWithPricing()
			a.tokenTrackingMutex.RLock()
			modelContextWindow := a.modelContextWindow
			contextWindowUsage := a.currentContextWindowUsage
			a.tokenTrackingMutex.RUnlock()

			toolEndEvent := events.NewToolCallEndEventWithTokenUsageAndModel(turn+1, tc.FunctionCall.Name, res.resultText, plan.serverName, res.duration, "", contextUsagePercent, modelContextWindow, contextWindowUsage, a.ModelID)
			toolEndEvent.ToolCallID = tc.ID
			a.EmitTypedEvent(ctx, toolEndEvent)
		} else if res.result != nil && res.result.IsError {
			// Tool returned error in result
			toolErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, res.resultText, plan.serverName, res.duration)
			toolErrorEvent.ToolCallID = tc.ID
			a.EmitTypedEvent(ctx, toolErrorEvent)
		}

		// Loop detection (sequential)
		if tc.FunctionCall != nil && res.resultText != "" {
			loopResult := loopDetector.CheckAndHandleLoop(tc.FunctionCall.Name, tc.FunctionCall.Arguments, res.resultText)
			if loopResult.Detected {
				HandleLoopDetection(a, ctx, loopResult, lastUserMessage, turn+1, conversationStartTime, &messages, v2Logger)
			}
		}

		// Track if add_tool was called for tool refresh
		if a.UseToolSearchMode && tc.FunctionCall != nil && tc.FunctionCall.Name == "add_tool" && res.toolErr == nil {
			needToolRefresh = true
		}
	}

	// Refresh tools if any add_tool was in the batch
	if needToolRefresh {
		a.filteredTools = a.getToolsForToolSearchMode()
		v2Logger.Debug("🔍 [TOOL_SEARCH] Tools refreshed after parallel add_tool",
			loggerv2.Int("discovered_count", a.GetDiscoveredToolCount()),
			loggerv2.Int("total_available", len(a.filteredTools)))
	}

	return messages, nil
}

// prepareToolExecution extracts the pre-processing logic from the sequential loop.
// It parses arguments, resolves clients, validates the tool, and builds a plan.
// Does NOT execute the tool or emit end events.
func prepareToolExecution(
	ctx context.Context,
	a *Agent,
	tc llmtypes.ToolCall,
	index int,
	turn int,
	traceID string,
	conversationStartTime time.Time,
	agentCtx context.Context,
) toolExecutionPlan {

	v2Logger := a.Logger

	plan := toolExecutionPlan{
		index:    index,
		toolCall: tc,
	}

	if tc.FunctionCall == nil {
		v2Logger.Warn("Tool call has nil FunctionCall", loggerv2.Int("tool_call_index", index+1))
		plan.skipExecution = true
		return plan
	}

	// Determine server name
	plan.serverName = a.toolToServer[tc.FunctionCall.Name]
	plan.isVirtual = isVirtualTool(tc.FunctionCall.Name)

	if plan.isVirtual {
		if tc.FunctionCall.Name == "get_api_spec" && tc.FunctionCall.Arguments != "" {
			var args map[string]interface{}
			if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err == nil {
				if srvName, ok := args["server_name"].(string); ok && srvName != "" {
					plan.serverName = srvName
				} else {
					plan.serverName = "virtual-tools"
				}
			} else {
				plan.serverName = "virtual-tools"
			}
		} else {
			plan.serverName = "virtual-tools"
		}
	}

	// Handle nil FunctionCall (already checked above, but guard for safety)
	if tc.FunctionCall == nil {
		plan.skipExecution = true
		return plan
	}

	// Empty tool name
	if tc.FunctionCall.Name == "" {
		v2Logger.Error("Empty tool name detected in tool call", nil,
			loggerv2.Int("turn", turn+1),
			loggerv2.String("arguments", tc.FunctionCall.Arguments))

		feedbackMessage := generateEmptyToolNameFeedback(tc.FunctionCall.Arguments)
		toolNameErrorEvent := events.NewToolCallErrorEvent(turn+1, "", "empty tool name", "", time.Since(conversationStartTime))
		toolNameErrorEvent.ToolCallID = tc.ID
		a.EmitTypedEvent(ctx, toolNameErrorEvent)

		msg := llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeTool,
			Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: feedbackMessage}},
		}
		plan.skipExecution = true
		plan.preErrorMessage = &msg
		return plan
	}

	// Parse arguments
	args, err := mcpclient.ParseToolArguments(tc.FunctionCall.Arguments)
	if err != nil {
		v2Logger.Error("Tool args parsing error", err)
		feedbackMessage := generateToolArgsParsingFeedback(tc.FunctionCall.Name, tc.FunctionCall.Arguments, err)
		toolArgsParsingErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, fmt.Sprintf("parse tool args: %v", err), "", time.Since(conversationStartTime))
		toolArgsParsingErrorEvent.ToolCallID = tc.ID
		a.EmitTypedEvent(ctx, toolArgsParsingErrorEvent)

		msg := llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeTool,
			Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: feedbackMessage}},
		}
		plan.skipExecution = true
		plan.preErrorMessage = &msg
		return plan
	}
	plan.args = args

	// Check custom tools
	if a.customTools != nil {
		if _, exists := a.customTools[tc.FunctionCall.Name]; exists {
			plan.isCustomTool = true
		}
	}

	// Resolve client
	plan.client = a.Client
	if a.toolToServer != nil {
		if mapped, ok := a.toolToServer[tc.FunctionCall.Name]; ok {
			if mapped == "custom" {
				plan.isCustomTool = true
			} else if a.Clients != nil {
				a.clientsMu.RLock()
				if c, exists := a.Clients[mapped]; exists {
					plan.client = c
				}
				a.clientsMu.RUnlock()
			}
		}
	}

	// Check for client requirement for non-custom, non-virtual tools
	if !plan.isCustomTool && !plan.isVirtual && plan.client == nil {
		if len(a.Clients) == 0 {
			serverName := ""
			if a.toolToServer != nil {
				serverName = a.toolToServer[tc.FunctionCall.Name]
			}
			if serverName == "" {
				feedbackMessage := fmt.Sprintf("❌ Tool '%s' is not available in this system.\n\n🔧 Available tools include:\n- get_prompt, get_resource (virtual tools)\n- read_large_output, search_large_output, query_large_output (file tools)\n- MCP server tools (check system prompt for full list)\n\n💡 Please use one of the available tools listed above.", tc.FunctionCall.Name)

				toolNotFoundEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, fmt.Sprintf("tool '%s' not found", tc.FunctionCall.Name), "", time.Since(conversationStartTime))
				toolNotFoundEvent.ToolCallID = tc.ID
				a.EmitTypedEvent(ctx, toolNotFoundEvent)

				msg := llmtypes.MessageContent{
					Role:  llmtypes.ChatMessageTypeTool,
					Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: feedbackMessage}},
				}
				plan.skipExecution = true
				plan.preErrorMessage = &msg
				return plan
			}

			// Attempt on-demand connection
			onDemandClient, err := mcpcache.GetFreshConnection(ctx, serverName, a.configPath, v2Logger)
			if err != nil {
				v2Logger.Error("Failed to create on-demand connection",
					err,
					loggerv2.String("server", serverName))
				conversationErrorEvent := events.NewConversationErrorEvent(getLastUserMessageForEvent(), fmt.Sprintf("failed to create on-demand connection for server %s: %v", serverName, err), turn+1, "on_demand_connection_failed", time.Since(conversationStartTime))
				a.EmitTypedEvent(ctx, conversationErrorEvent)
				// Mark as fatal — this should abort the conversation
				plan.skipExecution = true
				msg := llmtypes.MessageContent{
					Role:  llmtypes.ChatMessageTypeTool,
					Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: fmt.Sprintf("Error: failed to create on-demand connection for server %s: %v", serverName, err)}},
				}
				plan.preErrorMessage = &msg
				return plan
			}
			plan.client = onDemandClient
		} else {
			v2Logger.Error("No MCP client found for tool", nil,
				loggerv2.String("tool", tc.FunctionCall.Name))

			conversationErrorEvent := events.NewConversationErrorEvent("", fmt.Sprintf("no MCP client found for tool %s", tc.FunctionCall.Name), turn+1, "no_mcp_client", time.Since(conversationStartTime))
			a.EmitTypedEvent(ctx, conversationErrorEvent)

			msg := llmtypes.MessageContent{
				Role:  llmtypes.ChatMessageTypeTool,
				Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: fmt.Sprintf("Error: no MCP client found for tool %s", tc.FunctionCall.Name)}},
			}
			plan.skipExecution = true
			plan.preErrorMessage = &msg
			return plan
		}
	}

	// Determine tool timeout
	plan.toolTimeout = getToolExecutionTimeout(a)
	if plan.isCustomTool {
		if customTool, exists := a.customTools[tc.FunctionCall.Name]; exists && customTool.Timeout != -1 {
			if customTool.Timeout == 0 {
				plan.hasNoTimeout = true
			} else if customTool.Timeout > 0 {
				plan.toolTimeout = customTool.Timeout
			}
		}
	}

	// Determine tool type
	plan.toolType = "MCP"
	if plan.isVirtual {
		plan.toolType = "virtual"
	} else if plan.isCustomTool {
		plan.toolType = "custom"
	}

	return plan
}

// getLastUserMessageForEvent returns an empty string as a placeholder for event metadata.
// During parallel preparation, the actual lastUserMessage is not readily available,
// but it's only used for event metadata (not functional behavior).
func getLastUserMessageForEvent() string {
	return ""
}

// executeToolCall runs a single tool call and returns its result.
// This function is safe to call from a goroutine — it does NOT mutate shared state
// (messages slice, loop detector, event hierarchy). It only reads from the plan
// and calls the tool.
func executeToolCall(
	ctx context.Context,
	a *Agent,
	plan toolExecutionPlan,
	turn int,
	conversationStartTime time.Time,
	agentCtx context.Context,
) toolExecutionResult {

	v2Logger := a.Logger
	tc := plan.toolCall
	result := toolExecutionResult{}

	// Create timeout context for tool execution
	var toolCtx context.Context
	var cancel context.CancelFunc
	if plan.hasNoTimeout {
		toolCtx = ctx
		cancel = func() {}
	} else {
		toolCtx, cancel = context.WithTimeout(ctx, plan.toolTimeout)
	}
	defer cancel()

	startTime := time.Now()

	// Log tool call
	argsJSON, _ := json.Marshal(plan.args)
	timeoutStr := plan.toolTimeout.String()
	if plan.hasNoTimeout {
		timeoutStr = "none (indefinite)"
	}
	v2Logger.Debug("🔧 [TOOL_CALL] Tool called (parallel)",
		loggerv2.String("tool_name", tc.FunctionCall.Name),
		loggerv2.String("tool_type", plan.toolType),
		loggerv2.String("server_name", plan.serverName),
		loggerv2.String("tool_call_id", tc.ID),
		loggerv2.Int("turn", turn+1),
		loggerv2.String("arguments", string(argsJSON)),
		loggerv2.String("timeout", timeoutStr))

	// Cache hit event
	if len(a.Tracers) > 0 && plan.serverName != "" && plan.serverName != "virtual-tools" {
		connectionCacheHitEvent := events.NewCacheHitEvent(plan.serverName, fmt.Sprintf("unified_%s", plan.serverName), "unified_cache", 1, time.Duration(0))
		a.EmitTypedEvent(ctx, connectionCacheHitEvent)
	}

	// Inject tool execution metadata into context
	toolCtx = context.WithValue(toolCtx, ToolExecutionAgentKey, a)
	toolCtx = context.WithValue(toolCtx, ToolExecutionTurnKey, turn+1)
	toolCtx = context.WithValue(toolCtx, ToolExecutionServerKey, plan.serverName)
	toolCtx = context.WithValue(toolCtx, ToolExecutionLLMConfigKey, a.GetLLMModelConfig())

	// ─── Execute the tool ──────────────────────────────────────────────

	var mcpResult *mcp.CallToolResult
	var toolErr error

	if isVirtualTool(tc.FunctionCall.Name) {
		v2Logger.Debug("🔧 [TOOL_CALL] Executing virtual tool (parallel)",
			loggerv2.String("tool_name", tc.FunctionCall.Name))
		resultText, vtErr := a.HandleVirtualTool(toolCtx, tc.FunctionCall.Name, plan.args)
		if vtErr != nil {
			mcpResult = &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: vtErr.Error()}},
			}
		} else {
			if resultText == "" {
				resultText = fmt.Sprintf("Tool '%s' executed successfully but returned no output.", tc.FunctionCall.Name)
			}
			mcpResult = &mcp.CallToolResult{
				IsError: false,
				Content: []mcp.Content{&mcp.TextContent{Text: resultText}},
			}
		}
	} else if a.customTools != nil {
		if customTool, exists := a.customTools[tc.FunctionCall.Name]; exists {
			resultText, ctErr := customTool.Execution(toolCtx, plan.args)
			if ctErr != nil {
				mcpResult = &mcp.CallToolResult{
					IsError: true,
					Content: []mcp.Content{&mcp.TextContent{Text: ctErr.Error()}},
				}
			} else {
				mcpResult = &mcp.CallToolResult{
					IsError: false,
					Content: []mcp.Content{&mcp.TextContent{Text: resultText}},
				}
			}
		} else {
			// Fallback to MCP client
			mcpResult, toolErr = callToolWithTimeoutWrapper(toolCtx, plan.client, tc.FunctionCall.Name, plan.args, v2Logger, plan.serverName)
		}
	} else {
		mcpResult, toolErr = callToolWithTimeoutWrapper(toolCtx, plan.client, tc.FunctionCall.Name, plan.args, v2Logger, plan.serverName)
	}

	result.duration = time.Since(startTime)

	// Check for timeout
	if toolCtx.Err() == context.DeadlineExceeded {
		toolErr = fmt.Errorf("tool execution timed out after %s: %s", plan.toolTimeout.String(), tc.FunctionCall.Name)
	}

	// Handle tool execution errors
	if toolErr != nil {
		// Attempt error recovery
		errorRecoveryHandler := NewErrorRecoveryHandler(a)
		recoveredResult, recoveredDuration, wasRecovered, recoveredErr := errorRecoveryHandler.HandleError(
			ctx, &tc, plan.serverName, toolErr, time.Now().Add(-result.duration), plan.isCustomTool, plan.isVirtual)

		if wasRecovered && recoveredErr == nil {
			mcpResult = recoveredResult
			toolErr = nil
			result.duration = recoveredDuration
		} else {
			if wasRecovered {
				toolErr = recoveredErr
				result.duration = recoveredDuration
			}

			errorResultText := fmt.Sprintf("Tool execution failed - %v", toolErr)
			result.toolErr = toolErr
			result.resultText = errorResultText
			result.messages = []llmtypes.MessageContent{{
				Role:  llmtypes.ChatMessageTypeTool,
				Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: errorResultText}},
			}}
			return result
		}
	}

	// Process result
	var resultText string
	if mcpResult != nil {
		resultText = mcpclient.ToolResultAsString(mcpResult)

		if resultText == "" && !mcpResult.IsError {
			resultText = fmt.Sprintf("Tool '%s' executed successfully but returned no output.", tc.FunctionCall.Name)
		}

		// Check for broken pipe in content
		if mcpclient.IsBrokenPipeInContent(resultText) {
			v2Logger.Info(fmt.Sprintf("🔧 [BROKEN PIPE DETECTED IN RESULT] Turn %d, Tool: %s, Server: %s", turn+1, tc.FunctionCall.Name, plan.serverName))
			errorRecoveryHandler := NewErrorRecoveryHandler(a)
			fakeErr := fmt.Errorf("broken pipe detected in result: %s", resultText)
			recoveredResult, recoveredDuration, wasRecovered, recoveredErr := errorRecoveryHandler.HandleError(
				ctx, &tc, plan.serverName, fakeErr, time.Now().Add(-result.duration), plan.isCustomTool, plan.isVirtual)

			if wasRecovered && recoveredErr == nil {
				mcpResult = recoveredResult
				result.duration = recoveredDuration
				resultText = mcpclient.ToolResultAsString(mcpResult)
			}
		}

		// Context offloading
		if a.EnableContextOffloading && a.toolOutputHandler != nil {
			if a.toolOutputHandler.IsLargeToolOutputWithModel(resultText, a.ModelID) {
				detectedEvent := events.NewLargeToolOutputDetectedEvent(tc.FunctionCall.Name, len(resultText), a.toolOutputHandler.GetToolOutputFolder())
				detectedEvent.ServerAvailable = a.toolOutputHandler.IsServerAvailable()
				a.EmitTypedEvent(ctx, detectedEvent)

				filePath, writeErr := a.toolOutputHandler.WriteToolOutputToFile(resultText, tc.FunctionCall.Name)
				if writeErr == nil {
					preview := a.toolOutputHandler.ExtractFirstNCharacters(resultText, 100)
					fileWrittenEvent := events.NewLargeToolOutputFileWrittenEvent(tc.FunctionCall.Name, filePath, len(resultText), preview)
					a.EmitTypedEvent(ctx, fileWrittenEvent)
					fileMessage := a.toolOutputHandler.CreateToolOutputMessageWithPreview(tc.ID, filePath, resultText, 50, false)
					resultText = fileMessage
				} else {
					fileErrorEvent := events.NewLargeToolOutputFileWriteErrorEvent(tc.FunctionCall.Name, writeErr.Error(), len(resultText))
					a.EmitTypedEvent(ctx, fileErrorEvent)
				}
			}
		}

		// Safety check: Apply max token limit truncation regardless of context offloading setting
		// This prevents API errors from prompts exceeding model context limits
		if a.toolOutputHandler != nil && a.toolOutputHandler.ExceedsMaxTokenLimit(resultText, a.ModelID) {
			truncatedResult, wasTruncated := a.toolOutputHandler.TruncateToMaxTokenLimit(resultText, a.ModelID, tc.FunctionCall.Name)
			if wasTruncated {
				v2Logger.Warn("Tool output exceeded max token limit, truncated",
					loggerv2.String("tool", tc.FunctionCall.Name),
					loggerv2.Int("original_length", len(resultText)),
					loggerv2.Int("truncated_length", len(truncatedResult)),
					loggerv2.Int("max_tokens", a.toolOutputHandler.GetMaxToolOutputTokens()))
				resultText = truncatedResult
			}
		}
	} else {
		resultText = "Tool execution completed but no result returned"
	}

	result.result = mcpResult
	result.resultText = resultText
	result.messages = []llmtypes.MessageContent{{
		Role:  llmtypes.ChatMessageTypeTool,
		Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: resultText}},
	}}
	return result
}


