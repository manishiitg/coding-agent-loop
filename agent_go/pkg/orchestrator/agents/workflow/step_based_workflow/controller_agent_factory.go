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

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	browserinstructions "mcp-agent-builder-go/agent_go/pkg/instructions"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	baseevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/mcpclient"
	"github.com/manishiitg/mcpagent/observability"

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

	// Check for Playwright and Camofox MCP servers
	hasPlaywright := false
	hasCamofox := false
	for _, server := range effectiveServers {
		if server == "playwright" {
			hasPlaywright = true
		}
		if server == "camofox" {
			hasCamofox = true
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

	if !hasPlaywright && !hasAgentBrowser && !hasCamofox {
		return // No browser tool, nothing to configure
	}

	// Track which browser tool triggered this for later use
	browserToolType := "agent-browser"
	if hasPlaywright {
		browserToolType = "playwright"
	} else if hasCamofox {
		browserToolType = "camofox"
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

	// Resolve to absolute path for Playwright MCP server configuration
	// CRITICAL: workspacePath is relative to workspace-docs root (e.g., "Workflow/ICICI...")
	// We MUST resolve it relative to workspace-docs root, NOT the current working directory
	// Using filepath.Abs() on a relative path resolves relative to CWD, which is wrong!
	var absDownloadsPath string
	workspaceDocsRoot := getWorkspaceDocsRoot()
	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] getWorkspaceDocsRoot() returned: %s", workspaceDocsRoot))
	if workspaceDocsRoot == "" {
		// Fallback to old behavior if we can't determine workspace-docs root
		// This should NOT happen - log error and try to calculate it
		cwd, _ := os.Getwd()
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ getWorkspaceDocsRoot() returned empty! CWD: %s, workspacePath: %s", cwd, workspacePath))

		// Try one more time with explicit calculation
		calculatedRoot := filepath.Join(cwd, "..", "workspace-docs")
		if absCalculated, err := filepath.Abs(calculatedRoot); err == nil {
			workspaceDocsRoot = absCalculated
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Calculated workspace-docs root: %s", workspaceDocsRoot))
		}
	}

	if workspaceDocsRoot != "" {
		// CORRECT: Resolve relative to workspace-docs root
		// workspacePath is already relative to workspace-docs (e.g., "Workflow/ICICI...")
		// downloadsRelativePath is relative to workspacePath (e.g., "runs/.../execution/Downloads")
		// Final path: workspace-docs-root + workspacePath + downloadsRelativePath
		absDownloadsPath = filepath.Join(workspaceDocsRoot, workspacePath, downloadsRelativePath)
		absDownloadsPath = filepath.Clean(absDownloadsPath)
		hcpo.GetLogger().Info(fmt.Sprintf("✅ Resolved Downloads path relative to workspace-docs root: %s", absDownloadsPath))
	} else {
		// Last resort fallback - this should be avoided as it will resolve relative to CWD (WRONG!)
		// This path leads to agent_go/Downloads which is incorrect
		fullDownloadsPath := filepath.Join(workspacePath, downloadsRelativePath)
		var absErr error
		absDownloadsPath, absErr = filepath.Abs(fullDownloadsPath)
		if absErr != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to resolve absolute path for downloads %s: %v, using relative path", fullDownloadsPath, absErr))
			absDownloadsPath = fullDownloadsPath
		} else {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ CRITICAL: Could not determine workspace-docs root, using CWD-relative path (THIS IS WRONG): %s", absDownloadsPath))
		}
	}

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

// setupExecutionFolderGuard sets up folder guard paths for execution agents
// Returns readPaths and writePaths for folder guard configuration
// stepID: Step ID for step-specific learnings folder access (e.g., "step-3" or branch step ID)
// hasLearnings: If true, includes learnings folder in read paths; if false, excludes it
func (hcpo *StepBasedWorkflowOrchestrator) setupExecutionFolderGuard(stepPath string, stepID string, hasLearnings bool, useKnowledgebaseOverride ...bool) (readPaths, writePaths []string) {
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
	// READ: step-specific learnings folder (only if learnings exist) + execution folder (to read previous step results) + knowledgebase folder (if enabled)
	// WRITE: only the specific step folder (execution/step-{X}/ or execution/step-{X}-{branch}/) + knowledgebase folder (if enabled) + execution/Downloads folder to prevent writing to other steps
	// Use getExecutionFolderPath to support both regular and branch steps
	stepFolderPath := getExecutionFolderPath(executionWorkspacePath, stepPath)
	downloadsPath := fmt.Sprintf("%s/Downloads", executionWorkspacePath)
	readPaths = []string{executionWorkspacePath}
	// Only add learnings folder to read paths if learnings exist
	if hasLearnings {
		// Step-specific learnings folder: learnings/{stepID}/ (only this step's learnings, not full learnings folder)
		// In evaluation mode, learnings are stored in evaluation/learnings/
		var stepLearningsPath string
		if hcpo.isEvaluationMode {
			stepLearningsPath = fmt.Sprintf("%s/evaluation/learnings/%s", baseWorkspacePath, stepID)
		} else {
			stepLearningsPath = fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepID)
		}
		readPaths = append([]string{stepLearningsPath}, readPaths...)
	}
	writePaths = []string{stepFolderPath, downloadsPath}

	// Add knowledgebase folder paths only if enabled
	// Use per-step override if provided, otherwise fall back to orchestrator-level setting
	kbEnabled := hcpo.UseKnowledgebase()
	if len(useKnowledgebaseOverride) > 0 {
		kbEnabled = useKnowledgebaseOverride[0]
	}
	if kbEnabled {
		knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)
		readPaths = append(readPaths, knowledgebasePath)
		writePaths = append(writePaths, knowledgebasePath)
	}

	// Check if TARGET_RUN_PATH variable is set (used for evaluation) and add to read paths
	// This allows evaluation agents to read the artifacts of the run they are evaluating
	if targetRunPath, ok := hcpo.variableValues["TARGET_RUN_PATH"]; ok && targetRunPath != "" {
		readPaths = append(readPaths, targetRunPath)
		hcpo.GetLogger().Info(fmt.Sprintf("🔓 Added TARGET_RUN_PATH to read paths for evaluation: %s", targetRunPath))
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

// getToolSearchMode determines tool search mode with priority: step config > preset default
func (hcpo *StepBasedWorkflowOrchestrator) getToolSearchMode(stepConfig *AgentConfigs) bool {
	var isToolSearchMode bool
	if stepConfig != nil && stepConfig.UseToolSearchMode != nil {
		isToolSearchMode = *stepConfig.UseToolSearchMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific tool search mode: %v", isToolSearchMode))
	} else {
		isToolSearchMode = hcpo.GetUseToolSearchMode()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset tool search mode: %v", isToolSearchMode))
	}
	return isToolSearchMode
}

// getPreDiscoveredTools determines pre-discovered tools with priority: step config > preset default
func (hcpo *StepBasedWorkflowOrchestrator) getPreDiscoveredTools(stepConfig *AgentConfigs) []string {
	if stepConfig != nil && len(stepConfig.PreDiscoveredTools) > 0 {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific pre-discovered tools: %v", stepConfig.PreDiscoveredTools))
		return stepConfig.PreDiscoveredTools
	}
	preDiscoveredTools := hcpo.GetPreDiscoveredTools()
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset pre-discovered tools: %v", preDiscoveredTools))
	return preDiscoveredTools
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
// Priority for sub-agents (sub_agent_llm set in context):
//  1. sub_agent_llm from context  — skipped if enable_dynamic_tier_selection=true
//  2. (falls through to main step chain below)
//
// Priority for main step execution:
//  1. tempLLM2 / tempLLM1        — highest; requires learnings exist and disable_temp_llm=false
//  2. step config ExecutionLLM   — explicit per-step override; used when tempLLM is off/unavailable
//  3. tiered mode                — maturity-based tier resolution (preferred_tier ctx > maturity)
//  4. orchestrator main LLM      — final fallback
func (hcpo *StepBasedWorkflowOrchestrator) selectExecutionLLM(
	ctx context.Context,
	stepConfig *AgentConfigs,
	isRetryAfterValidationFailure bool,
	retryAttempt int,
	stepID string,
	stepPath string,
	learningsFolderEmpty bool,
) *orchestrator.LLMConfig {
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	// Guard against nil — scheduler-triggered sessions may not have an orchestrator LLM set.
	if orchestratorLLMConfig == nil {
		orchestratorLLMConfig = &orchestrator.LLMConfig{}
	}

	// ── 1. SUB-AGENT OVERRIDE ────────────────────────────────────────────────
	// When the todo-task orchestrator spawns a child agent, sub_agent_llm is
	// injected into context. Use it directly unless enable_dynamic_tier_selection
	// is set (which lets the orchestrator pick tiers dynamically instead).
	dynamicTierEnabled := stepConfig != nil && stepConfig.EnableDynamicTierSelection != nil && *stepConfig.EnableDynamicTierSelection
	if subAgentLLM, ok := ctx.Value(virtualtools.SubAgentLLMContextKey).(*AgentLLMConfig); ok &&
		subAgentLLM != nil && subAgentLLM.Provider != "" && subAgentLLM.ModelID != "" {
		if dynamicTierEnabled {
			hcpo.GetLogger().Info(fmt.Sprintf("⏭️ [SKIPPED] sub_agent_llm (%s/%s) skipped for step %s because enable_dynamic_tier_selection is true",
				subAgentLLM.Provider, subAgentLLM.ModelID, stepPath))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 [SUB-AGENT] Using sub_agent_llm override for step %s: %s/%s",
				stepPath, subAgentLLM.Provider, subAgentLLM.ModelID))
			return &orchestrator.LLMConfig{
				Primary: orchestrator.LLMModel{
					Provider: subAgentLLM.Provider,
					ModelID:  subAgentLLM.ModelID,
				},
				APIKeys: hcpo.GetAPIKeys(),
			}
		}
	}

	// ── 2. TEMP LLM ──────────────────────────────────────────────────────────
	// tempLLM takes highest priority for main-step execution.
	// Skipped entirely when: disable_temp_llm=true, learnings folder is empty,
	// or fallback_to_original_llm_on_failure is triggered on tempLLM1.
	disableTempLLM := stepConfig != nil && stepConfig.DisableTempLLM != nil && *stepConfig.DisableTempLLM
	shouldSkipTempOverride := isRetryAfterValidationFailure && hcpo.fallbackToOriginalLLMOnFailure
	hasTempLLM1 := hcpo.tempOverrideLLM != nil && hcpo.tempOverrideLLM.Provider != "" && hcpo.tempOverrideLLM.ModelID != ""
	hasTempLLM2 := hcpo.tempOverrideLLM2 != nil && hcpo.tempOverrideLLM2.Provider != "" && hcpo.tempOverrideLLM2.ModelID != ""

	if !disableTempLLM {
		if shouldSkipTempOverride && (hasTempLLM1 || hasTempLLM2) {
			hcpo.GetLogger().Info("🔄 Validation failed - skipping temp override LLM and falling back (fallback_to_original_llm_on_failure enabled)")
		}

		if learningsFolderEmpty && (hasTempLLM1 || hasTempLLM2) {
			hcpo.GetLogger().Info(fmt.Sprintf("📚 Step %s has no learnings - skipping temp override LLM (learnings folder is empty)", stepPath))

			// Emit event so UI shows why tempLLM was skipped
			eventBridge := hcpo.GetContextAwareBridge()
			if eventBridge != nil {
				stepId := stepID
				if stepId == "" {
					stepId = stepPath
				}
				var tempLLMProvider, tempLLMModel string
				if retryAttempt == 1 && hasTempLLM1 {
					tempLLMProvider = hcpo.tempOverrideLLM.Provider
					tempLLMModel = hcpo.tempOverrideLLM.ModelID
				} else if retryAttempt == 2 && hasTempLLM2 {
					tempLLMProvider = hcpo.tempOverrideLLM2.Provider
					tempLLMModel = hcpo.tempOverrideLLM2.ModelID
				} else if hasTempLLM1 {
					tempLLMProvider = hcpo.tempOverrideLLM.Provider
					tempLLMModel = hcpo.tempOverrideLLM.ModelID
				}
				baseWorkspacePath := hcpo.GetWorkspacePath()
				pathInfo := parseStepPath(stepPath)
				eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
					Type:      orchestrator_events.TempLLMSkipped,
					Timestamp: time.Now(),
					Data: &orchestrator_events.TempLLMSkippedEvent{
						BaseEventData: baseevents.BaseEventData{
							Timestamp: time.Now(),
							Component: "orchestrator",
						},
						StepID:          stepId,
						StepIndex:       pathInfo.ParentStepNumber - 1,
						StepPath:        stepPath,
						IsBranchStep:    pathInfo.IsBranchStep,
						Reason:          "learnings_folder_empty",
						TempLLMProvider: tempLLMProvider,
						TempLLMModel:    tempLLMModel,
						LearningsPath:   fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepPath),
						RunFolder:       hcpo.selectedRunFolder,
						WorkspacePath:   baseWorkspacePath,
					},
				})
				hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted temp_llm_skipped event for %s (skipped tempLLM: %s/%s)", stepPath, tempLLMProvider, tempLLMModel))
			}
		}

		// tempLLM2: attempt 2, or new loop iteration after validation failure
		shouldUseTempLLM2 := !learningsFolderEmpty && hasTempLLM2 && (retryAttempt == 2 || (isRetryAfterValidationFailure && retryAttempt == 1))
		if shouldUseTempLLM2 {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using TEMPORARY OVERRIDE LLM 2 (attempt %d, learnings exist): %s/%s",
				retryAttempt, hcpo.tempOverrideLLM2.Provider, hcpo.tempOverrideLLM2.ModelID))
			return &orchestrator.LLMConfig{
				Primary: orchestrator.LLMModel{
					Provider: hcpo.tempOverrideLLM2.Provider,
					ModelID:  hcpo.tempOverrideLLM2.ModelID,
				},
				APIKeys: orchestratorLLMConfig.APIKeys,
			}
		}

		// tempLLM1: attempt 1 (not blocked by shouldSkipTempOverride)
		if !shouldSkipTempOverride && !learningsFolderEmpty && retryAttempt == 1 && hasTempLLM1 {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using TEMPORARY OVERRIDE LLM 1 (attempt %d, learnings exist): %s/%s",
				retryAttempt, hcpo.tempOverrideLLM.Provider, hcpo.tempOverrideLLM.ModelID))
			return &orchestrator.LLMConfig{
				Primary: orchestrator.LLMModel{
					Provider: hcpo.tempOverrideLLM.Provider,
					ModelID:  hcpo.tempOverrideLLM.ModelID,
				},
				APIKeys: orchestratorLLMConfig.APIKeys,
			}
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step %s has disable_temp_llm=true - skipping tempLLM", stepPath))
	}

	// ── 3. STEP CONFIG ExecutionLLM ──────────────────────────────────────────
	// Explicit per-step model override — beats tiered mode when tempLLM is
	// off (disabled, no learnings, or all attempts exhausted).
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

	// ── 4. TIERED MODE ───────────────────────────────────────────────────────
	// Maturity-based tier resolution when no explicit step override is set.
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
		learningMode := ""
		if stepConfig != nil {
			learningMode = stepConfig.LearningMode
		}
		maturity := hcpo.getLearningMaturity(ctx, stepID, stepPath, learningMode)
		llmConfig, tier := hcpo.tierResolver.ResolveForExecution(maturity)
		if llmConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Execution agent for step %s using Tier %d (%s): %s/%s (maturity: %d)",
				stepPath, int(tier), TierLevelLabel(tier), llmConfig.Primary.Provider, llmConfig.Primary.ModelID, int(maturity)))
		}
		return llmConfig
	}

	// ── 5. ORCHESTRATOR MAIN LLM ─────────────────────────────────────────────
	if orchestratorLLMConfig.Primary.Provider != "" && orchestratorLLMConfig.Primary.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using main workflow LLM as final fallback: %s/%s",
			orchestratorLLMConfig.Primary.Provider, orchestratorLLMConfig.Primary.ModelID))
		return orchestratorLLMConfig
	}

	err := fmt.Errorf("no valid LLM configuration found for execution agent (step %s): tempLLM, step config, tiered mode, preset, and workflow LLM are all empty/unavailable", stepPath)
	hcpo.GetLogger().Error("❌ "+err.Error(), err)
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

	// Determine execution mode using 3-rule priority:
	// Rule 1: claude-code/gemini-cli providers ALWAYS use code execution mode
	// Rule 2: Step-specific config (if explicitly set)
	// Rule 3: Workflow/preset default
	actualProvider := config.LLMConfig.Primary.Provider
	if actualProvider == "claude-code" || actualProvider == "gemini-cli" {
		// Rule 1: CLI providers always use code execution mode
		config.UseCodeExecutionMode = true
		config.UseToolSearchMode = false
		config.PreDiscoveredTools = hcpo.getPreDiscoveredTools(stepConfig)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode forced for CLI provider '%s' - MCP tools accessed via HTTP bridge", actualProvider))
	} else if stepConfig != nil && stepConfig.UseCodeExecutionMode != nil {
		// Rule 2: Step explicitly set code execution mode
		config.UseCodeExecutionMode = *stepConfig.UseCodeExecutionMode
		// If code execution is enabled and tool search is NOT explicitly set on the step,
		// default tool search to false — they are mutually exclusive, don't inherit preset default
		if config.UseCodeExecutionMode && stepConfig.UseToolSearchMode == nil {
			config.UseToolSearchMode = false
		} else {
			config.UseToolSearchMode = hcpo.getToolSearchMode(stepConfig)
		}
		config.PreDiscoveredTools = hcpo.getPreDiscoveredTools(stepConfig)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode: %v, tool search mode: %v", config.UseCodeExecutionMode, config.UseToolSearchMode))
	} else {
		// Rule 3: Workflow/preset default — but code execution auto-enable from server.go
		// should NOT apply to non-CLI providers. Only use the preset value if it was
		// explicitly set by the user (not auto-enabled for claude-code at workflow level).
		// For non-CLI providers, code execution mode is false unless step explicitly enables it.
		config.UseCodeExecutionMode = false
		isToolSearchMode := hcpo.getToolSearchMode(stepConfig)
		config.UseToolSearchMode = isToolSearchMode
		config.PreDiscoveredTools = hcpo.getPreDiscoveredTools(stepConfig)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Provider '%s': code execution mode disabled (not CLI provider), tool search mode: %v", actualProvider, config.UseToolSearchMode))
	}

	// Set EnableContextOffloading if specified
	if stepConfig != nil && stepConfig.EnableContextOffloading != nil {
		config.EnableContextOffloading = stepConfig.EnableContextOffloading
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific context offloading setting: %v", *stepConfig.EnableContextOffloading))
	}
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
		// Auto-include image gen tools if workspace_image_gen exists in the workspace tools pool
		for _, tool := range hcpo.WorkspaceTools {
			if tool.Function != nil && tool.Function.Name == "workspace_image_gen" {
				defaultEnabledTools = append(defaultEnabledTools, "workspace_image_gen:*")
				defaultEnabledTools = append(defaultEnabledTools, "workspace_image_edit:*")
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
// stepPath: Step path identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// stepID: Step ID for step-specific learnings folder access (e.g., "step-3" or branch step ID)
// hasLearnings: If true, includes learnings folder in read paths; if false, excludes it
func (hcpo *StepBasedWorkflowOrchestrator) setupConditionalFolderGuard(stepPath string, stepID string, hasLearnings bool) (readPaths, writePaths []string) {
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
	stepFolderPath := getExecutionFolderPath(executionWorkspacePath, stepPath)

	// Set folder guard paths:
	// READ: step-specific learnings folder (only if learnings exist) + entire execution folder (to read all previous step results and verify conditions) + knowledgebase folder (if enabled)
	// WRITE: step-specific execution folder (to write evaluation results and intermediate files) + knowledgebase folder (if enabled)
	readPaths = []string{executionWorkspacePath}
	// Only add learnings folder to read paths if learnings exist
	if hasLearnings {
		// Step-specific learnings folder: learnings/{stepID}/ (only this step's learnings, not full learnings folder)
		// In evaluation mode, learnings are stored in evaluation/learnings/
		var stepLearningsPath string
		if hcpo.isEvaluationMode {
			stepLearningsPath = fmt.Sprintf("%s/evaluation/learnings/%s", baseWorkspacePath, stepID)
		} else {
			stepLearningsPath = fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepID)
		}
		readPaths = append([]string{stepLearningsPath}, readPaths...)
	}
	writePaths = []string{stepFolderPath}

	// Add knowledgebase folder paths only if enabled
	if hcpo.UseKnowledgebase() {
		knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)
		readPaths = append(readPaths, knowledgebasePath)
		writePaths = append(writePaths, knowledgebasePath)
	}
	return readPaths, writePaths
}

