Migrate this workflow's durable store from per-file `db/*.json` to a single SQLite database `db/db.sqlite` (one table per JSON file), then rewrite report widgets to query SQL.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

You perform this migration **inline yourself** using `execute_shell_command` (you have `sqlite3`, `python3`, `jq`, `mv`) and `diff_patch_workspace_file`. There is no separate tool — you do the work. Work entirely within the current workflow's root (its `db/` and `reports/` folders).

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
2. **Normalize the JSON to row objects first — NEVER drop ANY non-empty array, object, or field.** The cardinal rule: every piece of source data must land somewhere queryable (a row, a column, or its own table). Putting data "in `db/README.md`" does NOT count — the README is documentation, not storage. Only tiny bookkeeping scalars (`schema_version`, `last_updated`) may be dropped to README. Check these shapes in order:
   - **Array of objects** `[{...}, {...}]` → those are the rows (the common case).
   - **Wrapper object with ONE meaningful nested array** + only scalar/bookkeeping siblings — e.g. `{"_schema": {...}, "employees": [ …42… ], "total_count": 42, "last_synced": "..."}` or `{"records": [ …38… ]}`. Migrate the **inner array** (`employees` / `records`) as the rows; the scalar siblings are bookkeeping → note them in README. (If a scalar sibling is real data, keep it as a column on every row.)
   - **Rich state object with MULTIPLE non-empty collections** — e.g. `action_queue.json` = `{actions:[6], parameter_gaps:[6], notes:[3], source_files:[6], session_policy:{…}, status, …}`. This is the data-loss trap: do NOT pick one array and discard the rest. Preserve everything, by EITHER:
     - **(a) one row, JSON columns** (simplest, fully faithful): make the whole object **one row**; store each nested array/object as a JSON-text column (`json(...)`), keep scalars as their own columns. Queryable later via `json_extract` / `json_each`. Prefer this when the file is one logical state document (a queue, a config, a per-day snapshot).
     - **(b) one table per collection**: split each non-empty nested array into its own table named `<file>_<key>` (e.g. `action_queue_actions`, `action_queue_parameter_gaps`, `action_queue_notes`), plus a single-row `<file>_meta` table for the top-level scalars/objects. Prefer this when the collections are genuinely independent row sets a report will query separately.
     Pick (a) or (b) per file and record which in README. Either way, **no non-empty array or object may be dropped.**
   - **A single object** `{...}` (no nested rows array) → that is **ONE row** (its keys = columns). Do NOT create an empty table — that loses the data. (e.g. `{"status":"allowed"}` → one row `status="allowed"`.)
   - **An object whose values are all row objects** (a map/dict keyed by id, e.g. `{"a":{...},"b":{...}}`) → one row per value; add the key as an `id`/key column if it isn't already a field.
   - **Empty array `[]` / empty object `{}` / object whose only arrays are all empty** → create the table empty (preserve the contract); for a single object with empty embedded arrays, still make it one row with those arrays as empty JSON.
   - **A scalar or array of scalars** → wrap as a single-column table (`value`), one row per element.
3. Columns = the union of top-level keys across all normalized rows. Use the declared `primary_key` from `db/README.md` as `PRIMARY KEY` (only if it is unique across the rows; otherwise note it and use a synthetic key). Scalar values map to their natural type (INTEGER / REAL / TEXT); booleans → INTEGER 0/1; nested objects and arrays are stored as JSON **text** (use SQLite's `json(...)` so they round-trip and stay queryable via `json_extract`).
4. Write a small `python3` script (stdlib `sqlite3` + `json`) — the most reliable way to handle arbitrary/nested JSON. Build into **`db/db.sqlite.tmp`** (not the final name yet). The script must, per file: load + normalize to rows per step 2, `CREATE TABLE` with the PK, insert every row (`json.dumps` nested values), add the PK index + any field report widgets filter/sort by, and print the row count.

VERIFY (before committing) — this is the guard against silent data loss

1. For each table, compute the **expected** row count from the normalized shape and compare to `sqlite3 db/db.sqlite.tmp "SELECT COUNT(*) FROM <table>"`:
   - array → `jq 'length'`; wrapper with nested array → `jq '.<key> | length'` (the inner array, e.g. `jq '.employees | length'`); single object / state-doc as one row (option a) → **1**; state-doc split into tables (option b) → each `<file>_<key>` table = `jq '.<key> | length'`; keyed map → `jq 'length'` (number of keys); empty → 0.
   Do NOT blindly `jq 'length'` on the file (it returns the top-level key-count for a wrapper/object, not the row count). They MUST match. If any mismatch — especially a non-empty source mapping to 0 rows — STOP, do not commit, and report the discrepancy.
2. **No-dropped-data check (critical for rich state objects):** for each source file, list every top-level key whose value is a non-empty array or object; confirm each one is represented in SQLite (as a table, or as a non-null JSON column on the row). If any non-empty array/object from the source has no home in the DB, STOP — that is silent data loss.
3. Spot-check one row per table round-trips (including a nested field via `json_extract`).

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
