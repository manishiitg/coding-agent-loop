package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// executeConditionalStep executes a conditional step by evaluating the condition and executing the chosen branch
// depth: current nesting depth (0 = main plan, 1 = first level conditional, 2 = second level conditional)
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeConditionalStep(
	ctx context.Context,
	step TodoStep,
	stepIndex int,
	depth int,
	progress *StepProgress,
	previousExecutionResults []string, // Execution results from previous steps (in-memory, not file paths)
	iteration int, // Current iteration number
	execCtx *ExecutionContext, // Execution context with flags
	allSteps []TodoStep, // All steps in the plan (for previous steps summary in branch steps)
) error {
	const maxDepth = 2
	if depth > maxDepth {
		return fmt.Errorf(fmt.Sprintf("nesting depth %d exceeds maximum allowed depth of %d", depth, maxDepth), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔀 Executing conditional step %d (depth %d): %s", stepIndex+1, depth, step.Title))
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Conditional step %d has %d if_true_steps and %d if_false_steps", stepIndex+1, len(step.IfTrueSteps), len(step.IfFalseSteps)))

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

	if progress.BranchSteps == nil {
		progress.BranchSteps = make(map[int]BranchStepProgress)
	}

	// Always evaluate the condition
	hcpo.GetLogger().Info(fmt.Sprintf("🤔 Evaluating condition for step %d (depth %d): %s", stepIndex+1, depth, step.ConditionQuestion))

	// Build conditionContext - ONLY the last previous execution agent output (from in-memory results)
	// Learnings are passed separately as learningHistory variable
	contextBuilder := strings.Builder{}

	// Add context from the LAST previous execution agent output ONLY
	// Use only the most recent step's execution result (in-memory, not file paths)
	if len(previousExecutionResults) > 0 {
		// Get the last (most recent) execution result
		lastExecutionResult := ""
		for i := len(previousExecutionResults) - 1; i >= 0; i-- {
			if previousExecutionResults[i] != "" {
				lastExecutionResult = previousExecutionResults[i]
				break
			}
		}

		if lastExecutionResult != "" {
			contextBuilder.WriteString("Previous Step Execution Output:\n")
			contextBuilder.WriteString(fmt.Sprintf("%s\n", lastExecutionResult))
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Included last previous step execution output (length: %d chars)", len(lastExecutionResult)))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No previous step execution outputs available for conditional evaluation"))
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No previous step execution outputs available for conditional evaluation"))
	}

	conditionContext := contextBuilder.String()
	hcpo.GetLogger().Info(fmt.Sprintf("📋 Condition context length: %d characters (only last execution output)", len(conditionContext)))

	// Read learnings separately (passed as separate learningHistory variable, not in conditionContext)
	stepNumber := stepIndex + 1 // Convert to 1-based
	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepLearningsPath := fmt.Sprintf("%s/learnings/step-%d", baseWorkspacePath, stepNumber)

	// Check if learnings folder exists and has files
	learningsFolderEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepNumber)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check if step learnings folder is empty for step %d: %v, proceeding without learnings", stepNumber, err))
	}

	learningHistory := ""
	if !learningsFolderEmpty {
		// Read learning files from step folder
		learningFiles, err := hcpo.readStepLearningFiles(ctx, stepLearningsPath)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read learning files from %s: %v - will proceed without learnings", stepLearningsPath, err))
			learningHistory = ""
		} else if len(learningFiles) > 0 {
			// Format learnings for system prompt (separate from conditionContext)
			formattedLearnings, _ := hcpo.formatStepLearningFilesAsHistory(learningFiles)
			learningHistory = formattedLearnings
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Loaded %d learning file(s) for conditional agent system prompt (separate from conditionContext)", len(learningFiles)))
		} else {
			learningHistory = ""
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("📁 Step %d learnings folder is empty or does not exist: %s (proceeding without learnings)", stepNumber, stepLearningsPath))
		learningHistory = ""
	}

	// Determine code execution mode: Priority: step config > orchestrator default
	var isCodeExecutionMode bool
	if step.AgentConfigs != nil && step.AgentConfigs.UseCodeExecutionMode != nil {
		isCodeExecutionMode = *step.AgentConfigs.UseCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode for conditional evaluation: %v", isCodeExecutionMode))
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator code execution mode for conditional evaluation: %v", isCodeExecutionMode))
	}

	// Get conditional agent for this step (step-specific or default)
	// Pass stepIndex for proper factory setup
	// Note: CreateAndSetupStandardAgentWithConfig already sets the orchestrator context during agent creation
	conditionalAgent := hcpo.getConditionalAgentForStep(ctx, step, stepIndex, "conditional-step-evaluation", "conditional_evaluation")

	// Evaluate condition using ConditionalAgent
	// Note: Branch step execution agents will set their own context when created via createExecutionOnlyAgent
	conditionalResponse, err := conditionalAgent.Decide(ctx, conditionContext, step.ConditionQuestion, step.Description, stepIndex, 0, isCodeExecutionMode, learningHistory)

	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to evaluate condition for step %d: %v", stepIndex+1, err), nil)
		// Emit error event using centralized method
		hcpo.EmitOrchestratorAgentError(ctx, "conditional", "conditional-step-evaluation", fmt.Sprintf("Evaluate condition: %s", step.ConditionQuestion), err.Error(), stepIndex, 0)
		return fmt.Errorf(fmt.Sprintf("failed to evaluate condition: %w", err), nil)
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

	// Store conditional evaluation result to logs (if enabled)
	// Note: conditionalStepPath is already defined earlier in the function
	if hcpo.saveValidationResponses {
		// Determine validation workspace path (same logic as validation agent)
		var validationWorkspacePath string
		if hcpo.selectedRunFolder != "" {
			validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		} else {
			validationWorkspacePath = hcpo.GetWorkspacePath()
		}

		// Get validation folder path based on conditionalStepPath (step-{X})
		validationFolderPath := getValidationFolderPath(validationWorkspacePath, conditionalStepPath)

		// Save conditional evaluation result
		conditionalEvaluationFilePath := fmt.Sprintf("%s/conditional-evaluation.json", validationFolderPath)
		conditionalEvaluationResponse := map[string]interface{}{
			"step_index":            stepIndex + 1,
			"step_path":             conditionalStepPath,
			"conditional_step_id":   step.ID,
			"condition_question":    step.ConditionQuestion,
			"condition_result":      conditionResult,
			"condition_reason":      conditionReason,
			"branch_executed":       branchExecuted,
			"if_true_next_step_id":  step.IfTrueNextStepID,
			"if_false_next_step_id": step.IfFalseNextStepID,
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
	}

	// Always initialize fresh branch progress (never read from stored progress)
	progress.BranchSteps[stepIndex] = BranchStepProgress{
		BranchExecuted: branchExecuted,
		CompletedSteps: []string{},
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📝 Initialized branch progress for step %d: branch=%s", stepIndex+1, branchExecuted))

	// Log decision details
	hcpo.GetLogger().Info(fmt.Sprintf("📊 Conditional decision details - Step: %s, Question: %s, Result: %t, Depth: %d",
		step.Title, step.ConditionQuestion, conditionResult, depth))

	// Determine which branch to execute
	var branchSteps []TodoStep
	if conditionResult {
		branchSteps = step.IfTrueSteps
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing TRUE branch with %d steps", len(branchSteps)))
		if len(branchSteps) == 0 {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Conditional step %d evaluated to TRUE but has no if_true_steps defined in plan. Step will complete immediately without executing any branch steps.", stepIndex+1))
		}
	} else {
		branchSteps = step.IfFalseSteps
		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing FALSE branch with %d steps", len(branchSteps)))
		if len(branchSteps) == 0 {
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
			if prevStep.ContextOutput != "" {
				// Resolve variables in context output
				resolvedOutput := ResolveVariables(prevStep.ContextOutput, hcpo.variableValues)
				branchContextFiles = append(branchContextFiles, resolvedOutput)
			}
		}
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Added %d previous regular step context files to branch context", len(branchContextFiles)))
	}

	// Add conditional step's context output to branch context files if it exists
	if step.ContextOutput != "" {
		resolvedOutput := ResolveVariables(step.ContextOutput, hcpo.variableValues)
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
	for branchIdx, branchStep := range branchSteps {
		// Skip if resuming and this branch step is already completed
		if branchIdx < resumeFromBranchStep {
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping branch step %d/%d (already completed): %s", branchIdx+1, len(branchSteps), branchStep.Title))
			continue
		}

		// Generate branch step path
		branchStepPath := fmt.Sprintf("step-%d-%s-%d", stepIndex+1, branchExecutedStr, branchIdx)
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [BRANCH STEP] Generated branchStepPath: %s (stepIndex=%d, branchExecutedStr=%s, branchIdx=%d)", branchStepPath, stepIndex+1, branchExecutedStr, branchIdx))

		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing branch step %d/%d (depth %d): %s", branchIdx+1, len(branchSteps), depth+1, branchStep.Title))

		// Check if branch step is conditional (nested conditional)
		if branchStep.HasCondition {
			// Recursively execute nested conditional step - pass execution results (not file paths)
			hcpo.GetLogger().Info(fmt.Sprintf("🔀 Executing nested conditional step in branch: %s (depth %d)", branchStep.Title, depth+1))
			if err := hcpo.executeConditionalStep(ctx, branchStep, stepIndex, depth+1, progress, branchExecutionResults, iteration, execCtx, allSteps); err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute nested conditional step '%s' at depth %d: %v", branchStep.Title, depth+1, err), nil)
				return fmt.Errorf(fmt.Sprintf("failed to execute nested conditional step '%s': %w", branchStep.Title, err), nil)
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Completed nested conditional step: %s", branchStep.Title))
		} else {
			// Execute regular branch step using extracted execution logic
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 [BRANCH STEP] Calling executeSingleStep with branchStepPath: %s (isBranchStep=true)", branchStepPath))

			// Build previous branch steps context files (previous steps in the same branch)
			previousBranchContextFiles := make([]string, 0)
			// Include all previous branch steps' context outputs
			for prevBranchIdx := 0; prevBranchIdx < branchIdx; prevBranchIdx++ {
				if prevBranchIdx < len(branchSteps) && branchSteps[prevBranchIdx].ContextOutput != "" {
					resolvedOutput := ResolveVariables(branchSteps[prevBranchIdx].ContextOutput, hcpo.variableValues)
					previousBranchContextFiles = append(previousBranchContextFiles, resolvedOutput)
				}
			}
			// Combine: previous regular steps + previous branch steps
			combinedBranchContextFiles := make([]string, len(branchContextFiles))
			copy(combinedBranchContextFiles, branchContextFiles)
			combinedBranchContextFiles = append(combinedBranchContextFiles, previousBranchContextFiles...)

			branchExecutionResult, updatedBranchContextFiles, err := hcpo.executeSingleStep(
				ctx,
				branchStep,
				stepIndex, // Use parent step index for now
				branchStepPath,
				len(branchSteps), // Total steps in branch
				iteration,
				combinedBranchContextFiles, // Include previous regular steps + previous branch steps
				progress,
				true, // isBranchStep = true
				execCtx,
				allSteps, // Pass allSteps so branch steps can see previous regular steps
				false,    // isDecisionInnerStep = false (branch step)
				nil,      // decisionContext = nil (branch steps are not routed from decision steps)
				"",       // decisionEvaluationQuestion - empty for branch steps
				false,    // isSubAgent = false (branch step, not a sub-agent)
			)
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute branch step '%s': %v", branchStep.Title, err), nil)
				return fmt.Errorf(fmt.Sprintf("failed to execute branch step '%s': %w", branchStep.Title, err), nil)
			}

			// Track branch step completion
			branchProgress := progress.BranchSteps[stepIndex]
			branchProgress.CompletedSteps = append(branchProgress.CompletedSteps, branchStepPath)
			progress.BranchSteps[stepIndex] = branchProgress
			// Save progress after each branch step completion
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save branch step progress: %w", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved branch step progress: %s completed", branchStepPath))
			}

			// Update context files with branch step's output (for executeSingleStep context dependencies)
			branchContextFiles = updatedBranchContextFiles

			// Track execution result for use by subsequent conditional steps
			branchExecutionResults = append(branchExecutionResults, branchExecutionResult)
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Completed branch step: %s (execution result length: %d chars)", branchStep.Title, len(branchExecutionResult)))
			hcpo.GetLogger().Info(fmt.Sprintf("💾 Stored branch step execution result (will be used by subsequent conditional steps)"))
		}
	}

	// Verify all branch steps are completed
	branchProgress := progress.BranchSteps[stepIndex]
	expectedBranchSteps := len(branchSteps)
	completedBranchSteps := len(branchProgress.CompletedSteps)
	if completedBranchSteps < expectedBranchSteps {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Conditional step %d: only %d/%d branch steps completed", stepIndex+1, completedBranchSteps, expectedBranchSteps))
		// Don't mark as completed - will resume from incomplete branch steps
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ All %d branch steps completed for conditional step %d", expectedBranchSteps, stepIndex+1))
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Completed conditional step %d: executed %s branch", stepIndex+1, map[bool]string{true: "TRUE", false: "FALSE"}[conditionResult]))

	// Emit step_finished event for conditional step
	// Use regular step path for conditional step (not -conditional suffix)
	conditionalStepPath = fmt.Sprintf("step-%d", stepIndex+1)
	hcpo.emitStepFinishedEvent(ctx, step, stepIndex, conditionalStepPath, false)

	return nil
}
