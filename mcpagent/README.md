# MCP Agent - Go Library

[![Go Version](https://img.shields.io/badge/Go-1.24.4-blue.svg)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A **production-ready Go library** for building MCP (Model Context Protocol) agents that connect to multiple MCP servers and execute tools using LLMs. This is a fully independent package that can be used in any Go application.

## 🎯 What is MCP Agent?

MCP Agent is a Go library that provides a complete framework for building AI agents that interact with MCP servers. It handles:

- **Multi-Server MCP Connections**: Connect to multiple MCP servers simultaneously (HTTP, SSE, stdio protocols)
- **LLM Integration**: Works with OpenAI, AWS Bedrock, Google Vertex AI, and other LLM providers
- **Tool Execution**: Automatic tool discovery, execution, and result handling (with optional parallel execution)
- **Code Execution Mode**: Execute Go code instead of JSON tool calls for complex workflows
- **Tool Search Mode**: Dynamic tool discovery for large tool catalogs - LLM searches and loads tools on-demand
- **Smart Routing** (DEPRECATED): Dynamically filter tools based on conversation context
- **Context Offloading**: Automatically offload large tool outputs to filesystem to prevent context window overflow
- **Structured Output**: Get structured data from LLM responses using fixed conversion or tool-based methods
- **Custom Tools**: Register your own tools with the agent for extended functionality
- **Observability**: Built-in tracing with Langfuse support
- **Caching**: Intelligent caching of MCP server metadata and tool definitions
- **Node.js SDK**: Official TypeScript/JavaScript SDK with gRPC streaming support

## 🚀 Quick Start

### Installation

```bash
# Add to your go.mod
go get mcpagent

# Or use replace directive for local development
replace mcpagent => ../mcpagent
```

### Basic Usage

```go
package main

import (
    "context"
    "time"
    
    mcpagent "github.com/manishiitg/mcpagent/agent"
    "github.com/manishiitg/mcpagent/llm"
    "github.com/manishiitg/multi-llm-provider-go/pkg/adapters/openai"
)

func main() {
    // Initialize LLM
    llmModel, err := llm.InitializeLLM(llm.Config{
        Provider: llm.ProviderOpenAI,
        ModelID:  openai.ModelGPT41,
        APIKeys: &llm.ProviderAPIKeys{
            OpenAI: &openAIKey,
        },
    })
    
    // Create agent
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    
    agent, err := mcpagent.NewAgent(
        ctx,
        llmModel,
        "",              // server name (empty = all servers)
        "mcp_servers.json", // MCP config path
        openai.ModelGPT41,       // model ID
        nil,             // tracer (optional)
        "",              // trace ID
        nil,             // logger (optional)
    )
    
    // Ask a question
    response, err := agent.Ask(ctx, "What tools are available?")
    fmt.Println(response)
}
```

See [examples/](examples/) for complete working examples:

- **[basic/](examples/basic/)** - Basic agent setup with single MCP server
- **[multi-turn/](examples/multi-turn/)** - Multi-turn conversations with history
- **[multi-mcp-server/](examples/multi-mcp-server/)** - Connect to multiple MCP servers
- **[browser-automation/](examples/browser-automation/)** - Browser automation with Playwright
- **[structured_output/](examples/structured_output/)** - Structured output examples
  - **[fixed/](examples/structured_output/fixed/)** - Fixed conversion model (2 LLM calls)
  - **[tool/](examples/structured_output/tool/)** - Tool-based model (1 LLM call)
- **[custom_tools/](examples/custom_tools/)** - Register and use custom tools
- **[code_execution/](examples/code_execution/)** - Code execution mode examples
  - **[simple/](examples/code_execution/simple/)** - Basic code execution (no folder guards)
  - **[browser-automation/](examples/code_execution/browser-automation/)** - Code execution with browser automation
  - **[multi-mcp-server/](examples/code_execution/multi-mcp-server/)** - Code execution with tool filtering
  - **[custom_tools/](examples/code_execution/custom_tools/)** - Custom tools in code execution mode
- **[tool_search/](examples/tool_search/)** - Tool search mode for dynamic tool discovery
- **[nodejs-sdk/](examples/nodejs-sdk/)** - Node.js SDK examples (see below)

## 🟢 Node.js SDK

The official Node.js/TypeScript SDK provides a simple interface for building MCP agents in JavaScript/TypeScript applications. The SDK communicates with the Go server via **gRPC over Unix sockets** for low-latency, bidirectional streaming.

### Installation

```bash
npm install @mcpagent/node
```

### Basic Usage

```typescript
import { MCPAgent } from '@mcpagent/node';

const agent = new MCPAgent({
  serverOptions: {
    mcpConfigPath: './mcp_servers.json',
    logLevel: 'info',
  },
});

// Initialize with your LLM provider
await agent.initialize({
  provider: 'openai',
  modelId: 'gpt-4o',
  apiKeys: { openai: process.env.OPENAI_API_KEY },
});

// Ask a question
const response = await agent.ask('What tools do you have available?');
console.log(response.response);

// Streaming responses
for await (const event of agent.askStream('Explain quantum computing')) {
  if (event.type === 'chunk') {
    process.stdout.write(event.text);
  }
}

// Cleanup
await agent.destroy();
```

### Custom Tools

Register JavaScript/TypeScript handlers that the LLM can call:

```typescript
import { MCPAgent } from '@mcpagent/node';

const agent = new MCPAgent({
  serverOptions: { mcpConfigPath: './mcp_servers.json' },
});

// Register a calculator tool
agent.registerTool(
  'calculate',
  'Perform a mathematical calculation',
  {
    type: 'object',
    properties: {
      expression: { type: 'string', description: 'Math expression to evaluate' },
    },
    required: ['expression'],
  },
  async (args) => {
    const result = Function(`"use strict"; return (${args.expression})`)();
    return String(result);
  },
  { timeoutMs: 5000 }
);

await agent.initialize({
  provider: 'vertex',
  modelId: 'gemini-3-flash-preview',
});

// The LLM can now use your custom tool
const response = await agent.ask('What is 15 * 7 + 23?');
// Output: 15 * 7 + 23 = 128
```

### Architecture

The Node.js SDK uses a **gRPC bidirectional streaming** architecture:

```
Node.js SDK ◄══════════════════════════════════► Go Server
            Single bidirectional gRPC stream
            - Client sends: questions, tool results
            - Server sends: text chunks, tool calls, events, final response
```

Benefits:
- **Real-time streaming**: Token-by-token responses via gRPC stream
- **Inline tool callbacks**: Custom tools execute in the same connection (no separate callback server)
- **Low latency**: Unix domain sockets for IPC
- **Type-safe**: Protocol Buffers for all messages

### SDK Examples

See [examples/nodejs-sdk/](examples/nodejs-sdk/) for complete examples:

- **[basic.ts](examples/nodejs-sdk/src/basic.ts)** - Basic agent setup and queries
- **[custom-tools.ts](examples/nodejs-sdk/src/custom-tools.ts)** - Register and use custom tools
- **[multi-turn.ts](examples/nodejs-sdk/src/multi-turn.ts)** - Multi-turn conversations

For full SDK documentation, see [sdk-node/README.md](sdk-node/README.md).

## 📚 Core Features

### 1. **Standard Tool-Use Agent**

The default mode where the LLM invokes tools directly through native tool calling:

```go
agent, err := mcpagent.NewAgent(
    ctx, llmModel, "", "config.json", "model-id",
    nil, "", nil,
    mcpagent.WithMode(mcpagent.SimpleAgent),
)
```

### 2. **Tool Search Mode**

Enable dynamic tool discovery for large tool catalogs. The LLM starts with only `search_tools` and discovers tools on-demand:

```go
agent, err := mcpagent.NewAgent(
    ctx, llmModel, "config.json",
    mcpagent.WithToolSearchMode(true),
    // Optional: pre-discover frequently used tools
    mcpagent.WithPreDiscoveredTools([]string{"get_weather", "send_message"}),
)
```

**How it works:**
1. LLM sees only `search_tools` initially
2. LLM calls `search_tools(query: "weather")` to find relevant tools
3. Discovered tools (`get_weather`, `weather_forecast`) become available
4. LLM can now use discovered tools

**Features:**
- Regex pattern matching for flexible search
- Fuzzy search fallback when no exact matches found
- Pre-discovered tools option for frequently used tools
- Works with any LLM provider

See [docs/tool_search_mode.md](docs/tool_search_mode.md) for details.

### 3. **Code Execution Mode**

Execute Go code instead of JSON tool calls for complex logic:

```go
// Start HTTP server for tool execution (required)
handlers := executor.NewExecutorHandlers(configPath, logger)
mux := http.NewServeMux()
mux.HandleFunc("/api/mcp/execute", handlers.HandleMCPExecute)
mux.HandleFunc("/api/custom/execute", handlers.HandleCustomExecute)
mux.HandleFunc("/api/virtual/execute", handlers.HandleVirtualExecute)

server := &http.Server{Addr: "127.0.0.1:8000", Handler: mux}
go server.ListenAndServe()
defer server.Shutdown(ctx)

// Create agent with code execution mode
agent, err := mcpagent.NewAgent(
    ctx, llmModel, "", "config.json", "model-id",
    nil, "", nil,
    mcpagent.WithCodeExecutionMode(true),
    mcpagent.SetFolderGuardPaths([]string{"/workspace"}, []string{"/workspace"}),
)
```

The LLM can write Go programs that import and use MCP tools and custom tools as native functions. Custom tools are automatically generated as Go packages and accessible via HTTP API.

**Note**: Code execution mode requires an HTTP server running (default port 8000, configurable via `MCP_API_URL` environment variable).

### 4. **Smart Routing (DEPRECATED)**

⚠️ **DEPRECATED**: This feature is deprecated and will be removed in a future version. Only use when explicitly needed for legacy compatibility.

Dynamically filter tools based on conversation context to reduce token usage:

```go
agent, err := mcpagent.NewAgent(
    ctx, llmModel, "", "config.json", "model-id",
    nil, "", nil,
    mcpagent.WithSmartRouting(true), // DEPRECATED
    mcpagent.WithSmartRoutingThresholds(20, 3), // DEPRECATED
)
```

### 5. **Context Offloading**

Context offloading is a context engineering strategy that automatically saves large tool outputs to the filesystem instead of keeping them in the LLM's context window. This implements the **"offload context"** pattern, one of three primary context engineering approaches used in production agents like [Manus](https://rlancemartin.github.io/2025/10/15/manus/).

**Why Context Offloading?**

As agents execute tasks, tool call results accumulate in the context window. Research from [Chroma](https://www.trychroma.com/blog/context-rot) and [Anthropic](https://docs.anthropic.com/claude/docs/context-editing) shows that as context windows fill, LLM performance degrades due to attention budget depletion. Context offloading prevents this by:

- **Saving tokens**: Only file path + preview (~200 chars) instead of full content (potentially 50k+ chars)
- **Preventing context overflow**: Large outputs don't consume context window space
- **Maintaining performance**: LLM attention budget isn't depleted by large payloads
- **Enabling efficient exploration**: Agent can access data incrementally as needed

**How It Works:**

```go
agent, err := mcpagent.NewAgent(
    ctx, llmModel, "config.json",
    mcpagent.WithContextOffloading(true),
    mcpagent.WithLargeOutputThreshold(10000), // tokens (default)
)
```

When tool outputs exceed the threshold:

1. **External Storage**: Full content is saved to `tool_output_folder/{session-id}/` with unique filenames
2. **Compact Reference**: LLM receives file path + preview (first 50% of threshold) instead of full content
3. **On-Demand Access**: Agent uses virtual tools to access data incrementally:
   - `read_large_output` - Read specific character ranges
   - `search_large_output` - Search for patterns using ripgrep
   - `query_large_output` - Execute jq queries on JSON files

**Example Token Savings:**

```
Without Context Offloading:
- Tool Output: 50,000 characters (~12,500 tokens)
- Sent to LLM: 50,000 chars (~12,500 tokens)
- Result: Context window overflow, attention budget depletion

With Context Offloading:
- Tool Output: 50,000 characters (~12,500 tokens)
- Saved to filesystem: 50,000 chars
- Sent to LLM: ~200 chars (file path + preview) (~50 tokens)
- Result: 99.6% token reduction, no context overflow

Note: The threshold is measured in tokens (using tiktoken encoding), not characters.
A threshold of 10000 tokens roughly equals ~40,000 characters (assuming ~4 chars per token).
```

**Related Patterns:**

This implementation follows the context engineering strategies outlined in [Manus's approach](https://rlancemartin.github.io/2025/10/15/manus/):

- **Offload Context**: Store tool results externally, access on-demand ✅ **Implemented**
- **Reduce Context**: Compact stale results, summarize when needed ⏳ **Pending**
- **Isolate Context**: Use sub-agents for discrete tasks (multi-agent support)

Similar patterns are used in Claude Code, LangChain, and other production agent systems.

**Pending: Dynamic Context Reduction**

Currently, context offloading only applies to large tool outputs when they're first generated. A future enhancement will implement **dynamic context reduction** to compact stale tool results as the context window fills, even if they weren't initially large.

**What's Pending:**

1. **Compact Stale Results**
   - **Concept**: Replace older tool results with compact references (e.g., file paths) as context fills
   - **Behavior**: Keep recent tool results in full to guide the agent's next decision, while older results are replaced with references
   - **Implementation**: Automatically detect when tool results become "stale" (based on age, relevance, or context usage) and replace them with compact references
   - **Scope**: This would apply to ALL tool results (not just large ones), dynamically compacting them when they become "stale"
   - **Reference**: Similar to [Anthropic's context editing feature](https://docs.anthropic.com/claude/docs/context-editing)
   - **Example**: A 2000-token tool result from 10 turns ago becomes: `"Tool: search_docs returned results (saved to: tool_output_folder/session-123/search_20250101_120000.json)"`

2. **Summarize When Needed**
   - **Concept**: Once compaction reaches diminishing returns, apply schema-based summarization to the full trajectory
   - **Behavior**: Generate consistent summary objects using full tool results, further reducing context while preserving essential information
   - **Implementation**: When compaction alone isn't enough to manage context size, apply structured summarization with predefined schemas for different tool result types
   - **Scope**: Summarize the entire conversation trajectory when individual compaction is insufficient
   - **Example**: Instead of keeping 20 tool calls with full results, create a structured summary:
     ```json
     {
       "tool_calls_summary": [
         {"tool": "search", "count": 5, "key_findings": ["..."], "files": ["..."]},
         {"tool": "read_file", "count": 3, "files_read": ["..."]}
       ]
     }
     ```

**Current Behavior vs. Future Enhancement:**

```
Current (Context Offloading):
- Large output (>10k tokens) → Offloaded immediately
- Small output (<10k tokens) → Stays in context forever
- Result: Context can still fill up with many small tool results

Future (Context Reduction):
- Large output (>10k tokens) → Offloaded immediately ✅
- Small output (<10k tokens) → Stays in context initially
- As context fills → Small outputs become "stale" → Compacted to references
- When compaction insufficient → Summarize trajectory
- Result: Context window stays manageable throughout long conversations
```

This enhancement would complete the "Reduce Context" strategy from [Manus's context engineering approach](https://rlancemartin.github.io/2025/10/15/manus/), working alongside context offloading to maintain optimal context window usage.

See the [Context Offloading example](examples/offload_context/) for a complete demonstration.

See the [Context Offloading example](examples/offload_context/) for a complete demonstration.

### 6. **Context Summarization**

Automatically summarize conversation history when token usage exceeds a threshold to maintain long-running conversations:

```go
agent, err := mcpagent.NewAgent(
    ctx, llmModel, "", "config.json", "model-id",
    nil, "", nil,
    // Enable context summarization
    mcpagent.WithContextSummarization(true),
    // Trigger when token usage reaches 70% of context window
    mcpagent.WithSummarizeOnTokenThreshold(true, 0.7),
    // Keep last 8 messages intact
    mcpagent.WithSummaryKeepLastMessages(8),
)
```

The agent monitors token usage and automatically replaces older messages with a concise LLM-generated summary when the threshold is reached, while preserving recent messages and tool call integrity. This enables "infinite" conversation depth within fixed context windows.

### 7. **MCP Server Caching**

Intelligent caching reduces connection times by 60-85%:

```go
// Caching is enabled by default
// Configure via environment variables:
// MCP_CACHE_DIR=/path/to/cache
// MCP_CACHE_TTL_MINUTES=10080 (7 days)
```

### 8. **Structured Output**

Get structured data from LLM responses in two ways:

**Fixed Conversion Model** (2 LLM calls - reliable):
```go
type Person struct {
    Name  string `json:"name"`
    Age   int    `json:"age"`
    Email string `json:"email"`
}

person, err := agent.AskStructured[Person](
    ctx,
    "Create a person profile for John Doe, age 30, email john@example.com",
    Person{},
    schemaString,
)
```

**Tool-Based Model** (1 LLM call - faster):
```go
result, err := agent.AskWithHistoryStructuredViaTool[Order](
    ctx,
    messages,
    "submit_order",
    "Submit an order with items",
    orderSchema,
)
if result.HasStructuredOutput {
    order := result.StructuredResult
    // Use structured order data
}
```

See [examples/structured_output/](examples/structured_output/) for complete examples.

### 9. **Custom Tools**

Register your own tools that work alongside MCP server tools. Custom tools work in both standard mode and code execution mode:

**Standard Mode** (direct tool calls):
```go
// Define tool parameters (JSON schema)
params := map[string]interface{}{
    "type": "object",
    "properties": map[string]interface{}{
        "operation": map[string]interface{}{
            "type": "string",
            "enum": []string{"add", "subtract", "multiply", "divide"},
        },
        "a": map[string]interface{}{"type": "number"},
        "b": map[string]interface{}{"type": "number"},
    },
    "required": []string{"operation", "a", "b"},
}

// Register the tool
err := agent.RegisterCustomTool(
    "calculator",
    "Performs mathematical operations",
    params,
    calculatorFunction,
    "utility", // category (required)
)

// Tool execution function
func calculatorFunction(ctx context.Context, args map[string]interface{}) (string, error) {
    // Extract and validate arguments
    operation := args["operation"].(string)
    a := args["a"].(float64)
    b := args["b"].(float64)
    
    // Perform calculation
    var result float64
    switch operation {
    case "add": result = a + b
    case "subtract": result = a - b
    // ...
    }
    
    return fmt.Sprintf("Result: %.2f", result), nil
}
```

**Code Execution Mode** (via generated Go code):
```go
// In code execution mode, custom tools are automatically:
// 1. Generated as Go packages (e.g., data_tools, utility_tools)
// 2. Accessible via HTTP API endpoint (/api/custom/execute)
// 3. Included in tool structure JSON for LLM discovery

// Register custom tool (same API)
err := agent.RegisterCustomTool(
    "get_weather",
    "Gets weather data for a location",
    weatherParams,
    weatherFunction,
    "data", // category
)

// LLM can use it in generated Go code:
// import "data_tools"
// result := data_tools.GetWeather(data_tools.GetWeatherParams{
//     Location: "San Francisco",
//     Unit: "fahrenheit",
// })
```

See [examples/custom_tools/](examples/custom_tools/) for standard mode examples and [examples/code_execution/custom_tools/](examples/code_execution/custom_tools/) for code execution mode examples.

### 10. **Parallel Tool Execution**

When the LLM returns multiple tool calls in a single response, they can be executed concurrently using goroutines (fork-join pattern) instead of sequentially:

```go
agent, err := mcpagent.NewAgent(
    ctx, llmModel, "config.json",
    mcpagent.WithParallelToolExecution(true),
)
```

**How it works:**
1. LLM returns N tool calls in one response
2. All tool calls are prepared sequentially (argument parsing, client resolution)
3. Tool calls execute concurrently via goroutines
4. Results are collected in deterministic order matching the original tool call order

**Observability:** `ToolCallStartEvent` includes an `IsParallel` field (`true` when the tool call is part of a parallel batch, `false` for sequential execution) so event listeners and tracers can distinguish between parallel and sequential tool calls.

### 11. **Observability**

Built-in tracing with Langfuse support:

```go
tracer := observability.NewLangfuseTracer(...)
agent, err := mcpagent.NewAgent(
    ctx, llmModel, "", "config.json", "model-id",
    tracer, "trace-id", logger,
)
```

## 📖 Documentation

Comprehensive documentation is available in the [docs/](docs/) directory:

- **[OAuth Authentication](docs/oauth.md)** - OAuth 2.0 authentication for MCP servers
- **[Code Execution Agent](docs/code_execution_agent.md)** - Execute Go code with MCP tools
- **[Tool Search Mode](docs/tool_search_mode.md)** - Dynamic tool discovery for large tool catalogs
- **[Tool-Use Agent](docs/tool_use_agent.md)** - Standard tool calling mode
- **[Context Summarization](docs/context_summarization.md)** - Automatic history summarization
- **[Smart Routing](docs/smart_routing.md)** (DEPRECATED) - Dynamic tool filtering
- **[Context Offloading](docs/large_output_handling.md)** - Offload large tool outputs to filesystem (offload context pattern)
  - Implements the "offload context" strategy from [Manus's context engineering approach](https://rlancemartin.github.io/2025/10/15/manus/)
  - Prevents context window overflow and reduces token costs
  - Enables efficient on-demand data access via virtual tools
- **[MCP Cache System](docs/mcp_cache_system.md)** - Server metadata caching
- **[Folder Guard](docs/folder_guard.md)** - Fine-grained file access control
- **[LLM Resilience](docs/llm_resilience.md)** - Error handling and fallbacks
- **[Event System](docs/event_type_generation.md)** - Event architecture
- **[Parallel Tool Execution](docs/parallel_tool_execution.md)** - Concurrent tool call execution
- **[Token Tracking](docs/token-usage-tracking.md)** - Usage monitoring

## 📝 Examples

Complete working examples are available in the [examples/](examples/) directory:

### Basic Examples
- **[basic/](examples/basic/)** - Simple agent setup with a single MCP server
- **[multi-turn/](examples/multi-turn/)** - Multi-turn conversations with conversation history
- **[context_summarization/](examples/context_summarization/)** - Automatic context summarization

### Advanced Examples
- **[multi-mcp-server/](examples/multi-mcp-server/)** - Connect to multiple MCP servers simultaneously
- **[browser-automation/](examples/browser-automation/)** - Browser automation using Playwright MCP server

### Structured Output Examples
- **[structured_output/fixed/](examples/structured_output/fixed/)** - Fixed conversion model for structured output
  - Uses `AskStructured()` method
  - 2 LLM calls (text response + JSON conversion)
  - More reliable, works with complex schemas
  
- **[structured_output/tool/](examples/structured_output/tool/)** - Tool-based model for structured output
  - Uses `AskWithHistoryStructuredViaTool()` method
  - 1 LLM call (tool call during conversation)
  - Faster and more cost-effective

### Custom Tools Example
- **[custom_tools/](examples/custom_tools/)** - Register and use custom tools
  - Register multiple custom tools with different categories
  - Tools work alongside MCP server tools
  - Examples: calculator, text formatter, weather simulator, text counter

- **[offload_context/](examples/offload_context/)** - Context offloading example
  - Demonstrates automatic offloading of large tool outputs to filesystem
  - Shows how tool results are stored externally and accessed on-demand
  - Uses virtual tools (`read_large_output`, `search_large_output`, `query_large_output`) for efficient data exploration
  - Example: Search operations that produce large results, automatically offloaded and accessed incrementally

### Tool Search Example
- **[tool_search/](examples/tool_search/)** - Tool search mode for dynamic tool discovery
  - LLM starts with only `search_tools` virtual tool
  - Demonstrates searching for tools using regex patterns
  - Shows how discovered tools become available dynamically
  - Uses Vertex AI with Gemini 3 Flash
  - Example: Search for documentation tools and use them to get library information

### Code Execution Examples
- **[code_execution/simple/](examples/code_execution/simple/)** - Basic code execution mode
  - LLM writes and executes Go code instead of JSON tool calls
  - MCP tools are auto-generated as Go packages
  - Supports complex logic: loops, conditionals, data transformations
  - Security features: AST validation, isolated execution
  - Examples: discover code files, use multiple MCP servers together
  - No folder guards (simplest example)
  - HTTP server required (default port 8000)

- **[code_execution/browser-automation/](examples/code_execution/browser-automation/)** - Code execution with browser automation
  - Combines code execution mode with Playwright MCP server
  - Complex multi-step browser automation tasks
  - Example: IPO analysis with web scraping and data collection

- **[code_execution/multi-mcp-server/](examples/code_execution/multi-mcp-server/)** - Code execution with tool filtering
  - Demonstrates tool filtering in code execution mode
  - Uses `WithSelectedTools()` and `WithSelectedServers()` to filter available tools
  - Example: Selective tool access across multiple MCP servers

- **[code_execution/custom_tools/](examples/code_execution/custom_tools/)** - Custom tools in code execution mode
  - Register custom tools that work in code execution mode
  - Custom tools are auto-generated as Go packages
  - Accessible via HTTP API endpoint (`/api/custom/execute`)
  - Example: Weather tool accessible via generated Go code

Each example includes:
- Complete working code
- README with detailed documentation
- MCP server configuration
- Setup instructions

## 🔧 Configuration

### MCP Server Configuration

Create a JSON file with your MCP servers:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "./demo"]
    },
    "memory": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-memory"]
    }
  }
}
```

### Agent Options

The agent supports extensive configuration via functional options:

```go
agent, err := mcpagent.NewAgent(
    ctx, llmModel, "config.json",
    // Observability (optional)
    mcpagent.WithTracer(tracer),
    mcpagent.WithTraceID(traceID),
    mcpagent.WithLogger(logger),
    
    // Agent mode
    mcpagent.WithMode(mcpagent.SimpleAgent),
    
    // Conversation settings
    mcpagent.WithMaxTurns(30),
    mcpagent.WithTemperature(0.7),
    mcpagent.WithToolChoice("auto"),
    
    // Code execution
    mcpagent.WithCodeExecutionMode(true),
    mcpagent.SetFolderGuardPaths(allowedRead, allowedWrite),

    // Tool search mode (dynamic tool discovery)
    mcpagent.WithToolSearchMode(true),
    mcpagent.WithPreDiscoveredTools([]string{"tool1", "tool2"}),

    // Smart routing (DEPRECATED)
    mcpagent.WithSmartRouting(true), // DEPRECATED
    mcpagent.WithSmartRoutingThresholds(20, 3), // DEPRECATED
    
    // Parallel tool execution (concurrent goroutines for multiple tool calls)
    mcpagent.WithParallelToolExecution(true),

    // Context offloading (offload large tool outputs to filesystem)
    mcpagent.WithContextOffloading(true),
    mcpagent.WithLargeOutputThreshold(10000),

    // Context summarization
    mcpagent.WithContextSummarization(true),
    mcpagent.WithSummarizeOnTokenThreshold(true, 0.7),
    
    // Custom tools
    mcpagent.WithCustomTools(customTools),
    
    // Tool selection
    mcpagent.WithSelectedTools([]string{"server1:tool1", "server2:*"}),
    mcpagent.WithSelectedServers([]string{"server1", "server2"}),
    
    // Custom tool registration (after agent creation)
    // agent.RegisterCustomTool(name, description, params, execFunc, category)
)
```

## 🧪 Testing

The package includes comprehensive testing utilities:

```bash
# Run all tests
cd cmd/testing
go test ./...

# Run specific test
go run testing.go agent-mcp --log-file logs/test.log
go run testing.go code-exec --log-file logs/test.log
go run testing.go smart-routing --log-file logs/test.log
go run testing.go parallel-tool-exec --provider vertex --model gemini-3-flash-preview
```

See [cmd/testing/README.md](cmd/testing/README.md) for details.

## 📁 Package Structure

```
mcpagent/
├── agent/              # Core agent implementation
│   ├── agent.go       # Main Agent struct and NewAgent()
│   ├── conversation.go # Conversation loop and tool execution
│   ├── connection.go   # MCP server connection management
│   └── ...
├── grpcserver/        # gRPC server (for SDK communication)
│   ├── server.go      # gRPC server setup
│   ├── service.go     # AgentService implementation
│   ├── stream_handler.go # Bidirectional stream handling
│   └── pb/            # Generated protobuf code
├── mcpclient/         # MCP client implementations
│   ├── client.go       # Client interface and implementations
│   ├── stdio_manager.go # stdio protocol
│   ├── sse_manager.go  # SSE protocol
│   └── http_manager.go # HTTP protocol
├── mcpcache/          # Caching system
│   ├── manager.go     # Cache manager
│   └── codegen/       # Code generation for tools
├── llm/               # LLM provider integration
│   ├── providers.go   # Provider implementations
│   └── types.go       # LLM types
├── events/            # Event system
│   ├── data.go        # Event data structures
│   └── types.go       # Event types
├── logger/             # Logging
│   └── v2/            # Logger v2 interface
├── observability/     # Tracing and observability
│   ├── tracer.go      # Tracer interface
│   └── langfuse_tracer.go # Langfuse implementation
├── executor/          # Tool execution handlers
├── sdk-node/          # Node.js/TypeScript SDK
│   ├── src/           # SDK source code
│   │   ├── agent.ts   # MCPAgent class
│   │   ├── grpc-client.ts # gRPC client
│   │   └── stream-handler.ts # Stream management
│   └── README.md      # SDK documentation
├── proto/             # Protocol Buffer definitions
│   └── agent.proto    # gRPC service definitions
├── examples/          # Example applications
└── docs/              # Documentation
```

## 🔌 Supported LLM Providers

- **OpenAI**: GPT-4, GPT-3.5, and other models
- **AWS Bedrock**: Claude Sonnet, Claude Haiku, and other models
- **Google Vertex AI**: Gemini, PaLM, and other models
- **Custom Providers**: Extensible provider interface

## 🔌 Supported MCP Protocols

- **stdio**: Standard input/output (most common)
- **SSE**: Server-Sent Events
- **HTTP**: REST API

## 🤝 Contributing

Contributions are welcome! Please see the [Documentation Writing Guide](docs/doc_writing_guide.md) for standards.

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- **MCP Protocol**: Built on the [Model Context Protocol](https://modelcontextprotocol.io/)
- **multi-llm-provider-go**: LLM provider abstraction layer
- **mcp-go**: MCP protocol implementation
- **Context Engineering**: Context offloading implementation inspired by [Manus's context engineering strategies](https://rlancemartin.github.io/2025/10/15/manus/)

---

**Made with ❤️ for the AI community**

