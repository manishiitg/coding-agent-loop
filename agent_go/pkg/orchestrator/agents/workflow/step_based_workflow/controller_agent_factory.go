package step_based_workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mcpagent "github.com/manishiitg/mcpagent/agent"
	"github.com/manishiitg/mcpagent/agent/codeexec"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"
	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/browser"
	"mcp-agent-builder-go/agent_go/pkg/common"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// ============================================================================
// Phase 1: Helper Methods (Extracted for Reusability)
// ============================================================================

// normalizeServerNames ensures an empty server list is replaced with the NoServers
// sentinel so the connection layer doesn't interpret [] as "connect to all servers".
func normalizeServerNames(servers []string) []string {
	if len(servers) == 0 {
		return []string{mcpclient.NoServers}
	}
	return servers
}

// filterServersByWorkflow intersects stepServers with workflowServers so that the
// workflow-level server list acts as a hard cap. If the workflow has no servers
// (user removed all MCPs), no step can bypass that restriction.
// Returns mcpclient.NoServers sentinel when the result is empty, because an empty
// []string is treated as "all servers" by the connection layer.
func filterServersByWorkflow(stepServers, workflowServers []string) []string {
	if len(workflowServers) == 0 {
		return []string{mcpclient.NoServers}
	}
	workflowSet := make(map[string]bool, len(workflowServers))
	for _, s := range workflowServers {
		workflowSet[s] = true
	}
	result := make([]string, 0, len(stepServers))
	for _, s := range stepServers {
		if workflowSet[s] {
			result = append(result, s)
		}
	}
	if len(result) == 0 {
		return []string{mcpclient.NoServers}
	}
	return result
}

// filterToolsByWorkflow filters stepTools keeping only those whose server prefix
// is allowed by workflowServers. Workflow is the hard cap — if a server was
// removed from the workflow no step can re-enable it via its own tool list.
func filterToolsByWorkflow(stepTools, workflowServers []string) []string {
	if len(workflowServers) == 0 {
		return []string{}
	}
	workflowSet := make(map[string]bool, len(workflowServers))
	for _, s := range workflowServers {
		workflowSet[s] = true
	}
	result := make([]string, 0, len(stepTools))
	for _, t := range stepTools {
		serverName := t
		if idx := strings.Index(t, ":"); idx >= 0 {
			serverName = t[:idx]
		}
		if workflowSet[serverName] {
			result = append(result, t)
		}
	}
	return result
}

func buildSessionScopedMCPAPIURL(sessionID string) string {
	baseURL := strings.TrimSpace(os.Getenv("MCP_API_URL"))
	sessionID = strings.TrimSpace(sessionID)
	if baseURL == "" || sessionID == "" {
		return ""
	}
	if idx := strings.Index(baseURL, "/s/"); idx >= 0 {
		baseURL = baseURL[:idx]
	}
	return strings.TrimRight(baseURL, "/") + "/s/" + sessionID
}

func injectStepEnvIntoShellExecutor(executors map[string]interface{}, stepOutputAbsPath, stepExecutionAbsPath string, mcpSessionID string) {
	if len(executors) == 0 || strings.TrimSpace(stepOutputAbsPath) == "" {
		return
	}
	original, ok := executors["execute_shell_command"].(func(ctx context.Context, args map[string]interface{}) (string, error))
	if !ok || original == nil {
		return
	}
	executors["execute_shell_command"] = func(ctx context.Context, args map[string]interface{}) (string, error) {
		if args == nil {
			args = make(map[string]interface{})
		}
		mergedEnv := map[string]interface{}{
			"STEP_OUTPUT_DIR":    stepOutputAbsPath,
			"STEP_EXECUTION_DIR": stepExecutionAbsPath,
		}
		if rawExtraEnv, exists := args["extra_env"]; exists {
			switch typed := rawExtraEnv.(type) {
			case map[string]interface{}:
				for k, v := range typed {
					mergedEnv[k] = v
				}
			case map[string]string:
				for k, v := range typed {
					mergedEnv[k] = v
				}
			}
		}
		// Per-step values must always win over any stale caller-provided value.
		mergedEnv["STEP_OUTPUT_DIR"] = stepOutputAbsPath
		mergedEnv["STEP_EXECUTION_DIR"] = stepExecutionAbsPath
		if strings.TrimSpace(mcpSessionID) != "" {
			// Shell/file tools must resolve against the step-local MCP session so the
			// session-level folder guard matches the prompt's narrow read/write scope.
			mergedEnv["MCP_SESSION_ID"] = mcpSessionID
			if scopedURL := buildSessionScopedMCPAPIURL(mcpSessionID); scopedURL != "" {
				mergedEnv["MCP_API_URL"] = scopedURL
			}
		}
		args["extra_env"] = mergedEnv
		return original(ctx, args)
	}
}

// getWorkspaceDocsRoot returns the absolute path to the workspace-docs root directory.
// This is used specifically for resolving absolute paths for Playwright Downloads.
//
// CRITICAL FIX (Jan 2026): This function was added to fix a long-standing bug where
// Playwright downloads were being saved to the wrong location (agent_go/Downloads instead
// of workspace-docs/Workflow/.../execution/Downloads).
//
// Root Cause:
//   - workspacePath is relative to workspace-docs root (e.g., "Workflow/ICICI...")
//   - When using filepath.Abs() on a relative path, it resolves relative to the current
//     working directory (which is agent_go/), not relative to workspace-docs root
//   - This caused paths like "Workflow/..." to resolve to "agent_go/Workflow/..." instead
//     of "workspace-docs/Workflow/..."
//
// Solution:
// - Explicitly find the workspace-docs root directory
// - Resolve all Downloads paths relative to workspace-docs root, not CWD
// - This ensures paths are always correct regardless of where the process runs from
//
// It checks DOCS_DIR environment variable first, then falls back to calculating
// relative to the current working directory (../workspace-docs from agent_go).
func getWorkspaceDocsRoot() string {
	// Check environment variable first
	if docsDir := os.Getenv("DOCS_DIR"); docsDir != "" {
		if absPath, err := filepath.Abs(docsDir); err == nil {
			return absPath
		}
	}

	// Fallback: calculate relative to current working directory
	// Assuming agent_go is at mcp-agent-builder-go/agent_go, workspace-docs is at ../workspace-docs
	cwd, err := os.Getwd()
	if err != nil {
		// If we can't get CWD, return empty and let caller handle it
		return ""
	}

	// Try to find workspace-docs relative to current directory
	// Common locations: ../workspace-docs (from agent_go) or ./workspace-docs (from root)
	possiblePaths := []string{
		filepath.Join(cwd, "..", "workspace-docs"),
		filepath.Join(cwd, "workspace-docs"),
	}

	for _, path := range possiblePaths {
		if absPath, err := filepath.Abs(path); err == nil {
			// Check if directory exists
			if info, err := os.Stat(absPath); err == nil && info.IsDir() {
				return absPath
			}
		}
	}

	// Last resort: return calculated path even if it doesn't exist yet
	if absPath, err := filepath.Abs(filepath.Join(cwd, "..", "workspace-docs")); err == nil {
		return absPath
	}

	return ""
}

// setupBrowserDownloadsPathOverride configures the Downloads path for browser automation tools.
// This shared function is used by both execution agents and orchestrator agents to ensure
// browser downloads go to the correct group-specific folder.
//
// Supports both:
// - Playwright MCP server (checked via selectedServers)
// - agent-browser skill/virtual tool (checked via selectedSkills)
//
// The function:
// 1. Checks if any browser tool is available (Playwright server OR agent-browser skill)
// 2. Validates that selectedRunFolder is set (logs error if not)
// 3. Creates the Downloads folder via API
// 4. Resolves the absolute path relative to workspace-docs root (not CWD)
// 5. Sets the runtime override on the config before agent creation (for Playwright)
//
// This ensures the first agent that creates a browser connection will use the correct
// Downloads path, and all subsequent agents will reuse it.
func (hcpo *StepBasedWorkflowOrchestrator) setupBrowserDownloadsPathOverride(ctx context.Context, config *agents.OrchestratorAgentConfig, stepConfig *AgentConfigs) {
	// Check if any browser tool is available (Playwright server OR agent-browser skill)
	// Downloads folder is needed for any browser automation tool

	// Check effective servers (step config intersected with workflow — workflow is the hard cap)
	var effectiveServers []string
	if stepConfig != nil && stepConfig.SelectedServers != nil && len(stepConfig.SelectedServers) > 0 {
		effectiveServers = filterServersByWorkflow(stepConfig.SelectedServers, hcpo.GetSelectedServers())
	} else {
		effectiveServers = hcpo.GetSelectedServers()
	}

	// Check for Playwright MCP server
	hasPlaywright := false
	for _, server := range effectiveServers {
		if server == "playwright" {
			hasPlaywright = true
		}
	}

	// Check for agent-browser skill
	hasAgentBrowser := false
	for _, skill := range hcpo.GetSelectedSkills() {
		if skill == "agent-browser" {
			hasAgentBrowser = true
			break
		}
	}

	if !hasPlaywright && !hasAgentBrowser {
		return // No browser tool, nothing to configure
	}

	// Track which browser tool triggered this for later use
	browserToolType := "agent-browser"
	if hasPlaywright {
		browserToolType = "playwright"
	}

	// CRITICAL: Ensure selectedRunFolder is set before configuring Downloads path
	// If it's empty, all agents in this group will share a connection with wrong Downloads path
	if hcpo.selectedRunFolder == "" {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ [CRITICAL] selectedRunFolder is EMPTY when configuring %s Downloads path! This will cause all downloads to go to the wrong location. Ensure ApplyExecutionContext is called before creating agents.", browserToolType), nil)
		// Don't return error - continue with default path but log the issue
	}

	// Route browser downloads to execution/Downloads folder in the run directory
	workspacePath := hcpo.GetWorkspacePath()

	// Build the relative path to Downloads folder
	// If run folder is selected: "runs/{runFolder}/execution/Downloads"
	// Otherwise: "execution/Downloads"
	var downloadsRelativePath string
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Setting Downloads path - selectedRunFolder: '%s', workspacePath: '%s', sessionID: '%s'", hcpo.selectedRunFolder, workspacePath, hcpo.getSessionID()))
	if hcpo.selectedRunFolder != "" {
		downloadsRelativePath = filepath.Join("runs", hcpo.selectedRunFolder, "execution", "Downloads")
	} else {
		// WARNING: selectedRunFolder is empty - downloads will go to execution/Downloads instead of group-specific folder
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [CRITICAL] selectedRunFolder is EMPTY when setting Downloads path! Downloads will go to execution/Downloads instead of group-specific folder. This may indicate ApplyExecutionContext was not called or selectedRunFolder was not set correctly."))
		downloadsRelativePath = filepath.Join("execution", "Downloads")
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Downloads relative path: '%s'", downloadsRelativePath))

	// Create folder via Workspace API with workspacePath for normalization
	if err := createFolderViaAPI(ctx, downloadsRelativePath, workspacePath); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to create downloads directory via API %s: %v", downloadsRelativePath, err))
	}

	// Resolve Downloads path to host absolute path for Playwright MCP server configuration.
	// Playwright runs on the host (not in Docker), so it needs the real host filesystem path.
	// All other prompt-facing paths use GetPromptDocsRoot() (/app/workspace-docs).
	absDownloadsPath := filepath.Clean(filepath.Join(getWorkspaceDocsRoot(), workspacePath, downloadsRelativePath))
	hcpo.GetLogger().Info(fmt.Sprintf("✅ Resolved Downloads path: %s", absDownloadsPath))

	// Configure Playwright runtime override (only needed for Playwright MCP server, not agent-browser)
	// agent-browser uses its own download handling via workspace-api
	if hasPlaywright {
		// IMPORTANT: This override is set on the config BEFORE agent creation, so it will be
		// applied when the MCP connection is created.
		//
		// CRITICAL: If a Playwright connection already exists for this session, it will be REUSED
		// without applying the new override. This can happen if:
		// 1. Another agent in the same group already created the connection (this is OK - they should share)
		// 2. A connection exists from a previous run that wasn't cleaned up (this is BAD)
		//
		// To ensure the first agent in a group creates the connection with the correct override,
		// we rely on:
		// - Closing the previous session before starting a new group (in batch execution)
		// - Setting selectedRunFolder before setting session ID
		// - Setting the override before agent creation
		if config.RuntimeOverrides == nil {
			config.RuntimeOverrides = make(mcpclient.RuntimeOverrides)
		}
		playwrightOverride := config.RuntimeOverrides["playwright"]
		if playwrightOverride.ArgsReplace == nil {
			playwrightOverride.ArgsReplace = make(map[string]string)
		}
		playwrightOverride.ArgsReplace["--output-dir"] = absDownloadsPath
		playwrightOverride.WorkingDir = absDownloadsPath
		config.RuntimeOverrides["playwright"] = playwrightOverride

		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Configured Playwright downloads path to: %s (override will be applied when connection is created)", absDownloadsPath))
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Runtime override for playwright: ArgsReplace=%+v, WorkingDir=%s", playwrightOverride.ArgsReplace, playwrightOverride.WorkingDir))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Created Downloads folder for agent-browser: %s", absDownloadsPath))
	}

	// Store the workspace-relative downloads path for agent-browser.
	// The browser executor reads this from context and passes it as WorkingDirectory
	// to the workspace API, so it needs to be relative to workspace root (workspacePath + downloadsRelativePath).
	browserRelPath := filepath.Join(workspacePath, downloadsRelativePath)
	hcpo.SetBrowserDownloadsPath(browserRelPath)
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Set browser downloads path on orchestrator: %s", browserRelPath))

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Browser tool: %s, Session ID: %s, selectedRunFolder: '%s', absDownloadsPath: '%s'", browserToolType, hcpo.getSessionID(), hcpo.selectedRunFolder, absDownloadsPath))
}

