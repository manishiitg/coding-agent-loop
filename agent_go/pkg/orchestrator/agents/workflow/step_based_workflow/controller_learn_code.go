package step_based_workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/common"
	"mcp-agent-builder-go/agent_go/pkg/workspace"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// cleanStepOutputDir deletes all output files/folders in the step execution directory.
// preserveDirs lists subdirectory names to skip (e.g., "code" when main.py is inside it).
// Called before the controller re-runs main.py so pre-validation tests fresh output only.
func (hcpo *StepBasedWorkflowOrchestrator) cleanStepOutputDir(ctx context.Context, stepExecutionRelPath string, preserveDirs ...string) {
	entries, listErr := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, stepExecutionRelPath)
	if listErr != nil {
		return // folder doesn't exist yet — nothing to clean
	}
	preserveSet := make(map[string]bool, len(preserveDirs))
	for _, d := range preserveDirs {
		preserveSet[d] = true
	}
	workspacePath := hcpo.GetWorkspacePath()
	docsRoot := GetPromptDocsRoot()
	for _, entry := range entries {
		name := filepath.Base(entry)
		if name == "" || name == "." || preserveSet[name] {
			continue
		}
		entryRelPath := stepExecutionRelPath + "/" + name
		// Build absolute path for folder-delete API, accounting for the fact that
		// stepExecutionRelPath may already include the workspacePath prefix.
		var absPath string
		if strings.HasPrefix(entryRelPath, workspacePath+"/") || entryRelPath == workspacePath {
			absPath = filepath.Join(docsRoot, entryRelPath)
		} else {
			absPath = filepath.Join(docsRoot, workspacePath, entryRelPath)
		}
		// Try file delete first; if it fails (directory entries return error from /api/documents/),
		// fall back to folder delete via /api/folders/.
		fileDelErr := hcpo.DeleteWorkspaceFile(ctx, entryRelPath)
		if fileDelErr != nil {
			if folderErr := deleteFolderViaAPI(ctx, absPath); folderErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] cleanStepOutputDir: failed to delete %s: file=%v folder=%v", name, fileDelErr, folderErr))
			}
		}
	}
}

// LearnCodeMetadata persists script provenance and runtime statistics.
type LearnCodeMetadata struct {
	StepID         string         `json:"step_id"`
	ScriptVersion  int            `json:"script_version"` // incremented each time the LLM rewrites the script
	CreatedAt      string         `json:"created_at"`
	LastRunAt      string         `json:"last_run_at"`
	TotalRuns      int            `json:"total_runs"`
	SuccessfulRuns map[string]int `json:"successful_runs"` // per-mode success counts; canonical scripted key is "code_exec"
	FailedRuns     int            `json:"failed_runs"`
	RelearnCount   int            `json:"relearn_count"` // how many times the LLM had to rewrite
}

// LearnCodeFastPathResult is returned by tryRunSavedLearnCodeScript.
type LearnCodeFastPathResult struct {
	RanScript       bool   // true if a saved script was found and attempted
	Success         bool   // true if script ran and validation passed
	ExitCode        int    // actual Python exit code (0 = success, -1 = not-run/API error)
	Output          string // combined stdout from script
	Error           string // stderr / validation error (used as relearn context for LLM)
	ExecutionError  string // raw script execution failure, if any
	ValidationError string // output/pre-validation failure, if any
	FailureReason   string // "execution_error", "validation_error", or "execution_and_validation_error"
	ExistingScript  string // old script content (for LLM relearn prompt)
}

type learnCodeSelfRunInfo struct {
	Output   string
	ExitCode int
}

func parseExecuteShellCommandArgs(argsJSON string) string {
	if strings.TrimSpace(argsJSON) == "" {
		return ""
	}
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	return strings.TrimSpace(args.Command)
}

func parseExecuteShellCommandResult(result string) (stdout, stderr string, exitCode int, ok bool) {
	if strings.TrimSpace(result) == "" {
		return "", "", 0, false
	}
	var parsed struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		return "", "", 0, false
	}
	return parsed.Stdout, parsed.Stderr, parsed.ExitCode, true
}

func isReadOnlyMainPyCommand(command, mainPyAbsPath string) bool {
	cmd := strings.TrimSpace(command)
	if cmd == "" || !strings.Contains(cmd, mainPyAbsPath) {
		return false
	}
	readPrefixes := []string{
		"cat " + shellQuotePath(mainPyAbsPath),
		"cat " + mainPyAbsPath,
		"sed -n ",
		"nl -ba ",
		"ls ",
		"stat ",
		"wc ",
		"head ",
		"tail ",
	}
	for _, prefix := range readPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			return true
		}
	}
	return false
}

func detectSuccessfulLLMLearnCodeSelfRun(history []llmtypes.MessageContent, mainPyAbsPath string) *learnCodeSelfRunInfo {
	type shellCall struct {
		command string
		index   int
	}
	pendingShellCalls := map[string]shellCall{}
	lastSuccessfulRun := -1
	lastSuccessfulOutput := ""
	lastSuccessfulExitCode := 0
	lastMainPyMutation := -1
	callIndex := 0

	for _, msg := range history {
		switch msg.Role {
		case llmtypes.ChatMessageTypeAI:
			for _, part := range msg.Parts {
				toolCall, ok := part.(llmtypes.ToolCall)
				if !ok || toolCall.FunctionCall == nil {
					continue
				}
				callIndex++
				switch toolCall.FunctionCall.Name {
				case "execute_shell_command", "mcp_api-bridge_execute_shell_command":
					command := parseExecuteShellCommandArgs(toolCall.FunctionCall.Arguments)
					pendingShellCalls[toolCall.ID] = shellCall{command: command, index: callIndex}
					if strings.Contains(command, mainPyAbsPath) &&
						!strings.Contains(command, "python3 "+shellQuotePath(mainPyAbsPath)) &&
						!strings.Contains(command, "python3 "+mainPyAbsPath) &&
						!isReadOnlyMainPyCommand(command, mainPyAbsPath) {
						lastMainPyMutation = callIndex
					}
				case "diff_patch_workspace_file", "mcp_api-bridge_diff_patch_workspace_file":
					var args struct {
						Filepath string `json:"filepath"`
					}
					if err := json.Unmarshal([]byte(toolCall.FunctionCall.Arguments), &args); err == nil && strings.TrimSpace(args.Filepath) == mainPyAbsPath {
						lastMainPyMutation = callIndex
					}
				}
			}
		case llmtypes.ChatMessageTypeTool:
			for _, part := range msg.Parts {
				toolResp, ok := part.(llmtypes.ToolCallResponse)
				if !ok {
					continue
				}
				call, exists := pendingShellCalls[toolResp.ToolCallID]
				if !exists {
					continue
				}
				stdout, stderr, exitCode, ok := parseExecuteShellCommandResult(toolResp.Content)
				if !ok {
					continue
				}
				if exitCode == 0 &&
					(strings.Contains(call.command, "python3 "+shellQuotePath(mainPyAbsPath)) ||
						strings.Contains(call.command, "python3 "+mainPyAbsPath)) {
					lastSuccessfulRun = call.index
					lastSuccessfulExitCode = exitCode
					lastSuccessfulOutput = strings.TrimSpace(stdout)
					if strings.TrimSpace(stderr) != "" {
						if lastSuccessfulOutput != "" {
							lastSuccessfulOutput += "\n"
						}
						lastSuccessfulOutput += strings.TrimSpace(stderr)
					}
				}
			}
		}
	}

	if lastSuccessfulRun == -1 || lastMainPyMutation > lastSuccessfulRun {
		return nil
	}
	return &learnCodeSelfRunInfo{
		Output:   lastSuccessfulOutput,
		ExitCode: lastSuccessfulExitCode,
	}
}

