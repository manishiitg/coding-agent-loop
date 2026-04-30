# Evaluation System

This doc describes how workflow evaluation works now.

The current system is file-backed and execution-oriented:
- the evaluation plan lives in `evaluation/evaluation_plan.json`
- evaluation step config lives in `evaluation/step_config.json`
- evaluation execution runs in an internal sandbox under `evaluation/runs/iteration-0[/group]`
- the final report is published to `evaluation/runs/{targetRunFolder}/evaluation_report.json`

## What Evaluation Is For

Evaluation is the workflow's scoring layer for completed execution runs.

It answers:
- did a target run actually satisfy the intended outcomes?
- what did each evaluation step conclude?
- what overall score should this run receive?

It is separate from:
- execution-time pre-validation
- step learning
- workflow execution itself

## Current Mental Model

Use this mental model:

1. Build or edit the eval plan in `evaluation/evaluation_plan.json`.
2. Point evaluation at a completed execution run folder such as `iteration-8/production`.
3. Evaluation steps execute in an internal eval sandbox.
4. Those eval steps read the original execution artifacts through `{{TARGET_RUN_PATH}}`.
5. A scoring phase produces `evaluation_report.json`.
6. The report is published back under the requested target run folder path inside `evaluation/runs/`.

## Eval Plan Files

The core evaluation files are:

- `evaluation/evaluation_plan.json`
- `evaluation/step_config.json`
- `evaluation/eval_layout.json`
- `learnings/` — shared with execution-plan learnings (see "Learnings And Step Config In Eval Mode" below)

Frontend eval mode loads these files directly. The eval plan is not embedded in the main workflow manifest structure.

Eval steps can be scoped to the route or artifact they apply to:

```json
{
  "id": "eval-bid-submission",
  "title": "Evaluate Bid Submission",
  "description": "Verify bid submission artifacts...",
  "applies_to_routes": [
    { "routing_step_id": "workflow-mode-router", "route_ids": ["route-bid"] }
  ]
}
```

When this field is present, the runtime checks the target run's `routing-evaluation.json` before launching the eval step. Non-applicable checks are skipped and recorded in the final report with `max_score: 0`, so a run is not penalized for route paths it did not take.

Current frontend behavior is implemented in [useEvaluationPlanData.ts](/Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/workflow/hooks/useEvaluationPlanData.ts).

## Eval Mode In The UI

The UI still has a separate eval mode.

Current behavior:
- plan mode shows the main workflow
- eval mode shows `evaluation/evaluation_plan.json`
- eval steps use `evaluation/step_config.json`
- eval layout is separate from the main workflow layout

So evaluation is still a first-class workspace view, not just a hidden background feature.

## Running Evaluation

The runtime entry point is `ExecuteEvaluationOnly(...)`.

Current behavior:
- reads `evaluation/evaluation_plan.json`
- requires a target run folder
- computes an internal eval run folder using the workshop-style `iteration-0` sandbox
- sets `isEvaluationMode = true`
- injects `TARGET_RUN_PATH`
- applies `evaluation/step_config.json`
- runs the evaluation steps
- runs a final scoring phase

This is implemented in [evaluation_execution.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/evaluation_execution.go).

## Internal Eval Sandbox

One of the biggest architecture changes is the eval run folder model.

Evaluation does **not** primarily execute inside:
- `evaluation/runs/{targetRunFolder}/...`

Instead it executes inside an internal sandbox:
- `evaluation/runs/iteration-0`
- or `evaluation/runs/iteration-0/<group>`

The target run folder is still important, but it is the thing being evaluated, not the main place eval steps execute.

This is why the code uses:
- internal eval run folder for execution
- published target run folder for the final report

## TARGET_RUN_PATH

`TARGET_RUN_PATH` is the main bridge between evaluation and the original workflow run.

Current behavior:
- the runtime injects `TARGET_RUN_PATH` as the absolute path to the original execution folder
- eval steps are expected to read the original run artifacts through that variable

For example, the original run might be:
- `runs/iteration-12/production/execution`

The eval step runs in the eval sandbox, but reads the original artifacts through:
- `{{TARGET_RUN_PATH}}`

This is the correct way to reference original execution outputs in eval steps.

## Scoring Phase

After eval steps finish, the system runs a scoring phase that produces a single `evaluation_report.json` covering every eval step.

### Report shape

The on-disk report contains:
- `target_run_folder`, `generated_at`
- `total_score`, `max_possible_score`, `score_percentage` (computed in Go after validation)
- `step_scores[]`, one entry per eval step:
  - `step_id`, `score` (0-10), `max_score`, `reasoning` (≥20 chars), `evidence` (≥10 chars)

There is intentionally **no `summary`, `step_title`, or `success_criteria` field on the score**. UI consumers look those up by `step_id` from `evaluation_plan.json`, which is returned alongside reports by the same API endpoint.

### Two scoring paths

The scoring phase has two paths, controlled by an optional `__evaluation_scoring__` entry in `evaluation/step_config.json`:

**LLM scoring (default)**
- The runtime spins up a single scoring agent (one LLM call covering all eval steps)
- The agent receives every eval step's id/title/description/execution_output in its user prompt
- The agent uses standard workspace tools (`execute_shell_command`, `diff_patch_workspace_file`) to write the report file directly to an absolute path provided in the user prompt
- After the agent's turn loop ends, Go runs the fixed pre-validation schema against the produced file. On failure the runtime feeds the error list back to the same agent as a follow-up user message and lets it fix `evaluation_report.json` in place; this repeats up to 3 attempts before the eval fails.

