package llm

import (
	"context"
	"fmt"
	"mcp-agent/agent_go/internal/llm"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"time"

	"github.com/tmc/langchaingo/llms"
)

// BaseLLM provides common functionality for all LLM-based operations
type BaseLLM struct {
	llm          llms.Model
	logger       utils.ExtendedLogger
	tracer       observability.Tracer
	eventEmitter func(context.Context, events.EventData)
}

// NewBaseLLM creates a new BaseLLM instance with mandatory event bridge
func NewBaseLLM(
	llm llms.Model,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	llmType string,
) *BaseLLM {
	if eventBridge == nil {
		logger.Warnf("⚠️ Event bridge is nil for %s LLM - this may limit observability", llmType)
	}

	eventEmitter := CreateEventEmitter(eventBridge, logger, llmType)

	return &BaseLLM{
		llm:          llm,
		logger:       logger,
		tracer:       tracer,
		eventEmitter: eventEmitter,
	}
}

// GetLLM returns the underlying LLM instance
func (b *BaseLLM) GetLLM() llms.Model {
	return b.llm
}

// GetLogger returns the logger
func (b *BaseLLM) GetLogger() utils.ExtendedLogger {
	return b.logger
}

// GetTracer returns the tracer
func (b *BaseLLM) GetTracer() observability.Tracer {
	return b.tracer
}

// GetEventEmitter returns the event emitter function
func (b *BaseLLM) GetEventEmitter() func(context.Context, events.EventData) {
	return b.eventEmitter
}

// SetEventEmitter sets the event emitter function
func (b *BaseLLM) SetEventEmitter(emitter func(context.Context, events.EventData)) {
	b.eventEmitter = emitter
}

// CreateLLMInstance creates an LLM instance with standard configuration
func CreateLLMInstance(
	config *agents.OrchestratorAgentConfig,
	logger utils.ExtendedLogger,
	llmType string,
) (llms.Model, error) {
	logger.Infof("🔧 Creating %s LLM with standard configuration", llmType)

	// Generate trace ID for this LLM session
	traceID := observability.TraceID(fmt.Sprintf("%s-llm-%d", llmType, time.Now().UnixNano()))

	// Build fallback models list
	var fallbackModels []string

	// Add custom fallback models from config if provided
	if len(config.FallbackModels) > 0 {
		fallbackModels = append(fallbackModels, config.FallbackModels...)
		logger.Infof("🔧 Using custom fallback models for %s LLM: %v", llmType, config.FallbackModels)
	} else {
		// Use default fallback models for the provider
		fallbackModels = append(fallbackModels, llm.GetDefaultFallbackModels(llm.Provider(config.Provider))...)
		logger.Infof("🔧 Using default fallback models for %s LLM provider: %s", llmType, config.Provider)
	}

	// Add cross-provider fallback models if configured
	if config.CrossProviderFallback != nil && len(config.CrossProviderFallback.Models) > 0 {
		fallbackModels = append(fallbackModels, config.CrossProviderFallback.Models...)
		logger.Infof("🔧 Using configured cross-provider fallback models for %s LLM: %v", llmType, config.CrossProviderFallback.Models)
	} else {
		// Add default cross-provider fallbacks
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llm.Provider(config.Provider))
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		logger.Infof("🔧 Added default cross-provider fallback models for %s LLM: %v", llmType, crossProviderFallbacks)
	}

	// Create LLM configuration
	llmConfig := llm.Config{
		Provider:       llm.Provider(config.Provider),
		ModelID:        config.Model,
		Temperature:    config.Temperature,
		Tracers:        nil, // Tracers will be set later if needed
		TraceID:        traceID,
		FallbackModels: fallbackModels,
		MaxRetries:     config.MaxRetries,
		Logger:         logger,
	}

	// Initialize LLM using the existing factory
	llmInstance, err := llm.InitializeLLM(llmConfig)
	if err != nil {
		logger.Errorf("❌ Failed to create %s LLM: %v", llmType, err)
		return nil, fmt.Errorf("failed to create %s LLM: %w", llmType, err)
	}

	logger.Infof("✅ %s LLM created successfully - Provider: %s, Model: %s, Temperature: %.1f",
		llmType, config.Provider, config.Model, config.Temperature)

	return llmInstance, nil
}

// CreateEventEmitter creates a standard event emitter function for LLM operations
func CreateEventEmitter(
	eventBridge mcpagent.AgentEventListener,
	logger utils.ExtendedLogger,
	llmType string,
) func(context.Context, events.EventData) {
	return func(ctx context.Context, data events.EventData) {
		if eventBridge == nil {
			logger.Warnf("⚠️ No event bridge available, cannot emit %s LLM event", llmType)
			return
		}

		// Create agent event
		eventType := data.GetEventType()
		if eventType == "" {
			eventType = events.OrchestratorAgentStart // Fallback to current default
		}

		agentEvent := &events.AgentEvent{
			Type:      eventType,
			Timestamp: time.Now(),
			Data:      data,
		}

		// Emit through event bridge
		if err := eventBridge.HandleEvent(ctx, agentEvent); err != nil {
			logger.Warnf("⚠️ Failed to emit %s LLM event: %v", llmType, err)
		}
	}
}

// CreateConditionalLLMWithEventBridge creates a conditional LLM with mandatory event bridge integration
func CreateConditionalLLMWithEventBridge(
	config *agents.OrchestratorAgentConfig,
	eventBridge mcpagent.AgentEventListener,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
) (*ConditionalLLM, error) {
	logger.Infof("🔧 Creating conditional LLM with mandatory event bridge integration")

	// Create LLM instance using helper
	llmInstance, err := CreateLLMInstance(config, logger, "conditional")
	if err != nil {
		return nil, err
	}

	// Create conditional LLM with BaseLLM (which includes mandatory event bridge)
	conditionalLLM := &ConditionalLLM{
		BaseLLM: NewBaseLLM(llmInstance, logger, tracer, eventBridge, "conditional"),
	}

	logger.Infof("✅ Conditional LLM created successfully with event bridge - Provider: %s, Model: %s, Temperature: %.1f",
		config.Provider, config.Model, config.Temperature)

	return conditionalLLM, nil
}

// CreateStructuredOutputLLMWithEventBridge creates a structured output LLM with mandatory event bridge integration
func CreateStructuredOutputLLMWithEventBridge(
	config *agents.OrchestratorAgentConfig,
	eventBridge mcpagent.AgentEventListener,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
) (*StructuredOutputLLM, error) {
	logger.Infof("🔧 Creating structured output LLM with mandatory event bridge integration")

	// Create LLM instance using helper
	llmInstance, err := CreateLLMInstance(config, logger, "structured-output")
	if err != nil {
		return nil, err
	}

	// Create structured output LLM with BaseLLM (which includes mandatory event bridge)
	structuredOutputLLM := &StructuredOutputLLM{
		BaseLLM: NewBaseLLM(llmInstance, logger, tracer, eventBridge, "structured-output"),
	}

	logger.Infof("✅ Structured output LLM created successfully with event bridge - Provider: %s, Model: %s, Temperature: %.1f",
		config.Provider, config.Model, config.Temperature)

	return structuredOutputLLM, nil
}

// Close cleans up resources
func (b *BaseLLM) Close() error {
	return nil
}
