package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetSubAgentToolCategory returns the category name for sub-agent tools
func GetSubAgentToolCategory() string {
	return "sub_agent_tools"
}

// Context keys for sub-agent tool execution
type subAgentContextKey string

const (
	// ExecutePredefinedSubAgentKey is the context key for the predefined sub-agent execution function
	ExecutePredefinedSubAgentKey subAgentContextKey = "execute_predefined_sub_agent"
	// ExecuteGenericAgentKey is the context key for the generic agent execution function
	ExecuteGenericAgentKey subAgentContextKey = "execute_generic_agent"
	// MarkStepCompleteKey is the context key for the step completion function
	MarkStepCompleteKey subAgentContextKey = "mark_step_complete"
	// PredefinedRoutesKey is the context key for available predefined routes
	PredefinedRoutesKey subAgentContextKey = "predefined_routes"
)

// SubAgentResult represents the result of a sub-agent execution
type SubAgentResult struct {
	Success       bool      `json:"success"`
	TodoID        string    `json:"todo_id"`
	RouteID       string    `json:"route_id,omitempty"`
	AgentType     string    `json:"agent_type"` // "predefined" or "generic"
	Result        string    `json:"result"`
	Error         string    `json:"error,omitempty"`
	ExecutionTime string    `json:"execution_time"`
	CompletedAt   time.Time `json:"completed_at"`
}

// StepCompletionResult represents the result of marking a step complete
type StepCompletionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Reason  string `json:"reason"`
}

// ExecutePredefinedSubAgentFunc is the function signature for executing predefined sub-agents
// Injected via context by the controller
type ExecutePredefinedSubAgentFunc func(ctx context.Context, routeID, todoID, instructions, successCriteria string) (string, error)

// ExecuteGenericAgentFunc is the function signature for executing generic agents
// Injected via context by the controller
type ExecuteGenericAgentFunc func(ctx context.Context, todoID, instructions, successCriteria string) (string, error)

// MarkStepCompleteFunc is the function signature for marking the step complete
// Injected via context by the controller
type MarkStepCompleteFunc func(ctx context.Context, reason string) error

// CreateSubAgentTools creates the sub-agent calling virtual tools
func CreateSubAgentTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	// call_sub_agent tool - Execute a predefined sub-agent
	callSubAgentTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "call_sub_agent",
			Description: "Execute a predefined sub-agent to perform a specific task. The sub-agent will run to completion and return its result. Use this for tasks that match one of the predefined agent routes.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"route_id": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the predefined route/agent to execute (from the available routes in your context)",
					},
					"todo_id": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the todo task this execution is for (for tracking and event emission)",
					},
					"instructions": map[string]interface{}{
						"type":        "string",
						"description": "Detailed instructions for what the sub-agent should accomplish. Be specific about inputs, expected outputs, and any constraints.",
					},
					"success_criteria": map[string]interface{}{
						"type":        "string",
						"description": "How to verify the task was completed successfully. Include specific checks, file existence, data validation, etc.",
					},
				},
				"required": []string{"route_id", "todo_id", "instructions", "success_criteria"},
			}),
		},
	}
	tools = append(tools, callSubAgentTool)

	// call_generic_agent tool - Execute a generic agent for ad-hoc tasks
	callGenericAgentTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "call_generic_agent",
			Description: "Execute a generic agent for ad-hoc tasks that don't match predefined routes. The agent will use available tools to complete the task. Use this sparingly - prefer predefined routes when available.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"todo_id": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the todo task this execution is for (for tracking and event emission)",
					},
					"instructions": map[string]interface{}{
						"type":        "string",
						"description": "Detailed instructions for what the agent should accomplish. Be very specific since there's no predefined context.",
					},
					"success_criteria": map[string]interface{}{
						"type":        "string",
						"description": "How to verify the task was completed successfully. Include specific checks the agent should perform.",
					},
				},
				"required": []string{"todo_id", "instructions", "success_criteria"},
			}),
		},
	}
	tools = append(tools, callGenericAgentTool)

	// mark_step_complete tool - Signal that the entire step is complete
	markStepCompleteTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "mark_step_complete",
			Description: "Call this when ALL todos for this step have been completed and the step's objective has been met. This signals the controller that no more work is needed for this step.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"reason": map[string]interface{}{
						"type":        "string",
						"description": "Explanation of why the step is complete. Summarize what was accomplished and how the objective was met.",
					},
				},
				"required": []string{"reason"},
			}),
		},
	}
	tools = append(tools, markStepCompleteTool)

	return tools
}

