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
	orchestratoragents "mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
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

// executeTodoTaskStep executes a todo task step by:
//  1. The orchestrator LLM delegates to sub-agents and/or executes directly
//  2. Processing tool calls:
//     - call_sub_agent: Delegate to predefined sub-agents (with learning/prevalidation)
//     - call_generic_agent: Delegate to generic agent (no learning/prevalidation)
//  3. Pre-validation checks output files after execution
//  4. Retry with feedback on pre-validation failure (up to 3 attempts)
//  5. Return success status and next step ID
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
	// The orchestrator needs to read/write output files and access workspace files
	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepID := step.GetID()
	if stepID == "" {
		stepID = fmt.Sprintf("step-%d", stepIndex+1)
	}

	// Build paths for folder guard
	// All paths should include the workspace prefix (e.g., Workflow/codeanalysis/...)

	executionWorkspacePath := hcpo.getTodoTaskExecutionWorkspacePath()
	stepExecutionPath := hcpo.getTodoTaskStepExecutionPath(stepID, todoTaskStepPath)
	// DB folder: Workflow/codeanalysis/db/ (structured JSON data, always enabled, shared across runs)
	dbPath := getDBPath(baseWorkspacePath)
	skillStepConfig := getAgentConfigs(step)
	kbAccessForGuard := resolveKnowledgebaseAccess(skillStepConfig, hcpo.UseKnowledgebase())
	kbWriteMethodForGuard := resolveKnowledgebaseWriteMethod(skillStepConfig)
	learningsAccessForGuard := resolveLearningsAccess(skillStepConfig)

	// READ: current group's execution folder + db, plus KB/learnings only when
	// the step config grants those stores. WRITE: current group's execution
	// folder + db, plus KB notes only for direct KB writes.
	// Do not grant the workflow root here. That would expose workflow.json, variables/,
	// planning/, and sibling groups to a nested todo_task orchestrator whose job is
	// to coordinate work inside the current run, not inspect global workflow state.
	readPaths := []string{executionWorkspacePath, dbPath}
	writePaths := []string{executionWorkspacePath, dbPath}
	if learningsAccessForGuard != LearningsAccessNone {
		readPaths = append(readPaths, filepath.Join(baseWorkspacePath, "learnings", GlobalLearningID))
	}
	if kbAccessAllowsRead(kbAccessForGuard) {
		readPaths = append(readPaths, getKnowledgebasePath(baseWorkspacePath))
	}
	if kbAccessAllowsWrite(kbAccessForGuard) && kbWriteMethodForGuard == KBWriteMethodDirect {
		writePaths = append(writePaths, filepath.Join(getKnowledgebasePath(baseWorkspacePath), "notes"))
	}

	// Add skill folder paths to read paths (skills are read-only)
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

	// Keep only the latest iteration conversation history in-memory.
	// Todo-task state should come from current files (outputs, tool results),
	// not from replaying previous assistant narration across loop iterations.
	var conversationHistory []llmtypes.MessageContent
	defer func() {
		if execCtx != nil && execCtx.ConversationHistoryCapture != nil {
			historyCopy := append([]llmtypes.MessageContent(nil), conversationHistory...)
			*execCtx.ConversationHistoryCapture = historyCopy
		}
	}()

	stepConfig := getAgentConfigs(todoTaskStep)

	// Orchestrator learn_code fast path — builder-authored main.py runs before any LLM work.
	// Runs only when the step is learn_code-eligible (declared_execution_mode=learn_code plus
	// at least one predefined route) AND learnings/{stepID}/main.py exists.
	// Success → step done with zero LLM tokens. Any failure → fall through to normal LLM
	// orchestrator path with a fresh start (no state carryover). Unlike regular-step learn_code,
	// there is no repair loop and no save-back: the builder owns main.py, runtime just runs it.
	//
	// declared_execution_mode often lives only in step_config.json (not in plan.json's embedded
	// AgentConfigs). Mirror controller_execution.go's fallback: if the embedded config doesn't
	// declare learn_code, scan step_config.json for a matching step ID before deciding.
	fastPathConfig := stepConfig
	embeddedMode := ""
	if stepConfig != nil {
		embeddedMode = stepConfig.DeclaredExecutionMode
	}
	if (fastPathConfig == nil || !isScriptedExecutionModeConfig(fastPathConfig)) && stepID != "" {
		stepConfigs, cfgErr := hcpo.ReadStepConfigs(ctx)
		if cfgErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("🐍 [orchestrator_learn_code] ReadStepConfigs failed for step %s: %v", stepID, cfgErr))
		} else {
			found := false
			for _, sc := range stepConfigs {
				if sc.ID == stepID {
					found = true
					fileMode := ""
					if sc.AgentConfigs != nil {
						fileMode = sc.AgentConfigs.DeclaredExecutionMode
					}
					hcpo.GetLogger().Info(fmt.Sprintf("🐍 [orchestrator_learn_code] step_config.json scan hit for %s: declared_execution_mode=%q", stepID, fileMode))
					if isScriptedExecutionModeConfig(sc.AgentConfigs) {
						fastPathConfig = sc.AgentConfigs
					}
					break
				}
			}
			if !found {
				hcpo.GetLogger().Info(fmt.Sprintf("🐍 [orchestrator_learn_code] step_config.json scan miss for %s (scanned %d entries)", stepID, len(stepConfigs)))
			}
		}
	}
	mergedMode := ""
	if fastPathConfig != nil {
		mergedMode = fastPathConfig.DeclaredExecutionMode
	}
	scriptExists := hcpo.hasValidLearnedScriptAPI(ctx, stepID)
	hcpo.GetLogger().Info(fmt.Sprintf("🐍 [orchestrator_learn_code] eligibility check for %s: embedded_mode=%q merged_mode=%q routes=%d script_exists=%v",
		stepID, embeddedMode, mergedMode, len(todoTaskStep.PredefinedRoutes), scriptExists))
	if isOrchestratorLearnCodeEligible(todoTaskStep, fastPathConfig) && scriptExists {
		hcpo.GetLogger().Info(fmt.Sprintf("🐍 [orchestrator_learn_code] Attempting builder-authored main.py for step %d (%s)", stepIndex+1, stepID))

		// Register sub-agent executors in the session-scoped tool registry so main.py
		// can reach /tools/custom/call_sub_agent. Mirror the SubAgentExecutionContext
		// that executeTodoTaskOrchestratorAgent builds so routes, tier selection, and
		// LLM overrides all behave identically to the LLM path.
		workshopCorrelationID := ""
		if forcedID, ok := ctx.Value(events.ForceCorrelationIDKey).(string); ok {
			workshopCorrelationID = forcedID
		}
		fastPathExecCtx := &SubAgentExecutionContext{
			TodoTaskStep:          todoTaskStep,
			StepIndex:             stepIndex,
			StepPath:              todoTaskStepPath,
			AllSteps:              allSteps,
			Progress:              progress,
			StepConfig:            stepConfig,
			WorkshopCorrelationID: workshopCorrelationID,
		}
		hcpo.restoreSubAgentToolExecutors(fastPathExecCtx)

		stepExecutionRelPath := hcpo.getTodoTaskStepExecutionPath(stepID, todoTaskStepPath)
		fastResult := hcpo.tryRunSavedLearnCodeScript(ctx, step, stepIndex, todoTaskStepPath, allSteps,
			stepExecutionRelPath, executionWorkspacePath)

		if fastResult.RanScript {
			savedScriptPath := getLearnCodeScriptAbsPath(GetPromptDocsRoot(), hcpo.GetWorkspacePath(), stepID, hcpo.isEvaluationMode)
			hcpo.emitLearnCodeScriptExecutionEvent(ctx, step, stepIndex, todoTaskStepPath,
				savedScriptPath, fastResult.Success, fastResult.ExitCode, fastResult.Output, fastResult.Error, 0, true)
			hcpo.saveLearnCodeFastPathLog(ctx, stepIndex, stepID, todoTaskStepPath, savedScriptPath, fastResult)
		}

		if fastResult.RanScript && fastResult.Success {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ [orchestrator_learn_code] Fast path succeeded for step %d — 0 LLM tokens", stepIndex+1))
			hcpo.emitTodoTaskStepCompletedEvent(ctx, step, stepIndex, todoTaskStepPath, 1, nil, "learn_code: builder-authored script executed and validated", todoTaskStep.NextStepID)
			hcpo.emitStepFinishedEvent(ctx, step, stepIndex, todoTaskStepPath, false)
			return true, todoTaskStep.NextStepID, nil
		}

		// Stop must be terminal. If the builder-authored fast path failed because the
		// parent context was canceled, do not fall back into a fresh LLM orchestrator run.
		if ctx.Err() != nil {
			return false, "", fmt.Errorf("todo task execution canceled: %w", ctx.Err())
		}

		if fastResult.RanScript {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [orchestrator_learn_code] Builder-authored script failed for step %d — falling back to LLM orchestrator (fresh start)", stepIndex+1))
		}

		// fast_path_only=true (SavedScriptOnly) — workshop-level debug mode that forbids any
		// LLM fallback. Mirror controller_execution.go's behavior for regular learn_code steps.
		if execCtx != nil && execCtx.SavedScriptOnly {
			if fastResult.RanScript {
				return false, "", fmt.Errorf("saved main.py failed for orchestrator step %q:\n%s", stepID, fastResult.Error)
			}
			return false, "", fmt.Errorf("no saved main.py found for orchestrator step %q in learnings/%s/main.py", stepID, stepID)
		}
	} else if execCtx != nil && execCtx.SavedScriptOnly {
		// fast_path_only was requested but this step isn't learn_code-eligible — fail loudly
		// rather than silently running the LLM orchestrator (caller expected zero-LLM execution).
		return false, "", fmt.Errorf("orchestrator step %q is not in learn_code mode (requires declared_execution_mode=\"learn_code\" and ≥1 predefined route)", stepID)
	}

	// Learnings read gate — default-on unless learnings_access="none" or routing/eval.
	// Todo-task agents benefit from seeing _global/SKILL.md to reuse cross-step knowledge.
	isLearningDisabled := !canReadLearnings(stepConfig, todoTaskStep, hcpo.isEvaluationMode)
	// Check for context cancellation
	select {
	case <-ctx.Done():
		return false, "", fmt.Errorf("todo task execution canceled: %w", ctx.Err())
	default:
	}

	// Load orchestrator learnings from global learning skill
	var orchestratorLearningHistory string
	if isLearningDisabled {
		orchestratorLearningHistory = ""
	} else {
		orchestratorLearningHistory, _ = hcpo.readGlobalLearningHistory(ctx)
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
		orchestratorLearningHistory,
		execCtx,
	)

	// Capture tool calls and wall-clock duration per attempt so persisted logs show
	// where todo orchestration time was spent.
	var capturedToolCalls []orchestrator.ToolCallEntry
	var capturedLLMCalls []orchestrator.LLMCallEntry

	// Retry loop: execute with validation feedback on pre-validation failure
	maxRetryAttempts := 3
	validationSchema := step.GetValidationSchema()
	var validationResponse *ValidationResponse
	var todoTaskAgent orchestratoragents.OrchestratorAgent
	defer func() {
		if todoTaskAgent != nil {
			_ = todoTaskAgent.Close()
		}
	}()

	for retryAttempt := 1; retryAttempt <= maxRetryAttempts; retryAttempt++ {
		// Check for context cancellation before each attempt
		select {
		case <-ctx.Done():
			return false, "", fmt.Errorf("todo task execution canceled: %w", ctx.Err())
		default:
		}

		// On retry, inject validation feedback so the LLM knows what to fix
		if retryAttempt > 1 && validationResponse != nil {
			contextStr := fmt.Sprintf("Pre-Validation Feedback (Retry Attempt %d/%d)", retryAttempt, maxRetryAttempts)
			templateVars["ValidationFeedback"] = hcpo.formatValidationResponseForTemplate(validationResponse, contextStr)
			hcpo.GetLogger().Info(fmt.Sprintf("🔄 Retrying todo task step %d execution with validation feedback (attempt %d/%d)", stepIndex+1, retryAttempt, maxRetryAttempts))
		} else {
			templateVars["ValidationFeedback"] = ""
		}

		hcpo.GetLogger().Info(fmt.Sprintf("🎯 Executing todo task orchestrator (attempt %d/%d)", retryAttempt, maxRetryAttempts))
		if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
			cab.StartTimingCapture()
		}
		attemptStartedAt := time.Now().UTC()

		var (
			updatedHistory []llmtypes.MessageContent
			executionLLM   string
			err            error
		)
		shouldContinue := retryAttempt > 1 && todoTaskAgent != nil && len(conversationHistory) > 0
		if !shouldContinue {
			if todoTaskAgent != nil {
				_ = todoTaskAgent.Close()
				todoTaskAgent = nil
			}
			_, updatedHistory, executionLLM, _, todoTaskAgent, err = hcpo.executeTodoTaskOrchestratorAgent(
				ctx,
				todoTaskStep,
				stepIndex,
				todoTaskStepPath,
				templateVars,
				conversationHistory,
				allSteps,
				progress,
			)
		} else {
			feedbackUserMsg := buildValidationContinuationUserMessage(validationResponse, retryAttempt)
			hcpo.GetLogger().Info(fmt.Sprintf("🔁 Todo task step %d attempt %d/%d: continuing existing orchestrator with validation feedback (history=%d turns)",
				stepIndex+1, retryAttempt, maxRetryAttempts, len(conversationHistory)))
			executionLLM = agentConfigModelLabel(todoTaskAgent.GetConfig())
			ba := todoTaskAgent.GetBaseAgent()
			if ba == nil {
				return false, "", fmt.Errorf("todo task orchestrator has no base agent for continuation on attempt %d", retryAttempt)
			}
			_, updatedHistory, err = ba.Execute(ctx, feedbackUserMsg, conversationHistory, "", false)
		}
		attemptCompletedAt := time.Now().UTC()
		attemptDuration := attemptCompletedAt.Sub(attemptStartedAt)

		// Drain captured tool calls regardless of error
		if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
			timingCapture := cab.DrainTimingCapture()
			capturedToolCalls = timingCapture.ToolCalls
			capturedLLMCalls = timingCapture.LLMCalls
		}

		if err != nil {
			return false, "", fmt.Errorf("todo task orchestrator failed: %w", err)
		}
		conversationHistory = updatedHistory

		// Log execution
		hcpo.saveTodoTaskExecutionLog(ctx, step.GetID(), todoTaskStepPath, retryAttempt-1, executionLLM, updatedHistory, capturedToolCalls, capturedLLMCalls, attemptStartedAt, attemptCompletedAt, attemptDuration)

		// Run pre-validation if schema exists
		if validationSchema != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 Running pre-validation after execution (attempt %d/%d)", retryAttempt, maxRetryAttempts))
			preValidationPassed, formattedResults := hcpo.runTodoTaskPreValidation(ctx, step, stepIndex, todoTaskStepPath, stepExecutionPath)

			if preValidationPassed {
				hcpo.GetLogger().Info("✅ Todo task step complete (pre-validation passed)")
				hcpo.emitTodoTaskStepCompletedEvent(ctx, step, stepIndex, todoTaskStepPath, 1, nil, "Pre-validation passed", todoTaskStep.NextStepID)
				hcpo.emitStepFinishedEvent(ctx, step, stepIndex, todoTaskStepPath, false)
				return true, todoTaskStep.NextStepID, nil
			}

			// Build validation response for feedback on next retry
			validationResponse = &ValidationResponse{
				IsSuccessCriteriaMet: false,
				ExecutionStatus:      "FAILED",
				Reasoning:            formattedResults + "\n\nPre-validation failed - required output files are missing or invalid. Fix these issues.",
				Feedback: []ValidationFeedback{{
					Type:        "structural_validation",
					Description: "Pre-validation failed - output structure does not meet requirements",
					Severity:    "HIGH",
				}},
			}

			if retryAttempt >= maxRetryAttempts {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Todo task step %d pre-validation failed after %d attempts", stepIndex+1, maxRetryAttempts), nil)
				return false, todoTaskStep.NextStepID, nil
			}

			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Pre-validation failed for todo task step %d (attempt %d/%d) - retrying with feedback", stepIndex+1, retryAttempt, maxRetryAttempts))
			continue
		}

		// No validation schema — execution completion is the signal
		hcpo.GetLogger().Info("✅ Todo task step complete (execution finished)")
		hcpo.emitTodoTaskStepCompletedEvent(ctx, step, stepIndex, todoTaskStepPath, 1, nil, "Execution completed", todoTaskStep.NextStepID)
		hcpo.emitStepFinishedEvent(ctx, step, stepIndex, todoTaskStepPath, false)
		return true, todoTaskStep.NextStepID, nil
	}

	// Should not reach here, but handle gracefully
	return false, todoTaskStep.NextStepID, nil
}

