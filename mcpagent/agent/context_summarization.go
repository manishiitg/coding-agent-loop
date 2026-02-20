// context_summarization.go
//
// This file contains the context summarization logic for reducing conversation history
// when max turns is reached. It implements the "Summarize When Needed" pattern from
// context engineering best practices.
//
// The summarization process:
// 1. Splits messages into "old" (to summarize) and "recent" (to keep intact)
// 2. Generates a summary of old messages using LLM
// 3. Rebuilds the message array with: system prompt + summary + recent messages

package mcpagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

const (
	// DefaultSummaryKeepLastMessages is the default number of recent messages to keep
	// when summarizing. This is roughly 2 turns (each turn = user + assistant + tool results)
	DefaultSummaryKeepLastMessages = 4

	// DefaultSummarizationCooldownTurns is the number of turns to wait before allowing
	// another summarization. This prevents repeated summarization loops.
	DefaultSummarizationCooldownTurns = 3
)

// summarizeConversationHistory summarizes old conversation messages using LLM
// Returns: (summary, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, response, error)
func summarizeConversationHistory(a *Agent, ctx context.Context, oldMessages []llmtypes.MessageContent) (string, int, int, int, int, int, *llmtypes.ContentResponse, error) {
	v2Logger := a.Logger

	// Build a text representation of old messages for summarization
	conversationText := buildConversationTextForSummarization(oldMessages)

	// Create summarization prompt
	summaryPrompt := buildSummarizationPrompt()

	// Create messages for summarization LLM call
	summaryMessages := []llmtypes.MessageContent{
		{
			Role: llmtypes.ChatMessageTypeSystem,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: summaryPrompt},
			},
		},
		{
			Role: llmtypes.ChatMessageTypeHuman,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{Text: conversationText},
			},
		},
	}

	// Call LLM to generate summary
	summaryOpts := []llmtypes.CallOption{
		llmtypes.WithTemperature(0), // Temperature 0 for deterministic summaries
	}

	v2Logger.Info("📊 [CONTEXT_SUMMARIZATION] Generating conversation summary via LLM",
		loggerv2.Int("old_messages_count", len(oldMessages)),
		loggerv2.Int("conversation_text_length", len(conversationText)),
		loggerv2.String("model_id", a.ModelID))

	resp, _, err := GenerateContentWithRetry(a, ctx, summaryMessages, summaryOpts, 0)
	if err != nil {
		return "", 0, 0, 0, 0, 0, nil, fmt.Errorf("failed to generate conversation summary: %w", err)
	}

	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Content == "" {
		return "", 0, 0, 0, 0, 0, nil, fmt.Errorf("empty summary generated")
	}

	summary := resp.Choices[0].Content

	// Extract token usage from response
	var promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens int
	if resp.Usage != nil {
		promptTokens = resp.Usage.InputTokens
		completionTokens = resp.Usage.OutputTokens
		totalTokens = resp.Usage.TotalTokens
		// If total is 0, calculate it
		if totalTokens == 0 {
			totalTokens = promptTokens + completionTokens
		}
		// Extract cache tokens
		if resp.Usage.CacheTokens != nil {
			cacheTokens = *resp.Usage.CacheTokens
		}
		// Extract reasoning tokens
		if resp.Usage.ReasoningTokens != nil {
			reasoningTokens = *resp.Usage.ReasoningTokens
		}
	}

	// Fallback to GenerationInfo for cache/reasoning tokens if not in Usage
	if (cacheTokens == 0 || reasoningTokens == 0) && len(resp.Choices) > 0 && resp.Choices[0].GenerationInfo != nil {
		genInfo := resp.Choices[0].GenerationInfo
		if cacheTokens == 0 && genInfo.CachedContentTokens != nil {
			cacheTokens = *genInfo.CachedContentTokens
		}
		if reasoningTokens == 0 && genInfo.ReasoningTokens != nil {
			reasoningTokens = *genInfo.ReasoningTokens
		}
	}

	v2Logger.Info("✅ [CONTEXT_SUMMARIZATION] Conversation summary generated successfully",
		loggerv2.Int("summary_length_chars", len(summary)),
		loggerv2.Int("prompt_tokens", promptTokens),
		loggerv2.Int("completion_tokens", completionTokens),
		loggerv2.Int("total_tokens", totalTokens),
		loggerv2.Int("cache_tokens", cacheTokens),
		loggerv2.Int("reasoning_tokens", reasoningTokens))

	return summary, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, resp, nil
}

