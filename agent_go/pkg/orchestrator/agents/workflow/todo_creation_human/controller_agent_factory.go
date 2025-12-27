package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	orchestrator_events "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	mcpagent "mcpagent/agent"
	baseevents "mcpagent/events"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// ============================================================================
// Phase 1: Helper Methods (Extracted for Reusability)
// ============================================================================

// setupExecutionFolderGuard sets up folder guard paths for execution agents
// Returns readPaths and writePaths for folder guard configuration
// stepID: Step ID for step-specific learnings folder access (e.g., "step-3" or branch step ID)
func (hcpo *HumanControlledTodoPlannerOrchestrator) setupExecutionFolderGuard(stepPath string, stepID string) (readPaths, writePaths []string) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	// Step-specific learnings folder: learnings/{stepID}/ (only this step's learnings, not full learnings folder)
	stepLearningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepID)
	// Knowledgebase folder: execution/knowledgebase/ (persistent files across runs)
	knowledgebasePath := getKnowledgebasePath(executionWorkspacePath)

	// Set folder guard paths:
	// READ: step-specific learnings folder + execution folder (to read previous step results) + knowledgebase folder
	// WRITE: only the specific step folder (execution/step-{X}/ or execution/step-{X}-{branch}/) + knowledgebase folder to prevent writing to other steps
	// Use getExecutionFolderPath to support both regular and branch steps
	stepFolderPath := getExecutionFolderPath(executionWorkspacePath, stepPath)
	readPaths = []string{stepLearningsPath, executionWorkspacePath, knowledgebasePath}
	writePaths = []string{stepFolderPath, knowledgebasePath}
	return readPaths, writePaths
}

