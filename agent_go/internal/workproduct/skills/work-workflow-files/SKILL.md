---
name: work-workflow-files
description: Read and interpret files from attached folders or read-only AgentWorks workflow references in Work. Use when the user asks what an attached workflow contains, requests data or reports from it, or wants files compared across workflows.
---

# Read attached workflow files

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
