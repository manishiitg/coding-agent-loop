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
	eventBridge mcpagent.AgentEventListener,
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
		Objective:        objective,
		AgentsCount:      agentsCount,
		ServersCount:     len(bo.selectedServers),
		OrchestratorType: bo.GetType(),
		ExecutionMode:    executionMode,
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
		Objective:        objective,
		Result:           result,
		Status:           status,
		Duration:         duration,
		OrchestratorType: bo.GetType(),
		ExecutionMode:    executionMode,
	}

	bo.emitEvent(ctx, events.OrchestratorEnd, eventData)
}

// EmitUnifiedCompletionEvent emits a unified completion event
func (bo *BaseOrchestrator) EmitUnifiedCompletionEvent(ctx context.Context, agentType, agentMode, question, finalResult, status string, turns int) {
	bo.GetLogger().Infof("📤 Emitting unified completion event: %s", status)

	duration := time.Since(bo.startTime)
	completionEventData := events.NewUnifiedCompletionEvent(
		agentType,
		agentMode,
		question,
		finalResult,
		status,
		duration,
		turns,
	)

	agentEvent := events.NewAgentEvent(completionEventData)

	// Emit through event bridge directly
	if err := bo.contextAwareBridge.HandleEvent(ctx, agentEvent); err != nil {
		bo.GetLogger().Warnf("⚠️ Failed to emit unified completion event: %v", err)
	}
}

// ConnectAgentToEventBridge connects a sub-agent to the event bridge for proper event forwarding
// ConnectAgentToEventBridge removed: logic now inlined in CreateAndSetupStandardAgent

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

// CreateStandardAgentConfig creates a standardized agent configuration
// use CreateAndSetupStandardAgent instead which combines configuration and setup.
func (bo *BaseOrchestrator) CreateStandardAgentConfig(agentName string, maxTurns int, outputFormat agents.OutputFormat) *agents.OrchestratorAgentConfig {
	return bo.createAgentConfigWithLLM(agentName, maxTurns, outputFormat, bo.GetLLMConfig())
}

// createAgentConfigWithLLM creates a generic agent configuration with detailed LLM config
func (bo *BaseOrchestrator) createAgentConfigWithLLM(agentName string, maxTurns int, outputFormat agents.OutputFormat, llmConfig *LLMConfig) *agents.OrchestratorAgentConfig {
	config := agents.NewOrchestratorAgentConfig(agentName)

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
	config.CacheOnly = false // Allow fresh connections when cache is not available
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
	ctx context.Context,
	agentName string,
	phase string,
	step, iteration int,
	maxTurns int,
	outputFormat agents.OutputFormat,
	createAgentFunc func(*agents.OrchestratorAgentConfig, utils.ExtendedLogger, observability.Tracer, mcpagent.AgentEventListener) agents.OrchestratorAgent,
	customTools []llms.Tool,
	customToolExecutors map[string]interface{},
) (agents.OrchestratorAgent, error) {
	// Create standardized agent configuration using agentName as agentType
	config := bo.CreateStandardAgentConfig(agentName, maxTurns, outputFormat)

	// Create agent using provided factory function
	agent := createAgentFunc(config, bo.GetLogger(), bo.GetTracer(), bo.GetContextAwareBridge())

	// Initialize and setup agent (inlined from setupAgent)
	if err := agent.Initialize(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize %s: %w", agentName, err)
	}

	// Validate essentials and connect event bridge
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
		cab.SetOrchestratorContext(phase, step, iteration, baseAgentName)
		mcpAgent.AddEventListener(cab)
		bo.GetLogger().Infof("🔗 Reused context-aware bridge connected to %s (step %d, iteration %d, agent %s)", phase, step+1, iteration+1, baseAgentName)
		bo.GetLogger().Infof("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase)
	} else {
		return nil, fmt.Errorf("context-aware bridge type mismatch for %s", agentName)
	}

	// Register custom tools
	if customTools != nil && customToolExecutors != nil {
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
	}

	return agent, nil
}

// SetupStandardAgent removed: setup is now performed inline in CreateAndSetupStandardAgent

// setupAgent removed: logic is now inlined in CreateAndSetupStandardAgent
