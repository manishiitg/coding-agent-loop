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

// StructuredOutputEvent represents structured output operation events
type StructuredOutputEvent struct {
	events.BaseEventData
	Operation string `json:"operation"`
	EventType string `json:"event_type"`
	Error     string `json:"error,omitempty"`
	Duration  string `json:"duration,omitempty"`
}

// GetEventType returns the event type for StructuredOutputEvent
func (e *StructuredOutputEvent) GetEventType() events.EventType {
	switch e.EventType {
	case "structured_output_start":
		return events.StructuredOutputStart
	case "structured_output_end":
		return events.StructuredOutputEnd
	case "structured_output_error":
		return events.StructuredOutputError
	default:
		return events.StructuredOutputStart // Default fallback
	}
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
		startEventData := &StructuredOutputEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: startTime,
			},
			Operation: "generate_structured_output",
			EventType: "structured_output_start",
		}
		s.eventEmitter(ctx, startEventData)
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
			errorEventData := &StructuredOutputEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Operation: "generate_structured_output",
				EventType: "structured_output_error",
				Error:     err.Error(),
			}
			s.eventEmitter(ctx, errorEventData)
		}
		return "", fmt.Errorf("failed to generate structured output: %w", err)
	}

	duration := time.Since(startTime)

	// Emit end event
	if s.eventEmitter != nil {
		endEventData := &StructuredOutputEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Operation: "generate_structured_output",
			EventType: "structured_output_end",
			Duration:  duration.String(),
		}
		s.eventEmitter(ctx, endEventData)
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
