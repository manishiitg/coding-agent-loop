# Simple Code Execution Example

This example demonstrates **Code Execution Mode** in its simplest form, where the LLM writes and executes Go code instead of making JSON-based tool calls. The LLM can use MCP tools as native Go functions.

**This is the basic example without folder guards or additional security features.**

## What is Code Execution Mode?

Code Execution Mode is a powerful feature that allows the LLM to:
- **Write Go code** instead of making individual tool calls
- **Use MCP tools as Go functions** - tools are auto-generated as Go packages
- **Execute complex logic** - loops, conditionals, data transformations
- **Chain multiple operations** - multiple tool calls in a single execution
- **Type safety** - Go compiler enforces correct types

## How It Works

1. **Code Generation**: When MCP servers connect, Go wrapper code is auto-generated in `generated/<server>_tools/`
2. **Discovery**: LLM calls `discover_code_files` to see available packages and functions
3. **Code Writing**: LLM writes Go code importing the generated packages
4. **Execution**: LLM calls `write_code(go_source, args?)` to execute the code
5. **Validation**: Code is validated via AST analysis for security
6. **Isolation**: Code runs in a temporary workspace

## Methods Demonstrated

- `mcpagent.WithCodeExecutionMode(true)` - Enable code execution mode
- `mcpagent.WithCodeExecutionMode(true)` - Enable code execution mode
- `agent.Ask(ctx, question)` - LLM will use code execution tools

## Key Features

### 1. **Auto-Generated Go Packages**

MCP tools are automatically converted to Go packages:

```
generated/fetch_tools/
├── fetch_url.go
└── api_client.go

generated/time_tools/
├── get_current_time.go
└── api_client.go
```

### 2. **Virtual Tools**

In code execution mode, only these tools are available to the LLM:
- `discover_code_files` - Discover available packages and functions
- `write_code` - Execute Go code

Regular MCP tools are NOT directly available - they must be used via generated Go code.

### 3. **Security Features**

- **AST Validation**: Code is analyzed to block dangerous operations
- **Folder Guards**: File operations restricted to specified directories
- **Isolated Execution**: Each code execution runs in a temporary workspace
- **Timeout Protection**: Code execution has a timeout (default 30s)

### 4. **Workspace Tools**

The `workspace_tools` package provides safe file operations:
- `ReadWorkspaceFile` - Read files within workspace
- `UpdateWorkspaceFile` - Update files within workspace
- `ListWorkspaceFiles` - List files in workspace

## Setup

1. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Run the example:
   ```bash
   go run main.go
   ```

4. Or with custom questions:
   ```bash
   go run main.go mcp_servers.json "Write Go code to fetch a URL and save it"
   ```

## Important: HTTP Server Requirement

**Code execution mode requires an HTTP server running** (default port 8000).

The example automatically starts an HTTP server that handles API calls from generated Go code:
- **Port**: `8000` (default, configurable via `MCP_API_URL` environment variable)
- **Endpoints**:
  - `/api/mcp/execute` - For MCP tool execution
  - `/api/custom/execute` - For custom tool execution
  - `/api/virtual/execute` - For virtual tool execution

**Configuring the Port:**

You can change the port by setting the `MCP_API_URL` environment variable:
```bash
# Use a different port
export MCP_API_URL=http://localhost:9000
go run main.go

# Or just the port number
export MCP_API_URL=:9000
go run main.go
```

**Note:** If port 8000 is already in use, you'll get a "bind: address already in use" error. Either:
- Stop the service using port 8000, or
- Set `MCP_API_URL` to use a different port

The generated Go code makes HTTP POST requests to these endpoints to execute tools. The server is started automatically when you run the example and shut down when the program exits.

## Example Usage

The example demonstrates several scenarios. **You don't need to ask the agent to write code** - it will automatically use code execution mode when appropriate:

1. **Simple Request**: "Get me the documentation for React library"
   - Agent automatically writes Go code to fetch the docs

2. **Complex Thinking**: "Think through the problem: How can I improve the performance of a web application?"
   - Agent uses sequential-thinking server via generated Go code

3. **Multi-Server Operations**: "Get React documentation and analyze the key concepts using sequential thinking"
   - Agent combines multiple MCP servers in a single Go program

4. **Natural Instructions**: "Use multiple MCP servers to get information about React and then think through how to use it in a project"
   - Agent automatically decides when to use code execution mode

## Code Execution Flow

### Step 1: LLM Discovers Available Code

```go
// LLM calls discover_code_files
// Receives JSON with available packages:
{
  "servers": [{
    "name": "fetch",
    "package": "fetch_tools",
    "tools": ["FetchURL"]
  }],
  "workspace_tools": {
    "package": "workspace_tools",
    "tools": ["ReadWorkspaceFile", "UpdateWorkspaceFile"]
  }
}
```

### Step 2: LLM Writes Go Code

