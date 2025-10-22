package todo_creation_human

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"

	"github.com/tmc/langchaingo/llms"
)

// HumanControlledPlanBreakdownAgent analyzes dependencies and creates independent steps for parallel execution
type HumanControlledPlanBreakdownAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewHumanControlledPlanBreakdownAgent creates a new human-controlled plan breakdown agent
func NewHumanControlledPlanBreakdownAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledPlanBreakdownAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.PlanBreakdownAgentType,
		eventBridge,
	)

	return &HumanControlledPlanBreakdownAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// TodoStep represents a todo step in the breakdown analysis
type TodoStep struct {
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	WhyThisStep         string   `json:"why_this_step"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
}

// BreakdownResponse represents the structured response from breakdown analysis
type BreakdownResponse struct {
	Steps []TodoStep `json:"steps"`
}

// ExecuteStructured executes the plan breakdown agent and returns structured output
func (hcpba *HumanControlledPlanBreakdownAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (*BreakdownResponse, error) {
	// Define the JSON schema for todo breakdown analysis
	schema := `{
		"type": "object",
		"properties": {
			"steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"title": {
							"type": "string",
							"description": "Short, clear title for the todo step"
						},
						"description": {
							"type": "string",
							"description": "Detailed description of what this step accomplishes"
						},
						"success_criteria": {
							"type": "string",
							"description": "How to verify this step was completed successfully"
						},
						"why_this_step": {
							"type": "string",
							"description": "How this step contributes to achieving the objective"
						},
						"context_dependencies": {
							"type": "array",
							"items": {
								"type": "string"
							},
							"description": "List of context files from previous steps that this step depends on"
						},
						"context_output": {
							"type": "string",
							"description": "What context file this step will create for other agents"
						}
					},
					"required": ["title", "description", "success_criteria", "why_this_step"]
				}
			}
		},
		"required": ["steps"]
	}`

	// Use the base orchestrator agent's ExecuteStructured method
	result, err := agents.ExecuteStructuredWithInputProcessor[BreakdownResponse](hcpba.BaseOrchestratorAgent, ctx, templateVars, hcpba.breakdownInputProcessor, conversationHistory, schema)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// Execute executes the plan breakdown agent using the standard agent pattern
func (hcpba *HumanControlledPlanBreakdownAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	// This will automatically emit agent start/end events
	return hcpba.ExecuteWithInputProcessor(ctx, templateVars, hcpba.breakdownInputProcessor, conversationHistory)
}

// breakdownInputProcessor processes inputs for plan breakdown analysis
func (hcpba *HumanControlledPlanBreakdownAgent) breakdownInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := map[string]string{
		"Objective":     templateVars["Objective"],
		"WorkspacePath": templateVars["WorkspacePath"],
	}

	// Define the template for plan breakdown
	templateStr := `## 🔍 PRIMARY TASK - Convert the plan into a list of steps

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/todo_creation_human/planning/plan.md (plan to analyze)

## 📋 BREAKDOWN GUIDELINES
- Breakdown the plan into steps

## 📤 Output Format

**RETURN STRUCTURED JSON RESPONSE ONLY**

Analyze the planning result and break it down into executable steps. Return a JSON response with the following structure:

The response should be a JSON object with a steps array. Each step should have:
- title: Short, clear title for the todo step
- description: Detailed description of what this step accomplishes
- success_criteria: How to verify this step was completed successfully
- why_this_step: How this step contributes to achieving the objective
- context_dependencies: Array of context files from previous steps that this step depends on (OPTIONAL - RELATIVE PATHS ONLY)
- context_output: Single string - what context file this step will create for other agents (OPTIONAL - RELATIVE PATH ONLY - NOT AN ARRAY)

Example JSON structure:
` + "```json" + `
{
  "steps": [
    {
      "title": "Step 1 Title",
      "description": "Step 1 description",
      "success_criteria": "How to verify completion",
      "why_this_step": "Why this step is needed",
      "context_dependencies": ["../execution/step_0_context.md"],
      "context_output": "./execution/step_1_context.md"
    }
  ]
}
` + "```" + ``

	// Parse and execute the template
	tmpl, err := template.New("breakdown").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing breakdown template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing breakdown template: %v", err)
	}

	return result.String()
}
