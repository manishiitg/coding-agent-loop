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

// WorkspaceUpdateTemplate holds template variables for workspace update prompts
type WorkspaceUpdateTemplate struct {
	Objective        string
	WorkspacePath    string
	ExecutionOutput  string
	ValidationOutput string
}

// WorkspaceUpdateAgent extends BaseOrchestratorAgent with workspace update functionality
type WorkspaceUpdateAgent struct {
	*agents.BaseOrchestratorAgent // ✅ REUSE: All base functionality
}

// NewWorkspaceUpdateAgent creates a new workspace update agent
func NewWorkspaceUpdateAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) *WorkspaceUpdateAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.WorkspaceUpdateAgentType, // 🆕 NEW: Agent type
		eventBridge,
	)

	return &WorkspaceUpdateAgent{
		BaseOrchestratorAgent: baseAgent, // ✅ REUSE: All base functionality
	}
}

// Execute implements the OrchestratorAgent interface
func (wua *WorkspaceUpdateAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract required parameters
	objective, ok := templateVars["Objective"]
	if !ok {
		objective = "No objective provided"
	}

	workspacePath, ok := templateVars["WorkspacePath"]
	if !ok {
		workspacePath = "No workspace path provided"
	}

	executionOutput, ok := templateVars["ExecutionOutput"]
	if !ok {
		executionOutput = "No execution output provided"
	}

	validationOutput, ok := templateVars["ValidationOutput"]
	if !ok {
		validationOutput = "No validation output provided"
	}

	// Prepare template variables
	updateTemplateVars := map[string]string{
		"Objective":        objective,
		"WorkspacePath":    workspacePath,
		"ExecutionOutput":  executionOutput,
		"ValidationOutput": validationOutput,
	}

	// Execute using input processor
	return wua.ExecuteWithInputProcessor(ctx, updateTemplateVars, wua.workspaceUpdateInputProcessor, conversationHistory)
}

