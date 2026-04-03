package step_based_workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"

	baseevents "github.com/manishiitg/mcpagent/events"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/events"

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
//   - "step-2-sub-login" -> {ParentStepNumber: 2, BranchType: "", BranchIndex: -1, IsBranchStep: true} (todo_task route sub-agent step)
func parseStepPath(stepPath string) StepPathInfo {
	// Regular step pattern: "step-{number}"
	regularStepRegex := regexp.MustCompile(`^step-(\d+)$`)
	// Branch step pattern: "step-{number}-if-{true|false}-{index}"
	branchStepRegex := regexp.MustCompile(`^step-(\d+)-if-(true|false)-(\d+)$`)
	// Decision step inner step pattern: "step-{number}-decision"
	decisionStepRegex := regexp.MustCompile(`^step-(\d+)-decision$`)
	// Sub-agent step pattern: "step-{number}-sub-agent-{index}" or "step-{number}-sub-agent-{index}-i-{iteration}"
	subAgentStepRegex := regexp.MustCompile(`^step-(\d+)-sub-agent-(\d+)(?:-i-(\d+))?$`)
	// Todo-task route sub-agent step pattern: "step-{number}-sub-{routeId}"
	todoTaskSubAgentStepRegex := regexp.MustCompile(`^step-(\d+)-sub-.+$`)

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
	} else if matches := todoTaskSubAgentStepRegex.FindStringSubmatch(stepPath); matches != nil {
		// Todo-task route sub-agent step - treat as a dedicated sub-agent step.
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

func getArtifactFolderName(stepID string, stepPath string) string {
	stepID = strings.TrimSpace(stepID)
	if stepID != "" {
		return stepID
	}
	return stepPath
}

// getExecutionFolderPath returns the execution folder path based on the stable step ID when available.
// stepPath remains the control-flow identifier and fallback for older callers.
func getExecutionFolderPath(executionWorkspacePath string, stepID string, stepPath string) string {
	return fmt.Sprintf("%s/%s", executionWorkspacePath, getArtifactFolderName(stepID, stepPath))
}

func (hcpo *StepBasedWorkflowOrchestrator) resolveDependencyPathsWithWorkspace(
	ctx context.Context,
	deps []string,
	stepIndex int,
	stepPath string,
	allSteps []PlanStepInterface,
	executionWorkspacePath string,
	docsRoot string,
	variableValues map[string]string,
) []string {
	if len(deps) == 0 {
		return nil
	}

	appendUniqueCandidates := func(base []string, extras []string) []string {
		seen := make(map[string]bool, len(base))
		for _, item := range base {
			seen[item] = true
		}
		for _, item := range extras {
			if item == "" || seen[item] {
				continue
			}
			base = append(base, item)
			seen[item] = true
		}
		return base
	}

	resolvedPaths := make([]string, 0, len(deps))
	for _, dep := range deps {
		candidates := ResolveDependencyPathCandidates(dep, stepIndex, stepPath, allSteps, executionWorkspacePath, docsRoot, variableValues)
		candidates = appendUniqueCandidates(candidates, hcpo.findApprovedPlanProducerCandidates(dep, executionWorkspacePath, docsRoot, variableValues))
		if len(candidates) == 0 {
			resolvedPaths = append(resolvedPaths, dep)
			continue
		}

		chosen := candidates[0]
		for _, candidate := range candidates {
			if !filepath.IsAbs(candidate) {
				continue
			}
			relPath := strings.TrimPrefix(candidate, docsRoot+"/")
			if relPath == candidate {
				continue
			}
			if _, err := hcpo.ReadWorkspaceFile(ctx, relPath); err == nil {
				chosen = candidate
				break
			}
		}

		resolvedPaths = append(resolvedPaths, chosen)
	}
	return resolvedPaths
}

func (hcpo *StepBasedWorkflowOrchestrator) findApprovedPlanProducerCandidates(
	dep string,
	executionWorkspacePath string,
	docsRoot string,
	variableValues map[string]string,
) []string {
	if hcpo.approvedPlan == nil || dep == "" || filepath.IsAbs(dep) || strings.Contains(dep, "/") {
		return nil
	}

	allPlanSteps := collectAllSteps(hcpo.approvedPlan.Steps)
	candidates := make([]string, 0, 4)
	seen := make(map[string]bool)

	for _, info := range allPlanSteps {
		step := info.Step
		if step == nil {
			continue
		}
		output := ResolveVariables(step.GetContextOutput().String(), variableValues)
		if !contextOutputMatchesDependency(output, dep) {
			continue
		}

		stepID := strings.TrimSpace(step.GetID())
		if stepID == "" {
			continue
		}

		appendCandidate := func(candidate string) {
			if candidate == "" || seen[candidate] {
				return
			}
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}

		// Prefer the stable artifact folder keyed by step_id.
		appendCandidate(filepath.Join(docsRoot, getExecutionFolderPath(executionWorkspacePath, stepID, stepID), dep))

		// Backward compatibility: older runs may still have artifacts in positional step folders
		// like execution/step-1 or execution/step-1-sub-read-credentials.
		legacyStepPath := ""
		if info.TopIndex > 0 {
			legacyStepPath = fmt.Sprintf("step-%d", info.TopIndex)
		} else {
			infoCopy := info
			legacyStepPath = resolveInnerStepPath(hcpo.approvedPlan.Steps, &infoCopy)
		}
		if legacyStepPath != "" {
			appendCandidate(filepath.Join(docsRoot, getExecutionFolderPath(executionWorkspacePath, "", legacyStepPath), dep))
		}
	}

	return candidates
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

// deleteFolderViaAPI deletes a folder (and all its contents) via the Workspace API (DELETE /api/folders/{path}).
// The folderPath should be relative to workspace-docs root (e.g., "Workflow/X/learnings/step-1/code").
func deleteFolderViaAPI(ctx context.Context, folderPath string) error {
	pathSegments := strings.Split(folderPath, "/")
	encodedSegments := make([]string, len(pathSegments))
	for i, segment := range pathSegments {
		encodedSegments[i] = url.PathEscape(segment)
	}
	encodedPath := strings.Join(encodedSegments, "/")

	apiURL := getWorkspaceAPIURL() + "/api/folders/" + encodedPath + "?confirm=true"
	req, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call workspace API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil // already gone
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("workspace API returned status %d: %s", resp.StatusCode, string(body))
	}
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

// getValidationFolderPath returns the logs folder path based on the stable step ID when available.
func getValidationFolderPath(validationWorkspacePath string, stepID string, stepPath string) string {
	return fmt.Sprintf("%s/logs/%s", validationWorkspacePath, getArtifactFolderName(stepID, stepPath))
}

// getExecutionFolderPathForLogs returns the execution logs folder path based on the stable step ID when available.
func getExecutionFolderPathForLogs(validationWorkspacePath string, stepID string, stepPath string) string {
	return fmt.Sprintf("%s/logs/%s/execution", validationWorkspacePath, getArtifactFolderName(stepID, stepPath))
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

// saveExecutionConversationLogs saves execution result, conversation history, and prompts to log files.
// Called on both success and failure/cancellation paths so partial conversations from interrupted
// executions can be inspected via debug_step or direct log file access.
// Uses context.Background() internally so writes succeed even when the caller's context is canceled.
func (hcpo *StepBasedWorkflowOrchestrator) saveExecutionConversationLogs(
	stepIndex int, stepID string, stepPath string, retryAttempt int, loopIterationCount int,
	executionResult string, executionLLM string,
	conversationHistory []llmtypes.MessageContent,
	executionAgent agents.OrchestratorAgent,
) {
	// Use background context so saves succeed even when execution was canceled/stopped by user
	saveCtx := context.Background()

	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}
	logDir := getExecutionFolderPathForLogs(validationWorkspacePath, stepID, stepPath)
	filenameBase := fmt.Sprintf("execution-attempt-%d-iteration-%d", retryAttempt, loopIterationCount)

	// Save execution result
	resultPath := fmt.Sprintf("%s/%s.json", logDir, filenameBase)
	resultData := map[string]interface{}{
		"step_index":       stepIndex + 1,
		"step_path":        stepPath,
		"retry_attempt":    retryAttempt,
		"loop_iteration":   loopIterationCount,
		"execution_result": executionResult,
		"model":            executionLLM,
		"timestamp":        time.Now().Format(time.RFC3339),
	}
	if resultJSON, err := json.MarshalIndent(resultData, "", "  "); err == nil {
		if err := hcpo.WriteWorkspaceFile(saveCtx, resultPath, string(resultJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write execution result to %s: %v", resultPath, err))
		}
	}

	// Save conversation history
	convPath := fmt.Sprintf("%s/%s-conversation.json", logDir, filenameBase)
	convData := map[string]interface{}{
		"step_index":           stepIndex + 1,
		"step_path":            stepPath,
		"retry_attempt":        retryAttempt,
		"loop_iteration":       loopIterationCount,
		"conversation_history": conversationHistory,
		"timestamp":            time.Now().Format(time.RFC3339),
	}
	if convJSON, err := json.MarshalIndent(convData, "", "  "); err == nil {
		if err := hcpo.WriteWorkspaceFile(saveCtx, convPath, string(convJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write conversation history to %s: %v", convPath, err))
		}
	}

	// Save system prompt (overwrites the pre-execution save with the final rendered prompt)
	var capturedSystemPrompt string
	if executionAgent != nil {
		if ba := executionAgent.GetBaseAgent(); ba != nil && ba.Agent() != nil {
			capturedSystemPrompt = ba.Agent().GetSystemPrompt()
		}
	}
	promptsPath := fmt.Sprintf("%s/%s-prompts.json", logDir, filenameBase)
	promptsData := map[string]interface{}{
		"step_index":    stepIndex + 1,
		"step_path":     stepPath,
		"system_prompt": capturedSystemPrompt,
		"saved_at":      "post_execution",
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	if promptsJSON, err := json.MarshalIndent(promptsData, "", "  "); err == nil {
		if err := hcpo.WriteWorkspaceFile(saveCtx, promptsPath, string(promptsJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write prompts to %s: %v", promptsPath, err))
		}
	}

	hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved execution logs for step %d (%s) attempt %d", stepIndex+1, stepPath, retryAttempt))
}

// loadSingleStepResultFromLogs reads the execution result for a single step (1-based stepNumber)
// from its log files. Returns the result string and true if found, or "" and false otherwise.
func (hcpo *StepBasedWorkflowOrchestrator) loadSingleStepResultFromLogs(ctx context.Context, stepNumber int) (string, bool) {
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		validationWorkspacePath = fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		validationWorkspacePath = hcpo.GetWorkspacePath()
	}

	stepPath := fmt.Sprintf("step-%d", stepNumber)
	stepID := stepPath
	if hcpo.approvedPlan != nil && stepNumber >= 1 && stepNumber <= len(hcpo.approvedPlan.Steps) {
		if id := hcpo.approvedPlan.Steps[stepNumber-1].GetID(); id != "" {
			stepID = id
		}
	}
	executionLogsFolderPath := getExecutionFolderPathForLogs(validationWorkspacePath, stepID, stepPath)

	var latestExecutionResult string
	var latestAttempt, latestIteration int

	for attempt := 1; attempt <= 10; attempt++ {
		for iteration := 0; iteration <= 10; iteration++ {
			executionResultFilePath := fmt.Sprintf("%s/execution-attempt-%d-iteration-%d.json", executionLogsFolderPath, attempt, iteration)
			content, err := hcpo.ReadWorkspaceFile(ctx, executionResultFilePath)
			if err != nil {
				continue
			}

			var executionData map[string]interface{}
			if err := json.Unmarshal([]byte(content), &executionData); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("Failed to parse execution result from %s: %v", executionResultFilePath, err))
				continue
			}

			if execResult, ok := executionData["execution_result"].(string); ok {
				if attempt > latestAttempt || (attempt == latestAttempt && iteration > latestIteration) {
					latestExecutionResult = execResult
					latestAttempt = attempt
					latestIteration = iteration
				}
			}
		}
	}

	if latestExecutionResult != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("Loaded execution result from logs for step %d (attempt %d, iteration %d)", stepNumber, latestAttempt, latestIteration))
		return latestExecutionResult, true
	}
	return "", false
}

// loadExecutionResultsFromLogs loads execution results from logs folder for previous steps
// This is a shared/reusable function that can be called from anywhere in the controller
// It's used when resuming from a step or running a single step, where execution results aren't in memory
// Returns an array of execution results indexed by step index (0-based)
// For each step, it finds the latest execution result file (highest attempt, then highest iteration)
func (hcpo *StepBasedWorkflowOrchestrator) loadExecutionResultsFromLogs(ctx context.Context, allSteps []PlanStepInterface, currentStepIndex int) []string {
	executionResults := make([]string, currentStepIndex)

	for i := 0; i < currentStepIndex && i < len(allSteps); i++ {
		if result, ok := hcpo.loadSingleStepResultFromLogs(ctx, i+1); ok {
			executionResults[i] = result
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

	// Compute execution workspace path for building full output file paths
	var executionWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		executionWorkspacePath = fmt.Sprintf("%s/runs/%s/execution", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
	} else {
		executionWorkspacePath = fmt.Sprintf("%s/execution", hcpo.GetWorkspacePath())
	}

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

		// Compute the step execution folder path based on step type
		// Decision steps use step-N-decision, everything else uses step-N
		stepPath := fmt.Sprintf("step-%d", i+1)
		if step.StepType() == StepTypeDecision {
			stepPath = fmt.Sprintf("step-%d-decision", i+1)
		}
		stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, step.GetID(), stepPath)

		summary.WriteString(fmt.Sprintf("**Step %d: %s**\n", i+1, resolvedTitle))
		summary.WriteString(fmt.Sprintf("- **Description**: %s\n", description))
		// Strip workflow root prefix so paths are relative to working directory
		relStepExecPath := strings.TrimPrefix(stepExecutionPath, hcpo.GetWorkspacePath()+"/")
		summary.WriteString(fmt.Sprintf("- **Output File**: `%s/%s`\n", relStepExecPath, resolvedOutput))
		summary.WriteString("\n")

		stepCount++
	}

	if stepCount == 0 {
		return "" // No previous steps with context outputs
	}

	summary.WriteString("Use this context to understand the workflow progression and what has been accomplished so far.\n")

	// Include ALL human_input step results (regardless of position) as critical context,
	// plus the most recent non-human-input execution result for general context.
	// This matches the routing agent's behavior in controller_routing.go.
	humanFeedbackIncluded := false
	for idx := 0; idx < currentStepIndex && idx < len(previousExecutionResults); idx++ {
		if previousExecutionResults[idx] == "" {
			continue
		}
		if idx < len(allSteps) && allSteps[idx].StepType() == StepTypeHumanInput {
			stepTitle := ResolveVariables(allSteps[idx].GetTitle(), hcpo.variableValues)
			execOutput := previousExecutionResults[idx]
			if len(execOutput) > 2000 {
				execOutput = execOutput[:2000] + "\n... (truncated)"
			}
			summary.WriteString(fmt.Sprintf("\n## 🚨 HUMAN FEEDBACK (CRITICAL - READ CAREFULLY)\n\n"))
			summary.WriteString(fmt.Sprintf("The human provided the following feedback/input in **Step %d: %s**.\n", idx+1, stepTitle))
			summary.WriteString("**You MUST incorporate this human feedback into your work. This takes priority over other context.**\n\n")
			summary.WriteString(fmt.Sprintf("```\n%s\n```\n", execOutput))
			humanFeedbackIncluded = true
		}
	}

	// Include the most recent non-human-input execution result
	for idx := currentStepIndex - 1; idx >= 0; idx-- {
		if idx >= len(previousExecutionResults) || previousExecutionResults[idx] == "" {
			continue
		}
		if idx < len(allSteps) && allSteps[idx].StepType() == StepTypeHumanInput {
			continue // Already included above
		}
		execOutput := previousExecutionResults[idx]
		if len(execOutput) > 2000 {
			execOutput = execOutput[:2000] + "\n... (truncated)"
		}
		var stepTitle string
		if idx < len(allSteps) {
			stepTitle = ResolveVariables(allSteps[idx].GetTitle(), hcpo.variableValues)
		} else {
			stepTitle = fmt.Sprintf("Step %d", idx+1)
		}
		if humanFeedbackIncluded {
			summary.WriteString(fmt.Sprintf("\n## 📤 Most Recent Step Execution Output\n\n"))
		} else {
			summary.WriteString(fmt.Sprintf("\n## 📤 Previous Step Execution Output\n\n"))
		}
		summary.WriteString(fmt.Sprintf("**Step %d: %s** execution result:\n\n", idx+1, stepTitle))
		summary.WriteString(fmt.Sprintf("```\n%s\n```\n", execOutput))
		summary.WriteString("\nUse this execution output to understand what the immediately previous step accomplished.\n")
		break
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
	execCtx *ExecutionContext, // Execution context with flags (skipHumanInput, etc.)
	allSteps []PlanStepInterface, // All steps in the plan
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

	// Scripted code mode — determined once per step invocation (persists across outer-loop iterations).
	// Check embedded plan AgentConfigs first; fall back to step_configs.json so that workshop-saved
	// configs (use_code_execution_mode/use_learn_code_mode) also take effect for sub-agent steps whose embedded plan
	// config may not have the flag.
	isLearnCodeMode := false
	{
		agentCfgs := getAgentConfigs(step)
		if (agentCfgs == nil || !isScriptedExecutionModeConfig(agentCfgs)) && step.GetID() != "" {
			if stepConfigs, err := hcpo.ReadStepConfigs(ctx); err == nil {
				for _, sc := range stepConfigs {
					if sc.ID == step.GetID() && isScriptedExecutionModeConfig(sc.AgentConfigs) {
						agentCfgs = sc.AgentConfigs
						break
					}
				}
			}
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted_code] step=%s agentCfgs_nil=%v scripted=%v",
			step.GetID(), agentCfgs == nil,
			isScriptedExecutionModeConfig(agentCfgs)))
		isLearnCodeMode = isScriptedExecutionModeConfig(agentCfgs)
	}
	if execCtx != nil && execCtx.SavedScriptOnly && !isLearnCodeMode {
		return "", updatedContextFiles, fmt.Errorf("step %q is not in scripted code mode", step.GetID())
	}
	learnCodePriorScript := ""          // old script content when saved script failed (LLM relearn context)
	learnCodePriorError := ""           // error from failed saved script (LLM relearn context)
	learnCodeScriptNeedsSaving := false // set after LLM writes main.py and pre-validation passes
	learnCodeFastPathDone := false      // set when saved script ran successfully (skip execution loop)

	// Initialize variables for step execution.
	// Regular steps get 3 retries on pre-validation failure.
	// Sub-agents and workshop single-step mode get 2 — if it can't fix in 2 tries,
	// better to return to the orchestrator/user with feedback than burn more tokens.
	maxRetryAttempts := 3
	if isSubAgent {
		maxRetryAttempts = 3
	}
	if hcpo.runSingleStepOnly {
		maxRetryAttempts = 3
	}
	var executionConversationHistory []llmtypes.MessageContent // Only used for learning agents after execution
	var learnCodePreValidationResultsOverride *WorkspaceVerificationResult
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
		// Determine code execution mode: step config > workflow/preset default
		// Note: Provider-based auto-enable (claude-code/gemini-cli) is handled in applyStepConfigToAgentConfig.
		var isCodeExecutionMode bool
		agentConfigs := getAgentConfigs(step)
		if (agentConfigs == nil || (agentConfigs.UseCodeExecutionMode == nil && agentConfigs.UseToolSearchMode == nil && agentConfigs.UseLearnCodeMode == nil)) && step.GetID() != "" {
			if stepConfigs, err := hcpo.ReadStepConfigs(ctx); err == nil {
				for _, sc := range stepConfigs {
					if sc.ID == step.GetID() && sc.AgentConfigs != nil {
						agentConfigs = sc.AgentConfigs
						break
					}
				}
			}
		}
		if agentConfigs != nil && agentConfigs.UseCodeExecutionMode != nil {
			isCodeExecutionMode = *agentConfigs.UseCodeExecutionMode
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode: %v", isCodeExecutionMode))
		} else {
			isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using workflow/preset code execution mode: %v", isCodeExecutionMode))
		}
		// Determine tool search mode: Priority: step config > preset default
		// Code execution mode and tool search mode are mutually exclusive
		isToolSearchMode := hcpo.getToolSearchMode(agentConfigs)
		if isCodeExecutionMode {
			isToolSearchMode = false
		}
		// Scripted code mode implies code execution mode
		if isLearnCodeMode {
			isCodeExecutionMode = true
			isToolSearchMode = false
			hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted_code] Persistent scripted mode enabled for step %d — forcing code execution mode", stepIndex+1))
		}

		// Always use learnings folder (unified folder for all learning types)
		learningsPath := fmt.Sprintf("%s/learnings", hcpo.GetWorkspacePath())
		// Get execution folder path for this step (e.g., "execution/step-8" or "execution/step-3-true-0")
		stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, step.GetID(), stepPath)
		// Ensure step execution folder exists (create if it was previously deleted)
		if err := hcpo.ensureStepExecutionFolderExists(ctx, stepExecutionPath); err != nil {
			// Non-blocking: log warning but continue execution (folder will be created when files are written)
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure step execution folder exists: %v (continuing - folder will be created when files are written)", err))
		}
		// Get knowledgebase folder path (persistent files across runs, at workspace root) - only if enabled
		knowledgebasePath := ""
		useKnowledgebase := hcpo.UseKnowledgebase()
		// Per-step override: disable_knowledgebase in step config takes precedence
		if agentConfigs != nil && agentConfigs.DisableKnowledgebase != nil {
			if *agentConfigs.DisableKnowledgebase {
				useKnowledgebase = false
			} else {
				useKnowledgebase = true
			}
		}
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
		folderGuardReadPaths, folderGuardWritePaths := hcpo.setupExecutionFolderGuard(stepPath, step.GetID(), hasLearnings, useKnowledgebase)

		// Learn code mode: add code/ subdir to write paths so LLM can write main.py there
		if isLearnCodeMode {
			stepExecutionPathForGuard := getExecutionFolderPath(executionWorkspacePath, step.GetID(), stepPath)
			folderGuardWritePaths = append(folderGuardWritePaths, stepExecutionPathForGuard+"/code")
		}

		// Determine if skip execution cleanup is enabled
		skipExecutionCleanup := false
		if hcpo.executionOptions != nil {
			skipExecutionCleanup = hcpo.executionOptions.SkipExecutionCleanup
		}

		// Build absolute paths for agent prompts using the workspace docs root.
		// Absolute paths are unambiguous — agents can use them directly in shell commands.
		// e.g., "Workflow/HRMS/runs/iteration-1/group-1/execution/step-3"
		//     → "/app/workspace-docs/Workflow/HRMS/runs/iteration-1/group-1/execution/step-3"
		workflowRoot := hcpo.GetWorkspacePath()
		docsRoot := GetPromptDocsRoot()
		toAbsPath := func(path string) string {
			if path == "" || docsRoot == "" {
				return path
			}
			return filepath.Join(docsRoot, path)
		}
		toAbsPathSlice := func(paths []string) []string {
			result := make([]string, len(paths))
			for i, p := range paths {
				result[i] = toAbsPath(p)
			}
			return result
		}

		stepTitleForPrompt := ResolveVariables(step.GetTitle(), hcpo.variableValues)
		stepDescriptionForPrompt := ResolveVariables(step.GetDescription(), hcpo.variableValues)
		if isLearnCodeMode {
			// In learn_code mode the saved script is reused across groups/users, so keep
			// template placeholders in the task description instead of
			// injecting current-run values that the model might copy into main.py.
			stepDescriptionForPrompt = step.GetDescription()
		}

		contextDeps := step.GetContextDependencies()
		resolvedContextDeps := []string{}
		if len(contextDeps) > 0 {
			resolvedDeps := ResolveVariablesArray(contextDeps, hcpo.variableValues)
			resolvedContextDeps = hcpo.resolveDependencyPathsWithWorkspace(ctx, resolvedDeps, stepIndex, stepPath, allSteps, executionWorkspacePath, docsRoot, hcpo.variableValues)
		}

		learnCodeInputArgsForPrompt := ""
		if isLearnCodeMode && len(resolvedContextDeps) > 0 {
			// Reuse the exact same resolved dependency list for both the saved prompt
			// and the later python3 main.py invocation so they cannot drift.
			learnCodeInputArgsForPrompt = strings.Join(resolvedContextDeps, "\n")
		}

		templateVars := map[string]string{
			"StepTitle":             stepTitleForPrompt,
			"StepDescription":       stepDescriptionForPrompt,
			"StepSuccessCriteria":   "",
			"StepContextOutput":     ResolveVariables(step.GetContextOutput().String(), hcpo.variableValues),
			"WorkspacePath":         toAbsPath(executionWorkspacePath),                         // Absolute execution folder path (e.g., "/app/workspace-docs/Workflow/HRMS/runs/...")
			"LearningsPath":         toAbsPath(learningsPath),                                  // Absolute learnings folder path
			"KnowledgebasePath":     toAbsPath(knowledgebasePath),                              // Absolute knowledgebase folder path
			"UseKnowledgebase":      fmt.Sprintf("%v", useKnowledgebase),                       // Whether knowledgebase is enabled
			"IsCodeExecutionMode":   fmt.Sprintf("%v", isCodeExecutionMode),                    // Code execution mode flag (step-specific or preset)
			"UseToolSearchMode":     fmt.Sprintf("%v", isToolSearchMode),                       // Tool search mode flag (step-specific or preset)
			"StepNumber":            stepPath,                                                  // Step identifier (e.g., "step-8" or "step-3-if-true-0")
			"StepExecutionPath":     toAbsPath(stepExecutionPath),                              // Absolute step execution folder path
			"FolderGuardReadPaths":  strings.Join(toAbsPathSlice(folderGuardReadPaths), ", "),  // Absolute folder guard read paths
			"FolderGuardWritePaths": strings.Join(toAbsPathSlice(folderGuardWritePaths), ", "), // Absolute folder guard write paths
			"SkipExecutionCleanup":  fmt.Sprintf("%v", skipExecutionCleanup),                   // Skip cleanup mode flag for state verification prompt
			"IsEvaluationMode":      fmt.Sprintf("%v", hcpo.isEvaluationMode),                  // Evaluation mode flag for eval-specific prompt guidance
			"WorkflowRoot":          toAbsPath(workflowRoot),                                   // Absolute workflow root path (e.g., "/app/workspace-docs/Workflow/HRMS")
			"IsLearnCodeMode":       fmt.Sprintf("%v", isLearnCodeMode),
			"IsRelearnMode":         fmt.Sprintf("%v", isLearnCodeMode && learnCodePriorScript != ""),
			"LearnCodePriorScript":  learnCodePriorScript,
			"LearnCodePriorError":   learnCodePriorError,
			"LearnCodeInputArgs":    learnCodeInputArgsForPrompt,
			"LearnCodeEnvVarNames":  buildLearnCodeEnvVarNamesForPrompt(isLearnCodeMode, hcpo.GetWorkspaceEnvRef()),
			"LearnCodeVarMapping":   buildLearnCodeVarMappingForPrompt(isCodeExecutionMode || isLearnCodeMode, hcpo.variablesManifest),
		}

		// Inject workflow variables as VAR_* env vars and workspace path as VAR_WORKSPACE_PATH.
		// VAR_* passes through the shell whitelist (MCP_*, SECRET_*, VAR_*).
		// Available via os.environ["VAR_NAME"] in Python or $VAR_NAME in bash.
		if envRef := hcpo.GetWorkspaceEnvRef(); envRef != nil {
			for k, v := range hcpo.variableValues {
				envRef["VAR_"+k] = v
			}
			// Also inject the workflow workspace path as an absolute Docker-visible path
			// so Python/shell code can use it directly without guessing the docs root.
			if wp := hcpo.GetWorkspacePath(); wp != "" {
				envRef["VAR_WORKSPACE_PATH"] = toAbsPath(wp)
			}
		}

		// Add context dependencies with full absolute paths
		if len(resolvedContextDeps) > 0 {
			templateVars["StepContextDependencies"] = strings.Join(resolvedContextDeps, ", ")

			// Pre-flight: verify all resolved input files exist before launching any agent.
			// A missing file means a prior step didn't produce its output — retrying this step
			// won't fix that, so fail immediately with a clear message.
			for _, absPath := range resolvedContextDeps {
				if !filepath.IsAbs(absPath) {
					continue // bare/relative — can't check via workspace API, skip
				}
				relPath := strings.TrimPrefix(absPath, docsRoot+"/")
				if _, readErr := hcpo.ReadWorkspaceFile(ctx, relPath); readErr != nil {
					return "", updatedContextFiles, fmt.Errorf(
						"input file not found: %s\n(produced by a prior step — check that the previous step completed successfully)", absPath)
				}
			}
		} else {
			templateVars["StepContextDependencies"] = ""
		}

		// Add variable names if available (same format as other agents)
		if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
			templateVars["VariableNames"] = variableNames
		}

		// Add variable values only in tool search mode — tool search agents cannot read env vars,
		// so they need values in the prompt. Code exec and learn_code agents read values via
		// SECRET_<VAR> env vars and must NOT have values hardcoded in the prompt.
		if isToolSearchMode {
			if variableValues := FormatVariableValues(hcpo.variablesManifest, hcpo.variableValues); variableValues != "" {
				templateVars["VariableValues"] = variableValues
			}
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

		// Append workshop human input as critical feedback (passed via execute_step's human_input parameter)
		if hcpo.interactiveWorkflowHumanInput != "" {
			if previousStepsSummary == "" {
				previousStepsSummary = "## 📋 Previous Steps Context\n\n"
			}
			previousStepsSummary += fmt.Sprintf("\n## 🚨 HUMAN FEEDBACK (CRITICAL - READ CAREFULLY)\n\n")
			previousStepsSummary += "The human provided the following instructions via the interactive workshop.\n"
			previousStepsSummary += "**You MUST incorporate this human feedback into your work. This takes priority over other context.**\n\n"
			previousStepsSummary += fmt.Sprintf("```\n%s\n```\n", hcpo.interactiveWorkflowHumanInput)
		}

		templateVars["PreviousStepsSummary"] = previousStepsSummary

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

		// Inner loop: Automatic retry logic
		var validationResponse *ValidationResponse
		var previousValidationResponse *ValidationResponse // Preserve previous validation response for retry detection

		// Learn code mode: attempt fast path execution with saved script (before any LLM work).
		if isLearnCodeMode && !learnCodeFastPathDone {
			fastResult := hcpo.tryRunSavedLearnCodeScript(ctx, step, stepIndex, stepPath, allSteps,
				stepExecutionPath, executionWorkspacePath)
			if fastResult.RanScript {
				// Emit UI event for the saved-script execution attempt
				savedScriptPath := getLearnCodeScriptAbsPath(GetPromptDocsRoot(), hcpo.GetWorkspacePath(), step.GetID(), hcpo.isEvaluationMode)
				hcpo.emitLearnCodeScriptExecutionEvent(ctx, step, stepIndex, stepPath,
					savedScriptPath, fastResult.Success, fastResult.ExitCode, fastResult.Output, fastResult.Error, 0, true)
				// Save execution log so debug_step and direct file inspection can see fast-path output
				hcpo.saveLearnCodeFastPathLog(ctx, stepIndex, step.GetID(), stepPath, savedScriptPath, fastResult)
			}
			if fastResult.RanScript && fastResult.Success {
				// Saved script executed and validated — skip LLM entirely
				learnCodeFastPathDone = true
				executionResult = fastResult.Output
				validationResponse = &ValidationResponse{
					IsSuccessCriteriaMet: true,
					ExecutionStatus:      "COMPLETED",
					Reasoning:            "learn_code: saved script executed and validated (0 LLM tokens)",
				}
				hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] Fast path succeeded for step %d — skipping execution loop", stepIndex+1))
			} else if fastResult.RanScript {
				// Script ran but failed — fall through to LLM for relearn
				learnCodePriorScript = fastResult.ExistingScript
				learnCodePriorError = fastResult.Error
				hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] Script failed for step %d — falling back to LLM with error context", stepIndex+1))
			} else if fastResult.ExistingScript != "" {
				// A saved script exists but wasn't executed successfully. Pass it to the LLM
				// so it can adapt the working script rather than rewriting from scratch.
				learnCodePriorScript = fastResult.ExistingScript
				// No prior error — this is an update/reuse path, not a failure
				hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] Step %d found an existing saved script — LLM will update it in place", stepIndex+1))
			}

			// templateVars were built before the fast-path check. Refresh the learn_code
			// prompt fields here so a saved-script reuse/failure can surface the saved script
			// and prior error in the actual rendered prompt for this same execution pass.
			templateVars["IsRelearnMode"] = fmt.Sprintf("%v", isLearnCodeMode && learnCodePriorScript != "")
			templateVars["LearnCodePriorScript"] = learnCodePriorScript
			templateVars["LearnCodePriorError"] = learnCodePriorError

			if execCtx != nil && execCtx.SavedScriptOnly {
				if fastResult.RanScript && fastResult.Success {
					hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted_code] Saved-script-only run succeeded for step %d", stepIndex+1))
				} else if fastResult.RanScript {
					return "", updatedContextFiles, fmt.Errorf("saved main.py failed for step %q:\n%s", step.GetID(), fastResult.Error)
				} else {
					return "", updatedContextFiles, fmt.Errorf("no saved main.py found for scripted step %q in learnings/%s/main.py", step.GetID(), step.GetID())
				}
			}
		}

		// Main execution (single execution, no loop)
		// NOTE: No conversation history is passed to execution agent - all context via template variables
		if !learnCodeFastPathDone {
			// Check for context cancellation before execution
			select {
			case <-ctx.Done():
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled for step %d", stepIndex+1))
				return "", updatedContextFiles, fmt.Errorf("step execution canceled: %w", ctx.Err())
			default:
			}

			// Set loop template vars to empty (loop feature removed)
			templateVars["HasLoop"] = "false"
			templateVars["LoopCondition"] = ""
			templateVars["LoopDescription"] = ""
			templateVars["CurrentIteration"] = ""
			templateVars["MaxIterations"] = ""
			templateVars["PreviousIterationOutput"] = ""

			// Resolve variables in step title before using in agent name
			resolvedTitle := ResolveVariables(step.GetTitle(), hcpo.variableValues)
			sanitizedTitle := hcpo.sanitizeTitleForAgentName(resolvedTitle)

			// Run learning reading agent ONCE per main loop iteration (before retry loop)
			// This ensures learning is only discovered once, even if validation fails and we retry
			// Always reads fresh learnings (no caching)
			var formattedLearningHistory string
			var learningFilePaths string // File paths for user message when KeepLearningFull is false

			// Determine KeepLearningFull flag
			// Dynamic logic only: switch based on successful runs in metadata
			agentConfigs := getAgentConfigs(step)
			var keepLearningFull bool
			var keepLearningFullSource string

			// Human-assisted learning mode: learnings are manually curated and treated as locked/final.
			// Always use full learning text, skip exploration thresholds and content hash checks.
			isHumanAssistedLearning := agentConfigs != nil && agentConfigs.LearningMode == "human_assisted"

			// Read metadata (needed for hash checks and threshold decisions)
			learningPathIdentifier := step.GetID()
			metadata, _ := hcpo.GetLearningMetadata(ctx, learningPathIdentifier)

			// Always use paths-only mode — full learning content is too expensive for context.
			// The agent can read learning files if needed.
			keepLearningFull = false
			keepLearningFullSource = "always-false (paths only)"

			// DISABLED: Dynamic keepLearningFull logic (was switching to full content after 2 successful runs)
			// if isHumanAssistedLearning {
			// 	keepLearningFull = true
			// 	keepLearningFullSource = "human_assisted (learnings are final)"
			// } else {
			// 	keepLearningFull = false
			// 	if metadata != nil {
			// 		if metadata.SuccessfulRuns >= 2 {
			// 			keepLearningFull = true
			// 			keepLearningFullSource = fmt.Sprintf("dynamic (threshold met: %d successful runs)", metadata.SuccessfulRuns)
			// 		} else {
			// 			keepLearningFullSource = "dynamic (exploration phase)"
			// 		}
			// 	} else {
			// 		keepLearningFullSource = "dynamic (initial exploration)"
			// 	}
			// }

			hcpo.GetLogger().Info(fmt.Sprintf("🧠 KeepLearningFull decision: %v (Source: %s)", keepLearningFull, keepLearningFullSource))

			// Check if learning is disabled - if so, skip reading learnings entirely
			// Learning is disabled if explicitly set via step config, or if this is a routing step.
			// Routing steps are pure decision/evaluation logic — they don't produce learnable outcomes.
			isLearningDisabledStep := (agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning) || isRoutingStep(step)
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

				// Hash-based exploration reset: if learning content changed since last run, force exploration mode
				// Skip for human-assisted mode — learnings are final, never force exploration
				if !isHumanAssistedLearning && formattedLearningHistory != "" && metadata != nil {
					h := sha256.Sum256([]byte(formattedLearningHistory))
					currentHash := hex.EncodeToString(h[:])
					if metadata.LearningContentHash != "" && metadata.LearningContentHash != currentHash {
						keepLearningFull = false
						keepLearningFullSource = "dynamic (learning content changed — exploration reset)"
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 Learning content changed for step '%s' — forcing exploration mode (hash: %s → %s)", step.GetTitle(), metadata.LearningContentHash[:8], currentHash[:8]))
					}
					// Store the current hash in metadata for next comparison (saved when metadata is persisted later)
					metadata.LearningContentHash = currentHash
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

			// Track failure learning attempts for this execution session (currently unused - failure learning disabled)
			_ = 0 // failureLearningAttempts - disabled while failure learning is turned off

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

				// Add validation feedback to template variables if this is a retry
				if retryAttempt > 1 && validationResponse != nil {
					contextStr := fmt.Sprintf("Validation Feedback (Retry Attempt %d)", retryAttempt)
					templateVars["ValidationFeedback"] = hcpo.formatValidationResponseForTemplate(validationResponse, contextStr)
				} else {
					templateVars["ValidationFeedback"] = "" // No validation feedback for first attempt
				}

				// Step 2: Create and execute Execution-Only Agent with learning history (reused from above)
				executionAgentName := fmt.Sprintf("%s-execution-%s", stepPath, sanitizedTitle)
				// Add validation retry suffix if this is a retry after validation failure (val-2, val-3, etc.)
				if retryAttempt > 1 {
					executionAgentName = fmt.Sprintf("%s-val-%d", executionAgentName, retryAttempt)
				}
				// Add learning history to template vars for execution-only agent (reused for all retry attempts)
				templateVars["LearningHistory"] = formattedLearningHistory
				// Set HasLearnings flag to explicitly indicate whether learnings exist (prevents agent from searching)
				templateVars["HasLearnings"] = fmt.Sprintf("%t", formattedLearningHistory != "")

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
				// Works for:
				// 1. Retry attempts (retryAttempt > 1)
				// 2. Steps routed from decision step with false result (similar to validation failure - skip tempLLM)
				// Note: For tempLLM logic, only FAILED status counts as failure - COMPLETED/PARTIAL/INCOMPLETE are considered success
				isRetryAfterValidationFailure := isValidationFailure(previousValidationResponse) &&
					retryAttempt > 1
				// Also treat decision step false result as validation failure (skip tempLLM)
				isDecisionStepFalse := decisionContext != nil && !decisionContext.DecisionResult
				if isDecisionStepFalse {
					isRetryAfterValidationFailure = true
					hcpo.GetLogger().Info(fmt.Sprintf("🔄 Step routed from decision step with FALSE result - will skip tempLLM (treating as validation failure)"))
				}
				if isRetryAfterValidationFailure && hcpo.fallbackToOriginalLLMOnFailure {
					hcpo.GetLogger().Info(fmt.Sprintf("🔄 Validation failed on previous attempt - will use original LLM instead of temp override (fallback enabled)"))
				}
				agentConfigs := getAgentConfigs(step)
				executionAgentCtx := ctx

				// Pass stepPath to createExecutionOnlyAgent - it will determine the correct execution folder (supports branch and sub-agent steps)
				// For learnings / tempLLM selection, use the concrete step ID so sub-agents align with their own learnings folder.
				// allSteps is already []PlanStepInterface - no conversion needed
				executionAgent, err = hcpo.createExecutionOnlyAgent(executionAgentCtx, "execution_only", stepPath, executionAgentName, agentConfigs, isRetryAfterValidationFailure, retryAttempt, step.GetID())
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

				// Sync only the transport-level code execution flag from the resolved agent config.
				// Keep UseToolSearchMode from the step's logical config so a CLI-backed tool_search
				// step does not get rewritten into scripted-code prompt semantics.
				if executionAgent.GetConfig() != nil {
					templateVars["IsCodeExecutionMode"] = fmt.Sprintf("%v", executionAgent.GetConfig().UseCodeExecutionMode)
				}

				// Pre-save prompts.json so get_step_prompts works during execution (not just after)
				// Include appended supplementary prompts (skills, browser/CDP, secrets) to match
				// what the agent actually sees at runtime (SetSystemPrompt re-appends them).
				if eoa, ok := executionAgent.(*WorkflowExecutionOnlyAgent); ok {
					preSystemPrompt := eoa.executionOnlySystemPromptProcessor(templateVars)
					if ba := executionAgent.GetBaseAgent(); ba != nil {
						if mcpAg := ba.Agent(); mcpAg != nil {
							preSystemPrompt = composePromptWithAppendedSystemPrompts(preSystemPrompt, mcpAg)
						}
					}
					preUserMessage := eoa.executionOnlyUserMessageProcessor(templateVars)
					var preExecLLM string
					if executionAgent.GetConfig() != nil && executionAgent.GetConfig().LLMConfig.Primary.ModelID != "" {
						preExecLLM = fmt.Sprintf("%s/%s", executionAgent.GetConfig().LLMConfig.Primary.Provider, executionAgent.GetConfig().LLMConfig.Primary.ModelID)
					}
					fb := fmt.Sprintf("execution-attempt-%d-iteration-%d", retryAttempt, 0)
					hcpo.preSavePromptsJSON(stepIndex, step.GetID(), stepPath, "execution_only", preSystemPrompt, preUserMessage, preExecLLM, fb+"-prompts.json")
				}

				// Learn code mode: clean and pre-create the code/ subdirectory via workspace API.
				// Deleting all files ensures stale helpers from a previous LLM attempt don't accumulate.
				if isLearnCodeMode {
					codeDirRelPath := stepExecutionPath + "/code"
					// Delete all existing files in code/ (LLM may have written stale files on a prior attempt)
					if codeFiles, listErr := hcpo.ListWorkspaceFiles(ctx, codeDirRelPath); listErr == nil {
						for _, f := range codeFiles {
							fName := filepath.Base(f)
							if fName == "" || fName == "." {
								continue
							}
							if delErr := hcpo.DeleteWorkspaceFile(ctx, codeDirRelPath+"/"+fName); delErr != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to delete old %s from code/: %v", fName, delErr))
							}
						}
					}
					if mkErr := createFolderViaAPI(ctx, codeDirRelPath); mkErr != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to pre-create code/ dir for step %d: %v", stepIndex+1, mkErr))
					} else {
						hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] Pre-created code/ dir for step %d: %s", stepIndex+1, codeDirRelPath))
					}
				}

				// Execute execution-only agent with learning history (reused from learning reading above)
				executionResult, executionConversationHistory, err = executionAgent.Execute(ctx, templateVars, []llmtypes.MessageContent{})

				// Capture conversation history for callers that need it (e.g., get_sub_agent_conversation tool)
				if execCtx != nil && execCtx.ConversationHistoryCapture != nil {
					*execCtx.ConversationHistoryCapture = executionConversationHistory
				}

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

				if err != nil {
					// Execution errors
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step %d execution failed (attempt %d): %v", stepIndex+1, retryAttempt, err))

					// Save partial conversation history on failure/cancellation so users can inspect
					// tool responses from interrupted executions via debug_step or log files.
					if len(executionConversationHistory) > 0 {
						hcpo.GetLogger().Info(fmt.Sprintf("[PARTIAL-LOGS] Saving partial execution logs for step %d (%s) — %d conversation entries, error: %v", stepIndex+1, stepPath, len(executionConversationHistory), err))
						hcpo.saveExecutionConversationLogs(stepIndex, step.GetID(), stepPath, retryAttempt, 0,
							fmt.Sprintf("FAILED: %v", err), executionLLM, executionConversationHistory, executionAgent)
					} else {
						hcpo.GetLogger().Warn(fmt.Sprintf("[PARTIAL-LOGS] No conversation history to save for step %d (%s) — execution failed before any LLM turns", stepIndex+1, stepPath))
					}

					if retryAttempt >= maxRetryAttempts {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ Step %d execution failed after %d attempts, exiting retry loop", stepIndex+1, maxRetryAttempts), nil)
						break // Exit retry loop - will proceed to human feedback
					}
					continue // Retry on next attempt
				}

				hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d execution completed successfully (attempt %d)", stepIndex+1, retryAttempt))

				// Save execution logs (result, conversation history, system prompt)
				hcpo.saveExecutionConversationLogs(stepIndex, step.GetID(), stepPath, retryAttempt, 0,
					executionResult, executionLLM, executionConversationHistory, executionAgent)

				// Learn code mode: inner fix loop — run main.py and feed errors back as user messages
				// in the same conversation chain (no new agent, no system-prompt reset).
				if isLearnCodeMode {
					maxFixIter := 5
					if agentCfgs := getAgentConfigs(step); agentCfgs != nil && agentCfgs.LearnCodeMaxFixIter != nil {
						maxFixIter = *agentCfgs.LearnCodeMaxFixIter
					}
					codeDirAbsPath := filepath.Join(toAbsPath(stepExecutionPath), "code")
					mainPyPath := filepath.Join(codeDirAbsPath, "main.py")
					learnCodeRepairAgentUpgraded := false
					learnCodeRepairSystemPrompt := ""

					var lastLcResult *LearnCodeFastPathResult
					if selfRun := detectSuccessfulLLMLearnCodeSelfRun(executionConversationHistory, mainPyPath); selfRun != nil {
						preValResults, _ := RunPreValidation(ctx, getValidationSchema(step), stepExecutionPath, hcpo.BaseOrchestrator)
						if preValResults != nil && preValResults.OverallPass {
							learnCodePreValidationResultsOverride = preValResults
							lastLcResult = &LearnCodeFastPathResult{
								RanScript: true,
								Success:   true,
								ExitCode:  selfRun.ExitCode,
								Output:    selfRun.Output,
							}
							hcpo.GetLogger().Info(fmt.Sprintf("♻️ [learn_code] Reusing successful LLM self-test for step %d — skipping duplicate controller rerun", stepIndex+1))
							hcpo.emitLearnCodeScriptExecutionEvent(ctx, step, stepIndex, stepPath,
								mainPyPath, true, lastLcResult.ExitCode, lastLcResult.Output, "", 0, false)
						} else {
							hcpo.GetLogger().Info(fmt.Sprintf("🧪 [learn_code] LLM self-test detected for step %d, but current outputs did not pass pre-validation — running controller execution", stepIndex+1))
						}
					}

					for fixIter := 0; fixIter <= maxFixIter && (lastLcResult == nil || !lastLcResult.Success); fixIter++ {
						lastLcResult = hcpo.runLearnCodeMainPyFromExecution(ctx, step, stepIndex, stepPath, allSteps, stepExecutionPath, executionWorkspacePath)

						// Emit UI event only when a script was actually executed (not when main.py is simply absent).
						// The nil case (main.py not yet written) is internal control-flow — emitting a ✗ failed
						// event for it is misleading when the LLM writes the script on the next iteration.
						if lastLcResult != nil {
							hcpo.emitLearnCodeScriptExecutionEvent(ctx, step, stepIndex, stepPath,
								mainPyPath, lastLcResult.Success, lastLcResult.ExitCode, lastLcResult.Output, lastLcResult.Error,
								fixIter, false)
						}

						if lastLcResult != nil && lastLcResult.Success {
							break // script passed
						}
						if fixIter == maxFixIter {
							break // exhausted fix attempts
						}

						// Build feedback user message with the exact error
						var feedbackMsg string
						if lastLcResult == nil {
							feedbackMsg = fmt.Sprintf(
								"main.py was not found at %s/main.py.\n\nWrite the complete solution to main.py there. You may run it yourself to test, but the system will also execute it automatically after your turn.",
								codeDirAbsPath,
							)
						} else {
							feedbackMsg = fmt.Sprintf(
								"Your script failed (fix attempt %d/%d):\n```\n%s\n```\n\nFix the bug and rewrite main.py at %s/main.py.",
								fixIter+1, maxFixIter, lastLcResult.Error, codeDirAbsPath,
							)
						}
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] main.py failed for step %d (fix attempt %d/%d) — continuing conversation with error", stepIndex+1, fixIter+1, maxFixIter))

						if !learnCodeRepairAgentUpgraded {
							repairAgentName := fmt.Sprintf("%s-fix-%d-high", executionAgentName, fixIter+1)
							repairAgent, repairErr := hcpo.createExecutionOnlyAgent(ctx, "execution_only", stepPath, repairAgentName, agentConfigs, isRetryAfterValidationFailure, retryAttempt, step.GetID())
							if repairErr != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] failed to upgrade repair agent for step %d: %v", stepIndex+1, repairErr))
							} else {
								if repairCfg := repairAgent.GetConfig(); repairCfg != nil && repairCfg.LLMConfig.Primary.ModelID != "" {
									executionLLM = fmt.Sprintf("%s/%s", repairCfg.LLMConfig.Primary.Provider, repairCfg.LLMConfig.Primary.ModelID)
								}
								if repairEOA, ok := repairAgent.(*WorkflowExecutionOnlyAgent); ok {
									learnCodeRepairSystemPrompt = repairEOA.executionOnlySystemPromptProcessor(templateVars)
								}
								if executionAgent != nil {
									_ = executionAgent.Close()
								}
								executionAgent = repairAgent
								learnCodeRepairAgentUpgraded = true
								hcpo.GetLogger().Info(fmt.Sprintf("🔁 [learn_code] recreated repair agent for step %d using default tier selection: %s", stepIndex+1, executionLLM))
							}
						}

						// Continue the same conversation: inject error as a user turn
						if ba := executionAgent.GetBaseAgent(); ba != nil {
							systemPrompt := ""
							if learnCodeRepairSystemPrompt != "" {
								systemPrompt = learnCodeRepairSystemPrompt
								learnCodeRepairSystemPrompt = ""
							}
							_, executionConversationHistory, err = ba.Execute(ctx, feedbackMsg, executionConversationHistory, systemPrompt, false)
							if err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] agent error during fix attempt %d: %v", fixIter+1, err))
								break
							}
						} else {
							break
						}
					}

					if lastLcResult == nil || !lastLcResult.Success {
						var errMsg string
						if lastLcResult == nil {
							errMsg = fmt.Sprintf("main.py was never written to %s", codeDirAbsPath)
						} else {
							errMsg = fmt.Sprintf("main.py still failing after %d fix attempts:\n%s", maxFixIter, lastLcResult.Error)
						}
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] step %d failed after fix loop: %s", stepIndex+1, errMsg))
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: false,
							ExecutionStatus:      "FAILED",
							Reasoning:            errMsg,
						}
						if retryAttempt >= maxRetryAttempts {
							validationFailedAfterMaxRetries = true
						}
						continue
					}
					turnCount = len(executionConversationHistory)
					hcpo.GetLogger().Info(fmt.Sprintf("✅ [learn_code] main.py executed successfully for step %d — proceeding to validation", stepIndex+1))
				}

				// Run pre-validation (code-based structural checks) -- always active, independent of LLM validation.
				agentConfigs = getAgentConfigs(step)
				preValidationSchema := getValidationSchema(step)
				preValidationResults := learnCodePreValidationResultsOverride
				var preValidationErr error
				if preValidationResults == nil {
					preValidationResults, preValidationErr = RunPreValidation(ctx, preValidationSchema, stepExecutionPath, hcpo.BaseOrchestrator)
				}
				if preValidationErr != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("Pre-validation error for step %d: %v", stepIndex+1, preValidationErr))
					preValidationResults = &WorkspaceVerificationResult{
						OverallPass:  false,
						FilesChecked: []FileCheckResult{},
						Summary: ValidationSummary{
							TotalChecks:  0,
							PassedChecks: 0,
							FailedChecks: 1,
							SchemaErrors: 0,
							Errors: []ValidationError{{
								File:      "",
								Path:      "",
								CheckType: "pre_validation_error",
								Expected:  "pre-validation to run successfully",
								Actual:    "error occurred",
								Message:   fmt.Sprintf("Pre-validation failed to run: %v", preValidationErr),
							}},
							SchemaWarnings: []ValidationError{},
						},
					}
				} else if preValidationResults == nil && (preValidationSchema == nil || len(preValidationSchema.Files) == 0) {
					hcpo.GetLogger().Info(fmt.Sprintf("Pre-validation skipped for step %d (no validation schema)", stepIndex+1))
				}
				learnCodePreValidationResultsOverride = nil
				hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, stepPath, isBranchStep, preValidationResults)

				// Build validation response based on pre-validation results
				if !preValidationResults.OverallPass {
					hcpo.GetLogger().Warn(fmt.Sprintf("Pre-validation failed for step %d - rejecting", stepIndex+1))
					validationResponse = &ValidationResponse{
						IsSuccessCriteriaMet: false,
						ExecutionStatus:      "FAILED",
						Reasoning:            formatWorkspaceResults(preValidationResults) + "\n\nPre-validation failed - structural issues must be fixed before the step can complete.",
						Feedback: []ValidationFeedback{{
							Type:        "structural_validation",
							Description: "Pre-validation failed - output structure does not meet requirements",
							Severity:    "HIGH",
						}},
					}
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("Pre-validation passed for step %d - auto-approving", stepIndex+1))
					validationResponse = &ValidationResponse{
						IsSuccessCriteriaMet: true,
						ExecutionStatus:      "COMPLETED",
						Reasoning:            "Pre-validation passed - step auto-approved",
					}
					// Learn code mode: mark script for saving to learnings after the execution loop
					if isLearnCodeMode {
						learnCodeScriptNeedsSaving = true
					}
				}

				// LEARNING PHASE: Runs for ALL agents regardless of validation status
				// Validation being disabled does NOT prevent learning from running
				// Learning will run if: not disabled, not locked, and not skipped due to temp LLM override
				// LEARNING DISABLED: Skip learning agents entirely
				// Check step-specific learning detail level
				agentConfigs = getAgentConfigs(step)
				// Learning is disabled if explicitly set via step config, or if this is a routing step.
				// Routing steps are pure decision/evaluation logic — they don't produce learnable outcomes.
				isLearningDisabledStep := (agentConfigs != nil && agentConfigs.DisableLearning != nil && *agentConfigs.DisableLearning) || isRoutingStep(step)
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
				// SCRIPTED CODE MODE: keep main.py as executable truth, but still allow SKILL.md notes
				if isLearnCodeMode {
					hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted_code] Keeping learning enabled for step %d — main.py remains executable truth and SKILL.md can capture edge cases", stepIndex+1))
				}
				// HUMAN-ASSISTED LEARNING MODE: Skip all automatic learning — human triggers manually via generate_learnings
				if agentConfigs != nil && agentConfigs.LearningMode != "auto" {
					hcpo.GetLogger().Info(fmt.Sprintf("🧑‍🏫 Human-assisted learning mode: Skipping automatic learning for step %d (use generate_learnings to learn manually)", stepIndex+1))
					isLearningDisabled = true
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
				if isLearningDisabled || shouldSkipLearningDueToLock || shouldSkipLearningDueToTempOverride {
					if isLearningDisabled {
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
					// Pre-validation result drives validationResponse (set above).
					// Safety guard: if somehow nil, default to success so learning can run.
					if validationResponse == nil {
						hcpo.GetLogger().Info(fmt.Sprintf("Pre-validation result missing for step %d - defaulting to success for learning", stepIndex+1))
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: true,
							ExecutionStatus:      "COMPLETED",
							Reasoning:            "LLM validation disabled - step auto-approved for learning",
						}
					}

					// Run appropriate learning phase based on pre-validation result.
					// Pre-validation failure sets IsSuccessCriteriaMet=false, triggering failure learning.
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
							triggerReason := "Validation passed (success criteria met)"
							// Run success learning in background so next step can start immediately.
							// In workshop mode, register it as a tracked execution so the UI can show it
							// and stop_step/stop_all_executions can cancel it.
							if !hcpo.startTrackedSuccessLearningPhase(
								stepIndex,
								stepPath,
								learningPathIdentifier,
								totalSteps,
								todoStep,
								executionConversationHistory,
								validationResponse,
								isCodeExecutionMode,
								usedTempLLM,
								turnCount,
								executionLLM,
								triggerReason,
							) {
								go func() {
									bgCtx := context.Background()
									bgErr := hcpo.runSuccessLearningPhase(bgCtx, stepIndex, stepPath, learningPathIdentifier, totalSteps, todoStep, executionConversationHistory, validationResponse, isCodeExecutionMode, usedTempLLM, turnCount, executionLLM, triggerReason)
									if bgErr != nil {
										hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Success learning phase failed for %s: %v", stepPath, bgErr))
									} else {
										hcpo.GetLogger().Info(fmt.Sprintf("✅ Success learning analysis completed for %s", stepPath))
									}
								}()
							}
						}
					} else {
						// Failure Learning Agent - DISABLED: only trigger learnings on success
						hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Skipping failure learning for %s - failure learning disabled (learnings only trigger on success)", stepPath))
					}
				}

				// Check if success criteria was met
				// Check IsSuccessCriteriaMet instead of just ExecutionStatus - PARTIAL/INCOMPLETE can also mean criteria not met
				if validationResponse != nil && validationResponse.IsSuccessCriteriaMet {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d passed validation - success criteria met (Status: %s)", stepIndex+1, validationResponse.ExecutionStatus))

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

						if hcpo.fallbackToOriginalLLMOnFailure {
							hcpo.GetLogger().Info(fmt.Sprintf("🔄 Retrying step %d execution with validation feedback - next attempt will use original LLM (fallback enabled)", stepIndex+1))
						} else {
							hcpo.GetLogger().Info(fmt.Sprintf("🔄 Retrying step %d execution with validation feedback", stepIndex+1))
						}
						// Note: conversation history is preserved from previous attempts for context
						// Explicitly continue to next retry attempt
						continue
					}
				}
			} // End of retry loop

			// Exit immediately if validation failed after exhausting all retry attempts
			if validationFailedAfterMaxRetries {
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
				// Emit step_progress_updated (failed) event
				stepTitle := step.GetTitle()
				if stepTitle == "" {
					stepTitle = fmt.Sprintf("Step %d", stepIndex+1)
				}
				stepId := step.GetID()
				if stepId == "" {
					stepId = fmt.Sprintf("step-%d", stepIndex+1)
				}
				progress, loadErr := hcpo.loadStepProgress(ctx)
				if loadErr == nil && progress != nil {
					hcpo.emitStepProgressUpdatedEvent(ctx, progress, "failed", stepId, err.Error())
				}
				hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted step_progress_updated (failed) for step %d: %s (validation failed)", stepIndex+1, stepTitle))
				return executionResult, updatedContextFiles, err
			}

		} // End of main execution block

		// Learn code mode: save the newly written main.py to learnings (only when LLM generated it)
		if isLearnCodeMode && learnCodeScriptNeedsSaving {
			hcpo.saveLearnCodeScriptToLearnings(step, toAbsPath(stepExecutionPath))
			learnCodeScriptNeedsSaving = false
		}

		// BLOCKING HUMAN FEEDBACK - Ask user if they want to continue to next step
		// If user provides feedback (doesn't approve), stop workflow and ask user to manually update plan
		// SKIP HUMAN INPUT MODE: Skip human feedback but keep learning enabled
		// DECISION INNER STEP: Skip human feedback on success (decision step will handle routing)
		// SUB-AGENT: Never request human feedback (sub-agents run automatically)
		// NORMAL MODE: Always request human feedback before moving to next step
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
		} else if hcpo.runSingleStepOnly {
			// Single-step mode (workshop / run-single-step UI): no next step to continue with
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Single-step mode: Auto-approving step %d without human feedback (no next step)", stepIndex+1))
			approved = true
			feedback = ""
		} else if isSkipHumanInput {
			hcpo.GetLogger().Info(fmt.Sprintf("⚡ Skip human input mode: Auto-approving step %d without human feedback (learning will still run)", stepIndex+1))
			approved = true
			feedback = "" // No feedback in skip human input mode
		} else {
			// Auto-approve: no human feedback between steps — execution continues automatically
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d/%d completed — auto-approving (no inter-step human feedback)", stepIndex+1, totalSteps))
			approved = true
			feedback = ""
		}

		// Store human feedback for future steps (even if approved, user might have provided guidance)
		// Note: humanFeedbackHistory is not available in this function scope, so we skip storing it
		// It will be handled by the caller if needed

		if approved {
			// Write step_done.json marker file (for both regular and branch steps)
			stepDonePath := filepath.Join(stepExecutionPath, "step_done.json")
			stepDoneData := map[string]interface{}{
				"completed_at": time.Now().UTC().Format(time.RFC3339),
				"step_index":   stepIndex,
				"step_path":    stepPath,
				"step_id":      step.GetID(),
			}
			if jsonBytes, err := json.MarshalIndent(stepDoneData, "", "  "); err == nil {
				if err := hcpo.WriteWorkspaceFile(ctx, stepDonePath, string(jsonBytes)); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write step_done.json: %v", err))
				}
			}

			// User approved - mark step as completed and exit outer loop
			// Only update progress if this is not a branch step
			if !isBranchStep {
				hcpo.addCompletedStepIndex(progress, stepIndex)
				// Always save progress after marking a step as completed (both fast and normal mode)
				if err := hcpo.saveStepProgress(ctx, progress); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save step progress: %v", err))
				} else {
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d/%d marked as completed and saved - Total completed: %d/%d", stepIndex+1, totalSteps, len(progress.CompletedStepIndices), progress.TotalSteps))
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

// isHumanInputStep returns true if the step is a human input step (asks question and blocks for input)
func isHumanInputStep(step PlanStepInterface) bool {
	_, ok := step.(*HumanInputPlanStep)
	return ok
}

// isTodoTaskStep returns true if the step is a todo task step (orchestrator with todo list management)
func isTodoTaskStep(step PlanStepInterface) bool {
	_, ok := step.(*TodoTaskPlanStep)
	return ok
}

// isRoutingStep returns true if the step is a routing step (N-way LLM-based routing)
func isRoutingStep(step PlanStepInterface) bool {
	_, ok := step.(*RoutingPlanStep)
	return ok
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
	case *TodoTaskPlanStep:
		return s.AgentConfigs
	case *HumanInputPlanStep:
		return s.AgentConfigs
	case *EvaluationStep:
		return s.AgentConfigs
	case *RoutingPlanStep:
		return s.AgentConfigs
	default:
		return nil
	}
}