**learn_code fast path (opt-in)**
- Builder sets `{ "id": "__evaluation_scoring__", "agent_configs": { "declared_execution_mode": "learn_code" } }` in `evaluation/step_config.json`
- On every eval run, the runtime checks for `learnings/__evaluation_scoring__/main.py`
  - If present: writes `scoring_inputs.json` (with all step inputs + resolved `target_run_path`) and execs `python3 main.py <inputs> <output>`. The script writes `evaluation_report.json` deterministically; Go validates it against the same fixed schema. **No LLM call.**
  - If missing or the script fails or the output fails validation: falls back to the LLM scoring path. The LLM is also instructed to author/refine `main.py` so future runs hit the fast path.
- One `main.py` works across all groups — the runtime resolves all per-group paths into `scoring_inputs.json` so the script never hardcodes anything.

### Where the report lands

Whichever path produces it, the runtime writes the report to:
- internal path: `evaluation/runs/{internalEvalRunFolder}/evaluation_report.json`
- published path: `evaluation/runs/{targetRunFolder}/evaluation_report.json`

The publish step is what makes evaluation reports line up with the execution run the user asked about.

### Scoring agent overrides

The reserved `__evaluation_scoring__` step config entry accepts the same `agent_configs` shape as a regular step:
- `use_code_execution_mode` (`*bool`) — overrides the auto-detected code-exec mode (default: provider-based; CLI providers force true)
- `declared_execution_mode` (`"learn_code"`) — opts into the fast-path described above

Implementation lives in [evaluation_scoring_agent.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/evaluation_scoring_agent.go), [evaluation_scoring_learn_code.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/evaluation_scoring_learn_code.go), and `createEvaluationScoringAgent` in [controller_agent_factory.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_agent_factory.go).

## Auto-Evaluation

Evaluation can also run automatically after workflow execution.

Current behavior:
- after a successful batch group execution, the workflow checks whether `evaluation/evaluation_plan.json` exists
- if it exists, auto-evaluation runs for that target run folder
- evaluation failures do not fail the original group execution

This behavior is implemented in:
- [controller_batch_execution.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_batch_execution.go)
- [evaluation_execution.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/evaluation_execution.go)

## Evaluation Costs

The old mental model of evaluation writing a standalone `evaluation/runs/.../token_usage.json` as the main source of truth is no longer the right model.

Current cost architecture:
- evaluation cost data is stored in the `costs/` ledger under the `evaluation` scope
- `/api/workflow/costs` returns eval cost data as `evaluation_token_usage`
- the UI merges execution and evaluation costs when showing workflow run cost summaries

So:
- execution costs -> `costs/execution/...`
- evaluation costs -> `costs/evaluation/...`

For the full cost architecture, see [cost_and_log_measurement.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/cost_and_log_measurement.md).

## Evaluation Reports API

The evaluation report UI is driven by `/api/workflow/evaluation-reports`.

Current behavior:
- scans `evaluation/runs/*`
- finds `evaluation_report.json`
- supports both bare iteration folders and nested group folders
- returns aggregate statistics across reports
- can filter to a specific run folder

This is implemented in [workflow.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/cmd/server/workflow.go).

## Learnings And Step Config In Eval Mode

Evaluation mode has its own step config but shares the learnings namespace with execution steps.

Current behavior:
- step config comes from `evaluation/step_config.json` (separate from `planning/step_config.json`)
- learnings go under `learnings/{stepID}/` — the same folder execution steps write to
- cross-plan step-ID uniqueness between `planning/plan.json` and `evaluation/evaluation_plan.json` is enforced at write time by `validateCrossPlanStepIDUniqueness`, so the shared namespace cannot collide

Eval steps therefore reuse workflow learnings (e.g. a `_global` SKILL.md written by execution steps is visible to eval steps), but keep an independent `step_config.json`.

## Validation Of The Eval Plan

The system includes `validate_evaluation_plan` for checking the file after edits.

Current validation checks include:
- JSON parse validity
- non-empty step list
- unique IDs
- required title and description
- validation of `pre_validation` regex and JSONPath rules where present

This is implemented in [evaluation_helpers.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/evaluation_helpers.go).

## What Changed From Older Docs

The older evaluation doc described a different architecture in a few places.

Current corrections:
- evaluation is file-backed, not centered on a separate designer manager architecture
- eval execution uses an internal `evaluation/runs/iteration-0[/group]` sandbox
- the published report is copied to the target run folder path
- evaluation costs come from the `costs/` ledger, not primarily from legacy per-run token files
- the eval plan is edited and loaded directly from `evaluation/evaluation_plan.json`

## Practical Summary

Use this mental model:

- edit the eval plan in `evaluation/evaluation_plan.json`
- run evaluation against a completed execution run
- eval steps execute in the internal eval sandbox
- eval steps read original execution artifacts via `{{TARGET_RUN_PATH}}`
- scoring publishes `evaluation_report.json` back to the requested target run folder
- costs for eval runs are tracked in the `costs/evaluation/` ledger

## Related Docs

- [cost_and_log_measurement.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/cost_and_log_measurement.md)
- [iteration_run_folder_architecture.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/iteration_run_folder_architecture.md)
- [workflow_builder_interactive.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/workflow_builder_interactive.md)
- [workflow_monitoring.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/workflow_monitoring.md)
