# Tracing Implementation

This document describes the tracing implementation in mcpagent and how to use it with Langfuse and LangSmith.

## Overview

The mcpagent supports multiple observability backends for tracing LLM interactions, tool calls, and MCP server connections:

- **Langfuse**: Full-featured observability platform with cost tracking
- **LangSmith**: LangChain's observability platform with evaluation features

Both tracers follow the same subscriber pattern and can be used independently or simultaneously. The tracing captures:

- **Traces**: Top-level container for a complete agent interaction
- **GENERATION spans**: LLM generation calls with token usage and model info
- **Tool spans**: MCP tool calls with inputs and outputs
- **MCP Connection spans**: Server connection timing and status
- **MCP Discovery spans**: Server and tool discovery information

## Trace Hierarchy

```
Trace (query_...)
├── Agent Span (agent_<model>_<tools>)
│   └── Conversation Span (conversation_...)
│       ├── GENERATION (llm_generation_turn_<N>_<model>_<tools>)
│       │   └── Tool Span (tool_<server>_<tool_turn_<N>)
│       ├── GENERATION (llm_generation_turn_<N>_<model>_<tools>)
│       │   └── Tool Span (...)
│       └── GENERATION (final response with text output)
├── MCP Connection Span (mcp_connection_<server>)
└── MCP Discovery Span (mcp_discovery_<N>_servers_<M>_tools)
```

## Span Details

### GENERATION Spans

Captures LLM API calls with:

| Field | Description |
|-------|-------------|
| `name` | `llm_generation_turn_<turn>_<model>_<tool_count>_tools` |
| `model` | Model ID (e.g., `gemini-3-flash-preview`) |
| `startTime` | When LLM call started |
| `endTime` | When LLM call completed |
| `output` | Text content (null for tool-call-only responses) |
| `usage.input` | Prompt tokens |
| `usage.output` | Completion tokens |
| `usage.total` | Total tokens |

**Note**: When the LLM generates only tool calls (no text response), the `output` field is null. This is expected behavior - only the final response with actual text content will have output populated.

### Tool Spans

Captures MCP tool executions with:

| Field | Description |
|-------|-------------|
| `name` | `tool_<server>_<tool_name>_turn_<N>` |
| `startTime` | When tool call started |
| `endTime` | When tool call completed |
| `output.result` | Tool execution result |
| `output.duration` | Execution time |
| `output.error` | Error message (if failed) |

### MCP Connection Spans

Captures server connection with:

| Field | Description |
|-------|-------------|
| `name` | `mcp_connection_<server_name>` |
| `startTime` | Connection start time |
| `endTime` | Connection end time |
| `output.connection_time` | Total connection duration |
| `output.server_info.servers_count` | Number of servers connected |
| `output.server_info.tools_count` | Number of tools discovered |
| `output.server_info.cache_used` | Whether cache was used |

### MCP Discovery Spans

Captures tool discovery with:

| Field | Description |
|-------|-------------|
| `name` | `mcp_discovery_<N>_servers_<M>_tools` |
| `output.connected_servers` | Successfully connected servers |
| `output.failed_servers` | Failed server connections |
| `output.discovery_time` | Time taken for discovery |
| `output.tool_count` | Total tools discovered |

## Configuration

### Environment Variables

Set these in your `.env` file:

```bash
# Langfuse tracing
LANGFUSE_PUBLIC_KEY=pk-lf-...
LANGFUSE_SECRET_KEY=sk-lf-...
LANGFUSE_BASE_URL=https://us.cloud.langfuse.com  # Optional, defaults to cloud.langfuse.com

# LangSmith tracing
LANGSMITH_API_KEY=lsv2_pt_...
LANGSMITH_PROJECT=mcpagent                    # Optional, defaults to "default"
LANGSMITH_ENDPOINT=https://api.smith.langchain.com  # Optional
LANGSMITH_PROJECT_ID=eac64540-...             # Optional, UUID for API queries

# LLM Provider (for testing)
VERTEX_API_KEY=...  # For Vertex/Gemini
OPENAI_API_KEY=...  # For OpenAI
```

### Using Multiple Tracers

You can use both Langfuse and LangSmith simultaneously:

