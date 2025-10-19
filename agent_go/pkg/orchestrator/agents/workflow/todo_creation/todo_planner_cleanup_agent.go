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

// TodoPlannerCleanupTemplate holds template variables for cleanup prompts
type TodoPlannerCleanupTemplate struct {
	WorkspacePath string
}

// TodoPlannerCleanupAgent manages workspace cleanup after planning
type TodoPlannerCleanupAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewTodoPlannerCleanupAgent creates a new todo planner cleanup agent
func NewTodoPlannerCleanupAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge orchestrator.EventBridge) *TodoPlannerCleanupAgent {
	baseAgent := agents.NewBaseOrchestratorAgentWithEventBridge(
		config,
		logger,
		tracer,
		agents.TodoPlannerCleanupAgentType,
		eventBridge,
	)

	return &TodoPlannerCleanupAgent{
		BaseOrchestratorAgent: baseAgent,
	}
}

// GetBaseAgent implements the OrchestratorAgent interface
func (tpca *TodoPlannerCleanupAgent) GetBaseAgent() *agents.BaseAgent {
	return tpca.BaseOrchestratorAgent.BaseAgent()
}

// Execute implements the OrchestratorAgent interface
func (tpca *TodoPlannerCleanupAgent) Execute(ctx context.Context, templateVars map[string]string, conversationHistory []llms.MessageContent) (string, error) {
	// Extract workspace path from template variables
	workspacePath := templateVars["WorkspacePath"]

	// Prepare template variables
	cleanupTemplateVars := map[string]string{
		"WorkspacePath": workspacePath,
	}

	// Execute using input processor
	return tpca.ExecuteWithInputProcessor(ctx, cleanupTemplateVars, tpca.cleanupInputProcessor, conversationHistory)
}

// cleanupInputProcessor processes inputs specifically for workspace cleanup
func (tpca *TodoPlannerCleanupAgent) cleanupInputProcessor(templateVars map[string]string) string {
	// Create template data
	templateData := TodoPlannerCleanupTemplate{
		WorkspacePath: templateVars["WorkspacePath"],
	}

	// Define the template
	templateStr := `## PRIMARY TASK - CLEAN UP WORKSPACE & SYNC TO GITHUB

**WORKSPACE**: {{.WorkspacePath}}

**CORE TASK**: Clean up the planning workspace, organize files for the execution phase, and sync all changes to GitHub.

## Cleanup Strategy
- **Check GitHub Status**: Use get_workspace_github_status to check current sync status
- **Review Planning Files**: Check what files were created
- **Move Final Todo**: Move the final todo.md from todo_creation/ to main workspace root
- **Archive Intermediate Files**: Store planning artifacts in archived folder
- **Prepare for Execution**: Set up workspace for execution phase
- **Sync to GitHub**: Use sync_workspace_to_github to commit and push all changes

## Critical File Handling
**FINAL TODO FILE**: 
- **Source**: {{.WorkspacePath}}/todo_creation/todo.md
- **Destination**: {{.WorkspacePath}}/todo_final.md
- **Action**: Use move_workspace_file to move the final todo list to main workspace root
- **Purpose**: Make todo list accessible for execution phase

**INTERMEDIATE FILES**:
- **Archive Location**: {{.WorkspacePath}}/todo_creation/cleanup/archived/
- **Files to Archive**: Planning drafts, execution logs, validation reports, critique reports
- **Purpose**: Preserve planning artifacts while keeping workspace clean

## Workspace Updates
Create/update files in {{.WorkspacePath}}/todo_creation/cleanup/:
- **cleanup_report.md**: Summary of cleanup actions and sync status
- **archived/**: Archived planning files (intermediate files only)

**⚠️ IMPORTANT**: 
- Move final todo.md to main workspace root as todo_final.md ({{.WorkspacePath}}/todo_final.md)
- Only archive intermediate/planning files, not the final todo list
- Only create, update, or modify files within {{.WorkspacePath}}/todo_creation/ folder structure for cleanup reports

## GitHub Sync Process
1. **Check Status**: Use get_workspace_github_status to see pending changes
2. **Perform Cleanup**: Organize and archive files as needed
3. **Sync Changes**: Use sync_workspace_to_github with commit message "Cleanup planning workspace - organized files for execution phase"
4. **Verify Sync**: Confirm all changes were successfully pushed to GitHub

` + memory.GetWorkflowMemoryRequirements() + `

## Output Format
# Cleanup Report

## Cleanup Summary
[What was cleaned up and organized]

## Final Todo File Movement
- **Source**: {{.WorkspacePath}}/todo_creation/todo.md
- **Destination**: {{.WorkspacePath}}/todo_final.md
- **Status**: [Confirmation that final todo was moved to main workspace root as todo_final.md]

## Files Preserved
[Important files that were kept in main workspace]

## Files Archived
[Intermediate files that were moved to archive folder]

## GitHub Sync Status
[Sync status before and after cleanup]

## Workspace Status
[Current state of the workspace - final todo accessible at root level as todo_final.md]

## Sync Confirmation
[Confirmation that all changes were synced to GitHub]

Focus on creating a clean, organized workspace ready for the execution phase with the final todo list accessible at the main workspace root as todo_final.md and ensuring all changes are properly synced to GitHub.`

	// Parse and execute the template
	tmpl, err := template.New("cleanup").Parse(templateStr)
	if err != nil {
		return fmt.Sprintf("Error parsing cleanup template: %v", err)
	}

	var result strings.Builder
	if err := tmpl.Execute(&result, templateData); err != nil {
		return fmt.Sprintf("Error executing cleanup template: %v", err)
	}

	return result.String()
}