// buildConversationTextForSummarization converts messages to a text format for summarization
func buildConversationTextForSummarization(messages []llmtypes.MessageContent) string {
	var parts []string

	for i, msg := range messages {
		// Skip system messages in the old history (they'll be preserved separately)
		if msg.Role == llmtypes.ChatMessageTypeSystem {
			continue
		}

		roleLabel := getRoleLabel(msg.Role)
		content := extractMessageContent(msg)

		if content != "" {
			parts = append(parts, fmt.Sprintf("[Turn %d] %s: %s", i+1, roleLabel, content))
		}
	}

	return strings.Join(parts, "\n\n")
}

// getRoleLabel returns a human-readable label for a message role
func getRoleLabel(role llmtypes.ChatMessageType) string {
	switch role {
	case llmtypes.ChatMessageTypeHuman:
		return "User"
	case llmtypes.ChatMessageTypeAI:
		return "Assistant"
	case llmtypes.ChatMessageTypeTool:
		return "Tool"
	default:
		return string(role)
	}
}

// extractMessageContent extracts text content from a message
func extractMessageContent(msg llmtypes.MessageContent) string {
	var parts []string

	for _, part := range msg.Parts {
		switch p := part.(type) {
		case llmtypes.TextContent:
			if p.Text != "" {
				parts = append(parts, p.Text)
			}
		case llmtypes.ToolCall:
			if p.FunctionCall != nil {
				parts = append(parts, fmt.Sprintf("Tool call: %s(%s)", p.FunctionCall.Name, p.FunctionCall.Arguments))
			}
		case llmtypes.ToolCallResponse:
			// For tool responses, include the full content
			// This helps the summarization LLM understand the complete context
			parts = append(parts, fmt.Sprintf("Tool result (%s): %s", p.Name, p.Content))
		}
	}

	return strings.Join(parts, " ")
}

// buildSummarizationPrompt creates the prompt for the summarization LLM call
func buildSummarizationPrompt() string {
	return `You are an expert conversation summarizer specializing in preserving critical context for AI agents. Your task is to create a structured summary of the conversation history that allows the session to continue seamlessly.

## OUTPUT FORMAT

You MUST produce output in this exact format:

---
This session is being continued from a previous conversation that ran out of context. The conversation is summarized below:

**Analysis:**
Let me analyze the conversation chronologically:
[Provide a chronological narrative of what happened in the conversation - what the user asked, what the assistant did, key findings, decisions made, and outcomes]

**Summary:**

1. **Primary Request and Intent:**
   - [The main goal(s) the user is trying to accomplish]
   - [Any sub-tasks or related requests]

2. **Key Technical Concepts:**
   - [Important technical terms, patterns, or concepts discussed]
   - [Architecture decisions or design patterns mentioned]

3. **Files and Code Sections:**
   - [List all file paths mentioned with brief descriptions of what they contain or what was done to them]
   - [Include line numbers if specific code sections were referenced]
   - [Note any code snippets that were written or modified]

4. **Errors and Fixes:**
   - [Any errors encountered and how they were resolved]
   - [Root causes identified]
   - [Solutions implemented]

5. **Problem Solving:**
   - [Key insights or discoveries made during the conversation]
   - [Approaches that worked or didn't work]

6. **All User Messages:**
   - [List all user messages/requests chronologically as direct quotes or paraphrases]

7. **Pending Tasks:**
   - [Any tasks that were mentioned but not yet completed]
   - [TODOs or follow-up items]

8. **Current Work:**
   - [What was being worked on when the summary was created]
   - [The state of that work (in progress, blocked, etc.)]

9. **Optional Next Step:**
   - [Suggested next action to continue the work]
   - [Direct quote from user's most recent request if relevant]

Please continue the conversation from where we left it off without asking the user any further questions. Continue with the last task that you were asked to work on.
---

## CRITICAL PRESERVATION REQUIREMENTS

- Preserve ALL file paths, function names, tool names, and technical references exactly
- Include specific values: numbers, IDs, dates, measurements
- Maintain tool call/response relationships
- Note any errors and their resolutions
- Keep track of user preferences and constraints
- Include context about any files created or modified

## INSTRUCTIONS

Create a summary following the exact format above. The summary should be self-contained and allow an AI agent to continue the conversation with full context. Be thorough but concise - include all critical information while avoiding unnecessary verbosity.`
}

