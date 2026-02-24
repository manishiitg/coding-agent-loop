# Bug: Playwright MCP — Screenshots/Snapshots Saved to Repo Root Instead of Output Dir

## Status: Fixed

## Problem

When using the Playwright MCP server with `--output-dir` set (e.g. `workspace-docs/Downloads` or a workflow run’s `execution/Downloads`), some files still appeared in the **project/agent root** instead of the configured directory:

- **Downloads** and **auto-generated** filenames (e.g. `page-{timestamp}.png`) → correctly went to `--output-dir`.
- **Custom filenames** (e.g. `screenshot.png`, `snapshot.html`) → were written to the **workspace/process root**, ignoring `--output-dir`.

Issues were reported **mainly in workflows**, where the intended directory is run-specific (e.g. `runs/{runFolder}/execution/Downloads`).

## Root Cause

### 1. Upstream Playwright MCP behavior

In [microsoft/playwright-mcp#1390](https://github.com/microsoft/playwright-mcp/issues/1390): when the tool is called **with a custom filename**, the server resolves that path relative to the **process current working directory (cwd)**, not relative to `--output-dir`. So:

- No filename (auto-generated) → uses `--output-dir` ✅  
- Custom filename → uses cwd (often repo/agent root) ❌  

Maintainers suggested running the MCP process with **cwd set to the desired output directory** so relative paths resolve there.

### 2. No working-directory support when spawning

The MCP client (mcpagent) spawned the Playwright subprocess with the **default cwd** (e.g. `agent_go/`). There was no way to pass a custom working directory, so custom filenames always resolved relative to that cwd.

### 3. Workflow: connection reuse without config in key

The session registry reuses connections by **(sessionID, serverName)** only. The **config** (including output path) is **not** part of the key. So:

- The **first** agent in a session that uses Playwright creates the connection; that connection’s config (and cwd) is fixed.
- All later agents in the same session **reuse** that connection.
- If the first connection was created with the wrong or default path (e.g. `selectedRunFolder` not set yet), every step in that session saw the wrong directory.

In batch execution, multiple groups used to share the same session ID, so one group’s Playwright connection was reused by others → wrong folder. That was already fixed separately (unique session per group + close previous session).

## Fix

### 1. Working directory support in mcpagent (MCP client)

- **`MCPServerConfig.WorkingDir`** and **`RuntimeConfigOverride.WorkingDir`**  
  Optional path used as the subprocess **current working directory** when starting stdio MCP servers.

- **`ApplyOverride()`**  
  When applying runtime overrides, sets `newConfig.WorkingDir = override.WorkingDir` when present.

- **`StdioManager`**  
  Accepts `workingDir`. When non-empty, uses `NewStdioMCPClientWithOptions` with `transport.WithCommandFunc` so the subprocess is started with `cmd.Dir = workingDir`.

- **`client.go`**  
  Passes `c.config.WorkingDir` into `NewStdioManager`.

**Files (mcpagent):**

- `mcpclient/config.go` — `WorkingDir` on `MCPServerConfig` and `RuntimeConfigOverride`; `ApplyOverride` sets it.
- `mcpclient/stdio_manager.go` — `workingDir` parameter; `WithCommandFunc` sets `cmd.Dir`.
- `mcpclient/client.go` — Passes `config.WorkingDir` to `NewStdioManager`.

### 2. Workflow: set Playwright WorkingDir in runtime override

- **`setupBrowserDownloadsPathOverride()`** in `controller_agent_factory.go`  
  For Playwright, sets both:
  - `ArgsReplace["--output-dir"] = absDownloadsPath`
  - **`WorkingDir = absDownloadsPath`**

So the Playwright process is started with both `--output-dir` and **cwd** pointing at the run’s `execution/Downloads` (or chat’s `workspace-docs/Downloads` when no override). Custom filenames then resolve to that folder.

**File (mcp-agent-builder-go):**

- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go` — `playwrightOverride.WorkingDir = absDownloadsPath`.

### 3. Static config: optional `working_dir` for chat mode

For non-workflow use, the same path as `--output-dir` can be set in the Playwright server config so the subprocess cwd matches:

```json
"playwright": {
  "command": "npx",
  "args": ["@playwright/mcp@latest", "--output-dir", "../workspace-docs/Downloads", "--isolated"],
  "working_dir": "../workspace-docs/Downloads"
}
```

Example base config: `agent_go/configs/mcp_servers_clean.json` includes this. User config can override with an absolute path.

### 4. .gitignore fallback

Patterns were added so any Playwright artifacts that still land at repo/agent root are not committed (e.g. `.playwright-mcp/`, `agent_go/page-*.png`, `agent_go/snapshot*.html`, `/screenshot*.png`). See root `.gitignore` and `docs/playwright_artifacts.md`.

## Workflow-specific behavior

- **Run path**  
  For workflows, the directory is **per run**: e.g. `workspace-docs/{workspacePath}/runs/{runFolder}/execution/Downloads`. It is computed in `setupBrowserDownloadsPathOverride()` from `selectedRunFolder` and `getWorkspaceDocsRoot()`.

- **When it’s applied**  
  The override (including `WorkingDir`) is set on the agent config **before** the agent is created. When the MCP connection is created (first time in that session), `ApplyOverride()` is called, so the Playwright process is started with the correct cwd and `--output-dir`.

- **Session / connection reuse**  
  The first agent in a session that uses Playwright creates the connection; others reuse it. So the first agent must have the correct override (e.g. `selectedRunFolder` set before any such agent is created). Batch execution uses a **unique session ID per group** and closes the previous session before starting the next, so each group gets its own Playwright connection with the correct path.

## Affected / relevant files

| Area | File |
|------|------|
| mcpagent config | `mcpagent/mcpclient/config.go` |
| mcpagent stdio | `mcpagent/mcpclient/stdio_manager.go` |
| mcpagent client | `mcpagent/mcpclient/client.go` |
| Workflow override | `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go` |
| Base config | `agent_go/configs/mcp_servers_clean.json` |
| Docs | `docs/playwright_artifacts.md`, root `.gitignore` |

## References

- Upstream: [playwright-mcp#1390 — browser_take_screenshot: custom filenames bypass --output-dir](https://github.com/microsoft/playwright-mcp/issues/1390)
- Historical: `docs/historical_records.md` — “Playwright Downloads Going to Wrong Folder in Batch Execution” (session ID and override ordering fixes)
