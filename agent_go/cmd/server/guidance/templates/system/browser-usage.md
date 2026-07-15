## Browser Automation

Read this skill when you need to drive a real browser — open pages, click,
fill forms, take screenshots, upload files, scrape interactive sites, or
log into authenticated pages. The exact mode the session is in (CDP or
headless) is announced in the inline browser block of your
system prompt; skim that for the mode-specific behaviors, then use the
unified API below.

## Two modes

| Mode | Browser | Visibility | Logins / cookies |
|---|---|---|---|
| **CDP** (`agent_browser` with `--cdp`) | The user's real Chrome via Chrome DevTools Protocol | User sees every action | Existing cookies + sessions are available — leverage them |
| **Headless** (`agent_browser`) | Container-side Chromium | Invisible to user; take screenshots | Fresh each time, no cookies |

## Version-matched agent-browser skills

For CDP/headless mode, the installed `agent-browser` CLI is the source of
truth for commands and flags. Before the first browser action, load its current
core overview through the managed tool:

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

Call via HTTP API. CDP sessions also need the exact
`--cdp http://<host>:<port>` endpoint announced in the inline system prompt on
every call. The backend validates the port and canonicalizes the host; a
headless run cannot switch itself to a model-selected CDP endpoint.

```python
import requests, os
BROWSER = os.environ["MCP_API_URL"] + "/tools/mcp/workspace_browser/agent_browser"
HEADERS = {"Authorization": f"Bearer {os.environ['MCP_API_TOKEN']}", "Content-Type": "application/json"}
# Headless: []. CDP: use the exact ["--cdp", "<endpoint>"] announced in
# the inline system prompt. Do not invent or probe another endpoint.
BROWSER_PREFIX = []

def browser(command, args=None, session="default"):
    resp = requests.post(BROWSER, json={"command": command, "args": BROWSER_PREFIX + (args or []), "session": session}, headers=HEADERS, timeout=120)
    resp.raise_for_status()
    return resp.json().get("result", "")

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
- Call `browser("tab", [])` to list real tabs; if no tab is selected yet:
  `browser("tab", ["new", "--label", "<workflow-label>", "https://target"])`.
- **Do NOT call `close`** unless the user asks — it kills the user's tab.
- Avoid actions that bring Chrome to the foreground while the user is typing
  (navigation, tab switching, large screenshots). For unattended schedules,
  prefer headless mode.
- Keep diagnostics inside `agent_browser`; raw CDP bypasses the shared-tab
  lock and is not allowed for normal navigation or debugging.

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
`Chats/output.csv`). Do not construct absolute paths yourself. To upload a
file you generated, write it to `Chats/` via `execute_shell_command`, then
upload that path.

## Session limits

- Default per-agent / per-workflow / global concurrency caps are enforced
  by the runtime — keep one browser open at a time per agent. Re-use the
  same session name across calls within one task.
- In headless mode, parallel agents need unique session names. In shared CDP
  mode, sessions are intentionally remapped to one per port; isolation comes
  from workflow-owned labeled tabs plus the per-port select-and-act lock.

## Common mistakes

- Calling `open` with `["tab", "t1", url]` in CDP — `open` is URL-only.
- Closing the CDP browser at the end of a task — kills the user's tab.
- Forgetting to re-snapshot after every interaction in headless/CDP — refs
  go stale.
- Connecting directly to CDP WebSocket for click/fill/navigate — bypasses
  the shared tab lock and races with other workflows.
- Omitting the configured `--cdp` prefix or inventing another port — the
  backend rejects the call to keep prompt and runtime configuration aligned.
- Hardcoding absolute paths in `upload` — always use workspace-relative.
