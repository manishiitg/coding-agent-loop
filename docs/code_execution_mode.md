# Code Execution Mode

## Overview

Code Execution Mode allows the Execution Agent to generate and run Go code instead of using MCP tools directly. This enables complex operations, batch processing, and reusable code patterns.

## Critical Rule: WorkspacePath as CLI Argument

**🚨 ALWAYS pass WorkspacePath as the first CLI argument (os.Args[1])**

### Why This Matters

- Workspace paths change between iterations: `workspace/runs/run-1/execution/step-1` → `workspace/runs/run-2/execution/step-1`
- Hardcoded paths in Go code break when reused across iterations
- Passing path via CLI args makes code reusable - only the argument changes

### Two-Step Process

**1. Tool Call (write_code):**
- ✅ **Correct**: `args=["{{.WorkspacePath}}", "other_var1", "other_var2"]` (ONLY base path)
- ❌ **Wrong**: `args=["{{.WorkspacePath}}/step-1/file.json", ...]` (passing full file paths)
- ❌ **Wrong**: `args=["other_var1"]` (missing workspace path)

**2. Go Code Content:**
- ✅ **Correct**: `workspacePath := os.Args[1]` then use `filepath.Join(workspacePath, "step-N/file.json")`
- ❌ **Wrong**: `filepath := "workspace/runs/run-1/execution/step-1"` (hardcoded path)
- ❌ **Wrong**: Passing full file paths as CLI arguments

### Path Handling (CRITICAL)

**Base Path vs Relative Paths:**
- **Base Path**: `os.Args[1]` is the base execution workspace (e.g., `Workflow/runs/iteration-11/execution`)
- **Context Dependencies**: Use relative paths like `step-1/step_1_output.json` (NOT full paths)
- **File Construction**: `filepath.Join(basePath, relativePath)` for ALL file operations

**Example:**
- Base: `os.Args[1]` → `Workflow/runs/iteration-11/execution`
- Relative: `step-1/credentials.json`
- Full: `filepath.Join(basePath, "step-1/credentials.json")` → `Workflow/runs/iteration-11/execution/step-1/credentials.json`

### Example

**Tool Call:**
```
write_code(
  code="...",
  args=["{{.WorkspacePath}}", "userId"]
)
```

**Go Code:**
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

## Variable Handling

- **Pass**: All variables via `args` parameter: `args=["{{.WorkspacePath}}", "value1", "value2"]`
- **Access**: Read from `os.Args[1]` (workspace path), `os.Args[2]`, `os.Args[3]`, etc.
- **NO Hardcoding**: Never hardcode variable values OR workspace paths in Go code

## Packages & Operations

- **Packages**: Import generated tool packages (`aws_tools`, `workspace_tools`, `google_sheets_tools`, etc.)
- **File Ops**: Always use `workspace_tools` for file operations
- **Paths**: Always use `os.Args[1]` for workspace paths, never hardcode directory paths

## Common Mistakes to Avoid

❌ **Passing full file paths as CLI arguments:**
```
// WRONG - passing full file paths
args=["Workflow/runs/iteration-11/execution", "Workflow/runs/iteration-11/execution/step-1/file.json"]
```

✅ **Correct - pass only base path, use relative paths in code:**
```
// Correct tool call
args=["Workflow/runs/iteration-11/execution", "userId"]

// Correct code
basePath := os.Args[1]
filePath := filepath.Join(basePath, "step-1/file.json")
```

❌ **Hardcoding iteration-specific paths:**
```go
// WRONG - hardcoded iteration number
filepath := "Workflow/runs/iteration-11/execution/step-1/file.json"
```

✅ **Correct - use base path + relative path:**
```go
// Correct - works for any iteration
basePath := os.Args[1]
filePath := filepath.Join(basePath, "step-1/file.json")
```

## Code Generation Checklist

Before generating Go code:

1. **🚨 WorkspacePath as FIRST CLI argument**: ALWAYS pass ONLY base WorkspacePath as `os.Args[1]` - NEVER pass full file paths
2. **🚨 Use Relative Paths**: ALL file paths in code must use `filepath.Join(basePath, "step-N/file.json")`
3. Check learnings for error patterns to avoid
4. Use correct patterns from successful code examples (but replace hardcoded paths with `filepath.Join()`)
5. Verify Go syntax and imports
6. Parse tool responses correctly

## Learning Integration

When learnings contain Go code with hardcoded paths:
- Extract the code pattern
- Replace hardcoded paths with `os.Args[1]`
- Keep the logic and error handling patterns
- Update variable access to use CLI args

## Files

- **Execution Agent**: `execution_only_agent.go` - Contains code execution mode instructions
- **Learning Agent**: `learning_agent_code_execution.go` - Captures Go code patterns for reuse

