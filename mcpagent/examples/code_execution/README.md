# Code Execution Mode Examples

This directory contains multiple examples demonstrating **Code Execution Mode**, where the LLM writes and executes Go code instead of making JSON-based tool calls.

## Available Examples

### 1. **[simple/](simple/)** - Basic Code Execution

The simplest code execution example without folder guards or additional security features.

**Features:**
- Basic code execution mode setup
- Multiple MCP servers (playwright, sequential-thinking, context7, etc.)
- Simple Go code generation and execution
- No folder guards

**Use this when:**
- You want to understand the basics of code execution mode
- You don't need file system restrictions
- You want to see how MCP tools become Go functions

### 2. **[browser-automation/](browser-automation/)** - Code Execution with Browser Automation

Code execution mode combined with browser automation using Playwright MCP server.

**Features:**
- Code execution mode with browser automation
- Playwright MCP server for web browsing
- Automatic Go code generation for web tasks
- Multi-turn conversations with `AskWithHistory`
- Default task: IPO analysis from Indian financial websites

**Use this when:**
- You want to combine code execution with web automation
- You need to perform complex web research tasks
- You want to see how browser tools work as Go functions
- You're building web scraping or research automation

### 3. **[multi-mcp-server/](multi-mcp-server/)** - Code Execution with Tool Filtering

Code execution mode with multiple MCP servers and selective tool filtering.

**Features:**
- Code execution mode with tool filtering
- Multiple MCP servers (playwright, sequential-thinking, context7, aws-knowledge-mcp, google-sheets, gmail, everything)
- Selective tool access:
  - Specific tools from servers (e.g., only `read_email` and `search_emails` from gmail)
  - All tools from other servers
- Multi-turn conversations with `AskWithHistory`
- Demonstrates how filters work in code execution mode

**Use this when:**
- You want to restrict which tools are available to the agent
- You need to use multiple MCP servers with selective access
- You want to understand how tool filtering works in code execution mode
- You're building applications that need controlled tool access

### 4. **[custom_tools/](custom_tools/)** - Code Execution with Custom Tools

Code execution mode with custom Go functions registered as tools.

**Features:**
- Code execution mode with custom tools
- Register custom Go functions as tools
- Custom tools accessible via generated Go code
- HTTP API for custom tool execution
- Example custom tools: calculator, text formatter, weather simulator, text counter
- Multi-turn conversations with `AskWithHistory`

**Use this when:**
- You want to add domain-specific tools not available in MCP servers
- You need custom business logic as tools
- You want to see how custom tools work in code execution mode
- You're building applications with specialized tool requirements

### 5. **[with_folder_guards/](with_folder_guards/)** - Code Execution with Security

Code execution with folder guards for secure file operations.

**Features:**
- Code execution mode with folder guards
- Restricted file operations to workspace directory
- Security boundaries for read/write operations
- AST validation for code safety

**Use this when:**
- You need to restrict file system access
- You want to ensure code only accesses specific directories
- You're building production applications

## What is Code Execution Mode?

Code Execution Mode allows the LLM to:
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
6. **Isolation**: Code runs in a temporary workspace (with optional folder guards)

## Key Features

### Auto-Generated Go Packages

MCP tools are automatically converted to Go packages:

```
generated/context7_tools/
├── get_library_docs.go
└── api_client.go

generated/sequential_thinking_tools/
├── think.go
└── api_client.go
```

### Virtual Tools

In code execution mode, only these tools are available to the LLM:
- `discover_code_files` - Discover available packages and functions
- `write_code` - Execute Go code

Regular MCP tools and custom tools are NOT directly available - they must be used via generated Go code.

### Security Features

- **AST Validation**: Code is analyzed to block dangerous operations
- **Folder Guards** (optional): File operations restricted to specified directories
- **Isolated Execution**: Each code execution runs in a temporary workspace
- **Timeout Protection**: Code execution has a timeout (default 30s)

## Quick Start

1. Choose an example directory (e.g., `simple/`, `custom_tools/`, or `with_folder_guards/`)
2. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```
3. Install dependencies:
   ```bash
   cd simple  # or custom_tools, with_folder_guards, etc.
   go mod tidy
   ```
4. Run the example:
   ```bash
   go run main.go
   ```

## Example Code Pattern

```go
package main

import (
    "fmt"
    "strings"
    "context7_tools"
    "workspace_tools"
)

func main() {
    // Use MCP tool as Go function
    result := context7_tools.GetLibraryDocs(
        context7_tools.GetLibraryDocsParams{
            Library: "react",
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
            Filepath: "react_docs.md",
            Content:  result,
        },
    )
    
    fmt.Printf("Saved: %s\n", output)
}
```

## Documentation

For more details, see:
- [Code Execution Agent Documentation](../../docs/code_execution_agent.md)
- [Folder Guard Documentation](../../docs/folder_guard.md)

## Next Steps

- Explore the `generated/` directory to see auto-generated code
- Check `workspace/` for code execution workspaces
- Review logs for detailed execution traces
- Try writing more complex Go programs
- Combine multiple MCP servers in a single program

