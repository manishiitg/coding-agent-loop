package openaiadapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"mcp-agent/agent_go/internal/utils"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"

	"mcp-agent/agent_go/internal/llmtypes"
)

// OpenAIAdapter is an adapter that implements llmtypes.Model interface
// using the OpenAI Go SDK directly
type OpenAIAdapter struct {
	client  *openai.Client
	modelID string
	logger  utils.ExtendedLogger
}

// NewOpenAIAdapter creates a new adapter instance
func NewOpenAIAdapter(client *openai.Client, modelID string, logger utils.ExtendedLogger) *OpenAIAdapter {
	return &OpenAIAdapter{
		client:  client,
		modelID: modelID,
		logger:  logger,
	}
}

// GenerateContent implements the llmtypes.Model interface
func (o *OpenAIAdapter) GenerateContent(ctx context.Context, messages []llmtypes.MessageContent, options ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	// Parse call options
	opts := &llmtypes.CallOptions{}
	for _, opt := range options {
		opt(opts)
	}

	// Determine model ID (from option or default)
	modelID := o.modelID
	if opts.Model != "" {
		modelID = opts.Model
	}

	// Convert messages from llmtypes format to OpenAI format
	openaiMessages := convertMessages(messages, o.logger)

	// Build ChatCompletionNewParams from options
	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(modelID),
		Messages: openaiMessages,
	}

	// Set temperature - some models (gpt-5, o1, o3, o4) only support default temperature (1.0)
	// Check if model has temperature restrictions
	if opts.Temperature > 0 && !hasTemperatureRestrictions(modelID) {
		params.Temperature = param.NewOpt(opts.Temperature)
	} else if opts.Temperature > 0 && hasTemperatureRestrictions(modelID) {
		// Model has temperature restrictions - use default (1.0) or omit
		// For models that only support default, we omit the parameter to let OpenAI use default
		if o.logger != nil {
			o.logger.Warnf("Model %s only supports default temperature (1.0), omitting temperature parameter", modelID)
		}
		// Don't set temperature - OpenAI will use default
	}

	// Note: max_tokens is omitted - OpenAI API will use model defaults
	// Some newer models (o1, o3, o4, gpt-4.1) don't support max_tokens and require max_completion_tokens instead
	// To avoid parameter compatibility issues, we omit it entirely

	// Handle JSON mode if specified
	if opts.JSONMode {
		jsonObjParam := shared.NewResponseFormatJSONObjectParam()
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &jsonObjParam,
		}
	}

	// Convert tools if provided
	if len(opts.Tools) > 0 {
		tools := convertTools(opts.Tools)
		params.Tools = tools

		// Handle tool choice
		if opts.ToolChoice != nil {
			toolChoice := convertToolChoice(opts.ToolChoice)
			if toolChoice != nil {
				params.ToolChoice = *toolChoice
			}
		}
	}

	// Log input details if logger is available (for debugging errors)
	if o.logger != nil {
		o.logInputDetails(modelID, messages, params, opts)
	}

	// Call OpenAI API
	result, err := o.client.Chat.Completions.New(ctx, params)
	if err != nil {
		// Log error with input and response details
		if o.logger != nil {
			o.logErrorDetails(modelID, messages, params, opts, err, result)
		}
		return nil, fmt.Errorf("openai generate content: %w", err)
	}

	// Convert response from OpenAI format to llmtypes format
	return convertResponse(result), nil
}

// hasTemperatureRestrictions checks if a model only supports default temperature (1.0)
// Models like gpt-5, gpt-5-mini, o1, o3, o4 only support the default temperature value
func hasTemperatureRestrictions(modelID string) bool {
	modelIDLower := strings.ToLower(modelID)
	restrictedModels := []string{
		"gpt-5",
		"gpt-5-mini",
		"o1",
		"o1-mini",
		"o1-preview",
		"o3",
		"o3-mini",
		"o4",
		"o4-mini",
	}

	for _, restricted := range restrictedModels {
		if strings.Contains(modelIDLower, restricted) {
			return true
		}
	}
	return false
}

