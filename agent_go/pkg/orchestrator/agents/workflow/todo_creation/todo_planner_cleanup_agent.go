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

// TodoPlannerCleanupTemplate holds template variables for cleanup prompts
type TodoPlannerCleanupTemplate struct {
	WorkspacePath string
}

// TodoPlannerCleanupAgent manages workspace cleanup after planning
type TodoPlannerCleanupAgent struct {
	*agents.BaseOrchestratorAgent
}

// NewTodoPlannerCleanupAgent creates a new todo planner cleanup agent
func NewTodoPlannerCleanupAgent(config *agents.OrchestratorAgentConfig, logger utils.ExtendedLogger, tracer observability.Tracer, eventBridge mcpagent.AgentEventListener) *TodoPlannerCleanupAgent {
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
	templateStr := `## PRIMARY TASK - CLEAN UP WORKSPACE

**WORKSPACE**: {{.WorkspacePath}}

## 🤖 AGENT IDENTITY
- **Role**: Cleanup Agent
- **Responsibility**: Organize workspace, move final todo, archive intermediate files
- **Mode**: Tactical (file operations, optional GitHub sync)

## 📁 FILE PERMISSIONS
**READ:**
- {{.WorkspacePath}}/todo_creation/ (all files to decide what to archive)

**WRITE:**
- **MOVE** {{.WorkspacePath}}/todo_creation/todo.md → {{.WorkspacePath}}/todo_final.md
- **CREATE** {{.WorkspacePath}}/todo_creation/cleanup/archived/ (archive folder)
- **MOVE** intermediate files to archived/
- **CREATE** {{.WorkspacePath}}/todo_creation/cleanup/cleanup_report.md

**GITHUB (OPTIONAL):**
- Use get_workspace_github_status (if available)
- Use sync_workspace_to_github (if available and status shows changes)

**RESTRICTIONS:**
- DO NOT archive todo.md or todo_final.md (these are production files)
- Only archive intermediate planning files

## 🔧 CLEANUP STEPS

**1. Move Final Todo to Main Workspace:**
- Source: {{.WorkspacePath}}/todo_creation/todo.md
- Destination: {{.WorkspacePath}}/todo_final.md
- Tool: move_workspace_file

**2. Archive Intermediate Files (Pattern-Based):**
Archive these patterns to {{.WorkspacePath}}/todo_creation/cleanup/archived/:
- planning/*.md (all planning iterations)
- execution/*.md (execution results, completed_steps)
- validation/*.md (validation reports)
- execution/evidence/* (evidence files)
- iteration_analysis.md (writer's comparison)

**3. Optional GitHub Sync:**
IF get_workspace_github_status tool exists:
  - Check status
  - IF changes detected: sync_workspace_to_github with message "Cleanup planning workspace"
  - ELSE: skip sync

IF tool doesn't exist: Skip GitHub sync (report "GitHub tools not available")

` + GetTodoCreationMemoryRequirements() + `

## Output Format
# Cleanup Report

## Actions Performed

### 1. Final Todo Movement
- **Source**: {{.WorkspacePath}}/todo_creation/todo.md
- **Destination**: {{.WorkspacePath}}/todo_final.md
- **Status**: [MOVED/FAILED]

### 2. Files Archived
[List files moved to {{.WorkspacePath}}/todo_creation/cleanup/archived/]
- planning/plan.md → archived/
- execution/execution_results.md → archived/
- [etc...]

### 3. GitHub Sync (Optional)
- **Tool Available**: [Yes/No]
- **Changes Detected**: [Yes/No]
- **Sync Status**: [SYNCED/SKIPPED/FAILED]
- **Commit Message**: [If synced]

## Summary
- **Todo Final Location**: {{.WorkspacePath}}/todo_final.md
- **Archived Files**: [Count]
- **Workspace Status**: [Ready for execution]

---

Focus on file organization and optional GitHub sync.`

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
