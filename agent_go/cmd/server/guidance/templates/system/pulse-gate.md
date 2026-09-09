## Pulse Gate / Worklist

Use only for the scheduler's Gate stage. Gate performs a progressive evidence
scan and records which of the canonical review perspectives is due. It does
not audit deeply, launch reviewers, fix anything, write workflow artifacts,
publish, back up, notify, or render the dashboard.
Gate must not launch reviewers.

The canonical modules are:

- `technical_review`: one retained technical reviewer sequence. Engineering
  correctness, Stores Health, runtime operations, orchestration fitness,
  model/tier fitness, cost attribution, tool reliability, and execution
  efficiency are selectable focus lenses inside this module—not separate
  durable queues or separate default agents.
- `strategic_review`: one retained strategic reviewer sequence. It audits the
  current strategy and measurement system, and conditionally explores
  materially different approaches. It is never folded into Technical Review.
- `plan_drift_review`: event-triggered, not cadenced — due whenever
  `get_pulse_state(view="module")`'s `plan_drift_candidates` is non-empty
  (a canonical plan step has no `drift_review` record, or has one flagged
  `needs_review: true`). This is a plain fact, not a judgment call: the
  backend rejects the worklist if a non-empty candidate list is not marked
  due, so always record its true state rather than guessing.

Do not emit retired module names such as `workflow_review`, `llm_ops_review`,
`strategy_auditor`, or `goal_advisor`. Historical rows using those names are
migration inputs only.

## Progressive evidence scan

Read `soul/soul.md`, compact schedules from `workflow.json`,
`get_pulse_state(view="module")`, compact lifecycle/focus agendas, relevant
human inputs, and the smallest retained run summaries needed to judge whether
material evidence exists. Compare run timestamps with each module's
`last_ran_at` and `last_review_receipts` to measure evidence accumulated
since an actual review. `last_checked_at` records a Gate check, not a completed
review: repeated skips must not reset the evidence window. Do not treat every
retained folder as new. The triggering run is an entry point, not the entire
review window: include relevant accumulated Basic-mode runs across routes
since each module's last completed review. Use compact summaries first, keep
route attribution, and state which run IDs/time window support the worklist.
Do not claim another route is covered without reading its evidence.

Use returned open concerns, plan-change backlog, loop-closure state,
`deterministic_intake`, module history, and focus history as selectors. Do not inject or mechanically parse
the full plan or full review history. The later reviewer reads authoritative
files and tools selectively.

Use `get_plan_prompt_health` once when deciding whether a materially changed
plan needs Technical Review. It returns only compact deterministic description
size and long verbatim-duplication metrics; it is specifically the safe
alternative to dumping `planning/plan.json`. A triggered report is evidence
that a **plan-orchestration-integrity** review may be useful, not an automatic
finding and not authority for Gate to rewrite the workflow. The later Technical
Review must inspect the affected descriptions, schemas, and shared references
before it decides whether prompt-contract consolidation is safe.

Read fresh `CONCERNS:` markers from the relevant retained step summaries as
selectors, not automatic findings. `open_concerns` contains accepted canonical
issues, not raw step emissions; do not reactivate historical workflow-observation
rows as a queue. A concern can explain an incomplete output, failed side effect,
recovery, or goal/measurement gap. Gate uses it to judge review value; the later
reviewer decides whether it warrants a durable issue. Absence of a concern is
not proof of success: scripted steps and crashed agents may emit none.

If retention no longer covers the period since the last check, record that
coverage gap honestly. Do not treat a partial sample as complete.

Compare exact pins against `list_provider_models` and
`default_tier_models`. Provider-profile defaults auto-update, while exact pins
do not. Never infer freshness by name or silently rewrite an exact pin.

## Decide whether Technical Review is due

Technical Review is exception-driven. Ordinary healthy runs and the passage of
time do not require an engineering review. Select it for a concrete unresolved
outcome, an available repair, or new evidence that supports a bounded diagnosis
of material impact. Do not tour every technical lens as a recurring checklist.
Examples include:

- a failed or suspiciously successful production run;
- a verified runtime signal with unresolved step impact or recovery that cannot
  be established (`run_not_completed`, `completed_run_child_errors`, or
  `tool_success_with_structured_failure` are evidence leads, not automatic triggers);
