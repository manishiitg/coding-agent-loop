package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"

	baseevents "github.com/manishiitg/mcpagent/events"
	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func (hcpo *StepBasedWorkflowOrchestrator) getTodoTaskExecutionWorkspacePath() string {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	if hcpo.selectedRunFolder != "" {
		return filepath.Join(baseWorkspacePath, "runs", hcpo.selectedRunFolder, "execution")
	}
	return filepath.Join(baseWorkspacePath, "execution")
}

func (hcpo *StepBasedWorkflowOrchestrator) getTodoTaskStepExecutionPath(stepID, stepPath string) string {
	return getExecutionFolderPath(hcpo.getTodoTaskExecutionWorkspacePath(), stepID, stepPath)
}

func (hcpo *StepBasedWorkflowOrchestrator) getTodoTaskTasksFilePath(stepID, stepPath string) string {
	return filepath.Join(hcpo.getTodoTaskStepExecutionPath(stepID, stepPath), "tasks.md")
}

// executeTodoTaskStep executes a todo task step by:
//  1. The orchestrator LLM manages tasks.md (markdown format) via shell commands
//  2. Executing TodoTaskOrchestratorAgent in a loop
//  3. Processing tool calls:
//     - call_sub_agent: Delegate to predefined sub-agents (with learning/prevalidation)
//     - call_generic_agent: Delegate to generic agent (no learning/prevalidation)
//  4. Completion detection: all tasks marked [x] in tasks.md
//  5. Return success status and next step ID
//
// Task Management:
//   - LLM creates/updates tasks.md directly using execute_shell_command
//   - Step completes when all tasks in tasks.md are [x]
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

	// Ensure any pending debounced status update is flushed when the step completes
	defer hcpo.flushTodoTaskStatusDebouncer()

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

	executionWorkspacePath := hcpo.getTodoTaskExecutionWorkspacePath()
	stepExecutionPath := hcpo.getTodoTaskStepExecutionPath(stepID, todoTaskStepPath)
	// Run workspace path: the base workspace (e.g., Workflow/codeanalysis)
	runWorkspacePath := baseWorkspacePath
	// Step-specific learnings folder: Workflow/codeanalysis/learnings/{stepID}/
	stepLearningsPath := filepath.Join(baseWorkspacePath, "learnings", stepID)
	// Knowledgebase folder: Workflow/codeanalysis/knowledgebase/ (persistent files across runs)
	knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)

	// READ: step-specific learnings folder + execution folder + run folder + knowledgebase folder
	// WRITE: step execution path + knowledgebase folder

	readPaths := []string{stepLearningsPath, executionWorkspacePath, runWorkspacePath, knowledgebasePath}
	writePaths := []string{stepExecutionPath, knowledgebasePath, stepLearningsPath}

	// Add skill folder paths to read paths (skills are read-only)
	skillStepConfig := getAgentConfigs(step)
	effectiveSkills := GetEffectiveSkills(skillStepConfig, hcpo.BaseOrchestrator)
	if len(effectiveSkills) > 0 {
		skillReadPaths, _ := BuildSkillFolderGuardPaths(effectiveSkills)
		readPaths = append(readPaths, skillReadPaths...)
		hcpo.GetLogger().Info(fmt.Sprintf("🎯 Added skill folder paths to todo task folder guard: %v", skillReadPaths))
	}

	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for todo task orchestrator agent - Read paths: %v, Write paths: %v", readPaths, writePaths))

	// Ensure step execution folder exists before agent starts
	if err := hcpo.ensureStepExecutionFolderExists(ctx, stepExecutionPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure step execution folder exists: %v (continuing - folder will be created when files are written)", err))
	}

	// Emit step_started event
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, todoTaskStepPath, false)

	// Note: Task management is now handled via shell commands with tasks.md
	// The LLM creates and updates tasks.md directly using execute_shell_command
	// Sub-agents receive instructions via tool parameters (NOT by reading files)

	// Keep only the latest iteration conversation history in-memory.
	// Todo-task state should come from current files (tasks.md, outputs, tool results),
	// not from replaying previous assistant narration across loop iterations.
	var conversationHistory []llmtypes.MessageContent
	defer func() {
		if execCtx != nil && execCtx.ConversationHistoryCapture != nil {
			historyCopy := append([]llmtypes.MessageContent(nil), conversationHistory...)
			*execCtx.ConversationHistoryCapture = historyCopy
		}
	}()

	// Determine max iterations
	maxIterations := hcpo.GetMaxTurns()
	stepConfig := getAgentConfigs(todoTaskStep)
	if stepConfig != nil && stepConfig.OrchestrationMaxIterations != nil {
		maxIterations = *stepConfig.OrchestrationMaxIterations
	}

	// Learning config - determine once, reload each iteration
	isLearningDisabledStep := stepConfig != nil && stepConfig.DisableLearning != nil && *stepConfig.DisableLearning
	isLearningDetailLevelNone := stepConfig != nil && stepConfig.LearningDetailLevel == "none"
	isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
	learningFolderPath := getLearningFolderPathByStepID("", stepID, todoTaskStepPath, false)
	// Track sub-agent results for context
	var lastSubAgentResult string
	var lastSubAgentName string
	var lastTodoID string

	// Main todo task orchestration loop
	for taskIteration := 0; taskIteration < maxIterations; taskIteration++ {
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Todo task iteration %d/%d", taskIteration+1, maxIterations))

		// Start each orchestration loop from fresh conversational context.
		// Previous iterations may contain speculative diagnoses that are no longer true.
		conversationHistory = nil

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return false, "", fmt.Errorf("todo task execution canceled: %w", ctx.Err())
		default:
		}

		// Reload orchestrator learnings each iteration — provide file path reference instead of full content
		// Check for new SKILL.md format first, fall back to legacy orchestrator_learning.md
		var orchestratorLearningHistory string
		if isLearningDisabled {
			orchestratorLearningHistory = ""
		} else {
			docsRoot := GetPromptDocsRoot()
			orchestratorLearningFilePath := fmt.Sprintf("%s/SKILL.md", learningFolderPath)
			_, err := hcpo.ReadWorkspaceFile(ctx, orchestratorLearningFilePath)
			if err != nil {
				// Fall back to legacy format
				orchestratorLearningFilePath = fmt.Sprintf("%s/orchestrator_learning.md", learningFolderPath)
				_, err = hcpo.ReadWorkspaceFile(ctx, orchestratorLearningFilePath)
			}
			if err == nil {
				absLearningPath := filepath.Join(docsRoot, hcpo.GetWorkspacePath(), orchestratorLearningFilePath)
				orchestratorLearningHistory = fmt.Sprintf("📚 **Orchestrator learnings available** at `%s`. Read this file with execute_shell_command before delegating sub-agents — it contains error recovery patterns, system behaviors, and validated sequences from previous runs.", absLearningPath)
			}
		}

		// Read tasks.md to build dynamic user message
		var tasksFileContent string
		{
			tasksFilePath := hcpo.getTodoTaskTasksFilePath(stepID, todoTaskStepPath)
			content, err := hcpo.ReadWorkspaceFile(ctx, tasksFilePath)
			if err == nil && strings.TrimSpace(content) != "" {
				tasksFileContent = content
			}
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
			orchestratorLearningHistory,
			tasksFileContent,
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
		hcpo.saveTodoTaskExecutionLog(ctx, step.GetID(), todoTaskStepPath, taskIteration, executionLLM, updatedHistory)

		// Emit route selected event
		hcpo.emitTodoTaskRouteSelectedEvent(ctx, step, stepIndex, todoTaskStepPath, taskIteration, response, nil, executionLLM)

		// Check completion after agent execution
		if response.NextAction == "complete" && response.AllTasksComplete {
			// All tasks [x] in tasks.md — run pre-validation if schema exists, else done
			validationSchema := step.GetValidationSchema()
			if validationSchema != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("🔍 Running pre-validation after all tasks completed (iteration %d)", taskIteration+1))
				preValidationPassed, preValidationReason := hcpo.runTodoTaskPreValidation(ctx, step, stepIndex, todoTaskStepPath, stepExecutionPath)

				if preValidationPassed {
					completionReason := fmt.Sprintf("Pre-validation passed after iteration %d. %s", taskIteration+1, response.ProgressSummary)
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Todo task step complete (pre-validation passed): %s", completionReason))
					hcpo.emitTodoTaskStepCompletedEvent(ctx, step, stepIndex, todoTaskStepPath, taskIteration+1, nil, completionReason, todoTaskStep.NextStepID)
					hcpo.emitStepFinishedEvent(ctx, step, stepIndex, todoTaskStepPath, false)
					return true, todoTaskStep.NextStepID, nil
				}

				// Pre-validation failed — feed back to orchestrator so it can fix
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Pre-validation failed (iteration %d): continuing with feedback", taskIteration+1))
				lastSubAgentResult = fmt.Sprintf("\n\n---\nPRE-VALIDATION FAILED (all tasks are [x] but output validation did not pass):\n%s\n\nFix the failing checks — the step will re-run.", preValidationReason)
			} else {
				// No validation schema — tasks.md all [x] is the completion signal
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Todo task step complete (all tasks done): %s", response.CompletionReason))
				hcpo.emitTodoTaskStepCompletedEvent(ctx, step, stepIndex, todoTaskStepPath, taskIteration+1, nil, response.CompletionReason, todoTaskStep.NextStepID)
				hcpo.emitStepFinishedEvent(ctx, step, stepIndex, todoTaskStepPath, false)
				return true, todoTaskStep.NextStepID, nil
			}
		} else {
			// Tasks still pending/in-progress — agent needs more iterations
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Tasks not all complete (%s), continuing...", response.ProgressSummary))
		}
	}

	// Max iterations reached
	hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Todo task step reached max iterations (%d)", maxIterations))
	return false, todoTaskStep.NextStepID, nil
}

