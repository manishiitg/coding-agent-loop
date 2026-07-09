## Pulse — Dynamic Post-Run Steward

Pulse runs after a scheduled workflow run. It is not a fixed checklist. It is a small sequence with one mandatory intelligence turn:

1. **Gate / Worklist** — read the evidence, update `builder/improve.html`, and call `record_pulse_worklist`.
2. **Selected modules only** — the scheduler runs the modules Gate marked `due`.
3. **Dashboard / questions** — write org cards and durable human-input requests before backup/publish.
4. **Final backup / publish / notify** — back up and publish the final artifacts, then send the summary.

The durable state for module cadence lives in the workflow's `db/db.sqlite` table `pulse_module_state`. The visible user-facing state lives in `builder/improve.html`.

## Gate Contract

Gate decides what the next Pulse modules should do. It must call:

- `get_pulse_module_state(workspace_path="<current workflow>")` before deciding.
- `record_pulse_worklist(workspace_path="<current workflow>", pulse_run_id="<pulse session id>", decisions=[...])` exactly once before stopping.

Gate reads:

- latest run folder and run status
- `builder/improve.html`
- `soul/soul.md`
- `planning/plan.json`, `planning/step_config.json`, and `planning/changelog/`
- evaluation reports and `evaluation/evaluation_plan.json`
- `reports/report_plan.json` and `db/reports/*.html`
- `db/db.sqlite`, `db/README.md`, and DB assets/contracts
- `knowledgebase/notes` plus `knowledgebase/context` as read-only user context
- `learnings/_global/SKILL.md` and per-step learning metadata
- open and answered report human inputs in `db/db.sqlite`
- Chief of Staff recommendation cards in `builder/improve.html`
- cost and timing artifacts when present

Gate writes a clear **Pulse Gate / Worklist** card in `builder/improve.html`:

- Bug verdict: did the workflow run correctly?
- Goal verdict: is the workflow moving toward `soul.md` success criteria?
- Maintenance Radar: which lanes are quiet, watching, or due?
- Module worklist: each module `due` or `skipped`, with a short plain-language reason and evidence.

Gate does not call repair tools. It does not call `harden_workflow`, `improve_learnings`, `improve_kb`, `improve_db`, plan modification tools, backup, publish, or notify.

Gate must record exactly one decision for each module. A partial worklist is invalid because omitted modules would otherwise disappear silently.

## Module Decisions

Every decision needs a reason and evidence. Skips are useful only when they explain why work is not worth doing yet. Evidence can override any cooldown.

Use these module names exactly:

- `harden`
- `artifact_review`
- `report_health`
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

Usually mark due every Pulse run. It is report-only.

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

Goal Advisor is now a Pulse-selected module, not a separate recurring schedule. Pulse should not do the expensive strategic review inline. When the Gate selects Goal Advisor, the parent Pulse turn should call `run_goal_advisor_review(...)`, wait with `query_step(execution_id)`, then record the module result.

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

## Notifications

Pulse sends one summary every run unless the user's `soul/soul.md ## Notifications` section explicitly says not to notify.

The dashboard/questions turn runs before backup and publish. It writes `builder/card.health.html`, creates any needed `report_human_inputs` rows, and keeps `builder/improve.html` aligned with those asks. The notify turn runs after publish and should mainly deliver the already-recorded state.

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
