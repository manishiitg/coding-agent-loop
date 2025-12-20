package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// executeDecisionStep executes a decision step by:
// 1. Executing the single decision_step
// 2. Evaluating the output using ConditionalLLM with decision_evaluation_question
// 3. Returning the decision result for routing (handled by main execution loop)
//
// Unlike conditional steps which only evaluate conditions, decision steps
// execute a step first and then evaluate the output to determine routing.
//
// NOTE: This function works with TodoStep which uses boolean flags (has_decision_step).
// The step type is already validated by the main execution loop before calling this function.
// Returns: (decisionResult bool, executionResult string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeDecisionStep(
	ctx context.Context,
	step *TodoStep,
	stepIndex int,
	progress *StepProgress,
	previousContextFiles []string,
	iteration int,
	execCtx *ExecutionContext,
	allSteps []TodoStep,
) (bool, string, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("🎯 Executing decision step %d: %s", stepIndex+1, step.Title))

	// Validate decision step has required fields
	if step.DecisionStep == nil {
		return false, "", fmt.Errorf("decision step %d (%s) is missing required decision_step field", stepIndex+1, step.Title)
	}
	if step.DecisionEvaluationQuestion == "" {
		return false, "", fmt.Errorf("decision step %d (%s) is missing required decision_evaluation_question field", stepIndex+1, step.Title)
	}
	if step.IfTrueNextStepID == "" {
		return false, "", fmt.Errorf("decision step %d (%s) is missing required if_true_next_step_id field", stepIndex+1, step.Title)
	}
	if step.IfFalseNextStepID == "" {
		return false, "", fmt.Errorf("decision step %d (%s) is missing required if_false_next_step_id field", stepIndex+1, step.Title)
	}

	// Emit step_started event for decision step
	decisionStepPath := fmt.Sprintf("step-%d", stepIndex+1)
	hcpo.emitStepStartedEvent(ctx, *step, stepIndex, decisionStepPath, false)

	// Calculate run number based on how many times this decision step has been evaluated
	// Count both true and false evaluations to get total execution count
	runNumber := 1 // Default to run1 for first execution
	if progress.DecisionEvaluationCounts != nil {
		trueKey := fmt.Sprintf("%s:true", step.ID)
		falseKey := fmt.Sprintf("%s:false", step.ID)
		trueCount := progress.DecisionEvaluationCounts[trueKey]
		falseCount := progress.DecisionEvaluationCounts[falseKey]
		totalEvaluations := trueCount + falseCount
		runNumber = totalEvaluations + 1 // Next run number
	}

	// 1. Execute the decision_step
	hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing decision step's inner step: %s (run %d)", step.DecisionStep.Title, runNumber))
	// Use simple path: step-{X}-decision
	innerStepPath := fmt.Sprintf("step-%d-decision", stepIndex+1)

	// Prepare inner step with proper code execution mode configuration
	// Priority: inner step's own step config > parent decision step's config > preset default
	innerStep := *step.DecisionStep

	// First, try to match step configs at execution time (in case step_config.json was updated)
	_ = ApplyStepConfigFromFile(ctx, &innerStep, hcpo)
	// Ignore error - use defaults if config loading fails

	// If inner step still doesn't have code execution mode set, inherit from parent decision step
	if innerStep.AgentConfigs == nil || innerStep.AgentConfigs.UseCodeExecutionMode == nil {
		// Inner step doesn't have code execution mode set, inherit from parent if available
		if step.AgentConfigs != nil && step.AgentConfigs.UseCodeExecutionMode != nil {
			// Create AgentConfigs if it doesn't exist
			if innerStep.AgentConfigs == nil {
				innerStep.AgentConfigs = &AgentConfigs{}
			}
			// Copy the code execution mode from parent
			codeExecMode := *step.AgentConfigs.UseCodeExecutionMode
			innerStep.AgentConfigs.UseCodeExecutionMode = &codeExecMode
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Inherited code execution mode from parent decision step (ID: %s): %v", step.ID, codeExecMode))
		}
	}

	executionResult, updatedContextFiles, err := hcpo.executeSingleStep(
		ctx,
		innerStep,
		stepIndex,
		innerStepPath,
		1, // totalSteps = 1 for single decision step
		iteration,
		previousContextFiles,
		progress,
		false, // isBranchStep = false
		execCtx,
		allSteps,
		true,                            // isDecisionInnerStep = true (skip final human feedback on success)
		nil,                             // decisionContext = nil (this is the inner step, not a routed step)
		step.DecisionEvaluationQuestion, // decisionEvaluationQuestion - pass to execution agent for output formatting
		false,                           // isSubAgent = false (decision inner step, not a sub-agent)
	)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute decision step's inner step '%s': %v", step.DecisionStep.Title, err), nil)
		return false, "", fmt.Errorf("failed to execute decision step's inner step '%s': %w", step.DecisionStep.Title, err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Decision step's inner step completed. Output length: %d chars", len(executionResult)))

	// Check if inner step signaled early workflow termination
	// Inner steps can signal termination by including "WORKFLOW_END" or "END_WORKFLOW" in their output
	executionResultUpper := strings.ToUpper(executionResult)
	if strings.Contains(executionResultUpper, "WORKFLOW_END") || strings.Contains(executionResultUpper, "END_WORKFLOW") {
		hcpo.GetLogger().Info(fmt.Sprintf("🏁 Decision step's inner step '%s' signaled workflow termination - ending workflow early", step.DecisionStep.Title))
		// Return a special error that the caller can detect to signal termination
		return false, executionResult, fmt.Errorf("WORKFLOW_END: decision step's inner step '%s' signaled workflow termination", step.DecisionStep.Title)
	}

	// Store inner step execution result to logs (if enabled)
	if hcpo.saveValidationResponses {
		// Determine validation workspace path (same logic as validation agent)
		var validationWorkspacePath string
		if hcpo.selectedRunFolder != "" {
			validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		} else {
			validationWorkspacePath = hcpo.GetWorkspacePath()
		}

		// Get execution logs folder path based on innerStepPath (step-{X}-decision)
		executionLogsFolderPath := getExecutionFolderPathForLogs(validationWorkspacePath, innerStepPath)

		// Save inner step execution result
		executionResultFilePath := fmt.Sprintf("%s/decision-inner-step.json", executionLogsFolderPath)
		executionResponse := map[string]interface{}{
			"step_index":       stepIndex + 1,
			"step_path":        innerStepPath,
			"decision_step_id": step.ID,
			"execution_result": executionResult,
			"timestamp":        time.Now().Format(time.RFC3339),
		}

		// Marshal and save execution result
		executionJSON, err := json.MarshalIndent(executionResponse, "", "  ")
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal decision inner step execution response to JSON: %v", err))
		} else {
			if err := hcpo.WriteWorkspaceFile(ctx, executionResultFilePath, string(executionJSON)); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write decision inner step execution response to %s: %v", executionResultFilePath, err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Decision inner step execution response saved to: %s", executionResultFilePath))
			}
		}
	}

	// 2. Evaluate the execution output using the ConditionalAgent (full agent with workspace tools)
	hcpo.GetLogger().Info(fmt.Sprintf("🤔 Evaluating decision step output with question: %s", step.DecisionEvaluationQuestion))

	// Read learnings separately (passed as separate learningHistory variable, not in executionResult)
	// Use inner decision step ID for learnings (consistent with orchestration pattern)
	innerStepID := step.DecisionStep.ID
	if innerStepID == "" {
		// Fallback to parent wrapper ID if inner step has no ID
		innerStepID = step.ID
	}
	learningHistory, _ := hcpo.LoadStepLearningHistory(ctx, innerStepID, stepIndex, decisionStepPath, "decision")

	// Determine code execution mode: Priority: step config > orchestrator default
	var isCodeExecutionMode bool
	if step.AgentConfigs != nil && step.AgentConfigs.UseCodeExecutionMode != nil {
		isCodeExecutionMode = *step.AgentConfigs.UseCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode for decision evaluation: %v", isCodeExecutionMode))
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator code execution mode for decision evaluation: %v", isCodeExecutionMode))
	}

	// Ensure step config has UseCodeExecutionMode set if it differs from default or if we want code execution mode
	// This ensures getConditionalAgentForStep creates a step-specific agent with the correct mode
	if step.AgentConfigs == nil {
		step.AgentConfigs = &AgentConfigs{}
	}
	if step.AgentConfigs.UseCodeExecutionMode == nil {
		// Set it so getConditionalAgentForStep will create a step-specific agent
		step.AgentConfigs.UseCodeExecutionMode = &isCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Setting UseCodeExecutionMode=%v in step config for conditional agent creation", isCodeExecutionMode))
	}

	// Get conditional agent for this step (step-specific or default), with phase customized for decision evaluation
	conditionalAgent := hcpo.getConditionalAgentForStep(ctx, *step, stepIndex, "decision-step-evaluation", "decision_evaluation")

	// Evaluate decision using ConditionalAgent (agent has access to workspace tools)
	decisionResponse, err := conditionalAgent.EvaluateDecision(ctx, executionResult, step.DecisionEvaluationQuestion, stepIndex, 0, isCodeExecutionMode, learningHistory)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to evaluate decision step %d: %v", stepIndex+1, err), nil)
		// Emit error event using centralized method
		hcpo.EmitOrchestratorAgentError(ctx, "conditional", "decision-step-evaluation", fmt.Sprintf("Evaluate decision: %s", step.DecisionEvaluationQuestion), err.Error(), stepIndex, 0)
		return false, "", fmt.Errorf("failed to evaluate decision step: %w", err)
	}

	// Store structured response in the step for event emission
	step.DecisionResponse = decisionResponse

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Decision step evaluated: result=%t", decisionResponse.Result))

	// Emit decision_evaluated event with structured response
	hcpo.emitDecisionEvaluatedEvent(ctx, *step, stepIndex, decisionStepPath, decisionResponse)

	// Store decision evaluation result to logs (if enabled)
	if hcpo.saveValidationResponses {
		// Determine validation workspace path (same logic as validation agent)
		var validationWorkspacePath string
		if hcpo.selectedRunFolder != "" {
			validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		} else {
			validationWorkspacePath = hcpo.GetWorkspacePath()
		}

		// Get validation folder path based on decisionStepPath (step-{X})
		validationFolderPath := getValidationFolderPath(validationWorkspacePath, decisionStepPath)

		// Save decision evaluation result
		decisionEvaluationFilePath := fmt.Sprintf("%s/decision-evaluation.json", validationFolderPath)
		decisionEvaluationResponse := map[string]interface{}{
			"step_index":                   stepIndex + 1,
			"step_path":                    decisionStepPath,
			"decision_step_id":             step.ID,
			"decision_evaluation_question": step.DecisionEvaluationQuestion,
			"decision_result":              decisionResponse.Result,
			"decision_reasoning":           decisionResponse.Reasoning,
			"if_true_next_step_id":         step.IfTrueNextStepID,
			"if_false_next_step_id":        step.IfFalseNextStepID,
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
	}

	// Emit step_finished event
	hcpo.emitStepFinishedEvent(ctx, *step, stepIndex, decisionStepPath, false)

	// Update context files (use the inner step's output)
	_ = updatedContextFiles // Context files are managed by the caller

	// Return the decision result and execution result
	return decisionResponse.Result, executionResult, nil
}
