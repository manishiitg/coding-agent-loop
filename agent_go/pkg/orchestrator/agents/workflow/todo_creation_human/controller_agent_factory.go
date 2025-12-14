package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/events"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/mcpclient"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// createExecutionOnlyAgent creates an execution-only agent that receives pre-discovered learning history
// isRetryAfterValidationFailure: if true and fallbackToOriginalLLMOnFailure is enabled, will skip tempOverrideLLM and use original LLM
// stepPath: Step path identifier (e.g., "step-1" for regular steps, "step-3-if-true-0" for branch steps)
// retryAttempt: current retry attempt number (1 = first attempt, 2 = second attempt, etc.) - used for cascading LLM fallback
// prerequisiteInfo: Prerequisite information for this step (nil if prerequisite detection is disabled)
// allSteps: All steps in the plan (required if prerequisiteInfo is not nil)
// currentStepIndex: 0-based index of current step (required if prerequisiteInfo is not nil)
func (hcpo *HumanControlledTodoPlannerOrchestrator) createExecutionOnlyAgent(ctx context.Context, phase string, stepPath string, agentName string, stepConfig *AgentConfigs, isRetryAfterValidationFailure bool, retryAttempt int, prerequisiteInfo *PrerequisiteInfo, allSteps []TodoStep, currentStepIndex int, cancelFunc context.CancelFunc, prereqErrChan chan<- *PrerequisiteFailureError) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from learnings (read-only) and execution (via writePaths), writes only to execution
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionWorkspacePath := fmt.Sprintf("%s/execution", runWorkspacePath)
	// Determine code execution mode: Priority: step config > preset default
	var isCodeExecutionMode bool
	if stepConfig != nil && stepConfig.UseCodeExecutionMode != nil {
		isCodeExecutionMode = *stepConfig.UseCodeExecutionMode
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific code execution mode: %v", isCodeExecutionMode))
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset code execution mode: %v", isCodeExecutionMode))
	}
	// Always use learnings folder (unified folder for all learning types)
	learningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)

	// Set folder guard paths:
	// READ: learnings folder + execution folder (to read previous step results)
	// WRITE: only the specific step folder (execution/step-{X}/ or execution/step-{X}-{branch}/) to prevent writing to other steps
	// Use getExecutionFolderPath to support both regular and branch steps
	stepFolderPath := getExecutionFolderPath(executionWorkspacePath, stepPath)
	readPaths := []string{learningsPath, executionWorkspacePath}
	writePaths := []string{stepFolderPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for execution-only agent - Read paths: %v, Write paths: %v (can read learnings/ and execution/, can only write to %s)", readPaths, writePaths, stepPath))

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.ExecutionMaxTurns != nil {
		maxTurns = *stepConfig.ExecutionMaxTurns
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only max turns: %d (orchestrator default was: %d)", maxTurns, hcpo.GetMaxTurns()))
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default execution-only max turns: %d (no step-specific config)", maxTurns))
	}

	// Determine LLM config with cascading fallback: tempLLM1 → tempLLM2 → step LLM
	// Priority: tempLLM1 (attempt 1) > tempLLM2 (attempt 2) > step config > preset default > orchestrator default
	// Exception: If retrying after validation failure and fallbackToOriginalLLMOnFailure is enabled, skip temp overrides
	// NEW: Only use tempLLM if step learnings folder has files (has existing learnings to improve upon)
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	shouldSkipTempOverride := isRetryAfterValidationFailure && hcpo.fallbackToOriginalLLMOnFailure

	// Parse stepPath to extract step number for learnings folder check
	// For branch steps, we'll check the parent step's learnings folder (for tempLLM logic)
	pathInfo := parseStepPath(stepPath)
	stepNumber := pathInfo.ParentStepNumber // Use parent step number (works for both regular and branch steps)
	learningsFolderEmpty, err := hcpo.isStepLearningsFolderEmpty(ctx, stepNumber)
	if err != nil {
		hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to check if step %d learnings folder is empty: %v, assuming empty (will skip tempLLM)", stepNumber, err))
		learningsFolderEmpty = true // Conservative: assume empty on error, skip tempLLM
	}

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
			stepId := ""
			if stepConfig != nil {
				// Try to get step info from stepConfig if available
				// Note: stepConfig doesn't directly have title/ID, but we can construct basic info
			}
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

			stepLearningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, stepPath)
			tempLLMSkippedEvent := &events.TempLLMSkippedEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
					Component: "orchestrator",
				},
				StepID:          stepId,
				StepIndex:       stepNumber - 1, // Convert to 0-based for StepIndex
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
			eventBridge.HandleEvent(ctx, &events.AgentEvent{
				Type:      events.TempLLMSkipped,
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
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.tempOverrideLLM2.Provider,
			ModelID:        hcpo.tempOverrideLLM2.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for temp override
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using TEMPORARY OVERRIDE LLM 2 (attempt %d, learnings folder has files): %s/%s", retryAttempt, hcpo.tempOverrideLLM2.Provider, hcpo.tempOverrideLLM2.ModelID))
	} else if !shouldSkipTempOverride && !learningsFolderEmpty && retryAttempt == 1 && hasTempLLM1 {
		// First attempt: Use tempLLM1
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.tempOverrideLLM.Provider,
			ModelID:        hcpo.tempOverrideLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for temp override
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using TEMPORARY OVERRIDE LLM 1 (attempt %d, learnings folder has files): %s/%s", retryAttempt, hcpo.tempOverrideLLM.Provider, hcpo.tempOverrideLLM.ModelID))
	} else if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.ExecutionLLM.Provider,
			ModelID:        stepConfig.ExecutionLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only LLM: %s/%s", stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID))
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetExecutionLLM.Provider,
			ModelID:        hcpo.presetExecutionLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default execution-only LLM: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID))
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default execution-only LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID))
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Use step-specific servers/tools if provided, otherwise use orchestrator defaults
	if stepConfig != nil && len(stepConfig.SelectedServers) > 0 {
		config.ServerNames = stepConfig.SelectedServers
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only servers: %v", stepConfig.SelectedServers))
	} else if stepConfig != nil {
		// Log when stepConfig exists but SelectedServers is empty (will use orchestrator defaults)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but no SelectedServers specified - using orchestrator defaults"))
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		config.SelectedTools = stepConfig.SelectedTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific execution-only tools: %v", stepConfig.SelectedTools))
	} else if stepConfig != nil {
		// Log when stepConfig exists but SelectedTools is empty (will use orchestrator defaults)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Step config found but no SelectedTools specified - using orchestrator defaults"))
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

	// Create agent using execution-only factory function
	agent := NewHumanControlledTodoPlannerExecutionOnlyAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to initialize execution-only agent: %w", err), nil)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf(fmt.Sprintf("context-aware event bridge is nil for %s", agentName), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Checking agent structure for %s", agentName))
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf(fmt.Sprintf("base agent is nil for %s", agentName), nil)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf(fmt.Sprintf("MCP agent is nil for %s", agentName), nil)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		// Extract step number from stepPath for SetOrchestratorContext (which expects numeric step)
		pathInfo := parseStepPath(stepPath)
		stepNumberForContext := pathInfo.ParentStepNumber - 1 // Convert to 0-based for SetOrchestratorContext
		cab.SetOrchestratorContext(phase, stepNumberForContext, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Info(fmt.Sprintf("🔗 Context-aware bridge connected to %s (%s, agent %s)", phase, stepPath, baseAgentName))
	} else {
		return nil, fmt.Errorf(fmt.Sprintf("context-aware bridge type mismatch for %s", agentName), nil)
	}

	// Register custom tools - filter by enabled categories and/or specific tools if specified
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && (len(stepConfig.EnabledCustomToolCategories) > 0 || len(stepConfig.EnabledCustomTools) > 0) {
		// Convert old format (categories + tools) to new unified format (category:tool or category:*)
		unifiedEnabledTools := orchestrator.ConvertOldFormatToNewFormat(
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		// Filter tools based on unified format
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			unifiedEnabledTools,
		)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools))
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		// Wrap executors and enhance tool descriptions with folder guard (automatic)
		toolsToRegister, wrappedExecutors := hcpo.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode()))

		for _, tool := range toolsToRegister {
			if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to convert parameters for tool %s", tool.Function.Name))
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from ToolCategories map - REQUIRED
					var toolCategory string
					if hcpo.ToolCategories != nil {
						if cat, exists := hcpo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
						} else {
							hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name), nil)
							continue // Skip this tool
						}
					} else {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name), nil)
						continue // Skip this tool
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Failed to register tool %s: %v", tool.Function.Name, err), nil)
						continue // Skip this tool
					}
				} else {
					hcpo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to convert executor for tool %s", tool.Function.Name))
				}
			}
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode()))

		// Register prerequisite detection tool if prerequisite detection is enabled
		if prerequisiteInfo != nil && len(prerequisiteInfo.PrerequisiteRules) > 0 {
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
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ Registered prerequisite detection tool for %s agent", agentName))
			}
		}

		// Set folder guard paths on MCP agent (required for both code execution mode and simple mode)
		// This ensures path validation works at the tool executor level
		readPaths, writePaths := hcpo.GetFolderGuardPaths()
		mcpAgent.SetFolderGuardPaths(readPaths, writePaths)
		hcpo.GetLogger().Info(fmt.Sprintf("🔒 Folder guard paths set for %s agent - Read: %v, Write: %v", agentName, readPaths, writePaths))

		// Update code execution registry with wrapped executors for folder guard to work (code execution mode only)
		if isCodeExecutionMode {
			// CRITICAL: Folder guard paths already set above
			// The registry generation uses these paths to create the path validation code
			// This ensures LLM-generated Go code can only access paths within allowed boundaries
			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update code execution registry for %s: %v", agentName, err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ [CODE_EXECUTION] Registry updated for %s agent - folder guard enabled", agentName))
			}
		}
	}

	return agent, nil
}

