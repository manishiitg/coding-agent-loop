Set up automatic run + improve scheduling for this workflow.

Write to `builder/improve.html` — the single durable log. For the log/HTML format, the one-time migration (folding any legacy `builder/review.html` findings in), and how entries are recorded and closed out, follow `get_reference_doc(kind="review-improve-log")` (and `get_reference_doc(kind="html-output")` for HTML style).

FIRST check what already exists before proposing or creating anything. Do this autonomously and avoid duplicate schedules.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Auto-improve runs over the **one** workflow log (`builder/improve.html`), all sharing the Bug/Goal vocabulary: a per-run **Pulse** pass that detects every run AND **hardens** the Bug findings it finds, feeding **one scheduled improve pass** that reads the same log and the cross-run Goal/strategy evidence and **APPLIES** what the evidence warrants — a structural **replan** when cross-run evidence is strong (back up first; high bar; cite the evidence), plus holistic **corpus-freshness** curation of aging learnings/KB/db. Per-run hardening is Pulse's job; the scheduled loop owns structural replan and corpus freshness. They are the same system over the same log, not separate tools.

GOAL
Set up the per-run review **and** the complementary schedules:
0. **Pulse (per-run detect + harden, every run):** turn the per-run monitor ON — call `update_workflow_config(post_run_monitor=true)`. From then on, after every run Pulse backs up, records Bug/Goal findings in the log, and **applies the full plan-step harden** for the Bug findings (every reversible, plan-step-scoped reliability/contract fix). It never does a structural rewrite; see `get_reference_doc(kind="post-run-monitor")`. Tell the user in plain terms it's on (or that they can toggle it from the **Monitor** control in the toolbar).
1. a workshop Run-mode schedule for recurring execution
2. a workshop Optimizer-mode **IMPROVE** schedule that wakes on a regular cadence (initially after every 1-2 runs, widening as the workflow stabilizes) and, **each fire, reads the log + cross-run Goal/strategy evidence and APPLIES what the evidence warrants** — it runs the `improve-workflow` decision model over the latest run/eval evidence and the per-run Bug/Goal findings, then:
   - **Structural replan → APPLY when cross-run evidence is STRONG** — a primary metric clearly capped, repeated failure of the current approach across runs, or clear evidence of a materially better approach. Back up FIRST, then apply (it MAY call `replan_workflow_from_results` / rewrite `planning/plan.json`). The bar is HIGH: do NOT replan on thin or single-run evidence — when evidence is thin, record a finding and widen cadence instead.
   - **Corpus freshness → APPLY.** Review the whole corpus across runs for staleness and consolidate/prune aging **learnings** (`learnings/_global/SKILL.md`), **KB** (`knowledgebase/notes/`), and **db** — via `organize_global_learnings`, `consolidate_knowledgebase`, `improve_kb`, `improve_db`. Reversible curation.
   - It may apply a change or do nothing (no-action + widen cadence) in a single fire. The agent picks from the evidence.

The safety rail is **evidence + back-up-first**, not "never apply": every applied replan and every curation MUST cite the run/eval/metric evidence that justifies it — no speculative changes, if you can't point to evidence don't change it. Back up before applying. The replan bar is HIGH (strong cross-run evidence only). **oversight_mode is the user override:** a workflow set to a more cautious oversight can keep replan as propose-only; the DEFAULT is apply-when-evidence-strong. Per-run Bug/reliability findings remain Pulse's to harden.

