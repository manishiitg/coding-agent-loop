package step_based_workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
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

	// Check effective servers (step config takes precedence over orchestrator defaults)
	var effectiveServers []string
	if stepConfig != nil && stepConfig.SelectedServers != nil && len(stepConfig.SelectedServers) > 0 {
		effectiveServers = stepConfig.SelectedServers
	} else {
		effectiveServers = hcpo.GetSelectedServers()
	}

	// Check for Playwright MCP server
	hasPlaywright := false
	for _, server := range effectiveServers {
		if server == "playwright" {
			hasPlaywright = true
			break
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
		config.RuntimeOverrides["playwright"] = playwrightOverride

		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Configured Playwright downloads path to: %s (override will be applied when connection is created)", absDownloadsPath))
		hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Runtime override for playwright: ArgsReplace=%+v", playwrightOverride.ArgsReplace))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Created Downloads folder for agent-browser: %s", absDownloadsPath))
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] Browser tool: %s, Session ID: %s, selectedRunFolder: '%s', absDownloadsPath: '%s'", browserToolType, hcpo.getSessionID(), hcpo.selectedRunFolder, absDownloadsPath))
}

// setupExecutionFolderGuard sets up folder guard paths for execution agents
// Returns readPaths and writePaths for folder guard configuration
// stepID: Step ID for step-specific learnings folder access (e.g., "step-3" or branch step ID)
// hasLearnings: If true, includes learnings folder in read paths; if false, excludes it
func (hcpo *StepBasedWorkflowOrchestrator) setupExecutionFolderGuard(stepPath string, stepID string, hasLearnings bool) (readPaths, writePaths []string) {
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
	if hcpo.UseKnowledgebase() {
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

// getCodeExecutionMode determines code execution mode with priority: step config > preset default
func (hcpo *StepBasedWorkflowOrchestrator) getCodeExecutionMode(stepConfig *AgentConfigs) bool {
	var isCodeExecutionMode bool
	if stepConfig != nil && stepConfig.UseCodeExecutionMode != nil {
		isCodeExecutionMode = *stepConfig.UseCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode: %v", isCodeExecutionMode))
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset code execution mode: %v", isCodeExecutionMode))
	}
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

// resolveStepID resolves the step ID from stepPath, stepIDOverride, or allSteps
// Priority: stepIDOverride > allSteps lookup > stepPath fallback
func (hcpo *StepBasedWorkflowOrchestrator) resolveStepID(stepPath, stepIDOverride string, allSteps []PlanStepInterface, currentStepIndex int) string {
	stepID := stepIDOverride
	pathInfo := parseStepPath(stepPath)
	stepIndexForCheck := pathInfo.ParentStepNumber - 1 // Convert to 0-based

	if stepID == "" {
		// Try to get step ID from allSteps
		if allSteps != nil && stepIndexForCheck >= 0 && stepIndexForCheck < len(allSteps) {
			// Default: use the step's own ID
			stepID = allSteps[stepIndexForCheck].GetID()

			// Special case: decision step execution (step-{N}-decision)
			// DecisionPlanStep is now flattened - learnings are stored under the step's own ID
			if strings.HasSuffix(stepPath, "-decision") {
				decisionContainerStep := allSteps[currentStepIndex]
				// DecisionPlanStep is flattened - use the step's ID directly
				if decisionStep, ok := decisionContainerStep.(*DecisionPlanStep); ok {
					stepID = decisionStep.GetID()
				}
				// stepID already set above from allSteps[stepIndexForCheck].GetID()
			}
		}
	}

	// If we still couldn't get step ID, use stepPath as fallback (will use old format)
	if stepID == "" {
		stepID = stepPath // Fallback: use stepPath as identifier
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Could not determine step ID for %s, using stepPath as fallback", stepPath))
	}

	return stepID
}

// selectExecutionLLM selects the LLM config with cascading fallback logic
// Priority:
// - If disable_temp_llm is true: step config > preset (skip tempLLM)
// - Otherwise: tempLLM1 (attempt 1) > tempLLM2 (attempt 2) > step config > preset default
// Only uses tempLLM if learnings folder has files (has existing learnings to improve upon)
func (hcpo *StepBasedWorkflowOrchestrator) selectExecutionLLM(
	ctx context.Context,
	stepConfig *AgentConfigs,
	isRetryAfterValidationFailure bool,
	retryAttempt int,
	stepID string,
	stepPath string,
	learningsFolderEmpty bool,
) *orchestrator.LLMConfig {
	// TIERED MODE: Bypass all manual selection logic
	if hcpo.useTieredMode && hcpo.tierResolver != nil {
		// Check for preferred tier override from sub-agent tool context
		if preferredTier, ok := ctx.Value(virtualtools.PreferredTierContextKey).(int); ok && preferredTier >= 1 && preferredTier <= 3 {
			tier := TierLevel(preferredTier)
			llmConfig := hcpo.tierResolver.ResolveTier(tier)
			if llmConfig != nil {
				hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Execution agent using PREFERRED Tier %d (%s) for step %s: %s/%s",
					preferredTier, TierLevelLabel(tier), stepPath, llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
			}
			return llmConfig
		}
		// Fall through to maturity-based resolution
		maturity := hcpo.getLearningMaturity(ctx, stepID, stepPath)
		llmConfig, tier := hcpo.tierResolver.ResolveForExecution(maturity)
		if llmConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Execution agent for step %s using Tier %d (%s): %s/%s (maturity: %d)",
				stepPath, int(tier), TierLevelLabel(tier), llmConfig.Primary.Provider, llmConfig.Primary.ModelID, int(maturity)))
		}
		return llmConfig
	}

	orchestratorLLMConfig := hcpo.GetLLMConfig()
	shouldSkipTempOverride := isRetryAfterValidationFailure && hcpo.fallbackToOriginalLLMOnFailure

	// Check if step config explicitly disables tempLLM
	disableTempLLM := stepConfig != nil && stepConfig.DisableTempLLM != nil && *stepConfig.DisableTempLLM
	if disableTempLLM {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step %s has disable_temp_llm=true - skipping tempLLM override and using base LLM (step config > preset)", stepPath))
		// Skip tempLLM entirely and go straight to step config > preset
		if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only LLM: %s/%s", stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID))
			return &orchestrator.LLMConfig{
				Primary: orchestrator.LLMModel{
					Provider: stepConfig.ExecutionLLM.Provider,
					ModelID:  stepConfig.ExecutionLLM.ModelID,
				},
				APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			}
		} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default execution-only LLM: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID))
			return &orchestrator.LLMConfig{
				Primary: orchestrator.LLMModel{
					Provider: hcpo.presetExecutionLLM.Provider,
					ModelID:  hcpo.presetExecutionLLM.ModelID,
				},
				APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			}
		} else {
			err := fmt.Errorf("no valid LLM configuration found for execution agent: step config and preset execution LLM are both empty or invalid")
			hcpo.GetLogger().Error("❌ No valid LLM configuration found for execution agent: step config and preset execution LLM are both empty or invalid", err)
			return nil
		}
	}

	// Cascading LLM selection based on retry attempt:
	// - retryAttempt == 1: Use tempLLM1 (if available AND learnings folder has files)
	// - retryAttempt == 2: Use tempLLM2 (if tempLLM1 was used and tempLLM2 is available AND learnings folder has files)
	// - retryAttempt >= 3: Use step LLM (step config > preset)
	hasTempLLM1 := hcpo.tempOverrideLLM != nil && hcpo.tempOverrideLLM.Provider != "" && hcpo.tempOverrideLLM.ModelID != ""
	hasTempLLM2 := hcpo.tempOverrideLLM2 != nil && hcpo.tempOverrideLLM2.Provider != "" && hcpo.tempOverrideLLM2.ModelID != ""

	if shouldSkipTempOverride && (hasTempLLM1 || hasTempLLM2) {
		hcpo.GetLogger().Info(fmt.Sprintf("🔄 Validation failed - skipping temp override LLM and falling back to original LLM (fallback_to_original_llm_on_failure enabled)"))
	}

	if learningsFolderEmpty && (hasTempLLM1 || hasTempLLM2) {
		hcpo.GetLogger().Info(fmt.Sprintf("📚 Step %s has no learnings - skipping temp override LLM and using original LLM (learnings folder is empty)", stepPath))

		// Emit event when tempLLM is skipped due to learnings folder being empty
		eventBridge := hcpo.GetContextAwareBridge()
		if eventBridge != nil {
			stepTitle := ""
			stepId := stepID
			if stepId == "" {
				stepId = stepPath
			}

			// Determine which tempLLM was skipped (tempLLM1 or tempLLM2)
			var tempLLMProvider, tempLLMModel string
			if retryAttempt == 1 && hasTempLLM1 {
				tempLLMProvider = hcpo.tempOverrideLLM.Provider
				tempLLMModel = hcpo.tempOverrideLLM.ModelID
			} else if retryAttempt == 2 && hasTempLLM2 {
				tempLLMProvider = hcpo.tempOverrideLLM2.Provider
				tempLLMModel = hcpo.tempOverrideLLM2.ModelID
			} else if hasTempLLM1 {
				// Default to tempLLM1 if available
				tempLLMProvider = hcpo.tempOverrideLLM.Provider
				tempLLMModel = hcpo.tempOverrideLLM.ModelID
			}

			baseWorkspacePath := hcpo.GetWorkspacePath()
			stepLearningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepPath)
			pathInfo := parseStepPath(stepPath)
			tempLLMSkippedEvent := &orchestrator_events.TempLLMSkippedEvent{
				BaseEventData: baseevents.BaseEventData{
					Timestamp: time.Now(),
					Component: "orchestrator",
				},
				StepID:          stepId,
				StepIndex:       pathInfo.ParentStepNumber - 1, // 0-based
				StepTitle:       stepTitle,
				StepPath:        stepPath,
				IsBranchStep:    pathInfo.IsBranchStep,
				Reason:          "learnings_folder_empty",
				TempLLMProvider: tempLLMProvider,
				TempLLMModel:    tempLLMModel,
				LearningsPath:   stepLearningsPath,
				RunFolder:       hcpo.selectedRunFolder,
				WorkspacePath:   baseWorkspacePath,
			}
			eventBridge.HandleEvent(ctx, &baseevents.AgentEvent{
				Type:      orchestrator_events.TempLLMSkipped,
				Timestamp: time.Now(),
				Data:      tempLLMSkippedEvent,
			})
			hcpo.GetLogger().Info(fmt.Sprintf("📤 Emitted temp_llm_skipped event for %s: %s (learnings folder empty, skipped tempLLM: %s/%s)", stepPath, stepPath, tempLLMProvider, tempLLMModel))
		}
	}

	// Cascading logic: tempLLM1 → tempLLM2 → step LLM
	// Only use tempLLM if learnings folder has files (has existing learnings to improve upon)
	// Note: shouldSkipTempOverride only applies to tempLLM1, not tempLLM2
	// tempLLM2 is part of the cascading fallback strategy and should be used even after tempLLM1 fails

	// Check tempLLM2 FIRST (on attempt 2 OR new loop iteration after failure) - it's part of the cascading fallback and should take priority
	// This ensures tempLLM2 is used even if other conditions might match
	// Use tempLLM2 when: (1) retryAttempt == 2 (normal retry), OR (2) isRetryAfterValidationFailure && retryAttempt == 1 (new loop iteration after failure)
	shouldUseTempLLM2 := !learningsFolderEmpty && hasTempLLM2 && (retryAttempt == 2 || (isRetryAfterValidationFailure && retryAttempt == 1))
	if shouldUseTempLLM2 {
		// Second attempt or new loop iteration after failure: Use tempLLM2 (can be used independently or as fallback after tempLLM1)
		// Note: tempLLM2 is NOT blocked by shouldSkipTempOverride - it's part of the cascading fallback strategy
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using TEMPORARY OVERRIDE LLM 2 (attempt %d, learnings folder has files): %s/%s", retryAttempt, hcpo.tempOverrideLLM2.Provider, hcpo.tempOverrideLLM2.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.tempOverrideLLM2.Provider,
				ModelID:  hcpo.tempOverrideLLM2.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else if !shouldSkipTempOverride && !learningsFolderEmpty && retryAttempt == 1 && hasTempLLM1 {
		// First attempt: Use tempLLM1
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using TEMPORARY OVERRIDE LLM 1 (attempt %d, learnings folder has files): %s/%s", retryAttempt, hcpo.tempOverrideLLM.Provider, hcpo.tempOverrideLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.tempOverrideLLM.Provider,
				ModelID:  hcpo.tempOverrideLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only LLM: %s/%s", stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: stepConfig.ExecutionLLM.Provider,
				ModelID:  stepConfig.ExecutionLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default execution-only LLM: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetExecutionLLM.Provider,
				ModelID:  hcpo.presetExecutionLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else {
		err := fmt.Errorf("no valid LLM configuration found for execution agent: temp override, step config, and preset execution LLM are all empty or invalid")
		hcpo.GetLogger().Error("❌ No valid LLM configuration found for execution agent: temp override, step config, and preset execution LLM are all empty or invalid", err)
		return nil
	}
}

// applyStepConfigToAgentConfig applies step-specific configuration overrides to agent config
func (hcpo *StepBasedWorkflowOrchestrator) applyStepConfigToAgentConfig(config *agents.OrchestratorAgentConfig, stepConfig *AgentConfigs, isCodeExecutionMode bool) {
	// Use step-specific servers if provided, otherwise use orchestrator defaults
	// NO_SERVERS is a valid config value - if step explicitly sets it, use it
	if stepConfig != nil && stepConfig.SelectedServers != nil && len(stepConfig.SelectedServers) > 0 {
		// Step has explicit server selection (including NO_SERVERS) - use it
		config.ServerNames = stepConfig.SelectedServers
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only servers: %v", stepConfig.SelectedServers))
	} else {
		// Use orchestrator defaults when stepConfig is nil or SelectedServers is empty
		config.ServerNames = hcpo.GetSelectedServers()
		if stepConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but SelectedServers is empty - using orchestrator defaults: %v", config.ServerNames))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config not found - using orchestrator defaults: %v", config.ServerNames))
		}
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		config.SelectedTools = stepConfig.SelectedTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only tools: %v", stepConfig.SelectedTools))
	} else {
		// Explicitly set orchestrator defaults when stepConfig is nil or SelectedTools is empty
		config.SelectedTools = hcpo.GetSelectedTools()
		if stepConfig != nil {
			// Log when stepConfig exists but SelectedTools is empty (will use orchestrator defaults)
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but no SelectedTools specified - using orchestrator defaults: %v", config.SelectedTools))
		}
	}

	// Code execution mode: Priority: step config > preset default (already resolved above)
	// Note: config.UseCodeExecutionMode is set by CreateStandardAgentConfigWithLLM based on orchestrator setting
	// We override it based on step config or preset default
	config.UseCodeExecutionMode = isCodeExecutionMode
	if isCodeExecutionMode {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode enabled for execution-only agent - MCP tools will be accessed via generated Go code"))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Code execution mode disabled for execution-only agent - MCP tools will be exposed directly"))
	}

	// Tool search mode: Priority: step config > preset default
	isToolSearchMode := hcpo.getToolSearchMode(stepConfig)
	config.UseToolSearchMode = isToolSearchMode
	config.PreDiscoveredTools = hcpo.getPreDiscoveredTools(stepConfig)
	if isToolSearchMode {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Tool search mode enabled for execution-only agent - tools discovered on-demand via search_tools"))
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
		// Filter tools based on unified format (category:tool or category:*)
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			stepConfig.EnabledCustomTools,
		)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(stepConfig.EnabledCustomTools), stepConfig.EnabledCustomTools))
	} else {
		// Use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	return toolsToRegister, executorsToUse
}

