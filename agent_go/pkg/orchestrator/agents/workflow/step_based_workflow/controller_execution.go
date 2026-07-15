package step_based_workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/common"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator"
	"github.com/manishiitg/coding-agent-loop/agent_go/pkg/orchestrator/agents"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// KnowledgebaseFolderName is the name of the persistent knowledgebase folder at workspace root
// This folder is never deleted during cleanup operations and is shared across all runs
const KnowledgebaseFolderName = "knowledgebase"

// KnowledgebaseContextFolderName stores user-supplied runtime business context.
// It is read by steps when knowledgebase_access grants read, but excluded from
// automatic KB note reorganization/consolidation because the content is user-owned.
const KnowledgebaseContextFolderName = "context"

// DBFolderName is the name of the persistent structured-data folder at workspace root.
// Always created on workspace init (no preset toggle, unlike knowledgebase). All regular steps
// get read+write by default. Evaluation steps get read always, write only if DBWrite: true
// on the step. State lives in db/db.sqlite (one table per entity); steps should upsert via
// INSERT ... ON CONFLICT rather than recreate tables. See docs/workflow/persistent_stores_design.md section 1.
const DBFolderName = "db"

// DBAssetsFolderName stores durable binary/media assets referenced by db rows,
// reports, or later steps. Keep metadata in a db/db.sqlite table and the actual files here.
const DBAssetsFolderName = "assets"

// getKnowledgebasePath returns the full path to the knowledgebase folder
// Path format: {workspaceRoot}/knowledgebase/
// Knowledgebase is at workspace root level (same as runs/, planning/, learnings/) to be shared across all runs
func getKnowledgebasePath(workspaceRoot string) string {
	return fmt.Sprintf("%s/%s", workspaceRoot, KnowledgebaseFolderName)
}

// getDBPath returns the full path to the db folder.
// Path format: {workspaceRoot}/db/
func getDBPath(workspaceRoot string) string {
	return fmt.Sprintf("%s/%s", workspaceRoot, DBFolderName)
}

// SoulFolderName is the name of the builder's long-term memory folder at workspace root.
// Contains soul.md — a free-form markdown file maintained by the workflow interactive builder
// across sessions. Not read or written by execution, evaluation, learning, or KB agents.
// Kept outside planning/ because planning/ is read-only to the builder (modifications go
// through typed plan-mod tools); soul.md is updated by shell write directly.
// See docs/workflow/persistent_stores_design.md section 8.
const SoulFolderName = "soul"

// Standard workflow subfolder names. Kept as constants so every site that needs to
// reference them (folder-guard allow-lists, prompt paths, pre-creation scaffolds)
// goes through a single source. When adding a new workflow subfolder, update the
// constant AND WorkflowWritableSubfolders below — skipping either half is what
// produced the reports/ + db/ + soul/ drift bug previously.
const (
	ReportsFolderName   = "reports"
	PlanningFolderName  = "planning"
	ExecutionFolderName = "execution"
	LearningsFolderName = "learnings"
	ScriptsFolderName   = "scripts"
	RunsFolderName      = "runs"
)

// WorkflowWritableSubfolders is the canonical list of subfolders under a workflow's
// root directory that any workflow-scoped agent session should be able to write.
// Trailing slashes are included so the list can drop directly into folder-guard
// allow-list construction (prefix-match behavior is slash-sensitive in several
// callers). Consumed by server.go's chat-agent / phase-chat / delegation setup
// paths — DO NOT duplicate this list in those callers.
//
// `planning/` is deliberately EXCLUDED. plan.json / step_config.json /
// workflow_layout.json are managed through typed plan-mod tools that serialize
// the full struct to JSON; raw writes would corrupt them. isProtectedPlanningPath
// (base_orchestrator_folder_guard.go) is the runtime backstop for custom tools,
// but the cleanest enforcement is keeping planning/ off this list entirely.
var WorkflowWritableSubfolders = []string{
	KnowledgebaseFolderName + "/",
	DBFolderName + "/",
	SoulFolderName + "/",
	ReportsFolderName + "/",
	ExecutionFolderName + "/",
	LearningsFolderName + "/",
	ScriptsFolderName + "/",
	RunsFolderName + "/",
}

// SoulFileName is the fixed markdown filename inside soul/.
const SoulFileName = "soul.md"

// getSoulPath returns the full path to the soul folder.
// Path format: {workspaceRoot}/soul/
//
//nolint:unused // reserved for the upcoming soul-store read/write helpers.
func getSoulPath(workspaceRoot string) string {
	return fmt.Sprintf("%s/%s", workspaceRoot, SoulFolderName)
}

// getSoulFilePath returns the full path to soul/soul.md.
//
//nolint:unused // reserved for the upcoming soul-store read/write helpers.
func getSoulFilePath(workspaceRoot string) string {
	return fmt.Sprintf("%s/%s/%s", workspaceRoot, SoulFolderName, SoulFileName)
}

// Knowledgebase access modes — see docs/workflow/persistent_stores_design.md section 3.
const (
	KBAccessReadWrite = "read-write"
	KBAccessRead      = "read"
	KBAccessWrite     = "write"
	KBAccessNone      = "none"
)

// resolveKnowledgebaseAccess resolves the effective KB access mode for a step.
//
// Policy: KB access is opt-in per step. Default is "none" — a step only gets KB read
// or write when knowledgebase_access is explicitly set on its step_config.json entry.
// The preset-level UseKnowledgebase flag is a prerequisite (when off, all steps are
// forced to "none" regardless of explicit setting); it controls whether knowledgebase/
// exists at all, not whether any given step can touch it.
func resolveKnowledgebaseAccess(stepConfig *AgentConfigs, presetEnabled bool) string {
	if !presetEnabled {
		return KBAccessNone
	}
	if stepConfig != nil && stepConfig.KnowledgebaseAccess != "" {
		switch stepConfig.KnowledgebaseAccess {
		case KBAccessRead, KBAccessWrite, KBAccessReadWrite, KBAccessNone:
			return stepConfig.KnowledgebaseAccess
		}
	}
	return KBAccessNone
}

// DB access modes. Unlike KB, db/ is read+write by DEFAULT for every execution step
// (the workflow's structured state surface) — "read-write" is the back-compat default
// when db_access is empty. "read" is opt-in least-privilege: db/ stays readable but is
// stripped from the step's write paths (mirrors the eval-step db_write opt-out).
const (
	DBAccessReadWrite = "read-write"
	DBAccessRead      = "read"
)

// resolveDBAccess returns the effective db/ access mode for a step. Empty / unknown →
// "read-write" (default), so existing plans are unchanged. Only an explicit "read"
// downgrades the step to read-only db.
func resolveDBAccess(stepConfig *AgentConfigs) string {
	if stepConfig != nil && stepConfig.DBAccess == DBAccessRead {
		return DBAccessRead
	}
	return DBAccessReadWrite
}

// kbAccessAllowsRead reports whether the given access mode grants read access.
func kbAccessAllowsRead(mode string) bool {
	return mode == KBAccessRead || mode == KBAccessReadWrite
}

// kbContributionForPrompt returns the step's knowledgebase_contribution string
// safely — nil step config and unset field both map to the empty string.
func kbContributionForPrompt(stepConfig *AgentConfigs) string {
	if stepConfig == nil {
		return ""
	}
	return stepConfig.KnowledgebaseContribution
}

// kbAccessAllowsWrite reports whether the mode is eligible for KB contribution.
// This gates whether KB writes happen at all for the step; the mechanism
// (post-step agent vs step-level direct upserts) is picked separately by
// resolveKnowledgebaseWriteMethod.
func kbAccessAllowsWrite(mode string) bool {
	return mode == KBAccessWrite || mode == KBAccessReadWrite
}

// Knowledgebase write methods — picked per step, only meaningful when
// kbAccessAllowsWrite(kbAccess) is true.
const (
	// KBWriteMethodAgent is the default: the step executes, and the post-step KB
	// update agent (controller_kb_update.go) reads the step's tool trail plus its
	// knowledgebase_contribution and merges observations into the relevant notes/
	// topic file(s).
	KBWriteMethodAgent = "agent"
	// KBWriteMethodDirect gives the step agent itself direct write authority:
	// notes/ is added to its folder-guard writePaths and it writes narrative via
	// shell + diff_patch_workspace_file inline, with a post-completion self-review
	// turn. The post-step KB update agent does NOT run for steps using this method.
	KBWriteMethodDirect = "direct"
)

// resolveKnowledgebaseWriteMethod returns which mechanism writes KB for the step.
// Only consulted when kbAccessAllowsWrite reports true for the effective access mode.
// Default when unset is "agent" so every existing workflow keeps current behavior.
func resolveKnowledgebaseWriteMethod(stepConfig *AgentConfigs) string {
	if stepConfig == nil {
		return KBWriteMethodAgent
	}
	switch stepConfig.KnowledgebaseWriteMethod {
	case KBWriteMethodDirect:
		return KBWriteMethodDirect
	case KBWriteMethodAgent, "":
		return KBWriteMethodAgent
	default:
		return KBWriteMethodAgent
	}
}

// kbAccessLabel returns a human-readable label for prompt display.
func kbAccessLabel(mode string) string {
	switch mode {
	case KBAccessReadWrite:
		return "READ/WRITE"
	case KBAccessRead:
		return "READ"
	case KBAccessWrite:
		return "WRITE"
	default:
		return "NONE"
	}
}

// isValidationFailure checks if validation failed (triggers human feedback)
// Returns true only if ExecutionStatus is "FAILED"
// Does NOT trigger on PARTIAL, COMPLETED, or INCOMPLETE status
//
//nolint:unused // retained for follow-up validation routing cleanup.
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

const maxInlineFileSize = 15 * 1024  // 15 KB — inline small text files into LLM prompt
const maxTotalInlineSize = 50 * 1024 // 50 KB — total budget across all inlined deps

// isLikelyTextContent checks if content is text (not binary) by scanning for null bytes.
func isLikelyTextContent(content string) bool {
	checkLen := len(content)
	if checkLen > 512 {
		checkLen = 512
	}
	for i := 0; i < checkLen; i++ {
		if content[i] == 0 {
			return false
		}
	}
	return true
}

func formatFileSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	kb := float64(bytes) / 1024.0
	if kb < 1024 {
		return fmt.Sprintf("%.1f KB", kb)
	}
	mb := kb / 1024.0
	return fmt.Sprintf("%.1f MB", mb)
}

// formatContextDependenciesWithContent resolves context dependency paths and inlines
// small text file contents directly into the prompt. Large or binary files are listed
// as paths only. This saves the LLM one tool call per inlined file.
func (hcpo *StepBasedWorkflowOrchestrator) formatContextDependenciesWithContent(
	ctx context.Context,
	resolvedContextDeps []string,
	docsRoot string,
) (string, error) {
	if len(resolvedContextDeps) == 0 {
		return "", nil
	}
	var sb strings.Builder
	totalInlined := 0
	for i, absPath := range resolvedContextDeps {
		if i > 0 {
			sb.WriteString("\n")
		}
		if !filepath.IsAbs(absPath) {
			sb.WriteString(fmt.Sprintf("**File**: `%s`\n", absPath))
			continue
		}
		relPath := strings.TrimPrefix(absPath, docsRoot+"/")
		content, readErr := hcpo.ReadWorkspaceFile(ctx, relPath)
		if readErr != nil {
			return "", fmt.Errorf(
				"input file not found: %s\n(produced by a prior step — check that the previous step completed successfully)", absPath)
		}

		contentLen := len(content)
		if contentLen <= maxInlineFileSize && totalInlined+contentLen <= maxTotalInlineSize && isLikelyTextContent(content) {
			totalInlined += contentLen
			sb.WriteString(fmt.Sprintf("**File**: `%s` (inlined, %s)\n<content>\n%s\n</content>\n", absPath, formatFileSize(contentLen), content))
		} else {
			sb.WriteString(fmt.Sprintf("**File**: `%s` (%s — read via tool)\n", absPath, formatFileSize(contentLen)))
		}
	}
	return sb.String(), nil
}

func getArtifactFolderName(stepID string, stepPath string) string {
	stepID = strings.TrimSpace(stepID)
	if stepID != "" {
		return stepID
	}
	return stepPath
}

func getExecutionArtifactFolderOverride(execCtx *ExecutionContext) string {
	if execCtx == nil {
		return ""
	}
	return strings.TrimSpace(execCtx.ArtifactFolderNameOverride)
}

