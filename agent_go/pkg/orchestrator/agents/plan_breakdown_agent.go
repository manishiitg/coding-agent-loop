package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents/prompts"

	"github.com/tmc/langchaingo/llms"
)

// PlanBreakdownAgent analyzes dependencies and creates independent steps for parallel execution
type PlanBreakdownAgent struct {
	*BaseOrchestratorAgent
	breakdownPrompts    *prompts.PlanBreakdownPrompts
	structuredOutputLLM llms.Model
}

// NewPlanBreakdownAgent creates a new plan breakdown agent
func NewPlanBreakdownAgent(config *OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge interface{}) *PlanBreakdownAgent {
	breakdownPrompts := prompts.NewPlanBreakdownPrompts()

	baseAgent := NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		PlanBreakdownAgentType,
		eventBridge,
	)

	// Create LLM model for structured output (reuse the same config as base agent)
	var structuredOutputLLM llms.Model
	// The LLM model will be created on-demand when needed
	// For now, we'll set it to nil and create it in the structured method

	return &PlanBreakdownAgent{
		BaseOrchestratorAgent: baseAgent,
		breakdownPrompts:      breakdownPrompts,
		structuredOutputLLM:   structuredOutputLLM,
	}
}

// Initialize initializes the plan breakdown agent (delegates to base)
func (pba *PlanBreakdownAgent) Initialize(ctx context.Context) error {
	return pba.BaseOrchestratorAgent.Initialize(ctx)
}

// Execute executes the plan breakdown agent with breakdown-specific input processing
func (pba *PlanBreakdownAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	// This will automatically emit agent start/end events
	result, err := pba.ExecuteWithInputProcessor(ctx, templateVars, pba.breakdownInputProcessor, conversationHistory)
	if err != nil {
		return "", fmt.Errorf("plan breakdown execution failed: %w", err)
	}

	// The result should already be the structured JSON response from the LLM
	// Parse it to validate it's proper JSON
	var breakdownResponse BreakdownResponse
	if err := json.Unmarshal([]byte(result), &breakdownResponse); err != nil {
		return "", fmt.Errorf("failed to parse breakdown response: %w", err)
	}

	// Return the JSON string result
	return result, nil
}

// breakdownInputProcessor processes inputs specifically for plan breakdown using structured output
func (pba *PlanBreakdownAgent) breakdownInputProcessor(templateVars map[string]string) string {
	// Extract template variables for dependency analysis
	planningResult := templateVars["PlanningResult"]
	objective := templateVars["Objective"]
	workspacePath := templateVars["WorkspacePath"]

	// Call AnalyzeDependencies to get structured response
	// This will emit structured output events
	breakdownResponse, err := pba.AnalyzeDependencies(context.Background(), planningResult, objective, workspacePath)
	if err != nil {
		// Return error as JSON string
		errorResponse := fmt.Sprintf(`{"error": "dependency analysis failed: %s"}`, err.Error())
		return errorResponse
	}

	// Convert structured response back to JSON string
	jsonResponse, err := json.Marshal(breakdownResponse)
	if err != nil {
		// Return error as JSON string
		errorResponse := fmt.Sprintf(`{"error": "failed to marshal breakdown response: %s"}`, err.Error())
		return errorResponse
	}

	return string(jsonResponse)
}

// BreakdownStep represents a step in the breakdown analysis
type BreakdownStep struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	Dependencies  []string `json:"dependencies"`
	IsIndependent bool     `json:"is_independent"`
	Reasoning     string   `json:"reasoning"`
}

// BreakdownResponse represents the structured response from breakdown analysis
type BreakdownResponse struct {
	Steps []BreakdownStep `json:"steps"`
}

// GetBreakdownSchema returns the JSON schema for breakdown analysis
func (pba *PlanBreakdownAgent) GetBreakdownSchema() string {
	return `{
		"type": "object",
		"properties": {
			"steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"id": {
							"type": "string",
							"description": "Unique identifier for the step"
						},
						"description": {
							"type": "string",
							"description": "Clear description of what this step does"
						},
						"dependencies": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": "List of step IDs this step depends on"
						},
						"is_independent": {
							"type": "boolean",
							"description": "Whether this step can be executed independently"
						},
						"reasoning": {
							"type": "string",
							"description": "Clear explanation for independence assessment"
						}
					},
					"required": ["id", "description", "dependencies", "is_independent", "reasoning"]
				}
			}
		},
		"required": ["steps"]
	}`
}

