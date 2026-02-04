package virtualtools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// GetDelegationToolCategory returns the category name for delegation tools
// Using "human" category makes the tool directly available (not a code tool) in:
// - Code execution mode: human category tools are always directly callable
// - Tool search mode: human category tools are immediately available without searching
func GetDelegationToolCategory() string {
	return "human"
}

// Context keys for delegation tool execution
type delegationContextKey string

const (
	// ExecuteDelegatedTaskKey is the context key for the delegation execution function
	ExecuteDelegatedTaskKey delegationContextKey = "execute_delegated_task"
	// DelegationDepthKey is the context key for tracking delegation depth
	DelegationDepthKey delegationContextKey = "delegation_depth"
	// MaxDelegationDepth is the maximum allowed delegation depth to prevent infinite recursion
	MaxDelegationDepth = 3
	// DelegationTimeout is the timeout for a single delegation (sub-agent execution)
	// This is longer than typical tool timeout since sub-agents can run complex multi-turn tasks
	DelegationTimeout = 30 * time.Minute
)

// DelegationResult represents the result of a delegated task execution
type DelegationResult struct {
	Success       bool      `json:"success"`
	Result        string    `json:"result"`
	Error         string    `json:"error,omitempty"`
	ExecutionTime string    `json:"execution_time"`
	CompletedAt   time.Time `json:"completed_at"`
	Depth         int       `json:"depth"`
}

// ExecuteDelegatedTaskFunc is the function signature for executing delegated tasks
// Injected via context by the server
type ExecuteDelegatedTaskFunc func(ctx context.Context, instruction string) (string, error)

// CreateDelegationTools creates the delegation virtual tool
func CreateDelegationTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	// delegate tool - Execute a sub-agent to handle a task
	delegateTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "delegate",
			Description: "Delegate a task to a sub-agent. The sub-agent will have access to the same tools as you and will execute the task independently. Use this when you need to break down a complex task into smaller pieces or when a subtask can be handled autonomously. The sub-agent will run to completion and return its result.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"instruction": map[string]interface{}{
						"type":        "string",
						"description": "Clear, detailed instructions for what the sub-agent should accomplish. Be specific about the goal, any constraints, and what output is expected.",
					},
				},
				"required": []string{"instruction"},
			}),
		},
	}
	tools = append(tools, delegateTool)

	return tools
}

// CreateDelegationToolExecutors creates the execution functions for delegation tools
// Note: These executors require context injection from the server to work
func CreateDelegationToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["delegate"] = handleDelegate

	return executors
}

// handleDelegate executes a delegated task via sub-agent
func handleDelegate(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract instruction argument
	instruction, ok := args["instruction"].(string)
	if !ok || instruction == "" {
		return "", fmt.Errorf("instruction is required")
	}

	// Check delegation depth to prevent infinite recursion
	currentDepth := 0
	if depth, ok := ctx.Value(DelegationDepthKey).(int); ok {
		currentDepth = depth
	}

	if currentDepth >= MaxDelegationDepth {
		return "", fmt.Errorf("maximum delegation depth (%d) reached - cannot delegate further to prevent infinite recursion", MaxDelegationDepth)
	}

	// Get the execution function from context
	executeFunc, ok := ctx.Value(ExecuteDelegatedTaskKey).(ExecuteDelegatedTaskFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("delegation execution function not available - delegation mode may not be enabled")
	}

	log.Printf("[DELEGATION] Executing delegated task at depth %d: %s", currentDepth+1, truncateString(instruction, 100))

	startTime := time.Now()

	// Increment depth in context for the sub-agent
	subCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)

	// Execute the delegated task
	result, err := executeFunc(subCtx, instruction)

	executionTime := time.Since(startTime)

	// Build result
	delegationResult := DelegationResult{
		Success:       err == nil,
		Result:        result,
		ExecutionTime: executionTime.String(),
		CompletedAt:   time.Now(),
		Depth:         currentDepth + 1,
	}

	if err != nil {
		delegationResult.Error = err.Error()
		log.Printf("[DELEGATION] Delegated task failed at depth %d: %v", currentDepth+1, err)
	} else {
		log.Printf("[DELEGATION] Delegated task completed at depth %d in %s", currentDepth+1, executionTime)
	}

	resultJSON, _ := json.MarshalIndent(delegationResult, "", "  ")
	return string(resultJSON), nil
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// GetDelegationInstructions returns system prompt instructions for delegation mode
func GetDelegationInstructions() string {
	return `
## Delegation Mode

IMPORTANT: The 'delegate' tool is ALREADY AVAILABLE - do NOT search for it. Just use it directly.

You have the 'delegate' tool to spawn sub-agents for handling tasks independently.

### When to Delegate

**DO delegate for:**
- Independent research tasks (e.g., "Research library X" while you work on something else)
- File operations on multiple unrelated files in parallel
- Running multiple tool calls that don't depend on each other
- Complex subtasks that need focused, multi-step execution
- Tasks requiring exploration of different approaches simultaneously

**DON'T delegate for:**
- Simple single-tool operations you can do directly
- Tasks that need your conversation context or previous results
- Sequential operations where each step depends on the previous
- Quick lookups or simple questions

### Examples

**Good delegation:**
- "Implement the login form" + "Implement the signup form" (parallel, independent)
- "Search for all TODO comments in src/" + "Read the README for project structure"
- "Write unit tests for UserService" while you implement another feature

**Bad delegation:**
- Delegating a single file read (just do it yourself)
- Delegating something that needs info from your current conversation
- Breaking a sequential task into delegations that need each other's results

### How to Delegate

Call 'delegate' with complete, self-contained instructions:
- Include ALL context the sub-agent needs (it has no access to your conversation)
- Specify the expected output format
- Be explicit about file paths, variable names, requirements

### Parallel Execution

You can call 'delegate' multiple times in one response - they run in parallel:
` + "```" + `
// These run simultaneously:
delegate("Implement feature A in src/featureA.ts")
delegate("Implement feature B in src/featureB.ts")
` + "```" + `

### Limitations

- Max depth: 3 levels (sub-agents cannot delegate further)
- No conversation history: sub-agents start fresh
- Same tools: sub-agents have your same tool access
`
}
