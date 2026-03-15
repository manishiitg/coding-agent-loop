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
	// SubAgentLLMContextKey is the context key for direct LLM override for sub-agents (works in both tiered and manual modes)
	SubAgentLLMContextKey subAgentContextKey = "sub_agent_llm"
	// SubAgentShareBrowserKey is the context key for controlling browser session isolation in sub-agents
	SubAgentShareBrowserKey subAgentContextKey = "share_browser"
	// SubAgentIsolatedSessionIDKey is the context key for the isolated MCP session ID (set when share_browser=false)
	SubAgentIsolatedSessionIDKey subAgentContextKey = "isolated_session_id"
	// GetSubAgentConversationKey is the context key for the get_sub_agent_conversation function
	GetSubAgentConversationKey subAgentContextKey = "get_sub_agent_conversation"
)

// GetSubAgentConversationFunc is the function signature for retrieving sub-agent conversation history.
// todoID identifies the sub-agent call, fromLastX is how many entries to return,
// offsetLastX skips that many entries from the tail before applying fromLastX (for paging).
type GetSubAgentConversationFunc func(ctx context.Context, todoID string, fromLastX, offsetLastX int) (string, error)

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
		"share_browser": map[string]interface{}{
			"type":        "boolean",
			"description": "Whether the sub-agent shares the parent's browser session (Playwright/Camofox) or gets an isolated browser. Default: true (shared). Set to false for parallel browsing, different auth contexts, or to avoid state interference.",
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
		"share_browser": map[string]interface{}{
			"type":        "boolean",
			"description": "Whether the sub-agent shares the parent's browser session (Playwright/Camofox) or gets an isolated browser. Default: true (shared). Set to false for parallel browsing, different auth contexts, or to avoid state interference.",
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

	// get_sub_agent_conversation tool - Query the internal conversation of a previous sub-agent call
	getSubAgentConversationTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_sub_agent_conversation",
			Description: "Get the internal conversation of a sub-agent call — all tool calls, tool results, and reasoning steps. Returns the last 'from_last_x' entries. Use 'offset_last_x' to page backwards through the conversation.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"todo_id": map[string]interface{}{
						"type":        "string",
						"description": "Required. The task ID that was delegated (e.g. 'task-003').",
					},
					"from_last_x": map[string]interface{}{
						"type":        "integer",
						"description": "Required. Number of conversation entries to return from the end.",
					},
					"offset_last_x": map[string]interface{}{
						"type":        "integer",
						"description": "Optional. Skip this many entries from the tail before applying from_last_x. Use to page backwards. Default 0.",
					},
				},
				"required": []string{"todo_id", "from_last_x"},
			}),
		},
	}
	tools = append(tools, getSubAgentConversationTool)

	return tools
}

// CreateSubAgentToolExecutors creates the execution functions for sub-agent tools
// Note: These executors require context injection from the controller to work
func CreateSubAgentToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["call_sub_agent"] = handleCallSubAgent
	executors["call_generic_agent"] = handleCallGenericAgent
	executors["get_sub_agent_conversation"] = handleGetSubAgentConversation

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

	// Extract share_browser param (defaults to true — shared browser)
	if sb, ok := args["share_browser"].(bool); ok && !sb {
		ctx = context.WithValue(ctx, SubAgentShareBrowserKey, false)
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

	// Extract share_browser param (defaults to true — shared browser)
	if sb, ok := args["share_browser"].(bool); ok && !sb {
		ctx = context.WithValue(ctx, SubAgentShareBrowserKey, false)
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

// handleGetSubAgentConversation retrieves the internal conversation of a previous sub-agent call
func handleGetSubAgentConversation(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract todo_id (required)
	todoID, ok := args["todo_id"].(string)
	if !ok || todoID == "" {
		return "", fmt.Errorf("todo_id is required")
	}

	// Extract from_last_x (required, must be > 0)
	fromLastXRaw, ok := args["from_last_x"].(float64)
	if !ok {
		return "", fmt.Errorf("from_last_x is required and must be an integer")
	}
	fromLastX := int(fromLastXRaw)
	if fromLastX <= 0 {
		return "", fmt.Errorf("from_last_x must be greater than 0")
	}

	// Extract offset_last_x (optional, default 0)
	offsetLastX := 0
	if offsetLastXRaw, ok := args["offset_last_x"].(float64); ok {
		offsetLastX = int(offsetLastXRaw)
		if offsetLastX < 0 {
			offsetLastX = 0
		}
	}

	// Get the conversation retrieval function from context
	getConvFunc, ok := ctx.Value(GetSubAgentConversationKey).(GetSubAgentConversationFunc)
	if !ok || getConvFunc == nil {
		return "", fmt.Errorf("get_sub_agent_conversation function not available in context - this tool can only be used within a todo task orchestrator after a sub-agent has been called")
	}

	return getConvFunc(ctx, todoID, fromLastX, offsetLastX)
}
