package orchestrator

import (
	"context"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/conditional"

	"github.com/tmc/langchaingo/llms"
)

// OrchestratorType represents the type of orchestrator
type OrchestratorType string

const (
	OrchestratorTypePlanner  OrchestratorType = "planner"
	OrchestratorTypeWorkflow OrchestratorType = "workflow"
)

// BaseOrchestrator provides unified functionality for all orchestrators
type BaseOrchestrator struct {
	// Base orchestrator agent for MCP tool access and event emission
	*agents.BaseOrchestratorAgent

	// Event bridge for frontend communication
	eventBridge interface{}

	// Fallback logger for when AgentTemplate is nil
	fallbackLogger utils.ExtendedLogger

	// Workspace tools for file operations
	WorkspaceTools         []llms.Tool
	WorkspaceToolExecutors map[string]interface{}

	// Conditional LLM for decision making
	conditionalLLM *conditional.ConditionalLLM

	// Orchestrator type and configuration
	orchestratorType OrchestratorType
	startTime        time.Time

	// Optional simple state (for workflow orchestrators)
	objective     string
	workspacePath string
}

// NewBaseOrchestrator creates a new unified base orchestrator
func NewBaseOrchestrator(
	config *agents.OrchestratorAgentConfig,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	eventBridge interface{},
	agentType agents.AgentType,
	orchestratorType OrchestratorType,
) (*BaseOrchestrator, error) {

	// Create base orchestrator agent for MCP tool access and event emission
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agentType,
		nil, // No event bridge for base orchestrator
	)

	// Create conditional LLM factory
	conditionalLLMFactory := conditional.NewConditionalLLMFactory(logger, tracer)

	// Create conditional LLM with automatic event emission
	conditionalLLM, err := conditionalLLMFactory.CreateConditionalLLMWithEventBridge(config, eventBridge)
	if err != nil {
		return nil, fmt.Errorf("failed to create conditional LLM: %w", err)
	}

	return &BaseOrchestrator{
		BaseOrchestratorAgent:  baseAgent,
		eventBridge:            eventBridge,
		fallbackLogger:         logger,
		WorkspaceTools:         nil, // Will be set via SetWorkspaceTools
		WorkspaceToolExecutors: make(map[string]interface{}),
		conditionalLLM:         conditionalLLM,
		orchestratorType:       orchestratorType,
		startTime:              time.Now(),
	}, nil
}

// getLogger safely gets the logger, with fallback if AgentTemplate is nil
func (bo *BaseOrchestrator) getLogger() utils.ExtendedLogger {
	if bo.BaseOrchestratorAgent != nil && bo.AgentTemplate != nil {
		return bo.AgentTemplate.GetLogger()
	}
	// Return the fallback logger
	return bo.fallbackLogger
}

// getConfig safely gets the config, with fallback if AgentTemplate is nil
func (bo *BaseOrchestrator) getConfig() *agents.OrchestratorAgentConfig {
	if bo.BaseOrchestratorAgent != nil && bo.AgentTemplate != nil {
		return bo.AgentTemplate.GetConfig()
	}
	// Return a default config as fallback
	return &agents.OrchestratorAgentConfig{
		ServerNames: []string{},
		Model:       "unknown",
		Provider:    "unknown",
		MaxTurns:    10,
	}
}

// emitEvent emits an event through the event bridge
func (bo *BaseOrchestrator) emitEvent(ctx context.Context, eventType events.EventType, data events.EventData) {
	if bo.eventBridge == nil {
		bo.getLogger().Warnf("⚠️ No event bridge available, cannot emit event: %s", eventType)
		return
	}

	// Create agent event
	agentEvent := &events.AgentEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	// Emit through event bridge - cast to the proper interface
	if bridge, ok := bo.eventBridge.(interface {
		HandleEvent(context.Context, *events.AgentEvent) error
	}); ok {
		if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
			bo.getLogger().Warnf("⚠️ Failed to emit event %s: %v", eventType, err)
		}
	} else {
		bo.getLogger().Warnf("⚠️ Event bridge does not implement HandleEvent method: %T", bo.eventBridge)
	}
}

// EmitOrchestratorStart emits an orchestrator start event
func (bo *BaseOrchestrator) EmitOrchestratorStart(ctx context.Context, objective string, agentsCount int, executionMode string) {
	bo.getLogger().Infof("📤 Emitting orchestrator start event")

	eventData := &events.OrchestratorStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		Objective:     objective,
		AgentsCount:   agentsCount,
		ServersCount:  len(bo.getConfig().ServerNames),
		ExecutionMode: executionMode,
	}

	bo.emitEvent(ctx, events.OrchestratorStart, eventData)
}