// buildCurrentTodosDisplay returns a display string for the CurrentTodos template var
func buildCurrentTodosDisplay(tasksFileContent string) string {
	if tasksFileContent == "" {
		return "(tasks.md does not exist yet — create it)"
	}
	return tasksFileContent
}

// buildProgressSummaryDisplay returns a brief summary of task progress from tasks.md content
func buildProgressSummaryDisplay(tasksFileContent string) string {
	if tasksFileContent == "" {
		return "(No tasks.md yet)"
	}
	pending := strings.Count(tasksFileContent, "- [ ]")
	inProgress := strings.Count(tasksFileContent, "- [~]")
	completed := strings.Count(tasksFileContent, "- [x]")
	total := pending + inProgress + completed
	if total == 0 {
		return "(tasks.md exists but no tasks found)"
	}
	return fmt.Sprintf("%d/%d completed, %d pending, %d in-progress", completed, total, pending, inProgress)
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
	orchestratorLearningHistory string, // Persisted learnings from previous runs
	tasksFileContent string, // Content of tasks.md (empty if file doesn't exist)
) map[string]string {
	// Build predefined routes list (title + ID only — use get_route_description tool for details)
	var routesBuilder strings.Builder
	for i, route := range step.PredefinedRoutes {
		if i > 0 {
			routesBuilder.WriteString("\n")
		}
		fmt.Fprintf(&routesBuilder, "- **%s** (`%s`)", ResolveVariables(route.RouteName, hcpo.variableValues), route.RouteID)
		if route.SubAgentStep != nil {
			subStepPath := fmt.Sprintf("%s-sub-%s", stepPath, route.RouteID)
			subExecRelPath := getExecutionFolderPath(hcpo.getTodoTaskExecutionWorkspacePath(), route.SubAgentStep.GetID(), subStepPath)
			subExecAbsPath := filepath.Join(GetPromptDocsRoot(), subExecRelPath)
			contextOutput := strings.TrimSpace(ResolveVariables(route.SubAgentStep.GetContextOutput().String(), hcpo.variableValues))
			if contextOutput != "" {
				fmt.Fprintf(&routesBuilder, " → output: `%s` | folder: `%s/`", contextOutput, subExecAbsPath)
			} else {
				fmt.Fprintf(&routesBuilder, " → folder: `%s/`", subExecAbsPath)
			}
		}
	}

	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepID := step.GetID()
	if stepID == "" {
		stepID = fmt.Sprintf("step-%d", stepIndex+1)
	}
	executionPath := hcpo.getTodoTaskStepExecutionPath(stepID, stepPath)
	shellWorkingDirectory := filepath.Join(GetPromptDocsRoot(), executionPath)

	// Get step config for code execution mode: step config > workflow/preset default
	stepConfig := getAgentConfigs(step)
	isCodeExecutionMode := hcpo.getCodeExecutionMode(stepConfig)

	// Get knowledgebase setting
	useKnowledgebase := hcpo.UseKnowledgebase()

	// Determine if skip execution cleanup is enabled
	skipExecutionCleanup := false
	if hcpo.executionOptions != nil {
		skipExecutionCleanup = hcpo.executionOptions.SkipExecutionCleanup
	}

	// Build folder guard paths for prompt (same logic as executeTodoTaskStep setup)
	docsRoot := GetPromptDocsRoot()
	fgExecPath := hcpo.getTodoTaskExecutionWorkspacePath()
	fgStepExecPath := executionPath
	fgLearningsPath := filepath.Join(baseWorkspacePath, "learnings", stepID)
	fgKnowledgebasePath := getKnowledgebasePath(baseWorkspacePath)
	fgReadPaths := []string{fgLearningsPath, fgExecPath, baseWorkspacePath, fgKnowledgebasePath}
	fgWritePaths := []string{fgStepExecPath, fgKnowledgebasePath, fgLearningsPath}

	templateVars := map[string]string{
		// Resolve variables in step metadata
		"StepTitle":           ResolveVariables(step.GetTitle(), hcpo.variableValues),
		"StepDescription":     ResolveVariables(step.GetDescription(), hcpo.variableValues),
		"StepSuccessCriteria": "",
		"StepContextDependencies": strings.Join(hcpo.resolveDependencyPathsWithWorkspace(
			ctx,
			ResolveVariablesArray(previousContextFiles, hcpo.variableValues),
			stepIndex, stepPath, allSteps, fgExecPath, docsRoot, hcpo.variableValues,
		), ", "),
		"WorkspacePath":         filepath.Join(GetPromptDocsRoot(), hcpo.GetWorkspacePath()),
		"ExecutionFolderPath":   filepath.Join(docsRoot, fgExecPath),
		"DownloadsPath":         filepath.Join(docsRoot, fgExecPath, "Downloads"),
		"StepNumber":            fmt.Sprintf("step-%d", stepIndex+1),
		"StepExecutionPath":     filepath.Join(docsRoot, executionPath),
		"ShellWorkingDirectory": shellWorkingDirectory,
		"PredefinedRoutes":      routesBuilder.String(),
		"EnableGenericAgent":    fmt.Sprintf("%t", step.EnableGenericAgent),
		"CurrentTodos":          buildCurrentTodosDisplay(tasksFileContent),
		"ProgressSummary":       buildProgressSummaryDisplay(tasksFileContent),
		"TasksFileContent":      tasksFileContent,
		"SubAgentResult":        lastSubAgentResult,
		"LastSubAgentName":      lastSubAgentName,
		"LastTodoID":            lastTodoID,
		"HasBrowserAccess":      fmt.Sprintf("%t", hcpo.GetBrowserMode() != "" && hcpo.GetBrowserMode() != "none"),
		// Add code execution mode and knowledgebase flags
		"IsCodeExecutionMode":  fmt.Sprintf("%v", isCodeExecutionMode),
		"UseKnowledgebase":     fmt.Sprintf("%v", useKnowledgebase),
		"SkipExecutionCleanup": fmt.Sprintf("%v", skipExecutionCleanup),
		// Workspace paths and folder guard (consistent with execution agent)
		"FolderGuardReadPaths":  strings.Join(toAbsPaths(docsRoot, fgReadPaths), ", "),
		"FolderGuardWritePaths": strings.Join(toAbsPaths(docsRoot, fgWritePaths), ", "),
		"KnowledgebasePath":     filepath.Join(docsRoot, fgKnowledgebasePath),
		"WorkflowRoot":          filepath.Join(docsRoot, baseWorkspacePath),
	}

	// Build previous steps summary (includes descriptions, output files, and execution results like human_input responses)
	previousStepsSummary := hcpo.buildPreviousStepsSummary(allSteps, stepIndex, previousContextFiles, previousExecutionResults)

	// Append workshop human input as critical feedback (passed via execute_step's human_input parameter)
	if hcpo.interactiveWorkflowHumanInput != "" {
		if previousStepsSummary == "" {
			previousStepsSummary = "## 📋 Previous Steps Context\n\n"
		}
		previousStepsSummary += fmt.Sprintf("\n## 🚨 HUMAN FEEDBACK (CRITICAL - READ CAREFULLY)\n\n")
		previousStepsSummary += "The human provided the following instructions via the interactive workshop.\n"
		previousStepsSummary += "**You MUST incorporate this human feedback into your work. This takes priority over other context.**\n\n"
		previousStepsSummary += fmt.Sprintf("```\n%s\n```\n", hcpo.interactiveWorkflowHumanInput)
	}

	templateVars["PreviousStepsSummary"] = previousStepsSummary

	// EnableDynamicTierSelection: enabled by default when tier resolver is available.
	// Can be explicitly disabled via step config enable_dynamic_tier_selection=false.
	enableDynamicTier := hcpo.tierResolver != nil
	if enableDynamicTier {
		if stepConfig := getAgentConfigs(step); stepConfig != nil &&
			stepConfig.EnableDynamicTierSelection != nil && !*stepConfig.EnableDynamicTierSelection {
			enableDynamicTier = false
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

	// Add orchestrator learning history if available
	if orchestratorLearningHistory != "" {
		templateVars["LearningHistory"] = orchestratorLearningHistory
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

// selectTodoTaskOrchestratorLLM selects the LLM config for todo task orchestrator.
//
// Priority:
//  1. step config OrchestratorLLM — explicit override always wins
//  2. step config TodoTaskOrchestratorTier — explicit tier override in tiered mode
//  3. Tier 1 (High) — default for orchestrator (returns nil if tier resolver is unavailable)
func (hcpo *StepBasedWorkflowOrchestrator) selectTodoTaskOrchestratorLLM(
	ctx context.Context,
	stepConfig *AgentConfigs,
	stepID string,
	stepPath string,
) *orchestrator.LLMConfig {
	// 1. Step config OrchestratorLLM always takes highest precedence.
	if stepConfig != nil && stepConfig.OrchestratorLLM != nil &&
		stepConfig.OrchestratorLLM.Provider != "" && stepConfig.OrchestratorLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 [STEP OVERRIDE] Todo task orchestrator using step-config OrchestratorLLM: %s/%s",
			stepConfig.OrchestratorLLM.Provider, stepConfig.OrchestratorLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: stepConfig.OrchestratorLLM.Provider,
				ModelID:  stepConfig.OrchestratorLLM.ModelID,
			},
			APIKeys: hcpo.GetAPIKeys(),
		}
	}

	// 2. Tiered mode: todo task orchestrators default to Tier 1 (High), including nested
	// todo-task orchestrators. An explicit todo_task_orchestrator_tier can override this.
	if hcpo.tierResolver == nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("selectTodoTaskOrchestratorLLM: tier resolver is nil for step %s — returning nil so caller surfaces a user-visible error", stepPath))
		return nil
	}
	tier := TierHigh
	if stepConfig != nil && stepConfig.TodoTaskOrchestratorTier != nil {
		switch *stepConfig.TodoTaskOrchestratorTier {
		case int(TierHigh):
			tier = TierHigh
		case int(TierMedium):
			tier = TierMedium
		case int(TierLow):
			tier = TierLow
		default:
			hcpo.GetLogger().Warn(fmt.Sprintf(
				"selectTodoTaskOrchestratorLLM: invalid todo_task_orchestrator_tier=%d for step %s (%s) — falling back to Tier %d (%s)",
				*stepConfig.TodoTaskOrchestratorTier,
				stepID,
				stepPath,
				int(tier),
				TierLevelLabel(tier),
			))
		}
	}
	llmConfig := hcpo.tierResolver.ResolveTier(tier)
	if llmConfig == nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("selectTodoTaskOrchestratorLLM: tier resolver returned nil for Tier %d (%s) on step %s", int(tier), TierLevelLabel(tier), stepPath))
		return nil
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Todo task orchestrator using Tier %d (%s): %s/%s",
		int(tier), TierLevelLabel(tier), llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
	return llmConfig
}