// AnalyzeDependencies analyzes dependencies using structured output
func (pba *PlanBreakdownAgent) AnalyzeDependencies(ctx context.Context, planningResult, objective, workspacePath string) (*BreakdownResponse, error) {
	pba.AgentTemplate.GetLogger().Infof("🔍 Starting dependency analysis for plan breakdown")
	startTime := time.Now()

	// Emit structured output start event
	if pba.GetEventBridge() != nil {
		startEventData := &events.StructuredOutputEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: startTime,
			},
			Operation: "plan_breakdown_analysis",
			EventType: "structured_output_start",
		}

		// Create unified event wrapper
		unifiedEvent := &events.AgentEvent{
			Type:      events.StructuredOutputStart,
			Timestamp: startTime,
			Data:      startEventData,
		}

		// Emit through the bridge
		if bridge, ok := pba.GetEventBridge().(mcpagent.AgentEventListener); ok {
			bridge.HandleEvent(ctx, unifiedEvent)
			pba.AgentTemplate.GetLogger().Infof("✅ Emitted structured output start event")
		}
	}

	// Create LLM model on-demand if not available
	if pba.structuredOutputLLM == nil {
		llm, err := pba.BaseOrchestratorAgent.createLLM(ctx)
		if err != nil {
			// Emit error event
			pba.emitStructuredOutputError(ctx, "failed to create LLM model", err, startTime)
			return nil, fmt.Errorf("failed to create LLM model: %w", err)
		}
		pba.structuredOutputLLM = llm
	}

	// Use the Execute method with template variables
	templateVars := map[string]string{
		"PlanningResult": planningResult,
		"Objective":      objective,
		"WorkspacePath":  workspacePath,
	}

	// Generate the prompt using template
	prompt := pba.breakdownInputProcessor(templateVars)
	schema := pba.GetBreakdownSchema()

	// Create structured output generator
	config := mcpagent.LangchaingoStructuredOutputConfig{
		UseJSONMode:    true,
		ValidateOutput: true,
		MaxRetries:     2,
	}
	generator := mcpagent.NewLangchaingoStructuredOutputGenerator(pba.structuredOutputLLM, config, pba.AgentTemplate.GetLogger())

	// Generate structured output
	result, err := generator.GenerateStructuredOutput(ctx, prompt, schema)
	if err != nil {
		// Emit error event
		pba.emitStructuredOutputError(ctx, "failed to generate structured breakdown analysis", err, startTime)
		return nil, fmt.Errorf("failed to generate structured breakdown analysis: %w", err)
	}

	// Parse the JSON response
	var response BreakdownResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		// Emit error event
		pba.emitStructuredOutputError(ctx, "failed to parse breakdown response", err, startTime)
		return nil, fmt.Errorf("failed to parse breakdown response: %w", err)
	}

	duration := time.Since(startTime)

	// Emit structured output end event
	if pba.GetEventBridge() != nil {
		endEventData := &events.StructuredOutputEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Operation: "plan_breakdown_analysis",
			EventType: "structured_output_end",
			Duration:  duration.String(),
		}

		// Create unified event wrapper
		unifiedEvent := &events.AgentEvent{
			Type:      events.StructuredOutputEnd,
			Timestamp: time.Now(),
			Data:      endEventData,
		}

		// Emit through the bridge
		if bridge, ok := pba.GetEventBridge().(mcpagent.AgentEventListener); ok {
			bridge.HandleEvent(ctx, unifiedEvent)
			pba.AgentTemplate.GetLogger().Infof("✅ Emitted structured output end event")
		}
	}

	pba.AgentTemplate.GetLogger().Infof("✅ Dependency analysis completed successfully with %d steps", len(response.Steps))
	return &response, nil
}

// emitStructuredOutputError emits a structured output error event
func (pba *PlanBreakdownAgent) emitStructuredOutputError(ctx context.Context, operation string, err error, startTime time.Time) {
	if pba.GetEventBridge() != nil {
		duration := time.Since(startTime)
		errorEventData := &events.StructuredOutputEvent{
			BaseEventData: events.BaseEventData{
				Timestamp: time.Now(),
			},
			Operation: operation,
			EventType: "structured_output_error",
			Error:     err.Error(),
			Duration:  duration.String(),
		}

		// Create unified event wrapper
		unifiedEvent := &events.AgentEvent{
			Type:      events.StructuredOutputError,
			Timestamp: time.Now(),
			Data:      errorEventData,
		}

		// Emit through the bridge
		if bridge, ok := pba.GetEventBridge().(mcpagent.AgentEventListener); ok {
			bridge.HandleEvent(ctx, unifiedEvent)
			pba.AgentTemplate.GetLogger().Infof("✅ Emitted structured output error event")
		}
	}
}

// GetAgentType returns the agent type
func (pba *PlanBreakdownAgent) GetAgentType() AgentType {
	return PlanBreakdownAgentType
}

// GetAgentName returns a human-readable name for the agent
func (pba *PlanBreakdownAgent) GetAgentName() string {
	return "Plan Breakdown Agent"
}

// GetAgentDescription returns a description of what this agent does
func (pba *PlanBreakdownAgent) GetAgentDescription() string {
	return "Analyzes execution plans and identifies independent steps that can be executed in parallel"
}