// prepareWorkspaceToolsOnly prepares workspace tools excluding human tools
// This is used for learning agents which should NOT have access to human tools (like human_feedback)
// Learning agents are pure LLM analysis agents that should not block on human input
func (hcpo *StepBasedWorkflowOrchestrator) prepareWorkspaceToolsOnly() ([]llmtypes.Tool, map[string]interface{}) {
	var filteredTools []llmtypes.Tool
	filteredExecutors := make(map[string]interface{})

	for _, tool := range hcpo.WorkspaceTools {
		toolName := tool.Function.Name
		// Check if this tool is a human tool by looking at its category
		if hcpo.ToolCategories != nil {
			if category, exists := hcpo.ToolCategories[toolName]; exists && category == "human" {
				// Skip human tools for learning agents
				hcpo.GetLogger().Info(fmt.Sprintf("🔧 Excluding human tool '%s' from learning agent (learning agents should not have human tools)", toolName))
				continue
			}
		}
		filteredTools = append(filteredTools, tool)
		// Also include corresponding executor
		if executor, exists := hcpo.WorkspaceToolExecutors[toolName]; exists {
			filteredExecutors[toolName] = executor
		}
	}

	return filteredTools, filteredExecutors
}

// addPrerequisiteDetectionTool adds prerequisite detection tool if prerequisite detection is enabled
// This must be called AFTER the agent is created, as it directly registers the tool on the mcpAgent
func (hcpo *StepBasedWorkflowOrchestrator) addPrerequisiteDetectionTool(
	prerequisiteInfo *PrerequisiteInfo,
	allSteps []PlanStepInterface,
	currentStepIndex int,
	cancelFunc context.CancelFunc,
	prereqErrChan chan<- *PrerequisiteFailureError,
	agentName string,
	mcpAgent *mcpagent.Agent,
) error {
	if prerequisiteInfo == nil || len(prerequisiteInfo.PrerequisiteRules) == 0 {
		return nil // No prerequisite detection needed
	}

	toolExecutor := hcpo.createPrerequisiteDetectionTool(prerequisiteInfo, allSteps, currentStepIndex, cancelFunc, prereqErrChan)
	toolParams := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"depends_on_step_id": map[string]interface{}{
				"type":        "string",
				"description": "Step ID from one of the prerequisite rules to navigate back to (e.g., \"step-0\")",
			},
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Brief explanation of why the prerequisite failure was detected, matching the condition described in the prerequisite rule",
			},
		},
		"required": []string{"depends_on_step_id", "reason"},
	}

	toolDescription := "Detect a prerequisite failure and navigate back to a prerequisite step. Call this tool when you detect that a prerequisite condition (as described in the prerequisite rules) is met during execution. Execution will stop and automatically navigate back to the specified prerequisite step."

	// Use "structured_output" category so it's always available even in code execution mode
	if err := mcpAgent.RegisterCustomTool(
		"detect_prerequisite_failure",
		toolDescription,
		toolParams,
		toolExecutor,
		"structured_output",
	); err != nil {
		hcpo.GetLogger().Error(fmt.Sprintf("❌ Failed to register prerequisite detection tool: %v", err), nil)
		return err
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Registered prerequisite detection tool for %s agent", agentName))
	return nil
}

