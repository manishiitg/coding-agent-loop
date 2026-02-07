package virtualtools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/workspace"

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
	// WorkspaceClientKey is the context key for the workspace client (plan file I/O)
	WorkspaceClientKey delegationContextKey = "workspace_client"
	// DelegationTierConfigKey is the context key for the delegation tier configuration
	DelegationTierConfigKey delegationContextKey = "delegation_tier_config"
	// ReasoningLevelKey is the context key for the reasoning level of a delegation
	ReasoningLevelKey delegationContextKey = "reasoning_level"
	// PlanFileFolderPath is the workspace folder for delegation plan files
	PlanFileFolderPath = "Chats/Delegations"
)

// DelegationTierConfig holds provider/model for each reasoning tier
type DelegationTierConfig struct {
	High   *TierModel `json:"high,omitempty"`
	Medium *TierModel `json:"medium,omitempty"`
	Low    *TierModel `json:"low,omitempty"`
}

// TierModel represents a specific provider+model for a tier
type TierModel struct {
	Provider string `json:"provider"`
	ModelID  string `json:"model_id"`
}

// DelegationPlan represents a structured plan with tasks for delegation
type DelegationPlan struct {
	PlanID    string           `json:"plan_id"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Status    string           `json:"status"` // planning, in_progress, completed, failed
	Objective string           `json:"objective"`
	Strategy  string           `json:"strategy"`
	Tasks     []DelegationTask `json:"tasks"`
}

// DelegationTask represents a single task within a delegation plan
type DelegationTask struct {
	ID            string     `json:"id"`          // task-1, task-2, etc.
	Title         string     `json:"title"`
	Description   string     `json:"description"`
	Priority      string     `json:"priority"`    // high, medium, low
	Status        string     `json:"status"`      // pending, in_progress, completed, failed
	Result        string     `json:"result,omitempty"`
	Error         string     `json:"error,omitempty"`
	Notes         string     `json:"notes,omitempty"`
	ExecutionTime string     `json:"execution_time,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

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

// CreateDelegationTools creates all delegation virtual tools
func CreateDelegationTools() []llmtypes.Tool {
	var tools []llmtypes.Tool

	// delegate tool - Execute a sub-agent to handle a task (quick one-off delegation)
	delegateTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "delegate",
			Description: "Delegate a task to a sub-agent for execution. Use for quick one-off tasks. For complex multi-step projects, prefer create_delegation_plan + execute_plan_task instead.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"instruction": map[string]interface{}{
						"type":        "string",
						"description": "Comprehensive, self-contained instructions for the sub-agent. Include all necessary context, requirements, and expected outcomes.",
					},
					"reasoning_level": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"high", "medium", "low"},
						"description": "Optional reasoning tier for this task. 'high' for complex planning/architecture, 'medium' for standard implementation, 'low' for simple tasks like formatting/tests. If not specified, uses the parent agent's model.",
					},
				},
				"required": []string{"instruction"},
			}),
		},
	}
	tools = append(tools, delegateTool)

	// create_delegation_plan - Delegate planning to a sub-agent, then save the plan
	createPlanTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "create_delegation_plan",
			Description: "Delegate planning to a sub-agent that analyzes the objective, creates a strategy, and breaks it into tasks. The plan is saved to workspace (Chats/Delegations/) for tracking. To update an existing plan, pass plan_id along with new context.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"objective": map[string]interface{}{
						"type":        "string",
						"description": "What needs to be built or accomplished. The planner sub-agent will analyze this and create a task breakdown.",
					},
					"context": map[string]interface{}{
						"type":        "string",
						"description": "Optional additional context for the planner: codebase structure, constraints, file paths, tech stack, or any information that helps create a better plan.",
					},
					"plan_id": map[string]interface{}{
						"type":        "string",
						"description": "Optional. If provided, updates an existing plan instead of creating a new one. The planner sub-agent will receive the current plan and incorporate the new objective/context.",
					},
				},
				"required": []string{"objective"},
			}),
		},
	}
	tools = append(tools, createPlanTool)

	// execute_plan_task - Execute a specific task from a plan
	executePlanTaskTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "execute_plan_task",
			Description: "Execute a specific task from a delegation plan by spawning a sub-agent. Automatically updates the plan file with status and results.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"plan_id": map[string]interface{}{
						"type":        "string",
						"description": "The plan ID returned by create_delegation_plan.",
					},
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to execute (e.g., 'task-1').",
					},
					"additional_context": map[string]interface{}{
						"type":        "string",
						"description": "Optional additional context or instructions to append to the task description.",
					},
					"reasoning_level": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"high", "medium", "low"},
						"description": "Optional reasoning tier. 'high' for complex planning/architecture, 'medium' for standard implementation, 'low' for simple tasks. If not specified, uses the parent agent's model.",
					},
				},
				"required": []string{"plan_id", "task_id"},
			}),
		},
	}
	tools = append(tools, executePlanTaskTool)

	// update_plan_task - Manually update a task's status/notes
	updatePlanTaskTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "update_plan_task",
			Description: "Manually update a task's status, notes, or result in a delegation plan without executing it. Useful for marking tasks as completed after manual verification, adding notes, or skipping tasks.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"plan_id": map[string]interface{}{
						"type":        "string",
						"description": "The plan ID.",
					},
					"task_id": map[string]interface{}{
						"type":        "string",
						"description": "The task ID to update (e.g., 'task-1').",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"pending", "in_progress", "completed", "failed"},
						"description": "New status for the task.",
					},
					"notes": map[string]interface{}{
						"type":        "string",
						"description": "Notes to add to the task.",
					},
					"result": map[string]interface{}{
						"type":        "string",
						"description": "Result text for the task.",
					},
				},
				"required": []string{"plan_id", "task_id"},
			}),
		},
	}
	tools = append(tools, updatePlanTaskTool)

	// get_plan_status - Get plan summary and next pending task
	getPlanStatusTool := llmtypes.Tool{
		Type: "function",
		Function: &llmtypes.FunctionDefinition{
			Name:        "get_plan_status",
			Description: "Get the current status of a delegation plan including task progress summary and the next pending task.",
			Parameters: llmtypes.NewParameters(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"plan_id": map[string]interface{}{
						"type":        "string",
						"description": "The plan ID to check status for.",
					},
				},
				"required": []string{"plan_id"},
			}),
		},
	}
	tools = append(tools, getPlanStatusTool)

	return tools
}