// primeBrowserServerConfigsForSavedScript registers browser-capable MCP server configs
// for the current tool session before running a saved main.py fast path.
//
// Why this is needed:
//   - Normal execution agents preload lazy-connect server configs (with runtime overrides)
//     during agent startup, so the first Playwright tool call uses the workflow's
//     run-specific browser settings.
//   - Saved-script fast path skips execution-agent startup and goes straight to main.py.
//   - Without this priming, the first browser call falls through to mcpcache and creates
//     a connection from the default MCP config (e.g. global Downloads/), bypassing the
//     workflow's run-specific override.
func (hcpo *StepBasedWorkflowOrchestrator) primeBrowserServerConfigsForSavedScript(ctx context.Context, stepConfig *AgentConfigs) {
	sessionID := strings.TrimSpace(hcpo.GetMCPSessionID())
	if sessionID == "" {
		return
	}

	// Reuse the same browser/download setup logic as normal execution-agent creation.
	config := agents.NewOrchestratorAgentConfig("saved-script-browser-prime")
	config.MCPConfigPath = hcpo.GetMCPConfigPath()
	config.MCPSessionID = sessionID
	if stepConfig != nil && len(stepConfig.SelectedServers) > 0 {
		config.ServerNames = filterServersByWorkflow(stepConfig.SelectedServers, hcpo.GetSelectedServers())
	} else {
		config.ServerNames = hcpo.GetSelectedServers()
	}
	hcpo.setupBrowserDownloadsPathOverride(ctx, config, stepConfig)

	registry := mcpclient.GetSessionRegistry()
	mergedConfig, err := mcpclient.LoadMergedConfig(config.MCPConfigPath, hcpo.GetLogger())
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to load MCP config for saved-script browser priming: %v", err))
		return
	}

	var primed []string
	for _, serverName := range config.ServerNames {
		if serverName != "playwright" {
			continue
		}
		serverConfig, err := mergedConfig.GetServer(serverName)
		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to get %s config for saved-script browser priming: %v", serverName, err))
			continue
		}
		if override, ok := config.RuntimeOverrides[serverName]; ok {
			serverConfig = serverConfig.ApplyOverride(override)
		}
		// Store the config on the logical tool session. The executor will later resolve
		// browser-capable servers onto the shared browser session before connecting.
		registry.StoreServerConfig(sessionID, serverName, serverConfig)
		// Also store under the shared browser session ID so that ANY session sharing
		// this browser (e.g. the chat session) can lazy-reconnect to playwright.
		connSessionID := registry.ResolveConnectionSessionID(sessionID, serverName)
		if connSessionID != sessionID {
			registry.StoreServerConfig(connSessionID, serverName, serverConfig)
		}
		primed = append(primed, fmt.Sprintf("%s->%s", serverName, connSessionID))
	}

	if len(primed) > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf(
			"🌐 Primed saved-script browser server configs for session %s: %s",
			sessionID,
			strings.Join(primed, ", "),
		))
	}
}

// isGenericAgentStep reports whether a step was spawned via call_generic_agent.
// executeGenericAgent sets stepID to "generic-{parentPath}-{todoID}" and stepPath to
// "{parentPath}-generic-{todoID}", so either form is enough to identify these ad-hoc
// steps and widen their folder guard.
func isGenericAgentStep(stepID, stepPath string) bool {
	return strings.HasPrefix(stepID, "generic-") || strings.Contains(stepPath, "-generic-")
}

// setupExecutionFolderGuard sets up folder guard paths for execution agents.
// kbAccess must be one of KBAccessRead / Write / ReadWrite / None — callers resolve it
// via resolveKnowledgebaseAccess before invoking. learningsAccess must be resolved via
// resolveLearningsAccess. kbWriteMethod must be one of KBWriteMethodAgent / Direct and
// is only consulted when kbAccess permits writes; callers resolve it via
// resolveKnowledgebaseWriteMethod. Returns readPaths and writePaths.
func (hcpo *StepBasedWorkflowOrchestrator) setupExecutionFolderGuard(stepPath string, stepID string, kbAccess string, learningsAccess string, kbWriteMethod string) (readPaths, writePaths []string) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	// Set folder guard paths:
	// READ: execution folder (to read previous step results) + global learnings (if mode grants read) + knowledgebase folder (if mode grants read)
	// WRITE: only the specific step folder (execution/step-{X}/ or execution/step-{X}-{branch}/) + execution/Downloads folder to prevent writing to other steps
	// NOTE: under kbWriteMethod=direct we add knowledgebase/notes/ to writePaths so the
	// step can write per-topic markdown via shell + diff_patch_workspace_file. Under
	// kbWriteMethod=agent we add nothing — notes/ is only writable by the post-step KB
	// update agent (setupKBUpdateFolderGuard, triggered by a non-empty knowledgebase_contribution).
	// Use getExecutionFolderPath to support both regular and branch steps
	stepFolderPath := getExecutionFolderPath(executionWorkspacePath, stepID, stepPath)
	downloadsPath := fmt.Sprintf("%s/Downloads", executionWorkspacePath)
	readPaths = []string{executionWorkspacePath}
	if learningsAccess != LearningsAccessNone {
		globalLearningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, GlobalLearningID)
		readPaths = append(readPaths, globalLearningsPath)
	}

	// Generic agents (spawned via call_generic_agent) get write access to the entire
	// execution/ folder. They run ad-hoc tasks that may span multiple step folders
	// (e.g. patching sibling outputs, staging downloads under a step-owned path),
	// and locking them to a single folder causes spurious sandbox denials.
	if isGenericAgentStep(stepID, stepPath) {
		writePaths = []string{executionWorkspacePath}
	} else {
		writePaths = []string{stepFolderPath, downloadsPath}
	}

	// Always add db/ folder to read+write paths (no preset toggle). Evaluation DBWrite enforcement
	// is applied by callers that know the step is an eval step (they post-process writePaths).
	dbPath := getDBPath(baseWorkspacePath)
	readPaths = append(readPaths, dbPath)
	writePaths = append(writePaths, dbPath)

	// Add knowledgebase folder to READ paths when the mode grants read. Under
	// kbWriteMethod=direct, also add knowledgebase/notes/ to WRITE paths so the step
	// can author per-topic markdown via shell + diff_patch_workspace_file.
	if kbAccess != KBAccessNone && kbAccessAllowsRead(kbAccess) {
		knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)
		readPaths = append(readPaths, knowledgebasePath)
	}
	if kbAccessAllowsWrite(kbAccess) && kbWriteMethod == KBWriteMethodDirect {
		notesPath := fmt.Sprintf("%s/notes", getKnowledgebasePath(baseWorkspacePath))
		writePaths = append(writePaths, notesPath)
	}

	// Auto-improvement framework: business rules live under knowledgebase/rules/
	// — read access is granted by the same kbAccess check above (recursive
	// subtree). No separate flag. The optimizer's reorganize/consolidate passes
	// are responsible for skipping knowledgebase/rules/ so user-supplied content
	// is never silently rewritten.

	// Check if TARGET_RUN_PATH variable is set (used for evaluation) and add to read paths
	// This allows evaluation agents to read the artifacts of the run they are evaluating.
	// Also grant the parent run folder so evals can reach sibling logs (e.g. logs/<step>/execution/
	// learn_code_fast_path.json) — under sandbox-exec, stat() on a denied path raises EPERM, which
	// Python surfaces as PermissionError and breaks callers that only guard against FileNotFoundError.
	if targetRunPath, ok := hcpo.variableValues["TARGET_RUN_PATH"]; ok && targetRunPath != "" {
		readPaths = append(readPaths, targetRunPath)
		targetRunParent := filepath.Dir(targetRunPath)
		if targetRunParent != "" && targetRunParent != "." && targetRunParent != "/" {
			readPaths = append(readPaths, targetRunParent)
			hcpo.GetLogger().Info(fmt.Sprintf("🔓 Added TARGET_RUN_PATH (+parent for sibling logs) to read paths for evaluation: %s, %s", targetRunPath, targetRunParent))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔓 Added TARGET_RUN_PATH to read paths for evaluation: %s", targetRunPath))
		}
	}

	return readPaths, writePaths
}

// getCodeExecutionMode determines code execution mode with priority: step config > workflow/preset default
// Note: The workflow/preset default reflects what the user explicitly set. Server.go no longer
// auto-enables code execution mode for the entire workflow. Provider-based auto-enable
// (claude-code/gemini-cli) is handled per-agent in applyStepConfigToAgentConfig and createConditionalAgent.
func (hcpo *StepBasedWorkflowOrchestrator) getCodeExecutionMode(stepConfig *AgentConfigs) bool {
	if stepConfig != nil && stepConfig.UseCodeExecutionMode != nil {
		isCodeExecutionMode := *stepConfig.UseCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode: %v", isCodeExecutionMode))
		return isCodeExecutionMode
	}
	isCodeExecutionMode := hcpo.GetUseCodeExecutionMode()
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using workflow/preset code execution mode: %v", isCodeExecutionMode))
	return isCodeExecutionMode
}

// getExecutionMaxTurns determines max turns with priority: step config > orchestrator default
func (hcpo *StepBasedWorkflowOrchestrator) getExecutionMaxTurns(stepConfig *AgentConfigs) int {
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.ExecutionMaxTurns != nil {
		maxTurns = *stepConfig.ExecutionMaxTurns
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only max turns: %d (orchestrator default was: %d)", maxTurns, hcpo.GetMaxTurns()))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default execution-only max turns: %d (no step-specific config)", maxTurns))
	}
	return maxTurns
}

// resolveStepID resolves the step ID from stepIDOverride or falls back to stepPath
// Priority: stepIDOverride > stepPath fallback
func (hcpo *StepBasedWorkflowOrchestrator) resolveStepID(stepPath, stepIDOverride string) string {
	if stepIDOverride != "" {
		return stepIDOverride
	}

	hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Could not determine step ID for %s, using stepPath as fallback", stepPath))
	return stepPath
}

// selectExecutionLLM selects the LLM config with cascading fallback logic
//
// Priority for main step execution:
//  1. step config ExecutionLLM   — explicit per-step override; always wins when set
//  2. parent ExecutionLLM via context — propagated when the parent todo-task step
//     has an ExecutionLLM set; when present, tier selection is skipped entirely
//  3. tiered mode                — workshop override, persistent step execution_tier,
//     preferred_tier from context, or the default tier
//  4. orchestrator main LLM      — final fallback
func (hcpo *StepBasedWorkflowOrchestrator) selectExecutionLLM(
	ctx context.Context,
	stepConfig *AgentConfigs,
	stepPath string,
) *orchestrator.LLMConfig {
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	// Guard against nil — scheduler-triggered sessions may not have an orchestrator LLM set.
	if orchestratorLLMConfig == nil {
		orchestratorLLMConfig = &orchestrator.LLMConfig{}
	}

	// ── 1. STEP CONFIG ExecutionLLM ──────────────────────────────────────────
	// Explicit per-step execution model always wins, including in tiered mode.
	if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 [STEP OVERRIDE] Using step ExecutionLLM for step %s: %s/%s",
			stepPath, stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: stepConfig.ExecutionLLM.Provider,
				ModelID:  stepConfig.ExecutionLLM.ModelID,
			},
			Fallbacks: convertAgentFallbacks(stepConfig.ExecutionLLM.Fallbacks),
			APIKeys:   orchestratorLLMConfig.APIKeys,
		}
	}

	// ── 2. SUB-AGENT OVERRIDE ────────────────────────────────────────────────
	// When the parent todo-task step pins an ExecutionLLM, the wrapper injects it
	// into the sub-agent's context. Its presence is itself the signal that we're
	// in "propagate parent's LLM" mode — tier selection is bypassed.
	if subAgentLLM, ok := ctx.Value(virtualtools.SubAgentLLMContextKey).(*AgentLLMConfig); ok &&
		subAgentLLM != nil && subAgentLLM.Provider != "" && subAgentLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🎯 [SUB-AGENT] Using parent ExecutionLLM for step %s: %s/%s",
			stepPath, subAgentLLM.Provider, subAgentLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: subAgentLLM.Provider,
				ModelID:  subAgentLLM.ModelID,
			},
			Fallbacks: convertAgentFallbacks(subAgentLLM.Fallbacks),
			APIKeys:   hcpo.GetAPIKeys(),
		}
	}

	// ── 3. TIERED MODE ───────────────────────────────────────────────────────
	// Default-tier resolution when no explicit step override is set.
	if hcpo.tierResolver != nil {
		// Workshop execute_step tier override (e.g., execute_step(step_id, tier="medium"))
		if workshopTier, ok := ctx.Value(WorkshopTierOverrideKey).(int); ok && workshopTier >= 1 && workshopTier <= 3 {
			tier := TierLevel(workshopTier)
			llmConfig := hcpo.tierResolver.ResolveTier(tier)
			if llmConfig != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Workshop tier override: Tier %d (%s) for step %s: %s/%s",
					workshopTier, TierLevelLabel(tier), stepPath, llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
			}
			return llmConfig
		}
		if stepConfig != nil {
			if fixedTier, ok := ParseTierOverride(stepConfig.ExecutionTier); ok {
				llmConfig := hcpo.tierResolver.ResolveTier(fixedTier)
				if llmConfig != nil {
					hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Execution agent for step %s using fixed execution_tier=%s: %s/%s",
						stepPath, NormalizeTierOverride(stepConfig.ExecutionTier), llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
				}
				return llmConfig
			}
			if strings.TrimSpace(stepConfig.ExecutionTier) != "" {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Invalid execution_tier=%q for step %s — ignoring and continuing with normal tier selection", stepConfig.ExecutionTier, stepPath))
			}
		}
		if preferredTier, ok := ctx.Value(virtualtools.PreferredTierContextKey).(int); ok && preferredTier >= 1 && preferredTier <= 3 {
			tier := TierLevel(preferredTier)
			llmConfig := hcpo.tierResolver.ResolveTier(tier)
			if llmConfig != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Execution agent using PREFERRED Tier %d (%s) for step %s: %s/%s",
					preferredTier, TierLevelLabel(tier), stepPath, llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
			}
			return llmConfig
		}
		// If disable_tier_optimization is set, always use Tier 1 regardless of learning maturity
		if stepConfig != nil && stepConfig.DisableTierOptimization != nil && *stepConfig.DisableTierOptimization {
			llmConfig := hcpo.tierResolver.ResolveTier(TierHigh)
			if llmConfig != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Execution agent for step %s using Tier 1 (High) — tier optimization disabled: %s/%s",
					stepPath, llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
			}
			return llmConfig
		}
		// Evaluation mode defaults to medium tier — eval steps are verification checks
		// that don't need the most powerful model. Step config can still override via ExecutionLLM (step 3).
		if hcpo.isEvaluationMode {
			llmConfig := hcpo.tierResolver.ResolveTier(TierMedium)
			if llmConfig != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Evaluation step %s defaulting to Tier 2 (Medium): %s/%s",
					stepPath, llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
			}
			return llmConfig
		}
		llmConfig, tier := hcpo.tierResolver.ResolveForExecution()
		if llmConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Execution agent for step %s using Tier %d (%s): %s/%s",
				stepPath, int(tier), TierLevelLabel(tier), llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
		}
		return llmConfig
	}

	// ── 4. NO VALID CONFIG ──────────────────────────────────────────────────
	// Return nil so the caller can surface a user-visible error instead of crashing.
	hcpo.GetLogger().Warn(fmt.Sprintf("❌ selectExecutionLLM: no valid LLM configuration found for step %s — tier resolver is required", stepPath))
	return nil
}

