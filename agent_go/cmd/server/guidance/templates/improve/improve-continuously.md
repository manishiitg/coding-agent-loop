Set up automatic run + improve scheduling for this workflow. FIRST check what already exists before proposing or creating anything. Do this autonomously and avoid creating duplicate schedules.{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

GOAL
Create or update TWO complementary schedules:
1. a normal workflow run schedule for recurring execution
2. a slower optimizer/workshop schedule that continuously improves the workflow and evaluation over time

DISCOVERY
1. Call get_workflow_config and inspect the current schedule list carefully before doing anything else.
2. If there are existing candidate schedules, use get_schedule_runs on the most relevant ones to understand whether they are active, useful, stale, too frequent, or missing coverage.
3. Read soul/soul.md to understand the objective and success criteria.
4. Read variables/variables.json to identify valid group names and enabled groups.
5. **Framework precheck.** Read builder/improve.md. If there is no "## Workflow Profile" section, stop and redirect: "Run /improve-setup-framework first." A continuous-improvement schedule with no profile and no metrics will optimize nothing concrete. If the profile declares business-context accumulation or a frozen/ratchet plan and planning/metrics.json is empty, also redirect.
6. **Framework mode.** Read planning/metrics.json. If it has at least one entry, the scheduled improve runs will operate in EXPERIMENT MODE — open at most one experiment per fire, gated through propose_experiment. If empty, scheduled improve runs are in DIRECT MODE — apply changes directly. Note this in the schedule's name/description so the operator knows which mode the schedule is using.
7. Read planning/experiments/config.json (if it exists) to find default_measurement_runs / target_runs — needed to size the improve cadence correctly (see SCHEDULE STRATEGY below).

SCHEDULE STRATEGY
1. Prefer updating or reusing good existing schedules instead of creating duplicates.
2. Only create a new schedule when there is no existing schedule that already serves that purpose.
3. The improve schedule must be LESS frequent than the run schedule.
4. **Experiment cadence guard (EXPERIMENT MODE only).** The improve schedule MUST fire less often than (target_runs × run_schedule_period). Reason: each experiment needs target_runs of the workflow to conclude, and opening a new experiment before prior ones conclude confounds attribution. Example: run_schedule daily, target_runs=5 → improve schedule no more frequent than weekly. If the desired improve cadence violates this, slow it down or raise the run schedule frequency, not the other way around.
5. If cadence is not obvious:
   - choose a practical recurring run cadence based on the workflow objective and any existing schedules
   - choose a larger/slower cadence for optimizer improvement (subject to the cadence guard above in EXPERIMENT MODE)
   - stay conservative if the workflow does not appear highly time-sensitive
6. Preserve a good existing timezone if one is already in use. Otherwise use the workflow's local/current timezone.

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
- a clear name and description that make it obvious this is the slower recurring optimizer schedule
- a single scheduled message whose purpose is to improve BOTH workflow quality and eval quality over time

The optimizer schedule message must encode the following behavior. Write it explicitly into the schedule message — the agent that fires has no other context.

OPENING (every fire):
- read builder/improve.md (prior improvement log + decision history)
- read soul/soul.md (objective + success criteria — the north star)
- read planning/metrics.json — branches the rest of this turn:
   • if metrics.json has at least one entry → **EXPERIMENT MODE**
   • if empty or missing → **DIRECT MODE**
- read experiments/active.json — list active experiments and their status (proposed / awaiting-approval / measuring / evaluating)

EXPERIMENT MODE (when metrics.json is non-empty):
1. **Check active experiments first.** If 3+ experiments are already active, do nothing this fire — log "deferring: too many active experiments" to improve.md and return. Opening experiment #4 while 1–3 are still measuring confounds attribution.
2. If any active experiment is in 'awaiting-approval' or 'awaiting-conclusion-approval', do nothing this fire — those need human action, not new proposals. Log and return.
3. **Discover** by reading run outputs first (latest iteration under runs/), eval reports, decisions.jsonl. Compare outputs to soul.md success criteria as a business analyst — what gap is most worth testing? Look for things the user is NOT asking about: tone uniformity that should segment, redundant work that should cache, weak validation that should tighten, content that misses the prospect's pain point. Read enough run output that patterns appear; do not skim.
4. **Pick exactly ONE candidate** — the highest-impact change defensible by specific run-output evidence. Multi-file bundles are fine if they share ONE underlying belief; if you need an "and" connecting distinct claims, those are separate experiments and you only open one.
5. **Pick the target metric.** Prefer outcome metrics (linked_success_criteria non-empty) whose criterion is failing or drifting. Telemetry SLOs are last resort. State explicitly: "this experiment targets <metric_id> which operationalizes success criterion: <quoted criterion>."
6. **Call propose_experiment.** Do NOT call harden_workflow or apply changes directly — the framework gates and reverts on a bad verdict.
7. If no candidate is strong enough (no clear evidence-backed hypothesis), do nothing this fire. Log "no high-confidence hypothesis surfaced" to improve.md and return. A scheduled fire with no proposal is a valid outcome.

DIRECT MODE (when metrics.json is empty):
1. Apply the legacy autonomous improvement logic — review evidence, optimize_workflow, harden_workflow / replan_workflow_from_results as needed, improve evaluation_plan / report_plan when their coverage is weak.
2. Be conservative and bounded — do not loop or run a fresh workflow pass unless verification is genuinely needed.

ALWAYS:
- improve the evaluation plan when objective/success-criteria coverage is weak or misleading (this is exempt from the experiment gate — eval definition isn't a workflow change)
- update builder/improve.md with: timestamp, mode used, evidence reviewed, what was opened (experiment_id) or applied, what was deferred and why, remaining gaps, next hypotheses{{if .Focus}} Focus especially on: {{.Focus}}.{{end}}

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
