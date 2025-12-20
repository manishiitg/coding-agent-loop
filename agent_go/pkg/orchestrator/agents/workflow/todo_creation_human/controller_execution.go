package todo_creation_human

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/shared"

	"mcpagent/events"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// PrerequisiteFailureError is a special error type that signals a prerequisite failure detected during execution
// When this error is returned from a tool call, it triggers navigation to the prerequisite step
type PrerequisiteFailureError struct {
	DependsOnStepID string // Step ID to navigate back to
	StepIndex       int    // 0-based step index (computed from step ID)
	Reason          string // Reason for prerequisite failure
}

func (e *PrerequisiteFailureError) Error() string {
	return fmt.Sprintf("prerequisite failure detected: %s (navigate to step %s, index %d)", e.Reason, e.DependsOnStepID, e.StepIndex)
}

// isValidationFailure checks if validation failed (triggers human feedback)
// Returns true only if ExecutionStatus is "FAILED"
// Does NOT trigger on PARTIAL, COMPLETED, or INCOMPLETE status
func isValidationFailure(validationResponse *ValidationResponse) bool {
	if validationResponse == nil {
		return false
	}
	return validationResponse.ExecutionStatus == "FAILED"
}

// StepPathInfo contains parsed information from a stepPath
type StepPathInfo struct {
	ParentStepNumber int    // 1-based step number (for regular steps) or parent step number (for branch steps)
	BranchType       string // "true", "false", or "" (empty for regular steps)
	BranchIndex      int    // Branch step index (0-based) or -1 for regular steps
	IsBranchStep     bool   // True if this is a branch step
}

// parseStepPath parses a stepPath string to extract step and branch information
// Examples:
//   - "step-1" -> {ParentStepNumber: 1, BranchType: "", BranchIndex: -1, IsBranchStep: false}
//   - "step-3-if-true-0" -> {ParentStepNumber: 3, BranchType: "true", BranchIndex: 0, IsBranchStep: true}
//   - "step-3-if-false-1" -> {ParentStepNumber: 3, BranchType: "false", BranchIndex: 1, IsBranchStep: true}
//   - "step-2-decision" -> {ParentStepNumber: 2, BranchType: "", BranchIndex: -1, IsBranchStep: false} (decision step inner step)
//   - "step-2-sub-agent-1" -> {ParentStepNumber: 2, BranchType: "", BranchIndex: -1, IsBranchStep: true} (sub-agent step)
func parseStepPath(stepPath string) StepPathInfo {
	// Regular step pattern: "step-{number}"
	regularStepRegex := regexp.MustCompile(`^step-(\d+)$`)
	// Branch step pattern: "step-{number}-if-{true|false}-{index}"
	branchStepRegex := regexp.MustCompile(`^step-(\d+)-if-(true|false)-(\d+)$`)
	// Decision step inner step pattern: "step-{number}-decision"
	decisionStepRegex := regexp.MustCompile(`^step-(\d+)-decision$`)
	// Sub-agent step pattern: "step-{number}-sub-agent-{index}"
	subAgentStepRegex := regexp.MustCompile(`^step-(\d+)-sub-agent-(\d+)$`)

	if matches := branchStepRegex.FindStringSubmatch(stepPath); matches != nil {
		// Branch step
		parentStepNumber := 0
		branchIndex := -1
		fmt.Sscanf(matches[1], "%d", &parentStepNumber)
		fmt.Sscanf(matches[3], "%d", &branchIndex)
		return StepPathInfo{
			ParentStepNumber: parentStepNumber,
			BranchType:       matches[2], // "true" or "false"
			BranchIndex:      branchIndex,
			IsBranchStep:     true,
		}
	} else if matches := subAgentStepRegex.FindStringSubmatch(stepPath); matches != nil {
		// Sub-agent step - treat as branch step
		parentStepNumber := 0
		fmt.Sscanf(matches[1], "%d", &parentStepNumber)
		return StepPathInfo{
			ParentStepNumber: parentStepNumber,
			BranchType:       "",
			BranchIndex:      -1,
			IsBranchStep:     true,
		}
	} else if matches := decisionStepRegex.FindStringSubmatch(stepPath); matches != nil {
		// Decision step inner step - treat as regular step but use parent step number
		stepNumber := 0
		fmt.Sscanf(matches[1], "%d", &stepNumber)
		return StepPathInfo{
			ParentStepNumber: stepNumber,
			BranchType:       "",
			BranchIndex:      -1,
			IsBranchStep:     false,
		}
	} else if matches := regularStepRegex.FindStringSubmatch(stepPath); matches != nil {
		// Regular step
		stepNumber := 0
		fmt.Sscanf(matches[1], "%d", &stepNumber)
		return StepPathInfo{
			ParentStepNumber: stepNumber,
			BranchType:       "",
			BranchIndex:      -1,
			IsBranchStep:     false,
		}
	}

	// Fallback: try to extract just the step number
	stepNumber := 0
	fmt.Sscanf(stepPath, "step-%d", &stepNumber)
	return StepPathInfo{
		ParentStepNumber: stepNumber,
		BranchType:       "",
		BranchIndex:      -1,
		IsBranchStep:     false,
	}
}

// getExecutionFolderPath returns the execution folder path based on stepPath
// For regular steps: "execution/step-{X}/"
// For branch steps: "execution/step-{parentStep}-{true/false}-{branchIdx}/"
// For decision step inner steps: "execution/step-{X}-decision/"
// For sub-agent steps: "execution/step-{X}-sub-agent-{index}/"
func getExecutionFolderPath(executionWorkspacePath string, stepPath string) string {
	// Check if this is a sub-agent step
	// Pattern: step-{X}-sub-agent-{index}
	if strings.Contains(stepPath, "-sub-agent-") {
		return fmt.Sprintf("%s/%s", executionWorkspacePath, stepPath)
	}
	pathInfo := parseStepPath(stepPath)
	if pathInfo.IsBranchStep {
		return fmt.Sprintf("%s/step-%d-%s-%d", executionWorkspacePath, pathInfo.ParentStepNumber, pathInfo.BranchType, pathInfo.BranchIndex)
	}
	// Check if this is a decision step inner step
	// Pattern: step-{X}-decision
	if strings.Contains(stepPath, "-decision") {
		return fmt.Sprintf("%s/%s", executionWorkspacePath, stepPath)
	}
	return fmt.Sprintf("%s/step-%d", executionWorkspacePath, pathInfo.ParentStepNumber)
}

// getValidationFolderPath returns the validation folder path based on stepPath
// For regular steps: "logs/step-{X}/"
// For branch steps: "logs/step-{parentStep}-{true/false}-{branchIdx}/"
// For sub-agent steps: "logs/step-{X}-sub-agent-{index}/"
func getValidationFolderPath(validationWorkspacePath string, stepPath string) string {
	// Check if this is a sub-agent step
	// Pattern: step-{X}-sub-agent-{index}
	if strings.Contains(stepPath, "-sub-agent-") {
		return fmt.Sprintf("%s/logs/%s", validationWorkspacePath, stepPath)
	}
	pathInfo := parseStepPath(stepPath)
	if pathInfo.IsBranchStep {
		return fmt.Sprintf("%s/logs/step-%d-%s-%d", validationWorkspacePath, pathInfo.ParentStepNumber, pathInfo.BranchType, pathInfo.BranchIndex)
	}
	return fmt.Sprintf("%s/logs/step-%d", validationWorkspacePath, pathInfo.ParentStepNumber)
}

// getExecutionFolderPathForLogs returns the execution logs folder path based on stepPath
// For regular steps: "logs/step-{X}/execution/"
// For branch steps: "logs/step-{parentStep}-{true/false}-{branchIdx}/execution/"
// For sub-agent steps: "logs/step-{X}-sub-agent-{index}/execution/"
func getExecutionFolderPathForLogs(validationWorkspacePath string, stepPath string) string {
	// Check if this is a sub-agent step
	// Pattern: step-{X}-sub-agent-{index}
	if strings.Contains(stepPath, "-sub-agent-") {
		return fmt.Sprintf("%s/logs/%s/execution", validationWorkspacePath, stepPath)
	}
	pathInfo := parseStepPath(stepPath)
	if pathInfo.IsBranchStep {
		return fmt.Sprintf("%s/logs/step-%d-%s-%d/execution", validationWorkspacePath, pathInfo.ParentStepNumber, pathInfo.BranchType, pathInfo.BranchIndex)
	}
	return fmt.Sprintf("%s/logs/step-%d/execution", validationWorkspacePath, pathInfo.ParentStepNumber)
}

// getLearningFolderPath returns the learning folder path based on stepPath (OLD FORMAT - for backward compatibility only)
// This function is kept for migration purposes only. New code should use getLearningFolderPathByStepID.
// For regular steps: "learnings/step-{X}/" (old format)
// For branch steps: "learnings/step-{parentStep}-{true/false}-{branchIdx}/" (old format)
// For sub-agent steps: "learnings/step-{N}-sub-agent-{index}/" (old format)
func getLearningFolderPath(baseWorkspacePath string, stepPath string) string {
	// Check if this is a sub-agent step (pattern: step-{N}-sub-agent-{index})
	if strings.Contains(stepPath, "-sub-agent-") {
		// Return learnings path for sub-agents (old format, e.g., "learnings/step-2-sub-agent-1/")
		return fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepPath)
	}
	pathInfo := parseStepPath(stepPath)
	if pathInfo.IsBranchStep {
		// Check if this is actually a sub-agent (BranchType empty and BranchIndex -1 indicates sub-agent)
		// This is a safeguard in case the string check above didn't catch it
		if pathInfo.BranchType == "" && pathInfo.BranchIndex == -1 && strings.Contains(stepPath, "-sub-agent-") {
			return fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepPath)
		}
		// Only format as branch step if it's a real branch step (has BranchType)
		if pathInfo.BranchType != "" {
			return fmt.Sprintf("%s/learnings/step-%d-%s-%d", baseWorkspacePath, pathInfo.ParentStepNumber, pathInfo.BranchType, pathInfo.BranchIndex)
		}
		// If it's a branch step but no BranchType, it's likely a sub-agent - use stepPath as-is
		return fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepPath)
	}
	return fmt.Sprintf("%s/learnings/step-%d", baseWorkspacePath, pathInfo.ParentStepNumber)
}

// getLearningFolderPathByStepID returns the learning folder path using step ID (NEW FORMAT)
// For all steps (regular, branch, sub-agent): "learnings/{stepID}/"
// All steps have their own unique step IDs, so we just use the stepID directly
func getLearningFolderPathByStepID(baseWorkspacePath string, stepID string, stepPath string) string {
	// All steps (regular, branch, sub-agent) have their own unique step IDs
	// Just use the stepID directly without any suffix
	return fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepID)
}

// addCompletedStepIndex safely adds a step index to the completed list, preventing duplicates
// This is important when decision steps route back to previous steps, which can cause
// the same step index to be added multiple times if not checked
func (hcpo *HumanControlledTodoPlannerOrchestrator) addCompletedStepIndex(progress *StepProgress, stepIndex int) {
	// Check if already in list to prevent duplicates
	for _, idx := range progress.CompletedStepIndices {
		if idx == stepIndex {
			hcpo.GetLogger().Debug(fmt.Sprintf("⚠️ Step %d already in completed list, skipping duplicate", stepIndex+1))
			return // Already exists, don't add duplicate
		}
	}
	// Not found, safe to append
	progress.CompletedStepIndices = append(progress.CompletedStepIndices, stepIndex)
	hcpo.GetLogger().Debug(fmt.Sprintf("✅ Added step %d to completed list (total: %d)", stepIndex+1, len(progress.CompletedStepIndices)))
}

// getLearningPathIdentifier returns a unique identifier for learning folder based on step ID (NEW FORMAT)
// For all steps (regular, branch, sub-agent): "{stepID}"
// All steps have their own unique step IDs, so we just use the stepID directly
func getLearningPathIdentifier(stepID string, stepPath string) string {
	// All steps (regular, branch, sub-agent) have their own unique step IDs
	// Just use the stepID directly without any suffix
	return stepID
}

// getLearningPathIdentifierOld returns a unique identifier for learning folder based on stepPath (OLD FORMAT - for backward compatibility)
// For regular steps: "step-{X}"
// For branch steps: "step-{parentStep}-{true/false}-{branchIdx}"
// For sub-agent steps: "step-{N}-sub-agent-{index}"
func getLearningPathIdentifierOld(stepPath string) string {
	// Check if this is a sub-agent step (pattern: step-{N}-sub-agent-{index})
	if strings.Contains(stepPath, "-sub-agent-") {
		// Return the stepPath as-is for sub-agents (e.g., "step-2-sub-agent-1")
		return stepPath
	}
	pathInfo := parseStepPath(stepPath)
	if pathInfo.IsBranchStep {
		return fmt.Sprintf("step-%d-%s-%d", pathInfo.ParentStepNumber, pathInfo.BranchType, pathInfo.BranchIndex)
	}
	return fmt.Sprintf("step-%d", pathInfo.ParentStepNumber)
}

// executeConditionalStep is now in controller_conditional.go

// executeDecisionStep is now in controller_decision.go

// PrerequisiteInfo represents information about prerequisite steps for validation
type PrerequisiteInfo struct {
	CurrentStepID               string                 `json:"current_step_id"`
	CurrentStepIndex            int                    `json:"current_step_index"`
	EnablePrerequisiteDetection bool                   `json:"enable_prerequisite_detection"`
	PrerequisiteRules           []PrerequisiteRuleInfo `json:"prerequisite_rules"` // Array of prerequisite rules
}

// PrerequisiteRuleInfo represents information about a single prerequisite rule
type PrerequisiteRuleInfo struct {
	DependsOnStep      string             `json:"depends_on_step"`      // Step ID
	Description        string             `json:"description"`          // User description for this rule
	DependencyStepInfo DependencyStepInfo `json:"dependency_step_info"` // Info about the dependency step
}

// DependencyStepInfo represents information about a single dependency step
type DependencyStepInfo struct {
	StepID              string `json:"step_id"`               // Step ID
	StepIndex           int    `json:"step_index"`            // 0-based index
	StepTitle           string `json:"step_title"`            // Step title
	IsCompleted         bool   `json:"is_completed"`          // Whether step is completed
	ContextOutput       string `json:"context_output"`        // Context output file path
	ContextOutputExists bool   `json:"context_output_exists"` // Whether context output file exists
}

// gatherPrerequisiteInfo gathers information about prerequisite steps for the current step
func (hcpo *HumanControlledTodoPlannerOrchestrator) gatherPrerequisiteInfo(
	step TodoStep,
	stepIndex int,
	allSteps []TodoStep,
	progress *StepProgress,
	workspacePath string,
) *PrerequisiteInfo {
	// Check if prerequisite detection is enabled
	if step.AgentConfigs == nil || step.AgentConfigs.EnablePrerequisiteDetection == nil || !*step.AgentConfigs.EnablePrerequisiteDetection {
		return nil // Not enabled, return nil
	}

	// If allSteps is nil (e.g., in branch/conditional context), we can't gather prerequisite info
	if allSteps == nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Prerequisite detection enabled for step %d but allSteps not available (branch/conditional context)", stepIndex+1))
		return nil
	}

	// Get prerequisite rules
	prerequisiteRules := step.AgentConfigs.PrerequisiteRules
	if len(prerequisiteRules) == 0 {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Prerequisite detection enabled for step %d but no prerequisite_rules configured", stepIndex+1))
		return nil
	}

	// Create map of step ID to step index for quick lookup
	stepIDToIndex := make(map[string]int)
	for i, s := range allSteps {
		if s.ID != "" {
			stepIDToIndex[s.ID] = i
		}
	}

	// Create set of completed step indices for quick lookup
	completedSet := make(map[int]bool)
	for _, idx := range progress.CompletedStepIndices {
		completedSet[idx] = true
	}

	// Gather info about each prerequisite rule
	ruleInfos := make([]PrerequisiteRuleInfo, 0, len(prerequisiteRules))
	for _, rule := range prerequisiteRules {
		if rule.DependsOnStep == "" {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Prerequisite rule for step %d has empty depends_on_step, skipping", stepIndex+1))
			continue
		}

		depStepID := rule.DependsOnStep
		depStepIndex, exists := stepIDToIndex[depStepID]
		if !exists {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Dependency step ID %s not found in plan steps", depStepID))
			continue
		}

		// Validate: dependency step must be before current step
		if depStepIndex >= stepIndex {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Dependency step %s (index %d) is not before current step %d, skipping", depStepID, depStepIndex, stepIndex))
			continue
		}

		depStep := allSteps[depStepIndex]
		isCompleted := completedSet[depStepIndex]

		// Check if context output file exists
		contextOutputExists := false
		if depStep.ContextOutput != "" {
			// Resolve context output path
			contextOutputPath := filepath.Join(workspacePath, "execution", depStep.ContextOutput)
			if _, err := os.Stat(contextOutputPath); err == nil {
				contextOutputExists = true
			}
		}

		ruleInfos = append(ruleInfos, PrerequisiteRuleInfo{
			DependsOnStep: depStepID,
			Description:   rule.Description,
			DependencyStepInfo: DependencyStepInfo{
				StepID:              depStepID,
				StepIndex:           depStepIndex,
				StepTitle:           depStep.Title,
				IsCompleted:         isCompleted,
				ContextOutput:       depStep.ContextOutput,
				ContextOutputExists: contextOutputExists,
			},
		})
	}

	if len(ruleInfos) == 0 {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ No valid prerequisite rules found for step %d", stepIndex+1))
		return nil
	}

	return &PrerequisiteInfo{
		CurrentStepID:               step.ID,
		CurrentStepIndex:            stepIndex,
		EnablePrerequisiteDetection: true,
		PrerequisiteRules:           ruleInfos,
	}
}

