package todo_creation_human

import (
	"context"
	"encoding/json"
	"fmt"

	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcpagent/mcpclient"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

func (hcpo *HumanControlledTodoPlannerOrchestrator) createExecutionAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
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
		hcpo.GetLogger().Infof("🔧 Using step-specific code execution mode: %v", isCodeExecutionMode)
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
		hcpo.GetLogger().Infof("🔧 Using preset code execution mode: %v", isCodeExecutionMode)
	}
	// Use learning_code_exec folder if code execution mode is enabled, otherwise use learnings folder
	var learningsPath string
	if isCodeExecutionMode {
		learningsPath = fmt.Sprintf("%s/learning_code_exec", baseWorkspacePath)
	} else {
		learningsPath = fmt.Sprintf("%s/learnings", baseWorkspacePath)
	}

	// Only specify learnings in readPaths - execution is automatically readable since it's in writePaths
	readPaths := []string{learningsPath}
	writePaths := []string{executionWorkspacePath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard - Read paths: %v, Write paths: %v (execution automatically readable via writePaths)", readPaths, writePaths)

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.ExecutionMaxTurns != nil {
		maxTurns = *stepConfig.ExecutionMaxTurns
		hcpo.GetLogger().Infof("🔧 Using step-specific execution max turns: %d", maxTurns)
	}

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.ExecutionLLM.Provider,
			ModelID:        stepConfig.ExecutionLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific execution LLM: %s/%s", stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID)
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetExecutionLLM.Provider,
			ModelID:        hcpo.presetExecutionLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using preset default execution LLM: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID)
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Infof("🔧 Using orchestrator default execution LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID)
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Use step-specific servers/tools if provided, otherwise use orchestrator defaults
	if stepConfig != nil && len(stepConfig.SelectedServers) > 0 {
		config.ServerNames = stepConfig.SelectedServers
		hcpo.GetLogger().Infof("🔧 Using step-specific execution servers: %v", stepConfig.SelectedServers)
	} else if stepConfig != nil {
		// Log when stepConfig exists but SelectedServers is empty (will use orchestrator defaults)
		hcpo.GetLogger().Infof("🔧 Step config found but no SelectedServers specified - using orchestrator defaults")
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		config.SelectedTools = stepConfig.SelectedTools
		hcpo.GetLogger().Infof("🔧 Using step-specific execution tools: %v", stepConfig.SelectedTools)
	} else if stepConfig != nil {
		// Log when stepConfig exists but SelectedTools is empty (will use orchestrator defaults)
		hcpo.GetLogger().Infof("🔧 Step config found but no SelectedTools specified - using orchestrator defaults")
	}

	// Code execution mode: Priority: step config > preset default (already resolved above)
	// Note: config.UseCodeExecutionMode is set by CreateStandardAgentConfigWithLLM based on orchestrator setting
	// We override it based on step config or preset default
	config.UseCodeExecutionMode = isCodeExecutionMode
	if isCodeExecutionMode {
		hcpo.GetLogger().Infof("🔧 Code execution mode enabled for execution agent - MCP tools will be accessed via generated Go code")
	} else {
		hcpo.GetLogger().Infof("🔧 Code execution mode disabled for execution agent - MCP tools will be exposed directly")
	}

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Infof("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools)
	}

	// Create agent using provided factory function
	agent := NewHumanControlledTodoPlannerExecutionAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from CreateAndSetupStandardAgent)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize execution agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	hcpo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
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
		hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools)
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		// Wrap executors and enhance tool descriptions with folder guard (automatic)
		toolsToRegister, wrappedExecutors := hcpo.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode())

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
					hcpo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from ToolCategories map - REQUIRED
					var toolCategory string
					if hcpo.ToolCategories != nil {
						if cat, exists := hcpo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
						} else {
							hcpo.GetLogger().Errorf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
							continue // Skip this tool
						}
					} else {
						hcpo.GetLogger().Errorf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
						continue // Skip this tool
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						hcpo.GetLogger().Errorf("❌ [DISCOVERY] Failed to register tool %s: %v", tool.Function.Name, err)
						continue // Skip this tool
					}
				} else {
					hcpo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		hcpo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())

		// Update code execution registry with wrapped executors for folder guard to work
		if isCodeExecutionMode {
			// CRITICAL: Set folder guard paths BEFORE updating code execution registry
			// The registry generation uses these paths to create the path validation code
			// This ensures LLM-generated Go code can only access paths within allowed boundaries
			readPaths, writePaths := hcpo.GetFolderGuardPaths()
			mcpAgent.SetFolderGuardPaths(readPaths, writePaths)
			hcpo.GetLogger().Infof("🔒 [CODE_EXECUTION] Folder guard paths set for %s agent - Read: %v, Write: %v", agentName, readPaths, writePaths)

			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to update code execution registry for %s: %v", agentName, err)
			} else {
				hcpo.GetLogger().Infof("✅ [CODE_EXECUTION] Registry updated for %s agent - folder guard enabled", agentName)
			}
		}
	}

	return agent, nil
}