// selectValidationLLM selects the LLM config for validation agents
// Priority: step config > preset default
func (hcpo *StepBasedWorkflowOrchestrator) selectValidationLLM(stepConfig *AgentConfigs) *orchestrator.LLMConfig {
	// TIERED MODE: Bypass all manual selection logic
	if hcpo.useTieredMode && hcpo.tierResolver != nil {
		llmConfig, tier := hcpo.tierResolver.ResolveForValidation()
		if llmConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Validation agent using Tier %d (%s): %s/%s",
				int(tier), TierLevelLabel(tier), llmConfig.Primary.Provider, llmConfig.Primary.ModelID))
		}
		return llmConfig
	}

	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.ValidationLLM != nil && stepConfig.ValidationLLM.Provider != "" && stepConfig.ValidationLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific validation LLM: %s/%s", stepConfig.ValidationLLM.Provider, stepConfig.ValidationLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: stepConfig.ValidationLLM.Provider,
				ModelID:  stepConfig.ValidationLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else if hcpo.presetValidationLLM != nil && hcpo.presetValidationLLM.Provider != "" && hcpo.presetValidationLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default validation LLM: %s/%s", hcpo.presetValidationLLM.Provider, hcpo.presetValidationLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetValidationLLM.Provider,
				ModelID:  hcpo.presetValidationLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else {
		err := fmt.Errorf("no valid LLM configuration found for validation agent: step config and preset validation LLM are both empty or invalid")
		hcpo.GetLogger().Error("❌ No valid LLM configuration found for validation agent: step config and preset validation LLM are both empty or invalid", err)
		return nil
	}
}

