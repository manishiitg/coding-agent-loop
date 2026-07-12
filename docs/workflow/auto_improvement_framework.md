# Auto-Improvement Framework

Auto-improvement gives the optimizer durable evidence and an audit trail for improving a workflow. It is **one system running at several cadences** over a **single log** (`builder/improve.html`, the **Pulse**), all sharing the same **Bug / Goal** vocabulary:

- **Per-run monitor (detect, every run):** a cheap, read-only pass after each run that records whether the workflow ran correctly and whether it is achieving its goal — it never fixes anything.
- **Scheduled harden (act, frequent):** applies local reliability/contract/artifact fixes for **Bug** findings.
- **Scheduled replan-proposal (act, less frequent):** recommends plan/strategy changes for **Goal** gaps — it proposes, it does not auto-rewrite the plan.

The model is intentionally simple: run and eval evidence are the inputs, and the optimizer chooses between hardening, replanning, eval-plan improvement, or no action.

## Two verdicts: Bug and Goal

Every run is judged on two independent axes, shown as separate pills in the Pulse header — never collapsed into one "health":

- **Bug** — did it *run correctly*? Errors, skipped steps, missing/empty artifacts, regressions vs the last run. Fixed by **hardening**. Roughly binary.
- **Goal** — is it *achieving its success criteria*? Eval verdicts and run outputs vs `soul.md`, trending over runs via the goal card in `builder/improve.html`. Fixed by **refining or replanning**. Continuous.

They are orthogonal — a run can be Bug-broken while Goal-on-target, or Bug-clean while Goal-short. **Health gates goal:** a run that wasn't operationally clean produces no trustworthy goal signal, so the goal is never judged on a broken run. For routed workflows the monitor judges **only the path that ran** — a step or eval belonging to a route this run didn't take is not-applicable, never a failure.

## Files

- `soul/soul.md`: stable intent only — objective, success criteria, optional explicit user-approved constraints, and optional notification preferences. Architecture and agent assumptions are revisable and do not belong here. It stays Markdown; there is no `soul.html`.
- `builder/improve.html`: the **Pulse** — the single, self-contained, human-readable HTML log and the user's primary window into the workflow. Runloop renders pending decisions first; the HTML prioritizes active challenged assumptions, today's outcome, goal progress, and recent activity, with signal/cost/maintenance detail collapsed. A bottom Agent log carries only compact handoff state and evidence pointers, never duplicate narrative. Read it before every improve pass. See `get_reference_doc(kind="review-improve-log")` for the format.
- `builder/improve-archive/YYYY-MM.html`: monthly archive files for old resolved findings and routine entries. Read only the archive files referenced by the active log's archive index or an unresolved id.
- `builder/card.health.html`: the compact per-run dashboard card the monitor's final notify/summary step overwrites each run (final post-Pulse status + headline/detail in `data-*` attributes). The Bug/Goal verdicts themselves live in the Pulse log's pills + goal card — there is no separate verdict file.
- `route_selection.json`: which route a run took (so the monitor judges only that path).
- `runs/iteration-0`: current optimizer evidence target.

Old Markdown improve logs are **legacy**. Carry their unresolved findings into `builder/improve.html` as open-finding entries and stop writing Markdown logs. The old structured `improve-decision` JSON blocks and `F-`/`I-` ids are retired in favor of readable prose cards.

## Truth Hierarchy

Use this hierarchy when deciding what is true:

1. `soul/soul.md`: canonical stable intent. Only explicit user-approved constraints are authoritative; architecture and agent-inferred assumptions remain challengeable.
2. `runs/iteration-0/<group>/...`: current reality from actual outputs, tool logs, validation, and eval reports.
3. `evaluation/evaluation_plan.json`: measurement definition; fix it when it conflicts with `soul.md`.
4. `planning/plan.json`: current implementation attempt, judged against `soul.md` and iteration-0 evidence.
5. `builder/improve.html` + referenced `builder/improve-archive/*.html`: memory and audit trail for past decisions, unresolved findings, deferred ideas, resolution links — and the per-criterion goal card, which is the durable goal signal over runs.

