package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/shared"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

const (
	maxOrchestrationIterations = 10 // Maximum iterations for orchestration step loop
)

// executeOrchestrationStep executes an orchestration step by:
//  1. Looping until success criteria is met:
//     a. Execute main orchestration step (with sub-agent output in context if available)
//     b. Evaluate success criteria + route selection using OrchestrationAgent
//     c. If success criteria met → return success
//     d. If not met → execute selected sub-agent → loop back
//  2. Return success status and next step ID
//
// Returns: (successCriteriaMet bool, nextStepID string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeOrchestrationStep(
	ctx context.Context,
	step *TodoStep,
	stepIndex int,
	progress *StepProgress,
	previousContextFiles []string,
	iteration int,
	execCtx *ExecutionContext,
	allSteps []TodoStep,
) (bool, string, error) {
	hcpo.GetLogger().Info(fmt.Sprintf("🎯 Executing orchestration step %d: %s", stepIndex+1, step.Title))

	// Validate orchestration step has required fields
	if step.OrchestrationStep == nil {
		return false, "", fmt.Errorf("orchestration step %d (%s) is missing required orchestration_step field", stepIndex+1, step.Title)
	}
	if len(step.OrchestrationRoutes) == 0 {
		return false, "", fmt.Errorf("orchestration step %d (%s) has no orchestration routes defined", stepIndex+1, step.Title)
	}
	if step.NextStepID == "" {
		return false, "", fmt.Errorf("orchestration step %d (%s) is missing required next_step_id field", stepIndex+1, step.Title)
	}

	// Emit step_started event for orchestration step
	orchestrationStepPath := fmt.Sprintf("step-%d", stepIndex+1)
	hcpo.emitStepStartedEvent(ctx, *step, stepIndex, orchestrationStepPath, false)

	// Initialize or load orchestration progress
	if progress.OrchestrationSteps == nil {
		progress.OrchestrationSteps = make(map[int]OrchestrationStepProgress)
	}
	orchestrationProgress, exists := progress.OrchestrationSteps[stepIndex]
	if !exists {
		// Initialize fresh orchestration progress
		orchestrationProgress = OrchestrationStepProgress{
			MainStepExecuted:    false,
			SubAgentCompleted:   false,
			SuccessCriteriaMet:  false,
			IterationCount:      0,
			SubAgentOutput:      "",
			ConversationHistory: []llmtypes.MessageContent{},
		}
		progress.OrchestrationSteps[stepIndex] = orchestrationProgress
	}

	hcpo.GetLogger().Info(fmt.Sprintf("📊 Orchestration step progress: iteration=%d, success_criteria_met=%t, selected_route=%s",
		orchestrationProgress.IterationCount, orchestrationProgress.SuccessCriteriaMet, orchestrationProgress.SelectedRouteID))

	// Main orchestration loop: execute until success criteria is met
	for orchestrationIteration := orchestrationProgress.IterationCount; orchestrationIteration < maxOrchestrationIterations; orchestrationIteration++ {
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Orchestration step iteration %d/%d", orchestrationIteration+1, maxOrchestrationIterations))

		// Check for context cancellation
		select {
		case <-ctx.Done():
			return false, "", fmt.Errorf("orchestration step execution canceled: %w", ctx.Err())
		default:
		}

		// Build context for main orchestration step execution
		orchestrationContext := previousContextFiles

		// 1. Execute main orchestration step using OrchestrationOrchestratorAgent
		mainStepPath := fmt.Sprintf("step-%d-orchestration", stepIndex+1)
		if orchestrationIteration > 0 {
			mainStepPath = fmt.Sprintf("step-%d-orchestration-%d", stepIndex+1, orchestrationIteration+1)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing main orchestration step: %s (iteration %d)", step.OrchestrationStep.Title, orchestrationIteration+1))

		// Prepare main orchestration step
		mainOrchestrationStep := *step.OrchestrationStep

		// Execute using OrchestrationOrchestratorAgent with structured output (includes evaluation)
		orchestrationResponse, updatedConversationHistory, err := hcpo.executeOrchestrationOrchestratorStep(
			ctx,
			mainOrchestrationStep,
			stepIndex,
			mainStepPath,
			iteration,
			orchestrationContext,
			step.OrchestrationRoutes,
			orchestrationProgress.ConversationHistory,
			allSteps,
			execCtx,
		)
		if err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute main orchestration step '%s': %v", step.OrchestrationStep.Title, err), nil)
			return false, "", fmt.Errorf("failed to execute main orchestration step '%s': %w", step.OrchestrationStep.Title, err)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Main orchestration step completed. Success criteria met: %t, Selected route: %s", orchestrationResponse.SuccessCriteriaMet, orchestrationResponse.SelectedRouteID))

		// Update orchestration progress with conversation history
		orchestrationProgress.MainStepExecuted = true
		orchestrationProgress.IterationCount = orchestrationIteration + 1
		orchestrationProgress.ConversationHistory = updatedConversationHistory

		// Store main step execution result to logs (if enabled)
		if hcpo.saveValidationResponses {
			var validationWorkspacePath string
			if hcpo.selectedRunFolder != "" {
				validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
			} else {
				validationWorkspacePath = hcpo.GetWorkspacePath()
			}

			executionLogsFolderPath := getExecutionFolderPathForLogs(validationWorkspacePath, mainStepPath)
			executionResultFilePath := fmt.Sprintf("%s/orchestration-main-step.json", executionLogsFolderPath)
			executionResponse := map[string]interface{}{
				"step_index":             stepIndex + 1,
				"step_path":              mainStepPath,
				"orchestration_step_id":  step.ID,
				"iteration":              orchestrationIteration + 1,
				"orchestration_response": orchestrationResponse,
				"timestamp":              time.Now().Format(time.RFC3339),
			}

			executionJSON, err := json.MarshalIndent(executionResponse, "", "  ")
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal orchestration main step execution response to JSON: %v", err))
			} else {
				if err := hcpo.WriteWorkspaceFile(ctx, executionResultFilePath, string(executionJSON)); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write orchestration main step execution response to %s: %v", executionResultFilePath, err))
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("💾 Orchestration main step execution response saved to: %s", executionResultFilePath))
				}
			}
		}

		// Store structured response in the step for event emission
		step.OrchestrationResponse = orchestrationResponse

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Orchestration step evaluated: success_criteria_met=%t, selected_route_id=%s",
			orchestrationResponse.SuccessCriteriaMet, orchestrationResponse.SelectedRouteID))

		// Update orchestration progress
		orchestrationProgress.SuccessCriteriaMet = orchestrationResponse.SuccessCriteriaMet
		orchestrationProgress.SelectedRouteID = orchestrationResponse.SelectedRouteID
		progress.OrchestrationSteps[stepIndex] = orchestrationProgress

		// Save progress
		if err := hcpo.saveStepProgress(ctx, progress); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save orchestration step progress: %w", err))
		}

		// Store orchestration evaluation result to logs (if enabled)
		if hcpo.saveValidationResponses {
			var validationWorkspacePath string
			if hcpo.selectedRunFolder != "" {
				validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
			} else {
				validationWorkspacePath = hcpo.GetWorkspacePath()
			}

			validationFolderPath := getValidationFolderPath(validationWorkspacePath, orchestrationStepPath)
			orchestrationEvaluationFilePath := fmt.Sprintf("%s/orchestration-evaluation.json", validationFolderPath)
			orchestrationEvaluationResponse := map[string]interface{}{
				"step_index":            stepIndex + 1,
				"step_path":             orchestrationStepPath,
				"orchestration_step_id": step.ID,
				"iteration":             orchestrationIteration + 1,
				"selected_route_id":     orchestrationResponse.SelectedRouteID,
				"reasoning":             orchestrationResponse.Reasoning,
				"success_criteria_met":  orchestrationResponse.SuccessCriteriaMet,
				"success_reasoning":     orchestrationResponse.SuccessReasoning,
				"next_step_id":          step.NextStepID,
				"timestamp":             time.Now().Format(time.RFC3339),
			}

			orchestrationJSON, err := json.MarshalIndent(orchestrationEvaluationResponse, "", "  ")
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal orchestration evaluation response to JSON: %v", err))
			} else {
				if err := hcpo.WriteWorkspaceFile(ctx, orchestrationEvaluationFilePath, string(orchestrationJSON)); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write orchestration evaluation response to %s: %v", orchestrationEvaluationFilePath, err))
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("💾 Orchestration evaluation response saved to: %s", orchestrationEvaluationFilePath))
				}
			}
		}

		// 3. Check if success criteria is met
		if orchestrationResponse.SuccessCriteriaMet {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Orchestration step success criteria met after %d iterations", orchestrationIteration+1))

			// Check if validation has already verified success
			if orchestrationResponse.SuccessCriteriaVerifiedByValidation {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Validation confirmed success criteria is met"))
				orchestrationProgress.SuccessCriteriaMet = true
				progress.OrchestrationSteps[stepIndex] = orchestrationProgress

				// Note: Learning was already triggered in a previous iteration when validation first verified success
				// No need to trigger learning again here

				// Emit orchestration_finished event
				// TODO: Add orchestration_finished event emission

				// Emit step_finished event
				hcpo.emitStepFinishedEvent(ctx, *step, stepIndex, orchestrationStepPath, false)

				// Return success
				return true, step.NextStepID, nil
			}

			// Success criteria met but not yet verified by validation - call validation as sub-agent
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 Success criteria met, calling validation to verify"))

			// Prepare validation template variables
			var validationWorkspacePath string
			if hcpo.selectedRunFolder != "" {
				validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
			} else {
				validationWorkspacePath = hcpo.GetWorkspacePath()
			}

			validationTemplateVars := map[string]string{
				"StepTitle":           step.OrchestrationStep.Title,
				"StepDescription":     step.OrchestrationStep.Description,
				"StepSuccessCriteria": step.OrchestrationStep.SuccessCriteria,
				"StepContextOutput":   step.OrchestrationStep.ContextOutput,
				"WorkspacePath":       validationWorkspacePath,
				"ExecutionHistory":    shared.FormatConversationHistory(orchestrationProgress.ConversationHistory),
			}

			// Add context dependencies
			if len(step.OrchestrationStep.ContextDependencies) > 0 {
				validationTemplateVars["StepContextDependencies"] = strings.Join(step.OrchestrationStep.ContextDependencies, ", ")
			} else {
				validationTemplateVars["StepContextDependencies"] = ""
			}

			// No loop condition for orchestration steps
			validationTemplateVars["LoopCondition"] = ""
			validationTemplateVars["DecisionReasoning"] = ""

			// Create validation agent
			validationAgentName := fmt.Sprintf("orchestration-validation-step-%d", stepIndex+1)
			validationAgent, err := hcpo.createValidationAgent(ctx, "validation", stepIndex+1, validationAgentName, step.OrchestrationStep.AgentConfigs)
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to create validation agent for orchestration step %d: %v", stepIndex+1, err), nil)
				return false, "", fmt.Errorf("failed to create validation agent for orchestration step: %w", err)
			}

			// Call validation
			validationResponse, _, err := validationAgent.(*HumanControlledTodoPlannerValidationAgent).ExecuteStructured(ctx, validationTemplateVars, []llmtypes.MessageContent{})
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Validation failed for orchestration step %d: %v", stepIndex+1, err), nil)
				return false, "", fmt.Errorf("validation failed for orchestration step: %w", err)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Validation completed: is_success_criteria_met=%t, status=%s", validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus))

			// Add validation response to conversation history as an assistant message
			validationMessageText := fmt.Sprintf("Validation agent completed with the following result:\n\n**Success Criteria Met**: %t\n**Execution Status**: %s\n**Reasoning**: %s",
				validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus, validationResponse.Reasoning)
			if len(validationResponse.Feedback) > 0 {
				validationMessageText += "\n\n**Feedback**:\n"
				for i, feedback := range validationResponse.Feedback {
					validationMessageText += fmt.Sprintf("%d. [%s] %s: %s\n", i+1, feedback.Severity, feedback.Type, feedback.Description)
				}
			}

			validationMessage := llmtypes.MessageContent{
				Role: llmtypes.ChatMessageTypeAI,
				Parts: []llmtypes.ContentPart{
					llmtypes.TextContent{
						Text: validationMessageText,
					},
				},
			}
			orchestrationProgress.ConversationHistory = append(orchestrationProgress.ConversationHistory, validationMessage)

			// Re-evaluate orchestration with validation response
			hcpo.GetLogger().Info(fmt.Sprintf("🤔 Re-evaluating orchestration step with validation response"))

			// Re-execute orchestration orchestrator agent with updated conversation history (includes validation response)
			orchestrationResponse, updatedConversationHistory, err = hcpo.executeOrchestrationOrchestratorStep(
				ctx,
				mainOrchestrationStep,
				stepIndex,
				mainStepPath,
				iteration,
				orchestrationContext,
				step.OrchestrationRoutes,
				orchestrationProgress.ConversationHistory,
				allSteps,
				execCtx,
			)
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to re-evaluate orchestration step %d: %v", stepIndex+1, err), nil)
				return false, "", fmt.Errorf("failed to re-evaluate orchestration step: %w", err)
			}

			// Update conversation history with latest
			orchestrationProgress.ConversationHistory = updatedConversationHistory

			// Store updated structured response
			step.OrchestrationResponse = orchestrationResponse

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Orchestration step re-evaluated: success_criteria_met=%t, success_criteria_verified_by_validation=%t",
				orchestrationResponse.SuccessCriteriaMet, orchestrationResponse.SuccessCriteriaVerifiedByValidation))

			// Update orchestration progress
			orchestrationProgress.SuccessCriteriaMet = orchestrationResponse.SuccessCriteriaMet
			// ConversationHistory is already updated above when validation message was added
			progress.OrchestrationSteps[stepIndex] = orchestrationProgress

			// Save progress
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save orchestration step progress: %w", err))
			}

			// Check if validation verified success
			if orchestrationResponse.SuccessCriteriaVerifiedByValidation {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Validation verified success criteria - proceeding to next step"))
				orchestrationProgress.SuccessCriteriaMet = true
				progress.OrchestrationSteps[stepIndex] = orchestrationProgress

				// Determine code execution mode
				var isCodeExecutionMode bool
				if step.OrchestrationStep.AgentConfigs != nil && step.OrchestrationStep.AgentConfigs.UseCodeExecutionMode != nil {
					isCodeExecutionMode = *step.OrchestrationStep.AgentConfigs.UseCodeExecutionMode
				} else {
					isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
				}

				// Check learning flags (similar to regular steps)
				isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
				isLearningDisabledStep := step.OrchestrationStep.AgentConfigs != nil && step.OrchestrationStep.AgentConfigs.DisableLearning != nil && *step.OrchestrationStep.AgentConfigs.DisableLearning
				isLearningDetailLevelNone := false
				if step.OrchestrationStep.AgentConfigs != nil && step.OrchestrationStep.AgentConfigs.LearningDetailLevel == "none" {
					isLearningDetailLevelNone = true
				}
				isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
				// CODE EXECUTION MODE: Force learning enabled regardless of step config
				if isCodeExecutionMode && isLearningDisabled {
					hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing learning for orchestration step %d (overriding step config)", stepIndex+1))
					isLearningDisabled = false
				}
				// LOCK LEARNINGS: Check if learnings are locked
				isLearningsLocked := step.OrchestrationStep.AgentConfigs != nil && step.OrchestrationStep.AgentConfigs.LockLearnings != nil && *step.OrchestrationStep.AgentConfigs.LockLearnings
				// TEMP LLM OVERRIDE: Check if learning should be skipped based on which tempLLM was used
				shouldSkipLearningDueToTempOverride := false
				usedTempLLM := "" // Orchestration steps don't use temp LLM, but check for consistency
				if hcpo.executionOptions != nil && usedTempLLM != "" {
					if usedTempLLM == "tempLLM1" && hcpo.executionOptions.SkipLearningWhenTempLLM1 {
						shouldSkipLearningDueToTempOverride = true
					} else if usedTempLLM == "tempLLM2" && hcpo.executionOptions.SkipLearningWhenTempLLM2 {
						shouldSkipLearningDueToTempOverride = true
					}
				}

				// Trigger success learning if enabled
				if !isFastExecuteStep && !isLearningDisabled && !isLearningsLocked && !shouldSkipLearningDueToTempOverride {
					learningPathIdentifier := getLearningPathIdentifier(orchestrationStepPath)
					totalSteps := len(allSteps)
					hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running success learning analysis for orchestration step %s", orchestrationStepPath))
					err := hcpo.runSuccessLearningPhase(ctx, stepIndex, orchestrationStepPath, learningPathIdentifier, totalSteps, step.OrchestrationStep, orchestrationProgress.ConversationHistory, validationResponse, isCodeExecutionMode)
					if err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Success learning phase failed for orchestration step %s: %v", orchestrationStepPath, err))
					} else {
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Success learning analysis completed for orchestration step %s", orchestrationStepPath))
					}
				} else {
					if isFastExecuteStep {
						hcpo.GetLogger().Info(fmt.Sprintf("⚡ Fast mode: Skipping learning agents for orchestration step %d", stepIndex+1))
					} else if isLearningDisabled {
						hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Learning disabled: Skipping learning agents for orchestration step %d", stepIndex+1))
					} else if isLearningsLocked {
						hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping learning agents for orchestration step %d (using existing learnings)", stepIndex+1))
					} else if shouldSkipLearningDueToTempOverride {
						hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM override: Skipping learning agents for orchestration step %d", stepIndex+1))
					}
				}

				// Emit orchestration_finished event
				// TODO: Add orchestration_finished event emission

				// Emit step_finished event
				hcpo.emitStepFinishedEvent(ctx, *step, stepIndex, orchestrationStepPath, false)

				// Return success
				return true, step.NextStepID, nil
			}

			// Validation did not confirm success - proceed to route selection
			hcpo.GetLogger().Info(fmt.Sprintf("⚠️ Validation did not confirm success - proceeding to route selection"))
			// Fall through to route selection logic below
		}

		// 4. Success criteria not met - execute selected sub-agent
		if orchestrationResponse.SelectedRouteID == "" {
			return false, "", fmt.Errorf("orchestration step %d: success criteria not met but no route selected", stepIndex+1)
		}

		// Find the selected route and capture its index
		var selectedRoute *OrchestrationRoute
		subAgentIndex := 0 // Will be set when route is found
		for i := range step.OrchestrationRoutes {
			if step.OrchestrationRoutes[i].RouteID == orchestrationResponse.SelectedRouteID {
				selectedRoute = &step.OrchestrationRoutes[i]
				subAgentIndex = i + 1 // Use 1-based index for path (route 0 -> sub-agent-1, route 1 -> sub-agent-2, etc.)
				break
			}
		}

		if selectedRoute == nil {
			return false, "", fmt.Errorf("orchestration step %d: selected route ID '%s' not found in orchestration routes", stepIndex+1, orchestrationResponse.SelectedRouteID)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("🔀 Executing sub-agent: %s (route: %s, index: %d)", selectedRoute.SubAgentStep.Title, selectedRoute.RouteID, subAgentIndex))

		// Prepare sub-agent step with validation disabled
		subAgentStep := selectedRoute.SubAgentStep
		if subAgentStep.AgentConfigs == nil {
			subAgentStep.AgentConfigs = &AgentConfigs{}
		}
		val := true
		subAgentStep.AgentConfigs.DisableValidation = &val

		// Sub-agents don't receive previous steps history - they work independently based on orchestrator instructions

		// Modify sub-agent step with orchestrator-provided instructions, success criteria, and context settings
		if orchestrationResponse.InstructionsToSubAgent != "" {
			subAgentStep.Description = orchestrationResponse.InstructionsToSubAgent
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Using orchestrator-provided instructions for sub-agent (replacing step description)"))
		}
		if orchestrationResponse.SuccessCriteriaForSubAgent != "" {
			subAgentStep.SuccessCriteria = orchestrationResponse.SuccessCriteriaForSubAgent
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Using orchestrator-provided success criteria for sub-agent (replacing step success criteria)"))
		}
		if orchestrationResponse.ContextDependenciesForSubAgent != "" {
			// Parse comma-separated context dependencies into array
			deps := strings.Split(orchestrationResponse.ContextDependenciesForSubAgent, ",")
			subAgentStep.ContextDependencies = make([]string, 0, len(deps))
			for _, dep := range deps {
				dep = strings.TrimSpace(dep)
				if dep != "" {
					subAgentStep.ContextDependencies = append(subAgentStep.ContextDependencies, dep)
				}
			}
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Using orchestrator-provided context dependencies for sub-agent (replacing step context dependencies): %v", subAgentStep.ContextDependencies))
		}
		if orchestrationResponse.ContextOutputForSubAgent != "" {
			subAgentStep.ContextOutput = orchestrationResponse.ContextOutputForSubAgent
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Using orchestrator-provided context output for sub-agent (replacing step context output): %s", orchestrationResponse.ContextOutputForSubAgent))
		}

		// Execute sub-agent (without previous steps history - sub-agents don't need it)
		// Use format: step-{N}-sub-agent-{index} (e.g., "step-2-sub-agent-1")
		// Index is derived from the route's position in the orchestration routes array (1-based)
		subAgentPath := fmt.Sprintf("step-%d-sub-agent-%d", stepIndex+1, subAgentIndex)
		// Pass empty previousContextFiles to skip building previous steps summary for sub-agents
		subAgentExecutionResult, updatedSubAgentContextFiles, err := hcpo.executeSingleStep(
			ctx,
			subAgentStep,
			stepIndex, // Use parent step index
			subAgentPath,
			1, // totalSteps = 1 for single sub-agent
			iteration,
			[]string{}, // Empty - sub-agents don't need previous steps history
			progress,
			true, // isBranchStep = true (sub-agent is like a branch step)
			execCtx,
			allSteps,
			false, // isDecisionInnerStep = false
			nil,   // decisionContext = nil
			"",    // decisionEvaluationQuestion - empty
			true,  // isSubAgent = true (sub-agents never request human feedback)
		)
		if err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute sub-agent '%s': %v", selectedRoute.SubAgentStep.Title, err), nil)
			return false, "", fmt.Errorf("failed to execute sub-agent '%s': %w", selectedRoute.SubAgentStep.Title, err)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Sub-agent completed. Output length: %d chars", len(subAgentExecutionResult)))

		// Update orchestration progress with sub-agent output
		orchestrationProgress.SubAgentCompleted = true
		orchestrationProgress.SubAgentOutput = subAgentExecutionResult

		// Add sub-agent output to conversation history as an assistant message
		// This makes it feel like a continuous conversation for the main agent
		subAgentMessage := llmtypes.MessageContent{
			Role: llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{
					Text: fmt.Sprintf("Sub-agent (%s) completed with the following output:\n\n%s", selectedRoute.RouteName, subAgentExecutionResult),
				},
			},
		}
		orchestrationProgress.ConversationHistory = append(orchestrationProgress.ConversationHistory, subAgentMessage)

		progress.OrchestrationSteps[stepIndex] = orchestrationProgress

		// Save progress
		if err := hcpo.saveStepProgress(ctx, progress); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save orchestration step progress after sub-agent: %w", err))
		}

		// Update context files for next iteration
		_ = updatedSubAgentContextFiles

		// Loop back to execute main orchestration step again with sub-agent output in context
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Sub-agent completed, looping back to main orchestration step (iteration %d/%d)", orchestrationIteration+2, maxOrchestrationIterations))
	}

	// Max iterations reached without success
	// Trigger failure learning before returning error
	// Determine code execution mode
	var isCodeExecutionMode bool
	if step.OrchestrationStep.AgentConfigs != nil && step.OrchestrationStep.AgentConfigs.UseCodeExecutionMode != nil {
		isCodeExecutionMode = *step.OrchestrationStep.AgentConfigs.UseCodeExecutionMode
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
	}

	// Check learning flags (similar to regular steps)
	isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
	isLearningDisabledStep := step.OrchestrationStep.AgentConfigs != nil && step.OrchestrationStep.AgentConfigs.DisableLearning != nil && *step.OrchestrationStep.AgentConfigs.DisableLearning
	isLearningDetailLevelNone := false
	if step.OrchestrationStep.AgentConfigs != nil && step.OrchestrationStep.AgentConfigs.LearningDetailLevel == "none" {
		isLearningDetailLevelNone = true
	}
	isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
	// CODE EXECUTION MODE: Force learning enabled regardless of step config
	if isCodeExecutionMode && isLearningDisabled {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing learning for orchestration step %d (overriding step config)", stepIndex+1))
		isLearningDisabled = false
	}
	// LOCK LEARNINGS: Check if learnings are locked
	isLearningsLocked := step.OrchestrationStep.AgentConfigs != nil && step.OrchestrationStep.AgentConfigs.LockLearnings != nil && *step.OrchestrationStep.AgentConfigs.LockLearnings
	// TEMP LLM OVERRIDE: Check if learning should be skipped based on which tempLLM was used
	shouldSkipLearningDueToTempOverride := false
	usedTempLLM := "" // Orchestration steps don't use temp LLM, but check for consistency
	if hcpo.executionOptions != nil && usedTempLLM != "" {
		if usedTempLLM == "tempLLM1" && hcpo.executionOptions.SkipLearningWhenTempLLM1 {
			shouldSkipLearningDueToTempOverride = true
		} else if usedTempLLM == "tempLLM2" && hcpo.executionOptions.SkipLearningWhenTempLLM2 {
			shouldSkipLearningDueToTempOverride = true
		}
	}

	// Trigger failure learning if enabled
	if !isFastExecuteStep && !isLearningDisabled && !isLearningsLocked && !shouldSkipLearningDueToTempOverride {
		learningPathIdentifier := getLearningPathIdentifier(orchestrationStepPath)
		totalSteps := len(allSteps)

		// Create a minimal validation response indicating failure for learning purposes
		failureValidationResponse := &ValidationResponse{
			IsSuccessCriteriaMet: false,
			ExecutionStatus:      "failed",
			Reasoning:            fmt.Sprintf("Orchestration step failed: max iterations (%d) reached without meeting success criteria", maxOrchestrationIterations),
			Feedback:             []ValidationFeedback{},
		}

		hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running failure learning analysis for orchestration step %s (max iterations reached)", orchestrationStepPath))
		_, _, err := hcpo.runFailureLearningPhase(ctx, stepIndex, orchestrationStepPath, learningPathIdentifier, totalSteps, step.OrchestrationStep, orchestrationProgress.ConversationHistory, failureValidationResponse, isCodeExecutionMode)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failure learning phase failed for orchestration step %s: %v", orchestrationStepPath, err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Failure learning analysis completed for orchestration step %s", orchestrationStepPath))
		}
	} else {
		if isFastExecuteStep {
			hcpo.GetLogger().Info(fmt.Sprintf("⚡ Fast mode: Skipping learning agents for orchestration step %d", stepIndex+1))
		} else if isLearningDisabled {
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Learning disabled: Skipping learning agents for orchestration step %d", stepIndex+1))
		} else if isLearningsLocked {
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping learning agents for orchestration step %d (using existing learnings)", stepIndex+1))
		} else if shouldSkipLearningDueToTempOverride {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM override: Skipping learning agents for orchestration step %d", stepIndex+1))
		}
	}

	return false, "", fmt.Errorf("orchestration step %d: max iterations (%d) reached without meeting success criteria", stepIndex+1, maxOrchestrationIterations)
}

