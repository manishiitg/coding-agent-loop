// conversation.go
//
// This file contains the synchronous conversation logic for the Agent, including Ask, AskWithHistory, and generateContentWithRetry.
// These functions handle multi-turn LLM conversations, tool call execution, and error handling.
//
// Exported:
//   - Ask
//   - AskWithHistory
//   - generateContentWithRetry

package mcpagent

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpcache"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/mark3labs/mcp-go/mcp"

	"mcp-agent/agent_go/internal/llmtypes"
)

// getLogger returns the agent's logger (guaranteed to be non-nil)
func getLogger(a *Agent) utils.ExtendedLogger {
	// Agent logger is guaranteed to be non-nil in the new architecture
	return a.Logger
}

// isVirtualTool checks if a tool name is a virtual tool
func isVirtualTool(toolName string) bool {
	// Check hardcoded virtual tools
	virtualTools := []string{"get_prompt", "get_resource", "read_large_output", "search_large_output", "query_large_output"}
	for _, vt := range virtualTools {
		if vt == toolName {
			return true
		}
	}

	// Check if it's a custom tool (this will be checked in the calling function)
	return false
}

// getToolExecutionTimeout returns the tool execution timeout duration
func getToolExecutionTimeout(a *Agent) time.Duration {
	// First check if agent has a specific timeout configured
	if a.ToolTimeout > 0 {
		return a.ToolTimeout
	}

	// Fall back to environment variable
	timeoutStr := os.Getenv("TOOL_EXECUTION_TIMEOUT")
	if timeoutStr == "" {
		return 5 * time.Minute // Default 5 minutes (changed from 10 seconds)
	}

	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		// Log parsing error - this function doesn't have access to agent logger
		// so we'll just return the default without logging (or could use fmt.Printf for debugging)
		return 5 * time.Minute // Default 5 minutes (changed from 10 seconds)
	}

	return timeout
}

// ensureSystemPrompt ensures that the system prompt is included in the messages
func ensureSystemPrompt(a *Agent, messages []llmtypes.MessageContent) []llmtypes.MessageContent {
	// Check if the first message is already a system message
	if len(messages) > 0 && messages[0].Role == llmtypes.ChatMessageTypeSystem {
		return messages
	}

	// Check if there's already a system message anywhere in the conversation
	for _, msg := range messages {
		if msg.Role == llmtypes.ChatMessageTypeSystem {
			// System message already exists, don't add another one
			return messages
		}
	}

	// Use the agent's existing system prompt (which should already be correct for the mode)
	systemPrompt := a.SystemPrompt

	// Create system message
	systemMessage := llmtypes.MessageContent{
		Role:  llmtypes.ChatMessageTypeSystem,
		Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: systemPrompt}},
	}

	// Prepend system message to the beginning
	return append([]llmtypes.MessageContent{systemMessage}, messages...)
}

