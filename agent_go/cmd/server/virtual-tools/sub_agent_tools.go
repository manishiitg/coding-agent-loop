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
	// PredefinedRoutesKey is the context key for available predefined routes
	PredefinedRoutesKey subAgentContextKey = "predefined_routes"
	// ValidateTodoExistsKey is the context key for the todo validation function
	ValidateTodoExistsKey subAgentContextKey = "validate_todo_exists"
	// PreferredTierContextKey is the context key for preferred LLM tier override (1/2/3)
	PreferredTierContextKey subAgentContextKey = "preferred_tier"
)

// ValidateTodoExistsFunc is the function signature for validating if a task exists in tasks.md
// Returns (exists bool, totalTasks int, tasksFilePath string, error)
type ValidateTodoExistsFunc func(ctx context.Context, todoID string) (bool, int, string, error)

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

// ExecutePredefinedSubAgentFunc is the function signature for executing predefined sub-agents
// Injected via context by the controller
type ExecutePredefinedSubAgentFunc func(ctx context.Context, routeID, todoID, instructions, successCriteria string) (string, error)

// ExecuteGenericAgentFunc is the function signature for executing generic agents
// Injected via context by the controller
type ExecuteGenericAgentFunc func(ctx context.Context, todoID, instructions, successCriteria string) (string, error)

// CreateSubAgentTools creates the sub-agent calling virtual tools
// If enableTierSelection is true, both tools include an optional preferred_tier parameter (1/2/3)
func CreateSubAgentTools(enableTierSelection bool) []llmtypes.Tool {
	var tools []llmtypes.Tool

	// call_sub_agent tool - Execute a predefined sub-agent
	callSubAgentProperties := map[string]interface{}{
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
	}
	if enableTierSelection {
		callSubAgentProperties["preferred_tier"] = map[string]interface{}{
			"type":        "integer",
			"description": "LLM reasoning tier for this sub-agent. 1 = high reasoning (complex/novel tasks), 2 = medium reasoning (routine tasks), 3 = low reasoning (simple/validation tasks). If omitted, system auto-selects based on learning maturity.",
			"enum":        []int{1, 2, 3},
		}
	}
	callSubAgentTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "call_sub_agent",
			Description: "Execute a predefined sub-agent to perform a specific task. The sub-agent will run to completion and return its result. Use this for tasks that match one of the predefined agent routes.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":       "object",
				"properties": callSubAgentProperties,
				"required":   []string{"route_id", "todo_id", "instructions", "success_criteria"},
			}),
		},
	}
	tools = append(tools, callSubAgentTool)

	// call_generic_agent tool - Execute a generic agent for ad-hoc tasks
	callGenericAgentProperties := map[string]interface{}{
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
	}
	if enableTierSelection {
		callGenericAgentProperties["preferred_tier"] = map[string]interface{}{
			"type":        "integer",
			"description": "LLM reasoning tier for this sub-agent. 1 = high reasoning (complex/novel tasks), 2 = medium reasoning (routine tasks), 3 = low reasoning (simple/validation tasks). If omitted, system auto-selects based on learning maturity.",
			"enum":        []int{1, 2, 3},
		}
	}
	callGenericAgentTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "call_generic_agent",
			Description: "Execute a generic agent for ad-hoc tasks that don't match predefined routes. The agent will use available tools to complete the task. Use this sparingly - prefer predefined routes when available.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":       "object",
				"properties": callGenericAgentProperties,
				"required":   []string{"todo_id", "instructions", "success_criteria"},
			}),
		},
	}
	tools = append(tools, callGenericAgentTool)

	return tools
}

// CreateSubAgentToolExecutors creates the execution functions for sub-agent tools
// Note: These executors require context injection from the controller to work
func CreateSubAgentToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["call_sub_agent"] = handleCallSubAgent
	executors["call_generic_agent"] = handleCallGenericAgent

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

	// Extract preferred_tier if provided (for tiered LLM allocation)
	if preferredTier, ok := args["preferred_tier"].(float64); ok && int(preferredTier) >= 1 && int(preferredTier) <= 3 {
		ctx = context.WithValue(ctx, PreferredTierContextKey, int(preferredTier))
	}

	// VALIDATION: Check if task exists in tasks.md before delegation
	if validateFunc, ok := ctx.Value(ValidateTodoExistsKey).(ValidateTodoExistsFunc); ok && validateFunc != nil {
		exists, totalTasks, tasksFilePath, err := validateFunc(ctx, todoID)
		if err != nil {
			return "", fmt.Errorf("failed to validate task: %w", err)
		}
		if totalTasks == 0 {
			return "", fmt.Errorf("VALIDATION ERROR: Cannot delegate - tasks.md is EMPTY or does not exist. You MUST create tasks.md with task entries first using execute_shell_command before delegating. Expected path: %s", tasksFilePath)
		}
		if !exists {
			return "", fmt.Errorf("VALIDATION ERROR: Task '%s' does not exist in tasks.md. Create it first using execute_shell_command, or use an existing task_id from tasks.md. File path: %s", todoID, tasksFilePath)
		}
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

	// Extract preferred_tier if provided (for tiered LLM allocation)
	if preferredTier, ok := args["preferred_tier"].(float64); ok && int(preferredTier) >= 1 && int(preferredTier) <= 3 {
		ctx = context.WithValue(ctx, PreferredTierContextKey, int(preferredTier))
	}

	// VALIDATION: Check if task exists in tasks.md before delegation
	if validateFunc, ok := ctx.Value(ValidateTodoExistsKey).(ValidateTodoExistsFunc); ok && validateFunc != nil {
		exists, totalTasks, tasksFilePath, err := validateFunc(ctx, todoID)
		if err != nil {
			return "", fmt.Errorf("failed to validate task: %w", err)
		}
		if totalTasks == 0 {
			return "", fmt.Errorf("VALIDATION ERROR: Cannot delegate - tasks.md is EMPTY or does not exist. You MUST create tasks.md with task entries first using execute_shell_command before delegating. Expected path: %s", tasksFilePath)
		}
		if !exists {
			return "", fmt.Errorf("VALIDATION ERROR: Task '%s' does not exist in tasks.md. Create it first using execute_shell_command, or use an existing task_id from tasks.md. File path: %s", todoID, tasksFilePath)
		}
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