// convertMessages converts llmtypes messages to OpenAI message format
func convertMessages(langMessages []llmtypes.MessageContent, logger utils.ExtendedLogger) []openai.ChatCompletionMessageParamUnion {
	openaiMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(langMessages))

	for _, msg := range langMessages {
		// Extract content parts
		var contentParts []string
		var imageParts []llmtypes.ImageContent
		var toolResponses []llmtypes.ToolCallResponse // Support multiple tool responses
		var toolCalls []llmtypes.ToolCall

		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				contentParts = append(contentParts, p.Text)
			case llmtypes.ImageContent:
				imageParts = append(imageParts, p)
			case llmtypes.ToolCallResponse:
				// Collect all tool responses (a message can have multiple tool responses)
				toolResponses = append(toolResponses, p)
			case llmtypes.ToolCall:
				// Tool call in assistant message
				toolCalls = append(toolCalls, p)
			}
		}

		// Create appropriate message type based on role
		switch string(msg.Role) {
		case string(llmtypes.ChatMessageTypeSystem):
			content := ""
			if len(contentParts) > 0 {
				content = contentParts[0]
				// If multiple parts, join them
				for i := 1; i < len(contentParts); i++ {
					content += "\n" + contentParts[i]
				}
			}
			openaiMessages = append(openaiMessages, openai.SystemMessage(content))
		case string(llmtypes.ChatMessageTypeHuman):
			// User message can have text and/or images
			// If images are present, use content array format
			if len(imageParts) > 0 {
				// Build content array with text and image parts
				contentPartsArray := make([]openai.ChatCompletionContentPartUnionParam, 0)

				// Add text parts
				for _, text := range contentParts {
					if text != "" {
						contentPartsArray = append(contentPartsArray, openai.TextContentPart(text))
					}
				}

				// Add image parts
				for _, img := range imageParts {
					imagePart := createImageContentPart(img)
					if imagePart != nil {
						contentPartsArray = append(contentPartsArray, *imagePart)
					}
				}

				// Only add message if there's content
				if len(contentPartsArray) > 0 {
					openaiMessages = append(openaiMessages, openai.UserMessage(contentPartsArray))
				}
			} else {
				// Text-only message (existing behavior)
				content := ""
				if len(contentParts) > 0 {
					content = contentParts[0]
					// If multiple parts, join them
					for i := 1; i < len(contentParts); i++ {
						content += "\n" + contentParts[i]
					}
				}
				openaiMessages = append(openaiMessages, openai.UserMessage(content))
			}
		case string(llmtypes.ChatMessageTypeAI):
			// Assistant message can have text content or tool calls
			content := ""
			if len(contentParts) > 0 {
				content = contentParts[0]
				for i := 1; i < len(contentParts); i++ {
					content += "\n" + contentParts[i]
				}
			}
			// If there are tool calls, include them
			if len(toolCalls) > 0 {
				// Convert tool calls to OpenAI format
				openaiToolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(toolCalls))
				for _, tc := range toolCalls {
					// Arguments are already in JSON string format
					functionToolCall := openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      tc.FunctionCall.Name,
						Arguments: tc.FunctionCall.Arguments, // Already a JSON string
					}

					openaiToolCalls = append(openaiToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
						OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
							ID:       tc.ID,
							Type:     "function", // constant.Function value
							Function: functionToolCall,
						},
					})
				}

				// Create assistant message with tool calls
				assistantMsg := openai.ChatCompletionAssistantMessageParam{
					ToolCalls: openaiToolCalls,
				}
				if content != "" {
					assistantMsg.Content = openai.ChatCompletionAssistantMessageParamContentUnion{
						OfString: param.NewOpt(content),
					}
				}

				openaiMessages = append(openaiMessages, openai.ChatCompletionMessageParamUnion{
					OfAssistant: &assistantMsg,
				})
			} else {
				openaiMessages = append(openaiMessages, openai.AssistantMessage(content))
			}
		case string(llmtypes.ChatMessageTypeTool):
			// Tool message - handle tool responses
			// A single message can contain multiple tool responses, each needs to be a separate tool message
			if len(toolResponses) > 0 {
				for _, toolResp := range toolResponses {
					if toolResp.ToolCallID == "" {
						// Skip tool responses without a tool call ID (invalid)
						if logger != nil {
							logger.Warnf("⚠️ Skipping tool response with empty ToolCallID - Name: %s, Content length: %d", toolResp.Name, len(toolResp.Content))
						}
						continue
					}
					// Use raw content directly (can be JSON string or plain text)
					// OpenAI allows empty content for tool responses
					openaiMessages = append(openaiMessages, openai.ToolMessage(toolResp.Content, toolResp.ToolCallID))
					if logger != nil {
						logger.Debugf("✅ Added tool message - ToolCallID: %s, Name: %s, Content length: %d", toolResp.ToolCallID, toolResp.Name, len(toolResp.Content))
					}
				}
			} else {
				// No tool responses found in a tool message - this is unusual
				if logger != nil {
					logger.Warnf("⚠️ Tool message has no ToolCallResponse parts - skipping message")
				}
			}
		default:
			// Default to user message - can have text and/or images
			// If images are present, use content array format
			if len(imageParts) > 0 {
				// Build content array with text and image parts
				contentPartsArray := make([]openai.ChatCompletionContentPartUnionParam, 0)

				// Add text parts
				for _, text := range contentParts {
					if text != "" {
						contentPartsArray = append(contentPartsArray, openai.TextContentPart(text))
					}
				}

				// Add image parts
				for _, img := range imageParts {
					imagePart := createImageContentPart(img)
					if imagePart != nil {
						contentPartsArray = append(contentPartsArray, *imagePart)
					}
				}

				// Only add message if there's content
				if len(contentPartsArray) > 0 {
					openaiMessages = append(openaiMessages, openai.UserMessage(contentPartsArray))
				}
			} else {
				// Text-only message (existing behavior)
				content := ""
				if len(contentParts) > 0 {
					content = contentParts[0]
					for i := 1; i < len(contentParts); i++ {
						content += "\n" + contentParts[i]
					}
				}
				openaiMessages = append(openaiMessages, openai.UserMessage(content))
			}
		}
	}

	return openaiMessages
}

