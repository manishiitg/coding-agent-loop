package vertex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"mcp-agent/agent_go/internal/utils"

	"google.golang.org/genai"

	"mcp-agent/agent_go/internal/llmtypes"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const (
	// ResponseSchemaKey is the context key for passing ResponseSchema
	ResponseSchemaKey contextKey = "vertex_response_schema"
)

// GoogleGenAIAdapter is an adapter that implements llmtypes.Model interface
// using the Google GenAI SDK directly
type GoogleGenAIAdapter struct {
	client  *genai.Client
	modelID string
	logger  utils.ExtendedLogger
}

// NewGoogleGenAIAdapter creates a new adapter instance
func NewGoogleGenAIAdapter(client *genai.Client, modelID string, logger utils.ExtendedLogger) *GoogleGenAIAdapter {
	return &GoogleGenAIAdapter{
		client:  client,
		modelID: modelID,
		logger:  logger,
	}
}

// GenerateContent implements the llmtypes.Model interface
func (g *GoogleGenAIAdapter) GenerateContent(ctx context.Context, messages []llmtypes.MessageContent, options ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	// Parse call options
	opts := &llmtypes.CallOptions{}
	for _, opt := range options {
		opt(opts)
	}

	// Determine model ID (from option or default)
	modelID := g.modelID
	if opts.Model != "" {
		modelID = opts.Model
	}

	// Convert messages from llmtypes format to genai format
	genaiContents := make([]*genai.Content, 0, len(messages))

	// Track function calls from previous AI message to ensure function responses match
	var previousFunctionCallIDs []string

	if g.logger != nil {
		g.logger.Debugf("🔍 [GEMINI] Processing %d messages for conversion", len(messages))
	}

	// CRITICAL FIX: Combine consecutive Tool messages that follow an AI message with function calls
	// Gemini requires ALL function responses to be in a SINGLE message, matching the order of function calls
	combinedMessages := make([]llmtypes.MessageContent, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		// If this is a Tool message and the previous message (in original array) was an AI message with function calls
		if msg.Role == llmtypes.ChatMessageTypeTool && i > 0 {
			prevMsg := messages[i-1]
			if prevMsg.Role == llmtypes.ChatMessageTypeAI {
				// Check if previous message has function calls
				hasFunctionCalls := false
				for _, part := range prevMsg.Parts {
					if _, ok := part.(llmtypes.ToolCall); ok {
						hasFunctionCalls = true
						break
					}
				}

				// If previous message has function calls, combine this and following Tool messages
				if hasFunctionCalls {
					combinedParts := make([]llmtypes.ContentPart, 0)

					// Collect all ToolCallResponse parts from consecutive Tool messages
					j := i
					for j < len(messages) && messages[j].Role == llmtypes.ChatMessageTypeTool {
						for _, part := range messages[j].Parts {
							if _, ok := part.(llmtypes.ToolCallResponse); ok {
								combinedParts = append(combinedParts, part)
							}
						}
						j++
					}

					// Create a single combined Tool message
					if len(combinedParts) > 0 {
						combinedMessages = append(combinedMessages, llmtypes.MessageContent{
							Role:  llmtypes.ChatMessageTypeTool,
							Parts: combinedParts,
						})
						if g.logger != nil {
							g.logger.Debugf("🔍 [GEMINI] Combined %d Tool messages (indices %d-%d) into single message with %d responses",
								j-i, i, j-1, len(combinedParts))
						}
						// Skip the individual Tool messages we just combined
						i = j - 1
						continue
					}
				}
			}
		}

		// Add message as-is if not combined
		combinedMessages = append(combinedMessages, msg)
	}

	// Use combined messages for processing
	messages = combinedMessages
	if g.logger != nil {
		g.logger.Debugf("🔍 [GEMINI] After combining: %d messages (reduced from original)", len(messages))
	}

	for msgIdx, msg := range messages {
		// 🔍 DETECTION & FIX: Check for mixed Text + ToolCall parts (can cause Gemini empty responses)
		// If detected, split into separate messages automatically
		hasText := false
		hasToolCall := false
		var textParts []llmtypes.ContentPart
		var toolCallParts []llmtypes.ContentPart
		var otherParts []llmtypes.ContentPart

		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				hasText = true
				textParts = append(textParts, p)
			case llmtypes.ToolCall:
				hasToolCall = true
				toolCallParts = append(toolCallParts, p)
			default:
				otherParts = append(otherParts, part)
			}
		}

		// If message has both text and tool calls, split into separate messages
		if hasText && hasToolCall && msg.Role == llmtypes.ChatMessageTypeAI {
			if g.logger != nil {
				// Log detailed info about the mixed message for debugging
				textPreview := ""
				if len(textParts) > 0 {
					if tc, ok := textParts[0].(llmtypes.TextContent); ok {
						textPreview = tc.Text
						if len(textPreview) > 100 {
							textPreview = textPreview[:100] + "..."
						}
					}
				}
				toolNames := make([]string, 0, len(toolCallParts))
				for _, tc := range toolCallParts {
					if toolCall, ok := tc.(llmtypes.ToolCall); ok && toolCall.FunctionCall != nil {
						toolNames = append(toolNames, toolCall.FunctionCall.Name)
					}
				}
				g.logger.Warnf("⚠️ [GEMINI] Model message contains both TextContent and ToolCall parts - splitting into separate messages to avoid empty responses. Text preview: %q, Tool calls: %v", textPreview, toolNames)
			}

			// Create separate message for text content
			if len(textParts) > 0 || len(otherParts) > 0 {
				textOnlyParts := make([]llmtypes.ContentPart, 0, len(textParts)+len(otherParts))
				textOnlyParts = append(textOnlyParts, textParts...)
				textOnlyParts = append(textOnlyParts, otherParts...)
				if len(textOnlyParts) > 0 {
					textMsg := llmtypes.MessageContent{
						Role:  msg.Role,
						Parts: textOnlyParts,
					}
					// Convert and add text-only message
					genaiParts := g.convertMessageParts(textMsg.Parts)
					if len(genaiParts) > 0 {
						role := convertRole(string(textMsg.Role))
						genaiContents = append(genaiContents, &genai.Content{
							Role:  role,
							Parts: genaiParts,
						})
					}
				}
			}

			// Create separate message for tool calls only
			if len(toolCallParts) > 0 {
				toolCallMsg := llmtypes.MessageContent{
					Role:  msg.Role,
					Parts: toolCallParts,
				}
				// Convert and add tool-call-only message
				genaiParts := g.convertMessageParts(toolCallMsg.Parts)
				if len(genaiParts) > 0 {
					role := convertRole(string(toolCallMsg.Role))
					genaiContents = append(genaiContents, &genai.Content{
						Role:  role,
						Parts: genaiParts,
					})
				}
			}

			// Track function calls from the tool-call-only message
			if len(toolCallParts) > 0 {
				previousFunctionCallIDs = nil // Reset
				for _, tc := range toolCallParts {
					if toolCall, ok := tc.(llmtypes.ToolCall); ok {
						previousFunctionCallIDs = append(previousFunctionCallIDs, toolCall.ID)
						if g.logger != nil {
							g.logger.Debugf("🔍 [GEMINI] Message %d: Tracked function call ID: %s (name: %s)", msgIdx, toolCall.ID, toolCall.FunctionCall.Name)
						}
					}
				}
				if g.logger != nil {
					g.logger.Debugf("🔍 [GEMINI] Message %d: Tracked %d function calls total", msgIdx, len(previousFunctionCallIDs))
				}
			}

			// Skip processing the original mixed message
			continue
		}

		// Check if this is a Tool message with function responses
		// Gemini requires function responses to match previous function calls in count and order
		if msg.Role == llmtypes.ChatMessageTypeTool {
			// Extract function responses from this message
			var functionResponses []llmtypes.ToolCallResponse
			for _, part := range msg.Parts {
				if toolResp, ok := part.(llmtypes.ToolCallResponse); ok {
					functionResponses = append(functionResponses, toolResp)
				}
			}

			if g.logger != nil {
				g.logger.Debugf("🔍 [GEMINI] Message %d (Tool): Found %d function responses, previous function calls: %d",
					msgIdx, len(functionResponses), len(previousFunctionCallIDs))
				for i, resp := range functionResponses {
					g.logger.Debugf("🔍 [GEMINI]   Response %d: ToolCallID=%s", i+1, resp.ToolCallID)
				}
				for i, callID := range previousFunctionCallIDs {
					g.logger.Debugf("🔍 [GEMINI]   Expected call %d: ID=%s", i+1, callID)
				}
			}

			// If we have previous function calls, ensure responses match exactly
			// Gemini requires: number of function response parts = number of function call parts
			if len(previousFunctionCallIDs) > 0 {
				// Create a map of response IDs for quick lookup
				responseMap := make(map[string]llmtypes.ToolCallResponse)
				for _, resp := range functionResponses {
					responseMap[resp.ToolCallID] = resp
				}

				// Reorder responses to match the exact order of function calls
				// Only include responses that match a function call
				orderedResponses := make([]llmtypes.ContentPart, 0, len(previousFunctionCallIDs))
				missingCount := 0
				for _, callID := range previousFunctionCallIDs {
					if resp, found := responseMap[callID]; found {
						orderedResponses = append(orderedResponses, resp)
					} else {
						// Missing response - Gemini requires exact match, so we need to handle this
						missingCount++
						if g.logger != nil {
							g.logger.Warnf("⚠️ [GEMINI] Function response missing for call ID: %s (required for Gemini API)", callID)
						}
					}
				}

				// Log warnings for responses that don't match any call IDs
				for _, resp := range functionResponses {
					found := false
					for _, callID := range previousFunctionCallIDs {
						if resp.ToolCallID == callID {
							found = true
							break
						}
					}
					if !found {
						if g.logger != nil {
							g.logger.Warnf("⚠️ [GEMINI] Function response with ID %s doesn't match any previous function call (will be skipped)", resp.ToolCallID)
						}
					}
				}

				// Gemini requires exact match - if we have missing responses, log error
				if missingCount > 0 {
					if g.logger != nil {
						g.logger.Errorf("❌ [GEMINI] Function response count mismatch: expected %d responses, got %d. Missing %d responses. This will cause API error.",
							len(previousFunctionCallIDs), len(orderedResponses), missingCount)
					}
				}

				// Update message parts with ordered responses (only matching ones)
				// Note: If responses don't match exactly, Gemini will return an error
				// but we'll send what we have in the correct order
				if len(orderedResponses) > 0 {
					if g.logger != nil {
						g.logger.Debugf("🔍 [GEMINI] Message %d: Reordered %d responses to match %d function calls",
							msgIdx, len(orderedResponses), len(previousFunctionCallIDs))
					}
					msg.Parts = orderedResponses
				} else {
					// No matching responses - this will cause an error, but we'll let Gemini handle it
					if g.logger != nil {
						g.logger.Errorf("❌ [GEMINI] No matching function responses found for %d function calls", len(previousFunctionCallIDs))
					}
				}

				// CRITICAL: Gemini requires EXACT match - if counts don't match, we must not send the message
				if len(orderedResponses) != len(previousFunctionCallIDs) {
					if g.logger != nil {
						g.logger.Errorf("❌ [GEMINI] CRITICAL: Function response count mismatch - expected %d, got %d. Skipping this message to avoid API error.",
							len(previousFunctionCallIDs), len(orderedResponses))
					}
					// Skip this message entirely - don't add it to genaiContents
					previousFunctionCallIDs = nil
					continue
				}
			} else if len(functionResponses) > 0 {
				// We have responses but no previous function calls tracked
				if g.logger != nil {
					g.logger.Warnf("⚠️ [GEMINI] Message %d: Found %d function responses but no previous function calls tracked",
						msgIdx, len(functionResponses))
				}
			}

			// Clear previous function calls after processing responses
			previousFunctionCallIDs = nil
		}

		// Track function calls from AI messages for next iteration
		if msg.Role == llmtypes.ChatMessageTypeAI {
			previousFunctionCallIDs = nil // Reset
			for _, part := range msg.Parts {
				if toolCall, ok := part.(llmtypes.ToolCall); ok {
					previousFunctionCallIDs = append(previousFunctionCallIDs, toolCall.ID)
					if g.logger != nil {
						g.logger.Debugf("🔍 [GEMINI] Message %d (AI): Tracked function call ID: %s (name: %s)",
							msgIdx, toolCall.ID, toolCall.FunctionCall.Name)
					}
				}
			}
			if len(previousFunctionCallIDs) > 0 && g.logger != nil {
				g.logger.Debugf("🔍 [GEMINI] Message %d (AI): Tracked %d function calls total", msgIdx, len(previousFunctionCallIDs))
			}
		}

		// Normal processing for messages without mixed parts
		// Use convertMessageParts helper to handle all part types including ImageContent
		genaiParts := g.convertMessageParts(msg.Parts)

		if len(genaiParts) > 0 {
			role := convertRole(string(msg.Role))
			genaiContents = append(genaiContents, &genai.Content{
				Role:  role,
				Parts: genaiParts,
			})
		}
	}

	// Build GenerateContentConfig from options
	config := &genai.GenerateContentConfig{}

	// Set temperature
	if opts.Temperature > 0 {
		temp := float32(opts.Temperature)
		config.Temperature = &temp
	}

	// Set max output tokens
	if opts.MaxTokens > 0 {
		config.MaxOutputTokens = int32(opts.MaxTokens)
	}

	// Handle JSON mode if specified
	if opts.JSONMode {
		config.ResponseMIMEType = "application/json"
	}

	// Handle ResponseSchema from context (for structured output)
	if schema, ok := ctx.Value(ResponseSchemaKey).(*genai.Schema); ok && schema != nil {
		config.ResponseSchema = schema
		// If ResponseSchema is set, ensure JSON mode is enabled
		if config.ResponseMIMEType == "" {
			config.ResponseMIMEType = "application/json"
		}
	}

	// Convert tools if provided
	if len(opts.Tools) > 0 {
		if g.logger != nil {
			g.logger.Infof("🔍 [VERTEX] Converting %d tools to Gemini format", len(opts.Tools))
			for i, tool := range opts.Tools {
				if tool.Function != nil {
					g.logger.Infof("🔍 [VERTEX] Tool %d: Name=%s, Description length=%d, HasParameters=%v",
						i+1, tool.Function.Name, len(tool.Function.Description), tool.Function.Parameters != nil)
				}
			}
		}
		genaiTools := convertTools(opts.Tools, g.logger)
		config.Tools = genaiTools
		if g.logger != nil && genaiTools != nil && len(genaiTools) > 0 {
			if len(genaiTools[0].FunctionDeclarations) > 0 {
				g.logger.Infof("🔍 [VERTEX] Converted to %d function declarations in 1 Tool", len(genaiTools[0].FunctionDeclarations))
			}
		}

		// Handle tool choice
		if opts.ToolChoice != nil {
			toolConfig := convertToolChoice(opts.ToolChoice)
			if toolConfig != nil {
				config.ToolConfig = toolConfig
			}
		}
	}

	// Generate unique request ID for tracking request/response correlation (only logged on errors)
	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())

	// Track if we had to split any mixed messages - this helps correlate with empty responses
	var hadMixedMessages bool
	for _, msg := range messages {
		if msg.Role == llmtypes.ChatMessageTypeAI {
			hasText := false
			hasToolCall := false
			for _, part := range msg.Parts {
				if _, ok := part.(llmtypes.TextContent); ok {
					hasText = true
				}
				if _, ok := part.(llmtypes.ToolCall); ok {
					hasToolCall = true
				}
			}
			if hasText && hasToolCall {
				hadMixedMessages = true
				break
			}
		}
	}

	// Call Google GenAI API
	result, err := g.client.Models.GenerateContent(ctx, modelID, genaiContents, config)

	if err != nil {
		// Log error with input and response details (including request ID for correlation)
		if g.logger != nil {
			if hadMixedMessages {
				g.logger.Warnf("⚠️ [REQUEST_ID: %s] ERROR occurred after detecting mixed TextContent+ToolCall messages - correlation check", requestID)
			}
			g.logErrorDetails(requestID, modelID, messages, config, opts, err, result)
			g.logRawResponse(requestID, modelID, result, err)
		}
		return nil, fmt.Errorf("genai generate content: %w", err)
	}

	// Convert response from genai format to llmtypes format
	convertedResp := convertResponse(result, g.logger, hadMixedMessages)
	return convertedResp, nil
}