// findSafeSplitPoint finds a safe split point that doesn't break tool call/response pairs
// It works backwards from the desired split point to ensure tool calls and their responses stay together
func findSafeSplitPoint(messages []llmtypes.MessageContent, desiredSplitIndex int) int {
	if desiredSplitIndex <= 0 {
		return 0
	}

	// Start from desired split point and work backwards
	// We need to ensure that any tool responses in the "keep" section have their tool calls included
	splitIndex := desiredSplitIndex

	// Scan backwards from splitIndex to find any tool responses that need their tool calls
	for i := splitIndex; i < len(messages); i++ {
		msg := messages[i]

		// If this is a tool message (tool response), we must include its tool call
		if msg.Role == llmtypes.ChatMessageTypeTool {
			// Find the assistant message that called this tool (should be before this tool message)
			for j := i - 1; j >= 0; j-- {
				prevMsg := messages[j]
				if prevMsg.Role == llmtypes.ChatMessageTypeAI {
					// Check if this assistant message has tool calls
					hasToolCalls := false
					for _, part := range prevMsg.Parts {
						if _, ok := part.(llmtypes.ToolCall); ok {
							hasToolCalls = true
							break
						}
					}
					if hasToolCalls {
						// Found the tool call - if it's before splitIndex, move splitIndex back
						if j < splitIndex {
							splitIndex = j
						}
						break
					}
				}
				// Stop if we hit a user message (start of a turn)
				if prevMsg.Role == llmtypes.ChatMessageTypeHuman {
					break
				}
			}
		}
	}

	// Now check: if splitIndex points to an assistant message with tool calls,
	// ensure all its tool responses are in the "keep" section
	if splitIndex < len(messages) {
		msg := messages[splitIndex]
		if msg.Role == llmtypes.ChatMessageTypeAI {
			hasToolCalls := false
			for _, part := range msg.Parts {
				if _, ok := part.(llmtypes.ToolCall); ok {
					hasToolCalls = true
					break
				}
			}
			if hasToolCalls {
				// This assistant has tool calls - check if all tool responses are included
				// Look forward for tool responses
				for j := splitIndex + 1; j < len(messages); j++ {
					nextMsg := messages[j]
					if nextMsg.Role == llmtypes.ChatMessageTypeTool {
						// Tool response is in keep section, good
						continue
					}
					// If we hit another assistant message, we've passed all tool responses
					if nextMsg.Role == llmtypes.ChatMessageTypeAI {
						break
					}
				}
			}
		}
	}

	return splitIndex
}

