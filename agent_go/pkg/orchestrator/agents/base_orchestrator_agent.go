package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/tmc/langchaingo/llms"

	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
)

// OrchestratorContext holds context information for event emission
type OrchestratorContext struct {
	StepIndex int
	Iteration int
	Objective string
	AgentName string
}

// BaseOrchestratorAgent provides common functionality for all orchestrator agents
type BaseOrchestratorAgent struct {
	AgentTemplate       *AgentTemplate
	tracer              observability.Tracer
	agentType           AgentType
	systemPrompt        string
	eventBridge         interface{}          // Event bridge for auto events
	orchestratorContext *OrchestratorContext // Context info for events
}

// NewBaseOrchestratorAgentWithEventBridge creates a new base orchestrator agent with event bridge
func NewBaseOrchestratorAgentWithEventBridge(
	config *OrchestratorAgentConfig,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	agentType AgentType,
	eventBridge interface{},
) *BaseOrchestratorAgent {
	return &BaseOrchestratorAgent{
		AgentTemplate: &AgentTemplate{
			config: config,
			logger: logger,
		},
		tracer:              tracer,
		agentType:           agentType,
		systemPrompt:        "", // Not used for base orchestrator
		eventBridge:         eventBridge,
		orchestratorContext: nil, // Will be set dynamically
	}
}

// SetOrchestratorContext sets the orchestrator context for event emission
func (boa *BaseOrchestratorAgent) SetOrchestratorContext(stepIndex, iteration int, objective, agentName string) {
	boa.orchestratorContext = &OrchestratorContext{
		StepIndex: stepIndex,
		Iteration: iteration,
		Objective: objective,
		AgentName: agentName,
	}
}

// Initialize initializes the base orchestrator agent
func (boa *BaseOrchestratorAgent) Initialize(ctx context.Context) error {
	// Create LLM instance
	llmInstance, err := boa.createLLM(ctx)
	if err != nil {
		return fmt.Errorf("failed to create LLM: %w", err)
	}

	// Create traceID
	traceID := observability.TraceID(fmt.Sprintf("%s-agent-%s-%d",
		boa.agentType,
		boa.AgentTemplate.config.Model,
		time.Now().UnixNano()))

	// Create base agent
	baseAgent, err := NewBaseAgent(
		ctx,
		boa.agentType,
		boa.AgentTemplate.config.Name,
		llmInstance,
		boa.systemPrompt,
		boa.AgentTemplate.config.ServerNames,
		boa.AgentTemplate.config.Mode,
		boa.tracer,
		traceID,
		boa.AgentTemplate.config.MCPConfigPath,
		boa.AgentTemplate.config.Model,
		boa.AgentTemplate.config.Temperature,
		boa.AgentTemplate.config.ToolChoice,
		boa.AgentTemplate.config.MaxTurns,
		boa.AgentTemplate.config.Provider,
		boa.AgentTemplate.logger,
		boa.AgentTemplate.config.CacheOnly,
	)
	if err != nil {
		return fmt.Errorf("failed to create base agent: %w", err)
	}

	boa.AgentTemplate.baseAgent = baseAgent

	// Append the agent-specific prompt to the existing system prompt
	boa.AgentTemplate.baseAgent.agent.AppendSystemPrompt(boa.systemPrompt)

	boa.AgentTemplate.logger.Infof("✅ Base Orchestrator Agent (%s) created successfully", boa.agentType)
	return nil
}

// ExecuteWithInputProcessor executes the agent with a custom input processor
func (boa *BaseOrchestratorAgent) ExecuteWithInputProcessor(ctx context.Context, templateVars map[string]string, inputProcessor func(map[string]string) string, conversationHistory []llms.MessageContent) (string, error) {
	startTime := time.Now()

	// Auto-emit agent start event
	boa.emitAgentStartEvent(ctx, templateVars)

	// Starting orchestrator agent execution

	// Process inputs using the provided processor function
	userMessage := inputProcessor(templateVars)

	// Delegate to template's Execute method which enforces event patterns
	baseAgentTemplateVars := map[string]string{
		"userMessage": userMessage,
	}
	result, err := boa.AgentTemplate.baseAgent.Execute(ctx, baseAgentTemplateVars, conversationHistory)

	duration := time.Since(startTime)

	// Auto-emit agent end event
	boa.emitAgentEndEvent(ctx, templateVars, result, err, duration)

	if err != nil {
		boa.AgentTemplate.logger.Errorf("❌ Base Orchestrator Agent (%s) execution failed: %v", boa.agentType, err)
		return "", fmt.Errorf("base orchestrator execution failed: %w", err)
	}

	// Orchestrator agent execution completed
	return result, nil
}

// GetType returns the agent type
func (boa *BaseOrchestratorAgent) GetType() string {
	return string(boa.agentType)
}

// GetConfig returns the agent configuration
func (boa *BaseOrchestratorAgent) GetConfig() *OrchestratorAgentConfig {
	return boa.AgentTemplate.config
}

// Close closes the base orchestrator agent
func (boa *BaseOrchestratorAgent) Close() error {
	if boa.AgentTemplate.baseAgent != nil {
		return boa.AgentTemplate.baseAgent.Close()
	}
	return nil
}

// BaseAgent returns the base agent
func (boa *BaseOrchestratorAgent) BaseAgent() *BaseAgent {
	return boa.AgentTemplate.baseAgent
}

// GetBaseAgent returns the base agent (implements OrchestratorAgent interface)
func (boa *BaseOrchestratorAgent) GetBaseAgent() *BaseAgent {
	return boa.AgentTemplate.baseAgent
}

