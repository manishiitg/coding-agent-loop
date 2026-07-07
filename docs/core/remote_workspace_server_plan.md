# Remote Workspace Gateway + Local Runner Plan

**Status: DRAFT, NOT IMPLEMENTED** (2026-07-06)

Goal: `workspace-docs` lives **only on a server** (single physical copy, no local
working copies, no sync layer), while the user's laptop remains the **agent
runner**. Claude Code / Codex CLI / Cursor / other coding-agent CLIs run locally,
the local app runs the workshop/scheduler/control-plane logic, and every
workspace filesystem / shell / database / browser operation is executed through
a secure server-side workspace gateway.

This is not "server-hosted Runloop" and not "duplicate `agent_go` on both
machines." The server is a secure remote workspace runtime. The local app is the
agent runner and UI.

Multiple users may share one server: each user has **personal workflows** plus
access to a set of **shared team workflows**.

## Locked Decisions

| Decision | Choice |
|---|---|
| Server role | Remote Workspace Gateway: auth, one live `workspace-docs`, file/search/db/shell/browser tool APIs, leases, backups/snapshots |
| Local role | Agent Runner: UI/control plane, local `agent_go`, coding CLIs, `mcpbridge`, schedules/Pulse/auto-improve execution |
| Agent location | Claude/Codex/Cursor/LLM agents run locally unless a separate always-on runner machine is configured |
| Sharing model | Per-user personal workflows + explicitly shared team workflows |
| Copies of docs | Exactly one live copy on server disk; git Backups are recovery points, not working copies |
| Auth | Users authenticate to the server gateway via SSO/device grants; local runner gets a scoped token |
| Bridge | `mcpbridge` remains local beside the coding CLI and normally calls local `agent_go`, not the server directly |
| Schedules | Schedule definitions live with server workspace files; execution happens on an online local runner that claims a lease |

Related docs: [mcp_bridge_layer.md](mcp_bridge_layer.md),
[multi_user_authentication.md](multi_user_authentication.md),
[folder_guard_system.md](folder_guard_system.md).

---

## 1. Product Shape

### Server: Remote Workspace Gateway

Example target: one AWS/GCP/VPS instance at `https://workspace.example.com`
behind TLS. A VPN-only deployment is allowed, but the gateway should still be
safe if exposed to the public internet.

Hosted server responsibilities:

- **SSO / device connection**: authenticate the user, issue short-lived desktop
  import grants, mint scoped device/runner tokens, revoke devices.
- **Persistent workspace volume**: mount the single live copy at
  `/app/workspace-docs`.
- **Workspace file APIs**: document read/write/list/move/delete, search, glob,
  upload/import/export, version restore.
- **Workspace execution APIs**: shell execution inside the server workspace
  container, read-only SQLite query, browser/process helpers where supported.
- **Access enforcement**: token validation, user identity, folder guards,
  per-user/team path mapping, request limits, audit logs.
- **Write leases**: one active writer per shared workflow and claim leases for
  scheduled jobs so two laptops do not run or edit the same thing at once.
- **Durability**: snapshots, backup status, recovery tooling.

Server non-goals for v1:

- no Claude/Codex/Cursor installation requirement;
- no LLM provider auth requirement;
- no full workshop/chat orchestration;
- no autonomous Pulse/auto-improve execution without an online runner;
- no local-desktop UI replacement.

Implementation can reuse existing `workspace-api` and selected `agent_go`
code/routes, but the deployed server should be treated as a **gateway**, not as
a second full Runloop desktop backend.

### Local App: Agent Runner

Local responsibilities:

- run the desktop UI and local `agent_go` control plane;
- launch Claude Code / Codex CLI / Cursor / other coding-agent CLIs;
- run local `mcpbridge` as the stdio bridge for those CLIs;
- register and execute local custom tools such as `get_reference_doc`, plan
  editing tools, schedule tools, Pulse/auto-improve orchestration;
- point all workspace IO to the remote gateway via `WORKSPACE_API_URL`;
- store the imported server profile and device token securely;
- run schedules when the machine is online and successfully claims the server
  lease.

Local non-goals in remote workspace mode:

- no local checkout/copy of `workspace-docs`;
- no local `workspace-api` for the remote workspace;
- no direct raw access to server files outside the gateway APIs.

### Runtime Flow

