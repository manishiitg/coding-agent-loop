package virtualtools

import (
	"context"
	"fmt"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetCompletionToolCategory returns the category name for completion tools
func GetCompletionToolCategory() string {
	return "completion_tools"
}

// Context key for completion tool execution
type completionContextKey string

const (
	// MarkStepCompleteKey is the context key for the mark step complete function
	MarkStepCompleteKey completionContextKey = "mark_step_complete"
)

// MarkStepCompleteFunc is the function signature for marking a step as complete
// Injected via context by the controller
type MarkStepCompleteFunc func(ctx context.Context, reason string) (string, error)

// CreateCompletionTools creates the completion virtual tools
func CreateCompletionTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	markStepCompleteTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "mark_step_complete",
			Description: "Signal that the step's objective has been fully achieved. Call this ONCE when all required work is done and you've verified the objective is met. Without calling this, the step will continue iterating.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Summary of what was accomplished and why the objective is met.",
					},
				},
				"required": []string{"reason"},
			}),
		},
	}
	tools = append(tools, markStepCompleteTool)

	return tools
}

// CreateCompletionToolExecutors creates the execution functions for completion tools
// Note: These executors require context injection from the controller to work
func CreateCompletionToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["mark_step_complete"] = handleMarkStepComplete

	return executors
}

// handleMarkStepComplete marks the current step as complete
func handleMarkStepComplete(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract arguments
	reason, ok := args["reason"].(string)
	if !ok || reason == "" {
		return "", fmt.Errorf("reason is required")
	}

	// Get the mark step complete function from context
	markCompleteFunc, ok := ctx.Value(MarkStepCompleteKey).(MarkStepCompleteFunc)
	if !ok || markCompleteFunc == nil {
		return "", fmt.Errorf("mark step complete function not available in context - this tool can only be used within a todo task orchestrator")
	}

	// Execute the mark complete
	return markCompleteFunc(ctx, reason)
}
