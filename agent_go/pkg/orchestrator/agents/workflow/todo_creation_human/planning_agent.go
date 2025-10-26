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
	SuccessPatterns     []string `json:"success_patterns,omitempty"` // NEW - what worked (includes tools)
	FailurePatterns     []string `json:"failure_patterns,omitempty"` // NEW - what failed (includes tools to avoid)
}

// PlanningResponse represents the structured response from planning
type PlanningResponse struct {
	Steps []PlanStep `json:"steps"`
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
- **Agent Execution Limits**: Each step should be completable by one agent using MCP tools before reaching context output limits
- **Success/Failure Patterns**: ONLY include these if you have specific MCP tools, exact commands, or clear patterns from previous executions. Do NOT add empty or generic patterns.

## 🤖 MULTI-AGENT COORDINATION
- **Different Agents**: Each step is executed by a different agent
- **Data Sharing**: Steps may need to share context/data between each other
- **Context Dependencies**: Each step should specify what context files it needs from previous steps
- **Context Output**: Each step should specify what context file it will create for subsequent steps
- **Workspace Files**: Store data in workspace files when steps need to share information
- **Use relative paths only** - NEVER use absolute paths
- **Document findings** in workspace files for other agents

## 📝 EXAMPLE OF A WELL-FORMED STEP

` + "```markdown" + `
### Step 1: Analyze Codebase Structure
- **Description**: Use grep and read_file tools to identify all TypeScript files in the src/ directory and understand the project structure. Create a comprehensive map of main modules, their relationships, and key entry points.
- **Success Criteria**: Complete file tree with main modules identified and documented in codebase_structure.md
- **Why This Step**: Understanding the codebase structure is foundational for making informed changes without breaking existing functionality
- **Context Dependencies**: none (first step)
- **Context Output**: codebase_structure.md
- **Success Patterns**: 
  - Used grep with --type typescript flag to find all .ts files efficiently
  - read_file with line limits (max 1000 lines) prevented context overflow on large files
  - list_dir tool helped map directory structure before reading individual files
- **Failure Patterns**:
  - Avoid read_file without line limits on large files (causes context limit errors)
  - Don't use codebase_search for simple file listing (grep is faster and more reliable)
` + "```" + `

` + GetTodoCreationHumanMemoryRequirements() + `

## 📤 Output Format

**UPDATE PLAN.MD FILE**

**CRITICAL**: 
- Always update the existing plan.md file in the workspace
- If no plan exists, create a new one
- **DO NOT read any other files from the workspace** - only work with plan.md
- Focus on creating/updating plan.md, not on investigating the workspace structure

**File to Update:**
- **{{.WorkspacePath}}/todo_creation_human/planning/plan.md**

## 📋 MARKDOWN PLAN STRUCTURE

Create a markdown plan with this structure:

` + "```markdown" + `
# Plan: [Objective Title]

## Steps

### Step 1: [Step Name]
- **Description**: [Detailed description of what this step accomplishes - should be completable by one agent using MCP tools]
- **Success Criteria**: [How to verify this step was completed successfully]
- **Why This Step**: [How this step contributes to achieving the objective]
- **Context Dependencies**: [List of context files from previous steps - use "none" if first step]
- **Context Output**: [What context file this step will create for subsequent steps - e.g., "step_1_results.md"]
- **Success Patterns**: [Optional - ONLY include if you have specific tools/approaches that worked in previous executions]
- **Failure Patterns**: [Optional - ONLY include if you have specific tools/approaches that failed in previous executions]

[Continue this pattern for all steps...]
` + "```" + `

## 📤 YOUR RESPONSE AFTER WRITING FILE

After successfully writing the plan.md file, respond with:
- Brief summary of the plan created
- Number of steps in the plan
- Key milestones or phases identified
- Confirmation that plan.md was written successfully

**Example Response:**
"I've created a comprehensive plan with 5 steps in {{.WorkspacePath}}/todo_creation_human/planning/plan.md:
1. Analyze codebase structure
2. Identify modification points
3. Implement changes
4. Run tests
5. Document changes

The plan focuses on systematic analysis before implementation, with clear context handoffs between steps."

**IMPORTANT NOTES**: 
1. Focus on creating a clear, actionable markdown plan
2. Each step should be concrete and contribute directly to achieving the goal
3. Include context dependencies and outputs for multi-agent coordination
4. Remember: Success/Failure Patterns are OPTIONAL and should only be included when you have specific, concrete examples from previous executions
`

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