// applyStepConfigToAgentConfig applies step-specific configuration overrides to agent config
func (hcpo *StepBasedWorkflowOrchestrator) applyStepConfigToAgentConfig(config *agents.OrchestratorAgentConfig, stepConfig *AgentConfigs, isCodeExecutionMode bool) {
	workflowServers := hcpo.GetSelectedServers()
	// Use step-specific servers if provided, filtered against workflow-level servers.
	// Workflow is the hard cap: if a server was removed from the workflow no step can use it.
	if stepConfig != nil && stepConfig.SelectedServers != nil && len(stepConfig.SelectedServers) > 0 {
		filtered := filterServersByWorkflow(stepConfig.SelectedServers, workflowServers)
		config.ServerNames = filtered
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only servers (workflow-filtered): %v → %v", stepConfig.SelectedServers, filtered))
	} else {
		// Use orchestrator defaults when stepConfig is nil or SelectedServers is empty
		config.ServerNames = normalizeServerNames(workflowServers)
		if stepConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but SelectedServers is empty - using orchestrator defaults: %v", config.ServerNames))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config not found - using orchestrator defaults: %v", config.ServerNames))
		}
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		filtered := filterToolsByWorkflow(stepConfig.SelectedTools, workflowServers)
		config.SelectedTools = filtered
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only tools (workflow-filtered): %v → %v", stepConfig.SelectedTools, filtered))
	} else {
		// Explicitly set orchestrator defaults when stepConfig is nil or SelectedTools is empty
		config.SelectedTools = hcpo.GetSelectedTools()
		if stepConfig != nil {
			// Log when stepConfig exists but SelectedTools is empty (will use orchestrator defaults)
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but no SelectedTools specified - using orchestrator defaults: %v", config.SelectedTools))
		}
	}

	// Determine execution mode: CLI providers and learn_code steps always use code execution mode.
	// learn_code steps need code execution mode so the agent gets the tool index and get_api_spec
	// virtual tool — without these, the LLM has to guess MCP server/tool names when writing main.py.
	actualProvider := config.LLMConfig.Primary.Provider
	isLearnCode := isScriptedExecutionModeConfig(stepConfig)
	if actualProvider == "claude-code" || actualProvider == "kimi" || actualProvider == "gemini-cli" || actualProvider == "codex-cli" {
		config.UseCodeExecutionMode = true
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode forced for CLI provider '%s' - MCP tools accessed via HTTP bridge", actualProvider))
	} else if isLearnCode {
		config.UseCodeExecutionMode = true
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode forced for learn_code step — LLM needs tool index and get_api_spec to write main.py"))
	} else if stepConfig != nil && stepConfig.UseCodeExecutionMode != nil {
		config.UseCodeExecutionMode = *stepConfig.UseCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode: %v", config.UseCodeExecutionMode))
	} else {
		config.UseCodeExecutionMode = false
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Provider '%s': code execution mode disabled (not CLI provider)", actualProvider))
	}

	// Set EnableContextOffloading if specified
	if stepConfig != nil && stepConfig.EnableContextOffloading != nil {
		config.EnableContextOffloading = stepConfig.EnableContextOffloading
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific context offloading setting: %v", *stepConfig.EnableContextOffloading))
	}
}

// Long-running workflow execution agents should inherit cancellation from the
// outer workflow/tool context instead of the generic 5-minute agent timeout.
// Otherwise, saved-script execution and sub-agent orchestration get canceled
// even when their tool-specific timeouts are configured correctly.
func (hcpo *StepBasedWorkflowOrchestrator) disableParentAgentTimeout(config *agents.OrchestratorAgentConfig, agentKind string) {
	if config == nil {
		return
	}
	config.Timeout = 0
	hcpo.GetLogger().Info(fmt.Sprintf("⏱️ Disabled parent agent timeout for %s; using outer workflow/tool cancellation instead", agentKind))
}

// prepareCustomTools filters and prepares custom tools based on step config
func (hcpo *StepBasedWorkflowOrchestrator) prepareCustomTools(stepConfig *AgentConfigs) ([]llmtypes.Tool, map[string]interface{}) {
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && len(stepConfig.EnabledCustomTools) > 0 {
		// Migrate old tool configs: strip deprecated categories and ensure workspace_advanced is present.
		// workspace_basic and workspace_git are deprecated — shell_command handles all file/git ops.
		var enabledTools []string
		hasAdvanced := false
		for _, entry := range stepConfig.EnabledCustomTools {
			if strings.HasPrefix(entry, "workspace_basic") {
				continue // Drop deprecated workspace_basic entries
			}
			if strings.HasPrefix(entry, "workspace_git") {
				continue // Drop deprecated workspace_git entries
			}
			// workspace_image* entries are legacy — image tools now live inside workspace_advanced,
			// which is auto-added below, so these entries become no-ops and are dropped.
			if entry == "workspace_image:*" || strings.HasPrefix(entry, "workspace_image:") ||
				entry == "workspace_image_gen:*" || strings.HasPrefix(entry, "workspace_image_gen:") ||
				entry == "workspace_image_edit:*" || strings.HasPrefix(entry, "workspace_image_edit:") {
				continue
			}
			if strings.HasPrefix(entry, "workspace_advanced") {
				hasAdvanced = true
			}
			enabledTools = append(enabledTools, entry)
		}
		if !hasAdvanced {
			enabledTools = append(enabledTools, "workspace_advanced:*")
			hcpo.GetLogger().Info("🔧 Auto-including workspace_advanced:* (migrated from old workspace_basic config)")
		}

		// Auto-include workspace_browser:* if agent_browser exists in the workspace tools pool
		// (present when preset has enable_browser_access: true) and not already listed.
		hasBrowserCategory := false
		for _, entry := range enabledTools {
			if strings.HasPrefix(entry, "workspace_browser") {
				hasBrowserCategory = true
				break
			}
		}
		if !hasBrowserCategory {
			for _, tool := range hcpo.WorkspaceTools {
				if tool.Function != nil && tool.Function.Name == "agent_browser" {
					enabledTools = append(enabledTools, "workspace_browser:*")
					hcpo.GetLogger().Info("🔧 Auto-including workspace_browser:* (headless browser enabled at workflow level)")
					break
				}
			}
		}

		// Filter tools based on unified format (category:tool or category:*)
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			enabledTools,
		)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(enabledTools), enabledTools))
	} else {
		// Default: enable only advanced + human tools (not all tools)
		// This avoids exposing basic file tools that may not be needed
		defaultEnabledTools := []string{
			"workspace_advanced:*",
			"human_tools:*",
		}
		// Auto-include browser tools if agent_browser exists in the workspace tools pool
		// (present when preset has enable_browser_access: true)
		for _, tool := range hcpo.WorkspaceTools {
			if tool.Function != nil && tool.Function.Name == "agent_browser" {
				defaultEnabledTools = append(defaultEnabledTools, "workspace_browser:*")
				break
			}
		}
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			defaultEnabledTools,
		)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using default tool set (advanced + human): %d tools enabled", len(toolsToRegister)))
	}

	return toolsToRegister, executorsToUse
}

// prepareWorkspaceToolsOnly prepares minimal workspace tools for learning agents.
// Learning agents only need shell_command (for reading files) and diff_patch_workspace_file
// (for writing learnings). They should NOT have human tools (like human_feedback) since
// they are pure LLM analysis agents that should not block on human input.
func (hcpo *StepBasedWorkflowOrchestrator) prepareWorkspaceToolsOnly() ([]llmtypes.Tool, map[string]interface{}) {
	tools, executors := orchestrator.FilterCustomToolsByCategory(
		hcpo.WorkspaceTools,
		hcpo.WorkspaceToolExecutors,
		[]string{
			"workspace_advanced:execute_shell_command",
			"workspace_advanced:diff_patch_workspace_file",
		},
	)
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Prepared %d learning agent tools (execute_shell_command + diff_patch, no human tools)", len(tools)))
	return tools, executors
}

// setupConditionalFolderGuard sets up folder guard paths for conditional agents
// Returns readPaths and writePaths for folder guard configuration
func (hcpo *StepBasedWorkflowOrchestrator) setupConditionalFolderGuard(stepPath string, stepID string) (readPaths, writePaths []string) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	// Step-specific execution folder: execution/step-{X}/ or execution/step-{X}-{branch}/ (for writing evaluation results)
	stepFolderPath := getExecutionFolderPath(executionWorkspacePath, stepID, stepPath)

	// Set folder guard paths:
	// READ: execution folder (to read all previous step results and verify conditions) + global learnings + knowledgebase folder (if enabled)
	// WRITE: step-specific execution folder (to write evaluation results and intermediate files) + knowledgebase folder (if enabled)
	readPaths = []string{executionWorkspacePath}
	// Always add global learnings folder to read paths
	globalLearningsPath2 := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, GlobalLearningID)
	readPaths = append(readPaths, globalLearningsPath2)
	writePaths = []string{stepFolderPath}

	// Always add db/ folder to read+write paths
	dbPath := getDBPath(baseWorkspacePath)
	readPaths = append(readPaths, dbPath)
	writePaths = append(writePaths, dbPath)

	// Add knowledgebase folder paths only if enabled
	if hcpo.UseKnowledgebase() {
		knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)
		readPaths = append(readPaths, knowledgebasePath)
		writePaths = append(writePaths, knowledgebasePath)
	}
	return readPaths, writePaths
}

// setupKBUpdateFolderGuard grants the KB update agent read on the step's execution
// folder (+ siblings, so relative-path references to other step outputs resolve) and
// knowledgebase/, and write on knowledgebase/ only.
func (hcpo *StepBasedWorkflowOrchestrator) setupKBUpdateFolderGuard(stepID string, stepPath string) (readPaths, writePaths []string) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	stepFolderPath := getExecutionFolderPath(executionWorkspacePath, stepID, stepPath)
	knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)

	readPaths = []string{executionWorkspacePath, stepFolderPath, knowledgebasePath}
	writePaths = []string{knowledgebasePath}
	return readPaths, writePaths
}

// setupSubAgentSessionGuard creates a dedicated MCP session ID for a sub-agent
// that should NOT share the workflow's group MCP session, and registers its
// folder guard at that session's level so sandbox-exec enforces the correct
// writes when the sub-agent issues shell commands.
//
// Why this exists: sub-agents with `ServerNames = [NoServers]` (learning agent,
// KB update/consolidate/reorganize, eval scoring when workspace-only) don't
// need to share the group's MCP session for browser/gmail connection reuse.
// But if they DO share it, their folder guard is set at orchestrator level
// (SetWorkspacePathForFolderGuard) while the group session's guard is set at
// session level (SetSessionFolderGuard) — and session-level wins in
// pkg/workspace/execute_shell_command.go's priority order. Result: the sub-
// agent's writes get denied by the parent step's guard, which excludes paths
// like learnings/_global/ and knowledgebase/.
//
// Returns the dedicated session ID — assign to `config.MCPSessionID` BEFORE
// calling CreateAndSetupStandardAgentWithConfig. The caller should also call
// `common.ClearSessionShellConfig(sessionID)` after the agent finishes
// (typically deferred in the runXxxPhase function).
func (hcpo *StepBasedWorkflowOrchestrator) setupSubAgentSessionGuard(agentKind string, stepID string, readPaths []string, writePaths []string) string {
	sessionID := fmt.Sprintf("sub-%s-%s-%d", agentKind, stepID, time.Now().UnixNano())
	common.SetSessionFolderGuard(sessionID, readPaths, writePaths)

	// Carry the parent group session's working directory onto the dedicated
	// session so `ls learnings/_global/` style relative commands still resolve
	// against the workspace. Without this, the shell falls back to the Go
	// process's cwd (agent_go/), which would break every relative path the
	// learning/KB agents use.
	if parentSessionID := strings.TrimSpace(hcpo.GetMCPSessionID()); parentSessionID != "" {
		if parentCfg := common.GetSessionShellConfig(parentSessionID); parentCfg != nil && parentCfg.WorkingDir != "" {
			common.SetSessionWorkingDir(sessionID, parentCfg.WorkingDir)
		}
	}

	hcpo.GetLogger().Info(fmt.Sprintf(
		"🔒 Sub-agent session %q (%s/%s) — folder guard set at session level Read=%v Write=%v",
		sessionID, agentKind, stepID, readPaths, writePaths,
	))
	return sessionID
}

// createKBUpdateAgent builds the post-step KB update agent. Folder guard reads the
// step's execution output + knowledgebase/, writes knowledgebase/ only. Uses the
// learning LLM config (same cheap-post-step-analysis profile).
func (hcpo *StepBasedWorkflowOrchestrator) createKBUpdateAgent(ctx context.Context, phase string, agentName string, stepConfig *AgentConfigs, stepID string, stepPath string, stepIndex int) (agents.OrchestratorAgent, error) {
	readPaths, writePaths := hcpo.setupKBUpdateFolderGuard(stepID, stepPath)
	// Dedicated session so the session-level folder guard wins over the parent
	// step's guard when sandbox-exec enforces writes. See setupSubAgentSessionGuard.
	subAgentSessionID := hcpo.setupSubAgentSessionGuard("kb-update", stepID, readPaths, writePaths)
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for KB update agent - Read: %v, Write: %v", readPaths, writePaths))

	llmConfig := hcpo.selectLearningLLM(ctx, stepConfig, stepID, stepPath)
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for KB update agent")
	}

	// Cap below learning's 50 — KB merges should converge quickly.
	maxTurns := 40
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)
	config.ServerNames = []string{mcpclient.NoServers}
	config.MCPSessionID = subAgentSessionID
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(&AgentLLMConfig{
		Provider: config.LLMConfig.Primary.Provider,
		ModelID:  config.LLMConfig.Primary.ModelID,
	})
	// No context offloading — output is a short summary line, not a large artifact.
	disabled := false
	config.EnableContextOffloading = &disabled

	// 4. Workspace tools only (shell + diff_patch + read/write file). No human tools — this
	// is a background agent that must not block on prompts.
	toolsToRegister, executorsToUse := hcpo.prepareWorkspaceToolsOnly()

	// 5. Base factory.
	createAgentFunc := func(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewKBUpdateAgent(config, logger, tracer, eventBridge)
	}
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		stepIndex,
		0, // iteration (unused for post-step agents)
		stepID,
		createAgentFunc,
		toolsToRegister,
		executorsToUse,
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create KB update agent: %w", err)
	}
	if err := hcpo.applyPostSetupToAgent(agent, agentName, false); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for %s: %v", agentName, err))
	}
	return agent, nil
}

// createKBConsolidateAgent builds the one-shot KB consolidate agent. Same folder-guard
// shape as KB update/reorganize: read execution folder + KB, write KB only. The read
// path on executionWorkspacePath is what gives it access to ALL step output folders
// under the selected run, which is exactly what distinguishes consolidation from per-step
// updates and from reorganize.
func (hcpo *StepBasedWorkflowOrchestrator) createKBConsolidateAgent(ctx context.Context, phase string, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	stepID := "builder-consolidate"
	stepPath := "builder-consolidate"

	readPaths, writePaths := hcpo.setupKBUpdateFolderGuard(stepID, stepPath)
	subAgentSessionID := hcpo.setupSubAgentSessionGuard("kb-consolidate", stepID, readPaths, writePaths)
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for KB consolidate agent - Read: %v, Write: %v", readPaths, writePaths))

	llmConfig := hcpo.selectLearningLLM(ctx, stepConfig, stepID, stepPath)
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for KB consolidate agent")
	}

	// Consolidation may touch many entities and write multiple pattern notes — give it
	// the same headroom as reorganize (60 turns).
	maxTurns := 60
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)
	config.ServerNames = []string{mcpclient.NoServers}
	config.MCPSessionID = subAgentSessionID
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(&AgentLLMConfig{
		Provider: config.LLMConfig.Primary.Provider,
		ModelID:  config.LLMConfig.Primary.ModelID,
	})
	disabled := false
	config.EnableContextOffloading = &disabled

	toolsToRegister, executorsToUse := hcpo.prepareWorkspaceToolsOnly()

	createAgentFunc := func(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewKBConsolidateAgent(config, logger, tracer, eventBridge)
	}
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		0,
		0,
		stepID,
		createAgentFunc,
		toolsToRegister,
		executorsToUse,
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create KB consolidate agent: %w", err)
	}
	if err := hcpo.applyPostSetupToAgent(agent, agentName, false); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for %s: %v", agentName, err))
	}
	return agent, nil
}

