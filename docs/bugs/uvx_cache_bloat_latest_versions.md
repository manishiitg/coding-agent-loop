# Bug: uvx/npx Cache Bloat from @latest MCP Server Packages

## Status: Open

## Problem

The `~/.cache/uv/` directory grows unbounded (10GB+ observed) over time due to repeated `uvx` ephemeral venv creation for MCP servers configured with `@latest`.

## Root Cause

### 1. `@latest` prevents venv reuse

7 uvx servers and 2 npx servers in `mcp_servers_clean_user.json` use `@latest`:
- `awslabs.aws-api-mcp-server@latest`
- `awslabs.aws-pricing-mcp-server@latest`
- `awslabs.cost-explorer-mcp-server@latest`
- `awslabs.cloudwatch-mcp-server@latest`
- `awslabs.billing-cost-management-mcp-server@latest`
- `awslabs.aws-documentation-mcp-server@latest`
- `awslabs.eks-mcp-server@latest`
- `@playwright/mcp@latest` (npx)
- `kubernetes-mcp-server@latest` (npx)

`uvx package@latest` creates a new ephemeral virtual environment each time it runs because it cannot guarantee the cached venv has the latest version. Pinned versions (`uvx package==0.4.2`) allow uvx to reuse cached venvs.

### 2. Multiple spawn points create throwaway processes

Processes are spawned at:
- **Server startup**: Background discovery for uncached servers (`tools.go:runBackgroundDiscovery`)
- **Every 24h**: Periodic refresh (`tools.go:startPeriodicRefresh`) re-discovers all servers
- **Per query** (session registry path): Creates long-lived connections, but first spawn per server still adds to cache

Each throwaway discovery spawn creates a new venv entry in `~/.cache/uv/` that is never cleaned up.

### 3. No uvx cache cleanup

`uv` does not automatically prune old environments. They accumulate indefinitely.

## Impact

- Disk usage grows ~1.4GB per discovery cycle (7 servers x ~200MB venv with AWS SDK deps)
- Over weeks of server restarts + 24h refreshes: 10GB+
- Performance degradation from disk pressure

## Affected Files

- `agent_go/configs/mcp_servers_clean_user.json` — Server configs with `@latest`
- `mcpagent/mcpclient/client.go:connectOnce()` — Process spawn point (line ~192)
- `mcpagent/mcpclient/stdio_manager.go` — StdioManager creates subprocess
- `mcpagent/mcpcache/integration.go` — Background discovery creates throwaway connections
- `agent_go/cmd/server/tools.go` — Periodic refresh and background discovery triggers

## Proposed Fix: Version Pin Resolution

Resolve `@latest` to the actual version via PyPI/npm registry API, cache it for 24h, and use pinned versions for spawning.

### Flow
```
First run / every 24h:
  Resolve: GET https://pypi.org/pypi/awslabs.aws-api-mcp-server/json → version "0.4.2"
  Cache:   {"awslabs.aws-api-mcp-server": {"version": "0.4.2", "resolved_at": "..."}}
  Spawn:   uvx awslabs.aws-api-mcp-server==0.4.2  (uvx reuses cached venv)

Next 24h:
  Cache hit → uvx awslabs.aws-api-mcp-server==0.4.2  (no download, no new venv)

After 24h:
  Re-resolve @latest → maybe "0.4.3" → cache new version
```

### Implementation

1. **New: `mcpagent/mcpclient/version_pin.go`** — Core logic: detect `@latest` in args, resolve via HTTP to PyPI (`https://pypi.org/pypi/<pkg>/json`) or npm (`https://registry.npmjs.org/<pkg>/latest`), return pinned args
2. **New: `mcpagent/mcpclient/version_pin_cache.go`** — JSON file cache (`cache/version_pins.json`): singleton, 24h TTL, thread-safe, persists to disk
3. **Modify: `mcpagent/mcpclient/client.go`** — 2-line change in `connectOnce()` before `NewStdioManager`:
   ```go
   resolvedArgs := ResolveVersionPins(ctx, c.config.Command, c.config.Args, GetVersionPinCache(c.logger), c.logger)
   stdioManager := NewStdioManager(c.config.Command, resolvedArgs, env, c.logger)
   ```

### Why this interception point

All spawn paths converge at `client.go:connectOnce()` → `NewStdioManager(command, args, env)`:
- Session registry connections (main query path via `connection_session.go`)
- Background discovery (`integration.go:performOriginalConnectionLogic`)
- Periodic refresh (`tools.go:startPeriodicRefresh`)
- Fresh connection fallback (`integration.go:GetFreshConnection`)

One change covers all paths. Original config args are never mutated (copy is made).

### Pinned arg format
- uvx: `package==0.4.2` (PyPI convention)
- npx: `package@0.4.2` (npm convention)
- Scoped npm: `@playwright/mcp@1.2.3`

### Graceful degradation
- If PyPI/npm is unreachable: 10s timeout, warning logged, `@latest` kept
- If package not found: warning logged, `@latest` kept
- If cache file corrupt: fresh empty cache created
- SSE/HTTP protocol servers: unaffected (resolution only runs for stdio)

### Configuration
| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `MCP_VERSION_PIN_TTL_HOURS` | `24` | How long to cache resolved versions |
| `MCP_CACHE_DIR` | `./cache` | Shared with existing tool cache |

## Workaround (Manual)

Run periodically to clean old uvx cache:
```bash
uv cache prune
```

Or pin versions manually in `mcp_servers_clean_user.json`:
```json
"args": ["awslabs.aws-api-mcp-server==0.4.2"]
```