// ensureToolCallResponseIntegrity ensures that if we split at splitIndex, we don't break tool call/response pairs
// Specifically: if the last message in old section is a tool call, all its responses must be in old section
// If the first message in recent section is a tool response, its tool call must be in recent section
func ensureToolCallResponseIntegrity(messages []llmtypes.MessageContent, splitIndex int) int {
	if splitIndex <= 0 || splitIndex >= len(messages) {
		return splitIndex
	}

	// Check 1: If the last message in old section (splitIndex-1) is an assistant with tool calls,
	// ensure all its tool responses are also in the old section
	if splitIndex > 0 {
		lastOldMsg := messages[splitIndex-1]
		if lastOldMsg.Role == llmtypes.ChatMessageTypeAI {
			hasToolCalls := false
			for _, part := range lastOldMsg.Parts {
				if _, ok := part.(llmtypes.ToolCall); ok {
					hasToolCalls = true
					break
				}
			}
			if hasToolCalls {
				// This tool call is in old section - find all its tool responses
				// Tool responses should come immediately after the tool call
				toolResponseCount := 0
				for j := splitIndex; j < len(messages); j++ {
					nextMsg := messages[j]
					if nextMsg.Role == llmtypes.ChatMessageTypeTool {
						// This is a tool response - it should be in old section, not recent
						// Move split point forward to include it in old section
						toolResponseCount++
					} else if nextMsg.Role == llmtypes.ChatMessageTypeAI {
						// Hit another assistant message - we've passed all tool responses
						break
					} else if nextMsg.Role == llmtypes.ChatMessageTypeHuman {
						// Hit a user message - we've passed all tool responses
						break
					}
				}
				// If we found tool responses that would be in recent section, move split point forward
				if toolResponseCount > 0 {
					// Move split point to include all tool responses in old section
					splitIndex = splitIndex + toolResponseCount
				}
			}
		}
	}

	// Check 2: If the first message in recent section (splitIndex) is a tool response,
	// ensure its tool call is also in recent section (this should already be handled by findSafeSplitPoint,
	// but we double-check here)
	if splitIndex < len(messages) {
		firstRecentMsg := messages[splitIndex]
		if firstRecentMsg.Role == llmtypes.ChatMessageTypeTool {
			// Find the assistant message that called this tool
			for j := splitIndex - 1; j >= 0; j-- {
				prevMsg := messages[j]
				if prevMsg.Role == llmtypes.ChatMessageTypeAI {
					hasToolCalls := false
					for _, part := range prevMsg.Parts {
						if _, ok := part.(llmtypes.ToolCall); ok {
							hasToolCalls = true
							break
						}
					}
					if hasToolCalls {
						// Found the tool call - if it's in old section, move split point back
						if j < splitIndex {
							splitIndex = j
						}
						break
					}
				}
				if prevMsg.Role == llmtypes.ChatMessageTypeHuman {
					break
				}
			}
		}
	}

	return splitIndex
}

