Set up automatic run + improve scheduling for this workflow.

Before writing builder/improve.html, call get_reference_doc(kind="html-output") to load the HTML style guide. Write a self-contained HTML file — not Markdown.

MIGRATION (one-time): Check whether builder/improve.md exists. If it does, read it, extract all unresolved entries and structured blocks, incorporate them into builder/improve.html, then delete builder/improve.md with execute_shell_command. Also check for builder/review.md and migrate it to builder/review.html the same way.

FIRST check what already exists before proposing or creating anything. Do this autonomously and avoid duplicate schedules.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

GOAL
Create or update THREE complementary schedules:
1. a workshop Run-mode schedule for recurring execution
2. a workshop Optimizer-mode HARDEN schedule that wakes often — initially after every 1-2 runs — and APPLIES a hardening pass (local reliability/contract/artifact fixes + stale-description cleanup) off the latest run/eval evidence, widening its own interval as the workflow stabilizes
3. a workshop Optimizer-mode REPLAN-PROPOSAL schedule that wakes less often — initially after every 3-4 runs — and does NOT rewrite the plan: it reviews accumulated evidence, decides what the plan/strategy SHOULD change, and records that as a proposal in builder/improve.html for human review, widening its own interval as the workflow matures.

Harden is always the more frequent of the two improvement schedules; the replan-proposal schedule never fires more often than harden. The harden schedule APPLIES fixes; the replan-proposal schedule only RECOMMENDS — it never calls `replan_workflow_from_results` and never rewrites `planning/plan.json`.

DISCOVERY
1. Call get_workflow_config and inspect the current schedule list.
2. If existing candidate schedules exist, call get_schedule_runs on the most relevant ones to understand whether they are active, useful, stale, too frequent, or missing coverage.
3. Read soul/soul.md to understand objective and success criteria.
4. Read variables/variables.json to identify valid group names and enabled groups.
5. Read builder/improve.html's active sections. This is mandatory: it contains the Workflow Profile, Active Improvement Index, Archive Index, Recent Entries, prior actions, deferred ideas, open replan proposals, and prior scheduled-improvement history. If the file has no retention/index structure yet, read it in full. Read only archive files referenced by unresolved ids, current focus, schedule drift, or the selected evidence window.
6. Read builder/review.html if present. Carry unresolved `F-...` findings into the scheduled harden message.
7. Read planning/metrics.json and recent db/metrics_history.jsonl rows. Metrics are evidence for harden and for replan proposals; they do not create a separate action path.
8. Read planning/changelog/ if present and compare recent plan/config changes against builder/improve.html and builder/review.html. Recent plan changes increase regression risk and require tighter harden cadence until one or two post-change runs have been reviewed.
9. **Success must be defined before scheduling — check it FIRST and bootstrap it if missing.** auto-improve cannot optimize toward an undefined goal, so never set up an optimizer schedule for a workflow with no success definition. If builder/improve.html has no "## Workflow Profile" section — or the profile declares business-context accumulation or a frozen/ratchet plan and planning/metrics.json is empty — do NOT skip ahead and do NOT just tell the user to "run /define-success" and stop. Instead, run the define-success bootstrap **inline now**: call `get_workflow_command_guidance(kind="define-success")` and follow it to completion with the user (establish the Workflow Profile + metrics), then resume these steps and continue to scheduling. Only proceed directly to the schedule steps when a Workflow Profile already exists and metrics are defined.

SCHEDULE STRATEGY
1. Prefer updating or reusing good existing schedules instead of creating duplicates.
2. Only create a new schedule when no existing schedule already serves the purpose.
3. The HARDEN schedule is frequent: initial cadence ≈ every 1-2 runs (fire shortly after a run or two so it hardens off fresh evidence), widening as the workflow stabilizes. The REPLAN-PROPOSAL schedule is less frequent: initial cadence ≈ every 3-4 runs, widening further as the workflow matures. Harden never fires less often than the replan-proposal.
4. If cadence is not obvious:
   - choose a practical recurring run cadence based on the workflow objective and existing schedules
   - start harden ≈ every 1-2 runs and the replan-proposal ≈ every 3-4 runs; for unknown active workflows base the run cadence at every 6-12 hours, not weekly
