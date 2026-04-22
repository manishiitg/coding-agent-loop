package orchestrator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	baseevents "github.com/manishiitg/mcpagent/events"
)

// routeFeedbackToParentChat checks if the given workflow session was invoked
// from a builder chat session, and if so, injects a message describing the
// pending human-input question into that parent chat. Returns true when the
// question was routed (so callers should skip emitting the blocking popup UI
// event and rely on the builder to call submit_human_answer instead).
func (bo *BaseOrchestrator) routeFeedbackToParentChat(
	ctx context.Context,
	sessionID string,
	requestID string,
	question string,
	kind string,
	options []string,
	yesLabel string,
	noLabel string,
) bool {
	pc := virtualtools.GetParentChat(sessionID)
	if pc == nil || pc.SessionID == "" {
		return false
	}
	// Without an installed injector, routing would silently black-hole the
	// question. Fall back to the popup path in that case.
	if !virtualtools.HasChatInjector() {
		return false
	}

	var msg strings.Builder
	msg.WriteString("[WORKFLOW_HUMAN_INPUT] The workflow you launched is waiting on a human_input step. ")
	msg.WriteString("If you already know the answer from the conversation so far, answer directly by calling submit_human_answer. ")
	msg.WriteString("Otherwise, ask the user for what you need, then submit their reply.\n\n")
	if pc.WorkflowPath != "" {
		msg.WriteString(fmt.Sprintf("Workflow: %s\n", pc.WorkflowPath))
	}
	if pc.GroupName != "" {
		msg.WriteString(fmt.Sprintf("Group: %s\n", pc.GroupName))
	}
	msg.WriteString(fmt.Sprintf("Request ID: %s\n", requestID))
	msg.WriteString(fmt.Sprintf("Response type: %s\n", kind))
	msg.WriteString(fmt.Sprintf("Question: %s\n", question))
	switch kind {
	case "yesno":
		msg.WriteString(fmt.Sprintf("Expected answer: %q (yes) or %q (no).\n", yesLabel, noLabel))
		msg.WriteString("Submit the exact yes/no label as the answer.\n")
	case "multiple_choice":
		msg.WriteString("Options:\n")
		for i, opt := range options {
			msg.WriteString(fmt.Sprintf("  %d. %s\n", i, opt))
		}
		msg.WriteString("Submit the answer as 'option0', 'option1', ... matching the index above.\n")
	default:
		msg.WriteString("Submit the user's free-text answer as the answer.\n")
	}

	if err := virtualtools.InjectChatMessage(ctx, pc.SessionID, pc.UserID, msg.String()); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to inject human-input question into parent chat %s: %v (falling back to popup)", pc.SessionID, err))
		return false
	}
	bo.GetLogger().Info(fmt.Sprintf("📨 Routed human_input question to parent chat session %s (request=%s, kind=%s)", pc.SessionID, requestID, kind))
	return true
}

// RequestHumanFeedback is a common function for requesting human feedback with blocking behavior
// Returns: (approved bool, feedback string, error)
func (bo *BaseOrchestrator) RequestHumanFeedback(
	ctx context.Context,
	requestID string,
	question string,
	context string,
	sessionID string,
	workflowID string,
) (bool, string, error) {
	// Removed verbose logging

	// Use HumanFeedbackStore to wait for response
	feedbackStore := virtualtools.GetHumanFeedbackStore()

	// Create feedback request without notifications (only registers in store for WaitForResponse)
	if err := feedbackStore.CreateRequestWithoutNotification(requestID, question); err != nil {
		return false, "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	// If this workflow was invoked from a builder chat session, route the
	// question into that chat instead of emitting the blocking popup UI.
	if bo.routeFeedbackToParentChat(ctx, sessionID, requestID, question, "text", nil, "Approve & Continue", "Reject") {
		// Skip popup event emission — the builder will resolve this via
		// submit_human_answer.
	} else {
		// Emit human feedback request event
		// Note: YesNoOnly is false to allow text feedback in frontend (textarea + "Approve & Continue" button)
		// But we'll still send buttons to Slack for convenience
		feedbackEvent := &events.BlockingHumanFeedbackEvent{
			BaseEventData: baseevents.BaseEventData{
				Timestamp: time.Now(),
			},
			Question:      question,
			AllowFeedback: true, // Allow text feedback in frontend
			Context:       context,
			SessionID:     sessionID,
			WorkflowID:    workflowID,
			RequestID:     requestID,
			YesNoOnly:     false,                // false = frontend shows textarea + "Approve & Continue" button
			YesLabel:      "Approve & Continue", // Button label for frontend and Slack
			NoLabel:       "Reject",             // Default reject label
		}

		// Emit the event using the public method
		agentEvent := &baseevents.AgentEvent{
			Type:      events.BlockingHumanFeedback,
			Timestamp: time.Now(),
			Data:      feedbackEvent,
		}

		// Use the context-aware bridge to emit the event
		bridge := bo.GetContextAwareBridge()
		if bridge == nil {
			bo.GetLogger().Error("❌ Context-aware bridge is nil, cannot emit blocking human feedback event", fmt.Errorf("context-aware bridge is nil"))
			return false, "", fmt.Errorf("context-aware bridge is nil")
		}
		if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
			bo.GetLogger().Error(fmt.Sprintf("❌ Failed to emit human feedback event: %v", err), err)
			return false, "", fmt.Errorf("failed to emit event: %w", err)
		}
	}

	// Removed verbose logging

	// BLOCKING CALL - waits here until response or timeout
	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		return false, "", fmt.Errorf("timeout waiting for human feedback: %w", err)
	}

	// Removed verbose logging

	// Parse response
	// Expected format: "Approve & Continue", "Approve", or feedback text for revision
	responseTrimmed := strings.TrimSpace(response)
	if responseTrimmed == "Approve & Continue" || responseTrimmed == "Approve" {
		return true, "", nil
	}

	// Default: treat as feedback for revision
	return false, response, nil
}

