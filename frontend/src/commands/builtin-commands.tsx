import React from 'react'
import { FileText, Lightbulb, Download, Server, Cpu, Bot, Layers, Minimize2, AlertTriangle, RefreshCw, Wrench, GitBranch, CheckCircle, Search, Lock, Database, Network } from 'lucide-react'
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
- Suggested fixes for each issue`)
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
- If READY with WARNs — "Ready but recommended to fix these first"`)
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

This is a plan/design review. Use review_workflow_results() when the question is whether a real run is actually achieving the goal and whether eval measures that properly.`)
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
- the top next actions, clearly separated into workflow fixes vs eval fixes.`)
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
- the top next actions, with expected impact and risk to success criteria.`)
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
- the top next actions, with expected savings and risk to success criteria.`)
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
List the exact update_step_config calls the user can copy/paste to apply each recommendation, one per line. Group by recommendation type (KB updates, lock updates) so the user can apply them selectively. Do NOT call update_step_config yourself.`)
    }
  },
  {
    command: 'improve-kb',
    description: 'Review and improve the knowledgebase using the current graph, plan, and run evidence',
    icon: <Network className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? `\nFocus especially on: ${focus}.` : ''
      const runText = runFolder
        ? `Use the selected run folder "${runFolder}" when recent run evidence helps explain KB drift or missing contributions.`
        : 'If recent run evidence is needed to explain KB drift or missing contributions, find the latest meaningful run first.'
      ctx.onSubmit(`Review and improve the workflow knowledgebase. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your KB findings and applied decisions when you finish.${focusText}

DISCOVERY
1. Read soul/soul.md to understand the objective and success criteria.
2. Read planning/plan.json and planning/step_config.json so you understand which steps should read from or contribute to the KB.
3. Read knowledgebase/notes/_index.json and skim the topic files that look relevant.
4. ${runText}

DECISION
Assess whether the KB needs:
- no change
- targeted notes reorganization
- cross-step consolidation
- both

Look for:
- duplicate or overlapping topics
- topic-name drift
- stale narrative that no longer matches current step outputs
- contested claims across topics
- missing step contributions that should have been persisted
- notes structure that no longer matches the workflow objective

ACTION
- Use reorganize_knowledgebase when the notes structure itself needs cleanup or normalization.
- Use consolidate_knowledgebase when cross-step consolidation or canonicalization is needed.
- Automatically APPLY high-confidence, evidence-backed KB improvements instead of only listing recommendations.
- Be conservative and bounded; do not ask for human confirmation during this improve run.

When you finish, update builder/improve.md with:
- what KB evidence you reviewed
- the main KB weaknesses you found
- what was reorganized or consolidated
- what was applied vs deferred
- remaining KB gaps`)
    }
  },
  {
    command: 'improve-learnings',
    description: 'Review and improve shared learnings using current learnings, plan, and run evidence',
    icon: <Lightbulb className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const runFolder = ctx.getWorkflowStore().selectedRunFolder
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? `\nFocus especially on: ${focus}.` : ''
      const runText = runFolder
        ? `Use the selected run folder "${runFolder}" when recent run evidence helps explain stale, missing, or duplicated learnings.`
        : 'If recent run evidence is needed to explain stale, missing, or duplicated learnings, find the latest meaningful run first.'
      ctx.onSubmit(`Review and improve workflow learnings. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your learnings findings and applied decisions when you finish.${focusText}

