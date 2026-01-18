# Bug: Excessive Disk Usage by `uv` Cache (180GB+)

## Status
Fixed

## Date
January 9, 2026

## Description
The `~/.cache/uv/archive-v0/` directory was found to grow excessively (reported size: 180GB). This was caused by the frequent and repeated execution of `uvx` commands with the `@latest` version specifier.

## Root Cause
1.  **Configuration:** Several MCP server configurations (e.g., `awslabs.aws-pricing-mcp-server@latest`, `mcp-google-sheets@latest`) included the `@latest` suffix in their execution commands.
2.  **Execution Frequency:** The `StreamingAPI` server in `agent_go` creates a **fresh agent instance for every query**. 
3.  **MCP Lifecycle:** Every time a new agent is created, it initializes its MCP client connections. When `uvx` is called with `@latest`, it performs a network check for the latest version and often recreates or updates the ephemeral environment for that package.
4.  **Result:** Every chat message sent to the agent triggered a new `uvx` version check and potential cache entry, leading to rapid disk space exhaustion.

## Impact
- Rapid consumption of disk space (hundreds of gigabytes).
- Slower agent response times due to mandatory version checks and environment recreation on every query.
- Potential for intermittent failures if the registry is unreachable or version checks fail.

## Resolution
Removed the `@latest` suffix from all `uvx` commands in the following configuration files:
- `mcpagent/cmd/testing/connection-isolation/mcp_servers_simple.json`
- `mcpagent/cmd/testing/agent-mcp/agent-mcp-test.go`
- `mcpagent/cmd/testing/smart-routing/smart-routing-test.go`
- `mcp-agent-builder-go/agent_go/configs/mcp_servers_simple.json`
- `mcp-agent-builder-go/agent_go/configs/mcp_servers_clean_user.json`

By removing `@latest`, `uvx` will use the existing cached environment unless the package is missing, significantly reducing disk I/O and network calls.

## Recovery Instructions
To reclaim the disk space already consumed by the cache, run:

```bash
uv cache clean
```
