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

// HumanControlledTodoPlannerPlanningTemplate holds template variables for human-controlled planning prompts
type HumanControlledTodoPlannerPlanningTemplate struct {
	Objective     string
	WorkspacePath string
}

// HumanControlledTodoPlannerPlanningAgent creates a fast, simplified plan from the objective
type HumanControlledTodoPlannerPlanningAgent struct {
	*agents.BaseOrchestratorAgent
}

// PlanStep represents a step in the planning output
type PlanStep struct {
	Title               string   `json:"title"`
	Description         string   `json:"description"`
	SuccessCriteria     string   `json:"success_criteria"`
	WhyThisStep         string   `json:"why_this_step"`
	ContextDependencies []string `json:"context_dependencies"`
	ContextOutput       string   `json:"context_output"`
}

// PlanningResponse represents the structured response from planning
type PlanningResponse struct {
	ObjectiveAnalysis string     `json:"objective_analysis"`
	Approach          string     `json:"approach"`
	Steps             []PlanStep `json:"steps"`
	ExpectedOutcome   string     `json:"expected_outcome"`
}

// NewHumanControlledTodoPlannerPlanningAgent creates a new human-controlled todo planner planning agent
func NewHumanControlledTodoPlannerPlanningAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *HumanControlledTodoPlannerPlanningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerPlanningAgentType, // Reuse the same type for now
		eventBridge,
	)

	return &HumanControlledTodoPlannerPlanningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// ExecuteStructured executes the planning agent and returns structured output
func (hctppa *HumanControlledTodoPlannerPlanningAgent) ExecuteStructured(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (*PlanningResponse, error) {
	// Define the JSON schema for planning output
	schema := `{
		"type": "object",
		"properties": {
			"objective_analysis": {
				"type": "string",
				"description": "Analysis of what needs to be achieved"
			},
			"approach": {
				"type": "string",
				"description": "Brief description of overall approach"
			},
			"steps": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"title": {
							"type": "string",
							"description": "Short, clear title for the step"
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
			},
			"expected_outcome": {
				"type": "string",
				"description": "What the complete plan should achieve"
			}
		},
		"required": ["objective_analysis", "approach", "steps", "expected_outcome"]
	}`

	// Use the base orchestrator agent's ExecuteStructured method
	result, err := agents.ExecuteStructuredWithInputProcessor[PlanningResponse](hctppa.BaseOrchestratorAgent, ctx, templateVars, hctppa.humanControlledPlanningInputProcessor, conversationHistory, schema)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// Execute implements the OrchestratorAgent interface
func (hctppa *HumanControlledTodoPlannerPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Use ExecuteWithInputProcessor to get agent events (orchestrator_agent_start/end)
	// This will automatically emit agent start/end events
	return hctppa.ExecuteWithInputProcessor(ctx, templateVars, hctppa.humanControlledPlanningInputProcessor, conversationHistory)
}

// humanControlledPlanningInputProcessor processes inputs specifically for fast, simplified planning
func (hctppa *HumanControlledTodoPlannerPlanningAgent) humanControlledPlanningInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := HumanControlledTodoPlannerPlanningTemplate{
		Objective:     templateVars["Objective"],
		WorkspacePath: templateVars["WorkspacePath"],
	}

	// Define the template - simplified for direct planning with structured output
	templateStr := `## 🚀 PRIMARY TASK - CREATE STRUCTURED PLAN TO EXECUTE OBJECTIVE

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Planning Agent
- **Responsibility**: Create a comprehensive structured plan to execute the objective

## 📋 PLANNING GUIDELINES
- **Comprehensive Scope**: Create complete plan to achieve objective
- **Actionable Steps**: Each step should be concrete and executable
- **Clear Success Criteria**: Define how to verify each step worked
- **Logical Order**: Steps should follow logical sequence
- **Focus on Strategy**: Plan what needs to be done, not how to do it (execution details will be handled by execution agents)

## 🤖 MULTI-AGENT COORDINATION
**Important**: Different agents will execute this plan. Ensure proper coordination through workspace files.

### **Context Sharing Rules**
- **Read context** from previous steps using relative paths (e.g., "../execution/step_1_context.md")
- **Write context** for next steps with clear documentation
- **Use relative paths only** - NEVER use absolute paths
- **Document findings** in workspace files for other agents

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

**RETURN STRUCTURED JSON RESPONSE ONLY**

Create a comprehensive plan and return it as a JSON object with the following structure:

The response should be a JSON object with:
- objective_analysis: Analysis of what needs to be achieved
- approach: Brief description of overall approach
- steps: Array of step objects, each with:
  - title: Short, clear title for the step
  - description: Detailed description of what this step accomplishes
  - success_criteria: How to verify this step was completed successfully
  - why_this_step: How this step contributes to achieving the objective
  - context_dependencies: Array of context files from previous steps that this step depends on (OPTIONAL - RELATIVE PATHS ONLY)
  - context_output: Single string - what context file this step will create for other agents (OPTIONAL - RELATIVE PATH ONLY - NOT AN ARRAY)
- expected_outcome: What the complete plan should achieve

Example JSON structure:
` + "```json" + `
{
  "objective_analysis": "Analysis of what needs to be achieved",
  "approach": "Brief description of overall approach",
  "steps": [
    {
      "title": "Step 1 Title",
      "description": "Step 1 description",
      "success_criteria": "How to verify completion",
      "why_this_step": "Why this step is needed",
      "context_dependencies": ["../execution/step_0_context.md"],
      "context_output": "./execution/step_1_context.md"
    }
  ],
  "expected_outcome": "What the complete plan should achieve"
}
` + "```" + `

**Note**: Focus on creating a clear, actionable plan to execute the objective. Each step should be concrete and contribute directly to achieving the goal. Remember that different steps may be executed by different agents, so include context dependencies and outputs to ensure proper coordination and memory sharing across the multi-agent system.`

	// Parse and execute the template
	tmpl, err := template.New("human_controlled_planning").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing human-controlled planning template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing human-controlled planning template: %v", err)
	}

	return result.String()
}