// setupLearningFolderGuard sets up folder guard paths for learning agents
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// stepPath: Step path identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps) - used for execution logs folder
func (hcpo *StepBasedWorkflowOrchestrator) setupLearningFolderGuard(learningPathIdentifier string, stepPath string) (readPaths, writePaths []string) {
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
	// Supports both regular steps (step-{X}) and branch steps (step-{X}-{true/false}-{Y})
	// In evaluation mode, learnings are stored in evaluation/learnings/
	var learningsPath string
	var baseLearningsPath string
	if hcpo.isEvaluationMode {
		learningsPath = fmt.Sprintf("%s/evaluation/learnings/%s", baseWorkspacePath, learningPathIdentifier)
		baseLearningsPath = fmt.Sprintf("%s/evaluation/learnings", baseWorkspacePath)
	} else {
		learningsPath = fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, learningPathIdentifier)
		baseLearningsPath = fmt.Sprintf("%s/learnings", baseWorkspacePath)
	}

	// Build read paths: execution path + base learnings path + execution logs folder
	readPaths = []string{executionPath}
	// Add base learnings path for reading existing learnings (we read from base but write to step folder)
	readPaths = append(readPaths, baseLearningsPath)

	// Add execution logs folder so learning agents can read execution logs if needed
	// Execution logs contain actual tool usage, conversation history, and execution results
	executionLogsPath := getExecutionFolderPathForLogs(validationWorkspacePath, stepPath)
	readPaths = append(readPaths, executionLogsPath)

	writePaths = []string{learningsPath}
	return readPaths, writePaths
}

