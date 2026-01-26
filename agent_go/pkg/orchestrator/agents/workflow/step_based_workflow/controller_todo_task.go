package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"

	baseevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// executeTodoTaskStep executes a todo task step by:
//  1. Loading/initializing todos.json
//  2. Executing TodoTaskOrchestratorAgent in a loop
//  3. Processing responses:
//     - Create/update tasks via todo tools
//     - Delegate to predefined sub-agents (with learning/prevalidation)
//     - Delegate to generic agent (no learning/prevalidation)
//     - Complete when all tasks done
//  4. Return success status and next step ID
//
// Returns: (successCriteriaMet bool, nextStepID string, error)
func (hcpo *StepBasedWorkflowOrchestrator) executeTodoTaskStep(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	progress *StepProgress,
	previousContextFiles []string,
	previousExecutionResults []string,
	iteration int,
	execCtx *ExecutionContext,
	allSteps []PlanStepInterface,
	stepPath string,
) (bool, string, error) {
	// Cast to TodoTaskPlanStep
	todoTaskStep, ok := step.(*TodoTaskPlanStep)
	if !ok {
		return false, "", fmt.Errorf("step is not a TodoTaskPlanStep")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🎯 Executing todo task step %d: %s", stepIndex+1, step.GetTitle()))

	// Use provided stepPath or generate from stepIndex
	todoTaskStepPath := stepPath
	if todoTaskStepPath == "" {
		todoTaskStepPath = fmt.Sprintf("step-%d", stepIndex+1)
	}

	// Setup folder guard for todo task orchestrator agent
	// The orchestrator needs to read/write todos.json and access workspace files
	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepID := step.GetID()
	if stepID == "" {
		stepID = fmt.Sprintf("step-%d", stepIndex+1)
	}

	// Build paths for folder guard
	// Step execution path: execution/{stepPath}/
	stepExecutionPath := filepath.Join("execution", todoTaskStepPath)
	// Execution workspace path: execution/ (to read previous step results)
	executionWorkspacePath := "execution"
	// Run workspace path: the base workspace (e.g., Workflow/codeanalysis)
	runWorkspacePath := baseWorkspacePath
	// Step-specific learnings folder: learnings/{stepID}/
	stepLearningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepID)
	// Knowledgebase folder: knowledgebase/ (persistent files across runs, at workspace root)
	knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)

	// READ: step-specific learnings folder + execution folder + run folder + knowledgebase folder
	// WRITE: step execution path + knowledgebase folder
	readPaths := []string{stepLearningsPath, executionWorkspacePath, runWorkspacePath, knowledgebasePath}
	writePaths := []string{stepExecutionPath, knowledgebasePath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for todo task orchestrator agent - Read paths: %v, Write paths: %v", readPaths, writePaths))

	// Emit step_started event
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, todoTaskStepPath, false)

	// Initialize or load todos.json
	// Path must include the run folder (iteration/group) and execution/ prefix to match what the agent is told in prompts
	var todosFilePath string
	if hcpo.selectedRunFolder != "" {
		// Include run folder: runs/iteration-X/group-Y/execution/step-Z/todos.json
		todosFilePath = filepath.Join("runs", hcpo.selectedRunFolder, "execution", todoTaskStepPath, "todos.json")
	} else {
		// Fallback for legacy/test scenarios without run folder
		todosFilePath = filepath.Join("execution", todoTaskStepPath, "todos.json")
	}
	todoFile, err := hcpo.loadOrCreateTodosFile(ctx, todosFilePath, todoTaskStep)
	if err != nil {
		return false, "", fmt.Errorf("failed to load/create todos file: %w", err)
	}

	// Keep conversation history in-memory
	var conversationHistory []llmtypes.MessageContent

	// Determine max iterations
	maxIterations := hcpo.GetMaxTurns()
	stepConfig := getAgentConfigs(todoTaskStep)
	if stepConfig != nil && stepConfig.OrchestrationMaxIterations != nil {
		maxIterations = *stepConfig.OrchestrationMaxIterations
	}

	// Track sub-agent results for context
	var lastSubAgentResult string
	var lastSubAgentName string
	var lastTodoID string

	// Main todo task orchestration loop
	for taskIteration := 0; taskIteration < maxIterations; taskIteration++ {
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Todo task iteration %d/%d", taskIteration+1, maxIterations))

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return false, "", fmt.Errorf("todo task execution canceled: %w", ctx.Err())
		default:
		}

		// Build template variables for orchestrator
		templateVars := hcpo.buildTodoTaskOrchestratorTemplateVars(
			ctx,
			todoTaskStep,
			stepIndex,
			todoTaskStepPath,
			previousContextFiles,
			todoFile,
			lastSubAgentResult,
			lastSubAgentName,
			lastTodoID,
		)

		// Execute TodoTaskOrchestratorAgent
		response, updatedHistory, executionLLM, subAgentExecCtx, err := hcpo.executeTodoTaskOrchestratorAgent(
			ctx,
			todoTaskStep,
			stepIndex,
			todoTaskStepPath,
			templateVars,
			conversationHistory,
			allSteps,
			progress,
		)
		if err != nil {
			return false, "", fmt.Errorf("todo task orchestrator failed: %w", err)
		}
		conversationHistory = updatedHistory

		// Store response in step
		todoTaskStep.TodoTaskResponse = response

		// Check if step was completed via mark_step_complete tool (new tool-based approach)
		if subAgentExecCtx != nil && subAgentExecCtx.StepCompleted {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Todo task step complete (via tool): %s", subAgentExecCtx.CompletionReason))
			// Reload todos file for final state
			todoFile, _ = hcpo.loadOrCreateTodosFile(ctx, todosFilePath, todoTaskStep)
			// Emit todo task step completed event
			hcpo.emitTodoTaskStepCompletedEvent(ctx, step, stepIndex, todoTaskStepPath, taskIteration+1, todoFile, subAgentExecCtx.CompletionReason, todoTaskStep.NextStepID)
			// Emit step finished event
			hcpo.emitStepFinishedEvent(ctx, step, stepIndex, todoTaskStepPath, false)
			return true, todoTaskStep.NextStepID, nil
		}

		hcpo.GetLogger().Info(fmt.Sprintf("📋 Todo task response: action=%s, all_complete=%v, progress=%s",
			response.NextAction, response.AllTasksComplete, response.ProgressSummary))

		// Detect and emit todo item change events by comparing before/after state
		previousTodoFile := todoFile
		updatedTodoFile, err := hcpo.loadOrCreateTodosFile(ctx, todosFilePath, todoTaskStep)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to reload todos file for change detection: %v", err))
		} else {
			hcpo.emitTodoItemChangeEvents(ctx, step, stepIndex, todoTaskStepPath, previousTodoFile, updatedTodoFile)
			todoFile = updatedTodoFile // Use updated state going forward
		}

		// Log routing decision to file (similar to orchestration step)
		hcpo.logTodoTaskRoutingDecision(ctx, step, stepIndex, todoTaskStepPath, taskIteration, response, todoFile, executionLLM)

		// Emit route selected event
		hcpo.emitTodoTaskRouteSelectedEvent(ctx, step, stepIndex, todoTaskStepPath, taskIteration, response, todoFile, executionLLM)

		// Process the response based on next_action (backward compatibility with structured output approach)
		// Note: In the new tool-based approach, the agent calls sub-agent tools directly and step completion
		// is handled via mark_step_complete. The structured output is maintained for backward compatibility.
		switch response.NextAction {
		case "complete":
			if response.AllTasksComplete {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Todo task step complete (via structured output): %s", response.CompletionReason))
				// Emit todo task step completed event
				hcpo.emitTodoTaskStepCompletedEvent(ctx, step, stepIndex, todoTaskStepPath, taskIteration+1, todoFile, response.CompletionReason, todoTaskStep.NextStepID)
				// Emit step finished event (use existing method from controller_progress.go)
				hcpo.emitStepFinishedEvent(ctx, step, stepIndex, todoTaskStepPath, false)
				return true, todoTaskStep.NextStepID, nil
			}
			// Not all tasks complete but agent said complete - continue
			hcpo.GetLogger().Warn("⚠️ Agent said complete but all_tasks_complete is false, continuing...")

		case "delegate":
			// Delegate to sub-agent (backward compatibility - in new approach, agent calls tools directly)
			// This path is taken when agent uses structured output for delegation
			var subAgentResult string
			var subAgentName string

			if response.UseGenericAgent {
				// Execute via generic agent
				hcpo.GetLogger().Info(fmt.Sprintf("🤖 Delegating todo %s to generic agent (via structured output)", response.TodoIDToExecute))
				subAgentResult, err = hcpo.executeGenericAgent(
					ctx,
					todoTaskStep,
					stepIndex,
					todoTaskStepPath,
					response,
					todoFile,
					allSteps,
					progress,
				)
				subAgentName = "generic"
			} else if response.SelectedRouteID != "" {
				// Execute via predefined sub-agent
				hcpo.GetLogger().Info(fmt.Sprintf("🤖 Delegating todo %s to predefined agent: %s (via structured output)", response.TodoIDToExecute, response.SelectedRouteID))
				subAgentResult, err = hcpo.executePredefinedSubAgent(
					ctx,
					todoTaskStep,
					stepIndex,
					todoTaskStepPath,
					response,
					allSteps,
					progress,
				)
				subAgentName = response.SelectedRouteID
			} else {
				return false, "", fmt.Errorf("delegate action requires either selected_route_id or use_generic_agent=true")
			}

			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Sub-agent execution failed: %v", err))
				subAgentResult = fmt.Sprintf("ERROR: %v", err)
			}

			// Store results for next iteration
			lastSubAgentResult = subAgentResult
			lastSubAgentName = subAgentName
			lastTodoID = response.TodoIDToExecute

		case "continue":
			// Continue managing todos without delegation
			hcpo.GetLogger().Info("📝 Continuing todo management...")
			// The orchestrator has made todo tool calls, reload the file
			todoFile, err = hcpo.loadOrCreateTodosFile(ctx, todosFilePath, todoTaskStep)
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to reload todos file: %v", err))
			}

		default:
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Unknown next_action: %s, continuing...", response.NextAction))
		}

		// Reload todos file after any action
		todoFile, err = hcpo.loadOrCreateTodosFile(ctx, todosFilePath, todoTaskStep)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to reload todos file: %v", err))
		}
	}

	// Max iterations reached
	hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Todo task step reached max iterations (%d)", maxIterations))
	return false, todoTaskStep.NextStepID, nil
}