// createKBReorganizeAgent builds the one-shot KB reorganize agent. Same folder-guard
// shape as KB update (read execution + KB, write KB only). stepID/stepPath are
// synthetic because reorganize runs outside step context.
func (hcpo *StepBasedWorkflowOrchestrator) createKBReorganizeAgent(ctx context.Context, phase string, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	stepID := "builder-reorganize"
	stepPath := "builder-reorganize"

	readPaths, writePaths := hcpo.setupKBUpdateFolderGuard(stepID, stepPath)
	subAgentSessionID := hcpo.setupSubAgentSessionGuard("kb-reorganize", stepID, readPaths, writePaths)
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for KB reorganize agent - Read: %v, Write: %v", readPaths, writePaths))

	llmConfig := hcpo.selectLearningLLM(ctx, stepConfig, stepID, stepPath)
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for KB reorganize agent")
	}

	// Reorganize may do larger edits than an update; give it more turns than update's 40
	// but still cap to prevent runaway agents under ambiguous instructions.
	maxTurns := 60
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)
	config.ServerNames = []string{mcpclient.NoServers}
	config.MCPSessionID = subAgentSessionID
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(&AgentLLMConfig{
		Provider: config.LLMConfig.Primary.Provider,
		ModelID:  config.LLMConfig.Primary.ModelID,
	})
	disabled := false
	config.EnableContextOffloading = &disabled

	toolsToRegister, executorsToUse := hcpo.prepareWorkspaceToolsOnly()

	createAgentFunc := func(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewKBReorganizeAgent(config, logger, tracer, eventBridge)
	}
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		0, // stepIndex (not applicable for reorganize)
		0, // iteration (not applicable)
		stepID,
		createAgentFunc,
		toolsToRegister,
		executorsToUse,
		false,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create KB reorganize agent: %w", err)
	}
	if err := hcpo.applyPostSetupToAgent(agent, agentName, false); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for %s: %v", agentName, err))
	}
	return agent, nil
}

// setupLearningFolderGuard sets up folder guard paths for learning agents
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// stepPath: Step path identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps) - used for execution logs folder
func (hcpo *StepBasedWorkflowOrchestrator) setupLearningFolderGuard(learningPathIdentifier string, stepPath string, extraWritePaths ...string) (readPaths, writePaths []string) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	var validationWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
		validationWorkspacePath = runWorkspacePath
	} else {
		runWorkspacePath = baseWorkspacePath
		validationWorkspacePath = baseWorkspacePath
	}
	executionPath := fmt.Sprintf("%s/execution", runWorkspacePath)

	// Step-specific learnings: write to learnings/{learningPathIdentifier} at workspace root (not inside runs/)
	// Supports regular, branch, sub-agent, and evaluation steps — all share the learnings/ namespace;
	// step-ID uniqueness across plan.json and evaluation_plan.json is enforced at write time.
	learningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, learningPathIdentifier)
	baseLearningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)

	// Build read paths: execution path + base learnings path + execution logs folder
	readPaths = []string{executionPath}
	// Add base learnings path for reading existing learnings (we read from base but write to step folder)
	readPaths = append(readPaths, baseLearningsPath)

	// Add execution logs folder so learning agents can read execution logs if needed
	// Execution logs contain actual tool usage, conversation history, and execution results
	executionLogsPath := getExecutionFolderPathForLogs(validationWorkspacePath, learningPathIdentifier, stepPath)
	readPaths = append(readPaths, executionLogsPath)

	// Add skills folder so learning agents can read skill-creator guide and other installed skills
	readPaths = append(readPaths, "skills")

	writePaths = []string{learningsPath}
	// Add extra write paths (e.g., per-step scripts folder for code exec + global learning)
	writePaths = append(writePaths, extraWritePaths...)
	return readPaths, writePaths
}

// getLearningMaxTurns determines max turns for learning agents.
// Fixed to 500 (not configurable)
func (hcpo *StepBasedWorkflowOrchestrator) getLearningMaxTurns(stepConfig *AgentConfigs) int {
	return 500
}

// selectLearningLLM selects the LLM config for learning agents
//
// Priority:
//  1. step config LearningLLM    — explicit per-step override; beats tiered mode
//  2. tiered mode                — maturity-based tier resolution
//  3. presetLearningLLM          — workflow-level default
func (hcpo *StepBasedWorkflowOrchestrator) selectLearningLLM(ctx context.Context, stepConfig *AgentConfigs, stepID string, stepPath string) *orchestrator.LLMConfig {
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if orchestratorLLMConfig == nil {
		orchestratorLLMConfig = &orchestrator.LLMConfig{}
	}

	// ── 1. STEP CONFIG LearningLLM ───────────────────────────────────────────
	// Skipped when tiered mode is active — tiered resolver handles model selection.
	// Only used as fallback when no tier resolver is configured (legacy/manual mode).
	if hcpo.tierResolver == nil && stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 [STEP OVERRIDE] Using step LearningLLM for step %s: %s/%s (no tier resolver)",
			stepPath, stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: stepConfig.LearningLLM.Provider,
				ModelID:  stepConfig.LearningLLM.ModelID,
			},
			Fallbacks: convertAgentFallbacks(stepConfig.LearningLLM.Fallbacks),
			APIKeys:   orchestratorLLMConfig.APIKeys,
		}
	} else if hcpo.tierResolver != nil && stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("⏭️ [SKIPPED] step LearningLLM (%s/%s) skipped for step %s — tiered mode is active",
			stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID, stepPath))
	}

	// ── 2. TIERED MODE ───────────────────────────────────────────────────────
	if hcpo.tierResolver != nil {
		llmConfig, tier := hcpo.tierResolver.ResolveForLearning()
		if llmConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Learning agent for step %s using Tier %d (%s): %s/%s",
				stepPath, int(tier), TierLevelLabel(tier), llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
		}
		return llmConfig
	}

	// Tiered mode is required. If we reach here, something is misconfigured.
	hcpo.GetLogger().Warn(fmt.Sprintf("selectLearningLLM: no valid LLM configuration found for step %s — tier resolver is required, returning nil", stepPath))
	return nil
}

// applyPostSetupToAgent applies post-setup configuration to an agent after base factory setup
// This includes setting folder guard paths and optionally updating the code execution registry
// agent: The orchestrator agent to configure
// agentName: Name of the agent (for logging)
// shouldUpdateRegistry: If true, updates the code execution registry after setting folder guard paths
func (hcpo *StepBasedWorkflowOrchestrator) applyPostSetupToAgent(agent agents.OrchestratorAgent, agentName string, shouldUpdateRegistry bool) error {
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil // No base agent, nothing to configure
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil // No MCP agent, nothing to configure
	}

	// Set folder guard paths on MCP agent (required for both code execution mode and simple mode)
	// This ensures path validation works at the tool executor level
	readPaths, writePaths := hcpo.GetFolderGuardPaths()
	mcpAgent.SetFolderGuardPaths(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Folder guard paths set for %s agent - Read: %v, Write: %v", agentName, readPaths, writePaths))

	// Update code execution registry AFTER setting folder guard paths (if requested)
	// Note: Base factory has already updated the registry (if bo.GetUseCodeExecutionMode() is true), but it didn't have folder guard paths set.
	// This update ensures the registry has the correct path validation code with folder guard enabled.
	if shouldUpdateRegistry {
		// CRITICAL: Folder guard paths must be set BEFORE registry update
		// The registry generation uses these paths to create the path validation code
		// This ensures LLM-generated code can only access paths within allowed boundaries
		if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update code execution registry for %s: %v", agentName, err))
			return err
		}
		hcpo.GetLogger().Info(fmt.Sprintf("✅ [CODE_EXECUTION] Registry updated for %s agent - folder guard enabled", agentName))
	}

	return nil
}

// ============================================================================
// Phase 2: Refactored Agent Creators (Using Base Factory)
// ============================================================================

// createExecutionOnlyAgent creates an execution-only agent that receives pre-discovered learning history.
// stepPath: Step path identifier (e.g., "step-1" for regular steps, "step-3-if-true-0" for branch steps, "step-2-sub-agent-1" for sub-agents)
// stepIDOverride: Optional explicit step ID to use for learnings / metadata selection (e.g., sub-agent step ID).
//
//	When empty, the step ID will be derived from stepPath.
func (hcpo *StepBasedWorkflowOrchestrator) createExecutionOnlyAgent(ctx context.Context, phase string, stepPath string, agentName string, stepConfig *AgentConfigs, stepIDOverride string) (agents.OrchestratorAgent, error) {
	// 1. Resolve stepID first (needed for folder guard setup)
	stepID := hcpo.resolveStepID(stepPath, stepIDOverride)

	// 2. Setup folder guard (extracted method). Empty kbAccess defaults to orchestrator-level UseKnowledgebase.
	kbAccess := resolveKnowledgebaseAccess(stepConfig, hcpo.UseKnowledgebase())
	kbWriteMethod := resolveKnowledgebaseWriteMethod(stepConfig)
	learningsAccess := resolveLearningsAccess(stepConfig)
	readPaths, writePaths := hcpo.setupExecutionFolderGuard(stepPath, stepID, kbAccess, learningsAccess, kbWriteMethod)

	// Scripted code mode: add code/ subdir to the enforced write paths so the LLM can write main.py there.
	// writePaths[0] is the step execution folder (e.g. execution/step-1); appending /code gives execution/step-1/code.
	hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted_code] stepConfig nil=%v scripted=%v", stepConfig == nil, isScriptedExecutionModeConfig(stepConfig)))
	if isScriptedExecutionModeConfig(stepConfig) {
		if len(writePaths) > 0 {
			codePath := writePaths[0] + "/code"
			writePaths = append(writePaths, codePath)
			hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scripted_code] Enforced write paths now include code/: %v", writePaths))
		} else {
			hcpo.GetLogger().Warn("🐍 [scripted_code] writePaths is empty — cannot append code/ subdir to folder guard")
		}
	}

	// Add skill folder paths to read paths (skills are read-only)
	effectiveSkills := GetEffectiveSkills(stepConfig, hcpo.BaseOrchestrator)
	if len(effectiveSkills) > 0 {
		skillReadPaths, _ := BuildSkillFolderGuardPaths(effectiveSkills)
		readPaths = append(readPaths, skillReadPaths...)
		hcpo.GetLogger().Info(fmt.Sprintf("🎯 Added skill folder paths to folder guard: %v", skillReadPaths))
	}

	// NOTE: We no longer call hcpo.SetWorkspacePathForFolderGuard here.
	// Instead, readPaths/writePaths are set on the per-agent config below (config.FolderGuardReadPaths/WritePaths)
	// to prevent race conditions when parallel sub-agents share the same orchestrator instance.
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Per-agent folder guard for execution-only agent - Read paths: %v, Write paths: %v (can write to %s and execution/Downloads/)", readPaths, writePaths, stepPath))

	// 3. Determine settings (extracted methods)
	isCodeExecutionMode := hcpo.getCodeExecutionMode(stepConfig)
	maxTurns := hcpo.getExecutionMaxTurns(stepConfig)

	// 4. Select LLM (extracted method)
	llmConfig := hcpo.selectExecutionLLM(ctx, stepConfig, stepPath)
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for execution agent: step config and tier/preset execution LLM are all empty or invalid")
	}

	// 4. Create config
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)
	hcpo.disableParentAgentTimeout(config, "execution-only agent")

	// Execution-only steps can run in parallel inside a group. If they all reuse the
	// group MCP session, the session-level folder guard becomes last-writer-wins and one
	// step can end up executing another step's commands under the wrong write scope.
	// Give each execution step its own session-level guard, just like learning/KB agents.
	// Dedicated tool session for this execution step's shell/filesystem calls. Browser
	// reuse is re-bound separately below, so shell isolation does not imply browser isolation.
	execSessionID := hcpo.setupSubAgentSessionGuard("exec", stepID, readPaths, writePaths)
	config.MCPSessionID = execSessionID
	// Keep browser-sharing behavior unchanged: bind the per-step execution session to the
	// same shared browser session the group session uses. If a caller later requests
	// share_browser=false, the isolated browser session override below still wins.
	// Re-bind the new tool session onto the group's shared browser session so shell
	// isolation does not accidentally disable browser reuse for share_browser=true.
	sharedBrowserSessionID := hcpo.resolveWorkshopBrowserSessionID(hcpo.currentGroupName)
	hcpo.bindWorkshopBrowserSession(execSessionID, sharedBrowserSessionID)

	// Set per-agent folder guard paths on config to avoid race conditions with parallel sub-agents.
	// These take precedence over the shared BaseOrchestrator.folderGuardReadPaths/WritePaths
	// in registerCustomToolsForAgent, ensuring each agent gets its own correct paths.
	config.FolderGuardReadPaths = readPaths
	config.FolderGuardWritePaths = writePaths

	// Setup Downloads folder for browser tools (Playwright or agent-browser)
	// Use shared function to ensure both execution and orchestrator agents set the override correctly
	hcpo.setupBrowserDownloadsPathOverride(ctx, config, stepConfig)

	// Apply step-specific overrides
	hcpo.applyStepConfigToAgentConfig(config, stepConfig, isCodeExecutionMode)

	// Enable parallel tool execution for execution agents
	// This allows concurrent execution of multiple independent tool calls
	config.EnableParallelToolExecution = true
	hcpo.GetLogger().Info("⚡ Parallel tool execution enabled for execution-only agent")

	// Allow step config to override parallel tool execution
	if stepConfig != nil && stepConfig.DisableParallelToolExecution != nil && *stepConfig.DisableParallelToolExecution {
		config.EnableParallelToolExecution = false
		hcpo.GetLogger().Info("🔧 Parallel tool execution DISABLED for execution-only agent via step config")
	}

	// Check for isolated browser session ID (from share_browser=false in sub-agent tools)
	if isolatedSessionID, ok := ctx.Value(virtualtools.SubAgentIsolatedSessionIDKey).(string); ok && isolatedSessionID != "" {
		config.MCPSessionID = isolatedSessionID
		hcpo.GetLogger().Info(fmt.Sprintf("Browser isolation: overriding MCPSessionID to %s", isolatedSessionID))
	}

	// 5. Prepare custom tools (filtered by step config)
	toolsToRegister, executorsToUse := hcpo.prepareCustomTools(stepConfig)
	// Inject STEP_OUTPUT_DIR and STEP_EXECUTION_DIR for all execution-only agents (both learn_code and code_exec).
	// Any script run via execute_shell_command may need STEP_OUTPUT_DIR to know where to write output
	// and STEP_EXECUTION_DIR to read sibling step outputs.
	{
		executionWorkspacePath := fmt.Sprintf("%s/runs/%s/execution", hcpo.GetWorkspacePath(), hcpo.selectedRunFolder)
		stepExecutionPath := getExecutionFolderPath(executionWorkspacePath, stepID, stepPath)
		stepOutputAbsPath := filepath.Join(GetPromptDocsRoot(), stepExecutionPath)
		stepExecutionAbsPath := filepath.Dir(stepOutputAbsPath)
		injectStepEnvIntoShellExecutor(executorsToUse, stepOutputAbsPath, stepExecutionAbsPath, config.MCPSessionID)
		hcpo.GetLogger().Info(fmt.Sprintf("📂 Injecting step shell env into execute_shell_command for %s: STEP_OUTPUT_DIR=%s MCP_SESSION_ID=%s", stepID, stepOutputAbsPath, config.MCPSessionID))
	}

	// 6. Use base factory! (This handles all setup automatically)
	pathInfo := parseStepPath(stepPath)
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		pathInfo.ParentStepNumber-1, // 0-based step number
		0,                           // iteration
		stepID,                      // Step ID (resolved from step path)
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowExecutionOnlyAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister,
		executorsToUse,
		false, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup execution-only agent: %w", err)
	}

	// 7. Post-setup: folder guard (after base factory setup)
	// Note: Base factory already updates code execution registry, but we need to set folder guard paths
	// on mcpAgent first, then update registry again with correct paths
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil after creation for %s - this should never happen", agentName)
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("mcp agent is nil after creation for %s - this should never happen", agentName)
	}

	// Inject supplementary prompts (skills, browser isolation, secrets, browser instructions)
	isolatedSessionID, _ := ctx.Value(virtualtools.SubAgentIsolatedSessionIDKey).(string)
	hcpo.appendSupplementaryPrompts(ctx, mcpAgent, config, effectiveSkills, isolatedSessionID)

	// Apply post-setup configuration (folder guard paths and optional registry update)
	if err := hcpo.applyPostSetupToAgent(agent, agentName, isCodeExecutionMode); err != nil {
		// Log warning but don't fail agent creation
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for %s: %v", agentName, err))
	}

	return agent, nil
}

