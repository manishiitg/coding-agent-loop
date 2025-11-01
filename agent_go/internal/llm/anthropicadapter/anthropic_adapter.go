package anthropicadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mcp-agent/agent_go/internal/utils"

	"github.com/anthropics/anthropic-sdk-go"

	"mcp-agent/agent_go/internal/llmtypes"
)

// AnthropicAdapter is an adapter that implements llmtypes.Model interface
// using the Anthropic SDK directly
type AnthropicAdapter struct {
	client  anthropic.Client
	modelID string
	logger  utils.ExtendedLogger
}

// NewAnthropicAdapter creates a new adapter instance
func NewAnthropicAdapter(client anthropic.Client, modelID string, logger utils.ExtendedLogger) *AnthropicAdapter {
	return &AnthropicAdapter{
		client:  client,
		modelID: modelID,
		logger:  logger,
	}
}

// GenerateContent implements the llmtypes.Model interface
func (a *AnthropicAdapter) GenerateContent(ctx context.Context, messages []llmtypes.MessageContent, options ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	// Parse call options
	opts := &llmtypes.CallOptions{}
	for _, opt := range options {
		opt(opts)
	}

	// Determine model ID (from option or default)
	modelID := a.modelID
	if opts.Model != "" {
		modelID = opts.Model
	}

	// Convert messages from llm format to Anthropic format
	anthropicMessages, systemMessage := convertMessages(messages)

	// Build MessageNewParams from options
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(modelID),
		Messages:  anthropicMessages,
		MaxTokens: 4096, // Default max tokens
	}

	// Set system message if present
	if systemMessage != "" {
		// Handle JSON mode by appending instruction to system message
		if opts.JSONMode {
			systemMessage = systemMessage + "\n\nYou must respond with valid JSON only, no other text. Return a JSON object."
		}
		params.System = []anthropic.TextBlockParam{
			{Text: systemMessage},
		}
	} else if opts.JSONMode && len(anthropicMessages) > 0 {
		// If no system message, prepend JSON instruction to first user message
		jsonInstruction := anthropic.NewTextBlock("You must respond with valid JSON only, no other text. Return a JSON object.")
		if len(anthropicMessages) > 0 && anthropicMessages[0].Role == anthropic.MessageParamRoleUser {
			anthropicMessages[0].Content = append([]anthropic.ContentBlockParamUnion{jsonInstruction}, anthropicMessages[0].Content...)
		}
	}

	// Set temperature
	if opts.Temperature > 0 {
		params.Temperature = anthropic.Float(opts.Temperature)
	}

	// Set max tokens
	if opts.MaxTokens > 0 {
		params.MaxTokens = int64(opts.MaxTokens)
	}

	// Convert tools if provided
	if len(opts.Tools) > 0 {
		tools := convertTools(opts.Tools)
		params.Tools = tools

		// Handle tool choice
		if opts.ToolChoice != nil {
			toolChoice := convertToolChoice(opts.ToolChoice)
			params.ToolChoice = toolChoice
		}
	}

	// Log input details if logger is available (for debugging errors)
	if a.logger != nil {
		a.logInputDetails(modelID, messages, params, opts)
	}

	// Always use streaming API for Anthropic to avoid "streaming is required" error
	// Anthropic requires streaming for operations that may take longer than 10 minutes
	// Using NewStreaming() disables this error check regardless of actual request size
	stream := a.client.Messages.NewStreaming(ctx, params)

	// Use Message.Accumulate to build the final message
	message := anthropic.Message{}
	for stream.Next() {
		event := stream.Current()

		// Accumulate event into message
		if err := message.Accumulate(event); err != nil {
			stream.Close()
			if a.logger != nil {
				a.logErrorDetails(modelID, messages, params, opts, err, &message)
			}
			return nil, fmt.Errorf("anthropic streaming accumulate error: %w", err)
		}

		// If streaming callback is provided, extract and send text chunks
		if opts.StreamingFunc != nil {
			switch eventVariant := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				// Check if this is a text delta
				switch deltaVariant := eventVariant.Delta.AsAny().(type) {
				case anthropic.TextDelta:
					if deltaVariant.Text != "" {
						if opts.StreamingFunc != nil {
							opts.StreamingFunc(deltaVariant.Text)
						}
					}
				}
			}
		}
	}

	// Check for stream errors
	if err := stream.Err(); err != nil {
		if a.logger != nil {
			a.logErrorDetails(modelID, messages, params, opts, err, &message)
		}
		return nil, fmt.Errorf("anthropic streaming error: %w", err)
	}
	stream.Close()

	// Convert the accumulated message to llm format
	return convertResponse(&message), nil
}

