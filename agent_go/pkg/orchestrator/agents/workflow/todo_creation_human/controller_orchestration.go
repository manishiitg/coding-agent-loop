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

// appendOrchestrationLogEntry appends a JSON entry to the orchestration execution log file (JSONL format)
// Each entry is a single JSON object on its own line
func (hcpo *HumanControlledTodoPlannerOrchestrator) appendOrchestrationLogEntry(ctx context.Context, filePath string, entry map[string]interface{}) error {
	// Marshal the entry to a single JSON line (no indentation for JSONL format)
	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal orchestration log entry to JSON: %w", err)
	}

	// Read existing file content if it exists
	existingContent := ""
	existingContent, err = hcpo.ReadWorkspaceFile(ctx, filePath)
	if err != nil {
		// File doesn't exist yet - this is expected for the first entry
		// Only log if it's not a "file not found" type error
		if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "no such file") {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read existing orchestration log file %s: %v (will create new file)", filePath, err))
		}
		existingContent = ""
	}

	// Append new entry (with newline if file already has content)
	newContent := existingContent
	if existingContent != "" {
		newContent += "\n"
	}
	newContent += string(entryJSON)

	// Write the updated content back
	if err := hcpo.WriteWorkspaceFile(ctx, filePath, newContent); err != nil {
		return fmt.Errorf("failed to append orchestration log entry to %s: %w", filePath, err)
	}

	return nil
}

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
	stepPath string, // Optional: if empty, will be generated from stepIndex (e.g., "step-1" or "step-1-if-true-0" for branch steps)
) (bool, string, error) {
	// Steps are already PlanStepInterface - no conversion needed

	// Get orchestration step as PlanStepInterface from original step
	orchestrationStepPlan, ok := step.(*OrchestrationPlanStep)
	if !ok {
		return false, "", fmt.Errorf("step is not an OrchestrationPlanStep")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🎯 Executing orchestration step %d: %s", stepIndex+1, step.GetTitle()))

	// Use provided stepPath or generate from stepIndex (for backward compatibility)
	orchestrationStepPath := stepPath
	if orchestrationStepPath == "" {
		orchestrationStepPath = fmt.Sprintf("step-%d", stepIndex+1)
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Using orchestration step path: %s", orchestrationStepPath))

	// Check if this is a branch step (branch steps don't need next_step_id - routing is handled by conditional step)
	isBranchStep := strings.Contains(orchestrationStepPath, "-if-true-") || strings.Contains(orchestrationStepPath, "-if-false-")

	// Validate orchestration step has required fields
	if orchestrationStepPlan.OrchestrationStep == nil {
		return false, "", fmt.Errorf("orchestration step %d (%s) is missing required orchestration_step field", stepIndex+1, step.GetTitle())
	}
	if len(orchestrationStepPlan.OrchestrationRoutes) == 0 {
		return false, "", fmt.Errorf("orchestration step %d (%s) has no orchestration routes defined", stepIndex+1, step.GetTitle())
	}
	// next_step_id is only required for regular orchestration steps, not branch steps
	if !isBranchStep && orchestrationStepPlan.NextStepID == "" {
		return false, "", fmt.Errorf("orchestration step %d (%s) is missing required next_step_id field", stepIndex+1, step.GetTitle())
	}
	if isBranchStep && orchestrationStepPlan.NextStepID == "" {
		hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Orchestration branch step %d (%s) has no next_step_id - this is expected for branch steps (routing handled by conditional step)", stepIndex+1, step.GetTitle()))
		// Set a default empty nextStepID for branch steps to avoid issues later
		orchestrationStepPlan.NextStepID = ""
	}

	// Emit step_started event for orchestration step
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, orchestrationStepPath, false)

	// Simplified: Keep conversation history in-memory only (not persisted)
	// Orchestration steps are tracked simply: completed or not (via CompletedStepIndices)
	var conversationHistory []llmtypes.MessageContent

	// Determine max iterations: use step-specific if provided, otherwise use orchestrator default
	maxOrchestrationIterations := hcpo.GetMaxTurns()
	stepConfig := getAgentConfigs(orchestrationStepPlan)
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
		// Use orchestrationStepPath as base to support branch steps (e.g., "step-1-if-true-0-orchestration")
		mainStepPath := fmt.Sprintf("%s-orchestration", orchestrationStepPath)
		if orchestrationIteration > 0 {
			mainStepPath = fmt.Sprintf("%s-orchestration-%d", orchestrationStepPath, orchestrationIteration+1)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing main orchestration step: %s (iteration %d)", orchestrationStepPlan.OrchestrationStep.GetTitle(), orchestrationIteration+1))

		// Load the main step's own config by its ID
		stepID := orchestrationStepPlan.GetID()
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 Loading step config for orchestration step (ID: %s)", stepID))

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

		// Load the main step's own config by its ID
		if err := ApplyStepConfigFromFile(ctx, orchestrationStepPlan, hcpo); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load step config for orchestration step '%s' (ID: %s): %v - will use preset defaults", orchestrationStepPlan.GetTitle(), stepID, err))
		}

		// Check if step config was applied and log the result
		stepConfig := getAgentConfigs(orchestrationStepPlan)
		if stepConfig != nil {
			if stepConfig.UseCodeExecutionMode != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Step config loaded for orchestration step (ID: %s) - use_code_execution_mode: %v", stepID, *stepConfig.UseCodeExecutionMode))
			}
			if stepConfig.SelectedServers != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Step config loaded for orchestration step (ID: %s) - selected_servers: %v", stepID, stepConfig.SelectedServers))
			}
			if stepConfig.UseCodeExecutionMode == nil && stepConfig.SelectedServers == nil {
				hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Step config found for orchestration step (ID: %s) but incomplete - will use preset defaults for missing fields", stepID))
			}
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Step config not found for orchestration step (ID: %s) - will use preset defaults", stepID))
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
			orchestrationStepPlan, // Pass main step so it can use its own config
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

		// Store orchestration routing JSON to logs folder (always saved, not conditional)
		var validationWorkspacePath string
		if hcpo.selectedRunFolder != "" {
			validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		} else {
			validationWorkspacePath = hcpo.GetWorkspacePath()
		}

		validationFolderPath := getValidationFolderPath(validationWorkspacePath, orchestrationStepPath)
		// Append routing decision to single orchestration execution log file (JSONL format)
		orchestrationLogFilePath := fmt.Sprintf("%s/orchestration-execution.json", validationFolderPath)
		routingEntry := map[string]interface{}{
			"type":                  "routing",
			"step_index":            stepIndex + 1,
			"step_path":             orchestrationStepPath,
			"orchestration_step_id": step.GetID(),
			"iteration":             orchestrationIteration + 1,
			"orchestration_response": map[string]interface{}{
				"selected_route_id":                  orchestrationResponse.SelectedRouteID,
				"success_criteria_met":               orchestrationResponse.SuccessCriteriaMet,
				"success_reasoning":                  orchestrationResponse.SuccessReasoning,
				"instructions_to_sub_agent":          orchestrationResponse.InstructionsToSubAgent,
				"success_criteria_for_sub_agent":     orchestrationResponse.SuccessCriteriaForSubAgent,
				"context_dependencies_for_sub_agent": orchestrationResponse.ContextDependenciesForSubAgent,
				"context_output_for_sub_agent":       orchestrationResponse.ContextOutputForSubAgent,
			},
			"timestamp": time.Now().Format(time.RFC3339),
		}

		if err := hcpo.appendOrchestrationLogEntry(ctx, orchestrationLogFilePath, routingEntry); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to append orchestration routing entry to log: %v", err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("💾 Orchestration routing entry appended to: %s", orchestrationLogFilePath))
		}

		// Store main step execution result to logs (if enabled)
		if hcpo.saveValidationResponses {
			validationFolderPath := getValidationFolderPath(validationWorkspacePath, orchestrationStepPath)
			orchestrationLogFilePath := fmt.Sprintf("%s/orchestration-execution.json", validationFolderPath)
			mainStepEntry := map[string]interface{}{
				"type":                  "main_step",
				"step_index":            stepIndex + 1,
				"step_path":             mainStepPath,
				"orchestration_step_id": step.GetID(),
				"iteration":             orchestrationIteration + 1,
				"orchestration_response": map[string]interface{}{
					"selected_route_id":                  orchestrationResponse.SelectedRouteID,
					"success_criteria_met":               orchestrationResponse.SuccessCriteriaMet,
					"success_reasoning":                  orchestrationResponse.SuccessReasoning,
					"instructions_to_sub_agent":          orchestrationResponse.InstructionsToSubAgent,
					"success_criteria_for_sub_agent":     orchestrationResponse.SuccessCriteriaForSubAgent,
					"context_dependencies_for_sub_agent": orchestrationResponse.ContextDependenciesForSubAgent,
					"context_output_for_sub_agent":       orchestrationResponse.ContextOutputForSubAgent,
				},
				"timestamp": time.Now().Format(time.RFC3339),
			}

			if err := hcpo.appendOrchestrationLogEntry(ctx, orchestrationLogFilePath, mainStepEntry); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to append orchestration main step entry to log: %v", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Orchestration main step entry appended to: %s", orchestrationLogFilePath))
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
			validationFolderPath := getValidationFolderPath(validationWorkspacePath, orchestrationStepPath)
			orchestrationLogFilePath := fmt.Sprintf("%s/orchestration-execution.json", validationFolderPath)
			evaluationEntry := map[string]interface{}{
				"type":                  "evaluation",
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

			if err := hcpo.appendOrchestrationLogEntry(ctx, orchestrationLogFilePath, evaluationEntry); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to append orchestration evaluation entry to log: %v", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Orchestration evaluation entry appended to: %s", orchestrationLogFilePath))
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
			stepConfigs := getAgentConfigs(orchestrationStepPlan)
			skipLLMValidation := stepConfigs != nil && stepConfigs.SkipLLMValidationIfPreValidationPasses != nil && *stepConfigs.SkipLLMValidationIfPreValidationPasses

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
				validationAgent, err := hcpo.createValidationAgent(ctx, "validation", stepIndex+1, validationAgentName, stepConfigs)
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
				orchestrationStepConfig := getAgentConfigs(orchestrationStepPlan)
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
				shouldSkipLearningDueToLock, _ := hcpo.ShouldSkipLearningDueToLock(ctx, orchestrationStepConfig, orchestrationStepID, stepIndex, orchestrationStepPath)
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
					
					// Calculate turn count for orchestration step
					turnCount := len(conversationHistory)
					err = hcpo.runSuccessLearningPhase(ctx, stepIndex, orchestrationStepPath, learningPathIdentifier, totalSteps, orchestrationStepPlan.OrchestrationStep, conversationHistory, validationResponse, isCodeExecutionMode, "", turnCount) // Orchestration steps don't use tempLLM
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
			shouldSkipLearningDueToLock, _ := hcpo.ShouldSkipLearningDueToLock(ctx, orchestrationStepConfig, orchestrationStepID, stepIndex, orchestrationStepPath)
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
				
				// Calculate turn count for orchestration step
				turnCount := len(conversationHistory)
				_, _, err = hcpo.runFailureLearningPhase(ctx, stepIndex, orchestrationStepPath, learningPathIdentifier, totalSteps, orchestrationStepPlan.OrchestrationStep, conversationHistory, validationResponse, isCodeExecutionMode, turnCount)
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

		// 4. Success criteria not met - must delegate to sub-agent or end workflow
		if orchestrationResponse.SelectedRouteID == "" {
			// Orchestrator must always delegate when success criteria is not met
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Orchestrator did not select a route when success criteria is not met (iteration %d/%d) - orchestrator must always delegate to a sub-agent", orchestrationIteration+1, maxOrchestrationIterations), nil)
			return false, "", fmt.Errorf("orchestration step %d: orchestrator must select a route (sub-agent) when success criteria is not met, but selected_route_id is empty", stepIndex+1)
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

		// Check if the selected route is "learning" - trigger orchestrator learning agent
		if strings.ToLower(selectedRoutePlan.RouteID) == "learning" || strings.ToLower(selectedRoutePlan.RouteID) == "orchestrator-learning" {
			hcpo.GetLogger().Info("🧠 Orchestrator chose to learn from recent decisions (route: 'learning') - triggering orchestration learning agent")

			// Get orchestration step ID for learning path
			orchestrationStepID := orchestrationStepPlan.OrchestrationStep.GetID()
			if orchestrationStepID == "" {
				orchestrationStepID = orchestrationStepPath // Fallback to stepPath
			}
			learningPathIdentifier := getLearningPathIdentifier(orchestrationStepID, orchestrationStepPath)

			// Get step config for learning agent
			orchestrationStepConfig := getAgentConfigs(orchestrationStepPlan)

			// Create orchestration learning agent
			learningAgentName := fmt.Sprintf("orchestration-learning-step-%d", stepIndex+1)
			learningAgent, err := hcpo.createOrchestrationLearningAgent(ctx, "orchestration_learning", learningPathIdentifier, learningAgentName, orchestrationStepConfig, orchestrationStepID, orchestrationStepPath)
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to create orchestration learning agent: %v", err), nil)
				return false, "", fmt.Errorf("failed to create orchestration learning agent: %w", err)
			}

			// Prepare template variables for learning agent
			runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
			executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
			stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, orchestrationStepPath)

			// Build orchestration routes description
			routesDescription := ""
			for i, route := range orchestrationStepPlan.OrchestrationRoutes {
				routesDescription += fmt.Sprintf("\n**Route %d: %s** (ID: %s)\n", i+1, route.RouteName, route.RouteID)
				routesDescription += fmt.Sprintf("- Condition: %s\n", route.Condition)
				if route.ContextToPass != "" {
					routesDescription += fmt.Sprintf("- Context to pass: %s\n", route.ContextToPass)
				}
				if route.SubAgentStep != nil {
					routesDescription += fmt.Sprintf("- Sub-agent: %s\n", route.SubAgentStep.GetTitle())
				}
			}

			// Format conversation history as orchestration history
			orchestrationHistory := shared.FormatConversationHistory(conversationHistory)

			// Read existing orchestrator learnings
			existingLearningsContent := ""
			baseWorkspacePath := hcpo.GetWorkspacePath()
			stepLearningsPath := getLearningFolderPathByStepID(baseWorkspacePath, orchestrationStepID, orchestrationStepPath)
			orchestratorLearningPath := fmt.Sprintf("%s/orchestrator_learning.md", stepLearningsPath)
			existingLearnings, err := hcpo.ReadWorkspaceFile(ctx, orchestratorLearningPath)
			if err == nil && existingLearnings != "" {
				existingLearningsContent = existingLearnings
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Read existing orchestrator learnings from: %s", orchestratorLearningPath))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ No existing orchestrator learnings found at: %s (will create new)", orchestratorLearningPath))
			}

			// Prepare validation result (use last validation response if available)
			validationResult := "No validation results available yet."
			if orchestrationStepPlan.OrchestrationResponse != nil {
				if orchestrationStepPlan.OrchestrationResponse.SuccessCriteriaMet {
					validationResult = fmt.Sprintf("Success criteria met: %s", orchestrationStepPlan.OrchestrationResponse.SuccessReasoning)
				} else {
					validationResult = fmt.Sprintf("Success criteria not met: %s", orchestrationStepPlan.OrchestrationResponse.SuccessReasoning)
				}
			}

			learningTemplateVars := map[string]string{
				"StepTitle":                orchestrationStepPlan.OrchestrationStep.GetTitle(),
				"StepDescription":          orchestrationStepPlan.OrchestrationStep.GetDescription(),
				"StepSuccessCriteria":      orchestrationStepPlan.OrchestrationStep.GetSuccessCriteria(),
				"StepContextDependencies":  strings.Join(orchestrationStepPlan.OrchestrationStep.GetContextDependencies(), ", "),
				"StepContextOutput":        orchestrationStepPlan.OrchestrationStep.GetContextOutput().String(),
				"WorkspacePath":            executionWorkspacePath,
				"OrchestrationHistory":     orchestrationHistory,
				"ValidationResult":         validationResult,
				"OrchestrationRoutes":      routesDescription,
				"StepExecutionPath":        stepExecutionPath,
				"StepNumber":               learningPathIdentifier,
				"ExistingLearningsContent": existingLearningsContent,
			}

			// Add variable names if available
			if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
				learningTemplateVars["VariableNames"] = variableNames
			}

			// Execute learning agent
			learningResult, _, err := learningAgent.Execute(ctx, learningTemplateVars, []llmtypes.MessageContent{})
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Orchestration learning agent failed: %v", err), nil)
				return false, "", fmt.Errorf("orchestration learning agent failed: %w", err)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Orchestration learning completed. Result: %s", learningResult))

			// Add learning result to conversation history
			learningMessage := llmtypes.MessageContent{
				Role: llmtypes.ChatMessageTypeAI,
				Parts: []llmtypes.ContentPart{
					llmtypes.TextContent{
						Text: fmt.Sprintf("Orchestration learning agent completed:\n\n%s", learningResult),
					},
				},
			}
			conversationHistory = append(conversationHistory, learningMessage)

			// Loop back to execute main orchestration step again with learnings applied
			hcpo.GetLogger().Info(fmt.Sprintf("🔄 Orchestration learning completed, looping back to main orchestration step (iteration %d/%d)", orchestrationIteration+2, maxOrchestrationIterations))
			continue // Loop back to start of for loop
		}

		hcpo.GetLogger().Info(fmt.Sprintf("🔀 Executing sub-agent: %s (route: %s, index: %d, ID: %s)", subAgentStepPlan.GetTitle(), selectedRoutePlan.RouteID, subAgentIndex, subAgentStepPlan.GetID()))

		// Prepare sub-agent step with validation disabled
		// First, ensure AgentConfigs exists (create empty if needed) before loading config
		// This ensures ApplyStepConfigFromFile can properly merge config
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
			subAgentConfigs = getAgentConfigs(subAgentStepPlan)
		}

		// Match sub-agent's own step config from step_config.json (mandatory - no inheritance from parent)
		// Load config after ensuring AgentConfigs exists so merge works correctly
		if err := ApplyStepConfigFromFile(ctx, subAgentStepPlan, hcpo); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load step config for sub-agent '%s' (ID: %s): %v - will use defaults", subAgentStepPlan.GetTitle(), subAgentStepPlan.GetID(), err))
		} else {
			// Log what config was loaded
			subAgentConfigs = getAgentConfigs(subAgentStepPlan)
			if subAgentConfigs != nil && subAgentConfigs.UseCodeExecutionMode != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Loaded step config for sub-agent '%s' (ID: %s) - use_code_execution_mode: %v", subAgentStepPlan.GetTitle(), subAgentStepPlan.GetID(), *subAgentConfigs.UseCodeExecutionMode))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Step config loaded for sub-agent '%s' (ID: %s) but UseCodeExecutionMode not set - will use preset default", subAgentStepPlan.GetTitle(), subAgentStepPlan.GetID()))
			}
		}

		// Get AgentConfigs again after config loading
		subAgentConfigs = getAgentConfigs(subAgentStepPlan)
		val := true
		subAgentConfigs.DisableValidation = &val

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
				// Pre-validation error means we can't verify structure - create error result
				workspaceResults = &WorkspaceVerificationResult{
					OverallPass:  false, // Block on pre-validation errors
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
			} else if workspaceResults.OverallPass {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Pre-validation passed for sub-agent '%s'", subAgentStepPlan.GetTitle()))
			} else {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Pre-validation failed for sub-agent '%s' - %d checks failed", subAgentStepPlan.GetTitle(), workspaceResults.Summary.FailedChecks))
				// Log validation errors for debugging
				for _, validationError := range workspaceResults.Summary.Errors {
					hcpo.GetLogger().Warn(fmt.Sprintf("  - %s: %s (expected: %s, actual: %s)", validationError.File, validationError.Message, validationError.Expected, validationError.Actual))
				}
			}

			// Emit pre-validation completed event for sub-agent
			hcpo.emitPreValidationCompletedEvent(ctx, subAgentStepPlan, stepIndex, subAgentPath, true, workspaceResults)
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
	orchestrationStepConfig := getAgentConfigs(orchestrationStepPlan)
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
	shouldSkipLearningDueToLock, _ := hcpo.ShouldSkipLearningDueToLock(ctx, orchestrationStepConfig, orchestrationStepID, stepIndex, orchestrationStepPath)
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
						
						// Calculate turn count for orchestration step
						turnCount := len(conversationHistory)
						_, _, err := hcpo.runFailureLearningPhase(ctx, stepIndex, orchestrationStepPath, learningPathIdentifier, totalSteps, orchestrationStepPlan.OrchestrationStep, conversationHistory, failureValidationResponse, isCodeExecutionMode, turnCount)
						if err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failure learning phase failed for orchestration step %s: %v", orchestrationStepPath, err))
						} else {
							hcpo.GetLogger().Info(fmt.Sprintf("✅ Failure learning analysis completed for orchestration step %s", orchestrationStepPath))
						}
					} else {		if isFastExecuteStep {
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

	// Get the inner step for metadata (title, description, etc.) if this is an OrchestrationPlanStep
	// The main step's config is used, but inner step's metadata is used for template variables
	var innerStep PlanStepInterface = step
	orchestrationStepPlan, isOrchestrationStep := step.(*OrchestrationPlanStep)
	if isOrchestrationStep && orchestrationStepPlan.OrchestrationStep != nil {
		innerStep = orchestrationStepPlan.OrchestrationStep
	}

	// Determine code execution mode using helper method
	// Every step has its own config by its own ID - use main step's config
	orchestrationStepConfig := getAgentConfigs(step)
	isCodeExecutionMode := hcpo.getCodeExecutionMode(orchestrationStepConfig)

	stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, stepPath)
	// Ensure step execution folder exists before creating orchestration agent (agent needs to write to this folder)
	if err := hcpo.ensureStepExecutionFolderExists(ctx, stepExecutionPath); err != nil {
		// Non-blocking: log warning but continue execution (folder will be created when files are written)
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure orchestration step execution folder exists: %v (continuing - folder will be created when files are written)", err))
	}

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

	// Read orchestrator learnings (like regular execution agents read learning history)
	var formattedOrchestratorLearningHistory string
	orchestrationStepID := innerStep.GetID()
	if orchestrationStepID == "" {
		orchestrationStepID = stepPath // Fallback to stepPath
	}

	// Check if learning is disabled (orchestrationStepConfig already declared above)
	isLearningDisabledStep := orchestrationStepConfig != nil && orchestrationStepConfig.DisableLearning != nil && *orchestrationStepConfig.DisableLearning
	isLearningDetailLevelNone := false
	if orchestrationStepConfig != nil && orchestrationStepConfig.LearningDetailLevel == "none" {
		isLearningDetailLevelNone = true
	}
	isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone

	if isLearningDisabled {
		formattedOrchestratorLearningHistory = ""
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Learning disabled for orchestration step %d - skipping orchestrator learning history reading", stepIndex+1))
	} else {
		// Read orchestrator learnings from orchestrator_learning.md
		baseWorkspacePath := hcpo.GetWorkspacePath()
		stepLearningsPath := getLearningFolderPathByStepID(baseWorkspacePath, orchestrationStepID, stepPath)
		orchestratorLearningPath := fmt.Sprintf("%s/orchestrator_learning.md", stepLearningsPath)

		orchestratorLearnings, err := hcpo.ReadWorkspaceFile(ctx, orchestratorLearningPath)
		if err == nil && orchestratorLearnings != "" {
			// Format orchestrator learnings similar to regular learning history
			formattedOrchestratorLearningHistory = fmt.Sprintf("## 📚 ORCHESTRATOR LEARNINGS\n\n%s", orchestratorLearnings)
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Read orchestrator learnings from: %s (length: %d chars)", orchestratorLearningPath, len(orchestratorLearnings)))
		} else {
			formattedOrchestratorLearningHistory = ""
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ No orchestrator learnings found for orchestration step %d - learnings folder: %s", stepIndex+1, stepLearningsPath))
		}
	}

	// Prepare template variables
	// Use inner step's metadata (title, description, etc.) for template variables
	templateVars := map[string]string{
		"StepTitle":            ResolveVariables(innerStep.GetTitle(), hcpo.variableValues),
		"StepDescription":      ResolveVariables(innerStep.GetDescription(), hcpo.variableValues),
		"StepSuccessCriteria":  ResolveVariables(innerStep.GetSuccessCriteria(), hcpo.variableValues),
		"StepContextOutput":    ResolveVariables(innerStep.GetContextOutput().String(), hcpo.variableValues),
		"WorkspacePath":        executionWorkspacePath,
		"IsCodeExecutionMode":  fmt.Sprintf("%v", isCodeExecutionMode),
		"StepNumber":           stepPath,
		"StepExecutionPath":    stepExecutionPath,
		"PreviousStepsSummary": previousStepsSummary,
		"OrchestrationRoutes":  routesDescription,
		"LearningHistory":      formattedOrchestratorLearningHistory, // Pass orchestrator learnings (like regular execution agents)
	}

	// Add context dependencies
	contextDeps := innerStep.GetContextDependencies()
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

	// Set folder guard paths: allow reads from step-specific learnings, execution, and run folder, writes only to current step folder
	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepID := step.GetID()
	if stepID == "" {
		stepID = fmt.Sprintf("step-%d", stepIndex+1) // Fallback to step number if ID not available
	}
	// Step-specific learnings folder: learnings/{stepID}/ (only this step's learnings, not full learnings folder)
	stepLearningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepID)
	// Knowledgebase folder: knowledgebase/ (persistent files across runs, at workspace root)
	knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)
	// READ: step-specific learnings folder + execution folder + run folder + knowledgebase folder (to read previous step results and run folder files)
	// Note: runWorkspacePath is already defined earlier in this function (line 810)
	// WRITE: only the specific step folder (execution/step-{X}-orchestration/ or execution/step-{X}-orchestration-{N}/) + knowledgebase folder
	readPaths := []string{stepLearningsPath, executionWorkspacePath, runWorkspacePath, knowledgebasePath}
	writePaths := []string{stepExecutionPath, knowledgebasePath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for orchestration orchestrator agent - Read paths: %v, Write paths: %v (can read learnings/%s/, execution/, and run folder, can only write to %s)", readPaths, writePaths, stepID, stepPath))

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

	// Get step config - every step has its own config by its own ID
	orchestrationStepConfig := getAgentConfigs(step)
	stepID := step.GetID()
	if orchestrationStepConfig != nil && orchestrationStepConfig.UseCodeExecutionMode != nil {
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Step config found - UseCodeExecutionMode: %v (step ID: %s)", *orchestrationStepConfig.UseCodeExecutionMode, stepID))
	} else if orchestrationStepConfig != nil {
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Step config found but UseCodeExecutionMode is nil (step ID: %s)", stepID))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Step config is nil (step ID: %s)", stepID))
	}

	// Get step ID for orchestration learnings (use step's own ID)
	// For OrchestrationPlanStep, learnings are stored using the inner step's ID (for folder organization)
	stepPath := fmt.Sprintf("step-%d", stepIndex+1)
	orchestrationStepPlan, isOrchestrationStep := step.(*OrchestrationPlanStep)
	if isOrchestrationStep && orchestrationStepPlan.OrchestrationStep != nil {
		// For orchestration steps, use inner step ID for learnings folder path
		stepID = orchestrationStepPlan.OrchestrationStep.GetID()
		if stepID == "" {
			stepID = stepPath // Fallback: use stepPath as identifier
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Could not determine step ID for step %d, using stepPath as fallback", stepIndex+1))
		}
	} else {
		// Not an orchestration step - use step's own ID
		stepID = step.GetID()
		if stepID == "" {
			stepID = stepPath // Fallback: use stepPath as identifier
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Could not determine step ID for step %d, using stepPath as fallback", stepIndex+1))
		}
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

	// Create agent name with step ID
	agentName := fmt.Sprintf("orchestrator-%s", stepID)

	// Create orchestration orchestrator agent using factory
	// orchestrationStepConfig already set above
	orchestrationOrchestratorAgent, err := hcpo.createOrchestrationOrchestratorAgent(ctx, "orchestrator", stepIndex, iteration, agentName, orchestrationStepConfig, llmConfig)
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
