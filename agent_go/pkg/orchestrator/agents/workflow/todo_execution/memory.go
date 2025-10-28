package todo_execution

import "mcp-agent/agent_go/pkg/orchestrator/agents/workflow/memory"

// GetTodoExecutionMemoryRequirements returns memory requirements specific to todo execution workflow
func GetTodoExecutionMemoryRequirements() string {
	return `
## 📁 TODO EXECUTION WORKSPACE STRUCTURE

### **Directory Structure**
` + "```" + `
{{.WorkspacePath}}/
├── validation_report.md             (Validation agent writes report here)
└── outputs/                         (Generated files, artifacts, evidence)
    └── (any files created during execution)
` + "```" + `

**Note**: WorkspacePath already points to the selected run folder (e.g., workspace/runs/2025-01-27-iteration-1)

**Simplified Structure**: 1 core file + 1 outputs folder. Execution results are captured in conversation history, not files.

### **📁 STEP INFORMATION**

**Step Details**: You receive step information via template variables:
- **Title**: Clear, concise step name
- **Description**: Detailed what and how, including specific tools and approaches
- **Success Criteria**: Measurable completion criteria
- **Why This Step**: Explain purpose and value
- **Context Dependencies**: What needs to be done before this step
- **Context Output**: What this step produces for subsequent steps
- **Success Patterns**: List of approaches/tools that worked (use these proven approaches)
- **Failure Patterns**: List of approaches/tools that failed (avoid these approaches)

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
**WRITE:**
- {{.WorkspacePath}}/outputs/* (any files created during execution, if needed)

**EXECUTION FOCUS:**
- **Execute the step using MCP tools** - focus on getting the work done
- **Create files in outputs/ only if required** by the step itself
- **No documentation required** - don't create execution_output.md or summary files
- **Follow Success Patterns exactly** - these are validated approaches
- **Avoid all Failure Patterns** - these approaches have failed
- **The orchestrator captures your execution results** automatically

---

#### Validation Agent:
**WRITE:**
- {{.WorkspacePath}}/validation_report.md ONLY (step-wise validation results)

**EXECUTION HISTORY:**
- **Receive full conversation history** - all execution conversations, tool calls, and responses
- **Validate based on conversation history** - not file contents
- **No file reading needed** - all information provided in execution history

**RESTRICTIONS:**
- Only write to validation_report.md
- Validation is based on the execution history provided, not file contents
- **Validate single step only** - check if THIS SPECIFIC STEP was completed correctly
- **Step-focused** - each validation report covers only one specific step

---

### **🔍 Execution Context**

**Important**: The workspace path already points to your current run folder.
- The orchestrator handles run folder selection and naming
- You work directly within the assigned run folder
- Focus on executing the step with the given information

### **🔄 Feedback Loop**

**Execution-Validation Feedback Loop:**
1. **Execute step** - Execution agent uses MCP tools to complete the step
2. **Validate execution** - Validation agent reviews the full execution conversation history
3. **Provide feedback** - If validation fails, feedback is sent back to execution agent
4. **Retry with feedback** - Execution agent retries with feedback (up to 3 attempts)
5. **Continue to next step** - Once validation passes, move to next step

**Key Points:**
- **Full conversation history** shared with validation agent (not just summary)
- **Automatic feedback loop** - validation feedback automatically improves execution
- **Step-by-step validation** - each step validated individually before moving to next
- **Up to 3 retry attempts** per step with validation feedback

---

### **⚠️ CRITICAL RESTRICTIONS**

**NEVER MODIFY:**
- Files outside {{.WorkspacePath}}/
- Other workspace folders

**ALWAYS CREATE:**
- {{.WorkspacePath}}/validation_report.md (step-wise validation results)

**OPTIONAL CREATE:**
- {{.WorkspacePath}}/outputs/* (files created during execution, only if step requires them)

**OPTIONAL (with tool check):**
- GitHub sync (only if sync_workspace_to_github tool exists)

**STEP-WISE EXECUTION REQUIREMENTS:**
- **Execute one step at a time** - focus on single step completion using MCP tools
- **Follow Success Patterns exactly** - these are validated approaches
- **Avoid all Failure Patterns** - these approaches have failed
- **Check Context Dependencies** - ensure prerequisites are satisfied
- **Produce Context Output** - ensure step produces expected output for subsequent steps
- **No documentation required** - focus on execution, not writing summaries
- **Execution results captured automatically** - orchestrator tracks full conversation history
- **Validation feedback loop** - use feedback from validation to improve retries (up to 3 attempts)

` + memory.GetWorkflowMemoryRequirements() // Include generic memory requirements
}
