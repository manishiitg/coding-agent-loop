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
//  1. The orchestrator LLM manages tasks.md (markdown format) via shell commands
//  2. Executing TodoTaskOrchestratorAgent in a loop
//  3. Processing tool calls:
//     - call_sub_agent: Delegate to predefined sub-agents (with learning/prevalidation)
//     - call_generic_agent: Delegate to generic agent (no learning/prevalidation)
//  4. Completion detection: LLM writes completed.txt via shell when done
//  5. Return success status and next step ID
//
// Task Management:
//   - LLM creates/updates tasks.md directly using execute_shell_command
//   - LLM writes completed.txt when step objective is met
//   - Sub-agents receive instructions via tool parameters (NOT by reading files)
//   - Validation reads tasks.md to ensure tasks exist before delegation
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
	decisionContext *DecisionContext, // Optional: context from decision step that routed to this step (nil if not routed from decision)
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
	// The orchestrator needs to read/write tasks.md and access workspace files
	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepID := step.GetID()
	if stepID == "" {
		stepID = fmt.Sprintf("step-%d", stepIndex+1)
	}

	// Build paths for folder guard
	// All paths should include the workspace prefix (e.g., Workflow/codeanalysis/...)
	// This matches what the LLM uses in execute_shell_command's working_directory parameter
	var stepExecutionPath string
	var executionWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		// Include run folder: Workflow/codeanalysis/runs/iteration-X/group-Y/execution/step-Z/
		stepExecutionPath = filepath.Join(baseWorkspacePath, "runs", hcpo.selectedRunFolder, "execution", todoTaskStepPath)
		executionWorkspacePath = filepath.Join(baseWorkspacePath, "runs", hcpo.selectedRunFolder, "execution")
	} else {
		stepExecutionPath = filepath.Join(baseWorkspacePath, "execution", todoTaskStepPath)
		executionWorkspacePath = filepath.Join(baseWorkspacePath, "execution")
	}
	// Run workspace path: the base workspace (e.g., Workflow/codeanalysis)
	runWorkspacePath := baseWorkspacePath
	// Step-specific learnings folder: Workflow/codeanalysis/learnings/{stepID}/
	stepLearningsPath := filepath.Join(baseWorkspacePath, "learnings", stepID)
	// Knowledgebase folder: Workflow/codeanalysis/knowledgebase/ (persistent files across runs)
	knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)

	// READ: step-specific learnings folder + execution folder + run folder + knowledgebase folder
	// WRITE: step execution path + knowledgebase folder
	// All paths include workspace prefix to match shell working_directory parameter
	readPaths := []string{stepLearningsPath, executionWorkspacePath, runWorkspacePath, knowledgebasePath}
	writePaths := []string{stepExecutionPath, knowledgebasePath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for todo task orchestrator agent - Read paths: %v, Write paths: %v", readPaths, writePaths))

	// Emit step_started event
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, todoTaskStepPath, false)

	// Note: Task management is now handled via shell commands with tasks.md
	// The LLM creates and updates tasks.md directly using execute_shell_command
	// Sub-agents receive instructions via tool parameters (NOT by reading files)

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
			previousExecutionResults,
			allSteps,
			lastSubAgentResult,
			lastSubAgentName,
			lastTodoID,
			decisionContext,
		)

		// Execute TodoTaskOrchestratorAgent
		response, updatedHistory, executionLLM, _, err := hcpo.executeTodoTaskOrchestratorAgent(
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

		hcpo.GetLogger().Info(fmt.Sprintf("📋 Todo task response: action=%s, all_complete=%v, progress=%s",
			response.NextAction, response.AllTasksComplete, response.ProgressSummary))

		// Log routing decision to file (similar to orchestration step)
		hcpo.logTodoTaskRoutingDecision(ctx, step, stepIndex, todoTaskStepPath, taskIteration, response, nil, executionLLM)

		// Save execution log for this iteration (so UI can show full execution history)
		hcpo.saveTodoTaskExecutionLog(ctx, todoTaskStepPath, taskIteration, executionLLM, updatedHistory)

		// Emit route selected event
		hcpo.emitTodoTaskRouteSelectedEvent(ctx, step, stepIndex, todoTaskStepPath, taskIteration, response, nil, executionLLM)

		// Run pre-validation after each agent execution (if validation schema exists)
		// This is the PRIMARY completion check - step completes when validation passes
		validationSchema := step.GetValidationSchema()
		if validationSchema != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 Running pre-validation after agent execution (iteration %d)", taskIteration+1))
			preValidationPassed, preValidationReason := hcpo.runTodoTaskPreValidation(ctx, step, stepIndex, todoTaskStepPath, stepExecutionPath)

			if preValidationPassed {
				// Pre-validation passed - step is complete!
				completionReason := fmt.Sprintf("Pre-validation passed after iteration %d. %s", taskIteration+1, response.ProgressSummary)
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Todo task step complete (pre-validation passed): %s", completionReason))
				// Emit todo task step completed event
				hcpo.emitTodoTaskStepCompletedEvent(ctx, step, stepIndex, todoTaskStepPath, taskIteration+1, nil, completionReason, todoTaskStep.NextStepID)
				// Emit step finished event
				hcpo.emitStepFinishedEvent(ctx, step, stepIndex, todoTaskStepPath, false)
				return true, todoTaskStep.NextStepID, nil
			}

			// Pre-validation failed - continue with feedback
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Pre-validation failed for todo task step (iteration %d): continuing with feedback", taskIteration+1))
			lastSubAgentResult = fmt.Sprintf("PRE-VALIDATION FAILED:\n%s\n\nPlease fix the validation issues before the step can complete.", preValidationReason)
			lastSubAgentName = "pre-validation"
			lastTodoID = "validation-failure"
			// Continue to next iteration with feedback
			continue
		}

		// No validation schema - use legacy completion detection
		// Process the response based on next_action
		switch response.NextAction {
		case "complete":
			if response.AllTasksComplete {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Todo task step complete (no validation schema): %s", response.CompletionReason))
				// Emit todo task step completed event
				hcpo.emitTodoTaskStepCompletedEvent(ctx, step, stepIndex, todoTaskStepPath, taskIteration+1, nil, response.CompletionReason, todoTaskStep.NextStepID)
				// Emit step finished event
				hcpo.emitStepFinishedEvent(ctx, step, stepIndex, todoTaskStepPath, false)
				return true, todoTaskStep.NextStepID, nil
			}
			// Not all tasks complete but agent said complete - continue
			hcpo.GetLogger().Warn("⚠️ Agent said complete but all_tasks_complete is false, continuing...")

		case "delegate":
			// Delegate to sub-agent via structured output (legacy path)
			// In the new approach, agent calls call_sub_agent/call_generic_agent tools directly
			var subAgentResult string
			var subAgentName string

			if response.UseGenericAgent {
				// Execute via generic agent
				hcpo.GetLogger().Info(fmt.Sprintf("🤖 Delegating task %s to generic agent", response.TodoIDToExecute))
				subAgentResult, err = hcpo.executeGenericAgent(
					ctx,
					todoTaskStep,
					stepIndex,
					todoTaskStepPath,
					response,
					allSteps,
					progress,
				)
				subAgentName = "generic"
			} else if response.SelectedRouteID != "" {
				// Execute via predefined sub-agent
				hcpo.GetLogger().Info(fmt.Sprintf("🤖 Delegating task %s to predefined agent: %s", response.TodoIDToExecute, response.SelectedRouteID))
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
			// Continue - agent is managing tasks via shell commands
			hcpo.GetLogger().Info("📝 Continuing task management...")

		default:
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Unknown next_action: %s, continuing...", response.NextAction))
		}
	}

	// Max iterations reached
	hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Todo task step reached max iterations (%d)", maxIterations))
	return false, todoTaskStep.NextStepID, nil
}