// CreateDelegationToolExecutors creates the execution functions for delegation tools
// Note: These executors require context injection from the server to work
func CreateDelegationToolExecutors() map[string]func(ctx context.Context, args map[string]interface{}) (string, error) {
	executors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error))

	executors["delegate"] = handleDelegate
	executors["create_delegation_plan"] = handleCreateDelegationPlan
	executors["execute_plan_task"] = handleExecutePlanTask
	executors["update_plan_task"] = handleUpdatePlanTask
	executors["get_plan_status"] = handleGetPlanStatus

	return executors
}

// handleDelegate executes a delegated task via sub-agent
func handleDelegate(ctx context.Context, args map[string]interface{}) (string, error) {
	// Extract instruction argument
	instruction, ok := args["instruction"].(string)
	if !ok || instruction == "" {
		return "", fmt.Errorf("instruction is required")
	}

	// Extract optional reasoning_level
	reasoningLevel, _ := args["reasoning_level"].(string)

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

	// Increment depth in context for the sub-agent and pass reasoning level
	subCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)
	if reasoningLevel != "" {
		subCtx = context.WithValue(subCtx, ReasoningLevelKey, reasoningLevel)
	}

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

// plannerPrompt is the system instruction for the planner sub-agent
const plannerPrompt = `You are a Technical Planner. Your job is to analyze an objective and produce a structured plan.

You MUST respond with ONLY a valid JSON object (no markdown, no explanation, no code fences). The JSON must have this exact structure:

{
  "strategy": "High-level approach description",
  "tasks": [
    {
      "title": "Short task title",
      "description": "Detailed self-contained instructions a developer can follow without any other context. Include file paths, requirements, and expected outcomes.",
      "priority": "high|medium|low"
    }
  ]
}

Guidelines:
- Break the objective into 3-10 concrete, actionable tasks
- Each task must be self-contained (a sub-agent with no context will execute it)
- Include specific file paths, function names, and technical details when possible
- Priority: "high" for architecture/core logic, "medium" for standard implementation, "low" for tests/docs/formatting
- Order tasks by dependency (tasks that others depend on should come first)
- Strategy should be 1-2 sentences describing the overall approach`

