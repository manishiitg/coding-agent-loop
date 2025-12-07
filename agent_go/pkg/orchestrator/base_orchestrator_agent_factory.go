package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"

	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// CreateStandardAgentConfig creates a standardized agent configuration
// use CreateAndSetupStandardAgent instead which combines configuration and setup.
func (bo *BaseOrchestrator) CreateStandardAgentConfig(agentName string, maxTurns int, outputFormat agents.OutputFormat) *agents.OrchestratorAgentConfig {
	return bo.createAgentConfigWithLLM(agentName, maxTurns, outputFormat, bo.GetLLMConfig())
}

// CreateStandardAgentConfigWithCustomServers creates a standardized agent configuration with custom MCP servers
// This allows specific agents to override the default MCP server list
func (bo *BaseOrchestrator) CreateStandardAgentConfigWithCustomServers(agentName string, maxTurns int, outputFormat agents.OutputFormat, customServers []string) *agents.OrchestratorAgentConfig {
	config := bo.createAgentConfigWithLLM(agentName, maxTurns, outputFormat, bo.GetLLMConfig())

	// Override the server names with custom servers
	config.ServerNames = customServers

	bo.GetLogger().Infof("🔧 Created agent config for %s with custom MCP servers: %v", agentName, customServers)
	return config
}

// CreateStandardAgentConfigWithLLM creates a standardized agent configuration with custom LLM config
// This allows specific agents to override the default LLM configuration
func (bo *BaseOrchestrator) CreateStandardAgentConfigWithLLM(agentName string, maxTurns int, outputFormat agents.OutputFormat, llmConfig *LLMConfig) *agents.OrchestratorAgentConfig {
	return bo.createAgentConfigWithLLM(agentName, maxTurns, outputFormat, llmConfig)
}

// createAgentConfigWithLLM creates a generic agent configuration with detailed LLM config
func (bo *BaseOrchestrator) createAgentConfigWithLLM(agentName string, maxTurns int, outputFormat agents.OutputFormat, llmConfig *LLMConfig) *agents.OrchestratorAgentConfig {
	config := agents.NewOrchestratorAgentConfig(agentName)

	// Store the unique agent name for use in agent initialization
	config.AgentName = agentName

	// Use detailed LLM configuration from frontend if available
	llmProvider := bo.GetProvider()
	llmModel := bo.GetModel()
	// Use orchestrator-configured temperature unless an agent must override explicitly
	llmTemp := bo.GetTemperature()

	if llmConfig != nil {
		llmProvider = llmConfig.Provider
		llmModel = llmConfig.ModelID
		bo.GetLogger().Infof("🔧 Using detailed LLM config for %s agent - Provider: %s, Model: %s",
			agentName, llmProvider, llmModel)
	}

	config.Provider = llmProvider
	config.Model = llmModel
	config.Temperature = llmTemp // Uses orchestrator-configured temperature
	config.MCPConfigPath = bo.GetMCPConfigPath()
	config.MaxTurns = maxTurns
	config.ToolChoice = "auto"
	config.ServerNames = bo.GetSelectedServers()
	config.SelectedTools = bo.GetSelectedTools()               // NEW field
	config.UseCodeExecutionMode = bo.GetUseCodeExecutionMode() // NEW field
	config.Mode = agents.AgentMode(bo.GetAgentMode())
	config.OutputFormat = outputFormat
	config.MaxRetries = 3
	config.Timeout = 300 // Same timeout for all agents
	config.RateLimit = 60

	// Detailed LLM configuration from frontend
	if llmConfig != nil {
		config.FallbackModels = llmConfig.FallbackModels
		config.CrossProviderFallback = llmConfig.CrossProviderFallback
		// Convert API keys from orchestrator format to agent format
		if llmConfig.APIKeys != nil {
			config.APIKeys = &agents.AgentAPIKeys{
				OpenRouter: llmConfig.APIKeys.OpenRouter,
				OpenAI:     llmConfig.APIKeys.OpenAI,
				Anthropic:  llmConfig.APIKeys.Anthropic,
				Vertex:     llmConfig.APIKeys.Vertex,
			}
			if llmConfig.APIKeys.Bedrock != nil {
				config.APIKeys.Bedrock = &agents.BedrockAgentConfig{
					Region: llmConfig.APIKeys.Bedrock.Region,
				}
			}
		}
	}

	return config
}

