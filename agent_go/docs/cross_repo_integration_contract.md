# Cross-Repo Integration Contract — mcp-agent-builder-go

This repo is the **top layer** in the 3-repo LLM pipeline:

```
mcp-agent-builder-go (HTTP API + frontend)      ← YOU ARE HERE
  → mcpagent (orchestrator + agent loop)
    → multi-llm-provider-go (adapter + real CLI)
```

## Canonical Contract

The full integration contract (10 areas, IC-1 through IC-10) is maintained in:

```
multi-llm-provider-go/docs/cross_repo_integration_contract.md
```

## Tests in This Repo

| IC | Area | Test File | Count |
|----|------|-----------|-------|
| IC-3 | Token usage & cost | `cmd/server/cost_routes_test.go` | 20 subtests |
| IC-7 | SSE error serialization | `cmd/server/sse_test.go` | 4 tests |
| IC-8 | Cancellation (subscriber) | `internal/events/event_store_test.go` | 19 subtests |
| IC-8 | Cancellation (shutdown) | `cmd/server/shutdown_cleanup_test.go` | 2 tests |

## Key Functions Under Contract

- `costObserver.HandleEvent()` — IC-3: token_usage event → cost ledger entry
- `extractCacheTokens()` — IC-3: cache token extraction from generation info
- `handleCostSummary()` — IC-3: GET /api/cost/summary aggregation
- `writeSSEEvent()` — IC-7: SSE message formatting + panic recovery
- `EventStore.Subscribe()` / `Unsubscribe()` — IC-8: subscriber lifecycle
- `EventStore.AddEvent()` — IC-8: event filtering (hidden vs streaming bypass)
- `ShouldShowEvent()` — IC-8: event visibility filtering
- `cancelActiveWorkForShutdown()` — IC-8: graceful shutdown cancellation

## Remaining E2E Gaps

These require full-stack tests (running server + real HTTP client):

- **IC-8 full e2e**: SSE client disconnect → context cancel → CLI process group kill
- **IC-3 full e2e**: Chat request → token_usage event → cost ledger → GET /api/cost/summary
- **IC-7 full e2e**: Agent error → error event → SSE `event: error` at client
