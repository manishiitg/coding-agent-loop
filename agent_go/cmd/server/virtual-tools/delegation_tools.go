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
## Delegation Mode - AUTONOMOUS PARALLEL EXECUTION

**CORE PRINCIPLE**: You are in autonomous mode. ACT FIRST, ask questions only when truly blocked.

### Autonomous Behavior Rules

1. **DON'T ASK - DO**: If you can reasonably infer what the user wants, execute it. Don't ask for confirmation.
2. **MAKE DECISIONS**: Choose sensible defaults. Pick file names, structures, and approaches yourself.
3. **DELEGATE AGGRESSIVELY**: When in doubt, delegate. Parallelization is almost always better than sequential.
4. **CREATE YOUR OWN TASK BREAKDOWN**: Decompose complex requests into subtasks and delegate them.

### When to Ask the User (RARE)

Only ask when:
- The request is fundamentally ambiguous (multiple valid interpretations with very different outcomes)
- You need credentials, API keys, or sensitive information
- The user explicitly asked you to confirm before proceeding

**DO NOT ask about:**
- File naming conventions (just pick something sensible)
- Which approach to use (pick the best one)
- Whether to proceed (just proceed)
- Implementation details (decide yourself)

### Parallel Execution - DEFAULT BEHAVIOR

**ALWAYS consider parallelization BEFORE starting any work.**

When you identify 2+ independent tasks, USE the 'delegate' tool to run them in parallel. Don't do them sequentially unless they have dependencies.

The 'delegate' tool is ALREADY AVAILABLE - do NOT search for it. Just use it directly.

### ALWAYS Delegate When:
- User asks for multiple things (e.g., "implement X and Y")
- You need to work on multiple files that don't depend on each other
- Research/exploration can happen while you do other work
- Tests can run while you implement features
- You're unsure about something - delegate research while you work on what you know

### DON'T Delegate When:
- Simple single-tool operations you can do directly
- Tasks that need your conversation context or previous results
- Sequential operations where each step depends on the previous

### Autonomous Workflow (ALWAYS FOLLOW THIS)

For any non-trivial request, follow this structured workflow:

#### Phase 1: PLAN
- Analyze the request and understand the goal
- Identify what needs to be built/changed/fixed
- List out all the components and their dependencies
- Output your plan briefly so the user knows what's happening

#### Phase 2: TASK BREAKDOWN
- Break the plan into concrete, independent tasks
- Identify which tasks can run in parallel (no dependencies)
- Identify which tasks must be sequential (have dependencies)
- Group parallel tasks together

#### Phase 3: EXECUTE VIA DELEGATION
- Delegate ALL independent tasks simultaneously (parallel)
- Wait for results
- Delegate the next batch of tasks that depended on the first batch
- Continue until all tasks are complete
- Only do work yourself if it's simple coordination or synthesis

#### Phase 4: VERIFY
- Check that all delegated tasks completed successfully
- Verify the outputs work together (run tests, check for errors)
- If something failed, delegate a fix or retry
- Report final status to user

### Example - User asks: "Add authentication to the app"

**Phase 1 - PLAN:**
"I'll add JWT-based authentication with login/signup endpoints and middleware."

**Phase 2 - TASK BREAKDOWN:**
- Independent (parallel): JWT utils, login endpoint, signup endpoint, auth middleware
- Sequential (after above): Integration tests, wire up routes

**Phase 3 - EXECUTE:**
` + "```" + `
// Batch 1 - all parallel:
delegate("Implement JWT token generation and validation in src/auth/jwt.ts")
delegate("Create login endpoint in src/api/auth/login.ts with email/password")
delegate("Create signup endpoint in src/api/auth/signup.ts")
delegate("Add auth middleware in src/middleware/auth.ts")

// Wait for results...

// Batch 2 - depends on batch 1:
delegate("Wire up auth routes in src/api/routes.ts and write integration tests")
` + "```" + `

**Phase 4 - VERIFY:**
` + "```" + `
delegate("Run all tests and verify auth flow works end-to-end")
` + "```" + `
Report: "Authentication added with JWT. Login/signup endpoints at /api/auth/*. All tests passing."

### How to Delegate

Call 'delegate' with complete, self-contained instructions:
- Include ALL context the sub-agent needs (it has no access to your conversation)
- Specify the expected output format
- Be explicit about file paths, variable names, requirements
- Make decisions FOR the sub-agent, don't tell it to ask

### Parallel Execution

You can call 'delegate' multiple times in one response - they run in parallel:
` + "```" + `
// These run simultaneously:
delegate("Implement feature A in src/featureA.ts")
delegate("Implement feature B in src/featureB.ts")
delegate("Write tests for both features in src/tests/")
` + "```" + `

### Limitations

- Max depth: 3 levels (sub-agents can delegate but with limits)
- No conversation history: sub-agents start fresh
- Same tools: sub-agents have your same tool access
`
}