// setupStandardAgent is a private helper that performs the common setup logic for all agent creation methods
// It handles initialization, event bridge connection, and tool registration
func (bo *BaseOrchestrator) setupStandardAgent(
	ctx context.Context,
	agent agents.OrchestratorAgent,
	agentName string,
	phase string,
	step, iteration int,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
) error {
	// Initialize agent
	if err := agent.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize %s: %w", agentName, err)
	}

	// Validate essentials and connect event bridge
	eventBridge := bo.GetContextAwareBridge()
	if eventBridge == nil {
		return fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	bo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// 🔗 Connect agent to orchestrator's main event bridge using existing bridge (reuse)
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, baseAgentName)
		// Ensure iteration folder is applied to bridge (for token persistence)
		// This ensures all agents automatically get the iteration folder if it's been set
		bo.applyIterationFolderToBridge()
		mcpAgent.AddEventListener(cab)
		bo.GetLogger().Infof("🔗 Reused context-aware bridge connected to %s (step %d, agent %s)", phase, step+1, baseAgentName)
		bo.GetLogger().Infof("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase)
	} else {
		// Fallback for interface-based bridge
		if cab, ok := eventBridge.(interface {
			SetOrchestratorContext(phase string, step int, agentName string)
		}); ok {
			cab.SetOrchestratorContext(phase, step, baseAgentName)
			// Ensure iteration folder is applied to bridge (for token persistence)
			bo.applyIterationFolderToBridge()
			mcpAgent.AddEventListener(eventBridge)
			bo.GetLogger().Infof("🔗 Reused context-aware bridge connected to %s (step %d, agent %s)", phase, step+1, baseAgentName)
			bo.GetLogger().Infof("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase)
		} else {
			return fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
		}
	}

	// Register custom tools
	if customTools != nil && customToolExecutors != nil {
		if err := bo.registerCustomToolsForAgent(mcpAgent, baseAgent, agentName, customTools, customToolExecutors); err != nil {
			return err
		}
	}

	return nil
}

