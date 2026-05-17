package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
	// PreferredTierContextKey is the context key for preferred LLM tier override (1/2/3)
	PreferredTierContextKey subAgentContextKey = "preferred_tier"
	// TierSelectionRequiredKey is the context key that signals handlers to reject
	// sub-agent calls missing a valid preferred_tier. Set by the orchestrator wrapper
	// when dynamic tier selection is active.
	TierSelectionRequiredKey subAgentContextKey = "tier_selection_required"
	// SubAgentLLMContextKey is the context key for direct LLM override for sub-agents (works in both tiered and manual modes)
	SubAgentLLMContextKey subAgentContextKey = "sub_agent_llm"
	// SubAgentShareBrowserKey is the context key for controlling browser session isolation in sub-agents
	SubAgentShareBrowserKey subAgentContextKey = "share_browser"
	// SubAgentIsolatedSessionIDKey is the context key for the isolated MCP session ID (set when share_browser=false)
	SubAgentIsolatedSessionIDKey subAgentContextKey = "isolated_session_id"
	// SubAgentMessageSequenceRestartKey is the context key for forcing a message_sequence route to start fresh.
	SubAgentMessageSequenceRestartKey subAgentContextKey = "message_sequence_restart"
	// GetSubAgentConversationKey is the context key for the get_sub_agent_conversation function
	GetSubAgentConversationKey subAgentContextKey = "get_sub_agent_conversation"
	// RouteDescriptionsKey is the context key for a map of route_id -> description string
	RouteDescriptionsKey subAgentContextKey = "route_descriptions"
)

// GetSubAgentConversationFunc is the function signature for retrieving sub-agent conversation history.
// todoID identifies the sub-agent call, fromLastX is how many entries to return,
// offsetLastX skips that many entries from the tail before applying fromLastX (for paging).
type GetSubAgentConversationFunc func(ctx context.Context, todoID string, fromLastX, offsetLastX int) (string, error)

// SubAgentResult represents the result of a sub-agent execution
type SubAgentResult struct {
	Success       bool      `json:"success"`
	TodoID        string    `json:"todo_id"`
	RouteID       string    `json:"route_id,omitempty"`
	AgentType     string    `json:"agent_type"` // "predefined" or "generic"
	Result        string    `json:"result"`
	Error         string    `json:"error,omitempty"`
	Hint          string    `json:"hint,omitempty"`
	RetryAttempts int       `json:"retry_attempts"` // Number of attempts the sub-agent made (including retries)
	ExecutionTime string    `json:"execution_time"`
	CompletedAt   time.Time `json:"completed_at"`
}

// maxSubAgentRetryAttempts is the number of retry attempts a sub-agent makes internally before returning failure
const maxSubAgentRetryAttempts = 3

// ExecutePredefinedSubAgentFunc is the function signature for executing predefined sub-agents
// Injected via context by the controller
type ExecutePredefinedSubAgentFunc func(ctx context.Context, routeID, todoID, instructions string) (string, error)

// ExecuteGenericAgentFunc is the function signature for executing generic agents
// Injected via context by the controller
type ExecuteGenericAgentFunc func(ctx context.Context, todoID, instructions string) (string, error)