// createLearningReadingAgent creates a learning reading agent for discovering and reading learning files
// codeExecutionMode: The code execution mode value (already computed with step-level priority) to ensure consistency with execution agent
// executionWorkspacePath: The execution workspace path where context dependency files are located (for code execution mode)
func (hcpo *HumanControlledTodoPlannerOrchestrator) createLearningReadingAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs, codeExecutionMode bool, executionWorkspacePath string) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from learnings (read-only), no writes
	baseWorkspacePath := hcpo.GetWorkspacePath()
	// Use the provided code execution mode (already computed with step-level priority) to ensure consistency
	isCodeExecutionMode := codeExecutionMode
	hcpo.GetLogger().Infof("🔧 Learning reading agent using code execution mode: %v (matches execution agent)", isCodeExecutionMode)
	// Use learning_code_exec folder if code execution mode is enabled, otherwise use learnings folder
	var learningsPath string
	if isCodeExecutionMode {
		learningsPath = fmt.Sprintf("%s/learning_code_exec", baseWorkspacePath)
	} else {
		learningsPath = fmt.Sprintf("%s/learnings", baseWorkspacePath)
	}

	// Build read paths: learnings path + execution workspace path (for context dependencies in code execution mode)
	readPaths := []string{learningsPath}
	if isCodeExecutionMode && executionWorkspacePath != "" {
		// Add execution workspace path for reading context dependency files
		readPaths = append(readPaths, executionWorkspacePath)
		hcpo.GetLogger().Infof("🔧 Learning reading agent: Added execution workspace path for context dependencies: %s", executionWorkspacePath)
	}
	writePaths := []string{} // No write permissions for learning reading agent
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard for learning reading agent - Read paths: %v, Write paths: %v (read-only)", readPaths, writePaths)

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.ExecutionMaxTurns != nil {
		maxTurns = *stepConfig.ExecutionMaxTurns
		hcpo.GetLogger().Infof("🔧 Using step-specific learning reading max turns: %d", maxTurns)
	}

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.ExecutionLLM.Provider,
			ModelID:        stepConfig.ExecutionLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific learning reading LLM: %s/%s", stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID)
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetExecutionLLM.Provider,
			ModelID:        hcpo.presetExecutionLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using preset default learning reading LLM: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID)
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Infof("🔧 Using orchestrator default learning reading LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID)
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Learning reading agent uses NoServers (read-only file operations via workspace tools)
	// Step-specific server/tool selection is only for execution agents
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - uses workspace tools for file reading

	// Learning reading agent ALWAYS uses simple mode (direct MCP tool access) regardless of execution agent's mode
	// The codeExecutionMode parameter is only used to determine which learnings folder to read from (learnings vs learning_code_exec)
	// CRITICAL: Override orchestrator-level code execution mode setting - learning reading agent always uses simple mode
	config.UseCodeExecutionMode = false
	hcpo.GetLogger().Infof("🔧 Learning reading agent always uses simple mode (direct MCP tool access) - code execution mode: %v only determines learnings folder path", isCodeExecutionMode)

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Infof("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools)
	}

	// Create agent using learning reading factory function
	agent := NewHumanControlledTodoPlannerLearningReadingAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize learning reading agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	hcpo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	// CRITICAL: Learning reading agent ONLY uses these two essential tools
	// These tools are required for discovering and reading learning files
	// Ignore step config filtering - learning reading agent has fixed tool set
	essentialTools := []string{"list_workspace_files", "read_workspace_file"}
	toolsToRegister := make([]llmtypes.Tool, 0, len(essentialTools))
	executorsToUse := make(map[string]interface{})

	for _, toolName := range essentialTools {
		// Find the tool in the full workspace tools list
		found := false
		for _, tool := range hcpo.WorkspaceTools {
			if tool.Function.Name == toolName {
				toolsToRegister = append(toolsToRegister, tool)
				// Ensure executor is also added
				if executor, exists := hcpo.WorkspaceToolExecutors[toolName]; exists {
					executorsToUse[toolName] = executor
				}
				found = true
				hcpo.GetLogger().Infof("✅ [LEARNING_READING] Found essential tool '%s'", toolName)
				break
			}
		}
		if !found {
			hcpo.GetLogger().Warnf("⚠️ [LEARNING_READING] Essential tool '%s' not found in workspace tools - learning reading agent may not function correctly", toolName)
		}
	}
	hcpo.GetLogger().Infof("🔧 [LEARNING_READING] Learning reading agent will use ONLY these %d essential tools: %v", len(toolsToRegister), essentialTools)

	if len(toolsToRegister) > 0 && len(executorsToUse) > 0 {
		hcpo.GetLogger().Infof("🔧 [LEARNING_READING] Starting tool registration for %s agent", agentName)
		hcpo.GetLogger().Infof("🔧 [LEARNING_READING] Tools to register: %d, Executors available: %d", len(toolsToRegister), len(executorsToUse))

		// Wrap executors and enhance tool descriptions with folder guard (automatic)
		toolsToRegister, wrappedExecutors := hcpo.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)
		hcpo.GetLogger().Infof("🔧 [LEARNING_READING] Wrapped executors: %d (after folder guard)", len(wrappedExecutors))

		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode())

		registeredCount := 0
		skippedCount := 0
		for _, tool := range toolsToRegister {
			hcpo.GetLogger().Debugf("🔧 [LEARNING_READING] Processing tool: %s", tool.Function.Name)
			if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
				hcpo.GetLogger().Debugf("🔧 [LEARNING_READING] Found executor for tool: %s", tool.Function.Name)
				var params map[string]interface{}
				if tool.Function.Parameters != nil {
					paramsBytes, err := json.Marshal(tool.Function.Parameters)
					if err == nil {
						json.Unmarshal(paramsBytes, &params)
					}
				}
				if params == nil {
					hcpo.GetLogger().Warnf("⚠️ [LEARNING_READING] Failed to convert parameters for tool %s", tool.Function.Name)
					skippedCount++
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from ToolCategories map - REQUIRED
					var toolCategory string
					if hcpo.ToolCategories != nil {
						if cat, exists := hcpo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
						} else {
							hcpo.GetLogger().Errorf("❌ [LEARNING_READING] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
							skippedCount++
							continue // Skip this tool
						}
					} else {
						hcpo.GetLogger().Errorf("❌ [LEARNING_READING] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
						skippedCount++
						continue // Skip this tool
					}

					hcpo.GetLogger().Debugf("🔧 [LEARNING_READING] Registering tool %s with category %s", tool.Function.Name, toolCategory)
					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						hcpo.GetLogger().Errorf("❌ [LEARNING_READING] Failed to register tool %s: %v", tool.Function.Name, err)
						skippedCount++
						continue // Skip this tool
					}
					registeredCount++
					hcpo.GetLogger().Infof("✅ [LEARNING_READING] Successfully registered tool: %s (category: %s)", tool.Function.Name, toolCategory)
				} else {
					hcpo.GetLogger().Warnf("⚠️ [LEARNING_READING] Failed to convert executor for tool %s", tool.Function.Name)
					skippedCount++
				}
			} else {
				hcpo.GetLogger().Warnf("⚠️ [LEARNING_READING] Executor not found in wrappedExecutors for tool: %s (available executors: %d)", tool.Function.Name, len(wrappedExecutors))
				skippedCount++
			}
		}

		hcpo.GetLogger().Infof("✅ [LEARNING_READING] Tool registration complete for %s agent - Registered: %d, Skipped: %d, Total: %d (%s mode)",
			agentName, registeredCount, skippedCount, len(toolsToRegister), baseAgent.GetMode())
	} else {
		hcpo.GetLogger().Warnf("⚠️ [LEARNING_READING] Cannot register tools - toolsToRegister=%v, executorsToUse=%v",
			toolsToRegister != nil, executorsToUse != nil)
	}

	return agent, nil
}

