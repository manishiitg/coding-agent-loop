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

In CDP mode, list and reuse a suitable tab before asking to create one. Include
the returned real tab ID (`t1`, `t2`, and so on) inline for every page action.
`open` itself remains URL-only. The inline system prompt gives the exact
endpoint and argument form for the active session.

## Shared CDP tab lifecycle

One visible Chrome is shared safely by selecting and acting under a per-port
lock. A workflow must not assume that the tab selected during its previous tool
call is still active: the user, the website, or another workflow may have
changed Chrome in the meantime. The backend therefore explicitly reselects the
resolved real `tN` tab immediately before every page action while it holds the
shared lock.

The normal flow is:

1. Call `agent_browser(command="tab", args=["--cdp", "<endpoint>"])` once to
   inspect real tab IDs and query-free display URLs.
2. Reuse the workflow's already-owned labeled tab when one exists. It may be
   navigated to the requested URL.
3. Otherwise, reuse a pre-existing tab only when its normalized URL exactly
   matches the requested URL.
4. If neither matches, request a stable labeled tab with
   `agent_browser(command="tab", args=["--cdp", "<endpoint>", "new",
   "--label", "<workflow-label>", "https://target.example"])`.
5. Keep the returned real `tN` and provide it inline on subsequent actions.

The backend repeats the list-and-reuse check atomically before executing
`tab new`. It refuses creation if the real tab list is unavailable or invalid,
rather than risking a duplicate. A label collision with a pre-existing tab at a
different URL is also an error. An arbitrary same-origin tab is deliberately
not reused because navigating it could destroy unrelated user state. URL query
parameters are hidden from model-facing tab lists, but the backend retains the
full normalized URL for exact-match decisions.

`tab new` arguments are parsed and rewritten into the canonical
`new --label <label> <absolute-url>` order before reaching agent-browser. This
prevents a misplaced URL or option from being interpreted as the page to open.

### Model-context behavior

Tab management is intentionally compact:

| Operation | Returned to the agent |
|---|---|
| Explicit `tab` list | At most 20 compact lines; labels, titles, and URLs are individually truncated. |
| Select one tab | A short selected-tab message, not the raw tab list. |
| Automatic selection before a page action | Nothing extra; the internal selection response is discarded. |
| Atomic reuse check before `tab new` | Nothing extra; only the reused/created tab summary is returned. |

Consequently, a large Chrome window does not add every tab to context on every
browser action. Repeated explicit list calls can still accumulate in the
conversation history, so agents should list once, retain the returned `tN`, and
list again only when the tab disappears or the target is genuinely unknown.

### Ownership and cleanup

Only tabs actually created by a workflow are registered for automatic cleanup,
and ownership is recorded against the real `tN` ID returned by agent-browser.
A pre-existing tab reused by exact URL remains user-owned and is never enrolled
in cleanup.

After the final browser-owner lease is released, created tabs remain available
for review for one hour and are then closed by real `tN` ID. Concurrent runs
delay that timer until the final lease ends. Already-missing tabs, including
agent-browser's `No tab with label` response, are retired from the registry
instead of being retried forever. Never call the top-level browser `close` in
CDP mode because it can terminate the user's real Chrome session. Close a
specific workflow-owned tab immediately only when the user requests it or the
workflow must replace it.

## State and isolation

- CDP mode uses the user's real Chrome cookies and login state.
- Headless mode starts fresh and should use screenshots to expose visual state.
- Shared CDP concurrency is isolated by real tab IDs plus a per-port
  select-and-act lock; labels are aliases, not durable tab identities.
- `share_browser=false` gives a sub-agent a separate agent-browser session.
- Workflow-created CDP tabs are closed automatically one hour after the final
  run releases its lease; reused user tabs are preserved.

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

### Persistent browser artifact handoff

The agent-browser daemon may outlive the workflow process and therefore cannot
safely rely on that process's current directory or inherited FolderGuard. For a
named screenshot or recording, the managed adapter rewrites the browser output
to a unique file under `/tmp/agentworks-browser-artifacts`. The trusted
workspace server then validates that the staged file is regular, non-empty, of
the expected image/video type, and that the requested destination is covered by
the current request's write paths and is not blocked. It publishes the artifact
atomically into the workflow workspace and removes the staged source.

Screenshots are finalized in the same call. Video recording uses an
owner-and-session-scoped lease: `record start` stores the staged source and
`record stop` finalizes that exact source into the requested workspace path.
This handoff applies to both headless and CDP modes.

## Recent failure findings and fixes