// CreateSubAgentTools creates the sub-agent calling virtual tools.
// preferred_tier is always a REQUIRED parameter on both sub-agent tools — the
// orchestrator must explicitly reason about task difficulty on every delegation.
// When the workflow has no tier resolver or the step pins an ExecutionLLM, the
// tier value is informational (inherited/pinned LLM is used regardless), but the
// parameter remains required for prompt discipline.
func CreateSubAgentTools() []llmtypes.Tool {
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
		"share_browser": map[string]interface{}{
			"type":        "boolean",
			"description": "Whether the sub-agent shares the parent's browser session (Playwright/agent-browser) or gets an isolated browser. Default: true (shared). Set to false for parallel browsing, different auth contexts, or to avoid state interference.",
		},
		"preferred_tier": map[string]interface{}{
			"type":        "integer",
			"description": "REQUIRED. LLM reasoning tier for this sub-agent. 1 = high reasoning (complex/novel tasks), 2 = medium reasoning (routine tasks), 3 = low reasoning (simple/validation tasks). You must pick a tier for every call based on the task's difficulty.",
			"enum":        []int{1, 2, 3},
		},
		"message_sequence_restart": map[string]interface{}{
			"type":        "boolean",
			"description": "Optional. Only for message_sequence routes. If true, archive the existing route conversation and replay the configured item queue from the beginning. Default: false, which resumes the existing route conversation and sends instructions as the re-entry message.",
		},
	}
	callSubAgentRequired := []string{"route_id", "todo_id", "instructions", "preferred_tier"}
	callSubAgentTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "call_sub_agent",
			Description: "Execute a predefined sub-agent to perform a specific task. The sub-agent will run to completion and return its result. For message_sequence routes, repeated calls resume the route conversation unless message_sequence_restart is true.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":       "object",
				"properties": callSubAgentProperties,
				"required":   callSubAgentRequired,
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
		"share_browser": map[string]interface{}{
			"type":        "boolean",
			"description": "Whether the sub-agent shares the parent's browser session (Playwright/agent-browser) or gets an isolated browser. Default: true (shared). Set to false for parallel browsing, different auth contexts, or to avoid state interference.",
		},
	}
	callGenericAgentProperties["preferred_tier"] = map[string]interface{}{
		"type":        "integer",
		"description": "REQUIRED. LLM reasoning tier for this sub-agent. 1 = high reasoning (complex/novel tasks), 2 = medium reasoning (routine tasks), 3 = low reasoning (simple/validation tasks). You must pick a tier for every call based on the task's difficulty.",
		"enum":        []int{1, 2, 3},
	}
	callGenericAgentRequired := []string{"todo_id", "instructions", "preferred_tier"}
	callGenericAgentTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "call_generic_agent",
			Description: "Execute a generic agent for ad-hoc tasks that don't match predefined routes. The agent will use available tools to complete the task. Use this sparingly - prefer predefined routes when available.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type":       "object",
				"properties": callGenericAgentProperties,
				"required":   callGenericAgentRequired,
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

	// get_route_description tool - Get the full description/instructions for a predefined route
	getRouteDescriptionTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_route_description",
			Description: "Get the full description and instructions for a predefined sub-agent route. Call this before delegating to understand what the route does, what instructions to pass, and whether it is a message_sequence route with resume/restart behavior.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"route_id": map[string]interface{}{
						"type":        "string",
						"description": "The ID of the predefined route to get the description for",
					},
				},
				"required": []string{"route_id"},
			}),
		},
	}
	tools = append(tools, getRouteDescriptionTool)

	return tools
}

