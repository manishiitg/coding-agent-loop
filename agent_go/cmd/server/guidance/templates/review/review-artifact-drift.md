**Saved-code paths:** Read `workflow.json.code_layout_version` first. In this reference, `<script-dir>` means `code/<step-id>` for version 1, or `learnings/<step-id>` for absent/zero (legacy). Resolve the placeholder before using a path; never infer the version from folders or migrate an existing workflow implicitly. Version 1 executes and repairs canonical source directly, with shared helpers under `WORKFLOW_CODE_ROOT`; only legacy workflows copy code into runs and save it back.

Use this to run a manual artifact-drift audit after plan or configuration
changes — the on-demand equivalent of Pulse's scheduled pass, sharing the
exact same candidate collector, repair contract, and completion writer as
`plan_drift_review`, not a separate parallel implementation of it.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Run this full audit for scheduled Pulse or an explicit user request. Ordinary plan edits and targeted test runs use `builder-reference/references/plan-change-impact.md`: one combined compatibility check in the current agent after related edits. A stale `drift_review.needs_review` flag alone does not require this audit before testing. Merely reading this guide during an edit does not authorize launching a background audit.

## Execution model

- In Pulse, the parent may include this checklist in the normal Engineering
  background executor when artifact-drift evidence is selected.
- Outside Pulse, launch one `run_in_background` executor with this checklist as
  its instructions.
- If you are already that background reviewer, perform the audit directly. Never launch another reviewer, background tool, or nested maintenance agent.
- The call returns an `execution_id` immediately. End the current turn and wait
  for the automatic completion notification; do not poll, sleep, or repeatedly
  call `query_step`.
- **Part 1 (below) has real repair authority, identical to `plan_drift_review`'s
  own scheduled turn** — read/apply/verify/persist exactly as
  `plan-drift-review.md` describes, because you ARE running that same
  contract, just dispatched manually instead of by Pulse Gate. **Part 2 (the
  remaining checklist) stays strictly read-only**, as before: it must not edit
  files, mutate the plan/config, mark changelog entries, or mark Pulse module
  state — everything it finds becomes a recorded finding for the parent to act
  on, never a same-turn edit.

## Part 1 — run plan_drift_review's real procedure now

First call `record_pulse_module_due(pulse_run_id="current", module=
"plan_drift_review", reason="manual /review-artifact-drift invocation")`.
This manual invocation has no Gate-recorded worklist entry the way a
scheduled Pulse pass does, and `record_pulse_result`'s own write only accepts
a terminal result when the durable worklist already shows the module due for
this exact conversation's run id — without this call, the repair work below
would complete but its receipt would fail to persist, the exact
"review completed but result failed" outcome this call exists to prevent.

If `record_pulse_module_due` refuses because a real scheduled Pulse pass is
already mid-flight for `plan_drift_review` (a genuine, rare collision — it
means Gate is actively running this same module right now under a different
run), stop here: state that plainly and do not proceed into repair work whose
receipt could not be recorded anyway. Do not retry in a loop; the operator can
re-run this command once the scheduled pass finishes.

Call `get_pulse_state(view="module")`. Its `plan_drift_candidates` array is
the exact same durable candidate set the scheduled Pulse pass reads — every
step with no `drift_review` record or `needs_review == true`, plus (per
`plan_drift_candidates_note`) a possible workflow-level candidate with
`step_id == "__workflow_drift_review__"` when a step deletion since the last
audit still needs its dependent-artifact fallout traced.

If the array is non-empty, load
`read_skill(skills=[{"name":"builder-reference","path":"references/plan-drift-review.md"}])`
and follow its steps 1-6 (and its workflow-level deletion audit section, for
the `__workflow_drift_review__` candidate if present) exactly, for every
candidate — applying and verifying safe workflow-owned fixes directly,
routing only genuine human decisions or platform-owned boundaries, and
persisting via `record_plan_drift_review` and `record_pulse_result
(module="plan_drift_review", ...)`, precisely as that module's own scheduled
turn would. This is not optional evidence to defer past a stale record — it
is the same due work Pulse would otherwise run on its own schedule, done now
because the operator asked for it directly.

If the array is empty, state that plainly (nothing plan_drift_review-scoped
is currently due) and move on to Part 2 below.

## Part 2 — the read-only checklist for everything plan_drift_review does not cover

This checklist's job is what Part 1 does not cover: schedule
cron/timezone/queue drift, eval/success-criteria coverage, downstream-step
field consumption, dead step/schedule references not already resolved by
Part 1's deletion audit, and duplicate control stores. It checks whether step
config, the schedules that drive the workflow, learnings, saved code,
knowledge-base notes, database contracts, reports, evaluation, and recent run
evidence still match the current workflow, and flags missing or stale eval
coverage with an `Eval fix` owner label. Read only matching typed state
through `get_pulse_state(view="review")` with `module=technical_review`, using
the managed SQLite-backed tools and structured finding lifecycle; do not query
SQLite directly or inspect/create Pulse presentation artifacts.