// executeTodoTaskOrchestratorAgent executes the orchestrator agent using the standard factory pattern
// This ensures proper event bridge connection for sub-event tracking
// Returns: response, updatedHistory, executionLLM, subAgentExecCtx, error
// The subAgentExecCtx contains execution state for sub-agent tool calls
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
	agentName := step.Title
	if agentName == "" {
		agentName = fmt.Sprintf("todo-task-orchestrator-step-%d", stepIndex+1)
	}

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
	// Propagate workshop correlation ID from the calling context so sub-agent events
	// are tagged with the workshop step's ID (enables frontend auto-notifications).
	workshopCorrelationID := ""
	if forcedID, ok := ctx.Value(events.ForceCorrelationIDKey).(string); ok {
		workshopCorrelationID = forcedID
	}
	subAgentExecCtx := &SubAgentExecutionContext{
		TodoTaskStep:          step,
		StepIndex:             stepIndex,
		StepPath:              stepPath,
		AllSteps:              allSteps,
		Progress:              progress,
		StepConfig:            stepConfig, // Pass step config for sub_agent_llm override
		WorkshopCorrelationID: workshopCorrelationID,
	}

	// Use factory method to create agent with proper event bridge connection
	// This handles initialization, event bridge connection, and tool registration
	agent, err := hcpo.createTodoTaskOrchestratorAgent(
		ctx,
		"todo_task", // phase
		stepIndex,   // step
		0,           // iteration
		stepID,
		stepPath, // step path for todo tools context injection
		agentName,
		stepConfig,
		llmConfig,
		subAgentExecCtx, // Sub-agent execution context for tool-based delegation
	)
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("failed to create todo task orchestrator agent: %w", err)
	}
	defer agent.Close()

	// Sync template vars with actual agent config — the factory may have overridden
	// code execution mode (for CLI providers) or tool search mode after template vars were built.
	if agent.GetConfig() != nil {
		if agent.GetConfig().UseCodeExecutionMode {
			templateVars["IsCodeExecutionMode"] = "true"
		}
		if getEffectiveToolSearchMode(agent.GetConfig()) {
			templateVars["UseToolSearchMode"] = "true"
		}
		// Show tools reference section for CLI providers ONLY when NOT in code execution mode.
		// In code exec mode, the {{TOOL_STRUCTURE}} JSON already provides the authoritative tool index.
		provider := agent.GetConfig().LLMConfig.Primary.Provider
		if isCliProviderForPrompt(provider) && !agent.GetConfig().UseCodeExecutionMode {
			templateVars["ShowToolsSection"] = "true"
		}
	}

	// Pre-save prompts.json so get_step_prompts works during execution (not just after)
	if todoAgent, ok := agent.(*WorkflowTodoTaskOrchestratorAgent); ok {
		preSystemPrompt := todoAgent.todoTaskOrchestratorSystemPromptProcessor(templateVars)
		preUserMessage := todoAgent.todoTaskOrchestratorUserMessageProcessor(templateVars, conversationHistory)
		hcpo.preSavePromptsJSON(stepIndex, step.GetID(), stepPath, "todo_task_orchestrator", preSystemPrompt, preUserMessage, executionLLM, "todo-task-prompts.json")
	}

	// Execute with tool-based approach (no structured output)
	// The agent manages tasks via shell (tasks.md) and delegates via call_sub_agent/call_generic_agent
	// Completion is detected by checking tasks.md — when all tasks are [x] done, the step is complete.
	_, updatedHistory, err := agent.Execute(ctx, templateVars, conversationHistory)
	if err != nil {
		return nil, nil, "", subAgentExecCtx, fmt.Errorf("todo task orchestrator execution failed: %w", err)
	}

	// Detect completion by checking tasks.md — when all tasks are [x], the step is done.
	// No need for mark_step_complete tool or completed.txt file.
	tasksFilePath := hcpo.getTodoTaskTasksFilePath(step.GetID(), stepPath)

	response := &TodoTaskResponse{}
	tasksContent, tasksErr := hcpo.ReadWorkspaceFile(ctx, tasksFilePath)
	if tasksErr == nil && tasksContent != "" {
		pending := strings.Count(tasksContent, "- [ ]")
		inProgress := strings.Count(tasksContent, "- [~]")
		completed := strings.Count(tasksContent, "- [x]")
		total := pending + inProgress + completed
		if total > 0 && pending == 0 && inProgress == 0 {
			response.NextAction = "complete"
			response.AllTasksComplete = true
			response.CompletionReason = fmt.Sprintf("All %d/%d tasks completed in tasks.md", completed, total)
			response.ProgressSummary = fmt.Sprintf("%d/%d completed", completed, total)
		} else {
			response.NextAction = "continue"
			response.AllTasksComplete = false
			response.ProgressSummary = fmt.Sprintf("%d/%d completed, %d pending, %d in-progress", completed, total, pending, inProgress)
		}
	} else {
		response.NextAction = "continue"
		response.AllTasksComplete = false
		response.ProgressSummary = "tasks.md not found"
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
) (string, []llmtypes.MessageContent, error) {
	// Use todoID as the task title
	// All actual task content comes from response.InstructionsToSubAgent
	taskTitle := response.TodoIDToExecute
	parentTodoTitle := step.GetTitle()
	if parentTodoTitle == "" {
		parentTodoTitle = stepPath
	}

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
		AgentConfigs: func() *AgentConfigs {
			// Inherit parallel tool execution setting from parent step
			var disableParallelToolExec *bool
			if parentConfig := getAgentConfigs(step); parentConfig != nil {
				disableParallelToolExec = parentConfig.DisableParallelToolExecution
			}
			return &AgentConfigs{
				DisableLearning:              boolPtr(true), // No learning for generic agent
				UseToolSearchMode:            useToolSearchMode,
				UseCodeExecutionMode:         useCodeExecutionMode,
				DisableParallelToolExecution: disableParallelToolExec, // inherit from parent
			}
		}(),
	}

	// Build generic step path
	genericStepPath := fmt.Sprintf("%s-generic-%s", stepPath, response.TodoIDToExecute)

	// Get execution path using full workspace-relative paths (consistent with setupExecutionFolderGuard)
	baseWorkspacePath := hcpo.GetWorkspacePath()
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	executionPath := getExecutionFolderPath(executionWorkspacePath, genericStepPath, genericStepPath)
	downloadsPath := fmt.Sprintf("%s/Downloads", executionWorkspacePath)

	// Setup folder guard for generic agent
	// Include parent step execution path so sub-agents can write output files
	// to the orchestrator's step folder (e.g., technical_check.json in step-3/)
	parentStepExecutionPath := getExecutionFolderPath(executionWorkspacePath, step.GetID(), stepPath)
	readPaths := []string{executionWorkspacePath, parentStepExecutionPath}
	writePaths := []string{executionPath, downloadsPath, parentStepExecutionPath}

	// Add knowledgebase folder paths if enabled
	if hcpo.UseKnowledgebase() {
		knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)
		readPaths = append(readPaths, knowledgebasePath)
		writePaths = append(writePaths, knowledgebasePath)
	}

	// Add skill folder paths to read paths (skills are read-only)
	genericStepConfig := getAgentConfigs(genericStep)
	genericEffectiveSkills := GetEffectiveSkills(genericStepConfig, hcpo.BaseOrchestrator)
	if len(genericEffectiveSkills) > 0 {
		skillReadPaths, _ := BuildSkillFolderGuardPaths(genericEffectiveSkills)
		readPaths = append(readPaths, skillReadPaths...)
	}

	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)

	// Build execution context
	var capturedHistory []llmtypes.MessageContent
	execCtx := &ExecutionContext{
		SkipHumanInput:             true, // Generic agents don't request human feedback
		FastExecuteMode:            false,
		FastExecuteEndStep:         -1,
		RunSingleStepOnly:          false,
		SingleStepTarget:           -1,
		ResumeBranchStep:           nil,
		IsEvaluationMode:           false,
		ConversationHistoryCapture: &capturedHistory,
	}

	// Notify sub-agent start
	agentID := fmt.Sprintf("todo-generic-%s-%s", stepPath, response.TodoIDToExecute)
	agentName := fmt.Sprintf("%s -> Generic (%s)", parentTodoTitle, taskTitle)
	subAgentCtx, subAgentCancel := context.WithCancel(ctx)
	defer subAgentCancel()
	if hcpo.subAgentNotifier != nil {
		hcpo.subAgentNotifier.OnSubAgentStart(WorkshopExecutionStart{
			ID:     agentID,
			Name:   agentName,
			Cancel: subAgentCancel,
		})
	}

	// Push context before sub-agent execution (preserve orchestrator context)
	if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PushContext("execution", stepIndex, genericStep.GetID(), genericStep.GetTitle())
	}

	// Execute using executeSingleStep (reuses standard execution infrastructure)
	executionResult, _, err := hcpo.executeSingleStep(
		subAgentCtx,
		genericStep,
		stepIndex,       // Use parent step index for context
		genericStepPath, // stepPath
		1,               // totalSteps = 1 for single generic task
		0,               // iteration
		[]string{},      // previousContextFiles - empty for generic tasks
		progress,        // progress
		true,            // isBranchStep = true (generic task is like a branch step)
		execCtx,         // execCtx
		allSteps,        // allSteps
		false,           // isDecisionInnerStep = false
		nil,             // decisionContext = nil
		"",              // decisionEvaluationQuestion - empty
		true,            // isSubAgent = true (sub-agents never request human feedback)
		[]string{response.InstructionsToSubAgent}, // previousExecutionResults - pass instructions
		nil, // orchestrationRoutes - none for generic agent
	)

	// Pop context after sub-agent execution (restore orchestrator context)
	if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PopContext()
	}

	// Notify sub-agent completion
	if hcpo.subAgentNotifier != nil {
		resultStr := fmt.Sprintf("Generic agent completed: %s", executionResult)
		if err != nil {
			resultStr = fmt.Sprintf("Generic agent failed: %v", err)
		}
		hcpo.subAgentNotifier.OnSubAgentComplete(agentID, agentName, resultStr, err)
	}

	if err != nil {
		return fmt.Sprintf("Generic agent failed: %v", err), capturedHistory, err
	}

	result := fmt.Sprintf("Generic agent completed: %s", executionResult)
	return result, capturedHistory, nil
}

