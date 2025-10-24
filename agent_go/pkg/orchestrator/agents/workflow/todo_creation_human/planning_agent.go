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

**WRITE MARKDOWN PLAN FILE - DO NOT RETURN JSON**

**CRITICAL**: 
- Write plan.md file to workspace - do NOT return JSON response
- Do NOT use structured output - generate markdown plan instead
- Create comprehensive markdown plan for human review

Write the following file:

1. **{{.WorkspacePath}}/todo_creation_human/planning/plan.md**
   - Create a comprehensive markdown plan file
   - Include objective analysis, approach, and all steps
   - Use clear markdown formatting with headers and lists
   - This plan will be reviewed by humans before execution

## 📋 MARKDOWN PLAN STRUCTURE

Create a markdown plan with this structure:

` + "```markdown" + `
# Plan: [Objective Title]

## Objective Analysis
[Analysis of what needs to be achieved]

## Approach
[Brief description of overall approach]

## Steps

### Step 1: [Step Name]
- **Description**: [Detailed description of what this step accomplishes]
- **Success Criteria**: [How to verify this step was completed successfully]
- **Why This Step**: [How this step contributes to achieving the objective]
- **Context Dependencies**: [List of context files from previous steps]
- **Context Output**: [What context file this step will create]

### Step 2: [Step Name]
- **Description**: [Detailed description of what this step accomplishes]
- **Success Criteria**: [How to verify this step was completed successfully]
- **Why This Step**: [How this step contributes to achieving the objective]
- **Context Dependencies**: [List of context files from previous steps]
- **Context Output**: [What context file this step will create]

### Step 3: [Step Name]
[Continue pattern for all steps...]

## Expected Outcome
[What the complete plan should achieve]
` + "```" + `

**IMPORTANT NOTES**: 
1. Focus on creating a clear, actionable markdown plan
2. Each step should be concrete and contribute directly to achieving the goal
3. Include context dependencies and outputs for multi-agent coordination
4. **WRITE plan.md FILE** - do not return JSON response
5. The plan reader agent will convert this to JSON in the next phase`

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
