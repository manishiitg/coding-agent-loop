## main.py authoring rules

Apply these when writing or patching a step's `main.py`. Scripts must run identically for every group/user, every iteration — so the rules err toward strictness.

**Source layout (read workflow.json first)**
- `code_layout_version: 1`: saved source is `code/<step-id>/main.py`. Steps execute it directly and may read/write shared helpers anywhere under this workflow's `code/` when unlocked. There is no execution copy or copy-back. Repair the canonical code and retry. `WORKFLOW_CODE_ROOT` is the absolute import root; use packages such as `from shared.utils import ...` with `__init__.py` where needed. Never infer paths from the DB or directory depth.
- Absent/zero `code_layout_version`: legacy source remains `learnings/<step-id>/main.py` with execution copies and controller save-back. Do not move an existing workflow to the new layout or infer its version from folder existence. The legacy path examples below apply only to this version.
- New-layout Python dependencies: use `python3 -m pip install --target "$WORKFLOW_CODE_DEPS" <package>`. The controller adds this persistent workflow directory to PYTHONPATH for both execution and repair shells. Verify with the actual `execute_step` runner for the intended group. Keep outputs in `STEP_OUTPUT_DIR` or durable `db/assets/`, never mixed into code.

**Environment access (strict)**
- Use `os.environ['KEY']` for required configuration, credentials, and paths. A missing required variable must raise KeyError; never mask it with a fallback. Explicitly optional context/diagnostic flags such as `VAR_GROUP_NAME` and `SCRIPT_VERBOSE` may use `.get()` with a documented safe default.
- Workflow variables → `VAR_<NAME>` (config: user IDs, sheet IDs, URLs).
- Secrets → `SECRET_<NAME>` (passwords, API keys, tokens).
- Special vars: `STEP_OUTPUT_DIR` (write all step outputs here), `STEP_EXECUTION_DIR` (parent execution folder; never a write target or a substitute for controller-resolved context dependencies), `DB_PATH` (**ABSOLUTE** path to the workflow `db/db.sqlite` — ALWAYS use `os.environ['DB_PATH']` / `"$DB_PATH"` for sqlite; never a relative `db/db.sqlite`: the working directory is the canonical step source directory in version 1 or a run directory in legacy, not the workflow root, so a relative path fails with "unable to open database file" or silently writes a stray empty db), `MCP_API_URL`, `MCP_API_TOKEN`, `VAR_GROUP_NAME` (use `.get('VAR_GROUP_NAME', '')` — this one is optional).
- A parameterized scripted step reads its non-secret, per-call values from `json.loads(os.environ['STEP_PARAMS_JSON'])`. Its plan-level `script_parameters` declaration is the only public contract: do not add a competing CLI flag or free-form instruction path. `context_dependencies` remain positional `sys.argv` inputs.
- NO hardcoded user IDs, account numbers, URLs, paths, or credentials. Every dynamic value flows from env or sys.argv.
- **The step description shows RESOLVED current-run values.** Those are for context only. NEVER copy any name, ID, or literal value from the description into the script — or into any `export` you issue manually. The same script runs for every group/user; a copied value from one run breaks the others.

**Input/output**
- Input data arrives via `sys.argv[1]`, `sys.argv[2]`, ... — these are the resolved `context_dependencies`. Read them.
- NEVER construct paths to sibling step folders (e.g. `execution/login-step/output.json`). The controller resolves correct per-group paths and passes them as sys.argv. If you need data not in sys.argv, add it as a `context_dependency` in `plan.json` — do not hardcode.
- Write output files to `os.environ['STEP_OUTPUT_DIR']` with the exact filenames and structure the validation_schema requires. `STEP_OUTPUT_DIR` is **volatile** (per-run, wiped on re-run). A durable **file** that later steps, runs, or the builder must reach — a download, generated PDF/CSV/image/zip, any format — goes under `db/assets/` (write it via the workspace root, e.g. `os.path.join(os.path.dirname(os.environ['DB_PATH']), 'assets', name)`), with a reference row in `db.sqlite`. Use `db/assets/` for durable output files. Version 1 source and shared helpers belong in `code/`; other paths require explicit Folder Guard grants.
- Check `python3 -m pip --version` before installing a Python dependency. Missing pip/venv is a server prerequisite failure; report it rather than repeatedly rewriting the test. For version 1, install with `python3 -m pip install --target "$WORKFLOW_CODE_DEPS" package`; the controller includes that persistent directory in `PYTHONPATH`. Do not choose a separate venv interpreter for a saved script: the runner uses `python3`. For legacy workflows verify the actual runner dependency path before installing. Never disable host package protections. Package caches persist under `.sandbox-cache/`. A deterministic saved browser suite may use Python Playwright directly; first read `references/playwright-scripted.md`. Anything needing root or `apt` must be installed by the server operator.