// convertRole converts llmtypes message role to genai role
func convertRole(role string) string {
	switch role {
	case string(llmtypes.ChatMessageTypeSystem):
		return "user" // GenAI uses "user" for system messages typically
	case string(llmtypes.ChatMessageTypeHuman):
		return "user"
	case string(llmtypes.ChatMessageTypeAI):
		return "model"
	case string(llmtypes.ChatMessageTypeTool):
		return "user" // Tool responses are typically sent as user messages
	default:
		return "user"
	}
}

// convertTools converts llmtypes tools to genai tools
// IMPORTANT: Gemini API requires all function declarations to be in a single Tool
// unless all tools are search tools. We combine all functions into one Tool.
func convertTools(llmTools []llmtypes.Tool, logger utils.ExtendedLogger) []*genai.Tool {
	if len(llmTools) == 0 {
		return nil
	}

	// Collect all function declarations
	functionDeclarations := make([]*genai.FunctionDeclaration, 0, len(llmTools))

	for i, tool := range llmTools {
		if tool.Function == nil {
			if logger != nil {
				logger.Warnf("⚠️ [VERTEX] Tool %d has nil Function, skipping", i+1)
			}
			continue
		}

		// Validate function name (Gemini requires valid function names)
		if tool.Function.Name == "" {
			if logger != nil {
				logger.Errorf("❌ [VERTEX] Tool %d has empty function name, skipping", i+1)
			}
			continue
		}

		// Convert function definition
		functionDef := &genai.FunctionDeclaration{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
		}

		// Convert parameters (JSON Schema)
		// The Parameters field in FunctionDeclaration expects a *genai.Schema
		// We'll convert the JSON Schema map to a genai.Schema structure
		if tool.Function.Parameters != nil {
			// Convert from typed Parameters to map
			paramsMap := make(map[string]interface{})
			if tool.Function.Parameters.Type != "" {
				paramsMap["type"] = tool.Function.Parameters.Type
			}
			if tool.Function.Parameters.Properties != nil {
				paramsMap["properties"] = tool.Function.Parameters.Properties
			}
			if tool.Function.Parameters.Required != nil {
				paramsMap["required"] = tool.Function.Parameters.Required
			}
			if tool.Function.Parameters.AdditionalProperties != nil {
				paramsMap["additionalProperties"] = tool.Function.Parameters.AdditionalProperties
			}
			if tool.Function.Parameters.PatternProperties != nil {
				paramsMap["patternProperties"] = tool.Function.Parameters.PatternProperties
			}
			if tool.Function.Parameters.Additional != nil {
				for k, v := range tool.Function.Parameters.Additional {
					paramsMap[k] = v
				}
			}

			// Validate schema before conversion
			if logger != nil {
				logger.Infof("🔍 [VERTEX] Validating schema for function %s", tool.Function.Name)
				validateSchemaForGemini(paramsMap, tool.Function.Name, logger)
			}

			schema := convertJSONSchemaToSchema(paramsMap)
			if schema != nil {
				functionDef.Parameters = schema
				if logger != nil {
					logger.Infof("🔍 [VERTEX] Function %s: Schema converted successfully", tool.Function.Name)
				}
			} else {
				if logger != nil {
					logger.Warnf("⚠️ [VERTEX] Function %s: Schema conversion returned nil", tool.Function.Name)
				}
			}
		} else {
			if logger != nil {
				logger.Infof("🔍 [VERTEX] Function %s: No parameters", tool.Function.Name)
			}
		}

		functionDeclarations = append(functionDeclarations, functionDef)
		if logger != nil {
			logger.Infof("🔍 [VERTEX] Added function declaration %d: %s", len(functionDeclarations), tool.Function.Name)
		}
	}

	// Combine all function declarations into a single Tool
	// This is required by Gemini API: multiple tools are only supported
	// when they are all search tools, otherwise all functions must be in one Tool
	if len(functionDeclarations) > 0 {
		return []*genai.Tool{
			{
				FunctionDeclarations: functionDeclarations,
			},
		}
	}

	return nil
}