// createExecutionOnlyAgent creates an execution-only agent that receives pre-discovered learning history
func (hcpo *HumanControlledTodoPlannerOrchestrator) createExecutionOnlyAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
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
		hcpo.GetLogger().Infof("🔧 Using step-specific code execution mode: %v", isCodeExecutionMode)
	} else {
		isCodeExecutionMode = hcpo.GetUseCodeExecutionMode()
		hcpo.GetLogger().Infof("🔧 Using preset code execution mode: %v", isCodeExecutionMode)
	}
	// Use learning_code_exec folder if code execution mode is enabled, otherwise use learnings folder
	var learningsPath string
	if isCodeExecutionMode {
		learningsPath = fmt.Sprintf("%s/learning_code_exec", baseWorkspacePath)
	} else {
		learningsPath = fmt.Sprintf("%s/learnings", baseWorkspacePath)
	}

	// Only specify learnings in readPaths - execution is automatically readable since it's in writePaths
	readPaths := []string{learningsPath}
	writePaths := []string{executionWorkspacePath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard for execution-only agent - Read paths: %v, Write paths: %v (execution automatically readable via writePaths)", readPaths, writePaths)

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.ExecutionMaxTurns != nil {
		maxTurns = *stepConfig.ExecutionMaxTurns
		hcpo.GetLogger().Infof("🔧 Using step-specific execution-only max turns: %d", maxTurns)
	}

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.ExecutionLLM != nil && stepConfig.ExecutionLLM.Provider != "" && stepConfig.ExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.ExecutionLLM.Provider,
			ModelID:        stepConfig.ExecutionLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific execution-only LLM: %s/%s", stepConfig.ExecutionLLM.Provider, stepConfig.ExecutionLLM.ModelID)
	} else if hcpo.presetExecutionLLM != nil && hcpo.presetExecutionLLM.Provider != "" && hcpo.presetExecutionLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetExecutionLLM.Provider,
			ModelID:        hcpo.presetExecutionLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using preset default execution-only LLM: %s/%s", hcpo.presetExecutionLLM.Provider, hcpo.presetExecutionLLM.ModelID)
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Infof("🔧 Using orchestrator default execution-only LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID)
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Use step-specific servers/tools if provided, otherwise use orchestrator defaults
	if stepConfig != nil && len(stepConfig.SelectedServers) > 0 {
		config.ServerNames = stepConfig.SelectedServers
		hcpo.GetLogger().Infof("🔧 Using step-specific execution-only servers: %v", stepConfig.SelectedServers)
	} else if stepConfig != nil {
		// Log when stepConfig exists but SelectedServers is empty (will use orchestrator defaults)
		hcpo.GetLogger().Infof("🔧 Step config found but no SelectedServers specified - using orchestrator defaults")
	}
	if stepConfig != nil && len(stepConfig.SelectedTools) > 0 {
		config.SelectedTools = stepConfig.SelectedTools
		hcpo.GetLogger().Infof("🔧 Using step-specific execution-only tools: %v", stepConfig.SelectedTools)
	} else if stepConfig != nil {
		// Log when stepConfig exists but SelectedTools is empty (will use orchestrator defaults)
		hcpo.GetLogger().Infof("🔧 Step config found but no SelectedTools specified - using orchestrator defaults")
	}

	// Code execution mode: Priority: step config > preset default (already resolved above)
	// Note: config.UseCodeExecutionMode is set by CreateStandardAgentConfigWithLLM based on orchestrator setting
	// We override it based on step config or preset default
	config.UseCodeExecutionMode = isCodeExecutionMode
	if isCodeExecutionMode {
		hcpo.GetLogger().Infof("🔧 Code execution mode enabled for execution-only agent - MCP tools will be accessed via generated Go code")
	} else {
		hcpo.GetLogger().Infof("🔧 Code execution mode disabled for execution-only agent - MCP tools will be exposed directly")
	}

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Infof("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools)
	}

	// Create agent using execution-only factory function
	agent := NewHumanControlledTodoPlannerExecutionOnlyAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize execution-only agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	hcpo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
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
		hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools)
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		// Wrap executors and enhance tool descriptions with folder guard (automatic)
		toolsToRegister, wrappedExecutors := hcpo.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode())

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
					hcpo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from ToolCategories map - REQUIRED
					var toolCategory string
					if hcpo.ToolCategories != nil {
						if cat, exists := hcpo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
						} else {
							hcpo.GetLogger().Errorf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
							continue // Skip this tool
						}
					} else {
						hcpo.GetLogger().Errorf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
						continue // Skip this tool
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						hcpo.GetLogger().Errorf("❌ [DISCOVERY] Failed to register tool %s: %v", tool.Function.Name, err)
						continue // Skip this tool
					}
				} else {
					hcpo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		hcpo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())

		// Update code execution registry with wrapped executors for folder guard to work
		if isCodeExecutionMode {
			// CRITICAL: Set folder guard paths BEFORE updating code execution registry
			// The registry generation uses these paths to create the path validation code
			// This ensures LLM-generated Go code can only access paths within allowed boundaries
			readPaths, writePaths := hcpo.GetFolderGuardPaths()
			mcpAgent.SetFolderGuardPaths(readPaths, writePaths)
			hcpo.GetLogger().Infof("🔒 [CODE_EXECUTION] Folder guard paths set for %s agent - Read: %v, Write: %v", agentName, readPaths, writePaths)

			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to update code execution registry for %s: %v", agentName, err)
			} else {
				hcpo.GetLogger().Infof("✅ [CODE_EXECUTION] Registry updated for %s agent - folder guard enabled", agentName)
			}
		}
	}

	return agent, nil
}

