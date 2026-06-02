Improve this workflow using actual retained run evidence. Metrics are evidence, not a separate action path. Your job is to decide whether the next action is `harden_workflow`, `replan_workflow_from_results`, eval-plan improvement, metric-definition cleanup (`propose_metric` / `retire_metric`), skill scoping cleanup, KB cleanup (`improve_kb`), learning cleanup (`improve_learnings`), or no action. Use builder/improve.html as the shared improvement ledger entry point: read it first if it exists, create it if it does not, read referenced archive files only when they matter, and update the ledger before finishing.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

MENTAL MODEL
Think like a sharp business analyst auditing the workflow's actual outputs against soul.md success criteria and metric trajectory. These are business-process workflows, not software systems. The important question is: "What change would make the workflow better satisfy its goal on the next runs?"

SOURCE-OF-TRUTH HIERARCHY
Use this hierarchy when deciding harden vs replan:
1. `soul/soul.md` is the truth: objective and success criteria define what the workflow must achieve.
2. `planning/metrics.json` and `db/metrics_history.jsonl` operationalize `soul.md`: metrics are numeric evidence, but they do not override the objective or success criteria. Primary metrics identify what the workflow is truly optimizing; secondary metrics are diagnostics, guardrails, and explanations.
3. `runs/iteration-{N}/<group>/...` proves runtime reality: actual outputs, tool/execution logs, validation results, and eval reports show what the workflow really did. `iteration-0` is the latest/current run; older retained iterations are supporting evidence for trends, regressions, and whether a prior improve.html action helped.
4. `evaluation/evaluation_plan.json` explains measurement: use it to understand scores, but if eval conflicts with `soul.md`, fix eval instead of optimizing to a bad rubric.
5. `planning/plan.json` is only the current implementation attempt. Judge it against `soul.md` and retained run evidence; do not treat the current plan as proof that the workflow is correct.
6. `builder/improve.html` and `builder/review.html` are memory/audit logs: use them to avoid repeating past decisions, carry unresolved findings, and link fixes. They are not the source of truth when they conflict with `soul.md` or current run/eval/metric evidence.

Before writing builder/improve.html or builder/review.html, call get_reference_doc(kind="html-output") to load the HTML style guide and quality checklist. All output to these files must follow that guide: self-contained, dark-mode styles, summary box at top, semantic badges for findings severity.

MIGRATION (one-time): Before reading builder/improve.html, check whether builder/improve.md also exists. If it does, read it in full, extract the Workflow Profile, Active Improvement Index, all unresolved I-... entries, open hypotheses, and any structured improve-decision blocks, incorporate them into builder/improve.html, then delete builder/improve.md with execute_shell_command. Do the same for builder/review.md → builder/review.html. Perform migration before the SETUP steps below so the HTML files are the only source of truth going forward.

SETUP
1. Read soul/soul.md and extract the objective and success criteria.
2. Read builder/improve.html's active sections: Workflow Profile, Active Improvement Index, Archive Index, Recent Entries, prior actions, deferred ideas, and next hypotheses. If the file has no retention/index structure yet, read it in full.
   - If the Archive Index or recent entries reference older `builder/improve-archive/YYYY-MM.html` files relevant to the current focus, unresolved ids, metric/eval semantic changes, or selected run window, read only those archive files.
