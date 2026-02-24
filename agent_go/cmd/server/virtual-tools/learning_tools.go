package virtualtools

import (
	"context"
	"fmt"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetLearningToolCategory returns the category name for learning tools
func GetLearningToolCategory() string {
	return "learning_tools"
}

// Context key for learning tool execution
type learningContextKey string

const (
	// SaveLearningKey is the context key for the save learning function
	SaveLearningKey learningContextKey = "save_learning"
)

// SaveLearningFunc is the function signature for saving orchestrator learnings
// Injected via context by the controller
type SaveLearningFunc func(ctx context.Context, category, insight string) (string, error)

// CreateLearningTools creates the learning virtual tools
func CreateLearningTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	saveLearningTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "save_learning",
			Description: "Save an actionable insight or learning from the current orchestration session. Use this to capture important patterns, discoveries, or strategies that would improve future runs of this step.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Category of the learning to help organize insights",
						"enum":        []string{"routing", "task_planning", "error_recovery", "delegation", "optimization", "general"},
					},
					"insight": map[string]interface{}{
						"type":        "string",
						"description": "The actionable learning or insight to save. Be specific and include context so it's useful in future runs.",
					},
				},
				"required": []string{"category", "insight"},
			}),
		},
	}
	tools = append(tools, saveLearningTool)

	return tools
}

// CreateLearningToolExecutors creates the execution functions for learning tools
// Note: These executors require context injection from the controller to work
func CreateLearningToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["save_learning"] = handleSaveLearning

	return executors
}

// handleSaveLearning saves an orchestrator learning
func handleSaveLearning(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract arguments
	category, ok := args["category"].(string)
	if !ok || category == "" {
		return "", fmt.Errorf("category is required")
	}

	insight, ok := args["insight"].(string)
	if !ok || insight == "" {
		return "", fmt.Errorf("insight is required")
	}

	// Validate category
	validCategories := map[string]bool{
		"routing": true, "task_planning": true, "error_recovery": true,
		"delegation": true, "optimization": true, "general": true,
	}
	if !validCategories[category] {
		return "", fmt.Errorf("invalid category '%s': must be one of routing, task_planning, error_recovery, delegation, optimization, general", category)
	}

	// Get the save learning function from context
	saveFunc, ok := ctx.Value(SaveLearningKey).(SaveLearningFunc)
	if !ok || saveFunc == nil {
		return "", fmt.Errorf("save learning function not available in context - this tool can only be used within a todo task orchestrator")
	}

	// Execute the save
	return saveFunc(ctx, category, insight)
}
