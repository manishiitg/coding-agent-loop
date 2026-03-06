// Package minimax provides an adapter for MiniMax's Anthropic-compatible API.
// MiniMax exposes an Anthropic-compatible endpoint at https://api.minimax.io
// We use the Anthropic Go SDK with a custom base URL.
package minimax

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/interfaces"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
)

const (
	// MiniMaxAnthropicBaseURL is the base URL for MiniMax's Anthropic-compatible API.
	// The Anthropic SDK appends /v1/messages, resulting in https://api.minimax.io/anthropic/v1/messages.
	MiniMaxAnthropicBaseURL = "https://api.minimax.io/anthropic"
)

// MiniMaxAdapter implements the llmtypes.Model interface using MiniMax's
// Anthropic-compatible API.
type MiniMaxAdapter struct {
	client  anthropic.Client
	modelID string
	logger  interfaces.Logger
}

// NewMiniMaxAdapter creates a new MiniMax adapter using the Anthropic SDK.
func NewMiniMaxAdapter(apiKey, modelID string, logger interfaces.Logger) *MiniMaxAdapter {
	client := anthropic.NewClient(
		anthropicoption.WithAPIKey(apiKey),
		anthropicoption.WithBaseURL(MiniMaxAnthropicBaseURL),
	)
	return &MiniMaxAdapter{
		client:  client,
		modelID: modelID,
		logger:  logger,
	}
}

// GetModelID implements the llmtypes.Model interface
func (m *MiniMaxAdapter) GetModelID() string {
	return m.modelID
}

// GetModelMetadata implements the llmtypes.Model interface
func (m *MiniMaxAdapter) GetModelMetadata(modelID string) (*llmtypes.ModelMetadata, error) {
	return GetMiniMaxModelMetadata(modelID)
}

