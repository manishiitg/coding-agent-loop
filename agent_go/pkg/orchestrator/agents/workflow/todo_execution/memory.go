package todo_execution

import "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

// GetTodoExecutionMemoryRequirements returns memory requirements specific to todo execution workflow
func GetTodoExecutionMemoryRequirements() string {
	return `
## 📁 TODO EXECUTION WORKSPACE STRUCTURE

### **Directory Structure**
` + "```" + `
{{.WorkspacePath}}/
├── todo_final.md                    (READ-ONLY: Structured todo list with Success/Failure Patterns)
└── runs/                            (Execution runs folder)
    └── {selected-run}/              (Run folder - name depends on run mode)
        ├── execution_output.md      (Execution agent writes summary here)
        ├── validation_report.md     (Validation agent writes report here)
        └── outputs/                 (Generated files, artifacts, evidence)
            └── (any files created during execution)
` + "```" + `

**Note**: Run folder name depends on run mode (see "Run Folder Selection Rules" below)

**Simplified Structure**: 2 core files + 1 outputs folder for any generated files/artifacts/evidence.

### **📁 STRUCTURED TODO FORMAT**

The ` + "`todo_final.md`" + ` file contains structured steps with the following format:

#### **Step Structure**
Each step in ` + "`todo_final.md`" + ` contains:
- **Title**: Clear, concise step name
- **Description**: Detailed what and how, including specific tools and approaches
- **Success Criteria**: Measurable completion criteria
- **Why This Step**: Explain purpose and value
- **Context Dependencies**: What needs to be done before this step
- **Context Output**: What this step produces for subsequent steps
- **Success Patterns**: List of approaches/tools that WORKED (from learning reports)
- **Failure Patterns**: List of approaches/tools that FAILED (from learning reports)

#### **Success Patterns Usage**
- **Follow Success Patterns Exactly**: These are validated approaches that worked before
- **Include Specific Tools**: Success Patterns contain exact MCP server and tool names
- **Include Specific Arguments**: Success Patterns contain exact command arguments
- **Include Specific Methods**: Success Patterns contain exact approaches that succeeded

#### **Failure Patterns Avoidance**
- **Avoid All Failure Patterns**: These approaches have failed and should not be used
- **Include Specific Tools**: Failure Patterns contain exact MCP server and tool names to avoid
- **Include Specific Arguments**: Failure Patterns contain exact command arguments that failed
- **Include Specific Methods**: Failure Patterns contain exact approaches that failed

#### **Context Dependencies**
- **Check Prerequisites**: Before executing each step, verify Context Dependencies are satisfied
- **Sequential Execution**: Steps must be executed in order due to dependencies
- **Context Output**: Each step must produce the Context Output listed for subsequent steps

### **📁 FILE PERMISSIONS BY AGENT**

#### Execution Agent:
**READ:**
- {{.WorkspacePath}}/todo_final.md (READ-ONLY: structured todo list with patterns)

**WRITE:**
- {{.WorkspacePath}}/runs/{selected}/execution_output.md (step-wise execution summary)
- {{.WorkspacePath}}/runs/{selected}/outputs/* (any files created during execution)

**RESTRICTIONS:**
- **NEVER** modify {{.WorkspacePath}}/todo_final.md (READ-ONLY)
- **Focus on execution only** - no evidence collection or complex documentation
- **Follow Success Patterns exactly** - these are validated approaches
- **Avoid all Failure Patterns** - these approaches have failed
- **Produce step-wise output** - clear summary of what was executed for this specific step

---

#### Validation Agent:
**READ:**
- {{.WorkspacePath}}/todo_final.md (READ-ONLY: reference original with patterns)
- {{.WorkspacePath}}/runs/{selected}/execution_output.md (step-wise execution summary to validate)
- {{.WorkspacePath}}/runs/{selected}/outputs/* (check any files created during execution)

**WRITE:**
- {{.WorkspacePath}}/runs/{selected}/validation_report.md (validation results)

**RESTRICTIONS:**
- READ-ONLY for execution_output.md and outputs/
- Only write to validation_report.md
- **Validate step-wise execution** - check if this specific step was completed correctly
- **Validate Success Patterns were used** - check proven approaches were followed
- **Validate Failure Patterns were avoided** - check failed approaches were not used
- **Validate Context Output was produced** - check step produced expected output

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

5. **Fresh execution**:
   - Each run starts fresh with no state tracking
   - Execution and validation files are created as needed

---

### **📝 Step-Wise Execution Output**

**Execution Agent Output Format:**
Each step produces a simple execution summary in execution_output.md:

` + "```markdown" + `
# Step Execution Summary

## Step Details
- **Step Number**: 1/3
- **Step Title**: Setup Environment
- **Status**: Completed

## What Was Done
- **Approach**: [Brief description of what was executed]
- **Tools Used**: [MCP tools used for this step]
- **Success Criteria Status**: [Which criteria were met]

## Files Created/Modified
- [List any files created or modified during execution]
` + "```" + `

**Key Points:**
- **Step-focused**: Each execution output covers only one specific step
- **Simple format**: Basic summary without complex evidence collection
- **Clear status**: Shows if the step was completed successfully
- **Tool usage**: Lists MCP tools used for transparency
- **File tracking**: Notes any files created/modified during execution

---

### **🔄 Run Mode Behavior**

#### use_same_run:
- **Behavior**: Reuse the same run folder across executions
- List runs/, select latest existing folder (any name)
- If no runs exist: Create runs/initial/ (or another base name)
- Fresh execution each time with no state persistence
- **Folder naming**: Non-dated names like initial/, main/, batch-1/

#### create_new_runs_always:
- **Behavior**: Create a new run folder for EVERY execution
- Always create new runs/YYYY-MM-DD-{name}/
- Fresh start with no state persistence
- Isolate from previous runs
- Use incremental names: iteration-1, iteration-2, etc.
- **Folder naming**: Dated names like 2025-01-27-iteration-1/, 2025-01-27-iteration-2/

#### create_new_run_once_daily:
- **Behavior**: Create ONE new run folder per day, reuse for same day
- Check for today's YYYY-MM-DD-* folder
- Create only if doesn't exist for today
- First execution today: new folder (YYYY-MM-DD-{name})
- Subsequent executions same day: use existing today's folder
- Fresh execution each time with no state persistence
- **Folder naming**: Dated names like 2025-01-27-daily/

---

### **⚠️ CRITICAL RESTRICTIONS**

**NEVER MODIFY:**
- {{.WorkspacePath}}/todo_final.md (original structured todo list is READ-ONLY)
- Files outside {{.WorkspacePath}}/
- Other workspace folders

**ALWAYS CREATE:**
- {{.WorkspacePath}}/runs/{selected}/execution_output.md (step-wise execution summary)
- {{.WorkspacePath}}/runs/{selected}/validation_report.md (step-wise validation results)

**OPTIONAL (with tool check):**
- GitHub sync (only if sync_workspace_to_github tool exists)

**STEP-WISE EXECUTION REQUIREMENTS:**
- **Execute one step at a time** - focus on single step completion
- **Follow Success Patterns exactly** - these are validated approaches
- **Avoid all Failure Patterns** - these approaches have failed
- **Check Context Dependencies** - ensure prerequisites are satisfied
- **Produce Context Output** - ensure step produces expected output for subsequent steps
- **Simple output format** - basic summary without complex evidence collection

` + memory.GetWorkflowMemoryRequirements() // Include generic memory requirements
}
