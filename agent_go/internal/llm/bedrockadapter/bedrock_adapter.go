package bedrockadapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mcp-agent/agent_go/internal/utils"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"

	"mcp-agent/agent_go/internal/llmtypes"
)

// BedrockAdapter is an adapter that implements llmtypes.Model interface
// using the AWS Bedrock SDK directly
type BedrockAdapter struct {
	client  *bedrockruntime.Client
	modelID string
	logger  utils.ExtendedLogger
}

// NewBedrockAdapter creates a new adapter instance
func NewBedrockAdapter(client *bedrockruntime.Client, modelID string, logger utils.ExtendedLogger) *BedrockAdapter {
	return &BedrockAdapter{
		client:  client,
		modelID: modelID,
		logger:  logger,
	}
}

// GenerateContent implements the llmtypes.Model interface
func (b *BedrockAdapter) GenerateContent(ctx context.Context, messages []llmtypes.MessageContent, options ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	// Parse call options
	opts := &llmtypes.CallOptions{}
	for _, opt := range options {
		opt(opts)
	}

	// Determine model ID (from option or default)
	modelID := b.modelID
	if opts.Model != "" {
		modelID = opts.Model
	}

	// Convert messages from llmtypes format to Claude format
	claudeMessages := convertMessages(messages)

	// Build request body for InvokeModel
	requestBody := map[string]interface{}{
		"anthropic_version": "bedrock-2023-05-31",
		"messages":          claudeMessages,
	}

	// Set temperature
	if opts.Temperature > 0 {
		requestBody["temperature"] = opts.Temperature
	}

	// Set max tokens (default to 4096 if not specified)
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}
	requestBody["max_tokens"] = maxTokens

	// Handle JSON mode if specified
	// Claude 3.5+ supports structured output via response schema
	// For earlier versions, we add JSON mode instruction to the first system/user message
	if opts.JSONMode {
		// Claude 3.5+ uses response_schema for structured output
		// For now, we'll prepend JSON instruction to the first message
		// This ensures the model returns JSON format
		if len(claudeMessages) > 0 {
			firstMsg := claudeMessages[0]
			if firstMsgContent, ok := firstMsg["content"].([]map[string]interface{}); ok && len(firstMsgContent) > 0 {
				// Prepend JSON instruction as a text block
				jsonInstruction := map[string]interface{}{
					"type": "text",
					"text": "You must respond with valid JSON only, no other text. Return a JSON object.",
				}
				firstMsg["content"] = append([]map[string]interface{}{jsonInstruction}, firstMsgContent...)
			}
		}
	}

	// Convert tools if provided
	if len(opts.Tools) > 0 {
		tools := convertTools(opts.Tools)
		requestBody["tools"] = tools

		// Handle tool choice
		toolChoice := convertToolChoice(opts.ToolChoice)
		if toolChoice != nil {
			requestBody["tool_choice"] = toolChoice
		}
	}

	// Log input details if logger is available (for debugging errors)
	if b.logger != nil {
		b.logInputDetails(modelID, messages, requestBody, opts)
	}

	// Marshal request body to JSON
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal bedrock request: %w", err)
	}

	// Call AWS Bedrock InvokeModel API
	result, err := b.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		ContentType: aws.String("application/json"),
		Body:        bodyBytes,
	})

	if err != nil {
		// Log error with input and response details
		if b.logger != nil {
			b.logErrorDetails(modelID, messages, requestBody, opts, err, result)
		}
		return nil, fmt.Errorf("bedrock invoke model: %w", err)
	}

	// Parse response body JSON
	var responseBody map[string]interface{}
	if err := json.Unmarshal(result.Body, &responseBody); err != nil {
		return nil, fmt.Errorf("unmarshal bedrock response: %w", err)
	}

	// Convert response from Claude format to llmtypes format
	return convertResponse(responseBody), nil
}

// Call implements a convenience method for simple text generation
func (b *BedrockAdapter) Call(ctx context.Context, prompt string, options ...llmtypes.CallOption) (string, error) {
	messages := []llmtypes.MessageContent{
		llmtypes.TextParts(llmtypes.ChatMessageTypeHuman, prompt),
	}

	resp, err := b.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	// Extract text content from first choice
	// Content is a string in llmtypes
	return resp.Choices[0].Content, nil
}