// GenerateContent implements the llmtypes.Model interface
func (m *MiniMaxAdapter) GenerateContent(ctx context.Context, messages []llmtypes.MessageContent, options ...llmtypes.CallOption) (*llmtypes.ContentResponse, error) {
	opts := &llmtypes.CallOptions{}
	for _, opt := range options {
		opt(opts)
	}

	modelID := m.modelID
	if opts.Model != "" {
		modelID = opts.Model
	}

	anthropicMessages, systemMessage := convertMessages(messages)

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(modelID),
		Messages:  anthropicMessages,
		MaxTokens: 8192,
	}

	if systemMessage != "" {
		if opts.JSONMode {
			systemMessage = systemMessage + "\n\nYou must respond with valid JSON only, no other text. Return a JSON object."
		}
		params.System = []anthropic.TextBlockParam{
			{Text: systemMessage},
		}
	} else if opts.JSONMode && len(anthropicMessages) > 0 {
		jsonInstruction := anthropic.NewTextBlock("You must respond with valid JSON only, no other text. Return a JSON object.")
		if len(anthropicMessages) > 0 && anthropicMessages[0].Role == anthropic.MessageParamRoleUser {
			anthropicMessages[0].Content = append([]anthropic.ContentBlockParamUnion{jsonInstruction}, anthropicMessages[0].Content...)
		}
	}

	if opts.Temperature > 0 {
		params.Temperature = anthropic.Float(opts.Temperature)
	}
	if opts.MaxTokens > 0 {
		params.MaxTokens = int64(opts.MaxTokens)
	}

	if len(opts.Tools) > 0 {
		params.Tools = convertTools(opts.Tools)
		if opts.ToolChoice != nil {
			params.ToolChoice = convertToolChoice(opts.ToolChoice)
		}
	}

	if m.logger != nil {
		m.logger.Debugf("[MINIMAX] GenerateContent model=%s messages=%d tools=%d", modelID, len(messages), len(params.Tools))
	}

	stream := m.client.Messages.NewStreaming(ctx, params)

	defer func() {
		if opts.StreamChan != nil {
			close(opts.StreamChan)
		}
	}()

	message := anthropic.Message{}
	var contentChunksSent int
	for stream.Next() {
		event := stream.Current()

		if err := message.Accumulate(event); err != nil {
			stream.Close()
			return nil, fmt.Errorf("minimax streaming accumulate error: %w", err)
		}

		if opts.StreamChan != nil {
			switch eventVariant := event.AsAny().(type) {
			case anthropic.ContentBlockDeltaEvent:
				switch deltaVariant := eventVariant.Delta.AsAny().(type) {
				case anthropic.TextDelta:
					if deltaVariant.Text != "" {
						contentChunksSent++
						select {
						case opts.StreamChan <- llmtypes.StreamChunk{
							Type:    llmtypes.StreamChunkTypeContent,
							Content: deltaVariant.Text,
						}:
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					}
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		if m.logger != nil {
			m.logger.Errorf("[MINIMAX] Streaming error: %v", err)
		}
		return nil, fmt.Errorf("minimax streaming error: %w", err)
	}
	stream.Close()

	if m.logger != nil {
		m.logger.Debugf("[MINIMAX] Stream complete: content_chunks=%d stop_reason=%s", contentChunksSent, message.StopReason)
	}

	// Fallback: if no chunks sent during streaming but message has text, emit it now
	if opts.StreamChan != nil {
		if contentChunksSent == 0 {
			var textContent strings.Builder
			for _, block := range message.Content {
				if block.Type == "text" && block.Text != "" {
					textContent.WriteString(block.Text)
				}
			}
			if textContent.Len() > 0 {
				select {
				case opts.StreamChan <- llmtypes.StreamChunk{
					Type:    llmtypes.StreamChunkTypeContent,
					Content: textContent.String(),
				}:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}

		// Stream tool calls
		for _, block := range message.Content {
			if block.Type == "tool_use" {
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
				select {
				case opts.StreamChan <- llmtypes.StreamChunk{
					Type:     llmtypes.StreamChunkTypeToolCall,
					ToolCall: &toolCall,
				}:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
		}
	}

	return convertResponse(&message), nil
}

// Call implements a convenience method for simple text generation
func (m *MiniMaxAdapter) Call(ctx context.Context, prompt string, options ...llmtypes.CallOption) (string, error) {
	messages := []llmtypes.MessageContent{
		{
			Role:  llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{llmtypes.TextContent{Text: prompt}},
		},
	}
	resp, err := m.GenerateContent(ctx, messages, options...)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return resp.Choices[0].Content, nil
}

// convertMessages converts llmtypes messages to Anthropic message format
func convertMessages(langMessages []llmtypes.MessageContent) ([]anthropic.MessageParam, string) {
	anthropicMessages := make([]anthropic.MessageParam, 0, len(langMessages))
	var systemMessage string

	for _, msg := range langMessages {
		var contentParts []string
		var imageParts []llmtypes.ImageContent
		var toolCallID string
		var toolResponseContent string
		var toolCalls []llmtypes.ToolCall

		for _, part := range msg.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				contentParts = append(contentParts, p.Text)
			case llmtypes.ImageContent:
				imageParts = append(imageParts, p)
			case llmtypes.ToolCallResponse:
				toolCallID = p.ToolCallID
				toolResponseContent = p.Content
			case llmtypes.ToolCall:
				toolCalls = append(toolCalls, p)
			}
		}

		switch string(msg.Role) {
		case string(llmtypes.ChatMessageTypeSystem):
			if len(contentParts) > 0 {
				systemMessage = strings.Join(contentParts, "\n")
			}
		case string(llmtypes.ChatMessageTypeHuman):
			contentBlocks := []anthropic.ContentBlockParamUnion{}
			if len(contentParts) > 0 {
				contentBlocks = append(contentBlocks, anthropic.NewTextBlock(strings.Join(contentParts, "\n")))
			}
			for _, img := range imageParts {
				if block := createImageBlock(img); block != nil {
					contentBlocks = append(contentBlocks, *block)
				}
			}
			if len(contentBlocks) > 0 {
				anthropicMessages = append(anthropicMessages, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleUser,
					Content: contentBlocks,
				})
			}
		case string(llmtypes.ChatMessageTypeAI):
			contentBlocks := []anthropic.ContentBlockParamUnion{}
			if len(contentParts) > 0 {
				contentBlocks = append(contentBlocks, anthropic.NewTextBlock(strings.Join(contentParts, "\n")))
			}
			for _, tc := range toolCalls {
				var args map[string]interface{}
				if tc.FunctionCall.Arguments != "" {
					if err := json.Unmarshal([]byte(tc.FunctionCall.Arguments), &args); err != nil {
						args = make(map[string]interface{})
					}
				} else {
					args = make(map[string]interface{})
				}
				contentBlocks = append(contentBlocks, anthropic.NewToolUseBlock(tc.ID, args, tc.FunctionCall.Name))
			}
			if len(contentBlocks) > 0 {
				anthropicMessages = append(anthropicMessages, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleAssistant,
					Content: contentBlocks,
				})
			}
		case string(llmtypes.ChatMessageTypeTool):
			if toolCallID != "" {
				anthropicMessages = append(anthropicMessages, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleUser,
					Content: []anthropic.ContentBlockParamUnion{anthropic.NewToolResultBlock(toolCallID, toolResponseContent, false)},
				})
			}
		default:
			contentBlocks := []anthropic.ContentBlockParamUnion{}
			if len(contentParts) > 0 {
				contentBlocks = append(contentBlocks, anthropic.NewTextBlock(strings.Join(contentParts, "\n")))
			}
			for _, img := range imageParts {
				if block := createImageBlock(img); block != nil {
					contentBlocks = append(contentBlocks, *block)
				}
			}
			if len(contentBlocks) > 0 {
				anthropicMessages = append(anthropicMessages, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleUser,
					Content: contentBlocks,
				})
			}
		}
	}

	return anthropicMessages, systemMessage
}

func createImageBlock(img llmtypes.ImageContent) *anthropic.ContentBlockParamUnion {
	if img.SourceType == "base64" {
		block := anthropic.NewImageBlockBase64(img.MediaType, img.Data)
		return &block
	} else if img.SourceType == "url" {
		block := anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: img.Data})
		return &block
	}
	return nil
}