// rebuildMessagesWithSummary rebuilds the messages array with summarized old history
func rebuildMessagesWithSummary(
	a *Agent,
	ctx context.Context,
	messages []llmtypes.MessageContent,
	keepLastMessages int,
) ([]llmtypes.MessageContent, error) {
	v2Logger := a.Logger

	// Validate and clamp keepLastMessages
	if keepLastMessages < 0 {
		return nil, fmt.Errorf("keepLastMessages must be >= 0, got %d", keepLastMessages)
	}
	if keepLastMessages > len(messages) {
		keepLastMessages = len(messages)
	}

	// Determine desired split point (now guaranteed to be in range [0, len(messages)])
	desiredSplitIndex := len(messages) - keepLastMessages

	// Find a safe split point that doesn't break tool call/response pairs
	splitIndex := findSafeSplitPoint(messages, desiredSplitIndex)

	// Additional validation: ensure we don't cut between tool call and its responses
	// Check if the last message in old section is a tool call - if so, all its responses must be in old section
	splitIndex = ensureToolCallResponseIntegrity(messages, splitIndex)

	// If there's nothing to summarize, return original messages (and don't emit started/completed)
	if splitIndex == 0 {
		v2Logger.Info("📊 [CONTEXT_SUMMARIZATION] No messages to summarize, keeping all messages",
			loggerv2.Int("total_messages", len(messages)))
		return messages, nil
	}

	// Emit summarization started event only when we will actually summarize
	startedEvent := events.NewContextSummarizationStartedEvent(len(messages), keepLastMessages, desiredSplitIndex)
	a.EmitTypedEvent(ctx, startedEvent)

	oldMessages := messages[:splitIndex]
	recentMessages := messages[splitIndex:]

	v2Logger.Info("📊 [CONTEXT_SUMMARIZATION] Splitting messages for summarization",
		loggerv2.Int("total_messages", len(messages)),
		loggerv2.Int("desired_split_index", desiredSplitIndex),
		loggerv2.Int("safe_split_index", splitIndex),
		loggerv2.Int("old_messages_count", len(oldMessages)),
		loggerv2.Int("recent_messages_count", len(recentMessages)),
		loggerv2.Int("keep_last_messages", keepLastMessages),
		loggerv2.Any("split_adjusted", splitIndex != desiredSplitIndex))

	// Check if first message is system prompt
	var systemMessage *llmtypes.MessageContent
	if len(oldMessages) > 0 && oldMessages[0].Role == llmtypes.ChatMessageTypeSystem {
		// Extract system message
		systemMsg := oldMessages[0]
		systemMessage = &systemMsg
		// Remove system message from oldMessages (we'll add it back separately)
		oldMessages = oldMessages[1:]
		splitIndex-- // Adjust split index
	}

	// If no old messages left after removing system, nothing to summarize
	if len(oldMessages) == 0 {
		v2Logger.Info("📊 [CONTEXT_SUMMARIZATION] No messages to summarize after removing system prompt")
		return messages, nil
	}

	v2Logger.Info("📊 [CONTEXT_SUMMARIZATION] Starting summarization",
		loggerv2.Int("old_messages_to_summarize", len(oldMessages)),
		loggerv2.Any("has_system_message", systemMessage != nil))

	// Generate summary
	summary, promptTokens, completionTokens, totalTokens, cacheTokens, reasoningTokens, summaryResp, err := summarizeConversationHistory(a, ctx, oldMessages)
	if err != nil {
		// Emit error event
		errorEvent := events.NewContextSummarizationErrorEvent(err.Error(), len(messages), keepLastMessages)
		a.EmitTypedEvent(ctx, errorEvent)
		return nil, fmt.Errorf("failed to summarize conversation history: %w", err)
	}

	// Accumulate summarization token usage into agent's cumulative tracking
	// This ensures summarization LLM calls are included in total token usage
	v2Logger.Info("📊 [CONTEXT_SUMMARIZATION] Accumulating summarization tokens",
		loggerv2.Int("prompt_tokens", promptTokens),
		loggerv2.Int("completion_tokens", completionTokens),
		loggerv2.Int("total_tokens", totalTokens),
		loggerv2.Int("cache_tokens", cacheTokens),
		loggerv2.Int("reasoning_tokens", reasoningTokens))
	a.accumulateTokenUsage(ctx, events.UsageMetrics{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		CacheTokens:      cacheTokens,
		ReasoningTokens:  reasoningTokens,
	}, summaryResp, 0) // Use turn 0 for summarization calls

	// Build new messages array
	newMessages := []llmtypes.MessageContent{}

	// 1. Add system prompt (if it exists)
	if systemMessage != nil {
		newMessages = append(newMessages, *systemMessage)
	}

	// 2. Add summary as a user message
	summaryMessage := llmtypes.MessageContent{
		Role: llmtypes.ChatMessageTypeHuman,
		Parts: []llmtypes.ContentPart{
			llmtypes.TextContent{
				Text: fmt.Sprintf("=== CONVERSATION SUMMARY (Previous %d messages) ===\n\n%s\n\n=== END SUMMARY ===",
					len(oldMessages), summary),
			},
		},
	}
	newMessages = append(newMessages, summaryMessage)

	// 3. Add recent messages (unchanged)
	newMessages = append(newMessages, recentMessages...)

	v2Logger.Info("✅ [CONTEXT_SUMMARIZATION] Messages rebuilt with summary",
		loggerv2.Int("original_message_count", len(messages)),
		loggerv2.Int("new_message_count", len(newMessages)),
		loggerv2.Int("messages_reduced_by", len(messages)-len(newMessages)),
		loggerv2.Int("summary_length_chars", len(summary)),
		loggerv2.Int("old_messages_summarized", len(oldMessages)),
		loggerv2.Int("recent_messages_kept", len(recentMessages)))

	// Emit summarization completed event
	completedEvent := events.NewContextSummarizationCompletedEvent(
		len(messages),
		len(newMessages),
		len(oldMessages),
		len(recentMessages),
		len(summary),
		splitIndex,
		desiredSplitIndex,
		summary, // Include summary in event for observability
		promptTokens,
		completionTokens,
		totalTokens,
		cacheTokens,
		reasoningTokens,
	)
	a.EmitTypedEvent(ctx, completedEvent)

	return newMessages, nil
}