// getCodeExecutionMode determines code execution mode with priority: step config > preset default
func (hcpo *HumanControlledTodoPlannerOrchestrator) getCodeExecutionMode(stepConfig *AgentConfigs) bool {
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

// getExecutionMaxTurns determines max turns with priority: step config > orchestrator default
func (hcpo *HumanControlledTodoPlannerOrchestrator) getExecutionMaxTurns(stepConfig *AgentConfigs) int {
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) resolveStepID(stepPath, stepIDOverride string, allSteps []PlanStepInterface, currentStepIndex int) string {
	stepID := stepIDOverride
	pathInfo := parseStepPath(stepPath)
	stepIndexForCheck := pathInfo.ParentStepNumber - 1 // Convert to 0-based

	if stepID == "" {
		// Try to get step ID from allSteps
		if allSteps != nil && stepIndexForCheck >= 0 && stepIndexForCheck < len(allSteps) {
			// Default: use the step's own ID
			stepID = allSteps[stepIndexForCheck].GetID()

			// Special case: decision step inner execution (step-{N}-decision)
			// In this case, learnings for execution are stored under the INNER decision step ID,
			// not the outer decision container step ID. Use DecisionStep.ID when available.
			if strings.HasSuffix(stepPath, "-decision") {
				decisionContainerStep := allSteps[currentStepIndex]
				// Check if it's a DecisionPlanStep and get the inner DecisionStep
				if decisionStep, ok := decisionContainerStep.(*DecisionPlanStep); ok && decisionStep.DecisionStep != nil {
					stepID = decisionStep.DecisionStep.GetID()
					hcpo.GetLogger().Info(fmt.Sprintf("🔍 Using inner decision step ID for learnings folder: %s (stepPath: %s)", stepID, stepPath))
				} else {
					hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ stepPath %s indicates decision inner step but DecisionStep ID not available; falling back to outer step ID: %s", stepPath, stepID))
				}
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
// Priority: tempLLM1 (attempt 1) > tempLLM2 (attempt 2) > step config > preset default > orchestrator default
// Only uses tempLLM if learnings folder has files (has existing learnings to improve upon)
func (hcpo *HumanControlledTodoPlannerOrchestrator) selectExecutionLLM(
	ctx context.Context,
	stepConfig *AgentConfigs,
	isRetryAfterValidationFailure bool,
	retryAttempt int,
	stepID string,
	stepPath string,
	learningsFolderEmpty bool,
) *orchestrator.LLMConfig {
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	shouldSkipTempOverride := isRetryAfterValidationFailure && hcpo.fallbackToOriginalLLMOnFailure

	// Cascading LLM selection based on retry attempt:
	// - retryAttempt == 1: Use tempLLM1 (if available AND learnings folder has files)
	// - retryAttempt == 2: Use tempLLM2 (if tempLLM1 was used and tempLLM2 is available AND learnings folder has files)
	// - retryAttempt >= 3: Use step LLM (step config > preset > orchestrator)
	hasTempLLM1 := hcpo.tempOverrideLLM != nil && hcpo.tempOverrideLLM.Provider != "" && hcpo.tempOverrideLLM.ModelID != ""
	hasTempLLM2 := hcpo.tempOverrideLLM2 != nil && hcpo.tempOverrideLLM2.Provider != "" && hcpo.tempOverrideLLM2.ModelID != ""

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 [DEBUG] LLM selection - retryAttempt=%d, isRetryAfterValidationFailure=%v, fallbackToOriginalLLMOnFailure=%v, shouldSkipTempOverride=%v, hasTempLLM1=%v, hasTempLLM2=%v, learningsFolderEmpty=%v", retryAttempt, isRetryAfterValidationFailure, hcpo.fallbackToOriginalLLMOnFailure, shouldSkipTempOverride, hasTempLLM1, hasTempLLM2, learningsFolderEmpty))

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
			Provider:       hcpo.tempOverrideLLM2.Provider,
			ModelID:        hcpo.tempOverrideLLM2.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for temp override
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else if !shouldSkipTempOverride && !learningsFolderEmpty && retryAttempt == 1 && hasTempLLM1 {
		// First attempt: Use tempLLM1
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using TEMPORARY OVERRIDE LLM 1 (attempt %d, learnings folder has files): %s/%s", retryAttempt, hcpo.tempOverrideLLM.Provider, hcpo.tempOverrideLLM.ModelID))
		return &orchestrator.LLMConfig{
			Provider:       hcpo.tempOverrideLLM.Provider,
			ModelID:        hcpo.tempOverrideLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for temp override
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only LLM: %s/%s", stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID))
		return &orchestrator.LLMConfig{
			Provider:       stepConfig.ExecutionLLM.Provider,
			ModelID:        stepConfig.ExecutionLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default execution-only LLM: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID))
		return &orchestrator.LLMConfig{
			Provider:       hcpo.presetExecutionLLM.Provider,
			ModelID:        hcpo.presetExecutionLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default execution-only LLM: %s/%s", orchestratorLLMConfig.Provider, orchestratorLLMConfig.ModelID))
		return orchestratorLLMConfig
	}
}

// hasEffectiveServers checks if the servers list is effectively empty
// Returns false if servers is empty or only contains NO_SERVERS (should fall back to orchestrator defaults)
// Returns true if servers contains at least one valid server name
func hasEffectiveServers(servers []string) bool {
	if len(servers) == 0 {
		return false
	}
	// Check if all servers are NO_SERVERS (effectively empty)
	for _, server := range servers {
		if server != mcpclient.NoServers {
			return true // Found at least one valid server
		}
	}
	return false // All servers are NO_SERVERS or empty
}

// applyStepConfigToAgentConfig applies step-specific configuration overrides to agent config
func (hcpo *HumanControlledTodoPlannerOrchestrator) applyStepConfigToAgentConfig(config *agents.OrchestratorAgentConfig, stepConfig *AgentConfigs, isCodeExecutionMode bool) {
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

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools))
	}
}

// prepareCustomTools filters and prepares custom tools based on step config
func (hcpo *HumanControlledTodoPlannerOrchestrator) prepareCustomTools(stepConfig *AgentConfigs) ([]llmtypes.Tool, map[string]interface{}) {
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

// addPrerequisiteDetectionTool adds prerequisite detection tool if prerequisite detection is enabled
// This must be called AFTER the agent is created, as it directly registers the tool on the mcpAgent
func (hcpo *HumanControlledTodoPlannerOrchestrator) addPrerequisiteDetectionTool(
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
// Priority: step config > preset default > orchestrator default
func (hcpo *HumanControlledTodoPlannerOrchestrator) selectValidationLLM(stepConfig *AgentConfigs) *orchestrator.LLMConfig {
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.ValidationLLM != nil && stepConfig.ValidationLLM.Provider != "" && stepConfig.ValidationLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific validation LLM: %s/%s", stepConfig.ValidationLLM.Provider, stepConfig.ValidationLLM.ModelID))
		return &orchestrator.LLMConfig{
			Provider:       stepConfig.ValidationLLM.Provider,
			ModelID:        stepConfig.ValidationLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else if hcpo.presetValidationLLM != nil && hcpo.presetValidationLLM.Provider != "" && hcpo.presetValidationLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default validation LLM: %s/%s", hcpo.presetValidationLLM.Provider, hcpo.presetValidationLLM.ModelID))
		return &orchestrator.LLMConfig{
			Provider:       hcpo.presetValidationLLM.Provider,
			ModelID:        hcpo.presetValidationLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default validation LLM: %s/%s", orchestratorLLMConfig.Provider, orchestratorLLMConfig.ModelID))
		return orchestratorLLMConfig
	}
}

// getValidationMaxTurns determines max turns for validation agents
// Fixed to 25 (not configurable)
func (hcpo *HumanControlledTodoPlannerOrchestrator) getValidationMaxTurns(stepConfig *AgentConfigs) int {
	return 25
}

// setupValidationFolderGuard sets up folder guard paths for validation agents (read-only)
func (hcpo *HumanControlledTodoPlannerOrchestrator) setupValidationFolderGuard() (readPaths, writePaths []string) {
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
	readPaths = []string{executionPath}
	writePaths = []string{} // No write permissions - validation agent only reads and returns structured JSON
	return readPaths, writePaths
}

// setupConditionalFolderGuard sets up folder guard paths for conditional agents
// Returns readPaths and writePaths for folder guard configuration
// stepPath: Step path identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// stepID: Step ID for step-specific learnings folder access (e.g., "step-3" or branch step ID)
func (hcpo *HumanControlledTodoPlannerOrchestrator) setupConditionalFolderGuard(stepPath string, stepID string) (readPaths, writePaths []string) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	// Step-specific learnings folder: learnings/{stepID}/ (only this step's learnings, not full learnings folder)
	stepLearningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepID)
	// Step-specific execution folder: execution/step-{X}/ or execution/step-{X}-{branch}/ (for writing evaluation results)
	stepFolderPath := getExecutionFolderPath(executionWorkspacePath, stepPath)
	// Knowledgebase folder: execution/knowledgebase/ (persistent files across runs)
	knowledgebasePath := getKnowledgebasePath(executionWorkspacePath)

	// Set folder guard paths:
	// READ: step-specific learnings folder + entire execution folder (to read all previous step results and verify conditions) + knowledgebase folder
	// WRITE: step-specific execution folder (to write evaluation results and intermediate files) + knowledgebase folder
	readPaths = []string{stepLearningsPath, executionWorkspacePath, knowledgebasePath}
	writePaths = []string{stepFolderPath, knowledgebasePath}
	return readPaths, writePaths
}

// setupLearningFolderGuard sets up folder guard paths for learning agents
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
func (hcpo *HumanControlledTodoPlannerOrchestrator) setupLearningFolderGuard(learningPathIdentifier string) (readPaths, writePaths []string) {
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionPath := fmt.Sprintf("%s/execution", runWorkspacePath)

	// Step-specific learnings: write to learnings/{learningPathIdentifier} at workspace root (not inside runs/)
	// Supports both regular steps (step-{X}) and branch steps (step-{X}-{true/false}-{Y})
	learningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, learningPathIdentifier)

	// Build read paths: execution path + base learnings path (for reading existing learnings)
	readPaths = []string{executionPath}
	// Add base learnings path for reading existing learnings (we read from base but write to step folder)
	baseLearningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)
	readPaths = append(readPaths, baseLearningsPath)

	writePaths = []string{learningsPath}
	return readPaths, writePaths
}

// getLearningMaxTurns determines max turns for learning agents
// Fixed to 25 (not configurable)
func (hcpo *HumanControlledTodoPlannerOrchestrator) getLearningMaxTurns(stepConfig *AgentConfigs) int {
	return 25
}

// selectLearningLLM selects the LLM config for learning agents
// Priority: step config > preset default > orchestrator default
// Note: Temporary override only applies to execution agents, not learning agents
func (hcpo *HumanControlledTodoPlannerOrchestrator) selectLearningLLM(stepConfig *AgentConfigs) *orchestrator.LLMConfig {
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific learning LLM: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID))
		return &orchestrator.LLMConfig{
			Provider:       stepConfig.LearningLLM.Provider,
			ModelID:        stepConfig.LearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default learning LLM: %s/%s", hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID))
		return &orchestrator.LLMConfig{
			Provider:       hcpo.presetLearningLLM.Provider,
			ModelID:        hcpo.presetLearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default learning LLM: %s/%s", orchestratorLLMConfig.Provider, orchestratorLLMConfig.ModelID))
		return orchestratorLLMConfig
	}
}

// applyPostSetupToAgent applies post-setup configuration to an agent after base factory setup
// This includes setting folder guard paths and optionally updating the code execution registry
// agent: The orchestrator agent to configure
// agentName: Name of the agent (for logging)
// shouldUpdateRegistry: If true, updates the code execution registry after setting folder guard paths
func (hcpo *HumanControlledTodoPlannerOrchestrator) applyPostSetupToAgent(agent agents.OrchestratorAgent, agentName string, shouldUpdateRegistry bool) error {
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) createExecutionOnlyAgent(ctx context.Context, phase string, stepPath string, agentName string, stepConfig *AgentConfigs, isRetryAfterValidationFailure bool, retryAttempt int, prerequisiteInfo *PrerequisiteInfo, allSteps []PlanStepInterface, currentStepIndex int, cancelFunc context.CancelFunc, prereqErrChan chan<- *PrerequisiteFailureError, stepIDOverride string) (agents.OrchestratorAgent, error) {
	// 1. Resolve stepID first (needed for folder guard setup)
	stepID := hcpo.resolveStepID(stepPath, stepIDOverride, allSteps, currentStepIndex)

	// 2. Setup folder guard (extracted method) - uses step-specific learnings folder
	readPaths, writePaths := hcpo.setupExecutionFolderGuard(stepPath, stepID)
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for execution-only agent - Read paths: %v, Write paths: %v (can read learnings/%s/ and execution/, can only write to %s)", readPaths, writePaths, stepID, stepPath))

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

	// 4. Create config
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Apply step-specific overrides
	hcpo.applyStepConfigToAgentConfig(config, stepConfig, isCodeExecutionMode)

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
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerExecutionOnlyAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister,
		executorsToUse,
		false, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup execution-only agent: %v", err)
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

	// Apply post-setup configuration (folder guard paths and optional registry update)
	if err := hcpo.applyPostSetupToAgent(agent, agentName, isCodeExecutionMode); err != nil {
		// Log warning but don't fail agent creation
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Post-setup configuration failed for %s: %v", agentName, err))
	}

	return agent, nil
}

// createValidationAgent creates a validation agent for the current iteration
func (hcpo *HumanControlledTodoPlannerOrchestrator) createValidationAgent(ctx context.Context, phase string, step int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	// 1. Setup folder guard (read-only for validation agents)
	readPaths, writePaths := hcpo.setupValidationFolderGuard()
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for validation agent - Read paths: %v, Write paths: %v (read-only, no file writes)", readPaths, writePaths))

	// 2. Determine settings (extracted methods)
	maxTurns := hcpo.getValidationMaxTurns(stepConfig)
	llmConfig := hcpo.selectValidationLLM(stepConfig)

	// 3. Create config
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Validation agents always use NoServers (pure LLM validation agent)
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM validation agent

	// Code execution mode only applies to execution agents, not validation agents
	config.UseCodeExecutionMode = false
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling code execution mode for validation agent (only execution agents use MCP tools)"))

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools))
	}

	// 4. Prepare custom tools (filtered by step config)
	toolsToRegister, executorsToUse := hcpo.prepareCustomTools(stepConfig)

	// 5. Use base factory! (This handles all setup automatically)
	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		step,
		0, // iteration
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerValidationAgent(cfg, logger, tracer, eventBridge)
		},
		toolsToRegister,
		executorsToUse,
		false, // overwriteSystemPrompt
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create and setup validation agent: %v", err)
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) createLearningAgentInternal(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, isCodeExecutionMode bool) (agents.OrchestratorAgent, error) {
	// 1. Setup folder guard (extracted method)
	readPaths, writePaths := hcpo.setupLearningFolderGuard(learningPathIdentifier)
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	agentType := "learning agent"
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for %s - Read paths: %v, Write paths: %v", agentType, readPaths, writePaths))

	// Use the provided step-specific code execution mode (already computed with step-level priority) to ensure consistency
	wasCodeExecutionMode := isCodeExecutionMode
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 %s using step-specific code execution mode: %v (matches execution agent)", agentType, wasCodeExecutionMode))

	// 2. Determine settings (extracted methods)
	maxTurns := hcpo.getLearningMaxTurns(stepConfig)
	// Use learning LLM config - Priority: step config > preset default > orchestrator default
	llmConfig := hcpo.selectLearningLLM(stepConfig)

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
	config.EnableLargeOutputVirtualTools = &disabled
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling large output virtual tools (context offloading) for %s", agentType))

	// 4. Prepare custom tools (filtered by step config)
	toolsToRegister, executorsToUse := hcpo.prepareCustomTools(stepConfig)

	// 5. Use base factory! (This handles all setup automatically)
	// Extract step number from learningPathIdentifier for event bridge context
	pathInfo := parseStepPath(learningPathIdentifier)
	stepNumberForContext := pathInfo.ParentStepNumber - 1 // Convert to 0-based for SetOrchestratorContext

	// Create agent factory function based on code execution mode
	var createAgentFunc func(*agents.OrchestratorAgentConfig, loggerv2.Logger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent
	if wasCodeExecutionMode {
		createAgentFunc = func(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerCodeExecutionLearningAgent(config, logger, tracer, eventBridge)
		}
	} else {
		createAgentFunc = func(config *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerLearningAgent(config, logger, tracer, eventBridge)
		}
	}

	agent, err := hcpo.CreateAndSetupStandardAgentWithConfig(
		ctx,
		config,
		phase,
		stepNumberForContext,
		0, // iteration (not used for learning agents)
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
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) createLearningAgent(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, isCodeExecutionMode bool) (agents.OrchestratorAgent, error) {
	return hcpo.createLearningAgentInternal(ctx, phase, learningPathIdentifier, agentName, stepConfig, isCodeExecutionMode)
}

// Note: Learning integration functions removed - execution agent now auto-discovers learning files and scripts

// createSuccessLearningAgent is a backward compatibility wrapper for createLearningAgent
// Deprecated: Use createLearningAgent instead. The unified learning agent handles both success and failure cases.
func (hcpo *HumanControlledTodoPlannerOrchestrator) createSuccessLearningAgent(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, isCodeExecutionMode bool) (agents.OrchestratorAgent, error) {
	return hcpo.createLearningAgent(ctx, phase, learningPathIdentifier, agentName, stepConfig, isCodeExecutionMode)
}

// createFailureLearningAgent is a backward compatibility wrapper for createLearningAgent
// Deprecated: Use createLearningAgent instead. The unified learning agent handles both success and failure cases.
func (hcpo *HumanControlledTodoPlannerOrchestrator) createFailureLearningAgent(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, isCodeExecutionMode bool) (agents.OrchestratorAgent, error) {
	return hcpo.createLearningAgent(ctx, phase, learningPathIdentifier, agentName, stepConfig, isCodeExecutionMode)
}

// createConditionalAgent creates a conditional agent using the standard factory pattern
// This ensures proper event bridge connection, context setup, and tool registration
// stepPath: Step path identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// stepID: Step ID for step-specific learnings folder access (e.g., "step-3" or branch step ID)
func (hcpo *HumanControlledTodoPlannerOrchestrator) createConditionalAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs, conditionalLLMConfig *orchestrator.LLMConfig, stepPath string, stepID string) (agents.OrchestratorAgent, error) {
	// 1. Setup folder guard (similar to execution agent) - uses step-specific learnings folder and execution folder
	readPaths, writePaths := hcpo.setupConditionalFolderGuard(stepPath, stepID)
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for conditional agent - Read paths: %v, Write paths: %v (can read learnings/%s/ and execution/, can write to %s)", readPaths, writePaths, stepID, stepPath))

	// Determine max turns: use orchestrator default (conditional agents don't have step-specific max turns config)
	maxTurns := hcpo.GetMaxTurns()
	// Note: ConditionalMaxTurns doesn't exist in AgentConfigs - using orchestrator default

	// Determine LLM config: Priority: step execution_llm > preset execution_llm > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		// Use step-specific execution LLM config
		executionLLMConfig := stepConfig.ExecutionLLM
		llmConfig = &orchestrator.LLMConfig{
			Provider:       executionLLMConfig.Provider,
			ModelID:        executionLLMConfig.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution LLM for conditional agent: %s/%s", executionLLMConfig.Provider, executionLLMConfig.ModelID))
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		// Use preset execution LLM as fallback
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetExecutionLLM.Provider,
			ModelID:        hcpo.presetExecutionLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default execution LLM for conditional agent: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID))
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default conditional LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID))
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

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools))
	}

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
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerConditionalAgent(cfg, logger, tracer, eventBridge)
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
func (hcpo *HumanControlledTodoPlannerOrchestrator) createOrchestrationOrchestratorAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs, orchestrationLLMConfig *orchestrator.LLMConfig) (agents.OrchestratorAgent, error) {
	// Orchestration orchestrator agent needs folder guard (can write files)
	// Note: Folder guard is set by caller in controller_orchestration.go before agent creation
	// We apply it to the agent here via post-setup

	// Determine max turns: use orchestrator default
	maxTurns := hcpo.GetMaxTurns()

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if orchestrationLLMConfig != nil && orchestrationLLMConfig.Provider != "" && orchestrationLLMConfig.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       orchestrationLLMConfig.Provider,
			ModelID:        orchestrationLLMConfig.ModelID,
			FallbackModels: []string{},
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific orchestration orchestrator LLM: %s/%s", orchestrationLLMConfig.Provider, orchestrationLLMConfig.ModelID))
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default orchestration orchestrator LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID))
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

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools))
	}

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
		func(cfg *agents.OrchestratorAgentConfig, logger loggerv2.Logger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) agents.OrchestratorAgent {
			return NewHumanControlledTodoPlannerOrchestrationOrchestratorAgent(cfg, logger, tracer, eventBridge)
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

// Execute implements the Orchestrator interface