// getLearningMaxTurns determines max turns for learning agents
// Fixed to 50 (not configurable)
func (hcpo *StepBasedWorkflowOrchestrator) getLearningMaxTurns(stepConfig *AgentConfigs) int {
	return 50
}

// selectLearningLLM selects the LLM config for learning agents
//
// Priority:
//  1. tempLearningLLM            — if learnings already exist for this step
//  2. cost-optimization tempLLM  — if stability threshold reached (>50% of runs stable)
//  3. step config LearningLLM    — explicit per-step override; beats tiered mode
//  4. tiered mode                — maturity-based tier resolution
//  5. presetLearningLLM          — workflow-level default
func (hcpo *StepBasedWorkflowOrchestrator) selectLearningLLM(ctx context.Context, stepConfig *AgentConfigs, stepID string, stepPath string) *orchestrator.LLMConfig {
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if orchestratorLLMConfig == nil {
		orchestratorLLMConfig = &orchestrator.LLMConfig{}
	}

	// ── 1. TEMP LEARNING LLM ─────────────────────────────────────────────────
	// If tempLearningLLM is configured, always use it for all learning phases (first run or incremental).
	if hcpo.executionOptions != nil && hcpo.executionOptions.TempLearningLLM != nil &&
		hcpo.executionOptions.TempLearningLLM.Provider != "" && hcpo.executionOptions.TempLearningLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🧠 [TEMP_LEARNING_LLM] Using temp learning LLM for step %s: %s/%s",
			stepID, hcpo.executionOptions.TempLearningLLM.Provider, hcpo.executionOptions.TempLearningLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.executionOptions.TempLearningLLM.Provider,
				ModelID:  hcpo.executionOptions.TempLearningLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys,
		}
	}

	// ── 2. COST-OPTIMIZATION TEMP LLM ────────────────────────────────────────
	// Switch to cheaper tempLLM once a step reaches 50% of its stability threshold.
	// TODO: Turn-based classification is unreliable across models — needs a better metric.
	if stepID != "" {
		metadata, err := hcpo.readStepLearningMetadata(ctx, stepID, stepPath)
		if err == nil {
			shouldUseTempLLM := false
			reason := ""
			if metadata.LastTurnCount < 100 {
				if metadata.SuccessfulRunsSimple >= 2 {
					shouldUseTempLLM = true
					reason = fmt.Sprintf("stability threshold reached 50%% (Simple: %d/3)", metadata.SuccessfulRunsSimple)
				}
			} else if metadata.LastTurnCount <= 200 {
				if metadata.SuccessfulRunsMedium >= 3 {
					shouldUseTempLLM = true
					reason = fmt.Sprintf("stability threshold reached 50%% (Medium: %d/5)", metadata.SuccessfulRunsMedium)
				}
			} else {
				if metadata.SuccessfulRunsComplex >= 5 {
					shouldUseTempLLM = true
					reason = fmt.Sprintf("stability threshold reached 50%% (Complex: %d/10)", metadata.SuccessfulRunsComplex)
				}
			}
			if shouldUseTempLLM && hcpo.tempOverrideLLM != nil && hcpo.tempOverrideLLM.Provider != "" && hcpo.tempOverrideLLM.ModelID != "" {
				hcpo.GetLogger().Info(fmt.Sprintf("💰 [COST_OPTIMIZATION] Learning agent using cheaper tempLLM (%s/%s): %s",
					hcpo.tempOverrideLLM.Provider, hcpo.tempOverrideLLM.ModelID, reason))
				return &orchestrator.LLMConfig{
					Primary: orchestrator.LLMModel{
						Provider: hcpo.tempOverrideLLM.Provider,
						ModelID:  hcpo.tempOverrideLLM.ModelID,
					},
					APIKeys: orchestratorLLMConfig.APIKeys,
				}
			}
		}
	}

	// ── 3. STEP CONFIG LearningLLM ───────────────────────────────────────────
	// Explicit per-step override beats tiered mode when tempLLM is unavailable.
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 [STEP OVERRIDE] Using step LearningLLM for step %s: %s/%s",
			stepPath, stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: stepConfig.LearningLLM.Provider,
				ModelID:  stepConfig.LearningLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys,
		}
	}

	// ── 4. TIERED MODE ───────────────────────────────────────────────────────
	if hcpo.tierResolver != nil {
		learningMode := ""
		if stepConfig != nil {
			learningMode = stepConfig.LearningMode
		}
		maturity := hcpo.getLearningMaturity(ctx, stepID, stepPath, learningMode)
		llmConfig, tier := hcpo.tierResolver.ResolveForLearning(maturity)
		if llmConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Learning agent for step %s using Tier %d (%s): %s/%s (maturity: %d)",
				stepPath, int(tier), TierLevelLabel(tier), llmConfig.Primary.Provider, llmConfig.Primary.ModelID, int(maturity)))
		}
		return llmConfig
	}

	// ── 5. PRESET ────────────────────────────────────────────────────────────
	if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default learning LLM: %s/%s",
			hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetLearningLLM.Provider,
				ModelID:  hcpo.presetLearningLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys,
		}
	}

	err := fmt.Errorf("no valid LLM configuration found for learning agent (step %s): tempLLM, step config, tiered mode, and preset are all empty/unavailable", stepPath)
	hcpo.GetLogger().Error("❌ "+err.Error(), err)
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

