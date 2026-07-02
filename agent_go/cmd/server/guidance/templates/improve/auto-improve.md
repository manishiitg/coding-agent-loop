Set up automatic run + improve scheduling for this workflow.

Write to `builder/improve.html` - the single durable log. For the log/HTML format, one-time migration from legacy review files, and close-out rules, follow `get_reference_doc(kind="review-improve-log")` and `get_reference_doc(kind="html-output")`.

FIRST check what already exists before proposing or creating anything. Do this autonomously and avoid duplicate schedules.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

Auto-improve runs over the one workflow log (`builder/improve.html`) with the same Bug/Goal vocabulary:
- Pulse is the per-run detect + harden + report pass. It detects run-specific Bugs, applies reversible plan-step-scoped hardening, runs a separate report-only Artifact Review item for pending plan-change drift, and reports LLM/cost/time.
- The scheduled improve pass reads the same log plus cross-run Goal/strategy evidence. It applies a structural replan only when the evidence is strong, refreshes stale evidence-chain artifacts across reports, learnings, KB, and db, and performs an expert-advisor scan for out-of-plan opportunities that could materially advance the goal. Advisor ideas are proposal-only unless they independently clear the normal replan bar.

GOAL
Set up the per-run review and the complementary schedules:
0. Turn Pulse on: call `update_workflow_config(post_run_monitor=true)`. From then on, after every run Pulse backs up, records Bug/Goal findings in the log, applies the full plan-step harden for Bug findings, runs a separate report-only Artifact Review item, and records LLM/cost/time. It never does a structural rewrite; see `get_reference_doc(kind="post-run-monitor")`.
1. A workshop Run-mode schedule for recurring execution.
2. A workshop Optimizer-mode IMPROVE schedule. Each fire reads the log, run/eval evidence, and planning changelog, runs the expert-advisor scan for credible out-of-plan ideas, then follows `get_workflow_command_guidance(kind="improve-workflow")`.

The safety rail is evidence + back-up-first. Every applied replan and curation MUST cite the run/eval evidence that justifies it. Back up before applying. The replan bar is high: strong cross-run evidence only. Per-run Bug/reliability findings remain Pulse's to harden. Plan-change artifact drift is Pulse's separate report-only Artifact Review item, not part of harden.

DISCOVERY
1. Call `get_workflow_config` and inspect the current schedule list.
2. If existing candidate schedules exist, call `get_schedule_runs` on the most relevant ones.
3. Read `soul/soul.md` to understand objective and success criteria.
4. Read `variables/variables.json` to identify valid group names and enabled groups.
5. Read `builder/improve.html`. It contains the Workflow Profile, recent timeline entries, open findings, prior decisions, held replans, and prior scheduled-improvement history. Read archive files only when an open finding, current focus, schedule drift, or selected evidence window points into one.
6. Read `builder/improve.html` for unresolved findings and prior schedule/improvement history.
7. Read `planning/changelog/` if present and compare recent plan/config changes against the Artifact Sync Cursor and recent Artifact Review entries in `builder/improve.html`. Recent plan changes increase regression risk and require Pulse's separate Artifact Review item plus a tighter improve cadence until one or two post-change runs have been reviewed.
8. Success must be defined before scheduling. If `builder/improve.html` has no Workflow Profile block, call `get_workflow_command_guidance(kind="define-success")` and follow it to establish the Workflow Profile, then resume scheduling.

SCHEDULE STRATEGY
1. Prefer updating or reusing good existing schedules instead of creating duplicates.
2. Only create a new schedule when no existing schedule already serves the purpose.
3. The IMPROVE schedule fires on a regular cadence: initial roughly every 1-2 runs, widening as the workflow stabilizes. It applies a structural replan only when cross-run evidence is strong, refreshes stale evidence-chain artifacts per fire, and notifies only on decision-worthy changes.
4. If cadence is not obvious, choose a practical recurring run cadence from the workflow objective and start the improve schedule roughly every 1-2 runs. For unknown active workflows, base the run cadence at every 6-12 hours, not weekly.
5. If `planning/changelog/` shows material plan/config changes since the last log entry or unresolved open finding, tighten the improve schedule for 24-48 hours or until the next one or two post-change runs have been reviewed.
6. Each scheduled improve fire may review cadence and use `update_schedule` when history, schedule runs, changelog entries, or run/eval evidence shows the cadence is too slow, too fast, stale, or mis-scoped.
7. Preserve a good existing timezone if one is already in use. Otherwise use the workflow's local/current timezone.

RUN SCHEDULE
Create or update a schedule for normal recurring execution with:
- `mode="workshop"`
- `workshop_mode="run"`
- valid `group_names`
- a clear name and description marking it as the primary recurring run schedule
- a single unattended scheduled message that names the exact groups and tells Run mode to call `run_full_workflow(group_name="<group>")` for each configured group