// convertMessages converts llmtypes messages to Claude/Anthropic format
// Claude uses an array of content blocks instead of a single content string
func convertMessages(langMessages []llmtypes.MessageContent) []map[string]interface{} {
	claudeMessages := make([]map[string]interface{}, 0, len(langMessages))

	for _, msg := range langMessages {
		// Extract content parts
		var contentBlocks []map[string]interface{}
		var toolCallID string
		var toolResponseContent string
		var toolCalls []llmtypes.ToolCall

		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				// Add text content block
				contentBlocks = append(contentBlocks, map[string]interface{}{
					"type": "text",
					"text": p.Text,
				})
			case llmtypes.ToolCallResponse:
				// Tool response - extract tool call ID and content
				toolCallID = p.ToolCallID
				toolResponseContent = p.Content
			case llmtypes.ToolCall:
				// Tool call in assistant message
				toolCalls = append(toolCalls, p)
			}
		}

		// Create appropriate message type based on role
		switch string(msg.Role) {
		case string(llmtypes.ChatMessageTypeSystem):
			// Claude doesn't have a system role, so we convert it to a user message
			// In practice, system messages are often prepended to the first user message
			// For now, we'll convert it to a user message with the system content
			if len(contentBlocks) > 0 {
				claudeMessages = append(claudeMessages, map[string]interface{}{
					"role":    "user",
					"content": contentBlocks,
				})
			}
		case string(llmtypes.ChatMessageTypeHuman):
			// User message
			if len(contentBlocks) > 0 {
				claudeMessages = append(claudeMessages, map[string]interface{}{
					"role":    "user",
					"content": contentBlocks,
				})
			}
		case string(llmtypes.ChatMessageTypeAI):
			// Assistant message can have text content or tool calls
			if len(toolCalls) > 0 {
				// Convert tool calls to Claude format
				toolUseBlocks := make([]map[string]interface{}, 0, len(toolCalls))
				for _, tc := range toolCalls {
					// Parse arguments JSON string
					var input map[string]interface{}
					if tc.FunctionCall.Arguments != "" {
						if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &input); err != nil {
							// If parsing fails, use empty object
							input = make(map[string]interface{})
						}
					} else {
						input = make(map[string]interface{})
					}

					toolUseBlocks = append(toolUseBlocks, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.FunctionCall.Name,
						"input": input,
					})
				}

				// Combine text blocks and tool use blocks
				allBlocks := append(contentBlocks, toolUseBlocks...)
				if len(allBlocks) > 0 {
					claudeMessages = append(claudeMessages, map[string]interface{}{
						"role":    "assistant",
						"content": allBlocks,
					})
				}
			} else if len(contentBlocks) > 0 {
				// Assistant message with just text
				claudeMessages = append(claudeMessages, map[string]interface{}{
					"role":    "assistant",
					"content": contentBlocks,
				})
			}
		case string(llmtypes.ChatMessageTypeTool):
			// Tool message - handle tool responses
			// In Claude format, tool results are sent as user messages with tool_result content blocks
			if toolCallID != "" {
				// Use raw content directly (can be JSON string or plain text)
				claudeMessages = append(claudeMessages, map[string]interface{}{
					"role": "user",
					"content": []map[string]interface{}{
						{
							"type":        "tool_result",
							"tool_use_id": toolCallID,
							"content":     toolResponseContent,
						},
					},
				})
			}
		default:
			// Default to user message
			if len(contentBlocks) > 0 {
				claudeMessages = append(claudeMessages, map[string]interface{}{
					"role":    "user",
					"content": contentBlocks,
				})
			}
		}
	}

	return claudeMessages
}