// createValidationAgent creates a validation agent for the current iteration
func (hcpo *HumanControlledTodoPlannerOrchestrator) createValidationAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
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
	hcpo.GetLogger().Infof("🔒 Setting folder guard for validation agent - Read paths: %v, Write paths: %v (read-only, no file writes)", readPaths, writePaths)

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.ValidationMaxTurns != nil {
		maxTurns = *stepConfig.ValidationMaxTurns
		hcpo.GetLogger().Infof("🔧 Using step-specific validation max turns: %d", maxTurns)
	}

	// Determine LLM config: use step-specific if provided, otherwise use orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	// Priority: step config > preset default > orchestrator default
	if stepConfig != nil && stepConfig.ValidationLLM != nil && stepConfig.ValidationLLM.Provider != "" && stepConfig.ValidationLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.ValidationLLM.Provider,
			ModelID:        stepConfig.ValidationLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific validation LLM: %s/%s", stepConfig.ValidationLLM.Provider, stepConfig.ValidationLLM.ModelID)
	} else if hcpo.presetValidationLLM != nil && hcpo.presetValidationLLM.Provider != "" && hcpo.presetValidationLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetValidationLLM.Provider,
			ModelID:        hcpo.presetValidationLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using preset default validation LLM: %s/%s", hcpo.presetValidationLLM.Provider, hcpo.presetValidationLLM.ModelID)
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Infof("🔧 Using orchestrator default validation LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID)
	}

	// Create agent config with custom LLM if needed
	config := hcpo.CreateStandardAgentConfigWithLLM(agentName, maxTurns, agents.OutputFormatStructured, llmConfig)

	// Validation agents always use NoServers (pure LLM validation agent)
	// Step-specific server/tool selection is only for execution agents
	config.ServerNames = []string{mcpclient.NoServers} // No MCP servers needed - pure LLM validation agent

	// Code execution mode only applies to execution agents, not validation agents
	config.UseCodeExecutionMode = false
	hcpo.GetLogger().Infof("🔧 Disabling code execution mode for validation agent (only execution agents use MCP tools)")

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Infof("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools)
	}

	// Create agent using provided factory function
	agent := NewHumanControlledTodoPlannerValidationAgent(config, hcpo.GetLogger(), hcpo.GetTracer(), hcpo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from CreateAndSetupStandardAgent)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize validation agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	hcpo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
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
		hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools)
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		// Wrap executors and enhance tool descriptions with folder guard (automatic)
		toolsToRegister, wrappedExecutors := hcpo.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode())

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
					hcpo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from ToolCategories map - REQUIRED
					var toolCategory string
					if hcpo.ToolCategories != nil {
						if cat, exists := hcpo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
						} else {
							hcpo.GetLogger().Errorf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
							continue // Skip this tool
						}
					} else {
						hcpo.GetLogger().Errorf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
						continue // Skip this tool
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						hcpo.GetLogger().Errorf("❌ [DISCOVERY] Failed to register tool %s: %v", tool.Function.Name, err)
						continue // Skip this tool
					}
				} else {
					hcpo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		hcpo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())

		// Update code execution registry with wrapped executors for folder guard to work
		if hcpo.GetUseCodeExecutionMode() {
			// CRITICAL: Set folder guard paths BEFORE updating code execution registry
			// The registry generation uses these paths to create the path validation code
			// This ensures LLM-generated Go code can only access paths within allowed boundaries
			readPaths, writePaths := hcpo.GetFolderGuardPaths()
			mcpAgent.SetFolderGuardPaths(readPaths, writePaths)
			hcpo.GetLogger().Infof("🔒 [CODE_EXECUTION] Folder guard paths set for %s agent - Read: %v, Write: %v", agentName, readPaths, writePaths)

			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to update code execution registry for %s: %v", agentName, err)
			} else {
				hcpo.GetLogger().Infof("✅ [CODE_EXECUTION] Registry updated for %s agent - folder guard enabled", agentName)
			}
		}
	}

	return agent, nil
}