// SetEventBridge sets the event bridge for the agent
func (boa *BaseOrchestratorAgent) SetEventBridge(bridge interface{}) {
	boa.eventBridge = bridge
}

// GetTracer returns the tracer
func (boa *BaseOrchestratorAgent) GetTracer() observability.Tracer {
	return boa.tracer
}

// GetEventBridge returns the event bridge
func (boa *BaseOrchestratorAgent) GetEventBridge() mcpagent.AgentEventListener {
	if bridge, ok := boa.eventBridge.(mcpagent.AgentEventListener); ok {
		return bridge
	}
	return nil
}

// emitEvent emits an event through the event bridge
func (boa *BaseOrchestratorAgent) emitEvent(ctx context.Context, eventType events.EventType, data events.EventData) {
	// Create agent event
	agentEvent := &events.AgentEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	// Emit through event bridge - cast to the proper interface
	if bridge, ok := boa.eventBridge.(interface {
		HandleEvent(context.Context, *events.AgentEvent) error
	}); ok {
		if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
			boa.AgentTemplate.logger.Warnf("⚠️ Failed to emit event %s: %v", eventType, err)
		}
	} else {
		boa.AgentTemplate.logger.Warnf("⚠️ Event bridge does not implement HandleEvent method: %T", boa.eventBridge)
	}
}

// emitAgentStartEvent emits an agent start event automatically
func (boa *BaseOrchestratorAgent) emitAgentStartEvent(ctx context.Context, templateVars map[string]string) {
	if boa.orchestratorContext == nil {
		return // No context available yet
	}

	eventData := &events.OrchestratorAgentStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:    string(boa.agentType),
		AgentName:    boa.orchestratorContext.AgentName,
		Objective:    boa.orchestratorContext.Objective,
		InputData:    templateVars,
		ModelID:      boa.AgentTemplate.config.Model,
		Provider:     boa.AgentTemplate.config.Provider,
		ServersCount: len(boa.AgentTemplate.config.ServerNames),
		MaxTurns:     boa.AgentTemplate.config.MaxTurns,
		StepIndex:    boa.orchestratorContext.StepIndex,
		Iteration:    boa.orchestratorContext.Iteration,
	}

	boa.emitEvent(ctx, events.OrchestratorAgentStart, eventData)
}

// emitAgentEndEvent emits an agent end event automatically
func (boa *BaseOrchestratorAgent) emitAgentEndEvent(ctx context.Context, templateVars map[string]string, result string, err error, duration time.Duration) {
	if boa.orchestratorContext == nil {
		return // No context available yet
	}

	success := err == nil
	if !success {
		result = err.Error()
	}

	eventData := &events.OrchestratorAgentEndEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:    string(boa.agentType),
		AgentName:    boa.orchestratorContext.AgentName,
		Objective:    boa.orchestratorContext.Objective,
		InputData:    templateVars,
		Result:       result,
		Success:      success,
		Duration:     duration,
		ModelID:      boa.AgentTemplate.config.Model,
		Provider:     boa.AgentTemplate.config.Provider,
		ServersCount: len(boa.AgentTemplate.config.ServerNames),
		MaxTurns:     boa.AgentTemplate.config.MaxTurns,
		StepIndex:    boa.orchestratorContext.StepIndex,
		Iteration:    boa.orchestratorContext.Iteration,
	}

	boa.emitEvent(ctx, events.OrchestratorAgentEnd, eventData)
}

// createLLM creates an LLM instance based on the agent configuration
func (boa *BaseOrchestratorAgent) createLLM(ctx context.Context) (llms.Model, error) {
	// Generate trace ID for this agent session
	traceID := observability.TraceID(fmt.Sprintf("%s-agent-%d", boa.agentType, time.Now().UnixNano()))

	// Build fallback models list
	var fallbackModels []string

	// Add custom fallback models from frontend if provided
	if len(boa.AgentTemplate.config.FallbackModels) > 0 {
		fallbackModels = append(fallbackModels, boa.AgentTemplate.config.FallbackModels...)
		// Using custom fallback models from frontend
	} else {
		// Use default fallback models for the provider
		fallbackModels = append(fallbackModels, llm.GetDefaultFallbackModels(llm.Provider(boa.AgentTemplate.config.Provider))...)
		// Using default fallback models for provider
	}

	// Add cross-provider fallback models if configured
	if boa.AgentTemplate.config.CrossProviderFallback != nil && len(boa.AgentTemplate.config.CrossProviderFallback.Models) > 0 {
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llm.Provider(boa.AgentTemplate.config.CrossProviderFallback.Provider))
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		// Added cross-provider fallback models
	} else {
		// Add default cross-provider fallbacks
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llm.Provider(boa.AgentTemplate.config.Provider))
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		// Added default cross-provider fallback models
	}

	// Create LLM configuration
	config := llm.Config{
		Provider:       llm.Provider(boa.AgentTemplate.config.Provider),
		ModelID:        boa.AgentTemplate.config.Model,
		Temperature:    boa.AgentTemplate.config.Temperature,
		Tracers:        nil, // Tracers will be set later if needed
		TraceID:        traceID,
		FallbackModels: fallbackModels,
		MaxRetries:     boa.AgentTemplate.config.MaxRetries,
		Logger:         boa.AgentTemplate.logger,
	}

	// Initialize LLM using the existing factory
	llmInstance, err := llm.InitializeLLM(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize LLM: %w", err)
	}

	return llmInstance, nil
}
