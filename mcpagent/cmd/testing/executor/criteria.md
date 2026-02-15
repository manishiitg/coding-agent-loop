# Executor Package Test - Log Analysis Criteria

This test validates the `mcpagent/executor` package that provides HTTP handlers for tool execution. **These tests don't use traditional asserts** - instead, logs are analyzed (manually or by LLM) to verify test success.

## Running the Test

```bash
# Build the test binary
cd mcpagent
go build -o mcpagent-test ./cmd/testing

# Run the executor test
./mcpagent-test test executor

# With debug logging
./mcpagent-test test executor --log-level debug

# With custom log file
./mcpagent-test test executor --log-file logs/executor-test.log

# With custom MCP config
./mcpagent-test test executor --config configs/mcp_servers_simple.json

# With specific provider (default: openai)
./mcpagent-test test executor --provider openai
```

## What This Test Does

The test validates the refactored executor package that moved tool execution handlers from `agent_go/cmd/server` into the `mcpagent/executor` library:

1. **Initialize Agent & Registry** - Creates an agent to set up the `codeexec` registry
2. **Start HTTP Server** - Starts a real HTTP server on `127.0.0.1:18765` with executor handlers
3. **Test MCP Execute** - Makes POST request to `/api/mcp/execute`
4. **Test Custom Execute** - Makes POST request to `/api/custom/execute`
5. **Test Virtual Execute** - Makes POST request to `/api/virtual/execute`

## Log Analysis Checklist

After running the test, analyze the log file to verify:

### ✅ Test Initialization

- [ ] "=== Executor Package Integration Test ===" appears in logs
- [ ] "Starting HTTP server with executor handlers and making real requests" appears
- [ ] No panic or crash during startup

**What to look for in logs:**
```
=== Executor Package Integration Test ===
Starting HTTP server with executor handlers and making real requests
```

### ✅ Step 1: Agent Initialization

- [ ] "--- Step 1: Initialize Agent and Registry ---" appears
- [ ] "Creating agent to initialize tool registry..." appears
- [ ] "Using config path=..." appears
- [ ] "✅ Agent created and registry initialized" appears
- [ ] No errors during agent creation

**What to look for in logs:**
```
--- Step 1: Initialize Agent and Registry ---
Creating agent to initialize tool registry...
Using config path=configs/mcp_servers_simple.json
✅ Agent created and registry initialized
```

### ✅ Step 2: HTTP Server Startup

- [ ] "--- Step 2: Start HTTP Server ---" appears
- [ ] "Using config for handlers path=..." appears
- [ ] "✅ Server started url=http://127.0.0.1:18765" appears
- [ ] No port conflict errors

**What to look for in logs:**
```
--- Step 2: Start HTTP Server ---
Using config for handlers path=configs/mcp_servers_simple.json
✅ Server started url=http://127.0.0.1:18765
```

### ✅ Step 3: MCP Execute Endpoint

- [ ] "--- Step 3: Test MCP Execute Endpoint ---" appears
- [ ] "Testing POST /api/mcp/execute..." appears
- [ ] "Making request url=/api/mcp/execute" appears
- [ ] "Response status=200" appears
- [ ] "Response received" with JSON response appears
- [ ] Either "✅ MCP execute endpoint works" OR "⚠️ MCP execute test failed" appears

**What to look for in logs:**
```
--- Step 3: Test MCP Execute Endpoint ---
Testing POST /api/mcp/execute...
Making request url=http://127.0.0.1:18765/api/mcp/execute
Response status=200
Response received response={"success":false,"error":"..."}
Expected failure (server not configured) error=...
✅ MCP execute endpoint works
```

### ✅ Step 4: Custom Execute Endpoint

- [ ] "--- Step 4: Test Custom Execute Endpoint ---" appears
- [ ] "Testing POST /api/custom/execute..." appears
- [ ] "Making request url=/api/custom/execute" appears
- [ ] "Response status=200" appears
- [ ] "Response received" with JSON response appears
- [ ] Either "✅ Custom execute endpoint works" OR "⚠️ Custom execute test failed" appears

**What to look for in logs:**
```
--- Step 4: Test Custom Execute Endpoint ---
Testing POST /api/custom/execute...
Making request url=http://127.0.0.1:18765/api/custom/execute
Response status=200
Response received response={"success":false,"error":"..."}
Expected failure (tool not registered) error=...
✅ Custom execute endpoint works
```

### ✅ Step 5: Virtual Execute Endpoint

- [ ] "--- Step 5: Test Virtual Execute Endpoint ---" appears
- [ ] "Testing POST /api/virtual/execute..." appears
- [ ] "Making request url=/api/virtual/execute" appears
- [ ] "Response status=200" appears
- [ ] "Response received" with JSON response appears
- [ ] Either "✅ Virtual execute endpoint works" OR "⚠️ Virtual execute test failed" appears

**What to look for in logs:**
```
--- Step 5: Test Virtual Execute Endpoint ---
Testing POST /api/virtual/execute...
Making request url=http://127.0.0.1:18765/api/virtual/execute
Response status=200
Response received response={"success":false,"error":"..."}
Expected failure (virtual tool not registered) error=...
✅ Virtual execute endpoint works
```

### ✅ Test Completion

- [ ] "✅ All executor tests passed" appears
- [ ] "📋 For detailed verification, see criteria.md in cmd/testing/executor/" appears
- [ ] "Server stopped" appears
- [ ] No goroutine leaks or hanging processes

**What to look for in logs:**
```
✅ All executor tests passed

📋 For detailed verification, see criteria.md in cmd/testing/executor/
Server stopped
```

## Expected Test Outcome

A successful test run should:

1. Initialize agent and registry without errors
2. Start HTTP server on port 18765
3. Make successful HTTP requests to all three endpoints
4. Receive valid JSON responses (status 200) from all endpoints
5. Complete all tests without panics or unexpected errors
6. Shut down server cleanly

**Note**: It's **expected** that tool execution may fail (e.g., "server not configured", "tool not found") - the test validates that the **handlers work correctly**, not that tools execute successfully.

## Troubleshooting

### "failed to create LLM"

**Cause**: Missing LLM API keys in environment  
**Solution**: Set `OPENAI_API_KEY` or configure another provider  
**Impact**: Test will fail - this is a real issue

### "bind: address already in use"

**Cause**: Port 18765 already in use  
**Solution**: Kill process using port: `lsof -ti:18765 | xargs kill`  
**Impact**: Test fails - need to free the port

### "Request failed: connection refused"

**Cause**: Server didn't start properly  
**Solution**: Check server startup logs, increase sleep time in test  
**Impact**: Test fails - indicates server issue

### "code execution registry not initialized"

**Cause**: Agent creation failed  
**Solution**: Check LLM configuration and MCP config file  
**Impact**: Test fails - indicates agent initialization issue

### "failed to load config"

**Cause**: MCP config file not found  
**Solution**: Verify config path exists or use `--config` flag  
**Impact**: Test may fail or use empty config (which is OK for basic validation)

## Related Files

- `mcpagent/executor/handlers.go` - HTTP handlers being tested
- `mcpagent/executor/client.go` - Client factory being tested
- `mcpagent/agent/codeexec/registry.go` - Registry integration
- `docs/refactoring/tool_executor_refactoring.md` - Refactoring documentation
- `mcpagent/executor/executor_test.go` - Alternative Go test (can also be run with `go test`)