// createLearningAgentInternal is the unified internal function for creating learning agents (extraction or consolidation)
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
// stepIndex: 0-based step index for token tracking (should be passed from runSuccessLearningPhase)
func (hcpo *StepBasedWorkflowOrchestrator) createLearningAgentInternal(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, isCodeExecutionMode bool, stepID string, stepPath string, stepIndex int) (agents.OrchestratorAgent, error) {
	// 1. Setup folder guard (extracted method)
	// Learning agents always write to _global skill folder (template-controlled).
	// learningPathIdentifier is the step ID (for metadata), so we pass GlobalLearningID
	// for the folder guard write path, plus per-step scripts folder for code-exec mode.
	var extraWritePaths []string
	if isCodeExecutionMode && stepID != "" {
		baseWorkspacePath := hcpo.GetWorkspacePath()
		stepScriptsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepID)
		extraWritePaths = append(extraWritePaths, stepScriptsPath)
	}
	readPaths, writePaths := hcpo.setupLearningFolderGuard(GlobalLearningID, stepPath, extraWritePaths...)
	// Dedicated MCP session so the session-level folder guard registered here
	// is the one sandbox-exec enforces for this agent's shell commands — not
	// the parent group session's guard, which would deny writes to _global/.
	subAgentSessionID := hcpo.setupSubAgentSessionGuard("learn", stepID, readPaths, writePaths)
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	agentType := "learning agent"
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for %s - Read paths: %v, Write paths: %v (includes execution logs folder)", agentType, readPaths, writePaths))

	// Ensure the learning folder exists before running the agent, as it expects to list files in it
	if len(writePaths) > 0 {
		if err := hcpo.ensureStepLearningsFolderExists(ctx, writePaths[0]); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure learning folder exists: %v", err))
		}
	}

	// Use the provided step-specific code execution mode (already computed with step-level priority) to ensure consistency
	wasCodeExecutionMode := isCodeExecutionMode
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 %s using step-specific code execution mode: %v (matches execution agent)", agentType, wasCodeExecutionMode))

	// 2. Determine settings (extracted methods)
	maxTurns := hcpo.getLearningMaxTurns(stepConfig)
	// Use learning LLM config - Priority: cost-optimization > step config > preset default
	llmConfig := hcpo.selectLearningLLM(ctx, stepConfig, stepID, stepPath)
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for learning agent: step config and preset learning LLM are both empty or invalid")
	}

	// 3. Create config
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Learning agents always use NoServers (pure LLM analysis agent)
	// Step-specific server/tool selection is only for execution agents
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM analysis agent

	// Override the inherited group MCP session with the dedicated one set up
	// above. See setupSubAgentSessionGuard for why this matters.
	config.MCPSessionID = subAgentSessionID

	// Code execution mode and tool search mode only apply to execution agents, not learning agents
	// CRITICAL: Override orchestrator-level code execution mode and tool search mode setting - learning agents are pure LLM analysis agents
	// Use the agent's ACTUAL provider (from its LLM config), not the phase LLM provider.
	// The phase LLM may be claude-code but the learning agent uses a different provider (e.g., MiniMax).
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(&AgentLLMConfig{
		Provider: config.LLMConfig.Primary.Provider,
		ModelID:  config.LLMConfig.Primary.ModelID,
	})
	if wasCodeExecutionMode {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Execution was in code execution mode - using code execution learning agent (but agent itself does NOT use code execution mode)"))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling code execution mode for %s (only execution agents use MCP tools)", agentType))
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 %s code execution mode: %v (NoServers=%v, will be auto-disabled if needed)", agentType, config.UseCodeExecutionMode, config.ServerNames[0] == mcpclient.NoServers))

	// Disable large output virtual tools (context offloading) for learning agents
	// Learning agents should not offload their outputs to prevent issues with learning content
	disabled := false
	config.EnableContextOffloading = &disabled
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling large output virtual tools (context offloading) for %s", agentType))

	// Setup Downloads folder for browser tools (Playwright or agent-browser)
	// Use shared function to ensure all agent types set the override correctly if they use Playwright
	// Note: Learning agents typically use NoServers, but we call this for consistency and safety
	hcpo.setupBrowserDownloadsPathOverride(ctx, config, stepConfig)

	// 4. Prepare workspace tools EXCLUDING human tools
	// Learning agents are pure LLM analysis agents and should NOT have human tools (like human_feedback)
	// They should not block on human input - they only read execution history and write learnings
	toolsToRegister, executorsToUse := hcpo.prepareWorkspaceToolsOnly()
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using workspace-only tools for %s: %d tools (human tools excluded)", agentType, len(toolsToRegister)))

	// 5. Use base factory! (This handles all setup automatically)
	// Use stepIndex directly (passed from runSuccessLearningPhase) instead of parsing from stepPath
	// This ensures learning costs are correctly attributed to the step, even if stepPath parsing fails
	stepNumberForContext := stepIndex // stepIndex is already 0-based

	// Always use the unified learning agent — same prompt for both code execution and non-code execution modes
	createAgentFunc := func(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
		return NewWorkflowLearningAgent(config, logger, tracer, eventBridge)
	}

	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		stepNumberForContext,
		0,      // iteration (not used for learning agents)
		stepID, // Step ID
		createAgentFunc,
		toolsToRegister,
		executorsToUse,
		false, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup %s: %w", agentType, err)
	}

	// 6. Post-setup: folder guard paths and code execution registry (after base factory setup)
	// Note: Base factory already updates code execution registry, but we need to set folder guard paths
	// on mcpAgent first, then update registry again with correct paths (for extraction agents only)
	// Only update registry for extraction agents in code execution mode
	shouldUpdateRegistry := wasCodeExecutionMode
	if err := hcpo.applyPostSetupToAgent(agent, agentName, shouldUpdateRegistry); err != nil {
		// Log warning but don't fail agent creation
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for %s: %v", agentName, err))
	}

	return agent, nil
}

// createLearningAgent creates a unified learning agent for analyzing executions (both successful and failed)
// The agent handles both success and failure patterns automatically based on validation results
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// Note: Learning integration functions removed - execution agent now auto-discovers learning files and scripts

// createSuccessLearningAgent is a backward compatibility wrapper for createLearningAgent
// Deprecated: Use createLearningAgent instead. The unified learning agent handles both success and failure cases.
// stepIndex: 0-based step index for token tracking
func (hcpo *StepBasedWorkflowOrchestrator) createSuccessLearningAgent(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, isCodeExecutionMode bool, stepID string, stepPath string, stepIndex int) (agents.OrchestratorAgent, error) {
	return hcpo.createLearningAgentInternal(ctx, phase, learningPathIdentifier, agentName, stepConfig, isCodeExecutionMode, stepID, stepPath, stepIndex)
}

// createConditionalAgent creates a conditional agent using the standard factory pattern
// This ensures proper event bridge connection, context setup, and tool registration
// stepPath: Step path identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// stepID: Step ID for step-specific learnings folder access (e.g., "step-3" or branch step ID)
func (hcpo *StepBasedWorkflowOrchestrator) createConditionalAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs, conditionalLLMConfig *orchestrator.LLMConfig, stepPath string, stepID string) (agents.OrchestratorAgent, error) {
	// 1. Setup folder guard
	readPaths, writePaths := hcpo.setupConditionalFolderGuard(stepPath, stepID)
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for conditional agent - Read paths: %v, Write paths: %v (can write to %s)", readPaths, writePaths, stepPath))

	// Determine max turns: use orchestrator default (conditional agents don't have step-specific max turns config)
	maxTurns := hcpo.GetMaxTurns()
	// Note: ConditionalMaxTurns doesn't exist in AgentConfigs - using orchestrator default

	// Use the LLM config passed from caller. The caller resolves it from the tier
	// resolver (see getConditionalAgentForStep); there is no step-level LLM
	// override for conditional evaluators.
	llmConfig := conditionalLLMConfig
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for conditional agent: conditional override, step execution override, and tiered workflow config are unavailable")
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	workflowServersConditional := hcpo.GetSelectedServers()
	// Use step-specific servers filtered against workflow-level servers (workflow is the hard cap)
	if stepConfig != nil && stepConfig.SelectedServers != nil && len(stepConfig.SelectedServers) > 0 {
		filtered := filterServersByWorkflow(stepConfig.SelectedServers, workflowServersConditional)
		config.ServerNames = filtered
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific conditional servers (workflow-filtered): %v → %v", stepConfig.SelectedServers, filtered))
	} else {
		// Use orchestrator defaults when stepConfig is nil or SelectedServers is empty
		config.ServerNames = normalizeServerNames(workflowServersConditional)
		if stepConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but SelectedServers is empty - using orchestrator defaults: %v", config.ServerNames))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config not found - using orchestrator defaults: %v", config.ServerNames))
		}
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		filtered := filterToolsByWorkflow(stepConfig.SelectedTools, workflowServersConditional)
		config.SelectedTools = filtered
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific conditional tools (workflow-filtered): %v → %v", stepConfig.SelectedTools, filtered))
	} else {
		config.SelectedTools = hcpo.GetSelectedTools()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default conditional tools: %v", config.SelectedTools))
	}

	// Code execution mode: 3-rule priority (same as execution agent)
	// Rule 1: CLI providers always use code execution
	// Rule 2: Step config if explicitly set
	// Rule 3: Non-CLI providers default to false
	conditionalProvider := config.LLMConfig.Primary.Provider
	if conditionalProvider == "claude-code" || conditionalProvider == "kimi" || conditionalProvider == "gemini-cli" || conditionalProvider == "codex-cli" {
		config.UseCodeExecutionMode = true
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode forced for conditional agent CLI provider '%s'", conditionalProvider))
	} else if stepConfig != nil && stepConfig.UseCodeExecutionMode != nil {
		config.UseCodeExecutionMode = *stepConfig.UseCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode for conditional agent: %v", config.UseCodeExecutionMode))
	} else {
		config.UseCodeExecutionMode = false
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Conditional agent provider '%s': code execution mode disabled", conditionalProvider))
	}

	// Set EnableContextOffloading if specified
	if stepConfig != nil && stepConfig.EnableContextOffloading != nil {
		config.EnableContextOffloading = stepConfig.EnableContextOffloading
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific context offloading setting: %v", *stepConfig.EnableContextOffloading))
	}

	// Setup Downloads folder for browser tools (Playwright or agent-browser)
	// CRITICAL FIX (Jan 2026): Conditional agents can use Playwright (via step-specific servers or orchestrator defaults).
	// If the conditional agent creates the Playwright connection first, it must set the correct Downloads path override
	// before creating the connection, otherwise all subsequent agents will reuse the wrong connection.
	// Use shared function to ensure all agent types set the override correctly.
	hcpo.setupBrowserDownloadsPathOverride(ctx, config, stepConfig)

	// Prepare custom tools and executors (same as execution agent)
	// Filter by enabled categories and/or specific tools if specified
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && len(stepConfig.EnabledCustomTools) > 0 {
		// Filter tools based on unified format (category:tool or category:*)
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			stepConfig.EnabledCustomTools,
		)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered custom tools for conditional agent: %d tools enabled from %d entries: %v", len(toolsToRegister), len(stepConfig.EnabledCustomTools), stepConfig.EnabledCustomTools))
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using all workspace tools for conditional agent: %d tools", len(toolsToRegister)))
	}

	// Use standard factory pattern - this handles initialization, event bridge connection, and tool registration
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		step,
		iteration,
		stepID, // Step ID
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowConditionalAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister, // Pass workspace tools (filtered by step config if specified)
		executorsToUse,  // Pass workspace tool executors
		false,           // Don't overwrite system prompt - conditional agent manages its own prompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create conditional agent: %w", err)
	}

	// 7. Post-setup: folder guard paths (conditional agents don't use code execution mode, so no registry update needed)
	// Note: Folder guard paths are already set on orchestrator, but we need to apply them to the agent
	if err := hcpo.applyPostSetupToAgent(agent, agentName, false); err != nil {
		return nil, fmt.Errorf("failed to apply post-setup to conditional agent: %w", err)
	}

	// Inject supplementary prompts (skills, secrets, browser instructions)
	effectiveSkills := GetEffectiveSkills(stepConfig, hcpo.BaseOrchestrator)
	if baseAgent := agent.GetBaseAgent(); baseAgent != nil {
		if mcpAgent := baseAgent.Agent(); mcpAgent != nil {
			hcpo.appendSupplementaryPrompts(ctx, mcpAgent, config, effectiveSkills, "")
		}
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Created conditional agent using standard factory pattern: %s (step %d, phase %s)", agentName, step+1, phase))
	return agent, nil
}

// ConversationEntry is a single flattened message in the sub-agent's conversation
type ConversationEntry struct {
	Index    int    `json:"index"`
	Role     string `json:"role"`              // "user", "assistant", "tool_call", "tool_result"
	Content  string `json:"content,omitempty"` // text content
	ToolName string `json:"tool_name,omitempty"`
	ToolArgs string `json:"tool_args,omitempty"`
}

// SubAgentCallRecord stores the full record of a single call_sub_agent / call_generic_agent call
type SubAgentCallRecord struct {
	Index         int                 `json:"index"` // 1-based call order
	CalledAt      time.Time           `json:"called_at"`
	TodoID        string              `json:"todo_id"`
	RouteID       string              `json:"route_id,omitempty"` // empty for generic
	AgentType     string              `json:"agent_type"`         // "predefined" | "generic"
	Success       bool                `json:"success"`
	Result        string              `json:"result"`
	Error         string              `json:"error,omitempty"`
	ExecutionTime string              `json:"execution_time"`
	Conversation  []ConversationEntry `json:"conversation"`
}

