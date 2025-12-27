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
// NOTE: This function works with PlanStepInterface (specifically DecisionPlanStep).
// The step type is already validated by the main execution loop before calling this function.
// Returns: (decisionResult bool, executionResult string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeDecisionStep(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	progress *StepProgress,
	previousContextFiles []string,
	iteration int,
	execCtx *ExecutionContext,
	allSteps []PlanStepInterface,
) (bool, string, error) {
	// Steps are already PlanStepInterface - no conversion needed

	// Get inner step as PlanStepInterface from original step
	decisionStep, ok := step.(*DecisionPlanStep)
	if !ok {
		return false, "", fmt.Errorf("step is not a DecisionPlanStep")
	}
	innerStepPlan := decisionStep.DecisionStep

	hcpo.GetLogger().Info(fmt.Sprintf("🎯 Executing decision step %d: %s", stepIndex+1, step.GetTitle()))

	// Validate decision step has required fields
	if innerStepPlan == nil {
		return false, "", fmt.Errorf("decision step %d (%s) is missing required decision_step field", stepIndex+1, step.GetTitle())
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

	// 1. Execute the decision_step
	hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing decision step's inner step: %s (run %d)", innerStepPlan.GetTitle(), runNumber))
	// Use simple path: step-{X}-decision
	innerStepPath := fmt.Sprintf("step-%d-decision", stepIndex+1)

	// Prepare inner step with proper code execution mode configuration
	// Priority: inner step's own step config > parent decision step's config > preset default
	// First, try to match step configs at execution time (in case step_config.json was updated)
	_ = ApplyStepConfigFromFile(ctx, innerStepPlan, hcpo)
	// Ignore error - use defaults if config loading fails

	// If inner step still doesn't have code execution mode set, inherit from parent decision step
	innerStepConfigs := getAgentConfigs(innerStepPlan)
	parentStepConfigs := getAgentConfigs(step)
	if innerStepConfigs == nil || innerStepConfigs.UseCodeExecutionMode == nil {
		// Inner step doesn't have code execution mode set, inherit from parent if available
		if parentStepConfigs != nil && parentStepConfigs.UseCodeExecutionMode != nil {
			// Set AgentConfigs on inner step if it doesn't exist (handle all step types)
			if innerStepConfigs == nil {
				switch innerStep := innerStepPlan.(type) {
				case *RegularPlanStep:
					innerStep.AgentConfigs = &AgentConfigs{}
				case *ConditionalPlanStep:
					innerStep.AgentConfigs = &AgentConfigs{}
				case *DecisionPlanStep:
					innerStep.AgentConfigs = &AgentConfigs{}
				case *OrchestrationPlanStep:
					innerStep.AgentConfigs = &AgentConfigs{}
				}
				innerStepConfigs = getAgentConfigs(innerStepPlan)
			}
			// Inherit UseCodeExecutionMode from parent decision step
			if innerStepConfigs != nil {
				codeExecMode := *parentStepConfigs.UseCodeExecutionMode
				innerStepConfigs.UseCodeExecutionMode = &codeExecMode
				hcpo.GetLogger().Info(fmt.Sprintf("🔧 Inherited code execution mode from parent decision step (ID: %s) to inner step (ID: %s): %v", step.GetID(), innerStepPlan.GetID(), codeExecMode))
			}
		} else {
			// Try to load parent step config from file if not already loaded
			if err := ApplyStepConfigFromFile(ctx, step, hcpo); err == nil {
				parentStepConfigs = getAgentConfigs(step)
				if parentStepConfigs != nil && parentStepConfigs.UseCodeExecutionMode != nil {
					// Initialize inner step config if needed
					if innerStepConfigs == nil {
						switch innerStep := innerStepPlan.(type) {
						case *RegularPlanStep:
							innerStep.AgentConfigs = &AgentConfigs{}
						case *ConditionalPlanStep:
							innerStep.AgentConfigs = &AgentConfigs{}
						case *DecisionPlanStep:
							innerStep.AgentConfigs = &AgentConfigs{}
						case *OrchestrationPlanStep:
							innerStep.AgentConfigs = &AgentConfigs{}
						}
						innerStepConfigs = getAgentConfigs(innerStepPlan)
					}
					// Inherit UseCodeExecutionMode from parent decision step
					if innerStepConfigs != nil {
						codeExecMode := *parentStepConfigs.UseCodeExecutionMode
						innerStepConfigs.UseCodeExecutionMode = &codeExecMode
						hcpo.GetLogger().Info(fmt.Sprintf("🔧 Loaded and inherited code execution mode from parent decision step (ID: %s) to inner step (ID: %s): %v", step.GetID(), innerStepPlan.GetID(), codeExecMode))
					}
				}
			}
		}
	}

	executionResult, updatedContextFiles, err := hcpo.executeSingleStep(
		ctx,
		innerStepPlan, // Use PlanStepInterface for executeSingleStep
		stepIndex,
		innerStepPath,
		1, // totalSteps = 1 for single decision step
		iteration,
		previousContextFiles,
		progress,
		false, // isBranchStep = false
		execCtx,
		allSteps,                                // Use []PlanStepInterface
		true,                                    // isDecisionInnerStep = true (skip final human feedback on success)
		nil,                                     // decisionContext = nil (this is the inner step, not a routed step)
		decisionStep.DecisionEvaluationQuestion, // decisionEvaluationQuestion - pass to execution agent for output formatting
		false,                                   // isSubAgent = false (decision inner step, not a sub-agent)
		[]string{},                              // previousExecutionResults - empty for decision steps (they don't need previous execution outputs)
		nil,                                     // orchestrationRoutes - nil for decision steps (not sub-agents)
	)
	if err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute decision step's inner step '%s': %v", innerStepPlan.GetTitle(), err), nil)
		return false, "", fmt.Errorf("failed to execute decision step's inner step '%s': %w", innerStepPlan.GetTitle(), err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Decision step's inner step completed. Output length: %d chars", len(executionResult)))

	// Check if inner step signaled early workflow termination
	// Inner steps can signal termination by including "WORKFLOW_END" or "END_WORKFLOW" in their output
	executionResultUpper := strings.ToUpper(executionResult)
	if strings.Contains(executionResultUpper, "WORKFLOW_END") || strings.Contains(executionResultUpper, "END_WORKFLOW") {
		hcpo.GetLogger().Info(fmt.Sprintf("🏁 Decision step's inner step '%s' signaled workflow termination - ending workflow early", innerStepPlan.GetTitle()))
		// Return a special error that the caller can detect to signal termination
		return false, executionResult, fmt.Errorf("WORKFLOW_END: decision step's inner step '%s' signaled workflow termination", innerStepPlan.GetTitle())
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
			"decision_step_id": step.GetID(),
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
	hcpo.GetLogger().Info(fmt.Sprintf("🤔 Evaluating decision step output with question: %s", decisionStep.DecisionEvaluationQuestion))

	// Read learnings separately (passed as separate learningHistory variable, not in executionResult)
	// Use inner decision step ID for learnings (consistent with orchestration pattern)
	innerStepID := innerStepPlan.GetID()
	if innerStepID == "" {
		// Fallback to parent wrapper ID if inner step has no ID
		innerStepID = step.GetID()
	}
	learningHistory, _ := hcpo.LoadStepLearningHistory(ctx, innerStepID, stepIndex, decisionStepPath, "decision")

	// Determine code execution mode: Priority: step config > orchestrator default
	var isCodeExecutionMode bool
	parentStepConfigs = getAgentConfigs(step)
	if parentStepConfigs != nil && parentStepConfigs.UseCodeExecutionMode != nil {
		isCodeExecutionMode = *parentStepConfigs.UseCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode for decision evaluation: %v", isCodeExecutionMode))
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator code execution mode for decision evaluation: %v", isCodeExecutionMode))
	}

	// Ensure step config has UseCodeExecutionMode set if it differs from default or if we want code execution mode
	// This ensures getConditionalAgentForStep creates a step-specific agent with the correct mode
	if decisionStep.AgentConfigs == nil {
		decisionStep.AgentConfigs = &AgentConfigs{}
	}
	if decisionStep.AgentConfigs.UseCodeExecutionMode == nil {
		// Set it so getConditionalAgentForStep will create a step-specific agent
		decisionStep.AgentConfigs.UseCodeExecutionMode = &isCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Setting UseCodeExecutionMode=%v in step config for conditional agent creation", isCodeExecutionMode))
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

	decisionResponse, err := conditionalAgent.EvaluateDecision(ctx, executionResult, decisionStep.DecisionEvaluationQuestion, stepIndex, 0, isCodeExecutionMode, learningHistory, variableNames, variableValues)
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

	// AUTO-UNLOCK LEARNINGS: If decision result is false, automatically unlock learnings for the inner step
	// This ensures that when a decision step returns false, the inner step can learn from the failure
	if !decisionResponse.Result {
		innerStepID := innerStepPlan.GetID()
		if innerStepID == "" {
			// Fallback to parent wrapper ID if inner step has no ID
			innerStepID = step.GetID()
		}
		// Get agent configs for the inner step to check if learnings are locked
		innerStepConfigs := getAgentConfigs(innerStepPlan)
		isLearningsLocked := innerStepConfigs != nil && innerStepConfigs.LockLearnings != nil && *innerStepConfigs.LockLearnings
		if isLearningsLocked {
			hcpo.GetLogger().Info(fmt.Sprintf("🔓 Decision step returned FALSE - auto-unlocking learnings for inner step %s so it can learn from the failure", innerStepID))
			if unlockErr := hcpo.unlockStepLearningsInConfig(ctx, innerStepID); unlockErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to auto-unlock learnings for inner step %s: %v", innerStepID, unlockErr))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Auto-unlocked learnings for inner step %s (decision step returned false)", innerStepID))
				// Update unlock metadata - use inner step path for learning path identifier
				innerStepPath := fmt.Sprintf("step-%d-decision", stepIndex+1)
				learningPathIdentifier := innerStepID // Use step ID as learning path identifier (new format)
				if metadataErr := hcpo.updateUnlockMetadata(ctx, innerStepID, stepIndex, innerStepPath, learningPathIdentifier, "decision_step_false"); metadataErr != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update unlock metadata for inner step %s: %v", innerStepID, metadataErr))
				}
			}
		}
	}

	// Emit decision_evaluated event with structured response
	hcpo.emitDecisionEvaluatedEvent(ctx, step, stepIndex, decisionStepPath, decisionResponse)

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
	}

	// Emit step_finished event
	hcpo.emitStepFinishedEvent(ctx, step, stepIndex, decisionStepPath, false)

	// Update context files (use the inner step's output)
	_ = updatedContextFiles // Context files are managed by the caller

	// Return the decision result and execution result
	return decisionResponse.Result, executionResult, nil
}
