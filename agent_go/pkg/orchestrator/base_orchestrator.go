package orchestrator

import (
	"context"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// EventBridge defines the interface for event bridges
type EventBridge interface {
	mcpagent.AgentEventListener
}

// Orchestrator defines the common interface for all orchestrators
type Orchestrator interface {
	// Execute performs the orchestration logic
	Execute(ctx context.Context, objective string, workspacePath string, options map[string]interface{}) (string, error)

	// GetType returns the orchestrator type
	GetType() string
}

// LLMConfig represents the LLM configuration from frontend
type LLMConfig struct {
	Provider              string                        `json:"provider"`
	ModelID               string                        `json:"model_id"`
	FallbackModels        []string                      `json:"fallback_models"`
	CrossProviderFallback *agents.CrossProviderFallback `json:"cross_provider_fallback,omitempty"`
}

// OrchestratorType represents the type of orchestrator
type OrchestratorType string

const (
	OrchestratorTypePlanner  OrchestratorType = "planner"
	OrchestratorTypeWorkflow OrchestratorType = "workflow"
)

// BaseOrchestrator provides unified base functionality for all orchestrators
type BaseOrchestrator struct {
	// Context-aware event bridge for orchestrator-level events
	contextAwareBridge mcpagent.AgentEventListener

	// Logger for the orchestrator
	logger utils.ExtendedLogger

	// Workspace tools for file operations
	WorkspaceTools         []llms.Tool
	WorkspaceToolExecutors map[string]interface{}

	// Orchestrator type and configuration
	orchestratorType OrchestratorType
	startTime        time.Time

	// Common configuration shared between orchestrators
	provider        string
	model           string
	mcpConfigPath   string
	temperature     float64
	agentMode       string
	selectedServers []string
	llmConfig       *LLMConfig // LLM configuration
	maxTurns        int        // Maximum turns for the orchestrator

	// Optional simple state (for workflow orchestrators)
	objective     string
	workspacePath string
}

// NewBaseOrchestrator creates a new unified base orchestrator
func NewBaseOrchestrator(
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	agentType agents.AgentType,
	orchestratorType OrchestratorType,
	provider string,
	model string,
	mcpConfigPath string,
	temperature float64,
	agentMode string,
	selectedServers []string,
	llmConfig *LLMConfig,
	maxTurns int,
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
) (*BaseOrchestrator, error) {

	// Create context-aware event bridge that wraps the main event bridge
	contextAwareBridge := NewContextAwareEventBridge(eventBridge, logger)

	return &BaseOrchestrator{
		contextAwareBridge:     contextAwareBridge,
		logger:                 logger,
		WorkspaceTools:         customTools,
		WorkspaceToolExecutors: customToolExecutors,
		orchestratorType:       orchestratorType,
		startTime:              time.Now(),
		// Common configuration
		provider:        provider,
		model:           model,
		mcpConfigPath:   mcpConfigPath,
		temperature:     temperature,
		agentMode:       agentMode,
		selectedServers: selectedServers,
		llmConfig:       llmConfig,
		maxTurns:        maxTurns,
	}, nil
}

// GetLogger returns the orchestrator's logger
func (bo *BaseOrchestrator) GetLogger() utils.ExtendedLogger {
	return bo.logger
}

// emitEvent emits an event through the event bridge
func (bo *BaseOrchestrator) emitEvent(ctx context.Context, eventType events.EventType, data events.EventData) {
	// Create agent event
	agentEvent := &events.AgentEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	// Emit through event bridge
	if err := bo.contextAwareBridge.HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to emit event %s: %v", eventType, err)
	}
}

// EmitOrchestratorStart emits an orchestrator start event
func (bo *BaseOrchestrator) EmitOrchestratorStart(ctx context.Context, objective string, agentsCount int, executionMode string) {
	bo.GetLogger().Infof("📤 Emitting orchestrator start event")

	eventData := &events.OrchestratorStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:     objective,
		AgentsCount:   agentsCount,
		ServersCount:  len(bo.selectedServers),
		ExecutionMode: executionMode,
	}

	bo.emitEvent(ctx, events.OrchestratorStart, eventData)
}

