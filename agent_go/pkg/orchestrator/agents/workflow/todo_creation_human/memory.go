package todo_creation_human

import "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

// GetTodoCreationHumanMemoryRequirements returns memory requirements specific to human-controlled todo creation workflow
func GetTodoCreationHumanMemoryRequirements() string {
	return `
## 📁 TODO CREATION WORKSPACE STRUCTURE

### **Directory Structure**
` + "```" + `
{{.WorkspacePath}}/
├── todo_creation_human/              (Planning workspace - temporary)
│   ├── planning/
│   │   └── plan.json           (CREATE: Single execution plan)
│   ├── validation/
│   │   ├── step_1_validation_report.md     (CREATE: Step 1 validation report)
│   │   ├── step_2_validation_report.md     (CREATE: Step 2 validation report)
│   │   └── step_N_validation_report.md     (CREATE: Step N validation report)
│   └── learnings/
│       ├── success_patterns.md             (CREATE: Success learning reports)
│       ├── failure_analysis.md             (CREATE: Failure learning reports)
│       └── step_X_learning.md              (CREATE: Per-step learning details)
└── todo_final.md               (CREATE: Final todo list)
` + "```" + `

### **📁 SIMPLIFIED FILE PERMISSIONS**

**Each agent owns its phase folder:**

| Agent | Read From | Write To |
|-------|-----------|----------|
| **Planning** | - | Returns JSON (no files) |
| **Plan Writer** | - | planning/plan.json |
| **Execution** | planning/plan.json | Nothing (read-only, returns results) |
| **Validation** | planning/plan.json | validation/step_X_validation_report.md |
| **Success Learning** | planning/, validation/ | planning/plan.json, learnings/ |
| **Failure Learning** | planning/, validation/ | learnings/ |
| **Writer** | planning/, validation/, learnings/ | todo_final.md |

**Core Principles:**
- All paths relative to {{.WorkspacePath}}/todo_creation_human/
- Read context from previous phases as needed
- Write only to your designated phase folder
- Use workspace tools to check file existence before reading

---

### **🔍 File Discovery Requirements**
All agents should:
1. Use **list_workspace_files** to check if files exist before reading
2. Handle missing files gracefully (single execution mode)
3. Create new files for each run (no iteration complexity)

### **📝 Evidence Collection Rules**
**Collect evidence ONLY for:**
- Quantitative claims (numbers, metrics, percentages)
- Assumptions about system behavior
- Failed attempts (to understand what went wrong)
- Decision points (choosing between alternatives)

**NO evidence needed for:**
- Simple file operations (create, read, list)
- Obvious success indicators (file exists, command succeeded)

### **🚀 Simplified Workflow**
1. **Planning**: Generate structured plan directly (returns JSON, no file writing)
2. **Human Approval**: Human reviews and approves/rejects the structured plan
3. **Plan Writing**: Write approved plan to workspace files (plan.json)
4. **Step-by-Step Execution**: Execute each step using MCP tools and return results in response
5. **Step-by-Step Validation**: Validate each step execution result and create validation report
6. **Writing**: Create final todo list based on validation reports (reads from workspace)
7. **Complete**: Ready for human review and execution phase

### **🔍 Workspace-Only Reading Pattern**
All agents read from workspace files independently - no direct output passing between agents:
- **Planning Agent**: Generates structured JSON directly, no file writing
- **Plan Writer Agent**: Receives approved structured plan data, writes to workspace files
- **Execution Agent**: Reads plan.json, executes step using MCP tools, returns results in response
- **Validation Agent**: Validates step execution output directly (receives step title, description, and execution output as input)
- **Writer Agent**: Reads plan.json and step_*_validation_report.md to create final todo list

` + memory.GetWorkflowMemoryRequirements() // Include generic memory requirements
}
