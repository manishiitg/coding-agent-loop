## Browser Automation

Read this skill when you need to drive a real browser — open pages, click,
fill forms, take screenshots, upload files, scrape interactive sites, or
log into authenticated pages. Browser configuration is declared by the
workflow, but CDP reachability is live state. Query `agent_browser status`
before first use instead of relying on saved conversation or prompt state.

## Two modes

| Mode | Browser | Visibility | Logins / cookies |
|---|---|---|---|
| **CDP** (`agent_browser` with `--cdp`) | The user's real Chrome via Chrome DevTools Protocol | User sees every action | Existing cookies + sessions are available — leverage them |
| **Headless** (`agent_browser`) | Container-side Chromium | Invisible to user; take screenshots | Fresh each time, no cookies |

## Version-matched agent-browser skills

First call the managed live-status operation. It does not launch a browser and
does not require `--cdp`:

```python
status = browser_raw("status", [])
```

Use `effective_mode` and `authorized_endpoints` from that response to set the
prefix for later calls. Then load the installed CLI's current core overview:

```python
browser("skills", ["get", "core"])
```

The helper below automatically prepends the configured CDP endpoint in CDP
mode. Use `browser("skills", ["list"])` to discover specialized skills, and
load one only when needed—for example `dogfood` for
exploratory QA/bug hunts or `electron` for Electron desktop applications.
Use `core --full` only when the overview lacks an exact command or flag.
Upstream skill examples use shell syntax; translate them into managed
`agent_browser` tool calls. Never execute browser actions through shell.

## `agent_browser` (CDP and headless)

Call via HTTP API. CDP sessions need an exact
`--cdp http://<host>:<port>` endpoint returned by live status on every later
call. The backend validates the configured port, rechecks auto-mode
reachability, and canonicalizes the host; a headless run cannot switch itself
to a model-selected CDP endpoint.

```python
import json, requests, os
BROWSER = os.environ["MCP_API_URL"] + "/tools/mcp/workspace_browser/agent_browser"
HEADERS = {"Authorization": f"Bearer {os.environ['MCP_API_TOKEN']}", "Content-Type": "application/json"}
def browser_raw(command, args=None, session="default"):
    resp = requests.post(BROWSER, json={"command": command, "args": args or [], "session": session}, headers=HEADERS, timeout=120)
    resp.raise_for_status()
    return resp.json().get("result", "")

status = json.loads(browser_raw("status"))
# Headless: []. CDP: ["--cdp", first live authorized endpoint].
# Never invent or probe another endpoint.
BROWSER_PREFIX = (
    ["--cdp", status["authorized_endpoints"][0]]
    if status["effective_mode"] == "cdp" else []
)

def browser(command, args=None, session="default"):
    return browser_raw(command, BROWSER_PREFIX + (args or []), session)

# Standard flow
browser("open", ["https://example.com"])
snap = browser("snapshot", ["-i"])      # interactive elements as @e1, @e2, ...
browser("click", ["@e1"])
browser("fill", ["@e2", "search query"])
browser("press", ["Enter"])
snap = browser("snapshot", ["-i"])      # re-snapshot after every interaction
browser("screenshot", ["page.png"])
```

**Key commands:** `skills`, `open`, `snapshot`, `click`, `fill`, `type`, `press`,
`screenshot`, `wait`, `get`, `scroll`, `select`, `hover`, `upload`,
`download`, `eval`, `network`, `console`, `errors`, `record`, `trace`,
`profiler`, `close`, `back`, `forward`, `reload`, `reset`.

For exhaustive command docs call `browser("skills", ["get", "core", "--full"])`.

### CDP-specific rules

- The inline prompt may authorize multiple `--cdp` endpoints only for a
  specialized multi-login workflow. Each endpoint represents a different
  Chrome `--user-data-dir`/login identity. Choose the intended endpoint on
  every call and keep distinct labeled tabs per account. Normal concurrent
  workflows should share one CDP browser.
- `open` is URL-only: `browser("open", ["https://target"])`. Do **not** pass
  `["tab", "t1", url]` to `open`.
- For every page action after `open` (snapshot/click/fill/eval/wait/screenshot),
  include `["tab", "<tab-id-or-label>", ...]` inline so the action runs against
  a known tab and respects the shared CDP lock.
- Call `browser("tab", [])` to list real tab ids and query-free URLs. Reuse the workflow's existing owned tab or an exact target-URL match; if neither exists:
  `browser("tab", ["new", "--label", "<workflow-label>", "https://target"])`.