// loadOrCreateTodosFile loads an existing todos.json or creates a new one
func (hcpo *StepBasedWorkflowOrchestrator) loadOrCreateTodosFile(
	ctx context.Context,
	todosFilePath string,
	step *TodoTaskPlanStep,
) (*virtualtools.TodoFile, error) {
	// Try to read existing file
	content, err := hcpo.ReadWorkspaceFile(ctx, todosFilePath)
	if err == nil && content != "" {
		var todoFile virtualtools.TodoFile
		if err := json.Unmarshal([]byte(content), &todoFile); err != nil {
			return nil, fmt.Errorf("failed to parse todos.json: %w", err)
		}
		return &todoFile, nil
	}

	// Create new todos file
	todoFile := &virtualtools.TodoFile{
		StepID:    step.GetID(),
		Objective: step.GetDescription(),
		Todos:     []virtualtools.TodoItem{},
		Summary: virtualtools.TodoSummary{
			Total: 0,
		},
	}

	// Save initial file
	content2, err := json.MarshalIndent(todoFile, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal initial todos: %w", err)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, todosFilePath, string(content2)); err != nil {
		return nil, fmt.Errorf("failed to write initial todos.json: %w", err)
	}

	return todoFile, nil
}

// buildTodoTaskOrchestratorTemplateVars builds template variables for the orchestrator agent
func (hcpo *StepBasedWorkflowOrchestrator) buildTodoTaskOrchestratorTemplateVars(
	ctx context.Context,
	step *TodoTaskPlanStep,
	stepIndex int,
	stepPath string,
	previousContextFiles []string,
	todoFile *virtualtools.TodoFile,
	lastSubAgentResult string,
	lastSubAgentName string,
	lastTodoID string,
) map[string]string {
	// Build predefined routes description
	var routesBuilder strings.Builder
	for i, route := range step.PredefinedRoutes {
		if i > 0 {
			routesBuilder.WriteString("\n")
		}
		routesBuilder.WriteString(fmt.Sprintf("- **%s** (%s): %s", route.RouteName, route.RouteID, route.Condition))
		if route.SubAgentStep != nil {
			routesBuilder.WriteString(fmt.Sprintf("\n  Description: %s", route.SubAgentStep.GetDescription()))
		}
	}

	// Build current todos JSON
	todosJSON, _ := json.MarshalIndent(todoFile.Todos, "", "  ")

	// Build progress summary
	progressSummary := fmt.Sprintf("%d of %d tasks completed (%d open, %d in progress, %d blocked)",
		todoFile.Summary.Completed, todoFile.Summary.Total,
		todoFile.Summary.Open, todoFile.Summary.InProgress, todoFile.Summary.Blocked)

	// Get step execution path
	executionPath := filepath.Join("execution", stepPath)

	// Get step config for code execution mode
	stepConfig := getAgentConfigs(step)
	isCodeExecutionMode := hcpo.getCodeExecutionMode(stepConfig)

	// Get knowledgebase setting
	useKnowledgebase := hcpo.UseKnowledgebase()

	templateVars := map[string]string{
		// Resolve variables in step metadata
		"StepTitle":               ResolveVariables(step.GetTitle(), hcpo.variableValues),
		"StepDescription":         ResolveVariables(step.GetDescription(), hcpo.variableValues),
		"StepSuccessCriteria":     ResolveVariables(step.GetSuccessCriteria(), hcpo.variableValues),
		"StepContextDependencies": strings.Join(ResolveVariablesArray(previousContextFiles, hcpo.variableValues), ", "),
		"WorkspacePath":           hcpo.GetWorkspacePath(),
		"StepNumber":              fmt.Sprintf("step-%d", stepIndex+1),
		"StepExecutionPath":       executionPath,
		"PredefinedRoutes":        routesBuilder.String(),
		"EnableGenericAgent":      fmt.Sprintf("%t", step.EnableGenericAgent),
		"CurrentTodos":            string(todosJSON),
		"ProgressSummary":         progressSummary,
		"SubAgentResult":          lastSubAgentResult,
		"LastSubAgentName":        lastSubAgentName,
		"LastTodoID":              lastTodoID,
		// Add code execution mode and knowledgebase flags
		"IsCodeExecutionMode": fmt.Sprintf("%v", isCodeExecutionMode),
		"UseKnowledgebase":    fmt.Sprintf("%v", useKnowledgebase),
	}

	// Add variable names and values (like orchestration step)
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
		templateVars["VariableNames"] = variableNames
	}
	if variableValues := FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues); variableValues != "" {
		templateVars["VariableValues"] = variableValues
	}

	return templateVars
}

