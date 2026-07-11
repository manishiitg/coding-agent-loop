# Workflow Builder Commands And Tools

This document is a compact reference for the current workflow-builder surface. The canonical slash-command prose lives in `agent_go/cmd/server/guidance/templates/`; do not duplicate those templates here.

## Core Model

Workflow improvement has three layers:

- **Intent + plan**: `soul/soul.md` defines stable objective/success criteria and explicit user constraints; `planning/plan.json` defines the current, revisable implementation attempt.
- **Eval**: `evaluation/evaluation_plan.json` plus per-run reports. This measures success-criteria achievement (operational quality is Pulse triage's per-run job).
- **Goal card**: the per-criterion Met/Short/At-risk card in `builder/improve.html`, maintained agentically by Pulse from eval reports + `soul.md`. It is the durable goal signal over runs; there is no separate numeric metrics layer.

Optimizer actions are deliberately small in number:

- `harden_workflow(group_name?, focus?)`: use when the workflow path is basically right, but prompts, config, validation, KB, learnings, db/report wiring, or eval coverage need repair. It should delete stale `learnings/{step-id}/main.py` for `code_exec` steps and only patch `main.py` for `learn_code`.
- Goal Advisor proposals and experiments: use for Goal recovery, a capped strategy, or periodic healthy 10x/headroom review. Scheduled Pulse starts this through `run_goal_advisor_review(...)`; material plan changes are proposed through `create_human_input_request(source="goal_advisor", ...)` and later applied with normal plan/config/eval/report tools only after approval. `builder/improve.html` holds at most one active `.advisor-experiment` card, which preserves the current baseline and advances through proposal, approval, running, measurement, and a terminal adopted/rejected/retired outcome. Pulse schedules the next meaningful checkpoint instead of generating bold ideas every run.
- Eval-plan improvement: use when eval coverage, scoring, structured output, or validation schema is weak enough that measurement cannot be trusted, or eval cost is out of proportion to run cost.

All plan/design reviews and KB/learnings/DB/eval/report/workflow improvement commands load the shared `assumption-audit` reference. It prevents repeated agent-written choices from becoming accidental constraints: explicit user constraints and verified external facts are preserved; current design choices remain revisable; unsupported assumptions are corrected within command boundaries or surfaced once under Pulse's **Assumptions challenged** for Goal Advisor/user judgment.

## Workshop Modes

- `builder`: creates and debugs workflow structure. Builder defaults steps to `code_exec`; learn-code promotion belongs to Optimizer only after explicit user request, deterministic behavior, and 10+ scenario-covering successful runs.
- `optimizer`: improves existing workflows from `runs/iteration-0`, eval reports, logs, and `builder/improve.html`.
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
design-plan
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
| `/design-plan` | Builder | Design step flow and context handoffs. |
| `/ready-to-optimize` | Builder | Check whether the workflow is ready to hand to Optimizer. |
| `/review-plan` | Builder, Optimizer, Run | Structural and artifact-sync review through `review_plan`. |
| `/review-code` | Optimizer | Review all saved code artifacts, including learn-code scripts and eval code. |
| `/review-artifact-drift` | Builder, Optimizer | Audit whether learnings, code, KB, db, reports, and eval wiring drifted from recent plan changes. |
| `/review-speed` | Optimizer | Review latency and safe speedups. |
| `/review-cost` | Optimizer | Review cost and safe reductions. |
| `/improve-knowledge` | Builder, Optimizer | Improve knowledgebase notes with targeted cleanup or cross-step consolidation. |
| `/improve-learnings` | Builder, Optimizer | Improve global learnings with targeted cleanup or current-plan consolidation. |
| `/improve-data` | Builder, Optimizer | Improve durable data contracts, schemas, and report compatibility. |
| `/define-success` | Optimizer | Confirm the goal, write the workflow profile, and seed the Pulse goal card. |
| `/improve-workflow` | Optimizer | Read prior improve/review logs and run/eval/log evidence, then choose harden, replan, eval-plan improvement, or no action. |
| `/improve-evaluation` | Optimizer | Improve evaluation coverage and rubric quality. |
| `/auto-improve` | Optimizer | Create/update frequent Run-mode and Optimizer-mode schedules. |
| `/improve-report` | Builder, Optimizer | Improve report layout, color, density, and widget/data wiring. |

## Common Tool Groups

| Area | Tools |
|---|---|
| Execution | `execute_step`, `query_step`, `stop_step`, `stop_all_executions`, `list_executions`, `run_full_workflow`, `debug_step` |
| Plan/config | `add_regular_step`, `add_routing_step`, `add_human_input_step`, `add_todo_task_step`, `update_*_step`, `delete_plan_steps`, `cleanup_orphan_step_configs`, `update_step_config`, `update_validation_schema` |
| Review | `review_plan`, `review_artifact_sync`, `review_workflow_results`, `review_workflow_timing`, `review_workflow_costs` |
| Optimizer | `harden_workflow`, `run_goal_advisor_review`, Goal Advisor proposal cards |
| Eval | `validate_evaluation_plan`, `run_full_evaluation` |
| Reports | `get_report_plan`, `upsert_report_widget`, `move_report_widget`, `toggle_report_widget`, `remove_report_widget`, `set_report_theme`, `set_section_layout`, `validate_report_plan`, `preview_report_render` |
| Schedules | `create_schedule`, `create_calendar_schedule`, `update_schedule`, `delete_schedule`, `trigger_schedule`, `get_schedule_runs` |

## Continuous Improvement Cadence

`/auto-improve` creates or updates two workshop schedules:

- Run schedule: `mode="workshop"`, `workshop_mode="run"`, message can call `run_full_workflow(group_name=...)`, execute targeted/orphan steps, or answer directly from KB/learnings/db/run state when that is the scheduled job.
- Improve schedule: `mode="workshop"`, `workshop_mode="optimizer"`, message performs a short cadence/group-scope check, then calls `get_workflow_command_guidance(kind="improve-workflow", ...)` and follows that canonical improvement flow. It does not duplicate the harden/replan decision model inline.

For active workflows, the improve schedule should normally run after every run or every two runs. Weekly cadence is only appropriate when the workflow itself runs weekly or the user explicitly asks for low-touch maintenance.
