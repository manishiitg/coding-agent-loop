package llm

import (
	"context"
	"fmt"
	agentlogger "mcp-agent-builder-go/agent_go/pkg/logger"
	"mcp-agent-builder-go/agent_go/pkg/orchestrator/agents"
	mcpagent "mcpagent/agent"
	"mcpagent/events"
	"mcpagent/llm"
	loggerv2 "mcpagent/logger/v2"
	"mcpagent/observability"
	"time"

	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
)

// BaseLLM provides common functionality for all LLM-based operations
type BaseLLM struct {
	llm          llmtypes.Model
	logger       loggerv2.Logger
	tracer       observability.Tracer
	eventEmitter func(context.Context, events.EventData)
}

// NewBaseLLM creates a new BaseLLM instance with mandatory event bridge
func NewBaseLLM(
	llm llmtypes.Model,
	logger loggerv2.Logger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
	llmType string,
) *BaseLLM {
	if eventBridge == nil {
		logger.Warn(fmt.Sprintf("⚠️ Event bridge is nil for %s LLM - this may limit observability", llmType))
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
func (b *BaseLLM) GetLLM() llmtypes.Model {
	return b.llm
}

// GetLogger returns the logger
func (b *BaseLLM) GetLogger() loggerv2.Logger {
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

// createLLMLogger creates a separate logger instance for LLM operations
// This logger writes to logs/llm_debug.log to separate LLM logs from server logs
func createLLMLogger() loggerv2.Logger {
	llmLogger, err := agentlogger.CreateLogger("logs/llm_debug.log", "debug", "text", false)
	if err != nil {
		// Fallback to default logger if creation fails
		return loggerv2.NewDefault()
	}
	return llmLogger
}

// CreateLLMInstance creates an LLM instance with standard configuration
func CreateLLMInstance(
	config *agents.OrchestratorAgentConfig,
	logger loggerv2.Logger,
	llmType string,
) (llmtypes.Model, error) {
	logger.Info(fmt.Sprintf("🔧 Creating %s LLM with standard configuration", llmType))

	// Use separate LLM logger for multi-llm-provider-go logs
	llmLogger := createLLMLogger()

	// Generate trace ID for this LLM session
	traceID := observability.TraceID(fmt.Sprintf("%s-llm-%d", llmType, time.Now().UnixNano()))

	// Build fallback models list
	var fallbackModels []string

	// Add custom fallback models from config if provided
	if len(config.FallbackModels) > 0 {
		fallbackModels = append(fallbackModels, config.FallbackModels...)
		logger.Info(fmt.Sprintf("🔧 Using custom fallback models for %s LLM: %v", llmType, config.FallbackModels))
	} else {
		// Use default fallback models for the provider
		fallbackModels = append(fallbackModels, llm.GetDefaultFallbackModels(llm.Provider(config.Provider))...)
		logger.Info(fmt.Sprintf("🔧 Using default fallback models for %s LLM provider: %s", llmType, config.Provider))
	}

	// Add cross-provider fallback models if configured
	if config.CrossProviderFallback != nil && len(config.CrossProviderFallback.Models) > 0 {
		fallbackModels = append(fallbackModels, config.CrossProviderFallback.Models...)
		logger.Info(fmt.Sprintf("🔧 Using configured cross-provider fallback models for %s LLM: %v", llmType, config.CrossProviderFallback.Models))
	} else {
		// Add default cross-provider fallbacks
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llm.Provider(config.Provider))
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		logger.Info(fmt.Sprintf("🔧 Added default cross-provider fallback models for %s LLM: %v", llmType, crossProviderFallbacks))
	}

	// Convert API keys from agent config to LLM config format
	var llmAPIKeys *llm.ProviderAPIKeys
	if config.APIKeys != nil {
		llmAPIKeys = &llm.ProviderAPIKeys{
			OpenRouter: config.APIKeys.OpenRouter,
			OpenAI:     config.APIKeys.OpenAI,
			Anthropic:  config.APIKeys.Anthropic,
			Vertex:     config.APIKeys.Vertex,
		}
		if config.APIKeys.Bedrock != nil {
			llmAPIKeys.Bedrock = &llm.BedrockConfig{
				Region: config.APIKeys.Bedrock.Region,
			}
		}
	}

	// Create LLM configuration
	// Use llmLogger (separate file) for multi-llm-provider-go logs, not the server logger
	llmConfig := llm.Config{
		Provider:       llm.Provider(config.Provider),
		ModelID:        config.Model,
		Temperature:    config.Temperature,
		Tracers:        nil, // Tracers will be set later if needed
		TraceID:        traceID,
		FallbackModels: fallbackModels,
		MaxRetries:     config.MaxRetries,
		Logger:         llmLogger, // Use separate LLM logger for multi-llm-provider-go logs
		APIKeys:        llmAPIKeys,
	}

	// Initialize LLM using the existing factory
	llmInstance, err := llm.InitializeLLM(llmConfig)
	if err != nil {
		logger.Error(fmt.Sprintf("❌ Failed to create %s LLM: %v", llmType, err), err)
		return nil, fmt.Errorf("failed to create %s LLM: %w", llmType, err)
	}

	logger.Info(fmt.Sprintf("✅ %s LLM created successfully - Provider: %s, Model: %s, Temperature: %.1f",
		llmType, config.Provider, config.Model, config.Temperature))

	return llmInstance, nil
}

// CreateEventEmitter creates a standard event emitter function for LLM operations
func CreateEventEmitter(
	eventBridge mcpagent.AgentEventListener,
	logger loggerv2.Logger,
	llmType string,
) func(context.Context, events.EventData) {
	return func(ctx context.Context, data events.EventData) {
		if eventBridge == nil {
			logger.Warn(fmt.Sprintf("⚠️ No event bridge available, cannot emit %s LLM event", llmType))
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
			logger.Warn(fmt.Sprintf("⚠️ Failed to emit %s LLM event: %v", llmType, err), loggerv2.Field{Key: "error", Value: err})
		}
	}
}

// CreateConditionalLLMWithEventBridge creates a conditional LLM with mandatory event bridge integration
func CreateConditionalLLMWithEventBridge(
	config *agents.OrchestratorAgentConfig,
	eventBridge mcpagent.AgentEventListener,
	logger loggerv2.Logger,
	tracer observability.Tracer,
) (*ConditionalLLM, error) {
	logger.Info(fmt.Sprintf("🔧 Creating conditional LLM with mandatory event bridge integration"))

	// Create LLM instance using helper
	llmInstance, err := CreateLLMInstance(config, logger, "conditional")
	if err != nil {
		return nil, err
	}

	// Create conditional LLM with BaseLLM (which includes mandatory event bridge)
	conditionalLLM := &ConditionalLLM{
		BaseLLM: NewBaseLLM(llmInstance, logger, tracer, eventBridge, "conditional"),
	}

	logger.Info(fmt.Sprintf("✅ Conditional LLM created successfully with event bridge - Provider: %s, Model: %s, Temperature: %.1f",
		config.Provider, config.Model, config.Temperature))

	return conditionalLLM, nil
}

// CreateStructuredOutputLLMWithEventBridge creates a structured output LLM with mandatory event bridge integration
func CreateStructuredOutputLLMWithEventBridge(
	config *agents.OrchestratorAgentConfig,
	eventBridge mcpagent.AgentEventListener,
	logger loggerv2.Logger,
	tracer observability.Tracer,
) (*StructuredOutputLLM, error) {
	logger.Info(fmt.Sprintf("🔧 Creating structured output LLM with mandatory event bridge integration"))

	// Create LLM instance using helper
	llmInstance, err := CreateLLMInstance(config, logger, "structured-output")
	if err != nil {
		return nil, err
	}

	// Create structured output LLM with BaseLLM (which includes mandatory event bridge)
	structuredOutputLLM := &StructuredOutputLLM{
		BaseLLM: NewBaseLLM(llmInstance, logger, tracer, eventBridge, "structured-output"),
	}

	logger.Info(fmt.Sprintf("✅ Structured output LLM created successfully with event bridge - Provider: %s, Model: %s, Temperature: %.1f",
		config.Provider, config.Model, config.Temperature))

	return structuredOutputLLM, nil
}

// Close cleans up resources
func (b *BaseLLM) Close() error {
	return nil
}
