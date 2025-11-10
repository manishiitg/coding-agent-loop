# Code Execution with MCP

## Overview

The code execution feature allows the LLM agent to write and execute Go code that interacts with MCP tools, custom tools, and virtual tools. Instead of directly exposing all MCP tools to the LLM, the agent operates in "code execution mode" where it:

1. **Discovers** available Go code packages via `discover_code_structure` and `discover_code_files`
2. **Writes** Go code using the `write_code` tool
3. **Executes** the code in-process using Go plugins (Linux/macOS) or AST parsing (fallback)

This approach improves context efficiency by loading tools on-demand and processing data in an execution environment.

## Architecture

### Key Components

1. **Code Generation** (`pkg/mcpcache/codegen/`)
   - Generates Go code from MCP tool definitions
   - Creates one file per tool in `generated/{server_name}_tools/`
   - Converts JSON schemas to Go structs and functions

2. **Runtime Registry** (`pkg/mcpagent/codeexec/`)
   - Global registry that maps tool names to executable functions
   - Provides `CallMCPTool()`, `CallCustomTool()`, and `CallVirtualTool()` functions
   - Initialized once per agent with all MCP clients and tools

3. **Code Execution** (`pkg/mcpagent/code_execution_tools.go`)
   - Handles `discover_code_structure`, `discover_code_files`, and `write_code` virtual tools
   - Executes Go code in-process using plugins or AST parsing
   - Captures stdout/stderr and returns execution results

## Key Files

### Core Execution Files

- **`pkg/mcpagent/code_execution_tools.go`** (900 lines)
  - Virtual tool handlers: `handleDiscoverCodeStructure()`, `handleDiscoverCodeFiles()`, `handleWriteCode()`
  - Code execution: `executeGoCode()`, `executeGoCodeAsPlugin()`, `executeGoCodeViaAST()`
  - Plugin wrapping: `wrapCodeForPlugin()` - transforms user code into plugin-compatible format

- **`pkg/mcpagent/codeexec/registry.go`** (174 lines)
  - `ToolRegistry` struct - holds MCP clients, custom tools, virtual tools
  - `InitRegistryWithVirtualTools()` - initializes global registry
  - `CallMCPTool()`, `CallCustomTool()`, `CallVirtualTool()` - runtime tool execution

### Code Generation Files

- **`pkg/mcpcache/codegen/generator.go`**
  - `GenerateServerCode()` - generates Go code for MCP server tools
  - `GenerateCustomToolCode()` - generates Go code for custom tools
  - `GenerateVirtualToolsCode()` - generates Go code for virtual tools
  - Creates one `.go` file per tool with snake_case filenames

- **`pkg/mcpcache/codegen/schema_parser.go`**
  - `ParseJSONSchemaToGoStruct()` - converts JSON schema to Go struct
  - `sanitizeFunctionName()` - converts tool names to valid Go identifiers
  - `ToolNameToSnakeCase()` - converts tool names to snake_case filenames

- **`pkg/mcpcache/codegen/templates.go`**
  - `GeneratePackageHeader()` - generates package declaration and imports
  - `GenerateStruct()` - generates Go struct code
  - `GenerateFunctionWithParams()` - generates function that calls `codeexec.CallMCPTool()`

### Integration Files

- **`pkg/mcpagent/agent.go`**
  - `UseCodeExecutionMode` field - enables/disables code execution mode
  - `InitRegistryWithVirtualTools()` call - initializes registry during agent creation
  - Tool filtering - excludes MCP tools from LLM when in code execution mode

- **`pkg/mcpagent/virtual_tools.go`**
  - Virtual tool definitions for `discover_code_structure`, `discover_code_files`, `write_code`
  - Routes virtual tool calls to handlers in `code_execution_tools.go`

- **`pkg/mcpagent/prompt/builder.go`**
  - `buildVirtualToolsSection()` - customizes system prompt for code execution mode
  - Provides instructions on how to use `discover_code_files` and `write_code`

## How It Works

### 1. Code Generation

When MCP servers are connected, the agent automatically generates Go code:

```
generated/
├── aws_tools/
│   ├── get_document.go
│   ├── list_buckets.go
│   └── ...
├── context7_tools/
│   ├── get_library_docs.go
│   └── ...
└── virtual_tools/
    ├── discover_code_files.go
    └── write_code.go
```

Each generated file contains:
- A params struct (e.g., `GetLibraryDocsParams`)
- A function that calls `codeexec.CallMCPTool()` (e.g., `GetLibraryDocs()`)

### 2. Registry Initialization

During agent initialization:
```go
codeexec.InitRegistryWithVirtualTools(
    mcpClients,      // All connected MCP clients
    customTools,     // Custom tool executors
    virtualTools,    // Virtual tool executors
    toolToServer,    // Tool name → server name mapping
    logger,
)
```

The registry is stored in a global variable and accessible to all executed code.

### 3. Code Discovery

The LLM can discover available code:
- `discover_code_structure` (no params) - Returns JSON list of all servers/tools
- `discover_code_files(server_name, tool_name)` - Returns Go source code for specific tool

### 4. Code Writing & Execution

The LLM writes Go code using `write_code`:
```go
package main

import (
    "context"
    "fmt"
    "mcp-agent/agent_go/generated/aws_tools"
)

func main() {
    ctx := context.Background()
    params := aws_tools.GetDocumentParams{
        DocumentID: "doc123",
    }
    result, err := aws_tools.GetDocument(ctx, params)
    if err != nil {
        fmt.Printf("Error: %v\n", err)
        return
    }
    fmt.Printf("Result: %s\n", result)
}
```

The execution flow:
1. **Write code** to workspace directory
2. **Wrap code** for plugin execution (captures stdout/stderr)
3. **Compile as plugin** using `go build -buildmode=plugin`
4. **Load plugin** and call exported `Run()` function
5. **Capture output** and return to LLM

### 5. Platform Support

- **Linux/macOS**: Uses Go plugin system (compiles and loads `.so` file)
- **Windows/Other**: Falls back to AST parsing (extracts and executes tool calls directly)

## Usage Example

### Agent Configuration

```go
agent := mcpagent.NewAgent(
    mcpagent.WithCodeExecutionMode(true), // Enable code execution mode
    // ... other options
)
```

### LLM Interaction Flow

1. **Discover available tools:**
   ```
   discover_code_structure() → Returns list of all servers and tools
   ```

2. **Get specific tool code:**
   ```
   discover_code_files(server_name="aws", tool_name="GetDocument") → Returns Go code
   ```

3. **Write and execute code:**
   ```
   write_code(code="...") → Executes and returns output (filename is auto-generated)
   ```

## Generated Code Structure

### Example: `generated/aws_tools/get_document.go`

```go
package aws_tools

import (
    "context"
    "mcp-agent/agent_go/pkg/mcpagent/codeexec"
)

type GetDocumentParams struct {
    DocumentID string `json:"documentID"`
}

func GetDocument(ctx context.Context, params GetDocumentParams) (string, error) {
    args := make(map[string]interface{})
    args["documentID"] = params.DocumentID
    return codeexec.CallMCPTool(ctx, "get-document", args)
}
```

## Testing

Test file: `cmd/testing/code-execution-test.go`

Tests verify:
- Code generation for MCP tools
- Registry initialization
- Tool discovery
- Code execution with real MCP tool calls

Run tests:
```bash
go run main.go test code-execution --log-file logs/code_execution_test.log
```

## Benefits

1. **Context Efficiency**: Only exposes 3 virtual tools instead of 100+ MCP tools
2. **On-Demand Loading**: Tools are discovered and loaded as needed
3. **Type Safety**: Generated Go code provides type-safe interfaces
4. **Execution Environment**: Code can process data, make multiple tool calls, and combine results
5. **Error Handling**: Execution errors are captured and returned to LLM for debugging

## Limitations

1. **Platform Support**: Go plugins only work on Linux/macOS (Windows uses AST fallback)
2. **Plugin Isolation**: Plugins run in the same process but with separate output capture
3. **Code Validation**: No compile-time validation before execution (errors returned at runtime)

## Future Enhancements

- Type-safe return values (if MCP provides output schemas)
- Code formatting and linting
- Incremental code generation (only regenerate changed tools)
- Better error messages with code context
- Support for Go modules in workspace

