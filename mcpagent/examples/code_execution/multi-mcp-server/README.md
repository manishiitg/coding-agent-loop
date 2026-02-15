# Code Execution Mode with Multi-MCP Server and Filters

This example demonstrates how to use the MCP Agent with **code execution mode**, **multiple MCP servers**, and **tool filtering**. The agent automatically writes and executes Go code while only having access to filtered tools from specific servers.

## Features

- **Code Execution Mode**: The agent automatically writes Go code instead of making JSON tool calls
- **Tool Filtering**: Selectively enable specific tools or entire servers
- **Multiple MCP Servers**: Uses multiple MCP servers simultaneously (playwright, sequential-thinking, context7, aws-knowledge-mcp, gmail, everything)
- **Selective Tool Access**: 
  - Only specific tools from gmail (read_email, search_emails)
  - All tools from other servers (playwright, sequential-thinking, context7, aws-knowledge-mcp)
  - google-sheets is excluded from the filter
- **Automatic Code Generation**: The agent generates Go code that uses filtered tools as Go functions
- **Multi-turn Conversations**: Uses `AskWithHistory` for maintaining conversation context

## How It Works

### Tool Filtering

The example demonstrates two types of filtering:

1. **Specific Tool Filtering** (`WithSelectedTools`):
   ```go
   mcpagent.WithSelectedTools([]string{"gmail:read_email", "gmail:search_emails"})
   ```
   - Only allows specific tools from a server
   - Format: `"server:tool_name"`
   - Example: Only `read_email` and `search_emails` from gmail are available

2. **Server-Level Filtering** (`WithSelectedServers`):
   ```go
   mcpagent.WithSelectedServers([]string{"playwright", "sequential-thinking", "context7", "aws-knowledge-mcp"})
   ```
   - Allows ALL tools from specified servers
   - Example: All tools from playwright, sequential-thinking, context7, aws-knowledge-mcp are available
   - Note: google-sheets is excluded from the filter

### Filter Behavior

- **Combined Filters**: When both `WithSelectedTools` and `WithSelectedServers` are used:
  - Specific tools take precedence (e.g., gmail only has read_email and search_emails)
  - Server-level filters apply to other servers (all tools from those servers)
  - Tools not mentioned in either filter are excluded

- **In Code Execution Mode**: 
  - Only filtered tools are available to the agent
  - Generated Go code can only use filtered tools
  - The agent automatically discovers which tools are available

## Setup

### Prerequisites

1. **Go 1.24.4 or later**
2. **OpenAI API Key**: Set `OPENAI_API_KEY` environment variable
3. **MCP Servers**: Ensure required MCP servers are installed and configured

### Installation

1. Navigate to the example directory:
   ```bash
   cd mcpagent/examples/code_execution/multi-mcp-server
   ```

2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Configure MCP servers in `mcp_servers.json` (already configured for this example)

4. Set environment variables:
   ```bash
   export OPENAI_API_KEY="your-api-key-here"
   # Optional: Set custom port for HTTP server
   export MCP_API_URL="8000"  # or "http://localhost:8000"
   ```

### Running the Example

```bash
go run main.go
```

The example will:
1. Start an HTTP server on port 8000 (or the port specified in `MCP_API_URL`)
2. Connect to multiple MCP servers
3. Apply tool filters (only specific gmail tools, all tools from other servers)
4. Process example questions using code execution mode
5. Generate and execute Go code that uses filtered tools

## Example Usage

The example demonstrates several scenarios. **You don't need to ask the agent to write code** - it will automatically use code execution mode when appropriate:

1. **Multi-Server Research**: "Research cloud computing trends using browser automation, analyze the findings with sequential thinking, and access AWS documentation for relevant services"
   - Agent automatically writes Go code to:
     - Use playwright for web browsing
     - Use sequential-thinking for analysis
     - Use aws-knowledge-mcp for AWS documentation
   - Only filtered tools are available