// validateSchemaForGemini validates JSON Schema for common issues that cause MALFORMED_FUNCTION_CALL
func validateSchemaForGemini(schema map[string]interface{}, functionName string, logger utils.ExtendedLogger) {
	if schema == nil {
		return
	}

	// Check for array types without items (required by Gemini)
	if props, ok := schema["properties"].(map[string]interface{}); ok {
		for propName, propValue := range props {
			if propMap, ok := propValue.(map[string]interface{}); ok {
				if propType, ok := propMap["type"].(string); ok && propType == "array" {
					if _, hasItems := propMap["items"]; !hasItems {
						logger.Errorf("❌ [VERTEX] Function %s: Property '%s' is array type but missing 'items' field - this will cause MALFORMED_FUNCTION_CALL", functionName, propName)
					}
				}
				// Recursively check nested objects
				if propType, ok := propMap["type"].(string); ok && propType == "object" {
					if nestedProps, ok := propMap["properties"].(map[string]interface{}); ok {
						validateSchemaForGemini(map[string]interface{}{"properties": nestedProps}, functionName+"."+propName, logger)
					}
				}
			}
		}
	}

	// Check for invalid type values
	if schemaType, ok := schema["type"].(string); ok {
		validTypes := map[string]bool{"object": true, "array": true, "string": true, "number": true, "integer": true, "boolean": true, "null": true}
		if !validTypes[schemaType] {
			logger.Warnf("⚠️ [VERTEX] Function %s: Schema has invalid type '%s'", functionName, schemaType)
		}
	}
}

