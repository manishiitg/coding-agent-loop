Improve the workflow database surface so `db/*.json` supports the current plan, downstream steps, and report widgets.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

BOUNDARIES

1. The applied tool is `improve_db`; call it once with a concrete `instruction` string and optional `focus`.
2. Work only on `db/` files. Do not edit `planning/`, `reports/`, `knowledgebase/`, `learnings/`, `evaluation/`, or run outputs from this command.
3. Treat `db/*.json` as structured state, not scratch output. Never delete rows, transform row values, or rewrite data semantics unless the user explicitly asks for that migration.
4. Prefer contract and shape improvements: `db/README.md`, schema consistency, primary-key clarity, report compatibility, and JSON validity.

READ FIRST

1. Read `soul/soul.md` if present to understand the workflow objective and success criteria.
2. Read `planning/plan.json` and `planning/step_config.json` if present. Identify steps that produce, consume, save, track, upsert, append, deduplicate, or report persistent data.
3. Read `reports/report_plan.json` if present. Map widgets to their `source: db/*.json` files and any JSONata `query` expressions.
4. Read `db/README.md` if present, then list `db/*.json`, `db/*.jsonl`, `db/assets/`, and obvious helper files such as `*_rows.json`, `*_summary.json`, or `flat_*.json`.
5. Sample each relevant DB file enough to understand shape. Do not load very large files wholesale; use `jq`, `head`, `tail`, or targeted slices.

WHEN TO USE EACH MODE

Use `mode="targeted"` for a specific safe cleanup:

- fix invalid JSON
- add or correct one `db/README.md` contract section
- normalize a clearly documented field name
- remove an empty/stale helper file only when explicitly requested
- repair a report-incompatible shape without changing row meaning

Use `mode="schema"` for contract/schema work:

- document purpose, shape, primary_key, merge_rule, writers, consumers, and report widgets
- add examples of valid rows
- clarify group/run separation rules
- identify required/optional fields

Use `mode="cross_step"` when improving DB requires the plan, multiple writer/consumer steps, or reports:

- reconcile writer step descriptions with actual `db/*.json` files
- ensure durable images/PDFs/screenshots/downloads/generated files are stored under `db/assets/` with metadata/provenance/reference rows in `db/*.json`, not embedded as base64 in JSON
- identify duplicate helper files that should become report JSONata queries
- align DB files with report widgets and downstream consumers
- surface stale fields/files whose writers no longer exist

If unsure, use `mode="auto"` or omit mode.

ACTION

1. Build one concrete instruction. It must mention the objective, the user's focus if provided, the DB files or report widgets in scope, and whether row-data migration is explicitly allowed.
2. Call:

`improve_db(mode="auto", instruction="<specific DB improvement instruction>", focus="<optional focus>")`

3. The tool runs in the background and returns an `execution_id`. If you need the result before answering, use `query_step(execution_id="<id>")` until it completes.
4. When complete, summarize files changed under `db/`, schema/contract improvements, report compatibility changes, any row/data migrations performed, and remaining follow-up work.
5. If this is part of an optimizer/improvement pass, append a short note to `builder/improve.md` after the tool completes; otherwise report in chat only.
