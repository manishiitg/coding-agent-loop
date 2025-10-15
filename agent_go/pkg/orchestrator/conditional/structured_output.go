package conditional

import (
	"context"
	"encoding/json"
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

// GenericStructuredResponse represents a generic structured response interface
type GenericStructuredResponse interface {
	// GetData returns the parsed data as a generic interface{}
	GetData() interface{}
}

// GenericStructuredResponseImpl is a basic implementation of GenericStructuredResponse
type GenericStructuredResponseImpl struct {
	Data interface{} `json:"data"`
}

// GetData returns the parsed data
func (r *GenericStructuredResponseImpl) GetData() interface{} {
	return r.Data
}

// StructuredOutputLLM represents a generic structured output LLM for any structured output extraction
type StructuredOutputLLM struct {
	llm          llms.Model
	logger       utils.ExtendedLogger
	tracer       observability.Tracer
	eventEmitter func(context.Context, events.EventData)
}

// NewStructuredOutputLLMWithEventEmitter creates a new generic structured output LLM with event emission
func NewStructuredOutputLLMWithEventEmitter(
	llm llms.Model,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	eventEmitter func(context.Context, events.EventData),
) *StructuredOutputLLM {
	return &StructuredOutputLLM{
		llm:          llm,
		logger:       logger,
		tracer:       tracer,
		eventEmitter: eventEmitter,
	}
}

// GenerateStructuredOutput generates structured output using provided prompt and schema
func (s *StructuredOutputLLM) GenerateStructuredOutput(ctx context.Context, prompt, schema string) (string, error) {
	startTime := time.Now()

	// Emit start event
	if s.eventEmitter != nil {
		startEvent := &events.GenericEventData{
			BaseEventData: events.BaseEventData{
				Timestamp: startTime,
			},
			Data: map[string]interface{}{"operation": "generate_structured_output", "event_type": "structured_output_start"},
		}
		s.eventEmitter(ctx, startEvent)
	}

	// Create structured output generator
	config := mcpagent.LangchaingoStructuredOutputConfig{
		UseJSONMode:    true,
		ValidateOutput: true,
		MaxRetries:     2,
	}
	generator := mcpagent.NewLangchaingoStructuredOutputGenerator(s.llm, config, s.logger)

	// Generate structured output
	result, err := generator.GenerateStructuredOutput(ctx, prompt, schema)
	if err != nil {
		// Emit error event
		if s.eventEmitter != nil {
			errorEvent := &events.GenericEventData{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Data: map[string]interface{}{"operation": "generate_structured_output", "error": err.Error(), "event_type": "structured_output_error"},
			}
			s.eventEmitter(ctx, errorEvent)
		}
		return "", fmt.Errorf("failed to generate structured output: %w", err)
	}

	duration := time.Since(startTime)

	// Emit end event
	if s.eventEmitter != nil {
		endEvent := &events.GenericEventData{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Data: map[string]interface{}{"operation": "generate_structured_output", "duration": duration.String(), "event_type": "structured_output_end"},
		}
		s.eventEmitter(ctx, endEvent)
	}

	s.logger.Infof("✅ Successfully generated structured output in %v", duration)
	return result, nil
}

// ParseGenericStructuredResponse parses JSON output into a generic structured response
func ParseGenericStructuredResponse(jsonOutput string) (*GenericStructuredResponseImpl, error) {
	var response GenericStructuredResponseImpl
	if err := json.Unmarshal([]byte(jsonOutput), &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal structured JSON: %w", err)
	}
	return &response, nil
}

// CreateStructuredOutputLLMWithEventBridge creates a structured output LLM with event bridge integration
func CreateStructuredOutputLLMWithEventBridge(
	config *agents.OrchestratorAgentConfig,
	eventBridge interface{},
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
) (*StructuredOutputLLM, error) {
	logger.Infof("🔧 Creating structured output LLM for independent steps extraction with automatic event emission")

	// Generate trace ID for this structured output LLM
	traceID := observability.TraceID(fmt.Sprintf("structured-output-llm-%d", time.Now().UnixNano()))

	// Build fallback models list
	var fallbackModels []string

	// Add custom fallback models from config if provided
	if len(config.FallbackModels) > 0 {
		fallbackModels = append(fallbackModels, config.FallbackModels...)
		logger.Infof("🔧 Using custom fallback models for structured output LLM: %v", config.FallbackModels)
	} else {
		// Use default fallback models for the provider
		fallbackModels = append(fallbackModels, llm.GetDefaultFallbackModels(llm.Provider(config.Provider))...)
		logger.Infof("🔧 Using default fallback models for structured output LLM provider: %s", config.Provider)
	}

	// Add cross-provider fallback models if configured
	if config.CrossProviderFallback != nil && len(config.CrossProviderFallback.Models) > 0 {
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llm.Provider(config.CrossProviderFallback.Provider))
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		logger.Infof("🔧 Added cross-provider fallback models for structured output LLM: %v", crossProviderFallbacks)
	} else {
		// Add default cross-provider fallbacks
		crossProviderFallbacks := llm.GetCrossProviderFallbackModels(llm.Provider(config.Provider))
		fallbackModels = append(fallbackModels, crossProviderFallbacks...)
		logger.Infof("🔧 Added default cross-provider fallback models for structured output LLM: %v", crossProviderFallbacks)
	}

	// Create LLM configuration for structured output
	// Use low temperature (0.1) for consistent structured output
	llmConfig := llm.Config{
		Provider:       llm.Provider(config.Provider),
		ModelID:        config.Model,
		Temperature:    0.1, // Low temperature for consistent structured output
		Tracers:        nil, // Tracers will be set later if needed
		TraceID:        traceID,
		FallbackModels: fallbackModels,
		MaxRetries:     config.MaxRetries,
		Logger:         logger,
	}

	// Initialize LLM using the existing factory
	llmInstance, err := llm.InitializeLLM(llmConfig)
	if err != nil {
		logger.Errorf("❌ Failed to create structured output LLM: %v", err)
		return nil, fmt.Errorf("failed to create structured output LLM: %w", err)
	}

	// Create event emitter function that uses the event bridge
	eventEmitter := func(ctx context.Context, data events.EventData) {
		if eventBridge == nil {
			logger.Warnf("⚠️ No event bridge available, cannot emit structured output LLM event")
			return
		}

		// Create agent event
		agentEvent := &events.AgentEvent{
			Type:      events.OrchestratorAgentStart, // Will be overridden by the structured output LLM
			Timestamp: time.Now(),
			Data:      data,
		}

		// Emit through event bridge - cast to the proper interface
		if bridge, ok := eventBridge.(interface {
			HandleEvent(context.Context, *events.AgentEvent) error
		}); ok {
			if err := bridge.HandleEvent(ctx, agentEvent); err != nil {
				logger.Warnf("⚠️ Failed to emit structured output LLM event: %v", err)
			}
		} else {
			logger.Warnf("⚠️ Event bridge does not implement HandleEvent method: %T", eventBridge)
		}
	}

	// Create structured output LLM instance with event emitter
	structuredOutputLLM := NewStructuredOutputLLMWithEventEmitter(llmInstance, logger, tracer, eventEmitter)

	logger.Infof("✅ Structured output LLM created successfully with automatic event emission - Provider: %s, Model: %s, Temperature: %.1f",
		config.Provider, config.Model, 0.1)

	return structuredOutputLLM, nil
}
