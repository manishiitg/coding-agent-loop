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
	config              *OrchestratorAgentConfig
	logger              utils.ExtendedLogger
	baseAgent           *BaseAgent // set during init
	tracer              observability.Tracer
	agentType           AgentType
	systemPrompt        string
	eventBridge         mcpagent.AgentEventListener // Event bridge for auto events
	orchestratorContext *OrchestratorContext        // Context info for events
}

// NewBaseOrchestratorAgentWithEventBridge creates a new base orchestrator agent with event bridge
func NewBaseOrchestratorAgentWithEventBridge(
	config *OrchestratorAgentConfig,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	agentType AgentType,
	eventBridge mcpagent.AgentEventListener,
) *BaseOrchestratorAgent {
	return &BaseOrchestratorAgent{
		config:              config,
		logger:              logger,
		tracer:              tracer,
		agentType:           agentType,
		systemPrompt:        "", // Not used for base orchestrator
		eventBridge:         eventBridge,
		orchestratorContext: nil, // Will be set dynamically
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
		boa.config.Model,
		time.Now().UnixNano()))

	// Create base agent
	baseAgent, err := NewBaseAgent(
		ctx,
		boa.agentType,
		string(boa.agentType), // Use agent type as name
		llmInstance,
		boa.systemPrompt,
		boa.config.ServerNames,
		boa.config.Mode,
		boa.tracer,
		traceID,
		boa.config.MCPConfigPath,
		boa.config.Model,
		boa.config.Temperature,
		boa.config.ToolChoice,
		boa.config.MaxTurns,
		boa.config.Provider,
		boa.logger,
		boa.config.CacheOnly,
	)
	if err != nil {
		return fmt.Errorf("failed to create base agent: %w", err)
	}

	boa.baseAgent = baseAgent

	// Append the agent-specific prompt to the existing system prompt
	boa.baseAgent.agent.AppendSystemPrompt(boa.systemPrompt)

	boa.logger.Infof("✅ Base Orchestrator Agent (%s) created successfully", boa.agentType)
	return nil
}