// convertTools converts llmtypes tools to Claude tool format
func convertTools(llmTools []llmtypes.Tool) []map[string]interface{} {
	claudeTools := make([]map[string]interface{}, 0, len(llmTools))

	for _, tool := range llmTools {
		if tool.Function == nil {
			continue
		}

		// Extract function parameters as JSON schema
		var inputSchema map[string]interface{}
		if params, ok := tool.Function.Parameters.(map[string]interface{}); ok {
			inputSchema = params
		} else if tool.Function.Parameters != nil {
			// Try to marshal and unmarshal if not a map
			paramsBytes, err := json.Marshal(tool.Function.Parameters)
			if err == nil {
				var paramsMap map[string]interface{}
				if err := json.Unmarshal(paramsBytes, &paramsMap); err == nil {
					inputSchema = paramsMap
				}
			}
		}

		// Ensure input_schema has required fields
		if inputSchema == nil {
			inputSchema = map[string]interface{}{
				"type":       "object",
				"properties": make(map[string]interface{}),
			}
		}

		// Create Claude tool definition
		claudeTool := map[string]interface{}{
			"name":         tool.Function.Name,
			"description":  tool.Function.Description,
			"input_schema": inputSchema,
		}

		claudeTools = append(claudeTools, claudeTool)
	}

	return claudeTools
}

// convertToolChoice converts llmtypes tool choice to Claude tool choice format
func convertToolChoice(toolChoice interface{}) map[string]interface{} {
	if toolChoice == nil {
		return nil
	}

	// Handle string-based tool choice
	if choiceStr, ok := toolChoice.(string); ok {
		switch choiceStr {
		case "auto", "":
			return map[string]interface{}{
				"type": "auto",
			}
		case "required", "any":
			return map[string]interface{}{
				"type": "any",
			}
		case "none":
			return map[string]interface{}{
				"type": "none",
			}
		default:
			// Specific tool name
			return map[string]interface{}{
				"type": "tool",
				"name": choiceStr,
			}
		}
	}

	// Handle ToolChoice struct if it's that type
	if tc, ok := toolChoice.(*llmtypes.ToolChoice); ok && tc != nil {
		// Check the type field
		if tc.Type == "required" {
			return map[string]interface{}{
				"type": "any",
			}
		} else if tc.Type == "none" {
			return map[string]interface{}{
				"type": "none",
			}
		} else if tc.Type == "function" && tc.Function != nil && tc.Function.Name != "" {
			return map[string]interface{}{
				"type": "tool",
				"name": tc.Function.Name,
			}
		}
	}

	// Default to auto
	return map[string]interface{}{
		"type": "auto",
	}
}

// convertResponse converts Claude/Bedrock response to llmtypes.ContentResponse format
func convertResponse(responseBody map[string]interface{}) *llmtypes.ContentResponse {
	resp := &llmtypes.ContentResponse{
		Choices: []*llmtypes.ContentChoice{},
	}

	// Extract content from response
	var contentText strings.Builder
	var toolCalls []llmtypes.ToolCall

	// Claude response has a "content" array
	if contentArray, ok := responseBody["content"].([]interface{}); ok {
		for _, block := range contentArray {
			if blockMap, ok := block.(map[string]interface{}); ok {
				blockType, _ := blockMap["type"].(string)
				switch blockType {
				case "text":
					if text, ok := blockMap["text"].(string); ok {
						if contentText.Len() > 0 {
							contentText.WriteString("\n")
						}
						contentText.WriteString(text)
					}
				case "tool_use":
					// Extract tool call information
					id, _ := blockMap["id"].(string)
					name, _ := blockMap["name"].(string)
					input, _ := blockMap["input"].(map[string]interface{})

					// Convert input to JSON string
					inputJSON := "{}"
					if input != nil {
						inputBytes, err := json.Marshal(input)
						if err == nil {
							inputJSON = string(inputBytes)
						}
					}

					toolCalls = append(toolCalls, llmtypes.ToolCall{
						ID: id,
						FunctionCall: &llmtypes.FunctionCall{
							Name:      name,
							Arguments: inputJSON,
						},
					})
				}
			}
		}
	}

	// Extract stop reason
	stopReason := ""
	if stop, ok := responseBody["stop_reason"].(string); ok {
		stopReason = stop
	}

	// Create choice
	choice := &llmtypes.ContentChoice{
		Content:        contentText.String(),
		StopReason:     stopReason,
		ToolCalls:      toolCalls,
		GenerationInfo: make(map[string]interface{}),
	}

	// Extract token usage
	if usage, ok := responseBody["usage"].(map[string]interface{}); ok {
		if inputTokens, ok := usage["input_tokens"].(float64); ok {
			choice.GenerationInfo["prompt_tokens"] = int(inputTokens)
		}
		if outputTokens, ok := usage["output_tokens"].(float64); ok {
			choice.GenerationInfo["completion_tokens"] = int(outputTokens)
		}
		// Calculate total
		var totalTokens int
		if promptTokens, ok := choice.GenerationInfo["prompt_tokens"].(int); ok {
			totalTokens += promptTokens
		}
		if completionTokens, ok := choice.GenerationInfo["completion_tokens"].(int); ok {
			totalTokens += completionTokens
		}
		if totalTokens > 0 {
			choice.GenerationInfo["total_tokens"] = totalTokens
		}
	}

	resp.Choices = append(resp.Choices, choice)

	return resp
}

