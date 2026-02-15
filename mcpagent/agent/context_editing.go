// context_editing.go
//
// This file contains the context editing logic for compacting stale tool responses
// in conversation history. It implements the "Dynamic Context Reduction" pattern
// where large tool responses (> threshold tokens) that are older than N turns
// are replaced with file path references, similar to context offloading.
//
// The compaction process:
// 1. Scans messages for tool responses (ChatMessageTypeTool)
// 2. For each tool response, counts tokens and calculates turn age
// 3. If tokens > threshold AND turns > turn threshold:
//    - Saves content to filesystem (reusing context offloading infrastructure)
//    - Replaces message content with file path reference + preview

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
	// DefaultContextEditingThreshold is the default token threshold for context editing
	// Tool responses exceeding this threshold (in tokens) are candidates for compaction
	// Set to 10k tokens to preserve cached tokens for cost efficiency
	DefaultContextEditingThreshold = 10000

	// DefaultContextEditingTurnThreshold is the default turn age threshold
	// Tool responses older than this many turns will be compacted
	// Set to 10 turns to preserve cached tokens for cost efficiency
	DefaultContextEditingTurnThreshold = 10
)

// CompactStaleToolResponses scans messages and compacts stale tool responses
// by replacing them with file path references when they exceed token threshold
// and are older than the turn threshold
// This is a public function that can be called manually to trigger context editing
func CompactStaleToolResponses(a *Agent, ctx context.Context, messages []llmtypes.MessageContent, currentTurn int) ([]llmtypes.MessageContent, error) {
	return compactStaleToolResponses(a, ctx, messages, currentTurn)
}

