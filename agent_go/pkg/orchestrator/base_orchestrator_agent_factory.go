package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	virtualtools "mcp-agent-builder-go/agent_go/cmd/server/virtual-tools"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	orchEvents "mcp-agent-builder-go/agent_go/pkg/orchestrator/events"
	mcpagent "github.com/manishiitg/mcpagent/agent"
	baseevents "github.com/manishiitg/mcpagent/events"
	loggerv2 "github.com/manishiitg/mcpagent/logger/v2"
	"github.com/manishiitg/mcpagent/observability"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// BridgeSessionEventEmitter implements virtualtools.SessionEventEmitter
// by emitting events through the orchestrator's ContextAwareBridge.
// This ensures human_feedback/human_questions tools work correctly when
// called from workflow agents (not just chat API agents).
type BridgeSessionEventEmitter struct {
	Bridge mcpagent.AgentEventListener
}

func (e *BridgeSessionEventEmitter) EmitBlockingHumanFeedback(requestID, question, contextText string, yesNoOnly bool, yesLabel, noLabel string, options ...string) {
	now := time.Now()
	eventData := &orchEvents.BlockingHumanFeedbackEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: now,
		},
		Question:      question,
		AllowFeedback: !yesNoOnly && len(options) == 0,
		Context:       contextText,
		RequestID:     requestID,
		YesNoOnly:     yesNoOnly,
		YesLabel:      yesLabel,
		NoLabel:       noLabel,
		Options:       options,
	}
	agentEvent := &baseevents.AgentEvent{
		Type:      orchEvents.BlockingHumanFeedback,
		Timestamp: now,
		Data:      eventData,
	}
	if err := e.Bridge.HandleEvent(context.Background(), agentEvent); err != nil {
		// Best-effort emission; the tool will still wait for the response
	}
}

func (e *BridgeSessionEventEmitter) EmitBlockingHumanQuestions(requestID string, questions []map[string]string) {
	now := time.Now()
	var eventQuestions []orchEvents.BlockingHumanQuestionsQuestion
	for _, q := range questions {
		eventQuestions = append(eventQuestions, orchEvents.BlockingHumanQuestionsQuestion{
			ID:       q["id"],
			Question: q["question"],
		})
	}
	eventData := &orchEvents.BlockingHumanQuestionsEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: now,
		},
		RequestID: requestID,
		Questions: eventQuestions,
	}
	agentEvent := &baseevents.AgentEvent{
		Type:      orchEvents.BlockingHumanQuestions,
		Timestamp: now,
		Data:      eventData,
	}
	if err := e.Bridge.HandleEvent(context.Background(), agentEvent); err != nil {
		// Best-effort emission
	}
}

func (e *BridgeSessionEventEmitter) EmitPlanApproval(question, contextText, yesLabel string) {
	now := time.Now()
	eventData := &orchEvents.PlanApprovalEvent{
		BaseEventData: baseevents.BaseEventData{
			Timestamp: now,
		},
		Question: question,
		Context:  contextText,
		YesLabel: yesLabel,
	}
	agentEvent := &baseevents.AgentEvent{
		Type:      orchEvents.PlanApproval,
		Timestamp: now,
		Data:      eventData,
	}
	if err := e.Bridge.HandleEvent(context.Background(), agentEvent); err != nil {
		// Best-effort emission
	}
}