// selectTodoTaskOrchestratorLLM selects the LLM config for todo task orchestrator
// Uses selectExecutionLLM helper with learningsFolderEmpty=true to skip tempLLM logic
// Priority: step config execution LLM > preset execution LLM > orchestrator default
func (hcpo *StepBasedWorkflowOrchestrator) selectTodoTaskOrchestratorLLM(
	ctx context.Context,
	stepConfig *AgentConfigs,
	stepID string,
	stepPath string,
) *orchestrator.LLMConfig {
	// Use selectExecutionLLM with learningsFolderEmpty=true since orchestrators don't accumulate learnings
	// This will skip tempLLM logic and use step config > preset > orchestrator fallback
	return hcpo.selectExecutionLLM(
		ctx,
		stepConfig,
		false, // isRetryAfterValidationFailure - orchestrator doesn't retry
		1,     // retryAttempt - first attempt
		stepID,
		stepPath,
		true, // learningsFolderEmpty - orchestrator has no learnings, skip tempLLM
	)
}

// executeTodoTaskOrchestratorAgent executes the orchestrator agent using the standard factory pattern
// This ensures proper event bridge connection for sub-event tracking
// Returns: response, updatedHistory, executionLLM, subAgentExecCtx, error
// The subAgentExecCtx contains execution state including whether mark_step_complete was called
func (hcpo *StepBasedWorkflowOrchestrator) executeTodoTaskOrchestratorAgent(
	ctx context.Context,
	step *TodoTaskPlanStep,
	stepIndex int,
	stepPath string,
	templateVars map[string]string,
	conversationHistory []llmtypes.MessageContent,
	allSteps []PlanStepInterface,
	progress *StepProgress,
) (*TodoTaskResponse, []llmtypes.MessageContent, string, *SubAgentExecutionContext, error) {
	agentName := fmt.Sprintf("todo-task-orchestrator-step-%d", stepIndex+1)

	// Get step config
	stepConfig := getAgentConfigs(step)

	// Select LLM config using helper function
	stepID := step.GetID()
	if stepID == "" {
		stepID = fmt.Sprintf("step-%d", stepIndex+1)
	}
	llmConfig := hcpo.selectTodoTaskOrchestratorLLM(ctx, stepConfig, stepID, stepPath)
	if llmConfig == nil {
		return nil, nil, "", nil, fmt.Errorf("no valid LLM configuration found for todo task orchestrator")
	}

	// Capture execution LLM for logging before creating agent
	var executionLLM string
	if llmConfig != nil && llmConfig.Primary.ModelID != "" {
		executionLLM = fmt.Sprintf("%s/%s", llmConfig.Primary.Provider, llmConfig.Primary.ModelID)
	}

	// Build sub-agent execution context for tool-based delegation
	subAgentExecCtx := &SubAgentExecutionContext{
		TodoTaskStep: step,
		StepIndex:    stepIndex,
		StepPath:     stepPath,
		AllSteps:     allSteps,
		Progress:     progress,
	}

	// Use factory method to create agent with proper event bridge connection
	// This handles initialization, event bridge connection, and tool registration
	agent, err := hcpo.createTodoTaskOrchestratorAgent(
		ctx,
		"todo_task", // phase
		stepIndex,   // step
		0,           // iteration
		stepID,
		stepPath,   // step path for todo tools context injection
		agentName,
		stepConfig,
		llmConfig,
		subAgentExecCtx, // Sub-agent execution context for tool-based delegation
	)
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("failed to create todo task orchestrator agent: %w", err)
	}
	defer agent.Close()

	// Cast to the specific type to access ExecuteStructured
	todoTaskAgent, ok := agent.(*WorkflowTodoTaskOrchestratorAgent)
	if !ok {
		return nil, nil, "", nil, fmt.Errorf("agent is not a WorkflowTodoTaskOrchestratorAgent")
	}

	// Execute with structured output
	response, updatedHistory, err := todoTaskAgent.ExecuteStructured(ctx, templateVars, conversationHistory)
	if err != nil {
		return nil, nil, "", subAgentExecCtx, fmt.Errorf("todo task orchestrator execution failed: %w", err)
	}

	return response, updatedHistory, executionLLM, subAgentExecCtx, nil
}