For a suspected drift that recurs or a repaired dependency awaiting proof,
compare the current artifact/run evidence with up to three comparable retained
runs (same route/group and materially equivalent configuration). Read compact
receipts first; open raw traces only for the changed or suspicious step/attempt.
State an evidence limitation when fewer comparable runs remain.

Load `read_skill(skills=[{"name":"builder-reference","path":"references/assumption-audit.md"}])`. While tracing changed surfaces, identify dependent artifacts that preserved an old architecture, tactic, schema, metric, or execution assumption after the plan evolved. Keep consequential unresolved restrictions under Pulse's Assumptions challenged.

## Audit checklist

1. List `planning/changelog/changelog-*.json` in filename order and select entries where `artifact_review.done` is not true.
   - Changelog markers are the only artifact-review cursor. If a marker is absent,
     inspect the exact entry; do not rely on a separate presentation cursor.
   - If no cursor exists and more than 100 entries are unreviewed, inspect only the latest 100 and report that the older entries remain unreviewed.
   - Never advance the proposed cursor past an entry that was not fully inspected or safely cursor-backfilled.
   - **The changelog only records plan mutations** (`update_scripted_step` and
     the other typed plan-mod tools) — `planning/` is never a granted write
     path for any other tool, so `plan.json` itself is always fully and
     truthfully captured here. A direct edit to a step's own code
     (`<script-dir>/main.py`) or a shared doc (`db/README.md`) made via
     `diff_patch_workspace_file`/`update_workspace_file` is a different kind
     of change and does not produce a changelog entry, even when it happens
     in the same turn as a plan-tool call — the entry that turn produces
     describes only the plan-tool's own field change. When a changelog entry
     does flag a step as affected, step 3 below already directs inspecting
     that step's `main.py` directly, so the code change is still caught. The
     residual gap is narrower: an isolated code/doc edit with **no**
     accompanying plan-tool call in the same turn produces no changelog
     entry at all, so nothing routes that step into this checklist's
     cursor-selection step in the first place — this is a scope boundary of
     the changelog cursor, not evidence the checklist itself missed
     something once a step is in scope.
3. For each affected step, inspect only relevant current artifacts:
   - `planning/plan.json` and `planning/step_config.json`
   - **Deferral to plan_drift_review (Part 1 above)**: never re-derive
     `report_query_compatibility`, `validation_schema_db_rules`,
     `validation_schema_file_rules`, `scripted_code_db_queries`,
     `scripted_best_practices`, `message_sequence_best_practices`,
     `orchestrator_best_practices`,
     `routing_best_practices`, `branch_best_practices`,
     `db_readme_contract`, or `orphaned_tables` by hand. Part 1 already ran
     the real `plan_drift_review` procedure earlier in this same turn for
     every due candidate, applying and verifying fixes directly — read the
     step's now-current `agent_configs.drift_review` record in
     `step_config.json` as the authoritative, just-updated result. Only
     inspect one of these surfaces directly yourself when the step was not a
     Part 1 candidate at all (already clean and current) and its evidence
     still looks insufficient for the entry under review.
   - the workflow's schedules in `workflow.json` — for each schedule, its cron,
     timezone, and the `messages` queue. The queue is what the scheduler
     actually sends, so it is a first-class contract with the plan, not
     configuration noise: read every message and resolve each to the plan step
     it drives.
   - `<script-dir>/main.py`, script metadata, per-step learning metadata, and relevant `learnings/_global/` guidance — plan_drift_review's
     `scripted_code_db_queries` check only covers whether a scripted step's SQL
     still resolves against the live schema; content staleness, stale locks,
     and access-mode appropriateness stay this checklist's job (or the
     judgment pass inside plan_drift_review's own reviewer turn — do not
     duplicate a finding both places for the same step)
   - relevant `knowledgebase/notes/` content and KB access/contribution settings; treat `knowledgebase/context/` as read-only user-owned context
   - `db/README.md`, named DB tables/assets/contracts, and their writers/consumers — deferral above covers the DDL-vs-live-schema contract check;
     this checklist still owns writer/consumer disagreement across steps
   - `db/reports/index.html`, its internal views, SQL, and data contracts — deferral above covers whether each `window.report.query(...)` call still
     resolves; this checklist still owns data-contract/semantic disagreement.
     Flag a remaining `reports/report_plan.json` as an incomplete version migration
   - `evaluation/evaluation_plan.json`, `evaluation/step_config.json`, and matching goal/success-criteria coverage — deferral above covers whether an
     eval step's `PreValidation` SQL/JSONPath rules still resolve; coverage
     gaps (an orphaned or missing eval) stay this checklist's job
   - one representative recent run for changed runtime behavior when evidence exists
   - for any changed status, strategy, feature flag, guard, routing rule, or
     other control value, trace the exact changed record to the current runtime
     reader and one resulting decision/output. If similarly named tables/files
     carry the same logical IDs, compare them and identify the canonical owner
     plus the required mirror rule. A clean changelog/file diff is not enough
     when the runtime reads a different store.
