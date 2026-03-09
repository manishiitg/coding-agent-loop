# Playwright MCP Artifacts and Output Location

## Summary

When using the Playwright MCP server, handling downloads and auto-generated artifacts (like screenshots) requires careful configuration of the working directory and output paths to ensure files are accessible to the agent and the workspace.

## The Problem

By default, `@playwright/mcp` might save artifacts in the current working directory of the process that spawned it. In a containerized environment, this often leads to files being lost in temporary directories or scattered across the repository root, making them invisible to the agent's file-searching tools.

## The Solution: `working_dir` Injection

The project uses a custom MCP client that supports a `working_dir` property in the server configuration. This ensures that the Playwright process is launched with its base path set correctly.

### Configuration (`agent_go/configs/mcp_servers_clean.json`)

```json
"playwright": {
  "command": "npx",
  "args": [
    "@playwright/mcp@latest",
    "--output-dir",
    "../workspace-docs/Downloads",
    "--isolated"
  ],
  "working_dir": "../workspace-docs/Downloads"
}
```

### Key Parameters:
1.  **`working_dir`**: Sets the OS-level working directory for the spawned `npx` process.
2.  **`--output-dir`**: Specifically tells Playwright where to save browser snapshots and traces.
3.  **`--isolated`**: Ensures that browser data (cookies, storage) doesn't leak between sessions.

## Benefits

1.  **Visibility**: Artifacts land in `workspace-docs/Downloads`, which is indexed by the agent's semantic search and listed by the `list_directory` tool.
2.  **Persistence**: Files survive session restarts because they are stored in the persistent `workspace-docs` volume.
3.  **Cleanliness**: Prevents the repository root from being cluttered with `screenshot_123.png` or `download.pdf`.

## Interaction with CDP Mode

When using **CDP Mode** (connecting to your local Chrome), the `working_dir` injection is still active. This means even if the agent is controlling your local browser, any downloads it triggers via Playwright tools will still be routed into the designated workspace folder.

## Troubleshooting

### "File not found" after download
Check the `working_dir` path in your MCP config. If it's relative, it's relative to the `agent_go` directory. Ensure the path exists and has write permissions.

### Duplicate filenames
The Playwright MCP server typically appends a timestamp or UUID to screenshots to prevent overwriting. If you need a specific filename, you must use the `take_screenshot` tool which supports custom paths.
