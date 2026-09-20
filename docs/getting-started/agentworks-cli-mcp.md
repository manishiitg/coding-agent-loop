# AgentWorks CLI and MCP

Use a hosted AgentWorks server from Claude Code, another MCP client, or scripts.
The CLI and MCP bridge use the same authenticated API. Tokens read and run,
like the Slack and WhatsApp run-mode channels: tools read, and run-mode tools
execute in pinned Run-mode sessions. Nothing creates, edits, or authors. File
writes, plan mutations, and Builder execution are not exposed; the dispatch
paths stay in the server for a future write-enabled API version.

## Install the CLI

Open Setup → Integrations → Connect on your server (server installs only)
and paste the one command: it downloads the CLI build matching that server,
verifies its checksum, installs it to `~/.local/bin`, and logs in with the
generated token, which reads and runs. macOS and Linux on arm64/amd64 are
supported.
The same token is reused on every visit until it is revoked or expires.

The binaries and installer are served by the server itself at
`/api/downloads/cli/` (public, like the existing launcher downloads), so
the CLI always matches the API it talks to. `agentworks version` prints the
build; `agentworks update` (or `update --check`) self-updates from the
connected server. Developers can still build from source as below.

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
The internal `/api/workflow-files` and `/api/shared-assets` endpoints fail closed
when this token is missing and are blocked by the generic workspace proxy. Keep the workspace
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
read/search, plan and guidance reads, and narrow-token permission checks. It
also creates a WAV asset larger than 2 MiB, gets its share link, downloads it
through the CLI, and verifies an authenticated byte-range request through the
browser file endpoint. It then connects the actual stdio MCP bridge, verifies
plan/context reads, and asserts the probe left no changelog entries. It revokes
the token and checks that both the CLI and the existing MCP connection are
denied, and asserts every mutation path answers `unknown_tool`. Run/log
inspection uses explicitly synthetic artifacts.

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
installs show it beside **Change password**. Give the token a name, choose its workflow
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

Tokens carry two read permissions: `workflows:read` and `files:read`.
Write permissions (`files:write`, `plan:write`, `builder:chat`) are not issued
in v1. Choose all currently/future accessible workflows or specific workflow
IDs. Every call checks both the token restrictions and the user's current
workflow access. Tokens cannot call account management, the general query
endpoint, or the workspace proxy; only the external tool and asset-content
endpoints accept them. A token still cannot exceed the user's normal account
permissions.

In the account menu, inspect each token's expiry and last-used time or revoke it.
Every CLI/MCP HTTP request checks the persisted token record. Revocation rejects
subsequent calls, including calls from an MCP bridge already running. A local
revocation also requests immediate cancellation of that server's token-owned
sessions.

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

> Find the invoice workflow, read its process documents, and summarize what its
> fetch step does.

The agent discovers the workflow ID, then reads the plan, files, and runs. If
the task needs a change, it says so instead of attempting one.

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

Load guidance and knowledge for the task:

```sh
agentworks guidance context --workflow WORKFLOW_ID
agentworks guidance topics
agentworks guidance topic --topic plan-change-impact
agentworks knowledge list --workflow WORKFLOW_ID
agentworks knowledge read --workflow WORKFLOW_ID --path learnings/_global/SKILL.md
```

`--input -` reads JSON arguments from stdin. `--set key=JSON` supplies
additional native fields. `tools list` is authoritative for the current
server's schemas:

```sh
agentworks tools list
agentworks tools call get_guidance_topic --input ./topic.json
```

```sh
agentworks runs list --workflow WORKFLOW_ID
agentworks runs get --workflow WORKFLOW_ID --run-folder iteration-0/group-name
agentworks runs logs --workflow WORKFLOW_ID --run-folder iteration-0/group-name
```

Saved run/log artifacts are browsed with `runs list|get|logs`; retrieve
selected paths with `files read`. Workflow creation/deletion stays outside
this surface.

## Running steps, workflows, and schedules

Every run-mode chat tool from `product.yaml` is callable here under the
`runs:execute` scope — the run list is the single source of truth, so a tool
added to run mode appears in `tools list` and `mcp serve` with no client
change. Each call starts a new pinned Run-mode session (or continues
`--session`), and its reply carries `session_id`; poll `runs status` for
completion. Structured arguments travel via `--set key=JSON`.