// convertJSONSchemaToSchema converts a JSON Schema map to genai.Schema
// Uses JSON marshaling/unmarshaling for proper conversion
func convertJSONSchemaToSchema(jsonSchema map[string]interface{}) *genai.Schema {
	if jsonSchema == nil {
		return nil
	}

	// Convert the JSON Schema map to JSON bytes
	jsonBytes, err := json.Marshal(jsonSchema)
	if err != nil {
		return nil
	}

	// Unmarshal into genai.Schema
	// The genai.Schema should accept JSON Schema format via JSON tags
	var schema genai.Schema
	if err := json.Unmarshal(jsonBytes, &schema); err != nil {
		// If direct unmarshaling fails, try building it manually
		return buildSchemaManually(jsonSchema)
	}

	return &schema
}

// buildSchemaManually manually builds a genai.Schema from JSON Schema map
// This is a fallback if JSON unmarshaling doesn't work
func buildSchemaManually(jsonSchema map[string]interface{}) *genai.Schema {
	schema := &genai.Schema{}

	// Extract basic fields
	if desc, ok := jsonSchema["description"].(string); ok {
		schema.Description = desc
	}

	// Extract properties for object type
	if props, ok := jsonSchema["properties"].(map[string]interface{}); ok {
		schema.Properties = make(map[string]*genai.Schema)
		for key, value := range props {
			if propMap, ok := value.(map[string]interface{}); ok {
				schema.Properties[key] = buildSchemaManually(propMap)
			}
		}
	}

	// Extract required fields
	if req, ok := jsonSchema["required"].([]interface{}); ok {
		schema.Required = make([]string, 0, len(req))
		for _, r := range req {
			if str, ok := r.(string); ok {
				schema.Required = append(schema.Required, str)
			}
		}
	}

	// Extract items for array type
	if items, ok := jsonSchema["items"].(map[string]interface{}); ok {
		schema.Items = buildSchemaManually(items)
	}

	return schema
}

// convertToolChoice converts llmtypes tool choice to genai tool config
func convertToolChoice(toolChoice interface{}) *genai.ToolConfig {
	if toolChoice == nil {
		return nil
	}

	config := &genai.ToolConfig{
		FunctionCallingConfig: &genai.FunctionCallingConfig{},
	}

	// Handle string-based tool choice (from ConvertToolChoice)
	if choiceStr, ok := toolChoice.(string); ok {
		switch choiceStr {
		case "auto":
			config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeAuto
		case "none":
			config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeNone
		case "required":
			config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeAny
		default:
			config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeAuto
		}
		return config
	}

	// Handle ToolChoice struct if it's that type
	if tc, ok := toolChoice.(*llmtypes.ToolChoice); ok && tc != nil {
		// Note: llmtypes ToolChoice structure may vary, adjust as needed
		// For now, default to AUTO
		config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeAuto

		// If there's a function specified, we could set AllowedFunctionNames
		// This would require knowing the actual ToolChoice structure
		return config
	}

	// Handle map-based tool choice (from ConvertToolChoice)
	if choiceMap, ok := toolChoice.(map[string]interface{}); ok {
		if typ, ok := choiceMap["type"].(string); ok && typ == "function" {
			if fnMap, ok := choiceMap["function"].(map[string]interface{}); ok {
				if name, ok := fnMap["name"].(string); ok {
					config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeAny
					config.FunctionCallingConfig.AllowedFunctionNames = []string{name}
					return config
				}
			}
		}
	}

	// Default to AUTO mode
	config.FunctionCallingConfig.Mode = genai.FunctionCallingConfigModeAuto
	return config
}