// buildTodoTaskOrchestratorTemplateVars builds template variables for the orchestrator agent
// Note: Task management is handled via shell commands with tasks.md - the LLM reads it directly
func (hcpo *StepBasedWorkflowOrchestrator) buildTodoTaskOrchestratorTemplateVars(
	ctx context.Context,
	step *TodoTaskPlanStep,
	stepIndex int,
	stepPath string,
	previousContextFiles []string,
	previousExecutionResults []string,
	allSteps []PlanStepInterface,
	lastSubAgentResult string,
	lastSubAgentName string,
	lastTodoID string,
	decisionContext *DecisionContext, // Optional: context from decision step that routed to this step
) map[string]string {
	// Build predefined routes description
	var routesBuilder strings.Builder
	for i, route := range step.PredefinedRoutes {
		if i > 0 {
			routesBuilder.WriteString("\n")
		}
		fmt.Fprintf(&routesBuilder, "- **%s** (%s): %s", route.RouteName, route.RouteID, route.Condition)
		if route.SubAgentStep != nil {
			fmt.Fprintf(&routesBuilder, "\n  Description: %s", route.SubAgentStep.GetDescription())
		}
	}

	// Get step execution path (include run folder if set)
	var executionPath string
	if hcpo.selectedRunFolder != "" {
		executionPath = filepath.Join("runs", hcpo.selectedRunFolder, "execution", stepPath)
	} else {
		executionPath = filepath.Join("execution", stepPath)
	}

	// Build shell working directory (WorkspacePath + StepExecutionPath)
	// This is the path to use in execute_shell_command's working_directory parameter
	shellWorkingDirectory := filepath.Join(hcpo.GetWorkspacePath(), executionPath)

	// Get step config for code execution mode
	stepConfig := getAgentConfigs(step)
	isCodeExecutionMode := hcpo.getCodeExecutionMode(stepConfig)

	// Get knowledgebase setting
	useKnowledgebase := hcpo.UseKnowledgebase()

	// Determine if skip execution cleanup is enabled
	skipExecutionCleanup := false
	if hcpo.executionOptions != nil {
		skipExecutionCleanup = hcpo.executionOptions.SkipExecutionCleanup
	}

	// Note: CurrentTodos and ProgressSummary are not populated here because
	// the LLM manages tasks.md directly via shell commands and reads it directly
	templateVars := map[string]string{
		// Resolve variables in step metadata
		"StepTitle":               ResolveVariables(step.GetTitle(), hcpo.variableValues),
		"StepDescription":         ResolveVariables(step.GetDescription(), hcpo.variableValues),
		"StepSuccessCriteria":     ResolveVariables(step.GetSuccessCriteria(), hcpo.variableValues),
		"StepContextDependencies": strings.Join(ResolveVariablesArray(previousContextFiles, hcpo.variableValues), ", "),
		"WorkspacePath":           hcpo.GetWorkspacePath(),
		"StepNumber":              fmt.Sprintf("step-%d", stepIndex+1),
		"StepExecutionPath":       executionPath,
		"ShellWorkingDirectory":   shellWorkingDirectory,
		"PredefinedRoutes":        routesBuilder.String(),
		"EnableGenericAgent":      fmt.Sprintf("%t", step.EnableGenericAgent),
		"CurrentTodos":            "(Read tasks.md using shell command)",
		"ProgressSummary":         "(Check tasks.md for current progress)",
		"SubAgentResult":          lastSubAgentResult,
		"LastSubAgentName":        lastSubAgentName,
		"LastTodoID":              lastTodoID,
		// Add code execution mode and knowledgebase flags
		"IsCodeExecutionMode":    fmt.Sprintf("%v", isCodeExecutionMode),
		"UseKnowledgebase":       fmt.Sprintf("%v", useKnowledgebase),
		"SkipExecutionCleanup":   fmt.Sprintf("%v", skipExecutionCleanup), // Skip cleanup mode flag for state verification prompt
	}

	// Build previous steps summary (includes descriptions, output files, and execution results like human_input responses)
	previousStepsSummary := hcpo.buildPreviousStepsSummary(allSteps, stepIndex, previousContextFiles, previousExecutionResults)
	templateVars["PreviousStepsSummary"] = previousStepsSummary

	// Add EnableDynamicTierSelection flag for system prompt
	enableDynamicTier := false
	if hcpo.useTieredMode {
		if stepConfig := getAgentConfigs(step); stepConfig != nil &&
			stepConfig.EnableDynamicTierSelection != nil {
			enableDynamicTier = *stepConfig.EnableDynamicTierSelection
		}
	}
	templateVars["EnableDynamicTierSelection"] = fmt.Sprintf("%t", enableDynamicTier)

	// Add variable names and values (like orchestration step)
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
		templateVars["VariableNames"] = variableNames
	}
	if variableValues := FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues); variableValues != "" {
		templateVars["VariableValues"] = variableValues
	}

	// Add decision context if this step was routed from a decision step
	if decisionContext != nil {
		decisionReasoning := fmt.Sprintf(
			"## 🎯 Decision Context\n\n"+
				"This step was routed from decision step **%d: %s**.\n\n"+
				"**Decision Result**: %v\n"+
				"**Decision Reasoning**: %s\n\n"+
				"## 📋 Decision Step Execution Output\n\n"+
				"The following is the execution output from the decision step's inner step that was evaluated:\n\n"+
				"```\n%s\n```\n\n"+
				"Use this context to understand why this step is being executed and what conditions led to routing here.",
			decisionContext.DecisionStepIndex+1, // Convert to 1-based for display
			decisionContext.DecisionStepTitle,
			decisionContext.DecisionResult,
			decisionContext.DecisionReasoning,
			decisionContext.DecisionExecutionResult,
		)
		templateVars["DecisionReasoning"] = decisionReasoning
	} else {
		templateVars["DecisionReasoning"] = ""
	}

	return templateVars
}