// handleCreateDelegationPlan spawns a planner sub-agent to create or update a plan
func handleCreateDelegationPlan(ctx context.Context, args map[string]interface{}) (string, error) {
	objective, _ := args["objective"].(string)
	if objective == "" {
		return "", fmt.Errorf("objective is required")
	}
	additionalContext, _ := args["context"].(string)
	existingPlanID, _ := args["plan_id"].(string)

	// Check delegation depth
	currentDepth := 0
	if depth, ok := ctx.Value(DelegationDepthKey).(int); ok {
		currentDepth = depth
	}
	if currentDepth >= MaxDelegationDepth {
		return "", fmt.Errorf("maximum delegation depth (%d) reached", MaxDelegationDepth)
	}

	// Get the execution function from context
	executeFunc, ok := ctx.Value(ExecuteDelegatedTaskKey).(ExecuteDelegatedTaskFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("delegation execution function not available")
	}

	// Build the planner instruction
	var plannerInstruction string

	if existingPlanID != "" {
		// Update mode: load existing plan and ask planner to revise it
		existingPlan, err := loadPlanFromWorkspace(ctx, existingPlanID)
		if err != nil {
			return "", fmt.Errorf("failed to load existing plan %s: %w", existingPlanID, err)
		}

		existingPlanJSON, _ := json.MarshalIndent(existingPlan, "", "  ")
		plannerInstruction = fmt.Sprintf(`%s

## Current Plan (to be updated)
%s

## Update Request
Objective: %s`, plannerPrompt, string(existingPlanJSON), objective)
	} else {
		// Create mode: fresh plan
		plannerInstruction = fmt.Sprintf(`%s

## Objective
%s`, plannerPrompt, objective)
	}

	if additionalContext != "" {
		plannerInstruction += fmt.Sprintf("\n\n## Additional Context\n%s", additionalContext)
	}

	log.Printf("[DELEGATION PLAN] Spawning planner sub-agent for: %s", truncateString(objective, 80))

	// Spawn planner sub-agent (use "high" reasoning for planning)
	subCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)
	subCtx = context.WithValue(subCtx, ReasoningLevelKey, "high")

	startTime := time.Now()
	plannerResult, err := executeFunc(subCtx, plannerInstruction)
	planDuration := time.Since(startTime)

	if err != nil {
		return "", fmt.Errorf("planner sub-agent failed: %w", err)
	}

	log.Printf("[DELEGATION PLAN] Planner completed in %s", planDuration)

	// Parse the planner's JSON response
	// Try to extract JSON from the response (planner may include extra text)
	planJSON := extractJSON(plannerResult)
	if planJSON == "" {
		return "", fmt.Errorf("planner did not return valid JSON. Raw response: %s", truncateString(plannerResult, 500))
	}

	var plannerOutput struct {
		Strategy string `json:"strategy"`
		Tasks    []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			Priority    string `json:"priority"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(planJSON), &plannerOutput); err != nil {
		return "", fmt.Errorf("failed to parse planner output: %w (raw: %s)", err, truncateString(planJSON, 500))
	}

	if len(plannerOutput.Tasks) == 0 {
		return "", fmt.Errorf("planner returned zero tasks")
	}

	// Build the plan
	now := time.Now()
	var planID string
	var createdAt time.Time

	if existingPlanID != "" {
		planID = existingPlanID
		// Preserve original creation time
		if existingPlan, err := loadPlanFromWorkspace(ctx, existingPlanID); err == nil {
			createdAt = existingPlan.CreatedAt
		} else {
			createdAt = now
		}
	} else {
		planID = generatePlanID(objective)
		createdAt = now
	}

	var tasks []DelegationTask
	for i, t := range plannerOutput.Tasks {
		priority := t.Priority
		if priority == "" {
			priority = "medium"
		}
		tasks = append(tasks, DelegationTask{
			ID:          fmt.Sprintf("task-%d", i+1),
			Title:       t.Title,
			Description: t.Description,
			Priority:    priority,
			Status:      "pending",
			CreatedAt:   now,
		})
	}

	plan := &DelegationPlan{
		PlanID:    planID,
		CreatedAt: createdAt,
		UpdatedAt: now,
		Status:    "planning",
		Objective: objective,
		Strategy:  plannerOutput.Strategy,
		Tasks:     tasks,
	}

	// Save to workspace
	if err := savePlanToWorkspace(ctx, plan); err != nil {
		return "", fmt.Errorf("failed to save plan: %w", err)
	}

	action := "Created"
	if existingPlanID != "" {
		action = "Updated"
	}
	log.Printf("[DELEGATION PLAN] %s plan %s with %d tasks: %s", action, planID, len(tasks), truncateString(objective, 80))

	// Return summary
	result := map[string]interface{}{
		"plan_id":        planID,
		"objective":      objective,
		"strategy":       plannerOutput.Strategy,
		"task_count":     len(tasks),
		"status":         "planning",
		"tasks":          tasks,
		"planning_time":  planDuration.String(),
		"message":        fmt.Sprintf("Plan %s with %d tasks. Use execute_plan_task to start executing tasks.", strings.ToLower(action), len(tasks)),
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// extractJSON tries to find and extract a JSON object from a string
// The planner sub-agent should return pure JSON, but may include extra text
func extractJSON(s string) string {
	// Try the whole string first
	s = strings.TrimSpace(s)
	if json.Valid([]byte(s)) {
		return s
	}

	// Try to find JSON between code fences
	if idx := strings.Index(s, "```json"); idx != -1 {
		start := idx + 7
		if end := strings.Index(s[start:], "```"); end != -1 {
			candidate := strings.TrimSpace(s[start : start+end])
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}
	if idx := strings.Index(s, "```"); idx != -1 {
		start := idx + 3
		// Skip language identifier if on same line
		if nl := strings.Index(s[start:], "\n"); nl != -1 {
			start = start + nl + 1
		}
		if end := strings.Index(s[start:], "```"); end != -1 {
			candidate := strings.TrimSpace(s[start : start+end])
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}

	// Try to find first { and last }
	firstBrace := strings.Index(s, "{")
	lastBrace := strings.LastIndex(s, "}")
	if firstBrace != -1 && lastBrace > firstBrace {
		candidate := s[firstBrace : lastBrace+1]
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}

	return ""
}

// handleExecutePlanTask executes a specific task from a plan via sub-agent
func handleExecutePlanTask(ctx context.Context, args map[string]interface{}) (string, error) {
	planID, _ := args["plan_id"].(string)
	if planID == "" {
		return "", fmt.Errorf("plan_id is required")
	}
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return "", fmt.Errorf("task_id is required")
	}
	additionalContext, _ := args["additional_context"].(string)
	reasoningLevel, _ := args["reasoning_level"].(string)

	// Load the plan
	plan, err := loadPlanFromWorkspace(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("failed to load plan %s: %w", planID, err)
	}

	// Find the task
	var task *DelegationTask
	var taskIdx int
	for i := range plan.Tasks {
		if plan.Tasks[i].ID == taskID {
			task = &plan.Tasks[i]
			taskIdx = i
			break
		}
	}
	if task == nil {
		return "", fmt.Errorf("task %s not found in plan %s", taskID, planID)
	}

	if task.Status == "completed" {
		return "", fmt.Errorf("task %s is already completed", taskID)
	}

	// Mark task as in_progress and update plan status
	plan.Tasks[taskIdx].Status = "in_progress"
	plan.Status = "in_progress"
	plan.UpdatedAt = time.Now()
	if err := savePlanToWorkspace(ctx, plan); err != nil {
		log.Printf("[DELEGATION PLAN] Warning: failed to save in_progress status: %v", err)
	}

	// Build instruction from task description + additional context
	instruction := fmt.Sprintf("## Task: %s\n\n%s", task.Title, task.Description)
	if additionalContext != "" {
		instruction += fmt.Sprintf("\n\n## Additional Context\n%s", additionalContext)
	}

	// Check delegation depth
	currentDepth := 0
	if depth, ok := ctx.Value(DelegationDepthKey).(int); ok {
		currentDepth = depth
	}
	if currentDepth >= MaxDelegationDepth {
		return "", fmt.Errorf("maximum delegation depth (%d) reached", MaxDelegationDepth)
	}

	// Get execution function
	executeFunc, ok := ctx.Value(ExecuteDelegatedTaskKey).(ExecuteDelegatedTaskFunc)
	if !ok || executeFunc == nil {
		return "", fmt.Errorf("delegation execution function not available")
	}

	log.Printf("[DELEGATION PLAN] Executing task %s from plan %s: %s", taskID, planID, truncateString(task.Title, 60))

	startTime := time.Now()

	// Set up context with depth and reasoning level
	subCtx := context.WithValue(ctx, DelegationDepthKey, currentDepth+1)
	if reasoningLevel != "" {
		subCtx = context.WithValue(subCtx, ReasoningLevelKey, reasoningLevel)
	}

	// Execute
	result, execErr := executeFunc(subCtx, instruction)

	executionTime := time.Since(startTime)

	// Reload plan (may have been modified concurrently) and update task
	plan, err = loadPlanFromWorkspace(ctx, planID)
	if err != nil {
		log.Printf("[DELEGATION PLAN] Warning: failed to reload plan after execution: %v", err)
	} else {
		// Re-find task in reloaded plan
		for i := range plan.Tasks {
			if plan.Tasks[i].ID == taskID {
				now := time.Now()
				plan.Tasks[i].ExecutionTime = executionTime.String()
				plan.Tasks[i].CompletedAt = &now

				if execErr != nil {
					plan.Tasks[i].Status = "failed"
					plan.Tasks[i].Error = execErr.Error()
				} else {
					plan.Tasks[i].Status = "completed"
					plan.Tasks[i].Result = truncateString(result, 2000)
				}
				break
			}
		}

		// Update overall plan status
		plan.UpdatedAt = time.Now()
		allDone := true
		anyFailed := false
		for _, t := range plan.Tasks {
			if t.Status != "completed" && t.Status != "failed" {
				allDone = false
			}
			if t.Status == "failed" {
				anyFailed = true
			}
		}
		if allDone {
			if anyFailed {
				plan.Status = "failed"
			} else {
				plan.Status = "completed"
			}
		}

		if saveErr := savePlanToWorkspace(ctx, plan); saveErr != nil {
			log.Printf("[DELEGATION PLAN] Warning: failed to save task result: %v", saveErr)
		}
	}

	// Return result
	taskResult := map[string]interface{}{
		"plan_id":        planID,
		"task_id":        taskID,
		"task_title":     task.Title,
		"success":        execErr == nil,
		"execution_time": executionTime.String(),
	}
	if execErr != nil {
		taskResult["error"] = execErr.Error()
	} else {
		taskResult["result"] = result
	}

	resultJSON, _ := json.MarshalIndent(taskResult, "", "  ")
	return string(resultJSON), nil
}

