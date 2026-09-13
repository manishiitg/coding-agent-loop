---
name: critical-journey-validation
description: Build AgentWorks browser regression validation using existing application knowledge, test setup, and verified locators. Run approved journeys, investigate failures, and publish durable evidence-backed results.
---

# Critical Journey Validation

## Outcome

Create repeatable regression coverage for agreed critical journeys using the application's existing browser foundation, canonical locators, recording policy, durable results, and live report.

## When to use

Use after Basic Browser Setup or with an equivalent verified configuration. Use it to add, execute, and investigate approved journeys while preserving the customer's runner, process, and approval rules.

## Required inputs

Resolve the application profile, canonical suite and locator helpers, journeys and expected outcomes, account/test-data boundaries, execution and diagnostic limits, recording retention, and reporting preferences.

## Plan and AgentWorks tools

Use scripted steps for saved Playwright execution and deterministic persistence. Keep investigation and evidence-based classification in a coherent message sequence. Add branches only when failure classifications lead to different actions. Follow the shared plan/tool guide and live platform schemas.

## Knowledge and persistence

Reuse application knowledge rather than copying it into this skill. Add only verified locator, route, or setup discoveries to knowledgebase notes. Keep executable changes in canonical tests/helpers, results in workflow DB tables, and videos, console/network logs, screenshots, and traces in durable assets.

## Validation and reporting

Require a terminal result for every expected journey and attempt, original assertions, source/config revisions, and video plus console/network evidence status. Required skipped, blocked, missing cases, or required evidence gaps cannot produce a passing run. Verify dashboard summary, filters, failure detail, history, logs, and video playback.

## Guardrails

Do not weaken assertions, silently change approved outcomes, hide missing coverage, or overwrite original failure evidence. Keep diagnostic retries bounded and distinguish product defects, test defects, environment blockers, and unresolved findings.

## Read details when needed

- [Workflow design and outcomes](../../references/workflow-design-and-outcomes.md): goals, metrics, and current-versus-separate workflow decisions.
- [AgentWorks plan and tools](../references/agentworks-plan-and-tools.md): step boundaries, platform tools, stores, and execution.
- [Evidence capture](../references/evidence-capture.md): video, console/network logs, redaction, storage, and dashboard behavior.
- [Execution and results](references/execution-and-results.md): builder wiring, KB updates, classifications, and status rules.
- [Regression workflow](references/regression-workflow.md): adaptable steps, routes, runner, and recording lifecycle.
- [Dashboard](references/dashboard.md): durable data, recordings, status, and Report actions.
- [Example journeys](examples/journeys.json): illustrative scenarios, not approved behavior.
- [Catalog metadata](playbook.json): presentation and optional recommendations.

## Completion contract

Return installed playbook, profile, suite, and journey revisions; material customer overrides; actual case/attempt results; KB, report, and evidence locations; capability resolution; and unresolved limitations. Schedules, PR triggers, fixes, and external delivery require configured scope.
