# Native Workspace Mode

## Overview

The system supports two deployment modes for the workspace server:

1. **Docker mode** (default): Workspace runs inside a Docker container at `/app/workspace-docs`. Shell commands execute inside the container and need `host.docker.internal` to reach the Go agent server on the host.
2. **Native mode**: Workspace runs directly on the host filesystem. Shell commands execute on the host and use `localhost`/`127.0.0.1` for all connectivity.

## Detection

A single environment variable controls mode detection:

```
NATIVE_WORKSPACE=true
```

Set automatically by `run_server_with_logging.sh --with-workspace`. All runtime code checks this via:

```go
common.IsNativeWorkspace()  // pkg/common/types.go
```

Do **not** infer the mode from `WORKSPACE_DOCS_PATH`, `WORKSPACE_API_URL`, or any other env var. Those are used for path resolution and connectivity, not mode detection.

## What Changes Between Modes

| Concern | Docker | Native |
|---------|--------|--------|
| `MCP_API_URL` seen by shell commands | `http://host.docker.internal:<port>` | `http://127.0.0.1:<port>` |
| CDP URL for agent-browser | `http://host.docker.internal:9222` | `http://localhost:9222` |
| CDP instructions shown to LLM | Uses `host.docker.internal` | Uses `localhost` |
| `blockAbsoluteHostPaths` guard | Active (VirtioFS can leak `/Users/`) | Skipped (sandbox-exec handles it) |
| Sandbox isolation (Folder Guard) | Linux mount namespaces (`unshare -m`) | macOS `sandbox-exec` profiles |

## Files Involved

### Centralized helper

- `agent_go/pkg/common/types.go` — `IsNativeWorkspace()` checks `NATIVE_WORKSPACE=true`

### Detection consumers

| File | Function | What it decides |
|------|----------|-----------------|
| `agent_go/cmd/server/server.go` | `GetCodeExecAPIURL()` | `MCP_API_URL` for shell commands |
| `agent_go/pkg/browser/executor.go` | `resolveCdpURL()` | CDP connection URL |
| `agent_go/pkg/instructions/browser.go` | `cdpHost()` | Host in LLM CDP instructions |
| `agent_go/pkg/workspace/execute_shell_command.go` | `blockAbsoluteHostPaths` caller | Whether to block `/Users/` etc. in commands |

### Path resolution (NOT mode detection)

These use `WORKSPACE_DOCS_PATH` for the actual filesystem path, not to detect Docker vs native:

- `agent_go/pkg/fsutil/atomic.go` — `WorkspaceDocsRoot()`, `WorkspaceShellRoot()`
- `agent_go/pkg/orchestrator/base_orchestrator_folder_guard.go` — strips absolute prefixes
- `agent_go/pkg/workspace/diff_patch_workspace_file.go` — strips absolute prefixes

### Sandbox profile generation

- `workspace/security/isolator.go` — `generateSandboxProfile()` converts relative `ReadPaths`/`WritePaths` to absolute using `baseDir` (works in both modes since `baseDir` comes from `--docs-dir`)

### Startup

- `agent_go/run_server_with_logging.sh` — sets `NATIVE_WORKSPACE=true` when `--with-workspace` is used

## Overrides

- `CDP_HOST` env var overrides CDP URL detection (highest priority, checked before `IsNativeWorkspace()`)
- `WORKSPACE_DOCS_PATH` can be set manually to point workspace at any directory

## Adding New Mode-Dependent Behavior

When adding code that behaves differently in Docker vs native:

1. Import `mcp-agent-builder-go/agent_go/pkg/common`
2. Use `common.IsNativeWorkspace()` — do not check other env vars for mode detection
3. Add an entry to the table above
