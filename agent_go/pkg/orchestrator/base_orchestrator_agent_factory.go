package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"

	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	loggerv2 "mcpagent/logger/v2"
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

	// Removed verbose logging
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
		// Removed verbose logging
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

	// Context summarization configuration from orchestrator
	config.EnableContextSummarization = bo.GetEnableContextSummarization()
	config.SummarizeOnTokenThreshold = bo.GetSummarizeOnTokenThreshold()
	config.TokenThresholdPercent = bo.GetTokenThresholdPercent()
	config.SummarizeOnFixedTokenThreshold = bo.GetSummarizeOnFixedTokenThreshold()
	config.FixedTokenThreshold = bo.GetFixedTokenThreshold()
	config.SummaryKeepLastMessages = bo.GetSummaryKeepLastMessages()

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

	// Removed verbose logging
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
		// Removed verbose logging
	} else {
		// Fallback for interface-based bridge
		if cab, ok := eventBridge.(interface {
			SetOrchestratorContext(phase string, step int, agentName string)
		}); ok {
			cab.SetOrchestratorContext(phase, step, baseAgentName)
			// Ensure iteration folder is applied to bridge (for token persistence)
			bo.applyIterationFolderToBridge()
			mcpAgent.AddEventListener(eventBridge)
			bo.GetLogger().Info(fmt.Sprintf("🔗 Reused context-aware bridge connected to %s (step %d, agent %s)", phase, step+1, baseAgentName))
			bo.GetLogger().Info(fmt.Sprintf("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase))
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
			// Removed verbose logging
		}
	}

	// Wrap executors and enhance tool descriptions with folder guard (automatic)
	filteredTools, wrappedExecutors := bo.PrepareWorkspaceToolsWithFolderGuard(filteredTools, customToolExecutors)

	// Removed excessive discovery logging

	for _, tool := range filteredTools {
		if executor, exists := wrappedExecutors[tool.Function.Name]; exists {
			// Convert Parameters to map[string]interface{}
			var params map[string]interface{}
			if tool.Function.Parameters != nil {
				paramsBytes, err := json.Marshal(tool.Function.Parameters)
				if err == nil {
					if err := json.Unmarshal(paramsBytes, &params); err != nil {
						bo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to unmarshal parameters for tool %s: %v", tool.Function.Name, err))
						params = nil
					}
				}
			}
			if params == nil {
				bo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to convert parameters for tool %s", tool.Function.Name))
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
						// Removed verbose logging
					} else {
						// Tool not found in map - throw error
						bo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Tool %s not found in ToolCategories map - category is REQUIRED!", tool.Function.Name), nil)
						bo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Available keys in ToolCategories map: %v", getMapKeys(bo.ToolCategories)), nil)
						bo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] Tool name being looked up: '%s' (len=%d)", tool.Function.Name, len(tool.Function.Name)), nil)
						return fmt.Errorf("tool %s not found in ToolCategories map - category is REQUIRED", tool.Function.Name)
					}
				} else {
					bo.GetLogger().Error(fmt.Sprintf("❌ [DISCOVERY] ToolCategories map is nil - category is REQUIRED for tool %s!", tool.Function.Name), nil)
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
				bo.GetLogger().Warn(fmt.Sprintf("Warning: Failed to convert executor for tool %s", tool.Function.Name))
			}
		}
	}

	// Removed excessive category summary logging

	// 🔧 CRITICAL FIX: Explicitly update code execution registry after all tools are registered
	// This ensures workspace and human tools are available in code execution mode
	if bo.GetUseCodeExecutionMode() {
		if err := mcpAgent.UpdateCodeExecutionRegistry(); err != nil {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Failed to update code execution registry for %s: %v", agentName, err))
			// Don't fail agent creation if registry update fails, but log the warning
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
	createAgentFunc func(*agents.OrchestratorAgentConfig, loggerv2.Logger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
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
	createAgentFunc func(*agents.OrchestratorAgentConfig, loggerv2.Logger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
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
	createAgentFunc func(*agents.OrchestratorAgentConfig, loggerv2.Logger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
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
	createAgentFunc func(*agents.OrchestratorAgentConfig, loggerv2.Logger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
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
			// Removed verbose logging
		} else {
			bo.GetLogger().Warn(fmt.Sprintf("⚠️ Could not set user message processor for %s - agent does not implement UserMessageProcessorSetter", agentName))
		}
	}

	// Setup agent using common helper (skips initialization since we already did it)
	// We need to manually do the setup since we already initialized
	eventBridge := bo.GetContextAwareBridge()
	if eventBridge == nil {
		return nil, fmt.Errorf("context-aware event bridge is nil for %s", agentName)
	}

	// Removed verbose logging
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
		// Removed verbose logging
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