- a previously fixed defect reproduced by new evidence;
- an answered technical decision that remains unapplied;
- a material plan, artifact, report/evaluation, DB, knowledgebase, or learnings
  change;
- material cost, latency, model, tool, retry, timeout, completion, schedule, or
  capacity behavior;
- a cadence-threatening run with repeated context reconstruction, large
  retained tool output, duplicated discovery/validation, or an unnecessary
  container/sequence boundary.
- a triggered compact prompt-health report: one step over 20k description
  characters, at least 30% of described steps over 5k, or at least 10k
  extractable verbatim duplicate-description characters. These are objective
  triage thresholds, not a conclusion that long work is wrong.

A plan, DB, knowledgebase, or learnings edit alone is not evidence of a defect.
Use completed builder/dependency/drift receipts when available. Outside the
mandatory checks below, name the remaining contract mismatch or material
uncertainty before scheduling another technical pass. If the root cause is an
already-linked platform defect and the workflow has no safe repair available,
retain that handoff and wait for a relevant platform change or new impact;
do not repeat the same workflow diagnosis on every tick.

The worklist reason proposes the best current technical focus. Use these stable
focus keys when applicable:

- `execution_health` — correctness, efficiency, tool/runtime reliability, and schedule recovery. For schedule-fire/capacity-wait history (did a schedule fire on time, was a run suspended for provider capacity, did recent scheduled fires error), read this workflow's own `schedule-runs.json` at the workflow root — it carries status/error/duration per fire that individual run folders don't.
- `plan_orchestration_integrity` — plan contracts, dependencies, context/handoffs, step types, scripted-vs-agentic choices, and sequence/todo orchestration fitness
- `store_integrity`
- `report_quality_truth` — report accuracy plus reporting UI, accessibility, and performance practices
- `evaluation_quality_truth` — evaluator truth, rubrics, thresholds, negative tests, and reproducibility
- `model_cost_fitness` — model/tier/reasoning/fallback choices, quality-cost fit, and cost attribution. Read this workflow's own per-run/per-step/per-item cost and token breakdown with `query_workflow_costs` (PLAT-184) — do not rely on the global Cost Analysis dashboard, which this workflow's own agents cannot reach at all.

This is agentic selection, not a Go threshold or semantic classifier. A large
run can be justified by adaptive research, browser dwell, or independent
verification. Cite exact step/item IDs and compact evidence paths, and explain
whether the evidence supports review now or needs a named future boundary.

An applied technical fix is treated as fixed unless new evidence reproduces it.
`changed_unverified` is closed, not a verification backlog. A later run, elapsed
next-check date, or lack of runtime proof alone must not select Technical Review,
reopen a fix, or generate a verification-only task. Failed application is still
active work. Strategic experiment outcome assessment remains separate.

## Deterministic-intake boundary

Treat a verified deterministic signal as a focused evidence lead, never as an
automatic finding. The collector proves only an objective fact — for example,
that a completed outer run contains an errored child call or a success-labelled
tool result with a non-zero exit code. Gate first assesses whether that fact
has unresolved step impact or useful new review value. If selected, the
Technical reviewer determines whether it needs a new durable finding.
Do not keyword-scan ordinary
output for words such as "error" and do not launch a Fixer directly from the
signal.

Runtime intake does not force Technical Review. Prioritize impact on required
outputs and workflow usefulness, not the number of failed tool calls. Small
exploratory misses, corrected arguments, or transient failures that recover
with correct outputs and negligible overhead do not warrant a review slot,
durable finding, or follow-up verification task. One severe failure can warrant
review; recurrence alone does not establish material impact.
Before choosing a reviewer,
inspect the smallest affected step summary, validation/output receipt, or tool
trace needed to answer: did the error prevent the step from doing its job?
An errored attempt followed by a successful retry or fallback with adequate
existing output evidence should normally be skipped. A `completed` status by
itself is not recovery proof: use the smallest relevant output or side-effect
receipt to look for missing, stale, partial, or contradictory results. Do not
audit every recovered call or demand new verification runs. Uncertainty merits
a focused diagnosis when it leaves a material required outcome in doubt, not
merely because a minor error's details are incomplete.
Repeated recovery can still justify review when its cost, latency, or
reliability impact is material. A recurring, already-understood error without
new impact, an available repair, or a reproduced defect must not reserve the
review slot or displace eligible Strategic Review. Record the evidence for this
judgment in the worklist reason/evidence, not a new issue per failed tool call.

