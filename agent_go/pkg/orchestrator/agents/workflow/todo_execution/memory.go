package todo_execution

import "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

// GetTodoExecutionMemoryRequirements returns memory requirements specific to todo execution workflow
func GetTodoExecutionMemoryRequirements() string {
	return `
## 📁 WORKSPACE STRUCTURE

` + "```" + `
{{.WorkspacePath}}/
├── validation_report.md    (validation results)
└── outputs/                (execution artifacts, if needed)
` + "```" + `

**Note**: WorkspacePath points to the run folder (e.g., workspace/runs/2025-01-27-iteration-1)

## 📁 FILE PERMISSIONS

### Execution Agent:
- **Write**: {{.WorkspacePath}}/outputs/* (only if step requires files)
- **Focus**: Execute using MCP tools, orchestrator captures results automatically

### Validation Agent:
- **Write**: {{.WorkspacePath}}/validation_report.md ONLY
- **Source**: Execution conversation history (may read files to verify artifacts)

## ⚠️ RESTRICTIONS

**Never modify**: Files outside {{.WorkspacePath}}/

**Always create**: validation_report.md (validation agent)

**Optional**: outputs/* (execution agent, only if step requires)

` + memory.GetWorkflowMemoryRequirements() // Include generic memory requirements
}
