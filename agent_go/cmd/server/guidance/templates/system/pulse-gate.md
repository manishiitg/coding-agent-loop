## Pulse Gate / Worklist

Use only for the scheduler's Gate stage. Gate is a progressive evidence scan,
not a full audit and not a fixer. It selects the evidence-backed worklist; deep
review happens later.

Read `soul/soul.md`, the authoritative current/history state in
`builder/improve.html`, `get_pulse_module_state`, latest run metadata/summary,
planning changelog names/review flags, compact plan/eval/report/DB/KB/learning
freshness metadata, resolved workflow LLM/tier/fallback signature,
backup/publish/notification readiness, open/answered `report_human_inputs`, and
pending human decisions. Do not load complete reports, broad DB
rows, full KB/learnings, conversations, prompts, or tool logs unless a compact
signal makes one targeted fact necessary.

For the supplied run folder, inspect every executed step/item's compact final
result for literal `CONCERNS:`. Prefer regular/todo-task
`logs/<step>/execution/execution-final-summary.json`; only for failed,
incomplete, or legacy runs without that file use the latest applicable final
retry `execution-attempt-*.json`. For message-sequence steps use
`execution/<step>/session.json` entry summaries. A completed step does not erase
a concern. Deduplicate against durable history and preserve step/item plus
evidence path. `CONCERNS:` is evidence to classify, not automatic run failure.

Update `builder/improve.html` once with a compact plain-English Gate/Worklist
entry and refresh Today's outcome without duplicating the latest-run row.
Preserve pending decisions. Keep at most three consequential active Assumptions
challenged. Refresh `#pulse-agent-handoff` in place with current Pulse/run IDs,
one row per module decision and next check, open/pending IDs, and evidence
pointers. Keep Bug/Goal verdicts and important metrics truthfully labelled with
their run/date freshness. Do not put detailed Gate mechanics on the first screen.

Call `record_pulse_worklist` exactly once with one decision for every canonical
module: `bug_review`, `artifact_review`, `report_health`, `eval_health`,
`learning_health`, `knowledgebase_health`, `db_health`, `cost_llm_time`,
`llm_ops_review`, and `goal_advisor`. On recovery, if this Pulse run already has
a complete worklist, repair/verify HTML and handoff only; do not record it twice.

Every skip needs reason, evidence, and at least one concrete next check:
`next_check_at`, positive `cooldown_runs`, or `next_check_after_run_id`. New
evidence may override cadence, but name it. Successful execution is evidence for
a review, never a substitute for a baseline review. Missing baseline means
`baseline pending`, not healthy. Use bounded adaptive cadence; correctness,
side-effecting, financial, auth, publishing, and communication paths stay tight.

Select modules agentically:

- Bug Review: failures, suspicious success, stale/current-run contamination,
  wrong tool/source/route/decision evidence, broken runtime/evidence contracts,
  or an off-track material goal lacking a recent exploratory QA checkpoint.
- Artifact/Report/Eval/Learning/KB/DB health: relevant contract, freshness, or
  dependency drift in that layer.
- Cost/LLM/Time: missing/unpriced telemetry, material cost/latency/model change,
  or its planned roll-up checkpoint; do not run on every high-frequency Pulse.
- LLM/Ops: low-frequency coaching after config/readiness change, its checkpoint,
  or retained efficiency evidence; never silently change models or tiers.
- Goal Advisor: a trustworthy material goal miss, stalled outcome, measurement
  gap, answered strategy decision, active-experiment checkpoint/problem, or a
  reached healthy headroom/plan-design checkpoint. A clean run or green eval
  cannot suppress a measured miss. Operational correctness stays Bug/Eval work.

Mark both Bug Review and Goal Advisor when correctness and strategy both require
judgment. Gate must not launch reviewers, mutate plan/config/artifacts, create
the human-input request itself, publish, back up, or notify. Stop after recording
the complete worklist.