// selectTodoTaskOrchestratorLLM selects the LLM config for todo task orchestrator
// Priority: OrchestratorLLM (direct override) > configured orchestrator tier (tiered mode) > selectExecutionLLM fallback
func (hcpo *StepBasedWorkflowOrchestrator) selectTodoTaskOrchestratorLLM(
	ctx context.Context,
	stepConfig *AgentConfigs,
	stepID string,
	stepPath string,
) *orchestrator.LLMConfig {
	// DIRECT LLM OVERRIDE: Check for orchestrator_llm first (works in both tiered and manual modes)
	if stepConfig != nil && stepConfig.OrchestratorLLM != nil &&
		stepConfig.OrchestratorLLM.Provider != "" && stepConfig.OrchestratorLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🎯 [DIRECT] Todo task orchestrator using direct LLM override: %s/%s",
			stepConfig.OrchestratorLLM.Provider, stepConfig.OrchestratorLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: stepConfig.OrchestratorLLM.Provider,
				ModelID:  stepConfig.OrchestratorLLM.ModelID,
			},
			APIKeys: hcpo.GetAPIKeys(),
		}
	}

	// TIERED MODE: Use configured orchestrator tier if set
	if hcpo.useTieredMode && hcpo.tierResolver != nil && stepConfig != nil &&
		stepConfig.TodoTaskOrchestratorTier != nil {
		tier := TierLevel(*stepConfig.TodoTaskOrchestratorTier)
		if tier >= TierHigh && tier <= TierLow {
			llmConfig := hcpo.tierResolver.ResolveTier(tier)
			if llmConfig != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Todo task orchestrator using configured Tier %d (%s): %s/%s",
					int(tier), TierLevelLabel(tier), llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
				return llmConfig
			}
		}
	}
	// Fallback: selectExecutionLLM with learningsFolderEmpty=true since orchestrators don't accumulate learnings
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
	if llmConfig.Primary.ModelID != "" {
		executionLLM = fmt.Sprintf("%s/%s", llmConfig.Primary.Provider, llmConfig.Primary.ModelID)
	}

	// Build sub-agent execution context for tool-based delegation
	subAgentExecCtx := &SubAgentExecutionContext{
		TodoTaskStep: step,
		StepIndex:    stepIndex,
		StepPath:     stepPath,
		AllSteps:     allSteps,
		Progress:     progress,
		StepConfig:   stepConfig, // Pass step config for sub_agent_llm override
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

	// Execute with tool-based approach (no structured output)
	// The agent manages tasks via shell (tasks.md) and delegates via call_sub_agent/call_generic_agent
	// Completion is detected by checking for completed.txt file
	_, updatedHistory, err := agent.Execute(ctx, templateVars, conversationHistory)
	if err != nil {
		return nil, nil, "", subAgentExecCtx, fmt.Errorf("todo task orchestrator execution failed: %w", err)
	}

	// Return a default response - actual state is tracked via files (tasks.md, completed.txt)
	// The controller checks for completed.txt file for completion detection
	response := &TodoTaskResponse{
		NextAction:       "continue",
		AllTasksComplete: false,
		ProgressSummary:  "Execution completed via tools",
	}

	return response, updatedHistory, executionLLM, subAgentExecCtx, nil
}