// selectTodoTaskExecutionLLM selects the LLM config for todo task execution (generic agent)
// Uses selectExecutionLLM helper with learningsFolderEmpty=true to skip tempLLM logic
// Priority: step config > preset default > orchestrator config
func (hcpo *StepBasedWorkflowOrchestrator) selectTodoTaskExecutionLLM(
	ctx context.Context,
	stepConfig *AgentConfigs,
	stepID string,
	stepPath string,
) *orchestrator.LLMConfig {
	// Use selectExecutionLLM with learningsFolderEmpty=true since generic agents don't accumulate learnings
	// This will skip tempLLM logic and use step config > preset > orchestrator fallback
	return hcpo.selectExecutionLLM(
		ctx,
		stepConfig,
		false, // isRetryAfterValidationFailure - generic agent doesn't retry
		1,     // retryAttempt - first attempt
		stepID,
		stepPath,
		true, // learningsFolderEmpty - generic agent has no learnings, skip tempLLM
	)
}

// executeGenericAgent executes a generic task using the standard execution agent
// This uses the same execution infrastructure as other steps but with:
// - Learning DISABLED (no learnings accumulated)
// - Validation DISABLED (no validation schema required)
// - Full MCP server access (same as predefined sub-agents)
func (hcpo *StepBasedWorkflowOrchestrator) executeGenericAgent(
	ctx context.Context,
	step *TodoTaskPlanStep,
	stepIndex int,
	stepPath string,
	response *TodoTaskResponse,
	todoFile *virtualtools.TodoFile,
	allSteps []PlanStepInterface,
	progress *StepProgress,
) (string, error) {
	// Find the todo item
	var todoItem *virtualtools.TodoItem
	for i, item := range todoFile.Todos {
		if item.ID == response.TodoIDToExecute {
			todoItem = &todoFile.Todos[i]
			break
		}
	}
	if todoItem == nil {
		return "", fmt.Errorf("todo item %s not found", response.TodoIDToExecute)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🤖 Executing generic agent for todo: %s (%s)", todoItem.Title, todoItem.ID))

	// Get parent step config to inherit execution modes
	parentConfig := getAgentConfigs(step)
	var useToolSearchMode *bool
	var useCodeExecutionMode *bool
	if parentConfig != nil {
		useToolSearchMode = parentConfig.UseToolSearchMode
		useCodeExecutionMode = parentConfig.UseCodeExecutionMode
	}

	// Create a synthetic RegularPlanStep for the generic execution
	// Use the orchestrator's instructions and success criteria
	genericStepID := fmt.Sprintf("generic-%s-%s", stepPath, response.TodoIDToExecute)
	genericStep := &RegularPlanStep{
		Type: StepTypeRegular,
		CommonStepFields: CommonStepFields{
			ID:              genericStepID,
			Title:           todoItem.Title,
			Description:     response.InstructionsToSubAgent,
			SuccessCriteria: response.SuccessCriteriaForSubAgent,
			ContextOutput:   FlexibleContextOutput(fmt.Sprintf("%s-result.json", response.TodoIDToExecute)),
		},
		HasLoop: false,
		// Configure to disable learning and validation, but inherit execution modes
		AgentConfigs: &AgentConfigs{
			DisableLearning:      boolPtr(true), // No learning for generic agent
			DisableValidation:    boolPtr(true), // No validation for generic agent
			UseToolSearchMode:    useToolSearchMode,
			UseCodeExecutionMode: useCodeExecutionMode,
		},
	}

	// Build generic step path
	genericStepPath := fmt.Sprintf("%s-generic-%s", stepPath, response.TodoIDToExecute)

	// Get execution path
	executionPath := filepath.Join("execution", genericStepPath)

	// Setup folder guard for generic agent
	readPaths := []string{executionPath, "execution", filepath.Join("execution", stepPath)}
	writePaths := []string{executionPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)

	// Build execution context
	execCtx := &ExecutionContext{
		SkipHumanInput:     true, // Generic agents don't request human feedback
		FastExecuteMode:    false,
		FastExecuteEndStep: -1,
		RunSingleStepOnly:  false,
		SingleStepTarget:   -1,
		ResumeBranchStep:   nil,
		IsEvaluationMode:   false,
	}

	// Execute using executeSingleStep (reuses standard execution infrastructure)
	executionResult, _, err := hcpo.executeSingleStep(
		ctx,
		genericStep,
		stepIndex,        // Use parent step index for context
		genericStepPath,  // stepPath
		1,                // totalSteps = 1 for single generic task
		0,                // iteration
		[]string{},       // previousContextFiles - empty for generic tasks
		progress,         // progress
		true,             // isBranchStep = true (generic task is like a branch step)
		execCtx,          // execCtx
		allSteps,         // allSteps
		false,            // isDecisionInnerStep = false
		nil,              // decisionContext = nil
		"",               // decisionEvaluationQuestion - empty
		true,             // isSubAgent = true (sub-agents never request human feedback)
		[]string{response.InstructionsToSubAgent}, // previousExecutionResults - pass instructions
		nil,              // orchestrationRoutes - none for generic agent
	)

	if err != nil {
		return fmt.Sprintf("Generic agent failed: %v", err), err
	}

	result := fmt.Sprintf("Generic agent completed: %s", executionResult)
	return result, nil
}

// boolPtr returns a pointer to a bool value
func boolPtr(b bool) *bool {
	return &b
}

// executePredefinedSubAgent executes a predefined sub-agent for a todo task
// This uses the same execution pattern as orchestration steps (with learning/prevalidation)
func (hcpo *StepBasedWorkflowOrchestrator) executePredefinedSubAgent(
	ctx context.Context,
	step *TodoTaskPlanStep,
	stepIndex int,
	stepPath string,
	response *TodoTaskResponse,
	allSteps []PlanStepInterface,
	progress *StepProgress,
) (string, error) {
	// Find the route
	var route *PlanOrchestrationRoute
	for i, r := range step.PredefinedRoutes {
		if r.RouteID == response.SelectedRouteID {
			route = &step.PredefinedRoutes[i]
			break
		}
	}
	if route == nil {
		return "", fmt.Errorf("route %s not found in predefined routes", response.SelectedRouteID)
	}

	if route.SubAgentStep == nil {
		return "", fmt.Errorf("route %s has no sub_agent_step defined", response.SelectedRouteID)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🤖 Executing predefined sub-agent: %s (%s)", route.RouteName, route.RouteID))

	// Use the sub-agent step from the route
	subAgentStep := route.SubAgentStep

	// Build sub-agent step path
	subAgentStepPath := fmt.Sprintf("%s-sub-%s", stepPath, route.RouteID)

	// Get execution path
	executionPath := filepath.Join("execution", subAgentStepPath)

	// Setup folder guard for sub-agent
	readPaths := []string{executionPath, "execution", filepath.Join("execution", stepPath)}
	writePaths := []string{executionPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)

	// Build orchestration routes for sub-agent (so it knows about other agents)
	var orchestrationRoutesForSubAgent []OrchestrationRoute
	for _, r := range step.PredefinedRoutes {
		orchestrationRoutesForSubAgent = append(orchestrationRoutesForSubAgent, OrchestrationRoute{
			RouteID:      r.RouteID,
			RouteName:    r.RouteName,
			Condition:    r.Condition,
			SubAgentStep: r.SubAgentStep,
		})
	}

	// Execute the sub-agent step using executeSingleStep
	// This will include learning and prevalidation like regular orchestration sub-agents
	// Signature: ctx, step, stepIndex, stepPath, totalSteps, iteration, previousContextFiles, progress,
	//            isBranchStep, execCtx, allSteps, isDecisionInnerStep, decisionContext,
	//            decisionEvaluationQuestion, isSubAgent, previousExecutionResults, orchestrationRoutes
	execCtx := &ExecutionContext{
		SkipHumanInput:     true, // Sub-agents don't request human feedback
		FastExecuteMode:    false,
		FastExecuteEndStep: -1,
		RunSingleStepOnly:  false,
		SingleStepTarget:   -1,
		ResumeBranchStep:   nil,
		IsEvaluationMode:   false,
	}

	executionResult, _, err := hcpo.executeSingleStep(
		ctx,
		subAgentStep,
		stepIndex,                               // Use parent step index for context
		subAgentStepPath,                        // stepPath
		1,                                       // totalSteps = 1 for single sub-agent
		0,                                       // iteration
		[]string{},                              // previousContextFiles - empty for sub-agents
		progress,                                // progress
		true,                                    // isBranchStep = true (sub-agent is like a branch step)
		execCtx,                                 // execCtx
		allSteps,                                // allSteps
		false,                                   // isDecisionInnerStep = false
		nil,                                     // decisionContext = nil
		"",                                      // decisionEvaluationQuestion - empty
		true,                                    // isSubAgent = true (sub-agents never request human feedback)
		[]string{response.InstructionsToSubAgent}, // previousExecutionResults - pass instructions
		orchestrationRoutesForSubAgent,          // orchestrationRoutes
	)

	if err != nil {
		return fmt.Sprintf("Sub-agent %s failed: %v", route.RouteName, err), err
	}

	result := fmt.Sprintf("Sub-agent %s completed: %s", route.RouteName, executionResult)
	return result, nil
}

// updateTodoStatus updates a todo item's status in the todos.json file
func (hcpo *StepBasedWorkflowOrchestrator) updateTodoStatus(
	ctx context.Context,
	todosFilePath string,
	todoID string,
	status string,
	result string,
) error {
	// Read current todos
	content, err := hcpo.ReadWorkspaceFile(ctx, todosFilePath)
	if err != nil {
		return fmt.Errorf("failed to read todos file: %w", err)
	}

	var todoFile virtualtools.TodoFile
	if err := json.Unmarshal([]byte(content), &todoFile); err != nil {
		return fmt.Errorf("failed to parse todos file: %w", err)
	}

	// Find and update the todo
	found := false
	for i, item := range todoFile.Todos {
		if item.ID == todoID {
			todoFile.Todos[i].Status = status
			if result != "" {
				todoFile.Todos[i].Result = result
			}
			todoFile.Todos[i].UpdatedAt = time.Now()
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("todo %s not found", todoID)
	}

	// Update summary
	todoFile.Summary = virtualtools.TodoSummary{Total: len(todoFile.Todos)}
	for _, item := range todoFile.Todos {
		switch item.Status {
		case "open":
			todoFile.Summary.Open++
		case "in_progress":
			todoFile.Summary.InProgress++
		case "completed":
			todoFile.Summary.Completed++
		case "blocked":
			todoFile.Summary.Blocked++
		}
	}

	// Write back
	content2, err := json.MarshalIndent(todoFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal todos: %w", err)
	}

	if err := hcpo.WriteWorkspaceFile(ctx, todosFilePath, string(content2)); err != nil {
		return fmt.Errorf("failed to write todos file: %w", err)
	}

	return nil
}

// emitTodoTaskRouteSelectedEvent emits an event when the todo task orchestrator selects a route/sub-agent
func (hcpo *StepBasedWorkflowOrchestrator) emitTodoTaskRouteSelectedEvent(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	iteration int,
	response *TodoTaskResponse,
	todoFile *virtualtools.TodoFile,
	executionLLM string,
) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	// Get todo title if a todo is being executed
	var todoTitle string
	if response.TodoIDToExecute != "" && todoFile != nil {
		for _, todo := range todoFile.Todos {
			if todo.ID == response.TodoIDToExecute {
				todoTitle = todo.Title
				break
			}
		}
	}

	// Get route name if predefined route selected
	var selectedRouteName string
	if response.SelectedRouteID != "" {
		todoTaskStep, ok := step.(*TodoTaskPlanStep)
		if ok {
			for _, route := range todoTaskStep.PredefinedRoutes {
				if route.RouteID == response.SelectedRouteID {
					selectedRouteName = route.RouteName
					break
				}
			}
		}
	}

	event := &TodoTaskRouteSelectedEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Component: "orchestrator",
		},
		StepIndex:              stepIndex,
		StepPath:               stepPath,
		StepID:                 step.GetID(),
		StepTitle:              step.GetTitle(),
		Iteration:              iteration + 1, // 1-based for display
		NextAction:             response.NextAction,
		SelectedRouteID:        response.SelectedRouteID,
		SelectedRouteName:      selectedRouteName,
		UseGenericAgent:        response.UseGenericAgent,
		TodoIDToExecute:        response.TodoIDToExecute,
		TodoTitle:              todoTitle,
		InstructionsToSubAgent: response.InstructionsToSubAgent,
		SelectionReasoning:     response.ProgressSummary, // Use progress summary as reasoning
		AllTasksComplete:       response.AllTasksComplete,
		ProgressSummary:        response.ProgressSummary,
		Model:                  executionLLM,
	}

	agentEvent := &baseevents.AgentEvent{
		Type:      events.TodoTaskRouteSelected,
		Timestamp: time.Now(),
		Data:      event,
	}

	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit todo task route selected event: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📢 Emitted todo task route selected event: action=%s, route=%s, todo=%s",
			response.NextAction, response.SelectedRouteID, response.TodoIDToExecute))
	}
}

