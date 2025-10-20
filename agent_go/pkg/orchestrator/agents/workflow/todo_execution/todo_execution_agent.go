package todo_execution

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
func NewTodoExecutionAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) *TodoExecutionAgent {
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

// todoExecutionInputProcessor processes inputs specifically for todo execution
func (tea *TodoExecutionAgent) todoExecutionInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := TodoExecutionTemplate{
		Objective:     templateVars["Objective"],
		WorkspacePath: templateVars["WorkspacePath"],
		RunOption:     templateVars["RunOption"],
	}

	// Define the template
	templateStr := `## PRIMARY TASK - EXECUTE AS MANY TODOS AS POSSIBLE

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}
**RUN OPTION**: {{.RunOption}}

**IMPORTANT**: You must do the following:
1. **Read the todo_final.md list** from {{.WorkspacePath}}/todo_final.md
2. **Identify all incomplete todos** to execute
3. **Execute as many todos as possible** using MCP tools in sequence
4. **DO NOT UPDATE the main todo_final.md file** - only update todo_snapshot.md in runs folder
5. **Report execution results** for all completed todos and next steps

## File Context Instructions
- **WORKSPACE PATH**: {{.WorkspacePath}}
- **Use the correct folder**: Read from {{.WorkspacePath}}/todo_final.md
- **Required**: Workspace path is provided to identify the specific folder
- **STRICT BOUNDARY**: ONLY work within the specified {{.WorkspacePath}} folder - do not access other folders

## ⚠️ CRITICAL RESTRICTIONS
- **DO NOT UPDATE main todo_final.md**: Never modify the main {{.WorkspacePath}}/todo_final.md file
- **ONLY UPDATE todo_snapshot.md**: Only update the todo_snapshot.md file in the runs folder
- **READ ONLY for main todo_final.md**: The main {{.WorkspacePath}}/todo_final.md file is READ ONLY for this agent
- **Preserve original**: The main {{.WorkspacePath}}/todo_final.md file must remain unchanged during execution

` + memory.GetWorkflowMemoryRequirements() + `

## Instructions
1. **Use workspace path**: {{.WorkspacePath}} to identify the correct folder
2. **Follow the specific instructions below** for your selected run option:

{{if eq .RunOption "use_same_run"}}
   **Use Same Run**: Check if any existing runs folder exists, use it if it does
   - If any runs folder exists: Continue using the most recent existing run folder
   - If no runs folder exists: Create a new runs folder with an appropriate name and start fresh
   - Always preserve and build upon previous execution data
   - **Runs Folder Structure**: Store outputs in the existing or new runs folder
   - **Example**: If workspace is "Workflow/MyProject", use "Workflow/MyProject/runs/existing-folder/" or create "Workflow/MyProject/runs/initial/"
   
   **Execution Steps:**
   1. **Create runs folder**: Use workspace tools to create the runs folder structure
   2. **Try to read existing snapshot**: First attempt to read todo_snapshot.md from the runs folder
   3. **If snapshot exists**: Use the existing todo_snapshot.md to continue from current state
   4. **If snapshot doesn't exist**: Read todo_final.md and copy it to todo_snapshot.md in the runs folder (new run)
   5. **Parse todo list**: Analyze the markdown structure to identify individual todos
   6. **Find all incomplete todos**: Look for all incomplete todos (check for "Status: Pending" or unchecked success criteria)
   7. **Execute todos in sequence**: Use MCP tools to complete as many todos as possible, updating todo_snapshot.md after each completion
   8. **Create output files**: Generate output files that can be consumed by validation agent
   9. **Document results**: Create evidence files in the runs folder and update progress tracking for all completed todos

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
   2. **Read original todo_final.md**: Get the complete todo list from {{.WorkspacePath}}/todo_final.md
   3. **Create snapshot**: Copy todo_final.md content to runs/{date}-{descriptive-name}/todo_snapshot.md
   4. **Parse todo list**: Analyze the markdown structure to identify individual todos
   5. **Find all incomplete todos**: Look for all incomplete todos (check for "Status: Pending" or unchecked success criteria)
   6. **Execute todos in sequence**: Use MCP tools to complete as many todos as possible, updating todo_snapshot.md after each completion
   7. **Create output files**: Generate output files that can be consumed by validation agent
   8. **Document results**: Create evidence files in the runs/{date}-{descriptive-name} folder and update progress tracking for all completed todos

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
   2. **Try to read existing snapshot**: First attempt to read todo_snapshot.md from runs/{date}-{descriptive-name}/ folder
   3. **If snapshot exists**: Use the existing todo_snapshot.md to continue from current state
   4. **If snapshot doesn't exist**: Read todo_final.md and copy it to todo_snapshot.md in runs/{date}-{descriptive-name}/ (new run)
   5. **Parse todo list**: Analyze the markdown structure to identify individual todos
   6. **Find all incomplete todos**: Look for all incomplete todos (check for "Status: Pending" or unchecked success criteria)
   7. **Execute todos in sequence**: Use MCP tools to complete as many todos as possible, updating todo_snapshot.md after each completion
   8. **Create output files**: Generate output files that can be consumed by validation agent
   9. **Document results**: Create evidence files in the runs/{date}-{descriptive-name} folder and update progress tracking for all completed todos

{{end}}
3. **Execute the workflow**: Follow the specific instructions for your selected run option above

## Output Files for Validation Agent
Create the following files in your runs folder for validation:

### execution_output.md
Main output file containing:
- **Todos executed**: Which todos were completed
- **Execution summary**: What was accomplished across all todos
- **Success criteria met**: Which criteria were satisfied for each todo
- **Files created/modified**: List of files changed
- **Data generated**: Any data or results produced
- **Evidence**: Links to evidence files for all completed todos
- **Validation notes**: Information for validation agent

### data/ folder
Store any data files created during execution:
- **CSV files**: Data exports, reports
- **JSON files**: Structured data
- **Text files**: Logs, outputs
- **Other formats**: As needed

### artifacts/ folder
Store any artifacts generated:
- **Images**: Screenshots, diagrams
- **Documents**: Generated reports
- **Code**: Any code files created
- **Configs**: Configuration files

## Runs Folder Structure
Create the following folder structure for execution outputs:

Workflow/[FolderName]/
├── todo.md (read from here, updated after execution)
├── runs/
│   └── {xxxx}/
│       ├── todo_snapshot.md (copy of todo.md at execution start)
│       ├── logs/ (execution logs)
│       ├── results/ (execution results)
│       ├── evidence/ (step evidence)
│       ├── outputs/ (files for validation agent)
│       │   ├── execution_output.md (main output file)
│       │   ├── data/ (any data files created)
│       │   └── artifacts/ (any artifacts generated)
│       └── execution_report.md (summary report)

## File Operations
- **Read**: Use read_workspace_file to read todo_final.md from Workflow/[FolderName]/
- **Copy**: Use update_workspace_file to create todo_snapshot.md in runs/{date}/
- **DO NOT UPDATE main todo_final.md**: Never modify the main todo_final.md file - only update todo_snapshot.md
- **Runs Folder**: Create runs/{date} folder for execution outputs
- **Evidence**: Create step execution files in Workflow/[FolderName]/runs/{date}/
- **Logs**: Store execution logs in Workflow/[FolderName]/runs/{date}/logs/
- **Results**: Store execution results in Workflow/[FolderName]/runs/{date}/results/
- **Outputs**: Create validation-ready files in Workflow/[FolderName]/runs/{date}/outputs/
- **Progress**: Update progress tracking files in the runs folder

## Output Format
Provide a detailed execution report:

# Todo Execution Report

## Execution Summary
- **Total Todos Attempted**: [Number of todos attempted]
- **Todos Completed**: [Number of todos successfully completed]
- **Todos Failed**: [Number of todos that failed]
- **Execution Time**: [Total time taken]

## Completed Todos
### Todo 1: [Todo ID]
- **Title**: [Todo title]
- **Description**: [Todo description]
- **Status**: [Completed/In Progress/Failed]
- **Execution Steps**: [Description of what was done]
- **Success Criteria**: [Which criteria were met]
- **Evidence**: [Links to evidence files]

### Todo 2: [Todo ID]
- **Title**: [Todo title]
- **Description**: [Todo description]
- **Status**: [Completed/In Progress/Failed]
- **Execution Steps**: [Description of what was done]
- **Success Criteria**: [Which criteria were met]
- **Evidence**: [Links to evidence files]

## Failed Todos (if any)
### Todo X: [Todo ID]
- **Title**: [Todo title]
- **Description**: [Todo description]
- **Status**: [Failed]
- **Error**: [What went wrong]
- **Next Steps**: [How to fix or retry]

## Overall Success Criteria Verification
- [x] Criterion 1: [Status and evidence across all todos]
- [x] Criterion 2: [Status and evidence across all todos]

## Evidence Files
- **Runs Folder**: Workflow/[FolderName]/runs/{run-folder}/
- **Todo Snapshot**: Workflow/[FolderName]/runs/{run-folder}/todo_snapshot.md
- **Execution Logs**: Workflow/[FolderName]/runs/{run-folder}/logs/
- **Results**: Workflow/[FolderName]/runs/{run-folder}/results/
- **Evidence**: Workflow/[FolderName]/runs/{run-folder}/evidence/
- **Outputs for Validation**: Workflow/[FolderName]/runs/{run-folder}/outputs/
  - **Main Output**: execution_output.md
  - **Data Files**: data/ folder
  - **Artifacts**: artifacts/ folder
- [List of specific files created or modified]

## Next Steps
- [What should be done next]
- [Remaining todos to execute if any]
- [Runs folder location for future reference]

Focus on executing as many incomplete todos as possible effectively and providing comprehensive results.`

	// Parse and execute the template
	tmpl, err := template.New("todoExecution").Parse(templateStr)
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

// Execute implements the OrchestratorAgent interface
func (tea *TodoExecutionAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Execute using input processor
	return tea.ExecuteWithInputProcessor(ctx, templateVars, tea.todoExecutionInputProcessor, conversationHistory)
}
