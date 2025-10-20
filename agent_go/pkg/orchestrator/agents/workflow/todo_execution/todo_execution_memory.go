package todo_execution

import "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

// GetTodoExecutionMemoryRequirements returns memory requirements specific to todo execution workflow
func GetTodoExecutionMemoryRequirements() string {
	return `
## 📁 TODO EXECUTION WORKSPACE STRUCTURE

### **Directory Structure**
` + "```" + `
{{.WorkspacePath}}/
├── todo_final.md                    (READ-ONLY: Original todo list - never modify)
└── runs/                            (Execution runs folder)
    └── {selected-run}/              (Run folder - name depends on run mode)
        ├── todo_snapshot.md         (WORKING COPY: Mark completions + add evidence links)
        ├── execution_output.md      (Execution agent writes summary here)
        ├── validation_report.md     (Validation agent writes report here)
        └── outputs/                 (Generated files, artifacts, evidence)
            └── (any files created during execution)
` + "```" + `

**Note**: Run folder name depends on run mode (see "Run Folder Selection Rules" below)

**Balanced Structure**: 3 core files + 1 outputs folder for any generated files/artifacts/evidence.

### **📁 FILE PERMISSIONS BY AGENT**

#### Execution Agent:
**READ:**
- {{.WorkspacePath}}/todo_final.md (READ-ONLY: original todo list)
- {{.WorkspacePath}}/runs/{selected}/todo_snapshot.md (if continuing existing run)

**WRITE:**
- {{.WorkspacePath}}/runs/{selected}/execution_output.md (execution summary + results)
- {{.WorkspacePath}}/runs/{selected}/outputs/* (generated files, evidence, artifacts)

**RESTRICTIONS:**
- **NEVER** modify {{.WorkspacePath}}/todo_final.md (READ-ONLY)
- **NEVER** modify todo_snapshot.md (Workspace Update Agent does this)
- Store large outputs/evidence in outputs/ folder, reference in execution_output.md

---

#### Validation Agent:
**READ:**
- {{.WorkspacePath}}/todo_final.md (READ-ONLY: reference original)
- {{.WorkspacePath}}/runs/{selected}/todo_snapshot.md (READ-ONLY: current state)
- {{.WorkspacePath}}/runs/{selected}/execution_output.md (what to validate)
- {{.WorkspacePath}}/runs/{selected}/outputs/* (check evidence files)

**WRITE:**
- {{.WorkspacePath}}/runs/{selected}/validation_report.md (validation results)

**RESTRICTIONS:**
- READ-ONLY for todo_snapshot.md, execution_output.md, and outputs/
- Only write to validation_report.md
- Verify claims against evidence files in outputs/

---

#### Workspace Update Agent:
**READ:**
- {{.WorkspacePath}}/todo_final.md (READ-ONLY: reference original)
- {{.WorkspacePath}}/runs/{selected}/todo_snapshot.md (current state)
- {{.WorkspacePath}}/runs/{selected}/execution_output.md (execution summary)
- {{.WorkspacePath}}/runs/{selected}/validation_report.md (validation results)
- {{.WorkspacePath}}/runs/{selected}/outputs/* (any generated files)

**WRITE:**
- **UPDATE** {{.WorkspacePath}}/runs/{selected}/todo_snapshot.md (add evidence references, mark completions)

**OPTIONAL WRITE (if tool available):**
- GitHub sync via sync_workspace_to_github tool

**RESTRICTIONS:**
- **NEVER** modify {{.WorkspacePath}}/todo_final.md (READ-ONLY)
- Only update todo_snapshot.md with evidence references and completion status
- GitHub sync is optional - check if tool exists before using

---

### **🔍 Run Folder Selection Rules**

All agents must follow this deterministic selection process:

1. **List existing runs**: Use list_workspace_files to list {{.WorkspacePath}}/runs/
2. **Parse run folders**: Identify folders matching pattern YYYY-MM-DD-*
3. **Select based on mode**:
   - **use_same_run**: Pick latest existing (highest date/name), create if none exist
   - **create_new_runs_always**: Always create new YYYY-MM-DD-{descriptive-name}
   - **create_new_run_once_daily**: Check if today's YYYY-MM-DD-* exists; create if not, use if exists

4. **Naming convention**:
   - Format: YYYY-MM-DD-{descriptive-name}
   - Example: 2025-01-27-iteration-1, 2025-01-27-iteration-2
   - Use descriptive names: initial, retry, batch-1, etc.

5. **Snapshot handling**:
   - If todo_snapshot.md exists in selected run: Continue from current state
   - If todo_snapshot.md missing: Copy from {{.WorkspacePath}}/todo_final.md

---

### **📝 Evidence Collection Rules**

**Collect evidence for:**
- Every completed todo (link to execution logs, results)
- Quantitative claims (metrics, data, measurements)
- Failed attempts (error logs, debug info)
- Generated outputs (data files, artifacts)

**Evidence format in todo_snapshot.md:**
` + "```markdown" + `
### Todo ID: todo_001
**Status**: ✅ Completed
**Evidence**:
- Execution: [runs/{run}/execution_output.md](runs/{run}/execution_output.md)
- Validation: [runs/{run}/validation_report.md](runs/{run}/validation_report.md)
- Outputs: [runs/{run}/outputs/](runs/{run}/outputs/)

**Success Criteria**:
- [x] Criterion 1: ✅ Verified in validation report
- [x] Criterion 2: ✅ Confirmed in execution output
` + "```" + `

---

### **🔄 Run Mode Behavior**

#### use_same_run:
- **Behavior**: Reuse the same run folder across executions
- List runs/, select latest existing folder (any name)
- If no runs exist: Create runs/initial/ (or another base name)
- Continue from existing todo_snapshot.md if present
- Preserves all previous execution data
- **Folder naming**: Non-dated names like initial/, main/, batch-1/

#### create_new_runs_always:
- **Behavior**: Create a new run folder for EVERY execution
- Always create new runs/YYYY-MM-DD-{name}/
- Fresh start with copy of todo_final.md
- Isolate from previous runs
- Use incremental names: iteration-1, iteration-2, etc.
- **Folder naming**: Dated names like 2025-01-27-iteration-1/, 2025-01-27-iteration-2/

#### create_new_run_once_daily:
- **Behavior**: Create ONE new run folder per day, reuse for same day
- Check for today's YYYY-MM-DD-* folder
- Create only if doesn't exist for today
- First execution today: new folder (YYYY-MM-DD-{name})
- Subsequent executions same day: use existing today's folder
- **Folder naming**: Dated names like 2025-01-27-daily/

---

### **⚠️ CRITICAL RESTRICTIONS**

**NEVER MODIFY:**
- {{.WorkspacePath}}/todo_final.md (original todo list is READ-ONLY)
- Files outside {{.WorkspacePath}}/
- Other workspace folders

**ALWAYS MODIFY:**
- {{.WorkspacePath}}/runs/{selected}/todo_snapshot.md (working copy)

**OPTIONAL (with tool check):**
- GitHub sync (only if sync_workspace_to_github tool exists)

` + memory.GetWorkflowMemoryRequirements() // Include generic memory requirements
}
