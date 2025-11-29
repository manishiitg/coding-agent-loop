package vertex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"llm-providers/interfaces"
	"llm-providers/internal/recorder"
	"llm-providers/llmtypes"
	"llm-providers/pkg/utils"

	"google.golang.org/genai"
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
	logger  interfaces.Logger
}

// NewGoogleGenAIAdapter creates a new adapter instance
func NewGoogleGenAIAdapter(client *genai.Client, modelID string, logger interfaces.Logger) *GoogleGenAIAdapter {
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

	// Build a map of all function call IDs to their tool names for matching responses from previous turns
	allFunctionCallIDs := make(map[string]string) // ID -> tool name
	for _, msg := range messages {
		if msg.Role == llmtypes.ChatMessageTypeAI {
			for _, part := range msg.Parts {
				if toolCall, ok := part.(llmtypes.ToolCall); ok {
					allFunctionCallIDs[toolCall.ID] = toolCall.FunctionCall.Name
				}
			}
		}
	}

	if g.logger != nil {
		g.logger.Debugf("🔍 [GEMINI] Processing %d messages for conversion, found %d total function calls in conversation history",
			len(messages), len(allFunctionCallIDs))
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
				g.logger.Debugf("⚠️ [GEMINI] Model message contains both TextContent and ToolCall parts - splitting into separate messages to avoid empty responses. Text preview: %q, Tool calls: %v", textPreview, toolNames)
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
					genaiParts := g.convertMessageParts(textMsg.Parts, modelID)
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
				genaiParts := g.convertMessageParts(toolCallMsg.Parts, modelID)
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
			// IMPORTANT: Gemini matches responses to calls by POSITION/ORDER, not by ID
			if len(previousFunctionCallIDs) > 0 {
				// First, try to match by ID (for proper association)
				responseMap := make(map[string]llmtypes.ToolCallResponse)
				for _, resp := range functionResponses {
					responseMap[resp.ToolCallID] = resp
				}

				// Try to match responses to calls by ID
				orderedResponses := make([]llmtypes.ContentPart, 0, len(previousFunctionCallIDs))
				matchedByID := 0
				for _, callID := range previousFunctionCallIDs {
					if resp, found := responseMap[callID]; found {
						orderedResponses = append(orderedResponses, resp)
						matchedByID++
					} else {
						// ID not found - will use order-based fallback
						orderedResponses = append(orderedResponses, nil) // placeholder
					}
				}

				// If ID matching failed, fall back to order-based matching
				// This handles cases where IDs don't match but we have the right number of responses
				if matchedByID < len(previousFunctionCallIDs) && len(functionResponses) == len(previousFunctionCallIDs) {
					if g.logger != nil {
						g.logger.Debugf("🔍 [GEMINI] ID matching incomplete (%d/%d matched), falling back to order-based matching", matchedByID, len(previousFunctionCallIDs))
					}
					// Use responses in order (Gemini matches by position)
					orderedResponses = make([]llmtypes.ContentPart, 0, len(functionResponses))
					for _, resp := range functionResponses {
						orderedResponses = append(orderedResponses, resp)
					}
				} else if matchedByID == 0 && len(functionResponses) == len(previousFunctionCallIDs) {
					// No IDs matched but count matches - use order-based
					if g.logger != nil {
						g.logger.Debugf("🔍 [GEMINI] No IDs matched, using order-based matching (%d responses for %d calls)", len(functionResponses), len(previousFunctionCallIDs))
					}
					orderedResponses = make([]llmtypes.ContentPart, 0, len(functionResponses))
					for _, resp := range functionResponses {
						orderedResponses = append(orderedResponses, resp)
					}
				} else if matchedByID < len(previousFunctionCallIDs) {
					// Some IDs matched but not all - use what we have, fill missing with order
					if g.logger != nil {
						g.logger.Debugf("🔍 [GEMINI] Partial ID matching (%d/%d), using hybrid approach", matchedByID, len(previousFunctionCallIDs))
					}
					// Remove nil placeholders and use order for unmatched
					orderedResponses = make([]llmtypes.ContentPart, 0, len(functionResponses))
					usedResponses := make(map[int]bool)
					// First add matched responses
					for _, callID := range previousFunctionCallIDs {
						if resp, found := responseMap[callID]; found {
							orderedResponses = append(orderedResponses, resp)
							// Find which response index this was
							for j, r := range functionResponses {
								if r.ToolCallID == resp.ToolCallID {
									usedResponses[j] = true
									break
								}
							}
						}
					}
					// Then add unmatched responses in order
					for i, resp := range functionResponses {
						if !usedResponses[i] {
							orderedResponses = append(orderedResponses, resp)
						}
					}
				}

				// Check if we have the right number of responses
				if len(orderedResponses) != len(previousFunctionCallIDs) {
					if g.logger != nil {
						g.logger.Errorf("❌ [GEMINI] Function response count mismatch - expected %d, got %d. This may cause API error.",
							len(previousFunctionCallIDs), len(orderedResponses))
					}
					// Still try to send what we have - Gemini might handle it
					if len(orderedResponses) == 0 {
						// No responses at all - skip this message
						previousFunctionCallIDs = nil
						continue
					}
				}

				// Update message parts with ordered responses
				if len(orderedResponses) > 0 {
					if g.logger != nil {
						g.logger.Debugf("🔍 [GEMINI] Message %d: Using %d responses for %d function calls (matched by ID: %d)",
							msgIdx, len(orderedResponses), len(previousFunctionCallIDs), matchedByID)
					}
					msg.Parts = orderedResponses
				}
			} else if len(functionResponses) > 0 {
				// We have responses but no previous function calls tracked in the immediately previous message
				// Gemini requires function responses to match the IMMEDIATELY previous function calls
				// Even if these responses match function calls from earlier turns, Gemini will reject them
				// So we must convert them to text so the LLM can see them
				if g.logger != nil {
					g.logger.Errorf("⚠️ [GEMINI] Message %d: Found %d function responses but no immediately previous function calls. "+
						"Gemini requires responses to match immediately previous calls, so converting to text so LLM can see them.",
						msgIdx, len(functionResponses))
					for i, resp := range functionResponses {
						contentPreview := resp.Content
						if len(contentPreview) > 100 {
							contentPreview = contentPreview[:100] + "..."
						}
						matched := ""
						if _, exists := allFunctionCallIDs[resp.ToolCallID]; exists {
							matched = fmt.Sprintf(" (matches call from earlier turn: %s)", allFunctionCallIDs[resp.ToolCallID])
						} else {
							matched = " (no matching call found)"
						}
						g.logger.Debugf("🔍 [GEMINI]   Response %d: ToolCallID=%s, Name=%s, Content preview: %s%s",
							i+1, resp.ToolCallID, resp.Name, contentPreview, matched)
					}
				}
				// Convert all tool responses to text messages so Gemini can see them
				// Format: "Tool [name] returned: [content]"
				textParts := make([]llmtypes.ContentPart, 0, len(functionResponses))
				for _, resp := range functionResponses {
					textContent := fmt.Sprintf("Tool %s returned: %s", resp.Name, resp.Content)
					textParts = append(textParts, llmtypes.TextContent{Text: textContent})
					if g.logger != nil {
						contentPreview := resp.Content
						if len(contentPreview) > 50 {
							contentPreview = contentPreview[:50] + "..."
						}
						g.logger.Debugf("🔍 [GEMINI]   Converting response to text: Tool %s returned: %s",
							resp.Name, contentPreview)
					}
				}
				// Replace tool response parts with text parts
				msg.Parts = textParts
				// Change role to user so it's visible in conversation history
				msg.Role = llmtypes.ChatMessageTypeHuman
				if g.logger != nil {
					g.logger.Infof("✅ [GEMINI] Converted %d tool responses to text message (role changed from Tool to Human, %d text parts)",
						len(functionResponses), len(textParts))
					// Verify the conversion
					for i, part := range msg.Parts {
						if textPart, ok := part.(llmtypes.TextContent); ok {
							preview := textPart.Text
							if len(preview) > 100 {
								preview = preview[:100] + "..."
							}
							g.logger.Debugf("🔍 [GEMINI]   Text part %d: %s", i+1, preview)
						} else {
							g.logger.Errorf("❌ [GEMINI]   Part %d is not TextContent! Type: %T", i+1, part)
						}
					}
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
		if g.logger != nil {
			g.logger.Infof("🔍 [GEMINI] Message %d (role: %s): Converting %d parts", msgIdx, msg.Role, len(msg.Parts))
			for i, part := range msg.Parts {
				switch p := part.(type) {
				case llmtypes.TextContent:
					preview := p.Text
					if len(preview) > 50 {
						preview = preview[:50] + "..."
					}
					g.logger.Infof("🔍 [GEMINI]   Part %d: TextContent: %s", i+1, preview)
				case llmtypes.ToolCallResponse:
					contentPreview := p.Content
					if len(contentPreview) > 50 {
						contentPreview = contentPreview[:50] + "..."
					}
					g.logger.Infof("🔍 [GEMINI]   Part %d: ToolCallResponse: ToolCallID=%s, Name=%s, Content: %s",
						i+1, p.ToolCallID, p.Name, contentPreview)
				case llmtypes.ToolCall:
					if p.FunctionCall != nil {
						g.logger.Infof("🔍 [GEMINI]   Part %d: ToolCall: ID=%s, Name=%s, Arguments length=%d",
							i+1, p.ID, p.FunctionCall.Name, len(p.FunctionCall.Arguments))
					} else {
						g.logger.Errorf("❌ [GEMINI]   Part %d: ToolCall: ID=%s, FunctionCall is nil!", i+1, p.ID)
					}
				default:
					g.logger.Infof("🔍 [GEMINI]   Part %d: Unknown type: %T", i+1, part)
				}
			}
		}

		genaiParts := g.convertMessageParts(msg.Parts, modelID)

		if g.logger != nil {
			g.logger.Infof("🔍 [GEMINI] Message %d (role: %s): convertMessageParts returned %d genai parts",
				msgIdx, msg.Role, len(genaiParts))
		}

		if len(genaiParts) > 0 {
			role := convertRole(string(msg.Role))

			// Log tool responses being sent to Gemini
			if msg.Role == llmtypes.ChatMessageTypeTool {
				if g.logger != nil {
					for _, part := range msg.Parts {
						if toolResp, ok := part.(llmtypes.ToolCallResponse); ok {
							contentPreview := toolResp.Content
							if len(contentPreview) > 100 {
								contentPreview = contentPreview[:100] + "..."
							}
							g.logger.Infof("✅ [GEMINI] Sending tool response to Gemini - ToolCallID: %s, Name: %s, Content: %s",
								toolResp.ToolCallID, toolResp.Name, contentPreview)
						}
					}
				}
			}

			genaiContents = append(genaiContents, &genai.Content{
				Role:  role,
				Parts: genaiParts,
			})
		} else {
			// Message with no parts after conversion - log details for debugging
			if g.logger != nil {
				g.logger.Errorf("❌ [GEMINI] Message %d (role: %s) has no parts after conversion! Original parts count: %d",
					msgIdx, msg.Role, len(msg.Parts))
				for i, part := range msg.Parts {
					g.logger.Errorf("❌ [GEMINI]   Part %d: type=%T", i+1, part)
					if textPart, ok := part.(llmtypes.TextContent); ok {
						preview := textPart.Text
						if len(preview) > 100 {
							preview = preview[:100] + "..."
						}
						g.logger.Errorf("❌ [GEMINI]     Text content: %s", preview)
					} else if toolResp, ok := part.(llmtypes.ToolCallResponse); ok {
						g.logger.Errorf("❌ [GEMINI]     Tool response: ToolCallID=%s, Name=%s, Content length=%d",
							toolResp.ToolCallID, toolResp.Name, len(toolResp.Content))
					}
				}
			}
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
		// Clamp to int32 max to prevent integer overflow
		maxTokens := opts.MaxTokens
		if maxTokens > math.MaxInt32 {
			maxTokens = math.MaxInt32
		}
		config.MaxOutputTokens = int32(maxTokens)
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

	// Use streaming path for both streaming and non-streaming requests
	// For non-streaming (StreamChan == nil), the streaming function will accumulate tokens
	// without sending chunks to the channel, ensuring consistent thought signature handling
	return g.generateContentStreaming(ctx, modelID, genaiContents, config, opts, hadMixedMessages, requestID, messages)
}

// generateContentStreaming handles streaming responses from Google GenAI API
// It works for both streaming (StreamChan != nil) and non-streaming (StreamChan == nil) requests
// For non-streaming, it accumulates tokens without sending chunks to the channel
func (g *GoogleGenAIAdapter) generateContentStreaming(ctx context.Context, modelID string, genaiContents []*genai.Content, config *genai.GenerateContentConfig, opts *llmtypes.CallOptions, hadMixedMessages bool, requestID string, messages []llmtypes.MessageContent) (*llmtypes.ContentResponse, error) {
	// Ensure channel is closed when done (only if streaming was requested)
	defer func() {
		if opts.StreamChan != nil {
			close(opts.StreamChan)
		}
	}()

	// Check for recorder in context
	rec, found := recorder.FromContext(ctx)
	if g.logger != nil {
		if found && rec != nil {
			if rec.IsRecordingEnabled() {
				g.logger.Infof("📹 [RECORDER] Recording enabled in adapter")
			} else if rec.IsReplayEnabled() {
				g.logger.Infof("▶️  [RECORDER] Replay enabled in adapter")
			}
		} else {
			g.logger.Debugf("📹 [RECORDER] No recorder found in context")
		}
	}
	var recordedChunks []interface{}

	// Accumulate response data
	var accumulatedContent strings.Builder
	var accumulatedToolCalls []llmtypes.ToolCall
	var usage *genai.GenerateContentResponseUsageMetadata
	var sharedThoughtSignature string // For parallel tool calls, share thought signature across all

	// Handle replay mode - create iterator from recorded chunks
	if rec != nil && rec.IsReplayEnabled() {
		// Build request info for matching
		requestInfo := buildRequestInfo(messages, modelID, opts)
		chunks, err := rec.LoadVertexChunks(requestInfo)
		if err != nil {
			return nil, fmt.Errorf("failed to load recorded chunks: %w", err)
		}

		if g.logger != nil {
			g.logger.Infof("▶️  [RECORDER] Replaying %d recorded chunks", len(chunks))
		}

		// Convert loaded chunks to genai responses and iterate
		for _, chunk := range chunks {
			// Convert map to genai.GenerateContentResponse
			chunkJSON, _ := json.Marshal(chunk)
			var response genai.GenerateContentResponse
			if err := json.Unmarshal(chunkJSON, &response); err != nil {
				continue
			}

			// Process this replayed chunk
			if response.UsageMetadata != nil {
				usage = response.UsageMetadata
			}

			// Process candidates (same logic as below)
			for _, candidate := range response.Candidates {
				if candidate.Content != nil {
					for _, part := range candidate.Content.Parts {
						if part.Text != "" {
							accumulatedContent.WriteString(part.Text)
							if opts.StreamChan != nil {
								select {
								case opts.StreamChan <- llmtypes.StreamChunk{
									Type:    llmtypes.StreamChunkTypeContent,
									Content: part.Text,
								}:
								case <-ctx.Done():
									return nil, ctx.Err()
								}
							}
						}
						if part.FunctionCall != nil {
							thoughtSignature := extractThoughtSignature(part, g.logger)
							if thoughtSignature == "" && sharedThoughtSignature != "" {
								thoughtSignature = sharedThoughtSignature
							}
							toolCallID := generateToolCallID()
							argsJSON := convertArgumentsToString(part.FunctionCall.Args)
							toolCall := llmtypes.ToolCall{
								ID:               toolCallID,
								Type:             "function",
								ThoughtSignature: thoughtSignature,
								FunctionCall: &llmtypes.FunctionCall{
									Name:      part.FunctionCall.Name,
									Arguments: argsJSON,
								},
							}
							accumulatedToolCalls = append(accumulatedToolCalls, toolCall)
							if opts.StreamChan != nil {
								toolCallCopy := toolCall
								select {
								case opts.StreamChan <- llmtypes.StreamChunk{
									Type:     llmtypes.StreamChunkTypeToolCall,
									ToolCall: &toolCallCopy,
								}:
								case <-ctx.Done():
									return nil, ctx.Err()
								}
							}
						}
					}
				}
			}
		}
	} else {
		// Normal mode: Call Google GenAI streaming API and process responses
		stream := g.client.Models.GenerateContentStream(ctx, modelID, genaiContents, config)

		// Process streaming responses
		for response, err := range stream {
			if err != nil {
				if g.logger != nil {
					g.logErrorDetails(requestID, modelID, messages, config, opts, err, nil)
				}
				return nil, fmt.Errorf("genai streaming error: %w", err)
			}

			// Record chunk if recording is enabled
			if rec != nil && rec.IsRecordingEnabled() {
				// Marshal response to JSON for recording
				chunkJSON, err := json.Marshal(response)
				if err == nil {
					var chunkMap map[string]interface{}
					if json.Unmarshal(chunkJSON, &chunkMap) == nil {
						recordedChunks = append(recordedChunks, chunkMap)
						if g.logger != nil {
							g.logger.Debugf("📹 [RECORDER] Captured chunk %d", len(recordedChunks))
						}
					}
				} else if g.logger != nil {
					g.logger.Debugf("📹 [RECORDER] Failed to marshal chunk: %v", err)
				}
			}

			// Extract usage metadata if available
			if response.UsageMetadata != nil {
				usage = response.UsageMetadata
			}

			// Process candidates
			for _, candidate := range response.Candidates {
				// First pass: Extract thought signature from any part (for parallel calls)
				if candidate.Content != nil {
					for _, part := range candidate.Content.Parts {
						if part.FunctionCall != nil {
							thoughtSig := extractThoughtSignature(part, g.logger)
							if thoughtSig != "" && sharedThoughtSignature == "" {
								sharedThoughtSignature = thoughtSig
								if g.logger != nil {
									g.logger.Infof("✅ [VERTEX] Found thought signature in streaming part (function call: %s), will share with all parallel tool calls", part.FunctionCall.Name)
								}
							}
						} else if part.Text == "" {
							// Check empty text parts too
							thoughtSig := extractThoughtSignature(part, g.logger)
							if thoughtSig != "" && sharedThoughtSignature == "" {
								sharedThoughtSignature = thoughtSig
								if g.logger != nil {
									g.logger.Infof("✅ [VERTEX] Found thought signature in streaming empty text part, will share with all parallel tool calls")
								}
							}
						}
					}
				}

				// Second pass: Extract content and tool calls
				if candidate.Content != nil {
					for _, part := range candidate.Content.Parts {
						// Extract text content and stream immediately
						if part.Text != "" {
							accumulatedContent.WriteString(part.Text)
							if opts.StreamChan != nil {
								select {
								case opts.StreamChan <- llmtypes.StreamChunk{
									Type:    llmtypes.StreamChunkTypeContent,
									Content: part.Text,
								}:
								case <-ctx.Done():
									return nil, ctx.Err()
								}
							}
						}

						// Extract function calls (tool calls)
						if part.FunctionCall != nil {
							// Extract thought signature from this specific part first
							thoughtSignature := extractThoughtSignature(part, g.logger)
							// If not found in this part, use the shared one (for parallel calls)
							if thoughtSignature == "" && sharedThoughtSignature != "" {
								thoughtSignature = sharedThoughtSignature
								if g.logger != nil {
									g.logger.Debugf("🔍 [VERTEX] Using shared thought signature for streaming tool call %s (from another part)", part.FunctionCall.Name)
								}
							}

							// Generate tool call ID
							toolCallID := generateToolCallID()
							argsJSON := convertArgumentsToString(part.FunctionCall.Args)
							toolCall := llmtypes.ToolCall{
								ID:               toolCallID,
								Type:             "function",
								ThoughtSignature: thoughtSignature,
								FunctionCall: &llmtypes.FunctionCall{
									Name:      part.FunctionCall.Name,
									Arguments: argsJSON,
								},
							}
							accumulatedToolCalls = append(accumulatedToolCalls, toolCall)

							// Stream tool call when complete
							if opts.StreamChan != nil {
								toolCallCopy := toolCall
								select {
								case opts.StreamChan <- llmtypes.StreamChunk{
									Type:     llmtypes.StreamChunkTypeToolCall,
									ToolCall: &toolCallCopy,
								}:
								case <-ctx.Done():
									return nil, ctx.Err()
								}
							}
						}
					}
				}
			}
		}
	}

	// Build final response
	choice := &llmtypes.ContentChoice{
		Content: accumulatedContent.String(),
	}
	if len(accumulatedToolCalls) > 0 {
		choice.ToolCalls = accumulatedToolCalls
	}

	// Extract token usage if available
	choice.GenerationInfo = utils.ExtractGenerationInfoFromVertexUsage(usage)

	// Save recorded chunks if recording was enabled
	if rec != nil && rec.IsRecordingEnabled() && len(recordedChunks) > 0 {
		// Build request info for matching
		requestInfo := buildRequestInfo(messages, modelID, opts)
		filePath, err := rec.RecordVertexChunks(recordedChunks, requestInfo)
		if err != nil {
			if g.logger != nil {
				g.logger.Errorf("Failed to save recorded chunks: %v", err)
			}
		} else if g.logger != nil {
			g.logger.Infof("📹 [RECORDER] Saved %d chunks to %s", len(recordedChunks), filePath)
		}
	}

	return &llmtypes.ContentResponse{
		Choices: []*llmtypes.ContentChoice{choice},
	}, nil
}

// buildRequestInfo creates a RequestInfo from messages and options for recording/matching
func buildRequestInfo(messages []llmtypes.MessageContent, modelID string, opts *llmtypes.CallOptions) recorder.RequestInfo {
	// Convert messages to RequestInfo format
	messageInfos := make([]recorder.MessageInfo, 0, len(messages))
	for _, msg := range messages {
		// Convert parts to interface{} for JSON serialization
		parts := make([]interface{}, 0, len(msg.Parts))
		for _, part := range msg.Parts {
			// Marshal and unmarshal to get clean JSON representation
			partJSON, _ := json.Marshal(part)
			var partInterface interface{}
			json.Unmarshal(partJSON, &partInterface)
			parts = append(parts, partInterface)
		}
		messageInfos = append(messageInfos, recorder.MessageInfo{
			Role:  string(msg.Role),
			Parts: parts,
		})
	}

	// Build options info
	optionsInfo := recorder.OptionsInfo{
		Temperature: opts.Temperature,
		MaxTokens:   opts.MaxTokens,
		JSONMode:    opts.JSONMode,
		ToolsCount:  len(opts.Tools),
	}

	return recorder.RequestInfo{
		Messages: messageInfos,
		ModelID:  modelID,
		Options:  optionsInfo,
	}
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
func convertTools(llmTools []llmtypes.Tool, logger interfaces.Logger) []*genai.Tool {
	if len(llmTools) == 0 {
		return nil
	}

	// Collect all function declarations
	functionDeclarations := make([]*genai.FunctionDeclaration, 0, len(llmTools))

	for i, tool := range llmTools {
		if tool.Function == nil {
			if logger != nil {
				logger.Errorf("⚠️ [VERTEX] Tool %d has nil Function, skipping", i+1)
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
					logger.Errorf("⚠️ [VERTEX] Function %s: Schema conversion returned nil", tool.Function.Name)
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
func validateSchemaForGemini(schema map[string]interface{}, functionName string, logger interfaces.Logger) {
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
			logger.Errorf("⚠️ [VERTEX] Function %s: Schema has invalid type '%s'", functionName, schemaType)
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
func convertResponse(result *genai.GenerateContentResponse, logger interfaces.Logger, hadMixedMessages bool) *llmtypes.ContentResponse {
	if result == nil {
		if logger != nil {
			logger.Errorf("⚠️ [VERTEX] convertResponse received nil result")
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
		var sharedThoughtSignature string // For parallel tool calls, share thought signature across all

		if candidate.Content != nil {
			if logger != nil {
				logger.Debugf("🔍 [VERTEX] Candidate %d: Content is not nil, Parts count: %d", i, len(candidate.Content.Parts))
			}

			// Check candidate-level metadata for thought signature (might be at candidate level, not part level)
			if logger != nil {
				candidateJSON, _ := json.Marshal(candidate)
				var candidateMap map[string]interface{}
				if err := json.Unmarshal(candidateJSON, &candidateMap); err == nil {
					logger.Debugf("🔍 [VERTEX] Candidate %d keys: %v", i, getMapKeysForDebug(candidateMap))
					// Check for thought signature at candidate level
					if thoughtSig, ok := candidateMap["thoughtSignature"].(string); ok && thoughtSig != "" {
						sharedThoughtSignature = thoughtSig
						logger.Infof("✅ [VERTEX] Found thought signature at candidate level (length: %d)", len(thoughtSig))
					}
				}
			}

			// First pass: Extract thought signature from any part (for parallel calls, it might be in the first one)
			for j, part := range candidate.Content.Parts {
				if part.FunctionCall != nil {
					thoughtSig := extractThoughtSignature(part, logger)
					if thoughtSig != "" && sharedThoughtSignature == "" {
						sharedThoughtSignature = thoughtSig
						if logger != nil {
							logger.Infof("✅ [VERTEX] Found thought signature in part %d (function call: %s), will share with all parallel tool calls", j, part.FunctionCall.Name)
						}
					}
				} else if part.Text == "" {
					// Check empty text parts too
					thoughtSig := extractThoughtSignature(part, logger)
					if thoughtSig != "" && sharedThoughtSignature == "" {
						sharedThoughtSignature = thoughtSig
						if logger != nil {
							logger.Infof("✅ [VERTEX] Found thought signature in empty text part %d, will share with all parallel tool calls", j)
						}
					}
				}
			}

			// Second pass: Extract all tool calls and assign shared thought signature
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
					// Extract thought signature from this specific part first
					thoughtSignature := extractThoughtSignature(part, logger)
					// If not found in this part, use the shared one (for parallel calls)
					if thoughtSignature == "" && sharedThoughtSignature != "" {
						thoughtSignature = sharedThoughtSignature
						if logger != nil {
							logger.Debugf("🔍 [VERTEX] Using shared thought signature for tool call %s (from another part)", part.FunctionCall.Name)
						}
					}

					// 🔧 FIX: Generate ToolCallID for FunctionCall
					// Gemini's FunctionCall doesn't include an ID field, so we generate one.
					// This ID is used later when creating ToolCallResponse to match
					// the response to the original call. Gemini matches FunctionResponses
					// to FunctionCalls primarily by sequence/position, but the ID is still
					// used in NewPartFromFunctionResponse for proper association.
					toolCall := llmtypes.ToolCall{
						ID:               generateToolCallID(),
						Type:             "function",
						ThoughtSignature: thoughtSignature,
						FunctionCall: &llmtypes.FunctionCall{
							Name:      part.FunctionCall.Name,
							Arguments: convertArgumentsToString(part.FunctionCall.Args),
						},
					}
					toolCalls = append(toolCalls, toolCall)
					if logger != nil {
						if thoughtSignature != "" {
							logger.Debugf("🔍 [VERTEX] Candidate %d, Part %d: Created ToolCall - ID: %s, Name: %s, ThoughtSignature: present (length: %d)", i, j, toolCall.ID, toolCall.FunctionCall.Name, len(thoughtSignature))
						} else {
							logger.Debugf("🔍 [VERTEX] Candidate %d, Part %d: Created ToolCall - ID: %s, Name: %s, ThoughtSignature: missing", i, j, toolCall.ID, toolCall.FunctionCall.Name)
						}
					}
				} else {
					// Check for thought signature in text parts (for responses without function calls)
					// Gemini 3 Pro may return thought signature in the last part when there are no function calls
					if part.Text == "" {
						thoughtSignature := extractThoughtSignature(part, logger)
						if thoughtSignature != "" && logger != nil {
							logger.Debugf("🔍 [VERTEX] Candidate %d, Part %d: Found thought signature in empty text part", i, j)
						}
					} else if logger != nil {
						logger.Debugf("🔍 [VERTEX] Candidate %d, Part %d: No FunctionCall (Text: %q, Text length: %d)", i, j, part.Text, len(part.Text))
					}
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
			} else if finishReason == "UNEXPECTED_TOOL_CALL" {
				logger.Errorf("   ❌ FinishReason is UNEXPECTED_TOOL_CALL - This indicates a problem with tool call format:")
				logger.Errorf("      - Invalid thought signature format (most likely for Gemini 3 Pro)")
				logger.Errorf("      - Tool call structure doesn't match expected format")
				logger.Errorf("      - Thought signature may be incorrectly formatted or missing required fields")
				logger.Errorf("      - Check thought signature extraction and inclusion logs above")
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
		choice.GenerationInfo = utils.ExtractGenerationInfoFromVertexUsage(result.UsageMetadata)

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

// GenerateEmbeddings implements the llmtypes.EmbeddingModel interface
// Input can be a single string or a slice of strings
func (g *GoogleGenAIAdapter) GenerateEmbeddings(ctx context.Context, input interface{}, options ...llmtypes.EmbeddingOption) (*llmtypes.EmbeddingResponse, error) {
	// Parse embedding options
	opts := &llmtypes.EmbeddingOptions{
		Model: "text-embedding-004", // Default model (latest Vertex AI embedding model)
	}
	for _, opt := range options {
		opt(opts)
	}

	// Use provided model or default
	modelID := opts.Model
	if modelID == "" {
		modelID = "text-embedding-004"
	}

	// Convert input to slice of strings
	var inputTexts []string
	switch v := input.(type) {
	case string:
		// Validate single string input
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("input cannot be empty")
		}
		inputTexts = []string{v}
	case []string:
		// Array of strings input
		if len(v) == 0 {
			return nil, fmt.Errorf("input cannot be empty")
		}
		// Validate that no string in the array is empty
		for i, text := range v {
			if strings.TrimSpace(text) == "" {
				return nil, fmt.Errorf("input at index %d cannot be empty", i)
			}
		}
		inputTexts = v
	default:
		return nil, fmt.Errorf("input must be a string or []string, got %T", input)
	}

	// Convert strings to genai.Content format
	genaiContents := make([]*genai.Content, 0, len(inputTexts))
	for _, text := range inputTexts {
		genaiContents = append(genaiContents, &genai.Content{
			Parts: []*genai.Part{
				genai.NewPartFromText(text),
			},
		})
	}

	// Build EmbedContentConfig from options
	config := &genai.EmbedContentConfig{}

	// Add dimensions if specified (for text-embedding-004 and newer models)
	if opts.Dimensions != nil {
		dims := int32(*opts.Dimensions)
		config.OutputDimensionality = &dims
	}

	// Log input details if logger is available
	if g.logger != nil {
		g.logger.Debugf("Vertex AI GenerateEmbeddings INPUT - model: %s, input_count: %d, dimensions: %v",
			modelID, len(inputTexts), opts.Dimensions)
	}

	// Call Vertex AI EmbedContent API
	result, err := g.client.Models.EmbedContent(ctx, modelID, genaiContents, config)
	if err != nil {
		if g.logger != nil {
			g.logger.Errorf("Vertex AI GenerateEmbeddings ERROR - model: %s, error: %v", modelID, err)
		}
		return nil, fmt.Errorf("vertex ai generate embeddings: %w", err)
	}

	// Convert response from Vertex AI format to llmtypes format
	return convertEmbeddingResponse(result, modelID), nil
}

// convertEmbeddingResponse converts Vertex AI embedding response to llmtypes EmbeddingResponse
func convertEmbeddingResponse(result *genai.EmbedContentResponse, modelID string) *llmtypes.EmbeddingResponse {
	if result == nil {
		return &llmtypes.EmbeddingResponse{
			Embeddings: []llmtypes.Embedding{},
			Model:      modelID,
		}
	}

	embeddings := make([]llmtypes.Embedding, 0, len(result.Embeddings))
	for i, item := range result.Embeddings {
		// Vertex AI returns []float32 directly, so we can use it as-is
		embeddings = append(embeddings, llmtypes.Embedding{
			Index:     i,
			Embedding: item.Values,
			Object:    "embedding",
		})
	}

	response := &llmtypes.EmbeddingResponse{
		Embeddings: embeddings,
		Model:      modelID,
		Object:     "list",
	}

	// Extract usage information if available in metadata
	if result.Metadata != nil {
		// Note: Vertex AI EmbedContentMetadata may not have token usage info
		// We'll set usage if available, otherwise leave it nil
		// The metadata structure may vary, so we check what's available
	}

	return response
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
// modelID is passed to check if we're using Gemini 3 Pro (which requires thought signatures)
func (g *GoogleGenAIAdapter) convertMessageParts(parts []llmtypes.ContentPart, modelID string) []*genai.Part {
	genaiParts := make([]*genai.Part, 0)

	if g.logger != nil {
		g.logger.Infof("🔍 [GEMINI] convertMessageParts called with %d parts", len(parts))
	}

	// Track the first function call in this message (for parallel calls, only first needs thought signature)
	firstFunctionCallIndex := -1
	var sharedThoughtSignature string // Find thought signature from any tool call to share with ALL calls
	var toolCallCount int             // Count total tool calls in this message

	for i, part := range parts {
		if toolCall, ok := part.(llmtypes.ToolCall); ok {
			toolCallCount++
			if firstFunctionCallIndex == -1 {
				firstFunctionCallIndex = i
			}
			// Collect thought signature from any tool call (for parallel calls, they should share it)
			if toolCall.ThoughtSignature != "" && sharedThoughtSignature == "" {
				sharedThoughtSignature = toolCall.ThoughtSignature
				if g.logger != nil {
					g.logger.Infof("✅ [GEMINI] Found thought signature in tool call %s (index: %d, length: %d), will share with all %d tool calls",
						toolCall.FunctionCall.Name, i, len(toolCall.ThoughtSignature), toolCallCount)
				}
			}
		}
	}

	// Log summary of thought signature collection
	// CRITICAL: For Gemini 3 Pro, ALL tool calls MUST have thought signatures
	isGemini3Pro := strings.Contains(modelID, "gemini-3")
	if toolCallCount > 0 && g.logger != nil {
		if sharedThoughtSignature != "" {
			g.logger.Infof("✅ [GEMINI] Collected shared thought signature (length: %d) for %d tool calls", len(sharedThoughtSignature), toolCallCount)
		} else {
			if isGemini3Pro {
				g.logger.Errorf("❌ [GEMINI] CRITICAL ERROR: Found %d tool calls but NO thought signatures! Gemini 3 Pro REQUIRES thought signatures for all function calls", toolCallCount)
				g.logger.Errorf("   This will cause API errors. Check if thought signatures are being preserved from API responses.")
			} else {
				g.logger.Debugf("⚠️ [GEMINI] Found %d tool calls but NO thought signatures (not required for non-Gemini-3 models)", toolCallCount)
			}
		}
	}

	for i, part := range parts {
		// Log the actual type before switching
		if g.logger != nil {
			g.logger.Infof("🔍 [GEMINI] convertMessageParts: Processing part %d, type: %T", i, part)
		}

		// Use reflection to get the actual type name for debugging
		partType := fmt.Sprintf("%T", part)
		if g.logger != nil {
			g.logger.Infof("🔍 [GEMINI] Part type string: %s", partType)
		}

		// Try multiple type assertion approaches
		// First try direct assertion
		if toolCall, ok := part.(llmtypes.ToolCall); ok {
			if g.logger != nil {
				g.logger.Infof("🔍 [GEMINI] Matched ToolCall case (via assertion), ID=%s", toolCall.ID)
			}
			// Handle ToolCall
			if toolCall.FunctionCall == nil {
				if g.logger != nil {
					g.logger.Errorf("❌ [GEMINI] ToolCall at index %d has nil FunctionCall! ID: %s", i, toolCall.ID)
				}
				continue
			}
			argsMap := parseJSONObject(toolCall.FunctionCall.Arguments)
			if g.logger != nil {
				g.logger.Infof("🔍 [GEMINI] Converting ToolCall: ID=%s, Name=%s, Args length=%d",
					toolCall.ID, toolCall.FunctionCall.Name, len(toolCall.FunctionCall.Arguments))
			}
			genaiPart := genai.NewPartFromFunctionCall(toolCall.FunctionCall.Name, argsMap)
			if genaiPart == nil {
				if g.logger != nil {
					g.logger.Errorf("❌ [GEMINI] Failed to create genai.Part from ToolCall: ID=%s, Name=%s", toolCall.ID, toolCall.FunctionCall.Name)
				}
				continue
			}

			// Handle thought signature
			// CRITICAL: Gemini 3 Pro requires ALL function calls to have thought signatures
			// when sending them back in conversation history, not just the first one
			thoughtSignature := toolCall.ThoughtSignature
			if thoughtSignature == "" {
				// Use shared thought signature if this tool call doesn't have one
				if sharedThoughtSignature != "" {
					thoughtSignature = sharedThoughtSignature
					if g.logger != nil {
						g.logger.Infof("🔍 [GEMINI] Tool call %s (index %d) missing thought signature, using shared one (length: %d)",
							toolCall.FunctionCall.Name, i, len(sharedThoughtSignature))
					}
				} else {
					if g.logger != nil {
						g.logger.Errorf("❌ [GEMINI] Tool call %s (index %d) is missing thought signature AND no shared signature available!",
							toolCall.FunctionCall.Name, i)
						g.logger.Errorf("   This will cause Gemini 3 Pro to reject the request. Tool call ID: %s", toolCall.ID)
					}
				}
			} else {
				if g.logger != nil {
					g.logger.Debugf("✅ [GEMINI] Tool call %s (index %d) has its own thought signature (length: %d)",
						toolCall.FunctionCall.Name, i, len(thoughtSignature))
				}
			}

			// Inject thought signature into ALL function calls (required for Gemini 3 Pro)
			// CRITICAL: If we don't have a thought signature, we MUST fail for Gemini 3 Pro
			if thoughtSignature != "" {
				genaiPartJSON, err := json.Marshal(genaiPart)
				if err == nil {
					var partMap map[string]interface{}
					if err := json.Unmarshal(genaiPartJSON, &partMap); err == nil {
						partMap["thoughtSignature"] = thoughtSignature
						if partMap["extra_content"] == nil {
							partMap["extra_content"] = make(map[string]interface{})
						}
						extraContent := partMap["extra_content"].(map[string]interface{})
						if extraContent["google"] == nil {
							extraContent["google"] = make(map[string]interface{})
						}
						google := extraContent["google"].(map[string]interface{})
						google["thought_signature"] = thoughtSignature
						updatedJSON, err := json.Marshal(partMap)
						if err == nil {
							var updatedPart genai.Part
							if err := json.Unmarshal(updatedJSON, &updatedPart); err == nil {
								genaiPart = &updatedPart
								if g.logger != nil {
									g.logger.Debugf("✅ [GEMINI] Injected thought signature into tool call %s (index %d, length: %d)", toolCall.FunctionCall.Name, i, len(thoughtSignature))
								}
							}
						}
					}
				}
			} else {
				// CRITICAL: For Gemini 3 Pro, we MUST have thought signatures
				// Don't append the part if it's missing - this will cause API errors
				if g.logger != nil {
					g.logger.Errorf("❌ [GEMINI] CRITICAL: Tool call %s (index %d, ID: %s) is missing thought signature!",
						toolCall.FunctionCall.Name, i, toolCall.ID)
					g.logger.Errorf("   Cannot send this tool call to Gemini 3 Pro - it will be rejected by the API")
					g.logger.Errorf("   This indicates that thought signatures were not preserved from the original API response")
				}
				// Still append it - the API will reject it with a clear error message
				// This is better than silently dropping tool calls
			}

			genaiParts = append(genaiParts, genaiPart)
			continue
		}

		// Try ToolCallResponse assertion
		if toolResp, ok := part.(llmtypes.ToolCallResponse); ok {
			if g.logger != nil {
				g.logger.Infof("🔍 [GEMINI] Matched ToolCallResponse case (via assertion), ToolCallID=%s", toolResp.ToolCallID)
			}
			// Handle ToolCallResponse
			contentPreview := toolResp.Content
			if len(contentPreview) > 50 {
				contentPreview = contentPreview[:50] + "..."
			}
			if g.logger != nil {
				g.logger.Infof("🔍 [GEMINI] Converting ToolCallResponse: ToolCallID=%s, Name=%s, Content: %s",
					toolResp.ToolCallID, toolResp.Name, contentPreview)
			}
			responseMap := map[string]interface{}{
				"result": toolResp.Content,
			}
			genaiPart := genai.NewPartFromFunctionResponse(toolResp.ToolCallID, responseMap)
			if genaiPart == nil {
				if g.logger != nil {
					g.logger.Errorf("❌ [GEMINI] Failed to create genai.Part from ToolCallResponse: ToolCallID=%s, Name=%s", toolResp.ToolCallID, toolResp.Name)
				}
				continue
			}
			genaiParts = append(genaiParts, genaiPart)
			continue
		}

		// Fallback: Try JSON marshal/unmarshal for type conversion
		// This handles cases where types don't match due to package differences
		partTypeStr := fmt.Sprintf("%T", part)
		if strings.Contains(partTypeStr, "ToolCall") && !strings.Contains(partTypeStr, "Response") {
			// Try to convert via JSON
			jsonData, err := json.Marshal(part)
			if err == nil {
				var toolCall llmtypes.ToolCall
				if err := json.Unmarshal(jsonData, &toolCall); err == nil && toolCall.ID != "" && toolCall.FunctionCall != nil {
					if g.logger != nil {
						g.logger.Infof("🔍 [GEMINI] Converted ToolCall via JSON fallback, ID=%s, Name=%s", toolCall.ID, toolCall.FunctionCall.Name)
					}
					argsMap := parseJSONObject(toolCall.FunctionCall.Arguments)
					genaiPart := genai.NewPartFromFunctionCall(toolCall.FunctionCall.Name, argsMap)
					if genaiPart != nil {
						genaiParts = append(genaiParts, genaiPart)
						continue
					}
				}
			}
		}

		if strings.Contains(partTypeStr, "ToolCallResponse") {
			// Try to convert via JSON
			jsonData, err := json.Marshal(part)
			if err == nil {
				var toolResp llmtypes.ToolCallResponse
				if err := json.Unmarshal(jsonData, &toolResp); err == nil && toolResp.ToolCallID != "" {
					if g.logger != nil {
						g.logger.Infof("🔍 [GEMINI] Converted ToolCallResponse via JSON fallback, ToolCallID=%s, Name=%s", toolResp.ToolCallID, toolResp.Name)
					}
					responseMap := map[string]interface{}{
						"result": toolResp.Content,
					}
					genaiPart := genai.NewPartFromFunctionResponse(toolResp.ToolCallID, responseMap)
					if genaiPart != nil {
						genaiParts = append(genaiParts, genaiPart)
						continue
					}
				}
			}
		}

		// Switch statement for other types (TextContent, ImageContent, etc.)
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
						g.logger.Debugf("Image part created but InlineData is nil")
					}
				}
				genaiParts = append(genaiParts, imagePart)
			} else {
				if g.logger != nil {
					g.logger.Debugf("Failed to create image part from ImageContent")
				}
			}
		default:
			// Unknown part type - log it for debugging
			if g.logger != nil {
				g.logger.Errorf("❌ [GEMINI] convertMessageParts: Unknown part type at index %d: %T (value: %+v)", i, part, part)
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
		"error_type":    fmt.Sprintf("%T", err),
		"model_id":      modelID,
		"message_count": len(messages),
	}

	// Try to extract more error details by unwrapping
	errorChain := []string{err.Error()}
	currentErr := err
	for i := 0; i < 10; i++ { // Limit unwrapping depth
		// Try to unwrap the error
		if unwrapper, ok := currentErr.(interface{ Unwrap() error }); ok {
			unwrapped := unwrapper.Unwrap()
			if unwrapped == nil {
				break
			}
			errorChain = append(errorChain, unwrapped.Error())
			currentErr = unwrapped
		} else {
			break
		}
	}
	if len(errorChain) > 1 {
		errorInfo["error_chain"] = errorChain
		errorInfo["full_error_chain"] = strings.Join(errorChain, " -> ")
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
			g.logger.Debugf("⚠️ [REQUEST_ID: %s] PromptFeedback.BlockReason present: %q (Note: Safety blocks usually cause API errors, not empty content)", requestID, result.PromptFeedback.BlockReason)
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
		g.logger.Debugf("⚠️ [REQUEST_ID: %s] Failed to serialize response summary to JSON: %v", requestID, err)
	}

	// Try to log the complete raw response as JSON for maximum debugging
	// This captures everything including PromptFeedback, SafetyRatings, and any error details
	if resultJSON, err := json.MarshalIndent(result, "   ", "  "); err == nil {
		jsonStr := string(resultJSON)
		// For very large responses, truncate but keep important parts
		if len(jsonStr) > 10000 {
			// Keep first 5000 chars and last 5000 chars
			jsonStr = jsonStr[:5000] + "\n   ... (truncated, total length: " + fmt.Sprintf("%d", len(jsonStr)) + " bytes) ...\n   " + jsonStr[len(jsonStr)-5000:]
		}
		g.logger.Infof("🔍 [REQUEST_ID: %s] COMPLETE RAW VERTEX API RESPONSE (FULL JSON):\n   %s", requestID, jsonStr)
	} else {
		g.logger.Debugf("🔍 [REQUEST_ID: %s] Could not serialize complete response to JSON (may have unexported fields): %v", requestID, err)
	}
}

// WithResponseSchema returns a context with the ResponseSchema set
// This allows structured output generation with schema validation
func WithResponseSchema(ctx context.Context, schema *genai.Schema) context.Context {
	return context.WithValue(ctx, ResponseSchemaKey, schema)
}

// WithResponseSchemaFromJSON accepts a JSON Schema map and converts it to genai.Schema
// This allows consumers to avoid importing genai directly
func WithResponseSchemaFromJSON(ctx context.Context, jsonSchema map[string]interface{}) context.Context {
	if jsonSchema == nil {
		return ctx
	}
	schema := convertJSONSchemaToSchema(jsonSchema)
	if schema == nil {
		return ctx
	}
	return WithResponseSchema(ctx, schema)
}

// createImagePart creates a genai.Part from ImageContent
func (g *GoogleGenAIAdapter) createImagePart(img llmtypes.ImageContent) *genai.Part {
	if img.SourceType == "base64" {
		// Decode base64 string to bytes
		imageBytes, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			if g.logger != nil {
				g.logger.Debugf("Failed to decode base64 image: %v", err)
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
		// Note: context is not available here, use background context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		imageBytes, mimeType, err := g.fetchImageFromURL(ctx, img.Data)
		if err != nil {
			if g.logger != nil {
				g.logger.Debugf("Failed to fetch image from URL %s: %v", img.Data, err)
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
		g.logger.Debugf("Invalid image source type: %s", img.SourceType)
	}
	return nil
}

// fetchImageFromURL fetches an image from a URL and returns the bytes and MIME type
func (g *GoogleGenAIAdapter) fetchImageFromURL(ctx context.Context, url string) ([]byte, string, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Fetch the image with context
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}
	resp, err := client.Do(req)
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

// extractThoughtSignature extracts thought signature from genai.Part's ExtraContent
// Thought signatures are in extra_content.google.thought_signature according to Gemini API docs
func extractThoughtSignature(part *genai.Part, logger interfaces.Logger) string {
	if part == nil {
		return ""
	}

	// Marshal the part to JSON to access ExtraContent fields
	partJSON, err := json.Marshal(part)
	if err != nil {
		if logger != nil {
			logger.Debugf("🔍 [VERTEX] Failed to marshal part for thought signature extraction: %v", err)
		}
		return ""
	}

	// Unmarshal into a map to access nested fields
	var partMap map[string]interface{}
	if err := json.Unmarshal(partJSON, &partMap); err != nil {
		if logger != nil {
			logger.Debugf("🔍 [VERTEX] Failed to unmarshal part JSON: %v", err)
		}
		return ""
	}

	// Check for thoughtSignature directly (genai SDK exposes it as a top-level field)
	if thoughtSig, ok := partMap["thoughtSignature"].(string); ok && thoughtSig != "" {
		if logger != nil {
			logger.Infof("✅ [VERTEX] Extracted thought signature directly (length: %d)", len(thoughtSig))
		}
		return thoughtSig
	}

	// Debug: Log the part structure to see what fields are available
	if logger != nil {
		// Check if extra_content exists
		if extraContent, exists := partMap["extra_content"]; exists {
			logger.Debugf("🔍 [VERTEX] Found extra_content: %+v", extraContent)
		} else {
			logger.Debugf("🔍 [VERTEX] No extra_content field found in part. Available fields: %v", getMapKeysForDebug(partMap))
		}
	}

	// Navigate to extra_content.google.thought_signature (fallback for API response format)
	if extraContent, ok := partMap["extra_content"].(map[string]interface{}); ok {
		if logger != nil {
			logger.Debugf("🔍 [VERTEX] extra_content keys: %v", getMapKeysForDebug(extraContent))
		}
		if google, ok := extraContent["google"].(map[string]interface{}); ok {
			if logger != nil {
				logger.Debugf("🔍 [VERTEX] google keys: %v", getMapKeysForDebug(google))
			}
			if thoughtSig, ok := google["thought_signature"].(string); ok && thoughtSig != "" {
				if logger != nil {
					logger.Infof("✅ [VERTEX] Extracted thought signature from extra_content.google (length: %d)", len(thoughtSig))
				}
				return thoughtSig
			} else if logger != nil {
				logger.Debugf("🔍 [VERTEX] thought_signature not found in google map")
			}
		} else if logger != nil {
			logger.Debugf("🔍 [VERTEX] google not found in extra_content")
		}
	}

	return ""
}

// getMapKeysForDebug returns the keys of a map as a slice of strings (for debugging)
func getMapKeysForDebug(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// generateToolCallID generates a unique ID for tool calls
// In a real implementation, you might want to use a proper ID generator
var toolCallCounter int64 = 0

func generateToolCallID() string {
	toolCallCounter++
	return fmt.Sprintf("call_%d", toolCallCounter)
}
