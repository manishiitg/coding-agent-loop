---
name: scheduled-regression-synthetic-monitoring
description: Configure AgentWorks to run approved browser journeys on a schedule, retain trends, and notify on actionable changes. Use for ongoing regression checks or controlled synthetic monitoring.
---

# Scheduled Regression and Synthetic Monitoring

## Outcome

Create an unattended schedule that runs a saved browser QA route for explicit groups, preserves comparable history and evidence, and delivers bounded notifications for actionable changes.

## When to use

Use only after the selected browser suite and its unattended authentication, fixtures, cleanup, evidence, and report behavior have passed manually. Synthetic checks must target an authorized environment.

## Required inputs

Resolve the exact saved route, groups, cadence/calendar and timezone, environment, concurrency and timeout limits, unattended human-decision defaults, retention, notification conditions/destinations, ownership, and pause/escalation policy.

## Plan and AgentWorks tools

Reuse the existing validation route; scheduling is configuration, not a duplicate test workflow. Inspect with `list_schedules`, create or update through current schedule tools, and test with `trigger_schedule`. Pass route selections and safe human defaults explicitly. API-triggered CI work belongs to the Release/PR Gate.

## Knowledge and persistence

Persist every scheduled run and notification receipt with schedule, route, group, build/environment, lifecycle, result, and evidence references. Keep schedule policy in configuration and reusable application facts in KB notes. Never place secret values in messages or reports.

## Validation and reporting

Verify one controlled scheduled/triggered run, overlap behavior, timeout/cancellation, cleanup, history, report trends, and notification deduplication. The dashboard shows schedule and route health, environment, latest result, duration, evidence, run history, and notification state. Missing runs, stale data, and notification failures remain visible and cannot imply health.

## Guardrails

Do not schedule an unproven route, wait indefinitely for humans, auto-approve changes, overload customer systems, or notify on every unchanged run. Provide pause/disable ownership and retain the original failing evidence.

## Read details when needed

- [AgentWorks plan and tools](../references/agentworks-plan-and-tools.md): execution and persistence choices.
- [Evidence capture](../references/evidence-capture.md): unattended evidence handling.
- [Scheduling guide](references/scheduling-workflow.md): readiness, AgentWorks tools, history, and notifications.
- [Example schedule policy](examples/schedule-policy.json): fictional configuration shape.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook version, schedule ID/type/cadence/timezone, selected route and groups, customer overrides, controlled trial receipt/result, retention/report/notification configuration, ownership, capability resolution, and blockers.