// emitTodoTaskStepCompletedEvent emits an event when the entire todo task step is completed
func (hcpo *StepBasedWorkflowOrchestrator) emitTodoTaskStepCompletedEvent(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	totalIterations int,
	todoFile *virtualtools.TodoFile,
	completionReason string,
	nextStepID string,
) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	totalTodos := 0
	completedCount := 0
	if todoFile != nil {
		totalTodos = todoFile.Summary.Total
		completedCount = todoFile.Summary.Completed
	}

	event := &TodoTaskStepCompletedEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Component: "orchestrator",
		},
		StepIndex:        stepIndex,
		StepPath:         stepPath,
		StepID:           step.GetID(),
		StepTitle:        step.GetTitle(),
		TotalIterations:  totalIterations,
		TotalTodosCount:  totalTodos,
		CompletedCount:   completedCount,
		CompletionReason: completionReason,
		NextStepID:       nextStepID,
	}

	agentEvent := &baseevents.AgentEvent{
		Type:      events.TodoTaskStepCompleted,
		Timestamp: time.Now(),
		Data:      event,
	}

	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit todo task step completed event: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📢 Emitted todo task step completed event: step=%s, iterations=%d, todos=%d/%d",
			stepPath, totalIterations, completedCount, totalTodos))
	}
}