func getExecutionArtifactIdentity(stepID string, stepPath string, execCtx *ExecutionContext) (string, string) {
	if override := getExecutionArtifactFolderOverride(execCtx); override != "" {
		return "", override
	}
	return stepID, stepPath
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
// - workspace bridge/shell views used by LLM agents
// - report/file panels that read from the workspace API
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
	return "http://127.0.0.1:8081"
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
// Creates folder via Workspace API only (ensures consistency with workspace listings).
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

// getLearningFolderPathByStepID returns the RELATIVE learning folder path using step ID.
// All steps (regular, branch, sub-agent, evaluation) share the "learnings/{stepID}/"
// namespace; validateCrossPlanStepIDUniqueness guarantees no collision between
// plan.json and evaluation_plan.json step IDs.
// Returns a RELATIVE path for use with workspace functions — they auto-prepend workspacePath.
// baseWorkspacePath and isEvaluationMode are retained for call-site compatibility.
func getLearningFolderPathByStepID(baseWorkspacePath string, stepID string, stepPath string, isEvaluationMode bool) string {
	_ = baseWorkspacePath
	_ = stepPath
	_ = isEvaluationMode
	return fmt.Sprintf("learnings/%s", stepID)
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

// getEffectiveLearningPathIdentifier returns the step ID for metadata tracking.
// The learning agent always writes to the _global skill folder (controlled by the template),
// but metadata is tracked per step for clarity.
func getEffectiveLearningPathIdentifier(stepID string, stepPath string, agentConfigs *AgentConfigs) string {
	return stepID
}

// executeConditionalStep is now in controller_conditional.go

// saveExecutionConversationLogs saves execution result, conversation history, and prompts to log files.
// Called on both success and failure/cancellation paths so partial conversations from interrupted
// executions can be inspected via debug_step or direct log file access.
// Uses context.Background() internally so writes succeed even when the caller's context is canceled.
func (hcpo *StepBasedWorkflowOrchestrator) saveExecutionConversationLogs(
	stepIndex int, stepID string, stepPath string, retryAttempt int, loopIterationCount int,
	executionResult string, executionLLM string,
	conversationHistory []llmtypes.MessageContent,
	executionAgent agents.OrchestratorAgent,
	toolCalls []orchestrator.ToolCallEntry,
	llmCalls []orchestrator.LLMCallEntry,
	attemptStartedAt time.Time,
	attemptCompletedAt time.Time,
	attemptDuration time.Duration,
) error {
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
	if attemptCompletedAt.IsZero() {
		attemptCompletedAt = time.Now().UTC()
	}
	if attemptStartedAt.IsZero() {
		attemptStartedAt = attemptCompletedAt.Add(-attemptDuration)
	}
	toolTiming := normalizeToolTimingEntries(toolCalls, attemptStartedAt)
	llmTiming := normalizeLLMTimingEntries(llmCalls, attemptStartedAt)
	agentName := ""
	if executionAgent != nil && executionAgent.GetBaseAgent() != nil {
		agentName = executionAgent.GetBaseAgent().GetName()
	}
	traceSpans, timingBreakdown := buildTimingTrace(stepID, agentName, executionLLM, attemptStartedAt, attemptCompletedAt, attemptDuration, llmTiming, toolTiming)
	timingData := map[string]interface{}{
		"schema_version": 2,
		"step_index":     stepIndex + 1,
		"step_id":        stepID,
		"step_path":      stepPath,
		"retry_attempt":  retryAttempt,
		"loop_iteration": loopIterationCount,
		"run_folder":     hcpo.selectedRunFolder,
		"agent": map[string]interface{}{
			"name":                          agentName,
			"model":                         executionLLM,
			"started_at":                    formatRFC3339UTC(attemptStartedAt),
			"completed_at":                  formatRFC3339UTC(attemptCompletedAt),
			"duration_ns":                   int64(attemptDuration),
			"duration_ms":                   durationToMillis(attemptDuration),
			"llm_call_count":                llmTiming.Count,
			"llm_duration_ms":               llmTiming.TotalDurationMs,
			"llm_time_to_first_response_ms": llmTiming.TimeToFirstResponseMs,
		},
		"llm":         llmTiming,
		"tools":       toolTiming,
		"trace_spans": traceSpans,
		"breakdown":   timingBreakdown,
	}

	// Save execution result
	resultPath := fmt.Sprintf("%s/%s.json", logDir, filenameBase)
	resultData := map[string]interface{}{
		"step_index":                    stepIndex + 1,
		"step_id":                       stepID,
		"step_path":                     stepPath,
		"retry_attempt":                 retryAttempt,
		"loop_iteration":                loopIterationCount,
		"execution_result":              executionResult,
		"model":                         executionLLM,
		"started_at":                    formatRFC3339UTC(attemptStartedAt),
		"completed_at":                  formatRFC3339UTC(attemptCompletedAt),
		"duration_ms":                   durationToMillis(attemptDuration),
		"duration_ns":                   int64(attemptDuration),
		"llm_call_count":                llmTiming.Count,
		"llm_duration_ms":               llmTiming.TotalDurationMs,
		"llm_time_to_first_response_ms": llmTiming.TimeToFirstResponseMs,
		"tool_call_count":               toolTiming.Count,
		"tool_duration_ms":              toolTiming.TotalDurationMs,
		"tracked_union_duration_ms":     timingBreakdown.TrackedUnionDurationMs,
		"untracked_duration_ms":         timingBreakdown.UntrackedDurationMs,
		"total_input_tokens":            timingBreakdown.TotalInputTokens,
		"total_output_tokens":           timingBreakdown.TotalOutputTokens,
		"total_tokens":                  timingBreakdown.TotalTokens,
		"tool_args_bytes":               timingBreakdown.ToolArgsBytes,
		"tool_result_bytes":             timingBreakdown.ToolResultBytes,
		"timing":                        timingData,
		"timestamp":                     attemptCompletedAt.Format(time.RFC3339),
	}
	resultJSON, err := json.MarshalIndent(resultData, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal required execution result %s: %w", resultPath, err)
	}
	if err := hcpo.WriteWorkspaceFile(saveCtx, resultPath, string(resultJSON)); err != nil {
		return fmt.Errorf("write required execution result %s: %w", resultPath, err)
	}

	// Save conversation history
	convPath := fmt.Sprintf("%s/%s-conversation.json", logDir, filenameBase)
	convData := map[string]interface{}{
		"step_index":           stepIndex + 1,
		"step_id":              stepID,
		"step_path":            stepPath,
		"retry_attempt":        retryAttempt,
		"loop_iteration":       loopIterationCount,
		"conversation_history": conversationHistory,
		"llm_calls":            llmCalls,
		"tool_calls":           toolCalls,
		"llm_call_count":       llmTiming.Count,
		"tool_call_count":      toolTiming.Count,
		"timing":               timingData,
		"timestamp":            attemptCompletedAt.Format(time.RFC3339),
	}
	if handle := currentAgentSessionHandle(executionAgent); handle != nil {
		convData["agent_session_handle"] = handle
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
		"step_id":       stepID,
		"step_path":     stepPath,
		"system_prompt": capturedSystemPrompt,
		"saved_at":      "post_execution",
		"timestamp":     attemptCompletedAt.Format(time.RFC3339),
	}
	if promptsJSON, err := json.MarshalIndent(promptsData, "", "  "); err == nil {
		if err := hcpo.WriteWorkspaceFile(saveCtx, promptsPath, string(promptsJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write prompts to %s: %v", promptsPath, err))
		}
	}

	timingPath := fmt.Sprintf("%s/%s-timing.json", logDir, filenameBase)
	if timingJSON, err := json.MarshalIndent(timingData, "", "  "); err == nil {
		if err := hcpo.WriteWorkspaceFile(saveCtx, timingPath, string(timingJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write timing summary to %s: %v", timingPath, err))
		}
	}

	hcpo.GetLogger().Info(fmt.Sprintf("💾 Saved execution logs for step %d (%s) attempt %d", stepIndex+1, stepPath, retryAttempt))
	return nil
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
	var plannedStep PlanStepInterface
	if hcpo.approvedPlan != nil && stepNumber >= 1 && stepNumber <= len(hcpo.approvedPlan.Steps) {
		plannedStep = hcpo.approvedPlan.Steps[stepNumber-1]
		if id := plannedStep.GetID(); id != "" {
			stepID = id
		}
	}

	// Human-input steps don't write execution-attempt-*.json files. Their answer
	// is saved to the step's context_output file (e.g. step-N.json) with schema
	// {"response": "...", ...}. Without this branch, downstream routing steps
	// that run in a separate invocation (e.g. workshop execute_step) would see
	// an empty prior result and route blindly.
	if plannedStep != nil && plannedStep.StepType() == StepTypeHumanInput {
		executionWorkspacePath := fmt.Sprintf("%s/execution", validationWorkspacePath)
		stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, stepID, stepPath)
		contextOutput := plannedStep.GetContextOutput().String()
		if contextOutput == "" {
			contextOutput = fmt.Sprintf("step-%d.json", stepNumber)
		}
		resolvedContextOutput := ResolveVariables(contextOutput, hcpo.variableValues)
		responseFilePath := filepath.Join(stepExecutionPath, resolvedContextOutput)
		if content, err := hcpo.ReadWorkspaceFile(ctx, responseFilePath); err == nil {
			var responseData map[string]interface{}
			if err := json.Unmarshal([]byte(content), &responseData); err == nil {
				if response, ok := responseData["response"].(string); ok && response != "" {
					hcpo.GetLogger().Info(fmt.Sprintf("Loaded human_input response for step %d from %s (length=%d)", stepNumber, responseFilePath, len(response)))
					return response, true
				}
			}
		}
		// Fall through to the execution-attempt scan below — harmless, won't find anything.
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

		// Compute the step execution folder path
		stepPath := fmt.Sprintf("step-%d", i+1)
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
	isSubAgent bool, // true if this is a sub-agent from an orchestration step (never requests human feedback)
	previousExecutionResults []string, // Execution outputs from previous steps (indexed by step index)
	orchestrationRoutes []OrchestrationRoute, // Optional: orchestration routes (sub-agents) - only used when isSubAgent is true
) (executionResult string, updatedContextFiles []string, err error) {
	// Initialize updated context files as copy of previous context files
	updatedContextFiles = make([]string, len(previousContextFiles))
	copy(updatedContextFiles, previousContextFiles)
	artifactStepID, artifactStepPath := getExecutionArtifactIdentity(step.GetID(), stepPath, execCtx)

	// Emit step_started event (also emits step progress with status="start")
	// Note: Conditional steps emit their own step_started event in executeConditionalStep before calling executeSingleStep for branch steps
	hcpo.emitStepStartedEvent(ctx, step, stepIndex, stepPath, isBranchStep)

	// Guarantee the step leaves "running" on EVERY return path. This function has
	// many early returns between here and the bottom (scripted fast-path failures,
	// dependency/agent errors, context cancellations) — without this defer they'd
	// skip the finished event and the UI would show the step stuck on "running"
	// even after it completed or pre-validation failed. Emit "failed" on an error
	// return, "end" otherwise. Use a background context so a cancellation-driven
	// return still reaches the bridge. (Replaces the single success-path emit that
	// used to live at the end of the function.)
	defer func() {
		emitCtx := context.Background()
		if err != nil {
			hcpo.emitStepFailedEvent(emitCtx, step, stepIndex, stepPath, isBranchStep, err.Error())
		} else {
			hcpo.emitStepFinishedEvent(emitCtx, step, stepIndex, stepPath, isBranchStep)
		}
	}()

	// Narrow the session-level folder guard to this step's paths for the duration of
	// the step. Session guard is set workspace-wide by the interactive builder and batch
	// execution (server.go:4002, controller_batch_execution.go:323). Because shell commands
	// prefer session config over context keys (execute_shell_command.go:146), a broad
	// session guard would otherwise let a step in iteration-N read/write other iterations
	// via `cat`/`cp`. Snapshot and restore the prior config so the builder's out-of-step
	// chat shell commands keep their broader access.
	if sessionID := hcpo.GetMCPSessionID(); sessionID != "" {
		narrowAgentCfg := getAgentConfigs(step)
		narrowKBAccess := resolveKnowledgebaseAccess(narrowAgentCfg, hcpo.UseKnowledgebase())
		narrowKBWriteMethod := resolveKnowledgebaseWriteMethod(narrowAgentCfg)
		narrowLearningsAccess := resolveLearningsAccess(narrowAgentCfg)
		narrowRead, narrowWrite := hcpo.setupExecutionFolderGuard(artifactStepPath, artifactStepID, narrowKBAccess, narrowLearningsAccess, narrowKBWriteMethod, resolveDBAccess(narrowAgentCfg))
		var prevRead, prevWrite []string
		if prevCfg := common.GetSessionShellConfig(sessionID); prevCfg != nil {
			prevRead = prevCfg.ReadPaths
			prevWrite = prevCfg.WritePaths
		}
		common.SetSessionFolderGuard(sessionID, narrowRead, narrowWrite)
		hcpo.grantSessionCDPHostDownloadsReadOnly(sessionID)
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 [FOLDER_GUARD_STEP] Narrowed session %s for step %s: read=%v write=%v", sessionID, step.GetID(), narrowRead, narrowWrite))
		defer func() {
			common.SetSessionFolderGuard(sessionID, prevRead, prevWrite)
			hcpo.grantSessionCDPHostDownloadsReadOnly(sessionID)
			hcpo.GetLogger().Info(fmt.Sprintf("🔓 [FOLDER_GUARD_STEP] Restored session %s after step %s", sessionID, step.GetID()))
		}()
	}

	// Scripted code mode — determined once per step invocation (persists across outer-loop iterations).
	// Check embedded plan AgentConfigs first; fall back to step_configs.json so that workshop-saved
	// configs (use_code_execution_mode) also take effect for sub-agent steps whose embedded plan
	// config may not have the flag.
	isScriptedMode := false
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
		isScriptedMode = isScriptedExecutionModeConfig(agentCfgs)
	}
	if execCtx != nil && execCtx.SavedScriptOnly && !isScriptedMode {
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
		// Provider-based auto-enable is handled in applyStepConfigToAgentConfig.
		var isCodeExecutionMode bool
		agentConfigs := getAgentConfigs(step)
		if (agentConfigs == nil || agentConfigs.UseCodeExecutionMode == nil) && step.GetID() != "" {
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
		// Scripted code mode implies code execution mode
		if isScriptedMode {
			isCodeExecutionMode = true
			hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted_code] Persistent scripted mode enabled for step %d — forcing code execution mode", stepIndex+1))
		}

		// Always use learnings folder (unified folder for all learning types)
		learningsPath := fmt.Sprintf("%s/learnings", hcpo.GetWorkspacePath())
		// Get execution folder path for this step. Sub-agent calls may override the
		// artifact folder while keeping the stable step ID for configs/learnings.
		stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, artifactStepID, artifactStepPath)
		// Ensure step execution folder exists (create if it was previously deleted)
		if err := hcpo.ensureStepExecutionFolderExists(ctx, stepExecutionPath); err != nil {
			// Non-blocking: log warning but continue execution (folder will be created when files are written)
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure step execution folder exists: %v (continuing - folder will be created when files are written)", err))
		}
		// Resolve KB access mode for this step (explicit step config > preset default)
		kbAccess := resolveKnowledgebaseAccess(agentConfigs, hcpo.UseKnowledgebase())
		kbWriteMethod := resolveKnowledgebaseWriteMethod(agentConfigs)
		useKnowledgebase := kbAccess != KBAccessNone
		knowledgebasePath := ""
		if useKnowledgebase {
			knowledgebasePath = getKnowledgebasePath(hcpo.GetWorkspacePath())
		}

		// Get folder guard paths for template (so agent knows exact paths it can access)
		learningsAccess := resolveLearningsAccess(agentConfigs)
		evaluationDBWrite := false
		if evalStep, ok := step.(*EvaluationStep); ok {
			evaluationDBWrite = evalStep.DBWrite
		}
		dbAccess := resolveEffectiveDBAccess(agentConfigs, hcpo.isEvaluationMode, evaluationDBWrite)
		folderGuardReadPaths, folderGuardWritePaths := hcpo.setupExecutionFolderGuard(artifactStepPath, artifactStepID, kbAccess, learningsAccess, kbWriteMethod, dbAccess)

		// Learn code mode: add code/ subdir to write paths so LLM can write main.py there
		if isScriptedMode {
			stepExecutionPathForGuard := getExecutionFolderPath(executionWorkspacePath, artifactStepID, artifactStepPath)
			folderGuardWritePaths = append(folderGuardWritePaths, stepExecutionPathForGuard+"/code")
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
		if isScriptedMode {
			// In scripted mode the saved script is reused across groups/users, so keep
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
		if isScriptedMode && len(resolvedContextDeps) > 0 {
			// Reuse the exact same resolved dependency list for both the saved prompt
			// and the later python3 main.py invocation so they cannot drift.
			learnCodeInputArgsForPrompt = strings.Join(resolvedContextDeps, "\n")
		}

		// Inject VAR_GROUP_NAME early so it appears in the snapshotWorkspaceEnv used by ScriptedEnvVarNames.
		if hcpo.currentGroupName != "" {
			if envRef := hcpo.GetWorkspaceEnvRef(); envRef != nil {
				hcpo.LockWorkspaceEnv()
				envRef["VAR_GROUP_NAME"] = hcpo.currentGroupName
				hcpo.UnlockWorkspaceEnv()
			}
		}
		kbNotesPathForPrompt := toAbsPath(filepath.Join(getKnowledgebasePath(hcpo.GetWorkspacePath()), KBNotesFolderName))

		templateVars := map[string]string{
			"StepTitle":                 stepTitleForPrompt,
			"StepDescription":           stepDescriptionForPrompt,
			"StepSuccessCriteria":       "",
			"StepContextOutput":         ResolveVariables(step.GetContextOutput().String(), hcpo.variableValues),
			"WorkspacePath":             toAbsPath(executionWorkspacePath),                                                                                   // Absolute execution folder path (e.g., "/app/workspace-docs/Workflow/HRMS/runs/...")
			"LearningsPath":             toAbsPath(learningsPath),                                                                                            // Absolute learnings folder path
			"KnowledgebasePath":         toAbsPath(knowledgebasePath),                                                                                        // Absolute knowledgebase folder path
			"DBPath":                    toAbsPath(getDBPath(hcpo.GetWorkspacePath())),                                                                       // Absolute db folder path (always enabled)
			"UseKnowledgebase":          fmt.Sprintf("%v", useKnowledgebase),                                                                                 // Whether knowledgebase is enabled (deprecated, retained for backward compat)
			"KbAccess":                  kbAccess,                                                                                                            // KB access mode: "read" | "write" | "read-write" | "none"
			"KbAccessLabel":             kbAccessLabel(kbAccess),                                                                                             // Human-readable label for prompt display
			"KbWriteMethod":             kbWriteMethod,                                                                                                       // "agent" | "direct" — who writes KB
			"KnowledgebaseContribution": kbContributionForPrompt(agentConfigs),                                                                               // Author's contribution instruction (direct mode surfaces it to the step)
			"KBGuidanceBlock":           BuildStepKBGuidanceWithTarget(kbAccess, kbWriteMethod, kbContributionForPrompt(agentConfigs), kbNotesPathForPrompt), // Direct-mode-only KB contribution guidance
			"IsCodeExecutionMode":       fmt.Sprintf("%v", isCodeExecutionMode),                                                                              // Code execution mode flag (step-specific or preset)
			"StepNumber":                stepPath,                                                                                                            // Step identifier (e.g., "step-8" or "step-3-if-true-0")
			"StepExecutionPath":         toAbsPath(stepExecutionPath),                                                                                        // Absolute step execution folder path
			"FolderGuardReadPaths":      strings.Join(toAbsPathSlice(folderGuardReadPaths), ", "),                                                            // Absolute folder guard read paths
			"FolderGuardWritePaths":     strings.Join(toAbsPathSlice(folderGuardWritePaths), ", "),                                                           // Absolute folder guard write paths
			"IsEvaluationMode":          fmt.Sprintf("%v", hcpo.isEvaluationMode),                                                                            // Evaluation mode flag for eval-specific prompt guidance
			"WorkflowRoot":              toAbsPath(workflowRoot),                                                                                             // Absolute workflow root path (e.g., "/app/workspace-docs/Workflow/HRMS")
			"IsScriptedMode":            fmt.Sprintf("%v", isScriptedMode),
			"IsScriptedLocked":          fmt.Sprintf("%v", isScriptedMode && getAgentConfigs(step) != nil && getAgentConfigs(step).LockCode != nil && *getAgentConfigs(step).LockCode),
			"IsRelearnMode":             fmt.Sprintf("%v", isScriptedMode && learnCodePriorScript != ""),
			"ScriptedPriorScript":       learnCodePriorScript,
			"ScriptedPriorError":        learnCodePriorError,
			"ScriptedInputArgs":         learnCodeInputArgsForPrompt,
			"ScriptedEnvVarNames":       buildScriptedEnvVarNamesForPrompt(isScriptedMode, hcpo.snapshotWorkspaceEnv()),
			"ScriptedVarMapping":        buildScriptedVarMappingForPrompt(isCodeExecutionMode || isScriptedMode, hcpo.variablesManifest),
			"GroupName":                 hcpo.currentGroupName,
		}

		// In evaluation mode, inject TARGET_RUN_PATH into the prompt so the agent
		// knows where the original execution artifacts are located.
		if hcpo.isEvaluationMode {
			if targetRunPath, ok := hcpo.variableValues["TARGET_RUN_PATH"]; ok && targetRunPath != "" {
				varMapping := templateVars["ScriptedVarMapping"]
				targetLine := fmt.Sprintf("{{TARGET_RUN_PATH}} → os.environ['VAR_TARGET_RUN_PATH']  (= %s)", targetRunPath)
				if varMapping != "" {
					templateVars["ScriptedVarMapping"] = varMapping + "\n" + targetLine
				} else {
					templateVars["ScriptedVarMapping"] = targetLine
				}
			}
		}

		// Inject workflow variables as VAR_* env vars and workspace path as VAR_WORKSPACE_PATH.
		// VAR_* passes through the shell whitelist (MCP_*, SECRET_*, VAR_*).
		// Available via os.environ["VAR_NAME"] in Python or $VAR_NAME in bash.
		if envRef := hcpo.GetWorkspaceEnvRef(); envRef != nil {
			hcpo.LockWorkspaceEnv()
			for k, v := range hcpo.variableValues {
				envRef["VAR_"+k] = v
			}
			// Also inject the workflow workspace path as an absolute Docker-visible path
			// so Python/shell code can use it directly without guessing the docs root.
			if wp := hcpo.GetWorkspacePath(); wp != "" {
				envRef["VAR_WORKSPACE_PATH"] = toAbsPath(wp)
			}
			hcpo.UnlockWorkspaceEnv()
		}

		// Add context dependencies with full absolute paths and inline small file contents.
		// This validates existence (pre-flight) and inlines small text files into the prompt
		// so the LLM doesn't waste tool calls reading them.
		if len(resolvedContextDeps) > 0 {
			formattedDeps, depsErr := hcpo.formatContextDependenciesWithContent(ctx, resolvedContextDeps, docsRoot)
			if depsErr != nil {
				return "", updatedContextFiles, depsErr
			}
			templateVars["StepContextDependencies"] = formattedDeps
		} else {
			templateVars["StepContextDependencies"] = ""
		}

		// Add variable names if available (same format as other agents)
		if variableNames := FormatVariableNames(hcpo.variablesManifest); variableNames != "" {
			templateVars["VariableNames"] = variableNames
		}

		// Build previous steps summary from completed steps (include execution outputs)
		previousStepsSummary := hcpo.buildPreviousStepsSummary(allSteps, stepIndex, previousContextFiles, previousExecutionResults)

		templateVars["PreviousStepsSummary"] = previousStepsSummary
		if execCtx != nil && execCtx.WorkshopHumanInput != "" {
			templateVars["WorkshopHumanInput"] = execCtx.WorkshopHumanInput
			hcpo.GetLogger().Info(fmt.Sprintf("[WORKSHOP] Injecting human_input into step %q prompt (%d chars)", step.GetID(), len(execCtx.WorkshopHumanInput)))
		} else {
			templateVars["WorkshopHumanInput"] = ""
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

		// Inner loop: Automatic retry logic
		var validationResponse *ValidationResponse
		// KB contribution self-review is one-shot per step execution (direct mode with a
		// non-empty contribution). Declared outside the retry loop so it survives
		// validation retries without re-firing.
		var kbReviewPerformed bool
		// Direct-learnings turn is one-shot per step execution. Declared here so
		// validation retries don't re-fire it, and so the post-step learning-agent
		// trigger further down can see "already handled direct".
		var learningsDirectPerformed bool
		// Direct-mode UI should still emit one final step notification, but its
		// summary must include the main execution plus any inline KB/learnings turns.
		var mainExecutionSummary string
		var directKBReviewSummary string
		var directLearningsSummary string

		// Learn code mode: attempt fast path execution with saved script (before any LLM work).
		if isScriptedMode && !learnCodeFastPathDone {
			fastResult := hcpo.tryRunSavedScriptedScript(ctx, step, stepIndex, stepPath, allSteps,
				stepExecutionPath, executionWorkspacePath)
			if fastResult.RanScript {
				// Emit UI event for the saved-script execution attempt
				savedScriptPath := getScriptedScriptAbsPath(GetPromptDocsRoot(), hcpo.GetWorkspacePath(), step.GetID(), hcpo.isEvaluationMode)
				hcpo.emitScriptedExecutionEvent(ctx, step, stepIndex, stepPath,
					savedScriptPath, fastResult.Success, fastResult.ExitCode, fastResult.Output, fastResult.Error, 0, true)
				// Save execution log so debug_step and direct file inspection can see fast-path output
				hcpo.saveScriptedFastPathLog(ctx, stepIndex, artifactStepID, artifactStepPath, savedScriptPath, fastResult)
			}
			if fastResult.RanScript && fastResult.Success {
				// Saved script executed and validated — skip LLM entirely
				learnCodeFastPathDone = true
				executionResult = fastResult.Output
				validationResponse = &ValidationResponse{
					IsSuccessCriteriaMet: true,
					ExecutionStatus:      "COMPLETED",
					Reasoning:            "scripted: saved script executed and validated (0 LLM tokens)",
				}
				// If the script in execution/code/ differs from learnings (e.g., LLM-fixed version
				// from a previous attempt), save the working version back to learnings.
				learnCodeScriptNeedsSaving = true
				hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted] Fast path succeeded for step %d — skipping execution loop", stepIndex+1))
			} else if fastResult.RanScript {
				// Script ran but failed — fall through to LLM for relearn
				learnCodePriorScript = fastResult.ExistingScript
				learnCodePriorError = fastResult.Error
				hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted] Script failed for step %d — falling back to LLM with error context", stepIndex+1))
			} else if fastResult.ExistingScript != "" {
				// A saved script exists but wasn't executed successfully. Pass it to the LLM
				// so it can adapt the working script rather than rewriting from scratch.
				learnCodePriorScript = fastResult.ExistingScript
				// No prior error — this is an update/reuse path, not a failure
				hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted] Step %d found an existing saved script — LLM will update it in place", stepIndex+1))
			}

			// templateVars were built before the fast-path check. Refresh the scripted
			// prompt fields here so a saved-script reuse/failure can surface the saved script
			// and prior error in the actual rendered prompt for this same execution pass.
			templateVars["IsRelearnMode"] = fmt.Sprintf("%v", isScriptedMode && learnCodePriorScript != "")
			templateVars["ScriptedPriorScript"] = learnCodePriorScript
			templateVars["ScriptedPriorError"] = learnCodePriorError

			// On failure, point the relearn agent to script_metadata.json so it can
			// read run history, failure patterns, per-group stats, and streaks itself.
			if learnCodePriorError != "" {
				metaRelPath := getScriptedDirRelPath(step.GetID(), hcpo.isEvaluationMode) + "/script_metadata.json"
				templateVars["ScriptedMetadataPath"] = metaRelPath
			}

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

			// Signal whether this workflow has any browser MCP available (playwright /
			// agent_browser / Playwright / CDP). Downstream prompt builders use this to gate
			// the browser-specific main.py authoring rules + DOM probe so non-browser workflows
			// don't pay the prompt tax. Note: empty browserMode means "auto-detect", NOT "no
			// browser" — HasBrowserCapability() checks registered servers+skills directly.
			templateVars["HasBrowserAccess"] = fmt.Sprintf("%t", hcpo.HasBrowserCapability())

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

			learningPathIdentifier := step.GetID()
			currentDescriptionHash := hashStepDescription(step.GetDescription())

			// Always use paths-only mode — full learning content is too expensive for context.
			// The agent can read learning files if needed.
			keepLearningFull = false
			keepLearningFullSource = "always-false (paths only)"

			hcpo.GetLogger().Info(fmt.Sprintf("🧠 KeepLearningFull decision: %v (Source: %s)", keepLearningFull, keepLearningFullSource))

			// Learnings READ gate — controlled by learnings_access.
			// Default is "read": every step sees _global/SKILL.md in its prompt.
			// Only routing/eval steps or explicit learnings_access="none" opt out.
			// Contribution (write) is a separate gate further below.
			if !canReadLearnings(agentConfigs, step, hcpo.isEvaluationMode) {
				formattedLearningHistory = ""
				learningFilePaths = ""
				hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Learnings read disabled for step %d (learnings_access=%s) - skipping _global/ injection", stepIndex+1, resolveLearningsAccess(agentConfigs)))
			} else {
				// Learning is enabled - read from global learning skill
				formattedLearningHistory, err = hcpo.readGlobalLearningHistory(ctx)
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

			// Track which LLM model was used for execution (to be stored in learning metadata)
			var executionLLM string

			// executionAgent persists across retries so retry N>1 can continue the
			// existing conversation (sending validation feedback as a user message)
			// instead of re-running the whole step from scratch.
			var executionAgent agents.OrchestratorAgent
			// If scripted was ever active in any attempt, the conversation is polluted
			// with main.py authoring turns — fall back to fresh agents instead of continuing.
			learnCodeActiveInAnyAttempt := false
			adaptiveTierEnabled := hcpo.shouldUseAdaptiveExecutionTiering(ctx, agentConfigs)
			adaptiveTier := TierHigh
			adaptiveTierReason := "high (adaptive tiering disabled)"
			if adaptiveTierEnabled {
				decision, tierErr := hcpo.decideAdaptiveExecutionTier(
					ctx,
					learningPathIdentifier,
					stepPath,
					currentDescriptionHash,
				)
				if tierErr != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to resolve adaptive execution tier for step %d: %v — defaulting to Tier 1 (High)", stepIndex+1, tierErr))
					adaptiveTierEnabled = false
				} else {
					adaptiveTier = decision.Tier
					adaptiveTierReason = decision.Reason
					hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [ADAPTIVE] Step %d initial execution tier: %s (%s)", stepIndex+1, TierLevelLabel(adaptiveTier), adaptiveTierReason))
				}
			}

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
				// If a saved main.py exists for this step, point to the execution/code/ copy (not learnings/)
				// so the LLM doesn't reference or hardcode learnings paths in generated scripts.
				stepLearningHistory := formattedLearningHistory
				execCodeMainPyRelPath := stepExecutionPath + "/code/main.py"
				if _, mainPyReadErr := hcpo.ReadWorkspaceFile(ctx, execCodeMainPyRelPath); mainPyReadErr == nil {
					execCodeMainPyAbsPath := filepath.Join(toAbsPath(stepExecutionPath), "code", "main.py")
					if stepLearningHistory != "" {
						stepLearningHistory += "\n\n"
					}
					stepLearningHistory += fmt.Sprintf("📜 **Saved script available** at `%s` — this is a working implementation from a previous run. Read it before starting, then use `diff_patch_workspace_file` to update it rather than rewriting the entire file.", execCodeMainPyAbsPath)
				}
				templateVars["LearningHistory"] = stepLearningHistory
				// Set HasLearnings flag to explicitly indicate whether learnings exist (prevents agent from searching)
				templateVars["HasLearnings"] = fmt.Sprintf("%t", stepLearningHistory != "")

				templateVars["KeepLearningFull"] = fmt.Sprintf("%t", keepLearningFull)
				templateVars["LearningFilePaths"] = learningFilePaths // Set file paths for user message when KeepLearningFull is false

				// Check for context cancellation before creating execution agent
				select {
				case <-ctx.Done():
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step execution canceled before creating execution agent for step %d", stepIndex+1))
					return "", updatedContextFiles, fmt.Errorf("step execution canceled: %w", ctx.Err())
				default:
				}

				agentConfigs := getAgentConfigs(step)
				executionAgentCtx := ctx
				attemptTier := adaptiveTier
				var capturedToolCalls []orchestrator.ToolCallEntry
				var capturedLLMCalls []orchestrator.LLMCallEntry
				var attemptStartedAt time.Time
				var attemptCompletedAt time.Time
				var attemptDuration time.Duration

				// Track scripted presence to poison continuation in future attempts.
				if isScriptedMode {
					learnCodeActiveInAnyAttempt = true
				}

				// Decide whether to continue the prior conversation or start fresh.
				// Continuation reuses the existing agent + feeds validation feedback as a
				// user message — cheaper and keeps the agent's working memory.
				//
				// In normal flow, the ONLY trigger that lands in the fresh-agent branch
				// AFTER a pre-validation failure is scripted: its authoring turns must
				// not mix with validation-fix turns, and its inner fix loop creates its
				// own repair agents. Attempt #1 also takes the fresh path, but with no
				// validation feedback (it's just the initial execution).
				//
				// The agent / history guards below are defensive — they catch an
				// Execute() that errored before producing any LLM turns (saved in the
				// `err != nil` branch above without running pre-validation). They are
				// not the primary semantic; see the scripted condition for that.
				mustRestartForScripted := isScriptedMode || learnCodeActiveInAnyAttempt
				shouldContinue := retryAttempt > 1 && !mustRestartForScripted
				if shouldContinue && (executionAgent == nil || len(executionConversationHistory) == 0) {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step %d attempt %d: continuation requested but agent/history missing — falling back to fresh agent", stepIndex+1, retryAttempt))
					shouldContinue = false
				}

				if adaptiveTierEnabled {
					executionAgentCtx = context.WithValue(executionAgentCtx, WorkshopTierOverrideKey, int(attemptTier))
					hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [ADAPTIVE] Step %d attempt %d/%d forcing Tier %d (%s): %s",
						stepIndex+1, retryAttempt, maxRetryAttempts, int(attemptTier), TierLevelLabel(attemptTier), adaptiveTierReason))
				}

				if !shouldContinue {
					// Fresh-agent path: close prior (if any) and create a new execution agent.
					if executionAgent != nil {
						_ = executionAgent.Close()
						executionAgent = nil
					}

					// Pass stepPath to createExecutionOnlyAgent - it will determine the correct execution folder (supports branch and sub-agent steps)
					// For learnings / metadata selection, use the concrete step ID so sub-agents align with their own learnings folder.
					// allSteps is already []PlanStepInterface - no conversion needed
					executionAgent, err = hcpo.createExecutionOnlyAgent(executionAgentCtx, "execution_only", stepPath, executionAgentName, agentConfigs, step.GetID(), getExecutionArtifactFolderOverride(execCtx), evaluationDBWrite)
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

					// Sync the transport-level code execution flag from the resolved agent config.
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
						preExecLLM := agentConfigModelLabel(executionAgent.GetConfig())
						fb := fmt.Sprintf("execution-attempt-%d-iteration-%d", retryAttempt, 0)
						hcpo.preSavePromptsJSON(stepIndex, artifactStepID, artifactStepPath, "execution_only", preSystemPrompt, preUserMessage, preExecLLM, fb+"-prompts.json")
					}

					// Learn code mode: ensure code/ subdirectory exists (don't clean — LLM's
					// previous fix may be there and will be overwritten only if the LLM writes a new version).
					if isScriptedMode {
						codeDirRelPath := stepExecutionPath + "/code"
						if mkErr := createFolderViaAPI(ctx, codeDirRelPath); mkErr != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [scripted] Failed to pre-create code/ dir for step %d: %v", stepIndex+1, mkErr))
						}
					}

					if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
						cab.StartTimingCapture()
					}
					attemptStartedAt = time.Now().UTC()
					hcpo.recordWorkflowContinuationPhase(ctx, artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseMainExecution, workflowContinuationStatusRunning, "", executionAgent)
					// Execute execution-only agent with learning history (reused from learning reading above)
					executionResult, executionConversationHistory, err = hcpo.withWorkshopMessageTarget(ctx, step.GetID(), "execution", executionAgent, func() (string, []llmtypes.MessageContent, error) {
						return executionAgent.Execute(ctx, templateVars, []llmtypes.MessageContent{})
					})
				} else {
					// Continuation path: send validation feedback as a follow-up user
					// message on the existing agent. The system prompt, tool state, and
					// working memory from prior attempts are all preserved — the agent
					// sees its own prior tool calls + outputs and can fix surgically.
					feedbackUserMsg := buildValidationContinuationUserMessage(validationResponse, retryAttempt)
					hcpo.GetLogger().Info(fmt.Sprintf("🔁 Step %d attempt %d/%d: continuing existing execution agent with validation feedback (history=%d turns)",
						stepIndex+1, retryAttempt, maxRetryAttempts, len(executionConversationHistory)))

					ba := executionAgent.GetBaseAgent()
					if ba == nil {
						return "", updatedContextFiles, fmt.Errorf("execution agent has no base agent for continuation on step %d attempt %d", stepIndex+1, retryAttempt)
					}
					if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
						cab.StartTimingCapture()
					}
					attemptStartedAt = time.Now().UTC()
					hcpo.recordWorkflowContinuationPhase(ctx, artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseMainExecution, workflowContinuationStatusRunning, "", executionAgent)
					executionResult, executionConversationHistory, err = hcpo.withWorkshopMessageTarget(ctx, step.GetID(), "validation-retry", executionAgent, func() (string, []llmtypes.MessageContent, error) {
						return ba.Execute(ctx, feedbackUserMsg, executionConversationHistory, "", false)
					})
				}
				attemptCompletedAt = time.Now().UTC()
				attemptDuration = attemptCompletedAt.Sub(attemptStartedAt)
				if cab, ok := hcpo.GetContextAwareBridge().(*orchestrator.ContextAwareEventBridge); ok {
					timingCapture := cab.DrainTimingCapture()
					capturedToolCalls = timingCapture.ToolCalls
					capturedLLMCalls = timingCapture.LLMCalls
				}

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
					hcpo.recordWorkflowContinuationPhase(context.Background(), artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseMainExecution, workflowContinuationStatusFailed, err.Error(), executionAgent)
					if adaptiveTierEnabled && attemptTier == TierMedium {
						failureReason := fmt.Sprintf("execution error on attempt %d: %v", retryAttempt, err)
						if metaErr := hcpo.recordAdaptiveExecutionTierFailure(ctx, learningPathIdentifier, stepPath, attemptTier, currentDescriptionHash, failureReason); metaErr != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to record adaptive tier failure for step %d: %v", stepIndex+1, metaErr))
						}
						adaptiveTier = TierHigh
						adaptiveTierReason = "high (medium-tier execution failed in this run)"
					}

					// Save partial conversation history on failure/cancellation so users can inspect
					// tool responses from interrupted executions via debug_step or log files.
					if len(executionConversationHistory) > 0 {
						hcpo.GetLogger().Info(fmt.Sprintf("[PARTIAL-LOGS] Saving partial execution logs for step %d (%s) — %d conversation entries, error: %v", stepIndex+1, stepPath, len(executionConversationHistory), err))
						if logErr := hcpo.saveExecutionConversationLogs(stepIndex, artifactStepID, artifactStepPath, retryAttempt, 0,
							fmt.Sprintf("FAILED: %v", err), executionLLM, executionConversationHistory, executionAgent, capturedToolCalls, capturedLLMCalls, attemptStartedAt, attemptCompletedAt, attemptDuration); logErr != nil {
							hcpo.recordRunPersistenceError(context.Background(), artifactStepID, logErr)
						}
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
				mainExecutionSummary = summarizeExecutionResultForNotification(executionResult)
				hcpo.recordWorkflowContinuationPhase(context.Background(), artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseMainExecution, workflowContinuationStatusCompleted, "", executionAgent)

				// Save execution logs (result, conversation history, system prompt)
				if logErr := hcpo.saveExecutionConversationLogs(stepIndex, artifactStepID, artifactStepPath, retryAttempt, 0,
					executionResult, executionLLM, executionConversationHistory, executionAgent, capturedToolCalls, capturedLLMCalls, attemptStartedAt, attemptCompletedAt, attemptDuration); logErr != nil {
					hcpo.recordRunPersistenceError(context.Background(), artifactStepID, logErr)
					hcpo.GetLogger().Warn(fmt.Sprintf("Step %s completed, but its required execution result could not be persisted: %v", artifactStepID, logErr))
				}

				// Learn code mode: inner fix loop — run main.py and feed errors back as user messages
				// in the same conversation chain (no new agent, no system-prompt reset).
				if isScriptedMode {
					// If learnings are locked, skip the fix loop entirely — the LLM's rewrite
					// won't be saved back anyway, so there's no point spending tokens on repair.
					// Set maxFixIter to -1 so the fix loop is skipped and we fall through
					// to the agentic fallback below.
					isCodeLockedForFixLoop := agentConfigs != nil && agentConfigs.LockCode != nil && *agentConfigs.LockCode
					maxFixIter := 3
					if isCodeLockedForFixLoop {
						hcpo.GetLogger().Info(fmt.Sprintf("🔒 [scripted] Code locked for step %d — skipping fix loop, will fall back to agentic mode", stepIndex+1))
						maxFixIter = -1
					} else if agentCfgs := getAgentConfigs(step); agentCfgs != nil && agentCfgs.ScriptedMaxFixIter != nil {
						maxFixIter = *agentCfgs.ScriptedMaxFixIter
					}
					codeDirAbsPath := filepath.Join(toAbsPath(stepExecutionPath), "code")
					mainPyPath := filepath.Join(codeDirAbsPath, "main.py")
					var lastLcResult *ScriptedFastPathResult
					// Check if pre-validation already passes (LLM may have run main.py or produced outputs).
					// If outputs are valid AND main.py exists, skip the fix loop — no need to re-run.
					preValResults, _ := RunPreValidation(ctx, getValidationSchema(step), stepExecutionPath, hcpo.BaseOrchestrator)
					mainPyRelPath := stepExecutionPath + "/code/main.py"
					_, mainPyExistsErr := hcpo.ReadWorkspaceFile(ctx, mainPyRelPath)
					if preValResults != nil && preValResults.OverallPass && mainPyExistsErr == nil {
						learnCodePreValidationResultsOverride = preValResults
						// Try to get exit code from LLM self-run detection (optional — for logging)
						var exitCode int
						var output string
						if selfRun := detectSuccessfulLLMScriptedSelfRun(executionConversationHistory, mainPyPath); selfRun != nil {
							exitCode = selfRun.ExitCode
							output = selfRun.Output
						}
						lastLcResult = &ScriptedFastPathResult{
							RanScript: true,
							Success:   true,
							ExitCode:  exitCode,
							Output:    output,
						}
						hcpo.GetLogger().Info(fmt.Sprintf("✅ [scripted] Pre-validation passed and main.py exists for step %d — skipping fix loop", stepIndex+1))
						hcpo.emitScriptedExecutionEvent(ctx, step, stepIndex, stepPath,
							mainPyPath, true, lastLcResult.ExitCode, lastLcResult.Output, "", 0, false)
					} else if preValResults != nil && preValResults.OverallPass {
						hcpo.GetLogger().Info(fmt.Sprintf("🧪 [scripted] Pre-validation passed but main.py not found for step %d — entering fix loop to generate script", stepIndex+1))
					}

					// Track script content before each fix iteration to generate diffs
					prevFixScript := ""
					if s, readErr := hcpo.ReadWorkspaceFile(ctx, stepExecutionPath+"/code/main.py"); readErr == nil {
						prevFixScript = s
					}

					// Fix loop: check pre-validation after each LLM turn.
					// The LLM writes and runs main.py itself — the controller only checks if
					// the outputs are valid. If not, feed validation errors back and let the LLM fix.
					for fixIter := 0; fixIter <= maxFixIter && (lastLcResult == nil || !lastLcResult.Success); fixIter++ {
						// Check for context cancellation before each fix iteration
						select {
						case <-ctx.Done():
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [scripted] Step execution canceled during fix iteration %d for step %d", fixIter+1, stepIndex+1))
							return "", updatedContextFiles, fmt.Errorf("step execution canceled during scripted fix loop: %w", ctx.Err())
						default:
						}

						// Check pre-validation — did the LLM produce valid outputs?
						fixPreValResults, _ := RunPreValidation(ctx, getValidationSchema(step), stepExecutionPath, hcpo.BaseOrchestrator)
						mainPyRelPath := stepExecutionPath + "/code/main.py"
						_, mainPyErr := hcpo.ReadWorkspaceFile(ctx, mainPyRelPath)

						if fixPreValResults != nil && fixPreValResults.OverallPass && mainPyErr == nil {
							// Outputs valid + main.py exists → success
							lastLcResult = &ScriptedFastPathResult{RanScript: true, Success: true}
							hcpo.emitScriptedExecutionEvent(ctx, step, stepIndex, stepPath,
								mainPyPath, true, 0, "", "", fixIter, false)
							hcpo.GetLogger().Info(fmt.Sprintf("✅ [scripted] Pre-validation passed for step %d on fix iteration %d", stepIndex+1, fixIter))
							learnCodePreValidationResultsOverride = fixPreValResults
							break
						}

						if fixIter == maxFixIter {
							// Record the failure for the outer loop — include execution output
							// so the next retry attempt knows what the script actually did
							var errMsg string
							if mainPyErr != nil {
								errMsg = "main.py was not written"
							} else if fixPreValResults != nil {
								errMsg = formatWorkspaceResults(fixPreValResults)
							} else {
								errMsg = "pre-validation could not run"
							}
							// Append last execution output so the next retry has full context
							if lastRunOutput, lastRunExitCode, lastRunFound := extractLastMainPyRunOutput(executionConversationHistory, mainPyPath); lastRunFound {
								outputSnippet := lastRunOutput
								if len(outputSnippet) > 4000 {
									outputSnippet = outputSnippet[:2000] + "\n... (truncated) ...\n" + outputSnippet[len(outputSnippet)-2000:]
								}
								errMsg = fmt.Sprintf("%s\n\nLast execution output (exit code %d):\n%s", errMsg, lastRunExitCode, outputSnippet)
							}
							// Use the latest main.py from execution/code/ (LLM may have rewritten it during fix loop)
							latestScript := ""
							if mainPyErr == nil {
								if s, readErr := hcpo.ReadWorkspaceFile(ctx, mainPyRelPath); readErr == nil {
									latestScript = s
								}
							}
							lastLcResult = &ScriptedFastPathResult{RanScript: mainPyErr == nil, Success: false, Error: errMsg, ExistingScript: latestScript}
							hcpo.emitScriptedExecutionEvent(ctx, step, stepIndex, stepPath,
								mainPyPath, false, 1, "", errMsg, fixIter, false)
							break // exhausted fix attempts
						}

						// Build feedback message with validation errors
						var feedbackMsg string
						stepDesc := templateVars["StepDescription"]
						var sb strings.Builder
						sb.WriteString(fmt.Sprintf("## Task\n%s\n\n", stepDesc))

						if mainPyErr != nil {
							// main.py not written yet
							if priorCtx := BuildScriptedPriorContext(templateVars["ScriptedPriorScript"], templateVars["ScriptedPriorError"], templateVars["ScriptedMetadataPath"], templateVars["IsScriptedLocked"] == "true"); priorCtx != "" {
								sb.WriteString(priorCtx)
								sb.WriteString("\n")
							}
							sb.WriteString(fmt.Sprintf("main.py was not found at %s/main.py.\n\n", codeDirAbsPath))
							sb.WriteString("Write the complete solution to main.py there, run it, and ensure the output files are produced.")
						} else {
							// main.py exists but outputs are invalid
							var actualScript string
							if s, readErr := hcpo.ReadWorkspaceFile(ctx, mainPyRelPath); readErr == nil && s != "" {
								actualScript = s
								sb.WriteString(fmt.Sprintf("### Your Script\n\nRead the current script at `%s/main.py` before making changes.\n\n", codeDirAbsPath))
							}
							// Static code review: catch anti-patterns before they get saved to learnings
							if actualScript != "" {
								// Pass declared env vars so the review can distinguish declared vs invented vars
								var declaredVars []string
								if envNames := templateVars["ScriptedEnvVarNames"]; envNames != "" {
									declaredVars = strings.Split(envNames, "\n")
								}
								if codeIssues := reviewMainPyScript(actualScript, declaredVars...); len(codeIssues) > 0 {
									sb.WriteString(fmt.Sprintf("**⚠️ Code review found %d issue(s) that MUST be fixed:**\n", len(codeIssues)))
									for idx, issue := range codeIssues {
										sb.WriteString(fmt.Sprintf("%d. %s\n", idx+1, issue))
									}
									sb.WriteString("\nThese issues will cause failures when the script is reused for other groups/users. Fix them before running.\n\n")
									hcpo.GetLogger().Warn(fmt.Sprintf("🔍 [review-code] Found %d issue(s) in main.py for step %d", len(codeIssues), stepIndex+1))
								}
							}
							// Include the last main.py execution output so the fix agent knows what happened
							if lastRunOutput, lastRunExitCode, lastRunFound := extractLastMainPyRunOutput(executionConversationHistory, mainPyPath); lastRunFound {
								sb.WriteString(fmt.Sprintf("**Last execution output (exit code %d):**\n```\n", lastRunExitCode))
								// Truncate to avoid oversized prompts
								if len(lastRunOutput) > 8000 {
									sb.WriteString(lastRunOutput[:4000])
									sb.WriteString("\n... (truncated) ...\n")
									sb.WriteString(lastRunOutput[len(lastRunOutput)-4000:])
								} else {
									sb.WriteString(lastRunOutput)
								}
								sb.WriteString("\n```\n\n")
							}
							sb.WriteString(fmt.Sprintf("**Output validation failed (attempt %d/%d).** The script did not produce the correct outputs. Re-read the task requirements, check what your script actually did, and fix it.\n\n", fixIter+1, maxFixIter))
							sb.WriteString("**CRITICAL: Your script must actually fetch/compute data by calling MCP tools or APIs or processing real input files. Do NOT hardcode, fabricate, or hallucinate output data — every value in the output must come from a real data source.**\n\n")
							sb.WriteString("Fix main.py, run it again, and ensure all required output files are produced correctly.")
						}
						feedbackMsg = sb.String()
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [scripted] validation failed for step %d (fix attempt %d/%d) — continuing conversation with feedback", stepIndex+1, fixIter+1, maxFixIter))

						// Create a fresh repair agent for each fix iteration — each attempt gets
						// a clean conversation with the latest script + errors as context.
						// This avoids accumulated confusion from prior failed attempts.
						repairAgentName := fmt.Sprintf("%s-fix-%d-high", executionAgentName, fixIter+1)
						// Force Tier 1 (High) for repair agents — they need to fix a failure,
						// so they should use at least the same tier as the original execution.
						repairCtx := context.WithValue(ctx, WorkshopTierOverrideKey, int(TierHigh))
						repairAgent, repairErr := hcpo.createExecutionOnlyAgent(repairCtx, "execution_only", stepPath, repairAgentName, agentConfigs, step.GetID(), getExecutionArtifactFolderOverride(execCtx), evaluationDBWrite)
						if repairErr != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [scripted] failed to create repair agent for step %d fix %d: %v", stepIndex+1, fixIter+1, repairErr))
							break
						}
						if repairCfg := repairAgent.GetConfig(); repairCfg != nil && repairCfg.LLMConfig.Primary.ModelID != "" {
							executionLLM = fmt.Sprintf("%s/%s", repairCfg.LLMConfig.Primary.Provider, repairCfg.LLMConfig.Primary.ModelID)
						}
						repairSystemPrompt := ""
						if repairEOA, ok := repairAgent.(*WorkflowExecutionOnlyAgent); ok {
							repairSystemPrompt = repairEOA.executionOnlySystemPromptProcessor(templateVars)
						}
						if executionAgent != nil {
							_ = executionAgent.Close()
						}
						executionAgent = repairAgent
						hcpo.GetLogger().Info(fmt.Sprintf("🔁 [scripted] created fresh repair agent for step %d fix %d: %s", stepIndex+1, fixIter+1, executionLLM))

						// Fresh conversation each time — feedback message already contains
						// the full script + execution output + validation errors as context.
						if ba := executionAgent.GetBaseAgent(); ba != nil {
							_, executionConversationHistory, err = hcpo.withWorkshopMessageTarget(ctx, step.GetID(), "script-repair", executionAgent, func() (string, []llmtypes.MessageContent, error) {
								return ba.Execute(ctx, feedbackMsg, nil, repairSystemPrompt, false)
							})
							if err != nil {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [scripted] agent error during fix attempt %d: %v", fixIter+1, err))
								break
							}
						} else {
							break
						}

						// Capture diff between fix iterations for debugging
						mainPyFixRelPath := stepExecutionPath + "/code/main.py"
						if curScript, readErr := hcpo.ReadWorkspaceFile(ctx, mainPyFixRelPath); readErr == nil && prevFixScript != "" && curScript != prevFixScript {
							fixDiffsRelPath := stepExecutionPath + "/code/fix-diffs"
							if mkErr := createFolderViaAPI(ctx, fixDiffsRelPath); mkErr == nil {
								diff := generateSimpleDiff("main.py", prevFixScript, curScript)
								diffFile := fmt.Sprintf("fix-%d-to-%d.diff", fixIter, fixIter+1)
								if writeErr := hcpo.WriteWorkspaceFile(ctx, fixDiffsRelPath+"/"+diffFile, diff); writeErr == nil {
									hcpo.GetLogger().Info(fmt.Sprintf("📝 [scripted] Saved fix diff %s for step %d", diffFile, stepIndex+1))
								}
							}
							prevFixScript = curScript
						} else if readErr == nil {
							prevFixScript = curScript
						}
					}

					// The saved learnings script already failed — any LLM-produced script is a
					// newer attempt and likely better. Save it to learnings regardless of success
					// so the next run starts from the latest version, not the known-broken one.
					// BUT: don't save scripts with syntax errors — those are definitely worse.
					// AND: don't save if code is locked — the user froze the script intentionally.
					isCodeLocked := agentConfigs != nil && agentConfigs.LockCode != nil && *agentConfigs.LockCode
					if isCodeLocked {
						hcpo.GetLogger().Info(fmt.Sprintf("🔒 [scripted] Code locked for step %d — NOT saving script back to learnings", stepIndex+1))
						learnCodeScriptNeedsSaving = false
					} else if mainPyRelCheck := stepExecutionPath + "/code/main.py"; true {
						if scriptContent, checkErr := hcpo.ReadWorkspaceFile(ctx, mainPyRelCheck); checkErr == nil && scriptContent != "" {
							// Quick syntax check: run python3 -c "compile(...)" to catch syntax errors
							hasSyntaxError := false
							if lastLcResult != nil && !lastLcResult.Success && lastLcResult.Error != "" {
								if strings.Contains(lastLcResult.Error, "SyntaxError") {
									hasSyntaxError = true
								}
							}
							if hasSyntaxError {
								hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [scripted] NOT saving main.py to learnings for step %d — script has syntax errors", stepIndex+1))
							} else {
								hcpo.saveScriptedScriptToLearnings(step, toAbsPath(stepExecutionPath))
								hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted] Saved LLM-produced main.py to learnings for step %d (replaces known-broken version)", stepIndex+1))
								learnCodeScriptNeedsSaving = false // already saved, don't duplicate later
							}
						}
					}

					if lastLcResult == nil || !lastLcResult.Success {
						var errMsg string
						if lastLcResult == nil {
							errMsg = fmt.Sprintf("main.py was never written to %s", codeDirAbsPath)
						} else {
							errMsg = fmt.Sprintf("main.py still failing after %d fix attempts:\n%s", maxFixIter, lastLcResult.Error)
						}
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [scripted] step %d failed after fix loop: %s", stepIndex+1, errMsg))
						// Fallback: disable scripted mode for remaining retries.
						// The LLM couldn't write a working main.py — let it use tools
						// directly in normal agentic mode to complete the task.
						isScriptedMode = false
						templateVars["IsScriptedMode"] = "false"
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 [scripted] Switching step %d to normal agentic mode for remaining retries", stepIndex+1))
						// Add explicit guidance for the agentic fallback — tell the LLM
						// that the scripted approach failed and it should use tools directly.
						fallbackGuidance := fmt.Sprintf(
							"%s\n\n"+
								"**IMPORTANT: The scripted main.py approach failed after multiple attempts. "+
								"Do NOT write a main.py script this time. Instead, complete the task by calling "+
								"MCP tools directly via the API step by step. Use the tools to understand the "+
								"current state first (e.g. read files, take browser snapshots, inspect pages), "+
								"then perform the required actions interactively. Focus on completing the task, "+
								"not on writing reusable code.**",
							errMsg)
						validationResponse = &ValidationResponse{
							IsSuccessCriteriaMet: false,
							ExecutionStatus:      "FAILED",
							Reasoning:            fallbackGuidance,
						}
						if retryAttempt >= maxRetryAttempts {
							validationFailedAfterMaxRetries = true
						}
						continue
					}
					turnCount = len(executionConversationHistory)
					hcpo.GetLogger().Info(fmt.Sprintf("✅ [scripted] main.py executed successfully for step %d — proceeding to validation", stepIndex+1))
				}

				// Run pre-validation (code-based structural checks) -- always active, independent of LLM validation.
				agentConfigs = getAgentConfigs(step)
				hcpo.recordWorkflowContinuationPhase(ctx, artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhasePreValidation, workflowContinuationStatusRunning, "", executionAgent)
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
				preValidationStatus := workflowContinuationStatusCompleted
				preValidationError := ""
				if preValidationResults != nil && !preValidationResults.OverallPass {
					preValidationStatus = workflowContinuationStatusFailed
					preValidationError = formatWorkspaceResults(preValidationResults)
				}
				hcpo.recordWorkflowContinuationPhase(ctx, artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhasePreValidation, preValidationStatus, preValidationError, executionAgent)

				// Persist pre-validation results for Pulse Bug Review and diagnostics.
				if hcpo.selectedRunFolder != "" {
					preValLogPath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
					SavePreValidationLog(ctx, hcpo.BaseOrchestrator, preValLogPath, step.GetID(), stepPath, preValidationResults, preValidationSchema)
				}

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

					// KB contribution self-review (direct-write mode only): one-shot nudge
					// asking the step agent to verify it fulfilled its knowledgebase_contribution
					// contract. Does NOT consume a retry slot, does NOT re-run pre-validation,
					// and fires at most once per step execution. The agent's follow-up tool
					// calls (if any) land via the normal notes-write pipeline.
					if !kbReviewPerformed && executionAgent != nil {
						stepCfgForReview := getAgentConfigs(step)
						reviewMsg := BuildKBContributionReviewMessageWithTarget(
							resolveKnowledgebaseAccess(stepCfgForReview, hcpo.UseKnowledgebase()),
							resolveKnowledgebaseWriteMethod(stepCfgForReview),
							kbContributionForPrompt(stepCfgForReview),
							filepath.Join(GetPromptDocsRoot(), hcpo.GetWorkspacePath(), KnowledgebaseFolderName, KBNotesFolderName),
						)
						if reviewMsg != "" {
							kbReviewPerformed = true
							hcpo.GetLogger().Info(fmt.Sprintf("🧠 KB contribution self-review: firing one-shot continuation for step %d", stepIndex+1))
							hcpo.recordWorkflowContinuationPhase(ctx, artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseKBReview, workflowContinuationStatusRunning, "", executionAgent)
							if ba := executionAgent.GetBaseAgent(); ba != nil {
								reviewResult, updatedHistory, reviewErr := hcpo.withWorkshopMessageTarget(ctx, step.GetID(), "knowledgebase-review", executionAgent, func() (string, []llmtypes.MessageContent, error) {
									return ba.Execute(ctx, reviewMsg, executionConversationHistory, "", false)
								})
								if reviewErr != nil {
									hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ KB self-review continuation failed for step %d: %v (accepting step anyway)", stepIndex+1, reviewErr))
									hcpo.recordWorkflowContinuationPhase(context.Background(), artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseKBReview, workflowContinuationStatusFailed, reviewErr.Error(), executionAgent)
								} else {
									directKBReviewSummary = summarizeExecutionResultForNotification(reviewResult)
									executionConversationHistory = updatedHistory
									hcpo.GetLogger().Info(fmt.Sprintf("🧠 KB self-review completed for step %d (history=%d turns)", stepIndex+1, len(executionConversationHistory)))
									hcpo.recordWorkflowContinuationPhase(context.Background(), artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseKBReview, workflowContinuationStatusCompleted, "", executionAgent)
								}
							}
						}
					}

					// Direct-learnings turn (fires after KB review, if any). Runs only when
					// learnings_write_method is "direct" AND the access/objective gates
					// agree (see shouldDirectWriteLearnings). lock_learnings is honored
					// below. Writes target learnings/_global/
					// (shared across steps), with learnings/<stepID>/ opened only in scripted
					// mode for the main.py copy. Folder guard is widened here for the turn only —
					// the main-step execution did NOT have write access to either path. The
					// outer step defer that restores session shell config to pre-step state
					// handles cleanup when the step exits. Parallel steps writing to _global/
					// are serialized by learningsGlobalFileMutex.
					if !learningsDirectPerformed && executionAgent != nil {
						stepCfgForLearn := getAgentConfigs(step)
						if shouldDirectWriteLearnings(stepCfgForLearn, step, hcpo.isEvaluationMode) {
							// Lock + empty-folder override: lock_learnings is honored only when
							// _global/ already has content to protect. An empty _global/ folder
							// allows the first direct-mode write to bootstrap initial learnings.
							skipDueToLock := hcpo.shouldSkipDirectLearningsDueToLock(ctx, stepCfgForLearn, stepIndex)

							learnObjective := ""
							if stepCfgForLearn != nil {
								learnObjective = stepCfgForLearn.LearningObjective
							}
							learningsTurnMsg := ""
							if !skipDueToLock {
								learningsTurnMsg = hcpo.buildLearningsContributionTurn(step.GetID(), templateVars["StepDescription"], learnObjective, isScriptedMode)
							} else {
								hcpo.recordWorkflowContinuationPhase(ctx, artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseDirectLearning, workflowContinuationStatusSkipped, "lock_learnings=true with existing _global content", executionAgent)
							}
							if learningsTurnMsg != "" {
								learningsDirectPerformed = true
								baseWorkspace := hcpo.GetWorkspacePath()
								globalLearningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspace, GlobalLearningID)
								// Only widen _global/. learnings/<stepID>/ is NOT added even in
								// scripted mode — main.py copying is handled by Go code
								// (saveScriptedScriptToLearnings, called after the execution block)
								// independent of the direct-learnings turn. The step agent has no
								// reason to write under learnings/<stepID>/ in direct mode, so we
								// keep the guard tight.
								addedPaths := []string{globalLearningsPath}

								// Serialize against parallel direct-learnings turns. Each step has its
								// own MCP session so folder guards don't collide, but _global/SKILL.md
								// is a shared file — parallel diff_patches would race without this.
								hcpo.recordWorkflowContinuationPhase(ctx, artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseDirectLearning, workflowContinuationStatusWaitingForLock, "", executionAgent)
								func() {
									restoreDirectLearningTurn := hcpo.prepareDirectLearningTurn(executionAgent, addedPaths)
									defer restoreDirectLearningTurn()

									if cfg := executionAgent.GetConfig(); cfg != nil && strings.TrimSpace(cfg.MCPSessionID) != "" {
										hcpo.GetLogger().Info(fmt.Sprintf("🔓 [LEARN_DIRECT] Widened sub-agent session %s for learnings turn on step %s: +%v", strings.TrimSpace(cfg.MCPSessionID), step.GetID(), addedPaths))
									} else {
										hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [LEARN_DIRECT] Execution agent has no MCPSessionID on config — learnings writes may fail for step %d", stepIndex+1))
									}

									learningsGlobalFileMutex.Lock()
									defer learningsGlobalFileMutex.Unlock()
									hcpo.GetLogger().Info(fmt.Sprintf("🧠 Direct-learnings: firing one-shot continuation for step %d (objective length=%d)", stepIndex+1, len(learnObjective)))
									hcpo.recordWorkflowContinuationPhase(ctx, artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseDirectLearning, workflowContinuationStatusRunning, "", executionAgent)
									if ba := executionAgent.GetBaseAgent(); ba != nil {
										learnResult, learnHistory, learnErr := hcpo.withWorkshopMessageTarget(ctx, step.GetID(), "learnings", executionAgent, func() (string, []llmtypes.MessageContent, error) {
											return ba.Execute(ctx, learningsTurnMsg, executionConversationHistory, "", false)
										})
										if learnErr != nil {
											hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Direct-learnings continuation failed for step %d: %v (accepting step anyway)", stepIndex+1, learnErr))
											hcpo.recordWorkflowContinuationPhase(context.Background(), artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseDirectLearning, workflowContinuationStatusFailed, learnErr.Error(), executionAgent)
										} else {
											directLearningsSummary = summarizeExecutionResultForNotification(learnResult)
											executionConversationHistory = learnHistory
											hcpo.GetLogger().Info(fmt.Sprintf("🧠 Direct-learnings completed for step %d (history=%d turns)", stepIndex+1, len(executionConversationHistory)))
											hcpo.recordWorkflowContinuationPhase(context.Background(), artifactStepID, artifactStepPath, workflowContinuationOwnerStepExecution, workflowContinuationPhaseDirectLearning, workflowContinuationStatusCompleted, "", executionAgent)

											// Direct-mode learnings are the only runtime write path, so
											// metadata bookkeeping happens here. Runtime execution never
											// auto-locks learnings; builder/user decisions own lock_learnings.
											directLearningPathIdentifier := getEffectiveLearningPathIdentifier(step.GetID(), stepPath, stepCfgForLearn)
											hasNewLearning, directLearningReasoning, directLearningConfidence := inferHasNewLearningFromResult(learnResult)
											directLearningLLM := executionLLM
											if metadataErr := hcpo.updateLearningMetadataWithTurnCount(
												ctx,
												stepIndex,
												stepPath,
												directLearningPathIdentifier,
												hasNewLearning,
												directLearningReasoning,
												directLearningConfidence,
												turnCount,
												step,
												true, // pre-validation already passed
												executionLLM,
												directLearningLLM,
											); metadataErr != nil {
												hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update direct-learnings metadata for step %s: %v", step.GetID(), metadataErr))
											}
										}
									}
								}()
							}
						}
					}
					if combinedSummary := buildDirectModeCompletionSummary(mainExecutionSummary, directKBReviewSummary, directLearningsSummary); combinedSummary != "" {
						executionResult = combinedSummary
					}

					validationResponse = &ValidationResponse{
						IsSuccessCriteriaMet: true,
						ExecutionStatus:      "COMPLETED",
						Reasoning:            "Pre-validation passed - step auto-approved",
					}
					// Learn code mode: mark script for saving to learnings after the execution loop
					if isScriptedMode {
						learnCodeScriptNeedsSaving = true
					}
				}

				// LEARNING PHASE: Runs for ALL agents regardless of validation status
				// Validation being disabled does NOT prevent learning from running
				// Learning will run if: not disabled, not locked, and not skipped due to temp LLM override
				// LEARNING DISABLED: Skip learning agents entirely
				// Learnings WRITE gate — controlled by learnings_access="read-write" AND
				// non-empty learning_objective (the extraction target for the writer).
				// No more code-exec force-enable — that was papering over the fact that
				// "empty objective" used to also kill read access; with learnings_access
				// split, write is honest opt-in and read is default-on.
				agentConfigs = getAgentConfigs(step)
				isLearningDisabled := !canWriteLearnings(agentConfigs, step, hcpo.isEvaluationMode)
				if isScriptedMode {
					hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted_code] Step %d — main.py remains executable truth; SKILL.md writes gated by learnings_access=%s", stepIndex+1, resolveLearningsAccess(agentConfigs)))
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
				// Pre-validation result drives validationResponse (set above).
				// Safety guard: if somehow nil, default to success so learning + KB can proceed.
				if validationResponse == nil {
					hcpo.GetLogger().Info(fmt.Sprintf("Pre-validation result missing for step %d - defaulting to success for post-step phases", stepIndex+1))
					validationResponse = &ValidationResponse{
						IsSuccessCriteriaMet: true,
						ExecutionStatus:      "COMPLETED",
						Reasoning:            "LLM validation disabled - step auto-approved for post-step phases",
					}
				}

				// Populate runtime fields once — shared by both learning AND KB update.
				// Done outside the learning skip-block so KB still runs when learning is
				// disabled or locked (KB and learning are orthogonal persistent stores).
				var populatedTodoStep PlanStepInterface
				if validationResponse.IsSuccessCriteriaMet {
					stepConfigs, cfgErr := hcpo.ReadStepConfigs(ctx)
					if cfgErr != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read step_config.json: %v (using defaults)", cfgErr))
						stepConfigs = []StepConfig{}
					}
					ts, popErr := populateStepRuntimeFields(step, stepConfigs)
					if popErr != nil {
						hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to populate runtime fields for post-step phases: %v", popErr))
					} else {
						populatedTodoStep = ts
					}
				}

				// Post-step learning AGENT (the separate analytical reviewer that
				// reads the step trace and writes SKILL.md as its own LLM turn) is
				// retired. All steps now use direct-mode learning: the step agent
				// itself writes SKILL.md inline during its dedicated post-completion
				// turn (gated earlier in this method by shouldDirectWriteLearnings
				// and the learningsDirectPerformed flag). The lock / disabled log
				// lines are kept so operators see the gate, but no background
				// launch happens.
				if isLearningDisabled {
					hcpo.GetLogger().Info(fmt.Sprintf("⏭️ Learning disabled: Skipping learning for step %d", stepIndex+1))
				} else if shouldSkipLearningDueToLock {
					hcpo.GetLogger().Info(fmt.Sprintf("🔒 Learnings locked: Skipping learning for step %d (using existing learnings)", stepIndex+1))
				}

				// Post-step knowledgebase update — INDEPENDENT of learning lock/disable.
				// KB and learnings are orthogonal stores: a frozen SKILL.md does NOT mean
				// we should stop accumulating durable domain facts. Gated only on:
				//   - validation success (don't extract from a failed run's output)
				//   - the three conditions inside maybeEnqueueKBUpdate (KB enabled +
				//     not workflow-locked + per-step write access + non-empty contribution)
				// Serialized through kbUpdateQueue so concurrent step completions don't
				// race on knowledgebase/notes/ writes.
				if validationResponse.IsSuccessCriteriaMet && populatedTodoStep != nil {
					hcpo.maybeEnqueueKBUpdate(stepIndex, stepPath, populatedTodoStep)
				}

				// Check if success criteria was met
				// Check IsSuccessCriteriaMet instead of just ExecutionStatus - PARTIAL/INCOMPLETE can also mean criteria not met
				if validationResponse != nil && validationResponse.IsSuccessCriteriaMet {
					if adaptiveTierEnabled {
						if metaErr := hcpo.recordAdaptiveExecutionTierSuccess(ctx, learningPathIdentifier, stepPath, attemptTier, currentDescriptionHash); metaErr != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to record adaptive tier success for step %d: %v", stepIndex+1, metaErr))
						}
					}
					hcpo.GetLogger().Info(fmt.Sprintf("✅ Step %d passed validation - success criteria met (Status: %s)", stepIndex+1, validationResponse.ExecutionStatus))

					break // Exit retry loop and continue to next step
				} else {
					statusStr := "unknown"
					if validationResponse != nil {
						statusStr = validationResponse.ExecutionStatus
					}
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Step %d failed validation - success criteria not met (Status: %s, attempt %d/%d)", stepIndex+1, statusStr, retryAttempt, maxRetryAttempts))
					if adaptiveTierEnabled && attemptTier == TierMedium {
						failureReason := fmt.Sprintf("validation failed on attempt %d (status=%s)", retryAttempt, statusStr)
						if metaErr := hcpo.recordAdaptiveExecutionTierFailure(ctx, learningPathIdentifier, stepPath, attemptTier, currentDescriptionHash, failureReason); metaErr != nil {
							hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to record adaptive tier failure for step %d: %v", stepIndex+1, metaErr))
						}
						adaptiveTier = TierHigh
						adaptiveTierReason = "high (medium-tier validation failed in this run)"
					}

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
						hcpo.GetLogger().Info(fmt.Sprintf("🔄 Retrying step %d execution with validation feedback", stepIndex+1))
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

			// Exit immediately if execution failed after exhausting all retry attempts.
			// Without this check, sub-agent execution errors are silently swallowed:
			// the error is non-nil but the function returns nil error at the end,
			// causing the orchestrator to think the sub-agent completed successfully.
			if err != nil {
				hcpo.GetLogger().Error(fmt.Sprintf("🛑 Step %d execution failed after %d attempts - propagating error", stepIndex+1, maxRetryAttempts), err)
				return executionResult, updatedContextFiles, fmt.Errorf("step %d execution failed after %d retry attempts: %w", stepIndex+1, maxRetryAttempts, err)
			}

		} // End of main execution block

		// Learn code mode: save the newly written main.py to learnings (only when LLM generated it)
		// Skip if code is locked — the user froze the script intentionally.
		isCodeLockedForSave := agentConfigs != nil && agentConfigs.LockCode != nil && *agentConfigs.LockCode
		if isScriptedMode && learnCodeScriptNeedsSaving && !isCodeLockedForSave {
			hcpo.saveScriptedScriptToLearnings(step, toAbsPath(stepExecutionPath))
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

	// step_finished (status="end") is emitted by the defer at the top of this
	// function, which covers this success path AND every early return.
	return executionResult, updatedContextFiles, nil
}