// CreateStandardAgentConfig creates a standardized agent configuration
// use CreateAndSetupStandardAgent instead which combines configuration and setup.
func (bo *BaseOrchestrator) CreateStandardAgentConfig(agentName string, maxTurns int, outputFormat agents.OutputFormat) *agents.OrchestratorAgentConfig {
	return bo.createAgentConfigWithLLM(agentName, maxTurns, outputFormat, bo.GetLLMConfig())
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

	// Use orchestrator-configured temperature unless an agent must override explicitly
	llmTemp := bo.GetTemperature()

	// Populate LLMConfig from orchestrator.LLMConfig (unified structure)
	if llmConfig != nil {
		// Copy Primary directly - no fallback to orchestrator default (LLM selection uses temp override → step config → preset LLM)
		config.LLMConfig.Primary = agents.LLMModel{
			Provider: llmConfig.Primary.Provider,
			ModelID:  llmConfig.Primary.ModelID,
			APIKey:   llmConfig.Primary.APIKey,
			Region:   llmConfig.Primary.Region,
		}
		// Copy Fallbacks
		for _, fallback := range llmConfig.Fallbacks {
			config.LLMConfig.Fallbacks = append(config.LLMConfig.Fallbacks, agents.LLMModel{
				Provider: fallback.Provider,
				ModelID:  fallback.ModelID,
				APIKey:   fallback.APIKey,
				Region:   fallback.Region,
			})
		}
		// Convert API keys from orchestrator format to agent format
		if llmConfig.APIKeys != nil {
			config.APIKeys = &agents.AgentAPIKeys{
				OpenRouter: llmConfig.APIKeys.OpenRouter,
				OpenAI:     llmConfig.APIKeys.OpenAI,
				Anthropic:  llmConfig.APIKeys.Anthropic,
				Vertex:     llmConfig.APIKeys.Vertex,
				GeminiCLI:  llmConfig.APIKeys.GeminiCLI,
				MiniMax:           llmConfig.APIKeys.MiniMax,
				MiniMaxCodingPlan: llmConfig.APIKeys.MiniMaxCodingPlan,
			}
			if llmConfig.APIKeys.Bedrock != nil {
				config.APIKeys.Bedrock = &agents.BedrockAgentConfig{
					Region: llmConfig.APIKeys.Bedrock.Region,
				}
			}
			if llmConfig.APIKeys.Azure != nil {
				config.APIKeys.Azure = &agents.AzureAgentConfig{
					Endpoint:   llmConfig.APIKeys.Azure.Endpoint,
					APIKey:     llmConfig.APIKeys.Azure.APIKey,
					APIVersion: llmConfig.APIKeys.Azure.APIVersion,
					Region:     llmConfig.APIKeys.Azure.Region,
				}
			}
		}
	} else {
		// No fallback to orchestrator defaults - llmConfig must be provided
		// LLM selection uses temp override → step config → preset LLM priority
		panic(fmt.Sprintf("CRITICAL: llmConfig is nil in createAgentConfigWithLLM() for agent %s - LLM config must be provided. LLM selection uses temp override → step config → preset LLM priority, no orchestrator default fallback.", agentName))
	}

	config.Temperature = llmTemp
	config.MCPConfigPath = bo.GetMCPConfigPath()
	config.MaxTurns = maxTurns
	config.ToolChoice = "auto"
	config.ServerNames = bo.GetSelectedServers()
	config.SelectedTools = bo.GetSelectedTools()
	config.UseCodeExecutionMode = bo.GetUseCodeExecutionMode()
	config.UseToolSearchMode = bo.GetUseToolSearchMode()
	config.PreDiscoveredTools = bo.GetPreDiscoveredTools()
	config.Mode = agents.AgentMode(bo.GetAgentMode())
	config.OutputFormat = outputFormat
	config.MaxRetries = 3
	config.Timeout = 300 // Same timeout for all agents
	config.RateLimit = 60

	// Inject MCP session ID for connection sharing across agents in the same workflow
	// When set, connections are stored in a session registry and reused
	// DEBUG: Panic if sessionID is empty to catch cases where it wasn't set properly
	if bo.mcpSessionID == "" {
		// PANIC for debugging: sessionID should always be set before creating agents
		// This helps catch cases where sessionID is not properly initialized before agent creation
		panic(fmt.Sprintf("CRITICAL: mcpSessionID is empty in BaseOrchestrator.createAgentConfigWithLLM() - cannot create agent without sessionID. SessionID must be set via SetMCPSessionID() before creating agents."))
	}
	config.MCPSessionID = bo.mcpSessionID

	// Context summarization configuration from orchestrator
	config.EnableContextSummarization = bo.GetEnableContextSummarization()
	config.SummarizeOnTokenThreshold = bo.GetSummarizeOnTokenThreshold()
	config.TokenThresholdPercent = bo.GetTokenThresholdPercent()
	config.SummarizeOnFixedTokenThreshold = bo.GetSummarizeOnFixedTokenThreshold()
	config.FixedTokenThreshold = bo.GetFixedTokenThreshold()
	config.SummaryKeepLastMessages = bo.GetSummaryKeepLastMessages()

	// Context editing configuration from orchestrator
	config.EnableContextEditing = bo.GetEnableContextEditing()
	config.ContextEditingThreshold = bo.GetContextEditingThreshold()
	config.ContextEditingTurnThreshold = bo.GetContextEditingTurnThreshold()

	// Context offloading configuration from orchestrator
	config.LargeOutputThreshold = bo.GetLargeOutputThreshold()

	return config
}