// executeOrchestrationOrchestratorStep executes the main orchestration step using OrchestrationOrchestratorAgent
// Returns structured OrchestrationResponse with routing decisions and success criteria evaluation
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeOrchestrationOrchestratorStep(
	ctx context.Context,
	step TodoStep,
	stepIndex int,
	stepPath string,
	iteration int,
	previousContextFiles []string,
	orchestrationRoutes []OrchestrationRoute,
	conversationHistory []llmtypes.MessageContent,
	allSteps []TodoStep,
	execCtx *ExecutionContext,
) (*OrchestrationResponse, []llmtypes.MessageContent, error) {
	// Prepare template variables similar to executeSingleStep
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)

	// Determine code execution mode
	var isCodeExecutionMode bool
	if step.AgentConfigs != nil && step.AgentConfigs.UseCodeExecutionMode != nil {
		isCodeExecutionMode = *step.AgentConfigs.UseCodeExecutionMode
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
	}

	stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, stepPath)

	// Build orchestration routes description
	routesDescription := ""
	for i, route := range orchestrationRoutes {
		routesDescription += fmt.Sprintf("\n**Route %d: %s** (ID: %s)\n", i+1, route.RouteName, route.RouteID)
		routesDescription += fmt.Sprintf("- Condition: %s\n", route.Condition)
		if route.ContextToPass != "" {
			routesDescription += fmt.Sprintf("- Context to pass: %s\n", route.ContextToPass)
		}
		routesDescription += fmt.Sprintf("- Sub-agent: %s\n", route.SubAgentStep.Title)
	}

	// Build previous steps summary
	previousStepsSummary := hcpo.buildPreviousStepsSummary(allSteps, stepIndex, previousContextFiles)

	// Prepare template variables
	templateVars := map[string]string{
		"StepTitle":            ResolveVariables(step.Title, hcpo.variableValues),
		"StepDescription":      ResolveVariables(step.Description, hcpo.variableValues),
		"StepSuccessCriteria":  ResolveVariables(step.SuccessCriteria, hcpo.variableValues),
		"StepContextOutput":    ResolveVariables(step.ContextOutput, hcpo.variableValues),
		"WorkspacePath":        executionWorkspacePath,
		"IsCodeExecutionMode":  fmt.Sprintf("%v", isCodeExecutionMode),
		"StepNumber":           stepPath,
		"StepExecutionPath":    stepExecutionPath,
		"PreviousStepsSummary": previousStepsSummary,
		"OrchestrationRoutes":  routesDescription,
	}

	// Add context dependencies
	if len(step.ContextDependencies) > 0 {
		resolvedDeps := ResolveVariablesArray(step.ContextDependencies, hcpo.variableValues)
		templateVars["StepContextDependencies"] = strings.Join(resolvedDeps, ", ")
	} else {
		templateVars["StepContextDependencies"] = ""
	}

	// Add variable names and values
	if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
		templateVars["VariableNames"] = variableNames
	}
	if variableValues := FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues); variableValues != "" {
		templateVars["VariableValues"] = variableValues
	}

	// Get orchestration orchestrator agent
	orchestrationOrchestratorAgent, err := hcpo.getOrchestrationOrchestratorAgentForStep(ctx, step, stepIndex, iteration)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get orchestration orchestrator agent: %w", err)
	}

	// Execute the agent with structured output (includes evaluation and routing decisions)
	orchestrationResponse, updatedConversationHistory, err := orchestrationOrchestratorAgent.ExecuteStructured(ctx, templateVars, conversationHistory)
	if err != nil {
		return nil, nil, fmt.Errorf("orchestration orchestrator agent execution failed: %w", err)
	}

	return orchestrationResponse, updatedConversationHistory, nil
}