5. If planning/changelog shows material plan/config changes since the last builder/improve.html entry or unresolved builder/review.html finding, tighten the harden schedule for the next 24-48 hours or until the next one or two post-change runs have been reviewed.
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
- Use default evaluation behavior so latest run evidence, retained-run history, and metric history are available for the harden and replan-proposal schedules.
- Stop after the configured group_names have run and report the run status plainly.

HARDEN SCHEDULE
Create or update the frequent hardening schedule with:
- mode="workshop", workshop_mode="optimizer"
- valid group_names
- a clear name and description marking it the frequent HARDEN schedule
- initial cadence ≈ every 1-2 runs
- a single scheduled message that APPLIES a hardening pass off the latest run/eval evidence: improve local reliability, validation, artifact shape, KB/db/report/eval contracts, learning hygiene, and stale-description cleanup. It does NOT redesign strategy — the replan-proposal schedule owns path redesign.

The harden message must be a short wrapper around `improve-workflow` (steered to harden via focus), not a duplicate copy of the improve decision model. Write the wrapper explicitly into the schedule message; the agent that fires has no other context.

REPLAN-PROPOSAL SCHEDULE
Create or update the less-frequent replan-proposal schedule with:
- mode="workshop", workshop_mode="optimizer"
- valid group_names
- a clear name and description marking it the REPLAN-PROPOSAL schedule
- initial cadence ≈ every 3-4 runs
- a single scheduled message that REVIEWS accumulated evidence and decides what the workflow's strategy/path SHOULD change to reach the success criteria — and WRITES that as a proposal, WITHOUT applying it.

The replan-proposal message must NOT call `replan_workflow_from_results` and must NOT rewrite `planning/plan.json` or any step. Each fire (mechanical, on cadence, whenever there is fresh run evidence to reason about) it:
- reads the evidence: builder/improve.html, builder/review.html, soul/soul.md, planning/metrics.json + recent db/metrics_history.jsonl, the latest eval reports and run outputs, and planning/changelog/
- assesses whether the current approach is capped on the primary metric/success (executed well yet still short), or whether the evidence reveals a materially better different approach
- writes a REPLAN PROPOSAL entry into builder/improve.html containing: the **evidence** (what is capping the metric / why the current path falls short), the **recommended plan/strategy change** (which steps to add/remove/reorder/redraw, what business work or capability to change, what output artifact or collected evidence to change), the **expected impact** against the success criteria, and the **risk**. Tag it `PROPOSAL` with a stable `F-YYYY-MM-DD-NNN` id.
- does NOT apply anything — a human, or a later explicit `/improve-workflow` or `replan_workflow_from_results`, decides and applies.
- if the evidence is too thin to propose anything useful, logs no-action and widens its own cadence.

OPENING (every improve fire — harden or replan-proposal):
- call get_workflow_config and inspect schedules; call get_schedule_runs for the run schedule and the firing improve schedule when deciding whether cadence or group scope needs adjustment
- read builder/improve.html's active sections; read only referenced `builder/improve-archive/YYYY-MM.html` files when older history matters
- read builder/review.html if present
- read soul/soul.md
- read planning/changelog/ if present and detect material plan/config changes since the last scheduled-improvement review
- read variables/variables.json and confirm the configured group_names are still valid
- carry unresolved KB and learning findings into the harden focus when builder/review.html or builder/improve.html names stale `knowledgebase/notes/`, `learnings/_global/`, saved scripts, or plan-change drift
- if group_names, schedule ids, cadence, or recent plan changes indicate schedule drift, prepare a concise cadence note before continuing

HARDEN DELEGATION (harden fire only):
After the opening check, the harden message must call:
`get_workflow_command_guidance(kind="improve-workflow", focus="<harden focus>")`
Then follow the returned `improve-workflow` instructions verbatim. Do not inline or paraphrase the `improve-workflow` decision model in the schedule message.

