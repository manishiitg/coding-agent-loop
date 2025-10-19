package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
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
	*BaseLLM // Embed BaseLLM for common functionality
}

// NewStructuredOutputLLMWithEventBridge creates a new structured output LLM with mandatory event bridge
func NewStructuredOutputLLMWithEventBridge(
	llm llms.Model,
	logger utils.ExtendedLogger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
) *StructuredOutputLLM {
	return &StructuredOutputLLM{
		BaseLLM: NewBaseLLM(llm, logger, tracer, eventBridge, "structured-output"),
	}
}

// GenerateStructuredOutput generates structured output using provided prompt and schema
func (s *StructuredOutputLLM) GenerateStructuredOutput(ctx context.Context, prompt, schema string) (string, error) {
	startTime := time.Now()

	// Emit start event
	if s.GetEventEmitter() != nil {
		startEventData := &events.StructuredOutputEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: startTime,
			},
			Operation: "generate_structured_output",
			EventType: "structured_output_start",
		}
		s.GetEventEmitter()(ctx, startEventData)
	}

	// Create structured output generator
	config := mcpagent.LangchaingoStructuredOutputConfig{
		UseJSONMode:    true,
		ValidateOutput: true,
		MaxRetries:     2,
	}
	generator := mcpagent.NewLangchaingoStructuredOutputGenerator(s.GetLLM(), config, s.GetLogger())

	// Generate structured output
	result, err := generator.GenerateStructuredOutput(ctx, prompt, schema)
	if err != nil {
		// Emit error event
		if s.GetEventEmitter() != nil {
			errorEventData := &events.StructuredOutputEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				Operation: "generate_structured_output",
				EventType: "structured_output_error",
				Error:     err.Error(),
			}
			s.GetEventEmitter()(ctx, errorEventData)
		}
		return "", fmt.Errorf("failed to generate structured output: %w", err)
	}

	duration := time.Since(startTime)

	// Emit end event
	if s.GetEventEmitter() != nil {
		endEventData := &events.StructuredOutputEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Operation: "generate_structured_output",
			EventType: "structured_output_end",
			Duration:  duration.String(),
		}
		s.GetEventEmitter()(ctx, endEventData)
	}

	s.GetLogger().Infof("✅ Successfully generated structured output in %v", duration)
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
// Note: This function is now provided by BaseLLM for consistency
