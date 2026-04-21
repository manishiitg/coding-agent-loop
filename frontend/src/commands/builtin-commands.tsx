import React from 'react'
import { FileText, Lightbulb, Download, Server, Cpu, Bot, Layers, Minimize2, AlertTriangle, RefreshCw, Shield, Wrench, Play, GitBranch, CheckCircle, Search, Lock, Database, Network } from 'lucide-react'
import type { CommandDefinition } from './types'

export const builtinCommands: CommandDefinition[] = [
  {
    command: 'test-step',
    description: 'Run a step once to test if it works during design',
    icon: <Play className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'builder',
    validate: (ctx) => ctx.beforeSlash.trim() ? null : 'Usage: <step-id> /test-step',
    source: 'builtin',
    execute: (ctx) => {
      const stepId = ctx.beforeSlash.trim()
      ctx.onSubmit(`Test step "${stepId}" — quick design validation, not optimization.

1. Read variables.json to pick a group. Find the latest iteration from runs/.
2. Run execute_step(step_id="${stepId}", group_name=<group>).
3. When it completes, read the output file and show me:
   - Did it succeed or fail?
   - What output did it produce? (show key fields, not the full dump)
   - If it failed: what went wrong? (read the execution log)
4. Based on the result, suggest whether the step description needs changes.

Do NOT fix anything automatically — just report what happened.`)
    }
  },
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
    command: 'check-goal',
    description: 'Check whether a real run achieves the goal, and whether eval is measuring that properly',
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
    command: 'audit-config',
    description: 'Audit per-step KB / db / lock_learnings / lock_code recommendations',
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
    command: 'kb-review',
    description: 'Review knowledgebase contents per step (read-only)',
    icon: <Network className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer', 'run'],
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Focus: ${focus}.` : ''
      ctx.onSubmit(`Review knowledgebase/graph.json and index.json. Group entities and relationships by source.step so I can see what each step has contributed. Flag duplicates, type drift (e.g. both "company" and "organization"), orphaned relationships, and missing required fields. Suggest concrete reorganize_knowledgebase("...") calls for each cleanup. Read-only — do NOT run the reorganize tool yourself.${focusText}`)
    }
  },
  {
    command: 'kb-reorganize',
    description: 'Apply a natural-language transformation to the knowledgebase graph',
    icon: <Database className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer'],
    source: 'builtin',
    execute: (ctx) => {
      const instruction = ctx.beforeSlash.trim()
      if (instruction) {
        ctx.onSubmit(`Run reorganize_knowledgebase(instruction="${instruction.replace(/"/g, '\\"')}").`)
      } else {
        ctx.setInputText(`I want to reorganize knowledgebase/graph.json. First read it and tell me what's in there (entity types, counts, any obvious cleanup candidates), then ask me what transformation to apply. Once I confirm, call reorganize_knowledgebase(instruction="...").`)
      }
    }
  },
  {
    command: 'kb-consolidate',
    description: 'Run a cross-step KB consolidation pass (holistic view, distinct from reorganize)',
    icon: <Layers className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer'],
    source: 'builtin',
    execute: (ctx) => {
      const objective = ctx.beforeSlash.trim()
      if (objective) {
        ctx.onSubmit(`Run consolidate_knowledgebase(objective="${objective.replace(/"/g, '\\"')}").`)
      } else {
        ctx.setInputText(`I want to run a cross-step knowledgebase consolidation pass — this is DIFFERENT from reorganize. Consolidation reads every step's knowledgebase_contribution plus the step output folders from the selected run, and does work only possible with the holistic view: type-name drift across steps (e.g. "company" vs "organization" for the same concept), property-name drift (e.g. "industry" vs "sector"), entity dedupe by label across step ids, cross-step pattern narratives in notes/pattern-*.md, and contested-property surfacing where two steps disagree.

First read planning/plan.json + planning/step_config.json and summarize each step's knowledgebase_contribution (group/diff them). Also jq graph.json for entity type counts and relationship type counts. Flag visible drift candidates (same concept under different type names; same property under different names; probable duplicate entities). Then propose a specific, concrete consolidation objective for me to confirm (examples: "reconcile company/organization type-name drift; canonicalize to company", "write pattern-*.md for repeating shapes across the per-account steps", "surface contested employee-count values between step-extract and step-enrich without rewriting the graph"). Once I confirm, call consolidate_knowledgebase(objective="...").

Avoid vague objectives like "clean up the KB" — the tool will reject them.`)
      }
    }
  },
  {
    command: 'learnings-organize',
    description: 'Reorganize learnings/_global/ + consolidate cross-step lessons (holistic)',
    icon: <Lightbulb className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer'],
    source: 'builtin',
    execute: (ctx) => {
      const guidance = ctx.beforeSlash.trim()
      if (guidance) {
        ctx.onSubmit(`Run organize_global_learnings(guidance="${guidance.replace(/"/g, '\\"')}").`)
      } else {
        ctx.setInputText(`I want to clean up learnings/_global/. The organize tool now sees every step's learning_objective as a cross-step view, so it can do BOTH targeted cleanup (split bloated files, merge small ones, dedupe) AND holistic consolidation (promote lessons implied by multiple steps' objectives into shared references/ sections; flag declared objectives whose scope has no matching content in SKILL.md — that usually means the per-step learning agent failed for that step).

First read learnings/_global/SKILL.md + references/*.md and summarize the current structure (files, approximate sizes, main topics). Then read planning/step_config.json for every step's learning_objective and cross-check: which objectives' scope is reflected in SKILL.md? Which aren't? Any lessons that appear under multiple step-specific headings? Propose a specific guidance string for me to confirm (examples: "promote HOW-knowledge implied by multiple learning_objectives into shared references/ sections and remove step-specific duplicates", "flag any declared learning_objective whose scope has no matching content in SKILL.md", "merge the auth files into one", "split the API section by endpoint"). Once I confirm, call organize_global_learnings(guidance="...").`)
      }
    }
  },
  {
    command: 'report-improve',
    description: 'Validate reports/report_plan.md and suggest layout/color improvements',
    icon: <CheckCircle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'builder',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusLine = focus ? `\n\nFocus on: ${focus}.` : ''
      ctx.onSubmit(`Review and improve reports/report_plan.md in two passes.${focusLine}

PASS 1 — VALIDATION
Call validate_report_plan.
- For each error: explain what's wrong in plain language, show the section + widget it refers to, and propose the exact fix (which line to change, to what).
- For warnings: group by severity. Flag ones that would visibly degrade the report (unknown chart_type, missing axis fields, invalid colors) separately from cosmetic ones (empty arrays that will fill in after a run).

PASS 2 — IMPROVEMENT SUGGESTIONS
Read reports/report_plan.md yourself and also sample the underlying db/*.json and knowledgebase/*.json sources. Then propose improvements in these categories:

1. **Layout**: are the most important widgets at the top? Are there too many widgets in a row cramming the view? Is the H2 structure grouping related content?
2. **Chart-type fit**: for each chart, is bar/line/area/pie the right choice for that data? (bar=categorical, line=time series, pie=composition ≤6 slices)
3. **Color**: does the report use semantic coloring where meaningful (status fields, pass/fail, severity)? Suggest adding color_by + color_map for any status-like fields you see in the data. Propose palettes (colors + colors_dark) for brand consistency if multiple charts share a theme.
4. **Formatting**: any number/date/currency fields that should have a format preset? Any tables with too many columns that could benefit from hide_columns?
5. **Density**: any charts with >10 points that need top_n? Any tables without default_sort that would be hard to scan?

Show ALL proposed changes as a diff (before/after snippets per widget) before editing. Ask whether to apply all, some, or none. Don't edit report_plan.md until I confirm.`)
    }
  },
  {
    command: 'harden',
    description: 'Run workflow → eval → harden (full cycle on the latest run)',
    icon: <Shield className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusArg = focus ? `focus="${focus}"` : ''
      const focusLine = focus ? `\nFocus areas for harden: **${focus}**.` : ''
      ctx.onSubmit(`Run a full execute → evaluate → harden cycle on this workflow. Do all phases autonomously without pausing for confirmation.${focusLine}

SETUP
1. Read variables.json to get the list of enabled group names.

PER-GROUP CYCLE — repeat the following for each group, sequentially. Do NOT start the next group until the previous group's full cycle (run + eval + harden) is complete.

For group {group}:
  a. **EXECUTE** — call run_full_workflow(enabled_group_names=["{group}"]) and wait for completion.
     - The first group's run triggers paired iteration rotation (iteration-0 → iteration-N for both runs/ and evaluation/runs/). Subsequent partial-group runs in this same cycle reuse iteration-0 without rotating.
     - If this group's run fails outright (no usable outputs), skip eval+harden for this group and move to the next. If ALL groups fail, stop and report.
  b. **EVALUATE** — call run_full_evaluation(target_run_folder="iteration-0/{group}") and wait for completion.
     - Confirm evaluation/runs/iteration-0/{group}/evaluation_report.json exists before continuing.
  c. **HARDEN** — call harden_workflow(iteration="iteration-0", group_name="{group}"${focusArg ? ', ' + focusArg : ''}). Wait for the background harden agent to finish.
     - Per-group harden scopes ALL analysis and fixes to this group's data only. Failure aggregation, optimized-marking, and successful_runs increments are based on this group alone (more conservative than cross-group hardening — the agent is told to prefer incrementing successful_runs over locking outright).
     - Per-group harden fixes apply BEFORE the next group runs, so subsequent groups benefit from improvements (e.g. tighter pre-validation, patched main.py).

WHY PER-GROUP (not iteration-wide):
- Each group gets dedicated attention; smaller context per harden invocation.
- Fixes from group A apply before group B runs — so B benefits without re-running A.
- Faster failure isolation: if group A's harden reveals a structural issue, you can stop the loop before wasting compute on B and C.
- Trade-off accepted: cross-group failure patterns aren't aggregated. If you want cross-group analysis, run /harden once per iteration without the loop instead.

FINAL REPORT (after all groups)
Summarize:
- Per-group: run outcome (succeeded/failed), eval score, harden changes (steps touched + what changed), steps newly marked optimized.
- Across-group: any step touched by multiple group's harden — flag if the fixes diverged or conflicted.
- What still needs human attention, if anything.`)
    }
  },
  {
    command: 'improve-workflow',
    description: 'Run structural review, replan if needed, then execute → eval → harden',
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
      ctx.onSubmit(`Your single goal: improve this workflow end-to-end with the minimum necessary changes. Start with structural diagnosis, then execute, evaluate, and harden. Do all phases autonomously without pausing for confirmation.${focusText}

SETUP
1. Read planning/plan.json to extract the objective and success_criteria. These are the north star for every decision.
2. Read evaluation/evaluation_plan.json so you understand what the eval is measuring.
3. Read variables.json to get the enabled group names.
4. ${iterationHint}

PHASE 1 — STRUCTURAL DIAGNOSIS
1. Call optimize_workflow(${focus ? `focus="${focus}"` : ''}).
2. Read the optimize_workflow result carefully and classify the findings:
   - **Structural**: missing steps, redundant steps, wrong ordering, wrong step type, broken context flow.
   - **Non-structural**: weak prompts, weak validation, step reliability issues, tool/config issues.
3. If the findings show a MATERIAL structural problem and you have real run evidence, call replan_workflow_from_results(iteration="{starting_iter}"${focusArg}) ONCE before doing the hardening loop.
4. If the findings are minor or mostly non-structural, skip replanning and keep the current structure.
5. Do not thrash the plan. At most one structural replan in this command run.

PHASE 2 — PER-GROUP EXECUTE → EVAL → HARDEN
Repeat the following for each enabled group, sequentially. Do NOT start the next group until the previous group's full cycle is complete.

For group {group}:
  a. **EXECUTE** — call run_full_workflow(enabled_group_names=["{group}"]) and wait for completion.
     - The first group's run triggers paired iteration rotation (iteration-0 → iteration-N for both runs/ and evaluation/runs/). Subsequent partial-group runs in this same cycle reuse iteration-0 without rotating.
     - If this group's run fails outright (no usable outputs), inspect the logs before deciding next action.
  b. **EVALUATE** — call run_full_evaluation(target_run_folder="iteration-0/{group}") and wait for completion.
     - Confirm evaluation/runs/iteration-0/{group}/evaluation_report.json exists before continuing.
  c. **DECIDE** — based on the eval report and logs:
     - If issues are structural and cannot be fixed by hardening a step, and you have not already replanned in this command run, call replan_workflow_from_results(iteration="iteration-0", group_name="{group}"${focusArg}), then continue with the remaining groups using the updated plan.
     - Otherwise call harden_workflow(iteration="iteration-0", group_name="{group}"${focusArg}) and wait for it to finish.
  d. **BE CONSERVATIVE** — prefer harden_workflow for reliability fixes; use replanning only when the workflow shape is actually wrong.

PHASE 3 — VERIFY THE IMPROVEMENT
1. After all groups have completed, compare:
   - structural issues found in Phase 1
   - per-group run outcomes
   - per-group eval results
   - harden/replan changes applied
2. If the workflow still misses key success criteria and the cause is clearly fixable within one more pass, do ONE targeted verification pass on the highest-value group only:
   - run_full_workflow(enabled_group_names=["{group}"])
   - run_full_evaluation(target_run_folder="iteration-0/{group}")
3. Do not loop indefinitely. Maximum one extra verification pass.

FINAL REPORT
Summarize:
- Structural diagnosis from optimize_workflow
- Whether replan_workflow_from_results was used, and why
- Per-group: run outcome, eval score, harden changes, steps newly marked optimized
- Which success criteria are now better satisfied than before
- Remaining gaps that still need human attention, if any`)
    }
  },
  {
    command: 'audit-descriptions',
    description: 'Check all steps for description vs skill/learning confusion',
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
    command: 'audit-orchestrators',
    description: 'Audit todo_task orchestrator descriptions for quality',
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