// CreateSubAgentToolExecutors creates the execution functions for sub-agent tools
// Note: These executors require context injection from the controller to work
func CreateSubAgentToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["call_sub_agent"] = handleCallSubAgent
	executors["call_generic_agent"] = handleCallGenericAgent
	executors["get_sub_agent_conversation"] = handleGetSubAgentConversation
	executors["get_route_description"] = handleGetRouteDescription
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

	tierRequired, _ := ctx.Value(TierSelectionRequiredKey).(bool)
	preferredTierF, hasTier := args["preferred_tier"].(float64)
	validTier := hasTier && int(preferredTierF) >= 1 && int(preferredTierF) <= 3
	if tierRequired && !validTier {
		return "", fmt.Errorf("preferred_tier is required and must be 1, 2, or 3 (1=high reasoning, 2=medium, 3=low) — pick a tier based on task difficulty")
	}
	if validTier {
		ctx = context.WithValue(ctx, PreferredTierContextKey, int(preferredTierF))
	}

	// Extract share_browser param (defaults to true — shared browser)
	if sb, ok := args["share_browser"].(bool); ok && !sb {
		ctx = context.WithValue(ctx, SubAgentShareBrowserKey, false)
	}
	if restart, ok := args["message_sequence_restart"].(bool); ok && restart {
		ctx = context.WithValue(ctx, SubAgentMessageSequenceRestartKey, true)
	}

	// Get the execution function from context
	executeFunc, ok := ctx.Value(ExecutePredefinedSubAgentKey).(ExecutePredefinedSubAgentFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("predefined sub-agent execution function not available in context - this tool can only be used within a todo task orchestrator")
	}

	startTime := time.Now()

	// Execute the sub-agent
	result, err := executeFunc(ctx, routeID, todoID, instructions)

	executionTime := time.Since(startTime)

	// Build result
	// Determine success: err must be nil AND result must not contain failure indicators
	// (execution failures can be auto-approved by sub-agent flow, returning nil error with empty/failed result)
	isSuccess := err == nil && !isSubAgentResultFailure(result)

	subAgentResult := SubAgentResult{
		Success:       isSuccess,
		TodoID:        todoID,
		RouteID:       routeID,
		AgentType:     "predefined",
		Result:        result,
		RetryAttempts: maxSubAgentRetryAttempts,
		ExecutionTime: executionTime.String(),
		CompletedAt:   time.Now(),
	}

	if !isSuccess {
		if err != nil {
			subAgentResult.Error = err.Error()
		} else {
			// No Go error, but result contains failure details (e.g. "ERROR: agent execution failed: ...")
			subAgentResult.Error = result
		}
		subAgentResult.Hint = fmt.Sprintf("Sub-agent failed after %d internal retry attempts. Consider using get_sub_agent_conversation to inspect what went wrong, review LEARNING HISTORY, and execute the task yourself — you have a higher-reasoning LLM and the same tool access.", maxSubAgentRetryAttempts)
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

	tierRequired, _ := ctx.Value(TierSelectionRequiredKey).(bool)
	preferredTierF, hasTier := args["preferred_tier"].(float64)
	validTier := hasTier && int(preferredTierF) >= 1 && int(preferredTierF) <= 3
	if tierRequired && !validTier {
		return "", fmt.Errorf("preferred_tier is required and must be 1, 2, or 3 (1=high reasoning, 2=medium, 3=low) — pick a tier based on task difficulty")
	}
	if validTier {
		ctx = context.WithValue(ctx, PreferredTierContextKey, int(preferredTierF))
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
	result, err := executeFunc(ctx, todoID, instructions)

	executionTime := time.Since(startTime)

	// Build result
	// Determine success: err must be nil AND result must not contain failure indicators
	isSuccess := err == nil && !isSubAgentResultFailure(result)

	subAgentResult := SubAgentResult{
		Success:       isSuccess,
		TodoID:        todoID,
		AgentType:     "generic",
		Result:        result,
		RetryAttempts: maxSubAgentRetryAttempts,
		ExecutionTime: executionTime.String(),
		CompletedAt:   time.Now(),
	}

	if !isSuccess {
		if err != nil {
			subAgentResult.Error = err.Error()
		} else {
			// No Go error, but result contains failure details (e.g. "ERROR: agent execution failed: ...")
			subAgentResult.Error = result
		}
		subAgentResult.Hint = fmt.Sprintf("Sub-agent failed after %d internal retry attempts. Consider using get_sub_agent_conversation to inspect what went wrong, review LEARNING HISTORY, and execute the task yourself — you have a higher-reasoning LLM and the same tool access.", maxSubAgentRetryAttempts)
	}

	resultJSON, _ := json.MarshalIndent(subAgentResult, "", "  ")
	return string(resultJSON), nil
}

// isSubAgentResultFailure checks if a sub-agent result string indicates failure
// This catches cases where execution failed but was auto-approved (nil error with empty/failed result)
func isSubAgentResultFailure(result string) bool {
	if strings.TrimSpace(result) == "" {
		return true
	}
	lower := strings.ToLower(result)
	return strings.Contains(lower, "failed:") ||
		strings.Contains(lower, "failed validation after") ||
		strings.Contains(lower, "execution failed") ||
		strings.Contains(lower, "error:")
}

// handleGetRouteDescription returns the full description of a predefined route
func handleGetRouteDescription(ctx context.Context, args map[string]interface{}) (string, error) {
	routeID, ok := args["route_id"].(string)
	if !ok || routeID == "" {
		return "", fmt.Errorf("route_id is required")
	}

	// Get route descriptions map from context
	descriptionsMap, ok := ctx.Value(RouteDescriptionsKey).(map[string]string)
	if !ok || descriptionsMap == nil {
		return "", fmt.Errorf("route descriptions not available in context")
	}

	description, exists := descriptionsMap[routeID]
	if !exists {
		// List available routes for the error message
		var available []string
		for id := range descriptionsMap {
			available = append(available, id)
		}
		return "", fmt.Errorf("route '%s' not found. Available routes: %s", routeID, strings.Join(available, ", "))
	}

	result := map[string]string{
		"route_id":    routeID,
		"description": description,
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
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
