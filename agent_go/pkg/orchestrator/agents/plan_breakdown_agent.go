package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"text/template"
	"time"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/events"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents/prompts"

	"github.com/tmc/langchaingo/llms"
)

// PlanBreakdownStructuredOutputEvent represents structured output operation events for plan breakdown
type PlanBreakdownStructuredOutputEvent struct {
	events.BaseEventData
	Operation string `json:"operation"`
	EventType string `json:"event_type"`
	Error     string `json:"error,omitempty"`
	Duration  string `json:"duration,omitempty"`
}

// GetEventType returns the event type for PlanBreakdownStructuredOutputEvent
func (e *PlanBreakdownStructuredOutputEvent) GetEventType() events.EventType {
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
	return pba.ExecuteWithInputProcessor(ctx, templateVars, pba.breakdownInputProcessor, conversationHistory)
}

// breakdownInputProcessor processes inputs specifically for plan breakdown using structured output
func (pba *PlanBreakdownAgent) breakdownInputProcessor(templateVars map[string]string) string {
	// Use the predefined prompt with template variable replacement
	templateStr := pba.breakdownPrompts.AnalyzeDependenciesPrompt

	// Parse and execute the template
	tmpl, err := template.New("breakdown").Parse(templateStr)
	if err != nil {
		// Log the full error for debugging
		if pba.AgentTemplate != nil && pba.AgentTemplate.GetLogger() != nil {
			pba.AgentTemplate.GetLogger().Errorf("Error parsing breakdown template: %v", err)
		} else {
			log.Printf("Error parsing breakdown template: %v", err)
		}
		// Return original template with safe context
		return templateStr + "\n\n[NOTE: rendering failed; using raw template]"
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateVars)
	if err != nil {
		// Log the full error for debugging
		if pba.AgentTemplate != nil && pba.AgentTemplate.GetLogger() != nil {
			pba.AgentTemplate.GetLogger().Errorf("Error executing breakdown template: %v", err)
		} else {
			log.Printf("Error executing breakdown template: %v", err)
		}
		// Return original template with safe context
		return templateStr + "\n\n[NOTE: rendering failed; using raw template]"
	}

	return result.String()
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

// AnalyzeDependenciesStructured analyzes dependencies using structured output
func (pba *PlanBreakdownAgent) AnalyzeDependenciesStructured(ctx context.Context, planningResult, objective, workspacePath string) (*BreakdownResponse, error) {
	pba.AgentTemplate.GetLogger().Infof("🔍 Starting structured dependency analysis for plan breakdown")
	startTime := time.Now()

	// Emit structured output start event
	if pba.GetEventBridge() != nil {
		startEventData := &PlanBreakdownStructuredOutputEvent{
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
		endEventData := &PlanBreakdownStructuredOutputEvent{
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

	pba.AgentTemplate.GetLogger().Infof("✅ Structured dependency analysis completed successfully with %d steps", len(response.Steps))
	return &response, nil
}

// emitStructuredOutputError emits a structured output error event
func (pba *PlanBreakdownAgent) emitStructuredOutputError(ctx context.Context, operation string, err error, startTime time.Time) {
	if pba.GetEventBridge() != nil {
		duration := time.Since(startTime)
		errorEventData := &PlanBreakdownStructuredOutputEvent{
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

// AnalyzeDependencies analyzes the planning result and returns independent steps
func (pba *PlanBreakdownAgent) AnalyzeDependencies(ctx context.Context, planningResult, objective, workspacePath string) (string, error) {
	pba.AgentTemplate.GetLogger().Infof("🔍 Starting dependency analysis for plan breakdown")

	// Try structured output first if available
	if pba.structuredOutputLLM != nil {
		response, err := pba.AnalyzeDependenciesStructured(ctx, planningResult, objective, workspacePath)
		if err != nil {
			pba.AgentTemplate.GetLogger().Warnf("⚠️ Structured analysis failed, falling back to text: %v", err)
		} else {
			// Return structured response as JSON string for backward compatibility
			jsonBytes, err := json.Marshal(response)
			if err != nil {
				pba.AgentTemplate.GetLogger().Warnf("⚠️ Failed to marshal structured response, falling back to text: %v", err)
			} else {
				return string(jsonBytes), nil
			}
		}
	}

	// Fallback to original text-based approach
	templateVars := map[string]string{
		"PlanningResult": planningResult,
		"Objective":      objective,
		"WorkspacePath":  workspacePath,
	}

	response, err := pba.Execute(ctx, templateVars, []llms.MessageContent{})
	if err != nil {
		return "", fmt.Errorf("failed to generate dependency analysis: %w", err)
	}

	pba.AgentTemplate.GetLogger().Infof("✅ Dependency analysis completed successfully")
	return response, nil
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