## Decision Model

The per-run monitor only **detects and records**. The scheduled passes then choose one bounded action.

Harden and replan are the two ends of an **exploit/explore** ladder against the success-criteria definition — same plan tools, opposite intent:

- `Pulse Bug Review/Fixer(group_name?, focus?)` — **exploit: refine the current strategy.** The approach is right but execution/wiring is weak: prompts, config, validation, KB, learnings, db/report wiring, or eval coverage need repair. Not a redesign. Harden removes stale `learnings/{step-id}/main.py` for `code_exec` steps; only `learn_code` steps should retain reusable `main.py`.
- Goal Advisor proposal/application — **explore: a different strategy for better success.** The current approach is **capped** — even executed cleanly it cannot satisfy the success criteria — or run evidence reveals a materially better approach. Scheduled Pulse starts this as a background `run_goal_advisor_review(...)` pass. New material strategy changes are proposal-first: create a `source="goal_advisor"` human-input request with exact intended edits, rationale, expected impact, risk, and evidence; a later Pulse pass applies approved changes with normal plan/config/eval/report tools.
- Eval-plan improvement: evaluation coverage, scoring, structured output, or validation schema is weak enough that measurement cannot be trusted — a success criterion is unmeasured, an eval step is orphaned or duplicates Pulse/pre-validation, the rubric drifts, or eval cost is out of proportion to run cost.
- No action: evidence is weak, recent changes need more runs, or the workflow is already aligned.

Each improve pass should perform at most one primary action unless the user explicitly asks for a broader pass.

## Commands

- `/define-success`: confirms the goal with the user, writes the workflow profile, and seeds the Pulse goal card from `soul.md`.
- The **Pulse** toolbar control enables the per-run review/fix pass via `post_run_monitor`.
- `/improve-evaluation`: improves eval coverage and rubric quality.
- Goal Advisor is selected dynamically by Pulse Gate; no separate recurring optimizer schedule is required.

## Audit Discipline

- **Open findings** carry a short anchor id only so a later fix can close them; closing a finding edits its card in place to add a `Resolved …` line — never delete it, never open a duplicate.
- **Human input requests are durable.** If Pulse or Auto Improve asks the user a question in email/chat, it first writes or refreshes a `Human input requested` card in `builder/improve.html` with question, why it matters, options, default if no answer, evidence, and status. Email is a delivery channel, not the source of truth.
- **Decisions are confirmed, not assumed.** A harden/replan decision states the effect it expects when written, and stays **unconfirmed** until a later run measures it — at which point the monitor stamps the decision card once: confirmed (cite before → after), no-effect/regressed (reopen a finding), or inconclusive (the run didn't exercise the changed path). A change that quietly failed is worse than no change, so it is never hidden.
- Eval-score movement is evidence, not proof. Do not claim an improvement worked until run/eval evidence supports it, and call out confounds such as small sample size, source-data drift, rubric changes, or multiple decisions in the same window. Rubric changes are the loop's biggest confound — they change what scores mean, so they go through a deliberate eval-plan-improvement pass with a major Decision card, never bundled with a harden/replan.

## Pulse Log Retention

`builder/improve.html` must stay readable for users and cheap for scheduled agents to load. When it passes roughly **800 lines, 60 KB, or 20 timeline entries**, move older **resolved** findings, superseded decisions, and routine run rows into a monthly archive `builder/improve-archive/YYYY-MM.html`, leaving a one-row entry in the archive index (date range, count, any still-unresolved ids).

**Never archive** open findings, user rules, current notes, the goal card, or the latest few entries — the active Pulse should always answer "what's the state of this workflow right now, and what still needs attention." Archiving is append-preserving: move old detail, leave an index row, and never rewrite the meaning of an old decision.
