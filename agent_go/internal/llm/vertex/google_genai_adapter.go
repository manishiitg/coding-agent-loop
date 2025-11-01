package vertex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	for _, msg := range messages {
		genaiParts := make([]*genai.Part, 0)

		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				genaiParts = append(genaiParts, genai.NewPartFromText(p.Text))
			case llmtypes.ToolCallResponse:
				// Convert tool response to function response format
				// Try to parse as JSON first, but if it fails (returns empty map),
				// wrap the content as a string value to preserve the actual result
				responseMap := parseJSONObject(p.Content)
				// If parsing failed (empty map) and content exists and doesn't look like JSON, wrap it
				if len(responseMap) == 0 && p.Content != "" && !strings.HasPrefix(strings.TrimSpace(p.Content), "{") {
					// Wrap non-JSON string content in a map
					responseMap = map[string]interface{}{
						"result": p.Content,
					}
				}
				genaiParts = append(genaiParts, genai.NewPartFromFunctionResponse(p.ToolCallID, responseMap))
			}
		}

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
		genaiTools := convertTools(opts.Tools)
		config.Tools = genaiTools

		// Handle tool choice
		if opts.ToolChoice != nil {
			toolConfig := convertToolChoice(opts.ToolChoice)
			if toolConfig != nil {
				config.ToolConfig = toolConfig
			}
		}
	}

	// Log input details if logger is available (for debugging errors)
	if g.logger != nil {
		g.logInputDetails(modelID, messages, config, opts)
	}

	// Call Google GenAI API
	result, err := g.client.Models.GenerateContent(ctx, modelID, genaiContents, config)

	// 🆕 LOG RAW RESPONSE - Always log raw response for debugging (both success and error cases)
	if g.logger != nil {
		g.logRawResponse(modelID, result, err)
	}

	if err != nil {
		// Log error with input and response details
		if g.logger != nil {
			g.logErrorDetails(modelID, messages, config, opts, err, result)
		}
		return nil, fmt.Errorf("genai generate content: %w", err)
	}

	// Convert response from genai format to llmtypes format
	convertedResp := convertResponse(result, g.logger)
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
func convertTools(llmTools []llmtypes.Tool) []*genai.Tool {
	genaiTools := make([]*genai.Tool, 0, len(llmTools))
	for _, tool := range llmTools {
		if tool.Function == nil {
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
		if params, ok := tool.Function.Parameters.(map[string]interface{}); ok {
			schema := convertJSONSchemaToSchema(params)
			if schema != nil {
				functionDef.Parameters = schema
			}
		} else if tool.Function.Parameters != nil {
			// Try to marshal and unmarshal if not a map
			paramsBytes, err := json.Marshal(tool.Function.Parameters)
			if err == nil {
				var paramsMap map[string]interface{}
				if err := json.Unmarshal(paramsBytes, &paramsMap); err == nil {
					schema := convertJSONSchemaToSchema(paramsMap)
					if schema != nil {
						functionDef.Parameters = schema
					}
				}
			}
		}

		genaiTools = append(genaiTools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{functionDef},
		})
	}

	return genaiTools
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
func convertResponse(result *genai.GenerateContentResponse, logger utils.ExtendedLogger) *llmtypes.ContentResponse {
	if result == nil {
		return &llmtypes.ContentResponse{
			Choices: []*llmtypes.ContentChoice{},
		}
	}

	choices := make([]*llmtypes.ContentChoice, 0, len(result.Candidates))

	for i, candidate := range result.Candidates {
		choice := &llmtypes.ContentChoice{}

		// Extract text content and tool calls from parts
		var textParts []string
		var toolCalls []llmtypes.ToolCall

		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				if part.Text != "" {
					textParts = append(textParts, part.Text)
				}

				if part.FunctionCall != nil {
					toolCall := llmtypes.ToolCall{
						ID:   generateToolCallID(),
						Type: "function",
						FunctionCall: &llmtypes.FunctionCall{
							Name:      part.FunctionCall.Name,
							Arguments: convertArgumentsToString(part.FunctionCall.Args),
						},
					}
					toolCalls = append(toolCalls, toolCall)
				}
			}
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
		} else if result.Text() != "" {
			// Fallback to using result.Text() helper
			choice.Content = result.Text()
		}

		// 🆕 LOG EMPTY CONTENT WARNING - Detailed logging when content is empty
		if choice.Content == "" && logger != nil {
			logger.Errorf("🔍 [VERTEX] Candidate %d has EMPTY CONTENT - Debugging info:", i)
			logger.Errorf("   Candidate.Content: %v (nil: %v)", candidate.Content != nil, candidate.Content == nil)
			if candidate.Content != nil {
				logger.Errorf("   Candidate.Content.Parts count: %d", len(candidate.Content.Parts))
				for j, part := range candidate.Content.Parts {
					logger.Errorf("     Part %d - Text: %q, Text length: %d, FunctionCall: %v",
						j, part.Text, len(part.Text), part.FunctionCall != nil)
				}
			}
			logger.Errorf("   Candidate.FinishReason: %q", candidate.FinishReason)
			logger.Errorf("   result.Text() fallback: %q (length: %d)", result.Text(), len(result.Text()))
			logger.Errorf("   TextParts extracted: %d", len(textParts))
			logger.Errorf("   ToolCalls extracted: %d", len(toolCalls))
		}

		// Set tool calls if any
		if len(toolCalls) > 0 {
			choice.ToolCalls = toolCalls
		} else {
			// Also check result.FunctionCalls() helper
			if funcCalls := result.FunctionCalls(); len(funcCalls) > 0 {
				toolCalls = make([]llmtypes.ToolCall, 0, len(funcCalls))
				for _, fc := range funcCalls {
					toolCalls = append(toolCalls, llmtypes.ToolCall{
						ID:   generateToolCallID(),
						Type: "function",
						FunctionCall: &llmtypes.FunctionCall{
							Name:      fc.Name,
							Arguments: convertArgumentsToString(fc.Args),
						},
					})
				}
				choice.ToolCalls = toolCalls
			}
		}

		// Extract token usage if available
		if result.UsageMetadata != nil {
			choice.GenerationInfo = make(map[string]interface{})

			// Basic token counts
			choice.GenerationInfo["input_tokens"] = int(result.UsageMetadata.PromptTokenCount)
			choice.GenerationInfo["output_tokens"] = int(result.UsageMetadata.CandidatesTokenCount)

			// Use TotalTokenCount if available, otherwise calculate
			if result.UsageMetadata.TotalTokenCount > 0 {
				choice.GenerationInfo["total_tokens"] = int(result.UsageMetadata.TotalTokenCount)
			} else {
				totalTokens := result.UsageMetadata.PromptTokenCount + result.UsageMetadata.CandidatesTokenCount
				choice.GenerationInfo["total_tokens"] = int(totalTokens)
			}

			// Cache token information
			if result.UsageMetadata.CachedContentTokenCount > 0 {
				choice.GenerationInfo["cached_content_tokens"] = int(result.UsageMetadata.CachedContentTokenCount)

				// Calculate cache discount percentage (0.0 to 1.0)
				if result.UsageMetadata.PromptTokenCount > 0 {
					cacheDiscount := float64(result.UsageMetadata.CachedContentTokenCount) / float64(result.UsageMetadata.PromptTokenCount)
					choice.GenerationInfo["cache_discount"] = cacheDiscount
				}
			}

			// Additional token counts if available
			if result.UsageMetadata.ToolUsePromptTokenCount > 0 {
				choice.GenerationInfo["tool_use_prompt_tokens"] = int(result.UsageMetadata.ToolUsePromptTokenCount)
			}

			if result.UsageMetadata.ThoughtsTokenCount > 0 {
				choice.GenerationInfo["thoughts_tokens"] = int(result.UsageMetadata.ThoughtsTokenCount)
			}
		}

		// Set stop reason
		if candidate.FinishReason != "" {
			choice.StopReason = string(candidate.FinishReason)
		}

		choices = append(choices, choice)
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

// parseJSONObject parses a JSON string into a map
func parseJSONObject(jsonStr string) map[string]interface{} {
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return make(map[string]interface{})
	}
	return result
}

