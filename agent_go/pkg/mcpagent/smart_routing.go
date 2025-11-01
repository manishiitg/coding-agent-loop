package mcpagent

import (
	"context"
	"encoding/json"
	"fmt"
	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/pkg/events"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/llmtypes"
)

// Smart routing detection
func (a *Agent) shouldUseSmartRouting() bool {
	logger := a.Logger

	// In cache-only mode, count unique servers from tool-to-server mapping; otherwise use active clients count
	var serverCount int
	if a.CacheOnly {
		// Count unique servers from tool-to-server mapping
		serverSet := make(map[string]bool)
		for _, serverName := range a.toolToServer {
			serverSet[serverName] = true
		}
		serverCount = len(serverSet)
		logger.Infof("🔧 DEBUG: shouldUseSmartRouting - Cache-only mode: toolToServer entries=%d, unique servers=%d", len(a.toolToServer), serverCount)
	} else {
		serverCount = len(a.Clients) // Use active clients count
		logger.Infof("🔧 DEBUG: shouldUseSmartRouting - Active mode: Clients count=%d", serverCount)
	}

	result := a.EnableSmartRouting &&
		len(a.Tools) > a.SmartRoutingThreshold.MaxTools &&
		serverCount > a.SmartRoutingThreshold.MaxServers

	logger.Infof("🔧 DEBUG: shouldUseSmartRouting result=%v (EnableSmartRouting=%v, Tools=%d>%d, Servers=%d>%d)",
		result, a.EnableSmartRouting, len(a.Tools), a.SmartRoutingThreshold.MaxTools, serverCount, a.SmartRoutingThreshold.MaxServers)

	return result
}

// Build conversation context for smart routing
func (a *Agent) buildConversationContext(messages []llmtypes.MessageContent) string {
	var contextBuilder strings.Builder

	// Always send FULL conversation context - no limits, no truncation
	// This ensures smart routing has complete information for proper tool selection
	contextBuilder.WriteString("FULL CONVERSATION CONTEXT:\n")

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role == llmtypes.ChatMessageTypeHuman {
			content := a.extractTextContent(msg)
			contextBuilder.WriteString(fmt.Sprintf("User: %s\n", content))
		} else if msg.Role == llmtypes.ChatMessageTypeAI {
			content := a.extractTextContent(msg)
			contextBuilder.WriteString(fmt.Sprintf("Assistant: %s\n", content))
		}
	}

	return contextBuilder.String()
}

// Helper function to get server count based on cache mode
func (a *Agent) getServerCount() int {
	if a.CacheOnly {
		// Count unique servers from tool-to-server mapping
		serverSet := make(map[string]bool)
		for _, serverName := range a.toolToServer {
			serverSet[serverName] = true
		}
		return len(serverSet)
	}
	return len(a.Clients) // Use active clients count
}