// registerCustomToolsForAgent registers custom tools for an agent with folder guard and category validation
func (bo *BaseOrchestrator) registerCustomToolsForAgent(
	mcpAgent *mcpagent.Agent,
	baseAgent *agents.BaseAgent,
	agentName string,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
) error {
	// Filter out write tools if there's no write access
	filteredTools := make([]llmtypes.Tool, 0, len(customTools))
	for _, tool := range customTools {
		if tool.Function != nil && !bo.ShouldFilterWriteTool(tool.Function.Name) {
			filteredTools = append(filteredTools, tool)
		} else if tool.Function != nil && bo.ShouldFilterWriteTool(tool.Function.Name) {
			bo.GetLogger().Infof("🚫 Filtering out write tool %s (no write access)", tool.Function.Name)
		}
	}

	// Wrap executors and enhance tool descriptions with folder guard (automatic)
	filteredTools, wrappedExecutors := bo.PrepareWorkspaceToolsWithFolderGuard(filteredTools, customToolExecutors)

	bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(customTools), agentName, baseAgent.GetMode())
	if bo.ToolCategories != nil {
		bo.GetLogger().Infof("🔍 [DISCOVERY] ToolCategories map has %d entries", len(bo.ToolCategories))
		// Log ALL entries for debugging (not just first 10)
		for toolName, category := range bo.ToolCategories {
			bo.GetLogger().Infof("🔍 [DISCOVERY]   - %s -> %s", toolName, category)
		}
	} else {
		bo.GetLogger().Warnf("🔍 [DISCOVERY] ToolCategories map is nil - all tools will default to 'custom' category")
	}

	// Also log all tool names being registered for comparison
	bo.GetLogger().Infof("🔍 [DISCOVERY] Tools being registered (count: %d):", len(customTools))
	for _, tool := range customTools {
		if tool.Function != nil {
			bo.GetLogger().Infof("🔍 [DISCOVERY]   - Tool name: %s", tool.Function.Name)
		}
	}

	bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode) (filtered from %d)", len(filteredTools), agentName, baseAgent.GetMode(), len(customTools))

	for _, tool := range filteredTools {
		if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
			// Convert Parameters to map[string]interface{}
			var params map[string]interface{}
			if tool.Function.Parameters != nil {
				paramsBytes, err := json.Marshal(tool.Function.Parameters)
				if err == nil {
					if err := json.Unmarshal(paramsBytes, &params); err != nil {
						bo.GetLogger().Warnf("Warning: Failed to unmarshal parameters for tool %s: %v", tool.Function.Name, err)
						params = nil
					}
				}
			}
			if params == nil {
				bo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
				continue
			}

			// Type assert executor to function type
			if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
				// Get tool category from stored map - REQUIRED, no default
				// All tools must have a category from ToolCategories map
				var toolCategory string
				if bo.ToolCategories != nil {
					if cat, exists := bo.ToolCategories[tool.Function.Name]; exists {
						toolCategory = cat
						bo.GetLogger().Infof("🔍 [DISCOVERY] Tool %s assigned category: %s", tool.Function.Name, toolCategory)
					} else {
						// Tool not found in map - throw error
						bo.GetLogger().Errorf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name)
						bo.GetLogger().Errorf("❌ [DISCOVERY] Available keys in ToolCategories map: %v", getMapKeys(bo.ToolCategories))
						bo.GetLogger().Errorf("❌ [DISCOVERY] Tool name being looked up: '%s' (len=%d)", tool.Function.Name, len(tool.Function.Name))
						return fmt.Errorf("tool %s not found in ToolCategories map - category is REQUIRED", tool.Function.Name)
					}
				} else {
					bo.GetLogger().Errorf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name)
					return fmt.Errorf("ToolCategories map is nil - category is REQUIRED for tool %s", tool.Function.Name)
				}

				// Validate category is not empty
				if toolCategory == "" {
					return fmt.Errorf("tool %s has empty category - category is REQUIRED", tool.Function.Name)
				}

				if err := mcpAgent.RegisterCustomTool(
					tool.Function.Name,
					tool.Function.Description,
					params,
					toolExecutor,
					toolCategory,
				); err != nil {
					return fmt.Errorf("failed to register tool %s: %w", tool.Function.Name, err)
				}
			} else {
				bo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
			}
		}
	}

	// Log summary of category assignments
	categorySummary := make(map[string]int)
	for _, tool := range customTools {
		if tool.Function != nil {
			toolName := tool.Function.Name
			category := "custom"
			if bo.ToolCategories != nil {
				if cat, exists := bo.ToolCategories[toolName]; exists {
					category = cat
				}
			}
			categorySummary[category]++
		}
	}
	bo.GetLogger().Infof("🔍 [DISCOVERY] Category assignment summary:")
	for category, count := range categorySummary {
		bo.GetLogger().Infof("🔍 [DISCOVERY]   - %s: %d tools", category, count)
	}

	bo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())

	// 🔧 CRITICAL FIX: Explicitly update code execution registry after all tools are registered
	// This ensures workspace and human tools are available in code execution mode
	if bo.GetUseCodeExecutionMode() {
		if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
			bo.GetLogger().Warnf("⚠️ Failed to update code execution registry for %s: %v", agentName, err)
			// Don't fail agent creation if registry update fails, but log the warning
		} else {
			bo.GetLogger().Infof("✅ [CODE_EXECUTION] Registry updated for %s agent - workspace and human tools are now available", agentName)
		}
	}

	return nil
}