// extractLastMainPyRunOutput returns the output from the last execution of main.py
// in the conversation history, regardless of exit code. This is used to provide
// execution context to repair agents that start with a fresh conversation.
func extractLastMainPyRunOutput(history []llmtypes.MessageContent, mainPyAbsPath string) (output string, exitCode int, found bool) {
	type shellCall struct {
		command string
	}
	pendingShellCalls := map[string]shellCall{}
	var lastOutput string
	var lastExitCode int
	lastFound := false

	for _, msg := range history {
		switch msg.Role {
		case llmtypes.ChatMessageTypeAI:
			for _, part := range msg.Parts {
				toolCall, ok := part.(llmtypes.ToolCall)
				if !ok || toolCall.FunctionCall == nil {
					continue
				}
				switch toolCall.FunctionCall.Name {
				case "execute_shell_command", "mcp_api-bridge_execute_shell_command":
					command := parseExecuteShellCommandArgs(toolCall.FunctionCall.Arguments)
					pendingShellCalls[toolCall.ID] = shellCall{command: command}
				}
			}
		case llmtypes.ChatMessageTypeTool:
			for _, part := range msg.Parts {
				toolResp, ok := part.(llmtypes.ToolCallResponse)
				if !ok {
					continue
				}
				call, exists := pendingShellCalls[toolResp.ToolCallID]
				if !exists {
					continue
				}
				if strings.Contains(call.command, "python3 "+shellQuotePath(mainPyAbsPath)) ||
					strings.Contains(call.command, "python3 "+mainPyAbsPath) {
					stdout, stderr, ec, ok := parseExecuteShellCommandResult(toolResp.Content)
					if !ok {
						continue
					}
					combined := strings.TrimSpace(stdout)
					if strings.TrimSpace(stderr) != "" {
						if combined != "" {
							combined += "\n"
						}
						combined += strings.TrimSpace(stderr)
					}
					lastOutput = combined
					lastExitCode = ec
					lastFound = true
				}
			}
		}
	}

	return lastOutput, lastExitCode, lastFound
}

// reviewMainPyScript performs static analysis on a main.py script to catch common
// anti-patterns that would cause failures when the script is reused across groups.
// declaredEnvVars is the list of env var names actually available to the script (e.g. VAR_USER_ID, SECRET_PASSWORD).
// If nil, the VAR_*/SECRET_* check is skipped (can't distinguish declared vs invented vars).
// Returns a list of human-readable issues. Empty list means the script looks clean.
func reviewMainPyScript(script string, declaredEnvVars ...string) []string {
	var issues []string
	lines := strings.Split(script, "\n")

	// Build set of declared env vars for quick lookup
	declaredSet := map[string]bool{}
	for _, v := range declaredEnvVars {
		declaredSet[v] = true
	}

	// Precompile patterns
	reHardcodedRunPath := regexp.MustCompile(`['"]/?app/workspace-docs/.*/runs/iteration-\d+/[^/'"]*/execution`)
	reHardcodedWorkspacePath := regexp.MustCompile(`['"]/?app/workspace-docs/[^'"]{20,}['"]`)
	reEnvGetWithDefault := regexp.MustCompile(`os\.environ\.get\(\s*['"][^'"]+['"]\s*,\s*['"][^'"]+['"]\s*\)`)
	reEnvGetVarName := regexp.MustCompile(`os\.environ\.get\(\s*['"]((?:VAR_|SECRET_)[^'"]+)['"]\s*,\s*['"][^'"]+['"]\s*\)`)
	reSiblingStepPath := regexp.MustCompile(`/execution/[a-z][\w-]+/`)
	reStepOutputDirUsed := regexp.MustCompile(`os\.environ\[['"]STEP_OUTPUT_DIR['"]\]|STEP_OUTPUT_DIR`)
	reStepExecDirUsed := regexp.MustCompile(`os\.environ\[['"]STEP_EXECUTION_DIR['"]\]|STEP_EXECUTION_DIR`)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip comments
		if strings.HasPrefix(trimmed, "#") {
			continue
		}

		lineNum := i + 1

		// Check 1: Hardcoded execution paths with group names
		if reHardcodedRunPath.MatchString(line) {
			issues = append(issues, fmt.Sprintf(
				"Line %d: Hardcoded execution path with group name. Use `os.environ['STEP_EXECUTION_DIR']` instead. Found: %s",
				lineNum, strings.TrimSpace(line)))
		}

		// Check 2: os.environ.get with hardcoded fallback for VAR_*/SECRET_* — only flag if the var is actually declared
		if matches := reEnvGetVarName.FindStringSubmatch(line); len(matches) > 1 {
			varName := matches[1]
			if len(declaredSet) > 0 && declaredSet[varName] {
				// Declared variable used with fallback — should use os.environ['KEY'] instead
				issues = append(issues, fmt.Sprintf(
					"Line %d: Using os.environ.get() with hardcoded fallback for declared variable %s. Use `os.environ['%s']` (no default) so missing vars fail loudly. Found: %s",
					lineNum, varName, varName, strings.TrimSpace(line)))
			}
			// If not in declaredSet, the script invented this var — don't flag it
		}

		// Check 3: os.environ.get with any hardcoded default (weaker signal, only flag for known env vars)
		if !reEnvGetVarName.MatchString(line) && reEnvGetWithDefault.MatchString(line) {
			// Check if it's for a known step env var
			if strings.Contains(line, "STEP_OUTPUT_DIR") || strings.Contains(line, "STEP_EXECUTION_DIR") ||
				strings.Contains(line, "MCP_API_URL") || strings.Contains(line, "MCP_API_TOKEN") {
				issues = append(issues, fmt.Sprintf(
					"Line %d: Using os.environ.get() with fallback for a required env var. Use `os.environ['KEY']` instead. Found: %s",
					lineNum, strings.TrimSpace(line)))
			}
		}
	}

	// Check 4: Script references sibling step folders but doesn't use STEP_EXECUTION_DIR
	hasSiblingRef := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if reSiblingStepPath.MatchString(line) && !strings.Contains(line, "STEP_OUTPUT_DIR") && !strings.Contains(line, "STEP_EXECUTION_DIR") {
			hasSiblingRef = true
			break
		}
	}
	if hasSiblingRef && !reStepExecDirUsed.MatchString(script) {
		issues = append(issues, "Script references sibling step folders via hardcoded paths. Use `os.environ['STEP_EXECUTION_DIR']` to build paths to other steps (e.g., `os.path.join(os.environ['STEP_EXECUTION_DIR'], 'other-step/output.json')`).")
	}

	// Check 5: Long hardcoded workspace paths (even if not matching the run pattern)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if reHardcodedWorkspacePath.MatchString(line) && !reHardcodedRunPath.MatchString(line) {
			// Only flag if not using an env var on the same line
			if !reStepOutputDirUsed.MatchString(line) && !reStepExecDirUsed.MatchString(line) {
				issues = append(issues, fmt.Sprintf(
					"Line %d: Long hardcoded workspace path detected. Derive paths from environment variables (STEP_OUTPUT_DIR, STEP_EXECUTION_DIR) instead. Found: %s",
					i+1, strings.TrimSpace(line)))
			}
		}
	}

	// Check 6: Script writes output but doesn't use STEP_OUTPUT_DIR
	reOpenWrite := regexp.MustCompile(`open\s*\(\s*['"][^'"]+['"].*['"]w['"]`)
	hasWriteWithoutEnv := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if reOpenWrite.MatchString(line) && !reStepOutputDirUsed.MatchString(line) && !strings.Contains(line, "output_dir") && !strings.Contains(line, "OUTPUT_DIR") {
			hasWriteWithoutEnv = true
			break
		}
	}
	if hasWriteWithoutEnv && !reStepOutputDirUsed.MatchString(script) {
		issues = append(issues, "Script writes files but never references STEP_OUTPUT_DIR. Output files must be written to `os.environ['STEP_OUTPUT_DIR']`.")
	}

	// Check 7: Script has sys.argv references but builds sibling paths manually instead
	reSysArgv := regexp.MustCompile(`sys\.argv\[`)
	reManualSiblingPath := regexp.MustCompile(`os\.path\.join\s*\(.*execution.*,\s*['"][a-z][\w-]+`)
	if reSysArgv.MatchString(script) && reManualSiblingPath.MatchString(script) {
		issues = append(issues, "Script receives input via sys.argv but also constructs manual paths to sibling step folders. Read ALL input data from sys.argv — do not build sibling paths manually. If you need additional input, add it as a context_dependency in the plan.")
	}

	return issues
}

