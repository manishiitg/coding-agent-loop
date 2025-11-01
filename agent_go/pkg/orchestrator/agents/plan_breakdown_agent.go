package agents

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/llmtypes"
	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents/prompts"
)

// PlanBreakdownAgent analyzes dependencies and creates independent steps for parallel execution
type PlanBreakdownAgent struct {
	*BaseOrchestratorAgent
	breakdownPrompts *prompts.PlanBreakdownPrompts
}

// NewPlanBreakdownAgent creates a new plan breakdown agent
func NewPlanBreakdownAgent(config *OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *PlanBreakdownAgent {
	breakdownPrompts := prompts.NewPlanBreakdownPrompts()

	baseAgent := NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		PlanBreakdownAgentType,
		eventBridge,
	)

	return &PlanBreakdownAgent{
		BaseOrchestratorAgent: baseAgent,
		breakdownPrompts:      breakdownPrompts,
	}
}

// ExecuteStructured executes the plan breakdown agent and returns structured output
func (pba *PlanBreakdownAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (*BreakdownResponse, error) {
	// Define the JSON schema for breakdown analysis
	schema := `{
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

	// Use the base orchestrator agent's ExecuteStructured method
	result, err := ExecuteStructuredWithInputProcessor[BreakdownResponse](pba.BaseOrchestratorAgent, ctx, templateVars, pba.breakdownInputProcessor, conversationHistory, schema)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// Execute executes the plan breakdown agent using the standard agent pattern
func (pba *PlanBreakdownAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llmtypes.MessageContent) (string, []llmtypes.MessageContent, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	// This will automatically emit agent start/end events
	return pba.ExecuteWithInputProcessor(ctx, templateVars, pba.breakdownInputProcessor, conversationHistory)
}

// breakdownInputProcessor processes inputs specifically for plan breakdown - pure prompt renderer
func (pba *PlanBreakdownAgent) breakdownInputProcessor(templateVars map[string]string) string {
	// Use the predefined prompt with template variable replacement
	templateStr := pba.breakdownPrompts.AnalyzeDependenciesPrompt

	// Parse and execute the template
	tmpl, err := template.New("breakdown").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing breakdown template: %w", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateVars)
	if err != nil {
		return fmt.Sprintf("Error executing breakdown template: %w", err)
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