// workspaceUpdateInputProcessor processes inputs specifically for workspace updates
func (wua *WorkspaceUpdateAgent) workspaceUpdateInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := WorkspaceUpdateTemplate{
		Objective:        templateVars["Objective"],
		WorkspacePath:    templateVars["WorkspacePath"],
		ExecutionOutput:  templateVars["ExecutionOutput"],
		ValidationOutput: templateVars["ValidationOutput"],
	}

	// Define the template
	templateStr := `## PRIMARY TASK - UPDATE WORKSPACE

**OBJECTIVE**: {{.Objective}}
**WORKSPACE**: {{.WorkspacePath}}

**EXECUTION OUTPUT**:
{{.ExecutionOutput}}

**VALIDATION OUTPUT**:
{{.ValidationOutput}}

**IMPORTANT**: You must do the following:
1. **Read current todo_final.md snapshot** from the latest runs folder
2. **Analyze execution and validation outputs** to understand what was accomplished and validated
3. **Update todo_snapshot.md** to reflect current state based on execution/validation results
4. **Cleanup workspace files** and organize completed work
5. **Update progress tracking** files and document current state
6. **Sync workspace to GitHub** using sync_workspace_to_github tool for version control and backup

## File Context Instructions
- **WORKSPACE PATH**: {{.WorkspacePath}}
- **Use the correct folder**: Work with {{.WorkspacePath}} folder
- **Required**: Workspace path is provided to identify the specific folder
- **STRICT BOUNDARY**: ONLY work within the specified {{.WorkspacePath}} folder - do not access other folders

## ⚠️ CRITICAL RESTRICTIONS
- **ONLY UPDATE todo_snapshot.md**: Only update the todo_snapshot.md file in the runs/{date}/ folder
- **DO NOT UPDATE main todo_final.md**: The main {{.WorkspacePath}}/todo_final.md file is READ ONLY for this agent

` + memory.GetWorkflowMemoryRequirements() + `

## Todo Completion Analysis
- **Parse todo_snapshot**: Analyze the current todo_snapshot.md structure to identify all todo items
- **Check execution results**: Review what was accomplished based on execution output
- **Cross-reference validation**: Ensure execution claims align with validation results
- **Count progress**: Count total todos vs validated completed todos
- **Calculate completion percentage**: Determine overall completion rate based on validated work

### Completion Criteria
A todo is considered completed when:
- **Status field**: Shows "Status: Completed"
- **All success criteria**: All checkboxes are marked as [x]
- **Execution successful**: Execution output shows the todo was completed
- **Validation passed**: Validation output confirms the todo passed validation
- **Results align**: Execution claims match validation findings

### Evidence References
When marking a todo as completed, add evidence references:
- **Evidence files**: Link to specific evidence files in runs/{xxx}/evidence/
- **Artifacts**: Reference artifacts in runs/{xxx}/outputs/artifacts/
- **Data files**: Link to data files in runs/{xxx}/outputs/data/
- **Execution logs**: Reference execution logs in runs/{xxx}/logs/
- **Validation report**: Link to validation_report.md
- **Execution output**: Link to execution_output.md

## Workspace Management
- **Read todo_final.md snapshot**: From runs/{date}/todo_snapshot.md
- **Update snapshot**: Update todo_snapshot.md to reflect current state based on execution/validation results
- **Add evidence references**: When marking todos as completed, include evidence links
- **Cleanup workspace**: Remove temporary files, organize structure, archive old runs
- **Update progress**: Update progress tracking files and documentation
- **Sync workspace to GitHub**: Use sync_workspace_to_github tool to commit changes and push to repository

## Evidence Reference Format
When updating todo_snapshot.md, add evidence references in this format:

markdown
### Todo ID: todo_001
**Status**: ✅ Completed
**Evidence**:
- **Execution**: [runs/{xxx}/outputs/execution_output.md](runs/{xxx}/outputs/execution_output.md)
- **Validation**: [runs/{xxx}/outputs/validation_report.md](runs/{xxx}/outputs/validation_report.md)
- **Artifacts**: [runs/{xxx}/outputs/artifacts/](runs/{xxx}/outputs/artifacts/)
- **Data**: [runs/{xxx}/outputs/data/](runs/{xxx}/outputs/data/)
- **Logs**: [runs/{xxx}/logs/](runs/{xxx}/logs/)
- **Evidence**: [runs/{xxx}/evidence/](runs/{xxx}/evidence/)

**Success Criteria**:
- [x] Criterion 1: ✅ Verified in validation report
- [x] Criterion 2: ✅ Confirmed in execution output


## Output Format
Provide a detailed workspace update report:

# Workspace Update Report

## Todo Completion Status
- **Total Todos**: [Number of todos in the list]
- **Completed Todos**: [Number of completed todos based on execution output]
- **Validated Completed**: [Number of todos that passed validation]
- **Remaining Todos**: [Number of incomplete todos]
- **Completion Percentage**: [X%% complete based on validated work]
- **Overall Status**: [COMPLETED/IN PROGRESS/NEEDS ATTENTION]

## Completion Summary
**🎯 PROJECT STATUS**: [ALL TODOS COMPLETED / TODOS REMAINING]

### If ALL TODOS COMPLETED:
- **🎉 CONGRATULATIONS**: All todo items have been successfully completed!
- **Project Status**: COMPLETED
- **Next Actions**: Archive project, create summary, clean workspace

### If TODOS REMAINING:
- **Project Status**: IN PROGRESS
- **Remaining Count**: [X] todos still need completion
- **Next Actions**: Continue execution, focus on high-priority items

## Updates Made
- **Todo Snapshot**: [Status of todo_snapshot.md update]
- **Workspace Cleanup**: [Files removed, organized, archived]
- **Progress Tracking**: [Progress files updated]
- **Workspace Sync to GitHub**: [Success/Failed] - [Details of sync operation]

Focus on maintaining a clean, organized, and up-to-date workspace with proper todo tracking and completion analysis.`

	// Parse and execute the template
	tmpl, err := template.New("workspaceUpdate").Parse(templateStr)
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