// buildTodoTaskOrchestratorTemplateVars builds template variables for the orchestrator agent
func (hcpo *StepBasedWorkflowOrchestrator) buildTodoTaskOrchestratorTemplateVars(
	ctx context.Context,
	step *TodoTaskPlanStep,
	stepIndex int,
	stepPath string,
	previousContextFiles []string,
	previousExecutionResults []string,
	allSteps []PlanStepInterface,
	orchestratorLearningHistory string, // Persisted learnings from previous runs
	execCtx *ExecutionContext,
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

	// Resolve KB access mode for this step (explicit step config > preset default).
	kbAccess := resolveKnowledgebaseAccess(stepConfig, hcpo.UseKnowledgebase())
	kbWriteMethod := resolveKnowledgebaseWriteMethod(stepConfig)
	learningsAccess := resolveLearningsAccess(stepConfig)
	useKnowledgebase := kbAccess != KBAccessNone

	// Build folder guard paths for prompt (same logic as executeTodoTaskStep setup)
	docsRoot := GetPromptDocsRoot()
	fgExecPath := hcpo.getTodoTaskExecutionWorkspacePath()
	fgGlobalLearningsPath := filepath.Join(baseWorkspacePath, "learnings", GlobalLearningID)
	fgKnowledgebasePath := getKnowledgebasePath(baseWorkspacePath)
	fgDBPath := getDBPath(baseWorkspacePath)
	fgSoulPath := filepath.Join(baseWorkspacePath, "soul")
	fgBuilderPath := filepath.Join(baseWorkspacePath, "builder")
	fgReadPaths := []string{fgExecPath, fgDBPath, fgSoulPath, fgBuilderPath}
	fgWritePaths := []string{fgExecPath, fgDBPath}
	if learningsAccess != LearningsAccessNone {
		fgReadPaths = append(fgReadPaths, fgGlobalLearningsPath)
	}
	if kbAccessAllowsRead(kbAccess) {
		fgReadPaths = append(fgReadPaths, fgKnowledgebasePath)
	}
	if kbAccessAllowsWrite(kbAccess) && kbWriteMethod == KBWriteMethodDirect {
		fgWritePaths = append(fgWritePaths, filepath.Join(fgKnowledgebasePath, "notes"))
	}

	templateVars := map[string]string{
		// Resolve variables in step metadata
		"StepTitle":           ResolveVariables(step.GetTitle(), hcpo.variableValues),
		"StepDescription":     ResolveVariables(step.GetDescription(), hcpo.variableValues),
		"StepSuccessCriteria": "",
		"StepContextDependencies": func() string {
			resolvedDeps := hcpo.resolveDependencyPathsWithWorkspace(
				ctx,
				ResolveVariablesArray(previousContextFiles, hcpo.variableValues),
				stepIndex, stepPath, allSteps, fgExecPath, docsRoot, hcpo.variableValues,
			)
			formatted, err := hcpo.formatContextDependenciesWithContent(ctx, resolvedDeps, docsRoot)
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to inline context deps for todo task step: %v", err))
				return strings.Join(resolvedDeps, ", ")
			}
			return formatted
		}(),
		"WorkspacePath":         filepath.Join(GetPromptDocsRoot(), hcpo.GetWorkspacePath()),
		"ExecutionFolderPath":   filepath.Join(docsRoot, fgExecPath),
		"DownloadsPath":         filepath.Join(docsRoot, fgExecPath, "Downloads"),
		"StepNumber":            fmt.Sprintf("step-%d", stepIndex+1),
		"StepExecutionPath":     filepath.Join(docsRoot, executionPath),
		"ShellWorkingDirectory": shellWorkingDirectory,
		"PredefinedRoutes":      routesBuilder.String(),
		"HasBrowserAccess":      fmt.Sprintf("%t", hcpo.HasBrowserCapability()),
		// Add code execution mode and knowledgebase flags
		"IsCodeExecutionMode":       fmt.Sprintf("%v", isCodeExecutionMode),
		"UseKnowledgebase":          fmt.Sprintf("%v", useKnowledgebase), // deprecated, retained for back-compat in template
		"KbAccess":                  kbAccess,
		"KbAccessLabel":             kbAccessLabel(kbAccess),
		"KbWriteMethod":             kbWriteMethod,
		"KnowledgebaseContribution": kbContributionForPrompt(stepConfig),
		"KBGuidanceBlock":           BuildStepKBGuidance(kbAccess, kbWriteMethod, kbContributionForPrompt(stepConfig)),
		// Workspace paths and folder guard (consistent with execution agent)
		"FolderGuardReadPaths":  strings.Join(toAbsPaths(docsRoot, fgReadPaths), ", "),
		"FolderGuardWritePaths": strings.Join(toAbsPaths(docsRoot, fgWritePaths), ", "),
		"KnowledgebasePath":     filepath.Join(docsRoot, fgKnowledgebasePath),
		"DBPath":                filepath.Join(docsRoot, fgDBPath),
		"WorkflowRoot":          filepath.Join(docsRoot, baseWorkspacePath),
		"LearningsPath":         filepath.Join(docsRoot, fgGlobalLearningsPath),
	}

	// Build previous steps summary (includes descriptions, output files, and execution results like human_input responses)
	previousStepsSummary := hcpo.buildPreviousStepsSummary(allSteps, stepIndex, previousContextFiles, previousExecutionResults)

	templateVars["PreviousStepsSummary"] = previousStepsSummary
	if execCtx != nil && execCtx.WorkshopHumanInput != "" {
		templateVars["WorkshopHumanInput"] = execCtx.WorkshopHumanInput
		hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Injecting human_input into todo_task step %q prompt (%d chars)", step.GetID(), len(execCtx.WorkshopHumanInput)))
	} else {
		templateVars["WorkshopHumanInput"] = ""
	}

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

	// Surface the pre-validation schema in the prompt so the orchestrator knows
	// which output files must exist on the first attempt — otherwise it only
	// learns the requirements via ValidationFeedback after a failed attempt.
	if validationSchema := step.GetValidationSchema(); validationSchema != nil {
		if schemaJSON, err := json.MarshalIndent(validationSchema, "", "  "); err == nil {
			templateVars["ValidationSchema"] = string(schemaJSON)
		} else {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal validation schema for todo task step %d: %v", stepIndex+1, err))
		}
	}

	return templateVars
}