// createValidationAgent creates a validation agent for the current iteration
func (hcpo *HumanControlledTodoPlannerOrchestrator) createValidationAgent(ctx context.Context, phase string, step int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from execution (read-only), no write permissions
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
	readPaths := []string{executionPath}
	writePaths := []string{} // No write permissions - validation agent only reads and returns structured JSON
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for validation agent - Read paths: %v, Write paths: %v (read-only, no file writes)", readPaths, writePaths))

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.ValidationMaxTurns != nil {
		maxTurns = *stepConfig.ValidationMaxTurns
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific validation max turns: %d", maxTurns))
	}

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	// Note: Temporary override only applies to execution agents, not validation agents
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.ValidationLLM != nil && stepConfig.ValidationLLM.Provider != "" && stepConfig.ValidationLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.ValidationLLM.Provider,
			ModelID:        stepConfig.ValidationLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific validation LLM: %s/%s", stepConfig.ValidationLLM.Provider, stepConfig.ValidationLLM.ModelID))
	} else if hcpo.presetValidationLLM != nil && hcpo.presetValidationLLM.Provider != "" && hcpo.presetValidationLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetValidationLLM.Provider,
			ModelID:        hcpo.presetValidationLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default validation LLM: %s/%s", hcpo.presetValidationLLM.Provider, hcpo.presetValidationLLM.ModelID))
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default validation LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID))
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Validation agents always use NoServers (pure LLM validation agent)
	// Step-specific server/tool selection is only for execution agents
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM validation agent

	// Code execution mode only applies to execution agents, not validation agents
	config.UseCodeExecutionMode = false
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling code execution mode for validation agent (only execution agents use MCP tools)"))

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools))
	}

	// Create agent using provided factory function
	agent := NewHumanControlledTodoPlannerValidationAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from CreateAndSetupStandardAgent)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to initialize validation agent: %w", err), nil)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf(fmt.Sprintf("context-aware event bridge is nil for %s", agentName), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Checking agent structure for %s", agentName))
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf(fmt.Sprintf("base agent is nil for %s", agentName), nil)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf(fmt.Sprintf("MCP agent is nil for %s", agentName), nil)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Info(fmt.Sprintf("🔗 Context-aware bridge connected to %s (step %d, agent %s)", phase, step+1, baseAgentName))
	} else {
		return nil, fmt.Errorf(fmt.Sprintf("context-aware bridge type mismatch for %s", agentName), nil)
	}

	// Register custom tools - filter by enabled categories and/or specific tools if specified
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && (len(stepConfig.EnabledCustomToolCategories) > 0 || len(stepConfig.EnabledCustomTools) > 0) {
		// Convert old format (categories + tools) to new unified format (category:tool or category:*)
		unifiedEnabledTools := orchestrator.ConvertOldFormatToNewFormat(
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		// Filter tools based on unified format
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			unifiedEnabledTools,
		)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools))
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		// Wrap executors and enhance tool descriptions with folder guard (automatic)
		toolsToRegister, wrappedExecutors := hcpo.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode()))

		for _, tool := range toolsToRegister {
			if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to convert parameters for tool %s", tool.Function.Name))
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from ToolCategories map - REQUIRED
					var toolCategory string
					if hcpo.ToolCategories != nil {
						if cat, exists := hcpo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
						} else {
							hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name), nil)
							continue // Skip this tool
						}
					} else {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name), nil)
						continue // Skip this tool
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Failed to register tool %s: %v", tool.Function.Name, err), nil)
						continue // Skip this tool
					}
				} else {
					hcpo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to convert executor for tool %s", tool.Function.Name))
				}
			}
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode()))

		// Update code execution registry with wrapped executors for folder guard to work
		if hcpo.GetUseCodeExecutionMode() {
			// CRITICAL: Set folder guard paths BEFORE updating code execution registry
			// The registry generation uses these paths to create the path validation code
			// This ensures LLM-generated Go code can only access paths within allowed boundaries
			readPaths, writePaths := hcpo.GetFolderGuardPaths()
			mcpAgent.SetFolderGuardPaths(readPaths, writePaths)
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 [CODE_EXECUTION] Folder guard paths set for %s agent - Read: %v, Write: %v", agentName, readPaths, writePaths))

			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update code execution registry for %s: %v", agentName, err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ [CODE_EXECUTION] Registry updated for %s agent - folder guard enabled", agentName))
			}
		}
	}

	return agent, nil
}

