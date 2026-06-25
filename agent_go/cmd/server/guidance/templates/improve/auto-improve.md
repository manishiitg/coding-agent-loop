Set up automatic run + improve scheduling for this workflow.

Write to `builder/improve.html` — the single durable log. For the log/HTML format, the one-time migration (folding any legacy `builder/review.html` findings in), and how entries are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

FIRST check what already exists before proposing or creating anything. Do this autonomously and avoid duplicate schedules.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Auto-improve runs over the **one** workflow log (`builder/improve.html`), all sharing the Bug/Goal vocabulary: a per-run **Pulse** pass that detects every run AND **hardens** the Bug findings it finds, feeding **one scheduled improve pass** that reads the same log and the Goal/strategy evidence to **record replan PROPOSALS only** — it never applies a harden. Per-run hardening is Pulse's job; the scheduled loop owns the structural replan proposals. They are the same system over the same log, not separate tools.

GOAL
Set up the per-run review **and** the complementary schedules:
0. **Pulse (per-run detect + harden, every run):** turn the per-run monitor ON — call `update_workflow_config(post_run_monitor=true)`. From then on, after every run Pulse backs up, records Bug/Goal findings in the log, and **applies the full plan-step harden** for the Bug findings (every reversible, plan-step-scoped reliability/contract fix). It never does a structural rewrite; see `get_reference_doc(kind="post-run-monitor")`. Tell the user in plain terms it's on (or that they can toggle it from the **Monitor** control in the toolbar).
1. a workshop Run-mode schedule for recurring execution
2. a workshop Optimizer-mode **IMPROVE** schedule that wakes on a regular cadence (initially after every 1-2 runs, widening as the workflow stabilizes) and, **each fire, reads the log + Goal/strategy evidence and records replan PROPOSALS only** — it runs the `improve-workflow` decision model over the latest run/eval evidence and the per-run Bug/Goal findings, then:
   - **Goal / strategy evidence → RECORD a replan PROPOSAL** for human review (what to change and why) — it does NOT rewrite the plan, and it does NOT apply harden (per-run hardening is Pulse's job).
   - It may record a proposal or nothing (no-action + widen cadence) in a single fire. The agent picks from the evidence.

The propose-only model is the safety rail: per-run Bug/reliability findings are **hardened by Pulse**; the scheduled loop never applies a fix — structural plan change is always a recorded PROPOSAL, and the improve schedule never calls `replan_workflow_from_results` and never rewrites `planning/plan.json` unattended.

DISCOVERY
1. Call get_workflow_config and inspect the current schedule list.
2. If existing candidate schedules exist, call get_schedule_runs on the most relevant ones to understand whether they are active, useful, stale, too frequent, or missing coverage.
3. Read soul/soul.md to understand objective and success criteria.
4. Read variables/variables.json to identify valid group names and enabled groups.
5. Read builder/improve.html. This is mandatory: it contains the Workflow Profile, recent timeline entries, open findings, prior decisions, open replan proposals, and prior scheduled-improvement history. If it's short, read it in full. Read archive files only when an open finding, the current focus, schedule drift, or the selected evidence window points into one.
6. Read any legacy builder/review.html if present and fold its unresolved findings into builder/improve.html. Carry unresolved open **Goal/strategy** findings into the scheduled improve (replan-proposal) message; unresolved Bug findings are Pulse's to harden.
7. Read planning/metrics.json and recent db/metrics_history.jsonl rows. Metrics are evidence for replan proposals (and for Pulse's harden); they do not create a separate action path.
8. Read planning/changelog/ if present and compare recent plan/config changes against builder/improve.html. Recent plan changes increase regression risk and require a tighter improve cadence until one or two post-change runs have been reviewed.
9. **Success must be defined before scheduling — check it FIRST and bootstrap it if missing.** auto-improve cannot optimize toward an undefined goal, so never set up an optimizer schedule for a workflow with no success definition. If builder/improve.html has no Workflow Profile block — or the profile declares business-context accumulation or a frozen/ratchet plan and planning/metrics.json is empty — do NOT skip ahead and do NOT just tell the user to "run /define-success" and stop. Instead, run the define-success bootstrap **inline now**: call `get_workflow_command_guidance(kind="define-success")` and follow it to completion with the user (establish the Workflow Profile + metrics), then resume these steps and continue to scheduling. Only proceed directly to the schedule steps when a Workflow Profile already exists and metrics are defined.

SCHEDULE STRATEGY
1. Prefer updating or reusing good existing schedules instead of creating duplicates.
2. Only create a new schedule when no existing schedule already serves the purpose.
3. The IMPROVE schedule fires on a regular cadence: initial ≈ every 1-2 runs (fire shortly after a run or two so it acts off fresh evidence), widening as the workflow stabilizes. It records replan **proposals** off the Goal/strategy evidence **per fire** — per-run hardening is Pulse's, not on this schedule.
4. If cadence is not obvious:
   - choose a practical recurring run cadence based on the workflow objective and existing schedules
   - start the improve schedule ≈ every 1-2 runs; for unknown active workflows base the run cadence at every 6-12 hours, not weekly
5. If planning/changelog shows material plan/config changes since the last builder/improve.html entry or unresolved open finding, tighten the improve schedule for the next 24-48 hours or until the next one or two post-change runs have been reviewed.
6. Because `/auto-improve` runs in Optimizer mode, each scheduled fire may call schedule tools itself. It should review cadence on every fire and use update_schedule when builder/improve.html history, schedule run history, recent planning/changelog entries, or run/eval/metric evidence shows the cadence is too slow, too fast, stale, or mis-scoped.
7. Preserve a good existing timezone if one is already in use. Otherwise use the workflow's local/current timezone.

RUN SCHEDULE
Create or update a schedule for normal recurring execution with:
- mode="workshop"
- workshop_mode="run"
- valid group_names
- a clear name and description that make it obvious this is the primary recurring run schedule
- a single unattended scheduled message that names the exact group_names and tells Run mode to call run_full_workflow(group_name="<group>") for each configured group. Do not use mode="workflow" for /auto-improve schedules.

The run schedule message must encode:
- Do not ask for confirmation; proceed autonomously.
- Read variables/variables.json only if needed to verify configured group names.
- For each configured group_name, call run_full_workflow(group_name="<group>").
- Use default evaluation behavior so latest run evidence, retained-run history, and metric history are available for the improve schedule.
- Stop after the configured group_names have run and report the run status plainly.

IMPROVE SCHEDULE
Create or update a single optimizer-mode improve schedule with:
- mode="workshop", workshop_mode="optimizer"
- valid group_names
- a clear name and description marking it the IMPROVE schedule (it RECORDS replan proposals only — one schedule; per-run hardening is Pulse's)
- a regular cadence (initial ≈ every 1-2 runs, widening as the workflow stabilizes)
- a single scheduled message that, each fire, runs the `improve-workflow` decision model over the latest evidence and the per-run Bug/Goal findings, then RECORDS what the Goal/strategy evidence warrants.

The improve message must be a short wrapper around `improve-workflow` (the decision model), not a duplicate copy of it. Write the wrapper explicitly into the schedule message; the agent that fires has no other context. Each fire (mechanical, on cadence, whenever there is fresh run evidence to reason about) it:
- reads the evidence: builder/improve.html, soul/soul.md, planning/metrics.json + recent db/metrics_history.jsonl, the latest eval reports and run outputs, and planning/changelog/
- **Bug / reliability findings → already hardened by Pulse:** per-run reliability/contract/artifact, KB/db/report/eval contracts, learning hygiene, and stale-description cleanup are Pulse's job, applied per run. This schedule does NOT apply harden — it only reads those findings as context for the strategy assessment below.
- **Goal / strategy findings → RECORD a replan PROPOSAL, do NOT apply it:** assess whether the current approach is capped on the primary metric/success (executed well yet still short) or whether the evidence reveals a materially better different approach, and write a REPLAN PROPOSAL entry into builder/improve.html as a prose timeline entry (tagged as an open proposal, newest on top) containing: the **evidence** (what is capping the metric / why the current path falls short), the **recommended plan/strategy change** (which steps to add/remove/reorder/redraw, what business work or capability to change, what output artifact or collected evidence to change), the **expected impact** against the success criteria, and the **risk**. Give it a short anchor id (e.g. `id="of-2026-06-07-replan"`) only so a later decision can mark it resolved.
- may record a proposal or nothing in a single fire — if the evidence is too thin to act, log no-action and widen its own cadence. The agent decides from the evidence.

It must NEVER call `replan_workflow_from_results` or rewrite `planning/plan.json` or any step unattended — structural change is always a recorded proposal that a human (or a later explicit `/improve-workflow` / `replan_workflow_from_results`) applies.

OPENING (every improve fire):
- call get_workflow_config and inspect schedules; call get_schedule_runs for the run schedule and the firing improve schedule when deciding whether cadence or group scope needs adjustment
- read builder/improve.html's active sections; read only referenced `builder/improve-archive/YYYY-MM.html` files when older history matters
- read any legacy builder/review.html if present and fold its unresolved findings into builder/improve.html
- read soul/soul.md
- read planning/changelog/ if present and detect material plan/config changes since the last scheduled-improvement review
- read variables/variables.json and confirm the configured group_names are still valid
- carry unresolved Goal/strategy findings into the replan-proposal focus when builder/improve.html names a capped metric, drift, or plan-change concern; unresolved Bug/reliability/KB/learning hygiene findings are Pulse's to harden, not this schedule's
- if group_names, schedule ids, cadence, or recent plan changes indicate schedule drift, prepare a concise cadence note before continuing

IMPROVE DELEGATION (every fire):
After the opening check, the improve message must call:
`get_workflow_command_guidance(kind="improve-workflow", focus="<improve focus>")`
Then follow the returned `improve-workflow` instructions verbatim. Do not inline or paraphrase the `improve-workflow` decision model in the schedule message.

The focus string must include:
- this is a scheduled IMPROVE fire — replan-proposal only: RECORD a replan PROPOSAL (do NOT apply structural change) from the Goal/strategy evidence. Do NOT apply harden — per-run Bug/reliability/contract/artifact, KB/db/report/eval contract, learning-hygiene and stale-description fixes are Pulse's job, applied per run; read them only as context for the strategy assessment
- the configured group_names
- any unresolved open findings or recent planning/changelog concern found during opening
- any KB/learnings/eval/report hygiene concern found during opening
- any cadence note that affects evidence freshness{{if .Focus}}
- user focus: {{.Focus}}{{end}}

SCHEDULE SELF-TUNING RULES (evidence-based backoff):
- If the run schedule is too infrequent to produce evidence for metrics and eval, update the run cadence or log the blocker when changing cadence would be risky.
- IMPROVE cadence: start ≈ every 1-2 runs. When recent fires increasingly surface no materially new strategy proposal (and metrics stay healthy), WIDEN the interval step by step (1-2 runs → 3-4 → weekly). When a material plan/config change lands (planning/changelog) or a regression appears, TIGHTEN back toward every 1-2 runs for 24-48h or until one or two post-change runs are reviewed.
- If group_names drift from variables/variables.json or from the intended measurement surface, update both schedules (run + improve) to the correct explicit group_names.
- Never create duplicate run/improve schedules when an existing schedule can be updated.

POST-FIRE CADENCE CHECK (every improve fire):
After the improve pass finishes, do one short schedule check:
- if the fire surfaced a new replan proposal, keep or tighten cadence per the backoff rules
- if no action was taken because there was no fresh evidence, widen cadence only when repeated recent schedule runs show no useful observation
- append the schedule decision to builder/improve.html if it was not already recorded

PERSISTENT IMPROVEMENT LOG
Create or update builder/improve.html now as the durable optimization ledger entry point for future scheduled improvement runs.
Bootstrap it with:
- objective and success criteria snapshot
- current schedule strategy
- run cadence
- improve cadence
- current known workflow gaps
- current known eval/metric gaps
- current known KB/learnings hygiene gaps
- open replan proposals (PROPOSAL entries awaiting a human decision)
- next improvement hypotheses

If builder/improve.html is already long, compact it while preserving the ledger:
- keep the Workflow Profile, all open findings, open replan proposals, and the latest ~10-20 timeline entries in builder/improve.html
- move older resolved/no-action/repeated detailed entries to `builder/improve-archive/YYYY-MM.html`
- leave Archive Index rows naming date range, entry count, unresolved ids, and summary
- never archive away unresolved findings, open replan proposals, active hypotheses, current schedule strategy, current metric/eval gaps, or the latest semantic plan/eval/metric change

SCHEDULE CREATION RULES
1. Do NOT delete schedules unless they are clearly redundant and safe to remove. Prefer update over delete.
2. If an existing run schedule already serves the purpose, keep it and refine it if needed. For /auto-improve, convert/update direct mode="workflow" run schedules to mode="workshop", workshop_mode="run" rather than leaving them as direct workflow schedules.
3. If an existing optimizer/improve schedule already serves harden or replan-proposal, keep it and refine it into the single IMPROVE schedule — now the **replan-proposal** schedule (harden findings are Pulse's). If there are TWO legacy optimizer schedules (a separate harden and a separate replan-proposal), consolidate them into one improve (replan-proposal) schedule rather than maintaining both.
4. Use create_schedule / update_schedule as appropriate.

FINAL REPORT
Summarize:
- what schedules already existed
- what you created vs updated
- run schedule: ID, name, cadence, timezone, groups, mode, workshop_mode
- improve schedule: ID, name, cadence, timezone, groups
- the exact Run-mode schedule message you configured
- the exact improve-schedule message you configured
- where you saved builder/improve.html
