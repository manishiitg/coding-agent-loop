Migrate this workflow's durable store from per-file `db/*.json` to a single SQLite database `db/db.sqlite` (one table per JSON file), then rewrite report widgets to query SQL.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

You perform this migration **inline yourself** using `execute_shell_command` (you have `sqlite3`, `python3`, `jq`, `mv`) and `diff_patch_workspace_file`. There is no separate tool — you do the work. Work entirely under `{{.WorkspacePath}}`.

This is a one-time, irreversible-by-default conversion. Be careful: never lose rows. Keep the original JSON as a backup so the step is reversible.

BOUNDARIES

1. Write only under `db/` and `reports/`. Do not touch `planning/`, `learnings/`, `knowledgebase/`, `evaluation/`, or run outputs.
2. Preserve every row and field. Do not drop, rename, or transform row values — only change the storage format and (for nested objects/arrays) wrap them as JSON text in a column.
3. `db/metrics_history.jsonl` stays as JSONL — it is backend-owned and append-only. Do NOT migrate it. Leave `db/assets/` (binary media) untouched.
4. Idempotent: if `db/db.sqlite` already exists, STOP and report that this workflow is already migrated.

READ FIRST

1. Read `soul/soul.md` if present (objective / success criteria).
2. Read `db/README.md` if present — it declares each file's `primary_key`, `merge_rule`, `writers`, and `shape`. You will reuse `primary_key` as the table PRIMARY KEY.
3. List `db/` and identify the `*.json` files to migrate (skip `*.jsonl`, `assets/`, and any `_json_backup/`).
4. Read `reports/report_plan.json` if present. Map every widget's `source: db/*.json` and any `sources` + JSONata `query` to the table(s) it will query in SQL. Also scan markdown docs under `reports/` and `db/` for ```report-widget fenced blocks — those embedded widgets must be rewritten too.
5. Sample each JSON file with `jq` to learn its shape and which fields are nested objects/arrays.

MIGRATE (build the database)

1. One table per JSON file. Table name = file basename without `.json`, lowercased, non-alphanumeric → `_` (e.g. `db/job_candidates.json` → table `job_candidates`).
2. Columns = the union of top-level keys across all rows. Use the declared `primary_key` from `db/README.md` as `PRIMARY KEY`. Scalar values map to their natural type (INTEGER / REAL / TEXT); booleans → INTEGER 0/1; nested objects and arrays are stored as JSON **text** (use SQLite's `json(...)` so they round-trip and stay queryable via `json_extract`).
3. Write a small `python3` script (stdlib `sqlite3` + `json`) to do this generically — it is the most reliable way to handle arbitrary/nested JSON. Build into **`db/db.sqlite.tmp`** (not the final name yet). The script must:
   - For each JSON file: load the array, collect the column union, `CREATE TABLE` with the PK, then insert every row (`json.dumps` nested values).
   - Add an index on the primary key and on any field the report widgets filter/sort by.
   - Print per-table row counts as it goes.
4. Empty or non-array JSON files: create the table empty (preserve the contract) and note it.

VERIFY (before committing)

1. For each table, compare counts: `sqlite3 db/db.sqlite.tmp "SELECT COUNT(*) FROM <table>"` vs `jq 'length' db/<file>.json`. They MUST match. If any mismatch, STOP, do not commit, and report the discrepancy.
2. Spot-check one row per table round-trips (including a nested field via `json_extract`).

REWRITE REPORTS

1. For each data widget (table/chart/cards/stat/alert/pivot/text) in `reports/report_plan.json`:
   - Replace `source: "db/<file>.json"` with `db: "db/db.sqlite"` and `sql: "SELECT ... FROM <table> ..."`.
   - Fold any JSONata `query` and multi-file `sources` joins into the SQL (`JOIN`, `WHERE`, `GROUP BY`, `ORDER BY`, `LIMIT`). Remove the now-unused `source`, `sources`, and `query` keys from those widgets.
   - Keep `path`/`filter`/`formats`/`showIf` — they shape the returned rows.
   - `file`/`file-list`/`markdown` widgets keep their `source` (they point at a file, not data) — do NOT touch them.
2. Apply the same rewrite to any ```report-widget embedded blocks you found in markdown docs.
3. Edit `reports/report_plan.json` with `diff_patch_workspace_file`.

FINALIZE

1. Rewrite `db/README.md` to the SQL contract: one section per table with its `CREATE TABLE` DDL, PRIMARY KEY, indexes, writer steps, and which report widgets read it.
2. Move the old JSON aside for rollback: `mkdir -p db/_json_backup && mv db/<each>.json db/_json_backup/`.
3. Promote the database atomically: `mv db/db.sqlite.tmp db/db.sqlite`. (Doing this last makes "db/db.sqlite exists" mean "fully migrated".)
4. Write a marker `db/_migration.json`: `{ "migrated_at": "<ISO time>", "tables": <n>, "rows_verified": true, "source_files": [ ... ] }`.

REPORT

Summarize: tables created with row counts, primary keys used, report widgets rewritten (JSON→SQL), where the original JSON was backed up, and any tables that were empty or needed special handling. State clearly that the workflow is now on SQLite and that `db/_json_backup/` can be deleted once verified.