// Note: Learning integration functions removed - execution agent now auto-discovers learning files and scripts

// createSuccessLearningAgent creates a success learning agent for analyzing successful executions
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) createSuccessLearningAgent(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, isCodeExecutionMode bool) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from execution and learnings (read-only), writes only to learnings
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionPath := fmt.Sprintf("%s/execution", runWorkspacePath)

	// Use the provided step-specific code execution mode (already computed with step-level priority) to ensure consistency
	wasCodeExecutionMode := isCodeExecutionMode
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Success learning agent using step-specific code execution mode: %v (matches execution agent)", wasCodeExecutionMode))

	// Step-specific learnings: write to learnings/{learningPathIdentifier} at workspace root (not inside runs/)
	// Supports both regular steps (step-{X}) and branch steps (step-{X}-{true/false}-{Y})
	learningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, learningPathIdentifier)
	hcpo.GetLogger().Info(fmt.Sprintf("📁 Step-specific learnings - writing to step folder: %s", learningsPath))

	// Build read paths: execution path + base learnings path (for reading existing learnings)
	readPaths := []string{executionPath}
	// Add base learnings path for reading existing learnings (we read from base but write to step folder)
	baseLearningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)
	readPaths = append(readPaths, baseLearningsPath)
	hcpo.GetLogger().Info(fmt.Sprintf("📁 Step-specific learnings: reading from base folder %s, writing to step folder %s", baseLearningsPath, learningsPath))

	writePaths := []string{learningsPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for success learning agent - Read paths: %v, Write paths: %v", readPaths, writePaths))

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.LearningMaxTurns != nil {
		maxTurns = *stepConfig.LearningMaxTurns
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific learning max turns: %d", maxTurns))
	}

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	// Note: Temporary override only applies to execution agents, not learning agents
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.LearningLLM.Provider,
			ModelID:        stepConfig.LearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific learning LLM: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID))
	} else if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetLearningLLM.Provider,
			ModelID:        hcpo.presetLearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default learning LLM: %s/%s", hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID))
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default learning LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID))
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Learning agents always use NoServers (pure LLM analysis agent)
	// Step-specific server/tool selection is only for execution agents
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM analysis agent

	// Code execution mode only applies to execution agents, not learning agents
	// CRITICAL: Override orchestrator-level code execution mode setting - learning agents are pure LLM analysis agents
	originalCodeExecMode := config.UseCodeExecutionMode
	config.UseCodeExecutionMode = false
	if wasCodeExecutionMode {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Execution was in code execution mode - using code execution learning agent (but agent itself does NOT use code execution mode)"))
		if originalCodeExecMode {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Code execution mode was enabled in config but disabled for learning agent (original: %v, new: %v)", originalCodeExecMode, config.UseCodeExecutionMode))
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling code execution mode for success learning agent (only execution agents use MCP tools)"))
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Learning agent code execution mode: %v (NoServers=%v, will be auto-disabled if needed)", config.UseCodeExecutionMode, config.ServerNames[0] == mcpclient.NoServers))

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools))
	}

	// Create agent using appropriate factory function based on code execution mode
	var agent agents.OrchestratorAgent
	if wasCodeExecutionMode {
		agent = NewHumanControlledTodoPlannerCodeExecutionLearningAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())
	} else {
		agent = NewHumanControlledTodoPlannerSuccessLearningAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())
	}

	// Initialize and setup agent (inlined from CreateAndSetupStandardAgentWithCustomServers)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to initialize success learning agent: %w", err), nil)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf(fmt.Sprintf("context-aware event bridge is nil for %s", agentName), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Checking agent structure for %s", agentName))
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf(fmt.Sprintf("base agent is nil for %s", agentName), nil)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf(fmt.Sprintf("MCP agent is nil for %s", agentName), nil)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		// Extract step number from learningPathIdentifier for SetOrchestratorContext (which expects numeric step)
		pathInfo := parseStepPath(learningPathIdentifier)
		stepNumberForContext := pathInfo.ParentStepNumber - 1 // Convert to 0-based for SetOrchestratorContext
		cab.SetOrchestratorContext(phase, stepNumberForContext, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Info(fmt.Sprintf("🔗 Context-aware bridge connected to %s (%s, agent %s)", phase, learningPathIdentifier, baseAgentName))
	} else {
		return nil, fmt.Errorf(fmt.Sprintf("context-aware bridge type mismatch for %s", agentName), nil)
	}

	// Register custom tools - filter by enabled categories and/or specific tools if specified
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && (len(stepConfig.EnabledCustomToolCategories) > 0 || len(stepConfig.EnabledCustomTools) > 0) {
		// Convert old format (categories + tools) to new unified format (category:tool or category:*)
		unifiedEnabledTools := orchestrator.ConvertOldFormatToNewFormat(
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		// Filter tools based on unified format
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			unifiedEnabledTools,
		)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools))
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		// Wrap executors and enhance tool descriptions with folder guard (automatic)
		toolsToRegister, wrappedExecutors := hcpo.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode()))

		for _, tool := range toolsToRegister {
			if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to convert parameters for tool %s", tool.Function.Name))
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from ToolCategories map - REQUIRED
					var toolCategory string
					if hcpo.ToolCategories != nil {
						if cat, exists := hcpo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
						} else {
							hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name), nil)
							continue // Skip this tool
						}
					} else {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name), nil)
						continue // Skip this tool
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Failed to register tool %s: %v", tool.Function.Name, err), nil)
						continue // Skip this tool
					}
				} else {
					hcpo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to convert executor for tool %s", tool.Function.Name))
				}
			}
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode()))

		// Update code execution registry with wrapped executors for folder guard to work
		if hcpo.GetUseCodeExecutionMode() {
			// CRITICAL: Set folder guard paths BEFORE updating code execution registry
			// The registry generation uses these paths to create the path validation code
			// This ensures LLM-generated Go code can only access paths within allowed boundaries
			readPaths, writePaths := hcpo.GetFolderGuardPaths()
			mcpAgent.SetFolderGuardPaths(readPaths, writePaths)
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 [CODE_EXECUTION] Folder guard paths set for %s agent - Read: %v, Write: %v", agentName, readPaths, writePaths))

			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update code execution registry for %s: %v", agentName, err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ [CODE_EXECUTION] Registry updated for %s agent - folder guard enabled", agentName))
			}
		}
	}

	return agent, nil
}