The run schedule message must encode:
- Do not ask for confirmation; proceed autonomously.
- Read `variables/variables.json` only if needed to verify configured group names.
- For each configured group, call `run_full_workflow(group_name="<group>")`.
- Use default evaluation behavior so latest run evidence and retained-run history are available for the improve schedule.
- Stop after the configured groups have run and report status plainly.

IMPROVE SCHEDULE
Create or update a single optimizer-mode improve schedule with:
- `mode="workshop"`, `workshop_mode="optimizer"`
- valid `group_names`
- a clear name and description marking it as the IMPROVE schedule
- a regular cadence, initially roughly every 1-2 runs and widening as the workflow stabilizes
- no custom scheduled message. Omit `messages` entirely for auto-improve; the scheduler owns the canonical improve prompt and ignores persisted optimizer messages.

The scheduler injects the runtime sequence:
STEP 1/5 pre-backup -> STEP 2/5 canonical improve prompt -> STEP 3/5 final backup -> STEP 4/5 publish -> STEP 5/5 notify.

Each fire:
- reads evidence: `builder/improve.html`, `soul/soul.md`, latest eval reports and run outputs, and `planning/changelog/`
- leaves immediate Bug/reliability findings to Pulse/harden, and plan-change artifact drift to Pulse's separate Artifact Review item
- applies structural replan only when cross-run Goal/strategy evidence is strong
- applies bounded evidence-chain freshness cleanup for reports, learnings, KB, and db when they are stale or misleading
- proposes high-leverage out-of-plan advisor ideas when the current plan is missing an opportunity, but logs them as `Advisor idea` entries instead of applying them unattended
- logs no-action and widens cadence when evidence is too thin
- does not call `notify_user`; the scheduler-injected final notify turn owns notification

OPENING (every improve fire)
- call `get_workflow_config` and inspect schedules
- call `get_schedule_runs` for the run schedule and firing improve schedule when cadence or group scope may need adjustment
- read `builder/improve.html` active sections and referenced archive files only when older history matters
- read `builder/improve.html` for unresolved findings and prior schedule/improvement history
- read `soul/soul.md`
- read `planning/changelog/`
- read `variables/variables.json`
- carry unresolved Goal/strategy findings into the replan focus; unresolved Bug/reliability findings are Pulse's to harden; unresolved Artifact Review findings are report findings until Builder/Optimizer chooses a fix path

IMPROVE DELEGATION (runtime STEP 2/5)
After the opening check, call:
`get_workflow_command_guidance(kind="improve-workflow", focus="<improve focus>")`

Then follow the returned `improve-workflow` instructions verbatim. Do not inline or paraphrase the decision model in the schedule message.

The focus string must include:
- this is a scheduled IMPROVE fire; apply a structural replan only when cross-run Goal/strategy evidence is strong, cite the runs/evals that justify it, and record findings instead of changing the plan when evidence is thin
- run the expert-advisor scan for out-of-plan opportunities the current plan misses; log credible proposal-only ideas with the `Advisor idea` label and do not auto-apply them unless the normal replan bar is met
- apply evidence-chain freshness cleanup when persisted artifacts are stale or misleading across runs: report accuracy/live-data fixes, learning cleanup, KB cleanup, and DB contract cleanup
- honor a more cautious `oversight_mode` by keeping replan propose-only
- do NOT call `harden_workflow`; immediate per-run Bugs are Pulse's job
- do NOT fold Artifact Review into harden; Pulse runs it as its own report-only item
- do NOT call `notify_user` in runtime STEP 2/5 because STEP 5/5 is the dedicated notify turn
- the configured `group_names`
- any unresolved open findings or recent changelog concern found during opening
- any KB/learnings/eval/report hygiene concern found during opening
- any cadence note that affects evidence freshness{{if .Focus}}
- user focus: {{.Focus}}{{end}}

SCHEDULE SELF-TUNING RULES
- If the run schedule is too infrequent to produce useful run/eval evidence, update cadence or log the blocker when changing cadence would be risky.
- IMPROVE cadence: start roughly every 1-2 runs. When recent fires increasingly surface no materially new strategy change and no stale evidence-chain artifacts to refresh, widen the interval step by step. When a material plan/config change lands or a regression appears, tighten back toward every 1-2 runs for 24-48 hours or until one or two post-change runs have been reviewed.
- If group names drift from `variables/variables.json`, update both schedules.
- Never create duplicate run/improve schedules when an existing schedule can be updated.

PERSISTENT IMPROVEMENT LOG
Create or update `builder/improve.html` now as the durable optimization ledger entry point.
Bootstrap it with:
- objective and success criteria snapshot
- current schedule strategy
- run cadence
- improve cadence
- current known workflow gaps
- current known eval/report/KB/learnings/db gaps
- current known advisor opportunities or out-of-plan ideas, if any