The focus string must include:
- this is a scheduled HARDEN fire — apply local reliability/contract/artifact + stale-description fixes off the latest run/eval evidence; do NOT replan (the replan-proposal schedule owns strategy)
- the configured group_names
- any unresolved `F-...` findings or recent planning/changelog concern found during opening
- any KB/learnings hygiene concern found during opening
- any cadence note that affects evidence freshness{{if .Focus}}
- user focus: {{.Focus}}{{end}}

SCHEDULE SELF-TUNING RULES (evidence-based backoff):
- If the run schedule is too infrequent to produce evidence for metrics and eval, update the run cadence or log the blocker when changing cadence would be risky.
- HARDEN cadence: start ≈ every 1-2 runs. When recent harden fires increasingly find nothing to fix and metrics stay healthy, WIDEN the interval step by step (1-2 runs → 3-4 → weekly). When a material plan/config change lands (planning/changelog) or a regression appears, TIGHTEN back toward every 1-2 runs for 24-48h or until one or two post-change runs are reviewed.
- REPLAN-PROPOSAL cadence: start ≈ every 3-4 runs. When recent proposals stop surfacing materially new strategy changes (repeated "no new proposal", or the same unresolved proposal still pending), WIDEN the interval further (3-4 runs → 6 → 8+). Tighten again only on a major objective/success change or a sustained metric regression that hardening cannot close.
- Never let the replan-proposal fire more often than harden; harden is always the more frequent of the two.
- If group_names drift from variables/variables.json or from the intended measurement surface, update all three schedules to the correct explicit group_names.
- Never create duplicate run/harden/replan-proposal schedules when an existing schedule can be updated.

POST-FIRE CADENCE CHECK (every improve fire):
After the harden pass or the replan-proposal write finishes, do one short schedule check:
- if a harden fire changed the workflow, or a replan-proposal fire surfaced a new proposal, keep or tighten cadence per the backoff rules
- if no action was taken because there was no fresh evidence, widen cadence only when repeated recent schedule runs show no useful observation
- append the schedule decision to builder/improve.html if it was not already recorded

PERSISTENT IMPROVEMENT LOG
Create or update builder/improve.html now as the durable optimization ledger entry point for future scheduled improvement runs.
Bootstrap it with:
- objective and success criteria snapshot
- current schedule strategy
- run cadence
- harden cadence
- replan-proposal cadence
- current known workflow gaps
- current known eval/metric gaps
- current known KB/learnings hygiene gaps
- open replan proposals (PROPOSAL entries awaiting a human decision)
- next improvement hypotheses

If builder/improve.html is already long, compact it while preserving the ledger:
- keep Workflow Profile, Active Improvement Index, Archive Index, open replan proposals, and latest 10-20 detailed entries in builder/improve.html
- move older resolved/no-action/repeated detailed entries to `builder/improve-archive/YYYY-MM.html`
- leave Archive Index rows naming date range, entry count, unresolved ids, and summary
- never archive away unresolved findings, open replan proposals, active hypotheses, current schedule strategy, current metric/eval gaps, or the latest semantic plan/eval/metric change

SCHEDULE CREATION RULES
1. Do NOT delete schedules unless they are clearly redundant and safe to remove. Prefer update over delete.
2. If an existing run schedule already serves the purpose, keep it and refine it if needed. For /auto-improve, convert/update direct mode="workflow" run schedules to mode="workshop", workshop_mode="run" rather than leaving them as direct workflow schedules.
3. If an existing optimizer/improve schedule already serves harden or replan-proposal, keep it and refine it. If a single legacy optimizer schedule does both, split it into a frequent harden schedule and a less-frequent replan-proposal schedule.
4. Use create_schedule / update_schedule as appropriate.

FINAL REPORT
Summarize:
- what schedules already existed
- what you created vs updated
- run schedule: ID, name, cadence, timezone, groups, mode, workshop_mode
- harden schedule: ID, name, cadence, timezone, groups
- replan-proposal schedule: ID, name, cadence, timezone, groups
- the exact Run-mode schedule message you configured
- the exact harden-schedule message you configured
- the exact replan-proposal-schedule message you configured
- where you saved builder/improve.html