// Note: Learning integration functions removed - execution agent now auto-discovers learning files and scripts

// createSuccessLearningAgent creates a success learning agent for analyzing successful executions
func (hcpo *HumanControlledTodoPlannerOrchestrator) createSuccessLearningAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from execution and learnings (read-only), writes only to learnings
	baseWorkspacePath := hcpo.GetWorkspacePath()
	executionPath := fmt.Sprintf("%s/execution", baseWorkspacePath)

	// Check if execution agent was in code execution mode to determine which learnings folder to use
	wasCodeExecutionMode := hcpo.GetUseCodeExecutionMode()
	var learningsPath string
	if wasCodeExecutionMode {
		learningsPath = fmt.Sprintf("%s/learning_code_exec", baseWorkspacePath)
	} else {
		learningsPath = fmt.Sprintf("%s/learnings", baseWorkspacePath)
	}

	// Only specify execution in readPaths - learnings is automatically readable since it's in writePaths
	readPaths := []string{executionPath}
	writePaths := []string{learningsPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard for success learning agent - Read paths: %v, Write paths: %v (learnings automatically readable via writePaths)", readPaths, writePaths)

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.LearningMaxTurns != nil {
		maxTurns = *stepConfig.LearningMaxTurns
		hcpo.GetLogger().Infof("🔧 Using step-specific learning max turns: %d", maxTurns)
	}

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.LearningLLM.Provider,
			ModelID:        stepConfig.LearningLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific learning LLM: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID)
	} else if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetLearningLLM.Provider,
			ModelID:        hcpo.presetLearningLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using preset default learning LLM: %s/%s", hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID)
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Infof("🔧 Using orchestrator default learning LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID)
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
		hcpo.GetLogger().Infof("🔧 Execution was in code execution mode - using code execution learning agent (but agent itself does NOT use code execution mode)")
		if originalCodeExecMode {
			hcpo.GetLogger().Warnf("⚠️ Code execution mode was enabled in config but disabled for learning agent (original: %v, new: %v)", originalCodeExecMode, config.UseCodeExecutionMode)
		}
	} else {
		hcpo.GetLogger().Infof("🔧 Disabling code execution mode for success learning agent (only execution agents use MCP tools)")
	}
	hcpo.GetLogger().Infof("🔧 Learning agent code execution mode: %v (NoServers=%v, will be auto-disabled if needed)", config.UseCodeExecutionMode, config.ServerNames[0] == mcpclient.NoServers)

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Infof("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools)
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
		return nil, fmt.Errorf("failed to initialize success learning agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	hcpo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
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
		hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools)
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		// Wrap executors and enhance tool descriptions with folder guard (automatic)
		toolsToRegister, wrappedExecutors := hcpo.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode())

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
					hcpo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from ToolCategories map - REQUIRED
					var toolCategory string
					if hcpo.ToolCategories != nil {
						if cat, exists := hcpo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
						} else {
							hcpo.GetLogger().Errorf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
							continue // Skip this tool
						}
					} else {
						hcpo.GetLogger().Errorf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
						continue // Skip this tool
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						hcpo.GetLogger().Errorf("❌ [DISCOVERY] Failed to register tool %s: %v", tool.Function.Name, err)
						continue // Skip this tool
					}
				} else {
					hcpo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		hcpo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())

		// Update code execution registry with wrapped executors for folder guard to work
		if hcpo.GetUseCodeExecutionMode() {
			// CRITICAL: Set folder guard paths BEFORE updating code execution registry
			// The registry generation uses these paths to create the path validation code
			// This ensures LLM-generated Go code can only access paths within allowed boundaries
			readPaths, writePaths := hcpo.GetFolderGuardPaths()
			mcpAgent.SetFolderGuardPaths(readPaths, writePaths)
			hcpo.GetLogger().Infof("🔒 [CODE_EXECUTION] Folder guard paths set for %s agent - Read: %v, Write: %v", agentName, readPaths, writePaths)

			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to update code execution registry for %s: %v", agentName, err)
			} else {
				hcpo.GetLogger().Infof("✅ [CODE_EXECUTION] Registry updated for %s agent - folder guard enabled", agentName)
			}
		}
	}

	return agent, nil
}

