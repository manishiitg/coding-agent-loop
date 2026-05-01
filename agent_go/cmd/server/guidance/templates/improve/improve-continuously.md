Set up automatic run + improve scheduling for this workflow. FIRST check what already exists before proposing or creating anything. Do this autonomously and avoid creating duplicate schedules.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

GOAL
Create or update TWO complementary schedules:
1. a normal workflow run schedule for recurring execution
2. a lightweight optimizer/workshop schedule that wakes frequently, chooses the right improvement action, and can adjust schedules when evidence shows the cadence is wrong

DISCOVERY
1. Call get_workflow_config and inspect the current schedule list carefully before doing anything else.
2. If there are existing candidate schedules, use get_schedule_runs on the most relevant ones to understand whether they are active, useful, stale, too frequent, or missing coverage.
3. Read soul/soul.md to understand the objective and success criteria.
4. Read variables/variables.json to identify valid group names and enabled groups.
5. **Prior improvement context.** Read builder/improve.md in full. This is mandatory: it contains the Workflow Profile, previous hypotheses, deferred ideas, and prior scheduled-improvement history.
6. **Prior review context.** Read builder/review.md if present. Carry unresolved `F-...` findings into the scheduled optimizer message so future fires know which known issues should be closed or linked.
7. **Framework precheck.** If builder/improve.md has no "## Workflow Profile" section, stop and redirect: "Run /improve-setup-framework first." A continuous-improvement schedule with no profile and no metrics will optimize nothing concrete. If the profile declares business-context accumulation or a frozen/ratchet plan and planning/metrics.json is empty, also redirect.
8. **Framework mode.** Read planning/metrics.json. If it has at least one entry, the scheduled improve runs will operate in EXPERIMENT MODE — open at most one experiment per fire, gated through propose_experiment. If empty, scheduled improve runs are in DIRECT MODE — use the decision model to optimize, harden, or replan directly. Note this in the schedule's name/description so the operator knows which mode the schedule is using.
9. Read experiments/config.json (if it exists) to find default_measurement_runs / target_runs — needed to size the improve cadence correctly (see SCHEDULE STRATEGY below).

SCHEDULE STRATEGY
1. Prefer updating or reusing good existing schedules instead of creating duplicates.
2. Only create a new schedule when there is no existing schedule that already serves that purpose.
3. Prefer a **frequent lightweight improve schedule**, not a once-a-week batch. If the workflow runs hourly or daily, schedule the optimizer at least daily; if the workflow runs weekly, schedule the optimizer shortly after each run.
4. **Experiment cadence guard (EXPERIMENT MODE only).** The guard limits **new experiment creation**, not cron wakeup frequency. Frequent optimizer fires are allowed, but each fire must check active experiments first and usually do nothing while measurement is still in progress. Do not open a new experiment until enough measurement capacity exists for the prior one.
5. If cadence is not obvious:
   - choose a practical recurring run cadence based on the workflow objective and any existing schedules
   - choose a frequent lightweight optimizer cadence that can observe, conclude/defer/log, and adjust schedules without necessarily mutating the workflow
   - stay conservative if the workflow does not appear highly time-sensitive
6. Because `/improve-continuously` runs in Optimizer mode, the scheduled optimizer may call schedule tools itself. It should review cadence on every fire and use update_schedule when prior builder/improve.md history, schedule run history, or active experiment state shows the run/improve cadence is too slow, too fast, stale, or mis-scoped. Prefer updating existing schedules over creating duplicates.
7. Preserve a good existing timezone if one is already in use. Otherwise use the workflow's local/current timezone.

RUN SCHEDULE
Create or update a schedule for normal recurring execution with:
- mode="workflow"
- valid group_names
- a clear name and description that make it obvious this is the primary recurring run schedule

