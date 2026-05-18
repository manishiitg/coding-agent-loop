# Auto-Improvement Framework

The auto-improvement framework gives Optimizer durable evidence and audit logs for improving workflows. The current model is intentionally simple: metrics are evidence, and the optimizer chooses between hardening, replanning, eval-plan improvement, metric cleanup, or no action.

## Files

- `soul/soul.md`: objective and success criteria. This is the north star.
- `builder/improve.md`: single source-of-truth entry point for improvement narrative, workflow profile, active findings/hypotheses, schedule state, archive index, recent decisions, and structured `improve-decision` fenced audit blocks for harden/replan/report/eval/metric/rule changes. Read the active sections before every improve pass.
- `builder/improve-archive/YYYY-MM.md`: monthly archive files for old detailed improve entries. These are part of the improve ledger, but agents should read only the archive files referenced by the active index, unresolved ids, or selected evidence window.
- `builder/review.md`: review findings. Read this before every improve pass as unresolved-risk memory, and close findings when fixes land. Findings do not override `soul.md` or current run evidence.
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
6. `builder/improve.md` + referenced `builder/improve-archive/*.md` + `builder/review.md`: memory and audit trail for past decisions, unresolved findings, deferred ideas, and resolution links.

## Decision Model

Then choose one bounded action:

- `harden_workflow(group_name?, focus?)`: the workflow path is basically right, but prompts, config, validation, KB, learnings, db/report wiring, eval coverage, or metric wiring need repair. Harden removes stale `learnings/{step-id}/main.py` for `code_exec` steps; only `learn_code` steps should retain reusable `main.py`.
- `replan_workflow_from_results(group_name?, focus?)`: the workflow path is not aligned with success criteria or outcome metrics. When replan keeps or converts a step to `code_exec`, it should remove stale `learnings/{step-id}/main.py` and clear `lock_code` to avoid confusing future improvement agents.
- Eval-plan improvement: evaluation coverage, scoring, structured output, validation schema, or metric-to-eval wiring is weak enough that measurement cannot be trusted.
- `propose_metric` or `retire_metric`: the metric definition is missing, stale, duplicated, unresolved, or no longer tied to the goal. Use `propose_metric` with `amend_existing:{id,reason}` to correct an existing metric definition/source under the same id; the previous definition is archived and the version increments.
- No action: evidence is weak, recent changes need more runs, or the workflow is already aligned.

Each improve pass should perform at most one primary action unless the user explicitly asks for a broader pass.

## Commands

- `/define-success`: writes the workflow profile and starter metrics.
- `/improve-workflow`: reads prior improve/review logs and current evidence, then chooses harden, replan, eval-plan improvement, metric cleanup, or no action.
- `/improve-evaluation`: improves eval coverage and rubric quality.
- `/auto-improve`: creates or updates Run-mode and Optimizer-mode schedules. The Optimizer schedule delegates each improvement pass to canonical `/improve-workflow` guidance, then performs only schedule cadence/group-scope self-tuning. Active workflows should usually be checked after every run or every two runs, not weekly, unless the workflow itself runs weekly or the user asks for a low-touch cadence.

## Audit Discipline

When a fix addresses an item in `builder/review.md` or `builder/improve.md`, append a resolved or partially-resolved marker next to the original entry. When a fix writes a structured `improve-decision` block in `builder/improve.md`, include `linked_review_finding` or `linked_improve_entry` so the audit trail can be searched from the single source-of-truth file.

Metric movement is evidence, not proof. Do not claim an improvement worked until run/eval/metric evidence supports it, and call out confounds such as small sample size, source-data drift, rubric changes, or multiple decisions in the same window.

## Improve Log Retention

`builder/improve.md` should stay readable for users and cheap for scheduled agents to load. It is the canonical entry point, not necessarily the only physical file.

Keep in `builder/improve.md`:

- `## Workflow Profile (auto-improvement framework)`
- `## Active Improvement Index`: unresolved findings, open hypotheses, current schedule/cadence state, current metric/eval gaps, and the latest run window to inspect
- `## Archive Index`: one row per archive file with date range, entry count, unresolved ids, and a one-line summary
- `## Recent Entries`: the latest 10-20 detailed entries or roughly the last 30 days, whichever is smaller and still readable

Move to `builder/improve-archive/YYYY-MM.md`:

- resolved detailed entries older than the recent window
- repeated no-action schedule fires after summarizing the pattern in the active index
- old structured `improve-decision` blocks whose ids are preserved in the archive index

Do not archive away unresolved findings, active hypotheses, current workflow-profile implications, current metric/eval gaps, or the latest decision that changed plan/eval/metrics semantics. If old detail still matters, keep a one-line active pointer such as `See builder/improve-archive/2026-05.md#dec-social-media-20260505-002`.

Recommended compaction trigger: when `builder/improve.md` exceeds roughly 800 lines, 60 KB, or contains more than 20 detailed entries. Compaction is append-preserving at the ledger level: move old detail to an archive file, leave an index row, and never rewrite the meaning of old decisions.
