package virtualtools

import (
	"context"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/llmtypes"
)

// CreateHumanTools creates human interaction tools
func CreateHumanTools() []llmtypes.Tool {
	var humanTools []llmtypes.Tool

	// Add human_feedback tool
	humanFeedbackTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "human_feedback",
			Description: "Use then when there is no option except to get human input, when you are stuck and need to ask a question that requires human input. This tool will pause execution until the user provides input via the UI. Ideal for things like OTP, 2FA, etc.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message_for_user": map[string]interface{}{
						"type":        "string",
						"description": "Message to display to the user requesting their feedback",
					},
					"unique_id": map[string]interface{}{
						"type":        "string",
						"description": "Unique identifier for this feedback request. Always generate a UUID (e.g., '550e8400-e29b-41d4-a716-446655440000').",
					},
				},
				"required": []string{"message_for_user", "unique_id"},
			},
		},
	}
	humanTools = append(humanTools, humanFeedbackTool)

	return humanTools
}

// CreateHumanToolExecutors creates the execution functions for human tools
func CreateHumanToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["human_feedback"] = handleHumanFeedback

	return executors
}

// handleHumanFeedback handles the human_feedback tool execution
func handleHumanFeedback(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract parameters
	messageForUser, ok := args["message_for_user"].(string)
	if !ok {
		return "", fmt.Errorf("message_for_user is required and must be a string")
	}

	uniqueID, ok := args["unique_id"].(string)
	if !ok {
		return "", fmt.Errorf("unique_id is required and must be a string")
	}
	// Get global feedback store
	feedbackStore := GetHumanFeedbackStore()

	// Create feedback request
	if err := feedbackStore.CreateRequest(uniqueID, messageForUser); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	// TODO: Emit event to frontend to show UI
	// This would need to be integrated with the event system

	// Wait for user response (with timeout)
	response, err := feedbackStore.WaitForResponse(uniqueID, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to get user feedback: %w", err)
	}

	return response, nil
}