```text
Claude/Codex/Cursor on laptop
  -> local mcpbridge
  -> local agent_go tool handler
  -> WorkspaceClient / WORKSPACE_API_URL
  -> HTTPS Remote Workspace Gateway
  -> server workspace-api / /app/workspace-docs
```

This keeps the agent stack local while moving the workspace and dangerous
operations to the server.

---

## 2. Connection Flow

1. Admin deploys the workspace gateway and configures SSO.
2. User opens the gateway connection page and signs in.
3. Server shows **Connect Desktop**.
4. Connect Desktop creates a short-lived, single-use grant and either:
   - opens `runloop://connect?server=...&code=...`, or
   - downloads a `.runloop-connection` file containing server URL + grant code.
5. Local app exchanges the grant for a scoped device/runner token.
6. Local app stores a remote profile:
   - `workspace_api_url = https://workspace.example.com`
   - `server_id`
   - `user_id`
   - `device_id`
   - token in OS keychain or equivalent secure storage
7. Local app verifies gateway health, token scope, visible workflows, and bridge
   compatibility.
8. Local runner starts using `WORKSPACE_API_URL=https://workspace.example.com`
   for the selected remote workspace.

Security rule: the server gateway derives identity from the validated token.
It must not trust browser/client-supplied `X-User-ID` as authority.

---

## 3. How Existing Tools Map

### `get_reference_doc`

Already fits the model. It is a local custom tool that renders embedded
`agent_go` guidance templates; it does not need server files.

```text
coding CLI -> mcpbridge -> local agent_go -> embedded guidance template
```

### Plan Editing Tools

`create_plan`, `add_regular_step`, `update_regular_step`,
`delete_plan_steps`, route tools, validation tools, and `update_step_config`
should keep executing in local `agent_go`.

They already use the orchestrator's `ReadWorkspaceFile` / `WriteWorkspaceFile`
callbacks, backed by `WorkspaceClient`. In remote mode that client must point to
the server gateway.

```text
plan tool in local agent_go
  -> WorkspaceClient
  -> remote gateway
  -> Workflow/<name>/planning/plan.json
```

Required hardening:

- `WorkspaceClient` must send a verified device/user token, not only
  `X-User-ID`.
- Server gateway must enforce write permission and workflow write leases.
- Dedicated plan tools remain the only allowed path for protected planning
  files; raw `diff_patch` / shell writes to `planning/plan.json` stay blocked.

### Workspace File / Shell / DB / Browser Tools

These should execute against the remote gateway:

- file read/write/list/search;
- `execute_shell_command`;
- `diff_patch_workspace_file`;
- SQLite query;
- browser/process helpers where the capability is server-side.

CDP against the user's visible local Chrome is a separate local-browser mode and
should not be confused with server-side browser execution. In remote workspace
mode, unattended schedules should prefer server-side headless/browser helpers or
explicitly require a local runner/browser.

---

## 4. Schedules, Pulse, Auto-Improve, Chief Of Staff

The server does not have coding CLIs or LLM agents in this model, so it cannot
execute scheduled agent work by itself.

### Schedule Storage

Schedule definitions remain workspace artifacts, for example:

- workflow schedules in `Workflow/<name>/workflow.json`;
- workflow schedule history in `Workflow/<name>/schedule-runs.json`;
- Chief of Staff schedules under the user's server workspace area.

Because those files live on the server, all runners see the same schedule
configuration.

### Schedule Execution

An online local runner:

1. reads schedules from the remote workspace;
2. computes due jobs locally;
3. asks the server gateway to claim a job lease;
4. runs the scheduled messages locally using the local coding agent stack;
5. all file/shell/db/browser operations go to the server gateway;
6. writes run outputs, Pulse HTML, costs, reports, notify state, and run history
   back to the server workspace;
7. releases or completes the lease.

If no runner is online, schedules wait. For always-on automation, the user can
run a dedicated runner machine. That machine is still a **runner** with coding
CLIs installed; the workspace server remains a gateway.

### Multi-Runner Rules

- A scheduled job must have a server-side claim lease before execution starts.
- A shared workflow must have a write lease before plan/config/report/KB/db
  mutations.
- If a lease expires, another runner may recover only after checking run state
  and marking stale `running` records safely.
- Notification and publish steps must be idempotent enough that a recovered run
  does not double-send or double-publish without checking status.

---

## 5. Sharing Model

