package todo_creation

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

// TodoPlannerWriterTemplate holds template variables for todo writing prompts
type TodoPlannerWriterTemplate struct {
	Objective        string
	PlanResult       string
	ExecutionResult  string
	ValidationResult string
	CritiqueResult   string
	WorkspacePath    string
	TotalIterations  string
}

// TodoPlannerWriterAgent creates optimal todo list based on execution experience
type TodoPlannerWriterAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewTodoPlannerWriterAgent creates a new todo planner writer agent
func NewTodoPlannerWriterAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoPlannerWriterAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerWriterAgentType,
		eventBridge,
	)

	return &TodoPlannerWriterAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (tpwa *TodoPlannerWriterAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract variables from template variables
	// Writer is strategic - synthesizes from all iterations
	objective := templateVars["Objective"]
	planResult := templateVars["PlanResult"]
	executionResult := templateVars["ExecutionResult"]
	validationResult := templateVars["ValidationResult"]
	critiqueResult := templateVars["CritiqueResult"]
	workspacePath := templateVars["WorkspacePath"]
	totalIterations := templateVars["TotalIterations"]
	if strings.TrimSpace(totalIterations) == "" {
		totalIterations = "1"
	}

	// Prepare template variables
	writerTemplateVars := map[string]string{
		"Objective":        objective,
		"PlanResult":       planResult,
		"ExecutionResult":  executionResult,
		"ValidationResult": validationResult,
		"CritiqueResult":   critiqueResult,
		"WorkspacePath":    workspacePath,
		"TotalIterations":  totalIterations,
	}

	// Execute using input processor
	return tpwa.ExecuteWithInputProcessor(ctx, writerTemplateVars, tpwa.writerInputProcessor, conversationHistory)
}

// writerInputProcessor processes inputs specifically for todo list creation
func (tpwa *TodoPlannerWriterAgent) writerInputProcessor(templateVars map[string]string) string {
	// Create template data
	totalIterationsForTemplate := templateVars["TotalIterations"]
	if strings.TrimSpace(totalIterationsForTemplate) == "" {
		totalIterationsForTemplate = "1"
	}
	templateData := TodoPlannerWriterTemplate{
		Objective:        templateVars["Objective"],
		PlanResult:       templateVars["PlanResult"],
		ExecutionResult:  templateVars["ExecutionResult"],
		ValidationResult: templateVars["ValidationResult"],
		CritiqueResult:   templateVars["CritiqueResult"],
		WorkspacePath:    templateVars["WorkspacePath"],
		TotalIterations:  totalIterationsForTemplate,
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - SYNTHESIZE FINAL TODO LIST FROM ALL ITERATIONS

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}
**TOTAL ITERATIONS**: {{.TotalIterations}}

## 🤖 AGENT IDENTITY
- **Role**: Writer Agent
- **Responsibility**: Synthesize best methods from ALL iterations into final todo list
- **Mode**: Strategic (analyze all iterations, create production-ready output)

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/todo_creation/planning/plan.md (ALL "## Iteration X" sections)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (ALL iterations)
- {{.WorkspacePath}}/todo_creation/execution/completed_steps.md
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md (ALL validations)
- {{.WorkspacePath}}/todo_creation/execution/evidence/

**WRITE:**
- **CREATE** {{.WorkspacePath}}/todo_creation/iteration_analysis.md (comparison of all iterations)
- **UPDATE** {{.WorkspacePath}}/todo_creation/todo.md (final synthesized todo list)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation/
- Must read ALL {{.TotalIterations}} iterations (not just latest)
- Keep todo.md under 1000 lines (concise and actionable)

## 🔍 YOUR SYNTHESIS MISSION

**Core Task:** Analyze ALL {{.TotalIterations}} iterations to identify methods with HIGHEST success rates, then create final todo list using ONLY proven methods.

**Process:**
1. Read ALL "## Iteration X" sections from plan.md and execution_results.md
2. Calculate success rate for each approach tried
3. Select methods with highest success + validation
4. Create 2 outputs: iteration_analysis.md (comparison) + todo.md (final list)

` + GetTodoCreationMemoryRequirements() + `

## 📁 Read ALL Iteration History First

**CRITICAL**: You MUST read ALL "## Iteration X" sections from these files:

### Planning History (All Approaches Tried)
- {{.WorkspacePath}}/todo_creation/planning/plan.md
  - Read EVERY "## Iteration X" section (not just latest!)
  - See ALL approaches that were planned
  - Understand evolution of planning strategy

### Execution History (What Worked/Failed)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md
  - Read EVERY "## Iteration X" section (not just latest!)
  - Compare success rates across iterations
  - Identify which approaches/tools worked best
  - Learn from failures and rejected methods
- {{.WorkspacePath}}/todo_creation/execution/completed_steps.md
  - Track all completed work
  - Extract proven MCP tools and arguments

### Validation History (What Was Verified)
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md
  - Read EVERY "## Iteration X" validation
  - See evidence quality across iterations
  - Only use validated methods in final todo

### Evidence Files
- {{.WorkspacePath}}/todo_creation/execution/evidence/
  - Review critical evidence from all iterations

## 🔍 Extract Best Methods from ALL Iterations
Parse ALL iterations to extract proven methods:
- **MCP Servers**: Which servers worked best (aws, gitlab, github, filesystem)
- **Tool Names**: Exact tools with highest success rates (aws_cli_query, gitlab_get_project)
- **Arguments**: Exact parameters that were successful
- **Patterns**: Approaches that worked consistently across iterations
- **Success Rates**: Compare approaches across iterations (Iteration 1: 80%, Iteration 3: 95%)

**Comparison Process**:
1. List all approaches tried across iterations
2. Calculate success rate for each approach
3. Identify highest performing methods
4. Select BEST methods for final todo list

## 📤 Output Format - CREATE 2 FILES

### File 1: {{.WorkspacePath}}/todo_creation/iteration_analysis.md

# Iteration Analysis - {{.TotalIterations}} Iterations

## Project Overview
- **Objective**: {{.Objective}}
- **Created**: [Current date]
- **Total Iterations**: [Number of iterations run]
- **Best Methods**: [Synthesized from iteration history]
- **Total Steps in Todo**: [Final count]

## Methods Tried Across Iterations
**Iteration 1**: [Approach, success rate %]
**Iteration 2**: [Approach, success rate %]
[etc for all iterations...]

## 🏆 Winner: Best Method Selected
- **Method**: [Highest success rate method]
- **Success Rate**: [%]
- **Source**: Iteration [X]
- **Why Best**: [Evidence from validation]

---

### File 2: {{.WorkspacePath}}/todo_creation/todo.md

# Todo List - {{.Objective}}

## Prerequisites
[What needs to be set up first]

## Steps

### Step 1: [Step Name]
- **What**: [Indepth description]
- **How**: MCP Server: [server], Tool: [tool], Arguments: [args]
- **Success**: [How to verify it worked]
- **Dependencies**: [Prerequisites]

### Step 2: [Step Name]
- **What**: [Indepth description]
- **How**: MCP Server: [server], Tool: [tool], Arguments: [args]
- **Success**: [How to verify]
- **Dependencies**: [Prerequisites]

[Continue for all steps - with indepth descriptions and be comprehensive]

## Summary
- **Total Steps**: [Count]

---

Focus on clarity and actionability. Each step has only 4 fields: What, How, Success, Dependencies.`

	// Parse and execute the template
	tmpl, err := template.New("writer").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing writer template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing writer template: %v", err)
	}

	return result.String()
}
