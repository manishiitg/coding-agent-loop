## main.py authoring rules

Apply these when writing or patching a step's `main.py`. Scripts must run identically for every group/user, every iteration — so the rules err toward strictness.

**Environment access (strict)**
- `os.environ['KEY']` ALWAYS. NEVER `os.environ.get('KEY', 'fallback')`. A missing env var must raise KeyError — silent fallbacks hide misconfig.
- Workflow variables → `VAR_<NAME>` (config: user IDs, sheet IDs, URLs).
- Secrets → `SECRET_<NAME>` (passwords, API keys, tokens).
- Special vars: `STEP_OUTPUT_DIR` (write all step outputs here), `STEP_EXECUTION_DIR` (parent execution folder; use for sibling-step reads only, never as a write target), `DB_PATH` (**ABSOLUTE** path to the workflow `db/db.sqlite` — ALWAYS use `os.environ['DB_PATH']` / `"$DB_PATH"` for sqlite; never a relative `db/db.sqlite`: a step's working directory is its own execution folder, not the workflow root, so a relative path fails with "unable to open database file" or silently writes a stray empty db), `MCP_API_URL`, `MCP_API_TOKEN`, `VAR_GROUP_NAME` (use `.get('VAR_GROUP_NAME', '')` — this one is optional).
- NO hardcoded user IDs, account numbers, URLs, paths, or credentials. Every dynamic value flows from env or sys.argv.
- **The step description shows RESOLVED current-run values.** Those are for context only. NEVER copy any name, ID, or literal value from the description into the script — or into any `export` you issue manually. The same script runs for every group/user; a copied value from one run breaks the others.

**Input/output**
- Input data arrives via `sys.argv[1]`, `sys.argv[2]`, ... — these are the resolved `context_dependencies`. Read them.
- NEVER construct paths to sibling step folders (e.g. `execution/login-step/output.json`). The controller resolves correct per-group paths and passes them as sys.argv. If you need data not in sys.argv, add it as a `context_dependency` in `plan.json` — do not hardcode.
- Write output files to `os.environ['STEP_OUTPUT_DIR']` with the exact filenames and structure the validation_schema requires. `STEP_OUTPUT_DIR` is **volatile** (per-run, wiped on re-run). A durable **file** that later steps, runs, or the builder must reach — a download, generated PDF/CSV/image/zip, any format — goes under `db/assets/` (write it via the workspace root, e.g. `os.path.join(os.path.dirname(os.environ['DB_PATH']), 'assets', name)`), with a reference row in `db.sqlite`. `db/assets/` is the only durable location a step can write an arbitrary file; a custom folder is denied by the sandbox.

**Data authenticity — no fabrication**
- Every value written to output files MUST trace to a real MCP tool call, API response, or input file. No hardcoded rows, no invented records.
- If the script writes output without making any external calls or reading real input, it will be rejected.

**Logging**
- `VERBOSE = os.environ.get('SCRIPT_VERBOSE', '') == '1'`. Guard debug prints with `if VERBOSE:`. Log state before and after each major action. Stdout is the ONLY debugging channel available to the fix loop.

**Robustness across groups**
- The same script runs for every group/user with different data. Use `.get()` with safe defaults for *data* fields (not env vars), handle empty lists, `None` values, date-as-string-vs-number variants, missing optional files.
- Print diagnostic context BEFORE raising. The error output is how the next fix pass understands what broke.
- If the same script keeps failing for specific groups, branch on `os.environ.get('VAR_GROUP_NAME', '')` rather than forcing one code path.

**Patching discipline**
- Edit `learnings/{step-id}/main.py` — this is the source of truth. NEVER edit `execution/{step-id}/code/main.py`; the controller overwrites it from learnings on every run.
- Prefer `diff_patch_workspace_file` for targeted changes — preserves working code and reduces regressions. Full rewrite (cat-heredoc) only when restructuring large portions.
- Helper files alongside main.py also live in `learnings/{step-id}/` — patch them the same way.

**Output format — HTML vs JSON vs Markdown**

- **JSON** — for structured data consumed by downstream steps or db writes. Always use JSON for `context_output` files other steps read.
- **Markdown (`.md`) — the default for human-readable output**: reports, analyses, summaries. It renders richly in the file viewer (headings, tables, lists), gets clickable workspace file links that HTML doesn't, and is simpler and more robust to author than self-contained HTML.
- **HTML** — only when you genuinely need rich/branded layout markdown can't express. For an actual dashboard, use the report system (`reports/report_plan.json` + `window.report`), which is the purpose-built HTML surface — don't hand-author a standalone `.html` report as the default.
- Do NOT make HTML copies of Markdown stores (`soul.md`, learnings, KB) — those stay Markdown and are read as Markdown.

Before writing a `.html` output file, call `get_reference_doc(kind="html-output")` — it has the full layout baseline, dark-mode styles, inline chart pattern, and quality checklist.