// getValidationMaxTurns determines max turns for validation agents
// Fixed to 25 (not configurable)
func (hcpo *StepBasedWorkflowOrchestrator) getValidationMaxTurns(stepConfig *AgentConfigs) int {
	return 25
}

// setupValidationFolderGuard sets up folder guard paths for validation agents (read-only)
func (hcpo *StepBasedWorkflowOrchestrator) setupValidationFolderGuard() (readPaths, writePaths []string) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionPath := fmt.Sprintf("%s/execution", runWorkspacePath)

	// Validation agent only reads - no write permissions needed
	// Read access to execution folder (for step outputs) and knowledgebase folder (for reference data) if enabled
	readPaths = []string{executionPath}
	if hcpo.UseKnowledgebase() {
		knowledgebasePath := getKnowledgebasePath(baseWorkspacePath)
		readPaths = append(readPaths, knowledgebasePath)
	}
	writePaths = []string{} // No write permissions - validation agent only reads and returns structured JSON
	return readPaths, writePaths
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
// Priority: tempLearningLLM (if learnings exist) > cost-optimization (tempLLM if >50% stable) > step config > preset default
// Note: If learnings already exist for a step, use tempLearningLLM if configured. For new learning (no learnings), always use default LLM.
func (hcpo *StepBasedWorkflowOrchestrator) selectLearningLLM(ctx context.Context, stepConfig *AgentConfigs, stepID string, stepPath string) *orchestrator.LLMConfig {
	// TIERED MODE: Bypass all manual selection logic
	if hcpo.useTieredMode && hcpo.tierResolver != nil {
		maturity := hcpo.getLearningMaturity(ctx, stepID, stepPath)
		llmConfig, tier := hcpo.tierResolver.ResolveForLearning(maturity)
		if llmConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🏷️ [TIERED] Learning agent for step %s using Tier %d (%s): %s/%s (maturity: %d)",
				stepPath, int(tier), TierLevelLabel(tier), llmConfig.Primary.Provider, llmConfig.Primary.ModelID, int(maturity)))
		}
		return llmConfig
	}

	orchestratorLLMConfig := hcpo.GetLLMConfig()

	// 0. TEMP LEARNING LLM: Check if learnings exist for this step and use tempLearningLLM if configured
	// If learnings exist, we can use a cheaper LLM. If no learnings exist (new learning), always use default LLM for quality.
	if stepID != "" {
		// Check if learnings folder exists and has content
		learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepID, 0, stepPath) // stepIndex not needed for this check
		if err == nil && !learningsEmpty {
			// Learnings exist - check if tempLearningLLM is configured
			if hcpo.executionOptions != nil && hcpo.executionOptions.TempLearningLLM != nil &&
				hcpo.executionOptions.TempLearningLLM.Provider != "" && hcpo.executionOptions.TempLearningLLM.ModelID != "" {
				hcpo.GetLogger().Info(fmt.Sprintf("🧠 [TEMP_LEARNING_LLM] Using temp learning LLM (%s/%s) because learnings already exist for step %s",
					hcpo.executionOptions.TempLearningLLM.Provider, hcpo.executionOptions.TempLearningLLM.ModelID, stepID))
				return &orchestrator.LLMConfig{
					Primary: orchestrator.LLMModel{
						Provider: hcpo.executionOptions.TempLearningLLM.Provider,
						ModelID:  hcpo.executionOptions.TempLearningLLM.ModelID,
					},
					APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
				}
			}
		} else if err == nil && learningsEmpty {
			// No learnings exist (new learning) - always use default LLM for quality
			hcpo.GetLogger().Info(fmt.Sprintf("🧠 [TEMP_LEARNING_LLM] No learnings exist for step %s - using default LLM for new learning", stepID))
		}
	}

	// 1. COST OPTIMIZATION: Check if we should switch to cheaper tempLLM based on stability threshold
	// If stable runs reach 50% of threshold, use tempLLM for learning
	if stepID != "" {
		metadata, err := hcpo.readStepLearningMetadata(ctx, stepID, stepPath)
		if err == nil {
			// Thresholds: Simple (3), Medium (5), Complex (10)
			// 50% Thresholds: Simple (2), Medium (3), Complex (5)
			//
			// TODO: Turn-based classification is not reliable - turn count varies significantly based on
			// the LLM model used (e.g., Claude vs GPT vs cheaper models) and doesn't reflect actual
			// step complexity. We need to develop a better complexity metric.
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
				hcpo.GetLogger().Info(fmt.Sprintf("💰 [COST_OPTIMIZATION] Switching learning agent to cheaper tempLLM (%s/%s): %s", hcpo.tempOverrideLLM.Provider, hcpo.tempOverrideLLM.ModelID, reason))
				return &orchestrator.LLMConfig{
					Primary: orchestrator.LLMModel{
						Provider: hcpo.tempOverrideLLM.Provider,
						ModelID:  hcpo.tempOverrideLLM.ModelID,
					},
					APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
				}
			}
		}
	}

	// 2. Fallback to normal priority
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific learning LLM: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: stepConfig.LearningLLM.Provider,
				ModelID:  stepConfig.LearningLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default learning LLM: %s/%s", hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetLearningLLM.Provider,
				ModelID:  hcpo.presetLearningLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else {
		err := fmt.Errorf("no valid LLM configuration found for learning agent: step config and preset learning LLM are both empty or invalid")
		hcpo.GetLogger().Error("❌ No valid LLM configuration found for learning agent: step config and preset learning LLM are both empty or invalid", err)
		return nil
	}
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
		// This ensures LLM-generated Go code can only access paths within allowed boundaries
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
// prerequisiteInfo: Prerequisite information for this step (nil if prerequisite detection is disabled)
// allSteps: All steps in the plan (required if prerequisiteInfo is not nil)
// currentStepIndex: 0-based index of current step (required if prerequisiteInfo is not nil)
// stepIDOverride: Optional explicit step ID to use for learnings / tempLLM selection (e.g., sub-agent step ID).
//
//	When empty, the step ID will be derived from allSteps based on stepPath as before.
func (hcpo *StepBasedWorkflowOrchestrator) createExecutionOnlyAgent(ctx context.Context, phase string, stepPath string, agentName string, stepConfig *AgentConfigs, isRetryAfterValidationFailure bool, retryAttempt int, prerequisiteInfo *PrerequisiteInfo, allSteps []PlanStepInterface, currentStepIndex int, cancelFunc context.CancelFunc, prereqErrChan chan<- *PrerequisiteFailureError, stepIDOverride string) (agents.OrchestratorAgent, error) {
	// 1. Resolve stepID first (needed for folder guard setup)
	stepID := hcpo.resolveStepID(stepPath, stepIDOverride, allSteps, currentStepIndex)

	// 2. Check if learnings exist for this step (needed for folder guard setup)
	hasLearnings := false
	if ctx != nil {
		learningsEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepID, currentStepIndex, stepPath)
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

	// 7. Post-setup: prerequisite tool and folder guard (after base factory setup)
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

	// Add prerequisite detection tool if needed (must be called after agent creation)
	// CRITICAL: If prerequisite detection is enabled, tool registration failure is fatal
	// The agent needs this tool to detect and handle prerequisite failures correctly
	if err := hcpo.addPrerequisiteDetectionTool(prerequisiteInfo, allSteps, currentStepIndex, cancelFunc, prereqErrChan, agentName, mcpAgent); err != nil {
		// Check if prerequisite detection is actually enabled (not just nil prerequisiteInfo)
		if prerequisiteInfo != nil && len(prerequisiteInfo.PrerequisiteRules) > 0 {
			// Prerequisite detection is enabled but tool registration failed - this is critical
			return nil, fmt.Errorf("failed to register prerequisite detection tool for %s (prerequisite detection is enabled): %w", agentName, err)
		}
		// Prerequisite detection not enabled, just log warning
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to add prerequisite detection tool: %v", err))
	}

	// Add skill prompt if skills are selected (uses effectiveSkills from folder guard setup above)
	if len(effectiveSkills) > 0 {
		skillPrompt := BuildWorkflowSkillPrompt(ctx, effectiveSkills, hcpo.BaseOrchestrator)
		if skillPrompt != "" {
			mcpAgent.AppendSystemPrompt(skillPrompt)
			hcpo.GetLogger().Info(fmt.Sprintf("🎯 Added skill prompt to execution agent (%d skills): %v", len(effectiveSkills), effectiveSkills))
		}
	}

	// Apply post-setup configuration (folder guard paths and optional registry update)
	if err := hcpo.applyPostSetupToAgent(agent, agentName, isCodeExecutionMode); err != nil {
		// Log warning but don't fail agent creation
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for %s: %v", agentName, err))
	}

	return agent, nil
}