// createImageContentPart creates an OpenAI image content part from ImageContent
func createImageContentPart(img llmtypes.ImageContent) *openai.ChatCompletionContentPartUnionParam {
	if img.SourceType == "base64" {
		// Format base64 as data URL: data:image/<type>;base64,<data>
		dataURL := fmt.Sprintf("data:%s;base64,%s", img.MediaType, img.Data)
		imageURLParam := openai.ChatCompletionContentPartImageImageURLParam{
			URL: dataURL,
		}
		imagePart := openai.ImageContentPart(imageURLParam)
		return &imagePart
	} else if img.SourceType == "url" {
		// Use URL directly
		imageURLParam := openai.ChatCompletionContentPartImageImageURLParam{
			URL: img.Data,
		}
		imagePart := openai.ImageContentPart(imageURLParam)
		return &imagePart
	}
	// Invalid source type
	return nil
}

// convertTools converts llmtypes tools to OpenAI tools format
func convertTools(llmTools []llmtypes.Tool) []openai.ChatCompletionToolUnionParam {
	openaiTools := make([]openai.ChatCompletionToolUnionParam, 0, len(llmTools))

	for _, tool := range llmTools {
		if tool.Function == nil {
			continue
		}

		// Extract function parameters as JSON schema
		var parameters shared.FunctionParameters
		if tool.Function.Parameters != nil {
			// Convert from typed Parameters to map for langchaingo compatibility
			paramsMap := make(map[string]interface{})
			if tool.Function.Parameters.Type != "" {
				paramsMap["type"] = tool.Function.Parameters.Type
			}
			// Only add properties if they exist and are not empty
			// OpenAI requires that if type is "object", properties must either be omitted or have at least one property
			// IMPORTANT: Check for nil first, then check length - len(nil map) returns 0 in Go
			if tool.Function.Parameters.Properties != nil && len(tool.Function.Parameters.Properties) > 0 {
				paramsMap["properties"] = tool.Function.Parameters.Properties
			}
			// Only add required if they exist and are not empty
			if tool.Function.Parameters.Required != nil && len(tool.Function.Parameters.Required) > 0 {
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

			// CRITICAL FIX: OpenAI API has conflicting requirements:
			// 1. If type is "object", properties field MUST be present
			// 2. But empty properties: {} is rejected
			// Solution: For empty schemas, provide a minimal valid schema with a dummy optional property
			// This satisfies OpenAI's requirement while being functionally equivalent to empty
			if paramsMap["type"] == "object" {
				if _, hasProperties := paramsMap["properties"]; !hasProperties {
					// Empty object schema - OpenAI requires properties to be present
					// Add a dummy optional property that will never be used
					// This is a workaround for OpenAI's API limitation
					paramsMap["properties"] = map[string]interface{}{
						"_": map[string]interface{}{
							"type":        "string",
							"description": "Unused parameter (required by OpenAI API for empty schemas)",
						},
					}
					// Don't add "_" to required array - it's optional
				}
			}

			parameters = shared.FunctionParameters(paramsMap)
		}

		// Create OpenAI function definition
		functionDef := shared.FunctionDefinitionParam{
			Name:        tool.Function.Name,
			Description: param.NewOpt(tool.Function.Description),
			Parameters:  parameters,
		}

		// Create OpenAI tool using helper function
		openaiTool := openai.ChatCompletionFunctionTool(functionDef)

		openaiTools = append(openaiTools, openaiTool)
	}

	return openaiTools
}

// convertToolChoice converts llmtypes tool choice to OpenAI tool choice format
func convertToolChoice(toolChoice interface{}) *openai.ChatCompletionToolChoiceOptionUnionParam {
	if toolChoice == nil {
		return nil
	}

	// Handle string-based tool choice
	if choiceStr, ok := toolChoice.(string); ok {
		switch choiceStr {
		case "auto":
			result := openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: param.NewOpt("auto"),
			}
			return &result
		case "none":
			result := openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: param.NewOpt("none"),
			}
			return &result
		case "required":
			result := openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: param.NewOpt("required"),
			}
			return &result
		default:
			// Default to auto
			result := openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: param.NewOpt("auto"),
			}
			return &result
		}
	}

	// Handle ToolChoice struct if it's that type
	if tc, ok := toolChoice.(*llmtypes.ToolChoice); ok && tc != nil {
		// For now, default to auto - could be enhanced to handle function-specific choices
		result := openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt("auto"),
		}
		return &result
	}

	// Handle map-based tool choice (from ConvertToolChoice)
	if choiceMap, ok := toolChoice.(map[string]interface{}); ok {
		if typ, ok := choiceMap["type"].(string); ok && typ == "function" {
			if fnMap, ok := choiceMap["function"].(map[string]interface{}); ok {
				if name, ok := fnMap["name"].(string); ok {
					// Function-specific tool choice
					result := openai.ToolChoiceOptionFunctionToolChoice(openai.ChatCompletionNamedToolChoiceFunctionParam{
						Name: name,
					})
					return &result
				}
			}
		}
	}

	// Default to auto
	result := openai.ChatCompletionToolChoiceOptionUnionParam{
		OfAuto: param.NewOpt("auto"),
	}
	return &result
}