// Tool filtering by relevance
func (a *Agent) filterToolsByRelevance(ctx context.Context, conversationContext string) ([]llmtypes.Tool, error) {
	// Emit smart routing start event
	startEvent := events.NewSmartRoutingStartEvent(
		len(a.Tools),
		a.getServerCount(),
		a.SmartRoutingThreshold.MaxTools,
		a.SmartRoutingThreshold.MaxServers,
	)
	// Add additional context for debugging
	startEvent.LLMPrompt = a.buildServerSelectionPrompt(conversationContext)
	startEvent.UserQuery = conversationContext
	// Add LLM information for smart routing
	startEvent.LLMModelID = a.ModelID
	startEvent.LLMProvider = string(a.GetProvider())
	startEvent.LLMTemperature = a.SmartRoutingConfig.Temperature
	if startEvent.LLMTemperature == 0 {
		startEvent.LLMTemperature = 0.1 // Default temperature
	}
	startEvent.LLMMaxTokens = a.SmartRoutingConfig.MaxTokens
	if startEvent.LLMMaxTokens == 0 {
		startEvent.LLMMaxTokens = 1000 // Default max tokens
	}
	a.EmitTypedEvent(ctx, startEvent)

	startTime := time.Now()

	// Get relevant servers with reasoning
	relevantServers, reasoning, llmResponse, err := a.determineRelevantServersWithReasoning(ctx, conversationContext)
	if err != nil {
		// Emit failure event
		endEvent := events.NewSmartRoutingEndEvent(
			len(a.Tools), 0, a.getServerCount(), nil, "",
			time.Since(startTime), false, err.Error(),
		)

		// NEW: Add appended prompt information even for failures
		endEvent.HasAppendedPrompts = a.HasAppendedPrompts
		endEvent.AppendedPromptCount = len(a.AppendedSystemPrompts)

		if a.HasAppendedPrompts && len(a.AppendedSystemPrompts) > 0 {
			// Create a summary of appended prompts
			var summary strings.Builder
			for i, prompt := range a.AppendedSystemPrompts {
				if i > 0 {
					summary.WriteString("; ")
				}
				// Take first 100 chars of each prompt
				content := prompt
				if len(content) > 100 {
					content = content[:100] + "..."
				}
				summary.WriteString(content)
			}
			endEvent.AppendedPromptSummary = summary.String()
		}

		// Add LLM information for smart routing
		endEvent.LLMModelID = a.ModelID
		endEvent.LLMProvider = string(a.GetProvider())
		endEvent.LLMTemperature = a.SmartRoutingConfig.Temperature
		if endEvent.LLMTemperature == 0 {
			endEvent.LLMTemperature = 0.1 // Default temperature
		}
		endEvent.LLMMaxTokens = a.SmartRoutingConfig.MaxTokens
		if endEvent.LLMMaxTokens == 0 {
			endEvent.LLMMaxTokens = 1000 // Default max tokens
		}

		a.EmitTypedEvent(ctx, endEvent)
		return nil, err
	}

	// 🔄 NEW: Rebuild system prompt with filtered servers
	if err := a.RebuildSystemPromptWithFilteredServers(ctx, relevantServers); err != nil {
		// Log error but don't fail the entire operation
		a.Logger.Warnf("Failed to rebuild system prompt with filtered servers: %v", err)
	}

	filteredTools := a.filterToolsByServers(relevantServers)

	// Emit success event with reasoning and LLM response
	endEvent := events.NewSmartRoutingEndEvent(
		len(a.Tools), len(filteredTools), a.getServerCount(), relevantServers, reasoning,
		time.Since(startTime), true, "",
	)
	// Populate LLM response fields for debugging
	endEvent.LLMResponse = llmResponse
	endEvent.SelectedServers = strings.Join(relevantServers, ", ")

	// NEW: Add appended prompt information
	endEvent.HasAppendedPrompts = a.HasAppendedPrompts
	endEvent.AppendedPromptCount = len(a.AppendedSystemPrompts)

	if a.HasAppendedPrompts && len(a.AppendedSystemPrompts) > 0 {
		// Create a summary of appended prompts
		var summary strings.Builder
		for i, prompt := range a.AppendedSystemPrompts {
			if i > 0 {
				summary.WriteString("; ")
			}
			// Take first 100 chars of each prompt
			content := prompt
			if len(content) > 100 {
				content = content[:100] + "..."
			}
			summary.WriteString(content)
		}
		endEvent.AppendedPromptSummary = summary.String()
	}

	// Add LLM information for smart routing
	endEvent.LLMModelID = a.ModelID
	endEvent.LLMProvider = string(a.GetProvider())
	endEvent.LLMTemperature = a.SmartRoutingConfig.Temperature
	if endEvent.LLMTemperature == 0 {
		endEvent.LLMTemperature = 0.1 // Default temperature
	}
	endEvent.LLMMaxTokens = a.SmartRoutingConfig.MaxTokens
	if endEvent.LLMMaxTokens == 0 {
		endEvent.LLMMaxTokens = 1000 // Default max tokens
	}

	a.EmitTypedEvent(ctx, endEvent)

	return filteredTools, nil
}