```sh
agentworks runs start-step --workflow WORKFLOW_ID --step-id fetch-invoices --set 'script_parameters={"limit":10}'
agentworks runs start-workflow --workflow WORKFLOW_ID --group group-1
agentworks runs status --workflow WORKFLOW_ID --session SESSION_ID
agentworks runs executions --workflow WORKFLOW_ID
agentworks runs message --workflow WORKFLOW_ID --session SESSION_ID --execution-id EXEC_ID --message "slow down"
agentworks runs stop --workflow WORKFLOW_ID --session SESSION_ID --execution-id EXEC_ID
agentworks runs stop-all --workflow WORKFLOW_ID --session SESSION_ID
agentworks schedules list --workflow WORKFLOW_ID
agentworks schedules runs --workflow WORKFLOW_ID --schedule-id daily
agentworks schedules trigger --workflow WORKFLOW_ID --schedule-id daily
```

`runs:execute` implies workflow visibility (`list_workflows`, `get_plan`,
run evidence, status), never file content, which stays behind `files:read`.
A read-only token sees neither the run tools in `tools list` nor their MCP
entries, and calling one returns `insufficient_scope`. Sessions are owned by
the token that started them: revoking the token cancels its runs, and one
token can never status, message, or stop another token's session. New tools
added to run mode later work immediately through `tools call`; typed
subcommands cover the core operations above.

## Asset links and downloads

Use the existing Share file viewer for a clickable output link:

```sh
agentworks files link --workflow WORKFLOW_ID --path db/assets/report.pdf
agentworks files download --workflow WORKFLOW_ID --path db/assets/report.pdf \
  --output ./report.pdf
```

MCP exposes the same `get_file_link` tool with `workflow_id` and `path` arguments.
It returns the file size, content type, `preview_url`, and `download_url` without
loading the asset into model context. An external agent can call `get_file_link`
and give the user its `preview_url` for any existing output.

The preview opens `/file?path=…` in AgentWorks. Local installations initialize
the local app session before fetching; hosted installations preserve the file
or folder URL through password or OAuth sign-in. Images, audio, video, PDF,
Markdown, and text have previews. HTML renders in a sandbox without scripts;
other binary formats offer a download. Markdown workspace images use authenticated
requests, and linked workspace documents open their own shared viewer.

**A share link identifies a file; it does not grant permission.** Workflow owners
and readers can view/download it. Every file, folder listing, and ZIP request
checks the recipient's current workflow access. Removing access also blocks old
links. Personal Chats/Downloads remain private to their owner; an old `uid` link
cannot grant another user access. Private files and symlinks are excluded.

Preview URLs contain no credentials. The `download_url` requires a PAT or app
session in the `Authorization: Bearer …` header; clicking that API URL alone does
not supply a header. Use `preview_url` for people and `files download` for a local
agent. Downloads require `files:read` and access to the selected workflow, stream
without the tool's 2 MiB read limit, and refuse to overwrite existing local files.
Folder listings are bounded at 10,000 scanned entries; ZIP downloads are bounded
at 512 MiB of uncompressed files. Choose a smaller folder when needed.

Set `PUBLIC_URL` to the externally reachable AgentWorks origin when running
behind a proxy. The normal hosted/local app serves the viewer and API on that
origin. A separate frontend development server needs the appropriate `PUBLIC_URL`
and API runtime configuration. A localhost link works on the machine running
that installation; sharing it with someone on another machine requires a reachable
hosted address.

## Workflow Builder chat

Not exposed. Builder chat runs the existing builder runtime with authoring
tools, so it stays out of the catalog alongside file writes and plan
mutations; external execution runs in pinned Run-mode sessions instead. The
server keeps its session binding, ownership checks, and revocation-driven
cancellation for a future write-enabled API version.

## External agent guidance

The local implementation now gives MCP clients short initialization
instructions and exposes five guidance and knowledge operations. Builder chat
is not exposed, so the external agent relies on these operations plus the
read tools; runtime steps separately receive their explicitly enabled step
skills.

The external surface is intended to add the decision context that bare tool
schemas do not provide: which guidance applies, what other files and
configuration are worth checking, and how to answer from reading. AgentWorks
builds the canonical `builder-reference`, `workflow-commands`, and
`system-tools` bundles in
`agent_go/cmd/server/guidance/materialize.go`; the external implementation
reuses those renderers rather than maintaining another complete body of
guidance.

Two constraints define the intended boundary:

- Builder guidance cannot be exposed unchanged. Some documents instruct the
  Builder to call internal tools the external catalog does not provide, so the
  external surface needs a guidance profile filtered by actual tools and
  permissions.