// CreateAndSetupStandardAgent creates and sets up an agent with standardized configuration
func (bo *BaseOrchestrator) CreateAndSetupStandardAgent(
	ctx context.Context,
	agentName string,
	phase string,
	step, iteration int,
	maxTurns int,
	outputFormat agents.OutputFormat,
	createAgentFunc func(*agents.OrchestratorAgentConfig, utils.ExtendedLogger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
) (agents.OrchestratorAgent, error) {
	// Create standardized agent configuration using agentName as agentType
	config := bo.CreateStandardAgentConfig(agentName, maxTurns, outputFormat)

	// Create agent using provided factory function
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Setup agent using common helper
	if err := bo.setupStandardAgent(ctx, agent, agentName, phase, step, iteration, customTools, customToolExecutors); err != nil {
		return nil, err
	}

	return agent, nil
}

// CreateAndSetupStandardAgentWithCustomServers creates and sets up an agent with custom MCP servers
// This allows specific agents to override the default MCP server list
func (bo *BaseOrchestrator) CreateAndSetupStandardAgentWithCustomServers(
	ctx context.Context,
	agentName string,
	phase string,
	step, iteration int,
	maxTurns int,
	outputFormat agents.OutputFormat,
	customServers []string,
	createAgentFunc func(*agents.OrchestratorAgentConfig, utils.ExtendedLogger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
) (agents.OrchestratorAgent, error) {
	// Create standardized agent configuration with custom servers
	config := bo.CreateStandardAgentConfigWithCustomServers(agentName, maxTurns, outputFormat, customServers)

	// Create agent using provided factory function
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Setup agent using common helper
	if err := bo.setupStandardAgent(ctx, agent, agentName, phase, step, iteration, customTools, customToolExecutors); err != nil {
		return nil, err
	}

	return agent, nil
}

// CreateAndSetupStandardAgentWithConfig creates and sets up an agent with a pre-created configuration
// This allows agents to have full control over config (custom LLM, servers, EnableLargeOutputVirtualTools, etc.)
// while still using the standard setup logic (initialization, event bridge connection, tool registration)
func (bo *BaseOrchestrator) CreateAndSetupStandardAgentWithConfig(
	ctx context.Context,
	config *agents.OrchestratorAgentConfig,
	phase string,
	step, iteration int,
	createAgentFunc func(*agents.OrchestratorAgentConfig, utils.ExtendedLogger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
	overwriteSystemPrompt bool,
) (agents.OrchestratorAgent, error) {
	// Apply overwriteSystemPrompt parameter to config so callers can override default system prompt behavior
	config.OverwriteSystemPrompt = &overwriteSystemPrompt

	// Create agent using provided factory function with pre-created config
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Setup agent using common helper
	if err := bo.setupStandardAgent(ctx, agent, config.AgentName, phase, step, iteration, customTools, customToolExecutors); err != nil {
		return nil, err
	}

	return agent, nil
}

// CreateAndSetupStandardAgentWithSystemPrompt creates and sets up an agent with system prompt and user message processors
// This allows agents to have detailed system prompts while keeping user messages simple
func (bo *BaseOrchestrator) CreateAndSetupStandardAgentWithSystemPrompt(
	ctx context.Context,
	agentName string,
	phase string,
	step, iteration int,
	maxTurns int,
	outputFormat agents.OutputFormat,
	systemPromptProcessor func(map[string]string) string,
	userMessageProcessor func(map[string]string) string,
	createAgentFunc func(*agents.OrchestratorAgentConfig, utils.ExtendedLogger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
) (agents.OrchestratorAgent, error) {
	// Create standardized agent configuration using agentName as agentType
	config := bo.CreateStandardAgentConfig(agentName, maxTurns, outputFormat)

	// Create agent using provided factory function
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Initialize agent
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize %s: %w", agentName, err)
	}

	// Set user message processor if provided
	// Since agents embed *BaseOrchestratorAgent, methods are promoted
	// Note: systemPromptProcessor is now passed as parameter to Execute methods, not set here
	if userMessageProcessor != nil {
		if settable, ok := agent.(agents.UserMessageProcessorSetter); ok {
			settable.SetUserMessageProcessor(userMessageProcessor)
			bo.GetLogger().Infof("✅ User message processor set for %s", agentName)
		} else {
			bo.GetLogger().Warnf("⚠️ Could not set user message processor for %s - agent does not implement UserMessageProcessorSetter", agentName)
		}
	}

	// Setup agent using common helper (skips initialization since we already did it)
	// We need to manually do the setup since we already initialized
	eventBridge := bo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	bo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return nil, fmt.Errorf("base agent is nil for %s", agentName)
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return nil, fmt.Errorf("MCP agent is nil for %s", agentName)
	}

	// 🔗 Connect agent to orchestrator's main event bridge using existing bridge (reuse)
	baseAgentName := baseAgent.GetName()
	if cab, ok := eventBridge.(*ContextAwareEventBridge); ok {
		cab.SetOrchestratorContext(phase, step, baseAgentName)
		// Ensure iteration folder is applied to bridge (for token persistence)
		// This ensures all agents automatically get the iteration folder if it's been set
		bo.applyIterationFolderToBridge()
		mcpAgent.AddEventListener(cab)
		bo.GetLogger().Infof("🔗 Reused context-aware bridge connected to %s (step %d, agent %s)", phase, step+1, baseAgentName)
		bo.GetLogger().Infof("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	// Register custom tools
	if customTools != nil && customToolExecutors != nil {
		if err := bo.registerCustomToolsForAgent(mcpAgent, baseAgent, agentName, customTools, customToolExecutors); err != nil {
			return nil, err
		}
	}

	return agent, nil
}
