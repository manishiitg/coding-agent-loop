package shared

import (
	"fmt"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// FormatConversationHistory converts a slice of llms.MessageContent into a
// markdown-formatted history string suitable for prompt/template variables.
// - Skips system messages
// - Labels sections by role (Human/Assistant/Tool)
// - Renders text, tool calls, and tool call responses
func FormatConversationHistory(conversationHistory []llms.MessageContent) string {
	var result strings.Builder

	for _, message := range conversationHistory {
		if message.Role == llms.ChatMessageTypeSystem {
			continue
		}

		switch message.Role {
		case llms.ChatMessageTypeHuman:
			result.WriteString("## Human Message\n")
		case llms.ChatMessageTypeAI:
			result.WriteString("## Assistant Response\n")
		case llms.ChatMessageTypeTool:
			result.WriteString("## Tool Response\n")
		default:
			result.WriteString("## Message\n")
		}

		for _, part := range message.Parts {
			switch p := part.(type) {
			case llms.TextContent:
				result.WriteString(p.Text)
				result.WriteString("\n\n")
			case llms.ToolCall:
				result.WriteString("### Tool Call\n")
				result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.FunctionCall.Name))
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ID))
				if p.FunctionCall.Arguments != "" {
					result.WriteString(fmt.Sprintf("**Arguments:** %s\n", p.FunctionCall.Arguments))
				}
				result.WriteString("\n")
			case llms.ToolCallResponse:
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