// createFailureLearningAgent creates a failure learning agent for analyzing failed executions
// Note: This now uses the unified learning agent which handles both success and failure cases
// learningPathIdentifier: Learning folder identifier (e.g., "step-3" for regular steps, "step-3-true-0" for branch steps)
// isCodeExecutionMode: The step-specific code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
func (hcpo *HumanControlledTodoPlannerOrchestrator) createFailureLearningAgent(ctx context.Context, phase string, learningPathIdentifier string, agentName string, stepConfig *AgentConfigs, isCodeExecutionMode bool) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from execution and learnings (read-only), writes only to learnings
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use run folder if available, otherwise use base workspace (backward compatibility)
	var runWorkspacePath string
	if hcpo.selectedRunFolder != "" {
		runWorkspacePath = fmt.Sprintf("%s/runs/%s", baseWorkspacePath, hcpo.selectedRunFolder)
	} else {
		runWorkspacePath = baseWorkspacePath
	}
	executionPath := fmt.Sprintf("%s/execution", runWorkspacePath)

	// Use the provided step-specific code execution mode (already computed with step-level priority) to ensure consistency
	wasCodeExecutionMode := isCodeExecutionMode
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Failure learning agent using step-specific code execution mode: %v (matches execution agent)", wasCodeExecutionMode))

	// Step-specific learnings: write to learnings/{learningPathIdentifier} at workspace root (not inside runs/)
	// Supports both regular steps (step-{X}) and branch steps (step-{X}-{true/false}-{Y})
	learningsPath := fmt.Sprintf("%s/learnings/%s", baseWorkspacePath, learningPathIdentifier)
	hcpo.GetLogger().Info(fmt.Sprintf("📁 Step-specific learnings - writing to step folder: %s", learningsPath))

	// Build read paths: execution path + base learnings path (for reading existing learnings)
	readPaths := []string{executionPath}
	// Add base learnings path for reading existing learnings (we read from base but write to step folder)
	baseLearningsPath := fmt.Sprintf("%s/learnings", baseWorkspacePath)
	readPaths = append(readPaths, baseLearningsPath)
	hcpo.GetLogger().Info(fmt.Sprintf("📁 Step-specific learnings: reading from base folder %s, writing to step folder %s", baseLearningsPath, learningsPath))

	writePaths := []string{learningsPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Info(fmt.Sprintf("🔒 Setting folder guard for failure learning agent - Read paths: %v, Write paths: %v", readPaths, writePaths))

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.LearningMaxTurns != nil {
		maxTurns = *stepConfig.LearningMaxTurns
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific learning max turns: %d", maxTurns))
	}

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	// Note: Temporary override only applies to execution agents, not learning agents
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.LearningLLM.Provider,
			ModelID:        stepConfig.LearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific learning LLM: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID))
	} else if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetLearningLLM.Provider,
			ModelID:        hcpo.presetLearningLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default learning LLM: %s/%s", hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID))
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default learning LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID))
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Learning agents always use NoServers (pure LLM analysis agent)
	// Step-specific server/tool selection is only for execution agents
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM analysis agent

	// Code execution mode only applies to execution agents, not learning agents
	// CRITICAL: Override orchestrator-level code execution mode setting - learning agents are pure LLM analysis agents
	originalCodeExecMode := config.UseCodeExecutionMode
	config.UseCodeExecutionMode = false
	if wasCodeExecutionMode {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Execution was in code execution mode - using code execution learning agent (but agent itself does NOT use code execution mode)"))
		if originalCodeExecMode {
			hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Code execution mode was enabled in config but disabled for learning agent (original: %v, new: %v)", originalCodeExecMode, config.UseCodeExecutionMode))
		}
	} else {
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Disabling code execution mode for failure learning agent (only execution agents use MCP tools)"))
	}
	hcpo.GetLogger().Info(fmt.Sprintf("🔧 Learning agent code execution mode: %v (NoServers=%v, will be auto-disabled if needed)", config.UseCodeExecutionMode, config.ServerNames[0] == mcpclient.NoServers))

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools))
	}

	// Create agent using appropriate factory function based on code execution mode
	var agent agents.OrchestratorAgent
	if wasCodeExecutionMode {
		agent = NewHumanControlledTodoPlannerCodeExecutionLearningAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())
	} else {
		agent = NewHumanControlledTodoPlannerLearningAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())
	}

	// Initialize and setup agent (inlined from CreateAndSetupStandardAgentWithCustomServers)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf(fmt.Sprintf("failed to initialize failure learning agent: %w", err), nil)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf(fmt.Sprintf("context-aware event bridge is nil for %s", agentName), nil)
	}

	hcpo.GetLogger().Info(fmt.Sprintf("🔍 Checking agent structure for %s", agentName))
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf(fmt.Sprintf("base agent is nil for %s", agentName), nil)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf(fmt.Sprintf("MCP agent is nil for %s", agentName), nil)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		// Extract step number from learningPathIdentifier for SetOrchestratorContext (which expects numeric step)
		pathInfo := parseStepPath(learningPathIdentifier)
		stepNumberForContext := pathInfo.ParentStepNumber - 1 // Convert to 0-based for SetOrchestratorContext
		cab.SetOrchestratorContext(phase, stepNumberForContext, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Info(fmt.Sprintf("🔗 Context-aware bridge connected to %s (%s, agent %s)", phase, learningPathIdentifier, baseAgentName))
	} else {
		return nil, fmt.Errorf(fmt.Sprintf("context-aware bridge type mismatch for %s", agentName), nil)
	}

	// Register custom tools - filter by enabled categories and/or specific tools if specified
	var toolsToRegister []llmtypes.Tool
	var executorsToUse map[string]interface{}

	if stepConfig != nil && (len(stepConfig.EnabledCustomToolCategories) > 0 || len(stepConfig.EnabledCustomTools) > 0) {
		// Convert old format (categories + tools) to new unified format (category:tool or category:*)
		unifiedEnabledTools := orchestrator.ConvertOldFormatToNewFormat(
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		// Filter tools based on unified format
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			unifiedEnabledTools,
		)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools))
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		// Wrap executors and enhance tool descriptions with folder guard (automatic)
		toolsToRegister, wrappedExecutors := hcpo.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode()))

		for _, tool := range toolsToRegister {
			if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					hcpo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to convert parameters for tool %s", tool.Function.Name))
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from ToolCategories map - REQUIRED
					var toolCategory string
					if hcpo.ToolCategories != nil {
						if cat, exists := hcpo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
						} else {
							hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name), nil)
							continue // Skip this tool
						}
					} else {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name), nil)
						continue // Skip this tool
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						hcpo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Failed to register tool %s: %v", tool.Function.Name, err), nil)
						continue // Skip this tool
					}
				} else {
					hcpo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to convert executor for tool %s", tool.Function.Name))
				}
			}
		}

		hcpo.GetLogger().Info(fmt.Sprintf("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode()))

		// Update code execution registry with wrapped executors for folder guard to work
		if hcpo.GetUseCodeExecutionMode() {
			// CRITICAL: Set folder guard paths BEFORE updating code execution registry
			// The registry generation uses these paths to create the path validation code
			// This ensures LLM-generated Go code can only access paths within allowed boundaries
			readPaths, writePaths := hcpo.GetFolderGuardPaths()
			mcpAgent.SetFolderGuardPaths(readPaths, writePaths)
			hcpo.GetLogger().Info(fmt.Sprintf("🔒 [CODE_EXECUTION] Folder guard paths set for %s agent - Read: %v, Write: %v", agentName, readPaths, writePaths))

			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				hcpo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update code execution registry for %s: %v", agentName, err))
			} else {
				hcpo.GetLogger().Info(fmt.Sprintf("✅ [CODE_EXECUTION] Registry updated for %s agent - folder guard enabled", agentName))
			}
		}
	}

	return agent, nil
}

