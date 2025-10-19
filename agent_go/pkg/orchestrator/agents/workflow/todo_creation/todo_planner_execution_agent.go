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
func NewTodoPlannerExecutionAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) *TodoPlannerExecutionAgent {
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

// GetBaseAgent implements the OrchestratorAgent interface
func (tpea *TodoPlannerExecutionAgent) GetBaseAgent() *agents.BaseAgent {
	return tpea.BaseOrchestratorAgent.BaseAgent()
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

**CORE TASK**: Execute the steps in the provided plan. You are the tactical execution agent - just execute what the planner designed.

**⚠️ IMPORTANT**: Execute ONLY the steps in the plan. Don't interpret, expand, or strategize - just execute what's specified.

## 🔍 Execution Mission

**IMPORTANT - Your Role**:
- 📝 **Plan** = What the planner designed (small, executable steps)
- 🧪 **Your Job** = Execute the plan steps using MCP tools
- 📊 **Goal** = Record what worked and what failed
- 🔄 **Memory** = Append results to workspace files

**Execution Approach**:
- Execute the plan as specified
- Use the exact MCP tools/arguments planned
- Record detailed results (successes and failures)
- Save evidence only for critical steps
- Append results to existing files (preserve history)

## 📋 Execution Guidelines
- **Read Previous Work**: Check {{.WorkspacePath}}/todo_creation/execution/execution_results.md for context
- **Execute Plan Steps**: All steps in the provided plan
- **Record Everything**: MCP servers/tools/arguments that worked or failed
- **Selective Evidence**: Save evidence only for critical steps (magic numbers, assumptions, failures, decisions)
- **Append Results**: Add your results to execution/execution_results.md
- **Stay Focused**: Execute ONLY what's in the plan
- **Build on Memory**: Learn from previous execution results

## 💾 Workspace Updates
Create/update files in {{.WorkspacePath}}/todo_creation/execution/:
- **execution_results.md**: APPEND results (preserve previous iterations)
- **completed_steps.md**: Track completed steps
- **evidence/**: Evidence files for critical steps only

**⚠️ CRITICAL**: 
1. **PRESERVE PREVIOUS WORK**: Read existing files first, keep completed steps
2. **APPEND ONLY**: Add new results, never overwrite existing completed steps
3. **EXECUTE ALL STEPS**: Try to execute ALL plan steps in one iteration
4. **FILE SCOPE**: Only modify files within {{.WorkspacePath}}/todo_creation/

` + memory.GetWorkflowMemoryRequirements() + `

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
[Files created or outputs generated during execution]

## 💡 Key Insights
[Important discoveries about execution strategies, tool selection, and best practices]

## ❌ Failed Steps (Learning Opportunities)
[Steps that didn't work and why:]

- **Step**: [Step name]
- **Failure Reason**: [Why it failed]
- **Lessons Learned**: [Insights from failure]
- **Better Approaches**: [What might work better]

## ✨ Successful Patterns
[Reusable patterns discovered:]

- **Pattern Name**: [Name of pattern]
- **Description**: [What it does]
- **When to Use**: [Applicable scenarios]
- **Implementation**: [How to implement]
- **Success Rate**: [How often it works]

## 📝 Evidence Requirements
**Evidence Required For:**
- **Magic Numbers**: Steps with specific numbers, metrics, quantitative claims
- **Assumptions**: Steps where assumptions are made
- **Failures**: Failed steps needing analysis
- **Decisions**: Steps choosing between alternatives

**No Evidence Required For:**
- **Routine Steps**: Simple, straightforward completions
- **Standard Operations**: Common file/command operations
- **Clear Success**: Steps with obvious success indicators

Focus on executing ALL steps efficiently while collecting evidence only for critical decision points.`

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