// createExecutionOnlyAgent creates an execution-only agent that receives pre-discovered learning history
// isRetryAfterValidationFailure: if true and fallbackToOriginalLLMOnFailure is enabled, will skip tempOverrideLLM and use original LLM
// stepPath: Step path identifier (e.g., "step-1" for regular steps, "step-3-if-true-0" for branch steps, "step-2-sub-agent-1" for sub-agents)
// retryAttempt: current retry attempt number (1 = first attempt, 2 = second attempt, etc.) - used for cascading LLM fallback
// stepIDOverride: Optional explicit step ID to use for learnings / tempLLM selection (e.g., sub-agent step ID).
//
//	When empty, the step ID will be derived from stepPath.
func (hcpo *StepBasedWorkflowOrchestrator) createExecutionOnlyAgent(ctx context.Context, phase string, stepPath string, agentName string, stepConfig *AgentConfigs, isRetryAfterValidationFailure bool, retryAttempt int, stepIDOverride string) (agents.OrchestratorAgent, error) {
	// 1. Resolve stepID first (needed for folder guard setup)
	stepID := hcpo.resolveStepID(stepPath, stepIDOverride)

	// 2. Check if learnings exist for this step (needed for folder guard setup)
	hasLearnings := false
	if ctx != nil {
		pathInfo := parseStepPath(stepPath)
		stepIndexForLearnings := pathInfo.ParentStepNumber - 1
		learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepID, stepIndexForLearnings, stepPath)
		if err == nil {
			hasLearnings = !learningsEmpty
		}
	}

	// 3. Setup folder guard (extracted method) - uses step-specific learnings folder only if learnings exist
	readPaths, writePaths := hcpo.setupExecutionFolderGuard(stepPath, stepID, hasLearnings)

	// Add skill folder paths to read paths (skills are read-only)
	effectiveSkills := GetEffectiveSkills(stepConfig, hcpo.BaseOrchestrator)
	if len(effectiveSkills) > 0 {
		skillReadPaths, _ := BuildSkillFolderGuardPaths(effectiveSkills)
		readPaths = append(readPaths, skillReadPaths...)
		hcpo.GetLogger().Info(fmt.Sprintf("🎯 Added skill folder paths to folder guard: %v", skillReadPaths))
	}

	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	if hasLearnings {
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for execution-only agent - Read paths: %v, Write paths: %v (can read learnings/%s/ and execution/, can write to %s and execution/Downloads/)", readPaths, writePaths, stepID, stepPath))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for execution-only agent - Read paths: %v, Write paths: %v (no learnings folder, can read execution/, can write to %s and execution/Downloads/)", readPaths, writePaths, stepPath))
	}

	// 3. Determine settings (extracted methods)
	isCodeExecutionMode := hcpo.getCodeExecutionMode(stepConfig)
	maxTurns := hcpo.getExecutionMaxTurns(stepConfig)

	// 4. Select LLM (extracted method)
	pathInfo := parseStepPath(stepPath)
	stepIndexForCheck := pathInfo.ParentStepNumber - 1 // Convert to 0-based
	learningsFolderEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepID, stepIndexForCheck, stepPath)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check if step %s learnings folder is empty: %v, assuming empty (will skip tempLLM)", stepID, err))
		learningsFolderEmpty = true // Conservative: assume empty on error, skip tempLLM
	}
	llmConfig := hcpo.selectExecutionLLM(ctx, stepConfig, isRetryAfterValidationFailure, retryAttempt, stepID, stepPath, learningsFolderEmpty)
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for execution agent: temp override, step config, and preset execution LLM are all empty or invalid")
	}

	// 4. Create config
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

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

	// 6. Use base factory! (This handles all setup automatically)
	pathInfo = parseStepPath(stepPath)
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

	// Add skill prompt if skills are selected (uses effectiveSkills from folder guard setup above)
	if len(effectiveSkills) > 0 {
		skillPrompt := BuildWorkflowSkillPrompt(ctx, effectiveSkills, hcpo.BaseOrchestrator)
		if skillPrompt != "" {
			mcpAgent.AppendSystemPrompt(skillPrompt)
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Added skill prompt to execution agent (%d skills): %v", len(effectiveSkills), effectiveSkills))
		}
	}

	// Browser isolation: for agent-browser skill, inject system prompt with unique session name
	if isolatedSessionID, ok := ctx.Value(virtualtools.SubAgentIsolatedSessionIDKey).(string); ok && isolatedSessionID != "" {
		for _, skill := range effectiveSkills {
			if skill == "agent-browser" {
				mcpAgent.AppendSystemPrompt(fmt.Sprintf(
					"## Browser Isolation\nYou have an isolated browser session. When using the agent_browser tool, use session name %q instead of \"default\" to avoid sharing browser state with other agents.",
					isolatedSessionID,
				))
				hcpo.GetLogger().Info("Added browser isolation guidance to sub-agent system prompt for agent-browser")
				break
			}
		}
	}

	// Add secrets to execution agent's system prompt
	effectiveSecrets := GetEffectiveSecrets(hcpo.BaseOrchestrator)
	if len(effectiveSecrets) > 0 {
		secretPrompt := BuildWorkflowSecretPrompt(effectiveSecrets)
		if secretPrompt != "" {
			mcpAgent.AppendSystemPrompt(secretPrompt)
			hcpo.GetLogger().Info(fmt.Sprintf("🔐 Added secret prompt to execution agent (%d secrets)", len(effectiveSecrets)))
		}
	}

	// Add browser instructions if browser tools are available
	hasPlaywrightServer := false
	hasCamofoxServer := false
	hasAgentBrowserSkill := false
	for _, s := range config.ServerNames {
		if s == "playwright" {
			hasPlaywrightServer = true
		}
		if s == "camofox" {
			hasCamofoxServer = true
		}
	}
	for _, skill := range effectiveSkills {
		if skill == "agent-browser" {
			hasAgentBrowserSkill = true
		}
	}
	if hasPlaywrightServer || hasCamofoxServer || hasAgentBrowserSkill {
		mcpAgent.AppendSystemPrompt(browserinstructions.GetBrowserUploadInstructions())
		hcpo.GetLogger().Info(fmt.Sprintf("🌐 Added browser upload instructions to execution agent (playwright=%v, camofox=%v, agent-browser=%v)", hasPlaywrightServer, hasCamofoxServer, hasAgentBrowserSkill))
		// Add CDP/headless mode-specific instructions
		if hcpo.GetCdpPort() > 0 {
			mcpAgent.AppendSystemPrompt(browserinstructions.GetCdpModeInstructions())
			hcpo.GetLogger().Info(fmt.Sprintf("🌐 Added CDP mode instructions to execution agent (port=%d)", hcpo.GetCdpPort()))
		} else {
			mcpAgent.AppendSystemPrompt(browserinstructions.GetHeadlessModeInstructions())
			hcpo.GetLogger().Info("🌐 Added headless mode instructions to execution agent")
		}
	}
	if hasCamofoxServer {
		mcpAgent.AppendSystemPrompt(browserinstructions.GetCamofoxInstructions())
		hcpo.GetLogger().Info("🦊 Added camofox-specific instructions to execution agent")
	}

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
// stepIndex: 0-based step index for token tracking (should be passed from runSuccessLearningPhase/runFailureLearningPhase)
func (hcpo *StepBasedWorkflowOrchestrator) createLearningAgentInternal(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, isCodeExecutionMode bool, stepID string, stepPath string, stepIndex int) (agents.OrchestratorAgent, error) {
	// 1. Setup folder guard (extracted method)
	// Pass stepPath to include execution logs folder in read paths
	readPaths, writePaths := hcpo.setupLearningFolderGuard(learningPathIdentifier, stepPath)
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

	// Code execution mode and tool search mode only apply to execution agents, not learning agents
	// CRITICAL: Override orchestrator-level code execution mode and tool search mode setting - learning agents are pure LLM analysis agents
	// Use the agent's ACTUAL provider (from its LLM config), not the phase LLM provider.
	// The phase LLM may be claude-code but the learning agent uses a different provider (e.g., MiniMax).
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(&AgentLLMConfig{
		Provider: config.LLMConfig.Primary.Provider,
		ModelID:  config.LLMConfig.Primary.ModelID,
	})
	config.UseToolSearchMode = false
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
	// Use stepIndex directly (passed from runSuccessLearningPhase/runFailureLearningPhase) instead of parsing from stepPath
	// This ensures learning costs are correctly attributed to the step, even if stepPath parsing fails
	stepNumberForContext := stepIndex // stepIndex is already 0-based

	// Create agent factory function based on code execution mode
	var createAgentFunc func(*agents.OrchestratorAgentConfig, loggerv2.Logger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent
	if wasCodeExecutionMode {
		createAgentFunc = func(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowCodeExecutionLearningAgent(config, logger, tracer, eventBridge)
		}
	} else {
		createAgentFunc = func(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowLearningAgent(config, logger, tracer, eventBridge)
		}
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

// createFailureLearningAgent is a backward compatibility wrapper for createLearningAgent
// Deprecated: Use createLearningAgent instead. The unified learning agent handles both success and failure cases.
// stepIndex: 0-based step index for token tracking
func (hcpo *StepBasedWorkflowOrchestrator) createFailureLearningAgent(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, isCodeExecutionMode bool, stepID string, stepPath string, stepIndex int) (agents.OrchestratorAgent, error) {
	return hcpo.createLearningAgentInternal(ctx, phase, learningPathIdentifier, agentName, stepConfig, isCodeExecutionMode, stepID, stepPath, stepIndex)
}

// createConditionalAgent creates a conditional agent using the standard factory pattern
// This ensures proper event bridge connection, context setup, and tool registration
// stepPath: Step path identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// stepID: Step ID for step-specific learnings folder access (e.g., "step-3" or branch step ID)
func (hcpo *StepBasedWorkflowOrchestrator) createConditionalAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs, conditionalLLMConfig *orchestrator.LLMConfig, stepPath string, stepID string) (agents.OrchestratorAgent, error) {
	// 1. Check if learnings exist for this step (needed for folder guard setup)
	hasLearnings := false
	if ctx != nil {
		learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepID, step, stepPath)
		if err == nil {
			hasLearnings = !learningsEmpty
		}
	}

	// 2. Setup folder guard (similar to execution agent) - uses step-specific learnings folder only if learnings exist
	readPaths, writePaths := hcpo.setupConditionalFolderGuard(stepPath, stepID, hasLearnings)
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	if hasLearnings {
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for conditional agent - Read paths: %v, Write paths: %v (can read learnings/%s/ and execution/, can write to %s)", readPaths, writePaths, stepID, stepPath))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for conditional agent - Read paths: %v, Write paths: %v (no learnings folder, can read execution/, can write to %s)", readPaths, writePaths, stepPath))
	}

	// Determine max turns: use orchestrator default (conditional agents don't have step-specific max turns config)
	maxTurns := hcpo.GetMaxTurns()
	// Note: ConditionalMaxTurns doesn't exist in AgentConfigs - using orchestrator default

	// Use the LLM config passed from caller (which handles ConditionalLLM > ExecutionLLM > preset > orchestrator priority)
	// Note: conditionalLLMConfig is selected by the caller with proper priority including ConditionalLLM
	llmConfig := conditionalLLMConfig
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for conditional agent: step config and preset execution LLM are both empty or invalid")
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
	if conditionalProvider == "claude-code" || conditionalProvider == "gemini-cli" {
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
	Index         int                 `json:"index"`   // 1-based call order
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
		llmConfig = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: todoTaskLLMConfig.Primary.Provider,
				ModelID:  todoTaskLLMConfig.Primary.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
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
			APIKeys: orchestratorLLMConfig.APIKeys,
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset phase LLM for todo task orchestrator: %s/%s", hcpo.presetPhaseLLM.Provider, hcpo.presetPhaseLLM.ModelID))
	}
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for todo task orchestrator agent: step config, tiered, and preset phase LLM are all empty or invalid")
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

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
	isCodeExecutionMode := llmConfig.Primary.Provider == "claude-code" || llmConfig.Primary.Provider == "gemini-cli"
	config.UseCodeExecutionMode = isCodeExecutionMode
	if isCodeExecutionMode {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Todo task orchestrator: code execution mode enabled for CLI provider '%s'", llmConfig.Primary.Provider))
	}

	isToolSearchMode := false
	config.UseToolSearchMode = isToolSearchMode
	config.PreDiscoveredTools = nil

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
		executorsToUse = hcpo.WorkspaceToolExecutors
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using all workspace tools for todo task orchestrator agent: %d tools", len(toolsToRegister)))
	}

	// NOTE: Task management is handled via shell commands (execute_shell_command)
	// The LLM manages tasks.md (markdown format) using shell tools like cat, sed, heredoc

	// Filter out human tools if "no human" execution mode is active
	execOpts := hcpo.GetExecutionOptions()
	if execOpts != nil && (execOpts.ExecutionStrategy == ExecutionStrategyStartFromBeginningNoHuman || execOpts.ExecutionStrategy == ExecutionStrategyResumeFromStepNoHuman || execOpts.ExecutionStrategy == ExecutionStrategyFastExecuteAll) {
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
		// Determine if dynamic tier selection is enabled for sub-agent tools
		enableTierSelection := hcpo.tierResolver != nil && subAgentExecCtx.TodoTaskStep != nil
		if enableTierSelection {
			stepConfig := getAgentConfigs(subAgentExecCtx.TodoTaskStep)
			enableTierSelection = stepConfig != nil &&
				stepConfig.EnableDynamicTierSelection != nil &&
				*stepConfig.EnableDynamicTierSelection
		}
		subAgentTools := virtualtools.CreateSubAgentTools(enableTierSelection)
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

	// IMPORTANT: Inject completion tools for step completion signaling
	// The mark_step_complete tool writes completed.txt so the controller loop can detect completion
	{
		completionTools := virtualtools.CreateCompletionTools()
		completionExecutors := virtualtools.CreateCompletionToolExecutors()
		completionCategory := virtualtools.GetCompletionToolCategory()

		// Add completion tools to the tools list and register their category
		for _, tool := range completionTools {
			toolsToRegister = append(toolsToRegister, tool)
			if hcpo.ToolCategories != nil {
				hcpo.ToolCategories[tool.Function.Name] = completionCategory
			}
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Added completion tool '%s' to todo task orchestrator (category: %s)", tool.Function.Name, completionCategory))
		}

		// Wrap completion executors with context injection
		markStepCompleteFunc := hcpo.createMarkStepCompleteFunc(stepPath)
		for toolName, executor := range completionExecutors {
			wrappedExecutor := hcpo.wrapCompletionToolExecutor(executor, markStepCompleteFunc)
			executorsToUse[toolName] = wrappedExecutor
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Wrapped completion tool '%s' with mark complete function injection", toolName))
		}
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

	// Add skill prompt if skills are selected
	effectiveSkills := GetEffectiveSkills(stepConfig, hcpo.BaseOrchestrator)
	if len(effectiveSkills) > 0 {
		baseAgent := agent.GetBaseAgent()
		if baseAgent != nil {
			mcpAgent := baseAgent.Agent()
			if mcpAgent != nil {
				skillPrompt := BuildWorkflowSkillPrompt(ctx, effectiveSkills, hcpo.BaseOrchestrator)
				if skillPrompt != "" {
					mcpAgent.AppendSystemPrompt(skillPrompt)
					hcpo.GetLogger().Info(fmt.Sprintf("🎯 Added skill prompt to todo task orchestrator agent (%d skills): %v", len(effectiveSkills), effectiveSkills))
				}
			}
		}
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Created todo task orchestrator agent using standard factory pattern: %s (step %d, phase %s)", agentName, step+1, phase))
	return agent, nil
}

// wrapSubAgentToolExecutor wraps a sub-agent tool executor to inject execution functions
// The wrapper adds: execute_predefined_sub_agent, execute_generic_agent, mark_step_complete, predefined_routes, validate_todo_exists, sub_agent_llm
func (hcpo *StepBasedWorkflowOrchestrator) wrapSubAgentToolExecutor(
	originalExecutor func(ctx context.Context, args map[string]interface{}) (string, error),
	execCtx *SubAgentExecutionContext,
) func(ctx context.Context, args map[string]interface{}) (string, error) {
	// Return wrapper function that injects execution functions into context
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		// Inject validate_todo_exists function for validation before delegation
		validateTodoFunc := hcpo.createValidateTodoExistsFunc(execCtx)
		ctx = context.WithValue(ctx, virtualtools.ValidateTodoExistsKey, validateTodoFunc)

		// Inject execute_predefined_sub_agent function
		executePredefinedFunc := hcpo.createExecutePredefinedSubAgentFunc(execCtx)
		ctx = context.WithValue(ctx, virtualtools.ExecutePredefinedSubAgentKey, executePredefinedFunc)

		// Inject execute_generic_agent function
		executeGenericFunc := hcpo.createExecuteGenericAgentFunc(execCtx)
		ctx = context.WithValue(ctx, virtualtools.ExecuteGenericAgentKey, executeGenericFunc)

		// Inject predefined routes for route lookup
		if execCtx.TodoTaskStep != nil {
			ctx = context.WithValue(ctx, virtualtools.PredefinedRoutesKey, execCtx.TodoTaskStep.PredefinedRoutes)
		}

		// Inject sub_agent_llm override if configured (works in both tiered and manual modes)
		if execCtx.StepConfig != nil && execCtx.StepConfig.SubAgentLLM != nil {
			ctx = context.WithValue(ctx, virtualtools.SubAgentLLMContextKey, execCtx.StepConfig.SubAgentLLM)
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

		// Before sub-agent: emit current tasks.md state so UI shows pre-execution state
		hcpo.emitTodoTaskStatusUpdate(ctx, args, execCtx)
		hcpo.flushTodoTaskStatusDebouncer()

		// Call original executor with enriched context
		result, err := originalExecutor(ctx, args)

		// After sub-agent completes, emit tasks.md state (debounced to coalesce parallel completions)
		hcpo.emitTodoTaskStatusUpdate(ctx, args, execCtx)

		return result, err
	}
}

// wrapCompletionToolExecutor wraps a completion tool executor to inject the mark step complete function into context
func (hcpo *StepBasedWorkflowOrchestrator) wrapCompletionToolExecutor(
	originalExecutor func(ctx context.Context, args map[string]interface{}) (string, error),
	markStepCompleteFunc virtualtools.MarkStepCompleteFunc,
) func(ctx context.Context, args map[string]interface{}) (string, error) {
	return func(ctx context.Context, args map[string]interface{}) (string, error) {
		ctx = context.WithValue(ctx, virtualtools.MarkStepCompleteKey, markStepCompleteFunc)
		return originalExecutor(ctx, args)
	}
}

// createMarkStepCompleteFunc creates a function that writes completed.txt to signal step completion
// The file is written to {stepExecutionPath}/completed.txt via the workspace API
func (hcpo *StepBasedWorkflowOrchestrator) createMarkStepCompleteFunc(stepPath string) virtualtools.MarkStepCompleteFunc {
	return func(ctx context.Context, reason string) (string, error) {
		// Build the completed.txt path (relative to workspace)
		var stepExecutionPath string
		if hcpo.selectedRunFolder != "" {
			stepExecutionPath = filepath.Join("runs", hcpo.selectedRunFolder, "execution", stepPath)
		} else {
			stepExecutionPath = filepath.Join("execution", stepPath)
		}
		completedFilePath := filepath.Join(stepExecutionPath, "completed.txt")

		// Write the reason to completed.txt
		if err := hcpo.WriteWorkspaceFile(ctx, completedFilePath, reason); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to write completed.txt: %s: %v", completedFilePath, err))
			return "", fmt.Errorf("failed to write completion marker: %w", err)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ Step marked as complete via mark_step_complete tool: %s (reason: %s)", completedFilePath, reason))
		return fmt.Sprintf("Step marked as complete. Reason recorded: %s", reason), nil
	}
}

// createValidateTodoExistsFunc creates a function that validates if a task exists
// This function is injected into context for call_sub_agent/call_generic_agent to use
// Reads tasks.md and parses markdown checkboxes to find tasks
// Returns (exists, totalTasks, fullTasksFilePath, error) - fullTasksFilePath includes workspace path
func (hcpo *StepBasedWorkflowOrchestrator) createValidateTodoExistsFunc(
	execCtx *SubAgentExecutionContext,
) virtualtools.ValidateTodoExistsFunc {
	return func(ctx context.Context, todoID string) (bool, int, string, error) {
		// Build the tasks file path (relative to workspace)
		var tasksFilePathRelative string
		if hcpo.selectedRunFolder != "" {
			tasksFilePathRelative = filepath.Join("runs", hcpo.selectedRunFolder, "execution", execCtx.StepPath, "tasks.md")
		} else {
			tasksFilePathRelative = filepath.Join("execution", execCtx.StepPath, "tasks.md")
		}

		// Build full path including workspace for error messages
		fullTasksFilePath := filepath.Join(hcpo.GetWorkspacePath(), tasksFilePathRelative)

		// Read the tasks file
		content, err := hcpo.ReadWorkspaceFile(ctx, tasksFilePathRelative)
		if err != nil {
			// File doesn't exist means no tasks
			return false, 0, fullTasksFilePath, nil
		}

		// Parse markdown to find tasks
		// Format: - [ ] task_id: description  OR  - [x] task_id: description  OR  - [~] task_id: description
		lines := strings.Split(content, "\n")
		var taskIDs []string

		for _, line := range lines {
			line = strings.TrimSpace(line)
			// Match checkbox patterns: - [ ], - [x], - [~]
			if strings.HasPrefix(line, "- [ ]") || strings.HasPrefix(line, "- [x]") || strings.HasPrefix(line, "- [~]") {
				// Extract task ID (text after checkbox up to colon)
				// Format: "- [ ] task_id: description"
				checkboxEnd := 6 // Length of "- [ ] " or "- [x] " or "- [~] "
				if len(line) > checkboxEnd {
					remainder := line[checkboxEnd:]
					// Find the colon separator
					colonIdx := strings.Index(remainder, ":")
					if colonIdx > 0 {
						taskID := strings.TrimSpace(remainder[:colonIdx])
						taskIDs = append(taskIDs, taskID)
					}
				}
			}
		}

		// Check if task exists
		totalTasks := len(taskIDs)
		for _, id := range taskIDs {
			if id == todoID {
				return true, totalTasks, fullTasksFilePath, nil
			}
		}

		return false, totalTasks, fullTasksFilePath, nil
	}
}

// createExecutePredefinedSubAgentFunc creates a function that executes predefined sub-agents
// This function is injected into context for the call_sub_agent tool to use
func (hcpo *StepBasedWorkflowOrchestrator) createExecutePredefinedSubAgentFunc(
	execCtx *SubAgentExecutionContext,
) virtualtools.ExecutePredefinedSubAgentFunc {
	return func(ctx context.Context, routeID, todoID, instructions, successCriteria string) (string, error) {
		hcpo.GetLogger().Info(fmt.Sprintf("🤖 [TOOL] Executing predefined sub-agent via tool: route=%s, todo=%s", routeID, todoID))

		// Browser isolation: generate isolated session ID when share_browser=false
		if sb, ok := ctx.Value(virtualtools.SubAgentShareBrowserKey).(bool); ok && !sb {
			isolatedSessionID := fmt.Sprintf("%s-isolated-%d", hcpo.getSessionID(), time.Now().UnixNano())
			ctx = context.WithValue(ctx, virtualtools.SubAgentIsolatedSessionIDKey, isolatedSessionID)
			hcpo.GetLogger().Info(fmt.Sprintf("Browser isolation: sub-agent gets session %s", isolatedSessionID))
			defer func() {
				mcpagent.CloseSession(isolatedSessionID)
				hcpo.GetLogger().Info(fmt.Sprintf("Closed isolated browser session: %s", isolatedSessionID))
			}()
		}

		// Build a TodoTaskResponse to reuse existing execution logic
		response := &TodoTaskResponse{
			NextAction:                 "delegate",
			SelectedRouteID:            routeID,
			TodoIDToExecute:            todoID,
			InstructionsToSubAgent:     instructions,
			SuccessCriteriaForSubAgent: successCriteria,
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
			return fmt.Sprintf("ERROR: %v", err), nil // Return error as result, not as error (agent can handle)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ [TOOL] Predefined sub-agent completed successfully: route=%s, todo=%s", routeID, todoID))
		return result, nil
	}
}

// createExecuteGenericAgentFunc creates a function that executes generic agents
// This function is injected into context for the call_generic_agent tool to use
// Sub-agents get all their input from the tool parameters (instructions, successCriteria)
// They do NOT read the tasks.md file - the orchestrator provides everything via the tool call
func (hcpo *StepBasedWorkflowOrchestrator) createExecuteGenericAgentFunc(
	execCtx *SubAgentExecutionContext,
) virtualtools.ExecuteGenericAgentFunc {
	return func(ctx context.Context, todoID, instructions, successCriteria string) (string, error) {
		hcpo.GetLogger().Info(fmt.Sprintf("🤖 [TOOL] Executing generic agent via tool: todo=%s", todoID))

		// Browser isolation: generate isolated session ID when share_browser=false
		if sb, ok := ctx.Value(virtualtools.SubAgentShareBrowserKey).(bool); ok && !sb {
			isolatedSessionID := fmt.Sprintf("%s-isolated-%d", hcpo.getSessionID(), time.Now().UnixNano())
			ctx = context.WithValue(ctx, virtualtools.SubAgentIsolatedSessionIDKey, isolatedSessionID)
			hcpo.GetLogger().Info(fmt.Sprintf("Browser isolation: sub-agent gets session %s", isolatedSessionID))
			defer func() {
				mcpagent.CloseSession(isolatedSessionID)
				hcpo.GetLogger().Info(fmt.Sprintf("Closed isolated browser session: %s", isolatedSessionID))
			}()
		}

		// Build a TodoTaskResponse to reuse existing execution logic
		// All task info comes from the tool parameters, not from a file
		response := &TodoTaskResponse{
			NextAction:                 "delegate",
			UseGenericAgent:            true,
			TodoIDToExecute:            todoID,
			InstructionsToSubAgent:     instructions,
			SuccessCriteriaForSubAgent: successCriteria,
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
// Priority: tiered medium > presetPhaseLLM > orchestrator fallback
func (hcpo *StepBasedWorkflowOrchestrator) selectEvaluationScoringLLM() (*orchestrator.LLMConfig, error) {
	// Prefer tiered medium — scoring is analysis, not generation
	if hcpo.tierResolver != nil {
		llmConfig := hcpo.tierResolver.ResolveTier(TierMedium)
		if llmConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🏷️ Using Tier 2 (Medium) for evaluation scoring: %s/%s", llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
			return llmConfig, nil
		}
	}

	orchestratorLLMConfig := hcpo.GetLLMConfig()

	if hcpo.presetPhaseLLM != nil && hcpo.presetPhaseLLM.Provider != "" && hcpo.presetPhaseLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset phase LLM for evaluation scoring: %s/%s", hcpo.presetPhaseLLM.Provider, hcpo.presetPhaseLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetPhaseLLM.Provider,
				ModelID:  hcpo.presetPhaseLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys,
		}, nil
	}

	if orchestratorLLMConfig != nil && orchestratorLLMConfig.Primary.Provider != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator LLM for evaluation scoring: %s/%s", orchestratorLLMConfig.Primary.Provider, orchestratorLLMConfig.Primary.ModelID))
		return orchestratorLLMConfig, nil
	}

	return nil, fmt.Errorf("no valid LLM configuration found for evaluation scoring agent")
}

// createEvaluationScoringAgent creates a single scoring agent that scores ALL evaluation steps
// at once. It registers submit_score (called per step) and submit_summary (called once at the end).
func (hcpo *StepBasedWorkflowOrchestrator) createEvaluationScoringAgent(ctx context.Context, phase string, evaluationPlan *EvaluationPlan, stepInputs []EvaluationStepInput) (*EvaluationReport, error) {
	agentName := "evaluation-scoring-agent"

	// Select LLM config
	llmConfig, err := hcpo.selectEvaluationScoringLLM()
	if err != nil {
		return nil, err
	}

	maxTurns := 100
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	config.ServerNames = []string{mcpclient.NoServers}
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(&AgentLLMConfig{
		Provider: config.LLMConfig.Primary.Provider,
		ModelID:  config.LLMConfig.Primary.ModelID,
	})
	config.UseToolSearchMode = false

	hcpo.setupBrowserDownloadsPathOverride(ctx, config, nil)

	// Build step lookup for title/criteria resolution
	stepLookup := make(map[string]*EvaluationStep)
	for _, step := range evaluationPlan.Steps {
		stepLookup[step.ID] = step
	}

	// Captured scores and summary
	var capturedScores []*EvaluationStepScore
	var capturedSummary string
	expectedScores := len(stepInputs)

	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		0, 0, phase,
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowEvaluationScoringAgent(cfg, logger, tracer, eventBridge)
		},
		nil, nil, true,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create evaluation scoring agent: %w", err)
	}

	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil after creation")
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("mcp agent is nil after creation")
	}

	// submit_score — called once per step
	mcpAgent.RegisterCustomTool(
		"submit_score",
		"Submit the evaluation score for one step. Call this once per evaluation step.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id":   map[string]interface{}{"type": "string", "description": "The ID of the evaluation step"},
				"score":     map[string]interface{}{"type": "integer", "description": "Score (0-10 based on success criteria)"},
				"reasoning": map[string]interface{}{"type": "string", "description": "Why this score was assigned"},
				"evidence":  map[string]interface{}{"type": "string", "description": "Key evidence from the output"},
			},
			"required": []string{"step_id", "score", "reasoning", "evidence"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepID, _ := args["step_id"].(string)
			scoreFloat, _ := args["score"].(float64)
			score := int(scoreFloat)
			reasoning, _ := args["reasoning"].(string)
			evidence, _ := args["evidence"].(string)

			stepTitle := stepID
			stepCriteria := ""
			if s, ok := stepLookup[stepID]; ok {
				stepTitle = s.Title
				stepCriteria = s.SuccessCriteria
			}

			capturedScores = append(capturedScores, &EvaluationStepScore{
				StepID:          stepID,
				StepTitle:       stepTitle,
				Score:           score,
				MaxScore:        10,
				Reasoning:       reasoning,
				Evidence:        evidence,
				SuccessCriteria: stepCriteria,
			})

			remaining := expectedScores - len(capturedScores)
			hcpo.GetLogger().Info(fmt.Sprintf("📊 Score captured: %s = %d/10 (%d remaining)", stepID, score, remaining))

			if remaining > 0 {
				return fmt.Sprintf("Score submitted: %d/10 for %s. %d steps remaining — continue scoring.", score, stepID, remaining), nil
			}
			return fmt.Sprintf("Score submitted: %d/10 for %s. All steps scored! Now call submit_summary.", score, stepID), nil
		},
		"structured_output",
	)

	// submit_summary — called once after all scores
	mcpAgent.RegisterCustomTool(
		"submit_summary",
		"Submit the overall evaluation summary after scoring all steps. Call this ONCE after all submit_score calls.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"summary": map[string]interface{}{
					"type":        "string",
					"description": "Holistic evaluation summary: overall assessment, cross-step patterns, strengths, weaknesses, and recommendations.",
				},
			},
			"required": []string{"summary"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			summary, _ := args["summary"].(string)
			capturedSummary = summary
			hcpo.GetLogger().Info("📝 Evaluation summary captured")
			return "Summary submitted. Evaluation scoring complete.", nil
		},
		"structured_output",
	)

	// Set up user prompt with all steps
	scoringAgent, ok := agent.(*WorkflowEvaluationScoringAgent)
	if !ok {
		return nil, fmt.Errorf("failed to cast agent to WorkflowEvaluationScoringAgent")
	}

	userPrompt := scoringAgent.GetUserPromptForAllSteps(stepInputs)
	scoringAgent.SetUserMessageProcessor(func(map[string]string) string {
		return userPrompt
	})

	// Execute
	_, _, err = scoringAgent.Execute(ctx, nil, nil)
	if err != nil {
		// If we got some scores before the error, use them
		if len(capturedScores) == 0 {
			return nil, fmt.Errorf("scoring agent failed with no scores captured: %w", err)
		}
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Scoring agent ended with error but captured %d/%d scores: %v", len(capturedScores), expectedScores, err))
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Evaluation scoring complete: %d/%d steps scored", len(capturedScores), expectedScores))

	report := &EvaluationReport{
		StepScores: capturedScores,
		Summary:    capturedSummary,
	}

	return report, nil
}