// createValidationAgent creates a validation agent for the current iteration
func (hcpo *StepBasedWorkflowOrchestrator) createValidationAgent(ctx context.Context, phase string, step int, stepID string, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	// 1. Setup folder guard (read-only for validation agents)
	readPaths, writePaths := hcpo.setupValidationFolderGuard()
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for validation agent - Read paths: %v, Write paths: %v (read-only, no file writes)", readPaths, writePaths))

	// 2. Determine settings (extracted methods)
	maxTurns := hcpo.getValidationMaxTurns(stepConfig)
	llmConfig := hcpo.selectValidationLLM(stepConfig)
	if llmConfig == nil {
		return nil, fmt.Errorf("no valid LLM configuration found for validation agent: step config and preset validation LLM are both empty or invalid")
	}

	// 3. Create config
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Validation agents always use NoServers (pure LLM validation agent)
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM validation agent

	// Code execution mode only applies to execution agents, not validation agents
	config.UseCodeExecutionMode = false
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling code execution mode for validation agent (only execution agents use MCP tools)"))

	// Set EnableContextOffloading if specified
	if stepConfig != nil && stepConfig.EnableContextOffloading != nil {
		config.EnableContextOffloading = stepConfig.EnableContextOffloading
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific context offloading setting: %v", *stepConfig.EnableContextOffloading))
	}

	// Setup Downloads folder for browser tools (Playwright or agent-browser)
	// Use shared function to ensure all agent types set the override correctly if they use Playwright
	hcpo.setupBrowserDownloadsPathOverride(ctx, config, stepConfig)

	// 4. Prepare custom tools (filtered by step config)
	toolsToRegister, executorsToUse := hcpo.prepareCustomTools(stepConfig)

	// 5. Use base factory! (This handles all setup automatically)
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		step,
		0,      // iteration
		stepID, // Step ID
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowValidationAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister,
		executorsToUse,
		false, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup validation agent: %w", err)
	}

	// 6. Post-setup: folder guard paths (validation agents don't use code execution mode, so no registry update needed)
	// Note: Validation agents have config.UseCodeExecutionMode = false, so base factory won't update registry
	// and we don't need to update it either - validation agents are pure LLM agents with no code execution
	if err := hcpo.applyPostSetupToAgent(agent, agentName, false); err != nil {
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

	// Code execution mode only applies to execution agents, not learning agents
	// CRITICAL: Override orchestrator-level code execution mode setting - learning agents are pure LLM analysis agents
	config.UseCodeExecutionMode = false
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

	// Use step-specific servers if provided, otherwise use orchestrator defaults
	// NO_SERVERS is a valid config value - if step explicitly sets it, use it
	if stepConfig != nil && stepConfig.SelectedServers != nil && len(stepConfig.SelectedServers) > 0 {
		// Step has explicit server selection (including NO_SERVERS) - use it
		config.ServerNames = stepConfig.SelectedServers
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific conditional servers: %v", stepConfig.SelectedServers))
	} else {
		// Use orchestrator defaults when stepConfig is nil or SelectedServers is empty
		config.ServerNames = hcpo.GetSelectedServers()
		if stepConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but SelectedServers is empty - using orchestrator defaults: %v", config.ServerNames))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config not found - using orchestrator defaults: %v", config.ServerNames))
		}
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		config.SelectedTools = stepConfig.SelectedTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific conditional tools: %v", stepConfig.SelectedTools))
	} else {
		config.SelectedTools = hcpo.GetSelectedTools()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default conditional tools: %v", config.SelectedTools))
	}

	// Code execution mode: Priority: step config > orchestrator default (same as execution agent)
	var isCodeExecutionMode bool
	if stepConfig != nil && stepConfig.UseCodeExecutionMode != nil {
		isCodeExecutionMode = *stepConfig.UseCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode for conditional agent: %v", isCodeExecutionMode))
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator code execution mode for conditional agent: %v", isCodeExecutionMode))
	}
	config.UseCodeExecutionMode = isCodeExecutionMode

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

