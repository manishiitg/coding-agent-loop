# Code Execution Mode with Custom Tools

This example demonstrates how to use **custom tools** in **code execution mode**. In this mode, the agent generates and executes Go code instead of making direct JSON tool calls. Custom tools are accessible via generated Go code through the HTTP API.

## Key Features

1. **Code Execution Mode**: The agent writes and executes Go code automatically
2. **Custom Tools**: Register custom Go functions as tools that can be called via generated code
3. **HTTP Server**: Local HTTP server handles API calls from generated Go code
4. **Multi-Turn Conversations**: Uses `AskWithHistory` for maintaining conversation context

## How It Works

### Code Execution Mode

When code execution mode is enabled:
- The agent only sees virtual tools: `discover_code_files` and `write_code`
- MCP tools and custom tools are **NOT** directly exposed to the LLM
- The agent generates Go code that calls tools via HTTP API
- Generated code is executed in an isolated environment

### Custom Tools in Code Execution Mode

Custom tools work differently in code execution mode:

1. **Registration**: Custom tools are registered using `agent.RegisterCustomTool()`
2. **Code Generation**: Go code is automatically generated for custom tools
3. **Execution**: Generated code calls custom tools via `/api/custom/execute` HTTP endpoint
4. **Access**: Custom tools are accessible through generated Go functions, not direct LLM tool calls

### Example Custom Tools

This example includes four custom tools:

1. **calculator**: Performs mathematical operations (add, subtract, multiply, divide, power, sqrt)
2. **format_text**: Formats text (uppercase, lowercase, reverse, title case)
3. **get_weather**: Gets simulated weather data for a location
4. **count_text**: Counts characters, words, sentences, or paragraphs in text

## Setup

### Prerequisites

- Go 1.24.4 or later
- Docker (for MCP servers)
- Python with `uvx` (for time server)
- OpenAI API key

### Environment Variables

Create a `.env` file or set environment variables:

```bash
export OPENAI_API_KEY="your-api-key-here"
export MCP_API_URL="8000"  # Optional: HTTP server port (default: 8000)
```

### Installation

1. Navigate to this directory:
```bash
cd mcpagent/examples/code_execution/custom_tools
```

2. Install dependencies:
```bash
go mod tidy
```

3. Run the example:
```bash
go run main.go
```

## Usage

The example will:
1. Start an HTTP server on port 8000 (or the port specified in `MCP_API_URL`)
2. Register custom tools
3. Process example questions that trigger code execution
4. Generate and execute Go code that uses custom tools

### Example Questions

The example includes questions like:
- "Calculate 15 multiplied by 23, then format the result as text in uppercase"
- "Get the weather for San Francisco in fahrenheit, then count the number of words in the weather description"
- "Calculate the square root of 144, then format 'The answer is' followed by the result in title case"

These questions will cause the agent to:
1. Generate Go code that calls custom tools
2. Execute the code via HTTP API
3. Return the results

## How Custom Tools Work in Code Execution Mode

### Registration

```go
agent.RegisterCustomTool(
    "calculator",
    "Performs basic mathematical operations",
    calculatorParams,  // JSON schema
    calculatorTool,    // Execution function
    "utility",         // Category
)
```

### Generated Code

The agent generates Go code like:

```go
// Generated function for calculator tool
func calculator(params CalculatorParams) string {
    payload := map[string]interface{}{
        "server": "custom",
        "tool":   "calculator",
        "args":   params,
    }
    return callAPI("/api/custom/execute", payload)
}
```

### Execution Flow

1. Agent receives question
2. Agent generates Go code using `write_code` tool
3. Generated code calls custom tools via HTTP API
4. HTTP server routes to `/api/custom/execute`
5. Custom tool execution function is called
6. Result is returned to generated code
7. Generated code returns final result

## Differences from Normal Mode

| Feature | Normal Mode | Code Execution Mode |
|---------|-------------|---------------------|
| Custom Tools | Directly exposed to LLM | Accessible via generated Go code |
| Tool Calls | JSON tool calls | Go code execution |
| Execution | Synchronous | Via HTTP API |
| Code Generation | Not required | Required for all tools |

## Logging

Logs are written to:
- `logs/llm.log`: LLM API calls and token usage
- `logs/custom-tools-code-execution.log`: Agent operations, code generation, and execution

## Troubleshooting

### Port Already in Use

If port 8000 is already in use, set a different port:
```bash
export MCP_API_URL="8001"
go run main.go
```

### Custom Tool Not Found

Ensure custom tools are registered **before** calling `agent.Ask()` or `agent.AskWithHistory()`.

### Code Execution Errors

Check the agent log file for detailed error messages. Common issues:
- HTTP server not running
- Invalid tool parameters
- Network connectivity issues

## Next Steps

- Add more custom tools for your specific use case
- Combine custom tools with MCP server tools
- Use custom tools for domain-specific operations
- Implement complex workflows using multiple custom tools

