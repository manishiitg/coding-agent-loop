# Scheduled regression and synthetic monitoring

## Prove unattended readiness first

The selected route must already complete manually with the exact groups, secret references, fixtures, cleanup, evidence policy, and report persistence intended for unattended runs. Resolve all human inputs in advance. Every human branch needs a safe `default_route_id`; schedules must not wait for approval or auto-approve a consequential action.

Confirm environment authorization, expected load, concurrency, maintenance windows, account ownership, retention/storage limits, notification owner, and pause procedure. Monitoring a production environment does not authorize destructive test data or unrestricted capture.

## Configure through AgentWorks

Read the current `builder-reference` schedules and execution guidance. Use `list_schedules` before adding anything. Use `create_schedule` for recurring cron behavior or `create_calendar_schedule` for explicit future dates; use `update_schedule`, `delete_schedule`, `trigger_schedule`, and `get_schedule_runs` for lifecycle and verification. Preserve an equivalent schedule rather than creating a duplicate.

Bind the schedule to explicit variable groups and the existing route selection. Use the deployment's timezone-aware schedule contract and user-visible message. A scheduled run executes the saved plan from the beginning; do not create a duplicate “scheduled” copy of the test steps.

## Results, trends, and notifications

Persist schedule ID/type/revision, trigger/run IDs, planned and actual time, route/group, environment/build, lifecycle, QA status, expected/executed cases, evidence, duration, and notification receipts. Treat missed, cancelled, timed-out, overlapping, or persistence-failed runs as distinct operational states.

Default notification behavior should be quiet for unchanged healthy state. Notify on a new actionable failure, worsening state, recovery when requested, schedule failure, repeated missing run, or required user action. Deduplicate by stable finding/state key and record attempted versus delivered status. External destinations require configured authorization.

The dashboard shows current health, last successful and failed runs, run cadence, latency, trends, evidence, consecutive failures, notification state, and owner/pause controls supported by the product.

## Acceptance cases

Test one controlled trigger, healthy run, new failure, unchanged repeated failure, recovery, timeout, overlap, expired auth, missing required route/group, notification failure, paused schedule, timezone boundary, and retention cleanup. Verify no duplicate schedule, no alert storm, no human wait, and no result loss.
