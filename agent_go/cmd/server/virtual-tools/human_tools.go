package virtualtools

import (
	"context"
	"fmt"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// CreateHumanTools creates human interaction tools
func CreateHumanTools() []llmtypes.Tool {
	var humanTools []llmtypes.Tool

	// Add human_feedback tool
	humanFeedbackTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "human_feedback",
			Description: "Use this tool when you need to get human input, confirmation, or feedback. This tool will pause execution until the user provides input via the UI. The tool returns the user's response as text - you must interpret the response to determine the user's intent (approval, rejection, questions, etc.). Ideal for requesting confirmation before making plan modifications, asking for OTP/2FA codes, or any situation requiring human decision-making.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
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
			}),
		},
	}
	humanTools = append(humanTools, humanFeedbackTool)

	return humanTools
}

// GetToolCategory returns the category name for human tools
func GetHumanToolCategory() string {
	return "human"
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

	// Create feedback request (automatically sends notifications via notification manager)
	// This will send to all enabled connectors (Slack, Gmail, WhatsApp, etc.)
	// No button options for simple human_feedback tool (just text input)
	if err := feedbackStore.CreateRequestWithSlack(ctx, uniqueID, messageForUser, "", nil); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	// Wait for user response (with timeout)
	response, err := feedbackStore.WaitForResponse(uniqueID, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to get user feedback: %w", err)
	}

	return response, nil
}