// compactStaleToolResponses scans messages and compacts stale tool responses
// by replacing them with file path references when they exceed token threshold
// and are older than the turn threshold
func compactStaleToolResponses(a *Agent, ctx context.Context, messages []llmtypes.MessageContent, currentTurn int) ([]llmtypes.MessageContent, error) {
	v2Logger := a.Logger

	if !a.EnableContextEditing {
		v2Logger.Info("⏭️ [CONTEXT_EDITING] Skipping - context editing disabled",
			loggerv2.Int("current_turn", currentTurn))
		return messages, nil
	}
	threshold := a.ContextEditingThreshold
	if threshold == 0 {
		threshold = DefaultContextEditingThreshold
	}
	turnThreshold := a.ContextEditingTurnThreshold
	if turnThreshold == 0 {
		turnThreshold = DefaultContextEditingTurnThreshold
	}

	// Early return: Can't compact anything if we haven't had enough turns yet
	if currentTurn <= turnThreshold {
		v2Logger.Info("⏭️ [CONTEXT_EDITING] Skipping - not enough turns yet",
			loggerv2.Int("current_turn", currentTurn),
			loggerv2.Int("turn_threshold", turnThreshold))
		return messages, nil
	}

	// Early return: Check if there are any tool response messages at all
	hasToolResponses := false
	initialToolResponseCount := 0
	for _, msg := range messages {
		if msg.Role == llmtypes.ChatMessageTypeTool {
			hasToolResponses = true
			initialToolResponseCount++
		}
	}
	if !hasToolResponses {
		v2Logger.Info("⏭️ [CONTEXT_EDITING] Skipping - no tool responses found",
			loggerv2.Int("current_turn", currentTurn),
			loggerv2.Int("total_messages", len(messages)))
		return messages, nil
	}

	v2Logger.Info("🔍 [CONTEXT_EDITING] Checking for compaction",
		loggerv2.Int("current_turn", currentTurn),
		loggerv2.Int("turn_threshold", turnThreshold),
		loggerv2.Int("token_threshold", threshold),
		loggerv2.Int("tool_response_count", initialToolResponseCount),
		loggerv2.Int("total_messages", len(messages)))

	// Create a deep copy of messages to modify
	// We need to create new message structs to avoid modifying the original
	modifiedMessages := make([]llmtypes.MessageContent, len(messages))
	for i, msg := range messages {
		// Create a new Parts slice to avoid sharing references
		newParts := make([]llmtypes.ContentPart, len(msg.Parts))
		copy(newParts, msg.Parts)
		modifiedMessages[i] = llmtypes.MessageContent{
			Role:  msg.Role,
			Parts: newParts,
		}
	}

	compactedCount := 0
	totalTokensSaved := 0
	alreadyCompactedCount := 0
	toolResponseCount := 0
	var evaluations []events.ToolResponseEvaluation

	// Debug: Log message structure to understand ordering
	messageRoles := make([]string, 0, len(modifiedMessages))
	for _, msg := range modifiedMessages {
		messageRoles = append(messageRoles, string(msg.Role))
	}
	v2Logger.Debug("🔍 [CONTEXT_EDITING] Message structure",
		loggerv2.Int("total_messages", len(modifiedMessages)),
		loggerv2.String("message_roles_sample", func() string {
			// Show first 20 and last 10 roles
			if len(messageRoles) <= 30 {
				return strings.Join(messageRoles, ",")
			}
			first := strings.Join(messageRoles[:20], ",")
			last := strings.Join(messageRoles[len(messageRoles)-10:], ",")
			return first + "...(..." + last
		}()))

	// Scan messages from oldest to newest
	for i := 0; i < len(modifiedMessages); i++ {
		msg := modifiedMessages[i]

		// Only process tool response messages
		if msg.Role != llmtypes.ChatMessageTypeTool {
			continue
		}

		toolResponseCount++

		// Extract tool response content
		var toolResponse *llmtypes.ToolCallResponse
		var toolName string
		for _, part := range msg.Parts {
			if tr, ok := part.(llmtypes.ToolCallResponse); ok {
				toolResponse = &tr
				toolName = tr.Name
				break
			}
		}

		if toolResponse == nil {
			continue
		}

		// Skip if already compacted (contains file path reference)
		content := toolResponse.Content
		if isCompactedContent(content) {
			alreadyCompactedCount++
			// Still record it in evaluations for visibility
			evaluations = append(evaluations, events.ToolResponseEvaluation{
				ToolName:            toolName,
				TokenCount:          0, // Don't count tokens for already compacted
				TurnAge:             0,
				MeetsTokenThreshold: false,
				MeetsTurnThreshold:  false,
				WasCompacted:        false,
				SkipReason:          "already_compacted",
			})
			continue
		}

		// Count tokens for this tool response
		tokenCount := a.toolOutputHandler.CountTokensForModel(content, a.ModelID)

		// Calculate turn age: simple approach - count user messages before to determine creation turn
		turnAge := calculateTurnAge(modifiedMessages, i, currentTurn)

		// Debug: Calculate creation turn for logging
		userMessagesBefore := 0
		for j := 0; j < i; j++ {
			if modifiedMessages[j].Role == llmtypes.ChatMessageTypeHuman {
				userMessagesBefore++
			}
		}
		creationTurn := userMessagesBefore + 1

		meetsTokenThreshold := tokenCount > threshold
		meetsTurnThreshold := turnAge >= turnThreshold // Use >= so turn_age=5 meets threshold=5
		willCompact := meetsTokenThreshold && meetsTurnThreshold

		// Debug logging for each tool response evaluation
		if tokenCount > 0 || turnAge > 0 {
			// Calculate position in tool responses (1-indexed)
			toolResponsePosition := 0
			for j := 0; j <= i; j++ {
				if modifiedMessages[j].Role == llmtypes.ChatMessageTypeTool {
					toolResponsePosition++
				}
			}

			v2Logger.Debug("🔍 [CONTEXT_EDITING] Evaluating tool response",
				loggerv2.String("tool_name", toolName),
				loggerv2.Int("token_count", tokenCount),
				loggerv2.Int("token_threshold", threshold),
				loggerv2.Int("turn_age", turnAge),
				loggerv2.Int("creation_turn", creationTurn),
				loggerv2.Int("current_turn", currentTurn),
				loggerv2.Int("turn_threshold", turnThreshold),
				loggerv2.String("meets_token_threshold", fmt.Sprintf("%v", meetsTokenThreshold)),
				loggerv2.String("meets_turn_threshold", fmt.Sprintf("%v", meetsTurnThreshold)),
				loggerv2.String("will_compact", fmt.Sprintf("%v", willCompact)),
				loggerv2.Int("tool_response_position", toolResponsePosition),
				loggerv2.Int("message_index", i))
		}

		// Determine skip reason if not compacting
		skipReason := ""
		if !willCompact {
			if !meetsTokenThreshold && !meetsTurnThreshold {
				skipReason = fmt.Sprintf("token_count_too_low(%d<=%d) and turn_age_too_low(%d<=%d)", tokenCount, threshold, turnAge, turnThreshold)
			} else if !meetsTokenThreshold {
				skipReason = fmt.Sprintf("token_count_too_low(%d<=%d)", tokenCount, threshold)
			} else if !meetsTurnThreshold {
				skipReason = fmt.Sprintf("turn_age_too_low(%d<=%d)", turnAge, turnThreshold)
			}
		}

		// Check if this tool response should be compacted
		if willCompact {
			v2Logger.Info("📝 [CONTEXT_EDITING] Compacting stale tool response",
				loggerv2.String("tool_name", toolName),
				loggerv2.Int("token_count", tokenCount),
				loggerv2.Int("turn_age", turnAge),
				loggerv2.Int("threshold", threshold),
				loggerv2.Int("turn_threshold", turnThreshold))

			// Save content to file (reuse context offloading infrastructure)
			filePath, err := a.toolOutputHandler.WriteToolOutputToFile(content, toolName)
			if err != nil {
				v2Logger.Warn("Failed to save tool response to file for compaction",
					loggerv2.String("tool_name", toolName),
					loggerv2.Error(err))
				continue // Skip this one if file write fails
			}

			// Create compacted message with file path reference (10% preview for context editing)
			compactedContent := a.toolOutputHandler.CreateToolOutputMessageWithPreview(
				toolResponse.ToolCallID,
				filePath,
				content,
				10,   // 10% preview for context editing (stale responses)
				true, // isContextEditing=true for concise message
			)

			// Replace the tool response content
			newParts := make([]llmtypes.ContentPart, 0, len(msg.Parts))
			for _, part := range msg.Parts {
				if tr, ok := part.(llmtypes.ToolCallResponse); ok {
					// Replace with compacted content
					newParts = append(newParts, llmtypes.ToolCallResponse{
						ToolCallID: tr.ToolCallID,
						Name:       tr.Name,
						Content:    compactedContent,
					})
				} else {
					newParts = append(newParts, part)
				}
			}

			modifiedMessages[i] = llmtypes.MessageContent{
				Role:  msg.Role,
				Parts: newParts,
			}

			// Verify the modification was applied
			verifyMsg := modifiedMessages[i]
			var verifyContent string
			for _, part := range verifyMsg.Parts {
				if tr, ok := part.(llmtypes.ToolCallResponse); ok {
					verifyContent = tr.Content
					break
				}
			}
			if !strings.Contains(verifyContent, "has been saved to:") && !strings.Contains(verifyContent, "tool_output_folder") {
				v2Logger.Warn("⚠️ [CONTEXT_EDITING] Modification verification failed - content not updated",
					loggerv2.String("tool_name", toolName),
					loggerv2.String("content_preview", func() string {
						if len(verifyContent) > 100 {
							return verifyContent[:100] + "..."
						}
						return verifyContent
					}()))
			}

			compactedCount++
			totalTokensSaved += tokenCount

			v2Logger.Info("✅ [CONTEXT_EDITING] Tool response compacted",
				loggerv2.String("tool_name", toolName),
				loggerv2.String("file_path", filePath),
				loggerv2.Int("tokens_saved", tokenCount),
				loggerv2.String("compacted_content_preview", func() string {
					if len(compactedContent) > 150 {
						return compactedContent[:150] + "..."
					}
					return compactedContent
				}()))

			// Record successful compaction
			evaluations = append(evaluations, events.ToolResponseEvaluation{
				ToolName:            toolName,
				TokenCount:          tokenCount,
				TurnAge:             turnAge,
				MeetsTokenThreshold: meetsTokenThreshold,
				MeetsTurnThreshold:  meetsTurnThreshold,
				WasCompacted:        true,
				TokensSaved:         tokenCount,
			})
		} else {
			// Record evaluation for non-compacted response
			evaluations = append(evaluations, events.ToolResponseEvaluation{
				ToolName:            toolName,
				TokenCount:          tokenCount,
				TurnAge:             turnAge,
				MeetsTokenThreshold: meetsTokenThreshold,
				MeetsTurnThreshold:  meetsTurnThreshold,
				WasCompacted:        false,
				SkipReason:          skipReason,
			})
		}
	}

	// Always emit context editing completed event (even when nothing was compacted)
	completedEvent := events.NewContextEditingCompletedEvent(
		len(messages),
		toolResponseCount,
		compactedCount,
		totalTokensSaved,
		threshold,
		turnThreshold,
		currentTurn,
		alreadyCompactedCount,
		evaluations,
	)
	a.EmitTypedEvent(ctx, completedEvent)

	v2Logger.Info("📊 [CONTEXT_EDITING] Context editing completed",
		loggerv2.Int("tool_response_count", toolResponseCount),
		loggerv2.Int("compacted_count", compactedCount),
		loggerv2.Int("already_compacted_count", alreadyCompactedCount),
		loggerv2.Int("total_tokens_saved", totalTokensSaved),
		loggerv2.Int("evaluations_count", len(evaluations)))

	// Verify that compacted messages are actually in the returned slice
	if compactedCount > 0 {
		compactedInReturn := 0
		for _, msg := range modifiedMessages {
			if msg.Role == llmtypes.ChatMessageTypeTool {
				for _, part := range msg.Parts {
					if tr, ok := part.(llmtypes.ToolCallResponse); ok {
						if strings.Contains(tr.Content, "has been saved to:") || strings.Contains(tr.Content, "tool_output_folder") {
							compactedInReturn++
						}
					}
				}
			}
		}
		// Note: compactedInReturn includes both newly compacted messages AND already compacted messages
		// So we verify that at least the newly compacted ones are present
		expectedTotalCompacted := alreadyCompactedCount + compactedCount
		v2Logger.Debug("🔍 [CONTEXT_EDITING] Verification of returned messages",
			loggerv2.Int("compacted_count", compactedCount),
			loggerv2.Int("already_compacted_count", alreadyCompactedCount),
			loggerv2.Int("compacted_found_in_return", compactedInReturn),
			loggerv2.Int("expected_total_compacted", expectedTotalCompacted),
			loggerv2.String("all_compacted_present", fmt.Sprintf("%v", compactedInReturn >= expectedTotalCompacted)))
	}

	return modifiedMessages, nil
}

