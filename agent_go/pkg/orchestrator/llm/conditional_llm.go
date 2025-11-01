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

	"mcp-agent/agent_go/internal/llmtypes"
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
	logger utils.ExtendedLogger,
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
	startTime := time.Now()

	cl.GetLogger().Infof("🤔 Making conditional decision: %s", question)

	// Build prompt
	prompt := GetPrompt(context, question)
	schema := GetSchema()

	// Create structured output generator
	config := mcpagent.LangchaingoStructuredOutputConfig{
		UseJSONMode:    true,
		ValidateOutput: true,
		MaxRetries:     2,
	}
	generator := mcpagent.NewLangchaingoStructuredOutputGenerator(cl.GetLLM(), config, cl.GetLogger())

	// Generate structured output
	jsonOutput, err := generator.GenerateStructuredOutput(ctx, prompt, schema)
	if err != nil {
		duration := time.Since(startTime)
		cl.GetLogger().Errorf("❌ Conditional decision failed: %w", err)

		// Emit orchestrator agent error event
		if cl.GetEventEmitter() != nil {
			errorEvent := &events.OrchestratorAgentErrorEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				AgentType: "conditional",
				AgentName: "conditional-llm",
				Objective: fmt.Sprintf("Conditional decision: %s", question),
				Error:     err.Error(),
				Duration:  duration,
				StepIndex: stepIndex,
				Iteration: iteration,
			}
			cl.GetEventEmitter()(ctx, errorEvent)
		}

		return nil, fmt.Errorf("failed to make conditional decision: %w", err)
	}

	// Parse JSON
	var result ConditionalResponse
	if err := json.Unmarshal([]byte(jsonOutput), &result); err != nil {
		duration := time.Since(startTime)
		cl.GetLogger().Errorf("❌ Failed to parse conditional response: %w", err)

		// Emit orchestrator agent error event
		if cl.GetEventEmitter() != nil {
			errorEvent := &events.OrchestratorAgentErrorEvent{
				BaseEventData: events.BaseEventData{
					Timestamp: time.Now(),
				},
				AgentType: "conditional",
				AgentName: "conditional-llm",
				Objective: fmt.Sprintf("Conditional decision: %s", question),
				Error:     err.Error(),
				Duration:  duration,
				StepIndex: stepIndex,
				Iteration: iteration,
			}
			cl.GetEventEmitter()(ctx, errorEvent)
		}

		return nil, fmt.Errorf("failed to parse conditional response: %w", err)
	}

	duration := time.Since(startTime)
	cl.GetLogger().Infof("✅ Conditional decision made: result=%t, reason=%s", result.Result, result.Reason)

	// Emit orchestrator agent end event
	if cl.GetEventEmitter() != nil {
		resultText := fmt.Sprintf("Decision: %t, Reason: %s", result.Result, result.Reason)
		endEvent := &events.OrchestratorAgentEndEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			AgentType: "conditional",
			AgentName: "conditional-llm",
			Objective: fmt.Sprintf("Conditional decision: %s", question),
			InputData: map[string]string{
				"context":  context,
				"question": question,
			},
			Result:    resultText,
			Duration:  duration,
			StepIndex: stepIndex,
			Iteration: iteration,
		}
		cl.GetEventEmitter()(ctx, endEvent)
	}

	return &result, nil
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