// logInputDetails logs the input parameters before making the API call
func (b *BedrockAdapter) logInputDetails(modelID string, messages []llmtypes.MessageContent, requestBody map[string]interface{}, opts *llmtypes.CallOptions) {
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

	// Add request body details
	if temp, ok := requestBody["temperature"].(float64); ok {
		inputSummary["request_temperature"] = temp
	}
	if maxTokens, ok := requestBody["max_tokens"].(int); ok {
		inputSummary["request_max_tokens"] = maxTokens
	}
	if tools, ok := requestBody["tools"].([]map[string]interface{}); ok {
		inputSummary["request_tools_count"] = len(tools)
	}

	b.logger.Debugf("Bedrock GenerateContent INPUT - %+v", inputSummary)
}

// logErrorDetails logs both input and error response details when an error occurs
func (b *BedrockAdapter) logErrorDetails(modelID string, messages []llmtypes.MessageContent, requestBody map[string]interface{}, opts *llmtypes.CallOptions, err error, result *bedrockruntime.InvokeModelOutput) {
	// Log error with input context
	errorInfo := map[string]interface{}{
		"error":                err.Error(),
		"error_type":           fmt.Sprintf("%T", err),
		"model_id":             modelID,
		"message_count":        len(messages),
		"error_classification": "unknown",
	}

	// Extract detailed error information if it's an AWS SDK error
	var awsErrCode, awsErrMessage, awsRequestID string
	var awsHTTPStatusCode int

	// Try to extract AWS error details using type assertions
	if errWithCode, ok := err.(interface{ Code() string }); ok {
		awsErrCode = errWithCode.Code()
		errorInfo["aws_error_code"] = awsErrCode
	}
	if errWithMsg, ok := err.(interface{ Message() string }); ok {
		awsErrMessage = errWithMsg.Message()
		errorInfo["aws_error_message"] = awsErrMessage
	}
	if errWithRequestID, ok := err.(interface{ RequestID() string }); ok {
		awsRequestID = errWithRequestID.RequestID()
		errorInfo["aws_request_id"] = awsRequestID
	}
	if errWithStatusCode, ok := err.(interface{ StatusCode() int }); ok {
		awsHTTPStatusCode = errWithStatusCode.StatusCode()
		errorInfo["http_status_code"] = awsHTTPStatusCode
	}

	// Classify error based on AWS error code and HTTP status
	errMsg := err.Error()
	classified := false

	if awsHTTPStatusCode > 0 {
		switch awsHTTPStatusCode {
		case 400:
			errorInfo["error_classification"] = "bad_request"
			b.logger.Warnf("🔄 400 Bad Request error - Check request parameters")
			classified = true
		case 401:
			errorInfo["error_classification"] = "unauthorized"
			b.logger.Warnf("🔄 401 Unauthorized error - Check AWS credentials and permissions")
			classified = true
		case 403:
			errorInfo["error_classification"] = "access_denied"
			b.logger.Warnf("🔄 403 Access Denied error - Check AWS credentials and permissions")
			classified = true
		case 429:
			errorInfo["error_classification"] = "rate_limit"
			b.logger.Warnf("🔄 429 Rate Limit/Throttling error detected, will trigger fallback mechanism")
			classified = true
		case 500:
			errorInfo["error_classification"] = "server_error"
			b.logger.Warnf("🔄 500 Internal Server Error detected, will trigger fallback mechanism")
			classified = true
		case 502:
			errorInfo["error_classification"] = "bad_gateway"
			b.logger.Warnf("🔄 502 Bad Gateway error detected, will trigger fallback mechanism")
			classified = true
		case 503:
			errorInfo["error_classification"] = "service_unavailable"
			b.logger.Warnf("🔄 503 Service Unavailable error detected, will trigger fallback mechanism")
			classified = true
		case 504:
			errorInfo["error_classification"] = "gateway_timeout"
			b.logger.Warnf("🔄 504 Gateway Timeout error detected, will trigger fallback mechanism")
			classified = true
		}
	}

	// Fallback to error code classification if HTTP status wasn't available
	if !classified {
		if awsErrCode != "" {
			switch awsErrCode {
			case "AccessDeniedException", "AccessDenied":
				errorInfo["error_classification"] = "access_denied"
				b.logger.Warnf("🔄 Access Denied error - Check AWS credentials and permissions")
				classified = true
			case "ValidationException", "InvalidParameterException":
				errorInfo["error_classification"] = "validation_error"
				b.logger.Warnf("🔄 Validation error - Check request parameters")
				classified = true
			case "ThrottlingException", "TooManyRequestsException":
				errorInfo["error_classification"] = "rate_limit"
				b.logger.Warnf("🔄 Rate Limit/Throttling error detected, will trigger fallback mechanism")
				classified = true
			case "ModelNotReadyException", "ModelStreamErrorException":
				errorInfo["error_classification"] = "model_error"
				b.logger.Warnf("🔄 Model Error - Model may not be ready or encountered an error")
				classified = true
			case "InternalServerException":
				errorInfo["error_classification"] = "server_error"
				b.logger.Warnf("🔄 Internal Server Error detected, will trigger fallback mechanism")
				classified = true
			case "ServiceQuotaExceededException":
				errorInfo["error_classification"] = "quota_exceeded"
				b.logger.Warnf("🔄 Service Quota Exceeded - Check AWS service limits")
				classified = true
			}
		}

		// Final fallback to message-based classification
		if !classified {
			if strings.Contains(errMsg, "AccessDenied") || strings.Contains(errMsg, "access denied") || strings.Contains(errMsg, "403") {
				errorInfo["error_classification"] = "access_denied"
				b.logger.Warnf("🔄 Access Denied error - Check AWS credentials and permissions")
			} else if strings.Contains(errMsg, "ValidationException") || strings.Contains(errMsg, "validation") || strings.Contains(errMsg, "400") {
				errorInfo["error_classification"] = "validation_error"
				b.logger.Warnf("🔄 Validation error - Check request parameters")
			} else if strings.Contains(errMsg, "ThrottlingException") || strings.Contains(errMsg, "throttl") || strings.Contains(errMsg, "429") {
				errorInfo["error_classification"] = "rate_limit"
				b.logger.Warnf("🔄 Rate Limit/Throttling error detected, will trigger fallback mechanism")
			} else if strings.Contains(errMsg, "500") || strings.Contains(errMsg, "internal server error") {
				errorInfo["error_classification"] = "server_error"
				b.logger.Warnf("🔄 Internal Server Error detected, will trigger fallback mechanism")
			} else if strings.Contains(errMsg, "503") || strings.Contains(errMsg, "service unavailable") {
				errorInfo["error_classification"] = "service_unavailable"
				b.logger.Warnf("🔄 Service Unavailable error detected, will trigger fallback mechanism")
			} else if strings.Contains(errMsg, "502") || strings.Contains(errMsg, "bad gateway") {
				errorInfo["error_classification"] = "bad_gateway"
				b.logger.Warnf("🔄 Bad Gateway error detected, will trigger fallback mechanism")
			} else if strings.Contains(errMsg, "504") || strings.Contains(errMsg, "gateway timeout") {
				errorInfo["error_classification"] = "gateway_timeout"
				b.logger.Warnf("🔄 Gateway Timeout error detected, will trigger fallback mechanism")
			}
		}
	}

	// Add request body summary
	if temp, ok := requestBody["temperature"].(float64); ok {
		errorInfo["temperature"] = temp
	}
	if maxTokens, ok := requestBody["max_tokens"].(int); ok {
		errorInfo["max_tokens"] = maxTokens
	}
	if tools, ok := requestBody["tools"].([]map[string]interface{}); ok {
		errorInfo["tools_count"] = len(tools)
		// Log tool names for debugging
		toolNames := make([]string, 0, len(tools))
		for _, tool := range tools {
			if name, ok := tool["name"].(string); ok {
				toolNames = append(toolNames, name)
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
	if result != nil && result.Body != nil {
		responseInfo := map[string]interface{}{}
		var responseBody map[string]interface{}
		if err := json.Unmarshal(result.Body, &responseBody); err == nil {
			if contentArray, ok := responseBody["content"].([]interface{}); ok && len(contentArray) > 0 {
				if firstBlock, ok := contentArray[0].(map[string]interface{}); ok {
					if text, ok := firstBlock["text"].(string); ok {
						content := text
						if len(content) > 500 {
							content = content[:500] + "..."
						}
						responseInfo["content_preview"] = content
						responseInfo["content_length"] = len(text)
					}
				}
			}
			if stopReason, ok := responseBody["stop_reason"].(string); ok {
				responseInfo["stop_reason"] = stopReason
			}
			// Extract usage metadata if available (Claude format)
			if usage, ok := responseBody["usage"].(map[string]interface{}); ok {
				usageInfo := map[string]interface{}{}
				if inputTokens, ok := usage["input_tokens"].(float64); ok {
					usageInfo["input_tokens"] = int(inputTokens)
					errorInfo["usage_input_tokens"] = int(inputTokens)
				}
				if outputTokens, ok := usage["output_tokens"].(float64); ok {
					usageInfo["output_tokens"] = int(outputTokens)
					errorInfo["usage_output_tokens"] = int(outputTokens)
				}
				if totalTokens, ok := usage["cache_creation_input_tokens"].(float64); ok {
					usageInfo["cache_creation_input_tokens"] = int(totalTokens)
				}
				if cacheReadTokens, ok := usage["cache_read_input_tokens"].(float64); ok {
					usageInfo["cache_read_input_tokens"] = int(cacheReadTokens)
				}
				if len(usageInfo) > 0 {
					responseInfo["usage"] = usageInfo
				}
			}
		}
		if len(responseInfo) > 0 {
			errorInfo["response"] = responseInfo
		}
	}

	// Log comprehensive error information
	b.logger.Errorf("Bedrock GenerateContent ERROR - %+v", errorInfo)

	// Log additional error details for debugging
	b.logger.Infof("❌ Bedrock LLM generation failed - model: %s, error: %v", modelID, err)
	b.logger.Infof("❌ Error details - type: %T, message: %s", err, err.Error())

	// Log AWS-specific error details if available
	if awsErrCode != "" {
		b.logger.Infof("❌ AWS Error Code: %s", awsErrCode)
	}
	if awsErrMessage != "" {
		b.logger.Infof("❌ AWS Error Message: %s", awsErrMessage)
	}
	if awsRequestID != "" {
		b.logger.Infof("❌ AWS Request ID: %s", awsRequestID)
	}
	if awsHTTPStatusCode > 0 {
		b.logger.Infof("❌ HTTP Status Code: %d", awsHTTPStatusCode)
	}

	// Log request parameters
	b.logger.Infof("📝 Request Parameters:")
	if temp, ok := requestBody["temperature"].(float64); ok {
		b.logger.Infof("   Temperature: %v", temp)
	}
	if maxTokens, ok := requestBody["max_tokens"].(int); ok {
		b.logger.Infof("   Max Tokens: %d", maxTokens)
	}
	if tools, ok := requestBody["tools"].([]map[string]interface{}); ok {
		b.logger.Infof("   Tools Count: %d", len(tools))
	}
	if jsonMode, ok := requestBody["anthropic_version"].(string); ok {
		if jsonMode != "" {
			b.logger.Infof("   Anthropic Version: %s", jsonMode)
		}
	}

	// Log messages sent for debugging
	b.logger.Infof("📤 Messages sent to Bedrock LLM - count: %d", len(messages))
	for i, msg := range messages {
		// Calculate actual content length from message parts
		contentLength := 0
		for _, part := range msg.Parts {
			if textPart, ok := part.(llmtypes.TextContent); ok {
				contentLength += len(textPart.Text)
			}
		}
		b.logger.Infof("📤 Message %d - Role: %s, Content length: %d", i+1, msg.Role, contentLength)
		if i >= 4 { // Limit to first 5 messages
			break
		}
	}

	// Also log input details for full context
	b.logInputDetails(modelID, messages, requestBody, opts)
}
