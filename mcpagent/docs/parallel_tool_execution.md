# Parallel Tool Execution

## Overview

When the LLM returns multiple tool calls in a single response, the agent can execute them concurrently using goroutines instead of processing them one at a time. This reduces wall-clock latency when multiple independent tool calls are issued together.

## Enabling

```go
agent, err := mcpagent.NewAgent(
    ctx, llmModel, "config.json",
    mcpagent.WithParallelToolExecution(true),
)
```

Default: `false` (sequential execution).

## How It Works

The implementation uses a three-phase fork-join pattern in `agent/parallel_tool_execution.go`:

### Phase 1: Sequential Preparation

All tool calls are prepared sequentially:
- Parse tool arguments
- Resolve MCP clients
- Emit `ToolCallStartEvent` (with `IsParallel: true`)

This keeps event ordering deterministic.

### Phase 2: Parallel Execution

Each tool call runs in its own goroutine:

```go
results := make([]toolExecutionResult, len(plans))
var wg sync.WaitGroup

for i, plan := range plans {
    wg.Add(1)
    go func(idx int, p toolExecutionPlan) {
        defer wg.Done()
        results[idx] = executeToolCall(ctx, a, p, ...)
    }(i, plan)
}

wg.Wait()
```

Results are written to pre-allocated indexed slots (no mutex needed since each goroutine writes to a unique index).

### Phase 3: Sequential Assembly

Results are collected in deterministic order matching the original tool call order:
- Append tool response messages
- Emit `ToolCallEndEvent` / `ToolCallErrorEvent`
- Run loop detection

## Observability

`ToolCallStartEvent` includes an `IsParallel` field:

| Field | Type | Description |
|-------|------|-------------|
| `IsParallel` | `bool` | `true` when the tool call is part of a parallel execution batch, `false` for sequential execution |

This allows event listeners and tracers to distinguish between parallel and sequential tool calls without relying on timing heuristics.

### Example event data

```json
{
  "event_type": "tool_call_start",
  "turn": 2,
  "tool_name": "resolve_library_id",
  "is_parallel": true,
  "server_name": "context7"
}
```

## Testing

Run the parallel tool execution test:

```bash
go run ./cmd/testing parallel-tool-exec \
  --provider vertex \
  --model gemini-3-flash-preview \
  --log-level info
```

The test:
1. Runs the same prompt with `WithParallelToolExecution(true)` and then with sequential execution (default)
2. Compares wall-clock duration, overlap count, and `IsParallel` flag values
3. Validates that the parallel run has `IsParallel=true` on all tool call events and the sequential run has `IsParallel=false`

### Expected output

```
Parallel execution completed  duration=10.6s  tool_calls=6  is_parallel_true=6  is_parallel_false=0
Sequential execution completed duration=13.8s  tool_calls=6  is_parallel_true=0  is_parallel_false=6
Speedup: 1.30x faster with parallel execution
Parallel run: 6/6 tool calls marked IsParallel=true
Sequential run: all 6 tool calls marked IsParallel=false (correct)
```

## When to Use

Parallel execution is beneficial when:
- The LLM frequently issues multiple independent tool calls in one response
- Tool calls involve network I/O (MCP server calls, API requests)
- Latency reduction matters more than strict sequential ordering

It is safe to enable because:
- Results are always assembled in the original tool call order
- Each goroutine writes to an isolated result slot
- Event emission (start/end) is still sequential

## Key Files

| File | Purpose |
|------|---------|
| `agent/parallel_tool_execution.go` | Fork-join implementation |
| `agent/conversation.go:920` | Entry point — branches to parallel or sequential |
| `agent/agent.go` | `WithParallelToolExecution()` option |
| `events/data.go` | `ToolCallStartEvent.IsParallel` field |
| `cmd/testing/parallel-tool-exec/` | Integration test |