// calculateTurnAge calculates how many turns ago a tool response was made
// Simple approach: count user messages BEFORE the tool response to determine which turn it was created in
// Then: turn_age = current_turn - creation_turn
func calculateTurnAge(messages []llmtypes.MessageContent, toolResponseIndex int, currentTurn int) int {
	// Count user messages BEFORE this tool response
	// Each user message represents a new turn, so this tells us which turn the tool response was created in
	userMessagesBefore := 0
	for i := 0; i < toolResponseIndex; i++ {
		if messages[i].Role == llmtypes.ChatMessageTypeHuman {
			userMessagesBefore++
		}
	}

	// The turn when this tool response was created = userMessagesBefore + 1
	// (turn 1 has 0 user messages before it, turn 2 has 1, etc.)
	creationTurn := userMessagesBefore + 1

	// Turn age = how many turns have passed since creation
	turnAge := currentTurn - creationTurn

	// Ensure non-negative (shouldn't happen, but safety check)
	if turnAge < 0 {
		turnAge = 0
	}

	return turnAge
}

// isCompactedContent checks if content is already a compacted reference (contains file path)
func isCompactedContent(content string) bool {
	// Check for indicators that content is already compacted:
	// 1. Contains "has been saved to:" (from CreateToolOutputMessageWithPreview)
	// 2. Contains "tool_output_folder" path
	return strings.Contains(content, "has been saved to:") || strings.Contains(content, "tool_output_folder")
}