// ShouldSummarizeOnTokenThreshold checks if summarization should be performed based on token usage
// Returns true if token usage exceeds either:
// 1. The threshold percentage of the model's context window (if SummarizeOnTokenThreshold is enabled)
// 2. The fixed token threshold (if SummarizeOnFixedTokenThreshold is enabled)
// Uses OR logic: either threshold can trigger summarization
func ShouldSummarizeOnTokenThreshold(a *Agent, currentTokenUsage int) (bool, error) {
	if !a.EnableContextSummarization {
		return false, nil
	}

	// Check fixed token threshold first (doesn't require model metadata)
	if a.SummarizeOnFixedTokenThreshold && a.FixedTokenThreshold > 0 {
		if currentTokenUsage >= a.FixedTokenThreshold {
			return true, nil
		}
	}

	// Check percentage-based threshold (requires model metadata)
	if !a.SummarizeOnTokenThreshold {
		return false, nil
	}

	// Get model metadata to determine context window
	if a.LLM == nil {
		return false, fmt.Errorf("LLM is nil, cannot determine context window")
	}

	modelID := a.ModelID
	if modelID == "" {
		modelID = a.LLM.GetModelID()
	}

	metadata, err := a.LLM.GetModelMetadata(modelID)
	if err != nil || metadata == nil {
		// Metadata unavailable: caller can fall back to max-turns (treat as "no decision" not hard error)
		return false, nil
	}

	// Calculate threshold in tokens
	percent := a.TokenThresholdPercent
	if percent <= 0 || percent > 1 {
		percent = 0.8
	}
	thresholdTokens := int(float64(metadata.ContextWindow) * percent)

	// Check if current usage exceeds threshold
	shouldSummarize := currentTokenUsage >= thresholdTokens

	return shouldSummarize, nil
}

// GetSummaryKeepLastMessages returns the number of recent messages to keep when summarizing
func GetSummaryKeepLastMessages(a *Agent) int {
	if a.SummaryKeepLastMessages > 0 {
		return a.SummaryKeepLastMessages
	}
	return DefaultSummaryKeepLastMessages
}

// GetSummarizationCooldownTurns returns the number of turns to wait after summarization
func GetSummarizationCooldownTurns(a *Agent) int {
	if a.SummarizationCooldownTurns > 0 {
		return a.SummarizationCooldownTurns
	}
	return DefaultSummarizationCooldownTurns
}

// SummarizeConversationHistory is a public wrapper for rebuildMessagesWithSummary
// It allows external callers (like the server) to manually trigger conversation summarization
// Note: Manual triggers do not update lastSummarizationTurn (cooldown tracking)
func SummarizeConversationHistory(a *Agent, ctx context.Context, messages []llmtypes.MessageContent, keepLastMessages int) ([]llmtypes.MessageContent, error) {
	return rebuildMessagesWithSummary(a, ctx, messages, keepLastMessages)
}