// boolPtr returns a pointer to a bool value
func boolPtr(b bool) *bool {
	return &b
}

func appendDelegationInstructions(originalDescription, instructions string) string {
	if instructions == "" {
		return originalDescription
	}
	if originalDescription == "" {
		return instructions
	}
	return fmt.Sprintf("%s\n\n## Orchestrator Instructions\n\n%s", originalDescription, instructions)
}

func applyDelegationOverridesToCommonFields(fields *CommonStepFields, instructions string) {
	if fields == nil {
		return
	}
	fields.Description = appendDelegationInstructions(fields.Description, instructions)
}

func cloneStepWithDelegationOverrides(
	step PlanStepInterface,
	instructions string,
) (PlanStepInterface, error) {
	switch s := step.(type) {
	case *RegularPlanStep:
		stepCopy := *s
		applyDelegationOverridesToCommonFields(&stepCopy.CommonStepFields, instructions)
		return &stepCopy, nil
	case *ConditionalPlanStep:
		stepCopy := *s
		applyDelegationOverridesToCommonFields(&stepCopy.CommonStepFields, instructions)
		return &stepCopy, nil
	case *DecisionPlanStep:
		stepCopy := *s
		applyDelegationOverridesToCommonFields(&stepCopy.CommonStepFields, instructions)
		return &stepCopy, nil
	case *RoutingPlanStep:
		stepCopy := *s
		applyDelegationOverridesToCommonFields(&stepCopy.CommonStepFields, instructions)
		return &stepCopy, nil
	case *HumanInputPlanStep:
		stepCopy := *s
		applyDelegationOverridesToCommonFields(&stepCopy.CommonStepFields, instructions)
		return &stepCopy, nil
	case *TodoTaskPlanStep:
		stepCopy := *s
		if s.TodoTaskStep != nil {
			innerStepCopy, err := cloneStepWithDelegationOverrides(s.TodoTaskStep, instructions)
			if err != nil {
				return nil, err
			}
			stepCopy.TodoTaskStep = innerStepCopy
		}
		return &stepCopy, nil
	case *OrchestrationPlanStep:
		stepCopy := *s
		if s.OrchestrationStep != nil {
			innerStepCopy, err := cloneStepWithDelegationOverrides(s.OrchestrationStep, instructions)
			if err != nil {
				return nil, err
			}
			stepCopy.OrchestrationStep = innerStepCopy
		}
		return &stepCopy, nil
	default:
		return step, nil
	}
}