// executeGenericAgent executes a generic task using the standard execution agent
// This uses the same execution infrastructure as other steps but with:
// - Learning DISABLED (no learnings accumulated)
// - Validation DISABLED (no validation schema required)
// - Full MCP server access (same as predefined sub-agents)
// All task input comes from response (tool parameters), not from files
func (hcpo *StepBasedWorkflowOrchestrator) executeGenericAgent(
	ctx context.Context,
	step *TodoTaskPlanStep,
	stepIndex int,
	stepPath string,
	response *TodoTaskResponse,
	allSteps []PlanStepInterface,
	progress *StepProgress,
) (string, error) {
	// Use todoID as the task title
	// All actual task content comes from response.InstructionsToSubAgent
	taskTitle := response.TodoIDToExecute

	hcpo.GetLogger().Info(fmt.Sprintf("🤖 Executing generic agent for task: %s", taskTitle))

	// TEMP: Force simple agent mode for generic sub-agents — always disable code execution and tool search
	// TODO: Remove this override once todo task step supports tool_search/code_exec agent types properly
	useToolSearchMode := boolPtr(false)
	useCodeExecutionMode := boolPtr(false)

	// Create a synthetic RegularPlanStep for the generic execution
	// Use the orchestrator's instructions and success criteria
	genericStepID := fmt.Sprintf("generic-%s-%s", stepPath, response.TodoIDToExecute)
	genericStep := &RegularPlanStep{
		Type: StepTypeRegular,
		CommonStepFields: CommonStepFields{
			ID:              genericStepID,
			Title:           taskTitle,
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

	// Push context before sub-agent execution (preserve orchestrator context)
	if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PushContext("execution", stepIndex, genericStep.GetID(), genericStep.GetTitle())
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

	// Pop context after sub-agent execution (restore orchestrator context)
	if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PopContext()
	}

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
	// CRITICAL: Create a COPY of the step to avoid modifying the original plan in memory
	// This ensures CheckAndResetStepHash can still compare against the original unmodified plan
	var stepToExecute PlanStepInterface = route.SubAgentStep

	if regularStep := getRegularPlanStep(route.SubAgentStep); regularStep != nil {
		// Create a shallow copy of the struct
		stepCopy := *regularStep

		if response.InstructionsToSubAgent != "" {
			// Append orchestrator instructions to original description
			originalDescription := stepCopy.Description
			if originalDescription != "" {
				stepCopy.Description = fmt.Sprintf("%s\n\n## Orchestrator Instructions\n\n%s", originalDescription, response.InstructionsToSubAgent)
			} else {
				stepCopy.Description = response.InstructionsToSubAgent
			}
		}
		if response.SuccessCriteriaForSubAgent != "" {
			stepCopy.SuccessCriteria = response.SuccessCriteriaForSubAgent
		}

		// Use the copy for execution
		stepToExecute = &stepCopy
	}

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
	execCtx := &ExecutionContext{
		SkipHumanInput:     true, // Sub-agents don't request human feedback
		FastExecuteMode:    false,
		FastExecuteEndStep: -1,
		RunSingleStepOnly:  false,
		SingleStepTarget:   -1,
		ResumeBranchStep:   nil,
		IsEvaluationMode:   false,
	}

	// Push context before sub-agent execution (preserve orchestrator context)
	if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PushContext("execution", stepIndex, route.SubAgentStep.GetID(), route.SubAgentStep.GetTitle())
	}

	executionResult, _, err := hcpo.executeSingleStep(
		ctx,
		stepToExecute,                           // Use the modified COPY
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
		[]string{},                              // previousExecutionResults - empty (instructions are now in description)
		orchestrationRoutesForSubAgent,          // orchestrationRoutes
	)

	// Pop context after sub-agent execution (restore orchestrator context)
	if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PopContext()
	}

	if err != nil {
		return fmt.Sprintf("Sub-agent %s failed: %v", route.RouteName, err), err
	}

	result := fmt.Sprintf("Sub-agent %s completed: %s", route.RouteName, executionResult)
	return result, nil
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

	// Build todo summary (handle nil todoFile - tasks now managed via tasks.md)
	todoSummary := map[string]interface{}{
		"total":       0,
		"completed":   0,
		"in_progress": 0,
		"open":        0,
		"blocked":     0,
	}
	if todoFile != nil {
		todoSummary["total"] = todoFile.Summary.Total
		todoSummary["completed"] = todoFile.Summary.Completed
		todoSummary["in_progress"] = todoFile.Summary.InProgress
		todoSummary["open"] = todoFile.Summary.Open
		todoSummary["blocked"] = todoFile.Summary.Blocked
	}

	// Determine selected sub-agent path for logging (so UI can link to it)
	var selectedSubAgentPath string
	if response.NextAction == "delegate" {
		if response.UseGenericAgent {
			selectedSubAgentPath = fmt.Sprintf("%s-generic-%s", stepPath, response.TodoIDToExecute)
		} else if response.SelectedRouteID != "" {
			selectedSubAgentPath = fmt.Sprintf("%s-sub-%s", stepPath, response.SelectedRouteID)
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
			"selected_sub_agent_path":         selectedSubAgentPath,
			"todo_id_to_execute":              response.TodoIDToExecute,
			"todo_title":                      todoTitle,
			"instructions_to_sub_agent":       response.InstructionsToSubAgent,
			"success_criteria_for_sub_agent": response.SuccessCriteriaForSubAgent,
			"all_tasks_complete":              response.AllTasksComplete,
			"progress_summary":                response.ProgressSummary,
			"completion_reason":               response.CompletionReason,
		},
		"todo_summary": todoSummary,
		"timestamp":    time.Now().Format(time.RFC3339),
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

// saveTodoTaskExecutionLog saves the execution log for a todo task iteration
// This allows the UI to show the full execution history (conversation, tool calls) for each iteration
func (hcpo *StepBasedWorkflowOrchestrator) saveTodoTaskExecutionLog(
	ctx context.Context,
	stepPath string,
	iteration int,
	executionLLM string,
	conversationHistory []llmtypes.MessageContent,
) {
	// Get workspace path for logs folder
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}

	// Get execution logs folder path
	executionLogsFolderPath := getExecutionFolderPathForLogs(validationWorkspacePath, stepPath)

	// Create filename: execution-attempt-1-iteration-{iteration}.json
	// Use attempt=1 since todo task orchestrator doesn't have retry attempts like regular steps
	filename := fmt.Sprintf("execution-attempt-1-iteration-%d.json", iteration)
	filePath := fmt.Sprintf("%s/%s", executionLogsFolderPath, filename)

	// Extract execution summary from conversation history
	var executionSummary string
	for _, msg := range conversationHistory {
		if msg.Role == llmtypes.ChatMessageTypeAI {
			// Get assistant's text content from Parts
			for _, part := range msg.Parts {
				if textContent, ok := part.(llmtypes.TextContent); ok {
					if len(executionSummary) < 2000 { // Limit summary size
						executionSummary += textContent.Text + "\n"
					}
				}
			}
		}
	}

	// Build execution log entry
	executionLog := map[string]interface{}{
		"step_path":        stepPath,
		"attempt":          1,
		"iteration":        iteration,
		"model":            executionLLM,
		"execution_result": executionSummary,
		"message_count":    len(conversationHistory),
		"timestamp":        time.Now().Format(time.RFC3339),
	}

	// Marshal to JSON
	logJSON, err := json.MarshalIndent(executionLog, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal todo task execution log: %v", err))
		return
	}

	// Write to file
	if err := hcpo.WriteWorkspaceFile(ctx, filePath, string(logJSON)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save todo task execution log to %s: %v", filePath, err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("💾 Todo task execution log saved to: %s", filePath))
	}
}

