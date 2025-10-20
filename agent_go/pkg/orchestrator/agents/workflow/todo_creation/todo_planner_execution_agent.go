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

// TodoPlannerExecutionTemplate holds template variables for execution prompts
type TodoPlannerExecutionTemplate struct {
	Plan          string
	WorkspacePath string
}

// TodoPlannerExecutionAgent executes the objective using MCP servers to understand requirements
type TodoPlannerExecutionAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewTodoPlannerExecutionAgent creates a new todo planner execution agent
func NewTodoPlannerExecutionAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoPlannerExecutionAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerExecutionAgentType,
		eventBridge,
	)

	return &TodoPlannerExecutionAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// Execute implements the OrchestratorAgent interface
func (tpea *TodoPlannerExecutionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract plan and workspace path from template variables
	// Execution agent is tactical - just executes the plan without strategic context
	plan := templateVars["Plan"]
	workspacePath := templateVars["WorkspacePath"]

	// Prepare template variables
	executionTemplateVars := map[string]string{
		"Plan":          plan,
		"WorkspacePath": workspacePath,
	}

	// Execute using input processor
	return tpea.ExecuteWithInputProcessor(ctx, executionTemplateVars, tpea.executionInputProcessor, conversationHistory)
}

// executionInputProcessor processes inputs specifically for plan execution
func (tpea *TodoPlannerExecutionAgent) executionInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := TodoPlannerExecutionTemplate{
		Plan:          templateVars["Plan"],
		WorkspacePath: templateVars["WorkspacePath"],
	}

	// Define the template
	templateStr := `## 🎯 PRIMARY TASK - EXECUTE THE PLAN

**PLAN**: {{.Plan}}
**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Execution Agent
- **Responsibility**: Execute plan steps using MCP tools, record results
- **Mode**: Tactical (execute only, don't strategize or plan ahead)

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/todo_creation/planning/plan.md (current iteration plan)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (previous results)
- {{.WorkspacePath}}/todo_creation/execution/completed_steps.md (SKIP completed steps)

**WRITE:**
- **APPEND** to {{.WorkspacePath}}/todo_creation/execution/execution_results.md
- **UPDATE** {{.WorkspacePath}}/todo_creation/execution/completed_steps.md (add newly completed)
- **CREATE** files in {{.WorkspacePath}}/todo_creation/execution/evidence/ (critical steps only)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation/
- SKIP any steps already in completed_steps.md
- Evidence ONLY for: quantitative claims, assumptions, failures, decisions

## 🔍 BEFORE EXECUTING

**CRITICAL - Check Completed Steps:**
1. Read {{.WorkspacePath}}/todo_creation/execution/completed_steps.md
2. SKIP any steps marked as COMPLETED
3. Execute ONLY steps marked as PENDING or not yet attempted

**If completed_steps.md doesn't exist:** All steps are pending, execute all.

## 📋 EXECUTION GUIDELINES
- **Use exact MCP tools/arguments** from the plan
- **Record everything**: What worked, what failed, tool outputs
- **Evidence for critical steps only**: Quantitative claims, assumptions, failures, decisions
- **Append results**: Never overwrite previous iterations

` + GetTodoCreationMemoryRequirements() + `

## 📤 Output Format

**APPEND to** {{.WorkspacePath}}/todo_creation/execution/execution_results.md

---

## 📋 Execution Results
**Date**: [Current date/time]
**Planned Steps**: [Number from plan]
**Actual Executed**: [Number actually executed]

### Previous Execution Summary
[Briefly summarize from execution_results.md if it exists]
- Previous run: [What was tried, what worked, what failed]
- Key learnings: [Important discoveries]

### Current Execution

#### Step 1: [Step Name]
- **Status**: [COMPLETED/FAILED/PARTIAL]
- **MCP Server**: [Server used - e.g., aws, gitlab, github]
- **MCP Tool**: [Exact tool name - e.g., aws_cli_query]
- **Tool Arguments**: [Exact arguments passed]
- **Tool Call Result**: [What the tool returned]
- **Tool Call Duration**: [Execution time]
- **Success Criteria Met**: [Yes/No with details]
- **What Worked**: [Specific successes]
- **What Failed**: [Specific failures]
- **Evidence Files**: [Only for critical steps - magic numbers, assumptions, failures]

#### Step 2: [Step Name - if executed]
- **Status**: [COMPLETED/FAILED/PARTIAL]
- **MCP Server**: [Server used]
- **MCP Tool**: [Tool name]
- **Tool Arguments**: [Arguments]
- **Result**: [What happened]
- **Success**: [Yes/No]

### Iteration Learning Summary
- **Key Discovery**: [Main learning from this iteration]
- **What Worked Best**: [Successful approach/method]
- **What Failed**: [Failed approach/method]
- **For Next Iteration**: [What to try next based on learnings]

### Cumulative Progress
- **Total Steps Attempted So Far**: [Across all iterations]
- **Total Steps Completed**: [Successful steps]
- **Success Rate**: [% successful]
- **Best Methods Found**: [List proven MCP tools/approaches]

---

## 📜 Previously Completed Steps (Preserve These)
[Steps completed in previous iterations:]

- **Step Name**: [Name of completed step]
- **Status**: [COMPLETED - Keep results]
- **MCP Server**: [Server that was used]
- **MCP Tool**: [Tool that worked]
- **Tool Arguments**: [Arguments that succeeded]
- **Tool Call Result**: [What was produced]
- **Execution Method**: [Method that worked]
- **Evidence Files**: [Reference to evidence]
- **Action**: [PRESERVE - Keep completed results]

## ⏳ Steps Still Pending
[Steps waiting to be executed:]

- **Step Name**: [Name of step]
- **Status**: [PENDING - Not executed]
- **Reason**: [Why not executed this iteration]
- **Dependencies**: [Prerequisites needed]
- **Next Priority**: [HIGH/MEDIUM/LOW]

## 📁 Evidence Files
[Only for critical steps - list file paths in evidence/ folder]

## 💡 Key Insights
[Important discoveries from THIS iteration's execution]

---

**Note**: Focus on tactical execution. Report what worked/failed without suggesting strategic improvements (that's planning agent's job).`

	// Parse and execute the template
	tmpl, err := template.New("execution").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing execution template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing execution template: %v", err)
	}

	return result.String()
}
