---
name: work-workflow-files
description: Read and interpret files from attached folders or read-only AgentWorks workflow references in Work, and securely link files from the active Work project. Use when the user asks what an attached workflow contains, requests data or reports from it, wants files compared across workflows, or asks for a share link to a Work project file or folder.
---

# Read attached workflow files

## Share an active Work project file or folder

Call `get_file_link` with the path relative to the active Work project. The
server verifies that the target exists, rejects private or escaping paths,
detects file versus folder, and returns the correct authenticated
`preview_url`. Never construct `/file` or `/folder` URLs manually.

Work projects are personal. The URL contains no credentials and grants no
access; it can currently be opened only by the same signed-in Work account.
Do not describe it as public publishing or as a way to grant another user
access. Use a publishing workflow when the user explicitly needs public or
cross-user distribution.

Resolve the authorized root before reading:

- For an AgentWorks workflow reference, use the exact `Workflow/<folder>` path
  supplied in attached context. Use `list_accessible_workflows` only to find or
  confirm a durable reference; never guess a workflow path or scan other
  workflows.
- For a host folder, call `list_work_folders` and use its
  `$WORK_FOLDER_<ALIAS>` variable. Do not inspect the parent directory.
- Both kinds are read-only unless a host-folder grant explicitly says
  `read_write`. Never edit or execute a referenced workflow.

A `#` workflow selection applies only to that message. For durable access, use
`list_accessible_workflows`, disambiguate by its exact returned path, and call
`attach_workflow_reference` only when the user asks to keep it attached. Use
the exact saved path for `detach_workflow_reference`. Durable references live
in the Work project's `product.json` and are re-authorized on every turn.

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
| `db/reports/` | Dashboard source, commonly `index.html`, plus report assets. |
| `runs/run_index.json` and `runs/iteration-*` | Run metadata, logs, and per-step execution outputs. Select the relevant/latest run instead of crawling all runs. |
| `reports/` | Published or user-scoped report artifacts, when present. |

Folders such as `builder/`, `planning/`, `config/`, `costs/`, `evaluation/`,
`pulse/`, `scores/`, `soul/`, `backup/`, `versions/`, hidden provider folders,
and `tool_output_folder/` are platform/runtime state. Inspect them only when the
user's question specifically requires diagnostics, history, or architecture.

For an attached workflow database, use a read-only shell query such as
`sqlite3 -readonly "$root/db/db.sqlite" ...`. The Work project's
`query_workflow_db` tool targets the current Work project, not an attached
workflow. Treat files as evidence that may change while the source workflow is
running, and state when a conclusion depends on a particular run or snapshot.

Do not search files for credentials. Workflow secrets remain behind the
Secrets system and are not granted by read-only folder access.
