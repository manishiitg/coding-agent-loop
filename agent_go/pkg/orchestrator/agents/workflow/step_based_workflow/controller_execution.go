package step_based_workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/shared"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	baseevents "mcpagent/events"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// KnowledgebaseFolderName is the name of the persistent knowledgebase folder at workspace root
// This folder is never deleted during cleanup operations and is shared across all runs
const KnowledgebaseFolderName = "knowledgebase"

// getKnowledgebasePath returns the full path to the knowledgebase folder
// Path format: {workspaceRoot}/knowledgebase/
// Knowledgebase is at workspace root level (same as runs/, planning/, learnings/) to be shared across all runs
func getKnowledgebasePath(workspaceRoot string) string {
	return fmt.Sprintf("%s/%s", workspaceRoot, KnowledgebaseFolderName)
}

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
	// Sub-agent step pattern: "step-{number}-sub-agent-{index}" or "step-{number}-sub-agent-{index}-i-{iteration}"
	subAgentStepRegex := regexp.MustCompile(`^step-(\d+)-sub-agent-(\d+)(?:-i-(\d+))?$`)

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

// =============================================================================
// IMPORTANT: Workspace File/Folder Operations
// =============================================================================
// NEVER use os.MkdirAll, os.WriteFile, os.Remove, or other os.* functions directly
// for workspace file/folder operations. Always use the Workspace API instead.
//
// Reason: The Workspace API ensures consistency between:
// - Folder/file creation
// - list_workspace_files tool (used by LLM agents)
// - read_workspace_file tool (used by LLM agents)
// - update_workspace_file tool (used by LLM agents)
//
// Using os.* directly can cause "folder does not exist" errors because the
// Workspace API may have a different root path than the Go agent's filesystem.
//
// Use these functions instead:
// - createFolderViaAPI() - for creating folders
// - WriteWorkspaceFile() - for creating/updating files (auto-creates parent dirs)
// - Workspace API endpoints directly when needed
// =============================================================================

// getWorkspaceAPIURL returns the workspace API base URL from environment or default
func getWorkspaceAPIURL() string {
	if url := os.Getenv("WORKSPACE_API_URL"); url != "" {
		return url
	}
	return "http://localhost:8081"
}

// normalizePathForWorkspaceAPI normalizes a relative path to be relative to workspace-docs root.
//
// The Workspace API expects all paths relative to workspace-docs root (e.g., "Workflow/ICICI.../runs/...").
// This function handles two input path formats:
//
//  1. Paths relative to workflow workspace (e.g., "learnings/step-1", "runs/iteration-1")
//     - Prepends the workspacePath to create full relative path
//
//  2. Paths already relative to workspace-docs root (e.g., "Workflow/ICICI.../runs/...")
//     - Returns as-is (already in correct format)
//
// IMPORTANT: Absolute paths are NOT allowed and will return empty string (triggering an error).
// All paths should be relative to the workspace. If you have an absolute path, that's a bug.
//
// Parameters:
//   - path: The relative path to normalize (must NOT be absolute)
//   - workspacePath: The workflow workspace path relative to workspace-docs root
//     (e.g., "Workflow/ICICI Bank Account Opening"). Pass empty string if path is already
//     relative to workspace-docs root.
//
// Returns the path relative to workspace-docs root, suitable for Workspace API calls.
func normalizePathForWorkspaceAPI(path string, workspacePath string) string {
	if path == "" {
		return ""
	}

	// Clean the path to remove redundant separators and dots
	path = filepath.Clean(path)

	// REJECT absolute paths - this is always a bug
	if filepath.IsAbs(path) {
		panic(fmt.Sprintf("normalizePathForWorkspaceAPI: Absolute paths are not allowed: %s. All paths must be relative to workspace (e.g., 'Workflow/...'). Fix the caller.", path))
	}

	// Remove leading slash if present (relative paths should not start with /)
	path = strings.TrimPrefix(path, "/")

	// If path is already relative to workspace-docs root (starts with workspacePath),
	// return it as-is
	if workspacePath != "" {
		cleanWorkspacePath := strings.TrimPrefix(filepath.Clean(workspacePath), "/")
		if strings.HasPrefix(path, cleanWorkspacePath) {
			return path
		}

		// Path is relative to workflow workspace - prepend workspacePath
		// e.g., "learnings/step-1" -> "Workflow/ICICI.../learnings/step-1"
		return filepath.Join(cleanWorkspacePath, path)
	}

	return path
}

// createFolderViaAPI creates a folder via the Workspace API (POST /api/folders).
//
// The folderPath parameter can be in any format - this function normalizes it internally.
// If workspacePath is provided, it will be used to convert workflow-relative paths
// to workspace-docs-relative paths.
//
// Parameters:
//   - ctx: Context for the HTTP request
//   - folderPath: Path to create (absolute, workspace-relative, or workflow-relative)
//   - workspacePath: Optional workflow workspace path for normalization (e.g., "Workflow/ICICI...").
//     Pass empty string if folderPath is already relative to workspace-docs root.
func createFolderViaAPI(ctx context.Context, folderPath string, workspacePath ...string) error {
	// Normalize the path for the Workspace API
	wp := ""
	if len(workspacePath) > 0 {
		wp = workspacePath[0]
	}
	normalizedPath := normalizePathForWorkspaceAPI(folderPath, wp)

	if normalizedPath == "" {
		return fmt.Errorf("cannot create folder: path is empty after normalization")
	}

	apiURL := getWorkspaceAPIURL() + "/api/folders"

	// Debug logging
	fmt.Printf("[DEBUG createFolderViaAPI] Creating folder via API: %s (original: %s, workspacePath: %s) at %s\n",
		normalizedPath, folderPath, wp, apiURL)

	// Prepare request body with normalized path
	requestBody := map[string]string{
		"folder_path": normalizedPath,
	}
	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		fmt.Printf("[DEBUG createFolderViaAPI] Failed to marshal request body: %v\n", err)
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(jsonBody))
	if err != nil {
		fmt.Printf("[DEBUG createFolderViaAPI] Failed to create request: %v\n", err)
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Set timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[DEBUG createFolderViaAPI] Failed to call workspace API: %v\n", err)
		return fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("[DEBUG createFolderViaAPI] Failed to read response: %v\n", err)
		return fmt.Errorf("failed to read response: %w", err)
	}

	fmt.Printf("[DEBUG createFolderViaAPI] Response status: %d, body: %s\n", resp.StatusCode, string(body))

	// Check HTTP status - 201 Created or 409 Conflict (folder already exists) are both OK
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusConflict {
		fmt.Printf("[DEBUG createFolderViaAPI] Unexpected status code: %d\n", resp.StatusCode)
		return fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}

	fmt.Printf("[DEBUG createFolderViaAPI] Successfully created folder: %s\n", normalizedPath)
	return nil
}

// ensureStepExecutionFolderExists ensures the step execution folder exists by creating it if needed.
// This is called when a step starts running to ensure the folder exists even if it was previously deleted.
// Creates folder via Workspace API only (ensures consistency with list_workspace_files).
//
// The stepExecutionPath can be in any format - the function normalizes it internally using
// the orchestrator's workspace path.
func (hcpo *StepBasedWorkflowOrchestrator) ensureStepExecutionFolderExists(ctx context.Context, stepExecutionPath string) error {
	if stepExecutionPath == "" {
		return fmt.Errorf("invalid step execution path: empty")
	}

	fmt.Printf("[DEBUG ensureStepExecutionFolderExists] Called with stepExecutionPath: %s\n", stepExecutionPath)

	// Create folder via Workspace API - normalization happens inside createFolderViaAPI
	// Pass empty workspacePath since stepExecutionPath is already relative to workspace-docs root
	if err := createFolderViaAPI(ctx, stepExecutionPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create step execution folder via API: %s: %v", stepExecutionPath, err))
		return fmt.Errorf("failed to create step execution folder: %w", err)
	}

	return nil
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

// getLearningFolderPathByStepID returns the RELATIVE learning folder path using step ID (NEW FORMAT)
// For all steps (regular, branch, sub-agent): "learnings/{stepID}/"
// For evaluation steps (when isEvaluationMode=true): "evaluation/learnings/{stepID}/"
// All steps have their own unique step IDs, so we just use the stepID directly
// NOTE: This returns a RELATIVE path for use with workspace functions (ReadWorkspaceFile, WriteWorkspaceFile, etc.)
// The baseWorkspacePath parameter is IGNORED and kept only for backward compatibility - will be removed in future
func getLearningFolderPathByStepID(baseWorkspacePath string, stepID string, stepPath string, isEvaluationMode bool) string {
	// All steps (regular, branch, sub-agent) have their own unique step IDs
	// Just use the stepID directly without any suffix
	// Return RELATIVE path - workspace functions auto-prepend workspacePath
	if isEvaluationMode {
		return fmt.Sprintf("evaluation/learnings/%s", stepID)
	}
	return fmt.Sprintf("learnings/%s", stepID)
}

// ensureStepLearningsFolderExists ensures the step learnings folder exists by creating it if needed.
// Takes a relative path within the workspace (e.g., "learnings/step-1") and uses createFolderViaAPI
// to create it with proper path normalization.
func (hcpo *StepBasedWorkflowOrchestrator) ensureStepLearningsFolderExists(ctx context.Context, stepLearningsRelativePath string) error {
	if stepLearningsRelativePath == "" {
		return fmt.Errorf("invalid step learnings path: empty")
	}

	// Create folder via Workspace API with workspacePath for normalization
	// e.g., "learnings/step-1" + "Workflow/ICICI..." -> "Workflow/ICICI.../learnings/step-1"
	workspacePath := hcpo.GetWorkspacePath()
	if err := createFolderViaAPI(ctx, stepLearningsRelativePath, workspacePath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create step learnings folder via API: %s: %v", stepLearningsRelativePath, err))
		return fmt.Errorf("failed to create step learnings folder: %w", err)
	}
	return nil
}

// addCompletedStepIndex safely adds a step index to the completed list, preventing duplicates
// This is important when decision steps route back to previous steps, which can cause
// the same step index to be added multiple times if not checked
func (hcpo *StepBasedWorkflowOrchestrator) addCompletedStepIndex(progress *StepProgress, stepIndex int) {
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
func (hcpo *StepBasedWorkflowOrchestrator) gatherPrerequisiteInfo(
	step PlanStepInterface,
	stepIndex int,
	allSteps []PlanStepInterface,
	progress *StepProgress,
	workspacePath string,
) *PrerequisiteInfo {
	// Check if prerequisite detection is enabled
	agentConfigs := getAgentConfigs(step)
	if agentConfigs == nil || agentConfigs.EnablePrerequisiteDetection == nil || !*agentConfigs.EnablePrerequisiteDetection {
		return nil // Not enabled, return nil
	}

	// If allSteps is nil (e.g., in branch/conditional context), we can't gather prerequisite info
	if allSteps == nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Prerequisite detection enabled for step %d but allSteps not available (branch/conditional context)", stepIndex+1))
		return nil
	}

	// Get prerequisite rules
	prerequisiteRules := agentConfigs.PrerequisiteRules
	if len(prerequisiteRules) == 0 {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Prerequisite detection enabled for step %d but no prerequisite_rules configured", stepIndex+1))
		return nil
	}

	// Create map of step ID to step index for quick lookup
	stepIDToIndex := make(map[string]int)
	for i, s := range allSteps {
		stepID := s.GetID()
		if stepID != "" {
			stepIDToIndex[stepID] = i
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
		contextOutput := depStep.GetContextOutput().String()
		if contextOutput != "" {
			// Resolve context output path
			contextOutputPath := filepath.Join(workspacePath, "execution", contextOutput)
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
				StepTitle:           depStep.GetTitle(),
				IsCompleted:         isCompleted,
				ContextOutput:       contextOutput,
				ContextOutputExists: contextOutputExists,
			},
		})
	}

	if len(ruleInfos) == 0 {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ No valid prerequisite rules found for step %d", stepIndex+1))
		return nil
	}

	return &PrerequisiteInfo{
		CurrentStepID:               step.GetID(),
		CurrentStepIndex:            stepIndex,
		EnablePrerequisiteDetection: true,
		PrerequisiteRules:           ruleInfos,
	}
}