```go
package main

import (
    "fmt"
    "fetch_tools"
    "workspace_tools"
)

func main() {
    // Use MCP tool as Go function
    result := fetch_tools.FetchURL(
        fetch_tools.FetchURLParams{
            URL: "https://example.com",
        },
    )
    
    // Check for errors (functions return strings)
    if strings.HasPrefix(result, "Error:") {
        fmt.Printf("Error: %s\n", result)
        return
    }
    
    // Save to workspace
    output := workspace_tools.UpdateWorkspaceFile(
        workspace_tools.UpdateWorkspaceFileParams{
            Filepath: "example.html",
            Content:  result,
        },
    )
    
    fmt.Printf("Saved: %s\n", output)
}
```

### Step 3: LLM Executes Code

```go
// LLM calls write_code with the Go source
write_code(
    code="package main\nimport...",
    args=[] // Optional CLI arguments
)
```

The generated Go code makes HTTP POST requests to `http://localhost:8000/api/mcp/execute` (or other endpoints) to execute tools. This requires the HTTP server to be running.

## Security & Validation

### Forbidden Operations

The AST validator blocks:
- ❌ Direct file I/O: `os.ReadFile`, `os.WriteFile`, `os.Create`
- ❌ Dangerous imports: `io/ioutil`, `os/exec`
- ❌ Path traversal: `../`, absolute paths outside workspace
- ❌ System commands: `exec.Command`, `syscall`

### Allowed Operations

- ✅ Generated tool packages: `fetch_tools`, `time_tools`, etc.
- ✅ Workspace tools: `workspace_tools` package
- ✅ Standard library: `fmt`, `strings`, `encoding/json`, `time`
- ✅ Relative paths within workspace

### Folder Guard (Not Used in This Example)

This simple example does not use folder guards. For folder guard examples, see `with_folder_guards/`.

Folder guards are not used in this simple example. All file operations use the default workspace directory.

## Error Handling

Tool functions return only `string` (no error return value):
- **Success**: Returns result as string
- **Error**: Returns string with "Error:" prefix

Always check output:
```go
result := fetch_tools.FetchURL(params)
if strings.HasPrefix(result, "Error:") {
    // Handle error
    fmt.Printf("Error: %s\n", result)
    return
}
// Use result
fmt.Printf("Success: %s\n", result)
```

## Generated Code Structure

```
generated/
├── fetch_tools/
│   ├── fetch_url.go          # Tool function
│   └── api_client.go          # HTTP client
├── time_tools/
│   ├── get_current_time.go    # Tool function
│   └── api_client.go          # HTTP client
└── index.go                   # Package index
```

## Workspace Structure

```
workspace/
└── code_<timestamp>/          # Temporary execution workspace
    ├── main.go                 # Generated main.go
    └── go.work                 # Go workspace file
```

## Logs

The example creates log files in the `logs/` directory:
- `llm.log` - LLM API calls and token usage
- `code-execution.log` - Agent operations, code generation, and execution

## Key Differences from Standard Mode

| Feature | Standard Mode | Code Execution Mode |
|---------|--------------|---------------------|
| **Tool Calls** | JSON tool calls | Go code execution |
| **Available Tools** | All MCP tools directly | Only `discover_code_files`, `write_code` |
| **MCP Tools** | Direct LLM tool calls | Via generated Go packages |
| **Complex Logic** | Limited to tool chaining | Full Go language features |
| **Type Safety** | Runtime validation | Compile-time validation |
| **Performance** | Multiple API calls | Single code execution |

## When to Use Code Execution Mode

**Use Code Execution Mode when:**
- ✅ You need complex logic (loops, conditionals, data transformations)
- ✅ You want to chain multiple operations efficiently
- ✅ You need type safety and compile-time validation
- ✅ You want to reduce API calls (single execution vs multiple)

**Use Standard Mode when:**
- ✅ Simple tool calls are sufficient
- ✅ You want direct tool visibility to the LLM
- ✅ You need faster iteration (no code generation step)
- ✅ You prefer JSON-based tool calls

## Next Steps

- Explore the `generated/` directory to see auto-generated code
- Check `workspace/` for code execution workspaces
- Review logs for detailed execution traces
- Try writing more complex Go programs
- Combine multiple MCP servers in a single program

## Troubleshooting

### "package not found"
- Check that `generated/` directory exists
- Restart the agent to regenerate code

### "forbidden import"
- Use `workspace_tools` package instead of `os` or `io/ioutil`
- Check error message for correct alternative

### "path outside boundary"
- Use relative paths within workspace
- Avoid `../` or absolute paths

### "validation failed"
- Review error message for blocked operation
- Use suggested alternative (usually `workspace_tools`)

## Documentation

For more details, see:
- [Code Execution Agent Documentation](../../docs/code_execution_agent.md)
- [Folder Guard Documentation](../../docs/folder_guard.md)

