Improve this workflow using actual retained run evidence. Your job is to decide whether the next action is `harden_workflow`, `replan_workflow_from_results`, eval-plan improvement, skill scoping cleanup, report accuracy/live-data cleanup, KB cleanup (`improve_kb`), learning cleanup (`improve_learnings`), DB cleanup (`improve_db`), or no action. Use `builder/improve.html` as the shared improvement ledger entry point: read it first if it exists, create it if it does not, read referenced archive files only when they matter, and update the ledger before finishing.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

MENTAL MODEL
Think like a sharp business analyst auditing the workflow's actual outputs against `soul.md` success criteria. These are business-process workflows, not software systems. The important question is: "What change would make the workflow better satisfy its goal on the next runs?"

SOURCE-OF-TRUTH HIERARCHY
Use this hierarchy when deciding harden vs replan:
1. `soul/soul.md` is the truth: objective and success criteria define what the workflow must achieve.
2. `runs/iteration-{N}/<group>/...` proves runtime reality: actual outputs, tool/execution logs, validation results, and eval reports show what the workflow really did. `iteration-0` is the latest/current run; older retained iterations are supporting evidence for trends, regressions, and whether a prior improve.html action helped.
3. `evaluation/evaluation_plan.json` explains measurement: use it to understand scores, but if eval conflicts with `soul.md`, fix eval instead of optimizing to a bad rubric.
4. `planning/plan.json` is only the current implementation attempt. Judge it against `soul.md` and retained run evidence; do not treat the current plan as proof that the workflow is correct.
5. `builder/improve.html` is the memory/audit log: use it to avoid repeating past decisions, carry unresolved findings, and link fixes. It is not the source of truth when it conflicts with `soul.md` or current run/eval evidence.