// formatPrerequisiteRulesForExecutionAgent formats prerequisite rules for the execution agent system prompt
// This provides the LLM with information about available prerequisite rules and when to call the tool
func (hcpo *StepBasedWorkflowOrchestrator) formatPrerequisiteRulesForExecutionAgent(prerequisiteInfo *PrerequisiteInfo) string {
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
func (hcpo *StepBasedWorkflowOrchestrator) createPrerequisiteDetectionTool(prerequisiteInfo *PrerequisiteInfo, allSteps []PlanStepInterface, currentStepIndex int, cancelFunc context.CancelFunc, prereqErrChan chan<- *PrerequisiteFailureError) func(ctx context.Context, args map[string]interface{}) (string, error) {
	// Create map of step ID to step index for validation
	stepIDToIndex := make(map[string]int)
	for i, s := range allSteps {
		if s.GetID() != "" {
			stepIDToIndex[s.GetID()] = i
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

// buildOtherAgentsCapabilitiesSummary builds a formatted summary of other sub-agents' capabilities
// This helps sub-agents know what other agents are optimized for, so they can communicate with the orchestrator
// if they encounter something better suited for another agent
// currentSubAgentStep: The current sub-agent step (to exclude from the list)
// orchestrationRoutes: All available orchestration routes (sub-agents)
func (hcpo *StepBasedWorkflowOrchestrator) buildOtherAgentsCapabilitiesSummary(currentSubAgentStep PlanStepInterface, orchestrationRoutes []OrchestrationRoute) string {
	if len(orchestrationRoutes) == 0 {
		return "" // No other agents
	}

	var summary strings.Builder
	summary.WriteString("## 🤝 Other Sub-Agents Capabilities\n\n")
	summary.WriteString("You are part of an orchestration step with other specialized sub-agents. ")
	summary.WriteString("If you encounter a task that another agent is better optimized for, ")
	summary.WriteString("you can communicate this to the orchestrator in your output.\n\n")
	summary.WriteString("**Available Sub-Agents:**\n\n")

	agentCount := 0
	for _, route := range orchestrationRoutes {
		// Skip the current sub-agent (don't list itself)
		if route.SubAgentStep.GetID() == currentSubAgentStep.GetID() {
			continue
		}

		// Resolve variables in agent information
		routeName := ResolveVariables(route.RouteName, hcpo.variableValues)
		condition := ResolveVariables(route.Condition, hcpo.variableValues)

		summary.WriteString(fmt.Sprintf("**%s** (Route ID: `%s`)\n", routeName, route.RouteID))
		summary.WriteString(fmt.Sprintf("- **Specialization**: %s\n", condition))
		if route.ContextToPass != "" {
			summary.WriteString(fmt.Sprintf("- **Context Focus**: %s\n", ResolveVariables(route.ContextToPass, hcpo.variableValues)))
		}
		summary.WriteString("\n")

		agentCount++
	}

	if agentCount == 0 {
		return "" // No other agents (only current one)
	}

	summary.WriteString("**How to Communicate with Orchestrator:**\n\n")
	summary.WriteString("If you encounter a task that matches another agent's specialization, ")
	summary.WriteString("include a clear note in your output like:\n\n")
	summary.WriteString("```\n")
	summary.WriteString("🤝 ORCHESTRATOR SUGGESTION: I encountered [task description] which appears to be ")
	summary.WriteString("better suited for the [Route Name] agent (route_id: [route_id]). ")
	summary.WriteString("Reason: [why this agent is better suited].\n")
	summary.WriteString("```\n\n")
	summary.WriteString("The orchestrator will review your suggestion and may route the task to the appropriate agent.\n")

	return summary.String()
}

// loadExecutionResultsFromLogs loads execution results from logs folder for previous steps
// This is a shared/reusable function that can be called from anywhere in the controller
// It's used when resuming from a step or running a single step, where execution results aren't in memory
// Returns an array of execution results indexed by step index (0-based)
// For each step, it finds the latest execution result file (highest attempt, then highest iteration)
func (hcpo *StepBasedWorkflowOrchestrator) loadExecutionResultsFromLogs(ctx context.Context, allSteps []PlanStepInterface, currentStepIndex int) []string {
	executionResults := make([]string, currentStepIndex)

	// Determine validation workspace path
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}

	// Load execution results for each previous step
	for i := 0; i < currentStepIndex && i < len(allSteps); i++ {
		// Determine step path (similar to how it's done in executeSingleStep)
		stepPath := fmt.Sprintf("step-%d", i+1)

		// Get execution logs folder path
		executionLogsFolderPath := getExecutionFolderPathForLogs(validationWorkspacePath, stepPath)

		// Try to find the latest execution result file
		// Pattern: execution-attempt-{N}-iteration-{M}.json
		// We'll try a few common attempts and iterations, looking for the latest one
		var latestExecutionResult string
		var latestAttempt, latestIteration int

		for attempt := 1; attempt <= 10; attempt++ {
			for iteration := 0; iteration <= 10; iteration++ {
				executionResultFilePath := fmt.Sprintf("%s/execution-attempt-%d-iteration-%d.json", executionLogsFolderPath, attempt, iteration)
				content, err := hcpo.ReadWorkspaceFile(ctx, executionResultFilePath)
				if err != nil {
					// File doesn't exist, try next
					continue
				}

				// Parse JSON to extract execution_result
				var executionData map[string]interface{}
				if err := json.Unmarshal([]byte(content), &executionData); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to parse execution result from %s: %v", executionResultFilePath, err))
					continue
				}

				// Extract execution_result field
				if execResult, ok := executionData["execution_result"].(string); ok {
					// Keep track of the latest one (highest attempt, then highest iteration)
					if attempt > latestAttempt || (attempt == latestAttempt && iteration > latestIteration) {
						latestExecutionResult = execResult
						latestAttempt = attempt
						latestIteration = iteration
					}
				}
			}
		}

		if latestExecutionResult != "" {
			executionResults[i] = latestExecutionResult
			hcpo.GetLogger().Info(fmt.Sprintf("📖 Loaded execution result from logs for step %d (attempt %d, iteration %d)", i+1, latestAttempt, latestIteration))
		}
	}

	return executionResults
}

// buildPreviousStepsSummary builds a formatted summary of previous completed steps
// This provides context to the execution agent about what steps have already been executed
// previousExecutionResults: array of execution outputs from previous steps (indexed by step index)
func (hcpo *StepBasedWorkflowOrchestrator) buildPreviousStepsSummary(allSteps []PlanStepInterface, currentStepIndex int, previousContextFiles []string, previousExecutionResults []string) string {
	if len(allSteps) == 0 || currentStepIndex == 0 || len(previousContextFiles) == 0 {
		return "" // No previous steps
	}

	// Create a map of context output files to step indices for quick lookup
	contextFileToStepIndex := make(map[string]int)
	for i := 0; i < currentStepIndex && i < len(allSteps); i++ {
		contextOutput := allSteps[i].GetContextOutput().String()
		if contextOutput != "" {
			// Resolve variables in context output to match what's in previousContextFiles
			resolvedOutput := ResolveVariables(contextOutput, hcpo.variableValues)
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
		contextOutput := step.GetContextOutput().String()
		if contextOutput == "" {
			continue // Skip steps without context output
		}

		// Check if this step's context output is in previousContextFiles
		resolvedOutput := ResolveVariables(contextOutput, hcpo.variableValues)
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
		resolvedTitle := ResolveVariables(step.GetTitle(), hcpo.variableValues)
		resolvedDescription := ResolveVariables(step.GetDescription(), hcpo.variableValues)

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

	// Add execution output from the immediately previous step only (most recent)
	previousStepIndex := currentStepIndex - 1
	if previousStepIndex >= 0 && previousStepIndex < len(previousExecutionResults) && previousExecutionResults[previousStepIndex] != "" {
		execOutput := previousExecutionResults[previousStepIndex]
		// Truncate execution output if too long (keep first 2000 characters)
		if len(execOutput) > 2000 {
			execOutput = execOutput[:2000] + "\n... (truncated)"
		}

		// Get previous step title for context
		var previousStepTitle string
		if previousStepIndex < len(allSteps) {
			previousStepTitle = ResolveVariables(allSteps[previousStepIndex].GetTitle(), hcpo.variableValues)
		} else {
			previousStepTitle = fmt.Sprintf("Step %d", previousStepIndex+1)
		}

		summary.WriteString(fmt.Sprintf("\n## 📤 Previous Step Execution Output\n\n"))
		summary.WriteString(fmt.Sprintf("**Step %d: %s** execution result:\n\n", previousStepIndex+1, previousStepTitle))
		summary.WriteString(fmt.Sprintf("```\n%s\n```\n", execOutput))
		summary.WriteString("\nUse this execution output to understand what the immediately previous step accomplished.\n")
	}

	return summary.String()
}

// executeSingleStep executes a single step with full functionality (execution, validation, learning, human feedback)
// This is a reusable function extracted from runExecutionPhase to support both regular steps and branch steps
func (hcpo *StepBasedWorkflowOrchestrator) executeSingleStep(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string, // e.g., "step-1" or "step-1-if-true-0" for branch steps
	totalSteps int,
	iteration int,
	previousContextFiles []string,
	progress *StepProgress,
	isBranchStep bool, // true if this is a branch step (affects progress tracking)
	execCtx *ExecutionContext, // Execution context with flags (skipHumanInput, fastExecuteMode, etc.)
	allSteps []PlanStepInterface, // All steps in the plan (for prerequisite detection)
	isDecisionInnerStep bool, // true if this is the inner step of a decision step (skips final human feedback on success)
	decisionContext *DecisionContext, // Optional: context from decision step that routed to this step (nil if not routed from decision)
	decisionEvaluationQuestion string, // Optional: evaluation question for decision inner steps (used to format output for LLM evaluation)
	isSubAgent bool, // true if this is a sub-agent from an orchestration step (never requests human feedback)
	previousExecutionResults []string, // Execution outputs from previous steps (indexed by step index)
	orchestrationRoutes []OrchestrationRoute, // Optional: orchestration routes (sub-agents) - only used when isSubAgent is true
) (executionResult string, updatedContextFiles []string, err error) {
	// Initialize updated context files as copy of previous context files
	updatedContextFiles = make([]string, len(previousContextFiles))
	copy(updatedContextFiles, previousContextFiles)

	// Emit step_started event (also emits step progress with status="start")
	// Note: Conditional steps emit their own step_started event in executeConditionalStep before calling executeSingleStep for branch steps
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, stepPath, isBranchStep)

	// STEP HASH GUARD: Detect plan changes and reset learnings if needed
	// This ensures that if the user modifies a step's description, criteria, etc.,
	// the learning stability counters are reset and learnings are unlocked.
	if err := hcpo.CheckAndResetStepHash(ctx, step, step.GetID(), stepIndex, stepPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step hash guard check failed for %s: %v (continuing)", stepPath, err))
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
			return "", updatedContextFiles, fmt.Errorf("step execution canceled: %w", ctx.Err())
		default:
		}

		// Prepare template variables for this specific step with individual fields
		// RESOLVE VARIABLES: Replace {{VARS}} with actual values for execution
		// Execution agent workspace path includes run folder: workspacePath/runs/{selectedRunFolder}/execution
		runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
		// Determine code execution mode: Priority: step config > preset default
		var isCodeExecutionMode bool
		agentConfigs := getAgentConfigs(step)
		if agentConfigs != nil && agentConfigs.UseCodeExecutionMode != nil {
			isCodeExecutionMode = *agentConfigs.UseCodeExecutionMode
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode: %v", isCodeExecutionMode))
		} else {
			isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset code execution mode: %v", isCodeExecutionMode))
		}
		// Determine tool search mode: Priority: step config > preset default
		isToolSearchMode := hcpo.getToolSearchMode(agentConfigs)

		// Always use learnings folder (unified folder for all learning types)
		learningsPath := fmt.Sprintf("%s/learnings", hcpo.GetWorkspacePath())
		// Get execution folder path for this step (e.g., "execution/step-8" or "execution/step-3-true-0")
		stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, stepPath)
		// Ensure step execution folder exists (create if it was previously deleted)
		if err := hcpo.ensureStepExecutionFolderExists(ctx, stepExecutionPath); err != nil {
			// Non-blocking: log warning but continue execution (folder will be created when files are written)
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure step execution folder exists: %v (continuing - folder will be created when files are written)", err))
		}
		// Get knowledgebase folder path (persistent files across runs, at workspace root) - only if enabled
		knowledgebasePath := ""
		useKnowledgebase := hcpo.UseKnowledgebase()
		if useKnowledgebase {
			knowledgebasePath = getKnowledgebasePath(hcpo.GetWorkspacePath())
		}

		// Get folder guard paths for template (so agent knows exact paths it can access)
		// Use step.GetID() as stepID for folder guard setup
		// Check if learnings exist to determine if learnings folder should be included
		hasLearnings := false
		learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, step.GetID(), stepIndex, stepPath)
		if err == nil {
			hasLearnings = !learningsEmpty
		}
		folderGuardReadPaths, folderGuardWritePaths := hcpo.setupExecutionFolderGuard(stepPath, step.GetID(), hasLearnings)

		templateVars := map[string]string{
			"StepTitle":             ResolveVariables(step.GetTitle(), hcpo.variableValues),
			"StepDescription":       ResolveVariables(step.GetDescription(), hcpo.variableValues),
			"StepSuccessCriteria":   ResolveVariables(step.GetSuccessCriteria(), hcpo.variableValues),
			"StepContextOutput":     ResolveVariables(step.GetContextOutput().String(), hcpo.variableValues),
			"WorkspacePath":         executionWorkspacePath,                    // Execution subdirectory (folder guard validates against this)
			"LearningsPath":         learningsPath,                             // Learnings folder path for reading learning files and scripts/code
			"KnowledgebasePath":     knowledgebasePath,                         // Knowledgebase folder path (persistent files across runs) - empty if disabled
			"UseKnowledgebase":      fmt.Sprintf("%v", useKnowledgebase),       // Whether knowledgebase is enabled
			"IsCodeExecutionMode":   fmt.Sprintf("%v", isCodeExecutionMode),    // Code execution mode flag (step-specific or preset)
			"UseToolSearchMode":     fmt.Sprintf("%v", isToolSearchMode),       // Tool search mode flag (step-specific or preset)
			"HumanFeedback":         "",                                        // Human feedback for retry attempts (set after validation failure)
			"StepNumber":            stepPath,                                  // Step identifier (e.g., "step-8" or "step-3-if-true-0")
			"StepExecutionPath":     stepExecutionPath,                         // Full execution folder path (e.g., "execution/step-8")
			"FolderGuardReadPaths":  strings.Join(folderGuardReadPaths, ", "),  // Folder guard read paths for agent guidance
			"FolderGuardWritePaths": strings.Join(folderGuardWritePaths, ", "), // Folder guard write paths for agent guidance
		}

		// Add context dependencies as a comma-separated string (also resolve variables)
		contextDeps := step.GetContextDependencies()
		if len(contextDeps) > 0 {
			resolvedDeps := ResolveVariablesArray(contextDeps, hcpo.variableValues)
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
		} else {
			templateVars["DecisionReasoning"] = ""
		}

		// Build previous steps summary from completed steps (include execution outputs)
		previousStepsSummary := hcpo.buildPreviousStepsSummary(allSteps, stepIndex, previousContextFiles, previousExecutionResults)
		templateVars["PreviousStepsSummary"] = previousStepsSummary
		if previousStepsSummary != "" {
		}

		// Build other agents capabilities summary for sub-agents
		if isSubAgent && len(orchestrationRoutes) > 0 {
			otherAgentsCapabilities := hcpo.buildOtherAgentsCapabilitiesSummary(step, orchestrationRoutes)
			templateVars["OtherAgentsCapabilities"] = otherAgentsCapabilities
			if otherAgentsCapabilities != "" {
				hcpo.GetLogger().Info(fmt.Sprintf("🤝 Added other agents capabilities summary to template variables for sub-agent %s", stepPath))
			}
		} else {
			templateVars["OtherAgentsCapabilities"] = ""
		}

		// Add validation schema to template variables so execution agent knows expected file structure
		validationSchema := getValidationSchema(step)
		if validationSchema != nil {
			validationSchemaJSON, err := json.MarshalIndent(validationSchema, "", "  ")
			if err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to marshal validation schema for step %d: %v", stepIndex+1, err))
				templateVars["ValidationSchema"] = ""
			} else {
				templateVars["ValidationSchema"] = string(validationSchemaJSON)
			}
		} else {
			templateVars["ValidationSchema"] = ""
		}

		// Validate loop condition is provided when has_loop is true
		if hasLoop(step) {
			stepHasLoop, loopCondition, maxIterations, _ := getLoopFields(step)
			if !stepHasLoop {
				// Should not happen, but handle gracefully
				return "", updatedContextFiles, fmt.Errorf("step %d: hasLoop returned true but getLoopFields returned false", stepIndex+1)
			}
			if loopCondition == "" {
				return "", updatedContextFiles, fmt.Errorf("step %d has has_loop=true but loop_condition is empty (required)", stepIndex+1)
			}
			// Set default max_iterations if not provided
			if maxIterations == 0 {
				// Update the step's MaxIterations field
				if regularStep, ok := step.(*RegularPlanStep); ok {
					regularStep.MaxIterations = 10
					hcpo.GetLogger().Info(fmt.Sprintf("⚠️ Step %d has loop but no max_iterations specified, using default: 10", stepIndex+1))
				}
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
				return "", updatedContextFiles, fmt.Errorf("step execution canceled: %w", ctx.Err())
			default:
			}

			// Initialize loop state on first iteration
			if loopIteration == 0 && hasLoop(step) {
				loopConditionMet = false
				loopIterationCount = 0
				previousIterationExecutionOutput = ""
				previousIterationValidationOutput = ""
				_, loopCondition, maxIterations, _ := getLoopFields(step)
				hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step %d loop starting (max iterations: %d, condition: %s)", stepIndex+1, maxIterations, loopCondition))
			} else if loopIteration > 0 && hasLoop(step) {
				// Previous iteration outputs are passed via template variables (PreviousIterationOutput)
				// Execution conversation history will be captured fresh from this iteration for learning agents
				_, _, maxIterations, _ := getLoopFields(step)
				hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step %d loop iteration %d/%d starting", stepIndex+1, loopIterationCount, maxIterations))
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
				_, loopCondition, maxIterations, _ := getLoopFields(step)
				if loopIterationCount >= maxIterations {
					hcpo.GetLogger().Error(fmt.Sprintf("❌ Step %d reached max iterations (%d) without meeting loop condition, requesting human intervention", stepIndex+1, maxIterations), nil)
					// Request human intervention immediately, skip validation
					var err error
					var approved bool
					approved, _, err = hcpo.requestHumanFeedback(ctx, stepIndex+1, totalSteps,
						fmt.Sprintf("Loop reached max iterations (%d) without meeting condition: %s", maxIterations, loopCondition))
					if err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Human feedback request failed: %v", err))
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
				_, _, maxIterations, _ = getLoopFields(step)
				hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step %d loop iteration %d/%d", stepIndex+1, loopIterationCount, maxIterations))
			}

			// Add loop context to template variables if in loop mode
			if hasLoop(step) {
				_, loopCondition, maxIterations, loopDescription := getLoopFields(step)
				templateVars["HasLoop"] = "true"
				templateVars["LoopCondition"] = loopCondition
				templateVars["LoopDescription"] = loopDescription
				templateVars["CurrentIteration"] = fmt.Sprintf("%d", loopIterationCount)
				templateVars["MaxIterations"] = fmt.Sprintf("%d", maxIterations)
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
			resolvedTitle := ResolveVariables(step.GetTitle(), hcpo.variableValues)
			sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)

			// Run learning reading agent ONCE per main loop iteration (before retry loop)
			// This ensures learning is only discovered once, even if validation fails and we retry
			// Always reads fresh learnings (no caching)
			var formattedLearningHistory string
			var learningFilePaths string // File paths for user message when KeepLearningFull is false

			// Determine KeepLearningFull flag
			// Priority: step config > environment variable > dynamic logic (based on successful runs)
			agentConfigs := getAgentConfigs(step)
			var keepLearningFull bool
			var keepLearningFullSource string

			if agentConfigs != nil && agentConfigs.KeepLearningFull != nil {
				keepLearningFull = *agentConfigs.KeepLearningFull
				keepLearningFullSource = "step config"
			} else if envVal := os.Getenv("KEEP_LEARNING_FULL"); envVal != "" {
				keepLearningFull = envVal == "true" || envVal == "1"
				keepLearningFullSource = "environment variable"
			} else {
				// Dynamic Logic: Switch based on successful runs
				// Default to Exploration Mode (False - paths only) to encourage trying different ways
				keepLearningFull = false

				// Read metadata to check successful runs
				learningPathIdentifier := step.GetID() // Use ID as identifier
				metadata, err := hcpo.GetLearningMetadata(ctx, learningPathIdentifier)
				if err == nil && metadata != nil {
					// Check thresholds: Simple >= 2, Medium >= 3, Complex >= 5
					if metadata.SuccessfulRunsSimple >= 2 {
						keepLearningFull = true
						keepLearningFullSource = "dynamic (simple threshold met)"
					} else if metadata.SuccessfulRunsMedium >= 3 {
						keepLearningFull = true
						keepLearningFullSource = "dynamic (medium threshold met)"
					} else if metadata.SuccessfulRunsComplex >= 5 {
						keepLearningFull = true
						keepLearningFullSource = "dynamic (complex threshold met)"
					} else {
						keepLearningFullSource = "dynamic (exploration phase)"
					}
				} else {
					// No metadata (first run) or error reading -> Stay in Exploration Mode
					keepLearningFullSource = "dynamic (initial exploration)"
				}
			}

			hcpo.GetLogger().Info(fmt.Sprintf("🧠 KeepLearningFull decision: %v (Source: %s)", keepLearningFull, keepLearningFullSource))

			// Check if learning is disabled - if so, skip reading learnings entirely
			isLearningDisabledStep := agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning
			isLearningDetailLevelNone := false
			if agentConfigs != nil && agentConfigs.LearningDetailLevel == "none" {
				isLearningDetailLevelNone = true
			}
			isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone

			if isLearningDisabled {
				// Learning is disabled - skip reading learnings and set empty strings
				formattedLearningHistory = ""
				learningFilePaths = ""
				hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Learning disabled for step %d - skipping learning history reading (no learnings will be passed to execution agent)", stepIndex+1))
			} else {
				// Learning is enabled - read learning history as normal
				formattedLearningHistory, err = hcpo.readLearningHistory(
					ctx,
					stepIndex,
					step.GetID(),
					stepPath,
				)
				if err != nil {
					return "", updatedContextFiles, fmt.Errorf("failed to read learning history for step %d: %w", stepIndex+1, err)
				}

				// Get learning file paths for user message (when KeepLearningFull is false)
				if !keepLearningFull {
					// Generate file paths list for user message
					// getLearningFolderPathByStepID now returns RELATIVE path - workspace functions auto-prepend workspacePath
					stepLearningsPath := getLearningFolderPathByStepID("", step.GetID(), stepPath, execCtx.IsEvaluationMode)
					learningFiles, readErr := hcpo.readStepLearningFiles(ctx, stepLearningsPath)
					if readErr == nil && len(learningFiles) > 0 {
						// Build list of file paths
						var paths []string
						for filename := range learningFiles {
							// Construct full path relative to workspace
							filePath := fmt.Sprintf("%s/%s", stepLearningsPath, filename)
							paths = append(paths, filePath)
						}
						// Format as bullet list
						if len(paths) > 0 {
							learningFilePaths = strings.Join(paths, "\n- ")
							learningFilePaths = "- " + learningFilePaths
							hcpo.GetLogger().Info(fmt.Sprintf("📁 Generated %d learning file path(s) for user message", len(paths)))
						}
					}
				}
			}

			// Track if validation failed after exhausting all retry attempts
			validationFailedAfterMaxRetries := false

			// Track which tempLLM was used during successful execution (for learning phase decision)
			var usedTempLLM string // "tempLLM1", "tempLLM2", or "" (original LLM)

			// Track which LLM model was used for execution (to be stored in learning metadata)
			var executionLLM string

			// Track failure learning attempts for this execution session
			failureLearningAttempts := 0

			// Retry loop: Execute with validation feedback, reusing the same learning history
			for retryAttempt := 1; retryAttempt <= maxRetryAttempts; retryAttempt++ {
				// Check for context cancellation before retry attempt
				select {
				case <-ctx.Done():
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled during retry attempt %d for step %d", retryAttempt, stepIndex+1))
					return "", updatedContextFiles, fmt.Errorf("step execution canceled: %w", ctx.Err())
				default:
				}

				hcpo.GetLogger().Info(fmt.Sprintf("🔄 Executing step %d/%d (attempt %d/%d): %s", stepIndex+1, totalSteps, retryAttempt, maxRetryAttempts, step.GetTitle()))

				// Track which tempLLM will be used for THIS attempt (BEFORE execution, not after validation)
				// This ensures we can skip failure learning if tempLLM fails validation
				hasTempLLM1 := hcpo.tempOverrideLLM != nil && hcpo.tempOverrideLLM.Provider != "" && hcpo.tempOverrideLLM.ModelID != ""
				hasTempLLM2 := hcpo.tempOverrideLLM2 != nil && hcpo.tempOverrideLLM2.Provider != "" && hcpo.tempOverrideLLM2.ModelID != ""
				// Only use tempLLM if learnings exist (check if learning history was loaded)
				hasLearnings := formattedLearningHistory != ""
				if retryAttempt == 1 && hasTempLLM1 && hasLearnings {
					usedTempLLM = "tempLLM1"
					hcpo.GetLogger().Info(fmt.Sprintf("📍 [TRACKING] Will use tempLLM1 for attempt %d (learnings exist)", retryAttempt))
				} else if retryAttempt == 2 && hasTempLLM2 && hasLearnings {
					usedTempLLM = "tempLLM2"
					hcpo.GetLogger().Info(fmt.Sprintf("📍 [TRACKING] Will use tempLLM2 for attempt %d (learnings exist)", retryAttempt))
				} else {
					usedTempLLM = "" // Original LLM
					hcpo.GetLogger().Info(fmt.Sprintf("📍 [TRACKING] Will use original LLM for attempt %d", retryAttempt))
				}

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
				} else {
					templateVars["ValidationFeedback"] = "" // No validation feedback for first attempt/first iteration
				}

				// Note: HumanFeedback is set in templateVars after validation failure (see validation failure handling above)
				// It persists across retry attempts until cleared or step succeeds

				// Step 2: Create and execute Execution-Only Agent with learning history (reused from above)
				executionAgentName := fmt.Sprintf("%s-execution-%s", stepPath, sanitizedTitle)
				// Add validation retry suffix if this is a retry after validation failure (val-2, val-3, etc.)
				if retryAttempt > 1 {
					executionAgentName = fmt.Sprintf("%s-val-%d", executionAgentName, retryAttempt)
				}
				// Add loop iteration to agent name if in loop mode
				if hasLoop(step) && loopIterationCount > 0 {
					executionAgentName = fmt.Sprintf("%s-loop-%d", executionAgentName, loopIterationCount)
				}

				// Add learning history to template vars for execution-only agent (reused for all retry attempts)
				templateVars["LearningHistory"] = formattedLearningHistory
				// Set HasLearnings flag to explicitly indicate whether learnings exist (prevents agent from searching)
				templateVars["HasLearnings"] = fmt.Sprintf("%t", formattedLearningHistory != "")

				// Set KeepLearningFull feature flag (already determined above, just log and set template var)
				if agentConfigs != nil && agentConfigs.KeepLearningFull != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step config KeepLearningFull: %v", keepLearningFull))
				} else if envVal := os.Getenv("KEEP_LEARNING_FULL"); envVal != "" {
					hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using environment variable KEEP_LEARNING_FULL: %v", keepLearningFull))
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using default KeepLearningFull: true (full content in system prompt)"))
				}
				templateVars["KeepLearningFull"] = fmt.Sprintf("%t", keepLearningFull)
				templateVars["LearningFilePaths"] = learningFilePaths // Set file paths for user message when KeepLearningFull is false

				// Check for context cancellation before creating execution agent
				select {
				case <-ctx.Done():
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled before creating execution agent for step %d", stepIndex+1))
					return "", updatedContextFiles, fmt.Errorf("step execution canceled: %w", ctx.Err())
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
				// Gather prerequisite info if enabled (needed for tool registration and prompt)
				var prerequisiteInfoForExecution *PrerequisiteInfo

				// Prefer AgentConfigs flag if present; otherwise fall back to implicit enablement
				// when prerequisite rules exist at the AgentConfigs level. PlanStepInterface now carries
				// the top-level planning fields (EnablePrerequisiteDetection / PrerequisiteRules),
				// but at execution time we rely on AgentConfigs only.
				agentConfigs := getAgentConfigs(step)
				enablePrereq := false
				if agentConfigs != nil && agentConfigs.EnablePrerequisiteDetection != nil {
					enablePrereq = *agentConfigs.EnablePrerequisiteDetection
				} else if agentConfigs != nil && len(agentConfigs.PrerequisiteRules) > 0 {
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
				// allSteps is already []PlanStepInterface - no conversion needed
				executionAgent, err = hcpo.createExecutionOnlyAgent(executionCtx, "execution_only", stepPath, executionAgentName, agentConfigs, isRetryAfterValidationFailure, retryAttempt, prerequisiteInfoForExecution, allSteps, stepIndex, cancelExecution, prereqErrChan, step.GetID())
				if err != nil {
					return "", updatedContextFiles, fmt.Errorf("failed to create execution-only agent for step %d: %w", stepIndex+1, err)
				}

				// Check for context cancellation before executing agent
				select {
				case <-ctx.Done():
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled before agent execution for step %d", stepIndex+1))
					return "", updatedContextFiles, fmt.Errorf("step execution canceled: %w", ctx.Err())
				default:
				}

				// Execute execution-only agent with learning history (reused from learning reading above)
				executionResult, executionConversationHistory, err = executionAgent.Execute(executionCtx, templateVars, []llmtypes.MessageContent{})

				// CAPTURE EXECUTION LLM: Get the model used for execution (to be stored in learning metadata)
				if executionAgent != nil && executionAgent.GetConfig() != nil {
					config := executionAgent.GetConfig()
					if config.LLMConfig.Primary.ModelID != "" {
						executionLLM = fmt.Sprintf("%s/%s", config.LLMConfig.Primary.Provider, config.LLMConfig.Primary.ModelID)
					}
				}

				// CAPTURE TURN COUNT: Calculate total LLM turns from conversation history
				// Each turn consists of a user message and an assistant response (including tool calls)
				turnCount := len(executionConversationHistory)

				// Check for prerequisite failure (from tool call via channel)
				var prereqErr *PrerequisiteFailureError
				select {
				case prereqErr = <-prereqErrChan:
					// Prerequisite failure detected - tool called and context was canceled
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
					// Use step ID to find target step (more reliable than using computed index)
					retryReason := prereqErr.Reason
					targetStepID := prereqErr.DependsOnStepID
					currentStepID := step.GetID()

					// Find target step by ID in allSteps array
					targetStepIndex := -1
					if targetStepID != "" && allSteps != nil {
						for idx, s := range allSteps {
							if s.GetID() == targetStepID {
								targetStepIndex = idx
								break
							}
						}
					}

					// Validate target step was found
					if targetStepIndex < 0 {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Could not find step with ID %s in allSteps, ignoring navigation", targetStepID))
					} else if targetStepIndex >= stepIndex {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Target step index %d (ID: %s) is not before current step %d (ID: %s), ignoring navigation", targetStepIndex+1, targetStepID, stepIndex+1, currentStepID))
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
								return "", updatedContextFiles, fmt.Errorf("failed to cleanup progress for prerequisite navigation: %w", err)
							}

							// Emit prerequisite navigation event
							eventBridge := hcpo.GetContextAwareBridge()
							if eventBridge != nil {
								navigationEvent := &baseevents.PrerequisiteNavigationEvent{
									BaseEventData: baseevents.BaseEventData{
										Timestamp: time.Now(),
										Component: "orchestrator",
									},
									FromStepIndex: stepIndex,
									ToStepIndex:   targetStepIndex,
									FromStepID:    currentStepID,
									ToStepID:      targetStepID,
									Reason:        retryReason,
									FailureType:   "prerequisite",
								}
								eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
									Type:      baseevents.PrerequisiteNavigation,
									Timestamp: time.Now(),
									Data:      navigationEvent,
								})
								hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted prerequisite_navigation event: step %d (ID: %s) → step %d (ID: %s) (%s)", stepIndex+1, currentStepID, targetStepIndex+1, targetStepID, retryReason))
							}

							// Return navigation error to restart from target step
							// Wrap the PrerequisiteFailureError to preserve type information
							return "", updatedContextFiles, fmt.Errorf("prerequisite failure detected: %s (navigate to step %d, ID: %s): %w", retryReason, targetStepIndex+1, targetStepID, prereqErr)
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
					"model":            executionLLM, // Store the model used for execution
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
					}
				}

				// Check if LLM validation is enabled for this step (disabled by default)
				agentConfigs = getAgentConfigs(step)
				// LLM validation disabled by default (nil = disabled). Only enabled when explicitly set to false.
				enableLLMValidation := agentConfigs != nil && agentConfigs.DisableValidation != nil && !*agentConfigs.DisableValidation
				if !enableLLMValidation {
					hcpo.GetLogger().Info(fmt.Sprintf("⏭️ LLM validation disabled for step %d - auto-approving (pre-validation and learning will still run)", stepIndex+1))
					// Auto-approve: create a success validation response
					// NOTE: LLM validation being disabled does NOT prevent pre-validation or learning from running
					validationResponse = &ValidationResponse{
						IsSuccessCriteriaMet: true,
						ExecutionStatus:      "COMPLETED",
						Reasoning:            "LLM validation disabled - step auto-approved",
					}
					if hasLoop(step) {
						// For loop steps, mark condition as met when LLM validation is disabled
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
					// Get step ID for validation agent
					validationStepID := step.GetID()
					validationAgent, err := hcpo.createValidationAgent(ctx, "validation", stepIndex+1, validationStepID, validationAgentName, agentConfigs)
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
						"StepTitle":           step.GetTitle(),
						"StepDescription":     step.GetDescription(),
						"StepSuccessCriteria": step.GetSuccessCriteria(),
						"StepContextOutput":   step.GetContextOutput().String(),
						"WorkspacePath":       validationWorkspacePath,
						"ExecutionHistory":    shared.FormatConversationHistory(executionConversationHistory),
					}

					// Add context dependencies as a comma-separated string
					contextDeps := step.GetContextDependencies()
					if len(contextDeps) > 0 {
						validationTemplateVars["StepContextDependencies"] = strings.Join(contextDeps, ", ")
					} else {
						validationTemplateVars["StepContextDependencies"] = ""
					}

					// If in loop mode, pass loop condition to validation agent
					if hasLoop(step) {
						_, loopCondition, _, _ := getLoopFields(step)
						validationTemplateVars["LoopCondition"] = loopCondition
						hcpo.GetLogger().Info(fmt.Sprintf("🔍 Checking loop condition for step %d (iteration %d): %s", stepIndex+1, loopIterationCount, loopCondition))
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
					} else {
						validationTemplateVars["DecisionReasoning"] = ""
					}

					// Prerequisite detection is handled by execution agent tool (detect_prerequisite_failure)
					// No need to pass prerequisite info to validation agent

					// Run pre-validation (code-based structural checks)
					// Pass validation schema directly from step (no need to read plan.json)
					// Use stepExecutionPath (step's execution folder) instead of validationWorkspacePath (run folder)
					// Files to validate are in the step's execution folder, not the run folder root
					validationSchema := getValidationSchema(step)
					workspaceResults, err := RunPreValidation(ctx, validationSchema, stepExecutionPath, hcpo.BaseOrchestrator)
					if err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Pre-validation error for step %d: %v - blocking LLM validation", stepIndex+1, err))
						// Pre-validation error means we can't verify structure - block LLM validation
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
					} else if validationSchema == nil {
						// Log when pre-validation is skipped (schema is nil)
						hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Pre-validation skipped for step %d (no validation schema provided)", stepIndex+1))
					}

					// Format pre-validation results and add to template variables
					validationTemplateVars["WorkspaceVerificationResults"] = formatWorkspaceResults(workspaceResults)

					// Emit pre-validation completed event
					hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, stepPath, isBranchStep, workspaceResults)

					// If pre-validation failed, reject immediately without calling LLM validation
					if !workspaceResults.OverallPass {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Pre-validation failed for step %d - rejecting immediately without LLM validation", stepIndex+1))
						// Create a failed validation response immediately
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: false,
							ExecutionStatus:      "FAILED",
							Reasoning:            formatWorkspaceResults(workspaceResults) + "\n\nPre-validation failed - structural issues must be fixed before execution can be validated.",
							Feedback: []ValidationFeedback{
								{
									Type:        "structural_validation",
									Description: "Pre-validation failed - output structure does not meet requirements",
									Severity:    "HIGH",
								},
							},
						}
						if hasLoop(step) {
							validationResponse.LoopConditionMet = false
							validationResponse.LoopReasoning = "Loop condition cannot be evaluated due to pre-validation failure"
						}
					} else {
						// Pre-validation passed - check if we should skip LLM validation
						agentConfigs := getAgentConfigs(step)

						// Determine validation mode (default: "skip")
						validationMode := "skip"
						if agentConfigs != nil {
							if agentConfigs.LLMValidationMode != "" {
								validationMode = agentConfigs.LLMValidationMode
							}
						}

						shouldSkipLLMValidation := false
						skipReason := ""

						if validationMode == "skip" {
							shouldSkipLLMValidation = true
							skipReason = "configured to skip when pre-validation passes"
						} else if validationMode == "auto" {
							// Check if we have enough successful runs to trust pre-validation only
							learningPathIdentifier := getLearningPathIdentifier(step.GetID(), stepPath)
							metadata, err := hcpo.GetLearningMetadata(ctx, learningPathIdentifier)
							if err == nil && metadata != nil {
								totalSuccess := metadata.SuccessfulRunsSimple + metadata.SuccessfulRunsMedium + metadata.SuccessfulRunsComplex
								if totalSuccess >= 3 {
									shouldSkipLLMValidation = true
									skipReason = fmt.Sprintf("auto-skipped after %d successful runs (threshold: 3)", totalSuccess)
								} else {
									hcpo.GetLogger().Info(fmt.Sprintf("🔍 Step %d auto-validation: %d/3 successful runs - running LLM validation", stepIndex+1, totalSuccess))
								}
							} else {
								// If metadata fails or doesn't exist, default to running validation (safest)
								hcpo.GetLogger().Info(fmt.Sprintf("🔍 Step %d auto-validation: No metadata found - running LLM validation", stepIndex+1))
							}
						}
						// "always" mode falls through (shouldSkipLLMValidation = false)

						if shouldSkipLLMValidation {
							// Skip LLM validation and assume validation success
							hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d pre-validation passed - skipping LLM validation (%s)", stepIndex+1, skipReason))
							validationResponse = &ValidationResponse{
								IsSuccessCriteriaMet: true,
								ExecutionStatus:      "COMPLETED",
								Reasoning:            formatWorkspaceResults(workspaceResults) + fmt.Sprintf("\n\nPre-validation passed - LLM validation skipped (%s).", skipReason),
								Feedback:             []ValidationFeedback{},
							}
							if hasLoop(step) {
								validationResponse.LoopConditionMet = true
								validationResponse.LoopReasoning = fmt.Sprintf("Loop condition met (pre-validation passed, LLM validation skipped: %s)", skipReason)
							}
						} else {
							// Pre-validation passed - proceed to LLM validation
							// Check for context cancellation before validation
							select {
							case <-ctx.Done():
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled before validation for step %d", stepIndex+1))
								return "", updatedContextFiles, fmt.Errorf("step execution canceled: %w", ctx.Err())
							default:
							}

							// Validate this step's execution using structured output
							validationResponse, _, err = validationAgent.(*WorkflowValidationAgent).ExecuteStructured(ctx, validationTemplateVars, []llmtypes.MessageContent{})
							if err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step %d validation failed (attempt %d): %v", stepIndex+1, retryAttempt, err))
								if retryAttempt >= maxRetryAttempts {
									break // Exit retry loop - will proceed to human feedback with nil validationResponse
								}
								continue // Retry on next attempt
							}
						}
					}

					hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d validation completed successfully (attempt %d)", stepIndex+1, retryAttempt))

					// Store validation response to workspace (if enabled)
					if validationResponse != nil && hcpo.saveValidationResponses {
						// Increment validation counter for numbered files
						validationCounter++
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

						// Run success learning when loop completes successfully (before breaking)
						// FAST MODE & LEARNING DISABLED: Skip learning agents entirely
						isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
						// Check step-specific learning detail level
						agentConfigs := getAgentConfigs(step)
						isLearningDisabledStep := agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning
						isLearningDetailLevelNone := false
						if agentConfigs != nil && agentConfigs.LearningDetailLevel == "none" {
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
						if !isFastExecuteStep && !isLearningDisabled && !shouldSkipLearningDueToTempOverride {
							// Success Learning Agent - analyze what worked well and update plan.json
							// Loop condition met means step completed successfully
							learningPathIdentifier := getLearningPathIdentifier(step.GetID(), stepPath)
							hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running success learning analysis for %s (loop completed)", stepPath))
							// Populate runtime fields for runSuccessLearningPhase
							stepConfigs, err := hcpo.ReadStepConfigs(ctx)
							if err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_config.json: %v (using defaults)", err))
								stepConfigs = []StepConfig{}
							}
							// Populate runtime fields before learning
							if err := populateRuntimeFields(step, stepConfigs); err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to populate runtime fields for learning: %v", err))
							}
							err = hcpo.runSuccessLearningPhase(ctx, stepIndex, stepPath, learningPathIdentifier, totalSteps, step, executionConversationHistory, validationResponse, isCodeExecutionMode, usedTempLLM, turnCount, executionLLM)
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
								hcpo.GetLogger().Info(fmt.Sprintf("🔧 %s was used and skip learning flag enabled: Skipping learning agents for step %d (loop completed)", usedTempLLM, stepIndex+1))

								// IMPORTANT: Update metadata even when skipping learning due to tempLLM
								// We still want to count this success toward the auto-lock threshold (3 successes)
								learningPathIdentifier := getLearningPathIdentifier(step.GetID(), stepPath)
								agentConfigs := getAgentConfigs(step)
								learningLLMConfig := hcpo.selectLearningLLM(ctx, agentConfigs, step.GetID(), stepPath)
								if learningLLMConfig == nil {
									err := fmt.Errorf("no valid LLM configuration found for learning agent")
									hcpo.GetLogger().Error("❌ No valid LLM configuration found for learning agent, skipping metadata update", err)
									continue
								}
								learningLLM := fmt.Sprintf("%s/%s", learningLLMConfig.Primary.Provider, learningLLMConfig.Primary.ModelID)

								_, metadataErr := hcpo.updateLearningMetadataWithTurnCount(
									ctx,
									stepIndex,
									stepPath,
									learningPathIdentifier,
									false, // hasNewLearning = false (learning was skipped)
									fmt.Sprintf("Success learning skipped (loop completed): %s was used and skip flag enabled. Metadata updated.", usedTempLLM),
									0.0, // confidence = 0 (not applicable when skipped)
									turnCount,
									step,
									true, // validationPassed = true (execution succeeded)
									executionLLM,
									learningLLM,
								)
								if metadataErr != nil {
									hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update learning metadata (tempLLM skip, loop) for %s: %v", learningPathIdentifier, metadataErr))
								} else {
									hcpo.GetLogger().Info(fmt.Sprintf("📊 Updated metadata for %s (loop success learning skipped due to %s, turnCount: %d)", learningPathIdentifier, usedTempLLM, turnCount))
								}

								// Emit learning skipped event
								eventBridge := hcpo.GetContextAwareBridge()
								if eventBridge != nil {
									stepTitle := step.GetTitle()
									if stepTitle == "" {
										stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
									}
									stepId := step.GetID()
									if stepId == "" {
										stepId = fmt.Sprintf("step-%d", stepIndex+1)
									}
									learningSkippedEvent := &events.LearningSkippedEvent{
										BaseEventData: baseevents.BaseEventData{
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
									eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
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
						_, _, maxIterations, _ := getLoopFields(step)
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step %d loop condition not met yet (iteration %d/%d), continuing loop", stepIndex+1, loopIterationCount, maxIterations))

						// Preserve validation response for next loop iteration (for fallback LLM detection)
						// If validation failed (success criteria not met) in this iteration, next iteration will use original LLM
						if isValidationFailure(validationResponse) {
							// Increment validation failure count for UI display
							if err := hcpo.IncrementValidationFailureCount(ctx, stepPath); err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to increment validation failure count for %s: %v", stepPath, err))
							}

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
									_, _, maxIterations, _ := getLoopFields(step)
									hcpo.GetLogger().Info(fmt.Sprintf("✅ User approved next loop iteration for step %d (iteration %d/%d) - no specific feedback provided", stepIndex+1, loopIterationCount, maxIterations))
									humanFeedback = ""
								} else {
									// User provided feedback - store it for next loop iteration
									humanFeedback = feedback
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
							} else {
								// Clear any previous human feedback if none provided
								templateVars["HumanFeedback"] = ""
							}
						}

						// Check if learning should run after each loop iteration
						// SMART DEFAULT: Run learning for first 2 iterations only (to learn patterns)
						// After 2 iterations, stop learning to save costs
						learningAfterLoopIteration := false
						agentConfigs := getAgentConfigs(step)

						// Check if explicitly configured in step config
						configuredExplicitly := agentConfigs != nil && agentConfigs.LearningAfterLoopIteration

						if configuredExplicitly {
							// User explicitly enabled it - respect their choice
							learningAfterLoopIteration = true
							hcpo.GetLogger().Info(fmt.Sprintf("📝 Loop learning explicitly enabled via config for step %d (iteration %d)", stepIndex+1, loopIterationCount))
						} else {
							// Smart default: only run for first 2 iterations
							if loopIterationCount <= 2 {
								learningAfterLoopIteration = true
								hcpo.GetLogger().Info(fmt.Sprintf("📝 Running loop learning for iteration %d/%d (auto-enabled for first 2 iterations)", loopIterationCount, 2))
							} else {
								learningAfterLoopIteration = false
								hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping loop learning for iteration %d (only first 2 iterations learn by default)", loopIterationCount))
							}
						}

						if learningAfterLoopIteration {
							// Run learning after this loop iteration
							isFastExecuteStep := execCtx.FastExecuteMode && stepIndex <= execCtx.FastExecuteEndStep
							// Check step-specific learning detail level
							agentConfigs := getAgentConfigs(step)
							isLearningDisabledStep := agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning
							isLearningDetailLevelNone := false
							if agentConfigs != nil && agentConfigs.LearningDetailLevel == "none" {
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
							isLearningsLocked := agentConfigs != nil && agentConfigs.LockLearnings != nil && *agentConfigs.LockLearnings
							shouldSkipLearningDueToLock := false
							if isLearningsLocked {
								// Check if learnings folder exists and has content
								learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, step.GetID(), stepIndex, stepPath)
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
								learningPathIdentifier := getLearningPathIdentifier(step.GetID(), stepPath)
								hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running learning analysis after loop iteration %d for %s", loopIterationCount, stepPath))
								// Run learning even though condition not met (for iteration analysis)
								// Populate runtime fields for runSuccessLearningPhase
								stepConfigs, err := hcpo.ReadStepConfigs(ctx)
								if err != nil {
									hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_config.json: %v (using defaults)", err))
									stepConfigs = []StepConfig{}
								}
								todoStep, err := populateStepRuntimeFields(step, stepConfigs)
								if err != nil {
									hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to populate runtime fields for learning: %v", err))
								} else {
									// For loop iterations, usedTempLLM is in scope but typically empty (loop iterations use original LLM)
									err = hcpo.runSuccessLearningPhase(ctx, stepIndex, stepPath, learningPathIdentifier, totalSteps, todoStep, executionConversationHistory, validationResponse, isCodeExecutionMode, usedTempLLM, turnCount, executionLLM)
								}
								if err != nil {
									hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Learning phase failed after loop iteration %d for %s: %v", loopIterationCount, stepPath, err))
								} else {
									hcpo.GetLogger().Info(fmt.Sprintf("✅ Learning analysis completed after loop iteration %d for step %d", loopIterationCount, stepIndex+1))
								}
							} else if shouldSkipLearningDueToTempOverride {
								// IMPORTANT: Update metadata even when skipping learning due to tempLLM
								// We still want to count this success toward the auto-lock threshold (3 successes)
								learningPathIdentifier := getLearningPathIdentifier(step.GetID(), stepPath)
								agentConfigs := getAgentConfigs(step)
								learningLLMConfig := hcpo.selectLearningLLM(ctx, agentConfigs, step.GetID(), stepPath)
								if learningLLMConfig == nil {
									err := fmt.Errorf("no valid LLM configuration found for learning agent")
									hcpo.GetLogger().Error("❌ No valid LLM configuration found for learning agent, skipping metadata update", err)
									continue
								}
								learningLLM := fmt.Sprintf("%s/%s", learningLLMConfig.Primary.Provider, learningLLMConfig.Primary.ModelID)

								_, metadataErr := hcpo.updateLearningMetadataWithTurnCount(

									ctx,

									stepIndex,

									stepPath,

									learningPathIdentifier,

									false, // hasNewLearning = false (learning was skipped)

									fmt.Sprintf("Success learning skipped (loop iteration %d): %s was used and skip flag enabled. Metadata updated.", loopIterationCount, usedTempLLM),

									0.0, // confidence = 0 (not applicable when skipped)

									turnCount,

									step,

									true, // validationPassed = true (execution succeeded)

									executionLLM,

									learningLLM,
								)

								if metadataErr != nil {
									hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update learning metadata (tempLLM skip, loop iter) for %s: %v", learningPathIdentifier, metadataErr))
								} else {
									hcpo.GetLogger().Info(fmt.Sprintf("📊 Updated metadata for %s (loop iteration %d learning skipped due to %s, turnCount: %d)", learningPathIdentifier, loopIterationCount, usedTempLLM, turnCount))
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
				agentConfigs = getAgentConfigs(step)
				isLearningDisabledStep := agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning
				isLearningDetailLevelNone := false
				if agentConfigs != nil && agentConfigs.LearningDetailLevel == "none" {
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
				isLearningsLocked := agentConfigs != nil && agentConfigs.LockLearnings != nil && *agentConfigs.LockLearnings
				shouldSkipLearningDueToLock := false
				if isLearningsLocked {
					// Check if learnings folder exists and has content
					learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, step.GetID(), stepIndex, stepPath)
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
				if isFastExecuteStep || isLearningDisabled || shouldSkipLearningDueToLock || shouldSkipLearningDueToTempOverride {
					if isFastExecuteStep {
						hcpo.GetLogger().Info(fmt.Sprintf("⚡ Fast mode: Skipping learning agents for step %d", stepIndex+1))
					} else if isLearningDisabled {
						hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Learning disabled: Skipping learning agents for step %d", stepIndex+1))
					} else if shouldSkipLearningDueToLock {
						hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping learning agents for step %d (using existing learnings)", stepIndex+1))
					} else if shouldSkipLearningDueToTempOverride {
						hcpo.GetLogger().Info(fmt.Sprintf("🔧 %s was used and skip learning flag enabled: Skipping learning agents for step %d", usedTempLLM, stepIndex+1))

						// IMPORTANT: Update metadata even when skipping learning due to tempLLM
						// We still want to count this success toward the auto-lock threshold (3 successes)
						// This ensures the step can progress and eventually lock/optimize
						learningPathIdentifier := getLearningPathIdentifier(step.GetID(), stepPath)
						agentConfigs := getAgentConfigs(step)
						learningLLMConfig := hcpo.selectLearningLLM(ctx, agentConfigs, step.GetID(), stepPath)
						learningLLM := fmt.Sprintf("%s/%s", learningLLMConfig.Primary.Provider, learningLLMConfig.Primary.ModelID)

						_, metadataErr := hcpo.updateLearningMetadataWithTurnCount(
							ctx,
							stepIndex,
							stepPath,
							learningPathIdentifier,
							false, // hasNewLearning = false (learning was skipped)
							fmt.Sprintf("Success learning skipped: %s was used and skip flag enabled. Metadata updated to track success.", usedTempLLM),
							0.0, // confidence = 0 (not applicable when skipped)
							turnCount,
							step,
							true, // validationPassed = true (execution succeeded)
							executionLLM,
							learningLLM,
						)
						if metadataErr != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update learning metadata (tempLLM skip) for %s: %v", learningPathIdentifier, metadataErr))
						} else {
							hcpo.GetLogger().Info(fmt.Sprintf("📊 Updated metadata for %s (success learning skipped due to %s, turnCount: %d)", learningPathIdentifier, usedTempLLM, turnCount))
						}

						// Emit learning skipped event
						eventBridge := hcpo.GetContextAwareBridge()
						if eventBridge != nil {
							stepTitle := step.GetTitle()
							if stepTitle == "" {
								stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
							}
							stepId := step.GetID()
							if stepId == "" {
								stepId = fmt.Sprintf("step-%d", stepIndex+1)
							}
							learningSkippedEvent := &events.LearningSkippedEvent{
								BaseEventData: baseevents.BaseEventData{
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
							eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
								Type:      events.LearningSkipped,
								Timestamp: time.Now(),
								Data:      learningSkippedEvent,
							})
							hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted learning_skipped event for step %d: %s (temp override: %s/%s)", stepIndex+1, stepTitle, hcpo.tempOverrideLLM.Provider, hcpo.tempOverrideLLM.ModelID))
						}
					}
				} else {
					// Ensure validationResponse exists - if validation is disabled, assume success
					agentConfigs := getAgentConfigs(step)
					// LLM validation disabled by default (nil = disabled). Only enabled when explicitly set to false.
					enableLLMValidation := agentConfigs != nil && agentConfigs.DisableValidation != nil && !*agentConfigs.DisableValidation
					if validationResponse == nil && !enableLLMValidation {
						// LLM validation is disabled but response is nil - create success response for learning
						hcpo.GetLogger().Info(fmt.Sprintf("⏭️ LLM validation disabled for step %d - creating success response for learning", stepIndex+1))
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: true,
							ExecutionStatus:      "COMPLETED",
							Reasoning:            "LLM validation disabled - step auto-approved for learning",
						}
					}

					// Run appropriate learning phase based on validation result
					// If validation is disabled, we assume IsSuccessCriteriaMet = true
					if validationResponse != nil && validationResponse.IsSuccessCriteriaMet {
						// Success Learning Agent - analyze what worked well and update plan.json
						learningPathIdentifier := getLearningPathIdentifier(step.GetID(), stepPath)
						hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running success learning analysis for %s", stepPath))
						// Populate runtime fields for runSuccessLearningPhase
						stepConfigs, err := hcpo.ReadStepConfigs(ctx)
						if err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_config.json: %v (using defaults)", err))
							stepConfigs = []StepConfig{}
						}
						todoStep, err := populateStepRuntimeFields(step, stepConfigs)
						if err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to populate runtime fields for learning: %v", err))
						} else {
							// usedTempLLM is set in the retry loop above when validation passes
							err = hcpo.runSuccessLearningPhase(ctx, stepIndex, stepPath, learningPathIdentifier, totalSteps, todoStep, executionConversationHistory, validationResponse, isCodeExecutionMode, usedTempLLM, turnCount, executionLLM)
						}
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
						} else if failureLearningAttempts >= 2 {
							hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Failure learning attempts limit reached (%d >= 2) - skipping failure learning analysis for %s", failureLearningAttempts, stepPath))
						} else {
							failureLearningAttempts++
							var refinedTaskDescription string
							learningPathIdentifier := getLearningPathIdentifier(step.GetID(), stepPath)
							hcpo.GetLogger().Info(fmt.Sprintf("🧠 Running failure learning analysis for %s (attempt %d/2)", stepPath, failureLearningAttempts))
							// Populate runtime fields for runFailureLearningPhase
							stepConfigs, err := hcpo.ReadStepConfigs(ctx)
							if err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_config.json: %v (using defaults)", err))
								stepConfigs = []StepConfig{}
							}
							// Populate runtime fields before learning
							var learningErr error
							if err := populateRuntimeFields(step, stepConfigs); err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to populate runtime fields for learning: %v", err))
								refinedTaskDescription = ""
							} else {
								refinedTaskDescription, _, learningErr = hcpo.runFailureLearningPhase(ctx, stepIndex, stepPath, learningPathIdentifier, totalSteps, step, executionConversationHistory, validationResponse, isCodeExecutionMode, usedTempLLM, turnCount, executionLLM)
							}
							if learningErr != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failure learning phase failed for %s: %v", stepPath, learningErr))
							} else {
								hcpo.GetLogger().Info(fmt.Sprintf("✅ Failure learning analysis completed for %s", stepPath))

								// Update step description for retry
								if refinedTaskDescription != "" {
									// Update description on RegularPlanStep if possible
									if regularStep := getRegularPlanStep(step); regularStep != nil {
										regularStep.Description = refinedTaskDescription
									}
									templateVars["StepDescription"] = refinedTaskDescription
									hcpo.GetLogger().Info(fmt.Sprintf("🔄 Updated step %d description with refined task for retry", stepIndex+1))
								}

								// Re-read learnings after failure learning updates them (if we're going to retry)
								// This ensures the next retry attempt uses the updated learnings from failure analysis
								// BUT: Skip re-reading if learning is disabled
								if retryAttempt < maxRetryAttempts {
									// Check if learning is disabled before re-reading
									agentConfigs := getAgentConfigs(step)
									isLearningDisabledStep := agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning
									isLearningDetailLevelNone := false
									if agentConfigs != nil && agentConfigs.LearningDetailLevel == "none" {
										isLearningDetailLevelNone = true
									}
									isLearningDisabled := isLearningDisabledStep || isLearningDetailLevelNone

									if isLearningDisabled {
										hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Learning disabled - skipping re-read after failure learning (for retry attempt %d)", retryAttempt+1))
									} else {
										hcpo.GetLogger().Info(fmt.Sprintf("📚 Re-reading learnings after failure learning update (for retry attempt %d)", retryAttempt+1))
										// Force re-read by temporarily disabling cache check for non-loop steps
										// For loop steps, respect the LearningAfterLoopIteration setting
										if !hasLoop(step) {
											// For regular steps, always re-read after failure learning
											updatedLearningHistory, readErr := hcpo.readLearningHistory(
												ctx,
												stepIndex,
												step.GetID(),
												stepPath,
											)
											if readErr != nil {
												hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to re-read learnings after failure learning: %v - will use previous learnings", readErr))
											} else {
												formattedLearningHistory = updatedLearningHistory
												templateVars["LearningHistory"] = formattedLearningHistory                       // Update template vars for next retry
												templateVars["HasLearnings"] = fmt.Sprintf("%t", formattedLearningHistory != "") // Update HasLearnings flag
												hcpo.GetLogger().Info(fmt.Sprintf("✅ Re-read learnings after failure learning update (length: %d chars)", len(formattedLearningHistory)))
											}
										} else {
											// For loop steps, only re-read if LearningAfterLoopIteration is true
											// Default to true for loop steps
											learningAfterLoopIteration := hasLoop(step) // Always true for loop steps
											if learningAfterLoopIteration {
												updatedLearningHistory, readErr := hcpo.readLearningHistory(
													ctx,
													stepIndex,
													step.GetID(),
													stepPath,
												)
												if readErr != nil {
													hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to re-read learnings after failure learning: %v - will use previous learnings", readErr))
												} else {
													formattedLearningHistory = updatedLearningHistory
													templateVars["LearningHistory"] = formattedLearningHistory                       // Update template vars for next retry
													templateVars["HasLearnings"] = fmt.Sprintf("%t", formattedLearningHistory != "") // Update HasLearnings flag
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
				}

				// Check if success criteria was met (only for non-loop steps or when loop handling is done)
				if !hasLoop(step) {
					// Check IsSuccessCriteriaMet instead of just ExecutionStatus - PARTIAL/INCOMPLETE can also mean criteria not met
					if validationResponse != nil && validationResponse.IsSuccessCriteriaMet {
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d passed validation - success criteria met (Status: %s)", stepIndex+1, validationResponse.ExecutionStatus))

						// Clear human feedback since validation succeeded
						templateVars["HumanFeedback"] = ""
						break // Exit retry loop and continue to next step
					} else {
						statusStr := "unknown"
						if validationResponse != nil {
							statusStr = validationResponse.ExecutionStatus
						}
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step %d failed validation - success criteria not met (Status: %s, attempt %d/%d)", stepIndex+1, statusStr, retryAttempt, maxRetryAttempts))

						// Increment validation failure count for UI display
						if err := hcpo.IncrementValidationFailureCount(ctx, stepPath); err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to increment validation failure count for %s: %v", stepPath, err))
						}

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
							// Explicitly continue to next retry attempt
							continue
						}
					}
				}
			} // End of retry loop

			// Exit immediately if validation failed after exhausting all retry attempts
			if validationFailedAfterMaxRetries && !hasLoop(step) {
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
				err := fmt.Errorf("step %d failed validation after %d retry attempts. %s. Please review the execution results and update the plan if needed", stepIndex+1, maxRetryAttempts, validationDetails)
				// Emit step_failed event using centralized method
				stepTitle := step.GetTitle()
				if stepTitle == "" {
					stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
				}
				stepId := step.GetID()
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
			if !hasLoop(step) {
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
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Human feedback request failed: %v", err))
				// Default to continue if feedback fails
				approved = true
			}
		}

		// Store human feedback for future steps (even if approved, user might have provided guidance)
		// Note: humanFeedbackHistory is not available in this function scope, so we skip storing it
		// It will be handled by the caller if needed

		if approved {
			// User approved - mark step as completed and exit outer loop
			// Only update progress if this is not a branch step
			if !isBranchStep {
				hcpo.addCompletedStepIndex(progress, stepIndex)
				// Always save progress after marking a step as completed (both fast and normal mode)
				if err := hcpo.saveStepProgress(ctx, progress); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save step progress: %v", err))
				} else {
					modeStr := "fast mode"
					if !isFastExecuteStep {
						modeStr = "normal mode"
					}
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d/%d marked as completed and saved (%s) - Total completed: %d/%d", stepIndex+1, totalSteps, modeStr, len(progress.CompletedStepIndices), progress.TotalSteps))
				}

				// Emit step token usage summary
				stepTitle := step.GetTitle()
				if stepTitle == "" {
					stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
				}
				stepID := step.GetID()
				if stepID == "" {
					stepID = fmt.Sprintf("step-%d", stepIndex+1)
				}
				hcpo.EmitStepTokenUsage(ctx, "execution", stepIndex, stepID, stepTitle, false) // Don't clear - keep for potential future queries
				// Note: Token usage is now persisted in real-time on each token_usage event, not just at step completion
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Branch step %d completed (not updating main progress)", stepIndex+1))
			}
			stepCompleted = true
		} else {
			// User provided feedback (didn't approve) - stop workflow and ask user to manually update plan
			hcpo.GetLogger().Info(fmt.Sprintf("🛑 User provided feedback - stopping workflow. Feedback: %s", feedback))
			planPath := fmt.Sprintf("%s/planning/plan.json", hcpo.GetWorkspacePath())
			return executionResult, updatedContextFiles, fmt.Errorf("workflow stopped: user feedback received. please manually update the plan at %s with the following feedback, then restart the workflow: %s", planPath, feedback)
		}
	} // End of outer loop for step execution

	// Append step's context output to context files if it exists
	contextOutput := step.GetContextOutput().String()
	if contextOutput != "" {
		updatedContextFiles = append(updatedContextFiles, contextOutput)
	}

	// Emit step_finished event (also emits step progress with status="end")
	// Note: Conditional steps emit their own step_finished event in executeConditionalStep after branch execution completes
	hcpo.emitStepFinishedEvent(ctx, step, stepIndex, stepPath, isBranchStep)

	return executionResult, updatedContextFiles, nil
}

