package todo_execution

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

// TodoExecutionTemplate holds template variables for todo execution prompts
type TodoExecutionTemplate struct {
	Objective     string // The workflow objective
	WorkspacePath string // The workspace path extracted from objective
	RunOption     string // Selected run option: use_same_run, create_new_runs_always, create_new_run_once_daily
}

// TodoExecutionAgent extends BaseOrchestratorAgent with todo execution functionality
type TodoExecutionAgent struct {
	*agents.BaseOrchestratorAgent // ✅ REUSE: All base functionality
}

// NewTodoExecutionAgent creates a new todo execution agent
func NewTodoExecutionAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoExecutionAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoExecutionAgentType, // 🆕 NEW: Agent type
		eventBridge,
	)

	return &TodoExecutionAgent{
		BaseOrchestratorAgent: baseAgent, // ✅ REUSE: All base functionality
	}
}

// todoExecutionInputProcessor processes inputs specifically for single step execution
func (tea *TodoExecutionAgent) todoExecutionInputProcessor(templateVars map[string]string) string {

	// Define the template for single step execution
	templateStr := `## PRIMARY TASK - EXECUTE SINGLE STEP

**STEP**: {{.StepNumber}}/{{.TotalSteps}}
**TITLE**: {{.StepTitle}}
**OBJECTIVE**: {{.StepDescription}}

## STEP DETAILS

**Why This Step:**
{{.StepWhyThisStep}}

**Success Criteria:**
{{.StepSuccessCriteria}}

**Context Dependencies:**
{{.StepContextDependencies}}

**Context Output to Produce:**
{{.StepContextOutput}}

## PROVEN APPROACHES (Follow These)

**Success Patterns (What Worked):**
{{.StepSuccessPatterns}}

**Failure Patterns (Avoid These):**
{{.StepFailurePatterns}}

{{if .PreviousFeedback}}
## 🔄 PREVIOUS FEEDBACK
**Previous Validation Feedback**: {{.PreviousFeedback}}

**IMPORTANT**: Use this feedback to improve your execution. Address any issues mentioned and follow the recommendations provided.
{{end}}

## 🤖 AGENT IDENTITY
- **Role**: Todo Execution Agent
- **Responsibility**: Execute ONE specific step using MCP tools and proven approaches
- **Mode**: Tactical (execute this step, do not strategize)

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/todo_final.md (READ-ONLY)

**WRITE:**
- {{.WorkspacePath}}/runs/{selected}/execution_output.md (simple execution summary)
- {{.WorkspacePath}}/runs/{selected}/outputs/** (any files created during execution)

**RESTRICTIONS:**
- Never modify {{.WorkspacePath}}/todo_final.md
- Only work within {{.WorkspacePath}}/
- Focus on execution, not evidence collection

## EXECUTION STRATEGY
1. **Check Context Dependencies**: Ensure prerequisites are satisfied before starting
2. **Follow Success Patterns Exactly**: These are validated approaches that worked before
3. **Avoid All Failure Patterns**: These approaches have failed and should not be used
4. **Execute the Step**: Use proven tools and approaches from Success Patterns
5. **Produce Context Output**: Ensure this step produces what subsequent steps need
6. **Verify Success Criteria**: Confirm all criteria are met before completion

## File Context Instructions
- **WORKSPACE PATH**: {{.WorkspacePath}}
- **Use the correct folder**: Work within {{.WorkspacePath}}/
- **Required**: Workspace path is provided to identify the specific folder
- **STRICT BOUNDARY**: ONLY work within the specified {{.WorkspacePath}} folder

## ⚠️ CRITICAL RESTRICTIONS
- **DO NOT UPDATE main todo_final.md**: Never modify the main {{.WorkspacePath}}/todo_final.md file
- **READ ONLY for main todo_final.md**: The main {{.WorkspacePath}}/todo_final.md file is READ ONLY for this agent
- **Preserve original**: The main {{.WorkspacePath}}/todo_final.md file must remain unchanged during execution

` + GetTodoExecutionMemoryRequirements() + `

## Instructions
1. **Use workspace path**: {{.WorkspacePath}} to identify the correct folder
2. **Follow the specific instructions below** for your selected run option:

{{if eq .RunOption "use_same_run"}}
   **Use Same Run**: Check if any existing runs folder exists, use it if it does
   - If any runs folder exists: Continue using the most recent existing run folder
   - If no runs folder exists: Create a new runs folder with an appropriate name and start fresh
   - **Runs Folder Structure**: Store outputs in the existing or new runs folder
   - **Example**: If workspace is "Workflow/MyProject", use "Workflow/MyProject/runs/existing-folder/" or create "Workflow/MyProject/runs/initial/"
   
   **Execution Steps:**
   1. **Create runs folder**: Use workspace tools to create the runs folder structure
   2. **Execute this specific step**: Use MCP tools to complete this step, following Success Patterns exactly
   3. **Create simple output**: Write basic execution summary to execution_output.md

{{else if eq .RunOption "create_new_runs_always"}}
   **Create New Runs Always**: Always create a new runs/{date}-{descriptive-name}/ folder
   - Get current date: Use system tools or current date to get YYYY-MM-DD format
   - Always create a new runs/{date}-{descriptive-name}/ folder (use descriptive name like "iteration-1", "iteration-2")
   - Start with a clean slate, don't use previous execution data
   - Isolate this execution from previous runs
   - **Runs Folder Structure**: Store outputs in the new runs/{date}-{descriptive-name}/ folder
   - **Example**: If workspace is "Workflow/MyProject", create "Workflow/MyProject/runs/2025-01-27-iteration-1/"
   
   **Execution Steps:**
   1. **Create runs folder**: Use workspace tools to create "runs/{date}-{descriptive-name}" folder structure
   2. **Execute this specific step**: Use MCP tools to complete this step, following Success Patterns exactly
   3. **Create simple output**: Write basic execution summary to execution_output.md

{{else if eq .RunOption "create_new_run_once_daily"}}
   **Create New Run Once Daily**: Create new run folder only once per day
   - Get current date: Use system tools or current date to get YYYY-MM-DD format
   - Check if runs/{date}-{descriptive-name}/ exists, create only if it doesn't exist
   - First execution today: Create new runs/{date}-{descriptive-name}/ folder
   - Subsequent executions today: Use existing runs/{date}-{descriptive-name}/ folder
   - **Runs Folder Structure**: Store outputs in the runs/{date}-{descriptive-name}/ folder
   - **Example**: If workspace is "Workflow/MyProject", create "Workflow/MyProject/runs/2025-01-27-initial/"
   
   **Execution Steps:**
   1. **Create runs folder**: Use workspace tools to create "runs/{date}-{descriptive-name}" folder structure (only if it doesn't exist)
   2. **Execute this specific step**: Use MCP tools to complete this step, following Success Patterns exactly
   3. **Create simple output**: Write basic execution summary to execution_output.md

{{end}}
3. **Execute this specific step**: Follow the specific instructions for your selected run option above

## Simple Output Format
Provide a basic execution summary for this specific step:

# Step Execution Summary

## Step Details
- **Step Number**: {{.StepNumber}}/{{.TotalSteps}}
- **Step Title**: {{.StepTitle}}
- **Status**: [Completed/In Progress/Failed]

## What Was Done
- **Approach**: [Brief description of what was executed]
- **Tools Used**: [MCP tools used for this step]
- **Success Criteria Status**: [Which criteria were met]

## Files Created/Modified
- [List any files created or modified during execution]

Focus on executing this step effectively using proven approaches and avoiding failed patterns.`

	// Parse and execute the template
	tmpl, err := template.New("todoExecution").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing template: %v", err)
	}

	var result strings.Builder
	err = tmpl.Execute(&result, map[string]interface{}{
		"StepNumber":              templateVars["StepNumber"],
		"TotalSteps":              templateVars["TotalSteps"],
		"StepTitle":               templateVars["StepTitle"],
		"StepDescription":         templateVars["StepDescription"],
		"StepWhyThisStep":         templateVars["StepWhyThisStep"],
		"StepSuccessCriteria":     templateVars["StepSuccessCriteria"],
		"StepContextDependencies": templateVars["StepContextDependencies"],
		"StepContextOutput":       templateVars["StepContextOutput"],
		"StepSuccessPatterns":     templateVars["StepSuccessPatterns"],
		"StepFailurePatterns":     templateVars["StepFailurePatterns"],
		"PreviousFeedback":        templateVars["PreviousFeedback"],
		"WorkspacePath":           templateVars["WorkspacePath"],
		"RunOption":               templateVars["RunOption"],
	})
	if err != nil {
		return fmt.Sprintf("Error executing template: %v", err)
	}

	return result.String()
}

// Execute processes the todo execution request using the input processor
func (tea *TodoExecutionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, []llms.MessageContent, error) {
	// Use the base orchestrator agent's Execute method with our custom input processor
	return tea.BaseOrchestratorAgent.ExecuteWithInputProcessor(ctx, templateVars, tea.todoExecutionInputProcessor, conversationHistory)
}
