package todo_creation_human

import "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

// GetTodoCreationHumanMemoryRequirements returns SHARED memory requirements for all human-controlled todo creation agents
func GetTodoCreationHumanMemoryRequirements() string {
	return `
## 📁 TODO CREATION WORKSPACE STRUCTURE

### **Directory Structure**
` + "```" + `
{{.WorkspacePath}}/
├── planning/
│   └── plan.json           (Structured plan JSON - primary execution plan)
├── execution/              (Execution agent outputs - temporary)
│   └── step_X_results.md   (Context output files from execution)
├── learnings/              (Normal mode learnings)
│   ├── success_patterns.md
│   ├── failure_analysis.md
│   ├── step_X_learning.md
│   └── scripts/            (Python scripts)
├── learning_code_exec/      (Code execution mode learnings)
│   ├── step_X_learning.md
│   └── code/                (Go code patterns)
├── variables/
│   └── variables.json      (Variable definitions and values)
└── ../todo_final.md        (Final todo list - one level up from {{.WorkspacePath}})
` + "```" + `
` + memory.GetWorkflowMemoryRequirements() // Include generic memory requirements
}