// emitTodoItemChangeEvents compares before/after todo states and emits events for changes
func (hcpo *StepBasedWorkflowOrchestrator) emitTodoItemChangeEvents(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	before *virtualtools.TodoFile,
	after *virtualtools.TodoFile,
) {
	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		return
	}

	if before == nil || after == nil {
		return
	}

	// Build map of previous todos by ID for comparison
	previousTodos := make(map[string]virtualtools.TodoItem)
	for _, todo := range before.Todos {
		previousTodos[todo.ID] = todo
	}

	// Check for created, updated, and completed todos
	for _, todo := range after.Todos {
		prevTodo, existed := previousTodos[todo.ID]

		if !existed {
			// New todo created
			event := &TodoTaskItemCreatedEvent{
				BaseEventData: baseevents.BaseEventData{
					Timestamp: time.Now(),
					Component: "orchestrator",
				},
				StepIndex:   stepIndex,
				StepPath:    stepPath,
				StepID:      step.GetID(),
				TodoID:      todo.ID,
				Title:       todo.Title,
				Description: todo.Description,
				Priority:    todo.Priority,
				CreatedBy:   "orchestrator",
			}

			agentEvent := &baseevents.AgentEvent{
				Type:      events.TodoTaskItemCreated,
				Timestamp: time.Now(),
				Data:      event,
			}

			if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit todo item created event: %v", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("📢 Todo item created: %s - %s", todo.ID, todo.Title))
			}
		} else if todo.Status == "completed" && prevTodo.Status != "completed" {
			// Todo was completed
			event := &TodoTaskItemCompletedEvent{
				BaseEventData: baseevents.BaseEventData{
					Timestamp: time.Now(),
					Component: "orchestrator",
				},
				StepIndex:   stepIndex,
				StepPath:    stepPath,
				StepID:      step.GetID(),
				TodoID:      todo.ID,
				Title:       todo.Title,
				Result:      todo.Result,
				CompletedBy: "orchestrator",
			}

			agentEvent := &baseevents.AgentEvent{
				Type:      events.TodoTaskItemCompleted,
				Timestamp: time.Now(),
				Data:      event,
			}

			if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit todo item completed event: %v", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("📢 Todo item completed: %s - %s", todo.ID, todo.Title))
			}
		} else if todo.Status != prevTodo.Status {
			// Todo status changed (but not to completed - that's handled above)
			event := &TodoTaskItemUpdatedEvent{
				BaseEventData: baseevents.BaseEventData{
					Timestamp: time.Now(),
					Component: "orchestrator",
				},
				StepIndex: stepIndex,
				StepPath:  stepPath,
				StepID:    step.GetID(),
				TodoID:    todo.ID,
				Title:     todo.Title,
				OldStatus: prevTodo.Status,
				NewStatus: todo.Status,
				UpdatedBy: "orchestrator",
				Notes:     todo.Notes,
			}

			agentEvent := &baseevents.AgentEvent{
				Type:      events.TodoTaskItemUpdated,
				Timestamp: time.Now(),
				Data:      event,
			}

			if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit todo item updated event: %v", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("📢 Todo item updated: %s - %s -> %s", todo.ID, prevTodo.Status, todo.Status))
			}
		}
	}
}

