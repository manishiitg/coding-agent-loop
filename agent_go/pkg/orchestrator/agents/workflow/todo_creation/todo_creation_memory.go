package todo_creation

import "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

// GetTodoCreationMemoryRequirements returns memory requirements specific to todo creation workflow
func GetTodoCreationMemoryRequirements() string {
	return `
## 📁 TODO CREATION WORKSPACE STRUCTURE

### **Directory Structure**
` + "```" + `
{{.WorkspacePath}}/
├── todo_creation/              (Planning workspace - temporary)
│   ├── planning/
│   │   └── plan.md            (APPEND: All iteration plans with "## Iteration X" sections)
│   ├── execution/
│   │   ├── execution_results.md       (APPEND: All execution results per iteration)
│   │   ├── completed_steps.md        (UPDATE: Track completed steps, avoid re-execution)
│   │   └── evidence/                 (CREATE: Evidence files for critical steps)
│   ├── validation/
│   │   └── execution_validation_report.md  (APPEND: All validations per iteration)
│   ├── cleanup/
│   │   ├── cleanup_report.md         (CREATE: Cleanup summary)
│   │   └── archived/                 (MOVE: Archived intermediate files)
│   ├── todo.md                (UPDATE: Working todo list during iterations)
│   └── iteration_analysis.md   (CREATE: Analysis of all iterations)
└── todo_final.md               (CREATE: Final todo list moved here after cleanup)
` + "```" + `

### **📁 FILE PERMISSIONS BY AGENT**

#### Planning Agent:
**READ:**
- {{.WorkspacePath}}/todo_creation/planning/plan.md (all "## Iteration X" sections)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (for learnings)
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md (for insights)

**WRITE:**
- **APPEND** to {{.WorkspacePath}}/todo_creation/planning/plan.md (add "## Iteration X" section)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation/
- Never overwrite existing iterations - always APPEND
- If iteration == 1, create new file; if iteration > 1, append to existing

---

#### Execution Agent:
**READ:**
- {{.WorkspacePath}}/todo_creation/planning/plan.md (current iteration plan)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (previous results)
- {{.WorkspacePath}}/todo_creation/execution/completed_steps.md (SKIP completed steps)

**WRITE:**
- **APPEND** to {{.WorkspacePath}}/todo_creation/execution/execution_results.md
- **UPDATE** {{.WorkspacePath}}/todo_creation/execution/completed_steps.md (add newly completed)
- **CREATE** files in {{.WorkspacePath}}/todo_creation/execution/evidence/ (only for critical steps)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation/
- SKIP any steps marked as COMPLETED in completed_steps.md
- Evidence ONLY for: quantitative claims, assumptions, failures, decisions

---

#### Validation Agent:
**READ:**
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (current iteration)
- {{.WorkspacePath}}/todo_creation/execution/completed_steps.md
- {{.WorkspacePath}}/todo_creation/execution/evidence/ (verify evidence exists)
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md (previous validations)

**WRITE:**
- **APPEND** to {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation/
- READ-ONLY for execution files - never modify execution results
- Focus on validating evidence quality and identifying gaps

---

#### Writer Agent:
**READ:**
- {{.WorkspacePath}}/todo_creation/planning/plan.md (ALL "## Iteration X" sections)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (ALL iterations)
- {{.WorkspacePath}}/todo_creation/execution/completed_steps.md (all completed work)
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md (ALL validations)
- {{.WorkspacePath}}/todo_creation/execution/evidence/ (review all evidence)

**WRITE:**
- **UPDATE** {{.WorkspacePath}}/todo_creation/todo.md (synthesized todo list)
- **CREATE** {{.WorkspacePath}}/todo_creation/iteration_analysis.md (iteration comparison)

**RESTRICTIONS:**
- Only modify files within {{.WorkspacePath}}/todo_creation/
- Must read ALL "## Iteration X" sections (not just latest)
- Keep todo.md under 1000 lines (concise and actionable)

---

#### Critique Agent:
**READ:**
- {{.WorkspacePath}}/todo_creation/todo.md (final todo to critique)
- {{.WorkspacePath}}/todo_creation/planning/plan.md (verify all iterations referenced)
- {{.WorkspacePath}}/todo_creation/execution/execution_results.md (verify best methods selected)
- {{.WorkspacePath}}/todo_creation/validation/execution_validation_report.md (verify evidence quality)
- {{.WorkspacePath}}/todo_creation/iteration_analysis.md (verify synthesis)

**WRITE:**
- Returns critique report directly (no file writes)

**RESTRICTIONS:**
- READ-ONLY agent - does not modify any files
- Must count iteration references to verify synthesis completeness
- Must provide clear STOP_ITERATIONS: Yes/No signal

---

#### Cleanup Agent:
**READ:**
- Use list_workspace_files to discover all files in {{.WorkspacePath}}/todo_creation/
- {{.WorkspacePath}}/todo_creation/todo.md (final todo to move)

**WRITE:**
- **MOVE** {{.WorkspacePath}}/todo_creation/todo.md → {{.WorkspacePath}}/todo_final.md
- **CREATE** {{.WorkspacePath}}/todo_creation/cleanup/cleanup_report.md
- **MOVE** intermediate files to {{.WorkspacePath}}/todo_creation/cleanup/archived/

**RESTRICTIONS:**
- Can MOVE todo.md outside todo_creation/ to workspace root (special exception)
- Archive patterns: *_draft.md, temp_*, intermediate_*
- Preserve: plan.md, execution_results.md, validation report, iteration_analysis.md

---

### **🔍 File Discovery Requirements**
All agents should:
1. Use **list_workspace_files** to check if files exist before reading
2. Use **regex_search_workspace_files** to find specific patterns (e.g., "## Iteration" count)
3. Handle missing files gracefully (iteration == 1 vs iteration > 1)

### **📝 Evidence Collection Rules**
**Collect evidence ONLY for:**
- Quantitative claims (numbers, metrics, percentages)
- Assumptions about system behavior
- Failed attempts (to understand what went wrong)
- Decision points (choosing between alternatives)

**NO evidence needed for:**
- Simple file operations (create, read, list)
- Obvious success indicators (file exists, command succeeded)

### **🔄 Iteration Awareness**
- **Iteration 1**: Create new files (plan.md, execution_results.md, etc.)
- **Iteration > 1**: APPEND to existing files (never overwrite previous iterations)
- **Check before read**: If file doesn't exist and iteration > 1, warn about missing context

` + memory.GetWorkflowMemoryRequirements() // Include generic memory requirements
}