// logInputDetails logs the input parameters before making the API call
func (g *GoogleGenAIAdapter) logInputDetails(modelID string, messages []llmtypes.MessageContent, config *genai.GenerateContentConfig, opts *llmtypes.CallOptions) {
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

	g.logger.Debugf("Google GenAI GenerateContent INPUT - %+v", inputSummary)
}

// logErrorDetails logs both input and error response details when an error occurs
func (g *GoogleGenAIAdapter) logErrorDetails(modelID string, messages []llmtypes.MessageContent, config *genai.GenerateContentConfig, opts *llmtypes.CallOptions, err error, result *genai.GenerateContentResponse) {
	// Log error with input context
	errorInfo := map[string]interface{}{
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
	g.logger.Errorf("Google GenAI GenerateContent ERROR - %+v", errorInfo)

	// Also log input details for full context
	g.logInputDetails(modelID, messages, config, opts)
}

// logRawResponse logs the complete raw GenAI API response as JSON for debugging
func (g *GoogleGenAIAdapter) logRawResponse(modelID string, result *genai.GenerateContentResponse, err error) {
	g.logger.Infof("🔍 Raw Vertex (GenAI) response received - model: %s, err: %v, result: %v", modelID, err != nil, result != nil)

	if result == nil {
		g.logger.Infof("🔍 Raw Vertex response is nil")
		return
	}

	// Log response structure summary
	g.logger.Infof("🔍 Raw Vertex response structure - Candidates: %d", len(result.Candidates))

	// Log candidates details
	for i, candidate := range result.Candidates {
		g.logger.Infof("🔍 Candidate %d:", i)
		g.logger.Infof("   FinishReason: %q", candidate.FinishReason)
		if candidate.Content != nil {
			g.logger.Infof("   Content.Parts count: %d", len(candidate.Content.Parts))
			for j, part := range candidate.Content.Parts {
				if part.Text != "" {
					textPreview := part.Text
					if len(textPreview) > 200 {
						textPreview = textPreview[:200] + "..."
					}
					g.logger.Infof("     Part %d - Text: %q (length: %d)", j, textPreview, len(part.Text))
				}
				if part.FunctionCall != nil {
					g.logger.Infof("     Part %d - FunctionCall: Name=%q, Args=%v", j, part.FunctionCall.Name, part.FunctionCall.Args != nil)
				}
			}
		} else {
			g.logger.Infof("   Content: nil")
		}
	}

	// Log usage metadata
	if result.UsageMetadata != nil {
		g.logger.Infof("🔍 UsageMetadata:")
		g.logger.Infof("   PromptTokenCount: %d", result.UsageMetadata.PromptTokenCount)
		g.logger.Infof("   CandidatesTokenCount: %d", result.UsageMetadata.CandidatesTokenCount)
		g.logger.Infof("   TotalTokenCount: %d", result.UsageMetadata.TotalTokenCount)
		g.logger.Infof("   CachedContentTokenCount: %d", result.UsageMetadata.CachedContentTokenCount)
		g.logger.Infof("   ToolUsePromptTokenCount: %d", result.UsageMetadata.ToolUsePromptTokenCount)
		g.logger.Infof("   ThoughtsTokenCount: %d", result.UsageMetadata.ThoughtsTokenCount)
	}

	// Log prompt feedback if available (important for understanding why content might be empty)
	if result.PromptFeedback != nil {
		g.logger.Infof("🔍 PromptFeedback:")
		g.logger.Infof("   BlockReason: %q", result.PromptFeedback.BlockReason)
		if result.PromptFeedback.BlockReason != "" {
			g.logger.Warnf("⚠️ Vertex API blocked the prompt - BlockReason: %q", result.PromptFeedback.BlockReason)
		}
		if result.PromptFeedback.SafetyRatings != nil && len(result.PromptFeedback.SafetyRatings) > 0 {
			g.logger.Infof("   SafetyRatings count: %d", len(result.PromptFeedback.SafetyRatings))
			for k, rating := range result.PromptFeedback.SafetyRatings {
				g.logger.Infof("     SafetyRating %d - Category: %q, Probability: %q", k, rating.Category, rating.Probability)
			}
		}
	}

	// Try to serialize the full response to JSON for complete debugging
	// Note: This may fail if genai.GenerateContentResponse has unexported fields or circular references
	// We'll log what we can extract manually above, but try JSON as well
	type responseSummary struct {
		CandidatesCount            int
		HasUsageMetadata           bool
		HasPromptFeedback          bool
		FirstCandidateFinishReason string
		FirstCandidatePartsCount   int
		FirstCandidateTextLength   int
		ResultTextHelper           string
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
			for _, part := range firstCandidate.Content.Parts {
				summary.FirstCandidateTextLength += len(part.Text)
			}
		}
		summary.ResultTextHelper = result.Text()
	}

	if summaryJSON, err := json.MarshalIndent(summary, "   ", "  "); err == nil {
		jsonStr := string(summaryJSON)
		if len(jsonStr) > 5000 {
			jsonStr = jsonStr[:5000] + "\n   ... (truncated)"
		}
		g.logger.Infof("🔍 RAW VERTEX RESPONSE SUMMARY (JSON):")
		g.logger.Infof("%s", jsonStr)
	} else {
		g.logger.Warnf("⚠️ Failed to serialize response summary to JSON: %v", err)
	}
}

// WithResponseSchema returns a context with the ResponseSchema set
// This allows structured output generation with schema validation
func WithResponseSchema(ctx context.Context, schema *genai.Schema) context.Context {
	return context.WithValue(ctx, ResponseSchemaKey, schema)
}

// generateToolCallID generates a unique ID for tool calls
// In a real implementation, you might want to use a proper ID generator
var toolCallCounter int64 = 0

func generateToolCallID() string {
	toolCallCounter++
	return fmt.Sprintf("call_%d", toolCallCounter)
}
