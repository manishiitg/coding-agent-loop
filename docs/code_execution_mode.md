# Code Execution Mode

## 📋 Overview

Code Execution Mode allows the Execution Agent to generate and run Go code instead of using MCP tools directly. This enables complex operations, batch processing, and reusable code patterns across workflow iterations.

**Key Benefits:**
- **Reusability**: Code works across iterations with different workspace paths
- **Complex operations**: Batch processing and multi-step logic in single execution
- **Pattern capture**: Learning agent captures successful code patterns for reuse

---

## 📁 Key Files & Locations

| Component | File Path | Key Functions |
|-----------|-----------|---------------|
| **Execution Agent** | [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/execution_only_agent.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/execution_only_agent.go) | Code execution mode instructions |
| **Learning Agent** | [`agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/learning_agent_code_execution.go`](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/learning_agent_code_execution.go) | Captures Go code patterns for reuse |

---

## 🚨 Critical Rule: WorkspacePath as CLI Argument

**ALWAYS pass WorkspacePath as the first CLI argument (`os.Args[1]`)**

### Why This Matters

- Workspace paths change between iterations: `workspace/runs/run-1/execution/step-1` → `workspace/runs/run-2/execution/step-1`
- Hardcoded paths in Go code break when reused across iterations
- Passing path via CLI args makes code reusable - only the argument changes

---

## 🔄 Two-Step Process

### 1. Tool Call (`write_code`)

**✅ Correct:**
```json
{
  "code": "package main\n...",
  "args": ["{{.WorkspacePath}}", "other_var1", "other_var2"]
}
```
- Pass ONLY base path as first argument
- Additional variables follow

**❌ Wrong:**
```json
// Passing full file paths
{"args": ["{{.WorkspacePath}}/step-1/file.json", ...]}

// Missing workspace path
{"args": ["other_var1"]}
```

### 2. Go Code Content

**✅ Correct:**
```go
package main
import (
    "os"
    "path/filepath"
    "workspace_tools"
)

func main() {
    // 1. Read base workspace path (ALWAYS first argument)
    basePath := os.Args[1]      // e.g., "Workflow/runs/iteration-11/execution"
    userId := os.Args[2]        // Additional variables
    
    // 2. Use relative paths with filepath.Join()
    // Read context dependency (previous step output)
    inputPath := filepath.Join(basePath, "step-1/credentials.json")
    inputData := workspace_tools.ReadWorkspaceFile(workspace_tools.ReadWorkspaceFileParams{
        Filepath: inputPath,
    })
    
    // Write to current step folder
    outputPath := filepath.Join(basePath, "step-2/analysis.json")
    result := workspace_tools.UpdateWorkspaceFile(workspace_tools.UpdateWorkspaceFileParams{
        Filepath: outputPath,
        Content:  "...",
    })
}
```

**❌ Wrong:**
```go
// Hardcoded path
filepath := "workspace/runs/run-1/execution/step-1"

// Passing full file paths as CLI arguments
// (should use relative paths with filepath.Join)
```

---

## ⚙️ Path Handling

### WorkspacePath vs StepExecutionPath

| Path Type | Source | Example | Usage |
|-----------|--------|---------|-------|
| **WorkspacePath** | `os.Args[1]` | `Workflow/runs/iteration-11/execution` | Base execution workspace (root) |
| **StepExecutionPath** | Template variable | `execution/step-8` | Specific step folder (relative to workspace root) |
| **Relative Path** | Context dependencies | `step-1/step_1_output.json` | File paths within workspace |
| **Full Path** | `filepath.Join(basePath, relativePath)` | `Workflow/runs/iteration-11/execution/step-1/credentials.json` | All file operations |

### Example

```
WorkspacePath (os.Args[1]): "Workflow/runs/iteration-11/execution"
StepExecutionPath: "execution/step-8"
Relative: "step-1/credentials.json"
Full: filepath.Join(basePath, "step-1/credentials.json") 
     → "Workflow/runs/iteration-11/execution/step-1/credentials.json"
```

---

## ⚙️ Variable Handling

| Aspect | Implementation | Example |
|--------|----------------|---------|
| **Pass** | All variables via `args` parameter | `args=["{{.WorkspacePath}}", "value1", "value2"]` |
| **Access** | Read from `os.Args` | `os.Args[1]` (workspace), `os.Args[2]`, `os.Args[3]`, etc. |
| **NO Hardcoding** | Never hardcode values OR workspace paths | ❌ `path := "fixed/path"` |

---

## ⚙️ Packages & Operations

| Type | Implementation | Example |
|------|----------------|---------|
| **Packages** | Import generated tool packages | `aws_tools`, `workspace_tools`, `google_sheets_tools` |
| **File Ops** | Always use `workspace_tools` | `workspace_tools.ReadWorkspaceFile()` |
| **Paths** | Always use `os.Args[1]` + `filepath.Join()` | `filepath.Join(os.Args[1], "step-1/file.json")` |

---

## 📁 File System Access

Path validation is enforced at runtime. Code can only access allowed folders.

| Access Type | Folders | Purpose |
|------------|---------|---------|
| **READ** | `learnings/`, `execution/` (previous steps), `knowledgebase/` | Read context dependencies, learnings, shared data |
| **WRITE** | `{{.StepExecutionPath}}/` (current step), `knowledgebase/` | Write step output, persistent shared files |

