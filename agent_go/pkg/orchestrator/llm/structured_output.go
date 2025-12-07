package llm

import (
	"context"
	"encoding/json"
	"fmt"
	loggerv2 "mcpagent/logger/v2"
	mcpagent "mcpagent/agent"
	"mcpagent/events"
	"mcpagent/observability"
	"time"


	"github.com/manishiitg/multi-llm-provider-go/llmtypes"
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
	llm llmtypes.Model,
	logger loggerv2.Logger,
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
	correlationID := fmt.Sprintf("struct-output-%d", startTime.UnixNano())

	// Emit start event
	if s.GetEventEmitter() != nil {
		startEventData := &events.StructuredOutputEvent{
			BaseEventData: events.BaseEventData{
				Timestamp:     startTime,
				CorrelationID: correlationID,
				Component:     "llm",
			},
			Operation: "generate_structured_output",
			EventType: "structured_output_start",
		}
		s.GetEventEmitter()(ctx, startEventData)
	}

	// Create structured output generator
	var v2Logger loggerv2.Logger
	if s.GetLogger() != nil {
		v2Logger = s.GetLogger()
	} else {
		v2Logger = loggerv2.NewDefault()
	}

	config := mcpagent.LangchaingoStructuredOutputConfig{
		UseJSONMode:    true,
		ValidateOutput: true,
		MaxRetries:     2,
	}
	generator := mcpagent.NewLangchaingoStructuredOutputGenerator(s.GetLLM(), config, v2Logger)
	jsonOutput, err := generator.GenerateStructuredOutput(ctx, prompt, schema)
	if err != nil {
		return "", err
	}
	return jsonOutput, nil
}

// ParseGenericStructuredResponse parses JSON output into a generic structured response
func ParseGenericStructuredResponse(jsonOutput string) (*GenericStructuredResponseImpl, error) {
	var response GenericStructuredResponseImpl
	if err := json.Unmarshal([]byte(jsonOutput), &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal structured JSON: %w", err)
	}
	return &response, nil
}