// formatPrerequisiteRulesForExecutionAgent formats prerequisite rules for the execution agent system prompt
// This provides the LLM with information about available prerequisite rules and when to call the tool
func (hcpo *HumanControlledTodoPlannerOrchestrator) formatPrerequisiteRulesForExecutionAgent(prerequisiteInfo *PrerequisiteInfo) string {
	if prerequisiteInfo == nil || len(prerequisiteInfo.PrerequisiteRules) == 0 {
		return ""
	}

	var result strings.Builder
	result.WriteString("## 🔄 Prerequisite Detection\n\n")
	result.WriteString("**Prerequisite detection is enabled for this step.** If you detect that a prerequisite condition described below is met during execution, you can call the `detect_prerequisite_failure` tool to navigate back to the prerequisite step.\n\n")
	result.WriteString("### Available Prerequisite Rules:\n\n")

	for i, rule := range prerequisiteInfo.PrerequisiteRules {
		result.WriteString(fmt.Sprintf("**Rule %d:**\n", i+1))
		result.WriteString(fmt.Sprintf("- **Step ID**: `%s` (Step %d: %s)\n", rule.DependsOnStep, rule.DependencyStepInfo.StepIndex+1, rule.DependencyStepInfo.StepTitle))
		result.WriteString(fmt.Sprintf("- **Condition**: %s\n", rule.Description))
		result.WriteString("\n")
	}

	result.WriteString("### How to Use:\n")
	result.WriteString("If you detect that one of the prerequisite conditions above is met during execution:\n")
	result.WriteString("1. Call `detect_prerequisite_failure` with:\n")
	result.WriteString("   - `depends_on_step_id`: The step ID from the matching rule (e.g., `\"step-0\"`)\n")
	result.WriteString("   - `reason`: A brief explanation of why the prerequisite failure was detected\n")
	result.WriteString("2. Execution will stop and automatically navigate back to the prerequisite step\n")
	result.WriteString("3. The prerequisite step will be re-executed to restore the missing prerequisite\n\n")
	result.WriteString("**Important**: Only call this tool if you are certain a prerequisite condition is met. If the failure is due to execution issues (not missing prerequisites), continue with normal execution.\n")

	return result.String()
}

// createPrerequisiteDetectionTool creates a tool execution function for prerequisite detection
// The returned function validates the step ID, cancels the execution context, and sends the error via channel
func (hcpo *HumanControlledTodoPlannerOrchestrator) createPrerequisiteDetectionTool(prerequisiteInfo *PrerequisiteInfo, allSteps []TodoStep, currentStepIndex int, cancelFunc context.CancelFunc, prereqErrChan chan<- *PrerequisiteFailureError) func(ctx context.Context, args map[string]interface{}) (string, error) {
	// Create map of step ID to step index for validation
	stepIDToIndex := make(map[string]int)
	for i, s := range allSteps {
		if s.ID != "" {
			stepIDToIndex[s.ID] = i
		}
	}

	// Create map of valid prerequisite step IDs (from rules)
	validPrerequisiteStepIDs := make(map[string]PrerequisiteRuleInfo)
	for _, rule := range prerequisiteInfo.PrerequisiteRules {
		validPrerequisiteStepIDs[rule.DependsOnStep] = rule
	}

	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Extract parameters
		dependsOnStepID, ok := args["depends_on_step_id"].(string)
		if !ok || dependsOnStepID == "" {
			return "", fmt.Errorf("depends_on_step_id parameter is required and must be a non-empty string")
		}

		reason, ok := args["reason"].(string)
		if !ok || reason == "" {
			return "", fmt.Errorf("reason parameter is required and must be a non-empty string")
		}

		// Validate step ID is in prerequisite rules
		_, isValid := validPrerequisiteStepIDs[dependsOnStepID]
		if !isValid {
			validIDs := make([]string, 0, len(validPrerequisiteStepIDs))
			for id := range validPrerequisiteStepIDs {
				validIDs = append(validIDs, id)
			}
			return "", fmt.Errorf("invalid depends_on_step_id: %s. Valid prerequisite step IDs are: %v", dependsOnStepID, validIDs)
		}

		// Get step index
		stepIndex, exists := stepIDToIndex[dependsOnStepID]
		if !exists {
			return "", fmt.Errorf("step ID %s not found in plan steps", dependsOnStepID)
		}

		// Validate: dependency step must be before current step
		if stepIndex >= currentStepIndex {
			return "", fmt.Errorf("prerequisite step %s (index %d) must be before current step %d", dependsOnStepID, stepIndex, currentStepIndex)
		}

		// Check max navigation distance (safety limit: 10 steps)
		navigationDistance := currentStepIndex - stepIndex
		if navigationDistance > 10 {
			return "", fmt.Errorf("navigation distance %d exceeds maximum (10 steps)", navigationDistance)
		}

		// Create prerequisite failure error
		prereqErr := &PrerequisiteFailureError{
			DependsOnStepID: dependsOnStepID,
			StepIndex:       stepIndex,
			Reason:          reason,
		}

		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Prerequisite failure detected via tool call: %s (navigate to step %s, index %d) - stopping execution immediately", reason, dependsOnStepID, stepIndex))

		// Send error to channel (non-blocking)
		select {
		case prereqErrChan <- prereqErr:
		default:
			// Channel full or closed - log warning but continue
			hcpo.GetLogger().Warn("⚠️ Prerequisite error channel full or closed, but continuing with cancellation")
		}

		// Cancel the execution context to stop agent immediately
		if cancelFunc != nil {
			cancelFunc()
		}

		// Return minimal response - execution will be stopped by context cancellation
		return "", nil
	}
}

// buildPreviousStepsSummary builds a formatted summary of previous completed steps
// This provides context to the execution agent about what steps have already been executed
func (hcpo *HumanControlledTodoPlannerOrchestrator) buildPreviousStepsSummary(allSteps []TodoStep, currentStepIndex int, previousContextFiles []string) string {
	if len(allSteps) == 0 || currentStepIndex == 0 || len(previousContextFiles) == 0 {
		return "" // No previous steps
	}

	// Create a map of context output files to step indices for quick lookup
	contextFileToStepIndex := make(map[string]int)
	for i := 0; i < currentStepIndex && i < len(allSteps); i++ {
		if allSteps[i].ContextOutput != "" {
			// Resolve variables in context output to match what's in previousContextFiles
			resolvedOutput := ResolveVariables(allSteps[i].ContextOutput, hcpo.variableValues)
			contextFileToStepIndex[resolvedOutput] = i
		}
	}

	// Build summary for steps that have context outputs in previousContextFiles
	var summary strings.Builder
	summary.WriteString("## 📋 Previous Steps Context\n\n")
	summary.WriteString("The following steps have been completed before this step:\n\n")

	stepCount := 0
	for i := 0; i < currentStepIndex && i < len(allSteps); i++ {
		step := allSteps[i]
		if step.ContextOutput == "" {
			continue // Skip steps without context output
		}

		// Check if this step's context output is in previousContextFiles
		resolvedOutput := ResolveVariables(step.ContextOutput, hcpo.variableValues)
		found := false
		for _, prevFile := range previousContextFiles {
			if prevFile == resolvedOutput {
				found = true
				break
			}
		}

		if !found {
			continue // Skip steps whose context output is not in previousContextFiles
		}

		// Resolve variables in title and description
		resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
		resolvedDescription := ResolveVariables(step.Description, hcpo.variableValues)

		// Truncate description if too long (keep first 200 characters)
		description := resolvedDescription
		if len(description) > 200 {
			description = description[:200] + "..."
		}

		summary.WriteString(fmt.Sprintf("**Step %d: %s**\n", i+1, resolvedTitle))
		summary.WriteString(fmt.Sprintf("- **Description**: %s\n", description))
		summary.WriteString(fmt.Sprintf("- **Output File**: %s\n", resolvedOutput))
		summary.WriteString("\n")

		stepCount++
	}

	if stepCount == 0 {
		return "" // No previous steps with context outputs
	}

	summary.WriteString("Use this context to understand the workflow progression and what has been accomplished so far.\n")

	return summary.String()
}