// handleUpdatePlanTask manually updates a task's status/notes/result
func handleUpdatePlanTask(ctx context.Context, args map[string]interface{}) (string, error) {
	planID, _ := args["plan_id"].(string)
	if planID == "" {
		return "", fmt.Errorf("plan_id is required")
	}
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return "", fmt.Errorf("task_id is required")
	}

	// Load plan
	plan, err := loadPlanFromWorkspace(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("failed to load plan %s: %w", planID, err)
	}

	// Find and update task
	found := false
	for i := range plan.Tasks {
		if plan.Tasks[i].ID == taskID {
			found = true
			if status, ok := args["status"].(string); ok && status != "" {
				plan.Tasks[i].Status = status
				if status == "completed" {
					now := time.Now()
					plan.Tasks[i].CompletedAt = &now
				}
			}
			if notes, ok := args["notes"].(string); ok && notes != "" {
				plan.Tasks[i].Notes = notes
			}
			if result, ok := args["result"].(string); ok && result != "" {
				plan.Tasks[i].Result = result
			}
			break
		}
	}

	if !found {
		return "", fmt.Errorf("task %s not found in plan %s", taskID, planID)
	}

	// Update overall plan status
	plan.UpdatedAt = time.Now()
	allDone := true
	anyFailed := false
	for _, t := range plan.Tasks {
		if t.Status != "completed" && t.Status != "failed" {
			allDone = false
		}
		if t.Status == "failed" {
			anyFailed = true
		}
	}
	if allDone {
		if anyFailed {
			plan.Status = "failed"
		} else {
			plan.Status = "completed"
		}
	} else {
		plan.Status = "in_progress"
	}

	if err := savePlanToWorkspace(ctx, plan); err != nil {
		return "", fmt.Errorf("failed to save plan: %w", err)
	}

	result := map[string]interface{}{
		"plan_id":     planID,
		"task_id":     taskID,
		"plan_status": plan.Status,
		"message":     fmt.Sprintf("Task %s updated successfully", taskID),
	}
	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// handleGetPlanStatus returns the current status summary of a plan
