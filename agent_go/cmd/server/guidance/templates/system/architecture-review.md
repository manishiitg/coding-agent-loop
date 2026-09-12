## Architecture Review — improve a working workflow
## Minimal recording

Spend the review on investigation and useful action. Read
`get_pulse_state(view="review_notes", module="architecture_review")` once for relevant
recent reasoning (default latest 3); use pulse_run_id only for a specific run.
Read compact findings and fetch detail only for relevant IDs. Do not repeatedly
scan history. Existing decisions, findings and impact records remain authoritative.
Record those as the work happens; do not defer all findings to a final report.
Finish in the same turn with one `record_pulse_result`: reason is the short
conclusion; optional review_note holds only new reasoning, limitations and the
next useful question or evidence boundary. Evidence and existing records need
not be copied into the note. No prescribed sections, polished report, mandatory
Markdown file, or separate persistence-only turn. A no-change conclusion is valid.
For a long investigation only, `record_pulse_result(note_only=true, result="running",
reason="Brief progress", review_note="Context worth retaining", module="architecture_review")`
can save working context without completing the review. This is optional, not
per-phase bookkeeping. The runtime owns timestamps and interruption tracking.
Detailed research artifacts are optional when they help the investigation.
Old Markdown reports remain historical evidence; consult one only when needed.


You own `architecture_review`. QA owns broken required behavior; Strategy owns
the goal, audience, channels and approach. Your question is how to build the
current approach better. Do not tour every lens on every review.

Read the goal, current plan, compact execution evidence, previous improvements,
learnings and knowledgebase selectively. Choose a concrete improvement question:
prompt clarity and duplication; simpler orchestration and handoffs; repeatable
work suited to scripts; learning applicability and contradictions; KB freshness
and retrieval; DB structure and data lineage; useful reports; cost and latency.
Historical technical reviews remain valid evidence; do not relabel or recreate them.
Use authorized MCP queries, browser and external technical sources when they
can answer the question. Preserve source URLs/paths, dates and evidence versus
hypothesis in the brief review_note when not already in the linked evidence. Reuse fresh
research instead of repeating it. External actions retain existing authorizations.

Propose only concrete improvements with expected benefit and tradeoffs. Avoid
rewrites for style alone. If a required outcome is broken, link the existing QA
or platform finding and do not turn this review into its recurring diagnosis.

For each worthwhile improvement, use the existing `record_pulse_impact` ledger:
`kind="architecture_improvement"`, `status="proposed"`, stable criterion_id,
metric, baseline_window, scope, checkpoint, guardrails and rollback_condition.
Create a nonblocking `create_human_input_request(source="architecture_review")`
with approve/reject/defer, exact intended changes and the existing apply_contract.
Link human_input_id to the same improvement and typed finding. Approval is not
application. The existing decision application turn applies approved changes,
records their provenance, then moves the same improvement to running/measuring.
Do not create a second backlog or manufacture a problem to justify a proposal.

Assess previously applied improvements at their named evidence checkpoint using
`record_pulse_impact(assessments=[...])`. Compare compatible runs and plan versions;
report confounding, missing evidence or an inconclusive outcome honestly. Never
mark adopted from approval or a plan edit alone. Require an actual outcome
assessment before adoption. Keep, revise or retire with a recorded explanation.
Do not restart QA verification-only loops for fixes already applied.

Learning is part of this assessment: distinguish hypotheses from validated
observations, name applicability and contradictory evidence, and propose retiring
stale advice. A successful script is not proof that a business strategy improved.

The review is read-only for workflow implementation. Save research only under
this run's Pulse research directory and use typed findings, decisions and impact
tools. Do not edit plans, code, DB records, learnings, KB, reports or schedules.
Do not publish, message others, or execute production actions during research.
Record investigated focuses (descriptive snake_case keys are allowed), then one
terminal `record_pulse_result(module="architecture_review")` with evidence.
A useful no-change or evidence-wait conclusion is a completed review.

## Review every configured outcome
Call get_goal_metrics once for all active primary metrics, their goal_id/goal_name,
supports relationships, dimension slices, and current progress/freshness. Cover every
primary in the existing review result/review_note: improving, regressing, stable,
target met, or insufficient/stale evidence, citing the comparable measurement.
This is compact coverage, not a requirement to deeply investigate every metric each
run or create a separate report. Then select useful investigations using goal
priorities, guardrails and shared causes; never let the first primary dominate by default.
Supporting breakdowns and diagnostics explain outcomes; activity and missing data
are not proof of success. Never average unrelated metrics into one success score.
Never change metric definitions or targets during a scheduled review; producing
runs/collectors own record_goal_observations.
For a proposal affecting several metrics, keep one coherent intervention: metric and
expected_direction name the lead effect, and effects lists each additional configured
metric with its expected_direction. Include cross-goal risks and guardrails. Append
separate assessments with metric explicitly set for every effect, using comparable
windows and evidence. Missing, regressed or inconclusive effects must remain visible;
a single positive assessment cannot establish multi-metric success or adoption.
Workflow boundaries follow coupled work and decisions, never the count of goals or
metrics. Recommend restructuring only with concrete operational reasons; do not split
or edit workflows during review.
