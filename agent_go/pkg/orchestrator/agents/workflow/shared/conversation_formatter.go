package shared

import (
	"fmt"
	"strings"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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

// FormatHistoryForLearningAggressive converts conversation history with very aggressive truncation
// specifically designed to reduce learning agent costs.
//
// Problem: Learning agents receive full execution history which can be 50K-200K+ tokens for complex steps,
// leading to extremely high input costs. This function reduces costs by 70-90% while preserving essential patterns.
//
// Strategy:
// 1. Limits to only the most recent N messages (default: 15 messages) - older messages are completely dropped
// 2. More aggressive tool response truncation - recent messages get 1000 chars, older get 100 chars
// 3. Preserves critical write_workspace_file inputs - these contain code/scripts that learning agents need
//
// Rationale:
// - Recent messages (last 8) contain the most relevant patterns and outcomes
// - Older messages are less critical for learning extraction
// - Write operations must be preserved as they show what the agent actually created
// - Tool responses can be heavily truncated as they're often verbose file contents or logs
//
// Expected impact: Reduces learning agent input costs from 50K-200K tokens to 10K-15K tokens per call.
func FormatHistoryForLearningAggressive(conversationHistory []llmtypes.MessageContent) string {
	const (
		// maxMessagesToInclude: Only process the last N messages to dramatically reduce token count.
		// For steps with 100+ messages, this alone saves 85% of tokens.
		// 15 messages is enough to capture the final execution pattern and outcome.
		maxMessagesToInclude = 15

		// recentMessagesFull: The last N messages get more detail (1000 char tool responses).
		// These contain the most relevant information: final tool calls, validation results, errors.
		// 8 messages typically covers the last 2-3 turns which is where success/failure patterns emerge.
		recentMessagesFull = 8

		// recentToolLimit: Tool response size limit for recent messages (1000 chars).
		// This is enough to capture error messages, validation results, and key outputs without
		// including massive file contents that inflate token counts.
		recentToolLimit = 1000

		// oldToolLimit: Tool response size limit for older messages (100 chars).
		// Very aggressive truncation for older messages since they're less critical.
		// Just enough to identify what tool was called and basic success/failure.
		oldToolLimit = 100
	)

	var result strings.Builder
	totalMsgs := len(conversationHistory)

	// Calculate which messages to include (only the most recent ones)
	// This is the primary cost reduction mechanism: drop all older messages entirely.
	startIdx := 0
	if totalMsgs > maxMessagesToInclude {
		startIdx = totalMsgs - maxMessagesToInclude
		// Inform the learning agent that truncation occurred so it understands context
		result.WriteString(fmt.Sprintf("_Note: Showing only the last %d messages of %d total messages. Older messages were truncated to reduce costs._\n\n", maxMessagesToInclude, totalMsgs))
	}

	// Calculate threshold for "recent" messages that get more detail
	// Recent messages (last 8) get 1000 char tool responses, older get 100 chars
	recentThreshold := totalMsgs - recentMessagesFull
	// Ensure recent threshold doesn't go below startIdx (in case totalMsgs < maxMessagesToInclude)
	if recentThreshold < startIdx {
		recentThreshold = startIdx
	}

	// Process only the selected messages (last N messages)
	for i := startIdx; i < totalMsgs; i++ {
		message := conversationHistory[i]
		// Skip system messages - they're not part of the execution history we want to learn from
		if message.Role == llmtypes.ChatMessageTypeSystem {
			continue
		}

		// Determine if this message is "recent" (gets more detail) or "old" (heavily truncated)
		isRecent := i >= recentThreshold

		// Add message type header for clarity
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
				// Text content (human messages, assistant responses) - keep full text
				// These are typically short and important for understanding context
				text := p.Text
				result.WriteString(text)
				result.WriteString("\n\n")

			case llmtypes.ToolCall:
				// Tool calls show what actions the agent took
				result.WriteString("### Tool Call\n")
				result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.FunctionCall.Name))
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ID))

				// CRITICAL: Preserve write_workspace_file arguments fully (they contain code/scripts)
				// These are essential for learning agents to understand what was created.
				// Other tool arguments can be truncated more aggressively.
				if p.FunctionCall.Arguments != "" {
					args := p.FunctionCall.Arguments
					// Preserve write_workspace_file content (critical for learning extraction)
					// Learning agents need to see the actual code/scripts that were written
					if p.FunctionCall.Name == "write_workspace_file" || strings.Contains(p.FunctionCall.Name, "write") {
						// Keep full arguments for write operations - this is the most important data
						result.WriteString(fmt.Sprintf("**Arguments:** %s\n", args))
					} else {
						// Truncate other tool arguments to 500 chars
						// Tool arguments for read/list operations can be very long (file paths, etc.)
						// 500 chars is enough to understand what was requested without bloating tokens
						maxArgsLen := 500
						if len(args) > maxArgsLen {
							result.WriteString(fmt.Sprintf("**Arguments:** %s... [TRUNCATED %d chars]\n", args[:maxArgsLen], len(args)-maxArgsLen))
						} else {
							result.WriteString(fmt.Sprintf("**Arguments:** %s\n", args))
						}
					}
				}
				result.WriteString("\n")

			case llmtypes.ToolCallResponse:
				// Tool responses often contain massive file contents or verbose logs
				// This is where most token bloat occurs - aggressive truncation is essential
				result.WriteString("### Tool Response\n")
				result.WriteString(fmt.Sprintf("**Tool ID:** %s\n", p.ToolCallID))
				if p.Name != "" {
					result.WriteString(fmt.Sprintf("**Tool Name:** %s\n", p.Name))
				}

				// Apply different truncation limits based on message recency
				// Recent messages: 1000 chars (enough for error traces, validation results)
				// Older messages: 100 chars (just enough to see success/failure)
				content := p.Content
				limit := oldToolLimit
				if isRecent {
					limit = recentToolLimit
				}

				if len(content) > limit {
					// Keep head only - most important info (errors, results) is usually at the start
					// File contents and logs are typically less critical for learning extraction
					head := content[:limit]
					result.WriteString(fmt.Sprintf("**Response:** %s... [TRUNCATED %d chars]\n", head, len(content)-limit))
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
