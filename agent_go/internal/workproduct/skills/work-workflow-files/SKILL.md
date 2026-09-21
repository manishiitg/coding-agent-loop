---
name: work-workflow-files
description: Read and interpret attached folders or read-only AgentWorks workflow references in Crew, invoke an attached workflow through its Crew-scoped internal trigger, and securely link files from the active Crew project. Use when the user asks what an attached workflow contains, wants it run, requests data or Dashboards from it, compares files across workflows, or asks for a share link to a Crew project file or folder.
---

# Use attached workflows

## Share an active Crew project file or folder

For the project's live Dashboard, call `get_report_link` with no arguments and
use its returned `url`. Do not pass `db/reports/index.html` to `get_file_link`:
that would open a restricted generic HTML preview instead of the full Dashboard
runtime, and the file-link tool rejects that Dashboard entry path. The dedicated
Dashboard URL renders the Dashboard full-page and uses the normal AgentWorks SSO;
do not add a separate publish password or login to this internal URL. Publicly
hosted static Dashboards remain a separate publish flow with their own visibility
controls.

Call `get_file_link` with the path relative to the active Crew project. The
server verifies that the target exists, rejects private or escaping paths,
detects file versus folder, and returns the correct authenticated
`preview_url`. Never construct `/file` or `/folder` URLs manually.

Always inspect `shareable`, `scope`, and `warning` in either link tool's result.
If `shareable` is false, tell the user it is only a same-machine preview and
include the warning; do not call it a shareable link. A URL based on `localhost`,
`127.0.0.1`, or `::1` cannot be opened by another user or device. The deployment
must have a reachable `PUBLIC_URL` before the link can be shared.

Crew projects are personal. The URL contains no credentials and grants no
access; it can currently be opened only by the same signed-in Crew account.
Do not describe it as public publishing or as a way to grant another user
access. Use a publishing workflow when the user explicitly needs public or
cross-user distribution.

Resolve the authorized root before reading:

- For an AgentWorks workflow or another Crew reference, use the exact path
  supplied in attached context. `list_accessible_workflows` returns separate
  `workflows` and `crews` lists; every entry includes its project `name` and
  display `identity` (`name` and `icon`). Another Crew can be attached only
  when it belongs to the same signed-in account. Use the exact returned path;
  never guess a project path or scan unattached projects.
- For a host folder, call `list_work_folders` and use its
  `$WORK_FOLDER_<ALIAS>` variable. Do not inspect the parent directory.
- Both kinds are read-only unless a host-folder grant explicitly says
  `read_write`. Never edit a referenced workflow. Execution is allowed only
  through the attached-workflow trigger procedure below.

A `#` workflow selection applies only to that message. For durable access to a
workflow or another same-account Crew, use
`list_accessible_workflows`, disambiguate by its exact returned path, and call
`attach_workflow_reference` only when the user asks to keep it attached. Use
the exact saved path for `detach_workflow_reference`. Durable references live
in the Crew project's `workflow.json` and are re-authorized on every turn.

## Invoke an attached workflow

Run a workflow only when the user's request requires that workflow to execute;
attaching or inspecting it alone is not permission to start a job.

1. Call `list_attached_workflows` and use its exact `workspace_path`. A `#`
   selection is temporary context and is not an invokable durable attachment.
2. Call `list_workflow_triggers` when trigger discovery or selection matters.
   Public triggers may be visible for context, but this Crew path never exposes
   their secrets and cannot invoke them.
3. Call `run_workflow_trigger` with the exact path and the smallest required
   JSON payload. Normally omit `trigger_id`: the server reuses or creates the
   secretless internal trigger bound to this exact Crew. Pass an ID only when
   it is an enabled internal trigger clearly bound to this Crew.
4. Preserve the returned `delivery_id` and reuse it for retries of the same
   request so a retry cannot duplicate work. Poll `get_workflow_trigger_run`
   with the returned workflow path, trigger ID, and run ID until terminal;
   avoid rapid polling.
5. Report the actual terminal status and inspect returned step outputs and
   artifact references before claiming success. Download time-limited artifacts
   promptly when the user's task needs them.

The read-only attachment authorizes only creation or reuse of that narrowly
scoped internal binding. It does not permit workflow edits, public webhook
invocation, or management of unrelated triggers. Detachment, lost workflow
access, a disabled or rebound trigger, or a missing run must fail closed; do not
work around those checks with shell access or constructed HTTP requests.

## Inspect progressively

Start with a shallow listing and read only what answers the request. Workflow
folders can contain large run histories and caches.

| Path | Meaning |
| --- | --- |
| `workflow.json` | Authoritative workflow identity, version, capabilities, schedules, and durable selections. |
| `instructions.md` | Human-authored operating instructions, when present. |
| `code/` | Current step implementations and reusable workflow code. |
| `knowledgebase/` | Curated source context, notes, and indexes. |
| `learnings/` | Accumulated global or step-specific lessons. |
| `db/db.sqlite` | Managed workflow data. Query read-only; never edit SQLite, WAL, or SHM files. |
| `db/reports/` | Dashboard source, commonly `index.html`, plus Dashboard assets. |
| `runs/run_index.json` and `runs/iteration-*` | Run metadata, logs, and per-step execution outputs. Select the relevant/latest run instead of crawling all runs. |
| `reports/` | Published or user-scoped Dashboard artifacts, when present. |

Folders such as `builder/`, `planning/`, `config/`, `costs/`, `evaluation/`,
`pulse/`, `scores/`, `soul/`, `backup/`, `versions/`, hidden provider folders,
and `tool_output_folder/` are platform/runtime state. Inspect them only when the
user's question specifically requires diagnostics, history, or architecture.

For an attached workflow database, use a read-only shell query such as
`sqlite3 -readonly "$root/db/db.sqlite" ...`. The Crew project's
`query_workflow_db` tool targets the current Crew project, not an attached
workflow. Treat files as evidence that may change while the source workflow is
running, and state when a conclusion depends on a particular run or snapshot.

Do not search files for credentials. Workflow secrets remain behind the
Secrets system and are not granted by read-only folder access.