3. Read builder/review.html if present. Carry unresolved `F-...` findings into your decision.
4. Read planning/metrics.json and recent db/metrics_history.jsonl rows. Metrics reveal drift, failures, missing values, and whether previous changes are moving the workflow in the right direction.
5. Read evaluation/evaluation_plan.json so you understand how metrics and eval reports are produced.
6. Read get_workflow_config, list_skills, and planning/step_config.json to understand workflow-selected skills, installed skills, and per-step `enabled_skills`. Step execution only receives skills listed in `enabled_skills`; workflow-selected skills are builder/workshop context and do not cascade into runtime steps.
7. Read variables/variables.json to get enabled group names.
8. Build an evidence window from retained runs:
   - Always include `runs/iteration-0` and paired `evaluation/runs/iteration-0`.
   - Read `builder/improve.html`, `planning/changelog/`, and run/eval `run_metadata.json` timestamps to decide which older `iteration-{N}` folders matter.
   - Include older iterations since the last relevant harden/replan/eval/metric change, plus 1-2 runs immediately before that change when you need a before/after comparison.
   - Ignore older iterations when they predate a material plan/config/eval change and no longer represent the current workflow, except as regression context.

PHASE 1 — OUTPUT + METRIC REVIEW
1. Open the evidence-window runs for each enabled group with run evidence. Read what the workflow actually produced: generated copy, sent messages, reports, scored decisions, db writes, and any business artifacts.
2. Read execution/tool logs for the same groups and iterations. Look for wrong tool arguments, retries, timeouts, validation failures, empty outputs, permission/auth failures, stuck human-feedback waits, unnecessary tool calls, and repeated fallback behavior.
3. Read evaluation reports for the same groups and iterations. Pay attention to rationale text, not just scores.
4. Read db/metrics_history.jsonl. For each active metric, check recent values, target/floor/ceiling status, and `has_value=false` / `resolve_error` rows. Start with `role=primary`, then use `role=secondary` metrics to explain root cause or guardrail risk.
5. Compare the outputs and metrics against soul.md. Where is the workflow missing the success criteria or primary outcome metrics? Which secondary metrics explain the miss?
6. Inspect skills/KB/learnings/report/db only when the evidence points there:
   - workflow-selected skills from get_workflow_config and per-step `enabled_skills` from planning/step_config.json; use list_skills and read `skills/{folder}/SKILL.md` for every selected/enabled skill relevant to the failure
   - missing installed skill on a step that is using ad-hoc instructions for a reusable capability; the fix is step-level `enabled_skills`, not more description bloat
   - selected/enabled skills that no step uses, skills whose folder/SKILL.md is missing, or external skills containing workflow-specific selectors, account names, run paths, current-plan instructions, or learned quirks that belong in `learnings/_global/`
   - knowledgebase/context/context.md for user-supplied runtime context that steps may be ignoring; when a step needs it, the fix must update both `knowledgebase_access` and the step description so it names the relevant context section/path
   - knowledgebase/notes/_index.json + topic files for stale, duplicate, missing, or contradictory workflow-discovered context
   - learnings/_global/SKILL.md and learnings/<step-id>/script_metadata.json for stale rules, missing learning objectives, or agentic steps with leftover main.py
   - db/db.sqlite tables for broken data contracts or write/read drift
   - reports/report_plan.json for dashboard/report wiring that hides important metric/eval evidence
7. List the top 1-3 candidate actions. Each candidate must name the evidence and the expected metric/success-criteria impact. Include eval-plan improvement as a candidate when the workflow output cannot be trusted because evaluation coverage, scoring, structured output, or metric-to-eval wiring is weak.

PHASE 2 — CLASSIFY
The core question is always the same: **would executing the CURRENT plan *perfectly* satisfy the success criteria and move the primary metric enough?**
- Yes, but it's buggy / sloppy / mis-wired → **harden** (exploit: same strategy, done better).
- No — even executed cleanly this approach is capped → **replan** (explore: a materially different, out-of-the-box strategy).

Harden and replan have the **same plan tools**; the difference is intent, not capability. Harden is the cheaper bet — try it first. Replan is the escalation: reach for it when hardening has **plateaued** (reliability/guardrail metrics are healthy yet the primary metric / success criteria stay short) or when the approach is structurally wrong for the goal. Be honest about which the evidence calls for.