**Data authenticity — no fabrication**
- Every value written to output files MUST trace to a real MCP tool call, API response, or input file. No hardcoded rows, no invented records.
- If the script writes output without making any external calls or reading real input, it will be rejected.

**Deliberate refusal — fail-closed guards must exit code 2, not 1**
- Any non-zero exit is treated as a bug by default: the failure is handed back to you as repair context so you fix the script. That is correct for a real error, but wrong for a guard that deliberately detected an unsafe condition (stale data, a write that would overwrite history it could not verify, a precondition that isn't met) and refused to proceed on purpose.
- Use `sys.exit(2)` — not `sys.exit(1)` or any other code — for that second case. Exit code 2 is reserved and means "this refusal is terminal, do not attempt an agentic workaround." The step fails outright instead of falling back to a relearn turn, and the refusal is never handed to an agent as "here is an error, fix it."
- Print the reason for the refusal to stdout before exiting — that text becomes the step's failure detail, so it must say plainly what condition was detected and why proceeding was unsafe.
- Get this distinction right: `sys.exit(1)` on a guard that should be terminal lets an agentic retry read your own refusal, agree it was correct, and then perform the exact write the guard existed to prevent — which has happened live. `sys.exit(2)` on an ordinary bug wrongly aborts the step instead of letting the normal repair loop fix the script.

**Logging**
- `VERBOSE = os.environ.get('SCRIPT_VERBOSE', '') == '1'`. Guard debug prints with `if VERBOSE:`. Log state before and after each major action. Stdout is the ONLY debugging channel available to the fix loop.

**Robustness across groups**
- The same script runs for every group/user with different data. Use `.get()` with safe defaults for optional *data* fields (never required configuration), handle empty lists, `None` values, date-as-string-vs-number variants, missing optional files.
- Print diagnostic context BEFORE raising. The error output is how the next fix pass understands what broke.
- If the same script keeps failing for specific groups, branch on `os.environ.get('VAR_GROUP_NAME', '')` rather than forcing one code path.

**Code documentation — builder and repair agents**
- Apply these requirements when creating or repairing scripts and shared helpers. Keep documentation proportional to the code; a simple test should remain simple.
- Start each entry point with a concise module docstring explaining its purpose, required environment-variable names and input arguments, outputs/side effects, and how it is executed through the platform. Document names and formats, never actual credentials or sensitive values.
- Give shared helpers and non-obvious functions docstrings describing their inputs, return values, side effects, and important failure behavior. Use clear names and small functions; do not comment every obvious assignment or repeat the code in prose.
- Explain the reasoning behind non-obvious assertions, selectors, retries, timeouts, ordering constraints, and workarounds in nearby comments. Identify observed limitations honestly; do not invent explanations for failures.
- During repair, update affected docstrings and comments together with the implementation. Remove stale explanations. Explain a workaround's reason and when it can be removed; keep the repair history in the run/repair report rather than accumulating dated change logs in the source.
- Before finishing, check that the documented inputs, outputs, shared-helper behavior, and failure conditions agree with the changed code and the verification actually performed.

**Patching discipline**
- In builder chat, edit the saved source selected by the manifest: `code/{step-id}/main.py` for version 1, `learnings/{step-id}/main.py` for legacy. During controller-managed authoring/repair, use the explicit working directory supplied in that turn. Version 1 edits canonical source in place; only legacy execution copies are saved back by the controller. Never patch a stale run copy from builder chat.
- Prefer `diff_patch_workspace_file` for targeted changes — preserves working code and reduces regressions. Full rewrite (cat-heredoc) only when restructuring large portions.
- Version 1 helpers may live anywhere in the workflow's `code/` tree; import shared packages through `WORKFLOW_CODE_ROOT`, already included in `PYTHONPATH`. Unlocked steps may repair shared helpers in place. Keep outputs in `STEP_OUTPUT_DIR` or durable `db/assets/`, not in source folders.
- For legacy workflows, keep helpers adjacent to main.py in `learnings/{step-id}/`; the controller copies them with the entry point. Do not infer source/import roots from `__file__.parents[...]` or `DB_PATH`, or assume `learnings/_global/scripts` is on the runtime import path.
- Test with `execute_step` for the intended group so the controller supplies the runtime environment and input arguments. When the step declares `script_parameters`, pass its test values in `execute_step(script_parameters={...})`; the controller validates them and supplies the exact same `STEP_PARAMS_JSON` contract used by orchestrator routes. A generic builder shell is not a step execution: do not invent `STEP_OUTPUT_DIR` or bypass managed DB access to simulate it. Missing declared `VAR_*` in a builder shell is a platform environment issue to report.
- Shell commands must be POSIX-compatible unless explicitly wrapped in `bash -lc`; `set -o pipefail` requires Bash. Tool JSON does not expand shell variables: resolve `os.environ['VAR_BASE_URL']` before passing the URL to `agent_browser`.
- Distinguish a failed assertion from a script that could not run. Inspect the actual page/result and diagnostics before repairing an assertion; do not weaken expected behavior merely to obtain a pass.

**Never hand-escape prose into a string literal**

Text you are inserting into generated code — an HTML fragment, a log line, a
sentence for the user — has usually already been escaped once on its way to you
through the tool call. Escaping it again is what produces a backslash before the
quote, and a backslash before a quote does not escape it: the quote still closes
the string.

A real failure: `'page\\'s Current work counts...'` — an apostrophe in the word
"page's" written as `\\'`, which Python reads as a literal backslash followed by
a string-terminating quote. `SyntaxError: unterminated string literal`. It was
the third attempt at the same file (`migrate3.py`), because each retry rewrote
the same prose the same way.

Keep text out of code instead of trying to quote it correctly:

- Write the text to its own file and have the script read it. Prose in a data
  file cannot break the parser that reads the code.
- If it must be inline, use a triple-quoted block and pick a delimiter the text
  does not contain. Do not add backslashes by hand.
- When writing the file through the shell, use a QUOTED heredoc delimiter —
  `<< 'PYEOF'` and not `<< PYEOF`. Quoting the delimiter stops the shell
  expanding or re-escaping anything in the body, which is the other half of the
  same problem.
- Prefer `diff_patch_workspace_file` over a full heredoc rewrite. A targeted
  patch touches fewer lines, so there is less text to get wrong.

If a generated script fails to parse twice, stop rewriting it the same way. The
quoting is the bug, not the logic — move the text out of the code.

**Output format — HTML vs JSON vs Markdown**

- **JSON** — for structured data consumed by downstream steps or db writes. Always use JSON for `context_output` files other steps read.
- **Markdown (`.md`) — the default for human-readable output**: reports, analyses, summaries. It renders richly in the file viewer (headings, tables, lists), gets clickable workspace file links that HTML doesn't, and is simpler and more robust to author than self-contained HTML.
- **HTML** — only when you genuinely need a rich/branded layout markdown cannot express. For an actual dashboard, use the single workflow-owned `db/reports/index.html` experience with `window.report`; do not create separate platform pages or a JSON layout registry.
- Do NOT make HTML copies of Markdown stores (`soul.md`, learnings, KB) — those stay Markdown and are read as Markdown.

Before writing a `.html` output file, call `read_skill(skills=[{"name":"builder-reference","path":"references/html-output.md"}])` — it has the full layout baseline, dark-mode styles, inline chart pattern, and quality checklist.