| Finding | User-visible symptom | Current fix |
|---|---|---|
| A tab label was sometimes stored as though it were a real tab ID. | Delayed cleanup called `tab close <label>`, failed, and tabs remained open. | Parse both direct `tab new` and tab-list JSON, persist the returned real `tN`, and treat missing-label errors as already cleaned. |
| Cached backend active-tab state was trusted between calls. | A page action could affect the wrong tab after Chrome changed externally. | Explicitly select the resolved `tN` before every page action under the shared lock. |
| Agents could request `tab new` without a fresh reuse decision. | Repeated workflows accumulated duplicate tabs. | Perform an atomic owned-tab/exact-URL reuse check; fail closed when listing is unavailable. |
| Flexible or malformed `tab new` argument ordering could reach the CLI. | A new tab sometimes opened an unintended URL. | Validate an absolute URL and canonicalize the command before execution. |
| Raw tab output was suspected of entering context on every selection. | Concern about context growth with many Chrome tabs. | Return tab lists only for explicit list calls, cap them at 20 compact entries, and discard internal selection/reuse responses. |
| A persistent daemon resolved named evidence paths from stale sandbox state. | Named screenshots or recordings failed with path/`getcwd` errors. | Use the guarded staging-and-finalization artifact handoff described above. |
| Upload paths were forwarded unchanged while the command working directory pointed at the run Downloads folder. | Workspace-relative paths could resolve as `Downloads/Workflow/...`, and a daemon launched by an older step could not see a newly granted input folder. | Resolve and authorize upload sources in workspace-api, copy them into short-lived managed staging with the original basename, and remove staging after the command. |
| CSS ID selectors were not shell-quoted. | `upload #file path` was parsed by the shell as a comment and agent-browser reported missing arguments. | Treat `#`, backslashes, and home-prefix characters as shell-sensitive arguments and quote them. |
| Brokered output destinations were joined to the browser working directory. | A requested `Downloads/report.csv` could be published as `Downloads/Downloads/report.csv`. | Resolve brokered screenshot/video/download destinations once from the workspace root. |

## Live E2E contract

Run the real managed-browser contract with:

```bash
scripts/run-browser-e2e.sh
```

The test launches a dedicated temporary headless Chrome profile on a random CDP
port. It then exercises the production path from the managed executor, through
the real workspace `/api/execute` handler, into the installed agent-browser CLI
and Chrome. Two simulated workflow owners share that same CDP daemon and issue
overlapping requests. It verifies:

- exact-URL reuse does not create a duplicate or claim a user-owned tab;
- flexible input is canonicalized before a new tab opens;
- newly created tabs are tracked by their real `tN` IDs;
- changing Chrome's active tab externally cannot redirect the next managed
  action;
- selecting one tab returns a compact response rather than the all-tabs JSON;
- parallel page actions are re-selected onto each workflow's own `tN` tab;
- uploads from two newly granted, disjoint workspace trees cross an older daemon
  sandbox while preserving both filename and file content;
- cross-workflow upload reads and artifact writes are rejected by FolderGuard;
- parallel screenshots and explicit downloads are published only into each
  workflow's authorized evidence/Downloads folders and have valid content;
- video recording produces real WebM files, remains exclusive to one workflow
  at a time, rejects another workflow's start/stop calls, and transfers cleanly
  to the next workflow after the first recording stops;
- shared reset is rejected while another workflow owns the CDP port;
- delayed cleanup closes each workflow's created tab independently and preserves
  both the other live workflow tab and the reused pre-existing user tab.

The test never attaches to the default port or normal Chrome profile. Override
Chrome discovery with `BROWSER_E2E_CHROME_BINARY=/path/to/chrome` when needed.

## File uploads and downloads

Use workspace-relative paths such as `Downloads/report.pdf` or
`Chats/output.csv`. Upload with the `upload` command. Browser downloads for a
workflow run are routed into its execution `Downloads` directory.

Upload paths are not passed directly to the persistent daemon. The workspace
server resolves each source against the workspace root (with a run working-dir
fallback for a bare filename), checks the current FolderGuard read grants,
rejects blocked paths and symlinks, and copies the file into short-lived managed
staging. The daemon receives that staged path with the original basename, and
the staging slot is removed as soon as the command finishes. This allows a
later workflow step to upload from its own authorized folder even if the daemon
was originally launched under a different step's sandbox.

There are two CDP download paths:

- A normal click in visible Chrome may place a file in the user's system
  Downloads folder. That folder is exposed read-only when explicitly granted;
  copy the required file into the run-scoped workspace before processing it.
- `agent_browser(command="download", args=[..., "<selector>",
  "<workspace-path>"])` is an explicit managed download. Its output is written
  to backend staging and atomically published into the requested authorized
  workspace path, like screenshots. It never writes through the persistent
  daemon directly into an arbitrary workspace folder.

## Operational rules

- Re-snapshot after interactions because snapshot refs are ephemeral.
- Persist durable selectors or parse fresh refs at runtime; never save a literal
  `@e1`-style ref in a workflow script.
- Poll for page state instead of relying on long fixed sleeps.
- Never connect to the CDP WebSocket directly for normal actions; that bypasses
  tab locking and can race other workflows.
- If a site rejects headless mode, change the workflow to `cdp` and record that
  precondition in its learnings.