```go
import "github.com/manishiitg/mcpagent/observability"

// Get multiple tracers from comma-separated providers
tracers := observability.GetTracers("langfuse,langsmith", logger)

// Create agent with multiple tracers
agent, err := mcpagent.NewAgent(ctx, llmModel, configPath,
    mcpagent.WithTracer(tracers[0]),
    mcpagent.WithTracer(tracers[1]),
)
```

Or programmatically:

```go
// Get individual tracers
langfuseTracer := observability.GetTracerWithLogger("langfuse", logger)
langsmithTracer := observability.GetTracerWithLogger("langsmith", logger)

// Add both to agent
agent, err := mcpagent.NewAgent(ctx, llmModel, configPath,
    mcpagent.WithTracer(langfuseTracer),
    mcpagent.WithTracer(langsmithTracer),
)
```

## Testing

### Running the Agent MCP Test

The `agent-mcp` test automatically uses both Langfuse and LangSmith if credentials are available:

```bash
# Run with default settings (Vertex + gemini-3-flash-preview)
go run ./cmd/testing/... agent-mcp

# Run with specific provider/model
go run ./cmd/testing/... agent-mcp --provider openai --model gpt-4o

# Run with verbose logging
go run ./cmd/testing/... agent-mcp --verbose
```

The test will:
1. Connect to 3 MCP servers (sequential-thinking, context7, aws-pricing)
2. Run an agent query that uses MCP tools
3. Emit all events to both Langfuse and LangSmith (if configured)
4. Output the `trace_id` for verification
5. Flush both tracers before completion

### Running Tests with Log Files

To avoid filling the terminal or LLM context with log output during tests, use the `--log-file` flag and suppress console output:

```bash
# Run test and save all output to a log file
go run ./cmd/testing agent-mcp --log-file agent_test.log --show-output=false
```

### Reading Langfuse Traces

Use the `langfuse-read` command to verify Langfuse traces:

```bash
# List recent traces
go run ./cmd/testing/... langfuse-read --traces --limit 5

# Read a specific trace
go run ./cmd/testing/... langfuse-read --trace-id <trace-id>

# Read trace with observations (spans)
go run ./cmd/testing/... langfuse-read --trace-id <trace-id> --observations --limit 20
```

### Reading LangSmith Runs

Use the `langsmith-read` command to verify LangSmith runs:

```bash
# List recent runs
go run ./cmd/testing/... langsmith-read --runs --limit 5

# Get a specific run by ID
go run ./cmd/testing/... langsmith-read --run-id <run-id>

# Get all runs in a trace
go run ./cmd/testing/... langsmith-read --trace-id <trace-id> --limit 20

# Filter by run type (llm, chain, tool)
go run ./cmd/testing/... langsmith-read --runs --run-type llm --limit 10

# Filter by project name
go run ./cmd/testing/... langsmith-read --runs --project my-project --limit 5
```

#### LangSmith Read Command Flags

| Flag | Description |
|------|-------------|
| `--run-id` | Get specific run by ID |
| `--trace-id` | Get all runs in a trace |
| `--project` | Filter by project name |
| `--limit` | Max results (default: 10) |
| `--runs` | List recent runs |
| `--run-type` | Filter by type (llm, chain, tool) |

### Automated Verification

To verify tracing is working correctly, check for these in the trace:

#### 1. GENERATION Spans Have Required Fields

```bash
# Check GENERATION spans have model and usage
go run ./cmd/testing/... langfuse-read --trace-id <trace-id> --observations --limit 20 2>&1 | \
  grep -A10 '"type": "GENERATION"' | grep -E "(model|usage|endTime)"
```

Expected output:
```
"model": "gemini-3-flash-preview",
"endTime": "2026-01-14T07:38:04.154Z",
"usage": { "input": 8413, "output": 252, "total": 8665, "unit": "TOKENS" }
```

#### 2. Tool Spans Have Output

```bash
# Check tool spans have output
go run ./cmd/testing/... langfuse-read --trace-id <trace-id> --observations --limit 30 2>&1 | \
  grep -B2 -A5 '"name": "tool_'
```

Expected: Tool spans should have `output` with `result` or `error` fields.

#### 3. MCP Spans Have Timing

```bash
# Check MCP connection/discovery spans
go run ./cmd/testing/... langfuse-read --trace-id <trace-id> --observations --limit 30 2>&1 | \
  grep -B2 -A10 'mcp_connection\|mcp_discovery'
```