// convertResponse converts OpenAI response to llmtypes ContentResponse
func convertResponse(result *openai.ChatCompletion) *llmtypes.ContentResponse {
	if result == nil {
		return &llmtypes.ContentResponse{
			Choices: []*llmtypes.ContentChoice{},
		}
	}

	choices := make([]*llmtypes.ContentChoice, 0, len(result.Choices))

	for _, choice := range result.Choices {
		langChoice := &llmtypes.ContentChoice{}

		// Extract text content
		// Content is a string in OpenAI SDK v3
		if choice.Message.Content != "" {
			langChoice.Content = choice.Message.Content
		}

		// Extract tool calls
		if len(choice.Message.ToolCalls) > 0 {
			toolCalls := make([]llmtypes.ToolCall, 0, len(choice.Message.ToolCalls))
			for _, tc := range choice.Message.ToolCalls {
				langToolCall := llmtypes.ToolCall{
					ID:   tc.ID,
					Type: string(tc.Type),
				}

				// Extract function call - ToolCalls contains Function field directly
				langToolCall.FunctionCall = &llmtypes.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: convertArgumentsToString(tc.Function.Arguments),
				}

				toolCalls = append(toolCalls, langToolCall)
			}
			langChoice.ToolCalls = toolCalls
		}

		// Extract finish reason / stop reason
		if choice.FinishReason != "" {
			langChoice.StopReason = choice.FinishReason
		}

		// Extract token usage if available
		// Usage is not a pointer in OpenAI SDK v3
		inputTokens := int(result.Usage.PromptTokens)
		outputTokens := int(result.Usage.CompletionTokens)
		totalTokens := int(result.Usage.TotalTokens)

		langChoice.GenerationInfo = &llmtypes.GenerationInfo{
			InputTokens:         &inputTokens,
			OutputTokens:        &outputTokens,
			TotalTokens:         &totalTokens,
			PromptTokens:        &inputTokens,
			CompletionTokens:    &outputTokens,
			PromptTokensCap:     &inputTokens,
			CompletionTokensCap: &outputTokens,
			TotalTokensCap:      &totalTokens,
		}

		// Handle reasoning tokens for o3 models (if available)
		// CompletionTokensDetails is not a pointer
		if result.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			reasoningTokens := int(result.Usage.CompletionTokensDetails.ReasoningTokens)
			langChoice.GenerationInfo.ReasoningTokens = &reasoningTokens
		}

		choices = append(choices, langChoice)
	}

	return &llmtypes.ContentResponse{
		Choices: choices,
	}
}

