# AgentWorks CLI and MCP

Use a hosted AgentWorks server from Claude Code, another MCP client, or scripts.
The CLI and MCP bridge use the same authenticated API. Plan edits invoke the
existing native tools; they do not launch an internal LLM or rewrite plan files
from the client. Workflow Builder chat is also available when a task needs the
builder to reason and act.

## Build and server setup

Requires the repository's Go toolchain (Go 1.26) and its normal local module
replacements. From the repository:

```sh
cd agent_go
go build -o bin/agentworks ./cmd/agentworks
```

Put the binary on your PATH. Rebuild and deploy **both the agent server and the
workspace service** from this revision. Existing deployments do not acquire
these endpoints just by installing the client. No server deployment is performed
by building this binary.

Configure the same nonempty `WORKSPACE_API_TOKEN` in the agent and workspace
services. This is a **server-to-server credential**, never a user's CLI token.
The new internal `/api/workflow-files` endpoint fails closed when this token is
missing and is blocked by the generic workspace proxy. Keep the workspace
service on the internal network; expose only the authenticated AgentWorks server.

## Test locally with the testing workflow

From the repository root, run:

```sh
python3 scripts/test-agentworks-external-local.py
```

This builds the agent server, workspace service, and CLI into a fresh directory
under `.local/workflow-tests/`. It copies only the design inputs from
`workspace-docs/Workflow/testing`, sanitizes its manifest, and starts separate
services on loopback ports with fresh test credentials. It does not use the
running development services or their workspace.

The probe exercises app-generated PAT login, workflow/tool discovery, document
read/write/patch/search, native plan edits, revision conflicts, read-only user
permissions, and protected plan paths. It then connects the actual stdio MCP
bridge, restores the edited step title through MCP, and checks the native
changelog, then revokes the token and checks that both the CLI and the existing
MCP connection are denied. Run/log inspection uses explicitly synthetic artifacts.

Each run prints the artifact directory and writes `receipt.json`, including
source-file hashes and results. It stops its own services and verifies the
original workflow inputs stayed unchanged. This test makes no model calls,
does not execute the copied workflow, and does not test a live Builder model
conversation. See [local workflow isolation](isolated-workflow-testing.md) for
the procedure for live agent testing.

## Connect to a hosted server

In the app, open your account menu and choose **Access tokens**. This is
available in both hosted and local installations. Single-user installs show a
**Local account** menu after the local app session initializes; multi-user
installs show it beside **Change password**. Give the token a name, choose its permissions and workflow
access, then select a 7-, 30-, or 90-day expiry. Copy the token when it is shown;
the app cannot display its secret again. The connection command in the dialog
uses the active installation's API URL, including the local desktop server's
loopback address and port.

```sh
agentworks login --server https://agentworks.example.com --token-stdin
```

Supply the generated `aw_pat_…` token on stdin. For interactive stdin on
macOS/Linux, paste it, press Enter, then Ctrl-D. A credential manager can also
pipe the token into this command. The CLI verifies access before saving it.
Username/password flags and app-session JWTs are no longer accepted by CLI
login. The app keeps its existing password and SSO sign-in flows.

Tokens have five permissions: `workflows:read`, `files:read`, `files:write`,
`plan:write`, and `builder:chat`. Read access is selected by default. Choose all
currently/future accessible workflows or specific workflow IDs. Every call
checks both the token restrictions and the user's current workflow access.
Tokens cannot call account management, the general query endpoint, or the
workspace proxy; only the external tool endpoints accept them.

**Builder chat requires all five permissions and all accessible workflows.**
Its existing runtime can execute shell commands and use integrations, so this
release does not offer a misleading restricted Builder token. Direct file and
plan tools support narrower tokens. A full-access token still cannot exceed
the user's normal account permissions.

In the account menu, inspect each token's expiry and last-used time or revoke it.
Every CLI/MCP HTTP request checks the persisted token record. Revocation rejects
subsequent calls, including calls from an MCP bridge already running. A Builder
session started by a token is bound to that token; it cannot inherit a browser
conversation or another token's conversation. Running Builder sessions are
rechecked every five seconds and canceled if the token expires, is revoked,
or loses its account/workflow access. A local revocation also requests immediate
cancellation of that server's token-owned sessions. Already completed external
actions cannot be undone by cancellation.

