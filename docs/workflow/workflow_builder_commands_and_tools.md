# Workflow Builder Commands And Tools

This doc is a reference for the workflow-builder surface.

It separates two different concepts that are easy to confuse:

- **Slash commands**: frontend shortcuts defined in [`frontend/src/commands/builtin-commands.tsx`](../../frontend/src/commands/builtin-commands.tsx). These are prompt macros. They do not perform work themselves.
- **Tools**: backend workflow-builder tools registered in [`interactive_workshop_manager.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/interactive_workshop_manager.go). These are the actual callable operations the builder agent can execute.

Short version:

- `/improve-workflow` is a slash command
- `harden_workflow` is a tool
- `/review-goal` is a slash command
- `review_workflow_results` is a tool

## Mental Model

Use this rule:

- Add a **slash command** when you want a guided workflow or a common prompt pattern
- Add a **tool** when you need a new primitive capability

A slash command can call multiple tools.
A tool can exist without any dedicated slash command.

## Workshop Modes

Current workflow-builder workshop modes used by the UI:

- `builder`
- `optimizer`
- `run`

Availability in this doc is expressed in terms of those three modes.

## Slash Commands

These are the current workflow slash commands.

| Slash Command | Purpose | Available In | Typical Tools It Drives |
|---|---|---|---|
| `/design-flow` | Inspect context dependencies and handoff design | `builder` | read-only shell/file tools |
| `/ready-to-optimize` | Run a readiness checklist before switching into optimizer work | `builder` | read-only shell/file tools |
| `/review-plan` | Critically review workflow design decisions | `builder`, `optimizer`, `run` | `review_plan` |
| `/review-goal` | Check whether a real run is achieving the goal and whether eval measures it properly | `optimizer` | `review_workflow_results` |
| `/review-speed` | Analyze workflow latency and recommend how to make it faster safely | `optimizer` | `review_workflow_timing` |
| `/review-cost` | Analyze workflow cost and recommend how to reduce it safely | `optimizer` | `review_workflow_costs` |
| `/review-config` | Review KB/db/lock recommendations | `builder`, `optimizer`, `run` | read-only shell/file tools, step-config inspection |
| `/improve-kb` | Review and improve the knowledgebase using the current graph, plan, and run evidence | `optimizer` | `reorganize_knowledgebase`, `consolidate_knowledgebase`, read-only inspection tools |
| `/improve-learnings` | Review and improve shared learnings using current learnings, plan, and run evidence | `optimizer` | `organize_global_learnings`, read-only inspection tools |
| `/improve-eval` | Validate and improve `evaluation/evaluation_plan.json` against the objective, success criteria, and real run evidence | `optimizer` | `validate_evaluation_plan`, `review_workflow_results`, sometimes `run_full_evaluation` |
| `/improve-report` | Validate and improve `reports/report_plan.md` using a rendered preview plus underlying data | `optimizer` | `validate_report_plan`, `preview_report_render`, read-only shell/file tools |
| `/improve-continuously` | Set up recurring run + slower recurring optimizer improvement, with `builder/improve.md` as durable memory | `optimizer` | `get_workflow_config`, `get_schedule_runs`, `create_schedule`, `update_schedule`, file-edit tools |
| `/improve-workflow` | Hybrid optimization flow: structural review, optional replan, evidence-based harden, optional verification run | `optimizer` | `optimize_workflow`, `replan_workflow_from_results`, `harden_workflow`, sometimes `run_full_evaluation`, optionally `run_full_workflow` |
| `/review-descriptions` | Review plan descriptions for confusion, hardcoding, browser anti-patterns, and missing validation | `builder`, `optimizer`, `run` | read-only shell/file tools |
| `/review-code` | Review saved `main.py` scripts against step descriptions | `optimizer` | `review_step_code` |
| `/review-orchestrators` | Review `todo_task` orchestrator descriptions and routes | `builder`, `optimizer`, `run` | read-only shell/file tools |
| `/capture-context` | Add a Type-3 business rule to `context/rules.md`, anchored to a metric — auto-improvement framework | `optimizer` (Type 3 only) | `POST /api/workflow/capture-context` |
| `/exp-abort` | Revert and abort the active experiment via the captured revertable diff | `optimizer` | `POST /api/workflow/experiments/abort` |
| `/exp-extend` | Add more measurement runs to the active experiment | `optimizer` | `POST /api/workflow/experiments/extend` |
| `/exp-conclude` | Manually render a verdict (overrides the evaluator) — use only when the heuristic verdict is clearly wrong | `optimizer` | `POST /api/workflow/experiments/manual-conclude` |

The `/capture-context` and `/exp-*` commands belong to the auto-improvement framework. See `auto_improvement_framework.md` for the design and the experiment lifecycle these commands operate on.

## Backend Tools

These are the actual workflow-builder tools. The builder agent can call them directly when the current workshop mode allows it.

### Always Available Base Tools

These are available regardless of workshop mode.
This section lists tools actually exposed to the workflow-builder LLM. Internal workspace executors such as `list_workspace_files`, `read_workspace_file`, `update_workspace_file`, `delete_workspace_file`, and `move_workspace_file` are intentionally excluded here because they are backend APIs/executors, not workflow-mode LLM tools.

| Tool | Purpose |
|---|---|
| `execute_shell_command` | Run shell commands in the workflow workspace |
| `diff_patch_workspace_file` | Patch a workspace file with a diff |
| `read_image` | Read an image artifact |
| `read_pdf` | Read a PDF artifact |
| `generate_text_llm` | One-off text generation helper |
| `search_web_llm` | Web-search helper |
| `image_gen` | Generate images |
| `image_edit` | Edit images |
| `list_secrets` / `set_user_secret` / `delete_user_secret` | Inspect or manage user secrets |
| `human_feedback` | Ask the human for a response |
| `agent_browser` | Browser automation entry point |
| `get_api_spec` / `get_prompt` / `get_resource` | MCP virtual helper tools |
| `call_sub_agent` / `call_generic_agent` / `get_sub_agent_conversation` / `get_route_description` | Sub-agent helper tools |
| `get_step_prompts` | Show prompts for a workflow step |
| `get_workflow_config` | Inspect workflow-level config |
| `get_llm_config` | Inspect active LLM config |
| `get_cost_summary` | Show token/cost breakdown |

### Execution Tools

These are the run/test/inspection primitives.

| Tool | Purpose | Available In |
|---|---|---|
| `execute_step` | Run one step in the background | `builder`, `optimizer`, `run` |
| `query_step` | Inspect execution progress and tool calls | `builder`, `optimizer`, `run` |
| `stop_step` | Stop one running execution | `builder`, `optimizer`, `run` |
| `stop_all_executions` | Stop all running executions in the session | `builder`, `optimizer`, `run` |
| `list_executions` | List known background executions | `builder`, `optimizer`, `run` |
| `run_in_background` | Spawn a background agent with the same workflow context | `builder`, `optimizer`, `run` |
| `run_full_workflow` | Run the full workflow | `builder`, `optimizer`, `run` |
| `debug_step` | Inspect a step’s execution state, logs, and learning status | `builder`, `optimizer`, `run` |

### Review And Optimization Tools

These are the most important tools when people say “review”, “optimize”, “replan”, or “harden”.

| Tool | Purpose | Mutates? | Available In |
|---|---|---|---|
| `review_plan` | Critically review workflow design decisions, optionally using run evidence | No | `builder`, `optimizer`, `run` |
| `review_workflow_results` | Review actual outcomes against objective/success criteria and audit eval quality | No | `builder`, `optimizer`, `run` |
| `review_workflow_timing` | Review actual run latency and recommend safe speedups | No | `builder`, `optimizer`, `run` |
| `review_workflow_costs` | Review actual run cost and recommend safe cost reductions | No | `builder`, `optimizer`, `run` |
| `review_step_code` | Review saved `main.py` against current step descriptions | No | `builder`, `optimizer` |
| `optimize_step` | Analyze one step for optimization opportunities | No | `builder`, `optimizer` |
| `optimize_workflow` | Analyze the overall workflow structure against the objective | No | `builder`, `optimizer` |
| `replan_workflow_from_results` | Rewrite the workflow structure from actual run evidence | Yes | `builder`, `optimizer` |
| `harden_workflow` | Apply non-structural reliability fixes from eval failures | Yes | `builder`, `optimizer` |
| `analyze_step` | Step-focused inspection helper | Yes/No depends on usage context, but used as analysis tooling | `builder`, `optimizer` |

Important distinction:

- Use `review_workflow_results` when the question is: “Did the run actually achieve the goal, and is eval measuring that correctly?”
- Use `replan_workflow_from_results` when the question is: “Is the workflow structure wrong based on what happened in a real run?”
- Use `harden_workflow` when the structure is right but the steps need to become more reliable.

### Plan And Step-Editing Tools

These mutate the workflow plan/config directly.

| Tool | Purpose | Available In |
|---|---|---|
| `add_regular_step` | Add a regular step | `builder`, `optimizer` |
| `add_routing_step` | Add a routing step | `builder`, `optimizer` |
| `add_human_input_step` | Add a human-input step | `builder`, `optimizer` |
| `add_todo_task_step` | Add a `todo_task` step | `builder`, `optimizer` |
| `add_todo_task_route` | Add a route inside a `todo_task` | `builder`, `optimizer` |
| `update_regular_step` | Update a regular step | `builder`, `optimizer` |
| `update_routing_step` | Update a routing step | `builder`, `optimizer` |
| `update_human_input_step` | Update a human-input step | `builder`, `optimizer` |
| `update_todo_task_step` | Update a `todo_task` step | `builder`, `optimizer` |
| `update_todo_task_route` | Update a `todo_task` route | `builder`, `optimizer` |
| `delete_todo_task_route` | Delete a `todo_task` route | `builder`, `optimizer` |
| `delete_plan_steps` | Delete workflow steps | `builder`, `optimizer` |
| `update_validation_schema` | Update a step’s validation schema | `builder`, `optimizer` |
| `update_step_config` | Update per-step execution/configuration settings | `builder`, `optimizer` |
| `publish_workflow_version` | Publish a workflow version | `builder`, `optimizer` |
| `restore_workflow_version` | Restore a prior workflow version | `builder`, `optimizer` |

### Variables, Workflow Config, Scheduling, Skills

| Tool | Purpose | Available In |
|---|---|---|
| `update_variable` | Update a variable definition | `builder`, `optimizer` |
| `add_group` / `update_group` / `delete_group` | Manage variable groups | `builder`, `optimizer` |
| `update_workflow_config` | Update workflow-level config | `builder`, `optimizer` |
| `create_schedule` / `update_schedule` / `delete_schedule` / `trigger_schedule` / `get_schedule_runs` | Manage workflow schedules | `builder`, `optimizer` |
| `list_skills` / `search_skills` / `install_skill` / `uninstall_skill` / `import_skill` | Manage skills | `builder`, `optimizer` |
| `list_published_llms` / `list_provider_models` / `test_llm` / `set_workflow_llm_config` | Inspect or configure LLM settings | `builder`, `optimizer` |

### Evaluation And Reporting Tools

| Tool | Purpose | Available In |
|---|---|---|
| `validate_evaluation_plan` | Validate the evaluation plan | `builder`, `optimizer` |
| `run_full_evaluation` | Execute the evaluation plan | `builder`, `optimizer` |
| `get_reporting_capabilities` | Show report grammar/capabilities | `builder`, `optimizer` |
| `validate_report_plan` | Validate `reports/report_plan.md` | `builder`, `optimizer` |
| `preview_report_render` | Preview report rendering | `builder`, `optimizer` |

### Knowledgebase Tools

| Tool | Purpose | Available In |
|---|---|---|
| `reorganize_knowledgebase` | Apply a natural-language KB reorganization | `builder`, `optimizer` |
| `consolidate_knowledgebase` | Consolidate KB content across steps/runs | `builder`, `optimizer` |

## Common Crosswalks

These are the most common “what slash command maps to what tool?” relationships.

| Slash Command | Usually Calls |
|---|---|
| `/review-plan` | `review_plan` |
| `/review-goal` | `review_workflow_results` |
| `/review-speed` | `review_workflow_timing` |
| `/review-cost` | `review_workflow_costs` |
| `/review-code` | `review_step_code` |
| `/improve-eval` | `validate_evaluation_plan`, `review_workflow_results`, and sometimes `run_full_evaluation` |
| `/improve-continuously` | `get_workflow_config`, `get_schedule_runs`, `create_schedule`, `update_schedule`, and file-edit tools for `builder/improve.md` |
| `/improve-workflow` | `optimize_workflow`, sometimes `replan_workflow_from_results`, then `harden_workflow`; only uses `run_full_evaluation` or `run_full_workflow` when existing evidence is missing or verification is needed |
| `/improve-kb` | `reorganize_knowledgebase`, `consolidate_knowledgebase`, and read-only KB inspection |
| `/improve-learnings` | `organize_global_learnings` and read-only learnings inspection |
| `/improve-report` | `validate_report_plan`, `preview_report_render`, plus read-only data inspection |

## Which One Should I Use?

If you are trying to:

- **Review the design**: use `/review-plan` or `review_plan`
- **Review whether a real run actually achieved the goal**: use `/review-goal` or `review_workflow_results`
- **Review where runtime is being spent**: use `/review-speed` or `review_workflow_timing`
- **Review where cost is being spent**: use `/review-cost` or `review_workflow_costs`
- **Improve the knowledgebase**: use `/improve-kb`
- **Improve learnings**: use `/improve-learnings`
- **Improve the evaluation plan**: use `/improve-eval`
- **Set up recurring execution + continuous optimization**: use `/improve-continuously`
- **Rewrite the workflow structure from evidence**: use `replan_workflow_from_results` or let `/improve-workflow` decide
- **Improve reliability without redesigning the workflow**: use `harden_workflow` or let `/improve-workflow` decide
- **Do an end-to-end optimization pass**: use `/improve-workflow`

## Exact Slash Command Messages

This appendix captures the exact normalized prompt templates used by the workflow slash commands in [`frontend/src/commands/builtin-commands.tsx`](../../frontend/src/commands/builtin-commands.tsx).

Normalization rules:

- Runtime values are shown as placeholders such as `<focus>`, `<selected-run-folder>`, `<step-id>`, and `<instruction>`.
- If a command has multiple behaviors depending on whether text appears before the slash, both variants are shown.
- If a command does not immediately submit a message and instead pre-fills the chat input, that draft text is shown explicitly.
- Common placeholders:
  - `<focus-text>` means either empty or ` Focus especially on: <focus>.`
  - `<focus-line>` means either empty or a new line carrying the focus text, exactly as shown in the relevant command section
  - `<run-text>` means the selected-run-folder sentence or the fallback “find the latest meaningful evidence first” sentence

### `/design-flow`

If text exists before the slash, append:

```text
Pay special attention to: <focus>
```

Submitted message:

```text
Read planning/plan.json and analyze the context flow between steps.<focus-text>

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
```

### `/ready-to-optimize`

Submitted message:

```text
Run an optimization-readiness checklist. Check each item and report PASS or FAIL:

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
```

### `/review-plan`

Submitted message:

```text
Run review_plan() to critically analyze the current workflow plan.<focus-text>

Challenge every decision: step boundaries, step types, execution modes, context flow, validation coverage, portability, and whether choices are justified by the objective and success criteria. Report findings by severity — don't just summarize, identify what's weak, risky, or unjustified.

This is a plan/design review. Use review_workflow_results() when the question is whether a real run is actually achieving the goal and whether eval measures that properly.
```

### `/review-goal`

Submitted message:

```text
Run review_workflow_results() to judge actual workflow outcomes, not just the plan.<focus-text>

<run-text>

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
```

### `/review-speed`

Submitted message:

```text
Run review_workflow_timing() to analyze where workflow time is actually going and how to make it faster.<focus-text>

<run-text>

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
```

Where `<run-text>` is one of:

```text
Use the selected run folder "<selected-run-folder>" unless you find stronger newer evidence is needed.
```

or

```text
If no run folder is selected, find the latest meaningful run evidence first.
```

### `/review-cost`

Submitted message:

```text
Run review_workflow_costs() to analyze where workflow cost is going and how to reduce it without hurting results.<focus-text>

<run-text>

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
```

Where `<run-text>` is one of:

```text
Use the selected run folder "<selected-run-folder>" unless you find stronger newer evidence is needed.
```

or

```text
If no run folder is selected, find the latest meaningful run and cost evidence first.
```

### `/review-config`

Submitted message:

```text
Audit every step in this workflow's plan and recommend the right values for: knowledgebase_access, knowledgebase_contribution, lock_learnings, and lock_code (learn_code steps only). This is a READ-ONLY analysis pass — do NOT apply any changes via update_step_config. Produce a recommendation table the user will review and apply selectively.<focus-text>

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
```

### `/improve-kb`

Submitted message:

```text
Review and improve the workflow knowledgebase. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your KB findings and applied decisions when you finish.<focus-text>

DISCOVERY
1. Read soul/soul.md to understand the objective and success criteria.
2. Read planning/plan.json and planning/step_config.json so you understand which steps should read from or contribute to the KB.
3. Read knowledgebase/notes/_index.json and any topic files the task references.
4. <run-text>

DECISION
Assess whether the KB needs:
- no change
- targeted graph reorganization
- cross-step consolidation
- both

Look for:
- duplicate entities
- type-name drift
- property-name drift
- orphaned relationships
- contested facts
- missing step contributions that should have been persisted
- KB structure that no longer matches the workflow objective or current step outputs

ACTION
- Use reorganize_knowledgebase when the graph structure itself needs cleanup or normalization.
- Use consolidate_knowledgebase when cross-step consolidation or canonicalization is needed.
- Automatically APPLY high-confidence, evidence-backed KB improvements instead of only listing recommendations.
- Be conservative and bounded; do not ask for human confirmation during this improve run.

When you finish, update builder/improve.md with:
- what KB evidence you reviewed
- the main KB weaknesses you found
- what was reorganized or consolidated
- what was applied vs deferred
- remaining KB gaps
```

Where `<run-text>` is one of:

```text
Use the selected run folder "<selected-run-folder>" when recent run evidence helps explain KB drift or missing contributions.
```

or

```text
If recent run evidence is needed to explain KB drift or missing contributions, find the latest meaningful run first.
```

### `/improve-learnings`

Submitted message:

```text
Review and improve workflow learnings. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your learnings findings and applied decisions when you finish.<focus-text>

DISCOVERY
1. Read soul/soul.md to understand the objective and success criteria.
2. Read planning/step_config.json to inspect learning_objective coverage across steps.
3. Read learnings/_global/SKILL.md and references/*.md when present.
4. Inspect relevant step learnings when current failures, duplication, or missing knowledge appear to be step-specific.
5. <run-text>

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
- remaining learnings gaps
```

Where `<run-text>` is one of:

```text
Use the selected run folder "<selected-run-folder>" when recent run evidence helps explain stale, missing, or duplicated learnings.
```

or

```text
If recent run evidence is needed to explain stale, missing, or duplicated learnings, find the latest meaningful run first.
```

### `/improve-report`

Submitted message:

```text
Review and improve reports/report_plan.md in two passes. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your report-plan findings and applied decisions when you finish.<focus-line>

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

Show ALL proposed changes as a diff (before/after snippets per widget) before editing. Ask whether to apply all, some, or none. Don't edit report_plan.md until I confirm.

When you finish, update builder/improve.md with:
- what report evidence you reviewed
- the main report weaknesses you found
- what you recommended
- what was applied vs deferred
```

Where `<focus-line>` is either empty or:

```text

Focus on: <focus>.
```

### `/improve-eval`

Submitted message:

```text
Review and improve evaluation/evaluation_plan.json in three passes. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and append your eval findings and applied decisions when you finish.<focus-line>

PASS 1 — VALIDATION
1. Call validate_evaluation_plan.
2. For each error: explain what's wrong in plain language, show the eval step/widget/field it refers to, and propose the exact fix.
3. For warnings: separate correctness-risk warnings from lower-priority quality issues.

PASS 2 — GOAL / SUCCESS-CRITERIA ALIGNMENT
1. Read soul/soul.md and extract the objective and success criteria.
2. Read planning/plan.json so you understand what the workflow is actually producing.
3. <run-text>
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
- what was applied vs deferred
```

Where `<focus-line>` is either empty or:

```text

Focus on: <focus>.
```

Where `<run-text>` is one of:

```text
Use the selected run folder "<selected-run-folder>" as the primary evidence set when judging whether eval measures the real objective and success criteria well.
```

or

```text
If a meaningful prior run exists, use it as evidence when judging whether eval measures the real objective and success criteria well. You may inspect existing evaluation results and only run evaluation if needed to test the current eval plan.
```

### `/improve-continuously`

Submitted message:

```text
Set up automatic run + improve scheduling for this workflow. Do this autonomously and avoid creating duplicate schedules.<focus-text>

GOAL
Create or update TWO complementary schedules:
1. a normal workflow run schedule for recurring execution
2. a slower optimizer/workshop schedule that continuously improves the workflow and evaluation over time

DISCOVERY
1. Read soul/soul.md to understand the objective and success criteria.
2. Read variables/variables.json to identify valid group names and enabled groups.
3. Call get_workflow_config and inspect the current schedule list carefully.
4. If there are existing candidate schedules, use get_schedule_runs on the most relevant ones to understand whether they are active, useful, stale, too frequent, or missing coverage.

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
- update builder/improve.md with timestamp, evidence reviewed, schedule context, workflow changes, eval changes, report changes, what was applied automatically, remaining gaps, and next hypotheses<improve-continuously-focus>

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
```

Where `<improve-continuously-focus>` is either empty or:

```text
 Focus especially on: <focus>.
```

### `/improve-workflow`

Submitted message:

```text
Your single goal: improve this workflow end-to-end with the minimum necessary changes, starting from existing run evidence. Use builder/improve.md as the shared improvement log: read it first if it exists, create it if it does not, and update it with your decisions at the end. Review the current iteration first, then replan or harden from that evidence. Only run a fresh verification pass if it is actually needed.<focus-text>

SETUP
1. Read planning/plan.json to extract the objective and success_criteria. These are the north star for every decision.
2. Read evaluation/evaluation_plan.json so you understand what the eval is measuring.
3. Read variables.json to get the enabled group names.
4. <iteration-hint> Treat that iteration as the default evidence set for this command run.

PHASE 1 — STRUCTURAL DIAGNOSIS
1. Call optimize_workflow(<focus-call>).
2. Read the optimize_workflow result carefully and classify the findings:
   - **Structural**: missing steps, redundant steps, wrong ordering, wrong step type, broken context flow.
   - **Non-structural**: weak prompts, weak validation, step reliability issues, tool/config issues.
3. If the findings show a MATERIAL structural problem and you have real run evidence, call replan_workflow_from_results(iteration="{starting_iter}"<focus-arg>) ONCE before doing the hardening review.
4. If the findings are minor or mostly non-structural, skip replanning and keep the current structure.
5. Do not thrash the plan. At most one structural replan in this command run.

PHASE 2 — PER-GROUP EVIDENCE REVIEW → HARDEN
Repeat the following for each enabled group, sequentially, using the selected/latest iteration as the default evidence set.

For group {group}:
  a. **REVIEW EXISTING EVIDENCE** — inspect this group's existing outputs, logs, validation failures, and evaluation report for "{starting_iter}/{group}".
     - If the workflow run exists but the evaluation report is missing, you MAY call run_full_evaluation(target_run_folder="{starting_iter}/{group}") to score the existing outputs. Do NOT execute a fresh workflow run in this phase.
     - If there is no meaningful run evidence for this group in the chosen iteration, report that gap clearly and continue with the groups that do have evidence.
  b. **DECIDE** — based on the existing evidence:
     - If issues are structural and cannot be fixed by hardening a step, and you have not already replanned in this command run, call replan_workflow_from_results(iteration="{starting_iter}", group_name="{group}"<focus-arg>), then continue reviewing the remaining groups against the updated plan.
     - Otherwise call harden_workflow(iteration="{starting_iter}", group_name="{group}"<focus-arg>) and wait for it to finish.
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
- next hypotheses
```

### `/review-descriptions`

Submitted message:

```text
Audit every step's description in plan.json. For each step, do the following:

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

End with a summary table of all steps and their status.<focus-text>
```

### `/review-code`

If text exists before the slash, the command immediately submits:

```text
Run review_step_code(step_id="<step-id>") to check if the saved main.py for step "<step-id>" still matches its current description. Report any drift — missing functionality, stale behavior, hardcoded values, or output format mismatches.
```

If no text exists before the slash, the command immediately submits:

```text
Run review_step_code() to compare ALL learn_code steps' saved main.py scripts against their current descriptions. For each step, check if the script still does what the description says — flag missing features, stale logic, hardcoded values, and output format drift. Report findings by severity.
```

### `/review-orchestrators`

Submitted message:

```text
Audit all todo_task steps in plan.json. For each todo_task step, read its todo_task_step description and all its predefined_routes sub-agent descriptions. Check for these problems:

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

End with a summary table.<focus-text>
```

## Source Of Truth

This doc is only a guide.

The current source of truth is the code:

- Slash commands: [`frontend/src/commands/builtin-commands.tsx`](../../frontend/src/commands/builtin-commands.tsx)
- Tool gating and registration: [`interactive_workshop_manager.go`](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/interactive_workshop_manager.go)
