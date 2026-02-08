# Bug: Dependency Updates in Sibling Module Not Reflected in Agent Build

## Symptom
When modifying code in the sibling module `../multi-llm-provider-go` (e.g., adding debug logs, fixing bugs in Azure adapter), the changes are **not** reflected in the deployed `mcp-agent` container, even when using `go.work` and local builds.

## verification Steps
1. Added `[AZURE_PATCH_V5_ACTIVE]` log to `azure_adapter.go`.
2. Redeployed using `./deploy.sh agent --local`.
3. Logs showed `Initialized Azure AI LLM` but **missing** the patch logs.
4. Health check showed `status: healthy` but no indication of the new code version.

## Root Cause Analysis (Suspected)
- **Docker Build Context:** The `Dockerfile.agent` copies `multi-llm-provider-go/` but might be caching the `COPY` layer if the file modification timestamps aren't propagated or if `go build` is hitting a cached intermediate layer that ignores the new files.
- **Go Cache:** `go build` inside the container might be using a cached module state if `go.work` isn't fully respected or if `go.mod` sums match (even if code changed).
- **Orphaned Containers:** Azure Container Apps might be rolling back to a previous healthy revision if the new one fails startup checks (though logs suggest it started).

## Proposed Solution: Version Verification
To definitively prove which code is running:
1.  Add a `const VERSION` string in `multi-llm-provider-go/llmtypes/version.go`.
2.  Update `agent_go/cmd/server/server.go` to import `llmtypes` and include `llmtypes.VERSION` in the `/api/health` response.
3.  Increment this version string on every significant patch.
4.  Check `/api/health` after deployment. If the version matches, the code is present.

## Workaround
- Force cache bust by modifying `multi-llm-provider-go/go.mod` (adding a comment) before build.
- Ensure `deploy.sh` effectively cleans or ignores Docker cache for the relevant layers.