// convertResponse converts genai response to llmtypes ContentResponse
// hadMixedMessages is used to check correlation with empty content errors
func convertResponse(result *genai.GenerateContentResponse, logger utils.ExtendedLogger, hadMixedMessages bool) *llmtypes.ContentResponse {
	if result == nil {
		if logger != nil {
			logger.Warnf("⚠️ [VERTEX] convertResponse received nil result")
		}
		return &llmtypes.ContentResponse{
			Choices: []*llmtypes.ContentChoice{},
		}
	}

	if logger != nil {
		logger.Debugf("🔍 [VERTEX] convertResponse - Candidates count: %d, hadMixedMessages: %v", len(result.Candidates), hadMixedMessages)
	}

	choices := make([]*llmtypes.ContentChoice, 0, len(result.Candidates))

	for i, candidate := range result.Candidates {
		if logger != nil {
			logger.Debugf("🔍 [VERTEX] Processing candidate %d/%d", i+1, len(result.Candidates))
		}

		choice := &llmtypes.ContentChoice{
			ToolCalls: []llmtypes.ToolCall{}, // Initialize as empty slice, not nil
		}

		// Extract text content and tool calls from parts
		var textParts []string
		var toolCalls []llmtypes.ToolCall

		if candidate.Content != nil {
			if logger != nil {
				logger.Debugf("🔍 [VERTEX] Candidate %d: Content is not nil, Parts count: %d", i, len(candidate.Content.Parts))
			}

			for j, part := range candidate.Content.Parts {
				if part.Text != "" {
					textParts = append(textParts, part.Text)
					if logger != nil {
						logger.Debugf("🔍 [VERTEX] Candidate %d, Part %d: Found text content (length: %d)", i, j, len(part.Text))
					}
				}

				if part.FunctionCall != nil {
					if logger != nil {
						logger.Debugf("🔍 [VERTEX] Candidate %d, Part %d: Found FunctionCall - Name: %s", i, j, part.FunctionCall.Name)
					}
					// 🔧 FIX: Generate ToolCallID for FunctionCall
					// Gemini's FunctionCall doesn't include an ID field, so we generate one.
					// This ID is used later when creating ToolCallResponse to match
					// the response to the original call. Gemini matches FunctionResponses
					// to FunctionCalls primarily by sequence/position, but the ID is still
					// used in NewPartFromFunctionResponse for proper association.
					toolCall := llmtypes.ToolCall{
						ID:   generateToolCallID(),
						Type: "function",
						FunctionCall: &llmtypes.FunctionCall{
							Name:      part.FunctionCall.Name,
							Arguments: convertArgumentsToString(part.FunctionCall.Args),
						},
					}
					toolCalls = append(toolCalls, toolCall)
					if logger != nil {
						logger.Debugf("🔍 [VERTEX] Candidate %d, Part %d: Created ToolCall - ID: %s, Name: %s", i, j, toolCall.ID, toolCall.FunctionCall.Name)
					}
				} else if logger != nil {
					logger.Debugf("🔍 [VERTEX] Candidate %d, Part %d: No FunctionCall (Text: %q, Text length: %d)", i, j, part.Text, len(part.Text))
				}
			}
		} else if logger != nil {
			logger.Debugf("🔍 [VERTEX] Candidate %d: Content is nil", i)
		}

		// Combine text parts - use Text() helper if available
		if len(textParts) > 0 {
			choice.Content = ""
			for j, text := range textParts {
				if j > 0 {
					choice.Content += "\n"
				}
				choice.Content += text
			}
			if logger != nil {
				logger.Debugf("🔍 [VERTEX] Candidate %d: Combined %d text parts into content (length: %d)", i, len(textParts), len(choice.Content))
			}
		} else if result.Text() != "" {
			// Fallback to using result.Text() helper
			choice.Content = result.Text()
			if logger != nil {
				logger.Debugf("🔍 [VERTEX] Candidate %d: Used result.Text() fallback (length: %d)", i, len(choice.Content))
			}
		} else if logger != nil {
			logger.Debugf("🔍 [VERTEX] Candidate %d: No text content found (textParts: %d, result.Text(): %q)", i, len(textParts), result.Text())
		}

		// 🆕 LOG EMPTY CONTENT WARNING - Detailed logging when content is empty
		if choice.Content == "" && logger != nil {
			if hadMixedMessages {
				logger.Errorf("❌ [VERTEX] Candidate %d has EMPTY CONTENT - ⚠️ CORRELATION: This request had mixed TextContent+ToolCall messages that were split. This may indicate mixed messages caused the empty response.", i)
			} else {
				logger.Errorf("❌ [VERTEX] Candidate %d has EMPTY CONTENT - No mixed messages detected. This may indicate other issues (context length, API throttling, etc.). Debugging info:", i)
			}
			logger.Errorf("   Candidate.Content: %v (nil: %v)", candidate.Content != nil, candidate.Content == nil)
			if candidate.Content != nil {
				logger.Errorf("   Candidate.Content.Parts count: %d", len(candidate.Content.Parts))
				for j, part := range candidate.Content.Parts {
					logger.Errorf("     Part %d - Text: %q, Text length: %d, FunctionCall: %v",
						j, part.Text, len(part.Text), part.FunctionCall != nil)
				}
			}
			logger.Errorf("   Candidate.FinishReason: %q", candidate.FinishReason)
			// Check for specific finish reasons that might explain empty content
			finishReason := string(candidate.FinishReason)
			if finishReason == "STOP" {
				logger.Errorf("   ⚠️ FinishReason is STOP - This is normal, but content is empty. May indicate:")
				logger.Errorf("      - Conversation ended naturally but no text was generated")
				logger.Errorf("      - Only tool calls were requested")
				logger.Errorf("      - Context exhaustion (conversation too long)")
			} else if finishReason == "MAX_TOKENS" {
				logger.Errorf("   ⚠️ FinishReason is MAX_TOKENS - Token limit reached, content may be truncated")
			} else if finishReason == "RECITATION" {
				logger.Errorf("   ⚠️ FinishReason is RECITATION - Content blocked due to recitation concerns")
			} else if finishReason == "MALFORMED_FUNCTION_CALL" {
				logger.Errorf("   ❌ FinishReason is MALFORMED_FUNCTION_CALL - This indicates a problem with function declarations:")
				logger.Errorf("      - Missing 'items' field in array parameters (most common)")
				logger.Errorf("      - Invalid schema structure")
				logger.Errorf("      - Invalid function names or descriptions")
				logger.Errorf("      - Check tool conversion logs above for validation errors")
			}
			// Note: SAFETY blocks typically return API errors, not empty content, so we don't check for SAFETY here
			logger.Errorf("   result.Text() fallback: %q (length: %d)", result.Text(), len(result.Text()))
			logger.Errorf("   TextParts extracted: %d", len(textParts))
			logger.Errorf("   ToolCalls extracted: %d", len(toolCalls))
		}

		// Set tool calls if any
		if len(toolCalls) > 0 {
			choice.ToolCalls = toolCalls
			if logger != nil {
				logger.Debugf("🔍 [VERTEX] Candidate %d: Set %d tool calls from candidate.Content.Parts", i, len(toolCalls))
				for j, tc := range toolCalls {
					logger.Debugf("🔍 [VERTEX] Candidate %d, ToolCall %d: ID=%s, Name=%s", i, j+1, tc.ID, tc.FunctionCall.Name)
				}
			}
		} else {
			// Also check result.FunctionCalls() helper
			if logger != nil {
				logger.Debugf("🔍 [VERTEX] Candidate %d: No tool calls found in candidate.Content.Parts, checking result.FunctionCalls() fallback", i)
			}
			if funcCalls := result.FunctionCalls(); len(funcCalls) > 0 {
				if logger != nil {
					logger.Debugf("🔍 [VERTEX] Candidate %d: Found %d function calls via result.FunctionCalls() fallback", i, len(funcCalls))
				}
				toolCalls = make([]llmtypes.ToolCall, 0, len(funcCalls))
				for k, fc := range funcCalls {
					// 🔧 FIX: Generate ToolCallID for FunctionCall (same as above)
					// This ensures consistent ID generation for all FunctionCalls
					toolCall := llmtypes.ToolCall{
						ID:   generateToolCallID(),
						Type: "function",
						FunctionCall: &llmtypes.FunctionCall{
							Name:      fc.Name,
							Arguments: convertArgumentsToString(fc.Args),
						},
					}
					toolCalls = append(toolCalls, toolCall)
					if logger != nil {
						logger.Debugf("🔍 [VERTEX] Candidate %d, Fallback FunctionCall %d: Created ToolCall - ID: %s, Name: %s", i, k+1, toolCall.ID, toolCall.FunctionCall.Name)
					}
				}
				choice.ToolCalls = toolCalls
				if logger != nil {
					logger.Debugf("🔍 [VERTEX] Candidate %d: Set %d tool calls from result.FunctionCalls() fallback", i, len(toolCalls))
				}
			} else if logger != nil {
				logger.Debugf("🔍 [VERTEX] Candidate %d: No function calls found in result.FunctionCalls() fallback either", i)
			}
		}

		// Final state logging for empty content cases
		if choice.Content == "" && logger != nil {
			logger.Debugf("🔍 [VERTEX] Candidate %d FINAL STATE - Content: empty, ToolCalls: %d", i, len(choice.ToolCalls))
			if len(choice.ToolCalls) > 0 {
				logger.Debugf("🔍 [VERTEX] Candidate %d: This is a VALID tool call response (empty content but tool calls present)", i)
			} else {
				logger.Debugf("🔍 [VERTEX] Candidate %d: This is an INVALID response (empty content AND no tool calls)", i)
			}
		}

		// Extract token usage if available
		if result.UsageMetadata != nil {
			inputTokens := int(result.UsageMetadata.PromptTokenCount)
			outputTokens := int(result.UsageMetadata.CandidatesTokenCount)
			var totalTokens int
			if result.UsageMetadata.TotalTokenCount > 0 {
				totalTokens = int(result.UsageMetadata.TotalTokenCount)
			} else {
				totalTokens = int(result.UsageMetadata.PromptTokenCount + result.UsageMetadata.CandidatesTokenCount)
			}

			genInfo := &llmtypes.GenerationInfo{
				InputTokens:  &inputTokens,
				OutputTokens: &outputTokens,
				TotalTokens:  &totalTokens,
			}

			// Cache token information
			if result.UsageMetadata.CachedContentTokenCount > 0 {
				cachedTokens := int(result.UsageMetadata.CachedContentTokenCount)
				genInfo.CachedContentTokens = &cachedTokens

				// Calculate cache discount percentage (0.0 to 1.0)
				if result.UsageMetadata.PromptTokenCount > 0 {
					cacheDiscount := float64(result.UsageMetadata.CachedContentTokenCount) / float64(result.UsageMetadata.PromptTokenCount)
					genInfo.CacheDiscount = &cacheDiscount
				}
			}

			// Additional token counts if available
			if result.UsageMetadata.ToolUsePromptTokenCount > 0 {
				toolUseTokens := int(result.UsageMetadata.ToolUsePromptTokenCount)
				genInfo.ToolUsePromptTokens = &toolUseTokens
			}

			if result.UsageMetadata.ThoughtsTokenCount > 0 {
				thoughtsTokens := int(result.UsageMetadata.ThoughtsTokenCount)
				genInfo.ThoughtsTokens = &thoughtsTokens
			}

			choice.GenerationInfo = genInfo
		}

		// Set stop reason
		if candidate.FinishReason != "" {
			choice.StopReason = string(candidate.FinishReason)
		}

		choices = append(choices, choice)
	}

	// Final summary logging
	if logger != nil {
		logger.Debugf("🔍 [VERTEX] convertResponse COMPLETE - Returning %d choices", len(choices))
		for i, ch := range choices {
			logger.Debugf("🔍 [VERTEX] Choice %d: Content length: %d, ToolCalls: %d, StopReason: %q",
				i, len(ch.Content), len(ch.ToolCalls), ch.StopReason)
		}
	}

	return &llmtypes.ContentResponse{
		Choices: choices,
	}
}