Expected:
```json
"name": "mcp_connection_all",
"output": {
  "connection_time": "5.732819875s",
  "server_info": { "servers_count": 3, "tools_count": 12 }
}
```

### CI/CD Integration

For automated testing in CI/CD:

```bash
#!/bin/bash
set -e

# Run agent-mcp test and capture trace ID
OUTPUT=$(go run ./cmd/testing/... agent-mcp 2>&1)
TRACE_ID=$(echo "$OUTPUT" | grep "trace_id=" | tail -1 | sed 's/.*trace_id=//' | cut -d' ' -f1)

if [ -z "$TRACE_ID" ]; then
  echo "ERROR: No trace ID found"
  exit 1
fi

echo "Trace ID: $TRACE_ID"

# Wait for Langfuse to ingest the trace
sleep 10

# Verify trace exists and has observations
TRACE_DATA=$(go run ./cmd/testing/... langfuse-read --trace-id "$TRACE_ID" --observations --limit 10 2>&1)

# Check for GENERATION spans with model
if echo "$TRACE_DATA" | grep -q '"model": "gemini-3-flash-preview"'; then
  echo "✅ GENERATION spans have model field"
else
  echo "❌ GENERATION spans missing model field"
  exit 1
fi

# Check for MCP connection spans
if echo "$TRACE_DATA" | grep -q 'mcp_connection'; then
  echo "✅ MCP connection spans present"
else
  echo "❌ MCP connection spans missing"
  exit 1
fi

# Check for tool spans with output
if echo "$TRACE_DATA" | grep -q '"name": "tool_'; then
  echo "✅ Tool spans present"
else
  echo "❌ Tool spans missing"
  exit 1
fi

echo "✅ All tracing checks passed"
```

## Implementation Details

### Key Files

| File | Description |
|------|-------------|
| `observability/tracer.go` | Tracer interface definition |
| `observability/factory.go` | Tracer factory with provider selection |
| `observability/langfuse_tracer.go` | Langfuse tracer implementation |
| `observability/langsmith_tracer.go` | LangSmith tracer implementation |
| `events/data.go` | Event type definitions |
| `agent/agent.go` | Agent event emission |
| `cmd/testing/agent-mcp/` | Agent MCP test command |
| `cmd/testing/langfuse/` | Langfuse read command |
| `cmd/testing/langsmith/` | LangSmith read command |

### Event Flow

1. **Agent emits events** via `EmitTypedEvent()`:
   - `LLMGenerationStartEvent` / `LLMGenerationEndEvent`
   - `ToolCallStartEvent` / `ToolCallEndEvent` / `ToolCallErrorEvent`
     - `ToolCallStartEvent.IsParallel` is `true` when the tool call is part of a parallel execution batch (multiple tool calls running concurrently via goroutines), `false` for sequential execution
   - `MCPServerConnectionStartEvent` / `MCPServerConnectionEndEvent`
   - `MCPServerDiscoveryEvent`

2. **Tracers process events** in `EmitEvent()`:
   - Each tracer (Langfuse, LangSmith) receives the event independently
   - Routes to appropriate handler based on event type
   - Creates/updates spans or runs with proper hierarchy
   - Queues events for batch sending

3. **Batch sender** sends events to respective API:
   - Langfuse: `POST /api/public/ingestion` with Basic Auth
   - LangSmith: `POST /runs/batch` with X-API-Key header
   - Both batch events for efficiency (2-second intervals, 50-event threshold)
   - Handle retries and errors independently

### Span Tracking Maps

The tracer maintains maps to track span relationships:

```go
agentSpans         map[string]string // traceID -> agent span ID
conversationSpans  map[string]string // traceID -> conversation span ID
llmGenerationSpans map[string]string // traceID -> current LLM generation span ID
toolCallSpans      map[string]string // {traceID}_{turn}_{toolName} -> tool span ID
mcpConnectionSpans map[string]string // serverName -> mcp connection span ID
```

This allows proper span ending when the corresponding end event is received.

## Troubleshooting

### Traces Not Appearing in Langfuse

