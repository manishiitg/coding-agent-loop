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
├── learnings/
│   ├── success_patterns.md
│   ├── failure_analysis.md
│   └── step_X_learning.md
├── variables/
│   └── variables.json      (Variable definitions and values)
└── ../todo_final.md        (Final todo list - one level up from {{.WorkspacePath}})
` + "```" + `

### **Core Principles (All Agents)**
- **Relative Paths Only**: All paths relative to {{.WorkspacePath}}/
- **Workspace Boundaries**: Only read/write within designated workspace folders
- **File Discovery**: Use **list_workspace_files** to check file existence before reading
- **Graceful Handling**: Handle missing files appropriately
- **Context Sharing**: Share data between steps via workspace files

## 🔐 VARIABLE HANDLING (CRITICAL - ALL AGENTS)

**Variables** are placeholders like AWS_ACCOUNT_ID or GITHUB_REPO_URL (wrapped in double curly braces) that represent values changing across environments.

**RULES:**
1. **NEVER hard-code values** - Always preserve variable placeholders
2. **NEVER replace placeholders** - Keep them as-is when reading/writing files
3. **Execution agents see actual values** - Other agents only see placeholders

**Examples:**
- ✅ CORRECT: "Deploy to account AWS_ACCOUNT_ID" (placeholder preserved)
- ❌ WRONG: "Deploy to account 123456789" (hard-coded value)

**Why?** Plans must work across dev/staging/prod environments without modification

` + memory.GetWorkflowMemoryRequirements() // Include generic memory requirements
}
