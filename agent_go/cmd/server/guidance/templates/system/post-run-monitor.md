## Pulse — Dynamic Post-Run Steward

Pulse runs after a scheduled workflow run. It is not a fixed checklist. It is a small sequence with one mandatory intelligence turn:

1. **Gate / Worklist** — read the evidence, update `builder/improve.html`, and call `record_pulse_worklist`.
2. **Selected modules only** — the scheduler runs the modules Gate marked `due`.
3. **One ordered finalizer turn** — dashboard/questions, backup, conditional publish, then notify. Each command records its own live/final status in `pulse_final_command_state`.

`builder/improve.html` is the authoritative durable source for Pulse history, prior fixes, findings, cadence reasoning, and decisions. The workflow's `db/db.sqlite` table `pulse_module_state` is only the current machine-readable Gate/worklist/result cache used by the scheduler and Pulse popup; it must not replace or contradict the HTML history. Every Gate decision, cadence reason, and module outcome that matters later must also be recorded visibly in `builder/improve.html`.

When updating `builder/improve.html`, keep the first screen short. It may show the workflow name, one compact status headline, current goal/health widgets, and the next useful action. Do not duplicate the full latest-run Bug/Goal narrative at the top if the same details already appear in Recent runs or the timeline.

## Timeout Recovery

The scheduler uses a sliding inactivity timeout: 10 minutes without observable progress for a normal Pulse step and 30 minutes without progress for Goal Advisor. Tmux output, tool calls, tracked execution changes, and session activity reset that timer, so healthy long-running work is not canceled merely because its total duration exceeds 10 or 30 minutes. When a step makes no progress for its full inactivity window, the scheduler records the selected module as `timed_out`, cancels work owned by the old Pulse session, and skips the remaining optional maintenance modules so concurrent repairs cannot race. It then resumes the single ordered finalizer in a fresh recovery session. If the finalizer itself times out, any final command that did not record an outcome is marked `timed_out`. Recovery turns must report the partial outcome plainly and must not claim that timed-out or skipped work succeeded.

## Gate Contract

Gate decides what the next Pulse modules should do. Read `builder/improve.html` as the primary historical source before using the SQLite cache for current scheduler state. It must call:

- `get_pulse_module_state(workspace_path="<current workflow>")` before deciding.
- `record_pulse_worklist(workspace_path="<current workflow>", pulse_run_id="<pulse session id>", decisions=[...])` exactly once before stopping.

Gate uses **progressive evidence triage**. Start with compact state and metadata:

- latest run metadata/summary and run status, not every long log
- `builder/improve.html` current dashboard, open items, recent timeline, and cadence
- `soul/soul.md`
- `planning/plan.json`, `planning/step_config.json`, and `planning/changelog/`
- existence/freshness of evaluation reports and `evaluation/evaluation_plan.json`
- existence/freshness of `reports/report_plan.json` and report HTML
- `db/README.md` and a compact DB schema summary
- a compact KB note index; `knowledgebase/context` remains read-only user context
- per-step learning metadata and whether global learnings changed
- open and answered report human inputs in `db/db.sqlite`
- Chief of Staff recommendation cards in `builder/improve.html`
- compact cost/timing availability and change signals when present

Do not load full report HTML, full KB/learnings, broad DB rows, every cost file, or long run logs merely to decide cadence. Open large evidence only when a compact signal makes that module plausibly due or one targeted fact is needed to justify a decision. The selected module performs the deep inspection later; Gate is triage.

Gate writes a compact **Pulse Gate / Worklist** entry in the Pulse log/timeline area of `builder/improve.html`. Do not put full Gate details in the first-screen/top dashboard; the top dashboard should stay focused on latest outcome, goal health, and next useful action.

The first screen may legitimately combine evidence measured by different routes or runs, but freshness must be explicit. The overall status reflects the latest run. Every carried-forward verdict, goal criterion, brief cell, and important signal/cost tile must visibly say `as of run <id/date>` or `last measured <id/date>`; never leave an older value looking current. If the latest run did not measure a signal, retain the last trustworthy value and label it `not measured this run · last measured ...`.

Update the stable header elements `#pulse-bug-verdict` and `#pulse-goal-verdict` in place. If either is missing from an otherwise current-format page, insert the standard two-element `.verdicts` block beside the workflow title without rewriting the timeline. Never create a duplicate verdict block.

- Bug verdict: did the workflow run correctly?
- Goal verdict: is the workflow moving toward `soul.md` success criteria?
- Maintenance Radar: which lanes are quiet, watching, or due?
- Module worklist: each module `due` or `skipped`, with a short plain-language reason and evidence.