// ============================================================================
// STEP TYPE DETECTION HELPERS (for PlanStepInterface)
// ============================================================================
// These helper functions provide a cleaner way to detect step types from PlanStepInterface
// boolean flags, making the execution routing logic more maintainable and
// preparing for future migration to type-safe step types.

// isConditionalStep returns true if the step is a conditional step (has conditional branches)
func isConditionalStep(step PlanStepInterface) bool {
	_, ok := step.(*ConditionalPlanStep)
	return ok
}

// isDecisionStep returns true if the step is a decision step (executes inner step and routes based on evaluation)
func isDecisionStep(step PlanStepInterface) bool {
	_, ok := step.(*DecisionPlanStep)
	return ok
}

// isOrchestrationStep returns true if the step is an orchestration step (orchestrator with multiple sub-agents)
func isOrchestrationStep(step PlanStepInterface) bool {
	_, ok := step.(*OrchestrationPlanStep)
	return ok
}

// isHumanInputStep returns true if the step is a human input step (asks question and blocks for input)
func isHumanInputStep(step PlanStepInterface) bool {
	_, ok := step.(*HumanInputPlanStep)
	return ok
}

// hasLoop returns true if the step has loop mode enabled
func hasLoop(step PlanStepInterface) bool {
	switch s := step.(type) {
	case *RegularPlanStep:
		return s.HasLoop
	default:
		return false
	}
}