// getLearnCodeDirRelPath returns the learnings subdirectory (relative to workspace root).
func getLearnCodeDirRelPath(stepID string, isEvalMode bool) string {
	if isEvalMode {
		return fmt.Sprintf("evaluation/learnings/%s", stepID)
	}
	return fmt.Sprintf("learnings/%s", stepID)
}

// getLearnCodeScriptAbsPath returns the absolute path to the saved main.py.
// NOTE: Only used for passing paths to execLearnCodeScript (workspace API receives abs paths inside Docker).
func getLearnCodeScriptAbsPath(docsRoot, workspacePath, stepID string, isEvalMode bool) string {
	return filepath.Join(docsRoot, workspacePath, getLearnCodeDirRelPath(stepID, isEvalMode), "main.py")
}

// readLearnCodeMetadataAPI reads script_metadata.json via the workspace API.
// Returns nil if the file is missing or cannot be parsed.
func (hcpo *StepBasedWorkflowOrchestrator) readLearnCodeMetadataAPI(ctx context.Context, stepID string) *LearnCodeMetadata {
	relPath := getLearnCodeDirRelPath(stepID, hcpo.isEvaluationMode) + "/script_metadata.json"
	data, err := hcpo.ReadWorkspaceFile(ctx, relPath)
	if err != nil {
		return nil
	}
	var meta LearnCodeMetadata
	if err := json.Unmarshal([]byte(data), &meta); err == nil {
		if meta.SuccessfulRuns == nil {
			meta.SuccessfulRuns = map[string]int{}
		}
		if meta.SuccessfulRuns["code_exec"] == 0 && meta.SuccessfulRuns["learn_code"] > 0 {
			meta.SuccessfulRuns["code_exec"] = meta.SuccessfulRuns["learn_code"]
		}
		return &meta
	}
	// Legacy format: successful_runs was a plain int, not a map.
	var legacy struct {
		StepID         string `json:"step_id"`
		ScriptVersion  int    `json:"script_version"`
		CreatedAt      string `json:"created_at"`
		LastRunAt      string `json:"last_run_at"`
		TotalRuns      int    `json:"total_runs"`
		SuccessfulRuns int    `json:"successful_runs"`
		FailedRuns     int    `json:"failed_runs"`
		RelearnCount   int    `json:"relearn_count"`
	}
	if err := json.Unmarshal([]byte(data), &legacy); err != nil {
		return nil
	}
	return &LearnCodeMetadata{
		StepID:         legacy.StepID,
		ScriptVersion:  legacy.ScriptVersion,
		CreatedAt:      legacy.CreatedAt,
		LastRunAt:      legacy.LastRunAt,
		TotalRuns:      legacy.TotalRuns,
		SuccessfulRuns: map[string]int{"code_exec": legacy.SuccessfulRuns},
		FailedRuns:     legacy.FailedRuns,
		RelearnCount:   legacy.RelearnCount,
	}
}

// writeLearnCodeMetadataAPI writes script_metadata.json via the workspace API.
func (hcpo *StepBasedWorkflowOrchestrator) writeLearnCodeMetadataAPI(ctx context.Context, stepID string, meta LearnCodeMetadata) error {
	relPath := getLearnCodeDirRelPath(stepID, hcpo.isEvaluationMode) + "/script_metadata.json"
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return hcpo.WriteWorkspaceFile(ctx, relPath, string(data))
}

// hasValidLearnedScriptAPI returns true if main.py exists via workspace API.
func (hcpo *StepBasedWorkflowOrchestrator) hasValidLearnedScriptAPI(ctx context.Context, stepID string) bool {
	scriptRelPath := getLearnCodeDirRelPath(stepID, hcpo.isEvaluationMode) + "/main.py"
	_, err := hcpo.ReadWorkspaceFile(ctx, scriptRelPath)
	return err == nil
}

// buildLearnCodeVarMappingForPrompt returns a newline-joined list showing how workflow
// variables map to env vars, e.g. "{{TARGET_USER}} → os.environ['SECRET_TARGET_USER']".
// Returns "" when learn_code mode is disabled or there are no variables.
func buildLearnCodeVarMappingForPrompt(isLearnCodeMode bool, manifest *VariablesManifest) string {
	if !isLearnCodeMode || manifest == nil || len(manifest.Variables) == 0 {
		return ""
	}
	lines := make([]string, 0, len(manifest.Variables))
	for _, v := range manifest.Variables {
		lines = append(lines, fmt.Sprintf("{{%s}} → os.environ['VAR_%s']", v.Name, v.Name))
	}
	return strings.Join(lines, "\n")
}

// buildLearnCodeEnvVarNamesForPrompt returns newline-joined env var names for templateVars.
// Returns "" when learn_code mode is disabled.
func buildLearnCodeEnvVarNamesForPrompt(isLearnCodeMode bool, workspaceEnvRef map[string]string) string {
	if !isLearnCodeMode {
		return ""
	}
	names := []string{"STEP_OUTPUT_DIR", "MCP_API_URL"}
	for k := range workspaceEnvRef {
		if k != "MCP_API_URL" { // avoid duplicate
			names = append(names, k)
		}
	}
	sort.Strings(names[2:]) // sort the SECRET_*/VAR_* part
	return strings.Join(names, "\n")
}