// EmitOrchestratorEnd emits an orchestrator end event
func (bo *BaseOrchestrator) EmitOrchestratorEnd(ctx context.Context, objective, result, status, message string, executionMode string) {
	bo.GetLogger().Infof("📤 Emitting orchestrator end event: %s", status)

	duration := time.Since(bo.startTime)
	eventData := &events.OrchestratorEndEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:     objective,
		Result:        result,
		Status:        status,
		Duration:      duration,
		ExecutionMode: executionMode,
	}

	bo.emitEvent(ctx, events.OrchestratorEnd, eventData)
}

// EmitAgentStart emits an agent start event
func (bo *BaseOrchestrator) EmitAgentStart(ctx context.Context, agentType, agentName, objective string, stepIndex, iteration int, templateVars map[string]string, executionMode string) {
	bo.GetLogger().Infof("📤 Emitting agent start event: %s", agentName)

	eventData := &events.OrchestratorAgentStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:     agentType,
		AgentName:     agentName,
		Objective:     objective,
		InputData:     templateVars,
		ModelID:       bo.llmConfig.ModelID,
		Provider:      bo.llmConfig.Provider,
		ServersCount:  len(bo.selectedServers),
		MaxTurns:      bo.maxTurns,
		StepIndex:     stepIndex,
		Iteration:     iteration,
		ExecutionMode: executionMode,
	}

	bo.emitEvent(ctx, events.OrchestratorAgentStart, eventData)
}

// EmitAgentEnd emits an agent end event
func (bo *BaseOrchestrator) EmitAgentEnd(ctx context.Context, agentType, agentName, objective, result string, stepIndex, iteration int, duration time.Duration, executionMode string) {
	bo.GetLogger().Infof("📤 Emitting agent end event: %s", agentName)

	eventData := &events.OrchestratorAgentEndEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:     agentType,
		AgentName:     agentName,
		Objective:     objective,
		InputData:     make(map[string]string), // Empty - input data is captured in start event
		Result:        result,
		Success:       true, // Assume success unless explicitly set otherwise
		Duration:      duration,
		ModelID:       bo.llmConfig.ModelID,
		Provider:      bo.llmConfig.Provider,
		ServersCount:  len(bo.selectedServers),
		MaxTurns:      bo.maxTurns,
		StepIndex:     stepIndex,
		Iteration:     iteration,
		ExecutionMode: executionMode,
	}

	bo.emitEvent(ctx, events.OrchestratorAgentEnd, eventData)
}

// RegisterWorkspaceTools registers workspace tools with a sub-agent
func (bo *BaseOrchestrator) RegisterWorkspaceTools(agent agents.OrchestratorAgent) error {
	// Add nil check to prevent panic
	if bo == nil {
		return fmt.Errorf("BaseOrchestrator is nil")
	}

	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return fmt.Errorf("agent has no base agent")
	}

	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return fmt.Errorf("base agent has no MCP agent")
	}

	bo.GetLogger().Infof("🔧 Registering %d workspace tools for %s agent", len(bo.WorkspaceTools), agent.GetType())

	for _, tool := range bo.WorkspaceTools {
		if executor, exists := bo.WorkspaceToolExecutors[tool.Function.Name]; exists {
			// Type assert parameters to map[string]interface{}
			params, ok := tool.Function.Parameters.(map[string]interface{})
			if !ok {
				bo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
				continue
			}

			// Type assert executor to function type
			if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
				mcpAgent.RegisterCustomTool(
					tool.Function.Name,
					tool.Function.Description,
					params,
					toolExecutor,
				)
			} else {
				bo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
			}
		}
	}

	bo.GetLogger().Infof("✅ All workspace tools registered for %s agent", agent.GetType())
	return nil
}

