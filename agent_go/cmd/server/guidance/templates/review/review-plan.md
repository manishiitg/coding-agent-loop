Critical audit of the workflow design — the comprehensive review. Where /design-flow asks "what would a designer make better," this asks "what's wrong, weak, risky, stale, or unjustified, and which steps or artifacts need attention." Review the plan plus the dependent artifacts that make it executable: step config, skills, learnings, saved scripts, KB notes, db JSON files, report wiring, variables, and evaluation coverage.{{if eq .WorkshopMode "run"}} In Run mode, return findings in chat only; do not write files.{{else}} Findings go to builder/review.html as recommendations; nothing is applied here.{{end}}{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Write findings to `builder/review.html` — it is **regenerated (not appended) each run**, so read the existing file first and carry forward unresolved findings. For the log format/badges, the one-time `.md → .html` migration, and the `F-…` finding-id scheme, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

The audit has four phases. Run each in order. Skip Phase 3's orchestrator/router block when the workflow has no todo_task, routing, or orchestration steps. Do not skip Phase 4; if an artifact surface is absent, say whether that absence is acceptable or a finding.

PHASE 1 — STRUCTURAL ANALYSIS

1. Call review_plan() — the server-side review tool. It analyzes plan structure: step boundaries, step types, execution modes, context flow integrity, validation coverage, portability, and whether choices are justified by the objective + success_criteria from soul.md.
2. review_plan runs in the background and returns an execution_id. Capture that id and wait for completion before continuing. Use query_step(execution_id) to inspect status/results when needed; do not start Phase 2 or write the review log until Phase 1 has completed and you have the review_plan output.
3. Read its output carefully. Group findings by severity: CRITICAL (broken structure, missing required fields, contradictions vs soul.md), WARNING (questionable choices that need defense), INFO (style/minor).
4. Compare against soul.md's objective + success_criteria explicitly: for each weak structural choice, name which criterion it fails or under-serves.

PHASE 2 — PER-STEP DESCRIPTION AND SKILL-FIT AUDIT

For every executable step in plan.json, read the description. This includes top-level steps, todo_task routes, routing routes, and referenced orphan_steps. Read learnings/_global/SKILL.md once as the shared HOW-to-run source. Use get_workflow_config and list_skills to inspect workflow-selected skills and installed skills; read the SKILL.md for every skill selected at workflow level or enabled per step. For steps with learning writes or locked learning, inspect learnings/{step-id}/.learning_metadata.json when present. For scripted steps, inspect learnings/{step-id}/main.py when present. For agentic steps, verify learnings/{step-id}/main.py does not exist; if it does, flag it for deletion as a stale script. Apply each lens; skip a lens when it doesn't fire.

LENS 0 — Durable Boundary Fit
- **Do not flag size alone**: modern agents can handle long context and many tool calls. A step is not wrong just because it performs many screen actions, file reads, API calls, or small transformations.
- **Over-merged boundary**: flag when one step owns unrelated durable outputs, validation gates, retry/failure domains, tool/security contexts, downstream contracts, persistent stores, human approvals, or routing decisions.
- **Over-split boundary**: flag when adjacent steps share the same objective/output contract, fail and retry together, use the same tools/security context, produce only pass-through artifacts, depend on each other's transient reasoning, or exist only to hand context to the next step. Recommend `message_sequence` when several ordered turns should share one conversation and one durable output/validation boundary; recommend one stronger regular step only when a single turn is enough.
- **Boundary truth**: split for durable contracts; combine for scratch intermediates and coherent agentic work.

LENS A — Description vs Skill Confusion
- **Description contains runtime learnings**: the description should be an *instruction* (what to do), not a *retrospective* (what worked last time). "Use batch mode because single inserts timeout", "avoid X which caused failures", or specific tool parameter values discovered at runtime belong in SKILL.md, not the description.
- **Skill contains task instructions**: SKILL.md should capture *reusable patterns and pitfalls discovered during execution*, not restate what the step is supposed to do. If the skill reads like a task description, it's confused.
- **Duplication**: same guidance appearing in both description and skill — pick one home.
- **Description defers to skill**: phrases like "follow the skill" or "see learnings" instead of giving clear instructions.

LENS A2 — Installed / Selected Skill Fit
- **Needed skill missing**: if an installed skill clearly matches a step's task (for example browser/site skill, API integration skill, document/spreadsheet/media/domain skill) but the step has no matching enabled_skills and the description relies on ad-hoc instructions, flag it. Recommendation should name the skill folder and whether to enable it per step.
- **Enabled skill unused or noisy**: if a workflow-selected or step-enabled skill has no clear step consumer, creates broad irrelevant builder/runtime context, or overlaps another skill, flag it for removal or per-step scoping.
- **Skill referenced but not enabled**: if a description, learnings, KB note, or script says to use a skill that is not enabled in that step's `enabled_skills`, flag CRITICAL because the execution agent will not receive it.
- **Skill folder missing or malformed**: if a selected/enabled skill has no `skills/{folder}/SKILL.md`, has no clear description, or is not discoverable via `list_skills`, flag CRITICAL.
- **Workflow-level vs step-level mismatch**: workflow-selected skills are builder/workshop context and do not cascade into runtime step agents. If a selected skill is intended to affect step execution, each relevant step must list it in `enabled_skills`; otherwise flag the mismatch and recommend explicit per-step `enabled_skills` or moving shared workflow-specific HOW to `learnings/_global/`.
- **Skill vs learnings ownership**: external reusable capability docs belong in `skills/{folder}/SKILL.md`; workflow-specific discovered HOW belongs in `learnings/_global/`. Flag duplicate or misplaced content in either direction.

LENS A3 — Stale / Accumulated Description Bloat
- **Dated or transient notes**: descriptions accumulate situational content over iterations — dated verification stamps (`VERIFIED 2026-...`, `as of <date>`), `until X lands do Y` caveats, `currently the only ...` observations, single-run debugging scaffolding. Flag content tied to a past moment that has likely gone stale and recommend trimming to the durable instruction.
- **Obsolete workarounds**: instructions that work around a bug or limitation whose premise no longer holds (e.g. "tool X times out so do Y", "the db can't be opened directly so use the CLI"). Flag when the workaround is no longer needed and recommend removing it.
- **Redundant accumulation**: the same guidance restated multiple times from successive edits, or long historical preamble that buries the actual task. Recommend collapsing to one clear current instruction.
- **Boundary**: reusable run-discovered HOW belongs in `learnings/_global/SKILL.md`, not piled into the description; transient/dated context belongs nowhere — recommend deletion. The description should read as the current, minimal task contract, not a changelog of past test runs.

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

For every `todo_task` or `orchestration` step, read its description and ALL route/sub-agent descriptions (including referenced orphan_steps) and apply Lens E/F/G. **Routing steps are deterministic — they have NO description and run no agent — audit them with Lens R, not Lens E/F.**

LENS E — Orchestrator Description Quality (todo_task / orchestration only)
- **Missing objective/intent**: the parent description must clearly state WHAT we are trying to achieve — the overall goal. Without this, the orchestrator can't make intelligent decisions when things go wrong or unexpected situations arise. A good parent description answers: "Why do these routes/sub-agents exist together? What outcome are we after?"
- **Reduced to a sequencer**: if the description is just "run route A, then route B, then route C" or a fixed checklist, the parent may be over-engineered. If all it does is follow a fixed order, use regular steps when each task has a durable boundary, or `message_sequence` when the ordered turns share context and one output/validation boundary.
- **No edge case / failure guidance**: the description should explain how to handle failures, retries, partial results, missing data, or unexpected sub-agent states.
- **No dispatch criteria**: the description doesn't explain WHEN or WHY to pick each sub-agent/route. The orchestrator needs to know what conditions, inputs, or states map to which sub-agent.

LENS R — Routing Step Contract (routing steps only — deterministic, never run an agent)
- **Has a description or context_output**: a routing step MUST leave `description` and `context_output` empty — a non-empty description is a hard error at plan time. Flag CRITICAL; move any probe/judgment into a prior regular step.
- **No route source**: the selection must come from caller `route_selections`, a `route_source_file`, a `context_dependencies` entry named `route_selection.json` (written by a prior regular step as that step's `context_output`), or a `default_route_id`. Flag if none is present.
- **Dangling routes**: every route's `next_step_id` must exist (or be `end`); `default_route_id` must match one of the routes; `routing_question` must be non-empty (required).

LENS F — Orchestrator vs Sub-Agent Boundary
- **Inline execution logic**: detailed task instructions for a specific sub-task written inside the orchestrator description. Each distinct task should be its own route with its own description, learnings, and tools. Orchestrator dispatches; sub-agents execute.
- **Duplicates sub-agent descriptions**: orchestrator restates what sub-agents already describe. Orchestrator should focus on coordination and decision-making.
- **Sub-agent descriptions too vague**: route descriptions that are too thin because all the detail is in the orchestrator. Each sub-agent should be self-contained — a junior agent reading only its own description should know exactly what to do.

LENS G — Sub-Agent Hardcoded Values
- Same hardcoded-value checks from Lens B applied to sub-agent route descriptions (paths, run/iteration paths, credentials, IDs/URLs).

PHASE 4 — DEPENDENT ARTIFACT AUDIT

Treat plan, config, skills, learning, KB, db, reports, variables, and eval as one workflow contract. A plan review is incomplete if a step change leaves one of these surfaces stale or underspecified.

1. Read `planning/step_config.json`, `variables/variables.json`, `evaluation/evaluation_plan.json`, `reports/report_plan.json`, `builder/review.html`, `learnings/_global/SKILL.md`, `knowledgebase/context/context.md`, `knowledgebase/notes/_index.json`, `db/README.md`, and relevant files under `skills/{skill}/`, `learnings/{step-id}/`, `knowledgebase/notes/`, `db/`, and `db/assets/` when present. If a file is absent, decide whether absence is acceptable for this workflow.
2. **Skill audit**:
   - Use `get_workflow_config` for workflow-selected skills and `planning/step_config.json` for per-step `enabled_skills`. Use `list_skills` to verify selected/enabled skill folders exist.
   - For each selected/enabled skill, read `skills/{folder}/SKILL.md` and decide which step(s) need it. If no step needs it, flag it as prompt noise.
   - For each step, decide whether the best execution design needs a skill that is installed but not enabled. Flag missing skill scoping as a design issue, not just a config nit.
   - Verify descriptions do not say "use skill X" unless X is selected/enabled for the execution agent. Verify selected/enabled skills are not compensating for vague descriptions; the step must still state the task and output contract.
   - Flag skills that duplicate workflow-specific learnings or contain workflow-specific secrets, paths, run folders, account IDs, or current-plan task instructions.
   - If a workflow-level selected skill is expected to reach step execution, flag workflow-level-only selection for affected steps and recommend explicit per-step `enabled_skills`.
3. **Learning audit**:
   - Check every step with `learnings_access=read-write` has a real reason and a concrete `learning_objective`. SKILL.md is written by the step agent itself during a dedicated post-completion turn — `learning_objective` is the instruction the step agent uses to know what to extract, so it must be specific (selectors, timings, auth flows, tool-call patterns, API quirks) rather than generic ("learn from the run").
   - For every step with learning writes or `lock_learnings=true`, read `learnings/{step-id}/.learning_metadata.json` when present. Check `successful_runs`, `description_hash_runs`, and recent detection history.
   - Flag `lock_learnings=true` when there is no clear builder/user rationale in `review_notes`, the lock looks stale against the current step description, or step_config successful_runs/lock state contradicts metadata.
   - Flag any `agentic` step with `learnings/{step-id}/main.py`; agentic does not run or maintain persistent main.py, so the file is stale and should be deleted.
   - Flag learning writes for plumbing steps with no reusable HOW-to-run knowledge.
   - Flag browser-based steps using `scripted`; browser work should stay agentic/agentic unless there is an explicit user request plus a defensible 10+ run scenario-coverage exception.
   - Flag `scripted` unless all three gates are satisfied: user explicitly asked for it, the step is highly deterministic, and 10+ successful runs cover the relevant scenarios/groups with eval/metric evidence at target. Also flag it while recent metric evidence is inconclusive or while the step behavior is still changing.
   - Check `learnings/_global/SKILL.md` for stale step names, task descriptions masquerading as learnings, duplicated plan instructions, hardcoded values, and advice that contradicts current descriptions.
   - Flag bloated `SKILL.md` content: the root skill should be a lean index/overview (roughly under 80-100 lines) that links to focused files under `learnings/_global/references/`. Detailed selectors, auth/API quirks, browser timing, file-format rules, retry patterns, and step-specific HOW guidance should be moved into reference files.
   - For scripted steps, inspect `learnings/{step-id}/main.py` and `learnings/{step-id}/script_metadata.json` when present. Flag stale code, missing lock rationale, brittle hardcoded values, browser automation scripts, and code writing to the wrong store.
4. **Knowledgebase audit**:
   - Steps that produce durable narrative domain observations should declare `knowledgebase_access` plus a useful `knowledgebase_contribution`.
   - Steps that need user-provided runtime context from `knowledgebase/context/context.md` should declare `knowledgebase_access=read` or `read-write` AND their descriptions should explicitly name the relevant context section/path to read and apply. Flag either half missing: KB access without a description mention, description mention without KB read access, or reliance on chat memory instead of this file.
   - Prefer `knowledgebase_write_method=direct`; `agent` is only when the user explicitly wants a separate KB writer/reviewer.
   - Check `knowledgebase/context/context.md` contains user-supplied runtime rules/preferences/context, not workflow-discovered notes or execution recipes. It is user-owned; do not recommend optimizer rewrites except explicit user-requested cleanup.
   - Check `knowledgebase/notes/_index.json` points to coherent topic files and that topic notes contain durable WHAT-we-know facts discovered by the workflow, not execution recipes, run logs, raw rows, or user-owned runtime context that belongs in `knowledgebase/context/context.md`.
   - Flag steps that read KB without a clear need, write KB without a contribution contract, or store KB-worthy domain facts only in context outputs/db/learnings.
5. **Database audit**:
   - From `planning/plan.json`, find every step description that says it writes, saves, tracks, stores, accumulates, appends, caches, deduplicates, or reports data. Map those steps to concrete `db/db.sqlite` tables.
   - Read `db/README.md` if it exists. Then list tables (`sqlite3 db/db.sqlite ".tables"`) and sample each non-empty table.
   - Read `reports/report_plan.json` if present and map which widgets query which tables (the `db` + `sql` bindings).
   - For each table, check:
     - **Documented schema exists** in `db/README.md` with DDL, primary_key, upsert rule, indexes, writers, consumers, and report widgets that depend on it.
     - **Stable shape**: column types are consistent; no mixed unrelated record types in one table; nested data stored as JSON-text columns intentionally.
     - **Primary key discipline**: every table has a stable PRIMARY KEY (`id`, `group_name`, domain id, composite key). Without one, repeated runs duplicate or clobber rows.
     - **Upsert rule**: each writer description says use `INSERT ... ON CONFLICT(<pk>) DO UPDATE`, preserving unrelated rows/groups. Flag any DROP/recreate or delete-then-insert-whole-table risk.
     - **Writer ownership**: every table has one clear owner step or explicitly documented multi-writer rules. If multiple steps write the same table, their columns and upsert responsibilities must not conflict.
     - **Group/run separation**: group-specific rows include a `group_name` column or another scoped key when multiple variable groups can run.
     - **No volatile run paths as data model**: report widgets and downstream steps should query `db/db.sqlite`, not `runs/iteration-*` files. Stored paths may reference run artifacts only when intentionally archival and documented.
    - **Report compatibility**: the columns each widget's `sql` selects, plus aggregation/grouping keys and chart/table fields, exist in the tables.
     - **Asset discipline**: durable binary/media files live under `db/assets/`, with metadata/provenance/reference rows in a table; no blobs embedded inside rows.
     - **Data hygiene**: no duplicate primary keys, stale test rows, impossible nulls, mixed date formats, or columns that silently changed meaning across rows.
   - **Message-sequence item write access**: for every `message_sequence` step, check each item whose `message` or `output_files` writes to `db/` or `knowledgebase/`. That item MUST declare the matching `write_access` (`{"db": true}` and/or `{"knowledgebase": true}`) or an inferring `kind` (`db` / `knowledgebase` / `code`). Item writes are default-deny and folder-scoped — booleans only, a per-file `paths` list is invalid and ignored — while reads are always open, so a missing grant is easy to overlook and the write is silently blocked at runtime (the step then loops or fails late). Flag CRITICAL: name the item id and the file it tries to write.
   - For each step that writes `db/db.sqlite`, check that its description references the `db/README.md` contract and names the table, primary key, and upsert rule. If it only says "save the result" or writes to a run folder, flag it.
6. **Report audit**:
   - Check `reports/report_plan.json` data widgets query `db/db.sqlite` (and `file`/`file-list` widgets source durable `db/assets/`, KB context/notes, or `docs/`) rather than volatile run folders.
   - Check every column each widget's `sql` references exists in the tables, and each widget has a clear owner/source step.
   - Flag report widgets whose data is not produced by any step or whose table contract is undocumented.
   - Check whether widgets push joins/aggregation/sorting/limits into `sql` where appropriate, rather than over-fetching and reshaping client-side.
   - Flag redundant tables or a `step-generate-report` / "flatten data" step when the same result can be expressed as a widget `sql` (JOIN/GROUP BY) against the canonical tables.
   - For report findings, recommend collapsing helper tables into a widget `sql` query when this would reduce duplicated data, stale tables, or extra workflow steps.
7. **Evaluation and variables audit**:
   - Check `evaluation/evaluation_plan.json` covers the objective and success criteria with measurable rubrics, and that eval step IDs do not collide with execution step IDs.
   - Check `variables/variables.json` contains user-specific values that should not be hardcoded in descriptions, scripts, KB, db rows, or reports.
8. For every artifact finding, route ownership:
   - Builder owns skill selection/scoping, missing skill enablement, vague descriptions, and misplaced workflow-specific content.
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
**Lens A2 — Installed / selected skill fit:** <needed/missing/noisy skill findings or "clean">
**Lens A3 — Stale description bloat:** <dated notes / obsolete workarounds / accumulated cruft to trim, or "clean">
**Lens B — Hardcoded:** <findings or "clean">
**Lens C — Browser:** <findings or "n/a (no browser capability)" or "clean">
**Lens D — Validation:** <findings or "clean">
**Lens E — Orchestrator/router description:** <findings, or "n/a (not todo_task/routing/orchestration)" or "clean">
**Lens F — Orchestrator/sub-agent boundary:** <findings or "n/a" or "clean">
**Lens G — Sub-agent hardcoded:** <findings or "n/a" or "clean">
**Learning contract:** <learning objective/method/.learning_metadata/script issues, or "n/a">
**Skill contract:** <workflow-selected skills, enabled_skills, missing skill opportunities, or "n/a">
**KB contract:** <KB access/contribution/topic issues, or "n/a">
**DB contract:** <db files written/read, schema/merge issues, or "n/a">
**Report/eval/variable contract:** <wiring, eval, or variable issues, or "n/a">
**Severity verdict:** CRITICAL / WARNING / INFO / clean
**Top recommendation:** <single highest-value fix>
```

Then a cross-step summary:

- **Phase 1 structural findings** (from review_plan tool): list by severity.
- **Steps with boundary/description issues** (Lens 0/A/A3/B/C/D): per-step, which lenses fired. Call out stale/accumulated description bloat (Lens A3) explicitly — dated notes and obsolete workarounds to trim.
- **Skill findings** (Lens A2 / Phase 4): list missing needed skills, selected-but-unused skills, step scoping mistakes, malformed skill folders, and skill-vs-learning ownership problems.
- **Todo_task/orchestration steps (Lens E/F/G) and routing steps (Lens R)**: per-step, which lenses fired.
- **Learning findings** (Phase 4): list steps with unjustified learning, missing objective, wrong write method, missing/stale `.learning_metadata.json`, unsupported learning locks, agentic steps with leftover main.py, stale global skill content, stale main.py, or browser scripted.
- **Knowledgebase findings** (Phase 4): list missing or unjustified KB access/contribution, stale/malformed topic notes, and facts stored in the wrong place.
- **Database structure findings** (Phase 4): list by `db/db.sqlite` table, then by writer step. Include missing `db/README.md` entries, missing primary keys, unsafe overwrite/append behavior, asset metadata issues under `db/assets/`, report incompatibilities, and duplicate/stale rows.
- **Report/eval/variable findings** (Phase 4): list stale report wiring, missed `sql` join/aggregation opportunities, unnecessary report helper tables/flatten steps, missing eval coverage, and values that should be variables.
- **Steps that look clean across all phases.**
- **Top 5 issues to fix first** (highest-impact across all phases).

{{if eq .WorkshopMode "run"}}RUN MODE OUTPUT: do not write builder/review.html or any workspace file. Return the review in chat using the output format above. If the user wants the findings persisted, tell them to switch to Builder or Optimizer mode and rerun /review-plan.{{else}}REVIEW LOG: append a dated entry to builder/review.html (read it first if it exists, create it if it does not). Include: what was reviewed, the structural findings (Phase 1), the boundary/description findings grouped by lens (Phase 2), the orchestrator/router findings (Phase 3), the dependent artifact findings (Phase 4: learnings, KB, db, reports, variables, eval), the cross-step summary, the top-5 list, items flagged for follow-up. Mark this as REVIEW (recommend; do NOT apply). Route fixes by ownership: Builder handles structure, step descriptions, context dependencies, validation schemas, variables, basic config, db/KB/report wiring; Optimizer handles hardening, evaluation design/scoring, metric cleanup, scripted promotion, and lock decisions.{{end}}

**Finding IDs.** Give every finding a stable `F-…` id per `get_reference_doc(kind="review-improve-log")`{{if eq .WorkshopMode "run"}} — in Run mode assign temporary ids in the response only; do not scan or write builder/review.html{{end}}.