// Call implements a convenience method that wraps GenerateContent for simple text generation
func (g *GoogleGenAIAdapter) Call(ctx context.Context, prompt string, options ...llmtypes.CallOption) (string, error) {
	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: prompt},
			},
		},
	}

	resp, err := g.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return resp.Choices[0].Content, nil
}

// convertArgumentsToString converts function arguments to JSON string
func convertArgumentsToString(args map[string]interface{}) string {
	if args == nil {
		return "{}"
	}

	bytes, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}

	return string(bytes)
}

// convertMessageParts is a helper to convert llmtypes parts to genai parts
func (g *GoogleGenAIAdapter) convertMessageParts(parts []llmtypes.ContentPart) []*genai.Part {
	genaiParts := make([]*genai.Part, 0)
	for _, part := range parts {
		switch p := part.(type) {
		case llmtypes.TextContent:
			genaiParts = append(genaiParts, genai.NewPartFromText(p.Text))
		case llmtypes.ImageContent:
			// Convert ImageContent to genai.Part
			if g.logger != nil {
				g.logger.Debugf("Converting ImageContent to genai.Part: sourceType=%s, mediaType=%s, dataLength=%d", p.SourceType, p.MediaType, len(p.Data))
			}
			imagePart := g.createImagePart(p)
			if imagePart != nil {
				if g.logger != nil {
					// Log details about the created part
					if imagePart.InlineData != nil {
						g.logger.Debugf("Image part created successfully: MIME type=%s, data length=%d", imagePart.InlineData.MIMEType, len(imagePart.InlineData.Data))
					} else {
						g.logger.Warnf("Image part created but InlineData is nil")
					}
				}
				genaiParts = append(genaiParts, imagePart)
			} else {
				if g.logger != nil {
					g.logger.Warnf("Failed to create image part from ImageContent")
				}
			}
		case llmtypes.ToolCallResponse:
			// Convert tool response to function response format
			// Send the raw string content directly to Gemini - no JSON parsing needed
			// Gemini can handle the string content itself
			responseMap := map[string]interface{}{
				"result": p.Content,
			}
			genaiParts = append(genaiParts, genai.NewPartFromFunctionResponse(p.ToolCallID, responseMap))
		case llmtypes.ToolCall:
			// Convert ToolCall parts to genai.Part with FunctionCall
			if p.FunctionCall != nil {
				// Parse JSON arguments string to map
				argsMap := parseJSONObject(p.FunctionCall.Arguments)
				// Create genai.Part with FunctionCall
				genaiParts = append(genaiParts, genai.NewPartFromFunctionCall(p.FunctionCall.Name, argsMap))
			}
		}
	}
	return genaiParts
}

// parseJSONObject parses a JSON string into a map
func parseJSONObject(jsonStr string) map[string]interface{} {
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return make(map[string]interface{})
	}
	return result
}

// logInputDetails logs the input parameters before making the API call
func (g *GoogleGenAIAdapter) logInputDetails(requestID, modelID string, messages []llmtypes.MessageContent, config *genai.GenerateContentConfig, opts *llmtypes.CallOptions) {
	// Build input summary
	inputSummary := map[string]interface{}{
		"request_id":    requestID,
		"model_id":      modelID,
		"message_count": len(messages),
		"temperature":   opts.Temperature,
		"max_tokens":    opts.MaxTokens,
		"json_mode":     opts.JSONMode,
		"tools_count":   len(opts.Tools),
	}

	// Add message summaries (first 200 chars of each)
	messageSummaries := make([]string, 0, len(messages))
	for i, msg := range messages {
		role := string(msg.Role)
		var contentPreview string
		if len(msg.Parts) > 0 {
			if textPart, ok := msg.Parts[0].(llmtypes.TextContent); ok {
				content := textPart.Text
				if len(content) > 200 {
					contentPreview = content[:200] + "..."
				} else {
					contentPreview = content
				}
			} else {
				contentPreview = fmt.Sprintf("[%T]", msg.Parts[0])
			}
		}
		messageSummaries = append(messageSummaries, fmt.Sprintf("%s: %s", role, contentPreview))
		if i >= 4 { // Limit to first 5 messages
			break
		}
	}
	inputSummary["messages"] = messageSummaries

	// Add config details
	if config.Temperature != nil {
		inputSummary["config_temperature"] = *config.Temperature
	}
	if config.MaxOutputTokens > 0 {
		inputSummary["config_max_output_tokens"] = config.MaxOutputTokens
	}
	if config.ResponseMIMEType != "" {
		inputSummary["config_response_mime_type"] = config.ResponseMIMEType
	}
	if config.ResponseSchema != nil {
		inputSummary["config_has_response_schema"] = true
		inputSummary["config_response_schema_type"] = config.ResponseSchema.Type
	}
	if len(config.Tools) > 0 {
		inputSummary["config_tools_count"] = len(config.Tools)
	}

	inputSummaryJSON, _ := json.MarshalIndent(inputSummary, "", "  ")
	g.logger.Infof("🔍 [REQUEST_ID: %s] MESSAGES SENT TO LLM:\n%s", requestID, string(inputSummaryJSON))
}

