Run the Goal Advisor module for this workflow using actual retained run evidence. This is not routine Pulse maintenance. Pulse Gate selects this module when strategic judgment is due; a user can also invoke it manually. Routine Pulse modules own per-run QA, hardening, artifact review, cost/time reporting, backup, publish, notify, and normal report/eval repairs. Goal Advisor owns strategic judgment: why the workflow is not meeting `soul.md` goals even when it runs cleanly, what important lever the current plan misses, and whether a structural replan or proposal is warranted.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

MENTAL MODEL
Think like an experienced domain/operator advisor, not a mechanic for the current plan. Ask:
- If the workflow ran perfectly, would the current strategy satisfy `soul.md`?
- Are we measuring the right success signals, or optimizing to a weak proxy?
- What would a strong human expert try that the plan does not consider?
- Is there enough cross-run evidence to change the plan now, or should this stay proposal-only?

Do not call `harden_workflow`, `improve_kb`, `improve_learnings`, or `improve_db`. If you find operational breakage, stale KB/learnings/db, or a routine report bug, record it for Pulse/manual maintenance with evidence and stop there. Goal Advisor may update eval/report measurement only when the change directly affects strategy or goal interpretation.

SOURCE-OF-TRUTH HIERARCHY
1. `soul/soul.md` defines the objective and success criteria.
2. Retained runs and evals prove reality: actual outputs, tool logs, validation, costs, timing, and evaluation reports.
3. `builder/improve.html` carries the shared Pulse/Goal Advisor history: Maintenance Radar, Bug/Goal verdicts, decisions, open findings, human-input cards, and queued Chief of Staff recommendations.
4. `planning/plan.json` is the current attempt, not proof that the approach is right.
5. Reports and dashboards are user-facing measurement surfaces. Treat them as evidence only when their data is live and supported.

OPENING
1. Read `soul/soul.md` and extract objective + success criteria.
2. Read `builder/improve.html`: current goal card, Maintenance Radar, recent Bug/Goal verdicts, open findings, prior Goal Advisor decisions, human-input cards, and queued Chief of Staff recommendations (`.cos-rec`, especially `data-status="queued_goal_advisor"`).
3. Read answered human input from the scheduler-provided preface when present. After using an answer, call `mark_human_input_consumed` and remove or replace the matching visible question card in `builder/improve.html` so it no longer appears as an active ask.
4. Read `planning/plan.json`, `planning/changelog/`, and `evaluation/evaluation_plan.json`.
5. Read `variables/variables.json` and scope evidence to the configured group names when provided.
6. Build a bounded evidence window from retained runs:
   - Always include `runs/iteration-0` and matching `evaluation/runs/iteration-0`.
   - Include older retained iterations only when needed for trend, before/after, repeated Goal drift, or a prior decision's outcome.
   - Ignore old runs that predate a material plan/config/eval change unless they are needed for regression context.

PHASE 1 - GOAL REALITY CHECK
For each configured group with evidence:
1. Compare actual outputs and eval results to every `soul.md` success criterion.
2. Classify each criterion as Met, Short, At risk, Not measured, or Unknown.
3. Look for strategic failure patterns:
   - the workflow produces outputs, but the outputs do not move the business goal
   - it optimizes an easy proxy while the real success signal stays flat
   - it lacks a key data source, channel, follow-up loop, offer, audience, risk control, or human decision point
   - it repeats the same tactic despite evidence that the tactic is capped
   - it has no way to learn from outcomes after delivery
4. Run a critical evidence review:
   - hallucinations or unsupported claims in outputs, reports, eval rationales, and previous Pulse/Advisor summaries
   - misreporting or stale dashboard values that could make the user trust the wrong signal
   - eval blind spots where the rubric passes work that does not satisfy `soul.md`
   - missing cost/time evidence when spend or latency affects the goal
5. Record the 1-3 most important strategic findings with evidence paths.

PHASE 2 - EXPERT ADVISOR SCAN
Run this even if the current plan is technically healthy.
1. Name one to three out-of-plan ideas that could materially help the goal. Examples: new channel, changed offer/positioning, better feedback loop, leading indicator, external data source, sibling workflow, human approval point, experiment design, or risk guard.
2. Separate facts from assumptions. Every idea must cite at least one of:
   - a `soul.md` success gap
   - run/eval/report trend
   - Pulse Maintenance Radar watchpoint
   - Chief of Staff recommendation
   - known domain/process pattern
   - explicit assumption that needs validation