func (hcpo *StepBasedWorkflowOrchestrator) executeRoutedSubAgentStep(
	ctx context.Context,
	stepToExecute PlanStepInterface,
	stepIndex int,
	subAgentStepPath string,
	progress *StepProgress,
	execCtx *ExecutionContext,
	allSteps []PlanStepInterface,
	orchestrationRoutesForSubAgent []OrchestrationRoute,
) (string, []llmtypes.MessageContent, error) {
	var capturedHistory []llmtypes.MessageContent
	localExecCtx := execCtx
	if execCtx != nil {
		execCtxCopy := *execCtx
		execCtxCopy.ConversationHistoryCapture = &capturedHistory
		localExecCtx = &execCtxCopy
	}

	if isTodoTaskStep(stepToExecute) {
		successCriteriaMet, _, err := hcpo.executeTodoTaskStep(
			ctx,
			stepToExecute,
			stepIndex,
			progress,
			[]string{},
			[]string{},
			0,
			localExecCtx,
			allSteps,
			subAgentStepPath,
			nil,
		)
		if err != nil {
			return "", capturedHistory, err
		}
		if !successCriteriaMet {
			return "", capturedHistory, fmt.Errorf("nested todo task step did not complete successfully")
		}

		if todoTaskStep, ok := stepToExecute.(*TodoTaskPlanStep); ok && todoTaskStep.TodoTaskResponse != nil {
			if todoTaskStep.TodoTaskResponse.CompletionReason != "" {
				return todoTaskStep.TodoTaskResponse.CompletionReason, capturedHistory, nil
			}
			if todoTaskStep.TodoTaskResponse.ProgressSummary != "" {
				return todoTaskStep.TodoTaskResponse.ProgressSummary, capturedHistory, nil
			}
		}

		return "Nested todo task completed successfully", capturedHistory, nil
	}

	executionResult, _, err := hcpo.executeSingleStep(
		ctx,
		stepToExecute,
		stepIndex,
		subAgentStepPath,
		1,
		0,
		[]string{},
		progress,
		true,
		localExecCtx,
		allSteps,
		false,
		nil,
		"",
		true,
		[]string{},
		orchestrationRoutesForSubAgent,
	)
	return executionResult, capturedHistory, err
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
) (string, []llmtypes.MessageContent, error) {
	// Find the route
	var route *PlanOrchestrationRoute
	for i, r := range step.PredefinedRoutes {
		if r.RouteID == response.SelectedRouteID {
			route = &step.PredefinedRoutes[i]
			break
		}
	}
	if route == nil {
		return "", nil, fmt.Errorf("route %s not found in predefined routes", response.SelectedRouteID)
	}

	if route.SubAgentStep == nil {
		return "", nil, fmt.Errorf("route %s has no sub_agent_step defined", response.SelectedRouteID)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🤖 Executing predefined sub-agent: %s (%s)", route.RouteName, route.RouteID))
	parentTodoTitle := step.GetTitle()
	if parentTodoTitle == "" {
		parentTodoTitle = stepPath
	}

	// Use the sub-agent step from the route
	// CRITICAL: Create a COPY of the step to avoid modifying the original plan in memory
	// This keeps delegated instructions isolated from the original approved plan object.
	stepToExecute, err := cloneStepWithDelegationOverrides(
		route.SubAgentStep,
		response.InstructionsToSubAgent,
	)
	if err != nil {
		return "", nil, fmt.Errorf("failed to clone delegated sub-agent step: %w", err)
	}
	if err := validateTodoTaskNestingDepth(stepToExecute, strings.Count(stepPath, "-sub-")+1); err != nil {
		return "", nil, fmt.Errorf("route %s exceeds supported todo_task nesting depth: %w", response.SelectedRouteID, err)
	}

	// Build sub-agent step path
	subAgentStepPath := fmt.Sprintf("%s-sub-%s", stepPath, route.RouteID)

	// Get execution path using full workspace-relative paths (consistent with setupExecutionFolderGuard)
	baseWorkspacePath := hcpo.GetWorkspacePath()
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	executionPath := getExecutionFolderPath(executionWorkspacePath, route.SubAgentStep.GetID(), subAgentStepPath)
	downloadsPath := fmt.Sprintf("%s/Downloads", executionWorkspacePath)
	parentStepExecutionPath := getExecutionFolderPath(executionWorkspacePath, step.GetID(), stepPath)

	// Setup folder guard for sub-agent
	// Include parent step execution path so sub-agents can write output files
	// to the orchestrator's step folder (e.g., technical_check.json in step-3/)
	readPaths := []string{executionWorkspacePath, parentStepExecutionPath}
	writePaths := []string{executionPath, downloadsPath, parentStepExecutionPath}

	// Add knowledgebase folder paths if enabled
	if hcpo.UseKnowledgebase() {
		knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)
		readPaths = append(readPaths, knowledgebasePath)
		writePaths = append(writePaths, knowledgebasePath)
	}

	// Add skill folder paths to read paths (skills are read-only)
	subAgentStepConfig := getAgentConfigs(stepToExecute)
	subAgentEffectiveSkills := GetEffectiveSkills(subAgentStepConfig, hcpo.BaseOrchestrator)
	if len(subAgentEffectiveSkills) > 0 {
		skillReadPaths, _ := BuildSkillFolderGuardPaths(subAgentEffectiveSkills)
		readPaths = append(readPaths, skillReadPaths...)
	}

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
	var capturedHistory []llmtypes.MessageContent
	execCtx := &ExecutionContext{
		SkipHumanInput:             true, // Sub-agents don't request human feedback
		FastExecuteMode:            false,
		FastExecuteEndStep:         -1,
		RunSingleStepOnly:          false,
		SingleStepTarget:           -1,
		ResumeBranchStep:           nil,
		IsEvaluationMode:           false,
		ConversationHistoryCapture: &capturedHistory,
	}

	// Notify sub-agent start
	subAgentNotifID := fmt.Sprintf("todo-sub-%s-%s", stepPath, route.RouteID)
	subAgentNotifName := fmt.Sprintf("%s -> %s (%s)", parentTodoTitle, route.RouteName, response.TodoIDToExecute)
	subAgentCtx, subAgentCancel := context.WithCancel(ctx)
	defer subAgentCancel()
	if hcpo.subAgentNotifier != nil {
		hcpo.subAgentNotifier.OnSubAgentStart(WorkshopExecutionStart{
			ID:     subAgentNotifID,
			Name:   subAgentNotifName,
			Cancel: subAgentCancel,
		})
	}

	// Push context before sub-agent execution (preserve orchestrator context)
	if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PushContext("execution", stepIndex, route.SubAgentStep.GetID(), route.SubAgentStep.GetTitle())
	}

	executionResult, capturedHistory, err := hcpo.executeRoutedSubAgentStep(
		subAgentCtx,
		stepToExecute,
		stepIndex,
		subAgentStepPath,
		progress,
		execCtx,
		allSteps,
		orchestrationRoutesForSubAgent,
	)

	// Pop context after sub-agent execution (restore orchestrator context)
	if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
		cab.PopContext()
	}

	// Notify sub-agent completion
	if hcpo.subAgentNotifier != nil {
		resultStr := fmt.Sprintf("Sub-agent %s completed: %s", route.RouteName, executionResult)
		if err != nil {
			resultStr = fmt.Sprintf("Sub-agent %s failed: %v", route.RouteName, err)
		}
		hcpo.subAgentNotifier.OnSubAgentComplete(subAgentNotifID, subAgentNotifName, resultStr, err)
	}

	if err != nil {
		return fmt.Sprintf("Sub-agent %s failed: %v", route.RouteName, err), capturedHistory, err
	}

	result := fmt.Sprintf("Sub-agent %s completed: %s", route.RouteName, executionResult)
	return result, capturedHistory, nil
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

	// Extract preferred tier from context (set by call_sub_agent/call_generic_agent tools)
	var preferredTier int
	var preferredTierLabel string
	if tier, ok := ctx.Value(virtualtools.PreferredTierContextKey).(int); ok && tier >= 1 && tier <= 3 {
		preferredTier = tier
		preferredTierLabel = TierLevelLabel(TierLevel(tier))
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
		PreferredTier:          preferredTier,
		PreferredTierLabel:     preferredTierLabel,
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

// todoTaskStatusDebouncer coalesces rapid-fire todo task status update events
// from parallel sub-agent completions into a single debounced emission.
type todoTaskStatusDebouncer struct {
	mu    sync.Mutex
	timer *time.Timer
	delay time.Duration
	// Latest context to use when the debounce fires
	latestCtx     context.Context
	latestArgs    map[string]interface{}
	latestExecCtx *SubAgentExecutionContext
	latestPhase   string
}

func newTodoTaskStatusDebouncer(delay time.Duration) *todoTaskStatusDebouncer {
	return &todoTaskStatusDebouncer{delay: delay}
}

// emitTodoTaskStatusUpdate schedules a debounced status update event.
// If called multiple times within the debounce window, only the last call's
// context is used and a single event is emitted after the window expires.
func (hcpo *StepBasedWorkflowOrchestrator) emitTodoTaskStatusUpdate(
	ctx context.Context,
	args map[string]interface{},
	execCtx *SubAgentExecutionContext,
	phase string,
) {
	if hcpo.todoStatusDebouncer == nil {
		hcpo.todoStatusDebouncer = newTodoTaskStatusDebouncer(1 * time.Second)
	}

	d := hcpo.todoStatusDebouncer
	d.mu.Lock()
	defer d.mu.Unlock()

	// Store latest call context
	d.latestCtx = ctx
	d.latestArgs = args
	d.latestExecCtx = execCtx
	d.latestPhase = phase

	// Reset or start the debounce timer
	if d.timer != nil {
		d.timer.Stop()
	}
	d.timer = time.AfterFunc(d.delay, func() {
		hcpo.doEmitTodoTaskStatusUpdate()
	})
}

// doEmitTodoTaskStatusUpdate performs the actual tasks.md read and event emission.
// Called by the debounce timer after the window expires.
func (hcpo *StepBasedWorkflowOrchestrator) doEmitTodoTaskStatusUpdate() {
	d := hcpo.todoStatusDebouncer
	d.mu.Lock()
	ctx := d.latestCtx
	args := d.latestArgs
	execCtx := d.latestExecCtx
	phase := d.latestPhase
	d.mu.Unlock()

	bridge := hcpo.GetContextAwareBridge()
	if bridge == nil {
		hcpo.GetLogger().Warn("📋 emitTodoTaskStatusUpdate: bridge is nil, skipping")
		return
	}

	stepPath := ""
	stepIndex := 0
	var stepID, stepTitle string
	if execCtx != nil {
		stepPath = execCtx.StepPath
		stepIndex = execCtx.StepIndex
		if execCtx.TodoTaskStep != nil {
			stepID = execCtx.TodoTaskStep.GetID()
			stepTitle = execCtx.TodoTaskStep.GetTitle()
		}
	}

	// Build tasks.md path (use relative path so ReadWorkspaceFile prepends workspacePath correctly)
	tasksFilePath := hcpo.getTodoTaskTasksFilePath(stepID, stepPath)

	hcpo.GetLogger().Info(fmt.Sprintf("📋 emitTodoTaskStatusUpdate(debounced): reading tasks.md at '%s'", tasksFilePath))

	tasksContent, err := hcpo.ReadWorkspaceFile(ctx, tasksFilePath)
	if err != nil || tasksContent == "" {
		hcpo.GetLogger().Info(fmt.Sprintf("📋 emitTodoTaskStatusUpdate(debounced): tasks.md not found or empty at '%s' (err=%v), skipping", tasksFilePath, err))
		return
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📋 emitTodoTaskStatusUpdate(debounced): found tasks.md (%d chars), emitting event", len(tasksContent)))

	routeID, _ := args["route_id"].(string)
	todoID, _ := args["todo_id"].(string)

	event := &TodoTaskStatusUpdateEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: time.Now(),
			Component: "orchestrator",
		},
		StepIndex:    stepIndex,
		StepPath:     stepPath,
		StepID:       stepID,
		StepTitle:    stepTitle,
		TasksContent: tasksContent,
		RouteID:      routeID,
		TodoID:       todoID,
		StatusPhase:  phase,
	}

	agentEvent := &baseevents.AgentEvent{
		Type:      events.TodoTaskStatusUpdate,
		Timestamp: time.Now(),
		Data:      event,
	}

	if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to emit todo task status update event: %v", err))
	} else {
		hcpo.GetLogger().Info("✅ emitTodoTaskStatusUpdate(debounced): event emitted successfully")
	}
}