// logErrorDetails logs both input and error response details when an error occurs
func (g *GoogleGenAIAdapter) logErrorDetails(requestID, modelID string, messages []llmtypes.MessageContent, config *genai.GenerateContentConfig, opts *llmtypes.CallOptions, err error, result *genai.GenerateContentResponse) {
	// Log error with input context
	errorInfo := map[string]interface{}{
		"request_id":    requestID,
		"error":         err.Error(),
		"model_id":      modelID,
		"message_count": len(messages),
	}

	// Add config summary
	if config.ResponseMIMEType != "" {
		errorInfo["response_mime_type"] = config.ResponseMIMEType
	}
	if config.ResponseSchema != nil {
		errorInfo["has_response_schema"] = true
	}
	if len(config.Tools) > 0 {
		errorInfo["tools_count"] = len(config.Tools)
	}

	// Add response details if available (even though there was an error)
	if result != nil {
		if len(result.Candidates) > 0 {
			candidate := result.Candidates[0]
			if candidate.Content != nil && len(candidate.Content.Parts) > 0 {
				// Try to extract text from parts
				var responsePreview string
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						text := part.Text
						if len(text) > 500 {
							responsePreview = text[:500] + "..."
						} else {
							responsePreview = text
						}
						break
					}
				}
				if responsePreview != "" {
					errorInfo["response_preview"] = responsePreview
				}
			}
		}
		if result.UsageMetadata != nil {
			errorInfo["usage_metadata"] = map[string]interface{}{
				"prompt_token_count":         result.UsageMetadata.PromptTokenCount,
				"candidates_token_count":     result.UsageMetadata.CandidatesTokenCount,
				"cached_content_token_count": result.UsageMetadata.CachedContentTokenCount,
				"total_token_count":          result.UsageMetadata.TotalTokenCount,
			}
		}
		if result.PromptFeedback != nil {
			errorInfo["prompt_feedback"] = map[string]interface{}{
				"block_reason": result.PromptFeedback.BlockReason,
			}
		}
	}

	// Log full input details
	errorInfoJSON, _ := json.MarshalIndent(errorInfo, "", "  ")
	g.logger.Errorf("❌ [REQUEST_ID: %s] Google GenAI GenerateContent ERROR:\n%s", requestID, string(errorInfoJSON))

	// Also log input details for full context
	g.logInputDetails(requestID, modelID, messages, config, opts)
}

