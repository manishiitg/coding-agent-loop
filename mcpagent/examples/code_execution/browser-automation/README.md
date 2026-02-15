# Code Execution Mode with Browser Automation

This example demonstrates how to use the MCP Agent with **code execution mode** and **browser automation** capabilities. The agent automatically writes and executes Go code to perform web research and analysis tasks using the Playwright MCP server.

## Features

- **Code Execution Mode**: The agent automatically writes Go code instead of making JSON tool calls
- **Browser Automation**: Uses `@playwright/mcp` for web browsing and data extraction
- **Automatic Code Generation**: The agent generates Go code that uses browser tools as Go functions
- **Web Research**: Automatically navigates websites, searches, and extracts information
- **Analysis Tasks**: Performs complex research and analysis using browser tools
- **Multi-turn Conversations**: Uses `AskWithHistory` for maintaining conversation context

## How It Works

1. **Code Execution Mode**: When enabled, the LLM writes Go code instead of making direct JSON tool calls
2. **Generated Go Packages**: The agent automatically generates Go packages for MCP tools (like browser automation tools)
3. **Code Execution**: Generated code is executed in an isolated workspace
4. **HTTP Server**: A local HTTP server handles API calls from the generated Go code
5. **Browser Tools**: The generated code can use Playwright tools as Go functions to:
   - Navigate to URLs
   - Search the web
   - Extract page content
   - Take snapshots
   - Interact with web elements

## Setup

1. Create `.env` file with your OpenAI API key:
   ```
   OPENAI_API_KEY=your-api-key-here
   ```

2. Initialize the Go module:
   ```bash
   cd examples/code_execution/browser-automation
   go mod tidy
   ```

3. Ensure Node.js is installed (required for `npx` to run the Playwright MCP server):
   ```bash
   node --version  # Should be v18 or higher
   ```

4. (Optional) Configure HTTP server port:
   ```bash
   export MCP_API_URL=8000  # or http://localhost:8000
   ```
   The default port is 8000. The HTTP server is required for code execution mode.

## Usage

```bash
# Run with default IPO analysis task
go run main.go

# Run with custom config file
go run main.go mcp_servers.json

# Run with custom task
go run main.go mcp_servers.json "Search for the latest AI news and summarize the top 5 articles"
```

## Default Task

The default task performs a comprehensive analysis of the last 10 popular IPOs in India:

1. **Search**: Uses browser automation to search for recent Indian IPOs
2. **Data Collection**: Navigates to relevant websites and extracts IPO information
3. **Analysis**: Identifies patterns such as:
   - Sector distribution
   - Pricing trends
   - Performance metrics
   - Market timing patterns
   - Investor response patterns
4. **Reporting**: Presents findings in a structured format

**You don't need to ask the agent to write code** - it will automatically use code execution mode when appropriate. Just give normal instructions like:

- "Search for the last 10 popular IPOs from India"
- "Get me information about recent tech IPOs"
- "Analyze trends in the IPO market"

## Code Execution Mode

In code execution mode:

- The agent **automatically writes Go code** to accomplish tasks
- Generated code uses MCP tools as **Go functions** (not JSON calls)
- Code is executed in an **isolated workspace**
- An **HTTP server** handles API calls from generated code
- All code and execution logs are saved for debugging

### Generated Files

- `generated/` - Auto-generated Go packages for MCP tools
- `workspace/` - Execution workspace with generated code files
- `logs/` - Detailed execution logs

## Example Use Cases

- **Market Research**: Analyze trends in specific markets or industries
- **Competitive Analysis**: Research competitors and market positioning
- **Data Collection**: Gather information from multiple sources
- **Content Analysis**: Extract and analyze content from websites
- **Real-time Information**: Get up-to-date information from the web
- **Web Scraping**: Automatically extract structured data from websites

## Configuration

The `mcp_servers.json` file configures the Playwright MCP server:

```json
{
  "mcpServers": {
    "playwright": {
      "command": "npx",
      "args": ["@playwright/mcp@latest"]
    }
  }
}
```

## HTTP Server

Code execution mode requires a local HTTP server to handle API calls from generated Go code:

- **Default Port**: 8000
- **Configurable**: Set `MCP_API_URL` environment variable to change port
- **Endpoints**: 
  - `/api/mcp/execute` - Execute MCP tool calls
  - `/api/custom/execute` - Execute custom tool calls
  - `/api/virtual/execute` - Execute virtual tool calls

The server starts automatically when you run the example.

## Requirements

- Go 1.24.4+
- Node.js 18+ (for npx-based MCP servers)
- OpenAI API key in `.env` file
- Internet connection (for web browsing)

## Differences from Regular Browser Automation

| Feature | Regular Browser Automation | Code Execution Mode |
|---------|---------------------------|---------------------|
| Tool Calls | Direct JSON tool calls | Go code generation |
| Execution | Immediate tool execution | Code written then executed |
| Debugging | Tool call logs | Full code + execution logs |
| Flexibility | Limited to tool capabilities | Can combine tools with custom logic |
| Performance | Faster for simple tasks | Better for complex workflows |

## Notes

- Browser automation tasks may take longer than simple API calls
- The default timeout is set to 15 minutes to allow for complex research
- The browser MCP server will automatically handle web interactions
- Some websites may have rate limiting or require authentication
- Generated code is logged for easy debugging (check `logs/browser-automation-code-execution.log`)
- Code execution logs include both the code written and execution results

## Logging

The example includes comprehensive logging:

- **LLM Logs**: `logs/llm.log` - API calls, token usage, responses
- **Agent Logs**: `logs/browser-automation-code-execution.log` - Code execution, tool calls, errors

All code written and execution results are logged with the `[CODE_EXECUTION]` prefix for easy filtering.