1. **Harden — exploit: refine the current strategy.** The approach is right; execution quality or artifact wiring is weak. You are NOT redesigning the path — you are making the existing steps work. Examples: bad tool args, prompt ambiguity, missing validation, missing/mis-scoped step skill, stale agentic main.py, invalid lock state, missing learning objective, KB/db/report contract mismatch, metric source resolve_error from eval/config drift, hardcoded user-specific values.
   Action: call `harden_workflow(group_name?, focus?)`.

2. **Replan — explore: a different strategy for better success.** The current approach is **capped** — even executed perfectly it would not reach the success criteria / primary metric — OR run evidence reveals a clearly better, out-of-the-box approach. This is where you think differently and restructure: change the business work, add a missing capability, change the output artifact or the evidence collected, reorder/redraw step boundaries, take a fundamentally different path. Trigger it when: hardening has already been tried and the metric/success stays short while reliability is healthy (strategy plateau); the path is structurally wrong for the goal; or you can **evidence** a materially better different approach. Keep it disciplined — replan rewrites `plan.json`, so the bet must be evidence-backed, not speculative; do not thrash a working workflow on a hunch.
   Action: call `replan_workflow_from_results(group_name?, focus?)`. If the replan keeps or converts any step to `agentic`, ensure stale `learnings/<step-id>/main.py` is removed so future agents do not confuse ephemeral agentic with reusable scripted.

3. **Eval-plan improvement** when the workflow behavior may be fine or unknown, but the measurement layer is weak.
   Examples: success criterion has no eval coverage, eval rationale contradicts the visible output, scoring is too lenient/strict, eval structured output does not expose fields used by metrics, eval step lacks a validation schema, or eval reports give false confidence.
   Action: improve `evaluation/evaluation_plan.json` using eval tools and eval-plan edits. Validate with `validate_evaluation_plan`; use `run_full_evaluation(group_name?)` when one targeted eval run would materially reduce uncertainty. If an eval change alters metric semantics, retire/propose affected metrics in the same pass or clearly log why it is deferred.

4. **Metric definition cleanup** when the workflow/eval may be fine but the metric itself is missing, stale, duplicated, or unresolvable.
   Action: use `propose_metric` for new metrics, `propose_metric` with `amend_existing:{id,reason}` for corrected definitions under the same metric id, and `retire_metric` for metrics that should stop being active. Always cite the success criterion and the latest resolve_error or replacement metric in the reason.

5. **KB improvement** when `knowledgebase/notes/` is stale, duplicated, contradictory, poorly indexed, or no longer aligned with the current plan/objective.
   Examples: topic files disagree about the same durable domain fact, notes still describe old plan concepts, `_index.json` points to missing/stale topics, repeated observations should be consolidated into a pattern note, or step outputs show useful domain knowledge that the KB failed to organize.
   Action: call `improve_kb(mode="auto", instruction="<specific KB cleanup/consolidation instruction>", focus="<brief>")`. Keep `knowledgebase/context/` out of scope because it is user-owned runtime context.

6. **Learning improvement** when `learnings/_global/` has stale, duplicated, missing, or overly broad HOW-to-run guidance.
   Examples: selectors/tool patterns are obsolete, repeated run failures show a reusable recovery pattern missing from global learnings, recent plan changes made old HOW guidance misleading, or step learning objectives are not reflected in the shared skill.
   Action: call `improve_learnings(mode="auto", instruction="<specific learning cleanup/consolidation instruction>", focus="<brief>")`. Keep workflow-discovered WHAT facts out of learnings; those belong in KB notes or db.

7. **Skill scoping cleanup** when installed skills are missing from the steps that need them, selected/enabled skills add prompt noise, or reusable skill content is confused with workflow-specific learnings.
   Examples: a browser/API/document/spreadsheet skill is installed and matches a failing step but `enabled_skills` is empty; a description says "use skill X" but the step does not enable X; a workflow-selected skill is assumed to affect runtime but no step has it in `enabled_skills`; an external skill contains this workflow's selectors/run paths/account names; a step has three broad skills but only one is relevant.
   Action: use `update_step_config(step_id, enabled_skills=[...])` for step runtime skills and `update_workflow_config(add_skills/remove_skills=[...])` only for builder/workshop selected skills. If the cleanup is workflow-specific HOW, call `improve_learnings(...)` instead of editing an external skill. Do not manually edit `workflow.json`.