// RequestYesNoFeedback requests simple yes/no feedback from user with Approve/Reject buttons
// Returns: (approved bool, error)
func (bo *BaseOrchestrator) RequestYesNoFeedback(
	ctx context.Context,
	requestID string,
	question string,
	yesLabel string,
	noLabel string,
	context string,
	sessionID string,
	workflowID string,
) (bool, error) {
	// Removed verbose logging

	// Set default labels if not provided
	if yesLabel == "" {
		yesLabel = "Approve"
	}
	if noLabel == "" {
		noLabel = "Reject"
	}

	// Wait for response
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	// Create feedback request without notifications (only registers in store for WaitForResponse)
	if err := feedbackStore.CreateRequestWithoutNotification(requestID, question); err != nil {
		return false, fmt.Errorf("failed to create feedback request: %w", err)
	}

	// Route to parent chat if this workflow was invoked from a builder session.
	if bo.routeFeedbackToParentChat(ctx, sessionID, requestID, question, "yesno", nil, yesLabel, noLabel) {
		// Skip popup event emission.
	} else {
		// Emit human feedback request event with yes/no only mode
		feedbackEvent := &events.BlockingHumanFeedbackEvent{
			BaseEventData: baseevents.BaseEventData{
				Timestamp: time.Now(),
			},
			Question:      question,
			AllowFeedback: false, // No textarea in yes/no mode
			YesNoOnly:     true,  // Enable yes/no only mode
			YesLabel:      yesLabel,
			NoLabel:       noLabel,
			Context:       context,
			SessionID:     sessionID,
			WorkflowID:    workflowID,
			RequestID:     requestID,
		}

		// Emit the event
		agentEvent := &baseevents.AgentEvent{
			Type:      events.BlockingHumanFeedback,
			Timestamp: time.Now(),
			Data:      feedbackEvent,
		}

		if err := bo.GetContextAwareBridge().HandleEvent(ctx, agentEvent); err != nil {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit yes/no feedback event: %v", err))
		}
	}

	// Removed verbose logging

	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		return false, fmt.Errorf("timeout waiting for feedback: %w", err)
	}

	// Removed verbose logging

	// Parse response: "Approve" or the custom yesLabel means Yes; "yes"/"true"/"y"
	// are accepted as synonyms so a builder agent answering via chat can submit
	// natural text. Anything else means No.
	trimmed := strings.TrimSpace(response)
	if trimmed == "Approve" || strings.EqualFold(trimmed, yesLabel) {
		return true, nil
	}
	switch strings.ToLower(trimmed) {
	case "yes", "y", "true", "approve":
		return true, nil
	}
	return false, nil
}

// RequestMultipleChoiceFeedback requests multiple-choice feedback from user
// Returns: (choice string, error) where choice is "option0", "option1", "option2", etc. (0-based index)
func (bo *BaseOrchestrator) RequestMultipleChoiceFeedback(
	ctx context.Context,
	requestID string,
	question string,
	options []string,
	context string,
	sessionID string,
	workflowID string,
) (string, error) {
	// Removed verbose logging

	if len(options) == 0 {
		return "", fmt.Errorf("at least one option is required")
	}

	// Wait for response
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	// Create feedback request without notifications (only registers in store for WaitForResponse)
	if err := feedbackStore.CreateRequestWithoutNotification(requestID, question); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	// Route to parent chat if this workflow was invoked from a builder session.
	if bo.routeFeedbackToParentChat(ctx, sessionID, requestID, question, "multiple_choice", options, "", "") {
		// Skip popup event emission.
	} else {
		// Emit human feedback request event with multiple-choice mode
		feedbackEvent := &events.BlockingHumanFeedbackEvent{
			BaseEventData: baseevents.BaseEventData{
				Timestamp: time.Now(),
			},
			Question:      question,
			AllowFeedback: false, // No textarea in multiple-choice mode
			Options:       options,
			Context:       context,
			SessionID:     sessionID,
			WorkflowID:    workflowID,
			RequestID:     requestID,
		}

		// Emit the event
		agentEvent := &baseevents.AgentEvent{
			Type:      events.BlockingHumanFeedback,
			Timestamp: time.Now(),
			Data:      feedbackEvent,
		}

		if err := bo.GetContextAwareBridge().HandleEvent(ctx, agentEvent); err != nil {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit multiple-choice feedback event: %v", err))
		}
	}

	// Removed verbose logging

	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		return "", fmt.Errorf("timeout waiting for feedback: %w", err)
	}

	// Removed verbose logging

	// Parse response: should be "option0", "option1", "option2", etc. (0-based)
	response = strings.TrimSpace(response)

	// Validate response format (option0, option1, option2, etc.)
	if strings.HasPrefix(response, "option") {
		// Extract index from "option0", "option1", etc.
		indexStr := strings.TrimPrefix(response, "option")
		index, err := strconv.Atoi(indexStr)
		if err == nil && index >= 0 && index < len(options) {
			return response, nil
		}
	}

	// Fallback: match by option text (case-insensitive). This is handy when a
	// builder agent answering via chat submits the literal option text.
	for i, opt := range options {
		if strings.EqualFold(strings.TrimSpace(opt), response) {
			return fmt.Sprintf("option%d", i), nil
		}
	}

	// Default to option0 if response is unclear
	bo.GetLogger().Warn(fmt.Sprintf("⚠️ Unexpected response format: %s, defaulting to option0", response))
	return "option0", nil
}
