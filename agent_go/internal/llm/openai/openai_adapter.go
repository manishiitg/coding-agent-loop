package openai

import (
	"context"
	"encoding/json"
	"fmt"

	"mcp-agent/agent_go/internal/utils"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"

	"mcp-agent/agent_go/internal/llmtypes"
)

// OpenAIAdapter is an adapter that implements llmtypes.Model interface
// using the OpenAI Go SDK directly instead of langchaingo
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

	// Convert messages from langchaingo format to OpenAI format
	openaiMessages := convertMessages(messages)

	// Build ChatCompletionNewParams from options
	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(modelID),
		Messages: openaiMessages,
	}

	// Set temperature
	if opts.Temperature > 0 {
		params.Temperature = param.NewOpt(opts.Temperature)
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

	// Convert response from OpenAI format to langchaingo format
	return convertResponse(result), nil
}

// convertMessages converts langchaingo messages to OpenAI message format
func convertMessages(langMessages []llmtypes.MessageContent) []openai.ChatCompletionMessageParamUnion {
	openaiMessages := make([]openai.ChatCompletionMessageParamUnion, 0, len(langMessages))

	for _, msg := range langMessages {
		// Extract content parts
		var contentParts []string
		var toolCallID string
		var toolResponseContent map[string]interface{}

		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				contentParts = append(contentParts, p.Text)
			case llmtypes.ToolCallResponse:
				// Tool response - extract tool call ID and content
				toolCallID = p.ToolCallID
				toolResponseContent = parseJSONObject(p.Content)
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
			// User message can have text content
			content := ""
			if len(contentParts) > 0 {
				content = contentParts[0]
				// If multiple parts, join them
				for i := 1; i < len(contentParts); i++ {
					content += "\n" + contentParts[i]
				}
			}
			openaiMessages = append(openaiMessages, openai.UserMessage(content))
		case string(llmtypes.ChatMessageTypeAI):
			// Assistant message can have text content or tool calls
			content := ""
			if len(contentParts) > 0 {
				content = contentParts[0]
				for i := 1; i < len(contentParts); i++ {
					content += "\n" + contentParts[i]
				}
			}
			openaiMessages = append(openaiMessages, openai.AssistantMessage(content))
		case string(llmtypes.ChatMessageTypeTool):
			// Tool message - handle tool responses
			if toolCallID != "" {
				content := convertToolResponseContent(toolResponseContent)
				openaiMessages = append(openaiMessages, openai.ToolMessage(content, toolCallID))
			}
		default:
			// Default to user message
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

	return openaiMessages
}

// convertTools converts langchaingo tools to OpenAI tools format
func convertTools(llmTools []llmtypes.Tool) []openai.ChatCompletionToolUnionParam {
	openaiTools := make([]openai.ChatCompletionToolUnionParam, 0, len(llmTools))

	for _, tool := range llmTools {
		if tool.Function == nil {
			continue
		}

		// Extract function parameters as JSON schema
		var parameters shared.FunctionParameters
		if params, ok := tool.Function.Parameters.(map[string]interface{}); ok {
			parameters = shared.FunctionParameters(params)
		} else if tool.Function.Parameters != nil {
			// Try to marshal and unmarshal if not a map
			paramsBytes, err := json.Marshal(tool.Function.Parameters)
			if err == nil {
				var paramsMap map[string]interface{}
				if err := json.Unmarshal(paramsBytes, &paramsMap); err == nil {
					parameters = paramsMap
				}
			}
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

// convertToolChoice converts langchaingo tool choice to OpenAI tool choice format
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

// convertResponse converts OpenAI response to langchaingo ContentResponse
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
		langChoice.GenerationInfo = make(map[string]interface{})

		// Standardized field names
		langChoice.GenerationInfo["input_tokens"] = int(result.Usage.PromptTokens)
		langChoice.GenerationInfo["output_tokens"] = int(result.Usage.CompletionTokens)
		langChoice.GenerationInfo["total_tokens"] = int(result.Usage.TotalTokens)

		// OpenAI-specific field names for compatibility
		langChoice.GenerationInfo["PromptTokens"] = int(result.Usage.PromptTokens)
		langChoice.GenerationInfo["CompletionTokens"] = int(result.Usage.CompletionTokens)
		langChoice.GenerationInfo["TotalTokens"] = int(result.Usage.TotalTokens)

		// Handle reasoning tokens for o3 models (if available)
		// CompletionTokensDetails is not a pointer
		if result.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			langChoice.GenerationInfo["ReasoningTokens"] = int(result.Usage.CompletionTokensDetails.ReasoningTokens)
		}

		choices = append(choices, langChoice)
	}

	return &llmtypes.ContentResponse{
		Choices: choices,
	}
}

// Call implements the llmtypes.Model interface
// This is a convenience method that wraps GenerateContent for simple text generation
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

// parseJSONObject parses a JSON string into a map
func parseJSONObject(jsonStr string) map[string]interface{} {
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return make(map[string]interface{})
	}
	return result
}

// convertToolResponseContent converts tool response content to appropriate format
func convertToolResponseContent(content map[string]interface{}) string {
	if content == nil {
		return ""
	}

	bytes, err := json.Marshal(content)
	if err != nil {
		return ""
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
		"model_id":      modelID,
		"message_count": len(messages),
	}

	// Add params summary
	if params.ResponseFormat.OfJSONObject != nil {
		errorInfo["response_format"] = "json_object"
	}
	if len(params.Tools) > 0 {
		errorInfo["tools_count"] = len(params.Tools)
	}

	// Add response details if available (even though there was an error)
	if result != nil {
		if len(result.Choices) > 0 {
			choice := result.Choices[0]
			if choice.Message.Content != "" {
				content := choice.Message.Content
				if len(content) > 500 {
					content = content[:500] + "..."
				}
				errorInfo["response_preview"] = content
			}
		}
		// Usage is not a pointer
		errorInfo["usage"] = map[string]interface{}{
			"prompt_tokens":     result.Usage.PromptTokens,
			"completion_tokens": result.Usage.CompletionTokens,
			"total_tokens":      result.Usage.TotalTokens,
		}
	}

	// Log full input details
	o.logger.Errorf("OpenAI GenerateContent ERROR - %+v", errorInfo)

	// Also log input details for full context
	o.logInputDetails(modelID, messages, params, opts)
}