func handleGetPlanStatus(ctx context.Context, args map[string]interface{}) (string, error) {
	planID, _ := args["plan_id"].(string)
	if planID == "" {
		return "", fmt.Errorf("plan_id is required")
	}

	plan, err := loadPlanFromWorkspace(ctx, planID)
	if err != nil {
		return "", fmt.Errorf("failed to load plan %s: %w", planID, err)
	}

	// Build summary
	statusCounts := map[string]int{
		"pending":     0,
		"in_progress": 0,
		"completed":   0,
		"failed":      0,
	}
	var nextPendingTask *DelegationTask
	for i := range plan.Tasks {
		statusCounts[plan.Tasks[i].Status]++
		if plan.Tasks[i].Status == "pending" && nextPendingTask == nil {
			nextPendingTask = &plan.Tasks[i]
		}
	}

	result := map[string]interface{}{
		"plan_id":       plan.PlanID,
		"objective":     plan.Objective,
		"strategy":      plan.Strategy,
		"status":        plan.Status,
		"total_tasks":   len(plan.Tasks),
		"pending":       statusCounts["pending"],
		"in_progress":   statusCounts["in_progress"],
		"completed":     statusCounts["completed"],
		"failed":        statusCounts["failed"],
		"created_at":    plan.CreatedAt.Format(time.RFC3339),
		"updated_at":    plan.UpdatedAt.Format(time.RFC3339),
		"tasks":         plan.Tasks,
	}
	if nextPendingTask != nil {
		result["next_pending_task"] = map[string]interface{}{
			"id":          nextPendingTask.ID,
			"title":       nextPendingTask.Title,
			"description": nextPendingTask.Description,
			"priority":    nextPendingTask.Priority,
		}
	}

	resultJSON, _ := json.MarshalIndent(result, "", "  ")
	return string(resultJSON), nil
}

