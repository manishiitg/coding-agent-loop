# Workflow Builder Commands And Tools

This document is a compact reference for the current workflow-builder surface. The canonical slash-command prose lives in `agent_go/cmd/server/guidance/templates/`; do not duplicate those templates here.

## Core Model

Workflow improvement has three layers:

- **Plan**: `planning/plan.json` plus `soul/soul.md`. This defines what the workflow does and what "done" means.
- **Eval**: `evaluation/evaluation_plan.json` plus per-run reports. This measures both operational quality and success-criteria achievement.
- **Metrics**: `planning/metrics.json` plus `db/metrics_history.jsonl`. Metrics are evidence for decisions; they do not create a separate action path.

Optimizer actions are deliberately small in number:

- `harden_workflow(group_name?, focus?)`: use when the workflow path is basically right, but prompts, config, validation, KB, learnings, db/report wiring, eval coverage, or metric wiring need repair. It should delete stale `learnings/{step-id}/main.py` for `code_exec` steps and only patch `main.py` for `learn_code`.
- `replan_workflow_from_results(group_name?, focus?)`: use when run/eval/metric evidence shows the workflow path is not aligned with `soul.md` success criteria or outcome metrics. If replan keeps or converts a step to `code_exec`, it should remove stale `learnings/{step-id}/main.py` and clear `lock_code`.
- Eval-plan improvement: use when eval coverage, scoring, structured output, validation schema, or metric-to-eval wiring is weak enough that measurement cannot be trusted.
- `propose_metric` / `retire_metric`: use only for metric-definition cleanup.

## Workshop Modes

- `builder`: creates and debugs workflow structure. Builder defaults steps to `code_exec`; learn-code promotion belongs to Optimizer only after explicit user request, deterministic behavior, and 10+ scenario-covering successful runs.
- `optimizer`: improves existing workflows from `runs/iteration-0`, eval reports, metrics, logs, `builder/improve.md`, and `builder/review.md`.
- `run`: user-facing runtime for Slack/WhatsApp and normal operation. It can answer directly from workflow state, read KB/learnings/db/run artifacts, execute normal or orphan utility steps, or run the full workflow. It should not mutate plan/config/eval/report definitions; durable user-owned runtime context is captured through `capture_context`.
- Reporting authoring is available in Builder and Optimizer through report-plan tools. The legacy Reporting mode remains for compatibility.

## Guidance Tool

Slash commands are one-line UI shortcuts that call:

```text
get_workflow_command_guidance(kind="...", focus?)
```

The returned guidance is the source of truth for the command. Mode validation lives in the guidance registry. Slash-command callers should pass the conversation or request text before the slash command as `focus`, so guidance can apply the user's recent constraints and "based on what we just discussed" intent.

Current guidance kinds:

```text
design-flow
ready-to-optimize
review-plan
review-speed
review-cost
review-code
review-artifact-drift
improve-knowledge
improve-learnings
improve-data
define-success
improve-workflow
improve-evaluation
auto-improve
improve-report
```

## Key Slash Commands

| Command | Mode | Purpose |
|---|---|---|
| `/design-flow` | Builder | Design step flow and context handoffs. |
| `/ready-to-optimize` | Builder | Check whether the workflow is ready to hand to Optimizer. |
| `/review-plan` | Builder, Optimizer, Run | Structural and artifact-sync review through `review_plan`. |
| `/review-code` | Optimizer | Review all saved code artifacts, including learn-code scripts and eval code. |
| `/review-artifact-drift` | Builder, Optimizer | Audit whether learnings, code, KB, db, reports, and eval wiring drifted from recent plan changes. |
| `/review-speed` | Optimizer | Review latency and safe speedups. |
| `/review-cost` | Optimizer | Write a report-only cost review with optional safe reductions. |
| `/improve-knowledge` | Builder, Optimizer | Improve knowledgebase notes with targeted cleanup or cross-step consolidation. |
| `/improve-learnings` | Builder, Optimizer | Improve global learnings with targeted cleanup or current-plan consolidation. |
| `/improve-data` | Builder, Optimizer | Improve durable data contracts, schemas, and report compatibility. |
| `/define-success` | Optimizer | Write workflow profile and propose starter metrics. |
| `/improve-workflow` | Optimizer | Read prior improve/review logs, run/eval/metric/log evidence, then choose harden, replan, eval-plan improvement, metric cleanup, or no action. |
| `/improve-evaluation` | Optimizer | Improve evaluation coverage and rubric quality. |
| `/auto-improve` | Optimizer | Create/update recurring run, harden, and replan-proposal schedules with versioned workshop pulse prompts. |
| `/improve-report` | Builder, Optimizer | Improve report layout, color, density, and widget/data wiring. |