// selectTodoTaskOrchestratorLLM selects the LLM config for todo task orchestrator.
//
// Priority:
//  1. step config ExecutionLLM — explicit override always wins (same knob used for
//     regular step execution; sub-agents spawned by this orchestrator inherit it too)
//  2. step config TodoTaskOrchestratorTier — explicit tier override in tiered mode
//  3. Tier 1 (High) — default for orchestrator (returns nil if tier resolver is unavailable)
func (hcpo *StepBasedWorkflowOrchestrator) selectTodoTaskOrchestratorLLM(
	ctx context.Context,
	stepConfig *AgentConfigs,
	stepID string,
	stepPath string,
) *orchestrator.LLMConfig {
	// 1. Step config ExecutionLLM always takes highest precedence — one LLM knob
	// covers regular step execution, todo-task orchestrator role, and sub-agents.
	if stepConfig != nil && stepConfig.ExecutionLLM != nil &&
		stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 [STEP OVERRIDE] Todo task orchestrator using step-config ExecutionLLM: %s/%s",
			stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: stepConfig.ExecutionLLM.Provider,
				ModelID:  stepConfig.ExecutionLLM.ModelID,
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
) (*TodoTaskResponse, []llmtypes.MessageContent, string, *SubAgentExecutionContext, orchestratoragents.OrchestratorAgent, error) {
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
		return nil, nil, "", nil, nil, fmt.Errorf("no valid LLM configuration found for todo task orchestrator")
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
		return nil, nil, "", nil, nil, fmt.Errorf("failed to create todo task orchestrator agent: %w", err)
	}

	// Sync template vars with actual agent config — the factory may have overridden
	// code execution mode (for CLI providers) or tool search mode after template vars were built.
	if agent.GetConfig() != nil {
		if agentConfigUseCodeExecutionMode(agent.GetConfig()) {
			templateVars["IsCodeExecutionMode"] = "true"
		}
		// Show tools reference section for CLI providers ONLY when NOT in code execution mode.
		// In code exec mode, the {{TOOL_STRUCTURE}} JSON already provides the authoritative tool index.
		provider := agentConfigProvider(agent.GetConfig())
		if isCliProviderForPrompt(provider) && !agentConfigUseCodeExecutionMode(agent.GetConfig()) {
			templateVars["ShowToolsSection"] = "true"
		}
	}

	// Pre-save prompts.json so get_step_prompts works during execution (not just after)
	if todoAgent, ok := agent.(*WorkflowTodoTaskOrchestratorAgent); ok {
		preSystemPrompt := todoAgent.todoTaskOrchestratorSystemPromptProcessor(templateVars)
		preUserMessage := todoAgent.todoTaskOrchestratorUserMessageProcessor(templateVars, conversationHistory)
		hcpo.preSavePromptsJSON(stepIndex, step.GetID(), stepPath, "todo_task_orchestrator", preSystemPrompt, preUserMessage, executionLLM, "todo-task-prompts.json")
	}

	// Execute — single-shot, the agent delegates to sub-agents and runs to completion
	_, updatedHistory, err := agent.Execute(ctx, templateVars, conversationHistory)
	if err != nil {
		return nil, nil, "", subAgentExecCtx, agent, fmt.Errorf("todo task orchestrator execution failed: %w", err)
	}

	return nil, updatedHistory, executionLLM, subAgentExecCtx, agent, nil
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

	useCodeExecutionMode := boolPtr(false)

	// Create a synthetic RegularPlanStep for the generic execution
	// Use the orchestrator's instructions and success criteria
	genericStepID := fmt.Sprintf("generic-%s-%s", stepPath, response.TodoIDToExecute)
	genericStep := &RegularPlanStep{
		Type: StepTypeRegular,
		CommonStepFields: CommonStepFields{
			ID:            genericStepID,
			Title:         taskTitle,
			Description:   response.InstructionsToSubAgent,
			ContextOutput: FlexibleContextOutput(fmt.Sprintf("%s-result.json", response.TodoIDToExecute)),
		},
		HasLoop: false,
		// Generic agents do not contribute learnings and skip pre-validation, but
		// they still inherit execution-mode settings from the parent step.
		AgentConfigs: func() *AgentConfigs {
			// Inherit parallel tool execution setting from parent step
			var disableParallelToolExec *bool
			if parentConfig := getAgentConfigs(step); parentConfig != nil {
				disableParallelToolExec = parentConfig.DisableParallelToolExecution
			}
			return &AgentConfigs{
				// Learning is off by default (LearningObjective empty) — generic agents
				// don't generate persistent learnings by design.
				UseCodeExecutionMode:         useCodeExecutionMode,
				DisableParallelToolExecution: disableParallelToolExec, // inherit from parent
			}
		}(),
	}

	// Build generic step path
	genericStepPath := fmt.Sprintf("%s-generic-%s", stepPath, response.TodoIDToExecute)

	// Build execution context
	var capturedHistory []llmtypes.MessageContent
	execCtx := &ExecutionContext{
		SkipHumanInput:             true, // Generic agents don't request human feedback
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
	parentExecutionID, _ := ctx.Value(virtualtools.BackgroundAgentIDKey).(string)
	if hcpo.subAgentNotifier != nil {
		hcpo.subAgentNotifier.OnSubAgentStart(WorkshopExecutionStart{
			ID:                agentID,
			ParentExecutionID: parentExecutionID,
			Name:              agentName,
			Kind:              "workflow_generic_agent",
			Cancel:            subAgentCancel,
		})
	}
	subAgentCtx = context.WithValue(subAgentCtx, virtualtools.BackgroundAgentIDKey, agentID)
	subAgentCtx = context.WithValue(subAgentCtx, events.ParentExecutionIDKey, agentID)

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
		applyDelegationOverridesToCommonFields(&stepCopy.CommonStepFields, instructions)
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
	parentExecutionID, _ := ctx.Value(virtualtools.BackgroundAgentIDKey).(string)
	if hcpo.subAgentNotifier != nil {
		hcpo.subAgentNotifier.OnSubAgentStart(WorkshopExecutionStart{
			ID:                subAgentNotifID,
			ParentExecutionID: parentExecutionID,
			Name:              subAgentNotifName,
			Kind:              "workflow_sub_agent",
			Cancel:            subAgentCancel,
		})
	}
	subAgentCtx = context.WithValue(subAgentCtx, virtualtools.BackgroundAgentIDKey, subAgentNotifID)
	subAgentCtx = context.WithValue(subAgentCtx, events.ParentExecutionIDKey, subAgentNotifID)

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

// saveTodoTaskExecutionLog saves the execution log for a todo task iteration
// This allows the UI to show the full execution history (conversation, tool calls) for each iteration
func (hcpo *StepBasedWorkflowOrchestrator) saveTodoTaskExecutionLog(
	ctx context.Context,
	stepID string,
	stepPath string,
	iteration int,
	executionLLM string,
	conversationHistory []llmtypes.MessageContent,
	toolCalls []orchestrator.ToolCallEntry,
	llmCalls []orchestrator.LLMCallEntry,
	attemptStartedAt time.Time,
	attemptCompletedAt time.Time,
	attemptDuration time.Duration,
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
	if attemptCompletedAt.IsZero() {
		attemptCompletedAt = time.Now().UTC()
	}
	if attemptStartedAt.IsZero() {
		attemptStartedAt = attemptCompletedAt.Add(-attemptDuration)
	}
	toolTiming := normalizeToolTimingEntries(toolCalls, attemptStartedAt)
	llmTiming := normalizeLLMTimingEntries(llmCalls, attemptStartedAt)
	traceSpans, timingBreakdown := buildTimingTrace(stepID, stepPath, executionLLM, attemptStartedAt, attemptCompletedAt, attemptDuration, llmTiming, toolTiming)
	timingData := map[string]interface{}{
		"schema_version": 2,
		"step_id":        stepID,
		"step_path":      stepPath,
		"run_folder":     hcpo.selectedRunFolder,
		"agent": map[string]interface{}{
			"model":                         executionLLM,
			"started_at":                    formatRFC3339UTC(attemptStartedAt),
			"completed_at":                  formatRFC3339UTC(attemptCompletedAt),
			"duration_ns":                   int64(attemptDuration),
			"duration_ms":                   durationToMillis(attemptDuration),
			"llm_call_count":                llmTiming.Count,
			"llm_duration_ms":               llmTiming.TotalDurationMs,
			"llm_time_to_first_response_ms": llmTiming.TimeToFirstResponseMs,
		},
		"llm":         llmTiming,
		"tools":       toolTiming,
		"trace_spans": traceSpans,
		"breakdown":   timingBreakdown,
	}

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
		"step_path":                     stepPath,
		"attempt":                       1,
		"iteration":                     iteration,
		"model":                         executionLLM,
		"execution_result":              executionSummary,
		"message_count":                 len(conversationHistory),
		"started_at":                    formatRFC3339UTC(attemptStartedAt),
		"completed_at":                  formatRFC3339UTC(attemptCompletedAt),
		"duration_ms":                   durationToMillis(attemptDuration),
		"duration_ns":                   int64(attemptDuration),
		"llm_call_count":                llmTiming.Count,
		"llm_duration_ms":               llmTiming.TotalDurationMs,
		"llm_time_to_first_response_ms": llmTiming.TimeToFirstResponseMs,
		"tool_call_count":               toolTiming.Count,
		"tool_duration_ms":              toolTiming.TotalDurationMs,
		"tracked_union_duration_ms":     timingBreakdown.TrackedUnionDurationMs,
		"untracked_duration_ms":         timingBreakdown.UntrackedDurationMs,
		"total_input_tokens":            timingBreakdown.TotalInputTokens,
		"total_output_tokens":           timingBreakdown.TotalOutputTokens,
		"total_tokens":                  timingBreakdown.TotalTokens,
		"tool_args_bytes":               timingBreakdown.ToolArgsBytes,
		"tool_result_bytes":             timingBreakdown.ToolResultBytes,
		"timing":                        timingData,
		"timestamp":                     attemptCompletedAt.Format(time.RFC3339),
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

	// Save the full conversation history and tool calls,
	// so the execution popup can open it via the inferred conversation_path.
	conversationLog := map[string]interface{}{
		"step_path":            stepPath,
		"retry_attempt":        1,
		"loop_iteration":       iteration,
		"conversation_history": conversationHistory,
		"llm_calls":            llmCalls,
		"tool_calls":           toolCalls,
		"llm_call_count":       llmTiming.Count,
		"tool_call_count":      len(toolCalls),
		"timing":               timingData,
		"timestamp":            attemptCompletedAt.Format(time.RFC3339),
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

	timingPath := strings.TrimSuffix(filePath, ".json") + "-timing.json"
	timingJSON, err := json.MarshalIndent(timingData, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal todo task timing log: %v", err))
		return
	}
	if err := hcpo.WriteWorkspaceFile(saveCtx, timingPath, string(timingJSON)); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save todo task timing log to %s: %v", timingPath, err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("⏱️ Todo task timing log saved to: %s", timingPath))
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

	// Persist pre-validation results to disk for harden_workflow analysis
	if hcpo.selectedRunFolder != "" {
		preValLogPath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		SavePreValidationLog(ctx, hcpo.BaseOrchestrator, preValLogPath, step.GetID(), stepPath, workspaceResults, validationSchema)
	}

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
