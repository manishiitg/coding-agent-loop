# Browser Automation

Coding Agent Loop uses the managed `agent_browser` tool for all browser
automation. The browser can run headlessly in the workspace or attach to a
user-visible Chrome through CDP.

## Modes

| Mode | Behavior | Typical use |
|---|---|---|
| `none` | Browser tools are disabled. | Workflows that do not browse. |
| `auto` | Use a reachable configured CDP browser; otherwise use headless. | Default. |
| `headless` | Start isolated workspace Chromium. | Background and scheduled runs. |
| `cdp` | Attach to the configured Chrome debugging port. | Existing logins, visual QA, and sites that reject headless browsers. |

The workflow manifest stores the mode under
`capabilities.browser_mode`. Browser steps attach the `agent-browser` skill.

## Starting a CDP browser

On macOS, install the default launcher on port `9222` with:

```bash
curl -fsSL 'https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/scripts/install-chrome-cdp-macOS.sh' | bash
```

Install another independent launcher/profile by passing a port:

```bash
curl -fsSL 'https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/scripts/install-chrome-cdp-macOS.sh' | bash -s -- --port 9333
```

Each CDP profile must use its own port and `--user-data-dir`. The usual port is
`9222`; the port-specific installer creates a separate application and profile.

For a specialized workflow that needs multiple login identities, launch more
profiles on different ports, for example `9222` and `9333`, then configure:

```json
{
  "browser_mode": "cdp",
  "cdp_ports": [9222, 9333]
}
```

The runtime accepts at most four configured ports. Ordinary workflow
concurrency does not require multiple profiles: workflows share one CDP browser
and use labeled tabs plus a per-port select-and-act lock.

## Managed tool

Do not run the `agent-browser` CLI through the shell for browser actions. Call
the managed `agent_browser` tool. The runtime injects and validates the CDP
endpoint, applies session limits, serializes shared-tab actions, and keeps file
access inside the workspace.

Before the first browser action, load the installed CLI's matching command guide:

```text
agent_browser(command="skills", args=["get", "core"])
```

The common flow is:

```text
agent_browser(command="open", args=["https://example.com"])
agent_browser(command="snapshot", args=["-i"])
agent_browser(command="click", args=["@e1"])
agent_browser(command="snapshot", args=["-i"])
```

In CDP mode, list or create a labeled tab first and include the tab inline for
every page action. `open` itself remains URL-only. The inline system prompt gives
the exact endpoint and argument form for the active session.

## State and isolation

- CDP mode uses the user's real Chrome cookies and login state.
- Headless mode starts fresh and should use screenshots to expose visual state.
- Shared CDP concurrency is isolated by workflow-owned labeled tabs.
- `share_browser=false` gives a sub-agent a separate agent-browser session.
- Do not close a CDP tab unless the user explicitly asks.

Browser session tracking lives in `agent_go/pkg/browser`. MCP subprocess
connection pooling in `mcpagent` is independent of browser state.

## Debugging and evidence

Use agent-browser's managed diagnostic commands so they operate on the same tab
and session as the workflow:

- `network` for requests and HAR capture;
- `console` and `errors` for page diagnostics;
- `screenshot` for visual evidence;
- `record` for video evidence when the user or workflow explicitly requests it;
- `trace` and `profiler` for deeper debugging.

HAR and video artifacts may contain credentials, cookies, page content, or
personal data. Review them before sharing.

## File uploads and downloads

Use workspace-relative paths such as `Downloads/report.pdf` or
`Chats/output.csv`. Upload with the `upload` command. Browser downloads for a
workflow run are routed into its execution `Downloads` directory.

## Operational rules

- Re-snapshot after interactions because snapshot refs are ephemeral.
- Persist durable selectors or parse fresh refs at runtime; never save a literal
  `@e1`-style ref in a workflow script.
- Poll for page state instead of relying on long fixed sleeps.
- Never connect to the CDP WebSocket directly for normal actions; that bypasses
  tab locking and can race other workflows.
- If a site rejects headless mode, change the workflow to `cdp` and record that
  precondition in its learnings.
