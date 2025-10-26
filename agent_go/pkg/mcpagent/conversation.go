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

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpcache"
	"mcp-agent/agent_go/pkg/mcpclient"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/tmc/langchaingo/llms"
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
func ensureSystemPrompt(a *Agent, messages []llms.MessageContent) []llms.MessageContent {
	// Check if the first message is already a system message
	if len(messages) > 0 && messages[0].Role == llms.ChatMessageTypeSystem {
		return messages
	}

	// Check if there's already a system message anywhere in the conversation
	for _, msg := range messages {
		if msg.Role == llms.ChatMessageTypeSystem {
			// System message already exists, don't add another one
			return messages
		}
	}

	// Use the agent's existing system prompt (which should already be correct for the mode)
	systemPrompt := a.SystemPrompt

	// Create system message
	systemMessage := llms.MessageContent{
		Role:  llms.ChatMessageTypeSystem,
		Parts: []llms.ContentPart{llms.TextContent{Text: systemPrompt}},
	}

	// Prepend system message to the beginning
	return append([]llms.MessageContent{systemMessage}, messages...)
}

// AskWithHistory runs an interaction using the provided message history (multi-turn conversation).
func AskWithHistory(a *Agent, ctx context.Context, messages []llms.MessageContent) (string, []llms.MessageContent, error) {
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
	logger.Infof("🔍 HIERARCHY DEBUG: Initialized hierarchy for context - Level=%d, ParentID=%s",
		a.currentHierarchyLevel, a.currentParentEventID)

	// Ensure system prompt is included in messages
	messages = ensureSystemPrompt(a, messages)

	// NEW: Set current query for hierarchy tracking (will be set later when lastUserMessage is extracted)

	// Add cache validation AFTER the agent is fully initialized
	if a.Tracers != nil && len(a.Tracers) > 0 && len(a.Clients) > 0 {
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
		if messages[i].Role == llms.ChatMessageTypeHuman {
			// Get the text content from the message
			for _, part := range messages[i].Parts {
				if textPart, ok := part.(llms.TextContent); ok {
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

	// For ReAct agents, emit reasoning start event
	if a.AgentMode == ReActAgent {
		reactStartEvent := events.NewReActReasoningStartEvent(0, lastUserMessage)
		a.EmitTypedEvent(ctx, reactStartEvent)
	}

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
			logger.Warnf("Smart routing failed, using all tools: %v", err)
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

		// 🔍 DEBUG: Log the current state of messages array
		logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: Messages array has %d messages", turn+1, len(messages))

		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: Last message role: %s, has %d parts", turn+1, lastMsg.Role, len(lastMsg.Parts))

			for i, part := range lastMsg.Parts {
				logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: Part %d type: %T", turn+1, i+1, part)
				if textPart, ok := part.(llms.TextContent); ok {
					lastMessage = textPart.Text
					logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: Found text content: %s", turn+1, truncateString(lastMessage, 100))
					break
				} else if toolResp, ok := part.(llms.ToolCallResponse); ok {
					logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: Found tool response: %s", turn+1, truncateString(toolResp.Content, 100))
					lastMessage = toolResp.Content
					break
				} else if toolCall, ok := part.(llms.ToolCall); ok {
					logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: Found tool call: %s", turn+1, toolCall.FunctionCall.Name)
					lastMessage = fmt.Sprintf("Tool call: %s", toolCall.FunctionCall.Name)
					break
				}
			}

			if lastMessage == "" {
				logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: No text content found in last message parts", turn+1)
			}
		} else {
			logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: Messages array is empty", turn+1)
		}

		// If no message found, use the last user message as fallback
		if lastMessage == "" {
			lastMessage = lastUserMessage
			logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: Using fallback lastUserMessage: %s", turn+1, truncateString(lastMessage, 100))
		}

		// Emit conversation turn event using typed event data
		logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: filteredTools count: %d", turn+1, len(a.filteredTools))
		logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: toolToServer count: %d", turn+1, len(a.toolToServer))
		tools := events.ConvertToolsToToolInfo(a.filteredTools, a.toolToServer)
		logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: Converted tools count: %d", turn+1, len(tools))
		conversationTurnEvent := events.NewConversationTurnEvent(turn+1, lastMessage, len(messages), false, 0, tools, messages)
		a.EmitTypedEvent(ctx, conversationTurnEvent)
		logger.Infof("[CONVERSATION_TURN DEBUG] Turn %d: Emitted conversation_turn event with lastMessage: %s", turn+1, truncateString(lastMessage, 100))

		// Check for context cancellation at the start of each turn
		if agentCtx.Err() != nil {
			// Use agent's logger if available, otherwise use default
			logger := getLogger(a)
			logger.Infof("Context cancelled at start of turn - turn: %d, error: %s, duration: %s", turn+1, agentCtx.Err().Error(), time.Since(conversationStartTime).String())
			return "", messages, fmt.Errorf("conversation cancelled: %w", agentCtx.Err())
		}

		logger.Infof("[AGENT TRACE] AskWithHistory: turn %d loop entry", turn+1)
		logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Calling LLM.GenerateContent with %d filtered tools (out of %d total)", turn+1, len(a.filteredTools), len(a.Tools))

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
						if textPart, ok := part.(llms.TextContent); ok {
							contentLength += len(textPart.Text)
						}
					}
				}
				logger.Infof("[TURN 2 DEBUG] 📤 Message %d - Role: %s, Content length: %d", i+1, msg.Role, contentLength)
			}
		}

		// Before calling LLM.GenerateContent, log the message history as JSON
		if msgBytes, err := json.MarshalIndent(llmMessages, "", "  "); err == nil {
			// Use agent's logger if available, otherwise use default
			logger := getLogger(a)
			logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, message history before LLM call:\n%s", turn+1, string(msgBytes))
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

		opts := []llms.CallOption{}
		if !llm.IsO3O4Model(a.ModelID) {
			opts = append(opts, llms.WithTemperature(a.Temperature))
		}

		// Set a reasonable default max_tokens to prevent immediate completion
		// Use environment variable if available, otherwise default to 4000 tokens
		maxTokens := 40000 // Default value
		if maxTokensEnv := os.Getenv("ORCHESTRATOR_MAIN_LLM_MAX_TOKENS"); maxTokensEnv != "" {
			if parsed, err := strconv.Atoi(maxTokensEnv); err == nil && parsed > 0 {
				maxTokens = parsed
			}
		}
		opts = append(opts, llms.WithMaxTokens(maxTokens))

		// 🆕 OPTIONS DEBUGGING
		logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - LLM options: %d, MaxTokens: %d", turn+1, len(opts), maxTokens)

		// Use proper LLM function calling via llms.WithTools()
		// Use the pre-filtered tools that were determined at conversation start
		if len(a.filteredTools) > 0 {
			opts = append(opts, llms.WithTools(a.filteredTools))
			if toolChoiceOpt := ConvertToolChoice(a.ToolChoice); toolChoiceOpt != nil {
				opts = append(opts, llms.WithToolChoice(toolChoiceOpt))
			}
			logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Added %d tools to LLM options via llms.WithTools()", turn+1, len(a.filteredTools))
		}
		toolNames := make([]string, len(a.filteredTools))
		for i, tool := range a.filteredTools {
			toolNames[i] = tool.Function.Name
		}

		// Emit LLM Messages event to track what's being sent to the LLM
		// Build tool context from previous tool calls in this conversation
		var toolContext []events.ToolContext
		for _, msg := range messages {
			if msg.Role == llms.ChatMessageTypeTool {
				for _, part := range msg.Parts {
					if toolResp, ok := part.(llms.ToolCallResponse); ok {
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

		// 🆕 DETAILED GENERATECONTENTWITHRETRY DEBUGGING
		logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - About to call GenerateContentWithRetry - Time: %v", turn+1, time.Now())
		logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - GenerateContentWithRetry params - Messages: %d, Options: %d", turn+1, len(llmMessages), len(opts))
		logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - Agent details - Provider: %s, Model: %s", turn+1, string(a.GetProvider()), a.ModelID)
		logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - Context check - Err: %v, Done: %v", turn+1, ctx.Err(), ctx.Done())

		// Use GenerateContentWithRetry for robust fallback handling
		generateContentStart := time.Now()
		logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - Calling GenerateContentWithRetry NOW - Time: %v", turn+1, generateContentStart)

		resp, genErr, usage := GenerateContentWithRetry(a, ctx, llmMessages, opts, turn, func(msg string) {
			// Use agent's logger if available, otherwise use default
			logger := getLogger(a)
			logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: %s", turn+1, msg)

			// For ReAct agents, track reasoning in real-time
			if a.AgentMode == ReActAgent {
				// Create reasoning tracker if not already created
				if a.reasoningTracker == nil {
					a.reasoningTracker = NewReActReasoningTracker(a, ctx, turn)
				}
				// Process the chunk for reasoning detection
				a.reasoningTracker.ProcessChunk(msg)
			}
		})
		generateContentDuration := time.Since(generateContentStart)

		// 🆕 POST-GENERATECONTENTWITHRETRY DEBUGGING
		logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - GenerateContentWithRetry completed - Duration: %v, Error: %v", turn+1, generateContentDuration, genErr != nil)
		if genErr != nil {
			logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - GenerateContentWithRetry failed - Error: %v, Error type: %T", turn+1, genErr, genErr)
		} else {
			logger.Infof("💬 [DEBUG] CONVERSATION Turn %d - GenerateContentWithRetry succeeded - Response: %v, Usage: %+v", turn+1, resp != nil, usage)
		}

		// NEW: End LLM generation for hierarchy tracking
		if resp != nil && len(resp.Choices) > 0 {
			a.EndLLMGeneration(ctx, resp.Choices[0].Content, turn+1, len(resp.Choices[0].ToolCalls), time.Since(llmStartTime), events.UsageMetrics{
				PromptTokens:     usage.InputTokens,
				CompletionTokens: usage.OutputTokens,
				TotalTokens:      usage.TotalTokens,
			})
		}

		// 🆕 ENHANCED TURN 2 RESULT LOGGING
		if turn+1 == 2 {
			// Use agent's logger if available, otherwise use default
			logger := getLogger(a)
			logger.Infof("[TURN 2 DEBUG] 🔍 Turn 2 LLM generation completed")
			logger.Infof("[TURN 2 DEBUG] 🔍 Response: %v", resp != nil)
			logger.Infof("[TURN 2 DEBUG] 🔍 Error: %v", genErr)
			if resp != nil {
				logger.Infof("[TURN 2 DEBUG] 🔍 Response choices: %d", len(resp.Choices))
				if len(resp.Choices) > 0 {
					logger.Infof("[TURN 2 DEBUG] 🔍 First choice content length: %d", len(resp.Choices[0].Content))
					logger.Infof("[TURN 2 DEBUG] 🔍 First choice content preview: %s", truncateString(resp.Choices[0].Content, 100))
				}
			}
		}

		// Use agent's logger if available, otherwise use default
		logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: LLM.GenerateContent returned (err=%v, resp=%v)", turn+1, genErr, resp != nil)

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
			logger.Errorf("[AGENT TRACE] AskWithHistory: turn %d, returning early due to no response choices", turn+1)

			// 🎯 FIX: End the trace for error cases - replaced with event emission
			conversationErrorEvent := events.NewConversationErrorEvent(lastUserMessage, "no response choices returned", turn+1, "no_choices", time.Since(conversationStartTime))
			a.EmitTypedEvent(ctx, conversationErrorEvent)

			return "", messages, fmt.Errorf("no response choices returned")
		}

		choice := resp.Choices[0]
		lastResponse = choice.Content
		logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, LLM response content: %s", turn+1, choice.Content)

		// LLM generation end event is already emitted by EndLLMGeneration() method above

		// For ReAct agents, reasoning is finalized in ProcessChunk when completion patterns are detected
		// No need to call FinalizeReasoning as it's handled automatically

		// Token usage is already included in the LLMGenerationEndEvent above

		if len(choice.ToolCalls) > 0 {
			logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, detected %d tool calls", turn+1, len(choice.ToolCalls))

			// 1. Append the AI message (with tool_call) to the history
			assistantParts := []llms.ContentPart{}
			if choice.Content != "" {
				assistantParts = append(assistantParts, llms.TextContent{Text: choice.Content})
			}
			for _, tc := range choice.ToolCalls {
				assistantParts = append(assistantParts, tc)
			}
			messages = append(messages, llms.MessageContent{Role: llms.ChatMessageTypeAI, Parts: assistantParts})

			// 2. For each tool call, execute and append the tool result as a new message
			for i, tc := range choice.ToolCalls {
				logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Preparing to execute tool call %d: %s", turn+1, i+1, tc.FunctionCall.Name)

				// Determine server name for tool call events
				serverName := a.toolToServer[tc.FunctionCall.Name]
				if isVirtualTool(tc.FunctionCall.Name) {
					serverName = "virtual-tools"
				}

				// Debug: Check what arguments we're getting
				getLogger(a).Info("🔧 DEBUG: Tool Call Arguments", map[string]interface{}{
					"tool_name":        tc.FunctionCall.Name,
					"arguments":        tc.FunctionCall.Arguments,
					"arguments_length": len(tc.FunctionCall.Arguments),
				})

				// Emit tool call start event using typed event data with correlation
				toolStartEvent := events.NewToolCallStartEventWithCorrelation(turn+1, tc.FunctionCall.Name, events.ToolParams{
					Arguments: tc.FunctionCall.Arguments,
				}, serverName, traceID, traceID) // Using traceID for both traceID and parentID correlation

				// 🔧 DEBUG: Log tool call start event before emission
				logger.Infof("[TOOL CALL START DEBUG] About to emit tool call start event: %s, type: %s", tc.FunctionCall.Name, toolStartEvent.GetEventType())
				a.EmitTypedEvent(ctx, toolStartEvent)
				logger.Infof("[TOOL CALL START DEBUG] Tool call start event emitted successfully: %s", tc.FunctionCall.Name)

				// 🔧 COMPREHENSIVE TOOL CALL START LOGGING
				getLogger(a).Info("🔧 TOOL CALL START LOGGING", map[string]interface{}{
					"turn":            turn + 1,
					"tool_name":       tc.FunctionCall.Name,
					"tool_call_id":    tc.ID,
					"tool_arguments":  tc.FunctionCall.Arguments,
					"server_name":     serverName,
					"span_id":         "",
					"is_virtual_tool": isVirtualTool(tc.FunctionCall.Name),
				})

				// Log the complete tool call start for debugging
				logger.Infof("[TOOL START LOG] Turn %d, Tool: %s, Arguments: %s", turn+1, tc.FunctionCall.Name, tc.FunctionCall.Arguments)
				logger.Infof("[TOOL START LOG] Turn %d, Tool: %s, Server: %s, Virtual: %v", turn+1, tc.FunctionCall.Name, serverName, isVirtualTool(tc.FunctionCall.Name))

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
					toolNameErrorEvent := events.NewToolCallErrorEvent(turn+1, "", fmt.Sprintf("empty tool name"), "", time.Since(conversationStartTime))
					a.EmitTypedEvent(ctx, toolNameErrorEvent)

					// Add feedback to conversation so LLM can correct itself
					messages = append(messages, llms.MessageContent{
						Role:  llms.ChatMessageTypeTool,
						Parts: []llms.ContentPart{llms.ToolCallResponse{ToolCallID: tc.ID, Name: "", Content: feedbackMessage}},
					})

					logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Added empty tool name feedback to conversation, continuing", turn+1)
					continue
				}
				logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: About to parse tool arguments: %s", turn+1, tc.FunctionCall.Arguments)
				args, err := mcpclient.ParseToolArguments(tc.FunctionCall.Arguments)
				logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Finished parsing tool arguments. Args: %v, Err: %v", turn+1, args, err)
				if err != nil {
					logger.Errorf("[AGENT DEBUG] AskWithHistory Tool args parsing error: %v", err)

					// 🔧 ENHANCED: Instead of failing, provide feedback to LLM for self-correction
					feedbackMessage := generateToolArgsParsingFeedback(tc.FunctionCall.Name, tc.FunctionCall.Arguments, err)

					// Emit tool call error event for observability
					toolArgsParsingErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, fmt.Sprintf("parse tool args: %v", err), "", time.Since(conversationStartTime))
					a.EmitTypedEvent(ctx, toolArgsParsingErrorEvent)

					// Add feedback to conversation so LLM can correct itself
					messages = append(messages, llms.MessageContent{
						Role:  llms.ChatMessageTypeTool,
						Parts: []llms.ContentPart{llms.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: feedbackMessage}},
					})

					logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Added tool args parsing feedback to conversation, continuing", turn+1)
					continue
				}

				// 🔧 FIX: Check custom tools FIRST before MCP client lookup
				// Custom tools don't need MCP clients, so check them early
				isCustomTool := false
				if a.customTools != nil {
					if _, exists := a.customTools[tc.FunctionCall.Name]; exists {
						logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Found custom tool: %s, skipping MCP client lookup", turn+1, tc.FunctionCall.Name)
						isCustomTool = true
					}
				}

				client := a.Client
				if a.toolToServer != nil {
					if mapped, ok := a.toolToServer[tc.FunctionCall.Name]; ok {
						logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Tool %s mapped to server: %s", turn+1, tc.FunctionCall.Name, mapped)
						if a.Clients != nil {
							if c, exists := a.Clients[mapped]; exists {
								client = c
								logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Using client for server: %s", turn+1, mapped)
							} else {
								logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Client not found for server: %s, using default client", turn+1, mapped)
							}
						} else {
							logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: No clients map available, using default client", turn+1)
						}
					} else {
						logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Tool %s not mapped to any server, using default client", turn+1, tc.FunctionCall.Name)
					}
				} else {
					logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: No toolToServer map available, using default client", turn+1)
				}
				// Only check for client errors for non-custom tools and non-virtual tools
				if !isCustomTool && !isVirtualTool(tc.FunctionCall.Name) && client == nil {
					// Check if we're in cache-only mode with no active connections
					if len(a.Clients) == 0 {
						logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Cache-only mode detected - creating on-demand connection for tool %s", turn+1, tc.FunctionCall.Name)

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
							messages = append(messages, llms.MessageContent{
								Role:  llms.ChatMessageTypeTool,
								Parts: []llms.ContentPart{llms.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: feedbackMessage}},
							})

							logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Added tool not found feedback to conversation, continuing", turn+1)
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
						logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Created on-demand connection for server %s", turn+1, serverName)
					} else {
						logger.Errorf("[AGENT DEBUG] AskWithHistory Early return: no MCP client found for tool %s", tc.FunctionCall.Name)

						// 🎯 FIX: End the trace for no MCP client error - replaced with event emission
						conversationErrorEvent := events.NewConversationErrorEvent(lastUserMessage, fmt.Sprintf("no MCP client found for tool %s", tc.FunctionCall.Name), turn+1, "no_mcp_client", time.Since(conversationStartTime))
						a.EmitTypedEvent(ctx, conversationErrorEvent)

						err := fmt.Errorf("no MCP client found for tool %s", tc.FunctionCall.Name)
						return "", messages, err
					}
				}

				// Log client type for debugging (only for non-custom tools)
				if !isCustomTool {
					clientType := "Client"
					logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Using Client for tool %s", turn+1, tc.FunctionCall.Name)
					logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Client type: %s for tool %s", turn+1, clientType, tc.FunctionCall.Name)
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

				logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: About to call tool '%s' with args: %v (timeout: %s)", turn+1, tc.FunctionCall.Name, args, toolTimeout.String())
				startTime := time.Now()

				// Add cache hit event during tool execution to show cached connection usage
				if a.Tracers != nil && len(a.Tracers) > 0 && serverName != "" && serverName != "virtual-tools" {
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

					if wasRecovered {
						// Use the recovered result if recovery was successful
						if recoveredErr == nil {
							logger.Infof("🔧 [ERROR RECOVERY] Successfully recovered from error for tool '%s'", tc.FunctionCall.Name)
							result = recoveredResult
							toolErr = nil
							duration = recoveredDuration
						} else {
							logger.Errorf("🔧 [ERROR RECOVERY] Recovery failed for tool '%s': %v", tc.FunctionCall.Name, recoveredErr)
							toolErr = recoveredErr
							duration = recoveredDuration
						}
					}

					// Emit tool call error event using typed event data
					toolErrorEvent := events.NewToolCallErrorEvent(turn+1, tc.FunctionCall.Name, toolErr.Error(), serverName, duration)
					a.EmitTypedEvent(ctx, toolErrorEvent)

					// 🔧 ENHANCED TOOL ERROR LOGGING
					getLogger(a).Info("🔧 TOOL CALL ERROR LOGGING", map[string]interface{}{
						"turn":               turn + 1,
						"tool_name":          tc.FunctionCall.Name,
						"tool_call_id":       tc.ID,
						"error_message":      toolErr.Error(),
						"error_type":         fmt.Sprintf("%T", toolErr),
						"execution_duration": duration.String(),
						"server_name":        serverName,
						"context_cancelled":  agentCtx.Err() != nil,
						"was_recovered":      wasRecovered,
					})

					// Log the complete tool error for debugging
					logger.Infof("[TOOL ERROR LOG] Turn %d, Tool: %s, Error: %v", turn+1, tc.FunctionCall.Name, toolErr)
					logger.Infof("[TOOL ERROR LOG] Turn %d, Tool: %s, Error Type: %T", turn+1, tc.FunctionCall.Name, toolErr)
					logger.Infof("[TOOL ERROR LOG] Turn %d, Tool: %s, Duration: %v", turn+1, tc.FunctionCall.Name, duration)

					logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Tool call '%s' returned error after %v: %v", turn+1, tc.FunctionCall.Name, duration, toolErr)

					// Instead of failing the entire conversation, provide feedback to the LLM
					errorResultText := fmt.Sprintf("Tool execution failed - %v", toolErr)

					// Add the error result to the conversation so the LLM can continue
					messages = append(messages, llms.MessageContent{
						Role:  llms.ChatMessageTypeTool, // Use "tool" role for tool responses
						Parts: []llms.ContentPart{llms.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: errorResultText}},
					})

					// Continue to next turn instead of returning error
					logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Continuing conversation after tool error", turn+1)
					continue
				}
				logger.Infof("[AGENT DEBUG] AskWithHistory Turn %d: Tool call '%s' returned result after %v: %v", turn+1, tc.FunctionCall.Name, duration, result)
				var resultText string
				if result != nil {
					// CRITICAL DEBUG: About to process tool result in conversation.go (PATH 2)
					getLogger(a).Info("CRITICAL DEBUG: About to process tool result in conversation.go (PATH 2)", map[string]interface{}{
						"tool_name":  tc.FunctionCall.Name,
						"result_nil": result == nil,
					})

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

					// 🔧 COMPREHENSIVE TOOL OUTPUT LOGGING
					getLogger(a).Info("🔧 TOOL CALL RESPONSE LOGGING", map[string]interface{}{
						"turn":               turn + 1,
						"tool_name":          tc.FunctionCall.Name,
						"tool_call_id":       tc.ID,
						"result_text_length": len(resultText),
						"result_text_preview": func() string {
							if len(resultText) > 200 {
								return resultText[:200] + "..."
							}
							return resultText
						}(),
						"full_result_text":   resultText,
						"execution_duration": duration.String(),
						"server_name":        a.toolToServer[tc.FunctionCall.Name],
					})

					// Log the complete tool output for debugging
					logger.Infof("[TOOL OUTPUT LOG] Turn %d, Tool: %s, Response Length: %d chars", turn+1, tc.FunctionCall.Name, len(resultText))
					logger.Infof("[TOOL OUTPUT LOG] Turn %d, Tool: %s, Response Preview: %s", turn+1, func() string {
						if len(resultText) > 300 {
							return resultText[:300] + "..."
						}
						return resultText
					}())

					// CRITICAL DEBUG: Tool result processed in conversation.go (PATH 2)
					getLogger(a).Info("CRITICAL DEBUG: Tool result processed in conversation.go (PATH 2)", map[string]interface{}{
						"tool_name":          tc.FunctionCall.Name,
						"result_text_length": len(resultText),
						"result_text_empty":  resultText == "",
					})

					// Check if this is a large tool output that should be written to file
					if a.toolOutputHandler != nil {
						getLogger(a).Info("CRITICAL DEBUG: ToolOutputHandler present in conversation.go (PATH 2)", map[string]interface{}{
							"enabled":          a.toolOutputHandler.Enabled,
							"server_available": a.toolOutputHandler.ServerAvailable,
							"threshold":        a.toolOutputHandler.Threshold,
						})

						// Log tool output token count for debugging
						tokenCount := a.toolOutputHandler.CountTokensForModel(resultText, a.ModelID)
						getLogger(a).Info("📊 Tool output token count (conversation.go PATH 2)", map[string]interface{}{
							"tool_name":        tc.FunctionCall.Name,
							"content_length":   len(resultText),
							"token_count":      tokenCount,
							"threshold":        a.toolOutputHandler.Threshold,
							"is_large":         tokenCount > a.toolOutputHandler.Threshold,
							"server_available": a.toolOutputHandler.IsServerAvailable(),
						})

						// Check if this is a large tool output that should be written to file
						if a.toolOutputHandler.IsLargeToolOutputWithModel(resultText, a.ModelID) {
							getLogger(a).Info("CRITICAL DEBUG: Large tool output detected in conversation.go (PATH 2)", map[string]interface{}{
								"tool_name":      tc.FunctionCall.Name,
								"content_length": len(resultText),
								"token_count":    tokenCount,
								"threshold":      a.toolOutputHandler.Threshold,
							})

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

								getLogger(a).Info("CRITICAL DEBUG: Large tool output saved to file", map[string]interface{}{
									"tool_name":      tc.FunctionCall.Name,
									"file_path":      filePath,
									"content_length": len(resultText),
								})
							} else {
								// Emit file write error event
								fileErrorEvent := events.NewLargeToolOutputFileWriteErrorEvent(tc.FunctionCall.Name, writeErr.Error(), len(resultText))
								a.EmitTypedEvent(ctx, fileErrorEvent)

								getLogger(a).Info("CRITICAL DEBUG: Failed to write large tool output to file", map[string]interface{}{
									"tool_name": tc.FunctionCall.Name,
									"error":     writeErr.Error(),
								})
							}
						}
					} else {
						getLogger(a).Info("CRITICAL DEBUG: ToolOutputHandler is nil in conversation.go (PATH 2)", nil)
					}
				} else {
					resultText = "Tool execution completed but no result returned"
				}
				// 3. Append the tool result as a new message (after the AI tool_call message)
				logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, about to append tool result message to history", turn+1)
				// Add recover block to catch panics
				func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Errorf("[AGENT ERROR] Panic while appending tool result message: %v", r)
						}
					}()
					// Use the exact tool call ID from the LLM response
					messages = append(messages, llms.MessageContent{
						Role:  llms.ChatMessageTypeTool, // Use "tool" role for tool responses
						Parts: []llms.ContentPart{llms.ToolCallResponse{ToolCallID: tc.ID, Name: tc.FunctionCall.Name, Content: resultText}},
					})
				}()
				logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, tool result message appended to history", turn+1)
				logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, end of tool call handling for tool %s", turn+1, tc.FunctionCall.Name)

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

				logger.Infof("Finished tool call - turn: %d, tool_call_index: %d, tool_name: %s", turn+1, i+1, tc.FunctionCall.Name)
			}

			logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, continuing to next turn after tool calls", turn+1)
			continue
		} else {
			// No tool calls - add the assistant response to conversation history
			// This is CRITICAL to prevent conversation loops
			if choice.Content != "" {
				assistantMessage := llms.MessageContent{
					Role:  llms.ChatMessageTypeAI,
					Parts: []llms.ContentPart{llms.TextContent{Text: choice.Content}},
				}
				messages = append(messages, assistantMessage)
				logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, added assistant response to conversation history (no tool calls)", turn+1)
			}

			// Check if this is a ReAct agent and if it has a completion pattern
			if a.AgentMode == ReActAgent {
				if IsReActCompletion(choice.Content) {
					logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, ReAct completion detected, returning full reasoning process", turn+1)

					// 🆕 SIMPLIFIED: No need to parse reasoning steps since we're emitting real-time events
					// The reasoning tracker already emitted all the reasoning step events

					// Emit ReAct reasoning end event
					reactEndEvent := events.NewReActReasoningEndEvent(turn+1, choice.Content, 0, "Real-time reasoning events were emitted during generation")
					a.EmitTypedEvent(ctx, reactEndEvent)

					// Emit unified completion event
					unifiedCompletionEvent := events.NewUnifiedCompletionEvent(
						"react",                           // agentType
						string(a.AgentMode),               // agentMode
						lastUserMessage,                   // question
						choice.Content,                    // finalResult
						"completed",                       // status
						time.Since(conversationStartTime), // duration
						turn+1,                            // turns
					)
					a.EmitTypedEvent(ctx, unifiedCompletionEvent)

					// Agent end event removed - no longer needed

					// Agent processing end event removed - no longer needed

					// NEW: End agent session for hierarchy tracking
					a.EndAgentSession(ctx)

					// Append the final response to messages array for consistency
					if choice.Content != "" {
						assistantMessage := llms.MessageContent{
							Role:  llms.ChatMessageTypeAI,
							Parts: []llms.ContentPart{llms.TextContent{Text: choice.Content}},
						}
						messages = append(messages, assistantMessage)
					}

					// Return the FULL reasoning process, not just the final answer
					return choice.Content, messages, nil
				} else {
					// ReAct agent without completion pattern - continue to next turn
					// Note: Assistant response already added to history in the main else block above
					logger.Infof("[AGENT TRACE] AskWithHistory: turn %d, ReAct agent without completion pattern, continuing to next turn", turn+1)
					continue
				}
			} else {
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
	}

	// Max turns reached - give agent one final chance to provide a proper answer
	logger.Infof("[AGENT TRACE] AskWithHistory: max turns (%d) reached, giving agent final chance to provide answer.", a.MaxTurns)

	// Emit max turns reached event
	maxTurnsEvent := events.NewMaxTurnsReachedEvent(a.MaxTurns, a.MaxTurns, lastUserMessage, "You are out of turns, you need to generate final now. Please provide your final answer based on what you have accomplished so far.", string(a.AgentMode), time.Since(conversationStartTime))
	a.EmitTypedEvent(ctx, maxTurnsEvent)

	// Add a user message asking for final answer
	finalUserMessage := llms.MessageContent{
		Role: llms.ChatMessageTypeHuman,
		Parts: []llms.ContentPart{
			llms.TextContent{
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
	var finalResp *llms.ContentResponse
	var err error

	// Create options for final call with reasonable max_tokens
	maxTokens := 40000 // Default value
	if maxTokensEnv := os.Getenv("ORCHESTRATOR_MAIN_LLM_MAX_TOKENS"); maxTokensEnv != "" {
		if parsed, err := strconv.Atoi(maxTokensEnv); err == nil && parsed > 0 {
			maxTokens = parsed
		}
	}

	finalOpts := []llms.CallOption{
		llms.WithMaxTokens(maxTokens), // Set reasonable default for final answer
	}
	if !llm.IsO3O4Model(a.ModelID) {
		finalOpts = append(finalOpts, llms.WithTemperature(a.Temperature))
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
				assistantMessage := llms.MessageContent{
					Role:  llms.ChatMessageTypeAI,
					Parts: []llms.ContentPart{llms.TextContent{Text: lastResponse}},
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

	// Check if this is a ReAct agent and extract final answer
	if a.AgentMode == ReActAgent {
		finalAnswer := ExtractFinalAnswer(finalChoice.Content)
		if finalAnswer != "" {
			logger.Infof("[AGENT TRACE] AskWithHistory: final answer provided after max turns: %s", finalAnswer)

			// Emit unified completion event
			unifiedCompletionEvent := events.NewUnifiedCompletionEvent(
				"react",                           // agentType
				string(a.AgentMode),               // agentMode
				lastUserMessage,                   // question
				finalChoice.Content,               // finalResult
				"completed",                       // status
				time.Since(conversationStartTime), // duration
				a.MaxTurns+1,                      // turns (+1 for the final turn)
			)
			a.EmitTypedEvent(ctx, unifiedCompletionEvent)

			// Agent end event removed - no longer needed

			// Unified completion event already emitted above

			// NEW: End agent session for hierarchy tracking
			a.EndAgentSession(ctx)

			// Append the final response to messages array for consistency
			if finalChoice.Content != "" {
				assistantMessage := llms.MessageContent{
					Role:  llms.ChatMessageTypeAI,
					Parts: []llms.ContentPart{llms.TextContent{Text: finalChoice.Content}},
				}
				messages = append(messages, assistantMessage)
			}

			// Return the FULL reasoning process, not just the final answer
			return finalChoice.Content, messages, nil
		}
	}

	// For simple agents or if no final answer pattern found, return the content as-is
	logger.Infof("[AGENT TRACE] AskWithHistory: final answer provided after max turns: %s", finalChoice.Content)

	// Emit unified completion event for simple agents or fallback cases
	unifiedCompletionEvent := events.NewUnifiedCompletionEvent(
		"simple",                          // agentType (fallback for simple agents)
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
		assistantMessage := llms.MessageContent{
			Role:  llms.ChatMessageTypeAI,
			Parts: []llms.ContentPart{llms.TextContent{Text: finalChoice.Content}},
		}
		messages = append(messages, assistantMessage)
	}

	return finalChoice.Content, messages, nil
}

// truncateString truncates a string to the specified length and adds "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
