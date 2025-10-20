package todo_optimization

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/mcpagent"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

	"github.com/tmc/langchaingo/llms"
)

// TodoRefinePlannerTemplate holds template variables for todo refinement prompts
type TodoRefinePlannerTemplate struct {
	Objective        string
	WorkspacePath    string
	CritiqueFeedback string
}

// TodoRefinePlannerAgent extends BaseOrchestratorAgent with todo refinement functionality
type TodoRefinePlannerAgent struct {
	*agents.BaseOrchestratorAgent // ✅ REUSE: All base functionality
}

// NewTodoRefinePlannerAgent creates a new todo refine planner agent
func NewTodoRefinePlannerAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoRefinePlannerAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoRefinePlannerAgentType, // 🆕 NEW: Agent type
		eventBridge,
	)

	return &TodoRefinePlannerAgent{
		BaseOrchestratorAgent: baseAgent, // ✅ REUSE: All base functionality
	}
}

// Execute implements the OrchestratorAgent interface
func (trpa *TodoRefinePlannerAgent) Execute(ctx context.Context, templateVars map[string]string, messages []llms.MessageContent) (string, error) {
	// Extract objective from template variables
	objective, ok := templateVars["Objective"]
	if !ok {
		objective = "No objective provided"
	}

	// Extract workspace path from template variables
	workspacePath, ok := templateVars["WorkspacePath"]
	if !ok {
		workspacePath = "No workspace path provided"
	}

	// Extract critique feedback from template variables
	critiqueFeedback, ok := templateVars["CritiqueFeedback"]
	if !ok {
		critiqueFeedback = ""
	}

	// Prepare template variables
	refinementTemplateVars := map[string]string{
		"Objective":        objective,
		"WorkspacePath":    workspacePath,
		"CritiqueFeedback": critiqueFeedback,
	}

	// Execute using input processor
	return trpa.ExecuteWithInputProcessor(ctx, refinementTemplateVars, trpa.todoRefinePlannerInputProcessor, messages)
}

// todoRefinePlannerInputProcessor processes inputs specifically for todo refinement
func (trpa *TodoRefinePlannerAgent) todoRefinePlannerInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := TodoRefinePlannerTemplate{
		Objective:        templateVars["Objective"],
		WorkspacePath:    templateVars["WorkspacePath"],
		CritiqueFeedback: templateVars["CritiqueFeedback"],
	}

	// Define the template
	templateStr := `## PRIMARY TASK - REFINE TODO LIST

You are a todo list refinement agent. Analyze execution history and optimize the existing todo list based on learnings from execution.

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

{{if .CritiqueFeedback}}
## Previous Critique Feedback
Based on the previous critique, incorporate the following feedback to improve the refinement:

{{.CritiqueFeedback}}

Use this feedback to make the refinement more focused, accurate, and effective.
{{end}}

## Process
1. **Read execution data** from {{.WorkspacePath}}/runs/{xxx}/ folder
2. **Analyze todo completion status**: Categorize todos into completed, failed, and pending
3. **Extract execution history**: Document what worked, what failed, and why
4. **Identify critical steps**: What steps are essential to achieve the objective?
5. **Analyze patterns**: What worked, what failed, and why
6. **Optimize critical path**: Remove non-essential steps, fix sequencing
7. **Create focused todo list** with only critical steps
8. **Update {{.WorkspacePath}}/todo.md** file with complete execution history and explain changes

## Files to Read
- **{{.WorkspacePath}}/todo.md**: Current todo list (READ and UPDATE)
- **{{.WorkspacePath}}/runs/{xxx}/outputs/execution_output.md**: What happened during execution
- **{{.WorkspacePath}}/runs/{xxx}/outputs/validation_report.md**: What passed/failed validation
- **{{.WorkspacePath}}/runs/{xxx}/data/**: Supporting data from execution
- **{{.WorkspacePath}}/runs/{xxx}/artifacts/**: Files created during execution
- **{{.WorkspacePath}}/runs/{xxx}/logs/**: Detailed execution logs and error messages

` + memory.GetWorkflowMemoryRequirements() + `

## Critical Analysis
**Focus on identifying the CRITICAL STEPS needed to achieve the objective:**

### 1. Objective Achievement Analysis
- **What steps directly contribute to the objective?** (Keep these)
- **What steps are essential prerequisites?** (Must be done first)
- **What steps are nice-to-have but not critical?** (Consider removing)
- **What critical steps were missing?** (Add these)

### 2. Execution Learning Analysis
- **Success patterns**: What approaches/tools/sequences worked?
- **Failure patterns**: What caused failures or inefficiencies?
- **Root causes**: Why did steps fail? What assumptions were wrong?
- **Wrong order**: What steps were sequenced incorrectly?
- **Completed todos**: Document what was successfully completed with file references
- **Failed todos**: Document what failed with specific error details and root causes
- **Execution history**: Preserve complete history of all execution attempts

### 3. Critical Path Optimization
- **Identify the minimum viable path** to achieve the objective
- **Prioritize steps by impact** on objective achievement
- **Remove non-essential steps** that don't contribute to the goal
- **Ensure critical dependencies** are properly sequenced

**Create refined todo that:**
- **Focuses on critical steps** that directly achieve the objective
- **Removes non-essential steps** that don't contribute to the goal
- **Optimizes the critical path** for maximum efficiency
- **Incorporates execution learnings** to avoid past failures
- **Preserves execution history** for all completed and failed todos
- **Maintains detailed file references** to execution logs and artifacts
- **Documents key learnings** from both successes and failures

## Execution History Preservation
**CRITICAL**: When refining the todo list, you MUST:

1. **Preserve Completed Todos**: Keep all successfully completed todos in the execution history section
2. **Document Failed Todos**: Keep all failed todos with detailed error information and root causes
3. **Maintain File References**: Include file paths to detailed logs, outputs, and artifacts
4. **Capture Learnings**: Document what worked and what didn't for future reference
5. **Update Status Tracking**: Ensure all todo statuses are accurately reflected
6. **Create Comprehensive History**: Build a complete audit trail of all execution attempts

**Execution History Structure**:
` + "```" + `markdown
## Execution History Summary
### Completed Todos (X)
- **Todo 001**: Brief result summary → [Link to detailed logs]
- **Todo 002**: Brief result summary → [Link to detailed logs]

### Failed Todos (Y) 
- **Todo 003**: Failure reason → [Link to error logs]
- **Todo 004**: Failure reason → [Link to error logs]

### Current Status
- **Total Todos**: [Count]
- **Completed**: [Count]
- **Failed**: [Count]
- **Pending**: [Count]
- **In Progress**: [Count]
` + "```" + `

## Output Format
**IMPORTANT: Return ONLY clean markdown, not JSON or structured data**

**Provide:**
1. **Execution history analysis**: Summary of completed and failed todos with file references
2. **Critical steps identified**: Which steps are essential for achieving the objective
3. **Analysis summary**: What worked, what failed, key learnings from execution history
4. **Improvements made**: Specific changes and why (focus on critical path optimization)
5. **Updated todo list**: Clean markdown format with complete execution history preservation
6. **Status tracking**: Accurate counts and status for all todos

**Output should be:**
- Clean markdown text only
- No JSON structures or code blocks
- Evidence-based reasoning for changes

Begin the analysis and refinement process now.`

	// Parse and execute the template
	tmpl, err := template.New("todoRefinePlanner").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, templateData)
	if err != nil {
		return fmt.Sprintf("Error executing template: %v", err)
	}

	return result.String()
}