DISCOVERY
1. Call get_workflow_config and inspect the current schedule list.
2. If existing candidate schedules exist, call get_schedule_runs on the most relevant ones to understand whether they are active, useful, stale, too frequent, or missing coverage.
3. Read soul/soul.md to understand objective and success criteria.
4. Read variables/variables.json to identify valid group names and enabled groups.
5. Read builder/improve.html. This is mandatory: it contains the Workflow Profile, recent timeline entries, open findings, prior decisions, open strategy findings / held replans, and prior scheduled-improvement history. If it's short, read it in full. Read archive files only when an open finding, the current focus, schedule drift, or the selected evidence window points into one.
6. Read any legacy builder/review.html if present and fold its unresolved findings into builder/improve.html. Carry unresolved open **Goal/strategy** findings into the scheduled improve (replan) message; unresolved Bug findings are Pulse's to harden.
7. Read planning/metrics.json and recent db/metrics_history.jsonl rows. Metrics are evidence for the replan decision (and for Pulse's harden); they do not create a separate action path.
8. Read planning/changelog/ if present and compare recent plan/config changes against builder/improve.html. Recent plan changes increase regression risk and require a tighter improve cadence until one or two post-change runs have been reviewed.
9. **Success must be defined before scheduling — check it FIRST and bootstrap it if missing.** auto-improve cannot optimize toward an undefined goal, so never set up an optimizer schedule for a workflow with no success definition. If builder/improve.html has no Workflow Profile block — or the profile declares business-context accumulation or a frozen/ratchet plan and planning/metrics.json is empty — do NOT skip ahead and do NOT just tell the user to "run /define-success" and stop. Instead, run the define-success bootstrap **inline now**: call `get_workflow_command_guidance(kind="define-success")` and follow it to completion with the user (establish the Workflow Profile + metrics), then resume these steps and continue to scheduling. Only proceed directly to the schedule steps when a Workflow Profile already exists and metrics are defined.

SCHEDULE STRATEGY
1. Prefer updating or reusing good existing schedules instead of creating duplicates.
2. Only create a new schedule when no existing schedule already serves the purpose.
3. The IMPROVE schedule fires on a regular cadence: initial ≈ every 1-2 runs (fire shortly after a run or two so it acts off fresh evidence), widening as the workflow stabilizes. It **applies** a structural replan when cross-run evidence is strong (back up first; high bar; cite evidence) and refreshes aging corpus **per fire** — per-run hardening is Pulse's, not on this schedule.
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
- a clear name and description marking it the IMPROVE schedule (it APPLIES a strong-evidence structural replan and refreshes aging corpus — one schedule; per-run hardening is Pulse's)
- a regular cadence (initial ≈ every 1-2 runs, widening as the workflow stabilizes)
- a single scheduled message that, each fire, runs the `improve-workflow` decision model over the latest evidence and the per-run Bug/Goal findings, then APPLIES what the cross-run Goal/strategy evidence warrants (back up first; cite evidence).

The improve message must be a short wrapper around `improve-workflow` (the decision model), not a duplicate copy of it. Write the wrapper explicitly into the schedule message; the agent that fires has no other context. Each fire (mechanical, on cadence, whenever there is fresh run evidence to reason about) it:
- reads the evidence: builder/improve.html, soul/soul.md, planning/metrics.json + recent db/metrics_history.jsonl, the latest eval reports and run outputs, and planning/changelog/
- **Bug / reliability findings → already hardened by Pulse:** per-run reliability/contract/artifact, KB/db/report/eval contracts, learning hygiene, and stale-description cleanup are Pulse's job, applied per run. This schedule does NOT apply harden — it only reads those findings as context for the strategy assessment below.
- **Goal / strategy findings → APPLY a structural replan when cross-run evidence is STRONG:** assess whether the current approach is capped on the primary metric/success (executed well yet still short across runs), whether the current approach has repeatedly failed across runs, or whether the evidence reveals a materially better different approach. When the cross-run evidence is **strong**, **back up first**, then apply the replan (it MAY call `replan_workflow_from_results` / rewrite `planning/plan.json` / add/remove/reorder steps). Record an applied-replan entry into builder/improve.html as a prose timeline entry (newest on top) containing: the **evidence** (which runs/evals/metrics justify it — cite them; what is capping the metric / why the current path failed), the **change applied** (which steps added/removed/reordered/redrawn, what capability or output artifact changed), the **expected impact** against the success criteria, and the **risk**. Give it a short anchor id (e.g. `id="of-2026-06-07-replan"`). When evidence is **thin or single-run**, do NOT replan — record the finding as an open proposal and widen cadence instead. Every applied replan MUST cite the evidence that justifies it — no speculative changes. After applying, run `get_reference_doc(kind="plan-change-impact")` and reconcile the blast radius (downstream steps, evals, report dashboard, db, learnings, KB) before closing the fire — a structural replan ripples widest. (If `oversight_mode` is set more cautiously, keep replan propose-only and record instead of applying.)
- **Corpus freshness → APPLY (reversible curation):** review the whole corpus across runs for staleness — superseded or contradicted notes, stale selectors, duplicates, bloat — and consolidate/prune aging **learnings** (`learnings/_global/SKILL.md` via `organize_global_learnings`), **KB** (`knowledgebase/notes/` via `consolidate_knowledgebase` / `improve_kb`), and **db** (via `improve_db`). Cite the runs/notes that show the staleness; this curation is reversible (you backed up first).
- may apply a change or nothing in a single fire — if the evidence is too thin to act, log no-action and widen its own cadence. The agent decides from the evidence.

The safety rail is **evidence + back-up-first**, not "never apply": it applies a structural replan only on **strong cross-run evidence** and always backs up first; every applied replan and curation cites its evidence. When `oversight_mode` is more cautious it keeps replan as a recorded proposal a human applies; the DEFAULT is apply-when-evidence-strong.

OPENING (every improve fire):
- call get_workflow_config and inspect schedules; call get_schedule_runs for the run schedule and the firing improve schedule when deciding whether cadence or group scope needs adjustment
- read builder/improve.html's active sections; read only referenced `builder/improve-archive/YYYY-MM.html` files when older history matters
- read any legacy builder/review.html if present and fold its unresolved findings into builder/improve.html
- read soul/soul.md
- read planning/changelog/ if present and detect material plan/config changes since the last scheduled-improvement review
- read variables/variables.json and confirm the configured group_names are still valid
- carry unresolved Goal/strategy findings into the replan focus when builder/improve.html names a capped metric, drift, or plan-change concern; unresolved Bug/reliability findings are Pulse's to harden, not this schedule's
- if group_names, schedule ids, cadence, or recent plan changes indicate schedule drift, prepare a concise cadence note before continuing

IMPROVE DELEGATION (every fire):
After the opening check, the improve message must call:
`get_workflow_command_guidance(kind="improve-workflow", focus="<improve focus>")`
Then follow the returned `improve-workflow` instructions verbatim. Do not inline or paraphrase the `improve-workflow` decision model in the schedule message.

The focus string must include:
- this is a scheduled IMPROVE fire — APPLY when evidence is strong: back up first, then apply a structural replan from the cross-run Goal/strategy evidence when that evidence is STRONG (a clearly capped primary metric, repeated cross-run failure, or a materially better approach), citing the runs/evals/metrics that justify it; on thin/single-run evidence record a finding and widen cadence instead. Also APPLY corpus-freshness curation — consolidate/prune aging learnings/KB/db (`organize_global_learnings`, `consolidate_knowledgebase`, `improve_kb`, `improve_db`). Honor a more cautious `oversight_mode` by keeping replan propose-only. Do NOT apply harden — per-run Bug/reliability/contract/artifact, KB/db/report/eval contract, learning-hygiene and stale-description fixes are Pulse's job, applied per run; read them only as context for the strategy assessment
- the configured group_names
- any unresolved open findings or recent planning/changelog concern found during opening
- any KB/learnings/eval/report hygiene concern found during opening
- any cadence note that affects evidence freshness{{if .Focus}}
- user focus: {{.Focus}}{{end}}

SCHEDULE SELF-TUNING RULES (evidence-based backoff):
- If the run schedule is too infrequent to produce evidence for metrics and eval, update the run cadence or log the blocker when changing cadence would be risky.
- IMPROVE cadence: start ≈ every 1-2 runs. When recent fires increasingly surface no materially new strategy change and no stale corpus to refresh (and metrics stay healthy), WIDEN the interval step by step (1-2 runs → 3-4 → weekly). When a material plan/config change lands (planning/changelog) or a regression appears, TIGHTEN back toward every 1-2 runs for 24-48h or until one or two post-change runs are reviewed.
- If group_names drift from variables/variables.json or from the intended measurement surface, update both schedules (run + improve) to the correct explicit group_names.
- Never create duplicate run/improve schedules when an existing schedule can be updated.

POST-FIRE CADENCE CHECK (every improve fire):
After the improve pass finishes, do one short schedule check:
- if the fire applied a replan (or surfaced a new strategy concern), keep or tighten cadence per the backoff rules
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
- open strategy findings (any replan held for a cautious oversight_mode, awaiting a human decision)
- next improvement hypotheses

If builder/improve.html is already long, compact it while preserving the ledger:
- keep the Workflow Profile, all open findings, open strategy findings / held replans, and the latest ~10-20 timeline entries in builder/improve.html
- move older resolved/no-action/repeated detailed entries to `builder/improve-archive/YYYY-MM.html`
- leave Archive Index rows naming date range, entry count, unresolved ids, and summary
- never archive away unresolved findings, open strategy findings / held replans, active hypotheses, current schedule strategy, current metric/eval gaps, or the latest semantic plan/eval/metric change

SCHEDULE CREATION RULES
1. Do NOT delete schedules unless they are clearly redundant and safe to remove. Prefer update over delete.
2. If an existing run schedule already serves the purpose, keep it and refine it if needed. For /auto-improve, convert/update direct mode="workflow" run schedules to mode="workshop", workshop_mode="run" rather than leaving them as direct workflow schedules.
3. If an existing optimizer/improve schedule already serves harden or replan, keep it and refine it into the single IMPROVE schedule — now the **replan + corpus-freshness** schedule (harden findings are Pulse's). If there are TWO legacy optimizer schedules (a separate harden and a separate replan), consolidate them into one improve schedule rather than maintaining both.
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