- Permissions must be split per tool. Canonical server-owned guidance can use
  `workflows:read`, but workflow-authored skills and learnings must require
  `files:read`. Workflow `skills/` projection paths (`.pi/skills`,
  `.agents/skills`, `.claude/skills`) are generated provider artifacts and must
  not become a public API; `learnings/_global/` is a real workflow path but
  requires file-read permission.

The five implemented operations are:

- `get_agent_context`: role, token capabilities, available tools, and guidance
  version, plus the preparation checklist (with a run section when the token
  allows `runs:execute`). This is a global tool;
  pass `workflow_id` to include the caller's role on a workflow. CLI:
  `agentworks guidance context [--workflow ID]`.
- `list_guidance_topics` / `get_guidance_topic`: server-owned guidance for
  `plan-change-impact`, `plan-design`, `planning-steps`, `step-description`,
  `step-config`, `skill-management`, `file-layout`, and `secure-share-links`.
  Each topic is rendered live from the canonical Builder reference, and every
  internal-only operation named by served content is disclosed in that topic's
  external mapping note (enforced by test). Topics documenting internal-only
  tool names are excluded from the profile.
- `list_workflow_knowledge` / `read_workflow_knowledge`: workflow learnings,
  knowledgebase notes, workspace skill folders, and skill wiring
  (workflow-selected skills plus per-step `enabled_skills`). Reads are confined
  to `learnings/`, `knowledgebase/`, and `skills/<folder>/<file>` content, and
  skill folders are further restricted to the workflow's selected and
  step-enabled skills — a workflow ticket never grants the whole shared skill
  catalog. An unavailable skill catalog returns a `warnings` entry rather than
  an empty list. CLI: `agentworks knowledge list|read --workflow ID
  [--path PATH]`.

MCP initialization delivers short instructions that tell the client whether
the bridge reads only or also runs (chosen from the scope-filtered catalog),
to call `get_agent_context` first, and to load
relevant topics. The companion
skill source lives at
`agent_go/pkg/agentworksclient/skills/agentworks/SKILL.md`, embedded in the
CLI; `agentworks skills install --dir <skill-dir> [--force]` writes it to
`<dir>/agentworks/` (default `.agents/skills`, refusing to clobber without
`--force`). The guidance version is computed from the allowlist, mapping
notes, and rendered content, so cached clients detect canonical changes
without a manual bump. `get_agent_context` with `workflow_id` also returns
`effective_tools`, filtered by the caller's role on top of token scopes.

Canonical guidance tools require `workflows:read`; knowledge tools require
`files:read`. Nothing authors, so the follow-up contract is small: the agent
answers from what it reads (and what its runs report) and says so when a task
needs a change.

### Local implementation review (2026-09-20, second pass)

The implementation is committed and pushed as `ba5f282ae` on `main`, which
matches `origin/main`. The working tree is clean apart from the unrelated
untracked `tmp/` directory.

The second review confirmed these completed fixes:

- Workspace skill discovery decodes the shared-assets `filepath` field and has
  a non-empty discovery test.
- Skill listing and reads are restricted to the workflow's selected and
  step-enabled skills; unrelated global skill folders return `forbidden`.
- The skill catalog reports a warning when it is unavailable instead of looking
  empty.
- The guidance version is derived from the allowlist, mapping notes, and
  rendered canonical content.
- `get_agent_context` returns role-filtered `effective_tools` in addition to
  token-level availability.
- Global topic commands do not expose the inapplicable `--workflow` flag.
- `agentworks skills install --dir <skill-dir> [--force]` installs the embedded
  AgentWorks skill and refuses to overwrite it unless requested.
- `required_followups` are documented consistently as advisory receipts rather
  than server-enforced completion state.

Two functional blockers remain:

1. **Per-step skills are parsed from the wrong file shape.** Production
   `planning/step_config.json` uses
   `{ "steps": [{ "id": "...", "agent_configs": { "enabled_skills": [...] } }] }`,
   while `externalStepSkills` currently expects a top-level array with
   `step_id` and `enabled_skills`. A skill enabled only on a step is therefore
   absent from `step_skills`, omitted from `workspace_skills`, and rejected by
   `read_workflow_knowledge`. Parse the canonical `StepConfigFile` structure and
   make the external guidance test fixture use the production format.
