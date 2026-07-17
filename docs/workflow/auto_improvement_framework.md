# Auto-Improvement Framework

Pulse gives the workflow durable evidence, bounded maintenance, strategic review, and an audit trail. It is **one system running at several agent-selected cadences** over a **single time-series log** (`builder/improve.html`), all sharing the same **Bug / Goal** vocabulary:

- **Pulse Gate:** reads retained evidence and decides which review modules are due, with explicit evidence/cooldowns rather than assuming a successful run proves correctness.
- **Read-only reviewers:** inspect bugs, artifact drift, learnings, KB, DB, eval, reports, cost/LLM operations, and Goal strategy without mutating the workspace.
- **One Pulse Fixer:** consumes those findings sequentially, applies only bounded verified repairs, and records blocked or failed work honestly.
- **Goal Advisor:** proposes strategy/headroom experiments when goals are missed, the current strategy is capped, or a periodic healthy-workflow review is due. Material changes use the existing human-input approval flow.

The model is intentionally simple: run/eval/report evidence are inputs; reviewers produce Signals, Pulse reflects on what they mean, the Fixer applies safe Kaizen, and Goal Advisor proposes larger changes when justified.

## Two verdicts: Bug and Goal

Every run is judged on two independent axes, shown as separate pills in the Pulse header — never collapsed into one "health":

- **Bug** — did it *run correctly*? Errors, skipped steps, missing/empty artifacts, regressions vs the last run. Investigated by Bug Review and repaired by Pulse Fixer. Roughly binary.
- **Goal** — is it *achieving its success criteria*? Eval verdicts and run outputs vs `soul.md`, trended through evidence-stamped Reflection entries. Addressed by Goal Advisor proposals/experiments. Continuous.

They are orthogonal — a run can be Bug-broken while Goal-on-target, or Bug-clean while Goal-short. **Health gates goal:** a run that wasn't operationally clean produces no trustworthy goal signal, so the goal is never judged on a broken run. For routed workflows the monitor judges **only the path that ran** — a step or eval belonging to a route this run didn't take is not-applicable, never a failure.

## Files

- `soul/soul.md`: stable intent only — objective, success criteria, optional explicit user-approved constraints, and optional notification preferences. Architecture and agent assumptions are revisable and do not belong here. It stays Markdown; there is no `soul.html`.
- `builder/improve.html`: the **Pulse history** — a schema-2, newest-first HTML time series. Every dated card is attributed to Signals / Kizuki, Reflection / Hansei, or Improvements / Kaizen plus one canonical module id, allowing Runloop to present the same history section-first. Pending decisions are rendered from structured `report_human_inputs`; answered question/outcome history belongs under Reflection. A bottom Agent log carries only compact handoff state and evidence pointers, never duplicate narrative. Read it before every Pulse pass. See `get_reference_doc(kind="review-improve-log")` for the format.
- `builder/improve-archive/YYYY-MM.html`: monthly archive files for old resolved findings and routine entries. Read only the archive files referenced by the active log's archive index or an unresolved id.
- `builder/card.health.html`: the compact per-run dashboard card the final dashboard/notify step overwrites each run (final post-Pulse status + headline/detail in `data-*` attributes). Goal / Ikigai itself remains in `soul/soul.md`; run verdicts and progress are time-stamped in Pulse history.
- `route_selection.json`: which route a run took (so the monitor judges only that path).
- `runs/iteration-0`: current optimizer evidence target.

Old Markdown improve logs are **legacy**. Carry their unresolved findings into `builder/improve.html` as open-finding entries and stop writing Markdown logs. The old structured `improve-decision` JSON blocks and `F-`/`I-` ids are retired in favor of readable prose cards.

## Truth Hierarchy

Use this hierarchy when deciding what is true:

1. `soul/soul.md`: canonical stable intent. Only explicit user-approved constraints are authoritative; architecture and agent-inferred assumptions remain challengeable.
2. `runs/iteration-0/<group>/...`: current reality from actual outputs, tool logs, validation, and eval reports.
3. `evaluation/evaluation_plan.json`: measurement definition; fix it when it conflicts with `soul.md`.
4. `planning/plan.json`: current implementation attempt, judged against `soul.md` and iteration-0 evidence.
5. `builder/improve.html` + referenced `builder/improve-archive/*.html`: memory and audit trail for reviewer Signals, run/Q&A Reflection, past decisions, unresolved findings, deferred ideas, and measured outcomes.