// logTodoTaskRoutingDecision logs the routing decision to a JSON file (similar to orchestration step)
func (hcpo *StepBasedWorkflowOrchestrator) logTodoTaskRoutingDecision(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	iteration int,
	response *TodoTaskResponse,
	todoFile *virtualtools.TodoFile,
	executionLLM string,
) {
	// Get workspace path for validation folder
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}

	validationFolderPath := getValidationFolderPath(validationWorkspacePath, stepPath)
	todoTaskLogFilePath := fmt.Sprintf("%s/todo-task-execution.json", validationFolderPath)

	// Get todo title if a todo is being executed
	var todoTitle string
	if response.TodoIDToExecute != "" && todoFile != nil {
		for _, todo := range todoFile.Todos {
			if todo.ID == response.TodoIDToExecute {
				todoTitle = todo.Title
				break
			}
		}
	}

	// Get route name if predefined route selected
	var selectedRouteName string
	if response.SelectedRouteID != "" {
		todoTaskStep, ok := step.(*TodoTaskPlanStep)
		if ok {
			for _, route := range todoTaskStep.PredefinedRoutes {
				if route.RouteID == response.SelectedRouteID {
					selectedRouteName = route.RouteName
					break
				}
			}
		}
	}

	// Build log entry
	routingEntry := map[string]interface{}{
		"type":         "routing",
		"step_index":   stepIndex + 1,
		"step_path":    stepPath,
		"step_id":      step.GetID(),
		"step_title":   step.GetTitle(),
		"iteration":    iteration + 1,
		"model":        executionLLM,
		"todo_task_response": map[string]interface{}{
			"next_action":                     response.NextAction,
			"selected_route_id":               response.SelectedRouteID,
			"selected_route_name":             selectedRouteName,
			"use_generic_agent":               response.UseGenericAgent,
			"todo_id_to_execute":              response.TodoIDToExecute,
			"todo_title":                      todoTitle,
			"instructions_to_sub_agent":       response.InstructionsToSubAgent,
			"success_criteria_for_sub_agent": response.SuccessCriteriaForSubAgent,
			"all_tasks_complete":              response.AllTasksComplete,
			"progress_summary":                response.ProgressSummary,
			"completion_reason":               response.CompletionReason,
		},
		"todo_summary": map[string]interface{}{
			"total":       todoFile.Summary.Total,
			"completed":   todoFile.Summary.Completed,
			"in_progress": todoFile.Summary.InProgress,
			"open":        todoFile.Summary.Open,
			"blocked":     todoFile.Summary.Blocked,
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}

	// Append to log file using the same pattern as orchestration step
	if err := hcpo.appendTodoTaskLogEntry(ctx, todoTaskLogFilePath, routingEntry); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to append todo task routing entry to log: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("💾 Todo task routing entry appended to: %s", todoTaskLogFilePath))
	}
}

// appendTodoTaskLogEntry appends a JSON entry to the todo task execution log file (JSONL format)
func (hcpo *StepBasedWorkflowOrchestrator) appendTodoTaskLogEntry(ctx context.Context, filePath string, entry map[string]interface{}) error {
	// Marshal the entry to a single JSON line (no indentation for JSONL format)
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal todo task log entry to JSON: %w", err)
	}

	// Read existing file content if it exists
	existingContent := ""
	existingContent, err = hcpo.ReadWorkspaceFile(ctx, filePath)
	if err != nil {
		// File doesn't exist yet - this is expected for the first entry
		existingContent = ""
	}

	// Append new entry (JSONL format: each entry on its own line)
	newContent := existingContent
	if newContent != "" && !strings.HasSuffix(newContent, "\n") {
		newContent += "\n"
	}
	newContent += string(entryJSON)

	// Write the updated content back
	if err := hcpo.WriteWorkspaceFile(ctx, filePath, newContent); err != nil {
		return fmt.Errorf("failed to append todo task log entry to %s: %w", filePath, err)
	}

	return nil
}