// --- Helper functions ---

// generatePlanID creates a human-friendly plan ID from the objective
func generatePlanID(objective string) string {
	// Extract key words from objective, lowercase, kebab-case
	words := strings.Fields(strings.ToLower(objective))
	// Filter to meaningful words (skip short filler words)
	skip := map[string]bool{"a": true, "an": true, "the": true, "to": true, "and": true, "or": true, "for": true, "of": true, "in": true, "on": true, "is": true, "it": true, "with": true, "that": true, "this": true, "be": true, "as": true, "at": true, "by": true, "from": true, "i": true, "we": true, "my": true, "our": true, "me": true}
	var parts []string
	for _, w := range words {
		// Strip non-alphanumeric
		cleaned := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, w)
		if cleaned != "" && !skip[cleaned] && len(cleaned) > 1 {
			parts = append(parts, cleaned)
		}
		if len(parts) >= 4 {
			break
		}
	}
	if len(parts) == 0 {
		parts = []string{"plan"}
	}
	// Add short random suffix for uniqueness
	b := make([]byte, 3)
	rand.Read(b)
	suffix := hex.EncodeToString(b)
	return strings.Join(parts, "-") + "-" + suffix
}

// renderPlanToMarkdown renders a DelegationPlan as readable markdown
func renderPlanToMarkdown(plan *DelegationPlan) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Delegation Plan: %s\n\n", plan.Objective))
	sb.WriteString(fmt.Sprintf("**Plan ID:** `%s`\n", plan.PlanID))
	sb.WriteString(fmt.Sprintf("**Status:** %s\n", plan.Status))
	sb.WriteString(fmt.Sprintf("**Created:** %s\n", plan.CreatedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("**Updated:** %s\n\n", plan.UpdatedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("## Strategy\n%s\n\n", plan.Strategy))

	// Group tasks by status
	sb.WriteString("## Tasks\n\n")

	for _, task := range plan.Tasks {
		checkbox := "[ ]"
		statusBadge := ""
		switch task.Status {
		case "completed":
			checkbox = "[x]"
			statusBadge = " ✅"
		case "in_progress":
			checkbox = "[-]"
			statusBadge = " 🔄"
		case "failed":
			checkbox = "[!]"
			statusBadge = " ❌"
		}

		sb.WriteString(fmt.Sprintf("- %s **%s** - %s [%s]%s\n",
			checkbox, task.ID, task.Title, task.Priority, statusBadge))

		if task.Description != "" {
			sb.WriteString(fmt.Sprintf("  - %s\n", truncateString(task.Description, 200)))
		}
		if task.Result != "" {
			sb.WriteString(fmt.Sprintf("  - **Result:** %s\n", truncateString(task.Result, 200)))
		}
		if task.Error != "" {
			sb.WriteString(fmt.Sprintf("  - **Error:** %s\n", task.Error))
		}
		if task.Notes != "" {
			sb.WriteString(fmt.Sprintf("  - **Notes:** %s\n", task.Notes))
		}
		if task.ExecutionTime != "" {
			sb.WriteString(fmt.Sprintf("  - **Time:** %s\n", task.ExecutionTime))
		}
	}

	// Summary
	pending, inProgress, completed, failed := 0, 0, 0, 0
	for _, t := range plan.Tasks {
		switch t.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		case "failed":
			failed++
		}
	}
	sb.WriteString(fmt.Sprintf("\n## Progress\n"))
	sb.WriteString(fmt.Sprintf("- **Completed:** %d/%d\n", completed, len(plan.Tasks)))
	if inProgress > 0 {
		sb.WriteString(fmt.Sprintf("- **In Progress:** %d\n", inProgress))
	}
	if failed > 0 {
		sb.WriteString(fmt.Sprintf("- **Failed:** %d\n", failed))
	}
	if pending > 0 {
		sb.WriteString(fmt.Sprintf("- **Pending:** %d\n", pending))
	}

	// Embed plan data as hidden HTML comment for machine parsing
	jsonData, err := json.Marshal(plan)
	if err == nil {
		sb.WriteString(fmt.Sprintf("\n<!-- PLAN_DATA:%s -->\n", string(jsonData)))
	}

	return sb.String()
}