PATs do not silently refresh or extend their lifetime. Rotate by generating a
replacement, logging in with it, restarting the MCP bridge to load it, and
revoking the old token. Expired/revoked
tokens require a replacement from the app. `agentworks logout` removes the local
credential; it does not revoke the token on the server or unset environment
variables. There is no browser/device-login or refresh-token flow in this CLI.

Configuration is stored in the OS user-config directory under
`agentworks/config.json`, with private permissions. `--config` selects another
file. `AGENTWORKS_SERVER` and `AGENTWORKS_TOKEN` support automation without saving
credentials. HTTPS is required except on loopback development addresses.
Redirects are refused to avoid forwarding credentials to another location.

## Connect Claude Code

After login, register the local MCP bridge:

```sh
claude mcp add --transport stdio agentworks -- agentworks mcp serve
```

The bridge runs locally and calls your configured hosted server. It discovers
all tool schemas from that server at startup. Restart the bridge after upgrading
the server to refresh its catalog. This release provides stdio MCP; there is no
public Streamable HTTP MCP endpoint yet.

Example request:

> Find the invoice workflow, read its process documents, and rename its fetch
> step to "Fetch pending invoices". Record why the plan changed.

The agent discovers the workflow ID, reads the plan revision, and calls
`update_scripted_step` with the existing step ID, title, reason, and revision.
It receives the saved revision or a validation/conflict error.

## CLI examples

All output is JSON: pretty by default, compact with `--json`. IDs below are
examples; discover actual IDs first.

```sh
agentworks workflows list --json
agentworks workflows get --workflow WORKFLOW_ID
agentworks files list --workflow WORKFLOW_ID --path docs
agentworks files search --workflow WORKFLOW_ID --query invoices
agentworks files read --workflow WORKFLOW_ID --path docs/process.md
agentworks plan get --workflow WORKFLOW_ID
```

Use the returned revision for an edit:

```sh
agentworks plan update-scripted-step \
  --workflow WORKFLOW_ID --step STEP_ID \
  --title "Fetch pending invoices" \
  --reason "Clarify that this step retrieves pending invoices." \
  --expected-revision PLAN_REVISION

agentworks files write --workflow WORKFLOW_ID \
  --path docs/process.md --content-file ./process.md \
  --expected-revision FILE_REVISION

agentworks files patch --workflow WORKFLOW_ID \
  --path docs/process.md --diff-file ./process.patch \
  --expected-revision FILE_REVISION
```

`read_file` returns `exists:false, revision:"missing"` for a missing file. Use
`--expected-revision missing` to create it. Re-read and review conflicts before
retrying; clients never blindly retry a mutation. Do not use a plan revision for
a file edit or a file revision for a plan edit.

Native tools retain their original parameter names. For example,
`update_step_config` uses `step_id`, whereas `update_scripted_step` uses
`existing_step_id`. Use JSON for complex arguments:

```sh
agentworks tools list
agentworks tools call update_step_config --input ./step-config-change.json
```

Example input (the JSON file contains arguments only):

```json
{
  "workflow_id": "WORKFLOW_ID",
  "expected_revision": "PLAN_REVISION",
  "step_id": "STEP_ID",
  "reason": "Record the completed description review.",
  "description_reviewed": true
}
```

`--input -` reads JSON arguments from stdin. `--set key=JSON` supplies additional
native fields. `agentworks plan TOOL-NAME` maps hyphens to the native tool's
underscores. `tools list` is authoritative for the current server's schemas.

```sh
agentworks runs list --workflow WORKFLOW_ID
agentworks runs get --workflow WORKFLOW_ID --run-folder iteration-0/group-name
agentworks runs logs --workflow WORKFLOW_ID --run-folder iteration-0/group-name
```

Run tools browse saved run/log artifacts; retrieve selected paths with
`files read`. They do not start runs. Direct run start/stop, schedule management,
and workflow creation/deletion are outside this release.

## Workflow Builder chat

```sh
agentworks builder chat --workflow WORKFLOW_ID \
  --message "Review this plan and identify missing validation."
agentworks builder status --workflow WORKFLOW_ID --session SESSION_ID --limit 50
agentworks builder chat --workflow WORKFLOW_ID --session SESSION_ID \
  --message "Update the validation for the fetch step."
agentworks builder cancel --workflow WORKFLOW_ID --session SESSION_ID
```

