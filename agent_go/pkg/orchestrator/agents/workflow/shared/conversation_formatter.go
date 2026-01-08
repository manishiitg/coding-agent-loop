package shared

import (
	"fmt"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
	"strings"
)

// FormatConversationHistory converts a slice of llmtypes.MessageContent into a
// markdown-formatted history string suitable for prompt/template variables.
// - Skips system messages
// - Labels sections by role (Human/Assistant/Tool)
// - Renders text, tool calls, and tool call responses
func FormatConversationHistory(conversationHistory []llmtypes.MessageContent) string {
	var result strings.Builder

	for _, message := range conversationHistory {
		if message.Role == llmtypes.ChatMessageTypeSystem {
			continue
		}

		switch message.Role {
		case llmtypes.ChatMessageTypeHuman:
			result.WriteString("## Human Message\n")
		case llmtypes.ChatMessageTypeAI:
			result.WriteString("## Assistant Response\n")
		case llmtypes.ChatMessageTypeTool:
			result.WriteString("## Tool Response\n")
		default:
			result.WriteString("## Message\n")
		}

		for _, part := range message.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				result.WriteString(p.Text)
				result.WriteString("\n\n")
			case llmtypes.ToolCall:
				result.WriteString("### Tool Call\n")
				result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.FunctionCall.Name))
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ID))
				if p.FunctionCall.Arguments != "" {
					result.WriteString(fmt.Sprintf("**Arguments:** %s\n", p.FunctionCall.Arguments))
				}
				result.WriteString("\n")
			case llmtypes.ToolCallResponse:
				result.WriteString("### Tool Response\n")
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ToolCallID))
				if p.Name != "" {
					result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.Name))
				}
				result.WriteString(fmt.Sprintf("**Response:** %s\n", p.Content))
				result.WriteString("\n")
			default:
				result.WriteString(fmt.Sprintf("**Unknown Content Type:** %T\n", p))
			}
		}
		result.WriteString("---\n\n")
	}

	return result.String()
}

// FormatHistoryForLearning converts conversation history with smart truncation:
// 1. Recency Bias: Older messages are truncated more heavily than recent ones.
// 2. Tool Output Truncation: Large tool outputs (like read_file) are summarized.
// 3. Critical Data Preservation: 'write_workspace_file' inputs (scripts/code) are preserved.
func FormatHistoryForLearning(conversationHistory []llmtypes.MessageContent) string {
	var result strings.Builder
	totalMsgs := len(conversationHistory)
	// Define "Recent" as the last 20% of messages or at least the last 4 messages
	recentThreshold := totalMsgs - (totalMsgs / 5)
	if totalMsgs < 5 {
		recentThreshold = 0 // Treat all as recent if short
	} else if totalMsgs-recentThreshold < 4 {
		recentThreshold = totalMsgs - 4
		if recentThreshold < 0 {
			recentThreshold = 0
		}
	}

	for i, message := range conversationHistory {
		if message.Role == llmtypes.ChatMessageTypeSystem {
			continue
		}

		isRecent := i >= recentThreshold

		// Header
		switch message.Role {
		case llmtypes.ChatMessageTypeHuman:
			result.WriteString("## Human Message\n")
		case llmtypes.ChatMessageTypeAI:
			result.WriteString("## Assistant Response\n")
		case llmtypes.ChatMessageTypeTool:
			result.WriteString("## Tool Response\n")
		default:
			result.WriteString("## Message\n")
		}

		for _, part := range message.Parts {
			switch p := part.(type) {
			case llmtypes.TextContent:
				result.WriteString(p.Text)
				result.WriteString("\n\n")

			case llmtypes.ToolCall:
				result.WriteString("### Tool Call\n")
				result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.FunctionCall.Name))
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ID))
				
				// PRESERVE INPUTS: meaningful truncation for inputs is risky as we need the code.
				// However, if we really needed to, we could truncate 'content' arg for non-write tools.
				// For now, we print full arguments to ensure we capture the scripts the agent wrote.
				if p.FunctionCall.Arguments != "" {
					result.WriteString(fmt.Sprintf("**Arguments:** %s\n", p.FunctionCall.Arguments))
				}
				result.WriteString("\n")

			case llmtypes.ToolCallResponse:
				result.WriteString("### Tool Response\n")
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ToolCallID))
				if p.Name != "" {
					result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.Name))
				}

				// SMART TRUNCATION
				content := p.Content
				limit := 200 // Default: Heavy truncation for older history
				
				if isRecent {
					limit = 2000 // Recent: Light truncation (enough for error traces)
				}

				if len(content) > limit {
					// Keep the head and a tiny bit of tail (often contains error summaries)
					head := content[:limit]
					result.WriteString(fmt.Sprintf("**Response:** %s\n... [TRUNCATED %d chars] ...\n", head, len(content)-limit))
				} else {
					result.WriteString(fmt.Sprintf("**Response:** %s\n", content))
				}
				result.WriteString("\n")

			default:
				result.WriteString(fmt.Sprintf("**Unknown Content Type:** %T\n", p))
			}
		}
		result.WriteString("---\n\n")
	}

	return result.String()
}
