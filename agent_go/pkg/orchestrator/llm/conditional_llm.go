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

// ConditionalResponse represents a true/false response with reasoning
type ConditionalResponse struct {
	Result bool   `json:"result"`
	Reason string `json:"reason"`
}

// GetResult returns the boolean result
func (cr *ConditionalResponse) GetResult() bool {
	return cr.Result
}

// ConditionalLLM provides a simple true/false decision service
type ConditionalLLM struct {
	*BaseLLM // Embed BaseLLM for common functionality
}

// NewConditionalLLMWithEventBridge creates a new conditional LLM instance with mandatory event bridge
func NewConditionalLLMWithEventBridge(
	llm llmtypes.Model,
	logger loggerv2.Logger,
	tracer observability.Tracer,
	eventBridge mcpagent.AgentEventListener,
) *ConditionalLLM {
	return &ConditionalLLM{
		BaseLLM: NewBaseLLM(llm, logger, tracer, eventBridge, "conditional"),
	}
}

// SetEventEmitter sets the event emitter function
func (cl *ConditionalLLM) SetEventEmitter(emitter func(context.Context, events.EventData)) {
	cl.BaseLLM.SetEventEmitter(emitter)
}

// Decide makes a true/false decision based on context and question
func (cl *ConditionalLLM) Decide(ctx context.Context, context, question string, stepIndex, iteration int) (*ConditionalResponse, error) {
	cl.GetLogger().Info(fmt.Sprintf("🤔 Making conditional decision: %s", question))

	// Emit orchestrator agent start event
	if cl.GetEventEmitter() != nil {
		// Add context to InputData for display in frontend
		inputData := make(map[string]string)
		if context != "" {
			inputData["context"] = context
		}
		
		startEvent := &events.OrchestratorAgentStartEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			AgentType: "conditional",
			AgentName: "conditional-llm",
			Objective: fmt.Sprintf("Conditional decision: %s", question),
			InputData: inputData,
			StepIndex: stepIndex,
			Iteration: iteration,
		}
		cl.GetEventEmitter()(ctx, startEvent)
	}

	// Build prompt
	prompt := GetPrompt(context, question)
	schema := GetSchema()

	// Create structured output generator
	config := mcpagent.LangchaingoStructuredOutputConfig{
		UseJSONMode:    true,
		ValidateOutput: true,
		MaxRetries:     2,
	}
	// Convert loggerv2.Logger to loggerv2.Logger
	var v2Logger loggerv2.Logger
	if cl.GetLogger() != nil {
		v2Logger = cl.GetLogger()
	} else {
		v2Logger = loggerv2.NewDefault()
	}

	// Create structured output generator with logger
	generator := mcpagent.NewLangchaingoStructuredOutputGenerator(cl.GetLLM(), config, v2Logger)
	jsonOutput, err := generator.GenerateStructuredOutput(ctx, prompt, schema)
	if err != nil {
		return nil, err
	}
	
	// Parse JSON output into ConditionalResponse
	var response ConditionalResponse
	if err := json.Unmarshal([]byte(jsonOutput), &response); err != nil {
		return nil, fmt.Errorf("failed to parse conditional response: %w", err)
	}
	return &response, nil
}

// Close cleans up resources
func (cl *ConditionalLLM) Close() error {
	return cl.BaseLLM.Close()
}

// GetPrompt returns a prompt for true/false decisions with reasoning
func GetPrompt(context, question string) string {
	return `You are a decision assistant. Analyze the context and return a true/false decision with reasoning.

Context: ` + context + `

Question: ` + question + `

Instructions:
1. You mainly need to determine answer to the question based on question.
2. Yes = true , No = false
3. Provide clear reasoning for your decision

Return ONLY valid JSON: {"result": true/false, "reason": "your reasoning here"}`
}

// GetSchema returns the JSON schema
func GetSchema() string {
	return `{
  "type": "object",
  "properties": {
    "result": {"type": "boolean"},
    "reason": {"type": "string"}
  },
  "required": ["result", "reason"],
  "additionalProperties": false
}`
}