// Determine relevant servers with conversation context and reasoning
func (a *Agent) determineRelevantServersWithReasoning(ctx context.Context, conversationContext string) ([]string, string, string, error) {
	prompt := a.buildServerSelectionPrompt(conversationContext)
	servers, reasoning, llmResponse, err := a.makeLightweightLLMCallWithReasoning(ctx, prompt)
	return servers, reasoning, llmResponse, err
}

// Build server selection prompt with conversation context and appended system prompts
func (a *Agent) buildServerSelectionPrompt(conversationContext string) string {
	var serverList strings.Builder
	serverList.WriteString("AVAILABLE MCP SERVERS:\n")

	// In cache-only mode, get unique servers from tool-to-server mapping; otherwise use active clients
	var serversToIterate []string
	if a.CacheOnly {
		// Get unique servers from tool-to-server mapping
		serverSet := make(map[string]bool)
		for _, serverName := range a.toolToServer {
			serverSet[serverName] = true
		}
		for serverName := range serverSet {
			serversToIterate = append(serversToIterate, serverName)
		}
	} else {
		// Build list from active clients
		for serverName := range a.Clients {
			serversToIterate = append(serversToIterate, serverName)
		}
	}

	for _, serverName := range serversToIterate {
		// Count tools for this server
		toolCount := 0
		for _, server := range a.toolToServer {
			if server == serverName {
				toolCount++
			}
		}

		// Get first 5 tools with descriptions for better context
		var toolDetails []string
		for _, tool := range a.Tools {
			if serverName == a.toolToServer[tool.Function.Name] {
				if len(toolDetails) < 5 {
					// Include tool name and description (first 100 chars)
					description := tool.Function.Description
					if len(description) > 100 {
						description = description[:100] + "..."
					}
					toolDetails = append(toolDetails, fmt.Sprintf("%s: %s", tool.Function.Name, description))
				}
			}
		}

		serverList.WriteString(fmt.Sprintf("- %s: %d tools\n", serverName, toolCount))
		if len(toolDetails) > 0 {
			serverList.WriteString("  Tools: ")
			for i, toolDetail := range toolDetails {
				if i > 0 {
					serverList.WriteString(" | ")
				}
				serverList.WriteString(toolDetail)
			}
			serverList.WriteString("\n")
		}
	}

	// NEW: Build appended system prompt section
	var systemPromptSection strings.Builder
	if a.HasAppendedPrompts && len(a.AppendedSystemPrompts) > 0 {
		systemPromptSection.WriteString("IMPORTANT INSTRUCTIONS WHICH ARE ADDED AS A SYSTEM PROMPT TO THE AGENT:\n")
		for i, appendedPrompt := range a.AppendedSystemPrompts {
			// Truncate each appended prompt to avoid token bloat
			content := appendedPrompt
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			systemPromptSection.WriteString(fmt.Sprintf("Appended Prompt %d: %s\n", i+1, content))
		}
		systemPromptSection.WriteString("\n")
	}

	return fmt.Sprintf(`You are a tool routing assistant. Based on the user's query, conversation context, AND agent appended system instructions (if any), determine which MCP servers are most relevant.

%s

%s

CONVERSATION CONTEXT:
%s

INSTRUCTIONS:
1. Analyze the conversation context to understand what the user is trying to accomplish
2. If there are appended system instructions, analyze them to understand agent capabilities and requirements
3. Identify which MCP servers contain tools that would be most helpful
4. Consider BOTH the user needs (from conversation) AND agent capabilities (from appended instructions)
5. Return ONLY the server names that are relevant in the relevant_servers array
6. If multiple mcp servers can be useful, you can choose multiple servers over single server.
7. If in doubt, prefer MORE servers over fewer (better to have tools available)
8. Consider the full conversation flow, not just the last message
9. Include servers that might be needed for follow-up questions
10. When uncertain, err on the side of including more servers
11. Provide brief reasoning in the reasoning field

RESPONSE FORMAT: JSON with relevant_servers array and reasoning field

AVAILABLE SERVERS:`, serverList.String(), systemPromptSection.String(), conversationContext)
}