**Rules:**
- ✅ **Step folders** (`execution/step-N/`) are **VOLATILE** - deleted on re-execution/restart
- ✅ **Knowledgebase** (`knowledgebase/`) is **PERSISTENT** - survives across runs
- ❌ Cannot write to other steps' folders or outside allowed paths

---

## 💾 Knowledgebase Folder

Persistent storage shared across all workflow runs and steps.

**Location:** `{workspaceRoot}/knowledgebase/`

**Usage:**
- ✅ Templates, reference data, global configs
- ✅ Files that must survive across execution attempts
- ✅ Shared resources used by multiple steps
- ❌ Step-specific output (use step folder instead)

**Example:**
```go
// Store template in knowledgebase (persistent)
templatePath := filepath.Join(basePath, "../knowledgebase/email_template.json")
workspace_tools.WriteWorkspaceFile(workspace_tools.WriteWorkspaceFileParams{
    Filepath: templatePath,
    Content:  templateContent,
})

// Step output goes to step folder (volatile)
outputPath := filepath.Join(basePath, "step-2/output.json")
workspace_tools.WriteWorkspaceFile(workspace_tools.WriteWorkspaceFileParams{
    Filepath: outputPath,
    Content:  resultContent,
})
```

---

## 🛠️ Common Mistakes to Avoid

| Mistake | Wrong | Correct |
|---------|-------|---------|
| **Full file paths as args** | `args=["{{.WorkspacePath}}/step-1/file.json"]` | `args=["{{.WorkspacePath}}"]` + `filepath.Join(basePath, "step-1/file.json")` |
| **Hardcoded iteration paths** | `path := "Workflow/runs/iteration-11/execution/step-1/file.json"` | `basePath := os.Args[1]`<br>`path := filepath.Join(basePath, "step-1/file.json")` |
| **Missing workspace path** | `args=["userId"]` | `args=["{{.WorkspacePath}}", "userId"]` |

### Examples

**❌ Wrong - Passing full file paths:**
```json
{
  "args": [
    "Workflow/runs/iteration-11/execution",
    "Workflow/runs/iteration-11/execution/step-1/file.json"
  ]
}
```

**✅ Correct - Pass only base path:**
```json
{
  "args": ["Workflow/runs/iteration-11/execution", "userId"]
}
```

```go
basePath := os.Args[1]
filePath := filepath.Join(basePath, "step-1/file.json")
```

**❌ Wrong - Hardcoding iteration:**
```go
filepath := "Workflow/runs/iteration-11/execution/step-1/file.json"
```

**✅ Correct - Use base path + relative:**
```go
basePath := os.Args[1]
filePath := filepath.Join(basePath, "step-1/file.json")
```

---

## 📋 Code Generation Checklist

Before generating Go code:

1. **🚨 WorkspacePath as FIRST CLI argument**: ALWAYS pass ONLY base WorkspacePath as `os.Args[1]` - NEVER pass full file paths
2. **🚨 Use Relative Paths**: ALL file paths in code must use `filepath.Join(basePath, "step-N/file.json")`
3. Check learnings for error patterns to avoid
4. Use correct patterns from successful code examples (but replace hardcoded paths with `filepath.Join()`)
5. Verify Go syntax and imports
6. Parse tool responses correctly

---

## 🔄 Learning Integration

When learnings contain Go code with hardcoded paths:

1. Extract the code pattern
2. Replace hardcoded paths with `os.Args[1]`
3. Keep the logic and error handling patterns
4. Update variable access to use CLI args

---

## 🔍 For LLMs: Quick Reference

**Constraints:**
- ✅ **Allowed**: `os.Args[1]` for workspace path
- ✅ **Allowed**: `filepath.Join(basePath, relativePath)` for file paths
- ✅ **Allowed**: Generated tool packages (`workspace_tools`, `aws_tools`, etc.)
- ✅ **Allowed**: Writing to `knowledgebase/` for persistent files
- ❌ **Forbidden**: Hardcoded workspace paths
- ❌ **Forbidden**: Full file paths as CLI arguments
- ❌ **Forbidden**: Direct OS calls (use `workspace_tools`)
- ❌ **Forbidden**: Writing to other steps' folders

**Example Template:**
```go
package main
import (
    "os"
    "path/filepath"
    "workspace_tools"
)

func main() {
    basePath := os.Args[1]  // ALWAYS first argument
    // ... use filepath.Join(basePath, "relative/path") for all files
}
```

---

## 🔒 Security

### Environment Isolation

Code execution mode uses environment variable sanitization to prevent secret leakage:

- **Whitelist-only approach**: Only safe environment variables are included
- **No secret inheritance**: DATABASE_URL, API_KEYS, tokens are NOT accessible
- **Explicit additions**: MCP_API_URL and GOWORK added explicitly when needed

**What code CAN access:**
- `PATH`, `HOME`, `USER`, `SHELL` - Safe shell variables
- `LANG`, `LC_ALL` - Locale settings  
- `MCP_API_URL` - For MCP tool calls
- `GOWORK` - For Go workspace (when using generated packages)

**What code CANNOT access:**
- `DATABASE_URL`, `API_KEY`, `SECRET`, `TOKEN`, `PASSWORD` - Any parent secrets

---

## 📖 Related Documentation

- [Workflow Orchestrator](workflow_orchestrator.md) - Overall execution system
- [Execution Agent](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/execution_only_agent.go) - Code execution instructions
- [Learning Agent](../agent_go/pkg/orchestrator/agents/workflow/todo_creation_human/learning_agent_code_execution.go) - Code pattern capture