// executeSingleStep executes a single step with full functionality (execution, validation, learning, human feedback)
// This is a reusable function extracted from runExecutionPhase to support both regular steps and branch steps
func (hcpo *HumanControlledTodoPlannerOrchestrator) executeSingleStep(
	ctx context.Context,
	step TodoStep,
	stepIndex int,
	stepPath string, // e.g., "step-1" or "step-1-if-true-0" for branch steps
	totalSteps int,
	iteration int,
	previousContextFiles []string,
	progress *StepProgress,
	isBranchStep bool, // true if this is a branch step (affects progress tracking)
	execCtx *ExecutionContext, // Execution context with flags (skipHumanInput, fastExecuteMode, etc.)
	allSteps []TodoStep, // All steps in the plan (for prerequisite detection)
	isDecisionInnerStep bool, // true if this is the inner step of a decision step (skips final human feedback on success)
	decisionContext *DecisionContext, // Optional: context from decision step that routed to this step (nil if not routed from decision)
	decisionEvaluationQuestion string, // Optional: evaluation question for decision inner steps (used to format output for LLM evaluation)
	isSubAgent bool, // true if this is a sub-agent from an orchestration step (never requests human feedback)
) (executionResult string, updatedContextFiles []string, err error) {
	// Initialize updated context files as copy of previous context files
	updatedContextFiles = make([]string, len(previousContextFiles))
	copy(updatedContextFiles, previousContextFiles)

	// Emit step_started event
	// Note: Conditional steps emit their own step_started event in executeConditionalStep before calling executeSingleStep for branch steps
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, stepPath, isBranchStep)

	// Clean Downloads folder before step execution to ensure clean state
	execManager := hcpo.GetExecutionManager()
	if err := execManager.CleanupDownloadsFolder(ctx); err != nil {
		// Non-blocking: log warning but continue execution
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Downloads folder cleanup failed: %v (continuing with step execution)", err))
	}

	// Initialize variables for step execution
	maxRetryAttempts := 5
	var executionConversationHistory []llmtypes.MessageContent // Only used for learning agents after execution
	stepCompleted := false

	// Outer loop: Handle re-execution with human feedback
	for !stepCompleted {
		// Check for context cancellation before retry
		select {
		case <-ctx.Done():
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled during retry loop for step %d", stepIndex+1))
			return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("step execution canceled: %w", ctx.Err()), nil)
		default:
		}

		// Prepare template variables for this specific step with individual fields
		// RESOLVE VARIABLES: Replace {{VARS}} with actual values for execution
		// Execution agent workspace path includes run folder: workspacePath/runs/{selectedRunFolder}/execution
		runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
		// Determine code execution mode: Priority: step config > preset default
		var isCodeExecutionMode bool
		if step.AgentConfigs != nil && step.AgentConfigs.UseCodeExecutionMode != nil {
			isCodeExecutionMode = *step.AgentConfigs.UseCodeExecutionMode
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode: %v", isCodeExecutionMode))
		} else {
			isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset code execution mode: %v", isCodeExecutionMode))
		}
		// Always use learnings folder (unified folder for all learning types)
		learningsPath := fmt.Sprintf("%s/learnings", hcpo.GetWorkspacePath())
		// Get execution folder path for this step (e.g., "execution/step-8" or "execution/step-3-true-0")
		stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, stepPath)

		templateVars := map[string]string{
			"StepTitle":           ResolveVariables(step.Title, hcpo.variableValues),
			"StepDescription":     ResolveVariables(step.Description, hcpo.variableValues),
			"StepSuccessCriteria": ResolveVariables(step.SuccessCriteria, hcpo.variableValues),
			"StepContextOutput":   ResolveVariables(step.ContextOutput, hcpo.variableValues),
			"WorkspacePath":       executionWorkspacePath,                 // Execution subdirectory (folder guard validates against this)
			"LearningsPath":       learningsPath,                          // Learnings folder path for reading learning files and scripts/code
			"IsCodeExecutionMode": fmt.Sprintf("%v", isCodeExecutionMode), // Code execution mode flag (step-specific or preset)
			"HumanFeedback":       "",                                     // Human feedback for retry attempts (set after validation failure)
			"StepNumber":          stepPath,                               // Step identifier (e.g., "step-8" or "step-3-if-true-0")
			"StepExecutionPath":   stepExecutionPath,                      // Full execution folder path (e.g., "execution/step-8")
		}

		// Add context dependencies as a comma-separated string (also resolve variables)
		if len(step.ContextDependencies) > 0 {
			resolvedDeps := ResolveVariablesArray(step.ContextDependencies, hcpo.variableValues)
			templateVars["StepContextDependencies"] = strings.Join(resolvedDeps, ", ")
		} else {
			templateVars["StepContextDependencies"] = ""
		}

		// Add variable names if available (same format as other agents)
		if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
			templateVars["VariableNames"] = variableNames
		}

		// Add variable values if available (name = value - description format)
		if variableValues := FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues); variableValues != "" {
			templateVars["VariableValues"] = variableValues
		}

		// Add decision evaluation question if this is a decision inner step
		if isDecisionInnerStep && decisionEvaluationQuestion != "" {
			templateVars["DecisionEvaluationQuestion"] = decisionEvaluationQuestion
		} else {
			templateVars["DecisionEvaluationQuestion"] = ""
		}

		// Add decision context if this step was routed from a decision step
		if decisionContext != nil {
			decisionReasoning := fmt.Sprintf(
				"## 🎯 Decision Context\n\n"+
					"This step was routed from decision step **%d: %s**.\n\n"+
					"**Decision Result**: %v\n"+
					"**Decision Reasoning**: %s\n\n"+
					"## 📋 Decision Step Execution Output\n\n"+
					"The following is the execution output from the decision step's inner step that was evaluated:\n\n"+
					"```\n%s\n```\n\n"+
					"Use this context to understand why this step is being executed and what conditions led to routing here.",
				decisionContext.DecisionStepIndex+1, // Convert to 1-based for display
				decisionContext.DecisionStepTitle,
				decisionContext.DecisionResult,
				decisionContext.DecisionReasoning,
				decisionContext.DecisionExecutionResult,
			)
			templateVars["DecisionReasoning"] = decisionReasoning
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Added decision context to template variables for step %d (routed from decision step %d)", stepIndex+1, decisionContext.DecisionStepIndex+1))
		} else {
			templateVars["DecisionReasoning"] = ""
		}

		// Build previous steps summary from completed steps
		previousStepsSummary := hcpo.buildPreviousStepsSummary(allSteps, stepIndex, previousContextFiles)
		templateVars["PreviousStepsSummary"] = previousStepsSummary
		if previousStepsSummary != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Added previous steps summary to template variables for step %d (%d previous steps)", stepIndex+1, len(previousContextFiles)))
		}

		// Validate loop condition is provided when has_loop is true
		if hasLoop(step) {
			if step.LoopCondition == "" {
				return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("step %d has has_loop=true but loop_condition is empty (required)", stepIndex+1), nil)
			}
			// Set default max_iterations if not provided
			if step.MaxIterations == 0 {
				step.MaxIterations = 10
				hcpo.GetLogger().Info(fmt.Sprintf("⚠️ Step %d has loop but no max_iterations specified, using default: 10", stepIndex+1))
			}
		}

		// Inner loop: Automatic retry logic
		var validationResponse *ValidationResponse
		var previousValidationResponse *ValidationResponse // Preserve previous validation response for retry detection (works for both retries and loop iterations)

		// Loop handling: if step has loop, wrap execution in loop that checks loop condition
		var loopConditionMet bool
		var loopIterationCount int
		// Store previous iteration's execution and validation outputs for loop feedback
		var previousIterationExecutionOutput string
		var previousIterationValidationOutput string

		// Track validation counter for numbered validation files (validation-1.json, validation-2.json, etc.)
		// Persists across loop iterations and retry attempts
		validationCounter := 0

		// Main execution loop (either single execution or loop iterations)
		// For non-loop steps, this executes once. For loop steps, it iterates until condition is met.
		// NOTE: No conversation history is passed to execution agent - all context via template variables
		for loopIteration := 0; ; loopIteration++ {
			// Check for context cancellation before each iteration
			select {
			case <-ctx.Done():
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled during loop iteration %d for step %d", loopIteration, stepIndex+1))
				return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("step execution canceled: %w", ctx.Err()), nil)
			default:
			}

			// Initialize loop state on first iteration
			if loopIteration == 0 && hasLoop(step) {
				loopConditionMet = false
				loopIterationCount = 0
				previousIterationExecutionOutput = ""
				previousIterationValidationOutput = ""
				hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step %d loop starting (max iterations: %d, condition: %s)", stepIndex+1, step.MaxIterations, step.LoopCondition))
			} else if loopIteration > 0 && hasLoop(step) {
				// Previous iteration outputs are passed via template variables (PreviousIterationOutput)
				// Execution conversation history will be captured fresh from this iteration for learning agents
				hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step %d loop iteration %d/%d starting", stepIndex+1, loopIterationCount, step.MaxIterations))
			}

			// Check loop exit conditions (only for loop steps)
			if hasLoop(step) {
				if loopConditionMet {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d loop condition met after %d iterations, exiting loop", stepIndex+1, loopIterationCount))
					// Skip validation, mark as completed
					validationResponse = &ValidationResponse{
						IsSuccessCriteriaMet: true,
						ExecutionStatus:      "COMPLETED",
						Reasoning:            fmt.Sprintf("Loop condition met after %d iterations. Validation skipped per loop exit.", loopIterationCount),
					}
					break // Exit main loop - proceed to mark as completed
				}
				if loopIterationCount >= step.MaxIterations {
					hcpo.GetLogger().Error(fmt.Sprintf("❌ Step %d reached max iterations (%d) without meeting loop condition, requesting human intervention", stepIndex+1, step.MaxIterations), nil)
					// Request human intervention immediately, skip validation
					var err error
					var approved bool
					approved, _, err = hcpo.requestHumanFeedback(ctx, stepIndex+1, totalSteps,
						fmt.Sprintf("Loop reached max iterations (%d) without meeting condition: %s", step.MaxIterations, step.LoopCondition))
					if err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Human feedback request failed: %w", err))
						// Default to not approved so step doesn't complete
						approved = false
					}
					if approved {
						// User approved - treat as completed despite max iterations
						hcpo.GetLogger().Info(fmt.Sprintf("✅ User approved step %d despite max iterations, marking as completed", stepIndex+1))
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: true,
							ExecutionStatus:      "COMPLETED",
							Reasoning:            "User approved completion despite max iterations reached",
						}
						loopConditionMet = true // Mark condition as met so loop exits
						break                   // Exit main loop
					} else {
						// User rejected - will re-execute step
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 User rejected approval, will re-execute step %d", stepIndex+1))
						break // Exit main loop; outer loop will re-execute since stepCompleted is still false
					}
				}
				loopIterationCount++
				hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step %d loop iteration %d/%d", stepIndex+1, loopIterationCount, step.MaxIterations))
			}

			// Add loop context to template variables if in loop mode
			if hasLoop(step) {
				templateVars["HasLoop"] = "true"
				templateVars["LoopCondition"] = step.LoopCondition
				templateVars["LoopDescription"] = step.LoopDescription
				templateVars["CurrentIteration"] = fmt.Sprintf("%d", loopIterationCount)
				templateVars["MaxIterations"] = fmt.Sprintf("%d", step.MaxIterations)
				// Add previous iteration execution and validation outputs for loop steps (after iteration 1)
				if loopIterationCount > 1 && (previousIterationExecutionOutput != "" || previousIterationValidationOutput != "") {
					var combinedOutput strings.Builder
					if previousIterationExecutionOutput != "" {
						combinedOutput.WriteString("## Previous Loop Iteration Execution Output:\n")
						combinedOutput.WriteString(previousIterationExecutionOutput)
						combinedOutput.WriteString("\n\n")
					}
					if previousIterationValidationOutput != "" {
						combinedOutput.WriteString("## Previous Loop Iteration Validation Output:\n")
						combinedOutput.WriteString(previousIterationValidationOutput)
					}
					templateVars["PreviousIterationOutput"] = combinedOutput.String()
					hcpo.GetLogger().Info(fmt.Sprintf("📝 Added previous iteration outputs to template variables for step %d (loop iteration %d)", stepIndex+1, loopIterationCount))
				} else {
					templateVars["PreviousIterationOutput"] = ""
				}
			} else {
				templateVars["HasLoop"] = "false"
				templateVars["LoopCondition"] = ""
				templateVars["LoopDescription"] = ""
				templateVars["CurrentIteration"] = ""
				templateVars["MaxIterations"] = ""
				templateVars["PreviousIterationOutput"] = ""
			}

			// Resolve variables in step title before using in agent name
			resolvedTitle := ResolveVariables(step.Title, hcpo.variableValues)
			sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)

			// Run learning reading agent ONCE per main loop iteration (before retry loop)
			// This ensures learning is only discovered once, even if validation fails and we retry
			// Always reads fresh learnings (no caching)
			var formattedLearningHistory string
			formattedLearningHistory, err = hcpo.readLearningHistory(
				ctx,
				stepIndex,
				step.ID,
				stepPath,
			)
			if err != nil {
				return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("failed to read learning history for step %d: %w", stepIndex+1, err), nil)
			}

			// Track if validation failed after exhausting all retry attempts
			validationFailedAfterMaxRetries := false

			// Track which tempLLM was used during successful execution (for learning phase decision)
			var usedTempLLM string // "tempLLM1", "tempLLM2", or "" (original LLM)

			// Retry loop: Execute with validation feedback, reusing the same learning history
			for retryAttempt := 1; retryAttempt <= maxRetryAttempts; retryAttempt++ {
				// Check for context cancellation before retry attempt
				select {
				case <-ctx.Done():
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled during retry attempt %d for step %d", retryAttempt, stepIndex+1))
					return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("step execution canceled: %w", ctx.Err()), nil)
				default:
				}

				hcpo.GetLogger().Info(fmt.Sprintf("🔄 Executing step %d/%d (attempt %d/%d): %s", stepIndex+1, totalSteps, retryAttempt, maxRetryAttempts, step.Title))

				// Add validation feedback to template variables if this is a retry or loop iteration
				if (retryAttempt > 1 || (hasLoop(step) && loopIterationCount > 1)) && validationResponse != nil {
					var contextStr string
					if retryAttempt > 1 {
						contextStr = fmt.Sprintf("Validation Feedback (Retry Attempt %d)", retryAttempt)
					} else if hasLoop(step) && loopIterationCount > 1 {
						contextStr = fmt.Sprintf("Validation Feedback (Loop Iteration %d)", loopIterationCount-1)
					} else {
						contextStr = "Validation Feedback"
					}
					templateVars["ValidationFeedback"] = hcpo.formatValidationResponseForTemplate(validationResponse, contextStr)
					hcpo.GetLogger().Info(fmt.Sprintf("📝 Added validation feedback to template variables for step %d (retry: %d, loop iteration: %d)", stepIndex+1, retryAttempt, loopIterationCount))
				} else {
					templateVars["ValidationFeedback"] = "" // No validation feedback for first attempt/first iteration
				}

				// Note: HumanFeedback is set in templateVars after validation failure (see validation failure handling above)
				// It persists across retry attempts until cleared or step succeeds

				// Step 2: Create and execute Execution-Only Agent with learning history (reused from above)
				hcpo.GetLogger().Info(fmt.Sprintf("🔍 [AGENT NAME] Generating agent name with stepPath: %s (isBranchStep=%v)", stepPath, isBranchStep))
				executionAgentName := fmt.Sprintf("%s-execution-%s", stepPath, sanitizedTitle)
				// Add loop iteration to agent name if in loop mode
				if hasLoop(step) && loopIterationCount > 0 {
					executionAgentName = fmt.Sprintf("%s-loop-%d", executionAgentName, loopIterationCount)
				}
				hcpo.GetLogger().Info(fmt.Sprintf("🔍 [AGENT NAME] Final executionAgentName: %s", executionAgentName))

				// Add learning history to template vars for execution-only agent (reused for all retry attempts)
				templateVars["LearningHistory"] = formattedLearningHistory

				// Check for context cancellation before creating execution agent
				select {
				case <-ctx.Done():
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled before creating execution agent for step %d", stepIndex+1))
					return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("step execution canceled: %w", ctx.Err()), nil)
				default:
				}

				var executionAgent agents.OrchestratorAgent
				// Determine if this is a retry after validation failure
				// If validation failed on the previous attempt (even once), use original LLM instead of temp override
				// Works for both:
				// 1. Retry attempts within the same loop iteration (retryAttempt > 1)
				// 2. New loop iterations after a previous iteration failed validation (loopIterationCount > 1 for loop steps)
				// 3. Steps routed from decision step with false result (similar to validation failure - skip tempLLM)
				// Note: For tempLLM logic, only FAILED status counts as failure - COMPLETED/PARTIAL/INCOMPLETE are considered success
				isRetryAfterValidationFailure := isValidationFailure(previousValidationResponse) &&
					(retryAttempt > 1 || (hasLoop(step) && loopIterationCount > 1))
				// Also treat decision step false result as validation failure (skip tempLLM)
				isDecisionStepFalse := decisionContext != nil && !decisionContext.DecisionResult
				if isDecisionStepFalse {
					isRetryAfterValidationFailure = true
					hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step routed from decision step with FALSE result - will skip tempLLM (treating as validation failure)"))
				}
				if isRetryAfterValidationFailure && hcpo.fallbackToOriginalLLMOnFailure {
					hcpo.GetLogger().Info(fmt.Sprintf("🔄 Validation failed on previous attempt - will use original LLM instead of temp override (fallback enabled)"))
				}
				hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Retry check - retryAttempt=%d, previousValidationResponse!=nil=%v, previousIsSuccessCriteriaMet=%v, isRetryAfterValidationFailure=%v, fallbackToOriginalLLMOnFailure=%v",
					retryAttempt, previousValidationResponse != nil, func() bool {
						if previousValidationResponse != nil {
							return previousValidationResponse.IsSuccessCriteriaMet
						}
						return false
					}(), isRetryAfterValidationFailure, hcpo.fallbackToOriginalLLMOnFailure))
				// Gather prerequisite info if enabled (needed for tool registration and prompt)
				var prerequisiteInfoForExecution *PrerequisiteInfo

				// Prefer AgentConfigs flag if present; otherwise fall back to implicit enablement
				// when prerequisite rules exist at the AgentConfigs level. TodoStep does not carry
				// the top-level planning fields (EnablePrerequisiteDetection / PrerequisiteRules),
				// so at execution time we rely on AgentConfigs only.
				enablePrereq := false
				if step.AgentConfigs != nil && step.AgentConfigs.EnablePrerequisiteDetection != nil {
					enablePrereq = *step.AgentConfigs.EnablePrerequisiteDetection
				} else if step.AgentConfigs != nil && len(step.AgentConfigs.PrerequisiteRules) > 0 {
					enablePrereq = true
				}

				if enablePrereq {
					var validationWorkspacePath string
					if hcpo.selectedRunFolder != "" {
						validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
					} else {
						validationWorkspacePath = hcpo.GetWorkspacePath()
					}

					prerequisiteInfoForExecution = hcpo.gatherPrerequisiteInfo(step, stepIndex, allSteps, progress, validationWorkspacePath)

					// Add prerequisite rules info to template variables for execution agent prompt
					if prerequisiteInfoForExecution != nil {
						templateVars["PrerequisiteRulesInfo"] = hcpo.formatPrerequisiteRulesForExecutionAgent(prerequisiteInfoForExecution)
					} else {
						templateVars["PrerequisiteRulesInfo"] = ""
					}
				} else {
					templateVars["PrerequisiteRulesInfo"] = ""
				}

				// Create cancellable context for execution agent (to allow immediate stop on prerequisite failure)
				executionCtx, cancelExecution := context.WithCancel(ctx)
				defer cancelExecution()

				// Channel to receive prerequisite failure errors from tool
				prereqErrChan := make(chan *PrerequisiteFailureError, 1)

				// Pass stepPath to createExecutionOnlyAgent - it will determine the correct execution folder (supports branch and sub-agent steps)
				// For learnings / tempLLM selection, use the concrete step ID so sub-agents align with their own learnings folder.
				executionAgent, err = hcpo.createExecutionOnlyAgent(executionCtx, "execution_only", stepPath, executionAgentName, step.AgentConfigs, isRetryAfterValidationFailure, retryAttempt, prerequisiteInfoForExecution, allSteps, stepIndex, cancelExecution, prereqErrChan, step.ID)
				if err != nil {
					return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("failed to create execution-only agent for step %d: %w", stepIndex+1, err), nil)
				}

				// Check for context cancellation before executing agent
				select {
				case <-ctx.Done():
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled before agent execution for step %d", stepIndex+1))
					return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("step execution canceled: %w", ctx.Err()), nil)
				default:
				}

				// Execute execution-only agent with learning history (reused from learning reading above)
				executionResult, executionConversationHistory, err = executionAgent.Execute(executionCtx, templateVars, []llmtypes.MessageContent{})

				// Check for prerequisite failure (from tool call via channel)
				var prereqErr *PrerequisiteFailureError
				select {
				case prereqErr = <-prereqErrChan:
					// Prerequisite failure detected - tool called and context was cancelled
					hcpo.GetLogger().Info(fmt.Sprintf("🔄 Prerequisite failure detected via tool call for step %d: %s (target step: %d)", stepIndex+1, prereqErr.Reason, prereqErr.StepIndex+1))
				default:
					// No prerequisite failure - check for other errors
					if err != nil {
						// Check if this is a prerequisite failure error (legacy check - should not happen with new implementation)
						var legacyPrereqErr *PrerequisiteFailureError
						if errors.As(err, &legacyPrereqErr) {
							prereqErr = legacyPrereqErr
						}
					}
				}

				if prereqErr != nil {
					// Prerequisite failure detected via tool call - trigger navigation
					targetStepIndex := prereqErr.StepIndex
					retryReason := prereqErr.Reason

					// Validate target step
					if targetStepIndex < 0 {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Invalid target step index %d (must be >= 0), ignoring navigation", targetStepIndex))
					} else if targetStepIndex >= stepIndex {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Invalid target step index %d (must be before current step %d), ignoring navigation", targetStepIndex, stepIndex))
					} else if allSteps != nil && targetStepIndex >= len(allSteps) {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Invalid target step index %d (exceeds total steps %d), ignoring navigation", targetStepIndex, len(allSteps)))
					} else {
						// Check max navigation distance (safety limit: 10 steps)
						navigationDistance := stepIndex - targetStepIndex
						if navigationDistance > 10 {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Navigation distance %d exceeds maximum (10 steps), ignoring navigation", navigationDistance))
						} else {
							// Clean up progress from target step onward
							if err := hcpo.cleanupProgressFromStep(ctx, targetStepIndex, progress); err != nil {
								hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to cleanup progress from step %d: %v", targetStepIndex+1, err), nil)
								return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("failed to cleanup progress for prerequisite navigation: %w", err), nil)
							}

							// Emit prerequisite navigation event
							eventBridge := hcpo.GetContextAwareBridge()
							if eventBridge != nil {
								navigationEvent := &events.PrerequisiteNavigationEvent{
									BaseEventData: events.BaseEventData{
										Timestamp: time.Now(),
										Component: "orchestrator",
									},
									FromStepIndex: stepIndex,
									ToStepIndex:   targetStepIndex,
									Reason:        retryReason,
									FailureType:   "prerequisite",
								}
								eventBridge.HandleEvent(ctx, &events.AgentEvent{
									Type:      events.PrerequisiteNavigation,
									Timestamp: time.Now(),
									Data:      navigationEvent,
								})
								hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted prerequisite_navigation event: step %d → step %d (%s)", stepIndex+1, targetStepIndex+1, retryReason))
							}

							// Return navigation error to restart from target step
							return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("prerequisite failure detected: %s (navigate to step %d)", retryReason, targetStepIndex+1), nil)
						}
					}
					// If navigation validation failed, fall through to normal error handling
				} else if err != nil {
					// Other execution errors (not prerequisite failure)
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step %d execution failed (attempt %d): %v", stepIndex+1, retryAttempt, err))
					if retryAttempt >= maxRetryAttempts {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ Step %d execution failed after %d attempts, exiting retry loop", stepIndex+1, maxRetryAttempts), nil)
						break // Exit retry loop - will proceed to human feedback
					}
					continue // Retry on next attempt
				}

				hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d execution completed successfully (attempt %d)", stepIndex+1, retryAttempt))

				// Store execution response to workspace (if enabled)
				if hcpo.saveValidationResponses {
					// Determine validation workspace path (same logic as validation agent)
					var validationWorkspacePath string
					if hcpo.selectedRunFolder != "" {
						validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
					} else {
						validationWorkspacePath = hcpo.GetWorkspacePath()
					}

					// Get execution logs folder path based on stepPath
					executionLogsFolderPath := getExecutionFolderPathForLogs(validationWorkspacePath, stepPath)

					// Create unique filename base based on retry attempt and loop iteration
					// Format: execution-attempt-{retryAttempt}-iteration-{loopIterationCount}
					filenameBase := fmt.Sprintf("execution-attempt-%d-iteration-%d", retryAttempt, loopIterationCount)

					// Save execution result to separate file
					executionResultFilePath := fmt.Sprintf("%s/%s.json", executionLogsFolderPath, filenameBase)
					executionResponse := map[string]interface{}{
						"step_index":       stepIndex + 1,
						"step_path":        stepPath,
						"retry_attempt":    retryAttempt,
						"loop_iteration":   loopIterationCount,
						"execution_result": executionResult,
						"timestamp":        time.Now().Format(time.RFC3339),
					}

					// Marshal and save execution result
					executionJSON, err := json.MarshalIndent(executionResponse, "", "  ")
					if err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal execution response to JSON: %v", err))
					} else {
						if err := hcpo.WriteWorkspaceFile(ctx, executionResultFilePath, string(executionJSON)); err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write execution response to %s: %v", executionResultFilePath, err))
						} else {
							hcpo.GetLogger().Info(fmt.Sprintf("💾 Execution response saved to: %s", executionResultFilePath))
						}
					}

					// Save conversation history to separate file
					conversationFilePath := fmt.Sprintf("%s/%s-conversation.json", executionLogsFolderPath, filenameBase)
					conversationResponse := map[string]interface{}{
						"step_index":           stepIndex + 1,
						"step_path":            stepPath,
						"retry_attempt":        retryAttempt,
						"loop_iteration":       loopIterationCount,
						"conversation_history": executionConversationHistory, // Store original JSON structure
						"timestamp":            time.Now().Format(time.RFC3339),
					}

					// Marshal and save conversation history
					conversationJSON, err := json.MarshalIndent(conversationResponse, "", "  ")
					if err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal conversation history to JSON: %v", err))
					} else {
						if err := hcpo.WriteWorkspaceFile(ctx, conversationFilePath, string(conversationJSON)); err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write conversation history to %s: %v", conversationFilePath, err))
						} else {
							hcpo.GetLogger().Info(fmt.Sprintf("💾 Conversation history saved to: %s", conversationFilePath))
						}
					}
				}

				// Check if validation is disabled for this step
				disableValidation := step.AgentConfigs != nil && step.AgentConfigs.DisableValidation != nil && *step.AgentConfigs.DisableValidation
				if disableValidation {
					hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Validation disabled for step %d - auto-approving (learning will still run)", stepIndex+1))
					// Auto-approve: create a success validation response
					// NOTE: Validation being disabled does NOT prevent learning from running
					validationResponse = &ValidationResponse{
						IsSuccessCriteriaMet: true,
						ExecutionStatus:      "COMPLETED",
						Reasoning:            "Validation disabled - step auto-approved",
					}
					if hasLoop(step) {
						// For loop steps, mark condition as met when validation is disabled
						validationResponse.LoopConditionMet = true
						loopConditionMet = true
					}
				} else {
					// Always validate step execution
					hcpo.GetLogger().Info(fmt.Sprintf("🔍 Validating step %d execution (attempt %d)", stepIndex+1, retryAttempt))

					// Reuse sanitized title from execution agent (already computed above)
					validationAgentName := fmt.Sprintf("%s-validation-%s", stepPath, sanitizedTitle)
					// Add loop iteration to validation agent name if in loop mode
					if hasLoop(step) && loopIterationCount > 0 {
						validationAgentName = fmt.Sprintf("%s-loop-%d", validationAgentName, loopIterationCount)
					}
					validationAgent, err := hcpo.createValidationAgent(ctx, "validation", stepIndex+1, validationAgentName, step.AgentConfigs)
					if err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create validation agent for step %d: %v", stepIndex+1, err))
						if retryAttempt >= maxRetryAttempts {
							break // Exit retry loop - will proceed to human feedback
						}
						continue // Retry on next attempt
					}

					// Prepare validation template variables with individual fields
					// Use run folder path if available
					var validationWorkspacePath string
					if hcpo.selectedRunFolder != "" {
						validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
					} else {
						validationWorkspacePath = hcpo.GetWorkspacePath()
					}
					validationTemplateVars := map[string]string{
						"StepTitle":           step.Title,
						"StepDescription":     step.Description,
						"StepSuccessCriteria": step.SuccessCriteria,
						"StepContextOutput":   step.ContextOutput,
						"WorkspacePath":       validationWorkspacePath,
						"ExecutionHistory":    shared.FormatConversationHistory(executionConversationHistory),
					}

					// Add context dependencies as a comma-separated string
					if len(step.ContextDependencies) > 0 {
						validationTemplateVars["StepContextDependencies"] = strings.Join(step.ContextDependencies, ", ")
					} else {
						validationTemplateVars["StepContextDependencies"] = ""
					}

					// If in loop mode, pass loop condition to validation agent
					if hasLoop(step) {
						validationTemplateVars["LoopCondition"] = step.LoopCondition
						hcpo.GetLogger().Info(fmt.Sprintf("🔍 Checking loop condition for step %d (iteration %d): %s", stepIndex+1, loopIterationCount, step.LoopCondition))
					} else {
						validationTemplateVars["LoopCondition"] = ""
					}

					// Add decision context if this step was routed from a decision step
					if decisionContext != nil {
						decisionReasoning := fmt.Sprintf(
							"## 🎯 Decision Context\n\n"+
								"This step was routed from decision step **%d: %s**.\n\n"+
								"**Decision Result**: %v\n"+
								"**Decision Reasoning**: %s\n\n"+
								"## 📋 Decision Step Execution Output\n\n"+
								"The following is the execution output from the decision step's inner step that was evaluated:\n\n"+
								"```\n%s\n```\n\n"+
								"Use this context to understand why this step is being executed and what conditions led to routing here.",
							decisionContext.DecisionStepIndex+1, // Convert to 1-based for display
							decisionContext.DecisionStepTitle,
							decisionContext.DecisionResult,
							decisionContext.DecisionReasoning,
							decisionContext.DecisionExecutionResult,
						)
						validationTemplateVars["DecisionReasoning"] = decisionReasoning
						hcpo.GetLogger().Info(fmt.Sprintf("📝 Added decision context to validation template variables for step %d (routed from decision step %d)", stepIndex+1, decisionContext.DecisionStepIndex+1))
					} else {
						validationTemplateVars["DecisionReasoning"] = ""
					}

					// Prerequisite detection is handled by execution agent tool (detect_prerequisite_failure)
					// No need to pass prerequisite info to validation agent

					// Check for context cancellation before validation
					select {
					case <-ctx.Done():
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled before validation for step %d", stepIndex+1))
						return "", updatedContextFiles, fmt.Errorf(fmt.Sprintf("step execution canceled: %w", ctx.Err()), nil)
					default:
					}

					// Validate this step's execution using structured output
					validationResponse, _, err = validationAgent.(*HumanControlledTodoPlannerValidationAgent).ExecuteStructured(ctx, validationTemplateVars, []llmtypes.MessageContent{})
					if err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step %d validation failed (attempt %d): %v", stepIndex+1, retryAttempt, err))
						if retryAttempt >= maxRetryAttempts {
							break // Exit retry loop - will proceed to human feedback with nil validationResponse
						}
						continue // Retry on next attempt
					}

					hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d validation completed successfully (attempt %d)", stepIndex+1, retryAttempt))

					// Store validation response to workspace (if enabled)
					if validationResponse != nil && hcpo.saveValidationResponses {
						// Increment validation counter for numbered files
						validationCounter++
						hcpo.GetLogger().Info(fmt.Sprintf("📊 Validation result: Success Criteria Met: %v, Status: %s", validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus))
						// Determine validation workspace path (same logic as validation agent)
						var validationWorkspacePath string
						if hcpo.selectedRunFolder != "" {
							validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
						} else {
							validationWorkspacePath = hcpo.GetWorkspacePath()
						}

						// Get validation folder path based on stepPath
						validationFolderPath := getValidationFolderPath(validationWorkspacePath, stepPath)
						// Use numbered filename for multiple validations (validation-1.json, validation-2.json, etc.)
						var validationFilePath string
						if validationCounter == 1 {
							validationFilePath = fmt.Sprintf("%s/validation.json", validationFolderPath)
						} else {
							validationFilePath = fmt.Sprintf("%s/validation-%d.json", validationFolderPath, validationCounter)
						}

						// Marshal validation response to JSON
						validationJSON, err := json.MarshalIndent(validationResponse, "", "  ")
						if err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal validation response to JSON: %v", err))
						} else {
							// Write validation response to file
							if err := hcpo.WriteWorkspaceFile(ctx, validationFilePath, string(validationJSON)); err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write validation response to %s: %v", validationFilePath, err))
							} else {
								hcpo.GetLogger().Info(fmt.Sprintf("💾 Validation response saved to: %s", validationFilePath))
							}
						}
					}
				}

				// Prerequisite detection is now handled by execution agent tool (detect_prerequisite_failure)
				// No separate prerequisite detection agent needed - tool call stops execution immediately

				// If in loop mode, check loop condition instead of full validation
				if hasLoop(step) {
					// Check loop condition from validation response
					if validationResponse.LoopConditionMet {
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d loop condition met (iteration %d)", stepIndex+1, loopIterationCount))
						loopConditionMet = true

						// Track which tempLLM was used (for learning phase decision)
						// Determine based on retryAttempt and which tempLLMs exist
						hasTempLLM1 := hcpo.tempOverrideLLM != nil && hcpo.tempOverrideLLM.Provider != "" && hcpo.tempOverrideLLM.ModelID != ""
						hasTempLLM2 := hcpo.tempOverrideLLM2 != nil && hcpo.tempOverrideLLM2.Provider != "" && hcpo.tempOverrideLLM2.ModelID != ""
						if retryAttempt == 1 && hasTempLLM1 {
							usedTempLLM = "tempLLM1"
						} else if retryAttempt == 2 && hasTempLLM2 {
							usedTempLLM = "tempLLM2"
						} else {
							usedTempLLM = "" // Original LLM
						}

						// Run success learning when loop completes successfully (before breaking)
						// FAST MODE & LEARNING DISABLED: Skip learning agents entirely
						isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
						// Check step-specific learning detail level
						isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning
						isLearningDetailLevelNone := false
						if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
							isLearningDetailLevelNone = true
						}
						isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
						// CODE EXECUTION MODE: Force learning enabled regardless of step config
						// Use step-level code execution mode (already computed above)
						if isCodeExecutionMode && isLearningDisabled {
							hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing learning for step %d (overriding step config)", stepIndex+1))
							isLearningDisabled = false
						}
						// TEMP LLM OVERRIDE: Check if learning should be skipped based on which tempLLM was used (controlled by frontend flags)
						shouldSkipLearningDueToTempOverride := false
						if hcpo.executionOptions != nil && usedTempLLM != "" {
							if usedTempLLM == "tempLLM1" && hcpo.executionOptions.SkipLearningWhenTempLLM1 {
								shouldSkipLearningDueToTempOverride = true
								hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM1 was used and SkipLearningWhenTempLLM1 flag is enabled - will skip learning for step %d", stepIndex+1))
							} else if usedTempLLM == "tempLLM2" && hcpo.executionOptions.SkipLearningWhenTempLLM2 {
								shouldSkipLearningDueToTempOverride = true
								hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM2 was used and SkipLearningWhenTempLLM2 flag is enabled - will skip learning for step %d", stepIndex+1))
							}
						}
						hcpo.GetLogger().Info(fmt.Sprintf("🔍 DEBUG: Step %d (loop) - fastExecuteMode=%v, fastExecuteEndStep=%d, isFastExecuteStep=%v, isLearningDisabled=%v (detailLevelNone=%v, stepDisabled=%v, codeExecutionMode=%v), usedTempLLM=%v, skipLearningWhenTempLLM1=%v, skipLearningWhenTempLLM2=%v, shouldSkipLearningDueToTempOverride=%v", stepIndex+1, execCtx.FastExecuteMode, execCtx.FastExecuteEndStep, isFastExecuteStep, isLearningDisabled, isLearningDetailLevelNone, isLearningDisabledStep, isCodeExecutionMode, usedTempLLM, hcpo.executionOptions != nil && hcpo.executionOptions.SkipLearningWhenTempLLM1, hcpo.executionOptions != nil && hcpo.executionOptions.SkipLearningWhenTempLLM2, shouldSkipLearningDueToTempOverride))
						if !isFastExecuteStep && !isLearningDisabled && !shouldSkipLearningDueToTempOverride {
							// Success Learning Agent - analyze what worked well and update plan.json
							// Loop condition met means step completed successfully
							learningPathIdentifier := getLearningPathIdentifier(step.ID, stepPath)
							hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running success learning analysis for %s (loop completed)", stepPath))
							err := hcpo.runSuccessLearningPhase(ctx, stepIndex, stepPath, learningPathIdentifier, totalSteps, &step, executionConversationHistory, validationResponse, isCodeExecutionMode)
							if err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Success learning phase failed for %s: %v", stepPath, err))
							} else {
								hcpo.GetLogger().Info(fmt.Sprintf("✅ Success learning analysis completed for step %d", stepIndex+1))
							}
						} else {
							if isFastExecuteStep {
								hcpo.GetLogger().Info(fmt.Sprintf("⚡ Fast mode: Skipping learning agents for step %d", stepIndex+1))
							} else if isLearningDisabled {
								hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Learning disabled: Skipping learning agents for step %d", stepIndex+1))
							} else if shouldSkipLearningDueToTempOverride {
								hcpo.GetLogger().Info(fmt.Sprintf("🔧 %s was used and skip learning flag enabled: Skipping learning agents for step %d", usedTempLLM, stepIndex+1))
								// Emit learning skipped event
								eventBridge := hcpo.GetContextAwareBridge()
								if eventBridge != nil {
									stepTitle := step.Title
									if stepTitle == "" {
										stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
									}
									stepId := step.ID
									if stepId == "" {
										stepId = fmt.Sprintf("step-%d", stepIndex+1)
									}
									learningSkippedEvent := &events.LearningSkippedEvent{
										BaseEventData: events.BaseEventData{
											Timestamp: time.Now(),
											Component: "orchestrator",
										},
										StepID:          stepId,
										StepIndex:       stepIndex,
										StepTitle:       stepTitle,
										StepPath:        stepPath,
										IsBranchStep:    false, // Loop condition met is not a branch step
										Reason:          "temp_llm_override",
										TempLLMProvider: hcpo.tempOverrideLLM.Provider,
										TempLLMModel:    hcpo.tempOverrideLLM.ModelID,
										RunFolder:       hcpo.selectedRunFolder,
										WorkspacePath:   hcpo.GetWorkspacePath(),
									}
									eventBridge.HandleEvent(ctx, &events.AgentEvent{
										Type:      events.LearningSkipped,
										Timestamp: time.Now(),
										Data:      learningSkippedEvent,
									})
									hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted learning_skipped event for step %d: %s (temp override: %s/%s)", stepIndex+1, stepTitle, hcpo.tempOverrideLLM.Provider, hcpo.tempOverrideLLM.ModelID))
								}
							}
						}

						break // Exit retry loop, will exit main loop at top
					} else {
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step %d loop condition not met yet (iteration %d/%d), continuing loop", stepIndex+1, loopIterationCount, step.MaxIterations))

						// Preserve validation response for next loop iteration (for fallback LLM detection)
						// If validation failed (success criteria not met) in this iteration, next iteration will use original LLM
						if isValidationFailure(validationResponse) {
							previousValidationResponse = validationResponse
							if hcpo.fallbackToOriginalLLMOnFailure {
								hcpo.GetLogger().Info(fmt.Sprintf("🔄 Loop iteration %d validation failed - next iteration will use original LLM (fallback enabled)", loopIterationCount))
							}

							// Request blocking human feedback after loop iteration validation failure (only in normal mode)
							// FAST MODE & SKIP HUMAN INPUT MODE: Skip human feedback and auto-continue with next iteration
							// SUB-AGENT: Never request human feedback (sub-agents run automatically)
							isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
							isSkipHumanInput := execCtx.SkipHumanInput
							var humanFeedback string

							if !isFastExecuteStep && !isSkipHumanInput && !isSubAgent {
								// Normal mode: Request human feedback for guidance on next loop iteration
								validationSummary := hcpo.formatValidationResponseForTemplate(validationResponse, fmt.Sprintf("Loop Iteration %d Validation Feedback", loopIterationCount))
								approved, feedback, err := hcpo.requestHumanFeedback(ctx, stepIndex+1, totalSteps, validationSummary)
								if err != nil {
									hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Human feedback request failed after loop iteration validation failure: %v, continuing with next iteration", err))
									// Continue with next iteration even if feedback request fails
									humanFeedback = ""
								} else if approved {
									// User approved - no specific feedback, continue with next iteration
									hcpo.GetLogger().Info(fmt.Sprintf("✅ User approved next loop iteration for step %d (iteration %d/%d) - no specific feedback provided", stepIndex+1, loopIterationCount, step.MaxIterations))
									humanFeedback = ""
								} else {
									// User provided feedback - store it for next loop iteration
									humanFeedback = feedback
									hcpo.GetLogger().Info(fmt.Sprintf("📝 Human feedback received for step %d loop iteration %d: %s", stepIndex+1, loopIterationCount, humanFeedback))
								}
							} else {
								if isSubAgent {
									hcpo.GetLogger().Info(fmt.Sprintf("🤖 Sub-agent: Skipping human feedback after loop iteration validation failure for step %d (sub-agents never request human feedback)", stepIndex+1))
								} else if isFastExecuteStep {
									hcpo.GetLogger().Info(fmt.Sprintf("⚡ Fast mode: Skipping human feedback after loop iteration validation failure for step %d (auto-continuing)", stepIndex+1))
								} else {
									hcpo.GetLogger().Info(fmt.Sprintf("⚡ Skip human input mode: Skipping human feedback after loop iteration validation failure for step %d (auto-continuing)", stepIndex+1))
								}
								humanFeedback = ""
							}

							// Store human feedback in template variables for next loop iteration
							if humanFeedback != "" {
								templateVars["HumanFeedback"] = humanFeedback
								hcpo.GetLogger().Info(fmt.Sprintf("📝 Added human feedback to template variables for step %d loop iteration %d", stepIndex+1, loopIterationCount))
							} else {
								// Clear any previous human feedback if none provided
								templateVars["HumanFeedback"] = ""
							}
						}

						// Check if learning should run after each loop iteration
						// Default to true for loop steps
						learningAfterLoopIteration := false
						if hasLoop(step) {
							// For loop steps, always default to true
							learningAfterLoopIteration = true
						} else if step.AgentConfigs != nil {
							// For non-loop steps, use the explicit value (defaults to false)
							learningAfterLoopIteration = step.AgentConfigs.LearningAfterLoopIteration
						}
						if learningAfterLoopIteration {
							// Run learning after this loop iteration
							isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
							// Check step-specific learning detail level
							isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning
							isLearningDetailLevelNone := false
							if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
								isLearningDetailLevelNone = true
							}
							isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
							// CODE EXECUTION MODE: Force learning enabled regardless of step config
							// Use step-level code execution mode (already computed above)
							if isCodeExecutionMode && isLearningDisabled {
								hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing learning for step %d loop iteration (overriding step config)", stepIndex+1))
								isLearningDisabled = false
							}
							// LOCK LEARNINGS: Check if learnings are locked (prevents learning agent from running but still uses existing learnings)
							// EXCEPTION: If learnings are locked but learnings don't exist, still run learning to create initial learnings
							isLearningsLocked := step.AgentConfigs != nil && step.AgentConfigs.LockLearnings != nil && *step.AgentConfigs.LockLearnings
							shouldSkipLearningDueToLock := false
							if isLearningsLocked {
								// Check if learnings folder exists and has content
								learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, step.ID, stepIndex, stepPath)
								if err != nil {
									// If we can't check, assume empty and run learning
									hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked but cannot check if learnings exist - will run learning to create initial learnings for step %d loop iteration", stepIndex+1))
									shouldSkipLearningDueToLock = false
								} else if learningsEmpty {
									// Learnings are locked but folder is empty - run learning to create initial learnings
									hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked but folder is empty - will run learning to create initial learnings for step %d loop iteration", stepIndex+1))
									shouldSkipLearningDueToLock = false
								} else {
									// Learnings are locked and learnings exist - skip learning
									shouldSkipLearningDueToLock = true
								}
							}
							// TEMP LLM OVERRIDE: Check if learning should be skipped based on which tempLLM was used (controlled by frontend flags)
							shouldSkipLearningDueToTempOverride := false
							if hcpo.executionOptions != nil && usedTempLLM != "" {
								if usedTempLLM == "tempLLM1" && hcpo.executionOptions.SkipLearningWhenTempLLM1 {
									shouldSkipLearningDueToTempOverride = true
									hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM1 was used and SkipLearningWhenTempLLM1 flag is enabled - will skip learning for step %d loop iteration", stepIndex+1))
								} else if usedTempLLM == "tempLLM2" && hcpo.executionOptions.SkipLearningWhenTempLLM2 {
									shouldSkipLearningDueToTempOverride = true
									hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM2 was used and SkipLearningWhenTempLLM2 flag is enabled - will skip learning for step %d loop iteration", stepIndex+1))
								}
							}

							if !isFastExecuteStep && !isLearningDisabled && !shouldSkipLearningDueToLock && !shouldSkipLearningDueToTempOverride {
								learningPathIdentifier := getLearningPathIdentifier(step.ID, stepPath)
								hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running learning analysis after loop iteration %d for %s", loopIterationCount, stepPath))
								// Run learning even though condition not met (for iteration analysis)
								err := hcpo.runSuccessLearningPhase(ctx, stepIndex, stepPath, learningPathIdentifier, totalSteps, &step, executionConversationHistory, validationResponse, isCodeExecutionMode)
								if err != nil {
									hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Learning phase failed after loop iteration %d for %s: %v", loopIterationCount, stepPath, err))
								} else {
									hcpo.GetLogger().Info(fmt.Sprintf("✅ Learning analysis completed after loop iteration %d for step %d", loopIterationCount, stepIndex+1))
								}
							}
						}

						// Capture execution result (final response) and validation outputs for next iteration
						previousIterationExecutionOutput = executionResult
						validationOutputParts := []string{}
						if validationResponse.Reasoning != "" {
							validationOutputParts = append(validationOutputParts, fmt.Sprintf("**Reasoning**: %s", validationResponse.Reasoning))
						}
						if validationResponse.LoopReasoning != "" {
							validationOutputParts = append(validationOutputParts, fmt.Sprintf("**Loop Reasoning**: %s", validationResponse.LoopReasoning))
						}
						if len(validationResponse.Feedback) > 0 {
							feedbackParts := []string{"**Feedback**: "}
							for _, fb := range validationResponse.Feedback {
								feedbackParts = append(feedbackParts, fmt.Sprintf("- [%s] %s: %s", fb.Severity, fb.Type, fb.Description))
							}
							validationOutputParts = append(validationOutputParts, strings.Join(feedbackParts, "\n"))
						}
						previousIterationValidationOutput = strings.Join(validationOutputParts, "\n\n")
						hcpo.GetLogger().Info(fmt.Sprintf("📝 Captured execution and validation outputs for iteration %d (will be included in next iteration)", loopIterationCount))
						break // Exit retry loop, continue main loop for next iteration
					}
				}

				// LEARNING PHASE: Runs for ALL agents regardless of validation status
				// Validation being disabled does NOT prevent learning from running
				// Learning will run if: not in fast mode, not disabled, not locked, and not skipped due to temp LLM override
				// FAST MODE & LEARNING DISABLED: Skip learning agents entirely
				isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
				// Check step-specific learning detail level
				isLearningDisabledStep := step.AgentConfigs != nil && step.AgentConfigs.DisableLearning != nil && *step.AgentConfigs.DisableLearning
				isLearningDetailLevelNone := false
				if step.AgentConfigs != nil && step.AgentConfigs.LearningDetailLevel == "none" {
					isLearningDetailLevelNone = true
				}
				isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone
				// CODE EXECUTION MODE: Force learning enabled regardless of step config
				// Use step-level code execution mode (already computed above)
				if isCodeExecutionMode && isLearningDisabled {
					hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled - forcing learning for step %d (overriding step config)", stepIndex+1))
					isLearningDisabled = false
				}
				// LOCK LEARNINGS: Check if learnings are locked (prevents learning agent from running but still uses existing learnings)
				// EXCEPTION: If learnings are locked but learnings don't exist, still run learning to create initial learnings
				isLearningsLocked := step.AgentConfigs != nil && step.AgentConfigs.LockLearnings != nil && *step.AgentConfigs.LockLearnings
				shouldSkipLearningDueToLock := false
				if isLearningsLocked {
					// Check if learnings folder exists and has content
					learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, step.ID, stepIndex, stepPath)
					if err != nil {
						// If we can't check, assume empty and run learning
						hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked but cannot check if learnings exist - will run learning to create initial learnings for step %d", stepIndex+1))
						shouldSkipLearningDueToLock = false
					} else if learningsEmpty {
						// Learnings are locked but folder is empty - run learning to create initial learnings
						hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked but folder is empty - will run learning to create initial learnings for step %d", stepIndex+1))
						shouldSkipLearningDueToLock = false
					} else {
						// Learnings are locked and learnings exist - skip learning
						shouldSkipLearningDueToLock = true
					}
				}
				// TEMP LLM OVERRIDE: Check if learning should be skipped based on which tempLLM was used (controlled by frontend flags)
				shouldSkipLearningDueToTempOverride := false
				if hcpo.executionOptions != nil && usedTempLLM != "" {
					if usedTempLLM == "tempLLM1" && hcpo.executionOptions.SkipLearningWhenTempLLM1 {
						shouldSkipLearningDueToTempOverride = true
						hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM1 was used and SkipLearningWhenTempLLM1 flag is enabled - will skip learning for step %d", stepIndex+1))
					} else if usedTempLLM == "tempLLM2" && hcpo.executionOptions.SkipLearningWhenTempLLM2 {
						shouldSkipLearningDueToTempOverride = true
						hcpo.GetLogger().Info(fmt.Sprintf("🔧 Temp LLM2 was used and SkipLearningWhenTempLLM2 flag is enabled - will skip learning for step %d", stepIndex+1))
					}
				}
				hcpo.GetLogger().Info(fmt.Sprintf("🔍 DEBUG: Step %d - fastExecuteMode=%v, fastExecuteEndStep=%d, isFastExecuteStep=%v, isLearningDisabled=%v (detailLevelNone=%v, stepDisabled=%v, codeExecutionMode=%v), isLearningsLocked=%v, shouldSkipLearningDueToLock=%v, usedTempLLM=%v, skipLearningWhenTempLLM1=%v, skipLearningWhenTempLLM2=%v, shouldSkipLearningDueToTempOverride=%v", stepIndex+1, execCtx.FastExecuteMode, execCtx.FastExecuteEndStep, isFastExecuteStep, isLearningDisabled, isLearningDetailLevelNone, isLearningDisabledStep, isCodeExecutionMode, isLearningsLocked, shouldSkipLearningDueToLock, usedTempLLM, hcpo.executionOptions != nil && hcpo.executionOptions.SkipLearningWhenTempLLM1, hcpo.executionOptions != nil && hcpo.executionOptions.SkipLearningWhenTempLLM2, shouldSkipLearningDueToTempOverride))
				if isFastExecuteStep || isLearningDisabled || shouldSkipLearningDueToLock || shouldSkipLearningDueToTempOverride {
					if isFastExecuteStep {
						hcpo.GetLogger().Info(fmt.Sprintf("⚡ Fast mode: Skipping learning agents for step %d", stepIndex+1))
					} else if isLearningDisabled {
						hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Learning disabled: Skipping learning agents for step %d", stepIndex+1))
					} else if shouldSkipLearningDueToLock {
						hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping learning agents for step %d (using existing learnings)", stepIndex+1))
					} else if shouldSkipLearningDueToTempOverride {
						hcpo.GetLogger().Info(fmt.Sprintf("🔧 %s was used and skip learning flag enabled: Skipping learning agents for step %d", usedTempLLM, stepIndex+1))
						// Emit learning skipped event
						eventBridge := hcpo.GetContextAwareBridge()
						if eventBridge != nil {
							stepTitle := step.Title
							if stepTitle == "" {
								stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
							}
							stepId := step.ID
							if stepId == "" {
								stepId = fmt.Sprintf("step-%d", stepIndex+1)
							}
							learningSkippedEvent := &events.LearningSkippedEvent{
								BaseEventData: events.BaseEventData{
									Timestamp: time.Now(),
									Component: "orchestrator",
								},
								StepID:          stepId,
								StepIndex:       stepIndex,
								StepTitle:       stepTitle,
								StepPath:        stepPath,
								IsBranchStep:    isBranchStep,
								Reason:          "temp_llm_override",
								TempLLMProvider: hcpo.tempOverrideLLM.Provider,
								TempLLMModel:    hcpo.tempOverrideLLM.ModelID,
								RunFolder:       hcpo.selectedRunFolder,
								WorkspacePath:   hcpo.GetWorkspacePath(),
							}
							eventBridge.HandleEvent(ctx, &events.AgentEvent{
								Type:      events.LearningSkipped,
								Timestamp: time.Now(),
								Data:      learningSkippedEvent,
							})
							hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted learning_skipped event for step %d: %s (temp override: %s/%s)", stepIndex+1, stepTitle, hcpo.tempOverrideLLM.Provider, hcpo.tempOverrideLLM.ModelID))
						}
					}
				} else {
					// Ensure validationResponse exists - if validation is disabled, assume success
					disableValidation := step.AgentConfigs != nil && step.AgentConfigs.DisableValidation != nil && *step.AgentConfigs.DisableValidation
					if validationResponse == nil && disableValidation {
						// Validation is disabled but response is nil - create success response for learning
						hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Validation disabled for step %d - creating success response for learning", stepIndex+1))
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: true,
							ExecutionStatus:      "COMPLETED",
							Reasoning:            "Validation disabled - step auto-approved for learning",
						}
					}

					// Run appropriate learning phase based on validation result
					// If validation is disabled, we assume IsSuccessCriteriaMet = true
					if validationResponse != nil && validationResponse.IsSuccessCriteriaMet {
						// Success Learning Agent - analyze what worked well and update plan.json
						learningPathIdentifier := getLearningPathIdentifier(step.ID, stepPath)
						hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running success learning analysis for %s", stepPath))
						err := hcpo.runSuccessLearningPhase(ctx, stepIndex, stepPath, learningPathIdentifier, totalSteps, &step, executionConversationHistory, validationResponse, isCodeExecutionMode)
						if err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Success learning phase failed for %s: %v", stepPath, err))
						} else {
							hcpo.GetLogger().Info(fmt.Sprintf("✅ Success learning analysis completed for %s", stepPath))
						}
					} else {
						// Failure Learning Agent - analyze what went wrong and provide refined task description
						// SKIP failure learning for loop steps - loop steps only run success learning when condition is met
						if hasLoop(step) {
							hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step %s is a loop step - skipping failure learning (loop steps only run success learning when condition is met)", stepPath))
						} else {
							learningPathIdentifier := getLearningPathIdentifier(step.ID, stepPath)
							hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running failure learning analysis for %s", stepPath))
							refinedTaskDescription, _, err := hcpo.runFailureLearningPhase(ctx, stepIndex, stepPath, learningPathIdentifier, totalSteps, &step, executionConversationHistory, validationResponse, isCodeExecutionMode)
							if err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failure learning phase failed for %s: %v", stepPath, err))
							} else {
								hcpo.GetLogger().Info(fmt.Sprintf("✅ Failure learning analysis completed for %s", stepPath))

								// Update step description for retry
								if refinedTaskDescription != "" {
									step.Description = refinedTaskDescription
									templateVars["StepDescription"] = refinedTaskDescription
									hcpo.GetLogger().Info(fmt.Sprintf("🔄 Updated step %d description with refined task for retry", stepIndex+1))
								}

								// Re-read learnings after failure learning updates them (if we're going to retry)
								// This ensures the next retry attempt uses the updated learnings from failure analysis
								if retryAttempt < maxRetryAttempts {
									hcpo.GetLogger().Info(fmt.Sprintf("📚 Re-reading learnings after failure learning update (for retry attempt %d)", retryAttempt+1))
									// Force re-read by temporarily disabling cache check for non-loop steps
									// For loop steps, respect the LearningAfterLoopIteration setting
									if !step.HasLoop {
										// For regular steps, always re-read after failure learning
										updatedLearningHistory, readErr := hcpo.readLearningHistory(
											ctx,
											stepIndex,
											step.ID,
											stepPath,
										)
										if readErr != nil {
											hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to re-read learnings after failure learning: %v - will use previous learnings", readErr))
										} else {
											formattedLearningHistory = updatedLearningHistory
											templateVars["LearningHistory"] = formattedLearningHistory // Update template vars for next retry
											hcpo.GetLogger().Info(fmt.Sprintf("✅ Re-read learnings after failure learning update (length: %d chars)", len(formattedLearningHistory)))
										}
									} else {
										// For loop steps, only re-read if LearningAfterLoopIteration is true
										// Default to true for loop steps
										learningAfterLoopIteration := step.HasLoop // Always true for loop steps
										if learningAfterLoopIteration {
											updatedLearningHistory, readErr := hcpo.readLearningHistory(
												ctx,
												stepIndex,
												step.ID,
												stepPath,
											)
											if readErr != nil {
												hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to re-read learnings after failure learning: %v - will use previous learnings", readErr))
											} else {
												formattedLearningHistory = updatedLearningHistory
												templateVars["LearningHistory"] = formattedLearningHistory // Update template vars for next retry
												hcpo.GetLogger().Info(fmt.Sprintf("✅ Re-read learnings after failure learning update (length: %d chars)", len(formattedLearningHistory)))
											}
										} else {
											hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping re-read for loop step (LearningAfterLoopIteration=false, will use cached learnings)"))
										}
									}
								}
							}
						}
					}
				}

				// Check if success criteria was met (only for non-loop steps or when loop handling is done)
				if !step.HasLoop {
					// Check IsSuccessCriteriaMet instead of just ExecutionStatus - PARTIAL/INCOMPLETE can also mean criteria not met
					if validationResponse != nil && validationResponse.IsSuccessCriteriaMet {
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d passed validation - success criteria met (Status: %s)", stepIndex+1, validationResponse.ExecutionStatus))

						// Track which tempLLM was used (for learning phase decision)
						// Determine based on retryAttempt and which tempLLMs exist
						hasTempLLM1 := hcpo.tempOverrideLLM != nil && hcpo.tempOverrideLLM.Provider != "" && hcpo.tempOverrideLLM.ModelID != ""
						hasTempLLM2 := hcpo.tempOverrideLLM2 != nil && hcpo.tempOverrideLLM2.Provider != "" && hcpo.tempOverrideLLM2.ModelID != ""
						if retryAttempt == 1 && hasTempLLM1 {
							usedTempLLM = "tempLLM1"
						} else if retryAttempt == 2 && hasTempLLM2 {
							usedTempLLM = "tempLLM2"
						} else {
							usedTempLLM = "" // Original LLM
						}

						// Clear human feedback since validation succeeded
						templateVars["HumanFeedback"] = ""
						break // Exit retry loop and continue to next step
					} else {
						statusStr := "unknown"
						if validationResponse != nil {
							statusStr = validationResponse.ExecutionStatus
						}
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step %d failed validation - success criteria not met (Status: %s, attempt %d/%d)", stepIndex+1, statusStr, retryAttempt, maxRetryAttempts))

						if retryAttempt >= maxRetryAttempts {
							hcpo.GetLogger().Error(fmt.Sprintf("❌ Step %d failed validation after %d attempts", stepIndex+1, maxRetryAttempts), nil)
							// Mark that validation failed after exhausting all retries
							validationFailedAfterMaxRetries = true
							break
						} else {
							// Preserve validation response for next retry attempt (for fallback LLM detection)
							// If fallback is enabled, the next retry will use original LLM instead of temp override
							previousValidationResponse = validationResponse

							// Request blocking human feedback after validation failure (only in normal mode)
							// FAST MODE & SKIP HUMAN INPUT MODE: Skip human feedback and auto-continue with retry
							// SUB-AGENT: Never request human feedback (sub-agents run automatically)
							isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
							isSkipHumanInput := execCtx.SkipHumanInput
							var humanFeedback string

							if !isFastExecuteStep && !isSkipHumanInput && !isSubAgent {
								// Normal mode: Request human feedback for guidance on retry
								validationSummary := hcpo.formatValidationResponseForTemplate(validationResponse, fmt.Sprintf("Validation Feedback (Retry Attempt %d)", retryAttempt+1))
								approved, feedback, err := hcpo.requestHumanFeedback(ctx, stepIndex+1, totalSteps, validationSummary)
								if err != nil {
									hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Human feedback request failed after validation failure: %v, continuing with retry", err))
									// Continue with retry even if feedback request fails
									humanFeedback = ""
								} else if approved {
									// User approved - no specific feedback, continue with retry
									hcpo.GetLogger().Info(fmt.Sprintf("✅ User approved retry for step %d (attempt %d/%d) - no specific feedback provided", stepIndex+1, retryAttempt+1, maxRetryAttempts))
									humanFeedback = ""
								} else {
									// User provided feedback - store it for next retry attempt
									humanFeedback = feedback
									hcpo.GetLogger().Info(fmt.Sprintf("📝 Human feedback received for step %d retry (attempt %d/%d): %s", stepIndex+1, retryAttempt+1, maxRetryAttempts, humanFeedback))
								}
							} else {
								if isSubAgent {
									hcpo.GetLogger().Info(fmt.Sprintf("🤖 Sub-agent: Skipping human feedback after validation failure for step %d (sub-agents never request human feedback)", stepIndex+1))
								} else if isFastExecuteStep {
									hcpo.GetLogger().Info(fmt.Sprintf("⚡ Fast mode: Skipping human feedback after validation failure for step %d (auto-retrying)", stepIndex+1))
								} else {
									hcpo.GetLogger().Info(fmt.Sprintf("⚡ Skip human input mode: Skipping human feedback after validation failure for step %d (auto-retrying)", stepIndex+1))
								}
								humanFeedback = ""
							}

							// Store human feedback in template variables for next retry attempt
							if humanFeedback != "" {
								templateVars["HumanFeedback"] = humanFeedback
								hcpo.GetLogger().Info(fmt.Sprintf("📝 Added human feedback to template variables for step %d retry (attempt %d/%d)", stepIndex+1, retryAttempt+1, maxRetryAttempts))
							} else {
								// Clear any previous human feedback if none provided
								templateVars["HumanFeedback"] = ""
							}

							// Build retry message with optional human guidance mention
							retryMessageSuffix := ""
							if humanFeedback != "" {
								retryMessageSuffix = " and human guidance"
							}
							if hcpo.fallbackToOriginalLLMOnFailure {
								hcpo.GetLogger().Info(fmt.Sprintf("🔄 Retrying step %d execution with validation feedback%s - next attempt will use original LLM (fallback enabled)", stepIndex+1, retryMessageSuffix))
							} else {
								hcpo.GetLogger().Info(fmt.Sprintf("🔄 Retrying step %d execution with validation feedback%s", stepIndex+1, retryMessageSuffix))
							}
							// Note: conversation history is preserved from previous attempts for context
						}
					}
				}
			} // End of retry loop

			// Exit immediately if validation failed after exhausting all retry attempts
			if validationFailedAfterMaxRetries && !step.HasLoop {
				hcpo.GetLogger().Error(fmt.Sprintf("🛑 Step %d failed validation after %d attempts - exiting workflow", stepIndex+1, maxRetryAttempts), nil)
				var validationDetails string
				if validationResponse != nil {
					validationDetails = fmt.Sprintf("Success Criteria Met: %v, Status: %s", validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
					if validationResponse.Reasoning != "" {
						validationDetails += fmt.Sprintf(", Reasoning: %s", validationResponse.Reasoning)
					}
				} else {
					validationDetails = "No validation response available"
				}
				err := fmt.Errorf(fmt.Sprintf("step %d failed validation after %d retry attempts. %s. Please review the execution results and update the plan if needed", stepIndex+1, maxRetryAttempts, validationDetails), nil)
				// Emit step_failed event using centralized method
				stepTitle := step.Title
				if stepTitle == "" {
					stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
				}
				stepId := step.ID
				if stepId == "" {
					stepId = fmt.Sprintf("step-%d", stepIndex+1)
				}
				hcpo.EmitStepFailedEvent(ctx, stepId, stepTitle, stepPath, err.Error(), stepIndex, isBranchStep)
				hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted step_failed event for step %d: %s (validation failed)", stepIndex+1, stepTitle))
				return executionResult, updatedContextFiles, err
			}

			// If in loop mode and condition not met, continue main loop
			if hasLoop(step) && !loopConditionMet {
				continue // Continue main loop for next iteration
			}

			// Exit main loop if not in loop mode or loop condition met
			if !step.HasLoop {
				// Non-loop step: execute once and exit
				break // Exit main execution loop
			}
			if loopConditionMet {
				// Loop step with condition met: exit loop
				break // Exit main execution loop
			}
			// Loop step with condition not met: continue to next iteration
		} // End of main execution loop

		// BLOCKING HUMAN FEEDBACK - Ask user if they want to continue to next step
		// If user provides feedback (doesn't approve), stop workflow and ask user to manually update plan
		// FAST MODE: Skip human feedback and auto-approve
		// SKIP HUMAN INPUT MODE: Skip human feedback but keep learning enabled
		// DECISION INNER STEP: Skip human feedback on success (decision step will handle routing)
		// SUB-AGENT: Never request human feedback (sub-agents run automatically)
		// NORMAL MODE & LOOP MODE: Always request human feedback before moving to next step
		isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
		isSkipHumanInput := execCtx.SkipHumanInput
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 DEBUG: Step %d human feedback check - execCtx: fastExecuteMode=%v, fastExecuteEndStep=%d, stepIndex=%d, isFastExecuteStep=%v, skipHumanInput=%v, isDecisionInnerStep=%v, isSubAgent=%v", stepIndex+1, execCtx.FastExecuteMode, execCtx.FastExecuteEndStep, stepIndex, isFastExecuteStep, isSkipHumanInput, isDecisionInnerStep, isSubAgent))

		var approved bool
		var feedback string

		// For sub-agents, never request human feedback (they run automatically as part of orchestration)
		if isSubAgent {
			hcpo.GetLogger().Info(fmt.Sprintf("🤖 Sub-agent %d - auto-approving without human feedback (sub-agents never request human feedback)", stepIndex+1))
			approved = true
			feedback = "" // No feedback for sub-agents
		} else if isDecisionInnerStep && validationResponse != nil && !isValidationFailure(validationResponse) {
			// For decision inner steps that succeeded, skip human feedback (decision step will handle routing)
			// Still allow human feedback if validation failed (handled in retry loop above)
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Decision inner step %d succeeded - auto-approving without human feedback (decision step will handle routing)", stepIndex+1))
			approved = true
			feedback = "" // No feedback for decision inner steps
		} else if isFastExecuteStep || isSkipHumanInput {
			if isFastExecuteStep {
				hcpo.GetLogger().Info(fmt.Sprintf("⚡ Fast mode: Auto-approving step %d without human feedback (stepIndex=%d <= fastExecuteEndStep=%d)", stepIndex+1, stepIndex, execCtx.FastExecuteEndStep))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("⚡ Skip human input mode: Auto-approving step %d without human feedback (learning will still run)", stepIndex+1))
			}
			approved = true
			feedback = "" // No feedback in fast mode or skip human input mode
		} else {
			// Normal mode and loop mode: Request human feedback
			var validationSummary string
			if validationResponse != nil {
				validationSummary = fmt.Sprintf("Step %d validation completed. Success Criteria Met: %v, Status: %s", stepIndex+1, validationResponse.IsSuccessCriteriaMet, validationResponse.ExecutionStatus)
			} else {
				validationSummary = fmt.Sprintf("Step %d execution failed - no validation response available", stepIndex+1)
			}
			var err error
			approved, feedback, err = hcpo.requestHumanFeedback(ctx, stepIndex+1, totalSteps, validationSummary)
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Human feedback request failed: %w", err))
				// Default to continue if feedback fails
				approved = true
			}
		}

		// Store human feedback for future steps (even if approved, user might have provided guidance)
		if feedback != "" {
			feedbackEntry := fmt.Sprintf("Step %d/%d Feedback: %s", stepIndex+1, totalSteps, feedback)
			// Note: humanFeedbackHistory is not available in this function scope, so we skip storing it
			// It will be handled by the caller if needed
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Human feedback received for step %d: %s", stepIndex+1, feedbackEntry))
		}

		if approved {
			// User approved - mark step as completed and exit outer loop
			// Only update progress if this is not a branch step
			if !isBranchStep {
				hcpo.addCompletedStepIndex(progress, stepIndex)
				// Always save progress after marking a step as completed (both fast and normal mode)
				if err := hcpo.saveStepProgress(ctx, progress); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save step progress: %w", err))
				} else {
					modeStr := "fast mode"
					if !isFastExecuteStep {
						modeStr = "normal mode"
					}
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d/%d marked as completed and saved (%s) - Total completed: %d/%d", stepIndex+1, totalSteps, modeStr, len(progress.CompletedStepIndices), progress.TotalSteps))
				}

				// Emit step token usage summary
				stepTitle := step.Title
				if stepTitle == "" {
					stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
				}
				hcpo.EmitStepTokenUsage(ctx, "execution", stepIndex, stepTitle, false) // Don't clear - keep for potential future queries
				// Note: Token usage is now persisted in real-time on each token_usage event, not just at step completion
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Branch step %d completed (not updating main progress)", stepIndex+1))
			}
			stepCompleted = true
		} else {
			// User provided feedback (didn't approve) - stop workflow and ask user to manually update plan
			hcpo.GetLogger().Info(fmt.Sprintf("🛑 User provided feedback - stopping workflow. Feedback: %s", feedback))
			planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
			return executionResult, updatedContextFiles, fmt.Errorf(fmt.Sprintf("workflow stopped: user feedback received. please manually update the plan at %s with the following feedback, then restart the workflow: %s", planPath, feedback), nil)
		}
	} // End of outer loop for step execution

	// Append step's context output to context files if it exists
	if step.ContextOutput != "" {
		updatedContextFiles = append(updatedContextFiles, step.ContextOutput)
		hcpo.GetLogger().Info(fmt.Sprintf("📝 Added step context output to context files: %s", step.ContextOutput))
	}

	// Emit step_finished event
	// Note: Conditional steps emit their own step_finished event in executeConditionalStep after branch execution completes
	hcpo.emitStepFinishedEvent(ctx, step, stepIndex, stepPath, isBranchStep)

	return executionResult, updatedContextFiles, nil
}

