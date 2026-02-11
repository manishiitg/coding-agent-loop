# Bug Report: Agent Pod OOMKilled — MCP Discovery Memory & Retry Issues

## Summary

The agent pod repeatedly crashes with `OOMKilled` (exit code 137, memory limit 4Gi). Root causes are:
1. MCP connections not closed after background discovery, leaving duplicate subprocesses
2. OAuth-requiring servers (Smithery) retried indefinitely despite always failing
3. Memory limit too low for 13+ MCP server subprocess discovery

## Symptoms

- Pod status: `OOMKilled`, restarts every ~10 minutes
- `ps aux` shows **duplicate instances** of every MCP server subprocess (one from discovery, one from cache path)
- Total RSS from child processes: ~2.7GB
- `[AUTH] WARNING: Using default AUTH_SECRET` log spam on every API request
- `Connection attempt failed... unauthorized (401)... server=" []"` errors repeating (Smithery servers)

## Root Causes

### 1. Discovery connections not closed (FIXED)

**File:** `agent_go/cmd/server/tools.go` — `discoverServerToolsDetailed()`

`GetCachedOrFreshConnection()` creates live MCP connections (spawning subprocesses for stdio servers). Background discovery only needs tool metadata, but connections were left open. Each server had 2 subprocess instances: one from discovery, one from the cache path's `processCachedData()` which also creates connections.

**Fix:** Added `defer client.Close()` after extracting tool metadata:
```go
defer func() {
    for srvName, client := range result.Clients {
        if client != nil {
            if err := client.Close(); err != nil {
                api.logger.Debug(fmt.Sprintf("Failed to close MCP client for %s: %v", srvName, err))
            }
        }
    }
}()
```

### 2. OAuth servers not marked as permanently failed (FIXED)

**File:** `agent_go/cmd/server/tools.go` — `runBackgroundDiscovery()`

When `discoverServerToolsDetailed()` encounters a 401/OAuth error from `GetCachedOrFreshConnection()`, it returns `(toolStatus, nil)` — the error is in `toolStatus.Status == "error"`, NOT in the Go error return value. The tracking code in `runBackgroundDiscovery()` only checked `if err != nil`, so OAuth servers were **never** marked as permanently failed and were retried on every 24-hour cycle.

Affected servers: `notion`, `google-docs-mcp`, `google-sheets-mcp` (Smithery-hosted, require OAuth tokens).

**Fix:** Added check for error-status results after successful return:
```go
if result.Status == "error" {
    errMsg := result.Error
    if result.RequiresOAuth ||
        strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "401") ||
        strings.Contains(errMsg, "OAuth") || strings.Contains(errMsg, "forbidden") || strings.Contains(errMsg, "403") {
        api.discoveryFailedServers[serverName] = errMsg
    }
    api.toolStatusMux.Lock()
    api.toolStatus[serverName] = *result
    api.toolStatusMux.Unlock()
    continue
}
```

### 3. AUTH_SECRET not set in production (FIXED)

**File:** `agent_go/cmd/server/server.go`, `agent_go/cmd/server/auth_middleware.go`

`MULTI_USER_MODE=true` was set but `AUTH_SECRET` was missing from the k8s secret. `GetAuthSecret()` falls back to a hardcoded default and logs a warning on every API request. Both signing and verification used the same default, so auth "worked" but was insecure.

**Fix:**
- Added startup check: `log.Fatal` if `MULTI_USER_MODE=true` and `AUTH_SECRET` not set
- Generated and patched AUTH_SECRET into the k8s secret
- Updated `deploy-k8s.sh` to include `AUTH_SECRET` from `.env`

### 4. Memory limit too low (FIXED)

**File:** `deploy/k8s/agent/deployment.yaml`

Even with proper connection cleanup, sequential discovery of 13+ MCP servers causes peak memory usage because Go's runtime doesn't immediately release memory back to the OS between iterations.

**Fix:** Increased memory limit from 4Gi to 6Gi.

## Files Modified

| File | Changes |
|------|---------|
| `agent_go/cmd/server/tools.go` | `defer client.Close()` after discovery; check `result.Status == "error"` for OAuth servers; `ServerLogEntry` type + `appendServerLog()` + per-server log buffer |
| `agent_go/cmd/server/server.go` | `discoveryFailedServers` map field + init; startup `log.Fatal` for missing AUTH_SECRET; `serverLogs` map + mutex |
| `agent_go/cmd/server/mcp_config_routes.go` | Clear `discoveryFailedServers` on config save (so servers can be retried with new credentials) |
| `deploy/k8s/agent/deployment.yaml` | Memory limit 4Gi → 6Gi |
| `deploy/k8s/scripts/deploy-k8s.sh` | AUTH_SECRET extraction from `.env` and inclusion in k8s secret |
| `deploy/k8s/README.md` | AUTH_SECRET docs, MCP discovery behavior, troubleshooting section |

## Deployment

All code changes are committed locally. To deploy:

```bash
# Build and deploy agent
./deploy/k8s/scripts/deploy-k8s.sh --build agent
```

The deploy script will:
1. Build the Docker image with the fixes
2. Push to ECR
3. Apply the updated deployment.yaml (6Gi memory limit)
4. Restart the agent pod

## Verification After Deploy

```bash
# Check pod is running without restarts
kubectl get pods -n prod-mcpagent -w

# Check memory usage after discovery completes (~5-10 min)
kubectl top pod -n prod-mcpagent

# Verify no AUTH_SECRET warnings
kubectl logs deployment/mcpagent-agent-cs -n prod-mcpagent | grep "AUTH_SECRET" | wc -l
# Expected: 0

# Verify OAuth servers marked as permanently failed
kubectl logs deployment/mcpagent-agent-cs -n prod-mcpagent | grep "permanently failed"
# Expected: notion, google-docs-mcp, google-sheets-mcp

# Check process count (should NOT have duplicates)
kubectl exec <pod> -n prod-mcpagent -- ps aux | wc -l

# Check discovery completion
kubectl logs deployment/mcpagent-agent-cs -n prod-mcpagent | grep "Background tool discovery completed"
```

## Remaining Concerns

1. **Go memory not released to OS**: Even with `Close()`, Go's runtime may retain allocated memory. If 6Gi is still not enough, consider `GODEBUG=madvdontneed=1` env var to force aggressive memory release.

2. **mcpcache library creates connections even for cache hits**: `processCachedData()` in the external mcpcache library calls `performOriginalConnectionLogic()` which creates live connections even when using cached tool data. Our `defer client.Close()` handles this, but it means every discovery cycle still spawns+kills subprocesses.

3. **Smithery servers' internal retry logic**: The mcpagent library retries failed connections internally (3 attempts × 4 rounds). Even though we mark OAuth servers as permanently failed for subsequent cycles, the first discovery run still generates ~57 retry attempts across the 3 Smithery servers.