// loadPlanFromWorkspace loads a plan from the workspace markdown file
func loadPlanFromWorkspace(ctx context.Context, planID string) (*DelegationPlan, error) {
	wsClient, ok := ctx.Value(WorkspaceClientKey).(*workspace.Client)
	if !ok || wsClient == nil {
		return nil, fmt.Errorf("workspace client not available - plan tools require workspace access")
	}

	mdPath := fmt.Sprintf("%s/%s/plan.md", PlanFileFolderPath, planID)
	result, err := wsClient.ReadWorkspaceFile(ctx, workspace.ReadWorkspaceFileParams{
		Filepath: mdPath,
	})
	if err != nil {
		return nil, fmt.Errorf("plan not found: %w", err)
	}

	// The workspace client returns a JSON envelope with "content" field
	content := result
	var envelope struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err == nil && envelope.Content != "" {
		content = envelope.Content
	}

	// Extract JSON from <!-- PLAN_DATA:{...} --> comment
	const prefix = "<!-- PLAN_DATA:"
	const suffix = " -->"
	startIdx := strings.Index(content, prefix)
	if startIdx == -1 {
		return nil, fmt.Errorf("plan file does not contain PLAN_DATA marker")
	}
	startIdx += len(prefix)
	endIdx := strings.Index(content[startIdx:], suffix)
	if endIdx == -1 {
		return nil, fmt.Errorf("malformed PLAN_DATA marker in plan file")
	}

	var plan DelegationPlan
	if err := json.Unmarshal([]byte(content[startIdx:startIdx+endIdx]), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan data: %w", err)
	}
	return &plan, nil
}