// ============================================================================
// STEP TYPE DETECTION HELPERS (for TodoStep)
// ============================================================================
// These helper functions provide a cleaner way to detect step types from TodoStep
// boolean flags, making the execution routing logic more maintainable and
// preparing for future migration to type-safe step types.

// isConditionalStep returns true if the step is a conditional step (has conditional branches)
func isConditionalStep(step TodoStep) bool {
	return step.HasCondition
}

// isDecisionStep returns true if the step is a decision step (executes inner step and routes based on evaluation)
func isDecisionStep(step TodoStep) bool {
	return step.HasDecisionStep
}

// isOrchestrationStep returns true if the step is an orchestration step (orchestrator with multiple sub-agents)
func isOrchestrationStep(step TodoStep) bool {
	return step.HasOrchestrationStep
}

// isRegularStep returns true if the step is a regular step (not conditional, decision, or orchestration)
func isRegularStep(step TodoStep) bool {
	return !step.HasCondition && !step.HasDecisionStep && !step.HasOrchestrationStep
}

// hasLoop returns true if the step has loop mode enabled
func hasLoop(step TodoStep) bool {
	return step.HasLoop
}

// runExecutionPhase executes the plan steps one by one
func (hcpo *HumanControlledTodoPlannerOrchestrator) runExecutionPhase(
	ctx context.Context,
	breakdownSteps []TodoStep,
	iteration int,
	progress *StepProgress,
	startFromStep int,
	execCtx *ExecutionContext,
) error {
	hcpo.GetLogger().Info(fmt.Sprintf("🔄 Starting step-by-step execution of %d steps (starting from step %d)", len(breakdownSteps), startFromStep+1))

	// Learning detail level is now configured per-step via AgentConfigs
	// Each step can specify its own learning detail level, defaults to "exact" if not set
	hcpo.GetLogger().Info(fmt.Sprintf("📝 Using per-step learning detail level configuration"))

	// Run folder should already be resolved early (after plan approval)
	if hcpo.selectedRunFolder == "" {
		return fmt.Errorf(fmt.Sprintf("run folder not resolved - this should have been set after plan approval"), nil)
	}
	hcpo.GetLogger().Info(fmt.Sprintf("📁 Using resolved run folder: %s", hcpo.selectedRunFolder))

	// Track execution results in memory (instead of reading from files)
	// This allows conditional steps to use execution results directly
	previousExecutionResults := make([]string, 0)

	// Track decision context for steps routed from decision steps
	// Key: target step index (0-based), Value: decision context
	decisionContextMap := make(map[int]*DecisionContext)

	// Execute each step one by one
	// Use traditional for loop to allow jumping to different steps
	for i := 0; i < len(breakdownSteps); i++ {
		step := breakdownSteps[i]
		// Check for context cancellation before each step
		select {
		case <-ctx.Done():
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Workflow execution canceled before step %d/%d: %s", i+1, len(breakdownSteps), step.Title))
			return fmt.Errorf(fmt.Sprintf("workflow execution canceled: %w", ctx.Err()), nil)
		default:
		}

		// Reset fast execute mode if we've passed the fast execute range
		// This ensures normal execution (with learning and human feedback) for steps after fastExecuteEndStep
		// Note: execCtx is immutable, so we update the controller state for future steps
		if execCtx.FastExecuteMode && i > execCtx.FastExecuteEndStep {
			hcpo.GetLogger().Info(fmt.Sprintf("🔄 Fast execute mode completed (steps 0-%d), resetting to normal execution mode for step %d+", execCtx.FastExecuteEndStep, i+1))
			// Update execCtx for remaining steps (create new context with fast mode disabled)
			execCtx = &ExecutionContext{
				SkipHumanInput:     execCtx.SkipHumanInput,
				FastExecuteMode:    false,
				FastExecuteEndStep: -1,
				RunSingleStepOnly:  execCtx.RunSingleStepOnly,
				SingleStepTarget:   execCtx.SingleStepTarget,
			}
			hcpo.SetFastExecuteMode(false, -1)
			// Ensure progress is saved when transitioning from fast to normal mode
			// This catches any steps that were completed in fast mode but not yet saved
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress during fast→normal transition: %w", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved progress during fast→normal mode transition: %d/%d steps completed", len(progress.CompletedStepIndices), progress.TotalSteps))
			}
		}

		// Skip if step is already completed
		if i < startFromStep {
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping step %d/%d (already completed): %s", i+1, len(breakdownSteps), step.Title))
			continue
		}

		// Check if step is in completed list
		// BUT: Force execution if:
		//  1. Single-step mode and this is the target step, OR
		//  2. This is the explicit resume step (startFromStep) - user wants to re-run it
		isCompleted := false
		forceExecution := false
		if execCtx.RunSingleStepOnly && i == execCtx.SingleStepTarget {
			// Force execution of target step even if completed
			forceExecution = true
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Single-step mode: forcing execution of target step %d even if previously completed", i+1))
		} else if i == startFromStep {
			// This is the explicit resume step - user wants to re-run it even if marked as completed
			// (Cleanup should have removed it, but force execution as safety net)
			for _, completedIdx := range progress.CompletedStepIndices {
				if completedIdx == i {
					isCompleted = true
					break
				}
			}
			if isCompleted {
				forceExecution = true
				hcpo.GetLogger().Info(fmt.Sprintf("🎯 Explicit resume step %d: forcing execution even though marked as completed (cleanup may have failed)", i+1))
			}
		} else {
			for _, completedIdx := range progress.CompletedStepIndices {
				if completedIdx == i {
					isCompleted = true
					break
				}
			}
		}
		if isCompleted && !forceExecution {
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping step %d/%d (marked as completed): %s", i+1, len(breakdownSteps), step.Title))
			continue
		}

		hcpo.GetLogger().Info(fmt.Sprintf("📋 Executing step %d/%d: %s", i+1, len(breakdownSteps), step.Title))

		// Build context files from previous steps
		previousContextFiles := make([]string, 0)
		for prevIdx := 0; prevIdx < i; prevIdx++ {
			if prevIdx < len(breakdownSteps) && breakdownSteps[prevIdx].ContextOutput != "" {
				// Resolve variables in context output (consistent with conditional steps)
				resolvedOutput := ResolveVariables(breakdownSteps[prevIdx].ContextOutput, hcpo.variableValues)
				previousContextFiles = append(previousContextFiles, resolvedOutput)
			}
		}

		// Route execution based on step type using helper functions
		// Check if this is a conditional step
		if isConditionalStep(step) {
			// Execute conditional step - pass execution results directly (not file paths)
			hcpo.GetLogger().Info(fmt.Sprintf("🔀 Starting conditional step execution: %s", step.Title))
			if err := hcpo.executeConditionalStep(ctx, step, i, 0, progress, previousExecutionResults, iteration, execCtx, breakdownSteps); err != nil {
				// Check if this is a workflow termination signal
				if strings.Contains(err.Error(), "WORKFLOW_END") {
					hcpo.GetLogger().Info(fmt.Sprintf("🏁 Conditional step %d signaled workflow termination - ending workflow", i+1))
					// Mark step as completed and break to end workflow
					hcpo.addCompletedStepIndex(progress, i)
					if err := hcpo.saveStepProgress(ctx, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after conditional step termination: %w", err))
					}
					break // Break out of the execution loop to end workflow
				}
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Conditional step %d execution failed: %v", i+1, err), nil)
				// Emit error event using centralized method
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "conditional-step-execution", fmt.Sprintf("Execute conditional step: %s", step.Title), err.Error(), i, iteration)
				return fmt.Errorf(fmt.Sprintf("conditional step %d execution failed: %w", i+1, err), nil)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Conditional step %d completed successfully: %s", i+1, step.Title))

			// Mark conditional step as completed (executeConditionalStep handles progress internally)
			hcpo.addCompletedStepIndex(progress, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after conditional step: %w", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved progress: conditional step %d marked as completed", i+1))
			}

			// Check if we're in single step mode and should stop
			if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
				hcpo.GetLogger().Info(fmt.Sprintf("🎯 Single step mode: completed target step %d, stopping execution", i+1))
				hcpo.SetRunSingleStepMode(false, -1) // Reset mode
				break
			}

			// Determine next step based on branch execution and next_step_id
			// Get which branch was executed from branch progress
			var nextStepID string
			if branchProgress, exists := progress.BranchSteps[i]; exists {
				if branchProgress.BranchExecuted == "if_true" {
					// True branch was executed
					// Check if next_step_id is provided (optional when branch has steps, required when empty)
					if step.IfTrueNextStepID != "" {
						nextStepID = step.IfTrueNextStepID
						hcpo.GetLogger().Info(fmt.Sprintf("🔗 True branch completed - using if_true_next_step_id: %s", nextStepID))
					} else if len(step.IfTrueSteps) > 0 {
						// Branch has steps but no explicit next_step_id - default to next sequential step
						nextStepID = "" // Will default to next step in loop
						hcpo.GetLogger().Info(fmt.Sprintf("🔗 True branch completed - no explicit next_step_id, defaulting to next sequential step"))
					} else {
						// Empty branch - next_step_id should have been required, but handle gracefully
						nextStepID = "" // Will default to next step in loop
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ True branch is empty but no if_true_next_step_id provided - defaulting to next sequential step"))
					}
				} else {
					// False branch was executed
					// Check if next_step_id is provided (optional when branch has steps, required when empty)
					if step.IfFalseNextStepID != "" {
						nextStepID = step.IfFalseNextStepID
						hcpo.GetLogger().Info(fmt.Sprintf("🔗 False branch completed - using if_false_next_step_id: %s", nextStepID))
					} else if len(step.IfFalseSteps) > 0 {
						// Branch has steps but no explicit next_step_id - default to next sequential step
						nextStepID = "" // Will default to next step in loop
						hcpo.GetLogger().Info(fmt.Sprintf("🔗 False branch completed - no explicit next_step_id, defaulting to next sequential step"))
					} else {
						// Empty branch - next_step_id should have been required, but handle gracefully
						nextStepID = "" // Will default to next step in loop
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ False branch is empty but no if_false_next_step_id provided - defaulting to next sequential step"))
					}
				}
			} else {
				// No branch progress found (shouldn't happen, but handle gracefully)
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ No branch progress found for conditional step %d - defaulting to next sequential step", i+1))
				nextStepID = "" // Will default to next step in loop
			}

			// Handle next step navigation
			if nextStepID == "end" {
				// End workflow
				hcpo.GetLogger().Info(fmt.Sprintf("🏁 Conditional step %d specified 'end' - terminating workflow", i+1))
				break
			} else if nextStepID != "" {
				// Find target step by ID and jump to it
				targetStepIndex := -1
				for idx, s := range breakdownSteps {
					if s.ID == nextStepID {
						targetStepIndex = idx
						break
					}
				}
				if targetStepIndex >= 0 {
					hcpo.GetLogger().Info(fmt.Sprintf("🔗 Jumping to step %d (ID: %s) as specified by next_step_id", targetStepIndex+1, nextStepID))

					// Update startFromStep to allow execution from target step
					// This prevents the skip check (i < startFromStep) from blocking execution
					if targetStepIndex < startFromStep {
						startFromStep = targetStepIndex
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 Updated startFromStep to %d to allow execution from routed step", startFromStep+1))
					}

					// Set loop index to jump to target step (subtract 1 because loop will increment)
					i = targetStepIndex - 1
					continue
				} else {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Target step ID '%s' not found in plan - defaulting to next sequential step", nextStepID))
					// Fall through to default behavior (continue to next step)
				}
			}

			// Default: continue to next sequential step
			continue
		}

		// Check if this is a decision step
		if isDecisionStep(step) {
			// Execute decision step - executes inner step, evaluates output, returns result for routing
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Starting decision step execution: %s", step.Title))
			decisionResult, executionResult, err := hcpo.executeDecisionStep(ctx, &step, i, progress, previousContextFiles, iteration, execCtx, breakdownSteps)
			if err != nil {
				// Check if this is a workflow termination signal
				if strings.Contains(err.Error(), "WORKFLOW_END") {
					hcpo.GetLogger().Info(fmt.Sprintf("🏁 Decision step %d signaled workflow termination - ending workflow", i+1))
					// Mark step as completed and break to end workflow
					hcpo.addCompletedStepIndex(progress, i)
					if err := hcpo.saveStepProgress(ctx, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after decision step termination: %w", err))
					}
					break // Break out of the execution loop to end workflow
				}
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Decision step %d execution failed: %v", i+1, err), nil)
				// Emit error event using centralized method
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "decision-step-execution", fmt.Sprintf("Execute decision step: %s", step.Title), err.Error(), i, iteration)
				return fmt.Errorf("decision step %d execution failed: %w", i+1, err)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Decision step %d completed successfully: %s", i+1, step.Title))

			// Mark decision step as completed
			hcpo.addCompletedStepIndex(progress, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after decision step: %w", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved progress: decision step %d marked as completed", i+1))
			}

			// Check if we're in single step mode and should stop
			if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
				hcpo.GetLogger().Info(fmt.Sprintf("🎯 Single step mode: completed target step %d, stopping execution", i+1))
				hcpo.SetRunSingleStepMode(false, -1) // Reset mode
				break
			}

			// Determine next step based on decision result (using returned value instead of state variable)
			var nextStepID string
			var resultStr string
			if decisionResult {
				nextStepID = step.IfTrueNextStepID
				resultStr = "true"
				hcpo.GetLogger().Info(fmt.Sprintf("🔗 Decision step evaluated to TRUE - using if_true_next_step_id: %s", nextStepID))
			} else {
				nextStepID = step.IfFalseNextStepID
				resultStr = "false"
				hcpo.GetLogger().Info(fmt.Sprintf("🔗 Decision step evaluated to FALSE - using if_false_next_step_id: %s", nextStepID))
			}

			// Track decision evaluations to prevent infinite loops
			// Initialize DecisionEvaluationCounts if nil
			if progress.DecisionEvaluationCounts == nil {
				progress.DecisionEvaluationCounts = make(DecisionEvaluationCount)
			}

			// Create key: stepID:result (e.g., "verify-minute-file:false")
			decisionKey := fmt.Sprintf("%s:%s", step.ID, resultStr)
			currentCount := progress.DecisionEvaluationCounts[decisionKey]
			newCount := currentCount + 1
			progress.DecisionEvaluationCounts[decisionKey] = newCount

			hcpo.GetLogger().Info(fmt.Sprintf("📊 Decision evaluation count for %s: %d", decisionKey, newCount))

			// Check if we've made this same decision more than 2 times (3rd time = error)
			if newCount > 2 {
				errorMsg := fmt.Sprintf("infinite loop detected: decision step '%s' (ID: %s) has evaluated to %s %d times. This indicates a workflow logic error that would cause an infinite loop. Please review the decision step configuration and routing logic.", step.Title, step.ID, resultStr, newCount)
				hcpo.GetLogger().Error(errorMsg, nil)
				// Emit error event
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "decision-step-loop-detection", fmt.Sprintf("Decision step: %s", step.Title), errorMsg, i, iteration)
				return fmt.Errorf("workflow error: %s", errorMsg)
			}

			// Save progress with updated decision count
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after decision evaluation: %w", err))
			}

			// Handle next step navigation
			if nextStepID == "end" {
				// End workflow
				hcpo.GetLogger().Info(fmt.Sprintf("🏁 Decision step %d specified 'end' - terminating workflow", i+1))
				break
			} else if nextStepID != "" {
				// Find target step by ID and jump to it
				targetStepIndex := -1
				for idx, s := range breakdownSteps {
					if s.ID == nextStepID {
						targetStepIndex = idx
						break
					}
				}
				if targetStepIndex >= 0 {
					hcpo.GetLogger().Info(fmt.Sprintf("🔗 Jumping to step %d (ID: %s) as specified by next_step_id", targetStepIndex+1, nextStepID))

					// Store decision context for the target step ONLY when decision result is false
					// When decision is true, the step executes normally without decision context
					// When decision is false, we pass context to help understand why this step is being executed
					if !decisionResult {
						decisionContextMap[targetStepIndex] = &DecisionContext{
							DecisionStepIndex:       i,
							DecisionStepTitle:       step.Title,
							DecisionResult:          decisionResult,
							DecisionReasoning:       step.DecisionResponse.Reasoning,
							DecisionExecutionResult: executionResult,
						}
						hcpo.GetLogger().Info(fmt.Sprintf("💾 Stored decision context for step %d (from decision step %d: %s) - decision was FALSE", targetStepIndex+1, i+1, step.Title))
					} else {
						hcpo.GetLogger().Info(fmt.Sprintf("ℹ️ Skipping decision context for step %d - decision was TRUE (normal execution path)", targetStepIndex+1))
					}

					// When decision step routes back to a previous step, we need to:
					// 1. Remove target step AND all subsequent steps from completed list (they all depend on target step's output)
					// 2. Delete execution folders for target step AND all subsequent steps
					// This ensures a clean state for re-execution

					// Use cleanupProgressFromStep to remove all steps from targetStepIndex onward from progress
					// This also handles branch step cleanup and saves progress to steps_done.json
					if err := hcpo.cleanupProgressFromStep(ctx, targetStepIndex, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup progress from step %d: %v (continuing anyway)", targetStepIndex+1, err))
					} else {
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 Cleaned up progress: removed step %d and all subsequent steps from completed list", targetStepIndex+1))
					}

					// Delete execution folders for target step and all subsequent steps
					// This ensures old execution artifacts don't interfere with re-execution
					cleanedCount := 0
					for stepNum := targetStepIndex + 1; stepNum <= len(breakdownSteps); stepNum++ {
						if err := hcpo.deleteStepExecutionFolder(ctx, stepNum); err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete execution folder for step %d: %v (continuing)", stepNum, err))
						} else {
							cleanedCount++
							hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaned up execution folder for step %d", stepNum))
						}
					}
					if cleanedCount > 0 {
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Cleaned up execution folders for %d steps (step-%d to step-%d)", cleanedCount, targetStepIndex+1, len(breakdownSteps)))
					}

					// Update startFromStep to allow execution from target step
					// This prevents the skip check (i < startFromStep) from blocking execution
					if targetStepIndex < startFromStep {
						startFromStep = targetStepIndex
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 Updated startFromStep to %d to allow execution from routed step", startFromStep+1))
					}

					// Set loop index to jump to target step (subtract 1 because loop will increment)
					i = targetStepIndex - 1
					continue
				} else {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Target step ID '%s' not found in plan - defaulting to next sequential step", nextStepID))
					// Fall through to default behavior (continue to next step)
				}
			}

			// Default: continue to next sequential step
			continue
		}

		// Check if this is an orchestration step
		if isOrchestrationStep(step) {
			// Execute orchestration step - executes main step, evaluates output, routes to sub-agents, loops until success
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Starting orchestration step execution: %s", step.Title))
			successCriteriaMet, nextStepID, err := hcpo.executeOrchestrationStep(ctx, &step, i, progress, previousContextFiles, iteration, execCtx, breakdownSteps)
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Orchestration step %d execution failed: %v", i+1, err), nil)
				// Emit error event using centralized method
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "orchestration-step-execution", fmt.Sprintf("Execute orchestration step: %s", step.Title), err.Error(), i, iteration)
				return fmt.Errorf("orchestration step %d execution failed: %w", i+1, err)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Orchestration step %d completed successfully: %s (SuccessCriteriaMet: %t)", i+1, step.Title, successCriteriaMet))

			// Mark orchestration step as completed
			hcpo.addCompletedStepIndex(progress, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after orchestration step: %w", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved progress: orchestration step %d marked as completed", i+1))
			}

			// Check if we're in single step mode and should stop
			if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
				hcpo.GetLogger().Info(fmt.Sprintf("🎯 Single step mode: completed target step %d, stopping execution", i+1))
				hcpo.SetRunSingleStepMode(false, -1) // Reset mode
				break
			}

			// Handle next step navigation
			if nextStepID == "end" {
				// End workflow
				hcpo.GetLogger().Info(fmt.Sprintf("🏁 Orchestration step %d specified 'end' - terminating workflow", i+1))
				break
			} else if nextStepID != "" {
				// Find target step by ID and jump to it
				targetStepIndex := -1
				for idx, s := range breakdownSteps {
					if s.ID == nextStepID {
						targetStepIndex = idx
						break
					}
				}
				if targetStepIndex >= 0 {
					hcpo.GetLogger().Info(fmt.Sprintf("🔗 Jumping to step %d (ID: %s) as specified by next_step_id", targetStepIndex+1, nextStepID))

					// When orchestration step routes to a step, we need to:
					// 1. Remove target step AND all subsequent steps from completed list (they all depend on target step's output)
					// 2. Delete execution folders for target step AND all subsequent steps
					// This ensures a clean state for re-execution

					// Use cleanupProgressFromStep to remove all steps from targetStepIndex onward from progress
					// This also handles branch step cleanup and saves progress to steps_done.json
					if err := hcpo.cleanupProgressFromStep(ctx, targetStepIndex, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup progress from step %d: %v (continuing anyway)", targetStepIndex+1, err))
					} else {
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 Cleaned up progress: removed step %d and all subsequent steps from completed list", targetStepIndex+1))
					}

					// Delete execution folders for target step and all subsequent steps
					// This ensures old execution artifacts don't interfere with re-execution
					cleanedCount := 0
					for stepNum := targetStepIndex + 1; stepNum <= len(breakdownSteps); stepNum++ {
						if err := hcpo.deleteStepExecutionFolder(ctx, stepNum); err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to delete execution folder for step %d: %v (continuing)", stepNum, err))
						} else {
							cleanedCount++
							hcpo.GetLogger().Info(fmt.Sprintf("🗑️ Cleaned up execution folder for step %d", stepNum))
						}
					}
					if cleanedCount > 0 {
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Cleaned up execution folders for %d steps (step-%d to step-%d)", cleanedCount, targetStepIndex+1, len(breakdownSteps)))
					}

					// Update startFromStep to allow execution from target step
					// This prevents the skip check (i < startFromStep) from blocking execution
					if targetStepIndex < startFromStep {
						startFromStep = targetStepIndex
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 Updated startFromStep to %d to allow execution from routed step", startFromStep+1))
					}

					// Set loop index to jump to target step (subtract 1 because loop will increment)
					i = targetStepIndex - 1
					continue
				} else {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Target step ID '%s' not found in plan - defaulting to next sequential step", nextStepID))
					// Fall through to default behavior (continue to next step)
				}
			}

			// Default: continue to next sequential step
			continue
		}

		// Execute regular step using executeSingleStep
		// Note: previousContextFiles is still needed for executeSingleStep (for context dependencies)
		// But for conditional steps, we use previousExecutionResults instead
		previousContextFiles = make([]string, 0)
		for prevIdx := 0; prevIdx < i; prevIdx++ {
			if prevIdx < len(breakdownSteps) && breakdownSteps[prevIdx].ContextOutput != "" {
				// Resolve variables in context output (consistent with conditional steps)
				resolvedOutput := ResolveVariables(breakdownSteps[prevIdx].ContextOutput, hcpo.variableValues)
				previousContextFiles = append(previousContextFiles, resolvedOutput)
			}
		}

		// Check if this step has decision context (routed from a decision step)
		var decisionCtx *DecisionContext
		if dc, exists := decisionContextMap[i]; exists {
			decisionCtx = dc
			// Clean up after use (optional, but good practice)
			delete(decisionContextMap, i)
			hcpo.GetLogger().Info(fmt.Sprintf("📝 Using decision context for step %d (routed from decision step %d)", i+1, dc.DecisionStepIndex+1))
		}

		stepPath := fmt.Sprintf("step-%d", i+1)
		executionResult, _, err := hcpo.executeSingleStep(
			ctx,
			step,
			i,
			stepPath,
			len(breakdownSteps),
			iteration,
			previousContextFiles,
			progress,
			false, // isBranchStep = false
			execCtx,
			breakdownSteps, // allSteps - pass all steps for prerequisite detection
			false,          // isDecisionInnerStep = false (regular step)
			decisionCtx,    // decisionContext - nil if not routed from decision step
			"",             // decisionEvaluationQuestion - empty for regular steps
			false,          // isSubAgent = false (regular step)
		)
		if err != nil {
			// Check if this is a prerequisite navigation error
			errStr := err.Error()
			if strings.Contains(errStr, "prerequisite failure detected") && strings.Contains(errStr, "Navigating back to step") {
				// Parse target step index from error message: "prerequisite failure detected: {reason}. Navigating back to step {stepNumber}"
				// Extract the step number (1-based) from the error message
				var targetStepNumber int = -1
				// Try to find "Navigating back to step {number}" pattern
				parts := strings.Split(errStr, "Navigating back to step ")
				if len(parts) == 2 {
					if _, parseErr := fmt.Sscanf(parts[1], "%d", &targetStepNumber); parseErr == nil && targetStepNumber > 0 {
						targetStepIndex := targetStepNumber - 1 // Convert to 0-based
						if targetStepIndex >= 0 && targetStepIndex < len(breakdownSteps) {
							hcpo.GetLogger().Info(fmt.Sprintf("🔄 Prerequisite navigation: restarting execution from step %d", targetStepNumber))
							// Update loop index to restart from target step (subtract 1 because loop will increment)
							i = targetStepIndex - 1
							// Continue to restart the loop from target step
							continue
						}
					}
				}
			}

			hcpo.GetLogger().Error(fmt.Sprintf("❌ Step %d execution failed: %v", i+1, err), nil)
			// Emit step_failed event using centralized method
			stepTitle := step.Title
			if stepTitle == "" {
				stepTitle = fmt.Sprintf("Step %d", i+1)
			}
			stepId := step.ID
			if stepId == "" {
				stepId = fmt.Sprintf("step-%d", i+1)
			}
			hcpo.EmitStepFailedEvent(ctx, stepId, stepTitle, stepPath, err.Error(), i, false)
			hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted step_failed event for step %d: %s", i+1, stepTitle))
			return fmt.Errorf(fmt.Sprintf("step %d execution failed: %w", i+1, err), nil)
		}

		// Log execution result (for debugging)
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d execution completed (result length: %d chars)", i+1, len(executionResult)))

		// Track execution result in memory for use by subsequent conditional steps
		// Ensure slice is large enough (pad with empty strings if needed)
		for len(previousExecutionResults) <= i {
			previousExecutionResults = append(previousExecutionResults, "")
		}
		previousExecutionResults[i] = executionResult
		hcpo.GetLogger().Info(fmt.Sprintf("💾 Stored execution result for step %d (will be used by subsequent conditional steps)", i+1))

		// Check if we're in single step mode and should stop
		if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Single step mode: completed target step %d, stopping execution", i+1))
			hcpo.SetRunSingleStepMode(false, -1) // Reset mode
			break
		}

		// Note: Progress tracking is handled inside executeSingleStep
		// Continue to next step
		continue
	}

	// Final save to ensure all completed steps are persisted
	// This is a safety measure to catch any steps that might have been missed
	if err := hcpo.saveStepProgress(ctx, progress); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save final step progress: %w", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("💾 Final progress save completed: %d/%d steps completed", len(progress.CompletedStepIndices), progress.TotalSteps))
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ All steps execution completed"))
	return nil
}

