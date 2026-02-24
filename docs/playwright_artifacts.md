# Playwright MCP artifacts and output location

## Summary

When using the Playwright MCP server with `--output-dir` (e.g. `workspace-docs/Downloads` or a workflow `execution/Downloads`), **downloads and auto-generated artifacts** go to that directory. **Custom filenames** (e.g. `screenshot.png`, `snapshot.html`) used to be written to the workspace or process root because Playwright MCP resolved them relative to the process cwd ([upstream issue #1390](https://github.com/microsoft/playwright-mcp/issues/1390)).

## Fix in this repo

The MCP client (mcpagent) now supports a **working directory** for stdio servers:

1. **`MCPServerConfig.WorkingDir`** and **`RuntimeConfigOverride.WorkingDir`** — when set, the Playwright MCP subprocess is started with this directory as its current working directory, so relative custom filenames resolve there.
2. **Workflow orchestrator** — when configuring Playwright for a run, it sets both `--output-dir` and `WorkingDir` to the same path (e.g. `runs/{runFolder}/execution/Downloads`). So screenshots/snapshots with custom names go to the same folder as downloads.

So in **workflow runs**, Playwright artifacts (including custom-named screenshots/snapshots) should now land in the run’s `execution/Downloads` folder.

## Static config (e.g. chat mode)

For non-workflow usage you can set `working_dir` in the Playwright server config so the subprocess cwd matches your `--output-dir`:

```json
"playwright": {
  "command": "npx",
  "args": ["@playwright/mcp@latest", "--output-dir", "/path/to/workspace-docs/Downloads", "--isolated"],
  "working_dir": "/path/to/workspace-docs/Downloads"
}
```

## Fallback: `.gitignore`

The repo root `.gitignore` still includes patterns for Playwright artifacts at repo/agent_go root, in case any path still resolves outside the output dir.

## Upstream

- Issue: [browser_take_screenshot: custom filenames bypass --output-dir](https://github.com/microsoft/playwright-mcp/issues/1390) (closed with “run MCP with cwd pointing to output location”)
- Check for a fixed `@playwright/mcp` release and upgrade if available.