// SubAgentExecutionContext holds the context needed for sub-agent execution from tools
type SubAgentExecutionContext struct {
	TodoTaskStep *TodoTaskPlanStep
	StepIndex    int
	StepPath     string
	AllSteps     []PlanStepInterface
	Progress     *StepProgress
	StepConfig   *AgentConfigs // Step-level configuration for LLM overrides

	// TierSelectionRequired tells the sub-agent tool handlers to reject calls that
	// don't include a valid preferred_tier. Mirrors the enableTierSelection flag
	// used when building the tool schema.
	TierSelectionRequired bool

	// WorkshopCorrelationID is the correlation ID from the workshop's execute_step call.
	// Propagated to sub-agent contexts so their events are tagged with the workshop step's ID.
	WorkshopCorrelationID string

	// CallHistory records every sub-agent call made during this todo task step.
	// Protected by callHistoryMu for concurrent tool calls.
	CallHistory   []SubAgentCallRecord
	callHistoryMu sync.Mutex
}

// serializeConversationHistory converts raw llmtypes conversation history into a flat list of ConversationEntry
func serializeConversationHistory(history []llmtypes.MessageContent) []ConversationEntry {
	var entries []ConversationEntry
	for i, msg := range history {
		for _, part := range msg.Parts {
			entry := ConversationEntry{Index: i + 1}
			switch msg.Role {
			case llmtypes.ChatMessageTypeHuman:
				entry.Role = "user"
				if tc, ok := part.(llmtypes.TextContent); ok {
					entry.Content = tc.Text
				}
			case llmtypes.ChatMessageTypeAI:
				if tc, ok := part.(llmtypes.TextContent); ok {
					entry.Role = "assistant"
					entry.Content = tc.Text
				} else if tc, ok := part.(llmtypes.ToolCall); ok && tc.FunctionCall != nil {
					entry.Role = "tool_call"
					entry.ToolName = tc.FunctionCall.Name
					entry.ToolArgs = tc.FunctionCall.Arguments
				}
			case llmtypes.ChatMessageTypeTool:
				if tc, ok := part.(llmtypes.ToolCallResponse); ok {
					entry.Role = "tool_result"
					entry.ToolName = tc.Name
					entry.Content = tc.Content
				}
			}
			if entry.Role != "" {
				entries = append(entries, entry)
			}
		}
	}
	return entries
}

// createTodoTaskOrchestratorAgent creates a todo task orchestrator agent using the standard factory pattern
// This agent manages todo lists, creates tasks, and delegates to predefined or generic sub-agents
// Note: Folder guard paths should be set by the caller before calling this function (see controller_todo_task.go)
// The stepPath parameter is used to inject context for todo tools (e.g., "step-1")
// The subAgentExecCtx contains context for sub-agent tool execution (can be nil for simple cases)
func (hcpo *StepBasedWorkflowOrchestrator) createTodoTaskOrchestratorAgent(ctx context.Context, phase string, step, iteration int, stepID string, stepPath string, agentName string, stepConfig *AgentConfigs, todoTaskLLMConfig *orchestrator.LLMConfig, subAgentExecCtx *SubAgentExecutionContext) (agents.OrchestratorAgent, error) {
	// Todo task orchestrator agent needs folder guard (can write files)
	// Note: Folder guard is set by caller in controller_todo_task.go before agent creation
	// We apply it to the agent here via post-setup

	// Determine max turns: use orchestrator default
	maxTurns := hcpo.GetMaxTurns()

	// Determine LLM config: Priority: step config > preset default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if todoTaskLLMConfig != nil && todoTaskLLMConfig.Primary.Provider != "" && todoTaskLLMConfig.Primary.ModelID != "" {
		var apiKeys *orchestrator.APIKeys
		if orchestratorLLMConfig != nil {
			apiKeys = orchestratorLLMConfig.APIKeys
		}
		llmConfig = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: todoTaskLLMConfig.Primary.Provider,
				ModelID:  todoTaskLLMConfig.Primary.ModelID,
			},
			Fallbacks: todoTaskLLMConfig.Fallbacks,
			APIKeys:   apiKeys, // Preserve API keys from orchestrator (may be nil)
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific todo task orchestrator LLM: %s/%s", todoTaskLLMConfig.Primary.Provider, todoTaskLLMConfig.Primary.ModelID))
	} else if hcpo.tierResolver != nil {
		// Use tiered allocation (high tier for orchestration)
		tieredLLM := hcpo.tierResolver.ResolveTier(TierHigh)
		if tieredLLM != nil {
			llmConfig = tieredLLM
			hcpo.GetLogger().Info(fmt.Sprintf("🏷️ Using Tier 1 (High) for todo task orchestrator: %s/%s", tieredLLM.Primary.Provider, tieredLLM.Primary.ModelID))
		}
	}
	if llmConfig == nil && hcpo.presetPhaseLLM != nil && hcpo.presetPhaseLLM.Provider != "" && hcpo.presetPhaseLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetPhaseLLM.Provider,
				ModelID:  hcpo.presetPhaseLLM.ModelID,
			},
			Fallbacks: convertAgentFallbacks(hcpo.presetPhaseLLM.Fallbacks),
			APIKeys:   orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset phase LLM for todo task orchestrator: %s/%s", hcpo.presetPhaseLLM.Provider, hcpo.presetPhaseLLM.ModelID))
	}
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for todo task orchestrator agent: step config, tiered, and preset phase LLM are all empty or invalid")
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)
	hcpo.disableParentAgentTimeout(config, "todo task orchestrator agent")

	// Give nested todo_task orchestrators their own session-level folder guard just like
	// normal execution steps. Without this, shell calls fall back to the broader parent
	// workflow/group MCP session and can see workflow-root files or sibling groups.
	todoReadPaths, todoWritePaths := hcpo.GetFolderGuardPaths()
	todoSessionID := hcpo.setupSubAgentSessionGuard("todo", stepID, todoReadPaths, todoWritePaths)
	config.MCPSessionID = todoSessionID
	sharedBrowserSessionID := hcpo.resolveWorkshopBrowserSessionID(hcpo.currentGroupName)
	hcpo.bindWorkshopBrowserSession(todoSessionID, sharedBrowserSessionID)
	config.FolderGuardReadPaths = todoReadPaths
	config.FolderGuardWritePaths = todoWritePaths

	workflowServersTodo := hcpo.GetSelectedServers()
	// Use step-specific servers filtered against workflow-level servers (workflow is the hard cap)
	if stepConfig != nil && stepConfig.SelectedServers != nil && len(stepConfig.SelectedServers) > 0 {
		filtered := filterServersByWorkflow(stepConfig.SelectedServers, workflowServersTodo)
		config.ServerNames = filtered
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific todo task orchestrator servers (workflow-filtered): %v → %v", stepConfig.SelectedServers, filtered))
	} else {
		// Use orchestrator defaults when stepConfig is nil or SelectedServers is empty
		config.ServerNames = normalizeServerNames(workflowServersTodo)
		if stepConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but SelectedServers is empty - using orchestrator defaults: %v", config.ServerNames))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config not found - using orchestrator defaults: %v", config.ServerNames))
		}
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		filtered := filterToolsByWorkflow(stepConfig.SelectedTools, workflowServersTodo)
		config.SelectedTools = filtered
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific todo task orchestrator tools (workflow-filtered): %v → %v", stepConfig.SelectedTools, filtered))
	} else {
		config.SelectedTools = hcpo.GetSelectedTools()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default todo task orchestrator tools: %v", config.SelectedTools))
	}

	// Enable code execution mode for CLI providers (claude-code, gemini-cli) that need HTTP bridge for tool routing
	// Non-CLI providers use simple agent mode (no code execution)
	isCodeExecutionMode := llmConfig.Primary.Provider == "claude-code" || llmConfig.Primary.Provider == "kimi" || llmConfig.Primary.Provider == "gemini-cli" || llmConfig.Primary.Provider == "codex-cli"
	config.UseCodeExecutionMode = isCodeExecutionMode
	if isCodeExecutionMode {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Todo task orchestrator: code execution mode enabled for CLI provider '%s'", llmConfig.Primary.Provider))
	}

	// Enable parallel tool execution for todo task orchestrator
	// This allows concurrent execution of multiple tool calls (e.g., call_sub_agent, call_generic_agent)
	config.EnableParallelToolExecution = true
	hcpo.GetLogger().Info("⚡ Parallel tool execution enabled for todo task orchestrator agent")

	// Set EnableContextOffloading if specified
	if stepConfig != nil && stepConfig.EnableContextOffloading != nil {
		config.EnableContextOffloading = stepConfig.EnableContextOffloading
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific context offloading setting: %v", *stepConfig.EnableContextOffloading))
	}

	// Setup Downloads folder for browser tools (Playwright or agent-browser)
	hcpo.setupBrowserDownloadsPathOverride(ctx, config, stepConfig)

	// Prepare custom tools and executors
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && len(stepConfig.EnabledCustomTools) > 0 {
		// Filter tools based on unified format (category:tool or category:*)
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			stepConfig.EnabledCustomTools,
		)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered custom tools for todo task orchestrator agent: %d tools enabled from %d entries: %v", len(toolsToRegister), len(stepConfig.EnabledCustomTools), stepConfig.EnabledCustomTools))
	} else {
		toolsToRegister = hcpo.WorkspaceTools
		// Clone to avoid mutating the shared workspace executor map when we wrap
		// execute_shell_command below to inject STEP_OUTPUT_DIR / STEP_EXECUTION_DIR.
		executorsToUse = make(map[string]interface{}, len(hcpo.WorkspaceToolExecutors))
		for k, v := range hcpo.WorkspaceToolExecutors {
			executorsToUse[k] = v
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using all workspace tools for todo task orchestrator agent: %d tools", len(toolsToRegister)))
	}

	// Inject STEP_OUTPUT_DIR and STEP_EXECUTION_DIR into execute_shell_command so the
	// todo-task orchestrator's own shell calls resolve sibling step outputs via env vars
	// rather than having to rebuild absolute paths from the step context.
	{
		stepExecutionRelPath := hcpo.getTodoTaskStepExecutionPath(stepID, stepPath)
		stepOutputAbsPath := filepath.Join(GetPromptDocsRoot(), stepExecutionRelPath)
		stepExecutionAbsPath := filepath.Dir(stepOutputAbsPath)
		// The todo-task orchestrator now uses a dedicated MCP session for shell/file tools.
		// Browser reuse is bound separately above, so this session override narrows
		// filesystem scope without breaking shared browser behavior with the builder.
		injectStepEnvIntoShellExecutor(executorsToUse, stepOutputAbsPath, stepExecutionAbsPath, config.MCPSessionID)
		hcpo.GetLogger().Info(fmt.Sprintf("📂 Injecting step shell env into execute_shell_command for todo task %s: STEP_OUTPUT_DIR=%s MCP_SESSION_ID=%s", stepID, stepOutputAbsPath, config.MCPSessionID))
	}

	// NOTE: Task management is handled directly by the orchestrator LLM via shell commands

	// Filter out human tools if "no human" execution mode is active
	execOpts := hcpo.GetExecutionOptions()
	if execOpts != nil && (execOpts.ExecutionStrategy == ExecutionStrategyStartFromBeginningNoHuman || execOpts.ExecutionStrategy == ExecutionStrategyResumeFromStepNoHuman) {
		var filteredTools []llmtypes.Tool
		filteredExecutors := make(map[string]interface{})

		for _, tool := range toolsToRegister {
			// Check if this tool is a human tool by looking at its category
			if hcpo.ToolCategories != nil {
				if category, exists := hcpo.ToolCategories[tool.Function.Name]; exists && category == "human" {
					// Skip human tools in "no human" mode
					hcpo.GetLogger().Info(fmt.Sprintf("🔧 Excluding human tool '%s' from todo task orchestrator agent (no human mode)", tool.Function.Name))
					continue
				}
			}
			filteredTools = append(filteredTools, tool)
			// Also filter executors
			if executor, exists := executorsToUse[tool.Function.Name]; exists {
				filteredExecutors[tool.Function.Name] = executor
			}
		}

		toolsToRegister = filteredTools
		executorsToUse = filteredExecutors
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered out human tools for todo task orchestrator agent (no human mode): %d tools remaining", len(toolsToRegister)))
	}

	// IMPORTANT: Inject sub-agent tools for tool-based delegation
	// These tools allow the orchestrator to delegate work to sub-agents directly via tool calls
	if subAgentExecCtx != nil {
		// Tier selection is always required on every sub-agent call. The orchestrator
		// must reason about task difficulty per delegation — this is prompt-discipline
		// even when the workflow has no tier resolver or the step pins an ExecutionLLM.
		// In those cases the tier value is honored by the sub-agent LLM-selection path
		// if possible, else silently falls through to the inherited/pinned LLM.
		subAgentExecCtx.TierSelectionRequired = true
		subAgentTools := virtualtools.CreateSubAgentTools()
		subAgentExecutors := virtualtools.CreateSubAgentToolExecutors()
		subAgentCategory := virtualtools.GetSubAgentToolCategory()

		// Add sub-agent tools to the tools list and register their category
		for _, tool := range subAgentTools {
			toolsToRegister = append(toolsToRegister, tool)
			// CRITICAL: Add to ToolCategories map so the tool passes validation
			if hcpo.ToolCategories != nil {
				hcpo.ToolCategories[tool.Function.Name] = subAgentCategory
			}
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Added sub-agent tool '%s' to todo task orchestrator (category: %s)", tool.Function.Name, subAgentCategory))
		}

		// Wrap sub-agent executors with context injection
		for toolName, executor := range subAgentExecutors {
			wrappedExecutor := hcpo.wrapSubAgentToolExecutor(executor, subAgentExecCtx)
			executorsToUse[toolName] = wrappedExecutor
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Wrapped sub-agent tool '%s' with execution context injection", toolName))
		}
	} else {
		hcpo.GetLogger().Info("🔧 Sub-agent execution context not provided - sub-agent tools will not be available")
	}

	// NOTE: mark_step_complete tool removed — completion is detected by pre-validation.

	// Use standard factory pattern - this handles initialization, event bridge connection, and tool registration
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		step,
		iteration,
		stepID, // Step ID
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowTodoTaskOrchestratorAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister, // Pass workspace tools (filtered by step config if specified)
		executorsToUse,  // Pass workspace tool executors
		false,           // Don't overwrite system prompt - todo task orchestrator agent manages its own prompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create todo task orchestrator agent: %w", err)
	}

	// Post-setup: folder guard paths (todo task orchestrator agent may use code execution mode, so registry update may be needed)
	// Note: Folder guard paths are already set on orchestrator by caller, but we need to apply them to the agent
	if err := hcpo.applyPostSetupToAgent(agent, agentName, isCodeExecutionMode); err != nil {
		return nil, fmt.Errorf("failed to apply post-setup to todo task orchestrator agent: %w", err)
	}

	// Inject supplementary prompts (skills, secrets, browser instructions).
	effectiveSkills := GetEffectiveSkills(stepConfig, hcpo.BaseOrchestrator)
	if baseAgent := agent.GetBaseAgent(); baseAgent != nil {
		if mcpAgent := baseAgent.Agent(); mcpAgent != nil {
			hcpo.appendSupplementaryPrompts(ctx, mcpAgent, config, effectiveSkills, "")
		}
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Created todo task orchestrator agent using standard factory pattern: %s (step %d, phase %s)", agentName, step+1, phase))
	return agent, nil
}