// Call implements a convenience method that wraps GenerateContent for simple text generation
func (o *OpenAIAdapter) Call(ctx context.Context, prompt string, options ...llmtypes.CallOption) (string, error) {
	messages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: prompt},
			},
		},
	}

	resp, err := o.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return resp.Choices[0].Content, nil
}

// convertArgumentsToString converts function arguments to JSON string
func convertArgumentsToString(args interface{}) string {
	if args == nil {
		return "{}"
	}

	// Handle string arguments
	if argsStr, ok := args.(string); ok {
		return argsStr
	}

	// Handle map arguments
	if argsMap, ok := args.(map[string]interface{}); ok {
		bytes, err := json.Marshal(argsMap)
		if err != nil {
			return "{}"
		}
		return string(bytes)
	}

	// Try to marshal any other type
	bytes, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}

	return string(bytes)
}

// logInputDetails logs the input parameters before making the API call
func (o *OpenAIAdapter) logInputDetails(modelID string, messages []llmtypes.MessageContent, params openai.ChatCompletionNewParams, opts *llmtypes.CallOptions) {
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
	if !param.IsOmitted(params.Temperature) {
		inputSummary["params_temperature"] = params.Temperature.Value
	}
	// Note: max_tokens is not set - using OpenAI model defaults
	if params.ResponseFormat.OfJSONObject != nil {
		inputSummary["params_response_format"] = "json_object"
	}
	if len(params.Tools) > 0 {
		inputSummary["params_tools_count"] = len(params.Tools)
	}
	if !param.IsOmitted(params.ToolChoice.OfAuto) {
		inputSummary["params_tool_choice"] = "set"
	}

	o.logger.Debugf("OpenAI GenerateContent INPUT - %+v", inputSummary)
}

