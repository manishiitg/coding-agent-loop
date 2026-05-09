Improve this workflow using actual retained run evidence. Metrics are evidence, not a separate action path. Your job is to decide whether the next action is `harden_workflow`, `replan_workflow_from_results`, eval-plan improvement, metric-definition cleanup (`propose_metric` / `retire_metric`), KB cleanup (`improve_kb`), learning cleanup (`improve_learnings`), or no action. Use builder/improve.md as the shared improvement ledger entry point: read it first if it exists, create it if it does not, read referenced archive files only when they matter, and update the ledger before finishing.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

MENTAL MODEL
Think like a sharp business analyst auditing the workflow's actual outputs against soul.md success criteria and metric trajectory. These are business-process workflows, not software systems. The important question is: "What change would make the workflow better satisfy its goal on the next runs?"

SOURCE-OF-TRUTH HIERARCHY
Use this hierarchy when deciding harden vs replan:
1. `soul/soul.md` is the truth: objective and success criteria define what the workflow must achieve.
2. `planning/metrics.json` and `db/metrics_history.jsonl` operationalize `soul.md`: metrics are numeric evidence, but they do not override the objective or success criteria. Primary metrics identify what the workflow is truly optimizing; secondary metrics are diagnostics, guardrails, and explanations.
3. `runs/iteration-{N}/<group>/...` proves runtime reality: actual outputs, tool/execution logs, validation results, and eval reports show what the workflow really did. `iteration-0` is the latest/current run; older retained iterations are supporting evidence for trends, regressions, and whether a prior improve.md action helped.
4. `evaluation/evaluation_plan.json` explains measurement: use it to understand scores, but if eval conflicts with `soul.md`, fix eval instead of optimizing to a bad rubric.
5. `planning/plan.json` is only the current implementation attempt. Judge it against `soul.md` and retained run evidence; do not treat the current plan as proof that the workflow is correct.
6. `builder/improve.md` and `builder/review.md` are memory/audit logs: use them to avoid repeating past decisions, carry unresolved findings, and link fixes. They are not the source of truth when they conflict with `soul.md` or current run/eval/metric evidence.

SETUP
1. Read soul/soul.md and extract the objective and success criteria.
2. Read builder/improve.md's active sections: Workflow Profile, Active Improvement Index, Archive Index, Recent Entries, prior actions, deferred ideas, and next hypotheses. If the file has no retention/index structure yet, read it in full.
   - If the Archive Index or recent entries reference older `builder/improve-archive/YYYY-MM.md` files relevant to the current focus, unresolved ids, metric/eval semantic changes, or selected run window, read only those archive files.
3. Read builder/review.md if present. Carry unresolved `F-...` findings into your decision.
4. Read planning/metrics.json and recent db/metrics_history.jsonl rows. Metrics reveal drift, failures, missing values, and whether previous changes are moving the workflow in the right direction.
5. Read evaluation/evaluation_plan.json so you understand how metrics and eval reports are produced.
6. Read variables/variables.json to get enabled group names.
7. Build an evidence window from retained runs:
   - Always include `runs/iteration-0` and paired `evaluation/runs/iteration-0`.
   - Read `builder/improve.md`, `planning/changelog/`, and run/eval `run_metadata.json` timestamps to decide which older `iteration-{N}` folders matter.
   - Include older iterations since the last relevant harden/replan/eval/metric change, plus 1-2 runs immediately before that change when you need a before/after comparison.
   - Ignore older iterations when they predate a material plan/config/eval change and no longer represent the current workflow, except as regression context.