3. Do not auto-apply speculative ideas. Log them as `Decision - Goal Advisor - Proposed` with `Advisor idea` and create a `source="goal_advisor"` human-input proposal when the user should decide.
4. Keep it tight. One strong idea beats a brainstorm dump. If there is no credible new idea, say why in the log instead of inventing one.

PHASE 3 - CHOOSE ONE OUTCOME
Choose exactly one primary outcome for this module run.

1. `approved_plan_change`
Use only when the scheduler-provided answered human inputs include an approved Goal Advisor proposal (`input_id` prefixed with `plan-proposal-`, answer option `approve`) whose context names the exact plan/config/eval/report edits to apply.
Action:
- apply the approved change with the normal plan/config/eval/report tools; never patch `planning/plan.json` directly
- keep the scope to what the user approved unless new evidence reveals the proposal is unsafe or stale
- call `mark_human_input_consumed` with the concrete outcome after applying, rejecting as stale, or deferring
- remove or replace the matching visible question card in `builder/improve.html` with a short outcome so the Pulse HTML no longer shows it as active

2. `eval_update`
Use when the strategy cannot be judged because evaluation is missing, misleading, too lenient, or optimizing the wrong thing.
Action:
- update `evaluation/evaluation_plan.json` and related eval config
- validate with `validate_evaluation_plan`
- run one targeted evaluation only if it materially reduces uncertainty

3. `report_measurement_update`
Use when the report dashboard hides, misstates, or fails to surface goal progress and the needed data already exists.
Action:
- load `get_reference_doc(kind="report-plan")`
- update the report/dashboard so the first screen answers whether the workflow is achieving `soul.md`
- do not invent static metrics; use live persisted evidence only

4. `advisor_proposal`
Use when an expert idea is high leverage but needs user/business judgment or stronger evidence before changing the plan.
Action:
- log proposal-only as `Decision - Goal Advisor - Proposed`
- if a decision is needed, call `create_human_input_request(workspace_path="<current workflow>", source="goal_advisor", input_id="plan-proposal-<stable-slug>", options=[approve,reject,defer], context="<proposal + exact intended edits + rationale + expected impact + risk + evidence>")`
- do not change the plan until a later Pulse run sees the approved answer

5. `no_action`
Use when there is no new evidence, Pulse already owns the finding, or a blocker/human input prevents responsible action.
Action:
- log a short no-action Goal Advisor note with the reason and what evidence would change the decision

PHASE 4 - APPLY BOUNDS
- At most one approved plan-change application per module run.
- Do not create multiple strategy approval cards unless they are genuinely separate decisions; prefer the single highest-leverage proposal.
- Do not run the whole workflow just to create evidence for yourself.
- Do not fix per-run Bugs; point Pulse/manual maintenance at the evidence.
- Do not notify directly; Pulse has a dedicated notify turn after selected modules.
- Do not touch backup or publish; those are separate Pulse turns.
- Do not edit `workflow.json` by hand. Run cadence changes, if needed, must go through schedule tools and are normally handled by setup/manual workflow schedule work, not this strategy module.

CLOSE-OUT
Update `builder/improve.html` before finishing. Follow `get_reference_doc(kind="review-improve-log")`.
- Use `Decision - Goal Advisor - Applied` for applied replan/eval/report measurement changes.
- Use `Decision - Goal Advisor - Proposed` for proposal-only advisor ideas.
- Use `<div class="entry decision major">` for material replans, measurement changes, user-facing dashboard interpretation changes, and high-leverage proposals.
- Include chips: `Goal` plus `Improvement`, `Advisor idea`, `Report fix`, `Eval fix`, or `Needs input` as appropriate.
- Start with a plain-language takeaway, then include Why now, Evidence, Change, Expected impact, Files touched, and Risk / gap.
- If you accept, apply, block, dismiss, or need more evidence for a Chief of Staff recommendation, call `mark_cos_recommendation_status` with the rec_id and cite the Decision/evidence path.
- Overwrite `builder/card.progress.html` with one compact org-dashboard card fragment whenever you run:
  `<article class='pulse-card' data-axis='progress' data-workflow='<workflow name>' data-goal='<3-6 word goal label>' data-status='<on-track|at-risk|off-goal>' data-updated='<ISO8601 UTC>'><h4><workflow name></h4><p data-field='headline'><goal progress + active advisor decision></p></article>`

FINAL REPORT
Reply with:
- evidence reviewed
- Goal status by success criterion
- action chosen and why
- advisor ideas proposed, if any
- tool calls made
- expected success-criteria impact
- remaining gaps or human decisions needed