The deterministic hard requirement remains for `plan_change_dependencies`:
set `technical_review.due=true` unless Plan Drift takes this pass. This means a
current-contract change with a durable `change_id` lacks a complete receipt across downstream steps,
validation, evaluation, reporting, database, and learnings/knowledge. Select
`plan_orchestration_integrity`. The failure proves missing coverage, not that
all six surfaces need edits; the reviewer must inspect each surface and record
an evidence-backed disposition. Legacy reviewed entries without `change_id`
are not reopened by this check.

The same boundary applies to `plan_drift_candidates`: a non-empty list means
`plan_drift_review.due=true` is required, not agentic judgment. It is a
different fact from `plan_change_dependencies` — the latter is about a plan
edit's blast radius never having been traced; `plan_drift_candidates` is about
a step's per-check drift record specifically never having been recorded.

When DB, knowledgebase, or learnings integrity is selected, explicitly name the
Stores Health scope in the reason. Stores Health remains a technical lens, not
a separate module.

## Decide whether Strategic Review is due

Select Strategic Review for a useful product/goal question or grounded opportunity,
not merely a free slot. New goals, user feedback, material plan changes, overlooked
needs, and revisable assumptions can warrant exploration before outcome evidence
matures. Completed evaluations and a proven strategy ceiling are not prerequisites.
Accumulated outcomes may also warrant review, for example:

- the goal metric is flat, unmeasurable, or contradicted by outcomes;
- activity and outcomes diverge—for example activity is high but useful
  outcomes are not improving;
- selection bias, feedback loops, concentration, saturation, proxy
  optimization, or observation contamination may be shaping the plan;
- an experiment or approved strategic decision has reached its assessment or
  application boundary;
- enough evidence exists to compare the current approach with materially
  different alternatives.

Optional strategic coverage labels include (not a mandatory checklist):

- `goal_measurement_validity`
- `strategy_effectiveness`
- `feedback_loops_bias`
- `concentration_saturation`
- `alternative_headroom`
- `experiment_impact`

Do not select Strategic Review merely because Technical Review is skipped.
Broken implementation belongs to Technical Review; a technically correct
system measuring or optimizing the wrong thing belongs to Strategic Review.
Missing telemetry is a coverage gap, not evidence of health or zero impact.
Never make one reviewer due merely because another reviewer was skipped.
Strategic Review combines the former Strategy Auditor and Goal Advisor into
one sequence. Select Strategic Review for business usefulness or strategic headroom,
including materially different approaches. Once selected, the advisor may explore
alternatives immediately; Gate does not prescribe its reasoning or require a separate
opportunity permission. Reuse focus labels where they fit, or record a descriptive
lowercase snake_case strategic key (maximum 64 characters). Existing categories
must not exclude a useful question. No quota of proposals or category coverage.

## Focus priority and rotation

### Evidence accumulation between reviews

For Technical and Strategic Review, first inspect `last_ran_at`, the latest
`last_review_receipts` result/reason, module review history, and the recorded `next_check_at`,
`next_check_after_run_id`, or `cooldown_runs`. Compare with the latest relevant
focus/route review when available. A recent review with unchanged evidence is
normally a reason to wait, not repeat it.

Current-pass `last_result` can be empty after a skip; use the retained receipt,
not that empty field, for the last review conclusion. A failed, blocked, or
timed-out receipt is not a completed assessment: honor pending review recovery.
If `last_review_receipts_error` is non-empty or history is missing, state that
uncertainty instead of inventing a previous clean review.

Count distinct completed, comparable workflow runs since that actual review:
same relevant route/group and materially comparable configuration. Chat turns,
tool calls, retries, unrelated routes, and repeated views of one run are not new
data points. Use execution identity/provenance rather than iteration-folder
names, which can rotate. If retention/provenance is incomplete, state the known
sample and coverage gap; never invent a complete count. Read only compact run
summaries needed to establish this sample, not every trace.

Let the evidence question determine how many additional relevant runs or which
outcome boundary is needed; do not impose a universal run-count threshold.
Several ordinary successful runs may be needed for trend assessment, whereas
one run can settle a specific verification. Explain the last review outcome,
new relevant sample, what remains unknown, and why the sample is or is not
sufficient in `reason`/`evidence`. For a skip, persist the concrete next boundary
using the existing scheduling fields. Do not keep moving an unmet boundary
forward merely because another Gate check occurred, or blindly restart a
cooldown on every pass. A boundary becoming due prompts assessment, not an
automatic full review without useful evidence.

