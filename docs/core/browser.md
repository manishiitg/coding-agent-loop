# Browser System

Complete reference for browser automation in mcp-agent-builder-go — session limits, CDP local browser, Playwright artifacts, and known bugs.

---

## Table of Contents

- [Session Limit Manager](#session-limit-manager)
- [CDP Local Browser Connection](#cdp-local-browser-connection)
- [Playwright Artifacts and Output Location](#playwright-artifacts-and-output-location)
- [Browser Session Identity Split Plan](#browser-session-identity-split-plan)
- [Known Bugs](#known-bugs)

---

## Session Limit Manager

The session tracker prevents unbounded browser process growth by enforcing per-workflow and global limits.

**Source:** `agent_go/pkg/browser/session_tracker.go`

### Limits

| Limit | Value | Enforcement |
|-------|-------|-------------|
| Per workflow/chat | `MaxBrowserSessionsPerChat = 1` | Returns error to LLM — must close existing session first |
| Global (all sessions) | `MaxBrowserSessionsGlobal = 8` | Auto-evicts oldest (LRU) session |

### How It Works

1. **Registration:** When the LLM calls `agent_browser(command="open", ...)`, the executor extracts the `chatSessionID` from context and calls `tracker.CheckLimits()`.
2. **Per-chat check:** If the workflow already has 1 active session, returns an error:
   ```
   ERROR: Cannot open browser session "<name>" — you already have 1 active browser sessions
   (max 1 per workflow). Active sessions: [...]. Close one first using
   agent_browser(command="close", session="<name>") before opening a new one.
   ```
3. **Global check:** If 8+ sessions exist globally, the oldest (least-recently-used) session is auto-closed.
4. **Reuse:** If the named session already exists in the tracker, it's a reuse — always allowed.
5. **Touch:** Every `agent_browser` call updates the session's `lastUsed` timestamp.

### Session Lifecycle

```
1. Workflow starts
   └─ Browser tool executor created with chatSessionID in context

2. LLM calls agent_browser(command="open", session="my_session")
   └─ CheckLimits() validates per-chat (< 1) and global (< 8)
   └─ Touch() registers session with timestamps

3. During workflow
   └─ Each agent_browser call updates lastUsed via Touch()

4. Workflow ends (stop/clear/completion)
   └─ CloseAllForChat(sessionID) closes all browser processes
   └─ RemoveAllForChat() removes tracker entries

5. Server restart
   └─ In-memory tracker cleared
   └─ Kill-all sent to workspace-api for orphaned chromium processes
```

### Tracking Data Structure

```go
type browserSessionInfo struct {
    browserSession string    // e.g., "twitter_research"
    chatSessionID  string    // owning workflow/chat session
    lastUsed       time.Time
    createdAt      time.Time
}
```

### Key Functions

| Function | Purpose |
|----------|---------|
| `Touch(browserSession, chatSessionID)` | Register or update last-used time |
| `CheckLimits(browserSession, chatSessionID)` | Validate per-chat and global limits |
| `CountForChat(chatSessionID)` | Count active sessions for a workflow |
| `SessionsForChat(chatSessionID)` | List browser session names for a workflow |
| `GetOldestSession()` | Find LRU session globally (for auto-eviction) |
| `GetOldestSessionForChat(chatSessionID)` | Find LRU session for a specific workflow |
| `RemoveAllForChat(chatSessionID)` | Remove all tracker entries for a workflow |
| `CloseAllForChat(chatSessionID, client)` | Close browser processes + remove entries |
| `Clear()` | Remove all tracked sessions (server restart) |

### Key Files

| File | Role |
|------|------|
| `agent_go/pkg/browser/session_tracker.go` | Core tracker with limits |
| `agent_go/pkg/browser/executor.go` | Limit check + enforcement on each command |
| `agent_go/cmd/server/server.go` | Cleanup on workflow stop (`cleanupBrowserSessions`) |
| `agent_go/cmd/server/virtual-tools/workspace_browser_tools.go` | Injects chatSessionID into context |
| `workspace/handlers/browser_session_tracker.go` | Workspace-side tracker (code execution mode) |

### Frontend Monitoring

`frontend/src/components/workspace/BrowserProcesses.tsx` displays:
- Active sessions grouped by process
- Age, idle time, CPU/memory usage
- Buttons to kill individual sessions or cleanup all
- Orphaned session detection

API endpoints: `/api/browser/sessions` (agent_go), `/api/browser/processes` (workspace-api).

---

## CDP Local Browser Connection

Connect the agent's browser tools to your real Chrome browser via Chrome DevTools Protocol (CDP), allowing you to watch the agent navigate in real-time and reuse your logged-in sessions.

### Overview

By default, browser-based MCP servers (like `playwright` or `agent-browser`) run an isolated, headless browser inside a container. By using **CDP mode**, you can point these tools to a Chrome instance running on your host machine.

**Foreground behavior:** CDP is connected to a visible real Chrome window. Browser actions may bring Chrome to the foreground and steal keyboard focus from the user. This is expected for shared visible Chrome and is separate from tab isolation. Use headless mode for background-safe runs, or run schedules against a dedicated automation Chrome profile/port instead of the user's primary Chrome.

### How it Works

1. **Launch Chrome with Remote Debugging**: You start Chrome on your host with a specific port (default `9222`).
2. **Connection String**: The agent is given a CDP URL (e.g., `http://host.docker.internal:9222`).
3. **Connectivity Check**: The frontend verifies the connection before starting the session.
4. **Shared browser session**: In CDP mode, `agent_browser` commands are remapped to a shared raw agent-browser session per CDP port, e.g. `shared-cdp-9222`. This lets multiple workflows reuse the same Chrome while still forcing each command to name the tab it intends to use.

### Shared CDP Tabs

Native agent-browser tracks an active tab per `--session`. In a shared workflow environment that is too implicit: whichever workflow last selected a tab can influence the next page action. The project wrapper therefore requires an explicit tab for CDP work. Use the `tab` command to choose/create the tab, call `open` with URL-only args, then include an inline tab argument on later page actions.

Allowed page action forms:

```json
{"command": "tab", "args": ["profile"], "session": "workflow_a"}
{"command": "open", "args": ["https://example.com"], "session": "workflow_a"}
{"command": "snapshot", "args": ["tab", "profile", "-i"], "session": "workflow_a"}
{"command": "click", "args": ["--tab", "profile", "@e1"], "session": "workflow_a"}
{"command": "eval", "args": ["tab", "profile", "document.title"], "session": "workflow_a"}
```

If a CDP page action omits the tab, the tool returns an error with the current tab list. The LLM is expected to choose an existing tab or create a labeled tab, then retry the command. `open` is the exception: it uses the workflow's previously selected tab and passes only the URL to agent-browser.

Tab management commands:

```json
{"command": "tab", "args": [], "session": "workflow_a"}
{"command": "tab", "args": ["new", "--label", "profile", "https://example.com"], "session": "workflow_a"}
{"command": "tab", "args": ["profile"], "session": "workflow_a"}
```

Operational rules:

- List tabs first and reuse an existing suitable tab by URL/title whenever possible.
- Create one stable labeled tab only when no current tab matches the task.
- Do not close user tabs unless explicitly requested.
- Do not rely on "latest tab" or session active-tab state for page actions.
- The wrapper serializes `select tab -> action` with a per-CDP-port mutex. Two page commands on the same CDP port do not interleave; different CDP ports are independent.

### Configuration

#### Launch Chrome (Host)

**macOS:**
```bash
/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome --remote-debugging-port=9222
```

**Windows:**
```cmd
"C:\Program Files\Google\Chrome\Application\chrome.exe" --remote-debugging-port=9222
```

#### Connection Logic

The system uses a two-tier connectivity check (`frontend/src/services/api.ts` and `agentApi.checkCdpPort`):

1. **Agent API Check**: Calls `agent_go`'s `/api/cdp-check` — TCP dial from agent server to the port.
2. **Workspace API Check**: If running in Docker, also tries the Workspace API's `/api/cdp-check` — critical because browser tools execute inside the workspace container (different network view).

### Frontend UI Features

- **Check Connection Button**: Found in Preset Modal and Chat Input settings, with immediate feedback.
- **Auto-Check**: Debounced connection check as you type the port number.
- **macOS Helper**: Download link for a pre-configured macOS launcher.

### macOS "Damaged Package" Fix

```bash
xattr -cr /path/to/Chrome-CDP.app
```

### Network Paths (Docker)

- **macOS/Windows**: `http://host.docker.internal:9222`
- **Linux**: `http://172.17.0.1:9222` (or your host IP)

### Troubleshooting

- **Connection Refused**: Ensure Chrome is running with `--remote-debugging-port=9222`. Check no other process uses port 9222 (`lsof -i :9222`). Close all other Chrome instances first.
- **Agent sees a blank page**: CDP only allows one connection per tab. If DevTools "Inspect" window is open for that tab, the agent's tools may be blocked.
- **Missing tab error**: In shared CDP mode, retry with `["tab", "<tab-id-or-label>", ...]` or `["--tab", "<tab-id-or-label>", ...]` in `args`. The error includes the current tab list.
- **Wrong tab acted on**: Check for direct CDP code or shell scripts that bypass `agent_browser`. Raw CDP scripts must use `/json/list`, connect to the chosen target, and avoid navigation/actions plus `Target.createTarget` / `Target.closeTarget` unless disposable raw-CDP control is explicitly required and the user accepts that it bypasses shared-browser locking.

---

## Playwright Artifacts and Output Location

### The Problem

By default, `@playwright/mcp` might save artifacts in the process cwd. In containerized environments, files get lost in temporary directories or scattered across the repository root.

### The Solution: `working_dir` Injection

The project uses a custom MCP client that supports a `working_dir` property in the server configuration.

#### Configuration (`agent_go/configs/mcp_servers_clean.json`)

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

**Key Parameters:**
- **`working_dir`**: Sets the OS-level cwd for the spawned `npx` process.
- **`--output-dir`**: Tells Playwright where to save snapshots and traces.
- **`--isolated`**: Prevents browser data (cookies, storage) from leaking between sessions.

### Benefits

1. **Visibility**: Artifacts land in `workspace-docs/Downloads`, indexed by semantic search and `list_directory`.
2. **Persistence**: Files survive session restarts (persistent `workspace-docs` volume).
3. **Cleanliness**: Prevents repo root from being cluttered with screenshot/download files.

### Interaction with CDP Mode

Even when controlling your local browser via CDP, `working_dir` injection is active — downloads are still routed to the designated workspace folder.

### Troubleshooting

- **"File not found" after download**: Check the `working_dir` path in MCP config. If relative, it's relative to the `agent_go` directory.
- **Duplicate filenames**: Playwright appends timestamp/UUID to screenshots. For specific filenames, use `take_screenshot` with a custom path.

---

## Browser Session Identity Split Plan

### Problem

A single MCP session ID currently does two jobs:

1. **Tool session identity** — session-scoped `MCP_API_URL`, custom tool routing, code execution mode HTTP calls.
2. **Browser session identity** — Playwright / `agent_browser` browser reuse, page state continuity, login persistence.

This coupling breaks in the workflow builder.

### Concrete Failure: Builder + `run_saved_main_py`

- `run_saved_main_py(step_id, group_id)` executes through the workshop controller in a **group MCP session**.
- The builder chat agent uses its own **chat session**.
- Result: builder cannot inspect the browser opened by the workflow step because the session IDs differ.

### Current `share_browser` Behavior

Available on `call_sub_agent()`, `call_generic_agent()`, and `delegate()`:

- `share_browser=true` (default): Child keeps parent's MCP session → browser state shared.
- `share_browser=false`: Isolated MCP session ID → browser isolated, but **also changes tool routing** (undesirable side-effect).

### Goal: Separate the Two Identities

Each agent/session should have:
- **`tool_session_id`** — MCP/custom tool routing, `MCP_API_URL`, stable per chat/workflow agent.
- **`browser_session_id`** — browser reuse only, can be shared across agents when desired.

### Proposed Browser Session Key

For workflow builder: `browser::<workspace-hash>::<group-id>` (canonical `group_id`, not display name).

### Desired Behavior After Split

- **Workflow builder**: Builder keeps its own `tool_session_id`. `run_saved_main_py` publishes the active `browser_session_id`. Subsequent builder browser inspection uses that ID.
- **Sub-agents**: `share_browser=false` only creates a new browser session, not a new tool session.
- **Multi-agent chat**: Shared browser is opt-in, not default.

### What Must Change

1. **Session model**: Introduce `ToolSessionID` + `BrowserSessionID` (fallback to tool session if browser session empty).
2. **Browser tools**: Playwright and `agent_browser` must resolve `browser_session_id` first, fall back to `tool_session_id`.
3. **Code execution env**: Add `MCP_BROWSER_SESSION_ID` or equivalent, keep `MCP_API_URL` on tool session.
4. **Workshop controller state**: Track `map[groupID]browserSessionID` and optional `lastActiveGroupID`.
5. **Cleanup lifecycle**: Closing a tool session must not auto-destroy a shared browser session.

### Rollout Phases

1. **Phase 1**: Internal split with compatibility fallback (browser falls back to tool session).
2. **Phase 2**: Workflow builder/workshop adoption (`run_saved_main_py`, `execute_step`, builder inspection).
3. **Phase 3**: Delegation adoption (`share_browser=false` isolates browser only).
4. **Phase 4**: General multi-agent adoption (shared browser opt-in).

### Files Involved

**mcp-agent-builder-go:**
- `agent_go/pkg/orchestrator/base_orchestrator.go` — single-session propagation
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller.go` — workshop group session cache
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_workshop.go` — workshop group switching
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go` — agent config overrides, `share_browser` handling
- `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/planning_exports.go` — workshop session setup
- `agent_go/cmd/server/server.go` — workflow-phase chat agent creation, session-aware executors

**mcpagent:** Browser session registry/reuse logic, Playwright session lookup, `agent_browser` execution path.

### Risk

If browser sessions are shared too broadly, two agents may interfere with the same page. Shared browser reuse should be explicit, scoped, and default-on only where already expected (parent/sub-agent in same task).

---

## Known Bugs

### Playwright: "transport error: transport closed"

**Status:** Known / Upstream

**Symptom:** `failed to call tool browser_close: transport error: transport closed`

**Meaning:** The MCP connection (stdio pipe to `npx @playwright/mcp`) is already closed by the time the call runs. Not that `browser_close` is invalid — the transport is gone.

**Typical causes:**
1. Playwright MCP process exited (crash, OOM, uncaught exception).
2. Connection was closed on our side (`CloseSession` called, workflow ended).
3. Browser/process died earlier — MCP server closes transport or exits.
4. Timeout or kill (subprocess killed, pipes close).

**Fix implemented:** "transport closed" is now treated as a broken-pipe error:
- `mcpclient.IsBrokenPipeError` returns true for `"transport closed"`.
- Agent path: broken-pipe handler closes old client, gets fresh connection, retries once.
- HTTP executor path: same — fresh connection and one retry.
- Registry: new agents calling `GetOrCreateConnection` ping existing connection; if dead, it's replaced.

**When it still happens:** If retry also fails or the new process dies again. Start a new workflow/session.

**What to do:**
- If cleaning up after browser close: treat as "connection already gone", continue.
- If you need a fresh session: start a new workflow run / chat session.
- If happening often mid-workflow: check for Playwright/subprocess crashes, OOM, or premature `CloseSession`.

### Playwright: Screenshots/Snapshots Saved to Repo Root

**Status:** Fixed

**Problem:** With `--output-dir` set, custom filenames (e.g., `screenshot.png`) were written to process root instead of the configured directory. Auto-generated filenames worked correctly.

**Root cause:**
1. Upstream Playwright MCP resolves custom filenames relative to process cwd, not `--output-dir` ([playwright-mcp#1390](https://github.com/microsoft/playwright-mcp/issues/1390)).
2. No working-directory support when spawning the MCP subprocess.
3. Session registry reuses connections by `(sessionID, serverName)` only — config not in key.

**Fix:**
1. Added `MCPServerConfig.WorkingDir` and `RuntimeConfigOverride.WorkingDir` to mcpagent.
2. `StdioManager` starts subprocess with `cmd.Dir = workingDir`.
3. Workflow override (`setupBrowserDownloadsPathOverride`) sets both `--output-dir` and `WorkingDir` to the run's `execution/Downloads`.
4. `.gitignore` fallback for any artifacts that still land at repo root.

**Files:** `mcpagent/mcpclient/config.go`, `mcpagent/mcpclient/stdio_manager.go`, `mcpagent/mcpclient/client.go`, `agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go`.

### Related Issues

- **"Browser is already in use"** → use `--isolated` in Playwright MCP config.
- **Session not found (HTTP transport)** → applies to streamable-http; stdio uses "transport closed" as the equivalent.
- Upstream refs: [playwright-mcp#1245](https://github.com/microsoft/playwright-mcp/issues/1245), [#1307](https://github.com/microsoft/playwright-mcp/issues/1307), [#1140](https://github.com/microsoft/playwright-mcp/issues/1140), [#1390](https://github.com/microsoft/playwright-mcp/issues/1390).