PHASE 1 — OUTPUT + METRIC REVIEW
1. Open the evidence-window runs for each enabled group with run evidence. Read what the workflow actually produced: generated copy, sent messages, reports, scored decisions, db writes, and any business artifacts.
2. Read execution/tool logs for the same groups and iterations. Look for wrong tool arguments, retries, timeouts, validation failures, empty outputs, permission/auth failures, stuck human-feedback waits, unnecessary tool calls, and repeated fallback behavior.
3. Read evaluation reports for the same groups and iterations. Pay attention to rationale text, not just scores.
4. Read db/metrics_history.jsonl. For each active metric, check recent values, target/floor/ceiling status, and `has_value=false` / `resolve_error` rows. Start with `role=primary`, then use `role=secondary` metrics to explain root cause or guardrail risk.
5. Compare the outputs and metrics against soul.md. Where is the workflow missing the success criteria or primary outcome metrics? Which secondary metrics explain the miss?
6. Inspect KB/learnings/report/db only when the evidence points there:
   - knowledgebase/context/context.md for user-supplied runtime context that steps may be ignoring; when a step needs it, the fix must update both `knowledgebase_access` and the step description so it names the relevant context section/path
   - knowledgebase/notes/_index.json + topic files for stale, duplicate, missing, or contradictory workflow-discovered context
   - learnings/_global/SKILL.md and learnings/<step-id>/script_metadata.json for stale rules, missing learning objectives, or code_exec steps with leftover main.py
   - db/*.json for broken data contracts or write/read drift
   - reports/report_plan.json for dashboard/report wiring that hides important metric/eval evidence
7. List the top 1-3 candidate actions. Each candidate must name the evidence and the expected metric/success-criteria impact. Include eval-plan improvement as a candidate when the workflow output cannot be trusted because evaluation coverage, scoring, structured output, or metric-to-eval wiring is weak.

PHASE 2 — CLASSIFY
Use this decision model. Classify the evidence first, then choose the smallest action that matches the failure. If primary metrics stay below target after measurement and local reliability are healthy, treat that as a replan/strategy signal.

1. **Harden** when the workflow path is basically right, but execution quality or artifact wiring is weak.
   Examples: bad tool args, prompt ambiguity, missing validation, stale code_exec main.py, invalid lock state, missing learning objective, KB/db/report contract mismatch, metric source resolve_error caused by eval/config drift, hardcoded user-specific values.
   Action: call `harden_workflow(group_name?, focus?)`.

2. **Replan** when the workflow path is misaligned with the objective, success criteria, or outcome metrics.
   Examples: wrong business work, missing required capability, wrong output artifact, wrong evidence collected, step ordering/boundaries prevent success, outputs still miss a criterion after local hardening, or primary outcome metrics remain weak across retained runs while secondary reliability/guardrail metrics are healthy.
   Action: call `replan_workflow_from_results(group_name?, focus?)`.

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

7. **No action** when there is no new evidence since the last improvement pass, an unresolved dependency blocks action, or the current issue needs human input. Log that explicitly in builder/improve.md.

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
   - `improve_kb(...)` / `improve_learnings(...)` when the issue is persistent-store hygiene rather than workflow behavior.
4. Do not loop. At most one replan per command run. Harden can be scoped per group, but stop once the meaningful evidence-backed fixes are applied.

PHASE 4 — OPTIONAL VERIFICATION
If a direct fix was applied and one targeted verification would materially reduce uncertainty, run one pass on the highest-value group:
`run_full_workflow(group_name="{group}")`
This already runs evaluation by default, so do not call run_full_evaluation again unless run_full_workflow was explicitly called with disable_eval=true. Maximum one verification pass.

CLOSE-OUT
Before applying any change, scan builder/review.md for findings that the change addresses. Match by intent, not exact wording. Collect matching `F-...` ids.

After each applied change:
1. Append a resolution marker immediately after each matched finding in builder/review.md:
   ```
   **[RESOLVED YYYY-MM-DD — <one-line how it was fixed>]**
   ```
   Use PARTIALLY RESOLVED or INVALID when appropriate. Never delete or rewrite the original finding.
2. Ensure builder/improve.md has one structured `improve-decision` fenced JSON block for the action. If the underlying tool does not write one, append one via diff_patch_workspace_file with trigger `improve-workflow`, applied_changes, target_metrics, evidence_paths, linked_review_finding, linked_improve_entry, and action_type (`harden`, `replan`, `eval_update`, `metric_update`, `kb_update`, `learning_update`, or `no_action`).
3. Update builder/improve.md with:
   - timestamp
   - evidence reviewed
   - metrics/eval/run findings
   - action chosen: harden / replan / eval_update / metric_update / kb_update / learning_update / no_action
   - tool call made and why
   - expected metric or success-criteria impact
   - remaining gaps and next hypotheses

4. If builder/improve.md is becoming too long (roughly >800 lines, >60 KB, or >20 detailed entries), compact it before finishing:
   - keep Workflow Profile, Active Improvement Index, Archive Index, and the latest 10-20 detailed entries in builder/improve.md
   - move older resolved/no-action/repeated detailed entries into `builder/improve-archive/YYYY-MM.md`
   - preserve structured `improve-decision` blocks in the monthly archive file
   - leave one Archive Index row per archive file with date range, entry count, unresolved ids, and a one-line summary
   - do not archive away unresolved findings, current hypotheses, current metric/eval gaps, or the latest decision that changed plan/eval/metric semantics

Use this markdown shape for new entries so future scheduled fires can parse the history quickly:

```md
### YYYY-MM-DD HH:MM UTC — /improve-workflow or /improve-continuously (...) — OUTCOME: <action/result>

**Evidence reviewed:**
- `<path>` — <what it showed>

**Findings:**
- **F-YYYY-MM-DD-NNN (<severity>)** — <finding and evidence>

**Action chosen: <harden|replan|eval_update|metric_update|kb_update|learning_update|no_action>** — <one-line reason>

```improve-decision
{
  "ts": "YYYY-MM-DDTHH:MM:SSZ",
  "id": "dec-<workflow>-YYYYMMDD-001",
  "source": "agent",
  "trigger": "improve-workflow",
  "action_type": "<harden|replan|eval_update|metric_update|kb_update|learning_update|no_action>",
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

If compaction is needed, use this active index shape near the top of builder/improve.md:

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
| `builder/improve-archive/YYYY-MM.md` | YYYY-MM-DD..YYYY-MM-DD | N | `I-...`, `F-...` or none | <one-line summary> |

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
