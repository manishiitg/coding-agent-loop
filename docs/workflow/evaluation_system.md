# Evaluation System

This doc describes how workflow evaluation works now.

The current system is file-backed and execution-oriented:
- the evaluation plan lives in `evaluation/evaluation_plan.json`
- evaluation step config lives in `evaluation/step_config.json`
- evaluation execution runs in an internal sandbox under `evaluation/runs/iteration-0[/group]`
- the final report is published to `evaluation/runs/{targetRunFolder}/evaluation_report.json`

## What Evaluation Is For

Evaluation is the workflow's goal-measurement layer for completed execution runs.

It answers:
- did a target run actually satisfy the success criteria in `soul/soul.md`?
- what did each evaluation step conclude, with what evidence?

It is separate from:
- execution-time pre-validation (mechanical run-shape checks)
- Pulse per-run triage (operational breakage: errors, skipped steps, empty artifacts, hallucinated successes)
- step learning
- workflow execution itself

Division of labor: **Pulse owns "did it run right"; evals own "did it achieve the goal."** Eval steps should map one-to-one to success criteria and never duplicate operational checks — see the `evaluation-plan` guidance template for the authoring contract. Pulse reads the eval report each run and maps verdicts onto the per-criterion goal card in `builder/improve.html`, which is the durable goal signal (there is no separate numeric metrics layer).

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

Current frontend behavior is implemented in [useEvaluationPlanData.ts](../../frontend/src/components/workflow/hooks/useEvaluationPlanData.ts).

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

This is implemented in [evaluation_execution.go](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/evaluation_execution.go).

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

## Report Phase

After eval steps finish, the runtime builds a single `evaluation_report.json` covering every eval step — directly in Go (`runEvaluationReportPhase` in `evaluation_execution.go`). **There is no scoring agent, no combined LLM scoring call, and no learn_code scoring fast path** (all removed); each eval step's own structured output is its verdict.

### Report shape

The on-disk report contains:
- `target_run_folder`, `generated_at`
- `step_scores[]`, one entry per eval step:
  - `step_id`
  - placeholder `score: 0` / `max_score: 0` with fixed `reasoning`/`evidence` text pointing readers at `output_content` ("Final scoring is disabled; this report preserves the eval step output for review")
  - `output_content` — the eval step's own structured output, attached from the first of: `output_content.json`, the step's `context_output` file, a `pre_validation` file, or `context_output.json` in the eval sandbox (`enrichEvaluationReportWithStepOutputs`)
- eval steps skipped by `applies_to_routes` appear as skipped entries with `max_score: 0`, so a run is not penalized for route paths it did not take

`output_content` is the source of truth for each eval step's verdict: the step should emit its own `score`, `max_score`, `reasoning`, and `evidence` (plus any domain-specific judgment dimensions) as structured output, enforced by the step's validation schema. Consumers — Pulse triage, the goal card in `builder/improve.html`, the scheduled improve loop, the UI — read the per-step verdicts; nothing numeric is aggregated downstream.

### Where the report lands

Whichever path produces it, the runtime writes the report to:
- internal path: `evaluation/runs/{internalEvalRunFolder}/evaluation_report.json`
- published path: `evaluation/runs/{targetRunFolder}/evaluation_report.json`

The publish step is what makes evaluation reports line up with the execution run the user asked about.

## Auto-Evaluation

Evaluation can also run automatically after workflow execution.

Current behavior:
- after a successful batch group execution, the workflow checks whether `evaluation/evaluation_plan.json` exists
- if it exists, auto-evaluation runs for that target run folder
- evaluation failures do not fail the original group execution

This behavior is implemented in:
- [controller_batch_execution.go](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/controller_batch_execution.go)
- [evaluation_execution.go](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/evaluation_execution.go)

## Evaluation Costs

The old mental model of evaluation writing a standalone `evaluation/runs/.../token_usage.json` as the main source of truth is no longer the right model.

Current cost architecture:
- evaluation cost data is stored in the `costs/` ledger under the `evaluation` scope
- `/api/workflow/costs` returns eval cost data as `evaluation_token_usage`
- the UI merges execution and evaluation costs when showing workflow run cost summaries

So:
- execution costs -> `costs/execution/...`
- evaluation costs -> `costs/evaluation/...`

For the full cost architecture, see [cost_and_log_measurement.md](./cost_and_log_measurement.md).

## Evaluation Reports API

The evaluation report UI is driven by `/api/workflow/evaluation-reports`.

Current behavior:
- scans `evaluation/runs/*`
- finds `evaluation_report.json`
- supports both bare iteration folders and nested group folders
- returns aggregate statistics across reports
- can filter to a specific run folder

This is implemented in [workflow.go](../../agent_go/cmd/server/workflow.go).

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

This is implemented in [evaluation_helpers.go](../../agent_go/pkg/orchestrator/agents/workflow/step_based_workflow/evaluation_helpers.go).

## What Changed From Older Docs

The older evaluation doc described a different architecture in a few places.

Current corrections:
- evaluation is file-backed, not centered on a separate designer manager architecture
- eval execution uses an internal `evaluation/runs/iteration-0[/group]` sandbox
- the published report is copied to the target run folder path
- evaluation costs come from the `costs/` ledger, not primarily from legacy per-run token files
- the eval plan is edited and loaded directly from `evaluation/evaluation_plan.json`
- the combined LLM scoring agent and the `__evaluation_scoring__` learn_code fast path were removed — the report is assembled in Go from per-step `output_content`
- the numeric metrics layer (`planning/metrics.json`, `db/metrics_history.jsonl`, `propose_metric`/`retire_metric`) was removed (2026-07-01) — the goal signal is the per-criterion goal card in `builder/improve.html`, maintained agentically by Pulse from eval reports + `soul.md`

## Practical Summary

Use this mental model:

- edit the eval plan in `evaluation/evaluation_plan.json`
- run evaluation against a completed execution run
- eval steps execute in the internal eval sandbox
- eval steps read original execution artifacts via `{{TARGET_RUN_PATH}}`
- scoring publishes `evaluation_report.json` back to the requested target run folder
- costs for eval runs are tracked in the `costs/evaluation/` ledger

## Related Docs

- [cost_and_log_measurement.md](./cost_and_log_measurement.md)
- [iteration_run_folder_architecture.md](./iteration_run_folder_architecture.md)
- [workflow_builder_interactive.md](./workflow_builder_interactive.md)
- [workflow_monitoring.md](./workflow_monitoring.md)
