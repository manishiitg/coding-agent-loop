import React from 'react'
import { FileText, Lightbulb, Download, Server, Cpu, Bot, Layers, Minimize2, AlertTriangle, RefreshCw, Shield, Wrench, Play, GitBranch, CheckCircle, Search } from 'lucide-react'
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

1. **Objective set?** — Read planning/plan.json root "objective" field. FAIL if empty/missing.
2. **Success criteria set?** — Read planning/plan.json root "success_criteria" field. FAIL if empty/missing.
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
    description: 'Critically analyze the plan for weaknesses and improvements',
    icon: <Search className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer'],
    source: 'builtin',
    execute: (ctx) => {
      const focus = ctx.beforeSlash.trim()
      const focusText = focus ? ` Focus especially on: ${focus}.` : ''
      ctx.onSubmit(`Run review_plan() to critically analyze the current workflow plan.${focusText}

Challenge every decision: step boundaries, step types, execution modes, context flow, validation coverage, portability, and whether choices are justified by the objective and success criteria. Report findings by severity — don't just summarize, identify what's weak, risky, or unjustified.`)
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

PHASE 1 — EXECUTE (one group at a time)
1. Read variables.json to get the list of enabled group names.
2. For each group, sequentially: call run_full_workflow(enabled_group_names=["{group}"]) and wait for that group to complete before starting the next. Running one group at a time keeps signal clean (per-group failures are isolated), avoids resource contention (browsers, API rate limits), and lets you abort early if a group is failing badly.
   - The first group's run triggers paired iteration rotation (iteration-0 → iteration-N for both runs/ and evaluation/runs/). Subsequent partial-group runs in this same cycle reuse iteration-0 without rotating, so all groups end up sharing the same iteration-0 by the time PHASE 2 starts.
3. If a group fails outright (no usable outputs), note it but continue with remaining groups — partial coverage is still useful for harden. If ALL groups fail, stop and report what went wrong.

PHASE 2 — EVALUATE (one group at a time)
1. For each group that produced outputs in PHASE 1, sequentially: call run_full_evaluation(target_run_folder="iteration-0/{group}") and wait for it to complete before starting the next.
2. Eval always targets the current iteration-0; the per-group suffix narrows scoring to that group's artifacts.
3. Confirm evaluation/runs/iteration-0/{group}/evaluation_report.json exists for each evaluated group before continuing.

PHASE 3 — HARDEN
1. Call harden_workflow(target_run_folder="iteration-0"${focusArg ? ', ' + focusArg : ''}). Reads every group's eval report, identifies failing steps, applies targeted fixes (pre-validation rules, description tightening, main.py patches, KB config, optimized_reason), and marks passing steps as optimized where appropriate.
2. Wait for the harden agent to finish — it runs in the background and notifies on completion.

PHASE 4 — REPORT
Summarize the cycle:
- Workflow run outcome (groups that succeeded/failed)
- Evaluation scores per group (highlight any < 8)
- Harden agent's changes (list each step touched and what was changed)
- Steps newly marked optimized vs steps still failing
- What still needs human attention, if anything`)
    }
  },
  {
    command: 'tune-step',
    description: 'Run a step, evaluate it, and fix any issues',
    icon: <Wrench className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: 'optimizer',
    validate: (ctx) => ctx.beforeSlash.trim() ? null : 'Usage: /tune-step <step-id>',
    source: 'builtin',
    execute: (ctx) => {
      const stepId = ctx.beforeSlash.trim()
      ctx.onSubmit(`Tune step "${stepId}". Do all of this autonomously without pausing for confirmation:

1. Read variables.json to get a group ID. Find the latest iteration from runs/.
2. Run execute_step(step_id="${stepId}", group_name=<group>, iteration=<iter>). Wait for completion.
3. Check the result. If it failed, read the execution logs (learn_code_fast_path.json or conversation log) to understand why.
4. Read the step's current description, validation_schema, and learnings (main.py or SKILL.md).
5. Fix any issues found:
   - If main.py has a bug → patch it with diff_patch_workspace_file
   - If description is vague → tighten it with update_regular_step or update_todo_task_route
   - If validation_schema is missing checks → add them with update_validation_schema
   - If step config is wrong (wrong mode, missing servers) → fix with update_step_config
6. Re-run the step to verify the fix works.
7. Give a final summary of what was wrong and what changed.`)
    }
  },
  {
    command: 'auto-research-improve',
    description: 'Analyze outputs, evals & success criteria to auto-improve the workflow',
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
        ? `Use iteration "${runFolder.split('/')[0]}".`
        : 'Read runs/ to find the latest iteration.'
      ctx.onSubmit(`Your single goal: make this workflow achieve its success criteria as reliably as possible. Research what's going wrong, fix it, and verify the fix. Do all of this autonomously without pausing for confirmation. ${iterationHint}${focusText}

PHASE 1 — UNDERSTAND THE GOAL
1. Read planning/plan.json — extract the objective and success_criteria. These are your north star. Every change you make must move the workflow closer to meeting ALL success criteria.
2. Read evaluation/evaluation_plan.json — understand what's being measured and how.
3. Read variables.json to get all group IDs.

PHASE 2 — RESEARCH WHAT'S FAILING AND WHY
For each group in the latest iteration:
- Read every step's execution output (runs/{iter}/{group}/step-*/output.json or context output files).
- Read evaluation/runs/{iter}/{group}/evaluation_report.json for scores and failure reasons.
- For failed/low-scoring steps, read the full execution logs to understand the root cause.

Map every failure back to a specific success criterion that's not being met. Build a gap analysis:
| Success Criterion | Met? | Blocking Step(s) | Root Cause |

Prioritize by impact: fix the gaps that block the most success criteria first.

PHASE 3 — FIX EVERYTHING (apply all fixes automatically)
Work through the gap analysis top-down. For each issue, pick the right tool:

- **Wrong plan structure** (missing steps, wrong order, steps that should be split/merged) → replan_workflow_from_results(iteration="{iter}")
- **Bad step instructions** (step misunderstands the task, produces wrong output) → update_regular_step or update_todo_task_route — rewrite the description based on what the output SHOULD have been to satisfy the success criteria
- **Weak validation** (bad output slips through uncaught) → update_validation_schema — add checks that would have caught the failure
- **Repeated mistakes** (step keeps hitting the same issue) → harden_workflow(iteration="{iter}") — capture the fix as a durable learning
- **Poor orchestration** (todo_task steps with bad delegation/routing) → rewrite orchestrator descriptions with clear objectives, routing criteria, and failure handling
- **Missing or wrong tools/MCP servers on a step** → update_step_config to give the step what it needs

After applying fixes, do a second pass: re-read the success criteria and ask "is there anything in the plan that still can't produce what the criteria require?" Fix any remaining gaps.

PHASE 4 — VERIFY
1. Pick one group and re-run: run_full_workflow(iteration=next_iter, group_name="{group}").
2. Wait for completion, then evaluate: run_full_evaluation(iteration=next_iter, group_name="{group}").
3. Compare the gap analysis from Phase 2 against the new results. For each success criterion, report: was it met before? Is it met now?
4. If new failures appeared, fix them and re-verify (max 2 retry cycles).

PHASE 5 — REPORT & SCHEDULE
Produce a clear summary:
- **Goal**: The success criteria from plan.json
- **Before**: Which criteria were met/unmet, scores per group
- **Changes made**: What you fixed and why
- **After**: Which criteria are now met, new scores
- **Remaining gaps**: What still needs work (if any)

Then ask the user: "Would you like me to set up a recurring schedule to keep improving this workflow automatically? (e.g., daily at 2 AM). Each run will research the latest results, fix issues, and verify — continuously pushing toward 100% success criteria achievement." If yes, use create_schedule to set it up.`)
    }
  },
  {
    command: 'audit-descriptions',
    description: 'Check all steps for description vs skill/learning confusion',
    icon: <AlertTriangle className="w-4 h-4" />,
    modes: ['workflow'],
    requiredWorkflowMode: 'plan',
    requiredWorkshopMode: ['builder', 'optimizer'],
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
    requiredWorkshopMode: ['builder', 'optimizer'],
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
    requiredWorkshopMode: ['builder', 'optimizer'],
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
- Most steps should use \`"type": "regular"\`. Use \`"decision"\` / \`"conditional"\` / \`"routing"\` / \`"human_input"\` / \`"todo_task"\` only when the conversation genuinely called for branching or human-in-the-loop.

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
    command: 'compress-memory',
    description: 'Compress and clean up agent memories',
    icon: <Minimize2 className="w-4 h-4" />,
    modes: ['multi-agent'],
    source: 'builtin',
    execute: (ctx) => {
      const msg = ctx.beforeSlash
        ? `Compress and consolidate my memories, focusing on: ${ctx.beforeSlash}. Use compress_memory.`
        : 'Compress and consolidate all my memories. Use compress_memory to read all files, merge related entries, remove superseded info, and reduce verbosity.'
      ctx.onSubmit(msg)
    }
  }
]