// logRawResponse logs the complete raw GenAI API response as JSON for debugging
func (g *GoogleGenAIAdapter) logRawResponse(requestID, modelID string, result *genai.GenerateContentResponse, err error) {
	g.logger.Infof("🔍 [REQUEST_ID: %s] Raw Vertex (GenAI) response received - model: %s, err: %v, result: %v", requestID, modelID, err != nil, result != nil)

	if result == nil {
		g.logger.Infof("🔍 [REQUEST_ID: %s] Raw Vertex response is nil", requestID)
		return
	}

	// Log response structure summary
	g.logger.Infof("🔍 [REQUEST_ID: %s] Raw Vertex response structure - Candidates: %d", requestID, len(result.Candidates))

	// Log candidates details
	for i, candidate := range result.Candidates {
		g.logger.Infof("🔍 [REQUEST_ID: %s] Candidate %d:", requestID, i)
		g.logger.Infof("🔍 [REQUEST_ID: %s]    FinishReason: %q", requestID, candidate.FinishReason)
		if candidate.Content != nil {
			g.logger.Infof("🔍 [REQUEST_ID: %s]    Content.Parts count: %d", requestID, len(candidate.Content.Parts))
			for j, part := range candidate.Content.Parts {
				if part.Text != "" {
					textPreview := part.Text
					if len(textPreview) > 200 {
						textPreview = textPreview[:200] + "..."
					}
					g.logger.Infof("🔍 [REQUEST_ID: %s]      Part %d - Text: %q (length: %d)", requestID, j, textPreview, len(part.Text))
				}
				if part.FunctionCall != nil {
					// Log full FunctionCall arguments as JSON
					argsJSON := convertArgumentsToString(part.FunctionCall.Args)
					if len(argsJSON) > 1000 {
						argsPreview := argsJSON[:1000] + "... (truncated, total length: " + fmt.Sprintf("%d", len(argsJSON)) + " bytes)"
						g.logger.Infof("🔍 [REQUEST_ID: %s]      Part %d - FunctionCall: Name=%q, Args=%s", requestID, j, part.FunctionCall.Name, argsPreview)
					} else {
						g.logger.Infof("🔍 [REQUEST_ID: %s]      Part %d - FunctionCall: Name=%q, Args=%s", requestID, j, part.FunctionCall.Name, argsJSON)
					}
				}
			}
		} else {
			g.logger.Infof("🔍 [REQUEST_ID: %s]    Content: nil", requestID)
		}
	}

	// Log usage metadata
	if result.UsageMetadata != nil {
		g.logger.Infof("🔍 [REQUEST_ID: %s] UsageMetadata:", requestID)
		g.logger.Infof("🔍 [REQUEST_ID: %s]    PromptTokenCount: %d", requestID, result.UsageMetadata.PromptTokenCount)
		g.logger.Infof("🔍 [REQUEST_ID: %s]    CandidatesTokenCount: %d", requestID, result.UsageMetadata.CandidatesTokenCount)
		g.logger.Infof("🔍 [REQUEST_ID: %s]    TotalTokenCount: %d", requestID, result.UsageMetadata.TotalTokenCount)
		g.logger.Infof("🔍 [REQUEST_ID: %s]    CachedContentTokenCount: %d", requestID, result.UsageMetadata.CachedContentTokenCount)
		g.logger.Infof("🔍 [REQUEST_ID: %s]    ToolUsePromptTokenCount: %d", requestID, result.UsageMetadata.ToolUsePromptTokenCount)
		g.logger.Infof("🔍 [REQUEST_ID: %s]    ThoughtsTokenCount: %d", requestID, result.UsageMetadata.ThoughtsTokenCount)
	}

	// Log prompt feedback if available
	// Note: PromptFeedback.BlockReason typically indicates the API call failed with an error,
	// not just returned empty content. If we're here (no error), BlockReason is unlikely but worth logging.
	if result.PromptFeedback != nil {
		g.logger.Infof("🔍 [REQUEST_ID: %s] PromptFeedback:", requestID)
		g.logger.Infof("🔍 [REQUEST_ID: %s]    BlockReason: %q", requestID, result.PromptFeedback.BlockReason)
		if result.PromptFeedback.BlockReason != "" {
			g.logger.Warnf("⚠️ [REQUEST_ID: %s] PromptFeedback.BlockReason present: %q (Note: Safety blocks usually cause API errors, not empty content)", requestID, result.PromptFeedback.BlockReason)
		}
		if len(result.PromptFeedback.SafetyRatings) > 0 {
			g.logger.Infof("🔍 [REQUEST_ID: %s]    SafetyRatings count: %d", requestID, len(result.PromptFeedback.SafetyRatings))
			for k, rating := range result.PromptFeedback.SafetyRatings {
				g.logger.Infof("🔍 [REQUEST_ID: %s]      SafetyRating %d - Category: %q, Probability: %q", requestID, k, rating.Category, rating.Probability)
			}
		}
	}

	// Try to serialize the full response to JSON for complete debugging
	// Note: This may fail if genai.GenerateContentResponse has unexported fields or circular references
	// We'll log what we can extract manually above, but try JSON as well
	type functionCallSummary struct {
		Name string
		Args string // JSON string of arguments
	}

	type responseSummary struct {
		CandidatesCount             int
		HasUsageMetadata            bool
		HasPromptFeedback           bool
		FirstCandidateFinishReason  string
		FirstCandidatePartsCount    int
		FirstCandidateTextLength    int
		ResultTextHelper            string
		FirstCandidateFunctionCalls []functionCallSummary
	}

	summary := responseSummary{
		CandidatesCount:   len(result.Candidates),
		HasUsageMetadata:  result.UsageMetadata != nil,
		HasPromptFeedback: result.PromptFeedback != nil,
	}

	if len(result.Candidates) > 0 {
		firstCandidate := result.Candidates[0]
		summary.FirstCandidateFinishReason = string(firstCandidate.FinishReason)
		if firstCandidate.Content != nil {
			summary.FirstCandidatePartsCount = len(firstCandidate.Content.Parts)
			summary.FirstCandidateFunctionCalls = make([]functionCallSummary, 0)
			for _, part := range firstCandidate.Content.Parts {
				summary.FirstCandidateTextLength += len(part.Text)
				if part.FunctionCall != nil {
					summary.FirstCandidateFunctionCalls = append(summary.FirstCandidateFunctionCalls, functionCallSummary{
						Name: part.FunctionCall.Name,
						Args: convertArgumentsToString(part.FunctionCall.Args),
					})
				}
			}
		}
		summary.ResultTextHelper = result.Text()
	}

	if summaryJSON, err := json.MarshalIndent(summary, "   ", "  "); err == nil {
		jsonStr := string(summaryJSON)
		if len(jsonStr) > 5000 {
			jsonStr = jsonStr[:5000] + "\n   ... (truncated)"
		}
		g.logger.Infof("🔍 [REQUEST_ID: %s] RAW VERTEX RESPONSE SUMMARY (JSON):\n   %s", requestID, jsonStr)
	} else {
		g.logger.Warnf("⚠️ [REQUEST_ID: %s] Failed to serialize response summary to JSON: %v", requestID, err)
	}
}

// WithResponseSchema returns a context with the ResponseSchema set
// This allows structured output generation with schema validation
func WithResponseSchema(ctx context.Context, schema *genai.Schema) context.Context {
	return context.WithValue(ctx, ResponseSchemaKey, schema)
}

// createImagePart creates a genai.Part from ImageContent
func (g *GoogleGenAIAdapter) createImagePart(img llmtypes.ImageContent) *genai.Part {
	if img.SourceType == "base64" {
		// Decode base64 string to bytes
		imageBytes, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			if g.logger != nil {
				g.logger.Warnf("Failed to decode base64 image: %v", err)
			}
			return nil
		}
		if g.logger != nil {
			g.logger.Debugf("Created image part from base64: %d bytes, MIME type: %s", len(imageBytes), img.MediaType)
		}
		// Use NewPartFromBytes with decoded bytes and MIME type
		return genai.NewPartFromBytes(imageBytes, img.MediaType)
	} else if img.SourceType == "url" {
		// Fetch image from URL and convert to bytes
		if g.logger != nil {
			g.logger.Debugf("Fetching image from URL: %s", img.Data)
		}
		imageBytes, mimeType, err := g.fetchImageFromURL(img.Data)
		if err != nil {
			if g.logger != nil {
				g.logger.Warnf("Failed to fetch image from URL %s: %v", img.Data, err)
			}
			return nil
		}
		if g.logger != nil {
			g.logger.Debugf("Created image part from URL: %d bytes, MIME type: %s", len(imageBytes), mimeType)
		}
		// Use NewPartFromBytes with fetched bytes and detected MIME type
		return genai.NewPartFromBytes(imageBytes, mimeType)
	}
	// Invalid source type
	if g.logger != nil {
		g.logger.Warnf("Invalid image source type: %s", img.SourceType)
	}
	return nil
}

// fetchImageFromURL fetches an image from a URL and returns the bytes and MIME type
func (g *GoogleGenAIAdapter) fetchImageFromURL(url string) ([]byte, string, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Fetch the image
	resp, err := client.Get(url)
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch image: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read image data
	imageBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read image data: %w", err)
	}

	// Detect MIME type from Content-Type header or URL extension
	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" || !strings.HasPrefix(mimeType, "image/") {
		// Try to detect from URL extension
		urlLower := strings.ToLower(url)
		if strings.HasSuffix(urlLower, ".jpg") || strings.HasSuffix(urlLower, ".jpeg") {
			mimeType = "image/jpeg"
		} else if strings.HasSuffix(urlLower, ".png") {
			mimeType = "image/png"
		} else if strings.HasSuffix(urlLower, ".gif") {
			mimeType = "image/gif"
		} else if strings.HasSuffix(urlLower, ".webp") {
			mimeType = "image/webp"
		} else {
			// Default to JPEG if we can't determine
			mimeType = "image/jpeg"
		}
	}

	// Clean up MIME type (remove charset if present)
	if idx := strings.Index(mimeType, ";"); idx != -1 {
		mimeType = mimeType[:idx]
	}

	return imageBytes, strings.TrimSpace(mimeType), nil
}

// generateToolCallID generates a unique ID for tool calls
// In a real implementation, you might want to use a proper ID generator
var toolCallCounter int64 = 0

func generateToolCallID() string {
	toolCallCounter++
	return fmt.Sprintf("call_%d", toolCallCounter)
}