// CreateSubAgentToolExecutors creates the execution functions for sub-agent tools
// Note: These executors require context injection from the controller to work
func CreateSubAgentToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["call_sub_agent"] = handleCallSubAgent
	executors["call_generic_agent"] = handleCallGenericAgent
	executors["mark_step_complete"] = handleMarkStepComplete

	return executors
}

// handleCallSubAgent executes a predefined sub-agent
func handleCallSubAgent(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract arguments
	routeID, ok := args["route_id"].(string)
	if !ok || routeID == "" {
		return "", fmt.Errorf("route_id is required")
	}

	todoID, ok := args["todo_id"].(string)
	if !ok || todoID == "" {
		return "", fmt.Errorf("todo_id is required")
	}

	instructions, ok := args["instructions"].(string)
	if !ok || instructions == "" {
		return "", fmt.Errorf("instructions are required")
	}

	successCriteria, ok := args["success_criteria"].(string)
	if !ok || successCriteria == "" {
		return "", fmt.Errorf("success_criteria is required")
	}

	// Get the execution function from context
	executeFunc, ok := ctx.Value(ExecutePredefinedSubAgentKey).(ExecutePredefinedSubAgentFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("predefined sub-agent execution function not available in context - this tool can only be used within a todo task orchestrator")
	}

	startTime := time.Now()

	// Execute the sub-agent
	result, err := executeFunc(ctx, routeID, todoID, instructions, successCriteria)

	executionTime := time.Since(startTime)

	// Build result
	subAgentResult := SubAgentResult{
		Success:       err == nil,
		TodoID:        todoID,
		RouteID:       routeID,
		AgentType:     "predefined",
		Result:        result,
		ExecutionTime: executionTime.String(),
		CompletedAt:   time.Now(),
	}

	if err != nil {
		subAgentResult.Error = err.Error()
	}

	resultJSON, _ := json.MarshalIndent(subAgentResult, "", "  ")
	return string(resultJSON), nil
}

// handleCallGenericAgent executes a generic agent
func handleCallGenericAgent(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract arguments
	todoID, ok := args["todo_id"].(string)
	if !ok || todoID == "" {
		return "", fmt.Errorf("todo_id is required")
	}

	instructions, ok := args["instructions"].(string)
	if !ok || instructions == "" {
		return "", fmt.Errorf("instructions are required")
	}

	successCriteria, ok := args["success_criteria"].(string)
	if !ok || successCriteria == "" {
		return "", fmt.Errorf("success_criteria is required")
	}

	// Get the execution function from context
	executeFunc, ok := ctx.Value(ExecuteGenericAgentKey).(ExecuteGenericAgentFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("generic agent execution function not available in context - this tool can only be used within a todo task orchestrator")
	}

	startTime := time.Now()

	// Execute the generic agent
	result, err := executeFunc(ctx, todoID, instructions, successCriteria)

	executionTime := time.Since(startTime)

	// Build result
	subAgentResult := SubAgentResult{
		Success:       err == nil,
		TodoID:        todoID,
		AgentType:     "generic",
		Result:        result,
		ExecutionTime: executionTime.String(),
		CompletedAt:   time.Now(),
	}

	if err != nil {
		subAgentResult.Error = err.Error()
	}

	resultJSON, _ := json.MarshalIndent(subAgentResult, "", "  ")
	return string(resultJSON), nil
}

// handleMarkStepComplete signals that the step is complete
func handleMarkStepComplete(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract arguments
	reason, ok := args["reason"].(string)
	if !ok || reason == "" {
		return "", fmt.Errorf("reason is required")
	}

	// Get the completion function from context
	completeFunc, ok := ctx.Value(MarkStepCompleteKey).(MarkStepCompleteFunc)
	if !ok || completeFunc == nil {
		return "", fmt.Errorf("step completion function not available in context - this tool can only be used within a todo task orchestrator")
	}

	// Mark the step as complete
	err := completeFunc(ctx, reason)

	result := StepCompletionResult{
		Success: err == nil,
		Reason:  reason,
	}

	if err != nil {
		result.Message = fmt.Sprintf("Failed to mark step complete: %v", err)
	} else {
		result.Message = "Step marked as complete successfully"
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}
