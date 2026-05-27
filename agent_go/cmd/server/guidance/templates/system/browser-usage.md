## Browser Automation

Read this skill when you need to drive a real browser — open pages, click,
fill forms, take screenshots, upload files, scrape interactive sites, or
log into authenticated pages. The exact mode the session is in (CDP,
headless, Playwright) is announced in the inline browser block of your
system prompt; skim that for the mode-specific behaviors, then use the
unified API below.

## Three modes

| Mode | Browser | Visibility | Logins / cookies |
|---|---|---|---|
| **CDP** (`agent_browser` with `--cdp`) | The user's real Chrome via Chrome DevTools Protocol | User sees every action | Existing cookies + sessions are available — leverage them |
| **Headless** (`agent_browser`) | Container-side Chromium | Invisible to user; take screenshots | Fresh each time, no cookies |
| **Playwright** (`browser_*` MCP tools) | Container-side Chromium driven by Playwright | Invisible to user; use snapshots | Fresh each time |

Pick from the mode that's active in this session — do not mix `agent_browser`
calls with Playwright `browser_*` calls.

## `agent_browser` (CDP and headless)

Call via HTTP API. CDP sessions also need `--cdp http://<host>:<port>` in args
(`<host>` is `localhost` in native mode, `host.docker.internal` in Docker).

```python
import requests, os
BROWSER = os.environ["MCP_API_URL"] + "/tools/mcp/workspace_browser/agent_browser"
HEADERS = {"Authorization": f"Bearer {os.environ['MCP_API_TOKEN']}", "Content-Type": "application/json"}

def browser(command, args=None, session="default"):
    resp = requests.post(BROWSER, json={"command": command, "args": args or [], "session": session}, headers=HEADERS, timeout=120)
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

**Key commands:** `open`, `snapshot`, `click`, `fill`, `type`, `press`,
`screenshot`, `wait`, `get`, `scroll`, `select`, `hover`, `upload`,
`download`, `close`, `eval`, `back`, `forward`, `reload`, `reset`.

For exhaustive command docs run:
`execute_shell_command(command="agent-browser skills get core")`.

### CDP-specific rules

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
- Direct CDP WebSocket access is allowed only for read-only diagnostics that
  `agent_browser eval` can't cover. Always pass `Host: localhost` and
  `suppress_origin=True`. Do not use raw CDP for navigation, clicks, or
  multi-page loops.

### Headless-specific rules

- Browser is **fresh** — login from scratch when sites require auth.
- User cannot see the browser. **Take screenshots** to surface progress.
- Free to open/close tabs/sessions; state resets between runs.
- Use `browser("reset")` only when the daemon is genuinely broken; otherwise
  it wastes time.

## `browser_*` (Playwright MCP)

When the session is in Playwright mode, the `agent_browser` tool is NOT
available — use the Playwright MCP tools instead:

- `browser_snapshot` — inspect page, get refs/selectors.
- `browser_click(selector | ref)`, `browser_type`, `browser_press` — interactions.
- `browser_screenshot` — visual evidence.
- `browser_file_upload(paths=[...], selector="input[type=file]")` — uploads.

Keep interactions deterministic: snapshot → act → snapshot.

## File uploads

- **CDP / headless** (`agent_browser`):
  `browser("upload", ["@ref", "Downloads/report.pdf"])` (CDP also takes the
  tab inline: `["tab", "t1", "@ref", "Downloads/report.pdf"]`).
- **Playwright:** `browser_file_upload(paths=["Downloads/report.pdf"], selector="input[type=file]")`.

Always use **workspace-relative paths** (e.g. `Downloads/report.pdf`,
`Chats/output.csv`). The harness rewrites them to absolute container paths;
do not construct absolute paths yourself. To upload a file you generated,
write it to `Chats/` via `execute_shell_command`, then upload that path.

## Session limits

- Default per-agent / per-workflow / global concurrency caps are enforced
  by the runtime — keep one browser open at a time per agent. Re-use the
  same session name across calls within one task.
- When multiple parallel agents need browsers, each MUST use a **unique
  session name** (e.g. `session="twitter_research"`,
  `session="linkedin_lookup"`). Two agents sharing `session="default"` end
  up driving the same browser.

## Common mistakes

- Calling `open` with `["tab", "t1", url]` in CDP — `open` is URL-only.
- Closing the CDP browser at the end of a task — kills the user's tab.
- Forgetting to re-snapshot after every interaction in headless/CDP — refs
  go stale.
- Connecting directly to CDP WebSocket for click/fill/navigate — bypasses
  the shared tab lock and races with other workflows.
- Hardcoding absolute paths in `upload` — always use workspace-relative.
- Calling `agent_browser` and `browser_*` interchangeably — they belong to
  different modes; pick one.