// EmitOrchestratorEnd emits an orchestrator end event
func (bo *BaseOrchestrator) EmitOrchestratorEnd(ctx context.Context, objective, result, status, message string, executionMode string) {
	bo.getLogger().Infof("📤 Emitting orchestrator end event: %s", status)

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
func (bo *BaseOrchestrator) EmitAgentStart(ctx context.Context, agentType, agentName, objective string, stepIndex, iteration int, templateVars map[string]string) {
	bo.getLogger().Infof("📤 Emitting agent start event: %s", agentName)

	eventData := &events.OrchestratorAgentStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:    agentType,
		AgentName:    agentName,
		Objective:    objective,
		InputData:    templateVars,
		ModelID:      bo.getConfig().Model,
		Provider:     bo.getConfig().Provider,
		ServersCount: len(bo.getConfig().ServerNames),
		MaxTurns:     bo.getConfig().MaxTurns,
		StepIndex:    stepIndex,
		Iteration:    iteration,
	}

	bo.emitEvent(ctx, events.OrchestratorAgentStart, eventData)
}

// EmitAgentEnd emits an agent end event
func (bo *BaseOrchestrator) EmitAgentEnd(ctx context.Context, agentType, agentName, objective, result string, stepIndex, iteration int, duration time.Duration) {
	bo.getLogger().Infof("📤 Emitting agent end event: %s", agentName)

	eventData := &events.OrchestratorAgentEndEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:    agentType,
		AgentName:    agentName,
		Objective:    objective,
		InputData:    make(map[string]string), // Empty - input data is captured in start event
		Result:       result,
		Success:      true, // Assume success unless explicitly set otherwise
		Duration:     duration,
		ModelID:      bo.getConfig().Model,
		Provider:     bo.getConfig().Provider,
		ServersCount: len(bo.getConfig().ServerNames),
		MaxTurns:     bo.getConfig().MaxTurns,
		StepIndex:    stepIndex,
		Iteration:    iteration,
	}

	bo.emitEvent(ctx, events.OrchestratorAgentEnd, eventData)
}

// Execute implements the OrchestratorAgent interface with automatic orchestrator event emission
func (bo *BaseOrchestrator) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract objective from template variables
	objective := templateVars["Objective"]
	if objective == "" {
		objective = templateVars["objective"] // Try lowercase as fallback
	}
	if objective == "" {
		objective = fmt.Sprintf("%s execution", bo.orchestratorType) // Default objective
	}

	// Auto-emit orchestrator start event with appropriate agent count
	agentsCount := 3 // Default for workflow
	if bo.orchestratorType == OrchestratorTypePlanner {
		agentsCount = 5 // planning, execution, validation, plan organizer, report generation
	}
	bo.EmitOrchestratorStart(ctx, objective, agentsCount, "sequential_execution") // Default mode

	// Delegate to BaseOrchestratorAgent's ExecuteWithInputProcessor for agent event emission
	result, err := bo.BaseOrchestratorAgent.ExecuteWithInputProcessor(ctx, templateVars, func(vars map[string]string) string {
		// Simple input processor - just return the first value or a default message
		if msg, exists := vars["userMessage"]; exists {
			return msg
		}
		if obj, exists := vars["Objective"]; exists {
			return obj
		}
		if obj, exists := vars["objective"]; exists {
			return obj
		}
		return fmt.Sprintf("%s execution", bo.orchestratorType)
	}, conversationHistory)

	// Auto-emit orchestrator end event
	status := "completed"
	if err != nil {
		status = "failed"
	}
	bo.EmitOrchestratorEnd(ctx, objective, result, status, "", "sequential_execution") // Default mode

	return result, err
}

// Initialize implements the OrchestratorAgent interface
func (bo *BaseOrchestrator) Initialize(ctx context.Context) error {
	bo.getLogger().Infof("🚀 Initializing BaseOrchestrator (%s)", bo.orchestratorType)
	// The orchestrator is already initialized in the constructor
	// This method exists to satisfy the OrchestratorAgent interface
	return nil
}

// Close implements the OrchestratorAgent interface
func (bo *BaseOrchestrator) Close() error {
	bo.getLogger().Infof("🔒 Closing BaseOrchestrator (%s)", bo.orchestratorType)
	// Clean up any resources if needed
	// For now, just log the close operation
	return nil
}

// GetBaseAgent implements the OrchestratorAgent interface
func (bo *BaseOrchestrator) GetBaseAgent() *agents.BaseAgent {
	// Return the actual base agent so it can receive workspace tools
	// The orchestrator utils will register workspace tools on this base agent
	if bo.BaseOrchestratorAgent != nil {
		return bo.BaseOrchestratorAgent.BaseAgent()
	}
	return nil
}

