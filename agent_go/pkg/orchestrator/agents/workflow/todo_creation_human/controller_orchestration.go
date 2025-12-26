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

// executeOrchestrationStep executes an orchestration step by:
//  1. Looping until success criteria is met:
//     a. Execute main orchestration step using OrchestrationOrchestratorAgent (with sub-agent output in context if available)
//     - OrchestrationOrchestratorAgent.ExecuteStructured() handles both execution AND evaluation in one step
//     - It evaluates success criteria, selects routes, and provides instructions to sub-agents
//     b. If success criteria met → call validation → return success
//     c. If not met → execute selected sub-agent → loop back
//  2. Return success status and next step ID
//
// Note: The orchestration flow uses OrchestrationOrchestratorAgent which combines execution and evaluation.
// There is no separate "evaluation agent" - evaluation is built into the orchestrator agent.
//
// NOTE: This function works with PlanStepInterface (specifically OrchestrationPlanStep).
// The step type is already validated by the main execution loop before calling this function.
// Returns: (successCriteriaMet bool, nextStepID string, error)
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeOrchestrationStep(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	progress *StepProgress,
	previousContextFiles []string,
	previousExecutionResults []string,
	iteration int,
	execCtx *ExecutionContext,
	allSteps []PlanStepInterface,
) (bool, string, error) {
	// Steps are already PlanStepInterface - no conversion needed

	// Get orchestration step as PlanStepInterface from original step
	orchestrationStepPlan, ok := step.(*OrchestrationPlanStep)
	if !ok {
		return false, "", fmt.Errorf("step is not an OrchestrationPlanStep")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🎯 Executing orchestration step %d: %s", stepIndex+1, step.GetTitle()))

	// Validate orchestration step has required fields
	if orchestrationStepPlan.OrchestrationStep == nil {
		return false, "", fmt.Errorf("orchestration step %d (%s) is missing required orchestration_step field", stepIndex+1, step.GetTitle())
	}
	if len(orchestrationStepPlan.OrchestrationRoutes) == 0 {
		return false, "", fmt.Errorf("orchestration step %d (%s) has no orchestration routes defined", stepIndex+1, step.GetTitle())
	}
	if orchestrationStepPlan.NextStepID == "" {
		return false, "", fmt.Errorf("orchestration step %d (%s) is missing required next_step_id field", stepIndex+1, step.GetTitle())
	}

	// Emit step_started event for orchestration step
	orchestrationStepPath := fmt.Sprintf("step-%d", stepIndex+1)
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, orchestrationStepPath, false)

	// Simplified: Keep conversation history in-memory only (not persisted)
	// Orchestration steps are tracked simply: completed or not (via CompletedStepIndices)
	var conversationHistory []llmtypes.MessageContent

	// Determine max iterations: use step-specific if provided, otherwise use orchestrator default
	maxOrchestrationIterations := hcpo.GetMaxTurns()
	stepConfig := getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
	if stepConfig != nil && stepConfig.OrchestrationMaxIterations != nil {
		maxOrchestrationIterations = *stepConfig.OrchestrationMaxIterations
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific orchestration max iterations: %d (orchestrator default was: %d)", maxOrchestrationIterations, hcpo.GetMaxTurns()))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default orchestration max iterations: %d (no step-specific config)", maxOrchestrationIterations))
	}

	// Main orchestration loop: execute until success criteria is met
	for orchestrationIteration := 0; orchestrationIteration < maxOrchestrationIterations; orchestrationIteration++ {
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

		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing main orchestration step: %s (iteration %d)", orchestrationStepPlan.OrchestrationStep.GetTitle(), orchestrationIteration+1))

		// Apply step config to inner orchestration step (loads UseCodeExecutionMode and other configs from step_config.json)
		// This ensures the inner step's config is loaded before execution
		// Priority: inner step's own config > wrapper step's config > preset default
		innerStepID := orchestrationStepPlan.OrchestrationStep.GetID()
		wrapperStepID := orchestrationStepPlan.GetID()
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 Loading step config for inner orchestration step (ID: %s, wrapper ID: %s)", innerStepID, wrapperStepID))

		// Log available step config IDs for debugging
		stepConfigs, err := hcpo.ReadStepConfigs(ctx)
		if err == nil && len(stepConfigs) > 0 {
			configIDs := make([]string, 0, len(stepConfigs))
			for _, config := range stepConfigs {
				if config.ID != "" {
					configIDs = append(configIDs, config.ID)
				}
			}
			hcpo.GetLogger().Info(fmt.Sprintf("📋 Available step config IDs in step_config.json: %v", configIDs))
		}

		// First, try to load inner step's own config
		if err := ApplyStepConfigFromFile(ctx, orchestrationStepPlan.OrchestrationStep, hcpo); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load step config for inner orchestration step '%s' (ID: %s): %v", orchestrationStepPlan.OrchestrationStep.GetTitle(), innerStepID, err))
		}

		// Check if inner step config was applied
		innerStepConfig := getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
		if innerStepConfig == nil || innerStepConfig.UseCodeExecutionMode == nil {
			// Inner step config not found or UseCodeExecutionMode not set - try to inherit from wrapper step config
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Inner step config not found or UseCodeExecutionMode not set (ID: %s) - checking wrapper step config (ID: %s)", innerStepID, wrapperStepID))

			// Try to load wrapper step config and inherit UseCodeExecutionMode
			wrapperConfig := getAgentConfigs(orchestrationStepPlan)
			if wrapperConfig != nil && wrapperConfig.UseCodeExecutionMode != nil {
				// Initialize inner step config if needed
				if innerStepConfig == nil {
					switch s := orchestrationStepPlan.OrchestrationStep.(type) {
					case *RegularPlanStep:
						s.AgentConfigs = &AgentConfigs{}
					case *ConditionalPlanStep:
						s.AgentConfigs = &AgentConfigs{}
					case *DecisionPlanStep:
						s.AgentConfigs = &AgentConfigs{}
					case *OrchestrationPlanStep:
						s.AgentConfigs = &AgentConfigs{}
					}
					innerStepConfig = getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
				}
				// Inherit UseCodeExecutionMode from wrapper step
				innerStepConfig.UseCodeExecutionMode = wrapperConfig.UseCodeExecutionMode
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Inherited UseCodeExecutionMode from wrapper step (ID: %s) to inner step (ID: %s): %v", wrapperStepID, innerStepID, *wrapperConfig.UseCodeExecutionMode))
			} else {
				// Try to load wrapper step config from file if not already loaded
				if err := ApplyStepConfigFromFile(ctx, orchestrationStepPlan, hcpo); err == nil {
					wrapperConfig = getAgentConfigs(orchestrationStepPlan)
					if wrapperConfig != nil && wrapperConfig.UseCodeExecutionMode != nil {
						// Initialize inner step config if needed
						if innerStepConfig == nil {
							switch s := orchestrationStepPlan.OrchestrationStep.(type) {
							case *RegularPlanStep:
								s.AgentConfigs = &AgentConfigs{}
							case *ConditionalPlanStep:
								s.AgentConfigs = &AgentConfigs{}
							case *DecisionPlanStep:
								s.AgentConfigs = &AgentConfigs{}
							case *OrchestrationPlanStep:
								s.AgentConfigs = &AgentConfigs{}
							}
							innerStepConfig = getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
						}
						// Inherit UseCodeExecutionMode from wrapper step
						innerStepConfig.UseCodeExecutionMode = wrapperConfig.UseCodeExecutionMode
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Loaded and inherited UseCodeExecutionMode from wrapper step (ID: %s) to inner step (ID: %s): %v", wrapperStepID, innerStepID, *wrapperConfig.UseCodeExecutionMode))
					}
				}
			}
		}

		// Final check - log the result
		innerStepConfig = getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
		if innerStepConfig != nil && innerStepConfig.UseCodeExecutionMode != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Step config loaded for inner orchestration step (ID: %s) - use_code_execution_mode: %v", innerStepID, *innerStepConfig.UseCodeExecutionMode))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Step config not found or UseCodeExecutionMode not set for inner orchestration step (ID: %s) - will use preset default", innerStepID))
		}

		// Execute using OrchestrationOrchestratorAgent with structured output
		// OrchestrationOrchestratorAgent.ExecuteStructured() handles both execution and evaluation in one step
		// Convert OrchestrationRoutes to OrchestrationRoute format
		orchestrationRoutes := make([]OrchestrationRoute, len(orchestrationStepPlan.OrchestrationRoutes))
		for i, route := range orchestrationStepPlan.OrchestrationRoutes {
			// Use PlanStepInterface directly (no conversion needed)
			orchestrationRoutes[i] = OrchestrationRoute{
				RouteID:       route.RouteID,
				RouteName:     route.RouteName,
				Condition:     route.Condition,
				ContextToPass: route.ContextToPass,
				SubAgentStep:  route.SubAgentStep,
			}
		}
		orchestrationResponse, updatedConversationHistory, err := hcpo.executeOrchestrationOrchestratorStep(
			ctx,
			orchestrationStepPlan.OrchestrationStep,
			stepIndex,
			mainStepPath,
			iteration,
			orchestrationContext,
			previousExecutionResults,
			orchestrationRoutes,
			conversationHistory,
			allSteps,
			execCtx,
		)
		if err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute main orchestration step '%s': %v", orchestrationStepPlan.OrchestrationStep.GetTitle(), err), nil)
			return false, "", fmt.Errorf("failed to execute main orchestration step '%s': %w", orchestrationStepPlan.OrchestrationStep.GetTitle(), err)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Main orchestration step completed. Success criteria met: %t, Selected route: %s", orchestrationResponse.SuccessCriteriaMet, orchestrationResponse.SelectedRouteID))

		// Update conversation history (in-memory only, not persisted)
		conversationHistory = updatedConversationHistory

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
				"orchestration_step_id":  step.GetID(),
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
		if orchestrationStepPlan, ok := step.(*OrchestrationPlanStep); ok {
			orchestrationStepPlan.OrchestrationResponse = orchestrationResponse
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Orchestration step evaluated: success_criteria_met=%t, selected_route_id=%s",
			orchestrationResponse.SuccessCriteriaMet, orchestrationResponse.SelectedRouteID))

		// Note: Orchestration step progress is simplified - we only track completion via CompletedStepIndices
		// No need to save intermediate progress during orchestration loop

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
				"orchestration_step_id": step.GetID(),
				"iteration":             orchestrationIteration + 1,
				"selected_route_id":     orchestrationResponse.SelectedRouteID,
				"success_criteria_met":  orchestrationResponse.SuccessCriteriaMet,
				"success_reasoning":     orchestrationResponse.SuccessReasoning,
				"next_step_id":          orchestrationStepPlan.NextStepID,
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

			// Success criteria met - call validation to verify
			hcpo.GetLogger().Info("🔍 Success criteria met, calling validation to verify")

			// Prepare validation template variables
			var validationWorkspacePath string
			if hcpo.selectedRunFolder != "" {
				validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
			} else {
				validationWorkspacePath = hcpo.GetWorkspacePath()
			}

			validationTemplateVars := map[string]string{
				"StepTitle":           orchestrationStepPlan.OrchestrationStep.GetTitle(),
				"StepDescription":     orchestrationStepPlan.OrchestrationStep.GetDescription(),
				"StepSuccessCriteria": orchestrationStepPlan.OrchestrationStep.GetSuccessCriteria(),
				"StepContextOutput":   orchestrationStepPlan.OrchestrationStep.GetContextOutput().String(),
				"WorkspacePath":       validationWorkspacePath,
				"ExecutionHistory":    shared.FormatConversationHistory(conversationHistory),
			}

			// Add context dependencies
			contextDeps := orchestrationStepPlan.OrchestrationStep.GetContextDependencies()
			if len(contextDeps) > 0 {
				validationTemplateVars["StepContextDependencies"] = strings.Join(contextDeps, ", ")
			} else {
				validationTemplateVars["StepContextDependencies"] = ""
			}

			// No loop condition for orchestration steps
			validationTemplateVars["LoopCondition"] = ""
			validationTemplateVars["DecisionReasoning"] = ""

			// Skip pre-validation for orchestration steps - orchestration steps don't produce files themselves,
			// they orchestrate other steps. Pre-validation is only relevant for execution steps that produce output files.
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping pre-validation for orchestration step %d (orchestration steps don't produce files)", stepIndex+1))

			// Create empty pre-validation results (always pass) for template variables
			workspaceResults := &WorkspaceVerificationResult{
				OverallPass:  true, // Always pass for orchestration steps
				FilesChecked: []FileCheckResult{},
				Summary: ValidationSummary{
					TotalChecks:    0,
					PassedChecks:   0,
					FailedChecks:   0,
					SchemaErrors:   0,
					Errors:         []ValidationError{},
					SchemaWarnings: []ValidationError{},
				},
			}

			// Format pre-validation results and add to template variables (empty for orchestration steps)
			validationTemplateVars["WorkspaceVerificationResults"] = formatWorkspaceResults(workspaceResults)

			// Emit pre-validation completed event (with empty results for orchestration steps)
			hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, orchestrationStepPath, false, workspaceResults)

			var validationResponse *ValidationResponse

			// Check if we should skip LLM validation
			innerStepConfigs := getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
			skipLLMValidation := innerStepConfigs != nil && innerStepConfigs.SkipLLMValidationIfPreValidationPasses != nil && *innerStepConfigs.SkipLLMValidationIfPreValidationPasses

			if skipLLMValidation {
				// Skip LLM validation and assume validation success
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Orchestration step %d - skipping LLM validation (configured to skip)", stepIndex+1))
				validationResponse = &ValidationResponse{
					IsSuccessCriteriaMet: true,
					ExecutionStatus:      "COMPLETED",
					Reasoning:            "Orchestration step validation skipped (configured to skip LLM validation).",
					Feedback:             []ValidationFeedback{},
				}
			} else {
				// Proceed to LLM validation (pre-validation is skipped for orchestration steps)
				// Create validation agent
				validationAgentName := fmt.Sprintf("orchestration-validation-step-%d", stepIndex+1)
				validationAgent, err := hcpo.createValidationAgent(ctx, "validation", stepIndex+1, validationAgentName, innerStepConfigs)
				if err != nil {
					hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to create validation agent for orchestration step %d: %v", stepIndex+1, err), nil)
					return false, "", fmt.Errorf("failed to create validation agent for orchestration step: %w", err)
				}

				// Call validation
				validationResponse, _, err = validationAgent.(*HumanControlledTodoPlannerValidationAgent).ExecuteStructured(ctx, validationTemplateVars, []llmtypes.MessageContent{})
				if err != nil {
					hcpo.GetLogger().Error(fmt.Sprintf("❌ Validation failed for orchestration step %d: %v", stepIndex+1, err), nil)
					return false, "", fmt.Errorf("validation failed for orchestration step: %w", err)
				}
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
			// Add validation response to conversation history (in-memory only)
			conversationHistory = append(conversationHistory, validationMessage)

			// Check if validation verified success
			if validationResponse.IsSuccessCriteriaMet {
				hcpo.GetLogger().Info("✅ Validation verified success criteria - proceeding to next step")
				// Note: Orchestration step progress is simplified - completion tracked via CompletedStepIndices only

				// Determine code execution mode using helper method
				orchestrationStepConfig := getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
				isCodeExecutionMode := hcpo.getCodeExecutionMode(orchestrationStepConfig)

				// Check learning flags (similar to regular steps)
				isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
				isLearningDisabledStep := orchestrationStepConfig != nil && orchestrationStepConfig.DisableLearning != nil && *orchestrationStepConfig.DisableLearning
				isLearningDetailLevelNone := false
				if orchestrationStepConfig != nil && orchestrationStepConfig.LearningDetailLevel == "none" {
					isLearningDetailLevelNone = true
				}
				isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
				// CODE EXECUTION MODE: Force learning enabled regardless of step config
				if isCodeExecutionMode && isLearningDisabled {
					hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing learning for orchestration step %d (overriding step config)", stepIndex+1))
					isLearningDisabled = false
				}
				// LOCK LEARNINGS: Check if learnings are locked
				// EXCEPTION: If learnings are locked but learnings don't exist, still run learning to create initial learnings
				orchestrationStepID := orchestrationStepPlan.OrchestrationStep.GetID()
				shouldSkipLearningDueToLock, _ := hcpo.ShouldSkipLearningDueToLock(ctx, orchestrationStepPlan.OrchestrationStep, orchestrationStepID, stepIndex, orchestrationStepPath)
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
				if !isFastExecuteStep && !isLearningDisabled && !shouldSkipLearningDueToLock && !shouldSkipLearningDueToTempOverride {
					learningPathIdentifier := getLearningPathIdentifier(orchestrationStepPlan.OrchestrationStep.GetID(), orchestrationStepPath)
					totalSteps := len(allSteps)
					hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running success learning analysis for orchestration step %s", orchestrationStepPath))
					err = hcpo.runSuccessLearningPhase(ctx, stepIndex, orchestrationStepPath, learningPathIdentifier, totalSteps, orchestrationStepPlan.OrchestrationStep, conversationHistory, validationResponse, isCodeExecutionMode, "") // Orchestration steps don't use tempLLM
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
					} else if shouldSkipLearningDueToLock {
						hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping learning agents for orchestration step %d (using existing learnings)", stepIndex+1))
					} else if shouldSkipLearningDueToTempOverride {
						hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM override: Skipping learning agents for orchestration step %d", stepIndex+1))
					}
				}

				// Emit orchestration_finished event
				// TODO: Add orchestration_finished event emission

				// Emit step_finished event
				hcpo.emitStepFinishedEvent(ctx, step, stepIndex, orchestrationStepPath, false)

				// Return success
				return true, orchestrationStepPlan.NextStepID, nil
			}

			// Validation did not confirm success - trigger failure learning, then restart orchestrator from the beginning with validation feedback
			hcpo.GetLogger().Info("⚠️ Validation did not confirm success - triggering failure learning, then restarting orchestrator from beginning with validation feedback")

			// Note: Validation message was already added to conversation history (in-memory only)
			// No need to save progress - orchestration steps are tracked simply via CompletedStepIndices

			// Trigger failure learning if enabled (even when validation fails)
			// Determine code execution mode using helper method
			orchestrationStepConfig := getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
			isCodeExecutionMode := hcpo.getCodeExecutionMode(orchestrationStepConfig)

			// Check learning flags (similar to success learning)
			isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
			isLearningDisabledStep := orchestrationStepConfig != nil && orchestrationStepConfig.DisableLearning != nil && *orchestrationStepConfig.DisableLearning
			isLearningDetailLevelNone := false
			if orchestrationStepConfig != nil && orchestrationStepConfig.LearningDetailLevel == "none" {
				isLearningDetailLevelNone = true
			}
			isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
			// CODE EXECUTION MODE: Force learning enabled regardless of step config
			if isCodeExecutionMode && isLearningDisabled {
				hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing learning for orchestration step %d (overriding step config)", stepIndex+1))
				isLearningDisabled = false
			}
			// LOCK LEARNINGS: Check if learnings are locked
			// EXCEPTION: If learnings are locked but learnings don't exist, still run learning to create initial learnings
			orchestrationStepID := orchestrationStepPlan.OrchestrationStep.GetID()
			shouldSkipLearningDueToLock, _ := hcpo.ShouldSkipLearningDueToLock(ctx, orchestrationStepPlan.OrchestrationStep, orchestrationStepID, stepIndex, orchestrationStepPath)
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
			if !isFastExecuteStep && !isLearningDisabled && !shouldSkipLearningDueToLock && !shouldSkipLearningDueToTempOverride {
				learningPathIdentifier := getLearningPathIdentifier(orchestrationStepPlan.OrchestrationStep.GetID(), orchestrationStepPath)
				totalSteps := len(allSteps)
				hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running failure learning analysis for orchestration step %s (validation failed)", orchestrationStepPath))
				_, _, err = hcpo.runFailureLearningPhase(ctx, stepIndex, orchestrationStepPath, learningPathIdentifier, totalSteps, orchestrationStepPlan.OrchestrationStep, conversationHistory, validationResponse, isCodeExecutionMode)
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
				} else if shouldSkipLearningDueToLock {
					hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping learning agents for orchestration step %d (using existing learnings)", stepIndex+1))
				} else if shouldSkipLearningDueToTempOverride {
					hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM override: Skipping learning agents for orchestration step %d", stepIndex+1))
				}
			}

			// Loop back to start of orchestration step (increment iteration)
			hcpo.GetLogger().Info(fmt.Sprintf("🔄 Restarting orchestration step from beginning (iteration %d/%d) with validation feedback", orchestrationIteration+2, maxOrchestrationIterations))
			continue // Loop back to start of for loop
		}

		// 4. Success criteria not met - either continue working yourself, delegate to sub-agent, or end workflow
		if orchestrationResponse.SelectedRouteID == "" {
			// Orchestrator is continuing to work itself - loop back to continue in next iteration
			hcpo.GetLogger().Info(fmt.Sprintf("🔄 Orchestrator continuing to work itself (iteration %d/%d) - no sub-agent needed", orchestrationIteration+2, maxOrchestrationIterations))
			// Update progress and continue loop
			// Note: Orchestration step progress is simplified - no need to save intermediate state
			// Continue loop to next iteration
			continue
		}

		// Find the selected route and capture its index
		var selectedRoutePlan *PlanOrchestrationRoute
		var subAgentStepPlan PlanStepInterface
		subAgentIndex := 0 // Will be set when route is found
		for i := range orchestrationStepPlan.OrchestrationRoutes {
			if orchestrationStepPlan.OrchestrationRoutes[i].RouteID == orchestrationResponse.SelectedRouteID {
				selectedRoutePlan = &orchestrationStepPlan.OrchestrationRoutes[i]
				subAgentStepPlan = selectedRoutePlan.SubAgentStep
				subAgentIndex = i + 1 // Use 1-based index for path (route 0 -> sub-agent-1, route 1 -> sub-agent-2, etc.)
				break
			}
		}

		if selectedRoutePlan == nil {
			return false, "", fmt.Errorf("orchestration step %d: selected route ID '%s' not found in orchestration routes", stepIndex+1, orchestrationResponse.SelectedRouteID)
		}

		// Check if the selected route is "end" - terminate workflow immediately
		if strings.ToLower(selectedRoutePlan.RouteID) == "end" {
			hcpo.GetLogger().Info("🏁 Orchestrator chose to end workflow (route: 'end') - terminating workflow early")
			// Note: Orchestration step completion will be tracked via CompletedStepIndices when marked as completed
			// Return success with "end" to terminate workflow
			return true, "end", nil
		}

		hcpo.GetLogger().Info(fmt.Sprintf("🔀 Executing sub-agent: %s (route: %s, index: %d)", subAgentStepPlan.GetTitle(), selectedRoutePlan.RouteID, subAgentIndex))

		// Prepare sub-agent step with validation disabled
		// Apply step config directly to PlanStepInterface
		_ = ApplyStepConfigFromFile(ctx, subAgentStepPlan, hcpo)
		// Ignore error - use defaults if config loading fails

		// Ensure AgentConfigs exists on the sub-agent step
		subAgentConfigs := getAgentConfigs(subAgentStepPlan)
		if subAgentConfigs == nil {
			// Set AgentConfigs on the step if it doesn't exist
			switch subAgentStep := subAgentStepPlan.(type) {
			case *RegularPlanStep:
				subAgentStep.AgentConfigs = &AgentConfigs{}
			case *ConditionalPlanStep:
				subAgentStep.AgentConfigs = &AgentConfigs{}
			case *DecisionPlanStep:
				subAgentStep.AgentConfigs = &AgentConfigs{}
			case *OrchestrationPlanStep:
				subAgentStep.AgentConfigs = &AgentConfigs{}
			}
		}
		// Get AgentConfigs again after ensuring it exists
		subAgentConfigs = getAgentConfigs(subAgentStepPlan)
		val := true
		subAgentConfigs.DisableValidation = &val

		// Match sub-agent's own step config from step_config.json (mandatory - no inheritance from parent)
		_ = ApplyStepConfigFromFile(ctx, subAgentStepPlan, hcpo)
		// Ignore error - use defaults if config loading fails
		// Preserve DisableValidation = true (already set above)

		// Sub-agents don't receive previous steps history - they work independently based on orchestrator instructions

		// Modify sub-agent step with orchestrator-provided instructions, success criteria, and context settings
		// Update the PlanStepInterface version for execution
		if regularStep := getRegularPlanStep(subAgentStepPlan); regularStep != nil {
			if orchestrationResponse.InstructionsToSubAgent != "" {
				// Append orchestrator instructions to original description (preserve plan context)
				originalDescription := regularStep.Description
				if originalDescription != "" {
					regularStep.Description = fmt.Sprintf("%s\n\n## Orchestrator Instructions\n\n%s", originalDescription, orchestrationResponse.InstructionsToSubAgent)
				} else {
					regularStep.Description = orchestrationResponse.InstructionsToSubAgent
				}
				hcpo.GetLogger().Info("📝 Appending orchestrator-provided instructions to sub-agent step description")
			}
			if orchestrationResponse.SuccessCriteriaForSubAgent != "" {
				regularStep.SuccessCriteria = orchestrationResponse.SuccessCriteriaForSubAgent
				hcpo.GetLogger().Info("📝 Using orchestrator-provided success criteria for sub-agent (replacing step success criteria)")
			}
			if orchestrationResponse.ContextDependenciesForSubAgent != "" {
				// Parse comma-separated context dependencies into array
				deps := strings.Split(orchestrationResponse.ContextDependenciesForSubAgent, ",")
				regularStep.ContextDependencies = make([]string, 0, len(deps))
				for _, dep := range deps {
					dep = strings.TrimSpace(dep)
					if dep != "" {
						regularStep.ContextDependencies = append(regularStep.ContextDependencies, dep)
					}
				}
				hcpo.GetLogger().Info(fmt.Sprintf("📝 Using orchestrator-provided context dependencies for sub-agent (replacing step context dependencies): %v", regularStep.ContextDependencies))
			}
			if orchestrationResponse.ContextOutputForSubAgent != "" {
				regularStep.ContextOutput = FlexibleContextOutput(orchestrationResponse.ContextOutputForSubAgent)
				hcpo.GetLogger().Info(fmt.Sprintf("📝 Using orchestrator-provided context output for sub-agent (replacing step context output): %s", orchestrationResponse.ContextOutputForSubAgent))
			}
		}

		// Execute sub-agent (without previous steps history - sub-agents don't need it)
		// Use format: step-{N}-sub-agent-{index} (e.g., "step-2-sub-agent-1")
		// Index is derived from the route's position in the orchestration routes array (1-based)
		subAgentPath := fmt.Sprintf("step-%d-sub-agent-%d", stepIndex+1, subAgentIndex)
		// Pass empty previousContextFiles to skip building previous steps summary for sub-agents
		// Convert orchestration routes to OrchestrationRoute format for sub-agent context
		orchestrationRoutesForSubAgent := make([]OrchestrationRoute, len(orchestrationStepPlan.OrchestrationRoutes))
		for i, route := range orchestrationStepPlan.OrchestrationRoutes {
			orchestrationRoutesForSubAgent[i] = OrchestrationRoute{
				RouteID:       route.RouteID,
				RouteName:     route.RouteName,
				Condition:     route.Condition,
				ContextToPass: route.ContextToPass,
				SubAgentStep:  route.SubAgentStep,
			}
		}
		subAgentExecutionResult, updatedSubAgentContextFiles, err := hcpo.executeSingleStep(
			ctx,
			subAgentStepPlan, // Use PlanStepInterface for executeSingleStep
			stepIndex,        // Use parent step index
			subAgentPath,
			1, // totalSteps = 1 for single sub-agent
			iteration,
			[]string{}, // Empty - sub-agents don't need previous steps history
			progress,
			true, // isBranchStep = true (sub-agent is like a branch step)
			execCtx,
			allSteps,
			false,                          // isDecisionInnerStep = false
			nil,                            // decisionContext = nil
			"",                             // decisionEvaluationQuestion - empty
			true,                           // isSubAgent = true (sub-agents never request human feedback)
			[]string{},                     // previousExecutionResults - empty for sub-agents (they don't need previous execution outputs)
			orchestrationRoutesForSubAgent, // Pass orchestration routes so sub-agent knows about other agents
		)
		if err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to execute sub-agent '%s': %v", subAgentStepPlan.GetTitle(), err), nil)
			return false, "", fmt.Errorf("failed to execute sub-agent '%s': %w", subAgentStepPlan.GetTitle(), err)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Sub-agent completed. Output length: %d chars", len(subAgentExecutionResult)))

		// Run pre-validation on sub-agent output if validation schema exists
		validationSchema := getValidationSchema(subAgentStepPlan)
		if validationSchema != nil && len(validationSchema.Files) > 0 {
			// Construct execution workspace path for sub-agent
			runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
			executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
			subAgentExecutionPath := getExecutionFolderPath(executionWorkspacePath, subAgentPath)

			hcpo.GetLogger().Info(fmt.Sprintf("🔍 Running pre-validation on sub-agent output (path: %s)", subAgentExecutionPath))
			workspaceResults, err := RunPreValidation(ctx, validationSchema, subAgentExecutionPath, hcpo.BaseOrchestrator)
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Pre-validation error for sub-agent '%s': %v", subAgentStepPlan.GetTitle(), err))
			} else if workspaceResults.OverallPass {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Pre-validation passed for sub-agent '%s'", subAgentStepPlan.GetTitle()))
			} else {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Pre-validation failed for sub-agent '%s' - %d checks failed", subAgentStepPlan.GetTitle(), workspaceResults.Summary.FailedChecks))
				// Log validation errors for debugging
				for _, validationError := range workspaceResults.Summary.Errors {
					hcpo.GetLogger().Warn(fmt.Sprintf("  - %s: %s (expected: %s, actual: %s)", validationError.File, validationError.Message, validationError.Expected, validationError.Actual))
				}
			}
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping pre-validation for sub-agent '%s' (no validation schema provided)", subAgentStepPlan.GetTitle()))
		}

		// Add sub-agent output to conversation history as an assistant message (in-memory only)
		// This makes it feel like a continuous conversation for the main agent
		subAgentMessage := llmtypes.MessageContent{
			Role: llmtypes.ChatMessageTypeAI,
			Parts: []llmtypes.ContentPart{
				llmtypes.TextContent{
					Text: fmt.Sprintf("Sub-agent (%s) completed with the following output:\n\n%s", selectedRoutePlan.RouteName, subAgentExecutionResult),
				},
			},
		}
		conversationHistory = append(conversationHistory, subAgentMessage)

		// Note: Orchestration step progress is simplified - no need to save intermediate state

		// Update context files for next iteration
		_ = updatedSubAgentContextFiles

		// Loop back to execute main orchestration step again with sub-agent output in context
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Sub-agent completed, looping back to main orchestration step (iteration %d/%d)", orchestrationIteration+2, maxOrchestrationIterations))
	}

	// Max iterations reached without success
	// Trigger failure learning before returning error
	// Determine code execution mode using helper method
	orchestrationStepConfig := getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
	isCodeExecutionMode := hcpo.getCodeExecutionMode(orchestrationStepConfig)

	// Check learning flags (similar to regular steps)
	isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
	isLearningDisabledStep := orchestrationStepConfig != nil && orchestrationStepConfig.DisableLearning != nil && *orchestrationStepConfig.DisableLearning
	isLearningDetailLevelNone := false
	if orchestrationStepConfig != nil && orchestrationStepConfig.LearningDetailLevel == "none" {
		isLearningDetailLevelNone = true
	}
	isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
	// CODE EXECUTION MODE: Force learning enabled regardless of step config
	if isCodeExecutionMode && isLearningDisabled {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing learning for orchestration step %d (overriding step config)", stepIndex+1))
		isLearningDisabled = false
	}
	// LOCK LEARNINGS: Check if learnings are locked
	// EXCEPTION: If learnings are locked but learnings don't exist, still run learning to create initial learnings
	orchestrationStepID := orchestrationStepPlan.OrchestrationStep.GetID()
	shouldSkipLearningDueToLock, _ := hcpo.ShouldSkipLearningDueToLock(ctx, orchestrationStepPlan.OrchestrationStep, orchestrationStepID, stepIndex, orchestrationStepPath)
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
	if !isFastExecuteStep && !isLearningDisabled && !shouldSkipLearningDueToLock && !shouldSkipLearningDueToTempOverride {
		learningPathIdentifier := getLearningPathIdentifier(orchestrationStepPlan.OrchestrationStep.GetID(), orchestrationStepPath)
		totalSteps := len(allSteps)

		// Create a minimal validation response indicating failure for learning purposes
		failureValidationResponse := &ValidationResponse{
			IsSuccessCriteriaMet: false,
			ExecutionStatus:      "failed",
			Reasoning:            fmt.Sprintf("Orchestration step failed: max iterations (%d) reached without meeting success criteria", maxOrchestrationIterations),
			Feedback:             []ValidationFeedback{},
		}

		hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running failure learning analysis for orchestration step %s (max iterations reached)", orchestrationStepPath))
		_, _, err := hcpo.runFailureLearningPhase(ctx, stepIndex, orchestrationStepPath, learningPathIdentifier, totalSteps, orchestrationStepPlan.OrchestrationStep, conversationHistory, failureValidationResponse, isCodeExecutionMode)
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
		} else if shouldSkipLearningDueToLock {
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
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	iteration int,
	previousContextFiles []string,
	previousExecutionResults []string,
	orchestrationRoutes []OrchestrationRoute,
	conversationHistory []llmtypes.MessageContent,
	allSteps []PlanStepInterface,
	execCtx *ExecutionContext,
) (*OrchestrationResponse, []llmtypes.MessageContent, error) {
	// Prepare template variables similar to executeSingleStep
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)

	// Determine code execution mode using helper method
	// Note: This function receives the inner OrchestrationStep (which is a PlanStepInterface itself),
	// not the wrapper step. So the config is in step.AgentConfigs, not step.OrchestrationStep.AgentConfigs
	var orchestrationStepConfig *AgentConfigs
	orchestrationStepPlan, isOrchestrationWrapper := step.(*OrchestrationPlanStep)
	if isOrchestrationWrapper && orchestrationStepPlan.OrchestrationStep != nil {
		// This is the wrapper step - get config from inner step
		orchestrationStepConfig = getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
	} else {
		// This is the inner step itself - get config directly from step
		orchestrationStepConfig = getAgentConfigs(step)
	}
	isCodeExecutionMode := hcpo.getCodeExecutionMode(orchestrationStepConfig)

	stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, stepPath)

	// Build orchestration routes description
	routesDescription := ""
	for i, route := range orchestrationRoutes {
		routesDescription += fmt.Sprintf("\n**Route %d: %s** (ID: %s)\n", i+1, route.RouteName, route.RouteID)
		routesDescription += fmt.Sprintf("- Condition: %s\n", route.Condition)
		if route.ContextToPass != "" {
			routesDescription += fmt.Sprintf("- Context to pass: %s\n", route.ContextToPass)
		}
		routesDescription += fmt.Sprintf("- Sub-agent: %s\n", route.SubAgentStep.GetTitle())
	}

	// Build previous steps summary using shared function (includes descriptions, output files, and execution results)
	previousStepsSummary := hcpo.buildPreviousStepsSummary(allSteps, stepIndex, previousContextFiles, previousExecutionResults)
	if previousStepsSummary != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Added previous steps summary to template variables for orchestration step %d (%d previous steps)", stepIndex+1, len(previousContextFiles)))
	}

	// Prepare template variables
	templateVars := map[string]string{
		"StepTitle":            ResolveVariables(step.GetTitle(), hcpo.variableValues),
		"StepDescription":      ResolveVariables(step.GetDescription(), hcpo.variableValues),
		"StepSuccessCriteria":  ResolveVariables(step.GetSuccessCriteria(), hcpo.variableValues),
		"StepContextOutput":    ResolveVariables(step.GetContextOutput().String(), hcpo.variableValues),
		"WorkspacePath":        executionWorkspacePath,
		"IsCodeExecutionMode":  fmt.Sprintf("%v", isCodeExecutionMode),
		"StepNumber":           stepPath,
		"StepExecutionPath":    stepExecutionPath,
		"PreviousStepsSummary": previousStepsSummary,
		"OrchestrationRoutes":  routesDescription,
	}

	// Add context dependencies
	contextDeps := step.GetContextDependencies()
	if len(contextDeps) > 0 {
		resolvedDeps := ResolveVariablesArray(contextDeps, hcpo.variableValues)
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

	// Set folder guard paths: allow reads from learnings and execution, writes only to current step folder
	baseWorkspacePath := hcpo.GetWorkspacePath()
	learningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)
	// READ: learnings folder + execution folder (to read previous step results)
	// WRITE: only the specific step folder (execution/step-{X}-orchestration/ or execution/step-{X}-orchestration-{N}/)
	readPaths := []string{learningsPath, executionWorkspacePath}
	writePaths := []string{stepExecutionPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for orchestration orchestrator agent - Read paths: %v, Write paths: %v (can read learnings/ and execution/, can only write to %s)", readPaths, writePaths, stepPath))

	// Get orchestration orchestrator agent with tempLLM logic
	// Determine retryAttempt from orchestrationIteration (1-based: first iteration = 1, second = 2, etc.)
	retryAttempt := iteration + 1

	// Determine if this is a retry after validation failure by checking conversation history
	isRetryAfterValidationFailure := false
	for _, msg := range conversationHistory {
		// Check message content for validation feedback
		for _, part := range msg.Parts {
			// Extract text from TextContent type
			if textContent, ok := part.(llmtypes.TextContent); ok {
				if strings.Contains(textContent.Text, "Validation agent completed") {
					isRetryAfterValidationFailure = true
					break
				}
			}
		}
		if isRetryAfterValidationFailure {
			break
		}
	}

	orchestrationOrchestratorAgent, err := hcpo.getOrchestrationOrchestratorAgentForStep(ctx, step, stepIndex, iteration, retryAttempt, isRetryAfterValidationFailure, allSteps)
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
// Includes tempLLM logic similar to execution agents
func (hcpo *HumanControlledTodoPlannerOrchestrator) getOrchestrationOrchestratorAgentForStep(ctx context.Context, step PlanStepInterface, stepIndex int, iteration int, retryAttempt int, isRetryAfterValidationFailure bool, allSteps []PlanStepInterface) (*HumanControlledTodoPlannerOrchestrationOrchestratorAgent, error) {
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("event bridge is required for orchestration orchestrator agent")
	}

	// Get orchestration step config
	// Note: This function receives the inner OrchestrationStep (which is a PlanStepInterface itself),
	// not the wrapper step. So the config is in step.GetAgentConfigs(), not step.OrchestrationStep.GetAgentConfigs()
	var orchestrationStepConfig *AgentConfigs
	orchestrationStepPlan, isOrchestrationWrapper := step.(*OrchestrationPlanStep)
	if isOrchestrationWrapper && orchestrationStepPlan.OrchestrationStep != nil {
		// This is the wrapper step - get config from inner step
		orchestrationStepConfig = getAgentConfigs(orchestrationStepPlan.OrchestrationStep)
		innerStepID := orchestrationStepPlan.OrchestrationStep.GetID()
		if orchestrationStepConfig != nil && orchestrationStepConfig.UseCodeExecutionMode != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Orchestration step config found (from wrapper) - UseCodeExecutionMode: %v (step ID: %s)", *orchestrationStepConfig.UseCodeExecutionMode, innerStepID))
		} else if orchestrationStepConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Orchestration step config found (from wrapper) but UseCodeExecutionMode is nil (step ID: %s)", innerStepID))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Orchestration step config is nil (from wrapper) (step ID: %s)", innerStepID))
		}
	} else {
		// This is the inner step itself - get config directly from step.GetAgentConfigs()
		orchestrationStepConfig = getAgentConfigs(step)
		stepID := step.GetID()
		if orchestrationStepConfig != nil && orchestrationStepConfig.UseCodeExecutionMode != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Orchestration step config found (from inner step) - UseCodeExecutionMode: %v (step ID: %s)", *orchestrationStepConfig.UseCodeExecutionMode, stepID))
		} else if orchestrationStepConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Orchestration step config found (from inner step) but UseCodeExecutionMode is nil (step ID: %s)", stepID))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Orchestration step config is nil (from inner step) (step ID: %s)", stepID))
		}
	}

	// Get step ID for orchestration learnings:
	// Orchestration learnings are stored using the inner orchestration step ID.
	// Note: This function receives the inner OrchestrationStep (which is a PlanStepInterface itself),
	// not the wrapper step. So we use step.GetID() directly.
	stepID := ""
	if isOrchestrationWrapper && orchestrationStepPlan.OrchestrationStep != nil {
		// This is the wrapper step - get ID from inner step
		stepID = orchestrationStepPlan.OrchestrationStep.GetID()
	} else {
		// This is the inner step itself - use step.GetID() directly
		stepID = step.GetID()
	}

	// If inner step ID is not available, use stepPath as fallback (never fall back to parent wrapper ID)
	stepPath := fmt.Sprintf("step-%d", stepIndex+1)
	if stepID == "" {
		stepID = stepPath // Fallback: use stepPath as identifier
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Could not determine inner orchestration step ID for step %d, using stepPath as fallback", stepIndex+1))
	}

	// Check if learnings folder has files
	learningsFolderEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepID, stepIndex, stepPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check if orchestration step %s learnings folder is empty: %v, assuming empty (will skip tempLLM)", stepID, err))
		learningsFolderEmpty = true // Conservative: assume empty on error, skip tempLLM
	}

	// Use selectExecutionLLM helper for cascading fallback: tempLLM1 → tempLLM2 → step ExecutionLLM → preset ExecutionLLM → orchestrator default
	// This handles all the tempLLM logic, learnings folder checks, and retry attempt logic
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	llmConfig := hcpo.selectExecutionLLM(ctx, orchestrationStepConfig, isRetryAfterValidationFailure, retryAttempt, stepID, stepPath, learningsFolderEmpty)

	// Additional fallback for orchestration orchestrator: if no ExecutionLLM found, try ConditionalLLM
	// This is specific to orchestration orchestrator (similar purpose - structured decision making)
	// Only use ConditionalLLM if we got orchestrator default (meaning no step/preset ExecutionLLM was found, and no tempLLM was used)
	if orchestrationStepConfig != nil && orchestrationStepConfig.ExecutionLLM == nil && orchestrationStepConfig.ConditionalLLM != nil {
		// Check if we got orchestrator default (no tempLLM, no step ExecutionLLM, no preset ExecutionLLM)
		// If so, use ConditionalLLM as an additional fallback before orchestrator default
		if llmConfig.Provider == orchestratorLLMConfig.Provider && llmConfig.ModelID == orchestratorLLMConfig.ModelID {
			conditionalLLMConfig := orchestrationStepConfig.ConditionalLLM
			llmConfig = &orchestrator.LLMConfig{
				Provider:       conditionalLLMConfig.Provider,
				ModelID:        conditionalLLMConfig.ModelID,
				FallbackModels: []string{},
				APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys
			}
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific conditional LLM for orchestration orchestrator (fallback when ExecutionLLM not available): %s/%s", conditionalLLMConfig.Provider, conditionalLLMConfig.ModelID))
		}
	}

	// Create agent name
	agentName := fmt.Sprintf("orchestration-orchestrator-step-%d", stepIndex+1)

	// Create orchestration orchestrator agent using factory
	// orchestrationStepConfig already set above
	orchestrationOrchestratorAgent, err := hcpo.createOrchestrationOrchestratorAgent(ctx, "orchestration_orchestrator", stepIndex, iteration, agentName, orchestrationStepConfig, llmConfig)
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
