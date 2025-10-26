package todo_creation_human

import "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

// GetTodoCreationHumanMemoryRequirements returns SHARED memory requirements for all human-controlled todo creation agents
func GetTodoCreationHumanMemoryRequirements() string {
	return `
## 📁 TODO CREATION WORKSPACE STRUCTURE

### **Directory Structure**
` + "```" + `
{{.WorkspacePath}}/
├── todo_creation_human/              (Planning workspace - temporary)
│   ├── planning/
│   │   └── plan.md             (Execution plan)
│   ├── validation/
│   │   ├── step_1_validation_report.md
│   │   ├── step_2_validation_report.md
│   │   └── step_N_validation_report.md
│   └── learnings/
│       ├── success_patterns.md
│       ├── failure_analysis.md
│       └── step_X_learning.md
└── todo_final.md               (Final todo list - workspace root)
` + "```" + `

### **Core Principles (All Agents)**
- **Relative Paths Only**: All paths relative to {{.WorkspacePath}}/todo_creation_human/
- **Workspace Boundaries**: Only read/write within designated workspace folders
- **File Discovery**: Use **list_workspace_files** to check file existence before reading
- **Graceful Handling**: Handle missing files appropriately
- **Context Sharing**: Share data between steps via workspace files

` + memory.GetWorkflowMemoryRequirements() // Include generic memory requirements
}