2. **External guidance still contains undisclosed internal instructions.** The
   mapping test checks a fixed denylist rather than comparing rendered guidance
   with the actual external catalog. Current served documents still mention
   unsupported operations omitted from that denylist, including `add_step`,
   `update_step`, `run_workflow`, `create_human_input_request`,
   `update_validation_schema`, `query_workflow_costs`, and
   `get_workflow_config`, as well as reference topics unavailable to external
   clients. Render an external-specific form or validate every operation and
   reference against the actual external catalog and topic allowlist.

The external plan schemas need the same compatibility treatment. For example,
the `update_step_config` description tells callers about `execute_step` and
`run_full_workflow`, and its `enabled_skills` field recommends `list_skills` and
`get_workflow_config`; none of those operations are in the external catalog.
External schema descriptions should map these instructions to supported tools
or remove them.

Current validation status:

- `go build ./cmd/server` passes.
- The AgentWorks CLI and client tests pass, including skill installation and
  MCP coverage.
- `git diff --check` passes.
- `go test ./cmd/server ...` cannot compile because the pre-existing Crew test
  calls an undefined `mock.hasFolder`. This is unrelated to the external-agent
  change, but it prevents the focused server tests from being rerun against the
  current tree. The previously reported `registerWorkCrewProfile` error is no
  longer present.

Review acceptance now requires fixing the canonical step-config parsing,
eliminating unsupported instructions from returned guidance and external tool
schemas, adding production-shaped step-skill coverage, and restoring the server
test build.

## Architecture and limits

- `agent_go/pkg/agentworksclient`: hosted HTTP client, credential config, and MCP bridge.
- `agent_go/cmd/agentworks`: CLI argument handling.
- `agent_go/cmd/server/external_tools.go`: authenticated discovery, permissions,
  schema validation, workflow resolution, and operation dispatch.
- `step_based_workflow/external_plan_tools.go`: native plan schemas, kept for
  a future write-enabled API; unexposed.
- `external_builder.go`: existing query, event, human-input, and cancellation
  adapters; unexposed.
- `external_run.go`: run-mode tool proxy (pinned Run-mode sessions),
  `run_status` poller, and JSON-direct execution, schedule, and trigger reads.
- `workspace/handlers/workflow_files.go`: workflow-confined file access.

The exposed tool set has one source of truth:
`agent_go/internal/agentworksproduct/product.yaml`, `chat.run`. The server
exposes `external_tools` first, in yaml order, then every `tools` name without
a native implementation, proxied to a pinned Run-mode session in yaml order;
names with a native implementation (`get_file_link`, `list_executions`,
`list_schedules`, `get_schedule_runs`, `trigger_schedule`) keep it. Go defines
the implementations (schemas, dispatch) while the yaml admits them. A yaml
name without an implementation — or an implementation missing from both
lists — fails server startup, and the CLI subcommand mappings are test-pinned
to the union. Changing the surface means editing the yaml and the golden test
together, deliberately; adding a tool to run mode exposes it externally with
no further change.

Public tool endpoints are `GET /api/external/v1/tools` and
`POST /api/external/v1/call`. The CLI uses a PAT in the Bearer header; app sessions
can also use these endpoints with their normal JWT. Account token management is
`GET/POST /api/auth/access-tokens` and `DELETE /api/auth/access-tokens/{id}`, using
an app session only. Call bodies
are `{ "name": "TOOL_NAME", "arguments": { ... } }`.

Tool file reads are capped at 2 MiB; asset streaming uses
`GET/HEAD /api/external/v1/files/content?workflow_id=…&path=…`. Text is UTF-8; binary files return base64. Search
is literal and case-insensitive, with bounded depth, entry counts, and scanned
bytes. Pagination uses `next_offset` only when another result was found;
`truncated` can also mean the depth/scan budget was reached. Narrow the directory
or increase depth in that case. Symlinks, private credential directories, and
builder transcripts are excluded, as is coding-agent infrastructure:
AGENTS.md-style prompt files and the .claude, .agents, .codex, .cursor,
.gemini, and .pi tool directories, including the skills beneath them. Skills
stay readable through the knowledge tools, which serve the skill catalog;
learnings and ordinary documents are readable in their workflow's workspace.
Nothing is writable: run tools execute; they never author plans, files, or
configuration.

Errors use `{ "error": { "code": "...", "message": "..." } }`. CLI exit
codes are 3 for authentication/permission failure, 4 for conflicts, and 1 for
other failures (including `unknown_tool` for removed mutation paths).

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