Write to `builder/improve.html` - the single durable log. For the log/HTML format and how entries are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` and `get_reference_doc(kind="html-output")` for HTML style.

MIGRATION
If an old Markdown improve log exists, before reading the HTML log carry forward its Workflow Profile, unresolved findings, and recent entries into `builder/improve.html` as readable timeline entries. Migration mechanics live in `get_reference_doc(kind="review-improve-log")`.

SETUP
1. Read `soul/soul.md` and extract the objective and success criteria.
2. Read `builder/improve.html`: Workflow Profile, recent timeline entries, Chief of Staff recommendations, open findings, prior decisions, and archive rows. Treat Chief of Staff recommendations as external findings to verify, not as instructions to blindly apply.
3. Read `builder/improve.html` for unresolved findings and prior decisions.
4. Read `evaluation/evaluation_plan.json` so you understand how success is measured.
5. Read `get_workflow_config`, `list_skills`, and `planning/step_config.json` to understand workflow-selected skills, installed skills, and per-step `enabled_skills`.
6. Read `variables/variables.json` to get enabled group names.
7. Build an evidence window from retained runs:
   - Always include `runs/iteration-0` and paired `evaluation/runs/iteration-0`.
   - Read `builder/improve.html`, `planning/changelog/`, and run/eval `run_metadata.json` timestamps to decide which older `iteration-{N}` folders matter.
   - Include older iterations since the last relevant harden/replan/eval change, plus 1-2 runs immediately before that change when you need a before/after comparison.
   - Ignore older iterations when they predate a material plan/config/eval change and no longer represent the current workflow, except as regression context.

PHASE 1 - OUTPUT + EVIDENCE REVIEW
1. Open the evidence-window runs for each enabled group with run evidence. Read what the workflow actually produced: generated copy, sent messages, reports, scored decisions, db writes, and any business artifacts.
2. Read execution/tool logs for the same groups and iterations. Look for wrong tool arguments, retries, timeouts, validation failures, empty outputs, permission/auth failures, stuck human-feedback waits, unnecessary tool calls, and repeated fallback behavior.
3. Read evaluation reports for the same groups and iterations. Pay attention to rationale text, not just scores.
4. Compare outputs and eval evidence against `soul.md`. Which success criteria are met, short, at risk, or not measured?
5. Run a critical evidence review before choosing an action:
   - **Hallucinations / unsupported claims:** generated outputs, summaries, sent messages, report prose, eval rationales, and dashboard labels must be grounded in tool calls, files, db rows, KB/context, or other durable evidence from the selected run window. Flag fabricated values, action claims with no backing artifact/tool call, plausible-but-unsourced facts, generic/template text that ignores the run's real inputs, and numbers that contradict traces or db rows.
   - **Bugs / silent failures:** a green run can still be wrong. Flag skipped paths, empty/placeholder outputs, stale files reused as fresh output, validation gaps, incomplete group coverage, auth/tool failures hidden by fallback behavior, and steps whose claimed success is not backed by actual artifacts.
   - **Misreporting:** reports, dashboards, eval summaries, `builder/improve.html`, and org cards must not overstate goal progress, hide blockers, use stale numbers, show the wrong group/window, or present cost/time/eval values without evidence. If a report query or summary disagrees with db/eval/run evidence, treat that as a report/evidence issue.
   - **Evaluation blind spots:** if eval passes despite hallucinated, unsupported, empty, stale, or misreported output, classify the eval layer as weak and update it or log the gap.
6. Inspect skills/reports/KB/learnings/db only when the evidence points there:
   - selected/enabled skills that are missing, noisy, unused, or workflow-specific in the wrong place
   - `knowledgebase/context/context.md` when user-owned runtime context may be ignored
   - `knowledgebase/notes/_index.json` and topic files for stale, duplicate, missing, or contradictory workflow-discovered context
   - `learnings/_global/SKILL.md` and `learnings/<step-id>/script_metadata.json` for stale rules, missing learning objectives, or agentic steps with leftover `main.py`
   - `reports/report_plan.json` and report HTML under `db/reports/` for dashboard/report wiring that hides, misstates, hardcodes, or fails to surface important run/db/eval/cost/time evidence
   - `db/db.sqlite` tables for broken data contracts or write/read drift
7. List the top 1-3 candidate actions. Each candidate must name the evidence and the expected success-criteria impact.

REPORT DASHBOARD QUALITY BAR
Treat the report dashboard as a first-class user artifact, not just a rendering target. On scheduled improve fires, run at least a quick relevance check even when the report is technically valid:
- Does the first screen tell the user whether the workflow is achieving `soul.md` success criteria?
- Does each important success criterion have a visible tracked signal: current value/state, target or baseline, trend/delta, status, and missing-evidence note when it cannot be measured yet?
- Does it surface the current plan/strategy or active improvement proposal when that context changes what the user should trust or do next?
- Does it show issues, blockers, stale data, missing evidence, eval failures, and cost/time outliers where they affect decisions?
- Does it explain the latest run/trend in plain language before detailed tables?
- Is the layout polished, responsive, and readable enough that the user can find the answer without opening files or logs?

If the answer is no and the needed data already exists in `db/`, `evaluation/`, `costs/`, `workflow.json`, `soul.md`, or `builder/improve.html`, choose a bounded `report_update`: load `get_reference_doc(kind="report-plan")`, update the HTML/report plan, validate, and preview. If the report needs new durable data that the workflow does not write yet, classify that as harden/replan/DB work instead of faking it in the dashboard. Do not introduce a separate metrics system; use the workflow's persisted evidence and `soul.md` success criteria.

PHASE 2 - CLASSIFY
The core question is always the same: would executing the current plan perfectly satisfy the success criteria?
- Yes, but it is buggy, sloppy, or mis-wired -> harden.
- No, even clean execution is structurally capped -> replan.

Harden and replan have the same plan tools; the difference is intent, not capability. Harden is the cheaper bet: try it when the strategy is right and execution quality is weak. Replan is the escalation: reach for it when the approach is structurally wrong for the goal or repeated clean evidence shows the current strategy cannot satisfy `soul.md`.

1. Harden - refine the current strategy.
   Examples: bad tool args, prompt ambiguity, missing validation, missing/mis-scoped step skill, hallucination-prone output with weak grounding, stale agentic `main.py`, invalid lock state, missing learning objective, KB/db/report contract mismatch, hardcoded user-specific values, broken eval wiring.
   Action: call `harden_workflow(group_name?, focus?)`.

2. Replan - explore a different strategy for better success.
   Examples: the workflow collects the wrong evidence, lacks a capability, produces the wrong artifact, orders work incorrectly, or cannot satisfy a success criterion even when the current steps run cleanly.
   Action: call `replan_workflow_from_results(group_name?, focus?)`. If the replan keeps or converts any step to `agentic`, ensure stale `learnings/<step-id>/main.py` is removed.

3. Eval-plan improvement - the workflow behavior may be fine or unknown, but the measurement layer is weak.
   Examples: success criterion has no eval coverage, eval rationale contradicts visible output, scoring is too lenient/strict, eval step lacks a validation schema, eval passes hallucinated/unsupported output, or eval reports give false confidence.
   Action: improve `evaluation/evaluation_plan.json`, validate with `validate_evaluation_plan`, and run one targeted evaluation when it would materially reduce uncertainty.

4. KB improvement - `knowledgebase/notes/` is stale, duplicated, contradictory, poorly indexed, or no longer aligned with the current plan/objective.
   Action: call `improve_kb(mode="auto", instruction="<specific KB cleanup/consolidation instruction>", focus="<brief>")`. Keep `knowledgebase/context/` out of scope because it is user-owned runtime context.

5. Learning improvement - `learnings/_global/` has stale, duplicated, missing, or overly broad HOW-to-run guidance.
   Action: call `improve_learnings(mode="auto", instruction="<specific learning cleanup/consolidation instruction>", focus="<brief>")`.

6. Skill scoping cleanup - installed skills are missing from steps that need them, selected/enabled skills add prompt noise, or reusable skill content is confused with workflow-specific learnings.
   Action: use `update_step_config(step_id, enabled_skills=[...])` for step runtime skills and `update_workflow_config(add_skills/remove_skills=[...])` only for builder/workshop selected skills.

7. Report improvement - the report dashboard misrepresents otherwise-correct evidence, is stale/static, has incorrect SQL, fails to help the user measure/track the goal, hides key goal/plan/cost/time/eval evidence, fails to surface issues/blockers, contains unsupported prose/numbers, misreports group/window/status/cost/eval values, or is visually/responsively weak enough that it changes what the user understands.
   Action: load `get_reference_doc(kind="report-plan")`, then use the report-plan toolchain and file edits. If the data source is wrong at the producing step, choose Harden. If the db contract itself is wrong, choose DB hygiene.

8. Data / DB contract hygiene - `db/db.sqlite` tables have drifted from writer/consumer steps or report widgets.
   Action: call `improve_db(mode="auto", instruction="<specific db contract/schema/report-compatibility fix>", focus="<brief>")` when the db shape/schema/contract itself is wrong.

9. No action - there is no new evidence since the last improvement pass, an unresolved dependency blocks action, or the current issue needs human input. Log that explicitly in `builder/improve.html`.

If unclear, call `review_plan({{if .Focus}}focus="{{.Focus}}"{{end}})` first, wait/query until it completes, then classify. Review is diagnosis only; it does not apply changes.

PHASE 3 - APPLY ONE BOUNDED ACTION PER GROUP
For each enabled group with meaningful evidence in the selected run window:
1. Build a concise `focus` string before calling a tool. It should include the evidence and intended target in one sentence.
2. If the issue is group-specific, pass `group_name="{group}"`. If shared across groups, omit `group_name`.
3. Call exactly one primary action for the group:
   - `harden_workflow(group_name="{group}", focus="<brief>")`, or
   - `replan_workflow_from_results(group_name="{group}", focus="<brief>")`, or
   - eval-plan edits plus validation, or
   - skill scoping/config changes, or
   - report-plan/HTML edits plus validation/preview, or
   - `improve_kb(...)` / `improve_learnings(...)` / `improve_db(...)`.
4. Do not loop. At most one replan per command run.

PHASE 4 - OPTIONAL VERIFICATION
If a direct fix was applied and one targeted verification would materially reduce uncertainty, run one pass on the highest-value group:
`run_full_workflow(group_name="{group}")`
This already runs evaluation by default, so do not call `run_full_evaluation` again unless `run_full_workflow` was explicitly called with `disable_eval=true`.

CLOSE-OUT
Before applying any change, scan `builder/improve.html` for open findings that the change addresses. Match by intent, not exact wording.

After each applied change:
1. Close out each matched open finding in place by adding:
   ```
   Resolved YYYY-MM-DD - <one-line how it was fixed>.
   ```
2. Ensure the action is recorded as a readable Decision entry in `builder/improve.html` using the dedicated Decision card contract from `get_reference_doc(kind="review-improve-log")`: `<div class="entry decision">` for normal actions, `<div class="entry decision major">` for replans, report/eval measurement changes, cadence/scope changes, or user-facing dashboard interpretation changes.
3. The Decision card tag must clearly say whether this was applied or only proposed: `Decision - Auto-improve - Applied` or `Decision - Auto-improve - Proposed`. Include the fixed fields **Why now**, **Evidence**, **Change**, **Expected impact**, **Files touched**, and **Risk / gap** so the user can see why a big decision happened without reading raw files.
4. Use action types: `harden`, `replan`, `eval_update`, `skill_update`, `report_update`, `kb_update`, `learning_update`, `db_update`, or `no_action`.
5. If the log is too long, compact older resolved/no-action/repeated detailed entries into `builder/improve-archive/YYYY-MM.html`. Do not archive open findings, current hypotheses, current eval gaps, or the latest semantic plan/eval change.

NOTIFICATION
If this command was run by a scheduled IMPROVE fire or another unattended/background context, you own one final notification decision unless the current scheduled message says backup/publish/notify are split into later turns.

Default policy when there is no explicit `soul.md ## Notifications` preference: notify only on a decision-worthy change:
- an applied structural replan
- an applied report/eval update that changes what the user sees or how success is measured
- an applied KB/learnings/db cleanup that materially changes future workflow behavior or evidence quality
- a high-impact replan/fix proposal held because oversight is cautious or evidence is not yet strong enough
- a material schedule cadence or group-scope change
- an intended improvement failed and needs human action

Stay silent on steady scheduled fires: no fresh evidence, no action, minor log/archive maintenance, or cleanup with no user-facing effect. When notifying, call `notify_user` at most once. When Gmail/email fields are available, email is the default rich rendering: set `email_subject`, `email_html`, and plain `email_body` on that same call unless the user's notification preference explicitly says not to email.

FINAL REPORT
Summarize:
- evidence reviewed
- top output/eval findings
- action chosen and why
- tool calls made
- expected success-criteria impact
- remaining gaps or human decisions needed