// Make lightweight LLM call for server selection with structured output and reasoning
func (a *Agent) makeLightweightLLMCallWithReasoning(ctx context.Context, prompt string) ([]string, string, string, error) {
	startTime := time.Now()

	// 🆕 DETAILED SMART ROUTING DEBUG LOGGING
	a.Logger.Infof("🎯 [DEBUG] makeLightweightLLMCallWithReasoning START - Time: %v", startTime)
	a.Logger.Infof("🎯 [DEBUG] Smart routing prompt length: %d chars", len(prompt))
	a.Logger.Infof("🎯 [DEBUG] Context deadline check...")
	if deadline, ok := ctx.Deadline(); ok {
		timeUntilDeadline := time.Until(deadline)
		a.Logger.Infof("🎯 [DEBUG] Context deadline: %v, Time until deadline: %v", deadline, timeUntilDeadline)
	} else {
		a.Logger.Infof("🎯 [DEBUG] Context has no deadline")
	}

	// Define the expected JSON schema for structured output
	schema := `{
		"type": "object",
		"properties": {
			"relevant_servers": {
				"type": "array",
				"items": {
					"type": "string"
				},
				"description": "Array of relevant MCP server names"
			},
			"reasoning": {
				"type": "string",
				"description": "Brief explanation of why these servers were selected"
			}
		},
		"required": ["relevant_servers", "reasoning"]
	}`

	// Use configurable values with fallbacks
	temperature := a.SmartRoutingConfig.Temperature
	if temperature == 0 {
		temperature = 0.1 // Fallback to default if not set
	}
	maxTokens := a.SmartRoutingConfig.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1000 // Fallback to default if not set
	}

	// Build the enhanced prompt with the schema
	enhancedPrompt := a.buildStructuredPromptWithSchema(prompt, schema)

	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeSystem,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: "You are a tool routing assistant that generates structured JSON output according to the specified schema. Always respond with valid JSON only, no additional text or explanations."},
			},
		},
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: enhancedPrompt},
			},
		},
	}

	// Generate response with JSON mode for structured output
	opts := []llmtypes.CallOption{
		llmtypes.WithTemperature(temperature),
		llmtypes.WithMaxTokens(maxTokens),
		llmtypes.WithJSONMode(), // Use JSON mode for reliable structured output
	}

	// 🆕 DETAILED LLM CALL DEBUGGING
	a.Logger.Infof("🎯 [DEBUG] About to call GenerateContentWithRetry - Time: %v", time.Now())
	a.Logger.Infof("🎯 [DEBUG] GenerateContentWithRetry params - Messages: %d, Options: %d", len(messages), len(opts))

	// Use GenerateContentWithRetry for automatic fallback support
	llmCallStart := time.Now()
	response, err, usage := GenerateContentWithRetry(a, ctx, messages, opts, 0, func(msg string) {
		// Optional: Could emit streaming events for smart routing if needed
		// For now, we'll keep it simple since smart routing is typically fast
	})
	llmCallDuration := time.Since(llmCallStart)

	// 🆕 POST-LLM CALL DEBUGGING
	a.Logger.Infof("🎯 [DEBUG] GenerateContentWithRetry completed - Duration: %v, Error: %v", llmCallDuration, err != nil)
	if err != nil {
		a.Logger.Infof("🎯 [DEBUG] GenerateContentWithRetry failed - Error: %v, Error type: %T", err, err)
		return nil, "", "", err
	} else {
		a.Logger.Infof("🎯 [DEBUG] GenerateContentWithRetry succeeded - Response: %v, Usage: %+v", response != nil, usage)
	}

	// Emit enhanced token usage event for smart routing with cache information
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		var tokenEvent *events.TokenUsageEvent
		if response != nil && len(response.Choices) > 0 && response.Choices[0].GenerationInfo != nil {
			// Extract cache information from GenerationInfo
			_, cacheDiscount, reasoningTokens, generationInfo := llm.ExtractTokenUsageWithCacheInfo(response.Choices[0].GenerationInfo)
			tokenEvent = events.NewTokenUsageEventWithCache(
				0, // turn (smart routing is not part of conversation turns)
				"smart_routing",
				a.ModelID,
				string(a.GetProvider()),
				usage.InputTokens,
				usage.OutputTokens,
				usage.TotalTokens,
				time.Since(startTime), // duration
				"smart_routing",
				cacheDiscount, reasoningTokens, generationInfo,
			)
			a.Logger.Infof("[SMART ROUTING] Enhanced token usage - Prompt: %d, Completion: %d, Total: %d, Cache Discount: %.2f, Reasoning: %d",
				usage.InputTokens, usage.OutputTokens, usage.TotalTokens, cacheDiscount, reasoningTokens)
		} else {
			// Fallback to basic token usage event
			tokenEvent = events.NewTokenUsageEvent(
				0, // turn (smart routing is not part of conversation turns)
				"smart_routing",
				a.ModelID,
				string(a.GetProvider()),
				usage.InputTokens,
				usage.OutputTokens,
				usage.TotalTokens,
				time.Since(startTime), // duration
				"smart_routing",
			)
			a.Logger.Infof("[SMART ROUTING] Basic token usage - Prompt: %d, Completion: %d, Total: %d",
				usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
		}
		a.EmitTypedEvent(ctx, tokenEvent)
	}

	// Parse the structured response with reasoning
	servers, reasoning, err := a.parseStructuredServerResponseWithReasoning(response)
	if err != nil {
		return nil, "", "", err
	}

	// Extract the raw LLM response text
	llmResponse := ""
	if len(response.Choices) > 0 && response.Choices[0].Content != "" {
		llmResponse = response.Choices[0].Content
	}

	return servers, reasoning, llmResponse, nil
}