// createOrchestrationLearningAgent creates an orchestration learning agent for analyzing orchestrator decisions
// Deprecated: Kept for backward compatibility. Orchestration learning should be unified with the standard learning agent.
// stepIndex: 0-based step index for token tracking
func (hcpo *StepBasedWorkflowOrchestrator) createOrchestrationLearningAgent(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, stepID string, stepPath string, stepIndex int) (agents.OrchestratorAgent, error) {
	// 1. Setup folder guard (extracted method)
	// For orchestration learning, stepPath is the orchestration step path
	// Pass empty string for stepPath if not available (orchestration learning may not have stepPath)
	readPaths, writePaths := hcpo.setupLearningFolderGuard(learningPathIdentifier, stepPath)
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for orchestration learning agent - Read paths: %v, Write paths: %v (includes execution logs folder)", readPaths, writePaths))

	// Ensure the learning folder exists before running the agent
	if len(writePaths) > 0 {
		if err := hcpo.ensureStepLearningsFolderExists(ctx, writePaths[0]); err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to ensure learning folder exists: %v", err))
		}
	}

	// Determine settings
	maxTurns := hcpo.getLearningMaxTurns(stepConfig)
	// Use learning LLM config - Priority: step config > preset default
	llmConfig := hcpo.selectLearningLLM(ctx, stepConfig, stepID, stepPath)

	// Create config
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Orchestration learning agents always use NoServers (pure LLM analysis agent)
	// Phase agents always use simple mode UNLESS the provider requires code execution (claude-code, gemini-cli)
	config.ServerNames = []string{mcpclient.NoServers}

	// Code execution mode and tool search mode don't apply to learning agents
	// Use the agent's ACTUAL provider, not the phase LLM provider.
	config.UseCodeExecutionMode = requiresCodeExecutionForProvider(&AgentLLMConfig{
		Provider: config.LLMConfig.Primary.Provider,
		ModelID:  config.LLMConfig.Primary.ModelID,
	})
	config.UseToolSearchMode = false

	// Setup Downloads folder for browser tools (Playwright or agent-browser)
	// Use shared function to ensure all agent types set the override correctly if they use Playwright
	// Note: Orchestration learning agents use NoServers, but we call this for consistency and safety
	hcpo.setupBrowserDownloadsPathOverride(ctx, config, stepConfig)

	// Prepare workspace tools EXCLUDING human tools
	// Orchestration learning agents are pure LLM analysis agents and should NOT have human tools
	// They should not block on human input - they only analyze orchestration decisions and write learnings
	toolsToRegister, executorsToUse := hcpo.prepareWorkspaceToolsOnly()
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using workspace-only tools for orchestration learning agent: %d tools (human tools excluded)", len(toolsToRegister)))

	// Use base factory to create agent
	// Use stepIndex directly (passed from orchestration controller) instead of hardcoding to 0
	// This ensures orchestration learning costs are correctly attributed to the step
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		stepIndex, // Use stepIndex for proper token tracking
		0,         // iteration (not used for learning agents)
		stepID,    // Step ID
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowOrchestrationLearningAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister,
		executorsToUse,
		false, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup orchestration learning agent: %w", err)
	}

	// Post-setup: folder guard paths
	if err := hcpo.applyPostSetupToAgent(agent, agentName, false); err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for %s: %v", agentName, err))
	}

	return agent, nil
}

// Execute implements the Orchestrator interface