// setupStandardAgent is a private helper that performs the common setup logic for all agent creation methods
// It handles initialization, event bridge connection, and tool registration
func (bo *BaseOrchestrator) setupStandardAgent(
	ctx context.Context,
	agent agents.OrchestratorAgent,
	config *agents.OrchestratorAgentConfig,
	agentName string,
	phase string,
	step, iteration int,
	stepID string, // Step ID (e.g., "fetch-data", "process-results")
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
		cab.SetOrchestratorContext(phase, step, stepID, baseAgentName)
		// Ensure iteration folder is applied to bridge (for token persistence)
		// This ensures all agents automatically get the iteration folder if it's been set
		bo.applyIterationFolderToBridge()
		mcpAgent.AddEventListener(cab)
		// Removed verbose logging
	} else {
		// Fallback for interface-based bridge
		if cab, ok := eventBridge.(interface {
			SetOrchestratorContext(phase string, step int, stepID string, agentName string)
		}); ok {
			cab.SetOrchestratorContext(phase, step, stepID, baseAgentName)
			// Ensure iteration folder is applied to bridge (for token persistence)
			bo.applyIterationFolderToBridge()
			mcpAgent.AddEventListener(eventBridge)
			bo.GetLogger().Info(fmt.Sprintf("🔗 Reused context-aware bridge connected to %s (step %d, agent %s)", phase, step+1, baseAgentName))
			bo.GetLogger().Info(fmt.Sprintf("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase))
		} else {
			return fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
		}
	}

	// Register custom tools (pass config to check agent-specific code execution mode)
	if customTools != nil && customToolExecutors != nil {
		if err := bo.registerCustomToolsForAgent(mcpAgent, baseAgent, config, agentName, customTools, customToolExecutors); err != nil {
			return err
		}
	}

	return nil
}

// registerCustomToolsForAgent registers custom tools for an agent with folder guard and category validation
func (bo *BaseOrchestrator) registerCustomToolsForAgent(
	mcpAgent *mcpagent.Agent,
	baseAgent *agents.BaseAgent,
	config *agents.OrchestratorAgentConfig,
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

				// Wrap human tools to inject SessionEventEmitter via the orchestrator's bridge.
				// Without this, human_feedback/human_questions tools called from workflow agents
				// would silently skip event emission (no emitter in context) and time out.
				finalExecutor := toolExecutor
				if toolCategory == virtualtools.GetHumanToolCategory() && bo.GetContextAwareBridge() != nil {
					emitter := &BridgeSessionEventEmitter{Bridge: bo.GetContextAwareBridge()}
					origExec := toolExecutor
					finalExecutor = func(ctx context.Context, args map[string]interface{}) (string, error) {
						ctx = context.WithValue(ctx, virtualtools.SessionEventEmitterKey, emitter)
						return origExec(ctx, args)
					}
				}

				if err := mcpAgent.RegisterCustomTool(
					tool.Function.Name,
					tool.Function.Description,
					params,
					finalExecutor,
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
	// Check agent config first (if provided), otherwise fall back to orchestrator level
	useCodeExecutionMode := false
	if config != nil {
		useCodeExecutionMode = config.UseCodeExecutionMode
	} else {
		// Fallback to orchestrator level if config not provided (backward compatibility)
		useCodeExecutionMode = bo.GetUseCodeExecutionMode()
	}
	if useCodeExecutionMode {
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
	stepID string, // Step ID (e.g., "fetch-data", "process-results")
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

	// Setup agent using common helper (pass config to check agent-specific code execution mode)
	if err := bo.setupStandardAgent(ctx, agent, config, agentName, phase, step, iteration, stepID, customTools, customToolExecutors); err != nil {
		return nil, err
	}

	return agent, nil
}

// CreateAndSetupStandardAgentWithConfig creates and sets up an agent with a pre-created configuration
// This allows agents to have full control over config (custom LLM, servers, EnableContextOffloading, etc.)
// while still using the standard setup logic (initialization, event bridge connection, tool registration)
func (bo *BaseOrchestrator) CreateAndSetupStandardAgentWithConfig(
	ctx context.Context,
	config *agents.OrchestratorAgentConfig,
	phase string,
	step, iteration int,
	stepID string, // Step ID (e.g., "fetch-data", "process-results")
	createAgentFunc func(*agents.OrchestratorAgentConfig, loggerv2.Logger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
	customTools []llmtypes.Tool,
	customToolExecutors map[string]interface{},
	overwriteSystemPrompt bool,
) (agents.OrchestratorAgent, error) {
	// Apply overwriteSystemPrompt parameter to config so callers can override default system prompt behavior
	config.OverwriteSystemPrompt = &overwriteSystemPrompt

	// Create agent using provided factory function with pre-created config
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Setup agent using common helper (pass config to check agent-specific code execution mode)
	if err := bo.setupStandardAgent(ctx, agent, config, config.AgentName, phase, step, iteration, stepID, customTools, customToolExecutors); err != nil {
		return nil, err
	}

	return agent, nil
}
