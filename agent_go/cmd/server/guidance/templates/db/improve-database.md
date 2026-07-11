Improve the workflow database so `db/db.sqlite` supports the current plan, downstream steps, and report widgets.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Write to `builder/improve.html`. For the log format, the one-time migration, and how entries are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

Load `get_reference_doc(kind="assumption-audit")` and apply its DB lens within this command's boundaries. Check whether schemas, enums, keys, or cardinality unnecessarily hardcode one source, channel, entity type, group, or current tactic. Improve safe contracts, but do not perform speculative row migrations; surface a consequential strategy/schema choice under Pulse's Assumptions challenged when business judgment is required.

BOUNDARIES

1. The applied tool is `improve_db`; call it once with a concrete `instruction` string and optional `focus`.
2. Work only on `db/` files (`db/db.sqlite` + `db/README.md`). Do not edit `planning/`, `reports/`, `knowledgebase/`, `learnings/`, `evaluation/`, or run outputs from this command.
3. Treat `db/db.sqlite` as structured state, not scratch output. Never delete rows, transform column values, or rewrite data semantics unless the user explicitly asks for that migration.
4. Prefer contract and schema improvements: `db/README.md`, table schema consistency, PRIMARY KEY / index clarity, report compatibility (the `sql` widgets resolve), and data integrity.

READ FIRST

1. Read `soul/soul.md` if present to understand the workflow objective and success criteria.
2. Read `planning/plan.json` and `planning/step_config.json` if present. Identify steps that produce, consume, save, track, upsert, append, deduplicate, or report persistent data.
3. Read `reports/report_plan.json` if present. Map widgets to their `db: db/db.sqlite` + `sql` queries (and `source` for file/file-list widgets).
4. Read `db/README.md` if present, then inspect the database: `sqlite3 db/db.sqlite ".tables"` and `.schema <table>` for each table; also note `db/assets/`.
5. Sample each relevant table enough to understand shape. Do not dump whole tables; use `sqlite3 db/db.sqlite "SELECT * FROM <table> LIMIT 5"`, `SELECT COUNT(*)`, and targeted queries.

WHEN TO USE EACH MODE

Use `mode="targeted"` for a specific safe cleanup:

- fix a malformed table or a column with inconsistent value types
- add or correct one `db/README.md` contract section
- add a missing index a report widget's `sql` needs
- drop an empty/stale table only when explicitly requested
- repair a report-incompatible shape without changing row meaning

Use `mode="schema"` for contract/schema work:

- document each table's DDL, PRIMARY KEY, upsert rule, indexes, writers, and consumers
- add example rows
- clarify group/run separation rules (e.g. a `group_name` column when multiple groups share a table)
- identify required/optional columns

Use `mode="cross_step"` when improving DB requires the plan, multiple writer/consumer steps, or reports:

- reconcile writer step descriptions with the actual `db/db.sqlite` tables
- ensure durable images/PDFs/screenshots/downloads/generated files are stored under `db/assets/` with metadata/provenance/reference rows in a table, not embedded as blobs
- identify redundant tables that a report widget's `sql` (JOIN/GROUP BY) could replace
- align tables with report widgets and downstream consumers
- surface stale columns/tables whose writers no longer exist

If unsure, use `mode="auto"` or omit mode.

ACTION

1. Build one concrete instruction. It must mention the objective, the user's focus if provided, the DB files or report widgets in scope, and whether row-data migration is explicitly allowed.
2. Call:

`improve_db(mode="auto", instruction="<specific DB improvement instruction>", focus="<optional focus>")`

3. The tool runs in the background and returns an `execution_id`. Do not babysit it with `sleep`, repeated `list_executions`, or repeated `query_step` calls. Use `query_step(step_id="improve-db", execution_id="<returned execution_id>")` at most once for an immediate status/result check. If it is still running, stop and rely on `[AUTO-NOTIFICATION]` to resume when complete.
4. When complete via `[AUTO-NOTIFICATION]` or a one-off result check, summarize tables/schema changed in `db/db.sqlite`, `db/README.md` contract improvements, report compatibility changes, any row/data migrations performed, and remaining follow-up work.
5. If this is part of an optimizer/improvement pass, append a short note to `builder/improve.html` after the tool completes; otherwise report in chat only.
