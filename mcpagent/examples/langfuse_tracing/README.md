# Langfuse Tracing Example

This example demonstrates how to use Langfuse for observability and tracing of LLM interactions, tool calls, and MCP server connections.

## What Gets Traced

The Langfuse tracer captures:

- **Traces**: Top-level container for a complete agent interaction
- **GENERATION spans**: LLM generation calls with token usage, model info, and parameters
- **Tool spans**: MCP tool calls with inputs, outputs, duration, and context usage
- **MCP Connection spans**: Server connection timing and status
- **MCP Discovery spans**: Server and tool discovery information
- **Error spans**: Fallback models, throttling, token limits, and other errors

## Trace Hierarchy

```
Trace (query_...)
├── Agent Span (agent_<model>_<tools>)
│   └── Conversation Span (conversation_...)
│       ├── GENERATION (llm_generation_turn_<N>_<model>_<tools>)
│       │   └── Tool Span (tool_<server>_<tool>_turn_<N>)
│       ├── GENERATION (llm_generation_turn_<N>_<model>_<tools>)
│       │   └── Tool Span (...)
│       └── GENERATION (final response with text output)
├── MCP Connection Span (mcp_connection_<server>)
└── MCP Discovery Span (mcp_discovery_<N>_servers_<M>_tools)
```

## Setup

1. Create `.env` file with your API keys:
   ```bash
   # OpenAI API key (required)
   OPENAI_API_KEY=sk-...

   # Langfuse credentials (required for tracing)
   LANGFUSE_PUBLIC_KEY=pk-lf-...
   LANGFUSE_SECRET_KEY=sk-lf-...
   LANGFUSE_BASE_URL=https://us.cloud.langfuse.com  # Optional, defaults to cloud.langfuse.com
   ```

2. Run from the example directory:
   ```bash
   cd examples/langfuse_tracing
   go run main.go
   ```

## Usage

```bash
# Default question
go run main.go

# Custom MCP config and question
go run main.go mcp_servers.json "What are the latest AI developments?"
```

## Viewing Traces

After running the example, the trace ID will be printed. You can view the trace in several ways:

### 1. Langfuse Dashboard
Open your Langfuse dashboard and search for the trace ID.

### 2. Using the CLI
```bash
# List recent traces
go run ./cmd/testing/... langfuse-read --traces --limit 5

# Read a specific trace
go run ./cmd/testing/... langfuse-read --trace-id <trace-id>

# Read trace with all observations (spans)
go run ./cmd/testing/... langfuse-read --trace-id <trace-id> --observations --limit 30
```

## What to Look For in Traces

### GENERATION Spans
Each LLM call shows:
- Model name and parameters (temperature, tools_count)
- Token usage (input, output, total)
- Duration
- Output text (for responses with text content)

### Tool Spans
Each tool call shows:
- Tool name and server
- Input arguments
- Output result
- Duration
- Context usage (percentage of context window used)
- Model context window size

### MCP Connection Spans
Shows:
- Connection time
- Number of servers and tools discovered
- Whether cache was used

## Configuration

Edit `mcp_servers.json` to add/configure MCP servers. The example includes `context7` for documentation retrieval.

## Requirements

- Go 1.24.4+
- Node.js (for npx-based MCP servers)
- OpenAI API key
- Langfuse account and API keys

## Troubleshooting

### Traces Not Appearing in Langfuse

1. Check credentials are set correctly in `.env`
2. Verify `LANGFUSE_BASE_URL` if using self-hosted Langfuse
3. The example flushes the tracer before exiting - check for "Tracer flushed successfully" message
4. Wait 5-10 seconds for Langfuse to ingest the trace

### Missing Token Usage

- Ensure your LLM provider returns usage information in the response
- Check the GENERATION spans in Langfuse for the `usage` field

### Tool Spans Missing Output

- Check the tool execution for errors
- Verify the MCP server is responding correctly