2. **Documentation Analysis**: "Get React documentation from context7 and use sequential thinking to analyze the key concepts"
   - Agent automatically writes Go code to:
     - Fetch React docs from context7
     - Analyze using sequential-thinking
   - Only filtered tools are available

## Filtering Examples

### Example 1: Only Specific Gmail Tools

```go
mcpagent.WithSelectedTools([]string{"gmail:read_email", "gmail:search_emails"})
```

**Result**: Only `read_email` and `search_emails` from gmail are available. Other gmail tools (like `send_email`) are excluded.

### Example 2: All Tools from Multiple Servers

```go
mcpagent.WithSelectedServers([]string{"playwright", "sequential-thinking", "context7"})
```

**Result**: All tools from playwright, sequential-thinking, and context7 are available.

### Example 3: Combined Filtering

```go
mcpagent.WithSelectedTools([]string{"gmail:read_email", "gmail:search_emails"}),
mcpagent.WithSelectedServers([]string{"playwright", "sequential-thinking", "context7"}),
```

**Result**: 
- Gmail: Only `read_email` and `search_emails`
- Playwright, Sequential-thinking, Context7: All tools
- Other servers: Excluded

## HTTP Server Configuration

The example starts an HTTP server that handles tool execution requests from generated Go code:

- **Default Port**: 8000
- **Configurable**: Set `MCP_API_URL` environment variable to change the port
- **Address**: `127.0.0.1:8000` (localhost only, for security)

The server automatically shuts down when the program exits.

## Logging

The example creates two log files in the `logs/` directory:

1. **`llm.log`**: Logs LLM API calls, token usage, and responses
2. **`multi-mcp-server-code-execution.log`**: Logs agent operations, MCP connections, tool execution, and code generation

Logs are cleared at the start of each run for easier debugging.

## Code Execution Details

### Generated Code Structure

When the agent writes Go code, it:
1. Imports generated packages for filtered MCP tools
2. Uses Go functions that correspond to filtered tools
3. Makes HTTP requests to the local server for tool execution
4. Processes and returns results

### Filtered Tool Discovery

The agent automatically:
- Discovers which tools are available based on filters
- Generates Go packages only for filtered tools
- Ensures generated code only uses filtered tools

## Troubleshooting

### Issue: "Tool not available" errors

**Solution**: Check that the tool is included in your filter configuration. Tools not in `WithSelectedTools` or `WithSelectedServers` are excluded.

### Issue: HTTP server port conflict

**Solution**: Set `MCP_API_URL` to a different port:
```bash
export MCP_API_URL="8001"
```

### Issue: MCP server connection errors

**Solution**: 
- Verify MCP servers are installed and configured correctly
- Check `mcp_servers.json` configuration
- Ensure required environment variables are set for MCP servers

## Customization

### Adding More Filters

To add more specific tool filters:

```go
mcpagent.WithSelectedTools([]string{
    "gmail:read_email",
    "gmail:search_emails",
    "playwright:navigate",  // Only navigate from playwright
    "context7:search",      // Only search from context7
}),
```

### Adding More Servers

To include all tools from additional servers:

```go
mcpagent.WithSelectedServers([]string{
    "playwright",
    "sequential-thinking",
    "context7",
    "aws-knowledge-mcp",
    "new-server",  // Add new server here
}),
```

Note: google-sheets is excluded from the filter in this example.

### Removing Filters

To disable filtering and use all available tools:

```go
// Remove both WithSelectedTools and WithSelectedServers
agent, err := mcpagent.NewAgent(
    ctx,
    llmModel,
    configPath,
    mcpagent.WithLogger(agentLogger),
    mcpagent.WithCodeExecutionMode(true),
    // No filters - all tools available
)
```

## See Also

- [Simple Code Execution Example](../simple/README.md) - Basic code execution without filters
- [Browser Automation Example](../browser-automation/README.md) - Code execution with browser automation
- [Main README](../../README.md) - Overview of all examples