// flushTodoTaskStatusDebouncer forces immediate emission of any pending debounced event.
// Call this before the step completes to ensure the final state is emitted.
func (hcpo *StepBasedWorkflowOrchestrator) flushTodoTaskStatusDebouncer() {
	if hcpo.todoStatusDebouncer == nil {
		return
	}
	d := hcpo.todoStatusDebouncer
	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	hasLatest := d.latestCtx != nil
	d.mu.Unlock()

	if hasLatest {
		hcpo.doEmitTodoTaskStatusUpdate()
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

	validationFolderPath := getValidationFolderPath(validationWorkspacePath, step.GetID(), stepPath)
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

	// Extract preferred tier from context (set by call_sub_agent/call_generic_agent tools)
	var preferredTier int
	var preferredTierLabel string
	if tier, ok := ctx.Value(virtualtools.PreferredTierContextKey).(int); ok && tier >= 1 && tier <= 3 {
		preferredTier = tier
		preferredTierLabel = TierLevelLabel(TierLevel(tier))
	}

	// Build log entry
	routingEntry := map[string]interface{}{
		"type":       "routing",
		"step_index": stepIndex + 1,
		"step_path":  stepPath,
		"step_id":    step.GetID(),
		"step_title": step.GetTitle(),
		"iteration":  iteration + 1,
		"model":      executionLLM,
		"todo_task_response": map[string]interface{}{
			"next_action":                    response.NextAction,
			"selected_route_id":              response.SelectedRouteID,
			"selected_route_name":            selectedRouteName,
			"use_generic_agent":              response.UseGenericAgent,
			"selected_sub_agent_path":        selectedSubAgentPath,
			"todo_id_to_execute":             response.TodoIDToExecute,
			"todo_title":                     todoTitle,
			"instructions_to_sub_agent":      response.InstructionsToSubAgent,
			"success_criteria_for_sub_agent": response.SuccessCriteriaForSubAgent,
			"all_tasks_complete":             response.AllTasksComplete,
			"progress_summary":               response.ProgressSummary,
			"completion_reason":              response.CompletionReason,
			"preferred_tier":                 preferredTier,
			"preferred_tier_label":           preferredTierLabel,
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
	stepID string,
	stepPath string,
	iteration int,
	executionLLM string,
	conversationHistory []llmtypes.MessageContent,
) {
	// Use background context so logs are persisted even if execution was canceled.
	saveCtx := context.Background()

	// Get workspace path for logs folder
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}

	// Get execution logs folder path
	executionLogsFolderPath := getExecutionFolderPathForLogs(validationWorkspacePath, stepID, stepPath)

	// Create filename: execution-attempt-1-iteration-{iteration}.json
	// Use attempt=1 since todo task orchestrator doesn't have retry attempts like regular steps
	filename := fmt.Sprintf("execution-attempt-1-iteration-%d.json", iteration)
	filePath := fmt.Sprintf("%s/%s", executionLogsFolderPath, filename)
	conversationPath := strings.TrimSuffix(filePath, ".json") + "-conversation.json"

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
	if err := hcpo.WriteWorkspaceFile(saveCtx, filePath, string(logJSON)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save todo task execution log to %s: %v", filePath, err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("💾 Todo task execution log saved to: %s", filePath))
	}

	// Save the full conversation history in the same format regular execution logs use,
	// so the execution popup can open it via the inferred conversation_path.
	conversationLog := map[string]interface{}{
		"step_path":            stepPath,
		"retry_attempt":        1,
		"loop_iteration":       iteration,
		"conversation_history": conversationHistory,
		"timestamp":            time.Now().Format(time.RFC3339),
	}
	conversationJSON, err := json.MarshalIndent(conversationLog, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal todo task conversation log: %v", err))
		return
	}

	if err := hcpo.WriteWorkspaceFile(saveCtx, conversationPath, string(conversationJSON)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save todo task conversation log to %s: %v", conversationPath, err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("💬 Todo task conversation log saved to: %s", conversationPath))
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