// buildStructuredPromptWithSchema builds a prompt with the provided schema
func (a *Agent) buildStructuredPromptWithSchema(basePrompt string, schema string) string {
	var parts []string

	// Add base prompt
	parts = append(parts, basePrompt)

	// Add the provided schema
	if schema != "" {
		parts = append(parts, "\n\nIMPORTANT: You must respond with valid JSON that exactly matches this schema:")
		parts = append(parts, "\nSchema:")
		parts = append(parts, schema)
	} else {
		parts = append(parts, "\n\nIMPORTANT: You must respond with valid JSON that matches the expected structure.")
	}

	// Add final instruction
	parts = append(parts, "\n\nCRITICAL: Return ONLY the JSON object that matches the schema exactly. No text, no explanations, no markdown. Just the JSON.")

	return strings.Join(parts, "")
}

// Parse structured server selection response with reasoning
func (a *Agent) parseStructuredServerResponseWithReasoning(response *llmtypes.ContentResponse) ([]string, string, error) {
	// Extract the structured content
	if len(response.Choices) == 0 {
		return nil, "", fmt.Errorf("no response choices")
	}

	choice := response.Choices[0]

	// Get the text content directly (choice.Content is a string)
	textContent := choice.Content
	if textContent == "" {
		return nil, "", fmt.Errorf("no content in LLM response")
	}

	// Try to parse as JSON first (structured output)
	servers, reasoning, err := a.parseJSONServerResponseWithReasoningFromString(textContent)
	if err == nil {
		return servers, reasoning, nil
	}

	// Fallback to text parsing if JSON parsing fails
	servers, err = a.parseTextServerResponse(textContent)
	if err != nil {
		return nil, "", err
	}
	return servers, "Fallback text parsing used", nil
}

