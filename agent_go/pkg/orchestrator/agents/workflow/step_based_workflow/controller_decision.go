package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// executeDecisionStep executes a decision step by:
// 1. Executing the decision step itself (using its Description, SuccessCriteria, etc.)
// 2. Evaluating the output using ConditionalLLM with decision_evaluation_question
// 3. Returning the decision result for routing (handled by main execution loop)
//
// Unlike conditional steps which only evaluate conditions, decision steps
// execute a step first and then evaluate the output to determine routing.
// DecisionPlanStep is flattened - execution fields are directly on the step.
//
// NOTE: This function works with PlanStepInterface (specifically DecisionPlanStep).
// The step type is already validated by the main execution loop before calling this function.
// Returns: (decisionResult bool, executionResult string, error)
func (hcpo *StepBasedWorkflowOrchestrator) executeDecisionStep(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	progress *StepProgress,
	previousContextFiles []string,
	iteration int,
	execCtx *ExecutionContext,
	allSteps []PlanStepInterface,
) (bool, string, error) {
	// Get the DecisionPlanStep for decision-specific fields
	decisionStep, ok := step.(*DecisionPlanStep)
	if !ok {
		return false, "", fmt.Errorf("step is not a DecisionPlanStep")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🎯 Executing decision step %d: %s", stepIndex+1, step.GetTitle()))

	// Validate decision step has required fields (DecisionPlanStep is now flattened)
	if decisionStep.Description == "" {
		return false, "", fmt.Errorf("decision step %d (%s) is missing required description field", stepIndex+1, step.GetTitle())
	}
	if decisionStep.SuccessCriteria == "" {
		return false, "", fmt.Errorf("decision step %d (%s) is missing required success_criteria field", stepIndex+1, step.GetTitle())
	}
	if decisionStep.DecisionEvaluationQuestion == "" {
		return false, "", fmt.Errorf("decision step %d (%s) is missing required decision_evaluation_question field", stepIndex+1, step.GetTitle())
	}
	if decisionStep.IfTrueNextStepID == "" {
		return false, "", fmt.Errorf("decision step %d (%s) is missing required if_true_next_step_id field", stepIndex+1, step.GetTitle())
	}
	if decisionStep.IfFalseNextStepID == "" {
		return false, "", fmt.Errorf("decision step %d (%s) is missing required if_false_next_step_id field", stepIndex+1, step.GetTitle())
	}

	// Emit step_started event for decision step
	decisionStepPath := fmt.Sprintf("step-%d", stepIndex+1)
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, decisionStepPath, false)

	// Calculate run number based on how many times this decision step has been evaluated
	// Count both true and false evaluations to get total execution count
	runNumber := 1 // Default to run1 for first execution
	if progress.DecisionEvaluationCounts != nil {
		trueKey := fmt.Sprintf("%s:true", step.GetID())
		falseKey := fmt.Sprintf("%s:false", step.GetID())
		trueCount := progress.DecisionEvaluationCounts[trueKey]
		falseCount := progress.DecisionEvaluationCounts[falseKey]
		totalEvaluations := trueCount + falseCount
		runNumber = totalEvaluations + 1 // Next run number
	}

	// 1. Execute the decision step (DecisionPlanStep is flattened - it has its own description, success_criteria, etc.)
	hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing decision step: %s (run %d)", step.GetTitle(), runNumber))
	// Use simple path: step-{X}-decision
	executionStepPath := fmt.Sprintf("step-%d-decision", stepIndex+1)

	// Apply step config at execution time (in case step_config.json was updated)
	_ = ApplyStepConfigFromFile(ctx, step, hcpo)
	// Ignore error - use defaults if config loading fails

	executionResult, updatedContextFiles, err := hcpo.executeSingleStep(
		ctx,
		step, // Execute the decision step itself (it has description, success_criteria, etc.)
		stepIndex,
		executionStepPath,
		1, // totalSteps = 1 for single decision step
		iteration,
		previousContextFiles,
		progress,
		false, // isBranchStep = false
		execCtx,
		allSteps,                                // Use []PlanStepInterface
		true,                                    // isDecisionInnerStep = true (skip final human feedback on success)
		nil,                                     // decisionContext = nil (this is the decision step execution, not a routed step)
		decisionStep.DecisionEvaluationQuestion, // decisionEvaluationQuestion - pass to execution agent for output formatting
		false,                                   // isSubAgent = false (decision step, not a sub-agent)
		[]string{},                              // previousExecutionResults - empty for decision steps (they don't need previous execution outputs)
		nil,                                     // orchestrationRoutes - nil for decision steps (not sub-agents)
	)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute decision step '%s': %v", step.GetTitle(), err), nil)
		return false, "", fmt.Errorf("failed to execute decision step '%s': %w", step.GetTitle(), err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Decision step execution completed. Output length: %d chars", len(executionResult)))

	// Check if step signaled early workflow termination
	// Steps can signal termination by including "WORKFLOW_END" or "END_WORKFLOW" in their output
	executionResultUpper := strings.ToUpper(executionResult)
	if strings.Contains(executionResultUpper, "WORKFLOW_END") || strings.Contains(executionResultUpper, "END_WORKFLOW") {
		hcpo.GetLogger().Info(fmt.Sprintf("🏁 Decision step '%s' signaled workflow termination - ending workflow early", step.GetTitle()))
		// Return a special error that the caller can detect to signal termination
		return false, executionResult, fmt.Errorf("WORKFLOW_END: decision step '%s' signaled workflow termination", step.GetTitle())
	}

	// Store decision step execution result to logs (always enabled)
	// Determine validation workspace path (same logic as validation agent)
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}

	// Get execution logs folder path based on executionStepPath (step-{X}-decision)
	executionLogsFolderPath := getExecutionFolderPathForLogs(validationWorkspacePath, executionStepPath)

	// Save decision step execution result
	executionResultFilePath := fmt.Sprintf("%s/decision-execution.json", executionLogsFolderPath)
	executionResponse := map[string]interface{}{
		"step_index":       stepIndex + 1,
		"step_path":        executionStepPath,
		"decision_step_id": step.GetID(),
		"execution_result": executionResult,
		"timestamp":        time.Now().Format(time.RFC3339),
	}

	// Marshal and save execution result
	executionJSON, err := json.MarshalIndent(executionResponse, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal decision step execution response to JSON: %v", err))
	} else {
		if err := hcpo.WriteWorkspaceFile(ctx, executionResultFilePath, string(executionJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write decision step execution response to %s: %v", executionResultFilePath, err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("💾 Decision step execution response saved to: %s", executionResultFilePath))
		}
	}

	// 2. Evaluate the execution output using the ConditionalAgent (full agent with workspace tools)
	hcpo.GetLogger().Info(fmt.Sprintf("🤔 Evaluating decision step output with question: %s", decisionStep.DecisionEvaluationQuestion))

	// Read learnings for the decision step (learnings are stored under the step's ID)
	learningHistory, _ := hcpo.LoadStepLearningHistory(ctx, step.GetID(), stepIndex, decisionStepPath, "decision")

	// Code execution mode is determined by createConditionalAgent's 3-rule priority:
	// Rule 1: CLI providers (claude-code, gemini-cli) always use code execution
	// Rule 2: Step config if explicitly set by user
	// Rule 3: Non-CLI providers default to false
	// We do NOT override UseCodeExecutionMode here — let the factory decide based on the actual resolved LLM provider

	// Ensure step execution folder exists before creating conditional agent (agent needs to write to this folder)
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, decisionStepPath)
	if err := hcpo.ensureStepExecutionFolderExists(ctx, stepExecutionPath); err != nil {
		// Non-blocking: log warning but continue execution (folder will be created when files are written)
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure decision step execution folder exists: %v (continuing - folder will be created when files are written)", err))
	}

	// Get conditional agent for this step (step-specific or default), with phase customized for decision evaluation
	conditionalAgent := hcpo.getConditionalAgentForStep(ctx, step, stepIndex, "decision-step-evaluation", "decision_evaluation")

	// Evaluate decision using ConditionalAgent (agent has access to workspace tools)
	// Format variable names and values (same format as execution agent)
	var variableNames, variableValues string
	if hcpo.variablesManifest != nil {
		variableNames = FormatVariableNames(hcpo.variablesManifest)
		variableValues = FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues)
	}

	// Pre-save prompts.json so get_step_prompts works during execution
	{
		tv := map[string]string{
			"ExecutionOutput": executionResult,
			"Question":        decisionStep.DecisionEvaluationQuestion,
			"LearningHistory": learningHistory,
			"VariableNames":   variableNames,
			"VariableValues":  variableValues,
		}
		sp := conditionalAgent.decisionSystemPromptProcessor(tv)
		um := conditionalAgent.decisionUserMessageProcessor(tv)
		var model string
		if conditionalAgent.GetConfig() != nil && conditionalAgent.GetConfig().LLMConfig.Primary.ModelID != "" {
			model = fmt.Sprintf("%s/%s", conditionalAgent.GetConfig().LLMConfig.Primary.Provider, conditionalAgent.GetConfig().LLMConfig.Primary.ModelID)
		}
		hcpo.preSavePromptsJSON(stepIndex, decisionStepPath, "decision_evaluation", sp, um, model, "decision-prompts.json")
	}

	decisionResponse, err := conditionalAgent.EvaluateDecision(ctx, executionResult, decisionStep.DecisionEvaluationQuestion, stepIndex, 0, conditionalAgent.GetConfig().UseCodeExecutionMode, learningHistory, variableNames, variableValues)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to evaluate decision step %d: %v", stepIndex+1, err), nil)
		// Emit error event using centralized method
		hcpo.EmitOrchestratorAgentError(ctx, "conditional", "decision-step-evaluation", fmt.Sprintf("Evaluate decision: %s", decisionStep.DecisionEvaluationQuestion), err.Error(), stepIndex, 0)
		return false, "", fmt.Errorf("failed to evaluate decision step: %w", err)
	}

	// Store structured response in the step for event emission (on the PlanStepInterface)
	if decisionStepPlan, ok := step.(*DecisionPlanStep); ok {
		decisionStepPlan.DecisionResponse = decisionResponse
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Decision step evaluated: result=%t", decisionResponse.Result))

	// Emit decision_evaluated event with structured response
	hcpo.emitDecisionEvaluatedEvent(ctx, step, stepIndex, decisionStepPath, decisionResponse)

	// Store decision evaluation result to logs (always enabled)
	// Determine validation workspace path (same logic as validation agent)
	// validationWorkspacePath already defined above
	// Get validation folder path based on decisionStepPath (step-{X})
	validationFolderPath := getValidationFolderPath(validationWorkspacePath, decisionStepPath)

	// Save decision evaluation result
	decisionEvaluationFilePath := fmt.Sprintf("%s/decision-evaluation.json", validationFolderPath)
	decisionEvaluationResponse := map[string]interface{}{
		"step_index":                   stepIndex + 1,
		"step_path":                    decisionStepPath,
		"decision_step_id":             step.GetID(),
		"decision_evaluation_question": decisionStep.DecisionEvaluationQuestion,
		"decision_result":              decisionResponse.Result,
		"decision_reasoning":           decisionResponse.Reasoning,
		"if_true_next_step_id":         decisionStep.IfTrueNextStepID,
		"if_false_next_step_id":        decisionStep.IfFalseNextStepID,
		"timestamp":                    time.Now().Format(time.RFC3339),
	}

	// Marshal and save decision evaluation result
	decisionJSON, err := json.MarshalIndent(decisionEvaluationResponse, "", "  ")
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal decision evaluation response to JSON: %v", err))
	} else {
		if err := hcpo.WriteWorkspaceFile(ctx, decisionEvaluationFilePath, string(decisionJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write decision evaluation response to %s: %v", decisionEvaluationFilePath, err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("💾 Decision evaluation response saved to: %s", decisionEvaluationFilePath))
		}
	}

	// Emit step_finished event
	hcpo.emitStepFinishedEvent(ctx, step, stepIndex, decisionStepPath, false)

	// Update context files (use the inner step's output)
	_ = updatedContextFiles // Context files are managed by the caller

	// Return the decision result and execution result
	return decisionResponse.Result, executionResult, nil
}