## Decision Model

Pulse separates review from mutation. Due modules inspect in parallel where safe; one parent Fixer then applies bounded changes sequentially.

Pulse Fixer and Goal Advisor are the two ends of an **exploit/explore** ladder against the success-criteria definition:

- Pulse Fixer — **exploit: refine the current strategy.** The approach is right but execution/wiring is weak: prompts, config, validation, KB, learnings, db/report wiring, or eval coverage need repair. It applies only findings supplied by read-only review and verifies each repair.
- Goal Advisor proposal/application — **explore: a different strategy for better success.** The current approach is **capped** — even executed cleanly it cannot satisfy the success criteria — or run evidence reveals a materially better approach. Scheduled Pulse starts this as a background `run_goal_advisor_review(...)` pass. New material strategy changes are proposal-first: create a `source="goal_advisor"` human-input request with exact intended edits, rationale, expected impact, risk, and evidence; a later Pulse pass applies approved changes with normal plan/config/eval/report tools.
- Eval-plan improvement: evaluation coverage, scoring, structured output, or validation schema is weak enough that measurement cannot be trusted — a success criterion is unmeasured, an eval step is orphaned or duplicates Pulse/pre-validation, the rubric drifts, or eval cost is out of proportion to run cost.
- No action: evidence is weak, recent changes need more runs, or the workflow is already aligned.

Each module may return multiple findings. Pulse keeps all material findings, but mutations remain ordered and bounded so parallel reviewers cannot race.

## Commands

- `/define-success`: confirms and normalizes Goal / Ikigai in `soul/soul.md`; it does not create a duplicate Goal/Profile card.
- `/pulse`: runs one complete manual Pulse against retained evidence without changing schedules or running the workflow.
- `/pulse-fixer`: applies bounded fixes from existing review findings and verifies them.
- The **Pulse** toolbar control enables the per-run review/fix pass via `post_run_monitor`.
- `/improve-evaluation`: improves eval coverage and rubric quality.
- Goal Advisor is selected dynamically by Pulse Gate; no separate recurring optimizer schedule is required.

## Audit Discipline

- **Open findings** carry a short anchor id only so a later fix can close them; closing a finding edits its card in place to add a `Resolved …` line — never delete it, never open a duplicate.
- **Human input requests are durable.** Pulse stores current questions in structured `report_human_inputs`; Runloop renders them and saves selected options and/or free-form answers. Once processed, the question, answer, outcome, and evidence are preserved as Reflection history in `builder/improve.html`. Email is a delivery channel, not the source of truth.
- **Decisions are confirmed, not assumed.** A Pulse Fixer or Goal Advisor decision states the effect it expects when written, and stays **unconfirmed** until a later run measures it — at which point Pulse stamps the decision card once: confirmed (cite before → after), no-effect/regressed (reopen a finding), or inconclusive (the run didn't exercise the changed path). A change that quietly failed is worse than no change, so it is never hidden.
- Eval-score movement is evidence, not proof. Do not claim an improvement worked until run/eval evidence supports it, and call out confounds such as small sample size, source-data drift, rubric changes, or multiple decisions in the same window. Rubric changes are the loop's biggest confound — they change what scores mean, so they go through a deliberate eval-plan-improvement pass with a major Decision card, never bundled with a harden/replan.

## Pulse Log Retention

`builder/improve.html` must stay readable for users and cheap for scheduled agents to load. When it passes roughly **800 lines, 60 KB, or 20 timeline entries**, move older **resolved** findings, superseded decisions, and routine run rows into a monthly archive `builder/improve-archive/YYYY-MM.html`, leaving a one-row entry in the archive index (date range, count, any still-unresolved ids).

**Never archive** open findings, user rules, active advisor experiments, current notes, or the latest few entries — the active Pulse should always answer "what's the state of this workflow right now, and what still needs attention." Archiving is append-preserving: move old detail, leave an index row, and never rewrite the meaning of an old decision.