IMPROVE SCHEDULE
Create or update a schedule for recurring improvement with:
- mode="workshop"
- workshop_mode="optimizer"
- valid group_names
- a clear name and description that make it obvious this is the frequent lightweight optimizer schedule
- a single scheduled message whose purpose is to improve BOTH workflow quality and eval quality over time. The message must name the exact group_names it is scoped to, use only runs/iteration-0 evidence for those groups, read builder/improve.md and builder/review.md on every fire, include the optimize/harden/experiment decision model below, include the active-experiment guard below, and explicitly allow safe schedule cadence updates via update_schedule.

The optimizer schedule message must encode the following behavior. Write it explicitly into the schedule message — the agent that fires has no other context.

OPENING (every fire):
- call get_workflow_config and inspect current schedules; call get_schedule_runs for the primary run schedule and the improve schedule when deciding whether cadence or group scope needs adjustment
- read builder/improve.md in full (Workflow Profile, prior improvement log, deferred ideas, previous hypotheses, decision history)
- read builder/review.md if present (unresolved `F-...` findings, prior review risks, and anything already marked RESOLVED/PARTIALLY RESOLVED)
- read soul/soul.md (objective + success criteria — the north star)
- read planning/metrics.json — branches the rest of this turn:
   • if metrics.json has at least one entry → **EXPERIMENT MODE**
   • if empty or missing → **DIRECT MODE**
- read experiments/active.json — list active experiments and their status (proposed / awaiting-approval / measuring / evaluating)
- scope all run-output review, eval review, harden/replan calls, and experiment proposals to the schedule's configured group_names. Do not inspect older selected runs or unrelated groups.

PRIOR HISTORY RULES:
- Already applied items in builder/improve.md: do not repeat unless iteration-0 evidence shows regression.
- Deferred items: reconsider only if current iteration-0 evidence now supports them.
- Failed/reverted experiments: do not retry unless the hypothesis or intervention materially changed.
- Next hypotheses: prefer these when current evidence still supports them.
- Unresolved builder/review.md findings: prioritize them when they affect current success criteria or current run failures.

SCHEDULE SELF-TUNING RULES:
- If the run schedule is too infrequent to produce measurement data for active experiments, update the run schedule cadence or log the blocker when changing cadence would be risky.
- If the improve schedule is too infrequent, update it toward a frequent lightweight cadence. It is okay for many fires to log "no action" after checking active experiments.
- If the improve schedule is firing too often and repeatedly logging no useful observation, slow it modestly, but keep it frequent enough to catch completions and regressions soon after runs.
- If group_names drift from variables/variables.json or from the intended measurement surface, update the schedules to the correct explicit group_names.
- Never create duplicate run/improve schedules when an existing schedule can be updated.

DECISION MODEL (must be followed every fire):
1. **Optimize = diagnose and classify.** Use optimize_workflow when you need to understand whether the next action is structural replan, prompt/config/validation hardening, eval coverage, KB/learnings cleanup, or a metric-backed experiment candidate. Treat optimize as analysis/classification unless its tool result explicitly applies a safe change.
2. **Harden = direct repair.** Use harden_workflow only for clear failures or reliability issues grounded in iteration-0 evidence, or in DIRECT MODE when no metric gate exists. Harden fixes prompts/config/validation/scripted artifacts; it is not the path for speculative metric improvement.
3. **Experiment = metric-backed improvement.** When planning/metrics.json has metrics, package non-obvious improvements as propose_experiment instead of directly hardening/replanning. The hypothesis must name target_metrics, expected_direction, expected_magnitude, intervention_changes, and any linked_review_finding / linked_improve_entry ids.
4. **Replan = structural repair.** Use replan_workflow_from_results only when iteration-0 evidence shows the workflow structure is wrong (missing step, wrong order, broken context flow). Do not use replan for ordinary prompt hardening.
5. **Rule of thumb:** clear failure → harden; structural flaw → replan; metric-moving idea → propose_experiment; unclear category → optimize_workflow first.