// ExecuteStructuredWithInputProcessor executes the agent with structured output and proper event emission
func ExecuteStructuredWithInputProcessor[T any](boa *BaseOrchestratorAgent, ctx context.Context, templateVars map[string]string, inputProcessor func(map[string]string) string, conversationHistory []llms.MessageContent, schema string) (T, error) {
	startTime := time.Now()

	// Auto-emit agent start event
	boa.emitAgentStartEvent(ctx, templateVars)

	// Process inputs using the provided processor function
	userMessage := inputProcessor(templateVars)

	// Get the base agent for structured output
	baseAgent := boa.baseAgent

	// Use the agent's built-in structured output capability
	result, err := AskStructured[T](baseAgent, ctx, userMessage, schema)

	duration := time.Since(startTime)

	// Auto-emit agent end event
	// Convert structured response to string for event emission
	var resultStr string
	if err != nil {
		resultStr = "Error: " + err.Error()
	} else {
		resultStr = fmt.Sprintf("Generated %s structured output", boa.agentType)
	}
	boa.emitAgentEndEvent(ctx, templateVars, resultStr, err, duration)

	if err != nil {
		var zero T
		return zero, fmt.Errorf("structured execution failed: %w", err)
	}

	return result, nil
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
	result, err := boa.baseAgent.Execute(ctx, baseAgentTemplateVars, conversationHistory)

	duration := time.Since(startTime)

	// Auto-emit agent end event
	boa.emitAgentEndEvent(ctx, templateVars, result, err, duration)

	if err != nil {
		boa.logger.Errorf("❌ Base Orchestrator Agent (%s) execution failed: %v", boa.agentType, err)
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
	return boa.config
}

// Close closes the base orchestrator agent
func (boa *BaseOrchestratorAgent) Close() error {
	if boa.baseAgent != nil {
		return boa.baseAgent.Close()
	}
	return nil
}

// BaseAgent returns the base agent
func (boa *BaseOrchestratorAgent) BaseAgent() *BaseAgent {
	return boa.baseAgent
}

// GetBaseAgent returns the base agent (implements OrchestratorAgent interface)
func (boa *BaseOrchestratorAgent) GetBaseAgent() *BaseAgent {
	return boa.baseAgent
}

// SetEventBridge sets the event bridge for the agent
func (boa *BaseOrchestratorAgent) SetEventBridge(bridge mcpagent.AgentEventListener) {
	boa.eventBridge = bridge
}

// GetTracer returns the tracer
func (boa *BaseOrchestratorAgent) GetTracer() observability.Tracer {
	return boa.tracer
}

// GetEventBridge returns the event bridge
func (boa *BaseOrchestratorAgent) GetEventBridge() mcpagent.AgentEventListener {
	return boa.eventBridge
}

// emitEvent emits an event through the event bridge
func (boa *BaseOrchestratorAgent) emitEvent(ctx context.Context, eventType events.EventType, data events.EventData) {
	boa.logger.Infof("🔍 emitEvent called - EventType: %s, AgentType: %s", eventType, boa.agentType)

	// Create agent event
	agentEvent := &events.AgentEvent{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}

	// Emit through event bridge
	if err := boa.eventBridge.HandleEvent(ctx, agentEvent); err != nil {
		boa.logger.Warnf("⚠️ Failed to emit event %s: %v", eventType, err)
	} else {
		boa.logger.Infof("✅ Successfully emitted event %s for agent type %s", eventType, boa.agentType)
	}
}

// emitAgentStartEvent emits an agent start event automatically
func (boa *BaseOrchestratorAgent) emitAgentStartEvent(ctx context.Context, templateVars map[string]string) {
	boa.logger.Infof("🔍 emitAgentStartEvent called for agent type: %s", boa.agentType)

	if boa.orchestratorContext == nil {
		boa.logger.Warnf("⚠️ Orchestrator context is nil - skipping agent start event emission for %s", boa.agentType)
		return // No context available yet
	}

	boa.logger.Infof("✅ Orchestrator context available - AgentName: %s, StepIndex: %d, Iteration: %d",
		boa.orchestratorContext.AgentName, boa.orchestratorContext.StepIndex, boa.orchestratorContext.Iteration)

	eventData := &events.OrchestratorAgentStartEvent{
		BaseEventData: events.BaseEventData{
			Timestamp: time.Now(),
		},
		AgentType:    string(boa.agentType),
		AgentName:    boa.orchestratorContext.AgentName,
		Objective:    boa.orchestratorContext.Objective,
		InputData:    templateVars,
		ModelID:      boa.config.Model,
		Provider:     boa.config.Provider,
		ServersCount: len(boa.config.ServerNames),
		MaxTurns:     boa.config.MaxTurns,
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
		ModelID:      boa.config.Model,
		Provider:     boa.config.Provider,
		ServersCount: len(boa.config.ServerNames),
		MaxTurns:     boa.config.MaxTurns,
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
	if len(boa.config.FallbackModels) > 0 {
		fallbackModels = append(fallbackModels, boa.config.FallbackModels...)
		// Using custom fallback models from frontend
	} else {
		// Use default fallback models for the provider
		fallbackModels = append(fallbackModels, llm.GetDefaultFallbackModels(llm.Provider(boa.config.Provider))...)
		// Using default fallback models for provider
	}

	// Add cross-provider fallback models if configured
	if boa.config.CrossProviderFallback != nil && len(boa.config.CrossProviderFallback.Models) > 0 {
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llm.Provider(boa.config.CrossProviderFallback.Provider))
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		// Added cross-provider fallback models
	} else {
		// Add default cross-provider fallbacks
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llm.Provider(boa.config.Provider))
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		// Added default cross-provider fallback models
	}

	// Create LLM configuration
	config := llm.Config{
		Provider:       llm.Provider(boa.config.Provider),
		ModelID:        boa.config.Model,
		Temperature:    boa.config.Temperature,
		Tracers:        nil, // Tracers will be set later if needed
		TraceID:        traceID,
		FallbackModels: fallbackModels,
		MaxRetries:     boa.config.MaxRetries,
		Logger:         boa.logger,
	}

	// Initialize LLM using the existing factory
	llmInstance, err := llm.InitializeLLM(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize LLM: %w", err)
	}

	return llmInstance, nil
}