// createFailureLearningAgent creates a failure learning agent for analyzing failed executions
// Note: This now uses the unified learning agent which handles both success and failure cases
func (hcpo *HumanControlledTodoPlannerOrchestrator) createFailureLearningAgent(ctx context.Context, phase string, step, iteration int, agentName string, stepConfig *AgentConfigs) (agents.OrchestratorAgent, error) {
	// Set folder guard paths: allow reads from execution and learnings (read-only), writes only to learnings
	baseWorkspacePath := hcpo.GetWorkspacePath()
	executionPath := fmt.Sprintf("%s/execution", baseWorkspacePath)

	// Check if execution agent was in code execution mode to determine which learnings folder to use
	wasCodeExecutionMode := hcpo.GetUseCodeExecutionMode()
	var learningsPath string
	if wasCodeExecutionMode {
		learningsPath = fmt.Sprintf("%s/learning_code_exec", baseWorkspacePath)
	} else {
		learningsPath = fmt.Sprintf("%s/learnings", baseWorkspacePath)
	}

	// Only specify execution in readPaths - learnings is automatically readable since it's in writePaths
	readPaths := []string{executionPath}
	writePaths := []string{learningsPath}
	hcpo.SetWorkspacePathForFolderGuard(readPaths, writePaths)
	hcpo.GetLogger().Infof("🔒 Setting folder guard for failure learning agent - Read paths: %v, Write paths: %v (learnings automatically readable via writePaths)", readPaths, writePaths)

	// Determine max turns: use step-specific if provided, otherwise use orchestrator default
	maxTurns := hcpo.GetMaxTurns()
	if stepConfig != nil && stepConfig.LearningMaxTurns != nil {
		maxTurns = *stepConfig.LearningMaxTurns
		hcpo.GetLogger().Infof("🔧 Using step-specific learning max turns: %d", maxTurns)
	}

	// Determine LLM config: Priority: step config > preset default > orchestrator default
	var llmConfig *orchestrator.LLMConfig
	orchestratorLLMConfig := hcpo.GetLLMConfig()
	if stepConfig != nil && stepConfig.LearningLLM != nil && stepConfig.LearningLLM.Provider != "" && stepConfig.LearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       stepConfig.LearningLLM.Provider,
			ModelID:        stepConfig.LearningLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for step-specific configs
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using step-specific learning LLM: %s/%s", stepConfig.LearningLLM.Provider, stepConfig.LearningLLM.ModelID)
	} else if hcpo.presetLearningLLM != nil && hcpo.presetLearningLLM.Provider != "" && hcpo.presetLearningLLM.ModelID != "" {
		llmConfig = &orchestrator.LLMConfig{
			Provider:       hcpo.presetLearningLLM.Provider,
			ModelID:        hcpo.presetLearningLLM.ModelID,
			FallbackModels: nil,                           // Use empty fallback for preset defaults
			APIKeys:        orchestratorLLMConfig.APIKeys, // Preserve API keys from orchestrator
			Options:        orchestratorLLMConfig.Options, // Preserve LLM options from orchestrator
		}
		hcpo.GetLogger().Infof("🔧 Using preset default learning LLM: %s/%s", hcpo.presetLearningLLM.Provider, hcpo.presetLearningLLM.ModelID)
	} else {
		llmConfig = orchestratorLLMConfig
		hcpo.GetLogger().Infof("🔧 Using orchestrator default learning LLM: %s/%s", llmConfig.Provider, llmConfig.ModelID)
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
		hcpo.GetLogger().Infof("🔧 Execution was in code execution mode - using code execution learning agent (but agent itself does NOT use code execution mode)")
		if originalCodeExecMode {
			hcpo.GetLogger().Warnf("⚠️ Code execution mode was enabled in config but disabled for learning agent (original: %v, new: %v)", originalCodeExecMode, config.UseCodeExecutionMode)
		}
	} else {
		hcpo.GetLogger().Infof("🔧 Disabling code execution mode for failure learning agent (only execution agents use MCP tools)")
	}
	hcpo.GetLogger().Infof("🔧 Learning agent code execution mode: %v (NoServers=%v, will be auto-disabled if needed)", config.UseCodeExecutionMode, config.ServerNames[0] == mcpclient.NoServers)

	// Set EnableLargeOutputVirtualTools if specified
	if stepConfig != nil && stepConfig.EnableLargeOutputVirtualTools != nil {
		config.EnableLargeOutputVirtualTools = stepConfig.EnableLargeOutputVirtualTools
		hcpo.GetLogger().Infof("🔧 Using step-specific large output virtual tools setting: %v", *stepConfig.EnableLargeOutputVirtualTools)
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
		return nil, fmt.Errorf("failed to initialize failure learning agent: %w", err)
	}

	// Validate essentials and connect event bridge
	eventBridge := hcpo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	hcpo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// Connect agent to orchestrator's main event bridge
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*orchestrator.ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		hcpo.GetLogger().Infof("🔗 Context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
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
		hcpo.GetLogger().Infof("🔧 Filtered custom tools: %d tools enabled from %d entries: %v", len(toolsToRegister), len(unifiedEnabledTools), unifiedEnabledTools)
	} else {
		// Backward compatible: use all tools if no filtering specified (default behavior)
		toolsToRegister = hcpo.WorkspaceTools
		executorsToUse = hcpo.WorkspaceToolExecutors
	}

	if toolsToRegister != nil && executorsToUse != nil {
		// Wrap executors and enhance tool descriptions with folder guard (automatic)
		toolsToRegister, wrappedExecutors := hcpo.PrepareWorkspaceToolsWithFolderGuard(toolsToRegister, executorsToUse)

		hcpo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(toolsToRegister), agentName, baseAgent.GetMode())

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
					hcpo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
					continue
				}

				if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
					// Get tool category from ToolCategories map - REQUIRED
					var toolCategory string
					if hcpo.ToolCategories != nil {
						if cat, exists := hcpo.ToolCategories[tool.Function.Name]; exists {
							toolCategory = cat
						} else {
							hcpo.GetLogger().Errorf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
							continue // Skip this tool
						}
					} else {
						hcpo.GetLogger().Errorf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
						continue // Skip this tool
					}

					if err := mcpAgent.RegisterCustomTool(
						tool.Function.Name,
						tool.Function.Description,
						params,
						toolExecutor,
						toolCategory,
					); err != nil {
						hcpo.GetLogger().Errorf("❌ [DISCOVERY] Failed to register tool %s: %v", tool.Function.Name, err)
						continue // Skip this tool
					}
				} else {
					hcpo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
				}
			}
		}

		hcpo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())

		// Update code execution registry with wrapped executors for folder guard to work
		if hcpo.GetUseCodeExecutionMode() {
			// CRITICAL: Set folder guard paths BEFORE updating code execution registry
			// The registry generation uses these paths to create the path validation code
			// This ensures LLM-generated Go code can only access paths within allowed boundaries
			readPaths, writePaths := hcpo.GetFolderGuardPaths()
			mcpAgent.SetFolderGuardPaths(readPaths, writePaths)
			hcpo.GetLogger().Infof("🔒 [CODE_EXECUTION] Folder guard paths set for %s agent - Read: %v, Write: %v", agentName, readPaths, writePaths)

			if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
				hcpo.GetLogger().Warnf("⚠️ Failed to update code execution registry for %s: %v", agentName, err)
			} else {
				hcpo.GetLogger().Infof("✅ [CODE_EXECUTION] Registry updated for %s agent - folder guard enabled", agentName)
			}
		}
	}

	return agent, nil
}

// Execute implements the Orchestrator interface
