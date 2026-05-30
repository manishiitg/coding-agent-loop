Critical audit of the workflow design — the comprehensive review. Where /design-flow asks "what would a designer make better," this asks "what's wrong, weak, risky, stale, or unjustified, and which steps or artifacts need attention." Review the plan plus the dependent artifacts that make it executable: step config, learnings, saved scripts, KB notes, db JSON files, report wiring, variables, and evaluation coverage.{{if eq .WorkshopMode "run"}} In Run mode, return findings in chat only; do not write files.{{else}} Findings go to builder/review.html as recommendations; nothing is applied here.{{end}}{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Before writing builder/review.html, call get_reference_doc(kind="html-output") to load the HTML style guide. Findings use .badge.fail for CRITICAL, .badge.warn for WARNING, .badge.pass for INFO/resolved. The file is regenerated (not appended) each run — read the existing file first to carry forward unresolved findings.

MIGRATION (one-time): Check whether builder/review.md exists. If it does, read it in full, extract every unresolved F-... finding and any important context, incorporate them into the new builder/review.html, then delete builder/review.md with execute_shell_command. Do this before writing the new HTML so nothing is lost.

The audit has four phases. Run each in order. Skip Phase 3's orchestrator/router block when the workflow has no todo_task, routing, or orchestration steps. Do not skip Phase 4; if an artifact surface is absent, say whether that absence is acceptable or a finding.

PHASE 1 — STRUCTURAL ANALYSIS

1. Call review_plan() — the server-side review tool. It analyzes plan structure: step boundaries, step types, execution modes, context flow integrity, validation coverage, portability, and whether choices are justified by the objective + success_criteria from soul.md.
2. review_plan runs in the background and returns an execution_id. Capture that id and wait for completion before continuing. Use query_step(execution_id) to inspect status/results when needed; do not start Phase 2 or write the review log until Phase 1 has completed and you have the review_plan output.
3. Read its output carefully. Group findings by severity: CRITICAL (broken structure, missing required fields, contradictions vs soul.md), WARNING (questionable choices that need defense), INFO (style/minor).
4. Compare against soul.md's objective + success_criteria explicitly: for each weak structural choice, name which criterion it fails or under-serves.

PHASE 2 — PER-STEP DESCRIPTION AUDIT

For every executable step in plan.json, read the description. This includes top-level steps, todo_task routes, routing routes, and referenced orphan_steps. Read learnings/_global/SKILL.md once as the shared HOW-to-run source. For steps with learning writes or locked learning, inspect learnings/{step-id}/.learning_metadata.json when present. For scripted steps, inspect learnings/{step-id}/main.py when present. For agentic steps, verify learnings/{step-id}/main.py does not exist; if it does, flag it for deletion as a stale script. Apply each lens; skip a lens when it doesn't fire.

LENS 0 — Durable Boundary Fit
- **Do not flag size alone**: modern agents can handle long context and many tool calls. A step is not wrong just because it performs many screen actions, file reads, API calls, or small transformations.
- **Over-merged boundary**: flag when one step owns unrelated durable outputs, validation gates, retry/failure domains, tool/security contexts, downstream contracts, persistent stores, human approvals, or routing decisions.
- **Over-split boundary**: flag when adjacent steps share the same objective/output contract, fail and retry together, use the same tools/security context, produce only pass-through artifacts, or exist only to hand context to the next step.
- **Boundary truth**: split for durable contracts; combine for scratch intermediates and coherent agentic work.

LENS A — Description vs Skill Confusion
- **Description contains runtime learnings**: the description should be an *instruction* (what to do), not a *retrospective* (what worked last time). "Use batch mode because single inserts timeout", "avoid X which caused failures", or specific tool parameter values discovered at runtime belong in SKILL.md, not the description.
- **Skill contains task instructions**: SKILL.md should capture *reusable patterns and pitfalls discovered during execution*, not restate what the step is supposed to do. If the skill reads like a task description, it's confused.
- **Duplication**: same guidance appearing in both description and skill — pick one home.
- **Description defers to skill**: phrases like "follow the skill" or "see learnings" instead of giving clear instructions.

LENS B — Hardcoded Values
- **Hardcoded paths**: absolute paths like `/app/workspace-docs/...`, `/Users/...`, `/home/...`, or specific local paths. Should use workspace-relative or workspace-rooted paths instead.
- **Hardcoded run/iteration paths**: references to `runs/iteration-0/...`, `execution/step-3/...`, or hardcoded group names like `group-1`. These break across runs and groups — the orchestrator resolves these via context_dependencies at runtime.
- **Hardcoded credentials/secrets**: API keys, tokens, passwords, auth headers. Should reference `SECRET_*` environment variables.
- **Hardcoded IDs/URLs/user-specific values**: spreadsheet IDs, database names, API endpoints, user IDs, email addresses, phone numbers, account numbers. Should use variable placeholders (e.g., `{USER_ID}`, `{SHEET_ID}`, `{EMAIL}`) in descriptions, with actual values in `variables/variables.json` / variable groups.

LENS C — Browser Anti-Patterns (only for steps that use playwright/browser/agent_browser)
- **Prescribes browser_evaluate for interactions**: description tells the LLM to use `browser_evaluate`/`eval` to click, fill, or navigate. Should say "take a snapshot, find the element, click/type using its ref" instead.
- **Prescribes CSS selectors**: patterns like `browser_click({'selector': '...'})` or `browser_type({'selector': '...'})`. Use ref-based interaction from snapshots.
- **Prescribes hardcoded element references**: specific DOM selectors, iframe indices, or element IDs that may change. Describe intent ("find the password field", "click the login button") and let the LLM discover elements via snapshot.
- **Over-specifies implementation**: description dictates exact tool calls and parameters instead of describing WHAT to accomplish. For scripted steps, the description should focus on the goal and let the LLM figure out the implementation using `get_api_spec` and snapshots.

LENS D — Missing Pre-Validation Schema
- **No validation_schema**: every step that produces a context_output should have a `validation_schema` defined. Without it, there's no automated quality gate — a step can produce garbage and downstream steps will blindly consume it. Check that `validation_schema` exists, has file checks matching the context_output filename, and includes meaningful `json_checks` (not just `must_exist`).

PHASE 3 — ORCHESTRATOR / ROUTE AUDIT (skip if no todo_task, routing, or orchestration steps)

For every step where `step_type == "todo_task"`, `routing`, or `orchestration`, read its description and ALL route/sub-agent descriptions, including referenced orphan_steps. Apply each lens.

LENS E — Orchestrator / Router Description Quality
- **Missing objective/intent**: the parent description must clearly state WHAT we are trying to achieve — the overall goal. Without this, the orchestrator/router can't make intelligent decisions when things go wrong or unexpected situations arise. A good parent description answers: "Why do these routes/sub-agents exist together? What outcome are we after?"
- **Reduced to a sequencer**: if the description is just "run route A, then route B, then route C" or a fixed checklist, the parent may be over-engineered. If all it does is follow a fixed order, these should often be regular steps in sequence instead.
- **No edge case / failure guidance**: the description should explain how to handle failures, retries, partial results, missing data, or unexpected route/sub-agent states.
- **No routing criteria**: the description doesn't explain WHEN or WHY to pick each route. The parent needs to know what conditions, inputs, or states map to which sub-agent.

LENS F — Orchestrator vs Sub-Agent Boundary
- **Inline execution logic**: detailed task instructions for a specific sub-task written inside the orchestrator description. Each distinct task should be its own route with its own description, learnings, and tools. Orchestrator dispatches; sub-agents execute.
- **Duplicates sub-agent descriptions**: orchestrator restates what sub-agents already describe. Orchestrator should focus on coordination and decision-making.
- **Sub-agent descriptions too vague**: route descriptions that are too thin because all the detail is in the orchestrator. Each sub-agent should be self-contained — a junior agent reading only its own description should know exactly what to do.

LENS G — Sub-Agent Hardcoded Values
- Same hardcoded-value checks from Lens B applied to sub-agent route descriptions (paths, run/iteration paths, credentials, IDs/URLs).

PHASE 4 — DEPENDENT ARTIFACT AUDIT

Treat plan, config, learning, KB, db, reports, variables, and eval as one workflow contract. A plan review is incomplete if a step change leaves one of these surfaces stale or underspecified.

1. Read `planning/step_config.json`, `variables/variables.json`, `evaluation/evaluation_plan.json`, `reports/report_plan.json`, `builder/review.html`, `learnings/_global/SKILL.md`, `knowledgebase/context/context.md`, `knowledgebase/notes/_index.json`, `db/README.md`, and relevant files under `learnings/{step-id}/`, `knowledgebase/notes/`, `db/`, and `db/assets/` when present. If a file is absent, decide whether absence is acceptable for this workflow.
2. **Learning audit**:
   - Check every step with `learnings_access=read-write` has a real reason and a concrete `learning_objective`. SKILL.md is written by the step agent itself during a dedicated post-completion turn — `learning_objective` is the instruction the step agent uses to know what to extract, so it must be specific (selectors, timings, auth flows, tool-call patterns, API quirks) rather than generic ("learn from the run").
   - For every step with learning writes or `lock_learnings=true`, read `learnings/{step-id}/.learning_metadata.json` when present. Check `successful_runs`, `description_hash_runs`, `consecutive_no_new_learning_runs`, `auto_locked_at`, `auto_lock_reason`, `auto_unlocked_at`, and recent detection history.
   - Flag `lock_learnings=true` when metadata is missing, metadata shows fewer than 3 same-description successful runs, direct learning lacks repeated no-new-learning outcomes, metadata was auto-unlocked after a description change, or step_config successful_runs/lock state contradicts metadata.
   - Flag any `agentic` step with `learnings/{step-id}/main.py`; agentic does not run or maintain persistent main.py, so the file is stale and should be deleted.
   - Flag learning writes for plumbing steps with no reusable HOW-to-run knowledge.
   - Flag browser-based steps using `scripted`; browser work should stay agentic/agentic unless there is an explicit user request plus a defensible 10+ run scenario-coverage exception.
   - Flag `scripted` unless all three gates are satisfied: user explicitly asked for it, the step is highly deterministic, and 10+ successful runs cover the relevant scenarios/groups with eval/metric evidence at target. Also flag it while recent metric evidence is inconclusive or while the step behavior is still changing.
   - Check `learnings/_global/SKILL.md` for stale step names, task descriptions masquerading as learnings, duplicated plan instructions, hardcoded values, and advice that contradicts current descriptions.
   - Flag bloated `SKILL.md` content: the root skill should be a lean index/overview (roughly under 80-100 lines) that links to focused files under `learnings/_global/references/`. Detailed selectors, auth/API quirks, browser timing, file-format rules, retry patterns, and step-specific HOW guidance should be moved into reference files.
   - For scripted steps, inspect `learnings/{step-id}/main.py` and `learnings/{step-id}/script_metadata.json` when present. Flag stale code, missing lock rationale, brittle hardcoded values, browser automation scripts, and code writing to the wrong store.
3. **Knowledgebase audit**:
   - Steps that produce durable narrative domain observations should declare `knowledgebase_access` plus a useful `knowledgebase_contribution`.
   - Steps that need user-provided runtime context from `knowledgebase/context/context.md` should declare `knowledgebase_access=read` or `read-write` AND their descriptions should explicitly name the relevant context section/path to read and apply. Flag either half missing: KB access without a description mention, description mention without KB read access, or reliance on chat memory instead of this file.
   - Prefer `knowledgebase_write_method=direct`; `agent` is only when the user explicitly wants a separate KB writer/reviewer.
   - Check `knowledgebase/context/context.md` contains user-supplied runtime rules/preferences/context, not workflow-discovered notes or execution recipes. It is user-owned; do not recommend optimizer rewrites except explicit user-requested cleanup.
   - Check `knowledgebase/notes/_index.json` points to coherent topic files and that topic notes contain durable WHAT-we-know facts discovered by the workflow, not execution recipes, run logs, raw rows, or user-owned runtime context that belongs in `knowledgebase/context/context.md`.
   - Flag steps that read KB without a clear need, write KB without a contribution contract, or store KB-worthy domain facts only in context outputs/db/learnings.
4. **Database audit**:
   - From `planning/plan.json`, find every step description that says it writes, saves, tracks, stores, accumulates, appends, caches, deduplicates, or reports data. Map those steps to concrete `db/*.json` files.
   - Read `db/README.md` if it exists. Then list `db/*.json` files and sample each non-empty file.
   - Read `reports/report_plan.json` if present and map which widgets consume which `db/*.json` paths.
   - For each `db/*.json` file, check:
     - **Documented schema exists** in `db/README.md` with purpose, shape, primary_key, merge_rule, writers, consumers, and report widgets that depend on it.
     - **Stable JSON shape**: top-level object or array is intentional; rows have consistent keys/types; no mixed unrelated record types in one file.
     - **Primary key discipline**: array-like tables have a stable primary key (`id`, `group_name`, domain id, composite id). If no primary key exists, repeated runs will duplicate or clobber rows.
     - **Merge/upsert rule**: each writer description says read existing file, merge by primary key, preserve unrelated rows/groups, and write back. Flag wholesale overwrite risk.
     - **Writer ownership**: every file has one clear owner step or explicitly documented multi-writer rules. If multiple steps write the same file, their fields and merge responsibilities must not conflict.
     - **Group/run separation**: group-specific rows include `group_name` or another scoped key when multiple variable groups can run. Do not rely on folder names inside `db/` data.
     - **No volatile run paths as data model**: report widgets and downstream steps should bind to `db/*.json`, not `runs/iteration-*` files. Stored paths may reference run artifacts only when intentionally archival and documented.
    - **Report compatibility**: widget source paths, expected fields, aggregation/grouping keys, and chart/table fields exist in the sampled data.
     - **Asset discipline**: durable binary/media files live under `db/assets/`, with metadata/provenance/reference rows in `db/*.json`; no base64 blobs or large binaries embedded inside JSON rows.
     - **Data hygiene**: no duplicate primary keys, stale test rows, impossible nulls, mixed date formats, or fields that silently changed names across rows.
   - **Message-sequence item write access**: for every `message_sequence` step, check each item whose `message` or `output_files` writes to `db/` or `knowledgebase/`. That item MUST declare the matching `write_access` (`{"db": true}` and/or `{"knowledgebase": true}`) or an inferring `kind` (`db` / `knowledgebase` / `code`). Item writes are default-deny and folder-scoped — booleans only, a per-file `paths` list is invalid and ignored — while reads are always open, so a missing grant is easy to overlook and the write is silently blocked at runtime (the step then loops or fails late). Flag CRITICAL: name the item id and the file it tries to write.
   - For each step that writes `db/`, check that its description references the `db/README.md` contract and names the file, primary key, and merge rule. If it only says "save the result" or writes to a run folder, flag it.
5. **Report audit**:
   - Check `reports/report_plan.json` widgets source durable `db/*.json`, `db/assets/` references, KB context/notes, or built-in APIs rather than volatile run folders.
   - Check every referenced field exists in sampled source data and each widget has a clear owner/source step.
   - Flag report widgets whose source data is not produced by any step or whose data contract is undocumented.
   - Check whether widgets are using the Report UI's JSONata `query` feature where appropriate. The pipeline is `source -> query -> path -> filter -> render`; when `query` returns the final array/scalar, `path` should be empty or `$`.
   - Flag derived/helper report files like `*_rows.json`, `*_summary.json`, `flat_*.json`, or a `step-generate-report` / "flatten data" step when the same result can be expressed as a widget `query` against the canonical `db/*.json` source.
   - For report findings, recommend collapsing helper sources into canonical db source + `query` when this would reduce duplicated data, stale helper files, or extra workflow steps.
6. **Evaluation and variables audit**:
   - Check `evaluation/evaluation_plan.json` covers the objective and success criteria with measurable rubrics, and that eval step IDs do not collide with execution step IDs.
   - Check `variables/variables.json` contains user-specific values that should not be hardcoded in descriptions, scripts, KB, db rows, or reports.
7. For every artifact finding, route ownership:
   - Builder owns schema contract, step descriptions, context/output wiring, report widget source changes, and `db/README.md`.
   - Optimizer owns evidence-backed hardening when real runs show a step is violating the schema/merge contract.
   - Run mode only reports findings in chat.

OUTPUT FORMAT

For each step, produce a per-step report:

```
### step-id: <name> (type: <regular|todo_task|routing|human_input|message_sequence|orphan>)
**Description summary:** <one-line>
**Lens 0 — Durable boundary fit:** <findings or "clean">
**Lens A — Description vs Skill:** <findings or "clean">
**Lens B — Hardcoded:** <findings or "clean">
**Lens C — Browser:** <findings or "n/a (no browser capability)" or "clean">
**Lens D — Validation:** <findings or "clean">
**Lens E — Orchestrator/router description:** <findings, or "n/a (not todo_task/routing/orchestration)" or "clean">
**Lens F — Orchestrator/sub-agent boundary:** <findings or "n/a" or "clean">
**Lens G — Sub-agent hardcoded:** <findings or "n/a" or "clean">
**Learning contract:** <learning objective/method/.learning_metadata/script issues, or "n/a">
**KB contract:** <KB access/contribution/topic issues, or "n/a">
**DB contract:** <db files written/read, schema/merge issues, or "n/a">
**Report/eval/variable contract:** <wiring, eval, or variable issues, or "n/a">
**Severity verdict:** CRITICAL / WARNING / INFO / clean
**Top recommendation:** <single highest-value fix>
```

Then a cross-step summary:

- **Phase 1 structural findings** (from review_plan tool): list by severity.
- **Steps with boundary/description issues** (Lens 0/A/B/C/D): per-step, which lenses fired.
- **Todo_task/routing/orchestration steps with parent/route issues** (Lens E/F/G): per-step, which lenses fired.
- **Learning findings** (Phase 4): list steps with unjustified learning, missing objective, wrong write method, missing/stale `.learning_metadata.json`, unsupported learning locks, agentic steps with leftover main.py, stale global skill content, stale main.py, or browser scripted.
- **Knowledgebase findings** (Phase 4): list missing or unjustified KB access/contribution, stale/malformed topic notes, and facts stored in the wrong place.
- **Database structure findings** (Phase 4): list by `db/<file>.json`, then by writer step. Include missing `db/README.md` entries, missing primary keys, unsafe overwrite/append behavior, asset metadata issues under `db/assets/`, report incompatibilities, and duplicate/stale rows.
- **Report/eval/variable findings** (Phase 4): list stale report wiring, missed JSONata `query` opportunities, unnecessary report helper files/flatten steps, missing eval coverage, and values that should be variables.
- **Steps that look clean across all phases.**
- **Top 5 issues to fix first** (highest-impact across all phases).

{{if eq .WorkshopMode "run"}}RUN MODE OUTPUT: do not write builder/review.html or any workspace file. Return the review in chat using the output format above. If the user wants the findings persisted, tell them to switch to Builder or Optimizer mode and rerun /review-plan.{{else}}REVIEW LOG: append a dated entry to builder/review.html (read it first if it exists, create it if it does not). Include: what was reviewed, the structural findings (Phase 1), the boundary/description findings grouped by lens (Phase 2), the orchestrator/router findings (Phase 3), the dependent artifact findings (Phase 4: learnings, KB, db, reports, variables, eval), the cross-step summary, the top-5 list, items flagged for follow-up. Mark this as REVIEW (recommend; do NOT apply). Route fixes by ownership: Builder handles structure, step descriptions, context dependencies, validation schemas, variables, basic config, db/KB/report wiring; Optimizer handles hardening, evaluation design/scoring, metric cleanup, scripted promotion, and lock decisions.{{end}}

{{if eq .WorkshopMode "run"}}**Finding IDs.** In Run mode, assign temporary ids in the response only, using `F-YYYY-MM-DD-NNN` starting at `001`; do not scan or write builder/review.html.{{else}}**Finding IDs.** Every distinct finding from any phase gets a stable id of the form `F-YYYY-MM-DD-NNN` — today's date plus a 3-digit sequence that restarts at `001` per day. Scan builder/review.html for today's highest existing sequence and continue from there; never reuse an id. Format each finding line as `- [F-YYYY-MM-DD-NNN] <severity>: <step-id, db file, or "structural"> — <finding>` so the close-out edits performed later by `/improve-*` (or by chat-driven fixes) can target the exact entry.{{end}}