// restoreSubAgentToolExecutors re-registers the outer todo_task's sub-agent executors in the
// session-scoped code execution registry. This is called after a nested todo_task sub-agent
// completes, because the nested todo_task overwrites the session registry entry for call_sub_agent
// with its own inner routes. Without this restore, any subsequent call_sub_agent calls in the
// same LLM turn (code execution mode) would incorrectly route to the inner execCtx's routes.
func (hcpo *StepBasedWorkflowOrchestrator) restoreSubAgentToolExecutors(execCtx *SubAgentExecutionContext) {
	sessionID := hcpo.getSessionID()
	if sessionID == "" {
		return
	}
	subAgentExecutors := virtualtools.CreateSubAgentToolExecutors()
	wrappedExecutors := make(map[string]func(ctx context.Context, args map[string]interface{}) (string, error), len(subAgentExecutors))
	for toolName, executor := range subAgentExecutors {
		wrappedExecutors[toolName] = hcpo.wrapSubAgentToolExecutor(executor, execCtx)
	}
	codeexec.InitRegistryForSession(sessionID, wrappedExecutors, hcpo.GetLogger())
	hcpo.GetLogger().Info("🔄 Restored outer sub-agent tool executors in session registry after nested todo_task completion")
}

// wrapSubAgentToolExecutor wraps a sub-agent tool executor to inject execution functions
// The wrapper adds: execute_predefined_sub_agent, execute_generic_agent, predefined_routes, sub_agent_llm
func (hcpo *StepBasedWorkflowOrchestrator) wrapSubAgentToolExecutor(
	originalExecutor func(ctx context.Context, args map[string]interface{}) (string, error),
	execCtx *SubAgentExecutionContext,
) func(ctx context.Context, args map[string]interface{}) (string, error) {
	// Return wrapper function that injects execution functions into context
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Signal to handlers that preferred_tier must be supplied when dynamic tier
		// selection is active for this orchestrator.
		if execCtx.TierSelectionRequired {
			ctx = context.WithValue(ctx, virtualtools.TierSelectionRequiredKey, true)
		}

		// Inject execute_predefined_sub_agent function
		executePredefinedFunc := hcpo.createExecutePredefinedSubAgentFunc(execCtx)
		ctx = context.WithValue(ctx, virtualtools.ExecutePredefinedSubAgentKey, executePredefinedFunc)

		// Inject execute_generic_agent function
		executeGenericFunc := hcpo.createExecuteGenericAgentFunc(execCtx)
		ctx = context.WithValue(ctx, virtualtools.ExecuteGenericAgentKey, executeGenericFunc)

		// Inject predefined routes for route lookup
		if execCtx.TodoTaskStep != nil {
			ctx = context.WithValue(ctx, virtualtools.PredefinedRoutesKey, execCtx.TodoTaskStep.PredefinedRoutes)

			// Build route descriptions map for get_route_description tool
			routeDescriptions := make(map[string]string)
			for _, route := range execCtx.TodoTaskStep.PredefinedRoutes {
				desc := ResolveVariables(route.Condition, hcpo.variableValues)
				if route.SubAgentStep != nil {
					desc += "\n\nDescription: " + ResolveVariables(route.SubAgentStep.GetDescription(), hcpo.variableValues)
				}
				routeDescriptions[route.RouteID] = desc
			}
			ctx = context.WithValue(ctx, virtualtools.RouteDescriptionsKey, routeDescriptions)
		}

		// Inject the parent step's ExecutionLLM as the sub-agent LLM override so every
		// sub-agent spawned by this todo-task orchestrator uses the same LLM as the
		// orchestrator itself. Works in both tiered and manual modes; skipped for
		// dynamic tier selection at the consumer side.
		if execCtx.StepConfig != nil && execCtx.StepConfig.ExecutionLLM != nil {
			ctx = context.WithValue(ctx, virtualtools.SubAgentLLMContextKey, execCtx.StepConfig.ExecutionLLM)
		}

		// Inject get_sub_agent_conversation function
		getConvFunc := virtualtools.GetSubAgentConversationFunc(func(ctx context.Context, todoID string, fromLastX, offsetLastX int) (string, error) {
			execCtx.callHistoryMu.Lock()
			defer execCtx.callHistoryMu.Unlock()
			for i := len(execCtx.CallHistory) - 1; i >= 0; i-- {
				if execCtx.CallHistory[i].TodoID == todoID {
					record := execCtx.CallHistory[i]
					conv := record.Conversation
					total := len(conv)
					end := total - offsetLastX
					if end < 0 {
						end = 0
					}
					start := end - fromLastX
					if start < 0 {
						start = 0
					}
					trimmed := record // shallow copy
					trimmed.Conversation = conv[start:end]
					type resultWrapper struct {
						TotalEntries int                `json:"total_entries"`
						Showing      string             `json:"showing"`
						Record       SubAgentCallRecord `json:"record"`
					}
					out := resultWrapper{
						TotalEntries: total,
						Showing:      fmt.Sprintf("entries %d-%d of %d", start+1, end, total),
						Record:       trimmed,
					}
					data, _ := json.MarshalIndent(out, "", "  ")
					return string(data), nil
				}
			}
			return "", fmt.Errorf("no sub-agent call found for todo_id %q", todoID)
		})
		ctx = context.WithValue(ctx, virtualtools.GetSubAgentConversationKey, getConvFunc)

		// Only emit task status updates for tools that change state (call_sub_agent, call_generic_agent),
		// Call original executor with enriched context
		result, err := originalExecutor(ctx, args)

		return result, err
	}
}

// createExecutePredefinedSubAgentFunc creates a function that executes predefined sub-agents
// This function is injected into context for the call_sub_agent tool to use
func (hcpo *StepBasedWorkflowOrchestrator) createExecutePredefinedSubAgentFunc(
	execCtx *SubAgentExecutionContext,
) virtualtools.ExecutePredefinedSubAgentFunc {
	return func(ctx context.Context, routeID, todoID, instructions string) (string, error) {
		hcpo.GetLogger().Info(fmt.Sprintf("🤖 [TOOL] Executing predefined sub-agent via tool: route=%s, todo=%s", routeID, todoID))

		if strings.TrimSpace(routeID) == "" {
			return "", fmt.Errorf("invalid or missing route_id")
		}
		if execCtx.TodoTaskStep == nil {
			return "", fmt.Errorf("call_sub_agent is only available inside a todo_task step")
		}
		validRouteIDs := make([]string, 0, len(execCtx.TodoTaskStep.PredefinedRoutes))
		routeExists := false
		for _, route := range execCtx.TodoTaskStep.PredefinedRoutes {
			validRouteIDs = append(validRouteIDs, route.RouteID)
			if route.RouteID == routeID {
				routeExists = true
			}
		}
		if !routeExists {
			return "", fmt.Errorf("route_id %q not found in todo task step %q. Available route IDs: %v", routeID, execCtx.TodoTaskStep.GetID(), validRouteIDs)
		}

		// Propagate workshop correlation IDs to sub-agent context so events are tagged correctly.
		// The ctx here comes from the tool call (mcpagent), which may not have the workshop's
		// ForceCorrelationIDKey. Use the workshop correlation ID from SubAgentExecutionContext.
		if execCtx.WorkshopCorrelationID != "" {
			if forcedID, ok := ctx.Value(orchestrator_events.ForceCorrelationIDKey).(string); !ok || forcedID == "" {
				ctx = context.WithValue(ctx, orchestrator_events.ForceCorrelationIDKey, execCtx.WorkshopCorrelationID)
				ctx = context.WithValue(ctx, orchestrator_events.IsSubAgentContextKey, true)
			}
		}

		// Browser isolation: generate isolated session ID when share_browser=false
		if sb, ok := ctx.Value(virtualtools.SubAgentShareBrowserKey).(bool); ok && !sb {
			isolatedSessionID := fmt.Sprintf("%s-isolated-%d", hcpo.getSessionID(), time.Now().UnixNano())
			ctx = context.WithValue(ctx, virtualtools.SubAgentIsolatedSessionIDKey, isolatedSessionID)
			hcpo.GetLogger().Info(fmt.Sprintf("Browser isolation: sub-agent gets session %s", isolatedSessionID))
			defer func() {
				// Close the MCP session (Playwright connection)
				mcpagent.CloseSession(isolatedSessionID)
				// Close agent-browser processes and remove from tracker
				tracker := browser.GetSessionTracker()
				workspaceAPIURL := os.Getenv("WORKSPACE_API_URL")
				if workspaceAPIURL == "" {
					workspaceAPIURL = "http://127.0.0.1:8081"
				}
				browserClient := browser.NewClient(workspaceAPIURL)
				// Close all browser sessions tracked under this isolated agent ID
				sessions := tracker.SessionsForAgent(isolatedSessionID)
				for _, s := range sessions {
					tracker.CloseSession(s, browserClient)
				}
				hcpo.GetLogger().Info(fmt.Sprintf("Closed isolated browser session: %s (cleaned %d browser processes)", isolatedSessionID, len(sessions)))
			}()
		}

		// Build a TodoTaskResponse to reuse existing execution logic
		response := &TodoTaskResponse{
			NextAction:             "delegate",
			SelectedRouteID:        routeID,
			TodoIDToExecute:        todoID,
			InstructionsToSubAgent: instructions,
		}

		// Emit route selected event BEFORE sub-agent execution so it appears before the agent card
		hcpo.emitTodoTaskRouteSelectedEvent(ctx, execCtx.TodoTaskStep, execCtx.StepIndex, execCtx.StepPath, 0, response, nil, "")

		startTime := time.Now()

		// Execute using existing method
		result, history, err := hcpo.executePredefinedSubAgent(
			ctx,
			execCtx.TodoTaskStep,
			execCtx.StepIndex,
			execCtx.StepPath,
			response,
			execCtx.AllSteps,
			execCtx.Progress,
		)

		executionTime := time.Since(startTime)

		// RESTORE: Re-register outer sub-agent executors in the session registry.
		// A nested todo_task sub-agent overwrites the session-scoped call_sub_agent executor
		// with its own inner routes. After it returns, restore the outer executor so that
		// subsequent call_sub_agent calls in the same LLM turn (code execution mode) hit the
		// correct outer routes and not the inner ones.
		hcpo.restoreSubAgentToolExecutors(execCtx)

		// Store call record for get_sub_agent_conversation
		record := SubAgentCallRecord{
			CalledAt:      startTime,
			TodoID:        todoID,
			RouteID:       routeID,
			AgentType:     "predefined",
			Success:       err == nil,
			Result:        result,
			ExecutionTime: executionTime.String(),
			Conversation:  serializeConversationHistory(history),
		}
		if err != nil {
			record.Error = err.Error()
		}
		execCtx.callHistoryMu.Lock()
		record.Index = len(execCtx.CallHistory) + 1
		execCtx.CallHistory = append(execCtx.CallHistory, record)
		execCtx.callHistoryMu.Unlock()

		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [TOOL] Predefined sub-agent execution failed: %v", err))
			if strings.Contains(err.Error(), "route ") && strings.Contains(err.Error(), " not found in predefined routes") {
				return "", err
			}
			return fmt.Sprintf("ERROR: %v", err), nil // Return error as result, not as error (agent can handle)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ [TOOL] Predefined sub-agent completed successfully: route=%s, todo=%s", routeID, todoID))
		return result, nil
	}
}

// createExecuteGenericAgentFunc creates a function that executes generic agents
// This function is injected into context for the call_generic_agent tool to use
// Sub-agents get all their input from the tool parameters (instructions)
// They do NOT read the tasks.md file - the orchestrator provides everything via the tool call
func (hcpo *StepBasedWorkflowOrchestrator) createExecuteGenericAgentFunc(
	execCtx *SubAgentExecutionContext,
) virtualtools.ExecuteGenericAgentFunc {
	return func(ctx context.Context, todoID, instructions string) (string, error) {
		hcpo.GetLogger().Info(fmt.Sprintf("🤖 [TOOL] Executing generic agent via tool: todo=%s", todoID))

		// Propagate workshop correlation IDs to sub-agent context
		if execCtx.WorkshopCorrelationID != "" {
			if forcedID, ok := ctx.Value(orchestrator_events.ForceCorrelationIDKey).(string); !ok || forcedID == "" {
				ctx = context.WithValue(ctx, orchestrator_events.ForceCorrelationIDKey, execCtx.WorkshopCorrelationID)
				ctx = context.WithValue(ctx, orchestrator_events.IsSubAgentContextKey, true)
			}
		}

		// Browser isolation: generate isolated session ID when share_browser=false
		if sb, ok := ctx.Value(virtualtools.SubAgentShareBrowserKey).(bool); ok && !sb {
			isolatedSessionID := fmt.Sprintf("%s-isolated-%d", hcpo.getSessionID(), time.Now().UnixNano())
			ctx = context.WithValue(ctx, virtualtools.SubAgentIsolatedSessionIDKey, isolatedSessionID)
			hcpo.GetLogger().Info(fmt.Sprintf("Browser isolation: sub-agent gets session %s", isolatedSessionID))
			defer func() {
				// Close the MCP session (Playwright connection)
				mcpagent.CloseSession(isolatedSessionID)
				// Close agent-browser processes and remove from tracker
				tracker := browser.GetSessionTracker()
				workspaceAPIURL := os.Getenv("WORKSPACE_API_URL")
				if workspaceAPIURL == "" {
					workspaceAPIURL = "http://127.0.0.1:8081"
				}
				browserClient := browser.NewClient(workspaceAPIURL)
				// Close all browser sessions tracked under this isolated agent ID
				sessions := tracker.SessionsForAgent(isolatedSessionID)
				for _, s := range sessions {
					tracker.CloseSession(s, browserClient)
				}
				hcpo.GetLogger().Info(fmt.Sprintf("Closed isolated browser session: %s (cleaned %d browser processes)", isolatedSessionID, len(sessions)))
			}()
		}

		// Build a TodoTaskResponse to reuse existing execution logic
		// All task info comes from the tool parameters, not from a file
		response := &TodoTaskResponse{
			NextAction:             "delegate",
			UseGenericAgent:        true,
			TodoIDToExecute:        todoID,
			InstructionsToSubAgent: instructions,
		}

		// Emit route selected event BEFORE sub-agent execution so it appears before the agent card
		hcpo.emitTodoTaskRouteSelectedEvent(ctx, execCtx.TodoTaskStep, execCtx.StepIndex, execCtx.StepPath, 0, response, nil, "")

		startTime := time.Now()

		// Execute using existing method
		// All task info comes from tool parameters
		result, history, err := hcpo.executeGenericAgent(
			ctx,
			execCtx.TodoTaskStep,
			execCtx.StepIndex,
			execCtx.StepPath,
			response,
			execCtx.AllSteps,
			execCtx.Progress,
		)

		executionTime := time.Since(startTime)

		// Store call record for get_sub_agent_conversation
		record := SubAgentCallRecord{
			CalledAt:      startTime,
			TodoID:        todoID,
			AgentType:     "generic",
			Success:       err == nil,
			Result:        result,
			ExecutionTime: executionTime.String(),
			Conversation:  serializeConversationHistory(history),
		}
		if err != nil {
			record.Error = err.Error()
		}
		execCtx.callHistoryMu.Lock()
		record.Index = len(execCtx.CallHistory) + 1
		execCtx.CallHistory = append(execCtx.CallHistory, record)
		execCtx.callHistoryMu.Unlock()

		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [TOOL] Generic agent execution failed: %v", err))
			return fmt.Sprintf("ERROR: %v", err), nil // Return error as result, not as error (agent can handle)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ [TOOL] Generic agent completed successfully: todo=%s", todoID))
		return result, nil
	}
}