// createOrchestrationOrchestratorAgent creates an orchestration orchestrator agent using the standard factory pattern
// This agent executes the main orchestration step (orchestration and delegation, not direct execution)
// Note: Folder guard paths should be set by the caller before calling this function (see controller_orchestration.go)
func (hcpo *StepBasedWorkflowOrchestrator) createOrchestrationOrchestratorAgent(ctx context.Context, phase string, step, iteration int, stepID string, agentName string, stepConfig *AgentConfigs, orchestrationLLMConfig *orchestrator.LLMConfig) (agents.OrchestratorAgent, error) {
	// Orchestration orchestrator agent needs folder guard (can write files)
	// Note: Folder guard is set by caller in controller_orchestration.go before agent creation
	// We apply it to the agent here via post-setup

	// Determine max turns: use orchestrator default
	maxTurns := hcpo.GetMaxTurns()

	// Determine LLM config: Priority: step config > preset default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if orchestrationLLMConfig != nil && orchestrationLLMConfig.Primary.Provider != "" && orchestrationLLMConfig.Primary.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: orchestrationLLMConfig.Primary.Provider,
				ModelID:  orchestrationLLMConfig.Primary.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific orchestration orchestrator LLM: %s/%s", orchestrationLLMConfig.Primary.Provider, orchestrationLLMConfig.Primary.ModelID))
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetExecutionLLM.Provider,
				ModelID:  hcpo.presetExecutionLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default ExecutionLLM for orchestration orchestrator: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID))
	} else {
		return nil, fmt.Errorf("no valid LLM configuration found for orchestration orchestrator agent: step config and preset execution LLM are both empty or invalid")
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatText, llmConfig)

	// Use step-specific servers if provided, otherwise use orchestrator defaults
	// NO_SERVERS is a valid config value - if step explicitly sets it, use it
	if stepConfig != nil && stepConfig.SelectedServers != nil && len(stepConfig.SelectedServers) > 0 {
		// Step has explicit server selection (including NO_SERVERS) - use it
		config.ServerNames = stepConfig.SelectedServers
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific orchestration orchestrator servers: %v", stepConfig.SelectedServers))
	} else {
		// Use orchestrator defaults when stepConfig is nil or SelectedServers is empty
		config.ServerNames = hcpo.GetSelectedServers()
		if stepConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but SelectedServers is empty - using orchestrator defaults: %v", config.ServerNames))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config not found - using orchestrator defaults: %v", config.ServerNames))
		}
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		config.SelectedTools = stepConfig.SelectedTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific orchestration orchestrator tools: %v", stepConfig.SelectedTools))
	} else {
		config.SelectedTools = hcpo.GetSelectedTools()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default orchestration orchestrator tools: %v", config.SelectedTools))
	}

	// Code execution mode: Priority: step config > orchestrator default
	// Use helper method for consistency
	isCodeExecutionMode := hcpo.getCodeExecutionMode(stepConfig)
	config.UseCodeExecutionMode = isCodeExecutionMode

	// Tool search mode: Priority: step config > orchestrator default
	isToolSearchMode := hcpo.getToolSearchMode(stepConfig)
	config.UseToolSearchMode = isToolSearchMode
	config.PreDiscoveredTools = hcpo.getPreDiscoveredTools(stepConfig)

	// Set EnableContextOffloading if specified
	if stepConfig != nil && stepConfig.EnableContextOffloading != nil {
		config.EnableContextOffloading = stepConfig.EnableContextOffloading
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific context offloading setting: %v", *stepConfig.EnableContextOffloading))
	}

	// Setup Downloads folder for browser tools (Playwright or agent-browser)
	// CRITICAL FIX (Jan 2026): Orchestrator agents can also use Playwright (e.g., step 3 has SelectedServers: [playwright]).
	// If the orchestrator agent creates the Playwright connection first, it must set the correct Downloads path override
	// before creating the connection, otherwise all subsequent agents will reuse the wrong connection.
	// Use shared function to ensure both execution and orchestrator agents set the override correctly.
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
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered custom tools for orchestration orchestrator agent: %d tools enabled from %d entries: %v", len(toolsToRegister), len(stepConfig.EnabledCustomTools), stepConfig.EnabledCustomTools))
	} else {
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using all workspace tools for orchestration orchestrator agent: %d tools", len(toolsToRegister)))
	}

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
					hcpo.GetLogger().Info(fmt.Sprintf("🔧 Excluding human tool '%s' from orchestration orchestrator agent (no human mode)", tool.Function.Name))
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
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered out human tools for orchestration orchestrator agent (no human mode): %d tools remaining", len(toolsToRegister)))
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
			return NewWorkflowOrchestrationOrchestratorAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister, // Pass workspace tools (filtered by step config if specified)
		executorsToUse,  // Pass workspace tool executors
		false,           // Don't overwrite system prompt - orchestration orchestrator agent manages its own prompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create orchestration orchestrator agent: %w", err)
	}

	// Post-setup: folder guard paths (orchestration orchestrator agent may use code execution mode, so registry update may be needed)
	// Note: Folder guard paths are already set on orchestrator by caller, but we need to apply them to the agent
	if err := hcpo.applyPostSetupToAgent(agent, agentName, isCodeExecutionMode); err != nil {
		return nil, fmt.Errorf("failed to apply post-setup to orchestration orchestrator agent: %w", err)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Created orchestration orchestrator agent using standard factory pattern: %s (step %d, phase %s)", agentName, step+1, phase))
	return agent, nil
}

