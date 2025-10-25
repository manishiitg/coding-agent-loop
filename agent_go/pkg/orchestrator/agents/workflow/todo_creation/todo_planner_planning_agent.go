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
func NewTodoPlannerPlanningAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoPlannerPlanningAgent {
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
func (tppa *TodoPlannerPlanningAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Extract variables from template variables
	objective := templateVars["Objective"]
	workspacePath := templateVars["WorkspacePath"]

	// Use strategy from orchestrator (iteration-aware) or fall back to default
	strategy := templateVars["Strategy"]
	focus := templateVars["Focus"]
	if strategy == "" {
		strategy = "Default Strategy"
	}
	if focus == "" {
		focus = "Create comprehensive plan"
	}

	// Prepare template variables with iteration-aware strategy
	planningTemplateVars := map[string]string{
		"Objective":     objective,
		"WorkspacePath": workspacePath,
		"Strategy":      strategy,
		"Focus":         focus,
		"Iteration":     templateVars["Iteration"],
		"MaxIterations": templateVars["MaxIterations"],
	}

	// Execute using input processor
	result, conversationHistory, err := tppa.ExecuteWithInputProcessor(ctx, planningTemplateVars, tppa.planningInputProcessor, conversationHistory)
	return result, conversationHistory, err
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

## 🤖 AGENT IDENTITY
- **Role**: Planning Agent
- **Responsibility**: Create incremental plan for THIS iteration only (1-3 steps)
- **Mode**: Strategic (plan approach, not execution details)

## 📁 FILE PERMISSIONS
**READ (if iteration > 1):**
- {{.WorkspacePath}}/todo_creation/planning/plan.md (previous iterations)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (what worked/failed)
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md (validated methods)

**WRITE:**
- **APPEND** to {{.WorkspacePath}}/todo_creation/planning/plan.md (add "## Iteration X")
- **CREATE** {{.WorkspacePath}}/todo_creation/planning/plan.md (if iteration == 1)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation/
- Never overwrite existing iterations - always APPEND
- Plan only 1-3 steps per iteration

## 🔍 YOUR ROLE IN ITERATIVE PLANNING

**Key Concepts:**
- **This Iteration**: Plan 1-3 executable steps for current focus ({{.Focus}})
- **Iteration Phases**: 1-3 (Discovery) → 4-6 (Refinement) → 7-10 (Completion)
- **Final Todo**: Writer synthesizes best methods from ALL iterations (not you)

## 📁 BEFORE PLANNING (If Iteration > 1)

**Read previous work to avoid repeating failures:**
1. Read {{.WorkspacePath}}/todo_creation/planning/plan.md - see all "## Iteration X" sections
2. Read {{.WorkspacePath}}/todo_creation/execution/execution_results.md - identify success rates
3. Read {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md - verify evidence

**If Iteration == 1:** Skip reading (files don't exist yet), start fresh exploration.

## 📋 PLANNING GUIDELINES
- **Small Scope**: Plan ONLY 1-3 steps executable in THIS iteration
- **Evidence-Based**: Use methods with highest success rates from previous iterations
- **Iteration-Aware**: Early iterations explore, later iterations refine/complete
- **Append Only**: Add "## Iteration X" section to plan.md (never overwrite)

**⚠️ IMPORTANT**: Only create/modify files within {{.WorkspacePath}}/todo_creation/ folder structure.

` + GetTodoCreationMemoryRequirements() + `

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

---

**Note**: Focus ONLY on THIS iteration. Do not plan future iterations - that's for the next planning cycle.`

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