Gate does not call repair tools. It does not call `harden_workflow`, `improve_learnings`, `improve_kb`, `improve_db`, plan modification tools, backup, publish, or notify.

Gate must record exactly one decision for each module. A partial worklist is invalid because omitted modules would otherwise disappear silently.

## Module Decisions

Every decision needs a reason and evidence. Skips are useful only when they explain why work is not worth doing yet. Every skipped module must set at least one concrete next-check condition: `next_check_at`, `next_check_after_run_id`, or a positive `cooldown_runs`. Write that planned next check visibly in the Gate/Worklist entry in `builder/improve.html`; SQLite only mirrors it for the scheduler and popup.

Cadence remains agentic. New evidence can override any earlier cooldown or next-check suggestion, but when Gate checks a module earlier than previously planned, its reason and the visible Gate entry must say what new evidence caused the override. Do not silently ignore the prior cadence.

Use these module names exactly:

- `harden`
- `artifact_review`
- `report_health`
- `eval_health`
- `learning_health`
- `knowledgebase_health`
- `db_health`
- `cost_llm_time`
- `goal_advisor`

### harden

Mark due for real Bug findings:

- failed, skipped, or empty steps
- hallucinated or unsupported step success
- broken eval/report layers that make evidence untrustworthy
- selector/API/runtime breakage
- stale guards, validation, retry, or defaulting behavior
- Chief of Staff recommendations that are operational bugs

The module calls `harden_workflow` after loading `get_reference_doc(kind="optimize-playbook")`.

### artifact_review

Mark due when `planning/changelog/` has unreviewed material entries or when plan/config changes may have drifted dependent artifacts:

- reports
- evals
- DB contracts
- KB notes or step KB config
- learnings or learning locks
- saved code artifacts
- step prompts/configs

The module is report-only. It calls `review_artifact_sync` through `get_workflow_command_guidance(kind="review-artifact-drift")` and uses `mark_changelog_artifact_reviewed`; it does not fix artifacts directly.

### report_health

Mark due when the reporting dashboard is stale, misleading, broken, too text-heavy, not goal-oriented, or not using live persisted evidence correctly.

Good report-health work makes the report easier for the user to understand:

- clear goal progress
- current plan and strategy
- blockers and issues
- live SQL/window.report evidence
- compact visual cards before long text
- accurate tabs/sections and responsive layout

### eval_health

Mark due when evaluation evidence cannot be trusted or does not measure the workflow's stated success criteria:

- `evaluation/evaluation_plan.json` is missing, stale, too lenient, or not mapped to `soul.md`
- eval runs are missing, scoped to the wrong run/group, or using a stale `TARGET_RUN_PATH`
- rubric/thresholds can be gamed or mostly duplicate operational completion checks
- eval reports make misleading claims or cannot be reconciled with DB/report evidence
- plan, DB, report, or output contracts changed and eval coverage did not follow

The module calls `get_workflow_command_guidance(kind="improve-evaluation")` for bounded eval-plan/config repair. Prefer targeted evaluation changes, validate the plan when the validation tool is available, and only run a targeted eval when it materially helps verify the fix. Record changed eval artifacts as an `Eval fix` in `builder/improve.html`.

### learning_health

Mark due when workflow behavior changed or learning state may be stale:

- plan or prompt changes affected step behavior
- `learning_objective` no longer matches the step
- `lock_learnings` should be cleared because guidance is stale
- mature stable learnings should be locked with evidence
- a run discovered reusable HOW-to knowledge worth capturing
- selectors/API quirks changed

This module may update step config for learning lock/unlock decisions and may call `improve_learnings`. Use absolute workspace paths in prompts and evidence. Do not ask a step agent to hand-edit files through a path outside the workspace root.

### knowledgebase_health

Mark due when KB notes or KB config are missing, duplicated, stale, contradictory, or no longer aligned with the plan.

`knowledgebase/context` is user-owned runtime business context. Read it for evidence, but do not rewrite it. Use `improve_kb` for notes/config cleanup.

### db_health

Mark due when DB schema, table contracts, upsert rules, report SQL, eval consumers, or `db/README.md` no longer match current writers and readers.

Use `improve_db` for concrete DB contract/schema/report compatibility work. Do not run speculative row migrations.

### cost_llm_time

This is report-only, but it is not automatically due every Pulse. High-frequency workflows should normally roll up several runs and use `cooldown_runs` or a concrete next-check date. Mark it due immediately when telemetry is missing/unpriced, cost or latency changes materially, model/tier configuration changes, a prior cost finding needs follow-up, or its planned next check arrives.

