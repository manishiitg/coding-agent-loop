package orchestrator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcpagent/events"
)

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

	// Emit human feedback request event
	// Note: YesNoOnly is false to allow text feedback in frontend (textarea + "Approve & Continue" button)
	// But we'll still send buttons to Slack for convenience
	feedbackEvent := &events.BlockingHumanFeedbackEvent{
		BaseEventData: events.BaseEventData{
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
	agentEvent := &events.AgentEvent{
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

	// Use HumanFeedbackStore to wait for response
	// Note: blocking_human_feedback events do NOT send Slack notifications
	// Only request_human_feedback events send Slack notifications
	feedbackStore := virtualtools.GetHumanFeedbackStore()

	// Create feedback request without notifications (only registers in store for WaitForResponse)
	if err := feedbackStore.CreateRequestWithoutNotification(requestID, question); err != nil {
		return false, "", fmt.Errorf("failed to create feedback request: %w", err)
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

	// Emit human feedback request event with yes/no only mode
	feedbackEvent := &events.BlockingHumanFeedbackEvent{
		BaseEventData: events.BaseEventData{
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
	agentEvent := &events.AgentEvent{
		Type:      events.BlockingHumanFeedback,
		Timestamp: time.Now(),
		Data:      feedbackEvent,
	}

	if err := bo.GetContextAwareBridge().HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit yes/no feedback event: %v", err))
	}

	// Wait for response
	// Note: blocking_human_feedback events do NOT send Slack notifications
	// Only request_human_feedback events send Slack notifications
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	// Create feedback request without notifications (only registers in store for WaitForResponse)
	if err := feedbackStore.CreateRequestWithoutNotification(requestID, question); err != nil {
		return false, fmt.Errorf("failed to create feedback request: %w", err)
	}

	// Removed verbose logging

	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		return false, fmt.Errorf("timeout waiting for feedback: %w", err)
	}

	// Removed verbose logging

	// Parse response: "Approve" means Yes, anything else means No
	if strings.TrimSpace(response) == "Approve" {
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

	// Emit human feedback request event with multiple-choice mode
	feedbackEvent := &events.BlockingHumanFeedbackEvent{
		BaseEventData: events.BaseEventData{
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
	agentEvent := &events.AgentEvent{
		Type:      events.BlockingHumanFeedback,
		Timestamp: time.Now(),
		Data:      feedbackEvent,
	}

	if err := bo.GetContextAwareBridge().HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit multiple-choice feedback event: %v", err))
	}

	// Wait for response
	// Note: blocking_human_feedback events do NOT send Slack notifications
	// Only request_human_feedback events send Slack notifications
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	// Create feedback request without notifications (only registers in store for WaitForResponse)
	if err := feedbackStore.CreateRequestWithoutNotification(requestID, question); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
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

	// Default to option0 if response is unclear
	bo.GetLogger().Warn(fmt.Sprintf("⚠️ Unexpected response format: %s, defaulting to option0", response))
	return "option0", nil
}
