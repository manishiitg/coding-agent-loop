package prompts

import (
	"mcp-agent/agent_go/pkg/utils"
)

// MemoryManagement contains all the structured memory management requirements and templates
// that are shared across all agent types (planning, execution, validation)
type MemoryManagement struct {
}

// NewMemoryManagement creates a new instance with all memory management components
func NewMemoryManagement() *MemoryManagement {
	return &MemoryManagement{}
}

// GetUnifiedMemoryRequirements returns a single comprehensive memory management section
// that can be included in any agent prompt
func (mm *MemoryManagement) GetUnifiedMemoryRequirements() string {
	return `## 🧠 **MEMORY MANAGEMENT REQUIREMENTS**

### **📁 Task Structure**
Tasks/
├── [TaskName]/
│   ├── index.md (Task overview, objectives, progress)
│   ├── plan.md (Current execution plan and steps)
│   ├── report.md (Findings, results, and conclusions)
│   ├── evidence/ (All execution evidence)
│   │   ├── step_[N]_[Description].md (Step execution details)
│   │   ├── tool_outputs/ (Raw tool outputs and responses)
│   │   ├── screenshots/ (Visual evidence if applicable)
│   │   └── logs/ (Execution logs and timestamps)
│   ├── progress/ (Progress tracking)
│   │   ├── completed_steps.md (List of completed steps)
│   │   ├── pending_steps.md (List of pending steps)
│   │   └── validation_results.md (Validation outcomes)
│   └── context/ (Context and background information)
│       ├── requirements.md (Task requirements)
│       ├── constraints.md (Limitations and constraints)
│       └── resources.md (Available resources and tools)

### **🤖 Multi-Agent System Awareness**
You are part of a multi-agent orchestrator system with these agents:
- **Planning Agent**: Creates execution plans and step definitions
- **Execution Agent**: Executes planned steps using MCP tools
- **Validation Agent**: Validates execution results and quality
- **Organizer Agent**: Manages memory organization and cleanup

**Inter-Agent Coordination:**
- **Read Previous Work**: Check evidence/ folder for outputs from other agents
- **Share Your Work**: Document file paths in your output for other agents
- **Context Continuity**: Reference and build upon other agents' work

#### **📁 Workspace Memory (File System) - Detailed Documentation**

` + mm.GetCriticalFileOperationsProtocol() + `

**Workspace Memory Workflow:**
2. **Store Plan**: Update plan.md with current step (use diff_patch_workspace_file)
3. **Update Progress**: Mark step in progress tracking (use diff_patch_workspace_file)
4. **Store Evidence**: Create step_[N]_[Description].md in evidence folder
5. **Git Sync**: Use sync_workspace_to_github tool to sync changes and maintain version control
6. **Basic Cleanup**: Remove duplicates, organize structure

### **🔍 File Discovery & Search**
**Use these tools to efficiently find files:**

- **list_workspace_files**: List files and directories to check existence and structure
  - Use for: checking if files/folders exist, exploring directory structure, finding file names
  - Example: list_workspace_files(folder="Tasks/MyTask") to see what's in the task folder
  
- **regex_search_workspace_files**: Regex/text-based search for exact matches
  - Use for: finding specific text, file names, patterns, exact keywords, complex regex patterns
  - Example: regex_search_workspace_files(query="TODO", folder="Tasks/MyTask")
  
- **semantic_search_workspace_files**: AI-powered semantic search for meaning-based discovery
  - Use for: finding conceptually related content, understanding context
  - Example: semantic_search_workspace_files(query="error handling patterns", folder="Tasks/MyTask")

**Search Strategy:**
1. **Start with listing** to see what files and folders exist in the directory
2. **Use semantic search** to understand what files exist and their content
3. **Use regex search** for specific text patterns, file names, or exact matches
4. **Use regex patterns** for complex pattern matching and multi-line searches
5. **Combine all tools** for comprehensive file discovery
6. **Filter by folder** to search within specific task directories

### **🧹 Cleanup & Optimization**
- **Duplicates**: Use regex_search_workspace_files to find and consolidate
- **Orphaned Files**: Remove files not linked to active work
- **Large Files**: Archive or split files over 10KB
- **Archive Completed**: Move to archived/old_evidence/

### **📋 Step Evidence Template**
**Create step_[N]_[Description].md in evidence folder:**

## Step Execution: [Step Description]
**Step Number**: [N]
**Date**: [YYYY-MM-DD]
**Status**: [Completed/Failed/In Progress]
**Success Criteria Met**: [Yes/No/Partial]

### Execution Summary
[What was accomplished in this step]

### Tools Used
- **Tool Name**: [Tool name and parameters]
- **Command/Query**: [Specific command or query used]
- **Output**: [Relevant output snippet]
- **Evidence File**: evidence/tool_outputs/[filename].md

### Findings
- **Finding 1**: [Description with risk level if applicable]
- **Finding 2**: [Description with risk level if applicable]

### Issues Encountered
- **Issue 1**: [Description and resolution]
- **Issue 2**: [Description and resolution]

### Success Criteria Assessment
- **Criteria 1**: [Met/Not Met] - [Explanation]
- **Criteria 2**: [Met/Not Met] - [Explanation]

### Next Steps
[What should be done next based on this step's results]

### **⚠️ Guidelines**
- **Scope**: Only read/write within Tasks/[TaskName]/
- **Efficiency**: Use targeted reads, avoid full file reading
- **Search First**: Use search tools to locate relevant files before reading
- **Persistence**: Store work so it can resume later
- **Support Role**: Memory operations support your primary task, don't replace it


` + utils.GetCommonFileInstructions() + `


`

}