// runTodoTaskPreValidation runs pre-validation for a todo task step if validation schema exists
// Returns (passed bool, reason string) - reason contains formatted validation results if failed
func (hcpo *StepBasedWorkflowOrchestrator) runTodoTaskPreValidation(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	stepExecutionPath string,
) (bool, string) {
	// Get validation schema from step
	validationSchema := step.GetValidationSchema()
	if validationSchema == nil {
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Pre-validation skipped for todo task step %d (no validation schema)", stepIndex+1))
		return true, ""
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Running pre-validation for todo task step %d with %d file checks", stepIndex+1, len(validationSchema.Files)))

	// Run pre-validation
	workspaceResults, err := RunPreValidation(ctx, validationSchema, stepExecutionPath, hcpo.BaseOrchestrator)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Pre-validation error for todo task step %d: %v", stepIndex+1, err))
		// Pre-validation error means we can't verify structure - treat as failure
		workspaceResults = &WorkspaceVerificationResult{
			OverallPass:  false,
			FilesChecked: []FileCheckResult{},
			Summary: ValidationSummary{
				TotalChecks:  0,
				PassedChecks: 0,
				FailedChecks: 1,
				SchemaErrors: 0,
				Errors: []ValidationError{
					{
						File:      "",
						Path:      "",
						CheckType: "pre_validation_error",
						Expected:  "pre-validation to run successfully",
						Actual:    "error occurred",
						Message:   fmt.Sprintf("Pre-validation failed to run: %v", err),
					},
				},
				SchemaWarnings: []ValidationError{},
			},
		}
	}

	// Emit pre-validation completed event
	hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, stepPath, false, workspaceResults)

	// Format results for feedback
	formattedResults := formatWorkspaceResults(workspaceResults)

	if workspaceResults.OverallPass {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Pre-validation passed for todo task step %d: %d/%d checks passed",
			stepIndex+1, workspaceResults.Summary.PassedChecks, workspaceResults.Summary.TotalChecks))
		return true, formattedResults
	}

	hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Pre-validation failed for todo task step %d: %d/%d checks passed",
		stepIndex+1, workspaceResults.Summary.PassedChecks, workspaceResults.Summary.TotalChecks))
	return false, formattedResults
}