- `tab new` performs the exact-URL reuse check again atomically and returns the real `tN` id plus actual URL. If listing is unavailable, it refuses creation instead of risking a duplicate.
- Never call the top-level `close` command in CDP mode; it can disrupt the user's real Chrome session.
- At normal workflow completion, leave workflow-created labeled tabs open for review. The backend automatically closes only their registered real `tN` ids one hour after the final run releases its browser lease; it never includes pre-existing or exact-URL-reused user tabs.
- Use `browser("tab", ["close", "<owned-label>"])` only when the user explicitly requests immediate cleanup or the workflow must replace one of its own labeled tabs. Never close a pre-existing user tab.
- The backend avoids a redundant tab switch when the requested CDP tab is
  already active, so normal actions should not repeatedly steal OS focus.
  Creating or genuinely switching tabs can still bring Chrome forward; avoid
  unnecessary tab changes while the user is typing. For unattended schedules,
  prefer headless mode.
- Keep diagnostics inside `agent_browser`; raw CDP bypasses the shared-tab
  lock and is not allowed for normal navigation or debugging.

### Installing a CDP browser on macOS

When the user wants CDP but no configured endpoint is available, provide the
dedicated installer command. Do not run it yourself unless the user explicitly
asks you to install and open a visible local browser:

```bash
curl -fsSL 'https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/scripts/install-chrome-cdp-macOS.sh' | bash
```

For a specialized additional login identity, pass an unused port to the same
installer. It creates a separate app and `--user-data-dir`:

```bash
curl -fsSL 'https://raw.githubusercontent.com/manishiitg/coding-agent-loop/main/scripts/install-chrome-cdp-macOS.sh' | bash -s -- --port 9333
```

After installation, agents verify reachability with `agent_browser status`;
they must not probe Chrome's `/json/version` endpoint through shell.

After it is reachable, configure the workflow with that port, for example
`cdp_ports=[9222,9333]`. Do not add profiles for ordinary workflow concurrency.

### QA evidence and network debugging

- Requests: `browser("network", ["tab", "<label>", "requests"])`.
- Console/errors: `browser("console", ["tab", "<label>"])` and
  `browser("errors", ["tab", "<label>"])`.
- HAR capture: `browser("network", ["tab", "<label>", "har", "start"])`,
  reproduce, then stop to a workspace-relative file. HAR files can contain
  authorization headers, cookies, and response bodies; review before sharing.
- Video evidence: `browser("record", ["tab", "<label>", "start", "qa.webm"])`,
  reproduce, then `browser("record", ["tab", "<label>", "stop"])`. Recording
  must be requested by the user/workflow because visible secrets may be captured.

### Headless-specific rules

- Browser is **fresh** — login from scratch when sites require auth.
- User cannot see the browser. **Take screenshots** to surface progress.
- Free to open/close tabs/sessions; state resets between runs.
- Use `browser("reset")` only when the daemon is genuinely broken; otherwise
  it wastes time.

## File uploads

- Use `agent_browser`:
  `browser("upload", ["@ref", "Downloads/report.pdf"])` (CDP also takes the
  tab inline: `["tab", "t1", "@ref", "Downloads/report.pdf"]`).

Always use **workspace-relative paths** (e.g. `Downloads/report.pdf`,
`Chats/output.csv`). Do not construct absolute paths yourself. The backend
checks the current read grants and stages uploads for the persistent daemon.
To upload a file you generated, write it to `Chats/` via
`execute_shell_command`, then upload that path.

For an explicit managed download, pass a workspace-relative authorized output
path: `browser("download", ["@ref", "Downloads/report.pdf"])` (plus the CDP
tab inline when applicable). The backend stages and publishes the completed
file. A normal click in visible Chrome may instead download into the host
Downloads folder, which is read-only to the agent.

## Session limits

- Default per-agent / per-workflow / global concurrency caps are enforced
  by the runtime — keep one browser open at a time per agent. Re-use the
  same session name across calls within one task.
- In headless mode, parallel agents need unique session names. In shared CDP
  mode, sessions are intentionally remapped to one per port; isolation comes
  from workflow-owned labeled tabs plus the per-port select-and-act lock.

## Common mistakes

- Calling `open` with `["tab", "t1", url]` in CDP — `open` is URL-only.
- Calling top-level `close` in CDP mode, or manually closing a pre-existing user tab. Workflow-created labeled tabs are cleaned up automatically after the one-hour review window.
- Forgetting to re-snapshot after every interaction in headless/CDP — refs
  go stale.
- Connecting directly to CDP WebSocket for click/fill/navigate — bypasses
  the shared tab lock and races with other workflows.
- Omitting the configured `--cdp` prefix or inventing another port — the
  backend rejects the call to keep prompt and runtime configuration aligned.
- Hardcoding absolute paths in `upload` — always use workspace-relative.