// GetSimplifiedMemoryRequirements returns simplified memory management requirements
func (mm *MemoryManagement) GetSimplifiedMemoryRequirements() string {
	return `## 🧠 **SIMPLIFIED MEMORY MANAGEMENT**

### **📁 Workspace Structure**
{{.WorkspacePath}}/
├── plan.md (Current plan and steps)
├── evidence/ (All execution evidence)
├── progress.md (Completed steps tracking)
└── report.md (Final findings)

### **🔧 File Operations**
1. **READ**: Use read_workspace_file to see current content
2. **UPDATE**: Use diff_patch_workspace_file for changes
3. **VERIFY**: Check changes before applying

### **🔍 File Discovery**
- **list_workspace_files**: See what files exist
- **regex_search_workspace_files**: Find specific text
- **semantic_search_workspace_files**: Find related content

### **⚠️ Guidelines**
- Work only within {{.WorkspacePath}}/
- Preserve completed work
- Remove only true duplicates
- Document all changes

` + mm.GetCriticalFileOperationsProtocol() + `

### **🤖 Multi-Agent System Awareness**
You are part of a multi-agent orchestrator system with these agents:
- **Planning Agent**: Creates execution plans and step definitions
- **Execution Agent**: Executes planned steps using MCP tools
- **Validation Agent**: Validates execution results and quality
- **Organizer Agent**: Manages memory organization and cleanup

**Inter-Agent Coordination:**
- **Read Previous Work**: Check evidence/ folder for outputs from other agents
- **Share Your Work**: Document file paths in your output for other agents
- **Context Continuity**: Reference and build upon other agents' work

### **📂 Workflow Context**
- **Current Workflow**: {{.WorkflowPath}}
- **Workflow Files**: Check {{.WorkflowPath}}/ for workflow-specific files and configurations

` + utils.GetCommonFileInstructions() + `

`
}

// GetBasePromptTemplate returns a standardized prompt template for all orchestrator agents
func (mm *MemoryManagement) GetBasePromptTemplate(agentType, agentDescription, specificContext, specificInstructions, outputFormat string) string {
	return `## 🎯 OBJECTIVE & INPUTS
You are a ` + agentType + ` agent. ` + agentDescription + `

The Tasks/[TaskName] folder will be specified in the objective input.
` + specificContext + `

## 🔧 WORKSPACE SCOPE & BOUNDARIES  
- **STRICT BOUNDARY**: ONLY work within {{.WorkspacePath}} - do not access other folders
- **Write Bounds**: Only read/write within {{.WorkspacePath}}/; never touch .env, root configs, secrets
- **Memory Integration**: ` + mm.GetSimplifiedMemoryRequirements() + `

## 📋 AGENT-SPECIFIC INSTRUCTIONS
` + specificInstructions + `

## 📤 OUTPUT REQUIREMENTS
` + outputFormat
}

// GetCriticalFileOperationsProtocol returns the critical file operations protocol text
func (mm *MemoryManagement) GetCriticalFileOperationsProtocol() string {
	return `**🚨 CRITICAL FILE OPERATIONS PROTOCOL:**
When working with files, follow this MANDATORY 5-step workflow:
1. **READ FIRST**: 🚨 MANDATORY - Always use read_workspace_file to see exact current content
2. **CHOOSE METHOD**: 
   - **PREFERRED**: Use diff_patch_workspace_file for all file updates (more efficient, smaller payloads, better version control)
   - **ONLY for**: Use update_workspace_file for complete file rewrites or new files
3. **DIFF FORMAT**: If using diff_patch_workspace_file, generate perfect unified diff format like 'diff -U0'
4. **CONTEXT MATCHING**: 🚨 CRITICAL - Context lines (starting with space) must match file content EXACTLY
5. **VERIFY**: Test your approach before applying changes

**Diff Patch Requirements:**
- ✅ Use read_workspace_file first to get exact file content
- ✅ Copy context lines EXACTLY from the file (including spaces/tabs)
- ✅ Ensure diff ends with a newline character
- ✅ Use proper unified diff format with ---/+++ headers
- ✅ Generate diffs like 'diff -U0' would produce
- ✅ Verify line numbers in hunk headers match actual file

**🚨 CRITICAL CONTEXT LINE FORMAT:**
- Context lines MUST start with SPACE ( ), NOT minus (-)!
- Correct: ' # Header' (space + content)
- Wrong:   '- # Header' (minus + content)
- Context lines show unchanged content, removals show deleted content`
}