// SetWorkspaceTools sets workspace tools for file operations
func (bo *BaseOrchestrator) SetWorkspaceTools(tools []llms.Tool, executors map[string]interface{}) {
	// Add nil check to prevent panic if bo is nil
	if bo == nil {
		return
	}

	// Add nil check for BaseOrchestratorAgent field
	if bo.BaseOrchestratorAgent == nil {
		bo.fallbackLogger.Warnf("⚠️ BaseOrchestratorAgent is nil in SetWorkspaceTools")
		return
	}

	bo.WorkspaceTools = tools
	bo.WorkspaceToolExecutors = executors
	bo.getLogger().Infof("🔧 Workspace tools set for BaseOrchestrator (%s): %d tools", bo.orchestratorType, len(tools))
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

	bo.getLogger().Infof("🔧 Registering %d workspace tools for %s agent", len(bo.WorkspaceTools), agent.GetType())

	for _, tool := range bo.WorkspaceTools {
		if executor, exists := bo.WorkspaceToolExecutors[tool.Function.Name]; exists {
			// Type assert parameters to map[string]interface{}
			params, ok := tool.Function.Parameters.(map[string]interface{})
			if !ok {
				bo.getLogger().Warnf("Warning: Failed to convert parameters for tool %s", tool.Function.Name)
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
				bo.getLogger().Warnf("Warning: Failed to convert executor for tool %s", tool.Function.Name)
			}
		}
	}

	bo.getLogger().Infof("✅ All workspace tools registered for %s agent", agent.GetType())
	return nil
}

// ConnectAgentToEventBridge connects a sub-agent to the event bridge for proper event forwarding
func (bo *BaseOrchestrator) ConnectAgentToEventBridge(agent agents.OrchestratorAgent, phase string) error {
	if bo.eventBridge == nil {
		return fmt.Errorf("no event bridge available")
	}

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

	// Cast the event bridge to the proper interface
	bridge, ok := bo.eventBridge.(interface {
		HandleEvent(context.Context, *events.AgentEvent) error
		Name() string
	})
	if !ok {
		return fmt.Errorf("event bridge does not implement required interface")
	}

	// Create a context-aware event bridge wrapper
	contextAwareBridge := &ContextAwareEventBridge{
		underlyingBridge: bridge,
		logger:           bo.getLogger(),
	}
	contextAwareBridge.SetOrchestratorContext(phase, 0, 0, phase)

	// Connect the event bridge to receive detailed agent events
	mcpAgent.AddEventListener(contextAwareBridge)
	bo.getLogger().Infof("✅ Context-aware bridge connected to %s", phase)

	// Note: StartAgentSession is now handled at orchestrator level to avoid duplicate events
	bo.getLogger().Infof("ℹ️ Skipping StartAgentSession for %s - handled at orchestrator level", phase)

	return nil
}

// GetEventBridge returns the event bridge
func (bo *BaseOrchestrator) GetEventBridge() interface{} {
	return bo.eventBridge
}

// GetConditionalLLM returns the conditional LLM instance
func (bo *BaseOrchestrator) GetConditionalLLM() *conditional.ConditionalLLM {
	return bo.conditionalLLM
}

// GetStartTime returns the start time
func (bo *BaseOrchestrator) GetStartTime() time.Time {
	return bo.startTime
}

// GetOrchestratorType returns the orchestrator type
func (bo *BaseOrchestrator) GetOrchestratorType() OrchestratorType {
	return bo.orchestratorType
}

// SetOrchestratorContext sets the orchestrator context for event emission
func (bo *BaseOrchestrator) SetOrchestratorContext(stepIndex, iteration int, objective, agentName string) {
	// This method is required by the OrchestratorAgent interface
	// The actual context setting is handled by the ContextAwareEventBridge
	// This is a placeholder implementation for interface compliance
	bo.getLogger().Infof("🎯 SetOrchestratorContext called: step %d, iteration %d, objective: %s, agent: %s",
		stepIndex, iteration, objective, agentName)
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

// ContextAwareEventBridge wraps an existing AgentEventListener and adds orchestrator context
type ContextAwareEventBridge struct {
	underlyingBridge interface {
		HandleEvent(context.Context, *events.AgentEvent) error
		Name() string
	}
	currentPhase     string
	currentStep      int
	currentIteration int
	currentAgentName string
	logger           utils.ExtendedLogger
}

// SetOrchestratorContext sets the current orchestrator context
func (c *ContextAwareEventBridge) SetOrchestratorContext(phase string, step, iteration int, agentName string) {
	c.currentPhase = phase
	c.currentStep = step
	c.currentIteration = iteration
	c.currentAgentName = agentName
	c.logger.Infof("🎯 Set orchestrator context: %s (step %d, iteration %d)", phase, step+1, iteration+1)
}

// HandleEvent implements AgentEventListener interface
func (c *ContextAwareEventBridge) HandleEvent(ctx context.Context, event *events.AgentEvent) error {
	c.logger.Infof("🔍 ContextAwareBridge: Received event %s", event.Type)

	// Add orchestrator context to the event if we have context
	if c.currentPhase != "" {
		c.logger.Infof("🔍 ContextAwareBridge: Processing event %s with phase %s", event.Type, c.currentPhase)
	}

	// Forward the event to the underlying bridge
	return c.underlyingBridge.HandleEvent(ctx, event)
}

// Name returns the bridge name
func (c *ContextAwareEventBridge) Name() string {
	return fmt.Sprintf("ContextAwareBridge-%s", c.underlyingBridge.Name())
}