// sanitizeTitleForAgentName sanitizes a step title for use in agent names
// - Removes step number prefixes (e.g., "Step 4:", "Step 5 -", "Step 3.")
// - Removes/replaces special characters (colons, slashes, etc.)
// - Normalizes whitespace and converts to lowercase
// - Removes multiple consecutive dashes
func (hcpo *HumanControlledTodoPlannerOrchestrator) sanitizeTitleForAgentName(title string) string {
	sanitized := strings.TrimSpace(title)

	// Remove step number prefixes (case-insensitive)
	// Matches: "Step N:", "Step N -", "Step N.", "Step N ", etc.
	stepNumberPattern := regexp.MustCompile(`(?i)^step\s+\d+\s*[:.\-]*\s*`)
	sanitized = stepNumberPattern.ReplaceAllString(sanitized, "")

	// Replace spaces with dashes
	sanitized = strings.ReplaceAll(sanitized, " ", "-")

	// Remove or replace special characters that aren't safe for agent names
	// Keep: letters, numbers, dashes, underscores
	// Remove: colons, slashes, backslashes, pipes, etc.
	specialCharPattern := regexp.MustCompile(`[^a-zA-Z0-9\-_]`)
	sanitized = specialCharPattern.ReplaceAllString(sanitized, "-")

	// Normalize multiple consecutive dashes to single dash
	multiDashPattern := regexp.MustCompile(`-+`)
	sanitized = multiDashPattern.ReplaceAllString(sanitized, "-")

	// Remove leading/trailing dashes
	sanitized = strings.Trim(sanitized, "-")

	// Convert to lowercase for consistency
	sanitized = strings.ToLower(sanitized)

	// Ensure we have something left (fallback if everything was removed)
	if sanitized == "" {
		sanitized = "step"
	}

	return sanitized
}