// ============================================================================
// STEP TYPE DETECTION HELPERS (for PlanStepInterface)
// ============================================================================
// These helper functions provide a cleaner way to detect step types from PlanStepInterface
// boolean flags, making the execution routing logic more maintainable and
// preparing for future migration to type-safe step types.

// isHumanInputStep returns true if the step is a human input step (asks question and blocks for input)
func isHumanInputStep(step PlanStepInterface) bool {
	_, ok := step.(*HumanInputPlanStep)
	return ok
}

func summarizeExecutionResultForNotification(result string) string {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return ""
	}
	if summary := lastNonEmptyLine(trimmed); summary != "" {
		return summary
	}
	return trimmed
}

func buildDirectModeCompletionSummary(mainSummary, kbSummary, learningsSummary string) string {
	var parts []string
	if s := strings.TrimSpace(mainSummary); s != "" {
		parts = append(parts, fmt.Sprintf("Execution: %s", s))
	}
	if s := strings.TrimSpace(kbSummary); s != "" {
		parts = append(parts, fmt.Sprintf("KB review: %s", s))
	}
	if s := strings.TrimSpace(learningsSummary); s != "" {
		parts = append(parts, fmt.Sprintf("Learnings: %s", s))
	}
	return strings.Join(parts, "\n")
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

func isMessageSequenceStep(step PlanStepInterface) bool {
	_, ok := step.(*MessageSequencePlanStep)
	return ok
}

// getAgentConfigs returns AgentConfigs from a PlanStepInterface
func getAgentConfigs(step PlanStepInterface) *AgentConfigs {
	switch s := step.(type) {
	case *RegularPlanStep:
		return s.AgentConfigs
	case *TodoTaskPlanStep:
		return s.AgentConfigs
	case *HumanInputPlanStep:
		return s.AgentConfigs
	case *EvaluationStep:
		return s.AgentConfigs
	case *RoutingPlanStep:
		return s.AgentConfigs
	case *MessageSequencePlanStep:
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

	// Route conversations (msgSeqRoutes) are scoped to this execution phase.
	// Drain them — and close their runtimes — however the phase exits, so a
	// reused orchestrator instance starts the next iteration/run with fresh
	// route memory instead of resuming this run's conversations.
	defer hcpo.clearAllMsgSeqRouteSessions("execution phase finished")

	if err := hcpo.seedRouteSelectionsForRun(ctx, breakdownSteps); err != nil {
		return fmt.Errorf("failed to seed route selections: %w", err)
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
				SetCurrentStepContext(stepID, stepType string)
			}); ok {
				stepBridge.SetCurrentStepContext(stepID, string(step.StepType()))
			} else if stepBridge, ok := bridge.(interface {
				SetCurrentStepID(stepID string)
			}); ok {
				stepBridge.SetCurrentStepID(stepID)
			}
			// Attach rich step context (name, index, total, parent, attempt,
			// triggered_by) so the terminal pane's meta row can render
			// "step 3/7 · attempt 1 · triggered by workflow_executor".
			// Don't set ParentStepID to the previous step — that's
			// data-flow semantics ("step 3 consumes step 2's output"),
			// but the frontend's terminal tree builder uses parent_step_id
			// for hierarchy and would render sequential steps as an
			// ever-deepening chain (step1 → step2 → step3 nested under
			// each other) instead of siblings at the workflow level.
			// ParentStepID is reserved for true sub-agent / nested
			// spawning relationships (see controller_todo_task.go,
			// planning_agent.go), not sequential order.
			if cab, ok := bridge.(*orchestrator.ContextAwareEventBridge); ok {
				rich := orchestrator.RichStepContext{
					StepName:    step.GetTitle(),
					StepType:    string(step.StepType()),
					StepIndex:   i + 1,
					StepTotal:   len(breakdownSteps),
					Attempt:     1,
					TriggeredBy: "workflow_executor",
				}
				cab.SetRichStepContext(rich)
			}
		}

		// Route execution based on step type using helper functions
		// Check if this is a routing step
		if isRoutingStep(step) {
			hcpo.GetLogger().Info(fmt.Sprintf("🔀 Starting routing step execution: %s", step.GetTitle()))
			selectedRouteID, _, err := hcpo.executeRoutingStep(ctx, step, i, progress, previousContextFiles, iteration, execCtx, breakdownSteps, previousExecutionResults)
			if err != nil {
				if isWorkflowCancellationErr(ctx, err) {
					hcpo.GetLogger().Info(fmt.Sprintf("Routing step %d canceled", i+1))
					return err
				}
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

			// Track routing evaluations to prevent infinite loops
			if progress.RoutingEvaluationCounts == nil {
				progress.RoutingEvaluationCounts = make(RoutingEvaluationCount)
			}
			routingKey := fmt.Sprintf("%s:%s", step.GetID(), selectedRouteID)
			currentCount := progress.RoutingEvaluationCounts[routingKey]
			newCount := currentCount + 1
			progress.RoutingEvaluationCounts[routingKey] = newCount
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

			// Handle next step navigation. The per-route evaluation count above is
			// routing's own loop guard, so the generic jump-repeat guard is disabled
			// (maxRepeats=0).
			outcome, navErr := hcpo.navigateToNextStepID(ctx, step.GetID(), nextStepID, breakdownSteps, progress, &i, &startFromStep, 0)
			if navErr != nil {
				return navErr
			}
			if outcome == "end" {
				hcpo.GetLogger().Info(fmt.Sprintf("🏁 Routing step %d specified 'end' - terminating workflow", i+1))
				break
			}
			continue
		}

		// Check if this is a todo task step
		if isTodoTaskStep(step) {
			// Execute todo task step - manages todo list and delegates to sub-agents
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Starting todo task step execution: %s", step.GetTitle()))
			// Generate step path for todo task step
			todoTaskStepPath := fmt.Sprintf("step-%d", i+1)

			successCriteriaMet, nextStepID, err := hcpo.executeTodoTaskStep(ctx, step, i, progress, previousContextFiles, previousExecutionResults, iteration, execCtx, breakdownSteps, todoTaskStepPath)
			if err != nil {
				if isWorkflowCancellationErr(ctx, err) {
					hcpo.GetLogger().Info(fmt.Sprintf("Todo task step %d canceled", i+1))
					return err
				}
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

			// Handle next step navigation. Identical source→target jumps are capped:
			// the orchestrator is LLM-driven, so an unbounded next_step_id cycle
			// burns tokens on every pass.
			outcome, navErr := hcpo.navigateToNextStepID(ctx, step.GetID(), nextStepID, breakdownSteps, progress, &i, &startFromStep, maxLLMJumpRepeats)
			if navErr != nil {
				return navErr
			}
			if outcome == "end" {
				hcpo.GetLogger().Info(fmt.Sprintf("🏁 Todo task step %d specified 'end' - terminating workflow", i+1))
				break
			}
			// "jump" repointed i; "none" falls through to the next sequential step.
			continue
		}

		if isMessageSequenceStep(step) {
			hcpo.GetLogger().Info(fmt.Sprintf("💬 Starting message sequence step execution: %s", step.GetTitle()))
			stepPath := fmt.Sprintf("step-%d", i+1)
			callOptions := messageSequenceCallOptions{
				Source: "configured_queue",
			}
			if execCtx != nil {
				callOptions.Restart = execCtx.MessageSequenceRestart
				if strings.TrimSpace(execCtx.WorkshopHumanInput) != "" {
					callOptions.Source = "builder_resume"
					callOptions.ReentryMessage = execCtx.WorkshopHumanInput
				}
			}
			executionResult, _, err := hcpo.executeMessageSequenceStep(ctx, step, i, stepPath, progress, execCtx, breakdownSteps, callOptions)
			if err != nil {
				if isWorkflowCancellationErr(ctx, err) {
					hcpo.GetLogger().Info(fmt.Sprintf("Message sequence step %d canceled", i+1))
					return err
				}
				hcpo.GetLogger().Error(fmt.Sprintf("❌ Message sequence step %d execution failed: %v", i+1, err), nil)
				hcpo.EmitOrchestratorAgentError(ctx, "workflow", "message-sequence-step-execution", fmt.Sprintf("Execute message sequence step: %s", step.GetTitle()), err.Error(), i, iteration)
				return fmt.Errorf("message sequence step %d execution failed: %w", i+1, err)
			}
			previousExecutionResults = append(previousExecutionResults, executionResult)
			hcpo.addCompletedStepIndex(progress, i)
			if err := hcpo.saveStepProgress(ctx, progress); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to save progress after message sequence step: %v", err))
			}
			if hcpo.runSingleStepOnly && i == hcpo.singleStepTarget {
				hcpo.GetLogger().Info(fmt.Sprintf("🎯 Single step mode: completed target step %d, stopping execution", i+1))
				hcpo.SetRunSingleStepMode(false, -1)
				break
			}
			// Honor next_step_id so a branch converges to its shared downstream step
			// (skipping the sibling branches that sit after it in the list) instead of
			// falling through in list order. Without this, after a router selected one
			// branch the selected branch ran but then execution spilled into the next
			// (non-selected) branch — see routing convergence.
			if seqStep, ok := step.(*MessageSequencePlanStep); ok && strings.TrimSpace(seqStep.NextStepID) != "" {
				outcome, navErr := hcpo.navigateToNextStepID(ctx, step.GetID(), seqStep.NextStepID, breakdownSteps, progress, &i, &startFromStep, maxLLMJumpRepeats)
				if navErr != nil {
					return navErr
				}
				if outcome == "end" {
					hcpo.GetLogger().Info(fmt.Sprintf("🏁 message_sequence step %d next_step_id=end - terminating workflow", i+1))
					break
				}
			}
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
				if isWorkflowCancellationErr(ctx, err) {
					hcpo.GetLogger().Info(fmt.Sprintf("Human input step %d canceled", i+1))
					return err
				}
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

			// Handle next step navigation. This now goes through the same shared
			// helper as routing/todo_task — human_input jumps previously skipped
			// progress cleanup and execution-folder archival entirely, which let
			// converged steps read stale artifacts from a prior pass. The repeat
			// cap is generous: each pass blocks on a human response, so loops are
			// self-limiting and the guard is only a failsafe.
			outcome, navErr := hcpo.navigateToNextStepID(ctx, step.GetID(), nextStepID, breakdownSteps, progress, &i, &startFromStep, maxHumanJumpRepeats)
			if navErr != nil {
				return navErr
			}
			if outcome == "end" {
				hcpo.GetLogger().Info(fmt.Sprintf("🏁 Human input step %d specified 'end' - terminating workflow", i+1))
				break
			}
			// "jump" repointed i; "none" falls through to the next sequential step.
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
// readGlobalLearningHistory reads the global workflow-level learning history.
// Returns a file-path reference string (not full content) and any error.
func (hcpo *StepBasedWorkflowOrchestrator) readGlobalLearningHistory(
	ctx context.Context,
) (formattedLearningHistory string, err error) {
	hcpo.GetLogger().Info("🔀 Reading global learning history")

	globalLearningsPath := hcpo.getLearningsBasePath() + "/" + GlobalLearningID

	learningFiles, err := hcpo.listLearningManifestFiles(ctx, globalLearningsPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read global learning files from %s: %v - will proceed without learning history", globalLearningsPath, err))
		return "", nil
	}
	if len(learningFiles) > 0 {
		docsRoot := GetPromptDocsRoot()
		absLearningsPath := filepath.Join(docsRoot, hcpo.GetWorkspacePath(), globalLearningsPath)

		var listedFiles strings.Builder
		for _, filename := range learningFiles {
			listedFiles.WriteString("- `")
			listedFiles.WriteString(filename)
			listedFiles.WriteString("`\n")
		}

		formattedLearningHistory = fmt.Sprintf(
			"📚 **Workflow skill available** at `%s/`.\n"+
				"These files capture best practices and reusable patterns from previous successful runs.\n"+
				"Before executing this step, read the relevant workflow skill file(s) you need.\n"+
				"`SKILL.md` is the index and will point you to the right detailed files.\n\n"+
				"Available files:\n%s"+
				"\nThen read the relevant files from `references/`, `scripts/`, `code/`, or other listed paths needed to execute this step.",
			absLearningsPath, listedFiles.String())
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Found %d global learning file(s) (path reference only)", len(learningFiles)))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ No global learning files found: %s", globalLearningsPath))
		formattedLearningHistory = ""
	}

	return formattedLearningHistory, nil
}

// buildValidationContinuationUserMessage formats a pre-validation failure as a
// follow-up user message so the existing execution agent can fix the issues
// in-place rather than re-running the entire step from a fresh conversation.
// The agent already has the full task context in its system prompt + prior
// turns, so the message only needs to carry the new failure information.
func buildValidationContinuationUserMessage(vr *ValidationResponse, attempt int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Pre-validation failed (retry attempt %d)\n\n", attempt))
	if vr != nil {
		if vr.Reasoning != "" {
			sb.WriteString(vr.Reasoning)
			sb.WriteString("\n\n")
		}
		if len(vr.Feedback) > 0 {
			sb.WriteString("**Feedback:**\n")
			for _, fb := range vr.Feedback {
				sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", fb.Severity, fb.Type, fb.Description))
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("Fix the specific issues above and re-produce the required outputs. ")
	sb.WriteString("Do not restart from scratch — your prior tool calls and outputs are still valid where they passed; only address the listed failures.")
	return sb.String()
}