// Call implements a convenience method that wraps GenerateContent for simple text generation
func (a *AnthropicAdapter) Call(ctx context.Context, prompt string, options ...llmtypes.CallOption) (string, error) {
	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: prompt},
			},
		},
	}

	resp, err := a.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return resp.Choices[0].Content, nil
}

// convertMessages converts llmtypes messages to Anthropic message format
// Returns messages and system message (if present)
func convertMessages(langMessages []llmtypes.MessageContent) ([]anthropic.MessageParam, string) {
	anthropicMessages := make([]anthropic.MessageParam, 0, len(langMessages))
	var systemMessage string

	for _, msg := range langMessages {
		// Extract content parts
		var contentParts []string
		var toolCallID string
		var toolResponseContent string
		var toolCalls []llmtypes.ToolCall

		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				contentParts = append(contentParts, p.Text)
			case llmtypes.ToolCallResponse:
				// Tool response - extract tool call ID and content
				toolCallID = p.ToolCallID
				toolResponseContent = p.Content
			case llmtypes.ToolCall:
				// Tool call in assistant message
				toolCalls = append(toolCalls, p)
			}
		}

		// Handle different message roles
		switch string(msg.Role) {
		case string(llmtypes.ChatMessageTypeSystem):
			// System messages go to the system parameter, not messages array
			if len(contentParts) > 0 {
				systemMessage = strings.Join(contentParts, "\n")
			}
		case string(llmtypes.ChatMessageTypeHuman):
			// User message
			content := ""
			if len(contentParts) > 0 {
				content = strings.Join(contentParts, "\n")
			}

			// Create text content block using helper
			contentBlock := anthropic.NewTextBlock(content)

			anthropicMessages = append(anthropicMessages, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{contentBlock},
			})
		case string(llmtypes.ChatMessageTypeAI):
			// Assistant message can have text content or tool calls
			content := ""
			if len(contentParts) > 0 {
				content = strings.Join(contentParts, "\n")
			}

			// If there are tool calls, include them
			if len(toolCalls) > 0 {
				// Convert tool calls to Anthropic format
				contentBlocks := []anthropic.ContentBlockParamUnion{}
				if content != "" {
					contentBlocks = append(contentBlocks, anthropic.NewTextBlock(content))
				}
				for _, tc := range toolCalls {
					// Parse arguments
					var args map[string]interface{}
					if tc.FunctionCall.Arguments != "" {
						if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err != nil {
							// If parsing fails, create empty map
							args = make(map[string]interface{})
						}
					} else {
						args = make(map[string]interface{})
					}

					// Create tool use block using helper
					toolUseBlock := anthropic.NewToolUseBlock(tc.ID, args, tc.FunctionCall.Name)
					contentBlocks = append(contentBlocks, toolUseBlock)
				}

				anthropicMessages = append(anthropicMessages, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleAssistant,
					Content: contentBlocks,
				})
			} else {
				// Assistant message with just text
				contentBlock := anthropic.NewTextBlock(content)

				anthropicMessages = append(anthropicMessages, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleAssistant,
					Content: []anthropic.ContentBlockParamUnion{contentBlock},
				})
			}
		case string(llmtypes.ChatMessageTypeTool):
			// Tool message - handle tool responses
			if toolCallID != "" {
				// Create tool result content block using helper
				// isError is false - we could enhance this to detect errors
				contentBlock := anthropic.NewToolResultBlock(toolCallID, toolResponseContent, false)

				anthropicMessages = append(anthropicMessages, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleUser,
					Content: []anthropic.ContentBlockParamUnion{contentBlock},
				})
			}
		default:
			// Default to user message
			content := ""
			if len(contentParts) > 0 {
				content = strings.Join(contentParts, "\n")
			}

			contentBlock := anthropic.NewTextBlock(content)

			anthropicMessages = append(anthropicMessages, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleUser,
				Content: []anthropic.ContentBlockParamUnion{contentBlock},
			})
		}
	}

	return anthropicMessages, systemMessage
}