Chat returns the existing runtime's session ID promptly. With a PAT, omitting
`--session` creates a new conversation; supply the returned session ID to continue
it. Tokens never automatically attach to an existing browser conversation. Poll `builder status`
using `--since-index` with the previous `last_processed_index`. `has_more`
indicates another page is available. `cursor_reset` indicates old events were
pruned; restart from the returned retained cursor rather than assuming no work
occurred. After a server restart, a durable conversation may report `inactive`
when no runtime remains; this does not assert that its last turn completed.

For a blocking human-input tool request, status exposes `pending_inputs`. Answer
its `unique_id` through the reply operation:

```sh
agentworks builder reply --workflow WORKFLOW_ID --session SESSION_ID \
  --request-id INPUT_ID --response "Use the current quarter."
```

A normal chat follow-up is not a substitute for answering a blocked input tool.
Only the session owner may view, resume, reply to, or cancel its conversation,
even if others can read its workflow. Generic file tools do not expose private
builder transcripts.

Builder chat runs the **existing builder and its configured tools**, with normal
model usage and workflow permissions. Workflow readers retain the builder's
existing read-only tool policy; conversation ownership is checked separately.
The builder can execute work through its authorized tools.
The direct API's absence of run commands does not make builder chat read-only.
Direct file/plan mutations return `workflow_busy` while a run/builder turn is
active; wait for it to finish or use the builder conversation to coordinate work.

## Architecture and limits

- `agent_go/pkg/agentworksclient`: hosted HTTP client, credential config, and MCP bridge.
- `agent_go/cmd/agentworks`: CLI argument handling.
- `agent_go/cmd/server/external_tools.go`: authenticated discovery, permissions,
  schema validation, workflow resolution, and operation dispatch.
- `step_based_workflow/external_plan_tools.go`: native plan schemas/executors and
  extracted shared step-config implementation used by the internal builder too.
- `external_builder.go`: existing query, event, human-input, and cancellation adapters.
- `workspace/handlers/workflow_files.go`: workflow-confined file access and
  revision-checked commit of staged plan changes.

Public tool endpoints are `GET /api/external/v1/tools` and
`POST /api/external/v1/call`. The CLI uses a PAT in the Bearer header; app sessions
can also use these endpoints with their normal JWT. Account token management is
`GET/POST /api/auth/access-tokens` and `DELETE /api/auth/access-tokens/{id}`, using
an app session only. Call bodies
are `{ "name": "TOOL_NAME", "arguments": { ... } }`.

File reads are capped at 2 MiB. Text is UTF-8; binary files return base64. Search
is literal and case-insensitive, with bounded depth, entry counts, and scanned
bytes. Pagination uses `next_offset` only when another result was found;
`truncated` can also mean the depth/scan budget was reached. Narrow the directory
or increase depth in that case. Symlinks, private credential directories, and
builder transcripts are excluded. Plan/config, workflow ownership, and run-state
files cannot be changed through generic file tools. Skills and ordinary documents
can be edited in their workflow's workspace.

Typed changes stage all file writes before committing and check revisions of
all files read. API file writers serialize with the commit. This prevents stale
external changes from overwriting an intervening API edit. Ordinary IO failures
trigger rollback. The commit is not a crash-recovery database transaction across
multiple files; direct host/shell writers are outside its lock. Keep using the
existing guarded plan tools for plan changes. The older UI handlers remain in
place; the new external interface invokes the native typed tool path.

Errors use `{ "error": { "code": "...", "message": "..." } }`. CLI exit
codes are 3 for authentication/permission failure, 4 for conflicts, and 1 for
other failures. Successful cancellation may return an empty object. Mutation
logs record the user, workflow, tool, and revision without recording credentials
or document contents; native plan changelogs retain the reason and field changes.

## Token persistence and deployment

The server stores SHA-256 token hashes and metadata in SQLite under its private
`AGENTWORKS_STATE_ROOT/auth/` directory (with the normal durable runtime root as
a fallback). The directory is 0700 and database is 0600, outside workspace files.
The database is bound to `AUTH_SECRET`; rotating that secret invalidates previous
PATs as well as app sessions. Tokens are not stored in workflow documents or
returned by listing endpoints. Creation responses use `Cache-Control: no-store`.

Persist this state directory across agent-server restarts/redeployments. This
version supports a single hosted agent instance with persistent local storage;
it does not introduce a distributed token store for independent replicas.
Do not deploy independent token databases behind a load balancer. Multi-host
replication requires a shared transactional authentication store.
