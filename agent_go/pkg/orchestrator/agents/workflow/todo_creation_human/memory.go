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
│   │   └── plan.md            (CREATE: Single execution plan)
│   ├── execution/
│   │   ├── step_1_execution_results.md    (CREATE: Step 1 execution results)
│   │   ├── step_2_execution_results.md    (CREATE: Step 2 execution results)
│   │   ├── step_N_execution_results.md    (CREATE: Step N execution results)
│   │   ├── completed_steps.md        (CREATE: Track completed steps)
│   │   └── evidence/                 (CREATE: Evidence files for critical steps)
│   └── validation/
│       ├── step_1_validation_report.md     (CREATE: Step 1 validation report)
│       ├── step_2_validation_report.md     (CREATE: Step 2 validation report)
│       └── step_N_validation_report.md     (CREATE: Step N validation report)
└── todo_final.md               (CREATE: Final todo list)
` + "```" + `

### **📁 FILE PERMISSIONS BY AGENT**

#### Planning Agent:
**READ:**
- {{.WorkspacePath}}/todo_creation_human/planning/plan.md (if exists from previous runs)

**WRITE:**
- **CREATE** {{.WorkspacePath}}/todo_creation_human/planning/plan.md (single execution plan)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation_human/
- Single execution mode - create new plan each time
- Focus on direct, actionable steps
- **No input from other agents** - works independently

---

#### Plan Breakdown Agent:
**READ:**
- {{.WorkspacePath}}/todo_creation_human/planning/plan.md (plan to analyze)

**RESTRICTIONS:**
- Focus on extracting steps that can be executed independently
- Analyze dependencies between steps
- Return structured JSON response only (no file creation)
- **No input from other agents** - reads from workspace only

---

#### Execution Agent:
**READ:**
- {{.WorkspacePath}}/todo_creation_human/planning/plan.md (current plan)
- {{.WorkspacePath}}/todo_creation_human/execution/completed_steps.md (if exists)

**WRITE:**
- **CREATE** {{.WorkspacePath}}/todo_creation_human/execution/step_X_execution_results.md (where X is the step number)
- **CREATE** {{.WorkspacePath}}/todo_creation_human/execution/completed_steps.md
- **CREATE** files in {{.WorkspacePath}}/todo_creation_human/execution/evidence/ (only for critical steps)
- **CREATE/UPDATE** any files needed to complete the step

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation_human/
- Single execution - no iteration complexity
- Evidence ONLY for: quantitative claims, assumptions, failures, decisions
- **IMPORTANT**: Must clearly list all files created/updated in step-specific execution results
- **Receives step title and description** - knows exactly what step to execute
- **No input from other agents** - reads from workspace only

---

#### Validation Agent:
**READ:**
- {{.WorkspacePath}}/todo_creation_human/planning/plan.md (original plan)
- {{.WorkspacePath}}/todo_creation_human/execution/step_X_execution_results.md (step execution results, where X is the step number)
- {{.WorkspacePath}}/todo_creation_human/execution/completed_steps.md (completed steps)
- **VERIFY** files mentioned in step execution output (file existence and contents)

**WRITE:**
- **CREATE** {{.WorkspacePath}}/todo_creation_human/validation/step_X_validation_report.md (step validation report, where X is the step number)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation_human/
- Focus on step file-based validation - verify files claimed by execution agent
- Simple validation approach for human-controlled mode
- **Receives step title, description, and execution output** - validates specific step execution

---

#### Writer Agent:
**READ:**
- {{.WorkspacePath}}/todo_creation_human/planning/plan.md (plan)
- {{.WorkspacePath}}/todo_creation_human/execution/step_*_execution_results.md (all step execution results)
- {{.WorkspacePath}}/todo_creation_human/execution/completed_steps.md (completed work)
- {{.WorkspacePath}}/todo_creation_human/execution/evidence/ (evidence)
- {{.WorkspacePath}}/todo_creation_human/validation/step_*_validation_report.md (all step validation reports)

**WRITE:**
- **CREATE** {{.WorkspacePath}}/todo_final.md (final todo list)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/
- Single execution mode - no iteration analysis needed
- Keep todo_final.md concise and actionable

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
1. **Planning**: Create direct plan to execute objective
2. **Independent Steps Extraction**: Extract steps as structured JSON (no file creation, reads from workspace)
3. **Step-by-Step Execution with Validation**: For each step: Execute → Validate execution output
4. **Writing**: Create final todo list based on execution and validation (reads from workspace)
5. **Complete**: Ready for human review and execution phase

### **🔍 Workspace-Only Reading Pattern**
All agents read from workspace files independently - no direct output passing between agents:
- **Planning Agent**: Creates plan.md, no input from other agents
- **Plan Breakdown Agent**: Reads plan.md, returns JSON (no file creation)
- **Execution Agent**: Reads plan.md and completed_steps.md, creates execution results
- **Validation Agent**: Validates step execution output directly (receives step title, description, and execution output as input)
- **Writer Agent**: Reads plan.md, step_*_execution_results.md, completed_steps.md, evidence/, step_*_validation_report.md to create final todo list

` + memory.GetWorkflowMemoryRequirements() // Include generic memory requirements
}