EXPERIMENT MODE (when metrics.json is non-empty):
1. **Check active experiments first.** If 3+ experiments are already active, do nothing this fire — log "deferring: too many active experiments" to improve.md and return. Opening experiment #4 while 1–3 are still measuring confounds attribution.
2. If any active experiment is in 'awaiting-approval' or 'awaiting-conclusion-approval', do nothing this fire — those need human action, not new proposals. Log and return.
3. **Discover** by reading runs/iteration-0 outputs first, then iteration-0 eval reports and builder/decisions.jsonl. Compare outputs to soul.md success criteria as a business analyst — what gap is most worth testing? Look for things the user is NOT asking about: tone uniformity that should segment, redundant work that should cache, weak validation that should tighten, content that misses the prospect's pain point. Read enough run output that patterns appear; do not skim.
4. **Pick exactly ONE candidate** — the highest-impact change defensible by specific run-output evidence. Multi-file bundles are fine if they share ONE underlying belief; if you need an "and" connecting distinct claims, those are separate experiments and you only open one.
5. **Pick the target metric.** Prefer outcome metrics (linked_success_criteria non-empty) whose criterion is failing or drifting. Telemetry SLOs are last resort. State explicitly: "this experiment targets <metric_id> which operationalizes success criterion: <quoted criterion>."
6. **Call propose_experiment.** Do NOT call harden_workflow or apply changes directly — the framework gates and reverts on a bad verdict.
7. If no candidate is strong enough (no clear evidence-backed hypothesis), do nothing this fire. Log "no high-confidence hypothesis surfaced" to improve.md and return. A scheduled fire with no proposal is a valid outcome.

DIRECT MODE (when metrics.json is empty):
1. Apply the decision model directly — optimize_workflow to classify when unclear, then harden_workflow for reliability fixes or replan_workflow_from_results for structural flaws. Improve evaluation_plan when its coverage is weak.
2. Be conservative and bounded — do not loop or run a fresh workflow pass unless verification is genuinely needed.

ALWAYS:
- improve the evaluation plan when objective/success-criteria coverage is weak or misleading (this is exempt from the experiment gate — eval definition isn't a workflow change)
- do not edit reports/report_plan.json from this optimizer schedule. If report layout or widgets need changes, log that follow-up and tell the user to switch to Reporting mode.
- update builder/improve.md with: timestamp, mode used, evidence reviewed, what was opened (experiment_id) or applied, what was deferred and why, remaining gaps, next hypotheses{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}
- if schedules were updated, include the schedule ids, old cadence/group scope, new cadence/group scope, and why the change was made in builder/improve.md
- when this scheduled fire applies a change that addresses a finding from builder/review.md or a queued proposal in builder/improve.md, follow the resolution-discipline rules in the system prompt: scan for matching `F-...` / `I-...` ids, append `**[RESOLVED YYYY-MM-DD — <how>]**` markers inline after the original entries, and include `linked_review_finding` / `linked_improve_entry` in the experiment payload (EXPERIMENT MODE) or the builder/decisions.jsonl entry (DIRECT MODE).

PERSISTENT IMPROVEMENT LOG
Create or update builder/improve.md now as the durable optimization log for future scheduled improvement runs.
Bootstrap it with:
- objective and success criteria snapshot
- current schedule strategy
- what the run cadence is
- what the improve cadence is
- current known workflow gaps
- current known eval gaps
- next improvement hypotheses

SCHEDULE CREATION RULES
1. Do NOT delete schedules unless they are clearly redundant and safe to remove. Prefer update over delete.
2. If an existing run schedule already serves the purpose, keep it and only refine it if needed.
3. If an existing optimizer/improve schedule already serves the purpose, keep it and only refine it if needed.
4. Use create_schedule / update_schedule as appropriate.

FINAL REPORT
Summarize:
- what schedules already existed
- what you created vs updated
- run schedule: ID, name, cadence, timezone, groups
- improve schedule: ID, name, cadence, timezone, groups
- where you saved builder/improve.md
- the exact optimizer schedule message you configured
