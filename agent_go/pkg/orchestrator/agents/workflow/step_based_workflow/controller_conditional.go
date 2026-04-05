package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// executeConditionalStep executes a conditional step by evaluating the condition and executing the chosen branch
// depth: current nesting depth (0 = main plan, 1 = first level conditional, 2 = second level conditional)
//
// NOTE: This function works with PlanStepInterface (specifically ConditionalPlanStep).
// The step type is already validated by the main execution loop before calling this function.
func (hcpo *StepBasedWorkflowOrchestrator) executeConditionalStep(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	depth int,
	progress *StepProgress,
	previousExecutionResults []string, // Execution results from previous steps (in-memory, not file paths)
	iteration int, // Current iteration number
	execCtx *ExecutionContext, // Execution context with flags
	allSteps []PlanStepInterface, // All steps in the plan (for previous steps summary in branch steps)
) error {
	// Steps are already PlanStepInterface - no conversion needed
	conditionalStep, ok := step.(*ConditionalPlanStep)
	if !ok {
		return fmt.Errorf("step is not a ConditionalPlanStep")
	}

	const maxDepth = 2
	if depth > maxDepth {
		return fmt.Errorf("nesting depth %d exceeds maximum allowed depth of %d", depth, maxDepth)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔀 Executing conditional step %d (depth %d): %s", stepIndex+1, depth, step.GetTitle()))
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Conditional step %d has %d if_true_steps and %d if_false_steps", stepIndex+1, len(conditionalStep.IfTrueSteps), len(conditionalStep.IfFalseSteps)))

	// Emit step_started event for conditional step
	// Use regular step path for conditional step (not -conditional suffix)
	conditionalStepPath := fmt.Sprintf("step-%d", stepIndex+1)
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, conditionalStepPath, false)

	// Check if we're resuming from a specific branch step (from ExecutionContext)
	var resumeFromBranchStep int = -1 // -1 means not resuming from specific branch step

	if execCtx.ResumeBranchStep != nil && execCtx.ResumeBranchStep.ParentStepIndex == stepIndex {
		// We're resuming from a branch step in this conditional step
		resumeFromBranchStep = execCtx.ResumeBranchStep.BranchStepIndex
		hcpo.GetLogger().Info(fmt.Sprintf("🔀 Resuming from specific branch step: parent=%d, branch=%s, step=%d",
			stepIndex+1, execCtx.ResumeBranchStep.BranchType, resumeFromBranchStep+1))
	}

	// Always evaluate the condition - never skip based on existing branch progress
	var conditionResult bool
	var conditionReason string

	// Always evaluate the condition
	hcpo.GetLogger().Info(fmt.Sprintf("🤔 Evaluating condition for step %d (depth %d): %s", stepIndex+1, depth, conditionalStep.ConditionQuestion))

	// Build conditionContext - ONLY the last previous execution agent output (from in-memory results)
	// Learnings are passed separately as learningHistory variable
	contextBuilder := strings.Builder{}

	// Add context from the LAST previous execution agent output ONLY
	// Use only the most recent step's execution result (in-memory, not file paths)
	if len(previousExecutionResults) > 0 {
		// Get the last (most recent) execution result and its step index
		lastExecutionResult := ""
		lastResultStepIndex := -1
		for i := len(previousExecutionResults) - 1; i >= 0; i-- {
			if previousExecutionResults[i] != "" {
				lastExecutionResult = previousExecutionResults[i]
				lastResultStepIndex = i
				break
			}
		}

		if lastExecutionResult != "" {
			// Check if the source step was a human_input step — human feedback is critical
			isHumanInput := lastResultStepIndex >= 0 && lastResultStepIndex < len(allSteps) && allSteps[lastResultStepIndex].StepType() == StepTypeHumanInput
			if isHumanInput {
				contextBuilder.WriteString("🚨 HUMAN FEEDBACK (CRITICAL - This takes priority over other context):\n")
			} else {
				contextBuilder.WriteString("Previous Step Execution Output:\n")
			}
			contextBuilder.WriteString(fmt.Sprintf("%s\n", lastExecutionResult))
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Included last previous step execution output (length: %d chars, isHumanInput: %v)", len(lastExecutionResult), isHumanInput))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No previous step execution outputs available for conditional evaluation"))
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No previous step execution outputs available for conditional evaluation"))
	}

	conditionContext := contextBuilder.String()
	hcpo.GetLogger().Info(fmt.Sprintf("📋 Condition context length: %d characters (only last execution output)", len(conditionContext)))

	// Read learnings separately (passed as separate learningHistory variable, not in conditionContext)
	agentConfigs := getAgentConfigs(step)
	var learningHistory string
	learningHistory, _ = hcpo.LoadGlobalLearningHistory(ctx)

	// Determine code execution mode: step config > workflow/preset default
	// Note: Provider-based auto-enable (claude-code/gemini-cli) is handled in createConditionalAgent.
	var isCodeExecutionMode bool
	if agentConfigs != nil && agentConfigs.UseCodeExecutionMode != nil {
		isCodeExecutionMode = *agentConfigs.UseCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode for conditional evaluation: %v", isCodeExecutionMode))
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using workflow/preset code execution mode for conditional evaluation: %v", isCodeExecutionMode))
	}

	// Ensure step execution folder exists before creating conditional agent (agent needs to write to this folder)
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, step.GetID(), conditionalStepPath)
	if err := hcpo.ensureStepExecutionFolderExists(ctx, stepExecutionPath); err != nil {
		// Non-blocking: log warning but continue execution (folder will be created when files are written)
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure conditional step execution folder exists: %v (continuing - folder will be created when files are written)", err))
	}

	// Get conditional agent for this step (step-specific or default)
	// Pass stepIndex for proper factory setup
	// Note: CreateAndSetupStandardAgentWithConfig already sets the orchestrator context during agent creation
	conditionalAgent := hcpo.getConditionalAgentForStep(ctx, step, stepIndex, "conditional-step-evaluation", "conditional_evaluation")

	// Evaluate condition using ConditionalAgent
	// Note: Branch step execution agents will set their own context when created via createExecutionOnlyAgent
	stepDescription := step.GetDescription()

	// Format variable names and values (same format as execution agent)
	var variableNames, variableValues string
	if hcpo.variablesManifest != nil {
		variableNames = FormatVariableNames(hcpo.variablesManifest)
		variableValues = FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues)
	}

	// Pre-save prompts.json so get_step_prompts works during execution
	{
		tv := map[string]string{
			"ConditionContext": conditionContext,
			"Question":         conditionalStep.ConditionQuestion,
			"Description":      stepDescription,
			"LearningHistory":  learningHistory,
			"VariableNames":    variableNames,
			"VariableValues":   variableValues,
		}
		sp := conditionalAgent.conditionalSystemPromptProcessor(tv, isCodeExecutionMode)
		um := conditionalAgent.conditionalUserMessageProcessor(tv)
		var model string
		if conditionalAgent.GetConfig() != nil && conditionalAgent.GetConfig().LLMConfig.Primary.ModelID != "" {
			model = fmt.Sprintf("%s/%s", conditionalAgent.GetConfig().LLMConfig.Primary.Provider, conditionalAgent.GetConfig().LLMConfig.Primary.ModelID)
		}
		hcpo.preSavePromptsJSON(stepIndex, step.GetID(), conditionalStepPath, "conditional_evaluation", sp, um, model, "conditional-prompts.json")
	}

	conditionalResponse, err := conditionalAgent.Decide(ctx, conditionContext, conditionalStep.ConditionQuestion, stepDescription, stepIndex, 0, isCodeExecutionMode, learningHistory, variableNames, variableValues)

	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to evaluate condition for step %d: %v", stepIndex+1, err), nil)
		// Emit error event using centralized method
		hcpo.EmitOrchestratorAgentError(ctx, "conditional", "conditional-step-evaluation", fmt.Sprintf("Evaluate condition: %s", conditionalStep.ConditionQuestion), err.Error(), stepIndex, 0)
		return fmt.Errorf("failed to evaluate condition: %w", err)
	}

	// Store result
	conditionResult = conditionalResponse.Result
	conditionReason = conditionalResponse.Reason

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Condition evaluated for step %d: result=%t, reason=%s", stepIndex+1, conditionResult, conditionReason))

	// Determine which branch was executed
	branchExecuted := "if_false"
	if conditionResult {
		branchExecuted = "if_true"
	}

	// Store conditional evaluation result to logs (always enabled)
	// Note: conditionalStepPath is already defined earlier in the function
	// Determine validation workspace path (same logic as validation agent)
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}

	// Get validation folder path based on conditionalStepPath (step-{X})
	validationFolderPath := getValidationFolderPath(validationWorkspacePath, step.GetID(), conditionalStepPath)

	// Save conditional evaluation result
	conditionalEvaluationFilePath := fmt.Sprintf("%s/conditional-evaluation.json", validationFolderPath)
	conditionalEvaluationResponse := map[string]interface{}{
		"step_index":            stepIndex + 1,
		"step_path":             conditionalStepPath,
		"conditional_step_id":   step.GetID(),
		"condition_question":    conditionalStep.ConditionQuestion,
		"condition_result":      conditionResult,
		"condition_reason":      conditionReason,
		"branch_executed":       branchExecuted,
		"if_true_next_step_id":  conditionalStep.IfTrueNextStepID,
		"if_false_next_step_id": conditionalStep.IfFalseNextStepID,
		"depth":                 depth,
		"timestamp":             time.Now().Format(time.RFC3339),
	}

	// Marshal and save conditional evaluation result
	conditionalJSON, err := json.MarshalIndent(conditionalEvaluationResponse, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal conditional evaluation response to JSON: %v", err))
	} else {
		if err := hcpo.WriteWorkspaceFile(ctx, conditionalEvaluationFilePath, string(conditionalJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write conditional evaluation response to %s: %v", conditionalEvaluationFilePath, err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("💾 Conditional evaluation response saved to: %s", conditionalEvaluationFilePath))
		}
	}

	// Always initialize fresh branch progress (never read from stored progress)
	// Initialize BranchSteps map if it's nil
	if progress.BranchSteps == nil {
		progress.BranchSteps = make(map[int]BranchStepProgress)
	}
	progress.BranchSteps[stepIndex] = BranchStepProgress{
		BranchExecuted: branchExecuted,
		CompletedSteps: []string{},
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📝 Initialized branch progress for step %d: branch=%s", stepIndex+1, branchExecuted))

	// Log decision details
	hcpo.GetLogger().Info(fmt.Sprintf("📊 Conditional decision details - Step: %s, Question: %s, Result: %t, Depth: %d",
		step.GetTitle(), conditionalStep.ConditionQuestion, conditionResult, depth))

	// Determine which branch to execute
	// Get branch steps from original PlanStepInterface to maintain type safety
	var branchStepsPlan []PlanStepInterface
	if conditionResult {
		branchStepsPlan = conditionalStep.IfTrueSteps
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing TRUE branch with %d steps", len(branchStepsPlan)))
		if len(branchStepsPlan) == 0 {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Conditional step %d evaluated to TRUE but has no if_true_steps defined in plan. Step will complete immediately without executing any branch steps.", stepIndex+1))
		}
	} else {
		branchStepsPlan = conditionalStep.IfFalseSteps
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing FALSE branch with %d steps", len(branchStepsPlan)))
		if len(branchStepsPlan) == 0 {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Conditional step %d evaluated to FALSE but has no if_false_steps defined in plan. Step will complete immediately without executing any branch steps.", stepIndex+1))
		}
	}

	// Track context files for branch steps (still needed for executeSingleStep context dependencies)
	// Note: executeSingleStep still uses file paths for context dependencies, but conditional evaluation uses execution results
	branchContextFiles := make([]string, 0)

	// Build branchContextFiles to include:
	// 1. Previous regular steps (before this conditional step) - from allSteps
	// 2. Conditional step's context output (if it exists)
	if allSteps != nil {
		// Add context outputs from all previous regular steps (before this conditional step)
		for prevIdx := 0; prevIdx < stepIndex && prevIdx < len(allSteps); prevIdx++ {
			prevStep := allSteps[prevIdx]
			contextOutput := prevStep.GetContextOutput()
			if contextOutput.String() != "" {
				// Resolve variables in context output
				resolvedOutput := ResolveVariables(contextOutput.String(), hcpo.variableValues)
				branchContextFiles = append(branchContextFiles, resolvedOutput)
			}
		}
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Added %d previous regular step context files to branch context", len(branchContextFiles)))
	}

	// Add conditional step's context output to branch context files if it exists
	contextOutput := step.GetContextOutput()
	if contextOutput.String() != "" {
		resolvedOutput := ResolveVariables(contextOutput.String(), hcpo.variableValues)
		branchContextFiles = append(branchContextFiles, resolvedOutput)
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Added conditional step context output to branch context: %s", resolvedOutput))
	}

	// Track execution results for branch steps (for conditional evaluation)
	// Start with previous execution results
	branchExecutionResults := make([]string, len(previousExecutionResults))
	copy(branchExecutionResults, previousExecutionResults)

	// Get branch executed string for path generation
	branchExecutedStr := map[bool]string{true: "if-true", false: "if-false"}[conditionResult]

	// Find first branch step for resuming (only from explicit ExecutionContext, never from stored progress)
	if resumeFromBranchStep < 0 {
		resumeFromBranchStep = 0 // Always start from beginning if not explicitly resuming
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 Resuming from specified branch step %d (from ExecutionContext)", resumeFromBranchStep+1))
	}

	// Execute each step in the chosen branch
	for branchIdx, branchStepPlan := range branchStepsPlan {
		// Skip if resuming and this branch step is already completed
		if branchIdx < resumeFromBranchStep {
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping branch step %d/%d (already completed): %s", branchIdx+1, len(branchStepsPlan), branchStepPlan.GetTitle()))
			continue
		}

		// Generate branch step path
		branchStepPath := fmt.Sprintf("step-%d-%s-%d", stepIndex+1, branchExecutedStr, branchIdx)
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [BRANCH STEP] Generated branchStepPath: %s (stepIndex=%d, branchExecutedStr=%s, branchIdx=%d)", branchStepPath, stepIndex+1, branchExecutedStr, branchIdx))

		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing branch step %d/%d (depth %d): %s", branchIdx+1, len(branchStepsPlan), depth+1, branchStepPlan.GetTitle()))

		// Check if branch step is conditional (nested conditional)
		if isConditionalStep(branchStepPlan) {
			// Recursively execute nested conditional step - pass execution results (not file paths)
			hcpo.GetLogger().Info(fmt.Sprintf("🔀 Executing nested conditional step in branch: %s (depth %d)", branchStepPlan.GetTitle(), depth+1))
			if err := hcpo.executeConditionalStep(ctx, branchStepPlan, stepIndex, depth+1, progress, branchExecutionResults, iteration, execCtx, allSteps); err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute nested conditional step '%s' at depth %d: %v", branchStepPlan.GetTitle(), depth+1, err), nil)
				return fmt.Errorf("failed to execute nested conditional step '%s': %w", branchStepPlan.GetTitle(), err)
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Completed nested conditional step: %s", branchStepPlan.GetTitle()))
		} else {
			// Execute regular branch step using extracted execution logic
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 [BRANCH STEP] Calling executeSingleStep with branchStepPath: %s (isBranchStep=true)", branchStepPath))

			// Build previous branch steps context files (previous steps in the same branch)
			previousBranchContextFiles := make([]string, 0)
			// Include all previous branch steps' context outputs
			for prevBranchIdx := 0; prevBranchIdx < branchIdx; prevBranchIdx++ {
				if prevBranchIdx < len(branchStepsPlan) {
					prevBranchStep := branchStepsPlan[prevBranchIdx]
					contextOutput := prevBranchStep.GetContextOutput()
					if contextOutput.String() != "" {
						resolvedOutput := ResolveVariables(contextOutput.String(), hcpo.variableValues)
						previousBranchContextFiles = append(previousBranchContextFiles, resolvedOutput)
					}
				}
			}
			// Combine: previous regular steps + previous branch steps
			combinedBranchContextFiles := make([]string, len(branchContextFiles))
			copy(combinedBranchContextFiles, branchContextFiles)
			combinedBranchContextFiles = append(combinedBranchContextFiles, previousBranchContextFiles...)

			// Match branch step's own step config from step_config.json
			// Priority: branch step's own config > parent conditional step's config > preset default
			if err := ApplyStepConfigFromFile(ctx, branchStepPlan, hcpo); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load step config for branch step '%s' (ID: %s): %v", branchStepPlan.GetTitle(), branchStepPlan.GetID(), err))
			}

			// Check if branch step config was applied, if not inherit from parent conditional step
			branchStepConfig := getAgentConfigs(branchStepPlan)
			if branchStepConfig == nil || branchStepConfig.UseCodeExecutionMode == nil {
				// Branch step config not found or UseCodeExecutionMode not set - try to inherit from parent conditional step
				parentStepConfig := getAgentConfigs(step)
				if parentStepConfig != nil && parentStepConfig.UseCodeExecutionMode != nil {
					// Initialize branch step config if needed
					if branchStepConfig == nil {
						switch s := branchStepPlan.(type) {
						case *RegularPlanStep:
							s.AgentConfigs = &AgentConfigs{}
						case *ConditionalPlanStep:
							s.AgentConfigs = &AgentConfigs{}
						case *DecisionPlanStep:
							s.AgentConfigs = &AgentConfigs{}
						case *RoutingPlanStep:
							s.AgentConfigs = &AgentConfigs{}
						}
						branchStepConfig = getAgentConfigs(branchStepPlan)
					}
					// Inherit UseCodeExecutionMode from parent conditional step
					if branchStepConfig != nil {
						branchStepConfig.UseCodeExecutionMode = parentStepConfig.UseCodeExecutionMode
						hcpo.GetLogger().Info(fmt.Sprintf("🔧 Inherited code execution mode from parent conditional step (ID: %s) to branch step (ID: %s): %v", step.GetID(), branchStepPlan.GetID(), *parentStepConfig.UseCodeExecutionMode))
					}
				} else {
					// Try to load parent step config from file if not already loaded
					if err := ApplyStepConfigFromFile(ctx, step, hcpo); err == nil {
						parentStepConfig = getAgentConfigs(step)
						if parentStepConfig != nil && parentStepConfig.UseCodeExecutionMode != nil {
							// Initialize branch step config if needed
							if branchStepConfig == nil {
								switch s := branchStepPlan.(type) {
								case *RegularPlanStep:
									s.AgentConfigs = &AgentConfigs{}
								case *ConditionalPlanStep:
									s.AgentConfigs = &AgentConfigs{}
								case *DecisionPlanStep:
									s.AgentConfigs = &AgentConfigs{}
								case *RoutingPlanStep:
									s.AgentConfigs = &AgentConfigs{}
								}
								branchStepConfig = getAgentConfigs(branchStepPlan)
							}
							// Inherit UseCodeExecutionMode from parent conditional step
							if branchStepConfig != nil {
								branchStepConfig.UseCodeExecutionMode = parentStepConfig.UseCodeExecutionMode
								hcpo.GetLogger().Info(fmt.Sprintf("🔧 Loaded and inherited code execution mode from parent conditional step (ID: %s) to branch step (ID: %s): %v", step.GetID(), branchStepPlan.GetID(), *parentStepConfig.UseCodeExecutionMode))
							}
						}
					}
				}
			}

			branchExecutionResult, updatedBranchContextFiles, err := hcpo.executeSingleStep(
				ctx,
				branchStepPlan,
				stepIndex, // Use parent step index for now
				branchStepPath,
				len(branchStepsPlan), // Total steps in branch
				iteration,
				combinedBranchContextFiles, // Include previous regular steps + previous branch steps
				progress,
				true, // isBranchStep = true
				execCtx,
				allSteps,                 // Pass allSteps so branch steps can see previous regular steps
				false,                    // isDecisionInnerStep = false (branch step)
				nil,                      // decisionContext = nil (branch steps are not routed from decision steps)
				"",                       // decisionEvaluationQuestion - empty for branch steps
				false,                    // isSubAgent = false (branch step, not a sub-agent)
				previousExecutionResults, // Execution outputs from previous steps (for context)
				nil,                      // orchestrationRoutes - nil for branch steps (not sub-agents)
			)
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute branch step '%s': %v", branchStepPlan.GetTitle(), err), nil)
				return fmt.Errorf("failed to execute branch step '%s': %w", branchStepPlan.GetTitle(), err)
			}

			// Track branch step completion
			branchProgress := progress.BranchSteps[stepIndex]
			branchProgress.CompletedSteps = append(branchProgress.CompletedSteps, branchStepPath)
			progress.BranchSteps[stepIndex] = branchProgress
			// Save progress after each branch step completion
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save branch step progress: %v", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved branch step progress: %s completed", branchStepPath))
			}

			// Update context files with branch step's output (for executeSingleStep context dependencies)
			branchContextFiles = updatedBranchContextFiles

			// Check if branch step signaled early workflow termination
			// Branch steps can signal termination by including "WORKFLOW_END" or "END_WORKFLOW" in their output
			branchOutputUpper := strings.ToUpper(branchExecutionResult)
			if strings.Contains(branchOutputUpper, "WORKFLOW_END") || strings.Contains(branchOutputUpper, "END_WORKFLOW") {
				hcpo.GetLogger().Info(fmt.Sprintf("🏁 Branch step '%s' signaled workflow termination - ending workflow early", branchStepPlan.GetTitle()))
				// Return a special error that the caller can detect to signal termination
				return fmt.Errorf("WORKFLOW_END: branch step '%s' signaled workflow termination", branchStepPlan.GetTitle())
			}

			// Track execution result for use by subsequent conditional steps
			branchExecutionResults = append(branchExecutionResults, branchExecutionResult)
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Completed branch step: %s (execution result length: %d chars)", branchStepPlan.GetTitle(), len(branchExecutionResult)))
			hcpo.GetLogger().Info(fmt.Sprintf("💾 Stored branch step execution result (will be used by subsequent conditional steps)"))
		}
	}

	// Verify all branch steps are completed
	branchProgress := progress.BranchSteps[stepIndex]
	expectedBranchSteps := len(branchStepsPlan)
	completedBranchSteps := len(branchProgress.CompletedSteps)
	if completedBranchSteps < expectedBranchSteps {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Conditional step %d: only %d/%d branch steps completed", stepIndex+1, completedBranchSteps, expectedBranchSteps))
		// Don't mark as completed - will resume from incomplete branch steps
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ All %d branch steps completed for conditional step %d", expectedBranchSteps, stepIndex+1))
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Completed conditional step %d: executed %s branch", stepIndex+1, map[bool]string{true: "TRUE", false: "FALSE"}[conditionResult]))

	// Write step_done.json for the conditional step itself
	// executionWorkspacePath is defined earlier in the function
	stepExecutionPath = getExecutionFolderPath(executionWorkspacePath, step.GetID(), conditionalStepPath)
	stepDonePath := filepath.Join(stepExecutionPath, "step_done.json")
	stepDoneData := map[string]interface{}{
		"completed_at": time.Now().UTC().Format(time.RFC3339),
		"step_index":   stepIndex,
		"step_path":    conditionalStepPath,
		"step_id":      step.GetID(),
		"type":         "conditional",
	}
	if jsonBytes, err := json.MarshalIndent(stepDoneData, "", "  "); err == nil {
		if err := hcpo.WriteWorkspaceFile(ctx, stepDonePath, string(jsonBytes)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write step_done.json for conditional step: %v", err))
		}
	}

	// Emit step_finished event for conditional step
	// Use regular step path for conditional step (not -conditional suffix)
	conditionalStepPath = fmt.Sprintf("step-%d", stepIndex+1)
	hcpo.emitStepFinishedEvent(ctx, step, stepIndex, conditionalStepPath, false)

	return nil
}