// SubAgentExecutionContext holds the context needed for sub-agent execution from tools
type SubAgentExecutionContext struct {
	TodoTaskStep *TodoTaskPlanStep
	StepIndex    int
	StepPath     string
	AllSteps     []PlanStepInterface
	Progress     *StepProgress
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
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetExecutionLLM.Provider,
				ModelID:  hcpo.presetExecutionLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default ExecutionLLM for todo task orchestrator: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID))
	} else {
		return nil, fmt.Errorf("no valid LLM configuration found for todo task orchestrator agent: step config and preset execution LLM are both empty or invalid")
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Use step-specific servers if provided, otherwise use orchestrator defaults
	// NO_SERVERS is a valid config value - if step explicitly sets it, use it
	if stepConfig != nil && stepConfig.SelectedServers != nil && len(stepConfig.SelectedServers) > 0 {
		// Step has explicit server selection (including NO_SERVERS) - use it
		config.ServerNames = stepConfig.SelectedServers
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific todo task orchestrator servers: %v", stepConfig.SelectedServers))
	} else {
		// Use orchestrator defaults when stepConfig is nil or SelectedServers is empty
		config.ServerNames = hcpo.GetSelectedServers()
		if stepConfig != nil {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but SelectedServers is empty - using orchestrator defaults: %v", config.ServerNames))
		} else {
			hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config not found - using orchestrator defaults: %v", config.ServerNames))
		}
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		config.SelectedTools = stepConfig.SelectedTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific todo task orchestrator tools: %v", stepConfig.SelectedTools))
	} else {
		config.SelectedTools = hcpo.GetSelectedTools()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default todo task orchestrator tools: %v", config.SelectedTools))
	}

	// Code execution mode: Priority: step config > orchestrator default
	// Use helper method for consistency
	isCodeExecutionMode := hcpo.getCodeExecutionMode(stepConfig)
	config.UseCodeExecutionMode = isCodeExecutionMode

	// Tool search mode: Priority: step config > orchestrator default
	isToolSearchMode := hcpo.getToolSearchMode(stepConfig)
	config.UseToolSearchMode = isToolSearchMode
	config.PreDiscoveredTools = hcpo.getPreDiscoveredTools(stepConfig)

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
		enableTierSelection := hcpo.useTieredMode && subAgentExecCtx.TodoTaskStep != nil
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

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Created todo task orchestrator agent using standard factory pattern: %s (step %d, phase %s)", agentName, step+1, phase))
	return agent, nil
}

// wrapSubAgentToolExecutor wraps a sub-agent tool executor to inject execution functions
// The wrapper adds: execute_predefined_sub_agent, execute_generic_agent, mark_step_complete, predefined_routes, validate_todo_exists
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

		// Call original executor with enriched context
		return originalExecutor(ctx, args)
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

		// Build a TodoTaskResponse to reuse existing execution logic
		response := &TodoTaskResponse{
			NextAction:                 "delegate",
			SelectedRouteID:            routeID,
			TodoIDToExecute:            todoID,
			InstructionsToSubAgent:     instructions,
			SuccessCriteriaForSubAgent: successCriteria,
		}

		// Execute using existing method
		result, err := hcpo.executePredefinedSubAgent(
			ctx,
			execCtx.TodoTaskStep,
			execCtx.StepIndex,
			execCtx.StepPath,
			response,
			execCtx.AllSteps,
			execCtx.Progress,
		)

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

		// Build a TodoTaskResponse to reuse existing execution logic
		// All task info comes from the tool parameters, not from a file
		response := &TodoTaskResponse{
			NextAction:                 "delegate",
			UseGenericAgent:            true,
			TodoIDToExecute:            todoID,
			InstructionsToSubAgent:     instructions,
			SuccessCriteriaForSubAgent: successCriteria,
		}

		// Execute using existing method
		// All task info comes from tool parameters
		result, err := hcpo.executeGenericAgent(
			ctx,
			execCtx.TodoTaskStep,
			execCtx.StepIndex,
			execCtx.StepPath,
			response,
			execCtx.AllSteps,
			execCtx.Progress,
		)

		if err != nil {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ [TOOL] Generic agent execution failed: %v", err))
			return fmt.Sprintf("ERROR: %v", err), nil // Return error as result, not as error (agent can handle)
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ [TOOL] Generic agent completed successfully: todo=%s", todoID))
		return result, nil
	}
}