// getOrchestrationOrchestratorAgentForStep returns the OrchestrationOrchestratorAgent to use for the main orchestration step
func (hcpo *HumanControlledTodoPlannerOrchestrator) getOrchestrationOrchestratorAgentForStep(ctx context.Context, step TodoStep, stepIndex int, iteration int) (*HumanControlledTodoPlannerOrchestrationOrchestratorAgent, error) {
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("event bridge is required for orchestration orchestrator agent")
	}

	// Determine LLM config: Priority: step config > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()

	if step.AgentConfigs != nil && step.AgentConfigs.ConditionalLLM != nil {
		// Use conditional LLM config for orchestration (similar purpose - structured decision making)
		conditionalLLMConfig := step.AgentConfigs.ConditionalLLM
		llmConfig = &orchestrator.LLMConfig{
			Provider:       conditionalLLMConfig.Provider,
			ModelID:        conditionalLLMConfig.ModelID,
			FallbackModels: []string{},
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific conditional LLM for orchestration orchestrator: %s/%s", conditionalLLMConfig.Provider, conditionalLLMConfig.ModelID))
	} else {
		// Use orchestrator default LLM config
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default orchestration orchestrator LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID))
	}

	// Create agent name
	agentName := fmt.Sprintf("orchestration-orchestrator-step-%d", stepIndex+1)

	// Create orchestration orchestrator agent using factory
	orchestrationOrchestratorAgent, err := hcpo.createOrchestrationOrchestratorAgent(ctx, "orchestration_orchestrator", stepIndex, iteration, agentName, step.AgentConfigs, llmConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestration orchestrator agent: %w", err)
	}

	// Cast to orchestration orchestrator agent type
	orchestrationOrchestratorAgentTyped, ok := orchestrationOrchestratorAgent.(*HumanControlledTodoPlannerOrchestrationOrchestratorAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast agent to orchestration orchestrator agent type")
	}

	return orchestrationOrchestratorAgentTyped, nil
}