// AskWithHistory runs an interaction using the provided message history (multi-turn conversation).
func AskWithHistory(a *Agent, ctx context.Context, messages []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Use agent's logger if available, otherwise use default
	logger := getLogger(a)
	logger.Infof("Entered AskWithHistory - message_count: %d", len(messages))
	if len(a.Tracers) == 0 {
		a.Tracers = []observability.Tracer{observability.NoopTracer{}}
	}
	if a.MaxTurns <= 0 {
		a.MaxTurns = 50
	}

	// Use the passed context for cancellation checks (not the agent's internal context)
	// This ensures we use the context that the caller wants us to respect
	agentCtx := ctx

	// Track conversation start time for duration calculation
	conversationStartTime := time.Now()

	// ✅ CONTEXT-AWARE HIERARCHY: Initialize based on calling context
	// This ensures hierarchy reflects the actual calling context
	a.initializeHierarchyForContext(ctx)

	// Ensure system prompt is included in messages
	messages = ensureSystemPrompt(a, messages)

	// NEW: Set current query for hierarchy tracking (will be set later when lastUserMessage is extracted)

	// Add cache validation AFTER the agent is fully initialized
	if len(a.Tracers) > 0 && len(a.Clients) > 0 {
		// Debug: Log what's in the clients map
		clientKeys := make([]string, 0, len(a.Clients))
		for k := range a.Clients {
			clientKeys = append(clientKeys, k)
		}

		logger.Info("🔍 Debug: Checking clients map", map[string]interface{}{
			"clients_count": len(a.Clients),
			"clients_keys":  clientKeys,
		})

		// Get actual server information for better cache events
		serverNames := make([]string, 0, len(a.Clients))
		for serverName := range a.Clients {
			serverNames = append(serverNames, serverName)
		}

		// Emit comprehensive cache validation event for all servers
		serverStatus := make(map[string]mcpcache.ServerCacheStatus)
		for serverName := range a.Clients {
			serverStatus[serverName] = mcpcache.ServerCacheStatus{
				ServerName:     serverName,
				Status:         "validation",
				ToolsCount:     len(a.Tools),
				PromptsCount:   0, // Will be populated if available
				ResourcesCount: 0, // Will be populated if available
			}
		}

		// Emit cache operation start event through agent event system (frontend visible)
		cacheStartEvent := events.NewCacheOperationStartEvent("all-servers", "conversation_cache_validation")
		a.EmitTypedEvent(ctx, cacheStartEvent)

		// Also emit to tracers for observability (Langfuse, etc.)
		mcpcache.EmitComprehensiveCacheEvent(
			a.Tracers,
			"validation",
			"conversation_cache_validation",
			serverNames,
			nil, // No result available here
			serverStatus,
			time.Duration(0), // No connection time available
			time.Duration(0), // No cache time available
			nil,              // No errors
		)

		// Debug: Log the comprehensive cache event structure
		logger.Info("🔍 Comprehensive cache event emitted", map[string]interface{}{
			"servers_count": len(a.Clients),
			"server_names":  serverNames,
			"tools_count":   len(a.Tools),
		})

		logger.Info("🔍 Cache validation active during conversation", map[string]interface{}{
			"servers_count": len(a.Clients),
			"tools_count":   len(a.Tools),
			"server_names":  serverNames,
		})
	}

	// Emit user message event for the current conversation
	// Extract the last user message from the conversation history
	var lastUserMessage string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == llmtypes.ChatMessageTypeHuman {
			// Get the text content from the message
			for _, part := range messages[i].Parts {
				if textPart, ok := part.(llmtypes.TextContent); ok {
					lastUserMessage = textPart.Text
					break
				}
			}
			break
		}
	}

	// If no user message found, use a default
	if lastUserMessage == "" {
		lastUserMessage = "conversation_with_history"
	}

	// NEW: Set the current query for hierarchy tracking
	a.SetCurrentQuery(lastUserMessage)

	// NEW: Start agent session for hierarchy tracking
	a.StartAgentSession(ctx)

	userMessageEvent := events.NewUserMessageEvent(0, lastUserMessage, "user")
	a.EmitTypedEvent(ctx, userMessageEvent)

	serverList := strings.Join(a.servers, ",")

	// Events are now emitted directly to tracers (no event dispatcher)

	// Generate trace ID for this conversation
	traceID := events.GenerateEventID()

	// Store trace ID for correlation
	agentStartEventID := traceID

	// Metadata for conversation tracking (used in events)
	conversationMetadata := map[string]interface{}{
		"system_prompt":   a.SystemPrompt,
		"tools_count":     len(a.Tools),
		"agent_mode":      string(a.AgentMode),
		"model_id":        a.ModelID,
		"provider":        string(a.provider),
		"max_turns":       a.MaxTurns,
		"temperature":     a.Temperature,
		"tool_choice":     a.ToolChoice,
		"servers":         serverList,
		"conversation_id": fmt.Sprintf("conv_%d", time.Now().Unix()),
		"start_time":      conversationStartTime.Format(time.RFC3339),
	}

	// Use conversationMetadata to avoid unused variable error
	_ = conversationMetadata

	// Emit conversation start event with correlation (child of agent start)
	conversationStartEvent := events.NewConversationStartEventWithCorrelation(lastUserMessage, a.SystemPrompt, len(a.Tools), serverList, traceID, agentStartEventID)
	a.EmitTypedEvent(ctx, conversationStartEvent)

	// Store conversation start event ID for correlation
	// conversationStartEventID := conversationStartEvent.EventID
	// Metadata for processing tracking

	// 🎯 SMART ROUTING APPLICATION - Apply smart routing with conversation context
	// Reset filtered tools at the start of each conversation to ensure fresh evaluation
	a.filteredTools = a.Tools // Start with all tools, then filter based on conversation context

	// Only run smart routing if it was enabled during initialization
	// In cache-only mode, use cached servers count; otherwise use active clients count
	var serverCount int
	if a.CacheOnly {
		serverCount = len(a.servers) // Use cached servers count
	} else {
		serverCount = len(a.Clients) // Use active clients count
	}

	if a.EnableSmartRouting && len(a.Tools) > a.SmartRoutingThreshold.MaxTools && serverCount > a.SmartRoutingThreshold.MaxServers {
		logger := getLogger(a)
		logger.Infof("🎯 Smart routing enabled - applying conversation-specific tool filtering")

		// Get the full conversation history for context
		conversationContext := a.buildConversationContext(messages)

		filteredTools, err := a.filterToolsByRelevance(ctx, conversationContext)
		if err != nil {
			logger.Warnf("Smart routing failed, using all tools: %w", err)
			a.filteredTools = a.Tools // Fallback to all tools
		} else {
			a.filteredTools = filteredTools
			logger.Infof("🎯 Smart routing successful: using %d filtered tools out of %d total for entire conversation",
				len(filteredTools), len(a.Tools))
		}
	} else {
		// Smart routing was already determined during initialization
		logger := getLogger(a)
		logger.Infof("🔧 Using pre-determined tool set: %d tools (smart routing: %v)", len(a.filteredTools), a.EnableSmartRouting)
	}

	// ✅ Emit system prompt event AFTER smart routing has completed
	// This ensures the frontend sees the final system prompt with filtered servers
	systemPromptEvent := events.NewSystemPromptEvent(a.SystemPrompt, 0)
	a.EmitTypedEvent(ctx, systemPromptEvent)

	var lastResponse string
	for turn := 0; turn < a.MaxTurns; turn++ {
		// NEW: Start turn for hierarchy tracking
		a.StartTurn(ctx, turn+1)

		// Extract the last message from the conversation (could be user, assistant, or tool)
		var lastMessage string

		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]

			for _, part := range lastMsg.Parts {
				if textPart, ok := part.(llmtypes.TextContent); ok {
					lastMessage = textPart.Text
					break
				} else if toolResp, ok := part.(llmtypes.ToolCallResponse); ok {
					lastMessage = toolResp.Content
					break
				} else if toolCall, ok := part.(llmtypes.ToolCall); ok {
					lastMessage = fmt.Sprintf("Tool call: %s", toolCall.FunctionCall.Name)
					break
				}
			}
		}

		// If no message found, use the last user message as fallback
		if lastMessage == "" {
			lastMessage = lastUserMessage
		}

		// Emit conversation turn event using typed event data
		tools := events.ConvertToolsToToolInfo(a.filteredTools, a.toolToServer)
		conversationTurnEvent := events.NewConversationTurnEvent(turn+1, lastMessage, len(messages), false, 0, tools, messages)
		a.EmitTypedEvent(ctx, conversationTurnEvent)

		// Check for context cancellation at the start of each turn
		if agentCtx.Err() != nil {
			// Use agent's logger if available, otherwise use default
			logger := getLogger(a)
			logger.Infof("Context cancelled at start of turn - turn: %d, error: %s, duration: %s", turn+1, agentCtx.Err().Error(), time.Since(conversationStartTime).String())
			return "", messages, fmt.Errorf("conversation cancelled: %w", agentCtx.Err())
		}

		// Use the current messages that include tool results from previous turns
		llmMessages := messages

		// 🆕 ENHANCED TURN 2 DEBUGGING LOGGING
		if turn+1 == 2 {
			// Use agent's logger if available, otherwise use default
			logger := getLogger(a)
			logger.Infof("[TURN 2 DEBUG] 🔍 Starting turn 2 LLM generation...")
			logger.Infof("[TURN 2 DEBUG] 🔍 Message count: %d", len(llmMessages))
			logger.Infof("[TURN 2 DEBUG] 🔍 Filtered tool count: %d (out of %d total)", len(a.filteredTools), len(a.Tools))
			logger.Infof("[TURN 2 DEBUG] 🔍 Context length: %d characters", len(a.SystemPrompt))

			// Log message details for turn 2
			for i, msg := range llmMessages {
				contentLength := 0
				if msg.Parts != nil {
					for _, part := range msg.Parts {
						if textPart, ok := part.(llmtypes.TextContent); ok {
							contentLength += len(textPart.Text)
						}
					}
				}
				logger.Infof("[TURN 2 DEBUG] 📤 Message %d - Role: %s, Content length: %d", i+1, msg.Role, contentLength)
			}
		}

		// Track start time for duration calculation
		llmStartTime := time.Now()

		// 🆕 DETAILED CONVERSATION DEBUG LOGGING
		logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - About to call LLM - Time: %v", turn+1, llmStartTime)
		logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - Messages: %d, Tools: %d", turn+1, len(llmMessages), len(a.filteredTools))
		logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - Context deadline check...", turn+1)
		if deadline, ok := ctx.Deadline(); ok {
			timeUntilDeadline := time.Until(deadline)
			logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - Context deadline: %v, Time until deadline: %v", turn+1, deadline, timeUntilDeadline)
		} else {
			logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - Context has no deadline", turn+1)
		}

		opts := []llmtypes.CallOption{}
		if !llm.IsO3O4Model(a.ModelID) {
			opts = append(opts, llmtypes.WithTemperature(a.Temperature))
		}

		// Set a reasonable default max_tokens to prevent immediate completion
		// Use environment variable if available, otherwise default to 4000 tokens
		maxTokens := 40000 // Default value
		if maxTokensEnv := os.Getenv("ORCHESTRATOR_MAIN_LLM_MAX_TOKENS"); maxTokensEnv != "" {
			if parsed, err := strconv.Atoi(maxTokensEnv); err == nil && parsed > 0 {
				maxTokens = parsed
			}
		}
		opts = append(opts, llmtypes.WithMaxTokens(maxTokens))

		// Use proper LLM function calling via llmtypes.WithTools()
		// Use the pre-filtered tools that were determined at conversation start
		if len(a.filteredTools) > 0 {
			// Tools are already normalized during conversion in ToolsAsLLM() and cache loading
			// No need for extra normalization here since langchaingo bug is fixed
			opts = append(opts, llmtypes.WithTools(a.filteredTools))
			if toolChoiceOpt := ConvertToolChoice(a.ToolChoice); toolChoiceOpt != nil {
				opts = append(opts, llmtypes.WithToolChoice(toolChoiceOpt))
			}
		}
		toolNames := make([]string, len(a.filteredTools))
		for i, tool := range a.filteredTools {
			toolNames[i] = tool.Function.Name
		}

		// Emit LLM Messages event to track what's being sent to the LLM
		// Build tool context from previous tool calls in this conversation
		var toolContext []events.ToolContext
		for _, msg := range messages {
			if msg.Role == llmtypes.ChatMessageTypeTool {
				for _, part := range msg.Parts {
					if toolResp, ok := part.(llmtypes.ToolCallResponse); ok {
						toolContext = append(toolContext, events.ToolContext{
							ToolName:   "previous_tool_call", // We don't have the original tool name here
							ServerName: "unknown",            // We don't have the server name here
							Result:     toolResp.Content,
							Status:     "completed",
						})
					}
				}
			}
		}

		// NEW: Start LLM generation for hierarchy tracking
		a.StartLLMGeneration(ctx)

		// Use GenerateContentWithRetry for robust fallback handling
		resp, genErr, usage := GenerateContentWithRetry(a, ctx, llmMessages, opts, turn, func(msg string) {
			// Streaming callback - no ReAct reasoning tracking needed
		})

		// NEW: End LLM generation for hierarchy tracking
		if resp != nil && len(resp.Choices) > 0 {
			a.EndLLMGeneration(ctx, resp.Choices[0].Content, turn+1, len(resp.Choices[0].ToolCalls), time.Since(llmStartTime), events.UsageMetrics{
				PromptTokens:     usage.InputTokens,
				CompletionTokens: usage.OutputTokens,
				TotalTokens:      usage.TotalTokens,
			})
		}

		// Check for context cancellation after LLM generation
		// TEMPORARILY DISABLED: This check was causing issues with HTTP requests
		if agentCtx.Err() != nil {
			logger.Infof("Context cancelled after LLM generation - TEMPORARILY IGNORING - turn: %d, error: %s, duration: %s, note: This check is temporarily disabled due to HTTP context issues", turn+1, agentCtx.Err().Error(), time.Since(conversationStartTime).String())

			// TEMPORARILY DISABLED: Don't return error, continue with the turn
			// This allows HTTP requests to work while we investigate the root cause
			// return "", messages, fmt.Errorf("conversation cancelled after LLM generation: %w", agentCtx.Err())
		}

		if genErr != nil {
			// Check if this is an empty content error that should trigger fallback
			if strings.Contains(genErr.Error(), "Choice.Content is empty string") ||
				strings.Contains(genErr.Error(), "empty content error") ||
				strings.Contains(genErr.Error(), "choice.Content is empty") {

				logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, empty content error detected, triggering fallback...", turn+1)

				// Try fallback models by calling GenerateContentWithRetry again with fallback
				fallbackResp, fallbackErr, fallbackUsage := GenerateContentWithRetry(a, ctx, llmMessages, opts, turn, func(msg string) {
					logger.Infof("[FALLBACK] %s", msg)
				})

				if fallbackErr == nil && fallbackResp != nil && len(fallbackResp.Choices) > 0 &&
					fallbackResp.Choices[0].Content != "" {
					logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, fallback succeeded, continuing with fallback response", turn+1)
					// Use the fallback response instead
					resp = fallbackResp
					usage = fallbackUsage
					genErr = nil
				} else {
					if fallbackErr != nil {
						logger.Errorf("[AGENT TRACE] AskWithHistory: turn %d, fallback failed with error: %v", turn+1, fallbackErr)
					} else if fallbackResp == nil || len(fallbackResp.Choices) == 0 {
						logger.Errorf("[AGENT TRACE] AskWithHistory: turn %d, fallback failed - no response or choices", turn+1)
					} else {
						logger.Errorf("[AGENT TRACE] AskWithHistory: turn %d, fallback failed - empty content in response", turn+1)
					}
				}
			}

			// If still have an error after fallback attempt, emit error event and return
			if genErr != nil {
				// Emit LLM generation error event using typed event data
				llmErrorEvent := events.NewLLMGenerationErrorEvent(turn+1, a.ModelID, genErr.Error(), time.Since(llmStartTime))
				a.EmitTypedEvent(ctx, llmErrorEvent)

				// Agent processing end event removed - no longer needed

				// 🎯 FIX: End the trace for error cases - replaced with event emission
				conversationErrorEvent := events.NewConversationErrorEvent(lastUserMessage, genErr.Error(), turn+1, "conversation_error", time.Since(conversationStartTime))
				a.EmitTypedEvent(ctx, conversationErrorEvent)

				return "", messages, fmt.Errorf("llm error: %w", genErr)
			}
		}
		if resp == nil || resp.Choices == nil || len(resp.Choices) == 0 {

			// 🎯 FIX: End the trace for error cases - replaced with event emission
			conversationErrorEvent := events.NewConversationErrorEvent(lastUserMessage, "no response choices returned", turn+1, "no_choices", time.Since(conversationStartTime))
			a.EmitTypedEvent(ctx, conversationErrorEvent)

			return "", messages, fmt.Errorf("no response choices returned")
		}

		choice := resp.Choices[0]
		lastResponse = choice.Content

		// LLM generation end event is already emitted by EndLLMGeneration() method above

		// For ReAct agents, reasoning is finalized in ProcessChunk when completion patterns are detected
		// No need to call FinalizeReasoning as it's handled automatically

		// Token usage is already included in the LLMGenerationEndEvent above

		if len(choice.ToolCalls) > 0 {

			// 🔧 FIX: Separate text content and tool calls into different messages
			// Gemini API has issues when a model message contains both TextContent and ToolCall parts.
			// We create separate messages to avoid this issue.

			// 1. If there's text content, append it as a separate AI message
			if choice.Content != "" {
				messages = append(messages, llmtypes.MessageContent{
					Role:  llmtypes.ChatMessageTypeAI,
					Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: choice.Content}},
				})
			}

			// 2. Append tool calls as a separate AI message (without text)
			toolCallParts := make([]llmtypes.ContentPart, 0, len(choice.ToolCalls))
			for _, tc := range choice.ToolCalls {
				toolCallParts = append(toolCallParts, tc)
			}
			messages = append(messages, llmtypes.MessageContent{
				Role:  llmtypes.ChatMessageTypeAI,
				Parts: toolCallParts,
			})

			// 2. For each tool call, execute and append the tool result as a new message
			for _, tc := range choice.ToolCalls {

				// Determine server name for tool call events
				serverName := a.toolToServer[tc.FunctionCall.Name]
				if isVirtualTool(tc.FunctionCall.Name) {
					serverName = "virtual-tools"
				}

				// Emit tool call start event using typed event data with correlation
				toolStartEvent := events.NewToolCallStartEventWithCorrelation(turn+1, tc.FunctionCall.Name, events.ToolParams{
					Arguments: tc.FunctionCall.Arguments,
				}, serverName, traceID, traceID) // Using traceID for both traceID and parentID correlation

				a.EmitTypedEvent(ctx, toolStartEvent)

				if tc.FunctionCall == nil {
					logger.Errorf("[AGENT DEBUG] AskWithHistory Early return: invalid tool call: nil function call")

					// 🎯 FIX: End the trace for invalid tool call error - replaced with event emission
					conversationErrorEvent := events.NewConversationErrorEvent(lastUserMessage, "invalid tool call: nil function call", turn+1, "invalid_tool_call", time.Since(conversationStartTime))
					a.EmitTypedEvent(ctx, conversationErrorEvent)

					return "", messages, fmt.Errorf("invalid tool call: nil function call")
				}

				// 🔧 ENHANCED: Check for empty tool name and provide feedback to LLM for self-correction
				if tc.FunctionCall.Name == "" {
					logger.Errorf("[AGENT DEBUG] AskWithHistory Turn %d: Empty tool name detected in tool call - Arguments: %s", turn+1, tc.FunctionCall.Arguments)

					// Generate feedback message for empty tool name
					feedbackMessage := generateEmptyToolNameFeedback(tc.FunctionCall.Arguments)

					// Emit tool call error event for observability (after tool start event)
					toolNameErrorEvent := events.NewToolCallErrorEvent(turn+1, "", "empty tool name", "", time.Since(conversationStartTime))
					a.EmitTypedEvent(ctx, toolNameErrorEvent)

					// Add feedback to conversation so LLM can correct itself
					toolName := ""
					if tc.FunctionCall != nil {
						toolName = tc.FunctionCall.Name
					}
					messages = append(messages, llmtypes.MessageContent{
						Role:  llmtypes.ChatMessageTypeTool,
						Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: toolName, Content: feedbackMessage}},
					})

					continue
				}
				args, err := mcpclient.ParseToolArguments(tc.FunctionCall.Arguments)
				if err != nil {
					logger.Errorf("[AGENT DEBUG] AskWithHistory Tool args parsing error: %w", err)

					// 🔧 ENHANCED: Instead of failing, provide feedback to LLM for self-correction
					feedbackMessage := generateToolArgsParsingFeedback(tc.FunctionCall.Name, tc.FunctionCall.Arguments, err)

					// Emit tool call error event for observability
					toolArgsParsingErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, fmt.Sprintf("parse tool args: %w", err), "", time.Since(conversationStartTime))
					a.EmitTypedEvent(ctx, toolArgsParsingErrorEvent)

					// Add feedback to conversation so LLM can correct itself
					messages = append(messages, llmtypes.MessageContent{
						Role:  llmtypes.ChatMessageTypeTool,
						Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: feedbackMessage}},
					})

					continue
				}

				// 🔧 FIX: Check custom tools FIRST before MCP client lookup
				// Custom tools don't need MCP clients, so check them early
				isCustomTool := false
				if a.customTools != nil {
					if _, exists := a.customTools[tc.FunctionCall.Name]; exists {
						isCustomTool = true
					}
				}

				client := a.Client
				if a.toolToServer != nil {
					if mapped, ok := a.toolToServer[tc.FunctionCall.Name]; ok {
						if a.Clients != nil {
							if c, exists := a.Clients[mapped]; exists {
								client = c
							}
						}
					}
				}
				// Only check for client errors for non-custom tools and non-virtual tools
				if !isCustomTool && !isVirtualTool(tc.FunctionCall.Name) && client == nil {
					// Check if we're in cache-only mode with no active connections
					if len(a.Clients) == 0 {

						// Create connection on-demand for the specific server
						serverName := a.toolToServer[tc.FunctionCall.Name]
						if serverName == "" {
							logger.Warnf("[AGENT DEBUG] AskWithHistory Turn %d: Tool '%s' not mapped to any server. Providing feedback to LLM.", turn+1, tc.FunctionCall.Name)

							// Generate helpful feedback instead of failing
							feedbackMessage := fmt.Sprintf("❌ Tool '%s' is not available in this system.\n\n🔧 Available tools include:\n- get_prompt, get_resource (virtual tools)\n- read_large_output, search_large_output, query_large_output (file tools)\n- MCP server tools (check system prompt for full list)\n\n💡 Please use one of the available tools listed above.", tc.FunctionCall.Name)

							// Emit tool call error event for observability
							toolNotFoundEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, fmt.Sprintf("tool '%s' not found", tc.FunctionCall.Name), "", time.Since(conversationStartTime))
							a.EmitTypedEvent(ctx, toolNotFoundEvent)

							// Add feedback to conversation so LLM can correct itself
							messages = append(messages, llmtypes.MessageContent{
								Role:  llmtypes.ChatMessageTypeTool,
								Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: feedbackMessage}},
							})

							continue
						}

						// Create a fresh connection for this specific server
						onDemandClient, err := a.createOnDemandConnection(ctx, serverName)
						if err != nil {
							logger.Errorf("[AGENT DEBUG] AskWithHistory Early return: failed to create on-demand connection for server %s: %v", serverName, err)
							conversationErrorEvent := events.NewConversationErrorEvent(lastUserMessage, fmt.Sprintf("failed to create on-demand connection for server %s: %v", serverName, err), turn+1, "on_demand_connection_failed", time.Since(conversationStartTime))
							a.EmitTypedEvent(ctx, conversationErrorEvent)
							return "", messages, fmt.Errorf("failed to create on-demand connection for server %s: %w", serverName, err)
						}

						// Use the on-demand client
						client = onDemandClient
					} else {
						logger.Errorf("[AGENT DEBUG] AskWithHistory Early return: no MCP client found for tool %s", tc.FunctionCall.Name)

						// 🎯 FIX: End the trace for no MCP client error - replaced with event emission
						conversationErrorEvent := events.NewConversationErrorEvent(lastUserMessage, fmt.Sprintf("no MCP client found for tool %s", tc.FunctionCall.Name), turn+1, "no_mcp_client", time.Since(conversationStartTime))
						a.EmitTypedEvent(ctx, conversationErrorEvent)

						err := fmt.Errorf("no MCP client found for tool %s", tc.FunctionCall.Name)
						return "", messages, err
					}
				}

				// Check for context cancellation before tool execution
				if agentCtx.Err() != nil {
					// Use agent's logger if available, otherwise use default
					logger := getLogger(a)
					logger.Infof("Context cancelled before tool execution - turn: %d, tool_name: %s, error: %s, duration: %s", turn+1, tc.FunctionCall.Name, agentCtx.Err().Error(), time.Since(conversationStartTime).String())
					return "", messages, fmt.Errorf("conversation cancelled before tool execution: %w", agentCtx.Err())
				}

				// Create timeout context for tool execution
				toolTimeout := getToolExecutionTimeout(a)
				toolCtx, cancel := context.WithTimeout(ctx, toolTimeout)
				defer cancel()

				startTime := time.Now()

				// Add cache hit event during tool execution to show cached connection usage
				if len(a.Tracers) > 0 && serverName != "" && serverName != "virtual-tools" {
					// Emit connection cache hit event to show we're using cached MCP server connection
					// Note: We do NOT cache tool execution results - only server connections
					connectionCacheHitEvent := events.NewCacheHitEvent(serverName, fmt.Sprintf("unified_%s", serverName), "unified_cache", 1, time.Duration(0))

					// Debug: Log the connection cache hit event structure
					logger.Info("🔍 Connection cache hit event structure", map[string]interface{}{
						"event_type":  connectionCacheHitEvent.GetEventType(),
						"server_name": connectionCacheHitEvent.ServerName,
						"cache_key":   connectionCacheHitEvent.CacheKey,
						"config_path": connectionCacheHitEvent.ConfigPath,
						"tools_count": connectionCacheHitEvent.ToolsCount,
						"age":         connectionCacheHitEvent.Age,
						"timestamp":   connectionCacheHitEvent.Timestamp,
						"tool_name":   tc.FunctionCall.Name,
						"turn":        turn + 1,
						"note":        "Using cached MCP server connection, NOT caching tool execution results",
					})

					a.EmitTypedEvent(ctx, connectionCacheHitEvent)

					logger.Infof("[CONNECTION CACHE DEBUG] Turn %d, Tool: %s, Using cached connection for server: %s", turn+1, tc.FunctionCall.Name, serverName)
				}

				var result *mcp.CallToolResult
				var toolErr error

				// Check if this is a virtual tool
				if isVirtualTool(tc.FunctionCall.Name) {
					// Handle virtual tool execution
					resultText, toolErr := a.HandleVirtualTool(toolCtx, tc.FunctionCall.Name, args)
					if toolErr != nil {
						result = &mcp.CallToolResult{
							IsError: true,
							Content: []mcp.Content{&mcp.TextContent{Text: toolErr.Error()}},
						}
					} else {
						result = &mcp.CallToolResult{
							IsError: false,
							Content: []mcp.Content{&mcp.TextContent{Text: resultText}},
						}
					}
				} else if a.customTools != nil {
					// Check if this is a custom tool
					if customTool, exists := a.customTools[tc.FunctionCall.Name]; exists {
						// Handle custom tool execution using the stored execution function
						resultText, toolErr := customTool.Execution(toolCtx, args)

						if toolErr != nil {
							result = &mcp.CallToolResult{
								IsError: true,
								Content: []mcp.Content{&mcp.TextContent{Text: toolErr.Error()}},
							}
						} else {
							result = &mcp.CallToolResult{
								IsError: false,
								Content: []mcp.Content{&mcp.TextContent{Text: resultText}},
							}
						}
					} else {
						// Handle regular MCP tool execution
						result, toolErr = client.CallTool(toolCtx, tc.FunctionCall.Name, args)
					}
				} else {
					// Handle regular MCP tool execution
					result, toolErr = client.CallTool(toolCtx, tc.FunctionCall.Name, args)
				}

				duration := time.Since(startTime)

				// Check for timeout
				if toolCtx.Err() == context.DeadlineExceeded {
					toolErr = fmt.Errorf("tool execution timed out after %s: %s", toolTimeout.String(), tc.FunctionCall.Name)
					// Use agent's logger if available, otherwise use default
					logger := getLogger(a)
					logger.Infof("Tool call timed out - turn: %d, tool_name: %s, timeout: %s", turn+1, tc.FunctionCall.Name, toolTimeout.String())
				}

				if agentCtx.Err() != nil {
					// Use agent's logger if available, otherwise use default
					logger := getLogger(a)
					logger.Infof("Tool call context error - turn: %d, tool_name: %s, error: %s", turn+1, tc.FunctionCall.Name, agentCtx.Err().Error())
				}

				// Handle tool execution errors gracefully - provide feedback to LLM and continue
				if toolErr != nil {
					// 🔧 ENHANCED ERROR RECOVERY HANDLING
					errorRecoveryHandler := NewErrorRecoveryHandler(a)

					// Attempt error recovery for recoverable errors
					recoveredResult, recoveredErr, recoveredDuration, wasRecovered := errorRecoveryHandler.HandleError(
						ctx, &tc, serverName, toolErr, startTime, isCustomTool, isVirtualTool(tc.FunctionCall.Name))

					if wasRecovered && recoveredErr == nil {
						// Successfully recovered - use recovered result and continue normal flow
						logger.Infof("🔧 [ERROR RECOVERY] Successfully recovered from error for tool '%s'", tc.FunctionCall.Name)
						result = recoveredResult
						toolErr = nil
						duration = recoveredDuration
						// Continue to normal result processing below (outside this if block)
					} else {
						// Recovery failed or not attempted - proceed with error handling
						if wasRecovered {
							logger.Errorf("🔧 [ERROR RECOVERY] Recovery failed for tool '%s': %v", tc.FunctionCall.Name, recoveredErr)
							toolErr = recoveredErr
							duration = recoveredDuration
						}

						// Emit tool call error event using typed event data
						toolErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, toolErr.Error(), serverName, duration)
						a.EmitTypedEvent(ctx, toolErrorEvent)

						// Instead of failing the entire conversation, provide feedback to the LLM
						errorResultText := fmt.Sprintf("Tool execution failed - %v", toolErr)

						// Add the error result to the conversation so the LLM can continue
						messages = append(messages, llmtypes.MessageContent{
							Role:  llmtypes.ChatMessageTypeTool, // Use "tool" role for tool responses
							Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: errorResultText}},
						})

						// Continue to next turn instead of returning error
						continue
					}
				}
				var resultText string
				if result != nil {

					// Get the tool result as string (without prefix)
					resultText = mcpclient.ToolResultAsString(result, getLogger(a))

					// 🔧 BROKEN PIPE DETECTION IN SUCCESSFUL RESULT PATH
					if result.IsError && (strings.Contains(resultText, "Broken pipe") || strings.Contains(resultText, "[Errno 32]")) {
						logger.Infof("🔧 [BROKEN PIPE DETECTED IN RESULT] Turn %d, Tool: %s, Server: %s - Attempting immediate connection recreation", turn+1, tc.FunctionCall.Name, serverName)

						// Create error recovery handler
						errorRecoveryHandler := NewErrorRecoveryHandler(a)

						// Create a fake error for the recovery handler
						fakeErr := fmt.Errorf("broken pipe detected in result: %s", resultText)

						// Attempt error recovery
						recoveredResult, recoveredErr, recoveredDuration, wasRecovered := errorRecoveryHandler.HandleError(
							ctx, &tc, serverName, fakeErr, startTime, isCustomTool, isVirtualTool(tc.FunctionCall.Name))

						if wasRecovered && recoveredErr == nil {
							logger.Infof("🔧 [BROKEN PIPE RECOVERY SUCCESS] Turn %d, Tool: %s - Using recovered result", turn+1, tc.FunctionCall.Name)
							result = recoveredResult
							duration = recoveredDuration
							resultText = mcpclient.ToolResultAsString(result, getLogger(a))
						} else if wasRecovered {
							logger.Errorf("🔧 [BROKEN PIPE RECOVERY FAILED] Turn %d, Tool: %s - Recovery failed: %v", turn+1, tc.FunctionCall.Name, recoveredErr)
						}
					}

					// Check if this is a large tool output that should be written to file
					if a.toolOutputHandler != nil {
						// Check if this is a large tool output that should be written to file
						if a.toolOutputHandler.IsLargeToolOutputWithModel(resultText, a.ModelID) {

							// Emit large tool output detection event
							detectedEvent := events.NewLargeToolOutputDetectedEvent(tc.FunctionCall.Name, len(resultText), a.toolOutputHandler.GetToolOutputFolder())
							detectedEvent.ServerAvailable = a.toolOutputHandler.IsServerAvailable()
							a.EmitTypedEvent(ctx, detectedEvent)

							// Write large output to file
							filePath, writeErr := a.toolOutputHandler.WriteToolOutputToFile(resultText, tc.FunctionCall.Name)
							if writeErr == nil {
								// Extract first 100 characters for Langfuse observability
								preview := a.toolOutputHandler.ExtractFirstNCharacters(resultText, 100)

								// Emit successful file write event with preview
								fileWrittenEvent := events.NewLargeToolOutputFileWrittenEvent(tc.FunctionCall.Name, filePath, len(resultText), preview)
								a.EmitTypedEvent(ctx, fileWrittenEvent)

								// Create message with file path, first 100 characters, and instructions
								fileMessage := a.toolOutputHandler.CreateToolOutputMessageWithPreview(tc.ID, filePath, resultText)

								// Replace the result text with the file message
								resultText = fileMessage

							} else {
								// Emit file write error event
								fileErrorEvent := events.NewLargeToolOutputFileWriteErrorEvent(tc.FunctionCall.Name, writeErr.Error(), len(resultText))
								a.EmitTypedEvent(ctx, fileErrorEvent)
							}
						}
					}
				} else {
					resultText = "Tool execution completed but no result returned"
				}
				// 3. Append the tool result as a new message (after the AI tool_call message)
				// Add recover block to catch panics
				func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Errorf("[AGENT ERROR] Panic while appending tool result message: %v", r)
						}
					}()
					// Use the exact tool call ID from the LLM response
					messages = append(messages, llmtypes.MessageContent{
						Role:  llmtypes.ChatMessageTypeTool, // Use "tool" role for tool responses
						Parts: []llmtypes.ContentPart{llmtypes.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: resultText}},
					})
				}()

				// End the tool execution span with output and error information
				toolOutput := map[string]interface{}{
					"tool_name":   tc.FunctionCall.Name,
					"server_name": a.toolToServer[tc.FunctionCall.Name],
					"result":      resultText,
					"duration":    duration,
					"turn":        turn + 1,
					"success":     toolErr == nil,
					"timeout":     getToolExecutionTimeout(a).String(),
				}
				if toolErr != nil {
					toolOutput["error"] = toolErr.Error()
					if strings.Contains(toolErr.Error(), "timed out") {
						toolOutput["error_type"] = "tool_execution_timeout"
					} else {
						toolOutput["error_type"] = "tool_execution_error"
					}
				}

				// Tool execution completed - emit tool call end event

				// Emit tool call end event using typed event data (consolidated - contains all tool information)
				toolEndEvent := events.NewToolCallEndEvent(turn+1, tc.FunctionCall.Name, resultText, serverName, duration, "")
				a.EmitTypedEvent(ctx, toolEndEvent)

				// Note: Removed redundant tool_output and tool_response events
				// tool_call_end now contains all necessary tool information

			}

			continue
		} else {
			// No tool calls - add the assistant response to conversation history
			// This is CRITICAL to prevent conversation loops
			if choice.Content != "" {
				assistantMessage := llmtypes.MessageContent{
					Role:  llmtypes.ChatMessageTypeAI,
					Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: choice.Content}},
				}
				messages = append(messages, assistantMessage)
			}

			// Simple agent - return immediately when no tool calls
			logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, no tool calls detected, returning final answer", turn+1)

			// Emit unified completion event for simple agent
			unifiedCompletionEvent := events.NewUnifiedCompletionEvent(
				"simple",                          // agentType
				string(a.AgentMode),               // agentMode
				lastUserMessage,                   // question
				choice.Content,                    // finalResult
				"completed",                       // status
				time.Since(conversationStartTime), // duration
				turn+1,                            // turns
			)
			a.EmitTypedEvent(ctx, unifiedCompletionEvent)

			// NEW: End agent session for hierarchy tracking
			a.EndAgentSession(ctx)

			return choice.Content, messages, nil
		}
	}

	// Max turns reached - give agent one final chance to provide a proper answer
	logger.Infof("[AGENT TRACE] AskWithHistory: max turns (%d) reached, giving agent final chance to provide answer.", a.MaxTurns)

	// Emit max turns reached event
	maxTurnsEvent := events.NewMaxTurnsReachedEvent(a.MaxTurns, a.MaxTurns, lastUserMessage, "You are out of turns, you need to generate final now. Please provide your final answer based on what you have accomplished so far.", string(a.AgentMode), time.Since(conversationStartTime))
	a.EmitTypedEvent(ctx, maxTurnsEvent)

	// Add a user message asking for final answer
	finalUserMessage := llmtypes.MessageContent{
		Role: llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{
			llmtypes.TextContent{
				Text: "You are out of turns, you need to generate final now. Please provide your final answer based on what you have accomplished so far.",
			},
		},
	}

	// Add the final user message to the conversation
	messages = append(messages, finalUserMessage)

	// Emit user message event for the final request
	finalUserMessageEvent := events.NewUserMessageEvent(a.MaxTurns, "You are out of turns, you need to generate final now. Please provide your final answer based on what you have accomplished so far.", "user")
	a.EmitTypedEvent(ctx, finalUserMessageEvent)

	// Make one final LLM call to get the final answer
	var finalResp *llmtypes.ContentResponse
	var err error

	// Create options for final call with reasonable max_tokens
	maxTokens := 40000 // Default value
	if maxTokensEnv := os.Getenv("ORCHESTRATOR_MAIN_LLM_MAX_TOKENS"); maxTokensEnv != "" {
		if parsed, err := strconv.Atoi(maxTokensEnv); err == nil && parsed > 0 {
			maxTokens = parsed
		}
	}

	finalOpts := []llmtypes.CallOption{
		llmtypes.WithMaxTokens(maxTokens), // Set reasonable default for final answer
	}
	if !llm.IsO3O4Model(a.ModelID) {
		finalOpts = append(finalOpts, llmtypes.WithTemperature(a.Temperature))
	}

	finalResp, err, _ = GenerateContentWithRetry(a, ctx, messages, finalOpts, a.MaxTurns, func(msg string) {
		// Optional: stream the final response
	})

	if err != nil {
		// If the final call also fails, emit error event
		conversationErrorEvent := &events.ConversationErrorEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Question: lastUserMessage,
			Error:    "max turns reached and final attempt failed",
			Turn:     a.MaxTurns,
			Context:  "conversation",
			Duration: time.Since(conversationStartTime),
		}
		a.EmitTypedEvent(ctx, conversationErrorEvent)

		if lastResponse != "" {
			logger.Infof("[AGENT TRACE] AskWithHistory: forced FINAL_ANSWER due to max turns: %s", lastResponse)

			// Agent end event removed - no longer needed

			// 🎯 FIX: End the trace for fallback completion - replaced with event emission
			// Note: This was a successful completion, so we emit a completion event instead of error
			unifiedCompletionEvent := events.NewUnifiedCompletionEvent(
				"react",                           // agentType
				string(a.AgentMode),               // agentMode
				lastUserMessage,                   // question
				lastResponse,                      // finalResult
				"completed",                       // status
				time.Since(conversationStartTime), // duration
				a.MaxTurns,                        // turns
			)
			a.EmitTypedEvent(ctx, unifiedCompletionEvent)

			// NEW: End agent session for hierarchy tracking
			a.EndAgentSession(ctx)

			// Append the final response to messages array for consistency
			if lastResponse != "" {
				assistantMessage := llmtypes.MessageContent{
					Role:  llmtypes.ChatMessageTypeAI,
					Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: lastResponse}},
				}
				messages = append(messages, assistantMessage)
			}

			return lastResponse, messages, nil
		}
		logger.Infof("[AGENT TRACE] AskWithHistory: exiting with no final answer after %d turns.", a.MaxTurns)

		// 🎯 FIX: End the trace for max turns error - replaced with event emission
		maxTurnsErrorEvent := events.NewConversationErrorEvent(lastUserMessage, fmt.Sprintf("max turns (%d) reached without final answer", a.MaxTurns), a.MaxTurns, "max_turns_exceeded", time.Since(conversationStartTime))
		a.EmitTypedEvent(ctx, maxTurnsErrorEvent)

		return "", messages, fmt.Errorf("max turns (%d) reached without final answer", a.MaxTurns)
	}

	if finalResp == nil || finalResp.Choices == nil || len(finalResp.Choices) == 0 {
		logger.Infof("[AGENT TRACE] AskWithHistory: final call returned no response choices")

		// 🎯 FIX: End the trace for final call error - replaced with event emission
		finalCallErrorEvent := events.NewConversationErrorEvent(lastUserMessage, "final call returned no response choices", a.MaxTurns, "no_final_choices", time.Since(conversationStartTime))
		a.EmitTypedEvent(ctx, finalCallErrorEvent)

		return "", messages, fmt.Errorf("final call returned no response choices")
	}

	finalChoice := finalResp.Choices[0]

	// Token usage is already included in the LLMGenerationEndEvent above

	// Note: LLM generation end event is already emitted in the main conversation flow
	// No need to emit it again here to avoid duplication

	// Simple agent - use final choice content directly
	logger.Infof("[AGENT TRACE] AskWithHistory: final answer provided after max turns")

	// Emit unified completion event
	unifiedCompletionEvent := events.NewUnifiedCompletionEvent(
		"simple",                          // agentType
		string(a.AgentMode),               // agentMode
		lastUserMessage,                   // question
		finalChoice.Content,               // finalResult
		"completed",                       // status
		time.Since(conversationStartTime), // duration
		a.MaxTurns+1,                      // turns (+1 for the final turn)
	)
	a.EmitTypedEvent(ctx, unifiedCompletionEvent)

	// NEW: End agent session for hierarchy tracking
	a.EndAgentSession(ctx)

	// Append the final response to messages array for consistency
	if finalChoice.Content != "" {
		assistantMessage := llmtypes.MessageContent{
			Role:  llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: finalChoice.Content}},
		}
		messages = append(messages, assistantMessage)
	}

	return finalChoice.Content, messages, nil
}
