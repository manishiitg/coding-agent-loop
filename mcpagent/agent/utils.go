// utils.go
//
// This file contains shared helper functions for the Agent, including system prompt construction, tool choice conversion, string truncation, and usage metrics extraction.
//
// Exported:
//   - BuildSystemPrompt
//   - ConvertToolChoice
//   - TruncateString
//   - extractUsageMetrics
//   - castToInt

package mcpagent

import (
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetDefaultMaxTurns returns the default max turns for a given agent mode.
func GetDefaultMaxTurns(mode AgentMode) int {
	// All agents use the same default max turns
	return 50
}

// ConvertToolChoice converts a tool choice string to *llmtypes.ToolChoice.
// Returns nil if choice is empty, otherwise returns a properly constructed ToolChoice.
func ConvertToolChoice(choice string) *llmtypes.ToolChoice {
	if choice == "" {
		return nil
	}

	switch choice {
	case "auto", "none", "required":
		return &llmtypes.ToolChoice{Type: choice}
	default:
		// Specific tool name - create function-specific tool choice
		return &llmtypes.ToolChoice{
			Type:     "function",
			Function: &llmtypes.FunctionName{Name: choice},
		}
	}
}

// TruncateString truncates a string to a maximum length, adding ellipsis if needed.
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractUsageMetrics extracts token usage metrics from an LLM response.
// It prioritizes the unified Usage field, falling back to GenerationInfo if needed.
func extractUsageMetrics(resp *llmtypes.ContentResponse) observability.UsageMetrics {
	if resp == nil || len(resp.Choices) == 0 {
		return observability.UsageMetrics{}
	}

	m := observability.UsageMetrics{Unit: "TOKENS"}

	// Priority 1: Use unified Usage field (if available)
	if resp.Usage != nil {
		m.InputTokens = resp.Usage.InputTokens
		m.OutputTokens = resp.Usage.OutputTokens
		m.TotalTokens = resp.Usage.TotalTokens

		// If we got actual token usage from unified field, return it
		if m.InputTokens > 0 || m.OutputTokens > 0 || m.TotalTokens > 0 {
			// Ensure total is calculated if not provided
			if m.TotalTokens == 0 {
				m.TotalTokens = m.InputTokens + m.OutputTokens
			}
			return m
		}
	}

	// Priority 2: Fall back to GenerationInfo (for backward compatibility)
	info := resp.Choices[0].GenerationInfo
	if info != nil {
		// Extract input tokens (check multiple naming conventions)
		if info.InputTokens != nil {
			m.InputTokens = *info.InputTokens
		} else if info.InputTokensCap != nil {
			m.InputTokens = *info.InputTokensCap
		} else if info.PromptTokens != nil {
			m.InputTokens = *info.PromptTokens
		} else if info.PromptTokensCap != nil {
			m.InputTokens = *info.PromptTokensCap
		}

		// Extract output tokens (check multiple naming conventions)
		if info.OutputTokens != nil {
			m.OutputTokens = *info.OutputTokens
		} else if info.OutputTokensCap != nil {
			m.OutputTokens = *info.OutputTokensCap
		} else if info.CompletionTokens != nil {
			m.OutputTokens = *info.CompletionTokens
		} else if info.CompletionTokensCap != nil {
			m.OutputTokens = *info.CompletionTokensCap
		}

		// Extract total tokens (check multiple naming conventions)
		if info.TotalTokens != nil {
			m.TotalTokens = *info.TotalTokens
		} else if info.TotalTokensCap != nil {
			m.TotalTokens = *info.TotalTokensCap
		}
	}

	// If we got actual token usage, return it
	if m.InputTokens > 0 || m.OutputTokens > 0 || m.TotalTokens > 0 {
		// Ensure total is calculated if not provided
		if m.TotalTokens == 0 {
			m.TotalTokens = m.InputTokens + m.OutputTokens
		}
		return m
	}

	// Fallback: Estimate tokens based on content length
	// This is a rough approximation when actual usage is not available
	content := resp.Choices[0].Content
	if content != "" {
		// Rough estimation: 1 token ≈ 4 characters for English text
		estimatedTokens := len(content) / 4
		m.OutputTokens = estimatedTokens
		m.TotalTokens = estimatedTokens

		// For input tokens, we'd need the prompt length, but we don't have it here
		// This is a limitation of the current LangChain integration
	}

	return m
}

// extractUsageMetricsWithMessages extracts token usage with improved input token estimation
func extractUsageMetricsWithMessages(resp *llmtypes.ContentResponse, messages []llmtypes.MessageContent) observability.UsageMetrics {
	// Get base usage metrics
	usage := extractUsageMetrics(resp)

	// If we don't have input tokens, estimate them from conversation history
	if usage.InputTokens == 0 {
		usage.InputTokens = estimateInputTokens(messages)
		// Recalculate total if we now have both input and output
		if usage.OutputTokens > 0 {
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
		}
	}

	return usage
}

// estimateInputTokens estimates input tokens from conversation messages
func estimateInputTokens(messages []llmtypes.MessageContent) int {
	if len(messages) == 0 {
		return 0
	}

	totalChars := 0
	for _, msg := range messages {
		for _, part := range msg.Parts {
			if textPart, ok := part.(llmtypes.TextContent); ok {
				totalChars += len(textPart.Text)
			}
		}
	}

	// Rough estimation: 1 token ≈ 4 characters for English text
	// Add some overhead for system prompts and formatting
	estimatedTokens := (totalChars / 4) + 50 // Add 50 tokens for system overhead
	return estimatedTokens
}