Build on existing per-user isolation:

```text
workspace-docs/
  _users/<user-id>/...            # personal workflows + chat scratch
  _team/Workflow/<name>/...       # shared team workflows
```

Rules:

- **Personal**: only the owning user's runner sessions get folder-guard access.
- **Shared team workflows**: authenticated team members may read; writes require
  a server-side workflow lease.
- **System-like writes** from Pulse/auto-improve are still performed by the
  runner that claimed the schedule, but server audit records should mark them as
  schedule/automation writes, not arbitrary user edits.
- Keep v1 small: one team per server, personal vs shared boundary only. No
  per-workflow ACL matrix yet.

---

## 6. Implementation Phases

Order matters: first make the gateway secure, then point local runners at it.

### Phase 0 — Mode Boundary

- Add an explicit **remote workspace profile** to the local app.
- In remote mode, local `agent_go` remains the control plane, but
  `WORKSPACE_API_URL` points to the server gateway.
- Do not start/use local `workspace-api` for the remote workspace.
- Make the UI show active mode, server, user, and runner identity.

Exit criteria: local UI can browse a remote workspace through the gateway
without any local workspace copy.

### Phase 1 — SSO + Device Token

- Add the Connect Desktop grant flow.
- Exchange one-time grants for scoped device/runner tokens.
- Store tokens securely.
- Add revoke/list devices.
- Make `WorkspaceClient` attach the device token to every gateway request.

Exit criteria: local runner can authenticate to the gateway without pasting raw
browser JWTs or long-lived shared tokens.

### Phase 2 — Secure Gateway

- Gateway validates token and derives user identity server-side.
- Remove trust in client-supplied `X-User-ID` as authority.
- Apply folder guards and per-user/team mapping at the gateway.
- Add request-size limits, rate limits, audit logs, and TLS-only public access.
- Keep `workspace-api` private behind the gateway layer if the gateway is split
  into auth/proxy + workspace-api.

Exit criteria: no anonymous request can read/write files or run shell; a stolen
token is scoped to one user/device and can be revoked.

### Phase 3 — Tool Compatibility

- Run the existing workshop tools against remote `WORKSPACE_API_URL`.
- Verify `get_reference_doc` stays local and unaffected.
- Verify plan tools mutate remote `planning/plan.json` through
  `WorkspaceClient`.
- Verify file, shell, db, report, cost, media, and browser helpers either work
  through the gateway or are explicitly marked local-only.
- Add tests for direct protected-planning-file writes being blocked in remote
  mode.

### Phase 4 — Leases

- Add workflow write leases for shared workflows.
- Add scheduled-job claim leases.
- Add recovery behavior for expired/stale leases.
- Surface lease errors clearly to agents and UI.

### Phase 5 — CLI Scratch Dir Audit

Today coding CLIs may be launched with cwd inside the workspace subtree. With
remote docs that path does not exist locally.

- Launch CLIs in per-session local scratch dirs.
- Write only local CLI config there (`.mcp.json`, `.cursor/mcp.json`, hooks,
  policy files).
- Audit CLI-specific implicit disk context (`CLAUDE.md`, `@file`,
  `.pi/APPEND_SYSTEM.md`) and inject/fetch needed context through tools instead.
- Update bridge guidance: prompt paths are server workspace paths; access them
  through tools, not local disk.

### Phase 6 — Migration

- Backup local workflow.
- Restore/import it into server `workspace-docs`.
- Rewrite embedded absolute host paths to the canonical server root where needed.
- Mark the local workflow copy read-only/retired to avoid accidental split-brain.

---

## 7. Risks

| Risk | Notes / mitigation |
|---|---|
| **Remote code execution surface** | `execute_shell_command` is remote code execution by design. Gateway auth, folder guards, request limits, and audit logs are product code, not hardening. |
| **False duplicate-server design** | Do not run a second full control plane on the server. Server is gateway; local runner owns agents and orchestration. |
| **Concurrent-write corruption** | Server-side workflow leases are required before shared workflow writes. |
| **Schedule double-runs** | Multiple laptops can be online. Every due job needs a server-side claim lease. |
| **Laptop-off schedules** | Schedules do not run if no runner is online. Use a dedicated always-on runner if needed. |
| **Token leakage on runners** | Use scoped device tokens, secure storage, short lifetimes/refresh, and revocation. |
| **Canonical-root drift** | Server workspace root should be stable, preferably `/app/workspace-docs`; changing it requires migration. |
| **Silent CLI context loss** | Local CLIs no longer cwd into workspace files; audit implicit disk context and route through tools. |
| **Local-browser ambiguity** | Server-side browser helpers and local CDP browser are different capabilities. Label them clearly. |