// ConnectAgentToEventBridge connects a sub-agent to the event bridge for proper event forwarding
func (bo *BaseOrchestrator) ConnectAgentToEventBridge(agent agents.OrchestratorAgent, phase string) error {
	// Get the base agent from the sub-agent
	baseAgent := agent.GetBaseAgent()
	if baseAgent == nil {
		return fmt.Errorf("agent has no BaseAgent")
	}

	// Get the MCP agent from the base agent
	mcpAgent := baseAgent.Agent()
	if mcpAgent == nil {
		return fmt.Errorf("base agent has no MCP agent")
	}

	// Create a dedicated context-aware bridge for this agent connection
	// This ensures proper context isolation per agent
	agentContextBridge := NewContextAwareEventBridge(bo.contextAwareBridge, bo.GetLogger())

	// Use agent type as the context name
	agentType := agent.GetType()

	agentContextBridge.SetOrchestratorContext("init", 0, 0, agentType)

	// Connect the dedicated bridge to receive agent events
	mcpAgent.AddEventListener(agentContextBridge)
	bo.GetLogger().Infof("🔗 Dedicated context-aware bridge connected to %s", phase)

	// Note: StartAgentSession is now handled at orchestrator level to avoid duplicate events
	bo.GetLogger().Infof("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase)

	return nil
}

// GetStartTime returns the start time
func (bo *BaseOrchestrator) GetStartTime() time.Time {
	return bo.startTime
}

// GetOrchestratorType returns the orchestrator type
func (bo *BaseOrchestrator) GetOrchestratorType() OrchestratorType {
	return bo.orchestratorType
}

// Workflow-specific methods (only available for workflow orchestrators)
// GetObjective returns the current objective
func (bo *BaseOrchestrator) GetObjective() string {
	return bo.objective
}

// SetObjective sets the objective
func (bo *BaseOrchestrator) SetObjective(objective string) {
	bo.objective = objective
}

// GetWorkspacePath returns the current workspace path
func (bo *BaseOrchestrator) GetWorkspacePath() string {
	return bo.workspacePath
}

// SetWorkspacePath sets the workspace path
func (bo *BaseOrchestrator) SetWorkspacePath(workspacePath string) {
	bo.workspacePath = workspacePath
}

// GetContextAwareBridge returns the context-aware event bridge
func (bo *BaseOrchestrator) GetContextAwareBridge() mcpagent.AgentEventListener {
	return bo.contextAwareBridge
}

// GetProvider returns the LLM provider
func (bo *BaseOrchestrator) GetProvider() string {
	return bo.provider
}

// GetModel returns the LLM model
func (bo *BaseOrchestrator) GetModel() string {
	return bo.model
}

// GetMCPConfigPath returns the MCP configuration path
func (bo *BaseOrchestrator) GetMCPConfigPath() string {
	return bo.mcpConfigPath
}

// GetTemperature returns the temperature setting
func (bo *BaseOrchestrator) GetTemperature() float64 {
	return bo.temperature
}

// GetAgentMode returns the agent mode
func (bo *BaseOrchestrator) GetAgentMode() string {
	return bo.agentMode
}

// GetSelectedServers returns the selected servers
func (bo *BaseOrchestrator) GetSelectedServers() []string {
	return bo.selectedServers
}

// GetLLMConfig returns the LLM configuration
func (bo *BaseOrchestrator) GetLLMConfig() *LLMConfig {
	return bo.llmConfig
}

// GetTracer returns the tracer (not implemented - orchestrator doesn't have its own tracer)
func (bo *BaseOrchestrator) GetTracer() observability.Tracer {
	// Orchestrators don't have their own tracer - they coordinate agents that have tracers
	return nil
}

// GetMaxTurns returns the maximum turns for the orchestrator
func (bo *BaseOrchestrator) GetMaxTurns() int {
	return bo.maxTurns
}

// GetType returns the orchestrator type
func (bo *BaseOrchestrator) GetType() string {
	return string(bo.orchestratorType)
}

// CreateStandardAgentConfig creates a standardized agent configuration using shared utilities
// NOTE: This method is exposed for internal orchestrator use. For standard agent creation,
// use CreateAndSetupStandardAgent instead which combines configuration and setup.
func (bo *BaseOrchestrator) CreateStandardAgentConfig(agentType, agentName string, maxTurns int, outputFormat agents.OutputFormat) *agents.OrchestratorAgentConfig {
	return bo.createAgentConfigWithLLM(agentType, agentName, maxTurns, outputFormat, bo.GetLLMConfig())
}

// createAgentConfigWithLLM creates a generic agent configuration with detailed LLM config
func (bo *BaseOrchestrator) createAgentConfigWithLLM(agentType, agentName string, maxTurns int, outputFormat agents.OutputFormat, llmConfig *LLMConfig) *agents.OrchestratorAgentConfig {
	config := agents.NewOrchestratorAgentConfig(agentType, agentName)

	// Use detailed LLM configuration from frontend if available
	llmProvider := bo.GetProvider()
	llmModel := bo.GetModel()
	// Temperature is always 0.0 for all agents
	llmTemp := 0.0

	if llmConfig != nil {
		llmProvider = llmConfig.Provider
		llmModel = llmConfig.ModelID
		bo.GetLogger().Infof("🔧 Using detailed LLM config for %s agent - Provider: %s, Model: %s",
			agentType, llmProvider, llmModel)
	}

	config.Provider = llmProvider
	config.Model = llmModel
	config.Temperature = llmTemp // Always 0.0
	config.MCPConfigPath = bo.GetMCPConfigPath()
	config.MaxTurns = maxTurns
	config.ToolChoice = "auto"
	config.CacheOnly = true // Use cache-only mode for smart routing optimization
	config.ServerNames = bo.GetSelectedServers()
	config.Mode = agents.AgentMode(bo.GetAgentMode())
	config.OutputFormat = outputFormat
	config.MaxRetries = 3
	config.Timeout = 300 // Same timeout for all agents
	config.RateLimit = 60

	// Detailed LLM configuration from frontend
	if llmConfig != nil {
		config.FallbackModels = llmConfig.FallbackModels
		config.CrossProviderFallback = llmConfig.CrossProviderFallback
	}

	return config
}

// CreateAndSetupStandardAgent creates and sets up an agent with standardized configuration
func (bo *BaseOrchestrator) CreateAndSetupStandardAgent(
	agentType, agentName string,
	phase string,
	step, iteration int,
	maxTurns int,
	outputFormat agents.OutputFormat,
	createAgentFunc func(*agents.OrchestratorAgentConfig, utils.ExtendedLogger, observability.Tracer, EventBridge) agents.OrchestratorAgent,
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
) (agents.OrchestratorAgent, error) {
	// Create standardized agent configuration
	config := bo.CreateStandardAgentConfig(agentType, agentName, maxTurns, outputFormat)

	// Create agent using provided factory function
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Setup agent with tools and event bridge
	if eventBridge := bo.GetContextAwareBridge(); eventBridge != nil {
		if bridge, ok := eventBridge.(EventBridge); ok {
			if err := bo.SetupStandardAgent(
				agent,
				agentType,
				agentName,
				phase,
				step,
				iteration,
				customTools,
				customToolExecutors,
				bridge,
			); err != nil {
				return nil, fmt.Errorf("failed to setup %s agent: %w", agentName, err)
			}
		}
	}

	return agent, nil
}

// SetupStandardAgent performs standardized agent setup with event bridge and tools
// NOTE: This method is exposed for internal orchestrator use. For standard agent creation,
// use CreateAndSetupStandardAgent instead which combines configuration and setup.
func (bo *BaseOrchestrator) SetupStandardAgent(
	agent agents.OrchestratorAgent,
	agentType, agentName string,
	phase string,
	step, iteration int,
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
	eventBridge EventBridge,
) error {
	return bo.setupAgent(
		agent,
		agentType,
		agentName,
		phase,
		step,
		iteration,
		customTools,
		customToolExecutors,
		eventBridge,
	)
}

// setupAgent performs common agent setup tasks
func (bo *BaseOrchestrator) setupAgent(
	agent agents.OrchestratorAgent,
	agentType, agentName string,
	phase string,
	step, iteration int,
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
	eventBridge EventBridge,
) error {
	ctx := context.Background()
	if err := agent.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize %s: %w", agentName, err)
	}

	// Connect event bridge and emit actual agent events
	if eventBridge != nil {
		bo.GetLogger().Infof("🔍 Checking agent structure for %s", agentName)

		baseAgent := agent.GetBaseAgent()
		if baseAgent == nil {
			bo.GetLogger().Infof("ℹ️ Agent %s is a pure orchestrator (no BaseAgent) - skipping agent event connection", agentName)
		} else {
			bo.GetLogger().Infof("✅ GetBaseAgent() returned non-nil for %s", agentName)

			mcpAgent := baseAgent.Agent()
			if mcpAgent == nil {
				bo.GetLogger().Warnf("⚠️ baseAgent.Agent() returned nil for %s", agentName)
			} else {
				bo.GetLogger().Infof("✅ baseAgent.Agent() returned non-nil for %s", agentName)

				// Create a context-aware event bridge for this sub-agent
				contextAwareBridge := NewContextAwareEventBridge(eventBridge, bo.GetLogger())
				contextAwareBridge.SetOrchestratorContext(phase, step, iteration, agentName)

				// Connect the event bridge to receive detailed agent events
				mcpAgent.AddEventListener(contextAwareBridge)
				bo.GetLogger().Infof("✅ Context-aware bridge connected to %s", agentName)

				// CRITICAL FIX: Update the agent's event bridge to use the context-aware bridge
				// This ensures orchestrator-level events also get context metadata
				if baseOrchestratorAgent, ok := agent.(interface {
					SetEventBridge(bridge interface{})
				}); ok {
					baseOrchestratorAgent.SetEventBridge(contextAwareBridge)
					bo.GetLogger().Infof("✅ Updated agent event bridge to context-aware bridge for %s", agentName)
				} else {
					bo.GetLogger().Warnf("⚠️ Agent %s does not support SetEventBridge method", agentName)
				}

				// Check if the agent has streaming capability
				if mcpAgent.HasStreamingCapability() {
					bo.GetLogger().Infof("✅ Agent %s has streaming capability", agentName)
				} else {
					bo.GetLogger().Warnf("⚠️ Agent %s does not have streaming capability", agentName)
				}

				// 🔗 CRITICAL: Also connect agent to orchestrator's main event bridge
				// This ensures ALL agents are connected to the shared orchestrator bridge
				if err := bo.ConnectAgentToEventBridge(agent, agentName); err != nil {
					bo.GetLogger().Warnf("⚠️ Failed to connect %s to orchestrator event bridge: %v", agentName, err)
				} else {
					bo.GetLogger().Infof("🔗 Agent %s connected to orchestrator's main event bridge", agentName)
				}
			}
		}
	}

	// Register custom tools
	if customTools != nil && customToolExecutors != nil {
		if baseAgent := agent.GetBaseAgent(); baseAgent != nil {
			if mcpAgent := baseAgent.Agent(); mcpAgent != nil {
				bo.GetLogger().Infof("🔧 Registering %d custom tools for %s agent (%s mode)", len(customTools), agentName, baseAgent.GetMode())

				for _, tool := range customTools {
					if executor, exists := customToolExecutors[tool.Function.Name]; exists {
						// Type assert parameters to map[string]interface{}
						params, ok := tool.Function.Parameters.(map[string]interface{})
						if !ok {
							bo.GetLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
							continue
						}

						// Type assert executor to function type
						if toolExecutor, ok := executor.(func(ctx context.Context, args map[string]interface{}) (string, error)); ok {
							mcpAgent.RegisterCustomTool(
								tool.Function.Name,
								tool.Function.Description,
								params,
								toolExecutor,
							)
						} else {
							bo.GetLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
						}
					}
				}

				bo.GetLogger().Infof("✅ All custom tools registered for %s agent (%s mode)", agentName, baseAgent.GetMode())
			} else {
				bo.GetLogger().Warnf("⚠️ %s base agent has no MCP agent", agentName)
			}
		} else {
			bo.GetLogger().Warnf("⚠️ %s has no base agent", agentName)
		}
	}

	return nil
}
