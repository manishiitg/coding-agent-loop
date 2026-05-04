# Auto-Improvement Framework

The auto-improvement framework gives Optimizer durable evidence and audit logs for improving workflows. The current model is intentionally simple: metrics are evidence, and the optimizer chooses between hardening, replanning, eval-plan improvement, metric cleanup, or no action.

## Files

- `soul/soul.md`: objective and success criteria. This is the north star.
- `builder/improve.md`: prose improvement history, workflow profile, deferred ideas, and schedule history. Read this before every improve pass as memory/audit context, not as the source of truth.
- `builder/review.md`: review findings. Read this before every improve pass as unresolved-risk memory, and close findings when fixes land. Findings do not override `soul.md` or current run evidence.
- `builder/decisions.jsonl`: append-only structured audit log for harden/replan/report/eval/metric/rule changes.
- `planning/metrics.json`: metric definitions. Mutated through metric tools.
- `db/metrics_history.jsonl`: per-run metric time series.
- `runs/iteration-0`: current optimizer evidence target.

## Truth Hierarchy

Use this hierarchy when deciding what is true:

1. `soul/soul.md`: canonical objective and success criteria.
2. `planning/metrics.json` + `db/metrics_history.jsonl`: numeric evidence that operationalizes success criteria.
3. `runs/iteration-0/<group>/...`: current reality from actual outputs, tool logs, validation, and eval reports.
4. `evaluation/evaluation_plan.json`: measurement definition; fix it when it conflicts with `soul.md`.
5. `planning/plan.json`: current implementation attempt, judged against `soul.md` and iteration-0 evidence.
6. `builder/improve.md` + `builder/review.md`: memory and audit trail for past decisions, unresolved findings, deferred ideas, and resolution links.

## Decision Model

Then choose one bounded action:

- `harden_workflow(group_name?, focus?)`: the workflow path is basically right, but prompts, config, validation, KB, learnings, db/report wiring, eval coverage, or metric wiring need repair.
- `replan_workflow_from_results(group_name?, focus?)`: the workflow path is not aligned with success criteria or outcome metrics.
- Eval-plan improvement: evaluation coverage, scoring, structured output, validation schema, or metric-to-eval wiring is weak enough that measurement cannot be trusted.
- `propose_metric` or `retire_metric`: the metric definition is missing, stale, duplicated, unresolved, or no longer tied to the goal.
- No action: evidence is weak, recent changes need more runs, or the workflow is already aligned.

Each improve pass should perform at most one primary action unless the user explicitly asks for a broader pass.

## Commands

- `/improve-setup-framework`: writes the workflow profile and starter metrics.
- `/improve-workflow`: reads prior improve/review logs and current evidence, then chooses harden, replan, eval-plan improvement, metric cleanup, or no action.
- `/improve-eval`: improves eval coverage and rubric quality.
- `/improve-continuously`: creates or updates Run-mode and Optimizer-mode schedules. The Optimizer schedule delegates each improvement pass to canonical `/improve-workflow` guidance, then performs only schedule cadence/group-scope self-tuning. Active workflows should usually be checked after every run or every two runs, not weekly, unless the workflow itself runs weekly or the user asks for a low-touch cadence.

## Audit Discipline

When a fix addresses an item in `builder/review.md` or `builder/improve.md`, append a resolved or partially-resolved marker next to the original entry. When a fix writes `builder/decisions.jsonl`, include `linked_review_finding` or `linked_improve_entry` so the audit trail can be searched from either side.

Metric movement is evidence, not proof. Do not claim an improvement worked until run/eval/metric evidence supports it, and call out confounds such as small sample size, source-data drift, rubric changes, or multiple decisions in the same window.