// convertTools converts llmtypes tools to Anthropic tool format
func convertTools(llmTools []llmtypes.Tool) []anthropic.ToolUnionParam {
	anthropicTools := make([]anthropic.ToolUnionParam, 0, len(llmTools))

	for _, tool := range llmTools {
		if tool.Function == nil {
			continue
		}

		// Extract function parameters as JSON schema
		var parameters map[string]interface{}
		if tool.Function.Parameters != nil {
			// Convert from typed Parameters to map
			// Parameters is now *llmtypes.Parameters, so convert it to map
			paramsBytes, err := json.Marshal(tool.Function.Parameters)
			if err == nil {
				var paramsMap map[string]interface{}
				if err := json.Unmarshal(paramsBytes, &paramsMap); err == nil {
					parameters = paramsMap
				}
			}
		}

		if parameters == nil {
			parameters = make(map[string]interface{})
		}

		// Extract required fields from parameters if available
		var required []string
		if req, ok := parameters["required"].([]interface{}); ok {
			required = make([]string, 0, len(req))
			for _, r := range req {
				if str, ok := r.(string); ok {
					required = append(required, str)
				}
			}
		}

		// Extract properties (remove type and required from parameters for InputSchema)
		properties := make(map[string]interface{})
		if props, ok := parameters["properties"].(map[string]interface{}); ok {
			properties = props
		}

		// Create Anthropic tool with InputSchema using helper
		// Type defaults to "object" if elided
		inputSchema := anthropic.ToolInputSchemaParam{
			Properties: properties,
			Required:   required,
		}
		anthropicTool := anthropic.ToolUnionParamOfTool(inputSchema, tool.Function.Name)

		// Set description if available
		if tool.Function.Description != "" {
			// Note: ToolUnionParam doesn't directly expose Description, so we add it to the schema description if needed
			// For now, we'll just use the tool name
		}

		anthropicTools = append(anthropicTools, anthropicTool)
	}

	return anthropicTools
}

// convertToolChoice converts langchaingo tool choice to Anthropic tool choice format
func convertToolChoice(toolChoice interface{}) anthropic.ToolChoiceUnionParam {
	// Handle string-based tool choice
	if choiceStr, ok := toolChoice.(string); ok {
		switch choiceStr {
		case "auto":
			return anthropic.ToolChoiceUnionParam{
				OfAuto: &anthropic.ToolChoiceAutoParam{},
			}
		case "none":
			return anthropic.ToolChoiceUnionParam{
				OfNone: &anthropic.ToolChoiceNoneParam{},
			}
		case "required":
			return anthropic.ToolChoiceUnionParam{
				OfAny: &anthropic.ToolChoiceAnyParam{},
			}
		default:
			// Default to auto
			return anthropic.ToolChoiceUnionParam{
				OfAuto: &anthropic.ToolChoiceAutoParam{},
			}
		}
	}

	// Handle ToolChoice struct if it's that type
	if tc, ok := toolChoice.(*llmtypes.ToolChoice); ok && tc != nil {
		// For now, default to auto - could be enhanced to handle function-specific choices
		return anthropic.ToolChoiceUnionParam{
			OfAuto: &anthropic.ToolChoiceAutoParam{},
		}
	}

	// Handle map-based tool choice (from ConvertToolChoice)
	if choiceMap, ok := toolChoice.(map[string]interface{}); ok {
		if typ, ok := choiceMap["type"].(string); ok && typ == "function" {
			if fnMap, ok := choiceMap["function"].(map[string]interface{}); ok {
				if name, ok := fnMap["name"].(string); ok {
					// Function-specific tool choice
					return anthropic.ToolChoiceParamOfTool(name)
				}
			}
		}
	}

	// Default to auto
	return anthropic.ToolChoiceUnionParam{
		OfAuto: &anthropic.ToolChoiceAutoParam{},
	}
}

// convertResponse converts Anthropic response to llmtypes ContentResponse
func convertResponse(result *anthropic.Message) *llmtypes.ContentResponse {
	if result == nil {
		return &llmtypes.ContentResponse{
			Choices: []*llmtypes.ContentChoice{},
		}
	}

	choices := make([]*llmtypes.ContentChoice, 0, 1) // Anthropic typically returns one choice

	choice := &llmtypes.ContentChoice{}

	// Extract text content and tool calls from content blocks
	var textParts []string
	var toolCalls []llmtypes.ToolCall

	// Content is a slice of ContentBlockUnion
	for _, block := range result.Content {
		// ContentBlockUnion uses Type field to determine the variant
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		case "tool_use":
			// Convert tool use to tool call
			var argsJSON []byte
			if len(block.Input) > 0 {
				argsJSON = block.Input
			} else {
				argsJSON = []byte("{}")
			}

			toolCall := llmtypes.ToolCall{
				ID:   block.ID,
				Type: "function",
				FunctionCall: &llmtypes.FunctionCall{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			}
			toolCalls = append(toolCalls, toolCall)
		}
	}

	// Combine text parts
	if len(textParts) > 0 {
		choice.Content = strings.Join(textParts, "\n")
	}

	// Set tool calls if any
	if len(toolCalls) > 0 {
		choice.ToolCalls = toolCalls
	}

	// Extract stop reason
	if result.StopReason != "" {
		choice.StopReason = string(result.StopReason)
	}

	// Extract token usage if available
	// Usage is not a pointer in Anthropic SDK
	inputTokens := int(result.Usage.InputTokens)
	outputTokens := int(result.Usage.OutputTokens)
	totalTokens := int(result.Usage.InputTokens + result.Usage.OutputTokens)

	genInfo := &llmtypes.GenerationInfo{
		InputTokens:     &inputTokens,
		OutputTokens:    &outputTokens,
		TotalTokens:     &totalTokens,
		InputTokensCap:  &inputTokens,
		OutputTokensCap: &outputTokens,
	}

	// Cache tokens if available
	if result.Usage.CacheReadInputTokens > 0 {
		cacheReadTokens := int(result.Usage.CacheReadInputTokens)
		genInfo.Additional = make(map[string]interface{})
		genInfo.Additional["cache_read_input_tokens"] = cacheReadTokens
		genInfo.Additional["CacheReadInputTokens"] = cacheReadTokens
	}
	if result.Usage.CacheCreationInputTokens > 0 {
		cacheCreationTokens := int(result.Usage.CacheCreationInputTokens)
		if genInfo.Additional == nil {
			genInfo.Additional = make(map[string]interface{})
		}
		genInfo.Additional["cache_creation_input_tokens"] = cacheCreationTokens
		genInfo.Additional["CacheCreationInputTokens"] = cacheCreationTokens
	}

	choice.GenerationInfo = genInfo

	choices = append(choices, choice)

	return &llmtypes.ContentResponse{
		Choices: choices,
	}
}

