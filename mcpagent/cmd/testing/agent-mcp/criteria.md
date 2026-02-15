# Agent MCP Test - Log Analysis Guide

This test validates agent functionality with multiple MCP servers. **These tests don't use traditional asserts** - instead, logs are analyzed (manually or by LLM) to verify test success.

## Running the Test

```bash
# Basic run (logs to stdout, uses OpenAI by default)
mcpagent-test test agent-mcp

# With log file (recommended for analysis)
mcpagent-test test agent-mcp --log-file logs/agent-mcp-test.log

# With specific model (provider defaults to openai)
mcpagent-test test agent-mcp --model gpt-4.1-mini --log-file logs/agent-mcp-test.log

# With debug logging
mcpagent-test test agent-mcp --log-level debug --log-file logs/agent-mcp-test.log
```

## What This Test Does

1. Creates a temporary MCP config with three servers:
   - `sequential-thinking` (stdio via npx)
   - `context7` (HTTP)
   - `awslabs.aws-pricing-mcp-server` (stdio via uvx)

2. Creates an agent with those MCP servers

3. Runs a question that requires tool usage:
   > "Use sequential thinking to analyze: What would be the cost of running an EC2 t3.medium instance in us-east-1 for 30 days? Then search for any relevant AWS documentation about EC2 pricing."

4. Verifies that MCP tools were called and used correctly

## Log Analysis Checklist

After running the test, analyze the log file to verify the following:

### ✅ MCP Server Connections

- [ ] Check for successful connection to `sequential-thinking` server
- [ ] Check for successful connection to `context7` server (HTTP protocol)
- [ ] Check for successful connection to `awslabs.aws-pricing-mcp-server` server
- [ ] Verify no connection errors or timeouts
- [ ] Check that all servers initialized properly

**What to look for in logs:**
```
✅ Created temporary MCP config
✅ Agent created with MCP servers
```

### ✅ Tool Discovery

- [ ] Verify tools from sequential-thinking server are discovered
- [ ] Verify tools from context7 server are discovered
- [ ] Verify tools from aws-pricing server are discovered
- [ ] Check tool registration in agent (should see tool names in logs)
- [ ] Verify tools are properly registered with the LLM

**What to look for in logs:**
```
tool registration
discovered tools
sequential_thinking_tools
context7_tools
awslabs_aws_pricing_mcp_server_tools
```

### ✅ Tool Execution

- [ ] Look for `tool_call_start` events for sequential-thinking tools
- [ ] Look for `tool_call_start` events for context7 tools (if used)
- [ ] Look for `tool_call_start` events for aws-pricing tools
- [ ] Verify `tool_call_end` events with successful results
- [ ] Check for any `tool_call_error` events (should be none)
- [ ] Verify tool inputs and outputs are logged correctly

**What to look for in logs:**
```
tool_call_start
tool_call_end
tool_call_error (should NOT appear)
```

### ✅ LLM Interaction

- [ ] Verify `llm_generation_start` events
- [ ] Check `llm_messages` events to see conversation context
- [ ] Verify `llm_generation_end` events with responses
- [ ] Check `token_usage` events for cost tracking
- [ ] Verify no `llm_error` or `throttling_detected` events
- [ ] Check that LLM received proper tool definitions

**What to look for in logs:**
```
llm_generation_start
llm_messages
llm_generation_end
token_usage
llm_error (should NOT appear)
throttling_detected (should NOT appear)
```

### ✅ Sequential Thinking Usage

- [ ] Verify sequential-thinking tool was called
- [ ] Check that reasoning steps are visible in tool output
- [ ] Verify the tool helped structure the analysis
- [ ] Check that sequential thinking output is properly formatted

**What to look for in logs:**
```
sequential_thinking
reasoning steps
step-by-step analysis
```

### ✅ AWS Pricing Query

- [ ] Verify aws-pricing tools were called (`get_pricing`, etc.)
- [ ] Check that EC2 t3.medium pricing was retrieved
- [ ] Verify cost calculation for 30 days is present
- [ ] Check for proper AWS region handling (us-east-1)
- [ ] Verify pricing data is accurate

**What to look for in logs:**
```
aws-pricing
get_pricing
EC2
t3.medium
us-east-1
cost calculation
```

### ✅ Context7 Usage (if applicable)

- [ ] Check if context7 tools were called for documentation search
- [ ] Verify search results or documentation retrieval
- [ ] Check that documentation is relevant to EC2 pricing

**What to look for in logs:**
```
context7
get_library_docs
documentation
search results
```

### ✅ Conversation Flow

- [ ] Verify multiple turns of conversation (agent reasoning)
- [ ] Check that agent used tools in correct sequence
- [ ] Verify `conversation_end` event was emitted
- [ ] Check final response contains EC2 pricing information
- [ ] Verify response is coherent and complete

**What to look for in logs:**
```
conversation_end
multiple turns
tool sequence
final response
```

### ✅ Tracing (if Langfuse enabled)

- [ ] Note the trace ID from test output
- [ ] Check Langfuse dashboard for complete trace
- [ ] Verify all spans are properly nested
- [ ] Check token usage and cost estimates
- [ ] Verify trace contains all tool calls and LLM interactions

**What to look for:**
- Trace ID is logged in test output
- Complete trace visible in Langfuse dashboard
- All events properly captured

### ❌ Error Indicators (should NOT appear)

- [ ] `tool_call_error` events
- [ ] `llm_error` events
- [ ] Connection failures or timeouts
- [ ] `throttling_detected` events
- [ ] `conversation_error` or `agent_error` events
- [ ] Panic or crash messages

**What to look for in logs (these indicate failures):**
```
tool_call_error
llm_error
connection failed
timeout
throttling_detected
conversation_error
agent_error
panic
```

## Useful Commands for Log Analysis

```bash
# Search for tool calls
grep "tool_call_start" logs/agent-mcp-test.log
grep "tool_call_end" logs/agent-mcp-test.log
grep "tool_call_error" logs/agent-mcp-test.log

# Search for specific servers
grep "sequential-thinking" logs/agent-mcp-test.log
grep "context7" logs/agent-mcp-test.log
grep "aws-pricing" logs/agent-mcp-test.log

# Search for LLM events
grep "llm_generation" logs/agent-mcp-test.log
grep "token_usage" logs/agent-mcp-test.log

# Search for errors
grep -i "error" logs/agent-mcp-test.log
grep -i "fail" logs/agent-mcp-test.log

# Count tool calls
grep -c "tool_call_start" logs/agent-mcp-test.log

# View conversation flow
grep "conversation_end\|agent_end" logs/agent-mcp-test.log
```

## Expected Test Outcome

A successful test should show:

1. ✅ All three MCP servers connected successfully
2. ✅ Tools from all servers discovered and registered
3. ✅ Sequential-thinking tool used for reasoning
4. ✅ AWS pricing tools used to get EC2 costs
5. ✅ Context7 tools potentially used for documentation (optional)
6. ✅ Multiple conversation turns with proper tool sequencing
7. ✅ Final response contains EC2 pricing information
8. ✅ No errors in the entire execution
9. ✅ Conversation ended cleanly

## Troubleshooting

### No tools discovered
- Check MCP server connections in logs
- Verify server configurations are correct
- Check for connection errors

### Tools not being called
- Check LLM tool registration in logs
- Verify question is triggering tool usage
- Check for LLM errors

### Connection failures
- Verify network connectivity for HTTP servers
- Check stdio server commands are available (npx, uvx)
- Verify AWS credentials for aws-pricing server

### Test hangs or times out
- Check for deadlocks in logs
- Verify LLM is responding
- Check for infinite loops in agent reasoning