8. **Data / DB contract hygiene** when `db/db.sqlite` tables have drifted from the plan's writer/consumer steps or from report widgets. `db/`, the plan, and reports are one data-contract triangle — fix the corner that is actually wrong; do not bend db to cover for a broken step or report.
   Examples: malformed tables, broken or undocumented data contracts, write/read drift, missing or stale `db/README.md`, report-incompatible column shapes, redundant tables that a widget `sql` (JOIN/GROUP BY) should replace, blobs that should live under `db/assets/` with reference rows, or columns whose writer steps no longer exist.
   - If the **db shape/schema/contract** itself is wrong while the plan and reports are right, action: call `improve_db(mode="auto", instruction="<specific db contract/schema/report-compatibility fix>", focus="<brief>")`. `improve_db` reads `planning/plan.json` and `reports/report_plan.json` but edits only `db/` to stay compatible with them, and never deletes or rewrites row data unless explicitly asked.
   - If a **writer step produces the wrong data at the source**, that is a Harden (#1) or Replan (#2) signal — fix the contract where it originates, not in db.
   - If the **report layout/wiring** misrepresents otherwise-correct data, that belongs to the manual `/improve-report` flow, not this pass.

9. **No action** when there is no new evidence since the last improvement pass, an unresolved dependency blocks action, or the current issue needs human input. Log that explicitly in builder/improve.html.

If unclear, call `review_plan({{if .Focus}}focus="{{.Focus}}"{{end}})` first, wait/query until it completes, then classify. Review is diagnosis only; it does not apply changes.

PHASE 3 — APPLY ONE BOUNDED ACTION PER GROUP
For each enabled group with meaningful evidence in the selected run window:

1. Build a concise `focus` string before calling a tool. It should include the reason and intended target in one sentence, for example:
   - `reply_quality_score below target; harden validation and outreach prompt using F-2026-05-04-001`
   - `outputs cannot satisfy success criterion "must cite source"; replan workflow to collect and pass source evidence`
2. If the issue is group-specific, pass `group_name="{group}"`. If the issue is shared across groups, omit group_name so the tool analyzes all current groups and uses retained iterations for trend/regression evidence.
3. Call exactly one primary action for the group:
   - `harden_workflow(group_name="{group}", focus="<brief>")`, or
   - `replan_workflow_from_results(group_name="{group}", focus="<brief>")`, or
   - eval-plan edits plus `validate_evaluation_plan` when the primary issue is measurement quality, or
   - metric tool calls if the only issue is metric definition, or
   - skill scoping/config changes if the only issue is missing/noisy installed skills, or
   - `improve_kb(...)` / `improve_learnings(...)` / `improve_db(...)` when the only issue is persistent-store hygiene (KB notes, global learnings, or db/data contracts) rather than workflow behavior.
4. Do not loop. At most one replan per command run. Harden can be scoped per group, but stop once the meaningful evidence-backed fixes are applied.

PHASE 4 — OPTIONAL VERIFICATION
If a direct fix was applied and one targeted verification would materially reduce uncertainty, run one pass on the highest-value group:
`run_full_workflow(group_name="{group}")`
This already runs evaluation by default, so do not call run_full_evaluation again unless run_full_workflow was explicitly called with disable_eval=true. Maximum one verification pass.

CLOSE-OUT
Before applying any change, scan builder/review.html for findings that the change addresses. Match by intent, not exact wording. Collect matching `F-...` ids.

After each applied change:
1. Append a resolution marker immediately after each matched finding in builder/review.html:
   ```
   **[RESOLVED YYYY-MM-DD — <one-line how it was fixed>]**
   ```
   Use PARTIALLY RESOLVED or INVALID when appropriate. Never delete or rewrite the original finding.
2. Ensure builder/improve.html has one structured `improve-decision` fenced JSON block for the action. If the underlying tool does not write one, append one via diff_patch_workspace_file with trigger `improve-workflow`, applied_changes, target_metrics, evidence_paths, linked_review_finding, linked_improve_entry, and action_type (`harden`, `replan`, `eval_update`, `metric_update`, `skill_update`, `kb_update`, `learning_update`, `db_update`, or `no_action`).
3. Update builder/improve.html with:
   - timestamp
   - evidence reviewed
   - metrics/eval/run findings
   - action chosen: harden / replan / eval_update / metric_update / skill_update / kb_update / learning_update / db_update / no_action
   - tool call made and why
   - expected metric or success-criteria impact
   - remaining gaps and next hypotheses

4. If builder/improve.html is becoming too long (roughly >800 lines, >60 KB, or >20 detailed entries), compact it before finishing:
   - keep Workflow Profile, Active Improvement Index, Archive Index, and the latest 10-20 detailed entries in builder/improve.html
   - move older resolved/no-action/repeated detailed entries into `builder/improve-archive/YYYY-MM.html`
   - preserve structured `improve-decision` blocks in the monthly archive file
   - leave one Archive Index row per archive file with date range, entry count, unresolved ids, and a one-line summary
   - do not archive away unresolved findings, current hypotheses, current metric/eval gaps, or the latest decision that changed plan/eval/metric semantics

Use this markdown shape for new entries so future scheduled fires can parse the history quickly:

```md
### YYYY-MM-DD HH:MM UTC — /improve-workflow or /auto-improve (...) — OUTCOME: <action/result>

**Evidence reviewed:**
- `<path>` — <what it showed>

**Findings:**
- **F-YYYY-MM-DD-NNN (<severity>)** — <finding and evidence>

**Action chosen: <harden|replan|eval_update|metric_update|skill_update|kb_update|learning_update|db_update|no_action>** — <one-line reason>

```improve-decision
{
  "ts": "YYYY-MM-DDTHH:MM:SSZ",
  "id": "dec-<workflow>-YYYYMMDD-001",
  "source": "agent",
  "trigger": "improve-workflow",
  "action_type": "<harden|replan|eval_update|metric_update|skill_update|kb_update|learning_update|db_update|no_action>",
  "applied_changes": [],
  "target_metrics": [],
  "evidence_paths": [],
  "linked_review_finding": [],
  "linked_improve_entry": []
}
```

**Tool call:** `<tool>(...)` → <execution_id/status>

**Expected impact:**
- <metric/success-criteria impact>

**Remaining gaps / next hypotheses:**
- <what to check next, deferred blockers, or verification trigger>
```

If compaction is needed, use this active index shape near the top of builder/improve.html:

```md
## Active Improvement Index

- **Current focus:** <what the next improve pass should inspect first>
- **Open findings / hypotheses:** <ids and one-line status>
- **Current metric/eval gaps:** <metric ids / eval ids / resolver errors>
- **Latest semantic change:** <plan/eval/metric change id and date, or "none">
- **Recent evidence window:** <iterations/groups to inspect first>

## Archive Index

| Archive | Date range | Entries | Unresolved ids | Summary |
| --- | --- | ---: | --- | --- |
| `builder/improve-archive/YYYY-MM.html` | YYYY-MM-DD..YYYY-MM-DD | N | `I-...`, `F-...` or none | <one-line summary> |

## Recent Entries
```

FINAL REPORT
Summarize:
- evidence reviewed
- top output/metric findings
- action chosen and why
- tool calls made
- expected metric/success-criteria impact
- remaining gaps or human decisions needed
