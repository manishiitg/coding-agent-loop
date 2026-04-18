package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// executeHumanInputStep executes a human input step by:
// 1. Asking a question to the human (blocking)
// 2. Saving the response to a JSON file in the execution folder
// 3. Optionally storing the response in a variable
// 4. Passing the response file to next steps via context output
//
// NOTE: This function works with PlanStepInterface (specifically HumanInputPlanStep).
// The step type is already validated by the main execution loop before calling this function.
// Returns: (updatedContextFiles []string, error)
func (hcpo *StepBasedWorkflowOrchestrator) executeHumanInputStep(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	progress *StepProgress,
	previousContextFiles []string,
	execCtx *ExecutionContext,
	allSteps []PlanStepInterface,
) ([]string, error) {
	// Get human input step as PlanStepInterface from original step
	humanInputStep, ok := step.(*HumanInputPlanStep)
	if !ok {
		return nil, fmt.Errorf("step is not a HumanInputPlanStep")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("👤 Executing human input step %d: %s", stepIndex+1, step.GetTitle()))

	// Validate human input step has required fields
	if humanInputStep.Question == "" {
		return nil, fmt.Errorf("human input step %d (%s) is missing required question field", stepIndex+1, step.GetTitle())
	}
	// NextStepID is required as fallback, but conditional routing can override it
	if humanInputStep.NextStepID == "" && humanInputStep.IfYesNextStepID == "" && humanInputStep.IfNoNextStepID == "" && len(humanInputStep.OptionRoutes) == 0 {
		return nil, fmt.Errorf("human input step %d (%s) must have at least one of: next_step_id, if_yes_next_step_id/if_no_next_step_id (for yesno), or option_routes (for multiple_choice)", stepIndex+1, step.GetTitle())
	}

	// Emit step_started event
	stepPath := fmt.Sprintf("step-%d", stepIndex+1)
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, stepPath, false)

	// Determine execution workspace path
	runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, step.GetID(), stepPath)
	// Ensure step execution folder exists before writing response file
	if err := hcpo.ensureStepExecutionFolderExists(ctx, stepExecutionPath); err != nil {
		// Non-blocking: log warning but continue execution (folder will be created when files are written)
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure human input step execution folder exists: %v (continuing - folder will be created when files are written)", err))
	}

	// Get context output path (default to step-{index}.json if not specified)
	contextOutput := step.GetContextOutput().String()
	if contextOutput == "" {
		contextOutput = fmt.Sprintf("step-%d.json", stepIndex+1)
		hcpo.GetLogger().Info(fmt.Sprintf("📝 No context_output specified, using default: %s", contextOutput))
	}
	// Resolve variables in context output
	resolvedContextOutput := ResolveVariables(contextOutput, hcpo.variableValues)

	// Determine response type (default: "text")
	responseType := humanInputStep.ResponseType
	if responseType == "" {
		responseType = "text"
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📋 Requesting human input with response type: %s", responseType))

	// Resolve variables in question
	resolvedQuestion := ResolveVariables(humanInputStep.Question, hcpo.variableValues)

	// Request human feedback based on response type
	var response string
	var selectedOptionIndex int = -1 // For multiple_choice tracking
	var err error

	// Workshop mode: if the builder agent provided human_input via execute_step,
	// use it as the response instead of blocking for user input.
	// The value is threaded on the ExecutionContext (WorkshopHumanInput), not a session field,
	// so it only applies to the specific execute_step call that set it.
	workshopHumanInput := ""
	if execCtx != nil {
		workshopHumanInput = execCtx.WorkshopHumanInput
	}
	if workshopHumanInput != "" {
		response = workshopHumanInput
		hcpo.GetLogger().Info(fmt.Sprintf("🤖 Workshop mode: using pre-filled response for human input step (response_type=%s, length=%d chars)", responseType, len(response)))

		// For yesno, normalize the response
		if responseType == "yesno" {
			normalized := strings.ToLower(strings.TrimSpace(response))
			if normalized == "yes" || normalized == "approve" || normalized == "true" || normalized == "y" {
				response = "yes"
			} else {
				response = "no"
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Workshop yesno response normalized to: %s", response))
		}

		// For multiple_choice, try to match the response to an option
		if responseType == "multiple_choice" && len(humanInputStep.Options) > 0 {
			resolvedOptions := make([]string, len(humanInputStep.Options))
			for i, option := range humanInputStep.Options {
				resolvedOptions[i] = ResolveVariables(option, hcpo.variableValues)
			}
			normalizedResponse := strings.ToLower(strings.TrimSpace(response))
			for i, opt := range resolvedOptions {
				if strings.ToLower(opt) == normalizedResponse || fmt.Sprintf("%d", i) == normalizedResponse {
					selectedOptionIndex = i
					response = opt
					break
				}
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Workshop multiple_choice response: %s (index: %d)", response, selectedOptionIndex))
		}
	} else if hcpo.IsSkipHumanInput() {
		// Full workflow run (start_from_beginning_no_human): resolve response from overrides or variables.
		// Priority: 1) humanInputOverrides[step_id], 2) variableValues[variable_name], 3) "approved" fallback
		if val, ok := hcpo.humanInputOverrides[step.GetID()]; ok && val != "" {
			response = val
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skip human input: using human_inputs override for step '%s': %s", step.GetID(), response))
		} else if humanInputStep.VariableName != "" {
			if val, ok := hcpo.variableValues[humanInputStep.VariableName]; ok && val != "" {
				response = val
			} else if val, ok := hcpo.variableValues[strings.ToUpper(humanInputStep.VariableName)]; ok && val != "" {
				response = val
			}
		}
		if response == "" {
			response = "approved"
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Skip human input: no pre-set value found for step '%s' (variable '%s'), using 'approved'", step.GetID(), humanInputStep.VariableName))
		} else if !strings.HasPrefix(response, "approved") {
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skip human input: using pre-set value for step '%s': %s", step.GetID(), response))
		}
		// Normalize for yesno
		if responseType == "yesno" {
			normalized := strings.ToLower(strings.TrimSpace(response))
			if normalized == "yes" || normalized == "approve" || normalized == "approved" || normalized == "true" || normalized == "y" {
				response = "yes"
			} else {
				response = "no"
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Skip-mode yesno response normalized to: %s", response))
		}
		// For multiple_choice, try to match the response to an option
		if responseType == "multiple_choice" && len(humanInputStep.Options) > 0 {
			resolvedOptions := make([]string, len(humanInputStep.Options))
			for i, option := range humanInputStep.Options {
				resolvedOptions[i] = ResolveVariables(option, hcpo.variableValues)
			}
			normalizedResponse := strings.ToLower(strings.TrimSpace(response))
			for i, opt := range resolvedOptions {
				if strings.ToLower(opt) == normalizedResponse || fmt.Sprintf("%d", i) == normalizedResponse {
					selectedOptionIndex = i
					response = opt
					break
				}
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Skip-mode multiple_choice response: %s (index: %d)", response, selectedOptionIndex))
		}
	} else {
		// Normal mode: block and wait for user input via UI
		requestID := fmt.Sprintf("human_input_step_%d_%d", stepIndex+1, time.Now().UnixNano())

		switch responseType {
		case "yesno":
			// Yes/No feedback
			approved, err := hcpo.RequestYesNoFeedback(
				ctx,
				requestID,
				resolvedQuestion,
				"Approve",
				"Reject",
				"", // No additional context
				hcpo.getSessionID(),
				hcpo.getWorkflowID(),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get yes/no feedback: %w", err)
			}
			if approved {
				response = "yes"
			} else {
				response = "no"
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Received yes/no response: %s", response))

		case "multiple_choice":
			// Multiple choice feedback
			if len(humanInputStep.Options) == 0 {
				return nil, fmt.Errorf("human input step %d (%s) has response_type=multiple_choice but no options provided", stepIndex+1, step.GetTitle())
			}
			// Resolve variables in options
			resolvedOptions := make([]string, len(humanInputStep.Options))
			for i, option := range humanInputStep.Options {
				resolvedOptions[i] = ResolveVariables(option, hcpo.variableValues)
			}
			choice, err := hcpo.RequestMultipleChoiceFeedback(
				ctx,
				requestID,
				resolvedQuestion,
				resolvedOptions,
				"", // No additional context
				hcpo.getSessionID(),
				hcpo.getWorkflowID(),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get multiple choice feedback: %w", err)
			}
			// Parse choice index from "option0", "option1", etc.
			if strings.HasPrefix(choice, "option") {
				indexStr := strings.TrimPrefix(choice, "option")
				var index int
				if _, err := fmt.Sscanf(indexStr, "%d", &index); err == nil && index >= 0 && index < len(resolvedOptions) {
					selectedOptionIndex = index
					response = resolvedOptions[index]
				} else {
					response = choice // Fallback to raw choice
				}
			} else {
				response = choice
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Received multiple choice response: %s (index: %d)", response, selectedOptionIndex))

		default:
			// Text feedback (default)
			approved, feedback, err := hcpo.RequestHumanFeedback(
				ctx,
				requestID,
				resolvedQuestion,
				"", // No additional context
				hcpo.getSessionID(),
				hcpo.getWorkflowID(),
			)
			if err != nil {
				return nil, fmt.Errorf("failed to get human feedback: %w", err)
			}
			if approved {
				// User approved - use feedback text if provided, otherwise "approved"
				if feedback != "" {
					response = feedback
				} else {
					response = "approved"
				}
			} else {
				// User provided feedback (rejection or revision)
				response = feedback
			}
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Received text response (length: %d chars)", len(response)))
		}
	}

	// Determine next step ID based on response and routing configuration
	nextStepID := hcpo.getNextStepIDForHumanInput(humanInputStep, response, responseType, selectedOptionIndex)

	// Store computed next step ID in the step for routing
	humanInputStep.SelectedNextStepID = nextStepID
	hcpo.GetLogger().Info(fmt.Sprintf("🔗 Computed next step ID: %s", nextStepID))

	// Create response JSON structure
	responseData := map[string]interface{}{
		"question":      resolvedQuestion,
		"response":      response,
		"response_type": responseType,
		"timestamp":     time.Now().Format(time.RFC3339),
		"step_index":    stepIndex + 1,
		"step_id":       step.GetID(),
		"step_title":    step.GetTitle(),
		"next_step_id":  nextStepID, // Include computed next step ID in response
	}
	if responseType == "multiple_choice" && len(humanInputStep.Options) > 0 {
		responseData["options"] = humanInputStep.Options
	}

	// Marshal response to JSON
	responseJSON, err := json.MarshalIndent(responseData, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response to JSON: %w", err)
	}

	// Save response to execution folder
	responseFilePath := filepath.Join(stepExecutionPath, resolvedContextOutput)
	if err := hcpo.WriteWorkspaceFile(ctx, responseFilePath, string(responseJSON)); err != nil {
		return nil, fmt.Errorf("failed to write response file to %s: %w", responseFilePath, err)
	}
	hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved human input response to: %s", responseFilePath))

	// Store response in variable if VariableName is provided
	if humanInputStep.VariableName != "" {
		if hcpo.variableValues == nil {
			hcpo.variableValues = make(map[string]string)
		}
		hcpo.variableValues[humanInputStep.VariableName] = response
		// Also sync to workspace env so shell commands can access it
		if envRef := hcpo.GetWorkspaceEnvRef(); envRef != nil {
			hcpo.LockWorkspaceEnv()
			envRef["SECRET_"+humanInputStep.VariableName] = response
			hcpo.UnlockWorkspaceEnv()
		}
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Stored response in variable: %s = %s", humanInputStep.VariableName, response))
	}

	// Update context files - add this step's context output
	updatedContextFiles := make([]string, len(previousContextFiles))
	copy(updatedContextFiles, previousContextFiles)
	if resolvedContextOutput != "" {
		updatedContextFiles = append(updatedContextFiles, resolvedContextOutput)
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Added human input response to context files: %s", resolvedContextOutput))
	}

	// Emit step_finished event
	hcpo.emitStepFinishedEvent(ctx, step, stepIndex, stepPath, false)

	// Mark step as completed
	hcpo.addCompletedStepIndex(progress, stepIndex)
	if err := hcpo.saveStepProgress(ctx, progress); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save step progress: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved progress: human input step %d marked as completed", stepIndex+1))
	}

	return updatedContextFiles, nil
}

// getNextStepIDForHumanInput determines the next step ID based on the response and routing configuration
// Returns the next step ID to use, or empty string if no routing is configured
func (hcpo *StepBasedWorkflowOrchestrator) getNextStepIDForHumanInput(
	humanInputStep *HumanInputPlanStep,
	response string,
	responseType string,
	optionIndex int, // For multiple_choice, the selected option index
) string {
	switch responseType {
	case "yesno":
		// Check conditional routing for yes/no
		if response == "yes" && humanInputStep.IfYesNextStepID != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("🔗 Using if_yes_next_step_id: %s", humanInputStep.IfYesNextStepID))
			return humanInputStep.IfYesNextStepID
		} else if response == "no" && humanInputStep.IfNoNextStepID != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("🔗 Using if_no_next_step_id: %s", humanInputStep.IfNoNextStepID))
			return humanInputStep.IfNoNextStepID
		}
		// Fallback to default NextStepID
		if humanInputStep.NextStepID != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("🔗 Using default next_step_id: %s", humanInputStep.NextStepID))
			return humanInputStep.NextStepID
		}

	case "multiple_choice":
		// Check option routes - try both index and value as keys
		if len(humanInputStep.OptionRoutes) > 0 {
			// Try option index first (as string "0", "1", etc.)
			indexKey := fmt.Sprintf("%d", optionIndex)
			if nextStepID, exists := humanInputStep.OptionRoutes[indexKey]; exists {
				hcpo.GetLogger().Info(fmt.Sprintf("🔗 Using option_routes[%s]: %s", indexKey, nextStepID))
				return nextStepID
			}
			// Try option value as key
			if nextStepID, exists := humanInputStep.OptionRoutes[response]; exists {
				hcpo.GetLogger().Info(fmt.Sprintf("🔗 Using option_routes[\"%s\"]: %s", response, nextStepID))
				return nextStepID
			}
		}
		// Fallback to default NextStepID
		if humanInputStep.NextStepID != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("🔗 Using default next_step_id: %s", humanInputStep.NextStepID))
			return humanInputStep.NextStepID
		}

	default:
		// Text response - use default NextStepID
		if humanInputStep.NextStepID != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("🔗 Using default next_step_id: %s", humanInputStep.NextStepID))
			return humanInputStep.NextStepID
		}
	}

	// No routing configured
	return ""
}