// Parse JSON server response with reasoning from string
func (a *Agent) parseJSONServerResponseWithReasoningFromString(jsonStr string) ([]string, string, error) {
	// Try to parse the JSON string
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return nil, "", fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Extract relevant_servers array
	serversInterface, exists := data["relevant_servers"]
	if !exists {
		return nil, "", fmt.Errorf("missing 'relevant_servers' field in response")
	}

	serversArray, ok := serversInterface.([]interface{})
	if !ok {
		return nil, "", fmt.Errorf("'relevant_servers' is not an array")
	}

	// Extract reasoning
	reasoning := ""
	if reasoningInterface, exists := data["reasoning"]; exists {
		if reasoningStr, ok := reasoningInterface.(string); ok {
			reasoning = reasoningStr
		}
	}

	// Convert to string slice
	var servers []string
	for _, server := range serversArray {
		if serverStr, ok := server.(string); ok {
			serverStr = strings.TrimSpace(serverStr)
			if serverStr != "" {
				servers = append(servers, serverStr)
			}
		}
	}

	return servers, reasoning, nil
}

// Parse text server response
func (a *Agent) parseTextServerResponse(response string) ([]string, error) {
	// Clean up response and extract server names
	response = strings.TrimSpace(response)
	response = strings.TrimSuffix(response, ".")

	// Split by comma and clean up each server name
	serverNames := strings.Split(response, ",")
	var cleanServers []string

	for _, server := range serverNames {
		server = strings.TrimSpace(server)
		if server != "" {
			cleanServers = append(cleanServers, server)
		}
	}

	return cleanServers, nil
}

// Filter tools by server
func (a *Agent) filterToolsByServers(relevantServers []string) []llmtypes.Tool {
		var filteredTools []llmtypes.Tool

	for _, tool := range a.Tools {
		// Check if this is a custom tool (no server mapping)
		if _, exists := a.toolToServer[tool.Function.Name]; !exists {
			// This is a custom tool (like memory tools) - always include it
			filteredTools = append(filteredTools, tool)
			continue
		}

		// This is a regular MCP tool - check if its server is relevant
		if serverName, exists := a.toolToServer[tool.Function.Name]; exists {
			for _, relevantServer := range relevantServers {
				if serverName == relevantServer {
					filteredTools = append(filteredTools, tool)
					break
				}
			}
		}
	}

	return filteredTools
}

// Helper function to extract text content
func (a *Agent) extractTextContent(msg llmtypes.MessageContent) string {
	var textParts []string
	for _, part := range msg.Parts {
		if textPart, ok := part.(llmtypes.TextContent); ok {
			textParts = append(textParts, textPart.Text)
		}
	}
	return strings.Join(textParts, " ")
}

// Getter and setter methods for smart routing configuration
func (a *Agent) IsSmartRoutingEnabled() bool {
	return a.EnableSmartRouting
}

func (a *Agent) SetSmartRouting(enabled bool) {
	a.EnableSmartRouting = enabled
}

func (a *Agent) GetSmartRoutingThresholds() struct {
	MaxTools   int
	MaxServers int
} {
	return a.SmartRoutingThreshold
}

func (a *Agent) SetSmartRoutingThresholds(maxTools, maxServers int) {
	a.SmartRoutingThreshold.MaxTools = maxTools
	a.SmartRoutingThreshold.MaxServers = maxServers
}

func (a *Agent) ShouldUseSmartRouting() bool {
	return a.shouldUseSmartRouting()
}

// Getter and setter methods for smart routing configuration
func (a *Agent) GetSmartRoutingConfig() struct {
	Temperature       float64
	MaxTokens         int
	MaxMessages       int
	UserMsgLimit      int
	AssistantMsgLimit int
} {
	return a.SmartRoutingConfig
}

func (a *Agent) SetSmartRoutingConfig(temperature float64, maxTokens, maxMessages, userMsgLimit, assistantMsgLimit int) {
	a.SmartRoutingConfig.Temperature = temperature
	a.SmartRoutingConfig.MaxTokens = maxTokens
	a.SmartRoutingConfig.MaxMessages = maxMessages
	a.SmartRoutingConfig.UserMsgLimit = userMsgLimit
	a.SmartRoutingConfig.AssistantMsgLimit = assistantMsgLimit
}
