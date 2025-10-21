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
	Title       string `json:"title"`
	Description string `json:"description"`
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
						}
					},
					"required": ["title", "description"]
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
func (hcpba *HumanControlledPlanBreakdownAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
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
	templateStr := `## 🔍 PRIMARY TASK - BREAK DOWN PLAN INTO SINGLE-GO EXECUTABLE STEPS

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Plan Breakdown Agent
- **Responsibility**: Break down the high-level plan into steps that can be executed in a single go by the LLM
- **Mode**: JSON-structured step creation with logical flow

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/todo_creation_human/planning/plan.md (plan to analyze)

**RESTRICTIONS:**
- Focus on creating steps that make logical sense for single-go execution
- Each step should be a complete, executable unit of work
- Return structured JSON response only (no file creation)

## 📋 BREAKDOWN GUIDELINES
- **Create Executable Steps**: Each step should be a complete, self-contained task that can be executed in one go
- **Logical Sequence**: Steps should follow a logical order and flow naturally from one to the next
- **Single-Go Execution**: Each step should be designed for the LLM to complete fully without needing to break it down further
- **Clear Boundaries**: Each step should have clear start and end points
- **Actionable Tasks**: Focus on concrete, actionable tasks rather than abstract concepts
- **Complete Work Units**: Each step should represent a meaningful unit of work that produces a tangible result

## 📤 Output Format

**RETURN STRUCTURED JSON RESPONSE ONLY**

Analyze the planning result and break it down into executable steps. Return a JSON response with the following structure:

The response should be a JSON object with a steps array. Each step should have:
- title: Short, clear title for the todo step
- description: Detailed description of what this step accomplishes

Requirements:
- Each step should be a complete, logical unit of work
- Steps should be executable in a single go by the LLM
- Focus on logical flow and executable completeness
- Return only the JSON structure, no additional text or formatting`

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
