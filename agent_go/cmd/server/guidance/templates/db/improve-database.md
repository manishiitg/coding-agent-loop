# READ-ONLY DATABASE HEALTH REVIEW

Review whether `db/db.sqlite`, `db/README.md`, and `db/assets/` support the
current plan, downstream steps, evals, and reports. This checklist is passed to
a generic read-only reviewer. Do not execute DDL/DML, edit any file, update
`builder/improve.html`, or call module-result or human-input tools. Any later
wording such as improve, apply, edit, update, add, drop, migrate, or resolve is a
recommendation for the **Pulse Fixer**, not permission for this reviewer to
mutate state.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

EXECUTION

The parent Workshop/Pulse agent first loads `assumption-audit`, then passes its
relevant lens and this rendered checklist to
`call_generic_agent` in an instruction beginning with `READ-ONLY REVIEW` and
waits for its synchronous result. The parent then validates and applies any
bounded safe edit. Do not create a dedicated DB-maintenance agent or use
`run_in_background` for this review.

Return only: `verdict`, ordered `findings`, precise `evidence`, a bounded
`recommended_fix`, migration risk, verification commands, and
`user_judgment_required` with reason. Use the remaining document only as the
database-health audit checklist.

Read `builder/improve.html` for prior context and matching open findings, but do
not write it. The Pulse Fixer owns the consolidated log update.

Apply the parent-provided `assumption-audit` DB lens within this command's boundaries. Check whether schemas, enums, keys, or cardinality unnecessarily hardcode one source, channel, entity type, group, or current tactic. Recommend safe contract changes, but do not perform speculative row migrations; surface a consequential strategy/schema choice for Pulse's Assumptions challenged when business judgment is required.

BOUNDARIES

1. Return one concrete recommended instruction and optional focus for the Pulse Fixer; there is no separate DB-maintenance tool.
2. Work only on `db/` files (`db/db.sqlite` + `db/README.md`). Do not edit `planning/`, `reports/`, `knowledgebase/`, `learnings/`, `evaluation/`, or run outputs from this command.
3. Treat `db/db.sqlite` as structured state, not scratch output. Never delete rows, transform column values, or rewrite data semantics unless the user explicitly asks for that migration.
4. Prefer contract and schema improvements: `db/README.md`, table schema consistency, PRIMARY KEY / index clarity, report compatibility (the `sql` widgets resolve), and data integrity.

READ FIRST

1. Read `soul/soul.md` if present to understand the workflow objective and success criteria.
2. Read `planning/plan.json` and `planning/step_config.json` if present. Identify steps that produce, consume, save, track, upsert, append, deduplicate, or report persistent data.
3. Read `reports/report_plan.json` if present. Map widgets to their `db: db/db.sqlite` + `sql` queries (and `source` for file/file-list widgets).
4. Read `db/README.md` if present, then inspect the database: `sqlite3 db/db.sqlite ".tables"` and `.schema <table>` for each table; also note `db/assets/`.
5. Sample each relevant table enough to understand shape. Do not dump whole tables; use `sqlite3 db/db.sqlite "SELECT * FROM <table> LIMIT 5"`, `SELECT COUNT(*)`, and targeted queries.
6. Build a control-state ownership map for tables that affect allocation,
   routing, lifecycle/status, feature flags, guards, retries, or other runtime
   decisions. For each logical entity, name its canonical table/field, all
   writers, the actual runtime reader, and any mirror/translation rule.
7. Search specifically for source-of-truth collisions: similarly named tables,
   JSON arrays, backup files, or report-only projections that carry the same
   IDs/statuses. Join or compare a bounded sample by stable ID and flag values
   that disagree. A write succeeding in one store is not evidence that the
   runtime consumer observed it.
8. For a recent claimed repair or status/config change, trace
   `writer -> canonical record -> runtime reader -> decision/output`. Return
   `wrong_store_write`, `shadow_store_drift`, or `dead_configuration` when that
   chain breaks. Recommend a single canonical owner or an explicit tested
   synchronization invariant; do not silently pick one and migrate rows.

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
- reconcile shadow control tables that encode the same IDs/statuses and prove
  which one the runtime allocator/router/guard actually reads

If unsure, use `mode="auto"` or omit mode.

REVIEW OUTPUT

1. Build one concrete instruction. It must mention the objective, the user's focus if provided, the DB files or report widgets in scope, and whether row-data migration is explicitly allowed.
2. Return the instruction and mode as `recommended_fix`.
3. Name tables/contracts/indexes/assets affected, report/eval compatibility
   impact, whether a row migration would be required, and exact verification
   commands such as `PRAGMA integrity_check`. For control-state findings, also
   include one bounded source-of-truth comparison query and one assertion that
   proves the runtime decision consumed the canonical value.
4. The Pulse Fixer owns every DB/file mutation and `builder/improve.html` update.
