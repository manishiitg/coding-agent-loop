package orchestrator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	virtualtools "mcp-agent/agent_go/cmd/server/virtual-tools"
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
	bo.GetLogger().Infof("🤔 Requesting human feedback: %s", question)

	// Emit human feedback request event
	feedbackEvent := &events.BlockingHumanFeedbackEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Question:      question,
		AllowFeedback: true,
		Context:       context,
		SessionID:     sessionID,
		WorkflowID:    workflowID,
		RequestID:     requestID,
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
		bo.GetLogger().Errorf("❌ Context-aware bridge is nil, cannot emit blocking human feedback event")
		return false, "", fmt.Errorf("context-aware bridge is nil")
	}
	bo.GetLogger().Infof("📤 Attempting to emit blocking_human_feedback event via context-aware bridge")
	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Errorf("❌ Failed to emit human feedback event: %w", err)
		return false, "", fmt.Errorf("failed to emit event: %w", err)
	}
	bo.GetLogger().Infof("✅ Successfully emitted blocking_human_feedback event: requestID=%s", requestID)

	// Use HumanFeedbackStore to wait for response
	feedbackStore := virtualtools.GetHumanFeedbackStore()

	// Create feedback request (this registers it in the store)
	if err := feedbackStore.CreateRequest(requestID, question); err != nil {
		return false, "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	bo.GetLogger().Infof("⏸️ Orchestrator paused, waiting for human response (timeout: 10 minutes)...")

	// BLOCKING CALL - waits here until response or timeout
	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		return false, "", fmt.Errorf("timeout waiting for human feedback: %w", err)
	}

	bo.GetLogger().Infof("▶️ Orchestrator resumed with human response: %s", response)

	// Parse response
	// Expected format: "Approve" or feedback text for revision
	if strings.TrimSpace(response) == "Approve" {
		bo.GetLogger().Infof("✅ User approved via button, continuing")
		return true, "", nil
	}

	// Default: treat as feedback for revision
	bo.GetLogger().Infof("🔄 User provided feedback: %s", response)
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
	bo.GetLogger().Infof("🤔 Requesting yes/no feedback: %s", question)

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
		bo.GetLogger().Warnf("⚠️ Failed to emit yes/no feedback event: %w", err)
	}

	// Wait for response
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	if err := feedbackStore.CreateRequest(requestID, question); err != nil {
		return false, fmt.Errorf("failed to create feedback request: %w", err)
	}

	bo.GetLogger().Infof("⏸️ Orchestrator paused, waiting for yes/no response...")

	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		return false, fmt.Errorf("timeout waiting for feedback: %w", err)
	}

	bo.GetLogger().Infof("▶️ Orchestrator resumed with response: %s", response)

	// Parse response: "Approve" means Yes, anything else means No
	if strings.TrimSpace(response) == "Approve" {
		bo.GetLogger().Infof("✅ User selected Yes (Approve)")
		return true, nil
	}

	bo.GetLogger().Infof("❌ User selected No (Reject)")
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
	bo.GetLogger().Infof("🤔 Requesting multiple-choice feedback: %s (%d options)", question, len(options))

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
		bo.GetLogger().Warnf("⚠️ Failed to emit multiple-choice feedback event: %w", err)
	}

	// Wait for response
	feedbackStore := virtualtools.GetHumanFeedbackStore()
	if err := feedbackStore.CreateRequest(requestID, question); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	bo.GetLogger().Infof("⏸️ Orchestrator paused, waiting for multiple-choice response...")

	response, err := feedbackStore.WaitForResponse(requestID, 10*time.Minute)
	if err != nil {
		return "", fmt.Errorf("timeout waiting for feedback: %w", err)
	}

	bo.GetLogger().Infof("▶️ Orchestrator resumed with response: %s", response)

	// Parse response: should be "option0", "option1", "option2", etc. (0-based)
	response = strings.TrimSpace(response)

	// Validate response format (option0, option1, option2, etc.)
	if strings.HasPrefix(response, "option") {
		// Extract index from "option0", "option1", etc.
		indexStr := strings.TrimPrefix(response, "option")
		index, err := strconv.Atoi(indexStr)
		if err == nil && index >= 0 && index < len(options) {
			bo.GetLogger().Infof("✅ User selected: %s (option %d: %s)", response, index, options[index])
			return response, nil
		}
	}

	// Default to option0 if response is unclear
	bo.GetLogger().Warnf("⚠️ Unexpected response format: %s, defaulting to option0", response)
	return "option0", nil
}
