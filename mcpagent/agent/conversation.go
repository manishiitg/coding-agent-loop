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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"mcpagent/events"
	"mcpagent/llm"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpcache"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	contextKeyWorkspaceEventEmitter contextKey = "workspace_event_emitter"
	contextKeyTurn                  contextKey = "turn"
	contextKeyServerName            contextKey = "server_name"
)

// getLogger returns the agent's logger (guaranteed to be non-nil)
func getLogger(a *Agent) loggerv2.Logger {
	// Agent logger is guaranteed to be non-nil in the new architecture
	return a.Logger
}

// isVirtualTool checks if a tool name is a virtual tool
func isVirtualTool(toolName string) bool {
	// Check hardcoded virtual tools (includes all possible virtual tools)
	virtualTools := []string{
		"get_prompt", "get_resource",
		"read_large_output", "search_large_output", "query_large_output",
		"discover_code_files", "write_code", // Code execution mode tools (discover_code_structure removed)
	}
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
	v2Logger := a.Logger
	v2Logger.Debug("Entered AskWithHistory", loggerv2.Int("message_count", len(messages)))
	if len(a.Tracers) == 0 {
		a.Tracers = []observability.Tracer{observability.NoopTracer{}}
	}
	if a.MaxTurns <= 0 {
		a.MaxTurns = 25
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
		v2Logger.Debug("Comprehensive cache event emitted")

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
	// Use active clients count
	serverCount := len(a.Clients)

	if a.EnableSmartRouting && len(a.Tools) > a.SmartRoutingThreshold.MaxTools && serverCount > a.SmartRoutingThreshold.MaxServers {
		v2Logger.Info("Smart routing enabled - applying conversation-specific tool filtering")

		// Get the full conversation history for context
		conversationContext := a.buildConversationContext(messages)

		filteredTools, err := a.filterToolsByRelevance(ctx, conversationContext)
		if err != nil {
			v2Logger.Warn("Smart routing failed, using all tools", loggerv2.Error(err))
			a.filteredTools = a.Tools // Fallback to all tools
		} else {
			a.filteredTools = filteredTools
			v2Logger.Info("Smart routing successful",
				loggerv2.Int("filtered_tools", len(filteredTools)),
				loggerv2.Int("total_tools", len(a.Tools)))
		}
	} else {
		// Smart routing was already determined during initialization
		v2Logger.Debug("Using pre-determined tool set",
			loggerv2.Int("tool_count", len(a.filteredTools)),
			loggerv2.Any("smart_routing", a.EnableSmartRouting))
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
			v2Logger.Debug("Context cancelled at start of turn",
				loggerv2.Int("turn", turn+1),
				loggerv2.Error(agentCtx.Err()),
				loggerv2.String("duration", time.Since(conversationStartTime).String()))
			return "", messages, fmt.Errorf("conversation cancelled: %w", agentCtx.Err())
		}

		// Use the current messages that include tool results from previous turns
		llmMessages := messages

		// Track start time for duration calculation
		llmStartTime := time.Now()

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

		// NEW: Start LLM generation for hierarchy tracking
		a.StartLLMGeneration(ctx)

		// Use GenerateContentWithRetry for robust fallback handling
		resp, usage, genErr := GenerateContentWithRetry(a, ctx, llmMessages, opts, turn, func(msg string) {
			// Streaming callback - no ReAct reasoning tracking needed
		})

		// NEW: End LLM generation for hierarchy tracking
		if resp != nil && len(resp.Choices) > 0 {
			a.EndLLMGeneration(ctx, resp.Choices[0].Content, turn+1, len(resp.Choices[0].ToolCalls), time.Since(llmStartTime), events.UsageMetrics{
				PromptTokens:     usage.InputTokens,
				CompletionTokens: usage.OutputTokens,
				TotalTokens:      usage.TotalTokens,
			}, resp)
		}

		// Check for context cancellation after LLM generation
		// TEMPORARILY DISABLED: This check was causing issues with HTTP requests
		if agentCtx.Err() != nil {
			v2Logger.Debug("Context cancelled after LLM generation (temporarily ignoring)",
				loggerv2.Int("turn", turn+1),
				loggerv2.Error(agentCtx.Err()),
				loggerv2.String("duration", time.Since(conversationStartTime).String()))

			// TEMPORARILY DISABLED: Don't return error, continue with the turn
			// This allows HTTP requests to work while we investigate the root cause
			// return "", messages, fmt.Errorf("conversation cancelled after LLM generation: %w", agentCtx.Err())
		}

		if genErr != nil {
			// Check if this is an empty content error that should trigger fallback
			if strings.Contains(genErr.Error(), "Choice.Content is empty string") ||
				strings.Contains(genErr.Error(), "empty content error") ||
				strings.Contains(genErr.Error(), "choice.Content is empty") {

				v2Logger.Debug("Empty content error detected, triggering fallback",
					loggerv2.Int("turn", turn+1))

				// Try fallback models by calling GenerateContentWithRetry again with fallback
				fallbackResp, fallbackUsage, fallbackErr := GenerateContentWithRetry(a, ctx, llmMessages, opts, turn, func(msg string) {
					v2Logger.Info(fmt.Sprintf("[FALLBACK] %s", msg))
				})

				if fallbackErr == nil && fallbackResp != nil && len(fallbackResp.Choices) > 0 &&
					fallbackResp.Choices[0].Content != "" {
					v2Logger.Debug("Fallback succeeded", loggerv2.Int("turn", turn+1))
					// Use the fallback response instead
					resp = fallbackResp
					usage = fallbackUsage
					genErr = nil
				} else {
					if fallbackErr != nil {
						v2Logger.Error("Fallback failed with error", fallbackErr, loggerv2.Int("turn", turn+1))
					} else if fallbackResp == nil || len(fallbackResp.Choices) == 0 {
						v2Logger.Error("Fallback failed - no response or choices", nil, loggerv2.Int("turn", turn+1))
					} else {
						v2Logger.Error("Fallback failed - empty content in response", nil, loggerv2.Int("turn", turn+1))
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

		// Log empty response as warning
		if len(choice.Content) == 0 && len(choice.ToolCalls) == 0 {
			v2Logger.Warn("LLM Response is empty", loggerv2.Int("turn", turn+1))
		}

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
			// Include read_image in tool call message so LLM knows it called the tool
			toolCallParts := make([]llmtypes.ContentPart, 0, len(choice.ToolCalls))
			for _, tc := range choice.ToolCalls {
				if tc.FunctionCall != nil {
					toolCallParts = append(toolCallParts, tc)
				}
			}
			// Add tool call message if there are any tool calls
			if len(toolCallParts) > 0 {
				messages = append(messages, llmtypes.MessageContent{
					Role:  llmtypes.ChatMessageTypeAI,
					Parts: toolCallParts,
				})
			}

			// 2. For each tool call, execute and append the tool result as a new message
			for i, tc := range choice.ToolCalls {
				if tc.FunctionCall == nil {
					v2Logger.Warn("Tool call has nil FunctionCall", loggerv2.Int("tool_call_index", i+1))
				}

				// Special handling for read_image tool - check FIRST before any other processing
				// This ensures we don't emit tool call events or add tool responses for read_image
				if tc.FunctionCall != nil && tc.FunctionCall.Name == "read_image" {

					// Execute read_image tool and handle specially
					// Don't add tool call message or tool response message
					// Instead, add user message with image + query directly

					// Determine server name for tool call events
					serverName := a.toolToServer[tc.FunctionCall.Name]
					if isVirtualTool(tc.FunctionCall.Name) {
						serverName = "virtual-tools"
					}

					// Emit tool call start event (for observability only, not for conversation)
					toolStartEvent := events.NewToolCallStartEventWithCorrelation(turn+1, tc.FunctionCall.Name, events.ToolParams{
						Arguments: tc.FunctionCall.Arguments,
					}, serverName, traceID, traceID)
					a.EmitTypedEvent(ctx, toolStartEvent)

					// Parse arguments
					args, err := mcpclient.ParseToolArguments(tc.FunctionCall.Arguments)
					if err != nil {
						v2Logger.Error(fmt.Sprintf("🖼️ [DEBUG] Failed to parse read_image arguments: %v", err), err)
						// Emit error event
						toolErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, fmt.Sprintf("parse arguments: %v", err), serverName, 0)
						a.EmitTypedEvent(ctx, toolErrorEvent)
						// Add error tool result message (required by Anthropic API - every tool_use must have tool_result)
						messages = append(messages, llmtypes.MessageContent{
							Role: llmtypes.ChatMessageTypeTool,
							Parts: []llmtypes.ContentPart{
								llmtypes.ToolCallResponse{
									ToolCallID: tc.ID,
									Name:       tc.FunctionCall.Name,
									Content:    fmt.Sprintf("Error: Failed to parse arguments: %v", err),
								},
							},
						})
						continue
					}

					// Create timeout context for tool execution
					toolTimeout := getToolExecutionTimeout(a)
					toolCtx, cancel := context.WithTimeout(ctx, toolTimeout)
					defer cancel()

					startTime := time.Now()

					// Execute the tool (read_image is a custom tool, not a virtual tool)
					var resultText string
					var toolErr error
					if a.customTools != nil {
						if customTool, exists := a.customTools[tc.FunctionCall.Name]; exists {
							resultText, toolErr = customTool.Execution(toolCtx, args)
						} else {
							toolErr = fmt.Errorf("read_image tool not found in custom tools")
						}
					} else {
						toolErr = fmt.Errorf("custom tools not initialized")
					}
					duration := time.Since(startTime)

					if toolErr != nil {
						v2Logger.Error("read_image tool execution failed", toolErr)
						// Emit tool call error event (for observability only)
						toolErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, toolErr.Error(), serverName, duration)
						a.EmitTypedEvent(ctx, toolErrorEvent)
						// Add error tool result message (required by Anthropic API - every tool_use must have tool_result)
						messages = append(messages, llmtypes.MessageContent{
							Role: llmtypes.ChatMessageTypeTool,
							Parts: []llmtypes.ContentPart{
								llmtypes.ToolCallResponse{
									ToolCallID: tc.ID,
									Name:       tc.FunctionCall.Name,
									Content:    fmt.Sprintf("Error: Tool execution failed: %v", toolErr),
								},
							},
						})
						continue
					}

					// Parse the result JSON
					var imageResult map[string]interface{}
					if err := json.Unmarshal([]byte(resultText), &imageResult); err != nil {
						previewLen := 500
						if len(resultText) < previewLen {
							previewLen = len(resultText)
						}
						v2Logger.Warn(fmt.Sprintf("🖼️ [DEBUG] Failed to parse read_image result as JSON: %v, raw result: %s", err, resultText[:previewLen]))
						// Add error tool result message (required by Anthropic API - every tool_use must have tool_result)
						messages = append(messages, llmtypes.MessageContent{
							Role: llmtypes.ChatMessageTypeTool,
							Parts: []llmtypes.ContentPart{
								llmtypes.ToolCallResponse{
									ToolCallID: tc.ID,
									Name:       tc.FunctionCall.Name,
									Content:    fmt.Sprintf("Error: Failed to parse result as JSON: %v", err),
								},
							},
						})
						continue
					}

					// Check if it's an image_query type
					if imageResult["_type"] != "image_query" {
						v2Logger.Warn(fmt.Sprintf("🖼️ [DEBUG] read_image result is not image_query type, got: %v", imageResult["_type"]))
						// Add error tool result message (required by Anthropic API - every tool_use must have tool_result)
						messages = append(messages, llmtypes.MessageContent{
							Role: llmtypes.ChatMessageTypeTool,
							Parts: []llmtypes.ContentPart{
								llmtypes.ToolCallResponse{
									ToolCallID: tc.ID,
									Name:       tc.FunctionCall.Name,
									Content:    fmt.Sprintf("Error: Result is not image_query type, got: %v", imageResult["_type"]),
								},
							},
						})
						continue
					}

					// Extract image data
					query, _ := imageResult["query"].(string)
					mimeType, _ := imageResult["mime_type"].(string)
					base64Data, _ := imageResult["data"].(string)

					if query == "" || mimeType == "" || base64Data == "" {
						v2Logger.Warn(fmt.Sprintf("🖼️ [DEBUG] Missing required fields in read_image result - query: %q, mimeType: %q, base64Data: %q", query != "", mimeType != "", base64Data != ""))
						// Add error tool result message (required by Anthropic API - every tool_use must have tool_result)
						messages = append(messages, llmtypes.MessageContent{
							Role: llmtypes.ChatMessageTypeTool,
							Parts: []llmtypes.ContentPart{
								llmtypes.ToolCallResponse{
									ToolCallID: tc.ID,
									Name:       tc.FunctionCall.Name,
									Content:    "Error: Missing required fields in read_image result (query, mime_type, or data)",
								},
							},
						})
						continue
					}

					// Create user message with image + query
					// Do NOT add tool call message or tool response message
					// Note: Vertex provider (both Gemini and Vertex Anthropic) requires image first, then text
					// This applies to both:
					//   - Gemini models (gemini-*) using GoogleGenAIAdapter
					//   - Anthropic models (claude-*) using VertexAnthropicAdapter
					// Other providers can use text first, then image
					var parts []llmtypes.ContentPart
					if a.provider == llm.ProviderVertex {
						// Vertex provider (Gemini and Vertex Anthropic): image first, then text (required by API)
						parts = []llmtypes.ContentPart{
							llmtypes.ImageContent{
								SourceType: "base64",
								MediaType:  mimeType,
								Data:       base64Data,
							},
							llmtypes.TextContent{Text: query},
						}
					} else {
						// Other providers: text first, then image
						parts = []llmtypes.ContentPart{
							llmtypes.TextContent{Text: query},
							llmtypes.ImageContent{
								SourceType: "base64",
								MediaType:  mimeType,
								Data:       base64Data,
							},
						}
					}

					userMessage := llmtypes.MessageContent{
						Role:  llmtypes.ChatMessageTypeHuman,
						Parts: parts,
					}

					// Add artificial tool response message FIRST (must be immediately after tool_use for Vertex AI)
					// This prevents the LLM from calling read_image again in a loop
					artificialResponse := llmtypes.MessageContent{
						Role: llmtypes.ChatMessageTypeTool,
						Parts: []llmtypes.ContentPart{
							llmtypes.ToolCallResponse{
								ToolCallID: tc.ID,
								Name:       tc.FunctionCall.Name,
								Content:    "Image loaded and processed. The image content has been added to the conversation.",
							},
						},
					}
					messages = append(messages, artificialResponse)

					// Add user message with image AFTER tool response (Vertex AI requires tool_result immediately after tool_use)
					messages = append(messages, userMessage)

					// Emit tool call end event (for observability only)
					toolEndEvent := events.NewToolCallEndEvent(turn+1, tc.FunctionCall.Name, "Image loaded and added to conversation", serverName, duration, "")
					a.EmitTypedEvent(ctx, toolEndEvent)

					// Continue to next iteration (tool call and response messages are already added)
					// This will cause the loop to continue processing other tool calls, then continue to next turn
					continue
				}

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
					v2Logger.Error("AskWithHistory Early return: invalid tool call: nil function call", nil)

					// 🎯 FIX: End the trace for invalid tool call error - replaced with event emission
					conversationErrorEvent := events.NewConversationErrorEvent(lastUserMessage, "invalid tool call: nil function call", turn+1, "invalid_tool_call", time.Since(conversationStartTime))
					a.EmitTypedEvent(ctx, conversationErrorEvent)

					return "", messages, fmt.Errorf("invalid tool call: nil function call")
				}

				// 🔧 ENHANCED: Check for empty tool name and provide feedback to LLM for self-correction
				if tc.FunctionCall.Name == "" {
					v2Logger.Error("AskWithHistory: Empty tool name detected in tool call", nil,
						loggerv2.Int("turn", turn+1),
						loggerv2.String("arguments", tc.FunctionCall.Arguments))

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
					v2Logger.Error("AskWithHistory Tool args parsing error", err)

					// 🔧 ENHANCED: Instead of failing, provide feedback to LLM for self-correction
					feedbackMessage := generateToolArgsParsingFeedback(tc.FunctionCall.Name, tc.FunctionCall.Arguments, err)

					// Emit tool call error event for observability
					toolArgsParsingErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, fmt.Sprintf("parse tool args: %v", err), "", time.Since(conversationStartTime))
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
						v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] Tool '%s' identified as custom tool (customTools map has %d tools)", tc.FunctionCall.Name, len(a.customTools)))
					} else {
						v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] Tool '%s' not found in customTools (map has %d tools)", tc.FunctionCall.Name, len(a.customTools)))
					}
				} else {
					v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] customTools map is nil for tool '%s'", tc.FunctionCall.Name))
				}

				// Check if it's a virtual tool
				isVirtual := isVirtualTool(tc.FunctionCall.Name)
				if isVirtual {
					v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] Tool '%s' identified as virtual tool", tc.FunctionCall.Name))
				}

				client := a.Client
				if a.toolToServer != nil {
					if mapped, ok := a.toolToServer[tc.FunctionCall.Name]; ok {
						v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] Tool '%s' mapped to server '%s' in toolToServer", tc.FunctionCall.Name, mapped))
						if mapped == "custom" {
							// Custom tool - no client needed
							v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] Tool '%s' is a custom tool (mapped to 'custom'), skipping client lookup", tc.FunctionCall.Name))
							isCustomTool = true // Ensure it's marked as custom
						} else if a.Clients != nil {
							if c, exists := a.Clients[mapped]; exists {
								client = c
								v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] Found client for tool '%s' from server '%s'", tc.FunctionCall.Name, mapped))
							} else {
								v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] Server '%s' mapped for tool '%s' but no client found in Clients map", mapped, tc.FunctionCall.Name))
							}
						}
					} else {
						v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] Tool '%s' not found in toolToServer mapping (map has %d entries)", tc.FunctionCall.Name, len(a.toolToServer)))
					}
				} else {
					v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] toolToServer map is nil for tool '%s'", tc.FunctionCall.Name))
				}

				// Only check for client errors for non-custom tools and non-virtual tools
				if !isCustomTool && !isVirtual && client == nil {
					v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] Tool '%s' requires client but none found (isCustomTool=%v, isVirtual=%v, client=nil)", tc.FunctionCall.Name, isCustomTool, isVirtual))
					// Check if we have no active connections
					if len(a.Clients) == 0 {
						v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_LOOKUP] No active clients (len(a.Clients)=%d), attempting on-demand connection for tool '%s'", len(a.Clients), tc.FunctionCall.Name))

						// Create connection on-demand for the specific server
						serverName := ""
						if a.toolToServer != nil {
							serverName = a.toolToServer[tc.FunctionCall.Name]
						}
						if serverName == "" {
							// Calculate counts for logging
							customToolsCount := 0
							if a.customTools != nil {
								customToolsCount = len(a.customTools)
							}
							toolToServerCount := 0
							if a.toolToServer != nil {
								toolToServerCount = len(a.toolToServer)
							}
							v2Logger.Warn(fmt.Sprintf("🔧 [TOOL_LOOKUP] Tool '%s' not mapped to any server. isCustomTool=%v, isVirtual=%v, customTools has %d tools, toolToServer has %d entries",
								tc.FunctionCall.Name, isCustomTool, isVirtual, customToolsCount, toolToServerCount))
							v2Logger.Warn(fmt.Sprintf("[AGENT DEBUG] AskWithHistory Turn %d: Tool '%s' not mapped to any server. Providing feedback to LLM.", turn+1, tc.FunctionCall.Name))

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

						// Create a fresh connection for this specific server using shared function
						onDemandClient, err := mcpcache.GetFreshConnection(ctx, serverName, a.configPath, v2Logger)
						if err != nil {
							v2Logger.Error("AskWithHistory Early return: failed to create on-demand connection",
								err,
								loggerv2.String("server", serverName))
							conversationErrorEvent := events.NewConversationErrorEvent(lastUserMessage, fmt.Sprintf("failed to create on-demand connection for server %s: %v", serverName, err), turn+1, "on_demand_connection_failed", time.Since(conversationStartTime))
							a.EmitTypedEvent(ctx, conversationErrorEvent)
							return "", messages, fmt.Errorf("failed to create on-demand connection for server %s: %w", serverName, err)
						}

						// Use the on-demand client
						client = onDemandClient
					} else {
						v2Logger.Error("AskWithHistory Early return: no MCP client found for tool", nil,
							loggerv2.String("tool", tc.FunctionCall.Name))

						// 🎯 FIX: End the trace for no MCP client error - replaced with event emission
						conversationErrorEvent := events.NewConversationErrorEvent(lastUserMessage, fmt.Sprintf("no MCP client found for tool %s", tc.FunctionCall.Name), turn+1, "no_mcp_client", time.Since(conversationStartTime))
						a.EmitTypedEvent(ctx, conversationErrorEvent)

						err := fmt.Errorf("no MCP client found for tool %s", tc.FunctionCall.Name)
						return "", messages, err
					}
				}

				// Check for context cancellation before tool execution
				if agentCtx.Err() != nil {
					v2Logger.Debug("Context cancelled before tool execution",
						loggerv2.Int("turn", turn+1),
						loggerv2.String("tool_name", tc.FunctionCall.Name),
						loggerv2.Error(agentCtx.Err()),
						loggerv2.String("duration", time.Since(conversationStartTime).String()))
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
					v2Logger.Debug("Connection cache hit")

					a.EmitTypedEvent(ctx, connectionCacheHitEvent)

				}

				// Inject event emitter, turn, and server name into context for workspace tools
				toolCtx = context.WithValue(toolCtx, contextKeyWorkspaceEventEmitter, a)
				toolCtx = context.WithValue(toolCtx, contextKeyTurn, turn+1)
				toolCtx = context.WithValue(toolCtx, contextKeyServerName, serverName)

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
						// Ensure resultText is never empty for virtual tools
						// This prevents empty content from being sent to LLM
						if resultText == "" {
							v2Logger.Warn("Virtual tool returned empty result - using default message",
								loggerv2.String("tool", tc.FunctionCall.Name))
							resultText = fmt.Sprintf("Tool '%s' executed successfully but returned no output.", tc.FunctionCall.Name)
						}
						result = &mcp.CallToolResult{
							IsError: false,
							Content: []mcp.Content{&mcp.TextContent{Text: resultText}},
						}
					}
				} else if a.customTools != nil {
					// Check if this is a custom tool
					if customTool, exists := a.customTools[tc.FunctionCall.Name]; exists {
						v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_EXECUTION] Executing custom tool '%s' (category: %s)", tc.FunctionCall.Name, customTool.Category))
						// Handle custom tool execution using the stored execution function
						resultText, toolErr := customTool.Execution(toolCtx, args)

						if toolErr != nil {
							v2Logger.Error(fmt.Sprintf("🔧 [TOOL_EXECUTION] Custom tool '%s' execution failed: %v", tc.FunctionCall.Name, toolErr), toolErr)
							result = &mcp.CallToolResult{
								IsError: true,
								Content: []mcp.Content{&mcp.TextContent{Text: toolErr.Error()}},
							}
						} else {
							v2Logger.Debug(fmt.Sprintf("🔧 [TOOL_EXECUTION] Custom tool '%s' executed successfully (result length: %d chars)", tc.FunctionCall.Name, len(resultText)))
							result = &mcp.CallToolResult{
								IsError: false,
								Content: []mcp.Content{&mcp.TextContent{Text: resultText}},
							}
						}
					} else {
						v2Logger.Warn(fmt.Sprintf("🔧 [TOOL_EXECUTION] Tool '%s' not found in customTools map (map has %d tools) - attempting MCP client call", tc.FunctionCall.Name, len(a.customTools)))
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
					v2Logger.Debug("Tool call timed out",
						loggerv2.Int("turn", turn+1),
						loggerv2.String("tool_name", tc.FunctionCall.Name),
						loggerv2.String("timeout", toolTimeout.String()))
				}

				if agentCtx.Err() != nil {
					v2Logger.Debug("Tool call context error",
						loggerv2.Int("turn", turn+1),
						loggerv2.String("tool_name", tc.FunctionCall.Name),
						loggerv2.Error(agentCtx.Err()))
				}

				// Handle tool execution errors gracefully - provide feedback to LLM and continue
				if toolErr != nil {
					// 🔧 ENHANCED ERROR RECOVERY HANDLING
					errorRecoveryHandler := NewErrorRecoveryHandler(a)

					// Attempt error recovery for recoverable errors
					recoveredResult, recoveredDuration, wasRecovered, recoveredErr := errorRecoveryHandler.HandleError(
						ctx, &tc, serverName, toolErr, startTime, isCustomTool, isVirtualTool(tc.FunctionCall.Name))

					if wasRecovered && recoveredErr == nil {
						// Successfully recovered - use recovered result and continue normal flow
						v2Logger.Debug("Successfully recovered from error for tool",
							loggerv2.String("tool", tc.FunctionCall.Name))
						result = recoveredResult
						toolErr = nil
						duration = recoveredDuration
						// Continue to normal result processing below (outside this if block)
					} else {
						// Recovery failed or not attempted - proceed with error handling
						if wasRecovered {
							v2Logger.Error("Recovery failed for tool", recoveredErr,
								loggerv2.String("tool", tc.FunctionCall.Name))
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
					resultText = mcpclient.ToolResultAsString(result)

					// Ensure resultText is never empty when sending to LLM
					// This is a safety check for all tool types (virtual, custom, MCP)
					if resultText == "" && !result.IsError {
						v2Logger.Warn("Tool returned empty result - using default message",
							loggerv2.String("tool", tc.FunctionCall.Name))
						resultText = fmt.Sprintf("Tool '%s' executed successfully but returned no output.", tc.FunctionCall.Name)
					}

					// 🔧 BROKEN PIPE DETECTION IN RESULT CONTENT (regardless of IsError flag)
					// Check for broken pipe errors in content text, even when IsError is false
					// This handles cases where the MCP server returns broken pipe errors in content rather than as error flags
					if mcpclient.IsBrokenPipeInContent(resultText) {
						v2Logger.Info(fmt.Sprintf("🔧 [BROKEN PIPE DETECTED IN RESULT] Turn %d, Tool: %s, Server: %s, IsError: %v - Attempting immediate connection recreation", turn+1, tc.FunctionCall.Name, serverName, result.IsError))

						// Create error recovery handler
						errorRecoveryHandler := NewErrorRecoveryHandler(a)

						// Create a fake error for the recovery handler
						fakeErr := fmt.Errorf("broken pipe detected in result: %s", resultText)

						// Attempt error recovery
						recoveredResult, recoveredDuration, wasRecovered, recoveredErr := errorRecoveryHandler.HandleError(
							ctx, &tc, serverName, fakeErr, startTime, isCustomTool, isVirtualTool(tc.FunctionCall.Name))

						if wasRecovered && recoveredErr == nil {
							v2Logger.Debug("Broken pipe recovery successful for tool",
								loggerv2.String("tool", tc.FunctionCall.Name))
							result = recoveredResult
							duration = recoveredDuration
							resultText = mcpclient.ToolResultAsString(result)
						} else if wasRecovered {
							v2Logger.Error("Broken pipe recovery failed for tool", recoveredErr,
								loggerv2.String("tool", tc.FunctionCall.Name))
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
							v2Logger.Error("Panic while appending tool result message", fmt.Errorf("%v", r))
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
				// Only emit ToolCallEndEvent if result is not an error (errors should emit ToolCallErrorEvent)
				if result == nil || !result.IsError {
					// Emit tool call end event using typed event data (consolidated - contains all tool information)
					toolEndEvent := events.NewToolCallEndEvent(turn+1, tc.FunctionCall.Name, resultText, serverName, duration, "")
					a.EmitTypedEvent(ctx, toolEndEvent)
				} else if result.IsError {
					// Result contains an error - emit tool call error event
					// This handles the case where tool execution succeeded but the tool returned an error result
					toolErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, resultText, serverName, duration)
					a.EmitTypedEvent(ctx, toolErrorEvent)
				}

				// Note: Removed redundant tool_output and tool_response events
				// tool_call_end now contains all necessary tool information

			}

			// After processing all tool calls, continue to next turn
			// The messages slice now includes any user messages added by read_image
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
			v2Logger.Debug("No tool calls detected, returning final answer", loggerv2.Int("turn", turn+1))

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
			a.EndAgentSession(ctx, time.Since(conversationStartTime))

			return choice.Content, messages, nil
		}
	}

	// Max turns reached - give agent one final chance to provide a proper answer
	v2Logger.Debug("Max turns reached, giving agent final chance to provide answer",
		loggerv2.Int("max_turns", a.MaxTurns))

	// Emit max turns reached event
	maxTurnsEvent := events.NewMaxTurnsReachedEvent(a.MaxTurns, a.MaxTurns, lastUserMessage, "You are out of turns, you need to generate final now. Please provide your final answer based on what you have accomplished so far. If your task is not complete, please provide a summary of what you have accomplished so far and what is missing.", string(a.AgentMode), time.Since(conversationStartTime))
	a.EmitTypedEvent(ctx, maxTurnsEvent)

	// Add a user message asking for final answer
	finalUserMessage := llmtypes.MessageContent{
		Role: llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{
			llmtypes.TextContent{
				Text: "You are out of turns, you need to generate a final answer now. Please provide your final answer based on what you have accomplished so far.",
			},
		},
	}

	// Add the final user message to the conversation
	messages = append(messages, finalUserMessage)

	// Emit user message event for the final request
	finalUserMessageEvent := events.NewUserMessageEvent(a.MaxTurns+1, "You are out of turns, you need to generate final now. Please provide your final answer based on what you have accomplished so far.", "user")
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

	finalResp, finalUsage, err := GenerateContentWithRetry(a, ctx, messages, finalOpts, a.MaxTurns+1, func(msg string) {
		// Optional: stream the final response
	})

	// Log finalUsage for debugging
	v2Logger.Info(fmt.Sprintf("🔍 [FINAL LLM CALL DEBUG] finalUsage from GenerateContentWithRetry:"))
	v2Logger.Info(fmt.Sprintf("   InputTokens: %d, OutputTokens: %d, TotalTokens: %d, Unit: %s",
		finalUsage.InputTokens, finalUsage.OutputTokens, finalUsage.TotalTokens, finalUsage.Unit))
	if finalResp != nil && len(finalResp.Choices) > 0 {
		if finalResp.Choices[0].GenerationInfo != nil {
			genInfo := finalResp.Choices[0].GenerationInfo
			v2Logger.Info(fmt.Sprintf("   GenerationInfo available:"))
			if genInfo.InputTokens != nil {
				v2Logger.Info(fmt.Sprintf("      InputTokens: %d", *genInfo.InputTokens))
			}
			if genInfo.OutputTokens != nil {
				v2Logger.Info(fmt.Sprintf("      OutputTokens: %d", *genInfo.OutputTokens))
			}
			if genInfo.TotalTokens != nil {
				v2Logger.Info(fmt.Sprintf("      TotalTokens: %d", *genInfo.TotalTokens))
			}
			if genInfo.CachedContentTokens != nil {
				v2Logger.Info(fmt.Sprintf("      CachedContentTokens: %d", *genInfo.CachedContentTokens))
			}
			if genInfo.ReasoningTokens != nil {
				v2Logger.Info(fmt.Sprintf("      ReasoningTokens: %d", *genInfo.ReasoningTokens))
			}
			if genInfo.CacheDiscount != nil {
				v2Logger.Info(fmt.Sprintf("      CacheDiscount: %.2f%%", *genInfo.CacheDiscount*100))
			}
			if len(genInfo.Additional) > 0 {
				v2Logger.Info(fmt.Sprintf("      Additional fields:"))
				for key, value := range genInfo.Additional {
					if strings.Contains(strings.ToLower(key), "cache") || strings.Contains(strings.ToLower(key), "token") {
						v2Logger.Info(fmt.Sprintf("         %s: %v", key, value))
					}
				}
			}
		} else {
			v2Logger.Info(fmt.Sprintf("   GenerationInfo is nil"))
		}
	} else {
		v2Logger.Info(fmt.Sprintf("   finalResp is nil or has no choices"))
	}

	// Accumulate token usage from final LLM call
	if finalResp != nil && len(finalResp.Choices) > 0 && finalUsage.TotalTokens > 0 {
		a.accumulateTokenUsage(ctx, events.UsageMetrics{
			PromptTokens:     finalUsage.InputTokens,
			CompletionTokens: finalUsage.OutputTokens,
			TotalTokens:      finalUsage.TotalTokens,
		}, finalResp, a.MaxTurns+1)
	} else {
		choicesCount := 0
		if finalResp != nil {
			choicesCount = len(finalResp.Choices)
		}
		v2Logger.Warn("Skipping token accumulation",
			loggerv2.Any("final_resp_exists", finalResp != nil),
			loggerv2.Int("choices", choicesCount),
			loggerv2.Int("total_tokens", finalUsage.TotalTokens))
	}

	if err != nil {
		// If the final call also fails, emit error event
		conversationErrorEvent := &events.ConversationErrorEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Question: lastUserMessage,
			Error:    "max turns reached and final attempt failed",
			Turn:     a.MaxTurns + 1,
			Context:  "conversation",
			Duration: time.Since(conversationStartTime),
		}
		a.EmitTypedEvent(ctx, conversationErrorEvent)

		if lastResponse != "" {
			v2Logger.Debug("Forced FINAL_ANSWER due to max turns")

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
				a.MaxTurns+1,                      // turns (+1 for the final turn)
			)
			a.EmitTypedEvent(ctx, unifiedCompletionEvent)

			// NEW: End agent session for hierarchy tracking
			a.EndAgentSession(ctx, time.Since(conversationStartTime))

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
		v2Logger.Warn("Exiting with no final answer after max turns",
			loggerv2.Int("max_turns", a.MaxTurns))

		// 🎯 FIX: End the trace for max turns error - replaced with event emission
		maxTurnsErrorEvent := events.NewConversationErrorEvent(lastUserMessage, fmt.Sprintf("max turns (%d) reached without final answer", a.MaxTurns), a.MaxTurns+1, "max_turns_exceeded", time.Since(conversationStartTime))
		a.EmitTypedEvent(ctx, maxTurnsErrorEvent)

		return "", messages, fmt.Errorf("max turns (%d) reached without final answer", a.MaxTurns)
	}

	if finalResp == nil || finalResp.Choices == nil || len(finalResp.Choices) == 0 {
		v2Logger.Warn("Final call returned no response choices")

		// 🎯 FIX: End the trace for final call error - replaced with event emission
		finalCallErrorEvent := events.NewConversationErrorEvent(lastUserMessage, "final call returned no response choices", a.MaxTurns+1, "no_final_choices", time.Since(conversationStartTime))
		a.EmitTypedEvent(ctx, finalCallErrorEvent)

		return "", messages, fmt.Errorf("final call returned no response choices")
	}

	finalChoice := finalResp.Choices[0]

	// Token usage is already included in the LLMGenerationEndEvent above

	// Note: LLM generation end event is already emitted in the main conversation flow
	// No need to emit it again here to avoid duplication

	// Simple agent - use final choice content directly
	v2Logger.Debug("Final answer provided after max turns")

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
	a.EndAgentSession(ctx, time.Since(conversationStartTime))

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
