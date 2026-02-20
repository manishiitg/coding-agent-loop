# MCP Agent Code Execution Test - Log Analysis Criteria

This test validates the **full code execution flow** with context7 MCP server through executor handlers.

## Running the Test

```bash
export OPENAI_API_KEY=your-key
cd mcpagent
go build -o mcpagent-test ./cmd/testing
./mcpagent-test mcp-agent-code-exec --log-level debug
```

## Running with Langfuse Tracing

To enable Langfuse tracing for observability:

```bash
export LANGFUSE_PUBLIC_KEY=pk-lf-...
export LANGFUSE_SECRET_KEY=sk-lf-...
export LANGFUSE_BASE_URL=https://us.cloud.langfuse.com  # Optional
export OPENAI_API_KEY=your-key

go run ./cmd/testing/... mcp-agent-code-exec --log-level debug
```

The test will output a `trace_id` that you can use to view the trace in Langfuse.

## What This Test Validates

**End-to-end code execution flow:**
Agent → Generates Go code → Executes code → Calls executor HTTP endpoint → Uses context7 MCP tool → Returns result

## Critical Success Criteria

### ✅ 1. Executor Server Started

**Must see:**
```
✅ Executor server started url=http://127.0.0.1:8000
```

**Why it matters:** Generated code calls `http://localhost:8000` by default

---

### ✅ 2. Code Execution Agent Created

**Must see:**
```
✅ Code execution mode enabled
MCP server configured: context7
```

**Why it matters:** Confirms agent can generate code and has context7 available

---

### ✅ 3. Context7 Tool Called Successfully

**Must see in response:**
- Response contains "react" or library information
- Response length > 50 characters
- No error messages about "tool not found" or "server not configured"

**What to look for:**
```
✅ Agent response received response=...react...
✅ Response indicates context7 tool was called successfully
```

**Why it matters:** Proves the full flow worked - code was generated, executed, and called context7

---

### ✅ 4. Full Flow Confirmation

**Must see:**
```
✅ Agent successfully executed code with MCP tool
This confirms:
  1. Agent generated Go code
  2. Code was executed via write_code virtual tool
  3. Generated code called executor HTTP endpoint
  4. Executor handler called context7 MCP tool
  5. Full code execution flow works end-to-end
```

---

## What Success Looks Like

**Successful test output:**
```
=== MCP Agent Code Execution Test ===
--- Step 1: Start Executor HTTP Server ---
✅ Executor server started url=http://127.0.0.1:8000

--- Step 2: Create Code Execution Agent ---
✅ Created temporary MCP config with context7
✅ Code execution mode enabled
MCP server configured: context7

--- Step 3: Test Code Execution Flow ---
Sending query to agent query=Use the context7 server to resolve...
✅ Agent response received response=...react library...
✅ Response indicates context7 tool was called successfully
✅ Agent successfully executed code with MCP tool

✅ Code execution flow completed successfully
✅ All code execution tests passed
```

---

## Debug Logs to Check

With `--log-level debug`, verify these appear:

### Code Generation
```
🔧 Executing virtual tool write_code
```

### Code Execution
```
🔧 Executing Go code using 'go run' command
```

### HTTP Call to Executor
```
Making request url=http://localhost:8000/api/mcp/execute
Response status=200
```

### Context7 Tool Execution
```
Calling MCP tool tool=resolve_library_id server=context7
```

---

## Common Failures

### ❌ "OPENAI_API_KEY is required"
**Fix:** `export OPENAI_API_KEY=your-key`

### ❌ "bind: address already in use" (port 8000)
**Fix:** `lsof -ti:8000 | xargs kill`

### ❌ Response doesn't mention "react"
**Problem:** Context7 tool wasn't called
**Check:** Look for "tool not found" or "server not configured" in logs

### ❌ "empty response from agent"
**Problem:** Agent didn't complete the task
**Check:** Agent logs for errors, verify LLM is working

---

## Quick Checklist

- [ ] Server starts on port 8000
- [ ] Agent created with code execution mode
- [ ] Context7 configured
- [ ] Query sent to agent
- [ ] Response received (not empty)
- [ ] Response mentions "react" or library info
- [ ] No errors in logs
- [ ] All 5 confirmation points logged

**If all checked:** ✅ Test passed!

---

## Related Files

- `mcpagent/executor/handlers.go` - HTTP handlers being tested
- `mcpagent/agent/code_execution_tools.go` - Code execution implementation
- `docs/code_execution_agent.md` - Code execution documentation

---

## Langfuse Trace Verification

When running with Langfuse enabled, verify the trace contains the expected spans:

### Reading the Trace

```bash
# List recent traces
go run ./cmd/testing/... langfuse-read --traces --limit 5

# Read a specific trace (use trace_id from test output)
go run ./cmd/testing/... langfuse-read --trace-id <trace-id>

# Read trace with observations (spans)
go run ./cmd/testing/... langfuse-read --trace-id <trace-id> --observations --limit 20
```

### Expected Trace Structure

```
Trace (test-trace-...)
├── Agent Span (agent_<model>_<tools>)
│   └── Conversation Span
│       ├── GENERATION (llm_generation_turn_1_...)
│       │   └── Tool Span (tool_context7_resolve_library_id_...)
│       └── GENERATION (final response)
├── MCP Connection Span (mcp_connection_context7)
└── MCP Discovery Span (mcp_discovery_1_servers_...)
```

### Verification Checklist

- [ ] **GENERATION spans** have `model`, `usage`, and `endTime` fields
- [ ] **Tool spans** have `output` with `result` or `error`
- [ ] **MCP Connection span** shows successful connection to context7
- [ ] **MCP Discovery span** shows 1 server and tools discovered
- [ ] Trace shows full code execution flow

### Automated Verification

```bash
# Check GENERATION spans have model and usage
go run ./cmd/testing/... langfuse-read --trace-id <trace-id> --observations --limit 20 2>&1 | \
  grep -A10 '"type": "GENERATION"' | grep -E "(model|usage|endTime)"

# Check for MCP connection spans
go run ./cmd/testing/... langfuse-read --trace-id <trace-id> --observations --limit 20 2>&1 | \
  grep -B2 -A5 'mcp_connection'

# Check for tool spans
go run ./cmd/testing/... langfuse-read --trace-id <trace-id> --observations --limit 20 2>&1 | \
  grep -B2 -A5 '"name": "tool_'
```

See `docs/tracing.md` for more details on the tracing implementation.