// selectEvaluationScoringLLM selects the LLM config for evaluation scoring agents
// Uses Tier 2 (Medium) — scoring is analysis, not generation.
func (hcpo *StepBasedWorkflowOrchestrator) selectEvaluationScoringLLM() (*orchestrator.LLMConfig, error) {
	if hcpo.tierResolver == nil {
		return nil, fmt.Errorf("selectEvaluationScoringLLM: tier resolver is nil — tiered mode is required")
	}
	llmConfig := hcpo.tierResolver.ResolveTier(TierMedium)
	if llmConfig == nil {
		return nil, fmt.Errorf("selectEvaluationScoringLLM: tier resolver returned nil for Tier 2 (Medium)")
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🏷️ Using Tier 2 (Medium) for evaluation scoring: %s/%s", llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
	return llmConfig, nil
}

// createEvaluationScoringAgent creates a single scoring agent that scores ALL evaluation steps
// in one shot. The agent calls submit_report exactly once with the full report; the tool
// writes the JSON to disk and runs RunPreValidation against the fixed schema returned by
// BuildEvaluationReportValidationSchema. On validation failure the tool returns the error
// list so the agent retries within the same conversation.
//
// Code-execution mode: defaults to provider auto-detection (CLI providers force true). The
// workflow builder can override by adding a StepConfig with id=EvaluationScoringStepID to
// evaluation/step_config.json — same agent_configs.use_code_execution_mode field as regular
// steps, no extra schema.
func (hcpo *StepBasedWorkflowOrchestrator) createEvaluationScoringAgent(ctx context.Context, phase string, evaluationPlan *EvaluationPlan, stepInputs []EvaluationStepInput, evalReportFolder string) (*EvaluationReport, error) {
	agentName := "evaluation-scoring-agent"

	// Select LLM config
	llmConfig, err := hcpo.selectEvaluationScoringLLM()
	if err != nil {
		return nil, err
	}

	maxTurns := 100
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	config.ServerNames = []string{mcpclient.NoServers}
	autoCodeExec := requiresCodeExecutionForProvider(&AgentLLMConfig{
		Provider: config.LLMConfig.Primary.Provider,
		ModelID:  config.LLMConfig.Primary.ModelID,
	})
	config.UseCodeExecutionMode = autoCodeExec

	// Look up scoring overrides from evaluation/step_config.json under the reserved
	// EvaluationScoringStepID. ReadStepConfigs already routes to the evaluation/
	// subdir because isEvaluationMode is set by the time scoring runs.
	stepConfigs, cfgErr := hcpo.ReadStepConfigs(ctx)
	if cfgErr != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to read evaluation/step_config.json for scoring overrides: %v", cfgErr))
	}
	scoringOverride := MatchStepConfigByID(EvaluationScoringStepID, stepConfigs)
	if scoringOverride != nil && scoringOverride.UseCodeExecutionMode != nil {
		override := *scoringOverride.UseCodeExecutionMode
		// CLI providers can't drop code-exec mode — they have no other tool path.
		if !override && autoCodeExec {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Eval scoring step_config requested code_exec=false but provider '%s' requires code-exec; ignoring override", config.LLMConfig.Primary.Provider))
		} else {
			config.UseCodeExecutionMode = override
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Eval scoring code execution mode overridden by step_config (%s): %v", EvaluationScoringStepID, override))
		}
	}

	// Detect learn_code mode for the scoring agent. In learn_code the workflow can
	// ship a deterministic main.py that scores without an LLM call — see the doc on
	// tryScoringFastPath for the contract. learn_code also implies code-exec so the
	// LLM fallback can author/refine the script via shell tools.
	learnCodeMode := scoringOverride != nil && scoringOverride.DeclaredExecutionMode == ScoringLearnCodeMode

	// Fixed validation schema: validates per-step structure (id/score/reasoning/evidence),
	// score range 0-10, min text lengths, and pinned step_scores array length.
	validationSchema := BuildEvaluationReportValidationSchema(len(stepInputs))

	// Fast path: if learn_code is declared and a saved main.py exists, run it and
	// skip the LLM entirely. Validation failures or exec errors fall through to the
	// regular LLM scoring path below (which can then refine/rewrite main.py).
	if learnCodeMode {
		if report, attempted, fpErr := hcpo.tryScoringFastPath(ctx, stepInputs, validationSchema, evalReportFolder); attempted {
			if fpErr == nil && report != nil {
				return report, nil
			}
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [scoring learn_code] Fast path failed, falling back to LLM: %v", fpErr))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🐍 [scoring learn_code] No saved main.py at %s/%s yet — LLM will score this run and may author one for future runs", EvaluationScoringLearningsDir, EvaluationScoringMainPyName))
		}
		// learn_code LLM fallback always uses code-exec so the agent has shell to write main.py
		config.UseCodeExecutionMode = true
	}

	// Folder guard for the scoring agent's shell commands. Without this the guard
	// falls back to client-level defaults (empty WritePaths → mkdir/cat are denied),
	// which is why the LLM's attempt to create learnings/__evaluation_scoring__ failed
	// with "Operation not permitted". Scoring needs:
	//   read:  whole workspace (eval outputs + plan configs + existing learnings)
	//   write: learnings/__evaluation_scoring__/ (for authored main.py),
	//          evaluation/ (for scoring_inputs.json + evaluation_report.json)
	// Narrow enough that the agent can't clobber runs/, planning/, or soul/.
	if config.UseCodeExecutionMode {
		workspacePath := hcpo.GetWorkspacePath()
		scoringReadPaths := []string{workspacePath}
		scoringWritePaths := []string{
			fmt.Sprintf("%s/%s", workspacePath, EvaluationScoringLearningsDir),
			fmt.Sprintf("%s/evaluation", workspacePath),
			fmt.Sprintf("%s/Downloads", workspacePath),
		}
		hcpo.SetWorkspacePathForFolderGuard(scoringReadPaths, scoringWritePaths)
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Scoring agent folder guard — Read: %v, Write: %v", scoringReadPaths, scoringWritePaths))

		// Pre-create learnings/__evaluation_scoring__/ so the LLM doesn't need to
		// issue mkdir at all — one less failure mode on the authoring path.
		if err := hcpo.ensureStepLearningsFolderExists(ctx, EvaluationScoringLearningsDir); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to pre-create %s: %v (LLM will try to create it)", EvaluationScoringLearningsDir, err))
		}
	}

	hcpo.setupBrowserDownloadsPathOverride(ctx, config, nil)

	reportRelativePath := filepath.Join(evalReportFolder, EvaluationReportFileName)
	// Absolute filesystem path the agent puts in shell commands. Relative paths are
	// brittle here — the LLM's shell session cwd doesn't necessarily match the
	// workspace root, so a relative path can land anywhere or be denied by the
	// folder guard. Absolute paths just work.
	reportAbsolutePath := filepath.Join(GetPromptDocsRoot(), hcpo.GetWorkspacePath(), reportRelativePath)

	// Workspace tools the agent uses to write the report:
	//   - execute_shell_command  → for `cat > path` / python heredoc on a fresh file
	//   - diff_patch_workspace_file → for incremental fixes if needed
	// (No write_workspace_file here — prepareWorkspaceToolsOnly only exposes shell + diff_patch.)
	shellTools, shellExecutors := hcpo.prepareWorkspaceToolsOnly()

	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		0, 0, phase,
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowEvaluationScoringAgent(cfg, logger, tracer, eventBridge)
		},
		shellTools, shellExecutors, true,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create evaluation scoring agent: %w", err)
	}

	// Rebuild code execution registry so workspace tools appear in the tool index
	// when running in code-exec mode.
	if config.UseCodeExecutionMode {
		baseAgent := agent.GetBaseAgent()
		if baseAgent != nil {
			if mcpAgent := baseAgent.Agent(); mcpAgent != nil {
				if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update code execution registry for scoring agent: %v", err))
				}
			}
		}
	}

	// Set up user prompt with all steps
	scoringAgent, ok := agent.(*WorkflowEvaluationScoringAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast agent to WorkflowEvaluationScoringAgent")
	}

	// TARGET_RUN_PATH value (the absolute path to the workflow run being evaluated)
	// is exposed to the scoring agent as prompt context so it can reference original
	// artifacts directly. Same value the eval steps see via {{TARGET_RUN_PATH}}
	// substitution; empty if not set.
	targetRunPath := hcpo.variableValues["TARGET_RUN_PATH"]
	userPrompt := scoringAgent.GetUserPromptForAllSteps(stepInputs, reportAbsolutePath, targetRunPath)
	scoringAgent.SetUserMessageProcessor(func(map[string]string) string {
		return userPrompt
	})

	// In learn_code mode the agent should also (optionally) author a deterministic
	// main.py at learnings/__evaluation_scoring__/main.py that future runs use as
	// the fast path. Inject the contract into the system prompt with the absolute
	// path baked in (relative paths are unsafe in the agent's shell sessions).
	// Flip the mode marker so the AgentStarted event is tagged "Learn Code".
	if learnCodeMode {
		mainPyAbsPath := filepath.Join(GetPromptDocsRoot(), hcpo.GetWorkspacePath(), EvaluationScoringLearningsDir, EvaluationScoringMainPyName)
		scoringAgent.SetExtraSystemPromptSection(scoringLearnCodePromptSection(mainPyAbsPath))
		scoringAgent.SetLearnCodeMode(true)
	}

	// Execute with retry-on-validation-failure. The agent writes evaluation_report.json
	// directly via workspace tools; we validate after its turn loop ends, and if the
	// report fails pre-validation we send the errors back as a follow-up user message
	// on the same agent rather than starting from scratch — same pattern the regular
	// step retry loop uses in controller_execution.go.
	const maxScoringAttempts = 3
	var (
		results                 *WorkspaceVerificationResult
		valErr                  error
		scoringConversationHist []llmtypes.MessageContent
		scoringValidationPassed bool
	)

	for scoringAttempt := 1; scoringAttempt <= maxScoringAttempts; scoringAttempt++ {
		var execErr error
		if scoringAttempt == 1 {
			_, scoringConversationHist, execErr = scoringAgent.Execute(ctx, nil, nil)
		} else {
			// Continuation: feed prior validation errors back as a user message on the
			// existing agent, preserving system prompt + tool state + prior turns.
			feedbackMsg := buildScoringValidationContinuationMessage(results, scoringAttempt)
			hcpo.GetLogger().Info(fmt.Sprintf("🔁 Evaluation scoring attempt %d/%d: continuing existing agent with validation feedback (history=%d turns)",
				scoringAttempt, maxScoringAttempts, len(scoringConversationHist)))
			ba := scoringAgent.GetBaseAgent()
			if ba == nil {
				return nil, fmt.Errorf("scoring agent has no base agent for continuation on attempt %d", scoringAttempt)
			}
			_, scoringConversationHist, execErr = ba.Execute(ctx, feedbackMsg, scoringConversationHist, "", false)
		}
		if execErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Scoring agent execution returned error on attempt %d (will still try to validate produced report): %v", scoringAttempt, execErr))
		}

		// Post-execution validation: read the file the agent produced and run the
		// fixed schema against it. Same engine every step uses for pre-validation.
		results, valErr = RunPreValidation(ctx, validationSchema, evalReportFolder, hcpo.BaseOrchestrator)
		if valErr != nil {
			return nil, fmt.Errorf("scoring agent finished but pre-validation engine failed reading %s: %w", reportRelativePath, valErr)
		}
		if results != nil && results.OverallPass {
			scoringValidationPassed = true
			if scoringAttempt > 1 {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Evaluation scoring validation passed on attempt %d/%d", scoringAttempt, maxScoringAttempts))
			}
			break
		}
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Scoring validation failed on attempt %d/%d", scoringAttempt, maxScoringAttempts))
	}

	if !scoringValidationPassed {
		return nil, fmt.Errorf("scoring agent's evaluation_report.json failed pre-validation after %d attempts:\n%s", maxScoringAttempts, formatScoringValidationErrors(results))
	}

	reportContent, readErr := hcpo.ReadWorkspaceFile(ctx, reportRelativePath)
	if readErr != nil {
		return nil, fmt.Errorf("scoring report passed validation but couldn't be read back: %w", readErr)
	}
	parsed := &EvaluationReport{}
	if unmarshalErr := json.Unmarshal([]byte(reportContent), parsed); unmarshalErr != nil {
		return nil, fmt.Errorf("scoring report passed validation but failed to parse: %w", unmarshalErr)
	}

	// Enrich each step score with the runtime-owned max_score. step_title and
	// success_criteria are no longer copied here — the report is consumed alongside
	// evaluation_plan.json and consumers look up titles/descriptions by step_id.
	for _, s := range parsed.StepScores {
		if s == nil {
			continue
		}
		s.MaxScore = 10
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Evaluation scoring complete: %d step scores validated", len(parsed.StepScores)))
	return parsed, nil
}

// buildScoringValidationContinuationMessage formats a scoring pre-validation
// failure as a follow-up user message so the existing scoring agent can correct
// evaluation_report.json in-place rather than start a fresh scoring turn loop.
// Mirrors buildValidationContinuationUserMessage in controller_execution.go but
// sources its error body from formatScoringValidationErrors.
func buildScoringValidationContinuationMessage(results *WorkspaceVerificationResult, attempt int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Evaluation report validation failed (retry attempt %d)\n\n", attempt))
	sb.WriteString(formatScoringValidationErrors(results))
	sb.WriteString("\n\nFix the listed issues in `evaluation_report.json` and re-save. Preserve the step scores that already passed; only correct the failures above — do not restart scoring from scratch.")
	return sb.String()
}

// formatScoringValidationErrors turns a pre-validation result into a human-readable
// error list the LLM can act on. Mirrors the shape callers like the LLM see when
// regular steps fail pre-validation.
func formatScoringValidationErrors(results *WorkspaceVerificationResult) string {
	if results == nil {
		return "Validation result was nil."
	}
	if len(results.Summary.Errors) == 0 && len(results.Summary.SchemaWarnings) == 0 {
		return fmt.Sprintf("Validation failed (passed=%d, failed=%d) but no error details were produced.", results.Summary.PassedChecks, results.Summary.FailedChecks)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Pre-validation: %d/%d checks failed.\n", results.Summary.FailedChecks, results.Summary.TotalChecks))
	for i, e := range results.Summary.Errors {
		sb.WriteString(fmt.Sprintf("  %d. file=%s path=%s check=%s expected=%s actual=%s — %s\n", i+1, e.File, e.Path, e.CheckType, e.Expected, e.Actual, e.Message))
	}
	for i, w := range results.Summary.SchemaWarnings {
		sb.WriteString(fmt.Sprintf("  schema-warning %d: %s — %s\n", i+1, w.Path, w.Message))
	}
	return sb.String()
}

// Execute implements the Orchestrator interface
