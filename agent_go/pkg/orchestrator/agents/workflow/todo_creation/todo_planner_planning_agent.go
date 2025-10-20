package todo_creation

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"mcp-agent/agent_go/internal/observability"
	"mcp-agent/agent_go/internal/utils"
	"mcp-agent/agent_go/pkg/orchestrator"
	"mcp-agent/agent_go/pkg/orchestrator/agents"
	"mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

	"github.com/tmc/langchaingo/llms"
)

// TodoPlannerPlanningTemplate holds template variables for planning prompts
type TodoPlannerPlanningTemplate struct {
	Objective     string
	WorkspacePath string
	Strategy      string
	Focus         string
	Iteration     string
	MaxIterations string
}

// TodoPlannerPlanningAgent creates a step-wise plan from the objective
type TodoPlannerPlanningAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewTodoPlannerPlanningAgent creates a new todo planner planning agent
func NewTodoPlannerPlanningAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) *TodoPlannerPlanningAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerPlanningAgentType,
		eventBridge,
	)

	return &TodoPlannerPlanningAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (tppa *TodoPlannerPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract variables from template variables
	objective := templateVars["Objective"]
	workspacePath := templateVars["WorkspacePath"]

	// Use strategy from orchestrator (iteration-aware) or fall back to default
	strategy := templateVars["Strategy"]
	focus := templateVars["Focus"]
	if strategy == "" {
		strategy = "Default Strategy"
		focus = "Create comprehensive plan"
	}

	// Prepare template variables with iteration-aware strategy
	planningTemplateVars := map[string]string{
		"Objective":     objective,
		"WorkspacePath": workspacePath,
		"Strategy":      strategy,
		"Focus":         focus,
	}

	// Execute using input processor
	return tppa.ExecuteWithInputProcessor(ctx, planningTemplateVars, tppa.planningInputProcessor, conversationHistory)
}

// planningInputProcessor processes inputs specifically for step-wise planning
func (tppa *TodoPlannerPlanningAgent) planningInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := TodoPlannerPlanningTemplate{
		Objective:     templateVars["Objective"],
		WorkspacePath: templateVars["WorkspacePath"],
		Strategy:      templateVars["Strategy"],
		Focus:         templateVars["Focus"],
		Iteration:     templateVars["Iteration"],
		MaxIterations: templateVars["MaxIterations"],
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - INCREMENTAL ITERATIVE PLANNING

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}
**ITERATION**: {{.Iteration}} of {{.MaxIterations}}
**ITERATION STRATEGY**: {{.Strategy}}
**CURRENT FOCUS**: {{.Focus}}

**CORE TASK**: Create incremental plan for THIS iteration. This is a long iterative process - plan small, executable chunks. Focus on {{.Focus}}.

## 🔍 Understanding Your Role

**IMPORTANT - Iterative Planning**:
- 📝 **Plan** = Incremental exploration across many iterations (NOT all at once)
- 🔄 **Iterations**: 10+ iterations to discover optimal approach
- 📄 **Todo List** = Final synthesis by Writer (after ALL iterations)
- 🎯 **Goal**: Incremental discovery through small experiments

**Iteration Strategy Phases**:
- **Iterations 1-3**: "Optimization & Method Discovery" - Try different approaches
- **Iterations 4-6**: "Refinement & Validation" - Refine what worked
- **Iterations 7-10**: "Completion & Execution" - Complete using best methods

## 📁 Read Previous Work First (If Iteration > 1)

Read these files to understand what was already tried:

### Previous Planning
- {{.WorkspacePath}}/todo_creation/planning/plan.md (all "## Iteration X" sections)
  - See what approaches were already tried
  - Understand the evolution of planning

### Execution Results (What Worked/Failed)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (all iterations)
  - Check success rates of different approaches
  - Identify which MCP tools worked best
  - Learn from failures and pivot approaches

### Validation Results (What Was Verified)
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md
  - See which claims were validated
  - Understand evidence quality
  - Avoid unvalidated approaches

**Use these learnings to plan BETTER for THIS iteration!**

## 📋 Planning Guidelines
- **Analyze Previous Work**: Read all previous iterations from files above
- **Plan Small Chunks**: Plan ONLY what can be executed in THIS iteration (1-3 steps max)
- **Build on Learnings**: Use execution results to choose better approaches
- **Pivot Based on Results**: If Iteration 1 API approach failed, try different approach in Iteration 2
- **Append to Plan**: Add "## Iteration X" section to planning/plan.md
- **Stay Incremental**: Don't try to solve everything - just next small step
- **Evidence-Based**: Only plan approaches that have evidence of working (from validation)

**⚠️ IMPORTANT**: Only create/modify files within {{.WorkspacePath}}/todo_creation/ folder structure.

` + memory.GetWorkflowMemoryRequirements() + `

## 📤 Output Format

**APPEND to** {{.WorkspacePath}}/todo_creation/planning/plan.md

---

## 🔄 Iteration {{.Iteration}} - {{.Strategy}}
**Date**: [Current date/time]
**Focus**: {{.Focus}}
**Progress**: Iteration {{.Iteration}} of {{.MaxIterations}}
**Previous Iterations**: [Read from plan.md - summarize key learnings]

### What We Learned So Far
[If iteration > 1, summarize from previous sections in plan.md]
- Iteration 1: [What approach/method was tried, results]
- Iteration 2: [What was tried, results]
- etc.

### This Iteration's Plan
**Approach to Try**: [Name - e.g., "Try direct API approach" or "Refine web scraping method"]
**Rationale**: [Why this approach? Based on previous learnings or first exploration]

#### Steps for This Iteration
**Step 1**: [First small step]
- **Description**: [What to do]
- **MCP Server**: [Server to use]
- **MCP Tool**: [Tool name]
- **Tool Arguments**: [Arguments]
- **Success Criteria**: [How to verify it worked]
- **Why This Step**: [How it builds on previous work]

**Step 2**: [Second small step - only if feasible in one iteration]
- **Description**: [What to do]
- **MCP Server**: [Server]
- **MCP Tool**: [Tool]
- **Success Criteria**: [Verification]

### Expected Learnings
- [What we hope to discover from this iteration]
- [What questions this iteration will answer]

### Next Iteration Plan
- [If this works: try...]
- [If this fails: pivot to...]

---

**Note**: Keep plans SMALL - only 1-3 steps per iteration that can realistically be executed and validated.

Focus on incremental progress through small, testable steps.`

	// Parse and execute the template
	tmpl, err := template.New("planning").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing planning template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing planning template: %v", err)
	}

	return result.String()
}