## Common Tool Groups

| Area | Tools |
|---|---|
| Execution | `execute_step`, `query_step`, `stop_step`, `stop_all_executions`, `list_executions`, `run_full_workflow`, `debug_step` |
| Plan/config | `add_regular_step`, `add_routing_step`, `add_human_input_step`, `add_todo_task_step`, `update_*_step`, `delete_plan_steps`, `cleanup_orphan_step_configs`, `update_step_config`, `update_validation_schema` |
| Review | `review_plan`, `review_artifact_sync`, `review_workflow_results`, `review_workflow_timing`, `review_workflow_costs` |
| Optimizer | `harden_workflow`, `replan_workflow_from_results` |
| Metrics | `propose_metric`, `retire_metric` |
| Eval | `validate_evaluation_plan`, `run_full_evaluation` |
| Reports | `get_report_plan`, `upsert_report_widget`, `move_report_widget`, `toggle_report_widget`, `remove_report_widget`, `set_report_theme`, `set_section_layout`, `validate_report_plan`, `preview_report_render` |
| Schedules | `create_schedule`, `create_calendar_schedule`, `update_schedule`, `delete_schedule`, `trigger_schedule`, `get_schedule_runs` |

## Schedule Model

New workflow schedules use two product-level paths:

- Normal Pulse: `mode="workshop"`, `workshop_mode="run"`. If the caller omits messages, the server creates an unattended `run_full_workflow(group_name=...)` message for the configured groups. This path participates in notifications, reports, cost tracking, and prompt-version migration.
- Auto Improve: workshop optimizer pulses created by `/auto-improve`.

`mode="workflow"` remains supported for legacy direct orchestrator schedules, but new normal Pulse schedules should not use it.

On scheduler startup, old unversioned direct schedules (`mode=""` or `mode="workflow"`) are migrated to Normal Pulse and stamped with `schedule_version`. If the workflow's top-level `workflow.json` originally had no `schema_version`, the migrated Pulse also gets a one-time first message that refreshes workflow-owned review/report HTML surfaces to the current `html-output`/`report-plan` guidance before the normal run, then calls `update_schedule` to remove itself so future fires keep only the normal Pulse message. Workflows that already had `schema_version` do not get that UI migration message. New schedules that explicitly request `mode="workflow"` keep the legacy direct path.

## Continuous Improvement Cadence

`/auto-improve` creates or updates three workshop schedules:

- Run schedule: `mode="workshop"`, `workshop_mode="run"`, message can call `run_full_workflow(group_name=...)`, execute targeted/orphan steps, or answer directly from KB/learnings/db/run state when that is the scheduled job.
- Harden schedule: `mode="workshop"`, `workshop_mode="optimizer"`, frequent message performs a short cadence/group-scope check, then calls `get_workflow_command_guidance(kind="improve-workflow", focus=...)` with a harden-only focus and follows that canonical improvement flow.
- Replan-proposal schedule: `mode="workshop"`, `workshop_mode="optimizer"`, less-frequent message reviews accumulated evidence and writes a proposal to `builder/improve.html` without rewriting `planning/plan.json`.

For active workflows, harden should normally run after every run or every two runs; replan-proposal starts less frequently, around every three or four runs. Weekly cadence is only appropriate when the workflow itself runs weekly or the user explicitly asks for low-touch maintenance.

Workshop schedule messages are stamped with `prompt_version` in `workflow.json`. Legacy schedules with no or old prompt version are not rewritten blindly; each stale workshop fire starts with an upgrade turn until the agent rewrites the saved pulse through `update_schedule`, preserving cadence and group scope where possible.
