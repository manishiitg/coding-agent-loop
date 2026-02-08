package virtualtools

import (
	"context"
	"fmt"
	"time"

	"mcp-agent-builder-go/agent_go/cmd/server/services"

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
			Description: "Use this tool when you need to get human input, confirmation, or feedback. This tool will pause execution until the user provides input via the UI. You can present multiple options as buttons for the user to choose from, or use free-text input. The tool returns the user's response as text. Ideal for asking clarifying questions, presenting choices, requesting confirmation, or any situation requiring human decision-making.",
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
					"options": map[string]interface{}{
						"type": "array",
						"items": map[string]interface{}{
							"type": "string",
						},
						"description": "Optional list of choices to present as buttons. When provided, the user clicks a button instead of typing. Use for multiple-choice questions (e.g. ['Option A: Use REST API', 'Option B: Use GraphQL', 'Option C: Use gRPC']). Omit for free-text input.",
					},
				},
				"required": []string{"unique_id", "message_for_user"},
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
	// Extract parameters - message_for_user is optional, use default if missing
	messageForUser, ok := args["message_for_user"].(string)
	if !ok || messageForUser == "" {
		messageForUser = "Please provide your feedback here..."
	}

	uniqueID, ok := args["unique_id"].(string)
	if !ok {
		return "", fmt.Errorf("unique_id is required and must be a string")
	}

	// Extract optional options array
	var options []string
	if optionsRaw, ok := args["options"].([]interface{}); ok {
		for _, opt := range optionsRaw {
			if s, ok := opt.(string); ok && s != "" {
				options = append(options, s)
			}
		}
	}

	// Emit blocking_human_feedback event so the frontend renders the proper UI
	if emitter, ok := ctx.Value(SessionEventEmitterKey).(SessionEventEmitter); ok && emitter != nil {
		hasOptions := len(options) > 0
		emitter.EmitBlockingHumanFeedback(uniqueID, messageForUser, hasOptions, "", "", options...)
	}

	// Build button options for notifications (Slack, etc.)
	var buttonOptions *services.ButtonOptions
	if len(options) > 0 {
		buttonOptions = &services.ButtonOptions{
			Options: options,
		}
	}

	// Get global feedback store
	feedbackStore := GetHumanFeedbackStore()

	// Create feedback request (automatically sends notifications via notification manager)
	if err := feedbackStore.CreateRequestWithSlack(ctx, uniqueID, messageForUser, "", buttonOptions); err != nil {
		return "", fmt.Errorf("failed to create feedback request: %w", err)
	}

	// Wait for user response (with timeout)
	response, err := feedbackStore.WaitForResponse(uniqueID, 5*time.Minute)
	if err != nil {
		return "", fmt.Errorf("failed to get user feedback: %w", err)
	}

	return response, nil
}