// buildLearnCodeInputArgs returns absolute paths to context_dependency files as positional args.
// These become: python3 main.py <input1> <input2> ...
func (hcpo *StepBasedWorkflowOrchestrator) buildLearnCodeInputArgs(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	allSteps []PlanStepInterface,
	executionWorkspacePath string,
	docsRoot string,
	variableValues map[string]string,
) []string {
	deps := step.GetContextDependencies()
	if len(deps) == 0 {
		return nil
	}
	resolved := ResolveVariablesArray(deps, variableValues)
	return hcpo.resolveDependencyPathsWithWorkspace(ctx, resolved, stepIndex, stepPath, allSteps, executionWorkspacePath, docsRoot, variableValues)
}

// shellQuotePath wraps an absolute path in single quotes for safe shell embedding.
func shellQuotePath(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (hcpo *StepBasedWorkflowOrchestrator) resolveLearnCodeShellGuard(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	stepExecutionRelPath string,
	includeCodeDir bool,
) *workspace.FolderGuardConfig {
	stepConfig := getAgentConfigs(step)
	useKnowledgebase := hcpo.UseKnowledgebase()
	if stepConfig != nil && stepConfig.DisableKnowledgebase != nil {
		if *stepConfig.DisableKnowledgebase {
			useKnowledgebase = false
		} else {
			useKnowledgebase = true
		}
	}

	readPaths, writePaths := hcpo.setupExecutionFolderGuard(stepPath, step.GetID(), useKnowledgebase)
	if includeCodeDir && len(writePaths) > 0 {
		writePaths = append(writePaths, writePaths[0]+"/code")
	}
	readPaths = common.DeduplicateStrings(append(readPaths, writePaths...))

	return &workspace.FolderGuardConfig{
		Enabled:    true,
		ReadPaths:  readPaths,
		WritePaths: writePaths,
	}
}

// execLearnCodeScript runs python3 <mainPy> <args...> via the workspace shell API.
// Uses workspace.Client (same path as the LLM agent's execute_shell_command tool) so that
// folder guard, ExtraEnv (SECRET_*, MCP_API_URL), and path handling all work consistently.
//
// workDirAbsPath  — absolute path to the script's working directory (code/ or learnings/{id}/)
// stepOutputAbsPath — absolute path for STEP_OUTPUT_DIR env var (execution/step-N/)
// stepExecutionRelPath — workspace-relative path to the execution folder (for FolderGuard write paths)
func (hcpo *StepBasedWorkflowOrchestrator) execLearnCodeScript(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	mainPyAbsPath string,
	inputArgs []string,
	stepOutputAbsPath string,
	workDirAbsPath string,
	stepExecutionRelPath string,
) (output string, exitCode int, execErr error) {
	docsRoot := GetPromptDocsRoot() // /app/workspace-docs

	// Convert absolute workDir to workspace-relative for WorkingDirectory field.
	// The workspace API prepends docsRoot, so passing the absolute path would double it.
	workDirRel := strings.TrimPrefix(workDirAbsPath, docsRoot+"/")
	if workDirRel == workDirAbsPath {
		// Didn't start with docsRoot — fall back to empty (workspace root)
		workDirRel = ""
	}

	// Build command: STEP_OUTPUT_DIR is passed via ExtraEnv so no inline env prefix needed.
	// Use absolute paths for main.py and args — /app/workspace-docs/... is valid inside Docker.
	var sb strings.Builder
	sb.WriteString("python3 -B ")
	sb.WriteString(shellQuotePath(mainPyAbsPath))
	for _, arg := range inputArgs {
		sb.WriteString(" ")
		sb.WriteString(shellQuotePath(arg))
	}

	includeCodeDir := workDirRel == stepExecutionRelPath+"/code"
	guard := hcpo.resolveLearnCodeShellGuard(ctx, step, stepIndex, stepPath, stepExecutionRelPath, includeCodeDir)

	// ExtraEnv: merge workspace env (SECRET_*, MCP_API_URL) with STEP_OUTPUT_DIR and STEP_EXECUTION_DIR.
	stepExecutionAbsPath := filepath.Dir(stepOutputAbsPath)
	extraEnv := map[string]string{
		"STEP_OUTPUT_DIR":         stepOutputAbsPath,
		"STEP_EXECUTION_DIR":     stepExecutionAbsPath,
		"PYTHONDONTWRITEBYTECODE": "1",
		"SCRIPT_VERBOSE":          "1", // Enable verbose logging in scripts — stdout is only read on failure
	}
	if envRef := hcpo.GetWorkspaceEnvRef(); envRef != nil {
		hcpo.LockWorkspaceEnv()
		for k, v := range envRef {
			if k == "STEP_OUTPUT_DIR" || k == "STEP_EXECUTION_DIR" {
				// Never let the shared workspace env override the per-step folders.
				// These are execution-specific and can leak across concurrently
				// running steps if read from the shared env map.
				continue
			}
			extraEnv[k] = v
		}
		hcpo.UnlockWorkspaceEnv()
	}

	envKeys := make([]string, 0, len(extraEnv))
	for k := range extraEnv {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	hcpo.GetLogger().Info(fmt.Sprintf(
		"🐍 [scripted_code] Direct script MCP preflight | script=%s | workdir=%s | step_output=%s | read_paths=%v | write_paths=%v | MCP_SESSION_ID=%s | MCP_API_URL=%s | env_keys=%v",
		mainPyAbsPath,
		workDirRel,
		stepOutputAbsPath,
		guard.ReadPaths,
		guard.WritePaths,
		extraEnv["MCP_SESSION_ID"],
		extraEnv["MCP_API_URL"],
		envKeys,
	))

	// Build request using the same ExecuteShellCommandParams struct the LLM agent uses,
	// so the workspace API receives the same JSON shape and applies the same logic.
	// Use timeout=0 so the parent workflow/sub-agent context controls cancellation.
	// Long-running scripted steps can legitimately exceed a fixed shell timeout.
	timeout := 0
	useShell := true
	reqParams := workspace.ExecuteShellCommandParams{
		Command:          sb.String(),
		WorkingDirectory: workDirRel,
		Timeout:          &timeout,
		UseShell:         &useShell,
		FolderGuard:      guard,
		ExtraEnv:         extraEnv,
	}

	jsonBody, err := json.Marshal(reqParams)
	if err != nil {
		return "", -1, fmt.Errorf("marshal shell request: %w", err)
	}

	apiURL := getWorkspaceAPIURL() + "/api/execute"
	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", -1, fmt.Errorf("create shell request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", -1, fmt.Errorf("workspace shell execute: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", -1, fmt.Errorf("read shell response: %w", err)
	}

	// Parse raw API response: {"success":..., "data":{"stdout":...,"stderr":...,"exit_code":...}, "error":...}
	var apiResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			ExitCode int    `json:"exit_code"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", -1, fmt.Errorf("parse shell response: %w (body: %s)", err, string(body))
	}

	combined := apiResp.Data.Stdout
	if apiResp.Data.Stderr != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += apiResp.Data.Stderr
	}
	exitCode = apiResp.Data.ExitCode
	hcpo.GetLogger().Info(fmt.Sprintf(
		"🐍 [scripted_code] Direct script shell result | success=%t | exit_code=%d | api_error=%q | script=%s",
		apiResp.Success,
		exitCode,
		apiResp.Error,
		mainPyAbsPath,
	))
	if !apiResp.Success && exitCode == 0 {
		exitCode = -1
		execErr = fmt.Errorf("workspace API error: %s", apiResp.Error)
	}
	return combined, exitCode, execErr
}

// tryRunSavedLearnCodeScript checks for a saved main.py and runs it:
//   - No saved script               → RanScript=false (fall through to LLM)
//   - Script ran + validation passed → RanScript=true, Success=true
//   - Script failed                  → RanScript=true, Success=false (fall through to LLM for relearn)
func (hcpo *StepBasedWorkflowOrchestrator) tryRunSavedLearnCodeScript(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	allSteps []PlanStepInterface,
	stepExecutionRelPath string, // workspace-relative, e.g. "Workflow/X/runs/iter-1/execution/step-2"
	executionWorkspacePath string, // workspace-relative execution root
) *LearnCodeFastPathResult {
	docsRoot := GetPromptDocsRoot()
	stepID := step.GetID()

	if !hcpo.hasValidLearnedScriptAPI(ctx, stepID) {
		scriptRelPath := getLearnCodeDirRelPath(stepID, hcpo.isEvaluationMode) + "/main.py"
		existingScript, _ := hcpo.ReadWorkspaceFile(ctx, scriptRelPath)
		hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] No saved script for step %d (%s) — LLM will generate from scratch", stepIndex+1, stepID))
		return &LearnCodeFastPathResult{RanScript: false, ExistingScript: existingScript}
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] Executing saved script for step %d (%s) — 0 LLM tokens", stepIndex+1, stepID))

	// Read existing script content for relearn context (if script fails).
	learnDirRelPath := getLearnCodeDirRelPath(stepID, hcpo.isEvaluationMode)
	scriptRelPath := learnDirRelPath + "/main.py"
	existingScript, _ := hcpo.ReadWorkspaceFile(ctx, scriptRelPath)

	stepExecutionAbsPath := filepath.Join(docsRoot, stepExecutionRelPath)
	codeDirRelPath := stepExecutionRelPath + "/code"
	codeDirAbsPath := filepath.Join(docsRoot, codeDirRelPath)
	mainPyAbsPath := filepath.Join(codeDirAbsPath, "main.py")

	inputArgs := hcpo.buildLearnCodeInputArgs(ctx, step, stepIndex, stepPath, allSteps, executionWorkspacePath, docsRoot, hcpo.variableValues)

	// Saved-script fast path bypasses execution-agent startup, so prime browser-capable
	// MCP server configs here to preserve the same Playwright/Camofox session + override
	// behavior as a normal workflow run.
	// Without this, the first browser tool call can fall through to default mcpcache
	// creation and lose run-specific settings like Downloads/output-dir.
	hcpo.primeBrowserServerConfigsForSavedScript(ctx, getAgentConfigs(step))

	// Clean previous output files before running saved script — fresh slate for this execution.
	// Preserve code/ — it may contain an LLM-fixed main.py from a previous attempt.
	hcpo.cleanStepOutputDir(ctx, stepExecutionRelPath, "code")

	// Copy saved script from learnings/ to execution/code/ — but only if execution/code/main.py
	// doesn't already exist. A previous LLM fix attempt may have written a newer version there,
	// and blindly overwriting it with the old saved script would discard the fix.
	execMainPyRelPath := codeDirRelPath + "/main.py"
	if _, existsErr := hcpo.ReadWorkspaceFile(ctx, execMainPyRelPath); existsErr != nil {
		// No main.py in execution/code/ — copy from learnings/
		if mkErr := createFolderViaAPI(ctx, codeDirRelPath); mkErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to create code/ dir for saved script copy: %v", mkErr))
		}
		learnFiles, listErr := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, learnDirRelPath)
		if listErr != nil {
			// Fallback: copy just main.py
			if writeErr := hcpo.WriteWorkspaceFile(ctx, codeDirRelPath+"/main.py", existingScript); writeErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to copy main.py to code/: %v", writeErr))
			}
		} else {
			for _, f := range learnFiles {
				fileName := filepath.Base(f)
				if fileName == "" || fileName == "." || fileName == "script_metadata.json" || fileName == "SKILL.md" || fileName == ".learning_metadata.json" {
					continue // skip metadata files, only copy script files
				}
				content, readErr := hcpo.ReadWorkspaceFile(ctx, learnDirRelPath+"/"+fileName)
				if readErr != nil {
					continue
				}
				if writeErr := hcpo.WriteWorkspaceFile(ctx, codeDirRelPath+"/"+fileName, content); writeErr != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to copy %s to code/: %v", fileName, writeErr))
				}
			}
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] Copied saved script from learnings/%s/ to %s/", stepID, codeDirRelPath))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] main.py already exists in %s/ — using existing version (may be LLM-fixed)", codeDirRelPath))
		// Update existingScript to the actual content in execution/code/ for relearn context
		if execScript, readErr := hcpo.ReadWorkspaceFile(ctx, execMainPyRelPath); readErr == nil {
			existingScript = execScript
		}
	}

	// Static code review before execution — catch anti-patterns that would fail on reuse
	codeIssues := reviewMainPyScript(existingScript)
	if len(codeIssues) > 0 {
		hcpo.GetLogger().Warn(fmt.Sprintf("🔍 [review-code] Saved script for step %d (%s) has %d issue(s) — skipping fast path, falling back to LLM for fix", stepIndex+1, stepID, len(codeIssues)))
		var issueReport strings.Builder
		issueReport.WriteString("Static code review found issues that must be fixed:\n")
		for idx, issue := range codeIssues {
			issueReport.WriteString(fmt.Sprintf("%d. %s\n", idx+1, issue))
		}
		return &LearnCodeFastPathResult{
			RanScript:      true,
			Success:        false,
			ExitCode:       -1,
			Error:          issueReport.String(),
			ExistingScript: existingScript,
			FailureReason:  "execution_error",
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ [review-code] Saved script for step %d (%s) passed static analysis", stepIndex+1, stepID))
	}

	// Run from execution/code/ — same location the LLM writes to
	output, exitCode, execErr := hcpo.execLearnCodeScript(ctx, step, stepIndex, stepPath, mainPyAbsPath, inputArgs, stepExecutionAbsPath, codeDirAbsPath, stepExecutionRelPath)
	if execErr != nil || exitCode != 0 {
		var execErrMsg string
		if execErr != nil {
			execErrMsg = fmt.Sprintf("execution error: %v\n%s", execErr, output)
		} else {
			execErrMsg = fmt.Sprintf("exit code %d:\n%s", exitCode, output)
		}
		// Even with non-zero exit, check if the script produced valid output.
		preValResults, _ := RunPreValidation(ctx, getValidationSchema(step), stepExecutionRelPath, hcpo.BaseOrchestrator)
		if preValResults != nil {
			hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, stepPath, false, preValResults)
			if hcpo.selectedRunFolder != "" {
				preValLogPath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
				SavePreValidationLog(ctx, hcpo.BaseOrchestrator, preValLogPath, step.GetID(), stepPath, preValResults, getValidationSchema(step))
			}
		}
		if preValResults != nil && preValResults.OverallPass {
			hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] Saved script exited %d but pre-validation passed — treating as success for step %d", exitCode, stepIndex+1))
			hcpo.updateLearnCodeRunStats(ctx, stepID, true)
			return &LearnCodeFastPathResult{RanScript: true, Success: true, ExitCode: exitCode, Output: output, ExistingScript: existingScript}
		}
		validationErrMsg := ""
		failureReason := "execution_error"
		if preValResults != nil {
			validationErrMsg = formatWorkspaceResults(preValResults)
			failureReason = "execution_and_validation_error"
		}
		errMsg := execErrMsg
		if validationErrMsg != "" {
			errMsg = fmt.Sprintf("%s\n\n%s", execErrMsg, validationErrMsg)
		}
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Script failed for step %d: %s", stepIndex+1, errMsg))
		hcpo.updateLearnCodeRunStats(ctx, stepID, false)
		return &LearnCodeFastPathResult{
			RanScript:       true,
			Success:         false,
			ExitCode:        exitCode,
			Output:          output,
			Error:           errMsg,
			ExecutionError:  execErrMsg,
			ValidationError: validationErrMsg,
			FailureReason:   failureReason,
			ExistingScript:  existingScript,
		}
	}

	// Script exited 0 — run pre-validation to confirm output structure
	preValResults, _ := RunPreValidation(ctx, getValidationSchema(step), stepExecutionRelPath, hcpo.BaseOrchestrator)
	if preValResults != nil {
		hcpo.emitPreValidationCompletedEvent(ctx, step, stepIndex, stepPath, false, preValResults)
		if hcpo.selectedRunFolder != "" {
			preValLogPath := fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
			SavePreValidationLog(ctx, hcpo.BaseOrchestrator, preValLogPath, step.GetID(), stepPath, preValResults, getValidationSchema(step))
		}
	}
	if preValResults != nil && !preValResults.OverallPass {
		errMsg := formatWorkspaceResults(preValResults)
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Script ran but output validation failed for step %d", stepIndex+1))
		hcpo.updateLearnCodeRunStats(ctx, stepID, false)
		return &LearnCodeFastPathResult{
			RanScript:       true,
			Success:         false,
			ExitCode:        0,
			Output:          output,
			Error:           errMsg,
			ValidationError: errMsg,
			FailureReason:   "validation_error",
			ExistingScript:  existingScript,
		}
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ [learn_code] Script executed and validated for step %d (%s) — 0 LLM tokens used", stepIndex+1, stepID))
	hcpo.updateLearnCodeRunStats(ctx, stepID, true)
	return &LearnCodeFastPathResult{RanScript: true, Success: true, Output: output}
}

// runLearnCodeMainPyFromExecution runs main.py written by the LLM in the step execution code folder.
// Returns nil if main.py has not been written yet (LLM hasn't finished writing).
// The LLM writes to execution/step-x/code/; STEP_OUTPUT_DIR is execution/step-x/.
func (hcpo *StepBasedWorkflowOrchestrator) runLearnCodeMainPyFromExecution(
	ctx context.Context,
	step PlanStepInterface,
	stepIndex int,
	stepPath string,
	allSteps []PlanStepInterface,
	stepExecutionRelPath string,
	executionWorkspacePath string,
) *LearnCodeFastPathResult {
	docsRoot := GetPromptDocsRoot()
	stepExecutionAbsPath := filepath.Join(docsRoot, stepExecutionRelPath)
	codeDirAbsPath := filepath.Join(stepExecutionAbsPath, "code")
	mainPyPath := filepath.Join(codeDirAbsPath, "main.py")

	// Check if main.py exists via workspace API (not os.Stat — Go server runs on Mac, files are in Docker).
	mainPyRelPath := stepExecutionRelPath + "/code/main.py"
	if _, err := hcpo.ReadWorkspaceFile(ctx, mainPyRelPath); err != nil {
		return nil // main.py not yet written
	}

	inputArgs := hcpo.buildLearnCodeInputArgs(ctx, step, stepIndex, stepPath, allSteps, executionWorkspacePath, docsRoot, hcpo.variableValues)

	// Clean previous output files before running so pre-validation tests fresh output only.
	// Preserve code/ — main.py lives there and we're about to run it.
	hcpo.cleanStepOutputDir(ctx, stepExecutionRelPath, "code")

	hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] Running main.py from code/ folder for step %d (LLM phase)", stepIndex+1))
	output, exitCode, execErr := hcpo.execLearnCodeScript(ctx, step, stepIndex, stepPath, mainPyPath, inputArgs, stepExecutionAbsPath, codeDirAbsPath, stepExecutionRelPath)
	if execErr != nil || exitCode != 0 {
		var execErrMsg string
		if execErr != nil {
			execErrMsg = fmt.Sprintf("execution error: %v\n%s", execErr, output)
		} else {
			execErrMsg = fmt.Sprintf("exit code %d:\n%s", exitCode, output)
		}
		// Run pre-validation even on failure — the LLM needs feedback about both
		// runtime errors AND missing/malformed output files to fix the script properly.
		validationErrMsg := ""
		preValResults, _ := RunPreValidation(ctx, getValidationSchema(step), stepExecutionRelPath, hcpo.BaseOrchestrator)
		if preValResults != nil && !preValResults.OverallPass {
			validationErrMsg = formatWorkspaceResults(preValResults)
		}
		errMsg := execErrMsg
		if validationErrMsg != "" {
			errMsg = fmt.Sprintf("%s\n\n%s", execErrMsg, validationErrMsg)
		}
		return &LearnCodeFastPathResult{RanScript: true, Success: false, ExitCode: exitCode, Error: errMsg}
	}
	return &LearnCodeFastPathResult{RanScript: true, Success: true, ExitCode: 0, Output: output}
}

// saveLearnCodeScriptToLearnings copies all files from the step's code/ subfolder to learnings/{step-id}/.
// This preserves main.py plus any helper modules the LLM wrote alongside it.
// Uses workspace API (not os.*) — Go server may not have direct filesystem access to Docker workspace.
func (hcpo *StepBasedWorkflowOrchestrator) saveLearnCodeScriptToLearnings(
	step PlanStepInterface,
	stepExecutionAbsPath string,
) {
	stepID := step.GetID()
	ctx := context.Background()

	// Check if main.py exists in the code/ subfolder via workspace API.
	// stepExecutionAbsPath = /app/workspace-docs/{workspace}/{runs}/execution/step-N
	// Convert to relative path for workspace API: strip docsRoot + workspacePath prefix.
	docsRoot := GetPromptDocsRoot()
	workspacePath := hcpo.GetWorkspacePath()
	wsPrefix := filepath.Join(docsRoot, workspacePath) + "/"
	stepExecRelPath := strings.TrimPrefix(stepExecutionAbsPath, wsPrefix)
	if stepExecRelPath == stepExecutionAbsPath {
		// Couldn't strip — fall back to relative path extraction from absolute
		stepExecRelPath = strings.TrimPrefix(stepExecutionAbsPath, docsRoot+"/")
		stepExecRelPath = strings.TrimPrefix(stepExecRelPath, workspacePath+"/")
	}
	codeRelPath := stepExecRelPath + "/code"
	mainPyRelPath := codeRelPath + "/main.py"

	if _, err := hcpo.ReadWorkspaceFile(ctx, mainPyRelPath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] main.py not found in code/ dir (%s) — skipping save", mainPyRelPath))
		return
	}

	learnDirRelPath := getLearnCodeDirRelPath(stepID, hcpo.isEvaluationMode)

	// Delete all existing files AND folders in learnings/{step-id}/ before copying new ones.
	// Files use DELETE /api/documents/; subdirectories use DELETE /api/folders/.
	// We try folder delete first (handles "code/" subdir), then fall back to file delete.
	if existingFiles, listErr := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, learnDirRelPath); listErr == nil {
		for _, f := range existingFiles {
			fileName := filepath.Base(f)
			if fileName == "" || fileName == "." || fileName == "script_metadata.json" || fileName == "SKILL.md" {
				continue // keep metadata and supplemental learning notes; refresh script files only
			}
			entryRelPath := learnDirRelPath + "/" + fileName
			absPath := filepath.Join(GetPromptDocsRoot(), workspacePath, entryRelPath)
			// File delete first; fall back to folder delete for subdirectories (e.g. code/).
			if fileDelErr := hcpo.DeleteWorkspaceFile(ctx, entryRelPath); fileDelErr != nil {
				if folderErr := deleteFolderViaAPI(ctx, absPath); folderErr != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to delete %s from learnings: file=%v folder=%v", fileName, fileDelErr, folderErr))
				}
			}
		}
	}

	// List files in code/ folder and copy each to learnings/{step-id}/
	files, listErr := hcpo.BaseOrchestrator.ListWorkspaceFiles(ctx, codeRelPath)
	if listErr != nil {
		// ListWorkspaceFiles failed — try to copy just main.py
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to list code/ dir (%s): %v — copying main.py only", codeRelPath, listErr))
		content, readErr := hcpo.ReadWorkspaceFile(ctx, mainPyRelPath)
		if readErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to read main.py: %v", readErr))
			return
		}
		if writeErr := hcpo.WriteWorkspaceFile(ctx, learnDirRelPath+"/main.py", content); writeErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to write main.py to learnings: %v", writeErr))
			return
		}
	} else {
		for _, f := range files {
			if f == "" {
				continue
			}
			// f may be a relative path within codeRelPath or the full path — normalize
			fileName := filepath.Base(f)
			if fileName == "" || fileName == "." {
				continue
			}
			srcRelPath := codeRelPath + "/" + fileName
			content, readErr := hcpo.ReadWorkspaceFile(ctx, srcRelPath)
			if readErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to read %s: %v", fileName, readErr))
				continue
			}
			if writeErr := hcpo.WriteWorkspaceFile(ctx, learnDirRelPath+"/"+fileName, content); writeErr != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to write %s to learnings: %v", fileName, writeErr))
			}
		}
	}

	oldMeta := hcpo.readLearnCodeMetadataAPI(ctx, stepID)
	version := 1
	createdAt := time.Now().UTC().Format(time.RFC3339)
	relearnCount := 0
	if oldMeta != nil {
		version = oldMeta.ScriptVersion + 1
		if oldMeta.CreatedAt != "" {
			createdAt = oldMeta.CreatedAt
		}
		relearnCount = oldMeta.RelearnCount + 1
	}

	newMeta := LearnCodeMetadata{
		StepID:        stepID,
		ScriptVersion: version,
		CreatedAt:     createdAt,
		LastRunAt:     time.Now().UTC().Format(time.RFC3339),
		RelearnCount:  relearnCount,
	}
	if err := hcpo.writeLearnCodeMetadataAPI(ctx, stepID, newMeta); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to write script_metadata.json: %v", err))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("✅ [learn_code] Saved main.py to learnings for step (%s) — version %d", stepID, version))
	}
}

// updateLearnCodeRunStats increments run counters in script_metadata.json via workspace API.
func (hcpo *StepBasedWorkflowOrchestrator) updateLearnCodeRunStats(ctx context.Context, stepID string, success bool) {
	meta := hcpo.readLearnCodeMetadataAPI(ctx, stepID)
	if meta == nil {
		return
	}
	meta.TotalRuns++
	meta.LastRunAt = time.Now().UTC().Format(time.RFC3339)
	if success {
		if meta.SuccessfulRuns == nil {
			meta.SuccessfulRuns = map[string]int{}
		}
		meta.SuccessfulRuns["code_exec"]++
	} else {
		meta.FailedRuns++
	}
	_ = hcpo.writeLearnCodeMetadataAPI(ctx, stepID, *meta)
}

// saveLearnCodeFastPathLog writes a JSON execution log to the standard logs directory
// so that debug_step and direct file inspection can see the fast-path script run.
func (hcpo *StepBasedWorkflowOrchestrator) saveLearnCodeFastPathLog(
	ctx context.Context,
	stepIndex int,
	stepID string,
	stepPath string,
	scriptPath string,
	result *LearnCodeFastPathResult,
) {
	if result == nil {
		return
	}
	logDir := getExecutionFolderPathForLogs(func() string {
		if hcpo.selectedRunFolder != "" {
			return fmt.Sprintf("%s/runs/%s", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		}
		return hcpo.GetWorkspacePath()
	}(), stepID, stepPath)

	logData := map[string]interface{}{
		"step_index":       stepIndex + 1,
		"step_path":        stepPath,
		"mode":             "learn_code_fast_path",
		"script_path":      scriptPath,
		"exit_code":        result.ExitCode,
		"success":          result.Success,
		"output":           result.Output,
		"error":            result.Error,
		"execution_error":  result.ExecutionError,
		"validation_error": result.ValidationError,
		"failure_reason":   result.FailureReason,
		"timestamp":        time.Now().Format(time.RFC3339),
	}
	if logJSON, err := json.MarshalIndent(logData, "", "  "); err == nil {
		logPath := logDir + "/learn_code_fast_path.json"
		if err := hcpo.WriteWorkspaceFile(context.Background(), logPath, string(logJSON)); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [learn_code] Failed to save fast path log: %v", err))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🐍 [learn_code] Saved fast path execution log: %s", logPath))
		}
	}
}

// GetLearnCodeModeInstructions returns system prompt additions for the persistent
// scripted-code path used by both code_exec and legacy learn_code steps.
// codeDirAbsPath is the LLM's working directory (execution/step-x/code/).
// stepOutputAbsPath is the step output folder (execution/step-x/) — STEP_OUTPUT_DIR env var points here.
// inputArgPaths are the absolute paths passed as sys.argv[1], sys.argv[2], ...
// envVarNames are the env var names available to the script (SECRET_*, MCP_API_URL, STEP_OUTPUT_DIR).
func GetLearnCodeModeInstructions(codeDirAbsPath, stepOutputAbsPath string, isRelearn bool, priorScript, priorError string, inputArgPaths []string, envVarNames []string, varMappingLines []string, validationSchemaJSON string) string {
	var sb strings.Builder
	sb.WriteString("\n\n## Code Execution Mode\n\n")
	sb.WriteString("You are in **code execution mode**. Your primary goal is to write a reusable Python solution (`main.py`). If you are unable to write a working main.py, you may fall back to calling MCP tools directly via the API to complete the task — but always prefer writing main.py since it becomes a saved script for future runs.\n\n")
	sb.WriteString("**Your working directory (write all code files here):**\n")
	sb.WriteString(fmt.Sprintf("```\n%s\n```\n\n", codeDirAbsPath))
	sb.WriteString("**Rules:**\n")
	sb.WriteString(fmt.Sprintf("- Write `main.py` (the entry point) to `%s/main.py`\n", codeDirAbsPath))
	sb.WriteString(fmt.Sprintf("- You may also write helper modules (e.g. `utils.py`) to `%s/` — they will be available since the script runs with that as cwd\n", codeDirAbsPath))
	sb.WriteString(fmt.Sprintf("- Write all **output files** to `%s` (available as `os.environ['STEP_OUTPUT_DIR']`)\n", stepOutputAbsPath))
	sb.WriteString("- Before writing main.py, you may call tools via the API to inspect the current state (e.g. take a browser snapshot, check page content, read files) to understand the system before coding your solution\n")
	sb.WriteString(fmt.Sprintf("- To test, just run: `python3 '%s/main.py'` — **all env vars listed below are pre-injected; do NOT manually `export` any values**\n", codeDirAbsPath))
	sb.WriteString("- After your turn, the system will run main.py and give you the error output if it fails — you'll get multiple fix attempts\n")
	sb.WriteString("- A passing `main.py` becomes the saved script for this step and will be tried first on future runs before calling the LLM again\n")
	sb.WriteString("- **LOGGING**: Use `VERBOSE = os.environ.get('SCRIPT_VERBOSE', '') == '1'` and guard debug prints with `if VERBOSE:`. Log state before/after each major action. For browser automation: print the snapshot/page state after each navigation and interaction so failures show exactly what the page looked like. The only way to debug a failed script is through its stdout.\n")
	sb.WriteString("- **IMPORTANT**: Do NOT hardcode any user/account/credential values — read ALL dynamic values from the environment variables listed below\n")
	sb.WriteString("- **CRITICAL**: Always use `os.environ['KEY']` (NO default). NEVER `os.environ.get('KEY', 'hardcoded')` — missing var must raise KeyError, not silently use a hardcoded value.\n")
	sb.WriteString("- **WARNING**: The step description shows the *current run's* values. This script is **reused for every group/user** — NEVER copy any name, ID, or value from the description into the script or into any `export` commands.\n")
	sb.WriteString("- **ROBUSTNESS**: This script runs across different groups/users with different data. Handle edge cases: missing fields (use `.get()` with safe defaults for data fields), empty lists, None values, different data formats (e.g. dates as string vs number), missing files (check existence before reading optional files). Always print diagnostic context before failing so the error output explains what went wrong. Do not assume the shape of external data — validate and handle gracefully.\n\n")

	// Show workflow variable → env var mapping so LLM knows exactly how to access each one
	if len(varMappingLines) > 0 {
		sb.WriteString("**Workflow variable → env var** (`VAR_*` = config values, `SECRET_*` = real credentials):\n")
		for _, line := range varMappingLines {
			sb.WriteString(fmt.Sprintf("- `%s`\n", line))
		}
		sb.WriteString("\n")
	}

	// Show exact positional arguments
	if len(inputArgPaths) > 0 {
		sb.WriteString("**Script invocation** (exact call the controller will make):\n```\n")
		sb.WriteString(fmt.Sprintf("python3 %s/main.py", codeDirAbsPath))
		for _, arg := range inputArgPaths {
			sb.WriteString(fmt.Sprintf(" \\\n    '%s'", arg))
		}
		sb.WriteString("\n```\n\n")
		sb.WriteString("**Positional arguments** (sys.argv):\n")
		for i, arg := range inputArgPaths {
			sb.WriteString(fmt.Sprintf("- `sys.argv[%d]` = `%s`\n", i+1, arg))
		}
		sb.WriteString("\n")
		sb.WriteString("**CRITICAL**: Read ALL input data from `sys.argv` — these are the declared dependencies from upstream steps. Do NOT construct paths to sibling step folders manually (e.g. do NOT build paths like `execution/login-step/output.json`). The controller resolves the correct paths per group/iteration and passes them as positional arguments. If you need data that isn't in sys.argv, add it as a context_dependency in the plan — don't hardcode paths.\n\n")
	} else {
		sb.WriteString("- No positional arguments — script takes no inputs\n\n")
	}

	// Show available env vars
	if len(envVarNames) > 0 {
		sb.WriteString("**Available environment variables**:\n")
		// Always show STEP_OUTPUT_DIR first, then sort the rest
		shown := map[string]bool{}
		sb.WriteString(fmt.Sprintf("- `STEP_OUTPUT_DIR` = `%s`  (write output files here)\n", stepOutputAbsPath))
		shown["STEP_OUTPUT_DIR"] = true
		stepExecutionDir := filepath.Dir(stepOutputAbsPath)
		sb.WriteString(fmt.Sprintf("- `STEP_EXECUTION_DIR` = `%s`  (parent execution folder — only use as fallback when data is not available via sys.argv)\n", stepExecutionDir))
		shown["STEP_EXECUTION_DIR"] = true
		sorted := make([]string, 0, len(envVarNames))
		for _, name := range envVarNames {
			if !shown[name] {
				sorted = append(sorted, name)
			}
		}
		sort.Strings(sorted)
		for _, name := range sorted {
			sb.WriteString(fmt.Sprintf("- `%s`\n", name))
		}
		sb.WriteString("\n")
	}

	// Python best practices section
	sb.WriteString(BuildPythonBestPractices(varMappingLines, len(inputArgPaths) > 0))

	// Show validation schema — tells the LLM exactly what output files to produce
	if validationSchemaJSON != "" {
		sb.WriteString("**Output requirements (validation schema)** — your script MUST produce these files:\n")
		sb.WriteString("```json\n")
		sb.WriteString(validationSchemaJSON)
		sb.WriteString("\n```\n")
		sb.WriteString("Write each required file to `STEP_OUTPUT_DIR` with the exact filename and structure shown.\n\n")
	}

	// NOTE: Prior script context (failed script + error, or existing script for update)
	// is now included in the USER MESSAGE via BuildLearnCodePriorContext(), not in the
	// system prompt. This ensures the LLM sees it with higher salience and it survives
	// repair agent transitions where the system prompt is regenerated.
	return sb.String()
}

// BuildLearnCodePriorContext generates the prior script context section for inclusion
// in the user message. Returns empty string if no prior context is needed.
func BuildLearnCodePriorContext(priorScript, priorError string) string {
	if priorScript == "" {
		return ""
	}
	var sb strings.Builder
	if priorError != "" {
		sb.WriteString("\n### Previous Script (Failed)\n\n")
		sb.WriteString("The previously saved script failed. Fix the bug and rewrite main.py:\n\n")
		sb.WriteString("```python\n")
		sb.WriteString(priorScript)
		sb.WriteString("\n```\n")
		sb.WriteString("\n**Error:**\n```\n")
		sb.WriteString(priorError)
		sb.WriteString("\n```\n")
	} else {
		sb.WriteString("\n### Existing Script (Update Required)\n\n")
		sb.WriteString("A saved script already exists for this step. Adapt and improve this working script to match the current step description above. ")
		sb.WriteString("Keep all working logic intact unless the current task explicitly requires a change:\n\n")
		sb.WriteString("```python\n")
		sb.WriteString(priorScript)
		sb.WriteString("\n```\n")
	}
	return sb.String()
}
