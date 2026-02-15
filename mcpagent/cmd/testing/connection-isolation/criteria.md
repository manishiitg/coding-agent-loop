# Connection Isolation Test Criteria

This test validates that the global STDIO connection pool has been removed and each agent has isolated, independent connections.

## What This Test Validates

### 1. Parallel Agent Execution
- Multiple agents (3) are created simultaneously with the same MCP server configuration
- Each agent gets its own independent STDIO connection
- No race conditions occur during parallel creation
- All agents are created successfully

### 2. Sequential Agent Lifecycle
- Agents can be created, used, and closed sequentially
- Closing one agent does NOT affect the next agent
- Each new agent gets a fresh, working connection
- No "broken pipe" or "connection already closed" errors

## Success Criteria (Log Analysis)

### For Parallel Agent Test
Look for these log patterns:
- `Creating multiple agents in parallel` with `num_agents=3`
- Multiple `Creating agent` logs (agent_id 0, 1, 2)
- Multiple `Agent created successfully` logs
- `All agents created successfully` with `created=3`
- `All agents closed successfully`
- `Parallel agent test PASSED`

### For Sequential Lifecycle Test
Look for these log patterns:
- 3 iterations of:
  - `Creating agent` (iteration 0, 1, 2)
  - `Agent created, running a simple query`
  - `Agent query successful`
  - `Agent closed, creating new agent...`
- `Sequential lifecycle test PASSED`
- `New agents work correctly after previous agents are closed`

## Failure Indicators

### Connection Pool Issues (Fixed)
If you see these, the pool wasn't properly removed:
- `[STDIO POOL]` log messages (pool still exists)
- `pool.GetConnection` references
- Race condition errors during parallel creation
- "broken pipe" after closing an agent

### Connection Isolation Issues
- Agent 2 fails because Agent 1's close affected it
- "connection reset by peer" errors
- Mutex deadlocks
- Timeout during agent creation (pool contention)

## Expected Behavior

With the pool removed:
1. Each `NewStdioManager().Connect()` creates a fresh subprocess
2. Each agent owns its connection exclusively
3. Closing one agent terminates only its subprocess
4. No shared state between agents

## Running the Test

```bash
# Basic run
mcpagent-test test connection-isolation

# With logging
mcpagent-test test connection-isolation --log-file logs/connection-isolation.log --verbose

# With specific model
mcpagent-test test connection-isolation --model gpt-4.1-mini
```

## Background

This test was created as part of the refactoring to remove the global STDIO connection pool (`stdio_pool.go`). The pool caused:
- Race conditions when multiple agents shared connections
- Broken pipe errors when one agent closed a shared connection
- Complex mutex logic and potential deadlocks

By removing the pool and having each agent own its connections directly:
- Simpler code (no mutexes, health checks, cleanup routines)
- No connection sharing between agents
- Each agent is isolated from others' failures