// savePlanToWorkspace saves a plan as a single markdown file with embedded JSON data
func savePlanToWorkspace(ctx context.Context, plan *DelegationPlan) error {
	wsClient, ok := ctx.Value(WorkspaceClientKey).(*workspace.Client)
	if !ok || wsClient == nil {
		return fmt.Errorf("workspace client not available - plan tools require workspace access")
	}

	mdContent := renderPlanToMarkdown(plan)
	mdPath := fmt.Sprintf("%s/%s/plan.md", PlanFileFolderPath, plan.PlanID)
	if _, err := wsClient.UpdateWorkspaceFile(ctx, workspace.UpdateWorkspaceFileParams{
		Filepath: mdPath,
		Content:  mdContent,
	}); err != nil {
		return fmt.Errorf("failed to write plan file: %w", err)
	}

	return nil
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
## Delegation Mode - THE MANAGER PROTOCOL

**YOUR IDENTITY**: You are the **Manager**. You do NOT plan, you do NOT code. You ONLY delegate and synthesize results.

### CORE RULE: NEVER PLAN YOURSELF
Do not analyze requirements, break down tasks, or design architecture yourself. Use tools to delegate ALL work:
- **Planning** → ` + "`create_delegation_plan`" + ` (a planner sub-agent does the thinking)
- **Execution** → ` + "`execute_plan_task`" + ` or ` + "`delegate`" + ` (worker sub-agents do the work)
- **Your job** → call the right tools and report results to the user

### Decision Framework: SIMPLE vs STRUCTURED DELEGATION

**Use ` + "`delegate`" + ` (simple, one-off tasks):**
- Quick independent tasks (single file changes, running tests, simple fixes)
- Tasks that don't need progress tracking
- Fire-and-forget: call it multiple times in parallel

**Use ` + "`create_delegation_plan`" + ` + ` + "`execute_plan_task`" + ` (complex projects):**
- Multi-step projects where you need organized task tracking
- When the user needs visibility into progress (plan file appears in workspace)
- When tasks build on each other and order matters
- To update an existing plan: pass ` + "`plan_id`" + ` to ` + "`create_delegation_plan`" + `

### THE FIRST RESPONSE RULE
If the user's request is complex, you **MUST** delegate in your **FIRST** response.
- **BAD**: Analyzing the request yourself, then delegating.
- **GOOD**: Immediately calling ` + "`create_delegation_plan`" + ` with the objective and any context you have.

### Reasoning Level Guide
When executing tasks, pick the appropriate reasoning_level:
- **"high"**: Complex architecture, system design, tricky algorithms
- **"medium"**: Standard implementation, CRUD endpoints, UI components
- **"low"**: Formatting, linting, simple tests, config changes

### Workflow A: Simple Parallel Delegation
` + "```" + `
delegate(instruction: "Implement JWT auth...", reasoning_level: "high")
delegate(instruction: "Create signup UI...", reasoning_level: "medium")
delegate(instruction: "Add unit tests...", reasoning_level: "low")
` + "```" + `

### Workflow B: Structured Plan Delegation
` + "```" + `
1. create_delegation_plan(objective: "Build user auth system", context: "Express + React app...")
   → Planner sub-agent creates the plan → returns plan_id + tasks
2. execute_plan_task(plan_id, "task-1", reasoning_level: "high")
3. execute_plan_task(plan_id, "task-2", reasoning_level: "medium")
4. get_plan_status(plan_id) → check progress
5. execute_plan_task(plan_id, "task-3", reasoning_level: "low")
6. Summarize results to user
` + "```" + `

### Updating a Plan
` + "```" + `
create_delegation_plan(objective: "Also add OAuth support", plan_id: "plan-xxx", context: "Current plan needs OAuth...")
   → Planner sub-agent revises the existing plan with new tasks
` + "```" + `

### Best Practices
- **Parallelism**: For simple delegation, call ` + "`delegate`" + ` multiple times in one turn.
- **Context is everything**: Pass file paths, tech stack, constraints to ` + "`create_delegation_plan`" + ` so the planner produces good tasks.
- **You are the Quality Gate**: Verify results before telling the user it's done.
- **Plan files are visible**: The user can see progress in Chats/Delegations/ folder.

### Limitations
- **No Nested Delegation**: Sub-agents cannot delegate further.
- **No Shared Context**: Sub-agents start fresh. The planner must write self-contained task descriptions.
`
}