func convertTools(llmTools []llmtypes.Tool) []anthropic.ToolUnionParam {
	anthropicTools := make([]anthropic.ToolUnionParam, 0, len(llmTools))
	for _, tool := range llmTools {
		if tool.Function == nil {
			continue
		}
		var parameters map[string]interface{}
		if tool.Function.Parameters != nil {
			if b, err := json.Marshal(tool.Function.Parameters); err == nil {
				var m map[string]interface{}
				if err := json.Unmarshal(b, &m); err == nil {
					parameters = m
				}
			}
		}
		if parameters == nil {
			parameters = make(map[string]interface{})
		}

		var required []string
		if req, ok := parameters["required"].([]interface{}); ok {
			for _, r := range req {
				if s, ok := r.(string); ok {
					required = append(required, s)
				}
			}
		}
		properties := make(map[string]interface{})
		if props, ok := parameters["properties"].(map[string]interface{}); ok {
			properties = props
		}

		anthropicTools = append(anthropicTools, anthropic.ToolUnionParamOfTool(
			anthropic.ToolInputSchemaParam{Properties: properties, Required: required},
			tool.Function.Name,
		))
	}
	return anthropicTools
}

func convertToolChoice(toolChoice interface{}) anthropic.ToolChoiceUnionParam {
	if choiceStr, ok := toolChoice.(string); ok {
		switch choiceStr {
		case "none":
			return anthropic.ToolChoiceUnionParam{OfNone: &anthropic.ToolChoiceNoneParam{}}
		case "required", "any":
			return anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}
		default:
			return anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{}}
		}
	}
	if tc, ok := toolChoice.(*llmtypes.ToolChoice); ok && tc != nil {
		if tc.Function != nil && tc.Function.Name != "" {
			return anthropic.ToolChoiceParamOfTool(tc.Function.Name)
		}
		switch tc.Type {
		case "none":
			return anthropic.ToolChoiceUnionParam{OfNone: &anthropic.ToolChoiceNoneParam{}}
		case "required", "any":
			return anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}
		}
	}
	return anthropic.ToolChoiceUnionParam{OfAuto: &anthropic.ToolChoiceAutoParam{}}
}

func convertResponse(result *anthropic.Message) *llmtypes.ContentResponse {
	if result == nil {
		return &llmtypes.ContentResponse{Choices: []*llmtypes.ContentChoice{}}
	}

	choice := &llmtypes.ContentChoice{}
	var textParts []string
	var toolCalls []llmtypes.ToolCall

	for _, block := range result.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		case "tool_use":
			argsJSON := []byte("{}")
			if len(block.Input) > 0 {
				argsJSON = block.Input
			}
			toolCalls = append(toolCalls, llmtypes.ToolCall{
				ID:   block.ID,
				Type: "function",
				FunctionCall: &llmtypes.FunctionCall{
					Name:      block.Name,
					Arguments: string(argsJSON),
				},
			})
		}
	}

	if len(textParts) > 0 {
		choice.Content = strings.Join(textParts, "\n")
	}
	if len(toolCalls) > 0 {
		choice.ToolCalls = toolCalls
	}
	if result.StopReason != "" {
		choice.StopReason = string(result.StopReason)
	}

	inputTokens := int(result.Usage.InputTokens)
	outputTokens := int(result.Usage.OutputTokens)
	totalTokens := inputTokens + outputTokens
	genInfo := &llmtypes.GenerationInfo{
		InputTokens:  &inputTokens,
		OutputTokens: &outputTokens,
		TotalTokens:  &totalTokens,
	}

	choice.GenerationInfo = genInfo
	usage := llmtypes.ExtractUsageFromGenerationInfo(genInfo)
	return &llmtypes.ContentResponse{
		Choices: []*llmtypes.ContentChoice{choice},
		Usage:   usage,
	}
}