4. Record a finding only when evidence shows drift, including:
   - code, paths, fields, selectors, tool/API usage, or validation still implement an old contract
   - stale code locks or stale learning access after a material change without review evidence
   - learnings or KB preserve obsolete behavior or agent-inferred policy
   - DB writers, report consumers, or eval consumers disagree on schema or semantics
   - a change updated a plausible but non-canonical store, or duplicate control
     stores disagree so the allocator/router/executor cannot observe the repair
   - report/eval checks use stale artifacts, fields, thresholds, or run identity
   - a changed success criterion lacks eval coverage, or an eval is orphaned/duplicative
   - new steps lack required dependent wiring (a deleted step's own dangling
     references are Part 1's workflow-level deletion audit's job now — report
     one here only if Part 1 found no due `__workflow_drift_review__`
     candidate at all yet a dangling reference still exists, which would
     itself be evidence the workflow-level flag was missed)
   - a schedule-local procedure that duplicates durable plan behavior, lacks
     `direct_messages_reason`, or claims canonical step-level learning,
     validation/retry, repair, or Pulse attribution that it does not receive.
     Direct schedule conversation is a supported execution model, so do not
     report it merely because it is long. Compare reuse, inputs/outputs,
     side effects, approval and failure boundaries; report the missing route
     only when those facts show the work belongs in the canonical plan.
   - a schedule message that drives no plan step, or a plan step no schedule message reaches and no other step invokes — dead work, or a queue that was
     never updated after the step was added
   - execution order or grouping that exists only in the message queue while
     `plan.json` leaves order/groups unset. The plan is then not runnable on its
     own, and the queue silently owns the sequence
   - a message queue that only restates steps the plan already orders, which is
     duplication that will drift the next time either side changes
   - schedule cron/timezone that contradicts what `soul.md` or the plan claims
     about cadence or the window the work is valid for
   - a schedule message that should invoke canonical work but bypasses
     `run_full_workflow`/`execute_step`; use `validate_plan_change` when a repair
     changes plan references or dependency wiring
5. Include clean checks briefly. Do not manufacture drift merely because an artifact exists.

## Reviewer result

This section covers Part 2's own findings only — Part 1 already persisted its
own `plan_drift_review` result (`record_plan_drift_review` per candidate,
`record_pulse_result(module="plan_drift_review", ...)` once) earlier in this
same turn; do not fold Part 1's checks or findings into the package below.

Return one compact review package containing:

- `module=technical_review`, `verdict`, and `next_check`
- cursor before and proposed cursor after
- changelog files and zero-based entry indexes fully inspected
- affected steps inspected
- findings ordered by severity; each includes no invented identifier, a plain-
  language root-cause summary, exact evidence, bounded `recommended_fix`,
  verification, recommended owner, and `user_judgment_required` with reason
- clean checks
- exact proposed marks grouped as `clean`, `findings`, or `cursor-backfill`
- for every proposed non-backfill mark, `surface_reviews` covering
  `downstream_steps`, `validation`, `evaluation`, `reporting`, `database`, and
  `learnings_and_knowledge`, each with one disposition (`updated`,
  `already_compatible`, `not_applicable`, `blocked`, or `broken`) and concrete
  evidence; `blocked` and `broken` also require durable Pulse `issue_ids`
- any blocked entry that prevented further cursor advancement

The parent Pulse Fixer/workshop agent validates this package, applies only bounded approved fixes, records typed Artifact Review findings/dispositions, and calls `mark_changelog_artifact_reviewed` for only the exact verified entries with all required surface reviews. Do not edit or delete changelog JSON directly and do not create a second cursor or state file.

A finding marked `user_judgment_required` needs the user's call before it is applied (never before). If this command is running as a live chat turn with the user present (a standalone `/review-artifact-drift`), ask directly in this chat and wait for the reply. If this is the unattended scheduled Pulse pass, never ask a direct question -- nobody is watching that chat and it would stall unanswered -- use `create_human_input_request` instead, which surfaces as a Needs your decision card the user answers later.
