import React from 'react'
import { FileText, Lightbulb, Download, Server, Cpu, Bot, Layers, Minimize2, AlertTriangle, RefreshCw, Wrench, GitBranch, CheckCircle, Search, Lock, Beaker } from 'lucide-react'
import type { CommandDefinition } from './types'

export const builtinCommands: CommandDefinition[] = [
  {
    command: 'design-flow',
    description: 'Validate context dependency chain between steps',
    icon: <GitBranch className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'builder',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? `\nPay special attention to: ${focus}` : ''
      ctx.onSubmit(`Read planning/plan.json and analyze the context flow between steps.${focusText}

Check for:
1. **Broken chain** — step depends on a context_output that no earlier step produces
2. **Orphaned outputs** — step produces context_output that no later step consumes
3. **Circular dependencies** — A depends on B depends on A
4. **Implicit dependencies** — step description references data from another step but context_dependencies doesn't list it
5. **Type mismatches** — upstream produces a JSON file but downstream expects CSV, or field names don't align
6. **Missing validation** — steps that produce context_output but have no validation_schema

Show me:
- A dependency graph: step-a (produces X) → step-b (consumes X, produces Y) → step-c (consumes Y)
- Any issues found with severity (CRITICAL / WARNING / INFO)
- Suggested fixes for each issue

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with what was reviewed, the main findings ordered by severity, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.`)
    }
  },
  {
    command: 'ready-to-optimize',
    description: 'Check if workflow is ready to move to optimizer mode',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'builder',
    source: 'builtin',
    execute: (ctx) => {
      ctx.onSubmit(`Run an optimization-readiness checklist. Check each item and report PASS or FAIL:

1. **Objective set?** — Read soul/soul.md and check the "## Objective" section. FAIL if the file is missing, the section is absent, or its body is empty / still a "<TODO: ...>" placeholder.
2. **Success criteria set?** — Read soul/soul.md and check the "## Success Criteria" section. Same FAIL conditions as objective.
3. **All steps have descriptions?** — Check every step in plan.json has a non-empty description. FAIL if any are empty.
4. **Context flow valid?** — Check every context_dependency references an existing context_output from an earlier step. FAIL if broken links.
5. **Variables configured?** — Read variables/variables.json, check at least one group exists with values. FAIL if empty.
6. **At least one successful run?** — Check runs/ folder for any completed iteration. FAIL if no runs exist.
7. **Validation schemas exist?** — Check that steps producing context_output have a validation_schema. WARN if missing.
8. **Evaluation plan exists?** — Check evaluation/evaluation_plan.json exists and has at least one eval step. WARN if missing.
9. **Step configs set?** — Check planning/step_config.json has entries for all steps with execution mode declared. WARN if missing.

Summary:
- READY if 0 FAILs
- NOT READY if any FAILs — list what needs to be done
- If READY with WARNs — "Ready but recommended to fix these first"

REVIEW LOG: append a dated entry to builder/review.md (create if absent) with the readiness verdict, the FAILs/WARNs list, and what needs to be done before optimizing.`)
    }
  },
  {
    command: 'review-plan',
    description: 'Critically analyze the workflow design for weaknesses and improvements',
    icon: <Search className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer', 'run'],
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Focus especially on: ${focus}.` : ''
      ctx.onSubmit(`Run review_plan() to critically analyze the current workflow plan.${focusText}

Challenge every decision: step boundaries, step types, execution modes, context flow, validation coverage, portability, and whether choices are justified by the objective and success criteria. Report findings by severity — don't just summarize, identify what's weak, risky, or unjustified.

This is a plan/design review. Use review_workflow_results() when the question is whether a real run is actually achieving the goal and whether eval measures that properly.

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with what was reviewed, the main findings ordered by severity, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.`)
    }
  },
  {
    command: 'review-goal',
    description: 'Review whether a real run achieves the goal, and whether eval is measuring that properly',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Focus especially on: ${focus}.` : ''
      const runText = runFolder
        ? `Use the selected run folder "${runFolder}" unless you find stronger newer evidence is needed.`
        : 'If no run folder is selected, find the latest meaningful run/eval evidence first.'
      ctx.onSubmit(`Run review_workflow_results() to judge actual workflow outcomes, not just the plan.${focusText}

${runText}

Assess three things separately:
1. Is the workflow actually achieving the stated objective?
2. Which success criteria are met, partial, unmet, or still unknown?
3. Does the evaluation plan/report actually measure the objective and success criteria properly, or is it giving false confidence?

For each success criterion, show:
- status: met / partial / unmet / unknown
- the strongest run evidence
- whether eval measures it directly, indirectly, weakly, or not at all

Then give:
- an overall verdict on goal achievement
- an overall verdict on evaluation quality
- the most important workflow gaps
- the most important eval gaps
- the top next actions, clearly separated into workflow fixes vs eval fixes.

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with the goal/eval verdict, the main gaps ordered by severity, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.`)
    }
  },
  {
    command: 'review-speed',
    description: 'Review workflow latency and how to make it faster',
    icon: <Minimize2 className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Focus especially on: ${focus}.` : ''
      const runText = runFolder
        ? `Use the selected run folder "${runFolder}" unless you find stronger newer evidence is needed.`
        : 'If no run folder is selected, find the latest meaningful run evidence first.'
      ctx.onSubmit(`Run review_workflow_timing() to analyze where workflow time is actually going and how to make it faster.${focusText}

${runText}

Assess four things separately:
1. What is the overall workflow wall-clock, and what is the biggest bottleneck class: LLM latency, tool latency, orchestration overhead, or plan shape?
2. Which groups and steps are consuming the most time, with the split between total time, LLM time, tool time, and unexplained overhead?
3. Which speedups come from tightening descriptions or reducing tool thrash versus changing the plan shape (merge/remove/reorder steps)?
4. Which speedups are safe versus risky for the objective and success criteria?

Then give:
- the top bottlenecks with evidence
- the best description/prompt changes
- the best plan changes to reduce handoffs or unnecessary steps
- the best model/tool/config changes
- the top next actions, with expected impact and risk to success criteria.

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with the timing analysis, the top bottlenecks, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.`)
    }
  },
  {
    command: 'review-cost',
    description: 'Review workflow cost and how to reduce it safely',
    icon: <Cpu className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Focus especially on: ${focus}.` : ''
      const runText = runFolder
        ? `Use the selected run folder "${runFolder}" unless you find stronger newer evidence is needed.`
        : 'If no run folder is selected, find the latest meaningful run and cost evidence first.'
      ctx.onSubmit(`Run review_workflow_costs() to analyze where workflow cost is going and how to reduce it without hurting results.${focusText}

${runText}

Assess four things separately:
1. Which steps, models, or phases are consuming the most cost?
2. Which spend is necessary for success versus waste from retries, too many handoffs, overly expensive models, or unnecessary evaluation breadth?
3. Which cost reductions come from tightening descriptions or reducing retries/tool calls versus changing the plan shape (merge/remove/reorder steps)?
4. Which cost reductions are safe versus risky for the objective and success criteria?

Then give:
- the top cost drivers with evidence
- the best description/prompt changes
- the best plan changes to reduce unnecessary steps or handoffs
- the best model/tool/config changes
- the top next actions, with expected savings and risk to success criteria.

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with the cost analysis, the top cost drivers, the recommendations (REVIEW = recommend; do NOT apply), and items flagged for follow-up.`)
    }
  },
  {
    command: 'review-config',
    description: 'Review per-step KB / db / lock_learnings / lock_code recommendations',
    icon: <Lock className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer', 'run'],
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? `\nFocus areas: ${focus}.` : ''
      ctx.onSubmit(`Audit every step in this workflow's plan and recommend the right values for: knowledgebase_access, knowledgebase_contribution, lock_learnings, and lock_code (learn_code steps only). This is a READ-ONLY analysis pass — do NOT apply any changes via update_step_config. Produce a recommendation table the user will review and apply selectively.${focusText}

PROCEDURE
1. Read planning/plan.json and planning/step_config.json. For each step capture: id, title, description, declared_execution_mode, and current AgentConfigs (knowledgebase_access, knowledgebase_contribution, lock_learnings, lock_code, optimized, successful_runs, description_hash).
2. For each step, inspect learnings/{step-id}/:
   - SKILL.md exists? size? recent edit signal (read first/last lines for context).
   - main.py exists? Read learnings/{step-id}/script_metadata.json for successful_runs counts (code_exec/learn_code) and recent failure history.
3. Sample run evidence from runs/iteration-0/{first-group}/logs/{step-id}/ when available:
   - learn_code_fast_path.json — recent main.py outcomes.
   - pre_validation.json — validation pass/fail.
   - Any signs of recent fix-loop rewrites or transient failures.

DECISION RULES (apply per step)

KB_ACCESS (none | read | write | read-write):
- Step CONSUMES durable subject-matter facts (entities, relationships, prior strategies)? → "read"
- Step PRODUCES durable facts (extracts companies, identifies recurring patterns, captures decisions)? → "write" + must set knowledgebase_contribution to a non-empty extraction instruction
- Both? → "read-write"
- Pure I/O / orchestration / data movement? → "none" (default; KB is opt-in per step)
- FLAG: knowledgebase_contribution is non-empty but knowledgebase_access lacks write — the post-step KB update agent is silently skipped (controller_kb_update.go gates on kbAccessAllowsWrite). Recommend either set access to "write"/"read-write" OR clear the contribution.

DB ACCESS:
- Every step already has db/ read+write via folder guard (unconditional, no opt-in). The audit question is INTENT: should this step write to db/?
- Recommend writing to db/{file}.json when: the step produces cross-run state, per-key data that subsequent runs upsert into, or data the report widgets need to bind to.
- Suggest the file name + primary_key convention (e.g. db/account_status.json, primary_key=group_name).
- FLAG: step description claims to "save state" / "update results" / "track" something but doesn't explicitly name a db/ file — likely a portability bug; the step is probably writing into runs/{iter}/ instead of the workspace-level db/.

LOCK_LEARNINGS (true | false):
- Lock when ALL: successful_runs >= 3 AND SKILL.md is stable across recent runs (learning agent producing near-duplicate edits) AND description_hash matches stored value.
- DO NOT lock when ANY: description recently changed (hash mismatch), recent failures triggered learning rewrites, fewer than 3 successful runs, step is still mid-iteration.
- If currently locked but description_hash drifted → recommend UNLOCK (learnings are stale relative to intent).

LOCK_CODE (true | false; learn_code steps only):
- Lock when ALL: main.py exists AND script_metadata.json shows >= 3 successful runs across all active groups AND no recent fix-loop rewrites AND description hash matches.
- DO NOT lock when ANY: main.py rewritten in last 2 runs, transient failures present, description being iterated, fewer than 3 successful runs.
- When recommending lock_code=true, also recommend lock_learnings=true and optimized=true together — they should move as a set per the workshop convention. Include optimized_reason citing the evidence (groups passed, eval scores, no fix-loop rewrites).
- If currently locked but description_hash drifted → recommend UNLOCK (main.py may be stale).

OUTPUT FORMAT

Single table, one row per step:

| Step ID | KB Access (cur → rec) | KB Contribution | DB Output | Lock Learnings (cur → rec) | Lock Code (cur → rec) | Reason |
|---|---|---|---|---|---|---|

Then summary sections:
- **To lock this round** — steps recommended for lock_learnings + lock_code (+ optimized).
- **KB misconfigs** — knowledgebase_contribution set but access blocks writes (silent skip).
- **Should write to db/ but doesn't** — steps producing cross-run state outside db/.
- **Stale locks (unlock + re-review)** — currently locked but description_hash drifted.

END WITH READY-TO-PASTE COMMANDS
List the exact update_step_config calls the user can copy/paste to apply each recommendation, one per line. Group by recommendation type (KB updates, lock updates) so the user can apply them selectively. Do NOT call update_step_config yourself.

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with what config you reviewed, the main misconfigs found, the recommended changes, and items flagged for follow-up.`)
    }
  },
  {
    command: 'improve-report',
    description: 'Validate reports/report_plan.json and suggest layout/color improvements',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'reporting',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusLine = focus ? `\n\nFocus on: ${focus}.` : ''
      ctx.onSubmit(`Review and improve reports/report_plan.json in two passes. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your report-plan findings and applied decisions when you finish.${focusLine}

PASS 1 — VALIDATION
Call validate_report_plan.
- For each error: explain what's wrong in plain language, show the section + widget it refers to, and propose the exact fix (which line to change, to what).
- For warnings: group by severity. Flag ones that would visibly degrade the report (unknown chart_type, missing axis fields, invalid colors) separately from cosmetic ones (empty arrays that will fill in after a run).

PASS 2 — IMPROVEMENT SUGGESTIONS
Call preview_report_render first so you can inspect what the report actually renders like with current data. Treat that rendered preview as a required input, not an optional extra.

Then call get_report_plan yourself and also sample the underlying db/*.json and knowledgebase/*.json sources. Use both the rendered preview and the raw data/plan to propose improvements in these categories:

1. **Layout**: are the most important widgets at the top? Are there too many widgets in a row cramming the view? Is the H2 structure grouping related content?
2. **Chart-type fit**: for each chart, is bar/line/area/pie the right choice for that data? (bar=categorical, line=time series, pie=composition ≤6 slices)
3. **Color**: does the report use semantic coloring where meaningful (status fields, pass/fail, severity)? Suggest adding color_by + color_map for any status-like fields you see in the data. Propose palettes (colors + colors_dark) for brand consistency if multiple charts share a theme.
4. **Formatting**: any number/date/currency fields that should have a format preset? Any tables with too many columns that could benefit from hide_columns?
5. **Density**: any charts with >10 points that need top_n? Any tables without default_sort that would be hard to scan?
6. **Rendered reality check**: based on the preview, what actually looks broken, cramped, misleading, empty, or visually weak even if the JSON is technically valid?

Show ALL proposed changes as a diff (before/after snippets per widget) before editing. Ask whether to apply all, some, or none. Don't edit report_plan.json until I confirm.

When you finish, update builder/improve.md with:
- what report evidence you reviewed
- the main report weaknesses you found
- what you recommended
- what was applied vs deferred`)
    }
  },
  {
    command: 'improve-eval',
    description: 'Validate evaluation/evaluation_plan.json and improve goal/criteria coverage',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      const focus = ctx.beforeSlash.trim()
      const focusLine = focus ? `\n\nFocus on: ${focus}.` : ''
      const runText = runFolder
        ? `Use the selected run folder "${runFolder}" as the primary evidence set when judging whether eval measures the real objective and success criteria well.`
        : 'If a meaningful prior run exists, use it as evidence when judging whether eval measures the real objective and success criteria well. You may inspect existing evaluation results and only run evaluation if needed to test the current eval plan.'
      ctx.onSubmit(`Review and improve evaluation/evaluation_plan.json. Eval changes are special-cased in the framework: they change WHAT is measured, not the workflow's behavior. So eval changes do NOT open experiments — but they DO have rules to follow because changing the rubric mid-stream invalidates trajectory baselines and active experiments. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your eval findings and applied decisions when you finish.${focusLine}

PASS 0 — FRAMEWORK PRECHECK + ACTIVE-EXPERIMENT GUARD
1. Read builder/improve.md. If there is no "## Workflow Profile" section, stop and redirect: "Run /improve-setup-framework first."
2. Read <workflow>/planning/metrics.json. If absent or empty AND the Workflow Profile declares business-context accumulation OR a frozen/ratchet plan, stop and redirect to /improve-setup-framework. Plain mutable+exploratory workflows may proceed without metrics.
3. **Active-experiment guard.** Read experiments/active.json. For each experiment whose status is 'measuring' or 'evaluating', look at its target_metrics and resolve each metric in metrics.json. If any of those metrics is sourced from an eval step you might be about to edit (source.type=eval_step, source.id matches an eval step id), STOP and tell the user: "experiment <id> is currently measuring metric <m> against eval step <step_id>. Editing that step now would change its rubric mid-stream and invalidate the experiment's baseline. Either wait for the experiment to conclude (or /exp-abort it) before editing this eval step, or focus this command on eval steps not under active measurement." Proceed only with eval steps that are NOT under measurement.

PASS 1 — VALIDATION
1. Call validate_evaluation_plan.
2. For each error: explain what's wrong in plain language, show the eval step/widget/field it refers to, and propose the exact fix.
3. For warnings: separate correctness-risk warnings from lower-priority quality issues.

PASS 2 — OUTPUT-FIRST ALIGNMENT (does eval catch what success_criteria care about?)
1. Read soul/soul.md and extract the objective and success criteria. These are the standard eval should measure against.
2. **Read run outputs first.** Open the latest meaningful iteration under runs/ and look at what was produced. Then read the matching eval reports. Where does the eval rubric MISS what a domain expert would notice? Examples: outputs are bland and repetitive but eval says they pass; outputs make unsupported claims but eval doesn't check; outputs ignore audience segmentation but eval has no segment-specific check.
3. Read planning/plan.json so you understand what the workflow is producing.
4. ${runText}
5. From the output review + run/eval comparison, judge:
   - which success criteria are directly measured by the current eval
   - which are only weakly or indirectly measured
   - which are not measured at all (coverage gap)
   - whether any eval checks give false confidence (says pass when outputs are clearly weak) or miss obvious failure modes

PASS 3 — IMPROVEMENT SUGGESTIONS
Propose improvements in these categories:
1. **Coverage**: does each important success criterion have a clear eval step?
2. **Directness**: is the eval checking the actual desired outcome, or only proxies?
3. **Determinism**: are any eval steps too vague, subjective, or hard to reproduce?
4. **Redundancy**: are multiple eval steps measuring the same thing with little added value?
5. **Thresholds / scoring**: are pass/fail thresholds or scores aligned with the stated success criteria?
6. **Reality check**: if outputs you read in Pass 2 show obvious failure or success, does the eval report reflect that honestly?

Show ALL proposed changes as a diff (before/after snippets per eval step) before editing. Ask whether to apply all, some, or none. Don't edit evaluation/evaluation_plan.json until I confirm.

PASS 4 — RECORD THE CHANGE (every eval edit)
After applying any change to evaluation/evaluation_plan.json:
1. Append an entry to decisions.jsonl using diff_patch_workspace_file. Format (one JSON object per line):
   {"id": "<short-id-or-uuid>", "ts": "<ISO-8601 UTC>", "source": "agent", "trigger": "improve-eval", "applied_changes": ["evaluation/evaluation_plan.json"], "rationale": "<one-line summary of what changed and why>", "target_metrics": [<list of metric ids whose source.id points to edited eval steps, if any>]}
2. The decisions entry serves as a "rubric change" marker. Trajectory chart renderers should break the line at this timestamp because pre-change and post-change scores aren't comparable.

When you finish, update builder/improve.md with:
- what workflow/eval evidence you reviewed (especially output-vs-rubric mismatches from Pass 2)
- the main eval weaknesses you found
- which eval steps you skipped because they're under active measurement (per Pass 0 guard)
- what you recommended and what was applied
- the decisions.jsonl entries you appended (rubric-change markers)`)
    }
  },
  {
    command: 'improve-continuously',
    description: 'Set up recurring workflow run + slower recurring optimizer improvement',
    icon: <Bot className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? `\nFocus especially on: ${focus}.` : ''
      const improveMessageFocus = focus ? ` Focus especially on: ${focus}.` : ''
      ctx.onSubmit(`Set up automatic run + improve scheduling for this workflow. FIRST check what already exists before proposing or creating anything. Do this autonomously and avoid creating duplicate schedules.${focusText}

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
- update builder/improve.md with: timestamp, mode used, evidence reviewed, what was opened (experiment_id) or applied, what was deferred and why, remaining gaps, next hypotheses${improveMessageFocus}

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
- the exact optimizer schedule message you configured`)
    }
  },
  {
    command: 'improve-workflow',
    description: 'Use existing run evidence to review, replan if needed, harden, then optionally verify',
    icon: <RefreshCw className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? `\nFocus especially on: ${focus}.` : ''
      const iterationHint = runFolder
        ? `Use iteration "${runFolder.split('/')[0]}" as the starting evidence set for structural review.`
        : 'Read runs/ to find the latest iteration and use that as the starting evidence set for structural review.'
      const focusArg = focus ? `, focus="${focus}"` : ''
      ctx.onSubmit(`Improve this workflow by surfacing changes the user is NOT thinking about — non-obvious improvements grounded in what the workflow actually produced. /review-* commands are for small fixes the user reviews; this command's job is AI-proposed changes the user wouldn't have asked for. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and update it with your decisions at the end.${focusText}

MENTAL MODEL
Think like a sharp business analyst auditing this workflow's actual outputs against its success criteria — not like a senior engineer reviewing code. These are business-process workflows, not software systems. The kinds of changes that matter here are things a domain expert would notice when reading what the workflow produced:
- "Every reply has the same tone, but success criteria mention engaging different audience segments — segment by follower type and vary voice."
- "The workflow researches every prospect from scratch, but 40% of last month's runs were repeats — cache and refresh deltas instead."
- "Outreach copy leads with our product; the high-converting examples in run history all led with the prospect's pain point."
- "Validation accepts any non-empty reply. Half the replies in run history are 'thanks' — that's not engagement, raise the bar."
You should be uncomfortable with how obvious-in-retrospect a change feels after you read enough run output. That's the right mode.

SETUP
1. Read planning/plan.json to extract the objective and success_criteria — north star for every decision.
2. Read evaluation/evaluation_plan.json so you understand what the eval is measuring.
3. Read variables.json to get the enabled group names.
4. **Framework precheck.** Read builder/improve.md. If there is no "## Workflow Profile" section, stop and redirect: "Run /improve-setup-framework first to write the Workflow Profile and bootstrap metrics." If the profile declares business-context accumulation or a frozen/ratchet plan and <workflow>/planning/metrics.json is empty, also redirect. Plain mutable+exploratory workflows may proceed without metrics.
5. **Framework mode.** Read <workflow>/planning/metrics.json. If it has at least one entry, you are in **EXPERIMENT MODE** for this command run: instead of applying changes directly via harden_workflow / replan_workflow_from_results, package the intended changes as experiments via propose_experiment so they're gated behind measurement and auto-revertible. If metrics.json is empty (or missing), you are in **DIRECT MODE**: harden / replan apply changes immediately. Note this choice in the final report.
6. ${iterationHint} Treat that iteration as the default evidence set for this command run.

PHASE 1 — OUTPUT REVIEW (the heart of the discovery)
This is the primary signal in EXPERIMENT MODE and the most undervalued one in DIRECT MODE. Do it first, before any tool call. This command subsumes what used to be /improve-kb and /improve-learnings — the discovery looks across plan, KB, and learnings as one surface, not as separate concerns.
1. Open the iteration folder under runs/ for the most recent meaningful iteration. Read what the workflow actually PRODUCED — generated copy, sent messages, written reports, scored decisions. Read enough of it that patterns start to appear. Don't skim.
2. Read evaluation reports for the same iteration. The eval rationale text is often the richest signal — pay attention to WHY something scored low, not just the score.
3. Compare outputs against the success criteria from soul.md. Where's the gap a domain expert would see?
4. Skim decisions.jsonl — what has the user been asking for? What's been tried before? Avoid re-proposing failed ideas.
5. **When output patterns suggest the issue isn't in the plan itself**, also inspect:
   - **knowledgebase/notes/_index.json + topic files** — outputs that contradict each other, leak stale facts, or miss context the workflow should have known often trace to KB drift (duplicate or overlapping topics, stale narrative, missing step contributions).
   - **learnings/_global/SKILL.md and references/*.md** — outputs that repeat known mistakes, or that reveal step rationale contradicting established guidance, often trace to learnings gaps (duplicated lessons, missing guidance for declared learning_objectives, repeated run fixes that should have become durable lessons, step-specific learnings that belong in _global).
   - **knowledgebase/rules/rules.md** — when outputs violate user-stated business rules, the rule may be missing or out of date (note: rule additions are user-authoritative and don't go through the experiment gate; this command flags them for the user, doesn't add them).
6. List 3–5 candidate changes ranked by expected business impact. Each candidate must name the FILES it would touch (plan/step descriptions, knowledgebase/notes/, learnings/, validation rules, prompts) and be defensible by something specific in run outputs ("posts 7, 12, 19 in iteration-3/group-a all scored <0.3 and all share <pattern>"), not by abstract reasoning. A single candidate may span multiple file kinds — that's fine if they share one underlying belief.

PHASE 2 — STRUCTURAL DIAGNOSIS (complement, not primary)
1. Call optimize_workflow(${focus ? `focus="${focus}"` : ''}).
2. Read the result and classify findings as Structural (missing steps, wrong ordering, broken context flow, wrong step type) vs Non-structural (weak prompts, weak validation, reliability gaps).
3. If a MATERIAL structural problem appears and you have real run evidence, call replan_workflow_from_results(iteration="{starting_iter}"${focusArg}) ONCE before continuing.
4. Do not thrash the plan. At most one structural replan per command run.
5. **Reconcile Phase 1 and Phase 2.** If output review surfaced something optimize_workflow missed (likely, because optimize_workflow looks at code-shape, not outputs), trust the output review.

PHASE 3 — PER-GROUP REVIEW → APPLY CHANGES
Repeat the following for each enabled group, sequentially.

For group {group}:
  a. **REVIEW EVIDENCE** — inspect outputs, logs, validation failures, and the evaluation report for "{starting_iter}/{group}".
     - If the workflow run exists but the evaluation report is missing, you MAY call run_full_evaluation(target_run_folder="{starting_iter}/{group}"). Do NOT execute a fresh workflow run here.
     - If there's no meaningful run evidence for this group, report the gap and continue with groups that have evidence.
  b. **DECIDE** based on the candidate changes from Phase 1 + the structural findings from Phase 2:
     - **DIRECT MODE (no metrics.json):**
       • If issues are structural and you haven't replanned yet, call replan_workflow_from_results(iteration="{starting_iter}", group_name="{group}"${focusArg}), then continue.
       • Otherwise call harden_workflow(iteration="{starting_iter}", group_name="{group}"${focusArg}) for plan/prompt/validation fixes.
       • If the candidate's primary lever is KB cleanup, call reorganize_knowledgebase or consolidate_knowledgebase as appropriate.
       • If the candidate's primary lever is learnings cleanup or promotion, call organize_global_learnings.
       • These tools may run in sequence within a single group's review.
     - **EXPERIMENT MODE (metrics.json non-empty):**
       • Pick the highest-impact candidate from Phase 1 for this group. Do NOT call harden_workflow / reorganize_knowledgebase / consolidate_knowledgebase / organize_global_learnings — they direct-edit, bypassing the gate.
       • Formulate a hypothesis tying the change to ONE belief about the workflow, expressed against ONE target metric. Bundled multi-file changes that span plan + KB + learnings are FINE if they share one underlying belief (example of a coherent bundle: "personalize outreach by reading prospect's last post + raise step-3 validation to require pain-point reference + promote 'always cite source' learning to _global" — three files across three layers, one belief about generic outreach). Incoherent bundle that should be split: "add personalization AND reduce step 4's temperature AND clean up unrelated KB topics" — three unrelated beliefs, three experiments. Single-belief test: write the hypothesis in one sentence; if you need an "and" connecting distinct claims, split.
       • Call propose_experiment with: hypothesis (≤200 chars, in form "<change> will <direction> <metric_id> by ≥<magnitude><unit> because <one-line mechanism rooted in run evidence>"), target_metrics, expected_direction (must match the metric's declared direction), expected_magnitude (absolute, > 0), intervention_changes (file edits across any of plan/, knowledgebase/notes/, learnings/, validation rules, prompts — paths must be in experiments/config.json::allowed_intervention_paths). The framework applies the diff atomically and reverts on a bad verdict. One experiment per group per command run.
       • Before proposing, read experiments/history.jsonl and experiments/config.json::pinned_hypotheses to avoid retrying anything that recently failed or that the user pinned as forbidden.
       • For structural problems severe enough to require replan, replan is exempt from the experiment gate (it changes plan shape, not the decisions metrics measure). Replan first, then continue.
       • If a target metric you need does not yet exist, call propose_metric first (with linked_success_criteria populated from soul.md).
  c. **THE CHANGE DOES NOT HAVE TO BE SMALL.** The framework auto-reverts on a bad verdict, so blast radius is recoverable. Optimize for "the experiment will tell us something useful" — not for "the change is tiny." Multi-file bundled changes that share one belief are often higher-signal than fragmented small ones.

PHASE 4 — VERIFY (DIRECT MODE only)
In DIRECT MODE: if the workflow still misses key success criteria and the cause is clearly fixable within one more pass, do ONE targeted verification on the highest-value group: run_full_workflow + run_full_evaluation. Maximum one extra pass; do not loop.
In EXPERIMENT MODE: skip this phase. Verification IS the experiment loop — running the workflow now would just be one of the measurement runs, and the framework will compute a verdict deterministically when target_runs is reached. The next workflow runs will populate measurement.values automatically.

FINAL REPORT
Summarize:
- Mode used (DIRECT or EXPERIMENT) and why
- Output-review findings (Phase 1) — what patterns in run outputs surfaced
- Structural diagnosis (Phase 2) — what optimize_workflow added or contradicted
- Whether replan_workflow_from_results was used, and why
- Per-group: evidence reviewed, harden changes (DIRECT) or experiment_ids opened with their hypotheses (EXPERIMENT)
- Which success criteria are now better satisfied (DIRECT), or which experiments are now measuring against which criteria (EXPERIMENT)
- Remaining gaps that still need human attention, if any

Before finishing, update builder/improve.md with:
- evidence reviewed
- mode (direct vs experiment) and any experiment ids opened
- workflow changes applied (or, in experiment mode, queued behind experiments)
- eval/report changes touched if any
- what improved
- remaining gaps
- next hypotheses`)
    }
  },
  {
    command: 'review-descriptions',
    description: 'Review all steps for description vs skill/learning confusion',
    icon: <AlertTriangle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer', 'run'],
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Pay special attention to: ${focus}.` : ''
      ctx.onSubmit(`Audit every step's description in plan.json. For each step, do the following:

1. Read the step description from plan.json.
2. Read the step's SKILL.md / learnings (if any exist).
3. Check for these problems:

   **Description vs Skill Confusion:**
   - **Description contains runtime learnings**: The description should be an *instruction* (what to do), not a *retrospective* (what worked last time). Phrases like "use batch mode because single inserts timeout", "avoid X which caused failures", or specific tool parameter values discovered at runtime belong in SKILL.md, not the description.
   - **Skill contains task instructions**: The SKILL.md should capture *reusable patterns and pitfalls discovered during execution*, not restate what the step is supposed to do. If the skill reads like a task description, it's confused.
   - **Duplication**: Same guidance appearing in both description and skill — pick one home.
   - **Description is vague because it defers to skill**: The description says something like "follow the skill" or "see learnings" instead of giving clear instructions.

   **Hardcoded Values:**
   - **Hardcoded paths**: Absolute paths ("/app/workspace-docs/...", "/Users/...", "/home/...") or specific local paths. Should use relative paths instead.
   - **Hardcoded run/iteration paths**: References to specific run folders like "runs/iteration-0/...", "execution/step-3/...", or hardcoded group names like "group-1". These break across different runs and groups — the orchestrator resolves these at runtime via context_dependencies.
   - **Hardcoded credentials/secrets**: API keys, tokens, passwords, auth headers, or any sensitive values. Should reference SECRET_* environment variables instead.
   - **Hardcoded IDs/URLs/user-specific values**: Specific spreadsheet IDs, database names, API endpoints, user IDs, email addresses, phone numbers, account numbers, or other environment-specific values. Should use variable placeholders (e.g., {USER_ID}, {SHEET_ID}, {EMAIL}) in descriptions, with actual values in variables.json / variable groups.

   **Todo Task / Orchestrator Description Quality (for todo_task steps only):**
   - **Missing objective/intent**: The orchestrator description should clearly state WHAT we are trying to achieve — the goal and purpose of this orchestration. Without this, the orchestrator can't make intelligent decisions or handle unexpected situations.
   - **Reduced to a sequencer**: If the description is just "call route A, then route B, then route C" or a fixed execution order, it's a script not orchestration. The orchestrator is a capable LLM — its description should enable it to reason about what to do, not just follow a checklist. If fixed sequencing is all that's needed, these should be regular steps instead.
   - **No edge case / failure guidance**: The description should explain how to handle failures, retries, partial results, or unexpected states. The orchestrator's value is making decisions when things don't go as planned.
   - **Inline execution logic**: Detailed task instructions for a specific sub-task written inside the orchestrator description instead of being a sub-agent route. Each distinct task should be its own route with its own description, learnings, and tools.
   - **Duplicates sub-agent descriptions**: The orchestrator restates what sub-agents do instead of focusing on dispatch logic and decision-making.
   - **No routing criteria**: The description doesn't explain WHEN or WHY to use each route — the orchestrator needs to know what conditions or inputs map to which sub-agent.

   **Browser Anti-Patterns in Description (for steps that use playwright/browser):**
   - **Prescribes browser_evaluate for interactions**: Description tells the LLM to use browser_evaluate/eval to click, fill, or navigate. Should say "take a snapshot, find the element, click/type using its ref" instead.
   - **Prescribes CSS selectors**: Description uses patterns like browser_click({'selector': '...'}) or browser_type({'selector': '...'}). Should use ref-based interaction from snapshots instead.
   - **Prescribes hardcoded element references**: Description references specific DOM selectors, iframe indices, or element IDs that may change. Should describe the intent ("find the password field", "click the login button") and let the LLM discover elements via snapshot.
   - **Over-specifies implementation**: Description dictates exact tool calls and parameters instead of describing WHAT to accomplish. For learn_code steps, the description should focus on the goal and let the LLM figure out the implementation using get_api_spec and snapshots.

   **Missing Pre-Validation Schema:**
   - **No validation_schema**: Every step that produces a context_output should have a validation_schema defined. Without it, there's no automated quality gate — a step can produce garbage output and downstream steps will blindly consume it. Check that validation_schema exists, has file checks matching the context_output filename, and includes meaningful json_checks (not just must_exist).

For each step, report:
- Step ID (and step type)
- Status: CLEAN, CONFUSED (description/skill issues), HARDCODED (hardcoded values found), WEAK_ORCHESTRATOR (for todo_task steps with orchestrator issues), BROWSER_ANTIPATTERN (prescribes evaluate/selectors instead of ref-based interaction), or NO_VALIDATION (missing or weak validation_schema) — a step can have multiple
- If issues found: which problems and a concrete fix suggestion

End with a summary table of all steps and their status.${focusText}

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with which steps had issues, the categories of confusion / hardcoding / weak orchestration / browser anti-pattern / missing validation, the recommended fixes, and items flagged for follow-up.`)
    }
  },
  {
    command: 'review-code',
    description: 'Review saved scripts (main.py) against step descriptions to detect drift',
    icon: <FileText className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const reviewLogSuffix = `\n\nREVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with which step(s) you reviewed, the drift findings, the recommendations, and items flagged for follow-up.`
      if (focus) {
        // If text before slash, treat it as a step ID
        ctx.onSubmit(`Run review_step_code(step_id="${focus}") to check if the saved main.py for step "${focus}" still matches its current description. Report any drift — missing functionality, stale behavior, hardcoded values, or output format mismatches.${reviewLogSuffix}`)
      } else {
        ctx.onSubmit(`Run review_step_code() to compare ALL learn_code steps' saved main.py scripts against their current descriptions. For each step, check if the script still does what the description says — flag missing features, stale logic, hardcoded values, and output format drift. Report findings by severity.${reviewLogSuffix}`)
      }
    }
  },
  {
    command: 'review-orchestrators',
    description: 'Review todo_task orchestrator descriptions for quality',
    icon: <AlertTriangle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer', 'run'],
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Pay special attention to: ${focus}.` : ''
      ctx.onSubmit(`Audit all todo_task steps in plan.json. For each todo_task step, read its todo_task_step description and all its predefined_routes sub-agent descriptions. Check for these problems:

**Orchestrator Description Quality:**
- **Missing objective/intent**: The orchestrator description must clearly state WHAT we are trying to achieve — the overall goal and purpose. Without this, the orchestrator can't make intelligent decisions when things go wrong or when it encounters unexpected situations. A good orchestrator description answers: "Why do these sub-agents exist together? What outcome are we after?"
- **Reduced to a sequencer**: If the description is just "run route A, then route B, then route C" or a fixed checklist, the orchestrator is being wasted. It's a capable LLM — its description should enable reasoning, not just list steps. If all it does is follow a fixed order, these should be regular steps in sequence instead.
- **No edge case / failure guidance**: The description should explain how to handle failures, retries, partial results, missing data, or unexpected states from sub-agents. What happens if a sub-agent fails? Skip it? Retry? Use a fallback? The orchestrator's core value is making decisions when things don't go as planned.
- **No routing criteria**: The description doesn't explain WHEN or WHY to pick each route. The orchestrator needs to know what conditions, inputs, or states map to which sub-agent.

**Orchestrator vs Sub-Agent Boundary:**
- **Inline execution logic**: Detailed task instructions for a specific sub-task written inside the orchestrator description. Each distinct task should be its own route with its own description, learnings, and tools. The orchestrator should dispatch, not execute.
- **Duplicates sub-agent descriptions**: The orchestrator restates what sub-agents already describe. The orchestrator should focus on coordination and decision-making, not repeat execution details.
- **Sub-agent descriptions too vague**: Sub-agent route descriptions that are too thin because all the detail is in the orchestrator. Each sub-agent should be self-contained — a junior agent reading only its own description should know exactly what to do.

**Hardcoded Values (same checks as regular steps):**
- Hardcoded paths, run/iteration paths, credentials, IDs, group names, or user-specific values in orchestrator or sub-agent descriptions.

For each todo_task step, report:
- Step ID
- Orchestrator description verdict: GOOD or issues found
- Per sub-agent route: route ID + verdict
- Concrete fix suggestions for each issue

End with a summary table.${focusText}

REVIEW LOG: append a dated entry to builder/review.md (read it first if it exists, create it if it does not) with which orchestrators you reviewed, the verdicts (good vs issues), the recommendations, and items flagged for follow-up.`)
    }
  },
  {
    command: 'build-skill',
    description: 'Build a new skill using the skill-creator',
    icon: <Lightbulb className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const currentSkills = ctx.tabConfig?.selectedSkills || []
      if (!currentSkills.includes('skill-creator')) {
        ctx.setTabConfig(ctx.activeTabId, { selectedSkills: [...currentSkills, 'skill-creator'] })
      }
      const wsStore = ctx.getWorkspaceStore()
      const expanded = new Set(wsStore.expandedFolders)
      expanded.add('skills')
      expanded.add('skills/custom')
      wsStore.setExpandedFolders(expanded)
      const skillContext = 'Refer to the skill-creator skill at skills/custom/skill-creator/SKILL.md for instructions on how to build skills.'
      const message = ctx.beforeSlash
        ? `${ctx.beforeSlash}\n\n${skillContext}`
        : `I want to build a skill based on our conversation. ${skillContext}`
      ctx.onSubmit(message)
    }
  },
  {
    command: 'build-subagent',
    description: 'Build a new sub-agent template',
    icon: <Bot className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const currentSkills = ctx.tabConfig?.selectedSkills || []
      if (!currentSkills.includes('subagent-creator') && !currentSkills.includes('custom/subagent-creator')) {
        ctx.setTabConfig(ctx.activeTabId, { selectedSkills: [...currentSkills, 'custom/subagent-creator'] })
      }
      const wsStore = ctx.getWorkspaceStore()
      const expanded = new Set(wsStore.expandedFolders)
      expanded.add('subagents')
      expanded.add('subagents/custom')
      wsStore.setExpandedFolders(expanded)
      const saContext = 'You are in Sub-Agent Builder mode. Create a new sub-agent template in subagents/custom/. Follow the SUBAGENT.md format with YAML frontmatter (name, description, default_reasoning_level) and markdown instructions.'
      const message = ctx.beforeSlash
        ? `${ctx.beforeSlash}\n\n${saContext}`
        : `I want to build a sub-agent template. ${saContext}`
      ctx.onSubmit(message)
    }
  },
  {
    command: 'add-skill',
    description: 'Import a skill from GitHub',
    icon: <Download className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.openDialog('skillImport')
    }
  },
  {
    command: 'mcp',
    description: 'View MCP server details and tools',
    icon: <Server className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.getAppStore().setWorkspaceMinimized(true)
      ctx.openDialog('mcpDetails')
    }
  },
  {
    command: 'mcp-add',
    description: 'Add or edit MCP server configuration',
    icon: <Server className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.getAppStore().setWorkspaceMinimized(true)
      ctx.openDialog('mcpConfig')
    }
  },
  {
    command: 'models',
    description: 'Open LLM model configuration',
    icon: <Cpu className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      ctx.getAppStore().setWorkspaceMinimized(true)
      ctx.openDialog('models')
    }
  },
  {
    command: 'workflow-builder',
    description: 'Turn this conversation into a reusable workflow (Workflow/<name>/)',
    icon: <Layers className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const instruction = `Turn our current conversation into a new reusable workflow by calling the \`create_workflow\` tool with a valid workflow.json and plan.json.

## Step 1 — Pick a folder_name AND a display label
Workflows have two separate names:
- **folder_name** (the on-disk path under \`Workflow/\`) — must be **shell-safe kebab-case**: lowercase letters/digits with hyphens between words, no spaces, no underscores, no uppercase, no special characters (e.g. "customer-onboarding", "sales-report", "api-health-check"). 2-5 words, ≤64 chars.
- **label** (the human-readable display name that goes in \`workflow_json.label\`) — can be any string: spaces, capitalization, punctuation, whatever reads naturally (e.g. "Customer Onboarding", "AWS Cost Analysis Q3", "Müller's Pipeline").

If I gave you a label in my preamble, keep it verbatim as the \`label\` and slugify it for the \`folder_name\`. If I gave you a kebab-case name, use it for \`folder_name\` and also as the starting point for \`label\` (titlecased). Otherwise infer both from what we've been working on. If you cannot produce a clean folder_name, ask me one clarifying question instead of proceeding.

## Step 2 — Pick the capabilities from context
Analyze this conversation and select ONLY the MCP servers, skills, and LLM tier settings that are actually relevant to the workflow being extracted. **Do not blindly copy every currently-enabled server and skill — pick the ones the steps actually need.** If a server was enabled in chat but never used for this specific work, leave it out.

## Step 3 — Extract the steps
Re-read the conversation and extract the concrete, repeatable steps the workflow should run. Each step must have:
- A stable kebab-case \`id\` (e.g. "fetch-data", "analyze-results"), unique within the plan
- A human \`title\`
- A detailed \`description\` of what the step does, in enough detail that a worker with no memory of this conversation could execute it
- A \`success_criteria\` line describing how to tell the step succeeded
- Optionally \`context_dependencies\` (file names produced by earlier steps) and \`context_output\` (file name this step produces)
- Most steps should use \`"type": "regular"\`. Use \`"conditional"\` / \`"routing"\` / \`"human_input"\` / \`"todo_task"\` only when the conversation genuinely called for branching or human-in-the-loop.

## Step 4 — Call create_workflow
Build the two JSON objects yourself in this turn and call the privileged tool:

\`create_workflow(folder_name: "<kebab-name>", workflow_json: {..., label: "<human-readable>", ...}, plan_json: {...})\`

**IMPORTANT**: Use the \`create_workflow\` tool — do NOT try to \`mkdir\` or write files with shell commands. The \`Workflow/\` folder is read-only to normal shell writes; \`create_workflow\` is the only path that can create a new workflow folder. The tool validates folder_name (shell-safe kebab-case), enforces required JSON fields, refuses to overwrite existing workflows, and writes both files in one call.

The workflow.json schema (required: schema_version, id, label) and the plan.json schema (required: steps array with type/id/title) are already documented in your system prompt — follow that shape exactly.

## Step 5 — Report back to me
After the tool returns, tell me:
- The folder path returned by the tool
- The display label
- A one-line summary of what the workflow does
- The step IDs + titles (numbered list)
- Tell me I can pick it from the workflow picker to activate it.`

      const message = ctx.beforeSlash
        ? `${ctx.beforeSlash}\n\n${instruction}`
        : instruction

      ctx.onSubmit(message)
    }
  },
  // ===== Auto-improvement framework =====
  // See docs/workflow/auto_improvement_framework.md.
  // Note: business-context capture is intentionally NOT a slash command.
  // The builder agent's system prompt teaches it to recognize business
  // rules in conversation and offer to persist them via the
  // capture_context tool. A separate slash command would be redundant.
  {
    command: 'improve-setup-framework',
    description: 'One-time setup: write the Workflow Profile to improve.md and bootstrap metrics for the auto-improvement framework',
    icon: <Wrench className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Set up the auto-improvement framework on this workflow. One-time configuration: write a "## Workflow Profile" section into builder/improve.md (the durable behavioral metadata the agent reads on every turn), set the two hard-gate fields in workflow.json (oversight_mode + decision_log_mutability), and bootstrap an initial metrics.json so /improve-* and the experiment loop have something concrete to work with.${focus ? `\n\nFocus / hints from user: ${focus}` : ''}

DISCOVERY (read-only)
1. Read workflow.json. Note any existing oversight_mode / decision_log_mutability.
2. Read builder/improve.md if present — note any existing "## Workflow Profile" section.
3. Read soul/soul.md to extract the workflow's objective and success_criteria.
4. Read planning/plan.json — note the steps, their types, and overall structure (frozen plan vs in flux vs explore/exploit).
5. Read evaluation/evaluation_plan.json if present — eval steps will be the natural source for many starter metrics.
6. Read <workflow>/planning/metrics.json if present.
7. Read runs/ to see how mature the workflow is.

STEP 1 — Classify the workflow profile
Walk the user through the four axes. Real workflows mix them; do not force a single enum.

- **Plan stability** — \`mutable\` (plan changes freely), \`ratchet\` (additions only — compliance, security), \`frozen\` (no plan-shape change without explicit user OK).
- **Runtime mode** — \`single\` (one plan, runs as-is) vs \`dual\` (alternates explore / exploit; e.g. social-media trying new tactics weekly then exploiting the winner).
- **Business context accumulation** — does the workflow accumulate user-supplied business rules (audit clauses, ICP filters, risk constraints)? \`yes\` for Type-3-style workflows; \`no\` for QA suites and pure exploratory creative.
- **Improvement cadence** — how often is this workflow expected to improve? Daily / weekly / per-incident / quarterly / never (frozen).

Show your inference + reasoning + the alternative answers you considered for each axis. Ask the user to confirm.

STEP 2 — Write the Workflow Profile to builder/improve.md
Append (or replace, if a section already exists) the following section in builder/improve.md. Use \`diff_patch_workspace_file\` — do NOT \`mkdir\` via shell. Use workflow-relative paths.

\`\`\`markdown
## Workflow Profile (auto-improvement framework)

- **Plan stability**: <chosen> — <one-line rationale>
- **Runtime mode**: <single | dual> — <one-line rationale; if dual, name the modes: e.g., "explore (weekly reset) / exploit (daily default)">
- **Business context**: <accumulating | none> — <one-line rationale>
- **Improvement cadence**: <chosen> — <one-line rationale>

Behavioral implications the agent should respect on every turn:
- <plan-stability implication, e.g. "Do not call replan_workflow_from_results or delete_plan_steps without explicit user approval.">
- <runtime-mode implication, e.g. "When dual: read metrics.json for active_mode; honor it in step branching.">
- <business-context implication, e.g. "Recognize user-supplied rules in conversation and offer capture_context.">
\`\`\`

STEP 3 — Set the two hard-gate fields in workflow.json
These are the only structured framework fields; they drive real behavior.

- \`oversight_mode\` — \`manual\` / \`supervised\` (default) / \`autonomous\`. Recommended defaults: deterministic + ratcheting workflow → \`manual\`; exploratory → \`autonomous\`; contextual / business-context → \`supervised\`.
- \`decision_log_mutability\` — \`append_only\` (default) / \`append_only_strict\`. Set strict ONLY for compliance / audit workflows where the decision log is forensic.

STEP 4 — Bootstrap metrics.json
Behavior depends on the profile from Step 1:

- Plan stability \`mutable\` + business context \`none\`: tell the user outcome metrics can be deferred. Track per-eval-step trajectories instead. **But still propose the two universal telemetry SLOs below** — they're free and catch cost/runtime regressions while exploring.
- Plan stability \`ratchet\`/\`frozen\` + business context \`none\` (e.g. QA suite, ETL): propose 3–5 SLO-mode metrics — success-rate (floor), \`cost_per_run\` (ceiling), \`run_duration_seconds\` (ceiling), data freshness. Source: \`telemetry\` for cost/duration, \`eval_step\` for the rest.
- Business context \`accumulating\`: REQUIRED. Propose 3–5 outcome + rule-conformance metrics derived from success_criteria, **plus the two universal telemetry SLOs**. Mix outcome metrics (mode=\`target\`) with cost / runtime SLOs.
- Runtime mode \`dual\`: also include at least one metric per mode (e.g. \`explore.discovery_rate\`, \`exploit.win_rate\`) so the experiment loop can tell whether mode flips actually help.

**Two universal telemetry SLOs** — propose these on EVERY workflow regardless of profile (the framework collects them automatically; ignoring them means missing free signal):
- \`cost_per_run\` — unit \`usd\`, direction \`lower_better\`, mode \`slo\`, ceiling chosen based on the workflow's expected complexity (start at 0.50 USD for short workflows, 5.00 USD for long ones; user can adjust). Source: \`{ type: telemetry, field: "run.total_cost_usd" }\`.
- \`run_duration_seconds\` — unit \`seconds\`, direction \`lower_better\`, mode \`slo\`, ceiling chosen based on the workflow (300s for short, 1800s for long). Source: \`{ type: telemetry, field: "run.duration_seconds" }\`.

For each proposed metric, show: id, label, unit, direction, mode, target/floor/ceiling, source, AND \`linked_success_criteria\` — the entries (or short labels) from soul/soul.md's "## Success Criteria" section that this metric operationalizes. Outcome metrics MUST link to at least one success criterion; the two universal telemetry SLOs (\`cost_per_run\`, \`run_duration_seconds\`) leave \`linked_success_criteria\` empty (telemetry is auxiliary, the popup flags them as "unanchored" by design — that's correct, not a warning the user must resolve). The linkage is what closes the Goodhart loop: an experiment moves a metric, the metric is traced to a criterion, the criterion is the user-facing outcome. Read soul/soul.md once at the start of this step to enumerate the criteria; quote them verbatim in \`linked_success_criteria\` so the trace is auditable. Rationale per metric should explicitly say which criterion it serves and how. Ask the user which to keep / drop / amend. For each accepted, call \`propose_metric\` with \`linked_success_criteria\` populated.

STEP 5 — Business-context only — scaffold the rules folder
If business context = accumulating:
- Use \`diff_patch_workspace_file\` to create knowledgebase/rules/rules.md with the standard preamble. The first \`capture_context\` tool call would auto-create it; doing it now gives the user a visible artifact and lets you write metric-keyed section headings.
- Create one \`## <section>\` heading per metric grouping. Future user rules will land under these.

STEP 6 — Runtime mode dual — initialize active_mode
If runtime_mode = dual: ask the user which mode the workflow should start in (typically explore for new workflows, exploit for mature ones). Then write the \`active_mode\` field into metrics.json via \`propose_metric\`'s sibling tool, or via direct file edit if no tool exists yet.

STEP 7 — Confirm and tell the user what's next
Print a clean summary:
- Workflow Profile written to builder/improve.md (paste the section)
- workflow.json: oversight_mode = <chosen>, decision_log_mutability = <chosen>
- metrics: <list of ids with units and targets>
- If business context: knowledgebase/rules/rules.md scaffolded with sections <list>
- If dual mode: metrics.json::active_mode = <chosen>

Then tell them:
1. They can now run /improve-eval, /improve-workflow, /improve-continuously and the framework precheck will pass.
2. If business context: they can mention rules in chat and the agent will offer to persist via capture_context.
3. Hypotheses they want to test should be opened as experiments via /improve-* or in conversation.

DO NOT do any actual workflow improvement here. /improve-setup-framework is setup only.

IMPROVE LOG: the Workflow Profile written in Step 2 IS the durable setup record. Below the profile, append a small dated entry recording the metrics created and what the user should run next. Future improvement turns will read both.`)
    }
  },
  {
    command: 'propose-experiment',
    description: 'Open ONE experiment: pick a metric, formulate a hypothesis, apply the intervention through the framework gate',
    icon: <Beaker className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? `\nFocus / hint from user: ${focus}` : ''
      ctx.onSubmit(`Open exactly one experiment that tests a falsifiable hypothesis against a declared metric. The framework's job is to surface non-obvious improvements the user is NOT thinking about — not to incrementally harden what's already there. /review-* commands are for small fixes the user reviews; this command (and the experiment loop) is for AI-proposed changes that move outcomes the user cares about.${focusText}

MENTAL MODEL
Think like a sharp business analyst auditing this workflow's actual outputs against its success criteria — not like a senior engineer reviewing code. These are business-process workflows, not software systems. The kinds of changes that surface here are things a domain expert would notice when reading what the workflow produced:
- "Every Twitter reply has the same tone, but the success criteria mention engaging different audience segments — segment by follower type and vary voice."
- "The workflow researches every prospect from scratch, but 40% of last month's runs were repeats — cache and refresh deltas instead."
- "Outreach copy leads with our product; the high-converting examples in run history all led with the prospect's pain point."
- "Validation accepts any non-empty reply. Half the replies in run history are 'thanks' — that's not engagement, raise the bar."
You should be uncomfortable with how obvious-in-retrospect the change feels after you read enough run output. That's the right mode.

PRECHECKS
1. Read <workflow>/planning/metrics.json. If empty or missing, stop and redirect: "Run /improve-setup-framework first to bootstrap metrics — propose_experiment requires at least one declared metric to target."
2. Read builder/improve.md. If there is no "## Workflow Profile" section, stop and redirect: "Run /improve-setup-framework first."
3. Read experiments/active.json. If 3+ experiments are already active, warn the user and ask whether to proceed (concurrent experiments on related steps confound attribution).

DISCOVER (this is the heart of the command)
1. Read soul/soul.md's "## Success Criteria" section — these are the north star.
2. **Read run outputs first, plan second.** Open the latest meaningful iteration under runs/ and read what the workflow actually PRODUCED — generated copy, sent messages, written reports, scored decisions. Compare those outputs against the success criteria as a domain expert would. Where's the gap?
3. Read evaluation reports for the same iteration — what scored poorly and why? The eval rationale text is often the richest signal.
4. Skim decisions.jsonl — what has the user been asking for? What's been tried before? Avoid re-proposing things that already failed.
5. Only after steps 2–4, look at planning/plan.json and step descriptions. Use the plan to understand structure; do NOT use it as the primary source of "what's wrong." Plans look fine on paper while outputs reveal the rot.
6. Surface 3–5 candidate hypotheses ranked by expected impact. Each candidate must be defensible by something specific in the run outputs ("in iteration-3/group-a, posts 7, 12, 19 all got <2 engagement and all share <pattern>"), not by abstract reasoning about the plan.

PICK A TARGET METRIC
1. List metrics from planning/metrics.json with their current trajectory and \`linked_success_criteria\`. For each, note: which success criterion it operationalizes, whether it's on target / off target / no recent data.
2. Pick exactly ONE metric to target. Prefer in this order:
   a) a metric the user named in their focus hint
   b) an OUTCOME metric (non-empty linked_success_criteria) whose criterion is currently failing
   c) an OUTCOME metric whose recent trajectory is drifting off target
   d) a TELEMETRY metric (cost_per_run / run_duration_seconds) only if its SLO is being violated AND no outcome metric is failing.
3. State explicitly: "this experiment targets metric <id>, which operationalizes success criterion: <quoted criterion>." If you can't fill that sentence in honestly, pick a different metric (or define one via propose_metric — populate linked_success_criteria from soul.md).

PICK ONE HYPOTHESIS FROM THE CANDIDATES
1. From the candidates surfaced in DISCOVER, pick the one with the highest expected impact on the chosen metric, grounded in the most concrete run-output evidence.
2. The change does NOT have to be small. Multi-file changes are fine if they share ONE underlying business belief. Examples of a coherent multi-file bundle: "personalize outreach by reading prospect's last post + change validation to require pain-point reference + add a fallback when no signal is available" — three files, one belief ("our outreach is too generic"). Examples of an INCOHERENT bundle that should be split: "add personalization AND reduce step 4's temperature AND fix the typo in step 7's prompt" — three unrelated beliefs, three experiments.
3. The single-belief test: write the hypothesis in one sentence first. If you need an "and" to connect two distinct claims, those are two experiments.
4. Bundled changes are recoverable: if the verdict is reverted, the framework restores every byte of \`intervention_changes\` atomically. Bigger blast radius is okay because it's auto-reversible. Optimize for "the experiment will tell us something useful" — not for "the change is tiny."

WRITE IT
1. Hypothesis (≤200 chars) in the form: "<change> will <direction> <metric_id> by ≥<magnitude><unit> because <one-line mechanism rooted in run-output evidence>." Example: "Switching outreach copy to lead with prospect pain point will increase outreach.reply_rate by ≥5pp because run history shows pain-led posts converted 4× more often."
2. expected_direction must match the metric's declared direction.
3. expected_magnitude is absolute, in the metric's unit, > 0.
4. intervention_changes: array of { path, operation, content }. Each path must be in experiments/config.json::allowed_intervention_paths. .env, .git/, and workflow.json are forbidden.

CALL THE TOOL
Call propose_experiment with the fields above. The framework captures the revertable diff, applies the changes, opens the measurement window, and returns { experiment_id, status, decisions_entry_id, linked_success_criteria, unanchored_metrics }.

REPORT
- Echo experiment_id, status, target metric, expected direction/magnitude.
- Restate the one belief the experiment is testing in plain English.
- Name the linked success criterion the metric operationalizes.
- Tell the user what to do next: in oversight=manual, run /exp-extend or wait for /improve-approve; in supervised/autonomous, the next workflow runs will populate measurement values automatically.

IMPROVE LOG: append a dated entry to builder/improve.md with the experiment_id, the one belief tested, the run-output evidence that surfaced it, target metric, expected direction/magnitude. The framework also records the proposal in decisions.jsonl automatically — improve.md is for the narrative, decisions.jsonl is the audit trail.

IMPROVE LOG: append a dated entry to builder/improve.md with the experiment_id, hypothesis, target metric, expected direction/magnitude, and a one-line note on why this hypothesis. The framework also records the proposal in decisions.jsonl automatically — improve.md is for the narrative, decisions.jsonl is the audit trail.`)
    }
  },
  {
    command: 'exp-abort',
    description: 'Revert and abort the active experiment',
    icon: <AlertTriangle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Abort the active experiment and revert its intervention. Use shell + file primitives — there is no dedicated tool for this and you should NOT call any HTTP API.${focus ? `\n\nReason: ${focus}` : ''}

DISCOVERY
1. Read experiments/active.json. List active experiments. If multiple, ask the user which one to abort.
2. Confirm the user wants to abort (this rolls back the intervention via the captured revertable_diff).

REVERT THE INTERVENTION
The experiment record carries \`revertable_diff\` — a JSON envelope listing each intervention path with its pre-intervention content. To revert:
1. For each entry in \`revertable_diff.changes\` (or whatever the field is named on the record): use \`diff_patch_workspace_file\` to restore that path's pre-intervention content. Use \`operation: "create"\` if the file didn't exist before the experiment, otherwise \`"replace"\` with the saved content.
2. Verify each restored file matches the saved content before moving on.

ARCHIVE THE EXPERIMENT
3. Append the experiment record to experiments/history.jsonl as a single JSON line. Set:
   - \`status\`: "aborted"
   - \`concluded_at\`: ISO-8601 UTC now
   - \`conclusion.verdict\`: omit or null (aborts have no verdict)
   - \`conclusion.rationale\`: the user's reason
4. Use \`diff_patch_workspace_file\` to remove the experiment from experiments/active.json (the JSON object's \`experiments\` array). Re-marshal the file with the entry removed; do NOT just delete the line.

AUDIT
5. Append one line to builder/decisions.jsonl:
   {"id":"<short-id>","ts":"<ISO-8601 UTC>","source":"user","trigger":"exp-abort","applied_changes":[<paths restored>],"linked_experiment_id":"<id>","rationale":"<user's reason>"}

REPORT
- Confirm the experiment is gone from active.json and present in history.jsonl with status=aborted.
- List the files restored.
- Echo the decisions.jsonl entry id.`)
    }
  },
  {
    command: 'exp-extend',
    description: 'Add more measurement runs to the active experiment',
    icon: <RefreshCw className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Extend the active experiment's measurement window. Use shell + file primitives — no API call.${focus ? `\n\nFocus / why: ${focus}` : ''}

DISCOVERY
1. Read experiments/active.json. List active experiments. If multiple, ask the user which one.
2. Ask the user how many additional runs to add (default = workflow's default_measurement_runs from planning/experiments/config.json).

ACTION
3. Use \`diff_patch_workspace_file\` on experiments/active.json:
   - Find the experiment's record.
   - Bump \`measurement.target_runs\` by the additional run count.
   - If \`status\` is "evaluating", flip it back to "measuring" (the verdict computer will re-fire when target_runs is hit again).
   - Re-marshal the JSON cleanly.
4. Append one line to builder/decisions.jsonl:
   {"id":"<short-id>","ts":"<ISO-8601 UTC>","source":"user","trigger":"exp-extend","linked_experiment_id":"<id>","rationale":"extend by <N> runs: <user reason>"}

REPORT
- New target_runs.
- Status after the edit (back to "measuring" if it was "evaluating").
- Decisions entry id.`)
    }
  },
  {
    command: 'exp-conclude',
    description: 'Manually render a verdict for the active experiment (overrides evaluator)',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Manually conclude the active experiment. Use shell + file primitives — no API call.${focus ? `\n\nFocus / reason: ${focus}` : ''}

This is the OVERRIDE path. Prefer letting the evaluator agent narrate the system-computed verdict. Use this only when you genuinely believe the heuristic is wrong (large world drift, broken eval, mistaken metric, or to early-conclude an experiment whose direction is obvious).

DISCOVERY
1. Read experiments/active.json. List active experiments and confirm the id with the user.
2. Decide the verdict with the user: kept | reverted | inconclusive | extend.
3. Write the rationale (≤500 chars) and the override reason.

REVERT (only if verdict = reverted)
If verdict=reverted, restore the intervention's files first:
- For each entry in the experiment record's \`revertable_diff\`, use \`diff_patch_workspace_file\` to write the saved pre-intervention content back. Use \`operation: "create"\` for files that didn't exist before, \`"replace"\` for files that did.

ARCHIVE THE EXPERIMENT
4. Append the experiment record to experiments/history.jsonl as a single JSON line. Set:
   - \`status\`: "concluded"
   - \`concluded_at\`: ISO-8601 UTC now
   - \`conclusion.verdict\`: <chosen verdict>
   - \`conclusion.rationale\`: <prose rationale>
   - \`conclusion.verdict_overridden\`: true
   - \`conclusion.override_reason\`: <user's override reason>
5. Use \`diff_patch_workspace_file\` to remove the experiment from experiments/active.json (the JSON object's \`experiments\` array). Re-marshal the file cleanly.

AUDIT
6. Append one line to builder/decisions.jsonl:
   {"id":"<short-id>","ts":"<ISO-8601 UTC>","source":"user","trigger":"exp-conclude","applied_changes":[<paths restored if any>],"linked_experiment_id":"<id>","rationale":"manual conclude: verdict=<v>; <rationale>","target_metrics":[<exp.target_metrics>]}

REPORT
- Final verdict.
- Whether it was archived to history.jsonl.
- If verdict=reverted, list the files restored.
- Decisions entry id.`)
    }
  },
  {
    command: 'enrich-memory',
    description: 'Distil recent chats into memory and consolidate (deletes chats older than 7 days)',
    icon: <Minimize2 className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const msg = ctx.beforeSlash
        ? `Enrich my memory, focusing on: ${ctx.beforeSlash}. Use enrich_memory — extract insights from chat_history into memories, then consolidate. Delete chat sessions older than 7 days.`
        : 'Enrich my memory. Use enrich_memory to extract insights from every session in chat_history into today\u2019s date folder + entity files, then consolidate all memories and regenerate index.md. Delete chat sessions older than 7 days.'
      ctx.onSubmit(msg)
    }
  }
]