// readLearningHistory reads learning history from the learnings folder
// Returns the formatted learning history string and any error
// Always reads fresh learnings (no caching)
func (hcpo *HumanControlledTodoPlannerOrchestrator) readLearningHistory(
	ctx context.Context,
	stepIndex int,
	stepID string,
	stepPath string,
) (formattedLearningHistory string, err error) {
	// Always read learnings (no caching)
	hcpo.GetLogger().Info(fmt.Sprintf("🔀 Reading learning history for %s (ID: %s)", stepPath, stepID))

	// Determine step folder path - learnings are at workspace root (not inside runs/)
	// Use step ID based path for learnings (new format)
	baseWorkspacePath := hcpo.GetWorkspacePath()
	stepLearningsPath := getLearningFolderPathByStepID(baseWorkspacePath, stepID, stepPath)

	// Read learning files from step folder (works for both regular and branch steps)
	// This automatically excludes metadata files and checks all subfolders (code/, scripts/)
	learningFiles, err := hcpo.readStepLearningFiles(ctx, stepLearningsPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read learning files from %s: %v - will proceed without learning history", stepLearningsPath, err))
		formattedLearningHistory = ""
	} else if len(learningFiles) > 0 {
		// Format file contents as learning history (only when we have files)
		formattedLearningHistory, _ = hcpo.formatStepLearningFilesAsHistory(learningFiles)
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Read %d learning file(s) from step folder for %s", len(learningFiles), stepPath))

		// Note: We no longer save previous learnings content to metadata
		// Previous learnings are read directly from files before the learning phase runs
	} else {
		// No learning files found
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ No learning files found for %s - learnings folder is empty: %s", stepPath, stepLearningsPath))
		formattedLearningHistory = ""
	}

	return formattedLearningHistory, nil
}
