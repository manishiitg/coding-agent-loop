# ENGINEERING REVIEW — STORES HEALTH / DATABASE LENS

Review whether `db/db.sqlite`, `db/README.md`, and `db/assets/` support the
current plan, downstream steps, evals, and reports. This checklist is passed to
a generic read-only reviewer. Do not execute DDL/DML, edit any file, update
any presentation artifact, or call module-result or human-input tools. Any later
wording such as improve, apply, edit, update, add, drop, migrate, or resolve is a
recommendation for the **Pulse Fixer**, not permission for this reviewer to
mutate state.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

EXECUTION

The parent Workshop/Pulse agent first loads `assumption-audit`, then includes
this rendered checklist in the normal Engineering/Ops background executor. The
executor returns compact evidence; the parent consolidates it after the
automatic completion notification. Do not create a dedicated DB-maintenance
agent.

This checklist is one of three store-integrity lenses (learnings, knowledgebase,
DB) that Engineering Review may load together — see the `pulse-review-fixer`
contract. Its
output is evidence for the one Engineering Review result, never a standalone
reviewer result.

Return only: `module=technical_review`, `verdict`, `db_ownership_manifest`,
`ownership_candidates`, `next_check`, and ordered `findings`.
Every finding includes stable `finding_id`, `target_key`, severity,
plain-language summary, precise `evidence`, a bounded `recommended_fix`,
migration risk, exact verification commands under `verification`, and
`user_judgment_required` with reason.
Use the remaining document only as the database-health audit checklist.

`db_ownership_manifest` is **required.** For every relevant table and every
content-bearing TEXT/JSON column, report its structured-state purpose, logical
entity, keys, writers, runtime/report/eval consumers, retention/lifecycle,
bounded sample evidence, and authoritative owner. `ownership_candidates`
records each misplaced or duplicated semantic item with `item`,
`current_location`, `semantic_type`, `authoritative_owner`,
`duplicate_locations`, `recommended_action`, and exact `verification`.

Read typed Pulse findings and review history for prior context and matching open
findings, but do not mutate them. Use targeted semantic reads only. The parent
agent owns any approved mutation and its typed disposition.

For a claimed schema/data regression or a repair awaiting proof, compare the
current state with up to three comparable retained runs (same route/group and
materially equivalent configuration). Inspect compact rows/receipts first and
open raw execution traces only for the differing writer or consumer. State an
evidence limitation when fewer comparable runs remain.

Apply the parent-provided `assumption-audit` DB lens within this command's boundaries. Check whether schemas, enums, keys, or cardinality unnecessarily hardcode one source, channel, entity type, group, or current tactic. Recommend safe contract changes, but do not perform speculative row migrations; surface a consequential strategy/schema choice for Pulse's Assumptions challenged when business judgment is required.

## Database purity and ownership contract

The DB owns structured operational state: entities, metrics, actions, queues,
statuses, timestamps, relationships, and historical rows with stable keys. It
does not own long-form execution HOW (Learnings), goals/preferences/constraints
(Soul), current workflow strategy or behavior (Plan), deterministic proof
contracts (Validation), durable domain documents/facts (Knowledgebase), or
review findings/diagnosis/attempts/decisions/fix narratives (Pulse).

Enforce **one semantic item, one authoritative owner**. Structured references
to an owner's stable ID/path are valid; copied prose or JSON snapshots that can
drift are not. Flag content-bearing TEXT/JSON columns used as hidden prompt,
plan, skill, KB, or Pulse stores. A risky semantic row migration requires
`user_judgment_required=yes`; never infer new row meaning merely to make the
manifest clean.

BOUNDARIES

1. Return one concrete recommended instruction and optional focus for the Pulse Fixer; there is no separate DB-maintenance tool.
2. Review DB contracts and data; read relevant producers and consumers for evidence. Return recommendations only. The reviewer edits no files or rows, including `db/`.
3. Treat `db/db.sqlite` as structured state, not scratch output. Recommend row deletion, value transformation, or semantic migration only within explicit user authorization; execution belongs to the parent fixer through managed tools.
4. Prefer contract and schema improvements: `db/README.md`, table schema consistency, PRIMARY KEY / index clarity, report compatibility (the `sql` widgets resolve), and data integrity.

READ FIRST

1. Read `soul/soul.md` if present to understand the workflow objective and success criteria.
2. Read `planning/plan.json` and `planning/step_config.json` if present. Identify steps that produce, consume, save, track, upsert, append, deduplicate, or report persistent data.
3. Read `db/reports/index.html`. Map each internal report view to its `window.report.query` SQL and durable file/asset sources.
4. Read `db/README.md` if present, then inspect tables and DDL through `query_workflow_db` (bounded queries against `sqlite_master`); also note `db/assets/`. Never open the database through shell, Python, or `sqlite3`.
5. Sample each relevant table enough to understand shape. Do not dump whole tables; use `query_workflow_db` with `SELECT * FROM <table> LIMIT 5`, `SELECT COUNT(*)`, and targeted queries.
   Include every content-bearing TEXT/JSON column in `db_ownership_manifest`,
   using bounded samples and length/count summaries rather than whole dumps.
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
   actions such as `query_workflow_db(action="integrity_check")`. Do not send
   raw PRAGMA SQL. For control-state findings, also
   include one bounded source-of-truth comparison query and one assertion that
   proves the runtime decision consumed the canonical value.
4. The parent agent owns every DB/file mutation and records the outcome through
   typed Pulse tools.