// getAgentConfigs returns AgentConfigs from a PlanStepInterface
func getAgentConfigs(step PlanStepInterface) *AgentConfigs {
	switch s := step.(type) {
	case *RegularPlanStep:
		return s.AgentConfigs
	case *ConditionalPlanStep:
		return s.AgentConfigs
	case *DecisionPlanStep:
		return s.AgentConfigs
	case *OrchestrationPlanStep:
		return s.AgentConfigs
	case *EvaluationStep:
		return s.AgentConfigs
	default:
		return nil
	}
}

// getValidationSchema returns ValidationSchema from a PlanStepInterface
func getValidationSchema(step PlanStepInterface) *ValidationSchema {
	return step.GetValidationSchema()
}

// getLoopFields returns loop-related fields from a RegularPlanStep, or default values
func getLoopFields(step PlanStepInterface) (hasLoop bool, loopCondition string, maxIterations int, loopDescription string) {
	switch s := step.(type) {
	case *RegularPlanStep:
		return s.HasLoop, s.LoopCondition, s.MaxIterations, s.LoopDescription
	case *EvaluationStep:
		return false, "", 0, ""
	default:
		return false, "", 0, ""
	}
}

// getRegularPlanStep returns a pointer to RegularPlanStep if the step is a regular step, nil otherwise
// This allows modification of step fields
func getRegularPlanStep(step PlanStepInterface) *RegularPlanStep {
	if regularStep, ok := step.(*RegularPlanStep); ok {
		return regularStep
	}
	return nil
}