Read workflow execution cost, evaluation cost, builder/Pulse overhead, token usage, model/tier evidence, missing cost buckets, and timing summaries. If any bucket is missing or unpriced, say that plainly instead of estimating.

Do not change model tiers, prompts, schedules, or agent allocation from this module. If model selection looks wrong, record it as evidence for `goal_advisor` or user review.

### goal_advisor

Mark due when strategic judgment is needed:

- Goal drift persists even when execution is clean
- the current strategy appears capped or too narrow
- a user answered a strategic question
- a Chief of Staff recommendation is strategic
- enough new cross-run evidence exists for an expert out-of-plan critique
- the workflow may need an eval/report measurement change to judge success correctly

Goal Advisor is now a Pulse-selected module, not a separate recurring schedule. Pulse should not do the expensive strategic review inline. When the Gate selects Goal Advisor, the parent Pulse turn should call `run_goal_advisor_review(...)`, capture the returned `execution_id`, optionally call `query_step(step_id="goal-advisor", execution_id="<returned execution_id>")` once for immediate status, then stop if it is still running and rely on `[AUTO-NOTIFICATION]` to resume result recording. Do not use `sleep`, `list_executions`, or repeated `query_step` calls as a polling loop.

The background Goal Advisor thinks like an experienced operator. It may apply a structural plan change only when the user already approved a Goal Advisor proposal in `report_human_inputs`. New strategic changes must be logged as proposal-only Advisor ideas and, when a decision is needed, created with `create_human_input_request`.

Goal Advisor does not do routine hardening, learning cleanup, KB cleanup, DB cleanup, or normal report repair. Those are separate Pulse modules.

## Human Input

If Pulse, Goal Advisor, or a module needs the user to decide something, create a durable request with:

`create_human_input_request(workspace_path="<current workflow>", source="pulse|goal_advisor", ...)`

For Goal Advisor plan-change proposals, use the existing interaction shape instead of a separate tool or file:

- `source="goal_advisor"`
- `input_id="plan-proposal-<stable-slug>"`
- options: `approve`, `reject`, and `defer`, each with a short title and description
- `context`: proposal, exact intended plan/config/eval/report edits, rationale, expected impact, risk, and evidence paths

On a later Pulse run, an approved proposal may be applied with normal plan/config/eval/report tools and then marked consumed with `mark_human_input_consumed`. Rejected or deferred proposals should be recorded and consumed, not silently retried. After consuming an answer, remove the matching visible question card from `builder/improve.html` or replace it with a short outcome Decision/Note; do not leave a consumed answer displayed as an active question.

Do not ask only in email or raw chat. Show the request in `builder/improve.html`, but treat `db/db.sqlite` as the source of truth. When a later pass uses an answer, call `mark_human_input_consumed` and clear the visible question from the Pulse HTML.

## Finalizer And Notifications

Pulse sends one summary every run unless the user's `soul/soul.md ## Notifications` section explicitly says not to notify.

Dashboard/questions, backup, publish, and notify run in one ordered finalizer turn to avoid four repeated context loads. Before and after each command, call `mark_pulse_final_command_result` so the Pulse popup shows `waiting`, `running`, `done`, `skipped`, `blocked`, `failed`, or scheduler-recorded `timed_out` instead of static labels. The scheduler treats any command left waiting/running after the turn as failed rather than pretending it completed.

The dashboard command writes `builder/card.health.html`, creates any needed `report_human_inputs` rows, and keeps `builder/improve.html` aligned with those asks. Backup skips its actual operation only when its source-hash check proves the exact current state is already backed up. Publish skips when disabled, unverified, or already current; it never performs a first/verifying publish unattended. Notify runs last and mainly delivers the already-recorded state, even when an earlier final command failed.

The notify turn should include:

- Bug and Goal state
- modules that ran and modules that skipped
- important changes/fixes/proposals
- user questions created
- backup/publish status
- dashboard URL when publish is live
- cost/time summary, including missing or unpriced buckets

When Gmail/email is available, default to rich email: set `email_subject`, `email_html`, and plain `email_body` on the same `notify_user` call. Use `email_to` only when the user's preference replaces the default To recipient. Use `email_cc` only when requested.

## Style

Write for the user first:

- takeaway first
- short labeled details after
- evidence paths last
- no long semicolon chains
- no compressed internal jargon unless also explained in plain language
- visual cards and chips before long text

Never invent values, trends, costs, or eval results. If evidence is missing, say exactly what is missing and why that matters.