---

## 8. Manual Validation Checklist

These are the areas to test manually before trusting remote workspace mode with
real workflows.

### Auth / Identity

- Log in via SSO, import a desktop connection, then revoke that device token.
  The local runner must stop working without needing a local app restart.
- Try to spoof another user by changing `X-User-ID` or a user id field in a
  request. The gateway must derive identity from the validated token and deny
  the spoof.
- Try to read another user's `_users/<id>` folder. The gateway must deny it.
- Try an expired token during a running session. Token refresh should recover,
  or the runner should fail clearly without partial writes.

### Remote Workspace Targeting

- With a remote profile active, verify every workspace read/write/shell request
  hits the server gateway, not local `127.0.0.1:8081`.
- Run `execute_shell_command pwd` and confirm it executes inside the server
  workspace/container.
- Run `get_reference_doc(kind=...)` and confirm it still works locally without
  requiring server file access.
- Disable or stop local `workspace-api` while using a remote profile; remote
  workflow browsing and tools should still work.

### Plan And Protected Files

- Edit a step with `update_regular_step`; confirm only the server-side
  `Workflow/<name>/planning/plan.json` changes.
- Try raw `diff_patch_workspace_file`, shell redirect, or direct write against
  `planning/plan.json`; it must be blocked.
- Simulate network loss during a plan edit; the result must be either fully
  applied or clearly failed, not half-written JSON.
- Have two runners try to edit the same shared workflow. One must acquire the
  workflow write lease and the other must get a clear lease error.

### Schedules / Multi-Runner

- Start two local runners with the same server profile and a due schedule. Only
  one runner should claim and execute the job.
- Kill the runner mid-schedule. The lease should expire or recover cleanly, and
  stale `running` records should not hide the failure.
- Let a schedule become due while no runner is online. It should wait, and the
  UI should say it is waiting for an online runner instead of implying the
  server is executing it.
- Run Pulse and auto-improve from a claimed schedule; confirm artifacts, cost
  report, Pulse HTML, dashboard cards, notify status, and run history all write
  to the server workspace.
- Retry/recover a scheduled run and confirm notify/publish/backup do not
  double-send or double-publish without checking status.

### Shell / File Boundary

- Test path traversal (`../`), symlink escape, absolute host paths, and attempts
  to access outside the allowed workspace root.
- Confirm folder guards apply consistently to read, write, move, delete, shell,
  db query, upload/import/export, and restore/version APIs.
- Run large file/report/media operations over the gateway and confirm timeouts,
  upload limits, and error messages are sane.

### Secrets / Logs

- Confirm local LLM/coding-agent credentials stay local and are not copied to the
  server workspace.
- Confirm server-side workflow/tool secrets are only available where intended.
- Confirm terminal logs, run files, Pulse, cost reports, and error messages
  redact device tokens, `Authorization` headers, and secrets.

### Browser Modes

- Test a workflow using local CDP/visible Chrome and a workflow using
  server-side/headless browser helpers. The UI and tool errors should make clear
  which browser mode is active.
- Run an unattended schedule that requires browser access. It should either use
  server-side/headless browser capability or explicitly require an online local
  runner/browser.

### Migration / Single Source Of Truth

- Migrate a local workflow to the server, then confirm the local copy is retired
  or marked read-only so it does not become a second source of truth.
- Restore from Backup into the server workspace and verify embedded absolute
  host paths are rewritten or flagged.
- Confirm published dashboards and report viewers read from server-side files/db
  through the gateway, not from stale local data.

---

## 9. Open Questions

- Should the gateway be implemented as a hardened mode of existing
  `workspace-api`, or as an auth/proxy layer in `agent_go` in front of private
  `workspace-api`?
- Deep link vs `.runloop-connection` file for desktop import.
- Exact device token lifetime and refresh behavior for long interactive runs.
- Whether shared workflows use `_team/` placement or `workflow.json shared=true`.
- Whether a dedicated "runner daemon" mode is needed for always-on schedules.
