# Workflow Scheduling

Workflow scheduling is a first-class workflow feature.

The current system is fully file-backed:

- schedule definitions live in `workflow.json`
- global scheduler state lives in `config/scheduler.json`
- per-workflow schedule run history lives in `schedule-runs.json`

There is no DB-backed workflow scheduler architecture anymore.

## Source Of Truth

Each workflow manifest can define zero or more schedules:

- `Workflow/<name>/workflow.json`

Current manifest schedule fields are defined in [workflow_manifest.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/cmd/server/workflow_manifest.go):

- `id`
- `name`
- `description`
- `cron_expression`
- `timezone`
- `enabled`
- `trigger_payload`
- `group_ids`
- `mode`
- `messages`
- `workshop_mode`

Validation rules that matter now:

- every schedule must have an `id`
- every schedule must have a valid `cron_expression`
- every schedule must include at least one valid `group_id`
- `group_ids` are validated against `variables/variables.json`

That means schedules are always group-aware now. A schedule without valid group selection is rejected.

## Storage Layout

### Workflow-local schedule definitions

Schedule definitions are persisted in:

- `Workflow/<name>/workflow.json`

They belong to the workflow manifest alongside capabilities, ownership, and execution defaults.

### Global scheduler config

Global scheduler pause and execution flags are persisted in:

- `config/scheduler.json`

Current fields are defined in [scheduler_config_store.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/cmd/server/scheduler_config_store.go):

- `globally_paused`
- `paused_at`
- `paused_by`
- `updated_at`
- `execution_enabled`
- `disabled_via_env`
- `disabled_reason`

Important distinction:

- `globally_paused` is persisted user-controlled state
- `execution_enabled` is computed runtime state

If `SCHEDULER_ENABLED=false`, automatic cron execution is disabled on that server, but manual trigger still works.

### Per-workflow run history

Schedule run history is persisted per workflow in:

- `Workflow/<name>/schedule-runs.json`

Entries are defined in [schedule_runs.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/cmd/server/schedule_runs.go):

- `id`
- `schedule_id`
- `run_folder`
- `session_id`
- `status`
- `error`
- `duration_ms`
- `group_ids`
- `started_at`
- `completed_at`

The file keeps the newest entries first and is capped at 200 runs.

## Runtime Model

The scheduler service is implemented in [scheduler.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/cmd/server/scheduler.go).

On startup it:

- scans workflow workspaces for `workflow.json`
- loads enabled manifest schedules into `gocron`
- indexes `schedule_id -> workspace`
- computes next-run timestamps
- marks stale `running` entries in `schedule-runs.json` as `error` after restart

Runtime-only state is kept in memory per schedule:

- last status
- last run time
- next run time
- last session id
- last error
- last duration
- run count
- consecutive failures

That runtime state is not written back into `workflow.json`.

## Execution Modes

Schedules support two execution modes.

### `workflow` mode

This is the standard orchestrator path.

The scheduler builds a normal workflow request with:

- `agent_mode = workflow`
- workflow capabilities from the manifest
- `triggered_by = cron`
- `execution_options.run_mode = use_same_run`
- `execution_options.selected_run_folder = iteration-0`
- `execution_options.execution_strategy = start_from_beginning`
- `execution_options.enabled_group_ids = schedule.group_ids`

The scheduler then waits for the workflow execution to finish and captures the real run folder from the active execution registry.

### `workshop` mode

This uses the interactive builder/workshop path instead of the normal workflow executor.

The scheduler builds a request with:

- `agent_mode = workflow_phase`
- `phase_id = workflow-builder`
- `triggered_by = cron`
- `execution_options.run_mode = use_same_run`
- `execution_options.selected_run_folder = iteration-0`
- `execution_options.execution_strategy = start_from_beginning_no_human`
- `execution_options.workshop_mode = schedule.workshop_mode || runner`
- `execution_options.enabled_group_ids = schedule.group_ids`

Then it sends the configured `messages[]` one by one and waits for the workshop session to become idle after each message.

If no messages are provided, it defaults to:

- `Run the full workflow using run_full_workflow tool.`

## Groups And Run Folders

Schedules are always tied to variable groups.

Current implications:

- group IDs are required at save time
- scheduled executions pass those group IDs into workflow execution options
- normal workflow-mode schedules start from `iteration-0`
- workshop-mode schedules also start from `iteration-0`

There is helper logic for resolving a group-scoped workshop run folder, but the standard workshop scheduler request still starts from `iteration-0`.

That means scheduled runs follow the same broader run-folder model documented in [iteration_run_folder_architecture.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/iteration_run_folder_architecture.md).

## Auto Report Generation

Workshop schedules have one extra behavior.

If:

- `mode = workshop`
- `workshop_mode` is `runner` or omitted
- none of the scheduled messages explicitly invoke `run_full_report`

then the scheduler tries to auto-generate the final report after the workshop message sequence completes.

That flow lives in [scheduler.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/cmd/server/scheduler.go#L684).

One nuance in current code:

- final report generation requires a group-scoped run folder like `iteration-0/<group>`
- the workshop scheduler path itself still initializes from plain `iteration-0`

So report auto-generation for workshop schedules is coupled to the resolved run-folder shape, not just to the presence of a schedule.

## APIs

Scheduler APIs are registered in [scheduler_routes.go](/Users/mipl/ai-work/mcp-agent-builder-go/agent_go/cmd/server/scheduler_routes.go):

- `GET /api/scheduler/config`
- `PUT /api/scheduler/config`
- `GET /api/scheduler/jobs`
- `POST /api/scheduler/jobs`
- `GET /api/scheduler/jobs/{id}`
- `PUT /api/scheduler/jobs/{id}`
- `DELETE /api/scheduler/jobs/{id}`
- `POST /api/scheduler/jobs/{id}/enable`
- `POST /api/scheduler/jobs/{id}/disable`
- `POST /api/scheduler/jobs/{id}/trigger`
- `POST /api/scheduler/jobs/{id}/stop`
- `GET /api/scheduler/jobs/{id}/runs`

The API response shape is a compatibility wrapper around:

- manifest schedule definition
- in-memory runtime state
- per-workflow run history

## UI Surfaces

The current frontend scheduling surfaces are:

- [SchedulePresetPopup.tsx](/Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/SchedulePresetPopup.tsx)
- [WorkflowScheduleRunsPanel.tsx](/Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/components/scheduler/WorkflowScheduleRunsPanel.tsx)
- [scheduler.ts](/Users/mipl/ai-work/mcp-agent-builder-go/frontend/src/api/scheduler.ts)

The UI supports:

- creating and editing workflow schedules
- selecting variable groups
- enabling and disabling schedules
- manual trigger
- stop for active sessions
- viewing schedule run history
- drilling into logs, costs, evaluation, and final output for scheduled runs
- global scheduler pause state and disabled-via-env state

## Current Architecture Summary

Use this mental model:

- `workflow.json` defines what should run and when
- `config/scheduler.json` controls whether automatic cron execution is paused or disabled on this server
- `schedule-runs.json` records what actually happened
- scheduler runtime state is mostly in memory
- scheduled execution still runs through the same workflow or workshop engines as manual execution

Related docs:

- [workflow_manifest_architecture.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/workflow_manifest_architecture.md)
- [iteration_run_folder_architecture.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/iteration_run_folder_architecture.md)
- [workflow_builder_interactive.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/workflow_builder_interactive.md)
- [workflow_monitoring.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/workflow_monitoring.md)
- [cost_and_log_measurement.md](/Users/mipl/ai-work/mcp-agent-builder-go/docs/workflow/cost_and_log_measurement.md)