// createConditionalAgent creates a conditional agent using the standard factory pattern
// This ensures proper event bridge connection, context setup, and tool registration
func (hcpo *HumanControlledTodoPlannerOrchestrator) createConditionalAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs, conditionalLLMConfig *orchestrator.LLMConfig) (agents.OrchestratorAgent, error) {
	// Conditional agent doesn't need folder guard (no file operations)
	// It only evaluates conditions using tools

	// Determine max turns: use orchestrator default (conditional agents don't have step-specific max turns config)
	maxTurns := hcpo.GetMaxTurns()
	// Note: ConditionalMaxTurns doesn't exist in AgentConfigs - using orchestrator default

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if conditionalLLMConfig != nil && conditionalLLMConfig.Provider != "" && conditionalLLMConfig.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       conditionalLLMConfig.Provider,
			ModelID:        conditionalLLMConfig.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific conditional LLM: %s/%s", conditionalLLMConfig.Provider, conditionalLLMConfig.ModelID))
	} else if hcpo.presetValidationLLM != nil && hcpo.presetValidationLLM.Provider != "" && hcpo.presetValidationLLM.ModelID != "" {
		// Use validation LLM as default for conditional agent (similar purpose - structured decision making)
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetValidationLLM.Provider,
			ModelID:        hcpo.presetValidationLLM.ModelID,
			FallbackModels: []string{},                    // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
		}
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using preset default validation LLM for conditional agent: %s/%s", hcpo.presetValidationLLM.Provider, hcpo.presetValidationLLM.ModelID))
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default conditional LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID))
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Use step-specific servers/tools if provided, otherwise use orchestrator defaults (same as execution agent)
	if stepConfig != nil && len(stepConfig.SelectedServers) > 0 {
		config.ServerNames = stepConfig.SelectedServers
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using step-specific conditional servers: %v", stepConfig.SelectedServers))
	} else {
		config.ServerNames = hcpo.GetSelectedServers()
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Using orchestrator default conditional servers: %v", config.ServerNames))
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

	if stepConfig != nil && (len(stepConfig.EnabledCustomToolCategories) > 0 || len(stepConfig.EnabledCustomTools) > 0) {
		// Convert old format (categories + tools) to new unified format (category:tool or category:*)
		unifiedEnabledTools := orchestrator.ConvertOldFormatToNewFormat(
			stepConfig.EnabledCustomToolCategories,
			stepConfig.EnabledCustomTools,
		)
		// Filter tools based on unified format
		toolsToRegister, executorsToUse = orchestrator.FilterCustomToolsByCategory(
			hcpo.WorkspaceTools,
			hcpo.WorkspaceToolExecutors,
			unifiedEnabledTools,
		)
		hcpo.GetLogger().Info(fmt.Sprintf("🔧 Filtered custom tools for conditional agent: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools))
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

	hcpo.GetLogger().Info(fmt.Sprintf("✅ Created conditional agent using standard factory pattern: %s (step %d, phase %s)", agentName, step+1, phase))
	return agent, nil
}

// Execute implements the Orchestrator interface