1. Check credentials are set correctly in `.env`
2. Verify `LANGFUSE_BASE_URL` if using self-hosted Langfuse
3. Check logs for "Langfuse: Sent batch successfully" messages
4. Wait 5-10 seconds for Langfuse ingestion

### Missing Token Usage

- Ensure `LLMGenerationEndEvent` is being emitted with `UsageMetrics`
- Check that the LLM provider returns usage information in the response

### Tool Spans Not Ending

- Check for `ToolCallErrorEvent` events (errors end spans differently)
- Verify tool name matches between start and end events

### MCP Spans Missing Server Name

- Check that `MCPServerConnectionEvent` has `ServerName` field populated
- The tracer extracts server name from this field for span naming

## LangSmith Integration

### Run Hierarchy

LangSmith uses "runs" instead of traces/spans. The hierarchy maps as follows:

```
Run (run_type=chain, query_...)        # Root run (trace equivalent)
├── Run (run_type=chain, agent_...)    # Agent run
│   └── Run (run_type=chain, conversation)
│       ├── Run (run_type=llm, llm_generation_turn_<N>)
│       │   └── Run (run_type=tool, tool_<server>_<tool>)
│       └── Run (run_type=llm, llm_generation_turn_<N>)
├── Run (run_type=chain, mcp_connection_<server>)
└── Run (run_type=chain, mcp_discovery_<N>_servers_<M>_tools)
```

### Concept Mapping

| Langfuse | LangSmith | Description |
|----------|-----------|-------------|
| Trace | Run (run_type=chain) | Top-level container |
| GENERATION span | Run (run_type=llm) | LLM API call with token usage |
| Tool span | Run (run_type=tool) | Tool execution |
| SPAN | Run (run_type=chain) | Generic span |
| parentObservationId | parent_run_id | Hierarchy support |

### Verifying LangSmith Traces

1. Go to https://smith.langchain.com
2. Select your project (default: "default", or set via `LANGSMITH_PROJECT`)
3. View runs in the "Runs" tab
4. Click on a run to see the hierarchy and details

### LangSmith Troubleshooting

**Runs Not Appearing**:
1. Check `LANGSMITH_API_KEY` is set correctly
2. Verify project name matches `LANGSMITH_PROJECT`
3. Check logs for "LangSmith: Sent batch successfully" messages
4. Wait 5-10 seconds for LangSmith ingestion

**Missing Token Usage**:
- Token usage is stored in `extra.tokens` field
- Verify LLM provider returns usage information

**Run Hierarchy Issues**:
- Check `parent_run_id` is being set correctly
- Verify `dotted_order` field for proper ordering

### LangSmith API Details

The LangSmith tracer uses the following API endpoints:

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/runs/batch` | POST | Create/update runs in batch |
| `/runs/{run_id}` | GET | Get specific run |
| `/runs` | GET | List runs with filters |

#### Run Types

| Type | Description |
|------|-------------|
| `chain` | Generic container (trace, agent, conversation) |
| `llm` | LLM generation call with token usage |
| `tool` | Tool execution |

#### Required Fields

For POST requests:
- `id`: UUID format (32 hex chars or hyphenated)
- `name`: Run name
- `run_type`: One of chain, llm, tool
- `start_time`: ISO 8601 timestamp
- `trace_id`: UUID of root run
- `dotted_order`: Ordering format `{timestamp}Z{uuid}`

For PATCH requests (ending runs):
- `id`: UUID of run to update
- `end_time`: ISO 8601 timestamp
- `trace_id`: UUID of root run
- `dotted_order`: Must match the POST request

#### UUID Mapping

The LangSmith tracer maintains an internal mapping (`traceIDToUUID`) to convert external trace IDs to LangSmith-compatible UUIDs. This allows integration with existing trace ID formats while meeting LangSmith's UUID requirements.

### Verifying Both Tracers

To verify both tracers are working in the agent-mcp test:

```bash
# Run the test
go run ./cmd/testing/... agent-mcp --verbose

# Check for tracer initialization logs:
# ✅ Langfuse tracer enabled
# ✅ LangSmith tracer enabled

# Check for successful batch sends:
# Langfuse: Sent batch successfully (events=N)
# LangSmith: Sent batch successfully

# Verify in UIs:
# - Langfuse: https://cloud.langfuse.com
# - LangSmith: https://smith.langchain.com
```