// selectEvaluationScoringLLM selects the LLM config for evaluation scoring agents
// Priority: presetExecutionLLM only
func (hcpo *StepBasedWorkflowOrchestrator) selectEvaluationScoringLLM() (*orchestrator.LLMConfig, error) {
	orchestratorLLMConfig := hcpo.GetLLMConfig()

	if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset execution LLM for evaluation scoring: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID))
		return &orchestrator.LLMConfig{
			Primary: orchestrator.LLMModel{
				Provider: hcpo.presetExecutionLLM.Provider,
				ModelID:  hcpo.presetExecutionLLM.ModelID,
			},
			APIKeys: orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}, nil
	} else {
		return nil, fmt.Errorf("no valid LLM configuration found for evaluation scoring agent: presetExecutionLLM is empty or invalid")
	}
}

// createEvaluationScoringAgent creates an evaluation scoring agent using the standard factory pattern
// This agent analyzes evaluation step outputs and calculates scores based on success criteria
func (hcpo *StepBasedWorkflowOrchestrator) createEvaluationScoringAgent(ctx context.Context, phase string, step *EvaluationStep, executionOutput string) (agents.OrchestratorAgent, *EvaluationStepScore, error) {
	agentName := "evaluation-scoring-agent"

	// Select LLM config using presetExecutionLLM priority
	llmConfig, err := hcpo.selectEvaluationScoringLLM()
	if err != nil {
		return nil, nil, err
	}

	// Create agent config
	maxTurns := 5 // Fixed max turns for scoring agent
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Scoring agents always use NoServers (pure LLM analysis agent)
	config.ServerNames = []string{mcpclient.NoServers}
	config.UseCodeExecutionMode = false

	// Setup Downloads folder for browser tools (Playwright or agent-browser)
	// Use shared function to ensure all agent types set the override correctly if they use Playwright
	// Note: Evaluation scoring agents use NoServers, but we call this for consistency and safety
	hcpo.setupBrowserDownloadsPathOverride(ctx, config, nil)

	// Variable to capture the score from the tool
	var capturedScore *EvaluationStepScore

	// Create a cancellable context to break conversation as soon as submit_score tool is called
	// This prevents the zero candidates error by stopping the conversation loop immediately after the tool call
	toolCalledCtx, cancelToolCalled := context.WithCancel(ctx)
	defer cancelToolCalled()

	// Use base factory to create agent with proper event bridging
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		0,     // step (not used for scoring agents)
		0,     // iteration (not used for scoring agents)
		phase, // stepID (use phase name for phase-only agents)
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewWorkflowEvaluationScoringAgent(cfg, logger, tracer, eventBridge)
		},
		nil,  // no additional tools to register
		nil,  // no additional executors
		true, // overwrite system prompt
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create and setup evaluation scoring agent: %w", err)
	}

	// Register the submit_score tool on the created agent
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, nil, fmt.Errorf("base agent is nil after creation for %s", agentName)
	}
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, nil, fmt.Errorf("mcp agent is nil after creation for %s", agentName)
	}

	mcpAgent.RegisterCustomTool(
		"submit_score",
		"Submit the evaluation score for this step. You MUST call this tool with your analysis.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"step_id": map[string]interface{}{
					"type":        "string",
					"description": "The ID of the evaluation step",
				},
				"score": map[string]interface{}{
					"type":        "integer",
					"description": "The score (0, 5, or 10 based on success criteria)",
				},
				"reasoning": map[string]interface{}{
					"type":        "string",
					"description": "Brief explanation of why this score was assigned",
				},
				"evidence": map[string]interface{}{
					"type":        "string",
					"description": "Key evidence from the execution output supporting this score",
				},
			},
			"required": []string{"step_id", "score", "reasoning", "evidence"},
		},
		func(ctx context.Context, args map[string]interface{}) (string, error) {
			stepID, _ := args["step_id"].(string)
			scoreFloat, _ := args["score"].(float64)
			score := int(scoreFloat)
			reasoning, _ := args["reasoning"].(string)
			evidence, _ := args["evidence"].(string)

			capturedScore = &EvaluationStepScore{
				StepID:          stepID,
				StepTitle:       step.Title,
				Score:           score,
				MaxScore:        10,
				Reasoning:       reasoning,
				Evidence:        evidence,
				SuccessCriteria: step.SuccessCriteria,
			}

			// Cancel the context to break the conversation immediately after capturing the score
			// This prevents the zero candidates error by stopping the conversation loop
			cancelToolCalled()

			return fmt.Sprintf("Score submitted: %d/10 for step %s", score, stepID), nil
		},
		"structured_output",
	)

	// Cast to concrete type and set up user message processor
	scoringAgent, ok := agent.(*WorkflowEvaluationScoringAgent)
	if !ok {
		return nil, nil, fmt.Errorf("failed to cast agent to WorkflowEvaluationScoringAgent")
	}

	// Build prompts and set user message
	userPrompt := scoringAgent.GetUserPrompt(step.ID, step.Title, step.Description, step.SuccessCriteria, executionOutput)
	scoringAgent.SetUserMessageProcessor(func(map[string]string) string {
		return userPrompt
	})

	// Execute the scoring agent with the cancellable context
	// The context will be canceled when submit_score is called, breaking the conversation loop
	_, _, err = scoringAgent.Execute(toolCalledCtx, nil, nil)

	// Check if score was captured - if so, context cancellation is expected and not an error
	if capturedScore != nil {
		// Score was captured successfully - context cancellation is expected behavior
		// Ignore cancellation errors since we got what we needed
		if err != nil && (toolCalledCtx.Err() == context.Canceled || strings.Contains(err.Error(), "context canceled") || strings.Contains(err.Error(), "context canceled")) {
			hcpo.GetLogger().Info(fmt.Sprintf("✅ Score captured successfully (context cancellation expected): %d/10 for step %s", capturedScore.Score, capturedScore.StepID))
			err = nil // Clear the cancellation error since we got the score
		}
	}

	if err != nil {
		return nil, nil, fmt.Errorf("scoring agent execution failed: %w", err)
	}

	if capturedScore == nil {
		return nil, nil, fmt.Errorf("scoring agent did not submit a score")
	}

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Evaluation scoring agent completed: %s scored %d/10", step.Title, capturedScore.Score))
	return agent, capturedScore, nil
}

// createOrchestrationLearningAgent creates an orchestration learning agent for analyzing orchestrator decisions
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
	config.ServerNames = []string{mcpclient.NoServers}

	// Code execution mode doesn't apply to learning agents
	config.UseCodeExecutionMode = false

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