// logInputDetails logs the input parameters before making the API call
func (a *AnthropicAdapter) logInputDetails(modelID string, messages []llmtypes.MessageContent, params anthropic.MessageNewParams, opts *llmtypes.CallOptions) {
	// Build input summary
	inputSummary := map[string]interface{}{
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

	// Add params details
	// Temperature is param.Opt[float64] - always log if set (param.Opt has IsOmitted check)
	// Since we only set it if opts.Temperature > 0, we can check that
	if opts.Temperature > 0 {
		inputSummary["params_temperature"] = opts.Temperature
	}
	if params.MaxTokens > 0 {
		inputSummary["params_max_tokens"] = params.MaxTokens
	}
	if len(params.System) > 0 {
		inputSummary["params_has_system"] = true
	}
	if len(params.Tools) > 0 {
		inputSummary["params_tools_count"] = len(params.Tools)
	}
	// Check if tool choice is set (check if any field is non-nil)
	if params.ToolChoice.OfAuto != nil || params.ToolChoice.OfAny != nil || params.ToolChoice.OfTool != nil || params.ToolChoice.OfNone != nil {
		inputSummary["params_tool_choice"] = "set"
	}

	a.logger.Debugf("Anthropic GenerateContent INPUT - %+v", inputSummary)
}

// logErrorDetails logs both input and error response details when an error occurs
func (a *AnthropicAdapter) logErrorDetails(modelID string, messages []llmtypes.MessageContent, params anthropic.MessageNewParams, opts *llmtypes.CallOptions, err error, result *anthropic.Message) {
	// Log error with input context
	errorInfo := map[string]interface{}{
		"error":         err.Error(),
		"error_type":    fmt.Sprintf("%T", err),
		"model_id":      modelID,
		"message_count": len(messages),
	}

	// Extract detailed error information if it's an API error
	// Anthropic SDK uses shared.Error types - check for APIErrorObject
	if errMsg := err.Error(); errMsg != "" {
		errorInfo["error_details"] = errMsg
	}

	// Add params summary
	if opts.Temperature > 0 {
		errorInfo["temperature"] = opts.Temperature
	}
	if params.MaxTokens > 0 {
		errorInfo["max_tokens"] = params.MaxTokens
	}
	if len(params.System) > 0 {
		errorInfo["has_system"] = true
	}
	if len(params.Tools) > 0 {
		errorInfo["tools_count"] = len(params.Tools)
	}

	// Add response details if available (even though there was an error)
	if result != nil {
		responseInfo := map[string]interface{}{}

		// Extract content preview
		for _, block := range result.Content {
			if block.Type == "text" && block.Text != "" {
				content := block.Text
				if len(content) > 500 {
					content = content[:500] + "..."
				}
				responseInfo["content_preview"] = content
				responseInfo["content_length"] = len(block.Text)
				break
			}
		}

		if len(result.Content) > 0 {
			responseInfo["content_blocks_count"] = len(result.Content)
		}
		if result.StopReason != "" {
			responseInfo["stop_reason"] = string(result.StopReason)
		}

		if len(responseInfo) > 0 {
			errorInfo["response"] = responseInfo
		}

		// Add usage information (Usage is not a pointer)
		errorInfo["usage"] = map[string]interface{}{
			"input_tokens":  result.Usage.InputTokens,
			"output_tokens": result.Usage.OutputTokens,
		}
	}

	// Log comprehensive error information
	a.logger.Errorf("Anthropic GenerateContent ERROR - %+v", errorInfo)

	// Also log input details for full context
	a.logInputDetails(modelID, messages, params, opts)
}