// runExecutionPhase executes the plan steps one by one
func (hcpo *StepBasedWorkflowOrchestrator) runExecutionPhase(
	ctx context.Context,
	breakdownSteps []PlanStepInterface,
	iteration int,
	progress *StepProgress,
	startFromStep int,
	execCtx *ExecutionContext,
) error {
	// Run folder should already be resolved early (after plan approval)
	if hcpo.selectedRunFolder == "" {
		return fmt.Errorf(fmt.Sprintf("run folder not resolved - this should have been set after plan approval"), nil)
	}

	// Track execution results in memory (instead of reading from files)
	// This allows conditional steps to use execution results directly
	previousExecutionResults := make([]string, 0)

	// If starting from a step > 0 or running a single step, load execution results from logs for previous steps
	// This ensures we have execution results available for buildPreviousStepsSummary
	// Single step mode: if target step > 0, we need previous steps' results
	// Resume mode: if startFromStep > 0, we need previous steps' results
	stepsToLoad := startFromStep
	if execCtx.RunSingleStepOnly && execCtx.SingleStepTarget > 0 {
		// Use the higher of the two (in case both are set)
		if execCtx.SingleStepTarget > stepsToLoad {
			stepsToLoad = execCtx.SingleStepTarget
		}
	}
	if stepsToLoad > 0 {
		previousExecutionResults = hcpo.loadExecutionResultsFromLogs(ctx, breakdownSteps, stepsToLoad)
	}

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
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Workflow execution canceled before step %d/%d: %s", i+1, len(breakdownSteps), step.GetTitle()))
			return fmt.Errorf("workflow execution canceled: %w", ctx.Err())
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
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress during fast→normal transition: %v", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved progress during fast→normal mode transition: %d/%d steps completed", len(progress.CompletedStepIndices), progress.TotalSteps))
			}
		}

		// Skip if step is already completed
		if i < startFromStep {
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping step %d/%d (already completed): %s", i+1, len(breakdownSteps), step.GetTitle()))
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
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping step %d/%d (marked as completed): %s", i+1, len(breakdownSteps), step.GetTitle()))
			continue
		}

		// Build context files from previous steps
		previousContextFiles := make([]string, 0)
		for prevIdx := 0; prevIdx < i; prevIdx++ {
			if prevIdx < len(breakdownSteps) {
				contextOutput := breakdownSteps[prevIdx].GetContextOutput().String()
				if contextOutput != "" {
					// Resolve variables in context output (consistent with conditional steps)
					resolvedOutput := ResolveVariables(contextOutput, hcpo.variableValues)
					previousContextFiles = append(previousContextFiles, resolvedOutput)
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("⚠️ Step %d (%s) has no context_output - skipping", prevIdx+1, breakdownSteps[prevIdx].GetTitle()))
				}
			}
		}

		// Set current step ID on context-aware bridge so ALL events have step info in metadata
		stepID := step.GetID()
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", i+1) // Fallback to step index if no ID
		}
		if bridge := hcpo.GetContextAwareBridge(); bridge != nil {
			if stepBridge, ok := bridge.(interface {
				SetCurrentStepID(stepID string)
			}); ok {
				stepBridge.SetCurrentStepID(stepID)
			}
		}

		// Route execution based on step type using helper functions
		// Check if this is a conditional step
		if isConditionalStep(step) {
			// Execute conditional step - pass execution results directly (not file paths)
			hcpo.GetLogger().Info(fmt.Sprintf("🔀 Starting conditional step execution: %s", step.GetTitle()))
			if err := hcpo.executeConditionalStep(ctx, step, i, 0, progress, previousExecutionResults, iteration, execCtx, breakdownSteps); err != nil {
				// Check if this is a workflow termination signal
				if strings.Contains(err.Error(), "WORKFLOW_END") {
					hcpo.GetLogger().Info(fmt.Sprintf("🏁 Conditional step %d signaled workflow termination - ending workflow", i+1))
					// Mark step as completed and break to end workflow
					hcpo.addCompletedStepIndex(progress, i)
					if err := hcpo.saveStepProgress(ctx, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after conditional step termination: %v", err))
					}
					break // Break out of the execution loop to end workflow
				}
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Conditional step %d execution failed: %v", i+1, err), nil)
				// Emit error event using centralized method
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "conditional-step-execution", fmt.Sprintf("Execute conditional step: %s", step.GetTitle()), err.Error(), i, iteration)
				return fmt.Errorf("conditional step %d execution failed: %w", i+1, err)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Conditional step %d completed successfully: %s", i+1, step.GetTitle()))

			// Mark conditional step as completed (executeConditionalStep handles progress internally)
			hcpo.addCompletedStepIndex(progress, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after conditional step: %v", err))
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
					if conditionalStep, ok := step.(*ConditionalPlanStep); ok && conditionalStep.IfTrueNextStepID != "" {
						nextStepID = conditionalStep.IfTrueNextStepID
						hcpo.GetLogger().Info(fmt.Sprintf("🔗 True branch completed - using if_true_next_step_id: %s", nextStepID))
					} else if conditionalStep, ok := step.(*ConditionalPlanStep); ok && len(conditionalStep.IfTrueSteps) > 0 {
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
					if conditionalStep, ok := step.(*ConditionalPlanStep); ok && conditionalStep.IfFalseNextStepID != "" {
						nextStepID = conditionalStep.IfFalseNextStepID
						hcpo.GetLogger().Info(fmt.Sprintf("🔗 False branch completed - using if_false_next_step_id: %s", nextStepID))
					} else if conditionalStep, ok := step.(*ConditionalPlanStep); ok && len(conditionalStep.IfFalseSteps) > 0 {
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
					if s.GetID() == nextStepID {
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
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Starting decision step execution: %s", step.GetTitle()))
			decisionResult, executionResult, err := hcpo.executeDecisionStep(ctx, step, i, progress, previousContextFiles, iteration, execCtx, breakdownSteps)
			if err != nil {
				// Check if this is a workflow termination signal
				if strings.Contains(err.Error(), "WORKFLOW_END") {
					hcpo.GetLogger().Info(fmt.Sprintf("🏁 Decision step %d signaled workflow termination - ending workflow", i+1))
					// Mark step as completed and break to end workflow
					hcpo.addCompletedStepIndex(progress, i)
					if err := hcpo.saveStepProgress(ctx, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after decision step termination: %v", err))
					}
					break // Break out of the execution loop to end workflow
				}
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Decision step %d execution failed: %v", i+1, err), nil)
				// Emit error event using centralized method
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "decision-step-execution", fmt.Sprintf("Execute decision step: %s", step.GetTitle()), err.Error(), i, iteration)
				return fmt.Errorf("decision step %d execution failed: %w", i+1, err)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Decision step %d completed successfully: %s", i+1, step.GetTitle()))

			// Mark decision step as completed
			hcpo.addCompletedStepIndex(progress, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after decision step: %v", err))
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
			if decisionStep, ok := step.(*DecisionPlanStep); ok {
				if decisionResult {
					nextStepID = decisionStep.IfTrueNextStepID
					resultStr = "true"
					hcpo.GetLogger().Info(fmt.Sprintf("🔗 Decision step evaluated to TRUE - using if_true_next_step_id: %s", nextStepID))
				} else {
					nextStepID = decisionStep.IfFalseNextStepID
					resultStr = "false"
					hcpo.GetLogger().Info(fmt.Sprintf("🔗 Decision step evaluated to FALSE - using if_false_next_step_id: %s", nextStepID))
				}
			}

			// Track decision evaluations to prevent infinite loops
			// Initialize DecisionEvaluationCounts if nil
			if progress.DecisionEvaluationCounts == nil {
				progress.DecisionEvaluationCounts = make(DecisionEvaluationCount)
			}

			// Create key: stepID:result (e.g., "verify-minute-file:false")
			decisionKey := fmt.Sprintf("%s:%s", step.GetID(), resultStr)
			currentCount := progress.DecisionEvaluationCounts[decisionKey]
			newCount := currentCount + 1
			progress.DecisionEvaluationCounts[decisionKey] = newCount

			hcpo.GetLogger().Info(fmt.Sprintf("📊 Decision evaluation count for %s: %d", decisionKey, newCount))

			// Check if we've made this same decision more than 2 times (3rd time = error)
			if newCount > 2 {
				errorMsg := fmt.Sprintf("infinite loop detected: decision step '%s' (ID: %s) has evaluated to %s %d times. This indicates a workflow logic error that would cause an infinite loop. Please review the decision step configuration and routing logic.", step.GetTitle(), step.GetID(), resultStr, newCount)
				hcpo.GetLogger().Error(errorMsg, nil)
				// Emit error event
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "decision-step-loop-detection", fmt.Sprintf("Decision step: %s", step.GetTitle()), errorMsg, i, iteration)
				return fmt.Errorf("workflow error: %s", errorMsg)
			}

			// Save progress with updated decision count
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after decision evaluation: %v", err))
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
					if s.GetID() == nextStepID {
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
							DecisionStepIndex: i,
							DecisionStepTitle: step.GetTitle(),
							DecisionResult:    decisionResult,
							DecisionReasoning: func() string {
								if decisionStep, ok := step.(*DecisionPlanStep); ok && decisionStep.DecisionResponse != nil {
									return decisionStep.DecisionResponse.Reasoning
								}
								return ""
							}(),
							DecisionExecutionResult: executionResult,
						}
						hcpo.GetLogger().Info(fmt.Sprintf("💾 Stored decision context for step %d (from decision step %d: %s) - decision was FALSE", targetStepIndex+1, i+1, step.GetTitle()))
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
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Starting orchestration step execution: %s", step.GetTitle()))
			// Generate step path for regular orchestration step
			orchestrationStepPath := fmt.Sprintf("step-%d", i+1)
			successCriteriaMet, nextStepID, err := hcpo.executeOrchestrationStep(ctx, step, i, progress, previousContextFiles, previousExecutionResults, iteration, execCtx, breakdownSteps, orchestrationStepPath)
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Orchestration step %d execution failed: %v", i+1, err), nil)
				// Emit error event using centralized method
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "orchestration-step-execution", fmt.Sprintf("Execute orchestration step: %s", step.GetTitle()), err.Error(), i, iteration)
				return fmt.Errorf("orchestration step %d execution failed: %w", i+1, err)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Orchestration step %d completed successfully: %s (SuccessCriteriaMet: %t)", i+1, step.GetTitle(), successCriteriaMet))

			// Mark orchestration step as completed
			hcpo.addCompletedStepIndex(progress, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after orchestration step: %v", err))
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
					if s.GetID() == nextStepID {
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

		// Check if this is a human input step
		if isHumanInputStep(step) {
			// Execute human input step - asks question and blocks for input
			hcpo.GetLogger().Info(fmt.Sprintf("👤 Starting human input step execution: %s", step.GetTitle()))

			// Build context files from previous steps
			previousContextFiles := make([]string, 0)
			for prevIdx := 0; prevIdx < i; prevIdx++ {
				if prevIdx < len(breakdownSteps) {
					contextOutput := breakdownSteps[prevIdx].GetContextOutput().String()
					if contextOutput != "" {
						// Resolve variables in context output
						resolvedOutput := ResolveVariables(contextOutput, hcpo.variableValues)
						previousContextFiles = append(previousContextFiles, resolvedOutput)
					}
				}
			}

			_, err := hcpo.executeHumanInputStep(ctx, step, i, progress, previousContextFiles, execCtx, breakdownSteps)
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Human input step %d execution failed: %v", i+1, err), nil)
				// Emit error event using centralized method
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "human-input-step-execution", fmt.Sprintf("Execute human input step: %s", step.GetTitle()), err.Error(), i, iteration)
				return fmt.Errorf("human input step %d execution failed: %w", i+1, err)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Human input step %d completed successfully: %s", i+1, step.GetTitle()))

			// Track execution result in memory for use by subsequent steps
			// Extract the response from the saved JSON file to create an execution result summary
			// Get the context output path to read the saved response
			contextOutput := step.GetContextOutput().String()
			if contextOutput == "" {
				contextOutput = fmt.Sprintf("step-%d.json", i+1)
			}
			resolvedContextOutput := ResolveVariables(contextOutput, hcpo.variableValues)

			// Read the saved response file to get the actual response
			runWorkspacePath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
			executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
			stepPath := fmt.Sprintf("step-%d", i+1)
			stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, stepPath)
			responseFilePath := filepath.Join(stepExecutionPath, resolvedContextOutput)

			var executionResult string
			responseContent, err := hcpo.ReadWorkspaceFile(ctx, responseFilePath)
			if err == nil {
				// Parse JSON to extract response
				var responseData map[string]interface{}
				if err := json.Unmarshal([]byte(responseContent), &responseData); err == nil {
					if response, ok := responseData["response"].(string); ok {
						question, _ := responseData["question"].(string)
						executionResult = fmt.Sprintf("Human input response to '%s': %s", question, response)
					} else {
						executionResult = fmt.Sprintf("Human input step completed: %s", step.GetTitle())
					}
				} else {
					executionResult = fmt.Sprintf("Human input step completed: %s", step.GetTitle())
				}
			} else {
				// Fallback if file can't be read
				executionResult = fmt.Sprintf("Human input step completed: %s", step.GetTitle())
			}

			// Ensure slice is large enough (pad with empty strings if needed)
			for len(previousExecutionResults) <= i {
				previousExecutionResults = append(previousExecutionResults, "")
			}
			previousExecutionResults[i] = executionResult
			hcpo.GetLogger().Info(fmt.Sprintf("💾 Stored execution result for human input step %d (will be used by subsequent steps): %s", i+1, executionResult))

			// Check if we're in single step mode and should stop
			if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
				hcpo.GetLogger().Info(fmt.Sprintf("🎯 Single step mode: completed target step %d, stopping execution", i+1))
				hcpo.SetRunSingleStepMode(false, -1) // Reset mode
				break
			}

			// Determine next step based on conditional routing (computed during execution)
			humanInputStep, ok := step.(*HumanInputPlanStep)
			if !ok {
				return fmt.Errorf("step %d is not a HumanInputPlanStep", i+1)
			}
			// Use SelectedNextStepID if computed, otherwise fallback to NextStepID
			nextStepID := humanInputStep.SelectedNextStepID
			if nextStepID == "" {
				nextStepID = humanInputStep.NextStepID
			}

			// Handle next step navigation
			if nextStepID == "end" {
				// End workflow
				hcpo.GetLogger().Info(fmt.Sprintf("🏁 Human input step %d specified 'end' - terminating workflow", i+1))
				break
			} else if nextStepID != "" {
				// Find target step by ID and jump to it
				targetStepIndex := -1
				for idx, s := range breakdownSteps {
					if s.GetID() == nextStepID {
						targetStepIndex = idx
						break
					}
				}
				if targetStepIndex >= 0 {
					hcpo.GetLogger().Info(fmt.Sprintf("🔗 Jumping to step %d (ID: %s) as specified by next_step_id", targetStepIndex+1, nextStepID))

					// Update startFromStep to allow execution from target step
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
			if prevIdx < len(breakdownSteps) {
				contextOutput := breakdownSteps[prevIdx].GetContextOutput().String()
				if contextOutput != "" {
					// Resolve variables in context output (consistent with conditional steps)
					resolvedOutput := ResolveVariables(contextOutput, hcpo.variableValues)
					previousContextFiles = append(previousContextFiles, resolvedOutput)
				}
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
			breakdownSteps,           // allSteps - pass all steps for prerequisite detection
			false,                    // isDecisionInnerStep = false (regular step)
			decisionCtx,              // decisionContext - nil if not routed from decision step
			"",                       // decisionEvaluationQuestion - empty for regular steps
			false,                    // isSubAgent = false (regular step)
			previousExecutionResults, // Execution outputs from previous steps
			nil,                      // orchestrationRoutes - nil for regular steps (not sub-agents)
		)
		if err != nil {
			// Check if this is a prerequisite navigation error using errors.As to extract the struct
			var prereqErr *PrerequisiteFailureError
			if errors.As(err, &prereqErr) {
				// Validate DependsOnStepID is not empty
				if prereqErr.DependsOnStepID == "" {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Prerequisite error has empty DependsOnStepID, ignoring navigation"))
				} else {
					// Find target step by ID in breakdownSteps array
					targetStepIndex := -1
					for idx, s := range breakdownSteps {
						if s.GetID() == prereqErr.DependsOnStepID {
							targetStepIndex = idx
							break
						}
					}

					// Validate step was found and perform safety checks
					if targetStepIndex < 0 {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Could not find step with ID %s in breakdownSteps, ignoring navigation", prereqErr.DependsOnStepID))
					} else if targetStepIndex >= len(breakdownSteps) {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Invalid target step index %d (exceeds array length %d), ignoring navigation", targetStepIndex, len(breakdownSteps)))
					} else if targetStepIndex >= i {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Target step index %d is not before current step %d, ignoring navigation", targetStepIndex+1, i+1))
					} else {
						// Safety check: navigation distance (max 10 steps)
						navigationDistance := i - targetStepIndex
						if navigationDistance > 10 {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Navigation distance %d exceeds maximum (10 steps), ignoring navigation", navigationDistance))
						} else {
							hcpo.GetLogger().Info(fmt.Sprintf("🔄 Prerequisite navigation: restarting execution from step %d (ID: %s, reason: %s)", targetStepIndex+1, prereqErr.DependsOnStepID, prereqErr.Reason))

							// Clean up progress from target step onward to ensure it gets re-executed
							// This removes the target step and all subsequent steps from the completed list
							if err := hcpo.cleanupProgressFromStep(ctx, targetStepIndex, progress); err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup progress from step %d: %v (continuing anyway)", targetStepIndex+1, err))
							} else {
								hcpo.GetLogger().Info(fmt.Sprintf("🔄 Cleaned up progress: removed step %d and all subsequent steps from completed list", targetStepIndex+1))
							}

							// Reset execution results to only include steps up to target step
							// This ensures the target step doesn't see stale results from later steps
							if targetStepIndex < len(previousExecutionResults) {
								previousExecutionResults = previousExecutionResults[:targetStepIndex]
								hcpo.GetLogger().Info(fmt.Sprintf("🔄 Reset previousExecutionResults to %d entries (removed results from step %d onward)", len(previousExecutionResults), targetStepIndex+1))
							}

							// Update startFromStep to ensure target step isn't skipped by the startFromStep check
							// This prevents the step from being skipped if targetStepIndex < startFromStep
							if targetStepIndex < startFromStep {
								startFromStep = targetStepIndex
								hcpo.GetLogger().Info(fmt.Sprintf("🔄 Updated startFromStep to %d to allow execution from prerequisite target step", startFromStep+1))
							}

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
			stepTitle := step.GetTitle()
			if stepTitle == "" {
				stepTitle = fmt.Sprintf("Step %d", i+1)
			}
			stepId := step.GetID()
			if stepId == "" {
				stepId = fmt.Sprintf("step-%d", i+1)
			}
			hcpo.EmitStepFailedEvent(ctx, stepId, stepTitle, stepPath, err.Error(), i, false)
			hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted step_failed event for step %d: %s", i+1, stepTitle))
			return fmt.Errorf("step %d execution failed: %w", i+1, err)
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

	// Clear current step ID on context-aware bridge (cleanup after execution ends)
	if bridge := hcpo.GetContextAwareBridge(); bridge != nil {
		if stepBridge, ok := bridge.(interface {
			ClearCurrentStepID()
		}); ok {
			stepBridge.ClearCurrentStepID()
		}
	}

	// Final save to ensure all completed steps are persisted
	// This is a safety measure to catch any steps that might have been missed
	if err := hcpo.saveStepProgress(ctx, progress); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save final step progress: %v", err))
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
func (hcpo *StepBasedWorkflowOrchestrator) sanitizeTitleForAgentName(title string) string {
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
func (hcpo *StepBasedWorkflowOrchestrator) readLearningHistory(
	ctx context.Context,
	stepIndex int,
	stepID string,
	stepPath string,
) (formattedLearningHistory string, err error) {
	// Always read learnings (no caching)
	hcpo.GetLogger().Info(fmt.Sprintf("🔀 Reading learning history for %s (ID: %s)", stepPath, stepID))

	// Determine step folder path - learnings are at workspace root (not inside runs/)
	// Use step ID based path for learnings (new format)
	// In evaluation mode, learnings are stored in evaluation/learnings/
	// getLearningFolderPathByStepID now returns RELATIVE path - workspace functions auto-prepend workspacePath
	stepLearningsPath := getLearningFolderPathByStepID("", stepID, stepPath, hcpo.isEvaluationMode)

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