// getValidationSchema returns ValidationSchema from a PlanStepInterface
func getValidationSchema(step PlanStepInterface) *ValidationSchema {
	return step.GetValidationSchema()
}

var _ = getRegularPlanStep

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

					// Archive execution folders for target step and all subsequent steps
					// This preserves execution artifacts for debugging while allowing clean re-execution
					runNumber := hcpo.getNextArchivalRunNumber(ctx, progress, targetStepIndex+1)
					archivedCount := 0
					for stepNum := targetStepIndex + 1; stepNum <= len(breakdownSteps); stepNum++ {
						if err := hcpo.archiveStepExecutionFolder(ctx, stepNum, runNumber); err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive execution folder for step %d: %v (continuing)", stepNum, err))
						} else {
							archivedCount++
							hcpo.GetLogger().Info(fmt.Sprintf("📦 Archived execution folder for step %d to run-%d", stepNum, runNumber))
						}
					}
					if archivedCount > 0 {
						hcpo.GetLogger().Info(fmt.Sprintf("✅ Archived execution folders for %d steps (step-%d to step-%d) to run-%d", archivedCount, targetStepIndex+1, len(breakdownSteps), runNumber))
					}
					// Save updated archival counts
					if err := hcpo.saveStepProgress(ctx, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save archival counts: %v", err))
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

		// Check if this is a routing step
		if isRoutingStep(step) {
			hcpo.GetLogger().Info(fmt.Sprintf("🔀 Starting routing step execution: %s", step.GetTitle()))
			selectedRouteID, _, err := hcpo.executeRoutingStep(ctx, step, i, progress, previousContextFiles, iteration, execCtx, breakdownSteps, previousExecutionResults)
			if err != nil {
				if strings.Contains(err.Error(), "WORKFLOW_END") {
					hcpo.GetLogger().Info(fmt.Sprintf("🏁 Routing step %d signaled workflow termination - ending workflow", i+1))
					hcpo.addCompletedStepIndex(progress, i)
					if err := hcpo.saveStepProgress(ctx, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after routing step termination: %v", err))
					}
					break
				}
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Routing step %d execution failed: %v", i+1, err), nil)
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "routing-step-execution", fmt.Sprintf("Execute routing step: %s", step.GetTitle()), err.Error(), i, iteration)
				return fmt.Errorf("routing step %d execution failed: %w", i+1, err)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Routing step %d completed: selected route %s", i+1, selectedRouteID))

			// Mark step as completed
			hcpo.addCompletedStepIndex(progress, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after routing step: %v", err))
			}

			// Check single step mode
			if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
				hcpo.GetLogger().Info(fmt.Sprintf("🎯 Single step mode: completed target step %d, stopping execution", i+1))
				hcpo.SetRunSingleStepMode(false, -1)
				break
			}

			// Find next step based on selected route
			var nextStepID string
			if routingStep, ok := step.(*RoutingPlanStep); ok {
				for _, route := range routingStep.Routes {
					if route.RouteID == selectedRouteID {
						nextStepID = route.NextStepID
						break
					}
				}
			}

			// Track routing evaluations to prevent infinite loops (reuse DecisionEvaluationCounts)
			if progress.DecisionEvaluationCounts == nil {
				progress.DecisionEvaluationCounts = make(DecisionEvaluationCount)
			}
			routingKey := fmt.Sprintf("%s:%s", step.GetID(), selectedRouteID)
			currentCount := progress.DecisionEvaluationCounts[routingKey]
			newCount := currentCount + 1
			progress.DecisionEvaluationCounts[routingKey] = newCount
			hcpo.GetLogger().Info(fmt.Sprintf("📊 Routing evaluation count for %s: %d", routingKey, newCount))

			if newCount > 2 {
				errorMsg := fmt.Sprintf("infinite loop detected: routing step '%s' (ID: %s) has selected route %s %d times", step.GetTitle(), step.GetID(), selectedRouteID, newCount)
				hcpo.GetLogger().Error(errorMsg, nil)
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "routing-step-loop-detection", fmt.Sprintf("Routing step: %s", step.GetTitle()), errorMsg, i, iteration)
				return fmt.Errorf("workflow error: %s", errorMsg)
			}

			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after routing evaluation: %v", err))
			}

			// Handle next step navigation
			if nextStepID == "end" {
				hcpo.GetLogger().Info(fmt.Sprintf("🏁 Routing step %d specified 'end' - terminating workflow", i+1))
				break
			} else if nextStepID != "" {
				targetStepIndex := -1
				for idx, s := range breakdownSteps {
					if s.GetID() == nextStepID {
						targetStepIndex = idx
						break
					}
				}
				if targetStepIndex >= 0 {
					hcpo.GetLogger().Info(fmt.Sprintf("🔗 Routing to step %d (ID: %s)", targetStepIndex+1, nextStepID))

					if err := hcpo.cleanupProgressFromStep(ctx, targetStepIndex, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup progress from step %d: %v", targetStepIndex+1, err))
					}

					runNumber := hcpo.getNextArchivalRunNumber(ctx, progress, targetStepIndex+1)
					for stepNum := targetStepIndex + 1; stepNum <= len(breakdownSteps); stepNum++ {
						if err := hcpo.archiveStepExecutionFolder(ctx, stepNum, runNumber); err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive execution folder for step %d: %v", stepNum, err))
						}
					}
					if err := hcpo.saveStepProgress(ctx, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save archival counts: %v", err))
					}

					if targetStepIndex < startFromStep {
						startFromStep = targetStepIndex
					}

					i = targetStepIndex - 1
					continue
				} else {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Target step ID '%s' not found in plan - defaulting to next sequential step", nextStepID))
				}
			}

			continue
		}

		// Check if this is a todo task step
		if isTodoTaskStep(step) {
			// Execute todo task step - manages todo list and delegates to sub-agents
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Starting todo task step execution: %s", step.GetTitle()))
			// Generate step path for todo task step
			todoTaskStepPath := fmt.Sprintf("step-%d", i+1)

			// Check if this todo task step has decision context (routed from a decision step)
			var todoTaskDecisionCtx *DecisionContext
			if dc, exists := decisionContextMap[i]; exists {
				todoTaskDecisionCtx = dc
				// Clean up after use (optional, but good practice)
				delete(decisionContextMap, i)
				hcpo.GetLogger().Info(fmt.Sprintf("📝 Using decision context for todo task step %d (routed from decision step %d)", i+1, dc.DecisionStepIndex+1))
			}

			successCriteriaMet, nextStepID, err := hcpo.executeTodoTaskStep(ctx, step, i, progress, previousContextFiles, previousExecutionResults, iteration, execCtx, breakdownSteps, todoTaskStepPath, todoTaskDecisionCtx)
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Todo task step %d execution failed: %v", i+1, err), nil)
				// Emit error event using centralized method
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "todo-task-step-execution", fmt.Sprintf("Execute todo task step: %s", step.GetTitle()), err.Error(), i, iteration)
				return fmt.Errorf("todo task step %d execution failed: %w", i+1, err)
			}

			hcpo.GetLogger().Info(fmt.Sprintf("✅ Todo task step %d completed successfully: %s (SuccessCriteriaMet: %t)", i+1, step.GetTitle(), successCriteriaMet))

			// Mark todo task step as completed
			hcpo.addCompletedStepIndex(progress, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after todo task step: %v", err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved progress: todo task step %d marked as completed", i+1))
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
				hcpo.GetLogger().Info(fmt.Sprintf("🏁 Todo task step %d specified 'end' - terminating workflow", i+1))
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

					// Clean up progress and execution folders for target step and subsequent steps
					if err := hcpo.cleanupProgressFromStep(ctx, targetStepIndex, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to cleanup progress from step %d: %v (continuing anyway)", targetStepIndex+1, err))
					}

					// Archive execution folders for target step and all subsequent steps
					// This preserves execution artifacts for debugging while allowing clean re-execution
					runNumber := hcpo.getNextArchivalRunNumber(ctx, progress, targetStepIndex+1)
					for stepNum := targetStepIndex + 1; stepNum <= len(breakdownSteps); stepNum++ {
						if err := hcpo.archiveStepExecutionFolder(ctx, stepNum, runNumber); err != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to archive execution folder for step %d: %v (continuing)", stepNum, err))
						}
					}
					// Save updated archival counts
					if err := hcpo.saveStepProgress(ctx, progress); err != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save archival counts: %v", err))
					}

					// Update startFromStep to allow execution from target step
					if targetStepIndex < startFromStep {
						startFromStep = targetStepIndex
					}

					// Set loop index to jump to target step (subtract 1 because loop will increment)
					i = targetStepIndex - 1
					continue
				} else {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Target step ID '%s' not found in plan - defaulting to next sequential step", nextStepID))
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
			stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, step.GetID(), stepPath)
			responseFilePath := filepath.Join(stepExecutionPath, resolvedContextOutput)

			var executionResult string
			responseContent, err := hcpo.ReadWorkspaceFile(ctx, responseFilePath)
			if err == nil {
				// Parse JSON to extract response
				var responseData map[string]interface{}
				if err := json.Unmarshal([]byte(responseContent), &responseData); err == nil {
					if response, ok := responseData["response"].(string); ok {
						executionResult = response
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
		// Allow workshop inner steps to use a custom step path (e.g., "step-3-sub-login-expert")
		// so they don't collide with top-level step folders.
		if execCtx.StepPathOverride != "" && execCtx.RunSingleStepOnly && i == execCtx.SingleStepTarget {
			hcpo.GetLogger().Info(fmt.Sprintf("[STEP-PATH] Overriding step path from %q to %q (inner step, target=%d)", stepPath, execCtx.StepPathOverride, i))
			stepPath = execCtx.StepPathOverride
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("[STEP-PATH] Using default step path %q for step index %d (override=%q, singleStep=%v, target=%d)",
				stepPath, i, execCtx.StepPathOverride, execCtx.RunSingleStepOnly, execCtx.SingleStepTarget))
		}
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
			breakdownSteps,           // allSteps
			false,                    // isDecisionInnerStep = false (regular step)
			decisionCtx,              // decisionContext - nil if not routed from decision step
			"",                       // decisionEvaluationQuestion - empty for regular steps
			false,                    // isSubAgent = false (regular step)
			previousExecutionResults, // Execution outputs from previous steps
			nil,                      // orchestrationRoutes - nil for regular steps (not sub-agents)
		)
		if err != nil {
			hcpo.GetLogger().Error(fmt.Sprintf("❌ Step %d execution failed: %v", i+1, err), nil)
			// Emit step_progress_updated (failed) event
			stepTitle := step.GetTitle()
			if stepTitle == "" {
				stepTitle = fmt.Sprintf("Step %d", i+1)
			}
			stepId := step.GetID()
			if stepId == "" {
				stepId = fmt.Sprintf("step-%d", i+1)
			}
			progress, loadErr := hcpo.loadStepProgress(ctx)
			if loadErr == nil && progress != nil {
				hcpo.emitStepProgressUpdatedEvent(ctx, progress, "failed", stepId, err.Error())
			}
			hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted step_progress_updated (failed) for step %d: %s", i+1, stepTitle))
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
// Returns a file-path reference string (not full content) and any error.
// The execution agent can read the files on demand via execute_shell_command.
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
		// Build file path reference instead of embedding full content
		docsRoot := GetPromptDocsRoot()
		absLearningsPath := filepath.Join(docsRoot, hcpo.GetWorkspacePath(), stepLearningsPath)

		// List the available files
		var fileList []string
		for filename := range learningFiles {
			fileList = append(fileList, filename)
		}
		sort.Strings(fileList)

		formattedLearningHistory = fmt.Sprintf(
			"📚 **Skill files available** at `%s/` (%d files: %s). Read these files with execute_shell_command before starting — they contain validated workflows, error recovery patterns, and code from previous runs.",
			absLearningsPath, len(learningFiles), strings.Join(fileList, ", "))
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Found %d learning file(s) from step folder for %s (path reference only)", len(learningFiles), stepPath))
	} else {
		// No learning files found
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ No learning files found for %s - learnings folder is empty: %s", stepPath, stepLearningsPath))
		formattedLearningHistory = ""
	}

	return formattedLearningHistory, nil
}