All three reviews may be skipped in `observe` mode when no mandatory plan check
is due and neither perspective has useful new evidence. Waiting for more data
is a valid result. Do not manufacture a technical or strategic review to fill
the slot. New critical regressions, security/data-loss risks, and materially
failed required outcomes override waiting for more samples; select a focused
Technical Review promptly. Ordinary recovered errors do not override it.

For each selected module, use the compact focus agenda and reason within the
highest applicable lifecycle class:

1. new critical regression, security issue, data loss, or widespread failure;
2. a reproduced technical defect or matured strategic experiment evidence;
3. answered but unapplied decision;
4. materially changed or never-reviewed focus;
5. overdue focus;
6. oldest remaining focus.

This is not rigid round-robin and not a numeric relevance score. A lightweight
safety scan may preempt normal rotation. Map evidence to stable
route/group/sub-workflow scopes first, then let the reviewer choose the smallest
sufficient focus set. A small route often needs one focus; materially distinct
large routes may justify several. Every additional focus must have distinct
evidence, risk, decision, or repair value. Record what won, what it inspected,
and what it deferred. Repeated selection of the same unchanged focus while
eligible never-reviewed or overdue work exists requires an explicit urgent or
verification reason.

It is valid to skip Technical Review, Strategic Review, or both. Every skip
must record why no useful review is currently possible and include evidence
plus `next_check_at`, a positive `cooldown_runs`, or
`next_check_after_run_id`. Skipping is a durable decision, not an assertion
that the module is healthy forever.

## Choose the pass shape

Choose one mode and give a concrete `mode_reason`:

- `backlog_drain`: retained active issues, reproduced failures, or answered
  decisions already provide the useful work. Do not add broad discovery.
- `discovery`: materially new technical or strategic evidence may reveal a
  root cause not explained by retained work.
- `strategy`: a product/goal question warrants Strategic Review. Do not use it
  as a disguised correctness pass.
- `observe`: neither review has actionable or mature evidence. Wait for the
  named next boundary.

An old backlog must not hide a new production failure. Conversely, unchanged
backlog must not force another expensive discovery pass. A cooldown or focus
rotation cannot suppress a new materially harmful miss or a critical regression.

Select **at most one** due module per Pulse pass. `plan_drift_review` has
priority: when `plan_drift_candidates` is non-empty, record only
`plan_drift_review` as due and skip both Technical and Strategic Review for
this pass. Its due state is a plain fact, not a judgment call. When Plan Drift
is not due, choose the single stronger perspective between `technical_review`
and `strategic_review`; it is valid to skip both when no evidence warrants a
review. An interrupted-review recovery remains durable while Plan Drift runs
and is resumed on the next eligible non-drift pass. Call
`record_pulse_worklist` exactly once with the mode, mode reason, and one
decision for every canonical module: `technical_review`, `strategic_review`,
and `plan_drift_review`. On recovery, if this Pulse run already has a complete
worklist, verify it and stop rather than recording it again.

### Worklist payload boundary (required)

`record_pulse_worklist.decisions[]` is deliberately a **small scheduling
receipt**, not a review plan. Each decision may contain only `module`, `due`,
`reason`, `evidence`, `next_check_at`, `next_check_after_run_id`, and
`cooldown_runs`. Do **not** put `focuses`, `route_scope`, `issue_ids`,
`deferred_focuses`, `decision`, or any review-plan field in this call. Explain
the selected scope in `reason` and `evidence`; the later reviewer records its
actual focus coverage with `record_pulse_review_focus` after inspecting the
evidence. An unknown field rejects the whole worklist and prevents all review
and repair work from starting.

After the worklist, optionally record trustworthy comparable success-criterion
measurements with `record_pulse_impact`. Use stable criterion IDs, producing run
IDs, route/environment, exact evidence provenance, and an honest value or
qualitative status. If trustworthy evidence does not exist, record nothing;
missing evidence is not zero or healthy. Gate never creates interventions or
impact assessments.

The later retained Review+Fix task owns selected technical work,
strategic work, lifecycle updates, verification, and terminal receipts. Stop
after recording the worklist and any honest impact observations.