DISCOVERY
1. Read soul/soul.md to understand the objective and success criteria.
2. Read planning/step_config.json to inspect learning_objective coverage across steps.
3. Read learnings/_global/SKILL.md and references/*.md when present.
4. Inspect relevant step learnings when current failures, duplication, or missing knowledge appear to be step-specific.
5. ${runText}

DECISION
Assess whether learnings need:
- no change
- targeted cleanup
- holistic consolidation
- promotion of repeated step-specific lessons into _global references

Look for:
- duplicated lessons
- stale guidance
- missing guidance for declared learning objectives
- repeated run fixes that should become durable learnings
- step-specific learnings that really belong in _global

ACTION
- Use organize_global_learnings when the learnings set needs cleanup, consolidation, or promotion into shared references.
- Automatically APPLY high-confidence, evidence-backed learnings improvements instead of only listing recommendations.
- Be conservative and bounded; do not ask for human confirmation during this improve run.

When you finish, update builder/improve.md with:
- what learnings evidence you reviewed
- the main learnings weaknesses you found
- what was reorganized or promoted
- what was applied vs deferred
- remaining learnings gaps`)
    }
  },
  {
    command: 'improve-report',
    description: 'Validate reports/report_plan.json and suggest layout/color improvements',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
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
      ctx.onSubmit(`Review and improve evaluation/evaluation_plan.json in four passes. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your eval findings and applied decisions when you finish.${focusLine}

PASS 0 — FRAMEWORK BOOTSTRAP (auto-improvement)
**0a — workflow type.** Read workflow.json. If \`workflow_type\` is unset (or still the default \`exploratory\` and the workflow is clearly past the exploration phase), classify it first — type drives which metrics make sense and how this command should behave.
   - **deterministic** — same steps every run (QA suites, ETL pipelines, scheduled scrapes). Eval is mechanical; metrics are SLOs.
   - **exploratory** — plan structure still in flux; you don't know what to measure yet. Often defer metrics.
   - **contextual** — stable plan with growing user-supplied business rules (audits, lead-gen, trading, AWS-cost-sec). Metrics are required and load-bearing.
   Show the user your inference + reasoning, ask to confirm, and update workflow.json to set \`workflow_type\` (and \`oversight_mode\` if unset; default \`supervised\`).

**0b — metrics.json.** Read <workflow>/metrics.json. If absent or empty, eval improvement is much weaker without metrics. Bootstrap based on the type from 0a:
   - Type 1 (deterministic): SLO-mode metrics — success-rate (floor), cost-per-run (ceiling), latency (ceiling), data freshness.
   - Type 2 (exploratory): you can defer; just track eval-step trajectories. Skip the bootstrap unless the user explicitly asks for metrics.
   - Type 3 (contextual): REQUIRED. Outcome metrics + rule-conformance + cost/latency SLO. Do not skip.

   For Type 1 and Type 3:
   a. Read soul/soul.md, planning/plan.json, evaluation/evaluation_plan.json.
   b. Propose 3–5 starter metrics with unit, direction, mode, and source (\`eval_step\` for existing eval steps, \`telemetry\` for cost/latency).
   c. Show the user with one-line rationales. Ask which to keep / drop / amend.
   d. For each accepted metric, call propose_metric.
3. If both \`workflow_type\` and metrics.json (where required) are already populated, skip the bootstrap and continue.

PASS 1 — VALIDATION
1. Call validate_evaluation_plan.
2. For each error: explain what's wrong in plain language, show the eval step/widget/field it refers to, and propose the exact fix.
3. For warnings: separate correctness-risk warnings from lower-priority quality issues.

PASS 2 — GOAL / SUCCESS-CRITERIA ALIGNMENT
1. Read soul/soul.md and extract the objective and success criteria.
2. Read planning/plan.json so you understand what the workflow is actually producing.
3. ${runText}
4. If existing run evidence is available, use review_workflow_results() or inspect the existing run/eval outputs to judge:
   - which success criteria are directly measured
   - which are only weakly or indirectly measured
   - which are not measured at all
   - whether any eval checks give false confidence or miss obvious failure modes

PASS 3 — IMPROVEMENT SUGGESTIONS
Propose improvements in these categories:
1. **Coverage**: does each important success criterion have a clear eval step?
2. **Directness**: is the eval checking the actual desired outcome, or only proxies?
3. **Determinism**: are any eval steps too vague, subjective, or hard to reproduce?
4. **Redundancy**: are multiple eval steps measuring the same thing with little added value?
5. **Thresholds / scoring**: are pass/fail thresholds or scores aligned with the stated success criteria?
6. **Reality check**: if existing run evidence shows obvious failure or success, does the eval report reflect that honestly?

Show ALL proposed changes as a diff (before/after snippets per eval step) before editing. Ask whether to apply all, some, or none. Don't edit evaluation/evaluation_plan.json until I confirm.

When you finish, update builder/improve.md with:
- what workflow/eval evidence you reviewed
- the main eval weaknesses you found
- what you recommended
- what was applied vs deferred`)
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

5. **Framework bootstrap** — workflow_type FIRST, then metrics. A continuous-improvement schedule with no type and no metrics will optimize nothing concrete:
   a. Read workflow.json. If \`workflow_type\` is unset, classify and confirm with the user (deterministic / exploratory / contextual). Update workflow.json with the chosen type and \`oversight_mode\` (default \`supervised\`).
   b. Read <workflow>/metrics.json. Bootstrap based on type:
      - Type 1 → SLO metrics (success-rate, cost, latency, freshness)
      - Type 2 → optional; the optimizer can run without metrics on Type 2 by tracking eval-step trajectories
      - Type 3 → REQUIRED. Outcome + rule-conformance + cost SLO
      For Type 1 / Type 3, propose 3–5 starter metrics, confirm with the user, call propose_metric for each.
   The optimizer schedule's message below will reference \`workflow_type\` and the metrics; do not skip this step.

SCHEDULE STRATEGY
1. Prefer updating or reusing good existing schedules instead of creating duplicates.
2. Only create a new schedule when there is no existing schedule that already serves that purpose.
3. The improve schedule must be LESS frequent than the run schedule.
4. If cadence is not obvious:
   - choose a practical recurring run cadence based on the workflow objective and any existing schedules
   - choose a larger/slower cadence for optimizer improvement
   - stay conservative if the workflow does not appear highly time-sensitive
5. Preserve a good existing timezone if one is already in use. Otherwise use the workflow's local/current timezone.

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

The optimizer schedule message should explicitly do the following:
- read builder/improve.md if it exists and treat it as the prior improvement log / decision history
- read soul/soul.md, planning/plan.json, evaluation/evaluation_plan.json, and current schedules
- review the latest meaningful run and evaluation evidence
- take decisions in continuity with builder/improve.md unless newer evidence clearly contradicts it
- use the same improvement logic as the improve commands for workflow, eval, and report when relevant
- automatically APPLY high-confidence, evidence-backed improvements instead of only listing recommendations
- do not wait for human confirmation during the scheduled improve run; be conservative and bounded instead
- improve the evaluation plan when objective/success-criteria coverage is weak or misleading
- improve the report plan when the rendered report is weak, misleading, or clearly behind the workflow's current outputs
- improve the workflow using existing evidence first; only run verification when genuinely needed
- update builder/improve.md with timestamp, evidence reviewed, schedule context, workflow changes, eval changes, report changes, what was applied automatically, remaining gaps, and next hypotheses${improveMessageFocus}

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
      ctx.onSubmit(`Your single goal: improve this workflow end-to-end with the minimum necessary changes, starting from existing run evidence. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and update it with your decisions at the end. Review the current iteration first, then replan or harden from that evidence. Only run a fresh verification pass if it is actually needed.${focusText}

SETUP
1. Read planning/plan.json to extract the objective and success_criteria. These are the north star for every decision.
2. Read evaluation/evaluation_plan.json so you understand what the eval is measuring.
3. Read variables.json to get the enabled group names.

4. **Framework bootstrap** — workflow_type FIRST, then metrics:
   a. Read workflow.json. If \`workflow_type\` is unset (or still default \`exploratory\` and the workflow is clearly past exploration), classify and confirm with the user:
      - **deterministic** — same steps every run; eval is mechanical; metrics are SLOs
      - **exploratory** — plan structure still in flux; defer metrics
      - **contextual** — stable plan with growing user-supplied rules; metrics required
      Update workflow.json to set \`workflow_type\` and \`oversight_mode\` (default \`supervised\`) once confirmed.
   b. Read <workflow>/metrics.json. Bootstrap based on the type from 4a:
      - Type 1 → SLO metrics (success-rate floor, cost ceiling, latency ceiling, freshness)
      - Type 2 → can defer; just track eval-step trajectories
      - Type 3 → REQUIRED. Outcome metrics + cost/latency SLO
      For Type 1 / Type 3: propose 3–5 starter metrics derived from success_criteria + existing eval steps. Show with rationales, ask user which to keep, then call propose_metric for each. Without metrics, harden_workflow and replan_workflow_from_results have no concrete target — the hardening loop devolves into vibes.

5. ${iterationHint} Treat that iteration as the default evidence set for this command run.

PHASE 1 — STRUCTURAL DIAGNOSIS
1. Call optimize_workflow(${focus ? `focus="${focus}"` : ''}).
2. Read the optimize_workflow result carefully and classify the findings:
   - **Structural**: missing steps, redundant steps, wrong ordering, wrong step type, broken context flow.
   - **Non-structural**: weak prompts, weak validation, step reliability issues, tool/config issues.
3. If the findings show a MATERIAL structural problem and you have real run evidence, call replan_workflow_from_results(iteration="{starting_iter}"${focusArg}) ONCE before doing the hardening review.
4. If the findings are minor or mostly non-structural, skip replanning and keep the current structure.
5. Do not thrash the plan. At most one structural replan in this command run.

PHASE 2 — PER-GROUP EVIDENCE REVIEW → HARDEN
Repeat the following for each enabled group, sequentially, using the selected/latest iteration as the default evidence set.

For group {group}:
  a. **REVIEW EXISTING EVIDENCE** — inspect this group's existing outputs, logs, validation failures, and evaluation report for "{starting_iter}/{group}".
     - If the workflow run exists but the evaluation report is missing, you MAY call run_full_evaluation(target_run_folder="{starting_iter}/{group}") to score the existing outputs. Do NOT execute a fresh workflow run in this phase.
     - If there is no meaningful run evidence for this group in the chosen iteration, report that gap clearly and continue with the groups that do have evidence.
  b. **DECIDE** — based on the existing evidence:
     - If issues are structural and cannot be fixed by hardening a step, and you have not already replanned in this command run, call replan_workflow_from_results(iteration="{starting_iter}", group_name="{group}"${focusArg}), then continue reviewing the remaining groups against the updated plan.
     - Otherwise call harden_workflow(iteration="{starting_iter}", group_name="{group}"${focusArg}) and wait for it to finish.
  c. **BE CONSERVATIVE** — prefer harden_workflow for reliability fixes; use replanning only when the workflow shape is actually wrong.

PHASE 3 — VERIFY THE IMPROVEMENT
1. After all groups have been reviewed, compare:
   - structural issues found in Phase 1
   - per-group existing run outcomes
   - per-group existing eval results
   - harden/replan changes applied
2. If the workflow still misses key success criteria and the cause is clearly fixable within one more pass, do ONE targeted verification pass on the highest-value group only:
   - run_full_workflow(enabled_group_names=["{group}"])
   - run_full_evaluation(target_run_folder="iteration-0/{group}")
3. Do not loop indefinitely. Maximum one extra verification pass.
4. Do not run a fresh workflow pass unless verification is genuinely needed to confirm the improvements.

FINAL REPORT
Summarize:
- Structural diagnosis from optimize_workflow
- Whether replan_workflow_from_results was used, and why
- Per-group: what existing evidence was reviewed, whether eval had to be filled in, harden changes, steps newly marked optimized
- Which success criteria are now better satisfied than before
- Remaining gaps that still need human attention, if any

Before finishing, update builder/improve.md with:
- evidence reviewed
- workflow changes applied
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

End with a summary table of all steps and their status.${focusText}`)
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
      if (focus) {
        // If text before slash, treat it as a step ID
        ctx.onSubmit(`Run review_step_code(step_id="${focus}") to check if the saved main.py for step "${focus}" still matches its current description. Report any drift — missing functionality, stale behavior, hardcoded values, or output format mismatches.`)
      } else {
        ctx.onSubmit(`Run review_step_code() to compare ALL learn_code steps' saved main.py scripts against their current descriptions. For each step, check if the script still does what the description says — flag missing features, stale logic, hardcoded values, and output format drift. Report findings by severity.`)
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

End with a summary table.${focusText}`)
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
  {
    command: 'capture-context',
    description: 'Add a Type-3 business rule to context/rules.md, anchored to a metric',
    icon: <Database className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      ctx.onSubmit(`Capture a new business rule into context/rules.md and link it to the metric(s) it should move.${focus ? `\n\nUser-supplied rule text or note: ${focus}` : ''}

PRECONDITIONS (do NOT bootstrap them here — redirect instead)
1. Read workflow.json. \`workflow_type\` must be \`contextual\`. If it isn't, stop and tell the user: "Rule capture is for Type 3 (contextual) workflows. Run /improve-workflow first to set the workflow type and bootstrap metrics." Do not flip the type or set up metrics inside this command.
2. Read <workflow>/metrics.json. It must have at least one metric. If empty, stop and tell the user: "No metrics defined yet. Run /improve-eval or /improve-workflow first — those commands bootstrap metrics. Then come back and run /capture-context."

ACTION
1. Read context/rules.md if it exists, so you can pick the right section heading for the new rule (or propose a new section).
2. Confirm with the user (a) the exact rule text, (b) the section heading, (c) which existing metric(s) this rule is meant to move.
3. Call the \`capture_context\` tool with: { section, rule_text, target_metrics, example_note }. The tool atomically appends the bullet, writes a clarifications.jsonl entry (source=user, target_metrics required), and writes a builder/decisions.jsonl audit entry.

REPORT
- Confirm the rule landed: paste the new line and section.
- Cite the clarification id and decision id from the response.
- Suggest opening an experiment via propose_experiment to validate that the rule actually moves the declared metric.`)
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
      ctx.onSubmit(`Abort the active experiment and revert its intervention.${focus ? `\n\nReason: ${focus}` : ''}

DISCOVERY
1. GET /api/workflow/experiments?workspace_path=<current> and find the active experiment.
2. If multiple are active, ask the user which one.
3. Confirm the user wants to abort (this rolls back the intervention via the captured revertable_diff).

ACTION
POST /api/workflow/experiments/abort with { workspace_path, experiment_id, reason: "<why>", actor_user: "<user>" }.

REPORT
- Confirm the experiment is gone from active.json and is now in history.jsonl with status=aborted.
- List the files restored.`)
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
      ctx.onSubmit(`Extend the active experiment's measurement window.${focus ? `\n\nFocus / why: ${focus}` : ''}

DISCOVERY
1. GET /api/workflow/experiments?workspace_path=<current> to find the active experiment.
2. Ask the user how many additional runs are needed (default = workflow's default_measurement_runs).

ACTION
POST /api/workflow/experiments/extend with { workspace_path, experiment_id, additional_runs, reason }.

REPORT
- New target_runs.
- Status (back to "measuring" if it was "evaluating").`)
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
      ctx.onSubmit(`Manually conclude the active experiment.${focus ? `\n\nFocus / reason: ${focus}` : ''}

This is the OVERRIDE path. Prefer letting the evaluator agent narrate the system-computed verdict. Use this only when you genuinely believe the heuristic is wrong (large world drift, broken eval, mistaken metric).

DISCOVERY
1. GET /api/workflow/experiments?workspace_path=<current> and confirm the experiment id.
2. Decide the verdict: kept | reverted | inconclusive | extend.
3. Write the rationale (≤500 chars) and the override reason.

ACTION
POST /api/workflow/experiments/manual-conclude with { workspace_path, experiment_id, verdict, reason, rationale, actor_user }.

REPORT
- final_verdict.
- Whether it was archived to history.jsonl.
- If verdict=reverted, list the files restored.`)
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