// logErrorDetails logs both input and error response details when an error occurs
func (o *OpenAIAdapter) logErrorDetails(modelID string, messages []llmtypes.MessageContent, params openai.ChatCompletionNewParams, opts *llmtypes.CallOptions, err error, result *openai.ChatCompletion) {
	// Log error with input context
	errorInfo := map[string]interface{}{
		"error":         err.Error(),
		"error_type":    fmt.Sprintf("%T", err),
		"model_id":      modelID,
		"message_count": len(messages),
	}

	// Extract detailed error information if it's an API error
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		errorInfo["api_error_code"] = apiErr.Code
		errorInfo["api_error_type"] = apiErr.Type
		errorInfo["api_error_param"] = apiErr.Param
		errorInfo["api_error_message"] = apiErr.Message
		errorInfo["http_status_code"] = apiErr.StatusCode

		// Classify error type
		switch apiErr.StatusCode {
		case 401:
			errorInfo["error_classification"] = "unauthorized"
			o.logger.Warnf("🔄 401 Unauthorized error - Invalid API key or authentication failed")
		case 429:
			errorInfo["error_classification"] = "rate_limit"
			o.logger.Warnf("🔄 429 Rate Limit error detected, will trigger fallback mechanism")
		case 500:
			errorInfo["error_classification"] = "server_error"
			o.logger.Warnf("🔄 500 Internal Server Error detected, will trigger fallback mechanism")
		case 502:
			errorInfo["error_classification"] = "bad_gateway"
			o.logger.Warnf("🔄 502 Bad Gateway error detected, will trigger fallback mechanism")
		case 503:
			errorInfo["error_classification"] = "service_unavailable"
			o.logger.Warnf("🔄 503 Service Unavailable error detected, will trigger fallback mechanism")
		case 504:
			errorInfo["error_classification"] = "gateway_timeout"
			o.logger.Warnf("🔄 504 Gateway Timeout error detected, will trigger fallback mechanism")
		default:
			errorInfo["error_classification"] = "unknown"
		}
	} else {
		// Check error message for common patterns
		errMsg := err.Error()
		if strings.Contains(errMsg, "502") || strings.Contains(errMsg, "bad gateway") {
			errorInfo["error_classification"] = "bad_gateway"
			o.logger.Warnf("🔄 502 Bad Gateway error detected, will trigger fallback mechanism")
		} else if strings.Contains(errMsg, "503") || strings.Contains(errMsg, "service unavailable") {
			errorInfo["error_classification"] = "service_unavailable"
			o.logger.Warnf("🔄 503 Service Unavailable error detected, will trigger fallback mechanism")
		} else if strings.Contains(errMsg, "504") || strings.Contains(errMsg, "gateway timeout") {
			errorInfo["error_classification"] = "gateway_timeout"
			o.logger.Warnf("🔄 504 Gateway Timeout error detected, will trigger fallback mechanism")
		} else if strings.Contains(errMsg, "500") || strings.Contains(errMsg, "internal server error") {
			errorInfo["error_classification"] = "server_error"
			o.logger.Warnf("🔄 500 Internal Server Error detected, will trigger fallback mechanism")
		} else if strings.Contains(errMsg, "429") || strings.Contains(errMsg, "rate limit") {
			errorInfo["error_classification"] = "rate_limit"
			o.logger.Warnf("🔄 429 Rate Limit error detected, will trigger fallback mechanism")
		} else if strings.Contains(errMsg, "401") || strings.Contains(errMsg, "unauthorized") {
			errorInfo["error_classification"] = "unauthorized"
			o.logger.Warnf("🔄 401 Unauthorized error - Invalid API key or authentication failed")
		}
	}

	// Add params summary
	if !param.IsOmitted(params.Temperature) {
		errorInfo["temperature"] = params.Temperature.Value
	}
	// Note: max_tokens is not set - using OpenAI model defaults
	if params.ResponseFormat.OfJSONObject != nil {
		errorInfo["response_format"] = "json_object"
	}
	if len(params.Tools) > 0 {
		errorInfo["tools_count"] = len(params.Tools)
		// Log tool names for debugging
		toolNames := make([]string, 0, len(params.Tools))
		for _, tool := range params.Tools {
			if tool.OfFunction != nil && tool.OfFunction.Function.Name != "" {
				toolNames = append(toolNames, tool.OfFunction.Function.Name)
			}
		}
		if len(toolNames) > 0 {
			errorInfo["tool_names"] = toolNames
		}
	}

	// Add message details for debugging
	errorInfo["messages"] = make([]map[string]interface{}, 0, len(messages))
	for i, msg := range messages {
		msgInfo := map[string]interface{}{
			"role":  string(msg.Role),
			"parts": len(msg.Parts),
		}
		// Calculate content length
		contentLength := 0
		for _, part := range msg.Parts {
			if textPart, ok := part.(llmtypes.TextContent); ok {
				contentLength += len(textPart.Text)
			}
		}
		msgInfo["content_length"] = contentLength
		if i < 5 { // Limit to first 5 messages
			errorInfo["messages"] = append(errorInfo["messages"].([]map[string]interface{}), msgInfo)
		}
	}

	// Add response details if available (even though there was an error)
	if result != nil {
		responseInfo := map[string]interface{}{}
		if len(result.Choices) > 0 {
			choice := result.Choices[0]
			if choice.Message.Content != "" {
				content := choice.Message.Content
				if len(content) > 500 {
					content = content[:500] + "..."
				}
				responseInfo["content_preview"] = content
				responseInfo["content_length"] = len(choice.Message.Content)
			}
			if len(choice.Message.ToolCalls) > 0 {
				responseInfo["tool_calls_count"] = len(choice.Message.ToolCalls)
				toolCallNames := make([]string, 0, len(choice.Message.ToolCalls))
				for _, tc := range choice.Message.ToolCalls {
					if tc.Function.Name != "" {
						toolCallNames = append(toolCallNames, tc.Function.Name)
					}
				}
				if len(toolCallNames) > 0 {
					responseInfo["tool_call_names"] = toolCallNames
				}
			}
			responseInfo["finish_reason"] = choice.FinishReason
		}
		if len(responseInfo) > 0 {
			errorInfo["response"] = responseInfo
		}

		// Usage is not a pointer
		errorInfo["usage"] = map[string]interface{}{
			"prompt_tokens":     result.Usage.PromptTokens,
			"completion_tokens": result.Usage.CompletionTokens,
			"total_tokens":      result.Usage.TotalTokens,
		}

		// Add reasoning tokens if available (for o3 models)
		if result.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			errorInfo["reasoning_tokens"] = result.Usage.CompletionTokensDetails.ReasoningTokens
		}
	}

	// Log comprehensive error information
	o.logger.Errorf("OpenAI GenerateContent ERROR - %+v", errorInfo)

	// Log additional error details for debugging
	o.logger.Infof("❌ OpenAI LLM generation failed - model: %s, error: %v", modelID, err)
	o.logger.Infof("❌ Error details - type: %T, message: %s", err, err.Error())
	if apiErr != nil {
		o.logger.Infof("❌ API Error - Code: %s, Type: %s, Status: %d, Param: %s",
			apiErr.Code, apiErr.Type, apiErr.StatusCode, apiErr.Param)
	}

	// Log messages sent for debugging
	o.logger.Infof("📤 Messages sent to OpenAI LLM - count: %d", len(messages))
	for i, msg := range messages {
		// Calculate actual content length from message parts
		contentLength := 0
		for _, part := range msg.Parts {
			if textPart, ok := part.(llmtypes.TextContent); ok {
				contentLength += len(textPart.Text)
			}
		}
		o.logger.Infof("📤 Message %d - Role: %s, Content length: %d", i+1, msg.Role, contentLength)
		if i >= 4 { // Limit to first 5 messages
			break
		}
	}

	// Also log input details for full context
	o.logInputDetails(modelID, messages, params, opts)
}
